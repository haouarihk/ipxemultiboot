// Package distro detects what an ISO contains and how to network-boot it.
//
// A Recipe boots the ISO's own kernel/initrd (served straight out of the ISO
// over HTTP) with kernel parameters that tell the distro's initramfs to fetch
// its root filesystem over the network instead of from a CD. ISOs without a
// recipe fall back to iPXE sanboot.
package distro

import (
	"path"
	"sort"
	"strings"

	"ipxemultiboot/internal/iso9660"
)

// Recipe describes how to boot an ISO over HTTP.
type Recipe struct {
	Family string // e.g. "archiso", "casper", "alpine"
	// Kernel and Initrds are paths inside the ISO.
	Kernel  string
	Initrds []string
	// Args is the kernel command line. Placeholders:
	//   {iso}  – URL of the raw ISO file
	//   {base} – URL prefix for files inside the ISO (no trailing slash)
	// iPXE variables such as ${netX/mac} are expanded on the client.
	Args string
}

// Expand substitutes URL placeholders in the kernel arguments.
func (r Recipe) Expand(isoURL, baseURL string) string {
	return strings.NewReplacer("{iso}", isoURL, "{base}", baseURL).Replace(r.Args)
}

type detector func(img *iso9660.Image) (*Recipe, bool)

var detectors = []detector{
	archiso,
	casper,
	debianLive,
	fedoraLive,
	fedoraInstaller,
	alpine,
	tinycore,
	debianInstaller,
}

// Detect returns a boot recipe, or nil if the ISO should be sanbooted.
func Detect(img *iso9660.Image) *Recipe {
	for _, d := range detectors {
		if r, ok := d(img); ok {
			return r
		}
	}
	return nil
}

// glob returns names in dir matching a shell pattern (case-insensitive), sorted.
func glob(img *iso9660.Image, dir, pattern string) []string {
	entries, err := img.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if ok, _ := path.Match(strings.ToLower(pattern), strings.ToLower(e.Name)); ok {
			out = append(out, path.Join(dir, e.Name))
		}
	}
	sort.Strings(out)
	return out
}

func first(img *iso9660.Image, candidates ...string) string {
	for _, c := range candidates {
		if img.Exists(c) {
			return c
		}
	}
	return ""
}

// Arch Linux and derivatives (CachyOS, EndeavourOS, ...). The archiso_pxe_http
// initramfs hook downloads airootfs.sfs into RAM.
func archiso(img *iso9660.Image) (*Recipe, bool) {
	const dir = "/arch/boot/x86_64"
	kernels := glob(img, dir, "vmlinuz-*")
	if len(kernels) == 0 {
		return nil, false
	}
	// Prefer the shortest name: "vmlinuz-linux" over "vmlinuz-linux-lts".
	sort.Slice(kernels, func(i, j int) bool { return len(kernels[i]) < len(kernels[j]) })
	kernel := kernels[0]
	flavor := strings.TrimPrefix(path.Base(kernel), "vmlinuz-")
	initrd := path.Join(dir, "initramfs-"+flavor+".img")
	if !img.Exists(initrd) {
		return nil, false
	}
	var initrds []string
	for _, uc := range []string{"/arch/boot/intel-ucode.img", "/arch/boot/amd-ucode.img"} {
		if img.Exists(uc) {
			initrds = append(initrds, uc)
		}
	}
	initrds = append(initrds, initrd)
	return &Recipe{
		Family:  "archiso",
		Kernel:  kernel,
		Initrds: initrds,
		Args:    "archisobasedir=arch archiso_http_srv={base}/ ip=dhcp net.ifnames=0 BOOTIF=01-${netX/mac:hexhyp} copytoram=y",
	}, true
}

// Ubuntu and derivatives (Mint, Pop!_OS, ...). Casper downloads the whole ISO
// into RAM, so the client needs more memory than the ISO size.
func casper(img *iso9660.Image) (*Recipe, bool) {
	kernel := first(img, "/casper/vmlinuz", "/casper/vmlinuz.efi")
	initrd := first(img, "/casper/initrd", "/casper/initrd.lz", "/casper/initrd.gz", "/casper/initrd.img")
	if kernel == "" || initrd == "" {
		return nil, false
	}
	return &Recipe{
		Family:  "casper",
		Kernel:  kernel,
		Initrds: []string{initrd},
		Args:    "boot=casper netboot=url url={iso} ip=dhcp",
	}, true
}

// Debian live and other live-boot based images.
func debianLive(img *iso9660.Image) (*Recipe, bool) {
	kernels := glob(img, "/live", "vmlinuz*")
	initrds := glob(img, "/live", "initrd*")
	squash := first(img, "/live/filesystem.squashfs")
	if len(kernels) == 0 || len(initrds) == 0 || squash == "" {
		return nil, false
	}
	return &Recipe{
		Family:  "debian-live",
		Kernel:  kernels[0],
		Initrds: initrds[:1],
		Args:    "boot=live components fetch={base}" + squash,
	}, true
}

// Fedora / RHEL-family live images (dracut dmsquash-live).
func fedoraLive(img *iso9660.Image) (*Recipe, bool) {
	kernel := first(img, "/images/pxeboot/vmlinuz", "/isolinux/vmlinuz", "/boot/x86_64/loader/linux")
	initrd := first(img, "/images/pxeboot/initrd.img", "/isolinux/initrd.img")
	squash := first(img, "/LiveOS/squashfs.img")
	if kernel == "" || initrd == "" || squash == "" {
		return nil, false
	}
	return &Recipe{
		Family:  "fedora-live",
		Kernel:  kernel,
		Initrds: []string{initrd},
		Args:    "root=live:{base}" + squash + " rd.live.image rd.neednet=1 ip=dhcp",
	}, true
}

// Fedora / RHEL-family installers (Anaconda). The ISO tree is the repo.
func fedoraInstaller(img *iso9660.Image) (*Recipe, bool) {
	kernel := first(img, "/images/pxeboot/vmlinuz")
	initrd := first(img, "/images/pxeboot/initrd.img")
	if kernel == "" || initrd == "" || !img.Exists("/images/install.img") {
		return nil, false
	}
	return &Recipe{
		Family:  "anaconda",
		Kernel:  kernel,
		Initrds: []string{initrd},
		Args:    "inst.repo={base}/ inst.stage2={base}/ ip=dhcp rd.neednet=1",
	}, true
}

// Alpine Linux: kernel modules come from modloop, packages from the ISO's apks.
func alpine(img *iso9660.Image) (*Recipe, bool) {
	if !img.Exists("/apks") {
		return nil, false
	}
	kernels := glob(img, "/boot", "vmlinuz-*")
	if len(kernels) == 0 {
		return nil, false
	}
	flavor := strings.TrimPrefix(path.Base(kernels[0]), "vmlinuz-")
	initrd := "/boot/initramfs-" + flavor
	modloop := "/boot/modloop-" + flavor
	if !img.Exists(initrd) || !img.Exists(modloop) {
		return nil, false
	}
	return &Recipe{
		Family:  "alpine",
		Kernel:  kernels[0],
		Initrds: []string{initrd},
		Args:    "ip=dhcp modules=loop,squashfs,sd-mod,usb-storage modloop={base}" + modloop + " alpine_repo={base}/apks",
	}, true
}

// Tiny Core Linux runs entirely from its initrd.
func tinycore(img *iso9660.Image) (*Recipe, bool) {
	for _, pair := range [][2]string{
		{"/boot/vmlinuz64", "/boot/corepure64.gz"},
		{"/boot/vmlinuz", "/boot/core.gz"},
	} {
		if img.Exists(pair[0]) && img.Exists(pair[1]) {
			return &Recipe{
				Family:  "tinycore",
				Kernel:  pair[0],
				Initrds: []string{pair[1]},
				Args:    "loglevel=3",
			}, true
		}
	}
	return nil, false
}

// Debian / Ubuntu legacy text installer (netinst). Packages come from a mirror.
func debianInstaller(img *iso9660.Image) (*Recipe, bool) {
	kernel := first(img, "/install.amd/vmlinuz", "/install/vmlinuz")
	initrd := first(img, "/install.amd/initrd.gz", "/install/initrd.gz")
	if kernel == "" || initrd == "" {
		return nil, false
	}
	return &Recipe{
		Family:  "debian-installer",
		Kernel:  kernel,
		Initrds: []string{initrd},
		Args:    "vga=788 ---",
	}, true
}
