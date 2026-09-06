package verifycmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/provenance"
)

func TestRecordProvenanceWorksWithoutEntire(t *testing.T) {
	root := newRepository(t)
	stateDir := t.TempDir()
	writeOfflineEvidence(t, root)
	writeClaim(t, root, "// Example API timeout is 10 seconds.\npackage demo\n")
	deps := DefaultDependencies()
	deps.Getwd = func() (string, error) { return root, nil }
	deps.ResolveRoot = func(string) (string, error) { return root, nil }
	deps.StateDir = func(context.Context, string) (string, error) { return stateDir, nil }

	report := executeVerify(t, deps, "--record-provenance", "--json")
	if report.ProvenanceID == "" {
		t.Fatal("verification did not return a provenance id")
	}
	record, ok, err := provenance.Open(stateDir).Find(report.ProvenanceID)
	if err != nil || !ok {
		t.Fatalf("find provenance: %t %v", ok, err)
	}
	if record.Adapter != "swipenode" || record.ExternalContext != nil || len(record.Artifacts) != 1 || record.Artifacts[0].Path != "demo.go" {
		t.Fatalf("unexpected standalone record: %#v", record)
	}
	if len(record.EvidenceIDs) == 0 || len(record.AuditEventIDs) < 2 || len(record.SourceRevisions) == 0 || record.Repository.GitCommit == "" || record.Repository.GitBranch == "" {
		t.Fatalf("record is missing references or git context: %#v", record)
	}
	for _, id := range record.EvidenceIDs {
		if _, ok, err := evidencestore.Open(stateDir).Find(id); err != nil || !ok {
			t.Fatalf("evidence reference %s is not resolvable: %t %v", id, ok, err)
		}
	}
	for _, id := range record.AuditEventIDs {
		if _, ok, err := audit.Open(stateDir).Find(id); err != nil || !ok {
			t.Fatalf("audit reference %s is not resolvable: %t %v", id, ok, err)
		}
	}
}

func TestProvenanceIsExplicitAndSupportsHardwareArtifacts(t *testing.T) {
	root := newRepository(t)
	stateDir := t.TempDir()
	writeOfflineEvidence(t, root)
	robot := filepath.Join(root, "robot.urdf")
	if err := os.WriteFile(robot, []byte("<robot name=\"test\"></robot>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "robot.urdf", ".swipenode/evidence.json")
	runGit(t, root, "commit", "-qm", "add engineering artifact")
	if err := os.WriteFile(robot, []byte("<robot name=\"test\"><!-- Example API timeout is 10 seconds. --></robot>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := DefaultDependencies()
	deps.Getwd = func() (string, error) { return root, nil }
	deps.ResolveRoot = func(string) (string, error) { return root, nil }
	deps.StateDir = func(context.Context, string) (string, error) { return stateDir, nil }

	without := executeVerify(t, deps, "--json")
	if without.ProvenanceID != "" {
		t.Fatalf("default behavior unexpectedly recorded provenance: %#v", without)
	}
	with := executeVerify(t, deps, "--record-provenance", "--json")
	record, ok, err := provenance.Open(stateDir).Find(with.ProvenanceID)
	if err != nil || !ok || len(record.Artifacts) != 1 || record.Artifacts[0].Path != "robot.urdf" || record.Artifacts[0].Kind != "file" {
		t.Fatalf("hardware-style artifact not preserved: %#v %t %v", record, ok, err)
	}
}

func TestProvenanceUsesOptionalEnricherWithoutChangingVerification(t *testing.T) {
	root := newRepository(t)
	stateDir := t.TempDir()
	writeOfflineEvidence(t, root)
	writeClaim(t, root, "// Example API timeout is 10 seconds.\npackage demo\n")
	deps := DefaultDependencies()
	deps.Getwd = func() (string, error) { return root, nil }
	deps.ResolveRoot = func(string) (string, error) { return root, nil }
	deps.StateDir = func(context.Context, string) (string, error) { return stateDir, nil }
	deps.ProvenanceEnricher = testEnricher{}
	report := executeVerify(t, deps, "--record-provenance", "--json")
	record, ok, err := provenance.Open(stateDir).Find(report.ProvenanceID)
	if err != nil || !ok || record.Adapter != "entire" || record.ExternalContext.Entire.SessionID != "session-test" {
		t.Fatalf("optional enrichment missing: %#v %t %v", record, ok, err)
	}
	if len(report.Claims) != 1 || string(report.Claims[0].VerificationStatus) != "VERIFIED" {
		t.Fatalf("enrichment changed verification semantics: %#v", report)
	}
}

func TestProvenanceCarriesResolvedPackReleaseWithoutEvidencePayload(t *testing.T) {
	root := newRepository(t)
	stateDir := t.TempDir()
	writePack(t, root)
	writeOfflineEvidence(t, root)
	writeClaim(t, root, "// Example API timeout is 10 seconds.\npackage demo\n")
	deps := DefaultDependencies()
	deps.Getwd = func() (string, error) { return root, nil }
	deps.ResolveRoot = func(string) (string, error) { return root, nil }
	deps.StateDir = func(context.Context, string) (string, error) { return stateDir, nil }
	deps.ResolvePackReference = func(_ string, pack knowledge.Pack) (provenance.KnowledgePackReference, error) {
		return provenance.KnowledgePackReference{ID: pack.ID, Origin: "managed", Version: "2026.09.7", Publisher: "SwipeNode", PublisherKeyID: "ed25519:test", PackageSHA256: strings.Repeat("d", 64)}, nil
	}
	report := executeVerify(t, deps, "--knowledge-pack", "team-pack", "--record-provenance", "--json")
	record, ok, err := provenance.Open(stateDir).Find(report.ProvenanceID)
	if err != nil || !ok || len(record.KnowledgePacks) != 1 {
		t.Fatalf("pack release provenance missing: %#v %t %v", record, ok, err)
	}
	pack := record.KnowledgePacks[0]
	if pack.Version != "2026.09.7" || pack.Publisher != "SwipeNode" || pack.PublisherKeyID != "ed25519:test" {
		t.Fatalf("pack release provenance incomplete: %#v", pack)
	}
	encoded, _ := json.Marshal(record)
	if strings.Contains(string(encoded), "The Example API timeout") {
		t.Fatal("evidence payload was duplicated into provenance")
	}
}

type testEnricher struct{}

func (testEnricher) Enrich(context.Context, string) provenance.Enrichment {
	return provenance.Enrichment{Adapter: "entire", ExternalContext: &provenance.ExternalContext{Entire: &provenance.EntireContext{ContextStatus: "available", SessionID: "session-test"}}}
}

func writeOfflineEvidence(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".swipenode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := map[string]any{
		"schema_version": "swipenode.evidence.v1",
		"evidence":       []map[string]any{{"source": "https://docs.github.com/example", "source_type": "official_documentation", "authority": "authoritative", "version_date": "2026-09-01", "confidence": 0.98, "content": "The Example API timeout is 10 seconds."}},
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evidence.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProvenanceDoesNotPersistCredentialAssignments(t *testing.T) {
	record := provenance.EngineeringProvenanceRecord{Timestamp: "2026-09-01T00:00:00Z", Repository: provenance.RepositoryContext{Root: "/repo"}, Verification: provenance.VerificationReference{ID: "vr_x"}, Claims: []provenance.ClaimReference{{Statement: strings.Join([]string{"password", "unsafe"}, "=")}}}
	if err := record.Prepare(); err == nil {
		t.Fatal("credential assignment accepted")
	}
}
