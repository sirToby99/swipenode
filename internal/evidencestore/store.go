// Package evidencestore keeps immutable, source-derived evidence versions and
// mutable last-checked source state in the repository's local Git metadata.
package evidencestore

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/localstate"
)

const SchemaVersion = "swipenode.evidence-record.v1"

type Provenance struct {
	CanonicalURL string `json:"canonical_url"`
	Owner        string `json:"owner"`
	SourceType   string `json:"source_type"`
}

type VerificationRelationship struct {
	VerificationID string `json:"verification_id,omitempty"`
	ClaimID        string `json:"claim_id,omitempty"`
}

type Record struct {
	SchemaVersion    string                   `json:"schema_version"`
	ID               string                   `json:"id"`
	PackID           string                   `json:"pack_id,omitempty"`
	SourceID         string                   `json:"source_id"`
	CanonicalURL     string                   `json:"canonical_url"`
	Owner            string                   `json:"owner"`
	SourceType       string                   `json:"source_type"`
	RetrievedAt      string                   `json:"retrieved_at"`
	DocumentRevision string                   `json:"document_revision,omitempty"`
	ETag             string                   `json:"etag,omitempty"`
	LastModified     string                   `json:"last_modified,omitempty"`
	ContentSHA256    string                   `json:"content_sha256"`
	ExtractorVersion string                   `json:"extractor_version,omitempty"`
	ExtractedFact    string                   `json:"extracted_fact,omitempty"`
	Provenance       Provenance               `json:"provenance"`
	Verification     VerificationRelationship `json:"verification,omitempty"`
}

type SourceState struct {
	PackID            string `json:"pack_id"`
	SourceID          string `json:"source_id"`
	CanonicalURL      string `json:"canonical_url"`
	LastChecked       string `json:"last_checked"`
	LastChanged       string `json:"last_changed,omitempty"`
	CurrentEvidenceID string `json:"current_evidence_id"`
	ContentSHA256     string `json:"content_sha256"`
	DocumentRevision  string `json:"document_revision,omitempty"`
	ETag              string `json:"etag,omitempty"`
	LastModified      string `json:"last_modified,omitempty"`
	Health            string `json:"health"`
	Status            string `json:"status"`
}

type Store struct{ dir string }

func Open(stateDir string) *Store { return &Store{dir: stateDir} }

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func EvidenceID(record Record) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{record.PackID, record.SourceID, record.CanonicalURL, record.ContentSHA256, record.DocumentRevision, record.RetrievedAt}, "\x00")))
	return "ev_" + hex.EncodeToString(sum[:16])
}

func (s *Store) Insert(record Record) (Record, error) {
	if record.SourceID == "" || record.CanonicalURL == "" || record.Owner == "" || record.SourceType == "" || record.RetrievedAt == "" || record.ContentSHA256 == "" {
		return Record{}, fmt.Errorf("source identity, provenance, retrieval time, and content hash are required")
	}
	parsed, err := url.ParseRequestURI(record.CanonicalURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return Record{}, fmt.Errorf("canonical evidence URL is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.RetrievedAt); err != nil {
		return Record{}, fmt.Errorf("invalid evidence retrieval time: %w", err)
	}
	if len(record.ContentSHA256) != 64 {
		return Record{}, fmt.Errorf("content_sha256 must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(record.ContentSHA256); err != nil {
		return Record{}, fmt.Errorf("content_sha256 must be a SHA-256 hex digest")
	}
	record.SchemaVersion = SchemaVersion
	if record.Provenance.CanonicalURL == "" {
		record.Provenance = Provenance{CanonicalURL: record.CanonicalURL, Owner: record.Owner, SourceType: record.SourceType}
	}
	if record.ID == "" {
		record.ID = EvidenceID(record)
	}
	records, err := s.List()
	if err != nil {
		return Record{}, err
	}
	for _, existing := range records {
		if existing.ID == record.ID {
			return Record{}, fmt.Errorf("evidence %s already exists", record.ID)
		}
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Record{}, fmt.Errorf("create evidence directory: %w", err)
	}
	f, err := localstate.OpenAppendOnly(filepath.Join(s.dir, "evidence.jsonl"), 0o600)
	if err != nil {
		return Record{}, fmt.Errorf("open evidence store: %w", err)
	}
	err = json.NewEncoder(f).Encode(record)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return Record{}, fmt.Errorf("append evidence: %w", err)
	}
	if closeErr != nil {
		return Record{}, fmt.Errorf("close evidence store: %w", closeErr)
	}
	return record, nil
}

func (s *Store) List() ([]Record, error) {
	path := filepath.Join(s.dir, "evidence.jsonl")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect evidence store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe evidence store")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open evidence store: %w", err)
	}
	defer f.Close()
	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("read evidence record %d: invalid JSON; prior Evidence records remain unchanged: %w", line, err)
		}
		if record.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("read evidence record %d: unsupported schema %q", line, record.SchemaVersion)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read evidence store: %w", err)
	}
	return records, nil
}

func (s *Store) Find(id string) (Record, bool, error) {
	records, err := s.List()
	if err != nil {
		return Record{}, false, err
	}
	for _, record := range records {
		if record.ID == id {
			return record, true, nil
		}
	}
	return Record{}, false, nil
}

func sourceKey(packID, sourceID, canonicalURL string) string {
	return packID + "\x00" + sourceID + "\x00" + canonicalURL
}

func (s *Store) SourceStates() ([]SourceState, error) {
	path := filepath.Join(s.dir, "sources.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return []SourceState{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read source state: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, fmt.Errorf("unsafe source state file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read source state: %w", err)
	}
	var states []SourceState
	if err := json.Unmarshal(data, &states); err != nil {
		return nil, fmt.Errorf("parse source state: invalid state; immutable Evidence history remains unchanged: %w", err)
	}
	seen := map[string]bool{}
	for _, state := range states {
		if err := validateSourceState(state); err != nil {
			return nil, fmt.Errorf("parse source state: %w", err)
		}
		key := sourceKey(state.PackID, state.SourceID, state.CanonicalURL)
		if seen[key] {
			return nil, fmt.Errorf("parse source state: duplicate source identity")
		}
		seen[key] = true
	}
	return states, nil
}

func (s *Store) SourceState(packID, sourceID, canonicalURL string) (SourceState, bool, error) {
	states, err := s.SourceStates()
	if err != nil {
		return SourceState{}, false, err
	}
	key := sourceKey(packID, sourceID, canonicalURL)
	for _, state := range states {
		if sourceKey(state.PackID, state.SourceID, state.CanonicalURL) == key {
			return state, true, nil
		}
	}
	return SourceState{}, false, nil
}

func (s *Store) UpdateSourceState(state SourceState) error {
	if state.Health == "" {
		state.Health = "healthy"
	}
	if state.Status == "" {
		state.Status = "unchanged"
	}
	if err := validateSourceState(state); err != nil {
		return err
	}
	states, err := s.SourceStates()
	if err != nil {
		return err
	}
	key := sourceKey(state.PackID, state.SourceID, state.CanonicalURL)
	found := false
	for i := range states {
		if sourceKey(states[i].PackID, states[i].SourceID, states[i].CanonicalURL) == key {
			states[i] = state
			found = true
			break
		}
	}
	if !found {
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool {
		return sourceKey(states[i].PackID, states[i].SourceID, states[i].CanonicalURL) < sourceKey(states[j].PackID, states[j].SourceID, states[j].CanonicalURL)
	})
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create evidence directory: %w", err)
	}
	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return fmt.Errorf("encode source state: %w", err)
	}
	temporaryFile, err := os.CreateTemp(s.dir, ".sources-*.tmp")
	if err != nil {
		return fmt.Errorf("create source state temporary file: %w", err)
	}
	temporary := temporaryFile.Name()
	defer os.Remove(temporary)
	if err := temporaryFile.Chmod(0o600); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("secure source state temporary file: %w", err)
	}
	if _, err := temporaryFile.Write(append(data, '\n')); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("write source state: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("sync source state: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close source state: %w", err)
	}
	if err := os.Rename(temporary, filepath.Join(s.dir, "sources.json")); err != nil {
		return fmt.Errorf("replace source state: %w", err)
	}
	if err := syncDirectory(s.dir); err != nil {
		return fmt.Errorf("sync source state directory: %w", err)
	}
	return nil
}

func validateSourceState(state SourceState) error {
	if state.SourceID == "" || strings.ContainsAny(state.SourceID, "/\\") || state.CanonicalURL == "" || state.LastChecked == "" {
		return fmt.Errorf("source state identity and last_checked are required")
	}
	parsed, err := url.ParseRequestURI(state.CanonicalURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("source state canonical URL is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, state.LastChecked); err != nil {
		return fmt.Errorf("invalid source last_checked: %w", err)
	}
	if state.LastChanged != "" {
		if _, err := time.Parse(time.RFC3339Nano, state.LastChanged); err != nil {
			return fmt.Errorf("invalid source last_changed: %w", err)
		}
	}
	if state.ContentSHA256 != "" {
		if len(state.ContentSHA256) != 64 {
			return fmt.Errorf("source state content hash is invalid")
		}
		if _, err := hex.DecodeString(state.ContentSHA256); err != nil {
			return fmt.Errorf("source state content hash is invalid")
		}
		if state.CurrentEvidenceID == "" {
			return fmt.Errorf("source state evidence relationship is incomplete")
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
