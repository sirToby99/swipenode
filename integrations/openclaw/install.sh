#!/bin/bash
set -e

echo "Installing SwipeNode from the official open-source GitHub repository..."
echo "Source: https://github.com/sirToby99/swipenode"

# Install directly via Go (this proves provenance and pulls the public source code)
go install github.com/sirToby99/swipenode@latest

echo "✅ SwipeNode successfully installed to your Go bin directory (usually ~/go/bin)."
echo "Make sure ~/go/bin is in your system PATH!"
