#!/usr/bin/env python3
"""Small memory-only guards over exact source31 disposal and catalog inputs."""

import ast
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import sys
from types import SimpleNamespace
import unittest

sys.dont_write_bytecode = True
OP = None
BASELINE = None
RUNNER_SQL = None


def require(value, message):
    if not value:
        raise ValueError(message)


class DisposalGuards(unittest.TestCase):
    def test_exact_run_schema_oid_and_pair_identity_are_not_prefix_authority(self):
        self.assertEqual(OP.RUN, '20260911_154145_d9df2c8f78f8')
        self.assertEqual(OP.EXTRA_SCHEMA, 'goby_server_test_32ed586c92dbf6c66de0b89c')
        self.assertEqual(OP.PAIRS, [{'name': 'goby_backup_m3e_source', 'role_oid': 8334353, 'database_oid': 8334354},
                                  {'name': 'goby_backup_m3e_target', 'role_oid': 8334355, 'database_oid': 8334356}])
        self.assertEqual(OP.OBSERVED[('goby_backup_m3e_source', OP.EXTRA_SCHEMA)]['oid'], 9014011)
        self.assertEqual(OP.OBSERVED[('goby_backup_m3e_target', 'public')]['oid'], 8961085)

    def test_public_residual_is_exact_catalog27_subset_with_only_three_foreign_keys_missing(self):
        m = SimpleNamespace(require=require)
        partial = OP.expected_objects(m, BASELINE, 'public')
        self.assertEqual(len(partial), 26)
        self.assertEqual(len(OP.MISSING_FOREIGN), 3)
        original = {(row['kind'], row['name']): row for row in BASELINE['objects']}
        self.assertTrue(all(original[(row['kind'], row['name'])] == row for row in partial))
        self.assertFalse(any(row['kind'] == 'constraint' and row['name'] in OP.MISSING_FOREIGN for row in partial))
        self.assertEqual(OP.expected_objects(m, BASELINE, OP.EXTRA_SCHEMA), BASELINE['objects'])
        with self.assertRaises(ValueError):
            OP.expected_objects(m, BASELINE, 'goby_server_test_unowned')

    def test_preparation_check_only_exempts_the_exact_namespace_and_checks_its_objects(self):
        m = SimpleNamespace(require=require, unsupported_objects_sql=RUNNER_SQL)
        original = RUNNER_SQL(8334353)
        actual = OP.preparation_unknown_sql(m, OP.PAIRS[0]['name'], 8334353)
        self.assertIn("oid=9014011 AND nspname='" + OP.EXTRA_SCHEMA + "' AND nspowner=8334353 AND nspacl IS NULL", actual)
        self.assertIn("n.nspname IN ('public','" + OP.EXTRA_SCHEMA + "')", actual)
        self.assertEqual(RUNNER_SQL(8334353), original)
        self.assertEqual(OP.preparation_unknown_sql(m, OP.PAIRS[1]['name'], 8334355), RUNNER_SQL(8334355))

    def model(self, source=True, residual=False):
        events = []
        pair = dict(OP.PAIRS[0 if source else 1])
        schemas = [OP.EXTRA_SCHEMA, 'public'] if source else ['public']
        before = {'namespaces': {name: {'table_data': dict.fromkeys(OP.TABLES if name == 'public' else ['items'])} for name in schemas}}
        checks = {name: [("SELECT '[]'::jsonb", '[]')] for name in schemas}
        def pg(sql, name):
            events.append(('sql', sql, name))
            return ''
        m = SimpleNamespace(require=require, unsupported_objects_sql=RUNNER_SQL, pg=pg,
            pair_rows=lambda name: {}, validate_pair_rows=lambda *_: None, require_role_isolated=lambda *_: None)
        runner = SimpleNamespace(tag='owned', hba_before=b'unchanged', check_cluster=lambda *_: None,
            inspect_objects=lambda _: [{'unknown': True}] if residual else [])
        publish = lambda path, value: events.append(('publish', path.name, value))
        return m, runner, pair, before, checks, publish, events

    def test_normalization_is_receipted_transactional_and_cascades_only_the_exact_schema(self):
        for source in (False, True):
            model = self.model(source)
            OP.normalize_reviewed_pair(*model[:6])
            events = model[6]
            self.assertEqual([event[0] for event in events], ['publish', 'sql', 'publish'])
            sql = events[1][1]
            self.assertTrue(sql.startswith('BEGIN;'))
            self.assertIn('LOCK TABLE ', sql)
            self.assertIn("RAISE EXCEPTION 'Reviewed residual state changed'", sql)
            self.assertIn('END;$source31_checked_state$;', sql)
            self.assertTrue(sql.endswith('DROP TABLE public."item_extra_resources",public."extra_reserved_paths"; COMMIT;'))
            self.assertEqual(sql.count(' CASCADE;'), 1 if source else 0)
            if source:
                self.assertIn('DROP SCHEMA "' + OP.EXTRA_SCHEMA + '" CASCADE;', sql)
            self.assertNotIn('DROP DATABASE', sql)
            self.assertNotIn('pg_terminate_backend', sql)

    def test_unknown_scope_or_nonempty_post_state_cannot_publish_normalization_success(self):
        for defect in ('oid', 'schema', 'table'):
            model = self.model()
            if defect == 'oid': model[2]['database_oid'] = 16385
            elif defect == 'schema': model[3]['namespaces']['unowned'] = {'table_data': {}}
            else: model[3]['namespaces']['public']['table_data']['users'] = None
            with self.subTest(defect=defect), self.assertRaises(ValueError):
                OP.normalize_reviewed_pair(*model[:6])
            self.assertEqual(model[6], [])
        model = self.model(residual=True)
        with self.assertRaises(ValueError):
            OP.normalize_reviewed_pair(*model[:6])
        self.assertEqual([event[0] for event in model[6]], ['publish', 'sql'])
        closure = OP.closure_sql(9014011)
        self.assertIn('reltoastrelid', closure)
        self.assertIn("d.deptype IN ('i','e','P','S')", closure)
        self.assertNotIn('LIKE', closure)


def main():
    global OP, BASELINE, RUNNER_SQL
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION') or len(sys.argv) != 2:
        raise SystemExit('Run only through authorized root SSH with the exact disposal wrapper path.')
    path = Path(sys.argv[1])
    if path.name != 'dispose-source31-special-features-full-failed-pair.py':
        raise SystemExit('Only the source31 wrapper is supported.')
    raw = path.read_bytes()
    tree = ast.parse(raw)
    scope = {'Path': Path, 'json': json, 'hashlib': hashlib, 're': re}
    for node in tree.body:
        if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
            scope[node.targets[0].id] = eval(compile(ast.Expression(node.value), '<constant>', 'eval'), {'__builtins__': {}, **scope})
    selected = {'fingerprint', 'expected_objects', 'identity_sql', 'closure_sql', 'preparation_unknown_sql', 'normalize_reviewed_pair'}
    functions = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in selected]
    exec(compile(ast.Module(functions, type_ignores=[]), '<pure-disposal-guards>', 'exec'), scope)
    OP = SimpleNamespace(**scope)
    catalog = (OP.SOURCE / 'internal/backuppg/catalogs/schema-27-postgresql-17.json').read_bytes()
    require(hashlib.sha256(catalog).hexdigest() == OP.CATALOG_SHA, 'The actual schema27 catalog changed.')
    BASELINE = json.loads(catalog)
    runner = OP.RUNNER.read_bytes()
    require(hashlib.sha256(runner).hexdigest() == OP.RUNNER_SHA, 'The frozen runner changed.')
    query = next(node for node in ast.parse(runner).body if isinstance(node, ast.FunctionDef) and node.name == 'unsupported_objects_sql')
    namespace = {}
    exec(compile(ast.Module([query], type_ignores=[]), '<pure-runner-sql>', 'exec'), namespace)
    RUNNER_SQL = namespace['unsupported_objects_sql']
    result = unittest.TestResult()
    unittest.defaultTestLoader.loadTestsFromTestCase(DisposalGuards).run(result)
    print(json.dumps({'suite': 'source31-disposal-guards', 'status': 'passed' if result.wasSuccessful() else 'failed',
        'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skips': len(result.skipped),
        'operator_sha256': hashlib.sha256(raw).hexdigest(), 'guard_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'fixtures': 'actual-catalog-and-memory-only', 'database_commands': 0, 'filesystem_mutations': 0,
        'summaries': [detail.rstrip().splitlines()[-1] for _, detail in [*result.failures, *result.errors]]}))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
