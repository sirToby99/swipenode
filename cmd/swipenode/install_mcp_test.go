package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfigAtomicallyCreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")
	previous := []byte(`{"mcpServers":{"other":{"command":"other"}},"keep":true}`)
	if err := os.WriteFile(path, previous, 0600); err != nil {
		t.Fatal(err)
	}
	next := []byte(`{"mcpServers":{"other":{"command":"other"},"swipenode":{"command":"/bin/swipenode","args":["mcp"]}},"keep":true}` + "\n")
	backup, err := writeConfigAtomically(path, previous, next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(backup, path+".bak.") {
		t.Fatalf("backup = %q", backup)
	}
	gotBackup, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBackup) != string(previous) {
		t.Fatalf("backup was not preserved: %q", gotBackup)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(got, &config); err != nil {
		t.Fatal(err)
	}
	servers := config["mcpServers"].(map[string]any)
	if _, ok := servers["other"]; !ok || config["keep"] != true {
		t.Fatalf("existing configuration was lost: %#v", config)
	}
}
