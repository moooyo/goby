"""Coordinate only the retained software installation during image refresh."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.request

BASE = Path('/opt/goby-test/player-release-20261005-165c')
ROOT = BASE / 'software-upgrade'
INSTALL = ROOT / 'installation'
REFRESH = BASE / 'refresh-20261006'
OWNER = 'goby-player-release-165c-20261005'
OLD_IMAGE = 'sha256:f61d774ca909094d917b3c4e9a806397718fafc20e9fe6e73800335dc6e42d2c'
PLAYER = 'sha256:7140ce5c531a302b0aca595ee47ce98c52cd08ed73d5a00e5a27972d1ad94192'
assert os.name == 'posix' and BASE.resolve(strict=True) == BASE
assert (BASE / '.owner').read_text().strip() == OWNER
assert INSTALL.resolve(strict=True) == INSTALL
REFRESH.mkdir(exist_ok=True, mode=0o700)
assert REFRESH.resolve(strict=True) == REFRESH
EVIDENCE = REFRESH / 'software-runtime'
EVIDENCE.mkdir(exist_ok=True, mode=0o700)


def run(args, timeout=180):
    value = subprocess.run(args, capture_output=True, timeout=timeout)
    if value.returncode:
        path = EVIDENCE / ('private-command-failure-' + str(time.time_ns()) + '.log')
        path.write_bytes(value.stdout + value.stderr)
        path.chmod(0o600)
        raise RuntimeError('Owned runtime operation failed; private diagnostics retained')
    return value.stdout


def write(path, data):
    if isinstance(data, dict):
        data = (json.dumps(data, indent=2) + '\n').encode()
    path.write_bytes(data)
    path.chmod(0o600)


def inspect(container):
    return json.loads(run(['docker', 'inspect', container]))[0]


def metadata():
    value = json.loads((INSTALL / 'installation.json').read_text())
    assert value['directory'] == str(INSTALL) and value['project_name'] == 'goby-f2cffcccc1e82484'
    assert value['player']['image_id'] == PLAYER and value['host_port'] == 39111
    return value


def compose(arguments):
    meta = metadata()
    args = ['docker', 'compose', '--project-directory', str(INSTALL), '--project-name', meta['project_name'], '--env-file', str(INSTALL / 'deployment.env')]
    for name in meta['compose_files']:
        assert name in {'compose.yaml', 'compose.background-previews.yaml', 'compose.player.yaml', 'compose.install.yaml'}
        args += ['-f', str(INSTALL / name)]
    return run(args + arguments)


def ready():
    for _ in range(120):
        try:
            with urllib.request.urlopen('http://127.0.0.1:39111/readyz', timeout=2) as result:
                if result.status == 200:
                    return
        except OSError:
            pass
        time.sleep(1)
    raise RuntimeError('Owned backend readiness timed out')


def identity():
    meta = metadata()
    result = {}
    for service in ['goby', 'player']:
        value = inspect(meta['project_name'] + '-' + service + '-1')
        assert value['Config']['Labels']['com.docker.compose.project'] == meta['project_name']
        result[service] = {'id': value['Id'], 'image': value['Image'], 'running': value['State']['Running'], 'oomKilled': value['State']['OOMKilled'], 'exitCode': value['State']['ExitCode']}
        if service == 'goby':
            mounts = {item['Destination']: item for item in value['Mounts']}
            assert mounts['/var/lib/goby']['Source'] == str(ROOT / 'old/state')
            assert mounts['/media']['Source'] == str(ROOT / 'media') and mounts['/media']['RW']
    return result


def config(meta, source):
    value = {'owner': OWNER, 'container': meta['project_name'] + '-goby-1', 'imageId': meta['image_id'],
             'applicationSHA256': meta['application']['sha256'], 'sourceRevision': meta['application']['sourceRevision'],
             'playerContainer': meta['project_name'] + '-player-1', 'playerImageId': PLAYER,
             'baseURL': 'http://127.0.0.1:39111', 'playerURL': 'http://127.0.0.1:39112',
             'externalFixtureRoot': str(source / 'internal/server/testdata/external-bitmap'), 'fixtureName': 'Main Refresh 20261006'}
    write(REFRESH / 'software.private.json', value)


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('action', choices=['start-old', 'replace', 'restart', 'close'])
parser.add_argument('--image')
parser.add_argument('--binary-sha256')
parser.add_argument('--revision')
parser.add_argument('--source', type=Path)
args = parser.parse_args()
meta = metadata()
if args.action == 'start-old':
    assert args.source and args.source.resolve(strict=True) == args.source
    assert meta['image_id'] == OLD_IMAGE
    assert not (EVIDENCE / 'before-start.json').exists()
    previous = {}
    for name in ['deployment.env', 'installation.json', 'current-release.json']:
        data = (INSTALL / name).read_bytes()
        target = EVIDENCE / ('previous-' + name)
        assert not target.exists()
        write(target, data)
        previous[name] = {'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}
    write(EVIDENCE / 'before-start.json', {'containers': identity(), 'backups': previous})
    config(meta, args.source)
    run(['python3', str(INSTALL / 'goby-docker.py'), 'start', '--directory', str(INSTALL), '--wait-timeout', '120'])
    ready()
    result = identity()
elif args.action == 'replace':
    assert re.fullmatch(r'sha256:[a-f0-9]{64}', args.image or '') and args.image != OLD_IMAGE
    assert re.fullmatch(r'[a-f0-9]{64}', args.binary_sha256 or '') and re.fullmatch(r'[a-f0-9]{40}', args.revision or '')
    assert args.source and args.source.resolve(strict=True) == args.source
    assert meta['image_id'] == OLD_IMAGE
    before = identity()
    assert before['goby']['running'] and before['player']['running']
    details = json.loads(run(['docker', 'image', 'inspect', args.image]))[0]
    assert details['Id'] == args.image and details['Config']['User'] == '10001:10001'
    deployment = (INSTALL / 'deployment.env').read_text().splitlines()
    assert len([line for line in deployment if line.startswith('GOBY_OCI_IMAGE=')]) == 1
    changed = ["GOBY_OCI_IMAGE='" + args.image + "'" if line.startswith('GOBY_OCI_IMAGE=') else line for line in deployment]
    write(INSTALL / 'deployment.env', ('\n'.join(changed) + '\n').encode())
    meta['image_id'] = args.image
    meta['application'] = {'sourceRevision': args.revision, 'sha256': args.binary_sha256}
    write(INSTALL / 'installation.json', meta)
    config(meta, args.source)
    compose(['up', '-d', '--no-deps', '--pull', 'never', 'goby'])
    ready()
    after = identity()
    assert after['goby']['image'] == args.image and after['goby']['id'] != before['goby']['id']
    assert after['player']['id'] == before['player']['id'] and after['player']['image'] == PLAYER
    result = {'before': before, 'after': after, 'playerContainerReused': True}
elif args.action == 'restart':
    before = identity()
    assert before['goby']['running'] and before['player']['running']
    compose(['restart', '--timeout', '120', 'goby'])
    ready()
    after = identity()
    assert after['goby']['id'] == before['goby']['id'] and after['player']['id'] == before['player']['id']
    result = {'before': before, 'after': after}
else:
    compose(['stop', '--timeout', '120', 'player', 'goby'])
    result = identity()
    assert all(not value['running'] and not value['oomKilled'] and value['exitCode'] == 0 for value in result.values())
path = EVIDENCE / (args.action + '-' + str(time.time_ns()) + '.json')
write(path, result)
print(json.dumps({'complete': True, 'action': args.action, 'evidence': str(path)}))
