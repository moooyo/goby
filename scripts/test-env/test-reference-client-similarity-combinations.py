#!/usr/bin/env python3
"""Run eight memory-only guard groups for an explicitly pinned recorder.

Invoke only through authorized root SSH on test-env. Synthetic response rows
exercise the operation's boundaries; they do not establish Emby update behavior.
The source is read before an audit hook and explicit external-effect fence are
installed. No live HTTP, database, process, or fixture mutation is permitted.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import hashlib
import http.client
import io
import json
import linecache
import os
from pathlib import Path
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
import urllib.parse
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
TESTED, SOURCE = None, b''
ACTIVE = []


def audit(event, _arguments):
    if ACTIVE and (event == 'open' or event.startswith(('socket.', 'subprocess.')) or event in {
        'os.system', 'os.fork', 'os.exec', 'os.spawn', 'os.posix_spawn', 'os.remove', 'os.rename',
        'os.rmdir', 'os.mkdir', 'os.listdir', 'os.scandir', 'os.chmod', 'os.chown', 'os.link', 'os.symlink'}):
        ACTIVE[-1].violations.append(event)
        raise AssertionError('Unexpected real external effect: ' + event)


class Fence(contextlib.ExitStack):
    def __enter__(self):
        super().__enter__()
        import fcntl
        self.violations = []
        ACTIVE.append(self)
        targets = ((builtins, ('open',)), (io, ('open', 'open_code', 'FileIO')),
                   (subprocess, ('run', 'Popen', 'call', 'check_call', 'check_output')),
                   (socket, ('socket', 'create_connection', 'getaddrinfo')),
                   (http.client, ('HTTPConnection', 'HTTPSConnection')),
                   (fcntl, ('flock', 'lockf', 'fcntl', 'ioctl')),
                   (signal, ('signal', 'setitimer')),
                   (time, ('sleep',)),
                   (os, ('open', 'fdopen', 'stat', 'lstat', 'fstat', 'read', 'write', 'readlink', 'close',
                         'fsync', 'mkdir', 'makedirs', 'unlink', 'remove', 'rmdir', 'rename', 'replace',
                         'link', 'symlink', 'chmod', 'chown', 'kill', 'killpg', 'listdir', 'scandir',
                         'system', 'popen', 'fork', 'execve', 'umask')),
                   (Path, ('open', 'read_bytes', 'read_text', 'write_bytes', 'write_text', 'stat', 'lstat',
                           'exists', 'is_file', 'is_dir', 'is_symlink', 'mkdir', 'iterdir', 'rglob', 'glob',
                           'unlink', 'rmdir', 'rename', 'replace', 'chmod', 'touch')))
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue

                def deny(*_args, _name=name, **_kwargs):
                    self.violations.append(_name)
                    raise AssertionError('Unstubbed external effect: ' + _name)

                self.enter_context(patch.object(owner, name, deny))
        self.enter_context(patch.object(linecache, 'checkcache', lambda filename=None: None))
        self.enter_context(patch.object(linecache, 'getlines', lambda filename, module_globals=None:
                                       SOURCE.decode().splitlines(True) if filename == TESTED.__file__ else []))
        return self

    def __exit__(self, *arguments):
        try:
            result = super().__exit__(*arguments)
        finally:
            ACTIVE.pop()
        if self.violations:
            raise AssertionError('Attempted external effects: ' + ', '.join(self.violations))
        return result


class MemoryOperator:
    MEDIA = Path('/opt/goby-fixtures/client-m3e')
    OWNER = Path('/opt/goby-test/exec-work-m3e/reference-owner.json')
    REPORT = Path('/opt/goby-test/exec-work-m3e/reference-report.json')
    BROWSER = Path('/opt/goby-test/exec-work-m3e/reference-browser.json')
    LOCK = Path('/opt/goby-test/exec-work-m3e/reference.lock')
    LIBRARIES = {'Movies': 'movies', 'TV': 'tvshows', 'Music': 'music'}

    def __init__(self, events):
        self.events, self.saved, self.original = events, {}, {}
        self.fail_intent, self.on_save = False, None

    def save(self, path, value):
        TESTED.require(path not in self.saved, 'Synthetic exclusive evidence collision.')
        if self.fail_intent and path.name.endswith('-intent.json'):
            raise TESTED.Failure('Synthetic durable intent failure.')
        self.events.append(('save', path.name))
        self.saved[path] = copy.deepcopy(value)
        if self.on_save:
            self.on_save(path, value)

    def read_private(self, path):
        TESTED.require(path == self.REPORT, 'An unapproved private input was read.')
        return copy.deepcopy(self.original)

    def digest(self, path):
        TESTED.require(path in (self.REPORT, self.BROWSER), 'An unapproved digest input was read.')
        return 'report-digest' if path == self.REPORT else 'browser-digest'

    def same_service(self, _owner):
        self.events.append(('service-check',))

    def verify_library(self, row, name):
        TESTED.require(row.get('Name') == 'M3e Reference ' + name and
                       row.get('Locations') == [str(self.MEDIA / name)], 'An original library escaped its scope.')


class MemoryHelper:
    def __init__(self, events):
        self.events, self.media = events, {'old14': 'unchanged', 'aux43': 'unchanged'}

    def media_snapshot(self, _operator, _owner, manifest_sha):
        TESTED.require(manifest_sha == TESTED.MANIFEST_SHA, 'An unapproved manifest was requested.')
        self.events.append(('media-check',))
        return {}, copy.deepcopy(self.media)

    @staticmethod
    def rows(body, maximum=256):
        TESTED.require(isinstance(body, dict) and isinstance(body.get('Items'), list) and
                       type(body.get('TotalRecordCount')) is int and body['TotalRecordCount'] == len(body['Items']) <= maximum,
                       'Synthetic catalog is incomplete.')
        result = {TESTED.identifier(row.get('Id')): row for row in body['Items']}
        TESTED.require(len(result) == len(body['Items']), 'Synthetic catalog repeats an identity.')
        return result

    @staticmethod
    def verify_new_library(_op, row, name):
        TESTED.require(row.get('Name') == 'M3e Auxiliary ' + name and row.get('Locations') == [str(TESTED.MEDIA / name)],
                       'An auxiliary library escaped its scope.')


class Transport:
    def __init__(self, events, replies):
        self.events, self.replies, self.sent = events, list(replies), []

    def __call__(self, host, port, timeout):
        if host != '127.0.0.1' or port != 18097 or not 0 < timeout <= 6:
            raise AssertionError('The recorder selected another transport boundary.')
        transport = self

        class Connection:
            def request(self, method, route, body, headers):
                transport.events.append(('http', method, route))
                transport.sent.append({'method': method, 'route': route, 'body': body, 'headers': copy.deepcopy(headers)})

            def getresponse(self):
                if not transport.replies:
                    raise AssertionError('An unscripted HTTP request was attempted.')
                reply = transport.replies.pop(0)
                if isinstance(reply, Exception):
                    raise reply
                content_type = reply.get('content_type', 'application/json')
                raw = reply.get('raw')
                if raw is None:
                    raw = b'' if reply.get('body') is None else TESTED.encoded(reply['body'])

                class Response:
                    status = reply['status']

                    def getheader(self, name):
                        return content_type if name == 'Content-Type' else reply.get('length', str(len(raw))) if name == 'Content-Length' else None

                    def read(self, limit):
                        if limit != TESTED.MAX_BODY + 1:
                            raise AssertionError('The response read limit changed.')
                        return raw

                return Response()

            def close(self):
                transport.events.append(('close',))

        return Connection()


def detail(item_id):
    name, path = TESTED.TARGETS[item_id]
    result = {'Id': item_id, 'Name': name, 'Type': 'Movie', 'Path': str(TESTED.MEDIA / path),
              'SortName': name, 'ForcedSortName': name, 'OriginalTitle': '', 'Overview': '',
              'ProductionYear': 2000, 'PremiereDate': None, 'EndDate': None, 'CommunityRating': None,
              'CriticRating': None, 'OfficialRating': '', 'CustomRating': '', 'ProviderIds': {},
              'Genres': ['Drama'] if item_id in ('35', '40') else [],
              'GenreItems': [{'Id': 53, 'Name': 'Drama'}] if item_id in ('35', '40') else [], 'Tags': [], 'TagItems': [],
              'Studios': [], 'People': [], 'LockedFields': [], 'LockData': False, 'Taglines': [],
              'ProductionLocations': [], 'PreferredMetadataLanguage': '', 'PreferredMetadataCountryCode': '',
              'IndexNumber': None, 'ParentIndexNumber': None, 'SortIndexNumber': None, 'SortParentIndexNumber': None,
              'DisplayOrder': '', 'Status': '', 'DateCreated': '2026-09-11T00:00:00Z', 'ETag': 'baseline',
              'UserData': {'PlaybackPositionTicks': 0, 'PlayCount': 0, 'Played': False, 'IsFavorite': False},
              'MediaSources': [{'Id': 'source-' + item_id, 'Path': str(TESTED.MEDIA / path), 'Size': 1234}],
              'UnreturnedEditableClaims': False}
    actor = {'Id': '51', 'Name': 'CommonActor', 'Type': 'Actor', 'Role': 'Preserved role'}
    director = {'Id': '52', 'Name': 'CommonDirector', 'Type': 'Director'}
    if item_id == '35':
        result.update(Genres=['Drama', 'Adventure'], GenreItems=[{'Id': 53, 'Name': 'Drama'}, {'Id': 54, 'Name': 'Adventure'}],
                      Studios=[{'Id': 50, 'Name': 'SharedStudio'}],
                      Tags=['SharedOne'], TagItems=[{'Id': 55, 'Name': 'SharedOne'}], People=[actor, director])
    elif item_id == '49':
        result['People'] = [actor]
    return result


def recorder(variant='eligibility'):
    TESTED.select_variant(variant)
    events = []
    op, helper = MemoryOperator(events), MemoryHelper(events)
    accounts = {key: {'userId': letter * 32, 'username': 'fixture-' + key, 'password': 'synthetic-password-' + key}
                for key, letter in zip(('admin', 'viewer', 'viewer2'), 'abc')}
    owner = {'serverId': 'synthetic-server', 'reportSha256': 'report-digest', 'browserSha256': 'browser-digest',
             'serviceIdentity': {'networkNamespace': 'net:[123]'}}
    index = {'existingUserIds': [letter * 32 for letter in 'abcde'],
             'newLibraryIds': {'Movies': '20', 'TV': '57', 'Music': '66'}}
    args = types.SimpleNamespace(script_sha256=TESTED.sha(SOURCE), snapshot_helper='/opt/goby-test/accepted-snapshot.py', variant=variant)
    rec = TESTED.Recorder(args, (helper, op, owner, {'accounts': accounts}, index, copy.deepcopy(helper.media)),
                          {'deviceId': 'goby-m3e-similarity-combinations-' + 'f' * 32})
    rec.check = Mock()
    rec.token, rec.proven = 'synthetic-only-new-recorder-token', True
    rec.secrets.add(rec.token)
    rec.details = {key: detail(key) for key in TESTED.TARGETS}
    return rec, op, helper, events


def acknowledgement(rec):
    return {'AccessToken': 'synthetic-new-login-token', 'ServerId': rec.owner['serverId'],
            'User': {'Id': rec.user['userId'], 'Name': rec.user['username'], 'Policy': {'IsAdministrator': True}},
            'SessionInfo': {'DeviceId': rec.device, 'UserId': rec.user['userId']}}


def mutated(case):
    value = copy.deepcopy(case['original'])
    value.update(copy.deepcopy(case['expected']))
    value['ETag'] = 'accepted-update'
    return value


class Guards(unittest.TestCase):
    def setUp(self):
        self.stack = contextlib.ExitStack()
        self.stack.enter_context(Fence())
        self.output = self.stack.enter_context(contextlib.redirect_stdout(io.StringIO()))

    def tearDown(self):
        try:
            self.assertNotIn('synthetic-only-new-recorder-token', self.output.getvalue())
            self.assertNotIn('synthetic-new-login-token', self.output.getvalue())
            self.assertNotIn('synthetic-password-', self.output.getvalue())
        finally:
            self.stack.close()

    def transport(self, events, replies):
        transport = Transport(events, replies)
        self.stack.enter_context(patch.object(TESTED.http.client, 'HTTPConnection', transport))
        self.stack.enter_context(patch.object(TESTED.signal, 'setitimer', lambda *_: None))
        return transport

    def test_complete_edit_clones_and_target_classification_preserve_unrelated_fields(self):
        for invalid in ('', 'v2', 'ranking/../eligibility', None):
            with self.subTest(variant=invalid), self.assertRaises(TESTED.Failure):
                TESTED.select_variant(invalid)
        roots = []
        for variant in ('eligibility', 'ranking'):
            rec, op, helper, _events = recorder(variant)
            self.assertEqual(TESTED.CONTROL, Path('/opt/goby-test/exec-work-m3e/reference-similarity-combinations-v3-' + variant))
            self.assertEqual(TESTED.PRIVATE, TESTED.CONTROL / 'private')
            self.assertEqual(TESTED.EXPORT, TESTED.CONTROL / 'export')
            self.assertEqual(TESTED.MARKER, 'goby-reference-similarity-combinations-m3e-v3-' + variant)
            roots.append(TESTED.CONTROL)
            TESTED.select_variant('ranking' if variant == 'eligibility' else 'eligibility')
            with self.assertRaises(TESTED.Failure):
                TESTED.Recorder(rec.args, (helper, op, rec.owner, rec.browser, rec.index, rec.media), rec.control)
            TESTED.select_variant(variant)
            for item_id in ('40', '49'):
                original = rec.details[item_id]
                body = TESTED.edit_body(original)
                self.assertEqual(set(body), {'Id', *TESTED.EDIT_FIELDS})
                self.assertTrue(all(TESTED.equal(body[key], original[key]) for key in TESTED.EDIT_FIELDS))
                body['Genres'].append('Unowned')
                body['ProviderIds']['Unowned'] = 'change'
                self.assertNotIn('Unowned', original['Genres'])
                self.assertEqual(original['ProviderIds'], {})
                case = rec.prepare_case(item_id)
                expected_changed = {'Studios'} if item_id == '40' else set()
                if variant == 'ranking' or item_id == '49':
                    expected_changed.update(('Genres', 'GenreItems'))
                self.assertEqual(set(case['changed']), expected_changed)
                self.assertEqual(set(case['expected']), expected_changed)
                self.assertEqual(case['variant'], variant)
                original_body = TESTED.edit_body(original)
                self.assertEqual({key: value for key, value in case['body'].items() if key not in ('Genres', 'Studios')},
                                 {key: value for key, value in original_body.items() if key not in ('Genres', 'Studios')})
                for field in expected_changed - {'GenreItems'}:
                    self.assertEqual(case['expected'][field], case['body'][field])
                self.assertEqual(case['body']['People'], original['People'])
                self.assertEqual(case['body']['Tags'], original['Tags'])
                self.assertEqual(case['body']['TagItems'], original['TagItems'])
                self.assertNotIn('GenreItems', case['body'])
                self.assertEqual(case['body']['Genres'], ['Drama', 'Adventure'] if variant == 'ranking' else ['Drama'])
                if item_id == '40':
                    self.assertEqual(case['body']['Studios'], [{'Id': 50, 'Name': 'SharedStudio'}])
                    self.assertIsNot(case['body']['Studios'], rec.details['35']['Studios'])
                    self.assertIsNot(case['body']['Studios'][0], rec.details['35']['Studios'][0])
                else:
                    self.assertEqual(case['body']['Studios'], [])
                if 'GenreItems' in expected_changed:
                    genres = rec.details['35']['GenreItems'] if variant == 'ranking' else rec.details['35']['GenreItems'][:1]
                    self.assertEqual(case['expected']['GenreItems'], genres)
                    self.assertIsNot(case['expected']['GenreItems'][0], rec.details['35']['GenreItems'][0])
                self.assertEqual(rec.classify(case, original)[0], 'original')
                accepted = mutated(case)
                self.assertEqual(rec.classify(case, accepted)[0], 'mutated')
                for field in set(original) - set(case['changed']) - TESTED.AUTOMATIC_FIELDS | {'InjectedForeignField'}:
                    foreign = copy.deepcopy(accepted)
                    foreign[field] = {'foreign': 'change'}
                    with self.subTest(variant=variant, item_id=item_id, field=field):
                        try:
                            self.assertEqual(rec.classify(case, foreign)[0], 'foreign-or-uncertain')
                        except TESTED.Failure:
                            pass
                for wrong_genres in ([], [{'Id': 99, 'Name': 'Drama'}], [{'Id': 53, 'Name': 'Foreign'}]):
                    foreign = copy.deepcopy(accepted)
                    foreign['GenreItems'] = wrong_genres
                    with self.subTest(variant=variant, item_id=item_id, genres=wrong_genres):
                        self.assertEqual(rec.classify(case, foreign)[0], 'foreign-or-uncertain')
                foreign = copy.deepcopy(accepted)
                foreign['Genres'] = ['Foreign']
                self.assertEqual(rec.classify(case, foreign)[0], 'foreign-or-uncertain')
                foreign = copy.deepcopy(accepted)
                foreign['Studios'] = [{'Id': 99, 'Name': 'SharedStudio'}]
                self.assertEqual(rec.classify(case, foreign)[0], 'foreign-or-uncertain')
                for field in TESTED.AUTOMATIC_FIELDS:
                    current = copy.deepcopy(original)
                    current[field] = 'recorded-etag' if field.lower() == 'etag' else '2026-09-11T01:00:00.1234567Z'
                    state, changes = rec.classify(case, current)
                    self.assertEqual(state, 'original')
                    self.assertIn(field, changes)
                for field, value in (('Id', '9'), ('Path', '/old/movie.mp4'), ('Type', 'Video'), ('Name', 'Other')):
                    foreign = dict(original, **{field: value})
                    with self.subTest(binding=field), self.assertRaises(TESTED.Failure):
                        TESTED.verify_target(foreign, item_id)
            for field, position in (('GenreItems', 0), ('GenreItems', 1), ('Studios', 0)):
                seed = copy.deepcopy(rec.details['35'])
                rec.details['35'][field][position]['Id'] = 99
                with self.subTest(variant=variant, seed_field=field, seed_position=position), self.assertRaises(TESTED.Failure):
                    rec.prepare_case('40')
                rec.details['35'] = seed
        self.assertNotEqual(*roots)

    def test_post_intent_precedes_send_and_unknown_outcomes_never_repeat(self):
        rec, op, helper, events = recorder()
        case = rec.prepare_case('40')
        transport = self.transport(events, [TimeoutError('Synthetic response loss.')])
        with self.assertRaises(TESTED.Failure):
            rec.authorize_post(case, False)
        dispatched = TESTED.decode(transport.sent[0]['body'])
        self.assertEqual(dispatched['Studios'], [{'Id': 50, 'Name': 'SharedStudio'}])
        self.assertEqual(dispatched['Genres'], ['Drama'])
        self.assertEqual(dispatched['TagItems'], [])
        self.assertEqual(dispatched['Tags'], [])
        self.assertTrue(case['mutationSent'])
        self.assertIn('modify-40', rec.sent)
        send = next(index for index, event in enumerate(events) if event[:1] == ('http',))
        names = [event[1] for event in events[:send] if event[0] == 'save']
        self.assertIn('modify-40-durable-intent.json', names)
        self.assertTrue(any(name.endswith('-modify-40-intent.json') for name in names))
        with self.assertRaises(TESTED.Failure):
            rec.request('duplicate', 'POST', '/emby/Items/40', case['body'], 'modify-40')
        self.assertEqual(len(transport.sent), 1)
        denied = (('/emby/Items/9', case['body']), ('/emby/Items/35', case['body']),
                  ('/emby/Library/Refresh', {}), ('/emby/Users/admin/Policy', {}))
        for index, (route, body) in enumerate(denied):
            mutation = 'denied-' + str(index)
            rec.allowed_posts[mutation] = {'route': route, 'body': body}
            with self.subTest(route=route), self.assertRaises(TESTED.Failure):
                rec.request('denied', 'POST', route, body, mutation)
        self.assertEqual(len(transport.sent), 1)
        for failure in ('intent', 'media'):
            another, other_op, other_helper, other_events = recorder()
            if failure == 'intent':
                other_op.fail_intent = True
            else:
                other_helper.media['aux43'] = 'changed'
            with self.subTest(failure=failure), self.assertRaises(TESTED.Failure):
                another.authorize_post(another.prepare_case('49'), False)
            self.assertFalse(any(event[0] == 'http' for event in other_events))
            self.assertEqual(len(transport.sent), 1)

    def test_login_ack_is_private_before_proof_and_foreign_identity_cannot_be_used(self):
        for mismatch in (None, 'server', 'user', 'name', 'admin', 'device', 'session-user'):
            rec, op, _helper, events = recorder()
            rec.token, rec.proven = None, False
            ack = acknowledgement(rec)
            if mismatch == 'server':
                ack['ServerId'] = 'foreign-server'
            elif mismatch == 'user':
                ack['User']['Id'] = 'e' * 32
            elif mismatch == 'name':
                ack['User']['Name'] = 'foreign-user'
            elif mismatch == 'admin':
                ack['User']['Policy']['IsAdministrator'] = False
            elif mismatch == 'device':
                ack['SessionInfo']['DeviceId'] = 'browser-device'
            elif mismatch == 'session-user':
                ack['SessionInfo']['UserId'] = 'e' * 32
            proof_at_save = []
            op.on_save = lambda path, _value: proof_at_save.append(rec.proven) if path.name == 'recorder-login.json' else None
            transport = self.transport(events, [{'status': 200, 'body': ack}])
            call = lambda: rec.request('login', 'POST', '/emby/Users/AuthenticateByName',
                                       {'Username': rec.user['username'], 'Pw': rec.user['password']}, 'login')
            if mismatch is None:
                call()
                self.assertTrue(rec.proven)
            else:
                with self.subTest(mismatch=mismatch), self.assertRaises(TESTED.Failure):
                    call()
                with self.assertRaises(TESTED.Failure):
                    rec.request('forbidden-research', 'GET', '/emby/Users')
                try:
                    rec.logout()
                except TESTED.Failure:
                    pass
                self.assertFalse(rec.revoked)
            self.assertEqual(proof_at_save, [False])
            self.assertEqual(op.saved[TESTED.PRIVATE / 'recorder-login.json'], ack)
            self.assertEqual(len(transport.sent), 1)

    def test_transport_bounds_and_reserved_exact_token_text_denial(self):
        self.assertEqual(TESTED.NORMAL_REQUESTS, {'eligibility': 37, 'ranking': 43})
        self.assertEqual(TESTED.SIMILAR_READS, {'eligibility': 1, 'ranking': 4})
        self.assertEqual(TESTED.MAX_REQUESTS, 45)
        for reply in ({'status': 302}, {'status': 200, 'raw': b'x', 'content_type': 'text/plain', 'length': '2'},
                      {'status': 200, 'raw': b'x', 'content_type': 'text/plain', 'length': str(TESTED.MAX_BODY + 1)},
                      {'status': 200, 'raw': b'\xff', 'content_type': 'text/plain'},
                      {'status': 200, 'raw': b'{"x":1,"x":2}'},
                      TimeoutError('Synthetic read timeout.')):
            rec, _op, _helper, events = recorder()
            self.transport(events, [reply])
            with self.subTest(reply=type(reply).__name__), self.assertRaises(TESTED.Failure):
                rec.request('bounded', 'GET', '/emby/Users')
        rec, _op, _helper, events = recorder()
        transport = self.transport(events, [{'status': 204}, {'status': 401, 'raw': b'Access token is invalid or expired.', 'content_type': 'text/plain'}])
        rec.count = TESTED.MAX_REQUESTS - 2
        with self.assertRaises(TESTED.Failure):
            rec.request('no-research-budget', 'GET', '/emby/Users')
        self.assertEqual(transport.sent, [])
        token = rec.token
        rec.logout()
        self.assertTrue(rec.revoked)
        self.assertEqual(rec.count, TESTED.MAX_REQUESTS)
        self.assertEqual([entry['method'] for entry in transport.sent], ['POST', 'GET'])
        self.assertTrue(all(entry['headers']['X-Emby-Token'] == token for entry in transport.sent))
        with self.assertRaises(TESTED.Failure):
            rec.request('logout-denied', 'GET', '/emby/Sessions')

    def test_two_successful_cases_restore_complete_original_bodies_and_leave_seed_read_only(self):
        for variant in ('eligibility', 'ranking'):
            rec, op, _helper, _events = recorder(variant)
            current = copy.deepcopy(rec.details)
            bodies, timeline = [], []

            def read(label, item_id):
                timeline.append(('detail', label))
                return copy.deepcopy(current[item_id])

            def request(label, method, route, body=None, mutation=None):
                timeline.append((method, label))
                if method == 'POST':
                    item_id = route.rsplit('/', 1)[1]
                    self.assertIn(item_id, ('40', '49'))
                    self.assertIn(TESTED.PRIVATE / (mutation + '-durable-intent.json'), op.saved)
                    self.assertEqual(body['People'], rec.details[item_id]['People'])
                    bodies.append((mutation, copy.deepcopy(body)))
                    current[item_id] = copy.deepcopy(rec.details[item_id]) if mutation.startswith('restore-') else mutated(rec.active)
                    current[item_id]['ETag'] = mutation
                    return 204, None, len(timeline)
                self.assertTrue(route.startswith('/emby/Items/35/Similar?'))
                self.assertIsNone(body)
                self.assertEqual(rec.classify(rec.active, current[rec.active['itemId']])[0], 'mutated')
                return 200, {'Items': [{'Id': rec.active['itemId']}], 'TotalRecordCount': 1}, len(timeline)

            rec.detail, rec.request = read, request
            for item_id in ('40', '49'):
                rec.run_case(item_id)
            reads_per_case = TESTED.SIMILAR_READS[variant]
            expected_timeline = []
            for item_id in ('40', '49'):
                prefix = 'case-' + item_id
                expected_timeline.extend([('detail', prefix + '-prewrite'), ('POST', 'modify-' + item_id), ('detail', prefix + '-accepted')])
                expected_timeline.extend(('GET', f'{prefix}-similar-{number:02d}') for number in range(1, reads_per_case + 1))
                expected_timeline.extend([('detail', prefix + '-prerestore'), ('POST', 'restore-' + item_id), ('detail', prefix + '-restored')])
            self.assertEqual(timeline, expected_timeline)
            labels = [label for method, label in timeline if method == 'GET']
            self.assertEqual(len(labels), len(set(labels)))
            self.assertEqual(len(labels), 2 * reads_per_case)
            # The unchanged plan has 23 other attempts: login, two eight-read
            # snapshots, three baseline details, seed preservation, and logout.
            self.assertEqual(23 + len(timeline), TESTED.NORMAL_REQUESTS[variant])
            self.assertEqual([name for name, _ in bodies], ['modify-40', 'restore-40', 'modify-49', 'restore-49'])
            self.assertEqual(bodies[0][1]['Studios'], [{'Id': 50, 'Name': 'SharedStudio'}])
            self.assertEqual(bodies[0][1]['Genres'], ['Drama', 'Adventure'] if variant == 'ranking' else ['Drama'])
            self.assertEqual(bodies[2][1]['Genres'], ['Drama', 'Adventure'] if variant == 'ranking' else ['Drama'])
            self.assertEqual(bodies[1][1], TESTED.edit_body(rec.details['40']))
            self.assertEqual(bodies[3][1], TESTED.edit_body(rec.details['49']))
            self.assertEqual(current['35'], rec.details['35'])
            self.assertEqual(rec.restored, {'40': True, '49': True})
            self.assertIsNone(rec.active)
            self.assertEqual(len(rec.cases), 2)
            for case in rec.cases:
                self.assertEqual(case['variant'], variant)
                self.assertTrue(case['similar']['mutatedBusinessStateConfirmedBeforeAndAfterReads'])
                self.assertEqual([row['observation'] for row in case['similar']['observations']], list(range(1, reads_per_case + 1)))

    def test_recovery_only_restores_exact_expected_state_once(self):
        for state in ('original', 'mutated', 'foreign', 'unknown', 'restore-already-sent', 'restore-response-lost'):
            rec, _op, _helper, _events = recorder()
            case = rec.prepare_case('49')
            rec.active, case['mutationSent'] = case, True
            current = copy.deepcopy(case['original']) if state == 'original' else mutated(case)
            if state == 'foreign':
                current['Overview'] = 'Independent unowned edit'
            case['restoreSent'] = state == 'restore-already-sent'
            reads = []
            writes = []

            def read(_label, _item_id):
                reads.append(_label)
                if state == 'unknown':
                    raise TimeoutError('Synthetic unavailable readback.')
                return copy.deepcopy(case['original'] if writes else current)

            def write(_label, method, route, body=None, mutation=None):
                self.assertEqual(method, 'POST')
                self.assertEqual(route, '/emby/Items/49')
                self.assertEqual(body, TESTED.edit_body(case['original']))
                writes.append(mutation)
                if state == 'restore-response-lost':
                    raise TimeoutError('Synthetic restoration acknowledgement loss.')
                return 204, None, 1

            rec.detail, rec.request = read, write
            if state in ('foreign', 'unknown', 'restore-already-sent', 'restore-response-lost'):
                with self.subTest(state=state), self.assertRaises((TESTED.Failure, TimeoutError)):
                    rec.recover()
            else:
                rec.recover()
                self.assertTrue(rec.restored['49'])
            self.assertEqual(len(writes), int(state in ('mutated', 'restore-response-lost')))
            if state == 'restore-response-lost':
                self.assertTrue(case['restoreSent'])
                rec.detail = lambda *_: mutated(case)
                with self.assertRaises(TESTED.Failure):
                    rec.recover()
                self.assertEqual(len(writes), 1)

    def test_all_five_user_snapshots_preserve_fixed_visible_subsets_and_userdata(self):
        rec, op, _helper, _events = recorder()
        old = {'9': ('Movies/movie.mp4', 'Movie'), '10': ('Music/track.flac', 'Audio'), '11': ('Music/track.mp3', 'Audio'),
               '17': ('TV/episode-2.mp4', 'Episode'), '18': ('TV/episode-1.mp4', 'Episode'), '19': ('TV/episode-3.mp4', 'Episode')}
        op.original = {'items': [{'Id': key, 'Path': str(op.MEDIA / value[0]), 'Type': value[1]} for key, value in old.items()],
                       'libraries': [{'ItemId': key} for key in ('old-movies', 'old-tv', 'old-music')]}
        users = [{'Id': letter * 32, 'Name': 'Owned ' + letter, 'Configuration': {'Retain': letter}, 'Policy': {'EnableAllFolders': letter < 'd'}} for letter in 'abcde']
        originals = [{'ItemId': key, 'Name': 'M3e Reference ' + name, 'Locations': [str(op.MEDIA / name)]}
                     for key, name in zip(('old-movies', 'old-tv', 'old-music'), ('Movies', 'TV', 'Music'))]
        new = [{'ItemId': key, 'Name': 'M3e Auxiliary ' + name, 'Locations': [str(TESTED.MEDIA / name)]}
               for name, key in rec.index['newLibraryIds'].items()]
        population = {}
        for number, user in enumerate(users):
            selected = set(old) if number < 3 else {'17', '18', '19'} if number == 3 else set()
            rec.index.setdefault('originalVisibleItemIdsByUser', {})[user['Id']] = sorted(selected)
            population[user['Id']] = {key: {'Id': key, 'Path': str(op.MEDIA / old[key][0]), 'Type': old[key][1],
                                                'UserData': {'PlayCount': number, 'CustomPreciseValue': 'retained'}} for key in selected}
            if number < 3:
                population[user['Id']].update({key: detail(key) for key in ('40', '49')})

        def get(_label, route):
            if route == '/emby/Users':
                return copy.deepcopy(users)
            if route == '/emby/Library/VirtualFolders/Query':
                return {'Items': copy.deepcopy(originals + new), 'TotalRecordCount': 6}
            if route == '/emby/System/Configuration':
                return {'Keep': 'configuration'}
            self.assertTrue(route.startswith('/emby/Users/') and '/Items?' in route)
            user_id = route.split('/')[3]
            self.assertIn(user_id, population)
            self.assertIn('Ids=10%2C11%2C17%2C18%2C19%2C9%2C40%2C49', route)
            data = copy.deepcopy(list(population[user_id].values()))
            return {'Items': data, 'TotalRecordCount': len(data)}

        rec.get = get
        rec.baseline = rec.snapshot('before')
        self.assertEqual(len(rec.baseline['itemsByUser']['d' * 32]), 3)
        self.assertEqual(len(rec.baseline['itemsByUser']['e' * 32]), 0)
        rec.preserve()
        self.assertTrue(rec.preserved)
        for target in ('policy', 'old-data', 'modified-movie-data', 'foreign-path', 'roster'):
            changed = copy.deepcopy(rec.baseline)
            if target == 'policy':
                changed['users']['d' * 32]['Policy']['EnableAllFolders'] = True
            elif target == 'roster':
                del changed['users']['e' * 32]
            else:
                key = '40' if target == 'modified-movie-data' else '9'
                row = changed['itemsByUser'][rec.user['userId']][key]
                if target == 'foreign-path':
                    row['Path'] = '/old-unowned/movie.mp4'
                else:
                    row['UserData']['PlayCount'] = 99
            rec.snapshot = lambda _label, value=changed: copy.deepcopy(value)
            rec.final_snapshot_attempted, rec.preserved = False, False
            op.saved.pop(TESTED.PRIVATE / 'preservation-automatic-differences.json', None)
            with self.subTest(target=target), self.assertRaises(TESTED.Failure):
                rec.preserve()
            self.assertFalse(rec.preserved)

    def test_finally_order_and_exact_input_namespace_checks_fail_closed(self):
        rec, op, helper, events = recorder()
        rec.request = lambda *_args, **_kwargs: (200, {}, 1)
        rec.snapshot = lambda _label: {'retained': 'baseline'}

        def failed_case(_item_id):
            events.append(('mutation-failed',))
            raise TESTED.Failure('Synthetic uncertain mutation.')

        rec.run_case = failed_case
        rec.detail = lambda _label, item_id: detail(item_id)
        rec.recover = lambda: events.append(('recover',))
        rec.preserve = lambda: events.append(('preserve',))
        rec.preserve_seed = lambda: events.append(('seed-check',))
        logout_deadlines = []

        def logout():
            events.append(('logout',))
            logout_deadlines.append(rec.deadline - time.monotonic())
            rec.revoked = True

        rec.logout = logout
        self.assertEqual(rec.run(), 1)
        sequence = [event[0] for event in events]
        self.assertLess(sequence.index('recover'), sequence.index('preserve'))
        self.assertLess(sequence.index('preserve'), sequence.index('media-check'))
        self.assertLess(sequence.index('media-check'), sequence.index('logout'))
        self.assertTrue(0 < logout_deadlines[0] <= TESTED.LOGOUT_SECONDS)
        for variant in ('eligibility', 'ranking'):
            worker_rec, worker_op, worker_helper, _events = recorder(variant)
            binding = {'scriptSha256': worker_rec.args.script_sha256, 'snapshotHelper': worker_rec.args.snapshot_helper,
                       'variant': variant, 'helperSha256': TESTED.HELPER_SHA, 'operatorSha256': TESTED.OPERATOR_SHA,
                       'ownerSha256': TESTED.OWNER_SHA, 'indexReportSha256': TESTED.INDEX_SHA, 'manifestSha256': TESTED.MANIFEST_SHA}
            control = {'marker': TESTED.MARKER, 'path': str(TESTED.CONTROL), 'inputs': binding,
                       'supervisor': {'pid': 4444, 'startTicks': 'fixed-parent'}, 'media': copy.deepcopy(worker_rec.media),
                       'deviceId': worker_rec.device}
            facts = types.SimpleNamespace(st_dev=1, st_ino=2, st_uid=0, st_gid=0, st_mode=stat.S_IFREG | 0o600,
                                          st_nlink=1, st_size=0, st_mtime_ns=1, st_ctime_ns=1)
            worker_op.canonical = Mock(return_value=facts)
            worker_op.process_identity = lambda pid: {'pid': pid, 'startTicks': 'fixed-parent'}
            argv = [TESTED.__file__, '--variant', variant, '--script-sha256', worker_rec.args.script_sha256,
                    '--snapshot-helper', worker_rec.args.snapshot_helper, '--worker', '--lock-fd', '9']
            with contextlib.ExitStack() as worker_stubs:
                worker_stubs.enter_context(patch.object(TESTED.sys, 'argv', argv))
                parser = types.SimpleNamespace(add_argument=Mock(), parse_args=Mock(return_value=types.SimpleNamespace(
                    variant=variant, script_sha256=worker_rec.args.script_sha256,
                    snapshot_helper=worker_rec.args.snapshot_helper, worker=True, lock_fd=9)))
                worker_stubs.enter_context(patch.object(TESTED.argparse, 'ArgumentParser', return_value=parser))
                worker_stubs.enter_context(patch.object(TESTED, 'load_inputs', return_value=(worker_helper, worker_op,
                    worker_rec.owner, worker_rec.browser, worker_rec.index, worker_rec.media)))
                worker_stubs.enter_context(patch.object(TESTED.os, 'umask', return_value=None))
                worker_stubs.enter_context(patch.object(TESTED.signal, 'signal', return_value=None))
                worker_stubs.enter_context(patch.object(TESTED.os, 'getppid', return_value=4444))
                worker_stubs.enter_context(patch.object(TESTED.os, 'readlink', return_value='net:[123]'))
                opened = worker_stubs.enter_context(patch.object(TESTED.os, 'fstat', return_value=facts))
                read_control = worker_stubs.enter_context(patch.object(TESTED, 'protected_bytes'))
                run_worker = worker_stubs.enter_context(patch.object(TESTED.Recorder, 'run', return_value=0))
                for change in (None, 'variant', 'path', 'marker', 'supervisor', 'media', 'lock'):
                    observed = copy.deepcopy(control)
                    opened.return_value = facts
                    if change == 'variant':
                        observed['inputs']['variant'] = 'ranking' if variant == 'eligibility' else 'eligibility'
                    elif change == 'path':
                        observed['path'] = str(TESTED.WORK / 'reference-similarity-combinations-v2')
                    elif change == 'marker':
                        observed['marker'] = 'goby-reference-similarity-combinations-m3e-v2'
                    elif change == 'supervisor':
                        observed['supervisor']['pid'] = 4445
                    elif change == 'media':
                        observed['media']['aux43'] = 'changed'
                    elif change == 'lock':
                        opened.return_value = types.SimpleNamespace(**dict(vars(facts), st_ino=999))
                    read_control.return_value = TESTED.encoded(observed)
                    run_worker.reset_mock()
                    worker_op.saved.pop(TESTED.PRIVATE / 'worker.json', None)
                    if change is None:
                        self.assertEqual(TESTED.main(), 0)
                        run_worker.assert_called_once_with()
                    else:
                        with self.subTest(variant=variant, worker_binding=change), self.assertRaises(TESTED.Failure):
                            TESTED.main()
                        run_worker.assert_not_called()
        check_rec, check_op, _helper, _events = recorder()
        raw_map = {Path(TESTED.__file__): SOURCE, Path(check_rec.args.snapshot_helper): b'helper', TESTED.OPERATOR: b'operator',
                   check_op.OWNER: b'owner', TESTED.INDEX: b'index', TESTED.CONTROL / 'OWNER.json': TESTED.encoded(check_rec.control)}
        for constant, raw in (('HELPER_SHA', b'helper'), ('OPERATOR_SHA', b'operator'), ('OWNER_SHA', b'owner'), ('INDEX_SHA', b'index')):
            self.stack.enter_context(patch.object(TESTED, constant, TESTED.sha(raw)))
        self.stack.enter_context(patch.object(TESTED, 'protected_bytes', lambda path: raw_map[path]))
        namespace = self.stack.enter_context(patch.object(TESTED.os, 'readlink', return_value='net:[123]'))
        TESTED.Recorder.check(check_rec)
        namespace.return_value = 'net:[foreign]'
        with self.assertRaises(TESTED.Failure):
            TESTED.Recorder.check(check_rec)
        namespace.return_value = 'net:[123]'
        raw_map[check_op.OWNER] = b'changed-owner'
        with self.assertRaises(TESTED.Failure):
            TESTED.Recorder.check(check_rec)
        raw_map[check_op.OWNER] = b'owner'
        check_op.same_service = Mock(side_effect=TESTED.Failure('Synthetic service lifetime drift.'))
        with self.assertRaises(TESTED.Failure):
            TESTED.Recorder.check(check_rec)


def main():
    global TESTED, SOURCE
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operator', type=Path)
    parser.add_argument('--operator-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise SystemExit('Run memory guards only through authorized root SSH on test-env.')
    import fcntl
    SOURCE = args.operator.read_bytes()
    observed = hashlib.sha256(SOURCE).hexdigest()
    if not re.fullmatch(r'[0-9a-f]{64}', args.operator_sha256) or observed != args.operator_sha256:
        raise SystemExit('The explicitly pinned operator SHA-256 differs.')
    guard_sha = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    TESTED = types.ModuleType('reference_similarity_combination_guard_target')
    TESTED.__file__ = str(args.operator.absolute())
    sys.addaudithook(audit)
    with Fence():
        exec(compile(SOURCE, TESTED.__file__, 'exec'), TESTED.__dict__)
    output = io.StringIO()
    result = unittest.TextTestRunner(stream=output, verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guards))
    passed = result.wasSuccessful() and not result.skipped
    print(json.dumps({'suite': 'reference-similarity-combinations-memory-guards', 'passed': passed,
                      'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors),
                      'skips': len(result.skipped), 'operator_sha256': observed, 'guard_sha256': guard_sha,
                      'live_http_executed': False, 'database_mutations': 0, 'service_actions': 0,
                      'failure_details': None if passed else output.getvalue()}))
    return 0 if passed else 1


if __name__ == '__main__':
    raise SystemExit(main())
