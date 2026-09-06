package entirecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirToby99/swipenode/internal/entirectx"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/spf13/cobra"
)

const contextSchemaVersion = "swipenode.context.v1"

type contextEntire struct {
	Available  bool   `json:"available"`
	CLIVersion string `json:"cli_version,omitempty"`
	Mode       string `json:"mode"`
}

type contextMCP struct {
	Available  bool `json:"available"`
	Configured bool `json:"configured"`
}

type contextReport struct {
	SchemaVersion    string                `json:"schema_version"`
	Repository       gitcontext.Repository `json:"repository"`
	Entire           contextEntire         `json:"entire"`
	SwipeNodeVersion string                `json:"swipenode_version"`
	MCP              contextMCP            `json:"mcp"`
}

func newContextCommand(info BuildInfo, deps Dependencies) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "context",
		Short: "Show safe repository and Entire execution context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := collectContext(cmd.Context(), info, deps)
			if err != nil {
				return err
			}
			if asJSON {
				return writeIndentedJSON(cmd, report)
			}
			return writeContextHuman(cmd, report)
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable machine-readable JSON")
	return command
}

func collectContext(ctx context.Context, info BuildInfo, deps Dependencies) (contextReport, error) {
	entire := entirectx.FromLookup(deps.Getenv)
	cwd, err := deps.Getwd()
	if err != nil {
		return contextReport{}, fmt.Errorf("get working directory: %w", err)
	}
	root, err := entirectx.ResolveRepoRoot(entire, cwd)
	if err != nil {
		return contextReport{}, err
	}
	repository, err := gitcontext.Inspect(ctx, root)
	if err != nil {
		return contextReport{}, err
	}
	return contextReport{
		SchemaVersion: contextSchemaVersion,
		Repository:    repository,
		Entire: contextEntire{
			Available: entire.Available, CLIVersion: entire.CLIVersion, Mode: safeModeName(entire.Available),
		},
		SwipeNodeVersion: info.Version,
		MCP: contextMCP{
			Available:  deps.MCPAvailable,
			Configured: detectMCPConfiguration(root),
		},
	}, nil
}

func writeContextHuman(cmd *cobra.Command, report contextReport) error {
	out := cmd.OutOrStdout()
	head := report.Repository.Head
	if head == "" {
		head = "(no commits)"
	}
	state := "clean"
	if report.Repository.Dirty {
		state = "dirty"
	}
	lines := []string{
		"Repository root: " + report.Repository.Root,
		"Git branch: " + report.Repository.Branch,
		"HEAD: " + head,
		"Working tree: " + state,
		"Entire mode: " + report.Entire.Mode,
	}
	if report.Entire.CLIVersion != "" {
		lines = append(lines, "Entire CLI version: "+report.Entire.CLIVersion)
	}
	lines = append(lines,
		"SwipeNode version: "+report.SwipeNodeVersion,
		"Languages: "+joinOrNone(report.Repository.Languages),
		"Manifests: "+joinOrNone(report.Repository.Manifests),
		fmt.Sprintf("MCP support: available=%t configured=%t", report.MCP.Available, report.MCP.Configured),
	)
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none detected"
	}
	return strings.Join(values, ", ")
}

func detectMCPConfiguration(repoRoot string) bool {
	paths := []string{
		filepath.Join(repoRoot, ".mcp.json"),
		filepath.Join(repoRoot, ".claude", "mcp.json"),
	}
	if configDir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(configDir, "Claude", "claude_desktop_config.json"))
	}
	for _, path := range paths {
		if configHasSwipeNode(path) {
			return true
		}
	}
	return false
}

func configHasSwipeNode(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 1<<20 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &config) != nil {
		return false
	}
	_, ok := config.MCPServers["swipenode"]
	return ok
}
