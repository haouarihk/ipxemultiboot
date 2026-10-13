#!/usr/bin/env bash
# Installs or updates iPXE Multiboot from the latest GitHub release on a systemd host.
#
#   curl -fsSL https://raw.githubusercontent.com/haouarihk/ipxemultiboot/main/scripts/install.sh | sudo bash
#
# Env: IPXEMULTIBOOT_VERSION (default: latest), IPXEMULTIBOOT_REPO (default: haouarihk/ipxemultiboot)
set -euo pipefail

REPO="${IPXEMULTIBOOT_REPO:-haouarihk/ipxemultiboot}"
VERSION="${IPXEMULTIBOOT_VERSION:-latest}"

die() { echo "error: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "run as root (sudo)"
command -v systemctl >/dev/null || die "systemd is required"
command -v curl >/dev/null || die "curl is required"

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if [ "$VERSION" = latest ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$VERSION" ] || die "could not determine the latest release"
fi
asset="ipxemultiboot_${VERSION#v}_linux_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$VERSION/$asset"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $asset ..."
curl -fsSL -o "$tmp/$asset" "$url"
curl -fsSL -o "$tmp/checksums.txt" "https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
(cd "$tmp" && grep " $asset\$" checksums.txt | sha256sum -c -) || die "checksum mismatch"
tar -xzf "$tmp/$asset" -C "$tmp"

if ! id ipxemultiboot >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/ipxemultiboot --shell /usr/sbin/nologin ipxemultiboot
fi
install -d -o ipxemultiboot -g ipxemultiboot -m 0750 /var/lib/ipxemultiboot
install -d -o ipxemultiboot -g ipxemultiboot -m 0755 /var/lib/ipxemultiboot/isos
install -d -m 0755 /etc/ipxemultiboot
[ -f /etc/haouarihk/ipxemultiboot.env ] || cat >/etc/haouarihk/ipxemultiboot.env <<'ENV'
# iPXE Multiboot defaults. Settings saved in the web interface take precedence.
#IPXEMULTIBOOT_DHCP_MODE=proxy
#IPXEMULTIBOOT_IFACE=eth0
#IPXEMULTIBOOT_HTTP_PORT=8080
#IPXEMULTIBOOT_ISO_DIR=/var/lib/ipxemultiboot/isos
ENV

systemctl stop ipxemultiboot 2>/dev/null || true
install -m 0755 "$tmp/ipxemultiboot" /usr/local/bin/ipxemultiboot
install -m 0644 "$tmp/ipxemultiboot.service" /etc/systemd/system/ipxemultiboot.service
systemctl daemon-reload
systemctl enable --now ipxemultiboot

ip=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')
echo
echo "iPXE Multiboot $VERSION is running: http://${ip:-<this-host>}:8080"
echo "ISO folder: /var/lib/ipxemultiboot/isos   Config: /etc/haouarihk/ipxemultiboot.env"
