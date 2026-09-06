# Install and authenticate SwipeNode

No customer should treat an unsigned release candidate as a trusted release.
Once a signed GitHub Release is published, use `scripts/install.sh` as the one
recommended installation path. Before it extracts or installs the candidate,
the installer performs these fail-closed checks:

1. Obtain the public software Trust Root and signed `software-release.json`
   from the repository `trust/software/` channel and compare it with the
   Managed Knowledge-domain mirror advertised by service discovery.
2. Authenticate the metadata and confirm that its active `content_signing` key
   authorizes the chosen version. A root obtained only beside the binary is not
   an independent trust anchor.
3. Verify `checksums.txt.sig` with the authorized Ed25519 release key (OpenSSH
   SSHSIG namespace `swipenode-release`).
4. Verify the selected archive SHA-256 against the authenticated checksum list.
5. Only after all four checks pass, extract and install the binary.

The installer supports Linux and macOS on amd64 and arm64. Windows amd64/arm64
archives are installed manually after the same signature and hash checks.

Download the installer and its source-auditable bootstrap verifier from the
public source repository:

```sh
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
  -o install-swipenode.sh \
  https://raw.githubusercontent.com/sirToby99/swipenode/main/scripts/install.sh
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
  -o verify-software-trust.py \
  https://raw.githubusercontent.com/sirToby99/swipenode/main/scripts/verify-software-trust.py
SWIPENODE_TRUST_BOOTSTRAP_VERIFIER="$PWD/verify-software-trust.py" \
  sh install-swipenode.sh
```

The repository and `knowledge.swipenode.dev` are compared publication channels;
that comparison is not equivalent to an independently provisioned Trust Root.
Direct archive downloads remain available for controlled/manual installation,
but must pass the same Trust, signature and hash checks before any extracted
binary is executed. Details are in `docs/release-security.md`.
