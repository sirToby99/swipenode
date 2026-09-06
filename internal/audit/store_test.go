package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/verification"
)

func TestEventPersistenceRetrievalAndOrdering(t *testing.T) {
	store := Open(t.TempDir())
	ids := []string{"evt_one", "evt_two"}
	store.newID = func() (string, error) { id := ids[0]; ids = ids[1:]; return id, nil }
	times := []time.Time{time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC)}
	store.now = func() time.Time { value := times[0]; times = times[1:]; return value }
	if _, err := store.Append(Event{Type: PackAdded, PackID: "demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Event{Type: SourceChecked, PackID: "demo", SourceID: "docs"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID != "evt_two" || events[1].ID != "evt_one" {
		t.Fatalf("unexpected order: %#v", events)
	}
	event, ok, err := store.Find("evt_one")
	if err != nil || !ok || event.Type != PackAdded {
		t.Fatalf("find = %#v, %t, %v", event, ok, err)
	}
}

func TestV1FixtureReadableAndUnknownSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	fixture := `{"schema_version":"swipenode.audit-event.v1","id":"evt_compat","event":"source_checked","timestamp":"2026-01-01T00:00:00Z","pack_id":"pack","source_id":"docs"}` + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := Open(dir).List()
	if err != nil || len(events) != 1 || events[0].ID != "evt_compat" {
		t.Fatalf("current v1 Audit fixture unreadable: %#v %v", events, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(fixture, SchemaVersion, "swipenode.audit-event.v2", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir).List(); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("unknown Audit schema accepted: %v", err)
	}
}

func TestStoreRejectsSymlinkedAuditLog(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "audit.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := Open(dir)
	if _, err := store.Append(Event{Type: PackAdded, PackID: "demo"}); err == nil {
		t.Fatal("symlinked audit log accepted for append")
	}
	if _, err := store.List(); err == nil {
		t.Fatal("symlinked audit log accepted for read")
	}
}

func TestVerificationEventsAndStatusChange(t *testing.T) {
	store := Open(t.TempDir())
	report := verification.Report{SchemaVersion: verification.SchemaVersion, Scope: "working_tree", Claims: []verification.Claim{{Location: verification.Location{Path: "main.go", Line: 1}, Statement: "Example API returns 1", VerificationStatus: verification.StatusUnverified}}}
	if _, err := store.RecordVerification(report, "demo"); err != nil {
		t.Fatal(err)
	}
	report.Claims[0].VerificationStatus = verification.StatusVerified
	if _, err := store.RecordVerification(report, "demo"); err != nil {
		t.Fatal(err)
	}
	events, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[EventType]int{}
	for _, event := range events {
		counts[event.Type]++
	}
	if counts[VerificationCompleted] != 2 || counts[VerificationStatusChanged] != 1 {
		t.Fatalf("event counts: %#v", counts)
	}
}

func TestVerificationTraceReturnsResolvableEventIDs(t *testing.T) {
	store := Open(t.TempDir())
	report := verification.Report{SchemaVersion: verification.SchemaVersion, Scope: "working_tree", Claims: []verification.Claim{{Location: verification.Location{Path: "robot.urdf", Line: 2}, Statement: "Maximum temperature is 70 C.", VerificationStatus: verification.StatusVerified}}}
	trace, err := store.RecordVerificationWithEvents(report, "hardware")
	if err != nil {
		t.Fatal(err)
	}
	if trace.VerificationID == "" || len(trace.EventIDs) != 1 {
		t.Fatalf("unexpected trace: %#v", trace)
	}
	event, ok, err := store.Find(trace.EventIDs[0])
	if err != nil || !ok || event.VerificationID != trace.VerificationID || event.Type != VerificationCompleted {
		t.Fatalf("trace event is not resolvable: %#v %t %v", event, ok, err)
	}
}

func TestUnknownEventTypeIsRejected(t *testing.T) {
	if _, err := Open(t.TempDir()).Append(Event{Type: "arbitrary"}); err == nil {
		t.Fatal("unknown audit event type was accepted")
	}
}

func TestF02InterruptedAuditAppendPreservesLastKnownGoodBytes(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	stored, err := store.Append(Event{Type: VerificationCompleted, Timestamp: "2026-09-01T10:00:00Z", PackID: "pack"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "audit.jsonl")
	lastKnownGood, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"schema_version":"swipenode.audit-event.v1"`); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := store.List(); err == nil {
		t.Fatal("interrupted final Audit record was silently accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(after), string(lastKnownGood)) {
		t.Fatalf("last-known-good Audit bytes changed: %v", err)
	}
	if err := os.Truncate(path, int64(len(lastKnownGood))); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.List()
	if err != nil || len(recovered) != 1 || recovered[0].ID != stored.ID {
		t.Fatalf("last-known-good Audit did not recover: %#v %v", recovered, err)
	}
}
