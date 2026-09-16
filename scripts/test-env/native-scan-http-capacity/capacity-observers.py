"""Six fixed SQL observers and bounded cgroup samples; import performs no work.

API: Observers(base, support, workload_module, context, *, deadline, record=None,
               measure=None).
record is an optional new empty dict retained by reference. The caller owns the
global deployment lock, protected-state checks, APP, PG, anchor, and all closure.
measure optionally wraps measure(category, operation, *args) for private writes;
the callback keeps inclusive elapsed-time counters in caller memory.
No main, thread, process launcher, HTTP exchange or service action exists here.
Only observe calls the caller's frozen base.run, once per consumed SQL slot.

observe(slot, *, application=None, deadline=None) consumes these slots exactly
once and in order: prestart-identity, empty-catalog, cold-catalog,
post-favorite-baseline, cached-catalog, final-after-stop. A failed slot stops
later SQL admission; it cannot be retried under another label. The first slot
requires application=None and an absent/empty fixed APP cgroup. Its statement
reads system catalogs only, because APP has not migrated the database. The four
business slots take the actual Application.check_owned() result, including
unit/ownership/process/descendants/observedMonotonicNs. The final slot takes the
actual closed Application.record, including physicalClosed, closed, cgroupEmpty,
runtimeDirectoryRemoved, process, knownProcesses, and startOwnership; the caller
passes the independent absolute cleanup deadline. Known PID reuse is a closed
old lifetime, never authority over its replacement. Scanner descendants are
allowed and remain the Application module's responsibility.

The default deadline is the absolute business deadline. Any explicit deadline
is bounded by the original anchor start plus the frozen 2,700-second envelope.
Each SQL dispatch reserves 26 seconds before that phase deadline: base.run's
10-second process wait, its existing maximum 11-second failure group cleanup,
and at most five seconds for the observed backend to disappear. Statement and
lock limits remain 10 seconds and one second. This module never extends them.

context is the actual capacity preparation context: bootId, anchorPid, anchor,
postgres (pid/process/unit/systemId), networkNamespace, hostNetworkNamespace,
database/databaseOid, role/roleOid, installed.binary, and the fixed unit names
from support. tools.psql carries the preparation's path/resolvedPath, SHA256,
bytes and metadata descriptor; its fixed binary is reread before and after SQL.
Database and role are goby_native_capacity; PG uses port 25499
and F/pg/data. Full PG/anchor identities are checked before and after SQL.
The private Unix socket is metadata-bound. A new empty root:root 0600 passfile
prevents a permissions warning; no old password, service or home file is read.
The statement uses -X -q -A -t --no-password with read-only defaults, a repeatable
read transaction and UTC. Every observer checks all five explicit numeric OID
projections, including the shared support marker/reader. Initial and final SQL
must find no APP clients. Middle slots bind every APP client port to sockets
held by the actual main process before or after the query.

For snapshots, each public table is selected into a MATERIALIZED CTE with its
fixed maximum plus one before conversion/aggregation. Zero-row tables still
read one sentinel. No full-table count and no count that hides an extra row is
used. Composite row-text sizes are bounded before JSON conversion; all aggregate
row JSON is built only after every count and cumulative row-text byte bound
passes. The final emitted JSON payload is capped at 900 KiB, below base.run's
1 MiB stream cap. Schema versions are independently bounded to 29 and must be
exactly 1..28. Returned snapshot values are precisely workload.TABLES arrays;
schema, raw SQL, original command streams and identity remain in private records.
UserData includes the full row plus rowVersion=xmin::text. Sessions select only
id/user_id/kind/revoked_at/token_hash, with encode(token_hash,'hex').

metrics_due() checks only the monotonic clock and the last sample time. A false
result does not retain or authorize reuse of an application observation.
capture_metrics(app_observation) is opportunistic, single-threaded metadata
reading. It performs no SQL or systemctl call. At most 600 new samples, at least
two seconds apart, and at most 32 MiB of serialized raw sample evidence are
allowed. Each sample reads only the fixed APP/PG cgroups and their bound main
PID stat/status/cgroup data. It records actual timestamps/gaps and unmodified
counter text. Missing metric files are explicitly unavailable; missing/reused
processes have no invented RSS. memory.peak is a cgroup metric, never an RSS
claim. The caller must classify unavailable fields against its admitted host
profile and compute any sampled maxima separately. The Application module
already checks and classifies its ffprobe/FFmpeg descendants.
"""

import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
import time


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
ROOT = E / 'private/observers'
SQL_ROOT = ROOT / 'sql'
METRIC_ROOT = ROOT / 'metrics'
PREFIX = 'goby-native-capacity-20260915'
APP = PREFIX + '-app.service'
PGUNIT = PREFIX + '-postgres.service'
RUNTIME = Path('/run') / PREFIX
PG_SOCKET = F / 'pg/socket/.s.PGSQL.25499'
PSQL = '/usr/lib/postgresql/17/bin/psql'
SLOTS = ('prestart-identity', 'empty-catalog', 'cold-catalog',
         'post-favorite-baseline', 'cached-catalog', 'final-after-stop')
TABLE_LIMITS = {'items': 1076, 'library_roots': 2, 'libraries': 2,
                'user_item_data': 2, 'task_runs': 2, 'task_run_children': 4,
                'scan_jobs': 4, 'task_run_requests': 2, 'task_triggers': 0,
                'task_occurrences': 0, 'sessions': 4, 'play_sessions': 0}
ITEM_COLUMNS = ('id', 'library_id', 'root_id', 'parent_id', 'type', 'path',
                'relative_path', 'name', 'sort_name', 'is_folder',
                'index_number', 'parent_index_number')
PROCESS_FIELDS = ('pid', 'parentPid', 'startTicks', 'uids', 'gids', 'exe',
                  'executableDevice', 'executableInode', 'networkNamespace',
                  'cgroup', 'noNewPrivileges', 'capabilities')
CAPABILITIES = ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb')
METRIC_FILES = ('memory.current', 'memory.peak', 'memory.events', 'cpu.stat')
METRIC_CAP = 32 << 20
SAMPLE_CAP = 48 << 10
PAYLOAD_CAP = 900 << 10
ROW_TEXT_CAP = 800 << 10
COMMAND_CAP = 1 << 20
STABLE = ('device', 'inode', 'uid', 'gid', 'mode', 'type')
SAFE_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8',
            'LC_ALL': 'C.UTF-8', 'TZ': 'UTC'}


class ObserverError(RuntimeError):
    """A fixed safe error code, without raw database or credential text."""


def need(value, code):
    if not value:
        raise ObserverError(code)


def error_code(error):
    value = str(error)
    return value if re.fullmatch('[A-Za-z0-9_.-]{1,160}', value) else type(error).__name__


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False) + '\n').encode()


def metadata(value):
    return {'device': value.st_dev, 'inode': value.st_ino, 'uid': value.st_uid,
            'gid': value.st_gid, 'mode': stat.S_IMODE(value.st_mode),
            'type': stat.S_IFMT(value.st_mode), 'bytes': value.st_size,
            'links': value.st_nlink, 'mtimeNs': value.st_mtime_ns, 'ctimeNs': value.st_ctime_ns}


def kernel_file(path, maximum):
    """Read a bounded virtual regular file; st_size is not its content length."""
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = metadata(os.fstat(fd))
        need(before['type'] == stat.S_IFREG, 'observer_virtual_file_type')
        chunks, count = [], 0
        while True:
            part = os.read(fd, min(4096, maximum + 1 - count))
            if not part:
                break
            chunks.append(part)
            count += len(part)
            need(count <= maximum, 'observer_virtual_file_bound')
        after = metadata(os.fstat(fd))
        need(all(before[key] == after[key] for key in STABLE), 'observer_virtual_file_replaced')
        raw = b''.join(chunks)
        return raw, {'path': str(path), 'metadata': before, 'bytes': len(raw),
                     'sha256': hashlib.sha256(raw).hexdigest()}
    finally:
        os.close(fd)


def process_stat(pid):
    raw, evidence = kernel_file(Path('/proc') / str(pid) / 'stat', 8192)
    text = raw.decode('ascii', 'strict')
    prefix, fields = text.rsplit(') ', 1)
    values = fields.split()
    need(prefix.split(' ', 1)[0] == str(pid) and len(values) >= 22 and
         re.fullmatch('[0-9]+', values[19]), 'observer_process_stat_contract')
    return {'pid': pid, 'parentPid': int(values[1]), 'startTicks': values[19],
            'state': values[0]}, dict(evidence, text=text)


def process_profile(pid):
    first, _ = process_stat(pid)
    need(first['state'] not in ('Z', 'X'), 'observer_process_not_live')
    root = Path('/proc') / str(pid)
    raw, _ = kernel_file(root / 'status', 16384)
    pairs = [line.split(':', 1) for line in raw.decode('ascii', 'strict').splitlines() if ':' in line]
    values = dict(pairs)
    need(len(values) == len(pairs), 'observer_process_status_duplicate')
    executable = (root / 'exe').stat()
    cgroup_raw, _ = kernel_file(root / 'cgroup', 8192)
    result = {key: first[key] for key in ('pid', 'parentPid', 'startTicks')}
    result.update(uids=[int(value) for value in values['Uid'].split()],
                  gids=[int(value) for value in values['Gid'].split()],
                  exe=os.readlink(root / 'exe'), executableDevice=executable.st_dev,
                  executableInode=executable.st_ino, networkNamespace=os.readlink(root / 'ns/net'),
                  cgroup=cgroup_raw.decode('ascii', 'strict').strip(),
                  noNewPrivileges=int(values['NoNewPrivs']),
                  capabilities={key: values[key].strip() for key in CAPABILITIES})
    last, _ = process_stat(pid)
    after = (root / 'exe').stat()
    need(last['state'] not in ('Z', 'X') and
         all(first[key] == last[key] for key in ('pid', 'parentPid', 'startTicks')) and
         os.readlink(root / 'exe') == result['exe'] and
         (after.st_dev, after.st_ino) == (executable.st_dev, executable.st_ino),
         'observer_process_changed_during_read')
    return result


def main_database_ports(process):
    """Bind PostgreSQL client ports only to descriptors held by this main PID."""
    pid = process['pid']
    root = Path('/proc') / str(pid)
    inodes = set()
    with os.scandir(root / 'fd') as entries:
        for index, entry in enumerate(entries):
            need(index < 1024, 'observer_application_fd_bound')
            try:
                match = re.fullmatch(r'socket:\[([0-9]+)\]', os.readlink(entry.path))
                if match:
                    inodes.add(match.group(1))
            except (FileNotFoundError, ProcessLookupError):
                continue
    raw, pin = kernel_file(root / 'net/tcp', 512 << 10)
    ports, selected = set(), []
    for line in raw.decode('ascii', 'strict').splitlines()[1:]:
        fields = line.split()
        need(len(fields) >= 10, 'observer_tcp_row_contract')
        if fields[9] not in inodes or fields[3] != '01' or fields[2] != '0100007F:%04X' % 25499:
            continue
        need(fields[1].startswith('0100007F:'), 'observer_pg_client_not_loopback')
        port = int(fields[1].split(':')[1], 16)
        need(1024 <= port <= 65535, 'observer_pg_client_port')
        ports.add(port)
        selected.append({'inode': fields[9], 'local': fields[1], 'remote': fields[2], 'state': fields[3]})
    return ports, {'processId': pid, 'startTicks': process['startTicks'],
                   'ports': sorted(ports), 'selectedSockets': selected, 'namespaceTcp': pin}


def marker_sql():
    return ("jsonb_build_object('kind','observer','pid',pg_backend_pid(),"
            "'database',current_database(),'databaseOid',(SELECT oid::bigint FROM pg_database "
            "WHERE datname=current_database()),'user',current_user,'applicationName',"
            "current_setting('application_name'),'readOnly',current_setting('transaction_read_only'),"
            "'isolation',current_setting('transaction_isolation'),'systemId',(SELECT system_identifier::text "
            "FROM pg_control_system()),'dataDirectory',current_setting('data_directory'),"
            "'port',current_setting('port'))")


def snapshot_ctes(workload_module):
    """Build fixed bounded SQL fragments, without opening a connection."""
    contract = workload_module.sql_contract()
    need(set(contract) == set(TABLE_LIMITS) and set(workload_module.TABLES) == set(TABLE_LIMITS) and
         tuple(workload_module.ITEM_COLUMNS) == ITEM_COLUMNS and
         all(type(contract[name]['maximum']) is int and contract[name]['maximum'] == maximum
             for name, maximum in TABLE_LIMITS.items()),
         'observer_workload_sql_contract_changed')
    ctes, summaries, tables = [], [], []
    for name, maximum in TABLE_LIMITS.items():
        if name == 'items':
            projection = ','.join(("COALESCE(r." + key + ",'') AS " + key) if key in
                                  ('root_id', 'parent_id') else 'r.' + key for key in ITEM_COLUMNS)
        elif name == 'sessions':
            projection = "r.id,r.user_id,r.kind,r.revoked_at,encode(r.token_hash,'hex') AS token_hash"
        elif name == 'user_item_data':
            projection = 'r.*,r.xmin::text AS "rowVersion"'
        else:
            projection = 'r.*'
        raw_name = 'raw_' + name
        ctes.append(raw_name + ' AS MATERIALIZED (SELECT ' + projection + ' FROM public.' + name +
                    ' r LIMIT ' + str(maximum + 1) + ')')
        summaries.append("SELECT '" + name + "'::text AS name," + str(maximum) + '::bigint AS maximum,' +
                         'count(*)::bigint AS seen,CASE WHEN count(*)<=' + str(maximum) +
                         ' THEN (SELECT COALESCE(sum(octet_length(q::text)),0)::bigint FROM ' + raw_name +
                         ' q) ELSE NULL::bigint END AS row_text_bytes FROM ' + raw_name)
        tables.append("'" + name + "',COALESCE((SELECT jsonb_agg(to_jsonb(q) ORDER BY to_jsonb(q)::text) FROM " +
                      raw_name + " q),'[]'::jsonb)")
    ctes.append('table_bounds AS MATERIALIZED (' + ' UNION ALL '.join(summaries) + ')')
    ctes.append('raw_schema AS MATERIALIZED (SELECT version FROM public.schema_migrations LIMIT 29)')
    ctes.append("raw_server_id AS MATERIALIZED (SELECT value FROM public.server_settings WHERE key='server_id' LIMIT 2)")
    bounds = "(SELECT jsonb_agg(jsonb_build_object('table',name,'maximum',maximum,'seen',seen,'rowTextBytes',row_text_bytes) ORDER BY name) FROM table_bounds)"
    valid = ('(SELECT bool_and(seen<=maximum) AND COALESCE(sum(row_text_bytes),0)<=' + str(ROW_TEXT_CAP) + ' FROM table_bounds)' +
             ' AND (SELECT count(*)=28 FROM raw_schema)' +
             ' AND (SELECT count(*)=1 AND COALESCE(max(octet_length(value::text)),0)<=256 FROM raw_server_id)')
    fields = ("'schema',(SELECT jsonb_build_object('versions',jsonb_agg(version ORDER BY version),'count',count(*)) FROM raw_schema)," +
              "'serverId',(SELECT value FROM raw_server_id),'tables',jsonb_build_object(" + ','.join(tables) + "),'bounds'," + bounds)
    return ctes, valid, fields, bounds


def build_statement(slot, support, workload_module):
    need(slot in SLOTS, 'observer_sql_slot_name')
    clients = ("raw_clients AS MATERIALIZED (SELECT pid,datid::bigint AS database_oid,datname,"
               "usesysid::bigint AS role_oid,usename,application_name,host(client_addr) AS client_address,"
               "client_port FROM pg_stat_activity WHERE backend_type='client backend' LIMIT 66)")
    client_json = ("COALESCE((SELECT jsonb_agg(jsonb_build_object('pid',pid,'databaseOid',database_oid,"
                   "'database',datname,'roleOid',role_oid,'user',usename,'applicationName',application_name,"
                   "'clientAddress',client_address,'clientPort',client_port) ORDER BY pid) FROM raw_clients),'[]'::jsonb)")
    ctes = [clients]
    common = ("'kind','capacity-observation','slot','" + slot + "','clients'," + client_json +
              ",'sharedIdentity'," + support.final_observer_marker_sql())
    if slot == 'prestart-identity':
        ctes.append("raw_public AS MATERIALIZED (SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                    "WHERE n.nspname='public' AND c.relkind IN ('r','p') LIMIT 1)")
        payload = 'jsonb_build_object(' + common + ",'publicTablesSeen',(SELECT count(*) FROM raw_public))"
    else:
        added, valid, fields, bounds = snapshot_ctes(workload_module)
        ctes.extend(added)
        payload = ('CASE WHEN ' + valid + " THEN jsonb_build_object(" + common + ',' + fields + ')' +
                   " ELSE jsonb_build_object('kind','capacity-observation-rejected','slot','" + slot +
                   "','reason','snapshot-bounds','bounds'," + bounds + ') END')
    ctes.append('payload AS MATERIALIZED (SELECT ' + payload + ' AS value)')
    select = ("SELECT CASE WHEN octet_length(value::text)<=" + str(PAYLOAD_CAP) +
              " THEN value ELSE jsonb_build_object('kind','capacity-observation-rejected','slot','" + slot +
              "','reason','payload-byte-limit','bytes',octet_length(value::text)) END FROM payload;")
    return ("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;\n"
            "SET LOCAL statement_timeout='10s';\nSET LOCAL lock_timeout='1s';\nSET LOCAL TIME ZONE 'UTC';\n"
            'SELECT ' + marker_sql() + ';\nWITH ' + ',\n'.join(ctes) + '\n' + select + '\nCOMMIT;\n')


class Observers:
    def __init__(self, base, support, workload_module, context, *, deadline, record=None, measure=None):
        need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
             sys.flags.isolated and sys.flags.dont_write_bytecode, 'observer_remote_root_isolated_python_required')
        need(record is None or (type(record) is dict and not record), 'observer_record_not_new')
        need(measure is None or callable(measure), 'observer_cost_callback')
        self._measure = measure
        need(support.E == E and support.F == F and support.APP == APP and support.PGUNIT == PGUNIT and
             support.RUNTIME == RUNTIME and context['database'] == context['role'] == 'goby_native_capacity' and
             type(context['databaseOid']) is int and context['databaseOid'] > 0 and
             type(context['roleOid']) is int and context['roleOid'] > 0, 'observer_context_scope')
        self.base, self.support, self.workload, self.ctx = base, support, workload_module, context
        start = context['anchor']['unit']['ExecMainStartTimestampMonotonic']
        need(isinstance(start, str) and re.fullmatch('[1-9][0-9]*', start), 'observer_anchor_start_contract')
        self.anchor_deadline = int(start) / 1000000 + 2700.0
        self.deadline = self._phase_deadline(deadline)
        self.record = {} if record is None else record
        self.record.update(kind='native-scan-http-capacity-observers', version=1, scope=str(E),
                           sql=[], samples=[], failed=False, errors=[], metricBytes=0,
                           metricRequestCount=0, newServiceActions=0, periodicSqlConnections=0,
                           sqlSlotOrder=list(SLOTS), commandWaitSeconds=10,
                           inheritedCommandCleanupMaximumSeconds=11, backendWaitMaximumSeconds=5)
        tool = context['tools']['psql']
        need(Path(tool['path']).resolve() == Path(tool['resolvedPath']) == Path(PSQL),
             'observer_psql_tool_path')
        self._psql_pin = {key: tool[key] for key in ('sha256', 'bytes', 'metadata')}
        self._psql_pin['path'] = tool['resolvedPath']
        support.check_file(base, self._psql_pin)
        self.record['psqlTool'] = self._psql_pin
        self._directories = {}
        for path in (ROOT, SQL_ROOT, METRIC_ROOT, ROOT / 'libpq-home'):
            need(path.parent.resolve() == path.parent and not os.path.lexists(path), 'observer_directory_not_fresh')
            os.mkdir(path, 0o700)
            os.chmod(path, 0o700)
            self._directories[str(path)] = base.metadata(path)
        self._passfile = self._save(ROOT / 'libpq-pass', b'')
        self._servicefile = self._save(ROOT / 'libpq-service', b'')
        self._pg_socket = base.metadata(PG_SOCKET)
        need(self._pg_socket['type'] == stat.S_IFSOCK and self._pg_socket['uid'] == 103 and
             self._pg_socket['gid'] == 106 and self._pg_socket['mode'] == 0o700,
             'observer_pg_socket_contract')
        self._last_sample_ns = None
        self._cgroup_meta = {}
        mount_raw, mount_pin = kernel_file(Path('/proc/self/mountinfo'), 1 << 20)
        matches = [line for line in mount_raw.decode('ascii', 'strict').splitlines()
                   if line.split()[4] == '/sys/fs/cgroup']
        need(len(matches) == 1 and matches[0].split(' - ', 1)[1].split()[0] == 'cgroup2',
             'observer_cgroup2_required')
        self.record['cgroupMount'] = {'mountLine': matches[0], 'source': mount_pin}
        self.record['libpqFiles'] = {'passfile': self._passfile, 'servicefile': self._servicefile}

    def _phase_deadline(self, value):
        need(type(value) in (int, float) and math.isfinite(value) and
             time.monotonic() < value <= self.anchor_deadline, 'observer_absolute_phase_deadline')
        return float(value)

    def _save(self, path, value):
        if self._measure is not None:
            return self._measure('evidenceWrite', self._write, path, value)
        return self._write(path, value)

    def _write(self, path, value):
        raw = value if isinstance(value, bytes) else encoded(value)
        before = self.base.metadata(path.parent)
        need(before['type'] == stat.S_IFDIR and before['uid'] == before['gid'] == 0 and
             before['mode'] == 0o700 and path.parent.resolve() == path.parent,
             'observer_private_destination')
        pin = self.base.write_new(path, raw)
        pin['bytes'] = len(raw)
        need(pin['metadata']['mode'] == 0o600 and pin['metadata']['uid'] == pin['metadata']['gid'] == 0 and
             pin['metadata']['type'] == stat.S_IFREG and pin['metadata']['links'] == 1,
             'observer_private_file_profile')
        return pin

    def _failure(self, entry, stage, error):
        row = {'stage': stage, 'code': error_code(error), 'type': type(error).__name__}
        entry.setdefault('errors', []).append(row)
        self.record['errors'].append(dict(row, slot=entry.get('slot')))
        self.record['failed'] = True

    def _check_private(self):
        self.support.check_file(self.base, self._psql_pin)
        for path, expected in self._directories.items():
            current = self.base.metadata(Path(path))
            need(all(current[key] == expected[key] for key in STABLE), 'observer_directory_replaced')
        for pin in (self._passfile, self._servicefile):
            need(self.base.metadata(Path(pin['path'])) == pin['metadata'], 'observer_libpq_file_changed')
        need(self.base.metadata(PG_SOCKET) == self._pg_socket, 'observer_pg_socket_changed')

    def _app_empty(self):
        group = Path('/sys/fs/cgroup/system.slice') / APP
        if not os.path.lexists(group):
            return {'path': str(group), 'state': 'absent', 'members': []}
        need(group.resolve() == group and stat.S_ISDIR(group.lstat().st_mode), 'observer_app_cgroup_path')
        raw, pin = kernel_file(group / 'cgroup.procs', 8192)
        need(not raw.split(), 'observer_app_cgroup_not_empty')
        events_raw, events_pin = kernel_file(group / 'cgroup.events', 4096)
        events = dict(line.split() for line in events_raw.decode('ascii', 'strict').splitlines())
        need(events.get('populated') == '0', 'observer_app_cgroup_descendants_populated')
        return {'path': str(group), 'state': 'present-empty', 'members': [],
                'cgroupProcs': pin, 'cgroupEvents': dict(events_pin, text=events_raw.decode('ascii', 'strict'))}

    def _closed_app(self, application):
        need(isinstance(application, dict) and application.get('unit') == APP and
             all(application.get(key) is True for key in
                 ('closed', 'physicalClosed', 'cgroupEmpty', 'runtimeDirectoryRemoved')) and
             isinstance(application.get('knownProcesses'), dict), 'observer_final_app_closure_contract')
        group = self._app_empty()
        need(not os.path.lexists(RUNTIME), 'observer_final_runtime_directory_present')
        known = list(application['knownProcesses'].values())
        for key in ('process', 'startOwnership', 'cleanupOwnership'):
            if isinstance(application.get(key), dict):
                known.append(application[key])
        need(0 < len(known) <= 4100, 'observer_final_known_lifetime_inventory')
        gone = []
        for process in known:
            pid, ticks = process.get('pid'), process.get('startTicks')
            need(type(pid) is int and pid > 1 and isinstance(ticks, str) and re.fullmatch('[1-9][0-9]*', ticks),
                 'observer_final_known_lifetime_contract')
            try:
                actual, _ = process_stat(pid)
                need(actual['startTicks'] != ticks, 'observer_final_owned_lifetime_present')
                gone.append({'pid': pid, 'startTicks': ticks, 'reason': 'pid-reused',
                             'observedStartTicks': actual['startTicks']})
            except (FileNotFoundError, ProcessLookupError):
                gone.append({'pid': pid, 'startTicks': ticks, 'reason': 'pid-absent'})
        return {'cgroup': group, 'knownLifetimesGone': gone, 'runtimeDirectoryAbsent': True}

    def _live_app(self, application, *, fresh):
        need(isinstance(application, dict) and isinstance(application.get('process'), dict) and
             isinstance(application.get('ownership'), dict) and isinstance(application.get('unit'), dict),
             'observer_application_observation_contract')
        process = application['process']
        expected_group = '0::/system.slice/' + APP
        need(application['unit'].get('Id') == APP and application['unit'].get('ActiveState') == 'active' and
             application['unit'].get('SubState') == 'running' and application['unit'].get('NRestarts') == '0' and
             process.get('cgroup') == expected_group and process.get('uids') == [995] * 4 and
             process.get('gids') == [986] * 4 and process.get('networkNamespace') == self.ctx['networkNamespace'] and
             process.get('pid') == application['ownership'].get('pid') and
             process.get('startTicks') == application['ownership'].get('startTicks'),
             'observer_application_observation_scope')
        if fresh:
            observed = application.get('observedMonotonicNs')
            need(type(observed) is int and 0 <= time.monotonic_ns() - observed <= 5_000_000_000,
                 'observer_application_observation_stale')
        current = process_profile(process['pid'])
        need(current == {key: process[key] for key in PROCESS_FIELDS}, 'observer_application_process_changed')
        ports, sockets = main_database_ports(current)
        need(process_profile(process['pid']) == current, 'observer_application_changed_after_sockets')
        return ports, {'process': current, 'sockets': sockets}

    def _marker(self, value, tag):
        need(isinstance(value, dict) and value.get('kind') == 'observer' and
             type(value.get('pid')) is int and value['pid'] > 1 and
             type(value.get('databaseOid')) is int and value['databaseOid'] == self.ctx['databaseOid'] and
             value.get('database') == self.ctx['database'] and value.get('user') == 'postgres' and
             value.get('applicationName') == tag and value.get('readOnly') == 'on' and
             value.get('isolation') == 'repeatable read' and value.get('systemId') == self.ctx['postgres']['systemId'] and
             value.get('dataDirectory') == str(F / 'pg/data') and value.get('port') == '25499',
             'observer_database_marker_contract')

    def _backend_close(self, entry, deadline):
        pid = entry.get('backendPid')
        if type(pid) is not int or pid <= 1:
            return
        first = None
        until = min(deadline, time.monotonic() + 5.0)
        while True:
            try:
                current, _ = process_stat(pid)
            except (FileNotFoundError, ProcessLookupError):
                entry['backendGone'] = True
                entry['backendClosureReason'] = 'pid-absent'
                return
            if first is None:
                first = current
                entry['backendObservedLifetime'] = current
            elif current['startTicks'] != first['startTicks']:
                entry['backendGone'] = True
                entry['backendClosureReason'] = 'observed-lifetime-replaced'
                entry['backendReplacementStartTicks'] = current['startTicks']
                return
            if time.monotonic() >= until:
                return
            time.sleep(min(0.05, max(0, until - time.monotonic())))

    def _read_command_stream(self, entry, stream, name):
        path = Path(stream['path'])
        current = self.base.metadata(path)
        need(current['type'] == stat.S_IFREG and current['uid'] == current['gid'] == 0 and
             current['mode'] == 0o600 and current['links'] == 1 and
             path.is_relative_to(E / 'private') and path.resolve() == path,
             'observer_command_stream_profile')
        entry[name + 'Metadata'] = {'path': str(path), 'metadata': current}
        if current['bytes'] <= COMMAND_CAP:
            raw = self.base.read_regular(path, COMMAND_CAP)
            need(self.base.metadata(path) == current, 'observer_command_stream_changed')
            entry[name] = {'path': str(path), 'bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(),
                           'metadata': current}
            return raw
        # Keep a bounded first marker even if a command overflowed its expected
        # stream limit. The unchanged oversized original is retained by base.
        if name == 'stdout':
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                prefix = os.read(fd, 16384)
            finally:
                os.close(fd)
            need(self.base.metadata(path) == current, 'observer_oversized_stream_changed')
            first = prefix.split(b'\n', 1)[0]
            if b'\n' in prefix:
                entry['markerPrefix'] = self._save(SQL_ROOT / (entry['slot'] + '-marker-prefix.json'), first + b'\n')
                return first + b'\n'
        return b''

    def _payload(self, slot, raw, marker, tag, before_ports, after_ports):
        value = self.support.parse(raw)
        need(isinstance(value, dict) and value.get('kind') == 'capacity-observation' and
             value.get('slot') == slot, 'observer_payload_rejected_or_invalid')
        self.support.check_final_observer_identity(self.base, self.ctx, value.get('sharedIdentity'))
        need(value['sharedIdentity']['pid'] == marker['pid'], 'observer_shared_marker_pid')
        clients = value.get('clients')
        need(isinstance(clients, list) and 1 <= len(clients) <= 65 and
             all(isinstance(row, dict) and type(row.get('pid')) is int and row['pid'] > 1 for row in clients) and
             len({row['pid'] for row in clients}) == len(clients), 'observer_client_inventory')
        own = [row for row in clients if row['pid'] == marker['pid']]
        need(len(own) == 1 and own[0].get('user') == 'postgres' and own[0].get('applicationName') == tag and
             own[0].get('clientAddress') is None and own[0].get('database') == self.ctx['database'] and
             type(own[0].get('databaseOid')) is int and own[0]['databaseOid'] == self.ctx['databaseOid'] and
             type(own[0].get('roleOid')) is int and own[0]['roleOid'] > 0, 'observer_own_client_contract')
        app_clients = [row for row in clients if row['pid'] != marker['pid']]
        expect_app = slot not in ('prestart-identity', 'final-after-stop')
        need(expect_app or not app_clients, 'observer_app_clients_outside_lifetime')
        for row in app_clients:
            need(type(row.get('databaseOid')) is int and row['databaseOid'] == self.ctx['databaseOid'] and
                 row.get('database') == self.ctx['database'] and type(row.get('roleOid')) is int and
                 row['roleOid'] == self.ctx['roleOid'] and row.get('user') == self.ctx['role'] and
                 row.get('applicationName') == 'goby' and row.get('clientAddress') == '127.0.0.1' and
                 type(row.get('clientPort')) is int and row['clientPort'] in before_ports | after_ports,
                 'observer_unbound_postgres_client')
        if slot == 'prestart-identity':
            need(type(value.get('publicTablesSeen')) is int and value['publicTablesSeen'] == 0,
                 'observer_prestart_database_not_empty')
            return value, {'prestartOidContract': True, 'applicationBackendCount': 0}
        schema = value.get('schema')
        need(isinstance(schema, dict) and type(schema.get('count')) is int and
             isinstance(schema.get('versions'), list) and
             all(type(version) is int for version in schema['versions']) and
             schema == {'count': 28, 'versions': list(range(1, 29))} and
             isinstance(value.get('serverId'), str) and re.fullmatch('[0-9a-f]{32}', value['serverId']) and
             isinstance(value.get('tables'), dict) and set(value['tables']) == set(TABLE_LIMITS),
             'observer_schema_or_snapshot_contract')
        bounds = value.get('bounds')
        need(isinstance(bounds, list) and len(bounds) == len(TABLE_LIMITS) and
             {row.get('table') for row in bounds} == set(TABLE_LIMITS), 'observer_snapshot_bounds_inventory')
        total = 0
        for row in bounds:
            table = row['table']
            need(type(row.get('maximum')) is int and row['maximum'] == TABLE_LIMITS[table] and
                 type(row.get('seen')) is int and 0 <= row['seen'] <= TABLE_LIMITS[table] and
                 type(row.get('rowTextBytes')) is int and row['rowTextBytes'] >= 0 and
                 isinstance(value['tables'][table], list) and len(value['tables'][table]) == row['seen'] and
                 all(isinstance(item, dict) for item in value['tables'][table]), 'observer_snapshot_table_bound')
            total += row['rowTextBytes']
        need(total <= ROW_TEXT_CAP, 'observer_snapshot_row_text_bytes')
        return value, {'prestartOidContract': False, 'applicationBackendCount': len(app_clients),
                       'schema': value['schema'], 'serverId': value['serverId'], 'bounds': bounds}

    def observe(self, slot, *, application=None, deadline=None):
        need(not self.record['failed'] and len(self.record['sql']) < len(SLOTS) and
             slot == SLOTS[len(self.record['sql'])], 'observer_slot_order_or_already_consumed')
        phase_deadline = min(self.deadline, self.anchor_deadline)
        entry = {'slot': slot, 'attempted': True, 'frontendCreated': False, 'frontendClosed': False,
                 'backendPid': None, 'backendGone': False, 'markerIdentityAccepted': False,
                 'payloadAccepted': False, 'status': 'attempted', 'errors': [],
                 'intentMonotonicNs': time.monotonic_ns(), 'phaseDeadline': None}
        self.record['sql'].append(entry)
        failure, raw, command, marker = None, None, None, None
        before_ports, after_ports = set(), set()
        tag = 'goby-capacity-' + slot
        command_label = 'capacity-sql-' + slot
        command_start = len(self.base.report['commands'])
        old_env = self.base.ENV
        try:
            phase_deadline = self._phase_deadline(self.deadline if deadline is None else deadline)
            entry['phaseDeadline'] = phase_deadline
            need(phase_deadline - time.monotonic() >= 26.0, 'observer_sql_and_closeout_reservation')
            self._check_private()
            entry['infrastructureBefore'] = self.support.check_infrastructure(self.base, self.ctx)
            if slot == 'prestart-identity':
                need(application is None, 'observer_prestart_application_argument')
                entry['applicationBefore'] = self._app_empty()
            elif slot == 'final-after-stop':
                entry['applicationBefore'] = self._closed_app(application)
            else:
                before_ports, entry['applicationBefore'] = self._live_app(application, fresh=True)
            statement = build_statement(slot, self.support, self.workload).encode()
            entry['statement'] = self._save(SQL_ROOT / (slot + '.sql'), statement)
            entry['statementLimits'] = {'statementSeconds': 10, 'lockSeconds': 1,
                                        'commandWaitSeconds': 10, 'payloadBytes': PAYLOAD_CAP,
                                        'rowTextBytes': ROW_TEXT_CAP}
            self.base.ENV = {**SAFE_ENV, 'HOME': str(ROOT / 'libpq-home'), 'PGAPPNAME': tag,
                             'PGTZ': 'UTC', 'PGPASSFILE': self._passfile['path'],
                             'PGSERVICEFILE': self._servicefile['path'], 'PGCONNECT_TIMEOUT': '5',
                             'PGOPTIONS': '-c default_transaction_read_only=on -c statement_timeout=10000 -c lock_timeout=1000'}
            with self.support.app_network(self.ctx):
                wait_timeout = min(10.0, phase_deadline - time.monotonic() - 16.0)
                need(wait_timeout > 0, 'observer_command_closeout_reservation_lost')
                raw = self.base.run(command_label, [PSQL, '-X', '--no-password', '-h', str(F / 'pg/socket'),
                                    '-p', '25499', '-U', 'postgres', '-d', 'goby_native_capacity',
                                    '-v', 'ON_ERROR_STOP=1', '-A', '-t', '-q', '-f', entry['statement']['path']],
                                    wait_timeout)
        except BaseException as error:
            failure = error
            self._failure(entry, 'dispatch-or-query', error)
        finally:
            self.base.ENV = old_env
            matches = [(index, value) for index, value in enumerate(self.base.report['commands'][command_start:], command_start)
                       if value.get('label') == command_label]
            if len(matches) == 1:
                index, command = matches[0]
                entry['commandIndex'] = index
                entry['frontendCreated'] = type(command.get('pid')) is int and command['pid'] > 1
                entry['frontendClosed'] = command.get('closed') is True
                entry['frontendPid'] = command.get('pid')
                entry['commandExitCode'] = command.get('exitCode')
                for name in ('stdout', 'stderr'):
                    if isinstance(command.get(name), dict):
                        try:
                            body = self._read_command_stream(entry, command[name], name)
                            if name == 'stdout':
                                raw = body
                        except BaseException as error:
                            self._failure(entry, name + '-readback', error)
                            if failure is None:
                                failure = error
            elif matches:
                error = ObserverError('observer_duplicate_command_dispatch')
                self._failure(entry, 'command-binding', error)
                if failure is None:
                    failure = error
            try:
                lines = raw.splitlines() if isinstance(raw, bytes) else []
                marker = self.support.parse(lines[0]) if lines else None
                # Capture a syntactically valid reported PID before any other
                # marker field or table validation can fail.
                if isinstance(marker, dict) and type(marker.get('pid')) is int and marker['pid'] > 1:
                    entry['backendPid'] = marker['pid']
                    entry['reportedMarker'] = marker
                self._marker(marker, tag)
                entry['markerIdentityAccepted'] = True
            except BaseException as error:
                self._failure(entry, 'marker-reader', error)
                if failure is None:
                    failure = error
            try:
                self._backend_close(entry, phase_deadline)
                need(entry['frontendCreated'] and entry['frontendClosed'] and entry['backendGone'],
                     'observer_frontend_or_backend_not_closed')
            except BaseException as error:
                self._failure(entry, 'connection-closeout', error)
                if failure is None:
                    failure = error
            try:
                entry['infrastructureAfter'] = self.support.check_infrastructure(self.base, self.ctx)
                self._check_private()
                if slot == 'prestart-identity':
                    entry['applicationAfter'] = self._app_empty()
                elif slot == 'final-after-stop':
                    entry['applicationAfter'] = self._closed_app(application)
                else:
                    after_ports, entry['applicationAfter'] = self._live_app(application, fresh=False)
            except BaseException as error:
                self._failure(entry, 'boundary-after', error)
                if failure is None:
                    failure = error
        value = None
        if failure is None:
            try:
                lines = raw.splitlines()
                need(command is not None and command.get('exitCode') == 0 and
                     command.get('neededGroupClosure') is not True and
                     entry.get('stderr', {}).get('bytes') == 0 and len(lines) == 2 and
                     len(lines[1]) <= PAYLOAD_CAP and 'markerPrefix' not in entry,
                     'observer_command_or_response_incomplete')
                value, summary = self._payload(slot, lines[1], marker, tag, before_ports, after_ports)
                entry.update(summary)
                entry['parsed'] = self._save(SQL_ROOT / (slot + '-parsed.json'), value)
                entry['payloadAccepted'], entry['status'] = True, 'passed'
            except BaseException as error:
                self._failure(entry, 'payload-reader', error)
                failure = error
        entry['finishedMonotonicNs'] = time.monotonic_ns()
        if failure is not None:
            entry['status'] = 'failed-retained'
        try:
            entry['receipt'] = self._save(SQL_ROOT / (slot + '-receipt.json'), dict(entry))
        except BaseException as error:
            self._failure(entry, 'receipt-persistence', error)
            entry['status'] = 'failed-retained'
            if failure is None:
                failure = error
        if failure is not None:
            raise ObserverError(error_code(failure)) from None
        return value if slot == 'prestart-identity' else value['tables']

    def _metric_group(self, name):
        unit = APP if name == 'application' else PGUNIT
        group = Path('/sys/fs/cgroup/system.slice') / unit
        try:
            current = metadata(group.lstat())
        except (FileNotFoundError, ProcessLookupError):
            return {'path': str(group), 'status': 'unavailable', 'reason': 'cgroup-absent', 'files': {}}
        need(group.resolve() == group and current['type'] == stat.S_IFDIR and current['uid'] == current['gid'] == 0,
             'observer_metric_cgroup_profile')
        if name in self._cgroup_meta:
            need(all(current[key] == self._cgroup_meta[name][key] for key in STABLE),
                 'observer_metric_cgroup_replaced')
        else:
            self._cgroup_meta[name] = current
        result = {'path': str(group), 'metadata': current, 'files': {}, 'status': 'available'}
        for filename in METRIC_FILES:
            path = group / filename
            try:
                raw, pin = kernel_file(path, 4096)
                text = raw.decode('ascii', 'strict')
                if filename in ('memory.current', 'memory.peak'):
                    need(re.fullmatch('[0-9]+\n?', text), 'observer_memory_counter_contract')
                    value = int(text.strip())
                else:
                    pairs = [line.split() for line in text.splitlines()]
                    need(1 <= len(pairs) <= 64 and all(len(row) == 2 and re.fullmatch('[a-z][a-z0-9_.]*', row[0]) and
                         re.fullmatch('[0-9]+', row[1]) for row in pairs), 'observer_cgroup_counter_contract')
                    value = {key: int(number) for key, number in pairs}
                    need(len(value) == len(pairs), 'observer_cgroup_duplicate_counter')
                result['files'][filename] = {'status': 'available', 'value': value, 'raw': text, 'source': pin}
                required = {'memory.events': {'oom', 'oom_kill'},
                            'cpu.stat': {'usage_usec', 'user_usec', 'system_usec'}}.get(filename, set())
                missing = sorted(required - set(value)) if isinstance(value, dict) else []
                if missing:
                    result['files'][filename]['status'] = 'partial'
                    result['files'][filename]['unavailableFields'] = missing
                    result['status'] = 'partial'
            except (FileNotFoundError, ProcessLookupError):
                result['files'][filename] = {'status': 'unavailable', 'reason': 'metric-file-absent'}
                result['status'] = 'partial'
        return result

    def _rss(self, expected, uid, group):
        need(isinstance(expected, dict) and type(expected.get('pid')) is int and expected['pid'] > 1 and
             isinstance(expected.get('startTicks'), str) and re.fullmatch('[1-9][0-9]*', expected['startTicks']),
             'observer_rss_identity_contract')
        pid, ticks = expected['pid'], expected['startTicks']
        result = {'pid': pid, 'startTicks': ticks, 'expectedUid': uid, 'cgroup': group}
        try:
            before, stat_pin = process_stat(pid)
            if before['startTicks'] != ticks or before['state'] in ('Z', 'X'):
                return dict(result, status='not-sampled', reason='owned-lifetime-no-longer-live',
                            observedStartTicks=before['startTicks'], observedState=before['state'])
            root = Path('/proc') / str(pid)
            raw, pin = kernel_file(root / 'status', 16384)
            pairs = [line.split(':', 1) for line in raw.decode('ascii', 'strict').splitlines() if ':' in line]
            values = dict(pairs)
            cgroup_raw, cgroup_pin = kernel_file(root / 'cgroup', 8192)
            executable = os.readlink(root / 'exe')
            namespace = os.readlink(root / 'ns/net')
            after, _ = process_stat(pid)
            if after['startTicks'] != ticks or after['state'] in ('Z', 'X'):
                return dict(result, status='not-sampled', reason='owned-lifetime-disappeared-during-read',
                            observedStartTicks=after['startTicks'], observedState=after['state'])
            need(len(values) == len(pairs) and values.get('Uid', '').split() == [str(uid)] * 4,
                 'observer_rss_user_changed')
            need(cgroup_raw.decode('ascii', 'strict').strip() == group and
                 executable == expected.get('exe') and namespace == expected.get('networkNamespace'),
                 'observer_rss_cgroup_or_profile_changed')
            need(before['parentPid'] == after['parentPid'], 'observer_rss_parent_changed')
            text = values.get('VmRSS')
            if text is None:
                return dict(result, status='not-sampled', reason='VmRSS-unavailable',
                            stat=stat_pin, statusFile=dict(pin, text=raw.decode('ascii', 'strict')))
            match = re.fullmatch(r'\s*([0-9]+)\s+kB\s*', text)
            need(match is not None, 'observer_rss_value_contract')
            return dict(result, status='sampled', rssBytes=int(match.group(1)) * 1024,
                        stat=stat_pin, statusFile=dict(pin, text=raw.decode('ascii', 'strict')),
                        cgroupFile=dict(cgroup_pin, text=cgroup_raw.decode('ascii', 'strict')))
        except (FileNotFoundError, ProcessLookupError):
            return dict(result, status='not-sampled', reason='owned-process-disappeared')

    def metrics_due(self):
        return self._last_sample_ns is None or time.monotonic_ns() - self._last_sample_ns >= 2_000_000_000

    def capture_metrics(self, app_observation):
        now = time.monotonic_ns()
        if self._last_sample_ns is not None and now - self._last_sample_ns < 2_000_000_000:
            return {'status': 'not-due', 'nextDueMonotonicNs': self._last_sample_ns + 2_000_000_000}
        need(time.monotonic() < self.deadline and len(self.record['samples']) < 600,
             'observer_metric_time_or_sample_bound')
        need(isinstance(app_observation, dict) and isinstance(app_observation.get('process'), dict) and
             isinstance(app_observation.get('ownership'), dict) and isinstance(app_observation.get('unit'), dict),
             'observer_metric_application_observation')
        app = app_observation['process']
        supplied_time = app_observation.get('observedMonotonicNs')
        need(type(supplied_time) is int and 0 <= now - supplied_time <= 5_000_000_000 and
             app_observation['unit'].get('Id') == APP and
             app_observation['ownership'].get('bootId') == self.ctx['bootId'] and
             app.get('pid') == app_observation['ownership'].get('pid') and
             app.get('startTicks') == app_observation['ownership'].get('startTicks') and
             app.get('uids') == [995] * 4 and app.get('gids') == [986] * 4 and
             app.get('cgroup') == '0::/system.slice/' + APP and
             app.get('networkNamespace') == self.ctx['networkNamespace'], 'observer_metric_application_scope')
        pg = self.ctx['postgres']['process']
        need(pg.get('uid') == 103 and pg.get('cgroup') == '0::/system.slice/' + PGUNIT and
             pg.get('networkNamespace') == self.ctx['networkNamespace'], 'observer_metric_postgres_scope')
        before = time.monotonic_ns()
        sample = {'kind': 'native-capacity-resource-sample', 'sequence': len(self.record['samples']) + 1,
                  'beforeMonotonicNs': before, 'wallTimeNs': time.time_ns(),
                  'applicationObservationMonotonicNs': supplied_time,
                  'intervalFromPreviousNs': None if self._last_sample_ns is None else before - self._last_sample_ns,
                  'gapBeyondTargetNs': None if self._last_sample_ns is None else max(0, before - self._last_sample_ns - 2_000_000_000),
                  'cgroups': {}, 'processes': {}, 'errors': []}
        for name in ('application', 'postgres'):
            try:
                sample['cgroups'][name] = self._metric_group(name)
            except BaseException as error:
                sample['cgroups'][name] = {'status': 'invalid', 'error': error_code(error)}
                sample['errors'].append(name + '-cgroup')
        for name, process, uid, unit in (('application', app, 995, APP), ('postgres', pg, 103, PGUNIT)):
            try:
                sample['processes'][name] = self._rss(process, uid, '0::/system.slice/' + unit)
            except BaseException as error:
                sample['processes'][name] = {'pid': process['pid'], 'startTicks': process['startTicks'],
                                             'status': 'invalid', 'error': error_code(error)}
                sample['errors'].append(name + '-rss')
        sample['afterMonotonicNs'] = time.monotonic_ns()
        sample['status'] = 'invalid' if sample['errors'] else ('complete' if
            all(row['status'] == 'available' for row in sample['cgroups'].values()) and
            all(row['status'] == 'sampled' for row in sample['processes'].values()) else 'partial')
        raw = encoded(sample)
        need(len(raw) <= SAMPLE_CAP and self.record['metricBytes'] + len(raw) <= METRIC_CAP,
             'observer_metric_evidence_byte_bound')
        pin = self._save(METRIC_ROOT / ('sample-%03d.json' % sample['sequence']), raw)
        self.record['samples'].append({'sequence': sample['sequence'], 'status': sample['status'],
                                      'beforeMonotonicNs': before, 'afterMonotonicNs': sample['afterMonotonicNs'],
                                      'receipt': pin})
        self.record['metricBytes'] += len(raw)
        self.record['metricRequestCount'] += 1
        self._last_sample_ns = before
        if sample['errors']:
            self.record['failed'] = True
            self.record['errors'].append({'stage': 'resource-sample', 'slot': None,
                                           'code': 'observer_metric_identity_or_counter_failed',
                                           'sampleSequence': sample['sequence']})
            raise ObserverError('observer_metric_identity_or_counter_failed')
        return sample
