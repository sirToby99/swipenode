#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
  echo "usage: $0 <vMAJOR.MINOR.PATCH> <goos> <goarch> <output-directory>" >&2
  exit 2
fi

version=$1
goos=$2
goarch=$3
output=$4
case "$version" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "release version must be vMAJOR.MINOR.PATCH" >&2; exit 2 ;;
esac
printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || {
  echo "release version must be vMAJOR.MINOR.PATCH" >&2
  exit 2
}
case "$goos/$goarch" in
  linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64|windows/arm64) ;;
  *) echo "unsupported release target $goos/$goarch" >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output=$(mkdir -p "$output" && CDPATH= cd -- "$output" && pwd)
epoch=${SOURCE_DATE_EPOCH:-$(git -C "$root" log -1 --format=%ct)}
commit=${SWIPENODE_COMMIT:-$(git -C "$root" rev-parse HEAD)}
build_date=${SWIPENODE_BUILD_DATE:-$(git -C "$root" log -1 --format=%cI)}
work=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-release.XXXXXX")
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM

mkdir -p "$work/docs/compliance"
cp "$root/LICENSE" "$work/LICENSE"
cp "$root/docs/compliance/THIRD_PARTY_NOTICES.md" "$work/docs/compliance/THIRD_PARTY_NOTICES.md"
cp "$root/docs/compliance/sbom.spdx.json" "$work/docs/compliance/sbom.spdx.json"

extension=
[ "$goos" = windows ] && extension=.exe
ldflags="-s -w -buildid= -X github.com/sirToby99/swipenode/internal/buildinfo.Version=$version -X github.com/sirToby99/swipenode/internal/buildinfo.Commit=$commit -X github.com/sirToby99/swipenode/internal/buildinfo.BuildDate=$build_date"
(
  cd "$root"
  GOOS=$goos GOARCH=$goarch CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$work/swipenode$extension" .
  GOOS=$goos GOARCH=$goarch CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$work/entire-swipenode$extension" ./cmd/entire-swipenode
)
touch -d "@$epoch" "$work/swipenode$extension" "$work/entire-swipenode$extension" "$work/LICENSE" "$work/docs/compliance/THIRD_PARTY_NOTICES.md" "$work/docs/compliance/sbom.spdx.json"

for product in swipenode entire-swipenode; do
  binary="$product$extension"
  name="${product}_${version#v}_${goos}_${goarch}"
  if [ "$goos" = windows ]; then
    artifact="$output/$name.zip"
    [ ! -e "$artifact" ] || { echo "refusing to overwrite $artifact" >&2; exit 1; }
    (cd "$work" && zip -X -q "$artifact" "$binary" LICENSE docs/compliance/THIRD_PARTY_NOTICES.md docs/compliance/sbom.spdx.json)
  else
    artifact="$output/$name.tar.gz"
    [ ! -e "$artifact" ] || { echo "refusing to overwrite $artifact" >&2; exit 1; }
    tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner -C "$work" -cf - "$binary" LICENSE docs/compliance/THIRD_PARTY_NOTICES.md docs/compliance/sbom.spdx.json | gzip -n > "$artifact"
  fi
done
