package knowledgecmd

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/localstate"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const distributionOutputSchema = "swipenode.knowledge-distribution.v1"

func addDistributionCommands(parent *cobra.Command, deps Dependencies) {
	parent.AddCommand(
		newKeygenCommand(),
		newPackageCommand(deps),
		newInspectPackageCommand(),
		newVerifyPackageCommand(),
		newRemoteCommand(deps),
		newUpdateCheckCommand(deps),
		newPullCommand(deps),
		newInstalledCommand(deps),
		newActivateCommand(deps),
		newRollbackCommand(deps),
		newPolicyCommand(deps),
	)
}

func distributionStore(ctx context.Context, deps Dependencies) (string, *distribution.Store, error) {
	cwd, err := deps.Getwd()
	if err != nil {
		return "", nil, fmt.Errorf("get working directory: %w", err)
	}
	root, err := deps.ResolveRoot(cwd)
	if err != nil {
		return "", nil, err
	}
	stateDir, err := deps.StateDir(ctx, root)
	if err != nil {
		return "", nil, err
	}
	return stateDir, distribution.OpenStore(stateDir), nil
}

func withDistributionLock(stateDir string, fn func() error) (returnErr error) {
	lock, err := localstate.Acquire(stateDir, "knowledge-distribution")
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Release(); returnErr == nil && err != nil {
			returnErr = err
		}
	}()
	return fn()
}

func newKeygenCommand() *cobra.Command {
	var privatePath, publicPath string
	var asJSON bool
	cmd := &cobra.Command{Use: "keygen", Short: "Generate an Ed25519 Knowledge Pack publisher key pair", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if privatePath == "" || publicPath == "" {
			return fmt.Errorf("--private-key and --public-key are required")
		}
		publicKey, privateKey, err := distribution.GenerateKeyPair()
		if err != nil {
			return err
		}
		privatePEM, err := distribution.MarshalPrivateKey(privateKey)
		if err != nil {
			return err
		}
		publicPEM, err := distribution.MarshalPublicKey(publicKey)
		if err != nil {
			return err
		}
		if err := writeExclusive(privatePath, privatePEM, 0o600); err != nil {
			return err
		}
		if err := writeExclusive(publicPath, publicPEM, 0o644); err != nil {
			_ = os.Remove(privatePath)
			return err
		}
		result := struct {
			SchemaVersion string `json:"schema_version"`
			KeyID         string `json:"key_id"`
			PrivateKey    string `json:"private_key"`
			PublicKey     string `json:"public_key"`
		}{distributionOutputSchema, distribution.KeyID(publicKey), privatePath, publicPath}
		if asJSON {
			return writeJSON(cmd, result)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Key ID: %s\nPrivate key: %s\nPublic key: %s\n", result.KeyID, privatePath, publicPath)
		return err
	}}
	cmd.Flags().StringVar(&privatePath, "private-key", "", "path for the new private signing key")
	cmd.Flags().StringVar(&publicPath, "public-key", "", "path for the new trusted public key")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newPackageCommand(deps Dependencies) *cobra.Command {
	var version, publisher, createdAt, sourcePolicy, previous, notes, privatePath, output string
	var asJSON bool
	cmd := &cobra.Command{Use: "package <pack-id>", Short: "Build a deterministic signed Knowledge Pack release", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if version == "" || publisher == "" || createdAt == "" || privatePath == "" || output == "" {
			return fmt.Errorf("--version, --publisher, --created-at, --signing-key, and --output are required")
		}
		created, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return fmt.Errorf("parse --created-at: %w", err)
		}
		_, registry, err := rootAndRegistry(cmd.Context(), deps)
		if err != nil {
			return err
		}
		pack, ok := registry.Find(args[0])
		if !ok {
			return fmt.Errorf("unknown knowledge pack %q", args[0])
		}
		packBytes, err := yaml.Marshal(pack)
		if err != nil {
			return err
		}
		keyBytes, err := distribution.ReadPrivateKeyFile(privatePath)
		if err != nil {
			return err
		}
		privateKey, err := distribution.ParsePrivateKey(keyBytes)
		if err != nil {
			return err
		}
		keyID := distribution.KeyID(privateKey.Public().(ed25519.PublicKey))
		if sourcePolicy == "" {
			sourcePolicy = version
		}
		artifact, pkg, err := distribution.Build(pack, packBytes, distribution.BuildOptions{PackVersion: version, Publisher: publisher, CreatedAt: created, SourcePolicyVersion: sourcePolicy, PreviousVersion: previous, ReleaseNotes: notes, KeyID: keyID, PrivateKey: privateKey})
		if err != nil {
			return err
		}
		if err := distribution.WriteArtifact(output, artifact, 0o644); err != nil {
			return err
		}
		result := packageResult(pkg, output)
		if asJSON {
			return writeJSON(cmd, result)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Packaged: %s@%s\nPublisher: %s\nKey ID: %s\nSHA-256: %s\nArtifact: %s\n", pkg.Manifest.PackID, pkg.Manifest.PackVersion, pkg.Manifest.Publisher, pkg.Manifest.KeyID, pkg.PackageSHA256, output)
		return err
	}}
	cmd.Flags().StringVar(&version, "version", "", "immutable release version")
	cmd.Flags().StringVar(&publisher, "publisher", "", "publisher identity")
	cmd.Flags().StringVar(&createdAt, "created-at", "", "deterministic RFC3339 release timestamp")
	cmd.Flags().StringVar(&sourcePolicy, "source-policy-version", "", "source-policy revision (defaults to release version)")
	cmd.Flags().StringVar(&previous, "previous-version", "", "signed predecessor release")
	cmd.Flags().StringVar(&notes, "release-notes", "", "release notes")
	cmd.Flags().StringVar(&privatePath, "signing-key", "", "Ed25519 private key PEM")
	cmd.Flags().StringVar(&output, "output", "", "output .snpkg path")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

type packageOutput struct {
	SchemaVersion string                       `json:"schema_version"`
	Manifest      distribution.ReleaseManifest `json:"manifest"`
	PackageSHA256 string                       `json:"package_sha256"`
	Artifact      string                       `json:"artifact,omitempty"`
}

func packageResult(pkg distribution.Package, artifact string) packageOutput {
	return packageOutput{distributionOutputSchema, pkg.Manifest, pkg.PackageSHA256, artifact}
}

func newInspectPackageCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "inspect <artifact>", Short: "Inspect a Knowledge Pack release without trusting it", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		artifact, err := distribution.ReadArtifact(args[0])
		if err != nil {
			return err
		}
		pkg, err := distribution.Inspect(artifact)
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, packageResult(pkg, args[0]))
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s@%s\t%s\t%s\n", pkg.Manifest.PackID, pkg.Manifest.PackVersion, pkg.Manifest.Publisher, pkg.PackageSHA256)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newVerifyPackageCommand() *cobra.Command {
	var trustedKey string
	var asJSON bool
	cmd := &cobra.Command{Use: "verify-package <artifact>", Short: "Verify release integrity and publisher authenticity", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if trustedKey == "" {
			return fmt.Errorf("--trusted-key is required")
		}
		artifact, err := distribution.ReadArtifact(args[0])
		if err != nil {
			return err
		}
		keyBytes, err := readRegular(trustedKey, 64<<10)
		if err != nil {
			return err
		}
		key, err := distribution.ParsePublicKey(keyBytes)
		if err != nil {
			return err
		}
		pkg, err := distribution.Verify(artifact, key, distribution.KeyID(key))
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, packageResult(pkg, args[0]))
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Valid signed release: %s@%s (%s)\n", pkg.Manifest.PackID, pkg.Manifest.PackVersion, pkg.PackageSHA256)
		return err
	}}
	cmd.Flags().StringVar(&trustedKey, "trusted-key", "", "trusted publisher public key PEM")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newRemoteCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "remote", Short: "Manage Knowledge Pack distribution remotes", Args: cobra.NoArgs}
	var asJSON bool
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		state, err := store.Load()
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, struct {
				SchemaVersion string                `json:"schema_version"`
				Remotes       []distribution.Remote `json:"remotes"`
			}{distributionOutputSchema, state.Remotes})
		}
		for _, remote := range state.Remotes {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", remote.ID, remote.Publisher, remote.BaseURL, remote.KeyID); err != nil {
				return err
			}
		}
		return nil
	}}
	list.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	var remoteID, baseURL, publisher, keyPath string
	add := &cobra.Command{Use: "add", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if remoteID == "" || baseURL == "" || publisher == "" || keyPath == "" {
			return fmt.Errorf("--id, --url, --publisher, and --trusted-key are required")
		}
		keyBytes, err := readRegular(keyPath, 64<<10)
		if err != nil {
			return err
		}
		key, err := distribution.ParsePublicKey(keyBytes)
		if err != nil {
			return err
		}
		stateDir, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		remote := distribution.Remote{ID: remoteID, BaseURL: baseURL, Publisher: publisher, KeyID: distribution.KeyID(key), PublicKeyPEM: string(keyBytes)}
		return withDistributionLock(stateDir, func() error { return store.AddRemote(remote) })
	}}
	add.Flags().StringVar(&remoteID, "id", "", "local remote ID")
	add.Flags().StringVar(&baseURL, "url", "", "HTTPS registry base URL")
	add.Flags().StringVar(&publisher, "publisher", "", "expected publisher identity")
	add.Flags().StringVar(&keyPath, "trusted-key", "", "trusted publisher public key PEM")
	command.AddCommand(list, add)
	return command
}

func resolveRemote(store *distribution.Store, requested string) (distribution.Remote, error) {
	state, err := store.Load()
	if err != nil {
		return distribution.Remote{}, err
	}
	if requested != "" {
		remote, ok, err := store.Remote(requested)
		if err != nil {
			return distribution.Remote{}, err
		}
		if !ok {
			return distribution.Remote{}, fmt.Errorf("unknown knowledge remote %q", requested)
		}
		return remote, nil
	}
	if len(state.Remotes) != 1 {
		return distribution.Remote{}, fmt.Errorf("--remote is required unless exactly one remote is configured")
	}
	return state.Remotes[0], nil
}

func newUpdateCheckCommand(deps Dependencies) *cobra.Command {
	var remoteID string
	var asJSON bool
	cmd := &cobra.Command{Use: "update-check [pack-id]", Short: "Discover signed Pack releases without installing them", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		remote, err := resolveRemote(store, remoteID)
		if err != nil {
			return err
		}
		client := distribution.Client{BaseURL: remote.BaseURL}
		var value any
		if len(args) == 1 {
			value, err = client.Pack(cmd.Context(), args[0])
		} else {
			value, err = client.Registry(cmd.Context())
		}
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, value)
		}
		data, _ := json.MarshalIndent(value, "", "  ")
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}}
	cmd.Flags().StringVar(&remoteID, "remote", "", "configured remote ID")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newPullCommand(deps Dependencies) *cobra.Command {
	var remoteID string
	var asJSON bool
	cmd := &cobra.Command{Use: "pull <pack-id>", Short: "Download, verify, and stage a Pack without activating it", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		stateDir, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		remote, err := resolveRemote(store, remoteID)
		if err != nil {
			return err
		}
		var release distribution.InstalledRelease
		var installed bool
		err = withDistributionLock(stateDir, func() error {
			var pullErr error
			release, installed, pullErr = store.Pull(cmd.Context(), remote.ID, args[0])
			return pullErr
		})
		if err != nil {
			return err
		}
		result := struct {
			SchemaVersion string                        `json:"schema_version"`
			Installed     bool                          `json:"installed"`
			Release       distribution.InstalledRelease `json:"release"`
		}{distributionOutputSchema, installed, release}
		if asJSON {
			return writeJSON(cmd, result)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Staged: %s@%s (activated: false)\n", release.PackID, release.Version)
		return err
	}}
	cmd.Flags().StringVar(&remoteID, "remote", "", "configured remote ID")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newInstalledCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "installed [pack-id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		state, err := store.Load()
		if err != nil {
			return err
		}
		installed := state.Installed
		if len(args) == 1 {
			installed = map[string][]distribution.InstalledRelease{args[0]: state.Installed[args[0]]}
		}
		result := struct {
			SchemaVersion string                                     `json:"schema_version"`
			Installed     map[string][]distribution.InstalledRelease `json:"installed"`
			Active        map[string]distribution.ActiveRelease      `json:"active"`
		}{distributionOutputSchema, installed, state.Active}
		if asJSON {
			return writeJSON(cmd, result)
		}
		for id, releases := range installed {
			for _, release := range releases {
				marker := ""
				if state.Active[id].Version == release.Version {
					marker = " active"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s@%s%s\n", id, release.Version, marker); err != nil {
					return err
				}
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newActivateCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "activate <pack-id>@<version>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		packID, version, ok := strings.Cut(args[0], "@")
		if !ok || packID == "" || version == "" {
			return fmt.Errorf("release must be <pack-id>@<version>")
		}
		stateDir, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		var active distribution.ActiveRelease
		err = withDistributionLock(stateDir, func() error {
			var activateErr error
			active, activateErr = store.Activate(packID, version)
			return activateErr
		})
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, active)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Active: %s@%s\n", active.PackID, active.Version)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newRollbackCommand(deps Dependencies) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "rollback <pack-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		stateDir, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		var active distribution.ActiveRelease
		err = withDistributionLock(stateDir, func() error { var rollbackErr error; active, rollbackErr = store.Rollback(args[0]); return rollbackErr })
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, active)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Rolled back: %s@%s\n", active.PackID, active.Version)
		return err
	}}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newPolicyCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "policy", Args: cobra.NoArgs}
	show := &cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		state, err := store.Load()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), state.Policy)
		return err
	}}
	set := &cobra.Command{Use: "set <manual|auto-download|auto-update>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		stateDir, store, err := distributionStore(cmd.Context(), deps)
		if err != nil {
			return err
		}
		return withDistributionLock(stateDir, func() error { return store.SetPolicy(distribution.UpdatePolicy(args[0])) })
	}}
	command.AddCommand(show, set)
	return command
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("unsafe file %q", path)
	}
	return os.ReadFile(path)
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}
