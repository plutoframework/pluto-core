#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
for module in . stores/postgres stores/valkey; do
  cd "$root/$module"
  go test "$@" ./...
done
