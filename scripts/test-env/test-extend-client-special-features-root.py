#!/usr/bin/env python3
"""Exercise extension guards in memory on authorized remote test-env only.

Synthetic rows below are not a PostgreSQL catalog or deployed-state evidence.
The frozen fixture comparator is used with a synthetic schema-validation stub;
these cases test exact preservation and orchestration boundaries, not SQL DDL.
"""

from __future__ import annotations

import argparse
import copy
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import types
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
EXT = BASE = None
TABLE_NAMES = ()


def imported(path, expected, name):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('A guard input source changed.')
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def fixture_chain():
    process = {'pid': 4321, 'start_ticks': 76543, 'boot_id': '11111111-2222-3333-4444-555555555555'}
    args = types.SimpleNamespace(candidate_pid=process['pid'], candidate_start_ticks=process['start_ticks'], candidate_boot_id=process['boot_id'],
        candidate_sha256='1' * 64, operator_path=EXT.WORK / 'prepare-client-fixture.py', operator_sha256='2' * 64,
        upgrade_dir=EXT.WORK / ('client-upgrade-' + '3' * 32), source=EXT.WORK / 'synthetic-source', source_manifest_sha256='4' * 64,
        full_report=EXT.WORK / 'client-backup-run-20260912_000000_555555555555/report.json', full_report_sha256='5' * 64)
    completed = {'id': '3' * 32, 'phase': 'complete', 'from_schema': 26, 'to_schema': 27, 'from_sha256': '6' * 64,
        'to_sha256': args.candidate_sha256, 'new_process': process, 'installed_binary_sha256': args.candidate_sha256,
        'installed_binary_identity': {'device': 8, 'inode': 900}, 'evidence_directory': str(args.upgrade_dir),
        'schema_artifacts': {'source': str(args.source), 'source_manifest_sha256': args.source_manifest_sha256,
            'operator': {'path': str(args.operator_path), 'sha256': args.operator_sha256},
            'product_verification': {'report_path': str(args.full_report), 'report_sha256': args.full_report_sha256}}}
    state = {'marker': BASE.MARKER, 'phase': 'ready', 'schema': 27, 'process': process,
        'binary_sha256': args.candidate_sha256, 'binary_identity': completed['installed_binary_identity'],
        'runtime_sha256': EXT.OLD_RUNTIME_SHA, 'upgrade': copy.deepcopy(completed), 'libraries': {
            key: {'id': 'owned-' + key} for key in ('movies', 'tv', 'music')}}
    report = {'marker': BASE.MARKER, 'result': 'ready', 'phase': 'ready', 'process': process,
        'binary_sha256': args.candidate_sha256, 'upgrade': {key: completed.get(key) for key in EXT.PUBLIC_UPGRADE_FIELDS}}
    return args, state, completed, report


def synthetic_snapshot(state):
    tables = {key: [] for key in TABLE_NAMES}
    tables['items'] = [{'id': str(index), 'value': Decimal('12345678901234567890.0123456789')} for index in range(13)]
    tables['libraries'] = [{'id': item['id']} for item in state['libraries'].values()]
    return {'schema': 27, 'runtime_sha256': state['runtime_sha256'], 'browser_sha256': '7' * 64,
        'added_viewer_credentials': {'receipt_sha256': '8' * 64}, 'recovery': {'kept': {'sha256': '9' * 64}},
        'database': {'metadata': {'database': BASE.ROLE, 'server_version_num': 170009, 'schemas': ['public'],
            'public_schema': {'oid': 2200, 'owner': BASE.ROLE}, 'captured_at': '2026-09-12T00:00:00Z',
            'relations': {'items': {'oid': 100, 'owner': BASE.ROLE, 'acl': None}}, 'columns': {'items': ['id', 'value']}},
            'tables': tables, 'catalog': [{'kind': 'synthetic'}],
            'sequences': {'synthetic_sequence': {'last_value': 9223372036854775700, 'is_called': True}}}}


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.fences = [patch.object(subprocess, 'run', side_effect=AssertionError('Unexpected process execution.')),
            patch.object(socket, 'socket', side_effect=AssertionError('Unexpected network access.')),
            patch.object(os, 'replace', side_effect=AssertionError('Unexpected file replacement.')),
            patch.object(os, 'system', side_effect=AssertionError('Unexpected shell execution.'))]
        for fence in self.fences:
            fence.start()
            self.addCleanup(fence.stop)

    def test_exact_runtime_change_preserves_unrelated_secret_bytes(self):
        old = (b"GOBY_DATABASE_URL='synthetic-private-value'\nGOBY_SETUP_TOKEN='synthetic-token'\n" +
               b"GOBY_MEDIA_ROOTS='" + ':'.join(EXT.OLD_ROOTS).encode() + b"'\nGOBY_SERVER_NAME='Synthetic Name'\n")
        new = EXT.extend_runtime(old)
        self.assertEqual(new.replace((':' + EXT.ADDED_ROOT).encode(), b'', 1), old)
        self.assertEqual(new.count(EXT.ADDED_ROOT.encode()), 1)
        self.assertNotEqual(new, old)

    def test_runtime_rejects_adoption_broadening_duplicates_and_malformed_lines(self):
        valid = b"GOBY_MEDIA_ROOTS='" + ':'.join(EXT.OLD_ROOTS).encode() + b"'\n"
        bad = [b"OTHER='synthetic'\n", valid + valid, valid.replace(b"'\n", (':' + EXT.ADDED_ROOT + "'\n").encode()),
            b"GOBY_MEDIA_ROOTS='/opt/goby-fixtures'\n", valid.replace(b'\n', b'\r\n'), valid[:-1],
            valid + b"OTHER='first'\nOTHER='second'\n", valid.replace(b'/TV:', b'/Movies:')]
        for raw in bad:
            with self.subTest(raw_length=len(raw)), self.assertRaises(EXT.ExtensionError):
                EXT.extend_runtime(raw)

    def test_completed_chain_accepts_exact_actual_projection_shape(self):
        args, state, completed, report = fixture_chain()
        EXT.validate_receipt_chain(BASE, args, state, completed, report)

    def test_completed_chain_rejects_wrong_process_source_operator_or_report(self):
        for change in ('process', 'operator-path', 'operator-sha', 'source', 'full-report', 'projection', 'already-extended', 'runtime'):
            args, state, completed, report = fixture_chain()
            if change == 'process':
                state['process'] = {**state['process'], 'start_ticks': 76544}
            elif change.startswith('operator-'):
                completed['schema_artifacts']['operator']['path' if change.endswith('path') else 'sha256'] = 'wrong'
                state['upgrade'] = copy.deepcopy(completed)
            elif change == 'source':
                args.source = EXT.WORK / 'another-source'
            elif change == 'full-report':
                args.full_report_sha256 = '0' * 64
            elif change == 'projection':
                report['upgrade'].pop('schema_artifacts')
            elif change == 'already-extended':
                state[EXT.STATE_KEY] = {}
            else:
                state['runtime_sha256'] = '0' * 64
            with self.subTest(change=change), self.assertRaises(EXT.ExtensionError):
                EXT.validate_receipt_chain(BASE, args, state, completed, report)

    def test_quiescence_preserves_prepared_revoked_history(self):
        _, state, _, _ = fixture_chain()
        snapshot = synthetic_snapshot(state)
        snapshot['database']['tables']['sessions'] = [{'id': 'owned-auth', 'user_id': 'owned-viewer', 'kind': 'emby', 'revoked_at': '2026-09-11T23:00:00Z'}]
        snapshot['database']['tables']['play_sessions'] = [{'state': 'Prepared', 'auth_session_id': 'owned-auth', 'user_id': 'owned-viewer',
            'started_at': None, 'counted': False, 'application_client_id': None}]
        before = copy.deepcopy(snapshot)
        EXT.require_quiescent(snapshot, state)
        self.assertTrue(BASE.equal_json(snapshot, before))

    def test_quiescence_uses_actual_catalog_encoding_table_without_missing_table_fallback(self):
        _, state, _, _ = fixture_chain()
        snapshot = synthetic_snapshot(state)
        self.assertEqual(len(TABLE_NAMES), 35)
        self.assertIn('encoding_jobs', TABLE_NAMES)
        self.assertNotIn('encoding_states', TABLE_NAMES)
        EXT.require_quiescent(snapshot, state)
        for value in (None, [{'state': 'completed'}], [{'state': 'running'}]):
            changed = copy.deepcopy(snapshot)
            changed['database']['tables']['encoding_jobs'] = value
            with self.subTest(value=value), self.assertRaises(EXT.ExtensionError):
                EXT.require_quiescent(changed, state)
        changed = copy.deepcopy(snapshot)
        changed['database']['tables']['encoding_states'] = changed['database']['tables'].pop('encoding_jobs')
        with self.assertRaises(EXT.ExtensionError):
            EXT.require_quiescent(changed, state)

    def test_quiescence_rejects_active_or_unproven_work(self):
        for change in ('Playing', 'Paused', 'encoding', 'scan', 'task', 'prepared-live-auth', 'prepared-started', 'prepared-other-user'):
            _, state, _, _ = fixture_chain()
            snapshot = synthetic_snapshot(state)
            tables = snapshot['database']['tables']
            if change in ('Playing', 'Paused'):
                tables['play_sessions'] = [{'state': change}]
            elif change == 'encoding':
                tables['encoding_jobs'] = [{'state': 'running'}]
            elif change == 'scan':
                tables['scan_jobs'] = [{'status': 'queued'}]
            elif change == 'task':
                tables['task_runs'] = [{'state': 'running'}]
            else:
                tables['sessions'] = [{'id': 'auth', 'user_id': 'viewer', 'kind': 'emby', 'revoked_at': None if change == 'prepared-live-auth' else '2026-09-11T00:00:00Z'}]
                tables['play_sessions'] = [{'state': 'Prepared', 'auth_session_id': 'auth', 'user_id': 'other' if change == 'prepared-other-user' else 'viewer',
                    'started_at': '2026-09-11T00:00:00Z' if change == 'prepared-started' else None, 'counted': False, 'application_client_id': None}]
            with self.subTest(change=change), self.assertRaises(EXT.ExtensionError):
                EXT.require_quiescent(snapshot, state)

    def preservation(self, mutate=None):
        _, old_state, _, _ = fixture_chain()
        before = synthetic_snapshot(old_state)
        after = copy.deepcopy(before)
        new_state = copy.deepcopy(old_state)
        new_state['runtime_sha256'] = after['runtime_sha256'] = 'a' * 64
        after['database']['metadata']['captured_at'] = '2026-09-12T00:01:00Z'
        if mutate:
            mutate(after)
        def synthetic_validation(snapshot, version, state):
            BASE.require(version == snapshot['schema'] == 27 and snapshot['runtime_sha256'] == state['runtime_sha256'] and
                         set(snapshot['database']['tables']) == set(before['database']['tables']), 'Synthetic schema/runtime mismatch.')
        with patch.object(BASE, 'validate_preservation_snapshot', synthetic_validation):
            EXT.compare_after_extension(BASE, before, after, old_state, new_state, 'a' * 64)
        self.assertEqual(after['runtime_sha256'], 'a' * 64)

    def test_exact_comparator_allows_only_proven_runtime_and_capture_time_delta(self):
        self.preservation()

    def test_exact_comparator_rejects_rows_sequences_credentials_recovery_or_runtime(self):
        mutations = {
            'row': lambda value: value['database']['tables']['items'][0].update(value=Decimal('12345678901234567890.0123456790')),
            'sequence': lambda value: value['database']['sequences']['synthetic_sequence'].update(last_value=9223372036854775701),
            'credential': lambda value: value.update(browser_sha256='b' * 64),
            'recovery': lambda value: value['recovery']['kept'].update(sha256='c' * 64),
            'runtime': lambda value: value.update(runtime_sha256='d' * 64),
            'relation': lambda value: value['database']['metadata']['relations']['items'].update(oid=101),
            'new-table': lambda value: value['database']['tables'].update(unapproved=[]),
        }
        for name, mutation in mutations.items():
            with self.subTest(change=name), self.assertRaises((EXT.ExtensionError, BASE.FixtureError)):
                self.preservation(mutation)

    def test_state_extension_cannot_rewrite_upgrade_or_other_fields(self):
        _, before, _, _ = fixture_chain()
        after = copy.deepcopy(before)
        after.update(phase='extending_media_root', stage='prepared', runtime_sha256='e' * 64)
        after[EXT.STATE_KEY] = {'marker': EXT.MARKER, 'evidence_directory': str(EXT.OUTPUT), 'added_root': EXT.ADDED_ROOT}
        EXT.validate_state_delta(BASE, before, after)
        for change in ('upgrade', 'root', 'schema', 'unrelated'):
            altered = copy.deepcopy(after)
            if change == 'upgrade':
                altered['upgrade']['phase'] = 'replaced'
            elif change == 'root':
                altered[EXT.STATE_KEY]['added_root'] = '/opt/goby-fixtures'
            else:
                altered[change] = 99
            with self.subTest(change=change), self.assertRaises(EXT.ExtensionError):
                EXT.validate_state_delta(BASE, before, altered)

    def test_service_actions_are_exact_owned_once_and_ordered(self):
        unit = 'goby-client-m3e.service'
        fence = EXT.ServiceFence()
        for argv in (['/usr/bin/systemctl', 'start', unit], ['/usr/bin/systemctl', 'stop', unit],
                     ['/usr/bin/systemctl', 'daemon-reload'], ['systemctl', 'stop', unit]):
            with self.assertRaises(EXT.ExtensionError):
                fence.approve(argv, unit)
        fence.stage = 'stop_requested'
        with self.assertRaises(EXT.ExtensionError):
            fence.approve(['/usr/bin/systemctl', 'stop', 'goby.service'], unit)
        fence.approve(['/usr/bin/systemctl', 'stop', unit], unit)
        with self.assertRaises(EXT.ExtensionError):
            fence.approve(['/usr/bin/systemctl', 'stop', unit], unit)
        fence.stage = 'start_requested'
        fence.approve(['/usr/bin/systemctl', 'start', unit], unit)
        with self.assertRaises(EXT.ExtensionError):
            fence.approve(['/usr/bin/systemctl', 'start', unit], unit)
        self.assertEqual(fence.reserved, {'stop', 'start'})


def main():
    global EXT, BASE, TABLE_NAMES
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--extension', type=Path, required=True)
    parser.add_argument('--extension-sha256', required=True)
    parser.add_argument('--fixture-operator', type=Path, required=True)
    parser.add_argument('--fixture-operator-sha256', required=True)
    parser.add_argument('--catalog', type=Path, required=True)
    parser.add_argument('--catalog-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run these guards only through authorized root SSH on test-env.')
    EXT = imported(args.extension, args.extension_sha256, 'extension_under_guard')
    BASE = imported(args.fixture_operator, args.fixture_operator_sha256, 'frozen_fixture_under_guard')
    catalog_bytes = args.catalog.read_bytes()
    if hashlib.sha256(catalog_bytes).hexdigest() != args.catalog_sha256 or args.catalog_sha256 != BASE.CATALOG_27_SHA:
        raise RuntimeError('The generated schema27 catalog input changed.')
    catalog = json.loads(catalog_bytes)
    if catalog['version'] != 27 or catalog['postgresql_major'] != 17:
        raise RuntimeError('The guard catalog is not schema27 on PostgreSQL17.')
    TABLE_NAMES = tuple(table['Name'] for table in catalog['catalog']['Tables'])
    if len(TABLE_NAMES) != len(set(TABLE_NAMES)) or len(TABLE_NAMES) != 35:
        raise RuntimeError('The guard catalog table inventory is invalid.')
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(GuardTests))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
