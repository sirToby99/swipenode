# Reproducible JetPack relationship verification

This workflow uses the customer CLI and writes state only beneath the current
Git repository's Git metadata directory. It requires a published signed CLI
release and publicly comparable Trust files.

## Bootstrap Knowledge Trust

Download and compare these public files from both approved channels before use:

- `trust/knowledge/knowledge-trust-root.pub`
- `trust/knowledge/knowledge-pack.json`
- `trust/knowledge/knowledge-publisher.pub`
- `trust/fingerprints.sha256`

Authenticate the publisher authorization metadata, then initialize the local
Trust store:

```sh
swipenode trust verify --metadata trust/knowledge/knowledge-pack.json \
  --bootstrap-key trust/knowledge/knowledge-trust-root.pub --purpose knowledge_pack
swipenode trust bootstrap --metadata trust/knowledge/knowledge-pack.json \
  --bootstrap-key trust/knowledge/knowledge-trust-root.pub
```

Use `swipenode trust --help` for the exact installed-version flag contract.

## Pull and activate the signed Pack

```sh
mkdir verification-workspace && cd verification-workspace
git init
git config user.name clean-room
git config user.email clean-room@example.invalid
touch .gitkeep && git add .gitkeep && git commit -m baseline

swipenode knowledge remote add --id managed \
  --url https://knowledge.swipenode.dev \
  --publisher SwipeNode \
  --trusted-key ../trust/knowledge/knowledge-publisher.pub
swipenode knowledge update-check nvidia-jetson --remote managed
swipenode knowledge pull nvidia-jetson --remote managed
swipenode knowledge installed nvidia-jetson --json
swipenode knowledge activate nvidia-jetson@2026.09.2
```

`pull` checks the registry hash and Pack signature, then stages an immutable
copy. It does not activate it. Activation re-verifies the installed artifact.

## Create the Claim through an ordinary Git diff

SwipeNode verifies technical statements found in Git diffs; there is no hidden
Claim-creation API. Create and stage this source-backed Claim:

```sh
cat > engineering-claim.md <<'CLAIM'
JetPack 6.2.1 supports Jetson Linux 36.4.4 according to https://docs.nvidia.com/jetson/jetpack/6.2.1/release-notes/index.html
CLAIM
git add engineering-claim.md
```

Run the explicit network fetch and persist provenance:

```sh
swipenode verify --staged --knowledge-pack nvidia-jetson \
  --fetch --record-provenance --json > verification-report.json
swipenode verification list --json
swipenode evidence list --json
swipenode audit list --json
swipenode provenance list --json
```

The fetched authoritative source becomes versioned Evidence; the comparison
persists a `swipenode.verification-record.v1` record. A successful run should be
reported exactly as observed, including retrieval warnings or `UNVERIFIED`/
`CONFLICT` outcomes. Explicit `knowledge refresh --fetch --revalidate` is
required for later source-change checks; SwipeNode does not claim real-time or
background monitoring.
