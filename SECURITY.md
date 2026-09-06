# Security policy

## Supported release line

Only the current CLI/MCP release line is maintained. The hosted website and public API are intentionally offline.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Contact the project maintainer through the repository's private security advisory feature, with a minimal reproduction and impact assessment. Do not include credentials, access tokens, personal data, complete URLs containing credentials, request bodies, or extracted page content.

Reports are acknowledged within five business days. The maintainer will coordinate a fix, validation, and disclosure timeline with the reporter. No production deployment or release is automatic.

## Scope notes

The local HTTP server is loopback-only by design. Exposing it through a tunnel, reverse proxy, DNS record, or firewall rule is out of scope until an explicit production security review is completed. The CLI's `--file` option is intentionally local-only and is not accepted by the HTTP API.
