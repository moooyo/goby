#!/usr/bin/env python3
"""Add small capability fixtures to the explicitly owned live player environment."""

import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True, type=Path)
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    if (root / '.owner').read_text().strip() != 'goby-player-live-20261004-165c':
        raise RuntimeError('Unexpected acceptance workspace owner')
    os.umask(0o077)
    config = json.loads((root / 'browser.private.json').read_text())
    base = config['baseURL']
    if base != 'http://127.0.0.1:38974':
        raise RuntimeError('Unexpected acceptance origin')
    media = (root / 'media').resolve(strict=True)
    media.relative_to(root)
    movie_directory = media / 'Movies' / 'Player Integration (2026)'
    source = movie_directory / 'Player Integration.mp4'
    source.resolve(strict=True).relative_to(media)
    backdrop_directory = movie_directory / 'backdrops'
    backdrop_directory.mkdir(exist_ok=True)
    backdrop_directory.resolve(strict=True).relative_to(media)
    os.chown(backdrop_directory, 10001, 10001)
    backdrop_directory.chmod(0o755)
    backdrop = backdrop_directory / 'acceptance-preview.mp4'
    if backdrop.is_symlink():
        raise RuntimeError('The owned preview target must not be a symlink')
    if not backdrop.exists():
        subprocess.run([
            'docker', 'run', '--rm', '--network', 'none', '--user', '10001:10001',
            '-v', f'{media}:/out', '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg',
            'goby-player-accepted-base:20261004', '-hide_banner', '-loglevel', 'error',
            '-i', '/out/' + str(source.relative_to(media)), '-t', '3', '-map', '0:v:0',
            '-an', '-vf', 'scale=1280:-2', '-c:v', 'libx264', '-preset', 'ultrafast',
            '-crf', '30', '-pix_fmt', 'yuv420p', '-threads', '2', '-movflags', '+faststart',
            '/out/' + str(backdrop.relative_to(media)),
        ], check=True, stdout=subprocess.DEVNULL)
    backdrop.chmod(0o644)

    filters = [
        ('near4k.mp4', 'Filter 4K Boundary', True, False),
        ('boundary3798.mp4', 'Filter Below 4K', False, False),
        ('hdr10metadata.mp4', 'Filter HDR10', False, True),
        ('hlgmetadata.mp4', 'Filter HLG', False, True),
    ]
    for filename, title, _, _ in filters:
        reference = (root / 'reference-media' / filename).resolve(strict=True)
        reference.relative_to(root / 'reference-media')
        directory = media / 'Movies' / title
        directory.mkdir(exist_ok=True)
        directory.resolve(strict=True).relative_to(media)
        os.chown(directory, 10001, 10001)
        directory.chmod(0o755)
        target = directory / (title + '.mp4')
        if target.is_symlink():
            raise RuntimeError('An owned filter target must not be a symlink')
        if not target.exists():
            shutil.copyfile(reference, target)
        target.chmod(0o644)
        nfo = target.with_suffix('.nfo')
        if nfo.is_symlink():
            raise RuntimeError('An owned metadata target must not be a symlink')
        nfo.write_text(f'<movie><title>{title}</title><year>2026</year><plot>Real source filter acceptance.</plot></movie>')
        nfo.chmod(0o644)

    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    csrf = ''

    def request(method, path, body=None, admin=True):
        headers = {'Content-Type': 'application/json', 'Origin': base}
        if admin:
            headers['X-CSRF-Token'] = csrf
        else:
            headers['X-Emby-Token'] = config['token']
        req = urllib.request.Request(base + path, method=method, headers=headers,
                                     data=None if body is None else json.dumps(body).encode())
        with opener.open(req, timeout=120) as response:
            payload = response.read()
            return json.loads(payload) if payload else None

    csrf = request('POST', '/admin/v1/session', {'Name': config['username'], 'Password': config['password']})['CSRFToken']
    libraries = request('GET', '/admin/v1/libraries')['Items']
    library = next(item for item in libraries if '/media/Movies' in item['Paths'])
    job = request('POST', f'/admin/v1/libraries/{library["Id"]}/scan', {'ForceProbe': False})['Job']['Id']
    for _ in range(150):
        status = next((item['Status'].lower() for item in request('GET', '/admin/v1/jobs')['Items'] if item['Id'] == job), '')
        if status in ['completed', 'succeeded']:
            break
        if status in ['failed', 'canceled']:
            raise RuntimeError('Capability fixture library scan failed')
        time.sleep(1)
    else:
        raise RuntimeError('Capability fixture library scan timed out')
    items = request('GET', '/emby/Items?IncludeItemTypes=Movie&Recursive=true', admin=False)['Items']
    movie = next(item for item in items if item['Name'] == 'Player Integration')
    themes = request('GET', f'/emby/Items/{movie["Id"]}/ThemeMedia?EnableThemeSongs=false&EnableThemeVideos=true&Fields=MediaSources,MediaStreams', admin=False)
    if not themes['ThemeVideosResult']['Items']:
        raise RuntimeError('The real scanner did not publish the generated theme video')
    artifacts = (root / 'artifacts').resolve(strict=True)
    artifacts.relative_to(root)
    output = artifacts / 'live-capabilities'
    output.mkdir(exist_ok=True)
    output.resolve(strict=True).relative_to(root)
    if output.is_symlink() or (output / 'fixture.json').is_symlink():
        raise RuntimeError('Capability evidence targets must not be symbolic links')
    manifest = {'movieId': movie['Id'], 'themeCount': themes['ThemeVideosResult']['TotalRecordCount'],
                'filters': [{'name': title, 'is4K': is_4k, 'isHDR': is_hdr} for _, title, is_4k, is_hdr in filters]}
    (output / 'fixture.json').write_text(json.dumps(manifest, indent=2))
    print(json.dumps({'ready': True, 'themeVideos': manifest['themeCount'], 'filterMovies': len(filters)}))


if __name__ == '__main__':
    main()
