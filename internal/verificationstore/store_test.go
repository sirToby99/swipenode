package verificationstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRetainsVersionsAndFindsLatestDependencies(t *testing.T) {
	store := Open(t.TempDir())
	old := testVerificationRecord("2026-09-01T10:00:00Z", "", "VERIFIED")
	storedOld, err := store.Append(old)
	if err != nil {
		t.Fatal(err)
	}
	child := testVerificationRecord("2026-09-01T11:00:00Z", storedOld.ID, "CONFLICT")
	storedChild, err := store.Append(child)
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.List()
	if err != nil || len(records) != 2 {
		t.Fatalf("history: %#v %v", records, err)
	}
	dependents, err := store.LatestDependents("pack", "docs")
	if err != nil || len(dependents) != 1 || dependents[0].ID != storedChild.ID {
		t.Fatalf("latest dependents: %#v %v", dependents, err)
	}
	if found, ok, err := store.Find(storedOld.ID); err != nil || !ok || found.ID != storedOld.ID {
		t.Fatalf("find old version: %#v %t %v", found, ok, err)
	}
}

func TestVerificationRecordRejectsIncompleteEvidence(t *testing.T) {
	record := testVerificationRecord("2026-09-01T10:00:00Z", "", "VERIFIED")
	record.Claims[0].Evidence[0].ContentSHA256 = "bad"
	if _, err := Open(t.TempDir()).Append(record); err == nil {
		t.Fatal("invalid evidence dependency was accepted")
	}
}

func TestCurrentV1RecordReadableAndUnknownSchemaRejected(t *testing.T) {
	store := Open(t.TempDir())
	stored, err := store.Append(testVerificationRecord("2026-09-01T10:00:00Z", "", "VERIFIED"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Find(stored.ID); err != nil || !ok {
		t.Fatalf("current v1 Verification record unreadable: %t %v", ok, err)
	}
	record := testVerificationRecord("2026-09-01T10:00:00Z", "", "VERIFIED")
	record.SchemaVersion = "swipenode.verification-record.v2"
	if _, err := store.Append(record); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown Verification schema accepted: %v", err)
	}
}

func TestVerificationRecordRejectsNonLocalArtifactsAndMismatchedFiles(t *testing.T) {
	store := Open(t.TempDir())
	record := testVerificationRecord("2026-09-01T10:00:00Z", "", "VERIFIED")
	record.Artifacts = []string{"../secret"}
	if _, err := store.Append(record); err == nil {
		t.Fatal("non-local artifact was accepted")
	}
	record = testVerificationRecord("2026-09-01T10:00:00Z", "", "VERIFIED")
	stored, err := store.Append(record)
	if err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(store.dir, stored.ID+".json")
	payload, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, "vrec_00000000000000000000000000000000.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("mismatched verification record filename was accepted")
	}
}

func testVerificationRecord(at, parent, status string) Record {
	hash := strings.Repeat("a", 64)
	return Record{RecordedAt: at, ReportID: "vr_report", ParentVerificationID: parent, PackID: "pack", Scope: "working_tree", Repository: Repository{Root: "/repo"}, Artifacts: []string{"config.yaml"}, Claims: []Claim{{ArtifactPath: "config.yaml", Line: 1, Category: "timeout_or_duration", Statement: "API timeout is 10 seconds.", Status: status, Evidence: []EvidenceReference{{EvidenceID: "ev_1", PackID: "pack", SourceID: "docs", CanonicalURL: "https://docs.example.com", SourceType: "official_documentation", Authority: "authoritative", Confidence: 0.95, ContentSHA256: hash}}}}}
}
