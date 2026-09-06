package controlplane

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/verificationstore"
)

func TestServiceUsesSharedStoresAndRedactsDisplayData(t *testing.T) {
	service, fixture := seededService(t)
	packs, err := service.Packs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var private *PackView
	for index := range packs.Packs {
		if packs.Packs[index].ID == "customer-robot" {
			private = &packs.Packs[index]
		}
	}
	if private == nil || private.Category != "private" || private.Origin != "project" {
		t.Fatalf("project Pack not distinguished: %#v", private)
	}
	if private.Sources[0].CanonicalURL != "https://docs.example.com/robot" {
		t.Fatalf("unexpected source policy: %#v", private.Sources[0])
	}

	evidence, err := service.Evidence()
	if err != nil || len(evidence.Evidence) != 1 {
		t.Fatalf("evidence: %#v %v", evidence, err)
	}
	gotEvidence := evidence.Evidence[0]
	if gotEvidence.ID != fixture.evidenceID || !gotEvidence.Redacted || strings.Contains(gotEvidence.ExtractedFact, "hunter2") {
		t.Fatalf("evidence was not safely projected: %#v", gotEvidence)
	}
	if gotEvidence.CanonicalURL != "https://docs.example.com/robot" {
		t.Fatalf("URL parameters exposed: %q", gotEvidence.CanonicalURL)
	}

	verifications, err := service.Verifications()
	if err != nil || len(verifications.Records) != 1 {
		t.Fatalf("verifications: %#v %v", verifications, err)
	}
	claim := verifications.Records[0].Claims[0]
	if strings.Contains(claim.Reason, "hunter2") || claim.Evidence[0].CanonicalURL != "https://docs.example.com/robot" {
		t.Fatalf("verification was not safely projected: %#v", claim)
	}

	overview, err := service.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.ConflictCount != 1 || len(overview.Attention) != 1 || overview.KnowledgeHealth.Healthy != 1 {
		t.Fatalf("overview does not reflect Core state: %#v", overview)
	}
	if overview.RecentProvenance[0].Artifacts[0].Path != "hardware/robot.urdf" || overview.RecentAudit[0].ID != fixture.auditID {
		t.Fatalf("overview trail mismatch: %#v", overview)
	}
}

func TestSafeDisplayURLAndRedaction(t *testing.T) {
	for _, unsafe := range []string{"file:///etc/passwd", "https://user:secret@example.com/x", "javascript:alert(1)", "not a URL"} {
		if got := safeDisplayURL(unsafe); got != "" {
			t.Fatalf("unsafe URL %q projected as %q", unsafe, got)
		}
	}
	if got := safeDisplayURL("https://docs.example.com/path?token=secret#fragment"); got != "https://docs.example.com/path" {
		t.Fatalf("URL metadata not stripped: %q", got)
	}
	if got, changed := redact("value api_key=secret next"); !changed || strings.Contains(got, "secret") {
		t.Fatalf("secret not redacted: %q %t", got, changed)
	}
}

func TestOverviewUsesStableEmptyCollections(t *testing.T) {
	service := &Service{Root: t.TempDir(), StateDir: t.TempDir()}
	overview, err := service.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(overview)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"attention":[]`, `"current_verifications":[]`, `"recent_provenance":[]`, `"recent_audit":[]`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("overview JSON lacks stable empty collection %s: %s", field, data)
		}
	}
}

func TestOverviewSeparatesCurrentVerificationFromHistoryByArtifact(t *testing.T) {
	service, _ := seededService(t)
	evidence, err := evidencestore.Open(service.StateDir).List()
	if err != nil || len(evidence) != 1 {
		t.Fatalf("evidence fixture: %#v %v", evidence, err)
	}
	newer, err := verificationstore.Open(service.StateDir).Append(verificationstore.Record{
		RecordedAt: "2026-09-01T11:01:00Z", ReportID: "vr_customer_current", PackID: "customer-robot", Scope: "working_tree", Repository: verificationstore.Repository{Root: service.Root}, Artifacts: []string{"hardware/robot.urdf"},
		Claims: []verificationstore.Claim{{ArtifactPath: "hardware/robot.urdf", Line: 7, Category: "force_or_torque", Statement: "Gripper force is 140 N.", Status: "VERIFIED", Confidence: .99, Reason: "Matches reviewed source.", Evidence: []verificationstore.EvidenceReference{{EvidenceID: evidence[0].ID, PackID: "customer-robot", SourceID: "robot-docs", CanonicalURL: "https://docs.example.com/robot", SourceType: "official_documentation", Authority: "authoritative", Confidence: .99, RetrievedFact: "Maximum force is 140 N", Revision: "R2", ContentSHA256: evidence[0].ContentSHA256}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	overview, err := service.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if overview.ConflictCount != 0 || overview.UnverifiedCount != 0 || len(overview.Attention) != 0 {
		t.Fatalf("historical conflict dominated current state: %#v", overview)
	}
	if len(overview.CurrentVerifications) != 1 || overview.CurrentVerifications[0].VerificationID != newer.ID || overview.CurrentVerifications[0].Status != "VERIFIED" {
		t.Fatalf("current verification projection mismatch: %#v", overview.CurrentVerifications)
	}
	if history := overview.CurrentVerifications[0].History; len(history) != 1 || history[0].Status != "CONFLICT" {
		t.Fatalf("verification history was not preserved: %#v", history)
	}
}

type seededFixture struct {
	evidenceID     string
	verificationID string
	auditID        string
}

func seededService(t *testing.T) (*Service, seededFixture) {
	t.Helper()
	root, stateDir := t.TempDir(), t.TempDir()
	packDir := filepath.Join(root, ".swipenode", "knowledge")
	if err := os.MkdirAll(packDir, 0o700); err != nil {
		t.Fatal(err)
	}
	pack := `schema_version: swipenode.knowledge-pack.v1
id: customer-robot
owner: Customer Robotics
project: Robot
description: "<img src=x onerror=alert(1)> customer policy"
identities:
  - kind: product
    owner: Customer Robotics
    name: robot
scope:
  topics: [safety]
  components: [gripper]
sources:
  - id: robot-docs
    canonical_url: https://docs.example.com/robot
    authoritative_host: docs.example.com
    source_type: official_documentation
    strategy: explicit_url
    https_required: true
    authoritative: true
trust:
  require_provenance: true
freshness:
  check_strategy: http_metadata_and_content_hash
  suggested_interval: 24h
  revision_strategy: source_derived
provenance:
  reviewed_at: "2026-09-01"
  reviewed_by: customer-engineering
  basis: reviewed project policy
`
	if err := os.WriteFile(filepath.Join(packDir, "customer-robot.yaml"), []byte(pack), 0o600); err != nil {
		t.Fatal(err)
	}
	hash := evidencestore.ContentHash("maximum force is 140 N")
	evidence, err := evidencestore.Open(stateDir).Insert(evidencestore.Record{
		PackID: "customer-robot", SourceID: "robot-docs", CanonicalURL: "https://docs.example.com/robot?token=do-not-show#section",
		Owner: "Customer Robotics", SourceType: "official_documentation", RetrievedAt: "2026-09-01T10:00:00Z", DocumentRevision: "R2", ContentSHA256: hash,
		ExtractedFact: "Maximum force is 140 N; password=hunter2", Provenance: evidencestore.Provenance{CanonicalURL: "https://docs.example.com/robot", Owner: "Customer Robotics", SourceType: "official_documentation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := verificationstore.Open(stateDir).Append(verificationstore.Record{
		RecordedAt: "2026-09-01T10:01:00Z", ReportID: "vr_customer", PackID: "customer-robot", Scope: "working_tree", Repository: verificationstore.Repository{Root: root}, Artifacts: []string{"hardware/robot.urdf"},
		Claims: []verificationstore.Claim{{ArtifactPath: "hardware/robot.urdf", Line: 7, Category: "force_or_torque", Statement: "Gripper force is 200 N.", Status: "CONFLICT", Confidence: .99, Reason: "password=hunter2 does not match", Evidence: []verificationstore.EvidenceReference{{EvidenceID: evidence.ID, PackID: "customer-robot", SourceID: "robot-docs", CanonicalURL: "https://docs.example.com/robot?token=do-not-show", SourceType: "official_documentation", Authority: "authoritative", Confidence: .99, RetrievedFact: "Maximum force is 140 N", Revision: "R2", ContentSHA256: hash}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := audit.Open(stateDir).Append(audit.Event{Type: audit.VerificationStatusChanged, Timestamp: "2026-09-01T10:02:00Z", PackID: "customer-robot", SourceID: "robot-docs", EvidenceID: evidence.ID, VerificationID: verification.ID, ClaimID: verification.Claims[0].ID, ArtifactPath: "hardware/robot.urdf", OldStatus: "VERIFIED", NewStatus: "CONFLICT", CanonicalURL: "https://docs.example.com/robot?token=do-not-show"})
	if err != nil {
		t.Fatal(err)
	}
	record := provenance.EngineeringProvenanceRecord{Timestamp: "2026-09-01T10:03:00Z", Repository: provenance.RepositoryContext{Root: root, GitCommit: strings.Repeat("a", 40), GitBranch: "main"}, Artifacts: []provenance.Artifact{{Path: "hardware/robot.urdf", Kind: "urdf"}}, Verification: provenance.VerificationReference{ID: "vr_customer", RecordID: verification.ID, SchemaVersion: "swipenode.verify.v1", Scope: "working_tree", Statuses: map[string]int{"CONFLICT": 1}}, EvidenceIDs: []string{evidence.ID}, AuditEventIDs: []string{event.ID}, KnowledgePackIDs: []string{"customer-robot"}, Claims: []provenance.ClaimReference{{ArtifactPath: "hardware/robot.urdf", Line: 7, Statement: "Gripper force is 200 N.", Status: "CONFLICT", EvidenceIDs: []string{evidence.ID}}}, SourceRevisions: []provenance.SourceRevision{{EvidenceID: evidence.ID, SourceID: "robot-docs", Revision: "R2", ContentSHA256: hash}}}
	if _, err := provenance.Open(stateDir).Append(record); err != nil {
		t.Fatal(err)
	}
	if err := evidencestore.Open(stateDir).UpdateSourceState(evidencestore.SourceState{PackID: "customer-robot", SourceID: "robot-docs", CanonicalURL: "https://docs.example.com/robot", LastChecked: "2026-09-01T10:00:00Z", LastChanged: "2026-09-01T10:00:00Z", CurrentEvidenceID: evidence.ID, ContentSHA256: hash, DocumentRevision: "R2", Health: "healthy", Status: "unchanged"}); err != nil {
		t.Fatal(err)
	}
	return &Service{Root: root, StateDir: stateDir, Now: func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }}, seededFixture{evidence.ID, verification.ID, event.ID}
}
