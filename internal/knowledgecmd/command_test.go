package knowledgecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sirToby99/swipenode/internal/extractor"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/spf13/cobra"
)

func TestKnowledgeListShowValidateJSON(t *testing.T) {
	t.Setenv("ENTIRE_REPO_ROOT", "")
	t.Setenv("ENTIRE_SESSION_ID", "")
	t.Setenv("ENTIRE_DATA_DIR", "")
	root, state := t.TempDir(), t.TempDir()
	writeProjectPack(t, root, "team-pack")
	deps := testDeps(root, state)

	list := execute(t, NewKnowledgeCommand(deps), "list", "--json")
	if !strings.Contains(list, `"id": "team-pack"`) || !strings.Contains(list, `"origin": "project"`) || !strings.Contains(list, `"id": "nvidia-jetson"`) {
		t.Fatalf("unexpected list: %s", list)
	}
	show := execute(t, NewKnowledgeCommand(deps), "show", "team-pack", "--json")
	if !strings.Contains(show, `"schema_version": "swipenode.knowledge-pack.v1"`) || !strings.Contains(show, `"id": "docs"`) {
		t.Fatalf("unexpected show: %s", show)
	}
	valid := execute(t, NewKnowledgeCommand(deps), "validate", "--json")
	if !strings.Contains(valid, `"valid": true`) || !strings.Contains(valid, `"pack_count": 5`) {
		t.Fatalf("unexpected validation: %s", valid)
	}
}

func TestKnowledgeQualityMachineReadableAndHonest(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	output := execute(t, NewKnowledgeCommand(testDeps(root, state)), "quality", "nvidia-jetson", "--json")
	if !strings.Contains(output, `"schema_version": "swipenode.pack-quality-reports.v1"`) || !strings.Contains(output, `"release_ready": false`) || !strings.Contains(output, `"dimension": "release_signature"`) {
		t.Fatalf("quality output did not expose incomplete gates honestly: %s", output)
	}
	assessment := filepath.Join(t.TempDir(), "quality.json")
	data := `{"schema_version":"swipenode.pack-quality-assessment.v1","packs":{"nvidia-jetson":{"expected_source_ids":["jetson-linux-release-notes"],"regression_source_ids":["jetson-linux-release-notes"],"extraction_source_ids":["jetson-linux-release-notes"],"core_facts_regression_covered":true,"source_health":{"jetson-linux-release-notes":"healthy"},"release":{"version":"2026.09.1","publisher":"SwipeNode","publisher_key_id":"ed25519:test","package_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","signature_verified":true}}}}`
	if err := os.WriteFile(assessment, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	output = execute(t, NewKnowledgeCommand(testDeps(root, state)), "quality", "nvidia-jetson", "--assessment", assessment, "--json")
	if !strings.Contains(output, `"release_ready": false`) || !strings.Contains(output, "not an activated managed release") {
		t.Fatalf("built-in candidate was incorrectly treated as a managed release: %s", output)
	}
}

func TestRefreshOfflineAndFetchThenAuditCLI(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	writeProjectPack(t, root, "team-pack")
	calls := 0
	deps := testDeps(root, state)
	deps.Extract = func(context.Context, string) (*extractor.ExtractResult, error) {
		calls++
		return &extractor.ExtractResult{URL: "https://docs.example.com/reference", Content: "Example API timeout is 10 seconds.", FetchedAt: "2026-09-01T10:00:00Z", ETag: `"v1"`}, nil
	}
	offline := execute(t, NewKnowledgeCommand(deps), "refresh", "team-pack", "--json")
	if calls != 0 || !strings.Contains(offline, `"status": "offline"`) {
		t.Fatalf("offline refresh used network: %d %s", calls, offline)
	}
	fetched := execute(t, NewKnowledgeCommand(deps), "refresh", "team-pack", "--fetch", "--json")
	if calls != 1 || !strings.Contains(fetched, `"changed": 1`) {
		t.Fatalf("fetch result: calls=%d %s", calls, fetched)
	}
	auditList := execute(t, NewAuditCommand(deps), "list", "--json")
	var listing struct {
		Events []struct {
			ID   string `json:"id"`
			Type string `json:"event"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(auditList), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Events) == 0 || listing.Events[0].ID == "" {
		t.Fatalf("audit list empty: %s", auditList)
	}
	shown := execute(t, NewAuditCommand(deps), "show", listing.Events[0].ID, "--json")
	if !strings.Contains(shown, listing.Events[0].ID) {
		t.Fatalf("audit show missing event: %s", shown)
	}
}

func TestRefreshCLIUsesValidatorsAndDryRunDoesNotMutateStores(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	writeProjectPack(t, root, "team-pack")
	packPath := filepath.Join(root, ".swipenode", "knowledge", "team-pack.yaml")
	data, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "check_strategy: content_hash", "check_strategy: http_metadata_and_content_hash", 1))
	if err := os.WriteFile(packPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	deps := testDeps(root, state)
	calls := 0
	var validators []extractor.RequestOptions
	deps.ExtractConditional = func(_ context.Context, target string, options extractor.RequestOptions) (*extractor.ExtractResult, error) {
		calls++
		validators = append(validators, options)
		if calls == 2 {
			return &extractor.ExtractResult{URL: target, FetchedAt: "2026-09-01T11:00:00Z", ETag: `"v1"`, NotModified: true}, nil
		}
		if calls == 3 {
			return &extractor.ExtractResult{URL: target, Content: "Example API timeout is 20 seconds.", FetchedAt: "2026-09-01T12:00:00Z", ETag: `"v2"`}, nil
		}
		return &extractor.ExtractResult{URL: target, Content: "Example API timeout is 10 seconds.", FetchedAt: "2026-09-01T10:00:00Z", ETag: `"v1"`, LastModified: "Tue, 01 Sep 2026 10:00:00 GMT"}, nil
	}
	first := execute(t, NewKnowledgeCommand(deps), "refresh", "team-pack", "--fetch", "--json")
	if !strings.Contains(first, `"changed": 1`) {
		t.Fatalf("first refresh: %s", first)
	}
	second := execute(t, NewKnowledgeCommand(deps), "refresh", "team-pack", "--fetch", "--json")
	if !strings.Contains(second, `"unchanged": 1`) || len(validators) != 2 || validators[1].IfNoneMatch != `"v1"` || validators[1].IfModifiedSince == "" {
		t.Fatalf("conditional refresh: %s validators=%#v", second, validators)
	}
	before := stateSnapshot(t, state)
	dry := execute(t, NewKnowledgeCommand(deps), "refresh", "team-pack", "--fetch", "--dry-run", "--json")
	if !strings.Contains(dry, `"dry_run": true`) || !strings.Contains(dry, `"changed": 1`) {
		t.Fatalf("dry run report: %s", dry)
	}
	after := stateSnapshot(t, state)
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("dry run mutated state:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestRefreshCLIRejectsRevalidationWithoutFetch(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	writeProjectPack(t, root, "team-pack")
	command := NewKnowledgeCommand(testDeps(root, state))
	command.SetArgs([]string{"refresh", "team-pack", "--revalidate"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "--revalidate requires --fetch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractorFetcherClassifiesDisappearedSource(t *testing.T) {
	fetcher := extractorFetcher{extractConditional: func(context.Context, string, extractor.RequestOptions) (*extractor.ExtractResult, error) {
		return nil, extractor.HTTPStatusError{StatusCode: 410}
	}}
	_, err := fetcher.FetchConditional(context.Background(), "https://docs.example.com/reference", knowledge.FetchCondition{})
	var failure knowledge.FetchFailure
	if !errors.As(err, &failure) || failure.Kind != "source_disappeared" {
		t.Fatalf("disappeared source classification: %v", err)
	}
}

func TestExtractorFetcherClassifiesParserFailure(t *testing.T) {
	fetcher := extractorFetcher{extractConditional: func(context.Context, string, extractor.RequestOptions) (*extractor.ExtractResult, error) {
		return nil, extractor.ParseError{Err: errors.New("invalid document")}
	}}
	_, err := fetcher.FetchConditional(context.Background(), "https://docs.example.com/reference", knowledge.FetchCondition{})
	var failure knowledge.FetchFailure
	if !errors.As(err, &failure) || failure.Kind != "parser_failure" {
		t.Fatalf("parser failure classification: %v", err)
	}
}

func TestUnknownPackAndInvalidLocalPackFailClearly(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	deps := testDeps(root, state)
	command := NewKnowledgeCommand(deps)
	command.SetArgs([]string{"show", "missing"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unknown knowledge pack") {
		t.Fatalf("unexpected error: %v", err)
	}
	dir := filepath.Join(root, ".swipenode", "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("schema_version: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	command = NewKnowledgeCommand(deps)
	command.SetArgs([]string{"validate"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "parse knowledge pack") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func testDeps(root, state string) Dependencies {
	return Dependencies{Getwd: func() (string, error) { return root, nil }, ResolveRoot: func(string) (string, error) { return root, nil }, StateDir: func(context.Context, string) (string, error) { return state, nil }, InspectRepository: func(context.Context, string) (gitcontext.Repository, error) {
		return gitcontext.Repository{Root: root, Branch: "main", Head: strings.Repeat("a", 40)}, nil
	}}
}

func execute(t *testing.T, command *cobra.Command, args ...string) string {
	t.Helper()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("execute %v: %v\n%s", args, err, output.String())
	}
	return output.String()
}

func stateSnapshot(t *testing.T, root string) []string {
	t.Helper()
	values := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		values = append(values, filepath.ToSlash(relative)+"="+string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(values)
	return values
}

func writeProjectPack(t *testing.T, root, id string) {
	t.Helper()
	dir := filepath.Join(root, ".swipenode", "knowledge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `schema_version: swipenode.knowledge-pack.v1
id: ` + id + `
owner: Example
project: Demo
description: Test policy.
identities:
  - kind: library
    owner: Example
    name: demo
scope:
  topics: [demo]
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
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
