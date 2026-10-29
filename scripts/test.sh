#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
export GOWORK=off
for module in . stores/postgres stores/valkey telemetry; do
  cd "$root/$module"
  go test "$@" ./...
done
