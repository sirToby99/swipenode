package provenance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRetainsImmutableRecordsAndOrdersNewestFirst(t *testing.T) {
	store := Open(t.TempDir())
	older := testRecord("2026-09-01T10:00:00Z", "first.yaml")
	newer := testRecord("2026-09-01T11:00:00Z", "robot.urdf")
	storedOlder, err := store.Append(older)
	if err != nil {
		t.Fatal(err)
	}
	storedNewer, err := store.Append(newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(storedOlder); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected immutable duplicate rejection, got %v", err)
	}
	records, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ProvenanceID != storedNewer.ProvenanceID || records[1].ProvenanceID != storedOlder.ProvenanceID {
		t.Fatalf("unexpected order: %#v", records)
	}
	found, ok, err := store.Find(storedOlder.ProvenanceID)
	if err != nil || !ok || found.Artifacts[0].Path != "first.yaml" {
		t.Fatalf("find failed: %#v %t %v", found, ok, err)
	}
}

func TestCurrentV1RecordReadableAndUnknownSchemaRejected(t *testing.T) {
	store := Open(t.TempDir())
	stored, err := store.Append(testRecord("2026-09-01T10:00:00Z", "robot.urdf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Find(stored.ProvenanceID); err != nil || !ok {
		t.Fatalf("current v1 Provenance record unreadable: %t %v", ok, err)
	}
	record := testRecord("2026-09-01T10:00:00Z", "robot.urdf")
	record.SchemaVersion = "swipenode.engineering-provenance.v2"
	if _, err := store.Append(record); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown Provenance schema accepted: %v", err)
	}
}

func TestRecordRejectsTamperingSecretsAndUnsafeArtifacts(t *testing.T) {
	record := testRecord("2026-09-01T10:00:00Z", "config/bom.yaml")
	if err := record.Prepare(); err != nil {
		t.Fatal(err)
	}
	record.Repository.GitBranch = "changed"
	if err := record.Prepare(); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered content retained a stable id: %v", err)
	}
	secret := testRecord("2026-09-01T10:00:00Z", "config.yaml")
	secret.Claims[0].Statement = "api_key=do-not-store"
	if err := secret.Prepare(); err == nil || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("secret was accepted: %v", err)
	}
	unsafe := testRecord("2026-09-01T10:00:00Z", "../outside")
	if err := unsafe.Prepare(); err == nil || !strings.Contains(err.Error(), "repository-local") {
		t.Fatalf("unsafe artifact was accepted: %v", err)
	}
}

func TestStoreRejectsSymlinkRecord(t *testing.T) {
	state := t.TempDir()
	store := Open(state)
	dir := filepath.Join(state, "provenance", "records")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := "prov_0123456789abcdef0123456789abcdef"
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(dir, id+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestTruthTransitionRequiresLinkedVersionsAndEvidence(t *testing.T) {
	record := testRecord("2026-09-01T10:00:00Z", "config.yaml")
	record.EvidenceIDs = append(record.EvidenceIDs, "ev_new")
	record.SourceRevisions = append(record.SourceRevisions, SourceRevision{EvidenceID: "ev_new", SourceID: "docs", ContentSHA256: strings.Repeat("b", 64)})
	record.TruthTransitions = []TruthTransition{{ClaimID: "claim_one", ArtifactPath: "config.yaml", OldVerificationID: "vrec_old", NewVerificationID: "vrec_new", OldEvidenceID: "ev_test", NewEvidenceID: "ev_new", OldStatus: "VERIFIED", NewStatus: "CONFLICT", OldHash: strings.Repeat("a", 64), NewHash: strings.Repeat("b", 64)}}
	if err := record.Prepare(); err != nil {
		t.Fatalf("valid truth transition: %v", err)
	}
	record.ProvenanceID = ""
	record.TruthTransitions[0].NewEvidenceID = "ev_missing"
	if err := record.Prepare(); err == nil || !strings.Contains(err.Error(), "unknown evidence") {
		t.Fatalf("unlinked transition accepted: %v", err)
	}
}

func TestArtifactsRemainGenericAcrossEngineeringFileTypes(t *testing.T) {
	record := testRecord("2026-09-01T10:00:00Z", "src/controller.go")
	record.Artifacts = []Artifact{
		{Path: "src/controller.go", Kind: "file"},
		{Path: "robot/model.urdf", Kind: "file"},
		{Path: "config/runtime.yaml", Kind: "file"},
		{Path: "hardware/bom.csv", Kind: "file"},
		{Path: "requirements/system.req", Kind: "file"},
	}
	if err := record.Prepare(); err != nil {
		t.Fatal(err)
	}
	if len(record.Artifacts) != 5 {
		t.Fatalf("engineering artifacts were narrowed by file type: %#v", record.Artifacts)
	}
}

func TestManagedPackReferenceRequiresExactSignedReleaseIdentity(t *testing.T) {
	record := testRecord("2026-09-01T10:00:00Z", "config.yaml")
	record.KnowledgePacks = []KnowledgePackReference{{ID: "test-pack", Origin: "managed", Version: "2026.09.1", Publisher: "SwipeNode", PublisherKeyID: "ed25519:test", PackageSHA256: strings.Repeat("c", 64)}}
	if err := record.Prepare(); err != nil {
		t.Fatal(err)
	}
	record.ProvenanceID = ""
	record.KnowledgePacks[0].Version = ""
	if err := record.Prepare(); err == nil || !strings.Contains(err.Error(), "requires version") {
		t.Fatalf("incomplete managed release accepted: %v", err)
	}
}

func testRecord(timestamp, artifact string) EngineeringProvenanceRecord {
	hash := strings.Repeat("a", 64)
	return EngineeringProvenanceRecord{
		Timestamp:    timestamp,
		Repository:   RepositoryContext{Root: "/repo", GitCommit: strings.Repeat("b", 40), GitBranch: "main"},
		Artifacts:    []Artifact{{Path: artifact, Kind: "file"}},
		Verification: VerificationReference{ID: "vr_test", SchemaVersion: "swipenode.verify.v1", Scope: "working_tree", Statuses: map[string]int{"VERIFIED": 1}},
		EvidenceIDs:  []string{"ev_test"}, AuditEventIDs: []string{"evt_test"}, KnowledgePackIDs: []string{"test-pack"},
		Claims:          []ClaimReference{{ArtifactPath: artifact, Line: 1, Statement: "Technical value is 10 seconds.", Status: "VERIFIED", EvidenceIDs: []string{"ev_test"}}},
		SourceRevisions: []SourceRevision{{EvidenceID: "ev_test", SourceID: "docs", ContentSHA256: hash}},
	}
}
