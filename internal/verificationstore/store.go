// Package verificationstore persists immutable verification versions and their
// lightweight source dependencies for deterministic refresh revalidation.
package verificationstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "swipenode.verification-record.v1"

type Repository struct {
	Root      string `json:"root"`
	GitCommit string `json:"git_commit,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
}

type EvidenceReference struct {
	EvidenceID    string  `json:"evidence_id"`
	PackID        string  `json:"pack_id,omitempty"`
	SourceID      string  `json:"source_id"`
	CanonicalURL  string  `json:"canonical_url"`
	SourceType    string  `json:"source_type"`
	Authority     string  `json:"authority"`
	Confidence    float64 `json:"confidence"`
	RetrievedFact string  `json:"retrieved_fact,omitempty"`
	Revision      string  `json:"revision,omitempty"`
	ContentSHA256 string  `json:"content_sha256"`
}

type Claim struct {
	ID           string              `json:"id"`
	ArtifactPath string              `json:"artifact_path"`
	Line         int                 `json:"line"`
	Category     string              `json:"category"`
	Statement    string              `json:"statement"`
	Status       string              `json:"status"`
	Confidence   float64             `json:"confidence"`
	Reason       string              `json:"reason"`
	Evidence     []EvidenceReference `json:"evidence"`
}

type Record struct {
	SchemaVersion        string     `json:"schema_version"`
	ID                   string     `json:"id"`
	RecordedAt           string     `json:"recorded_at"`
	ReportID             string     `json:"report_id"`
	ParentVerificationID string     `json:"parent_verification_id,omitempty"`
	PackID               string     `json:"pack_id,omitempty"`
	Scope                string     `json:"scope"`
	Repository           Repository `json:"repository"`
	Artifacts            []string   `json:"artifacts"`
	Claims               []Claim    `json:"claims"`
}

type Store struct{ dir string }

func Open(stateDir string) *Store {
	return &Store{dir: filepath.Join(stateDir, "verification", "records")}
}

func ClaimID(packID, artifact string, line int, statement string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", packID, artifact, line, statement)))
	return "claim_" + hex.EncodeToString(sum[:16])
}

func (record *Record) prepare() error {
	if record.SchemaVersion != "" && record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported verification record schema %q", record.SchemaVersion)
	}
	record.SchemaVersion = SchemaVersion
	if _, err := time.Parse(time.RFC3339Nano, record.RecordedAt); err != nil {
		return fmt.Errorf("invalid verification record time: %w", err)
	}
	if record.ReportID == "" || record.Repository.Root == "" {
		return fmt.Errorf("report id and repository root are required")
	}
	for index := range record.Artifacts {
		artifact, err := localArtifact(record.Artifacts[index])
		if err != nil {
			return err
		}
		record.Artifacts[index] = artifact
	}
	for index := range record.Claims {
		claim := &record.Claims[index]
		artifact, err := localArtifact(claim.ArtifactPath)
		if err != nil {
			return err
		}
		claim.ArtifactPath = artifact
		if claim.ID == "" {
			claim.ID = ClaimID(record.PackID, claim.ArtifactPath, claim.Line, claim.Statement)
		}
		if claim.ArtifactPath == "" || claim.Statement == "" || claim.Status == "" {
			return fmt.Errorf("claim identity, statement, and status are required")
		}
		for _, evidence := range claim.Evidence {
			if evidence.EvidenceID == "" || evidence.SourceID == "" || evidence.CanonicalURL == "" || len(evidence.ContentSHA256) != 64 {
				return fmt.Errorf("claim evidence relationship is incomplete")
			}
			if strings.ContainsAny(evidence.SourceID, "/\\") {
				return fmt.Errorf("claim evidence source identity is invalid")
			}
			if _, err := hex.DecodeString(evidence.ContentSHA256); err != nil {
				return fmt.Errorf("claim evidence content hash is invalid")
			}
		}
	}
	sort.Strings(record.Artifacts)
	provided := record.ID
	record.ID = ""
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	expected := "vrec_" + hex.EncodeToString(sum[:16])
	if provided != "" && provided != expected {
		return fmt.Errorf("verification record id does not match content")
	}
	record.ID = expected
	return nil
}

func localArtifact(value string) (string, error) {
	value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	if value == "." || !filepath.IsLocal(filepath.FromSlash(value)) {
		return "", fmt.Errorf("verification artifact path is not repository-local")
	}
	return value, nil
}

func (store *Store) Append(record Record) (Record, error) {
	if err := record.prepare(); err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return Record{}, err
	}
	target := filepath.Join(store.dir, record.ID+".json")
	temporary, err := os.CreateTemp(store.dir, ".verification-*.tmp")
	if err != nil {
		return Record{}, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return Record{}, err
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		temporary.Close()
		return Record{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Record{}, err
	}
	if err := temporary.Close(); err != nil {
		return Record{}, err
	}
	if err := os.Link(name, target); err != nil {
		if os.IsExist(err) {
			return Record{}, fmt.Errorf("verification record %s already exists", record.ID)
		}
		return Record{}, err
	}
	return record, nil
}

func (store *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(store.dir)
	if os.IsNotExist(err) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		record, err := read(filepath.Join(store.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if entry.Name() != record.ID+".json" {
			return nil, fmt.Errorf("verification record filename does not match content")
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].RecordedAt > records[j].RecordedAt })
	return records, nil
}

func (store *Store) Find(id string) (Record, bool, error) {
	if !strings.HasPrefix(id, "vrec_") || strings.ContainsAny(id, "/\\") {
		return Record{}, false, nil
	}
	record, err := read(filepath.Join(store.dir, id+".json"))
	if os.IsNotExist(err) {
		return Record{}, false, nil
	}
	if err == nil && record.ID != id {
		return Record{}, false, fmt.Errorf("verification record filename does not match content")
	}
	return record, err == nil, err
}

// LatestDependents returns only leaf verification versions and de-duplicates
// claims by stable claim ID, newest first.
func (store *Store) LatestDependents(packID, sourceID string) ([]Record, error) {
	records, err := store.List()
	if err != nil {
		return nil, err
	}
	superseded := map[string]bool{}
	for _, record := range records {
		if record.ParentVerificationID != "" {
			superseded[record.ParentVerificationID] = true
		}
	}
	seenClaims := map[string]bool{}
	result := []Record{}
	for _, record := range records {
		if record.PackID != packID || superseded[record.ID] {
			continue
		}
		include := false
		for _, claim := range record.Claims {
			depends := false
			for _, evidence := range claim.Evidence {
				if evidence.PackID == packID && evidence.SourceID == sourceID {
					depends = true
					break
				}
			}
			if depends && !seenClaims[claim.ID] {
				seenClaims[claim.ID] = true
				include = true
			}
		}
		if include {
			result = append(result, record)
		}
	}
	return result, nil
}

func read(path string) (Record, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Record{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return Record{}, fmt.Errorf("unsafe verification record")
	}
	file, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Record{}, fmt.Errorf("trailing verification record content")
	}
	if err := record.prepare(); err != nil {
		return Record{}, err
	}
	return record, nil
}
