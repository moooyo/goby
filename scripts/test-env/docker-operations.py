#!/usr/bin/env python3
"""Accept the Docker operations helper in one explicitly provisioned fixture.

Run only on the owned test-env root supplied by --root. That root contains the
private backend.json-equivalent backend.private.json, existing external pgdata,
goby_ops_app.url, goby_ops_restore.url, pg-ca.crt and generated media. The helper
source and delivery catalog are under source/deploy/oci. The selected archive
directory is an explicit argument. No provider request or credential is used.
"""
import argparse
import atexit
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import urllib.parse

def need(value, message):
    if not value:
        raise RuntimeError(message)

def emit(event, **fields):
    print(json.dumps({'event': event, **fields}), flush=True)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', required=True, type=Path)
    parser.add_argument('--release-dir', required=True, type=Path)
    parser.add_argument('--resume', action='store_true', help='Retain the accepted fresh-install prefix and repeat the affected outage/restart scope.')
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    need(root.name.startswith('goby-docker-operations-'), 'Use an explicitly owned operations root')
    private = json.loads((root / 'backend.private.json').read_text())
    installation = root / 'installation with spaces'
    source_helper = root / 'source/deploy/oci/goby-docker.py'
    helper = installation / 'goby-docker.py'
    need(installation.is_dir(), 'Prepare the first installation separately and retain that receipt')
    # Updating this fixture's operator script is explicit and leaves its data/config intact.
    shutil.copyfile(source_helper, helper)
    secrets = [private['admin_password']]
    for name in ('goby_ops_app', 'goby_ops_restore'):
        secrets += [private[name]['url'], private[name]['password'], urllib.parse.quote(private[name]['password'], safe='')]
    secrets.append((installation / 'setup-token.txt').read_text().strip())
    artifacts = root / 'artifacts'
    finished = False
    def failure_cleanup():
        if not finished:
            closed = subprocess.run([sys.executable, str(helper), 'stop', '--directory', str(installation)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
            (artifacts / 'failure-closure.json').write_text(json.dumps({'stopExitCode': closed.returncode}) + '\n')
    atexit.register(failure_cleanup)
    operation = 0
    def run_helper(action, *extra, expected=0, use_source=False):
        nonlocal operation
        operation += 1
        command = [sys.executable, str(source_helper if use_source else helper), action, '--directory', str(installation), *extra]
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
        raw = result.stdout + result.stderr
        need(not any(secret.encode() in raw for secret in secrets if secret), 'Helper leaked a private value')
        prefix = 'resume-' if args.resume else ''
        (artifacts / (prefix + f'operation-{operation:02d}-' + action + '.log')).write_bytes(raw)
        need(result.returncode == expected, f'Unexpected helper exit for {action}: {result.returncode}')
        if action == 'logs':
            return raw
        value = json.loads(result.stdout)
        emit('helper_result', action=action, result=value)
        return value
    def fixture_inputs(directory=installation):
        return ['--release-dir', str(args.release_dir), '--media-dir', str(root / 'media'),
                '--database-url-file', str(root / 'goby_ops_app.url'), '--database-ca-file', str(root / 'pg-ca.crt'),
                '--recovery-database-url-file', str(root / 'goby_ops_restore.url'), '--public-url', 'http://127.0.0.1:38962',
                '--host-port', '38962', '--profile', 'software']
    def run_program(command):
        return subprocess.run(command, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout
    def pg(action):
        command = ['runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/pg_ctl', '-D', str(root / 'pgdata'), '-w']
        command += ['-m', 'fast', 'stop'] if action == 'stop' else ['-l', str(root / 'pgdata/server.log'), 'start']
        run_program(command)
    config_snapshot = {name: (installation / name).read_bytes() for name in ('installation.json', 'goby.env', 'deployment.env', 'setup-token.txt')}
    media = root / 'media'
    media_before = media.stat()
    need(all((installation / name).stat().st_uid == 10001 and (installation / name).stat().st_mode & 0o777 == 0o700 for name in ('state', 'cache', 'logs')), 'Incorrect prepared directory identity')
    need(all((installation / name).stat().st_mode & 0o077 == 0 for name in config_snapshot), 'Private prepared file is not private')
    if not args.resume:
        duplicate = run_helper('prepare', *fixture_inputs(), expected=1, use_source=True)
        need(duplicate['safeErrorCode'] == 'installation_already_exists', 'Duplicate preparation failed for the wrong reason')
        need(all((installation / name).read_bytes() == data for name, data in config_snapshot.items()), 'Duplicate preparation changed existing data')
        need(run_helper('status')['status'] == 'not_created', 'Preparation unexpectedly started the app')
        original_mode = media.stat().st_mode & 0o777
        try:
            os.chown(media, 0, 0)
            media.chmod(0o700)
            denied = run_helper('check', expected=1)
            need(denied['safeErrorCode'] == 'media_not_readable', 'Unreadable media was not diagnosed')
        finally:
            os.chown(media, media_before.st_uid, media_before.st_gid)
            media.chmod(original_mode)
        checked = run_helper('check')
        need(checked['status'] == 'checked' and not checked['databaseAuthenticationChecked'], 'Check overstated database acceptance')
    need(run_helper('start', '--wait-timeout', '30')['status'] == 'ready', 'Fresh start did not become ready')

    spec = importlib.util.spec_from_file_location('operations_oci_http', root / 'source/scripts/test-env/oci-delivery.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    config = {'base_url': 'http://127.0.0.1:38962', 'timeout_seconds': 120, 'output_dir': str(artifacts),
              'admin_name': private['admin_name'], 'admin_password': private['admin_password']}
    client = module.Delivery(config)
    token = (installation / 'setup-token.txt').read_text().strip()
    if not args.resume:
        client.request('POST', '/admin/v1/bootstrap', {'SetupToken': token, 'Name': private['admin_name'], 'Password': private['admin_password']}, expected=201)
    client.authenticate()
    html = client.request('GET', '/admin/', binary=True)
    need(b'<html' in html.lower() and b'/assets/' in html, 'Embedded administrator page missing')
    if args.resume:
        libraries = client.request('GET', '/admin/v1/libraries', admin=True)['Items']
        need(len(libraries) == 1 and libraries[0]['Name'] == 'Docker operations fixture', 'Unexpected retained library')
        library_id = libraries[0]['Id']
    else:
        library = client.request('POST', '/admin/v1/libraries', {'Name': 'Docker operations fixture', 'CollectionType': 'movies', 'Paths': ['/media'], 'Scan': True}, expected=201, admin=True)
        library_id = library['Library']['Id']
        client.scan_complete(library['Job']['Id'])
    query = '/emby/Items?' + urllib.parse.urlencode({'ParentId': library_id, 'Recursive': 'true', 'IncludeItemTypes': 'Movie'})
    page = client.request('GET', query, public=True)
    need(page['TotalRecordCount'] == 1, 'Expected one real scanned movie')
    item_id = page['Items'][0]['Id']
    route = '/emby/Videos/' + item_id + '/stream.mp4?Static=true'
    payload, _ = client.binary_request(route)
    expected_media = (media / 'Docker Setup Demo.mp4').read_bytes()
    need(payload == expected_media, 'Installed application changed direct-play bytes')
    emit('retained_installation_baseline' if args.resume else 'fresh_installation_verified', embedded_admin=True, scanned_movies=1, direct_play_sha256=hashlib.sha256(payload).hexdigest(), source_image_unchanged=True)
    deployment = installation / 'deployment.env'
    before_port_change = deployment.read_bytes()
    try:
        lines = deployment.read_text().splitlines()
        deployment.write_text('\n'.join("GOBY_HOST_PORT='38963'" if line.startswith('GOBY_HOST_PORT=') else line for line in lines) + '\n')
        mismatch = run_helper('status', expected=1)
        need(mismatch['safeErrorCode'] == 'container_port_mismatch', 'Readiness did not bind the selected container port')
    finally:
        deployment.write_bytes(before_port_change)
    pg('stop')
    try:
        unavailable = run_helper('status')
        need((unavailable['status'], unavailable['safeErrorCode']) in {('not_ready', 'database_not_ready'), ('exited', 'database_connection_lost')}, 'Database outage was not identified')
    finally:
        pg('start')
    run_helper('stop')
    need(run_helper('status')['status'] in {'stopped', 'exited'}, 'Stop did not retain an inactive installation')
    need(run_helper('start', '--wait-timeout', '30')['status'] == 'ready', 'Recovered installation did not restart')
    client.authenticate()
    page = client.request('GET', query, public=True)
    need(page['TotalRecordCount'] == 1 and page['Items'][0]['Id'] == item_id, 'Restart changed catalog identity')
    restored, _ = client.binary_request(route)
    need(restored == payload, 'Restart changed playable media')
    run_helper('logs', '--lines', '100')
    run_helper('stop')
    need(run_helper('status')['status'] == 'stopped', 'Final stop failed')
    need((installation / 'setup-token.txt').read_text().strip() == token, 'Operations rotated the setup secret')
    need((media.stat().st_uid, media.stat().st_gid) == (media_before.st_uid, media_before.st_gid), 'Operations changed media ownership')
    result = {'kind': 'goby-docker-operations-acceptance', 'passed': True, 'installationPathWithSpaces': True,
              'existingConfigurationPreserved': True, 'unreadableMediaDiagnosed': True,
              'freshSetupCatalogAndDirectPlayback': True, 'actualPortMappingChecked': True,
              'databaseOutageDiagnosed': True, 'restartPersistence': True, 'noPrivateOutput': True,
              'stoppedWithDataRetained': True, 'archiveDirectory': str(args.release_dir),
              'freshInstallationEvidence': 'runtime02.log' if args.resume else 'current run',
              'resumedAffectedOutageScope': args.resume,
              'helperSha256': hashlib.sha256(helper.read_bytes()).hexdigest()}
    (artifacts / 'runtime-result.json').write_text(json.dumps(result, indent=2) + '\n')
    finished = True
    emit('result', **result)

if __name__ == '__main__':
    main()
