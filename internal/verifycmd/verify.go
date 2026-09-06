// Package verifycmd constructs SwipeNode's repository verification command.
// It is independent of Entire; callers inject repository-root resolution when
// they need an integration-specific context.
package verifycmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/verification"
	"github.com/sirToby99/swipenode/internal/verificationstore"
	"github.com/spf13/cobra"
)

type ResolveRootFunc func(string) (string, error)

type Dependencies struct {
	Getwd                func() (string, error)
	ResolveRoot          ResolveRootFunc
	Lookup               verification.Lookup
	LoadRegistry         func(string) (*knowledge.Registry, error)
	StateDir             func(context.Context, string) (string, error)
	ProvenanceEnricher   provenance.Enricher
	ResolvePackReference func(string, knowledge.Pack) (provenance.KnowledgePackReference, error)
}

// DefaultDependencies provides standalone operation with ordinary Git root
// discovery and SwipeNode's guarded public-source lookup.
func DefaultDependencies() Dependencies {
	return Dependencies{
		Getwd:       os.Getwd,
		ResolveRoot: gitcontext.ResolveRoot,
		Lookup:      verification.SwipeNodeLookup{},
		LoadRegistry: func(root string) (*knowledge.Registry, error) {
			stateDir, err := gitcontext.StateDir(context.Background(), root)
			if err != nil {
				return nil, err
			}
			return knowledge.LoadWithState(root, stateDir)
		},
		StateDir:             gitcontext.StateDir,
		ResolvePackReference: resolvePackReference,
	}
}

func normalizeDependencies(deps Dependencies) Dependencies {
	defaults := DefaultDependencies()
	if deps.Getwd == nil {
		deps.Getwd = defaults.Getwd
	}
	if deps.ResolveRoot == nil {
		deps.ResolveRoot = defaults.ResolveRoot
	}
	if deps.Lookup == nil {
		deps.Lookup = defaults.Lookup
	}
	if deps.LoadRegistry == nil {
		deps.LoadRegistry = defaults.LoadRegistry
	}
	if deps.StateDir == nil {
		deps.StateDir = defaults.StateDir
	}
	if deps.ResolvePackReference == nil {
		deps.ResolvePackReference = defaults.ResolvePackReference
	}
	return deps
}

// NewCommand returns a fresh verify command with the complete standalone and
// plugin flag surface. The verification engine and output contract are shared
// by every entry point.
func NewCommand(deps Dependencies) *cobra.Command {
	deps = normalizeDependencies(deps)
	var staged bool
	var ref string
	var asJSON bool
	var fetch bool
	var knowledgePack string
	var recordProvenance bool
	command := &cobra.Command{
		Use:   "verify",
		Short: "Find external technical assumptions in a Git diff",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if staged && strings.TrimSpace(ref) != "" {
				return fmt.Errorf("--staged and --ref are mutually exclusive")
			}
			cwd, err := deps.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			root, err := deps.ResolveRoot(cwd)
			if err != nil {
				return err
			}
			var selectedPack *knowledge.Pack
			if strings.TrimSpace(knowledgePack) != "" {
				registry, err := deps.LoadRegistry(root)
				if err != nil {
					return err
				}
				pack, ok := registry.Find(strings.TrimSpace(knowledgePack))
				if !ok {
					return fmt.Errorf("unknown knowledge pack %q", strings.TrimSpace(knowledgePack))
				}
				selectedPack = &pack
			}
			diff, scope, err := gitcontext.Diff(cmd.Context(), root, staged, strings.TrimSpace(ref))
			if err != nil {
				return err
			}
			evidence, err := verification.LoadCatalog(root)
			if err != nil {
				return err
			}
			if selectedPack != nil {
				evidence = applyPackPolicy(*selectedPack, evidence)
			}
			options := verification.Options{EvidenceMode: verification.EvidenceModeOffline, Evidence: evidence}
			if fetch {
				options.EvidenceMode = verification.EvidenceModeNetwork
				options.Lookup = deps.Lookup
				if selectedPack != nil {
					options.Lookup = packLookup{pack: *selectedPack, base: deps.Lookup}
				}
			}
			report := verification.Analyze(cmd.Context(), diff, scope, options)
			if !staged && strings.TrimSpace(ref) == "" && gitcontext.HasUntrackedFiles(cmd.Context(), root) {
				report.Warnings = append(report.Warnings, "Untracked files are not included in the default git diff; add or stage them before verification.")
			}
			stateDir, err := deps.StateDir(cmd.Context(), root)
			if err != nil {
				return err
			}
			packID := ""
			if selectedPack != nil {
				packID = selectedPack.ID
			}
			auditor := audit.Open(stateDir)
			verificationID, err := audit.VerificationID(report)
			if err != nil {
				return err
			}
			evidenceTrace := newEvidenceRecording()
			if report.EvidenceMode == verification.EvidenceModeNetwork || recordProvenance || selectedPack != nil {
				evidenceTrace, err = recordEvidence(stateDir, auditor, report, selectedPack, verificationID)
				if err != nil {
					return fmt.Errorf("record verification evidence: %w", err)
				}
			}
			auditTrace, err := auditor.RecordVerificationWithEvents(report, packID)
			if err != nil {
				return fmt.Errorf("record verification audit event: %w", err)
			}
			var verificationRecord verificationstore.Record
			if selectedPack != nil || recordProvenance {
				verificationRecord, err = persistVerification(cmd.Context(), stateDir, root, report, packID, verificationID, evidenceTrace)
				if err != nil {
					return fmt.Errorf("record verification version: %w", err)
				}
			}
			if recordProvenance {
				var packReference *provenance.KnowledgePackReference
				if selectedPack != nil {
					resolved, err := deps.ResolvePackReference(stateDir, *selectedPack)
					if err != nil {
						return fmt.Errorf("resolve knowledge Pack provenance: %w", err)
					}
					packReference = &resolved
				}
				record, err := buildProvenance(cmd.Context(), root, report, selectedPack, packReference, verificationID, verificationRecord.ID, evidenceTrace, auditTrace, deps.ProvenanceEnricher)
				if err != nil {
					return fmt.Errorf("build engineering provenance: %w", err)
				}
				stored, err := provenance.Open(stateDir).Append(record)
				if err != nil {
					return fmt.Errorf("record engineering provenance: %w", err)
				}
				report.ProvenanceID = stored.ProvenanceID
			}
			if asJSON {
				return writeIndentedJSON(cmd, report)
			}
			return writeHuman(cmd, report)
		},
	}
	command.Flags().BoolVar(&staged, "staged", false, "inspect the staged diff")
	command.Flags().StringVar(&ref, "ref", "", "inspect the diff from this Git commit or ref")
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable machine-readable JSON")
	command.Flags().BoolVar(&fetch, "fetch", false, "allow guarded HTTP(S) retrieval for repository-derived evidence URLs")
	command.Flags().StringVar(&knowledgePack, "knowledge-pack", "", "constrain authority to one reviewed Knowledge Pack")
	command.Flags().BoolVar(&recordProvenance, "record-provenance", false, "persist immutable engineering provenance for this verification")
	return command
}

type evidenceRecording struct {
	EvidenceIDs      []string
	AuditEventIDs    []string
	ClaimEvidenceIDs map[string][]string
	SourceRevisions  []provenance.SourceRevision
	EvidenceRecords  map[string]evidencestore.Record
}

func newEvidenceRecording() evidenceRecording {
	return evidenceRecording{ClaimEvidenceIDs: map[string][]string{}, EvidenceRecords: map[string]evidencestore.Record{}}
}

func claimKey(claim verification.Claim) string {
	return fmt.Sprintf("%s:%d", claim.Location.Path, claim.Location.Line)
}

func recordEvidence(stateDir string, auditor *audit.Store, report verification.Report, pack *knowledge.Pack, verificationID string) (evidenceRecording, error) {
	trace := newEvidenceRecording()
	store := evidencestore.Open(stateDir)
	for _, claim := range report.Claims {
		for _, item := range claim.Evidence {
			if item.Content == "" {
				continue
			}
			canonicalURL := item.URLReference
			if canonicalURL == "" {
				canonicalURL = item.Source
			}
			if canonicalURL == "" {
				continue
			}
			packID, sourceID, owner, sourceType, revision := "", "source-"+evidencestore.ContentHash(canonicalURL)[:12], "unreviewed", item.SourceType, item.VersionDate
			if sourceType == "" {
				sourceType = "public_html"
			}
			if pack != nil {
				if source, ok := pack.SourceForURL(canonicalURL); ok {
					packID, sourceID, owner, sourceType = pack.ID, source.ID, pack.Owner, source.SourceType
					if revision == "" {
						revision = source.Revision
					}
				}
			}
			retrievedAt := item.RetrievedAt
			if retrievedAt == "" {
				retrievedAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			hash := evidencestore.ContentHash(item.Content)
			prior, exists, err := store.SourceState(packID, sourceID, canonicalURL)
			if err != nil {
				return trace, err
			}
			if exists && prior.ContentSHA256 == hash && prior.DocumentRevision == revision {
				existingRecord, found, err := store.Find(prior.CurrentEvidenceID)
				if err != nil {
					return trace, err
				}
				if !found || existingRecord.ContentSHA256 != hash || existingRecord.SourceID != sourceID {
					return trace, fmt.Errorf("source state references missing or inconsistent evidence %q", prior.CurrentEvidenceID)
				}
				prior.LastChecked, prior.Health, prior.Status = retrievedAt, "healthy", "unchanged"
				if err := store.UpdateSourceState(prior); err != nil {
					return trace, err
				}
				checked, err := auditor.Append(audit.Event{Type: audit.SourceChecked, PackID: packID, SourceID: sourceID, CanonicalURL: canonicalURL, OldHash: hash, NewHash: hash, OldRevision: revision, NewRevision: revision})
				if err != nil {
					return trace, err
				}
				trace.AuditEventIDs = append(trace.AuditEventIDs, checked.ID)
				trace.EvidenceIDs = append(trace.EvidenceIDs, prior.CurrentEvidenceID)
				trace.ClaimEvidenceIDs[claimKey(claim)] = append(trace.ClaimEvidenceIDs[claimKey(claim)], prior.CurrentEvidenceID)
				trace.SourceRevisions = append(trace.SourceRevisions, provenance.SourceRevision{EvidenceID: prior.CurrentEvidenceID, SourceID: sourceID, Revision: revision, ContentSHA256: hash})
				trace.EvidenceRecords[existingRecord.ID] = existingRecord
				continue
			}
			checked, err := auditor.Append(audit.Event{Type: audit.SourceChecked, PackID: packID, SourceID: sourceID, CanonicalURL: canonicalURL, OldHash: prior.ContentSHA256, NewHash: hash, OldRevision: prior.DocumentRevision, NewRevision: revision})
			if err != nil {
				return trace, err
			}
			trace.AuditEventIDs = append(trace.AuditEventIDs, checked.ID)
			record, err := store.Insert(evidencestore.Record{PackID: packID, SourceID: sourceID, CanonicalURL: canonicalURL, Owner: owner, SourceType: sourceType, RetrievedAt: retrievedAt, DocumentRevision: revision, ContentSHA256: hash, ExtractorVersion: "swipenode.extractor.v1", ExtractedFact: item.RetrievedFact, Verification: evidencestore.VerificationRelationship{VerificationID: verificationID, ClaimID: fmt.Sprintf("%s:%d", claim.Location.Path, claim.Location.Line)}})
			if err != nil {
				return trace, err
			}
			if err := store.UpdateSourceState(evidencestore.SourceState{PackID: packID, SourceID: sourceID, CanonicalURL: canonicalURL, LastChecked: retrievedAt, LastChanged: retrievedAt, CurrentEvidenceID: record.ID, ContentSHA256: hash, DocumentRevision: revision, Health: "healthy", Status: "changed"}); err != nil {
				return trace, err
			}
			if exists {
				changed, err := auditor.Append(audit.Event{Type: audit.SourceChanged, PackID: packID, SourceID: sourceID, EvidenceID: record.ID, CanonicalURL: canonicalURL, OldHash: prior.ContentSHA256, NewHash: hash, OldRevision: prior.DocumentRevision, NewRevision: revision})
				if err != nil {
					return trace, err
				}
				trace.AuditEventIDs = append(trace.AuditEventIDs, changed.ID)
			}
			created, err := auditor.Append(audit.Event{Type: audit.EvidenceCreated, PackID: packID, SourceID: sourceID, EvidenceID: record.ID, VerificationID: verificationID, CanonicalURL: canonicalURL, NewHash: hash, NewRevision: revision})
			if err != nil {
				return trace, err
			}
			trace.AuditEventIDs = append(trace.AuditEventIDs, created.ID)
			trace.EvidenceIDs = append(trace.EvidenceIDs, record.ID)
			trace.ClaimEvidenceIDs[claimKey(claim)] = append(trace.ClaimEvidenceIDs[claimKey(claim)], record.ID)
			trace.SourceRevisions = append(trace.SourceRevisions, provenance.SourceRevision{EvidenceID: record.ID, SourceID: sourceID, Revision: revision, ContentSHA256: hash})
			trace.EvidenceRecords[record.ID] = record
		}
	}
	return trace, nil
}

func persistVerification(ctx context.Context, stateDir, root string, report verification.Report, packID, reportID string, evidenceTrace evidenceRecording) (verificationstore.Record, error) {
	repository, err := gitcontext.Inspect(ctx, root)
	if err != nil {
		return verificationstore.Record{}, err
	}
	record := verificationstore.Record{
		RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), ReportID: reportID, PackID: packID, Scope: report.Scope,
		Repository: verificationstore.Repository{Root: repository.Root, GitCommit: repository.Head, GitBranch: repository.Branch}, Artifacts: append([]string(nil), report.ChangedFiles...),
	}
	for _, claim := range report.Claims {
		storedClaim := verificationstore.Claim{ArtifactPath: claim.Location.Path, Line: claim.Location.Line, Category: claim.Category, Statement: claim.Statement, Status: string(claim.VerificationStatus), Confidence: claim.Confidence, Reason: claim.Reason}
		ids := evidenceTrace.ClaimEvidenceIDs[claimKey(claim)]
		for index, id := range ids {
			evidenceRecord, ok := evidenceTrace.EvidenceRecords[id]
			if !ok {
				continue
			}
			ref := verificationstore.EvidenceReference{EvidenceID: id, PackID: evidenceRecord.PackID, SourceID: evidenceRecord.SourceID, CanonicalURL: evidenceRecord.CanonicalURL, SourceType: evidenceRecord.SourceType, Revision: evidenceRecord.DocumentRevision, ContentSHA256: evidenceRecord.ContentSHA256, RetrievedFact: evidenceRecord.ExtractedFact}
			if index < len(claim.Evidence) {
				ref.Authority, ref.Confidence = claim.Evidence[index].Authority, claim.Evidence[index].Confidence
				if ref.RetrievedFact == "" {
					ref.RetrievedFact = claim.Evidence[index].RetrievedFact
				}
			}
			storedClaim.Evidence = append(storedClaim.Evidence, ref)
		}
		record.Claims = append(record.Claims, storedClaim)
	}
	return verificationstore.Open(stateDir).Append(record)
}

func buildProvenance(ctx context.Context, root string, report verification.Report, pack *knowledge.Pack, packReference *provenance.KnowledgePackReference, verificationID, verificationRecordID string, evidenceTrace evidenceRecording, auditTrace audit.VerificationTrace, enricher provenance.Enricher) (provenance.EngineeringProvenanceRecord, error) {
	repository, err := gitcontext.Inspect(ctx, root)
	if err != nil {
		return provenance.EngineeringProvenanceRecord{}, err
	}
	statuses := map[string]int{}
	artifacts := make([]provenance.Artifact, 0, len(report.ChangedFiles))
	claims := make([]provenance.ClaimReference, 0, len(report.Claims))
	for _, path := range report.ChangedFiles {
		artifacts = append(artifacts, provenance.Artifact{Path: path, Kind: "file"})
	}
	for _, claim := range report.Claims {
		status := string(claim.VerificationStatus)
		statuses[status]++
		claims = append(claims, provenance.ClaimReference{ArtifactPath: claim.Location.Path, Line: claim.Location.Line, Category: claim.Category, Statement: claim.Statement, Status: status, EvidenceIDs: evidenceTrace.ClaimEvidenceIDs[claimKey(claim)]})
	}
	packIDs := []string{}
	packReferences := []provenance.KnowledgePackReference{}
	if pack != nil {
		packIDs = append(packIDs, pack.ID)
		if packReference != nil {
			packReferences = append(packReferences, *packReference)
		}
	}
	adapter := "swipenode"
	var external *provenance.ExternalContext
	if enricher != nil {
		enrichment := enricher.Enrich(ctx, root)
		if enrichment.Adapter != "" {
			adapter = enrichment.Adapter
		}
		external = enrichment.ExternalContext
	}
	return provenance.EngineeringProvenanceRecord{
		Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
		Repository:       provenance.RepositoryContext{Root: repository.Root, GitCommit: repository.Head, GitBranch: repository.Branch},
		Artifacts:        artifacts,
		Verification:     provenance.VerificationReference{ID: verificationID, RecordID: verificationRecordID, SchemaVersion: report.SchemaVersion, Scope: report.Scope, Clean: report.Clean, Statuses: statuses},
		EvidenceIDs:      evidenceTrace.EvidenceIDs,
		AuditEventIDs:    append(evidenceTrace.AuditEventIDs, auditTrace.EventIDs...),
		KnowledgePackIDs: packIDs,
		KnowledgePacks:   packReferences,
		Claims:           claims,
		SourceRevisions:  evidenceTrace.SourceRevisions,
		Adapter:          adapter,
		ExternalContext:  external,
	}, nil
}

func resolvePackReference(stateDir string, pack knowledge.Pack) (provenance.KnowledgePackReference, error) {
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
	reference.Version = release.Version
	reference.Publisher = release.Publisher
	reference.PublisherKeyID = release.KeyID
	reference.PackageSHA256 = release.PackageSHA256
	return reference, nil
}

type packLookup struct {
	pack knowledge.Pack
	base verification.Lookup
}

func (lookup packLookup) Lookup(ctx context.Context, sourceURL string) (verification.Evidence, error) {
	evidence, err := lookup.base.Lookup(ctx, sourceURL)
	if err != nil {
		return verification.Evidence{}, err
	}
	return applyPackPolicyOne(lookup.pack, evidence), nil
}

func applyPackPolicy(pack knowledge.Pack, evidence []verification.Evidence) []verification.Evidence {
	result := make([]verification.Evidence, len(evidence))
	for i := range evidence {
		result[i] = applyPackPolicyOne(pack, evidence[i])
	}
	return result
}

func applyPackPolicyOne(pack knowledge.Pack, evidence verification.Evidence) verification.Evidence {
	reference := evidence.URLReference
	if reference == "" {
		reference = evidence.Source
	}
	source, allowed := pack.SourceForURL(reference)
	if !allowed {
		evidence.Authority = "unknown"
		return evidence
	}
	evidence.Authority = "authoritative"
	evidence.SourceType = source.SourceType
	if evidence.VersionDate == "" {
		evidence.VersionDate = source.Revision
	}
	return evidence
}

func writeIndentedJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeHuman(cmd *cobra.Command, report verification.Report) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintln(out, "Evidence mode:", report.EvidenceMode); err != nil {
		return err
	}
	if report.Clean {
		_, err := fmt.Fprintf(out, "No changes in %s diff to verify.\n", report.Scope)
		for _, warning := range report.Warnings {
			_, _ = fmt.Fprintln(out, "WARN:", warning)
		}
		if report.ProvenanceID != "" {
			_, _ = fmt.Fprintln(out, "Provenance:", report.ProvenanceID)
		}
		return err
	}
	if _, err := fmt.Fprintf(out, "Scope: %s\nChanged files: %d\nCandidate external claims: %d\n", report.Scope, len(report.ChangedFiles), len(report.Claims)); err != nil {
		return err
	}
	for _, claim := range report.Claims {
		if _, err := fmt.Fprintf(out, "\n%s %s:%d [%s, confidence %.2f]\n%s\nReason: %s\n", claim.VerificationStatus, claim.Location.Path, claim.Location.Line, claim.Category, claim.Confidence, claim.Statement, claim.Reason); err != nil {
			return err
		}
		for _, evidence := range claim.Evidence {
			timing := ""
			if evidence.VersionDate != "" {
				timing += " version/date=" + evidence.VersionDate
			}
			if evidence.RetrievedAt != "" {
				timing += " retrieved_at=" + evidence.RetrievedAt
			}
			if _, err := fmt.Fprintf(out, "  Evidence: %s (type=%s authority=%s relevance=%.2f confidence=%.2f%s)\n  Fact: %s\n  Assessment: %s\n", evidence.Source, evidence.SourceType, evidence.Authority, evidence.Relevance, evidence.Confidence, timing, evidence.RetrievedFact, evidence.Reason); err != nil {
				return err
			}
		}
	}
	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintln(out, "WARN:", warning)
	}
	if report.ProvenanceID != "" {
		_, _ = fmt.Fprintln(out, "Provenance:", report.ProvenanceID)
	}
	return nil
}
