// Package distribution implements SwipeNode's customer-data-free Knowledge
// Pack release format. Release artifacts contain policy metadata only.
package distribution

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/knowledge"
)

const (
	ReleaseSchemaVersion   = "swipenode.pack-release.v1"
	SignatureSchemaVersion = "swipenode.pack-signature.v1"
	PackageMediaType       = "application/vnd.swipenode.pack-release.v1+tar"
	maxPackageBytes        = 4 << 20
)

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type FileDescriptor struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ReleaseManifest struct {
	SchemaVersion       string           `json:"schema_version"`
	PackID              string           `json:"pack_id"`
	PackVersion         string           `json:"pack_version"`
	PackSchemaVersion   string           `json:"pack_schema_version"`
	Publisher           string           `json:"publisher"`
	CreatedAt           string           `json:"created_at"`
	SourcePolicyVersion string           `json:"source_policy_version"`
	PreviousVersion     string           `json:"previous_version,omitempty"`
	ReleaseNotes        string           `json:"release_notes,omitempty"`
	Files               []FileDescriptor `json:"files"`
	SignatureAlgorithm  string           `json:"signature_algorithm"`
	KeyID               string           `json:"key_id"`
}

type Signature struct {
	SchemaVersion string `json:"schema_version"`
	Algorithm     string `json:"algorithm"`
	KeyID         string `json:"key_id"`
	SignedSHA256  string `json:"signed_sha256"`
	Value         string `json:"signature"`
}

type BuildOptions struct {
	PackVersion         string
	Publisher           string
	CreatedAt           time.Time
	SourcePolicyVersion string
	PreviousVersion     string
	ReleaseNotes        string
	KeyID               string
	PrivateKey          ed25519.PrivateKey
}

type Package struct {
	Manifest      ReleaseManifest
	ManifestBytes []byte
	PackBytes     []byte
	Signature     Signature
	PackageSHA256 string
}

func Build(pack knowledge.Pack, packBytes []byte, options BuildOptions) ([]byte, Package, error) {
	if err := validateBuildOptions(options); err != nil {
		return nil, Package{}, err
	}
	parsed, err := knowledge.Parse(packBytes, knowledge.OriginManaged)
	if err != nil {
		return nil, Package{}, fmt.Errorf("validate packaged knowledge policy: %w", err)
	}
	if parsed.ID != pack.ID || parsed.SchemaVersion != pack.SchemaVersion {
		return nil, Package{}, fmt.Errorf("pack identity does not match packaged policy")
	}
	packHash := sha256.Sum256(packBytes)
	manifest := ReleaseManifest{
		SchemaVersion: ReleaseSchemaVersion, PackID: pack.ID, PackVersion: options.PackVersion,
		PackSchemaVersion: pack.SchemaVersion, Publisher: strings.TrimSpace(options.Publisher),
		CreatedAt: options.CreatedAt.UTC().Format(time.RFC3339Nano), SourcePolicyVersion: options.SourcePolicyVersion,
		PreviousVersion: options.PreviousVersion, ReleaseNotes: strings.TrimSpace(options.ReleaseNotes),
		Files:              []FileDescriptor{{Path: "pack.yaml", SHA256: hex.EncodeToString(packHash[:]), Size: int64(len(packBytes))}},
		SignatureAlgorithm: "Ed25519", KeyID: options.KeyID,
	}
	manifestBytes, err := canonicalJSON(manifest)
	if err != nil {
		return nil, Package{}, err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	signature := Signature{SchemaVersion: SignatureSchemaVersion, Algorithm: "Ed25519", KeyID: options.KeyID, SignedSHA256: hex.EncodeToString(manifestHash[:]), Value: base64.StdEncoding.EncodeToString(ed25519.Sign(options.PrivateKey, manifestBytes))}
	signatureBytes, err := canonicalJSON(signature)
	if err != nil {
		return nil, Package{}, err
	}
	artifact, err := deterministicTar(map[string][]byte{"release.json": manifestBytes, "pack.yaml": packBytes, "signature.json": signatureBytes})
	if err != nil {
		return nil, Package{}, err
	}
	packageHash := sha256.Sum256(artifact)
	return artifact, Package{Manifest: manifest, ManifestBytes: manifestBytes, PackBytes: append([]byte(nil), packBytes...), Signature: signature, PackageSHA256: hex.EncodeToString(packageHash[:])}, nil
}

func Inspect(artifact []byte) (Package, error) {
	if len(artifact) == 0 || len(artifact) > maxPackageBytes {
		return Package{}, fmt.Errorf("package size is invalid")
	}
	files, err := readTar(artifact)
	if err != nil {
		return Package{}, err
	}
	canonicalArchive, err := deterministicTar(files)
	if err != nil || !bytes.Equal(canonicalArchive, artifact) {
		return Package{}, fmt.Errorf("package archive is not canonical or contains trailing data")
	}
	if len(files) != 3 || files["release.json"] == nil || files["pack.yaml"] == nil || files["signature.json"] == nil {
		return Package{}, fmt.Errorf("package must contain exactly release.json, pack.yaml, and signature.json")
	}
	var manifest ReleaseManifest
	if err := decodeStrict(files["release.json"], &manifest); err != nil {
		return Package{}, fmt.Errorf("parse release manifest: %w", err)
	}
	if err := validateManifest(manifest); err != nil {
		return Package{}, err
	}
	canonical, err := canonicalJSON(manifest)
	if err != nil || !bytes.Equal(canonical, files["release.json"]) {
		return Package{}, fmt.Errorf("release manifest is not canonical")
	}
	var signature Signature
	if err := decodeStrict(files["signature.json"], &signature); err != nil {
		return Package{}, fmt.Errorf("parse release signature: %w", err)
	}
	canonicalSignature, err := canonicalJSON(signature)
	if err != nil || !bytes.Equal(canonicalSignature, files["signature.json"]) {
		return Package{}, fmt.Errorf("release signature is not canonical")
	}
	if signature.SchemaVersion != SignatureSchemaVersion || signature.Algorithm != "Ed25519" || signature.KeyID != manifest.KeyID {
		return Package{}, fmt.Errorf("signature metadata does not match release manifest")
	}
	manifestHash := sha256.Sum256(files["release.json"])
	if signature.SignedSHA256 != hex.EncodeToString(manifestHash[:]) {
		return Package{}, fmt.Errorf("signed manifest hash does not match")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "pack.yaml" {
		return Package{}, fmt.Errorf("release file manifest is invalid")
	}
	packHash := sha256.Sum256(files["pack.yaml"])
	if manifest.Files[0].SHA256 != hex.EncodeToString(packHash[:]) || manifest.Files[0].Size != int64(len(files["pack.yaml"])) {
		return Package{}, fmt.Errorf("pack file hash or size does not match release manifest")
	}
	pack, err := knowledge.Parse(files["pack.yaml"], knowledge.OriginManaged)
	if err != nil {
		return Package{}, fmt.Errorf("validate packaged knowledge policy: %w", err)
	}
	if pack.ID != manifest.PackID || pack.SchemaVersion != manifest.PackSchemaVersion {
		return Package{}, fmt.Errorf("pack identity does not match release manifest")
	}
	packageHash := sha256.Sum256(artifact)
	return Package{Manifest: manifest, ManifestBytes: files["release.json"], PackBytes: files["pack.yaml"], Signature: signature, PackageSHA256: hex.EncodeToString(packageHash[:])}, nil
}

func Verify(artifact []byte, publicKey ed25519.PublicKey, expectedKeyID string) (Package, error) {
	pkg, err := Inspect(artifact)
	if err != nil {
		return Package{}, err
	}
	if expectedKeyID != "" && pkg.Manifest.KeyID != expectedKeyID {
		return Package{}, fmt.Errorf("release key id %q does not match trusted key %q", pkg.Manifest.KeyID, expectedKeyID)
	}
	value, err := base64.StdEncoding.DecodeString(pkg.Signature.Value)
	if err != nil || len(value) != ed25519.SignatureSize {
		return Package{}, fmt.Errorf("release signature encoding is invalid")
	}
	if !ed25519.Verify(publicKey, pkg.ManifestBytes, value) {
		return Package{}, fmt.Errorf("release signature is not valid for the trusted publisher key")
	}
	return pkg, nil
}

func GenerateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func MarshalPrivateKey(key ed25519.PrivateKey) ([]byte, error) {
	value, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: value}), nil
}

func ParsePrivateKey(data []byte) (ed25519.PrivateKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("private signing key must be one PKCS#8 PEM block")
	}
	value, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private signing key: %w", err)
	}
	key, ok := value.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private signing key is not Ed25519")
	}
	return key, nil
}

// ReadPrivateKeyFile accepts only a small regular file and, on platforms with
// POSIX permission bits, refuses a key readable or writable by group/others.
func ReadPrivateKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 {
		return nil, fmt.Errorf("unsafe private signing key file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("private signing key permissions must not grant group or other access")
	}
	return os.ReadFile(filepath.Clean(path))
}

func MarshalPublicKey(key ed25519.PublicKey) ([]byte, error) {
	value, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: value}), nil
}

func ParsePublicKey(data []byte) (ed25519.PublicKey, error) {
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("trusted publisher key must be one public-key PEM block")
	}
	value, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse trusted publisher key: %w", err)
	}
	key, ok := value.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("trusted publisher key is not Ed25519")
	}
	return key, nil
}

func KeyID(key ed25519.PublicKey) string {
	hash := sha256.Sum256(key)
	return "ed25519:" + hex.EncodeToString(hash[:12])
}

func ReadArtifact(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPackageBytes {
		return nil, fmt.Errorf("unsafe or oversized package artifact")
	}
	return os.ReadFile(path)
}

func WriteArtifact(path string, data []byte, mode os.FileMode) error {
	if len(data) == 0 || len(data) > maxPackageBytes {
		return fmt.Errorf("artifact size is invalid")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".swipenode-package-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
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
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("refusing to overwrite %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(name, path)
}

func validateBuildOptions(options BuildOptions) error {
	if !versionPattern.MatchString(options.PackVersion) || !versionPattern.MatchString(options.SourcePolicyVersion) {
		return fmt.Errorf("pack and source-policy versions must be stable identifiers")
	}
	if options.PreviousVersion != "" && !versionPattern.MatchString(options.PreviousVersion) {
		return fmt.Errorf("previous version is invalid")
	}
	if strings.TrimSpace(options.Publisher) == "" || len(options.Publisher) > 200 || !keyIDPattern.MatchString(options.KeyID) {
		return fmt.Errorf("publisher and valid key id are required")
	}
	if options.CreatedAt.IsZero() || len(options.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("created_at and Ed25519 private key are required")
	}
	return nil
}

func validateManifest(manifest ReleaseManifest) error {
	if manifest.SchemaVersion != ReleaseSchemaVersion || manifest.PackID == "" || !versionPattern.MatchString(manifest.PackVersion) || manifest.PackSchemaVersion != knowledge.SchemaVersion {
		return fmt.Errorf("release identity or schema is invalid")
	}
	if strings.TrimSpace(manifest.Publisher) == "" || !keyIDPattern.MatchString(manifest.KeyID) || manifest.SignatureAlgorithm != "Ed25519" || !versionPattern.MatchString(manifest.SourcePolicyVersion) {
		return fmt.Errorf("release publisher or signature policy is invalid")
	}
	if manifest.PreviousVersion != "" && !versionPattern.MatchString(manifest.PreviousVersion) {
		return fmt.Errorf("release previous version is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.CreatedAt); err != nil {
		return fmt.Errorf("release created_at is invalid")
	}
	return nil
}

func canonicalJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func deterministicTar(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	for _, name := range names {
		data := files[name]
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Unix(0, 0).UTC(), ChangeTime: time.Unix(0, 0).UTC(), Format: tar.FormatPAX}
		if err := writer.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func readTar(data []byte) (map[string][]byte, error) {
	reader := tar.NewReader(bytes.NewReader(data))
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read package archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || header.Name == "" || !filepath.IsLocal(filepath.FromSlash(header.Name)) || strings.Contains(header.Name, "\\") || header.Size < 0 || header.Size > maxPackageBytes {
			return nil, fmt.Errorf("unsafe package archive entry")
		}
		if _, exists := files[header.Name]; exists {
			return nil, fmt.Errorf("duplicate package archive entry %q", header.Name)
		}
		value, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(value)) != header.Size {
			return nil, fmt.Errorf("read package entry %q", header.Name)
		}
		files[header.Name] = value
	}
	return files, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON content")
	}
	return nil
}
