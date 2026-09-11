#!/usr/bin/env python3
"""Pure profile guards using names and full fields from the pinned PG17 catalog.

Rows are synthetic guard data, never deployed-state evidence. No HTTP, SQL,
filesystem mutation or service operation occurs after source/catalog loading.
"""

from __future__ import annotations

import argparse
import copy
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
PROFILE = BASE = CATALOG = OP = COLUMNS = None
CATALOG_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d'
NOW, OLD = '2026-09-12T00:01:00Z', '2026-09-11T00:00:00Z'


def load_module(path, expected, name):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('A frozen guard input changed.')
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def row(table, **values):
    if not set(values) <= set(COLUMNS[table]):
        raise RuntimeError('A synthetic row uses fields absent from the actual catalog: ' + table)
    return {**dict.fromkeys(COLUMNS[table]), **values}


def model():
    state = {'schema': 27, 'admin_id': '1' * 32, 'viewer_id': '2' * 32, 'added_viewer': {'user_id': '3' * 32},
             'server_id': '4' * 32, 'runtime_sha256': '5' * 64, 'browser_sha256': '6' * 64,
             'credentials_binding': {'receipt_sha256': '7' * 64}}
    tables = {name: [] for name in COLUMNS}
    old_libraries = ['a' * 31 + str(index) for index in range(3)]
    for library_id in old_libraries:
        tables['libraries'].append(row('libraries', id=library_id, name='Existing synthetic library', collection_type='movies', created_at=OLD))
    for index in range(13):
        item_id = f'{index + 10:032x}'
        tables['items'].append(row('items', id=item_id, library_id=old_libraries[0], name='Existing synthetic item', sort_name='existing',
            type='Movie', is_folder=False, path='/owned-original/' + str(index), relative_path=str(index), created_at=OLD, updated_at=OLD))
        tables['theme_owner_ids'].append(row('theme_owner_ids', id=index + 2, item_id=item_id, virtual_root=False))
    tables['theme_owner_ids'].append(row('theme_owner_ids', id=1, item_id=None, virtual_root=True))
    for number, user_id in enumerate((state['admin_id'], state['viewer_id'], state['added_viewer']['user_id'])):
        tables['users'].append(row('users', id=user_id, name='Synthetic user ' + str(number), is_administrator=number == 0,
            is_disabled=False, has_password=True, policy={}, configuration={}, created_at=OLD, updated_at=OLD))
    tables['server_settings'].append(row('server_settings', key='server_id', value=state['server_id'], created_at=OLD, updated_at=OLD))
    tables['user_item_data'].append(row('user_item_data', user_id=state['viewer_id'], item_id=tables['items'][0]['id'],
        playback_position_ticks=1220000000, play_count=1, is_favorite=False, played=False, last_played_at=OLD, updated_at=OLD))
    tables['schema_migrations'] = [row('schema_migrations', version=value['version'], name=value['name'], applied_at=OLD) for value in CATALOG['migrations']]
    sequences = {entry['Name']: {'last_value': 100, 'is_called': True} for entry in CATALOG['catalog']['Sequences']}
    sequences.update(theme_owner_ids_id_seq={'last_value': 14, 'is_called': True}, devices_id_seq={'last_value': 10, 'is_called': True},
                     activity_entries_id_seq={'last_value': 20, 'is_called': True})
    relations = {entry['name']: {'owner': BASE.ROLE, 'oid': index + 100} for index, entry in enumerate(CATALOG['objects']) if entry['kind'] == 'relation'}
    before = {'schema': 27, 'runtime_sha256': state['runtime_sha256'], 'browser_sha256': state['browser_sha256'],
        'added_viewer_credentials': state['credentials_binding'], 'recovery': {}, 'database': {'unsupported': False,
        'catalog': CATALOG['objects'], 'metadata': {'database': BASE.ROLE, 'server_version_num': 170009, 'schemas': ['public'],
        'public_schema': {'oid': 2200}, 'captured_at': OLD, 'relations': relations, 'columns': COLUMNS}, 'tables': tables, 'sequences': sequences}}
    after = copy.deepcopy(before)
    current = after['database']['tables']
    library_id, root_id = 'b' * 32, 'c' * 32
    ids = {'library': library_id, 'positive_folder': f'{100:032x}', 'empty_folder': f'{101:032x}',
           **{key: f'{index + 102:032x}' for index, key in enumerate(PROFILE.FILES)}}
    current['libraries'].append(row('libraries', id=library_id, name=PROFILE.LIBRARY_NAME, collection_type='movies', created_at=NOW, last_scan_at=NOW))
    current['library_roots'].append(row('library_roots', id=root_id, library_id=library_id, path=PROFILE.ROOT, allowed_path=PROFILE.ROOT, relative_path='.'))
    new_items = [row('items', id=library_id, library_id=library_id, name=PROFILE.LIBRARY_NAME, sort_name=PROFILE.LIBRARY_NAME.lower(),
        type='CollectionFolder', is_folder=True, root_id=None, parent_id=None, path='', relative_path='', media=None, created_at=NOW, updated_at=NOW)]
    for key, relative in (('positive_folder', PROFILE.POSITIVE), ('empty_folder', PROFILE.EMPTY)):
        new_items.append(row('items', id=ids[key], library_id=library_id, root_id=root_id, parent_id=library_id, name=relative,
            sort_name=relative.lower(), type='Folder', is_folder=True, path=PROFILE.ROOT + '/' + relative, relative_path=relative,
            media=None, created_at=NOW, updated_at=NOW))
    media = {'special': {'files': {}}}
    for index, (key, (relative, kind, name)) in enumerate(PROFILE.FILES.items()):
        source = {'bytes': 12345, 'sha256': 'd' * 64, 'identity': {'device': 2049, 'inode': 300 + index, 'ctimeNs': 1234567}}
        media['special']['files']['Movies/' + relative] = source
        main = key in ('positive', 'empty')
        nfo = (PROFILE.POSITIVE if key == 'positive' else PROFILE.EMPTY) + '/movie.nfo' if main else ''
        if main:
            media['special']['files']['Movies/' + nfo] = {'sha256': 'e' * 64}
        parent = ids['positive_folder' if key == 'positive' else 'empty_folder'] if main else ids['positive']
        new_items.append(row('items', id=ids[key], library_id=library_id, root_id=root_id, parent_id=parent, name=name,
            sort_name=name.lower() if main else name, type=kind, is_folder=False, path=PROFILE.ROOT + '/' + relative, relative_path=relative,
            file_size=12345, file_identity='2049:' + str(300 + index), overview='', local_metadata={} if main else None,
            local_metadata_hash='e' * 64 if main else '', local_metadata_path=nfo,
            media={'ProbeVersion': 6, 'FileChangeTimeNs': 1234567, 'Size': 12345, 'Streams': [{'CodecType': 'video'}, {'CodecType': 'audio'}]},
            created_at=NOW, updated_at=NOW))
    current['items'].extend(new_items)
    for index, item in enumerate(new_items):
        current['theme_owner_ids'].append(row('theme_owner_ids', id=15 + index, item_id=item['id'], virtual_root=False))
        current['item_metadata_state'].append(row('item_metadata_state', item_id=item['id'],
            automatic={'Name': item['name'], 'SortName': item['sort_name']}, source_key={'Path': item['path'], 'RootId': item['root_id'],
            'ParentId': item['parent_id'], 'RelativePath': item['relative_path']}, overrides={}, locked_values={}, music_source={},
            revision=1, updated_at=NOW, last_edited_by=None, last_edited_at=None))
    current['extra_reserved_paths'] = [row('extra_reserved_paths', root_id=root_id, relative_path=path, is_directory=True) for path in PROFILE.MARKERS]
    current['item_extra_resources'] = [row('item_extra_resources', resource_item_id=ids[key], owner_item_id=ids['positive'], kind=kind, active=True)
                                      for key, kind in PROFILE.KINDS.items()]
    job_id = 'f' * 32
    current['scan_jobs'].append(row('scan_jobs', id=job_id, library_id=library_id, status='Completed', error='', force_probe=False,
        cancel_requested=False, scanned=6, added=6, updated=0, created_at=NOW, started_at=NOW, finished_at=NOW))
    auth = {}
    for index, role in enumerate(('admin', 'viewer')):
        session_id, token_hash = f'{500 + index:032x}', str(index + 1) * 64
        user_id = state['admin_id'] if role == 'admin' else state['added_viewer']['user_id']
        auth[role] = {'login_status': 200, 'logout_status': 204, 'exact_status': 401, 'token_sha256': token_hash, 'session_id': session_id}
        current['sessions'].append(row('sessions', id=session_id, user_id=user_id, token_hash='\\x' + token_hash,
            kind='admin' if role == 'admin' else 'emby', client_name='Goby Dashboard' if role == 'admin' else 'Synthetic Profile Recorder',
            device_id='goby-dashboard' if role == 'admin' else 'synthetic-profile-device', device_name='Web browser' if role == 'admin' else 'Linux Recorder',
            client_version='1.0', device_registry_id=None if role == 'admin' else 11, created_at=NOW, last_seen_at=NOW, revoked_at=NOW,
            expires_at='2026-09-13T00:00:00Z', client_capabilities={}))
    current['devices'].append(row('devices', id=11, reported_device_id='synthetic-profile-device', reported_name='Linux Recorder',
        app_name='Synthetic Profile Recorder', app_version='1.0', last_user_id=state['added_viewer']['user_id'], custom_name=None,
        deleted_at=None, revision=1, ip_address='127.0.0.1', created_at=NOW, last_seen_at=NOW))
    events = []
    for role, user_id, source in (('admin', state['admin_id'], 'native'), ('viewer', state['added_viewer']['user_id'], 'emby')):
        for action in ('session.login', 'session.revoked'):
            events.append((action, source, 'user', user_id, auth[role]['session_id'], 'session', auth[role]['session_id']))
    events.extend([('library.created', 'native', 'user', state['admin_id'], auth['admin']['session_id'], 'library', library_id),
        ('scan.requested', 'native', 'user', state['admin_id'], auth['admin']['session_id'], 'scan', job_id),
        ('scan.finished', 'system', 'system', '', '', 'scan', job_id)])
    for index, event in enumerate(events):
        values = dict(zip(('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id'), event))
        current['activity_entries'].append(row('activity_entries', **values, id=21 + index, created_at=NOW, severity='Info', revision=0,
            changed_fields=[], state='completed' if event[0] == 'scan.finished' else '',
            affected_count=1 if event[0] in ('library.created', 'session.revoked') else 0))
    after['database']['sequences'].update(theme_owner_ids_id_seq={'last_value': 23, 'is_called': True},
        devices_id_seq={'last_value': 11, 'is_called': True}, activity_entries_id_seq={'last_value': 27, 'is_called': True})
    receipt = {'library_id': library_id, 'root_id': root_id, 'job_id': job_id, 'authentication': auth,
        'device_id': 'synthetic-profile-device', 'client_name': 'Synthetic Profile Recorder',
        'window': {'before': '2026-09-12T00:00:00Z', 'after': '2026-09-12T00:02:00Z'}}
    return state, before, after, receipt, media


class ProfileGuards(unittest.TestCase):
    def setUp(self):
        for owner, name in ((subprocess, 'run'), (socket, 'socket'), (os, 'replace'), (BASE, 'create'), (BASE, 'postgres')):
            mocked = patch.object(owner, name, side_effect=AssertionError('Unexpected external effect.'))
            mocked.start()
            self.addCleanup(mocked.stop)

    def test_real_catalog_population_and_full_fields(self):
        self.assertEqual(len(COLUMNS), 35)
        self.assertIn('encoding_jobs', COLUMNS)
        self.assertNotIn('encoding_states', COLUMNS)
        self.assertFalse(PROFILE.ADDITIVE_TABLES - set(COLUMNS))
        _, before, after, _, _ = model()
        for snapshot in (before, after):
            self.assertEqual(set(snapshot['database']['tables']), set(COLUMNS))
            for name, rows in snapshot['database']['tables'].items():
                self.assertTrue(all(set(value) == set(COLUMNS[name]) for value in rows))

    def test_complete_owned_transition(self):
        state, before, after, receipt, media = model()
        proof = PROFILE.validate_transition(OP, before, after, state, receipt, media)
        self.assertEqual(proof['new_item_count'], 9)
        self.assertTrue(proof['old_rows_preserved'])

    def test_old_rows_userdata_config_and_unapproved_additions_are_rejected(self):
        changes = {
            'old_item': lambda t: t['items'][0].update(name='changed'),
            'old_userdata': lambda t: t['user_item_data'][0].update(play_count=2),
            'old_policy': lambda t: t['users'][0].update(policy={'EnableAllFolders': False}),
            'new_userdata': lambda t: t['user_item_data'].append(row('user_item_data', user_id='2' * 32, item_id='b' * 32)),
            'encoder': lambda t: t['encoding_jobs'].append(row('encoding_jobs')),
        }
        for name, mutate in changes.items():
            state, before, after, receipt, media = model()
            mutate(after['database']['tables'])
            with self.subTest(change=name), self.assertRaises(PROFILE.ProfileError):
                PROFILE.validate_transition(OP, before, after, state, receipt, media)

    def test_resource_owner_root_kind_path_and_population_are_strict(self):
        changes = {
            'owner': lambda t: t['item_extra_resources'][0].update(owner_item_id=t['items'][-1]['id']),
            'kind': lambda t: t['item_extra_resources'][0].update(kind='trailer'),
            'inactive': lambda t: t['item_extra_resources'][0].update(active=False),
            'missing_resource': lambda t: t['item_extra_resources'].pop(),
            'extra_marker': lambda t: t['extra_reserved_paths'].append(row('extra_reserved_paths', root_id='c' * 32, relative_path='unexpected', is_directory=True)),
            'nested': lambda t: t['items'][-1].update(relative_path=PROFILE.POSITIVE + '/featurettes/nested/Hidden Nested.mp4'),
            'foreign_root': lambda t: t['items'][-1].update(root_id='0' * 32),
            'wrong_probe': lambda t: t['items'][-1]['media'].update(ProbeVersion=5),
            'inherited_nfo': lambda t: t['items'][-1].update(local_metadata={'Name': 'Owner title'}),
        }
        for name, mutate in changes.items():
            state, before, after, receipt, media = model()
            mutate(after['database']['tables'])
            with self.subTest(change=name), self.assertRaises((PROFILE.ProfileError, BASE.FixtureError)):
                PROFILE.validate_transition(OP, before, after, state, receipt, media)

    def test_session_audit_and_sequence_ownership(self):
        changes = {
            'token': lambda t: t['sessions'][-1].update(token_hash='\\x' + '0' * 64),
            'not_revoked': lambda t: t['sessions'][-1].update(revoked_at=None),
            'foreign_user': lambda t: t['sessions'][-1].update(user_id='1' * 32),
            'device': lambda t: t['devices'][-1].update(reported_device_id='another-device'),
            'audit': lambda t: t['activity_entries'][-1].update(resource_id='unowned-job'),
            'missing_audit': lambda t: t['activity_entries'].pop(),
            'wrong_scan': lambda t: t['scan_jobs'][-1].update(scanned=7),
        }
        for name, mutate in changes.items():
            state, before, after, receipt, media = model()
            mutate(after['database']['tables'])
            with self.subTest(change=name), self.assertRaises(PROFILE.ProfileError):
                PROFILE.validate_transition(OP, before, after, state, receipt, media)
        state, before, after, receipt, media = model()
        after['database']['sequences']['devices_id_seq']['last_value'] += 1
        with self.assertRaises(PROFILE.ProfileError):
            PROFILE.validate_transition(OP, before, after, state, receipt, media)

    def test_missing_catalog_table_column_and_private_state_are_rejected(self):
        changes = {
            'table': lambda s: s['database']['tables'].pop('encoding_jobs'),
            'column': lambda s: s['database']['tables']['items'][0].pop('overview'),
            'runtime': lambda s: s.update(runtime_sha256='0' * 64),
            'credential': lambda s: s.update(added_viewer_credentials={}),
            'schema': lambda s: s.update(schema=26),
        }
        for name, mutate in changes.items():
            state, before, after, receipt, media = model()
            mutate(after)
            with self.subTest(change=name), self.assertRaises(PROFILE.ProfileError):
                PROFILE.validate_transition(OP, before, after, state, receipt, media)


def main():
    global PROFILE, BASE, CATALOG, OP, COLUMNS
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('profile', 'fixture-operator', 'catalog'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name + '-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run these pure guards only through authorized root SSH.')
    PROFILE = load_module(args.profile, args.profile_sha256, 'nonempty_profile_under_guard')
    BASE = load_module(args.fixture_operator, args.fixture_operator_sha256, 'frozen_empty_fixture_support')
    raw = args.catalog.read_bytes()
    if args.catalog_sha256 != CATALOG_SHA or hashlib.sha256(raw).hexdigest() != CATALOG_SHA:
        raise RuntimeError('The actual generated schema27 catalog changed.')
    CATALOG = BASE.precise_json(raw)
    COLUMNS = BASE.baseline_table_columns(CATALOG)
    OP = types.SimpleNamespace(ROLE=BASE.ROLE, canonical_json=BASE.canonical_json, equal_json=BASE.equal_json,
        trusted_schema_baseline=lambda *_: CATALOG, schema27_binding=lambda _: {}, schema26_binding=lambda _: {}, schema25_binding=lambda _: {},
        baseline_table_columns=BASE.baseline_table_columns, validate_theme_state=BASE.validate_theme_state,
        added_viewer_credentials=lambda state: state['credentials_binding'])
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ProfileGuards))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
