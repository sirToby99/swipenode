package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirToby99/swipenode/internal/audit"
)

type packSnapshot struct {
	SchemaVersion string            `json:"schema_version"`
	Hashes        map[string]string `json:"hashes"`
}

// SyncRegistry records additions and changes relative to the last locally
// observed registry. It stores hashes only; pack definitions remain tracked in
// their built-in or project-local origin.
func SyncRegistry(registry *Registry, stateDir string) error {
	current := map[string]string{}
	for _, pack := range registry.List() {
		payload, err := json.Marshal(pack)
		if err != nil {
			return fmt.Errorf("encode knowledge pack %s: %w", pack.ID, err)
		}
		sum := sha256.Sum256(payload)
		current[pack.ID] = hex.EncodeToString(sum[:])
	}
	previous, err := readPackSnapshot(stateDir)
	if err != nil {
		return err
	}
	store := audit.Open(stateDir)
	for _, pack := range registry.List() {
		oldHash, exists := previous.Hashes[pack.ID]
		switch {
		case !exists:
			if _, err := store.Append(audit.Event{Type: audit.PackAdded, PackID: pack.ID, NewHash: current[pack.ID]}); err != nil {
				return err
			}
		case oldHash != current[pack.ID]:
			if _, err := store.Append(audit.Event{Type: audit.PackUpdated, PackID: pack.ID, OldHash: oldHash, NewHash: current[pack.ID]}); err != nil {
				return err
			}
		}
	}
	return writePackSnapshot(stateDir, packSnapshot{SchemaVersion: "swipenode.knowledge-snapshot.v1", Hashes: current})
}

func readPackSnapshot(stateDir string) (packSnapshot, error) {
	result := packSnapshot{Hashes: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(stateDir, "knowledge-packs.json"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read knowledge snapshot: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("parse knowledge snapshot: %w", err)
	}
	if result.Hashes == nil {
		result.Hashes = map[string]string{}
	}
	return result, nil
}

func writePackSnapshot(stateDir string, snapshot packSnapshot) error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create knowledge state directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode knowledge snapshot: %w", err)
	}
	temporary := filepath.Join(stateDir, "knowledge-packs.json.tmp")
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write knowledge snapshot: %w", err)
	}
	if err := os.Rename(temporary, filepath.Join(stateDir, "knowledge-packs.json")); err != nil {
		return fmt.Errorf("replace knowledge snapshot: %w", err)
	}
	return nil
}
