package distribution

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/trust"
)

func testPack(t *testing.T) (knowledge.Pack, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "knowledge", "builtins", "nvidia-jetson.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pack, err := knowledge.Parse(data, knowledge.OriginManaged)
	if err != nil {
		t.Fatal(err)
	}
	return pack, data
}

func testRelease(t *testing.T, version, previous string, key ed25519.PrivateKey) ([]byte, Package) {
	return testReleaseAt(t, version, previous, key, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
}

func testReleaseAt(t *testing.T, version, previous string, key ed25519.PrivateKey, createdAt time.Time) ([]byte, Package) {
	t.Helper()
	pack, data := testPack(t)
	public := key.Public().(ed25519.PublicKey)
	artifact, pkg, err := Build(pack, data, BuildOptions{PackVersion: version, Publisher: "SwipeNode", CreatedAt: createdAt, SourcePolicyVersion: version, PreviousVersion: previous, KeyID: KeyID(public), PrivateKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return artifact, pkg
}

func TestPackageDeterministicAndSigned(t *testing.T) {
	public, private, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	first, pkg := testRelease(t, "2026.09.1", "", private)
	second, _ := testRelease(t, "2026.09.1", "", private)
	if !bytes.Equal(first, second) {
		t.Fatal("identical release inputs produced different packages")
	}
	verified, err := Verify(first, public, KeyID(public))
	if err != nil {
		t.Fatal(err)
	}
	if verified.PackageSHA256 != pkg.PackageSHA256 || verified.Manifest.PackID != "nvidia-jetson" {
		t.Fatalf("unexpected verified package: %+v", verified.Manifest)
	}
}

func TestPackageTamperingAndWrongPublisherKeyRejected(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	wrongPublic, _, _ := GenerateKeyPair()
	artifact, _ := testRelease(t, "2026.09.1", "", private)
	if _, err := Verify(artifact, wrongPublic, KeyID(wrongPublic)); err == nil {
		t.Fatal("wrong publisher key accepted")
	}
	tampered := append([]byte(nil), artifact...)
	tampered[len(tampered)/2] ^= 1
	if _, err := Verify(tampered, public, KeyID(public)); err == nil {
		t.Fatal("tampered package accepted")
	}
}

func TestPackageRejectsUnsignedTrailingData(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	artifact, _ := testRelease(t, "2026.09.1", "", private)
	artifact = append(artifact, []byte("unsigned trailer")...)
	if _, err := Verify(artifact, public, KeyID(public)); err == nil {
		t.Fatal("unsigned trailing package data accepted")
	}
}

func TestUnsafeArchiveRejected(t *testing.T) {
	var output bytes.Buffer
	writer := tar.NewWriter(&output)
	data := []byte("unsafe")
	if err := writer.WriteHeader(&tar.Header{Name: "../pack.yaml", Mode: 0o644, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	_, _ = writer.Write(data)
	_ = writer.Close()
	if _, err := Inspect(output.Bytes()); err == nil {
		t.Fatal("unsafe archive path accepted")
	}
}

func TestOversizedPackageRejectedBeforeArchiveParsing(t *testing.T) {
	if _, err := Inspect(make([]byte, maxPackageBytes+1)); err == nil {
		t.Fatal("oversized package accepted")
	}
}

func TestProtocolValidatesOriginAndHash(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	artifact, pkg := testRelease(t, "2026.09.1", "", private)
	descriptor := VersionDescriptor{PackID: pkg.Manifest.PackID, Version: pkg.Manifest.PackVersion, Publisher: pkg.Manifest.Publisher, CreatedAt: pkg.Manifest.CreatedAt, ManifestURL: "https://packs.example/v1/packs/nvidia-jetson/versions/2026.09.1", ArtifactURL: "https://packs.example/v1/packs/nvidia-jetson/versions/2026.09.1/artifact", ArtifactSHA256: pkg.PackageSHA256, SignatureAlgorithm: "Ed25519", KeyID: KeyID(public)}
	client := Client{BaseURL: "https://packs.example", Fetch: func(_ context.Context, target string, _ int64) ([]byte, error) {
		if target == descriptor.ArtifactURL {
			return artifact, nil
		}
		return nil, os.ErrNotExist
	}}
	if _, err := client.Download(context.Background(), descriptor); err != nil {
		t.Fatal(err)
	}
	descriptor.ArtifactURL = "https://packs.example.evil.test/artifact"
	if _, err := client.Download(context.Background(), descriptor); err == nil {
		t.Fatal("deceptive registry origin accepted")
	}
	descriptor.ArtifactURL = "https://packs.example/artifact?token=secret"
	if _, err := client.Download(context.Background(), descriptor); err == nil {
		t.Fatal("registry URL with query accepted")
	}
}

func TestStoreInstallActivateUpgradeRollbackAndOfflineLoad(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	publicPEM, _ := MarshalPublicKey(public)
	stateDir := t.TempDir()
	store := OpenStore(stateDir)
	if err := store.AddRemote(Remote{ID: "managed", BaseURL: "https://packs.example", Publisher: "SwipeNode", KeyID: KeyID(public), PublicKeyPEM: string(publicPEM)}); err != nil {
		t.Fatal(err)
	}
	firstArtifact, firstPackage := testRelease(t, "2026.09.1", "", private)
	secondArtifact, secondPackage := testRelease(t, "2026.09.2", "2026.09.1", private)
	if _, installed, err := store.Install(firstArtifact, firstPackage, "managed"); err != nil || !installed {
		t.Fatalf("install first: installed=%t err=%v", installed, err)
	}
	if _, installed, err := store.Install(secondArtifact, secondPackage, "managed"); err != nil || !installed {
		t.Fatalf("install second: installed=%t err=%v", installed, err)
	}
	active, err := store.Activate("nvidia-jetson", "2026.09.1")
	if err != nil || active.Version != "2026.09.1" {
		t.Fatalf("activate first: %+v %v", active, err)
	}
	active, err = store.Activate("nvidia-jetson", "2026.09.2")
	if err != nil || active.Version != "2026.09.2" {
		t.Fatalf("activate second: %+v %v", active, err)
	}
	active, err = store.Rollback("nvidia-jetson")
	if err != nil || active.Version != "2026.09.1" {
		t.Fatalf("rollback: %+v %v", active, err)
	}
	registry, err := knowledge.LoadWithState(t.TempDir(), stateDir)
	if err != nil {
		t.Fatal(err)
	}
	pack, ok := registry.Find("nvidia-jetson")
	if !ok || pack.Origin != knowledge.OriginManaged {
		t.Fatalf("active managed pack unavailable offline: ok=%t origin=%q", ok, pack.Origin)
	}
}

func TestActiveInstalledReleaseReverifiesExactManagedMetadata(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	publicPEM, _ := MarshalPublicKey(public)
	stateDir := t.TempDir()
	store := OpenStore(stateDir)
	if err := store.AddRemote(Remote{ID: "managed", BaseURL: "https://packs.example", Publisher: "SwipeNode", KeyID: KeyID(public), PublicKeyPEM: string(publicPEM)}); err != nil {
		t.Fatal(err)
	}
	artifact, pkg := testRelease(t, "2026.09.3", "", private)
	if _, _, err := store.Install(artifact, pkg, "managed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(pkg.Manifest.PackID, pkg.Manifest.PackVersion); err != nil {
		t.Fatal(err)
	}
	release, ok, err := store.ActiveInstalledRelease(pkg.Manifest.PackID)
	if err != nil || !ok {
		t.Fatalf("active release: ok=%t err=%v", ok, err)
	}
	if release.Version != "2026.09.3" || release.Publisher != "SwipeNode" || release.KeyID != KeyID(public) || release.PackageSHA256 != pkg.PackageSHA256 {
		t.Fatalf("incomplete active release metadata: %#v", release)
	}
}

func TestInstalledArtifactReverifiedBeforeActivation(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	publicPEM, _ := MarshalPublicKey(public)
	stateDir := t.TempDir()
	store := OpenStore(stateDir)
	_ = store.AddRemote(Remote{ID: "managed", BaseURL: "https://packs.example", Publisher: "SwipeNode", KeyID: KeyID(public), PublicKeyPEM: string(publicPEM)})
	artifact, pkg := testRelease(t, "2026.09.1", "", private)
	release, _, err := store.Install(artifact, pkg, "managed")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "distribution", filepath.FromSlash(release.ArtifactPath))
	data, _ := os.ReadFile(path)
	data[len(data)/2] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(pkg.Manifest.PackID, pkg.Manifest.PackVersion); err == nil {
		t.Fatal("tampered installed release activated")
	}
}

func TestTrustPolicyPreservesAuthorizedRollbackButBlocksRevokedInstalledPack(t *testing.T) {
	public, private, _ := GenerateKeyPair()
	publicPEM, _ := MarshalPublicKey(public)
	stateDir := t.TempDir()
	store := OpenStore(stateDir)
	if err := store.AddRemote(Remote{ID: "managed", BaseURL: "https://packs.example", Publisher: "SwipeNode", KeyID: KeyID(public), PublicKeyPEM: string(publicPEM)}); err != nil {
		t.Fatal(err)
	}

	releaseSignedAt := time.Date(2020, 1, 2, 12, 0, 0, 0, time.UTC)
	issuedAt := releaseSignedAt.Add(-time.Hour)
	activeFrom := releaseSignedAt.Add(-24 * time.Hour)
	entry := trust.Key{ID: trust.KeyID(public), Algorithm: "Ed25519", PublicKey: trust.EncodePublicKey(public), CreatedAt: activeFrom.Add(-24 * time.Hour).Format(time.RFC3339Nano), ActiveFrom: activeFrom.Format(time.RFC3339Nano), Purpose: trust.KnowledgePack}
	initial, err := trust.Sign(trust.Document{Purpose: trust.KnowledgePack, Sequence: 1, IssuedAt: issuedAt.Format(time.RFC3339Nano), Keys: []trust.Key{entry}, MinimumVersions: trust.MinimumVersions{Packs: map[string]string{"nvidia-jetson": "2026.09.1"}}}, entry.ID, private)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Authorize(initial, entry.ID, "2026.09.1", "nvidia-jetson", releaseSignedAt, false); err != nil {
		t.Fatalf("valid content signing key was rejected inside its validity window: %v", err)
	}
	if err := trust.Authorize(initial, entry.ID, "2026.09.1", "nvidia-jetson", activeFrom.Add(-time.Second), false); err == nil {
		t.Fatal("content signing key was accepted before its validity window")
	}
	trustStore := trust.Open(stateDir)
	if err := trustStore.InstallInitial(initial, []byte(entry.PublicKey)); err != nil {
		t.Fatal(err)
	}

	firstArtifact, firstPackage := testReleaseAt(t, "2026.09.1", "", private, releaseSignedAt)
	secondArtifact, secondPackage := testReleaseAt(t, "2026.09.2", "2026.09.1", private, releaseSignedAt)
	if _, _, err := store.Install(firstArtifact, firstPackage, "managed"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Install(secondArtifact, secondPackage, "managed"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate("nvidia-jetson", "2026.09.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate("nvidia-jetson", "2026.09.2"); err != nil {
		t.Fatal(err)
	}

	nextEntry := entry
	next := trust.Document{Purpose: trust.KnowledgePack, Sequence: 2, IssuedAt: issuedAt.Add(time.Second).Format(time.RFC3339Nano), Keys: []trust.Key{nextEntry}, MinimumVersions: trust.MinimumVersions{Packs: map[string]string{"nvidia-jetson": "2026.09.2"}}}
	next, err = trust.Sign(next, entry.ID, private)
	if err != nil {
		t.Fatal(err)
	}
	if err := trustStore.Update(next); err != nil {
		t.Fatal(err)
	}
	if active, err := store.Rollback("nvidia-jetson"); err != nil || active.Version != "2026.09.1" || !active.AuthorizedRollback {
		t.Fatalf("authorized rollback was not preserved: active=%#v err=%v", active, err)
	}
	if _, ok, err := store.ActiveInstalledRelease("nvidia-jetson"); err != nil || !ok {
		t.Fatalf("authorized rollback was not usable offline: ok=%t err=%v", ok, err)
	}

	nextEntry.RevokedAt = issuedAt.Add(2 * time.Second).Format(time.RFC3339Nano)
	nextEntry.RevocationReason = "test compromise"
	revoked := trust.Document{Purpose: trust.KnowledgePack, Sequence: 3, IssuedAt: issuedAt.Add(2 * time.Second).Format(time.RFC3339Nano), Keys: []trust.Key{nextEntry}, MinimumVersions: next.MinimumVersions}
	revoked, err = trust.Sign(revoked, entry.ID, private)
	if err != nil {
		t.Fatal(err)
	}
	if err := trustStore.Update(revoked); err != nil {
		t.Fatal(err)
	}
	if err := trust.Authorize(revoked, entry.ID, "2026.09.1", "nvidia-jetson", releaseSignedAt, true); err == nil {
		t.Fatal("revoked content signing key was accepted for an authorized rollback")
	}
	if _, ok, err := store.ActiveInstalledRelease("nvidia-jetson"); err == nil || ok {
		t.Fatalf("revoked installed Pack remained usable: ok=%t err=%v", ok, err)
	}
}

func TestPrivateSigningKeyFileRequiresPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission check is not applicable")
	}
	_, private, _ := GenerateKeyPair()
	pem, err := MarshalPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "publisher.key")
	if err := os.WriteFile(path, pem, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateKeyFile(path); err == nil {
		t.Fatal("group/world-readable signing key accepted")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateKeyFile(path); err != nil {
		t.Fatalf("private signing key rejected: %v", err)
	}
}

func TestRegistryDocumentStrictDiscovery(t *testing.T) {
	document := RegistryDocument{SchemaVersion: RegistrySchemaVersion, GeneratedAt: "2026-09-01T12:00:00Z", Packs: []PackSummary{{ID: "nvidia-jetson", LatestVersion: "2026.09.1", Publisher: "SwipeNode", UpdatedAt: "2026-09-01T12:00:00Z", DescriptorURL: "https://packs.example/v1/packs/nvidia-jetson"}}}
	data, _ := json.Marshal(document)
	client := Client{BaseURL: "https://packs.example", Fetch: func(context.Context, string, int64) ([]byte, error) { return data, nil }}
	got, err := client.Registry(context.Background())
	if err != nil || len(got.Packs) != 1 {
		t.Fatalf("registry discovery: %+v %v", got, err)
	}
	hash := sha256.Sum256(data)
	if len(hex.EncodeToString(hash[:])) != 64 {
		t.Fatal("invalid test hash")
	}
}
