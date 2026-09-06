package entirectx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFromLookupAllVariables(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(t.TempDir(), "plugin-data")
	values := map[string]string{EnvRepoRoot: root, EnvCLIVersion: " 0.10.0 ", EnvPluginDataDir: data}
	got := FromLookup(func(key string) string { return values[key] })
	if !got.Available || got.RepoRoot != root || got.CLIVersion != "0.10.0" || got.PluginDataDir != data {
		t.Fatalf("unexpected context: %#v", got)
	}
}

func TestFromLookupAbsentAndPartial(t *testing.T) {
	if got := FromLookup(func(string) string { return "" }); got.Available {
		t.Fatalf("empty environment reported available: %#v", got)
	}
	got := FromLookup(func(key string) string {
		if key == EnvCLIVersion {
			return "dev"
		}
		return ""
	})
	if !got.Available || got.CLIVersion != "dev" || got.RepoRoot != "" || got.PluginDataDir != "" {
		t.Fatalf("unexpected partial context: %#v", got)
	}
}

func TestPathHandlingAndEntireRootPreference(t *testing.T) {
	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := FromLookup(func(key string) string {
		if key == EnvRepoRoot {
			return filepath.Join(repo, ".", "sub", "..")
		}
		return ""
	})
	root, err := ResolveRepoRoot(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if root != repo {
		t.Fatalf("root = %q, want %q", root, repo)
	}
}

func TestFallbackRepositoryDetection(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	root, err := ResolveRepoRoot(Context{}, nested)
	if err != nil {
		t.Fatal(err)
	}
	if root != repo {
		t.Fatalf("root = %q, want %q", root, repo)
	}
}

func TestEnsurePluginDataDirIsLazy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-created-yet")
	ctx := FromLookup(func(key string) string {
		if key == EnvPluginDataDir {
			return path
		}
		return ""
	})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("data directory was created while parsing: %v", err)
	}
	if _, err := EnsurePluginDataDir(ctx); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("data directory not created: info=%v err=%v", info, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
