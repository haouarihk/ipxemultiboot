#!/usr/bin/env bash
# Builds the iPXE binaries served over TFTP and embeds them into the Go binary.
# Output: internal/bootfiles/assets/{undionly.kpxe,ipxe.efi}
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/third_party/ipxe"
OUT="$ROOT/internal/bootfiles/assets"
IPXE_REF="${IPXE_REF:-master}"

if [ ! -d "$SRC/.git" ]; then
  git clone --depth 1 --branch "$IPXE_REF" https://github.com/ipxe/ipxe.git "$SRC"
fi

cat > "$SRC/src/config/local/general.h" <<'H'
#define DOWNLOAD_PROTO_HTTPS
#define IMAGE_ZLIB
#define IMAGE_GZIP
#define NSLOOKUP_CMD
#define PING_CMD
#define REBOOT_CMD
#define POWEROFF_CMD
#define PARAM_CMD
#define IMAGE_TRUST_CMD
#define SLEEP_CMD
H

make -C "$SRC/src" -j"$(nproc)" bin/undionly.kpxe bin-x86_64-efi/ipxe.efi NO_WERROR=1 EMBED="$ROOT/ipxe/embed.ipxe"
mkdir -p "$OUT"
cp "$SRC/src/bin/undionly.kpxe" "$OUT/undionly.kpxe"
cp "$SRC/src/bin-x86_64-efi/ipxe.efi" "$OUT/ipxe.efi"
# Record the exact upstream source for GPL compliance.
echo "https://github.com/ipxe/ipxe/commit/$(git -C "$SRC" rev-parse HEAD)" > "$OUT/IPXE_VERSION"
ls -l "$OUT"
