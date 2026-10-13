// Package testiso builds small ISO images for tests using xorriso.
package testiso

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Build creates an ISO from files (path → content). Rock Ridge and Joliet are
// enabled unless plain is set. The test is skipped when xorriso is missing.
func Build(t testing.TB, files map[string]string, plain bool) string {
	t.Helper()
	if _, err := exec.LookPath("xorriso"); err != nil {
		t.Skip("xorriso not installed")
	}
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(dir, "test.iso")
	args := []string{"-as", "mkisofs", "-quiet", "-V", "TESTVOL", "-o", out}
	if !plain {
		args = append(args, "-R", "-J")
	}
	args = append(args, root)
	if b, err := exec.Command("xorriso", args...).CombinedOutput(); err != nil {
		t.Fatalf("xorriso: %v\n%s", err, b)
	}
	return out
}
