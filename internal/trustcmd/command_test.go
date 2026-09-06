package trustcmd

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/distribution"
	"github.com/sirToby99/swipenode/internal/trust"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := New()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func TestEncodePublicContentKeys(t *testing.T) {
	directory := t.TempDir()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	openSSH, err := trust.EncodeOpenSSHPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	pemData, err := distribution.MarshalPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	for name, fixture := range map[string]struct {
		format string
		data   []byte
	}{
		"openssh": {"openssh", []byte(openSSH + " swipenode-release\n")},
		"pem":     {"pkix-pem", pemData},
	} {
		t.Run(name, func(t *testing.T) {
			input := filepath.Join(directory, name+".pub")
			output := filepath.Join(directory, name+".trust.pub")
			if err := os.WriteFile(input, fixture.data, 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := execute(t, "encode-public-key", "--input", input, "--format", fixture.format, "--output", output, "--json")
			if err != nil || !strings.Contains(result, trust.KeyID(public)) {
				t.Fatalf("encode result: %q %v", result, err)
			}
			data, err := os.ReadFile(output)
			if err != nil || strings.TrimSpace(string(data)) != trust.EncodePublicKey(public) {
				t.Fatalf("encoded public key: %q %v", data, err)
			}
		})
	}
}

func TestOfflineKeygenAndMetadataSigning(t *testing.T) {
	directory := t.TempDir()
	privatePath := filepath.Join(directory, "root.key")
	publicPath := filepath.Join(directory, "root.pub")
	output, err := execute(t, "keygen", "--private-key", privatePath, "--public-key", publicPath, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		KeyID string `json:"key_id"`
	}
	if err := json.Unmarshal([]byte(output), &generated); err != nil || generated.KeyID == "" {
		t.Fatalf("keygen result: %q %v", output, err)
	}
	info, err := os.Stat(privatePath)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("private key permissions: %v %v", info, err)
	}
	publicData, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := trust.ParsePublicKey(strings.TrimSpace(string(publicData)))
	if err != nil || trust.KeyID(publicKey) != generated.KeyID {
		t.Fatalf("public identity mismatch: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	template := trust.Document{
		SchemaVersion: trust.SchemaVersion,
		Purpose:       trust.SoftwareRelease,
		Sequence:      1,
		IssuedAt:      now.Format(time.RFC3339Nano),
		Keys: []trust.Key{{
			ID: generated.KeyID, Algorithm: "Ed25519", PublicKey: strings.TrimSpace(string(publicData)),
			CreatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), ActiveFrom: now.Format(time.RFC3339Nano),
			Purpose: trust.SoftwareRelease, Usages: []trust.KeyUsage{trust.TrustMetadata},
		}},
		MinimumVersions: trust.MinimumVersions{Software: "1.0.0"},
	}
	templateData, err := trust.Encode(template)
	if err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join(directory, "template.json")
	if err := os.WriteFile(templatePath, templateData, 0o644); err != nil {
		t.Fatal(err)
	}
	signedPath := filepath.Join(directory, "signed.json")
	if _, err := execute(t, "sign", "--template", templatePath, "--signing-key", privatePath, "--output", signedPath, "--json"); err != nil {
		t.Fatal(err)
	}
	signedData, err := os.ReadFile(signedPath)
	if err != nil || bytes.Contains(signedData, []byte("PRIVATE KEY")) {
		t.Fatalf("unsafe signed output: %v", err)
	}
	signed, err := trust.Decode(signedData)
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.VerifyInitial(signed, publicKey, trust.SoftwareRelease, now); err != nil {
		t.Fatal(err)
	}
	verifyOutput, err := execute(t, "verify", "--metadata", signedPath, "--bootstrap-key", publicPath, "--purpose", "software_release")
	if err != nil || !strings.Contains(verifyOutput, `"verified": true`) {
		t.Fatalf("verify result: %q %v", verifyOutput, err)
	}
	if _, err := execute(t, "sign", "--template", templatePath, "--signing-key", privatePath, "--output", signedPath); err == nil {
		t.Fatal("metadata output was overwritten")
	}
}
