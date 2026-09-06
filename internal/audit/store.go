// Package audit persists SwipeNode-native lifecycle events without Entire.
package audit

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/localstate"
)

const SchemaVersion = "swipenode.audit-event.v1"

type EventType string

const (
	PackAdded                 EventType = "pack_added"
	PackUpdated               EventType = "pack_updated"
	SourceChecked             EventType = "source_checked"
	SourceUnchanged           EventType = "source_unchanged"
	SourceChanged             EventType = "source_changed"
	EvidenceCreated           EventType = "evidence_created"
	VerificationCompleted     EventType = "verification_completed"
	VerificationStatusChanged EventType = "verification_status_changed"
	RevalidationStarted       EventType = "revalidation_started"
	VerificationRevalidated   EventType = "verification_revalidated"
)

type Event struct {
	SchemaVersion     string    `json:"schema_version"`
	ID                string    `json:"id"`
	Type              EventType `json:"event"`
	Timestamp         string    `json:"timestamp"`
	PackID            string    `json:"pack_id,omitempty"`
	SourceID          string    `json:"source_id,omitempty"`
	EvidenceID        string    `json:"evidence_id,omitempty"`
	VerificationID    string    `json:"verification_id,omitempty"`
	OldVerificationID string    `json:"old_verification_id,omitempty"`
	NewVerificationID string    `json:"new_verification_id,omitempty"`
	ClaimID           string    `json:"claim_id,omitempty"`
	ArtifactPath      string    `json:"artifact_path,omitempty"`
	OldEvidenceID     string    `json:"old_evidence_id,omitempty"`
	NewEvidenceID     string    `json:"new_evidence_id,omitempty"`
	FailureKind       string    `json:"failure_kind,omitempty"`
	CheckMethod       string    `json:"check_method,omitempty"`
	CanonicalURL      string    `json:"canonical_url,omitempty"`
	OldHash           string    `json:"old_hash,omitempty"`
	NewHash           string    `json:"new_hash,omitempty"`
	OldRevision       string    `json:"old_revision,omitempty"`
	NewRevision       string    `json:"new_revision,omitempty"`
	OldStatus         string    `json:"old_status,omitempty"`
	NewStatus         string    `json:"new_status,omitempty"`
	VerificationScope string    `json:"verification_scope,omitempty"`
}

type Store struct {
	dir   string
	now   func() time.Time
	newID func() (string, error)
}

func Open(stateDir string) *Store {
	return &Store{dir: stateDir, now: time.Now, newID: randomID}
}

func (s *Store) Append(event Event) (Event, error) {
	switch event.Type {
	case PackAdded, PackUpdated, SourceChecked, SourceUnchanged, SourceChanged, EvidenceCreated, VerificationCompleted, VerificationStatusChanged, RevalidationStarted, VerificationRevalidated:
	default:
		return Event{}, fmt.Errorf("unsupported audit event type %q", strings.TrimSpace(string(event.Type)))
	}
	if event.ID == "" {
		id, err := s.newID()
		if err != nil {
			return Event{}, fmt.Errorf("create audit event id: %w", err)
		}
		event.ID = id
	}
	if event.Timestamp == "" {
		event.Timestamp = s.now().UTC().Format(time.RFC3339Nano)
	}
	event.SchemaVersion = SchemaVersion
	if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
		return Event{}, fmt.Errorf("invalid audit timestamp: %w", err)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Event{}, fmt.Errorf("create audit directory: %w", err)
	}
	f, err := localstate.OpenAppendOnly(filepath.Join(s.dir, "audit.jsonl"), 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("open audit store: %w", err)
	}
	encoder := json.NewEncoder(f)
	err = encoder.Encode(event)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return Event{}, fmt.Errorf("append audit event: %w", err)
	}
	if closeErr != nil {
		return Event{}, fmt.Errorf("close audit store: %w", closeErr)
	}
	return event, nil
}

func (s *Store) List() ([]Event, error) {
	path := filepath.Join(s.dir, "audit.jsonl")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return []Event{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect audit store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unsafe audit store")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audit store: %w", err)
	}
	defer f.Close()
	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("read audit event %d: invalid JSON; prior Audit events remain unchanged: %w", line, err)
		}
		if event.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("read audit event %d: unsupported schema %q", line, event.SchemaVersion)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit store: %w", err)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp > events[j].Timestamp })
	return events, nil
}

func (s *Store) Find(id string) (Event, bool, error) {
	events, err := s.List()
	if err != nil {
		return Event{}, false, err
	}
	for _, event := range events {
		if event.ID == id {
			return event, true, nil
		}
	}
	return Event{}, false, nil
}

func randomID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "evt_" + hex.EncodeToString(value[:]), nil
}
