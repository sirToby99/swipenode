package evidencestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInsertRetainsVersionsAndLookup(t *testing.T) {
	store := Open(t.TempDir())
	first := testRecord("first body", "2026-09-01T10:00:00Z")
	inserted, err := store.Insert(first)
	if err != nil {
		t.Fatal(err)
	}
	second := testRecord("second body", "2026-09-01T11:00:00Z")
	insertedSecond, err := store.Insert(second)
	if err != nil {
		t.Fatal(err)
	}
	if inserted.ID == insertedSecond.ID || inserted.ContentSHA256 == insertedSecond.ContentSHA256 {
		t.Fatal("content versions must have distinct hashes and IDs")
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("record count = %d", len(records))
	}
	found, ok, err := store.Find(inserted.ID)
	if err != nil || !ok || found.ContentSHA256 != first.ContentSHA256 {
		t.Fatalf("lookup failed: %#v, %t, %v", found, ok, err)
	}
	if _, err := store.Insert(first); err == nil {
		t.Fatal("duplicate evidence silently overwrote history")
	}
}

func TestV1FixtureReadableAndUnknownSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.jsonl")
	fixture := `{"schema_version":"swipenode.evidence-record.v1","id":"ev_compat","source_id":"docs","canonical_url":"https://docs.example.com/v1","owner":"Example","source_type":"official_documentation","retrieved_at":"2026-01-01T00:00:00Z","content_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","provenance":{"canonical_url":"https://docs.example.com/v1","owner":"Example","source_type":"official_documentation"}}` + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := Open(dir).List()
	if err != nil || len(records) != 1 || records[0].ID != "ev_compat" {
		t.Fatalf("current v1 Evidence fixture unreadable: %#v %v", records, err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(fixture, SchemaVersion, "swipenode.evidence-record.v2", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir).List(); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("unknown Evidence schema accepted: %v", err)
	}
}

func TestSourceStateUpdateDoesNotChangeEvidenceHistory(t *testing.T) {
	store := Open(t.TempDir())
	record, err := store.Insert(testRecord("stable", "2026-09-01T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	state := SourceState{PackID: "pack", SourceID: "docs", CanonicalURL: "https://docs.example.com/reference", LastChecked: "2026-09-01T10:00:00Z", CurrentEvidenceID: record.ID, ContentSHA256: record.ContentSHA256}
	if err := store.UpdateSourceState(state); err != nil {
		t.Fatal(err)
	}
	state.LastChecked = "2026-09-01T12:00:00Z"
	if err := store.UpdateSourceState(state); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.SourceState(state.PackID, state.SourceID, state.CanonicalURL)
	if err != nil || !ok || got.LastChecked != state.LastChecked {
		t.Fatalf("state lookup = %#v, %t, %v", got, ok, err)
	}
	records, _ := store.List()
	if len(records) != 1 {
		t.Fatalf("freshness update changed evidence history: %d", len(records))
	}
}

func TestInvalidRecordIsRejected(t *testing.T) {
	record := testRecord("stable", "not-a-time")
	if _, err := Open(t.TempDir()).Insert(record); err == nil {
		t.Fatal("invalid retrieval time was accepted")
	}
	record = testRecord("stable", "2026-09-01T10:00:00Z")
	record.ContentSHA256 = "not-a-hash"
	if _, err := Open(t.TempDir()).Insert(record); err == nil {
		t.Fatal("invalid content hash was accepted")
	}
}

func TestSourceStateRejectsUnsafeOrInconsistentFiles(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(dir, "sources.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SourceStates(); err == nil {
		t.Fatal("symlink source state was accepted")
	}
	if err := os.Remove(filepath.Join(dir, "sources.json")); err != nil {
		t.Fatal(err)
	}
	invalid := `[{"source_id":"docs","canonical_url":"https://docs.example.com","last_checked":"2026-09-01T10:00:00Z","content_sha256":"` + ContentHash("body") + `"}]`
	if err := os.WriteFile(filepath.Join(dir, "sources.json"), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SourceStates(); err == nil {
		t.Fatal("source state without evidence relationship was accepted")
	}
}

func TestStoreRejectsSymlinkedEvidenceLog(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "evidence.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := Open(dir)
	if _, err := store.Insert(testRecord("stable", "2026-09-01T10:00:00Z")); err == nil {
		t.Fatal("symlinked evidence log accepted for append")
	}
	if _, err := store.List(); err == nil {
		t.Fatal("symlinked evidence log accepted for read")
	}
}

func TestF01InterruptedEvidenceAppendPreservesLastKnownGoodBytes(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	stored, err := store.Insert(testRecord("fact-v1", "2026-09-01T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evidence.jsonl")
	lastKnownGood, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"schema_version":"swipenode.evidence-record.v1"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("interrupted final Evidence record was silently accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(after), string(lastKnownGood)) {
		t.Fatalf("last-known-good Evidence bytes changed: %v", err)
	}
	if err := os.Truncate(path, int64(len(lastKnownGood))); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.List()
	if err != nil || len(recovered) != 1 || recovered[0].ID != stored.ID {
		t.Fatalf("last-known-good Evidence did not recover: %#v %v", recovered, err)
	}
}

func TestF03InterruptedSourceStateTemporaryFileIsIgnored(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	record, err := store.Insert(testRecord("fact-v1", "2026-09-01T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	state := SourceState{PackID: "pack", SourceID: "docs", CanonicalURL: "https://docs.example.com/reference", LastChecked: "2026-09-01T10:00:00Z", CurrentEvidenceID: record.ID, ContentSHA256: record.ContentSHA256, Health: "healthy", Status: "unchanged"}
	if err := store.UpdateSourceState(state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sources-interrupted.tmp"), []byte(`{"partial":`), 0o600); err != nil {
		t.Fatal(err)
	}
	states, err := store.SourceStates()
	if err != nil || len(states) != 1 || states[0].CurrentEvidenceID != record.ID {
		t.Fatalf("temporary interrupted update affected last-known-good state: %#v %v", states, err)
	}
}

func TestF05MalformedCompleteEvidenceTailFailsClosed(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	if _, err := store.Insert(testRecord("fact-v1", "2026-09-01T10:00:00Z")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evidence.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{malformed}\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := store.List(); err == nil {
		t.Fatal("malformed complete final JSONL record was silently ignored")
	}
}

func TestF06CorruptSourceStateFailsWithoutChangingEvidence(t *testing.T) {
	dir := t.TempDir()
	store := Open(dir)
	record, err := store.Insert(testRecord("fact-v1", "2026-09-01T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	evidenceBefore, err := os.ReadFile(filepath.Join(dir, "evidence.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sources.json"), []byte(`{"corrupt":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SourceStates(); err == nil {
		t.Fatal("corrupt source state was accepted")
	}
	evidenceAfter, err := os.ReadFile(filepath.Join(dir, "evidence.jsonl"))
	if err != nil || string(evidenceAfter) != string(evidenceBefore) {
		t.Fatalf("source-state corruption changed Evidence %s: %v", record.ID, err)
	}
}

func TestF07UnavailableStateDirectoryFailsWithoutFallbackWrite(t *testing.T) {
	store := Open("/proc/swipenode-failure-fixture")
	if _, err := store.Insert(testRecord("fact-v1", "2026-09-01T10:00:00Z")); err == nil {
		t.Fatal("unavailable state directory accepted an Evidence write")
	}
}

func testRecord(content, retrievedAt string) Record {
	return Record{PackID: "pack", SourceID: "docs", CanonicalURL: "https://docs.example.com/reference", Owner: "Example", SourceType: "official_documentation", RetrievedAt: retrievedAt, ContentSHA256: ContentHash(content), ExtractedFact: "A local fact", Provenance: Provenance{CanonicalURL: "https://docs.example.com/reference", Owner: "Example", SourceType: "official_documentation"}}
}
