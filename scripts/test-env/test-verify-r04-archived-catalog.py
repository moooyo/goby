"""Bounded remote tests for the r04 standalone reader; never run its actor main.

Run with python3 -I -B and --workdir pointing to a new private test directory.
Only synthetic files, finite Python leaf processes, and the pinned archive's
member metadata are used. No archive is extracted from the real saved bytes;
no PostgreSQL, SQL client, mount command, unit, service or application is run.
"""

import argparse
import contextlib
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import types
import unittest
from unittest import mock


MODULE_PATH = Path(__file__).with_name('verify-r04-archived-catalog.py')
spec = importlib.util.spec_from_file_location('r04_archived_catalog_under_test', MODULE_PATH)
reader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(reader)
WORKDIR = None


def snapshot():
    return {'serverId': 'a' * 32, 'schema': {'count': 28, 'min': 1, 'max': 28}, 'unrevokedSessions': 0,
            'sessions': [{'id': str(i), 'revokedAt': '2026-09-15T00:00:00Z', 'kind': 'emby',
                          'userId': 'fixture', 'tokenHash': str(i)} for i in range(8)],
            'tables': {name: {'count': count, 'rows': [{'row': {'value': i}, 'xmin': '42'} for i in range(count)]}
                       for name, count in reader.COUNTS.items()}}


def frame(value=None):
    value = value or {'identity': {}, 'snapshot': {}}
    return (b'\nPostgreSQL stand-alone backend 17.11\nbackend> ' + reader.FRAME_START +
            reader.encoded(value).hex().encode() + reader.FRAME_END + b'\nbackend> ')


def input_value():
    return {'kind': 'r04-archived-catalog-read-input', 'version': 1, 'approved': True,
            'sourceSha256': 'a' * 64, 'scopeId': '20260915-a1b2c3d4',
            'bootId': '11111111-1111-1111-1111-111111111111',
            'tools': {key: {'path': path, 'bytes': 1, 'sha256': 'a' * 64} for key, path in reader.TOOL_PATHS.items()},
            'protected': {'units': {name: {} for name in reader.PROTECTED_UNITS},
                          'files': [{'path': name, 'mode': 'metadata', 'metadata': {}, 'sha256': None}
                                    for name in sorted(reader.PROTECTED_FILES)]}}


def child_reader(process):
    value = object.__new__(reader.Reader)
    value.child = process
    value.child_identity = None
    value.child_reaped = False
    value.child_exit = None
    value.deadline = time.monotonic() + 15
    value.business_deadline = value.deadline - 2
    value.cleaning = False
    value.report = {}
    return value


def file_metadata(path):
    observed = path.lstat()
    return {'device': observed.st_dev, 'inode': observed.st_ino, 'uid': observed.st_uid,
            'gid': observed.st_gid, 'mode': stat.S_IMODE(observed.st_mode),
            'type': stat.S_IFMT(observed.st_mode)}


class ReaderTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='case-', dir=WORKDIR)
        self.path = Path(self.temporary.name)
        self.addCleanup(self.temporary.cleanup)
        self.previous_signal = reader.PENDING_SIGNAL
        reader.PENDING_SIGNAL = None
        self.handlers = {sig: signal.getsignal(sig) for sig in reader.CONTROL_SIGNALS}

    def tearDown(self):
        reader.PENDING_SIGNAL = self.previous_signal
        for sig, handler in self.handlers.items():
            signal.signal(sig, handler)

    def test_strict_json_rejects_duplicates_and_nonfinite_values(self):
        self.assertEqual(reader.parse(b'{"ok": 1}'), {'ok': 1})
        for raw in (b'{"x":1,"x":2}', b'{"x":NaN}', b'{"x":Infinity}', b'{"x":-Infinity}'):
            with self.subTest(raw=raw), self.assertRaises(reader.Rejected):
                reader.parse(raw)

    def test_complete_frame_has_one_strict_standalone_envelope(self):
        self.assertEqual(reader.parse_frame(frame(), b''), {'identity': {}, 'snapshot': {}})
        for raw in (b'', frame() + frame(), frame()[:-5], b'extra\n' + frame(), frame() + b'extra',
                    frame().replace(reader.FRAME_END, b''), frame().replace(reader.FRAME_START, reader.FRAME_START + b'f'),
                    frame().replace(reader.FRAME_START, reader.FRAME_START + b'g')):
            with self.subTest(bytes=len(raw)), self.assertRaises(reader.Rejected):
                reader.parse_frame(raw, b'')

    def test_sql_error_rejects_a_frame_even_when_child_exit_is_zero(self):
        completed = types.SimpleNamespace(returncode=0, stdout=frame())
        for error in (b'ERROR: failed\n', b'FATAL: failed\n', b'PANIC: failed\n', b'WARNING: changed\n'):
            with self.subTest(error=error), self.assertRaises(reader.Rejected):
                self.assertEqual(completed.returncode, 0)
                reader.parse_frame(completed.stdout, error)

    def test_snapshot_preserves_numeric_types_rows_xmin_and_sessions(self):
        expected = snapshot()
        reader.compare_snapshot(copy.deepcopy(expected), expected)
        mutations = [lambda v: v.update(unrevokedSessions=False),
                     lambda v: v['tables']['catalog_entities'].update(count=True),
                     lambda v: v['tables']['users']['rows'][0].update(xmin='43'),
                     lambda v: v['tables']['users']['rows'][0]['row'].update(value=True),
                     lambda v: v['sessions'][0].update(tokenHash='changed'),
                     lambda v: v['sessions'][0].update(revokedAt=None)]
        for mutate in mutations:
            actual = copy.deepcopy(expected)
            mutate(actual)
            with self.subTest(mutation=mutate), self.assertRaises(reader.Rejected):
                reader.compare_snapshot(actual, expected)

    def test_input_requires_fixed_scope_and_complete_protection(self):
        value = input_value()
        reader.validate_input(value)
        mutations = [lambda v: v.update(scopeId='../../old-scope'), lambda v: v.update(approved=False),
                     lambda v: v.update(version=True),
                     lambda v: v['protected']['units'].pop(next(iter(reader.PROTECTED_UNITS))),
                     lambda v: v['protected']['files'].pop(),
                     lambda v: v['protected']['files'].append(copy.deepcopy(v['protected']['files'][0]))]
        for mutate in mutations:
            invalid = copy.deepcopy(value)
            mutate(invalid)
            with self.subTest(mutation=mutate), self.assertRaises(reader.Rejected):
                reader.validate_input(invalid)
        invalid = copy.deepcopy(value)
        master = next(row for row in invalid['protected']['files'] if 'master.key' in row['path'])
        master.update(mode='hash', sha256='a' * 64)
        with self.assertRaises(reader.Rejected):
            reader.validate_input(invalid)

    def test_bootstrap_ignores_atime_but_rejects_stable_field_drift(self):
        path = self.path / 'input.json'
        path.write_bytes(b'{}')
        path.chmod(0o600)
        actual_fstat = os.fstat
        names = ('st_dev', 'st_ino', 'st_uid', 'st_gid', 'st_mode', 'st_size', 'st_nlink', 'st_mtime_ns', 'st_ctime_ns')
        for changed in ('atime', 'st_ino', 'st_mtime_ns'):
            calls = []
            def fstat(fd):
                original = actual_fstat(fd)
                row = types.SimpleNamespace(**{name: getattr(original, name) for name in names}, st_atime=len(calls))
                calls.append(fd)
                if changed != 'atime' and len(calls) == 2:
                    setattr(row, changed, getattr(row, changed) + 1)
                return row
            with self.subTest(changed=changed), mock.patch.object(reader.os, 'fstat', side_effect=fstat):
                if changed == 'atime':
                    self.assertEqual(reader.read_bootstrap(path, 10), b'{}')
                else:
                    with self.assertRaises(reader.Rejected):
                        reader.read_bootstrap(path, 10)

    def test_bootstrap_rejects_replaced_path_after_open(self):
        path, replacement = self.path / 'input', self.path / 'replacement'
        path.write_bytes(b'{}')
        replacement.write_bytes(b'{}')
        path.chmod(0o600)
        replacement.chmod(0o600)
        original_read = os.read
        def replace_after_read(fd, count):
            value = original_read(fd, count)
            replacement.replace(path)
            return value
        with mock.patch.object(reader.os, 'read', side_effect=replace_after_read), self.assertRaises(reader.Rejected):
            reader.read_bootstrap(path, 10)

    def test_actual_archive_metadata_and_recovery_rejections(self):
        raw = reader.read_bootstrap(Path(reader.ARCHIVE['path']), 8 << 20)
        self.assertEqual((len(raw), reader.sha(raw)), (reader.ARCHIVE['bytes'], reader.ARCHIVE['sha256']))
        subject = object.__new__(reader.Reader)
        with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
            members = subject.archive_members(archive)
        self.assertEqual(len(members), 1518)
        self.assertTrue(reader.CONFIG_MEMBERS <= {row.name for row in members})
        mutations = [('uid', 0), ('mode', 0o644), ('type', tarfile.SYMTYPE),
                     ('name', 'tree/data/../escape'), ('name', 'tree/data/recovery.signal'),
                     ('name', 'tree/data/pg_tblspc/external'), ('pax_headers', {'path': 'elsewhere'}),
                     ('name', 'tree/data/postmaster.opts.backup'), ('name', 'tree/data/other/postmaster.opts'),
                     ('name', 'tree/data/postmaster.conf'), ('name', 'tree/data/master.key'),
                     ('name', 'tree/data/secret.dat')]
        index = next(i for i, row in enumerate(members) if row.isfile() and row.name not in reader.CONFIG_MEMBERS)
        for attribute, value in mutations:
            changed = copy.deepcopy(members)
            setattr(changed[index], attribute, value)
            with self.subTest(attribute=attribute, value=value), self.assertRaises(reader.Rejected):
                subject.archive_members(types.SimpleNamespace(getmembers=lambda: changed))

    def test_synthetic_copy_uses_pinned_bytes_and_omits_private_configuration(self):
        volume = self.path / 'synthetic-copy'
        volume.mkdir()
        payloads = {'tree/data/PG_VERSION': b'17\n'}
        payloads.update({name: b'synthetic old configuration' for name in reader.CONFIG_MEMBERS})
        members = []
        for name in ('tree', 'tree/data', 'tree/socket', *payloads, 'tree/initdb-password'):
            row = tarfile.TarInfo(name)
            row.uid, row.gid = reader.UID, reader.GID
            row.type = tarfile.DIRTYPE if name in ('tree', 'tree/data', 'tree/socket') else tarfile.REGTYPE
            row.mode = 0o700 if row.isdir() else 0o600
            row.size = len(payloads.get(name, b''))
            members.append(row)
        writes, saved, opened = [], [], []
        archive = mock.MagicMock()
        archive.__enter__.return_value = archive
        def member_stream(member):
            self.assertNotEqual(member.name, 'tree/initdb-password')
            opened.append(member.name)
            return io.BytesIO(payloads[member.name])
        archive.extractfile.side_effect = member_stream
        subject = object.__new__(reader.Reader)
        subject.volume = volume
        subject.remaining = lambda _maximum: 1
        subject.pinned = lambda *_args: b'pinned synthetic archive'
        subject.archive_members = lambda _archive: members
        subject.save = lambda name, value: saved.append((name, value))
        subject.base = types.SimpleNamespace(write_new=lambda path, raw, **kwargs: writes.append((path, raw)))
        def open_archive(*args, **kwargs):
            self.assertFalse(args)
            self.assertEqual(kwargs['fileobj'].getvalue(), b'pinned synthetic archive')
            return archive
        with mock.patch.object(reader.tarfile, 'open', side_effect=open_archive), mock.patch.object(reader.os, 'chown'):
            subject.extract_copy()
        self.assertEqual(writes, [(volume / 'data/PG_VERSION', b'17\n')])
        manifest = {row['name']: row for row in saved[0][1]}
        self.assertEqual(manifest['tree/initdb-password']['disposition'], 'metadata_only_omitted')
        self.assertNotIn('tree/initdb-password', opened)
        for name in reader.CONFIG_MEMBERS:
            self.assertEqual(manifest[name]['disposition'], 'hash_only_omitted')
            self.assertNotIn('content', manifest[name])

    def test_fixture_traversal_under_private_umask_with_real_postgres_uid_leaf(self):
        raw, fixed = self.path / 'raw-fixture', self.path / 'fixed-fixture'
        subject = object.__new__(reader.Reader)
        subject.fixture = fixed
        subject.base = types.SimpleNamespace(metadata=file_metadata)
        subject.report = {}
        previous = os.umask(0o077)
        try:
            raw.mkdir(mode=0o711)
            subject.create_fixture_directory()
        finally:
            os.umask(previous)
        self.assertEqual(stat.S_IMODE(raw.stat().st_mode), 0o700)
        self.assertEqual(subject.report['fixtureDirectory']['mode'], 0o711)
        self.assertEqual((fixed.stat().st_uid, fixed.stat().st_gid), (0, 0))
        self.assertEqual(stat.S_IMODE(self.path.stat().st_mode), 0o700)
        program = ("import os,sys; from pathlib import Path; "
                   "assert (os.getuid(),os.getgid(),os.getgroups()) == (103,106,[]); "
                   "data=Path('pg/data/sample').read_bytes(); assert data == b'synthetic sample'; "
                   "sys.stdout.write('sample-readable\\n')")
        for fixture, accepted in ((raw, False), (fixed, True)):
            with self.subTest(accepted=accepted):
                pg, data = fixture / 'pg', fixture / 'pg/data'
                pg.mkdir(mode=0o700)
                data.mkdir(mode=0o700)
                sample = data / 'sample'
                sample.write_bytes(b'synthetic sample')
                sample.chmod(0o600)
                for path in (pg, data, sample):
                    os.chown(path, reader.UID, reader.GID)
                self.assertEqual((data.stat().st_uid, stat.S_IMODE(data.stat().st_mode)), (103, 0o700))
                def enter_then_drop():
                    # Enter while root, so private test ancestors remain mode 0700.
                    # The relative lookup still requires execute on this fixture.
                    os.chdir(fixture)
                    os.setgroups([])
                    os.setgid(reader.GID)
                    os.setuid(reader.UID)
                result = subprocess.run([sys.executable, '-I', '-B', '-c', program],
                                        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                        env=dict(reader.SAFE_ENV), preexec_fn=enter_then_drop, timeout=5)
                if accepted:
                    self.assertEqual((result.returncode, result.stdout, result.stderr), (0, b'sample-readable\n', b''))
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(b'PermissionError', result.stderr)
                    self.assertEqual(result.stdout, b'')
        self.assertEqual(stat.S_IMODE(self.path.stat().st_mode), 0o700)

    def test_wnowait_keeps_pid_until_group_is_closed_and_never_signals_after_reap(self):
        events = []
        process = types.SimpleNamespace(pid=4242, wait=lambda **_kw: events.append('wait'))
        subject = child_reader(process)
        result = types.SimpleNamespace(si_pid=4242, si_code=os.CLD_EXITED, si_status=0)
        with mock.patch.object(reader.os, 'waitid', return_value=result) as waitid:
            subject.observe_child_exit()
        self.assertTrue(waitid.call_args.args[2] & os.WNOWAIT)
        self.assertFalse(subject.child_reaped)
        self.assertEqual(events, [])
        def group():
            events.append('group')
            return [{'pid': 4242, 'state': 'Z', 'startTicks': '1'}]
        subject.owned_group_members = mock.Mock(side_effect=group)
        with mock.patch.object(reader.os, 'killpg') as kill:
            subject.close_child()
            self.assertEqual(events, ['group', 'group', 'wait'])
            subject.owned_group_members.reset_mock()
            subject.close_child()
            subject.owned_group_members.assert_not_called()
            kill.assert_not_called()
        self.assertTrue(subject.child_reaped)
        self.assertTrue(subject.report['childClosed'])

    def test_forced_group_close_checks_members_before_signal_and_reap(self):
        events = []
        process = types.SimpleNamespace(pid=4242, wait=lambda **_kw: events.append('wait'))
        subject = child_reader(process)
        calls = []
        def group():
            calls.append(True)
            events.append('group')
            if len(calls) >= 3:
                subject.child_exit = {'code': os.CLD_KILLED, 'status': signal.SIGTERM}
                return [{'pid': 4242, 'state': 'Z', 'startTicks': '1'}]
            return [{'pid': 4242, 'state': 'S', 'startTicks': '1'}]
        subject.owned_group_members = group
        with mock.patch.object(reader.os, 'killpg', side_effect=lambda *_args: events.append('signal')) as kill:
            subject.close_child()
        kill.assert_called_once_with(4242, signal.SIGTERM)
        self.assertLess(events.index('group'), events.index('signal'))
        self.assertLess(events.index('signal'), events.index('wait'))
        self.assertTrue(subject.report['forcedChildClosure'])

    def test_single_pipe_limits_timeout_and_natural_exit_with_finite_leaf_processes(self):
        programs = [('', b'ok', None), ('stdout', None, 'single_output_limit'),
                    ('stderr', None, 'single_output_limit'), ('sleep', None, 'single_child_timeout')]
        for mode, expected, failure in programs:
            with self.subTest(mode=mode):
                program = "import sys,time; sys.stdin.buffer.read(); "
                if mode == 'sleep':
                    program += 'time.sleep(1)'
                elif mode in ('stdout', 'stderr'):
                    program += 'sys.' + mode + '.buffer.write(b"x" * 1048577)'
                else:
                    program += 'sys.stdout.buffer.write(b"ok")'
                process = subprocess.Popen([sys.executable, '-I', '-B', '-c', program], stdin=subprocess.PIPE,
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
                subject = child_reader(process)
                subject.remaining = lambda _maximum: 0.15 if mode == 'sleep' else 3
                out, err = io.BytesIO(), io.BytesIO()
                try:
                    if failure:
                        with self.assertRaisesRegex(reader.Rejected, '^' + failure + '$'):
                            subject.capture_single(b'synthetic input', out, err)
                    else:
                        subject.capture_single(b'synthetic input', out, err)
                        self.assertEqual(out.getvalue(), expected)
                finally:
                    with mock.patch.object(reader, 'UID', os.geteuid()), mock.patch.dict(reader.TOOL_PATHS, postgres=os.path.realpath(sys.executable)):
                        subject.close_child()
                self.assertTrue(subject.child_reaped)
                self.assertTrue(subject.report['childClosed'])
                self.assertLessEqual(len(out.getvalue()), 1 << 20)
                self.assertLessEqual(len(err.getvalue()), 1 << 20)

    def test_mount_dispatch_failure_and_term_hup_route_through_owned_cleanup(self):
        for interrupt in (None, signal.SIGTERM, signal.SIGHUP):
            with self.subTest(interrupt=interrupt):
                reader.PENDING_SIGNAL = None
                scope = self.path / ('route-' + str(interrupt))
                scope.mkdir()
                lock = scope / 'lock'
                lock.write_bytes(b'')
                lock.chmod(0o600)
                events = []
                base = types.SimpleNamespace(Rejected=reader.Rejected, metadata=file_metadata)
                def write_new(path, raw, **_kwargs):
                    with open(path, 'xb') as file:
                        file.write(raw)
                    return {'path': str(path), 'sha256': reader.sha(raw), 'metadata': {}}
                base.write_new = write_new
                subject = reader.Reader(input_value(), base)
                subject.evidence, subject.fixture = scope / 'evidence', scope / 'fixture'
                subject.volume, subject.data = subject.fixture / 'pg', subject.fixture / 'pg/data'
                base.P = subject.evidence / 'private'
                subject.protected = lambda: {}
                subject.capacity = lambda: {}
                expected = snapshot()
                marker = {'database': 'goby_m6_systemd', 'databaseOid': 42, 'systemId': '123'}
                subject.pinned = lambda pin, *_args: reader.encoded(expected) if pin == reader.SNAPSHOT else reader.encoded(marker) + reader.encoded(expected)
                def command(label, _argv, _maximum):
                    self.assertEqual(label, 'mount-new-copy')
                    events.append('mount-dispatched')
                    if interrupt is None:
                        raise reader.Rejected('synthetic_mount_failure')
                    reader.PENDING_SIGNAL = interrupt
                    return b''
                base.run = command
                subject.close_child = lambda: events.append('child-cleanup')
                subject.close_volume = lambda: events.append('volume-cleanup')
                subject.check_originals = lambda: events.append('originals-check')
                with (mock.patch.object(reader, 'LOCK', lock),
                      mock.patch.object(reader, 'SEAL_SHA', reader.sha(b'fixture')),
                      mock.patch.object(reader, 'read_bootstrap', return_value=b'fixture'),
                      mock.patch.object(reader.pwd, 'getpwnam', return_value=types.SimpleNamespace(pw_uid=reader.UID, pw_gid=reader.GID)),
                      contextlib.redirect_stdout(io.StringIO())):
                    self.assertEqual(subject.run(), 1)
                self.assertEqual(events, ['mount-dispatched', 'child-cleanup', 'volume-cleanup', 'originals-check'])
                self.assertTrue(subject.report['mountAttempted'])
                self.assertTrue(subject.report['lockReleased'])
                self.assertEqual(subject.report['postgresStarts'], 0)
                self.assertEqual(subject.report['status'], 'failed')
                if interrupt is not None:
                    self.assertEqual(subject.report['interruptedBySignal'], interrupt)

    def test_mount_cleanup_uses_only_owned_identity_and_never_forces_unmount(self):
        subject = object.__new__(reader.Reader)
        subject.volume = self.path / 'volume'
        subject.volume.mkdir()
        subject.fixture = self.path
        subject.report = {'mountAttempted': True, 'underlyingVolume': {}}
        subject.child = None
        identity = {'id': '100', 'device': '0:100', 'root': '/', 'path': str(subject.volume)}
        subject.mount = dict(identity)
        subject.mount_identity = lambda: dict(identity)
        subject.base = types.SimpleNamespace(metadata=lambda _path: {})
        mounted = [True]
        commands = []
        def mountinfo(_path, *_args, **_kwargs):
            return '100 1 0:100 / ' + str(subject.volume) + ' rw - tmpfs synthetic rw\n' if mounted[0] else ''
        def unmount(label, argv):
            commands.append((label, argv))
            mounted[0] = False
        subject.command = unmount
        with mock.patch.object(reader.Path, 'read_text', mountinfo):
            subject.close_volume()
        self.assertEqual(commands, [('unmount-owned-copy', [reader.TOOL_PATHS['umount'], str(subject.volume)])])
        self.assertTrue(subject.report['volumeClosed'])
        mounted[0] = True
        subject.mount['id'] = 'changed'
        with mock.patch.object(reader.Path, 'read_text', mountinfo), self.assertRaises(reader.Rejected):
            subject.close_volume()
        self.assertEqual(len(commands), 1)


def main():
    global WORKDIR
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workdir', required=True)
    args = parser.parse_args()
    if os.name != 'posix' or os.geteuid() != 0 or not sys.flags.isolated or not sys.flags.dont_write_bytecode:
        raise SystemExit('Use the authorized remote environment with python3 -I -B.')
    WORKDIR = Path(args.workdir)
    if not WORKDIR.is_absolute() or WORKDIR.parent.resolve() != WORKDIR.parent or WORKDIR.exists():
        raise SystemExit('The workdir must be one new absolute private directory.')
    os.umask(0o077)
    WORKDIR.mkdir(mode=0o700)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ReaderTests))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
