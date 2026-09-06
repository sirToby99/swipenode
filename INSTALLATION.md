# Verified installation

The canonical customer installation path is `scripts/install.sh` together with
the source-auditable `scripts/verify-software-trust.py` bootstrap verifier.
Download both from the public repository and invoke only that installer:

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

Before extraction, this flow compares public Trust files from the repository
and Managed Knowledge channels, authenticates root-signed software metadata,
checks release-key authorization, verifies `checksums.txt.sig`, and matches the
selected archive SHA-256. Any missing, disagreeing or invalid check exits
nonzero. The downloaded SwipeNode binary is never invoked during verification.

Linux and macOS amd64/arm64 are supported by the installer. Windows amd64/arm64
archives require controlled manual installation after the same verification
sequence. See [the detailed installation contract](docs/INSTALL.md) and
[software release security](docs/release-security.md).
