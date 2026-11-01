"""Classify semantic SQL mutation failures, never generic test failures."""
import json
import re

REQUIRED = {
    'TestPostgresDispatchBinding',
    'TestPostgresConcurrentReplay',
    'TestPostgresOutboxRollback',
}


def events(result):
    if result.stderr:
        raise ValueError('unexpected stderr (build/tooling/infrastructure failure)')
    try:
        records = [json.loads(line) for line in result.stdout.splitlines()]
    except (ValueError, TypeError) as error:
        raise ValueError('invalid go test JSON') from error
    if not records or any(not isinstance(record, dict) for record in records):
        raise ValueError('missing or invalid go test events')
    if any(record.get('Action') == 'skip' for record in records):
        raise ValueError('skipped test or package is not evidence')
    return records


def pristine(result):
    records = events(result)
    passed = {record.get('Test') for record in records
              if record.get('Action') == 'pass'}
    if result.returncode != 0 or any(record.get('Action') == 'fail'
                                     for record in records) or not REQUIRED <= passed:
        raise ValueError('pristine SQL control failed or required cases absent')


def killed(result, target, assertion):
    records = events(result)
    expected = {target, target.split('/')[0]}
    failed = {record.get('Test') for record in records
              if record.get('Action') == 'fail' and record.get('Test')}
    if result.returncode != 1 or failed != expected:
        raise ValueError('survived or wrong failing test/subtest')
    marker = re.compile(r'^\s+writer_test\.go:\d+: ' + re.escape(assertion) + r'\n?$')
    found = 0
    for record in records:
        test = record.get('Test')
        if not test:
            output = record.get('Output', '')
            if output and not re.fullmatch(
                    r'(?:FAIL(?:\t[^\n]+)?|exit status 1)\n?', output):
                raise ValueError('unexpected package-level failure output')
            continue
        if test not in expected:
            raise ValueError('unrelated test activity')
        output = record.get('Output', '')
        if not output:
            continue
        if test == target and marker.fullmatch(output):
            found += 1
        elif output.startswith('=== RUN   ' + test) or re.fullmatch(
                r'\s*--- FAIL: ' + re.escape(test) + r' \([0-9.]+s\)\n?', output):
            continue
        else:
            raise ValueError('unexpected failure output (setup/infrastructure/other assertion)')
    if found != 1:
        raise ValueError('intended business assertion missing or duplicated')
