#!/usr/bin/env python3
"""Bounded guard mutations; build failures and timeouts are not killed mutants."""
from pathlib import Path
import shutil
import subprocess
import tempfile

root = Path(__file__).resolve().parents[1]
mutations = [
    ("state/state.go", "if transition.Guard != nil {", "if false {", "./state"),
    ("state/state.go", "!fromKnown || !toKnown || transition.On == \"\" || duplicate", "!fromKnown || !toKnown || transition.On == \"\" || (duplicate && false)", "./state"),
    ("cache/key.go", "scope.TenantID,", '"",', "./cache"),
    ("cache/memory/store.go", "!s.config.Now().Before(value.expires)", "false", "./cache/memory"),
    ("cache/loader.go", "pending.waiters >= l.options.MaxWaiters", "false", "./cache"),
    ("pagination/pagination.go", "limit > MaxLimit", "limit > 1000", "./pagination"),
    ("pagination/pagination.go", "value.Binding != binding", "false", "./pagination"),
    ("idempotency/memory/memory.go", "if r.claim.Fingerprint != claim.Fingerprint {", "if false {", "./idempotency/memory"),
    ("idempotency/memory/memory.go", "r.claim.Owner != claim.Owner", "false", "./idempotency/memory"),
    ("httpidem/http.go", "len(capture.header.Values(name)) > 0", "len(capture.header.Values(name)) < 0", "./httpidem"),
]
for path, old, new, package in mutations:
    with tempfile.TemporaryDirectory(prefix="pluto-core-mutation-") as temp:
        target = Path(temp)
        shutil.copytree(root, target, dirs_exist_ok=True, ignore=shutil.ignore_patterns('.git', 'go.work', 'go.work.sum'))
        file = target / path
        source = file.read_text()
        if source.count(old) != 1:
            raise SystemExit(f"literal target mismatch: {path}: {old}")
        file.write_text(source.replace(old, new, 1))
        try:
            result = subprocess.run(['go','test','-count=1',package],cwd=target,capture_output=True,text=True,timeout=60)
        except subprocess.TimeoutExpired:
            raise SystemExit(f"TIMEOUT: {path}")
        output = result.stdout + result.stderr
        if result.returncode == 0 or '[build failed]' in output or '--- FAIL:' not in output:
            raise SystemExit(f"SURVIVED/INVALID: {path}\n{output}")
        print(f"KILLED: {path}: {old}")
