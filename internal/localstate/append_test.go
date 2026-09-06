package localstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAppendOnlyRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "audit.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if file, err := OpenAppendOnly(link, 0o600); err == nil {
		_ = file.Close()
		t.Fatal("append-only state followed a symbolic link")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("symlink target changed: %q %v", data, err)
	}
}
