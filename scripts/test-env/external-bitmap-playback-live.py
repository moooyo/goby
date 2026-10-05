#!/usr/bin/env python3
"""Bounded host operations for the owned external bitmap playback acceptance.

prepare-backend accepts an already compiled Linux binary and immutable source
bindings. start creates new backend/player containers; it never restarts older
containers. mutate/restore retain each original subtitle separately. decode
reads a downloaded local media artifact and writes one RGBA frame plus receipt.
stop retains PostgreSQL until the separate close-database action is requested.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import stat
import subprocess
import sys
import time
import traceback
import urllib.parse

ROOT = Path('/opt/goby-test/external-bitmap-playback-20261006-165c')
OWNER = 'goby-external-bitmap-playback-165c-20261006'
BASE = 'sha256:318a7d1294d6cdc036c0f2ee41e5165e6a643048144885980299a8c9c76646d0'
PLAYER = 'sha256:7140ce5c531a302b0aca595ee47ce98c52cd08ed73d5a00e5a27972d1ad94192'
PG = 'goby-external-bitmap-playback-165c-pg'
NETWORK = 'goby-external-bitmap-playback-165c'
BACKEND = 'goby-external-bitmap-playback-165c-backend'
FRONTEND = 'goby-external-bitmap-playback-165c-player'
FFMPEG = '/opt/ffmpeg/9.0.1/bin/ffmpeg'
FFPROBE = '/opt/ffmpeg/9.0.1/bin/ffprobe'
EXTENSIONS = ('sup', 'idx', 'sub')
FRAME_BYTES = 320 * 192 * 4


def require(value, message):
    if not value:
        raise RuntimeError(message)


def owned(path, exists=False):
    require(path.is_relative_to(ROOT) and path.resolve(strict=exists) == path, 'A path escaped the owned scope or traversed a symlink')
    return path


def write(path, value, exclusive=True, mode=0o600):
    owned(path)
    data = json.dumps(value, indent=2) + '\n' if isinstance(value, (dict, list)) else value
    with path.open('x' if exclusive else 'w') as handle:
        handle.write(data)
    path.chmod(mode)


def run(arguments, timeout=120, input=None):
    result = subprocess.run(arguments, input=input, text=True, capture_output=True, timeout=timeout)
    if result.returncode:
        name = 'private-command-' + str(time.time_ns()) + '.log'
        write(ROOT / 'logs' / name, result.stdout + result.stderr)
        raise RuntimeError('Owned command failed; private diagnostics retained')
    return result.stdout.strip()


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()


def identity(path):
    owned(path, True)
    info = path.stat()
    return {'bytes': info.st_size, 'sha256': digest(path), 'device': info.st_dev, 'inode': info.st_ino,
            'mtimeNs': str(info.st_mtime_ns), 'uid': info.st_uid, 'gid': info.st_gid, 'mode': info.st_mode, 'links': info.st_nlink}


def directory(path, uid=None, mode=0o700):
    owned(path)
    path.mkdir(mode=mode)
    path.chmod(mode)
    if uid is not None:
        os.chown(path, uid, uid)


def inspect(name):
    value = json.loads(run(['docker', 'inspect', name]))[0]
    require(value['Config']['Labels'].get('goby.owner') == OWNER, 'Unexpected container owner')
    return value


def private():
    value = json.loads(owned(ROOT / 'live.private.json', True).read_text())
    require(value['owner'] == OWNER, 'Unexpected private configuration owner')
    return value


def prepare_pixels(name):
    require(re.fullmatch(r'[A-Za-z0-9 -]{1,64}', name), 'Invalid fixture name')
    target = ROOT / 'fixtures' / (name + ' (2026)')
    require(not target.exists(), 'Use a new pixel fixture directory')
    preparation = json.loads((ROOT / 'logs/fixture-preparation.json').read_text())
    directory(target, 10001, 0o755)
    retained = {}
    for suffix in ['.sup', '.idx', '.sub']:
        source = Path(preparation['copies'][suffix]['copy'])
        before = identity(source)
        require(before == preparation['copies'][suffix]['copyIdentity'], 'An original copy changed before pixel preparation')
        destination = target / (name + suffix)
        with source.open('rb') as reader, destination.open('xb') as writer:
            shutil.copyfileobj(reader, writer)
        destination.chmod(0o644)
        os.chown(destination, 10001, 10001)
        require(identity(source) == before and digest(destination) == before['sha256'], 'Source or destination bytes changed during copying')
        retained[suffix] = {'sourceIdentity': before, 'copyIdentity': identity(destination)}
    media = '/media/' + str((target / (name + '.mp4')).relative_to(ROOT / 'fixtures'))
    run(['docker', 'run', '--rm', '--pull=never', '--label', 'goby.owner=' + OWNER, '--network=none', '--user=10001:10001',
         '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges:true', '--cpus=1', '--memory=256m', '--pids-limit=64',
         '--mount', 'type=bind,source=' + str(ROOT / 'fixtures') + ',target=/media', '--entrypoint=' + FFMPEG, BASE,
         '-hide_banner', '-nostdin', '-v', 'error', '-n', '-filter_threads', '1', '-f', 'lavfi', '-i', 'color=c=0x14253b:s=320x192:r=24:d=35',
         '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=35', '-map', '0:v:0', '-map', '1:a:0',
         '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '18', '-threads', '1', '-pix_fmt', 'yuv420p', '-g', '24', '-bf', '0',
         '-color_primaries', 'bt709', '-color_trc', 'bt709', '-colorspace', 'bt709', '-color_range', 'tv',
         '-c:a', 'aac', '-b:a', '96k', '-movflags', '+faststart', media])
    source = target / (name + '.mp4')
    probe = json.loads(run(['docker', 'run', '--rm', '--pull=never', '--label', 'goby.owner=' + OWNER, '--network=none', '--user=10001:10001',
                           '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges:true', '--cpus=1', '--memory=128m', '--pids-limit=32',
                           '--mount', 'type=bind,source=' + str(ROOT / 'fixtures') + ',target=/media,readonly', '--entrypoint=' + FFPROBE, BASE,
                           '-v', 'error', '-show_streams', '-show_format', '-of', 'json', media]))
    require([stream['codec_type'] for stream in probe['streams']] == ['video', 'audio'], 'The source must contain no embedded subtitle')
    require(probe['streams'][0]['width'] == 320 and probe['streams'][0]['height'] == 192 and float(probe['format']['duration']) == 35, 'Pixel fixture geometry or clock differs')
    value = {'owner': OWNER, 'name': name, 'path': str(source), 'containerSource': media, 'sourceIdentity': identity(source),
             'subtitles': retained, 'probe': probe, 'canvas': {'width': 320, 'height': 192}, 'backgroundRGB': [20, 37, 59],
             'glyph': {'text': 'Hello world', 'width': 130, 'height': 14, 'opaquePixels': 492},
             'positions': {'top': [21, 40], 'bottom': [93, 100]}, 'subtitleOracle': preparation['subtitleOracle']}
    write(ROOT / 'logs/pixel-fixture.json', value)
    return {'pixelFixturePrepared': True, 'bytes': source.stat().st_size, 'sha256': value['sourceIdentity']['sha256'], 'subtitlesInVideo': 0}


def prepare_database():
    require(not (ROOT / 'live.private.json').exists(), 'Database preparation already exists')
    for port in [39221, 39222, 39223]:
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', port))
    require(shutil.disk_usage(ROOT).free >= 2 * 1024 ** 3, 'Reconcile capacity before admitting PostgreSQL')
    role = 'goby_bitmap_playback_165c'
    databases = {'app': role + '_app', 'test': role + '_test'}
    config = {'owner': OWNER, 'baseURL': 'http://127.0.0.1:39221', 'playerURL': 'http://127.0.0.1:39222',
              'container': BACKEND, 'playerContainer': FRONTEND, 'network': NETWORK, 'postgresContainer': PG,
              'databaseRole': role, 'databasePassword': secrets.token_urlsafe(32), 'databases': databases,
              'setupToken': secrets.token_urlsafe(32), 'username': 'bitmap-playback-reviewer', 'password': secrets.token_urlsafe(24),
              'mediaPath': json.loads((ROOT / 'logs/pixel-fixture.json').read_text())['containerSource']}
    write(ROOT / 'live.private.json', config)
    admin = {'role': role + '_admin', 'password': secrets.token_urlsafe(32)}
    write(ROOT / 'postgres.private.json', admin)
    write(ROOT / 'postgres.private.env', 'POSTGRES_USER=' + admin['role'] + '\nPOSTGRES_PASSWORD=' + admin['password'] + '\n')
    image = json.loads(run(['docker', 'image', 'inspect', 'mirror.gcr.io/library/postgres:17-bookworm']))[0]['Id']
    run(['docker', 'network', 'create', '--label', 'goby.owner=' + OWNER, NETWORK])
    directory(ROOT / 'pgdata', 999)
    run(['docker', 'run', '-d', '--pull=never', '--name', PG, '--label', 'goby.owner=' + OWNER, '--network', NETWORK,
         '--cpus=1', '--memory=512m', '--memory-swap=512m', '--pids-limit=128', '-p', '127.0.0.1:39223:5432',
         '--mount', 'type=bind,source=' + str(ROOT / 'pgdata') + ',target=/var/lib/postgresql/data',
         '--env-file', str(ROOT / 'postgres.private.env'), image, '-c', 'max_wal_size=128MB', '-c', 'min_wal_size=32MB', '-c', 'shared_buffers=64MB'])
    return finish_database()


def finish_database():
    config = private()
    admin = json.loads((ROOT / 'postgres.private.json').read_text())
    current = inspect(PG)
    require(current['State']['Running'] and current['HostConfig']['PortBindings']['5432/tcp'] == [{'HostIp': '127.0.0.1', 'HostPort': '39223'}], 'Unexpected owned PostgreSQL runtime')
    role = config['databaseRole']
    databases = config['databases']
    require(re.fullmatch(r'[a-z0-9_]{1,63}', role) and all(re.fullmatch(r'[a-z0-9_]{1,63}', value) for value in databases.values()), 'Invalid database identity')
    for _ in range(120):
        # PostgreSQL's initialization server only listens on its Unix socket.
        # Wait for TCP so that the transient bootstrap server cannot be mistaken
        # for the final ready process.
        value = subprocess.run(['docker', 'exec', PG, 'pg_isready', '-h', '127.0.0.1', '-U', admin['role']], capture_output=True)
        if value.returncode == 0:
            break
        time.sleep(0.25)
    else:
        raise RuntimeError('Owned PostgreSQL readiness timed out')
    sql_args = ['docker', 'exec', '-i', PG, 'psql', '-X', '-v', 'ON_ERROR_STOP=1', '-At', '-U', admin['role'], '-d', 'postgres']
    if run(sql_args, input="SELECT count(*) FROM pg_roles WHERE rolname='" + role + "';\n") == '0':
        run(sql_args, input="CREATE ROLE " + role + " LOGIN PASSWORD '" + config['databasePassword'] + "';\n")
    for database in databases.values():
        if run(sql_args, input="SELECT count(*) FROM pg_database WHERE datname='" + database + "';\n") == '0':
            run(sql_args, input='CREATE DATABASE ' + database + ' OWNER ' + role + ';\n')
            run(sql_args, input="COMMENT ON DATABASE " + database + " IS '" + OWNER + "';\n")
        check = run(sql_args, input="SELECT pg_get_userbyid(datdba)||'|'||coalesce(shobj_description(oid,'pg_database'),'') FROM pg_database WHERE datname='" + database + "';\n")
        require(check == role + '|' + OWNER, 'Database owner or scope comment differs')
    credentials = urllib.parse.quote(role, safe='') + ':' + urllib.parse.quote(config['databasePassword'], safe='')
    for key, database in databases.items():
        write(ROOT / (key + '-database.private.url'), 'postgres://' + credentials + '@127.0.0.1:39223/' + database + '?sslmode=disable\n')
    value = inspect(PG)
    receipt = {'owner': OWNER, 'containerId': value['Id'], 'imageId': value['Image'], 'databaseNames': databases, 'hostPort': 39223,
               'dataPath': str(ROOT / 'pgdata'), 'diskFreeBytes': shutil.disk_usage(ROOT).free,
               'testURLFile': str(ROOT / 'test-database.private.url'), 'applicationURLFile': str(ROOT / 'app-database.private.url')}
    write(ROOT / 'logs/database-prepared.json', receipt)
    return receipt


def read_json(path):
    owned(path, True)
    require(path.is_file() and path.stat().st_size <= 1024 * 1024, 'Expected bounded owned JSON')
    return json.loads(path.read_text())


def ensure_directory(path, uid=0, mode=0o700):
    owned(path)
    if not path.exists():
        directory(path, uid, mode)
    info = path.stat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == uid and info.st_gid == uid and
            stat.S_IMODE(info.st_mode) == mode, 'Owned directory permissions or ownership changed')
    return path


def replace_json(path, value):
    owned(path)
    temporary = path.with_name(path.name + '.' + str(time.time_ns()) + '.tmp')
    write(temporary, value)
    os.replace(temporary, path)


def optional_container(name):
    require(name in (BACKEND, FRONTEND, PG), 'Unexpected container name')
    identifiers = run(['docker', 'container', 'ls', '-aq', '--filter', 'name=^/' + name + '$']).splitlines()
    require(len(identifiers) <= 1, 'Ambiguous owned container')
    return inspect(name) if identifiers else None


def network():
    value = json.loads(run(['docker', 'network', 'inspect', NETWORK]))[0]
    require((value.get('Labels') or {}).get('goby.owner') == OWNER and value['Name'] == NETWORK,
            'Unexpected network owner')
    return value


def check_private(config):
    require(config['container'] == BACKEND and config['playerContainer'] == FRONTEND and
            config['postgresContainer'] == PG and config['network'] == NETWORK and
            config['baseURL'] == 'http://127.0.0.1:39221' and config['playerURL'] == 'http://127.0.0.1:39222',
            'Private runtime scope changed')
    info = owned(ROOT / 'live.private.json', True).stat()
    require(stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == 0 and info.st_nlink == 1,
            'Private configuration permissions changed')


def pixel_fixture():
    config = private()
    check_private(config)
    receipt = read_json(ROOT / 'logs/pixel-fixture.json')
    require(receipt['owner'] == OWNER, 'Unexpected pixel fixture owner')
    source = owned(Path(receipt['path']), True)
    require(source.is_relative_to(ROOT / 'fixtures') and source.suffix == '.mp4' and
            receipt['containerSource'] == '/media/' + str(source.relative_to(ROOT / 'fixtures')) and
            config['mediaPath'] == receipt['containerSource'], 'The selected pixel fixture changed')
    require(identity(source) == receipt['sourceIdentity'], 'Pixel media changed')
    files = {'mp4': source, **{extension: source.with_suffix('.' + extension) for extension in EXTENSIONS}}
    for path in files.values():
        info = owned(path, True).stat()
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'Fixture must be an independent regular file')
    return receipt, files


def snapshot():
    receipt, files = pixel_fixture()
    identities = {extension: {'path': str(path), 'containerPath': '/media/' + str(path.relative_to(ROOT / 'fixtures')),
                              'identity': identity(path)} for extension, path in files.items()}
    timeline = owned(files['mp4'].parent / 'backdrops/goby-subtitle-timelines')
    exists = timeline.exists()
    return {'owner': OWNER, 'mediaPath': receipt['containerSource'], 'sourceIdentities': identities,
            'sourceIdentity': identities['mp4']['identity'],
            'subtitles': {extension: identities[extension]['identity'] for extension in EXTENSIONS},
            'timelineSidecarPath': str(timeline), 'timelineSidecarExists': exists, 'noTimelineSidecar': not exists}


def prepare_backend(binary, binary_sha256, revision):
    require(binary is not None and binary_sha256 is not None and revision is not None,
            'prepare-backend requires binary, binary-sha256 and revision')
    require(re.fullmatch(r'[a-f0-9]{64}', binary_sha256) and re.fullmatch(r'[a-f0-9]{40}', revision),
            'Use an exact lowercase binary digest and full Git revision')
    source = owned(binary, True)
    require(source.is_file() and not source.is_relative_to(ROOT / 'fixtures') and
            0 < source.stat().st_size <= 256 * 1024 * 1024, 'Invalid application binary')
    before = identity(source)
    require(before['sha256'] == binary_sha256 and before['links'] == 1, 'Application binary identity differs')
    with source.open('rb') as handle:
        require(handle.read(4) == b'\x7fELF', 'Expected a Linux application binary')
    config = private()
    check_private(config)
    require(not (ROOT / 'logs/backend-prepared.json').exists(), 'The prepared application image is retained')
    require(optional_container(BACKEND) is None and optional_container(FRONTEND) is None,
            'Application containers already exist; do not replace an earlier runtime')
    base = json.loads(run(['docker', 'image', 'inspect', BASE]))[0]
    player = json.loads(run(['docker', 'image', 'inspect', PLAYER]))[0]
    require(base['Id'] == BASE and player['Id'] == PLAYER, 'Pinned software or player image differs')
    require(shutil.disk_usage(ROOT).free >= 1024 ** 3 + 2 * before['bytes'],
            'Reconcile capacity before preparing the application image')
    context = ROOT / ('backend-image-' + str(time.time_ns()))
    directory(context)
    destination = context / 'goby'
    with source.open('rb') as reader, destination.open('xb') as writer:
        shutil.copyfileobj(reader, writer)
    destination.chmod(0o555)
    require(identity(source) == before and digest(destination) == binary_sha256,
            'Application input changed during the independent copy')
    # Dockerfile FROM needs a repository reference. Bind a task-private alias
    # to the immutable local image ID, then recheck it after the build.
    alias = 'goby-external-bitmap-playback-165c-base:' + BASE.split(':')[1]
    existing = subprocess.run(['docker', 'image', 'inspect', alias], text=True, capture_output=True, timeout=30)
    if existing.returncode == 0:
        require(json.loads(existing.stdout)[0]['Id'] == BASE, 'Private base alias changed')
    else:
        run(['docker', 'image', 'tag', BASE, alias])
    require(json.loads(run(['docker', 'image', 'inspect', alias]))[0]['Id'] == BASE, 'Private base alias differs')
    dockerfile = ('FROM ' + alias + '\nUSER 0:0\n'
                  'COPY --chown=0:0 --chmod=0555 goby /usr/local/bin/goby\n'
                  'LABEL goby.owner="' + OWNER + '" io.goby.application.sha256="' + binary_sha256 + '" '
                  'io.goby.application.base-image-id="' + BASE + '" '
                  'io.goby.application.revision="' + revision + '" '
                  'org.opencontainers.image.revision="' + revision + '"\nUSER 10001:10001\n')
    write(context / 'Dockerfile', dockerfile)
    image = 'goby-external-bitmap-playback-165c:' + binary_sha256[:16]
    existing = subprocess.run(['docker', 'image', 'inspect', image], text=True, capture_output=True, timeout=30)
    if existing.returncode == 0:
        previous_labels = json.loads(existing.stdout)[0]['Config'].get('Labels') or {}
        require(previous_labels.get('goby.owner') == OWNER and previous_labels.get('io.goby.application.sha256') == binary_sha256,
                'The application image tag belongs to different content')
    build_log_path = ROOT / 'logs' / (context.name + '.private.log')
    with build_log_path.open('x') as build_log:
        result = subprocess.run(['docker', 'build', '--network=none', '--pull=false', '-t', image, str(context)],
                                stdout=build_log, stderr=subprocess.STDOUT, timeout=180)
    require(result.returncode == 0, 'Owned application image build failed; private build log retained')
    require(json.loads(run(['docker', 'image', 'inspect', alias]))[0]['Id'] == BASE, 'Private base alias changed during the build')
    prepared = json.loads(run(['docker', 'image', 'inspect', image]))[0]
    labels = prepared['Config']['Labels'] or {}
    require(labels.get('goby.owner') == OWNER and labels.get('io.goby.application.sha256') == binary_sha256 and
            labels.get('io.goby.application.revision') == revision and labels.get('io.goby.application.base-image-id') == BASE and
            prepared['Config']['User'] == '10001:10001' and prepared['Config']['Entrypoint'] == base['Config']['Entrypoint'] and
            prepared['RootFS']['Layers'][:-1] == base['RootFS']['Layers'], 'Prepared image binding differs')
    value = {'owner': OWNER, 'image': image, 'imageId': prepared['Id'], 'baseImageId': BASE, 'playerImageId': PLAYER,
             'applicationSHA256': binary_sha256, 'revision': revision, 'binaryPath': str(source),
             'binaryIdentity': before, 'imageContext': str(context), 'baseAlias': alias, 'buildLog': str(build_log_path),
             'copiedApplicationIdentity': identity(destination)}
    write(ROOT / 'logs/backend-prepared.json', value)
    config.update({key: value[key] for key in ('image', 'imageId', 'applicationSHA256', 'revision')})
    replace_json(ROOT / 'live.private.json', config)
    return value


def prepared_backend():
    receipt = read_json(ROOT / 'logs/backend-prepared.json')
    config = private()
    check_private(config)
    require(receipt['owner'] == OWNER and receipt['baseImageId'] == BASE and receipt['playerImageId'] == PLAYER and
            all(config[key] == receipt[key] for key in ('image', 'imageId', 'applicationSHA256', 'revision')),
            'Application image receipt or private binding changed')
    image = json.loads(run(['docker', 'image', 'inspect', receipt['imageId']]))[0]
    labels = image['Config'].get('Labels') or {}
    require(image['Id'] == receipt['imageId'] and labels.get('goby.owner') == OWNER and
            labels.get('io.goby.application.sha256') == receipt['applicationSHA256'] and
            labels.get('io.goby.application.revision') == receipt['revision'] and
            labels.get('io.goby.application.base-image-id') == BASE, 'Application image identity changed')
    return receipt


def runtime_flags():
    return ['--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges:true',
            '--log-driver=json-file', '--log-opt=max-size=8m', '--log-opt=max-file=2']


def start():
    config = private()
    check_private(config)
    prepared = prepared_backend()
    require(optional_container(BACKEND) is None and optional_container(FRONTEND) is None,
            'Only new owned backend and player containers can be started')
    network()
    pg = inspect(PG)
    require(pg['State']['Running'] and NETWORK in pg['NetworkSettings']['Networks'] and
            pg['HostConfig']['PortBindings']['5432/tcp'] == [{'HostIp': '127.0.0.1', 'HostPort': '39223'}],
            'Owned PostgreSQL is not ready for the selected network')
    baseline = snapshot()
    require(baseline['noTimelineSidecar'], 'Pixel fixture already has a timeline sidecar')
    receipt, _ = pixel_fixture()
    require(all(baseline['subtitles'][extension] == receipt['subtitles']['.' + extension]['copyIdentity']
                for extension in EXTENSIONS), 'A pixel subtitle changed before startup')
    for port in (39221, 39222):
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', port))
    require(shutil.disk_usage(ROOT).free >= 1024 ** 3, 'Reconcile capacity before starting playback')
    for name in ('state', 'cache', 'runtime-logs'):
        ensure_directory(ROOT / name, 10001)
    for name in ('frames', 'mutations', 'browser-output'):
        ensure_directory(ROOT / name)
    analysis = {'enabled': True, 'cacheDirectory': '/var/cache/goby/media-analysis', 'cacheMaxBytes': 64 << 20,
                'cacheMaxEntries': 32, 'maxEntryBytes': 16 << 20, 'maxFileBytes': 8 << 20}
    analysis_path = ROOT / 'state/media-analysis.json'
    write(analysis_path, analysis)
    os.chown(analysis_path, 10001, 10001)
    credentials = urllib.parse.quote(config['databaseRole'], safe='') + ':' + urllib.parse.quote(config['databasePassword'], safe='')
    environment = {'GOBY_DATABASE_URL': 'postgres://' + credentials + '@' + PG + ':5432/' + config['databases']['app'] + '?sslmode=disable',
                   'GOBY_SETUP_TOKEN': config['setupToken'], 'GOBY_PUBLIC_URL': config['playerURL'], 'GOBY_COOKIE_SECURE': 'false',
                   'GOBY_LISTEN': ':8096', 'GOBY_WEB_DIR': '', 'GOBY_MEDIA_ROOTS': '/media',
                   'GOBY_SERVER_NAME': 'External bitmap playback acceptance',
                   'GOBY_MEDIA_ANALYSIS_FILE': '/var/lib/goby/media-analysis.json', 'GOBY_TRANSCODING_ENABLED': 'true',
                   'GOBY_TRANSCODE_CACHE': '/var/cache/goby/transcodes', 'GOBY_TRANSCODE_THREADS': '1',
                   'GOBY_TRANSCODE_MAX_JOBS': '1', 'GOBY_TRANSCODE_MAX_USER_JOBS': '1', 'GOBY_TRANSCODE_MAX_SESSION_JOBS': '1',
                   'GOBY_TRANSCODE_MAX_QUEUE_JOBS': '4', 'GOBY_TRANSCODE_MAX_RETAINED_JOBS': '8',
                   'GOBY_TRANSCODE_MAX_CACHE_BYTES': str(256 << 20), 'GOBY_TRANSCODE_MAX_JOB_BYTES': str(64 << 20),
                   'GOBY_TRANSCODE_MIN_FREE_BYTES': str(256 << 20), 'GOBY_HW_DECODER': 'software', 'GOBY_HW_ENCODER': 'software',
                   'GOBY_HW_DEVICE': '', 'GOBY_ALLOWED_AMD_DEVICES': '', 'GOBY_FFMPEG': FFMPEG, 'GOBY_FFPROBE': FFPROBE,
                   'GOBY_API_KEY_MASTER_KEY_FILE': '/var/lib/goby/application-key-master.key', 'GOBY_LOG_DIR': '/var/log/goby'}
    require(all('\n' not in value and '\r' not in value for value in environment.values()), 'Invalid private environment value')
    envfile = ROOT / 'backend.private.env'
    write(envfile, ''.join(key + '=' + value + '\n' for key, value in environment.items()))
    write(ROOT / 'logs/start-baseline.json', baseline)
    run(['docker', 'run', '-d', '--pull=never', '--name', BACKEND, '--label', 'goby.owner=' + OWNER,
         '--network', NETWORK, '--user=10001:10001', '--init', '--cpus=2', '--memory=1g', '--memory-swap=1g', '--pids-limit=128',
         *runtime_flags(), '--env-file', str(envfile), '-p', '127.0.0.1:39221:8096',
         '--mount', 'type=bind,source=' + str(ROOT / 'state') + ',target=/var/lib/goby',
         '--mount', 'type=bind,source=' + str(ROOT / 'cache') + ',target=/var/cache/goby',
         '--mount', 'type=bind,source=' + str(ROOT / 'runtime-logs') + ',target=/var/log/goby',
         '--mount', 'type=bind,source=' + str(ROOT / 'fixtures') + ',target=/media,readonly',
         '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=67108864,mode=0700,uid=10001,gid=10001', prepared['imageId']])
    run(['docker', 'run', '-d', '--pull=never', '--name', FRONTEND, '--label', 'goby.owner=' + OWNER,
         '--network', NETWORK, '--user=101:101', '--cpus=1', '--memory=256m', '--memory-swap=256m', '--pids-limit=64',
         *runtime_flags(), '-p', '127.0.0.1:39222:8080', '-e', 'GOBY_API_UPSTREAM=http://' + BACKEND + ':8096',
         '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=67108864,mode=0700,uid=101,gid=101', PLAYER])
    result = describe()
    require(all(item['running'] for item in result['containers']), 'An owned runtime exited during startup')
    write(ROOT / 'logs/runtime-started.json', result)
    return {'started': True, **result}


def container_summary(value):
    return {'name': value['Name'].lstrip('/'), 'id': value['Id'], 'imageId': value['Image'],
            'running': value['State']['Running'], 'user': value['Config']['User'],
            'ports': value['HostConfig']['PortBindings'], 'readonlyRootfs': value['HostConfig']['ReadonlyRootfs'],
            'mounts': [{'source': mount['Source'], 'target': mount['Destination'], 'readonly': not mount['RW']}
                       for mount in value['Mounts'] if mount['Type'] == 'bind']}


def describe():
    config = private()
    check_private(config)
    prepared = prepared_backend()
    current_binary = identity(owned(Path(prepared['binaryPath']), True))
    current = []
    for name, image in ((BACKEND, prepared['imageId']), (FRONTEND, PLAYER)):
        value = optional_container(name)
        if value is not None:
            require(value['Image'] == image and NETWORK in value['NetworkSettings']['Networks'], 'Runtime identity changed')
            current.append(container_summary(value))
    return {'owner': OWNER, 'root': str(ROOT), 'baseURL': config['baseURL'], 'playerURL': config['playerURL'],
            'ports': {'backend': 39221, 'player': 39222, 'postgres': 39223}, 'network': NETWORK,
            'imageId': prepared['imageId'], 'baseImageId': BASE, 'playerImageId': PLAYER,
            'applicationSHA256': prepared['applicationSHA256'], 'revision': prepared['revision'],
            'binaryPath': prepared['binaryPath'], 'binaryIdentity': current_binary,
            'binaryMatchesPrepared': current_binary == prepared['binaryIdentity'], 'mediaPath': config['mediaPath'],
            'libraryPath': str(Path(config['mediaPath']).parent), 'containers': current,
            'paths': {name: str(ROOT / name) for name in ('fixtures', 'state', 'cache', 'runtime-logs', 'frames', 'browser-output')},
            'sourceIdentities': snapshot()['sourceIdentities']}


def process_stamp(pid):
    try:
        fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(')', 1)[1].split()
        return fields[19]
    except FileNotFoundError:
        return None


def media_processes():
    result, lifetimes = [], {}
    for name in (BACKEND, FRONTEND):
        container = optional_container(name)
        if container is None or not container['State']['Running']:
            continue
        # Docker top supplies the owned container's host PIDs, never its argv.
        rows = run(['docker', 'top', name, '-eo', 'pid']).splitlines()[1:]
        require(len(rows) <= 256, 'Owned process inventory exceeded its bound')
        for row in rows:
            require(re.fullmatch(r'\s*[0-9]+\s*', row), 'Invalid owned process identifier')
            pid = int(row.strip())
            directory_path = Path('/proc') / str(pid)
            try:
                stamp = process_stamp(pid)
                executable = Path(os.readlink(directory_path / 'exe').removesuffix(' (deleted)')).name
                if executable not in ('ffmpeg', 'ffprobe'):
                    continue
                with (directory_path / 'cmdline').open('rb') as handle:
                    raw = handle.read(256 * 1024 + 1)
                require(len(raw) <= 256 * 1024, 'Owned media command exceeded its bound')
                arguments = raw.decode('utf-8', errors='replace').split('\x00')
                render_nodes = set()
                for descriptor in (directory_path / 'fd').iterdir():
                    try:
                        target = os.readlink(descriptor)
                        if re.fullmatch(r'/dev/dri/renderD[0-9]{3}', target):
                            render_nodes.add(target)
                    except FileNotFoundError:
                        pass
                if process_stamp(pid) != stamp:
                    continue
            except FileNotFoundError:
                continue
            encoder = any(arguments[index] in ('-c:v', '-codec:v', '-vcodec', '-c:v:0') and
                          arguments[index + 1] == 'libx264' for index in range(len(arguments) - 1))
            bitmap = any(re.search(r'\[goby_(?:v[0-9]+_)?subtitle\]', argument) is not None and
                         'overlay=' in argument for argument in arguments)
            result.append({'pid': pid, 'name': executable, 'encoder': encoder,
                           'bitmapBurn': bitmap, 'renderNodes': sorted(render_nodes)})
            lifetimes[pid] = stamp
    return result, lifetimes


def processes():
    media, _ = media_processes()
    return {'media': media, 'encoding': any(item['encoder'] for item in media),
            'bitmapBurn': any(item['bitmapBurn'] for item in media)}


def mutation_record(extension):
    path = ROOT / 'mutations' / (extension + '.json')
    if not path.exists():
        return None
    value = read_json(path)
    require(value['owner'] == OWNER and value['extension'] == extension, 'Unexpected mutation owner')
    return value


def check_fixture_baseline(except_extension=None):
    receipt, files = pixel_fixture()
    require(snapshot()['noTimelineSidecar'], 'The pixel fixture acquired a timeline sidecar')
    for extension in EXTENSIONS:
        if extension == except_extension:
            continue
        record = mutation_record(extension)
        require(record is None or record.get('restored') is True, 'Restore the outstanding mutation first')
        require(identity(files[extension]) == receipt['subtitles']['.' + extension]['copyIdentity'],
                'An unrelated subtitle change was observed')
    return receipt, files


def mutate(extension):
    require(extension in EXTENSIONS, 'mutate requires a supported extension')
    ensure_directory(ROOT / 'mutations')
    receipt, files = check_fixture_baseline()
    source = files[extension]
    original = identity(source)
    require(original == receipt['subtitles']['.' + extension]['copyIdentity'] and original['bytes'] <= 32 << 20,
            'The mutation target differs from the new pixel fixture')
    archive = ROOT / 'mutations' / (extension + '-' + str(time.time_ns()))
    directory(archive)
    backup = archive / 'original.bin'
    data = source.read_bytes()
    with backup.open('xb') as handle:
        handle.write(data)
    backup.chmod(0o600)
    require(digest(backup) == original['sha256'] and identity(source) == original, 'Mutation backup differs')
    before_stat = source.stat()
    value = {'owner': OWNER, 'extension': extension, 'path': str(source), 'backup': str(backup),
             'originalIdentity': original, 'originalAtimeNs': str(before_stat.st_atime_ns),
             'mutationSHA256': hashlib.sha256(data + b'\x00').hexdigest(), 'mutationBytes': len(data) + 1,
             'restored': False}
    write(archive / 'original.json', value)
    replace_json(ROOT / 'mutations' / (extension + '.json'), value)
    with source.open('ab') as handle:
        opened = os.fstat(handle.fileno())
        require(opened.st_dev == original['device'] and opened.st_ino == original['inode'], 'Opened mutation target differs')
        require(identity(source) == original, 'Mutation target changed before append')
        handle.write(b'\x00')
        handle.flush()
        os.fsync(handle.fileno())
    changed = identity(source)
    require(changed['sha256'] == value['mutationSHA256'] and changed['bytes'] == value['mutationBytes'], 'Mutation bytes differ')
    value['mutationIdentity'] = changed
    replace_json(ROOT / 'mutations' / (extension + '.json'), value)
    write(archive / 'mutated.json', value)
    return {'mutated': True, 'extension': extension, 'originalIdentity': original, 'mutationIdentity': changed,
            'sourceIdentities': snapshot()['sourceIdentities']}


def restore(extension):
    require(extension in EXTENSIONS, 'restore requires a supported extension')
    _, files = check_fixture_baseline(extension)
    value = mutation_record(extension)
    require(value is not None, 'There is no retained mutation to restore')
    source = files[extension]
    require(value['path'] == str(source), 'Mutation target changed')
    if value['restored']:
        require(identity(source) == value['originalIdentity'], 'The restored source changed')
        return {'restored': True, 'alreadyRestored': True, 'extension': extension, 'sourceIdentity': identity(source)}
    changed = identity(source)
    backup = owned(Path(value['backup']), True)
    require(backup.is_relative_to(ROOT / 'mutations') and backup.is_file() and
            digest(backup) == value['originalIdentity']['sha256'], 'The retained original bytes changed')
    already_original = all(changed[key] == value['originalIdentity'][key]
                           for key in ('sha256', 'bytes', 'device', 'inode', 'uid', 'gid', 'mode', 'links'))
    if not already_original:
        expected = value.get('mutationIdentity')
        require(changed['sha256'] == value['mutationSHA256'] and changed['bytes'] == value['mutationBytes'] and
                (expected is None or changed == expected) and
                all(changed[key] == value['originalIdentity'][key] for key in ('device', 'inode', 'uid', 'gid', 'mode', 'links')),
                'Refuse to overwrite an unrelated source change')
        with source.open('r+b') as writer, backup.open('rb') as reader:
            opened = os.fstat(writer.fileno())
            require(opened.st_dev == changed['device'] and opened.st_ino == changed['inode'], 'Opened restore target differs')
            require(identity(source) == changed, 'Restore target changed before writing')
            shutil.copyfileobj(reader, writer)
            writer.truncate()
            writer.flush()
            os.fsync(writer.fileno())
    os.utime(source, ns=(int(value['originalAtimeNs']), int(value['originalIdentity']['mtimeNs'])))
    restored = identity(source)
    require(restored == value['originalIdentity'], 'Restored source identity differs')
    restoration = backup.parent / 'restored.json'
    if restoration.exists():
        retained = read_json(restoration)
        require(retained['owner'] == OWNER and retained['extension'] == extension and retained['path'] == str(source) and
                retained['originalIdentity'] == value['originalIdentity'] and retained['restoredIdentity'] == restored and
                retained['restored'] is True, 'Retained restoration receipt changed')
        value = retained
    else:
        value.update({'restored': True, 'restoredIdentity': restored, 'restoredAtNs': str(time.time_ns())})
        write(restoration, value)
    replace_json(ROOT / 'mutations' / (extension + '.json'), value)
    return {'restored': True, 'alreadyRestored': already_original, 'extension': extension, 'sourceIdentity': restored,
            'sourceIdentities': snapshot()['sourceIdentities']}


def decode(source, seconds, output):
    require(source is not None and output is not None and seconds is not None and math.isfinite(seconds) and 0 <= seconds <= 3600,
            'decode requires input, bounded seconds and output')
    owned(source, True)
    owned(output)
    require(any(source.is_relative_to(ROOT / name) for name in ('logs', 'browser-output')) and source.is_file() and
            source.stat().st_nlink == 1 and 0 < source.stat().st_size <= 128 << 20, 'Decode input is outside the bounded evidence scope')
    require(output.is_relative_to(ROOT / 'frames') and output.suffix == '.rgba' and not output.exists(), 'Use a new owned RGBA frame')
    ensure_directory(ROOT / 'frames')
    relative = output.parent.relative_to(ROOT / 'frames')
    parent = ROOT / 'frames'
    for component in relative.parts:
        parent /= component
        ensure_directory(parent)
    receipt_path = output.with_suffix(output.suffix + '.json')
    require(not receipt_path.exists(), 'A frame receipt already exists')
    before = identity(source)
    log = ROOT / 'logs' / ('decode-' + str(time.time_ns()) + '.private.log')
    container_name = BACKEND + '-decode-' + str(time.time_ns())
    # Root has no capabilities or write mount; it only reads a private 0600
    # browser artifact. Backend and player processes remain non-root.
    arguments = ['docker', 'run', '--rm', '--pull=never', '--name', container_name, '--label', 'goby.owner=' + OWNER,
                 '--network=none', '--user=0:0', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges:true',
                 '--cpus=1', '--memory=256m', '--memory-swap=256m', '--pids-limit=64', '--log-driver=none',
                 '--mount', 'type=bind,source=' + str(source) + ',target=/input/media,readonly', '--entrypoint=' + FFMPEG, BASE,
                 '-hide_banner', '-nostdin', '-v', 'info', '-threads', '1', '-filter_threads', '1',
                 '-protocol_whitelist', 'file', '-format_whitelist', 'mov,mpegts,matroska,webm', '-i', '/input/media',
                 '-map', '0:v:0', '-an', '-sn', '-dn', '-vf',
                 'setpts=PTS-STARTPTS,select=gte(t\\,' + format(seconds, '.9f') + '),scale=320:192:flags=bilinear,format=rgba,showinfo',
                 '-frames:v', '1', '-fps_mode', 'passthrough', '-threads', '1', '-pix_fmt', 'rgba',
                 '-f', 'rawvideo', '-fs', str(FRAME_BYTES + 1), 'pipe:1']
    with output.open('xb') as frame, log.open('x') as diagnostics:
        output.chmod(0o600)
        log.chmod(0o600)
        try:
            result = subprocess.run(arguments, stdout=frame, stderr=diagnostics, timeout=120)
        except subprocess.TimeoutExpired:
            # Only the unique decoder created by this invocation may be stopped.
            current = json.loads(run(['docker', 'inspect', container_name]))[0]
            require((current['Config'].get('Labels') or {}).get('goby.owner') == OWNER and current['Image'] == BASE,
                    'Decoder ownership changed')
            run(['docker', 'stop', '--time=5', container_name], timeout=15)
            raise RuntimeError('Bounded frame decode timed out')
    require(result.returncode == 0 and output.stat().st_size == FRAME_BYTES, 'Decode did not produce exactly one RGBA frame')
    require(identity(source) == before, 'Decode input changed')
    require(log.stat().st_size <= 1024 * 1024, 'Frame diagnostics exceeded their bound')
    times = re.findall(r'\bn:\s*0\s+pts:\s*-?\d+\s+pts_time:([0-9.eE+-]+)', log.read_text())
    require(len(times) == 1 and math.isfinite(float(times[0])) and float(times[0]) + 0.000001 >= seconds,
            'Frame time could not be attributed to the requested offset')
    value = {'owner': OWNER, 'input': str(source), 'inputIdentity': before, 'output': str(output),
             'outputIdentity': identity(output), 'imageId': BASE, 'requestedSeconds': seconds,
             'frameSeconds': float(times[0]), 'clock': 'seconds from first decoded video frame',
             'width': 320, 'height': 192, 'pixelFormat': 'rgba', 'frameCount': 1, 'diagnostics': str(log)}
    write(receipt_path, value)
    return {'decoded': True, 'receipt': str(receipt_path), **value}


def stop():
    _, lifetimes = media_processes()
    stopped = []
    for name in (FRONTEND, BACKEND):
        value = optional_container(name)
        if value is not None and value['State']['Running']:
            run(['docker', 'stop', '--time=30', name], timeout=45)
            stopped.append(name)
    for _ in range(120):
        remaining = [pid for pid, stamp in lifetimes.items() if process_stamp(pid) == stamp]
        current = [optional_container(name) for name in (BACKEND, FRONTEND)]
        if not remaining and not any(value is not None and value['State']['Running'] for value in current):
            break
        time.sleep(0.25)
    else:
        raise RuntimeError('Owned playback workers have not exited')
    require(not processes()['media'], 'Owned media helpers remain')
    pg = optional_container(PG)
    value = {'stopped': stopped, 'mediaHelpersRemaining': 0, 'postgresRunning': pg is not None and pg['State']['Running']}
    write(ROOT / 'logs' / ('runtime-stopped-' + str(time.time_ns()) + '.json'), value)
    return value


def close_database():
    require(all(value is None or not value['State']['Running'] for value in
                (optional_container(BACKEND), optional_container(FRONTEND))) and not processes()['media'],
            'Stop playback and wait for its helpers before closing PostgreSQL')
    config = private()
    check_private(config)
    value = inspect(PG)
    baseline = read_json(ROOT / 'logs/database-prepared.json')
    require(value['Id'] == baseline['containerId'] and value['Image'] == baseline['imageId'], 'PostgreSQL identity changed')
    if value['State']['Running']:
        run(['docker', 'stop', '--time=30', PG], timeout=45)
    require(not inspect(PG)['State']['Running'], 'Owned PostgreSQL remains running')
    result = {'databaseClosed': True, 'container': PG, 'containerId': value['Id'], 'dataRetained': str(ROOT / 'pgdata')}
    write(ROOT / 'logs' / ('database-closed-' + str(time.time_ns()) + '.json'), result)
    return result


def main():
    require(os.name == 'posix' and ROOT.resolve(strict=True) == ROOT, 'Run only in the remote owned Linux scope')
    require((ROOT / '.owner').read_text().strip() == OWNER, 'Unexpected scope owner')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare-pixels', 'prepare-database', 'finish-database', 'prepare-backend', 'start',
                                         'processes', 'describe', 'snapshot', 'mutate', 'restore', 'decode', 'stop', 'close-database'])
    parser.add_argument('--fixture-name', default='External Bitmap Pixels')
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--binary-sha256')
    parser.add_argument('--revision')
    parser.add_argument('--extension', choices=EXTENSIONS)
    parser.add_argument('--input', type=Path)
    parser.add_argument('--seconds', type=float)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    # Serialize mutating actions without blocking read-only process sampling.
    lock = None
    if args.action not in ('describe', 'snapshot', 'processes'):
        import fcntl
        lock = owned(ROOT / 'live-operation.lock').open('a')
        os.chmod(ROOT / 'live-operation.lock', 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    actions = {'prepare-pixels': lambda: prepare_pixels(args.fixture_name), 'prepare-database': prepare_database,
               'finish-database': finish_database, 'prepare-backend': lambda: prepare_backend(args.binary, args.binary_sha256, args.revision),
               'start': start, 'processes': processes, 'describe': describe, 'snapshot': snapshot,
               'mutate': lambda: mutate(args.extension), 'restore': lambda: restore(args.extension),
               'decode': lambda: decode(args.input, args.seconds, args.output), 'stop': stop, 'close-database': close_database}
    value = actions[args.action]()
    print(json.dumps({'complete': True, 'action': args.action, **value}))


if __name__ == '__main__':
    try:
        main()
    except Exception:
        diagnostic = None
        try:
            if os.name == 'posix' and ROOT.resolve(strict=True) == ROOT and (ROOT / '.owner').read_text().strip() == OWNER:
                path = ROOT / 'logs' / ('helper-failure-' + str(time.time_ns()) + '.private.log')
                write(path, traceback.format_exc())
                diagnostic = str(path)
        except Exception:
            pass
        print(json.dumps({'complete': False, 'error': 'Owned helper failed; inspect the private diagnostic',
                          'diagnostic': diagnostic}), file=sys.stderr)
        raise SystemExit(1)
