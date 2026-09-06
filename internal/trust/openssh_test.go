package trust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestOpenSSHEd25519RoundTrip(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeOpenSSHPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseOpenSSHPublicKey(encoded + " test-comment")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parsed, public) {
		t.Fatal("OpenSSH public key changed during round trip")
	}
}

func TestOpenSSHRejectsMalformedAndWrongKeyTypes(t *testing.T) {
	for _, value := range []string{"", "ssh-rsa AAAA", "ssh-ed25519 not-base64", "ssh-ed25519 AAAA"} {
		if _, err := ParseOpenSSHPublicKey(value); err == nil {
			t.Fatalf("accepted malformed key %q", value)
		}
	}
}
