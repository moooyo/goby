"""Read final deployment and source identities without exporting credentials."""
from pathlib import Path
import hashlib
import io
import json
import os
import subprocess
import tarfile

ROOT = Path('/opt/goby-test/player-live-20261004-165c/external-timeline-check/live')
OWNER = 'goby-external-timeline-165c-20261005'
assert os.name == 'posix' and ROOT.resolve() == ROOT and (ROOT / '.owner').read_text().strip() == OWNER
output = ROOT / 'artifacts'
config = json.loads((ROOT / 'live.private.json').read_text())
postgres = json.loads((ROOT / 'live-postgres.private.json').read_text())
records = []
for name in [config['container'], 'goby-external-timeline-165c-live-player', postgres['container']]:
    metadata = json.loads(subprocess.check_output(['docker', 'inspect', name], text=True))[0]
    assert metadata['Config']['Labels'].get('goby.owner') == OWNER and metadata['State']['Running']
    records.append({'name': name, 'image': metadata['Image'], 'running': True,
                    'readOnlyRootFilesystem': metadata['HostConfig']['ReadonlyRootfs'],
                    'ports': metadata['HostConfig']['PortBindings'],
                    'owner': metadata['Config']['Labels']['goby.owner']})
assert all(value['readOnlyRootFilesystem'] for value in records[:2])
deployment = json.loads((output / 'player-deployment.json').read_text())
payload = subprocess.check_output(['docker', 'exec', 'goby-external-timeline-165c-live-player', 'tar', '-C', '/usr/share/nginx/html', '-cf', '-', '.'])
with tarfile.open(fileobj=io.BytesIO(payload)) as archive:
    actual = {entry.name.removeprefix('./'): hashlib.sha256(archive.extractfile(entry).read()).hexdigest()
              for entry in archive.getmembers() if entry.isfile()}
expected = {entry['path']: entry['sha256'] for entry in deployment['sourceFiles']}
assert actual == expected
schema = subprocess.check_output(['docker', 'exec', postgres['container'], 'psql', '-X', '-U', postgres['user'],
                                  '-d', config['database'], '-Atc', 'SELECT max(version) FROM schema_migrations'], text=True).strip()
assert schema == '61'
fixture = ROOT / 'media/ExternalBitmap/External Bitmap (2026)'
oracle = json.loads((output / 'oracle.json').read_text())
files = []
for entry in oracle['Files']:
    path = fixture / ('External Bitmap' + Path(entry['Name']).suffix)
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    assert digest == entry['SHA256'] and path.stat().st_size == entry['Bytes']
    files.append({'name': path.name, 'sha256': digest, 'bytes': path.stat().st_size})
application = subprocess.check_output(['docker', 'exec', config['container'], 'sha256sum', '/usr/local/bin/goby'], text=True).split()[0]
assert application == config['applicationSHA256']
toolchain = subprocess.check_output(['docker', 'exec', config['container'], '/opt/ffmpeg/9.0.1/bin/ffmpeg', '-version'], text=True).splitlines()[0]
storage = fixture / 'backdrops/goby-subtitle-timelines' / hashlib.sha256(b'External Bitmap.mp4').hexdigest()
manifest = json.loads((storage / 'manifest.json').read_text())
publication = {'generation': manifest['Generation'], 'sha256': manifest['SHA256'],
               'sourceStamp': manifest['SourceStamp'], 'bytes': manifest['Summary']['Bytes']}
assert hashlib.sha256((storage / publication['generation']).read_bytes()).hexdigest() == publication['sha256']
result = {'containers': records, 'schema': schema, 'applicationSHA256': application, 'toolchain': toolchain,
          'servedPlayerAssetsMatchBuild': True, 'playerAssetCount': len(actual), 'restoredOriginalSubtitleFiles': files,
          'currentPublication': publication}
(output / 'deployment-final.json').write_text(json.dumps(result, indent=2))
print(json.dumps({'containersRunning': len(records), 'schema': schema, 'playerAssetsMatched': len(actual),
                  'restoredOriginalSubtitleFiles': len(files), 'applicationSHA256': application}))
