#!/bin/sh
# Sign a SwipeNode software-release checksum manifest using OpenSSH SSHSIG.
set -eu

fail() { printf '%s\n' "sign release checksums: $*" >&2; exit 1; }

[ "$#" -eq 3 ] || fail "usage: $0 <checksums.txt> <private-key> <signature-output>"
checksums=$1
private_key=$2
signature_output=$3

command -v ssh-keygen >/dev/null 2>&1 || fail "ssh-keygen with SSHSIG support is required"
[ -f "$checksums" ] && [ ! -L "$checksums" ] || fail "checksum manifest must be a regular non-symlink file"
[ -f "$private_key" ] && [ ! -L "$private_key" ] || fail "private key must be a regular non-symlink file"

if mode=$(stat -c '%a' "$private_key" 2>/dev/null); then
  case "$mode" in *00) ;; *) fail "private key must not be accessible by group or others" ;; esac
elif mode=$(stat -f '%Lp' "$private_key" 2>/dev/null); then
  case "$mode" in *00) ;; *) fail "private key must not be accessible by group or others" ;; esac
else
  fail "could not inspect private-key permissions"
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-release-sign.XXXXXX")
cleanup() { rm -rf "$work_dir"; }
trap cleanup EXIT HUP INT TERM
cp "$checksums" "$work_dir/checksums.txt"
ssh-keygen -Y sign -q -f "$private_key" -n swipenode-release "$work_dir/checksums.txt"
[ -s "$work_dir/checksums.txt.sig" ] || fail "ssh-keygen did not create a signature"
mv "$work_dir/checksums.txt.sig" "$signature_output"
printf 'Signed %s as %s\n' "$checksums" "$signature_output"
