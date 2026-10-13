package netcheck

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

// Offer is one DHCP reply seen during a probe.
type Offer struct {
	ServerID   string   `json:"serverId"`
	YourIP     string   `json:"yourIp,omitempty"` // empty for proxyDHCP offers
	NextServer string   `json:"nextServer,omitempty"`
	BootFile   string   `json:"bootFile,omitempty"`
	Router     string   `json:"router,omitempty"`
	DNS        []string `json:"dns,omitempty"`
	Netmask    string   `json:"netmask,omitempty"`
	PXEVendor  bool     `json:"pxeVendor"` // reply carries vendor class PXEClient
}

// Proxy reports whether the offer is an address-less proxyDHCP offer.
func (o Offer) Proxy() bool { return o.YourIP == "" }

// HasBootInfo reports whether the offer tells clients where to boot from.
func (o Offer) HasBootInfo() bool { return o.BootFile != "" }

// RandomMAC returns a locally administered unicast MAC for probing, so the
// probe doesn't collide with the host's own lease on strict DHCP servers.
func RandomMAC() net.HardwareAddr {
	mac := make(net.HardwareAddr, 6)
	rand.Read(mac)
	mac[0] = (mac[0] | 0x02) &^ 0x01
	return mac
}

// Probe broadcasts a DHCPDISCOVER from mac that looks like a UEFI PXE client
// and collects every reply until the timeout. It never sends a REQUEST, so no
// lease is taken. Needs CAP_NET_RAW. The host can't see its own outgoing
// broadcasts this way, so iPXE Multiboot's own replies must be tracked separately.
func Probe(ctx context.Context, iface string, mac net.HardwareAddr, timeout time.Duration) ([]Offer, error) {
	conn, err := nclient4.NewRawUDPConn(iface, 68)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	disc, err := dhcpv4.NewDiscovery(mac,
		// Ask servers to broadcast: a unicast reply to the made-up MAC would
		// be dropped by this host's NIC.
		dhcpv4.WithBroadcast(true),
		dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00007:UNDI:003016")),
		dhcpv4.WithOption(dhcpv4.OptGeneric(dhcpv4.OptionClientSystemArchitectureType, []byte{0, 7})),
		dhcpv4.WithRequestedOptions(dhcpv4.OptionTFTPServerName, dhcpv4.OptionBootfileName,
			dhcpv4.OptionVendorSpecificInformation, dhcpv4.OptionClassIdentifier),
	)
	if err != nil {
		return nil, err
	}
	bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: 67}
	if _, err := conn.WriteTo(disc.ToBytes(), bcast); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	// Resend once halfway: some servers drop the first DISCOVER from an unknown MAC.
	go func() {
		select {
		case <-time.After(timeout / 3):
			conn.WriteTo(disc.ToBytes(), bcast)
		case <-ctx.Done():
		}
	}()

	seen := map[string]bool{}
	var offers []Offer
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return offers, nil
			}
			return offers, err
		}
		m, err := dhcpv4.FromBytes(buf[:n])
		if err != nil || m.OpCode != dhcpv4.OpcodeBootReply || m.TransactionID != disc.TransactionID {
			continue
		}
		if mt := m.MessageType(); mt != dhcpv4.MessageTypeOffer && mt != dhcpv4.MessageTypeAck {
			continue
		}
		o := parseOffer(m)
		key := o.ServerID + "|" + o.YourIP
		if seen[key] {
			continue
		}
		seen[key] = true
		offers = append(offers, o)
	}
}

func parseOffer(m *dhcpv4.DHCPv4) Offer {
	o := Offer{PXEVendor: m.ClassIdentifier() == "PXEClient" || m.ClassIdentifier() == "HTTPClient"}
	if sid := m.ServerIdentifier(); sid != nil {
		o.ServerID = sid.String()
	} else if m.ServerIPAddr != nil {
		o.ServerID = m.ServerIPAddr.String()
	}
	if ip := m.YourIPAddr; ip != nil && !ip.IsUnspecified() {
		o.YourIP = ip.String()
	}
	if ip := m.ServerIPAddr; ip != nil && !ip.IsUnspecified() {
		o.NextServer = ip.String()
	}
	if s := m.TFTPServerName(); s != "" {
		o.NextServer = s
	}
	o.BootFile = m.BootFileName
	if f := m.BootFileNameOption(); f != "" {
		o.BootFile = f
	}
	if r := m.Router(); len(r) > 0 {
		o.Router = r[0].String()
	}
	for _, d := range m.DNS() {
		o.DNS = append(o.DNS, d.String())
	}
	if mask := m.SubnetMask(); mask != nil {
		o.Netmask = net.IP(mask).String()
	}
	return o
}
