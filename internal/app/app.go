// Package app wires the services together and owns the runtime settings.
// Settings saved from the dashboard (SQLite) override the env/flag defaults,
// and changing network settings restarts the DHCP service in place.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ipxemultiboot/internal/bootfiles"
	"ipxemultiboot/internal/dhcp"
	"ipxemultiboot/internal/events"
	"ipxemultiboot/internal/library"
	"ipxemultiboot/internal/store"
	"ipxemultiboot/internal/tftpd"
)

// Settings are user-configurable and persisted in the database.
type Settings struct {
	SetupComplete bool   `json:"setupComplete"`
	Language      string `json:"language"`
	Theme         string `json:"theme"`
	ISODir        string `json:"isoDir"`
	Interface     string `json:"interface"` // empty: auto-detect
	DHCPMode      string `json:"dhcpMode"`
	PoolStart     string `json:"poolStart"`
	PoolEnd       string `json:"poolEnd"`
	Router        string `json:"router"`
	DNS           string `json:"dns"`
}

var Languages = []string{"en", "de", "es", "fr", "zh"}

func (s Settings) toMap() map[string]string {
	return map[string]string{
		"setup_complete": strconv.FormatBool(s.SetupComplete),
		"language":       s.Language,
		"theme":          s.Theme,
		"iso_dir":        s.ISODir,
		"interface":      s.Interface,
		"dhcp_mode":      s.DHCPMode,
		"pool_start":     s.PoolStart,
		"pool_end":       s.PoolEnd,
		"router":         s.Router,
		"dns":            s.DNS,
	}
}

func (s *Settings) fromMap(m map[string]string) {
	set := func(dst *string, key string) {
		if v, ok := m[key]; ok {
			*dst = v
		}
	}
	if v, ok := m["setup_complete"]; ok {
		s.SetupComplete = v == "true"
	}
	set(&s.Language, "language")
	set(&s.Theme, "theme")
	set(&s.ISODir, "iso_dir")
	set(&s.Interface, "interface")
	set(&s.DHCPMode, "dhcp_mode")
	set(&s.PoolStart, "pool_start")
	set(&s.PoolEnd, "pool_end")
	set(&s.Router, "router")
	set(&s.DNS, "dns")
}

// Validate checks settings and normalizes values.
func (s *Settings) Validate() error {
	if s.Language == "" {
		s.Language = "en"
	}
	if !contains(Languages, s.Language) {
		return fmt.Errorf("unsupported language %q", s.Language)
	}
	switch s.Theme {
	case "":
		s.Theme = "system"
	case "system", "light", "dark":
	default:
		return fmt.Errorf("invalid theme %q", s.Theme)
	}
	if strings.TrimSpace(s.ISODir) == "" {
		return errors.New("ISO folder is required")
	}
	m, err := dhcp.ParseMode(s.DHCPMode)
	if err != nil {
		return err
	}
	s.DHCPMode = string(m)
	if m == dhcp.ModeFull {
		if net.ParseIP(s.PoolStart).To4() == nil || net.ParseIP(s.PoolEnd).To4() == nil {
			return errors.New("full DHCP mode needs a valid IPv4 pool start and end")
		}
		if s.Router != "" && net.ParseIP(s.Router).To4() == nil {
			return fmt.Errorf("invalid router address %q", s.Router)
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ServiceState is shown in the dashboard.
type ServiceState struct {
	State string `json:"state"` // running, error, disabled, stopped
	Error string `json:"error,omitempty"`
}

type service struct {
	cancel context.CancelFunc
	done   chan struct{}
	state  ServiceState
}

type App struct {
	Version  string
	HTTPPort int
	TFTPAddr string
	Defaults Settings
	Store    *store.Store
	Lib      *library.Library
	Hub      *events.Hub

	restartMu sync.Mutex // serializes restartNetwork
	mu        sync.Mutex
	ctx       context.Context
	settings  Settings
	iface     string
	ip        net.IP
	ipnet     *net.IPNet
	dhcp      *service
	tftp      *service

	dhcpSrv  *dhcp.Server // running DHCP server, for the network check self-test
	probeMAC string       // MAC of the running network-check probe
}

// Load reads persisted settings on top of the defaults.
func (a *App) Load(ctx context.Context) error {
	s := a.Defaults
	m, err := a.Store.Settings(ctx)
	if err != nil {
		return err
	}
	s.fromMap(m)
	if err := s.Validate(); err != nil {
		slog.Warn("stored settings invalid, using defaults", "err", err)
		s = a.Defaults
		s.SetupComplete = m["setup_complete"] == "true"
		if err := s.Validate(); err != nil {
			return fmt.Errorf("default settings: %w", err)
		}
	}
	a.settings = s
	a.Lib.SetDir(s.ISODir)
	if err := os.MkdirAll(s.ISODir, 0o755); err != nil {
		slog.Warn("cannot create ISO folder", "dir", s.ISODir, "err", err)
	}
	a.Hub.OnPublish(a.recordClient)
	return nil
}

func (a *App) Settings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// Update validates, persists and applies new settings.
func (a *App) Update(ctx context.Context, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.ISODir, 0o755); err != nil {
		return fmt.Errorf("cannot create ISO folder: %w", err)
	}
	if s.DHCPMode == string(dhcp.ModeFull) {
		if _, ipnet, err := PickInterface(s.Interface); err == nil {
			if _, err := parsePool(ipnet, s); err != nil {
				return err
			}
		}
	}
	if err := a.Store.SaveSettings(ctx, s.toMap()); err != nil {
		return err
	}
	a.mu.Lock()
	old := a.settings
	a.settings = s
	a.mu.Unlock()

	a.Lib.SetDir(s.ISODir)
	if old.Interface != s.Interface || old.DHCPMode != s.DHCPMode || old.PoolStart != s.PoolStart ||
		old.PoolEnd != s.PoolEnd || old.Router != s.Router || old.DNS != s.DNS {
		a.restartNetwork()
	}
	return nil
}

// Start launches TFTP and DHCP. HTTP is run by the caller.
func (a *App) Start(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	a.restartNetwork()
	go a.pruneLoop(ctx)
}

func (a *App) pruneLoop(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Store.PruneSessions(ctx)
		}
	}
}

// restartNetwork (re)resolves the interface and restarts TFTP and DHCP.
func (a *App) restartNetwork() {
	a.restartMu.Lock()
	defer a.restartMu.Unlock()
	a.mu.Lock()
	ctx := a.ctx
	s := a.settings
	a.mu.Unlock()
	if ctx == nil {
		return
	}

	a.stop(&a.dhcp)
	a.stop(&a.tftp)
	a.mu.Lock()
	a.dhcpSrv = nil
	a.mu.Unlock()

	ifc, ipnet, err := PickInterface(s.Interface)
	a.mu.Lock()
	if err != nil {
		a.iface, a.ip, a.ipnet = "", nil, nil
		a.dhcp = &service{state: ServiceState{State: "error", Error: err.Error()}}
		a.tftp = &service{state: ServiceState{State: "error", Error: err.Error()}}
		a.mu.Unlock()
		slog.Error("network setup failed", "err", err)
		return
	}
	a.iface, a.ip, a.ipnet = ifc.Name, ipnet.IP.To4(), ipnet
	a.mu.Unlock()

	menuURL := a.MenuURL()
	tftpSvc := a.run(ctx, "tftp", func(ctx context.Context) error {
		return tftpd.New(a.TFTPAddr, bootfiles.FS, func(name string) ([]byte, bool) {
			if name == "autoexec.ipxe" {
				return tftpd.AutoexecScript(menuURL), true
			}
			return nil, false
		}, a.onTFTP).Run(ctx)
	})
	a.mu.Lock()
	a.tftp = tftpSvc
	a.mu.Unlock()

	mode, _ := dhcp.ParseMode(s.DHCPMode)
	if mode == dhcp.ModeBootstrap {
		a.mu.Lock()
		a.dhcp = &service{state: ServiceState{State: "disabled"}}
		a.mu.Unlock()
		slog.Info("bootstrap mode: configure your DHCP server", "next-server", a.ip.String(),
			"bios", bootfiles.BIOS, "uefi", bootfiles.EFIx64, "ipxe", menuURL)
		return
	}
	cfg := dhcp.Config{Mode: mode, Interface: ifc.Name, ServerIP: a.ip, HTTPPort: a.HTTPPort, OnReply: a.onDHCP}
	if mode == dhcp.ModeFull {
		pool, err := parsePool(ipnet, s)
		if err != nil {
			a.mu.Lock()
			a.dhcp = &service{state: ServiceState{State: "error", Error: err.Error()}}
			a.mu.Unlock()
			return
		}
		cfg.Pool = pool
	}
	srv := dhcp.New(cfg)
	dhcpSvc := a.run(ctx, "dhcp", srv.Run)
	a.mu.Lock()
	a.dhcp, a.dhcpSrv = dhcpSvc, srv
	a.mu.Unlock()
}

func (a *App) run(parent context.Context, name string, fn func(context.Context) error) *service {
	ctx, cancel := context.WithCancel(parent)
	svc := &service{cancel: cancel, done: make(chan struct{}), state: ServiceState{State: "running"}}
	go func() {
		defer close(svc.done)
		err := fn(ctx)
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case err != nil && ctx.Err() == nil:
			slog.Error("service failed", "service", name, "err", err)
			svc.state = ServiceState{State: "error", Error: friendlyError(err)}
		default:
			svc.state = ServiceState{State: "stopped"}
		}
	}()
	return svc
}

func friendlyError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "permission denied") || strings.Contains(msg, "operation not permitted") {
		msg += " (ports 67/69 need root or CAP_NET_BIND_SERVICE + CAP_NET_RAW)"
	}
	if strings.Contains(msg, "address already in use") {
		msg += " (another DHCP/TFTP server is running on this host)"
	}
	return msg
}

func (a *App) stop(p **service) {
	a.mu.Lock()
	svc := *p
	a.mu.Unlock()
	if svc == nil || svc.cancel == nil {
		return
	}
	svc.cancel()
	select {
	case <-svc.done:
	case <-time.After(5 * time.Second):
		slog.Warn("service did not stop in time")
	}
}

// Status is the dashboard summary.
type Status struct {
	Version   string                  `json:"version"`
	Interface string                  `json:"interface"`
	ServerIP  string                  `json:"serverIp"`
	Subnet    string                  `json:"subnet"`
	HTTPPort  int                     `json:"httpPort"`
	MenuURL   string                  `json:"menuUrl"`
	DHCPMode  string                  `json:"dhcpMode"`
	Services  map[string]ServiceState `json:"services"`
	Bootstrap *BootstrapHints         `json:"bootstrap,omitempty"`
}

// BootstrapHints tell the user what to configure on their own DHCP server.
type BootstrapHints struct {
	NextServer string `json:"nextServer"`
	BIOSFile   string `json:"biosFile"`
	UEFIFile   string `json:"uefiFile"`
	IPXEURL    string `json:"ipxeUrl"`
}

func (a *App) MenuURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ip == nil {
		return ""
	}
	return fmt.Sprintf("http://%s:%d/boot.ipxe", a.ip, a.HTTPPort)
}

func (a *App) Status() Status {
	menu := a.MenuURL()
	a.mu.Lock()
	defer a.mu.Unlock()
	st := Status{
		Version:  a.Version,
		HTTPPort: a.HTTPPort,
		MenuURL:  menu,
		DHCPMode: a.settings.DHCPMode,
		Services: map[string]ServiceState{"http": {State: "running"}},
	}
	if a.ip != nil {
		st.Interface, st.ServerIP, st.Subnet = a.iface, a.ip.String(), a.ipnet.String()
	}
	for name, svc := range map[string]*service{"dhcp": a.dhcp, "tftp": a.tftp} {
		if svc == nil {
			st.Services[name] = ServiceState{State: "stopped"}
		} else {
			st.Services[name] = svc.state
		}
	}
	st.Bootstrap = &BootstrapHints{NextServer: st.ServerIP, BIOSFile: bootfiles.BIOS, UEFIFile: bootfiles.EFIx64, IPXEURL: menu}
	return st
}

func (a *App) onDHCP(r dhcp.Reply) {
	a.mu.Lock()
	if a.probeMAC != "" && r.MAC == a.probeMAC {
		// Network check traffic stays out of the activity feed and client list.
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	stage := events.StageDHCP
	if r.IPXE {
		stage = events.StageIPXE
	}
	fw := firmwareName(r.Arch, false)
	if r.IPXE {
		fw = ""
	}
	a.Hub.Publish(events.Event{Kind: events.DHCP, MAC: r.MAC, IP: r.IP, Stage: stage, Firmware: fw,
		Message: fmt.Sprintf("%s → %s (%s)", r.Request, r.Reply, firmwareName(r.Arch, r.IPXE)) + fileSuffix(r.File)})
}

func fileSuffix(f string) string {
	if f == "" {
		return ""
	}
	return ", file " + f
}

func (a *App) onTFTP(file, peer string, bytes int64, err error) {
	if err != nil && bytes == 0 && strings.Contains(err.Error(), "aborted") {
		return // UEFI firmware asks for the file size first, then aborts: not an error
	}
	ip, _, _ := net.SplitHostPort(peer)
	e := events.Event{Kind: events.TFTP, IP: ip, Message: fmt.Sprintf("TFTP sent %s (%d bytes)", file, bytes)}
	if err != nil {
		e.Message = fmt.Sprintf("TFTP %s failed: %v", file, err)
	} else if file == bootfiles.BIOS || file == bootfiles.EFIx64 {
		e.Stage = events.StageTFTP
	} else if file == "autoexec.ipxe" {
		e.Stage = events.StageIPXE
	}
	// TFTP only knows the client's IP; attribute it if we've seen that IP before.
	if e.Stage != "" {
		e.MAC = a.Store.ClientMACByIP(context.Background(), ip)
	}
	a.Hub.Publish(e)
}

func firmwareName(arch uint16, ipxe bool) string {
	name := "unknown"
	switch arch {
	case 0:
		name = "BIOS"
	case 6:
		name = "UEFI x86"
	case 7, 9:
		name = "UEFI x64"
	case 10:
		name = "UEFI ARM32"
	case 11:
		name = "UEFI ARM64"
	case 16:
		name = "UEFI HTTP x64"
	}
	if ipxe {
		name += ", iPXE"
	}
	return name
}

// recordClient keeps the clients table up to date from boot events.
func (a *App) recordClient(e events.Event) {
	if e.MAC == "" {
		return
	}
	u := store.ClientUpdate{MAC: e.MAC, IP: e.IP, Firmware: e.Firmware, LastISO: e.ISO, LastEvent: e.Message, Stage: string(e.Stage),
		IPXE: e.Stage == events.StageIPXE || e.Stage == events.StageMenu || e.Stage == events.StageBoot}
	if err := a.Store.TouchClient(context.Background(), u); err != nil {
		slog.Warn("record client", "err", err)
	}
}

// PickInterface returns the named interface, or the first up, non-virtual
// interface with an IPv4 address.
func PickInterface(name string) (*net.Interface, *net.IPNet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	for _, ifc := range ifaces {
		if name != "" && ifc.Name != name {
			continue
		}
		if name == "" && (ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || isVirtual(ifc.Name)) {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, addr := range addrs {
			if ipn, ok := addr.(*net.IPNet); ok && ipn.IP.To4() != nil {
				ifc := ifc
				return &ifc, ipn, nil
			}
		}
		if name != "" {
			return nil, nil, fmt.Errorf("interface %s has no IPv4 address", name)
		}
	}
	if name != "" {
		return nil, nil, fmt.Errorf("interface %s not found", name)
	}
	return nil, nil, errors.New("no usable network interface found; pick one in Settings → Network")
}

func isVirtual(name string) bool {
	for _, p := range []string{"docker", "br-", "virbr", "veth", "tailscale", "zt", "wg", "tun"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Interface describes a NIC for the settings UI.
type Interface struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac"`
	Up        bool     `json:"up"`
	Addresses []string `json:"addresses"`
	Virtual   bool     `json:"virtual"`
}

func Interfaces() []Interface {
	ifaces, _ := net.Interfaces()
	out := []Interface{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		it := Interface{Name: ifc.Name, MAC: ifc.HardwareAddr.String(), Up: ifc.Flags&net.FlagUp != 0,
			Virtual: isVirtual(ifc.Name), Addresses: []string{}}
		addrs, _ := ifc.Addrs()
		for _, addr := range addrs {
			if ipn, ok := addr.(*net.IPNet); ok && ipn.IP.To4() != nil {
				it.Addresses = append(it.Addresses, ipn.String())
			}
		}
		out = append(out, it)
	}
	return out
}

func parsePool(ipnet *net.IPNet, s Settings) (dhcp.Pool, error) {
	p := dhcp.Pool{Netmask: ipnet.Mask, LeaseTime: time.Hour}
	p.Start, p.End = net.ParseIP(s.PoolStart).To4(), net.ParseIP(s.PoolEnd).To4()
	if p.Start == nil || p.End == nil {
		return p, errors.New("full DHCP mode needs a pool start and end address")
	}
	if !ipnet.Contains(p.Start) || !ipnet.Contains(p.End) {
		return p, fmt.Errorf("pool %s – %s is outside the interface subnet %s", p.Start, p.End, ipnet)
	}
	if s.Router != "" {
		p.Router = net.ParseIP(s.Router).To4()
	}
	for _, d := range strings.Split(s.DNS, ",") {
		if ip := net.ParseIP(strings.TrimSpace(d)).To4(); ip != nil {
			p.DNS = append(p.DNS, ip)
		}
	}
	return p, nil
}
