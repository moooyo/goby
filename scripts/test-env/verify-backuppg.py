#!/usr/bin/env python3
"""Verify backup modules in an owned PostgreSQL 17 source/target pair on 15432."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import signal
import stat
import subprocess
import sys
import time

WORK = Path('/opt/goby-test/exec-work-m5j')
CONTROL = Path('/opt/goby-test')
PG_BASE = Path('/dev/shm/goby-pg-m4c')
PG_DATA = PG_BASE / 'data'
PG_HBA = PG_DATA / 'pg_hba.conf'
PG_BIN = Path('/usr/lib/postgresql/17/bin')
GO = Path('/opt/goby-toolchains/go1.27.1/bin/go')
MARKER = 'goby-backuppg-pair-m5j-v1'
BASE_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
BASE_HBA = b'''# Only local peer administration and the dedicated TCP test role.
local all postgres peer
local all all reject
host goby_test goby_test 127.0.0.1/32 scram-sha-256
host all all 0.0.0.0/0 reject
host all all ::0/0 reject
'''


class Failure(Exception):
    pass


def require(value, message):
    if not value:
        raise Failure(message)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def private_read(path, uid=0, maximum=1 << 20):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == uid and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and info.st_size <= maximum, 'Private control identity changed.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        return stream.read(maximum + 1)


def private_write(path, data):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())


def command(args, text=None, timeout=30, env=None):
    process = subprocess.Popen([str(a) for a in args], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env or BASE_ENV, start_new_session=True)
    try:
        output, _ = process.communicate(text, timeout=timeout)
        require(process.returncode == 0, 'A controlled command failed; private diagnostics were retained.')
        return output.strip()
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate(timeout=5)


def pg(sql):
    return command(['runuser', '--user', 'postgres', '--', PG_BIN / 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At', '-h', PG_BASE / 'socket', '-p', '15432', '-d', 'postgres'], text=sql + '\n')


def service_state():
    result = {}
    for name, expected in [('goby-foundation-test.service', 3668655), ('goby-emby-reference.service', 3131777)]:
        values = command(['systemctl', 'show', name, '--property=MainPID', '--property=ActiveState'])
        state = dict(line.split('=', 1) for line in values.splitlines() if '=' in line)
        require(state == {'MainPID': str(expected), 'ActiveState': 'active'}, 'A protected service identity changed.')
        proc = Path('/proc') / str(expected)
        data = (proc / 'stat').read_text()
        ticks = data[data.rfind(')') + 2:].split()[19]
        result[name] = {**state, 'start_ticks': ticks, 'binary_sha256': sha((proc / 'exe').read_bytes())}
    return result


def catalog_state():
    return json.loads(pg("SELECT jsonb_build_object('databases',(SELECT jsonb_agg(jsonb_build_object('name',datname,'owner',pg_get_userbyid(datdba),'oid',oid,'connect',datallowconn) ORDER BY datname) FROM pg_database),'roles',(SELECT jsonb_agg(jsonb_build_object('name',rolname,'oid',oid,'super',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'replication',rolreplication,'bypass',rolbypassrls) ORDER BY rolname) FROM pg_roles));"))


class Runner:
    def __init__(self, args):
        self.args = args
        self.run = time.strftime('%Y%m%d_%H%M%S', time.gmtime()) + '_' + secrets.token_hex(4)
        self.tag = MARKER + ':' + self.run
        self.control_name = 'goby_backuppg_control_' + self.run
        BASE_ENV['PGAPPNAME'] = self.control_name
        self.output = WORK / ('backuppg-' + args.mode + '-' + self.run)
        self.unit = 'goby-backuppg-' + self.run.replace('_', '-') + '.service'
        self.pairs = []
        self.lock = None
        self.original = None
        self.temporary = None
        self.hba_intent = False
        self.unit_intent = False
        self.postgres = pwd.getpwnam('postgres')
        self.report = {'marker': MARKER, 'run_id': self.run, 'mode': args.mode, 'status': 'running', 'checks': {}, 'cleanup': {}}
        self.catalog_before = None
        self.services_before = None

    def prepare(self):
        require(os.getuid() == 0, 'Root operator identity is required.')
        require(WORK.resolve() == WORK and stat.S_IMODE(WORK.stat().st_mode) == 0o700 and WORK.stat().st_uid == 0, 'Workspace identity changed.')
        source = self.args.source
        require(source.parent == WORK and source.resolve() == source and re.fullmatch(r'backuppg-source-attempt-[1-9][0-9]*', source.name), 'Source snapshot path is not owned.')
        manifest_bytes = private_read(source / 'source-inputs.json', maximum=4 << 20)
        require(sha(manifest_bytes) == self.args.manifest_sha256, 'Source manifest digest changed.')
        manifest = json.loads(manifest_bytes)
        require(manifest['marker'] == 'goby-backuppg-source-m5j-v1', 'Source marker changed.')
        actual = {}
        for path in source.rglob('*'):
            require(not path.is_symlink(), 'Source snapshot contains a symlink.')
            if path.is_file() and path.name not in ('source-inputs.json', 'OWNER.txt'):
                actual[str(path.relative_to(source))] = sha(path.read_bytes())
        require(actual == manifest['files'], 'Source snapshot bytes or membership changed.')
        self.output.mkdir(mode=0o700)
        private_write(self.output / 'owner.json', json.dumps({'marker': MARKER, 'run_id': self.run, 'unit': self.unit, 'source': str(source)}, sort_keys=True).encode())
        self.report['source'] = {'directory': str(source), 'manifest_sha256': sha(manifest_bytes), 'file_count': len(actual)}
        self.services_before = service_state()
        self.report['protected_services_before'] = self.services_before
        private_read(CONTROL / 'm4c-postgres.lock')
        self.lock = os.open(CONTROL / 'm4c-postgres.lock', os.O_RDWR | os.O_NOFOLLOW)
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        owner = json.loads(private_read(CONTROL / 'm4c-postgres.owner'))
        require(owner['marker'] == 'goby-postgres-scratch-m4c-v1' and owner['directory'] == str(PG_BASE) and owner['port'] == 15432 and owner['uid'] == self.postgres.pw_uid, 'Scratch cluster ownership record changed.')
        actual_cluster = json.loads(pg("SELECT jsonb_build_object('data',current_setting('data_directory'),'port',current_setting('port'),'hba',current_setting('hba_file'),'system_identifier',system_identifier::text) FROM pg_control_system();"))
        require(actual_cluster == {'data': str(PG_DATA), 'port': '15432', 'hba': str(PG_HBA), 'system_identifier': str(owner['system_identifier'])}, 'Connection is not the owned port-15432 cluster.')
        self.original = private_read(PG_HBA, uid=self.postgres.pw_uid)
        require(self.original == BASE_HBA, 'Scratch HBA differs from reviewed baseline.')
        self.catalog_before = catalog_state()
        self.report['hba_before_sha256'] = sha(self.original)
        self.report['catalog_before_sha256'] = sha(json.dumps(self.catalog_before, sort_keys=True).encode())
        self.report['checks']['scratch_cluster_identity'] = True
        rules = []
        for side in ('source', 'target'):
            suffix = self.run.replace('_', '')
            pair = {'database': 'goby_backup_' + side + '_' + suffix, 'role': 'goby_backup_' + side + '_r_' + suffix, 'password': secrets.token_hex(24), 'role_intent': False, 'database_intent': False, 'oid': None}
            self.pairs.append(pair)
            db, role = pair['database'], pair['role']
            require(pg(f"SELECT (SELECT count(*) FROM pg_database WHERE datname='{db}')+(SELECT count(*) FROM pg_roles WHERE rolname='{role}');") == '0', 'Generated database or role already exists.')
            pair['role_intent'] = True
            pg(f"BEGIN; SET password_encryption='scram-sha-256'; CREATE ROLE {role} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 12 PASSWORD '{pair['password']}'; COMMENT ON ROLE {role} IS '{self.tag}'; COMMIT;")
            pair['database_intent'] = True
            pg(f'CREATE DATABASE {db} OWNER {role} TEMPLATE template0 ENCODING \'UTF8\';')
            pair['oid'] = int(pg(f"SELECT oid FROM pg_database WHERE datname='{db}';"))
            pg(f"COMMENT ON DATABASE {db} IS '{self.tag}'; REVOKE CONNECT,TEMPORARY ON DATABASE {db} FROM PUBLIC;")
            rules.append(f'# {self.tag}\nhost {db} {role} 127.0.0.1/32 scram-sha-256\n')
            flags = json.loads(pg(f"SELECT jsonb_build_object('super',rolsuper,'createdb',rolcreatedb,'createrole',rolcreaterole,'replication',rolreplication,'bypass',rolbypassrls,'login',rolcanlogin) FROM pg_roles WHERE rolname='{role}';"))
            require(flags == {'super': False, 'createdb': False, 'createrole': False, 'replication': False, 'bypass': False, 'login': True}, 'Fixture role privileges changed.')
        self.temporary = ''.join(rules).encode() + self.original
        self.hba_intent = True
        self.replace_hba(self.original, self.temporary)
        require(pg('SELECT pg_reload_conf();') == 't' and pg('SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;') == '0', 'Exact temporary HBA rules did not load.')
        self.report['checks']['independent_low_privilege_pair'] = True
        self.report['checks']['exact_temporary_hba'] = True
        (self.output / 'tmp').mkdir(mode=0o700)
        env = {'GOCACHE': '/dev/shm/goby-go-cache', 'GOMODCACHE': '/dev/shm/goby-go-mod', 'GOMEMLIMIT': '384MiB', 'GOMAXPROCS': '2', 'GOTMPDIR': str(self.output / 'tmp'), 'TMPDIR': str(self.output / 'tmp'), 'GOBY_TEST_BACKUP_DISPOSABLE_DATABASES': '1', 'GOBY_TEST_PG_DUMP': str(PG_BIN / 'pg_dump'), 'GOBY_TEST_PG_RESTORE': str(PG_BIN / 'pg_restore'), 'GOBY_BACKUP_PG_RUN_ID': self.run}
        for pair, side in zip(self.pairs, ('SOURCE', 'TARGET')):
            env['GOBY_TEST_BACKUP_' + side + '_DATABASE_URL'] = f"postgresql://{pair['role']}:{pair['password']}@127.0.0.1:15432/{pair['database']}?sslmode=disable"
        if self.args.mode in ('recovery', 'bindings', 'full'):
            env['GOBY_TEST_DATABASE_URL'] = env['GOBY_TEST_BACKUP_SOURCE_DATABASE_URL']
        private_write(self.output / 'run.env', ''.join(f'{key}={value}\n' for key, value in env.items()).encode())
        self.report['databases'] = [{'database': p['database'], 'role': p['role'], 'oid': p['oid']} for p in self.pairs]

    def replace_hba(self, expected, replacement):
        require(private_read(PG_HBA, uid=self.postgres.pw_uid) == expected, 'Scratch HBA changed outside this operator.')
        temporary = PG_DATA / ('.backuppg-hba-' + self.run)
        private_write(temporary, replacement)
        os.chown(temporary, self.postgres.pw_uid, self.postgres.pw_gid)
        try:
            require(private_read(PG_HBA, uid=self.postgres.pw_uid) == expected, 'Scratch HBA changed before replacement.')
            os.replace(temporary, PG_HBA)
        finally:
            if temporary.exists():
                temporary.unlink()

    def execute(self):
        log = self.output / 'go.log'
        private_write(log, b'')
        if self.args.mode == 'catalog':
            run = [GO, 'run', '-p=1', 'scripts/test-env/generate-backuppg-catalog.go', self.output / 'schema-23-postgresql-17.json']
        elif self.args.mode == 'recovery':
            run = ['/bin/bash', 'scripts/test-env/run-backup-recovery-tests.sh']
        elif self.args.mode == 'bindings':
            run = ['/bin/bash', 'scripts/test-env/run-backup-bindings-tests.sh']
        elif self.args.mode == 'full':
            run = ['/bin/bash', 'scripts/test-env/run-backup-full-tests.sh']
        else:
            run = [GO, 'test', '-race', '-p=1', '-count=1', '-timeout=10m', '-json', './internal/backuppg']
        arguments = ['systemd-run', '--quiet', '--wait', '--unit=' + self.unit, '--service-type=exec', '--property=Description=' + self.tag,
                     '--property=MemoryMax=1536M', '--property=MemorySwapMax=0', '--property=CPUQuota=150%', '--property=KillMode=control-group',
                     '--property=TimeoutStopSec=10', '--property=RuntimeMaxSec=' + ('2400' if self.args.mode == 'full' else '660'), '--property=WorkingDirectory=' + str(self.args.source),
                     '--property=EnvironmentFile=' + str(self.output / 'run.env'), '--property=StandardOutput=append:' + str(log), '--property=StandardError=append:' + str(log)] + [str(a) for a in run]
        self.unit_intent = True
        wrapper = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=BASE_ENV, start_new_session=True)
        deadline = time.monotonic() + (2430 if self.args.mode == 'full' else 690)
        proof = None
        try:
            while wrapper.poll() is None:
                require(time.monotonic() < deadline, 'Controlled unit exceeded its outer deadline.')
                if proof is None:
                    raw = command(['systemctl', 'show', self.unit, '--property=Description', '--property=MemoryMax', '--property=MemorySwapMax', '--property=CPUQuotaPerSecUSec', '--property=KillMode', '--property=MainPID'])
                    values = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
                    if values.get('Description') == self.tag and values.get('MainPID') not in (None, '0'):
                        require(values['MemoryMax'] == str(1536 << 20) and values['MemorySwapMax'] == '0' and values['CPUQuotaPerSecUSec'] == '1.500000s' and values['KillMode'] == 'control-group', 'Verification cgroup bounds do not match.')
                        proof = values
                time.sleep(0.1)
            stdout, stderr = wrapper.communicate(timeout=5)
            private_write(self.output / 'unit.stdout', stdout)
            private_write(self.output / 'unit.stderr', stderr)
            self.report['unit_exit'] = wrapper.returncode
        finally:
            if wrapper.poll() is None:
                self.stop_unit()
                os.killpg(wrapper.pid, signal.SIGKILL)
                wrapper.communicate(timeout=5)
        require(proof is not None, 'Running verification cgroup was not observed.')
        self.report['cgroup'] = proof
        self.report['log_sha256'] = sha(log.read_bytes())
        require(wrapper.returncode == 0, 'Go verification failed; private log retained.')
        if self.args.mode == 'catalog':
            data = private_read(self.output / 'schema-23-postgresql-17.json', maximum=4 << 20)
            value = json.loads(data)
            require(value['version'] == 23 and value['postgresql_major'] == 17 and len(value['migrations']) == 23 and len(value['catalog']['Tables']) == 29, 'Generated catalog has unexpected version or inventory.')
            self.report['catalog_sha256'] = sha(data)
            self.report['checks']['fresh_trusted_catalog_export'] = True
        else:
            events = [json.loads(line) for line in log.read_text().splitlines() if line.startswith('{')]
            skips = [e for e in events if e.get('Action') == 'skip' and e.get('Test')]
            failures = [e for e in events if e.get('Action') == 'fail']
            passed = [e['Test'] for e in events if e.get('Action') == 'pass' and e.get('Test') and '/' not in e['Test']]
            expected_pg = {'TestPostgreSQLBackupConsistentSnapshotAndRestore',
                           'TestPostgreSQLRestoreFingerprintFailureLeavesEmptyTarget',
                           'TestPostgreSQLRestoreRejectsPopulatedTarget',
                           'TestPostgreSQLSnapshotRejectsSchemaDrift',
                           'TestPostgreSQLSnapshotCancellationAndSettingsIsolation',
                           'TestPostgreSQLOfflineRestoreWithUnavailableSource',
                           'TestPostgreSQLOfflineRestoreRejectsIdentityAndOverrides'}
            require(not skips and not failures and b'WARNING: DATA RACE' not in log.read_bytes(),
                    'Module execution had skips, failures, or a race warning.')
            if self.args.mode == 'recovery':
                required = {'TestEngineBackupRestoreRoundTrip',
                            'TestEngineRestoreRejectsWrongMasterBeforeCredentialRevocation',
                            'TestOpenArchiveRetainsScratchUntilCallerCloses',
                            'TestRestoredRootMustMatchApprovedIdentityAndExactRelativePath',
                            'TestDeploymentLeaseConcurrentAcquisitionHasOneDatabaseWideWinner',
                            'TestDeploymentLeaseReportsOwnedBackendLossAndAllowsSuccessor',
                            'TestDeploymentLeaseNilBoundaries',
                            'TestDeploymentLeaseCancelledAcquisitionLeavesNoOwnership',
                            'TestRecoveryDatabaseRequiresExplicitIndependentIdentity'}
                complete = {e.get('Test') for e in events if e.get('Action') == 'pass'}
                variants = {'TestEngineBackupRestoreRoundTrip/online',
                            'TestEngineBackupRestoreRoundTrip/offline_unreachable_original'}
                require(len(passed) >= 36 and required <= set(passed) and variants <= complete,
                        'Recovery pipeline execution omitted required real cases.')
                self.report['checks']['real_recovery_pipeline_and_offline_variant'] = True
            elif self.args.mode == 'full':
                required = expected_pg | {'TestRecoveryDatabaseStoreIntegration',
                    'TestRecoveryManagerNativeWorkflow', 'TestRecoveryManagerPublicationCrashRecovery',
                    'TestRecoveryManagerCancellationReceiptSurvivesRestart',
                    'TestRecoveryOperatorUsesOnlyTrustedOfflineAuthority',
                    'TestRecoveryManagerApplyRestartAndRollback',
                    'TestRecoveryManagerReturnsUnacceptedTargetAfterRestart',
                    'TestRecoveryManagerRejectsChangedCandidateBeforeActivation'}
                require(len(passed) >= 1500 and required <= set(passed),
                        'Complete execution omitted required product or database cases.')
                # go list membership is recorded inside the same frozen source
                # invocation. Every listed package must finish its test action.
                expected_packages = set((self.output / 'tmp' / 'packages.txt').read_text().splitlines())
                finished_packages = {e.get('Package') for e in events if not e.get('Test') and e.get('Action') in ('pass','skip')}
                require(len(expected_packages) >= 24 and finished_packages == expected_packages,
                        'Complete execution omitted a listed package.')
                for event in events:
                    if event.get('Action') == 'skip' and not event.get('Test'):
                        require(any(e.get('Package') == event.get('Package') and '[no test files]' in e.get('Output','') for e in events),
                                'A package was skipped without a no-test-files result.')
                self.report['packages'] = sorted(expected_packages)
                binary = self.output / 'tmp' / 'goby-linux-amd64'
                require(binary.is_file() and binary.stat().st_size > 1 << 20,
                        'Complete verified source did not produce an executable.')
                self.report['binary'] = {'path':str(binary),'sha256':sha(binary.read_bytes()),'bytes':binary.stat().st_size}
                self.report['checks']['complete_repository_race_and_build'] = True
            elif self.args.mode == 'bindings':
                required = expected_pg | {'TestRecoveryDatabaseStoreIntegration',
                                          'TestDeploymentLeaseTransactionBinding',
                                          'TestDeploymentLeaseConcurrentAcquisitionHasOneDatabaseWideWinner',
                                          'TestDeploymentLeaseReportsOwnedBackendLossAndAllowsSuccessor',
                                          'TestDeploymentLeaseNilBoundaries',
                                          'TestDeploymentLeaseCancelledAcquisitionLeavesNoOwnership'}
                require(len(passed) >= 70 and required <= set(passed) and
                        {name for name in passed if name.startswith('TestPostgreSQL')} == expected_pg,
                        'Bindings execution omitted required full module or actual database cases.')
                self.report['checks']['recovery_binding_transaction_and_retained_reset'] = True
            else:
                require(len(passed) >= 54 and {name for name in passed if name.startswith('TestPostgreSQL')} == expected_pg,
                        'Full module execution omitted PostgreSQL tests.')
            self.report['tests'] = {'passed': passed, 'top_level_passes': len(passed), 'skips': 0, 'failures': 0}
            self.report['checks']['recovery_configuration_and_lease_race' if self.args.mode == 'recovery' else 'full_race_suite_and_real_pair'] = True

    def stop_unit(self):
        if not self.unit_intent:
            return
        owner = json.loads(private_read(self.output / 'owner.json'))
        require(owner['marker'] == MARKER and owner['run_id'] == self.run and owner['unit'] == self.unit, 'Unit owner marker changed.')
        raw = command(['systemctl', 'show', self.unit, '--property=LoadState', '--property=Description'])
        values = dict(line.split('=', 1) for line in raw.splitlines() if '=' in line)
        if values.get('LoadState') == 'not-found':
            return
        require(values.get('Description') == self.tag, 'Unit description changed; refusing to stop unrelated work.')
        command(['systemctl', 'stop', self.unit], timeout=20)
        subprocess.run(['systemctl', 'reset-failed', self.unit], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False, timeout=10)

    def cleanup(self):
        def attempt(name, action):
            try:
                action()
                self.report['cleanup'][name] = True
            except Exception as error:
                self.report['cleanup'][name] = False
                self.report.setdefault('cleanup_errors', {})[name] = str(error) if isinstance(error, Failure) else type(error).__name__
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        attempt('owned_cgroup_stopped', self.stop_unit)
        if self.pairs:
            BASE_ENV['PGAPPNAME'] = self.control_name + '_cleanup'
            def stop_controls():
                pg(f"SELECT pg_terminate_backend(pid,5000) FROM pg_stat_activity WHERE application_name='{self.control_name}' AND pid<>pg_backend_pid();")
                require(pg(f"SELECT count(*) FROM pg_stat_activity WHERE application_name='{self.control_name}' AND pid<>pg_backend_pid();") == '0', 'An in-flight control connection remains.')
            attempt('inflight_control_connections_stopped', stop_controls)
        for pair in reversed(self.pairs):
            def drop_database(pair=pair):
                if not pair['database_intent']:
                    return
                require(self.report['cleanup'].get('inflight_control_connections_stopped') is True, 'Control connections were not fenced.')
                db, role = pair['database'], pair['role']
                current = json.loads(pg(f"SELECT COALESCE((SELECT jsonb_build_object('oid',oid,'owner',pg_get_userbyid(datdba),'tag',shobj_description(oid,'pg_database')) FROM pg_database WHERE datname='{db}'),'null'::jsonb);"))
                if current is None:
                    return
                require(current['owner'] == role and current['tag'] in (None, self.tag) and (pair['oid'] is None or int(current['oid']) == pair['oid']) and pg(f"SELECT shobj_description(oid,'pg_authid') FROM pg_roles WHERE rolname='{role}';") == self.tag, 'Fixture database ownership changed.')
                pg(f"SELECT pg_terminate_backend(pid,5000) FROM pg_stat_activity WHERE datname='{db}' AND pid<>pg_backend_pid();")
                pg(f'DROP DATABASE {db};')
                require(pg(f"SELECT count(*) FROM pg_database WHERE datname='{db}';") == '0', 'Fixture database remains.')
            def drop_role(pair=pair):
                if not pair['role_intent']:
                    return
                role = pair['role']
                if pg(f"SELECT count(*) FROM pg_roles WHERE rolname='{role}';") == '0':
                    return
                require(pg(f"SELECT shobj_description(oid,'pg_authid') FROM pg_roles WHERE rolname='{role}';") == self.tag, 'Fixture role ownership changed.')
                pg(f'DROP ROLE {role};')
            attempt(pair['database'] + '_removed', drop_database)
            attempt(pair['role'] + '_removed', drop_role)
        if self.hba_intent:
            def restore_hba():
                actual = private_read(PG_HBA, uid=self.postgres.pw_uid)
                require(actual in (self.original, self.temporary), 'Unrelated HBA edit prevents restoration.')
                if actual == self.temporary:
                    self.replace_hba(self.temporary, self.original)
                require(pg('SELECT pg_reload_conf();') == 't' and private_read(PG_HBA, uid=self.postgres.pw_uid) == self.original, 'Original HBA restoration failed.')
                self.report['hba_after_sha256'] = sha(self.original)
            attempt('hba_restored_exactly', restore_hba)
        if self.catalog_before is not None:
            def verify_catalog():
                after = catalog_state()
                require(after == self.catalog_before, 'Existing database or role catalog changed.')
                self.report['catalog_after_sha256'] = sha(json.dumps(after, sort_keys=True).encode())
            attempt('preexisting_database_and_role_catalog_unchanged', verify_catalog)
        if self.services_before is not None:
            def verify_services():
                after = service_state()
                require(after == self.services_before, 'Protected services changed.')
                self.report['protected_services_after'] = after
            attempt('protected_services_unchanged', verify_services)
        if self.lock is not None:
            fcntl.flock(self.lock, fcntl.LOCK_UN)
            os.close(self.lock)
            self.lock = None
        if not all(self.report['cleanup'].values()):
            self.report['status'] = 'failed'

    def run_all(self):
        try:
            self.prepare()
            self.execute()
            self.report['status'] = 'passed'
        except Exception as error:
            self.report['status'] = 'failed'
            self.report['error'] = str(error) if isinstance(error, Failure) else type(error).__name__
        finally:
            self.cleanup()
            if self.output.exists():
                private_write(self.output / 'report.json', json.dumps(self.report, sort_keys=True, indent=2).encode())
        print(json.dumps({'status': self.report['status'], 'report': str(self.output / 'report.json')}, sort_keys=True))
        return 0 if self.report['status'] == 'passed' else 1


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--manifest-sha256', required=True)
    parser.add_argument('--mode', choices=('catalog', 'tests', 'recovery', 'bindings', 'full'), required=True)
    args = parser.parse_args()
    require(re.fullmatch(r'[0-9a-f]{64}', args.manifest_sha256), 'Manifest digest is invalid.')
    def interrupted(signum, frame):
        raise Failure('Verification operator was interrupted.')
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    return Runner(args).run_all()


if __name__ == '__main__':
    sys.exit(main())
