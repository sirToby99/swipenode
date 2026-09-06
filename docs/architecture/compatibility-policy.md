# Public interface and persisted-schema compatibility

SwipeNode treats its CLI, loopback APIs, managed distribution API, and
customer-local persisted state as versioned contracts. A change is compatible
only when an existing valid caller or record keeps its meaning and security
boundary.

## HTTP and CLI policy

- Existing command names, flags, exit behavior, and documented JSON fields are
  retained within a released major version. New optional fields may be added;
  existing fields are not silently reinterpreted.
- The Customer Control Plane and Internal Admin are loopback-only. Their
  browser mutations remain same-origin, CSRF-protected JSON operations.
- The Managed Knowledge API remains public/read-only and never gains customer
  Evidence, Claims, Audit, Provenance, Entire, or engineering-file writes.
- Each embedded OpenAPI document has an exact method/path contract test against
  the corresponding registered surface. Adding, removing, or changing a route
  therefore requires an intentional OpenAPI and test update in the same change.
- Collection endpoints may offer bounded pagination and local filtering. An
  unparameterized Customer Control Plane list retains its original full-list
  response for existing API callers; the GUI requests bounded pages.

## Persisted schemas

Current persisted/public formats include:

| Concept | Current schema |
| --- | --- |
| Knowledge Pack | `swipenode.knowledge-pack.v1` |
| Evidence record | `swipenode.evidence-record.v1` |
| Verification record | `swipenode.verification-record.v1` |
| Audit Event | `swipenode.audit-event.v1` |
| Engineering Provenance | `swipenode.engineering-provenance.v1` |
| distribution state | `swipenode.knowledge-distribution-state.v1` |
| Pack release / signature | `swipenode.pack-release.v1` / `swipenode.pack-signature.v1` |
| managed registry | `swipenode.knowledge-registry.v1` |
| trust metadata | `swipenode.trust-metadata.v1` |

Readers accept known v1 records and fail closed on an unknown schema version;
they do not silently reinterpret a future record as v1. New in-memory records
may omit the schema only before the product prepares and persists them. Evidence,
Verification, Audit, and Provenance tests retain explicit known-v1 fixtures and
unknown-version rejection cases.

Trust metadata has one documented v1 migration exception: an empty key-usage
list retains the original v1 meaning so already-installed metadata stays
readable. Newly issued metadata must explicitly distinguish `trust_metadata`
from `content_signing` usage.

## Change procedure

An incompatible persisted-format change requires a new schema identifier, a
bounded and tested migration/read path, a rollback plan, and fixture coverage
for every still-supported predecessor. Unknown versions remain untouched on
disk and produce an actionable error. Destructive in-place migration and
best-effort parsing are not acceptable.

OpenAPI, CLI help, GUI calls, machine-readable schemas, compatibility fixtures,
and operator documentation must land together. The release checklist blocks a
release if their route, method, trust-domain, or schema identifiers disagree.
