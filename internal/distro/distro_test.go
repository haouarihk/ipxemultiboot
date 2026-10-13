package distro_test

import (
	"strings"
	"testing"

	"ipxemultiboot/internal/distro"
	"ipxemultiboot/internal/iso9660"
	"ipxemultiboot/internal/testiso"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		family string // "" means sanboot
		kernel string
	}{
		{"arch", map[string]string{
			"arch/boot/x86_64/vmlinuz-linux":       "k",
			"arch/boot/x86_64/initramfs-linux.img": "i",
			"arch/boot/intel-ucode.img":            "u",
		}, "archiso", "/arch/boot/x86_64/vmlinuz-linux"},
		{"cachyos-prefers-shortest-kernel", map[string]string{
			"arch/boot/x86_64/vmlinuz-linux-cachyos":           "k",
			"arch/boot/x86_64/initramfs-linux-cachyos.img":     "i",
			"arch/boot/x86_64/vmlinuz-linux-cachyos-lts":       "k",
			"arch/boot/x86_64/initramfs-linux-cachyos-lts.img": "i",
		}, "archiso", "/arch/boot/x86_64/vmlinuz-linux-cachyos"},
		{"ubuntu", map[string]string{"casper/vmlinuz": "k", "casper/initrd": "i"}, "casper", "/casper/vmlinuz"},
		{"mint", map[string]string{"casper/vmlinuz": "k", "casper/initrd.lz": "i"}, "casper", "/casper/vmlinuz"},
		{"debian-live", map[string]string{
			"live/vmlinuz-6.1.0-18-amd64": "k", "live/initrd.img-6.1.0-18-amd64": "i", "live/filesystem.squashfs": "s",
		}, "debian-live", "/live/vmlinuz-6.1.0-18-amd64"},
		{"fedora-live", map[string]string{
			"images/pxeboot/vmlinuz": "k", "images/pxeboot/initrd.img": "i", "LiveOS/squashfs.img": "s",
		}, "fedora-live", "/images/pxeboot/vmlinuz"},
		{"fedora-netinst", map[string]string{
			"images/pxeboot/vmlinuz": "k", "images/pxeboot/initrd.img": "i", "images/install.img": "s",
		}, "anaconda", "/images/pxeboot/vmlinuz"},
		{"alpine", map[string]string{
			"boot/vmlinuz-lts": "k", "boot/initramfs-lts": "i", "boot/modloop-lts": "m", "apks/.boot_repository": "",
		}, "alpine", "/boot/vmlinuz-lts"},
		{"tinycore", map[string]string{"boot/vmlinuz": "k", "boot/core.gz": "i"}, "tinycore", "/boot/vmlinuz"},
		{"debian-netinst", map[string]string{"install.amd/vmlinuz": "k", "install.amd/initrd.gz": "i"}, "debian-installer", "/install.amd/vmlinuz"},
		{"unknown", map[string]string{"README.TXT": "hello"}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img, err := iso9660.Open(testiso.Build(t, c.files, false))
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			r := distro.Detect(img)
			if c.family == "" {
				if r != nil {
					t.Fatalf("expected sanboot, got %+v", r)
				}
				return
			}
			if r == nil || r.Family != c.family {
				t.Fatalf("got %+v, want family %s", r, c.family)
			}
			if r.Kernel != c.kernel {
				t.Errorf("kernel = %s, want %s", r.Kernel, c.kernel)
			}
			for _, initrd := range r.Initrds {
				if !img.Exists(initrd) {
					t.Errorf("initrd %s does not exist in ISO", initrd)
				}
			}
		})
	}
}

func TestExpand(t *testing.T) {
	r := distro.Recipe{Args: "url={iso} fetch={base}/live/fs"}
	got := r.Expand("http://h/isos/x", "http://h/iso/x")
	if got != "url=http://h/isos/x fetch=http://h/iso/x/live/fs" {
		t.Errorf("Expand = %q", got)
	}
	if strings.Contains(got, "{") {
		t.Error("placeholder left unexpanded")
	}
}
