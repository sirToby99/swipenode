# SwipeNode Commercial License and Security Audit

Audit date: 2026-09-01

Technical-remediation baseline: `e893703f3dc404765108a27a0c70b3811a5564d8`

Required build and release toolchain: Go 1.25.13, `CGO_ENABLED=0`

This is an engineering compliance review, not legal advice. The technical commercial-distribution blockers identified in the Phase 3.5 baseline have been removed. SwipeNode's own license, licensor/contributor rights, customer rights, Change-Date strategy, and paid Knowledge Pack terms remain explicit owner/legal decisions and were not changed.

## Scope and method

The audit covers both release binaries on Linux, macOS, and Windows; the selected Go module graph; embedded Go/YAML/HTML/CSS/JavaScript/OpenAPI; source assets; signing and installer scripts; and clean release archives. Cross-platform `go version -m` inspection establishes a release union of the Go runtime plus exactly 17 modules. The machine-readable source of truth is [`dependency-licenses.json`](dependency-licenses.json); [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) contains exact license evidence from the pinned toolchain and checksum-verified module archives; [`sbom.spdx.json`](sbom.spdx.json) is the deterministic SPDX 2.3 SBOM.

## Release-linked inventory

All 17 release-linked modules are GREEN: commercial use, modification, and redistribution are permitted with the recorded notice obligations. No release dependency is `NOASSERTION`, REVIEW, or BLOCKING.

| Dependency | Version | Direct | Release target | License | Required action |
|---|---:|---:|---|---|---|
| PuerkitoBio/goquery | v1.11.0 | Yes | SwipeNode | BSD-3-Clause | Reproduce license |
| andybalholm/cascadia | v1.3.3 | No | SwipeNode | BSD-2-Clause | Reproduce license |
| bahlo/generic-list-go | v0.2.0 | No | SwipeNode | BSD-3-Clause | Reproduce license |
| buger/jsonparser | v1.1.2 | No | SwipeNode | MIT | Reproduce license |
| google/uuid | v1.6.0 | No | SwipeNode | BSD-3-Clause | Reproduce license |
| inconshreveable/mousetrap | v1.1.0 | No | Windows | Apache-2.0 | Reproduce license; preserve Apache obligations |
| invopop/jsonschema | v0.13.0 | No | SwipeNode | MIT | Reproduce license |
| mailru/easyjson | v0.7.7 | No | SwipeNode | MIT | Reproduce license |
| mark3labs/mcp-go | v0.45.0 | Yes | SwipeNode | MIT | Reproduce license |
| spf13/cast | v1.7.1 | No | SwipeNode | MIT | Reproduce license |
| spf13/cobra | v1.10.2 | Yes | Both | Apache-2.0 | Reproduce license; preserve Apache obligations |
| spf13/pflag | v1.0.9 | No | Both | BSD-3-Clause | Reproduce license |
| wk8/go-ordered-map/v2 | v2.1.8 | No | SwipeNode | Apache-2.0 | Reproduce license; preserve Apache obligations |
| yosida95/uritemplate/v3 | v3.0.2 | No | SwipeNode | BSD-3-Clause | Reproduce license |
| golang.org/x/net | v0.58.0 | Yes | Both | BSD-3-Clause + PATENTS | Reproduce LICENSE and PATENTS |
| golang.org/x/sys | v0.47.0 | Yes | Entire/Windows | BSD-3-Clause + PATENTS | Reproduce LICENSE and PATENTS |
| gopkg.in/yaml.v3 | v3.0.1 | Yes | Both | MIT AND Apache-2.0 | Reproduce LICENSE and NOTICE |

No GPL, AGPL, LGPL, MPL, EPL, SSPL, reciprocal copyleft, source-available, non-commercial, or custom third-party license is linked into a release binary. Known linked dependencies impose no source-code disclosure. Apache and Go PATENTS grants/termination provisions and all required attribution text are preserved in the generated notices.

The module graph additionally contains 24 test/tool-only modules that did not appear in any inspected release binary: go-md2man/v2, go-spew, quicktest, go-cmp, josharian/intern, kr/pretty, kr/pty, kr/text, go-difflib, rogpeppe/go-internal, blackfriday/v2, stretchr/objx, stretchr/testify, goldmark, go.yaml.in/yaml/v3, x/crypto, x/mod, x/sync, x/telemetry, x/term, x/text, x/tools, x/xerrors, and gopkg.in/check.v1. Their exact module license files were inspected; all are permissive MIT/BSD/ISC/Apache-family material and none is shipped in the current binary archives.

## Removal of browser-impersonation dependencies

`github.com/bogdanfinn/fhttp` and `github.com/bogdanfinn/tls-client` existed only in the legacy `extract`/`batch` path to imitate Chrome, Firefox, or Safari TLS fingerprints. Browser impersonation and anti-bot evasion are not SwipeNode requirements. The path now uses Go `net/http` with deterministic compatibility headers while retaining TLS verification, DNS resolution and all-address validation, address pinning, proxy disablement, redirect refusal, timeouts, response bounds, original Host/SNI routing, and credential rejection.

The migration removes `fhttp`, `tls-client`, and their 11 linked support modules from source, `go.mod`, `go.sum`, selected graph, and every cross-platform release binary. The missing `fhttp` license grant and `tls-client` BSD-4 placeholder therefore create no distribution obligation for the final product.

## Toolchain and vulnerability policy

`go.mod` declares Go 1.25.13 as the exact minimum. CI and release jobs install Go 1.25.13 and set `GOTOOLCHAIN=local`, preventing silent release builds with an older or automatically substituted toolchain. Both workflows assert `go env GOVERSION` and the module Go version before testing or building.

The vulnerable selections were replaced by `x/net v0.58.0` and selected `x/text v0.41.0`, exceeding the required fixed floors. `jsonparser` was patch-upgraded to v1.1.2 to remove a remaining imported-but-unreachable advisory. `govulncheck v1.7.0 ./...` with Go 1.25.13 reports no vulnerabilities found, zero called vulnerabilities, zero imported-package vulnerabilities, and zero required-module vulnerabilities.

## Software release authenticity

Knowledge Pack signatures and software-release signatures are separate identities. Release CI computes SHA-256 for every archive, signs the exact checksum manifest with an independently managed Ed25519 key using the established OpenSSH SSHSIG format and namespace `swipenode-release`, and self-verifies against the configured release public key before publication. The signing key is held only in protected release configuration and a mode-0600 ephemeral runner file; it is never archived or uploaded.

The installer compares the repository and Managed Knowledge Trust channels,
then its source-auditable bootstrap verifier authenticates root-signed
`software_release` metadata without executing the candidate binary. It enforces
signer usage, revocation, rotation, and the signed minimum version before
authenticating `checksums.txt`, then validates the selected archive SHA-256.
Missing, disagreeing, tampered, wrong-key, revoked, or malformed inputs fail
closed. See [`release-security.md`](../release-security.md).
Production publishing remains administratively blocked until maintainers
configure the protected keys and publish roots/fingerprints through independent
channels; tests need no production key.

## Frontend and assets

The embedded Control Plane is project-authored HTML, CSS, JavaScript, and OpenAPI with no npm dependency, lockfile, CDN, external script, icon library, or bundled font. Built-in Packs contain metadata and source URLs, not copied documentation.

`swipenode.png` is first-party/project-generated branding. The project owner confirmed that it was generated specifically for SwipeNode using Anthropic Claude, that no third-party source material was intentionally incorporated, and that it is intended for SwipeNode branding/product distribution. Current Anthropic commercial terms state that, as between the parties and to the extent permitted by law, the customer owns Outputs and receives assignment of Anthropic's rights, if any; current consumer terms similarly assign Anthropic's rights, if any, to the user subject to the terms. No contrary output-use restriction was found. The former unidentified-asset blocker is removed. Full evidence and the distinction between contractual usage rights and uncertain statutory AI authorship/copyright are recorded in [`asset-provenance.md`](asset-provenance.md). The image is unchanged and remains excluded from binary archives.

## SwipeNode's own license: unresolved owner/legal decisions

The root “Functional Source License v1.1” was not modified. The following are outside this technical gate and require an owner/legal decision:

- identify the legal licensor and confirm rights from all contributors;
- state customer reproduction, modification, derivative-work, and redistribution rights precisely;
- resolve the fixed Change-Date versus rolling two-year language;
- decide permanent proprietary versus automatic Apache-2.0 conversion strategy;
- define paid managed Knowledge Pack ownership and customer-use/update terms;
- assess statutory copyright/trademark protection and clearance for AI-generated branding.

These issues determine the final commercial legal posture even though the technical dependency/distribution gate is clean.

## Reproducible compliance and release checks

Strict generation succeeds without an unresolved override:

```sh
GOTOOLCHAIN=local go run ./tools/compliance
git diff --exit-code -- docs/compliance/THIRD_PARTY_NOTICES.md docs/compliance/sbom.spdx.json
```

The strict generator fails if any release dependency is non-GREEN, lacks a recognized license or exact license file, or does not have affirmative commercial-use/modification/redistribution evidence. The release workflow runs this before building.

The final clean acceptance also runs:

```sh
go mod tidy
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
git diff --check
sh scripts/test-release-signing.sh
sh scripts/test-entire-release-package.sh
```

Release archive inspection must show only the intended binary, SwipeNode `LICENSE`, generated `THIRD_PARTY_NOTICES.md`, and SPDX SBOM. `checksums.txt` and its detached SSHSIG signature are separate release assets. No production signing private key, project/customer fixture, private state, payment/deploy content, or test artifact may be included.
