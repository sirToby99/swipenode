# SwipeNode

Verified engineering knowledge infrastructure for AI agents. This public
repository is the reviewed customer distribution channel for the SwipeNode CLI,
MCP server, customer-hosted runtime, public schemas, release verification tools,
and public Trust metadata.

Status: **Private Beta**.

## What is implemented

- deterministic Knowledge Pack source resolution;
- signed `.snpkg` download, verification, staged installation, activation and rollback;
- customer-local Evidence, Verification History, Audit and Engineering Provenance;
- `VERIFIED`, `CONFLICT` and `UNVERIFIED` outcomes;
- explicit source fetch, refresh and revalidation (no background monitor);
- customer-local Control Plane on loopback and an optional Entire integration;
- stdio MCP tools for extraction and the evidence-backed robotics catalog.

The Managed Knowledge service at <https://knowledge.swipenode.dev> distributes
public Pack and release metadata. It does not ingest customer Claims, Evidence,
Audit, Provenance, source code, CAD, BOM or other private engineering state.

## Start here

- [Customer installation and Trust verification](docs/INSTALL.md)
- [End-to-end NVIDIA verification walkthrough](docs/VERIFY_JETSON.md)
- [Fail-closed customer-path behavior](docs/FAILURE_MODES.md)
- [Public/private capability boundary](docs/PUBLIC_BOUNDARY.md)
- [Knowledge distribution protocol](docs/knowledge-distribution.md)
- [Software release security](docs/release-security.md)

The files under [`trust/`](trust/) are public keys, signed metadata and
fingerprints—not private keys. Compare this repository channel with the Managed
Knowledge-domain mirror when that mirror is advertised by the service. TLS,
artifact hashes, release signatures and publisher authorization are distinct
checks; none substitutes for another.

## Build and test from source

The module requires the exact Go version declared in `go.mod`.

```sh
sh scripts/enforce-go-toolchain.sh
go test ./...
go build -o swipenode .
./swipenode --help
```

The software is licensed under the repository [`LICENSE`](LICENSE). Entire is
optional; normal SwipeNode operation does not require it.
