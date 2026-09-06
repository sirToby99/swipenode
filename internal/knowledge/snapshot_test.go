package knowledge

import (
	"testing"

	"github.com/sirToby99/swipenode/internal/audit"
)

func TestRegistrySyncRecordsPackAddedAndUpdated(t *testing.T) {
	dir := t.TempDir()
	registry := &Registry{packs: map[string]Pack{"demo": {SchemaVersion: SchemaVersion, ID: "demo", Owner: "Example", Description: "one", Origin: OriginProject}}}
	if err := SyncRegistry(registry, dir); err != nil {
		t.Fatal(err)
	}
	if err := SyncRegistry(registry, dir); err != nil {
		t.Fatal(err)
	}
	pack := registry.packs["demo"]
	pack.Description = "two"
	registry.packs["demo"] = pack
	if err := SyncRegistry(registry, dir); err != nil {
		t.Fatal(err)
	}
	events, err := audit.Open(dir).List()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[audit.EventType]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if counts[audit.PackAdded] != 1 || counts[audit.PackUpdated] != 1 {
		t.Fatalf("unexpected pack events: %#v", counts)
	}
}
