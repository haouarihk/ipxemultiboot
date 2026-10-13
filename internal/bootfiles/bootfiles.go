// Package bootfiles embeds the iPXE binaries handed out over TFTP.
// Build them with scripts/build-ipxe.sh.
package bootfiles

import (
	"embed"
	"io/fs"
)

//go:embed assets/*
var assets embed.FS

// FS contains undionly.kpxe (BIOS) and ipxe.efi (UEFI x86_64).
var FS, _ = fs.Sub(assets, "assets")

const (
	BIOS   = "undionly.kpxe"
	EFIx64 = "ipxe.efi"
)
