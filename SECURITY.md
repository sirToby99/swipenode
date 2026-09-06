# Security policy

## Supported release line

Only the current CLI/MCP release line is maintained. The public marketing
website is intentionally offline; the read-only Managed Knowledge API remains
public at `https://knowledge.swipenode.dev`.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Contact the project maintainer through the repository's private security advisory feature, with a minimal reproduction and impact assessment. Do not include credentials, access tokens, personal data, complete URLs containing credentials, request bodies, or extracted page content.

Reports are acknowledged within five business days. The maintainer will coordinate a fix, validation, and disclosure timeline with the reporter. No production deployment or release is automatic.

## Scope notes

The local HTTP server is loopback-only by design. Exposing it through a tunnel, reverse proxy, DNS record, or firewall rule is out of scope until an explicit production security review is completed. The CLI's `--file` option is intentionally local-only and is not accepted by the HTTP API.

The recommended installer authenticates signed software Trust metadata and
release-key authorization, then the signed checksum manifest and platform
archive hash, before extracting the candidate. It never invokes a downloaded
binary as a verifier. Repository and Managed Knowledge Trust files are
comparison channels, not a claim that a root fetched over one of those same
channels is an independent trust anchor.
