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
source_dir=$SWIPENODE_TEST_RELEASE_ASSETS
case "$url" in
  */repository/*) source_dir=${SWIPENODE_TEST_REPOSITORY_TRUST_ASSETS:-$source_dir} ;;
  */managed/*) source_dir=${SWIPENODE_TEST_MANAGED_TRUST_ASSETS:-$source_dir} ;;
esac
if [ -n "${SWIPENODE_TEST_CURL_LOG:-}" ]; then
  printf '%s\n' "$url" >> "$SWIPENODE_TEST_CURL_LOG"
fi
cp "$source_dir/$name" "$output"
