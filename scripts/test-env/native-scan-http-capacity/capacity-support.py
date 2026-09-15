"""Fixed shared identity and namespace support for native scan capacity.

Fresh capacity scope; callers require their reviewed, frozen inputs.
Import only. Existing configuration and environment files are hash-only; master
files are metadata-only. New fixture context is private and never a public report.
"""
from contextlib import contextmanager
import hashlib
import importlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import time
import types

E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
PREFIX = 'goby-native-capacity-20260915'
ANCHOR, PGUNIT, APP = PREFIX + '-net.service', PREFIX + '-postgres.service', PREFIX + '-app.service'
UNIT_ROOT = Path('/run/systemd/system')
BINARY, ENVIRONMENT = F / 'bin/goby', F / 'config/goby.env'
STATE, CACHE, LOGS = F / 'state', F / 'cache', F / 'log'
RUNTIME = Path('/run') / PREFIX
BASE_PATH = Path('/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-isolated-restore-01/private/prepare-isolated-infrastructure.py')
BASE_SHA = 'ce20e4dbe21add70f90370b84e1debcf136313db6b98e8fb62ebe47433e6a9d0'
GUARD_PATH = Path('/opt/goby-test/m5-refresh-browser-20260915/private/m5-refresh-browser-prepare.py')
GUARD_SHA = '8c682ccd5e616e44b32db52dbd9325e66d358f820ae75b1af5a0a1c5b2100927'
GUARD_INPUT = Path('/opt/goby-test/m5-refresh-browser-20260915/private/input.json')
GUARD_INPUT_SHA = 'ee93341937e01ac708b7f7245b7deae5cecb37451f04c44aa637da811c957332'
BINARY_SHA = '7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1'
BASE_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'SYSTEMD_COLORS': '0'}
ANCHOR_RUNTIME_SECONDS = 3600
RUNTIME_TOTAL_SECONDS = 1200
SEAL_TOTAL_SECONDS = 900
HANDOFF_SECONDS = 60
FINAL_STOP_SAFETY_SECONDS = 180
LIFETIME_REQUIRED_SECONDS = {
    'prepare_handoff': RUNTIME_TOTAL_SECONDS + SEAL_TOTAL_SECONDS + 2 * HANDOFF_SECONDS + FINAL_STOP_SAFETY_SECONDS,
    'runtime_entry': RUNTIME_TOTAL_SECONDS + SEAL_TOTAL_SECONDS + HANDOFF_SECONDS + FINAL_STOP_SAFETY_SECONDS,
    'seal_entry': SEAL_TOTAL_SECONDS + FINAL_STOP_SAFETY_SECONDS,
}
SERVICE_PROPERTIES = (
    'Id', 'LoadState', 'ActiveState', 'SubState', 'FragmentPath', 'DropInPaths', 'Type', 'User', 'Group',
    'ExecStart', 'EnvironmentFiles', 'Environment', 'WorkingDirectory', 'StateDirectory', 'StateDirectoryMode',
    'CacheDirectory', 'CacheDirectoryMode', 'LogsDirectory', 'LogsDirectoryMode', 'RuntimeDirectory', 'RuntimeDirectoryMode',
    'Restart', 'RestartUSec', 'TimeoutStopUSec', 'NoNewPrivileges', 'ProtectSystem', 'ProtectHome', 'PrivateTmp',
    'CapabilityBoundingSet', 'AmbientCapabilities', 'RestrictSUIDSGID', 'UMask', 'NetworkNamespacePath',
    'ReadOnlyPaths', 'ReadWritePaths', 'InaccessiblePaths', 'MemoryMax', 'MemorySwapMax', 'TasksMax', 'CPUQuotaPerSecUSec', 'RuntimeMaxUSec',
    'ProtectKernelTunables', 'ProtectKernelModules', 'ProtectKernelLogs', 'ProtectControlGroups', 'RestrictNamespaces',
    'RestrictAddressFamilies', 'StandardInput', 'LimitCORE', 'OOMPolicy', 'MainPID', 'ExecMainPID', 'InvocationID',
    'NRestarts', 'Result', 'ExecMainCode', 'ExecMainStatus', 'ActiveEnterTimestampMonotonic', 'ExecMainStartTimestampMonotonic',
    'NeedDaemonReload', 'KillMode', 'KillSignal', 'FinalKillSignal', 'SendSIGKILL', 'StandardOutput', 'StandardError',
    'ControlGroup', 'ExecMainExitTimestampMonotonic',
)
_guard = None
_guard_value = None
REFERENCE_API = {'path': '/opt/goby-test/m6-systemd-stop-evidence-20260915/private/api-preparation.json',
                 'sha256': '4668e5b4952eb8ee8a8f57d3a69863c309914e757fa85390bf3b5c70bc1a88df', 'bytes': 3666}
REFERENCE_UNIT_FIELDS = ('Id', 'LoadState', 'ActiveState', 'SubState', 'InvocationID', 'Refs')
REFERENCE_SERVICE_FIELDS = ('MainPID', 'ExecMainPID', 'ExecMainCode', 'ExecMainStatus', 'Result', 'NRestarts',
    'ExecMainStartTimestampMonotonic', 'ExecMainExitTimestampMonotonic', 'User', 'Group', 'Restart',
    'RuntimeMaxUSec', 'TimeoutStopUSec', 'PrivateNetwork')
REFERENCE_TYPES = {**{key: 'String' for key in ('Id', 'LoadState', 'ActiveState', 'SubState', 'Result', 'User', 'Group', 'Restart')},
    **{key: 'UInt32' for key in ('MainPID', 'ExecMainPID', 'NRestarts')},
    **{key: 'UInt64' for key in ('ExecMainStartTimestampMonotonic', 'ExecMainExitTimestampMonotonic', 'RuntimeMaxUSec', 'TimeoutStopUSec')},
    'ExecMainCode': 'Int32', 'ExecMainStatus': 'Int32', 'PrivateNetwork': 'Boolean', 'InvocationID': 'Array', 'Refs': 'Array'}


def reference_api(base):
    """Load only the eleven previously observed dbus-python dependencies."""
    base.need(sys.flags.isolated and sys.flags.dont_write_bytecode, 'reference_python_environment')
    raw = base.read_regular(Path(REFERENCE_API['path']), 1 << 20)
    base.need(len(raw) == REFERENCE_API['bytes'] and base.digest(raw) == REFERENCE_API['sha256'], 'reference_api_pin')
    api = parse(raw)
    base.need(api['status'] == 'prepared_readonly' and api['dbusVersion'] == '1.4.0' and api['busConnectionClosed'] is True and
              api['unitReferenceCalls'] == api['unitActions'] == api['servicesCreated'] == 0 and len(api['modules']) == 11,
              'reference_api_contract')
    for name, pin in api['modules'].items():
        base.need(name == 'dbus' or name.startswith('dbus.') or name in ('_dbus_bindings', '_dbus_glib_bindings'), 'reference_module_name')
        base.need(set(pin) == {'path', 'sha256', 'bytes'}, 'reference_module_descriptor')
        path = Path(pin['path']); before = base.metadata(path)
        body = base.read_regular(path, 4 << 20)
        base.need(before['uid'] == 0 and before['type'] == stat.S_IFREG and before['links'] == 1 and not before['mode'] & 0o022 and
                  len(body) == pin['bytes'] and base.digest(body) == pin['sha256'] and base.metadata(path) == before, 'reference_module_changed')
    dbus = importlib.import_module('dbus')
    for name, pin in api['modules'].items():
        loaded = importlib.import_module(name)
        base.need(Path(loaded.__file__).resolve() == Path(pin['path']) and
                  base.digest(base.read_regular(Path(pin['path']), 4 << 20)) == pin['sha256'], 'reference_loaded_module_changed')
    base.need(dbus.__version__ == '1.4.0', 'reference_dbus_version')
    return dbus


def application_ownership(base, row, dispatch_usec, boot_id, expected=None, allow_restart=False):
    """Bind the owned APP lifetime without accepting its pre-exec configuration.

    The caller must first verify its dispatched start and pinned unit file.
    UID, executable and namespace deliberately belong to final configuration
    acceptance: systemd's Type=simple executor can change them before exec.
    """
    base.need(type(dispatch_usec) is int and dispatch_usec > 0 and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == boot_id,
              'application_ownership_boot_or_dispatch')
    base.need(row.get('Id') == APP and row.get('LoadState') == 'loaded' and
              row.get('FragmentPath') == str(UNIT_ROOT / APP) and
              row.get('ActiveState') in ('active', 'activating', 'deactivating') and
              row.get('ControlGroup') == '/system.slice/' + APP and
              row.get('MainPID', '').isdigit() and int(row['MainPID']) > 1 and
              row.get('ExecMainPID') == row['MainPID'] and
              re.fullmatch('[0-9a-f]{32}', row.get('InvocationID', '')) and
              int(row['InvocationID'], 16) != 0 and row.get('NRestarts', '').isdigit() and
              row.get('ExecMainStartTimestampMonotonic', '').isdigit() and
              int(row['ExecMainStartTimestampMonotonic']) >= dispatch_usec,
              'application_ownership_unit')
    pid = int(row['MainPID'])
    def process_projection():
        root = Path('/proc') / str(pid)
        try:
            fields = (root / 'stat').read_text().rsplit(') ', 1)[1].split()
            base.need(fields[0] not in ('Z', 'X'), 'application_ownership_process_unavailable')
            return {'pid': pid, 'startTicks': fields[19], 'cgroup': (root / 'cgroup').read_text().strip()}
        except (FileNotFoundError, ProcessLookupError):
            raise base.Rejected('application_ownership_process_unavailable') from None
    projection = process_projection()
    base.need(projection['pid'] == pid and projection['cgroup'] == '0::/system.slice/' + APP and
              str(projection['startTicks']).isdigit() and int(projection['startTicks']) > 0,
              'application_ownership_process')
    result = {'bootId': boot_id, 'pid': pid, 'startTicks': projection['startTicks'],
              'invocationId': row['InvocationID'],
              'startMonotonicUsec': int(row['ExecMainStartTimestampMonotonic']),
              'dispatchMonotonicUsec': dispatch_usec, 'cgroup': projection['cgroup']}
    if expected is not None:
        base.need(isinstance(expected, dict) and set(expected) == set(result) and
                  expected['bootId'] == boot_id and expected['dispatchMonotonicUsec'] == dispatch_usec and
                  expected['cgroup'] == result['cgroup'] and type(expected['pid']) is int and expected['pid'] > 1 and
                  type(expected['startMonotonicUsec']) is int and expected['startMonotonicUsec'] >= dispatch_usec and
                  re.fullmatch('[0-9a-f]{32}', expected['invocationId']) and int(expected['invocationId'], 16) != 0,
                  'application_ownership_record')
        base.need(result == expected or (allow_restart and int(row['NRestarts']) > 0 and
                  result['invocationId'] != expected['invocationId'] and
                  result['startMonotonicUsec'] > expected['startMonotonicUsec']),
                  'application_ownership_replaced')
    base.need(process_projection() == projection and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == boot_id,
              'application_ownership_changed')
    return result


def application_start_cancellation(base, row, dispatch_usec, boot_id, before_start):
    """Prove APP start cancellation without establishing process ownership.

    The caller must verify its actual start dispatch, exact unit file pins,
    and an empty APP cgroup. The complete before_start row must have passed
    unit authority before dispatch. Historical process IDs are only evidence
    fields here; they never authorize a process lookup or a PID lifetime tuple.
    """
    base.need(type(dispatch_usec) is int and dispatch_usec > 0 and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == boot_id,
              'application_start_cancellation_boot_or_dispatch')
    identity_fields = ('Id', 'LoadState', 'FragmentPath', 'ControlGroup', 'ActiveState',
                       'MainPID', 'ExecMainPID', 'InvocationID', 'NRestarts',
                       'ExecMainStartTimestampMonotonic')
    numeric_fields = ('ExecMainPID', 'NRestarts', 'ExecMainStartTimestampMonotonic')
    for label, snapshot in (('before', before_start), ('observed', row)):
        base.need(isinstance(snapshot, dict) and snapshot.get('Id') == APP and
                  snapshot.get('LoadState') == 'loaded' and
                  snapshot.get('FragmentPath') == str(UNIT_ROOT / APP) and
                  snapshot.get('ControlGroup') in ('', '/system.slice/' + APP) and
                  snapshot.get('MainPID') == '0',
                  'application_start_cancellation_' + label + '_unit')
        invocation = snapshot.get('InvocationID')
        base.need(all(isinstance(snapshot.get(key), str) and
                      re.fullmatch('[0-9]+', snapshot[key]) for key in numeric_fields) and
                  isinstance(invocation, str) and
                  (invocation == '' or (re.fullmatch('[0-9a-f]{32}', invocation) and
                                        int(invocation, 16) != 0)),
                  'application_start_cancellation_' + label + '_identity')
    base.need(before_start.get('ActiveState') == 'inactive' and
              row.get('ActiveState') in ('active', 'activating', 'deactivating', 'inactive', 'failed'),
              'application_start_cancellation_state')
    previous_pid = int(before_start['ExecMainPID'])
    previous_start = int(before_start['ExecMainStartTimestampMonotonic'])
    observed_pid = int(row['ExecMainPID'])
    observed_start = int(row['ExecMainStartTimestampMonotonic'])
    base.need(previous_pid != 1 and observed_pid != 1 and previous_start < dispatch_usec,
              'application_start_cancellation_previous_lifetime')
    unchanged_or_cleared = (observed_pid in (0, previous_pid) and
                            observed_start in (0, previous_start))
    new_start_recorded = (observed_pid != 1 and observed_start >= dispatch_usec and
                          observed_start > previous_start and
                          (row['InvocationID'] == '' or
                           row['InvocationID'] != before_start['InvocationID']))
    base.need(unchanged_or_cleared or new_start_recorded,
              'application_start_cancellation_causality')
    return {'kind': 'm6-owned-application-start-cancellation', 'unit': APP,
            'bootId': boot_id, 'dispatchMonotonicUsec': dispatch_usec,
            'beforeStart': {key: before_start[key] for key in identity_fields},
            'observedUnit': {key: row[key] for key in identity_fields},
            'classification': 'owned_unit_start_without_live_process',
            'processOwnershipEstablished': False}


def pending_application_restart(base, row, dispatch_usec, boot_id, expected):
    """Prove cancellation of an owned restart job, without inventing a PID tuple.

    The caller has checked the pinned unit file and an empty APP cgroup.
    The previous process must already be gone; this cannot accept a live image.
    """
    base.need(isinstance(expected, dict) and expected['bootId'] == boot_id and
              expected['dispatchMonotonicUsec'] == dispatch_usec and
              expected['cgroup'] == '0::/system.slice/' + APP and
              type(expected['pid']) is int and expected['pid'] > 1 and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == boot_id,
              'pending_restart_previous_owner')
    base.need(row.get('Id') == APP and row.get('LoadState') == 'loaded' and
              row.get('FragmentPath') == str(UNIT_ROOT / APP) and
              row.get('ActiveState') == 'activating' and row.get('SubState') in ('auto-restart', 'auto-restart-queued') and
              row.get('MainPID') == '0' and row.get('ExecMainPID', '').isdigit() and
              row.get('ControlGroup') in ('', '/system.slice/' + APP) and row.get('NRestarts', '').isdigit() and
              row.get('ExecMainStartTimestampMonotonic', '').isdigit(), 'pending_restart_unit')
    pid, started = int(row['ExecMainPID']), int(row['ExecMainStartTimestampMonotonic'])
    invocation = row.get('InvocationID', '')
    same_start = (invocation in ('', expected['invocationId']) and pid in (0, expected['pid']) and
                  started == expected['startMonotonicUsec'])
    later_restart = (int(row['NRestarts']) > 0 and re.fullmatch('[0-9a-f]{32}', invocation) and
                     int(invocation, 16) != 0 and invocation != expected['invocationId'] and
                     pid > 1 and started > expected['startMonotonicUsec'])
    base.need((same_start or later_restart) and started >= dispatch_usec and
              not Path('/proc/' + str(expected['pid'])).exists() and
              (pid == 0 or not Path('/proc/' + str(pid)).exists()), 'pending_restart_causality')
    return {'previousOwnership': expected, 'execMainPid': pid, 'invocationId': invocation or expected['invocationId'],
            'startMonotonicUsec': started, 'nRestarts': int(row['NRestarts']), 'classification': 'owned_pending_restart_job'}


def check_stop_reference(record, expected):
    """Validate saved normal-exit evidence independently of later systemctl show."""
    def require(value, code):
        if not value: raise ValueError(code)
    require(expected['unit'] in (APP, PGUNIT, ANCHOR) and type(expected['pid']) is int and expected['pid'] > 1 and
            type(expected['startMonotonicUsec']) is int and expected['startMonotonicUsec'] > 0, 'reference_unit_scope')
    require(record['kind'] == 'm6-systemd-stop-reference' and type(record['version']) is int and record['version'] == 1 and
            encoded(record['expected']) == encoded(expected) and encoded(record['api']) == encoded(REFERENCE_API) and
            record['apiVerified'] is True and type(record['connectionAttempts']) is int and record['connectionAttempts'] == 1 and
            record['acquireAttempted'] is True and record['referenceConfirmed'] is True and record['stopSubmitted'] is True and record['connectionClosed'] is True and
            record['terminalPersistedWhileReferenced'] is True and not record['errors'], 'stop_reference_continuity')
    before, terminal, retained = (record[name] for name in ('before', 'terminal', 'retained'))
    for snapshot in (before, terminal, retained):
        require(snapshot['dbusTypes'] == REFERENCE_TYPES and set(snapshot['values']) == set(REFERENCE_TYPES), 'stop_reference_types')
        values = snapshot['values']
        require(values['Id'] == expected['unit'] and record['busUniqueName'] in values['Refs'] and
                values['InvocationID'] == expected['invocationId'] and values['ExecMainPID'] == expected['pid'] and
                values['ExecMainStartTimestampMonotonic'] == expected['startMonotonicUsec'] and values['NRestarts'] == 0,
                'stop_reference_identity')
        for key, kind in REFERENCE_TYPES.items():
            if kind in ('UInt32', 'UInt64', 'Int32'): require(type(values[key]) is int, 'stop_reference_numeric_type')
            elif kind == 'String': require(type(values[key]) is str, 'stop_reference_string_type')
            elif kind == 'Boolean': require(type(values[key]) is bool, 'stop_reference_boolean_type')
        require(isinstance(values['Refs'], list) and all(type(item) is str for item in values['Refs']) and
                type(values['InvocationID']) is str, 'stop_reference_array_projection')
    require(before['values']['MainPID'] == expected['pid'] and before['values']['ActiveState'] == 'active' and
            terminal['values'] == retained['values'], 'stop_reference_retained_values')
    values = terminal['values']
    allowed = ((1, 0), (2, 15)) if expected['unit'] == ANCHOR else ((1, 0),)
    require(values['MainPID'] == 0 and values['ActiveState'] == 'inactive' and values['Result'] == 'success' and
            (values['ExecMainCode'], values['ExecMainStatus']) in allowed, 'stop_reference_exit_not_normal')
    exit_ns = values['ExecMainExitTimestampMonotonic'] * 1000
    times = [before['observedMonotonicNs'], record['stopRequestedMonotonicNs'], terminal['observedMonotonicNs'],
             retained['observedMonotonicNs'], record['terminalPersistedMonotonicNs'], record['releasedMonotonicNs']]
    require(all(type(value) is int and value > 0 for value in times), 'stop_reference_time_type')
    require(expected['startMonotonicUsec'] * 1000 <= before['observedMonotonicNs'] <= record['stopRequestedMonotonicNs'] <= exit_ns <= terminal['observedMonotonicNs'] and
            terminal['observedMonotonicNs'] <= retained['observedMonotonicNs'] <= record['terminalPersistedMonotonicNs'] <= record['releasedMonotonicNs'],
            'stop_reference_exit_window')
    return {'normalExit': True, 'unit': expected['unit'], 'pid': expected['pid'], 'invocationId': expected['invocationId'],
            'execMainCode': values['ExecMainCode'], 'execMainStatus': values['ExecMainStatus'], 'result': values['Result'],
            'exitMonotonicUsec': values['ExecMainExitTimestampMonotonic'], 'connectionClosed': True}


class UnitReference:
    """Hold one private connection/reference through this owned unit's stop."""
    def __init__(self, base, *, unit, unit_file, process, invocation_id, start_usec, boot_id, remaining, writer=None):
        self.base, self.remaining = base, remaining
        self.writer = writer
        base.need(unit in (APP, PGUNIT, ANCHOR) and type(start_usec) is int and start_usec > 0 and
                  re.fullmatch('[0-9a-f]{32}', invocation_id) and int(invocation_id, 16) != 0, 'reference_expected_identity')
        path = str(UNIT_ROOT / unit)
        base.need(unit_file['path'] == path and process['pid'] > 1 and
                  process['uid'] == (995 if unit == APP else 103 if unit == PGUNIT else 0) and
                  process['cgroup'] == '0::/system.slice/' + unit, 'reference_expected_owner')
        self.unit = unit
        self.expected = {'unit': unit, 'unitFile': unit_file, 'process': process, 'pid': process['pid'],
                         'invocationId': invocation_id, 'startMonotonicUsec': start_usec, 'bootId': boot_id}
        self.record = {'kind': 'm6-systemd-stop-reference', 'version': 1, 'expected': self.expected, 'api': dict(REFERENCE_API),
            'apiVerified': False, 'acquireAttempted': False, 'connectionAttempts': 0, 'referenceAttempted': False, 'referenceConfirmed': False,
            'stopSubmitted': False, 'connectionClosed': False, 'terminalPersistedWhileReferenced': False,
            'errors': [], 'releaseErrors': []}
        self.dbus = self.bus = self.manager = None

    def error(self, phase, error):
        self.record['errors'].append({'phase': phase, 'type': type(error).__name__})

    def acquire(self):
        self.base.need(not self.record['acquireAttempted'], 'reference_reconnect_forbidden')
        self.record['acquireAttempted'] = True
        try:
            self.remaining(3)
            check_file(self.base, self.expected['unitFile'])
            self.base.need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == self.expected['bootId'] and
                           self.base.process_identity(self.expected['pid']) == self.expected['process'], 'reference_process_changed')
            self.dbus = reference_api(self.base); self.record['apiVerified'] = True
            self.record['connectionAttempts'] = 1
            self.bus = self.dbus.SystemBus(private=True)
            self.record['busUniqueName'] = str(self.bus.get_unique_name())
            proxy = self.bus.get_object('org.freedesktop.systemd1', '/org/freedesktop/systemd1', introspect=False)
            self.manager = self.dbus.Interface(proxy, 'org.freedesktop.systemd1.Manager')
            self.record['objectPath'] = str(self.manager.LoadUnit(self.unit, timeout=self.remaining(3)))
            self.record['referenceAttempted'] = True
            self.manager.RefUnit(self.unit, timeout=self.remaining(3)); self.record['referenceConfirmed'] = True
            self.record['before'] = self.snapshot()
            before = self.record['before']['values']
            self.base.need(before['MainPID'] == before['ExecMainPID'] == self.expected['pid'] and before['ActiveState'] == 'active' and
                before['InvocationID'] == self.expected['invocationId'] and before['NRestarts'] == 0 and
                before['ExecMainStartTimestampMonotonic'] == self.expected['startMonotonicUsec'] and
                self.base.process_identity(self.expected['pid']) == self.expected['process'], 'reference_live_binding')
        except BaseException as error:
            self.error('acquire', error)
            raise

    def snapshot(self):
        self.base.need(self.bus is not None and self.bus.get_is_connected() and self.record['referenceConfirmed'], 'reference_connection_lost')
        proxy = self.bus.get_object('org.freedesktop.systemd1', self.record['objectPath'], introspect=False)
        properties = self.dbus.Interface(proxy, 'org.freedesktop.DBus.Properties')
        unit = properties.GetAll('org.freedesktop.systemd1.Unit', timeout=self.remaining(3))
        service = properties.GetAll('org.freedesktop.systemd1.Service', timeout=self.remaining(3))
        values, types_seen = {}, {}
        for name, source in [(key, unit) for key in REFERENCE_UNIT_FIELDS] + [(key, service) for key in REFERENCE_SERVICE_FIELDS]:
            value = source[name]; types_seen[name] = type(value).__name__
            self.base.need(types_seen[name] == REFERENCE_TYPES[name], 'reference_dbus_type_changed')
            if name == 'InvocationID': values[name] = bytes(value).hex()
            elif name == 'Refs': values[name] = [str(item) for item in value]
            elif isinstance(value, self.dbus.Boolean): values[name] = bool(value)
            elif isinstance(value, (self.dbus.String, self.dbus.ObjectPath)): values[name] = str(value)
            else: values[name] = int(value)
        result = {'observedMonotonicNs': time.monotonic_ns(), 'values': values, 'dbusTypes': types_seen}
        self.base.need(values['Id'] == self.unit and self.record['busUniqueName'] in values['Refs'] and self.bus.get_is_connected(),
                       'reference_not_held')
        return result

    def mark_stop(self):
        self.base.need('stopRequestedMonotonicNs' not in self.record, 'reference_stop_already_marked')
        self.record['stopRequestedMonotonicNs'] = time.monotonic_ns()

    def set_stop_submitted(self, submitted):
        self.record['stopSubmitted'] = submitted is True

    def capture_terminal(self):
        try:
            self.base.need('terminal' not in self.record, 'reference_terminal_already_observed')
            self.record['terminal'] = self.snapshot()
            self.record['retained'] = self.snapshot()
            self.base.need(self.record['terminal']['values'] == self.record['retained']['values'], 'referenced_terminal_changed')
        except BaseException as error:
            self.error('terminal', error)
            raise

    def persist(self, path):
        try:
            path = Path(path)
            self.base.need(path.is_relative_to(E / 'private'), 'reference_receipt_scope')
            raw = encoded(self.record)
            self.base.need(len(raw) <= 1 << 20, 'reference_receipt_budget')
            result = {'path': str(path), 'bytes': len(raw), 'sha256': self.base.digest(raw)}
            if self.writer is None:
                self.base.write_new(path, raw)
            else:
                written = self.writer(path, self.record)
                self.base.need(written.get('persisted') is not False and all(written[key] == value for key, value in result.items()),
                               'reference_receipt_not_persisted')
            return result
        except BaseException as error:
            self.error('persist', error)
            raise

    def persist_terminal(self, path):
        self.base.need(self.bus is not None and self.bus.get_is_connected() and self.record['referenceConfirmed'], 'terminal_reference_lost')
        result = self.persist(path)
        self.base.need(self.bus.get_is_connected(), 'terminal_persistence_connection_lost')
        self.record.update(terminalEvidence=result, terminalPersistedWhileReferenced=True, terminalPersistedMonotonicNs=time.monotonic_ns())
        return result

    def release(self):
        try:
            if self.bus is not None:
                if not self.bus.get_is_connected():
                    self.error('release', RuntimeError('unexpected_connection_loss'))
                elif self.manager is not None and self.record['referenceAttempted']:
                    try:
                        self.manager.UnrefUnit(self.unit, timeout=self.remaining(3)); self.record['unrefConfirmed'] = True
                    except BaseException as error:
                        self.record['releaseErrors'].append(type(error).__name__)
        except BaseException as error:
            self.error('release', error)
        finally:
            try:
                if self.bus is not None:
                    self.bus.close()
                    self.record['connectionClosed'] = not self.bus.get_is_connected()
                else:
                    self.record['connectionClosed'] = True
            except BaseException as error:
                self.error('connection_close', error)
            finally:
                self.record['releasedMonotonicNs'] = time.monotonic_ns()
                self.bus = self.manager = None


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def parse(raw):
    def unique(pairs):
        result = dict(pairs)
        if len(result) != len(pairs): raise ValueError('duplicate_json_member')
        return result
    def invalid(_value): raise ValueError('nonfinite_json_number')
    return json.loads(raw, object_pairs_hook=unique, parse_constant=invalid)


def final_observer_marker_sql():
    """Return the fixed final observer identity projection."""
    return "jsonb_build_object('pid',pg_backend_pid(),'database',current_database(),'databaseOid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),'roleOid',(SELECT oid::bigint FROM pg_roles WHERE rolname='goby_native_capacity'),'user',current_user,'readOnly',current_setting('transaction_read_only'),'isolation',current_setting('transaction_isolation'),'systemId',(SELECT system_identifier::text FROM pg_control_system()),'dataDirectory',current_setting('data_directory'),'port',current_setting('port'))"


def check_final_observer_identity(base, context, identity):
    """Require the final observer's exact identity and numeric OID contract."""
    base.need(isinstance(identity, dict) and
              type(identity.get('pid')) is int and identity['pid'] > 1 and
              type(identity.get('databaseOid')) is int and identity['databaseOid'] > 0 and
              identity['databaseOid'] == context['databaseOid'] and
              type(identity.get('roleOid')) is int and identity['roleOid'] > 0 and
              identity['roleOid'] == context['roleOid'] and
              identity.get('database') == context['database'] and identity.get('user') == 'postgres' and
              identity.get('readOnly') == 'on' and identity.get('isolation') == 'repeatable read' and
              identity.get('systemId') == context['postgres']['systemId'] and
              identity.get('dataDirectory') == str(F / 'pg/data') and identity.get('port') == '25499',
              'final_database_identity_or_clients')


def bootstrap(path, checksum, name):
    if path.resolve() != path: raise ValueError('support_source_symlink')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_nlink != 1 or before.st_size > 2 << 20:
            raise ValueError('support_source_identity')
        raw = os.read(fd, (2 << 20) + 1)
        after = os.fstat(fd)
        if len(raw) != before.st_size or hashlib.sha256(raw).hexdigest() != checksum or any(
            getattr(before, field) != getattr(after, field) for field in ('st_dev', 'st_ino', 'st_mode', 'st_mtime_ns', 'st_ctime_ns')):
            raise ValueError('support_source_changed')
    finally: os.close(fd)
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def load_base():
    return bootstrap(BASE_PATH, BASE_SHA, 'native_capacity_owned_command_primitives')


def guard_module(base):
    global _guard, _guard_value
    if _guard is None:
        _guard = bootstrap(GUARD_PATH, GUARD_SHA, 'native_capacity_existing_runtime_guard')
        raw = base.read_regular(GUARD_INPUT, 2 << 20)
        base.need(base.digest(raw) == GUARD_INPUT_SHA, 'runtime_guard_input_changed')
        _guard_value = parse(raw)
    return _guard, _guard_value


def acquire_lock(base):
    module, _value = guard_module(base)
    return module.acquire_lock(base)


def protected(base, context=None):
    current = os.readlink('/proc/self/ns/net')
    base.need(current == os.readlink('/proc/1/ns/net') and
              (context is None or current == context['hostNetworkNamespace']), 'protected_observer_not_in_host_namespace')
    module, value = guard_module(base)
    return module.protected(base, value)


def file_pin(base, path, maximum=256 << 20):
    before = base.metadata(path)
    raw = base.read_regular(path, maximum)
    base.need(base.metadata(path) == before, 'file_changed_during_pin')
    return {'path': str(path), 'sha256': base.digest(raw), 'bytes': len(raw), 'metadata': before}


def descriptor(pin, path=None):
    if not isinstance(pin, dict) or set(pin) != {'path', 'sha256', 'bytes'} or not isinstance(pin['path'], str):
        raise ValueError('descriptor_shape')
    target = Path(pin['path'])
    if not target.is_absolute() or '..' in target.parts or (path is not None and target != path) or not re.fullmatch('[0-9a-f]{64}', pin['sha256']):
        raise ValueError('descriptor_path_or_digest')
    if type(pin['bytes']) is not int or not 0 < pin['bytes'] <= 256 << 20: raise ValueError('descriptor_size')
    return target


def pinned(base, pin, maximum=8 << 20, path=None):
    target = descriptor(pin, path)
    raw = base.read_regular(target, maximum)
    base.need(len(raw) == pin['bytes'] and base.digest(raw) == pin['sha256'], 'pinned_file_changed')
    return raw


def check_file(base, pin):
    base.need(isinstance(pin, dict) and set(pin) == {'path', 'sha256', 'bytes', 'metadata'}, 'owned_file_pin_contract')
    base.need(file_pin(base, Path(pin['path'])) == pin, 'owned_file_changed')


def check_installation(base, context):
    """Check only this fixture's three pinned files, without a shipped-unit claim.

    The preparation source generates the custom unit and binds its descriptor
    into the private context. Its caller must independently check the expected
    systemd properties and the prepared property snapshot before using the unit.
    The generated environment is hashed but is never decoded by this function.
    """
    expected = {'binary': str(BINARY), 'unit': str(UNIT_ROOT / APP), 'environment': str(ENVIRONMENT)}
    base.need(set(context['installed']) == set(expected), 'installation_file_set')
    for name, path in expected.items():
        row = context['installed'][name]
        base.need(row['path'] == path and row['metadata']['uid'] == row['metadata']['gid'] == 0 and
                  row['metadata']['mode'] == (0o755 if name == 'binary' else 0o600 if name == 'environment' else 0o644), 'installed_file_identity')
        # Hash the rendered environment without parsing its private bytes.
        check_file(base, row)
    base.need(context['installed']['binary']['sha256'] == BINARY_SHA, 'retained_binary_changed')
    return {'installedFilesExact': True, 'binarySha256': BINARY_SHA,
            'unitSha256': context['installed']['unit']['sha256'], 'installationAcceptance': False}


def service_properties(base):
    raw = base.run('capacity-service-properties', ['/usr/bin/systemctl', 'show', APP, '--no-pager',
                   '--property=' + ','.join(SERVICE_PROPERTIES)], 15)
    return dict(line.split('=', 1) for line in raw.decode().splitlines() if '=' in line)


def pg_mount(base):
    path = F / 'pg'
    rows = [line for line in Path('/proc/self/mountinfo').read_text().splitlines() if line.split()[4] == str(path)]
    base.need(len(rows) == 1, 'owned_pg_mount_count')
    fields = rows[0].split(); marker = fields.index('-')
    base.need(fields[marker + 1:marker + 3] == ['tmpfs', PREFIX + '-postgres'] and
              {'rw', 'nosuid', 'nodev', 'noexec'} <= set(fields[5].split(',')) and
              'size=786432k' in fields[marker + 3].split(','), 'owned_pg_mount_options')
    info = base.metadata(path)
    base.need(info['type'] == stat.S_IFDIR and info['uid'] == 103 and info['gid'] == 106 and info['mode'] == 0o700,
              'owned_pg_mount_directory')
    return {'mountLine': rows[0], 'metadata': {key: info[key] for key in ('device', 'inode', 'uid', 'gid', 'mode', 'type')}}


def check_infrastructure(base, context, full=False):
    base.need(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == context['bootId'], 'installation_boot_changed')
    for name, value in ((ANCHOR, context['anchor']), (PGUNIT, context['postgres'])):
        base.need(base.process_identity(value['process']['pid']) == value['process'], 'owned_infrastructure_process_changed')
        base.need(value['process']['networkNamespace'] == context['networkNamespace'], 'owned_infrastructure_namespace_changed')
        if full:
            check_file(base, context['unitFiles'][name])
            current = base.show(name)
            base.need(current == value['unit'] and current['ActiveState'] == 'active' and current['SubState'] == 'running',
                      'owned_infrastructure_unit_changed')
    base.need(context['anchorPid'] == context['anchor']['process']['pid'] and context['postgres']['pid'] == context['postgres']['process']['pid'] and
              context['networkNamespace'] != context['hostNetworkNamespace'], 'infrastructure_context_identity')
    if full and 'pgMount' in context:
        base.need(pg_mount(base) == context['pgMount'], 'prepared_pg_volume_changed')
    return {'anchorPid': context['anchorPid'], 'postgresPid': context['postgres']['pid'], 'identitiesExact': True}


def lifetime_budget(phase, start_usec, runtime_usec, now_usec):
    """Project only the three fixed handoffs against one original anchor clock."""
    if phase not in LIFETIME_REQUIRED_SECONDS or any(type(value) is not int or value < 0
                                                   for value in (start_usec, runtime_usec, now_usec)):
        raise ValueError('anchor_lifetime_arguments')
    deadline = start_usec + runtime_usec
    remaining = deadline - now_usec
    required = LIFETIME_REQUIRED_SECONDS[phase]
    profile_matches = runtime_usec == ANCHOR_RUNTIME_SECONDS * 1000000
    start_not_future = 0 < start_usec <= now_usec
    return {'phase': phase, 'anchorStartMonotonicUsec': start_usec, 'observedMonotonicUsec': now_usec,
            'runtimeMicroseconds': runtime_usec, 'absoluteDeadlineMonotonicUsec': deadline,
            'remainingMicroseconds': remaining, 'requiredSeconds': required,
            'runtimeTotalSeconds': RUNTIME_TOTAL_SECONDS, 'sealTotalSeconds': SEAL_TOTAL_SECONDS,
            'handoffSeconds': HANDOFF_SECONDS, 'finalStopSafetySeconds': FINAL_STOP_SAFETY_SECONDS,
            'profileMatches': profile_matches, 'startNotFuture': start_not_future,
            'sufficient': profile_matches and start_not_future and remaining >= required * 1000000,
            'anchorRestartPermitted': False}


def _runtime_microseconds(value):
    # systemctl renders a timespan (normally "1h"), not the property's raw D-Bus integer.
    factors = {'h': 3600000000, 'min': 60000000, 's': 1000000, 'ms': 1000, 'us': 1}
    tokens = value.split() if isinstance(value, str) else []
    if not tokens or len(tokens) > 5:
        raise ValueError('anchor_runtime_timespan')
    total = 0
    for token in tokens:
        match = re.fullmatch(r'([0-9]+)(h|min|s|ms|us)', token)
        if match is None:
            raise ValueError('anchor_runtime_timespan')
        total += int(match.group(1)) * factors[match.group(2)]
    return total


def anchor_lifetime(base, context, phase):
    """Record then admit remaining time; never start or extend infrastructure."""
    base.need(phase in LIFETIME_REQUIRED_SECONDS, 'anchor_lifetime_phase')
    observation = {'phase': phase, 'requiredSeconds': LIFETIME_REQUIRED_SECONDS[phase], 'sufficient': False,
                   'identityMatched': False, 'anchorRestartPermitted': False}
    base.report.setdefault('anchorLifetimeChecks', []).append(observation)
    expected = context['anchor']
    pid = context['anchorPid']
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    observation.update(bootId=boot, anchorPid=pid, expectedInvocationId=expected['unit']['InvocationID'])
    base.need(boot == context['bootId'] and pid == expected['process']['pid'], 'anchor_lifetime_boot_or_pid')
    check_file(base, context['unitFiles'][ANCHOR])
    before = base.process_identity(pid)
    observation['processBefore'] = before
    base.need(before == expected['process'], 'anchor_lifetime_process_changed')
    fields = tuple(base.PROPERTIES) + ('RuntimeMaxUSec', 'Restart', 'NRestarts')
    raw = base.run('anchor-lifetime-' + phase, ['/usr/bin/systemctl', 'show', ANCHOR, '--no-pager',
                   '--property=' + ','.join(fields)], 15)
    pairs = [line.split('=', 1) for line in raw.decode('utf-8', 'strict').splitlines() if '=' in line]
    current = dict(pairs)
    observation['unit'] = current
    base.need(len(pairs) == len(current) and set(current) == set(fields), 'anchor_lifetime_unit_fields')
    start_text = current['ExecMainStartTimestampMonotonic']
    base.need(re.fullmatch('[0-9]+', start_text) is not None, 'anchor_lifetime_start_shape')
    observation.update(lifetime_budget(phase, int(start_text), _runtime_microseconds(current['RuntimeMaxUSec']),
                                       time.monotonic_ns() // 1000))
    after = base.process_identity(pid)
    observation['processAfter'] = after
    base.need(all(current[key] == expected['unit'][key] for key in base.PROPERTIES) and
              current['Id'] == ANCHOR and current['MainPID'] == str(pid) and
              current['ActiveState'] == 'active' and current['SubState'] == 'running' and
              current['Restart'] == 'no' and current['NRestarts'] == '0' and after == before and
              Path('/proc/sys/kernel/random/boot_id').read_text().strip() == boot, 'anchor_lifetime_identity_changed')
    observation['identityMatched'] = True
    base.need(observation['profileMatches'] and observation['startNotFuture'], 'anchor_lifetime_clock_or_limit_changed')
    base.need(observation['sufficient'], 'anchor_lifetime_insufficient')
    return observation


@contextmanager
def app_network(context):
    """Enter the pinned private network only while making owned HTTP/SQL I/O."""
    if not hasattr(os, 'setns') or len(list(Path('/proc/self/task').iterdir())) != 1:
        raise ValueError('single_threaded_setns_required')
    host = os.readlink('/proc/self/ns/net')
    target = '/proc/' + str(context['anchorPid']) + '/ns/net'
    if host != context['hostNetworkNamespace'] or os.readlink('/proc/1/ns/net') != host or os.readlink(target) != context['networkNamespace']:
        raise ValueError('namespace_context_changed')
    host_fd = os.open('/proc/self/ns/net', os.O_RDONLY | os.O_CLOEXEC)
    target_fd = None
    try:
        target_fd = os.open(target, os.O_RDONLY | os.O_CLOEXEC)
        if 'net:[' + str(os.fstat(target_fd).st_ino) + ']' != context['networkNamespace']:
            raise ValueError('opened_namespace_changed')
        os.setns(target_fd, 0)
        if os.readlink('/proc/self/ns/net') != context['networkNamespace']:
            raise ValueError('private_namespace_not_entered')
        try: yield
        finally:
            os.setns(host_fd, 0)
            if os.readlink('/proc/self/ns/net') != host:
                raise ValueError('host_namespace_not_restored')
    finally:
        # Restore even if a check failed between setns and the yield boundary.
        try:
            if os.readlink('/proc/self/ns/net') != host: os.setns(host_fd, 0)
        finally:
            if target_fd is not None: os.close(target_fd)
            os.close(host_fd)
