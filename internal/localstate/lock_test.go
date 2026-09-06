package localstate

import (
	"strings"
	"testing"
)

func TestLockSerializesRefreshProcesses(t *testing.T) {
	dir := t.TempDir()
	first, err := Acquire(dir, "knowledge-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir, "knowledge-refresh"); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("concurrent lock was accepted: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(dir, "knowledge-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}
