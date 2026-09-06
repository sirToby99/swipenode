package agentinstructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateIsIdempotentAndPreservesUserContent(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "AGENTS.md")
	original := "# User instructions\n\nNever overwrite this.\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := Update(repo, false)
	if err != nil || !first.Changed || first.Action != "appended" {
		t.Fatalf("first update: %#v err=%v", first, err)
	}
	afterFirst, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Update(repo, false)
	if err != nil || second.Changed || second.Action != "updated" {
		t.Fatalf("second update: %#v err=%v", second, err)
	}
	afterSecond, _ := os.ReadFile(path)
	if string(afterFirst) != string(afterSecond) || !strings.Contains(string(afterSecond), original[:len(original)-1]) {
		t.Fatal("idempotent update changed or lost user content")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0600 {
		t.Fatalf("mode changed to %v", info.Mode().Perm())
	}
}

func TestDryRunDoesNotWrite(t *testing.T) {
	repo := t.TempDir()
	result, err := Update(repo, true)
	if err != nil || !result.Changed || !strings.Contains(result.Content, BeginMarker) {
		t.Fatalf("dry run: %#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote AGENTS.md: %v", err)
	}
}

func TestMergeRejectsMalformedGeneratedSection(t *testing.T) {
	if _, _, err := Merge("user\n" + BeginMarker + "\nbroken\n"); err == nil {
		t.Fatal("expected malformed marker error")
	}
}

func TestMergeConsolidatesDuplicateGeneratedSections(t *testing.T) {
	previous := "before\n" + generatedSection + "\nbetween\n" + generatedSection + "\nafter\n"
	merged, action, err := Merge(previous)
	if err != nil || action != "updated" {
		t.Fatalf("merge action=%q err=%v", action, err)
	}
	if strings.Count(merged, BeginMarker) != 1 || !strings.Contains(merged, "before") || !strings.Contains(merged, "between") || !strings.Contains(merged, "after") {
		t.Fatalf("duplicates or user content mishandled:\n%s", merged)
	}
}
