package trustcmd

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/sirToby99/swipenode/internal/softwaretrust"
	"github.com/sirToby99/swipenode/internal/trust"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	root := &cobra.Command{Use: "trust", Short: "Inspect and update signed public trust metadata", Args: cobra.NoArgs}
	root.AddCommand(keygen(), encodePublicKey(), sign(), verifyFile(), show(), bootstrap(), update(), authorize(), releaseKeys())
	return root
}

func writeExclusive(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func keygen() *cobra.Command {
	var privatePath, publicPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate an Ed25519 trust-metadata signing key pair",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			publicKey, privateKey, err := distribution.GenerateKeyPair()
			if err != nil {
				return err
			}
			privatePEM, err := distribution.MarshalPrivateKey(privateKey)
			if err != nil {
				return err
			}
			if err := writeExclusive(privatePath, privatePEM, 0o600); err != nil {
				return err
			}
			if err := writeExclusive(publicPath, []byte(trust.EncodePublicKey(publicKey)+"\n"), 0o644); err != nil {
				_ = os.Remove(privatePath)
				return err
			}
			result := struct {
				SchemaVersion string `json:"schema_version"`
				KeyID         string `json:"key_id"`
				Algorithm     string `json:"algorithm"`
				PrivateKey    string `json:"private_key_path"`
				PublicKey     string `json:"public_key_path"`
			}{"swipenode.trust-key.v1", trust.KeyID(publicKey), "Ed25519", privatePath, publicPath}
			if asJSON {
				return writeJSON(cmd, result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Key ID: %s\nAlgorithm: Ed25519\nPrivate key path: %s\nPublic key path: %s\n", result.KeyID, privatePath, publicPath)
			return err
		},
	}
	cmd.Flags().StringVar(&privatePath, "private-key", "", "exclusive output path for the PKCS#8 private key")
	cmd.Flags().StringVar(&publicPath, "public-key", "", "exclusive output path for the base64 Ed25519 public key")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit public identity and output paths as JSON")
	_ = cmd.MarkFlagRequired("private-key")
	_ = cmd.MarkFlagRequired("public-key")
	return cmd
}

func encodePublicKey() *cobra.Command {
	var inputPath, format, outputPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "encode-public-key",
		Short: "Convert a public content-signing key for trust metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := readSafeRegular(inputPath, 64<<10)
			if err != nil {
				return err
			}
			var publicKey ed25519.PublicKey
			switch format {
			case "openssh":
				publicKey, err = trust.ParseOpenSSHPublicKey(strings.TrimSpace(string(data)))
			case "pkix-pem":
				publicKey, err = distribution.ParsePublicKey(data)
			default:
				return fmt.Errorf("--format must be openssh or pkix-pem")
			}
			if err != nil {
				return err
			}
			if err := writeExclusive(outputPath, []byte(trust.EncodePublicKey(publicKey)+"\n"), 0o644); err != nil {
				return err
			}
			result := struct {
				SchemaVersion string `json:"schema_version"`
				KeyID         string `json:"key_id"`
				Algorithm     string `json:"algorithm"`
				Output        string `json:"output"`
			}{"swipenode.trust-public-key.v1", trust.KeyID(publicKey), "Ed25519", outputPath}
			if asJSON {
				return writeJSON(cmd, result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Key ID: %s\nAlgorithm: Ed25519\nTrust public key: %s\n", result.KeyID, result.Output)
			return err
		},
	}
	cmd.Flags().StringVar(&inputPath, "input", "", "OpenSSH or PKIX PEM public key file")
	cmd.Flags().StringVar(&format, "format", "", "input format: openssh or pkix-pem")
	cmd.Flags().StringVar(&outputPath, "output", "", "exclusive output path for base64 Ed25519 public key")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit public identity and output path as JSON")
	for _, name := range []string{"input", "format", "output"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

func sign() *cobra.Command {
	var templatePath, privatePath, outputPath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Sign public trust metadata with its declared metadata key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			templateData, err := readSafeRegular(templatePath, 1<<20)
			if err != nil {
				return err
			}
			document, err := trust.Decode(templateData)
			if err != nil {
				return fmt.Errorf("decode trust metadata template: %w", err)
			}
			privateData, err := distribution.ReadPrivateKeyFile(privatePath)
			if err != nil {
				return err
			}
			privateKey, err := distribution.ParsePrivateKey(privateData)
			if err != nil {
				return err
			}
			publicKey := privateKey.Public().(ed25519.PublicKey)
			signed, err := trust.Sign(document, trust.KeyID(publicKey), privateKey)
			if err != nil {
				return err
			}
			encoded, err := trust.Encode(signed)
			if err != nil {
				return err
			}
			if err := writeExclusive(outputPath, encoded, 0o644); err != nil {
				return err
			}
			result := struct {
				SchemaVersion string        `json:"schema_version"`
				Purpose       trust.Purpose `json:"purpose"`
				Sequence      uint64        `json:"sequence"`
				SignerKeyID   string        `json:"signer_key_id"`
				Output        string        `json:"output"`
			}{"swipenode.trust-signing-result.v1", signed.Purpose, signed.Sequence, signed.Signature.KeyID, outputPath}
			if asJSON {
				return writeJSON(cmd, result)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Signed %s sequence %d with %s as %s\n", result.Purpose, result.Sequence, result.SignerKeyID, result.Output)
			return err
		},
	}
	cmd.Flags().StringVar(&templatePath, "template", "", "unsigned public trust metadata JSON")
	cmd.Flags().StringVar(&privatePath, "signing-key", "", "mode-0600 PKCS#8 trust-metadata private key")
	cmd.Flags().StringVar(&outputPath, "output", "", "exclusive output path for signed public metadata")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit signing result as JSON")
	for _, name := range []string{"template", "signing-key", "output"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

func verifyFile() *cobra.Command {
	var metadataPath, bootstrapPath, previousPath, rawPurpose string
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify signed public trust metadata without installing it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := purpose(rawPurpose)
			if err != nil {
				return err
			}
			if (bootstrapPath == "") == (previousPath == "") {
				return fmt.Errorf("exactly one of --bootstrap-key or --previous is required")
			}
			document, err := readDocument(metadataPath)
			if err != nil {
				return err
			}
			if bootstrapPath != "" {
				data, err := readSafeRegular(bootstrapPath, 4096)
				if err != nil {
					return err
				}
				publicKey, err := trust.ParsePublicKey(strings.TrimSpace(string(data)))
				if err != nil {
					return err
				}
				if err := trust.VerifyInitial(document, publicKey, p, time.Now().UTC()); err != nil {
					return err
				}
			} else {
				previous, err := readDocument(previousPath)
				if err != nil {
					return err
				}
				if previous.Purpose != p {
					return fmt.Errorf("previous trust metadata purpose does not match --purpose")
				}
				if err := trust.VerifyUpdate(previous, document, time.Now().UTC()); err != nil {
					return err
				}
			}
			return writeJSON(cmd, map[string]any{"verified": true, "purpose": document.Purpose, "sequence": document.Sequence, "signer_key_id": document.Signature.KeyID})
		},
	}
	cmd.Flags().StringVar(&metadataPath, "metadata", "", "signed trust metadata JSON")
	cmd.Flags().StringVar(&bootstrapPath, "bootstrap-key", "", "independently obtained base64 root public key for initial metadata")
	cmd.Flags().StringVar(&previousPath, "previous", "", "previous verified metadata for a sequence update")
	cmd.Flags().StringVar(&rawPurpose, "purpose", "", "trust purpose: software_release or knowledge_pack")
	_ = cmd.MarkFlagRequired("metadata")
	_ = cmd.MarkFlagRequired("purpose")
	return cmd
}

func readSafeRegular(path string, maximum int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s must be a regular non-symlink file", path)
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("%s exceeds the size limit", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("%s exceeds the size limit", path)
	}
	return data, nil
}

func store(ctx context.Context) (*trust.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := gitcontext.ResolveRoot(cwd)
	if err != nil {
		return nil, err
	}
	state, err := gitcontext.StateDir(ctx, root)
	if err != nil {
		return nil, err
	}
	return trust.Open(state), nil
}
func purpose(value string) (trust.Purpose, error) {
	p := trust.Purpose(strings.TrimSpace(value))
	if p != trust.SoftwareRelease && p != trust.KnowledgePack {
		return "", fmt.Errorf("--purpose must be software_release or knowledge_pack")
	}
	return p, nil
}
func writeJSON(cmd *cobra.Command, value any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func readDocument(path string) (trust.Document, error) {
	data, err := readSafeRegular(path, 1<<20)
	if err != nil {
		return trust.Document{}, err
	}
	return trust.Decode(data)
}

func show() *cobra.Command {
	var raw string
	cmd := &cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		p, err := purpose(raw)
		if err != nil {
			return err
		}
		s, err := store(cmd.Context())
		if err != nil {
			return err
		}
		doc, ok, err := s.Load(p)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("trust metadata for %s is not installed", p)
		}
		return writeJSON(cmd, doc)
	}}
	cmd.Flags().StringVar(&raw, "purpose", "", "trust purpose")
	_ = cmd.MarkFlagRequired("purpose")
	return cmd
}
func bootstrap() *cobra.Command {
	var metadata, key string
	cmd := &cobra.Command{Use: "bootstrap", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		doc, err := readDocument(metadata)
		if err != nil {
			return err
		}
		keyBytes, err := readSafeRegular(key, 4096)
		if err != nil {
			return err
		}
		s, err := store(cmd.Context())
		if err != nil {
			return err
		}
		return s.InstallInitial(doc, []byte(strings.TrimSpace(string(keyBytes))))
	}}
	cmd.Flags().StringVar(&metadata, "metadata", "", "signed trust metadata JSON")
	cmd.Flags().StringVar(&key, "bootstrap-key", "", "independently obtained base64 Ed25519 public key file")
	_ = cmd.MarkFlagRequired("metadata")
	_ = cmd.MarkFlagRequired("bootstrap-key")
	return cmd
}
func update() *cobra.Command {
	var metadata string
	cmd := &cobra.Command{Use: "update", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		doc, err := readDocument(metadata)
		if err != nil {
			return err
		}
		s, err := store(cmd.Context())
		if err != nil {
			return err
		}
		return s.Update(doc)
	}}
	cmd.Flags().StringVar(&metadata, "metadata", "", "successor-signed trust metadata JSON")
	_ = cmd.MarkFlagRequired("metadata")
	return cmd
}
func authorize() *cobra.Command {
	var raw, keyID, version, packID, signedAt string
	var rollback bool
	cmd := &cobra.Command{Use: "authorize", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		p, err := purpose(raw)
		if err != nil {
			return err
		}
		stamp, err := time.Parse(time.RFC3339Nano, signedAt)
		if err != nil {
			return err
		}
		s, err := store(cmd.Context())
		if err != nil {
			return err
		}
		doc, ok, err := s.Load(p)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("trust metadata for %s is not installed", p)
		}
		if err := trust.Authorize(doc, keyID, version, packID, stamp, rollback); err != nil {
			return err
		}
		return writeJSON(cmd, map[string]any{"authorized": true, "purpose": p, "key_id": keyID, "version": version, "explicit_rollback": rollback})
	}}
	cmd.Flags().StringVar(&raw, "purpose", "", "trust purpose")
	cmd.Flags().StringVar(&keyID, "key-id", "", "content signing key ID")
	cmd.Flags().StringVar(&version, "version", "", "software or Pack version")
	cmd.Flags().StringVar(&packID, "pack-id", "", "Pack ID for knowledge_pack purpose")
	cmd.Flags().StringVar(&signedAt, "signed-at", "", "content signature RFC3339 timestamp")
	cmd.Flags().BoolVar(&rollback, "explicit-rollback", false, "authorize an already-installed Pack rollback without bypassing key validity")
	for _, name := range []string{"purpose", "key-id", "version", "signed-at"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

func releaseKeys() *cobra.Command {
	var metadataPath, rootPath, version, outputPath, authorizationTime string
	var allowDowngrade bool
	cmd := &cobra.Command{
		Use:   "release-keys",
		Short: "Authenticate software trust metadata and emit authorized SSHSIG public keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			metadata, err := readSafeRegular(metadataPath, 1<<20)
			if err != nil {
				return err
			}
			root, err := readSafeRegular(rootPath, 4096)
			if err != nil {
				return err
			}
			at := time.Now().UTC()
			if authorizationTime != "" {
				at, err = time.Parse(time.RFC3339Nano, authorizationTime)
				if err != nil {
					return fmt.Errorf("invalid --authorization-time: %w", err)
				}
			}
			result, err := softwaretrust.AuthorizedSigners(metadata, strings.TrimSpace(string(root)), version, at, allowDowngrade)
			if err != nil {
				return err
			}
			if info, err := os.Lstat(outputPath); err == nil {
				if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("--output must not be a symlink or special file")
				}
			} else if !os.IsNotExist(err) {
				return err
			}
			directory := filepath.Dir(outputPath)
			temporary, err := os.CreateTemp(directory, ".swipenode-release-keys-*")
			if err != nil {
				return err
			}
			temporaryPath := temporary.Name()
			defer os.Remove(temporaryPath)
			if err := temporary.Chmod(0o600); err != nil {
				temporary.Close()
				return err
			}
			_, writeErr := temporary.WriteString(strings.Join(result.AllowedSigners, "\n") + "\n")
			closeErr := temporary.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Rename(temporaryPath, outputPath); err != nil {
				return err
			}
			return writeJSON(cmd, result)
		},
	}
	cmd.Flags().StringVar(&metadataPath, "metadata", "", "root-signed software_release trust metadata JSON")
	cmd.Flags().StringVar(&rootPath, "trust-root", "", "independently obtained base64 Ed25519 trust-root file")
	cmd.Flags().StringVar(&version, "version", "", "software release version to authorize")
	cmd.Flags().StringVar(&outputPath, "output", "", "authorized OpenSSH allowed-signers output file")
	cmd.Flags().StringVar(&authorizationTime, "authorization-time", "", "verification time override in RFC3339 (test or explicit recovery use only)")
	cmd.Flags().BoolVar(&allowDowngrade, "allow-downgrade", false, "explicitly allow a release below the signed minimum; key validity and revocation still apply")
	for _, name := range []string{"metadata", "trust-root", "version", "output"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}
