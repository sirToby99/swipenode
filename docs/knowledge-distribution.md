# Knowledge Distribution

SwipeNode's managed/customer boundary is a one-way distribution of public source-policy metadata. The managed layer may publish reviewed Knowledge Packs, versions, release metadata, and public source health. Customer claims, repositories, private Packs, evidence, verification history, audit events, provenance, Entire context, and engineering artifacts remain customer-local and are not part of the protocol.

## Release format

`swipenode.pack-release.v1` is a deterministic, uncompressed tar artifact (`.snpkg`) containing exactly:

- `release.json`: canonical release manifest;
- `pack.yaml`: validated source-policy metadata, never copied documentation;
- `signature.json`: Ed25519 signature metadata.

The manifest identifies the Pack and immutable version, schema and source-policy versions, publisher, release timestamp, optional signed predecessor, release notes, file sizes and SHA-256 hashes, signature algorithm, and key ID. The Ed25519 signature covers the exact canonical manifest bytes; the manifest covers every packaged policy file. Identical Pack bytes, key, timestamp, version, and metadata produce identical artifacts.

Private keys are generated and held by the publisher and are never embedded in an artifact or repository. Customers configure independently obtained public keys as trusted remotes. The key ID is derived from the public key. Inspection is not trust: `inspect` validates structure and hashes, while `verify-package` also proves the publisher signature against a configured key.

## Discovery and activation

The versioned `swipenode.knowledge-registry.v1` discovery documents expose current/available versions, same-origin manifest and artifact URLs, SHA-256, key ID, signature algorithm, release timestamp, publisher, and predecessor. Registry downloads use the same DNS, IP, TLS, redirect, and SSRF controls as source retrieval. Remotes require HTTPS, contain no credentials/query/fragment, and returned URLs must remain on the exact configured origin.

```text
swipenode knowledge keygen --private-key publisher.key --public-key publisher.pub
swipenode knowledge package nvidia-jetson \
  --version 2026.09.1 --publisher SwipeNode \
  --created-at 2026-09-01T12:00:00Z \
  --signing-key publisher.key --output nvidia-jetson.snpkg
swipenode knowledge verify-package nvidia-jetson.snpkg --trusted-key publisher.pub

swipenode knowledge remote add \
  --id managed --url https://knowledge.example \
  --publisher SwipeNode --trusted-key publisher.pub
swipenode knowledge update-check nvidia-jetson --remote managed
swipenode knowledge pull nvidia-jetson --remote managed
swipenode knowledge activate nvidia-jetson@2026.09.1
swipenode knowledge rollback nvidia-jetson
```

Pull is a staged operation: discover, download, compare the registry SHA-256, verify the signed artifact against the trusted publisher key, and install an immutable local copy. It does not activate. Activation re-verifies the installed artifact and only permits a normal upgrade whose signed predecessor equals the current active version. Rollback is explicit and restores activation history without deleting any installed release. Policy values `manual`, `auto-download`, and `auto-update` are persisted, but Phase 1 deliberately includes no scheduler.

The active managed policy is exported into the repository-local Git metadata state and overlays the corresponding built-in Pack. Project-local Packs cannot silently replace built-in or managed IDs. Once installed and activated, resolution and verification need no managed-server connection.

## Threat model

- A compromised server cannot forge a release without the trusted publisher key; registry SHA mismatch, artifact tampering, or wrong keys fail closed.
- A compromised publisher key is handled through separately obtained, signed Knowledge Publisher trust metadata. It records successor keys, retirement/revocation reason and time, and conservatively rejects releases from a revoked key, including historical artifacts.
- Normal upgrades are predecessor-bound to resist accidental downgrade or version skipping. Signed minimum-version policy blocks replay below the accepted minimum. Customer-local explicit rollback remains available only for an already installed artifact and never bypasses signature or revocation validation.
- Malicious project Packs cannot override managed/built-in IDs or bypass global SSRF, private/loopback/metadata IP, HTTPS, DNS, credential, or redirect protections.
- Unsigned or structurally invalid packages cannot be installed through `pull` or activated. Installed artifacts are signature-checked again at activation.
- The distribution client sends only GET requests for public registry/artifact paths. There is no upload API and no customer-state serialization in the protocol.

This is the Knowledge Distribution Protocol only. It does not add a hosted server, customer control plane, internal admin, accounts, billing, telemetry, or customer-data access.
