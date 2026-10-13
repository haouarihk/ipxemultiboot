#!/usr/bin/env bash
# End-to-end PXE test without root: creates a private user+network namespace
# with a tap device, runs ipxemultiboot on it and boots a QEMU VM from the network.
#
#   scripts/qemu-test.sh [bios|uefi] [full|proxy|bootstrap]
#
# bios: QEMU's NIC option ROM (iPXE) does the PXE boot.
# uefi: OVMF's own PXE stack (NIC ROM disabled) -> TFTP ipxe.efi -> HTTP menu.
# proxy and bootstrap modes also run dnsmasq as the "router" DHCP server in the
# namespace; in bootstrap mode it is configured like a plain router with static
# next-server/filename options pointing at ipxemultiboot.
#
# Drive the VM with scripts/qmp.py (screenshots, key presses).
# Env: ISO_DIR, WORK (default /tmp/ipxemultiboot-test), MEM (MiB, default 4096).
set -euo pipefail
FW="${1:-bios}"
MODE="${2:-full}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ISO_DIR="${ISO_DIR:-$ROOT/testdata/isos}"
WORK="${WORK:-/tmp/ipxemultiboot-test}"
MEM="${MEM:-4096}"

if [ -z "${IPXEMULTIBOOT_IN_NS:-}" ]; then
  mkdir -p "$WORK"
  rm -f "$WORK"/qmp.sock "$WORK"/*.log
  exec unshare --user --map-root-user --net env IPXEMULTIBOOT_IN_NS=1 "$0" "$@"
fi

ip link set lo up
ip tuntap add tap0 mode tap
ip addr add 10.77.0.1/24 dev tap0
ip link set tap0 up

pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null || true; }
trap cleanup EXIT

router_opts=()
if [ "$MODE" = bootstrap ]; then
  router_opts=(--dhcp-match=set:efi,option:client-arch,7
    --dhcp-boot=tag:!efi,undionly.kpxe,,10.77.0.1
    --dhcp-boot=tag:efi,ipxe.efi,,10.77.0.1)
fi
if [ "$MODE" = proxy ] || [ "$MODE" = bootstrap ]; then
  dnsmasq "${router_opts[@]}" --no-daemon --conf-file=/dev/null --port=0 --interface=tap0 --bind-interfaces \
    --dhcp-range=10.77.0.50,10.77.0.99,1h --user=root --group=root \
    --pid-file="$WORK/dnsmasq.pid" --dhcp-leasefile="$WORK/dnsmasq.leases" --log-dhcp >"$WORK/dnsmasq.log" 2>&1 &
  pids+=($!)
fi

rm -rf "$WORK/data"
"$ROOT/bin/ipxemultiboot" --iface tap0 --iso-dir "$ISO_DIR" --data-dir "$WORK/data" --dhcp-mode "$MODE" \
  --pool-start 10.77.0.100 --pool-end 10.77.0.150 >"$WORK/server.log" 2>&1 &
pids+=($!)
sleep 1

qemu=(qemu-system-x86_64 -enable-kvm -cpu host -smp 2 -m "$MEM"
  -netdev tap,id=n0,ifname=tap0,script=no,downscript=no
  -display none -vga std
  -qmp "unix:$WORK/qmp.sock,server,nowait"
  -serial "file:$WORK/serial.log")
if [ "$FW" = uefi ]; then
  cp /usr/share/edk2/x64/OVMF_VARS.4m.fd "$WORK/vars.fd"
  qemu+=(-drive if=pflash,format=raw,readonly=on,file=/usr/share/edk2/x64/OVMF_CODE.4m.fd
    -drive "if=pflash,format=raw,file=$WORK/vars.fd"
    -device virtio-net-pci,netdev=n0,romfile=,bootindex=1)
else
  qemu+=(-device virtio-net-pci,netdev=n0,bootindex=1 -boot n)
fi
echo "VM starting ($FW, dhcp=$MODE); logs in $WORK"
"${qemu[@]}" 2>"$WORK/qemu.log"
