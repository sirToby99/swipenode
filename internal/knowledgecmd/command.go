// Package knowledgecmd constructs Entire-independent knowledge and audit CLI commands.
package knowledgecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/extractor"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/localstate"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/verificationstore"
	"github.com/spf13/cobra"
)

type ExtractFunc func(context.Context, string) (*extractor.ExtractResult, error)
type ConditionalExtractFunc func(context.Context, string, extractor.RequestOptions) (*extractor.ExtractResult, error)

type Dependencies struct {
	Getwd              func() (string, error)
	ResolveRoot        func(string) (string, error)
	StateDir           func(context.Context, string) (string, error)
	Extract            ExtractFunc
	ExtractConditional ConditionalExtractFunc
	InspectRepository  func(context.Context, string) (gitcontext.Repository, error)
}

func DefaultDependencies() Dependencies {
	return Dependencies{Getwd: os.Getwd, ResolveRoot: gitcontext.ResolveRoot, StateDir: gitcontext.StateDir, Extract: extractor.Extract, ExtractConditional: extractor.ExtractWithOptions, InspectRepository: gitcontext.Inspect}
}

func normalize(deps Dependencies) Dependencies {
	defaults := DefaultDependencies()
	customExtract := deps.Extract != nil
	if deps.Getwd == nil {
		deps.Getwd = defaults.Getwd
	}
	if deps.ResolveRoot == nil {
		deps.ResolveRoot = defaults.ResolveRoot
	}
	if deps.StateDir == nil {
		deps.StateDir = defaults.StateDir
	}
	if deps.Extract == nil {
		deps.Extract = defaults.Extract
	}
	if deps.ExtractConditional == nil {
		if customExtract {
			deps.ExtractConditional = func(ctx context.Context, target string, _ extractor.RequestOptions) (*extractor.ExtractResult, error) {
				return deps.Extract(ctx, target)
			}
		} else {
			deps.ExtractConditional = defaults.ExtractConditional
		}
	}
	if deps.InspectRepository == nil {
		deps.InspectRepository = defaults.InspectRepository
	}
	return deps
}

func rootAndRegistry(ctx context.Context, deps Dependencies) (string, *knowledge.Registry, error) {
	cwd, err := deps.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("get working directory: %w", err)
	}
	root, err := deps.ResolveRoot(cwd)
	if err != nil {
		return "", nil, err
	}
	stateDir, err := deps.StateDir(ctx, root)
	if err != nil {
		return "", nil, err
	}
	registry, err := knowledge.LoadWithState(root, stateDir)
	if err != nil {
		return "", nil, err
	}
	if err := knowledge.SyncRegistry(registry, stateDir); err != nil {
		return "", nil, err
	}
	return root, registry, nil
}

func NewKnowledgeCommand(deps Dependencies) *cobra.Command {
	deps = normalize(deps)
	command := &cobra.Command{Use: "knowledge", Short: "Inspect and refresh local Knowledge Packs", Args: cobra.NoArgs}
	command.AddCommand(newListCommand(deps), newShowCommand(deps), newValidateCommand(deps), newRefreshCommand(deps), newQualityCommand(deps))
	addDistributionCommands(command, deps)
	return command
}

type packSummary struct {
	ID          string                    `json:"id"`
	Owner       string                    `json:"owner"`
	SourceCount int                       `json:"source_count"`
	Scope       knowledge.Scope           `json:"scope"`
	Origin      knowledge.Origin          `json:"origin"`
	Freshness   knowledge.FreshnessPolicy `json:"freshness"`
}

func newListCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "list", Short: "List built-in and project Knowledge Packs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, registry, err := rootAndRegistry(cmd.Context(), deps)
		if err != nil {
			return err
		}
		summaries := []packSummary{}
		for _, pack := range registry.List() {
			summaries = append(summaries, packSummary{ID: pack.ID, Owner: pack.Owner, SourceCount: len(pack.Sources), Scope: pack.Scope, Origin: pack.Origin, Freshness: pack.Freshness})
		}
		if asJSON {
			return writeJSON(cmd, struct {
				SchemaVersion string        `json:"schema_version"`
				Packs         []packSummary `json:"packs"`
			}{"swipenode.knowledge-list.v1", summaries})
		}
		for _, pack := range summaries {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%d source(s)\t%s\n", pack.ID, pack.Owner, pack.Origin, pack.SourceCount, pack.Freshness.SuggestedInterval); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newShowCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "show <id>", Short: "Show one Knowledge Pack", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, registry, err := rootAndRegistry(cmd.Context(), deps)
		if err != nil {
			return err
		}
		pack, ok := registry.Find(args[0])
		if !ok {
			return fmt.Errorf("unknown knowledge pack %q", args[0])
		}
		if asJSON {
			return writeJSON(cmd, pack)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nOwner: %s\nProject: %s\nOrigin: %s\nDescription: %s\nSources: %d\nTopics: %s\nFreshness: %s (%s)\n", pack.ID, pack.Owner, pack.Project, pack.Origin, pack.Description, len(pack.Sources), strings.Join(pack.Scope.Topics, ", "), pack.Freshness.CheckStrategy, pack.Freshness.SuggestedInterval)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newValidateCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "validate", Short: "Validate all built-in and project Knowledge Packs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, registry, err := rootAndRegistry(cmd.Context(), deps)
		if err != nil {
			return err
		}
		result := struct {
			SchemaVersion string `json:"schema_version"`
			Valid         bool   `json:"valid"`
			PackCount     int    `json:"pack_count"`
		}{"swipenode.knowledge-validation.v1", true, len(registry.List())}
		if asJSON {
			return writeJSON(cmd, result)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Valid: %d Knowledge Packs\n", result.PackCount)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

type extractorFetcher struct {
	extract            ExtractFunc
	extractConditional ConditionalExtractFunc
}

func (f extractorFetcher) Fetch(ctx context.Context, target string) (knowledge.FetchedDocument, error) {
	result, err := f.extract(ctx, target)
	if err != nil {
		return knowledge.FetchedDocument{}, err
	}
	return knowledge.FetchedDocument{URL: result.URL, Content: result.Content, FetchedAt: result.FetchedAt, ETag: result.ETag, LastModified: result.LastModified}, nil
}

func (f extractorFetcher) FetchConditional(ctx context.Context, target string, condition knowledge.FetchCondition) (knowledge.FetchedDocument, error) {
	result, err := f.extractConditional(ctx, target, extractor.RequestOptions{IfNoneMatch: condition.ETag, IfModifiedSince: condition.LastModified})
	if err != nil {
		var statusError extractor.HTTPStatusError
		if errors.As(err, &statusError) && (statusError.StatusCode == 404 || statusError.StatusCode == 410) {
			return knowledge.FetchedDocument{}, knowledge.FetchFailure{Kind: "source_disappeared", Err: err}
		}
		var parseError extractor.ParseError
		if errors.As(err, &parseError) {
			return knowledge.FetchedDocument{}, knowledge.FetchFailure{Kind: "parser_failure", Err: err}
		}
		return knowledge.FetchedDocument{}, err
	}
	return knowledge.FetchedDocument{URL: result.URL, Content: result.Content, FetchedAt: result.FetchedAt, ETag: result.ETag, LastModified: result.LastModified, NotModified: result.NotModified}, nil
}

func newRefreshCommand(deps Dependencies) *cobra.Command {
	var fetch, asJSON, revalidate, dryRun bool
	cmd := &cobra.Command{Use: "refresh <id>", Short: "Check a pack's deterministic sources (network requires --fetch)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) (returnErr error) {
		if revalidate && !fetch {
			return fmt.Errorf("--revalidate requires --fetch")
		}
		cwd, err := deps.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		root, err := deps.ResolveRoot(cwd)
		if err != nil {
			return err
		}
		stateDir, err := deps.StateDir(cmd.Context(), root)
		if err != nil {
			return err
		}
		registry, err := knowledge.LoadWithState(root, stateDir)
		if err != nil {
			return err
		}
		pack, ok := registry.Find(args[0])
		if !ok {
			return fmt.Errorf("unknown knowledge pack %q", args[0])
		}
		lock, err := localstate.Acquire(stateDir, "knowledge-refresh")
		if err != nil {
			return err
		}
		defer func() {
			if err := lock.Release(); returnErr == nil && err != nil {
				returnErr = err
			}
		}()
		if !dryRun {
			if err := knowledge.SyncRegistry(registry, stateDir); err != nil {
				return err
			}
		}
		repository, err := deps.InspectRepository(cmd.Context(), root)
		if err != nil {
			return err
		}
		packReference, err := activePackReference(stateDir, pack)
		if err != nil {
			return err
		}
		service := knowledge.RefreshService{Evidence: evidencestore.Open(stateDir), Audit: audit.Open(stateDir), Verification: verificationstore.Open(stateDir), Provenance: provenance.Open(stateDir), Fetcher: extractorFetcher{extract: deps.Extract, extractConditional: deps.ExtractConditional}, Repository: provenance.RepositoryContext{Root: repository.Root, GitCommit: repository.Head, GitBranch: repository.Branch}, PackReference: &packReference}
		report, refreshErr := service.RefreshWithOptions(cmd.Context(), pack, knowledge.RefreshOptions{Fetch: fetch, DryRun: dryRun, Revalidate: revalidate})
		if asJSON {
			if err := writeJSON(cmd, report); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Pack: %s\nFetch enabled: %t\nDry run: %t\nChecked: %d\nChanged: %d\nUnchanged: %d\nFailed: %d\nNew revisions: %d\nAffected verifications: %d\nRevalidated: %d\n", report.PackID, report.FetchEnabled, report.DryRun, report.Checked, report.Changed, report.Unchanged, report.Failed, report.NewRevisions, report.AffectedVerifications, report.Revalidated); err != nil {
				return err
			}
			for _, result := range report.Results {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", result.Status, result.SourceID, result.URL)
			}
			for _, transition := range report.StatusChanges {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s -> %s\t%s\n", transition.ClaimID, transition.OldStatus, transition.NewStatus, transition.ArtifactPath)
			}
		}
		return refreshErr
	}}
	cmd.Flags().BoolVar(&fetch, "fetch", false, "allow guarded HTTPS retrieval")
	cmd.Flags().BoolVar(&revalidate, "revalidate", false, "revalidate affected persisted claims after a source change")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check and report without mutating local state")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func activePackReference(stateDir string, pack knowledge.Pack) (provenance.KnowledgePackReference, error) {
	reference := provenance.KnowledgePackReference{ID: pack.ID, Origin: string(pack.Origin)}
	if pack.Origin != knowledge.OriginManaged {
		return reference, nil
	}
	release, ok, err := distribution.OpenStore(stateDir).ActiveInstalledRelease(pack.ID)
	if err != nil {
		return reference, err
	}
	if !ok {
		return reference, fmt.Errorf("managed Pack %q has no active signed release metadata", pack.ID)
	}
	reference.Version, reference.Publisher, reference.PublisherKeyID, reference.PackageSHA256 = release.Version, release.Publisher, release.KeyID, release.PackageSHA256
	return reference, nil
}

func NewAuditCommand(deps Dependencies) *cobra.Command {
	deps = normalize(deps)
	command := &cobra.Command{Use: "audit", Short: "Inspect local SwipeNode audit events", Args: cobra.NoArgs}
	command.AddCommand(newAuditListCommand(deps), newAuditShowCommand(deps))
	return command
}

func auditStore(ctx context.Context, deps Dependencies) (*audit.Store, error) {
	cwd, err := deps.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	root, err := deps.ResolveRoot(cwd)
	if err != nil {
		return nil, err
	}
	dir, err := deps.StateDir(ctx, root)
	if err != nil {
		return nil, err
	}
	return audit.Open(dir), nil
}

func newAuditListCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "list", Short: "List newest audit events first", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := auditStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		events, err := store.List()
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, struct {
				SchemaVersion string        `json:"schema_version"`
				Events        []audit.Event `json:"events"`
			}{"swipenode.audit-list.v1", events})
		}
		for _, event := range events {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", event.ID, event.Timestamp, event.Type, event.PackID); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newAuditShowCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "show <event-id>", Short: "Show one audit event", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := auditStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		event, ok, err := store.Find(args[0])
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("unknown audit event %q", args[0])
		}
		if asJSON {
			return writeJSON(cmd, event)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nEvent: %s\nTimestamp: %s\nPack: %s\nSource: %s\nEvidence: %s\n", event.ID, event.Type, event.Timestamp, event.PackID, event.SourceID, event.EvidenceID)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
