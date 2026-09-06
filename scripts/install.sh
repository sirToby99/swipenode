#!/usr/bin/env sh
# Install a published SwipeNode release after verifying its SHA-256 checksum.
set -eu

repo="sirToby99/swipenode"
install_dir="${SWIPENODE_INSTALL_DIR:-${HOME:?HOME must be set}/.local/bin}"
version="${SWIPENODE_VERSION:-}"

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
command -v ssh-keygen >/dev/null 2>&1 || fail "ssh-keygen with SSHSIG support is required to authenticate a release."
if [ -z "$version" ]; then
  version=$(curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
    "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$version" ] || fail "no published release was found. Set SWIPENODE_VERSION after the first GitHub release exists."
fi
case "$version" in v[0-9]*.[0-9]*.[0-9]*) ;; *) fail "SWIPENODE_VERSION must be a semantic tag such as v2.0.1." ;; esac

archive="swipenode_${version#v}_${os}_${arch}.tar.gz"
base_url="https://github.com/$repo/releases/download/$version"
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/swipenode-install.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM
umask 077

trust_metadata_file=${SWIPENODE_SOFTWARE_TRUST_METADATA_FILE:-}
trust_root_file=${SWIPENODE_SOFTWARE_TRUST_ROOT_FILE:-}
if [ -n "$trust_metadata_file" ] || [ -n "$trust_root_file" ]; then
  [ -n "$trust_metadata_file" ] && [ -n "$trust_root_file" ] || fail "both SWIPENODE_SOFTWARE_TRUST_METADATA_FILE and SWIPENODE_SOFTWARE_TRUST_ROOT_FILE are required."
  [ -f "$trust_metadata_file" ] && [ ! -L "$trust_metadata_file" ] || fail "software trust metadata must be a regular non-symlink file."
  [ -f "$trust_root_file" ] && [ ! -L "$trust_root_file" ] || fail "software trust root must be a regular non-symlink file."
  trust_verifier=${SWIPENODE_TRUST_VERIFIER:-$install_dir/swipenode}
  [ -f "$trust_verifier" ] && [ ! -L "$trust_verifier" ] && [ -x "$trust_verifier" ] || fail "SWIPENODE_TRUST_VERIFIER must name an independently trusted executable SwipeNode verifier."
  allow_downgrade=${SWIPENODE_ALLOW_DOWNGRADE:-0}
  trust_time=${SWIPENODE_TRUST_AUTHORIZATION_TIME:-}
  if [ -n "$trust_time" ] && [ "${SWIPENODE_ALLOW_TRUST_TIME_OVERRIDE:-0}" != "1" ]; then
    fail "SWIPENODE_TRUST_AUTHORIZATION_TIME requires the explicit SWIPENODE_ALLOW_TRUST_TIME_OVERRIDE=1 recovery override."
  fi
  case "$allow_downgrade:${SWIPENODE_ALLOW_TRUST_TIME_OVERRIDE:-0}" in
    0:0) "$trust_verifier" trust release-keys --metadata "$trust_metadata_file" --trust-root "$trust_root_file" --version "$version" --output "$tmpdir/allowed_signers" >/dev/null ;;
    1:0) "$trust_verifier" trust release-keys --metadata "$trust_metadata_file" --trust-root "$trust_root_file" --version "$version" --output "$tmpdir/allowed_signers" --allow-downgrade >/dev/null ;;
    0:1) [ -n "$trust_time" ] || fail "trust-time override enabled without SWIPENODE_TRUST_AUTHORIZATION_TIME."; "$trust_verifier" trust release-keys --metadata "$trust_metadata_file" --trust-root "$trust_root_file" --version "$version" --output "$tmpdir/allowed_signers" --authorization-time "$trust_time" >/dev/null ;;
    1:1) [ -n "$trust_time" ] || fail "trust-time override enabled without SWIPENODE_TRUST_AUTHORIZATION_TIME."; "$trust_verifier" trust release-keys --metadata "$trust_metadata_file" --trust-root "$trust_root_file" --version "$version" --output "$tmpdir/allowed_signers" --authorization-time "$trust_time" --allow-downgrade >/dev/null ;;
    *) fail "trust overrides must be unset, 0, or 1." ;;
  esac
else
  release_public_key_file=${SWIPENODE_RELEASE_PUBLIC_KEY_FILE:-}
  [ -n "$release_public_key_file" ] || fail "configure signed software trust metadata, or explicitly select the manual direct-key verification path."
  [ "${SWIPENODE_ALLOW_DIRECT_RELEASE_KEY:-0}" = "1" ] || fail "direct release-key verification requires SWIPENODE_ALLOW_DIRECT_RELEASE_KEY=1."
  [ -f "$release_public_key_file" ] && [ ! -L "$release_public_key_file" ] || fail "trusted software-release public key must be a regular non-symlink file."
  key_count=$(awk 'NF && $1 !~ /^#/ {count++} END {print count+0}' "$release_public_key_file")
  [ "$key_count" -eq 1 ] || fail "trusted public-key file must contain exactly one key."
  release_public_key=$(awk 'NF && $1 !~ /^#/ {print; exit}' "$release_public_key_file")
  case "$release_public_key" in ssh-ed25519\ *) ;; *) fail "trusted release key must be an Ed25519 public key." ;; esac
  printf 'swipenode-release %s\n' "$release_public_key" > "$tmpdir/allowed_signers"
fi

curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/checksums.txt" "$base_url/checksums.txt"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$tmpdir/checksums.txt.sig" "$base_url/checksums.txt.sig"

ssh-keygen -Y verify -q -f "$tmpdir/allowed_signers" -I swipenode-release -n swipenode-release -s "$tmpdir/checksums.txt.sig" < "$tmpdir/checksums.txt" || fail "software-release signature verification failed."

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

tar -xzf "$tmpdir/$archive" -C "$tmpdir"
[ -f "$tmpdir/swipenode" ] || fail "archive did not contain the expected swipenode binary."
mkdir -p "$install_dir"
install -m 0755 "$tmpdir/swipenode" "$install_dir/swipenode"
printf 'Installed swipenode %s to %s/swipenode\n' "$version" "$install_dir"
case ":${PATH}:" in *":${install_dir}:"*) ;; *) printf 'Add %s to PATH to invoke swipenode directly. No shell configuration was changed.\n' "$install_dir" >&2 ;; esac
