package entirecmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstalledEntireCLI is opt-in because it exercises an actual Entire CLI
// binary and builds a fresh plugin. Run with ENTIRE_BIN=/path/to/entire.
func TestInstalledEntireCLI(t *testing.T) {
	entireBinary := os.Getenv("ENTIRE_BIN")
	if entireBinary == "" {
		t.Skip("set ENTIRE_BIN to run the real Entire CLI integration test")
	}
	entireBinary, err := filepath.Abs(entireBinary)
	if err != nil {
		t.Fatal(err)
	}
	repoSource, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	pluginBinary := filepath.Join(work, "entire-swipenode")
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", pluginBinary, "./cmd/entire-swipenode")
	build.Dir = repoSource
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, output)
	}

	repo := filepath.Join(work, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".swipenode"), 0755); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "init", "-q")
	gitCommand(t, repo, "config", "user.email", "entire-integration@example.invalid")
	gitCommand(t, repo, "config", "user.name", "Entire Integration")
	if err := os.WriteFile(filepath.Join(repo, "claims.md"), []byte("# Claims\n"), 0644); err != nil {
		t.Fatal(err)
	}
	catalog := map[string]any{
		"schema_version": "swipenode.evidence.v1",
		"evidence": []map[string]any{
			{
				"source": "https://vendor.example/acme-manual", "source_type": "official_documentation",
				"authority": "authoritative", "version_date": "2026-08-29", "confidence": 0.98,
				"content": "The Acme API timeout is 30 seconds.\nThe Acme API retry limit is 3 attempts.",
			},
			{
				"source": "fixture:irrelevant", "source_type": "official_documentation",
				"authority": "authoritative", "version_date": "2026-08-29", "confidence": 1.0,
				"content": "The Other API supports lunar mode version 9.",
			},
		},
	}
	encoded, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".swipenode", "evidence.json"), append(encoded, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, repo, "add", ".")
	gitCommand(t, repo, "commit", "-qm", "base")
	claims := "# Claims\n\n" +
		"The Acme API timeout is 30 seconds. https://evidence.invalid/acme\n" +
		"The Acme API retry limit is 5 attempts. https://evidence.invalid/acme\n" +
		"The Acme API supports lunar mode version 9. https://evidence.invalid/acme\n"
	if err := os.WriteFile(filepath.Join(repo, "claims.md"), []byte(claims), 0644); err != nil {
		t.Fatal(err)
	}

	pluginDir := filepath.Join(work, "entire-plugins")
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"ENTIRE_PLUGIN_DIR="+pluginDir,
		"ENTIRE_TELEMETRY_OPTOUT=1",
		"HOME="+home,
		"NO_COLOR=1",
	)
	runEntire := func(args ...string) string {
		t.Helper()
		command := exec.Command(entireBinary, args...)
		command.Dir = repo
		command.Env = env
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("entire %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return string(output)
	}

	installOutput := runEntire("plugin", "install", pluginBinary, "--force")
	versionOutput := runEntire("version")
	helpOutput := runEntire("swipenode", "--help")
	contextOutput := runEntire("swipenode", "context")
	doctorOutput := runEntire("swipenode", "doctor")
	verifyOutput := runEntire("swipenode", "verify")
	fetchOutput := runEntire("swipenode", "verify", "--fetch")

	for _, expected := range []string{"context", "doctor", "research", "verify", "init-agents"} {
		if !strings.Contains(helpOutput, expected) {
			t.Fatalf("plugin help missing %q:\n%s", expected, helpOutput)
		}
	}
	for _, forbidden := range []string{"serve", "extract", "mcp", "robotics", "payment", "deploy", "deployment"} {
		if strings.Contains(helpOutput, forbidden) {
			t.Fatalf("plugin help contains unrelated command %q:\n%s", forbidden, helpOutput)
		}
	}
	versionLine := strings.SplitN(strings.TrimSpace(versionOutput), "\n", 2)[0]
	cliVersion := strings.TrimSpace(strings.TrimPrefix(versionLine, "Entire CLI"))
	if cliVersion == "" || cliVersion == versionLine || !strings.Contains(contextOutput, "Repository root: "+repo) || !strings.Contains(contextOutput, "Entire CLI version: "+cliVersion) {
		t.Fatalf("Entire did not forward repository root and CLI version:\n%s", contextOutput)
	}
	expectedDataDir := filepath.Join(pluginDir, "data", "swipenode")
	if !strings.Contains(doctorOutput, expectedDataDir) {
		t.Fatalf("Entire did not forward plugin data directory %q:\n%s", expectedDataDir, doctorOutput)
	}
	for _, status := range []string{"VERIFIED", "CONFLICT", "UNVERIFIED"} {
		if !strings.Contains(verifyOutput, status) {
			t.Fatalf("offline verification missing %s:\n%s", status, verifyOutput)
		}
	}
	if !strings.Contains(verifyOutput, "Evidence mode: offline") || strings.Contains(verifyOutput, "Evidence retrieval failed") {
		t.Fatalf("default verify was not observably offline:\n%s", verifyOutput)
	}
	if !strings.Contains(fetchOutput, "Evidence mode: network-enabled") || !strings.Contains(fetchOutput, "Evidence retrieval failed") {
		t.Fatalf("fetch mode was not observably network-enabled:\n%s", fetchOutput)
	}
	t.Logf("install:\n%s\nversion:\n%s\nhelp:\n%s\ncontext:\n%s\ndoctor:\n%s\nverify:\n%s\nverify --fetch:\n%s", installOutput, versionOutput, helpOutput, contextOutput, doctorOutput, verifyOutput, fetchOutput)
}

func gitCommand(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
