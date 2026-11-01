#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${PLUTO_CORE_TEST_DATABASE_URL:?required isolated database URL is missing}"
export GOWORK=off PLUTO_CORE_REQUIRE_DATABASE=1
result=$(mktemp)
trap 'rm -f "$result"' EXIT
sql_test() {
  if [[ "${PLUTO_CORE_CANDIDATE_DATA:-0}" == "1" ]]; then
    go test -tags plutocandidate "$@"
  else
    go test "$@"
  fi
}
sql_test -race -count=1 -json ./... -run '^TestPostgres' >"$result" || {
  cat "$result"
  exit 1
}
python3 - "$result" <<'PY'
import json, os, sys
from pathlib import Path
results = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines()]
expected = {'TestPostgresDispatchBinding', 'TestPostgresConcurrentReplay', 'TestPostgresOutboxRollback'}
if os.environ.get('PLUTO_CORE_CANDIDATE_DATA') == '1':
    expected |= {'TestPostgresAuthorizedScopedCache', 'TestPostgresVisiblePaginationAndBoundedExport', 'TestPostgresDeclaredAtomicDispatch', 'TestPostgresTypedDeliveryAfterCommit', 'TestPostgresConcurrentIdempotentDispatch', 'TestPostgresAuthoritativeGuardFacts'}
passed = {r.get('Test') for r in results if r.get('Action') == 'pass'}
skipped = [r.get('Test') for r in results if r.get('Action') == 'skip' and r.get('Test')]
if skipped or not expected <= passed:
    raise SystemExit(f'Required SQL evidence missing: skipped={skipped}, absent={expected-passed}')
print('Required shipment PostgreSQL cases executed: ' + ', '.join(sorted(expected)))
PY
