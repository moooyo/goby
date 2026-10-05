"""Deploy the final administrator wording layer without changing accepted media state."""
from pathlib import Path
import hashlib
import json
import os
import shutil
import subprocess
import time
import urllib.error
import urllib.request

SCOPE = Path('/opt/goby-test/player-live-20261004-165c/external-timeline-check')
ROOT = SCOPE / 'live'
OWNER = 'goby-external-timeline-165c-20261005'
assert os.name == 'posix' and ROOT.resolve() == ROOT and (ROOT / '.owner').read_text().strip() == OWNER
config_path = ROOT / 'live.private.json'
config = json.loads(config_path.read_text())
output = ROOT / 'artifacts'
assert not (output / 'deployment-before-copy-final.json').exists(), 'Do not repeat an already recorded deployment.'
shutil.copy2(output / 'deployment-final.json', output / 'deployment-before-copy-final.json')
baseline = json.loads((output / 'deployment-final.json').read_text())
backend = config['container']
metadata = json.loads(subprocess.check_output(['docker', 'inspect', backend], text=True))[0]
assert metadata['Config']['Labels'].get('goby.owner') == OWNER and metadata['State']['Running']
assert metadata['Image'] == baseline['containers'][0]['image']
storage = ROOT / 'media/ExternalBitmap/External Bitmap (2026)/backdrops/goby-subtitle-timelines' / hashlib.sha256(b'External Bitmap.mp4').hexdigest()
manifest_before = (storage / 'manifest.json').read_bytes()
publication = json.loads(manifest_before)
generation_before = (storage / publication['Generation']).read_bytes()
context = ROOT / 'backend-copy-final-image'
context.mkdir(mode=0o755)
shutil.copy2(SCOPE / 'goby-live-final', context / 'goby')
digest = hashlib.sha256((context / 'goby').read_bytes()).hexdigest()
image = 'goby-external-timeline-165c-live:copy-final-20261005'
(context / 'Dockerfile').write_text('FROM ' + config['image'] + '\nUSER 0:0\nCOPY --chown=10001:10001 --chmod=0555 goby /usr/local/bin/goby\nLABEL goby.owner="' + OWNER + '" io.goby.application.sha256="' + digest + '"\nUSER 10001:10001\n')
with (output / 'backend-copy-final-image-build.log').open('w') as log:
    subprocess.run(['docker', 'build', '--network=none', '--pull=false', '-t', image, str(context)], check=True, stdout=log, stderr=subprocess.STDOUT)
subprocess.run(['docker', 'stop', backend], check=True, stdout=subprocess.DEVNULL)
subprocess.run(['docker', 'rm', backend], check=True, stdout=subprocess.DEVNULL)
subprocess.run(['docker', 'run', '-d', '--pull=never', '--name', backend, '--label', 'goby.owner=' + OWNER, '--network', config['network'],
                '--user=10001:10001', '--init', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges:true',
                '--cpus=2', '--memory=2g', '--pids-limit=256', '--env-file', str(ROOT / 'live.private.env'), '-p', '127.0.0.1:38985:8096',
                '-v', str(ROOT / 'state') + ':/var/lib/goby', '-v', str(ROOT / 'cache') + ':/var/cache/goby',
                '-v', str(ROOT / 'logs') + ':/var/log/goby', '-v', str(ROOT / 'media') + ':/media',
                '--tmpfs', '/tmp:rw,nosuid,nodev,noexec,size=67108864,mode=0700,uid=10001,gid=10001', image], check=True, stdout=subprocess.DEVNULL)
for _ in range(120):
    try:
        with urllib.request.urlopen(config['baseURL'] + '/admin/v1/bootstrap', timeout=2) as response:
            assert response.status == 200
            break
    except (OSError, urllib.error.HTTPError):
        time.sleep(.25)
else:
    raise RuntimeError('The final backend did not become ready.')
assert (storage / 'manifest.json').read_bytes() == manifest_before
assert (storage / publication['Generation']).read_bytes() == generation_before
config['image'], config['applicationSHA256'] = image, digest
config_path.write_text(json.dumps(config))
config_path.chmod(0o600)
image_id = subprocess.check_output(['docker', 'inspect', '--format={{.Image}}', backend], text=True).strip()
result = {'previousImage': metadata['Image'], 'image': image_id, 'applicationSHA256': digest,
          'unchangedGeneration': publication['Generation'], 'manifestUnchanged': True, 'artifactBytesUnchanged': True}
(output / 'backend-copy-final-deploy.json').write_text(json.dumps(result, indent=2))
print(json.dumps(result))
