package iso9660_test

import (
	"io"
	"strings"
	"testing"

	"ipxemultiboot/internal/iso9660"
	"ipxemultiboot/internal/testiso"
)

var files = map[string]string{
	"boot/vmlinuz":                             "kernel-bytes",
	"boot/initramfs-linux.img":                 "initrd",
	"arch/x86_64/airootfs.sfs":                 strings.Repeat("x", 5000),
	"a/very/deep/directory/with-long-name.txt": "deep",
}

func TestRockRidge(t *testing.T) {
	img, err := iso9660.Open(testiso.Build(t, files, false))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if img.VolumeID() != "TESTVOL" {
		t.Errorf("volume id = %q", img.VolumeID())
	}
	for p, want := range files {
		b, err := img.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if string(b) != want {
			t.Errorf("%s: got %d bytes, want %d", p, len(b), len(want))
		}
	}
	entries, err := img.ReadDir("/boot")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "initramfs-linux.img" || entries[1].Name != "vmlinuz" {
		t.Errorf("unexpected /boot listing: %+v", entries)
	}
}

func TestSectionReaderRanges(t *testing.T) {
	img, err := iso9660.Open(testiso.Build(t, files, false))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	e, err := img.Stat("/arch/x86_64/airootfs.sfs")
	if err != nil {
		t.Fatal(err)
	}
	r := img.Reader(e)
	buf := make([]byte, 10)
	if _, err := r.ReadAt(buf, 4995); err != io.EOF && err != nil {
		t.Fatal(err)
	}
	if n, _ := r.ReadAt(buf, 4995); n != 5 {
		t.Errorf("read past end returned %d bytes, want 5", n)
	}
}

func TestPlainISO9660CaseInsensitive(t *testing.T) {
	img, err := iso9660.Open(testiso.Build(t, map[string]string{"BOOT/KERNEL": "k"}, true))
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if !img.Exists("/boot/kernel") {
		t.Error("case-insensitive lookup failed on a plain ISO9660 image")
	}
}

func TestNotISO(t *testing.T) {
	if _, err := iso9660.New(strings.NewReader(strings.Repeat("\x00", 64*2048))); err != iso9660.ErrNotISO {
		t.Errorf("err = %v, want ErrNotISO", err)
	}
}
