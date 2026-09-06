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

For an update, obtain the base64 Ed25519 software trust root and signed trust
metadata through the documented channels. The root must be pinned independently;
never trust a replacement root merely because the release server supplied it.
Use an already trusted SwipeNode binary as the metadata verifier:

```sh
SWIPENODE_SOFTWARE_TRUST_ROOT_FILE=/secure/trust/software-root.pub \
SWIPENODE_SOFTWARE_TRUST_METADATA_FILE=/secure/trust/software-release.json \
SWIPENODE_TRUST_VERIFIER=/usr/local/bin/swipenode \
SWIPENODE_VERSION=v2.1.0 \
sh scripts/install.sh
```

The verifier derives a temporary OpenSSH allowed-signers file only from active
`content_signing` keys in authenticated metadata. A `trust_metadata`-only root
cannot sign a software artifact. Revoked keys are rejected conservatively,
including for historical signatures, and a version below the signed minimum is
rejected. `SWIPENODE_ALLOW_DOWNGRADE=1` is the explicit operator recovery
override for only the minimum-version check; it does not bypass metadata
authentication, signature verification, key usage, validity, or revocation.

For the first installation or a fully offline recovery with no already trusted
verifier, obtain the software-release SSH public key through an independent
channel. Record and compare its fingerprint out of band:

```sh
ssh-keygen -lf swipenode-release.pub
```

Authenticate a downloaded checksum manifest before using its hashes:

```sh
sh scripts/verify-release-checksums.sh \
  checksums.txt checksums.txt.sig swipenode-release.pub
sha256sum -c checksums.txt
```

The direct-key bootstrap path is intentionally explicit and fails closed if the
signature is absent, malformed, tampered, or signed by another key:

```sh
SWIPENODE_RELEASE_PUBLIC_KEY_FILE=/secure/trust/swipenode-release.pub \
SWIPENODE_ALLOW_DIRECT_RELEASE_KEY=1 \
sh scripts/install.sh
```

`scripts/verify-release-checksums.sh` remains the offline/manual verification
tool. Neither installer path downloads private keys, and trust metadata contains
public keys only.

OpenSSH SSHSIG is used rather than a SwipeNode-specific artifact-signature
encoding. `scripts/test-installer-trust-chain.sh` uses ephemeral keys and covers
authenticated metadata, tampering, revocation, unauthorized replacement,
rotation, anti-downgrade, bad signatures, bad artifact hashes, manual offline
verification, and secret-material exclusion. No production key is committed or
required to run tests. Production release publication remains blocked until
repository administrators configure protected signing identities and publish
the matching roots through independent trusted channels.

## Reproducible Go toolchain

The module declares Go 1.25.13 as its exact minimum supported version. CI and release workflows install that exact release and use `GOTOOLCHAIN=local`; explicit assertions stop the job if either the running toolchain or module directive differs. This prevents a release binary from being produced silently with the formerly allowed, known-vulnerable Go 1.24.7 toolchain. Toolchain changes require an explicit `go.mod` and workflow update followed by vulnerability and cross-platform release validation.
