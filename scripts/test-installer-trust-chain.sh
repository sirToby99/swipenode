#!/bin/sh
# Exercise the root-authenticated software installer chain with ephemeral keys.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_command=${GO:-go}
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-installer-trust.XXXXXX")
cleanup() { rm -rf "$test_dir"; }
trap cleanup EXIT HUP INT TERM

pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2" >&2; exit 1; }

"$go_command" test "$root/internal/softwaretrust" -run 'TestI0[1-6]' >/dev/null
for case_id in I01 I02 I03 I04 I05 I06; do pass "$case_id"; done

mkdir -p "$test_dir/assets" "$test_dir/archive" "$test_dir/bin" "$test_dir/install"
ssh-keygen -q -t ed25519 -N '' -f "$test_dir/release-key" >/dev/null
"$go_command" run -buildvcs=false "$root/internal/softwaretrust/testfixture" \
  --release-public "$test_dir/release-key.pub" \
  --metadata "$test_dir/software-trust.json" \
  --root "$test_dir/software-root.pub"
"$go_command" build -buildvcs=false -o "$test_dir/archive/swipenode" "$root"
archive=swipenode_2.1.0_linux_amd64.tar.gz
tar -C "$test_dir/archive" -czf "$test_dir/assets/$archive" swipenode
(cd "$test_dir/assets" && sha256sum "$archive" > checksums.txt)
ssh-keygen -q -Y sign -f "$test_dir/release-key" -n swipenode-release "$test_dir/assets/checksums.txt" >/dev/null

install -m 0755 "$root/scripts/testdata/fixture-curl.sh" "$test_dir/bin/curl"
PATH="$test_dir/bin:$PATH" \
SWIPENODE_TEST_RELEASE_ASSETS="$test_dir/assets" \
SWIPENODE_INSTALL_DIR="$test_dir/install" \
SWIPENODE_VERSION=v2.1.0 \
SWIPENODE_TRUST_VERIFIER="$test_dir/archive/swipenode" \
SWIPENODE_SOFTWARE_TRUST_METADATA_FILE="$test_dir/software-trust.json" \
SWIPENODE_SOFTWARE_TRUST_ROOT_FILE="$test_dir/software-root.pub" \
sh "$root/scripts/install.sh" >/dev/null
[ -x "$test_dir/install/swipenode" ] || fail I01 "trust-authenticated installer did not install the binary"

cp "$test_dir/assets/checksums.txt" "$test_dir/tampered-checksums.txt"
printf '\nmalicious change\n' >> "$test_dir/tampered-checksums.txt"
if sh "$root/scripts/verify-release-checksums.sh" "$test_dir/tampered-checksums.txt" "$test_dir/assets/checksums.txt.sig" "$test_dir/release-key.pub" >/dev/null 2>&1; then
  fail I07 "bad checksum signature was accepted"
fi
pass I07

cp "$test_dir/assets/$archive" "$test_dir/original-archive"
printf 'corruption' >> "$test_dir/assets/$archive"
if PATH="$test_dir/bin:$PATH" \
  SWIPENODE_TEST_RELEASE_ASSETS="$test_dir/assets" \
  SWIPENODE_INSTALL_DIR="$test_dir/install-corrupt" \
  SWIPENODE_VERSION=v2.1.0 \
  SWIPENODE_TRUST_VERIFIER="$test_dir/archive/swipenode" \
  SWIPENODE_SOFTWARE_TRUST_METADATA_FILE="$test_dir/software-trust.json" \
  SWIPENODE_SOFTWARE_TRUST_ROOT_FILE="$test_dir/software-root.pub" \
  sh "$root/scripts/install.sh" >/dev/null 2>&1; then
  fail I08 "artifact hash mismatch was accepted"
fi
pass I08
mv "$test_dir/original-archive" "$test_dir/assets/$archive"

sh "$root/scripts/verify-release-checksums.sh" "$test_dir/assets/checksums.txt" "$test_dir/assets/checksums.txt.sig" "$test_dir/release-key.pub" >/dev/null
PATH="$test_dir/bin:$PATH" \
SWIPENODE_TEST_RELEASE_ASSETS="$test_dir/assets" \
SWIPENODE_INSTALL_DIR="$test_dir/install-manual" \
SWIPENODE_VERSION=v2.1.0 \
SWIPENODE_RELEASE_PUBLIC_KEY_FILE="$test_dir/release-key.pub" \
SWIPENODE_ALLOW_DIRECT_RELEASE_KEY=1 \
sh "$root/scripts/install.sh" >/dev/null
[ -x "$test_dir/install-manual/swipenode" ] || fail I09 "explicit offline/manual verification path failed"
pass I09

if rg -n 'PRIVATE KEY|BEGIN OPENSSH PRIVATE KEY' "$test_dir/software-trust.json" "$test_dir/software-root.pub" >/dev/null; then
  fail I10 "public trust outputs contained secret-key material"
fi
if rg -n 'curl.*(key|secret)|SWIPENODE_.*PRIVATE' "$root/scripts/install.sh" >/dev/null; then
  fail I10 "installer contains a secret-key download path"
fi
pass I10
printf 'INSTALLER TRUST CHAIN READY (10/10)\n'
