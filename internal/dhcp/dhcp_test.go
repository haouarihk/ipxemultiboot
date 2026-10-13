package dhcp

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

func discover(t *testing.T, arch uint16, ipxe bool) *dhcpv4.DHCPv4 {
	t.Helper()
	mac, _ := net.ParseMAC("52:54:00:12:34:56")
	m, err := dhcpv4.NewDiscovery(mac)
	if err != nil {
		t.Fatal(err)
	}
	m.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00000:UNDI:002001"))
	a := make([]byte, 2)
	binary.BigEndian.PutUint16(a, arch)
	m.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionClientSystemArchitectureType, a))
	if ipxe {
		m.UpdateOption(dhcpv4.OptUserClass("iPXE"))
	}
	return m
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{"proxy": ModeProxy, "FULL": ModeFull, "bootstrap": ModeBootstrap, "off": ModeBootstrap, "none": ModeBootstrap} {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseMode("bogus"); err == nil {
		t.Error("expected error for bogus mode")
	}
}

func TestBootFile(t *testing.T) {
	s := New(Config{Mode: ModeProxy, ServerIP: net.IPv4(10, 0, 0, 1), HTTPPort: 8080})
	cases := []struct {
		arch uint16
		ipxe bool
		want string
	}{
		{0, false, "undionly.kpxe"},
		{7, false, "ipxe.efi"},
		{9, false, "ipxe.efi"},
		{16, false, "http://10.0.0.1:8080/ipxe/ipxe.efi"},
		{0, true, "http://10.0.0.1:8080/boot.ipxe"},
		{7, true, "http://10.0.0.1:8080/boot.ipxe"},
	}
	for _, c := range cases {
		got, ok := s.bootFile(clientInfo{arch: c.arch, ipxe: c.ipxe})
		if !ok || got != c.want {
			t.Errorf("arch %d ipxe %v: got %q, want %q", c.arch, c.ipxe, got, c.want)
		}
	}
	if _, ok := s.bootFile(clientInfo{arch: 99}); ok {
		t.Error("unknown arch should not get a boot file")
	}
}

func TestProxyOffer(t *testing.T) {
	s := New(Config{Mode: ModeProxy, ServerIP: net.IPv4(10, 0, 0, 1), HTTPPort: 8080})
	m := discover(t, 7, false)
	reply, err := s.proxyReply(m, inspect(m), 67)
	if err != nil || reply == nil {
		t.Fatalf("reply=%v err=%v", reply, err)
	}
	if reply.MessageType() != dhcpv4.MessageTypeOffer {
		t.Errorf("type = %s", reply.MessageType())
	}
	if !reply.YourIPAddr.IsUnspecified() {
		t.Errorf("proxy offer must not assign an address, got %s", reply.YourIPAddr)
	}
	if reply.BootFileName != "ipxe.efi" || !reply.ServerIPAddr.Equal(net.IPv4(10, 0, 0, 1)) {
		t.Errorf("file=%q siaddr=%s", reply.BootFileName, reply.ServerIPAddr)
	}
	if !bytes.Equal(reply.Options.Get(dhcpv4.OptionVendorSpecificInformation), pxeVendorOptions) {
		t.Error("missing PXE discovery control option 43")
	}
	if reply.ClassIdentifier() != "PXEClient" {
		t.Errorf("vendor class = %q", reply.ClassIdentifier())
	}
}

func TestProxyIgnoresRequestsOnPort67(t *testing.T) {
	s := New(Config{Mode: ModeProxy, ServerIP: net.IPv4(10, 0, 0, 1), HTTPPort: 8080})
	m := discover(t, 0, false)
	m.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeRequest))
	if r, _ := s.proxyReply(m, inspect(m), 67); r != nil {
		t.Error("proxy must leave REQUESTs on :67 to the real DHCP server")
	}
	if r, _ := s.proxyReply(m, inspect(m), 4011); r == nil || r.MessageType() != dhcpv4.MessageTypeAck {
		t.Error("proxy must ACK REQUESTs on :4011")
	}
}

func TestLeases(t *testing.T) {
	l := newLeases(Pool{Start: net.IPv4(10, 0, 0, 10).To4(), End: net.IPv4(10, 0, 0, 11).To4(), LeaseTime: time.Hour})
	a := l.offer("aa")
	b := l.offer("bb")
	if a.Equal(b) {
		t.Fatal("two clients offered the same address")
	}
	if c := l.offer("cc"); c != nil {
		t.Errorf("pool exhausted, but offered %s", c)
	}
	if got := l.offer("aa"); !got.Equal(a) {
		t.Errorf("returning client got %s, want %s", got, a)
	}
	if l.commit("bb", a) != nil {
		t.Error("committed an address held by another client")
	}
	if l.commit("aa", net.IPv4(10, 0, 0, 99)) != nil {
		t.Error("committed an address outside the pool")
	}
	if !l.commit("aa", a).Equal(a) {
		t.Error("commit of own offer failed")
	}
	l.release("aa")
	if got := l.offer("cc"); !got.Equal(a) {
		t.Errorf("released address not reused: got %s", got)
	}
}

func TestFullModeAck(t *testing.T) {
	ipnet := &net.IPNet{IP: net.IPv4(10, 0, 0, 1), Mask: net.CIDRMask(24, 32)}
	s := New(Config{Mode: ModeFull, ServerIP: net.IPv4(10, 0, 0, 1).To4(), HTTPPort: 8080, Pool: Pool{
		Start: net.IPv4(10, 0, 0, 100).To4(), End: net.IPv4(10, 0, 0, 110).To4(), Netmask: ipnet.Mask,
		Router: net.IPv4(10, 0, 0, 254).To4(), LeaseTime: time.Hour,
	}})
	m := discover(t, 0, true)
	offer, err := s.fullReply(m, inspect(m))
	if err != nil || offer.MessageType() != dhcpv4.MessageTypeOffer {
		t.Fatalf("offer=%v err=%v", offer, err)
	}
	if offer.BootFileName != "http://10.0.0.1:8080/boot.ipxe" {
		t.Errorf("iPXE client got file %q", offer.BootFileName)
	}
	req, _ := dhcpv4.NewRequestFromOffer(offer)
	ack, err := s.fullReply(req, inspect(req))
	if err != nil || ack.MessageType() != dhcpv4.MessageTypeAck || !ack.YourIPAddr.Equal(offer.YourIPAddr) {
		t.Fatalf("ack=%v err=%v", ack, err)
	}
	if !ack.Router()[0].Equal(net.IPv4(10, 0, 0, 254)) {
		t.Errorf("router = %v", ack.Router())
	}
}
