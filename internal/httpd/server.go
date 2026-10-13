// Package httpd serves the boot side over HTTP: the iPXE menu, iPXE binaries
// (for UEFI HTTP boot), raw ISOs and files inside ISOs. These routes are
// unauthenticated because firmware has no way to log in.
package httpd

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"ipxemultiboot/internal/bootfiles"
	"ipxemultiboot/internal/events"
	"ipxemultiboot/internal/library"
)

type Boot struct {
	lib *library.Library
	hub *events.Hub

	mu     sync.Mutex
	recent map[string]time.Time // dedupes boot events from repeated range requests
}

func NewBoot(lib *library.Library, hub *events.Hub) *Boot {
	return &Boot{lib: lib, hub: hub, recent: map[string]time.Time{}}
}

// Register adds the boot routes to mux.
func (b *Boot) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /boot.ipxe", b.handleStage1)
	mux.HandleFunc("GET /menu.ipxe", b.handleMenu)
	mux.Handle("GET /ipxe/", http.StripPrefix("/ipxe/", http.FileServerFS(bootfiles.FS)))
	mux.HandleFunc("GET /isos/{id}", b.handleRawISO)
	mux.HandleFunc("GET /iso/{id}/{path...}", b.handleISOFile)
}

func baseURL(r *http.Request) string {
	return "http://" + r.Host
}

// normMAC turns iPXE's hexhyp form (52-54-00-..) into 52:54:00:...
func normMAC(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "-", ":"))
	if _, err := net.ParseMAC(s); err != nil {
		return ""
	}
	return s
}

func clientIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// handleStage1 is what DHCP points iPXE at. It re-requests the menu with the
// client's identity so activity can be attributed to a machine.
func (b *Boot) handleStage1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "#!ipxe\nchain --replace "+baseURL(r)+"/menu.ipxe?mac=${netX/mac:hexhyp}&platform=${platform}&arch=${buildarch}\n")
}

func (b *Boot) handleMenu(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	isos := b.lib.List()
	b.hub.Publish(events.Event{Kind: events.Menu, MAC: normMAC(q.Get("mac")), IP: clientIP(r), Stage: events.StageMenu,
		Message: fmt.Sprintf("Boot menu shown (%s %s, %d ISOs)", q.Get("platform"), q.Get("arch"), len(isos))})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, menuScript(baseURL(r), isos))
}

// bootEvent publishes "Booting X" at most once a minute per client and ISO.
func (b *Boot) bootEvent(r *http.Request, iso *library.ISO, how string) {
	mac := normMAC(r.URL.Query().Get("mac"))
	ip := clientIP(r)
	key := ip + "|" + iso.ID
	b.mu.Lock()
	last, seen := b.recent[key]
	now := time.Now()
	if seen && now.Sub(last) < time.Minute {
		b.mu.Unlock()
		return
	}
	b.recent[key] = now
	for k, t := range b.recent {
		if now.Sub(t) > time.Hour {
			delete(b.recent, k)
		}
	}
	b.mu.Unlock()
	b.hub.Publish(events.Event{Kind: events.Boot, MAC: mac, IP: ip, Stage: events.StageBoot, ISO: iso.Name, Message: "Booting " + iso.Name})
	slog.Info("boot", "iso", iso.Name, "method", how, "mac", mac, "ip", ip)
}

func (b *Boot) handleRawISO(w http.ResponseWriter, r *http.Request) {
	iso := b.lib.Get(r.PathValue("id"))
	if iso == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(iso.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	if r.URL.Query().Has("mac") {
		b.bootEvent(r, iso, "sanboot")
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, iso.Name, iso.ModTime, f)
}

func (b *Boot) handleISOFile(w http.ResponseWriter, r *http.Request) {
	iso := b.lib.Get(r.PathValue("id"))
	if iso == nil || iso.Image == nil {
		http.NotFound(w, r)
		return
	}
	p := path.Clean("/" + r.PathValue("path"))
	e, err := iso.Image.Stat(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if e.IsDir {
		entries, err := iso.Image.ReadDir(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<pre>\n")
		for _, c := range entries {
			name := c.Name
			if c.IsDir {
				name += "/"
			}
			fmt.Fprintf(w, "<a href=\"%s\">%s</a>\n", name, name)
		}
		fmt.Fprintf(w, "</pre>\n")
		return
	}
	if iso.Recipe != nil && p == iso.Recipe.Kernel {
		b.bootEvent(r, iso, iso.Recipe.Family)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, e.Name, e.ModTime, iso.Image.Reader(e))
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush keeps Server-Sent Events working through the logging wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// LogRequests logs boot traffic at info and everything else at debug.
func LogRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		h.ServeHTTP(sw, r)
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "bytes", sw.bytes,
			"dur", time.Since(start).Round(time.Millisecond), "peer", r.RemoteAddr}
		if rg := r.Header.Get("Range"); rg != "" {
			attrs = append(attrs, "range", rg)
		}
		p := r.URL.Path
		bootPath := p == "/boot.ipxe" || p == "/menu.ipxe" || strings.HasPrefix(p, "/iso/") || strings.HasPrefix(p, "/ipxe/")
		if bootPath || (strings.HasPrefix(p, "/isos/") && r.Header.Get("Range") == "") {
			slog.Info("http", attrs...)
		} else {
			slog.Debug("http", attrs...)
		}
	})
}
