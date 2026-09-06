#!/bin/sh
# Authenticate a SwipeNode software-release checksum manifest using SSHSIG.
set -eu

fail() { printf '%s\n' "verify release checksums: $*" >&2; exit 1; }

[ "$#" -eq 3 ] || fail "usage: $0 <checksums.txt> <signature> <trusted-public-key>"
checksums=$1
signature=$2
public_key_file=$3

command -v ssh-keygen >/dev/null 2>&1 || fail "ssh-keygen with SSHSIG support is required"
for input in "$checksums" "$signature" "$public_key_file"; do
  [ -f "$input" ] && [ ! -L "$input" ] || fail "$input must be a regular non-symlink file"
done

key_count=$(awk 'NF && $1 !~ /^#/ {count++} END {print count+0}' "$public_key_file")
[ "$key_count" -eq 1 ] || fail "trusted public-key file must contain exactly one key"
public_key=$(awk 'NF && $1 !~ /^#/ {print; exit}' "$public_key_file")
case "$public_key" in ssh-ed25519\ *) ;; *) fail "trusted release key must be an Ed25519 public key" ;; esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-release-verify.XXXXXX")
cleanup() { rm -rf "$work_dir"; }
trap cleanup EXIT HUP INT TERM
umask 077
printf 'swipenode-release %s\n' "$public_key" > "$work_dir/allowed_signers"
ssh-keygen -Y verify -q -f "$work_dir/allowed_signers" -I swipenode-release -n swipenode-release -s "$signature" < "$checksums" || fail "release signature verification failed"
printf 'Verified software-release signature for %s\n' "$checksums"
