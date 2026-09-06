// Command testfixture creates ephemeral public trust metadata for the
// installer black-box test. It is deliberately scoped under internal test
// support and must never be used for production key generation.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sirToby99/swipenode/internal/trust"
)

func main() {
	var releasePublicPath, metadataPath, rootPath string
	flag.StringVar(&releasePublicPath, "release-public", "", "ephemeral OpenSSH release public key")
	flag.StringVar(&metadataPath, "metadata", "", "output metadata path")
	flag.StringVar(&rootPath, "root", "", "output root public key path")
	flag.Parse()
	if releasePublicPath == "" || metadataPath == "" || rootPath == "" {
		fail("all paths are required")
	}
	releaseData, err := os.ReadFile(releasePublicPath)
	if err != nil {
		fail(err.Error())
	}
	releasePublic, err := trust.ParseOpenSSHPublicKey(string(releaseData))
	if err != nil {
		fail(err.Error())
	}
	rootPublic, rootPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail(err.Error())
	}
	now := time.Now().UTC().Truncate(time.Second)
	entry := func(public ed25519.PublicKey, usage trust.KeyUsage) trust.Key {
		return trust.Key{
			ID:         trust.KeyID(public),
			Algorithm:  "Ed25519",
			PublicKey:  trust.EncodePublicKey(public),
			CreatedAt:  now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			ActiveFrom: now.Add(-time.Hour).Format(time.RFC3339Nano),
			Purpose:    trust.SoftwareRelease,
			Usages:     []trust.KeyUsage{usage},
		}
	}
	document, err := trust.Sign(trust.Document{
		Purpose:         trust.SoftwareRelease,
		Sequence:        1,
		IssuedAt:        now.Format(time.RFC3339Nano),
		Keys:            []trust.Key{entry(rootPublic, trust.TrustMetadata), entry(releasePublic, trust.ContentSigning)},
		MinimumVersions: trust.MinimumVersions{Software: "2.1.0"},
	}, trust.KeyID(rootPublic), rootPrivate)
	if err != nil {
		fail(err.Error())
	}
	metadata, err := trust.Encode(document)
	if err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(metadataPath, metadata, 0o644); err != nil {
		fail(err.Error())
	}
	if err := os.WriteFile(rootPath, []byte(trust.EncodePublicKey(rootPublic)+"\n"), 0o644); err != nil {
		fail(err.Error())
	}
	// Ensure the test helper never serializes the generated root private key.
	for _, path := range []string{metadataPath, rootPath} {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), "PRIVATE KEY") || strings.Contains(string(data), fmt.Sprintf("%x", rootPrivate)) {
			fail("private test key material reached public fixture output")
		}
	}
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "testfixture:", message)
	os.Exit(1)
}
