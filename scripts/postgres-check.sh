#!/usr/bin/env bash
# Disposable container, dynamic loopback port, no persistent volume.
set -euo pipefail
cd "$(dirname "$0")/.."
container="pluto-core-check-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker info >/dev/null
docker run -d --rm --name "$container" \
  -e POSTGRES_USER=pluto_demo -e POSTGRES_PASSWORD=local_only -e POSTGRES_DB=pluto_demo \
  -p 127.0.0.1::5432 postgres:17.6-alpine >/dev/null
ready=false
for attempt in $(seq 1 40); do
  if docker exec "$container" pg_isready -U pluto_demo -d pluto_demo >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  docker logs "$container"
  exit 1
fi
port=$(docker port "$container" 5432/tcp | awk -F: '{print $NF}')
result=$(mktemp)
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -f "$result"
}
PLUTO_CORE_TEST_DATABASE_URL="postgres://pluto_demo:local_only@127.0.0.1:${port}/pluto_demo?sslmode=disable" \
  PLUTO_CORE_REQUIRE_DATABASE=1 GOWORK=off \
  sh -c 'cd stores/postgres && go test -race -count=1 -json ./... -run "^TestPostgres"' >"$result" || {
    cat "$result"
    exit 1
  }
python3 - "$result" <<'PYTHON'
import json, sys
from pathlib import Path
results = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines()]
expected = {'TestPostgresConcurrentClaimsAndRetention', 'TestPostgresBusinessCompletionAtomicity'}
passed = {r.get('Test') for r in results if r.get('Action') == 'pass'}
skipped = [r.get('Test') for r in results if r.get('Action') == 'skip' and r.get('Test')]
if skipped or not expected <= passed:
    raise SystemExit(f'Required SQL evidence missing: skipped={skipped}, absent={expected-passed}')
print('Required PostgreSQL adapter cases executed: ' + ', '.join(sorted(expected)))
PYTHON
PLUTO_CORE_TEST_DATABASE_URL="postgres://pluto_demo:local_only@127.0.0.1:${port}/pluto_demo?sslmode=disable" \
  ./examples/shipments/postgres/scripts/database-check.sh

if [[ "${PLUTO_CORE_SQL_MUTATION:-0}" == "1" ]]; then
  PLUTO_CORE_TEST_DATABASE_URL="postgres://pluto_demo:local_only@127.0.0.1:${port}/pluto_demo?sslmode=disable" \
    python3 ./examples/shipments/postgres/scripts/mutation-check.py
fi
