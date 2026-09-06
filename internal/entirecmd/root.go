// Package entirecmd implements the commands added by the entire-swipenode
// external-command entry point. Business logic lives in focused internal
// packages so the entry point remains thin.
package entirecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sirToby99/swipenode/internal/entirectx"
	webextractor "github.com/sirToby99/swipenode/internal/extractor"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/provenancecmd"
	"github.com/sirToby99/swipenode/internal/verification"
	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

func (info BuildInfo) String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", info.Version, info.Commit, info.BuildDate)
}

type ExtractFunc func(context.Context, string) (*webextractor.ExtractResult, error)

type Dependencies struct {
	Getenv             func(string) string
	Getwd              func() (string, error)
	LookPath           func(string) (string, error)
	Extract            ExtractFunc
	Lookup             verification.Lookup
	ProvenanceEnricher provenance.Enricher
	MCPAvailable       bool
}

var ErrDoctorFailed = errors.New("doctor found a hard failure")

func DefaultDependencies() Dependencies {
	return Dependencies{
		Getenv: os.Getenv, Getwd: os.Getwd, LookPath: exec.LookPath,
		Extract: webextractor.Extract, Lookup: verification.SwipeNodeLookup{}, MCPAvailable: false,
	}
}

func normalizeDependencies(deps Dependencies) Dependencies {
	defaults := DefaultDependencies()
	if deps.Getenv == nil {
		deps.Getenv = defaults.Getenv
	}
	if deps.Getwd == nil {
		deps.Getwd = defaults.Getwd
	}
	if deps.LookPath == nil {
		deps.LookPath = defaults.LookPath
	}
	if deps.Extract == nil {
		deps.Extract = defaults.Extract
	}
	if deps.Lookup == nil {
		deps.Lookup = defaults.Lookup
	}
	return deps
}

// NewCommands returns fresh Cobra commands so tests and alternate entry points
// never share mutable flags.
func NewCommands(info BuildInfo, deps Dependencies) []*cobra.Command {
	deps = normalizeDependencies(deps)
	return []*cobra.Command{
		newVersionCommand(info),
		newContextCommand(info, deps),
		newDoctorCommand(info, deps),
		newResearchCommand(info, deps),
		newVerifyCommand(deps),
		newProvenanceCommand(deps),
		newInitAgentsCommand(deps),
	}
}

func newProvenanceCommand(deps Dependencies) *cobra.Command {
	return provenancecmd.NewCommand(provenancecmd.Dependencies{
		Getwd: deps.Getwd,
		ResolveRoot: func(cwd string) (string, error) {
			return entirectx.ResolveRepoRoot(entirectx.FromLookup(deps.Getenv), cwd)
		},
	})
}

// NewRoot creates the plugin-only command root used by the production binary.
func NewRoot(info BuildInfo, deps Dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:           "entire-swipenode",
		Short:         "Source-backed external technical context for Entire",
		Version:       info.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(NewCommands(info, deps)...)
	return root
}

func newVersionCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print SwipeNode build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())
			return err
		},
	}
}

func Execute(info BuildInfo) int {
	root := NewRoot(info, DefaultDependencies())
	if err := root.Execute(); err != nil {
		if !errors.Is(err, ErrDoctorFailed) {
			_, _ = fmt.Fprintln(root.ErrOrStderr(), "Error:", err)
		}
		return 1
	}
	return 0
}

func writeIndentedJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func safeModeName(available bool) string {
	if available {
		return "entire"
	}
	return "standalone"
}

func trimError(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}
