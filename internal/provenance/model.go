// Package provenance models immutable engineering-decision provenance owned by
// SwipeNode. External systems may enrich records but are never required.
package provenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "swipenode.engineering-provenance.v1"

type RepositoryContext struct {
	Root      string `json:"root"`
	GitCommit string `json:"git_commit,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
}

type Artifact struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type VerificationReference struct {
	ID               string         `json:"id"`
	RecordID         string         `json:"record_id,omitempty"`
	PreviousRecordID string         `json:"previous_record_id,omitempty"`
	SchemaVersion    string         `json:"schema_version"`
	Scope            string         `json:"scope"`
	Clean            bool           `json:"clean"`
	Statuses         map[string]int `json:"statuses"`
}

type TruthTransition struct {
	ClaimID           string `json:"claim_id"`
	ArtifactPath      string `json:"artifact_path"`
	OldVerificationID string `json:"old_verification_id"`
	NewVerificationID string `json:"new_verification_id"`
	OldEvidenceID     string `json:"old_evidence_id"`
	NewEvidenceID     string `json:"new_evidence_id"`
	OldStatus         string `json:"old_status"`
	NewStatus         string `json:"new_status"`
	OldHash           string `json:"old_hash"`
	NewHash           string `json:"new_hash"`
	OldRevision       string `json:"old_revision,omitempty"`
	NewRevision       string `json:"new_revision,omitempty"`
}

type ClaimReference struct {
	ArtifactPath string   `json:"artifact_path"`
	Line         int      `json:"line"`
	Category     string   `json:"category"`
	Statement    string   `json:"statement"`
	Status       string   `json:"status"`
	EvidenceIDs  []string `json:"evidence_ids"`
}

type SourceRevision struct {
	EvidenceID    string `json:"evidence_id"`
	SourceID      string `json:"source_id"`
	Revision      string `json:"revision,omitempty"`
	ContentSHA256 string `json:"content_sha256"`
}

// KnowledgePackReference links an engineering decision to the exact Pack
// release selected by the customer runtime. Public Pack metadata is referenced;
// evidence payloads remain in the customer-local Evidence Store.
type KnowledgePackReference struct {
	ID             string `json:"id"`
	Origin         string `json:"origin"`
	Version        string `json:"version,omitempty"`
	Publisher      string `json:"publisher,omitempty"`
	PublisherKeyID string `json:"publisher_key_id,omitempty"`
	PackageSHA256  string `json:"package_sha256,omitempty"`
}

type EntireContext struct {
	ContextStatus    string     `json:"context_status"`
	CLIVersion       string     `json:"cli_version,omitempty"`
	SessionID        string     `json:"session_id,omitempty"`
	CheckpointID     string     `json:"checkpoint_id,omitempty"`
	Agent            string     `json:"agent,omitempty"`
	Model            string     `json:"model,omitempty"`
	SessionStatus    string     `json:"session_status,omitempty"`
	SessionArtifacts []Artifact `json:"session_artifacts,omitempty"`
	Limitation       string     `json:"limitation,omitempty"`
}

type ExternalContext struct {
	Entire *EntireContext `json:"entire,omitempty"`
}

type EngineeringProvenanceRecord struct {
	SchemaVersion    string                   `json:"schema_version"`
	ProvenanceID     string                   `json:"provenance_id"`
	Timestamp        string                   `json:"timestamp"`
	Repository       RepositoryContext        `json:"repository"`
	Artifacts        []Artifact               `json:"artifacts"`
	Verification     VerificationReference    `json:"verification"`
	EvidenceIDs      []string                 `json:"evidence_ids"`
	AuditEventIDs    []string                 `json:"audit_event_ids"`
	KnowledgePackIDs []string                 `json:"knowledge_pack_ids"`
	KnowledgePacks   []KnowledgePackReference `json:"knowledge_packs,omitempty"`
	Claims           []ClaimReference         `json:"claims"`
	SourceRevisions  []SourceRevision         `json:"source_revisions"`
	Adapter          string                   `json:"adapter"`
	ExternalContext  *ExternalContext         `json:"external_context,omitempty"`
	TruthTransitions []TruthTransition        `json:"truth_transitions,omitempty"`
}

// Enrichment is returned by an optional adapter. Missing external state is a
// valid enrichment result, not a reason to lose SwipeNode-owned provenance.
type Enrichment struct {
	Adapter         string
	ExternalContext *ExternalContext
}

type Enricher interface {
	Enrich(context.Context, string) Enrichment
}

var recordIDPattern = regexp.MustCompile(`^prov_[0-9a-f]{32}$`)
var secretAssignmentPattern = regexp.MustCompile(`(?i)\b(token|secret|password|passwd|api[_-]?key|authorization)\s*[:=]\s*[^\s,}\]]+`)

func (record *EngineeringProvenanceRecord) Prepare() error {
	if record.SchemaVersion != "" && record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported engineering provenance schema %q", record.SchemaVersion)
	}
	record.SchemaVersion = SchemaVersion
	if record.Adapter == "" {
		record.Adapter = "swipenode"
	}
	if _, err := time.Parse(time.RFC3339Nano, record.Timestamp); err != nil {
		return fmt.Errorf("invalid provenance timestamp: %w", err)
	}
	if strings.TrimSpace(record.Repository.Root) == "" || strings.TrimSpace(record.Verification.ID) == "" {
		return fmt.Errorf("repository root and verification id are required")
	}
	var err error
	record.Artifacts, err = uniqueArtifacts(record.Artifacts)
	if err != nil {
		return err
	}
	if record.ExternalContext != nil && record.ExternalContext.Entire != nil {
		record.ExternalContext.Entire.SessionArtifacts, err = uniqueArtifacts(record.ExternalContext.Entire.SessionArtifacts)
		if err != nil {
			return fmt.Errorf("invalid Entire session artifact: %w", err)
		}
	}
	record.EvidenceIDs = uniqueStrings(record.EvidenceIDs)
	record.AuditEventIDs = uniqueStrings(record.AuditEventIDs)
	record.KnowledgePackIDs = uniqueStrings(record.KnowledgePackIDs)
	record.KnowledgePacks, err = uniqueKnowledgePacks(record.KnowledgePacks)
	if err != nil {
		return err
	}
	for _, pack := range record.KnowledgePacks {
		if !containsString(record.KnowledgePackIDs, pack.ID) {
			return fmt.Errorf("knowledge Pack reference %q is missing from knowledge_pack_ids", pack.ID)
		}
	}
	record.SourceRevisions = uniqueSourceRevisions(record.SourceRevisions)
	evidenceSet := map[string]bool{}
	for _, id := range record.EvidenceIDs {
		evidenceSet[id] = true
	}
	for _, revision := range record.SourceRevisions {
		if !evidenceSet[revision.EvidenceID] || strings.TrimSpace(revision.SourceID) == "" || len(revision.ContentSHA256) != 64 {
			return fmt.Errorf("source revision must reference evidence, source identity, and SHA-256 content")
		}
		if _, err := hex.DecodeString(revision.ContentSHA256); err != nil {
			return fmt.Errorf("source revision content hash is invalid")
		}
	}
	for index := range record.Claims {
		record.Claims[index].ArtifactPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(record.Claims[index].ArtifactPath)))
		if record.Claims[index].ArtifactPath != "." && !filepath.IsLocal(filepath.FromSlash(record.Claims[index].ArtifactPath)) {
			return fmt.Errorf("claim artifact path is not repository-local")
		}
		record.Claims[index].EvidenceIDs = uniqueStrings(record.Claims[index].EvidenceIDs)
		for _, id := range record.Claims[index].EvidenceIDs {
			if !evidenceSet[id] {
				return fmt.Errorf("claim references unknown evidence %q", id)
			}
		}
	}
	for index := range record.TruthTransitions {
		transition := &record.TruthTransitions[index]
		transition.ArtifactPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(transition.ArtifactPath)))
		if transition.ClaimID == "" || transition.OldVerificationID == "" || transition.NewVerificationID == "" || transition.OldStatus == "" || transition.NewStatus == "" {
			return fmt.Errorf("truth transition identity and statuses are required")
		}
		if transition.ArtifactPath == "." || !filepath.IsLocal(filepath.FromSlash(transition.ArtifactPath)) {
			return fmt.Errorf("truth transition artifact is not repository-local")
		}
		if !evidenceSet[transition.OldEvidenceID] || !evidenceSet[transition.NewEvidenceID] {
			return fmt.Errorf("truth transition references unknown evidence")
		}
		for _, hash := range []string{transition.OldHash, transition.NewHash} {
			if len(hash) != 64 {
				return fmt.Errorf("truth transition hash is invalid")
			}
			if _, err := hex.DecodeString(hash); err != nil {
				return fmt.Errorf("truth transition hash is invalid")
			}
		}
	}
	providedID := record.ProvenanceID
	record.ProvenanceID = ""
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode provenance id input: %w", err)
	}
	sum := sha256.Sum256(payload)
	expectedID := "prov_" + hex.EncodeToString(sum[:16])
	if providedID != "" && providedID != expectedID {
		return fmt.Errorf("provenance id does not match record content")
	}
	record.ProvenanceID = expectedID
	if !recordIDPattern.MatchString(record.ProvenanceID) {
		return fmt.Errorf("invalid provenance id")
	}
	payload, err = json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode provenance validation input: %w", err)
	}
	if secretAssignmentPattern.Match(payload) {
		return fmt.Errorf("provenance record must not contain secrets")
	}
	return nil
}

func uniqueKnowledgePacks(values []KnowledgePackReference) ([]KnowledgePackReference, error) {
	seen := map[string]bool{}
	result := make([]KnowledgePackReference, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Origin = strings.TrimSpace(value.Origin)
		if value.ID == "" || value.Origin == "" {
			return nil, fmt.Errorf("knowledge Pack identity and origin are required")
		}
		if seen[value.ID] {
			return nil, fmt.Errorf("duplicate knowledge Pack reference %q", value.ID)
		}
		if value.Origin == "managed" {
			if value.Version == "" || value.Publisher == "" || value.PublisherKeyID == "" || len(value.PackageSHA256) != 64 {
				return nil, fmt.Errorf("managed knowledge Pack %q requires version, publisher, key id, and package hash", value.ID)
			}
			if _, err := hex.DecodeString(value.PackageSHA256); err != nil {
				return nil, fmt.Errorf("managed knowledge Pack package hash is invalid")
			}
		}
		seen[value.ID] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func uniqueSourceRevisions(values []SourceRevision) []SourceRevision {
	seen := map[string]bool{}
	result := make([]SourceRevision, 0, len(values))
	for _, value := range values {
		if value.EvidenceID == "" || seen[value.EvidenceID] {
			continue
		}
		seen[value.EvidenceID] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].EvidenceID < result[j].EvidenceID })
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueArtifacts(values []Artifact) ([]Artifact, error) {
	seen := map[string]bool{}
	result := make([]Artifact, 0, len(values))
	for _, value := range values {
		value.Path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value.Path)))
		if value.Path == "." {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(value.Path)) {
			return nil, fmt.Errorf("artifact path %q is not repository-local", value.Path)
		}
		if seen[value.Path] {
			continue
		}
		if value.Kind == "" {
			value.Kind = "file"
		}
		seen[value.Path] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
