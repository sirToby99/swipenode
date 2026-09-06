#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-software-pipeline.XXXXXX")
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM

export SOURCE_DATE_EPOCH=0 SWIPENODE_COMMIT=0000000000000000000000000000000000000000 SWIPENODE_BUILD_DATE=1970-01-01T00:00:00Z
sh "$root/scripts/build-software-release.sh" v0.0.0 linux amd64 "$work/first"
sh "$root/scripts/build-software-release.sh" v0.0.0 linux amd64 "$work/second"
(cd "$work/first" && sha256sum *) > "$work/first.sha"
(cd "$work/second" && sha256sum *) > "$work/second.sha"
cmp "$work/first.sha" "$work/second.sha"
sh "$root/scripts/inspect-software-release.sh" "$work/first"/*

ssh-keygen -q -t ed25519 -N '' -C swipenode-ci-test -f "$work/software-key"
(cd "$work/first" && sha256sum *) > "$work/checksums.txt"
sh "$root/scripts/sign-release-checksums.sh" "$work/checksums.txt" "$work/software-key" "$work/checksums.txt.sig"
sh "$root/scripts/verify-release-checksums.sh" "$work/checksums.txt" "$work/checksums.txt.sig" "$work/software-key.pub"

cp "$root/docs/compliance/dependency-licenses.json" "$work/invalid-inventory.json"
jq '.dependencies[0].risk = "BLOCKING"' "$work/invalid-inventory.json" > "$work/invalid.tmp"
mv "$work/invalid.tmp" "$work/invalid-inventory.json"
if (cd "$root" && go run ./tools/compliance -inventory "$work/invalid-inventory.json" -sbom "$work/invalid.spdx.json" -notices "$work/invalid-notices.md") >/dev/null 2>&1; then
  echo "strict commercial compliance accepted a blocking dependency" >&2
  exit 1
fi
printf 'PASS deterministic software archives, inspection, signature, and fail-closed compliance\n'
