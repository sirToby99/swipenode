package gitcontext

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectAndDiffModes(t *testing.T) {
	repo := initRepo(t)
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.test/repo\n\ngo 1.25\n")
	writeFile(t, filepath.Join(repo, "fact.txt"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "base")

	writeFile(t, filepath.Join(repo, "fact.txt"), "base\nAPI timeout is 30 seconds.\n")
	info, err := Inspect(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Dirty || info.Head == "" || info.Branch == "" || len(info.Languages) != 1 || info.Languages[0] != "Go" {
		t.Fatalf("unexpected repository info: %#v", info)
	}
	diff, scope, err := Diff(context.Background(), repo, false, "")
	if err != nil || scope != "working_tree" || !strings.Contains(string(diff), "+API timeout") {
		t.Fatalf("working diff scope=%q err=%v diff=%s", scope, err, diff)
	}
	git(t, repo, "add", "fact.txt")
	diff, scope, err = Diff(context.Background(), repo, true, "")
	if err != nil || scope != "staged" || !strings.Contains(string(diff), "+API timeout") {
		t.Fatalf("staged diff scope=%q err=%v diff=%s", scope, err, diff)
	}
	diff, scope, err = Diff(context.Background(), repo, false, "HEAD")
	if err != nil || scope != "ref:HEAD" || !strings.Contains(string(diff), "+API timeout") {
		t.Fatalf("ref diff scope=%q err=%v diff=%s", scope, err, diff)
	}
}

func TestCleanAndMalformedRepositories(t *testing.T) {
	repo := initRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-qm", "base")
	if diff, _, err := Diff(context.Background(), repo, false, ""); err != nil || len(diff) != 0 {
		t.Fatalf("clean diff=%q err=%v", diff, err)
	}
	if _, _, err := Diff(context.Background(), t.TempDir(), false, ""); err == nil {
		t.Fatal("expected malformed/non-git directory error")
	}
	if _, err := ResolveCommit(context.Background(), repo, "--output=/tmp/x"); err == nil {
		t.Fatal("expected option-shaped ref rejection")
	}
}

func TestResolveRootFromNestedDirectory(t *testing.T) {
	repo := initRepo(t)
	nested := filepath.Join(repo, "nested", "directory")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := ResolveRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	if root != repo {
		t.Fatalf("ResolveRoot()=%q, want %q", root, repo)
	}
}

func TestStateDirUsesGitMetadata(t *testing.T) {
	repo := initRepo(t)
	dir, err := StateDir(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repo, ".git", "swipenode")
	if dir != want {
		t.Fatalf("StateDir()=%q, want %q", dir, want)
	}
	if strings.HasPrefix(dir, filepath.Join(repo, ".swipenode")) {
		t.Fatal("runtime state would dirty the worktree")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "config", "user.email", "test@example.invalid")
	git(t, repo, "config", "user.name", "SwipeNode Test")
	return repo
}

func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
