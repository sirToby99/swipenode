// Package gitcontext provides read-only repository inspection for SwipeNode
// commands. It never reads remotes, file contents, or integration session data.
package gitcontext

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Repository is a safe summary of the current Git state.
type Repository struct {
	Root      string   `json:"root"`
	Branch    string   `json:"branch"`
	Head      string   `json:"head"`
	Dirty     bool     `json:"dirty"`
	Languages []string `json:"languages"`
	Manifests []string `json:"manifests"`
}

// ResolveRoot discovers the containing Git worktree without consulting any
// integration-specific environment.
func ResolveRoot(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}
	root, err := output(context.Background(), cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("detect git repository root: %w", err)
	}
	root, err = filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("resolve git repository root: %w", err)
	}
	return root, nil
}

// StateDir returns a repository-local runtime directory inside Git metadata.
// Keeping generated evidence and audit state here prevents verification from
// dirtying the worktree while remaining fully local and Entire-independent.
func StateDir(ctx context.Context, root string) (string, error) {
	value, err := output(ctx, root, "rev-parse", "--git-path", "swipenode")
	if err != nil {
		return "", fmt.Errorf("resolve SwipeNode state directory: %w", err)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(root, value)
	}
	return filepath.Abs(filepath.Clean(value))
}

// Inspect returns repository metadata without reading remotes or changed files.
func Inspect(ctx context.Context, root string) (Repository, error) {
	branch, err := output(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		branch = "HEAD"
	}
	head, err := output(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		// A newly initialized repository is valid even though it has no commit yet.
		if _, insideErr := output(ctx, root, "rev-parse", "--is-inside-work-tree"); insideErr != nil {
			return Repository{}, fmt.Errorf("inspect HEAD: %w", err)
		}
		head = ""
	}
	status, err := outputBytes(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return Repository{}, fmt.Errorf("inspect working tree: %w", err)
	}
	languages, manifests := DetectProject(root)
	return Repository{
		Root: root, Branch: branch, Head: head, Dirty: len(bytes.TrimSpace(status)) > 0,
		Languages: languages, Manifests: manifests,
	}, nil
}

// Diff returns the requested local diff and a stable scope label.
func Diff(ctx context.Context, root string, staged bool, ref string) ([]byte, string, error) {
	args := []string{"diff", "--no-ext-diff", "--no-renames", "--unified=0"}
	scope := "working_tree"
	if staged {
		args = append(args, "--cached")
		scope = "staged"
	}
	if ref != "" {
		resolved, err := ResolveCommit(ctx, root, ref)
		if err != nil {
			return nil, "", err
		}
		args = append(args, resolved)
		scope = "ref:" + ref
	}
	args = append(args, "--")
	diff, err := outputBytes(ctx, root, args...)
	if err != nil {
		return nil, "", fmt.Errorf("read %s diff: %w", scope, err)
	}
	return diff, scope, nil
}

// ResolveCommit validates and resolves a user-supplied ref before it is passed
// to git diff, preventing option-shaped values from reaching git.
func ResolveCommit(ctx context.Context, root, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n\t ") {
		return "", fmt.Errorf("invalid git ref %q", ref)
	}
	resolved, err := output(ctx, root, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil || len(resolved) != 40 {
		return "", fmt.Errorf("resolve git ref %q", ref)
	}
	return resolved, nil
}

// HasUntrackedFiles reports whether the default git diff omits untracked files.
func HasUntrackedFiles(ctx context.Context, root string) bool {
	out, err := outputBytes(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	return err == nil && len(out) > 0
}

// DetectProject checks only a controlled set of manifest names. It never opens
// project files or recursively scans repository content.
func DetectProject(root string) ([]string, []string) {
	type marker struct {
		path     string
		language string
	}
	markers := []marker{
		{"go.mod", "Go"}, {"Cargo.toml", "Rust"}, {"package.json", "JavaScript/TypeScript"},
		{"pyproject.toml", "Python"}, {"requirements.txt", "Python"}, {"Pipfile", "Python"},
		{"Gemfile", "Ruby"}, {"pom.xml", "Java"}, {"build.gradle", "Java/Kotlin"},
		{"build.gradle.kts", "Kotlin"}, {"composer.json", "PHP"}, {"Package.swift", "Swift"},
		{"CMakeLists.txt", "C/C++"}, {"Makefile", "Make"}, {"Dockerfile", "Docker"},
	}
	languageSet := map[string]struct{}{}
	manifests := []string{}
	for _, marker := range markers {
		path := filepath.Join(root, marker.path)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			manifests = append(manifests, filepath.ToSlash(marker.path))
			languageSet[marker.language] = struct{}{}
		}
	}
	languages := make([]string, 0, len(languageSet))
	for language := range languageSet {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	sort.Strings(manifests)
	return languages, manifests
}

func output(ctx context.Context, root string, args ...string) (string, error) {
	value, err := outputBytes(ctx, root, args...)
	return strings.TrimSpace(string(value)), err
}

func outputBytes(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
