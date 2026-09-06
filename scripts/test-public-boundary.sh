#!/bin/sh
set -eu

for forbidden in \
  internal/managedknowledge internal/knowledgeadmin internal/managedbackup \
  internal/managedbackupcmd deploy cmd/swipenode/knowledge_admin.go \
  cmd/swipenode/managed_backup.go cmd/swipenode/publisher.go; do
  test ! -e "$forbidden" || { echo "private boundary violation: $forbidden" >&2; exit 1; }
done

test -f PUBLIC_EXPORT_PROVENANCE.json
test "$(jq -r .schema_version PUBLIC_EXPORT_PROVENANCE.json)" = swipenode.public-export-provenance.v1
test "$(jq -r .product_source_sha PUBLIC_EXPORT_PROVENANCE.json)" = 3a38dafb97a163e63b6e8fe5fce452309d5b8ea4
test -z "$(find . -type l -print -quit)"

private_pattern='/opt/'"swipenode"'|/var/lib/'"swipenode"'|swipenode-'"admin"'\.service|swipenode-'"managed"'\.service|cloudflare'"d"'|Cloudflare Tun'"nel"'|127\.0\.0\.1:808[13]'
if rg -n --hidden --glob '!.git/**' --glob '!scripts/test-public-boundary.sh' "$private_pattern" .; then
  echo "private infrastructure marker found" >&2
  exit 1
fi

go list -deps ./... | rg 'internal/(managedknowledge|knowledgeadmin|managedbackup|managedbackupcmd)$' && {
  echo "private package dependency found" >&2
  exit 1
}

echo "public boundary: PASS"
