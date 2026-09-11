#!/usr/bin/env python3
"""Memory-only guards for the one source31 failed-disposal repair."""

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
        publish = lambda path, value: events.append(('publish', path.name, value, path.parent))
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
            self.assertIn("(SELECT '[]'::jsonb)::jsonb IS DISTINCT FROM E'[]'::jsonb", sql)
            self.assertIn('END;$source31_checked_state$;', sql)
            self.assertTrue(sql.endswith('DROP TABLE public."item_extra_resources",public."extra_reserved_paths"; COMMIT;'))
            self.assertEqual(sql.count(' CASCADE;'), 1 if source else 0)
            if source:
                self.assertIn('DROP SCHEMA "' + OP.EXTRA_SCHEMA + '" CASCADE;', sql)
            self.assertNotIn('DROP DATABASE', sql)
            self.assertNotIn('pg_terminate_backend', sql)
            self.assertEqual({event[3] for event in events if event[0] == 'publish'}, {OP.REPAIR_OUTPUT})

    def test_text_and_jsonb_scalars_keep_the_same_json_values_and_escape_boundaries(self):
        model = self.model(source=False)
        raw = json.dumps({'large': 9007199254740993, 'name': "O'Reilly\\archive"}, separators=(',', ':'))
        model[4]['public'] = [("SELECT '{}'::jsonb::text", raw), ("SELECT '{}'::jsonb", raw)]
        OP.normalize_reviewed_pair(*model[:6])
        sql = model[6][1][1]
        self.assertEqual(sql.count(')::jsonb IS DISTINCT FROM'), 2)
        self.assertIn("(SELECT '{}'::jsonb::text)::jsonb IS DISTINCT FROM", sql)
        self.assertIn("(SELECT '{}'::jsonb)::jsonb IS DISTINCT FROM", sql)
        self.assertEqual(sql.count(raw.replace('\\', '\\\\').replace("'", "''")), 2)
        self.assertIn('9007199254740993', sql)

    def test_first_attempt_is_pinned_immutable_evidence_and_repair_has_a_new_root(self):
        self.assertEqual(OP.REPAIR_OUTPUT, OP.WORK / 'source31-disposal-repair-01')
        self.assertNotEqual(OP.REPAIR_OUTPUT, OP.OUTPUT)
        self.assertEqual(OP.FAILED_OPERATOR_SHA, '85d63c61b938a5cf014a77a34bee085753c425d206aa923580b7c21ac4fb4800')
        self.assertEqual(OP.FAILED_GUARD_SHA, 'c9c70ff4c73abfdc96ffd5d01cfa4f02effc0dc00cc8fbe8ff921f80695f7b16')
        self.assertEqual(len(OP.FIRST_ATTEMPT_FILES), 5)
        before = {pair['name']: {} for pair in OP.PAIRS}
        failure = {'run_id': OP.RUN, 'status': 'failed', 'error_type': 'Failure',
            'source_receipt_sha256': OP.RECEIPT_SHA, 'pairs': [dict(pair, phase='owned') for pair in OP.PAIRS]}
        intent = {'run_id': OP.RUN, 'status': 'reviewed', 'removed': [], 'operator_sha256': OP.FAILED_OPERATOR_SHA,
            'source_receipt_sha256': OP.RECEIPT_SHA, 'before': before}
        normalization = {'run_id': OP.RUN, 'pair': OP.PAIRS[1], 'before_sha256': OP.fingerprint(before[OP.PAIRS[1]['name']]),
            'exact_schema': None, 'public_tables': list(OP.TABLES), 'schema_dependency_closure_verified': True, 'public_cascade': False}
        diagnosis = {'run_id': OP.RUN, 'operator_sha256': OP.FAILED_OPERATOR_SHA, 'database_read_only': True,
            'lock_or_drop_executed': False, 'before_matches_failed_attempt': True, 'after_unchanged': True,
            'original_receipt_unchanged': True, 'cases': [{'case': 'original', 'exit_code': 3}, {'case': 'cast-jsonb', 'exit_code': 0}]}
        documents = {'disposal-before.json': before, 'disposal-failed.json': failure, 'disposal-intent.json': intent,
            'disposal-normalize-goby_backup_m3e_target-intent.json': normalization, 'disposal-original-receipt.json': {}}
        reads, digests = {}, {}
        for name, value in documents.items():
            raw = json.dumps(value, sort_keys=True).encode()
            reads[OP.OUTPUT / name] = raw
            digests[raw] = OP.FIRST_ATTEMPT_FILES[name]
        for path, digest, raw in [(OP.FAILED_OPERATOR, OP.FAILED_OPERATOR_SHA, b'old operator'),
                (OP.FAILED_GUARD, OP.FAILED_GUARD_SHA, b'old guard'),
                (OP.DIAGNOSIS, OP.DIAGNOSIS_SHA, json.dumps(diagnosis).encode())]:
            reads[path], digests[raw] = raw, digest
        m = SimpleNamespace(require=require, private_read=lambda path, **_: reads[path],
            sha=lambda raw: digests.get(raw, 'changed'), decode=json.loads)
        OP.verify_failed_attempt(m)
        for path in list(reads):
            original = reads[path]
            reads[path] = original + b'changed'
            with self.subTest(changed=str(path)), self.assertRaises(ValueError):
                OP.verify_failed_attempt(m)
            reads[path] = original

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
    if path.name != 'repair-source31-special-features-disposal.py':
        raise SystemExit('Only the source31 wrapper is supported.')
    raw = path.read_bytes()
    tree = ast.parse(raw)
    scope = {'Path': Path, 'json': json, 'hashlib': hashlib, 're': re}
    for node in tree.body:
        if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
            scope[node.targets[0].id] = eval(compile(ast.Expression(node.value), '<constant>', 'eval'), {'__builtins__': {}, **scope})
    selected = {'fingerprint', 'expected_objects', 'identity_sql', 'closure_sql', 'preparation_unknown_sql', 'normalize_reviewed_pair', 'verify_failed_attempt'}
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
    print(json.dumps({'suite': 'source31-disposal-repair-guards', 'status': 'passed' if result.wasSuccessful() else 'failed',
        'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors), 'skips': len(result.skipped),
        'operator_sha256': hashlib.sha256(raw).hexdigest(), 'guard_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        'fixtures': 'actual-catalog-and-memory-only', 'database_commands': 0, 'filesystem_mutations': 0,
        'summaries': [detail.rstrip().splitlines()[-1] for _, detail in [*result.failures, *result.errors]]}))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
