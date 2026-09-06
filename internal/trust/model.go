// Package trust implements small, signed trust metadata for SwipeNode software
// releases and Managed Knowledge Pack publisher keys. It stores public data
// only and deliberately does not implement a general-purpose PKI.
package trust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = "swipenode.trust-metadata.v1"

type Purpose string

const (
	SoftwareRelease Purpose = "software_release"
	KnowledgePack   Purpose = "knowledge_pack"
)

// KeyUsage separates authority to sign trust metadata from authority to sign
// release content. Empty usage lists retain the v1 legacy meaning (both) so
// already-installed metadata remains readable; newly issued metadata should
// always declare usage explicitly.
type KeyUsage string

const (
	TrustMetadata  KeyUsage = "trust_metadata"
	ContentSigning KeyUsage = "content_signing"
)

type Key struct {
	ID               string     `json:"id"`
	Algorithm        string     `json:"algorithm"`
	PublicKey        string     `json:"public_key"`
	CreatedAt        string     `json:"created_at"`
	ActiveFrom       string     `json:"active_from"`
	RetiredAt        string     `json:"retired_at,omitempty"`
	RevokedAt        string     `json:"revoked_at,omitempty"`
	RevocationReason string     `json:"revocation_reason,omitempty"`
	SuccessorKeyID   string     `json:"successor_key_id,omitempty"`
	Purpose          Purpose    `json:"purpose"`
	Usages           []KeyUsage `json:"usages,omitempty"`
}

type MinimumVersions struct {
	Software string            `json:"software,omitempty"`
	Packs    map[string]string `json:"packs,omitempty"`
}

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Value     string `json:"value"`
}

type Document struct {
	SchemaVersion   string          `json:"schema_version"`
	Purpose         Purpose         `json:"purpose"`
	Sequence        uint64          `json:"sequence"`
	IssuedAt        string          `json:"issued_at"`
	Keys            []Key           `json:"keys"`
	MinimumVersions MinimumVersions `json:"minimum_versions"`
	Signature       Signature       `json:"signature"`
}

type unsignedDocument struct {
	SchemaVersion   string          `json:"schema_version"`
	Purpose         Purpose         `json:"purpose"`
	Sequence        uint64          `json:"sequence"`
	IssuedAt        string          `json:"issued_at"`
	Keys            []Key           `json:"keys"`
	MinimumVersions MinimumVersions `json:"minimum_versions"`
}

var stableID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func Sign(document Document, signerID string, privateKey ed25519.PrivateKey) (Document, error) {
	document.SchemaVersion = SchemaVersion
	document.Signature = Signature{}
	if err := validate(document); err != nil {
		return Document{}, err
	}
	if len(privateKey) != ed25519.PrivateKeySize || KeyID(privateKey.Public().(ed25519.PublicKey)) != signerID {
		return Document{}, fmt.Errorf("signing key does not match signer id")
	}
	signer, ok := findKey(document, signerID)
	if !ok || signer.Purpose != document.Purpose || !HasUsage(signer, TrustMetadata) {
		return Document{}, fmt.Errorf("signer is not declared for trust metadata usage")
	}
	payload, err := signingBytes(document)
	if err != nil {
		return Document{}, err
	}
	document.Signature = Signature{Algorithm: "Ed25519", KeyID: signerID, Value: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))}
	return document, nil
}

func VerifyInitial(document Document, bootstrap ed25519.PublicKey, purpose Purpose, now time.Time) error {
	if document.Purpose != purpose || document.Sequence == 0 {
		return fmt.Errorf("trust metadata purpose or initial sequence is invalid")
	}
	if err := verifySignature(document, bootstrap, KeyID(bootstrap)); err != nil {
		return err
	}
	key, ok := findKey(document, KeyID(bootstrap))
	if !ok || key.Purpose != purpose || !HasUsage(key, TrustMetadata) {
		return fmt.Errorf("bootstrap signer is not declared for trust purpose")
	}
	return validateAt(document, now)
}

func VerifyUpdate(previous, next Document, now time.Time) error {
	if previous.Purpose != next.Purpose || next.Sequence != previous.Sequence+1 {
		return fmt.Errorf("trust metadata purpose changed or sequence did not advance exactly once")
	}
	signer, ok := findKey(previous, next.Signature.KeyID)
	if !ok || signer.Purpose != previous.Purpose || !HasUsage(signer, TrustMetadata) {
		return fmt.Errorf("replacement metadata signer is not previously trusted")
	}
	publicKey, err := ParsePublicKey(signer.PublicKey)
	if err != nil {
		return err
	}
	issued, _ := time.Parse(time.RFC3339Nano, next.IssuedAt)
	if !keyPermits(signer, issued, false) {
		return fmt.Errorf("replacement metadata signer is not active at issuance")
	}
	if err := verifySignature(next, publicKey, signer.ID); err != nil {
		return err
	}
	return validateAt(next, now)
}

// Authorize validates a content-signing key and anti-downgrade policy. Explicit
// rollback applies only to an already installed Pack and never bypasses key
// revocation or signature validation.
func Authorize(document Document, keyID, version, packID string, signedAt time.Time, explicitRollback bool) error {
	key, ok := findKey(document, keyID)
	if !ok || key.Purpose != document.Purpose || !HasUsage(key, ContentSigning) {
		return fmt.Errorf("content signing key is not trusted for %s", document.Purpose)
	}
	if !keyPermits(key, signedAt, true) {
		return fmt.Errorf("content signing key is revoked, retired, or outside its validity period")
	}
	minimum := document.MinimumVersions.Software
	if document.Purpose == KnowledgePack {
		minimum = document.MinimumVersions.Packs[packID]
		if explicitRollback {
			return nil
		}
	}
	if minimum != "" {
		comparison, err := CompareVersions(version, minimum)
		if err != nil {
			return err
		}
		if comparison < 0 {
			return fmt.Errorf("version %s is below signed minimum %s", version, minimum)
		}
	}
	return nil
}

func PublicKey(document Document, keyID string) (ed25519.PublicKey, error) {
	key, ok := findKey(document, keyID)
	if !ok || key.Purpose != document.Purpose {
		return nil, fmt.Errorf("key is not trusted for %s", document.Purpose)
	}
	return ParsePublicKey(key.PublicKey)
}

func validateAt(document Document, now time.Time) error {
	if err := validate(document); err != nil {
		return err
	}
	issued, _ := time.Parse(time.RFC3339Nano, document.IssuedAt)
	if issued.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("trust metadata issuance is in the future")
	}
	return nil
}

func validate(document Document) error {
	if document.SchemaVersion != SchemaVersion || (document.Purpose != SoftwareRelease && document.Purpose != KnowledgePack) || document.Sequence == 0 || len(document.Keys) == 0 {
		return fmt.Errorf("invalid trust metadata identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, document.IssuedAt); err != nil {
		return fmt.Errorf("invalid trust metadata timestamp")
	}
	seen := map[string]bool{}
	for _, key := range document.Keys {
		if !stableID.MatchString(key.ID) || key.Algorithm != "Ed25519" || key.Purpose != document.Purpose || seen[key.ID] {
			return fmt.Errorf("invalid or duplicate trust key")
		}
		publicKey, err := ParsePublicKey(key.PublicKey)
		if err != nil || KeyID(publicKey) != key.ID {
			return fmt.Errorf("trust key id does not match public key")
		}
		usageSeen := map[KeyUsage]bool{}
		for _, usage := range key.Usages {
			if (usage != TrustMetadata && usage != ContentSigning) || usageSeen[usage] {
				return fmt.Errorf("invalid or duplicate trust key usage")
			}
			usageSeen[usage] = true
		}
		created, err1 := time.Parse(time.RFC3339Nano, key.CreatedAt)
		active, err2 := time.Parse(time.RFC3339Nano, key.ActiveFrom)
		if err1 != nil || err2 != nil || active.Before(created) {
			return fmt.Errorf("invalid trust key lifecycle timestamps")
		}
		if key.RetiredAt != "" {
			retired, err := time.Parse(time.RFC3339Nano, key.RetiredAt)
			if err != nil || retired.Before(active) {
				return fmt.Errorf("invalid retirement timestamp")
			}
		}
		if key.RevokedAt != "" {
			if _, err := time.Parse(time.RFC3339Nano, key.RevokedAt); err != nil || strings.TrimSpace(key.RevocationReason) == "" {
				return fmt.Errorf("revocation requires timestamp and reason")
			}
		} else if key.RevocationReason != "" {
			return fmt.Errorf("revocation reason requires revocation timestamp")
		}
		seen[key.ID] = true
	}
	for _, key := range document.Keys {
		if key.SuccessorKeyID != "" && (!seen[key.SuccessorKeyID] || key.SuccessorKeyID == key.ID) {
			return fmt.Errorf("invalid successor key")
		}
	}
	if document.MinimumVersions.Software != "" {
		if _, err := CompareVersions(document.MinimumVersions.Software, document.MinimumVersions.Software); err != nil {
			return err
		}
	}
	for id, version := range document.MinimumVersions.Packs {
		if id == "" {
			return fmt.Errorf("minimum Pack version needs Pack id")
		}
		if _, err := CompareVersions(version, version); err != nil {
			return err
		}
	}
	return nil
}

func keyPermits(key Key, at time.Time, rejectRevokedHistorically bool) bool {
	active, err := time.Parse(time.RFC3339Nano, key.ActiveFrom)
	if err != nil || at.Before(active) {
		return false
	}
	if key.RevokedAt != "" && rejectRevokedHistorically {
		return false
	}
	if key.RetiredAt != "" {
		retired, _ := time.Parse(time.RFC3339Nano, key.RetiredAt)
		if !at.Before(retired) {
			return false
		}
	}
	return true
}

func verifySignature(document Document, publicKey ed25519.PublicKey, expectedID string) error {
	if err := validate(document); err != nil {
		return err
	}
	if document.Signature.Algorithm != "Ed25519" || document.Signature.KeyID != expectedID {
		return fmt.Errorf("trust metadata signature identity is invalid")
	}
	value, err := base64.StdEncoding.DecodeString(document.Signature.Value)
	if err != nil || len(value) != ed25519.SignatureSize {
		return fmt.Errorf("trust metadata signature encoding is invalid")
	}
	payload, err := signingBytes(document)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, value) {
		return fmt.Errorf("trust metadata signature is invalid")
	}
	return nil
}

func signingBytes(document Document) ([]byte, error) {
	keys := append([]Key(nil), document.Keys...)
	sort.Slice(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
	payload := unsignedDocument{document.SchemaVersion, document.Purpose, document.Sequence, document.IssuedAt, keys, document.MinimumVersions}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func findKey(document Document, id string) (Key, bool) {
	for _, key := range document.Keys {
		if key.ID == id {
			return key, true
		}
	}
	return Key{}, false
}

// HasUsage reports whether a key may perform an operation. Metadata written
// before key usages were introduced had no usage field and remains compatible.
func HasUsage(key Key, usage KeyUsage) bool {
	if len(key.Usages) == 0 {
		return true
	}
	for _, candidate := range key.Usages {
		if candidate == usage {
			return true
		}
	}
	return false
}

func KeyID(key ed25519.PublicKey) string {
	sum := sha256.Sum256(key)
	return "ed25519:" + hex.EncodeToString(sum[:12])
}
func EncodePublicKey(key ed25519.PublicKey) string { return base64.StdEncoding.EncodeToString(key) }
func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(data) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid Ed25519 public key")
	}
	return ed25519.PublicKey(data), nil
}

func Encode(document Document) ([]byte, error) {
	if err := validate(document); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func Decode(data []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Document{}, fmt.Errorf("trailing trust metadata")
	}
	if err := validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

func CompareVersions(left, right string) (int, error) {
	parse := func(value string) ([]uint64, error) {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
		if len(parts) < 2 || len(parts) > 4 {
			return nil, fmt.Errorf("version %q must contain 2-4 numeric components", value)
		}
		out := make([]uint64, len(parts))
		for i, p := range parts {
			n, err := strconv.ParseUint(p, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("version %q is not numeric", value)
			}
			out[i] = n
		}
		return out, nil
	}
	a, err := parse(left)
	if err != nil {
		return 0, err
	}
	b, err := parse(right)
	if err != nil {
		return 0, err
	}
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for len(b) < len(a) {
		b = append(b, 0)
	}
	for i := range a {
		if a[i] < b[i] {
			return -1, nil
		}
		if a[i] > b[i] {
			return 1, nil
		}
	}
	return 0, nil
}
