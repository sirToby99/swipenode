#!/bin/sh
set -eu

required=go1.25.13
actual=$(go env GOVERSION)
module=$(go list -m -f '{{.GoVersion}}')

[ "$actual" = "$required" ] || {
  echo "SwipeNode requires $required for CI and release; found $actual" >&2
  exit 1
}
[ "$module" = "${required#go}" ] || {
  echo "go.mod must require ${required#go}; found $module" >&2
  exit 1
}
[ "${GOTOOLCHAIN:-}" = local ] || {
  echo "GOTOOLCHAIN=local is required so CI cannot silently substitute a toolchain" >&2
  exit 1
}
