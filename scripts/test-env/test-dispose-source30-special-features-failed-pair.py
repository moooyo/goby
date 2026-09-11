#!/usr/bin/env python3
"""Small source30 disposal guards; never import or execute the database runner."""

import ast
import copy
import hashlib
import json
import os
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest

sys.dont_write_bytecode = True
SOURCE = None
TREE = None
VALUES = None


def predicate(message, **values):
    matches = [node.args[0] for node in ast.walk(TREE) if isinstance(node, ast.Call) and
               isinstance(node.func, ast.Attribute) and node.func.attr == 'require' and len(node.args) == 2 and
               isinstance(node.args[1], ast.Constant) and node.args[1].value == message]
    if len(matches) != 1:
        raise AssertionError('The exact reviewed gate must exist once.')
    scope = {**VALUES, 'type': type, 'int': int, 'str': str, 'all': all, **values}
    return eval(compile(ast.Expression(matches[0]), '<reviewed-disposal-predicate>', 'eval'), {'__builtins__': {}, **scope})


class DisposalGuards(unittest.TestCase):
    def receipt(self):
        marker = 'goby-client-backup-pair-m3e-v1'
        return {'marker': marker, 'run_id': VALUES['RUN'], 'tag': marker + ':' + VALUES['RUN'],
            'unit': 'goby-client-backup-' + VALUES['RUN'].replace('_', '-') + '.service',
            'source': str(VALUES['SOURCE']), 'output': str(VALUES['OUTPUT']),
            'source_manifest_sha256': VALUES['MANIFEST_SHA'], 'schema': 27, 'mode': 'targeted', 'phase': 'retained',
            'catalog_sha256': VALUES['CATALOG_SHA'], 'cleanup_complete': False, 'cluster': copy.deepcopy(VALUES['CLUSTER']),
            'cluster_owner_sha256': VALUES['OWNER_SHA'], 'output_identity': copy.deepcopy(VALUES['OUTPUT_IDENTITY']),
            'pairs': [dict(pair, phase='owned') for pair in VALUES['PAIRS']]}

    def test_only_the_exact_source30_schema27_run_and_independent_runner_are_selected(self):
        self.assertEqual(VALUES['RUN'], '20260911_152703_711acca9cfa3')
        self.assertEqual(str(VALUES['SOURCE']), '/opt/goby-test/exec-work-m3e/source-attempt-30')
        self.assertEqual(str(VALUES['RUNNER']), '/opt/goby-test/exec-work-m3e/backup-schema27-tool-01/run-client-backup-tests.py')
        self.assertEqual(VALUES['RUNNER_SHA'], '7cf91591efaeca841907cfe1950d0bfcb6c33ef091a17fe54c69db167a74c22a')
        self.assertEqual(VALUES['PAIRS'], [{'name': 'goby_backup_m3e_source', 'role_oid': 8080578, 'database_oid': 8080579},
                                        {'name': 'goby_backup_m3e_target', 'role_oid': 8080580, 'database_oid': 8080581}])
        self.assertEqual(VALUES['CLUSTER']['system_identifier'], '7684040109719526738')

    def test_a_different_failure_receipt_or_incomplete_pair_cannot_authorize_disposal(self):
        base = self.receipt()
        context = {'m': SimpleNamespace(MARKER=base['marker']), 'tag': base['tag'], 'unit': base['unit']}
        gate = 'The failure receipt is outside this exact reviewed run.'
        self.assertTrue(predicate(gate, receipt=base, **context))
        for key, value in (('run_id', '20260911_000000_000000000000'), ('source', str(VALUES['SOURCE'].with_name('source-attempt-31'))),
                           ('schema', 26), ('schema', True), ('mode', 'full'), ('phase', 'finished'),
                           ('catalog_sha256', '0' * 64), ('cleanup_complete', True), ('cluster_owner_sha256', '0' * 64)):
            altered = copy.deepcopy(base)
            altered[key] = value
            with self.subTest(field=key, value=value):
                self.assertFalse(predicate(gate, receipt=altered, **context))
        gate = 'The reviewed pair identities changed.'
        self.assertTrue(predicate(gate, receipt=base))
        for key, value in (('role_oid', 16384), ('database_oid', 16385), ('phase', 'closed')):
            altered = copy.deepcopy(base)
            altered['pairs'][0][key] = value
            self.assertFalse(predicate(gate, receipt=altered))

    def test_the_wrapper_rejects_nonempty_objects_even_when_the_runner_accepts_a_catalog(self):
        gate = 'This disposal is authorized only for the reviewed empty pair.'
        self.assertTrue(predicate(gate, objects=[]))
        for objects in (None, {}, [{'kind': 'relation', 'name': 'users'}], [{'schema': 27, 'trusted': True}]):
            self.assertFalse(predicate(gate, objects=objects))

    def test_no_retest_hba_edit_receipt_replacement_or_direct_drop_is_added(self):
        calls = [node for node in ast.walk(TREE) if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)]
        names = [node.func.attr for node in calls]
        self.assertEqual(names.count('remove_pair'), 1)
        self.assertFalse({'prepare', 'cleanup', 'reload_hba', 'write_environment', 'replace_private', 'run_all'} & set(names))
        for node in calls:
            if node.func.attr == 'pg':
                literals = [part.value for part in ast.walk(node.args[0]) if isinstance(part, ast.Constant) and isinstance(part.value, str)]
                self.assertTrue(''.join(literals).startswith('SELECT '))
        self.assertIn('disposal-original-receipt.json', SOURCE)
        self.assertIn('runner.save = save_progress', SOURCE)
        self.assertIn('runner.inspect_objects = inspect_empty', SOURCE)


def main():
    global SOURCE, TREE, VALUES
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION') or len(sys.argv) != 2:
        raise SystemExit('Run these small read-only guards only through authorized root SSH.')
    path = Path(sys.argv[1])
    if path.name != 'dispose-source30-special-features-failed-pair.py':
        raise SystemExit('Only the fixed source30 disposal wrapper is accepted.')
    raw = path.read_bytes()
    SOURCE, TREE = raw.decode(), ast.parse(raw, filename=str(path))
    VALUES = {'Path': Path}
    # Evaluate only the simple constant declarations, never functions or imports.
    for node in TREE.body:
        if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
            VALUES[node.targets[0].id] = eval(compile(ast.Expression(node.value), '<constant>', 'eval'), {'__builtins__': {}, **VALUES})
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(DisposalGuards)
    result = unittest.TestResult()
    suite.run(result)
    print(json.dumps({'suite': 'source30-disposal-guards', 'status': 'passed' if result.wasSuccessful() else 'failed',
        'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skips': len(result.skipped),
        'operator_sha256': hashlib.sha256(raw).hexdigest(), 'guard_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'fixtures': 'syntax-tree-and-memory-only', 'database_commands': 0, 'filesystem_mutations': 0,
        'summaries': [detail.rstrip().splitlines()[-1] for _, detail in [*result.failures, *result.errors]]}))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
