#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

echo "Building swipenode..."
cd "$REPO_ROOT"
go build -o swipenode .

if [ "$(id -u)" -eq 0 ]; then
    INSTALL_DIR="/usr/local/bin"
else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
fi

mv swipenode "$INSTALL_DIR/swipenode"
echo "Installed swipenode to $INSTALL_DIR/swipenode"
