#!/usr/bin/env python3
"""Run eight memory-only guard groups against one explicitly pinned operator.

Execute only through authorized root SSH. The source is read once before the
effect fence; tests never contact Emby, enter a namespace, mutate files, launch
processes, or change account/library state. A passed suite is not a live scan.
"""

from __future__ import annotations

import argparse
from collections import Counter
import contextlib
import copy
import fcntl
import hashlib
import http.client
import io
import json
import linecache
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, patch
from urllib.parse import urlencode

sys.dont_write_bytecode = True
TESTED = None
SOURCE = b''
ACTIVE = []


def audit(event, _arguments):
    if ACTIVE and (event == 'open' or event.startswith(('socket.', 'subprocess.', 'os.', 'fcntl.'))):
        ACTIVE[-1].violations.append(event)
        raise AssertionError('A real external effect escaped its memory stub: ' + event)


class Fence(contextlib.ExitStack):
    def __enter__(self):
        super().__enter__()
        self.violations = []
        ACTIVE.append(self)
        targets = ((subprocess, ('run', 'Popen', 'call', 'check_output', 'check_call')),
                   (http.client, ('HTTPConnection', 'HTTPSConnection')),
                   (socket, ('socket', 'create_connection', 'getaddrinfo')),
                   (fcntl, ('flock', 'lockf')),
                   (signal, ('setitimer', 'signal')),
                   (time, ('sleep',)),
                   (os, ('open', 'fdopen', 'stat', 'lstat', 'fstat', 'readlink', 'read', 'write', 'close',
                         'fsync', 'fchmod', 'fchown', 'mkdir', 'makedirs', 'unlink', 'remove', 'rmdir',
                         'rename', 'replace', 'link', 'symlink', 'chmod', 'chown', 'truncate', 'kill', 'killpg',
                         'listdir', 'scandir', 'system', 'popen', 'fork', 'execve', 'umask')),
                   (Path, ('open', 'read_bytes', 'read_text', 'write_bytes', 'write_text', 'stat', 'lstat',
                           'exists', 'is_file', 'is_dir', 'is_symlink', 'mkdir', 'iterdir', 'rglob', 'glob',
                           'unlink', 'rmdir', 'rename', 'replace', 'chmod', 'touch')))
        for owner, names in targets:
            for name in names:
                if hasattr(owner, name):
                    def deny(*_args, _label=name, **_kwargs):
                        self.violations.append(_label)
                        raise AssertionError('Unstubbed external effect: ' + _label)
                    self.enter_context(patch.object(owner, name, deny))
        # unittest may inspect source while formatting a failed assertion. That
        # must not open arbitrary files while the fence is active.
        self.enter_context(patch.object(linecache, 'getlines', lambda filename, module_globals=None:
                                       SOURCE.decode().splitlines(True) if filename == TESTED.__file__ else []))
        return self

    def __exit__(self, *args):
        try:
            result = super().__exit__(*args)
        finally:
            ACTIVE.pop()
        if self.violations:
            raise AssertionError('Real external effects attempted: ' + ', '.join(self.violations))
        return result


class MemoryPreparer:
    MEDIA = Path('/opt/goby-fixtures/client-m3e')
    OWNER = Path('/opt/goby-test/exec-work-m3e/reference-owner.json')
    REPORT = Path('/opt/goby-test/exec-work-m3e/reference-report.json')
    BROWSER = Path('/opt/goby-test/exec-work-m3e/reference-browser.json')

    def __init__(self, events):
        self.events, self.saved = events, {}
        self.fail_intent = False
        self.on_save = None

    def save(self, path, value):
        if self.fail_intent and path.name.endswith('-intent.json'):
            raise TESTED.Failure('Synthetic intent publication failure.')
        if path in self.saved:
            raise TESTED.Failure('Synthetic exclusive publication collision.')
        self.events.append(('save', path.name))
        self.saved[path] = copy.deepcopy(value)
        if self.on_save:
            self.on_save(path, value)

    def library_body(self, name):
        return {'Name': 'M3e Reference ' + name, 'CollectionType': TESTED.LIBRARIES[name],
                'RefreshLibrary': False, 'Paths': [str(self.MEDIA / name)],
                'LibraryOptions': {'PathInfos': [{'Path': str(self.MEDIA / name)}],
                    'EnableRealtimeMonitor': False, 'EnableChapterImageExtraction': False,
                    'ExtractChapterImagesDuringLibraryScan': False, 'SaveLocalMetadata': False,
                    'SaveLocalThumbnailSets': False, 'SaveSubtitlesWithMedia': False,
                    'MetadataSavers': [], 'TypeOptions': [{'Type': 'Movie', 'MetadataFetchers': [], 'ImageFetchers': []}]}}

    def verify_library(self, row, name):
        expected = self.library_body(name)
        TESTED.require(row['Name'] == expected['Name'] and row['CollectionType'] == expected['CollectionType'] and
                       row['Locations'] == expected['Paths'] and row['LibraryOptions'] == expected['LibraryOptions'],
                       'Synthetic strict template mismatch.')

    def digest(self, path):
        return 'report-digest' if path == self.REPORT else 'browser-digest'

    def same_service(self, _owner):
        return None


class MemoryTransport:
    def __init__(self, events, replies):
        self.events, self.replies, self.sent = events, list(replies), []

    def __call__(self, host, port, timeout):
        assert (host, port, timeout) == ('127.0.0.1', 18097, 8)
        parent = self

        class Connection:
            def request(self, method, route, body, headers):
                parent.events.append(('http', method, route))
                parent.sent.append((method, route, body, copy.deepcopy(headers)))

            def getresponse(self):
                reply = parent.replies.pop(0)
                if isinstance(reply, Exception):
                    raise reply
                status, value, content_type = reply
                raw = b'' if value is None else TESTED.encoded(value) if content_type == 'application/json' else value.encode()

                class Response:
                    def __init__(self):
                        self.status = status

                    def getheader(self, name):
                        return content_type if name == 'Content-Type' else str(len(raw)) if name == 'Content-Length' else None

                    def read(self, limit):
                        assert limit == TESTED.MAX_BODY + 1
                        return raw

                return Response()

            def close(self):
                parent.events.append(('close',))

        return Connection()


def recorder():
    events = []
    op = MemoryPreparer(events)
    accounts = {key: {'userId': str(index) * 32, 'username': 'fixture-' + key,
                      'password': 'memory-password-' + key}
                for index, key in enumerate(('admin', 'viewer', 'viewer2'), 1)}
    owner = {'serverId': 'memory-server', 'reportSha256': 'report-digest', 'browserSha256': 'browser-digest',
             'serviceIdentity': {'networkNamespace': 'net:[123]'}}
    args = types.SimpleNamespace(script_sha256=TESTED.sha(SOURCE), operator_sha256=TESTED.sha(b'preparer'),
                                 owner_sha256=TESTED.sha(TESTED.encoded(owner)), manifest_sha256='a' * 64)
    control = {'deviceId': 'memory-new-recorder'}
    rec = TESTED.Recorder(args, op, owner, {'accounts': accounts}, control, {'expectedItems': []}, {'old': 'preserved'})
    rec.check = Mock()
    rec.token, rec.proven = 'only-new-recorder-token', True
    rec.secrets.add(rec.token)
    rec.baseline = {'libraries': {'old-movies': {'Name': 'M3e Reference Movies', 'Locations': [str(op.MEDIA / 'Movies')]}},
                    'users': {}, 'configuration': {}, 'itemsByUser': {}}
    return rec, op, events


def login_ack(rec):
    return {'AccessToken': 'acknowledged-new-token', 'ServerId': rec.owner['serverId'],
            'User': {'Id': rec.user['userId'], 'Name': rec.user['username'], 'Policy': {'IsAdministrator': True}},
            'SessionInfo': {'DeviceId': rec.device, 'UserId': rec.user['userId']}}


class Guards(unittest.TestCase):
    def setUp(self):
        self.fence = Fence()
        self.fence.__enter__()
        self.local = contextlib.ExitStack()

    def tearDown(self):
        self.local.close()
        self.fence.__exit__(None, None, None)

    def transport(self, events, replies):
        transport = MemoryTransport(events, replies)
        self.local.enter_context(patch.object(TESTED.http.client, 'HTTPConnection', transport))
        self.local.enter_context(patch.object(TESTED.signal, 'setitimer', lambda *_: None))
        return transport

    def test_route_and_template_boundaries_reject_old_global_policy_and_foreign_paths(self):
        rec, op, events = recorder()
        transport = self.transport(events, [])
        valid = TESTED.library_body(op, 'Movies')
        self.assertEqual(valid['Paths'], [str(TESTED.ROOT / 'Movies')])
        self.assertFalse(valid['LibraryOptions']['SaveLocalMetadata'])
        acknowledged = {'Name': valid['Name'], 'CollectionType': valid['CollectionType'],
                        'ItemId': 'new-movies', 'Locations': valid['Paths'], 'LibraryOptions': valid['LibraryOptions']}
        self.assertEqual(TESTED.verify_new_library(op, acknowledged, 'Movies'), 'new-movies')
        for field in ('SaveLocalMetadata', 'EnableRealtimeMonitor', 'EnableChapterImageExtraction'):
            changed = copy.deepcopy(acknowledged)
            changed['LibraryOptions'][field] = True
            with self.subTest(option=field), self.assertRaises(TESTED.Failure):
                TESTED.verify_new_library(op, changed, 'Movies')
        for route, body, mutation in (
            ('/emby/Library/Refresh', {}, 'refresh-Movies'),
            ('/emby/Items/old-movies/Refresh', {}, 'refresh-Movies'),
            ('/emby/Users/' + rec.user['userId'] + '/Policy', {}, 'policy'),
            ('/emby/Library/VirtualFolders', {**valid, 'Paths': ['/tmp/foreign']}, 'create-Movies'),
            ('/emby/Library/VirtualFolders', {**valid, 'LibraryOptions': {**valid['LibraryOptions'], 'SaveLocalMetadata': True}}, 'create-Movies')):
            with self.subTest(route=route, mutation=mutation), self.assertRaises(TESTED.Failure):
                rec.request('denied', 'POST', route, body, mutation=mutation)
        self.assertEqual(transport.sent, [])
        self.assertEqual(op.saved, {})
        with self.assertRaises(TESTED.Failure):
            TESTED.identifier('../old/Refresh')
        with self.assertRaises(TESTED.Failure):
            TESTED.decode(b'{"Id":"one","Id":"two"}')

    def test_intent_precedes_mutation_and_uncertain_outcome_cannot_retry(self):
        rec, op, events = recorder()
        transport = self.transport(events, [TimeoutError('memory timeout')])
        with patch.object(TESTED, 'media_snapshot', return_value=({}, rec.media)):
            op.fail_intent = True
            with self.assertRaises(TESTED.Failure):
                rec.request('create-first', 'POST', '/emby/Library/VirtualFolders', TESTED.library_body(op, 'Movies'), mutation='create-Movies')
            self.assertEqual(transport.sent, [])
            op.fail_intent = False
            with self.assertRaises(TESTED.Failure):
                rec.request('create-once', 'POST', '/emby/Library/VirtualFolders', TESTED.library_body(op, 'Movies'), mutation='create-Movies')
            self.assertEqual(len(transport.sent), 1)
            self.assertEqual(events[0][0], 'save')
            self.assertIn('create-Movies', rec.intents)
            with self.assertRaises(TESTED.Failure):
                rec.request('create-retry', 'POST', '/emby/Library/VirtualFolders', TESTED.library_body(op, 'Movies'), mutation='create-Movies')
            self.assertEqual(len(transport.sent), 1)
        rec.new_libraries['Movies'] = 'new-only-movies'
        transport.replies.append((204, None, 'application/json'))
        with patch.object(TESTED, 'media_snapshot', return_value=({}, rec.media)):
            self.assertEqual(rec.request('refresh-once', 'POST', rec.refresh_route('Movies'), {}, mutation='refresh-Movies')[0], 204)
        self.assertIn('Recursive=true', transport.sent[-1][1])
        self.assertIn('/new-only-movies/Refresh?', transport.sent[-1][1])
        self.assertTrue(any(event[1].endswith('refresh-once-intent.json') for event in events if event[0] == 'save'))

    def test_existing_name_or_path_and_unacknowledged_creation_are_not_adopted(self):
        for collision in ({'Name': 'M3e Auxiliary Movies', 'Locations': ['/unrelated']},
                          {'Name': 'foreign-name', 'Locations': [str(TESTED.ROOT / 'Movies')]}):
            rec, _op, _events = recorder()
            rec.virtuals = Mock(return_value={**rec.baseline['libraries'], 'foreign': collision})
            rec.request = Mock(side_effect=AssertionError('No create may be dispatched.'))
            with self.assertRaises(TESTED.Failure):
                rec.create_libraries()
            rec.request.assert_not_called()
        rec, op, events = recorder()
        transport = self.transport(events, [(204, None, 'application/json')])
        rec.virtuals = Mock(return_value=copy.deepcopy(rec.baseline['libraries']))
        with patch.object(TESTED, 'media_snapshot', return_value=({}, rec.media)), self.assertRaises(TESTED.Failure):
            rec.create_libraries()
        self.assertEqual(len(transport.sent), 1)
        self.assertEqual(rec.new_libraries, {})
        self.assertEqual(rec.intents, {'create-Movies'})

    def test_login_ack_is_private_before_proof_and_logout_uses_only_that_token(self):
        rec, op, events = recorder()
        rec.token, rec.proven = None, False
        proof_states = []
        op.on_save = lambda path, _value: proof_states.append(rec.proven) if path.name == 'recorder-login.json' else None
        ack = login_ack(rec)
        transport = self.transport(events, [(200, ack, 'application/json'), (204, None, 'application/json'),
                                            (401, 'Access token is invalid.', 'text/plain')])
        rec.request('login', 'POST', '/emby/Users/AuthenticateByName',
                    {'Username': rec.user['username'], 'Pw': rec.user['password']}, login=True, mutation='login')
        self.assertEqual(proof_states, [False])
        self.assertTrue(rec.proven)
        rec.logout()
        self.assertTrue(rec.revoked)
        self.assertEqual([request[3].get('X-Emby-Token') for request in transport.sent], [None, ack['AccessToken'], ack['AccessToken']])
        self.assertNotIn(rec.user['password'], json.dumps(op.saved[TESTED.PRIVATE / '0001-login-intent.json']))
        rec, op, events = recorder()
        rec.token, rec.proven = None, False
        ack = login_ack(rec)
        ack['ServerId'] = 'foreign-server'
        transport = self.transport(events, [(200, ack, 'application/json')])
        with self.assertRaises(TESTED.Failure):
            rec.request('login', 'POST', '/emby/Users/AuthenticateByName',
                        {'Username': rec.user['username'], 'Pw': rec.user['password']}, login=True, mutation='login')
        self.assertIn(TESTED.PRIVATE / 'recorder-login.json', op.saved)
        with self.assertRaises(TESTED.Failure):
            rec.logout()
        self.assertEqual(len(transport.sent), 1)

    def test_failure_finally_preserves_baseline_then_revokes_the_new_recorder(self):
        rec, op, events = recorder()
        baseline = copy.deepcopy(rec.baseline)
        rec.baseline, rec.token, rec.proven = None, None, False
        rec.snapshot = Mock(return_value=baseline)
        rec.create_libraries = Mock(side_effect=TESTED.Failure('Synthetic bounded create failure.'))
        transport = self.transport(events, [(200, {'Id': rec.owner['serverId'], 'Version': '4.9.5.0'}, 'application/json'),
                                            (200, login_ack(rec), 'application/json'), (204, None, 'application/json'),
                                            (401, 'Access token is invalid.', 'text/plain')])
        with patch.object(TESTED, 'media_snapshot', return_value=({}, rec.media)), contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(rec.run(), 1)
        self.assertTrue(rec.revoked)
        self.assertEqual([request[1] for request in transport.sent][-2:], ['/emby/Sessions/Logout', '/emby/Sessions'])
        report = op.saved[TESTED.EXPORT / 'report.json']
        self.assertEqual(report['result'], 'retained_for_review')
        self.assertTrue(report['originalLibrariesMetadataUserDataConfigurationPolicyPreserved'])
        self.assertTrue(report['newRecorderTokenRevoked'])
        self.assertNotIn('acknowledged-new-token', TESTED.encoded(report).decode())

    def test_owner_service_namespace_and_media_drift_stop_before_mutation(self):
        rec, op, events = recorder()
        transport = self.transport(events, [])
        rec.check = types.MethodType(TESTED.Recorder.check, rec)
        mapping = {Path(TESTED.__file__).absolute(): SOURCE, TESTED.OPERATOR: b'preparer',
                   op.OWNER: TESTED.encoded(rec.owner), TESTED.CONTROL / 'OWNER.json': TESTED.encoded(rec.control)}
        with (patch.object(TESTED, 'protected_bytes', side_effect=lambda path: mapping[path]),
              patch.object(TESTED.os, 'readlink', return_value='net:[123]')):
            rec.check()
            for changed in ('owner', 'service', 'namespace', 'media'):
                with self.subTest(changed=changed), contextlib.ExitStack() as local:
                    if changed == 'owner':
                        local.enter_context(patch.dict(mapping, {op.OWNER: b'{}'}))
                    elif changed == 'service':
                        local.enter_context(patch.object(op, 'same_service', side_effect=TESTED.Failure('Synthetic service drift.')))
                    elif changed == 'namespace':
                        local.enter_context(patch.object(TESTED.os, 'readlink', return_value='net:[foreign]'))
                    local.enter_context(patch.object(TESTED, 'media_snapshot', return_value=({}, {'old': 'changed'})))
                    with self.assertRaises(TESTED.Failure):
                        rec.request('drift', 'POST', '/emby/Library/VirtualFolders', TESTED.library_body(op, 'Movies'), mutation='create-Movies')
        self.assertEqual(transport.sent, [])
        self.assertEqual(op.saved, {})
        with patch.object(TESTED, 'protected_bytes', return_value=b'{}'), self.assertRaises(TESTED.Failure):
            TESTED.media_snapshot(op, rec.owner, 'f' * 64)

    def test_full_roster_and_restricted_user_subsets_are_preserved_without_policy_writes(self):
        rec, _op, _events = recorder()
        roster = [{'Id': account['userId'], 'Name': account['username'], 'Configuration': {},
                   'Policy': {'IsAdministrator': key == 'admin'}} for key, account in rec.browser['accounts'].items()]
        extra = {'Id': '4' * 32, 'Name': 'owned-tv-only', 'Configuration': {},
                 'Policy': {'IsAdministrator': False, 'EnableAllFolders': False}}
        rec.get = Mock(return_value=roster + [extra])
        retained = rec.user_rows('roster')
        self.assertEqual(len(retained), 4)
        rec.get.return_value = roster + [extra, extra]
        with self.assertRaises(TESTED.Failure):
            rec.user_rows('duplicate-extra')
        rec.get.return_value = roster[1:] + [extra]
        with self.assertRaises(TESTED.Failure):
            rec.user_rows('missing-admin')
        rec.old_ids = ['old-' + str(index) for index in range(6)]
        rec.old_item_facts = {key: ('/opt/goby-fixtures/client-m3e/TV/' + key + '.mp4', 'Episode') for key in rec.old_ids}
        subset = [{'Id': key, 'Path': rec.old_item_facts[key][0], 'Type': 'Episode', 'UserData': {'PlayCount': 0}}
                  for key in rec.old_ids[:3]]
        rec.get.return_value = {'Items': subset, 'TotalRecordCount': 3}
        self.assertEqual(len(rec.user_items('tv-subset', extra['Id'], rec.old_ids)), 3)
        rec.get.return_value = {'Items': [], 'TotalRecordCount': 0}
        self.assertEqual(rec.user_items('no-access', extra['Id'], rec.old_ids), {})
        with self.assertRaises(TESTED.Failure):
            rec.user_items('admin-incomplete', rec.user['userId'], rec.old_ids)
        rec.get.return_value = {'Items': [{**subset[0], 'Path': '/unowned/media.mp4'}], 'TotalRecordCount': 1}
        with self.assertRaises(TESTED.Failure):
            rec.user_items('foreign-path', extra['Id'], rec.old_ids)
        rec.baseline['users'] = retained
        rec.baseline['itemsByUser'][extra['Id']] = {row['Id']: row for row in subset}
        changed = copy.deepcopy(rec.baseline)
        changed['users'][extra['Id']]['Policy']['EnableAllFolders'] = True
        with self.assertRaises(TESTED.Failure):
            rec.preserve(changed)
        changed = copy.deepcopy(rec.baseline)
        changed['itemsByUser'][extra['Id']][subset[0]['Id']]['UserData']['PlayCount'] = 1
        with self.assertRaises(TESTED.Failure):
            rec.preserve(changed)

    def test_scan_requires_expected_primary_paths_types_and_streams(self):
        rec, _op, _events = recorder()
        rec.manifest['expectedItems'] = [{'kind': 'Movie', 'media': 'Movies/Seed/movie.mp4'}]
        item = {'Id': 'new-movie', 'Type': 'Movie', 'Path': str(TESTED.ROOT / 'Movies/Seed/movie.mp4'),
                'MediaSources': [{'MediaStreams': [{'Type': 'Video'}, {'Type': 'Audio'}]}]}
        self.assertEqual(rec.primary_ready('Movies', {'new-movie': item}), {'Movies/Seed/movie.mp4': 'new-movie'})
        self.assertIsNone(rec.primary_ready('Movies', {}))
        self.assertIsNone(rec.primary_ready('Movies', {'new-movie': {**item, 'MediaSources': []}}))
        with self.assertRaises(TESTED.Failure):
            rec.primary_ready('Movies', {'new-movie': {**item, 'Type': 'Video'}})
        rec.new_libraries['Movies'] = 'new-movies'
        rec.get = Mock(return_value={'Items': [{**item, 'Path': '/old-library/movie.mp4'}], 'TotalRecordCount': 1})
        with self.assertRaises(TESTED.Failure):
            rec.members('Movies', 0)
        rec.get.return_value = {'Items': [item, item], 'TotalRecordCount': 2}
        with self.assertRaises(TESTED.Failure):
            rec.members('Movies', 1)


def main():
    global TESTED, SOURCE
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operator', type=Path)
    parser.add_argument('--operator-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise SystemExit('Run guard verification only through authorized root SSH.')
    SOURCE = args.operator.read_bytes()
    operator_sha = hashlib.sha256(SOURCE).hexdigest()
    if not re.fullmatch(r'[0-9a-f]{64}', args.operator_sha256) or operator_sha != args.operator_sha256:
        raise SystemExit('The explicitly pinned operator digest differs.')
    guard_sha = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    TESTED = types.ModuleType('auxiliary_reference_guard_target')
    TESTED.__file__ = str(args.operator.absolute())
    sys.addaudithook(audit)
    with Fence():
        exec(compile(SOURCE, TESTED.__file__, 'exec'), TESTED.__dict__)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(Guards)
    output = io.StringIO()
    result = unittest.TextTestRunner(stream=output, verbosity=2).run(suite)
    print(json.dumps({'suite': 'auxiliary-reference-memory-guards', 'status': 'passed' if result.wasSuccessful() else 'failed',
                      'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors),
                      'skips': len(result.skipped), 'operator_sha256': operator_sha, 'guard_sha256': guard_sha,
                      'fixtures': 'memory-only-with-external-effect-fence', 'live_http_executed': False,
                      'failure_details': output.getvalue() if not result.wasSuccessful() else None}))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
