package verification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeModifiedDiffConservatively(t *testing.T) {
	diff := []byte("diff --git a/device.go b/device.go\n--- a/device.go\n+++ b/device.go\n@@ -1,0 +2,3 @@\n+// The UR10e payload limit is 12.5 kg.\n+value := internalCalculation(x)\n+// Compatible with ISO 9409-1-50-4-M6.\n")
	report := Analyze(context.Background(), diff, "working_tree", Options{})
	if report.Clean || report.EvidenceMode != EvidenceModeOffline || len(report.Claims) != 2 || len(report.ChangedFiles) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Claims[0].Location.Line != 2 || report.Claims[0].Category != "hardware_limit" || report.Claims[0].VerificationStatus != StatusUnverified {
		t.Fatalf("unexpected first claim: %#v", report.Claims[0])
	}
	if strings.Contains(report.Claims[0].Statement, "internalCalculation") {
		t.Fatalf("internal syntax was classified: %#v", report.Claims)
	}
}

func TestDeterministicVerificationStatusesAndIrrelevantEvidence(t *testing.T) {
	diff := []byte("diff --git a/claims.md b/claims.md\n--- a/claims.md\n+++ b/claims.md\n@@ -0,0 +1,3 @@\n+The Acme API timeout is 30 seconds.\n+The Acme API retry limit is 5 attempts.\n+The Acme API supports lunar mode version 9.\n")
	evidence := []Evidence{
		{
			Source: "fixture://acme-manual", SourceType: "official_documentation", Authority: "authoritative",
			VersionDate: "2026-08-29", Confidence: 0.98,
			Content: "The Acme API timeout is 30 seconds.\nThe Acme API retry limit is 3 attempts.",
		},
		{
			Source: "fixture://irrelevant", SourceType: "official_documentation", Authority: "authoritative",
			VersionDate: "2026-08-29", Confidence: 1,
			Content: "The Other API timeout is 30 seconds. The Other API supports lunar mode version 9.",
		},
	}
	report := Analyze(context.Background(), diff, "working_tree", Options{Evidence: evidence})
	if len(report.Claims) != 3 {
		t.Fatalf("claims=%d, want 3: %#v", len(report.Claims), report)
	}
	want := []Status{StatusVerified, StatusConflict, StatusUnverified}
	for index, status := range want {
		if report.Claims[index].VerificationStatus != status {
			t.Fatalf("claim %d status=%s, want %s: %#v", index, report.Claims[index].VerificationStatus, status, report.Claims[index])
		}
	}
	if report.Claims[0].Evidence[0].RetrievedFact == "" || report.Claims[0].Evidence[0].Relevance < minimumRelevance || report.Claims[0].Reason == "" {
		t.Fatalf("verified claim lacks quality metadata: %#v", report.Claims[0])
	}
	if report.Claims[2].VerificationStatus == StatusVerified {
		t.Fatal("irrelevant evidence created VERIFIED")
	}
}

func TestOfflineModeNeverCallsLookup(t *testing.T) {
	diff := []byte("diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -0,0 +1 @@\n+The Acme API timeout is 10 seconds: https://docs.github.com/?token=secret\n")
	calls := 0
	lookup := LookupFunc(func(context.Context, string) (Evidence, error) {
		calls++
		return Evidence{}, errors.New("must not be called")
	})
	report := Analyze(context.Background(), diff, "working_tree", Options{EvidenceMode: EvidenceModeOffline, Lookup: lookup})
	if calls != 0 || report.EvidenceMode != EvidenceModeOffline {
		t.Fatalf("offline lookup calls=%d report=%#v", calls, report)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("sensitive query leaked: %s", encoded)
	}
}

func TestNetworkModeSanitizesBeforeLookup(t *testing.T) {
	diff := []byte("diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -0,0 +1 @@\n+The GitHub API timeout is 10 seconds: https://docs.github.com/api?token=secret&page=1#fragment\n")
	var got string
	lookup := LookupFunc(func(_ context.Context, sourceURL string) (Evidence, error) {
		got = sourceURL
		return Evidence{
			Source: sourceURL, SourceType: "official_documentation", Authority: "authoritative",
			Confidence: .9, Content: "The GitHub API timeout is 10 seconds.",
		}, nil
	})
	report := Analyze(context.Background(), diff, "working_tree", Options{EvidenceMode: EvidenceModeNetwork, Lookup: lookup})
	if got != "https://docs.github.com/api?page=1" {
		t.Fatalf("lookup URL=%q", got)
	}
	if report.Claims[0].VerificationStatus != StatusVerified {
		t.Fatalf("network fixture was not verified: %#v", report.Claims[0])
	}
}

func TestAnalyzeClassifiesClaimProseWithoutCitationURL(t *testing.T) {
	diff := []byte("diff --git a/transport.go b/transport.go\n--- a/transport.go\n+++ b/transport.go\n@@ -0,0 +1 @@\n+// EXAMPLE over TLS uses default port 444: https://standards.example/rfc/rfc9999.html\n")
	evidence := []Evidence{{
		Source:     "https://standards.example/rfc/rfc9999.html",
		SourceType: "official_documentation",
		Authority:  "authoritative",
		Confidence: .99,
		Content:    "When EXAMPLE/TLS is run over TCP/IP, the default port is 443.",
	}}

	report := Analyze(context.Background(), diff, "working_tree", Options{Evidence: evidence})
	if len(report.Claims) != 1 {
		t.Fatalf("claims=%d, want 1: %#v", len(report.Claims), report)
	}
	claim := report.Claims[0]
	if claim.Category != "protocol_constant_or_behavior" {
		t.Fatalf("category=%q, want protocol_constant_or_behavior", claim.Category)
	}
	if claim.VerificationStatus != StatusConflict {
		t.Fatalf("status=%q, want CONFLICT: %#v", claim.VerificationStatus, claim)
	}
}

func TestFetchedUnknownSourceCannotVerify(t *testing.T) {
	diff := []byte("diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -0,0 +1 @@\n+The Acme API timeout is 10 seconds: https://public.example/api\n")
	lookup := LookupFunc(func(_ context.Context, sourceURL string) (Evidence, error) {
		return Evidence{
			Source: sourceURL, SourceType: "public_html", Authority: "unknown",
			Confidence: 1, Content: "The Acme API timeout is 10 seconds.",
		}, nil
	})
	report := Analyze(context.Background(), diff, "working_tree", Options{EvidenceMode: EvidenceModeNetwork, Lookup: lookup})
	if report.Claims[0].VerificationStatus != StatusUnverified {
		t.Fatalf("retrieval alone created verification: %#v", report.Claims[0])
	}
}

func TestNonEquivalentConstraintQualifiersRemainUnverified(t *testing.T) {
	diff := []byte("diff --git a/device.md b/device.md\n--- a/device.md\n+++ b/device.md\n@@ -0,0 +1 @@\n+The Acme gripper maximum payload is 10 kg.\n")
	evidence := []Evidence{{
		Source: "fixture:manual", SourceType: "official_documentation", Authority: "authoritative",
		Confidence: .99, Content: "The Acme gripper minimum payload is 10 kg.",
	}}
	report := Analyze(context.Background(), diff, "working_tree", Options{Evidence: evidence})
	if report.Claims[0].VerificationStatus != StatusUnverified {
		t.Fatalf("non-equivalent qualifiers should not verify: %#v", report.Claims[0])
	}
}

func TestAnalyzeCleanDiff(t *testing.T) {
	report := Analyze(context.Background(), nil, "staged", Options{})
	if !report.Clean || report.Claims == nil || report.ChangedFiles == nil || report.Warnings == nil {
		t.Fatalf("clean report is not stable: %#v", report)
	}
}

func TestRedactsSensitiveAssignmentsAndCredentialURLs(t *testing.T) {
	statement := redact("API token=super-secret uses https://user:pass@example.com/spec")
	if strings.Contains(statement, "super-secret") || strings.Contains(statement, "user:pass") {
		t.Fatalf("secret was not redacted: %q", statement)
	}
}

func TestLoadCatalog(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".swipenode", "evidence.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	data := `{"schema_version":"swipenode.evidence.v1","evidence":[{"source":"https://docs.github.com/api?token=secret&view=1","source_type":"official_documentation","authority":"authoritative","version_date":"2026-08-29","confidence":0.98,"content":"The GitHub API timeout is 10 seconds."}]}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	evidence, err := LoadCatalog(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].Source != "https://docs.github.com/api?view=1" || evidence[0].Content == "" {
		t.Fatalf("unexpected catalog: %#v", evidence)
	}
}
