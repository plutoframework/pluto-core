#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go run ./scripts/commentspacing -formatter "$PWD/scripts/golangci.sh" .
root="$PWD"
export GOWORK=off

python3 -B -m unittest discover -s scripts -p test_doc_examples.py
python3 -B scripts/doc_examples.py

python3 - <<'PYTHON'
from pathlib import Path

for path in Path(".").rglob("*"):
    if not path.is_file() or ".git" in path.parts:
        continue
    data = path.read_bytes()
    if b"\0" not in data and len(data.splitlines()) > 1000:
        raise SystemExit(f"File exceeds 1,000 lines: {path}")
PYTHON

run_go() {
  local go_action="$1"
  shift
  if [[ "${PLUTO_CORE_CANDIDATE_DATA:-0}" == "1" ]]; then
    go "$go_action" -tags plutocandidate "$@"
  else
    go "$go_action" "$@"
  fi
}

for module in . stores/postgres stores/valkey telemetry \
  examples/shipments/postgres; do
  cd "$root/$module"
  if [[ "${PLUTO_CORE_CANDIDATE_DATA:-0}" == "1" ]]; then
    "$root/scripts/lint.sh" --build-tags plutocandidate ./...
  else
    "$root/scripts/lint.sh" ./...
  fi
  run_go test ./...
  run_go vet ./...
  run_go build ./...
done
