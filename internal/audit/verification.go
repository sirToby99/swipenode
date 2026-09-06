package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sirToby99/swipenode/internal/verification"
)

type verificationSnapshot struct {
	SchemaVersion  string            `json:"schema_version"`
	VerificationID string            `json:"verification_id"`
	Statuses       map[string]string `json:"statuses"`
}

type VerificationTrace struct {
	VerificationID string
	EventIDs       []string
}

// RecordVerification appends a completion event and status transitions, then
// atomically updates the comparison snapshot. It does not depend on Entire.
func (s *Store) RecordVerification(report verification.Report, packID string) (string, error) {
	trace, err := s.RecordVerificationWithEvents(report, packID)
	return trace.VerificationID, err
}

// RecordVerificationWithEvents records verification lifecycle events and
// returns their immutable IDs for Engineering Provenance references.
func (s *Store) RecordVerificationWithEvents(report verification.Report, packID string) (VerificationTrace, error) {
	verificationID, err := VerificationID(report)
	if err != nil {
		return VerificationTrace{}, err
	}
	return s.recordVerification(report, packID, verificationID)
}

// VerificationID is a stable identifier for the exact machine-readable report.
func VerificationID(report verification.Report) (string, error) {
	payload, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("encode verification report: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "vr_" + hex.EncodeToString(sum[:16]), nil
}

func (s *Store) recordVerification(report verification.Report, packID, verificationID string) (VerificationTrace, error) {
	trace := VerificationTrace{VerificationID: verificationID}
	current := map[string]string{}
	for _, claim := range report.Claims {
		key := fmt.Sprintf("%s:%s:%s:%d:%s", packID, report.Scope, claim.Location.Path, claim.Location.Line, claim.Statement)
		current[key] = string(claim.VerificationStatus)
	}
	previous, err := s.readVerificationSnapshot()
	if err != nil {
		return VerificationTrace{}, err
	}
	completed, err := s.Append(Event{Type: VerificationCompleted, PackID: packID, VerificationID: verificationID, VerificationScope: report.Scope})
	if err != nil {
		return VerificationTrace{}, err
	}
	trace.EventIDs = append(trace.EventIDs, completed.ID)
	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		oldStatus, existed := previous.Statuses[key]
		if !existed || oldStatus == current[key] {
			continue
		}
		changed, err := s.Append(Event{Type: VerificationStatusChanged, PackID: packID, VerificationID: verificationID, VerificationScope: report.Scope, OldStatus: oldStatus, NewStatus: current[key]})
		if err != nil {
			return VerificationTrace{}, err
		}
		trace.EventIDs = append(trace.EventIDs, changed.ID)
	}
	if err := s.writeVerificationSnapshot(verificationSnapshot{SchemaVersion: "swipenode.verification-snapshot.v1", VerificationID: verificationID, Statuses: current}); err != nil {
		return VerificationTrace{}, err
	}
	return trace, nil
}

func (s *Store) readVerificationSnapshot() (verificationSnapshot, error) {
	result := verificationSnapshot{Statuses: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(s.dir, "verification-status.json"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read verification status: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, fmt.Errorf("parse verification status: %w", err)
	}
	if result.Statuses == nil {
		result.Statuses = map[string]string{}
	}
	return result, nil
}

func (s *Store) writeVerificationSnapshot(snapshot verificationSnapshot) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode verification status: %w", err)
	}
	temporary := filepath.Join(s.dir, "verification-status.json.tmp")
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write verification status: %w", err)
	}
	if err := os.Rename(temporary, filepath.Join(s.dir, "verification-status.json")); err != nil {
		return fmt.Errorf("replace verification status: %w", err)
	}
	return nil
}
