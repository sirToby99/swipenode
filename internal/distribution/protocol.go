package distribution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/extractor"
)

const (
	RegistrySchemaVersion = "swipenode.knowledge-registry.v1"
	RemoteSchemaVersion   = "swipenode.knowledge-remote.v1"
	maxRegistryBytes      = 1 << 20
)

type PackSummary struct {
	ID            string `json:"id"`
	LatestVersion string `json:"latest_version"`
	Publisher     string `json:"publisher"`
	UpdatedAt     string `json:"updated_at"`
	DescriptorURL string `json:"descriptor_url"`
	Health        string `json:"health,omitempty"`
}

type RegistryDocument struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	Packs         []PackSummary `json:"packs"`
}

type VersionDescriptor struct {
	PackID             string `json:"pack_id"`
	Version            string `json:"version"`
	Publisher          string `json:"publisher"`
	CreatedAt          string `json:"created_at"`
	PreviousVersion    string `json:"previous_version,omitempty"`
	ManifestURL        string `json:"manifest_url"`
	ArtifactURL        string `json:"artifact_url"`
	ArtifactSHA256     string `json:"artifact_sha256"`
	SignatureAlgorithm string `json:"signature_algorithm"`
	KeyID              string `json:"key_id"`
	SourceHealth       string `json:"source_health,omitempty"`
}

type PackDescriptor struct {
	SchemaVersion string              `json:"schema_version"`
	PackID        string              `json:"pack_id"`
	LatestVersion string              `json:"latest_version"`
	VersionsURL   string              `json:"versions_url"`
	Latest        VersionDescriptor   `json:"latest"`
	Versions      []VersionDescriptor `json:"versions,omitempty"`
}

type FetchFunc func(context.Context, string, int64) ([]byte, error)

type Client struct {
	BaseURL string
	Fetch   FetchFunc
}

func (client Client) Registry(ctx context.Context) (RegistryDocument, error) {
	var document RegistryDocument
	if err := client.getJSON(ctx, "/v1/packs", &document); err != nil {
		return document, err
	}
	if document.SchemaVersion != RegistrySchemaVersion {
		return document, fmt.Errorf("unsupported registry schema %q", document.SchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339Nano, document.GeneratedAt); err != nil {
		return document, fmt.Errorf("invalid registry timestamp")
	}
	for _, pack := range document.Packs {
		if pack.ID == "" || !versionPattern.MatchString(pack.LatestVersion) || pack.Publisher == "" {
			return document, fmt.Errorf("invalid pack registry entry")
		}
		if err := client.validateServerURL(pack.DescriptorURL); err != nil {
			return document, err
		}
	}
	return document, nil
}

func (client Client) Pack(ctx context.Context, packID string) (PackDescriptor, error) {
	var descriptor PackDescriptor
	if packID == "" || strings.ContainsAny(packID, "/\\") {
		return descriptor, fmt.Errorf("invalid pack id")
	}
	if err := client.getJSON(ctx, "/v1/packs/"+url.PathEscape(packID), &descriptor); err != nil {
		return descriptor, err
	}
	if descriptor.SchemaVersion != RegistrySchemaVersion || descriptor.PackID != packID || descriptor.LatestVersion != descriptor.Latest.Version {
		return descriptor, fmt.Errorf("invalid pack descriptor")
	}
	all := append([]VersionDescriptor{descriptor.Latest}, descriptor.Versions...)
	for _, version := range all {
		if err := client.validateVersion(packID, version); err != nil {
			return descriptor, err
		}
	}
	if err := client.validateServerURL(descriptor.VersionsURL); err != nil {
		return descriptor, err
	}
	return descriptor, nil
}

func (client Client) Download(ctx context.Context, descriptor VersionDescriptor) ([]byte, error) {
	if err := client.validateVersion(descriptor.PackID, descriptor); err != nil {
		return nil, err
	}
	fetch := client.Fetch
	if fetch == nil {
		fetch = guardedFetch
	}
	data, err := fetch(ctx, descriptor.ArtifactURL, maxPackageBytes)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(data)
	if descriptor.ArtifactSHA256 != hex.EncodeToString(hash[:]) {
		return nil, fmt.Errorf("downloaded package hash does not match registry descriptor")
	}
	return data, nil
}

func (client Client) getJSON(ctx context.Context, suffix string, target any) error {
	base, err := client.base()
	if err != nil {
		return err
	}
	endpoint := *base
	endpoint.Path = path.Join(strings.TrimSuffix(base.Path, "/"), suffix)
	endpoint.RawQuery, endpoint.Fragment = "", ""
	fetch := client.Fetch
	if fetch == nil {
		fetch = guardedFetch
	}
	data, err := fetch(ctx, endpoint.String(), maxRegistryBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(data), maxRegistryBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("parse registry response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("registry response has trailing content")
	}
	return nil
}

func (client Client) base() (*url.URL, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(client.BaseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("knowledge remote must be an HTTPS base URL without credentials, query, or fragment")
	}
	return parsed, nil
}

func (client Client) validateServerURL(raw string) error {
	base, err := client.base()
	if err != nil {
		return err
	}
	target, err := url.ParseRequestURI(raw)
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return fmt.Errorf("registry returned an unsafe URL")
	}
	if !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Host, target.Host) {
		return fmt.Errorf("registry URL is outside the configured remote origin")
	}
	return nil
}

func (client Client) validateVersion(packID string, descriptor VersionDescriptor) error {
	if descriptor.PackID != packID || !versionPattern.MatchString(descriptor.Version) || descriptor.Publisher == "" || descriptor.KeyID == "" || descriptor.SignatureAlgorithm != "Ed25519" || len(descriptor.ArtifactSHA256) != 64 {
		return fmt.Errorf("invalid release descriptor")
	}
	if _, err := hex.DecodeString(descriptor.ArtifactSHA256); err != nil {
		return fmt.Errorf("invalid release artifact hash")
	}
	if _, err := time.Parse(time.RFC3339Nano, descriptor.CreatedAt); err != nil {
		return fmt.Errorf("invalid release timestamp")
	}
	if err := client.validateServerURL(descriptor.ManifestURL); err != nil {
		return err
	}
	return client.validateServerURL(descriptor.ArtifactURL)
}

func guardedFetch(ctx context.Context, target string, limit int64) ([]byte, error) {
	result, err := extractor.Download(ctx, target, limit)
	if err != nil {
		return nil, err
	}
	return result.Body, nil
}
