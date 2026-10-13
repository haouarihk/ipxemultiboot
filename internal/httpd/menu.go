package httpd

import (
	"fmt"
	"net/url"
	"strings"

	"ipxemultiboot/internal/library"
)

// menuScript renders the iPXE boot menu for the given ISOs.
func menuScript(base string, isos []*library.ISO) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("#!ipxe")
	w("set menu-timeout 0")
	w("set base %s", base)
	w("")
	w(":menu")
	w("menu iPXE Multiboot - ${platform} ${buildarch} - ${net0/ip}")
	w("item --gap -- ---------------- ISO images ----------------")
	if len(isos) == 0 {
		w("item --gap -- (no ISO files found)")
	}
	for i, iso := range isos {
		label := iso.Name
		if iso.Recipe != nil {
			label += "  [" + iso.Recipe.Family + "]"
		} else {
			label += "  [sanboot]"
		}
		w("item iso%d %s", i, label)
	}
	w("item --gap -- ---------------- Options -------------------")
	w("item raw Raw sanboot (any ISO) >")
	w("item shell iPXE shell")
	w("item reboot Reboot")
	w("item exit Exit (boot next device)")
	w("choose --default iso0 target || goto menu")
	w("goto ${target}")
	w("")

	w(":raw")
	w("menu Raw sanboot - boots the ISO as a virtual CD (works until the OS loads its own drivers)")
	for i, iso := range isos {
		w("item raw%d %s", i, iso.Name)
	}
	w("item back < Back")
	w("choose target || goto menu")
	w("iseq ${target} back && goto menu ||")
	w("goto ${target}")
	w("")

	for i, iso := range isos {
		isoURL := base + "/isos/" + url.PathEscape(iso.ID)
		w(":iso%d", i)
		if r := iso.Recipe; r != nil {
			fileBase := base + "/iso/" + url.PathEscape(iso.ID)
			w("echo Booting %s (%s)", iso.Name, r.Family)
			// Drop leftover script images: on UEFI, iPXE passes every loaded
			// image to the kernel as part of initrd.magic.
			w("imgfree")
			w("kernel %s%s?mac=${netX/mac:hexhyp} initrd=initrd.magic %s || goto failed", fileBase, r.Kernel, r.Expand(isoURL, fileBase))
			for _, initrd := range r.Initrds {
				w("initrd %s%s || goto failed", fileBase, initrd)
			}
			w("boot || goto failed")
		} else {
			w("goto raw%d", i)
		}
		w("")
		w(":raw%d", i)
		w("echo Sanbooting %s", iso.Name)
		w("sanboot --no-describe --drive 0x80 %s?mac=${netX/mac:hexhyp} || goto failed", isoURL)
		w("")
	}

	w(":failed")
	w("echo Boot failed. Press any key to return to the menu.")
	w("prompt")
	w("goto menu")
	w(":shell")
	w("shell")
	w("goto menu")
	w(":reboot")
	w("reboot")
	w(":exit")
	w("exit")
	return b.String()
}
