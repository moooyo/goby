#!/usr/bin/env python3
"""Record filter behavior from a dedicated official Emby 4.9.5.0 container."""
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import time
import urllib.request
import urllib.error
import urllib.parse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True, type=Path)
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    assert (root / '.owner').read_text().strip() == 'goby-player-live-20261004-165c'
    os.umask(0o077)
    media = root / 'reference-media'
    config = root / 'reference-config'
    media.mkdir(exist_ok=True)
    config.mkdir(exist_ok=True)
    media.chmod(0o755)
    cases = [('hd', 1920, 1080, 'bt709'), ('portrait', 1920, 2160, 'bt709'),
             ('wide3000', 3000, 1600, 'bt709'), ('wide3600', 3600, 2000, 'bt709'),
             ('boundary3700', 3700, 2100, 'bt709'), ('boundary3798', 3798, 2100, 'bt709'),
             ('near4k', 3800, 2100, 'bt709'), ('scope4k', 3840, 1600, 'bt709'),
             ('cinema4k', 4096, 1716, 'bt709'), ('uhd', 3840, 2160, 'bt709'),
             ('hdr10', 1920, 1080, 'smpte2084'), ('hlg', 1920, 1080, 'arib-std-b67'),
             ('hdr10metadata', 1920, 1080, 'smpte2084'), ('hlgmetadata', 1920, 1080, 'arib-std-b67')]
    for name, width, height, transfer in cases:
        destination = media / f'{name}.mp4'
        if not destination.exists():
            command = ['docker', 'run', '--rm', '--network', 'none', '--user', '0:0', '-v', f'{media}:/out',
                       '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg', 'goby-player-accepted-base:20261004',
                       '-v', 'error', '-f', 'lavfi', '-i', f'color=c=blue:size={width}x{height}:rate=1', '-t', '2', '-an',
                       '-c:v', 'libx264' if transfer == 'bt709' else 'libx265', '-preset', 'ultrafast', '-threads', '1',
                       '-pix_fmt', 'yuv420p' if transfer == 'bt709' else 'yuv420p10le',
                       '-color_trc', transfer, '-color_primaries', 'bt709' if transfer == 'bt709' else 'bt2020',
                       '-colorspace', 'bt709' if transfer == 'bt709' else 'bt2020nc']
            if transfer != 'bt709':
                parameters = 'pools=1:frame-threads=1:log-level=error'
                if name.endswith('metadata'):
                    parameters += ':colorprim=bt2020:transfer=' + ('smpte2084' if transfer == 'smpte2084' else 'arib-std-b67') + ':colormatrix=bt2020nc'
                command += ['-x265-params', parameters]
            subprocess.run(command + ['-movflags', '+faststart', '/out/' + destination.name], check=True, stdout=subprocess.DEVNULL)
        destination.chmod(0o644)
    multistream_cases = [('mixed4kfirst', 'uhd', 'hdr10metadata'), ('mixedhdfirst', 'hdr10metadata', 'uhd'), ('mixedhddefault', 'uhd', 'hdr10metadata')]
    for name, first, second in multistream_cases:
        destination = media / f'{name}.mp4'
        if not destination.exists():
            command = ['docker', 'run', '--rm', '--network', 'none', '--user', '0:0', '-v', f'{media}:/out',
                       '--entrypoint', '/opt/ffmpeg/9.0.1/bin/ffmpeg', 'goby-player-accepted-base:20261004',
                       '-v', 'error', '-i', f'/out/{first}.mp4', '-i', f'/out/{second}.mp4',
                       '-map', '0:v', '-map', '1:v', '-c', 'copy']
            if name == 'mixedhddefault':
                command += ['-disposition:v:0', '0', '-disposition:v:1', 'default']
            subprocess.run(command + ['-movflags', '+faststart', '/out/' + destination.name], check=True, stdout=subprocess.DEVNULL)
        destination.chmod(0o644)
    television = config / 'test-tv'
    television.mkdir(exist_ok=True)
    television.chmod(0o755)
    for series, sources in [('Mixed resolution', ['uhd', 'hd']), ('HD only', ['hd']), ('HDR only', ['hdr10metadata'])]:
        folder = television / series / 'Season 01'
        folder.mkdir(parents=True, exist_ok=True)
        folder.parent.chmod(0o755)
        folder.chmod(0o755)
        for index, source in enumerate(sources, 1):
            destination = folder / f'{series} - S01E{index:02d}.mp4'
            if not destination.exists():
                shutil.copyfile(media / f'{source}.mp4', destination)
            destination.chmod(0o644)
    private_path = root / 'reference.private.json'
    if private_path.exists():
        private = json.loads(private_path.read_text())
    else:
        private = {'username': 'player-reference', 'password': secrets.token_hex(24)}
        private_path.write_text(json.dumps(private))
    if not (config / 'config' / 'system.xml').exists():
        (config / 'config').mkdir(exist_ok=True)
        (config / 'config' / 'system.xml').write_text('''<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema"><HttpServerPortNumber>8096</HttpServerPortNumber><EnableUPnP>false</EnableUPnP><EnableRemoteAccess>true</EnableRemoteAccess><EnableAutoUpdate>false</EnableAutoUpdate><EnableAutomaticRestart>false</EnableAutomaticRestart><AutoRunWebApp>false</AutoRunWebApp><EnableExternalContentInSuggestions>false</EnableExternalContentInSuggestions><ServerName>Player API reference</ServerName><DatabaseCacheSizeMB>64</DatabaseCacheSizeMB></ServerConfiguration>''')
    if subprocess.run(['docker', 'network', 'inspect', 'goby-player-reference-165c'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
        subprocess.run(['docker', 'network', 'create', '--internal', 'goby-player-reference-165c'], check=True, stdout=subprocess.DEVNULL)
    if subprocess.run(['docker', 'container', 'inspect', 'goby-player-reference-165c'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
        subprocess.run(['docker', 'run', '-d', '--name', 'goby-player-reference-165c', '--network', 'goby-player-reference-165c',
                        '--cpus', '1.5', '--memory', '1g', '--pids-limit', '256', '-e', 'UID=10001', '-e', 'GID=10001',
                        '-v', f'{config}:/config', '-v', f'{media}:/media:ro',
                        'emby/embyserver:4.9.5.0'], check=True, stdout=subprocess.DEVNULL)
    else:
        subprocess.run(['docker', 'start', 'goby-player-reference-165c'], check=True, stdout=subprocess.DEVNULL)
    # Docker internal networks do not publish host ports. Query only the owned
    # bridge address so the official reference remains isolated from Internet.
    container = json.loads(subprocess.check_output(['docker', 'container', 'inspect', 'goby-player-reference-165c']))[0]
    address = container['NetworkSettings']['Networks']['goby-player-reference-165c']['IPAddress']
    base = f'http://{address}:8096/emby'
    token = ''
    def request(method, path, body=None, form=False):
        headers = {'Accept': 'application/json', 'X-Emby-Authorization': 'Emby Client="Goby Filter Reference", Device="Test worker", DeviceId="player-filter-reference-165c", Version="1.0"'}
        if token:
            headers['X-Emby-Token'] = token
        if form:
            data = urllib.parse.urlencode(body).encode()
            headers['Content-Type'] = 'application/x-www-form-urlencoded'
        else:
            data = None if body is None else json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        try:
            with urllib.request.urlopen(urllib.request.Request(base + path, data=data, method=method, headers=headers), timeout=30) as response:
                raw = response.read()
                return response.status, json.loads(raw) if raw else None
        except urllib.error.HTTPError as error:
            return error.code, None
    for _ in range(90):
        try:
            status, system = request('GET', '/System/Info/Public')
            if status == 200:
                break
        except OSError:
            pass
        time.sleep(1)
    else:
        raise RuntimeError('The isolated official reference is unavailable')
    assert system['Version'] == '4.9.5.0'
    if not private.get('initialized'):
        assert request('GET', '/Startup/User')[0] == 200
        assert request('POST', '/Startup/User', {'Name': private['username'], 'Password': private['password']}, form=True)[0] == 200
        request('POST', '/Startup/RemoteAccess', {'EnableAutomaticPortMapping': 'false'}, form=True)
        assert request('POST', '/Startup/Complete')[0] in [200, 204]
        private['initialized'] = True
        private_path.write_text(json.dumps(private))
    status, authentication = request('POST', '/Users/AuthenticateByName', {'Username': private['username'], 'Pw': private['password']})
    assert status == 200
    token = authentication['AccessToken']
    user = authentication['User']['Id']
    if not private.get('library'):
        options = {'EnableRealtimeMonitor': False, 'EnableChapterImageExtraction': False,
                   'ExtractChapterImagesDuringLibraryScan': False, 'EnableMarkerDetection': False,
                   'EnableMarkerDetectionDuringLibraryScan': False, 'SaveLocalMetadata': False,
                   'SampleIgnoreSize': 0, 'AutomaticRefreshIntervalDays': 0, 'SubtitleDownloadLanguages': [],
                   'MetadataSavers': [], 'TypeOptions': [{'Type': kind, 'MetadataFetchers': [], 'ImageFetchers': []} for kind in ['Movie', 'Video', 'Folder']]}
        status, _ = request('POST', '/Library/VirtualFolders', {'Name': 'Filter reference', 'CollectionType': 'movies', 'RefreshLibrary': False, 'Paths': ['/media'], 'LibraryOptions': options})
        assert status in [200, 204]
        private['library'] = True
        private_path.write_text(json.dumps(private))
    if not private.get('tv_library'):
        options = {'EnableRealtimeMonitor': False, 'EnableChapterImageExtraction': False,
                   'ExtractChapterImagesDuringLibraryScan': False, 'EnableMarkerDetection': False,
                   'EnableMarkerDetectionDuringLibraryScan': False, 'SaveLocalMetadata': False,
                   'SampleIgnoreSize': 0, 'AutomaticRefreshIntervalDays': 0, 'SubtitleDownloadLanguages': [],
                   'MetadataSavers': [], 'TypeOptions': [{'Type': kind, 'MetadataFetchers': [], 'ImageFetchers': []} for kind in ['Series', 'Season', 'Episode', 'Folder']]}
        status, _ = request('POST', '/Library/VirtualFolders', {'Name': 'TV filter reference', 'CollectionType': 'tvshows', 'RefreshLibrary': False, 'Paths': ['/config/test-tv'], 'LibraryOptions': options})
        assert status in [200, 204]
        private['tv_library'] = True
        private_path.write_text(json.dumps(private))
    request('POST', '/Library/Refresh')
    query = {'UserId': user, 'IncludeItemTypes': 'Movie', 'Recursive': 'true', 'Fields': 'MediaStreams,MediaSources', 'Limit': '100'}
    for _ in range(120):
        status, items = request('GET', '/Items?' + urllib.parse.urlencode(query))
        if status == 200 and items['TotalRecordCount'] == len(cases) + len(multistream_cases):
            break
        time.sleep(1)
    else:
        raise RuntimeError('Reference scan did not publish every generated source')
    for _ in range(120):
        status, episodes = request('GET', '/Items?' + urllib.parse.urlencode({**query, 'IncludeItemTypes': 'Episode'}))
        if status == 200 and episodes['TotalRecordCount'] == 4:
            break
        time.sleep(1)
    else:
        raise RuntimeError('Reference scan did not publish every generated episode')
    records = []
    controls = [{}, {'Is4K': 'true'}, {'Is4K': 'false'}, {'MinWidth': '3840'}, {'ExtendedVideoTypes': 'Hdr10'},
                {'ExtendedVideoTypes': 'HyperLogGamma'}, {'ExtendedVideoTypes': 'None'},
                {'ExtendedVideoTypes': 'Hdr10,HyperLogGamma'}, {'Is4K': 'true', 'ExtendedVideoTypes': 'Hdr10'},
                {'Is4K': 'invalid'}, {'ExtendedVideoTypes': 'invalid'}]
    for control in controls:
        status, result = request('GET', '/Items?' + urllib.parse.urlencode({**query, **control}))
        record = {'query': control, 'status': status, 'total': result.get('TotalRecordCount') if isinstance(result, dict) else None,
                  'names': sorted(item['Name'] for item in result.get('Items', [])) if isinstance(result, dict) else []}
        records.append(record)
        print(json.dumps(record), flush=True)
    movie_streams = {item['Name']: item.get('MediaStreams', []) for item in items['Items']}
    tv_records = []
    for kind in ['Episode', 'Series', 'Season']:
        for control in [{}, {'Is4K': 'true'}, {'Is4K': 'false'}, {'ExtendedVideoTypes': 'Hdr10'}, {'ExtendedVideoTypes': 'None'}]:
            status, result = request('GET', '/Items?' + urllib.parse.urlencode({**query, 'IncludeItemTypes': kind, **control}))
            record = {'kind': kind, 'query': control, 'status': status, 'total': result.get('TotalRecordCount') if isinstance(result, dict) else None,
                      'names': sorted(item['Name'] for item in result.get('Items', [])) if isinstance(result, dict) else []}
            tv_records.append(record)
            print(json.dumps(record), flush=True)
    (root / 'artifacts/emby-filter-reference.json').write_text(json.dumps({'version': system['Version'], 'cases': cases, 'multistreamCases': multistream_cases, 'results': records, 'tvResults': tv_records, 'movieStreams': movie_streams}, indent=2))
    request('POST', '/Sessions/Logout')


if __name__ == '__main__':
    main()
