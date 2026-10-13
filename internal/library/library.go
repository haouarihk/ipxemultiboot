// Package library scans the ISO directory and keeps opened images cached.
package library

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"ipxemultiboot/internal/distro"
	"ipxemultiboot/internal/iso9660"
)

// ISO is one bootable image in the library.
type ISO struct {
	ID       string // URL-safe identifier, stable for a given file name
	Name     string // file name
	Path     string
	Size     int64
	ModTime  time.Time
	VolumeID string
	Recipe   *distro.Recipe // nil: sanboot only
	Image    *iso9660.Image // nil if not a readable ISO9660 image
}

// Library is a directory of ISO files.
type Library struct {
	dir string

	mu       sync.Mutex
	cache    map[string]*ISO // keyed by path
	last     []*ISO
	lastScan time.Time
}

// scanTTL bounds how often per-request lookups rescan the directory.
const scanTTL = 2 * time.Second

func New(dir string) *Library {
	return &Library{dir: dir, cache: map[string]*ISO{}}
}

func (l *Library) Dir() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dir
}

// SetDir switches to another folder; the next List rescans.
func (l *Library) SetDir(dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if dir != l.dir {
		l.dir = dir
		l.lastScan = time.Time{}
	}
}

// Invalidate forces the next List to rescan (after uploads/deletes).
func (l *Library) Invalidate() {
	l.mu.Lock()
	l.lastScan = time.Time{}
	l.mu.Unlock()
}

var unsafeChars = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(name string) string {
	s := unsafeChars.ReplaceAllString(strings.ToLower(name), "-")
	return strings.Trim(s, "-")
}

// List rescans the directory (recursively) and returns all ISOs sorted by name.
// Unchanged files reuse their cached, already-opened image.
func (l *Library) List() []*ISO {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.lastScan) < scanTTL {
		return l.last
	}

	seen := map[string]bool{}
	out := []*ISO{}
	filepath.WalkDir(l.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".iso") || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		fi, err := os.Stat(p) // follow symlinks
		if err != nil {
			return nil
		}
		seen[p] = true
		if c, ok := l.cache[p]; ok && c.Size == fi.Size() && c.ModTime.Equal(fi.ModTime()) {
			out = append(out, c)
			return nil
		}
		if c, ok := l.cache[p]; ok && c.Image != nil {
			c.Image.Close()
		}
		iso := &ISO{Name: d.Name(), Path: p, Size: fi.Size(), ModTime: fi.ModTime()}
		if img, err := iso9660.Open(p); err == nil {
			iso.Image = img
			iso.VolumeID = img.VolumeID()
			iso.Recipe = distro.Detect(img)
		} else {
			slog.Warn("not an ISO9660 image, sanboot only", "file", p, "err", err)
		}
		family := "sanboot"
		if iso.Recipe != nil {
			family = iso.Recipe.Family
		}
		slog.Info("found ISO", "file", d.Name(), "volume", iso.VolumeID, "boot", family)
		l.cache[p] = iso
		out = append(out, iso)
		return nil
	})
	for p, c := range l.cache {
		if !seen[p] {
			if c.Image != nil {
				c.Image.Close()
			}
			delete(l.cache, p)
		}
	}

	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	ids := map[string]int{}
	for _, iso := range out {
		id := slug(strings.TrimSuffix(iso.Name, filepath.Ext(iso.Name)))
		if n := ids[id]; n > 0 {
			ids[id]++
			id = id + "-" + string(rune('0'+n))
		} else {
			ids[id] = 1
		}
		iso.ID = id
	}
	l.last, l.lastScan = out, time.Now()
	return out
}

// Get finds an ISO by ID.
func (l *Library) Get(id string) *ISO {
	for _, iso := range l.List() {
		if iso.ID == id {
			return iso
		}
	}
	return nil
}
