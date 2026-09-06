#!/bin/sh
set -eu

[ "$#" -gt 0 ] || { echo "usage: $0 <release-archive>..." >&2; exit 2; }
for artifact in "$@"; do
  base=$(basename "$artifact")
  case "$base" in
    swipenode_*.zip) binary=swipenode.exe; contents=$(unzip -Z1 "$artifact") ;;
    entire-swipenode_*.zip) binary=entire-swipenode.exe; contents=$(unzip -Z1 "$artifact") ;;
    swipenode_*.tar.gz) binary=swipenode; contents=$(tar -tzf "$artifact") ;;
    entire-swipenode_*.tar.gz) binary=entire-swipenode; contents=$(tar -tzf "$artifact") ;;
    *) echo "unexpected release artifact name: $base" >&2; exit 1 ;;
  esac
  actual=$(printf '%s\n' "$contents" | LC_ALL=C sort)
  expected=$(printf '%s\n' "$binary" LICENSE docs/compliance/THIRD_PARTY_NOTICES.md docs/compliance/sbom.spdx.json | LC_ALL=C sort)
  [ "$actual" = "$expected" ] || {
    echo "release archive has unexpected contents: $base" >&2
    printf '%s\n' "$actual" >&2
    exit 1
  }
done
printf 'PASS inspected %d software release archive(s)\n' "$#"
