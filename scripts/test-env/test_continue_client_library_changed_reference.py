"""Focused continuation guards; execute exclusively on test-env."""

import copy
import importlib.util
import json
from pathlib import Path
import types
import unittest


SPEC = importlib.util.spec_from_file_location('continuation', Path(__file__).with_name('continue-client-library-changed-reference.py'))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class Parent:
    def allowed_get(self, *_args): return False
    def refresh_route(self): return '/emby/Items/93/Refresh?Recursive=true'


BASE = types.SimpleNamespace(Run=Parent, same=lambda a, b: json.dumps(a, sort_keys=True) == json.dumps(b, sort_keys=True),
    ANCHOR_FILE=Path('/synthetic/Movies/Anchor/Anchor.mp4'), ADDED=Path('/synthetic/Movies/Added'),
    ADDED_FILE=Path('/synthetic/Movies/Added/Added.mp4'), library_events=lambda record: [])
CONTINUATION = MODULE.continuation_type(BASE)


class ContinuationGuards(unittest.TestCase):
    def run_state(self):
        run = object.__new__(CONTINUATION)
        run.admin = types.SimpleNamespace(role='admin', proven=True, user_id='owned-admin')
        run.viewer = types.SimpleNamespace(role='viewer', proven=True, user_id=MODULE.USER)
        run.library_id, run.user_id = MODULE.LIBRARY, MODULE.USER
        run.metadata = {'Id': MODULE.ITEM, 'Name': MODULE.TITLE, 'Overview': MODULE.OVERVIEW}
        run.old_facts = {'3': {'Id': '3'}}
        run.removed_subtree = {'97', '98'}
        return run

    def catalog(self, movie_id='98', folder_id='97'):
        return {'94': {'Type': 'Folder'}, '95': {'Type': 'Folder'},
            '96': {'Type': 'Movie', 'Path': str(BASE.ANCHOR_FILE)},
            folder_id: {'Id': folder_id, 'Type': 'Folder', 'IsFolder': True, 'Path': str(BASE.ADDED)},
            movie_id: {'Id': movie_id, 'Type': 'Movie', 'Path': str(BASE.ADDED_FILE)}}

    def test_metadata_preserves_current_values_without_aliasing_or_server_fields(self):
        original = {'Id': '98', 'Type': 'Movie', 'Name': 'Original', 'Overview': 'Original overview',
            'LockedFields': ['Genres'], 'LockData': True, 'ProviderIds': {'test': 'current'},
            'DateCreated': '2026-09-11T23:00:00.0000000Z', 'UserData': {'Played': True},
            'DateModified': 'unsubmitted', 'Path': '/synthetic/source.mp4', 'MediaSources': []}
        before = copy.deepcopy(original)
        body = MODULE.metadata_body(original)
        self.assertEqual(body['Name'], MODULE.TITLE)
        self.assertEqual(body['Overview'], MODULE.OVERVIEW)
        self.assertEqual(body['DateCreated'], original['DateCreated'])
        self.assertTrue(body['LockData'])
        for key in ('UserData', 'DateModified', 'Path', 'MediaSources', 'Type'):
            self.assertNotIn(key, body)
        body['LockedFields'].append('Name')
        body['ProviderIds']['test'] = 'changed'
        self.assertEqual(original, before)

    def test_wrong_item_and_already_applied_changes_are_rejected(self):
        for change in ({'Id': '3'}, {'Type': 'Episode'}, {'Name': MODULE.TITLE}, {'Overview': MODULE.OVERVIEW}):
            with self.subTest(change=change):
                with self.assertRaises(RuntimeError):
                    MODULE.metadata_body(dict({'Id': '98', 'Type': 'Movie', 'Name': 'Original', 'Overview': 'Original'}, **change))

    def test_metadata_write_requires_exact_actor_route_and_saved_body(self):
        run = self.run_state()
        self.assertTrue(run.allowed_post(run.admin, '/emby/Items/98', copy.deepcopy(run.metadata), 'metadata-update'))
        self.assertFalse(run.allowed_post(run.viewer, '/emby/Items/98', run.metadata, 'metadata-update'))
        self.assertFalse(run.allowed_post(run.admin, '/emby/Items/3', run.metadata, 'metadata-update'))
        self.assertFalse(run.allowed_post(run.admin, '/emby/Items/98', dict(run.metadata, LockData=True), 'metadata-update'))
        run.user_id = 'existing-user'
        self.assertFalse(run.allowed_post(run.admin, '/emby/Items/98', run.metadata, 'metadata-update'))

    def test_setup_policy_password_and_global_refresh_are_excluded(self):
        run = self.run_state()
        for mutation, route in (('create-user', '/emby/Users/New'), ('policy', '/emby/Users/' + MODULE.USER + '/Policy'),
                                ('password', '/emby/Users/' + MODULE.USER + '/Password'),
                                ('create-library', '/emby/Library/VirtualFolders'), ('refresh-remove', '/emby/Library/Refresh')):
            self.assertFalse(run.allowed_post(run.admin, route, {}, mutation))

    def test_only_two_scoped_refresh_operations_are_available(self):
        run = self.run_state()
        for mutation in ('refresh-remove', 'refresh-readd'):
            self.assertTrue(run.allowed_post(run.admin, run.refresh_route(), {}, mutation))
        self.assertFalse(run.allowed_post(run.admin, run.refresh_route(), {}, 'refresh-setup'))
        run.library_id = '3'
        self.assertFalse(run.allowed_post(run.admin, run.refresh_route(), {}, 'refresh-remove'))

    def test_extra_detail_read_is_limited_to_admin_and_owned_movie(self):
        run = self.run_state()
        route = '/emby/Users/owned-admin/Items/98'
        self.assertTrue(run.allowed_get(run.admin, route, False))
        self.assertFalse(run.allowed_get(run.viewer, route, False))
        self.assertFalse(run.allowed_get(run.admin, '/emby/Users/owned-admin/Items/3', False))
        self.assertFalse(run.allowed_get(run.admin, route, True))

    def test_removal_requires_both_old_ids_and_their_paths_to_disappear(self):
        run = self.run_state()
        remaining = {key: value for key, value in self.catalog().items() if key not in {'97', '98'}}
        self.assertTrue(run.expected_catalog(remaining, 'remove'))
        self.assertFalse(run.expected_catalog(dict(remaining, **{'98': {'Type': 'Movie'}}), 'remove'))
        self.assertFalse(run.expected_catalog(dict(remaining, **{'99': {'Path': str(BASE.ADDED_FILE)}}), 'remove'))

    def test_readd_accepts_either_reused_or_new_id_but_keeps_the_anchor(self):
        run = self.run_state()
        self.assertTrue(run.expected_catalog(self.catalog(), 'readd'))
        self.assertTrue(run.expected_catalog(self.catalog('100', '99'), 'readd'))
        missing_anchor = self.catalog('100', '99')
        del missing_anchor['96']
        self.assertFalse(run.expected_catalog(missing_anchor, 'readd'))
        self.assertFalse(run.expected_catalog(self.catalog('100', '99'), 'metadata'))

    def test_closed_connection_cannot_satisfy_quiet_window(self):
        run = self.run_state()
        run.deadline = float('inf')
        run.ws = types.SimpleNamespace(open=False, record={'events': []}, mark=lambda _label: None)
        with self.assertRaises(RuntimeError):
            run.quiet()


if __name__ == '__main__':
    unittest.main()
