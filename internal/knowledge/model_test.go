package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsAndIdentityResolution(t *testing.T) {
	registry, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(registry.List()); got != 4 {
		t.Fatalf("built-in count = %d, want 4", got)
	}
	pack, err := registry.ResolveOne(TechnicalIdentity{Kind: "product_family", Owner: "nvidia", Name: "JETSON"})
	if err != nil {
		t.Fatal(err)
	}
	if pack.ID != "nvidia-jetson" || pack.Origin != OriginBuiltin {
		t.Fatalf("unexpected pack: %#v", pack)
	}
	if _, err := registry.ResolveOne(TechnicalIdentity{Kind: "protocol", Name: "not-real"}); err == nil || !strings.Contains(err.Error(), "no knowledge pack") {
		t.Fatalf("expected no match, got %v", err)
	}
}

func TestAmbiguousIdentity(t *testing.T) {
	r := &Registry{packs: map[string]Pack{
		"one": {ID: "one", Identities: []TechnicalIdentity{{Kind: "protocol", Name: "demo"}}},
		"two": {ID: "two", Identities: []TechnicalIdentity{{Kind: "protocol", Name: "demo"}}},
	}}
	if _, err := r.ResolveOne(TechnicalIdentity{Kind: "protocol", Name: "demo"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity, got %v", err)
	}
}

func TestPackParsingAndValidation(t *testing.T) {
	valid := testPackYAML("custom-pack", "https://docs.example.com/reference")
	pack, err := Parse([]byte(valid), OriginProject)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Origin != OriginProject || !pack.AllowsURL("https://docs.example.com/reference") {
		t.Fatalf("pack not parsed or URL not allowed: %#v", pack)
	}

	tests := []struct{ name, mutate, want string }{
		{"unknown field", strings.Replace(valid, "description:", "mystery: value\ndescription:", 1), "field mystery"},
		{"malformed", "schema_version: [", "parse knowledge pack"},
		{"multiple documents", valid + "---\nid: second\n", "exactly one YAML document"},
		{"http authority", strings.Replace(valid, "https://", "http://", 1), "must use HTTPS"},
		{"localhost", strings.Replace(valid, "docs.example.com", "localhost", -1), "network safety"},
		{"credentials", strings.Replace(valid, "https://", "https://user:pass@", 1), "credentials"},
		{"secret value", strings.Replace(valid, "basis: Local fixture.", "basis: token=do-not-store", 1), "must not contain secrets"},
		{"unknown identity", strings.Replace(valid, "kind: library", "kind: mystery", 1), "unsupported identity kind"},
		{"unknown source type", strings.Replace(valid, "source_type: official_documentation", "source_type: mystery", 1), "unsupported source_type"},
		{"authority without provenance", strings.Replace(valid, "require_provenance: true", "require_provenance: false", 1), "requires trust.require_provenance"},
		{"deceptive suffix", strings.Replace(valid, "canonical_url: https://docs.example.com/reference", "canonical_url: https://docs.example.com.evil.test/reference", 1), "host boundary"},
		{"duplicate source", strings.Replace(valid, "trust:", "  - id: docs\n    canonical_url: https://docs.example.com/second\n    authoritative_host: docs.example.com\n    source_type: official_documentation\n    strategy: explicit_url\n    https_required: true\n    authoritative: true\ntrust:", 1), "duplicate source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse([]byte(test.mutate), OriginProject); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDomainBoundaryAndResolutionStrategies(t *testing.T) {
	pack, err := Parse([]byte(testPackYAML("boundary-pack", "https://docs.example.com/reference")), OriginProject)
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []string{"https://docs.example.com.evil.test/reference", "https://evil-docs.example.com/reference", "http://docs.example.com/reference", "https://docs.example.com/other"} {
		if pack.AllowsURL(denied) {
			t.Fatalf("deceptive or unrelated URL allowed: %s", denied)
		}
	}
	pack.Sources[0].AllowSubdomains = true
	pack.Sources[0].AuthoritativeHost = "example.com"
	if !pack.AllowsURL("https://docs.example.com/reference") {
		t.Fatal("documented subdomain boundary should be accepted")
	}
	if pack.AllowsURL("https://example.com.evil.test/reference") {
		t.Fatal("deceptive suffix accepted")
	}

	pack.Sources[0].Strategy = "base_url_endpoints"
	pack.Sources[0].CanonicalURL = "https://docs.example.com/base/"
	pack.Sources[0].Endpoints = []string{"guide", "api"}
	resolved, err := pack.ResolveSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 || resolved[0].URL != "https://docs.example.com/base/api" || resolved[1].URL != "https://docs.example.com/base/guide" {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
	pack.Sources[0].Strategy, pack.Sources[0].PathPattern, pack.Sources[0].Endpoints = "path_pattern", "/base/*", nil
	resolved, err = pack.ResolveSources()
	if err != nil || len(resolved) != 0 {
		t.Fatalf("path-only policy must remain unresolved, got %#v, %v", resolved, err)
	}
}

func TestProjectDiscoveryAndDuplicateProtection(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".swipenode", "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(testPackYAML("custom-pack", "https://docs.example.com/reference")), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	pack, ok := registry.Find("custom-pack")
	if !ok || pack.Origin != OriginProject {
		t.Fatalf("project pack missing: %#v", pack)
	}
	if err := os.WriteFile(filepath.Join(dir, "duplicate.yaml"), []byte(strings.Replace(testPackYAML("custom-pack", "https://docs.example.com/reference"), "custom-pack", "nvidia-jetson", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "cannot override builtin") {
		t.Fatalf("expected duplicate built-in protection, got %v", err)
	}
}

func TestProjectPackSymlinkIsRejected(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".swipenode", "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte(testPackYAML("outside-pack", "https://docs.example.com/reference")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestPackRejectsIPSourceIdentity(t *testing.T) {
	for _, sourceURL := range []string{"https://93.184.216.34/reference", "https://[2606:2800:220:1:248:1893:25c8:1946]/reference"} {
		if _, err := Parse([]byte(testPackYAML("ip-source", sourceURL)), OriginProject); err == nil || !strings.Contains(err.Error(), "DNS hostname") {
			t.Fatalf("IP source identity %q accepted: %v", sourceURL, err)
		}
	}
}

func testPackYAML(id, sourceURL string) string {
	return `schema_version: swipenode.knowledge-pack.v1
id: ` + id + `
owner: Example
project: Demo
description: Test source policy.
identities:
  - kind: library
    owner: Example
    name: demo
scope:
  topics: [demo]
sources:
  - id: docs
    canonical_url: ` + sourceURL + `
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
}
