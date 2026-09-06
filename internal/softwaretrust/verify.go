// Package softwaretrust bridges signed SwipeNode trust metadata to OpenSSH
// SSHSIG release verification without handling private key material.
package softwaretrust

import (
	"fmt"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/trust"
)

type Result struct {
	Purpose               trust.Purpose `json:"purpose"`
	MetadataSequence      uint64        `json:"metadata_sequence"`
	Version               string        `json:"version"`
	MinimumVersion        string        `json:"minimum_version,omitempty"`
	AuthorizedKeyIDs      []string      `json:"authorized_key_ids"`
	AllowedSigners        []string      `json:"-"`
	DowngradeOverrideUsed bool          `json:"downgrade_override_used"`
}

// AuthorizedSigners authenticates software-release metadata with an
// independently configured root and emits only currently authorized public
// content-signing keys in OpenSSH allowed-signers format.
func AuthorizedSigners(metadata []byte, bootstrapKey, version string, at time.Time, allowDowngrade bool) (Result, error) {
	document, err := trust.Decode(metadata)
	if err != nil {
		return Result{}, fmt.Errorf("decode software trust metadata: %w", err)
	}
	bootstrap, err := trust.ParsePublicKey(strings.TrimSpace(bootstrapKey))
	if err != nil {
		return Result{}, fmt.Errorf("parse independent trust root: %w", err)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if err := trust.VerifyInitial(document, bootstrap, trust.SoftwareRelease, at); err != nil {
		return Result{}, fmt.Errorf("authenticate software trust metadata: %w", err)
	}
	if _, err := trust.CompareVersions(version, version); err != nil {
		return Result{}, err
	}

	policy := document
	if allowDowngrade {
		policy.MinimumVersions.Software = ""
	}
	result := Result{
		Purpose:               trust.SoftwareRelease,
		MetadataSequence:      document.Sequence,
		Version:               version,
		MinimumVersion:        document.MinimumVersions.Software,
		DowngradeOverrideUsed: allowDowngrade && document.MinimumVersions.Software != "",
	}
	for _, key := range document.Keys {
		if key.Purpose != trust.SoftwareRelease || !trust.HasUsage(key, trust.ContentSigning) {
			continue
		}
		if err := trust.Authorize(policy, key.ID, version, "", at, false); err != nil {
			continue
		}
		public, err := trust.ParsePublicKey(key.PublicKey)
		if err != nil {
			return Result{}, err
		}
		openSSH, err := trust.EncodeOpenSSHPublicKey(public)
		if err != nil {
			return Result{}, err
		}
		result.AuthorizedKeyIDs = append(result.AuthorizedKeyIDs, key.ID)
		result.AllowedSigners = append(result.AllowedSigners, "swipenode-release "+openSSH)
	}
	if len(result.AllowedSigners) == 0 {
		return Result{}, fmt.Errorf("no active software-release signing key authorizes version %s", version)
	}
	return result, nil
}
