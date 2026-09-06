# SwipeNode external command for Entire

## Contract

SwipeNode uses Entire's kubectl-style external-command contract. `entire swipenode <args>` resolves `entire-swipenode` on `PATH` and forwards arguments, stdio, signals, and the child exit code. This is not the Entire external-agent protocol. The external-command contract has no lifecycle callbacks or supported checkpoint metadata-write API.

The implementation reads only Entire's documented optional variables:

- `ENTIRE_CLI_VERSION`
- `ENTIRE_REPO_ROOT`
- `ENTIRE_PLUGIN_DATA_DIR`

`ENTIRE_REPO_ROOT` takes precedence over Git discovery. Missing or partial variables are valid standalone operation. The plugin data directory is created only by a command that actually checks or needs storage; the current feature set does not persist research or verification data and stores no secrets.

The contract was checked against [Entire's official external-command architecture](https://github.com/entireio/cli/blob/main/docs/architecture/external-commands.md) and the installed Entire CLI 0.10.3 command surface. That version exposes read-only `session current --json`, `session info --json`, and checkpoint listing. Sessions and checkpoints are described in [Entire's architecture documentation](https://github.com/entireio/cli/blob/main/docs/architecture/sessions-and-checkpoints.md). `session attach` associates an agent transcript with a commit and may amend it; it is not a general external-metadata attachment mechanism and SwipeNode does not invoke it. The external-agent protocol is for agent providers and transcript lifecycle events, not tool provenance.

The desired future contract is a supported bidirectional reference—`Entire checkpoint <-> SwipeNode provenance ID`—owned by explicit public Entire and SwipeNode APIs. Entire 0.10.3 has no such external-command write interface. Until one exists, SwipeNode only reads bounded session context and records the checkpoint ID on its own customer-local provenance record. It never edits `.entire`, Entire Git refs, session files, checkpoint trees, transcripts, trailers, or commits.

## Commands

### `context`

Reports the safe repository summary, current branch and commit, dirty state, detected top-level manifests/languages, Entire and SwipeNode versions, and MCP availability/configuration. `--json` emits `swipenode.context.v1`. It never prints remotes, environment dumps, credentials, or repository file content.

### `doctor`

Reports `PASS`, `WARN`, and `FAIL` checks for Git, repository resolution, Entire/standalone mode, the optional plugin data path, MCP availability, required runtime tools, and the plugin's no-listener boundary. Warnings exit 0; hard failures exit 1. `--json` emits `swipenode.doctor.v1`.

### `research <question>`

Uses the existing SwipeNode public-HTML extractor for sources supplied by repeated `--source` flags or URLs embedded in the question. It selects short relevant excerpts deterministically and preserves URL, title, retrieval time, and extractor warnings. It does not search the web, synthesize an answer with an LLM, execute JavaScript, or upload repository content. `--json` emits `swipenode.research.v1`.

### `verify`

Reads `git diff` by default, `git diff --cached` with `--staged`, or a safely resolved commit diff with `--ref <git-ref>`. Added lines pass through a deterministic candidate classifier for API/library behavior, hardware limits, protocols, timeouts, operating ranges, versions, compatibility, standards, and vendor behavior. Trivial structural syntax is ignored.

Categories describe the claim prose, not its citation location. URL text is
excluded from category detection but retained for reporting and explicit source
retrieval. For example, a default-port claim remains
`protocol_constant_or_behavior` when its source is an RFC URL; a claim whose
prose explicitly asserts an RFC, standard, or specification identifier is
`standards_specification`.

The JSON contract `swipenode.verify.v1` includes the claim location, category, statement, `VERIFIED`/`CONFLICT`/`UNVERIFIED` status, evidence mode, source authority/type, relevance, version/date when known, confidence, extracted fact, and the classification reason. Verification requires relevant authoritative evidence and a deterministic fact comparison. Fetching a page, finding a URL, or matching a keyword is never sufficient for `VERIFIED`; unsupported comparisons remain `UNVERIFIED`.

Verification is offline by default and performs no network requests. It reads an optional, bounded repository-local `.swipenode/evidence.json` catalog with this format:

```json
{
  "schema_version": "swipenode.evidence.v1",
  "evidence": [
    {
      "source": "https://vendor.example/documentation",
      "source_type": "official_documentation",
      "authority": "authoritative",
      "version_date": "2026-08-29",
      "confidence": 0.98,
      "content": "The Acme API timeout is 30 seconds."
    }
  ]
}
```

Catalog evidence is repository-controlled input, not independently trusted merely because it declares authority. Teams should review it like code and retain a traceable source and version/date. The deterministic MVP compares normalized measurements/durations, version and standards identifiers, explicit compatibility polarity, and exact normalized facts. More nuanced semantic claims remain `UNVERIFIED`.

`verify --fetch` explicitly enables retrieval only for HTTP(S) URLs written in candidate lines. Credentials and local/private/link-local/metadata targets are rejected, sensitive query parameters and fragments are removed before lookup or display, DNS results must all be public, the connection is pinned to the validated address, redirects are disabled, and time/response-size limits apply. Unknown public pages are not treated as authoritative. No diff, file contents, environment variables, or credentials are sent to a search or LLM service.

`verify --knowledge-pack <id>` uses the shared standalone Knowledge Registry and narrows authoritative classification to the selected pack's reviewed endpoint policy. Unknown packs fail clearly and out-of-pack sources remain non-authoritative. The same option is primary on `swipenode verify`; Entire is not required. See [knowledge-infrastructure.md](knowledge-infrastructure.md).

`verify --record-provenance` persists a SwipeNode-owned Engineering Provenance record after evidence and audit events have been recorded. On the standalone command the adapter is `swipenode`. Through `entire-swipenode` or `entire swipenode`, the shared verifier uses a read-only Entire adapter and records the documented CLI version plus current session/last-checkpoint context when it belongs to the exact repository worktree. Context absence never changes the verification result. Because Entire external commands do not expose a supported checkpoint metadata attachment API, the association is stored only in SwipeNode's immutable record; no `.entire` internal is modified.

Entire enrichment is customer-hosted only. The adapter executes the local `entire session current --json` command with the repository as its working directory and writes the bounded result only to the repository-local SwipeNode provenance store. The Managed Knowledge Server is download-only, Internal Admin exposes no Evidence/Audit/Provenance/session ingestion routes, Knowledge CI/CD accepts only public Pack release inputs, and the Pack publisher receives no customer runtime data. No Entire field is included in update-check, pull, Pack publication, or Admin requests, and SwipeNode emits no upstream telemetry.

### `provenance list/show`

Lists or displays repository-local `swipenode.engineering-provenance.v1` records. `--json` is supported. These commands share the standalone implementation:

```sh
swipenode provenance list --json
swipenode provenance show <id> --json
entire swipenode provenance list --json
```

Fetched-source authority uses a small, reviewed owner policy rather than a
general web-domain heuristic. Each owner entry declares documentation domains
and whether dot-delimited subdomains inherit that authority. Matching requires
HTTPS and exact hostname equality unless subdomains are explicitly enabled;
lookalike names and deceptive suffixes do not match. The policy is structured so
future reviewed Knowledge Packs can add owner/domain rules without changing the
matching algorithm. A domain match establishes source ownership only: evidence
must still pass independent relevance, confidence, and deterministic comparison
checks before a claim can become `VERIFIED` or `CONFLICT`.

Untracked files are not part of `git diff`; the report warns when they exist. Add or stage them and use `--staged` when they should be verified.

### `init-agents`

Adds or updates one clearly delimited SwipeNode section in the repository-root `AGENTS.md`. User content and file permissions are preserved, updates are atomic, duplicate sections are not appended, and malformed markers fail without writing. Use `--dry-run` to preview the resulting file.

## Local development

```sh
go build -o /tmp/entire-swipenode ./cmd/entire-swipenode
/tmp/entire-swipenode --help
entire plugin install /tmp/entire-swipenode
entire swipenode doctor
entire swipenode verify
entire swipenode verify --fetch
entire swipenode verify --record-provenance
entire swipenode provenance list
```

The always-supported local installation path is:

```sh
entire plugin install /path/to/entire-swipenode --force
```

The release-package layout can be reproduced without leaving stale binaries in the repository:

```sh
sh scripts/test-entire-release-package.sh
```

It builds in a temporary directory, checks the plugin-only help surface, verifies that the platform archive contains exactly the expected executable, and validates the generated checksum.

Repository installation is `entire plugin install https://github.com/sirToby99/swipenode` when using a release containing the matching `entire-swipenode_<version>_<os>_<arch>` archive and a covering `checksums.txt`; local executable installation remains the development path.
