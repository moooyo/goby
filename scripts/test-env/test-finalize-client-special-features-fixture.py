#!/usr/bin/env python3
"""Pure request-scope, original-byte and owned-cleanup finalizer gates."""

import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import sys
import types
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
TARGET = BASE = TRANSPORT = FINALIZER = None


def load(path, expected, name):
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected:
        raise RuntimeError('A frozen guard input changed.')
    value = types.ModuleType(name)
    value.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), value.__dict__)
    return value


def task():
    value = TRANSPORT.job()
    value.old_profile = types.SimpleNamespace(KINDS={'alpha': 'clip', 'deleted': 'deleted_scene', 'zeta': 'clip', 'trailer': 'trailer'})
    value.item_ids = {name: f'{index + 20:032x}' for index, name in enumerate(value.old_profile.KINDS)}
    value.api_items = {key: {'path': '/owned/' + key + '.mp4', 'relative_path': key + '.mp4'} for key in value.item_ids}
    data = b'x' * 2048
    value.ext = types.SimpleNamespace(protected_bytes=lambda *_: data)
    value.media = {'special': {'files': {'Movies/' + key + '.mp4': {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}
                                      for key in value.item_ids}}}
    return value


class FinalizerGates(unittest.TestCase):
    def test_01_only_fresh_viewer_and_eight_exact_media_requests(self):
        value = task()
        viewer = TRANSPORT.prove(value)
        for method, path in (('POST', '/admin/v1/libraries'), ('POST', '/admin/v1/libraries/owned/scan'),
                             ('GET', '/emby/Items/owned/PlaybackInfo'), ('GET', '/emby/Users/owned/Items/owned')):
            with self.subTest(path=path), self.assertRaises((TARGET.FinalizerError, BASE.SetupError)):
                value.approved('unapproved', method, path, viewer, None, False, False, None)
        admin = value.actors['admin']
        with self.assertRaises(TARGET.FinalizerError):
            value.approved('admin-login', 'POST', '/admin/v1/session', admin, {'Name': 'admin'}, True, False, None)
        route = '/emby/Videos/' + value.item_ids['alpha'] + '/original.mp4'
        value.allowed.add(('viewer', route, 'full'))
        value.approved('alpha-full', 'GET', route, viewer, None, False, False, 'full')
        value.sequence = 11
        with self.assertRaises(TARGET.FinalizerError):
            value.approved('alpha-full', 'GET', route, viewer, None, False, False, 'full')

    def test_02_eleven_requests_end_with_exact_owned_401(self):
        value = task()
        body, cookie = TRANSPORT.acknowledgement(value, 'viewer')
        replies = [TRANSPORT.Response(200, body, cookie)]
        for _ in value.item_ids:
            replies.append(TRANSPORT.Response(200, b'x' * 2048, mime='video/mp4'))
            response = TRANSPORT.Response(206, b'x' * 1024, mime='video/mp4')
            response.headers['Content-Range'] = 'bytes 0-1023/2048'
            replies.append(response)
        replies += [TRANSPORT.Response(204), TRANSPORT.Response(401)]
        wire = TRANSPORT.Wire(replies)
        with patch.object(BASE.http.client, 'HTTPConnection', wire.connection), patch.object(BASE.signal, 'setitimer', lambda *_: None):
            value.media_capture()
        self.assertEqual(value.sequence, 11)
        self.assertEqual(len(wire.sent), 11)
        self.assertTrue(value.actors['viewer'].closed)
        self.assertEqual([row[0] for row in wire.sent].count('POST'), 2)
        self.assertEqual(wire.sent[-2][1], '/emby/Sessions/Logout')
        self.assertEqual(wire.sent[-1][1], '/emby/System/Info')

    def test_03_bad_first_media_stops_before_other_resources_and_cleans_up(self):
        value = task()
        body, cookie = TRANSPORT.acknowledgement(value, 'viewer')
        wire = TRANSPORT.Wire([TRANSPORT.Response(200, body, cookie), TRANSPORT.Response(404, {'ErrorCode': 'not_found'}),
                               TRANSPORT.Response(204), TRANSPORT.Response(401)])
        with patch.object(BASE.http.client, 'HTTPConnection', wire.connection), patch.object(BASE.signal, 'setitimer', lambda *_: None):
            with self.assertRaises(TARGET.FinalizerError):
                value.media_capture()
            value.actors['viewer'].logout()
        self.assertEqual(len(wire.sent), 4)
        self.assertTrue(value.actors['viewer'].closed)

    def test_04_range_status_hash_and_content_range_are_exact(self):
        source = {'bytes': 2048, 'sha256': hashlib.sha256(b'x' * 2048).hexdigest()}
        prefix = hashlib.sha256(b'x' * 1024).hexdigest()
        response = {'status': 206, 'complete': True, 'failure_type': None, 'body': None, 'content_type': 'video/mp4',
                    'bytes': 1024, 'sha256': prefix, 'headers': {'Content-Range': 'bytes 0-1023/2048'}}
        TARGET.validate_media_response(response, 'range', source, prefix)
        for key, value in (('status', 200), ('complete', False), ('content_type', 'application/json'), ('sha256', '0' * 64),
                           ('headers', {'Content-Range': 'bytes 1-1024/2048'}), ('bytes', 1023)):
            changed = copy.deepcopy(response)
            changed[key] = value
            with self.subTest(key=key), self.assertRaises(TARGET.FinalizerError):
                TARGET.validate_media_response(changed, 'range', source, prefix)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('operator', 'origin-operator', 'origin-guards'):
        parser.add_argument('--' + name, required=True, type=Path)
        parser.add_argument('--' + name + '-sha256', required=True)
    args = parser.parse_args()
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION'):
        raise RuntimeError('Run pure guards only through authorized root SSH.')
    global TARGET, BASE, TRANSPORT, FINALIZER
    TARGET = load(args.operator, args.operator_sha256, 'finalizer_operator')
    BASE = load(args.origin_operator, args.origin_operator_sha256, 'retained_transport')
    TRANSPORT = load(args.origin_guards, args.origin_guards_sha256, 'retained_transport_guards')
    FINALIZER = TARGET.make_finalizer(BASE, types.SimpleNamespace())
    TRANSPORT.OP = types.SimpleNamespace(Setup=FINALIZER, Actor=BASE.Actor, SetupError=(BASE.SetupError, TARGET.FinalizerError),
        require=TARGET.require, http=BASE.http, signal=BASE.signal, JSON_LIMIT=BASE.JSON_LIMIT)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(FinalizerGates)
    selected = {4, 5, 8, 9, 11, 12}
    for name in unittest.defaultTestLoader.getTestCaseNames(TRANSPORT.Gates):
        if int(name.split('_')[1]) in selected:
            suite.addTest(TRANSPORT.Gates(name))
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print(json.dumps({'result': 'passed' if result.wasSuccessful() else 'failed', 'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'http_requests': 0, 'database_writes': 0, 'service_actions': 0}), flush=True)
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
