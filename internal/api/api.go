// Package api is the JSON API used by the web interface.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"ipxemultiboot/internal/app"
	"ipxemultiboot/internal/events"
	"ipxemultiboot/internal/store"
)

const sessionCookie = "ipxemultiboot_session"

type API struct {
	app *app.App
}

func New(a *app.App) *API { return &API{app: a} }

type ctxKey struct{}

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

// Register adds the /api routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup/status", a.setupStatus)
	mux.HandleFunc("POST /api/setup/register", a.register)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)

	auth := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.requireAuth(h)) }
	auth("GET /api/auth/me", a.me)
	auth("PUT /api/auth/password", a.changePassword)
	auth("GET /api/settings", a.getSettings)
	auth("PUT /api/settings", a.putSettings)
	auth("POST /api/setup/complete", a.completeSetup)
	auth("GET /api/status", a.status)
	auth("GET /api/interfaces", a.interfaces)
	auth("GET /api/network/check", a.networkCheck)
	auth("POST /api/fs/check", a.checkDir)
	auth("GET /api/isos", a.listISOs)
	auth("PUT /api/isos/upload", a.uploadISO)
	auth("DELETE /api/isos/{id}", a.deleteISO)
	auth("GET /api/clients", a.listClients)
	auth("DELETE /api/clients/{mac}", a.deleteClient)
	auth("GET /api/events", a.recentEvents)
	auth("GET /api/events/stream", a.streamEvents)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// csrfOK requires a custom header on state-changing requests. Browsers cannot
// send it cross-origin without a CORS preflight, which we never approve.
func csrfOK(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	return r.Header.Get("X-IPXEMultiboot") == "1"
}

func (a *API) requireAuth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		u, err := a.app.Store.SessionUser(r.Context(), c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		if !csrfOK(r) {
			writeError(w, http.StatusForbidden, "missing X-IPXEMultiboot header")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

func (a *API) startSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	token, expires, err := a.app.Store.CreateSession(r.Context(), u.ID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	return nil
}

// Setup & auth

func (a *API) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := a.app.Store.UserCount(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s := a.app.Settings()
	writeJSON(w, http.StatusOK, map[string]any{
		"needsRegistration": n == 0,
		"setupComplete":     s.SetupComplete,
		"version":           a.app.Version,
		"language":          s.Language,
		"theme":             s.Theme,
	})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func validateCredentials(c credentials) error {
	c.Username = strings.TrimSpace(c.Username)
	if l := utf8.RuneCountInString(c.Username); l < 3 || l > 64 {
		return errors.New("username must be 3–64 characters")
	}
	if utf8.RuneCountInString(c.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(c.Password) > 72 {
		return errors.New("password must be at most 72 bytes")
	}
	return nil
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "missing X-IPXEMultiboot header")
		return
	}
	if n, err := a.app.Store.UserCount(r.Context()); err == nil && n > 0 {
		writeError(w, http.StatusConflict, store.ErrAlreadySetUp.Error())
		return
	}
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateCredentials(c); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := a.app.Store.CreateFirstUser(r.Context(), strings.TrimSpace(c.Username), c.Password)
	if errors.Is(err, store.ErrAlreadySetUp) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.startSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slog.Info("admin account created", "user", u.Username)
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "missing X-IPXEMultiboot header")
		return
	}
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := a.app.Store.Authenticate(r.Context(), strings.TrimSpace(c.Username), c.Password)
	if err != nil {
		time.Sleep(500 * time.Millisecond) // slow down guessing
		writeError(w, http.StatusUnauthorized, store.ErrBadCredential.Error())
		return
	}
	if err := a.startSession(w, r, u); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.app.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userFrom(r))
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u := userFrom(r)
	if err := validateCredentials(credentials{Username: u.Username, Password: body.New}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err := a.app.Store.ChangePassword(r.Context(), u.ID, body.Current, body.New)
	if errors.Is(err, store.ErrBadCredential) {
		writeError(w, http.StatusBadRequest, "current password is incorrect")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Settings

type settingsResponse struct {
	app.Settings
	Defaults  app.Settings `json:"defaults"`
	Languages []string     `json:"languages"`
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, settingsResponse{a.app.Settings(), a.app.Defaults, app.Languages})
}

func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	// Merge a partial update on top of the current settings.
	s := a.app.Settings()
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.ISODir = strings.TrimSpace(s.ISODir)
	if s.ISODir != "" && !filepath.IsAbs(s.ISODir) {
		abs, err := filepath.Abs(s.ISODir)
		if err == nil {
			s.ISODir = abs
		}
	}
	if err := a.app.Update(r.Context(), s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.getSettings(w, r)
}

func (a *API) completeSetup(w http.ResponseWriter, r *http.Request) {
	s := a.app.Settings()
	s.SetupComplete = true
	if err := a.app.Update(r.Context(), s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.getSettings(w, r)
}

// Status

type storageInfo struct {
	Dir   string `json:"dir"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
	Error string `json:"error,omitempty"`
}

func diskUsage(dir string) storageInfo {
	si := storageInfo{Dir: dir}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		si.Error = err.Error()
		return si
	}
	si.Free = st.Bavail * uint64(st.Bsize)
	si.Total = st.Blocks * uint64(st.Bsize)
	return si
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	isos := a.app.Lib.List()
	clients, _ := a.app.Store.Clients(r.Context())
	var total int64
	for _, iso := range isos {
		total += iso.Size
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server":   a.app.Status(),
		"storage":  diskUsage(a.app.Lib.Dir()),
		"isoCount": len(isos),
		"isoBytes": total,
		"clients":  len(clients),
	})
}

func (a *API) networkCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.app.NetworkCheck(r.Context(), r.URL.Query().Get("fresh") == "1"))
}

func (a *API) interfaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.Interfaces())
}

func (a *API) checkDir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Path) == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	p, _ := filepath.Abs(strings.TrimSpace(body.Path))
	res := map[string]any{"path": p, "exists": false, "writable": false, "isos": 0}
	fi, err := os.Stat(p)
	switch {
	case err == nil && !fi.IsDir():
		res["error"] = "not a directory"
	case err == nil:
		res["exists"] = true
		f, err := os.CreateTemp(p, ".ipxemultiboot-check-*")
		if err == nil {
			f.Close()
			os.Remove(f.Name())
			res["writable"] = true
		} else {
			res["error"] = err.Error()
		}
		n := 0
		filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".iso") {
				n++
			}
			return nil
		})
		res["isos"] = n
		u := diskUsage(p)
		res["free"] = u.Free
	case errors.Is(err, os.ErrNotExist):
		res["error"] = "folder does not exist yet; it will be created"
	default:
		res["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

// ISOs

type isoJSON struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"modTime"`
	VolumeID string    `json:"volumeId"`
	Boot     string    `json:"boot"`
	Kernel   string    `json:"kernel,omitempty"`
}

func (a *API) listISOs(w http.ResponseWriter, r *http.Request) {
	out := []isoJSON{}
	dir := a.app.Lib.Dir()
	for _, iso := range a.app.Lib.List() {
		j := isoJSON{ID: iso.ID, Name: iso.Name, Size: iso.Size, ModTime: iso.ModTime, VolumeID: iso.VolumeID, Boot: "sanboot"}
		if rel, err := filepath.Rel(dir, iso.Path); err == nil {
			j.Path = rel
		}
		if iso.Recipe != nil {
			j.Boot, j.Kernel = iso.Recipe.Family, iso.Recipe.Kernel
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, out)
}

func validISOName(name string) error {
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\\x00") {
		return errors.New("invalid file name")
	}
	if !strings.EqualFold(filepath.Ext(name), ".iso") {
		return errors.New("only .iso files can be uploaded")
	}
	return nil
}

// uploadISO streams the raw request body to the ISO folder. The file is
// written under a hidden temporary name and renamed when complete, so a
// half-uploaded ISO never shows up in the boot menu.
func (a *API) uploadISO(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if err := validISOName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir := a.app.Lib.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dst := filepath.Join(dir, name)
	overwrite := r.URL.Query().Get("overwrite") == "1"
	if _, err := os.Stat(dst); err == nil && !overwrite {
		writeError(w, http.StatusConflict, "a file with this name already exists")
		return
	}
	if r.ContentLength > 0 {
		if u := diskUsage(dir); u.Error == "" && uint64(r.ContentLength) > u.Free {
			writeError(w, http.StatusInsufficientStorage, fmt.Sprintf("not enough free space (%d bytes free)", u.Free))
			return
		}
	}
	rnd := make([]byte, 6)
	rand.Read(rnd)
	tmp := filepath.Join(dir, ".upload-"+hex.EncodeToString(rnd)+".part")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := io.Copy(f, r.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && r.ContentLength > 0 && n != r.ContentLength {
		err = fmt.Errorf("upload incomplete: got %d of %d bytes", n, r.ContentLength)
	}
	if err != nil {
		os.Remove(tmp)
		slog.Warn("upload failed", "name", name, "err", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.app.Lib.Invalidate()
	u := userFrom(r)
	slog.Info("iso uploaded", "name", name, "bytes", n, "user", u.Username)
	a.app.Hub.Publish(events.Event{Kind: events.ISO, Message: fmt.Sprintf("%s uploaded %s", u.Username, name)})
	for _, iso := range a.app.Lib.List() {
		if iso.Path == dst {
			j := isoJSON{ID: iso.ID, Name: iso.Name, Size: iso.Size, ModTime: iso.ModTime, VolumeID: iso.VolumeID, Boot: "sanboot", Path: name}
			if iso.Recipe != nil {
				j.Boot, j.Kernel = iso.Recipe.Family, iso.Recipe.Kernel
			}
			writeJSON(w, http.StatusCreated, j)
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (a *API) deleteISO(w http.ResponseWriter, r *http.Request) {
	iso := a.app.Lib.Get(r.PathValue("id"))
	if iso == nil {
		writeError(w, http.StatusNotFound, "ISO not found")
		return
	}
	if err := os.Remove(iso.Path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.app.Lib.Invalidate()
	u := userFrom(r)
	a.app.Hub.Publish(events.Event{Kind: events.ISO, Message: fmt.Sprintf("%s deleted %s", u.Username, iso.Name)})
	w.WriteHeader(http.StatusNoContent)
}

// Clients & events

func (a *API) listClients(w http.ResponseWriter, r *http.Request) {
	clients, err := a.app.Store.Clients(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, clients)
}

func (a *API) deleteClient(w http.ResponseWriter, r *http.Request) {
	if err := a.app.Store.DeleteClient(r.Context(), r.PathValue("mac")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) recentEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.app.Hub.Recent(100))
}

func (a *API) streamEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, unsub := a.app.Hub.Subscribe()
	defer unsub()
	fmt.Fprint(w, ": connected\n\n")
	rc.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
