# Customer Control Plane

The SwipeNode Customer Control Plane is a customer-hosted governance surface over the existing local SwipeNode Runtime. Agents, MCP, CLI, the local API, and the UI use the same Knowledge Registry, signed managed Pack installation state, project-local Packs, Evidence Store, immutable verification history, Audit Events, refresh/revalidation service, and Engineering Provenance. The UI owns no second database and contains no independent verification, trust, distribution, refresh, or provenance logic.

```text
Agents / MCP / CLI / local API
              |
              v
        SwipeNode Core
              ^
              |
   Customer Control Plane
```

Run it from a Git worktree:

```sh
swipenode ui
# http://127.0.0.1:8082
```

The default and enforced listener is a literal loopback IP. Non-loopback, wildcard, and hostname-based binding are refused. Static HTML, CSS, JavaScript, and OpenAPI are embedded in the SwipeNode Go binary; customer runtime needs no Node/npm process, CDN, remote font, external script, or separate frontend server. The interface is desktop-first and collapses to tablet/mobile navigation.

## Views and shared operations

- **Overview** derives installed/active Pack, source-health, attention, audit, and Engineering Provenance summaries from the current Core records. Missing Core data stays unknown; the UI does not invent metrics.
- **Knowledge Packs** distinguishes signed managed installations, project-private policies, and built-ins. It exposes explicit update-check, pull, activate, rollback, refresh, dry-run, and refresh-plus-revalidate operations.
- **Evidence Explorer** projects the Claim → Verification → Evidence → Pack/Source → Revision/SHA-256 relationships recorded by the Evidence and verification stores.
- **Engineering Provenance** shows generic engineering artifacts—including URDF, YAML, BOM, requirements, simulation, and hardware configuration—plus claims, evidence IDs, Git context, audit relationships, and optional Entire enrichment.
- **Audit / Refresh** displays source checks, changes, failures, revision/hash transitions, evidence creation, revalidation, and verification status transitions.
- **Governance** displays and changes the local manual/auto-download/auto-update policy, lists trusted publisher key IDs without exposing key material, validates local Pack definitions offline, and inspects project source/authority policy.

Every UI action has a direct CLI equivalent and uses the same Core service:

| Capability | CLI | Local API |
|---|---|---|
| Pack list/show | `knowledge list/show` | `GET /api/v1/knowledge/packs[/{id}]` |
| Local Pack validation | `knowledge validate` | `GET /api/v1/knowledge/validate` |
| Update check/pull | `knowledge update-check/pull` | explicit Pack action `POST` |
| Activate/rollback | `knowledge activate/rollback` | explicit Pack action `POST` |
| Refresh/revalidate/dry-run | `knowledge refresh` flags | Pack `refresh` `POST` flags |
| Evidence | `evidence list/show` | `GET /api/v1/evidence[/{id}]` |
| Verification history | `verification list/show` | `GET /api/v1/verifications[/{id}]` |
| Audit | `audit list/show` | `GET /api/v1/audit[/{id}]` |
| Engineering Provenance | `provenance list/show` | `GET /api/v1/provenance[/{id}]` |
| Update policy | `knowledge policy show/set` | `POST /api/v1/governance/update-policy` |

The OpenAPI document is served locally at `/openapi.yaml`.

## Customer/managed data boundary

Local browsing is filesystem-only and causes no managed-server network request. The Control Plane never sends customer source code, BOM, CAD/ECAD, URDF, configuration, claims, private Packs, Evidence, verification history, Audit Events, Engineering Provenance, Entire sessions, or engineering artifacts to SwipeNode-managed infrastructure. There is no telemetry endpoint or background updater.

Only a deliberate update-check or pull calls a configured Managed Knowledge Server. Those requests use the Phase 1 public registry/artifact `GET` protocol and carry only the selected public Pack identifier in the path. Pull verifies the configured publisher key, package signature, manifest, hashes, origin, and immutable version before staging locally; it does not activate implicitly. Pack activation, rollback, verification, refresh, revalidation, Evidence, Audit, and Provenance remain customer-local. Refresh contacts only deterministic Pack source endpoints and remains separately explicit.

Optional Entire context follows the same boundary: it is read locally and may enrich only the local Engineering Provenance record. It is never serialized into update-check or pull requests. The Managed Knowledge Server and Internal Admin define no customer Evidence, Verification, Audit, Provenance, engineering-artifact, or Entire-session ingestion endpoints, so the normal boundary flow remains signed public Pack download only.

Entire is optional enrichment. It does not own the Knowledge Registry, Evidence Store, verification, Control Plane, or customer runtime.

## Local security boundary

- A loopback Host header is required in addition to the loopback-only listener.
- Mutations require JSON, a per-process same-origin CSRF token, and reject cross-site browser requests and foreign Origins.
- No CORS access is granted. CSP permits only embedded same-origin assets and API calls; framing, plugins, base rewriting, and external forms are denied.
- UI rendering uses DOM `textContent`; source HTML and Pack metadata are never interpreted as HTML. Outbound links are offered only for sanitized HTTPS URLs and use `noopener noreferrer`.
- Evidence/verification display strips URL credentials, queries, and fragments and redacts common secret assignments. Trusted public signing key material is intentionally not returned by the UI API.
- Routes use fixed operations and validated IDs. Query parameters, arbitrary commands, file paths, traversal, and local-file reads are not accepted.
- Project Pack validation and every refresh/fetch retain the Core's hostname, HTTPS, DNS, redirect, private/loopback/metadata-IP, and SSRF controls. A malicious private Pack cannot weaken them.

This local Control Plane is not the Internal Knowledge Admin and does not cross that trust boundary. It has no hosted customer account, central customer inspection, telemetry, billing, marketplace, editor, chat, or public deployment mode.
