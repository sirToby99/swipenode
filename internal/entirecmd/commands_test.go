package entirecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webextractor "github.com/sirToby99/swipenode/internal/extractor"
	"github.com/sirToby99/swipenode/internal/verification"
)

var testBuild = BuildInfo{Version: "v9.8.7", Commit: "abc123", BuildDate: "2026-08-28T00:00:00Z"}

func TestContextJSONEntireAndStandaloneModes(t *testing.T) {
	repo := commandTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".mcp.json"), []byte(`{"mcpServers":{"swipenode":{"command":"swipenode","args":["mcp"]}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(t.TempDir(), "plugin-data")
	deps := commandDeps(repo, map[string]string{
		"ENTIRE_REPO_ROOT": repo, "ENTIRE_CLI_VERSION": "0.10.0", "ENTIRE_PLUGIN_DATA_DIR": dataDir,
	})
	output, err := executeTestCommand(t, deps, "context", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report contextReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Repository.Root != repo || !report.Entire.Available || report.Entire.CLIVersion != "0.10.0" || report.SwipeNodeVersion != testBuild.Version || report.MCP.Available || !report.MCP.Configured {
		t.Fatalf("unexpected Entire context: %#v", report)
	}

	standalone := commandDeps(repo, nil)
	output, err = executeTestCommand(t, standalone, "context", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Entire.Available || report.Entire.Mode != "standalone" {
		t.Fatalf("unexpected standalone context: %#v", report.Entire)
	}
}

func TestDoctorExitCodes(t *testing.T) {
	repo := commandTestRepo(t)
	deps := commandDeps(repo, nil)
	output, err := executeTestCommand(t, deps, "doctor", "--json")
	if err != nil {
		t.Fatalf("warnings must not fail doctor: %v\n%s", err, output)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Overall != checkWarn {
		t.Fatalf("standalone doctor overall=%s, want WARN", report.Overall)
	}

	nonRepo := commandDeps(t.TempDir(), nil)
	output, err = executeTestCommand(t, nonRepo, "doctor", "--json")
	if !errors.Is(err, ErrDoctorFailed) {
		t.Fatalf("doctor error=%v, want hard failure; output=%s", err, output)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Overall != checkFail {
		t.Fatalf("failed doctor report=%#v err=%v", report, err)
	}
}

func TestResearchReusesExtractorAndForwardsQuestion(t *testing.T) {
	repo := commandTestRepo(t)
	deps := commandDeps(repo, nil)
	var gotURL string
	deps.Extract = func(_ context.Context, sourceURL string) (*webextractor.ExtractResult, error) {
		gotURL = sourceURL
		return &webextractor.ExtractResult{
			URL: sourceURL, Title: "Official API", Content: "The API timeout is 10 seconds.\nUnrelated material.",
			FetchedAt: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC).Format(time.RFC3339), Warnings: []string{},
		}, nil
	}
	question := "What timeout does the API use?"
	output, err := executeTestCommand(t, deps, "research", question, "--source", "https://docs.example.com/api", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report researchReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Question != question || gotURL != "https://docs.example.com/api" || len(report.Sources) != 1 || len(report.Sources[0].Excerpts) == 0 {
		t.Fatalf("unexpected research report: %#v", report)
	}
	if _, err := executeTestCommand(t, deps, "research", question); err == nil {
		t.Fatal("research without an explicit source should fail honestly")
	}
}

func TestVerifyCleanModifiedAndStaged(t *testing.T) {
	repo := commandTestRepo(t)
	deps := commandDeps(repo, nil)
	deps.Lookup = verification.LookupFunc(func(context.Context, string) (verification.Evidence, error) {
		return verification.Evidence{}, errors.New("network disabled in tests")
	})
	output, err := executeTestCommand(t, deps, "verify", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report verification.Report
	if err := json.Unmarshal([]byte(output), &report); err != nil || !report.Clean {
		t.Fatalf("clean report=%#v err=%v", report, err)
	}
	readmePath := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n\nThe vendor API timeout is 15 seconds.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	output, err = executeTestCommand(t, deps, "verify", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Clean || report.Scope != "working_tree" || len(report.Claims) != 1 {
		t.Fatalf("modified report=%#v err=%v", report, err)
	}
	if err := os.WriteFile(readmePath, []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	factPath := filepath.Join(repo, "fact.go")
	if err := os.WriteFile(factPath, []byte("package fact\n\n// The API timeout is 30 seconds.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Default git diff intentionally excludes untracked files.
	output, err = executeTestCommand(t, deps, "verify", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || !report.Clean || len(report.Warnings) != 1 {
		t.Fatalf("untracked report=%#v err=%v", report, err)
	}
	commandGit(t, repo, "add", "fact.go")
	output, err = executeTestCommand(t, deps, "verify", "--staged", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Clean || report.Scope != "staged" || len(report.Claims) != 1 {
		t.Fatalf("staged report=%#v err=%v", report, err)
	}
	output, err = executeTestCommand(t, deps, "verify", "--ref", "HEAD", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || report.Clean || report.Scope != "ref:HEAD" || len(report.Claims) != 1 {
		t.Fatalf("ref report=%#v err=%v", report, err)
	}
}

func TestVerifyNetworkIsExplicitOptIn(t *testing.T) {
	repo := commandTestRepo(t)
	readmePath := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test\n\nThe GitHub API timeout is 10 seconds: https://docs.github.com/api?token=secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	deps := commandDeps(repo, nil)
	calls := 0
	deps.Lookup = verification.LookupFunc(func(_ context.Context, sourceURL string) (verification.Evidence, error) {
		calls++
		if sourceURL != "https://docs.github.com/api" {
			t.Fatalf("unsafe lookup URL: %q", sourceURL)
		}
		return verification.Evidence{
			Source: sourceURL, SourceType: "official_documentation", Authority: "authoritative",
			Confidence: .9, Content: "The GitHub API timeout is 10 seconds.",
		}, nil
	})
	output, err := executeTestCommand(t, deps, "verify", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var report verification.Report
	if err := json.Unmarshal([]byte(output), &report); err != nil || calls != 0 || report.EvidenceMode != verification.EvidenceModeOffline {
		t.Fatalf("offline report=%#v calls=%d err=%v", report, calls, err)
	}
	output, err = executeTestCommand(t, deps, "verify", "--fetch", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil || calls != 1 || report.EvidenceMode != verification.EvidenceModeNetwork || report.Claims[0].VerificationStatus != verification.StatusVerified {
		t.Fatalf("network report=%#v calls=%d err=%v", report, calls, err)
	}
}

func TestInitAgentsCommandDryRunAndIdempotency(t *testing.T) {
	repo := commandTestRepo(t)
	deps := commandDeps(repo, nil)
	output, err := executeTestCommand(t, deps, "init-agents", "--dry-run")
	if err != nil || !strings.Contains(output, "BEGIN SWIPENODE") {
		t.Fatalf("dry-run output=%q err=%v", output, err)
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote file: %v", err)
	}
	if _, err := executeTestCommand(t, deps, "init-agents"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if _, err := executeTestCommand(t, deps, "init-agents"); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if !bytes.Equal(first, second) {
		t.Fatal("second init-agents invocation changed AGENTS.md")
	}
}

func TestMalformedGitStateAndArgumentValidation(t *testing.T) {
	deps := commandDeps(t.TempDir(), nil)
	if _, err := executeTestCommand(t, deps, "context", "--json"); err == nil {
		t.Fatal("context should reject a non-repository")
	}
	if _, err := executeTestCommand(t, deps, "verify", "--staged", "--ref", "HEAD"); err == nil {
		t.Fatal("verify should reject conflicting scopes")
	}
}

func TestVersionArgumentForwarding(t *testing.T) {
	repo := commandTestRepo(t)
	output, err := executeTestCommand(t, commandDeps(repo, nil), "version")
	if err != nil || !strings.Contains(output, testBuild.String()) {
		t.Fatalf("version output=%q err=%v", output, err)
	}
	if _, err := executeTestCommand(t, commandDeps(repo, nil), "version", "unexpected"); err == nil {
		t.Fatal("forwarded positional argument should reach Cobra validation")
	}
}

func executeTestCommand(t *testing.T, deps Dependencies, args ...string) (string, error) {
	t.Helper()
	root := NewRoot(testBuild, deps)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}

func commandDeps(cwd string, env map[string]string) Dependencies {
	deps := DefaultDependencies()
	deps.Getwd = func() (string, error) { return cwd, nil }
	deps.Getenv = func(key string) string { return env[key] }
	deps.Lookup = verification.LookupFunc(func(context.Context, string) (verification.Evidence, error) {
		return verification.Evidence{}, errors.New("network disabled in tests")
	})
	return deps
}

func commandTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	commandGit(t, repo, "init", "-q")
	commandGit(t, repo, "config", "user.email", "test@example.invalid")
	commandGit(t, repo, "config", "user.name", "SwipeNode Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	commandGit(t, repo, "add", "README.md")
	commandGit(t, repo, "commit", "-qm", "base")
	return repo
}

func commandGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
