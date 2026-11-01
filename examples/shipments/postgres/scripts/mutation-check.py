#!/usr/bin/env python3
"""Require pristine SQL controls and exact semantic assertions per mutant."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True

from mutation_evidence import killed, pristine

script_dir = Path(__file__).resolve().parent
subprocess.run([sys.executable, '-m', 'unittest', 'discover', '-s', str(script_dir),
                '-p', 'test_mutation_evidence.py'], check=True,
               env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'))
if not os.environ.get('PLUTO_CORE_TEST_DATABASE_URL'):
    raise SystemExit('required isolated database URL is missing')
root = Path(__file__).resolve().parents[4]
consumer = Path('examples/shipments/postgres')
query = consumer / 'internal/db/queries.sql.go'
writer = consumer / 'writer.go'
mutations = [
    (query, 'tenant_id = $2', '(tenant_id = $2 OR tenant_id <> $2)',
     'TestPostgresDispatchBinding/scope', 'got <nil> want shipment snapshot conflict'),
    (query, 'id = $3', '(id = $3 OR id <> $3)',
     'TestPostgresDispatchBinding/entity', 'got <nil> want shipment snapshot conflict'),
    (query, 'status = $4', '(status = $4 OR status <> $4)',
     'TestPostgresDispatchBinding/state', 'got <nil> want shipment snapshot conflict'),
    (query, 'version = $5', '(version = $5 OR version <> $5)',
     'TestPostgresDispatchBinding/version', 'got <nil> want shipment snapshot conflict'),
    (writer, 'db.New(tx)', 'db.New(w.pool)',
     'TestPostgresOutboxRollback', 'status=dispatched version=8 intents=0'),
    (writer, 'return tx.Commit(ctx)', 'return nil',
     'TestPostgresOutboxRollback', 'status=packed version=7 intents=0'),
]
env = dict(os.environ, GOWORK='off', PLUTO_CORE_REQUIRE_DATABASE='1',
           PYTHONDONTWRITEBYTECODE='1')


def run(directory, test):
    try:
        return subprocess.run(['go', 'test', '-json', '-count=1', '-timeout', '30s',
                               '-run', test, '.'], cwd=directory, env=env,
                              capture_output=True, text=True, timeout=60)
    except subprocess.TimeoutExpired:
        raise SystemExit(f'TIMEOUT is not semantic evidence: {test}')


def positive_control(phase):
    control = run(root / consumer, '^TestPostgres')
    try:
        pristine(control)
    except ValueError as error:
        raise SystemExit(f'PRISTINE INVALID: {error}\n{control.stdout}{control.stderr}')
    print(f'PRISTINE SQL control passed {phase} mutations', flush=True)


positive_control('before')

for path, old, new, target_test, assertion in mutations:
    with tempfile.TemporaryDirectory(prefix='pluto-shipment-sql-mutation-') as temp:
        target = Path(temp)
        shutil.copytree(root, target, dirs_exist_ok=True,
                        ignore=shutil.ignore_patterns('.git', 'go.work', 'go.work.sum',
                                                     '.DS_Store', '__pycache__'))
        source = (target / path).read_text()
        if source.count(old) != 1:
            raise SystemExit(f'literal mismatch: {path}: {old}')
        (target / path).write_text(source.replace(old, new, 1))
        test_pattern = '/'.join('^' + part + '$' for part in target_test.split('/'))
        evidence = run(target / consumer, test_pattern)
        try:
            killed(evidence, target_test, assertion)
        except ValueError as error:
            raise SystemExit(f'SURVIVED/INVALID: {path}: {old}: {error}\n'
                             f'{evidence.stdout}{evidence.stderr}')
        print(f'KILLED: {path}: {old}: {target_test}: {assertion}', flush=True)

positive_control('after')
