#!/usr/bin/env python3
"""Finalize one retained Programs transition without repeating its service actions.

Execution requires an independently reviewed outer dispatcher that holds the
existing deployment flock through this process and its owned-resource closure.
The outer dispatcher owns memory/CPU limits, the transient unit, final descriptor
and process closure, and the actual original SSH/caller exit evidence. This
program neither acquires that lock nor certifies those external observations.
SSH_CONNECTION is an environment prerequisite, not proof of an actual SSH session.
"""
from __future__ import annotations

import argparse
from copy import deepcopy
from datetime import datetime, timezone
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import subprocess
import sys
import time
import types


R = Path('/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14')
C = Path('/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b')
FINAL = R / 'candidate-programs-finalization-01'
ORIGINAL = R / 'candidate-programs-transition-01'
LIMITS = {'maximumSeconds': 300, 'maximumSqlCommands': 16, 'maximumPublicRequests': 0,
          'stopCalls': 0, 'replaceCalls': 0, 'startCalls': 0}
ZERO_CALLS = {'stop': 0, 'replace': 0, 'start': 0}
ORIGINAL_CALLS = {'stop': 1, 'replace': 1, 'start': 1}
OUTER_CONTRACT = {
    'deploymentLock': '/opt/goby-test/exec-work-m3e/main-deployment-schema25.lock',
    'lockRequired': 'existing_nonblocking_exclusive_flock_held_through_owned_closure',
    'resourcesAndTransportOwner': 'independently_reviewed_outer_dispatcher',
    'memoryCpuAndTransientUnitLimitsRequired': True,
    'actualFinalizerAndOriginalSshExitEvidenceRequired': True,
    'finalDescriptorAndProcessClosureRequired': True,
    'sshConnectionEnvironmentProvesActualSession': False,
    'outerLockOrClosureObservedByThisProgram': False,
}
MAX_METADATA_COMMANDS = 64
CLEANUP_SECONDS = 15
PROVISION_SHOW = 'Id,LoadState,ActiveState,SubState,MainPID,InvocationID,Result,ExecMainStatus,ControlGroup'
LOADED_SHOW = ('Type,User,Group,Restart,ProtectSystem,ProtectHome,PrivateTmp,NoNewPrivileges,ReadWritePaths,'
               'InaccessiblePaths,FragmentPath,DropInPaths,EnvironmentFiles,ExecStart,TimeoutStopUSec,KillMode')


class FinalizationError(Exception):
    pass


def need(value, code):
    if not value:
        raise FinalizationError(code)


def failure_code(error):
    return getattr(error, 'code', str(error) if isinstance(error, FinalizationError) else type(error).__name__)


def pin2(value):
    need(isinstance(value, dict) and set(value) == {'path', 'sha256'} and isinstance(value['path'], str) and
         Path(value['path']).is_absolute() and str(Path(value['path'])) == value['path'] and '..' not in Path(value['path']).parts and
         not any(char in value['path'] for char in '\r\n\x00') and isinstance(value['sha256'], str) and
         re.fullmatch(r'[0-9a-f]{64}', value['sha256']), 'finalization_descriptor')
    return value


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def parse(raw):
    def pairs(rows):
        value = {}
        for key, item in rows:
            need(key not in value, 'finalization_duplicate_json_key')
            value[key] = item
        return value
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda unused: need(False, 'finalization_nonfinite_json'))


def signature(info):
    return tuple(getattr(info, key) for key in ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_gid', 'st_nlink',
                                               'st_size', 'st_mtime_ns', 'st_ctime_ns'))


def safe(path, directory=False):
    path = Path(path)
    need(path.is_absolute() and '..' not in path.parts, 'finalization_path')
    for node in (path, *path.parents):
        info = node.lstat()
        need(info.st_uid == 0 and not info.st_mode & 0o022 and not stat.S_ISLNK(info.st_mode), 'finalization_file_authority')
        need(stat.S_ISDIR(info.st_mode) if node != path or directory else stat.S_ISREG(info.st_mode), 'finalization_file_type')
    return path.lstat()


class Files:
    def __init__(self, started):
        self.started, self.created = started, False
        self.readbacks, self.writes, self.fd_closures = [], [], []
        self.bytes_read = 0
        self.critical = 0
        self.interrupted, self.closing = False, False

    def remaining(self):
        need(not self.interrupted, 'finalization_interrupted')
        seconds = self.started + LIMITS['maximumSeconds'] - CLEANUP_SECONDS - time.monotonic()
        need(seconds > 0, 'finalization_deadline')
        return seconds

    def close(self, fd, label, close=None):
        row = {'fd': fd, 'label': label, 'closeAttempted': True, 'closed': False, 'error': None}
        self.critical += 1
        try:
            try:
                (close or (lambda: os.close(fd)))()
            except BaseException as error:
                row['error'] = type(error).__name__
            try:
                os.fstat(fd)
            except OSError as error:
                row['closed'] = error.errno == errno.EBADF
            except BaseException as error:
                row['error'] = row['error'] or type(error).__name__
            self.fd_closures.append(row)
        finally:
            self.critical -= 1
        return row['closed'] and row['error'] is None

    def read(self, pin, maximum=64 << 20):
        self.remaining()
        pin2(pin)
        path = Path(pin['path'])
        need(path.is_relative_to('/opt/goby-test') and path.suffix.lower() not in ('.key', '.env', '.dump', '.sql'),
             'finalization_saved_read_scope')
        before = safe(path)
        need(before.st_nlink == 1 and before.st_size <= maximum, 'finalization_saved_read_bound')
        fd = None
        parts, count, digest = [], 0, hashlib.sha256()
        self.critical += 1
        try:
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            self.critical -= 1
            need(signature(os.fstat(fd)) == signature(before), 'finalization_saved_open_changed')
            while block := os.read(fd, 65536):
                self.remaining()
                count += len(block)
                self.bytes_read += len(block)
                need(count <= maximum and self.bytes_read <= 512 << 20, 'finalization_saved_read_budget')
                digest.update(block)
                parts.append(block)
            need(signature(os.fstat(fd)) == signature(before), 'finalization_saved_read_changed')
        finally:
            if fd is None:
                self.critical -= 1
                closed = False
            else:
                closed = self.close(fd, 'read:' + str(path))
        need(closed and signature(safe(path)) == signature(before) and count == before.st_size and digest.hexdigest() == pin['sha256'],
             'finalization_saved_pin_changed')
        self.readbacks.append({**pin, 'bytes': count})
        return b''.join(parts)

    def descriptor(self, pin):
        return parse(self.read(pin, 16 << 20))

    def sync(self, path):
        fd = None
        self.critical += 1
        try:
            fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            self.critical -= 1
            os.fsync(fd)
        finally:
            if fd is None:
                self.critical -= 1
                closed = False
            else:
                closed = self.close(fd, 'directory:' + str(path))
        need(closed, 'finalization_directory_descriptor')

    def create(self):
        self.remaining()
        need(not os.path.lexists(FINAL), 'finalization_output_collision')
        safe(R, True)
        os.mkdir(FINAL, 0o700)
        self.created = True
        os.mkdir(FINAL / 'private', 0o700)
        self.sync(FINAL)
        self.sync(R)

    def write(self, name, raw):
        if not self.closing:
            self.remaining()
        need(self.created and re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,100}', name) and isinstance(raw, bytes) and
             len(raw) <= 16 << 20, 'finalization_output_scope_or_bound')
        parent = FINAL / 'private'
        safe(parent, True)
        path = parent / name
        fd = None
        self.critical += 1
        try:
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            self.critical -= 1
            view = memoryview(raw)
            while view:
                if not self.closing:
                    self.remaining()
                count = os.write(fd, view)
                need(count > 0, 'finalization_output_short_write')
                view = view[count:]
            os.fsync(fd)
        finally:
            if fd is None:
                self.critical -= 1
                closed = False
            else:
                closed = self.close(fd, 'write:' + str(path))
        need(closed, 'finalization_output_descriptor')
        self.sync(parent)
        pin = {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest()}
        self.writes.append(pin)
        return pin

    def save(self, name, value):
        return self.write(name, encoded(value))


def process_identity(pid):
    path = Path('/proc') / str(pid)
    raw = (path / 'stat').read_bytes()
    end = raw.rfind(b')')
    need(end > 0 and raw[:raw.find(b'(')].strip() == str(pid).encode(), 'finalization_process_stat')
    fields = raw[end + 2:].decode('ascii').split()
    need(len(fields) >= 20, 'finalization_process_fields')
    return {'pid': pid, 'startTicks': fields[19], 'processGroup': int(fields[2]), 'sessionId': int(fields[3]),
            'cgroup': (path / 'cgroup').read_text(), 'bootId': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}


def group_members(group):
    entries = list(Path('/proc').iterdir())
    need(len(entries) <= 32768, 'finalization_process_inventory')
    rows = []
    for path in entries:
        if path.name.isdigit():
            try:
                row = process_identity(int(path.name))
                if row['processGroup'] == group:
                    rows.append(row)
            except (FileNotFoundError, ProcessLookupError):
                pass
    return rows


class Commands:
    """Own only explicitly selected read commands and their private process groups."""
    def __init__(self, files, environment, sql_queue, metadata_argv):
        self.files, self.environment = files, dict(environment)
        self.sql_queue, self.sql_count = sql_queue, 0
        self.metadata_argv = {tuple(argv) for argv in metadata_argv}
        self.sql_rows, self.metadata_rows, self.children, self.errors = [], [], [], []
        self.identity = process_identity(os.getpid())
        self.closing, self.registering, self.interrupted = False, False, False

    def close_child(self, child):
        process, original = child['process'], child['identity']
        def close_tracked_leader():
            if process.returncode is None:
                process.kill()
            process.wait(timeout=5)
        try:
            if original is None:
                # Popen owns the unreaped leader even when metadata acquisition failed.
                # Unknown group membership never authorizes signals to other members.
                close_tracked_leader()
                return not group_members(process.pid)
            for number in (signal.SIGTERM, signal.SIGKILL):
                members = group_members(process.pid)
                if not members:
                    process.wait(timeout=1)
                    return True
                need(all(row['processGroup'] == row['sessionId'] == process.pid and row['cgroup'] == self.identity['cgroup'] and
                         row['bootId'] == self.identity['bootId'] and
                         (row['pid'] != process.pid or row['startTicks'] == original['startTicks']) for row in members),
                     'finalization_refuse_foreign_command_group')
                try:
                    os.killpg(process.pid, number)
                except ProcessLookupError:
                    pass
                deadline = min(time.monotonic() + 5, self.files.started + LIMITS['maximumSeconds'])
                while time.monotonic() < deadline:
                    process.poll()
                    if not group_members(process.pid):
                        process.wait(timeout=1)
                        return True
                    time.sleep(0.05)
            close_tracked_leader()
            return not group_members(process.pid)
        except BaseException:
            try:
                close_tracked_leader()
            except BaseException as cleanup_error:
                self.errors.append({'stage': 'tracked-leader-closure', 'code': failure_code(cleanup_error)})
            raise

    def execute(self, label, argv, payload, *, sql):
        self.files.remaining()
        need(not self.interrupted, 'finalization_interrupted_before_command')
        rows = self.sql_rows if sql else self.metadata_rows
        prefix = ('sql-' if sql else 'metadata-') + '%03d' % (len(rows) + 1)
        row = {'label': label, 'pid': None, 'processGroup': None, 'outcome': 'unknown',
               'sqlOutcome': 'unknown' if sql else None, 'timedOut': False, 'descendantsRemained': False,
               'processGroupClosed': False, 'exitCode': None, 'stdinBytesWritten': 0,
               'stdinComplete': payload is None, 'outputLimitExceeded': False}
        rows.append(row)
        self.files.save(prefix + '-intent.json', {'argv': argv, 'stdinSha256': hashlib.sha256(payload).hexdigest() if payload else None,
                                                'readOnly': True, 'maximumStreamBytes': (1 << 20) if sql else 65536})
        environment = dict(self.environment)
        if sql:
            environment['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=10000 -c lock_timeout=3000'
        output = {'stdout': bytearray(), 'stderr': bytearray()}
        process = selector = child = None
        streams, selector_fd, problem = {}, None, None
        try:
            self.registering = True
            try:
                process = subprocess.Popen(argv, stdin=subprocess.PIPE if payload is not None else subprocess.DEVNULL,
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment,
                                           start_new_session=True, close_fds=True)
                child = {'process': process, 'identity': None, 'row': row}
                self.children.append(child)
                row.update(pid=process.pid, processGroup=process.pid)
                child['identity'] = process_identity(process.pid)
                need(child['identity']['processGroup'] == child['identity']['sessionId'] == process.pid and
                     child['identity']['cgroup'] == self.identity['cgroup'], 'finalization_command_ownership')
                row['startTicks'] = child['identity']['startTicks']
                selector = selectors.DefaultSelector()
                selector_fd = selector.fileno()
            finally:
                self.registering = False
            need(not self.interrupted, 'finalization_interrupted_during_registration')
            self.files.save(prefix + '-process.json', {**row, 'identity': child['identity']})
            for name in ('stdout', 'stderr'):
                stream = getattr(process, name)
                streams[name] = stream
                os.set_blocking(stream.fileno(), False)
                selector.register(stream, selectors.EVENT_READ, name)
            pending = memoryview(payload or b'')
            if process.stdin is not None:
                streams['stdin'] = process.stdin
                os.set_blocking(process.stdin.fileno(), False)
                selector.register(process.stdin, selectors.EVENT_WRITE, 'stdin')
            deadline = time.monotonic() + min(45 if sql else 10, self.files.remaining())
            while selector.get_map() or process.poll() is None:
                need(time.monotonic() < deadline, 'finalization_command_timeout')
                self.files.remaining()
                for key, unused_mask in selector.select(0.1):
                    stream, name = key.fileobj, key.data
                    if name == 'stdin':
                        try:
                            count = os.write(stream.fileno(), pending[:4096]) if pending else 0
                            row['stdinBytesWritten'] += count
                            pending = pending[count:]
                        except BrokenPipeError:
                            raise FinalizationError('finalization_command_stdin_closed')
                        if not pending:
                            row['stdinComplete'] = row['stdinBytesWritten'] == len(payload)
                            selector.unregister(stream)
                            fd = stream.fileno()
                            need(self.files.close(fd, prefix + ':stdin', stream.close), 'finalization_stdin_descriptor')
                            streams.pop('stdin')
                    else:
                        maximum = (1 << 20) if sql else 65536
                        block = os.read(stream.fileno(), min(65536, maximum - len(output[name]) + 1))
                        if not block:
                            selector.unregister(stream)
                            fd = stream.fileno()
                            need(self.files.close(fd, prefix + ':' + name, stream.close), 'finalization_stream_descriptor')
                            streams.pop(name)
                        else:
                            output[name].extend(block)
                            if len(output[name]) > maximum:
                                row['outputLimitExceeded'] = True
                                raise FinalizationError('finalization_command_output_bound')
            row['exitCode'] = process.wait(timeout=1)
            row['descendantsRemained'] = bool(group_members(process.pid))
        except BaseException as error:
            problem = error
            row['timedOut'] = failure_code(error) in ('finalization_command_timeout', 'finalization_deadline')
        finally:
            self.closing = True
            try:
                if process is not None:
                    for name in ('stdin', 'stdout', 'stderr'):
                        try:
                            stream = getattr(process, name)
                            if stream is not None and not stream.closed:
                                need(self.files.close(stream.fileno(), prefix + ':' + name, stream.close), 'finalization_stream_close_failed')
                        except BaseException as error:
                            self.errors.append({'stage': prefix + '-stream-close', 'code': failure_code(error)})
                            problem = problem or error
                if selector is not None:
                    if not self.files.close(selector_fd, prefix + ':selector', selector.close):
                        self.errors.append({'stage': prefix + '-selector-close', 'code': 'finalization_selector_close_failed'})
                        problem = problem or FinalizationError('finalization_selector_close_failed')
            finally:
                try:
                    if child is not None:
                        try:
                            row['processGroupClosed'] = self.close_child(child)
                            row['exitCode'] = process.returncode
                        except BaseException as error:
                            self.errors.append({'stage': prefix + '-closure', 'code': failure_code(error)})
                            problem = problem or error
                finally:
                    self.closing = False
        for name in ('stdout', 'stderr'):
            row[name] = self.files.write(prefix + '.' + name, bytes(output[name]))
        if problem is None and row['exitCode'] == 0 and row['stdinComplete'] and not row['outputLimitExceeded'] and not row['timedOut'] and not row['descendantsRemained'] and row['processGroupClosed']:
            row['outcome'] = 'acknowledged'
            if sql:
                row['sqlOutcome'] = 'acknowledged'
        self.files.save(prefix + '-result.json', row)
        if problem is not None:
            raise problem
        need(row['outcome'] == 'acknowledged', 'finalization_read_command_not_acknowledged')
        return bytes(output['stdout']), bytes(output['stderr'])

    def sql_command(self, label, argv, payload=None):
        need(self.sql_count < len(self.sql_queue) == LIMITS['maximumSqlCommands'], 'finalization_sql_budget')
        expected = self.sql_queue[self.sql_count]
        need(label == expected['label'] and argv == expected['argv'] and payload == expected['payload'], 'finalization_sql_not_selected')
        self.sql_count += 1
        text = payload.decode()
        wire = payload if text.startswith('BEGIN READ ONLY; ') else ('BEGIN READ ONLY; ' + text + ' COMMIT;').encode()
        stdout, unused = self.execute(label, argv, wire, sql=True)
        return stdout.decode().strip()

    def metadata_run(self, argv, **options):
        need(isinstance(argv, list) and tuple(argv) in self.metadata_argv and len(self.metadata_rows) < MAX_METADATA_COMMANDS and
             set(options) <= {'capture_output', 'timeout', 'check', 'env'} and options.get('capture_output') is True and
             options.get('timeout') == 10 and options.get('check', False) in (False, True) and
             ('env' not in options or options['env'] == self.environment), 'finalization_metadata_not_selected')
        stdout, stderr = self.execute('systemctl-show', argv, None, sql=False)
        return subprocess.CompletedProcess(argv, 0, stdout, stderr)

    def close_all(self):
        self.closing = True
        for child in self.children:
            if not child['row']['processGroupClosed']:
                try:
                    child['row']['processGroupClosed'] = self.close_child(child)
                except BaseException as error:
                    self.errors.append({'stage': 'final-command-closure', 'code': failure_code(error)})
        return all(child['row']['processGroupClosed'] for child in self.children)

    def complete(self):
        need(self.sql_count == len(self.sql_rows) == LIMITS['maximumSqlCommands'] and not self.errors and
             all(row['closed'] and row['error'] is None for row in self.files.fd_closures) and
             all(row['outcome'] == 'acknowledged' and row['processGroupClosed'] and row['exitCode'] == 0 and
                 row['stdinComplete'] and not row['outputLimitExceeded'] and not row['timedOut'] and not row['descendantsRemained']
                 for row in self.sql_rows + self.metadata_rows),
             'finalization_command_closure_incomplete')


def build_capture_sql_queue(candidate, catalog, snapshot_sql):
    source, recovery = candidate['database'], candidate['recoveryDatabase']
    socket = '/opt/goby-audited-candidate-' + candidate['runId'] + '/postgres/socket'
    def item(label, sql, database='postgres'):
        return {'label': label, 'argv': ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/psql',
                '-X', '--no-password', '-h', socket, '-p', str(candidate['ports']['postgres']), '-U', 'postgres',
                '-d', database, '-v', 'ON_ERROR_STOP=1', '-Atq'], 'payload': sql.encode()}
    def cluster():
        return item('cluster-identity', "SELECT current_setting('data_directory'),current_setting('port'),(pg_control_system()).system_identifier,current_setting('server_version_num');")
    def read_only(label, database, query):
        return item(label, 'BEGIN READ ONLY; ' + query + '; COMMIT;', database)
    key = 4919415424202458201
    lease = "SELECT COALESCE(json_agg(json_build_object('backendPid',a.pid,'user',a.usename,'database',a.datname,'application',a.application_name,'clientHost',host(a.client_addr),'clientPort',a.client_port,'backendStart',a.backend_start,'mode',l.mode,'granted',l.granted) ORDER BY a.pid),'[]'::json) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.locktype='advisory' AND l.classid=" + str(key >> 32) + '::oid AND l.objid=' + str(key & 0xffffffff) + "::oid AND l.objsubid=1 AND l.database=(SELECT oid FROM pg_database WHERE datname='" + source + "')"
    snapshot = snapshot_sql(catalog)
    objects = "SELECT json_build_object('relations',(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace),'functions',(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace),'types',(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace),'schemas',(SELECT json_agg(nspname ORDER BY nspname) FROM pg_namespace WHERE nspname!~'^pg_' AND nspname<>'information_schema'));"
    queue = [cluster(), read_only('epoch-read-only', source, lease), cluster(), read_only('transition-read-only', source, snapshot),
             cluster(), read_only('transition-read-only', recovery, snapshot)]
    for name in (source, recovery):
        identity = "SELECT json_build_object('database',d.datname,'databaseOid',d.oid::bigint,'roleOid',r.oid::bigint,'owner',pg_get_userbyid(d.datdba),'superuser',r.rolsuper,'createDb',r.rolcreatedb,'createRole',r.rolcreaterole,'replication',r.rolreplication,'bypassRls',r.rolbypassrls,'inherit',r.rolinherit,'login',r.rolcanlogin) " + f"FROM pg_database d JOIN pg_roles r ON r.rolname='{name}' WHERE d.datname='{name}';"
        queue.extend([cluster(), item('database-identity-' + name, identity), cluster(), item('database-objects-' + name, objects, name)])
    queue.extend([cluster(), read_only('epoch-read-only', source, lease)])
    return queue


def load_module(files, pin, name):
    pin2(pin)
    need(Path(pin['path']).is_relative_to(R) and not Path(pin['path']).is_relative_to(FINAL), 'finalization_code_scope')
    raw = files.read(pin, 4 << 20)
    module = types.ModuleType(name)
    module.__file__ = pin['path']
    exec(compile(raw, pin['path'], 'exec'), module.__dict__)
    return module


def prohibited(*unused_args, **unused_options):
    raise FinalizationError('finalization_mutation_or_http_forbidden')


def prepare_reader(runtime, operator, files, value, records, validator_pin):
    transition, after, failure = records['transition'], records['after'], records['failure']
    job = operator.ProgramsSuccessor.__new__(operator.ProgramsSuccessor)
    job.initialize(runtime, transition, value['transitionInput'], value['transitionHelper'], validator_pin)
    job.output, job.private, job.remaining = FINAL, FINAL / 'private', files.remaining
    job.save = files.save
    job.stage = 'finalization_saved_context'
    job.calls, job.public_count = dict(ZERO_CALLS), 0
    job.previous_epoch = runtime.validate_epoch(files.descriptor(transition['previousEpoch']))
    job.seed = files.descriptor(runtime.SEED)
    job.previous_binding = runtime.validate_seed_runtime_binding(files.descriptor(transition['previousBinding']),
        transition['previousEpoch'], job.previous_epoch, job.seed)
    job.current_runtime = runtime.load_programs_previous_runtime(transition['currentRuntime'], transition, job.previous_epoch,
        files.descriptor, files.read)
    job.old = deepcopy(job.previous_epoch['candidate'])
    job.old_binary_sha = runtime.PROGRAMS_OLD_BINARY
    reference_pin = job.previous_epoch['after']
    job.recovery_master = runtime.programs_recovery_master_authority(files.descriptor(reference_pin), reference_pin)
    job.reviewed_state = files.descriptor(transition['reviewedState'])
    job.reviewed_summary = files.descriptor(transition['reviewedSummary'])
    job.retention_review = files.descriptor(job.reviewed_summary['retentionReview'])
    job.catalog = files.descriptor(transition['compiledCatalog'])
    job.a.validate_catalog(job.catalog, files.descriptor(transition['newSourceManifest']), transition['compiledCatalog'])
    job.sessions = runtime.programs_sessions(after['source'])
    process = {key: item for key, item in after['candidateAfter'].items() if key != 'listener'}
    need(process['pid'] == 1907978 and process['startTicks'] == '34901535', 'finalization_current_candidate_identity')
    captured = failure['failureCapture']
    candidate = deepcopy(job.old)
    installed = {'path': str(C / 'install/goby'), 'sha256': transition['newBinary']['sha256']}
    candidate.update(kind='audited-candidate-runtime-state', input=value['transitionInput'], productInput=value['transitionInput'],
        originalProvision=runtime.PROVISION, seedProvenance=runtime.SEED, currentSourceManifest=transition['newSourceManifest'],
        binary=installed, backendReport=transition['newFullReport'], bootstrapExecuted=True,
        sourceState={'users': 8, 'schema': 28, 'migrations': 28})
    candidate.pop('setupToken', None)
    candidate['processes']['server'] = deepcopy(captured['serverProperties'])
    candidate['serverIdentity'] = deepcopy(captured['serverIdentity'])
    candidate['listener'].update(pid=process['pid'], socketInode=after['candidateAfter']['listener']['socketInode'])
    job.candidate = candidate
    p = job.pmod.Provision({'runId': candidate['runId'], 'ports': candidate['ports']}, value['transitionInput'], transition['helpers']['provision'])
    p.private = job.private
    p.argv = {'server': [str(C / 'install/goby')], 'postgres': ['/usr/lib/postgresql/17/bin/postgres', '-D', str(C / 'postgres/data'),
              '-c', 'config_file=' + str(C / 'postgres/server.conf')]}
    p.pg_identity, p.pg_version = candidate['postgresIdentity'], candidate['postgresVersionNum']
    reader = runtime.RecoveredEpochReader.__new__(runtime.RecoveredEpochReader)
    reader.productEpoch = reader.epoch = job.previous_epoch
    reader.currentRuntime, reader.current = deepcopy(job.current_runtime), deepcopy(job.current_runtime['current'])
    reader.current.update(candidateProcess=deepcopy(process), serverProperties=deepcopy(candidate['processes']['server']),
        serverIdentity=deepcopy(candidate['serverIdentity']), listener=deepcopy(candidate['listener']), lease=deepcopy(after['leaseAfter']))
    reader.modules, reader.provision, reader.observationOnly = job.modules, p, False
    reader.manifest = reader.candidate = candidate
    job.p = p
    job.io = types.SimpleNamespace(reader=reader, candidate=candidate, provision=p, pin=reader.pin)
    job.epoch = {'candidateProcess': deepcopy(process), 'postgresProcess': deepcopy(after['postgresAfter'])}
    job.before, job.runtime_replaced, job.new_lease, job.source_pins = after, True, deepcopy(after['leaseAfter']), {}
    metadata = []
    for unit in set(p.units.values()) | set(after['protected']):
        metadata.append(['/usr/bin/systemctl', 'show', unit, '--property=' + PROVISION_SHOW])
    for unit in p.units.values():
        metadata.append(['/usr/bin/systemctl', 'show', unit, '--property=' + LOADED_SHOW])
    hosting = job.reviewed_state['hostingBefore']['unit']
    metadata.append(['/usr/bin/systemctl', 'show', hosting['Id'], '--property=' + ','.join(hosting)])
    commands = Commands(files, job.pmod.ENV, build_capture_sql_queue(candidate, job.catalog, job.a.snapshot_sql), metadata)
    p.command, p.command_responsibilities = commands.sql_command, commands.sql_rows
    proxy = types.SimpleNamespace(run=commands.metadata_run)
    operator.subprocess = job.pmod.subprocess = runtime.subprocess = proxy
    for name in ('run', 'open', 'products', 'public_read', 'capture_failure_evidence', 'adopt_runtime_context'):
        setattr(job, name, prohibited)
    for name in ('start', 'cleanup', 'write', 'mkdir'):
        setattr(p, name, prohibited)
    return job, commands


def publish_epoch(runtime, files, job, value, records, source_pin, validator_pin, input_pin, fresh, fresh_pin):
    before, after, transition = records['before'], records['after'], records['transition']
    antecedent = {'kind': 'audited-programs-transition-finalization', 'version': 1,
        'status': 'fresh_checked_awaiting_live_acceptance', 'publicationRoot': str(FINAL),
        'input': input_pin, 'producer': source_pin, 'validator': validator_pin,
        **{key: value[key] for key in ('transitionInput', 'transitionHelper', 'transitionRuntime', 'failure', 'before', 'after',
                                     'sourceBefore', 'sourceAfter', 'rolloverReview', 'closureReview')},
        'publicEvidence': deepcopy(records['publicEvidence']), 'freshState': fresh_pin, 'freshSource': job.source_pins['fresh'],
        'calls': dict(ZERO_CALLS), 'originalCalls': dict(ORIGINAL_CALLS), 'publicRequests': 0, 'originalPublicRequests': 3,
        'commandResponsibilities': deepcopy(job.p.command_responsibilities)}
    antecedent_pin = files.save('antecedent.json', antecedent)
    verified = runtime.validate_programs_finalization_antecedent(antecedent, antecedent_pin, None, files.descriptor, files.read)
    need(verified['pin'] == antecedent_pin and verified['record'] == antecedent, 'finalization_antecedent_readback')
    authorization = verified['diagnosticAuthorization']
    installed = deepcopy(job.candidate['binary'])
    compared = runtime.compare_programs_preservation(before, after, installed_binary=installed, diagnostic_authority=authorization)
    proof = {'before': value['before'], 'after': value['after'], 'sourceBefore': value['sourceBefore'], 'sourceAfter': value['sourceAfter'],
        'reviewedState': transition['reviewedState'], 'installedBinary': installed, **compared,
        'diagnostics': runtime.compare_diagnostics(before['diagnostics'], after['diagnostics'], diagnostic_authority=authorization),
        'oldBinaryCopy': records['failure']['oldBinaryCopy'], 'oldBinaryFacts': before['fixedFiles'][str(C / 'install/goby')],
        'previousLease': files.save('lease-before.json', before['leaseBefore']),
        'diagnosticsProof': {label: files.save('diagnostics-' + label + '.json', state['diagnostics']) for label, state in (('before', before), ('after', after))},
        'unitLogsProof': {label: files.save('unit-logs-' + label + '.json', state['unitLogs']) for label, state in (('before', before), ('after', after))}}
    current = deepcopy(job.candidate)
    for slot in ('source', 'recovery'):
        need(after['databases'][slot] == before['databases'][slot] == job.reviewed_state['databases'][slot], 'finalization_database_facts')
        current['databases'][slot]['afterStart'] = deepcopy(after['databases'][slot])
    source = {'archiveSha256': transition['newSourceArchive']['sha256'], 'sourceManifest': transition['newSourceManifest'],
        'binary': installed, 'fullReport': transition['newFullReport'], 'schema': 28, 'artifactReceipt': transition['newArtifactReceipt'],
        'buildManifest': transition['newBuildManifest'], 'sourceBridge': transition['sourceBridge']}
    epoch = {'kind': 'audited-candidate-runtime-epoch', 'version': 4, 'status': 'running_awaiting_live_acceptance',
        'operationKind': 'programs_successor', 'transitionInput': value['transitionInput'], 'productInput': value['transitionInput'],
        'configurationInput': job.previous_epoch['configurationInput'], 'previousEpoch': transition['previousEpoch'],
        'previousCurrentRuntime': transition['currentRuntime'], 'reviewedState': transition['reviewedState'],
        'reviewedSummary': transition['reviewedSummary'], 'transitionHelper': value['transitionHelper'], 'runtimeHelper': validator_pin,
        'originalProvision': runtime.PROVISION, 'seedProvenance': runtime.SEED, 'currentSource': source, 'candidate': current,
        'candidateProcess': deepcopy(job.epoch['candidateProcess']), 'postgresProcess': after['postgresAfter'], 'lease': after['leaseAfter'],
        'before': value['before'], 'after': value['after'], 'sourceBefore': value['sourceBefore'], 'sourceAfter': value['sourceAfter'],
        'preservation': files.save('preservation.json', proof), 'calls': dict(ORIGINAL_CALLS), 'helpers': transition['helpers'],
        'candidateAdmissionComplete': False, 'finalization': antecedent_pin}
    runtime.validate_epoch(epoch)
    runtime.validate_programs_runtime_change(job.current_runtime, epoch, before, after)
    runtime.validate_programs_finalization_antecedent(antecedent, antecedent_pin, epoch, files.descriptor, files.read)
    epoch_pin = files.save('runtime-epoch.json', epoch)
    seed = job.seed
    binding = {'kind': 'audited-candidate-seed-runtime-binding', 'version': 4, 'runtimeEpoch': epoch_pin, 'originalSeed': runtime.SEED,
        'seedExecutor': seed['helper'], 'seedInput': seed['input'], 'seedSessionAddendum': runtime.SEED_ADDENDUM, 'admission02': runtime.ADMISSION02,
        **{key: seed[key] for key in ('serverId', 'admin', 'actors', 'controlQ', 'catalog', 'catalogFile', 'actualCatalogDtos', 'libraries', 'roots', 'resources')},
        'seedCleanup': seed['cleanup'], 'currentSessions': job.sessions, 'previousBinding': transition['previousBinding'],
        'previousCurrentRuntime': transition['currentRuntime'], 'reviewedState': transition['reviewedState'], 'reviewedSummary': transition['reviewedSummary'],
        'priorCloseout': transition['priorCloseout'], 'priorSource': transition['priorSource'], 'candidateAdmissionComplete': False}
    runtime.validate_seed_runtime_binding(binding, epoch_pin, epoch, seed)
    return {'antecedent': antecedent_pin, 'runtimeEpoch': epoch_pin, 'seedRuntimeBinding': files.save('seed-runtime-binding.json', binding)}


def main():
    need(sys.platform == 'linux' and os.geteuid() == 0 and sys.flags.isolated and sys.flags.dont_write_bytecode and
         len(os.environ.get('SSH_CONNECTION', '').split()) == 4, 'finalization_isolated_root_ssh_required')
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    for name in ('input', 'input-sha256', 'source-sha256', 'runtime-helper', 'runtime-helper-sha256'):
        parser.add_argument('--' + name, required=True)
    args = parser.parse_args()
    started = time.monotonic()
    files = Files(started)
    source_pin = {'path': str(Path(__file__).absolute()), 'sha256': args.source_sha256}
    validator_pin = {'path': args.runtime_helper, 'sha256': args.runtime_helper_sha256}
    input_pin = {'path': args.input, 'sha256': args.input_sha256}
    for pin in (source_pin, validator_pin, input_pin):
        pin2(pin)
        need(Path(pin['path']).is_relative_to(R) and not Path(pin['path']).is_relative_to(FINAL), 'finalization_bootstrap_scope')
    files.read(source_pin, 4 << 20)
    runtime = load_module(files, validator_pin, 'programs_finalization_runtime')
    need(runtime.PROGRAMS_FINALIZATION_ROOT == FINAL and runtime.PROGRAMS_FINALIZATION_LIMITS == LIMITS, 'finalization_runtime_limits')
    value = files.descriptor(input_pin)
    runtime.validate_programs_finalization_input(value)
    commands = job = None
    stage, result, exit_code = 'saved_evidence', None, 2
    final_closure_errors = []
    closing = False
    interrupted = []
    def expired(number, unused_frame):
        interrupted.append(number)
        files.interrupted = True
        if commands is not None:
            commands.interrupted = True
        # Throw only at explicit safe points; asynchronous exceptions can skip finally entry.
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, max(0.1, started + LIMITS['maximumSeconds'] - CLEANUP_SECONDS - time.monotonic()))
    try:
        files.read(value['transitionRuntime'], 4 << 20)
        operator = load_module(files, value['transitionHelper'], 'programs_finalization_original_operator')
        records = runtime.load_programs_finalization_inputs(value, files.descriptor, files.read)
        job, commands = prepare_reader(runtime, operator, files, value, records, validator_pin)
        files.create()
        files.save('intent.json', {'kind': 'audited-programs-finalization-intent', 'version': 1, 'input': input_pin,
            'producer': source_pin, 'validator': validator_pin, 'originalTransitionInput': value['transitionInput'],
            'originalFailure': value['failure'], 'execution': commands.identity, 'budgets': LIMITS,
            'requiredOuterExecutionContract': OUTER_CONTRACT,
            'calls': ZERO_CALLS, 'publicRequests': 0, 'automaticRetry': False, 'automaticRecovery': False})
        stage = job.stage = 'fresh_read_only_state'
        fresh, fresh_pin = job.capture('fresh', stopped=False)
        runtime.compare_programs_finalization_fresh(records['after'], fresh)
        need(job.calls == ZERO_CALLS and job.public_count == 0 and not interrupted, 'finalization_operation_counter_or_signal_changed')
        commands.complete()
        job.pin()
        commands.complete()
        stage = 'publish_finalization'
        published = publish_epoch(runtime, files, job, value, records, source_pin, validator_pin, input_pin, fresh, fresh_pin)
        commands.complete()
        need(not interrupted, 'finalization_interrupted_before_result')
        need(all(row['closed'] and row['error'] is None for row in files.fd_closures), 'finalization_descriptor_closure')
        result = {'kind': 'audited-programs-transition-finalization-result', 'version': 1,
            'status': 'running_awaiting_live_acceptance', 'input': input_pin, 'producer': source_pin, 'validator': validator_pin,
            **published, 'freshState': fresh_pin, 'freshSource': job.source_pins['fresh'], 'calls': dict(ZERO_CALLS),
            'originalCalls': dict(ORIGINAL_CALLS), 'publicRequests': 0, 'originalPublicRequests': 3,
            'commandResponsibilities': deepcopy(commands.sql_rows), 'metadataCommands': deepcopy(commands.metadata_rows),
            'execution': commands.identity, 'originalClosureReview': value['closureReview'],
            'requiredOuterExecutionContract': OUTER_CONTRACT,
            'ownCommandGroupsClosed': True, 'outerUnitAndOriginalSshClosureRequired': True,
            'outerUnitClosure': None, 'originalSshExit': None, 'ownProcessExitObserved': False,
            'fdClosuresBeforeResult': deepcopy(files.fd_closures), 'readbacks': deepcopy(files.readbacks),
            'candidateAdmissionComplete': False, 'clientAcceptance': False, 'automaticRetry': False, 'automaticRecovery': False,
            'elapsedSeconds': time.monotonic() - started}
        result['receipt'] = files.save('result.json', result)
        need(not interrupted, 'finalization_interrupted_during_result_publication')
        exit_code = 0
    except BaseException as error:
        closing = True
        files.closing = True
        signal.setitimer(signal.ITIMER_REAL, 0)
        closed = commands is None
        if commands is not None:
            try:
                closed = commands.close_all()
            except BaseException as cleanup_error:
                final_closure_errors.append({'stage': 'exception-command-closure', 'code': failure_code(cleanup_error)})
        result = {'kind': 'audited-programs-transition-finalization-failure', 'version': 1,
            'status': 'finalization_failed_evidence_retained', 'stage': stage, 'code': failure_code(error),
            'input': input_pin, 'producer': source_pin, 'validator': validator_pin, 'originalFailure': value['failure'],
            'calls': dict(job.calls) if job is not None else dict(ZERO_CALLS), 'publicRequests': job.public_count if job is not None else 0,
            'commandResponsibilities': deepcopy(commands.sql_rows) if commands is not None else [],
            'metadataCommands': deepcopy(commands.metadata_rows) if commands is not None else [],
            'ownCommandGroupsClosed': closed, 'cleanupErrors': deepcopy(commands.errors) if commands is not None else [],
            'fdClosures': deepcopy(files.fd_closures), 'retainedOutputs': deepcopy(files.writes),
            'outerUnitAndOriginalSshClosureRequired': True, 'ownProcessExitObserved': False,
            'requiredOuterExecutionContract': OUTER_CONTRACT,
            'candidateAdmissionComplete': False, 'clientAcceptance': False, 'automaticRetry': False, 'automaticRecovery': False}
        if files.created:
            try:
                result['receipt'] = files.save('failure.json', result)
            except BaseException as receipt_error:
                result['receiptError'] = failure_code(receipt_error)
    finally:
        closing = True
        files.closing = True
        signal.setitimer(signal.ITIMER_REAL, 0)
        if commands is not None:
            try:
                if not commands.close_all():
                    final_closure_errors.append({'stage': 'final-command-closure', 'code': 'finalization_owned_group_retained'})
            except BaseException as cleanup_error:
                final_closure_errors.append({'stage': 'final-command-closure', 'code': failure_code(cleanup_error)})
        try:
            need(all(row['closed'] and row['error'] is None for row in files.fd_closures), 'finalization_final_descriptor_closure')
        except BaseException as cleanup_error:
            final_closure_errors.append({'stage': 'final-descriptor-closure', 'code': failure_code(cleanup_error)})
        if final_closure_errors:
            exit_code = 2
    result['finalClosureErrors'] = final_closure_errors
    if final_closure_errors:
        result['operationResultStatus'] = result['status']
        result['status'] = 'finalization_final_closure_failed_evidence_retained'
    result['finalCommandClosure'] = {
        'sql': deepcopy(commands.sql_rows) if commands is not None else [],
        'metadata': deepcopy(commands.metadata_rows) if commands is not None else [],
        'errors': deepcopy(commands.errors) if commands is not None else [],
    }
    result['fdClosuresAfterResult'] = deepcopy(files.fd_closures)
    result['descriptorClosureScope'] = 'Explicit finalizer file descriptors and command streams; outer final process/FD closure remains required.'
    result['prospectiveFinalizerExitCode'] = exit_code
    result['finalizerExitCodeObserved'] = False
    print(json.dumps(result), flush=True)
    return exit_code


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'status': 'finalization_bootstrap_rejected', 'code': failure_code(error),
                          'calls': ZERO_CALLS, 'publicRequests': 0, 'automaticRetry': False}), flush=True)
        raise SystemExit(2)
