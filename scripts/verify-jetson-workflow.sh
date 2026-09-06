#!/bin/sh
set -eu

cli=${SWIPENODE_BIN:-swipenode}
trust_dir=${SWIPENODE_TRUST_DIR:?SWIPENODE_TRUST_DIR must name the authenticated public Trust directory}
remote=${SWIPENODE_MANAGED_ORIGIN:-https://knowledge.swipenode.dev}
version=${SWIPENODE_PACK_VERSION:-2026.09.2}
workspace=${1:?usage: scripts/verify-jetson-workflow.sh NEW-WORKSPACE}

case "$workspace" in /*) ;; *) workspace="$(pwd)/$workspace" ;; esac
test ! -e "$workspace" || { echo "workspace already exists: $workspace" >&2; exit 1; }
test -x "$cli" || command -v "$cli" >/dev/null 2>&1
for file in knowledge/knowledge-trust-root.pub knowledge/knowledge-pack.json knowledge/knowledge-publisher.pub; do
  test -f "$trust_dir/$file" && test ! -L "$trust_dir/$file" || { echo "missing safe Trust file: $file" >&2; exit 1; }
done

mkdir -m 700 "$workspace"
cd "$workspace"
git init -q
git config user.name "SwipeNode clean-room"
git config user.email "clean-room@example.invalid"
: > .gitkeep
git add .gitkeep
git commit -qm baseline

"$cli" trust verify --metadata "$trust_dir/knowledge/knowledge-pack.json" \
  --bootstrap-key "$trust_dir/knowledge/knowledge-trust-root.pub" --purpose knowledge_pack > trust-verification.json
"$cli" trust bootstrap --metadata "$trust_dir/knowledge/knowledge-pack.json" \
  --bootstrap-key "$trust_dir/knowledge/knowledge-trust-root.pub"
"$cli" knowledge remote add --id managed --url "$remote" --publisher SwipeNode \
  --trusted-key "$trust_dir/knowledge/knowledge-publisher.pub"
"$cli" knowledge update-check nvidia-jetson --remote managed --json > update-check.json
"$cli" knowledge pull nvidia-jetson --remote managed --json > pack-pull.json
"$cli" knowledge installed nvidia-jetson --json > pack-installed.json
"$cli" knowledge activate "nvidia-jetson@$version"

printf '%s\n' 'JetPack 6.2.1 supports Jetson Linux 36.4.4 according to https://docs.nvidia.com/jetson/jetpack/6.2.1/release-notes/index.html' > engineering-claim.md
git add engineering-claim.md
"$cli" verify --staged --knowledge-pack nvidia-jetson --fetch --record-provenance --json > verification-report.json
"$cli" verification list --json > verification-history.json
"$cli" evidence list --json > evidence.json
"$cli" audit list --json > audit.json
"$cli" provenance list --json > provenance.json

jq -e '.schema_version == "swipenode.verify.v1" and (.claims | length) > 0 and all(.claims[]; .verification_status == "VERIFIED")' verification-report.json >/dev/null
jq -e '.schema_version == "swipenode.control-plane-verifications.v1" and (.records | length) > 0' verification-history.json >/dev/null
jq -e '.schema_version == "swipenode.control-plane-evidence.v1" and (.evidence | length) > 0' evidence.json >/dev/null
jq -e '.schema_version == "swipenode.audit-list.v1" and (.events | length) > 0' audit.json >/dev/null
jq -e '.schema_version == "swipenode.provenance-list.v1" and (.records | length) > 0' provenance.json >/dev/null

printf 'SwipeNode customer workflow completed in %s\n' "$workspace"
