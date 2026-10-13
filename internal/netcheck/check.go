// Package netcheck diagnoses whether the local network is ready for PXE/iPXE
// booting and suggests fixes based on what it finds.
package netcheck

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	OK    Status = "ok"
	Info  Status = "info"
	Warn  Status = "warn"
	Error Status = "error"
)

// Fix is a one-click settings change the UI can apply.
type Fix struct {
	Label    string            `json:"label"`
	Settings map[string]string `json:"settings"`
}

type Check struct {
	ID         string `json:"id"`
	Status     Status `json:"status"`
	Title      string `json:"title"`
	Detail     string `json:"detail,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
	Fix        *Fix   `json:"fix,omitempty"`
}

type Report struct {
	CheckedAt time.Time      `json:"checkedAt"`
	Status    Status         `json:"status"` // worst status
	Counts    map[Status]int `json:"counts"`
	Checks    []Check        `json:"checks"`
	Offers    []Offer        `json:"offers"`
	Probed    bool           `json:"probed"`
}

type ServiceState struct {
	State string
	Error string
}

// Funnel counts recent boot events per stage; it shows where clients get stuck.
type Funnel struct {
	FirmwareDHCP int // DHCP replies to firmware PXE clients
	TFTP         int // iPXE binaries sent
	IPXEDHCP     int // DHCP replies to clients already running iPXE
	Menu         int // boot menu fetched
	Boot         int // ISO boot started
}

// Input is everything the analysis looks at. Gathering it is the caller's job,
// which keeps this package testable without a network.
type Input struct {
	Mode          string // proxy, full, bootstrap
	Interface     string
	InterfaceAuto bool
	ServerIP      net.IP
	Subnet        *net.IPNet
	OtherIfaces   []string // other usable interfaces with IPv4, "name (cidr)"
	Wireless      bool
	InDocker      bool
	Firewall      string // "ufw", "firewalld" or ""
	HTTPPort      int
	Services      map[string]ServiceState
	PoolStart     string
	PoolEnd       string

	Probed    bool
	ProbeErr  error
	Offers    []Offer // replies seen on the LAN (never includes this host's own)
	SelfOffer *Offer  // how iPXE Multiboot's own DHCP service answers a PXE client

	ISOCount    int
	ISODir      string
	ISOWritable bool

	Funnel Funnel
}

func Analyze(in Input) Report {
	var checks []Check
	add := func(c Check) { checks = append(checks, c) }

	checkInterface(in, add)
	if in.ServerIP == nil {
		return finish(in, checks)
	}
	checkServices(in, add)
	checkDHCP(in, add)
	checkFirewall(in, add)
	checkFunnel(in, add)
	checkLibrary(in, add)
	return finish(in, checks)
}

func finish(in Input, checks []Check) Report {
	rank := map[Status]int{Error: 0, Warn: 1, Info: 2, OK: 3}
	sort.SliceStable(checks, func(i, j int) bool { return rank[checks[i].Status] < rank[checks[j].Status] })
	r := Report{CheckedAt: time.Now(), Status: OK, Counts: map[Status]int{}, Checks: checks, Offers: in.Offers, Probed: in.Probed}
	if r.Offers == nil {
		r.Offers = []Offer{}
	}
	for _, c := range checks {
		r.Counts[c.Status]++
		if rank[c.Status] < rank[r.Status] {
			r.Status = c.Status
		}
	}
	return r
}

func checkInterface(in Input, add func(Check)) {
	if in.ServerIP == nil {
		add(Check{ID: "interface.none", Status: Error, Title: "No usable network interface",
			Detail:     "iPXE Multiboot couldn't find a network interface with an IPv4 address to serve on.",
			Suggestion: "Connect the server to the network the clients boot from, then pick the interface under Settings → Network."})
		return
	}
	if in.ServerIP.IsLinkLocalUnicast() {
		add(Check{ID: "interface.linklocal", Status: Error, Title: "Interface only has a link-local address",
			Detail:     fmt.Sprintf("%s has %s, which means it never got an address from DHCP.", in.Interface, in.ServerIP),
			Suggestion: "Give the server a static IP address (recommended for boot servers) or fix its DHCP lease."})
	}
	if ones, bits := in.Subnet.Mask.Size(); bits == 32 && ones >= 31 {
		add(Check{ID: "interface.p2p", Status: Warn, Title: "Point-to-point subnet",
			Detail:     fmt.Sprintf("%s is a /%d network, so no other machines share it.", in.Subnet, ones),
			Suggestion: "PXE clients must be on the same layer-2 network. Pick the interface connected to your LAN."})
	}
	if in.InDocker && isDockerBridge(in.ServerIP) {
		add(Check{ID: "interface.docker", Status: Error, Title: "Running inside a Docker bridge network",
			Detail:     fmt.Sprintf("The container's address %s is on Docker's private bridge. PXE broadcasts from your LAN never reach it.", in.ServerIP),
			Suggestion: "Run the container with host networking (network_mode: host in docker-compose.yml)."})
	}
	if in.Wireless {
		add(Check{ID: "interface.wireless", Status: Warn, Title: "Serving on a Wi-Fi interface",
			Detail:     fmt.Sprintf("%s is wireless. PXE clients boot over Ethernet, and many access points don't forward DHCP broadcasts between wired and wireless clients.", in.Interface),
			Suggestion: "Connect the server with an Ethernet cable to the same switch or router as the clients."})
	}
	summary := fmt.Sprintf("Serving on %s with address %s (%s).", in.Interface, in.ServerIP, in.Subnet)
	if in.InterfaceAuto && len(in.OtherIfaces) > 0 {
		add(Check{ID: "interface.auto", Status: Info, Title: "Interface picked automatically",
			Detail:     summary + " Other interfaces: " + strings.Join(in.OtherIfaces, ", ") + ".",
			Suggestion: "If your clients are on one of the other networks, select that interface under Settings → Network."})
	} else {
		add(Check{ID: "interface.ok", Status: OK, Title: "Network interface", Detail: summary})
	}
}

func isDockerBridge(ip net.IP) bool {
	_, docker, _ := net.ParseCIDR("172.16.0.0/12")
	return docker.Contains(ip)
}

func checkServices(in Input, add func(Check)) {
	for _, name := range []string{"dhcp", "tftp"} {
		s, ok := in.Services[name]
		if !ok || s.State != "error" {
			continue
		}
		c := Check{ID: "service." + name, Status: Error, Title: strings.ToUpper(name) + " service failed", Detail: s.Error}
		switch {
		case strings.Contains(s.Error, "permission denied") || strings.Contains(s.Error, "not permitted"):
			c.Suggestion = "Run iPXE Multiboot as root, or grant CAP_NET_BIND_SERVICE and CAP_NET_RAW (the provided systemd unit and Docker setup do this)."
		case strings.Contains(s.Error, "address already in use"):
			c.Suggestion = "Another DHCP/TFTP server runs on this host (dnsmasq, isc-dhcp-server, tftpd-hpa…). Stop it, or use Boot server only mode if it should keep handling DHCP."
			if name == "dhcp" && in.Mode != "bootstrap" {
				c.Fix = &Fix{Label: "Switch to Boot server only", Settings: map[string]string{"dhcpMode": "bootstrap"}}
			}
		}
		add(c)
	}
}

func checkDHCP(in Input, add func(Check)) {
	if !in.Probed {
		if in.ProbeErr != nil {
			add(Check{ID: "probe.failed", Status: Info, Title: "Couldn't scan for DHCP servers",
				Detail:     in.ProbeErr.Error(),
				Suggestion: "The scan needs root or CAP_NET_RAW. Without it, iPXE Multiboot can't verify the DHCP setup automatically."})
		}
		return
	}
	self := in.ServerIP.String()
	var real, others, ours []Offer
	if in.SelfOffer != nil {
		ours = append(ours, *in.SelfOffer)
	}
	for _, o := range in.Offers {
		switch {
		case o.ServerID == self:
			// Shouldn't happen (the host can't see its own broadcasts); ignore.
		case o.Proxy():
			others = append(others, o)
		default:
			real = append(real, o)
		}
	}

	for _, o := range others {
		add(Check{ID: "dhcp.otherpxe", Status: Error, Title: "Another PXE server is active",
			Detail:     fmt.Sprintf("%s answers network-boot requests too (boot file %q). Clients may boot from it instead of iPXE Multiboot.", o.ServerID, o.BootFile),
			Suggestion: "Turn off the other PXE/proxyDHCP server (FOG, WDS, iVentoy, dnsmasq proxy…), or point it at iPXE Multiboot."})
	}

	switch in.Mode {
	case "proxy":
		if len(real) == 0 {
			add(Check{ID: "dhcp.noserver", Status: Error, Title: "No DHCP server found",
				Detail:     "Proxy DHCP only adds boot information. It relies on another DHCP server (usually your router) to hand out IP addresses, but none answered.",
				Suggestion: "If this is an isolated network without a router, let iPXE Multiboot assign addresses itself.",
				Fix:        fullModeFix(in, nil)})
		} else {
			for _, o := range real {
				c := Check{ID: "dhcp.server", Status: OK, Title: "DHCP server found",
					Detail: fmt.Sprintf("%s assigns addresses (offered %s). iPXE Multiboot adds the boot information on top.", o.ServerID, o.YourIP)}
				if o.HasBootInfo() && o.NextServer != self {
					c.Status = Warn
					c.Title = "Your DHCP server already sends PXE options"
					c.Detail = fmt.Sprintf("%s tells clients to boot %q from %s. Some firmware prefers these over iPXE Multiboot's proxy offer.", o.ServerID, o.BootFile, orDash(o.NextServer))
					c.Suggestion = "Remove the network boot options (next-server / filename, options 66/67) from your router, or point them at iPXE Multiboot and use Boot server only mode."
				}
				add(c)
			}
		}
		if len(ours) > 0 && in.Services["dhcp"].State == "running" {
			add(Check{ID: "dhcp.selfok", Status: OK, Title: "iPXE Multiboot answers PXE requests",
				Detail: fmt.Sprintf("Proxy DHCP is listening and offers UEFI clients %q.", ours[0].BootFile)})
		}

	case "full":
		if len(real) > 0 {
			ids := make([]string, len(real))
			for i, o := range real {
				ids[i] = o.ServerID
			}
			add(Check{ID: "dhcp.conflict", Status: Error, Title: "Another DHCP server is active",
				Detail:     fmt.Sprintf("%s also hands out addresses. Two DHCP servers on one network cause address conflicts and random boot failures.", strings.Join(ids, ", ")),
				Suggestion: "Keep your router's DHCP and switch iPXE Multiboot to Proxy DHCP, or disable DHCP on the other server.",
				Fix:        &Fix{Label: "Switch to Proxy DHCP", Settings: map[string]string{"dhcpMode": "proxy"}}})
		}
		if len(ours) > 0 && in.Services["dhcp"].State == "running" {
			add(Check{ID: "dhcp.selfok", Status: OK, Title: "iPXE Multiboot's DHCP server works",
				Detail: fmt.Sprintf("It offers PXE clients an address such as %s and boot file %q.", ours[0].YourIP, ours[0].BootFile)})
		} else if len(ours) == 0 && in.Services["dhcp"].State == "running" {
			add(Check{ID: "dhcp.poolfull", Status: Error, Title: "iPXE Multiboot can't offer addresses",
				Detail: "The DHCP service is running but has no free address in its pool.", Fix: fullModeFix(in, nil)})
		}
		checkPool(in, add)

	case "bootstrap":
		if len(real) == 0 {
			add(Check{ID: "dhcp.noserver", Status: Error, Title: "No DHCP server found",
				Detail:     "Boot server only mode needs your own DHCP server to send clients to iPXE Multiboot, but none answered.",
				Suggestion: "Check your router's DHCP server, or let iPXE Multiboot handle DHCP.",
				Fix:        fullModeFix(in, nil)})
			break
		}
		pointed := false
		for _, o := range real {
			if o.NextServer == self && o.BootFile != "" {
				pointed = true
				add(Check{ID: "dhcp.bootstrapok", Status: OK, Title: "DHCP server points to iPXE Multiboot",
					Detail: fmt.Sprintf("%s sends next-server %s and boot file %q.", o.ServerID, o.NextServer, o.BootFile)})
				if !knownBootFile(o.BootFile) {
					add(Check{ID: "dhcp.bootfile", Status: Warn, Title: "Unexpected boot file name",
						Detail:     fmt.Sprintf("Your DHCP server sends %q, which iPXE Multiboot doesn't serve.", o.BootFile),
						Suggestion: "Use ipxe.efi for UEFI clients and undionly.kpxe for BIOS clients."})
				}
			}
		}
		if !pointed {
			o := real[0]
			detail := fmt.Sprintf("%s doesn't send network boot options to PXE clients.", o.ServerID)
			if o.HasBootInfo() {
				detail = fmt.Sprintf("%s sends clients to %s (boot file %q) instead of iPXE Multiboot.", o.ServerID, orDash(o.NextServer), o.BootFile)
			}
			add(Check{ID: "dhcp.notpointed", Status: Error, Title: "DHCP server isn't pointing to iPXE Multiboot",
				Detail:     detail,
				Suggestion: fmt.Sprintf("On your DHCP server set next-server (option 66) to %s and the boot file (option 67) to ipxe.efi (UEFI) or undionly.kpxe (BIOS). Or let iPXE Multiboot add these automatically with Proxy DHCP.", self),
				Fix:        &Fix{Label: "Switch to Proxy DHCP", Settings: map[string]string{"dhcpMode": "proxy"}}})
		}
	}
}

func knownBootFile(f string) bool {
	f = strings.TrimPrefix(f, "/")
	return f == "ipxe.efi" || f == "undionly.kpxe" || strings.HasSuffix(f, "/boot.ipxe") || strings.HasSuffix(f, "/ipxe/ipxe.efi")
}

func checkPool(in Input, add func(Check)) {
	start, end := net.ParseIP(in.PoolStart).To4(), net.ParseIP(in.PoolEnd).To4()
	if start == nil || end == nil {
		return
	}
	s, e, self := ip4(start), ip4(end), ip4(in.ServerIP)
	switch {
	case s > e:
		add(Check{ID: "pool.order", Status: Error, Title: "Address pool is reversed",
			Detail: fmt.Sprintf("The pool starts at %s but ends at %s.", start, end), Fix: fullModeFix(in, nil)})
	case self >= s && self <= e:
		add(Check{ID: "pool.self", Status: Error, Title: "Pool contains iPXE Multiboot's own address",
			Detail:     fmt.Sprintf("%s is inside the pool %s – %s, so it could be handed to a client.", in.ServerIP, start, end),
			Suggestion: "Move the pool so it excludes the server's address.", Fix: fullModeFix(in, nil)})
	default:
		add(Check{ID: "pool.ok", Status: OK, Title: "Address pool",
			Detail: fmt.Sprintf("%d addresses from %s to %s.", e-s+1, start, end)})
	}
}

func checkFirewall(in Input, add func(Check)) {
	if in.Firewall == "" {
		return
	}
	ports := "69/udp and " + fmt.Sprint(in.HTTPPort) + "/tcp"
	if in.Mode != "bootstrap" {
		ports = "67/udp, 4011/udp, " + ports
	}
	cmd := ""
	switch in.Firewall {
	case "ufw":
		cmd = fmt.Sprintf("sudo ufw allow 67/udp && sudo ufw allow 69/udp && sudo ufw allow 4011/udp && sudo ufw allow %d/tcp", in.HTTPPort)
	case "firewalld":
		cmd = fmt.Sprintf("sudo firewall-cmd --permanent --add-service=dhcp --add-service=tftp --add-port=4011/udp --add-port=%d/tcp && sudo firewall-cmd --reload", in.HTTPPort)
	}
	add(Check{ID: "firewall", Status: Warn, Title: "Firewall is active (" + in.Firewall + ")",
		Detail:     "Make sure it allows " + ports + ", or clients won't be able to boot.",
		Suggestion: cmd})
}

func checkFunnel(in Input, add func(Check)) {
	f := in.Funnel
	switch {
	case f.FirmwareDHCP > 0 && f.TFTP == 0 && f.IPXEDHCP == 0 && f.Menu == 0:
		add(Check{ID: "funnel.tftp", Status: Error, Title: "Clients get a boot offer but never download iPXE",
			Detail:     fmt.Sprintf("%d PXE requests were answered recently, but no client fetched the iPXE binary over TFTP.", f.FirmwareDHCP),
			Suggestion: "UDP port 69 is probably blocked between the clients and iPXE Multiboot (host firewall, or a VLAN/ACL on the switch). Also check that Secure Boot is disabled on the clients."})
	case f.TFTP > 0 && f.Menu == 0 && f.IPXEDHCP == 0:
		add(Check{ID: "funnel.http", Status: Error, Title: "iPXE loads but never reaches the boot menu",
			Detail:     fmt.Sprintf("iPXE was sent %d times, but no client requested the menu over HTTP.", f.TFTP),
			Suggestion: fmt.Sprintf("Check that TCP port %d is reachable from the clients and that your DHCP server doesn't send its own filename to iPXE clients.", in.HTTPPort)})
	case f.Menu > 0:
		add(Check{ID: "funnel.ok", Status: OK, Title: "Clients reach the boot menu",
			Detail: fmt.Sprintf("Recently: %d boot menus shown, %d ISO boots started.", f.Menu, f.Boot)})
	}
}

func checkLibrary(in Input, add func(Check)) {
	if in.ISOCount == 0 {
		add(Check{ID: "library.empty", Status: Warn, Title: "No ISO files yet",
			Detail:     "The boot menu is empty until you add ISO files.",
			Suggestion: "Upload ISOs on the ISO library page, or copy them into " + in.ISODir + "."})
	}
	if !in.ISOWritable {
		add(Check{ID: "library.readonly", Status: Warn, Title: "ISO folder isn't writable",
			Detail:     in.ISODir + " can't be written to, so uploads from the web interface will fail.",
			Suggestion: "Fix the folder permissions or choose another folder under Settings → Storage."})
	}
}

// fullModeFix proposes full DHCP mode with a pool that fits the subnet and
// leaves out the server's own address.
func fullModeFix(in Input, router net.IP) *Fix {
	start, end := SuggestPool(in.Subnet, in.ServerIP)
	if start == nil {
		return nil
	}
	settings := map[string]string{"dhcpMode": "full", "poolStart": start.String(), "poolEnd": end.String()}
	if router != nil {
		settings["router"] = router.String()
	}
	return &Fix{Label: fmt.Sprintf("Use Full DHCP (%s – %s)", start, end), Settings: settings}
}

// SuggestPool picks up to 100 addresses in the upper part of the server's
// subnet (within its /24 for large networks), avoiding the server address.
func SuggestPool(subnet *net.IPNet, server net.IP) (net.IP, net.IP) {
	if subnet == nil || server.To4() == nil {
		return nil, nil
	}
	ones, _ := subnet.Mask.Size()
	self := ip4(server.To4())
	var lo, hi uint32
	if ones <= 24 {
		lo = self &^ 0xff
		hi = lo | 0xff
	} else {
		lo = ip4(subnet.IP.To4().Mask(subnet.Mask))
		hi = lo | ^ip4(net.IP(subnet.Mask).To4())
	}
	size := hi - lo + 1
	if size < 8 {
		return nil, nil
	}
	start, end := lo+size*100/256, lo+size*200/256-1
	if size < 256 {
		start, end = lo+size/2, hi-1
	}
	if self >= start && self <= end {
		// Use the half above the server if it fits, otherwise below.
		if hi-1-self >= self-start {
			start = self + 1
		} else {
			end = self - 1
		}
	}
	return u32(start), u32(end)
}

func ip4(ip net.IP) uint32 { return binary.BigEndian.Uint32(ip.To4()) }
func u32(v uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, v)
	return ip
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
