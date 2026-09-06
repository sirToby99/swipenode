#!/bin/sh
# Exercise first-install authentication without ever executing the candidate binary.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_command=${GO:-go}
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-installer-trust.XXXXXX")
cleanup() { rm -rf "$test_dir"; }
trap cleanup EXIT HUP INT TERM

pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2" >&2; exit 1; }
run_installer() {
  PATH="$test_dir/bin:$PATH" \
  SWIPENODE_TEST_RELEASE_ASSETS="$test_dir/assets" \
  SWIPENODE_TEST_REPOSITORY_TRUST_ASSETS="${TEST_REPOSITORY_TRUST_ASSETS:-$test_dir/repository-trust}" \
  SWIPENODE_TEST_MANAGED_TRUST_ASSETS="${TEST_MANAGED_TRUST_ASSETS:-$test_dir/managed-trust}" \
  SWIPENODE_TEST_CURL_LOG="$test_dir/curl.log" \
  SWIPENODE_INSTALL_DIR="${TEST_INSTALL_DIR:-$test_dir/install}" \
  SWIPENODE_VERSION=v2.1.0 \
  SWIPENODE_RELEASE_BASE_URL=https://fixture.invalid/release \
  SWIPENODE_REPOSITORY_TRUST_BASE_URL=https://fixture.invalid/repository \
  SWIPENODE_MANAGED_TRUST_BASE_URL=https://fixture.invalid/managed \
  SWIPENODE_TRUST_BOOTSTRAP_VERIFIER="$root/scripts/verify-software-trust.py" \
  sh "$root/scripts/install.sh"
}

"$go_command" test "$root/internal/softwaretrust" -run 'TestI0[1-6]' >/dev/null
for case_id in I01 I02 I03 I04 I05 I06; do pass "$case_id"; done

mkdir -p "$test_dir/assets" "$test_dir/repository-trust" "$test_dir/managed-trust" "$test_dir/archive" "$test_dir/bin" "$test_dir/install"
ssh-keygen -q -t ed25519 -N '' -f "$test_dir/release-key" >/dev/null
"$go_command" run -buildvcs=false "$root/internal/softwaretrust/testfixture" \
  --release-public "$test_dir/release-key.pub" \
  --metadata "$test_dir/repository-trust/software-release.json" \
  --root "$test_dir/repository-trust/software-trust-root.pub"
cp "$test_dir/release-key.pub" "$test_dir/repository-trust/software-release.key.pub"
cp "$test_dir/repository-trust/"* "$test_dir/managed-trust/"

cat > "$test_dir/archive/swipenode" <<'EOF'
#!/bin/sh
printf 'candidate binary was executed\n' >> "${SWIPENODE_TEST_BINARY_EXECUTION_LOG:?}"
exit 99
EOF
chmod 0755 "$test_dir/archive/swipenode"
archive=swipenode_2.1.0_linux_amd64.tar.gz
tar -C "$test_dir/archive" -czf "$test_dir/assets/$archive" swipenode
(cd "$test_dir/assets" && sha256sum "$archive" > checksums.txt)
ssh-keygen -q -Y sign -f "$test_dir/release-key" -n swipenode-release "$test_dir/assets/checksums.txt" >/dev/null
install -m 0755 "$root/scripts/testdata/fixture-curl.sh" "$test_dir/bin/curl"

SWIPENODE_TEST_BINARY_EXECUTION_LOG="$test_dir/executed" run_installer >/dev/null
[ -x "$test_dir/install/swipenode" ] || fail I11 "trust-authenticated installer did not install the binary"
[ ! -e "$test_dir/executed" ] || fail I11 "candidate binary ran before installation completed"
expected_order='software-trust-root.pub
software-trust-root.pub
software-release.json
software-release.json
software-release.key.pub
software-release.key.pub
checksums.txt
checksums.txt.sig
swipenode_2.1.0_linux_amd64.tar.gz'
actual_order=$(sed 's|.*/||' "$test_dir/curl.log")
[ "$actual_order" = "$expected_order" ] || fail I11 "installer download/authentication order changed"
pass I11

cp "$test_dir/assets/checksums.txt" "$test_dir/tampered-checksums.txt"
printf '\nmalicious change\n' >> "$test_dir/tampered-checksums.txt"
if sh "$root/scripts/verify-release-checksums.sh" "$test_dir/tampered-checksums.txt" "$test_dir/assets/checksums.txt.sig" "$test_dir/release-key.pub" >/dev/null 2>&1; then
  fail I07 "bad checksum signature was accepted"
fi
pass I07

cp "$test_dir/assets/checksums.txt" "$test_dir/original-checksums.txt"
cp "$test_dir/tampered-checksums.txt" "$test_dir/assets/checksums.txt"
if TEST_INSTALL_DIR="$test_dir/install-bad-signature" run_installer >/dev/null 2>&1; then
  fail I14 "installer accepted a checksum manifest with an invalid signature"
fi
[ ! -e "$test_dir/executed" ] || fail I14 "candidate binary ran after a failed signature gate"
mv "$test_dir/original-checksums.txt" "$test_dir/assets/checksums.txt"
pass I14

cp "$test_dir/assets/$archive" "$test_dir/original-archive"
printf 'corruption' >> "$test_dir/assets/$archive"
if TEST_INSTALL_DIR="$test_dir/install-corrupt" run_installer >/dev/null 2>&1; then
  fail I08 "artifact hash mismatch was accepted"
fi
pass I08
mv "$test_dir/original-archive" "$test_dir/assets/$archive"

mkdir "$test_dir/disagree-trust"
cp "$test_dir/managed-trust/"* "$test_dir/disagree-trust/"
printf '\n' >> "$test_dir/disagree-trust/software-release.json"
if TEST_MANAGED_TRUST_ASSETS="$test_dir/disagree-trust" TEST_INSTALL_DIR="$test_dir/install-disagree" run_installer >/dev/null 2>&1; then
  fail I09 "disagreeing public Trust channels were accepted"
fi
pass I09

mkdir "$test_dir/tampered-trust"
cp "$test_dir/repository-trust/"* "$test_dir/tampered-trust/"
python3 -c 'import json,sys; p=sys.argv[1]; d=json.load(open(p)); d["sequence"]+=1; open(p,"w").write(json.dumps(d,indent=2)+"\n")' "$test_dir/tampered-trust/software-release.json"
if TEST_REPOSITORY_TRUST_ASSETS="$test_dir/tampered-trust" TEST_MANAGED_TRUST_ASSETS="$test_dir/tampered-trust" TEST_INSTALL_DIR="$test_dir/install-tampered" run_installer >/dev/null 2>&1; then
  fail I10 "tampered signed Trust metadata was accepted"
fi
pass I10

sh "$root/scripts/verify-release-checksums.sh" "$test_dir/assets/checksums.txt" "$test_dir/assets/checksums.txt.sig" "$test_dir/release-key.pub" >/dev/null
pass I12

if rg -n 'PRIVATE KEY|BEGIN OPENSSH PRIVATE KEY' "$test_dir/repository-trust/software-release.json" "$test_dir/repository-trust/software-trust-root.pub" >/dev/null; then
  fail I13 "public trust outputs contained secret-key material"
fi
if rg -n 'curl.*(key|secret)|SWIPENODE_.*PRIVATE' "$root/scripts/install.sh" >/dev/null; then
  fail I13 "installer contains a secret-key download path"
fi
pass I13
printf 'INSTALLER TRUST CHAIN READY (14/14)\n'
