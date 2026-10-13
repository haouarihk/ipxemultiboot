// Package events is an in-memory pub/sub hub for boot activity shown live in
// the dashboard.
package events

import (
	"sync"
	"time"
)

type Kind string

const (
	DHCP Kind = "dhcp"
	TFTP Kind = "tftp"
	Menu Kind = "menu"
	Boot Kind = "boot"
	ISO  Kind = "iso"
)

type Event struct {
	ID      uint64    `json:"id"`
	Time    time.Time `json:"time"`
	Kind    Kind      `json:"kind"`
	MAC     string    `json:"mac,omitempty"`
	IP      string    `json:"ip,omitempty"`
	Message string    `json:"message"`

	// Structured details used for client tracking and diagnostics.
	Stage    Stage  `json:"stage,omitempty"`
	Firmware string `json:"firmware,omitempty"`
	ISO      string `json:"iso,omitempty"`
}

// Stage is how far a client got in the network boot sequence.
type Stage string

const (
	StageDHCP Stage = "dhcp" // firmware PXE request answered (starts a new attempt)
	StageTFTP Stage = "tftp" // iPXE binary downloaded
	StageIPXE Stage = "ipxe" // iPXE is running (DHCP from iPXE, autoexec.ipxe)
	StageMenu Stage = "menu" // boot menu shown
	StageBoot Stage = "boot" // ISO boot started
)

// Since returns events newer than t, newest first.
func (h *Hub) Since(t time.Time) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Event
	for i := len(h.ring) - 1; i >= 0 && h.ring[i].Time.After(t); i-- {
		out = append(out, h.ring[i])
	}
	return out
}

type Hub struct {
	mu     sync.Mutex
	nextID uint64
	ring   []Event
	size   int
	subs   map[chan Event]struct{}
	onPub  []func(Event)
}

func NewHub(size int) *Hub {
	return &Hub{size: size, subs: map[chan Event]struct{}{}}
}

// OnPublish registers a synchronous hook (e.g. persisting client info).
func (h *Hub) OnPublish(f func(Event)) {
	h.mu.Lock()
	h.onPub = append(h.onPub, f)
	h.mu.Unlock()
}

func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	h.nextID++
	e.ID = h.nextID
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h.ring = append(h.ring, e)
	if len(h.ring) > h.size {
		h.ring = h.ring[len(h.ring)-h.size:]
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default: // slow subscriber; drop
		}
	}
	hooks := h.onPub
	h.mu.Unlock()
	for _, f := range hooks {
		f(e)
	}
}

// Recent returns up to n most recent events, newest first.
func (h *Hub) Recent(n int) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Event, 0, n)
	for i := len(h.ring) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, h.ring[i])
	}
	return out
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}
