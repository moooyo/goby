"""Prepare the fixed native capacity fixture without starting its application.

Import definitions only. make_preparation requires an already configured base,
loaded and pinned support/units/corpus modules, verified package descriptors and
tool records, four source dependencies, and the caller's absolute 300-second
business deadline. The caller owns admission, the deployment lock, protected
state, capacity checks, signal handling, evidence publication, and phase handoff.

The factory reads pinned sources and creates an in-memory actor. It changes
base.run only as its final step. Keep the returned actor reachable throughout
initialize_cluster and install, including every exception. Those methods create
only this new fixture and start only its anchor and PostgreSQL. They never call
an old actor main, create accounts, enable services, or start APP.

Inherited ownership, control, SQL and failure-stop methods come from the exact
unmodified R04 preparation source. Only its module constants and collaborators
are explicitly rebound. initialize_cluster and install are overridden because
the historical methods contain old database and installation path literals.

On failure the caller must assign cleanup_limit once to its existing absolute
closure deadline, set cleanup_deadline within that limit, and call cleanup_owned.
The inherited controls preserve their first per-unit stop deadlines and dispatch
at most one stop. They retain files and the tmpfs. Normal stop references,
archive/readback, ordinary unmount and final unit/path closure belong to the
later controller; this module does not claim those responsibilities completed.
The caller restores base.run = actor.original_run in its outer finally.
"""

import hashlib
import math
import os
from pathlib import Path
import pwd
import re
import secrets
import stat
import sys
import time


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
PREFIX = 'goby-native-capacity-20260915'
PG, DATA, SOCKET = F / 'pg', F / 'pg/data', F / 'pg/socket'
ANCHOR, PGUNIT, APP = (PREFIX + suffix for suffix in ('-net.service', '-postgres.service', '-app.service'))
MOUNT_SOURCE = PREFIX + '-postgres'
PORT, DATABASE, ROLE = 25499, 'goby_native_capacity', 'goby_native_capacity'
ORIGIN = 'http://127.0.0.1:18099'
UNIT_ROOT = Path('/run/systemd/system')
BINARY, ENVIRONMENT = F / 'bin/goby', F / 'config/goby.env'
STATE, CACHE, LOGS = F / 'state', F / 'cache', F / 'log'
APP_LOG = LOGS / 'application.jsonl'
RUNTIME = Path('/run') / PREFIX
PREPARATION_SOURCE = {
    'path': '/opt/goby-test/m6-systemd-install-20260915-r04/private/m6-systemd-install-prepare.py',
    'sha256': '8cb9e46efdbfd76a3a2b05ecc5d40c43f74020ff66843cd63a94956a4a72f1c8',
    'bytes': 61473,
}
SOURCE_PATHS = {name: E / 'private' / ('capacity-' + name + '.py')
                for name in ('support', 'units', 'corpus')}
BINARY_SHA = '7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1'
SAFE_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8',
            'LC_ALL': 'C.UTF-8', 'SYSTEMD_COLORS': '0'}
BUSINESS_SECONDS = 300
CLEANUP_BUDGET = {
    'postgresStopWindowSeconds': 115, 'anchorStopWindowSeconds': 35,
    'finalEvidenceReserveSeconds': 20, 'mandatoryStopAndEvidenceSeconds': 170,
    'deadlineRefreshes': 0, 'fileOrMountRemoval': False,
    'normalStopReferenceAcceptance': False,
}
SETUP_SQL_LABELS = ('catalog-before-create', 'create-owned-role-and-database', 'database-after-create')


def _need(value, code):
    if not value:
        raise ValueError(code)


def _microseconds(value):
    """Parse bounded systemctl timespans without accepting numeric coercion."""
    factors = {'h': 3600000000, 'min': 60000000, 's': 1000000, 'ms': 1000, 'us': 1}
    tokens = value.split() if type(value) is str else []
    _need(0 < len(tokens) <= 5, 'prepared_timespan_shape')
    total, seen = 0, set()
    for token in tokens:
        match = re.fullmatch(r'([0-9]+)(h|min|s|ms|us)', token)
        _need(match is not None and match[2] not in seen, 'prepared_timespan_token')
        seen.add(match[2])
        total += int(match[1]) * factors[match[2]]
    return total


def check_prepared_properties(properties, expected):
    """Check declared static settings and the exact never-started APP profile."""
    _need(isinstance(properties, dict) and all(type(key) is str and type(value) is str
          for key, value in properties.items()) and set(expected) ==
          {'exact', 'tokenSets', 'microseconds', 'integers', 'execStart', 'environmentFiles'},
          'prepared_property_contract')
    for key, value in expected['exact'].items():
        _need(properties.get(key) == value, 'prepared_exact_property_' + key)
    for key, values in expected['tokenSets'].items():
        actual = properties.get(key, '').split()
        _need(len(actual) == len(set(actual)) and sorted(actual) == sorted(values),
              'prepared_token_property_' + key)
    for key, value in expected['microseconds'].items():
        _need(type(value) is int and _microseconds(properties.get(key)) == value,
              'prepared_time_property_' + key)
    for key, value in expected['integers'].items():
        text = properties.get(key)
        _need(type(value) is int and type(text) is str and re.fullmatch('[0-9]+', text) and
              int(text) == value, 'prepared_integer_property_' + key)
    command = expected['execStart']
    _need(command == {'property': 'ExecStart', 'commands': 1, 'path': str(BINARY),
                     'argv': (str(BINARY),), 'ignoreErrors': False}, 'prepared_exec_declaration')
    rendered = properties.get('ExecStart', '')
    _need(rendered.count('{') == rendered.count('}') == 1 and rendered.startswith(
          '{ path=' + str(BINARY) + ' ; argv[]=' + str(BINARY) + ' ; ignore_errors=no ;') and
          rendered.endswith(' }'), 'prepared_exec_command')
    fields = [part.strip() for part in rendered[1:-1].strip().split(';')]
    _need(all('=' in field for field in fields), 'prepared_exec_fields')
    pairs = [field.split('=', 1) for field in fields]
    parsed = dict(pairs)
    _need(len(parsed) == len(pairs) and set(parsed) ==
          {'path', 'argv[]', 'ignore_errors', 'start_time', 'stop_time', 'pid', 'code', 'status'} and
          parsed['path'] == str(BINARY) and parsed['argv[]'] == str(BINARY) and
          parsed['ignore_errors'] == 'no', 'prepared_exec_fixed_fields')
    # ExecStart includes a presentation of changing exit/time fields. The
    # independent typed lifecycle properties below prove the idle state.
    environment = expected['environmentFiles']
    _need(environment == {'property': 'EnvironmentFiles',
          'files': ({'path': str(ENVIRONMENT), 'ignoreErrors': False},)} and
          properties.get('EnvironmentFiles') == str(ENVIRONMENT) + ' (ignore_errors=no)',
          'prepared_environment_file')
    _need(properties.get('ActiveState') == 'inactive' and properties.get('SubState') == 'dead' and
          properties.get('MainPID') == properties.get('ExecMainPID') == '0' and
          properties.get('ExecMainStartTimestampMonotonic') == '0' and
          properties.get('ExecMainExitTimestampMonotonic') == '0' and
          properties.get('NRestarts') == '0' and properties.get('InvocationID') == '' and
          properties.get('ControlGroup') in ('', '/system.slice/' + APP),
          'prepared_application_already_started')
    return {'declaredPropertiesMatched': True, 'applicationStarts': 0,
            'customFixtureUnit': True, 'shippedInstallationAcceptance': False}


def _source_dependencies(base, support, units, corpus, dependencies):
    _need(isinstance(dependencies, dict) and set(dependencies) ==
          {'preparation', 'support', 'units', 'corpus'} and
          dependencies['preparation'] == PREPARATION_SOURCE, 'preparation_dependency_contract')
    for name, module in (('support', support), ('units', units), ('corpus', corpus)):
        path = SOURCE_PATHS[name]
        _need(Path(module.__file__) == path and module.E == E and module.F == F,
              'preparation_loaded_dependency_scope')
        row = base.metadata(path)
        _need(row['uid'] == row['gid'] == 0 and row['type'] == stat.S_IFREG and
              row['mode'] == 0o600 and row['links'] == 1, 'preparation_dependency_metadata')
        support.pinned(base, dependencies[name], 2 << 20, path)
        _need(base.metadata(path) == row, 'preparation_dependency_changed')
    _need((support.PREFIX, support.ANCHOR, support.PGUNIT, support.APP) ==
          (PREFIX, ANCHOR, PGUNIT, APP) and (units.PREFIX, units.ANCHOR, units.PGUNIT, units.APP) ==
          (PREFIX, ANCHOR, PGUNIT, APP) and corpus.ROOT == F / 'media' and
          corpus.TEMPLATE_ROOT == E / 'private/templates' and units.APP_LOG == APP_LOG,
          'preparation_dependency_constants')
    raw = support.pinned(base, PREPARATION_SOURCE, 2 << 20, Path(PREPARATION_SOURCE['path']))
    _need(len(raw) == PREPARATION_SOURCE['bytes'], 'preparation_source_size')
    return support.bootstrap(Path(PREPARATION_SOURCE['path']), PREPARATION_SOURCE['sha256'],
                             'native_capacity_frozen_preparation_definitions')


def make_preparation(base, support, units, corpus, *, package, tools, dependencies, deadline):
    """Return a reachable actor before any fixture mutation or unit dispatch."""
    _need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
          sys.flags.isolated and sys.flags.dont_write_bytecode, 'capacity_remote_root_isolated_required')
    now = time.monotonic()
    _need(type(deadline) in (int, float) and math.isfinite(deadline) and now < deadline <= now + BUSINESS_SECONDS,
          'capacity_absolute_preparation_deadline')
    _need(base.E == E and base.P == E / 'private/prepare-commands' and isinstance(base.report, dict),
          'capacity_base_preparation_scope')
    for path in (E, E / 'private', base.P):
        row = base.metadata(path)
        _need(path.resolve() == path and row['type'] == stat.S_IFDIR and
              row['uid'] == row['gid'] == 0 and row['mode'] == 0o700, 'capacity_private_preparation_directory')
    _need(os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net') and
          os.readlink('/proc/self/ns/mnt') == os.readlink('/proc/1/ns/mnt'), 'capacity_host_namespaces_required')
    legacy = _source_dependencies(base, support, units, corpus, dependencies)
    legacy.E, legacy.F, legacy.PG, legacy.DATA, legacy.SOCKET = E, F, PG, DATA, SOCKET
    legacy.PREFIX, legacy.MOUNT_SOURCE = PREFIX, MOUNT_SOURCE
    legacy.ANCHOR, legacy.PGUNIT, legacy.APP = ANCHOR, PGUNIT, APP
    legacy.PORT, legacy.DATABASE, legacy.ROLE, legacy.ORIGIN = PORT, DATABASE, ROLE, ORIGIN
    legacy.BUSINESS_SECONDS = BUSINESS_SECONDS
    legacy.base, legacy.support, legacy.units, legacy.media, legacy.report = base, support, units, corpus, base.report
    legacy.SUPPORT_SHA = dependencies['support']['sha256']
    legacy.UNITS_SHA = dependencies['units']['sha256']
    legacy.MEDIA_SHA = dependencies['corpus']['sha256']
    _need(set(package) == {'closure', 'review', 'archive', 'buildManifest', 'packageManifest'} and
          set(tools) == legacy.TOOL_NAMES and callable(base.run), 'capacity_verified_input_inventory')
    for name in ('commands', 'createdPaths', 'sqlConnections', 'cleanupErrors', 'receiptErrors'):
        base.report.setdefault(name, [])
        _need(type(base.report[name]) is list, 'capacity_preparation_report_list')
    base.report.setdefault('ownedUnits', {})
    _need(base.report['ownedUnits'] == {} and base.report['sqlConnections'] == [],
          'capacity_preparation_already_consumed')

    class CapacityPreparation(legacy.Preparation):
        def __init__(self):
            self.value, self.tools = {'package': package}, tools
            self.deadline, self.cleanup_deadline, self.cleanup_limit = deadline, None, None
            self.control_active = False
            self.original_run = base.run
            self.sources = dict(dependencies)
            self.cleanup_budget = dict(CLEANUP_BUDGET)
            self.legacy = legacy
            self.ctx = {'scope': str(E), 'fixtureRoot': str(F),
                'hostNetworkNamespace': os.readlink('/proc/self/ns/net'),
                'bootId': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
                'database': DATABASE, 'role': ROLE, 'origin': ORIGIN,
                'account': {'name': 'goby', 'uid': 995, 'gid': 986}, 'tools': tools, 'package': package,
                'unitFiles': {}, 'expectedEnvironment': {}, 'installed': {}, 'appDirectories': {},
                'nonce': secrets.token_hex(10), 'setupToken': secrets.token_hex(32),
                'passwords': {name: secrets.token_hex(24) for name in ('admin', 'visible', 'hidden')},
                'rolePassword': secrets.token_hex(32), 'sourceDependencies': dict(dependencies)}
            base.report['preparationAdapter'] = {'sources': dict(dependencies), 'businessDeadlineMonotonic': deadline,
                'cleanupBudget': dict(CLEANUP_BUDGET), 'applicationStarts': 0,
                'setupSqlBudget': {'connections': 3, 'readOnly': 2, 'creation': 1,
                                   'businessObserverSlotsConsumed': 0, 'labels': list(SETUP_SQL_LABELS)}}

        def new_directory(self, path, mode, uid=0, gid=0):
            self.remaining(1)
            super().new_directory(path, mode, uid, gid)

        def prepare_templates(self):
            self.remaining(1)
            recipe = corpus.template_recipe()
            _need(recipe['generatorCommandSeconds'] == 5 and recipe['generatorWaitDelaySeconds'] == 2 and
                  set(recipe['templates']) == {'video', 'audio'}, 'capacity_template_recipe')
            directory = corpus.TEMPLATE_ROOT
            self.new_directory(directory, 0o700)
            base.report['templatePreparation'] = {'status': 'generating', 'commands': [], 'files': {},
                'recipe': recipe, 'generationDispatches': 0}
            progress = base.report['templatePreparation']
            for kind in ('video', 'audio'):
                item = recipe['templates'][kind]
                path = Path(item['path'])
                _need(path.parent == directory and not os.path.lexists(path), 'capacity_template_destination')
                label = 'generate-capacity-template-' + kind
                previous = base.ENV
                old_mask = os.umask(0o077)
                try:
                    base.ENV = dict(recipe['environment'])
                    try:
                        base.run(label, [self.tool('ffmpeg'), *item['arguments']], 5)
                    finally:
                        matches = [row for row in base.report['commands'] if row['label'] == label]
                        _need(len(matches) <= 1, 'capacity_template_dispatch_duplicated')
                        if matches:
                            progress['commands'].append(matches[0])
                            progress['generationDispatches'] += int('pid' in matches[0])
                finally:
                    base.ENV = previous
                    os.umask(old_mask)
                _need(len(matches) == 1 and matches[0]['closed'] is True and matches[0]['exitCode'] == 0 and
                      matches[0]['stdout']['bytes'] == matches[0]['stderr']['bytes'] == 0,
                      'capacity_template_generation_incomplete')
                saved = legacy.pin(path, maximum=item['acceptedBytesMaximum'])
                _need(saved['metadata']['uid'] == saved['metadata']['gid'] == 0 and
                      saved['metadata']['mode'] == 0o600 and saved['metadata']['links'] == 1 and
                      0 < saved['bytes'] <= item['acceptedBytesMaximum'], 'capacity_template_identity')
                progress['files'][kind] = saved
            _need(progress['generationDispatches'] == 2, 'capacity_template_generation_count')
            receipt = {'kind': 'native-scan-http-capacity-template-preparation', 'version': 1,
                'status': 'generated', 'scope': str(E), 'source': 'fresh-fixed-recipe',
                'generationCount': 2, 'recipe': recipe, 'files': progress['files'],
                'generatorTool': tools['ffmpeg'], 'commands': progress['commands'],
                'transportTimeoutSeconds': 5, 'failureGroupCloseoutMaximumSeconds': 11,
                'sourceRecipeWaitDelayIsNotTransportGuarantee': True, 'mediaProbeCommands': 0}
            provenance = legacy.save(corpus.TEMPLATE_RECEIPT, receipt)
            progress.update(status='generated', receipt=provenance)
            ancestors = {str(path): base.metadata(path) for path in (directory, *directory.parents)}
            return {'kind': 'native-scan-http-capacity-templates', 'version': 1, 'scope': str(E),
                    'files': progress['files'], 'ancestors': ancestors, 'provenance': provenance}

        def initialize_cluster(self):
            self.remaining(1)
            self.new_directory(F, 0o755)
            template_input = self.prepare_templates()
            base.report['corpusCreation'] = {'status': 'creating', 'root': str(F / 'media'), 'templates': template_input}
            self.ctx['media'] = corpus.prepare_corpus(base, template_input)
            base.report['corpusCreation'].update(status='created', totals=self.ctx['media']['totals'])
            self.remaining(1)
            self.new_directory(PG, 0o700)
            base.report['pgUnderlying'] = base.metadata(PG)
            base.report['mountAttempted'] = True
            base.run('mount-new-pg-volume', [self.tool('mount'), '-t', 'tmpfs', '-o',
                'size=768M,nr_inodes=65536,mode=0700,nosuid,nodev,noexec', MOUNT_SOURCE, str(PG)], 30)
            os.chown(PG, 103, 106)
            os.chmod(PG, 0o700)
            base.report['pgMount'] = legacy.mount_identity()
            self.ctx['pgMount'] = base.report['pgMount']
            self.new_directory(SOCKET, 0o700, 103, 106)
            password_path = PG / 'initdb-password'
            password_bytes = (secrets.token_hex(32) + '\n').encode('ascii')
            base.write_new(password_path, password_bytes, mode=0o600, uid=103, gid=106)
            password_pin = legacy.pin(password_path, owner=103, maximum=128)
            _need(password_pin['bytes'] == 65 and password_pin['metadata']['uid'] == 103 and
                  password_pin['metadata']['gid'] == 106 and password_pin['metadata']['mode'] == 0o600 and
                  password_pin['sha256'] == hashlib.sha256(password_bytes).hexdigest(), 'initdb_password_identity')
            base.report['initdbPasswordFile'] = password_pin
            del password_bytes
            old_cwd, environment = Path.cwd(), base.ENV
            try:
                os.chdir(F)
                base.ENV = {**SAFE_ENV, 'HOME': pwd.getpwnam('postgres').pw_dir, 'TZ': 'UTC'}
                base.run('initdb-new-cluster', [self.tool('runuser'), '--user', 'postgres', '--', self.tool('initdb'),
                    '-D', str(DATA), '--username=postgres', '--encoding=UTF8', '--locale=C.UTF-8',
                    '--auth-local=peer', '--auth-host=scram-sha-256', '--pwfile=' + str(password_path)], 90)
            finally:
                os.chdir(old_cwd)
                base.ENV = environment
            base.report['initializedDataDirectory'] = base.metadata(DATA)
            _need(base.report['initializedDataDirectory']['uid'] == 103 and
                  base.report['initializedDataDirectory']['gid'] == 106 and
                  base.report['initializedDataDirectory']['mode'] == 0o700, 'initialized_data_directory')
            config = '\n'.join(("port = 25499", "listen_addresses = '127.0.0.1'",
                "unix_socket_directories = '" + str(SOCKET) + "'", "unix_socket_permissions = 0700",
                "fsync = on", "full_page_writes = on", "max_connections = 32", "shared_buffers = '32MB'",
                "work_mem = '4MB'", "maintenance_work_mem = '16MB'", "max_wal_size = '64MB'",
                "min_wal_size = '32MB'", "logging_collector = off", "log_destination = 'stderr'",
                "log_timezone = 'UTC'", "timezone = 'UTC'", "password_encryption = 'scram-sha-256'", ''))
            hba = ('local all postgres peer map=capacity_owner\nlocal all all reject\n'
                   'host all goby_native_capacity 127.0.0.1/32 scram-sha-256\n'
                   'host all all 0.0.0.0/0 reject\nhost all all ::0/0 reject\n')
            ident = 'capacity_owner root postgres\ncapacity_owner postgres postgres\n'
            for name, text in (('postgresql.conf', config), ('pg_hba.conf', hba), ('pg_ident.conf', ident)):
                self.replace_new_configuration(name, text)
            self.start_unit(ANCHOR, units.anchor_unit())
            for name in ('pg.stdout', 'pg.stderr'):
                base.write_new(E / 'private' / name, b'')
            pg_unit = units.postgres_unit(self.ctx['anchorPid']) + 'StandardOutput=append:' + str(E / 'private/pg.stdout') + '\nStandardError=append:' + str(E / 'private/pg.stderr') + '\n'
            self.start_unit(PGUNIT, pg_unit)
            until = time.monotonic() + self.remaining(30)
            ready = False
            while time.monotonic() < until:
                self.bind_unit(PGUNIT)
                raw = base.read_regular(E / 'private/pg.stderr', 4 << 20)
                if b'database system is ready to accept connections' in raw:
                    ready = True
                    break
                time.sleep(0.1)
            _need(ready, 'new_postgres_not_ready')
            base.report['postgresReadyLog'] = legacy.descriptor(legacy.pin(E / 'private/pg.stderr', maximum=4 << 20))
            own_socket = base.metadata(SOCKET / ('.s.PGSQL.' + str(PORT)))
            _need(own_socket['type'] == stat.S_IFSOCK and own_socket['uid'] == 103 and
                  own_socket['gid'] == 106 and own_socket['mode'] == 0o700, 'new_postgres_socket_identity')
            base.report['postgresSocket'] = own_socket
            common = "'version',current_setting('server_version_num')::int,'port',current_setting('port')::int,'dataDirectory',current_setting('data_directory'),'otherClientBackends',(SELECT count(*) FROM pg_stat_activity WHERE backend_type='client backend' AND pid<>pg_backend_pid())"
            initial = self.sql('catalog-before-create', 'postgres', "SELECT jsonb_build_object(" + common +
                ",'roleCount',(SELECT count(*) FROM pg_roles WHERE rolname='goby_native_capacity'),'databaseCount',(SELECT count(*) FROM pg_database WHERE datname='goby_native_capacity'));\n")
            _need(initial == {'version': 170011, 'port': PORT, 'dataDirectory': str(DATA),
                  'otherClientBackends': 0, 'roleCount': 0, 'databaseCount': 0}, 'new_cluster_catalog_not_empty')
            base.report['clusterBeforeCreate'] = initial
            password = self.ctx['rolePassword']
            _need(re.fullmatch('[0-9a-f]{64}', password), 'generated_database_password')
            statement = "DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='goby_native_capacity') OR EXISTS (SELECT 1 FROM pg_database WHERE datname='goby_native_capacity') THEN RAISE EXCEPTION 'fixture role or database exists'; END IF; END $$;\n"
            statement += "CREATE ROLE goby_native_capacity LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "';\n"
            statement += "CREATE DATABASE goby_native_capacity OWNER goby_native_capacity TEMPLATE template0 ENCODING 'UTF8' LC_COLLATE 'C.UTF-8' LC_CTYPE 'C.UTF-8';\n"
            statement += "SELECT jsonb_build_object('roleCreated',EXISTS(SELECT 1 FROM pg_roles WHERE rolname='goby_native_capacity'),'databaseCreated',EXISTS(SELECT 1 FROM pg_database WHERE datname='goby_native_capacity'));\n"
            created = self.sql('create-owned-role-and-database', 'postgres', statement, readonly=False)
            _need(created == {'roleCreated': True, 'databaseCreated': True}, 'owned_database_not_created')
            facts = self.sql('database-after-create', DATABASE, "SELECT jsonb_build_object(" + common +
                ",'database',current_database(),'databaseOid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),'ownerOid',(SELECT datdba::bigint FROM pg_database WHERE datname=current_database()),'role',(SELECT jsonb_build_object('name',rolname,'oid',oid::bigint,'login',rolcanlogin,'superuser',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'replication',rolreplication,'bypassrls',rolbypassrls) FROM pg_roles WHERE rolname='goby_native_capacity'),'publicTables',(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p')),'preparedTransactions',(SELECT count(*) FROM pg_prepared_xacts),'roleMemberships',(SELECT count(*) FROM pg_auth_members WHERE roleid=(SELECT oid FROM pg_roles WHERE rolname='goby_native_capacity') OR member=(SELECT oid FROM pg_roles WHERE rolname='goby_native_capacity')));\n")
            role = facts['role']
            _need(facts['version'] == 170011 and facts['port'] == PORT and facts['dataDirectory'] == str(DATA) and
                  facts['database'] == DATABASE and type(facts['databaseOid']) is int and facts['databaseOid'] > 0 and
                  type(facts['ownerOid']) is int and facts['ownerOid'] == role['oid'] and
                  type(role['oid']) is int and role['oid'] > 0 and role['name'] == ROLE and role['login'] is True and
                  all(role[key] is False for key in ('superuser', 'createdb', 'createrole', 'replication', 'bypassrls')) and
                  all(facts[key] == 0 for key in ('otherClientBackends', 'publicTables', 'preparedTransactions', 'roleMemberships')),
                  'owned_database_role_facts')
            self.ctx.update(databaseOid=facts['databaseOid'], roleOid=role['oid'])
            base.report['databaseFacts'] = facts
            _need(legacy.mount_identity() == base.report['pgMount'], 'postgres_volume_changed')
            rows = base.report['sqlConnections']
            _need(len(rows) == 3 and tuple(row['label'] for row in rows) == SETUP_SQL_LABELS and
                  [row['readOnly'] for row in rows] == [True, False, True] and
                  all(row['frontendClosed'] and row['backendGone'] for row in rows), 'setup_sql_connection_closure')
            base.report['preparationAdapter']['setupSqlCompleted'] = {
                'connections': 3, 'readOnly': 2, 'creation': 1, 'businessObserverSlotsConsumed': 0,
                'labels': list(SETUP_SQL_LABELS), 'allFrontendBackendClosed': True}

        def install(self, payloads, build_manifest):
            self.remaining(1)
            corpus.snapshot_corpus(base, self.ctx['media'])
            _need(set(payloads) == {'goby', 'goby.service', 'goby.env.example', 'manifest.json', 'INSTALL.md'} and
                  hashlib.sha256(payloads['goby']).hexdigest() == BINARY_SHA and
                  build_manifest['binary']['sha256'] == BINARY_SHA, 'capacity_install_binary_binding')
            self.ctx['buildManifest'] = build_manifest
            self.new_directory(F / 'bin', 0o755)
            self.new_directory(F / 'config', 0o700)
            for name, path in (('state', STATE), ('cache', CACHE), ('logs', LOGS)):
                self.new_directory(path, 0o700, 995, 986)
                self.ctx['appDirectories'][name] = {'path': str(path), 'metadata': base.metadata(path)}
            base.report['applicationLogCreation'] = {'path': str(APP_LOG), 'writeIntent': True, 'created': False}
            base.write_new(APP_LOG, b'', mode=0o600)
            log_metadata = base.metadata(APP_LOG)
            _need(log_metadata['type'] == stat.S_IFREG and log_metadata['uid'] == log_metadata['gid'] == 0 and
                  log_metadata['mode'] == 0o600 and log_metadata['links'] == 1 and log_metadata['bytes'] == 0,
                  'capacity_application_log_identity')
            self.ctx['appLog'] = {'path': str(APP_LOG), 'metadata': log_metadata,
                                  'sha256': hashlib.sha256(b'').hexdigest(), 'bytes': 0}
            base.report['applicationLogCreation'].update(created=True, metadata=log_metadata)
            self.ctx['appDirectories']['logs']['metadata'] = base.metadata(LOGS)
            template = payloads['goby.env.example'].decode('utf-8', 'strict')
            environment = {}
            for line in template.splitlines():
                if not line.strip() or line.lstrip().startswith('#'):
                    continue
                _need('=' in line and not line.startswith((' ', '\t')), 'packaged_environment_template')
                key, value = line.split('=', 1)
                _need(re.fullmatch('GOBY_[A-Z0-9_]+', key) and key not in environment and
                      not any(char in value for char in ('\x00', '\r', '\n')), 'packaged_environment_assignment')
                environment[key] = value
            overrides = {
                'GOBY_DATABASE_URL': 'postgres://' + ROLE + ':' + self.ctx['rolePassword'] + '@127.0.0.1:25499/' + DATABASE + '?sslmode=disable',
                'GOBY_SETUP_TOKEN': self.ctx['setupToken'], 'GOBY_LISTEN': '127.0.0.1:18099',
                'GOBY_PUBLIC_URL': ORIGIN, 'GOBY_COOKIE_SECURE': 'false', 'GOBY_TRUSTED_PROXIES': '',
                'GOBY_SERVER_NAME': 'Goby native capacity fixture', 'GOBY_FFMPEG': self.tool('ffmpeg'),
                'GOBY_FFPROBE': self.tool('ffprobe'), 'GOBY_MEDIA_ROOTS': str(F / 'media'),
                'GOBY_API_KEY_MASTER_KEY_FILE': str(STATE / 'application-key-master.key'),
                'GOBY_TRANSCODE_CACHE': str(CACHE / 'transcodes'), 'GOBY_LOG_DIR': str(LOGS / 'diagnostics'),
            }
            _need(set(overrides) <= set(environment) and 'GOBY_WEB_DIR' not in environment,
                  'capacity_environment_override_keys')
            environment.update(overrides)
            additions = {'GOBY_RECOVERY_STATE_DIR': str(STATE / 'recovery'),
                'GOBY_RECOVERY_OPERATIONS_DIR': str(STATE / 'recovery-operations'),
                'GOBY_BACKUP_DIR': str(STATE / 'backups'), 'GOBY_PG_DUMP': self.tool('pg_dump'),
                'GOBY_PG_RESTORE': self.tool('pg_restore')}
            _need(not set(additions) & set(environment), 'capacity_environment_addition_keys')
            environment.update(additions)
            _need(all(not any(char in value for char in ('"', "'", '\\', '\n', '\r', '\0'))
                      for value in environment.values()), 'environment_render_boundary')
            rendered = '\n'.join(key + '=' + value for key, value in environment.items()) + '\n'
            files = {'binary': (BINARY, payloads['goby'], 0o755),
                     'unit': (UNIT_ROOT / APP, units.app_unit(self.ctx['anchorPid']).encode(), 0o644),
                     'environment': (ENVIRONMENT, rendered.encode(), 0o600)}
            base.report['installationFiles'] = {}
            for name, (path, raw, mode) in files.items():
                self.remaining(1)
                _need(not os.path.lexists(path), 'capacity_install_destination_exists')
                row = {'path': str(path), 'writeIntent': True, 'mode': mode, 'written': False}
                base.report['installationFiles'][name] = row
                base.write_new(path, raw, mode=mode)
                saved = legacy.pin(path)
                self.ctx['installed'][name] = saved
                row.update(written=True, file=saved)
            self.ctx['unitFiles'][APP] = self.ctx['installed']['unit']
            self.ctx['expectedEnvironment'] = environment
            base.run('reload-capacity-unit-without-start', [self.tool('systemctl'), 'daemon-reload'], 30)
            wanted = tuple(dict.fromkeys((*support.SERVICE_PROPERTIES, 'TimeoutStartUSec')))
            raw = base.run('prepared-capacity-service-properties', [self.tool('systemctl'), 'show', APP,
                '--no-pager', '--property=' + ','.join(wanted)], 15)
            pairs = [line.split('=', 1) for line in raw.decode('utf-8', 'strict').splitlines()]
            _need(all(len(pair) == 2 for pair in pairs) and len(pairs) == len(dict(pairs)) and
                  set(dict(pairs)) == set(wanted), 'capacity_service_property_inventory')
            properties = dict(pairs)
            self.ctx['preparedServiceProperties'] = properties
            expected = units.expected_properties(self.ctx['anchorPid'])
            self.ctx['declaredServiceProperties'] = expected
            base.report['preparedServiceValidation'] = check_prepared_properties(properties, expected)
            support.check_installation(base, self.ctx)
            _need(not os.path.lexists(RUNTIME) and not os.path.lexists(STATE / 'application-key-master.key'),
                  'capacity_application_output_created_before_start')
            for row in self.ctx['appDirectories'].values():
                _need(base.metadata(Path(row['path'])) == row['metadata'] and
                      {path.name for path in Path(row['path']).iterdir()} ==
                      ({APP_LOG.name} if Path(row['path']) == LOGS else set()),
                      'capacity_prepared_directory_changed')
            _need(base.metadata(APP_LOG) == log_metadata, 'capacity_prepared_application_log_changed')
            corpus.snapshot_corpus(base, self.ctx['media'])
            self.remaining(1)

    actor = CapacityPreparation()
    base.run = actor.command
    return actor
