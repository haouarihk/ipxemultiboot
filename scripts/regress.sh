#!/usr/bin/env bash
# Boots a VM in every firmware x DHCP-mode combination and checks that it
# reaches the HTTP boot menu. Screenshots: $WORK/m-<fw>-<mode>.png
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="${WORK:-/tmp/ipxemultiboot-test}"
Q="$WORK/qmp.sock"
fail=0
for fw in bios uefi; do
  for m in full proxy bootstrap; do
    python3 "$ROOT/scripts/qmp.py" "$Q" quit 2>/dev/null; sleep 2
    "$ROOT/scripts/qemu-test.sh" "$fw" "$m" >/dev/null 2>&1 &
    ok=0
    for _ in $(seq 40); do
      sleep 1
      if grep -q 'path=/boot.ipxe status=200' "$WORK/server.log" 2>/dev/null; then ok=1; break; fi
    done
    sleep 2
    python3 "$ROOT/scripts/qmp.py" "$Q" shot "$WORK/m-$fw-$m.png" 2>/dev/null
    tftp=$(grep -o 'tftp: sent" file=[a-z.]*' "$WORK/server.log" | sed 's/.*file=//' | tr '\n' ' ')
    if [ $ok = 1 ]; then echo "PASS $fw/$m  tftp=[$tftp]"; else echo "FAIL $fw/$m  tftp=[$tftp]"; fail=1; fi
  done
done
python3 "$ROOT/scripts/qmp.py" "$Q" quit 2>/dev/null
exit $fail
