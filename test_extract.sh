#!/usr/bin/env bash
set -euo pipefail

URL="${1:-https://demo.vercel.store/product/acme-t-shirt}"
BINARY="./swipenode"

echo "=== SwipeNode Extract Test ==="
echo "URL: $URL"
echo ""

# Build fresh binary
echo "[1/4] Building swipenode..."
go build -o "$BINARY" .
echo "  OK"

# Run extraction
echo "[2/4] Extracting data..."
STDERR_LOG=$(mktemp)
trap "rm -f $STDERR_LOG" EXIT

if OUTPUT=$("$BINARY" extract --url "$URL" 2>"$STDERR_LOG"); then
    echo "  OK"
else
    EXIT_CODE=$?
    echo "  FAIL (exit code $EXIT_CODE)"
    echo "  stderr: $(cat "$STDERR_LOG")"
    exit 1
fi

# Validate output is non-empty
echo "[3/4] Validating output..."
if [ -z "$OUTPUT" ]; then
    echo "  FAIL: empty output"
    exit 1
fi

CHAR_COUNT=${#OUTPUT}
echo "  Output size: $CHAR_COUNT chars"

# Check if output is valid JSON (Next.js site should return JSON)
echo "[4/4] Checking output format..."
if echo "$OUTPUT" | jq . >/dev/null 2>&1; then
    echo "  Format: JSON (Next.js extraction)"
    echo ""
    echo "--- Extracted JSON (first 2000 chars) ---"
    echo "$OUTPUT" | jq . | head -c 2000
else
    echo "  Format: Plain text (fallback extraction)"
    echo ""
    echo "--- Extracted Text (first 2000 chars) ---"
    echo "$OUTPUT" | head -c 2000
fi

echo ""
echo ""
echo "=== Test Complete ==="
