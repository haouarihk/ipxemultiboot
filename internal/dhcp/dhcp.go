// Package dhcp implements the PXE side of DHCP.
//
// Modes:
//   - proxy: answers only PXE clients, without assigning addresses, so it can
//     coexist with an existing DHCP server (router). Also serves port 4011.
//   - full:  a complete DHCP server handing out leases from a pool.
//   - bootstrap: no DHCP at all; only TFTP/HTTP run. The existing DHCP server
//     must point clients at us (next-server / filename, options 66/67).
package dhcp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"

	"ipxemultiboot/internal/bootfiles"
)

type Mode string

const (
	ModeProxy     Mode = "proxy"
	ModeFull      Mode = "full"
	ModeBootstrap Mode = "bootstrap"
)

func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.ToLower(s)); m {
	case ModeProxy, ModeFull, ModeBootstrap:
		return m, nil
	case "off", "none":
		return ModeBootstrap, nil
	}
	return "", fmt.Errorf("invalid DHCP mode %q (want proxy, full or bootstrap)", s)
}

// Pool configures full mode.
type Pool struct {
	Start, End net.IP
	Netmask    net.IPMask
	Router     net.IP   // optional
	DNS        []net.IP // optional
	LeaseTime  time.Duration
}

type Config struct {
	Mode      Mode
	Interface string
	ServerIP  net.IP
	HTTPPort  int
	Pool      Pool        // full mode only
	OnReply   func(Reply) // optional, called after each reply is sent
}

// Reply summarizes a sent DHCP reply for activity logging.
type Reply struct {
	MAC, IP        string
	Request, Reply string
	Arch           uint16
	IPXE           bool
	File           string
}

type Server struct {
	cfg    Config
	leases *leases
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg}
	if cfg.Mode == ModeFull {
		s.leases = newLeases(cfg.Pool)
	}
	return s
}

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	if s.cfg.Mode == ModeBootstrap {
		<-ctx.Done()
		return nil
	}
	ports := []int{67}
	if s.cfg.Mode == ModeProxy {
		ports = append(ports, 4011)
	}
	errc := make(chan error, len(ports))
	var servers []*server4.Server
	for _, port := range ports {
		port := port
		conn, err := server4.NewIPv4UDPConn(s.cfg.Interface, &net.UDPAddr{Port: port})
		if err != nil {
			return fmt.Errorf("dhcp listen :%d on %s: %w", port, s.cfg.Interface, err)
		}
		srv, err := server4.NewServer(s.cfg.Interface, nil, func(c net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
			s.handle(c, peer, m, port)
		}, server4.WithConn(conn))
		if err != nil {
			return err
		}
		servers = append(servers, srv)
		go func() { errc <- srv.Serve() }()
	}
	slog.Info("dhcp listening", "mode", s.cfg.Mode, "iface", s.cfg.Interface, "server", s.cfg.ServerIP)
	select {
	case <-ctx.Done():
		for _, srv := range servers {
			srv.Close()
		}
		return nil
	case err := <-errc:
		return err
	}
}

// Client architecture types (RFC 4578 option 93).
const (
	archBIOS       = 0
	archEFIx64     = 7
	archEFIBC      = 9
	archEFIx64HTTP = 16
)

type clientInfo struct {
	pxe  bool // vendor class PXEClient / HTTPClient
	http bool // UEFI HTTP boot
	ipxe bool // already running iPXE
	arch uint16
}

func inspect(m *dhcpv4.DHCPv4) clientInfo {
	var ci clientInfo
	vc := m.ClassIdentifier()
	ci.pxe = strings.HasPrefix(vc, "PXEClient") || strings.HasPrefix(vc, "HTTPClient")
	ci.http = strings.HasPrefix(vc, "HTTPClient")
	for _, uc := range m.UserClass() {
		if uc == "iPXE" {
			ci.ipxe = true
		}
	}
	if b := m.Options.Get(dhcpv4.OptionClientSystemArchitectureType); len(b) >= 2 {
		ci.arch = binary.BigEndian.Uint16(b)
	}
	return ci
}

// bootFile picks what the client should load next.
func (s *Server) bootFile(ci clientInfo) (string, bool) {
	httpBase := fmt.Sprintf("http://%s:%d", s.cfg.ServerIP, s.cfg.HTTPPort)
	if ci.ipxe {
		return httpBase + "/boot.ipxe", true
	}
	switch ci.arch {
	case archBIOS:
		return bootfiles.BIOS, true
	case archEFIx64, archEFIBC:
		return bootfiles.EFIx64, true
	case archEFIx64HTTP:
		return httpBase + "/ipxe/" + bootfiles.EFIx64, true
	}
	return "", false
}

func (s *Server) handle(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4, port int) {
	if m.OpCode != dhcpv4.OpcodeBootRequest {
		return
	}
	ci := inspect(m)
	mt := m.MessageType()
	log := slog.With("mac", m.ClientHWAddr.String(), "type", mt.String(), "arch", ci.arch, "ipxe", ci.ipxe, "port", port)

	if s.cfg.Mode == ModeProxy && !ci.pxe {
		return
	}

	var reply *dhcpv4.DHCPv4
	var err error
	switch s.cfg.Mode {
	case ModeProxy:
		reply, err = s.proxyReply(m, ci, port)
	case ModeFull:
		reply, err = s.fullReply(m, ci)
	}
	if err != nil {
		log.Debug("no reply", "reason", err)
		return
	}
	if reply == nil {
		return
	}

	dst := peer.(*net.UDPAddr)
	if port == 67 {
		// Clients without an address can't receive unicast; broadcast to :68.
		if m.ClientIPAddr == nil || m.ClientIPAddr.IsUnspecified() {
			dst = &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
		} else {
			dst = &net.UDPAddr{IP: m.ClientIPAddr, Port: 68}
		}
	}
	if _, err := conn.WriteTo(reply.ToBytes(), dst); err != nil {
		log.Error("dhcp send failed", "err", err)
		return
	}
	log.Info("dhcp reply", "reply", reply.MessageType().String(), "ip", reply.YourIPAddr, "file", reply.BootFileName)
	if s.cfg.OnReply != nil {
		ip := ""
		if reply.YourIPAddr != nil && !reply.YourIPAddr.IsUnspecified() {
			ip = reply.YourIPAddr.String()
		}
		s.cfg.OnReply(Reply{MAC: m.ClientHWAddr.String(), IP: ip, Request: mt.String(),
			Reply: reply.MessageType().String(), Arch: ci.arch, IPXE: ci.ipxe, File: reply.BootFileName})
	}
}

// Simulate returns the reply this server would send to a PXE client's
// DISCOVER, without touching the network or the lease table.
func (s *Server) Simulate(mac net.HardwareAddr, arch uint16) (*dhcpv4.DHCPv4, error) {
	m, err := dhcpv4.NewDiscovery(mac)
	if err != nil {
		return nil, err
	}
	m.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00007:UNDI:003016"))
	m.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionClientSystemArchitectureType, []byte{byte(arch >> 8), byte(arch)}))
	ci := inspect(m)
	switch s.cfg.Mode {
	case ModeProxy:
		return s.proxyReply(m, ci, 67)
	case ModeFull:
		// Use a throwaway lease table so the simulation doesn't reserve an address.
		sim := &Server{cfg: s.cfg, leases: newLeases(s.cfg.Pool)}
		return sim.fullReply(m, ci)
	}
	return nil, nil
}

// pxeVendorOptions is option 43 telling PXE ROMs to skip boot server discovery
// and use the boot file name from this packet directly.
var pxeVendorOptions = []byte{6, 1, 8, 255}

func (s *Server) commonBootOptions(m, reply *dhcpv4.DHCPv4, ci clientInfo) error {
	file, ok := s.bootFile(ci)
	if !ok {
		return fmt.Errorf("unsupported client architecture %d", ci.arch)
	}
	reply.ServerIPAddr = s.cfg.ServerIP
	reply.BootFileName = file
	reply.UpdateOption(dhcpv4.OptServerIdentifier(s.cfg.ServerIP))
	if ci.http {
		reply.UpdateOption(dhcpv4.OptClassIdentifier("HTTPClient"))
	} else {
		reply.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient"))
	}
	if guid := m.Options.Get(dhcpv4.OptionClientMachineIdentifier); guid != nil {
		reply.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, guid))
	}
	if !ci.ipxe && !ci.http {
		reply.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionVendorSpecificInformation, pxeVendorOptions))
	}
	return nil
}

func (s *Server) proxyReply(m *dhcpv4.DHCPv4, ci clientInfo, port int) (*dhcpv4.DHCPv4, error) {
	var mt dhcpv4.MessageType
	switch {
	case port == 67 && m.MessageType() == dhcpv4.MessageTypeDiscover:
		mt = dhcpv4.MessageTypeOffer
	case port == 4011 && m.MessageType() == dhcpv4.MessageTypeRequest:
		mt = dhcpv4.MessageTypeAck
	case port == 67 && m.MessageType() == dhcpv4.MessageTypeRequest:
		// The real DHCP server ACKs the address; nothing for us to do.
		return nil, nil
	default:
		return nil, nil
	}
	reply, err := dhcpv4.NewReplyFromRequest(m, dhcpv4.WithMessageType(mt))
	if err != nil {
		return nil, err
	}
	reply.YourIPAddr = net.IPv4zero
	if err := s.commonBootOptions(m, reply, ci); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *Server) fullReply(m *dhcpv4.DHCPv4, ci clientInfo) (*dhcpv4.DHCPv4, error) {
	mac := m.ClientHWAddr.String()
	var mt dhcpv4.MessageType
	var ip net.IP
	switch m.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		mt = dhcpv4.MessageTypeOffer
		ip = s.leases.offer(mac)
	case dhcpv4.MessageTypeRequest:
		if sid := m.ServerIdentifier(); sid != nil && !sid.Equal(s.cfg.ServerIP) {
			return nil, nil // client picked another server
		}
		want := m.RequestedIPAddress()
		if want == nil || want.IsUnspecified() {
			want = m.ClientIPAddr
		}
		ip = s.leases.commit(mac, want)
		if ip == nil {
			nak, err := dhcpv4.NewReplyFromRequest(m, dhcpv4.WithMessageType(dhcpv4.MessageTypeNak))
			if err != nil {
				return nil, err
			}
			nak.UpdateOption(dhcpv4.OptServerIdentifier(s.cfg.ServerIP))
			return nak, nil
		}
		mt = dhcpv4.MessageTypeAck
	case dhcpv4.MessageTypeRelease:
		s.leases.release(mac)
		return nil, nil
	default:
		return nil, nil
	}
	if ip == nil {
		return nil, errors.New("address pool exhausted")
	}
	p := s.cfg.Pool
	reply, err := dhcpv4.NewReplyFromRequest(m,
		dhcpv4.WithMessageType(mt),
		dhcpv4.WithYourIP(ip),
		dhcpv4.WithNetmask(p.Netmask),
		dhcpv4.WithLeaseTime(uint32(p.LeaseTime.Seconds())),
	)
	if err != nil {
		return nil, err
	}
	if p.Router != nil {
		reply.UpdateOption(dhcpv4.OptRouter(p.Router))
	}
	if len(p.DNS) > 0 {
		reply.UpdateOption(dhcpv4.OptDNS(p.DNS...))
	}
	reply.UpdateOption(dhcpv4.OptServerIdentifier(s.cfg.ServerIP))
	if ci.pxe || ci.ipxe {
		if err := s.commonBootOptions(m, reply, ci); err != nil {
			slog.Debug("no boot options", "err", err)
		}
	}
	return reply, nil
}

// leases is a simple in-memory lease table for full mode.
type leases struct {
	mu    sync.Mutex
	pool  Pool
	byMAC map[string]lease
}

type lease struct {
	ip      net.IP
	expires time.Time
}

func newLeases(p Pool) *leases {
	if p.LeaseTime == 0 {
		p.LeaseTime = time.Hour
	}
	return &leases{pool: p, byMAC: map[string]lease{}}
}

func ipToU32(ip net.IP) uint32 { return binary.BigEndian.Uint32(ip.To4()) }
func u32ToIP(v uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, v)
	return ip
}

func (l *leases) inPool(ip net.IP) bool {
	if ip == nil || ip.To4() == nil {
		return false
	}
	v := ipToU32(ip)
	return v >= ipToU32(l.pool.Start) && v <= ipToU32(l.pool.End)
}

func (l *leases) takenBy(ip net.IP) string {
	now := time.Now()
	for mac, le := range l.byMAC {
		if le.ip.Equal(ip) && le.expires.After(now) {
			return mac
		}
	}
	return ""
}

func (l *leases) offer(mac string) net.IP {
	l.mu.Lock()
	defer l.mu.Unlock()
	if le, ok := l.byMAC[mac]; ok {
		return le.ip
	}
	for v := ipToU32(l.pool.Start); v <= ipToU32(l.pool.End); v++ {
		ip := u32ToIP(v)
		if l.takenBy(ip) == "" {
			l.byMAC[mac] = lease{ip: ip, expires: time.Now().Add(30 * time.Second)}
			return ip
		}
	}
	return nil
}

func (l *leases) commit(mac string, ip net.IP) net.IP {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.inPool(ip) {
		return nil
	}
	if owner := l.takenBy(ip); owner != "" && owner != mac {
		return nil
	}
	l.byMAC[mac] = lease{ip: ip, expires: time.Now().Add(l.pool.LeaseTime)}
	return ip
}

func (l *leases) release(mac string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byMAC, mac)
}
