"""Prepare only the explicitly owned, disposable external bitmap live environment."""
from pathlib import Path
import hashlib
import json
import os
import secrets
import shutil
import socket
import subprocess
import time


SCOPE = Path('/opt/goby-test/player-live-20261004-165c/external-timeline-check')
OWNER = 'goby-external-timeline-165c-20261005'
LIVE = SCOPE / 'live'
PG = 'goby-external-timeline-165c-live-pg'
PG_IMAGE = 'mirror.gcr.io/library/postgres:17-bookworm'
NETWORK = 'goby-external-timeline-165c-live'
BACKEND = 'goby-external-timeline-165c-live-backend'
IMAGE = 'goby-external-timeline-165c-live:20261005'
BASE = 'goby-player-accepted-base:20261004'
assert os.name == 'posix' and SCOPE.resolve() == SCOPE
assert (SCOPE / '.owner').read_text().strip() == OWNER


def run(args, **kwargs):
    value = subprocess.run(args, text=True, capture_output=True, **kwargs)
    if value.returncode:
        raise RuntimeError('Owned environment command failed: ' + args[0] + '\n' + value.stderr[-2000:])
    return value.stdout.strip()


def directory(path, uid=None):
    assert path.is_relative_to(LIVE) and not path.is_symlink()
    path.mkdir(exist_ok=True)
    assert path.resolve() == path
    if uid is not None:
        path.chmod(0o755)
        os.chown(path, uid, uid)


if LIVE.exists():
    assert (LIVE / '.owner').read_text().strip() == OWNER
else:
    LIVE.mkdir(mode=0o700)
    (LIVE / '.owner').write_text(OWNER + '\n')
private = LIVE / 'live.private.json'
assert not private.exists(), 'A completed environment is not recreated automatically.'
role = 'goby_external_live_165c'
database = role
password = secrets.token_urlsafe(32)
run(['docker', 'network', 'create', '--label', 'goby.owner=' + OWNER, NETWORK])
for name in ['image', 'state', 'cache', 'logs', 'media', 'artifacts']:
    directory(LIVE / name, 10001 if name in ['state', 'cache', 'logs', 'media'] else None)
    if name in ['state', 'cache', 'logs']:
        (LIVE / name).chmod(0o700)
directory(LIVE / 'pgdata', 999)
(LIVE / 'pgdata').chmod(0o700)
postgres = {'user': 'goby_external_live_admin', 'password': secrets.token_urlsafe(32), 'container': PG}
postgresFile = LIVE / 'live-postgres.private.json'
postgresFile.write_text(json.dumps(postgres))
postgresFile.chmod(0o600)
postgresEnvironment = LIVE / 'live-postgres.private.env'
postgresEnvironment.write_text('POSTGRES_USER=' + postgres['user'] + '\nPOSTGRES_PASSWORD=' + postgres['password'] + '\n')
postgresEnvironment.chmod(0o600)
run(['docker', 'run', '-d', '--pull=never', '--name', PG, '--label', 'goby.owner=' + OWNER, '--network', NETWORK,
     '--cpus=1', '--memory=1g', '--pids-limit=128', '-v', str(LIVE / 'pgdata') + ':/var/lib/postgresql/data',
     '--env-file', str(postgresEnvironment), PG_IMAGE, '-c', 'max_wal_size=128MB', '-c', 'min_wal_size=32MB'])
for _ in range(100):
    if subprocess.run(['docker', 'exec', PG, 'pg_isready', '-U', postgres['user']], capture_output=True).returncode == 0:
        break
    time.sleep(.1)
else:
    raise RuntimeError('The dedicated live database did not become ready.')
sql = f"CREATE ROLE {role} LOGIN PASSWORD '{password}';\nCREATE DATABASE {database} OWNER {role};\nCOMMENT ON DATABASE {database} IS '{OWNER}';\n"
run(['docker', 'exec', '-i', PG, 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-U', postgres['user'], '-d', 'postgres'], input=sql)
media = LIVE / 'media' / 'ExternalBitmap'
directory(media, 10001)
movie = media / 'External Bitmap (2026)'
directory(movie, 10001)
fixture = SCOPE / 'source/internal/server/testdata/external-bitmap'
oracle = json.loads((fixture / 'expected.json').read_text())
for value in oracle['Files']:
    data = (fixture / value['Name']).read_bytes()
    assert len(data) == value['Bytes'] and hashlib.sha256(data).hexdigest() == value['SHA256']
    target = movie / ('External Bitmap' + Path(value['Name']).suffix)
    target.write_bytes(data)
    os.chown(target, 10001, 10001)
    target.chmod(0o644)
run(['docker', 'run', '--rm', '--network=none', '--user=10001:10001', '--cpus=1', '--memory=256m', '--pids-limit=64',
     '-v', str(media) + ':/fixture', '--entrypoint=/opt/ffmpeg/9.0.1/bin/ffmpeg', BASE,
     '-hide_banner', '-nostdin', '-loglevel', 'error', '-n', '-filter_threads', '1', '-f', 'lavfi', '-i',
     'color=c=0x124860:s=640x360:r=12:d=20', '-vf', 'drawbox=x=20:y=20:w=120:h=80:color=0x65a8d9:t=fill',
     '-an', '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '24', '-pix_fmt', 'yuv420p', '-threads', '1',
     '-g', '24', '-bf', '0', '-movflags', '+faststart', '/fixture/External Bitmap (2026)/External Bitmap.mp4'])
probe = json.loads(run(['docker', 'run', '--rm', '--network=none', '--user=10001:10001', '--cpus=1', '--memory=256m',
                       '--pids-limit=64', '-v', str(media) + ':/fixture:ro', '--entrypoint=/opt/ffmpeg/9.0.1/bin/ffprobe', BASE,
                       '-v', 'error', '-show_streams', '-show_format', '-of', 'json', '/fixture/External Bitmap (2026)/External Bitmap.mp4']))
assert [entry['codec_type'] for entry in probe['streams']] == ['video']
assert float(probe['format']['duration']) == 20
(LIVE / 'artifacts/video-probe.json').write_text(json.dumps(probe, indent=2))
(LIVE / 'artifacts/oracle.json').write_text(json.dumps(oracle, indent=2))
shutil.copy2(SCOPE / 'goby-live', LIVE / 'image/goby')
digest = hashlib.sha256((LIVE / 'image/goby').read_bytes()).hexdigest()
(LIVE / 'image/Dockerfile').write_text('FROM ' + BASE + '\nUSER 0:0\nCOPY --chown=10001:10001 --chmod=0555 goby /usr/local/bin/goby\nLABEL goby.owner="' + OWNER + '" io.goby.application.sha256="' + digest + '"\nUSER 10001:10001\n')
run(['docker', 'build', '--network=none', '--pull=false', '-t', IMAGE, str(LIVE / 'image')])
ports = []
for port in [38985, 38986]:
    guard = socket.socket()
    guard.bind(('127.0.0.1', port))
    guard.close()
    ports.append(port)
config = {'baseURL': 'http://127.0.0.1:' + str(ports[0]), 'playerURL': 'http://127.0.0.1:' + str(ports[1]),
          'setupToken': secrets.token_urlsafe(32), 'username': 'external-reviewer', 'password': secrets.token_urlsafe(24),
          'database': database, 'databaseRole': role, 'databasePassword': password, 'container': BACKEND, 'network': NETWORK,
          'image': IMAGE, 'applicationSHA256': digest}
private.write_text(json.dumps(config))
private.chmod(0o600)
environment = {'GOBY_DATABASE_URL': f'postgresql://{role}:{password}@{PG}:5432/{database}?sslmode=disable',
               'GOBY_SETUP_TOKEN': config['setupToken'], 'GOBY_PUBLIC_URL': config['playerURL'], 'GOBY_COOKIE_SECURE': 'false',
               'GOBY_WEB_DIR': '', 'GOBY_MEDIA_ROOTS': '/media', 'GOBY_SERVER_NAME': 'External bitmap isolated acceptance',
               'GOBY_MEDIA_ANALYSIS_FILE': '/var/lib/goby/media-analysis.json', 'GOBY_TRANSCODING_ENABLED': 'false'}
envfile = LIVE / 'live.private.env'
envfile.write_text(''.join(key + '=' + value + '\n' for key, value in environment.items()))
envfile.chmod(0o600)
analysis = LIVE / 'state/media-analysis.json'
analysis.write_text(json.dumps({'enabled': True, 'cacheDirectory': '/var/cache/goby/media-analysis'}))
analysis.chmod(0o644)
os.chown(analysis, 10001, 10001)
run(['docker', 'run', '-d', '--pull=never', '--name', BACKEND, '--label', 'goby.owner=' + OWNER, '--network', NETWORK,
     '--user=10001:10001', '--init', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges:true',
     '--cpus=2', '--memory=2g', '--pids-limit=256', '--env-file', str(envfile), '-p', '127.0.0.1:' + str(ports[0]) + ':8096',
     '-v', str(LIVE / 'state') + ':/var/lib/goby', '-v', str(LIVE / 'cache') + ':/var/cache/goby',
     '-v', str(LIVE / 'logs') + ':/var/log/goby', '-v', str(LIVE / 'media') + ':/media',
     '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=67108864,mode=0700,uid=10001,gid=10001', IMAGE])
print(json.dumps({'prepared': True, 'backend': BACKEND, 'baseURL': config['baseURL'], 'playerURL': config['playerURL'],
                  'database': database, 'applicationSHA256': digest, 'mediaSeconds': 20, 'embeddedSubtitleTracks': 0}))
