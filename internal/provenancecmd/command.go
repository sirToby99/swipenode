// Package provenancecmd exposes SwipeNode-owned Engineering Provenance without
// depending on an external integration.
package provenancecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/spf13/cobra"
)

type Dependencies struct {
	Getwd       func() (string, error)
	ResolveRoot func(string) (string, error)
	StateDir    func(context.Context, string) (string, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{Getwd: os.Getwd, ResolveRoot: gitcontext.ResolveRoot, StateDir: gitcontext.StateDir}
}

func normalize(deps Dependencies) Dependencies {
	defaults := DefaultDependencies()
	if deps.Getwd == nil {
		deps.Getwd = defaults.Getwd
	}
	if deps.ResolveRoot == nil {
		deps.ResolveRoot = defaults.ResolveRoot
	}
	if deps.StateDir == nil {
		deps.StateDir = defaults.StateDir
	}
	return deps
}

func NewCommand(deps Dependencies) *cobra.Command {
	deps = normalize(deps)
	command := &cobra.Command{Use: "provenance", Short: "Inspect local Engineering Provenance", Args: cobra.NoArgs}
	command.AddCommand(newListCommand(deps), newShowCommand(deps))
	return command
}

func openStore(ctx context.Context, deps Dependencies) (*provenance.Store, error) {
	cwd, err := deps.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	root, err := deps.ResolveRoot(cwd)
	if err != nil {
		return nil, err
	}
	stateDir, err := deps.StateDir(ctx, root)
	if err != nil {
		return nil, err
	}
	return provenance.Open(stateDir), nil
}

func newListCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "list", Short: "List newest provenance records first", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := openStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		records, err := store.List()
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, struct {
				SchemaVersion string                                   `json:"schema_version"`
				Records       []provenance.EngineeringProvenanceRecord `json:"records"`
			}{SchemaVersion: "swipenode.provenance-list.v1", Records: records})
		}
		for _, record := range records {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%d artifacts\n", record.ProvenanceID, record.Timestamp, record.Adapter, record.Repository.GitCommit, len(record.Artifacts)); err != nil {
				return err
			}
		}
		return nil
	}}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}

func newShowCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{Use: "show <provenance-id>", Short: "Show one provenance record", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := openStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		record, ok, err := store.Find(args[0])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("unknown provenance record %q", args[0])
		}
		if asJSON {
			return writeJSON(cmd, record)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nTimestamp: %s\nAdapter: %s\nRepository: %s\nCommit: %s\nBranch: %s\nVerification: %s\nArtifacts: %d\nEvidence: %d\nAudit events: %d\n", record.ProvenanceID, record.Timestamp, record.Adapter, record.Repository.Root, record.Repository.GitCommit, record.Repository.GitBranch, record.Verification.ID, len(record.Artifacts), len(record.EvidenceIDs), len(record.AuditEventIDs))
		return err
	}}
	command.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return command
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
