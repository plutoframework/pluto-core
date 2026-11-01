#!/usr/bin/env python3
"""Real SQL negative evidence; missing DB, build failures and timeouts fail."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile

if not os.environ.get('PLUTO_CORE_TEST_DATABASE_URL'):
    raise SystemExit('required isolated database URL is missing')
root = Path(__file__).resolve().parents[4]
consumer = Path('examples/shipments/postgres')
query = consumer / 'internal/db/queries.sql.go'
writer = consumer / 'writer.go'
mutations = [
    (query, 'tenant_id = $2', '(tenant_id = $2 OR tenant_id <> $2)', '^TestPostgres'),
    (query, 'id = $3', '(id = $3 OR id <> $3)', '^TestPostgres'),
    (query, 'status = $4', '(status = $4 OR status <> $4)', '^TestPostgres'),
    (query, 'version = $5', '(version = $5 OR version <> $5)', '^TestPostgres'),
    (writer, 'db.New(tx)', 'db.New(w.pool)', '^TestPostgresOutboxRollback$'),
    (writer, 'return tx.Commit(ctx)', 'return nil', '^TestPostgresOutboxRollback$'),
]
for path, old, new, test in mutations:
    with tempfile.TemporaryDirectory(prefix='pluto-shipment-sql-mutation-') as temp:
        target = Path(temp)
        shutil.copytree(root, target, dirs_exist_ok=True,
                        ignore=shutil.ignore_patterns('.git', 'go.work', 'go.work.sum', '.DS_Store'))
        source = (target / path).read_text()
        if source.count(old) != 1:
            raise SystemExit(f'literal mismatch: {path}: {old}')
        (target / path).write_text(source.replace(old, new, 1))
        env = dict(os.environ, GOWORK='off', PLUTO_CORE_REQUIRE_DATABASE='1')
        try:
            result = subprocess.run(['go', 'test', '-count=1', '-timeout', '30s', '-run', test, './...'],
                                    cwd=target / consumer, env=env, capture_output=True,
                                    text=True, timeout=60)
        except subprocess.TimeoutExpired:
            raise SystemExit(f'TIMEOUT: {path}: {old}')
        output = result.stdout + result.stderr
        if result.returncode == 0 or '[build failed]' in output or 'test timed out' in output or '--- FAIL:' not in output:
            raise SystemExit(f'SURVIVED/INVALID: {path}: {old}\n{output}')
        print(f'KILLED: {path}: {old}')
