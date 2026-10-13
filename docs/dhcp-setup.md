# DHCP setup

iPXE Multiboot has three DHCP modes. Pick one in **Settings → Network** (or with `IPXEMULTIBOOT_DHCP_MODE`).

## Proxy DHCP (default, nothing to configure)

Your router keeps assigning IP addresses. iPXE Multiboot listens for DHCP requests from PXE clients
and sends a second, address-less offer containing only the boot information
(proxyDHCP, part of the PXE specification). Any normal DHCP server works with this.

Requirements:

- iPXE Multiboot and the clients are on the same layer-2 network / VLAN.
- UDP ports 67 and 4011 are reachable on the iPXE Multiboot host.
- No other proxyDHCP server (e.g. another PXE server, dnsmasq with `dhcp-range=...,proxy`) runs on the network.

## Full DHCP server

iPXE Multiboot hands out addresses from a pool. Only use this on a network without another DHCP
server, such as an isolated lab switch or after disabling DHCP on your router.
Configure the pool, gateway and DNS servers in the web interface.

## Boot server only (bootstrap)

iPXE Multiboot runs only TFTP and HTTP. You configure your own DHCP server to send clients to it:

| Option | Value |
| --- | --- |
| next-server / TFTP server (option 66, `siaddr`) | iPXE Multiboot's IP address |
| filename for BIOS clients (option 67) | `undionly.kpxe` |
| filename for UEFI x64 clients (option 67) | `ipxe.efi` |
| filename for iPXE / UEFI HTTP clients (optional) | `http://<ipxemultiboot-ip>:8080/boot.ipxe` |

If your DHCP server can't distinguish BIOS and UEFI clients, use `ipxe.efi` for modern UEFI
hardware or `undionly.kpxe` for legacy BIOS. You don't need a special rule for iPXE clients:
iPXE Multiboot's iPXE builds automatically load `autoexec.ipxe` from the next-server, which chains to
the HTTP menu, so there's no boot loop.

Replace `192.168.1.10` with iPXE Multiboot's address in the examples below.

### dnsmasq / Pi-hole

```ini
dhcp-match=set:efi-x64,option:client-arch,7
dhcp-match=set:efi-x64,option:client-arch,9
dhcp-match=set:ipxe,175
dhcp-boot=tag:ipxe,http://192.168.1.10:8080/boot.ipxe
dhcp-boot=tag:!ipxe,tag:efi-x64,ipxe.efi,,192.168.1.10
dhcp-boot=tag:!ipxe,tag:!efi-x64,undionly.kpxe,,192.168.1.10
```

### ISC dhcpd

```conf
option arch code 93 = unsigned integer 16;
next-server 192.168.1.10;
if exists user-class and option user-class = "iPXE" {
  filename "http://192.168.1.10:8080/boot.ipxe";
} elsif option arch = 00:07 or option arch = 00:09 {
  filename "ipxe.efi";
} else {
  filename "undionly.kpxe";
}
```

### Kea

```json
"client-classes": [
  { "name": "ipxe", "test": "substring(option[77].hex,0,4) == 'iPXE'",
    "boot-file-name": "http://192.168.1.10:8080/boot.ipxe" },
  { "name": "efi-x64", "test": "option[93].hex == 0x0007 or option[93].hex == 0x0009",
    "next-server": "192.168.1.10", "boot-file-name": "ipxe.efi" },
  { "name": "bios", "test": "option[93].hex == 0x0000",
    "next-server": "192.168.1.10", "boot-file-name": "undionly.kpxe" }
]
```

### pfSense / OPNsense

*Services → DHCP Server → (interface) → Network Booting* (OPNsense: *TFTP / Network Booting*):

- Enable network booting
- Next Server: `192.168.1.10`
- Default BIOS file name: `undionly.kpxe`
- UEFI 64-bit file name: `ipxe.efi`
- iPXE boot file name (if offered): `http://192.168.1.10:8080/boot.ipxe`

### MikroTik RouterOS

```routeros
/ip dhcp-server network set [find] next-server=192.168.1.10 boot-file-name=ipxe.efi
```

RouterOS 7 can choose per architecture with option sets matching option 93. The single-file
setup above works for UEFI clients.

### OpenWrt

```sh
uci set dhcp.@dnsmasq[0].dhcp_boot='ipxe.efi,,192.168.1.10'
uci commit dhcp && /etc/init.d/dnsmasq restart
```

### UniFi

*Settings → Networks → (network) → Advanced → DHCP Service Management*: enable **Network Boot**,
set the server to `192.168.1.10` and the filename to `ipxe.efi` (or `undionly.kpxe` for BIOS).
