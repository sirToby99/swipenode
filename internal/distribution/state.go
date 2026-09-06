package distribution

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/knowledge"
	"github.com/sirToby99/swipenode/internal/trust"
)

const StateSchemaVersion = "swipenode.knowledge-distribution-state.v1"

type UpdatePolicy string

const (
	PolicyManual       UpdatePolicy = "manual"
	PolicyAutoDownload UpdatePolicy = "auto-download"
	PolicyAutoUpdate   UpdatePolicy = "auto-update"
)

type Remote struct {
	ID           string `json:"id"`
	BaseURL      string `json:"base_url"`
	Publisher    string `json:"publisher"`
	KeyID        string `json:"key_id"`
	PublicKeyPEM string `json:"public_key_pem"`
}

type InstalledRelease struct {
	PackID          string `json:"pack_id"`
	Version         string `json:"version"`
	Publisher       string `json:"publisher"`
	KeyID           string `json:"key_id"`
	CreatedAt       string `json:"created_at"`
	PreviousVersion string `json:"previous_version,omitempty"`
	PackageSHA256   string `json:"package_sha256"`
	InstalledAt     string `json:"installed_at"`
	RemoteID        string `json:"remote_id,omitempty"`
	ArtifactPath    string `json:"artifact_path"`
}

type ActiveRelease struct {
	PackID             string `json:"pack_id"`
	Version            string `json:"version"`
	ActivatedAt        string `json:"activated_at"`
	PackageSHA256      string `json:"package_sha256"`
	AuthorizedRollback bool   `json:"authorized_rollback,omitempty"`
}

type State struct {
	SchemaVersion string                        `json:"schema_version"`
	Policy        UpdatePolicy                  `json:"policy"`
	Remotes       []Remote                      `json:"remotes"`
	Installed     map[string][]InstalledRelease `json:"installed"`
	Active        map[string]ActiveRelease      `json:"active"`
	History       map[string][]string           `json:"activation_history"`
}

type Store struct {
	stateDir string
	dir      string
	now      func() time.Time
}

func OpenStore(stateDir string) *Store {
	return &Store{stateDir: stateDir, dir: filepath.Join(stateDir, "distribution"), now: time.Now}
}

func (store *Store) Load() (State, error) {
	state := State{SchemaVersion: StateSchemaVersion, Policy: PolicyManual, Remotes: []Remote{}, Installed: map[string][]InstalledRelease{}, Active: map[string]ActiveRelease{}, History: map[string][]string{}}
	path := filepath.Join(store.dir, "state.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return State{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return State{}, fmt.Errorf("unsafe distribution state file")
	}
	file, err := os.Open(path)
	if err != nil {
		return State{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, (4<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("parse distribution state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return State{}, fmt.Errorf("distribution state has trailing content")
	}
	if err := validateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (store *Store) AddRemote(remote Remote) error {
	if err := validateRemote(remote); err != nil {
		return err
	}
	state, err := store.Load()
	if err != nil {
		return err
	}
	for _, existing := range state.Remotes {
		if existing.ID == remote.ID {
			return fmt.Errorf("knowledge remote %q already exists", remote.ID)
		}
	}
	state.Remotes = append(state.Remotes, remote)
	sort.Slice(state.Remotes, func(i, j int) bool { return state.Remotes[i].ID < state.Remotes[j].ID })
	return store.save(state)
}

func (store *Store) SetPolicy(policy UpdatePolicy) error {
	if !validPolicy(policy) {
		return fmt.Errorf("unsupported update policy %q", policy)
	}
	state, err := store.Load()
	if err != nil {
		return err
	}
	state.Policy = policy
	return store.save(state)
}

func (store *Store) Remote(id string) (Remote, bool, error) {
	state, err := store.Load()
	if err != nil {
		return Remote{}, false, err
	}
	for _, remote := range state.Remotes {
		if remote.ID == id {
			return remote, true, nil
		}
	}
	return Remote{}, false, nil
}

// ActiveInstalledRelease returns the signed release metadata backing the
// currently active managed Pack. The artifact is reverified against the
// customer-configured publisher trust root before metadata is returned.
func (store *Store) ActiveInstalledRelease(packID string) (InstalledRelease, bool, error) {
	state, err := store.Load()
	if err != nil {
		return InstalledRelease{}, false, err
	}
	active, ok := state.Active[packID]
	if !ok {
		return InstalledRelease{}, false, nil
	}
	release, ok := findInstalled(state, packID, active.Version)
	if !ok {
		return InstalledRelease{}, false, fmt.Errorf("active release %s@%s is not installed", packID, active.Version)
	}
	pkg, err := store.verifyInstalled(state, release, active.AuthorizedRollback)
	if err != nil {
		return InstalledRelease{}, false, err
	}
	if pkg.PackageSHA256 != active.PackageSHA256 {
		return InstalledRelease{}, false, fmt.Errorf("active release package hash does not match installed artifact")
	}
	return release, true, nil
}

func (store *Store) Pull(ctx context.Context, remoteID, packID string) (InstalledRelease, bool, error) {
	remote, ok, err := store.Remote(remoteID)
	if err != nil || !ok {
		if err == nil {
			err = fmt.Errorf("unknown knowledge remote %q", remoteID)
		}
		return InstalledRelease{}, false, err
	}
	descriptor, err := (Client{BaseURL: remote.BaseURL}).Pack(ctx, packID)
	if err != nil {
		return InstalledRelease{}, false, err
	}
	artifact, err := (Client{BaseURL: remote.BaseURL}).Download(ctx, descriptor.Latest)
	if err != nil {
		return InstalledRelease{}, false, err
	}
	publicKey, expectedKeyID, err := store.releaseVerificationKey(remote, descriptor.Latest.KeyID)
	if err != nil {
		return InstalledRelease{}, false, err
	}
	pkg, err := Verify(artifact, publicKey, expectedKeyID)
	if err != nil {
		return InstalledRelease{}, false, err
	}
	if err := matchDescriptor(pkg, descriptor.Latest, remote, expectedKeyID); err != nil {
		return InstalledRelease{}, false, err
	}
	return store.Install(artifact, pkg, remoteID)
}

func (store *Store) releaseVerificationKey(remote Remote, requestedKeyID string) (ed25519.PublicKey, string, error) {
	if requestedKeyID == remote.KeyID {
		key, err := ParsePublicKey([]byte(remote.PublicKeyPEM))
		return key, remote.KeyID, err
	}
	document, ok, err := trust.Open(store.stateDir).Load(trust.KnowledgePack)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", fmt.Errorf("release key %q is not the remote bootstrap key and no signed trust metadata is installed", requestedKeyID)
	}
	key, err := trust.PublicKey(document, requestedKeyID)
	return key, requestedKeyID, err
}

func (store *Store) Install(artifact []byte, pkg Package, remoteID string) (InstalledRelease, bool, error) {
	state, err := store.Load()
	if err != nil {
		return InstalledRelease{}, false, err
	}
	if policy, ok, err := trust.Open(store.stateDir).Load(trust.KnowledgePack); err != nil {
		return InstalledRelease{}, false, fmt.Errorf("load Knowledge Pack trust policy: %w", err)
	} else if ok {
		signedAt, err := time.Parse(time.RFC3339Nano, pkg.Manifest.CreatedAt)
		if err != nil {
			return InstalledRelease{}, false, err
		}
		if err := trust.Authorize(policy, pkg.Manifest.KeyID, pkg.Manifest.PackVersion, pkg.Manifest.PackID, signedAt, false); err != nil {
			return InstalledRelease{}, false, fmt.Errorf("Knowledge Pack trust policy rejected release: %w", err)
		}
	}
	targetDir := filepath.Join(store.dir, "installed", pkg.Manifest.PackID, pkg.Manifest.PackVersion)
	for _, installed := range state.Installed[pkg.Manifest.PackID] {
		if installed.Version == pkg.Manifest.PackVersion {
			if installed.PackageSHA256 != pkg.PackageSHA256 {
				return InstalledRelease{}, false, fmt.Errorf("immutable pack version collision for %s@%s", pkg.Manifest.PackID, pkg.Manifest.PackVersion)
			}
			return installed, false, nil
		}
	}
	if _, err := os.Lstat(targetDir); err == nil {
		return InstalledRelease{}, false, fmt.Errorf("installed release directory already exists")
	} else if !os.IsNotExist(err) {
		return InstalledRelease{}, false, err
	}
	parent := filepath.Dir(targetDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return InstalledRelease{}, false, err
	}
	temporary, err := os.MkdirTemp(parent, ".install-*")
	if err != nil {
		return InstalledRelease{}, false, err
	}
	defer os.RemoveAll(temporary)
	for name, data := range map[string][]byte{"release.snpkg": artifact, "release.json": pkg.ManifestBytes, "pack.yaml": pkg.PackBytes} {
		if err := os.WriteFile(filepath.Join(temporary, name), data, 0o600); err != nil {
			return InstalledRelease{}, false, err
		}
	}
	if err := os.Rename(temporary, targetDir); err != nil {
		return InstalledRelease{}, false, err
	}
	release := InstalledRelease{PackID: pkg.Manifest.PackID, Version: pkg.Manifest.PackVersion, Publisher: pkg.Manifest.Publisher, KeyID: pkg.Manifest.KeyID, CreatedAt: pkg.Manifest.CreatedAt, PreviousVersion: pkg.Manifest.PreviousVersion, PackageSHA256: pkg.PackageSHA256, InstalledAt: store.now().UTC().Format(time.RFC3339Nano), RemoteID: remoteID, ArtifactPath: filepath.ToSlash(filepath.Join("installed", pkg.Manifest.PackID, pkg.Manifest.PackVersion, "release.snpkg"))}
	state.Installed[pkg.Manifest.PackID] = append(state.Installed[pkg.Manifest.PackID], release)
	sort.Slice(state.Installed[pkg.Manifest.PackID], func(i, j int) bool {
		return state.Installed[pkg.Manifest.PackID][i].CreatedAt < state.Installed[pkg.Manifest.PackID][j].CreatedAt
	})
	if err := store.save(state); err != nil {
		_ = os.RemoveAll(targetDir)
		return InstalledRelease{}, false, err
	}
	return release, true, nil
}

func (store *Store) Activate(packID, version string) (ActiveRelease, error) {
	state, err := store.Load()
	if err != nil {
		return ActiveRelease{}, err
	}
	release, ok := findInstalled(state, packID, version)
	if !ok {
		return ActiveRelease{}, fmt.Errorf("pack release %s@%s is not installed", packID, version)
	}
	current, active := state.Active[packID]
	if active && current.Version == version {
		if _, err := store.verifyInstalled(state, release, current.AuthorizedRollback); err != nil {
			return ActiveRelease{}, err
		}
		return current, nil
	}
	if active && release.PreviousVersion != current.Version {
		return ActiveRelease{}, fmt.Errorf("release %s@%s does not continue active version %s; use rollback for downgrades", packID, version, current.Version)
	}
	if active {
		state.History[packID] = append(state.History[packID], current.Version)
	}
	return store.activateState(state, release, false)
}

func (store *Store) Rollback(packID string) (ActiveRelease, error) {
	state, err := store.Load()
	if err != nil {
		return ActiveRelease{}, err
	}
	history := state.History[packID]
	if len(history) == 0 {
		return ActiveRelease{}, fmt.Errorf("no previous active version for %q", packID)
	}
	version := history[len(history)-1]
	release, ok := findInstalled(state, packID, version)
	if !ok {
		return ActiveRelease{}, fmt.Errorf("rollback release %s@%s is missing", packID, version)
	}
	state.History[packID] = history[:len(history)-1]
	return store.activateState(state, release, true)
}

func (store *Store) activateState(state State, release InstalledRelease, explicitRollback bool) (ActiveRelease, error) {
	pkg, err := store.verifyInstalled(state, release, explicitRollback)
	if err != nil {
		return ActiveRelease{}, err
	}
	packBytes := pkg.PackBytes
	if _, err := knowledgeParseManaged(packBytes, release.PackID); err != nil {
		return ActiveRelease{}, err
	}
	activeDir := filepath.Join(store.stateDir, "knowledge-active")
	if err := os.MkdirAll(activeDir, 0o700); err != nil {
		return ActiveRelease{}, err
	}
	activePath := filepath.Join(activeDir, release.PackID+".yaml")
	previousBytes, previousErr := os.ReadFile(activePath)
	previousExisted := previousErr == nil
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return ActiveRelease{}, previousErr
	}
	temporary, err := os.CreateTemp(activeDir, ".active-*.tmp")
	if err != nil {
		return ActiveRelease{}, err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return ActiveRelease{}, err
	}
	if _, err := temporary.Write(packBytes); err != nil {
		temporary.Close()
		return ActiveRelease{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return ActiveRelease{}, err
	}
	if err := temporary.Close(); err != nil {
		return ActiveRelease{}, err
	}
	if err := os.Rename(name, activePath); err != nil {
		return ActiveRelease{}, err
	}
	active := ActiveRelease{PackID: release.PackID, Version: release.Version, ActivatedAt: store.now().UTC().Format(time.RFC3339Nano), PackageSHA256: release.PackageSHA256, AuthorizedRollback: explicitRollback}
	state.Active[release.PackID] = active
	if err := store.save(state); err != nil {
		if previousExisted {
			_ = restoreFile(activePath, previousBytes)
		} else {
			_ = os.Remove(activePath)
		}
		return ActiveRelease{}, err
	}
	return active, nil
}

func restoreFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".restore-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
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
	return os.Rename(name, path)
}

func (store *Store) verifyInstalled(state State, release InstalledRelease, explicitRollback bool) (Package, error) {
	var remote Remote
	found := false
	for _, candidate := range state.Remotes {
		if candidate.ID == release.RemoteID {
			remote, found = candidate, true
			break
		}
	}
	if !found {
		return Package{}, fmt.Errorf("trusted publisher configuration for installed release is missing")
	}
	key, expectedKeyID, err := store.releaseVerificationKey(remote, release.KeyID)
	if err != nil {
		return Package{}, err
	}
	artifactPath := filepath.Join(store.dir, filepath.FromSlash(release.ArtifactPath))
	if !filepath.IsLocal(filepath.FromSlash(release.ArtifactPath)) {
		return Package{}, fmt.Errorf("installed artifact path is unsafe")
	}
	artifact, err := ReadArtifact(artifactPath)
	if err != nil {
		return Package{}, err
	}
	pkg, err := Verify(artifact, key, expectedKeyID)
	if err != nil {
		return Package{}, fmt.Errorf("installed release verification failed: %w", err)
	}
	if pkg.PackageSHA256 != release.PackageSHA256 || pkg.Manifest.PackID != release.PackID || pkg.Manifest.PackVersion != release.Version || pkg.Manifest.Publisher != release.Publisher {
		return Package{}, fmt.Errorf("installed release state does not match signed artifact")
	}
	if policy, ok, err := trust.Open(store.stateDir).Load(trust.KnowledgePack); err != nil {
		return Package{}, fmt.Errorf("load Knowledge Pack trust policy: %w", err)
	} else if ok {
		signedAt, err := time.Parse(time.RFC3339Nano, pkg.Manifest.CreatedAt)
		if err != nil {
			return Package{}, fmt.Errorf("installed release timestamp is invalid: %w", err)
		}
		if err := trust.Authorize(policy, pkg.Manifest.KeyID, pkg.Manifest.PackVersion, pkg.Manifest.PackID, signedAt, explicitRollback); err != nil {
			return Package{}, fmt.Errorf("Knowledge Pack trust policy rejected installed release: %w", err)
		}
	}
	return pkg, nil
}

func (store *Store) save(state State) error {
	state.SchemaVersion = StateSchemaVersion
	if !validPolicy(state.Policy) {
		return fmt.Errorf("invalid distribution policy")
	}
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(store.dir, ".state-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
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
	return os.Rename(name, filepath.Join(store.dir, "state.json"))
}

func validateState(state State) error {
	if state.SchemaVersion != StateSchemaVersion || !validPolicy(state.Policy) || state.Installed == nil || state.Active == nil || state.History == nil {
		return fmt.Errorf("invalid distribution state schema")
	}
	seen := map[string]bool{}
	for _, remote := range state.Remotes {
		if err := validateRemote(remote); err != nil {
			return err
		}
		if seen[remote.ID] {
			return fmt.Errorf("duplicate knowledge remote %q", remote.ID)
		}
		seen[remote.ID] = true
	}
	for packID, releases := range state.Installed {
		if packID == "" || strings.ContainsAny(packID, "/\\") {
			return fmt.Errorf("invalid installed pack identity")
		}
		versions := map[string]bool{}
		for _, release := range releases {
			if release.PackID != packID || !versionPattern.MatchString(release.Version) || release.PackageSHA256 == "" || release.ArtifactPath == "" || !filepath.IsLocal(filepath.FromSlash(release.ArtifactPath)) {
				return fmt.Errorf("invalid installed release state for %q", packID)
			}
			if versions[release.Version] {
				return fmt.Errorf("duplicate installed release %s@%s", packID, release.Version)
			}
			versions[release.Version] = true
		}
	}
	for packID, active := range state.Active {
		if active.PackID != packID {
			return fmt.Errorf("active release identity mismatch")
		}
		if _, ok := findInstalled(state, packID, active.Version); !ok {
			return fmt.Errorf("active release %s@%s is not installed", packID, active.Version)
		}
	}
	return nil
}

func validateRemote(remote Remote) error {
	if remote.ID == "" || strings.ContainsAny(remote.ID, "/\\") || remote.Publisher == "" || !keyIDPattern.MatchString(remote.KeyID) {
		return fmt.Errorf("remote identity, publisher, and key id are required")
	}
	parsed, err := url.ParseRequestURI(remote.BaseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("knowledge remote must use a safe HTTPS base URL")
	}
	key, err := ParsePublicKey([]byte(remote.PublicKeyPEM))
	if err != nil {
		return err
	}
	if KeyID(key) != remote.KeyID {
		return fmt.Errorf("remote key id does not match public key")
	}
	return nil
}

func validPolicy(policy UpdatePolicy) bool {
	return policy == PolicyManual || policy == PolicyAutoDownload || policy == PolicyAutoUpdate
}

func findInstalled(state State, packID, version string) (InstalledRelease, bool) {
	for _, release := range state.Installed[packID] {
		if release.Version == version {
			return release, true
		}
	}
	return InstalledRelease{}, false
}

func matchDescriptor(pkg Package, descriptor VersionDescriptor, remote Remote, expectedKeyID string) error {
	manifest := pkg.Manifest
	if manifest.PackID != descriptor.PackID || manifest.PackVersion != descriptor.Version || manifest.Publisher != descriptor.Publisher || manifest.Publisher != remote.Publisher || manifest.CreatedAt != descriptor.CreatedAt || manifest.PreviousVersion != descriptor.PreviousVersion || manifest.KeyID != descriptor.KeyID || manifest.KeyID != expectedKeyID || pkg.PackageSHA256 != descriptor.ArtifactSHA256 {
		return fmt.Errorf("signed package does not match registry release descriptor")
	}
	return nil
}

// Kept local to avoid making activation depend on registry construction.
func knowledgeParseManaged(data []byte, expectedID string) (string, error) {
	pack, err := knowledge.Parse(data, knowledge.OriginManaged)
	if err != nil {
		return "", err
	}
	if pack.ID != expectedID {
		return "", fmt.Errorf("active pack identity mismatch")
	}
	return pack.ID, nil
}
