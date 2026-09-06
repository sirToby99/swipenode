// Package controlplane exposes customer-local governance views and actions over
// SwipeNode Core. It owns no second database and never uploads local state.
package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/audit"
	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/evidencestore"
	"github.com/sirToby99/swipenode/internal/extractor"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/localstate"
	"github.com/sirToby99/swipenode/internal/provenance"
	"github.com/sirToby99/swipenode/internal/verificationstore"
)

const (
	OverviewSchemaVersion = "swipenode.control-plane-overview.v1"
	PacksSchemaVersion    = "swipenode.control-plane-packs.v1"
	UpdateCacheSchema     = "swipenode.control-plane-update-cache.v1"
)

var secretAssignment = regexp.MustCompile(`(?i)\b(token|secret|password|passwd|api[_-]?key|authorization)\s*[:=]\s*[^\s,;}]+`)

type Service struct {
	Root     string
	StateDir string
	Now      func() time.Time
}

type SourcePolicyView struct {
	ID                string `json:"id"`
	CanonicalURL      string `json:"canonical_url"`
	AuthoritativeHost string `json:"authoritative_host"`
	SourceType        string `json:"source_type"`
	Authoritative     bool   `json:"authoritative"`
	HTTPSRequired     bool   `json:"https_required"`
	AllowSubdomains   bool   `json:"allow_subdomains"`
	Revision          string `json:"revision,omitempty"`
	Health            string `json:"health"`
	Status            string `json:"status"`
	LastChecked       string `json:"last_checked,omitempty"`
	ContentSHA256     string `json:"content_sha256,omitempty"`
}

type PackView struct {
	ID                string                          `json:"id"`
	Owner             string                          `json:"owner"`
	Project           string                          `json:"project,omitempty"`
	Description       string                          `json:"description"`
	Origin            knowledge.Origin                `json:"origin"`
	Category          string                          `json:"category"`
	Scope             knowledge.Scope                 `json:"scope"`
	Freshness         knowledge.FreshnessPolicy       `json:"freshness"`
	RequireProvenance bool                            `json:"require_provenance"`
	Sources           []SourcePolicyView              `json:"sources"`
	Installed         []distribution.InstalledRelease `json:"installed_versions"`
	Active            *distribution.ActiveRelease     `json:"active,omitempty"`
	AvailableVersion  string                          `json:"available_version,omitempty"`
	UpdateStatus      string                          `json:"update_status"`
}

type TrustedPublisher struct {
	RemoteID  string `json:"remote_id"`
	BaseURL   string `json:"base_url"`
	Publisher string `json:"publisher"`
	KeyID     string `json:"key_id"`
}

type PacksView struct {
	SchemaVersion     string                    `json:"schema_version"`
	Policy            distribution.UpdatePolicy `json:"update_policy"`
	Packs             []PackView                `json:"packs"`
	TrustedPublishers []TrustedPublisher        `json:"trusted_publishers"`
}

type EvidenceView struct {
	SchemaVersion     string                     `json:"schema_version"`
	ID                string                     `json:"id"`
	PackID            string                     `json:"pack_id,omitempty"`
	SourceID          string                     `json:"source_id"`
	CanonicalURL      string                     `json:"canonical_url"`
	Owner             string                     `json:"owner"`
	SourceType        string                     `json:"source_type"`
	RetrievedAt       string                     `json:"retrieved_at"`
	DocumentRevision  string                     `json:"document_revision,omitempty"`
	ContentSHA256     string                     `json:"content_sha256"`
	ExtractedFact     string                     `json:"extracted_fact,omitempty"`
	VerificationID    string                     `json:"verification_id,omitempty"`
	ClaimID           string                     `json:"claim_id,omitempty"`
	Redacted          bool                       `json:"redacted"`
	VerificationLinks []EvidenceVerificationLink `json:"verification_links"`
}

type EvidenceVerificationLink struct {
	VerificationID string `json:"verification_id"`
	ClaimID        string `json:"claim_id"`
	ArtifactPath   string `json:"artifact_path"`
	Line           int    `json:"line"`
	Statement      string `json:"statement"`
	Status         string `json:"status"`
	RecordedAt     string `json:"recorded_at"`
}

type EvidenceList struct {
	SchemaVersion string         `json:"schema_version"`
	Evidence      []EvidenceView `json:"evidence"`
	TotalCount    int            `json:"total_count,omitempty"`
	Limit         int            `json:"limit,omitempty"`
	Offset        int            `json:"offset,omitempty"`
}

type VerificationView struct {
	verificationstore.Record
}

type VerificationList struct {
	SchemaVersion string                     `json:"schema_version"`
	Records       []verificationstore.Record `json:"records"`
	TotalCount    int                        `json:"total_count,omitempty"`
	Limit         int                        `json:"limit,omitempty"`
	Offset        int                        `json:"offset,omitempty"`
}

type ProvenanceList struct {
	SchemaVersion string                                   `json:"schema_version"`
	Records       []provenance.EngineeringProvenanceRecord `json:"records"`
	TotalCount    int                                      `json:"total_count,omitempty"`
	Limit         int                                      `json:"limit,omitempty"`
	Offset        int                                      `json:"offset,omitempty"`
}

type AuditList struct {
	SchemaVersion string        `json:"schema_version"`
	Events        []audit.Event `json:"events"`
	TotalCount    int           `json:"total_count,omitempty"`
	Limit         int           `json:"limit,omitempty"`
	Offset        int           `json:"offset,omitempty"`
}

type ListOptions struct {
	Limit  int
	Offset int
	Query  string
	Status string
}

type Attention struct {
	VerificationID string                `json:"verification_id"`
	ClaimID        string                `json:"claim_id"`
	ArtifactPath   string                `json:"artifact_path"`
	Statement      string                `json:"statement"`
	Status         string                `json:"status"`
	PackID         string                `json:"pack_id,omitempty"`
	RecordedAt     string                `json:"recorded_at"`
	History        []VerificationHistory `json:"history,omitempty"`
}

type VerificationHistory struct {
	VerificationID string `json:"verification_id"`
	Status         string `json:"status"`
	RecordedAt     string `json:"recorded_at"`
}

type HealthCounts struct {
	Healthy int `json:"healthy"`
	Changed int `json:"changed"`
	Stale   int `json:"stale"`
	Failed  int `json:"failed"`
}

type Overview struct {
	SchemaVersion        string                                   `json:"schema_version"`
	PackCount            int                                      `json:"pack_count"`
	InstalledPackCount   int                                      `json:"installed_pack_count"`
	ActivePackCount      int                                      `json:"active_pack_count"`
	AvailableUpdates     int                                      `json:"available_updates"`
	SourceCount          int                                      `json:"source_count"`
	KnowledgeHealth      HealthCounts                             `json:"knowledge_health"`
	ConflictCount        int                                      `json:"conflict_count"`
	UnverifiedCount      int                                      `json:"unverified_count"`
	Attention            []Attention                              `json:"attention"`
	CurrentVerifications []Attention                              `json:"current_verifications"`
	RecentProvenance     []provenance.EngineeringProvenanceRecord `json:"recent_provenance"`
	RecentAudit          []audit.Event                            `json:"recent_audit"`
}

type UpdateCheck struct {
	PackID        string `json:"pack_id"`
	RemoteID      string `json:"remote_id"`
	LatestVersion string `json:"latest_version"`
	CheckedAt     string `json:"checked_at"`
}

type ValidationResult struct {
	SchemaVersion string `json:"schema_version"`
	Valid         bool   `json:"valid"`
	PackCount     int    `json:"pack_count"`
	ProjectPacks  int    `json:"project_pack_count"`
}

type updateCache struct {
	SchemaVersion string        `json:"schema_version"`
	Checks        []UpdateCheck `json:"checks"`
}

func Open(ctx context.Context, root string) (*Service, error) {
	resolved, err := gitcontext.ResolveRoot(root)
	if err != nil {
		return nil, err
	}
	stateDir, err := gitcontext.StateDir(ctx, resolved)
	if err != nil {
		return nil, err
	}
	return &Service{Root: resolved, StateDir: stateDir, Now: time.Now}, nil
}

func (service *Service) Packs(ctx context.Context) (PacksView, error) {
	registry, err := knowledge.LoadWithState(service.Root, service.StateDir)
	if err != nil {
		return PacksView{}, err
	}
	distributionState, err := distribution.OpenStore(service.StateDir).Load()
	if err != nil {
		return PacksView{}, err
	}
	sourceStates, err := evidencestore.Open(service.StateDir).SourceStates()
	if err != nil {
		return PacksView{}, err
	}
	cache, err := service.readUpdateCache()
	if err != nil {
		return PacksView{}, err
	}
	stateBySource := map[string]evidencestore.SourceState{}
	for _, state := range sourceStates {
		stateBySource[state.PackID+"\x00"+state.SourceID] = state
	}
	updates := map[string]UpdateCheck{}
	for _, check := range cache.Checks {
		updates[check.PackID] = check
	}
	packByID := map[string]knowledge.Pack{}
	for _, pack := range registry.List() {
		packByID[pack.ID] = pack
	}
	ids := map[string]bool{}
	for id := range packByID {
		ids[id] = true
	}
	for id := range distributionState.Installed {
		ids[id] = true
	}
	sortedIDs := make([]string, 0, len(ids))
	for id := range ids {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)
	views := make([]PackView, 0, len(sortedIDs))
	for _, id := range sortedIDs {
		pack, ok := packByID[id]
		view := PackView{ID: id, UpdateStatus: "not_checked", Installed: append([]distribution.InstalledRelease(nil), distributionState.Installed[id]...)}
		if ok {
			view.Owner, view.Project, view.Description, view.Origin, view.Scope, view.Freshness, view.RequireProvenance = pack.Owner, pack.Project, pack.Description, pack.Origin, pack.Scope, pack.Freshness, pack.Trust.RequireProvenance
			switch pack.Origin {
			case knowledge.OriginManaged:
				view.Category = "managed"
			case knowledge.OriginProject:
				view.Category = "private"
			default:
				view.Category = "builtin"
			}
			for _, source := range pack.Sources {
				state := stateBySource[pack.ID+"\x00"+source.ID]
				health, status := state.Health, state.Status
				if health == "" {
					health, status = "unknown", "not_checked"
				}
				view.Sources = append(view.Sources, SourcePolicyView{ID: source.ID, CanonicalURL: safeDisplayURL(source.CanonicalURL), AuthoritativeHost: source.AuthoritativeHost, SourceType: source.SourceType, Authoritative: source.Authoritative, HTTPSRequired: source.HTTPSRequired, AllowSubdomains: source.AllowSubdomains, Revision: source.Revision, Health: health, Status: status, LastChecked: state.LastChecked, ContentSHA256: state.ContentSHA256})
			}
		} else {
			view.Category, view.Origin, view.Description = "managed", knowledge.OriginManaged, "Installed signed managed Knowledge Pack."
			if len(view.Installed) > 0 {
				view.Owner = view.Installed[len(view.Installed)-1].Publisher
			}
		}
		if len(view.Installed) > 0 {
			view.Category = "managed"
		}
		if active, found := distributionState.Active[id]; found {
			copy := active
			view.Active = &copy
		}
		if check, found := updates[id]; found {
			view.AvailableVersion, view.UpdateStatus = check.LatestVersion, "checked"
		}
		views = append(views, view)
	}
	publishers := make([]TrustedPublisher, 0, len(distributionState.Remotes))
	for _, remote := range distributionState.Remotes {
		publishers = append(publishers, TrustedPublisher{RemoteID: remote.ID, BaseURL: safeDisplayURL(remote.BaseURL), Publisher: remote.Publisher, KeyID: remote.KeyID})
	}
	return PacksView{SchemaVersion: PacksSchemaVersion, Policy: distributionState.Policy, Packs: views, TrustedPublishers: publishers}, nil
}

func (service *Service) Pack(ctx context.Context, id string) (PackView, bool, error) {
	packs, err := service.Packs(ctx)
	if err != nil {
		return PackView{}, false, err
	}
	for _, pack := range packs.Packs {
		if pack.ID == id {
			return pack, true, nil
		}
	}
	return PackView{}, false, nil
}

func (service *Service) Evidence() (EvidenceList, error) {
	return service.EvidencePage(ListOptions{})
}

func (service *Service) EvidencePage(options ListOptions) (EvidenceList, error) {
	records, err := evidencestore.Open(service.StateDir).List()
	if err != nil {
		return EvidenceList{}, err
	}
	verificationRecords, err := verificationstore.Open(service.StateDir).List()
	if err != nil {
		return EvidenceList{}, err
	}
	links := map[string][]EvidenceVerificationLink{}
	for _, record := range verificationRecords {
		for _, claim := range record.Claims {
			statement, _ := redact(claim.Statement)
			for _, evidence := range claim.Evidence {
				links[evidence.EvidenceID] = append(links[evidence.EvidenceID], EvidenceVerificationLink{VerificationID: record.ID, ClaimID: claim.ID, ArtifactPath: claim.ArtifactPath, Line: claim.Line, Statement: statement, Status: claim.Status, RecordedAt: record.RecordedAt})
			}
		}
	}
	views := make([]EvidenceView, 0, len(records))
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		fact, redacted := redact(record.ExtractedFact)
		view := EvidenceView{SchemaVersion: record.SchemaVersion, ID: record.ID, PackID: record.PackID, SourceID: record.SourceID, CanonicalURL: safeDisplayURL(record.CanonicalURL), Owner: record.Owner, SourceType: record.SourceType, RetrievedAt: record.RetrievedAt, DocumentRevision: record.DocumentRevision, ContentSHA256: record.ContentSHA256, ExtractedFact: fact, VerificationID: record.Verification.VerificationID, ClaimID: record.Verification.ClaimID, Redacted: redacted, VerificationLinks: links[record.ID]}
		if view.VerificationID == "" && len(view.VerificationLinks) > 0 {
			view.VerificationID, view.ClaimID = view.VerificationLinks[0].VerificationID, view.VerificationLinks[0].ClaimID
		}
		views = append(views, view)
	}
	views = filterEvidence(views, options)
	total := len(views)
	views = page(views, options)
	return EvidenceList{SchemaVersion: "swipenode.control-plane-evidence.v1", Evidence: views, TotalCount: total, Limit: options.Limit, Offset: options.Offset}, nil
}

func (service *Service) Validate(ctx context.Context) (ValidationResult, error) {
	registry, err := knowledge.LoadWithState(service.Root, service.StateDir)
	if err != nil {
		return ValidationResult{}, err
	}
	result := ValidationResult{SchemaVersion: "swipenode.control-plane-knowledge-validation.v1", Valid: true, PackCount: len(registry.List())}
	for _, pack := range registry.List() {
		if pack.Origin == knowledge.OriginProject {
			result.ProjectPacks++
		}
	}
	return result, nil
}

func (service *Service) FindEvidence(id string) (EvidenceView, bool, error) {
	listing, err := service.Evidence()
	if err != nil {
		return EvidenceView{}, false, err
	}
	for _, record := range listing.Evidence {
		if record.ID == id {
			return record, true, nil
		}
	}
	return EvidenceView{}, false, nil
}

func (service *Service) Verifications() (VerificationList, error) {
	return service.VerificationsPage(ListOptions{})
}

func (service *Service) VerificationsPage(options ListOptions) (VerificationList, error) {
	records, err := verificationstore.Open(service.StateDir).List()
	if err != nil {
		return VerificationList{}, err
	}
	for recordIndex := range records {
		for claimIndex := range records[recordIndex].Claims {
			claim := &records[recordIndex].Claims[claimIndex]
			claim.Statement, _ = redact(claim.Statement)
			claim.Reason, _ = redact(claim.Reason)
			for evidenceIndex := range claim.Evidence {
				evidence := &claim.Evidence[evidenceIndex]
				evidence.CanonicalURL = safeDisplayURL(evidence.CanonicalURL)
				evidence.RetrievedFact, _ = redact(evidence.RetrievedFact)
			}
		}
	}
	records = filterVerifications(records, options)
	total := len(records)
	records = page(records, options)
	return VerificationList{SchemaVersion: "swipenode.control-plane-verifications.v1", Records: records, TotalCount: total, Limit: options.Limit, Offset: options.Offset}, nil
}

func (service *Service) FindVerification(id string) (verificationstore.Record, bool, error) {
	listing, err := service.Verifications()
	if err != nil {
		return verificationstore.Record{}, false, err
	}
	for _, record := range listing.Records {
		if record.ID == id {
			return record, true, nil
		}
	}
	return verificationstore.Record{}, false, nil
}

func (service *Service) Provenance() ([]provenance.EngineeringProvenanceRecord, error) {
	return provenance.Open(service.StateDir).List()
}

func (service *Service) ProvenancePage(options ListOptions) (ProvenanceList, error) {
	records, err := service.Provenance()
	if err != nil {
		return ProvenanceList{}, err
	}
	query := strings.ToLower(strings.TrimSpace(options.Query))
	if query != "" {
		filtered := make([]provenance.EngineeringProvenanceRecord, 0, len(records))
		for _, record := range records {
			parts := []string{record.ProvenanceID, record.Repository.GitBranch, record.Repository.GitCommit, record.Verification.ID, record.Verification.RecordID, record.Adapter, strings.Join(record.EvidenceIDs, " "), strings.Join(record.KnowledgePackIDs, " ")}
			for _, artifact := range record.Artifacts {
				parts = append(parts, artifact.Path, artifact.Kind)
			}
			for _, claim := range record.Claims {
				parts = append(parts, claim.ArtifactPath, claim.Statement, claim.Status)
			}
			if strings.Contains(strings.ToLower(strings.Join(parts, " ")), query) {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	total := len(records)
	records = page(records, options)
	return ProvenanceList{SchemaVersion: "swipenode.control-plane-provenance.v1", Records: records, TotalCount: total, Limit: options.Limit, Offset: options.Offset}, nil
}

func (service *Service) FindProvenance(id string) (provenance.EngineeringProvenanceRecord, bool, error) {
	return provenance.Open(service.StateDir).Find(id)
}

func (service *Service) Audit() ([]audit.Event, error) {
	events, err := audit.Open(service.StateDir).List()
	if err != nil {
		return nil, err
	}
	for index := range events {
		events[index].CanonicalURL = safeDisplayURL(events[index].CanonicalURL)
	}
	return events, nil
}

func (service *Service) AuditPage(options ListOptions) (AuditList, error) {
	events, err := service.Audit()
	if err != nil {
		return AuditList{}, err
	}
	query, wantedStatus := strings.ToLower(strings.TrimSpace(options.Query)), strings.ToLower(strings.TrimSpace(options.Status))
	if query != "" || wantedStatus != "" {
		filtered := make([]audit.Event, 0, len(events))
		for _, event := range events {
			text := strings.ToLower(strings.Join([]string{event.ID, string(event.Type), event.PackID, event.SourceID, event.EvidenceID, event.VerificationID, event.ArtifactPath, event.FailureKind, event.OldStatus, event.NewStatus}, " "))
			statusMatch := wantedStatus == "" || strings.EqualFold(event.NewStatus, wantedStatus) || strings.EqualFold(event.FailureKind, wantedStatus) || strings.EqualFold(string(event.Type), wantedStatus)
			if strings.Contains(text, query) && statusMatch {
				filtered = append(filtered, event)
			}
		}
		events = filtered
	}
	total := len(events)
	events = page(events, options)
	return AuditList{SchemaVersion: "swipenode.control-plane-audit.v1", Events: events, TotalCount: total, Limit: options.Limit, Offset: options.Offset}, nil
}

func (service *Service) FindAudit(id string) (audit.Event, bool, error) {
	event, found, err := audit.Open(service.StateDir).Find(id)
	if err == nil && found {
		event.CanonicalURL = safeDisplayURL(event.CanonicalURL)
	}
	return event, found, err
}

func (service *Service) Overview(ctx context.Context) (Overview, error) {
	packs, err := service.Packs(ctx)
	if err != nil {
		return Overview{}, err
	}
	verifications, err := service.Verifications()
	if err != nil {
		return Overview{}, err
	}
	provenanceRecords, err := service.Provenance()
	if err != nil {
		return Overview{}, err
	}
	auditEvents, err := service.Audit()
	if err != nil {
		return Overview{}, err
	}
	result := Overview{SchemaVersion: OverviewSchemaVersion, Attention: []Attention{}, CurrentVerifications: []Attention{}, RecentProvenance: []provenance.EngineeringProvenanceRecord{}, RecentAudit: []audit.Event{}}
	result.PackCount = len(packs.Packs)
	for _, pack := range packs.Packs {
		if len(pack.Installed) > 0 {
			result.InstalledPackCount++
		}
		if pack.Active != nil {
			result.ActivePackCount++
		}
		if pack.AvailableVersion != "" && (pack.Active == nil || pack.Active.Version != pack.AvailableVersion) {
			result.AvailableUpdates++
		}
		result.SourceCount += len(pack.Sources)
		for _, source := range pack.Sources {
			switch {
			case source.Health == "unhealthy" || source.Status == "failed" || strings.Contains(source.Status, "failure") || source.Status == "timeout":
				result.KnowledgeHealth.Failed++
			case source.Status == "changed":
				result.KnowledgeHealth.Changed++
			case source.Health == "stale":
				result.KnowledgeHealth.Stale++
			case source.Health == "healthy":
				result.KnowledgeHealth.Healthy++
			}
		}
	}
	currentByScope := map[string][]int{}
	currentRecordByScope := map[string]string{}
	for _, record := range verifications.Records {
		for _, claim := range record.Claims {
			scopeKey := record.PackID + "\x00" + claim.ArtifactPath
			currentRecordID, seen := currentRecordByScope[scopeKey]
			if seen && currentRecordID != record.ID {
				indexes := currentByScope[scopeKey]
				for _, index := range indexes {
					if len(result.CurrentVerifications[index].History) < 4 {
						result.CurrentVerifications[index].History = append(result.CurrentVerifications[index].History, VerificationHistory{VerificationID: record.ID, Status: claim.Status, RecordedAt: record.RecordedAt})
					}
				}
				continue
			}
			if !seen {
				currentRecordByScope[scopeKey] = record.ID
			}
			current := Attention{VerificationID: record.ID, ClaimID: claim.ID, ArtifactPath: claim.ArtifactPath, Statement: claim.Statement, Status: claim.Status, PackID: record.PackID, RecordedAt: record.RecordedAt, History: []VerificationHistory{}}
			result.CurrentVerifications = append(result.CurrentVerifications, current)
			currentByScope[scopeKey] = append(currentByScope[scopeKey], len(result.CurrentVerifications)-1)
			if claim.Status == "CONFLICT" || claim.Status == "UNVERIFIED" {
				if claim.Status == "CONFLICT" {
					result.ConflictCount++
				} else {
					result.UnverifiedCount++
				}
				if len(result.Attention) < 20 {
					result.Attention = append(result.Attention, current)
				}
			}
		}
	}
	result.RecentProvenance = limit(provenanceRecords, 10)
	result.RecentAudit = limit(auditEvents, 20)
	return result, nil
}

func filterEvidence(values []EvidenceView, options ListOptions) []EvidenceView {
	query, wantedStatus := strings.ToLower(strings.TrimSpace(options.Query)), strings.ToLower(strings.TrimSpace(options.Status))
	if query == "" && wantedStatus == "" {
		return values
	}
	result := make([]EvidenceView, 0, len(values))
	for _, value := range values {
		text := strings.ToLower(strings.Join([]string{value.ID, value.PackID, value.SourceID, value.Owner, value.DocumentRevision, value.ExtractedFact, value.VerificationID}, " "))
		statusMatch := wantedStatus == ""
		for _, link := range value.VerificationLinks {
			text += " " + strings.ToLower(strings.Join([]string{link.VerificationID, link.ClaimID, link.ArtifactPath, link.Statement, link.Status}, " "))
			if strings.EqualFold(link.Status, wantedStatus) {
				statusMatch = true
			}
		}
		if strings.Contains(text, query) && statusMatch {
			result = append(result, value)
		}
	}
	return result
}

func filterVerifications(values []verificationstore.Record, options ListOptions) []verificationstore.Record {
	query, wantedStatus := strings.ToLower(strings.TrimSpace(options.Query)), strings.ToLower(strings.TrimSpace(options.Status))
	if query == "" && wantedStatus == "" {
		return values
	}
	result := make([]verificationstore.Record, 0, len(values))
	for _, value := range values {
		parts := []string{value.ID, value.ReportID, value.PackID, value.Scope, strings.Join(value.Artifacts, " ")}
		statusMatch := wantedStatus == ""
		for _, claim := range value.Claims {
			parts = append(parts, claim.ID, claim.ArtifactPath, claim.Statement, claim.Status)
			if strings.EqualFold(claim.Status, wantedStatus) {
				statusMatch = true
			}
		}
		if strings.Contains(strings.ToLower(strings.Join(parts, " ")), query) && statusMatch {
			result = append(result, value)
		}
	}
	return result
}

func page[T any](values []T, options ListOptions) []T {
	if options.Limit <= 0 {
		return values
	}
	start := options.Offset
	if start >= len(values) {
		return []T{}
	}
	end := start + options.Limit
	if end > len(values) {
		end = len(values)
	}
	return values[start:end]
}

func (service *Service) UpdateCheck(ctx context.Context, packID, remoteID string) (distribution.PackDescriptor, error) {
	store := distribution.OpenStore(service.StateDir)
	remote, err := selectRemote(store, remoteID)
	if err != nil {
		return distribution.PackDescriptor{}, err
	}
	descriptor, err := (distribution.Client{BaseURL: remote.BaseURL}).Pack(ctx, packID)
	if err != nil {
		return descriptor, err
	}
	if descriptor.Latest.Publisher != remote.Publisher || descriptor.Latest.KeyID != remote.KeyID {
		return descriptor, fmt.Errorf("managed update descriptor does not match trusted publisher")
	}
	lock, err := localstate.Acquire(service.StateDir, "control-plane-update-cache")
	if err != nil {
		return descriptor, err
	}
	defer lock.Release()
	if err := service.writeUpdateCheck(UpdateCheck{PackID: packID, RemoteID: remote.ID, LatestVersion: descriptor.LatestVersion, CheckedAt: service.now().Format(time.RFC3339Nano)}); err != nil {
		return descriptor, err
	}
	return descriptor, nil
}

func (service *Service) Pull(ctx context.Context, packID, remoteID string) (distribution.InstalledRelease, bool, error) {
	store := distribution.OpenStore(service.StateDir)
	remote, err := selectRemote(store, remoteID)
	if err != nil {
		return distribution.InstalledRelease{}, false, err
	}
	lock, err := localstate.Acquire(service.StateDir, "knowledge-distribution")
	if err != nil {
		return distribution.InstalledRelease{}, false, err
	}
	defer lock.Release()
	return store.Pull(ctx, remote.ID, packID)
}

func (service *Service) Activate(packID, version string) (distribution.ActiveRelease, error) {
	store := distribution.OpenStore(service.StateDir)
	lock, err := localstate.Acquire(service.StateDir, "knowledge-distribution")
	if err != nil {
		return distribution.ActiveRelease{}, err
	}
	defer lock.Release()
	return store.Activate(packID, version)
}

func (service *Service) Rollback(packID string) (distribution.ActiveRelease, error) {
	store := distribution.OpenStore(service.StateDir)
	lock, err := localstate.Acquire(service.StateDir, "knowledge-distribution")
	if err != nil {
		return distribution.ActiveRelease{}, err
	}
	defer lock.Release()
	return store.Rollback(packID)
}

func (service *Service) SetPolicy(policy distribution.UpdatePolicy) error {
	lock, err := localstate.Acquire(service.StateDir, "knowledge-distribution")
	if err != nil {
		return err
	}
	defer lock.Release()
	return distribution.OpenStore(service.StateDir).SetPolicy(policy)
}

type RefreshRequest struct {
	Fetch      bool `json:"fetch"`
	Revalidate bool `json:"revalidate"`
	DryRun     bool `json:"dry_run"`
}

func (service *Service) Refresh(ctx context.Context, packID string, request RefreshRequest) (knowledge.RefreshReport, error) {
	if request.Revalidate && !request.Fetch {
		return knowledge.RefreshReport{}, fmt.Errorf("revalidation requires explicit fetch")
	}
	registry, err := knowledge.LoadWithState(service.Root, service.StateDir)
	if err != nil {
		return knowledge.RefreshReport{}, err
	}
	pack, found := registry.Find(packID)
	if !found {
		return knowledge.RefreshReport{}, fmt.Errorf("unknown knowledge pack %q", packID)
	}
	lock, err := localstate.Acquire(service.StateDir, "knowledge-refresh")
	if err != nil {
		return knowledge.RefreshReport{}, err
	}
	defer lock.Release()
	repository, err := gitcontext.Inspect(ctx, service.Root)
	if err != nil {
		return knowledge.RefreshReport{}, err
	}
	packReference, err := customerPackReference(service.StateDir, pack)
	if err != nil {
		return knowledge.RefreshReport{}, err
	}
	refresh := knowledge.RefreshService{Evidence: evidencestore.Open(service.StateDir), Audit: audit.Open(service.StateDir), Verification: verificationstore.Open(service.StateDir), Provenance: provenance.Open(service.StateDir), Fetcher: sourceFetcher{}, Repository: provenance.RepositoryContext{Root: repository.Root, GitCommit: repository.Head, GitBranch: repository.Branch}, PackReference: &packReference}
	return refresh.RefreshWithOptions(ctx, pack, knowledge.RefreshOptions{Fetch: request.Fetch, Revalidate: request.Revalidate, DryRun: request.DryRun})
}

func customerPackReference(stateDir string, pack knowledge.Pack) (provenance.KnowledgePackReference, error) {
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

type sourceFetcher struct{}

func (sourceFetcher) Fetch(ctx context.Context, target string) (knowledge.FetchedDocument, error) {
	result, err := extractor.Extract(ctx, target)
	if err != nil {
		return knowledge.FetchedDocument{}, classifyExtractError(err)
	}
	return knowledge.FetchedDocument{URL: result.URL, Content: result.Content, FetchedAt: result.FetchedAt, ETag: result.ETag, LastModified: result.LastModified}, nil
}

func (sourceFetcher) FetchConditional(ctx context.Context, target string, condition knowledge.FetchCondition) (knowledge.FetchedDocument, error) {
	result, err := extractor.ExtractWithOptions(ctx, target, extractor.RequestOptions{IfNoneMatch: condition.ETag, IfModifiedSince: condition.LastModified})
	if err != nil {
		return knowledge.FetchedDocument{}, classifyExtractError(err)
	}
	return knowledge.FetchedDocument{URL: result.URL, Content: result.Content, FetchedAt: result.FetchedAt, ETag: result.ETag, LastModified: result.LastModified, NotModified: result.NotModified}, nil
}

func classifyExtractError(err error) error {
	var status extractor.HTTPStatusError
	if errors.As(err, &status) && (status.StatusCode == 404 || status.StatusCode == 410) {
		return knowledge.FetchFailure{Kind: "source_disappeared", Err: err}
	}
	var parse extractor.ParseError
	if errors.As(err, &parse) {
		return knowledge.FetchFailure{Kind: "parser_failure", Err: err}
	}
	return err
}

func selectRemote(store *distribution.Store, requested string) (distribution.Remote, error) {
	state, err := store.Load()
	if err != nil {
		return distribution.Remote{}, err
	}
	if requested != "" {
		remote, found, err := store.Remote(requested)
		if err != nil {
			return distribution.Remote{}, err
		}
		if !found {
			return distribution.Remote{}, fmt.Errorf("unknown knowledge remote %q", requested)
		}
		return remote, nil
	}
	if len(state.Remotes) != 1 {
		return distribution.Remote{}, fmt.Errorf("remote_id is required unless exactly one remote is configured")
	}
	return state.Remotes[0], nil
}

func safeDisplayURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

func redact(value string) (string, bool) {
	redacted := secretAssignment.ReplaceAllString(value, "$1=[REDACTED]")
	return redacted, redacted != value
}

func (service *Service) now() time.Time {
	if service.Now == nil {
		return time.Now().UTC()
	}
	return service.Now().UTC()
}

func (service *Service) readUpdateCache() (updateCache, error) {
	result := updateCache{SchemaVersion: UpdateCacheSchema, Checks: []UpdateCheck{}}
	path := filepath.Join(service.StateDir, "control-plane-updates.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return result, fmt.Errorf("unsafe control-plane update cache")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, fmt.Errorf("update cache has trailing content")
	}
	if result.SchemaVersion != UpdateCacheSchema {
		return result, fmt.Errorf("unsupported update cache schema")
	}
	return result, nil
}

func (service *Service) writeUpdateCheck(check UpdateCheck) error {
	cache, err := service.readUpdateCache()
	if err != nil {
		return err
	}
	found := false
	for index := range cache.Checks {
		if cache.Checks[index].PackID == check.PackID {
			cache.Checks[index], found = check, true
		}
	}
	if !found {
		cache.Checks = append(cache.Checks, check)
	}
	sort.Slice(cache.Checks, func(i, j int) bool { return cache.Checks[i].PackID < cache.Checks[j].PackID })
	if err := os.MkdirAll(service.StateDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(service.StateDir, ".control-plane-updates-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(service.StateDir, "control-plane-updates.json"))
}

func limit[T any](values []T, maximum int) []T {
	if len(values) <= maximum {
		return values
	}
	return values[:maximum]
}
