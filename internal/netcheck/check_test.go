package netcheck

import (
	"errors"
	"net"
	"testing"
)

func base(mode string, offers ...Offer) Input {
	_, subnet, _ := net.ParseCIDR("192.168.1.0/24")
	return Input{
		Mode: mode, Interface: "eth0", ServerIP: net.IPv4(192, 168, 1, 10).To4(), Subnet: subnet, HTTPPort: 8080,
		Services: map[string]ServiceState{"dhcp": {State: "running"}, "tftp": {State: "running"}},
		Probed:   true, Offers: offers, ISOCount: 3, ISOWritable: true, ISODir: "/isos",
	}
}

var (
	router   = Offer{ServerID: "192.168.1.1", YourIP: "192.168.1.77", Router: "192.168.1.1"}
	self     = Offer{ServerID: "192.168.1.10", BootFile: "ipxe.efi", NextServer: "192.168.1.10", PXEVendor: true}
	selfDHCP = Offer{ServerID: "192.168.1.10", YourIP: "192.168.1.150", BootFile: "ipxe.efi", NextServer: "192.168.1.10"}
	fog      = Offer{ServerID: "192.168.1.20", BootFile: "undionly.kpxe", NextServer: "192.168.1.20", PXEVendor: true}
)

func find(r Report, id string) *Check {
	for i := range r.Checks {
		if r.Checks[i].ID == id {
			return &r.Checks[i]
		}
	}
	return nil
}

func TestProxyHealthy(t *testing.T) {
	in := base("proxy", router)
	in.SelfOffer = &self
	r := Analyze(in)
	if r.Status != OK {
		t.Fatalf("status = %s, checks = %+v", r.Status, r.Checks)
	}
	if find(r, "dhcp.server") == nil || find(r, "dhcp.selfok") == nil {
		t.Errorf("missing expected OK checks: %+v", r.Checks)
	}
}

func TestProxyWithoutDHCPServerSuggestsFullMode(t *testing.T) {
	r := Analyze(base("proxy", self))
	c := find(r, "dhcp.noserver")
	if c == nil || c.Status != Error || c.Fix == nil {
		t.Fatalf("expected noserver error with fix, got %+v", r.Checks)
	}
	if c.Fix.Settings["dhcpMode"] != "full" || c.Fix.Settings["poolStart"] != "192.168.1.100" || c.Fix.Settings["poolEnd"] != "192.168.1.199" {
		t.Errorf("fix = %+v", c.Fix.Settings)
	}
}

func TestRouterWithPXEOptionsWarns(t *testing.T) {
	r2 := router
	r2.BootFile, r2.NextServer = "pxelinux.0", "192.168.1.5"
	if c := find(Analyze(base("proxy", r2, self)), "dhcp.server"); c == nil || c.Status != Warn {
		t.Errorf("expected warning about router PXE options, got %+v", c)
	}
}

func TestOtherPXEServer(t *testing.T) {
	if c := find(Analyze(base("proxy", router, self, fog)), "dhcp.otherpxe"); c == nil || c.Status != Error {
		t.Errorf("expected other PXE server error, got %+v", c)
	}
}

func TestFullModeConflict(t *testing.T) {
	in := base("full", router)
	in.SelfOffer = &selfDHCP
	in.PoolStart, in.PoolEnd = "192.168.1.100", "192.168.1.199"
	r := Analyze(in)
	c := find(r, "dhcp.conflict")
	if c == nil || c.Fix == nil || c.Fix.Settings["dhcpMode"] != "proxy" {
		t.Fatalf("expected conflict with proxy fix, got %+v", r.Checks)
	}
	if find(r, "pool.ok") == nil {
		t.Error("expected pool.ok")
	}
}

func TestPoolContainsServer(t *testing.T) {
	in := base("full", selfDHCP)
	in.PoolStart, in.PoolEnd = "192.168.1.2", "192.168.1.50"
	if c := find(Analyze(in), "pool.self"); c == nil || c.Status != Error {
		t.Errorf("expected pool.self error")
	}
}

func TestBootstrap(t *testing.T) {
	pointed := router
	pointed.NextServer, pointed.BootFile = "192.168.1.10", "ipxe.efi"
	if c := find(Analyze(base("bootstrap", pointed)), "dhcp.bootstrapok"); c == nil {
		t.Error("expected bootstrapok")
	}
	r := Analyze(base("bootstrap", router))
	if c := find(r, "dhcp.notpointed"); c == nil || c.Status != Error {
		t.Errorf("expected notpointed error, got %+v", r.Checks)
	}
}

func TestProbeFailureIsInfo(t *testing.T) {
	in := base("proxy")
	in.Probed, in.ProbeErr = false, errors.New("operation not permitted")
	r := Analyze(in)
	if c := find(r, "probe.failed"); c == nil || c.Status != Info {
		t.Errorf("expected probe.failed info, got %+v", r.Checks)
	}
}

func TestEnvironment(t *testing.T) {
	in := base("proxy", router, self)
	in.ServerIP = net.IPv4(172, 17, 0, 2).To4()
	_, in.Subnet, _ = net.ParseCIDR("172.17.0.0/16")
	in.InDocker, in.Wireless, in.Firewall = true, true, "ufw"
	in.ISOCount = 0
	r := Analyze(in)
	for _, id := range []string{"interface.docker", "interface.wireless", "firewall", "library.empty"} {
		if find(r, id) == nil {
			t.Errorf("missing %s", id)
		}
	}
	if r.Checks[0].Status != Error {
		t.Error("errors must sort first")
	}
}

func TestFunnel(t *testing.T) {
	in := base("proxy", router, self)
	in.Funnel = Funnel{FirmwareDHCP: 4}
	if find(Analyze(in), "funnel.tftp") == nil {
		t.Error("expected funnel.tftp")
	}
	in.Funnel = Funnel{FirmwareDHCP: 2, TFTP: 2}
	if find(Analyze(in), "funnel.http") == nil {
		t.Error("expected funnel.http")
	}
	in.Funnel = Funnel{FirmwareDHCP: 1, TFTP: 1, IPXEDHCP: 1, Menu: 1, Boot: 1}
	if find(Analyze(in), "funnel.ok") == nil {
		t.Error("expected funnel.ok")
	}
}

func TestServiceErrors(t *testing.T) {
	in := base("proxy", router)
	in.Services["dhcp"] = ServiceState{State: "error", Error: "listen udp :67: bind: address already in use"}
	c := find(Analyze(in), "service.dhcp")
	if c == nil || c.Fix == nil || c.Fix.Settings["dhcpMode"] != "bootstrap" {
		t.Errorf("expected bootstrap fix for port conflict, got %+v", c)
	}
}

func TestSuggestPool(t *testing.T) {
	cases := []struct{ cidr, server, start, end string }{
		{"192.168.1.0/24", "192.168.1.10", "192.168.1.100", "192.168.1.199"},
		{"192.168.1.0/24", "192.168.1.150", "192.168.1.151", "192.168.1.199"},
		{"10.0.0.0/8", "10.20.30.40", "10.20.30.100", "10.20.30.199"},
		{"192.168.1.0/26", "192.168.1.1", "192.168.1.32", "192.168.1.62"},
	}
	for _, c := range cases {
		_, n, _ := net.ParseCIDR(c.cidr)
		s, e := SuggestPool(n, net.ParseIP(c.server))
		if s.String() != c.start || e.String() != c.end {
			t.Errorf("%s/%s: got %s-%s, want %s-%s", c.cidr, c.server, s, e, c.start, c.end)
		}
	}
}
