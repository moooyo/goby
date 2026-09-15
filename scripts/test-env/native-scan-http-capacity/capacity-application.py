"""Own one capacity APP lifetime; importing this module performs no work.

API: Application(base, support, units, context, *, deadline, record=None).
deadline is the controller's continuous absolute monotonic deadline. record is
an optional new empty dict, retained by reference for the controller's report.
start() returns that record after final nonroot configuration acceptance, not
HTTP readiness. check_owned() returns a bounded current unit/process/descendant
observation. stop(strict, deadline) uses a narrower absolute deadline, records
physical closure and normal-exit evidence, and never repeats a dispatched stop.
After a command failed before creating any recorded PID, a later cleanup call
may try the fixed stop again within the same controller deadline.
The controller calls stop(False, cleanup_deadline) after any failed phase.

base supplies need/Rejected, run, report['commands'], metadata, write_new,
digest and process_identity. run is the controller's sole bounded raw command
recorder. Actual command creation is recognized only from its matching recorded
PID; errors and original command indexes are retained before propagating.
No process signals, subprocess runner, HTTP, SQL, credentials or archive logic
exist here. Evidence files use exclusive base.write_new beneath E/private.

context has the capacity preparation shape: scope, fixtureRoot, bootId, anchorPid, anchor/postgres,
networkNamespace, installed={binary,unit,environment}, unitFiles, full tools
pins for ffmpeg/ffprobe, preparedServiceProperties, and appDirectories mapping
state/cache/logs to {path,metadata}. appLog is {path,metadata,sha256,bytes} for
the new root:root 0600 empty units.APP_LOG. Persistent directories are prepared
goby:goby 0700 objects; only RUNTIME is newly created by systemd. Diagnostics
uses the initially absent LOGS/diagnostics child, not the console's directory.

The native binary source uses JSON slog console output and a separate registered
diagnostics JSONL store (cmd/goby/main.go and internal/diagnostics). Normal stop
requires complete console EOF, closed registered diagnostic files, matching
shutdown-completed objects, no ERROR records, and the original typed referenced
code=1/status=0 exit proof. A completed message alone never accepts shutdown.
All paths and unit properties come from the pinned capacity declarations.
"""

from decimal import Decimal
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import time


STABLE = ('device', 'inode', 'uid', 'gid', 'mode', 'type')
CAPABILITIES = ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb')
EMPTY_SHA256 = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
MAX_PROCESSES, MAX_KNOWN, MAX_LOG_BYTES = 256, 4096, 64 << 20
STARTUP_SECONDS = 10


class ProcessUnavailable(Exception):
    """A sampled process exited or changed during a bounded observation."""


def _error_code(error):
    value = str(error)
    return value if re.fullmatch('[A-Za-z0-9_.-]{1,160}', value) else type(error).__name__


def _encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def _stat(pid):
    try:
        fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(') ', 1)[1].split()
        return {'pid': pid, 'state': fields[0], 'parentPid': int(fields[1]), 'startTicks': fields[19]}
    except (FileNotFoundError, ProcessLookupError):
        raise ProcessUnavailable('process_disappeared') from None


def _process(pid, *, command_line=False):
    root = Path('/proc') / str(pid)
    first = _stat(pid)
    if first['state'] in ('Z', 'X'):
        raise ProcessUnavailable('process_terminal_state')
    try:
        status = dict(line.split(':', 1) for line in (root / 'status').read_text().splitlines() if ':' in line)
        link_before = os.readlink(root / 'exe')
        executable = (root / 'exe').stat()
        result = {key: first[key] for key in ('pid', 'parentPid', 'startTicks')}
        result.update(uids=[int(value) for value in status['Uid'].split()],
            gids=[int(value) for value in status['Gid'].split()],
            noNewPrivileges=int(status['NoNewPrivs']),
            capabilities={key: status[key].strip() for key in CAPABILITIES},
            exe=link_before, executableDevice=executable.st_dev, executableInode=executable.st_ino,
            networkNamespace=os.readlink(root / 'ns/net'), cgroup=(root / 'cgroup').read_text().strip())
        if command_line:
            result['cmdline'] = [part.decode('utf-8', 'strict') for part in (root / 'cmdline').read_bytes().split(b'\0')[:-1]]
        last = _stat(pid)
        final_executable = (root / 'exe').stat()
        if last['state'] in ('Z', 'X') or any(first[key] != last[key] for key in ('pid', 'parentPid', 'startTicks')) or \
                os.readlink(root / 'exe') != link_before or \
                (final_executable.st_dev, final_executable.st_ino) != (executable.st_dev, executable.st_ino):
            raise ProcessUnavailable('process_changed_during_observation')
        return result
    except (FileNotFoundError, ProcessLookupError):
        raise ProcessUnavailable('process_disappeared') from None


def _microseconds(value):
    factors = {'h': 3600000000, 'min': 60000000, 's': 1000000, 'ms': 1000, 'us': 1}
    tokens = value.split() if isinstance(value, str) else []
    if not tokens or len(tokens) > 5:
        raise ValueError('capacity_property_timespan')
    total = Decimal(0)
    for token in tokens:
        match = re.fullmatch(r'([0-9]+(?:\.[0-9]{1,6})?)(h|min|s|ms|us)', token)
        if match is None:
            raise ValueError('capacity_property_timespan')
        total += Decimal(match.group(1)) * factors[match.group(2)]
    if total != total.to_integral_value():
        raise ValueError('capacity_property_timespan_precision')
    return int(total)


class Application:
    def __init__(self, base, support, units, context, *, deadline, record=None):
        self.base, self.support, self.units, self.ctx = base, support, units, context
        base.need(type(deadline) in (int, float) and math.isfinite(deadline) and deadline > time.monotonic(),
                  'capacity_application_deadline')
        base.need(record is None or (type(record) is dict and not record), 'capacity_application_record_not_new')
        base.need(all(getattr(support, key) == getattr(units, key) for key in
            ('E', 'F', 'APP', 'ANCHOR', 'PGUNIT', 'PREFIX', 'BINARY', 'ENVIRONMENT', 'STATE', 'CACHE', 'LOGS', 'RUNTIME')),
            'capacity_application_scope')
        base.need(context.get('scope') == str(support.E) and context.get('fixtureRoot') == str(support.F) and
            all(context[key]['unit']['Id'] == name and context[key]['process']['cgroup'] == '0::/system.slice/' + name and
                context['unitFiles'][name]['path'] == str(support.UNIT_ROOT / name)
                for name, key in ((support.ANCHOR, 'anchor'), (support.PGUNIT, 'postgres'))),
            'capacity_application_infrastructure_scope')
        self.deadline = float(deadline)
        self._operation_deadline = self.deadline
        self.record = {} if record is None else record
        self.record.update(kind='native-capacity-application', version=1, unit=support.APP,
            startAttempted=False, startDispatched=False, stopAttempted=False, stopDispatched=False,
            configurationAccepted=False, physicalClosed=False, normalExit=False, closed=False,
            commandReferences=[], observations=[], errors=[], processDisappearanceObservations=[],
            knownProcesses={}, automaticRestartObserved=False, oomObserved=False,
            httpRequests=0, sqlConnections=0, processSignals=0)
        self._first_error = None
        self._show_serial = 0
        self._receipts = {}
        self._declared = units.expected_properties(context['anchorPid'])
        self._properties = tuple(dict.fromkeys((*support.SERVICE_PROPERTIES, 'TimeoutStartUSec')))
        self._group = Path('/sys/fs/cgroup/system.slice') / support.APP
        self._cgroup = '0::/system.slice/' + support.APP
        self._diagnostics = support.LOGS / 'diagnostics'
        self._known = self.record['knownProcesses']
        self._native_pins = {}

    def _remaining(self, maximum):
        remaining = min(self.deadline, self._operation_deadline) - time.monotonic()
        self.base.need(remaining > 0, 'capacity_application_deadline')
        return min(maximum, remaining)

    def _remember_error(self, phase, error):
        row = {'phase': phase, 'code': _error_code(error), 'type': type(error).__name__}
        if self._first_error is None:
            self._first_error = error
            self.record['firstError'] = row
        if len(self.record['errors']) < 256:
            self.record['errors'].append(row)
        self.record['normalExit'] = False

    def _save(self, name, value):
        self.base.need(re.fullmatch(r'application-[a-z0-9-]+\.json', name), 'capacity_application_receipt_name')
        self.base.need(name not in self._receipts, 'capacity_application_receipt_reused')
        path = self.support.E / 'private' / name
        raw = _encoded(value)
        self.base.need(len(raw) <= 4 << 20, 'capacity_application_receipt_budget')
        self.base.write_new(path, raw)
        receipt = {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}
        self._receipts[name] = receipt
        return receipt

    def _reference_writer(self, path, value):
        self.base.need(Path(path).parent == self.support.E / 'private', 'capacity_reference_receipt_scope')
        return self._save(Path(path).name, value)

    def _command(self, label, argv, maximum):
        first = len(self.base.report['commands'])
        try:
            return self.base.run(label, argv, self._remaining(maximum))
        finally:
            indexes = [index for index in range(first, len(self.base.report['commands']))
                       if self.base.report['commands'][index].get('label') == label]
            self.record['commandReferences'].append({'label': label, 'commandIndexes': indexes})
            dispatched = any(type(self.base.report['commands'][index].get('pid')) is int and
                self.base.report['commands'][index]['pid'] > 1 for index in indexes)
            if label == 'capacity-app-start':
                self.record['startDispatched'] = self.record['startDispatched'] or dispatched
            elif label == 'capacity-app-stop':
                self.record['stopDispatched'] = self.record['stopDispatched'] or dispatched

    def _show(self):
        self._show_serial += 1
        raw = self._command('capacity-app-show-%05d' % self._show_serial,
            ['/usr/bin/systemctl', 'show', self.support.APP, '--no-pager', '--property=' + ','.join(self._properties)], 10)
        pairs = [line.split('=', 1) for line in raw.decode('utf-8', 'strict').splitlines()]
        self.base.need(all(len(pair) == 2 for pair in pairs), 'capacity_application_property_shape')
        row = dict(pairs)
        self.base.need(len(row) == len(pairs) and set(row) == set(self._properties), 'capacity_application_property_inventory')
        if row['NRestarts'] != '0':
            self.record['automaticRestartObserved'] = True
        if row['Result'] == 'oom-kill':
            self.record['oomObserved'] = True
        if len(self.record['observations']) < 4096:
            self.record['observations'].append({key: row[key] for key in
                ('Id', 'ActiveState', 'SubState', 'MainPID', 'ExecMainPID', 'InvocationID', 'NRestarts', 'Result',
                 'ExecMainStartTimestampMonotonic')})
        return row

    def _unit_contract(self, row):
        expected = self._declared
        self.base.need(all(row.get(key) == value for key, value in expected['exact'].items()), 'capacity_application_exact_properties')
        for key, values in expected['tokenSets'].items():
            observed = row.get(key, '').split()
            self.base.need(len(observed) == len(set(observed)) and sorted(observed) == sorted(values),
                           'capacity_application_token_properties')
        for key, value in expected['integers'].items():
            self.base.need(re.fullmatch('[0-9]+', row.get(key, '')) and int(row[key]) == value,
                           'capacity_application_integer_properties')
        for key, value in expected['microseconds'].items():
            self.base.need(_microseconds(row.get(key)) == value, 'capacity_application_time_properties')
        command = expected['execStart']
        text = row.get(command['property'], '')
        fields = text.split(' ; ', 3)
        self.base.need(command['commands'] == 1 and command['ignoreErrors'] is False and
            text.count('{') == text.count('}') == 1 and text.endswith(' }') and len(fields) == 4 and
            fields[:3] == ['{ path=' + command['path'], 'argv[]=' + ' '.join(command['argv']), 'ignore_errors=no'],
            'capacity_application_execstart')
        files = expected['environmentFiles']['files']
        self.base.need(len(files) == 1 and files[0]['ignoreErrors'] is False and
            row.get(expected['environmentFiles']['property']) == files[0]['path'] + ' (ignore_errors=no)',
            'capacity_application_environment_file')

    def _members(self):
        if not self._group.exists():
            return []
        self.base.need(self._group.is_dir() and not self._group.is_symlink(), 'capacity_application_cgroup_path')
        paths = list(self._group.rglob('cgroup.procs'))
        self.base.need(len(paths) <= 64, 'capacity_application_cgroup_inventory')
        try:
            pids = {int(value) for path in paths for value in path.read_text().split()}
        except FileNotFoundError:
            raise ProcessUnavailable('cgroup_membership_changed') from None
        self.base.need(len(pids) <= MAX_PROCESSES and all(pid > 1 for pid in pids), 'capacity_application_cgroup_bound')
        return sorted(pids)

    def _security(self, process):
        return (process['uids'] == [995] * 4 and process['gids'] == [986] * 4 and
            process['noNewPrivileges'] == 1 and all(int(value, 16) == 0 for value in process['capabilities'].values()) and
            process['networkNamespace'] == self.ctx['networkNamespace'] and process['cgroup'] == self._cgroup)

    def _matches_pin(self, process, pin):
        return process['exe'] == pin['path'] and (process['executableDevice'], process['executableInode']) == \
            (pin['metadata']['device'], pin['metadata']['inode'])

    def _directory_profile(self, *, before=False):
        result = {}
        self.base.need(set(self.ctx['appDirectories']) == {'state', 'cache', 'logs'}, 'capacity_prepared_directory_inventory')
        for key, path in (('state', self.support.STATE), ('cache', self.support.CACHE), ('logs', self.support.LOGS)):
            saved = self.ctx['appDirectories'][key]
            current = self.base.metadata(path)
            self.base.need(saved['path'] == str(path) and path.resolve() == path and current['type'] == stat.S_IFDIR and
                current['uid'] == 995 and current['gid'] == 986 and current['mode'] == 0o700 and
                all(current[name] == saved['metadata'][name] for name in STABLE), 'capacity_prepared_directory_changed')
            if before:
                expected_names = {self.units.APP_LOG.name} if key == 'logs' else set()
                self.base.need({item.name for item in path.iterdir()} == expected_names, 'capacity_prepared_directory_members')
            result[key] = current
        if before:
            self.base.need(not os.path.lexists(self.support.RUNTIME) and not os.path.lexists(self._diagnostics),
                           'capacity_created_directory_already_exists')
            self.record['runtimeDirectoryAbsentBeforeStart'] = True
            self.record['diagnosticsDirectoryAbsentBeforeStart'] = True
        else:
            path = self.support.RUNTIME
            current = self.base.metadata(path)
            self.base.need(path.resolve() == path and current['type'] == stat.S_IFDIR and current['uid'] == 995 and
                current['gid'] == 986 and current['mode'] == 0o700, 'capacity_runtime_directory_profile')
            if 'runtimeDirectory' in self.record:
                self.base.need(all(current[key] == self.record['runtimeDirectory'][key] for key in STABLE),
                               'capacity_runtime_directory_replaced')
            else:
                self.record['runtimeDirectory'] = current
            if self._diagnostics.exists():
                diagnostic = self.base.metadata(self._diagnostics)
                self.base.need(self._diagnostics.resolve() == self._diagnostics and diagnostic['type'] == stat.S_IFDIR and
                    diagnostic['uid'] == 995 and diagnostic['gid'] == 986 and diagnostic['mode'] == 0o700,
                    'capacity_diagnostics_directory_profile')
                if 'diagnosticsDirectory' in self.record:
                    self.base.need(all(diagnostic[key] == self.record['diagnosticsDirectory'][key] for key in STABLE),
                                   'capacity_diagnostics_directory_replaced')
                else:
                    self.record['diagnosticsDirectory'] = diagnostic
        return result

    def _console_identity(self, pid=None, *, initial=False):
        pin = self.ctx['appLog']
        self.base.need(set(pin) == {'path', 'metadata', 'sha256', 'bytes'} and pin['path'] == str(self.units.APP_LOG) and
            pin['bytes'] == pin['metadata']['bytes'] == 0 and pin['sha256'] == EMPTY_SHA256, 'capacity_console_origin')
        path = Path(pin['path'])
        current = self.base.metadata(path)
        self.base.need(path.resolve() == path and current['type'] == stat.S_IFREG and current['uid'] == current['gid'] == 0 and
            current['mode'] == 0o600 and current['links'] == 1 and
            all(current[key] == pin['metadata'][key] for key in STABLE), 'capacity_console_identity')
        if initial:
            self.base.need(current == pin['metadata'], 'capacity_console_not_initially_empty')
        if pid is not None:
            try:
                for number in (1, 2):
                    descriptor = Path('/proc') / str(pid) / 'fd' / str(number)
                    info = descriptor.stat()
                    self.base.need(os.readlink(descriptor) == str(path) and
                        (info.st_dev, info.st_ino) == (current['device'], current['inode']), 'capacity_console_descriptor_changed')
            except (FileNotFoundError, ProcessLookupError):
                raise ProcessUnavailable('process_disappeared') from None
        return current

    def _remember_process(self, process, kind):
        key = str(process['pid']) + ':' + process['startTicks']
        self.base.need(key in self._known or len(self._known) < MAX_KNOWN, 'capacity_known_process_bound')
        value = self._known.setdefault(key, {'pid': process['pid'], 'startTicks': process['startTicks'],
            'firstObservedMonotonicNs': time.monotonic_ns()})
        value.update(parentPid=process['parentPid'], exe=process['exe'], kind=kind,
            executableDevice=process['executableDevice'], executableInode=process['executableInode'])
        return value

    def _disappearance(self, pid, error):
        rows = self.record['processDisappearanceObservations']
        if len(rows) < 4096:
            rows.append({'pid': pid, 'code': _error_code(error), 'observedMonotonicNs': time.monotonic_ns()})

    def _oom(self):
        path = self._group / 'memory.events'
        if not path.exists():
            return
        try:
            pairs = [line.split() for line in path.read_text().splitlines()]
        except FileNotFoundError:
            return
        self.base.need(all(len(pair) == 2 and re.fullmatch('[0-9]+', pair[1]) for pair in pairs), 'capacity_memory_events_shape')
        values = {key: int(value) for key, value in pairs}
        self.record['memoryEvents'] = values
        if any(values.get(key, 0) != 0 for key in ('oom', 'oom_kill', 'oom_group_kill')):
            self.record['oomObserved'] = True
            self.base.need(False, 'capacity_application_oom')

    def _ownership(self, row, *, cleanup=False):
        self.support.check_file(self.base, self.ctx['installed']['unit'])
        self.base.need(self.record['startDispatched'] is True and row['DropInPaths'] == '' and
            row['Id'] == self.support.APP and row['LoadState'] == 'loaded' and
            row['FragmentPath'] == self.ctx['installed']['unit']['path'], 'capacity_cleanup_unit_origin')
        if row['NRestarts'] != '0':
            self.record['automaticRestartObserved'] = True
        if row['MainPID'] == '0':
            self.base.need(not self._members(), 'capacity_cleanup_main_absent_with_members')
            proof = self.support.application_start_cancellation(self.base, row, self.record['dispatchMonotonicUsec'],
                self.ctx['bootId'], self.record['beforeStart'])
            self.record['unitStartCancellationAuthority'] = {'unit': row, 'identity': proof}
            return proof
        owner = self.support.application_ownership(self.base, row, self.record['dispatchMonotonicUsec'],
            self.ctx['bootId'], expected=self.record.get('startOwnership'), allow_restart=cleanup)
        self.base.need(owner['invocationId'] != self.record['beforeStart']['InvocationID'], 'capacity_application_invocation_not_fresh')
        if 'startOwnership' not in self.record:
            self.record['startOwnership'] = owner
        if cleanup:
            self.record['cleanupOwnership'] = owner
            if self.record.pop('unitStartCancellationAuthority', None) is not None:
                self.record['unitOnlyAuthoritySupersededByLiveOwner'] = True
        return owner

    def start(self):
        self.base.need(self.record['startAttempted'] is False, 'capacity_application_start_repeated')
        self.record['startAttempted'] = True
        try:
            self.support.check_infrastructure(self.base, self.ctx)
            self.support.check_installation(self.base, self.ctx)
            for name in ('ffmpeg', 'ffprobe'):
                tool = self.ctx['tools'][name]
                self.base.need(Path(tool['path']).resolve() == Path(tool['resolvedPath']), 'capacity_native_tool_path')
                pin = {key: tool[key] for key in ('sha256', 'bytes', 'metadata')}
                pin['path'] = tool['resolvedPath']
                self.support.check_file(self.base, pin)
                self._native_pins[name] = pin
            self.record['preparedDirectories'] = self._directory_profile(before=True)
            self.record['consoleOrigin'] = self.ctx['appLog']
            self._console_identity(initial=True)
            before = self._show()
            self._unit_contract(before)
            self._unit_contract(self.ctx['preparedServiceProperties'])
            self.base.need(before['ActiveState'] == 'inactive' and before['MainPID'] == '0' and
                before['NRestarts'] == '0' and not self._members(), 'capacity_application_not_initially_inactive')
            self.record.update(beforeStart=before, dispatchMonotonicUsec=time.monotonic_ns() // 1000)
            self.record['startIntent'] = self._save('application-start-intent.json',
                {'before': before, 'dispatchMonotonicUsec': self.record['dispatchMonotonicUsec'],
                 'binary': self.ctx['installed']['binary'], 'preparedDirectories': self.record['preparedDirectories']})
            self._command('capacity-app-start', ['/usr/bin/systemctl', 'start', self.support.APP], 30)
            until = min(self.deadline, time.monotonic() + STARTUP_SECONDS)
            self.record['startupConfigurationDeadlineMonotonic'] = until
            while True:
                self.base.need(time.monotonic() < until, 'capacity_application_configuration_timeout')
                original = self._operation_deadline
                self._operation_deadline = min(original, until)
                try:
                    row = self._show()
                    self._unit_contract(row)
                    self.base.need(row['ActiveState'] in ('active', 'activating') and row['NRestarts'] == '0' and
                                   row['Result'] == 'success', 'capacity_application_start_identity')
                    if row['MainPID'] != '0':
                        try:
                            owner = self._ownership(row)
                            process = _process(owner['pid'], command_line=True)
                            self.base.need(process['startTicks'] == owner['startTicks'], 'capacity_application_start_changed')
                            accepted = (row['ActiveState'] == 'active' and row['SubState'] == 'running' and
                                process['parentPid'] == 1 and self._security(process) and
                                process['cmdline'] == [str(self.support.BINARY)] and
                                self._matches_pin(process, self.ctx['installed']['binary']) and process['pid'] in self._members())
                            if accepted:
                                self._console_identity(process['pid'])
                                self.base.need(_process(process['pid'], command_line=True) == process,
                                               'capacity_application_profile_changed')
                                self._directory_profile()
                                self._oom()
                                self.base.need(time.monotonic() < until, 'capacity_application_configuration_timeout')
                                self.record.update(configurationAccepted=True, process=process, observedStart=row,
                                    pid=owner['pid'], invocationId=owner['invocationId'])
                                self._remember_process(process, 'application')
                                self.record['startReceipt'] = self._save('application-start-owned.json',
                                    {'ownership': owner, 'process': process, 'unit': row, 'runtimeDirectory': self.record['runtimeDirectory']})
                                return self.record
                        except ProcessUnavailable as error:
                            self._disappearance(int(row['MainPID']), error)
                        except self.base.Rejected as error:
                            if str(error) != 'application_ownership_process_unavailable':
                                raise
                            self._disappearance(int(row['MainPID']), error)
                    self.record['startupTransitionObservations'] = self.record.get('startupTransitionObservations', 0) + 1
                finally:
                    self._operation_deadline = original
                time.sleep(min(0.05, max(0, until - time.monotonic())))
        except BaseException as error:
            self._remember_error('start', error)
            raise

    def _descendants(self, main):
        members = self._members()
        self.base.need(main['pid'] in members, 'capacity_application_missing_from_cgroup')
        processes, classes = {}, {}
        for pid in members:
            if pid == main['pid']:
                continue
            try:
                process = _process(pid)
            except ProcessUnavailable as error:
                self._disappearance(pid, error)
                continue
            self.base.need(self._security(process), 'capacity_descendant_security_profile')
            matched = [name for name, pin in self._native_pins.items() if self._matches_pin(process, pin)]
            if not matched and self._matches_pin(process, self.ctx['installed']['binary']):
                # Go's fork/exec child briefly retains the exact native image.
                self.base.need(process['parentPid'] == main['pid'], 'capacity_exec_transition_parent')
                matched = ['native_exec_transition']
            self.base.need(len(matched) == 1, 'capacity_descendant_executable')
            processes[pid], classes[pid] = process, matched[0]
        accepted = []
        for pid, process in processes.items():
            cursor = process
            visited = set()
            while cursor['parentPid'] != main['pid']:
                self.base.need(cursor['pid'] not in visited and len(visited) < MAX_PROCESSES,
                               'capacity_descendant_parent_cycle')
                visited.add(cursor['pid'])
                parent = processes.get(cursor['parentPid'])
                if parent is None:
                    # A live leaf with an unobserved parent is not a vanished leaf.
                    raise ProcessUnavailable('descendant_ancestry_unavailable')
                if int(parent['startTicks']) > int(cursor['startTicks']):
                    raise ProcessUnavailable('descendant_parent_lifetime_changed')
                cursor = parent
            self.base.need(int(main['startTicks']) <= int(cursor['startTicks']), 'capacity_descendant_precedes_application')
            try:
                current = _process(pid)
            except ProcessUnavailable as error:
                self._disappearance(pid, error)
                continue
            if current != process:
                raise ProcessUnavailable('descendant_exec_or_parent_changed')
            known = self._remember_process(process, classes[pid])
            if classes[pid] == 'native_exec_transition':
                self.base.need(time.monotonic_ns() - known['firstObservedMonotonicNs'] < STARTUP_SECONDS * 1000000000,
                               'capacity_descendant_exec_transition_timeout')
            accepted.append({'pid': process['pid'], 'startTicks': process['startTicks'], 'parentPid': process['parentPid'],
                             'executable': classes[pid]})
        return sorted(accepted, key=lambda process: process['pid'])

    def check_owned(self):
        try:
            self.base.need(self.record['configurationAccepted'] is True and not self.record['closed'],
                           'capacity_application_not_accepted_live')
            row = self._show()
            self._unit_contract(row)
            self.base.need(row['ActiveState'] == 'active' and row['SubState'] == 'running' and
                row['NRestarts'] == '0' and row['Result'] == 'success', 'capacity_application_not_running_normally')
            owner = self._ownership(row)
            self.base.need(owner == self.record['startOwnership'], 'capacity_application_lifetime_changed')
            until = min(self.deadline, self._operation_deadline, time.monotonic() + 1)
            while True:
                try:
                    process = _process(owner['pid'], command_line=True)
                    self.base.need(process == self.record['process'], 'capacity_application_profile_changed')
                    descendants = self._descendants(process)
                    self._console_identity(process['pid'])
                    self.base.need(_process(process['pid'], command_line=True) == process, 'capacity_application_profile_changed')
                    self._directory_profile()
                    self._oom()
                    result = {'unit': row, 'ownership': owner, 'process': process, 'descendants': descendants,
                              'observedMonotonicNs': time.monotonic_ns()}
                    self.record['latestOwnedObservation'] = result
                    return result
                except ProcessUnavailable as error:
                    self._disappearance(owner['pid'], error)
                    self.base.need(time.monotonic() < until, 'capacity_process_observation_unresolved')
                    time.sleep(min(0.05, max(0, until - time.monotonic())))
        except BaseException as error:
            self._remember_error('check_owned', error)
            raise

    def _cleanup_observation(self):
        until = min(self.deadline, self._operation_deadline, time.monotonic() + 10)
        original = self._operation_deadline
        self._operation_deadline = until
        try:
            while True:
                row = self._show()
                try:
                    authority = self._ownership(row, cleanup=True)
                    self.record['latestCleanupAuthority'] = {'unit': row, 'identity': authority}
                    return row
                except ProcessUnavailable as error:
                    self._disappearance(int(row.get('MainPID', '0')), error)
                except self.base.Rejected as error:
                    if str(error) not in ('application_ownership_process_unavailable', 'capacity_cleanup_main_absent_with_members'):
                        raise
                    self._disappearance(int(row.get('MainPID', '0')), error)
                self.base.need(time.monotonic() < until, 'capacity_cleanup_observation_timeout')
                time.sleep(min(0.05, max(0, until - time.monotonic())))
        finally:
            self._operation_deadline = original

    def _known_gone(self):
        known = dict(self._known)
        for key in ('startOwnership', 'cleanupOwnership'):
            if key in self.record:
                value = self.record[key]
                known[str(value['pid']) + ':' + value['startTicks']] = value
        gone, remaining = [], []
        for key, process in known.items():
            try:
                current = _stat(process['pid'])
            except ProcessUnavailable:
                gone.append(key)
                continue
            if current['startTicks'] != process['startTicks']:
                gone.append(key)
            else:
                remaining.append(key)
        self.record['knownOwnedLifetimesGone'] = gone
        self.record['knownOwnedLifetimesRemaining'] = remaining
        return not remaining

    def _physical_close(self):
        while True:
            row = self._show()
            self.support.check_file(self.base, self.ctx['installed']['unit'])
            self.base.need(row['Id'] == self.support.APP and row['FragmentPath'] == self.ctx['installed']['unit']['path'] and
                row['DropInPaths'] == '' and row['LoadState'] == 'loaded', 'capacity_terminal_unit_origin')
            if row['NRestarts'] != '0':
                self.record['automaticRestartObserved'] = True
            try:
                members = self._members()
            except ProcessUnavailable as error:
                self._disappearance(0, error)
                members = None
            if members == [] and row['MainPID'] == '0' and row['ActiveState'] in ('inactive', 'failed') and self._known_gone():
                self._ownership(row, cleanup=True)
                self.base.need(not os.path.lexists(self.support.RUNTIME), 'capacity_runtime_directory_retained')
                self.record.update(terminal=row, physicalClosed=True, closed=True, cgroupEmpty=True, runtimeDirectoryRemoved=True)
                return
            self.record['terminalObservation'] = row
            self._remaining(1)
            time.sleep(min(0.05, self._remaining(0.05)))

    def _read_log(self, path, *, uid, gid, maximum, identity=None):
        path = Path(path)
        before = self.base.metadata(path)
        self.base.need(path.resolve() == path and before['type'] == stat.S_IFREG and before['uid'] == uid and
            before['gid'] == gid and before['mode'] == 0o600 and before['links'] == 1 and
            0 <= before['bytes'] <= maximum, 'capacity_log_file_profile')
        if identity is not None:
            self.base.need(all(before[key] == identity[key] for key in STABLE), 'capacity_log_file_replaced')
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            opened = os.fstat(descriptor)
            self.base.need((opened.st_dev, opened.st_ino) == (before['device'], before['inode']), 'capacity_log_open_changed')
            body = bytearray()
            while len(body) < before['bytes']:
                self._remaining(1)
                part = os.read(descriptor, min(1 << 20, before['bytes'] - len(body)))
                self.base.need(bool(part), 'capacity_log_short_read')
                body.extend(part)
            self.base.need(os.read(descriptor, 1) == b'' and self.base.metadata(path) == before, 'capacity_log_changed_during_read')
        finally:
            os.close(descriptor)
        raw = bytes(body)
        return raw, {'path': str(path), 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(), 'metadata': before}

    def _log_records(self, raw):
        self.base.need(raw and raw.endswith(b'\n'), 'capacity_log_incomplete_record')
        records = []
        for line in raw.splitlines():
            self.base.need(len(records) < 65536, 'capacity_log_record_count')
            self.base.need(0 < len(line) <= 8192, 'capacity_log_record_bound')
            value = self.support.parse(line)
            self.base.need(isinstance(value, dict) and all(isinstance(value.get(key), str) for key in ('time', 'level', 'msg', 'event')),
                           'capacity_log_record_shape')
            records.append(value)
        return records

    def _shutdown_logs(self):
        self._console_identity()
        console, console_pin = self._read_log(self.units.APP_LOG, uid=0, gid=0, maximum=MAX_LOG_BYTES,
                                            identity=self.ctx['appLog']['metadata'])
        console_records = self._log_records(console)
        directory = self.base.metadata(self._diagnostics)
        self.base.need(self.record.get('diagnosticsDirectoryAbsentBeforeStart') is True and
            self._diagnostics.resolve() == self._diagnostics and directory['type'] == stat.S_IFDIR and
            directory['uid'] == 995 and directory['gid'] == 986 and directory['mode'] == 0o700, 'capacity_closed_diagnostics_directory')
        if 'diagnosticsDirectory' in self.record:
            self.base.need(all(directory[key] == self.record['diagnosticsDirectory'][key] for key in STABLE),
                           'capacity_closed_diagnostics_directory_replaced')
        marker_path = self._diagnostics / '.goby-diagnostics.json'
        marker_raw, marker_pin = self._read_log(marker_path, uid=995, gid=986, maximum=128 << 10)
        marker = self.support.parse(marker_raw)
        self.base.need(set(marker) == {'version', 'token', 'files'} and type(marker['version']) is int and marker['version'] == 1 and
            re.fullmatch('[0-9a-f]{32}', marker['token']) and isinstance(marker['files'], list) and
            1 <= len(marker['files']) <= 256, 'capacity_diagnostics_registry_shape')
        lock = self.base.metadata(self._diagnostics / '.goby-diagnostics.lock')
        self.base.need(lock['type'] == stat.S_IFREG and lock['uid'] == 995 and lock['gid'] == 986 and
            lock['mode'] == 0o600 and lock['links'] == 1 and lock['bytes'] == 0, 'capacity_diagnostics_lock_profile')
        names, files, diagnostic_records, total = set(), [], [], 0
        for entry in marker['files']:
            self.base.need(set(entry) <= {'name', 'created', 'identity', 'closed', 'size', 'deleting'} and
                entry['closed'] is True and entry.get('deleting', False) is False and type(entry['size']) is int and
                0 <= entry['size'] <= MAX_LOG_BYTES and isinstance(entry['created'], str) and
                re.fullmatch('goby-' + marker['token'] + r'-[0-9a-f]{32}\.jsonl', entry['name']) and entry['name'] not in names,
                'capacity_diagnostics_registered_file')
            names.add(entry['name'])
            raw, pin = self._read_log(self._diagnostics / entry['name'], uid=995, gid=986, maximum=MAX_LOG_BYTES)
            self.base.need(set(entry['identity']) == {'device', 'inode'} and
                all(type(entry['identity'][key]) is int and entry['identity'][key] == pin['metadata'][key] for key in ('device', 'inode')) and
                pin['bytes'] == entry['size'], 'capacity_diagnostics_file_identity')
            total += len(raw)
            self.base.need(total <= MAX_LOG_BYTES, 'capacity_diagnostics_total_bound')
            diagnostic_records.extend(self._log_records(raw))
            self.base.need(len(diagnostic_records) <= 65536, 'capacity_diagnostics_record_count')
            files.append(pin)
        self.base.need({path.name for path in self._diagnostics.iterdir()} == names | {'.goby-diagnostics.json', '.goby-diagnostics.lock'} and
            self.base.metadata(self._diagnostics) == directory and self.base.metadata(marker_path) == marker_pin['metadata'],
            'capacity_diagnostics_registry_changed')
        completed = lambda rows: [row for row in rows if row['event'] == 'server.shutdown.completed' and
            row['level'] == 'INFO' and row['msg'] == 'server shutdown completed' and
            type(row.get('duration_ms')) is int and row['duration_ms'] >= 0]
        bad = lambda rows: sum(row['level'] == 'ERROR' or row['event'] == 'server.shutdown.failed' for row in rows)
        console_completed, diagnostic_completed = completed(console_records), completed(diagnostic_records)
        self.record['shutdownLogs'] = {'console': console_pin, 'consoleRecords': len(console_records),
            'diagnosticsRegistry': marker_pin, 'diagnosticsFiles': files, 'diagnosticRecords': len(diagnostic_records),
            'consoleErrorEvents': bad(console_records), 'diagnosticErrorEvents': bad(diagnostic_records),
            'consoleCompletedCount': len(console_completed), 'diagnosticCompletedCount': len(diagnostic_completed),
            'completeConsoleEOF': True, 'allRegistryFilesClosed': True}
        self.base.need(len(console_completed) == len(diagnostic_completed) == 1 and
            console_completed[0] == diagnostic_completed[0] and bad(console_records) == bad(diagnostic_records) == 0,
            'capacity_shutdown_log_evidence')
        self.record['shutdownCompleted'] = True
        self.record['shutdownCompletedEvent'] = console_completed[0]

    def stop(self, strict, deadline):
        self.base.need(type(strict) is bool and type(deadline) in (int, float) and math.isfinite(deadline), 'capacity_stop_arguments')
        original = self._operation_deadline
        self._operation_deadline = min(original, self.deadline, float(deadline))
        reference = None
        try:
            if self.record['closed']:
                self.base.need(not strict or self.record['normalExit'] is True, 'capacity_stop_not_normal')
                return self.record
            if not self.record['startDispatched']:
                row = self._show()
                self._unit_contract(row)
                self.base.need(row['ActiveState'] == 'inactive' and row['MainPID'] == '0' and not self._members() and
                    not os.path.lexists(self.support.RUNTIME), 'capacity_unstarted_application_not_closed')
                self.record.update(closed=True, physicalClosed=True, cgroupEmpty=True, runtimeDirectoryRemoved=True,
                    knownOwnedLifetimesGone=[], knownOwnedLifetimesRemaining=[], neverStarted=True, normalExit=False, terminal=row)
                self.base.need(not strict, 'capacity_stop_not_normal')
                return self.record
            if strict and self.record['configurationAccepted']:
                try:
                    self.check_owned()
                except BaseException as error:
                    self._remember_error('before_normal_stop', error)
            row = self._cleanup_observation()
            if strict and self._first_error is None and self.record['configurationAccepted'] and \
                    not self.record.get('stopReferenceAttempted') and not self.record['stopAttempted'] and row['MainPID'] != '0':
                self.record['stopReferenceAttempted'] = True
                try:
                    process = self.record['process']
                    expected = {key: process[key] for key in ('pid', 'startTicks', 'exe', 'cgroup', 'networkNamespace')}
                    expected['uid'] = process['uids'][0]
                    reference = self.support.UnitReference(self.base, unit=self.support.APP,
                        unit_file=self.ctx['installed']['unit'], process=expected, invocation_id=self.record['invocationId'],
                        start_usec=self.record['startOwnership']['startMonotonicUsec'], boot_id=self.ctx['bootId'],
                        remaining=self._remaining, writer=self._reference_writer)
                    self.record['stopReferenceRecord'] = reference.record
                    reference.acquire()
                    self.record['stopReferenceBefore'] = reference.persist(self.support.E / 'private/application-stop-reference-before.json')
                except BaseException as error:
                    self._remember_error('stop_reference_acquire', error)
            try:
                if row['ActiveState'] in ('active', 'activating', 'deactivating') and not self.record['stopDispatched']:
                    self.record['stopAttempted'] = True
                    self.record['stopCommandAttempts'] = self.record.get('stopCommandAttempts', 0) + 1
                    if 'application-stop-intent.json' not in self._receipts:
                        try:
                            self.record['stopIntent'] = self._save('application-stop-intent.json',
                                {'unit': row, 'strictRequested': strict, 'authority': self.record['latestCleanupAuthority']})
                        except BaseException as error:
                            self._remember_error('stop_intent', error)
                    if reference is not None and reference.record['referenceConfirmed']:
                        try:
                            reference.mark_stop()
                        except BaseException as error:
                            self._remember_error('stop_reference_mark', error)
                    try:
                        self._command('capacity-app-stop', ['/usr/bin/systemctl', 'stop', self.support.APP], 25)
                    except BaseException as error:
                        self._remember_error('stop_control', error)
                    finally:
                        if reference is not None:
                            reference.set_stop_submitted(self.record['stopDispatched'])
                if reference is not None and reference.record['referenceConfirmed']:
                    try:
                        reference.capture_terminal()
                        self.record['stopReferenceTerminal'] = reference.persist_terminal(
                            self.support.E / 'private/application-stop-reference-terminal.json')
                        self._oom()
                    except BaseException as error:
                        self._remember_error('stop_reference_terminal', error)
            finally:
                if reference is not None:
                    reference.release()
                    self.record['stopReferenceReleased'] = reference.record['connectionClosed']
                    try:
                        self.record['stopReferenceReceipt'] = reference.persist(self.support.E / 'private/application-stop-reference.json')
                    except BaseException as error:
                        self._remember_error('stop_reference_receipt', error)
            self._physical_close()
            if strict and reference is not None and 'stopReferenceReceipt' in self.record:
                try:
                    proof = self.support.check_stop_reference(reference.record, reference.expected)
                    self.base.need(proof['execMainCode'] == 1 and proof['execMainStatus'] == 0 and proof['normalExit'] is True,
                                   'capacity_application_exit_status')
                    self.record['normalExitEvidence'] = proof
                    self._shutdown_logs()
                    self.record['normalExit'] = (self._first_error is None and self.record['stopDispatched'] and
                        not self.record['automaticRestartObserved'] and not self.record['oomObserved'])
                except BaseException as error:
                    self._remember_error('normal_stop_evidence', error)
            if 'closureReceipt' not in self.record:
                self.record['closureReceipt'] = self._save('application-closure.json', self.record)
            if strict:
                if self._first_error is not None:
                    raise self._first_error
                self.base.need(self.record['normalExit'] is True, 'capacity_stop_not_normal')
            return self.record
        except BaseException as error:
            self._remember_error('stop', error)
            raise
        finally:
            if reference is not None and reference.bus is not None:
                reference.release()
            self._operation_deadline = original
