# Software Release Authenticity

SwipeNode software releases use a root-authenticated trust chain and two
artifact integrity checks:

1. An independently obtained Ed25519 trust root authenticates signed
   `software_release` trust metadata.
2. That metadata authorizes active release-signing public keys and enforces
   revocation, retirement, rotation, and the signed minimum software version.
3. `checksums.txt.sig` authenticates the exact checksum manifest using OpenSSH
   SSHSIG Ed25519 namespace `swipenode-release`.
4. `checksums.txt` authenticates each archive's SHA-256 digest.

This identity is exclusively for SwipeNode software releases. It is not a Knowledge Pack publisher key and Pack keys must never be accepted for software installation. The private release key is supplied to the release job through protected secret configuration, is written only to a mode-0600 temporary runner file, and is removed before artifacts are uploaded. Release artifacts contain only the detached signature; they never contain the private key.

## Trust bootstrap and verification

For first installation and updates, use the repository's `scripts/install.sh`
with `scripts/verify-software-trust.py`. The source-auditable bootstrap verifier
uses OpenSSL Ed25519 verification and does not execute a candidate SwipeNode
binary. The installer downloads and compares the Trust Root, signed metadata
and published release key from the repository and Managed Knowledge channels,
then authenticates the metadata and key authorization:

```sh
SWIPENODE_TRUST_BOOTSTRAP_VERIFIER="$PWD/scripts/verify-software-trust.py" \
  sh scripts/install.sh
```

The verifier derives a temporary OpenSSH allowed-signers file only from active
`content_signing` keys in authenticated metadata. A `trust_metadata`-only root
cannot sign a software artifact. Revoked keys are rejected conservatively,
including for historical signatures, and a version below the signed minimum is
rejected. The bootstrap installer has no downgrade or Trust-time bypass.

For a fully offline/manual installation, obtain the Trust Root, signed metadata,
release public key, checksum manifest, detached signature and archive through
approved channels. Run `scripts/verify-software-trust.py` first to produce the
authorized-signers file, then verify the SSHSIG and archive hash. Record and
compare public fingerprints out of band:

```sh
ssh-keygen -lf swipenode-release.pub
```

Authenticate a downloaded checksum manifest before using its hashes:

```sh
sh scripts/verify-release-checksums.sh \
  checksums.txt checksums.txt.sig swipenode-release.pub
sha256sum -c checksums.txt
```

`scripts/verify-release-checksums.sh` remains the offline/manual verification

OpenSSH SSHSIG is used rather than a SwipeNode-specific artifact-signature
encoding. `scripts/test-installer-trust-chain.sh` uses ephemeral keys and covers
authenticated metadata, channel disagreement, tampering, revocation,
unauthorized replacement, rotation, anti-downgrade, bad signatures, bad artifact
hashes, pre-execution ordering, manual offline verification, and secret-material
exclusion. No production key is committed or
required to run tests. Production release publication remains blocked until
repository administrators configure protected signing identities and publish
the matching roots through independent trusted channels.

## Reproducible Go toolchain

The module declares Go 1.25.13 as its exact minimum supported version. CI and release workflows install that exact release and use `GOTOOLCHAIN=local`; explicit assertions stop the job if either the running toolchain or module directive differs. This prevents a release binary from being produced silently with the formerly allowed, known-vulnerable Go 1.24.7 toolchain. Toolchain changes require an explicit `go.mod` and workflow update followed by vulnerability and cross-platform release validation.
