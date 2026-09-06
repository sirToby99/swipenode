package trust

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

func pair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	p, s, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	return p, s
}
func key(public ed25519.PublicKey, purpose Purpose, active time.Time) Key {
	return Key{ID: KeyID(public), Algorithm: "Ed25519", PublicKey: EncodePublicKey(public), CreatedAt: active.Add(-time.Hour).Format(time.RFC3339Nano), ActiveFrom: active.Format(time.RFC3339Nano), Purpose: purpose}
}
func signed(t *testing.T, purpose Purpose, sequence uint64, keys []Key, minimum MinimumVersions, signer ed25519.PrivateKey) Document {
	t.Helper()
	doc, err := Sign(Document{Purpose: purpose, Sequence: sequence, IssuedAt: testNow.Format(time.RFC3339Nano), Keys: keys, MinimumVersions: minimum}, KeyID(signer.Public().(ed25519.PublicKey)), signer)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestK01InitialTrustRootAccepted(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, SoftwareRelease, 1, []Key{key(pub, SoftwareRelease, testNow.Add(-time.Hour))}, MinimumVersions{Software: "1.0.0"}, priv)
	if err := VerifyInitial(doc, pub, SoftwareRelease, testNow); err != nil {
		t.Fatal(err)
	}
	if err := VerifyInitial(doc, pub, KnowledgePack, testNow); err == nil {
		t.Fatal("software root accepted for Pack trust")
	}
}
func TestK02ValidKeyRotation(t *testing.T) {
	oldPub, oldPriv := pair(t)
	newPub, _ := pair(t)
	old := key(oldPub, KnowledgePack, testNow.Add(-time.Hour))
	first := signed(t, KnowledgePack, 1, []Key{old}, MinimumVersions{}, oldPriv)
	old.SuccessorKeyID = KeyID(newPub)
	next := signed(t, KnowledgePack, 2, []Key{old, key(newPub, KnowledgePack, testNow)}, MinimumVersions{}, oldPriv)
	if err := VerifyUpdate(first, next, testNow); err != nil {
		t.Fatal(err)
	}
}
func TestK03UnknownReplacementKeyRejected(t *testing.T) {
	oldPub, oldPriv := pair(t)
	unknownPub, unknownPriv := pair(t)
	first := signed(t, KnowledgePack, 1, []Key{key(oldPub, KnowledgePack, testNow.Add(-time.Hour))}, MinimumVersions{}, oldPriv)
	next := signed(t, KnowledgePack, 2, first.Keys, MinimumVersions{}, oldPriv)
	payload, err := signingBytes(next)
	if err != nil {
		t.Fatal(err)
	}
	next.Signature = Signature{Algorithm: "Ed25519", KeyID: KeyID(unknownPub), Value: base64.StdEncoding.EncodeToString(ed25519.Sign(unknownPriv, payload))}
	if err := VerifyUpdate(first, next, testNow); err == nil {
		t.Fatal("unknown signer accepted")
	}
}
func TestK04RevokedKeyRejected(t *testing.T) {
	pub, priv := pair(t)
	entry := key(pub, KnowledgePack, testNow.Add(-time.Hour))
	entry.RevokedAt = testNow.Format(time.RFC3339Nano)
	entry.RevocationReason = "compromise"
	doc := signed(t, KnowledgePack, 1, []Key{entry}, MinimumVersions{}, priv)
	if err := Authorize(doc, entry.ID, "1.0.0", "pack", testNow.Add(-time.Hour), false); err == nil {
		t.Fatal("revoked key accepted")
	}
}
func TestK05HistoricalArtifactPolicyIsConservative(t *testing.T) {
	pub, priv := pair(t)
	entry := key(pub, SoftwareRelease, testNow.Add(-2*time.Hour))
	entry.RevokedAt = testNow.Format(time.RFC3339Nano)
	entry.RevocationReason = "compromise invalidates historical signatures"
	doc := signed(t, SoftwareRelease, 1, []Key{entry}, MinimumVersions{}, priv)
	if err := Authorize(doc, entry.ID, "1.0.0", "", testNow.Add(-time.Hour), false); err == nil {
		t.Fatal("pre-revocation artifact accepted after compromise")
	}
}
func TestK06MinimumSoftwareVersionBlocksDowngrade(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, SoftwareRelease, 1, []Key{key(pub, SoftwareRelease, testNow.Add(-time.Hour))}, MinimumVersions{Software: "2.1.0"}, priv)
	if err := Authorize(doc, KeyID(pub), "2.0.9", "", testNow, false); err == nil {
		t.Fatal("software downgrade accepted")
	}
}
func TestK07MinimumPackVersionBlocksReplay(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, KnowledgePack, 1, []Key{key(pub, KnowledgePack, testNow.Add(-time.Hour))}, MinimumVersions{Packs: map[string]string{"robot": "2026.09.2"}}, priv)
	if err := Authorize(doc, KeyID(pub), "2026.09.1", "robot", testNow, false); err == nil {
		t.Fatal("Pack replay accepted")
	}
}
func TestK08ExplicitAuthorizedPackRollbackRemainsPossible(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, KnowledgePack, 1, []Key{key(pub, KnowledgePack, testNow.Add(-time.Hour))}, MinimumVersions{Packs: map[string]string{"robot": "2026.09.2"}}, priv)
	if err := Authorize(doc, KeyID(pub), "2026.09.1", "robot", testNow, true); err != nil {
		t.Fatal(err)
	}
}
func TestK09TamperedTrustMetadataRejected(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, SoftwareRelease, 1, []Key{key(pub, SoftwareRelease, testNow.Add(-time.Hour))}, MinimumVersions{Software: "1.0.0"}, priv)
	doc.MinimumVersions.Software = "9.0.0"
	if err := VerifyInitial(doc, pub, SoftwareRelease, testNow); err == nil {
		t.Fatal("tampering accepted")
	}
}
func TestK10OfflineLastKnownGood(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, KnowledgePack, 1, []Key{key(pub, KnowledgePack, testNow.Add(-time.Hour))}, MinimumVersions{}, priv)
	store := Open(t.TempDir())
	store.now = func() time.Time { return testNow }
	if err := store.InstallInitial(doc, []byte(EncodePublicKey(pub))); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := store.Load(KnowledgePack)
	if err != nil || !ok || loaded.Sequence != 1 {
		t.Fatalf("offline metadata unavailable: %t %v", ok, err)
	}
}
func TestPurposeAndPrivateMaterialBoundaries(t *testing.T) {
	pub, priv := pair(t)
	doc := signed(t, SoftwareRelease, 1, []Key{key(pub, SoftwareRelease, testNow.Add(-time.Hour))}, MinimumVersions{}, priv)
	data, _ := Encode(doc)
	if strings.Contains(string(data), base64Private(priv)) {
		t.Fatal("private key leaked")
	}
}

func TestSignRequiresDeclaredMetadataUsage(t *testing.T) {
	pub, priv := pair(t)
	entry := key(pub, SoftwareRelease, testNow.Add(-time.Hour))
	entry.Usages = []KeyUsage{ContentSigning}
	_, err := Sign(Document{Purpose: SoftwareRelease, Sequence: 1, IssuedAt: testNow.Format(time.RFC3339Nano), Keys: []Key{entry}}, KeyID(pub), priv)
	if err == nil || !strings.Contains(err.Error(), "trust metadata usage") {
		t.Fatalf("content signer signed metadata: %v", err)
	}
}
func base64Private(key ed25519.PrivateKey) string { return EncodePublicKey(ed25519.PublicKey(key)) }
