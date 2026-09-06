#!/bin/sh
set -eu

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-entire-package.XXXXXX")
cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT HUP INT TERM

goos=$(go env GOOS)
goarch=$(go env GOARCH)
version=0.0.0
binary=entire-swipenode
archive="entire-swipenode_${version}_${goos}_${goarch}.tar.gz"
if [ "$goos" = windows ]; then
  binary=entire-swipenode.exe
  archive="entire-swipenode_${version}_${goos}_${goarch}.zip"
fi

ldflags="-s -w -buildid= -X github.com/sirToby99/swipenode/internal/buildinfo.Version=v${version} -X github.com/sirToby99/swipenode/internal/buildinfo.Commit=package-test -X github.com/sirToby99/swipenode/internal/buildinfo.BuildDate=1970-01-01T00:00:00Z"
CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$work_dir/$binary" ./cmd/entire-swipenode
mkdir -p "$work_dir/docs/compliance"
cp LICENSE "$work_dir/LICENSE"
cp docs/compliance/THIRD_PARTY_NOTICES.md "$work_dir/docs/compliance/THIRD_PARTY_NOTICES.md"
cp docs/compliance/sbom.spdx.json "$work_dir/docs/compliance/sbom.spdx.json"

"$work_dir/$binary" --help | grep -q 'Source-backed external technical context for Entire'
if "$work_dir/$binary" --help | grep -Eq '(^|[[:space:]])(serve|extract|mcp|robotics|payment|deploy|deployment)([[:space:]]|$)'; then
  echo "plugin-only binary exposes an unrelated command" >&2
  exit 1
fi

if [ "$goos" = windows ]; then
  (cd "$work_dir" && zip -X -q "$archive" "$binary" LICENSE docs/compliance/THIRD_PARTY_NOTICES.md docs/compliance/sbom.spdx.json)
  archive_contents=$(unzip -Z1 "$work_dir/$archive")
else
  tar -C "$work_dir" -czf "$work_dir/$archive" "$binary" LICENSE docs/compliance/THIRD_PARTY_NOTICES.md docs/compliance/sbom.spdx.json
  archive_contents=$(tar -tzf "$work_dir/$archive")
fi
printf '%s\n' "$archive_contents" | grep -Fx "$binary"
printf '%s\n' "$archive_contents" | grep -Fx LICENSE
printf '%s\n' "$archive_contents" | grep -Fx docs/compliance/THIRD_PARTY_NOTICES.md
printf '%s\n' "$archive_contents" | grep -Fx docs/compliance/sbom.spdx.json

(cd "$work_dir" && sha256sum "$archive" > checksums.txt && sha256sum -c checksums.txt)
mkdir "$work_dir/extracted"
if [ "$goos" = windows ]; then
  unzip -q "$work_dir/$archive" -d "$work_dir/extracted"
else
  tar -xzf "$work_dir/$archive" -C "$work_dir/extracted"
fi
"$work_dir/extracted/$binary" --help | grep -q 'Source-backed external technical context for Entire'
printf 'PASS %s contains %s and required compliance artifacts\n' "$archive" "$binary"
