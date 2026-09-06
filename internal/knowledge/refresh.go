package knowledge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/verification"
	"github.com/sirToby99/swipenode/internal/verificationstore"
)

type FetchedDocument struct {
	URL          string
	Content      string
	FetchedAt    string
	ETag         string
	LastModified string
	NotModified  bool
}

type FetchCondition struct {
	ETag         string
	LastModified string
}

type Fetcher interface {
	Fetch(context.Context, string) (FetchedDocument, error)
}

type ConditionalFetcher interface {
	FetchConditional(context.Context, string, FetchCondition) (FetchedDocument, error)
}

type FetchFailure struct {
	Kind string
	Err  error
}

func (failure FetchFailure) Error() string {
	if failure.Err == nil {
		return failure.Kind
	}
	return failure.Err.Error()
}

func (failure FetchFailure) Unwrap() error { return failure.Err }

type RefreshOptions struct {
	Fetch      bool
	DryRun     bool
	Revalidate bool
}

type StatusTransition struct {
	ClaimID         string `json:"claim_id"`
	ArtifactPath    string `json:"artifact_path"`
	OldVerification string `json:"old_verification_id"`
	NewVerification string `json:"new_verification_id,omitempty"`
	OldStatus       string `json:"old_status"`
	NewStatus       string `json:"new_status"`
}

type RefreshResult struct {
	SourceID      string `json:"source_id"`
	URL           string `json:"url"`
	Status        string `json:"status"`
	CheckMethod   string `json:"check_method,omitempty"`
	EvidenceID    string `json:"evidence_id,omitempty"`
	OldEvidenceID string `json:"old_evidence_id,omitempty"`
	OldHash       string `json:"old_hash,omitempty"`
	NewHash       string `json:"new_hash,omitempty"`
	OldRevision   string `json:"old_revision,omitempty"`
	NewRevision   string `json:"new_revision,omitempty"`
	FailureKind   string `json:"failure_kind,omitempty"`
	WouldChange   bool   `json:"would_change,omitempty"`
	Error         string `json:"error,omitempty"`
}

type RefreshReport struct {
	SchemaVersion         string             `json:"schema_version"`
	PackID                string             `json:"pack_id"`
	FetchEnabled          bool               `json:"fetch_enabled"`
	DryRun                bool               `json:"dry_run"`
	RevalidationEnabled   bool               `json:"revalidation_enabled"`
	Checked               int                `json:"checked"`
	Changed               int                `json:"changed"`
	Unchanged             int                `json:"unchanged"`
	Failed                int                `json:"failed"`
	NewRevisions          int                `json:"new_revisions"`
	AffectedVerifications int                `json:"affected_verifications"`
	Revalidated           int                `json:"revalidated"`
	StatusChanges         []StatusTransition `json:"status_changes"`
	ProvenanceIDs         []string           `json:"provenance_ids"`
	Results               []RefreshResult    `json:"results"`
}

type RefreshService struct {
	Evidence      *evidencestore.Store
	Audit         *audit.Store
	Verification  *verificationstore.Store
	Provenance    *provenance.Store
	Fetcher       Fetcher
	Repository    provenance.RepositoryContext
	Enricher      provenance.Enricher
	PackReference *provenance.KnowledgePackReference
	Now           func() time.Time
}

func (service RefreshService) Refresh(ctx context.Context, pack Pack, fetch bool) (RefreshReport, error) {
	return service.RefreshWithOptions(ctx, pack, RefreshOptions{Fetch: fetch})
}

func (service RefreshService) RefreshWithOptions(ctx context.Context, pack Pack, options RefreshOptions) (RefreshReport, error) {
	report := RefreshReport{SchemaVersion: "swipenode.knowledge-refresh.v2", PackID: pack.ID, FetchEnabled: options.Fetch, DryRun: options.DryRun, RevalidationEnabled: options.Revalidate, Results: []RefreshResult{}, StatusChanges: []StatusTransition{}, ProvenanceIDs: []string{}}
	sources, err := pack.ResolveSources()
	if err != nil {
		return report, err
	}
	resolvedIDs := map[string]bool{}
	for _, source := range sources {
		resolvedIDs[source.SourceID] = true
	}
	for _, source := range pack.Sources {
		if !resolvedIDs[source.ID] {
			report.Results = append(report.Results, RefreshResult{SourceID: source.ID, URL: source.CanonicalURL, Status: "unresolved", Error: "source policy does not define a deterministic fetch endpoint"})
		}
	}
	if !options.Fetch {
		for _, source := range sources {
			report.Results = append(report.Results, RefreshResult{SourceID: source.SourceID, URL: source.URL, Status: "offline"})
		}
		return report, nil
	}
	if service.Fetcher == nil || service.Evidence == nil || service.Audit == nil {
		return report, fmt.Errorf("refresh fetcher and stores are required")
	}
	if options.Revalidate && service.Verification == nil {
		return report, fmt.Errorf("verification store is required for revalidation")
	}
	now := service.Now
	if now == nil {
		now = time.Now
	}
	for _, source := range sources {
		result := RefreshResult{SourceID: source.SourceID, URL: source.URL}
		prior, exists, stateErr := service.Evidence.SourceState(pack.ID, source.SourceID, source.URL)
		if stateErr != nil {
			return report, stateErr
		}
		result.OldHash, result.OldRevision, result.OldEvidenceID = prior.ContentSHA256, prior.DocumentRevision, prior.CurrentEvidenceID
		checkedAt := now().UTC().Format(time.RFC3339Nano)
		if !pack.AllowsURL(source.URL) {
			if err := service.recordFailure(&report, &result, prior, exists, checkedAt, "authority_failure", errors.New("resolved URL is outside pack trust policy"), options.DryRun); err != nil {
				return report, err
			}
			report.Results = append(report.Results, result)
			continue
		}
		condition := FetchCondition{}
		// An explicit revision policy change needs a body even if HTTP metadata
		// says the old representation is cache-valid.
		if pack.Freshness.CheckStrategy == "http_metadata_and_content_hash" && (!exists || prior.DocumentRevision == source.Revision || source.Revision == "") {
			condition = FetchCondition{ETag: prior.ETag, LastModified: prior.LastModified}
		}
		document, fetchErr := service.fetch(ctx, source.URL, condition)
		if fetchErr != nil {
			if err := service.recordFailure(&report, &result, prior, exists, checkedAt, classifyFetchFailure(fetchErr), fetchErr, options.DryRun); err != nil {
				return report, err
			}
			report.Results = append(report.Results, result)
			continue
		}
		report.Checked++
		if parsed, parseErr := time.Parse(time.RFC3339Nano, document.FetchedAt); parseErr == nil {
			checkedAt = parsed.UTC().Format(time.RFC3339Nano)
		}
		if document.NotModified {
			if !exists {
				if err := service.recordFailure(&report, &result, prior, false, checkedAt, "invalid_content", errors.New("server returned 304 without prior source state"), options.DryRun); err != nil {
					return report, err
				}
				report.Results = append(report.Results, result)
				continue
			}
			result.Status, result.CheckMethod, result.EvidenceID, result.NewHash, result.NewRevision = "unchanged", "http_304", prior.CurrentEvidenceID, prior.ContentSHA256, prior.DocumentRevision
			report.Unchanged++
			if !options.DryRun {
				prior.LastChecked, prior.Health, prior.Status = checkedAt, "healthy", "unchanged"
				if document.ETag != "" {
					prior.ETag = document.ETag
				}
				if document.LastModified != "" {
					prior.LastModified = document.LastModified
				}
				if err := service.Evidence.UpdateSourceState(prior); err != nil {
					return report, err
				}
				if err := service.appendUnchangedEvents(pack.ID, source, prior, "http_304"); err != nil {
					return report, err
				}
			}
			report.Results = append(report.Results, result)
			continue
		}
		document.Content = strings.TrimSpace(document.Content)
		if document.Content == "" {
			if err := service.recordFailure(&report, &result, prior, exists, checkedAt, "invalid_content", errors.New("source returned no usable technical content"), options.DryRun); err != nil {
				return report, err
			}
			report.Results = append(report.Results, result)
			continue
		}
		hash := evidencestore.ContentHash(document.Content)
		revision := detectedRevision(pack.Freshness, source, hash)
		result.NewHash, result.NewRevision, result.CheckMethod = hash, revision, "content_sha256"
		if exists && prior.ContentSHA256 == hash && prior.DocumentRevision != revision {
			result.CheckMethod = "source_revision"
		}
		if exists && prior.ContentSHA256 == hash && prior.DocumentRevision == revision {
			result.Status, result.EvidenceID = "unchanged", prior.CurrentEvidenceID
			report.Unchanged++
			if !options.DryRun {
				prior.LastChecked, prior.ETag, prior.LastModified, prior.Health, prior.Status = checkedAt, document.ETag, document.LastModified, "healthy", "unchanged"
				if err := service.Evidence.UpdateSourceState(prior); err != nil {
					return report, err
				}
				if err := service.appendUnchangedEvents(pack.ID, source, prior, "content_sha256"); err != nil {
					return report, err
				}
			}
			report.Results = append(report.Results, result)
			continue
		}
		result.Status, result.WouldChange = "changed", options.DryRun
		report.Changed++
		report.NewRevisions++
		newRecord := evidencestore.Record{PackID: pack.ID, SourceID: source.SourceID, CanonicalURL: source.URL, Owner: source.Owner, SourceType: source.Type, RetrievedAt: checkedAt, DocumentRevision: revision, ETag: document.ETag, LastModified: document.LastModified, ContentSHA256: hash, ExtractorVersion: "swipenode.extractor.v1"}
		lifecycleEventIDs := []string{}
		if !options.DryRun {
			checkedEvent, eventErr := service.Audit.Append(audit.Event{Type: audit.SourceChecked, PackID: pack.ID, SourceID: source.SourceID, CanonicalURL: source.URL, OldHash: prior.ContentSHA256, NewHash: hash, OldRevision: prior.DocumentRevision, NewRevision: revision, CheckMethod: result.CheckMethod})
			if eventErr != nil {
				return report, eventErr
			}
			lifecycleEventIDs = append(lifecycleEventIDs, checkedEvent.ID)
			stored, insertErr := service.Evidence.Insert(newRecord)
			if insertErr != nil {
				return report, insertErr
			}
			newRecord, result.EvidenceID = stored, stored.ID
			state := evidencestore.SourceState{PackID: pack.ID, SourceID: source.SourceID, CanonicalURL: source.URL, LastChecked: checkedAt, LastChanged: checkedAt, CurrentEvidenceID: stored.ID, ContentSHA256: hash, DocumentRevision: revision, ETag: document.ETag, LastModified: document.LastModified, Health: "healthy", Status: "changed"}
			if err := service.Evidence.UpdateSourceState(state); err != nil {
				return report, err
			}
			if exists {
				changedEvent, err := service.Audit.Append(audit.Event{Type: audit.SourceChanged, PackID: pack.ID, SourceID: source.SourceID, EvidenceID: stored.ID, OldEvidenceID: prior.CurrentEvidenceID, NewEvidenceID: stored.ID, CanonicalURL: source.URL, OldHash: prior.ContentSHA256, NewHash: hash, OldRevision: prior.DocumentRevision, NewRevision: revision})
				if err != nil {
					return report, err
				}
				lifecycleEventIDs = append(lifecycleEventIDs, changedEvent.ID)
			}
			createdEvent, err := service.Audit.Append(audit.Event{Type: audit.EvidenceCreated, PackID: pack.ID, SourceID: source.SourceID, EvidenceID: stored.ID, NewEvidenceID: stored.ID, CanonicalURL: source.URL, NewHash: hash, NewRevision: revision})
			if err != nil {
				return report, err
			}
			lifecycleEventIDs = append(lifecycleEventIDs, createdEvent.ID)
		}
		if service.Verification != nil {
			dependents, dependencyErr := service.Verification.LatestDependents(pack.ID, source.SourceID)
			if dependencyErr != nil {
				return report, dependencyErr
			}
			report.AffectedVerifications += len(dependents)
			if options.Revalidate {
				if err := service.revalidate(ctx, pack, source, prior, newRecord, document, dependents, lifecycleEventIDs, options.DryRun, &report); err != nil {
					return report, err
				}
			}
		}
		report.Results = append(report.Results, result)
	}
	if report.Failed > 0 {
		return report, fmt.Errorf("%d source refreshes failed", report.Failed)
	}
	return report, nil
}

func (service RefreshService) fetch(ctx context.Context, rawURL string, condition FetchCondition) (FetchedDocument, error) {
	if conditional, ok := service.Fetcher.(ConditionalFetcher); ok {
		return conditional.FetchConditional(ctx, rawURL, condition)
	}
	return service.Fetcher.Fetch(ctx, rawURL)
}

func detectedRevision(policy FreshnessPolicy, source ResolvedSource, hash string) string {
	if source.Revision != "" {
		return source.Revision
	}
	if policy.RevisionStrategy == "content_hash" {
		return "sha256:" + hash
	}
	return ""
}

func classifyFetchFailure(err error) string {
	var failure FetchFailure
	if errors.As(err, &failure) && failure.Kind != "" {
		return failure.Kind
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout"
	}
	return "network_failure"
}

func (service RefreshService) recordFailure(report *RefreshReport, result *RefreshResult, prior evidencestore.SourceState, exists bool, checkedAt, kind string, err error, dryRun bool) error {
	result.Status, result.FailureKind, result.Error = "failed", kind, err.Error()
	report.Failed++
	if dryRun {
		return nil
	}
	state := prior
	if !exists {
		state = evidencestore.SourceState{PackID: report.PackID, SourceID: result.SourceID, CanonicalURL: result.URL}
	}
	state.LastChecked, state.Health, state.Status = checkedAt, "unhealthy", kind
	if err := service.Evidence.UpdateSourceState(state); err != nil {
		return err
	}
	_, err = service.Audit.Append(audit.Event{Type: audit.SourceChecked, PackID: report.PackID, SourceID: result.SourceID, CanonicalURL: result.URL, OldHash: prior.ContentSHA256, NewHash: prior.ContentSHA256, OldRevision: prior.DocumentRevision, NewRevision: prior.DocumentRevision, FailureKind: kind})
	return err
}

func (service RefreshService) appendUnchangedEvents(packID string, source ResolvedSource, state evidencestore.SourceState, method string) error {
	if _, err := service.Audit.Append(audit.Event{Type: audit.SourceChecked, PackID: packID, SourceID: source.SourceID, EvidenceID: state.CurrentEvidenceID, CanonicalURL: source.URL, OldHash: state.ContentSHA256, NewHash: state.ContentSHA256, OldRevision: state.DocumentRevision, NewRevision: state.DocumentRevision}); err != nil {
		return err
	}
	_, err := service.Audit.Append(audit.Event{Type: audit.SourceUnchanged, PackID: packID, SourceID: source.SourceID, EvidenceID: state.CurrentEvidenceID, CanonicalURL: source.URL, OldHash: state.ContentSHA256, NewHash: state.ContentSHA256, OldRevision: state.DocumentRevision, NewRevision: state.DocumentRevision, CheckMethod: method})
	return err
}

func (service RefreshService) revalidate(ctx context.Context, pack Pack, source ResolvedSource, prior evidencestore.SourceState, newEvidence evidencestore.Record, document FetchedDocument, dependents []verificationstore.Record, lifecycleEventIDs []string, dryRun bool, report *RefreshReport) error {
	for _, oldRecord := range dependents {
		startedEventID := ""
		if !dryRun {
			started, err := service.Audit.Append(audit.Event{Type: audit.RevalidationStarted, PackID: pack.ID, SourceID: source.SourceID, OldVerificationID: oldRecord.ID, OldEvidenceID: prior.CurrentEvidenceID, NewEvidenceID: newEvidence.ID, OldHash: prior.ContentSHA256, NewHash: newEvidence.ContentSHA256, OldRevision: prior.DocumentRevision, NewRevision: newEvidence.DocumentRevision})
			if err != nil {
				return err
			}
			startedEventID = started.ID
		}
		newRecord := verificationstore.Record{RecordedAt: service.timestamp(), ParentVerificationID: oldRecord.ID, PackID: pack.ID, Scope: "revalidation", Repository: oldRecord.Repository, Artifacts: append([]string(nil), oldRecord.Artifacts...)}
		transitions := []provenance.TruthTransition{}
		for _, oldClaim := range oldRecord.Claims {
			if !claimDependsOn(oldClaim, pack.ID, source.SourceID) {
				newRecord.Claims = append(newRecord.Claims, oldClaim)
				continue
			}
			oldSourceReference, ok := claimSourceReference(oldClaim, pack.ID, source.SourceID)
			if !ok {
				return fmt.Errorf("verification %s has an incomplete source dependency", oldRecord.ID)
			}
			newReference := verificationstore.EvidenceReference{EvidenceID: newEvidence.ID, PackID: pack.ID, SourceID: source.SourceID, CanonicalURL: source.URL, SourceType: source.Type, Authority: "authoritative", Confidence: 0.95, Revision: newEvidence.DocumentRevision, ContentSHA256: newEvidence.ContentSHA256}
			candidates := []verification.Evidence{{Source: source.URL, URLReference: source.URL, SourceType: source.Type, Authority: "authoritative", VersionDate: newEvidence.DocumentRevision, RetrievedAt: newEvidence.RetrievedAt, Confidence: 0.95, Content: document.Content}}
			references := []verificationstore.EvidenceReference{newReference}
			// Retain dependencies on other reviewed sources. This lets sequential
			// source changes in one refresh continue to find the same claim.
			for _, oldReference := range oldClaim.Evidence {
				if oldReference.PackID == pack.ID && oldReference.SourceID == source.SourceID {
					continue
				}
				candidates = append(candidates, verification.Evidence{Source: oldReference.CanonicalURL, URLReference: oldReference.CanonicalURL, SourceType: oldReference.SourceType, Authority: oldReference.Authority, VersionDate: oldReference.Revision, Confidence: oldReference.Confidence, Content: oldReference.RetrievedFact})
				references = append(references, oldReference)
			}
			updated := verification.RevalidateClaim(verification.Claim{Location: verification.Location{Path: oldClaim.ArtifactPath, Line: oldClaim.Line}, Category: oldClaim.Category, Statement: oldClaim.Statement}, candidates)
			for index := range references {
				if fact := retrievedFactForSource(updated, references[index].CanonicalURL); fact != "" {
					references[index].RetrievedFact = fact
				}
			}
			newClaim := verificationstore.Claim{ID: oldClaim.ID, ArtifactPath: oldClaim.ArtifactPath, Line: oldClaim.Line, Category: updated.Category, Statement: updated.Statement, Status: string(updated.VerificationStatus), Confidence: updated.Confidence, Reason: updated.Reason, Evidence: references}
			newRecord.Claims = append(newRecord.Claims, newClaim)
			if oldClaim.Status != newClaim.Status {
				transition := StatusTransition{ClaimID: oldClaim.ID, ArtifactPath: oldClaim.ArtifactPath, OldVerification: oldRecord.ID, OldStatus: oldClaim.Status, NewStatus: newClaim.Status}
				report.StatusChanges = append(report.StatusChanges, transition)
				transitions = append(transitions, provenance.TruthTransition{ClaimID: oldClaim.ID, ArtifactPath: oldClaim.ArtifactPath, OldVerificationID: oldRecord.ID, OldEvidenceID: oldSourceReference.EvidenceID, NewEvidenceID: newEvidence.ID, OldStatus: oldClaim.Status, NewStatus: newClaim.Status, OldHash: oldSourceReference.ContentSHA256, NewHash: newEvidence.ContentSHA256, OldRevision: oldSourceReference.Revision, NewRevision: newEvidence.DocumentRevision})
			}
		}
		newRecord.ReportID = revalidationReportID(newRecord)
		if dryRun {
			report.Revalidated++
			continue
		}
		stored, err := service.Verification.Append(newRecord)
		if err != nil {
			return err
		}
		report.Revalidated++
		for index := range report.StatusChanges {
			if report.StatusChanges[index].OldVerification == oldRecord.ID && report.StatusChanges[index].NewVerification == "" {
				report.StatusChanges[index].NewVerification = stored.ID
			}
		}
		revalidatedEvent, err := service.Audit.Append(audit.Event{Type: audit.VerificationRevalidated, PackID: pack.ID, SourceID: source.SourceID, OldVerificationID: oldRecord.ID, NewVerificationID: stored.ID, OldEvidenceID: prior.CurrentEvidenceID, NewEvidenceID: newEvidence.ID, OldHash: prior.ContentSHA256, NewHash: newEvidence.ContentSHA256, OldRevision: prior.DocumentRevision, NewRevision: newEvidence.DocumentRevision})
		if err != nil {
			return err
		}
		eventIDs := append(append([]string(nil), lifecycleEventIDs...), startedEventID, revalidatedEvent.ID)
		for index := range transitions {
			transitions[index].NewVerificationID = stored.ID
			changedEvent, err := service.Audit.Append(audit.Event{Type: audit.VerificationStatusChanged, PackID: pack.ID, SourceID: source.SourceID, OldVerificationID: oldRecord.ID, NewVerificationID: stored.ID, ClaimID: transitions[index].ClaimID, ArtifactPath: transitions[index].ArtifactPath, OldEvidenceID: transitions[index].OldEvidenceID, NewEvidenceID: transitions[index].NewEvidenceID, OldStatus: transitions[index].OldStatus, NewStatus: transitions[index].NewStatus, OldHash: transitions[index].OldHash, NewHash: transitions[index].NewHash, OldRevision: transitions[index].OldRevision, NewRevision: transitions[index].NewRevision})
			if err != nil {
				return err
			}
			eventIDs = append(eventIDs, changedEvent.ID)
		}
		if len(transitions) > 0 && service.Provenance != nil {
			provenanceID, err := service.recordRevalidationProvenance(ctx, pack, oldRecord, stored, prior, newEvidence, eventIDs, transitions)
			if err != nil {
				return err
			}
			report.ProvenanceIDs = append(report.ProvenanceIDs, provenanceID)
		}
	}
	return nil
}

func claimDependsOn(claim verificationstore.Claim, packID, sourceID string) bool {
	_, ok := claimSourceReference(claim, packID, sourceID)
	return ok
}

func claimSourceReference(claim verificationstore.Claim, packID, sourceID string) (verificationstore.EvidenceReference, bool) {
	for _, evidence := range claim.Evidence {
		if evidence.PackID == packID && evidence.SourceID == sourceID {
			return evidence, true
		}
	}
	return verificationstore.EvidenceReference{}, false
}

func (service RefreshService) recordRevalidationProvenance(ctx context.Context, pack Pack, oldRecord, newRecord verificationstore.Record, prior evidencestore.SourceState, newEvidence evidencestore.Record, eventIDs []string, transitions []provenance.TruthTransition) (string, error) {
	statuses := map[string]int{}
	claims := []provenance.ClaimReference{}
	evidenceIDs := []string{newEvidence.ID}
	revisions := []provenance.SourceRevision{{EvidenceID: newEvidence.ID, SourceID: newEvidence.SourceID, Revision: newEvidence.DocumentRevision, ContentSHA256: newEvidence.ContentSHA256}}
	if prior.CurrentEvidenceID != "" {
		evidenceIDs = append(evidenceIDs, prior.CurrentEvidenceID)
		revisions = append(revisions, provenance.SourceRevision{EvidenceID: prior.CurrentEvidenceID, SourceID: newEvidence.SourceID, Revision: prior.DocumentRevision, ContentSHA256: prior.ContentSHA256})
	}
	for _, transition := range transitions {
		evidenceIDs = append(evidenceIDs, transition.OldEvidenceID, transition.NewEvidenceID)
		revisions = append(revisions, provenance.SourceRevision{EvidenceID: transition.OldEvidenceID, SourceID: newEvidence.SourceID, Revision: transition.OldRevision, ContentSHA256: transition.OldHash})
	}
	for _, claim := range newRecord.Claims {
		statuses[claim.Status]++
		claimEvidenceIDs := []string{}
		for _, reference := range claim.Evidence {
			claimEvidenceIDs = append(claimEvidenceIDs, reference.EvidenceID)
			evidenceIDs = append(evidenceIDs, reference.EvidenceID)
			if reference.EvidenceID == prior.CurrentEvidenceID || reference.EvidenceID == newEvidence.ID {
				continue
			}
			revisions = append(revisions, provenance.SourceRevision{EvidenceID: reference.EvidenceID, SourceID: reference.SourceID, Revision: reference.Revision, ContentSHA256: reference.ContentSHA256})
		}
		claims = append(claims, provenance.ClaimReference{ArtifactPath: claim.ArtifactPath, Line: claim.Line, Category: claim.Category, Statement: claim.Statement, Status: claim.Status, EvidenceIDs: claimEvidenceIDs})
	}
	artifacts := []provenance.Artifact{}
	for _, path := range newRecord.Artifacts {
		artifacts = append(artifacts, provenance.Artifact{Path: path, Kind: "file"})
	}
	adapter := "swipenode"
	var external *provenance.ExternalContext
	if service.Enricher != nil {
		enrichment := service.Enricher.Enrich(ctx, service.Repository.Root)
		if enrichment.Adapter != "" {
			adapter = enrichment.Adapter
		}
		external = enrichment.ExternalContext
	}
	packReference := provenance.KnowledgePackReference{ID: pack.ID, Origin: string(pack.Origin)}
	if service.PackReference != nil {
		packReference = *service.PackReference
	}
	record := provenance.EngineeringProvenanceRecord{Timestamp: service.timestamp(), Repository: service.Repository, Artifacts: artifacts, Verification: provenance.VerificationReference{ID: newRecord.ReportID, RecordID: newRecord.ID, PreviousRecordID: oldRecord.ID, SchemaVersion: verification.SchemaVersion, Scope: "revalidation", Statuses: statuses}, EvidenceIDs: evidenceIDs, AuditEventIDs: eventIDs, KnowledgePackIDs: []string{pack.ID}, KnowledgePacks: []provenance.KnowledgePackReference{packReference}, Claims: claims, SourceRevisions: revisions, Adapter: adapter, ExternalContext: external, TruthTransitions: transitions}
	stored, err := service.Provenance.Append(record)
	return stored.ProvenanceID, err
}

func (service RefreshService) timestamp() string {
	if service.Now != nil {
		return service.Now().UTC().Format(time.RFC3339Nano)
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func retrievedFactForSource(claim verification.Claim, source string) string {
	for _, evidence := range claim.Evidence {
		if evidence.URLReference == source || evidence.Source == source {
			return evidence.RetrievedFact
		}
	}
	return ""
}

func revalidationReportID(record verificationstore.Record) string {
	parts := []string{record.ParentVerificationID}
	for _, claim := range record.Claims {
		parts = append(parts, claim.ID, claim.Status, claim.Reason)
	}
	return "reval_" + evidencestore.ContentHash(strings.Join(parts, "\x00"))[:32]
}
