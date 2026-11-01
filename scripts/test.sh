#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
export GOWORK=off
for module in . stores/postgres stores/valkey telemetry examples/shipments/postgres; do
  cd "$root/$module"
  if [[ "${PLUTO_CORE_CANDIDATE_DATA:-0}" == "1" ]]; then
    go test "$@" -tags plutocandidate ./...
  else
    go test "$@" ./...
  fi
done
