#!/usr/bin/env sh
# Authenticate and install a published SwipeNode release without executing it first.
set -eu

repo="sirToby99/swipenode"
install_dir="${SWIPENODE_INSTALL_DIR:-${HOME:?HOME must be set}/.local/bin}"
version="${SWIPENODE_VERSION:-}"
release_api="${SWIPENODE_RELEASE_API_URL:-https://api.github.com/repos/$repo/releases/latest}"
managed_trust="${SWIPENODE_MANAGED_TRUST_BASE_URL:-https://knowledge.swipenode.dev/.well-known/swipenode/software}"

fail() { printf '%s\n' "swipenode installer: $*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported operating system: $(uname -s). Linux and macOS are supported." ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m). amd64 and arm64 are supported." ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required to download a verified release."
command -v cmp >/dev/null 2>&1 || fail "cmp is required to compare public Trust channels."
command -v openssl >/dev/null 2>&1 || fail "OpenSSL with Ed25519 support is required to authenticate software Trust metadata."
command -v python3 >/dev/null 2>&1 || fail "Python 3 is required by the source-auditable Trust bootstrap verifier."
command -v ssh-keygen >/dev/null 2>&1 || fail "ssh-keygen with SSHSIG support is required to authenticate a release."
if [ -z "$version" ]; then
  version=$(curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    "$release_api" | sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$version" ] || fail "no published release was found. Set SWIPENODE_VERSION after the first GitHub release exists."
fi
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) fail "SWIPENODE_VERSION must be a semantic tag such as v2.0.1." ;; esac

archive="swipenode_${version#v}_${os}_${arch}.tar.gz"
base_url="${SWIPENODE_RELEASE_BASE_URL:-https://github.com/$repo/releases/download/$version}"
repository_trust="${SWIPENODE_REPOSITORY_TRUST_BASE_URL:-https://raw.githubusercontent.com/$repo/$version/trust/software}"
verifier="${SWIPENODE_TRUST_BOOTSTRAP_VERIFIER:-}"
if [ -z "$verifier" ]; then
  script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
  verifier="$script_dir/verify-software-trust.py"
fi
[ -f "$verifier" ] && [ ! -L "$verifier" ] || fail "the source-auditable software Trust bootstrap verifier is missing."
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-install.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM
umask 077

for name in software-trust-root.pub software-release.json software-release.key.pub; do
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/repository-$name" "$repository_trust/$name"
  curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/managed-$name" "$managed_trust/$name"
  cmp "$tmpdir/repository-$name" "$tmpdir/managed-$name" >/dev/null || fail "public Trust channels disagree for $name."
done

python3 "$verifier" \
  --metadata "$tmpdir/repository-software-release.json" \
  --trust-root "$tmpdir/repository-software-trust-root.pub" \
  --release-key "$tmpdir/repository-software-release.key.pub" \
  --version "$version" \
  --allowed-signers "$tmpdir/allowed_signers" > "$tmpdir/trust-result.json" || fail "software Trust authentication failed."
printf 'Authenticated software Trust metadata and release-key authorization.\n'

curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/checksums.txt" "$base_url/checksums.txt"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/checksums.txt.sig" "$base_url/checksums.txt.sig"

ssh-keygen -Y verify -q -f "$tmpdir/allowed_signers" -I swipenode-release -n swipenode-release -s "$tmpdir/checksums.txt.sig" < "$tmpdir/checksums.txt" || fail "software-release signature verification failed."
printf 'Authenticated signed release checksum manifest.\n'

curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/$archive" "$base_url/$archive"

expected=$(awk -v f="$archive" '$2 == f || $2 == "*" f {print $1; exit}' "$tmpdir/checksums.txt")
[ -n "$expected" ] || fail "checksums.txt has no entry for $archive; refusing installation."
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmpdir/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmpdir/$archive" | awk '{print $1}')
else
  fail "sha256sum or shasum is required for checksum verification."
fi
[ "$actual" = "$expected" ] || fail "checksum verification failed for $archive; refusing installation."
printf 'Verified archive SHA-256 before extraction.\n'

tar -tzf "$tmpdir/$archive" > "$tmpdir/archive-contents"
awk 'BEGIN {ok=1} /^\// {ok=0} /(^|\/)\.\.($|\/)/ {ok=0} END {exit !ok}' "$tmpdir/archive-contents" || fail "archive contains an unsafe path."
tar -xzf "$tmpdir/$archive" -C "$tmpdir" swipenode
[ -f "$tmpdir/swipenode" ] && [ ! -L "$tmpdir/swipenode" ] || fail "archive did not contain the expected regular swipenode binary."
mkdir -p "$install_dir"
install -m 0755 "$tmpdir/swipenode" "$install_dir/swipenode"
printf 'Installed swipenode %s to %s/swipenode\n' "$version" "$install_dir"
case ":${PATH}:" in *":${install_dir}:"*) ;; *) printf 'Add %s to PATH to invoke swipenode directly. No shell configuration was changed.\n' "$install_dir" >&2 ;; esac
