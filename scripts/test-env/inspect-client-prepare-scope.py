#!/usr/bin/env python3
"""Observe the two fixed candidate users before or after scoped movie preparation.

Run only through authorized root SSH on test-env. PostgreSQL access is one
READ ONLY transaction against port15432/goby_client_m3e. No HTTP, lifecycle,
playback, schema, or data mutation is available. Optional reports retain exact
PostgreSQL JSON bytes in a newly owned root0700 directory with root0600 files.
Only counts and hashes reach stdout; authentication rows contain token hashes,
never raw tokens, passwords, application-key ciphertext, or client capabilities.
"""

from __future__ import annotations

import argparse
from decimal import Decimal
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import types

WORK = Path('/opt/goby-test/exec-work-m3e')
MARKER = 'goby-client-prepare-scope-observation-v1'
ACTORS = {'A': '34b4c24f6568659af7ce17938fae7f81', 'B': 'ecbbe4cb82403879bc4b4f78894c5738'}
MOVIE = '268051d3ca734aefcf94e245fb25ad55'
MAX_REPORT = 16 << 20
SCHEMA27_SCOPE = 'schema27-original-movie-01'
SCHEMA27_LEDGER = 'goby-client-schema27-prepare-ledger-v1'
SCHEMA27_ORIGIN_SHA = '0c005aeb0a2ea4a0b8362650a7e62102f1b938828be7c0cbe735ea92d3cf279c'
SCHEMA27_OPERATOR_SHA = '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'
SCHEMA27_CATALOG_SHA = '1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d'
SCHEMA27_MIGRATION_SHA = 'b62d0422dddb9e258f46589898f672b08fc6e4c12fea7456059f855a6600353c'
SCHEMA27_COUNTS = {'play_sessions': 22, 'client_playback_references': 0, 'user_item_data': 5, 'sessions': 51, 'encoding_jobs': 0}
CONNECTION = ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/psql',
              '-X', '--no-password', '-qAt', '-v', 'ON_ERROR_STOP=1',
              '-h', '/var/lib/postgresql/goby-workspace-v1/socket', '-p', '15432', '-d', 'goby_client_m3e']

SQL = r"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='10s';
SET LOCAL lock_timeout='2s';
SET LOCAL search_path=pg_catalog,public;
WITH target(label,id) AS (VALUES
 ('A','34b4c24f6568659af7ce17938fae7f81'),('B','ecbbe4cb82403879bc4b4f78894c5738')),
plays AS (SELECT p.* FROM public.play_sessions p JOIN target t ON t.id=p.user_id),
refs AS (SELECT r.* FROM public.client_playback_references r JOIN target t ON t.id=r.user_id),
userdata AS (SELECT d.* FROM public.user_item_data d JOIN target t ON t.id=d.user_id),
encodings AS (SELECT e.* FROM public.encoding_jobs e JOIN target t ON t.id=e.user_id),
auth AS (SELECT s.id,s.user_id,s.kind,s.device_id,s.created_at,s.last_seen_at,s.expires_at,s.revoked_at,
 encode(s.token_hash,'hex') AS token_fingerprint_sha256
 FROM public.sessions s JOIN target t ON t.id=s.user_id),
counts AS (SELECT (SELECT count(*) FROM plays) AS plays,(SELECT count(*) FROM refs) AS refs,
 (SELECT count(*) FROM userdata) AS userdata,(SELECT count(*) FROM auth) AS auth,
 (SELECT count(*) FROM encodings) AS encodings),
bounded AS (SELECT *,plays<=512 AND refs<=512 AND userdata<=512 AND auth<=2048 AND encodings<=512 AS complete FROM counts)
SELECT jsonb_build_object(
 'marker','goby-client-prepare-scope-observation-v1','version',1,'phase','__PHASE__',
 'observed_at',transaction_timestamp(),'complete',b.complete,
 'identity',jsonb_build_object('database',current_database(),'port',current_setting('port')::int,
  'readonly',current_setting('transaction_read_only'),'postgresql',current_setting('server_version_num')::int,
  'schema',(SELECT max(version) FROM public.schema_migrations),
  'system_identifier',(SELECT system_identifier::text FROM pg_control_system())),
 'counts',jsonb_build_object('play_sessions',b.plays,'client_playback_references',b.refs,
  'user_item_data',b.userdata,'sessions',b.auth,'encoding_jobs',b.encodings),
 'actors',(SELECT jsonb_agg(jsonb_build_object('label',t.label,'user_id',t.id,'exists',u.id IS NOT NULL,
  'disabled',u.is_disabled,'administrator',u.is_administrator,
  'all_folders',u.is_administrator OR NOT(u.policy?'EnableAllFolders') OR u.policy->'EnableAllFolders'='true'::jsonb,
  'playback_allowed',NOT(u.policy?'EnableMediaPlayback') OR u.policy->'EnableMediaPlayback'='true'::jsonb,
  'policy_sha256',encode(sha256(convert_to(u.policy::text,'UTF8')),'hex'),
  'movie_userdata_exists',EXISTS(SELECT 1 FROM userdata d WHERE d.user_id=t.id AND d.item_id='268051d3ca734aefcf94e245fb25ad55'))
  ORDER BY t.label) FROM target t LEFT JOIN public.users u ON u.id=t.id),
 'movie',(SELECT jsonb_build_object('id','268051d3ca734aefcf94e245fb25ad55','exists',count(*)=1,
  'type',min(i.type),'folder',bool_or(i.is_folder),'has_media',bool_and(i.media IS NOT NULL),
  'has_theme_association',bool_or(EXISTS(SELECT 1 FROM public.item_theme_resources r WHERE r.resource_item_id=i.id)),
  'reserved',bool_or(EXISTS(SELECT 1 FROM public.theme_reserved_paths m WHERE m.root_id=i.root_id
   AND (i.relative_path COLLATE "C"=m.relative_path OR
    (m.is_directory AND left(i.relative_path,length(m.relative_path)+1) COLLATE "C"=m.relative_path||'/')))))
  FROM public.items i WHERE i.id='268051d3ca734aefcf94e245fb25ad55'),
 'foreign_dependents',jsonb_build_object(
  'references',(SELECT count(*) FROM public.client_playback_references r JOIN plays p ON p.id=r.play_session_id
    WHERE NOT EXISTS(SELECT 1 FROM target t WHERE t.id=r.user_id)),
  'encodings',(SELECT count(*) FROM public.encoding_jobs e JOIN plays p ON p.id=e.play_session_id
    WHERE NOT EXISTS(SELECT 1 FROM target t WHERE t.id=e.user_id))),
 'play_sessions',CASE WHEN b.complete THEN (SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY p.user_id,p.id),'[]'::jsonb) FROM plays p) ELSE NULL END,
 'client_playback_references',CASE WHEN b.complete THEN (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.user_id,r.auth_session_id,r.device_id,r.client_nonce),'[]'::jsonb) FROM refs r) ELSE NULL END,
 'user_item_data',CASE WHEN b.complete THEN (SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY d.user_id,d.item_id),'[]'::jsonb) FROM userdata d) ELSE NULL END,
 'sessions',CASE WHEN b.complete THEN (SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY a.user_id,a.id),'[]'::jsonb) FROM auth a) ELSE NULL END,
 'encoding_jobs',CASE WHEN b.complete THEN (SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.user_id,e.id),'[]'::jsonb) FROM encodings e) ELSE NULL END
) FROM bounded b;
ROLLBACK;
"""

SQL27 = SQL.replace(" 'reserved',bool_or", """ 'has_extra_association',bool_or(EXISTS(SELECT 1 FROM public.item_extra_resources r WHERE r.resource_item_id=i.id)),
 'extra_reserved',bool_or(EXISTS(SELECT 1 FROM public.extra_reserved_paths m WHERE m.root_id=i.root_id
  AND (i.relative_path COLLATE "C"=m.relative_path OR
   (m.is_directory AND left(i.relative_path,length(m.relative_path)+1) COLLATE "C"=m.relative_path||'/')))),
 'reserved',bool_or""").replace("'schema',(SELECT max(version)", """'database_oid',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),
  'role_oid',(SELECT oid::bigint FROM pg_roles WHERE rolname='goby_client_m3e'),
  'database_owner_oid',(SELECT datdba::bigint FROM pg_database WHERE datname=current_database()),
  'schema',(SELECT max(version)""")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def exact(value):
    if value is None:
        return 'null'
    if type(value) is bool:
        return 'true' if value else 'false'
    if type(value) is int or isinstance(value, Decimal):
        return str(value)
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list):
        return '[' + ','.join(exact(item) for item in value) + ']'
    require(isinstance(value, dict), 'invalid_exact_json')
    return '{' + ','.join(exact(key) + ':' + exact(value[key]) for key in sorted(value)) + '}'


def read_record(record, *, modes=(0o600,)):
    require(isinstance(record, dict) and set(record) == {'path', 'sha256'} and
            re.fullmatch(r'[0-9a-f]{64}', record.get('sha256', '')), 'invalid_record_pin')
    path = Path(record['path'])
    require(path.is_absolute() and WORK in path.parents and '..' not in path.parts, 'record_path_escape')
    private_directory(path.parent)
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and info.st_nlink == 1 and
            stat.S_IMODE(info.st_mode) in modes and info.st_size <= MAX_REPORT, 'record_file_identity_changed')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        require((opened.st_dev, opened.st_ino) == (info.st_dev, info.st_ino), 'record_open_identity_changed')
        raw = stream.read(MAX_REPORT + 1)
    require(len(raw) == info.st_size and hashlib.sha256(raw).hexdigest() == record['sha256'], 'record_digest_changed')
    return raw


def json_record(record):
    return json.loads(read_record(record), parse_float=Decimal)


def schema27_expiry(origin):
    require(origin.get('marker') == MARKER and origin.get('phase') == 'after' and origin.get('complete') is True and
            origin.get('identity', {}).get('schema') == 26 and origin.get('counts') == SCHEMA27_COUNTS and
            all(isinstance(origin.get(name), list) and len(origin[name]) == count for name, count in SCHEMA27_COUNTS.items()),
            'invalid_schema26_ledger_origin')
    auth = {row['id']: row for row in origin['sessions']}
    prepared = [row for row in origin['play_sessions'] if row['state'] in ('Prepared', 'Playing', 'Paused')]
    require(len(prepared) == 2 and {row['user_id'] for row in prepared} == set(ACTORS.values()) and
            all(row['state'] == 'Prepared' and row['item_id'] == MOVIE and row['media_source_id'] == 'mediasource_' + MOVIE and
                row['started_at'] is None and row['stopped_at'] is None and
                auth[row['auth_session_id']]['user_id'] == row['user_id'] and auth[row['auth_session_id']]['revoked_at'] is not None
                for row in prepared), 'unreviewed_preparation_cleanup_origin')
    return {slot: hashlib.sha256(next(row['id'] for row in prepared if row['user_id'] == identifier).encode()).hexdigest()
            for slot, identifier in ACTORS.items()}


def schema27_receipts(ledger, state, completed, report, origin):
    require(set(ledger) == {'marker', 'version', 'scope', 'origin_after', 'fixture_state', 'completed', 'fixture_report', 'operator_proof'} and
            ledger['marker'] == SCHEMA27_LEDGER and type(ledger['version']) is int and ledger['version'] == 1 and
            ledger['scope'] == SCHEMA27_SCOPE and ledger['origin_after']['sha256'] == SCHEMA27_ORIGIN_SHA and
            ledger['fixture_state']['path'] == str(WORK / 'client-fixture.json') and
            ledger['operator_proof'] == {'path': str(WORK / 'client-schema27-tool-01/prepare-client-fixture.py'), 'sha256': SCHEMA27_OPERATOR_SHA},
            'invalid_schema27_expected_ledger')
    require(state.get('marker') == 'goby-m3e-client-acceptance-v1' and state.get('phase') == 'ready' and state.get('stage') == 'complete' and
            type(state.get('schema')) is int and state['schema'] == 27 and state.get('process') and
            state.get('viewer_id') == ACTORS['B'] and state.get('added_viewer', {}).get('user_id') == ACTORS['A'] and
            state['added_viewer'].get('phase') == 'complete', 'schema27_fixture_not_ready')
    require(state.get('work') == str(WORK) and state.get('cluster') == {'data': '/var/lib/postgresql/goby-workspace-v1/data',
            'port': 15432, 'system_identifier': '7684040109719526738'} and
            all(type(state.get(name)) is int and state[name] > 0 for name in ('database_oid', 'role_oid')),
            'schema27_fixture_cluster_mismatch')
    require(exact(state.get('upgrade')) == exact(completed) and completed.get('phase') == 'complete' and
            completed.get('from_schema') == 26 and completed.get('to_schema') == 27 and
            completed.get('to_sha256') == completed.get('installed_binary_sha256') == state.get('binary_sha256') and
            completed.get('new_process') == state['process'] and completed.get('old_process') != state['process'] and
            ledger['completed']['path'] == str(WORK / ('client-upgrade-' + completed['id']) / 'completed.json'),
            'schema27_completion_mismatch')
    artifacts = completed['schema_artifacts']
    binding = state.get('schema27_source')
    require(isinstance(binding, dict) and binding == artifacts.get('schema27_binding') and binding.get('schema') == 27 and
            binding.get('marker') == 'goby-client-schema27-source-m3e-v1' and binding.get('catalog_sha256') == SCHEMA27_CATALOG_SHA and
            binding.get('migration_27_sha256') == SCHEMA27_MIGRATION_SHA and artifacts.get('migration_count') == 27 and
            artifacts.get('source') == binding.get('source') and artifacts.get('source_manifest_sha256') == binding.get('source_manifest_sha256') and
            artifacts.get('operator') == {'path': str(WORK / 'prepare-client-fixture.py'), 'sha256': SCHEMA27_OPERATOR_SHA},
            'schema27_source_or_operator_mismatch')
    before, after = completed['preservation'], completed['after_preservation']
    require(before.get('schema') == 26 and before.get('table_count') == 33 and after.get('schema') == 27 and after.get('table_count') == 35 and
            set(after['row_counts']) - set(before['row_counts']) == {'extra_reserved_paths', 'item_extra_resources'} and
            all(after['row_counts'][name] == count + (1 if name == 'schema_migrations' else 0) for name, count in before['row_counts'].items()) and
            after.get('extras') == {'extra_reserved_paths': 0, 'item_extra_resources': 0} and
            all(after['row_counts'].get(name) == 0 for name in ('extra_reserved_paths', 'item_extra_resources')) and
            all(before.get(key) == after.get(key) for key in ('theme', 'recovery_sha256', 'runtime_sha256', 'browser_sha256',
                                                           'item_count', 'library_count', 'added_viewer_credentials')),
            'schema27_preservation_contract_mismatch')
    report_upgrade_keys = ('id', 'phase', 'from_sha256', 'to_sha256', 'old_process', 'new_process', 'rollback_binary',
                           'evidence_directory', 'protocol_version', 'product_version', 'preservation', 'after_preservation',
                           'from_schema', 'to_schema', 'schema_artifacts')
    require(report.get('result') == 'ready' and report.get('schema') == 27 and report.get('binary_sha256') == state['binary_sha256'] and
            report.get('process') == state['process'] and report.get('server_id') == state['server_id'] and
            report.get('database') == {'name': 'goby_client_m3e', 'oid': state['database_oid'], 'role_oid': state['role_oid']} and
            exact(report.get('upgrade')) == exact({key: completed[key] for key in report_upgrade_keys}),
            'schema27_fixture_report_mismatch')
    return schema27_expiry(origin)


def schema27_authority(record, *, live=False):
    ledger = json_record(record)
    state, completed, report, origin = (json_record(ledger[name]) for name in ('fixture_state', 'completed', 'fixture_report', 'origin_after'))
    expiry = schema27_receipts(ledger, state, completed, report, origin)
    authority = {'scope': SCHEMA27_SCOPE, 'expected_ledger_sha256': record['sha256'],
                 'fixture_state_sha256': ledger['fixture_state']['sha256'], 'completed_sha256': ledger['completed']['sha256'],
                 'fixture_report_sha256': ledger['fixture_report']['sha256'], 'origin_after_sha256': SCHEMA27_ORIGIN_SHA,
                 'process': state['process'], 'binary_sha256': state['binary_sha256'], 'source': state['schema27_source']['source'],
                 'source_manifest_sha256': state['schema27_source']['source_manifest_sha256'],
                 'database_oid': state['database_oid'], 'role_oid': state['role_oid'], 'approved_expiry_id_sha256': expiry}
    if live:
        raw = read_record(ledger['operator_proof'], modes=(0o600, 0o644, 0o700, 0o755))
        module = types.ModuleType('schema27_fixture_readonly_proof')
        module.__file__ = ledger['operator_proof']['path']
        exec(compile(raw, module.__file__, 'exec'), module.__dict__)
        product = completed['schema_artifacts']['product_verification']
        require(module.verify_product_upgrade(Path(completed['source']), state['binary_sha256'],
                Path(authority['source']), authority['source_manifest_sha256'], Path(product['report_path']),
                product['report_sha256'], target=27) == product, 'schema27_full_source_authority_changed')
        require(module.verify_service(state) == state['process'], 'schema27_live_process_changed')
    return authority, origin


def schema27_start(document, origin):
    require(document['counts'] == SCHEMA27_COUNTS and all(exact(document[name]) == exact(origin[name]) for name in SCHEMA27_COUNTS) and
            exact(document['actors']) == exact(origin['actors']), 'schema27_fresh_start_rows_changed')
    require(all(document['movie'].get(key) == value for key, value in origin['movie'].items()) and
            document['movie'].get('has_extra_association') is False and document['movie'].get('extra_reserved') is False,
            'schema27_original_movie_changed')


def observe_schema27(args):
    require(args.expected_ledger is not None and re.fullmatch(r'[0-9a-f]{64}', args.expected_ledger_sha256 or '') and
            args.output_root is not None, 'schema27_requires_private_ledger_and_output')
    record = {'path': str(args.expected_ledger), 'sha256': args.expected_ledger_sha256}
    authority, origin = schema27_authority(record, live=True)
    root = args.output_root
    require(root.parent == WORK and re.fullmatch(r'prepare-scope-observation-schema27-[a-z0-9][a-z0-9_-]{0,63}', root.name),
            'schema27_output_root_escape')
    owner = (json.dumps({'marker': MARKER, 'scope': SCHEMA27_SCOPE, 'path': str(root), 'expected_ledger': record},
                        sort_keys=True, separators=(',', ':')) + '\n').encode()
    private_directory(WORK, mode=0o700)
    if args.phase == 'before':
        root.mkdir(mode=0o700)
        private_write(root / 'OWNER.json', owner)
    else:
        private_directory(root, mode=0o700)
        require(private_read(root / 'OWNER.json') == owner, 'schema27_observation_owner_changed')
        prior = json.loads(private_read(root / 'before.json'), parse_float=Decimal)
        require(prior.get('preparation_scope') == SCHEMA27_SCOPE and prior.get('authority') == authority, 'schema27_before_authority_changed')
    require(not os.path.lexists(root / (args.phase + '.json')), 'schema27_observation_already_exists')
    statement = SQL27.replace('__PHASE__', args.phase).encode()
    result = subprocess.run(CONNECTION, input=statement, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=25,
                            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
                                 'PGOPTIONS': '-c default_transaction_read_only=on', 'PGAPPNAME': 'goby-client-schema27-prepare-observation'})
    require(result.returncode == 0 and 0 < len(result.stdout) <= MAX_REPORT, 'schema27_readonly_observation_failed')
    raw = result.stdout.strip()
    document = json.loads(raw, parse_float=Decimal)
    identity = document['identity']
    require(document.get('complete') is True and identity == {'database': 'goby_client_m3e', 'port': 15432, 'readonly': 'on',
            'postgresql': 170011, 'schema': 27, 'system_identifier': '7684040109719526738',
            'database_oid': authority['database_oid'], 'role_oid': authority['role_oid'], 'database_owner_oid': authority['role_oid']},
            'schema27_observed_database_mismatch')
    require(document['movie'].get('has_extra_association') is False and document['movie'].get('extra_reserved') is False and
            document['movie'].get('has_theme_association') is False and document['movie'].get('reserved') is False,
            'schema27_movie_is_auxiliary')
    if args.phase == 'before':
        schema27_start(document, origin)
    current, _ = schema27_authority(record, live=True)
    require(current == authority, 'schema27_authority_changed_during_read')
    # Retain PostgreSQL's exact numeric JSON bytes while adding only nonsecret authority facts.
    additions = json.dumps({'preparation_scope': SCHEMA27_SCOPE, 'authority': authority}, sort_keys=True, separators=(',', ':')).encode()
    raw = raw[:-1] + b',' + additions[1:] + b'\n'
    private_write(root / (args.phase + '.json'), raw)
    print(json.dumps({'status': 'observed', 'scope': SCHEMA27_SCOPE, 'phase': args.phase, 'schema': 27,
                      'counts': document['counts'], 'report_sha256': hashlib.sha256(raw).hexdigest(),
                      'expected_ledger_sha256': record['sha256'], 'report_path': str(root / (args.phase + '.json')),
                      'database_mutations': 0, 'http_requests': 0}, sort_keys=True))


def private_directory(path, *, mode=None):
    require(path.is_absolute() and '..' not in path.parts, 'The output path is not canonical.')
    for member in reversed((path, *path.parents)):
        info = member.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An output directory has an untrusted type, owner, or write permission.')
    if mode is not None:
        require(stat.S_IMODE(path.lstat().st_mode) == mode, 'The private directory mode differs.')


def private_read(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and
            stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and info.st_size <= MAX_REPORT,
            'A private observation file changed its identity or permissions.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        opened = os.fstat(stream.fileno())
        require((opened.st_dev, opened.st_ino) == (info.st_dev, info.st_ino), 'The private file changed while opening.')
        return stream.read(MAX_REPORT + 1)


def private_write(path, raw):
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase', choices=('before', 'after'))
    parser.add_argument('--output-root', type=Path)
    parser.add_argument('--preparation-scope', choices=(SCHEMA27_SCOPE,))
    parser.add_argument('--expected-ledger', type=Path)
    parser.add_argument('--expected-ledger-sha256')
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
            os.environ.get('SSH_CONNECTION'), 'Run only through authorized root SSH on test-env.')
    if args.preparation_scope == SCHEMA27_SCOPE:
        return observe_schema27(args)
    require(args.expected_ledger is None and args.expected_ledger_sha256 is None, 'unexpected_schema27_ledger')
    root = args.output_root
    if root is not None:
        require(root.parent == WORK and re.fullmatch(r'prepare-scope-observation-[a-z0-9][a-z0-9_-]{0,63}', root.name),
                'The output root escaped its new fixed workspace namespace.')
        private_directory(WORK, mode=0o700)
        owner = (json.dumps({'marker': MARKER, 'path': str(root), 'actors': ACTORS, 'movie': MOVIE},
                            sort_keys=True, separators=(',', ':')) + '\n').encode()
        if args.phase == 'before':
            root.mkdir(mode=0o700)
            private_directory(root, mode=0o700)
            private_write(root / 'OWNER.json', owner)
        else:
            private_directory(root, mode=0o700)
            require(private_read(root / 'OWNER.json') == owner, 'The existing observation root belongs to another scope.')
            before = json.loads(private_read(root / 'before.json'))
            require(before.get('marker') == MARKER and before.get('phase') == 'before' and before.get('complete') is True,
                    'The after observation lacks its complete private before record.')
        require(not os.path.lexists(root / (args.phase + '.json')), 'An existing observation must not be overwritten.')
    statement = SQL.replace('__PHASE__', args.phase).encode()
    result = subprocess.run(CONNECTION, input=statement, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=25,
                            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8',
                                 'PGOPTIONS': '-c default_transaction_read_only=on',
                                 'PGAPPNAME': 'goby-client-prepare-scope-observation'})
    require(result.returncode == 0 and 0 < len(result.stdout) <= MAX_REPORT, 'The bounded read-only observation failed.')
    raw = result.stdout.strip() + b'\n'
    document = json.loads(raw)
    expected = {'database': 'goby_client_m3e', 'port': 15432, 'readonly': 'on', 'postgresql': 170011,
                'schema': 26, 'system_identifier': '7684040109719526738'}
    require(document.get('identity') == expected and document.get('complete') is True and document.get('phase') == args.phase,
            'The database identity or complete observation bound differs.')
    require({entry['label']: entry['user_id'] for entry in document['actors']} == ACTORS and
            all(entry['exists'] is True and entry['disabled'] is False and entry['administrator'] is False and
                entry['all_folders'] is True and entry['playback_allowed'] is True for entry in document['actors']),
            'The two ordinary viewers no longer have the observed preparation scope.')
    movie = document['movie']
    require(movie['id'] == MOVIE and movie['exists'] is True and movie['type'] == 'Movie' and movie['folder'] is False and
            movie['has_media'] is True and movie['has_theme_association'] is False and movie['reserved'] is False and
            document['foreign_dependents'] == {'references': 0, 'encodings': 0},
            'The owned movie or dependent-row scope changed.')
    if root is not None:
        # Preserve the exact PostgreSQL JSON bytes, including wide integers and
        # decimal values, rather than round-tripping private rows through float.
        private_write(root / (args.phase + '.json'), raw)
    print(json.dumps({'status': 'observed', 'marker': MARKER, 'phase': args.phase, 'database': 'goby_client_m3e',
                      'port': 15432, 'database_mutations': 0, 'http_requests': 0, 'counts': document['counts'],
                      'report_sha256': hashlib.sha256(raw).hexdigest(), 'sql_sha256': hashlib.sha256(statement).hexdigest(),
                      'report_path': str(root / (args.phase + '.json')) if root else None}, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'status': 'failed', 'error_type': type(error).__name__, 'database_mutations': 0,
                          'http_requests': 0}), file=sys.stderr)
        raise SystemExit(1)
