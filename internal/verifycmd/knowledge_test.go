package verifycmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/verification"
	"github.com/sirToby99/swipenode/internal/verificationstore"
)

func TestVerifyWithKnowledgePackConstrainsAuthority(t *testing.T) {
	root := newRepository(t)
	writePack(t, root)
	writeClaim(t, root, "// Example API timeout is 10 seconds: https://docs.example.com/reference\npackage demo\n")
	deps := DefaultDependencies()
	stateDir := t.TempDir()
	deps.Getwd = func() (string, error) { return root, nil }
	deps.ResolveRoot = func(string) (string, error) { return root, nil }
	deps.StateDir = func(context.Context, string) (string, error) { return stateDir, nil }
	deps.Lookup = verification.LookupFunc(func(_ context.Context, sourceURL string) (verification.Evidence, error) {
		return verification.Evidence{Source: sourceURL, URLReference: sourceURL, SourceType: "public_html", Authority: "unknown", Content: "The Example API timeout is 10 seconds.", Confidence: 0.9}, nil
	})
	report := executeVerify(t, deps, "--knowledge-pack", "team-pack", "--fetch", "--json")
	if len(report.Claims) != 1 || len(report.Claims[0].Evidence) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Claims[0].Evidence[0].Authority != "authoritative" || report.Claims[0].Evidence[0].SourceType != "official_documentation" {
		t.Fatalf("pack did not apply: %#v", report.Claims[0].Evidence[0])
	}
	records, err := evidencestore.Open(stateDir).List()
	if err != nil || len(records) != 1 || records[0].PackID != "team-pack" || records[0].ExtractedFact == "" || records[0].Verification.VerificationID == "" {
		t.Fatalf("verification evidence was not preserved: %#v, %v", records, err)
	}
	verificationRecords, err := verificationstore.Open(stateDir).List()
	if err != nil || len(verificationRecords) != 1 || len(verificationRecords[0].Claims) != 1 || len(verificationRecords[0].Claims[0].Evidence) != 1 || verificationRecords[0].Claims[0].Evidence[0].EvidenceID != records[0].ID || verificationRecords[0].Claims[0].Evidence[0].SourceID != "docs" {
		t.Fatalf("verification dependency was not preserved: %#v, %v", verificationRecords, err)
	}

	writeClaim(t, root, "// Example API timeout is 10 seconds: https://docs.example.com.evil.test/reference\npackage demo\n")
	report = executeVerify(t, deps, "--knowledge-pack", "team-pack", "--fetch", "--json")
	if report.Claims[0].Evidence[0].Authority != "unknown" {
		t.Fatalf("deceptive hostname retained authority: %#v", report.Claims[0].Evidence[0])
	}
}

func TestVerifyUnknownKnowledgePackFails(t *testing.T) {
	root := newRepository(t)
	deps := DefaultDependencies()
	deps.Getwd = func() (string, error) { return root, nil }
	deps.ResolveRoot = func(string) (string, error) { return root, nil }
	command := NewCommand(deps)
	command.SetArgs([]string{"--knowledge-pack", "missing", "--json"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unknown knowledge pack") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func executeVerify(t *testing.T, deps Dependencies, args ...string) verification.Report {
	t.Helper()
	var output bytes.Buffer
	command := NewCommand(deps)
	command.SetArgs(args)
	command.SetOut(&output)
	command.SetErr(&output)
	if err := command.Execute(); err != nil {
		t.Fatalf("verify: %v\n%s", err, output.String())
	}
	var report verification.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, output.String())
	}
	return report
}

func newRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte("package demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "demo.go")
	runGit(t, root, "commit", "-qm", "initial")
	return root
}

func writeClaim(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "demo.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writePack(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".swipenode", "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `schema_version: swipenode.knowledge-pack.v1
id: team-pack
owner: Example
project: API
description: Test source policy.
identities:
  - kind: library
    owner: Example
    name: api
scope:
  topics: [api]
sources:
  - id: docs
    canonical_url: https://docs.example.com/reference
    authoritative_host: docs.example.com
    source_type: official_documentation
    strategy: explicit_url
    https_required: true
    authoritative: true
trust:
  require_provenance: true
freshness:
  check_strategy: content_hash
  suggested_interval: 24h
  revision_strategy: source_derived
provenance:
  reviewed_at: "2026-09-01"
  reviewed_by: tests
  basis: Local fixture.
`
	if err := os.WriteFile(filepath.Join(dir, "team-pack.yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
