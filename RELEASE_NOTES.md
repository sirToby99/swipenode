# SwipeNode v2.0.1 release candidate notes

Status: unsigned Private Beta release candidate. It is not a trusted public
release until the existing authorized software release key signs the final
checksum manifest and that signature is independently verified.

## Customer-facing changes

- Customer CLI, MCP and customer-hosted runtime for Knowledge Packs, Evidence,
  Verification History, Audit and Provenance.
- Signed `.snpkg` pull, staged installation and explicit activation.
- Fail-closed binary installer that authenticates software Trust metadata,
  release-key authorization, the checksum-manifest signature and archive hash
  before extraction; it never runs the candidate binary during verification.
- Release archives for Linux, macOS and Windows on amd64 and arm64, including
  SBOM, license and Third-Party Notices.

Fetch and revalidation remain explicit operations. The Managed Knowledge API is
read-only and customer engineering state remains local. Entire is optional.
