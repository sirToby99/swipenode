package softwaretrust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sirToby99/swipenode/internal/trust"
)

var installerTestNow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func installerPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

func installerKey(public ed25519.PublicKey, usage trust.KeyUsage) trust.Key {
	return trust.Key{
		ID:         trust.KeyID(public),
		Algorithm:  "Ed25519",
		PublicKey:  trust.EncodePublicKey(public),
		CreatedAt:  installerTestNow.Add(-2 * time.Hour).Format(time.RFC3339Nano),
		ActiveFrom: installerTestNow.Add(-time.Hour).Format(time.RFC3339Nano),
		Purpose:    trust.SoftwareRelease,
		Usages:     []trust.KeyUsage{usage},
	}
}

func installerMetadata(t *testing.T, root ed25519.PrivateKey, keys []trust.Key, minimum string) []byte {
	t.Helper()
	document, err := trust.Sign(trust.Document{
		Purpose:         trust.SoftwareRelease,
		Sequence:        7,
		IssuedAt:        installerTestNow.Add(-time.Minute).Format(time.RFC3339Nano),
		Keys:            keys,
		MinimumVersions: trust.MinimumVersions{Software: minimum},
	}, trust.KeyID(root.Public().(ed25519.PublicKey)), root)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := trust.Encode(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestI01ValidTrustMetadataAndReleaseAccepted(t *testing.T) {
	rootPublic, rootPrivate := installerPair(t)
	releasePublic, _ := installerPair(t)
	metadata := installerMetadata(t, rootPrivate, []trust.Key{
		installerKey(rootPublic, trust.TrustMetadata),
		installerKey(releasePublic, trust.ContentSigning),
	}, "2.0.0")
	result, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "v2.1.0", installerTestNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AuthorizedKeyIDs) != 1 || result.AuthorizedKeyIDs[0] != trust.KeyID(releasePublic) || strings.Contains(strings.Join(result.AllowedSigners, "\n"), trust.KeyID(rootPublic)) {
		t.Fatalf("unexpected authorized release keys: %+v", result)
	}
}

func TestI02TamperedTrustMetadataRejected(t *testing.T) {
	rootPublic, rootPrivate := installerPair(t)
	releasePublic, _ := installerPair(t)
	metadata := installerMetadata(t, rootPrivate, []trust.Key{installerKey(rootPublic, trust.TrustMetadata), installerKey(releasePublic, trust.ContentSigning)}, "2.0.0")
	var raw map[string]any
	if err := json.Unmarshal(metadata, &raw); err != nil {
		t.Fatal(err)
	}
	raw["minimum_versions"].(map[string]any)["software"] = "9.0.0"
	metadata, _ = json.Marshal(raw)
	if _, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "9.0.0", installerTestNow, false); err == nil {
		t.Fatal("tampered metadata accepted")
	}
}

func TestI03RevokedReleaseKeyRejected(t *testing.T) {
	rootPublic, rootPrivate := installerPair(t)
	releasePublic, _ := installerPair(t)
	release := installerKey(releasePublic, trust.ContentSigning)
	release.RevokedAt = installerTestNow.Add(-time.Minute).Format(time.RFC3339Nano)
	release.RevocationReason = "test compromise"
	metadata := installerMetadata(t, rootPrivate, []trust.Key{installerKey(rootPublic, trust.TrustMetadata), release}, "2.0.0")
	if _, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "2.1.0", installerTestNow, false); err == nil {
		t.Fatal("revoked release key accepted")
	}
}

func TestI04UnauthorizedReplacementKeyRejected(t *testing.T) {
	rootPublic, _ := installerPair(t)
	unknownPublic, unknownPrivate := installerPair(t)
	releasePublic, _ := installerPair(t)
	metadata := installerMetadata(t, unknownPrivate, []trust.Key{installerKey(unknownPublic, trust.TrustMetadata), installerKey(releasePublic, trust.ContentSigning)}, "2.0.0")
	if _, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "2.1.0", installerTestNow, false); err == nil {
		t.Fatal("metadata signed by an unrelated replacement root was accepted")
	}
}

func TestI05ValidRotatedReleaseKeyAccepted(t *testing.T) {
	rootPublic, rootPrivate := installerPair(t)
	oldPublic, _ := installerPair(t)
	newPublic, _ := installerPair(t)
	oldKey := installerKey(oldPublic, trust.ContentSigning)
	oldKey.RetiredAt = installerTestNow.Add(-time.Minute).Format(time.RFC3339Nano)
	oldKey.SuccessorKeyID = trust.KeyID(newPublic)
	metadata := installerMetadata(t, rootPrivate, []trust.Key{installerKey(rootPublic, trust.TrustMetadata), oldKey, installerKey(newPublic, trust.ContentSigning)}, "2.0.0")
	result, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "2.1.0", installerTestNow, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AuthorizedKeyIDs) != 1 || result.AuthorizedKeyIDs[0] != trust.KeyID(newPublic) {
		t.Fatalf("rotation did not select only the successor: %+v", result.AuthorizedKeyIDs)
	}
}

func TestI06StaleReleaseBelowMinimumRejectedUnlessExplicitlyOverridden(t *testing.T) {
	rootPublic, rootPrivate := installerPair(t)
	releasePublic, _ := installerPair(t)
	metadata := installerMetadata(t, rootPrivate, []trust.Key{installerKey(rootPublic, trust.TrustMetadata), installerKey(releasePublic, trust.ContentSigning)}, "2.2.0")
	if _, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "2.1.0", installerTestNow, false); err == nil {
		t.Fatal("release below signed minimum accepted")
	}
	result, err := AuthorizedSigners(metadata, trust.EncodePublicKey(rootPublic), "2.1.0", installerTestNow, true)
	if err != nil || !result.DowngradeOverrideUsed {
		t.Fatalf("explicit downgrade override failed: %+v %v", result, err)
	}
}
