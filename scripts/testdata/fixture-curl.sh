#!/bin/sh
set -eu

output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      [ "$#" -ge 2 ] || exit 2
      output=$2
      shift 2
      ;;
    *)
      url=$1
      shift
      ;;
  esac
done
[ -n "$output" ] && [ -n "$url" ] && [ -n "${SWIPENODE_TEST_RELEASE_ASSETS:-}" ] || exit 2
name=${url##*/}
cp "$SWIPENODE_TEST_RELEASE_ASSETS/$name" "$output"
