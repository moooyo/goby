"""Focused recorder authorization and evidence tests; run only on test-env."""

import importlib.util
from pathlib import Path
import types
import unittest


SPEC = importlib.util.spec_from_file_location('library_changed_reference',
    Path(__file__).with_name('capture-client-library-changed-reference.py'))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class RecorderGuards(unittest.TestCase):
    def run_state(self):
        run = object.__new__(MODULE.Run)
        run.admin = types.SimpleNamespace(role='admin', proven=True)
        run.viewer = types.SimpleNamespace(role='viewer', proven=True, user_id='new-viewer')
        run.user_id = 'new-viewer'
        run.library_id = 'new-library'
        run.credentials = {'password': 'new-fixture-password'}
        run.new_policy = {'EnableAllFolders': True, 'IsAdministrator': False}
        run.baseline = {'owned': True}
        run.extra_ids = set()
        run.extra_parent_ids = set()
        run.secrets = {'synthetic-owned-secret'}
        run.viewer.token = 'synthetic-owned-secret'
        run.viewer.device = 'synthetic-device'
        return run

    def test_password_and_policy_cannot_target_existing_users(self):
        run = self.run_state()
        for user_id in MODULE.OLD_USERS:
            with self.subTest(user_id=user_id):
                self.assertFalse(run.allowed_post(run.admin, '/emby/Users/' + user_id + '/Password',
                    {'Id': user_id, 'NewPw': run.credentials['password'], 'ResetPassword': False}, 'password'))
                self.assertFalse(run.allowed_post(run.admin, '/emby/Users/' + user_id + '/Policy', run.new_policy, 'policy'))
        self.assertTrue(run.allowed_post(run.admin, '/emby/Users/new-viewer/Policy', run.new_policy, 'policy'))
        self.assertFalse(run.allowed_post(run.viewer, '/emby/Users/new-viewer/Policy', run.new_policy, 'policy'))

    def test_creation_is_single_new_name_and_admin_only(self):
        run = self.run_state()
        body = {'Name': MODULE.VIEWER_NAME}
        self.assertFalse(run.allowed_post(run.admin, '/emby/Users/New', body, 'create-user'))
        run.user_id = None
        self.assertTrue(run.allowed_post(run.admin, '/emby/Users/New', body, 'create-user'))
        self.assertFalse(run.allowed_post(run.admin, '/emby/Users/New', {'Name': 'existing-user'}, 'create-user'))
        self.assertFalse(run.allowed_post(run.viewer, '/emby/Users/New', body, 'create-user'))

    def test_refresh_never_accepts_old_libraries_or_global_refresh(self):
        run = self.run_state()
        self.assertTrue(run.allowed_post(run.admin, run.refresh_route(), {}, 'refresh-add'))
        self.assertFalse(run.allowed_post(run.viewer, run.refresh_route(), {}, 'refresh-add'))
        self.assertFalse(run.allowed_post(run.admin, '/emby/Library/Refresh', {}, 'refresh-add'))
        for library_id in MODULE.OLD_LIBRARIES:
            run.library_id = library_id
            with self.subTest(library_id=library_id):
                route = run.route('/emby/Items/' + library_id + '/Refresh', MODULE.REFRESH)
                self.assertFalse(run.allowed_post(run.admin, route, {}, 'refresh-add'))

    def test_viewer_reads_cannot_expand_to_old_users(self):
        run = self.run_state()
        for user_id in MODULE.OLD_USERS:
            self.assertFalse(run.allowed_get(run.viewer, '/emby/Users/' + user_id + '/Items', False))
        self.assertTrue(run.allowed_get(run.viewer, '/emby/Users/new-viewer/Items?ParentId=new-library', False))
        self.assertFalse(run.allowed_get(run.viewer, '/emby/Users/new-viewer/Items', False))
        self.assertFalse(run.allowed_get(run.admin, '/emby/System/Configuration', True))
        self.assertTrue(run.allowed_get(run.admin, '/emby/System/Info', True))

    def test_wire_event_selection_preserves_order_and_duplicate_ids(self):
        data = {'FoldersAddedTo': ['42', '42', '41'], 'ItemsAdded': [], 'IsEmpty': False}
        event = {'direction': 'server-to-client', 'json': {'MessageType': 'LibraryChanged', 'Data': data}}
        assembled = {'direction': 'server-to-client', 'kind': 'message',
            'json': {'MessageType': 'LibraryChanged', 'Data': {'ItemsUpdated': ['44']}}}
        unrelated = {'direction': 'server-to-client', 'json': {'MessageType': 'UserDataChanged'}}
        outgoing = {'direction': 'client-to-server', 'json': event['json']}
        selected = MODULE.library_events({'events': [unrelated, event, outgoing, assembled]})
        self.assertEqual(selected, [event, assembled])
        self.assertEqual(selected[0]['json']['Data']['FoldersAddedTo'], ['42', '42', '41'])

    def test_sanitization_preserves_json_types_and_removes_nested_credentials(self):
        secret = 'synthetic-owned-secret'
        original = {'Data': {'ItemsAdded': ['9', '8', '9'], 'IsEmpty': False, 'Count': 3, 'Empty': None},
            'AccessToken': 'unknown-secret-value', 'nested': [{'password': secret, 'message': 'prefix ' + secret}]}
        sanitized = MODULE.sanitize(original, {secret})
        self.assertEqual(sanitized['Data'], original['Data'])
        self.assertEqual(sanitized['AccessToken'], '[redacted]')
        self.assertEqual(sanitized['nested'][0]['password'], '[redacted]')
        self.assertNotIn(secret, str(sanitized))
        self.assertEqual(original['nested'][0]['password'], secret)

    def test_public_capture_omits_all_raw_encoded_credential_copies(self):
        run = self.run_state()
        raw = {'request': {'path': '/embywebsocket?api_key=unknown-secret'},
            'response': {'status': 101, 'wireHeaders': 'Set-Cookie: unknown-secret'},
            'events': [{'payloadBase64': 'c2VjcmV0', 'text': 'unknown-secret', 'payloadLength': 6,
                'json': {'MessageType': 'LibraryChanged', 'Data': {'ItemsAdded': ['9', '8', '9']}, 'AccessToken': 'unknown-secret'}}]}
        public = run.public_capture(raw)
        self.assertNotIn('unknown-secret', str(public))
        self.assertNotIn('payloadBase64', str(public))
        self.assertNotIn('wireHeaders', str(public))
        self.assertEqual(public['events'][0]['payloadLength'], 6)
        self.assertEqual(public['events'][0]['json']['Data']['ItemsAdded'], ['9', '8', '9'])

    def test_removal_requires_stable_id_to_disappear_even_if_path_is_omitted(self):
        run = self.run_state()
        run.added_id = 'removed-movie'
        rows = {'anchor': {'Path': str(MODULE.ANCHOR_FILE), 'Type': 'Movie'}, 'removed-movie': {'Type': 'Movie'}}
        self.assertFalse(run.expected_catalog(rows, 'remove'))
        del rows['removed-movie']
        self.assertTrue(run.expected_catalog(rows, 'remove'))

    def test_closed_observer_aborts_before_filesystem_mutation(self):
        run = self.run_state()
        saved, mutations = [], []
        class ClosedCapture:
            def __init__(self, *_): self.open, self.record = False, {'events': []}
            def connect(self): pass
            def mark(self, _label): pass
            def close(self): pass
        run.transport = types.SimpleNamespace(Capture=ClosedCapture)
        run.save = lambda name, value, **_options: saved.append((name, value))
        run.new_catalog = lambda _label: {}
        run.stage_media = lambda phase: mutations.append(phase)
        with self.assertRaises(MODULE.CaptureError):
            run.capture_stage('add')
        self.assertEqual(mutations, [])
        public = next(value for name, value in saved if name == 'add-websocket.json')
        self.assertFalse(public['controlledCatalogEffectVerified'])


if __name__ == '__main__':
    unittest.main()
