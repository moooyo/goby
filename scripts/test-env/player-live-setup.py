#!/usr/bin/env python3
"""Prepare one isolated Docker-backed player acceptance environment."""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.request


def run(command, log=None, **kwargs):
    if log:
        with open(log, 'w') as output:
            subprocess.run(command, check=True, stdout=output, stderr=subprocess.STDOUT, **kwargs)
    else:
        subprocess.run(command, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True, type=Path)
    parser.add_argument('--reuse', action='store_true')
    parser.add_argument('--node-image', default='node:24-alpine')
    parser.add_argument('--nginx-image', default='nginxinc/nginx-unprivileged:1.29-alpine')
    parser.add_argument('--postgres-image', default='postgres:17-bookworm')
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    if (root / '.owner').read_text().strip() != 'goby-player-live-20261004-165c':
        raise RuntimeError('Unexpected acceptance workspace owner')
    os.umask(0o077)
    artifacts = root / 'artifacts'
    artifacts.mkdir(exist_ok=True)
    source = root / 'source'
    private_path = root / 'private.json'
    if private_path.exists():
        private = json.loads(private_path.read_text())
    else:
        private = {'name': 'player-acceptance', 'password': secrets.token_urlsafe(24),
                   'setup': secrets.token_urlsafe(30), 'database': secrets.token_urlsafe(24)}
        private_path.write_text(json.dumps(private))
    if not private.get('pgadmin'):
        private['pgadmin'] = secrets.token_urlsafe(24)
        private_path.write_text(json.dumps(private))
    for name, owner in [('state', 10001), ('cache', 10001), ('logs', 10001), ('media', 10001), ('pgdata', 999)]:
        path = root / name
        path.mkdir(exist_ok=True)
        os.chown(path, owner, owner)
        path.chmod(0o755 if name == 'media' else 0o700)
    media = root / 'media'
    for directory in [media / 'Movies' / 'Player Integration (2026)', media / 'Shows' / 'Player Series' / 'Season 01']:
        directory.mkdir(parents=True, exist_ok=True)
        for parent in [directory, *directory.parents]:
            if parent == root:
                break
            parent.chmod(0o755)
            os.chown(parent, 10001, 10001)
    movie = media / 'Movies' / 'Player Integration (2026)' / 'Player Integration.mp4'
    if not movie.exists():
        output = '/out/' + str(movie.relative_to(media))
        run(['docker', 'run', '--rm', '--network', 'none', '--user', '10001:10001',
             '-v', f'{media}:/out', '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg',
             'goby-player-accepted-base:20261004', '-hide_banner', '-loglevel', 'error',
             '-f', 'lavfi', '-i', 'testsrc2=size=1920x1080:rate=24',
             '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000',
             '-f', 'lavfi', '-i', 'sine=frequency=660:sample_rate=48000',
             '-t', '180', '-map', '0:v', '-map', '1:a', '-map', '2:a', '-c:v', 'libx264',
             '-preset', 'ultrafast', '-crf', '28', '-pix_fmt', 'yuv420p', '-g', '48',
             '-threads', '2', '-c:a', 'aac', '-b:a', '96k', '-ac', '2',
             '-metadata:s:a:0', 'language=eng', '-metadata:s:a:1', 'language=chi',
             '-movflags', '+faststart', output], log=artifacts / 'generate-media.log')
    duration = float(subprocess.check_output(['docker', 'run', '--rm', '--network', 'none', '-v', f'{media}:/out:ro',
        '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffprobe', 'goby-player-accepted-base:20261004', '-v', 'error',
        '-show_entries', 'format=duration', '-of', 'default=nw=1:nk=1', '/out/' + str(movie.relative_to(media))], text=True))
    if duration < 120:
        temporary = movie.with_name('extended-source.mp4')
        run(['docker', 'run', '--rm', '--network', 'none', '--user', '10001:10001', '-v', f'{media}:/out',
             '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg', 'goby-player-accepted-base:20261004', '-v', 'error', '-y',
             '-stream_loop', '2', '-i', '/out/' + str(movie.relative_to(media)), '-map', '0', '-c', 'copy', '-t', '180',
             '-movflags', '+faststart', '/out/' + str(temporary.relative_to(media))], log=artifacts / 'extend-source.log')
        temporary.replace(movie)
    movie.with_suffix('.nfo').write_text('<movie><title>Player Integration</title><year>2026</year><genre>Documentary</genre><plot>Real Docker playback acceptance.</plot><actor><name>Acceptance Actor</name><role>Host</role></actor></movie>')
    subtitle = '1\n00:00:00,000 --> 00:00:30,000\nReal server subtitle, first window.\n\n2\n00:00:30,000 --> 00:01:00,000\nReal server subtitle, second window.\n'
    movie.with_name(movie.stem + '.eng.srt').write_text(subtitle)
    for number in [1, 2]:
        episode = media / 'Shows' / 'Player Series' / 'Season 01' / f'Player Series S01E{number:02d}.mp4'
        if not episode.exists():
            os.link(movie, episode)
        episode.with_suffix('.nfo').write_text(f'<episodedetails><title>Window {number}</title><season>1</season><episode>{number}</episode><plot>Real episode selection and source-clock acceptance.</plot></episodedetails>')
        episode.with_name(episode.stem + '.eng.srt').write_text(subtitle)
    (media / 'Shows' / 'Player Series' / 'tvshow.nfo').write_text('<tvshow><title>Player Series</title><year>2026</year><genre>Documentary</genre></tvshow>')
    for path in media.rglob('*'):
        if path.is_file():
            path.chmod(0o644)
    application = {'GOBY_DATABASE_URL': f'postgres://player_app:{private["database"]}@db:5432/player?sslmode=disable',
                   'GOBY_PUBLIC_URL': 'http://127.0.0.1:38974', 'GOBY_SERVER_NAME': 'Player acceptance',
                   'GOBY_SETUP_TOKEN': private['setup'], 'GOBY_COOKIE_SECURE': 'false'}
    (root / 'goby.env').write_text(''.join(f'{key}={value}\n' for key, value in application.items()))
    deployment = {'GOBY_OCI_IMAGE': 'goby-player-backend:step1', 'GOBY_CONFIG_FILE': str(root / 'goby.env'),
                  'GOBY_HOST_PORT': '38973', 'GOBY_PLAYER_HOST_PORT': '38974', 'GOBY_PLAYER_IMAGE': 'goby-player-ui:step1',
                  'GOBY_STATE_DIR': str(root / 'state'), 'GOBY_CACHE_DIR': str(root / 'cache'),
                  'GOBY_LOG_DIR_HOST': str(root / 'logs'), 'GOBY_MEDIA_DIR': str(media),
                  'GOBY_CPU_LIMIT': '2.0', 'GOBY_MEMORY_LIMIT': '2g'}
    (root / 'deployment.env').write_text(''.join(f'{key}={value}\n' for key, value in deployment.items()))
    override = {'services': {
        'db': {'image': args.postgres_image, 'environment': {'POSTGRES_USER': 'fixture_admin', 'POSTGRES_DB': 'postgres', 'POSTGRES_PASSWORD': private['pgadmin']},
               'volumes': [f'{root / "pgdata"}:/var/lib/postgresql/data'],
               'healthcheck': {'test': ['CMD-SHELL', 'pg_isready -U fixture_admin -d postgres'], 'interval': '2s', 'timeout': '3s', 'retries': 30}},
        'goby': {'depends_on': {'db': {'condition': 'service_healthy'}}},
        'player': {'build': {'args': {'NODE_IMAGE': args.node_image, 'NGINX_IMAGE': args.nginx_image}}}}}
    (root / 'compose.acceptance.json').write_text(json.dumps(override))
    compose = ['docker', 'compose', '-p', 'goby-player-165c', '--env-file', str(root / 'deployment.env'),
               '-f', str(source / 'deploy/oci/compose.yaml'), '-f', str(source / 'deploy/oci/compose.player.yaml'),
               '-f', str(root / 'compose.acceptance.json')]
    if not args.reuse:
        for port in [38973, 38974]:
            with socket.socket() as probe:
                probe.bind(('127.0.0.1', port))
        run(compose + ['build', 'player'], log=artifacts / 'player-image-build.log')
        run(compose + ['up', '-d', 'db'], log=artifacts / 'database-start.log')
        for _ in range(60):
            probe = subprocess.run(['docker', 'exec', 'goby-player-165c-db-1', 'pg_isready'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            if probe.returncode == 0:
                break
            time.sleep(1)
        def sql(role, statement):
            return subprocess.run(['docker', 'exec', '-i', 'goby-player-165c-db-1', 'psql', '-v', 'ON_ERROR_STOP=1', '-U', role, '-d', 'postgres', '-At'],
                                  input=statement, text=True, capture_output=True)
        administrator = 'fixture_admin'
        if sql(administrator, 'SELECT 1;').returncode:
            administrator = 'player'
        prepared = sql(administrator, f"""DO $body$ BEGIN
          IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='fixture_admin') THEN CREATE ROLE fixture_admin LOGIN SUPERUSER; END IF;
          IF NOT EXISTS(SELECT FROM pg_roles WHERE rolname='player_app') THEN CREATE ROLE player_app LOGIN; END IF;
          END $body$;
          ALTER ROLE fixture_admin WITH SUPERUSER LOGIN PASSWORD '{private['pgadmin']}';""")
        if prepared.returncode:
            raise RuntimeError('Could not provision the dedicated non-superuser application role')
        prepared = sql('fixture_admin', f"ALTER ROLE player_app WITH NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS LOGIN PASSWORD '{private['database']}';")
        if prepared.returncode:
            raise RuntimeError('Could not constrain the dedicated application role')
        if sql('fixture_admin', "SELECT 1 FROM pg_database WHERE datname='player';").stdout.strip() != '1':
            if sql('fixture_admin', 'CREATE DATABASE player OWNER player_app TEMPLATE template0;').returncode:
                raise RuntimeError('Could not create the dedicated application database')
        if sql('fixture_admin', 'ALTER DATABASE player OWNER TO player_app;').returncode:
            raise RuntimeError('Could not establish the application database owner')
        run(compose + ['up', '-d'], log=artifacts / 'compose-start.log')
    base = 'http://127.0.0.1:38974'
    def request(method, path, body=None, token=None):
        headers = {'Content-Type': 'application/json', 'X-Emby-Authorization': 'Emby Client="Player Acceptance", Device="Remote Browser", DeviceId="goby-player-live-165c", Version="1.0"'}
        if token:
            headers['X-Emby-Token'] = token
        data = None if body is None else json.dumps(body).encode()
        with urllib.request.urlopen(urllib.request.Request(base + path, data=data, method=method, headers=headers), timeout=120) as response:
            payload = response.read()
            return json.loads(payload) if payload else None
    for _ in range(90):
        try:
            request('GET', '/emby/System/Info/Public')
            break
        except (OSError, urllib.error.HTTPError):
            time.sleep(1)
    else:
        raise RuntimeError('The Docker-backed player did not become ready')
    if not private.get('bootstrapped'):
        request('POST', '/admin/v1/bootstrap', {'SetupToken': private['setup'], 'Name': private['name'], 'Password': private['password']})
        private['bootstrapped'] = True
        private_path.write_text(json.dumps(private))
    auth = request('POST', '/emby/Users/AuthenticateByName', {'Username': private['name'], 'Pw': private['password']})
    token = auth['AccessToken']
    if not private.get('initialized'):
        # Native administrator mutations require their session cookie, not an Emby token.
        import http.cookiejar
        jar = http.cookiejar.CookieJar()
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
        csrf = ''
        def admin(method, path, body=None):
            req = urllib.request.Request(base + path, data=None if body is None else json.dumps(body).encode(), method=method,
                                         headers={'Content-Type': 'application/json', 'Origin': base, 'X-CSRF-Token': csrf})
            with opener.open(req, timeout=120) as response:
                payload = response.read()
                return json.loads(payload) if payload else None
        csrf = admin('POST', '/admin/v1/session', {'Name': private['name'], 'Password': private['password']})['CSRFToken']
        for name, kind, path in [('Player movies', 'movies', '/media/Movies'), ('Player shows', 'tvshows', '/media/Shows')]:
            created = admin('POST', '/admin/v1/libraries', {'Name': name, 'CollectionType': kind, 'Paths': [path], 'Scan': True})
            job = created['Job']['Id']
            for _ in range(150):
                jobs = admin('GET', '/admin/v1/jobs')['Items']
                status = next((entry['Status'].lower() for entry in jobs if entry['Id'] == job), '')
                if status in ['completed', 'succeeded']:
                    break
                if status in ['failed', 'canceled']:
                    raise RuntimeError(f'Library scan failed: {name}')
                time.sleep(1)
            else:
                raise RuntimeError(f'Library scan timed out: {name}')
        private['initialized'] = True
        private_path.write_text(json.dumps(private))
    items = request('GET', '/emby/Items?IncludeItemTypes=Movie,Series,Episode&Recursive=true&Fields=MediaSources,MediaStreams,Chapters', token=token)
    (artifacts / 'catalog.json').write_text(json.dumps(items, indent=2))
    (root / 'browser.private.json').write_text(json.dumps({'baseURL': base, 'username': private['name'], 'password': private['password'], 'token': token, 'userId': auth['User']['Id']}))
    print(json.dumps({'ready': True, 'baseURL': base, 'items': items['TotalRecordCount'], 'project': 'goby-player-165c'}))


if __name__ == '__main__':
    main()
