#!/usr/bin/env python3
"""Continue one failed LibraryChanged observation without replaying its setup.

Only Movie 98 receives one public metadata update. Its existing owned directory
then moves outside the watched Movies directory and back, with one scoped scan
per move. Each action follows 75 quiet seconds and receives at least 120 seconds
of continuous WebSocket observation after its public catalog effect is stable.
The old failed scope, seven libraries, five users and three media trees remain
immutable. This records a public protocol; it is not original-client acceptance.
"""

from __future__ import annotations

import argparse
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import time
import types
from urllib.parse import urlencode

sys.dont_write_bytecode = True
W = Path('/opt/goby-test/exec-work-m3e')
ROOT = W / 'reference-library-changed-continuation-v1'
OLD = W / 'reference-library-changed-v1'
MARKER = 'goby-reference-library-changed-continuation-v1'
SOURCE = W / 'reference-library-changed-tool-01/capture-client-library-changed-reference.py'
SOURCE_SHA = '51c05a0c826cd18983901f631d5c04163372f37591b980459c7fedac2fb5d445'
TRANSPORT = W / 'reference-library-changed-transport-tool-01/bounded_websocket_capture.py'
TRANSPORT_SHA = '54fd18d50cf254aa6e93ac587f0d8fdf17d6897432e0448c2d00ddeb75ec7f0e'
LIBRARY, USER, ITEM = '93', 'c5f36699a54f4971a891682cd9de410f', '98'
OLD_SUBTREE = {'97', '98'}
TITLE = 'M3e LibraryChanged API Updated'
OVERVIEW = 'Controlled public API metadata update for the continuation observation.'
PINS = {
    OLD / 'export/report.json': 'f502223df8e7de2afeb49b5a6285498d967f5c95e1435121b455d7e8ee8b0a33',
    OLD / 'private/final-old-state.json': '5898de2a2e99754d34a0b4402f8803a2aca07332781feba62e7d0dbb8b4d6710',
    OLD / 'private/update-media-after.json': 'ecba10ca770ad5d021e98b2a8f6f1748eb06316833322376d1419b3f8a26a370',
    OLD / 'private/old-media-after.json': '1adfd198b51527b558faa8b3e33d998f375dee1c6d2c7711a8a3b8fad0bf0315',
    W / 'reference-library-changed-execution-01/terminal.json': '2586b86a3ea0087be67c397f3c3c3dca2e75b7210d90c969d72225f2a1171b2f',
    W / 'reference-library-changed-tail-v1/export/report.json': '2dae87e06e3beda64421cc10c26c14889124f9885943cb29b960ccaf253d4aff',
    W / 'reference-library-changed-tail-tool-01/terminal.json': '2441846e7ae8923f5fcdcfac8e4a9f8d6aab53eb3e4034577391e2c6910c6dba',
}
# These two public terminal receipts were written with mode0644 inside protected
# directories. Preserve their observed permissions instead of rewriting evidence.
PUBLIC_TERMINALS = {
    W / 'reference-library-changed-execution-01/terminal.json',
    W / 'reference-library-changed-tail-tool-01/terminal.json',
}
UNITS = {
    'goby-reference-library-changed-v1.service': ('0d09ea52f4484e4e8dcf882f72c3ca5f', '1'),
    'goby-reference-library-changed-tail-v1.service': ('5e9d86a757d145ec9d486663657ba100', '0'),
}
# The public reference-metadata recorder's successful full-current DTO contract.
# Do not import that recorder: its eager setup targets a different old instance.
EDIT_FIELDS = (
    'Name', 'SortName', 'ForcedSortName', 'OriginalTitle', 'Overview', 'ProductionYear', 'PremiereDate',
    'EndDate', 'CommunityRating', 'CriticRating', 'OfficialRating', 'CustomRating', 'ProviderIds',
    'Genres', 'Tags', 'TagItems', 'Studios', 'People', 'LockedFields', 'LockData', 'Taglines', 'ProductionLocations',
    'PreferredMetadataLanguage', 'PreferredMetadataCountryCode', 'IndexNumber', 'ParentIndexNumber',
    'SortIndexNumber', 'SortParentIndexNumber', 'DisplayOrder', 'Status', 'DateCreated',
)


def need(value, message):
    if not value:
        raise RuntimeError(message)


def load_support():
    """Authenticate the fixed dependency before importing its definitions only."""
    def identity(value):
        return (value.st_dev, value.st_ino, value.st_uid, value.st_gid, value.st_mode,
                value.st_nlink, value.st_size, value.st_mtime_ns, value.st_ctime_ns)
    for parent in reversed(SOURCE.parents):
        info = parent.lstat()
        need(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
             'The frozen helper has an unprotected ancestor.')
    info = SOURCE.lstat()
    need(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and
         stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and info.st_size < 2 << 20,
         'The frozen helper is not an owned bounded file.')
    with os.fdopen(os.open(SOURCE, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        need(identity(os.fstat(stream.fileno())) == identity(info), 'The helper changed before reading.')
        raw = stream.read((2 << 20) + 1)
        need(identity(os.fstat(stream.fileno())) == identity(info), 'The helper changed during reading.')
    need(identity(SOURCE.lstat()) == identity(info) and hashlib.sha256(raw).hexdigest() == SOURCE_SHA, 'The frozen helper hash differs.')
    module = types.ModuleType('library_changed_continuation_support')
    module.__file__ = str(SOURCE)
    exec(compile(raw, str(SOURCE), 'exec'), module.__dict__)
    # Only output routing changes. All pinned original identities and paths stay fixed.
    module.ROOT = ROOT
    return module


def metadata_body(current):
    need(isinstance(current, dict) and current.get('Id') == ITEM and current.get('Type') == 'Movie',
         'The fresh metadata DTO is not the exact owned Movie 98.')
    need(current.get('Name') != TITLE and current.get('Overview') != OVERVIEW,
         'This metadata operation has already occurred or has no distinct public effect.')
    body = {key: copy.deepcopy(current[key]) for key in EDIT_FIELDS if key in current}
    body.update(Id=ITEM, Name=TITLE, Overview=OVERVIEW)
    return body


def terminal_units():
    states = {}
    for unit, (invocation, exit_status) in UNITS.items():
        command = ['/usr/bin/systemctl', 'show', unit, '-p', 'MainPID', '-p', 'SubState', '-p', 'ControlGroup',
                   '-p', 'InvocationID', '-p', 'ExecMainStatus', '-p', 'ActiveState', '-p', 'Result']
        result = subprocess.run(command, capture_output=True, timeout=8, check=False)
        need(result.returncode == 0 and len(result.stdout) < 16384, 'A prior unit could not be read completely.')
        rows = dict(line.split('=', 1) for line in result.stdout.decode('utf-8').splitlines())
        need(rows.get('MainPID') == '0' and rows.get('ControlGroup') == '' and
             rows.get('InvocationID') == invocation and rows.get('ExecMainStatus') == exit_status and
             rows.get('SubState') in ('failed', 'exited', 'dead'), 'A prior observer is not its exact retained terminal unit.')
        states[unit] = rows
    return states


def failed_tree(base):
    """Retain a bounded byte and inode witness of the one immutable failed scope."""
    paths = [OLD, *sorted(OLD.rglob('*'))]
    need(len(paths) <= 1800, 'The failed scope has an unexpected inventory size.')
    rows = {}
    for path in paths:
        info = path.lstat()
        need(info.st_uid == info.st_gid == 0, 'A failed-scope owner changed.')
        row = base.file_id(info)
        if stat.S_ISDIR(info.st_mode):
            need(stat.S_IMODE(info.st_mode) == 0o700, 'A failed-scope directory is not private.')
        else:
            row['sha256'] = base.sha(base.read(path))
        need(base.file_id(path.lstat()) == base.file_id(info), 'A failed-scope identity changed while reading.')
        rows[str(path.relative_to(OLD))] = row
    return rows


def read_history(base):
    values = {path: json.loads(base.read(path, digest, mode=0o644 if path in PUBLIC_TERMINALS else 0o600))
              for path, digest in PINS.items()}
    report = values[OLD / 'export/report.json']
    need(report.get('marker') == 'goby-reference-library-changed-v1' and report.get('result') == 'retained_for_review' and
         report.get('libraryId') == LIBRARY and report.get('newUserId') == USER and report.get('oldPublicStatePreserved') is True and
         set(report.get('stages', {})) == {'add'} and report.get('errors') == [{'failureType': 'CaptureError',
         'reason': 'The new catalog did not have two stable complete expected observations.', 'stage': 'capture'}],
         'The original failed attempt is not the reviewed post-NFO boundary.')
    need(all(report['authentication'][role]['closed'] is True and report['authentication'][role]['ownedIdentityProven'] is True
             for role in ('admin', 'viewer')), 'A previous owned session was not closed.')
    evidence = report['evidence']
    for name in ('final-old-state.json', 'update-media-after.json', 'old-media-after.json'):
        path = OLD / 'private' / name
        need(evidence[name] == {'path': str(path), 'sha256': PINS[path]}, 'A failed-report descriptor differs.')
    descriptor = evidence['viewer-identity-response.json']
    need(descriptor['path'] == str(OLD / 'private/viewer-identity-response.json'), 'The original viewer proof path differs.')
    viewer = json.loads(base.read(Path(descriptor['path']), descriptor['sha256']))
    need(viewer.get('complete') is True and viewer.get('status') == 200 and viewer['body']['Id'] == USER,
         'The original viewer identity is not proven.')
    tail = values[W / 'reference-library-changed-tail-v1/export/report.json']
    need(tail.get('marker') == 'goby-library-changed-tail-v1' and tail.get('result') == 'complete' and
         tail.get('sessionRevoked') is True and tail.get('errors') == [] and tail.get('catalogMutations') == 0 and
         tail.get('events') == [] and tail.get('durationMs', 0) >= 240000 and tail.get('userId') == USER,
         'The passive tail did not finish at its reviewed no-mutation boundary.')
    return values, viewer['body']


def context(args, base):
    need(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
         os.environ.get('SSH_CONNECTION'), 'Use authorized root SSH on test-env only.')
    base.read(Path(__file__).absolute(), args.script_sha256)
    need(args.transport_path == TRANSPORT and args.transport_sha256 == TRANSPORT_SHA,
         'The transport path or frozen hash differs.')
    op = base.module(base.OPERATOR, base.OPERATOR_SHA, 'continuation_reference_identity')
    owner = json.loads(base.read(op.OWNER, base.OWNER_SHA))
    op.validate_owner(owner)
    identity = owner['serviceIdentity']
    need(owner.get('phase') == 'ready' and identity['pid'] == 332054 and identity['startTicks'] == '357218' and
         identity['bootId'] == '6bdfc486-7bc8-412f-82b5-70095a09dde7' and
         identity['networkNamespace'] == 'net:[4026532602]', 'The reference process lifetime differs.')
    op.same_service(owner)
    base.read(op.BROWSER, base.BROWSER_SHA); base.read(op.REPORT, base.REPORT_SHA)
    base.read(base.SPECIAL_REPORT, base.SPECIAL_SHA)
    history, viewer = read_history(base)
    units = terminal_units()
    old_media = base.old_media(op, owner)
    need(base.same(old_media, history[OLD / 'private/old-media-after.json']), 'The original media baseline changed.')
    new_media = base.media_tree(base.MEDIA)
    need(base.same(new_media, history[OLD / 'private/update-media-after.json']), 'The retained new media changed before continuation.')
    need(set(path.name for path in base.ADDED.iterdir()) == {base.ADDED_FILE.name, 'movie.nfo'} and
         not list(base.QUARANTINE.iterdir()), 'The original added directory or empty quarantine differs.')
    credentials = json.loads(base.read(OLD / 'private/credentials.json'))
    need(credentials.get('marker') == 'goby-reference-library-changed-v1' and credentials.get('username') == base.VIEWER_NAME and
         isinstance(credentials.get('password'), str) and re.fullmatch('[0-9a-f]{64}', credentials['password']),
         'The retained viewer credential is not the original owned account.')
    descriptor = history[OLD / 'export/report.json']['evidence']['viewer-password-intent.json']
    need(descriptor['path'] == str(OLD / 'private/viewer-password-intent.json'), 'The original password intent path differs.')
    original_password = json.loads(base.read(Path(descriptor['path']), descriptor['sha256']))
    need(original_password.get('method') == 'POST' and original_password.get('actor') == 'admin' and
         original_password.get('route') == '/emby/Users/' + USER + '/Password' and original_password.get('body') ==
         {'Id': USER, 'NewPw': credentials['password'], 'ResetPassword': False}, 'The credential is not bound to the original acknowledged password operation.')
    transport = base.module(TRANSPORT, TRANSPORT_SHA, 'continuation_websocket_transport')
    return op, owner, transport, history, viewer, old_media, new_media, units, credentials


def continuation_type(base):
    class Continuation(base.Run):
        def __init__(self, args, op, owner, transport, history, viewer):
            super().__init__(args, op, owner, transport)
            self.library_id, self.user_id, self.viewer.user_id, self.added_id = LIBRARY, USER, USER, ITEM
            self.baseline = history[OLD / 'private/final-old-state.json']
            self.old_catalogs = self.baseline['catalogByLibrary']
            self.old_facts = {key: row for rows in self.old_catalogs.values() for key, row in rows.items()}
            self.extra_ids = {key for rows in self.baseline['explicitExtrasByUser'].values() for key in rows}
            self.extra_parent_ids = {key.partition('/')[0] for rows in self.baseline['extraListsByUser'].values() for key in rows}
            self.viewer_baseline = {key: viewer[key] for key in ('Id', 'Name', 'Configuration', 'Policy')}
            self.media_before = history[OLD / 'private/old-media-after.json']
            self.new_media_before = history[OLD / 'private/update-media-after.json']
            self.failed_before = json.loads(base.read(ROOT / 'private/failed-tree-before.json'))
            self.metadata = None
            self.removed_subtree = set(OLD_SUBTREE)
            self.old_sources[str(SOURCE)] = SOURCE_SHA
            self.old_sources[str(Path(__file__).absolute())] = args.script_sha256
            self.old_sources.update({str(path): digest for path, digest in PINS.items() if path not in PUBLIC_TERMINALS})
            self.old_sources[str(ROOT / 'private/failed-tree-before.json')] = base.sha(base.read(ROOT / 'private/failed-tree-before.json'))
            self.deadline = time.monotonic() + 1450
            self.media_preserved = self.failed_preserved = self.viewer_preserved = False
            self.phase = 'starting'

        def check(self, cleanup=False):
            super().check(cleanup)
            for path in PUBLIC_TERMINALS:
                base.read(path, PINS[path], mode=0o644)

        def allowed_get(self, actor, route, cleanup):
            if not cleanup and actor is self.admin and route == '/emby/Users/' + self.admin.user_id + '/Items/' + ITEM:
                return True
            return super().allowed_get(actor, route, cleanup)

        def allowed_post(self, actor, route, body, mutation):
            if mutation == 'login':
                return route == '/emby/Users/AuthenticateByName' and body == {'Username': actor.username, 'Pw': actor.password} and not actor.proven
            if mutation == 'logout':
                return route == '/emby/Sessions/Logout' and body is None and actor.proven
            if actor is not self.admin or not actor.proven or self.library_id != LIBRARY or self.user_id != USER:
                return False
            if mutation == 'metadata-update':
                return self.metadata is not None and route == '/emby/Items/' + ITEM and base.same(body, self.metadata)
            return mutation in ('refresh-remove', 'refresh-readd') and route == self.refresh_route() and body == {}

        def check_history(self):
            need(base.same(failed_tree(base), self.failed_before), 'The old failed attempt changed.')
            terminal_units()

        def check_viewer(self, label):
            self.viewer_preserved = False
            current = self.get(self.admin, label, '/emby/Users/' + USER)
            projection = {key: current[key] for key in self.viewer_baseline}
            need(base.same(projection, self.viewer_baseline), 'The existing ordinary viewer configuration or policy changed.')
            self.viewer_preserved = True

        def expected_catalog(self, rows, phase):
            if any(key not in rows for key in ('94', '95', '96')):
                return False
            if rows['96'].get('Type') != 'Movie' or rows['96'].get('Path') != str(base.ANCHOR_FILE):
                return False
            added = [row for row in rows.values() if row.get('Path') == str(base.ADDED_FILE)]
            if phase == 'remove':
                return set(rows) == {'94', '95', '96'} and not self.removed_subtree.intersection(rows) and not any(
                    isinstance(row.get('Path'), str) and (row['Path'] == str(base.ADDED) or
                    row['Path'].startswith(str(base.ADDED) + '/')) for row in rows.values())
            if len(added) != 1 or added[0].get('Type') != 'Movie' or added[0]['Id'] in self.old_facts:
                return False
            directories = [key for key, row in rows.items() if row.get('Path') == str(base.ADDED) and row.get('IsFolder') is True]
            if len(directories) != 1 or set(rows) != {'94', '95', '96', added[0]['Id'], directories[0]}:
                return False
            return phase == 'readd' or added[0]['Id'] == ITEM and directories == ['97']

        def quiet(self):
            self.ws.mark('pre-stage-quiet-start')
            last_count = len(base.library_events(self.ws.record))
            last_change, deadline = time.monotonic(), min(self.deadline, time.monotonic() + 240)
            while time.monotonic() < deadline:
                need(self.ws.open, 'The transport is not open during the quiet interval.')
                self.ws.wait(min(2, max(0, deadline - time.monotonic())))
                need(self.ws.open, 'The transport closed during the quiet interval.')
                count = len(base.library_events(self.ws.record))
                if count != last_count:
                    last_count, last_change = count, time.monotonic()
                if time.monotonic() - last_change >= 75:
                    self.ws.mark('pre-stage-quiet-complete')
                    return
            raise RuntimeError('The connection did not provide 75 quiet seconds within its bound.')

        def update_metadata(self):
            route = '/emby/Users/' + self.admin.user_id + '/Items/' + ITEM
            current = self.get(self.admin, 'metadata-fresh-detail', route)
            need(current.get('Path') == str(base.ADDED_FILE), 'Movie 98 no longer belongs to the owned added directory.')
            self.metadata = metadata_body(current)
            self.save('metadata-original-detail.json', current)
            self.ws.mark('metadata-post-dispatch')
            response = self.request(self.admin, 'metadata-update', 'POST', '/emby/Items/' + ITEM,
                                    self.metadata, mutation='metadata-update')
            need(response['complete'] and response['status'] == 204 and response['body'] is None,
                 'The single metadata POST was not acknowledged; it cannot be retried.')
            self.ws.mark('metadata-post-acknowledged')
            previous = None
            for number in range(16):
                self.check()
                detail = self.get(self.admin, f'metadata-result-{number}', route)
                ready = detail.get('Id') == ITEM and detail.get('Type') == 'Movie' and detail.get('Path') == str(base.ADDED_FILE) and \
                    detail.get('Name') == TITLE and detail.get('Overview') == OVERVIEW
                if ready and previous is not None and base.same(detail, previous):
                    self.save('metadata-confirmed-detail.json', detail)
                    rows = self.new_catalog('metadata-confirmed-members')
                    need(self.expected_catalog(rows, 'metadata') and rows[ITEM].get('Name') == TITLE and
                         rows[ITEM].get('Overview') == OVERVIEW, 'The ordinary viewer does not see the confirmed API metadata.')
                    return rows
                previous = detail if ready else None
                need(self.ws.open, 'The metadata observation transport closed.')
                self.ws.wait(2)
            raise RuntimeError('The metadata Name and Overview did not become stably visible.')

        def move_owned(self, phase):
            self.check_history()
            need(base.same(base.old_media(self.op, self.owner), self.media_before), 'Old media changed before the controlled move.')
            source, destination = (base.ADDED, base.QUARANTINE / base.ADDED.name) if phase == 'remove' else \
                (base.QUARANTINE / base.ADDED.name, base.ADDED)
            need(phase in ('remove', 'readd') and not os.path.lexists(destination), 'A move destination already exists.')
            need(base.directory_id(base.MEDIA) == self.inputs['mediaIdentity'] and
                 base.directory_id(source)['device'] == base.directory_id(destination.parent)['device'], 'The move escaped its original filesystem.')
            before = base.media_tree(source)
            expected = {key.removeprefix('Movies/' + base.ADDED.name + '/'): row for key, row in self.new_media_before.items()
                        if key.startswith('Movies/' + base.ADDED.name + '/')}
            actual_files = {key: row for key, row in before.items() if 'sha256' in row}
            need(set(before) == {'.', base.ADDED_FILE.name, 'movie.nfo'} and base.same(expected, actual_files),
                 'The exact owned files changed before the move.')
            self.save(phase + '-media-intent.json', {'source': str(source), 'destination': str(destination), 'before': before})
            self.check()
            need(not os.path.lexists(destination) and base.same(base.media_tree(source), before), 'The move inputs changed after its intent.')
            os.rename(source, destination)
            base.sync(source.parent); base.sync(destination.parent)
            after = base.media_tree(destination)
            need(set(before) == set(after) and all(base.same(value, after[key]) if 'sha256' in value else
                 all(value[field] == after[key][field] for field in value if field != 'ctime_ns') for key, value in before.items()),
                 'The rename changed file bytes, inode identities or links.')
            self.save(phase + '-media-after.json', base.media_tree(base.MEDIA))

        def observe(self, phase):
            self.phase = phase
            self.check_history(); self.check()
            before = self.new_catalog(phase + '-before-catalog')
            need(self.expected_catalog(before, 'remove' if phase == 'readd' else 'present'), 'The stage starting catalog differs.')
            self.save(phase + '-catalog-before.json', before)
            self.ws = self.transport.Capture('127.0.0.1', 18097,
                '/embywebsocket?' + urlencode({'api_key': self.viewer.token, 'deviceId': self.viewer.device}))
            complete, after = False, None
            try:
                self.ws.connect(); self.quiet(); self.check()
                begin = len(self.ws.record['events'])
                self.ws.mark(phase + '-action-start')
                if phase == 'metadata':
                    after = self.update_metadata()
                else:
                    if phase == 'remove':
                        subtree = {key for key, row in before.items() if row.get('Path') == str(base.ADDED) or
                                   isinstance(row.get('Path'), str) and row['Path'].startswith(str(base.ADDED) + '/')}
                        need(subtree == OLD_SUBTREE, 'The removed public subtree is not exactly the owned 97/98 pair.')
                    self.move_owned(phase)
                    self.ws.mark(phase + '-scoped-refresh-dispatch')
                    after = self.refresh(phase)
                need(self.ws.open, 'The transport closed before the public effect became stable.')
                self.save(phase + '-catalog-after.json', after)
                self.ws.mark(phase + '-catalog-stable')
                start = time.monotonic()
                while time.monotonic() - start < 120:
                    need(self.ws.open and time.monotonic() < self.deadline, 'The 120-second continuous observation was interrupted.')
                    self.ws.wait(max(0, min(10, 120 - (time.monotonic() - start))))
                    need(self.ws.open, 'EOF is not a valid negative event observation.')
                self.ws.mark(phase + '-window-complete')
                events = base.library_events({'events': self.ws.record['events'][begin:]})
                self.stages[phase] = {'catalogEffectVerified': True, 'beforeItemIds': list(before), 'afterItemIds': list(after),
                    'LibraryChangedObservations': len(events), 'eventOutcome': 'observed' if events else 'not_observed',
                    'quietSeconds': 75, 'windowAfterStableCatalogSeconds': 120, 'continuousOpenWindow': True,
                    'readdedMovieId': next((key for key, row in after.items() if row.get('Path') == str(base.ADDED_FILE)), None),
                    'attribution': 'Client receipt times in an isolated controlled window; array order and duplicates are preserved.'}
                complete = True
            finally:
                try:
                    self.ws.close()
                finally:
                    self.save(phase + '-websocket-private.json', self.ws.record, soft=True)
                    public = self.public_capture(self.ws.record)
                    public.update(phase=phase, controlledCatalogEffectVerified=complete)
                    self.save(phase + '-websocket.json', public, private=False, soft=True)
                    self.ws = None
            need(base.same(self.snapshot_old(phase + '-preservation'), self.baseline), 'Old public state changed during a stage.')
            self.check_viewer(phase + '-viewer-preservation')
            need(base.same(base.old_media(self.op, self.owner), self.media_before), 'Old media changed during a stage.')

        def run_continuation(self):
            self.save('worker-started.json', {'marker': MARKER, 'process': self.op.process_identity(os.getpid())})
            try:
                self.check_history()
                self.save('old-media-before.json', self.media_before)
                self.admin.login(); self.viewer.login()
                public = self.get(self.admin, 'public-identity', '/emby/System/Info/Public')
                need(public.get('Id') == self.owner['serverId'] and public.get('Version') == '4.9.5.0', 'The reference server identity differs.')
                self.check_viewer('initial-viewer-identity')
                current = self.snapshot_old('before')
                need(base.same(current, self.baseline), 'The fresh seven-library/five-user baseline differs from the retained final state.')
                libraries = self.virtuals('initial-owned-library')
                need(set(libraries) == base.OLD_LIBRARIES | {LIBRARY} and self.verify_new_library(libraries[LIBRARY]) == LIBRARY and
                     all(not any(key in row for key in ('RefreshStatus', 'RefreshProgress')) for row in libraries.values()),
                     'The existing owned library is not at its reviewed quiescent boundary.')
                for phase in ('metadata', 'remove', 'readd'):
                    self.observe(phase)
            except Exception as error:
                self.errors.append({'stage': self.phase, 'failureType': type(error).__name__,
                                    'reason': str(error) if type(error) in (RuntimeError, base.CaptureError) else None})
            finally:
                self.deadline = time.monotonic() + 120
                if self.ws is not None:
                    try: self.ws.close()
                    except Exception as error: self.errors.append({'stage': 'transport-cleanup', 'failureType': type(error).__name__})
                    self.save('terminal-websocket-private.json', self.ws.record, soft=True)
                    self.ws = None
                if self.admin.proven:
                    try:
                        self.after = self.snapshot_old('final')
                        need(base.same(self.after, self.baseline), 'The old public state changed.')
                        self.check_viewer('final-viewer-identity')
                    except Exception as error: self.errors.append({'stage': 'final-state', 'failureType': type(error).__name__})
                for actor in (self.viewer, self.admin):
                    try: actor.logout()
                    except Exception as error: self.errors.append({'stage': 'logout-' + actor.role, 'failureType': type(error).__name__})
                try:
                    media = base.old_media(self.op, self.owner)
                    self.save('old-media-after.json', media)
                    self.media_preserved = base.same(media, self.media_before)
                    need(self.media_preserved, 'Old media changed.')
                    self.save('new-media-after.json', base.media_tree(base.MEDIA))
                    tree = failed_tree(base)
                    self.save('failed-tree-after.json', tree)
                    self.failed_preserved = base.same(tree, self.failed_before)
                    need(self.failed_preserved, 'The failed scope changed.')
                    self.op.same_service(self.owner)
                    self.save('prior-units-after.json', terminal_units())
                except Exception as error: self.errors.append({'stage': 'final-preservation', 'failureType': type(error).__name__})
            result = {'marker': MARKER, 'result': 'complete' if not self.errors and set(self.stages) == {'metadata', 'remove', 'readd'} else 'retained_for_review',
                'referenceIdentity': self.owner['serviceIdentity'], 'serverId': self.owner['serverId'], 'libraryId': LIBRARY, 'viewerUserId': USER,
                'origin': {str(path): digest for path, digest in PINS.items()}, 'stages': self.stages, 'errors': self.errors,
                'oldPublicStatePreserved': base.same(self.baseline, self.after), 'oldMediaPreserved': self.media_preserved,
                'failedScopePreserved': self.failed_preserved, 'viewerConfigurationPolicyPreserved': self.viewer_preserved,
                'authentication': {actor.role: {'ownedIdentityProven': actor.proven, 'closed': actor.closed, 'userId': actor.user_id,
                    'credentialFingerprint': base.sha(actor.token.encode()) if actor.token else None} for actor in (self.admin, self.viewer)},
                'httpRequests': self.requests, 'responseBytes': self.bytes_read, 'evidence': self.records,
                'clientAcceptance': False, 'boundary': 'A new continuation observation. The original failed run stays failed. The passive tail started after that run ended; it did not overlap catalog mutations. No event is not a general no-notification guarantee.'}
            need(not any(secret and secret in base.encoded(result).decode() for secret in self.secrets), 'A credential survived public sanitization.')
            self.save('report.json', result, private=False)
            return result
    return Continuation


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=('check', 'capture', '_worker'), required=True)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--transport-path', type=Path, required=True)
    parser.add_argument('--transport-sha256', required=True)
    args = parser.parse_args()
    need(re.fullmatch('[0-9a-f]{64}', args.script_sha256), 'An explicit continuation source hash is required.')
    os.umask(0o077)
    base = load_support()
    op, owner, transport, history, viewer, media, new_media, units, old_credentials = context(args, base)
    if args.mode == '_worker':
        authority = json.loads(base.read(ROOT / 'OWNER.json'))
        need(authority['marker'] == MARKER and authority['sourceSha256'] == args.script_sha256 and
             authority['supportSha256'] == SOURCE_SHA and authority['transportSha256'] == TRANSPORT_SHA and
             authority['referenceIdentity'] == owner['serviceIdentity'] and authority['controlIdentity'] == base.directory_id(ROOT) and
             authority['mediaIdentity'] == base.directory_id(base.MEDIA) and os.getppid() == authority['parentProcess']['pid'] and
             op.process_identity(os.getppid()) == authority['parentProcess'] and
             os.readlink('/proc/self/ns/net') == owner['serviceIdentity']['networkNamespace'], 'The continuation worker lacks its exact live supervisor.')
        result = continuation_type(base)(args, op, owner, transport, history, viewer).run_continuation()
        print(json.dumps({'marker': MARKER, 'result': result['result'], 'httpRequests': result['httpRequests']}))
        return 0 if result['result'] == 'complete' else 1
    need(not os.path.lexists(ROOT), 'The one-shot continuation root exists; retry or adoption is forbidden.')
    need(os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net'), 'The supervisor must begin in the host namespace.')
    lock = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        need(base.file_id(os.fstat(lock)) == base.file_id(op.canonical(op.LOCK, mode=0o600)), 'The reference lock identity differs.')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        tree = failed_tree(base)
        terminal_units(); op.same_service(owner)
        need(base.same(base.old_media(op, owner), media) and base.same(base.media_tree(base.MEDIA), new_media),
             'A media baseline changed before acquiring the exclusive reference lock.')
        if args.mode == 'check':
            print(json.dumps({'marker': MARKER, 'result': 'preflight_passed', 'httpRequests': 0, 'outputCreated': False,
                              'failedScopeEntries': len(tree), 'libraryId': LIBRARY, 'viewerUserId': USER}))
            return 0
        base.new_directory(ROOT, 0o700)
        base.put(ROOT / 'intent.json', {'marker': MARKER, 'sourceSha256': args.script_sha256, 'supportSha256': SOURCE_SHA,
            'transportSha256': TRANSPORT_SHA, 'origin': {str(path): digest for path, digest in PINS.items()},
            'scope': 'One fresh admin/viewer pair; one Movie98 metadata POST; one remove and one re-add with only library93 refreshes.'})
        base.new_directory(ROOT / 'private', 0o700); base.new_directory(ROOT / 'export', 0o700)
        base.put(ROOT / 'private/failed-tree-before.json', tree)
        base.put(ROOT / 'private/prior-units-before.json', units)
        base.put(ROOT / 'private/credentials.json', {'marker': MARKER, 'username': base.VIEWER_NAME, 'userId': USER,
            'password': old_credentials['password'], 'devices': {role: 'goby-librarychanged-continuation-' + role + '-' + secrets.token_hex(16)
                                                               for role in ('admin', 'viewer')}})
        base.put(ROOT / 'OWNER.json', {'marker': MARKER, 'sourceSha256': args.script_sha256, 'supportSha256': SOURCE_SHA,
            'transportSha256': TRANSPORT_SHA, 'referenceIdentity': owner['serviceIdentity'], 'mediaRoot': str(base.MEDIA),
            'parentProcess': op.process_identity(os.getpid()), 'controlIdentity': base.directory_id(ROOT),
            'mediaIdentity': base.directory_id(base.MEDIA)})
        namespace = os.open('/proc/332054/ns/net', os.O_RDONLY)
        try:
            need(os.fstat(namespace).st_ino == 4026532602, 'The reference namespace descriptor changed.')
            terminal_units(); op.same_service(owner)
            command = ['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-B', str(Path(__file__).absolute()),
                       '--mode', '_worker', '--script-sha256', args.script_sha256, '--transport-path', str(TRANSPORT), '--transport-sha256', TRANSPORT_SHA]
            try:
                result = subprocess.run(command, capture_output=True, timeout=1750, check=False, pass_fds=(namespace,),
                                        env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
                stdout, stderr, code = result.stdout, result.stderr, result.returncode
            except subprocess.TimeoutExpired as error:
                stdout, stderr, code = error.stdout or b'', error.stderr or b'', 124
            base.put(ROOT / 'private/worker.stdout', stdout)
            base.put(ROOT / 'private/worker.stderr', stderr)
            base.put(ROOT / 'private/worker-exit.json', {'returncode': code, 'stdoutSha256': base.sha(stdout), 'stderrSha256': base.sha(stderr)})
            need(base.same(tree, failed_tree(base)), 'The immutable original scope changed across the worker lifetime.')
            print(json.dumps({'marker': MARKER, 'result': 'complete' if code == 0 else 'retained_for_review', 'report': str(ROOT / 'export/report.json')}))
            return code
        finally:
            os.close(namespace)
    finally:
        os.close(lock)


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({'marker': MARKER, 'result': 'failed', 'failureType': type(error).__name__}), file=sys.stderr)
        sys.exit(1)
