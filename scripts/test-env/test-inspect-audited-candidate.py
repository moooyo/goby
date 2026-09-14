#!/usr/bin/env python3
"""Synthetic guards for the fresh inspector; no host, SQL or network operations."""
import copy
import importlib.util
import io
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location('fresh_inspector', Path(__file__).with_name('inspect-audited-candidate.py'))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class Guards(unittest.TestCase):
    def setUp(self):
        self.spawn = patch.object(M.subprocess, 'Popen', side_effect=AssertionError('No real process is permitted.')).start()
        self.network = patch.object(M.http.client, 'HTTPConnection', side_effect=AssertionError('No real HTTP is permitted.')).start()
        self.addCleanup(patch.stopall)

    def fixture(self):
        run = '20260914T083143Z-123456abcdef'
        root = Path('/opt/goby-audited-candidate-' + run)
        hidden = ['/opt/goby-test', '/opt/goby-dev', '/opt/goby-client-m3e', '/opt/goby-fixtures',
                  '/var/lib/goby-test', '/var/lib/postgresql', str(M.OLD_CANDIDATE)]
        protected_names = ['goby-client-m3e.service', 'goby-foundation-test.service', 'postgresql@17-main.service',
            'goby-audited-20260913T073217Z-ef77f9ffcf0b-postgres.service', 'goby-audited-20260913T073217Z-ef77f9ffcf0b-server.service']
        helper = SimpleNamespace(validate_input=Mock(), inaccessible_paths=lambda value: tuple(hidden),
                                 protected_units=lambda value: tuple(protected_names))
        value = {'kind': 'audited-candidate-inspection-input', 'version': 1, 'output': str(M.E3 / 'initial-runtime-inspection-01'),
            'provisionManifest': {'path': str(root / 'private/manifest.json'), 'sha256': '1' * 64},
            'provisionInput': {'path': str(M.E3 / 'private/provision-input.json'), 'sha256': '2' * 64},
            'provisionHelper': {'path': str(M.E3 / 'private/preparation-guards/prepare-audited-candidate.py'), 'sha256': M.PROVISION_SHA},
            'sourceArchive': copy.deepcopy(M.SOURCE_ARCHIVE), 'sourceManifest': copy.deepcopy(M.SOURCE_MANIFEST), 'budgets': dict(M.BUDGETS)}
        provision = {'version': 2, 'runId': run, 'ports': {'http': 28698, 'postgres': 25698}, 'public_url': 'http://127.0.0.1:28696', **copy.deepcopy(M.PRODUCT)}
        manifest = {'kind': 'audited-candidate-private-manifest', 'status': 'running_awaiting_live_acceptance', 'provisionVersion': 2,
            'input': copy.deepcopy(value['provisionInput']), 'runId': run, 'productEvidence': copy.deepcopy(M.PRODUCT),
            'binary': {'path': str(root / 'install/goby'), 'sha256': M.BINARY_SHA}, 'ports': dict(provision['ports']),
            'directUrl': 'http://127.0.0.1:28698', 'publicUrl': provision['public_url'], 'dataDirectory': str(root / 'data'),
            'database': 'goby_candidate_123456abcdef', 'recoveryDatabase': 'goby_recovery_123456abcdef',
            'runtime': {'path': str(root / 'private/runtime.env'), 'sha256': '3' * 64}, 'ordinaryRegressionStatus': 'passed_with_explicit_profile_gap',
            'ordinaryRegressionPhases': 2, 'taggedFullRegressionClaimed': False,
            'dashboard': {'mode': 'embedded', 'buildManifest': copy.deepcopy(M.EMBEDDED_MANIFEST), 'assetCount': 57,
                          'externalDirectoryInstalled': False, 'webDirectoryOverridePresent': False},
            'backupProfile': {'name': 'bounded-fixture-backup-v1', 'environment': dict(M.BACKUP_ENV)},
            'bootstrapExecuted': False, 'recoveryRestoreExecuted': False, 'clientAcceptance': False, 'candidateAdmissionComplete': False,
            'sourceState': {'users': 0, 'schema': 28, 'migrations': 28}, 'processes': {}, 'units': {},
            'inaccessiblePaths': hidden, 'loadedInaccessiblePaths': {role: sorted(hidden) for role in ('server', 'postgres')},
            'clusterSystemIdentifier': '123456789', 'postgresVersionNum': 170011}
        for role, pid, uid, invocation in (('server', 11001, 995, 'a' * 32), ('postgres', 11002, 103, 'b' * 32)):
            name = 'goby-audited-' + run + '-' + role + '.service'
            manifest['units'][role] = {'path': '/run/systemd/system/' + name, 'sha256': '4' * 64}
            manifest['processes'][role] = {'Id': name, 'MainPID': str(pid), 'InvocationID': invocation, 'LoadState': 'loaded',
                'ActiveState': 'active', 'SubState': 'running', 'Result': 'success', 'ExecMainStatus': '0', 'ControlGroup': '/system.slice/' + name}
            manifest['serverIdentity' if role == 'server' else 'postgresIdentity'] = {'pid': pid, 'uid': uid, 'startTicks': '200',
                'bootId': '4de83999-7586-4716-83d1-0d81c9343126', 'invocationId': invocation, 'cgroup': '/system.slice/' + name,
                'exe': str(root / 'install/goby' if role == 'server' else M.PG / 'postgres'), 'executableDevice': 1, 'executableInode': pid}
        manifest['listener'] = {'pid': 11001, 'address': '127.0.0.1', 'port': 28698, 'socketInode': '444'}
        manifest['protectedBaseline'] = {'bootId': manifest['serverIdentity']['bootId'], 'units': {
            name: {**manifest['processes']['server'], 'Id': name} for name in protected_names}}
        return value, manifest, provision, helper, root

    def inspector(self):
        value, manifest, provision, helper, root = self.fixture()
        result = M.CandidateInspection.__new__(M.CandidateInspection)
        result.value, result.manifest, result.provision_input, result.helper, result.root = value, manifest, provision, helper, root
        result.httpport, result.pgport = 28698, 25698
        result.socket = root / 'postgres/socket'
        result.units = {role: row['Id'] for role, row in manifest['processes'].items()}
        result.runtime_context = False
        result.private = Path('/synthetic/private')
        result.output = Path(value['output'])
        result.public_requests = result.http_body_bytes = result.sql_sessions = 0
        result.sql_records, result.sql_raw, result.http_records = [], [], []
        result.command_records, result.command_failure_raw = [], []
        result.active_command = None
        result.helper.group_alive = Mock(return_value=False)
        result.dbus_metadata_calls = result.unit_observations = 0
        result.typed_unit_evidence = []
        result.limits = dict(M.BUDGETS)
        result.remaining = Mock(return_value=180)
        result.save = Mock()
        result.http_routes = {'/admin/', '/healthz'}
        result.owned_tcp = Mock(return_value={'pid': 11001, 'localPort': 28698, 'remotePort': None, 'socketInode': '444'})
        return result

    def test_fresh_manifest_matches_explicit_v2_source_and_process_identity(self):
        value, manifest, provision, helper, root = self.fixture()
        self.assertIs(M.validate_input(value), value)
        self.assertEqual(M.validate_manifest(value, manifest, provision, helper), root)
        helper.validate_input.assert_called_once_with(provision)

    def test_old_candidate_and_unselected_source_refused_without_io(self):
        for field, changed in (('provisionManifest', {'path': str(M.OLD_CANDIDATE / 'private/manifest.json'), 'sha256': '1' * 64}),
                               ('sourceArchive', {'path': '/opt/other.tar', 'sha256': '1' * 64}),
                               ('provisionHelper', {'path': str(M.E3 / 'private/prepare-audited-candidate.py'), 'sha256': '0' * 64})):
            value = self.fixture()[0]; value[field] = changed
            with self.subTest(field=field), self.assertRaises(M.InspectionError):
                M.validate_input(value)
        self.spawn.assert_not_called(); self.network.assert_not_called()

    def test_ordinary_binary_or_wrong_closed_product_cannot_be_admitted(self):
        for field in ('binary', 'review', 'closure', 'tagged', 'external'):
            value, manifest, provision, helper, unused = self.fixture()
            if field == 'binary': manifest['binary']['sha256'] = '9' * 64
            elif field in ('review', 'closure'):
                manifest['productEvidence']['regressionReview' if field == 'review' else 'regressionClosure']['sha256'] = '9' * 64
            elif field == 'tagged': manifest['taggedFullRegressionClaimed'] = True
            else: manifest['dashboard']['externalDirectoryInstalled'] = True
            with self.subTest(field=field), self.assertRaises(M.InspectionError):
                M.validate_manifest(value, manifest, provision, helper)

    def test_process_uid_pid_invocation_and_socket_mismatches_rejected(self):
        for field in ('root', 'same-user', 'same-pid', 'invocation', 'socket'):
            value, manifest, provision, helper, unused = self.fixture()
            if field == 'root': manifest['serverIdentity']['uid'] = 0
            elif field == 'same-user': manifest['serverIdentity']['uid'] = manifest['postgresIdentity']['uid']
            elif field == 'same-pid': manifest['serverIdentity']['pid'] = manifest['postgresIdentity']['pid']
            elif field == 'invocation': manifest['serverIdentity']['invocationId'] = 'f' * 32
            else: manifest['listener']['pid'] = manifest['postgresIdentity']['pid']
            with self.subTest(field=field), self.assertRaises(M.InspectionError):
                M.validate_manifest(value, manifest, provision, helper)

    def test_backup_limits_and_old_candidate_hiding_are_not_optional(self):
        for field in ('caps', 'hiding', 'loaded', 'protected'):
            value, manifest, provision, helper, unused = self.fixture()
            if field == 'caps': manifest['backupProfile']['environment']['GOBY_BACKUP_MAX_OBJECT_BYTES'] = '1'
            elif field == 'hiding': manifest['inaccessiblePaths'] = manifest['inaccessiblePaths'][:-1]
            elif field == 'loaded': manifest['loadedInaccessiblePaths']['postgres'] = []
            else: manifest['protectedBaseline']['units'].pop(next(iter(manifest['protectedBaseline']['units'])))
            with self.subTest(field=field), self.assertRaises(M.InspectionError):
                M.validate_manifest(value, manifest, provision, helper)

    def test_environment_exact_keys_caps_and_no_web_override(self):
        declared = b'GOBY_DATABASE_URL=synthetic-secret\nGOBY_BACKUP_MIN_FREE_BYTES=67108864\n' + b''.join(key.encode() + b'=' + value.encode() + b'\n' for key, value in M.BACKUP_ENV.items())
        actual = declared.replace(b'\n', b'\0') + b'PATH=/safe\0'
        self.assertTrue(M.validate_environment(declared, actual))
        for changed in (actual + b'GOBY_WEB_DIR=/external\0', actual + b'GOBY_WEB_DIR=\0', actual + b'GOBY_UNKNOWN=1\0',
                        actual.replace(b'synthetic-secret', b'wrong-secret'), actual + b'GOBY_DATABASE_URL=duplicate\0'):
            with self.subTest(length=len(changed)), self.assertRaises(M.InspectionError) as error:
                M.validate_environment(declared, changed)
            self.assertNotIn('synthetic-secret', str(error.exception))

    def test_runtime_context_has_separate_exact_budgets(self):
        M.validate_context(False, None, None)
        M.validate_context(True, lambda: None, dict(M.RUNTIME_BUDGETS))
        for mode, callback, budgets in ((True, None, M.RUNTIME_BUDGETS), (True, lambda: None, M.BUDGETS),
                                        (False, lambda: None, None), (True, lambda: None, {**M.RUNTIME_BUDGETS, 'maximumSeconds': 900.0})):
            with self.assertRaises(M.InspectionError):
                M.validate_context(mode, callback, budgets)
        value = self.fixture()[0]; value['budgets']['maximumSeconds'] = 180.0
        with self.assertRaises(M.InspectionError): M.validate_input(value)

    def test_public_headers_reject_cookie_redirect_encoding_and_ambiguous_length(self):
        self.assertEqual(M.response_headers([('Content-Length', '4')])['content-length'], '4')
        for pairs in ([('Set-Cookie', 'synthetic-cookie')], [('Location', '/other')], [('Content-Encoding', 'gzip')],
                      [('Content-Length', '4'), ('content-length', '4')], [('Content-Length', '4'), ('Transfer-Encoding', 'chunked')]):
            with self.subTest(names=[name for name, unused in pairs]), self.assertRaises(M.InspectionError):
                M.response_headers(pairs)

    def public_response(self, body=b'asset', length='5', status=200):
        response = Mock(status=status, length=0)
        response.getheaders.return_value = [('Content-Length', length)]
        response.read1.side_effect = [body, b'']
        response.isclosed.return_value = True
        connection = Mock()
        connection.getresponse.return_value = response
        return connection, response

    def test_public_get_uses_only_new_direct_endpoint_without_credentials(self):
        inspector = self.inspector(); connection, unused = self.public_response()
        with patch.object(M.http.client, 'HTTPConnection', return_value=connection) as factory:
            body, headers, record = inspector.public_get('/admin/', 200, 5)
        factory.assert_called_once_with('127.0.0.1', 28698, timeout=4)
        connection.request.assert_called_once_with('GET', '/admin/', headers={'Accept': '*/*', 'Accept-Encoding': 'identity', 'Connection': 'close'})
        connection.close.assert_called_once()
        self.assertEqual(body, b'asset'); self.assertTrue(record['complete']); self.assertEqual(inspector.public_requests, 1)
        self.assertEqual(inspector.http_body_bytes, 5)

    def test_partial_or_oversized_http_body_cannot_be_complete(self):
        for body, length, maximum in ((b'abc', '5', 5), (b'abcdef', '6', 5)):
            inspector = self.inspector(); connection, response = self.public_response(body, length)
            with patch.object(M.http.client, 'HTTPConnection', return_value=connection), self.assertRaises(M.InspectionError):
                inspector.public_get('/admin/', 200, maximum)
            connection.close.assert_called_once()
            self.assertFalse(inspector.http_records[0]['complete'])

    def test_runtime_context_and_consumed_budget_do_not_send_http(self):
        for change in ('runtime', 'budget', 'route'):
            inspector = self.inspector()
            if change == 'runtime': inspector.runtime_context = True
            if change == 'budget': inspector.public_requests = M.BUDGETS['maximumHttpRequests']
            with self.assertRaises(M.InspectionError):
                inspector.public_get('/admin/v1/bootstrap' if change == 'route' else '/admin/', 200, 5)
        self.network.assert_not_called()

    def test_sql_rejects_old_databases_multiple_statements_and_large_queries_before_io(self):
        inspector = self.inspector(); inspector.owned_unix = Mock(); inspector.command = Mock()
        for database, query in (('goby_test', 'SELECT 1'), (inspector.manifest['database'], 'SELECT 1; SELECT 2'),
                                 (inspector.manifest['database'], 'UPDATE users SET name=1'), (inspector.manifest['database'], 'SELECT ' + 'x' * 65536)):
            with self.assertRaises(M.InspectionError): inspector.sql_json(database, query)
        inspector.owned_unix.assert_not_called(); inspector.command.assert_not_called()

    def test_sql_requires_readonly_cluster_binding_commit_and_backend_exit(self):
        inspector = self.inspector(); inspector.owned_unix = Mock(return_value={'socket': 'synthetic'})
        database = inspector.manifest['database']
        binding = {'readOnly': 'on', 'backendPid': 55001, 'database': database, 'systemIdentifier': inspector.manifest['clusterSystemIdentifier']}
        inspector.command = Mock(return_value=(json.dumps(binding) + '\n{"rows":0}\n').encode())
        pidfile = '11002\n' + str(inspector.root / 'postgres/data') + '\n0\n25698\n'
        with patch.object(Path, 'read_text', return_value=pidfile), patch.object(Path, 'exists', return_value=False), \
             patch.object(M.os.path, 'lexists', return_value=False) as present:
            self.assertEqual(inspector.sql_json(database, 'SELECT json_build_object(\'rows\',0)'), {'rows': 0})
        argv, payload = inspector.command.call_args.args
        self.assertEqual(argv[0:4], ['/usr/sbin/runuser', '-u', 'postgres', '--'])
        self.assertIn(b'BEGIN READ ONLY;', payload); self.assertTrue(payload.endswith(b'COMMIT;\n'))
        environment = inspector.command.call_args.kwargs['environment']
        self.assertIn('default_transaction_read_only=on', environment['PGOPTIONS'])
        self.assertEqual(environment['PGPASSFILE'], str(inspector.socket / '.goby-inspection-no-password'))
        self.assertEqual(present.call_count, 2)
        self.assertEqual(inspector.sql_sessions, 1); self.assertTrue(inspector.sql_records[0]['backendGone'])

    def test_live_sql_backend_is_not_claimed_closed(self):
        inspector = self.inspector(); inspector.owned_unix = Mock(return_value={'socket': 'synthetic'})
        database = inspector.manifest['database']
        inspector.command = Mock(return_value=(json.dumps({'readOnly': 'on', 'backendPid': 55001, 'database': database,
            'systemIdentifier': inspector.manifest['clusterSystemIdentifier']}) + '\n{}\n').encode())
        pidfile = '11002\n' + str(inspector.root / 'postgres/data') + '\n0\n25698\n'
        with patch.object(Path, 'read_text', return_value=pidfile), patch.object(Path, 'exists', return_value=True), \
             patch.object(M.os.path, 'lexists', return_value=False), \
             patch.object(M.time, 'monotonic', side_effect=[0, 4]), self.assertRaises(M.InspectionError):
            inspector.sql_json(database, 'SELECT 1')
        self.assertEqual(inspector.sql_records, [])
        inspector.command.assert_called_once()

    def test_existing_password_path_including_dangling_link_is_rejected_before_dispatch(self):
        inspector = self.inspector(); inspector.owned_unix = Mock(return_value={'socket': 'synthetic'})
        inspector.command = Mock()
        with patch.object(M.os.path, 'lexists', return_value=True) as present, \
             patch.object(Path, 'read_text') as read, self.assertRaisesRegex(M.InspectionError, '^sql_password_file_present$'):
            inspector.sql_json(inspector.manifest['database'], 'SELECT 1')
        inspector.owned_unix.assert_called_once()
        present.assert_called_once_with(inspector.socket / '.goby-inspection-no-password')
        read.assert_not_called(); inspector.command.assert_not_called()
        self.assertEqual(inspector.sql_sessions, 0)
        self.spawn.assert_not_called()

    def test_password_path_created_during_sql_cannot_claim_success(self):
        inspector = self.inspector(); inspector.owned_unix = Mock(return_value={'socket': 'synthetic'})
        database = inspector.manifest['database']
        binding = {'readOnly': 'on', 'backendPid': 55001, 'database': database, 'systemIdentifier': inspector.manifest['clusterSystemIdentifier']}
        inspector.command = Mock(return_value=(json.dumps(binding) + '\n{}\n').encode())
        pidfile = '11002\n' + str(inspector.root / 'postgres/data') + '\n0\n25698\n'
        with patch.object(Path, 'read_text', return_value=pidfile), patch.object(Path, 'exists', return_value=False), \
             patch.object(M.os.path, 'lexists', side_effect=[False, True]), self.assertRaisesRegex(M.InspectionError, '^sql_password_file_present$'):
            inspector.sql_json(database, 'SELECT 1')
        self.assertEqual(inspector.sql_sessions, 1); self.assertEqual(inspector.sql_records, [])

    def synthetic_command(self, inspector, stdout=b'synthetic-private-stdout', stderr=b'synthetic-private-warning', exit_code=0, maximum=65536):
        """Drive the real command loop with fake descriptors, never real pipes or processes."""
        class Selector:
            def __init__(self): self.keys = {}
            def register(self, stream, events, data):
                self.keys[stream.fileno()] = (SimpleNamespace(fd=stream.fileno(), fileobj=stream, data=data), events)
            def unregister(self, stream): del self.keys[stream.fileno()]
            def get_map(self): return self.keys
            def select(self, timeout): return list(self.keys.values())
            def close(self): self.keys.clear()
        streams = [SimpleNamespace(fileno=lambda number=number: number, close=Mock()) for number in (31, 32, 33)]
        process = SimpleNamespace(pid=44001, returncode=exit_code, stdin=streams[0], stdout=streams[1], stderr=streams[2],
                                  poll=Mock(return_value=exit_code), wait=Mock(return_value=exit_code))
        chunks = {32: [stdout, b''] if stdout else [b''], 33: [stderr, b''] if stderr else [b'']}
        def read(number, size):
            block = chunks[number].pop(0)
            if len(block) > size:
                chunks[number].insert(0, block[size:]); block = block[:size]
            return block
        with patch.object(M.subprocess, 'Popen', return_value=process), patch.object(M.selectors, 'DefaultSelector', Selector), \
             patch.object(M.os, 'set_blocking'), patch.object(M.os, 'read', side_effect=read), \
             patch.object(M.os, 'write', side_effect=lambda fd, block: len(block)):
            return inspector.command(['/synthetic/psql'], b'SELECT synthetic-private-value;', maximum=maximum)

    def test_exit_zero_warning_retains_complete_private_failure_and_closed_group(self):
        inspector = self.inspector()
        inspector.save = Mock(side_effect=lambda name, value, raw=False: {'path': str(inspector.private / name)})
        with self.assertRaisesRegex(M.InspectionError, '^read_command_failed$'):
            self.synthetic_command(inspector)
        record = inspector.command_records[0]
        self.assertEqual(record['exitCode'], 0); self.assertTrue(record['frontendGroupClosed'])
        self.assertTrue(record['processCreated']); self.assertEqual(record['pid'], record['processGroup'])
        self.assertTrue(record['stdinComplete'])
        self.assertTrue(record['stdout']['complete']); self.assertTrue(record['stderr']['complete'])
        self.assertEqual(record['stdout']['sha256'], M.sha(b'synthetic-private-stdout'))
        self.assertEqual(record['stderr']['bytes'], len(b'synthetic-private-warning'))
        self.assertEqual(record['retentionErrors'], []); self.assertIsNone(inspector.active_command)
        self.assertEqual([call.args[0] for call in inspector.save.call_args_list],
                         ['command-0001-stdout.raw', 'command-0001-stderr.raw', 'command-0001-result.json'])
        self.assertEqual(inspector.command_failure_raw[0]['stderr'], b'synthetic-private-warning')
        self.assertNotIn('synthetic-private', json.dumps(record))

    def test_failed_command_evidence_write_failure_never_replaces_original_failure(self):
        inspector = self.inspector(); inspector.save = Mock(side_effect=OSError('synthetic-private-disk-error'))
        with self.assertRaisesRegex(M.InspectionError, '^read_command_failed$'):
            self.synthetic_command(inspector)
        record = inspector.command_records[0]
        self.assertEqual([row['part'] for row in record['retentionErrors']], ['stdout', 'stderr', 'result'])
        self.assertTrue(record['frontendGroupClosed']); self.assertEqual(len(inspector.command_failure_raw), 1)
        self.assertNotIn('synthetic-private', json.dumps(record))

    def test_nonzero_exit_and_output_bound_keep_failure_evidence_without_claiming_eof(self):
        for exit_code, maximum, expected, complete in ((7, 65536, 'read_command_failed', True),
                                                      (0, 3, 'read_command_output_limit', False)):
            inspector = self.inspector(); inspector.private = None
            with self.subTest(code=exit_code, maximum=maximum), self.assertRaisesRegex(M.InspectionError, '^' + expected + '$'):
                self.synthetic_command(inspector, stdout=b'private-value', stderr=b'', exit_code=exit_code, maximum=maximum)
            record = inspector.command_records[0]
            self.assertEqual(record['exitCode'], exit_code); self.assertEqual(record['stdout']['complete'], complete)
            self.assertTrue(record['frontendGroupClosed']); inspector.save.assert_not_called()
            self.assertEqual(len(inspector.command_failure_raw), 1)

    def test_launch_or_cleanup_failure_retains_metadata_without_secret_exception_text(self):
        inspector = self.inspector(); inspector.private = None
        with patch.object(M.subprocess, 'Popen', side_effect=OSError('synthetic-private-launch')), self.assertRaises(OSError):
            inspector.command(['/synthetic/psql'])
        record = inspector.command_records[0]
        self.assertFalse(record['processCreated']); self.assertIsNone(record['pid']); self.assertIsNone(record['exitCode'])
        self.assertTrue(record['frontendGroupClosed']); self.assertNotIn('synthetic-private', json.dumps(record))
        inspector = self.inspector(); inspector.private = None
        inspector.helper.group_alive = Mock(side_effect=[False, True])
        inspector.stop_command = Mock(side_effect=M.InspectionError('read_command_group_unclosed'))
        with self.assertRaisesRegex(M.InspectionError, '^read_command_failed$'):
            self.synthetic_command(inspector)
        record = inspector.command_records[0]
        self.assertFalse(record['frontendGroupClosed']); self.assertIsNotNone(inspector.active_command)
        self.assertEqual(record['failure']['code'], 'read_command_failed')
        self.assertEqual(record['cleanupFailure']['code'], 'read_command_cleanup_failed')
        inspector.stop_command.assert_called_once()

    def test_command_deadline_preserves_unread_output_state_in_runtime_memory_only(self):
        inspector = self.inspector(); inspector.private = None; inspector.runtime_context = True
        inspector.remaining = Mock(side_effect=[180, M.InspectionError('inspection_deadline')])
        with self.assertRaisesRegex(M.InspectionError, '^inspection_deadline$'):
            self.synthetic_command(inspector)
        record = inspector.command_records[0]
        self.assertTrue(record['processCreated']); self.assertTrue(record['frontendGroupClosed'])
        self.assertFalse(record['stdout']['complete']); self.assertFalse(record['stderr']['complete'])
        self.assertFalse(record['stdinComplete']); self.assertEqual(len(inspector.command_failure_raw), 1)
        inspector.save.assert_not_called(); self.assertIsNone(inspector.active_command)

    def test_foreign_tcp_inode_cannot_match_pinned_listener(self):
        for linked, accepted in (('444', True), ('999', False)):
            inspector = self.inspector()
            inspector.owned_tcp = M.CandidateInspection.owned_tcp.__get__(inspector)
            inspector.process = Mock()
            inspector.descriptor_links = Mock(return_value=(Path('/proc/11001'), {'socket:[' + linked + ']'}))
            raw = b'header\n0: 0100007F:701A 00000000:0000 0A 0:0 00:0 0 995 0 444 1\n'
            with patch.object(Path, 'read_bytes', return_value=raw):
                if accepted: self.assertEqual(inspector.owned_tcp('server', 28698)['socketInode'], '444')
                else:
                    with self.assertRaises(M.InspectionError): inspector.owned_tcp('server', 28698)

    def test_initial_output_cannot_be_reused(self):
        inspector = self.inspector(); inspector.private = None
        with patch.object(M.os.path, 'lexists', return_value=True), patch.object(Path, 'mkdir') as mkdir, self.assertRaises(M.InspectionError):
            inspector.run_initial()
        mkdir.assert_not_called()

    def metadata_failure(self, original):
        return {'kind': 'audited-candidate-runtime-inspection', 'version': 3, 'status': 'inspection_failed',
            'stage': 'runtime_configuration', 'failure': {'type': 'InspectionError', 'code': 'unit_process_changed'},
            'input': dict(M.PRIOR_INPUT), 'helper': dict(M.PRIOR_HELPER),
            **{key: original[key] for key in ('provisionManifest', 'provisionInput', 'provisionHelper')},
            'sourceEvidence': copy.deepcopy(M.PRODUCT), 'readProcessesClosed': True, 'sqlReadResults': [], 'httpResponses': [],
            'counters': {'readOnlySqlSessions': 0, 'publicHttpRequests': 0, 'httpBodyBytes': 0, 'unitMetadataCalls': 2},
            'databaseWrites': 0, 'bootstrapPerformed': False, 'restorePerformed': False, 'serviceChangesPerformed': False,
            'candidateAdmissionComplete': False, 'clientAcceptance': False}

    def test_only_fixed_zero_io_metadata_failure_admits_corrected_input(self):
        original = self.fixture()[0]
        value = {**copy.deepcopy(original), 'version': 2, 'output': str(M.E3 / 'initial-runtime-inspection-02'),
                 'priorMetadataFailure': dict(M.PRIOR_METADATA_FAILURE)}
        prior = self.metadata_failure(original)
        M.validate_input(value)
        M.validate_metadata_correction(value, prior, original)
        for changed in ('sql', 'http', 'unclosed', 'failure', 'helper', 'different-input', 'third-output'):
            v, p, old = copy.deepcopy(value), copy.deepcopy(prior), copy.deepcopy(original)
            if changed == 'sql': p['counters']['readOnlySqlSessions'] = 1
            elif changed == 'http': p['counters']['publicHttpRequests'] = 1
            elif changed == 'unclosed': p['readProcessesClosed'] = False
            elif changed == 'failure': p['failure']['code'] = 'other_failure'
            elif changed == 'helper': p['helper']['sha256'] = '0' * 64
            elif changed == 'different-input': old['provisionManifest']['sha256'] = '0' * 64
            else: v['output'] = str(M.E3 / 'initial-runtime-inspection-03')
            with self.subTest(changed=changed), self.assertRaises(M.InspectionError):
                M.validate_metadata_correction(v, p, old)

    def sql_correction_fixture(self):
        original = self.fixture()[0]
        old = {**copy.deepcopy(original), 'version': 2, 'output': str(M.E3 / 'initial-runtime-inspection-02'),
               'priorMetadataFailure': dict(M.PRIOR_METADATA_FAILURE)}
        value = {**copy.deepcopy(old), 'version': 3, 'output': str(M.E3 / 'initial-runtime-inspection-sql-corrected'),
                 'priorSqlFailure': dict(M.PRIOR_SQL_FAILURE), 'sqlFailureDiagnostic': dict(M.SQL_FAILURE_DIAGNOSTIC)}
        prior = self.metadata_failure(original)
        prior.update(stage='database_identity_and_lease', failure={'type': 'InspectionError', 'code': 'read_command_failed'},
            input={'path': str(M.E3 / 'private/synthetic-inspection-input-02.json'), 'sha256': '8' * 64},
            helper=dict(M.FAILED_SQL_HELPER), readProcessesClosed=False, priorMetadataFailure=dict(M.PRIOR_METADATA_FAILURE),
            metadataCorrectionReview=dict(M.METADATA_CORRECTION_REVIEW))
        prior['counters']['readOnlySqlSessions'] = 1
        stdout = {'path': str(M.E3 / 'private/first-sql-diagnostic/psql-once.stdout'), 'sha256': '9' * 64, 'bytes': 300}
        stderr = {'path': str(M.E3 / 'private/first-sql-diagnostic/psql-once.stderr'), 'sha256': M.SQL_WARNING_SHA, 'bytes': 55}
        diagnostic = {'kind': 'first-candidate-sql-diagnostic', 'version': 1, 'status': 'diagnostic_complete',
            'classification': 'libpq_nonregular_password_file_warning', 'readOnly': True, 'originalInspectorWouldReject': True,
            'frontendGroupClosed': True, 'backendGone': True, 'postmasterUnchanged': True, 'sqlCommands': 1,
            'httpRequests': 0, 'serviceChanges': 0, 'frontendExitCode': 0, 'stderrBytes': 55, 'otherPostgresPsqlBackendCount': 0,
            'otherPostgresPsqlBackends': [], 'backendPid': 55001, 'stdout': stdout, 'stderr': stderr,
            'commands': [{'label': label, 'groupClosed': True, 'exitCode': 0, 'timeoutOrFailure': None,
                          'stdout': stdout, 'stderr': stderr} for label in ('pg-before', 'psql-once', 'pg-after')]}
        return value, prior, old, diagnostic

    def test_only_fixed_sql_failure_and_closed_diagnostic_admit_named_continuation(self):
        value, prior, old, diagnostic = self.sql_correction_fixture()
        M.validate_input(value); M.validate_sql_correction(value, prior, old, diagnostic)
        self.assertFalse(prior['readProcessesClosed']); self.assertEqual(prior['sqlReadResults'], [])
        self.assertTrue(M.SQL_CORRECTION['priorFailurePreserved'])
        self.assertFalse(M.SQL_CORRECTION['priorSqlResultRecovered'])
        for changed in ('input1', 'input2', 'version-bool', 'version4', 'new-output', 'new-helper', 'different-common', 'other-failure-pin', 'other-diagnostic-pin'):
            v, p, o, d = copy.deepcopy((value, prior, old, diagnostic))
            if changed == 'input1': v = self.fixture()[0]
            elif changed == 'input2': v = o
            elif changed == 'version-bool': v['version'] = True
            elif changed == 'version4': v['version'] = 4
            elif changed == 'new-output': v['output'] = str(M.E3 / 'initial-runtime-inspection-03')
            elif changed == 'new-helper': p['helper']['sha256'] = '0' * 64
            elif changed == 'different-common': o['provisionManifest']['sha256'] = '0' * 64
            elif changed == 'other-failure-pin': v['priorSqlFailure']['sha256'] = '0' * 64
            else: v['sqlFailureDiagnostic']['sha256'] = '0' * 64
            with self.subTest(changed=changed), self.assertRaises(M.InspectionError):
                M.validate_sql_correction(v, p, o, d)
        self.spawn.assert_not_called(); self.network.assert_not_called()

    def test_sql_failure_cannot_be_reclassified_or_have_prior_io_hidden(self):
        for changed in ('success', 'closed', 'recovered-result', 'second-sql', 'sql-bool', 'http', 'wrong-stage', 'wrong-failure', 'business'):
            value, prior, old, diagnostic = self.sql_correction_fixture()
            if changed == 'success': prior['status'] = 'ready_pending_seed'
            elif changed == 'closed': prior['readProcessesClosed'] = True
            elif changed == 'recovered-result': prior['sqlReadResults'] = [{'readOnly': True}]
            elif changed == 'second-sql': prior['counters']['readOnlySqlSessions'] = 2
            elif changed == 'sql-bool': prior['counters']['readOnlySqlSessions'] = True
            elif changed == 'http': prior['counters']['publicHttpRequests'] = 1
            elif changed == 'wrong-stage': prior['stage'] = 'runtime_configuration'
            elif changed == 'wrong-failure': prior['failure']['code'] = 'other_failure'
            else: prior['bootstrapPerformed'] = True
            with self.subTest(changed=changed), self.assertRaises(M.InspectionError):
                M.validate_sql_correction(value, prior, old, diagnostic)

    def test_diagnostic_warning_and_all_command_closures_are_required(self):
        for changed in ('version', 'status', 'warning', 'warning-sha', 'stderr-bytes', 'other-backend', 'backend', 'frontend',
                        'postmaster', 'http', 'sql', 'read-only', 'exit', 'command-closure', 'command-missing', 'stdout-binding'):
            value, prior, old, diagnostic = self.sql_correction_fixture()
            if changed == 'version': diagnostic['version'] = True
            elif changed == 'status': diagnostic['status'] = 'failed'
            elif changed == 'warning': diagnostic['classification'] = 'nonempty_stderr_with_successful_sql'
            elif changed == 'warning-sha': diagnostic['stderr']['sha256'] = '0' * 64
            elif changed == 'stderr-bytes': diagnostic['stderrBytes'] = 54
            elif changed == 'other-backend': diagnostic['otherPostgresPsqlBackends'] = [{'pid': 123}]
            elif changed == 'backend': diagnostic['backendGone'] = False
            elif changed == 'frontend': diagnostic['frontendGroupClosed'] = False
            elif changed == 'postmaster': diagnostic['postmasterUnchanged'] = False
            elif changed == 'http': diagnostic['httpRequests'] = 1
            elif changed == 'sql': diagnostic['sqlCommands'] = True
            elif changed == 'read-only': diagnostic['readOnly'] = False
            elif changed == 'exit': diagnostic['frontendExitCode'] = 1
            elif changed == 'command-closure': diagnostic['commands'][0]['groupClosed'] = False
            elif changed == 'command-missing': diagnostic['commands'].pop()
            else: diagnostic['commands'][1]['stdout'] = {'sha256': '0' * 64}
            with self.subTest(changed=changed), self.assertRaises(M.InspectionError):
                M.validate_sql_correction(value, prior, old, diagnostic)

    def test_actual_postgres_unit_object_encoding(self):
        unit = 'goby-audited-20260914T083143Z-9b73ad46f2e6-postgres.service'
        self.assertEqual(M.postgres_unit_object(unit), '/org/freedesktop/systemd1/unit/goby_2daudited_2d20260914T083143Z_2d9b73ad46f2e6_2dpostgres_2eservice')
        with self.assertRaises(M.InspectionError):
            M.postgres_unit_object(unit.replace('-postgres.service', '-server.service'))

    def dbus_fixture(self):
        inspector = self.inspector()
        core = dict(inspector.manifest['processes']['postgres'])
        fields = M.UNIT_FIELDS | {'EnvironmentFiles', 'Type'}
        missing = {**core, 'Type': 'exec'}
        obj = M.postgres_unit_object(inspector.units['postgres'])
        replies = [('o ' + json.dumps(obj) + '\n').encode(),
                   ('s ' + json.dumps(inspector.units['postgres']) + '\n').encode(), b'a(sb) 0\n',
                   ''.join(key + '=' + value + '\n' for key, value in core.items()).encode()]
        inspector.command = Mock(side_effect=replies)
        return inspector, fields, missing, obj, replies

    def test_missing_pg_environment_files_requires_bound_typed_empty_array(self):
        inspector, fields, missing, obj, unused = self.dbus_fixture()
        result = inspector.complete_unit_metadata('postgres', fields, missing)
        self.assertEqual(result, {**missing, 'EnvironmentFiles': ''})
        calls = inspector.command.call_args_list
        self.assertEqual(len(calls), 4)
        self.assertEqual(calls[0].args[0][-3:], ['GetUnit', 's', inspector.units['postgres']])
        self.assertEqual(calls[1].args[0][-3:], [obj, 'org.freedesktop.systemd1.Unit', 'Id'])
        self.assertEqual(calls[2].args[0][-3:], [obj, 'org.freedesktop.systemd1.Service', 'EnvironmentFiles'])
        self.assertEqual(inspector.dbus_metadata_calls, 3)
        self.assertTrue(inspector.typed_unit_evidence[0]['typedEmptyVerified'])
        self.assertEqual(inspector.typed_unit_evidence[0]['signature'], 'a(sb)')
        self.assertEqual(inspector.typed_unit_evidence[0]['elementCount'], 0)
        self.assertEqual(inspector.sql_sessions, 0); self.assertEqual(inspector.public_requests, 0)

    def test_other_missing_fields_server_or_nonempty_pg_values_never_get_defaulted(self):
        for changed in ('other-missing', 'server-missing', 'nonempty-present', 'changed-pid'):
            inspector, fields, unit, unused, unused2 = self.dbus_fixture()
            role = 'postgres'
            if changed == 'other-missing': del unit['Type']
            elif changed == 'server-missing':
                role = 'server'; unit = {**inspector.manifest['processes']['server'], 'Type': 'exec'}
            elif changed == 'nonempty-present': unit['EnvironmentFiles'] = '/synthetic.env (ignore_errors=no)'
            else: unit['MainPID'] = '99999'
            with self.subTest(changed=changed), self.assertRaises(M.InspectionError):
                inspector.complete_unit_metadata(role, fields, unit)
            inspector.command.assert_not_called()

    def test_foreign_object_id_nonempty_or_untyped_property_and_changed_process_reject(self):
        for changed, count in (('foreign-object', 1), ('untyped-object', 1), ('wrong-id', 2),
                               ('nonempty', 3), ('wrong-signature', 3), ('changed-process', 4)):
            inspector, fields, missing, obj, replies = self.dbus_fixture()
            if changed == 'foreign-object': replies[0] = b'o "/org/freedesktop/systemd1/unit/foreign"\n'
            elif changed == 'untyped-object': replies[0] = ('s ' + json.dumps(obj) + '\n').encode()
            elif changed == 'wrong-id': replies[1] = b's "foreign.service"\n'
            elif changed == 'nonempty': replies[2] = b'a(sb) 1 "/synthetic.env" false\n'
            elif changed == 'wrong-signature': replies[2] = b'as 0\n'
            else: replies[3] = replies[3].replace(b'MainPID=11002', b'MainPID=99999')
            inspector.command = Mock(side_effect=replies)
            with self.subTest(changed=changed), self.assertRaises(M.InspectionError):
                inspector.complete_unit_metadata('postgres', fields, missing)
            self.assertEqual(inspector.command.call_count, count)
            self.assertEqual(inspector.typed_unit_evidence, [])


if __name__ == '__main__':
    unittest.main()
