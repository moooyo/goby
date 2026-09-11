#!/usr/bin/env python3
"""Guard the cache creator with memory-only files and descriptor operations.

Run through authorized root SSH with the new creator source as the sole
argument. The source is read before the effect fence; no deployment operator,
database, service, real cache, or external process is opened by the tests.
"""

from __future__ import annotations

import argparse
import builtins
from contextlib import ExitStack
import copy
import datetime
import grp
import hashlib
import io
import json
import linecache
import os
from pathlib import Path
import pwd
import re
import socket
import stat
import subprocess
import sys
import types
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
CREATOR = None
FENCED = False


def deny(*_args, **_kwargs):
    raise AssertionError('An unmocked external effect was attempted.')


def audited(event, _args):
    if FENCED and (event == 'open' or event.startswith(('socket.', 'subprocess.', 'os.', 'fcntl.'))):
        raise AssertionError('An audited external effect was attempted: ' + event)


def fence():
    stack = ExitStack()
    for owner, names in (
        (builtins, ('open',)), (io, ('open', 'FileIO')),
        (os, ('open', 'fdopen', 'stat', 'lstat', 'fstat', 'mkdir', 'makedirs', 'listdir', 'scandir', 'readlink',
              'close', 'fsync', 'fchown', 'fchmod', 'chmod', 'chown', 'rename', 'replace', 'unlink', 'remove',
              'rmdir', 'system', 'popen', 'fork', 'execve', 'kill', 'killpg')),
        (Path, ('lstat', 'stat', 'open', 'read_text', 'read_bytes', 'write_text', 'write_bytes', 'mkdir',
                'unlink', 'rmdir', 'rename', 'replace', 'chmod', 'iterdir', 'glob', 'rglob')),
        (socket, ('socket', 'create_connection', 'getaddrinfo')),
        (subprocess, ('run', 'Popen', 'call', 'check_call', 'check_output')),
    ):
        for name in names:
            if hasattr(owner, name):
                stack.enter_context(patch.object(owner, name, deny))
    stack.enter_context(patch.object(linecache, 'getlines', lambda *_args, **_kwargs: []))
    stack.enter_context(patch.object(linecache, 'checkcache', lambda *_args: None))
    return stack


def information(inode, *, uid=0, gid=0, mode=0o700, device=7, kind=stat.S_IFDIR):
    return types.SimpleNamespace(st_dev=device, st_ino=inode, st_uid=uid, st_gid=gid,
                                 st_mode=kind | mode, st_size=0, st_nlink=1, st_mtime_ns=0, st_ctime_ns=0)


class MemoryCreation:
    """Model only the exact new control and cache entries owned by create()."""

    def __init__(self):
        self.nodes = {CREATOR.WORK: information(100), CREATOR.CACHE.parent: information(200, mode=0o1777)}
        self.fds, self.files, self.events = {}, {}, []
        self.next_fd = 40
        self.fail_intent = False
        self.fail_receipt = False
        self.inspect_drift = False
        self.wrong_cache_fd = False
        self.nonempty_cache = False
        self.before = {'marker': CREATOR.MARKER, 'cache_missing': True, 'active_encodings': 0,
                       'parent': {'ancestors': [{'path': str(CREATOR.CACHE.parent), **CREATOR.fact(self.nodes[CREATOR.CACHE.parent])}]}}
        self.args = types.SimpleNamespace(inspection_sha256=CREATOR.sha(CREATOR.canonical(self.before)),
                                         parent_device=7, boot_id='00000000-0000-0000-0000-000000000001')

    def resolve(self, path, directory_fd=None):
        path = Path(path)
        return path if directory_fd is None else self.fds[directory_fd] / path

    def lstat(self, path):
        path = Path(path)
        if path not in self.nodes:
            raise FileNotFoundError(str(path))
        return self.nodes[path]

    def open(self, path, flags, mode=0o777, *, dir_fd=None):
        path = self.resolve(path, dir_fd)
        self.events.append(('open', str(path), flags))
        if flags & os.O_CREAT:
            if path in self.nodes:
                raise FileExistsError(str(path))
            if path.name == 'intent.json' and self.fail_intent or path.name == 'created.json' and self.fail_receipt:
                raise OSError('Synthetic publication failure')
            self.nodes[path] = information(500 + self.next_fd, mode=mode, kind=stat.S_IFREG)
        else:
            self.lstat(path)
        descriptor = self.next_fd
        self.next_fd += 1
        self.fds[descriptor] = path
        return descriptor

    def fstat(self, descriptor):
        path = self.fds[descriptor]
        info = self.lstat(path)
        if path == CREATOR.CACHE and self.wrong_cache_fd:
            changed = copy.copy(info)
            changed.st_ino += 1
            return changed
        return info

    def stat(self, name, *, dir_fd, follow_symlinks):
        if follow_symlinks:
            raise AssertionError('Directory entry checks must not follow links.')
        return self.lstat(self.resolve(name, dir_fd))

    def mkdir(self, name, mode, *, dir_fd):
        path = self.resolve(name, dir_fd)
        if path not in (CREATOR.CONTROL, CREATOR.CACHE) or path in self.nodes:
            raise FileExistsError(str(path))
        self.events.append(('mkdir', str(path), mode))
        self.nodes[path] = information(300 if path == CREATOR.CONTROL else 400, mode=mode)

    def fdopen(self, descriptor, mode):
        if mode != 'wb':
            raise AssertionError('Only new private record writes are permitted.')
        memory = self

        class Writer(io.BytesIO):
            def fileno(self):
                return descriptor

            def close(self):
                if not self.closed:
                    path = memory.fds[descriptor]
                    memory.files[path] = self.getvalue()
                    memory.events.append(('file-closed', path.name))
                super().close()

        return Writer()

    def fsync(self, descriptor):
        self.events.append(('fsync', str(self.fds[descriptor])))

    def close(self, descriptor):
        self.events.append(('close', str(self.fds[descriptor])))

    def listdir(self, descriptor):
        if self.fds[descriptor] != CREATOR.CACHE:
            raise AssertionError('Only the new cache descriptor may be enumerated.')
        return ['unexpected-output'] if self.nonempty_cache else []

    def fchown(self, descriptor, uid, gid):
        path = self.fds[descriptor]
        if path != CREATOR.CACHE:
            raise AssertionError('Ownership mutation escaped the new cache descriptor.')
        self.events.append(('fchown', str(path), uid, gid))
        self.nodes[path].st_uid, self.nodes[path].st_gid = uid, gid

    def fchmod(self, descriptor, mode):
        path = self.fds[descriptor]
        if path != CREATOR.CACHE:
            raise AssertionError('Mode mutation escaped the new cache descriptor.')
        self.events.append(('fchmod', str(path), mode))
        self.nodes[path].st_mode = stat.S_IFDIR | mode

    def inspect(self, _op, _args, created=None):
        self.events.append(('inspect', created is not None))
        if self.inspect_drift:
            return {**self.before, 'active_encodings': 1}
        if created is None:
            CREATOR.absent(CREATOR.CACHE)
        return {**self.before, 'cache_missing': created is None}

    def install(self, stack):
        for name in ('open', 'fstat', 'stat', 'mkdir', 'fdopen', 'fsync', 'close', 'listdir', 'fchown', 'fchmod'):
            stack.enter_context(patch.object(CREATOR.os, name, getattr(self, name)))
        stack.enter_context(patch.object(Path, 'lstat', lambda path: self.lstat(path)))
        stack.enter_context(patch.object(CREATOR, 'inspect', self.inspect))
        stack.enter_context(patch.object(CREATOR, 'secure_file', lambda path: b'Synthetic pinned creator source'))
        return self


class CacheCreationGuards(unittest.TestCase):
    def setUp(self):
        self.stack = fence()
        self.addCleanup(self.stack.close)
        self.memory = MemoryCreation().install(self.stack)

    def create(self):
        return CREATOR.create(types.SimpleNamespace(), self.memory.args, self.memory.before)

    def mutations(self):
        return [entry for entry in self.memory.events if entry[0] in ('fchown', 'fchmod')]

    def cache_creations(self):
        return [entry for entry in self.memory.events if entry[:2] == ('mkdir', str(CREATOR.CACHE))]

    def test_durable_intent_precedes_reinspection_and_exact_private_cache_creation(self):
        result = self.create()
        events = self.memory.events
        intent_sync = events.index(('fsync', str(CREATOR.CONTROL / 'intent.json')))
        control_sync = next(index for index in range(intent_sync + 1, len(events))
                            if events[index] == ('fsync', str(CREATOR.CONTROL)))
        inspected = events.index(('inspect', False))
        created = events.index(('mkdir', str(CREATOR.CACHE), 0o700))
        self.assertLess(intent_sync, control_sync)
        self.assertLess(control_sync, inspected)
        self.assertLess(inspected, created)
        self.assertEqual(self.mutations(), [('fchown', str(CREATOR.CACHE), 995, 986),
                                           ('fchmod', str(CREATOR.CACHE), 0o700)])
        self.assertEqual(len(self.cache_creations()), 1)
        receipt = json.loads(self.memory.files[CREATOR.CONTROL / 'created.json'])
        self.assertEqual(receipt['identity'], CREATOR.fact(self.memory.nodes[CREATOR.CACHE]))
        self.assertEqual((receipt['service_started'], receipt['database_mutated'], receipt['old_outputs_restored']),
                         (False, False, False))
        self.assertEqual(result['status'], 'created')

    def test_preexisting_control_or_cache_is_never_adopted_or_chowned(self):
        for path in (CREATOR.CONTROL, CREATOR.CACHE):
            with self.subTest(path=str(path)):
                memory = MemoryCreation()
                memory.nodes[path] = information(999, uid=995, gid=986, mode=0o755)
                with ExitStack() as stack:
                    memory.install(stack)
                    original = copy.deepcopy(memory.nodes[path])
                    with self.assertRaises(CREATOR.Failure):
                        CREATOR.create(types.SimpleNamespace(), memory.args, memory.before)
                    self.assertEqual(vars(memory.nodes[path]), vars(original))
                    self.assertFalse(any(x[0] in ('fchown', 'fchmod') for x in memory.events))
                    self.assertFalse(any(x[:2] == ('mkdir', str(CREATOR.CACHE)) for x in memory.events))

    def test_intent_failure_prevents_cache_mkdir_and_keeps_control_fence(self):
        self.memory.fail_intent = True
        with self.assertRaises(OSError):
            self.create()
        self.assertIn(CREATOR.CONTROL, self.memory.nodes)
        self.assertEqual(self.cache_creations(), [])
        self.assertEqual(self.mutations(), [])
        self.assertNotIn(CREATOR.CONTROL / 'created.json', self.memory.files)

    def test_prerequisite_drift_after_intent_prevents_cache_mkdir(self):
        self.memory.inspect_drift = True
        with self.assertRaises(CREATOR.Failure):
            self.create()
        self.assertIn(CREATOR.CONTROL / 'intent.json', self.memory.files)
        self.assertEqual(self.cache_creations(), [])
        self.assertEqual(self.mutations(), [])

    def test_wrong_fresh_descriptor_or_nonempty_cache_is_rejected_before_chown(self):
        for attribute in ('wrong_cache_fd', 'nonempty_cache'):
            with self.subTest(condition=attribute):
                memory = MemoryCreation()
                setattr(memory, attribute, True)
                with ExitStack() as stack:
                    memory.install(stack)
                    with self.assertRaises(CREATOR.Failure):
                        CREATOR.create(types.SimpleNamespace(), memory.args, memory.before)
                    self.assertIn(CREATOR.CACHE, memory.nodes)
                    self.assertFalse(any(x[0] in ('fchown', 'fchmod') for x in memory.events))
                    self.assertNotIn(CREATOR.CONTROL / 'created.json', memory.files)

    def test_receipt_failure_retains_created_cache_and_refuses_automatic_retry(self):
        self.memory.fail_receipt = True
        with self.assertRaises(OSError):
            self.create()
        self.assertEqual(CREATOR.fact(self.memory.nodes[CREATOR.CACHE]),
                         {'device': 7, 'inode': 400, 'uid': 995, 'gid': 986, 'mode': 0o700})
        self.assertIn(CREATOR.CONTROL / 'intent.json', self.memory.files)
        original = list(self.memory.events)
        with self.assertRaises(CREATOR.Failure):
            self.create()
        self.assertEqual(self.memory.events, original)
        self.assertEqual(len(self.cache_creations()), 1)

    def test_wrong_inspection_digest_fails_before_creating_control_or_cache(self):
        self.memory.args.inspection_sha256 = '0' * 64
        with self.assertRaises(CREATOR.Failure):
            self.create()
        self.assertEqual(self.memory.events, [])
        self.assertNotIn(CREATOR.CONTROL, self.memory.nodes)
        self.assertNotIn(CREATOR.CACHE, self.memory.nodes)


def main():
    global CREATOR, FENCED
    if sys.platform != 'linux' or os.geteuid() != 0 or not os.environ.get('SSH_CONNECTION') or len(sys.argv) != 2:
        print(json.dumps({'suite': 'main-transcode-cache-guards', 'status': 'blocked',
                          'reason': 'Authorized root SSH and one cache creator source path are required.'}))
        return 2
    source = Path(sys.argv[1])
    if not source.is_absolute() or source.name != 'prepare-main-transcode-cache.py':
        raise SystemExit('Only the new cache creator source may be loaded.')
    raw, suite_raw = source.read_bytes(), Path(__file__).read_bytes()
    CREATOR = types.ModuleType('memory_main_transcode_cache')
    CREATOR.__file__ = str(source)
    sys.addaudithook(audited)
    FENCED = True
    try:
        with fence():
            exec(compile(raw, str(source), 'exec'), CREATOR.__dict__)
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(CacheCreationGuards)
        names = sorted(test._testMethodName for test in suite)
        result = unittest.TestResult()
        suite.run(result)
    finally:
        FENCED = False
    failures = [*result.failures, *result.errors]
    passed = result.wasSuccessful() and not result.skipped
    print(json.dumps({'suite': 'main-transcode-cache-guards', 'status': 'passed' if passed else 'failed',
                      'tests': result.testsRun, 'failures': len(result.failures), 'errors': len(result.errors),
                      'skips': len(result.skipped), 'cases': names,
                      'failure_summaries': [{'test': test.id(), 'message': detail.rstrip().splitlines()[-1]}
                                            for test, detail in failures[:8]],
                      'creator_sha256': hashlib.sha256(raw).hexdigest(),
                      'guard_sha256': hashlib.sha256(suite_raw).hexdigest(),
                      'fixtures': 'synthetic-memory-only', 'real_cache_creation': False}))
    return 0 if passed else 1


if __name__ == '__main__':
    raise SystemExit(main())
