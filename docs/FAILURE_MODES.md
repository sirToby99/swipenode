# Customer-path failure behavior

The public tests exercise fail-closed behavior; they do not weaken verification
to make a demo pass.

| Failure | Expected behavior |
| --- | --- |
| Unsupported OS/architecture | installer or release builder rejects it |
| Missing/disagreeing Trust channels | installer stops before downloading or executing a candidate binary |
| Tampered Trust metadata or unauthorized release key | bootstrap verifier rejects it |
| Wrong archive hash | installer refuses replacement |
| Tampered checksum manifest/signature | SSHSIG verification fails |
| Unknown/revoked/unauthorized key | Trust authorization fails |
| Tampered `.snpkg` or wrong registry hash | pull fails before installation |
| Wrong/missing Pack version | install/activation fails |
| Managed API unavailable | update/pull fails; already activated Pack/local records remain usable |
| Source unavailable | fetch warning; Claim cannot become VERIFIED from missing Evidence |
| Unsupported Claim | `UNVERIFIED` |
| Stale Evidence | explicit refresh/revalidation is required and history is retained |
| Repeated activation | idempotently re-verifies the same installed release |
| Rollback | only a previously installed, signature-valid history entry may activate |

Relevant public suites include `internal/distribution`, `internal/trust`,
`internal/softwaretrust`, `internal/verification`, `internal/knowledge`,
`scripts/test-installer-trust-chain.sh` and
`scripts/test-software-release-pipeline.sh`.
