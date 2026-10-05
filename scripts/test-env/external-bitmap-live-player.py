"""Package the verified player build in the isolated external bitmap deployment."""
from pathlib import Path
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import time

SCOPE = Path('/opt/goby-test/player-live-20261004-165c/external-timeline-check')
ROOT = SCOPE / 'live'
OWNER = 'goby-external-timeline-165c-20261005'
assert os.name == 'posix' and ROOT.resolve() == ROOT and (ROOT / '.owner').read_text().strip() == OWNER
config = json.loads((ROOT / 'live.private.json').read_text())
arguments = argparse.ArgumentParser(description=__doc__)
arguments.add_argument('--replace', action='store_true', help='Replace only the existing owned acceptance player container.')
args = arguments.parse_args()
image = 'goby-external-timeline-165c-player:20261005'
name = 'goby-external-timeline-165c-live-player'
existing = subprocess.run(['docker', 'inspect', name], text=True, capture_output=True)
if existing.returncode == 0:
    assert args.replace, 'Use --replace for the explicitly owned acceptance player.'
    metadata = json.loads(existing.stdout)[0]
    assert metadata['Config']['Labels'].get('goby.owner') == OWNER
context = ROOT / ('player-image-' + str(time.time_ns()))
context.mkdir(mode=0o755)
source = SCOPE / 'source/web/player'
shutil.copytree(source / 'dist', context / 'dist')
shutil.copy2(source / 'nginx.conf', context / 'nginx.conf')
(context / 'Dockerfile').write_text('FROM goby-player-ui:step9-other-pages-final\nUSER 0:0\nRUN rm -rf /usr/share/nginx/html\nCOPY dist /usr/share/nginx/html\nCOPY nginx.conf /etc/nginx/templates/nginx.conf.template\nLABEL goby.owner="' + OWNER + '"\nUSER 101:101\n')
with (ROOT / 'artifacts/player-image-build.log').open('w') as log:
    subprocess.run(['docker', 'build', '--network=none', '--pull=false', '-t', image, str(context)], check=True, stdout=log, stderr=subprocess.STDOUT)
if existing.returncode == 0:
    subprocess.run(['docker', 'stop', name], check=True, stdout=subprocess.DEVNULL)
    subprocess.run(['docker', 'rm', name], check=True, stdout=subprocess.DEVNULL)
subprocess.run(['docker', 'run', '-d', '--pull=never', '--name', name, '--label', 'goby.owner=' + OWNER,
                '--network', config['network'], '--read-only', '--user=101:101', '--cap-drop=ALL', '--security-opt=no-new-privileges:true',
                '--cpus=1', '--memory=256m', '--pids-limit=64', '-p', '127.0.0.1:38986:8080',
                '-e', 'GOBY_API_UPSTREAM=http://' + config['container'] + ':8096', '--tmpfs', '/tmp:rw,nosuid,nodev,size=67108864,mode=1777', image], check=True, stdout=subprocess.DEVNULL)
manifest = [{'path': str(path.relative_to(context / 'dist')), 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
            for path in sorted((context / 'dist').rglob('*')) if path.is_file()]
image_id = subprocess.check_output(['docker', 'inspect', '--format={{.Image}}', name], text=True).strip()
evidence = {'container': name, 'image': image, 'imageId': image_id, 'sourceFiles': manifest, 'playerURL': config['playerURL']}
(ROOT / 'artifacts/player-deployment.json').write_text(json.dumps(evidence, indent=2))
print(json.dumps({'container': name, 'image': image, 'sourceFiles': len(manifest), 'playerURL': config['playerURL']}))
