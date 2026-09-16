"""Close and preserve only the fixed native scan/HTTP capacity fixture.

Definitions only; there is no actor main and importing this file performs no
work. The single parent controller owns admission and its deployment lock. It
must first join readers, finish workload cancellation/credential cleanup, call
Application.stop, and consume the final SQL slot. This module never stops APP,
opens SQL/HTTP, starts a unit, or releases the parent's lock.

Closure(base, support, units, corpus, context, preparation_report,
        application_record, *, deadline, budget, record=None).close()

deadline is the original absolute end of the whole 900-second closure phase,
including time already spent by the parent. It is never refreshed. base.P must
be the parent's new E/private/closure-commands directory; the parent must restore
the normal bounded base.run transport after leaving preparation. A base.run
timeout can use eleven further seconds to close its own command process group.
Its child receives a file-size limit before exec, so neither command stream can
grow beyond the reserved bound. The parent's limits and the inherited capture's
process creation, waiting, group closure and failure handling remain unchanged.

budget must contain rawEvidenceBytes, rawLimitBytes=384 MiB,
rootAllocationLimitBytes=2560 MiB, and rootReserveBytes=1024 MiB. The parent owns
the cumulative raw counter through entry, including previous HTTP, SQL, command,
metric and application/PG log bytes, even if a previous stream was removed.
This module adds its command streams and JSON records without resetting that
counter. Root allocation is independently measured from the actual E/F trees
(excluding the PG tmpfs), then charged for simultaneous archive/copy outputs.
The parent must keep its declared cleanup capture reserve writable: mandatory
owned-unit controls are still attempted when the budget contract is missing or
already invalid, and that condition makes the closure fail. Later preservation
metadata commands have a prospective raw/root allowance as well as readback
accounting; no archive or removal is admitted from an invalid budget.
The config environment is retained by whole-directory rename and checked by
hash only. Four byte copies cover the binary and three unique unit files; the
environment is the fifth original path with distinct retention evidence.

Missing budget or business evidence does not gate attempts to close owned PG
and anchor. Missing ownership never authorizes a stop or file removal. Partial
preparation is preserved where its recorded creation identities suffice; every
missing or retained obligation remains explicit and cannot pass full closure.
The parent must not subsequently replay preparation cleanup after this module.
The sole handoff exception is status
closure_authority_unavailable_resources_retained together with an empty
infrastructureStopMethodsEntered list: this module has not entered either
infrastructure stop method. The parent may then use its original preparation
object once within the original deadline. Any nonempty list forbids replaying
preparation cleanup, regardless of whether a stop command was dispatched.
"""

import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import resource
import stat
import tarfile
import time


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
PREFIX = 'goby-native-capacity-20260915'
APP, PGUNIT, ANCHOR = (PREFIX + suffix for suffix in ('-app.service', '-postgres.service', '-net.service'))
PRIVATE = E / 'private'
RECORDS, COMMANDS, RETAINED = PRIVATE / 'closure-records', PRIVATE / 'closure-commands', PRIVATE / 'retained'
UNIT_ROOT, PG = Path('/run/systemd/system'), F / 'pg'
BINARY, ENVIRONMENT, APP_LOG = F / 'bin/goby', F / 'config/goby.env', F / 'log/application.jsonl'
RUNTIME = Path('/run') / PREFIX
TREE_PATHS = {name: F / name for name in ('state', 'cache', 'log', 'config', 'media')}
STABLE = ('device', 'inode', 'uid', 'gid', 'mode', 'type')
ROOT_LIMIT, RAW_LIMIT, ROOT_RESERVE = 5 << 29, 384 << 20, 1 << 30
PG_LIMIT, STREAM_LIMIT, JSON_LIMIT = 768 << 20, 1 << 20, 8 << 20
GROUP_CLOSE_SECONDS, FINAL_RESERVE_SECONDS = 11, 20
STOP_SECONDS = {PGUNIT: 90, ANCHOR: 15}
EVIDENCE_EXCLUSIONS = ('private/retained', 'private/closure-records', 'private/closure-commands',
                       'private/controller.stdout', 'private/controller.stderr')
FORBIDDEN = {'upgrade-main-schema25.py', 'dispose-source41-resource-full-failed-pair.py',
             'test-dispose-source41-resource-full-failed-pair.py'}


class ClosureError(RuntimeError):
    """A bounded nonsecret closure rejection."""


def _need(value, code):
    if not value:
        raise ClosureError(code)


class _OutputLimitedSubprocess:
    """Add a child-only write boundary to the inherited file capture."""

    def __init__(self, original, stream_limit):
        self._original = original
        self._stream_limit = stream_limit

    def __getattr__(self, name):
        return getattr(self._original, name)

    def Popen(self, *args, **kwargs):
        _need(kwargs.get('preexec_fn') is None, 'closure_capture_preexec_changed')
        stream_limit = self._stream_limit

        def limit_child_files():
            inherited = resource.getrlimit(resource.RLIMIT_FSIZE)
            limit = min([stream_limit] + [value for value in inherited if value != resource.RLIM_INFINITY])
            resource.setrlimit(resource.RLIMIT_FSIZE, (limit, limit))

        return self._original.Popen(*args, **dict(kwargs, preexec_fn=limit_child_files))


def _capture_with_output_limit(base, capture, label, argv, timeout, stream_limit):
    """Keep the pinned capture while bounding writes to each inherited stream.

    Closure starts only after the parent's readers are joined. Replace this
    isolated base module's subprocess reference for one synchronous capture;
    never patch the shared subprocess module or change the parent's rlimits.
    The kernel enforces RLIMIT_FSIZE on the child's regular output files before
    a write can cross the bound, including writes by inherited descendants.
    """
    _need(type(stream_limit) is int and stream_limit > 0, 'closure_capture_stream_limit')
    original = base.subprocess
    base.subprocess = _OutputLimitedSubprocess(original, stream_limit)
    try:
        return capture(label, argv, timeout)
    finally:
        base.subprocess = original


def _error(error):
    message = str(error)
    return message if re.fullmatch('[A-Za-z0-9_.-]{1,160}', message) else type(error).__name__


def _encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def _same(left, right):
    return _encoded(left) == _encoded(right)


def _metadata(path):
    info = Path(path).lstat()
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'type': stat.S_IFMT(info.st_mode), 'bytes': info.st_size,
            'links': info.st_nlink, 'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}


def _signature(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def _key_name(path):
    name = Path(path).name.lower()
    return name.endswith('.key') or 'master' in name


def _fsync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _projection(pid):
    _need(type(pid) is int and pid > 1, 'closure_process_pid')
    root = Path('/proc') / str(pid)
    fields = (root / 'stat').read_text().rsplit(') ', 1)[1].split()
    return {'pid': pid, 'startTicks': fields[19], 'cgroup': (root / 'cgroup').read_text().strip()}


def _lifetime_gone(process):
    try:
        current = _projection(process['pid'])
    except (FileNotFoundError, ProcessLookupError):
        return True
    return current['startTicks'] != process['startTicks']


class Closure:
    def __init__(self, base, support, units, corpus, context, preparation_report,
                 application_record, *, deadline, budget=None, record=None):
        _need(type(deadline) in (int, float) and math.isfinite(deadline), 'closure_absolute_deadline')
        _need(context is None or isinstance(context, dict), 'closure_context_shape')
        _need(isinstance(preparation_report, dict) and (application_record is None or isinstance(application_record, dict)),
              'closure_source_record_shape')
        _need(record is None or (isinstance(record, dict) and not record), 'closure_record_not_new')
        self.base, self.support, self.units, self.corpus = base, support, units, corpus
        self._original_run = base.run
        self.ctx, self.prepared, self.application = context or {}, preparation_report, application_record or {}
        self.deadline, self.budget = float(deadline), budget
        self.record = {} if record is None else record
        self.record.update(kind='native-scan-http-capacity-closure', version=1, scope=str(E), status='not_started',
            absoluteDeadlineMonotonic=self.deadline, deadlineRefreshes=0, errors=[], missing=[], closedUnits={},
            infrastructureStopMethodsEntered=[],
            commandReferences=[], archives={}, moves=[], fileRetentions=[], removedFiles=[], removedEmptyDirectories=[],
            applicationStopCommands=0, serviceStarts=0, httpRequests=0, sqlConnections=0, processSignals=0,
            parentLockReleased=False, measurementAccepted=False, installationAccepted=False, wholeM6Accepted=False,
            keyContentsRead=False, environmentDecoded=False, sourcePreparationStatus=self.prepared.get('status'))
        self._started = False
        self._operation_deadline = self.deadline
        self._receipts, self._stop_dispatched, self._foreign = {}, set(), set()
        self._root_baseline, self._root_new_bytes, self._new_raw_bytes = None, 0, 0
        self._mandatory_controls = True
        self._created = {}
        self._pg_log_start = {}
        for row in self.prepared.get('createdPaths', []):
            if isinstance(row, dict) and isinstance(row.get('path'), str) and isinstance(row.get('metadata'), dict):
                self._created.setdefault(row['path'], []).append(row['metadata'])

    def _remember(self, phase, error):
        row = {'phase': phase, 'code': _error(error)}
        if 'firstError' not in self.record:
            self.record['firstError'] = row
        if len(self.record['errors']) < 256:
            self.record['errors'].append(row)

    def _missing(self, subject, reason):
        row = {'subject': str(subject), 'reason': reason}
        if row not in self.record['missing']:
            self.record['missing'].append(row)

    def _remaining(self, maximum, reserve=0):
        remaining = min(self.deadline, self._operation_deadline) - time.monotonic() - reserve
        _need(remaining > 0, 'closure_deadline')
        return min(maximum, remaining)

    def _budget_contract(self):
        _need(isinstance(self.budget, dict) and set(self.budget) ==
              {'rawEvidenceBytes', 'rawLimitBytes', 'rootAllocationLimitBytes', 'rootReserveBytes'} and
              all(type(value) is int for value in self.budget.values()) and
              0 <= self.budget['rawEvidenceBytes'] <= RAW_LIMIT and self.budget['rawLimitBytes'] == RAW_LIMIT and
              self.budget['rootAllocationLimitBytes'] == ROOT_LIMIT and self.budget['rootReserveBytes'] == ROOT_RESERVE,
              'closure_budget_contract_missing_or_changed')
        return self.budget

    def _raw_check(self, addition=0):
        budget = self._budget_contract()
        _need(budget['rawEvidenceBytes'] + self._new_raw_bytes + addition <= RAW_LIMIT, 'closure_raw_evidence_limit')

    def _reserve(self, addition=0):
        self._remaining(1)
        fs = os.statvfs(E)
        _need(fs.f_bavail * fs.f_frsize >= ROOT_RESERVE + addition, 'closure_root_free_reserve')
        if self._root_baseline is not None:
            _need(self._root_baseline + self._root_new_bytes + addition <= ROOT_LIMIT, 'closure_root_allocation_limit')

    def _scope(self):
        self._remaining(1)
        _need(all(getattr(module, 'E', None) == E and getattr(module, 'F', None) == F
                  for module in (self.support, self.units, self.corpus)), 'closure_loaded_scope')
        _need((self.support.APP, self.support.PGUNIT, self.support.ANCHOR) == (APP, PGUNIT, ANCHOR) and
              (self.units.APP, self.units.PGUNIT, self.units.ANCHOR) == (APP, PGUNIT, ANCHOR) and
              self.corpus.ROOT == TREE_PATHS['media'] and Path(self.base.P) == COMMANDS,
              'closure_dependency_or_command_scope')
        _need(not self.ctx or (self.ctx.get('scope') == str(E) and self.ctx.get('fixtureRoot') == str(F)),
              'closure_context_scope')
        for path in (E, PRIVATE, COMMANDS):
            row = _metadata(path)
            _need(path.resolve() == path and row['type'] == stat.S_IFDIR and row['uid'] == row['gid'] == 0 and
                  row['mode'] == 0o700, 'closure_private_directory')
        _need(os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net') and
              os.readlink('/proc/self/ns/mnt') == os.readlink('/proc/1/ns/mnt'), 'closure_host_namespaces')
        if 'bootId' in self.ctx:
            _need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self.ctx['bootId'], 'closure_boot_changed')
        dependencies = self.ctx.get('sourceDependencies', self.prepared.get('preparationAdapter', {}).get('sources'))
        _need(isinstance(dependencies, dict), 'closure_source_dependencies_missing')
        for name, module in (('support', self.support), ('units', self.units), ('corpus', self.corpus)):
            path = PRIVATE / ('capacity-' + name + '.py')
            _need(Path(module.__file__) == path and name in dependencies, 'closure_loaded_source_path')
            info = _metadata(path)
            _need(info['uid'] == info['gid'] == 0 and info['mode'] == 0o600 and info['links'] == 1 and
                  info['type'] == stat.S_IFREG, 'closure_loaded_source_metadata')
            self.support.pinned(self.base, dependencies[name], 2 << 20, path)
            _need(_metadata(path) == info, 'closure_loaded_source_changed')
        lock = self.prepared.get('lockMetadata', self.base.report.get('lockMetadata'))
        if lock is not None:
            _need(self.base.metadata(self.base.LOCK) == lock, 'closure_parent_lock_identity_changed')
            self.record['parentLockMetadata'] = lock
        _need(not os.path.lexists(RECORDS) and not os.path.lexists(RETAINED), 'closure_already_consumed')
        RECORDS.mkdir(mode=0o700)
        self.record['recordsDirectory'] = {'path': str(RECORDS), 'metadata': _metadata(RECORDS)}
        for name in ('pg.stdout', 'pg.stderr'):
            path = PRIVATE / name
            if not os.path.lexists(path):
                self._pg_log_start[str(path)] = None
                continue
            info = _metadata(path)
            _need(info['uid'] == info['gid'] == 0 and info['mode'] == 0o600 and info['type'] == stat.S_IFREG and
                  info['links'] == 1 and info['bytes'] <= RAW_LIMIT, 'closure_pg_log_entry_metadata')
            self._pg_log_start[str(path)] = info

    def _save(self, path, value):
        path = Path(path)
        _need(path.parent == RECORDS and path.name.endswith('.json') and
              re.fullmatch('[a-z0-9_.-]{1,160}', path.name), 'closure_receipt_path')
        _need(str(path) not in self._receipts, 'closure_receipt_reused')
        raw = _encoded(value)
        _need(len(raw) <= JSON_LIMIT, 'closure_receipt_size')
        self._raw_check(len(raw))
        self._reserve(len(raw) + 4096)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            os.fchmod(fd, 0o600)
            offset = 0
            while offset < len(raw):
                self._remaining(1)
                count = os.write(fd, raw[offset:])
                _need(count > 0, 'closure_receipt_short_write')
                offset += count
                self._new_raw_bytes += count
                if self._root_baseline is not None:
                    self._root_new_bytes += count
            os.fsync(fd)
        finally:
            os.close(fd)
        if self._root_baseline is not None:
            self._root_new_bytes += 4096
        result = self._hash_file(path, maximum=JSON_LIMIT)
        self._receipts[str(path)] = result
        return result

    def _save_optional(self, name, value):
        try:
            return self._save(RECORDS / name, value)
        except BaseException as error:
            self._remember('save_' + name, error)
            return {'path': str(RECORDS / name), 'persisted': False}

    def _check_tool(self, name):
        tool = self.ctx.get('tools', {}).get(name)
        expected = '/usr/bin/systemctl' if name == 'systemctl' else '/usr/bin/umount'
        _need(isinstance(tool, dict) and Path(tool['path']).resolve() == Path(tool['resolvedPath']) == Path(expected).resolve() and
              self.base.metadata(Path(tool['resolvedPath'])) == tool['metadata'], 'closure_control_tool_identity')
        self._hash_file(Path(tool['resolvedPath']), {'path': tool['resolvedPath'], 'bytes': tool['bytes'], 'sha256': tool['sha256']}, 256 << 20)
        return tool['path']

    def _command(self, label, argv, maximum, *, stop=None):
        return self._transport(label, argv, maximum, stop=stop)

    def _transport(self, label, argv, timeout=30, *, stop=None):
        """Bound even helper-issued metadata commands through the same clock."""
        systemctl = self._check_tool('systemctl')
        is_systemctl = len(argv) >= 2 and Path(argv[0]).resolve() == Path(systemctl).resolve()
        readonly = is_systemctl and argv[1] in ('show', 'list-units', 'list-unit-files')
        owned_stop = (is_systemctl and len(argv) == 3 and argv[1] == 'stop' and argv[2] in (PGUNIT, ANCHOR) and
                      label == 'capacity-closure-stop-' + argv[2] and
                      self.record['closedUnits'].get(argv[2], {}).get('ownershipEstablished') is True)
        reload_unit = is_systemctl and argv[1:] == ['daemon-reload'] and label == 'capacity-closure-daemon-reload'
        unmount = len(argv) == 3 and argv[1:] == ['--', str(PG)] and label == 'capacity-closure-ordinary-unmount' and \
            Path(argv[0]).resolve() == Path(self._check_tool('umount')).resolve()
        _need(readonly or owned_stop or reload_unit or unmount, 'closure_command_scope')
        if owned_stop:
            stop = argv[2]
            _need(stop not in self._stop_dispatched, 'closure_stop_already_dispatched')
        stream_bound = 8 << 20 if is_systemctl and argv[1] in ('list-units', 'list-unit-files') else 256 << 10
        try:
            self._raw_check(2 * stream_bound)
            self._reserve(2 * stream_bound + 8192)
        except BaseException as error:
            if not self._mandatory_controls:
                raise
            self._remember('mandatory_control_budget', error)
        first = len(self.base.report['commands'])
        try:
            return _capture_with_output_limit(self.base, self._original_run, label, argv,
                                              self._remaining(timeout, GROUP_CLOSE_SECONDS), stream_bound)
        finally:
            rows = self.base.report['commands'][first:]
            indexes = [first + index for index, row in enumerate(rows) if row.get('label') == label]
            dispatched = any(type(self.base.report['commands'][index].get('pid')) is int and
                             self.base.report['commands'][index]['pid'] > 1 for index in indexes)
            self.record['commandReferences'].append({'label': label, 'commandIndexes': indexes, 'dispatched': dispatched})
            if stop is not None and dispatched:
                self._stop_dispatched.add(stop)
                self.record['closedUnits'][stop]['stopDispatched'] = True
            for row in rows:
                added = sum(row.get(channel, {}).get('bytes', 0) for channel in ('stdout', 'stderr'))
                self._new_raw_bytes += added
                if self._root_baseline is not None:
                    self._root_new_bytes += added + 8192
                if any(row.get(channel, {}).get('bytes', 0) > stream_bound for channel in ('stdout', 'stderr')):
                    self._remember('command_stream_bound', ClosureError('closure_command_output_exceeded_declared_bound'))
            try:
                self._raw_check()
            except BaseException as error:
                self._remember('command_raw_budget', error)

    def _settle_pg_logs(self):
        growth = 0
        observations = []
        for name, before in self._pg_log_start.items():
            path = Path(name)
            if not os.path.lexists(path):
                _need(before is None, 'closure_pg_log_disappeared')
                observations.append({'path': name, 'absent': True})
                continue
            info = _metadata(path)
            _need(path.resolve() == path and info['uid'] == info['gid'] == 0 and info['mode'] == 0o600 and
                  info['type'] == stat.S_IFREG and info['links'] == 1 and info['bytes'] <= RAW_LIMIT,
                  'closure_pg_log_final_metadata')
            if before is not None:
                _need(all(info[key] == before[key] for key in STABLE) and info['bytes'] >= before['bytes'],
                      'closure_pg_log_identity_changed')
            added = info['bytes'] - (before['bytes'] if before is not None else 0)
            growth += added
            observations.append({'path': name, 'growthBytes': added, 'metadata': info})
        self._new_raw_bytes += growth
        self.record['postgresStopLogGrowth'] = {'bytes': growth, 'files': observations}
        self._raw_check()

    def _show(self, name):
        _need(name in (APP, PGUNIT, ANCHOR), 'closure_unit_scope')
        tool = self._check_tool('systemctl')
        fields = tuple(dict.fromkeys((*self.support.SERVICE_PROPERTIES, 'TimeoutStartUSec')))
        label = 'closure-show-%04d-%s' % (len(self.record['commandReferences']) + 1, name)
        raw = self._command(label, [tool, 'show', name, '--no-pager', '--property=' + ','.join(fields)], 10)
        pairs = [line.split('=', 1) for line in raw.decode('utf-8', 'strict').splitlines()]
        row = dict(pairs)
        _need(all(len(pair) == 2 for pair in pairs) and len(pairs) == len(row) and set(row) == set(fields),
              'closure_unit_property_shape')
        return row

    def _members(self, name):
        group = Path('/sys/fs/cgroup/system.slice') / name
        if not group.exists():
            return set()
        paths = list(group.rglob('cgroup.procs'))
        _need(len(paths) <= 64, 'closure_cgroup_count')
        return {int(value) for path in paths for value in path.read_text().split()}

    def _unit_pin(self, name):
        candidates = []
        if name in self.ctx.get('unitFiles', {}):
            candidates.append(self.ctx['unitFiles'][name])
        prepared = self.prepared.get('ownedUnits', {}).get(name, {})
        if 'file' in prepared:
            candidates.append(prepared['file'])
        if name == APP and 'unit' in self.ctx.get('installed', {}):
            candidates.append(self.ctx['installed']['unit'])
        _need(candidates and all(_same(pin, candidates[0]) for pin in candidates), 'closure_unit_pin_missing_or_conflicting')
        pin = candidates[0]
        _need(pin['path'] == str(UNIT_ROOT / name) and pin['metadata']['uid'] == pin['metadata']['gid'] == 0 and
              pin['metadata']['type'] == stat.S_IFREG and pin['metadata']['links'] == 1 and pin['metadata']['mode'] == 0o644,
              'closure_unit_file_scope')
        self.support.check_file(self.base, pin)
        return pin

    def _inherit_stop(self, name):
        labels = {'cleanup-stop-' + name, 'stop-' + name, 'capacity-closure-stop-' + name}
        for source in (self.prepared.get('commands', []), self.base.report.get('commands', [])):
            if any(row.get('label') in labels and type(row.get('pid')) is int and row['pid'] > 1 for row in source):
                self._stop_dispatched.add(name)
                self.record['closedUnits'][name]['priorStopDispatched'] = True
                return

    def _infra_authority(self, name, row, prepared, pin):
        _need(row['Id'] == name and row['LoadState'] == 'loaded' and row['FragmentPath'] == pin['path'] and
              row['DropInPaths'] == '' and row['ControlGroup'] in ('', '/system.slice/' + name) and row['NRestarts'] == '0',
              'closure_owned_unit_origin')
        _need(prepared.get('startIntent') is True and prepared.get('startSubmitted') is True and
              isinstance(prepared.get('beforeStart'), dict) and prepared['beforeStart']['MainPID'] == '0' and
              prepared['beforeStart']['ActiveState'] == 'inactive' and type(prepared.get('startIntentMonotonicUs')) is int and
              prepared['startIntentMonotonicUs'] > 0 and 'bootId' in self.ctx and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self.ctx['bootId'], 'closure_start_dispatch_authority')
        invocation = row['InvocationID']
        if invocation:
            _need(re.fullmatch('[0-9a-f]{32}', invocation) and int(invocation, 16) != 0 and
                  invocation != prepared['beforeStart']['InvocationID'] and
                  ('startInvocation' not in prepared or invocation == prepared['startInvocation']), 'closure_invocation_replaced')
        prior = self.record['closedUnits'][name].get('ownership', prepared.get('ownership'))
        if row['MainPID'] == '0':
            _need(not self._members(name) and row['ExecMainPID'].isdigit() and
                  row['ExecMainStartTimestampMonotonic'].isdigit(), 'closure_empty_main_pid_with_members')
            if prior is not None:
                _need(row['ExecMainPID'] in ('0', str(prior['pid'])) and invocation in ('', prior['invocationId']) and
                      _lifetime_gone(prior), 'closure_terminal_owner_changed')
            else:
                started = int(row['ExecMainStartTimestampMonotonic'])
                _need(started == 0 or started >= prepared['startIntentMonotonicUs'], 'closure_unobserved_start_causality')
            return {'kind': 'owned_unit_start_without_live_process', 'processOwnershipEstablished': False,
                    'unit': row, 'startIntentMonotonicUs': prepared['startIntentMonotonicUs']}
        _need(row['MainPID'].isdigit() and int(row['MainPID']) > 1 and row['ExecMainPID'] == row['MainPID'] and invocation and
              row['ExecMainStartTimestampMonotonic'].isdigit() and
              int(row['ExecMainStartTimestampMonotonic']) >= prepared['startIntentMonotonicUs'], 'closure_live_start_causality')
        process = _projection(int(row['MainPID']))
        _need(process['cgroup'] == '0::/system.slice/' + name and process == _projection(process['pid']),
              'closure_process_projection_changed')
        ownership = {**process, 'invocationId': invocation, 'execMainStartMonotonicUs': row['ExecMainStartTimestampMonotonic']}
        _need(prior is None or _same(prior, ownership), 'closure_owned_lifetime_replaced')
        self.record['closedUnits'][name]['ownership'] = ownership
        return {'kind': 'owned_unit_process', 'ownership': ownership, 'processOwnershipEstablished': True}

    def _normal_profile(self, name, row, pin):
        key = 'postgres' if name == PGUNIT else 'anchor'
        identity = self.ctx.get(key)
        _need(isinstance(identity, dict) and 'process' in identity and 'unit' in identity, 'closure_normal_profile_unavailable')
        expected = identity['process']
        native = self.ctx['tools']['postgres' if name == PGUNIT else 'sleep']
        self._hash_file(Path(native['resolvedPath']), {'path': native['resolvedPath'], 'bytes': native['bytes'],
                                                     'sha256': native['sha256']}, 256 << 20)
        _need(self.base.metadata(Path(native['resolvedPath'])) == native['metadata'] and
              self.base.process_identity(expected['pid']) == expected and expected['uid'] == (103 if name == PGUNIT else 0) and
              expected['exe'] == native['resolvedPath'] and expected['cgroup'] == '0::/system.slice/' + name and
              expected['networkNamespace'] == self.ctx.get('networkNamespace') and
              row['MainPID'] == str(expected['pid']) and row['InvocationID'] == identity['unit']['InvocationID'] and
              row['ExecMainStartTimestampMonotonic'] == identity['unit']['ExecMainStartTimestampMonotonic'] and
              row['ActiveState'] == 'active' and row['SubState'] == 'running' and row['Restart'] == 'no' and
              row['User'] == row['Group'] == ('postgres' if name == PGUNIT else 'root'), 'closure_normal_profile_changed')
        return self.support.UnitReference(self.base, unit=name, unit_file=pin, process=expected,
            invocation_id=row['InvocationID'], start_usec=int(row['ExecMainStartTimestampMonotonic']),
            boot_id=self.ctx['bootId'], remaining=self._remaining, writer=self._save)

    def _stop_infrastructure(self, name):
        item = self.record['closedUnits'].setdefault(name, {'normalExit': False, 'physicalClosed': False, 'stopDispatched': False})
        prepared = self.prepared.get('ownedUnits', {}).get(name, {})
        self._inherit_stop(name)
        tail = FINAL_RESERVE_SECONDS + (STOP_SECONDS[ANCHOR] + GROUP_CLOSE_SECONDS + 15 if name == PGUNIT else 0)
        previous_deadline = self._operation_deadline
        control_end = self.deadline - tail
        window = control_end
        self._operation_deadline = control_end
        reference = None
        try:
            if name == ANCHOR and PGUNIT in self._foreign:
                raise ClosureError('closure_foreign_postgres_dependency_retained')
            row = self._show(name)
            if not prepared:
                _need(not os.path.lexists(UNIT_ROOT / name) and row['LoadState'] == 'not-found' and
                      row['MainPID'] == '0' and not self._members(name), 'closure_unowned_unit_present')
                item.update(neverCreated=True, physicalClosed=True, terminal=row)
                self._missing(name, 'unit_was_not_created')
                return
            pin = self._unit_pin(name)
            if not prepared.get('startSubmitted'):
                _need(row['Id'] == name and row['LoadState'] in ('loaded', 'not-found') and
                      row['MainPID'] == '0' and row['ActiveState'] == 'inactive' and not self._members(name) and
                      row['FragmentPath'] in ('', pin['path']), 'closure_unstarted_unit_not_idle')
                item.update(neverStarted=True, physicalClosed=True, terminal=row)
                self._missing(name, 'unit_start_was_not_dispatched')
                return
            for attempt in range(3):
                try:
                    authority = self._infra_authority(name, row, prepared, pin)
                    break
                except (FileNotFoundError, ProcessLookupError):
                    item['processDisappearanceObservations'] = item.get('processDisappearanceObservations', 0) + 1
                    if attempt == 2:
                        raise
                    row = self._show(name)
            item['stopAuthority'] = authority
            item['ownershipEstablished'] = True
            if row['MainPID'] == '0' and row['ActiveState'] in ('inactive', 'failed') and name in self._stop_dispatched:
                item.update(alreadyClosed=True, physicalClosed=True, cgroupEmpty=True, terminal=row,
                            normalExitEvidence='not_acquired_by_this_closure')
                return
            if name not in self._stop_dispatched:
                # Evidence work cannot consume the complete stop/group-close
                # allowance. A submitted but unobserved queued start still
                # receives the one owned-unit cancellation stop below.
                optional_end = min(time.monotonic() + 12,
                    control_end - STOP_SECONDS[name] - GROUP_CLOSE_SECONDS - 12)
                self._operation_deadline = optional_end
                try:
                    _need(optional_end > time.monotonic(), 'closure_normal_evidence_time_unavailable')
                    reference = self._normal_profile(name, row, pin)
                    item['stopReferenceRecord'] = reference.record
                    reference.acquire()
                    item['beforeReceipt'] = reference.persist(RECORDS / ('reference-' + name + '-before.json'))
                except BaseException as error:
                    self._remember('normal_profile_' + name, error)
                    item['normalProfileRejected'] = _error(error)
                self._save_optional('stop-' + name + '-intent.json', {'unit': name, 'authority': authority})
                self._operation_deadline = control_end
                window = min(control_end, time.monotonic() + STOP_SECONDS[name] + GROUP_CLOSE_SECONDS + 12)
                if 'stopDeadlineMonotonic' in prepared:
                    window = min(window, prepared['stopDeadlineMonotonic'])
                item['absoluteStopDeadlineMonotonic'] = window
                item['stopTransportMaximumSeconds'] = STOP_SECONDS[name]
                item['commandGroupCloseReserveSeconds'] = GROUP_CLOSE_SECONDS
                self._operation_deadline = window
                if reference is not None and reference.record['referenceConfirmed']:
                    try:
                        reference.mark_stop()
                    except BaseException as error:
                        reference.error('mark_stop', error)
                        self._remember('mark_stop_' + name, error)
                try:
                    tool = self._check_tool('systemctl')
                    self._command('capacity-closure-stop-' + name, [tool, 'stop', name], STOP_SECONDS[name], stop=name)
                except BaseException as error:
                    item['stopControlError'] = _error(error)
                    self._remember('stop_' + name, error)
                finally:
                    if reference is not None:
                        reference.set_stop_submitted(name in self._stop_dispatched)
            else:
                window = min(control_end, time.monotonic() + STOP_SECONDS[name] + GROUP_CLOSE_SECONDS + 12)
                if 'stopDeadlineMonotonic' in prepared:
                    window = min(window, prepared['stopDeadlineMonotonic'])
                item['absoluteStopDeadlineMonotonic'] = window
                self._operation_deadline = window
            if reference is not None and reference.record['referenceConfirmed']:
                try:
                    reference.capture_terminal()
                    item['terminalReceipt'] = reference.persist_terminal(RECORDS / ('reference-' + name + '-terminal.json'))
                except BaseException as error:
                    reference.error('terminal_evidence', error)
                    self._remember('terminal_reference_' + name, error)
        finally:
            if reference is not None:
                reference.release()
                item['referenceReleased'] = reference.record['connectionClosed']
                try:
                    item['referenceReceipt'] = reference.persist(RECORDS / ('reference-' + name + '.json'))
                    item['normalExitEvidence'] = self.support.check_stop_reference(reference.record, reference.expected)
                    item['normalExit'] = 'stopControlError' not in item and 'normalProfileRejected' not in item
                except BaseException as error:
                    self._remember('reference_release_' + name, error)
            self._operation_deadline = previous_deadline
        self._operation_deadline = window
        try:
            while True:
                row = self._show(name)
                self.support.check_file(self.base, pin)
                try:
                    self._infra_authority(name, row, prepared, pin)
                except (FileNotFoundError, ProcessLookupError):
                    item['processDisappearanceObservations'] = item.get('processDisappearanceObservations', 0) + 1
                    self._remaining(1, GROUP_CLOSE_SECONDS)
                    continue
                item['terminalObservation'] = row
                if row['MainPID'] == '0' and row['ActiveState'] in ('inactive', 'failed') and not self._members(name):
                    item.update(physicalClosed=True, cgroupEmpty=True, terminal=row)
                    break
                self._remaining(1, GROUP_CLOSE_SECONDS)
                time.sleep(0.05)
        finally:
            self._operation_deadline = previous_deadline

    def _application_closed(self):
        row = self._show(APP)
        if APP in self.ctx.get('unitFiles', {}) or 'unit' in self.ctx.get('installed', {}):
            pin = self._unit_pin(APP)
            _need(row['LoadState'] == 'loaded' and row['FragmentPath'] == pin['path'] and not row['DropInPaths'],
                  'closure_application_source_changed')
        else:
            _need(not os.path.lexists(UNIT_ROOT / APP) and row['LoadState'] == 'not-found', 'closure_application_pin_missing')
        _need(row['Id'] == APP and row['MainPID'] == '0' and row['ActiveState'] in ('inactive', 'failed') and
              not self._members(APP) and not os.path.lexists(RUNTIME), 'closure_application_not_physically_closed')
        known = dict(self.application.get('knownProcesses', {}))
        for key in ('startOwnership', 'cleanupOwnership'):
            if key in self.application:
                owner = self.application[key]
                known[str(owner['pid']) + ':' + owner['startTicks']] = owner
        _need(all(_lifetime_gone(owner) for owner in known.values()), 'closure_application_owned_lifetime_retained')
        self.record['application'] = {'terminal': row, 'physicalClosed': True, 'sourceRecordClosed': self.application.get('closed') is True,
            'sourceNormalExit': self.application.get('normalExit') is True, 'knownLifetimesGone': sorted(known),
            'newStopSubmitted': False}
        if self.application.get('normalExit') is not True:
            self._missing(APP, 'normal_application_exit_not_proved_by_parent')

    def _namespace_absent(self):
        namespace = self.ctx.get('networkNamespace')
        if namespace is None:
            anchor = self.prepared.get('ownedUnits', {}).get(ANCHOR, {})
            namespace = anchor.get('identity', {}).get('process', {}).get('networkNamespace') or \
                anchor.get('ownershipObservation', {}).get('process', {}).get('networkNamespace')
        if namespace is None:
            if not any(row.get('startSubmitted') for row in self.prepared.get('ownedUnits', {}).values()) and \
                    not self.application.get('startDispatched'):
                self.record.update(privateNamespaceGone=True, privateNamespaceNeverCreated=True)
                return True
            self._missing('private_network_namespace', 'identity_was_not_established')
            return False
        _need(namespace != os.readlink('/proc/self/ns/net'), 'closure_private_namespace_is_host')
        holders = set()
        for root in Path('/proc').iterdir():
            self._remaining(1)
            if not root.name.isdigit():
                continue
            try:
                if os.readlink(root / 'ns/net') == namespace:
                    holders.add(int(root.name))
                for path in (root / 'fd').iterdir():
                    try:
                        if os.readlink(path) == namespace:
                            holders.add(int(root.name))
                    except (FileNotFoundError, ProcessLookupError):
                        pass
            except (FileNotFoundError, ProcessLookupError):
                pass
        _need(not holders, 'closure_private_namespace_retained')
        self.record['privateNamespaceGone'] = True
        return True

    def _hash_file(self, path, expected=None, maximum=ROOT_LIMIT):
        path = Path(path)
        _need(path.is_absolute() and path.resolve() == path and path.name not in FORBIDDEN,
              'closure_file_path_boundary')
        before = _metadata(path)
        exception = path == PG / 'data/postmaster.opts'
        if exception:
            _need(before['uid'] == 103 and before['gid'] == 106 and before['mode'] == 0o600 and
                  before['type'] == stat.S_IFREG and before['links'] == 1, 'closure_postmaster_options_exception')
        _need(not _key_name(path) or exception, 'closure_key_content_forbidden')
        _need(before['type'] == stat.S_IFREG and before['links'] == 1 and not before['mode'] & 0o022 and
              0 <= before['bytes'] <= maximum, 'closure_file_metadata')
        digest, total = hashlib.sha256(), 0
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            opened = os.fstat(fd)
            _need((opened.st_dev, opened.st_ino) == (before['device'], before['inode']), 'closure_file_open_changed')
            while True:
                self._remaining(1)
                block = os.read(fd, 1 << 20)
                if not block:
                    break
                total += len(block)
                _need(total <= maximum, 'closure_file_read_bound')
                digest.update(block)
            _need(_signature(os.fstat(fd)) == _signature(opened) and _metadata(path) == before and
                  total == before['bytes'], 'closure_file_changed_during_read')
        finally:
            os.close(fd)
        result = {'path': str(path), 'sha256': digest.hexdigest(), 'bytes': total}
        _need(expected is None or _same(result, expected), 'closure_file_pin_changed')
        return result

    def inventory(self, root, owners, maximum, *, keys=False, exclusions=(), tree_name=None):
        """Capture a fixed source tree without following links or reading keys."""
        root = Path(root)
        permitted = {E, PG, *TREE_PATHS.values(), *(RETAINED / name for name in TREE_PATHS)}
        _need(root in permitted and root.resolve() == root, 'closure_inventory_scope')
        device = root.lstat().st_dev
        files, directories, logical, allocated = {}, {}, 0, 0
        def visit(path):
            nonlocal logical, allocated
            self._remaining(1)
            relative = path.relative_to(root).as_posix()
            before = _metadata(path)
            _need(path.resolve() == path and before['type'] == stat.S_IFDIR and before['device'] == device and
                  (before['uid'], before['gid']) in owners and not before['mode'] & 0o022 and
                  len(relative.encode('utf-8')) <= 512, 'closure_inventory_directory')
            if root == E:
                _need(before['uid'] == before['gid'] == 0 and before['mode'] == 0o700, 'closure_private_tree_directory')
            directories[relative] = before
            allocated += path.lstat().st_blocks * 512
            for child in sorted(path.iterdir()):
                name = child.relative_to(root).as_posix()
                if name in exclusions:
                    continue
                info = _metadata(child)
                _need(child.resolve() == child and info['device'] == device and child.name not in FORBIDDEN and
                      '\\' not in name and len(name.encode('utf-8')) <= 512, 'closure_inventory_child_boundary')
                if info['type'] == stat.S_IFDIR:
                    visit(child)
                    continue
                allowed = (info['uid'], info['gid']) in owners
                if tree_name == 'log' and name == APP_LOG.name:
                    expected = self.ctx.get('appLog', {}).get('metadata')
                    _need(isinstance(expected, dict) and all(info[key] == expected[key] for key in STABLE) and
                          info['uid'] == info['gid'] == 0 and info['mode'] == 0o600, 'closure_application_log_source')
                    allowed = True
                _need(allowed and info['type'] == stat.S_IFREG and info['links'] == 1 and not info['mode'] & 0o022,
                      'closure_inventory_file')
                if root == E:
                    _need(info['uid'] == info['gid'] == 0 and info['mode'] == 0o600, 'closure_private_tree_file')
                logical += info['bytes']
                allocated += child.lstat().st_blocks * 512
                _need(logical <= maximum and len(files) < 20000 and len(directories) <= 20000,
                      'closure_inventory_budget')
                item = {'metadata': info, 'bytes': info['bytes']}
                if _key_name(child) and child != PG / 'data/postmaster.opts':
                    _need(keys and tree_name == 'state', 'closure_key_outside_state')
                    item['statOnly'] = True
                else:
                    item['sha256'] = self._hash_file(child, maximum=maximum)['sha256']
                _need(_metadata(child) == info, 'closure_inventory_file_changed')
                files[name] = item
            _need(_metadata(path) == before, 'closure_inventory_directory_changed')
        visit(root)
        return {'root': str(root), 'files': files, 'directories': directories, 'bytes': logical,
                'allocatedBytes': allocated, 'exclusions': list(exclusions)}

    def _allocated_tree(self, root, *, skip_pg=False):
        if not os.path.lexists(root):
            return 0
        device, total, entries = E.stat().st_dev, 0, 0
        def visit(path):
            nonlocal total, entries
            self._remaining(1)
            if skip_pg and path == PG:
                return
            info = path.lstat()
            _need(path.resolve() == path and info.st_dev == device and
                  (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)), 'closure_root_storage_boundary')
            entries += 1
            total += info.st_blocks * 512
            _need(entries <= 40000 and total <= ROOT_LIMIT, 'closure_root_storage_limit')
            if stat.S_ISDIR(info.st_mode):
                for child in path.iterdir():
                    visit(child)
        visit(Path(root))
        return total

    def _created_root(self, path, extra=None):
        path = Path(path)
        candidates = list(self._created.get(str(path), []))
        if extra is not None:
            candidates.append(extra)
        _need(candidates, 'closure_created_directory_authority_missing')
        current = _metadata(path)
        _need(path.resolve() == path and current['type'] == stat.S_IFDIR and
              all(all(current[key] == expected[key] for key in STABLE) for expected in candidates),
              'closure_created_directory_changed')
        return current

    def _source_tree(self, name):
        path = TREE_PATHS[name]
        if not os.path.lexists(path):
            self._missing(path, 'tree_was_not_created_or_is_missing')
            return None
        if name == 'media':
            manifest = self.ctx.get('media')
            _need(isinstance(manifest, dict), 'closure_partial_corpus_manifest_missing')
            self.corpus.snapshot_corpus(self.base, manifest)
            expected = manifest['directories']['.']['metadata']
            self._created_root(path, expected)
        else:
            directory = self.ctx.get('appDirectories', {}).get('logs' if name == 'log' else name)
            extra = directory.get('metadata') if isinstance(directory, dict) else None
            if directory is not None:
                _need(directory['path'] == str(path), 'closure_application_directory_path')
            self._created_root(path, extra)
        owners = {(0, 0)} if name in ('config', 'media') else {(995, 986)}
        return self.inventory(path, owners, ROOT_LIMIT, keys=name == 'state', tree_name=name)

    def _capture_exclusions(self):
        observations = []
        for relative in EVIDENCE_EXCLUSIONS:
            path = E / relative
            if not os.path.lexists(path):
                observations.append({'path': str(path), 'absent': True})
                continue
            info = _metadata(path)
            _need(path.resolve() == path and info['uid'] == info['gid'] == 0, 'closure_excluded_path_owner')
            if path.name in ('controller.stdout', 'controller.stderr'):
                _need(info['type'] == stat.S_IFREG and info['mode'] == 0o600 and info['links'] == 1 and
                      info['bytes'] <= STREAM_LIMIT, 'closure_controller_stream_boundary')
            else:
                _need(info['type'] == stat.S_IFDIR and info['mode'] == 0o700 and path != RETAINED,
                      'closure_excluded_directory_boundary')
            observations.append({'path': str(path), 'metadata': info})
        self.record['evidenceCapture'] = {'capturedMonotonicNs': time.monotonic_ns(),
            'boundary': 'before_parent_final_controller_receipt_and_dispatch',
            'exclusions': observations, 'parentFinalReceiptIncluded': False}

    @staticmethod
    def _archive_bound(tree):
        count = len(tree['files']) + len(tree['directories'])
        tar_bytes = sum(math.ceil(row['bytes'] / 512) * 512 for row in tree['files'].values()) + count * 2048 + 10240
        return tar_bytes + math.ceil(tar_bytes / 16383) * 5 + (1 << 20)

    def pack(self, path, tree):
        """Stream an archive and verify every member hash without extraction."""
        path = Path(path)
        _need(path in (RETAINED / 'closed-postgres-private.tar.gz', RETAINED / 'evidence-private.tar.gz') and
              not any(row.get('statOnly') for row in tree['files'].values()), 'closure_archive_scope_or_key')
        bound = self._archive_bound(tree)
        self._reserve(bound)
        owner = self
        class Writer:
            def __init__(self, stream):
                self.stream, self.count = stream, 0
            def write(self, raw):
                owner._remaining(1)
                owner._reserve(len(raw) + 4096)
                _need(self.count + len(raw) <= bound, 'closure_archive_output_bound')
                written = self.stream.write(raw)
                _need(written == len(raw), 'closure_archive_short_write')
                self.count += written
                owner._root_new_bytes += written
                return written
            def flush(self):
                self.stream.flush()
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, 'wb', closefd=False) as stream:
                writer = Writer(stream)
                with tarfile.open(fileobj=writer, mode='w|gz', format=tarfile.PAX_FORMAT) as archive:
                    for name, meta in sorted(tree['directories'].items()):
                        info = tarfile.TarInfo('tree' if name == '.' else 'tree/' + name)
                        info.type, info.uid, info.gid, info.mode, info.mtime = tarfile.DIRTYPE, meta['uid'], meta['gid'], meta['mode'], meta['mtimeNs'] // 1000000000
                        archive.addfile(info)
                    for name, item in sorted(tree['files'].items()):
                        self._remaining(1)
                        source = Path(tree['root']) / name
                        _need(_metadata(source) == item['metadata'], 'closure_archive_source_changed')
                        info = tarfile.TarInfo('tree/' + name)
                        info.size = item['bytes']
                        for field in ('uid', 'gid', 'mode'):
                            setattr(info, field, item['metadata'][field])
                        info.mtime = item['metadata']['mtimeNs'] // 1000000000
                        source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
                        try:
                            opened = os.fstat(source_fd)
                            _need((opened.st_dev, opened.st_ino) == (item['metadata']['device'], item['metadata']['inode']),
                                  'closure_archive_source_open_changed')
                            with os.fdopen(source_fd, 'rb', closefd=False) as body:
                                archive.addfile(info, body)
                            _need(_signature(os.fstat(source_fd)) == _signature(opened), 'closure_archive_source_fd_changed')
                        finally:
                            os.close(source_fd)
                        _need(_metadata(source) == item['metadata'], 'closure_archive_source_changed')
                stream.flush()
                os.fsync(fd)
        finally:
            os.close(fd)
        self._root_new_bytes += 4096
        digest = self._hash_file(path, maximum=ROOT_LIMIT)
        before = _metadata(path)
        seen_files, seen_dirs = set(), set()
        total = 0
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            with os.fdopen(fd, 'rb', closefd=False) as stream:
                with tarfile.open(fileobj=stream, mode='r|gz') as archive:
                    for member in archive:
                        self._remaining(1)
                        parts = member.name.split('/')
                        _need(parts[0] == 'tree' and not any(part in ('', '.', '..') for part in parts) and
                              '\\' not in member.name and not member.linkname and (member.isdir() or member.isfile()) and
                              set(member.pax_headers) <= {'path'} and
                              member.pax_headers.get('path', member.name) == member.name, 'closure_archive_member_boundary')
                        name = '.' if member.name == 'tree' else member.name.removeprefix('tree/')
                        _need(name not in seen_files | seen_dirs, 'closure_archive_duplicate_member')
                        if member.isdir():
                            _need(name in tree['directories'], 'closure_archive_directory_membership')
                            expected = tree['directories'][name]
                            seen_dirs.add(name)
                        else:
                            _need(name in tree['files'] and not tree['files'][name].get('statOnly'), 'closure_archive_file_membership')
                            item = tree['files'][name]
                            expected, checksum, size = item['metadata'], hashlib.sha256(), 0
                            with archive.extractfile(member) as body:
                                while True:
                                    self._remaining(1)
                                    block = body.read(1 << 20)
                                    if not block:
                                        break
                                    size += len(block)
                                    checksum.update(block)
                                    _need(size <= item['bytes'], 'closure_archive_readback_size')
                            _need(size == item['bytes'] == member.size and checksum.hexdigest() == item['sha256'],
                                  'closure_archive_readback_hash')
                            total += size
                            seen_files.add(name)
                        _need(all(getattr(member, field) == expected[field] for field in ('uid', 'gid', 'mode')),
                              'closure_archive_member_metadata')
        finally:
            os.close(fd)
        _need(_metadata(path) == before and seen_files == set(tree['files']) and
              seen_dirs == set(tree['directories']) and total == tree['bytes'], 'closure_archive_complete_readback')
        _need(all(PurePosixPath(name).parent.as_posix() in seen_dirs for name in seen_files | (seen_dirs - {'.'})),
              'closure_archive_parent_membership')
        return {'archive': digest, 'files': len(seen_files), 'directories': len(seen_dirs),
                'uncompressedBytes': total, 'readbackMatched': True, 'safeMembers': True, 'extracted': False}

    def move_tree(self, name, tree):
        source, destination = TREE_PATHS[name], RETAINED / name
        _need(tree['root'] == str(source) and not os.path.lexists(destination) and
              source.lstat().st_dev == RETAINED.stat().st_dev and _metadata(source) == tree['directories']['.'],
              'closure_move_source_identity')
        self._save(RECORDS / ('move-' + name + '-intent.json'), tree)
        self._remaining(1)
        os.rename(source, destination)
        for path in (source.parent, RETAINED):
            _fsync_directory(path)
        owners = {(0, 0)} if name in ('config', 'media') else {(995, 986)}
        moved = self.inventory(destination, owners, ROOT_LIMIT, keys=name == 'state', tree_name=name)
        _need(moved['files'] == tree['files'] and set(moved['directories']) == set(tree['directories']),
              'closure_moved_tree_membership')
        for relative, expected in tree['directories'].items():
            _need(all(moved['directories'][relative][key] == value for key, value in expected.items()
                      if not (relative == '.' and key == 'ctimeNs')), 'closure_moved_directory_metadata')
        receipt = self._save(RECORDS / ('move-' + name + '-after.json'), moved)
        result = {'name': name, 'source': str(source), 'destination': str(destination), 'sameInode': True,
                  'files': len(moved['files']), 'bytes': moved['bytes'], 'privateInventory': receipt, 'masterContentsRead': False}
        self.record['moves'].append(result)
        return result

    def _installed_pins(self):
        allowed = {'binary': BINARY, 'unit': UNIT_ROOT / APP, 'environment': ENVIRONMENT}
        pins = {}
        for name, path in allowed.items():
            candidates = []
            if name in self.ctx.get('installed', {}):
                candidates.append(self.ctx['installed'][name])
            written = self.prepared.get('installationFiles', {}).get(name, {})
            if written.get('written') is True and 'file' in written:
                candidates.append(written['file'])
            if not candidates:
                self._missing(path, 'installed_file_was_not_pinned')
                continue
            _need(all(_same(row, candidates[0]) for row in candidates) and candidates[0]['path'] == str(path),
                  'closure_installed_pin_conflict')
            pins[str(path)] = candidates[0]
        for name in (APP, PGUNIT, ANCHOR):
            if name not in self.ctx.get('unitFiles', {}) and not self.prepared.get('ownedUnits', {}).get(name, {}).get('file'):
                self._missing(UNIT_ROOT / name, 'unit_file_was_not_pinned')
                continue
            pin = self._unit_pin(name)
            _need(pin['path'] not in pins or _same(pins[pin['path']], pin), 'closure_duplicate_app_unit_conflict')
            pins[pin['path']] = pin
        for path, pin in pins.items():
            _need(Path(path) in {BINARY, ENVIRONMENT, *(UNIT_ROOT / name for name in (APP, PGUNIT, ANCHOR))},
                  'closure_installed_file_scope')
            self.support.check_file(self.base, pin)
        return pins

    def _copy_remove(self, pin):
        source = Path(pin['path'])
        _need(source in {BINARY, *(UNIT_ROOT / name for name in (APP, PGUNIT, ANCHOR))}, 'closure_byte_copy_scope')
        destination = RETAINED / 'installation-files' / (('binary' if source == BINARY else source.name) + '.bin')
        self.support.check_file(self.base, pin)
        self._reserve(pin['bytes'] + 4096)
        source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        destination_fd = None
        try:
            opened = os.fstat(source_fd)
            _need((opened.st_dev, opened.st_ino) == (pin['metadata']['device'], pin['metadata']['inode']),
                  'closure_copy_source_open_changed')
            destination_fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            os.fchmod(destination_fd, 0o600)
            while True:
                self._remaining(1)
                raw = os.read(source_fd, 1 << 20)
                if not raw:
                    break
                self._reserve(len(raw) + 4096)
                offset = 0
                while offset < len(raw):
                    count = os.write(destination_fd, raw[offset:])
                    _need(count > 0, 'closure_copy_short_write')
                    offset += count
                    self._root_new_bytes += count
            os.fsync(destination_fd)
            _need(_signature(os.fstat(source_fd)) == _signature(opened), 'closure_copy_source_changed')
        finally:
            if destination_fd is not None:
                os.close(destination_fd)
            os.close(source_fd)
        self._root_new_bytes += 4096
        retained = self._hash_file(destination, {'path': str(destination), 'sha256': pin['sha256'], 'bytes': pin['bytes']})
        self._save(RECORDS / ('remove-' + destination.name + '-intent.json'), {'original': pin, 'retained': retained})
        self.support.check_file(self.base, pin)
        self._remaining(1)
        os.unlink(source)
        _fsync_directory(source.parent)
        result = {'original': pin, 'retained': retained, 'method': 'verified_byte_copy_then_unlink'}
        self.record['removedFiles'].append(result)
        self.record['fileRetentions'].append(result)

    def _retain_environment(self, pin):
        destination = RETAINED / 'config/goby.env'
        _need(not os.path.lexists(ENVIRONMENT) and any(row['name'] == 'config' for row in self.record['moves']) and
              _metadata(destination) == pin['metadata'], 'closure_environment_rename_identity')
        retained = self._hash_file(destination, {'path': str(destination), 'sha256': pin['sha256'], 'bytes': pin['bytes']})
        self.record['fileRetentions'].append({'original': pin, 'retained': retained,
            'method': 'config_directory_rename_same_inode_hash_only', 'environmentDecoded': False, 'extraByteCopy': False})

    def _evidence_postflight(self, before):
        current = self.inventory(E, {(0, 0)}, RAW_LIMIT, exclusions=EVIDENCE_EXCLUSIONS)
        _need(current['files'] == before['files'] and set(current['directories']) == set(before['directories']),
              'closure_evidence_source_membership_changed')
        for name, expected in before['directories'].items():
            ignored = ('ctimeNs', 'mtimeNs', 'bytes', 'links') if name in ('.', 'private') else ()
            _need(all(current['directories'][name][key] == value for key, value in expected.items() if key not in ignored),
                  'closure_evidence_source_directory_changed')
        growth = 0
        for row in self.record['evidenceCapture']['exclusions']:
            path = Path(row['path'])
            if path.name not in ('controller.stdout', 'controller.stderr'):
                continue
            if row.get('absent'):
                _need(not os.path.lexists(path), 'closure_excluded_stream_appeared')
                continue
            info = _metadata(path)
            _need(all(info[key] == row['metadata'][key] for key in STABLE) and info['links'] == 1 and
                  info['bytes'] <= STREAM_LIMIT and info['bytes'] >= row['metadata']['bytes'], 'closure_controller_stream_changed')
            growth += info['bytes'] - row['metadata']['bytes']
        self._new_raw_bytes += growth
        self._raw_check()
        self.record['evidenceSourcePostflightMatched'] = True

    def _postflight_units(self):
        removed_units = [row for row in self.record['removedFiles'] if Path(row['original']['path']).parent == UNIT_ROOT]
        if removed_units:
            tool = self._check_tool('systemctl')
            self._command('capacity-closure-daemon-reload', [tool, 'daemon-reload'], 15)
            self.record['daemonReloadSubmitted'] = True
        for name in (APP, PGUNIT, ANCHOR):
            row = self._show(name)
            _need(row['Id'] == name and row['LoadState'] == 'not-found' and row['MainPID'] == '0' and
                  row['FragmentPath'] == row['DropInPaths'] == '' and not self._members(name), 'closure_unit_still_registered')
        registry = {}
        names = self.units._systemd_inventory(self.base, registry)
        _need(not registry['matchingAliases'] and not registry['matchingScopeEntries'] and
              not registry['relevantDropins'] and not registry['uninspectedDirectoryLinks'],
              'closure_unit_alias_or_dropin_retained')
        for operation in ('list-units', 'list-unit-files'):
            tool = self._check_tool('systemctl')
            raw = self._command('capacity-closure-' + operation, [tool, operation, '--all', '--no-legend', '--no-pager', '--plain'], 10)
            rows = [line.split() for line in raw.decode('utf-8', 'strict').splitlines() if line.strip()]
            _need(len(rows) <= 20000 and not any(row[0] in names or row[0].startswith(PREFIX) for row in rows),
                  'closure_owned_unit_in_registry')
        self.record['unitRegistrationsAbsent'] = True

    def _unmount(self, mount):
        _need(self.record.get('unitRegistrationsAbsent') is True and self._namespace_absent() and
              self.support.pg_mount(self.base) == mount, 'closure_unmount_authority')
        self._save(RECORDS / 'pg-unmount-intent.json', mount)
        tool = self._check_tool('umount')
        self._command('capacity-closure-ordinary-unmount', [tool, '--', str(PG)], 20)
        _need(not any(row.split()[4] == str(PG) or row.split()[4].startswith(str(PG) + '/')
                      for row in Path('/proc/self/mountinfo').read_text().splitlines()), 'closure_pg_mount_retained')
        expected = self.prepared.get('pgUnderlying')
        _need(isinstance(expected, dict) and _metadata(PG) == expected and not list(PG.iterdir()), 'closure_pg_underlying_changed')
        self.record['postgresUnmounted'] = True

    def _preserve(self):
        self._budget_contract()
        _need(all(row.get('physicalClosed') is True for row in self.record['closedUnits'].values()) and
              len(self.record['closedUnits']) == 2 and self.record.get('application', {}).get('physicalClosed') is True,
              'closure_resources_not_safe_for_preservation')
        self._created_root(F)
        trees = {}
        for name in TREE_PATHS:
            try:
                tree = self._source_tree(name)
                if tree is not None:
                    trees[name] = tree
            except BaseException as error:
                self._remember('source_tree_' + name, error)
                self._missing(TREE_PATHS[name], 'source_authority_unavailable_retained_in_place')
        mount = self.ctx.get('pgMount', self.prepared.get('pgMount'))
        pg_tree = None
        if mount is not None:
            _need(self.support.pg_mount(self.base) == mount, 'closure_pg_mount_changed')
            _need(not os.path.lexists(PG / 'data/postmaster.pid') and
                  (not (PG / 'socket').exists() or not list((PG / 'socket').iterdir())), 'closure_pg_process_artifact_retained')
            pg_tree = self.inventory(PG, {(103, 106)}, PG_LIMIT)
            _need({name.split('/')[0] for name in pg_tree['files']} <= {'data', 'socket', 'initdb-password'},
                  'closure_pg_source_membership')
        else:
            self._missing(PG, 'mount_identity_was_not_recorded')
        pins = self._installed_pins()
        self._capture_exclusions()
        evidence = self.inventory(E, {(0, 0)}, RAW_LIMIT, exclusions=EVIDENCE_EXCLUSIONS)
        self._root_baseline = self._allocated_tree(E) + self._allocated_tree(F, skip_pg=True)
        self._root_new_bytes = 0
        _need(self._root_baseline <= ROOT_LIMIT, 'closure_root_source_allocation_limit')
        copy_bound = sum(pin['bytes'] + 4096 for path, pin in pins.items() if Path(path) != ENVIRONMENT)
        archive_bound = self._archive_bound(evidence) + (self._archive_bound(pg_tree) if pg_tree else 0)
        future_raw = RAW_LIMIT - self.budget['rawEvidenceBytes'] - self._new_raw_bytes
        estimate = archive_bound + copy_bound + max(0, future_raw) + (2 * STREAM_LIMIT) + (8 << 20)
        self._reserve(estimate)
        self.record['storage'] = {'alreadyWrittenRootAllocatedBytes': self._root_baseline,
            'rawEvidenceBytesAtEntry': self.budget['rawEvidenceBytes'], 'archiveMaximumBytes': archive_bound,
            'byteCopyMaximumBytes': copy_bound, 'plannedAdditionalBytesUpperBound': estimate,
            'rootAllocationLimitBytes': ROOT_LIMIT, 'rawLimitBytes': RAW_LIMIT, 'rootReserveBytes': ROOT_RESERVE,
            'renameCountsAsNewCopy': False}
        manifest = {'trees': trees, 'postgres': pg_tree, 'evidence': evidence, 'installedPins': pins,
                    'capture': self.record['evidenceCapture'], 'storage': self.record['storage']}
        self.record['sourceManifest'] = self._save(RECORDS / 'source-manifest.json', manifest)
        RETAINED.mkdir(mode=0o700)
        self._root_new_bytes += 4096
        if pg_tree is not None:
            self.record['archives']['postgres'] = self.pack(RETAINED / 'closed-postgres-private.tar.gz', pg_tree)
        self.record['archives']['evidence'] = self.pack(RETAINED / 'evidence-private.tar.gz', evidence)
        _need(all(row['readbackMatched'] is True for row in self.record['archives'].values()), 'closure_archives_not_read_back')
        for name, tree in trees.items():
            self.move_tree(name, tree)
        copies = RETAINED / 'installation-files'
        copies.mkdir(mode=0o700)
        self._root_new_bytes += 4096
        for path, pin in pins.items():
            if Path(path) == ENVIRONMENT:
                if 'config' in trees:
                    self._retain_environment(pin)
                else:
                    self._missing(ENVIRONMENT, 'config_not_moved_environment_retained_in_place')
            else:
                self._copy_remove(pin)
        binary_directory = F / 'bin'
        if os.path.lexists(binary_directory):
            self._created_root(binary_directory)
            if not list(binary_directory.iterdir()):
                self._remaining(1)
                os.rmdir(binary_directory)
                self.record['removedEmptyDirectories'].append(str(binary_directory))
            else:
                self._missing(binary_directory, 'unretained_members_remain')
        self._postflight_units()
        if mount is not None:
            _need(pg_tree is not None and self.record['archives']['postgres']['readbackMatched'] is True,
                  'closure_pg_unmount_before_archive')
            self._unmount(mount)
        else:
            mounts = Path('/proc/self/mountinfo').read_text().splitlines()
            self.record['postgresUnmounted'] = not any(row.split()[4] == str(PG) or row.split()[4].startswith(str(PG) + '/') for row in mounts)
        self._evidence_postflight(evidence)
        remaining = sorted(path.name for path in F.iterdir())
        self.record['remainingFixtureEntries'] = remaining
        if remaining != ['pg'] or not self.record.get('postgresUnmounted') or list(PG.iterdir()):
            self._missing(F, 'fixture_not_reduced_to_empty_underlying_pg')
        self.record['fixtureApplicationPathsAbsent'] = all(not os.path.lexists(path) for path in
            (*TREE_PATHS.values(), BINARY, ENVIRONMENT, RUNTIME, *(UNIT_ROOT / name for name in (APP, PGUNIT, ANCHOR))))
        _need(self.record['fixtureApplicationPathsAbsent'], 'closure_fixture_application_paths_retained')
        accounts = self.units._accounts(self.base)
        expected_accounts = self.prepared.get('capacityAbsence', self.base.report.get('capacityAbsence', {})).get('accounts')
        _need(isinstance(expected_accounts, dict) and _same(accounts, expected_accounts), 'closure_shared_accounts_changed')
        self.record['sharedAccountsUnchanged'] = True
        self.record['actualRootAllocatedBytesAfterPreservation'] = self._allocated_tree(E) + self._allocated_tree(F, skip_pg=True)
        _need(self.record['actualRootAllocatedBytesAfterPreservation'] <= ROOT_LIMIT, 'closure_postflight_root_allocation_limit')
        self.record['preservationComplete'] = len(trees) == 5 and len(self.record['fileRetentions']) == 5 and pg_tree is not None

    def close(self):
        """Return a closure record; never convert upstream failures to success."""
        if self._started:
            return self.record
        self._started = True
        self.record['status'] = 'closing_owned_resources'
        self.record['enteredMonotonicNs'] = time.monotonic_ns()
        try:
            self._scope()
        except BaseException as error:
            self._remember('scope', error)
            self.record['status'] = 'closure_authority_unavailable_resources_retained'
            return self.record
        self.base.run = self._transport
        try:
            try:
                self._budget_contract()
            except BaseException as error:
                self._remember('budget', error)
            for name in (PGUNIT, ANCHOR):
                try:
                    self.record['infrastructureStopMethodsEntered'].append(name)
                    self._stop_infrastructure(name)
                except BaseException as error:
                    if (not self.record['closedUnits'].get(name, {}).get('ownershipEstablished') or _error(error) in
                            ('closure_owned_unit_origin', 'closure_invocation_replaced', 'closure_owned_lifetime_replaced', 'owned_file_changed')):
                        self._foreign.add(name)
                    self._remember('close_' + name, error)
            self._mandatory_controls = False
            try:
                self._settle_pg_logs()
            except BaseException as error:
                self._remember('postgres_log_growth', error)
            try:
                # The parent already owns a protected-state baseline. Avoid
                # spending the mandatory stop allowance on this later read.
                self.record['protectedBefore'] = self.support.protected(self.base, self.ctx or None)
                self.record['protectedBeforeObservedAfterInfrastructureStops'] = True
            except BaseException as error:
                self._remember('protected_before', error)
            safe = True
            try:
                self._application_closed()
                _need(all(self.record['closedUnits'].get(name, {}).get('physicalClosed') is True for name in (PGUNIT, ANCHOR)),
                      'closure_infrastructure_not_physically_closed')
                _need(self._namespace_absent(), 'closure_namespace_closure_not_proved')
            except BaseException as error:
                safe = False
                self._remember('physical_closure', error)
            if safe:
                try:
                    self._preserve()
                except BaseException as error:
                    self._remember('preservation', error)
            try:
                self.record['protectedAfter'] = self.support.protected(self.base, self.ctx or None)
                _need(self.record.get('protectedBefore') == self.record['protectedAfter'], 'closure_protected_state_changed')
                expected = self.prepared.get('protectedAfter', self.prepared.get('protectedBefore'))
                if expected is not None:
                    _need(_same(expected, self.record['protectedAfter']), 'closure_preparation_protected_state_changed')
            except BaseException as error:
                self._remember('protected_after', error)
        finally:
            self.base.run = self._original_run
        self.record['normalInfrastructureStops'] = all(self.record['closedUnits'].get(name, {}).get('normalExit') is True
                                                     for name in (PGUNIT, ANCHOR))
        self.record['newRawEvidenceBytesBeforeResultReceipt'] = self._new_raw_bytes
        self.record['newRootOutputBytesBeforeResultReceipt'] = self._root_new_bytes
        self.record['completedMonotonicNs'] = time.monotonic_ns()
        complete = (self.record.get('preservationComplete') is True and self.record.get('postgresUnmounted') is True and
                    self.record.get('unitRegistrationsAbsent') is True and self.record.get('privateNamespaceGone') is True and
                    self.record['normalInfrastructureStops'] and not self.record['errors'] and not self.record['missing'])
        self.record['status'] = 'owned_resources_preserved_and_closed' if complete else 'closure_failed_evidence_and_remaining_resources_retained'
        self.record['receipt'] = self._save_optional('closure-result.json', self.record)
        self.record['newRawEvidenceBytes'] = self._new_raw_bytes
        self.record['newRootOutputBytesConservativelyCharged'] = self._root_new_bytes
        self.record['finalReceiptPersisted'] = self.record['receipt'].get('persisted') is not False
        if not self.record['finalReceiptPersisted'] or self.record['errors']:
            self.record['status'] = 'closure_failed_evidence_and_remaining_resources_retained'
        return self.record
