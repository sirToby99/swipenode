package provenance

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxRecordBytes = 1 << 20

type Store struct{ dir string }

func Open(stateDir string) *Store {
	return &Store{dir: filepath.Join(stateDir, "provenance", "records")}
}

func (s *Store) Append(record EngineeringProvenanceRecord) (EngineeringProvenanceRecord, error) {
	if err := record.Prepare(); err != nil {
		return EngineeringProvenanceRecord{}, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("create provenance store: %w", err)
	}
	target := filepath.Join(s.dir, record.ProvenanceID+".json")
	if _, err := os.Stat(target); err == nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("provenance record %s already exists", record.ProvenanceID)
	} else if !os.IsNotExist(err) {
		return EngineeringProvenanceRecord{}, fmt.Errorf("inspect provenance target: %w", err)
	}
	temporary, err := os.CreateTemp(s.dir, ".record-*.tmp")
	if err != nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("create provenance temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return EngineeringProvenanceRecord{}, fmt.Errorf("secure provenance temporary file: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		temporary.Close()
		return EngineeringProvenanceRecord{}, fmt.Errorf("encode provenance record: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return EngineeringProvenanceRecord{}, fmt.Errorf("sync provenance record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("close provenance record: %w", err)
	}
	// Link commits the immutable record without ever replacing an existing path.
	if err := os.Link(temporaryName, target); err != nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("commit provenance record: %w", err)
	}
	_ = os.Remove(temporaryName)
	return record, nil
}

func (s *Store) List() ([]EngineeringProvenanceRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return []EngineeringProvenanceRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read provenance store: %w", err)
	}
	records := make([]EngineeringProvenanceRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !recordIDPattern.MatchString(id) {
			return nil, fmt.Errorf("invalid provenance filename %q", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect provenance record: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Size() > maxRecordBytes {
			return nil, fmt.Errorf("unsafe provenance record %q", entry.Name())
		}
		record, err := readRecord(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if record.ProvenanceID != id {
			return nil, fmt.Errorf("provenance filename does not match content")
		}
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Timestamp == records[j].Timestamp {
			return records[i].ProvenanceID > records[j].ProvenanceID
		}
		return records[i].Timestamp > records[j].Timestamp
	})
	return records, nil
}

func (s *Store) Find(id string) (EngineeringProvenanceRecord, bool, error) {
	if !recordIDPattern.MatchString(id) {
		return EngineeringProvenanceRecord{}, false, nil
	}
	record, err := readRecord(filepath.Join(s.dir, id+".json"))
	if os.IsNotExist(err) {
		return EngineeringProvenanceRecord{}, false, nil
	}
	if err != nil {
		return EngineeringProvenanceRecord{}, false, err
	}
	if record.ProvenanceID != id {
		return EngineeringProvenanceRecord{}, false, fmt.Errorf("provenance filename does not match content")
	}
	return record, true, nil
}

func readRecord(path string) (EngineeringProvenanceRecord, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return EngineeringProvenanceRecord{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecordBytes {
		return EngineeringProvenanceRecord{}, fmt.Errorf("unsafe provenance record %q", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return EngineeringProvenanceRecord{}, err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxRecordBytes+1))
	decoder.DisallowUnknownFields()
	var record EngineeringProvenanceRecord
	if err := decoder.Decode(&record); err != nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("parse provenance record: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return EngineeringProvenanceRecord{}, fmt.Errorf("parse provenance record: trailing JSON content")
	}
	if err := record.Prepare(); err != nil {
		return EngineeringProvenanceRecord{}, fmt.Errorf("validate provenance record: %w", err)
	}
	return record, nil
}
