package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ipxemultiboot/internal/events"
	"ipxemultiboot/internal/netcheck"
)

const (
	probeTimeout  = 6 * time.Second // DHCP servers often ping-check an address first (~3s)
	probeCacheTTL = 20 * time.Second
	funnelWindow  = 30 * time.Minute
)

type probeCache struct {
	mu     sync.Mutex
	key    string
	at     time.Time
	offers []netcheck.Offer
	err    error
}

var probes probeCache

// NetworkCheck diagnoses the network setup. The DHCP probe result is cached
// briefly so dashboard reloads don't flood the LAN with DISCOVERs.
func (a *App) NetworkCheck(ctx context.Context, fresh bool) netcheck.Report {
	st := a.Status()
	s := a.Settings()
	in := netcheck.Input{
		Mode:          st.DHCPMode,
		Interface:     st.Interface,
		InterfaceAuto: s.Interface == "",
		HTTPPort:      a.HTTPPort,
		PoolStart:     s.PoolStart,
		PoolEnd:       s.PoolEnd,
		Services:      map[string]netcheck.ServiceState{},
		ISODir:        a.Lib.Dir(),
		ISOCount:      len(a.Lib.List()),
		ISOWritable:   writable(a.Lib.Dir()),
		InDocker:      fileExists("/.dockerenv"),
		Firewall:      detectFirewall(),
	}
	for name, svc := range st.Services {
		in.Services[name] = netcheck.ServiceState{State: svc.State, Error: svc.Error}
	}
	a.mu.Lock()
	if a.ip != nil {
		in.ServerIP, in.Subnet = a.ip, a.ipnet
	}
	a.mu.Unlock()
	if in.ServerIP != nil {
		in.Wireless = fileExists(filepath.Join("/sys/class/net", in.Interface, "wireless"))
		for _, ifc := range Interfaces() {
			if ifc.Name != in.Interface && ifc.Up && !ifc.Virtual && len(ifc.Addresses) > 0 {
				in.OtherIfaces = append(in.OtherIfaces, fmt.Sprintf("%s (%s)", ifc.Name, strings.Join(ifc.Addresses, ", ")))
			}
		}
		in.Offers, in.ProbeErr = a.probe(ctx, in.Interface, fresh)
		in.Probed = in.ProbeErr == nil
		in.SelfOffer = a.selfTest()
	}
	in.Funnel = funnel(a.Hub.Since(time.Now().Add(-funnelWindow)))
	return netcheck.Analyze(in)
}

func (a *App) probe(ctx context.Context, iface string, fresh bool) ([]netcheck.Offer, error) {
	probes.mu.Lock()
	defer probes.mu.Unlock()
	key := iface + "|" + a.Settings().DHCPMode
	if !fresh && probes.key == key && time.Since(probes.at) < probeCacheTTL {
		return probes.offers, probes.err
	}
	mac := netcheck.RandomMAC()
	a.mu.Lock()
	a.probeMAC = mac.String()
	a.mu.Unlock()
	offers, err := netcheck.Probe(ctx, iface, mac, probeTimeout)
	a.mu.Lock()
	a.probeMAC = ""
	a.mu.Unlock()
	if err != nil {
		err = fmt.Errorf("DHCP scan on %s failed: %w", iface, err)
	}
	probes.key, probes.at, probes.offers, probes.err = key, time.Now(), offers, err
	return offers, err
}

// selfTest asks the running DHCP service how it would answer a UEFI PXE
// client. The LAN probe can't see this host's own replies.
func (a *App) selfTest() *netcheck.Offer {
	a.mu.Lock()
	srv, ip := a.dhcpSrv, a.ip
	a.mu.Unlock()
	if srv == nil || ip == nil {
		return nil
	}
	reply, err := srv.Simulate(netcheck.RandomMAC(), 7)
	if err != nil || reply == nil {
		return nil
	}
	o := netcheck.Offer{ServerID: ip.String(), BootFile: reply.BootFileName, NextServer: ip.String(), PXEVendor: true}
	if y := reply.YourIPAddr; y != nil && !y.IsUnspecified() {
		o.YourIP = y.String()
	}
	return &o
}

func funnel(evs []events.Event) netcheck.Funnel {
	var f netcheck.Funnel
	for _, e := range evs {
		switch e.Stage {
		case events.StageDHCP:
			f.FirmwareDHCP++
		case events.StageTFTP:
			f.TFTP++
		case events.StageIPXE:
			f.IPXEDHCP++
		case events.StageMenu:
			f.Menu++
		case events.StageBoot:
			f.Boot++
		}
	}
	return f
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".ipxemultiboot-check-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// detectFirewall reports an active host firewall we can give instructions for.
func detectFirewall() string {
	if b, err := os.ReadFile("/etc/ufw/ufw.conf"); err == nil && strings.Contains(string(b), "ENABLED=yes") {
		return "ufw"
	}
	for _, p := range []string{"/run/firewalld/firewalld.pid", "/var/run/firewalld.pid", "/run/firewalld.pid"} {
		if fileExists(p) {
			return "firewalld"
		}
	}
	return ""
}
