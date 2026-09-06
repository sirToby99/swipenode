package verification

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	CatalogSchemaVersion = "swipenode.evidence.v1"
	CatalogRelativePath  = ".swipenode/evidence.json"
	maxCatalogBytes      = 1 << 20
)

type Catalog struct {
	SchemaVersion string         `json:"schema_version"`
	Evidence      []catalogEntry `json:"evidence"`
}

type catalogEntry struct {
	Source      string  `json:"source"`
	SourceType  string  `json:"source_type"`
	Authority   string  `json:"authority"`
	Title       string  `json:"title,omitempty"`
	VersionDate string  `json:"version_date,omitempty"`
	Confidence  float64 `json:"confidence"`
	Content     string  `json:"content"`
}

// LoadCatalog reads the bounded, repository-local evidence catalog. A missing
// catalog is a valid empty offline evidence set.
func LoadCatalog(repoRoot string) ([]Evidence, error) {
	path := filepath.Join(repoRoot, filepath.FromSlash(CatalogRelativePath))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Evidence{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect evidence catalog: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("evidence catalog must be a regular file")
	}
	if info.Size() > maxCatalogBytes {
		return nil, fmt.Errorf("evidence catalog exceeds %d bytes", maxCatalogBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open evidence catalog: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxCatalogBytes+1))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode evidence catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode evidence catalog: trailing JSON content")
	}
	if catalog.SchemaVersion != CatalogSchemaVersion {
		return nil, fmt.Errorf("unsupported evidence catalog schema %q", catalog.SchemaVersion)
	}
	evidenceSet := make([]Evidence, 0, len(catalog.Evidence))
	for index := range catalog.Evidence {
		entry := &catalog.Evidence[index]
		evidence := Evidence{
			Source: entry.Source, SourceType: entry.SourceType, Authority: entry.Authority,
			Title: entry.Title, VersionDate: entry.VersionDate, Confidence: entry.Confidence, Content: entry.Content,
		}
		evidence.Source = cleanMetadata(evidence.Source, 300)
		evidence.SourceType = cleanMetadata(evidence.SourceType, 80)
		evidence.Authority = strings.ToLower(strings.TrimSpace(evidence.Authority))
		evidence.Title = cleanMetadata(evidence.Title, 200)
		evidence.VersionDate = cleanMetadata(evidence.VersionDate, 80)
		evidence.Content = strings.TrimSpace(evidence.Content)
		if evidence.Source == "" || evidence.SourceType == "" || evidence.Content == "" {
			return nil, fmt.Errorf("evidence entry %d requires source, source_type, and content", index)
		}
		if evidence.Authority != "authoritative" && evidence.Authority != "primary" && evidence.Authority != "secondary" && evidence.Authority != "unknown" {
			return nil, fmt.Errorf("evidence entry %d has unsupported authority %q", index, evidence.Authority)
		}
		if evidence.Confidence < 0 || evidence.Confidence > 1 {
			return nil, fmt.Errorf("evidence entry %d confidence must be between 0 and 1", index)
		}
		if strings.HasPrefix(evidence.Source, "http://") || strings.HasPrefix(evidence.Source, "https://") {
			safe, err := SanitizeFetchURL(evidence.Source)
			if err != nil {
				return nil, fmt.Errorf("evidence entry %d has an unsafe source URL", index)
			}
			evidence.Source = safe
			if evidence.URLReference == "" {
				evidence.URLReference = safe
			}
		}
		evidenceSet = append(evidenceSet, evidence)
	}
	return evidenceSet, nil
}

func cleanMetadata(value string, limit int) string {
	return excerpt(strings.Join(strings.Fields(value), " "), limit)
}
