# Install and authenticate SwipeNode

No customer should treat an unsigned release candidate as a trusted release.
Once a signed GitHub Release is published, first installation follows these
separate checks:

1. Obtain the public software Trust Root and signed `software-release.json`
   from the repository `trust/software/` channel and compare it with the
   Managed Knowledge-domain mirror advertised by service discovery.
2. Authenticate the metadata and confirm that its active `content_signing` key
   authorizes the chosen version. A root obtained only beside the binary is not
   an independent trust anchor.
3. Verify `checksums.txt.sig` with the authorized Ed25519 release key (OpenSSH
   SSHSIG namespace `swipenode-release`).
4. Verify the selected archive SHA-256 against the authenticated checksum list.
5. Extract or run `scripts/install.sh`; verify `swipenode --version`.

The installer supports Linux and macOS on amd64 and arm64. Windows amd64/arm64
archives are installed manually after the same signature and hash checks.

For the explicit first-install direct-key path:

```sh
SWIPENODE_VERSION=vX.Y.Z \
SWIPENODE_RELEASE_PUBLIC_KEY_FILE=/secure/software-release.key.pub \
SWIPENODE_ALLOW_DIRECT_RELEASE_KEY=1 \
SWIPENODE_INSTALL_DIR="$HOME/.local/bin" \
sh scripts/install.sh
```

This path is safe only when that public key and its fingerprint were obtained
and compared through an independently trusted channel. Full metadata-based
upgrade and offline verification commands are in `docs/release-security.md`.
