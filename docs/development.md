# Development

## Layout

```text
cmd/ipxemultiboot/        main: flags/env → app
cmd/isoinspect/       debugging tool: list/cat files inside an ISO
internal/app/         settings (SQLite over env defaults), service supervision, client tracking
internal/dhcp/        proxyDHCP / full DHCP server (PXE option handling, leases)
internal/tftpd/       TFTP server for the iPXE binaries and generated autoexec.ipxe
internal/httpd/       boot routes: /boot.ipxe, /menu.ipxe, /iso/<id>/<path>, /isos/<id>
internal/iso9660/     minimal ISO9660 + Rock Ridge + Joliet reader (files as io.ReaderAt)
internal/distro/      distro detection and netboot recipes
internal/netcheck/    network diagnostics: raw-socket DHCP probe + rule-based analysis/suggestions
internal/library/     ISO folder scanning and caching
internal/api/         JSON API for the web interface (auth, settings, uploads, SSE events)
internal/store/       SQLite persistence
internal/events/      in-memory pub/sub for the live activity feed
internal/bootfiles/   embedded iPXE binaries (built by scripts/build-ipxe.sh)
web/                  React + TypeScript + Tailwind frontend, embedded via web/embed.go
proxmox/              community-scripts style Proxmox LXC helper
```

## Running locally

```sh
make web build
sudo IPXEMULTIBOOT_DATA_DIR=./data IPXEMULTIBOOT_ISO_DIR=./isos ./bin/ipxemultiboot -v
```

Without root, everything except DHCP/TFTP works. To hack on the UI with hot reload, run the
backend on `:8080` and `make dev-web` in another terminal. Vite proxies `/api` to the backend.

UI strings live in `web/src/locales/`. `en.ts` defines the keys, and the other languages are
type-checked against it, so a missing translation fails `pnpm typecheck`.

## Testing real PXE boots without root

`scripts/qemu-test.sh [bios|uefi] [full|proxy|bootstrap]` creates a user and network namespace
(`unshare -rn`) containing a tap device, iPXE Multiboot, optionally dnsmasq as a "router", and a QEMU
VM that boots from the network:

- `bios`: SeaBIOS with QEMU's iPXE NIC ROM
- `uefi`: OVMF with the NIC ROM disabled, so the firmware's own PXE stack is used (real TFTP path)

Drive the VM with `scripts/qmp.py` (screenshots, key presses):

```sh
scripts/qemu-test.sh uefi proxy &
scripts/qmp.py /tmp/ipxemultiboot-test/qmp.sock shot menu.png
scripts/qmp.py /tmp/ipxemultiboot-test/qmp.sock keys down ret
```

`scripts/regress.sh` checks that every firmware × DHCP mode combination reaches the boot menu.
Logs are in `/tmp/ipxemultiboot-test/`.

Requirements: `qemu-system-x86_64`, OVMF (`/usr/share/edk2/x64`), `dnsmasq`, KVM access and
unprivileged user namespaces.

## Network check

`internal/netcheck` has two parts. `Probe` broadcasts a PXE-style DHCPDISCOVER through a raw
socket (with a random MAC and the broadcast flag set) and collects every reply for 6 seconds.
Many DHCP servers ping-check an address for about 3 seconds before offering it. `Analyze` is a pure
function that turns the probe results, settings, environment and recent boot-event counts into
checks with suggestions and optional one-click fixes. It's unit-tested without a network. The
host can't see its own broadcasts, so iPXE Multiboot's own DHCP answer is verified in-process
(`dhcp.Server.Simulate`).

## Rebuilding iPXE

The iPXE binaries in `internal/bootfiles/assets/` are committed so that `go build` works without
a C toolchain. Rebuild them with `make ipxe` (needs gcc, make, perl and liblzma headers). CI checks
that they still build, and release builds always rebuild them from source.
