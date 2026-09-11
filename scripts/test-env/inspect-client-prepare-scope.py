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
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

WORK = Path('/opt/goby-test/exec-work-m3e')
MARKER = 'goby-client-prepare-scope-observation-v1'
ACTORS = {'A': '34b4c24f6568659af7ce17938fae7f81', 'B': 'ecbbe4cb82403879bc4b4f78894c5738'}
MOVIE = '268051d3ca734aefcf94e245fb25ad55'
MAX_REPORT = 16 << 20
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


def require(condition, message):
    if not condition:
        raise ValueError(message)


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
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
            os.environ.get('SSH_CONNECTION'), 'Run only through authorized root SSH on test-env.')
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
