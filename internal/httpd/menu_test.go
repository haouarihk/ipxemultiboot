package httpd

import (
	"strings"
	"testing"

	"ipxemultiboot/internal/distro"
	"ipxemultiboot/internal/library"
)

func TestMenuScript(t *testing.T) {
	isos := []*library.ISO{
		{ID: "arch", Name: "arch.iso", Recipe: &distro.Recipe{Family: "archiso", Kernel: "/arch/vmlinuz", Initrds: []string{"/arch/initrd"}, Args: "srv={base}/"}},
		{ID: "tool", Name: "tool.iso"},
	}
	s := menuScript("http://10.0.0.1:8080", isos)
	for _, want := range []string{
		"#!ipxe",
		"item iso0 arch.iso  [archiso]",
		"item iso1 tool.iso  [sanboot]",
		"kernel http://10.0.0.1:8080/iso/arch/arch/vmlinuz?mac=${netX/mac:hexhyp} initrd=initrd.magic srv=http://10.0.0.1:8080/iso/arch/",
		"initrd http://10.0.0.1:8080/iso/arch/arch/initrd",
		"sanboot --no-describe --drive 0x80 http://10.0.0.1:8080/isos/tool",
		":iso1\ngoto raw1",
		"imgfree\nkernel",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("menu missing %q\n%s", want, s)
		}
	}
}

func TestMenuEmpty(t *testing.T) {
	if s := menuScript("http://h", nil); !strings.Contains(s, "no ISO files found") {
		t.Error("empty menu should say no ISOs were found")
	}
}

func TestNormMAC(t *testing.T) {
	if got := normMAC("52-54-00-AB-cd-EF"); got != "52:54:00:ab:cd:ef" {
		t.Errorf("normMAC = %q", got)
	}
	if got := normMAC("${netX/mac}"); got != "" {
		t.Errorf("garbage should normalize to empty, got %q", got)
	}
}
