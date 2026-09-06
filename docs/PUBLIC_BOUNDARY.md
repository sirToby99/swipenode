# Public customer boundary

| Capability | State | Location |
| --- | --- | --- |
| Customer CLI and stdio MCP | IMPLEMENTED | this repository |
| Customer-local Runtime and Control Plane | IMPLEMENTED | this repository |
| Knowledge Pack distribution client and public schemas | IMPLEMENTED | this repository |
| Local Claims, Evidence, Verification, Audit and Provenance | IMPLEMENTED | customer Git metadata only |
| Managed public Pack registry and artifacts | VALIDATED | `https://knowledge.swipenode.dev` |
| Signed public software release | PLANNED until a release is signed and published | GitHub Releases |
| Managed Knowledge server implementation | PRIVATE | not exported |
| Internal Knowledge Admin and production operations | PRIVATE | not exported |

Private/project Packs and engineering artifacts stay in the customer-controlled
workspace. Distribution calls are read-only GET requests for public registry and
artifact paths; they do not serialize customer state upstream.

This repository is a history-preserving export. `PUBLIC_EXPORT_PROVENANCE.json`
records the authoritative Product SHA and SHA-256 of every exported file.
