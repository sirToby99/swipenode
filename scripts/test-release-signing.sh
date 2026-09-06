#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-release-signing-test.XXXXXX")
cleanup() { rm -rf "$work_dir"; }
trap cleanup EXIT HUP INT TERM

ssh-keygen -q -t ed25519 -N '' -C swipenode-release-test -f "$work_dir/release-key"
ssh-keygen -q -t ed25519 -N '' -C swipenode-wrong-key -f "$work_dir/wrong-key"
printf '%064d  swipenode_1.2.3_linux_amd64.tar.gz\n' 0 > "$work_dir/checksums.txt"

sh "$root/scripts/sign-release-checksums.sh" "$work_dir/checksums.txt" "$work_dir/release-key" "$work_dir/checksums.txt.sig"
sh "$root/scripts/verify-release-checksums.sh" "$work_dir/checksums.txt" "$work_dir/checksums.txt.sig" "$work_dir/release-key.pub"

cp "$work_dir/checksums.txt" "$work_dir/tampered.txt"
printf '# tampered\n' >> "$work_dir/tampered.txt"
if sh "$root/scripts/verify-release-checksums.sh" "$work_dir/tampered.txt" "$work_dir/checksums.txt.sig" "$work_dir/release-key.pub" >/dev/null 2>&1; then
  echo "tampered checksum manifest was accepted" >&2
  exit 1
fi
if sh "$root/scripts/verify-release-checksums.sh" "$work_dir/checksums.txt" "$work_dir/checksums.txt.sig" "$work_dir/wrong-key.pub" >/dev/null 2>&1; then
  echo "wrong release key was accepted" >&2
  exit 1
fi
chmod 0644 "$work_dir/release-key"
if sh "$root/scripts/sign-release-checksums.sh" "$work_dir/checksums.txt" "$work_dir/release-key" "$work_dir/should-not-exist.sig" >/dev/null 2>&1; then
  echo "insecure private-key permissions were accepted" >&2
  exit 1
fi
[ ! -e "$work_dir/should-not-exist.sig" ]
if grep -Fq "PRIVATE KEY" "$work_dir/checksums.txt.sig"; then
  echo "signature contains private-key material" >&2
  exit 1
fi
printf 'PASS software release SSHSIG signing, tamper rejection, wrong-key rejection, and key handling\n'
