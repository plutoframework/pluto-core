#!/usr/bin/env python3
"""Require pristine SQL controls and exact semantic assertions per mutant."""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.dont_write_bytecode = True

from mutation_dependencies import prepare
from mutation_evidence import killed, pristine

script_dir = Path(__file__).resolve().parent
subprocess.run(
    [
        sys.executable,
        "-m",
        "unittest",
        "discover",
        "-s",
        str(script_dir),
        "-p",
        "test_mutation_*.py",
    ],
    check=True,
    env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1"),
)
if not os.environ.get("PLUTO_CORE_TEST_DATABASE_URL"):
    raise SystemExit("required isolated database URL is missing")
root = Path(__file__).resolve().parents[4]
consumer = Path("examples/shipments/postgres")
query = consumer / "internal/db/queries.sql.go"
writer = consumer / "writer.go"
mutations = [
    (
        query,
        "WHERE tenant_id = $2 AND id = $3",
        "WHERE (tenant_id = $2 OR tenant_id <> $2) AND id = $3",
        "TestPostgresDispatchBinding/scope",
        "got <nil> want shipment snapshot conflict",
    ),
    (
        query,
        "WHERE tenant_id = $2 AND id = $3",
        "WHERE tenant_id = $2 AND (id = $3 OR id <> $3)",
        "TestPostgresDispatchBinding/entity",
        "got <nil> want shipment snapshot conflict",
    ),
    (
        query,
        "AND status = $4\n  AND version = $5",
        "AND (status = $4 OR status <> $4)\n  AND version = $5",
        "TestPostgresDispatchBinding/state",
        "got <nil> want shipment snapshot conflict",
    ),
    (
        query,
        "AND status = $4\n  AND version = $5",
        "AND status = $4\n  AND (version = $5 OR version <> $5)",
        "TestPostgresDispatchBinding/version",
        "got <nil> want shipment snapshot conflict",
    ),
    (
        writer,
        "db.New(tx)",
        "db.New(w.pool)",
        "TestPostgresOutboxRollback",
        "status=dispatched version=8 intents=0",
    ),
    (
        writer,
        "return tx.Commit(ctx)",
        "return nil",
        "TestPostgresOutboxRollback",
        "status=packed version=7 intents=0",
    ),
]
if os.environ.get("PLUTO_CORE_CANDIDATE_DATA") == "1":
    mutations += [
        (
            consumer / "reads.go",
            "SubjectID:   execution.SubjectID(),",
            'SubjectID:   "",',
            "TestPostgresAuthorizedScopedCache",
            "actor cache key collision",
            "data_test.go",
        ),
        (
            consumer / "reads.go",
            (
                "if err := d.requireVisible(ctx, execution, id); "
                "err != nil {\n\t\t\t\treturn err\n\t\t\t}"
            ),
            "",
            "TestPostgresAuthorizedScopedCache",
            "revoked row visibility hit cache: <nil>",
            "data_test.go",
        ),
        (
            consumer / "data.go",
            "idempostgres.New(tx)",
            "idempostgres.New(d.pool)",
            "TestPostgresDeclaredAtomicDispatch/commit",
            "partial transaction committed jobs=0 completed=1",
            "data_test.go",
        ),
        (
            consumer / "data.go",
            "delivery.EnqueueTx(ctx, tx, attempt.job)",
            "delivery.Enqueue(ctx, attempt.job)",
            "TestPostgresDeclaredAtomicDispatch/completion",
            "partial transaction committed jobs=1 completed=0",
            "data_test.go",
        ),
        (
            consumer / "data.go",
            "db.New(tx)",
            "db.New(d.pool)",
            "TestPostgresDeclaredAtomicDispatch/audit",
            "status=dispatched version=8 intents=0",
            "data_test.go",
        ),
        (
            query,
            "SELECT s.id, s.status, s.version FROM shipments s\nJOIN",
            (
                "SELECT s.id, s.status, s.version FROM (SELECT * "
                "FROM shipments WHERE tenant_id=$1 AND id>$3 "
                "ORDER BY id ASC LIMIT $4) s\nJOIN"
            ),
            "TestPostgresVisiblePaginationAndBoundedExport",
            "SQL_VISIBILITY_BEFORE_LIMIT",
            "data_test.go",
        ),
        (
            consumer / "reads.go",
            "execution.TenantID(),\n\t\tid,",
            "execution.TenantID(),\n\t\texecution.ResourceID(),",
            "TestPostgresAuthorizedScopedCache",
            "collection execution authorized entity read",
            "data_test.go",
        ),
        (
            consumer / "reads.go",
            "d.load(ctx, execution, id, false)",
            "d.load(ctx, execution, id, true)",
            "TestPostgresAuthoritativeGuardFacts",
            "CACHED_GUARD_FACTS_AUTHORIZED_TRANSITION",
            "data_authoritative_test.go",
        ),
    ]
env = dict(
    os.environ,
    GOWORK="off",
    PLUTO_CORE_REQUIRE_DATABASE="1",
    PYTHONDONTWRITEBYTECODE="1",
)


def run(directory, test):
    try:
        tags = (
            ["-tags", "plutocandidate"]
            if os.environ.get("PLUTO_CORE_CANDIDATE_DATA") == "1"
            else []
        )
        return subprocess.run(
            [
                "go",
                "test",
                *tags,
                "-json",
                "-count=1",
                "-timeout",
                "30s",
                "-run",
                test,
                ".",
            ],
            cwd=directory,
            env=env,
            capture_output=True,
            text=True,
            timeout=60,
        )
    except subprocess.TimeoutExpired:
        raise SystemExit(f"TIMEOUT is not semantic evidence: {test}")


def positive_control(phase):
    control = run(root / consumer, "^TestPostgres")
    try:
        pristine(control)
    except ValueError as error:
        raise SystemExit(
            f"PRISTINE INVALID: {error}\n{control.stdout}{control.stderr}"
        )
    print(f"PRISTINE SQL control passed {phase} mutations", flush=True)


prepare(root / consumer, env)
positive_control("before")

for mutation in mutations:
    path, old, new, target_test, assertion = mutation[:5]
    test_file = mutation[5] if len(mutation) > 5 else "writer_test.go"
    with tempfile.TemporaryDirectory(
        prefix="pluto-shipment-sql-mutation-"
    ) as temp:
        target = Path(temp)
        shutil.copytree(
            root,
            target,
            dirs_exist_ok=True,
            ignore=shutil.ignore_patterns(
                ".git", "go.work", "go.work.sum", ".DS_Store", "__pycache__"
            ),
        )
        if os.environ.get("PLUTO_CORE_CANDIDATE_DATA") == "1":
            for line in (root / consumer / "go.mod").read_text().splitlines():
                fields = line.split()
                if (
                    len(fields) == 4
                    and fields[0] == "replace"
                    and fields[2] == "=>"
                ):
                    resolved = (root / consumer / fields[3]).resolve()
                    if not resolved.is_relative_to(root):
                        subprocess.run(
                            [
                                "go",
                                "mod",
                                "edit",
                                "-replace=" + fields[1] + "=" + str(resolved),
                            ],
                            cwd=target / consumer,
                            env=env,
                            check=True,
                        )
        source = (target / path).read_text()
        if source.count(old) != 1:
            raise SystemExit(f"literal mismatch: {path}: {old}")
        (target / path).write_text(source.replace(old, new, 1))
        test_pattern = "/".join(
            "^" + part + "$" for part in target_test.split("/")
        )
        evidence = run(target / consumer, test_pattern)
        try:
            killed(evidence, target_test, assertion, test_file)
        except ValueError as error:
            raise SystemExit(
                f"SURVIVED/INVALID: {path}: {old}: {error}\n"
                f"{evidence.stdout}{evidence.stderr}"
            )
        print(f"KILLED: {path}: {old}: {target_test}: {assertion}", flush=True)

positive_control("after")
