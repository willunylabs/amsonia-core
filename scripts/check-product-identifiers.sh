#!/usr/bin/env bash
set -euo pipefail

legacy_server="willuny""-go-admin"
legacy_web="willuny""-go-ui"
status=0

if git grep -n -e "$legacy_server" -e "$legacy_web" -- .; then
  echo "retired repository identifiers remain in tracked content" >&2
  status=1
fi

if git ls-files | grep -E "${legacy_server}|${legacy_web}"; then
  echo "retired repository identifiers remain in tracked file names" >&2
  status=1
fi

if [[ "$status" -ne 0 ]]; then
  exit "$status"
fi

echo "Source provenance uses the Amsonia repository names."
