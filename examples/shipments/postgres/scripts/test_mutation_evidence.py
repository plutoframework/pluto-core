import json
from types import SimpleNamespace
import unittest

from mutation_evidence import REQUIRED, killed, pristine

TARGET = 'TestPostgresDispatchBinding/scope'
ASSERTION = 'got <nil> want shipment snapshot conflict'


def result(records, code=1, stderr=''):
    return SimpleNamespace(returncode=code, stderr=stderr,
                           stdout='\n'.join(json.dumps(record) for record in records))


def failure(message=ASSERTION, target=TARGET):
    return [
        {'Action': 'output', 'Test': target,
         'Output': f'    writer_test.go:258: {message}\n'},
        {'Action': 'fail', 'Test': target},
        {'Action': 'fail', 'Test': target.split('/')[0]},
        {'Action': 'fail'},
    ]


class EvidenceTests(unittest.TestCase):
    def test_expected_semantic_assertion(self):
        killed(result(failure()), TARGET, ASSERTION)

    def test_infrastructure_and_setup_are_not_kills(self):
        for message in ('connection refused', 'CREATE SCHEMA permission denied',
                        'required shipment SQL integration needs URL',
                        'ERROR: syntax error at or near WHERE',
                        'panic: test timed out after 30s', 'different business assertion'):
            with self.subTest(message=message), self.assertRaises(ValueError):
                killed(result(failure(message)), TARGET, ASSERTION)

    def test_build_timeout_missing_or_unrelated_failure_is_not_kill(self):
        cases = [
            result(failure(), stderr='[build failed]'),
            result(failure(), code=0),
            result(failure(), code=2),
            result([]),
            result([{'Action': 'skip', 'Test': TARGET}]),
            result(failure(target='TestUnrelated')),
            result(failure() + [{'Action': 'fail', 'Test': 'TestUnrelated'}]),
            result(failure() + [{'Action': 'output',
                                'Output': 'panic: unrelated background failure\n'}]),
            result(failure() + [{'Action': 'output', 'Test': TARGET,
                                'Output': 'panic: test timed out after 30s\n'}]),
            SimpleNamespace(returncode=1, stderr='', stdout='[build failed]'),
        ]
        for case in cases:
            with self.subTest(case=case), self.assertRaises(ValueError):
                killed(case, TARGET, ASSERTION)

    def test_pristine_requires_all_real_cases(self):
        records = [{'Action': 'pass', 'Test': test} for test in REQUIRED]
        pristine(result(records, code=0))
        for case in (result(records), result(records[:-1], code=0),
                     result(records + [{'Action': 'skip', 'Test': TARGET}], code=0),
                     result(records, code=0, stderr='connection refused')):
            with self.subTest(case=case), self.assertRaises(ValueError):
                pristine(case)


if __name__ == '__main__':
    unittest.main()
