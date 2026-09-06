// Package entirectx reads the small environment contract exposed to Entire
// external-command plugins. It deliberately has no dependency on Entire itself.
package entirectx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	EnvCLIVersion    = "ENTIRE_CLI_VERSION"
	EnvRepoRoot      = "ENTIRE_REPO_ROOT"
	EnvPluginDataDir = "ENTIRE_PLUGIN_DATA_DIR"
)

// Context is the complete, optional environment contract supplied by Entire.
type Context struct {
	RepoRoot      string `json:"repo_root,omitempty"`
	CLIVersion    string `json:"cli_version,omitempty"`
	PluginDataDir string `json:"plugin_data_dir,omitempty"`
	Available     bool   `json:"available"`
}

// FromEnvironment reads Entire's optional environment variables. Missing or
// partial environments are valid and never prevent standalone use.
func FromEnvironment() Context {
	return FromLookup(os.Getenv)
}

// FromLookup is the testable form of FromEnvironment.
func FromLookup(getenv func(string) string) Context {
	ctx := Context{
		RepoRoot:      cleanOptionalPath(getenv(EnvRepoRoot)),
		CLIVersion:    strings.TrimSpace(getenv(EnvCLIVersion)),
		PluginDataDir: cleanOptionalPath(getenv(EnvPluginDataDir)),
	}
	ctx.Available = ctx.RepoRoot != "" || ctx.CLIVersion != "" || ctx.PluginDataDir != ""
	return ctx
}

// ResolveRepoRoot prefers ENTIRE_REPO_ROOT. Without it, git discovers the root
// from cwd, preserving the caller's working directory.
func ResolveRepoRoot(ctx Context, cwd string) (string, error) {
	if ctx.RepoRoot != "" {
		info, err := os.Stat(ctx.RepoRoot)
		if err != nil {
			return "", fmt.Errorf("Entire repository root %q: %w", ctx.RepoRoot, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("Entire repository root %q is not a directory", ctx.RepoRoot)
		}
		return ctx.RepoRoot, nil
	}
	if strings.TrimSpace(cwd) == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
	}
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(exitErr.Stderr))
			if message != "" {
				return "", fmt.Errorf("detect git repository root: %s", message)
			}
		}
		return "", fmt.Errorf("detect git repository root: %w", err)
	}
	return cleanRequiredPath(strings.TrimSpace(string(out)))
}

// EnsurePluginDataDir creates Entire's per-plugin directory only when a caller
// explicitly needs storage. No fallback directory is invented in standalone mode.
func EnsurePluginDataDir(ctx Context) (string, error) {
	if ctx.PluginDataDir == "" {
		return "", fmt.Errorf("%s is not available", EnvPluginDataDir)
	}
	if err := os.MkdirAll(ctx.PluginDataDir, 0700); err != nil {
		return "", fmt.Errorf("create Entire plugin data directory: %w", err)
	}
	return ctx.PluginDataDir, nil
}

func cleanOptionalPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned, err := cleanRequiredPath(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return cleaned
}

func cleanRequiredPath(value string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return abs, nil
}
