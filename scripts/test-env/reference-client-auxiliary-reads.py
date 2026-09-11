#!/usr/bin/env python3
"""Capture bounded Similar and ThemeMedia contracts from the owned reference.

Only one new ordinary-viewer recorder credential is created and revoked. The
base recorder retains its twenty-request hard cap. Durable intent counts also
span cleanup recovery, so no retry can reset the complete study's HTTP budget.
No playback, preference, metadata, library, scan, or account-creation API is used.
"""

from __future__ import annotations

import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
from urllib.parse import urlencode, urlsplit

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = WORK / 'reference-auxiliary-reads-v1'
PRIVATE, EXPORT = ROOT / 'private', ROOT / 'export'
MARKER = 'goby-m3e-reference-auxiliary-reads-v1'
DEVICE = 'goby-m3e-auxiliary-reads-recorder-v1'
CLIENT = 'Goby Auxiliary Read Contract Recorder'
PID, TICKS = 332054, '357218'
BINARY_SHA256 = 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2'
MEDIA_ROOT = '/opt/goby-fixtures/client-m3e'
MOVIE_PATH = MEDIA_ROOT + '/Movies/M3e Client Movie.mp4'
MUSIC_PATH = MEDIA_ROOT + '/Music'
MP3_PATH = MUSIC_PATH + '/M3e Client Audio.mp3'
ALBUM_NAME = 'M3e Synthetic Album'
MISSING_ID = '999999999'
MAX_BUSINESS, MAX_REQUESTS = 17, 20
ITEM_TYPES = 'Movie,Series,Season,Episode,MusicAlbum,Audio'
CATALOG_FIELDS = 'Path,Genres,People,Studios,Tags,ProductionYear,ParentId'
SIMILAR_FIELDS = 'PrimaryImageAspectRatio,DateCreated'
PRIVATE_URL = re.compile(r"https?://[^\s\"'<>]+", re.IGNORECASE)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def load_support():
    source = Path('/opt/goby-test/workspace-operator-review.WCj6Fjt4/reference-client-initialization.py')
    expected = json.loads((WORK / 'reference-client-initialization-v1/export/safety-report.json').read_text())['sourceSha256']
    require(hashlib.sha256(source.read_bytes()).hexdigest() == expected, 'The accepted recorder source changed.')
    spec = importlib.util.spec_from_file_location('auxiliary_capture_support', source)
    support = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(support)
    operator = support.load_operator()
    operator.canonical(source)
    require(operator.BINARY_SHA256 == BINARY_SHA256 and operator.digest(operator.BINARY, allow_links=True) == BINARY_SHA256,
            'The pinned reference executable changed.')
    support.ROOT, support.PRIVATE, support.EXPORT = ROOT, PRIVATE, EXPORT
    support.MARKER, support.DEVICE, support.CLIENT = MARKER, DEVICE, CLIENT
    return support, operator


def safe_urls(value):
    if isinstance(value, dict):
        return {key: safe_urls(item) for key, item in value.items()}
    if isinstance(value, list):
        return [safe_urls(item) for item in value]
    if isinstance(value, str):
        return PRIVATE_URL.sub('[redacted URL]', value).replace(MEDIA_ROOT + '/', '[fixture media]/')
    return value


def numeric_id(value):
    require(isinstance(value, str) and re.fullmatch(r'[1-9][0-9]{0,18}', value) and
            int(value) <= 9223372036854775807, 'A catalog identity is not a bounded positive numeric item ID.')
    return value


def catalog_rows(body):
    require(isinstance(body, dict) and isinstance(body.get('Items'), list) and
            0 < len(body['Items']) <= 100 and type(body.get('TotalRecordCount')) is int and
            body['TotalRecordCount'] == len(body['Items']), 'The bounded fixture catalog is incomplete.')
    rows = body['Items']
    ids = []
    for item in rows:
        require(isinstance(item, dict) and item.get('Type') in ITEM_TYPES.split(','), 'The catalog contains an unexpected item type.')
        require(isinstance(item.get('UserData'), dict), 'The catalog omits the user-state preservation witness.')
        ids.append(numeric_id(item.get('Id')))
        path = item.get('Path')
        require(path is None or path == '' or isinstance(path, str) and
                path.startswith(MEDIA_ROOT + '/') and '..' not in Path(path).parts,
                'The catalog escaped the fixed fixture media paths.')
    require(len(ids) == len(set(ids)), 'The fixture catalog contains duplicate item identities.')
    return rows


def select_items(rows):
    def unique(kind, path=None, name=None):
        matches = [item for item in rows if item.get('Type') == kind and
                   (path is None or item.get('Path') == path) and (name is None or item.get('Name') == name)]
        require(len(matches) == 1, 'A required fixture item is missing or ambiguous.')
        return matches[0]

    movie = unique('Movie', MOVIE_PATH)
    mp3 = unique('Audio', MP3_PATH)
    album = unique('MusicAlbum', name=ALBUM_NAME)
    album_id = numeric_id(album['Id'])
    require(album.get('Path') == MUSIC_PATH or album_id in (mp3.get('AlbumId'), mp3.get('ParentId')),
            'The album is not linked to the fixed music directory or its actual MP3 item.')
    require(mp3.get('Album') in (None, ALBUM_NAME), 'The fixed MP3 belongs to a different album.')
    artist_ids = []
    for field in ('AlbumArtists', 'ArtistItems'):
        credits = album.get(field)
        require(isinstance(credits, list) and len(credits) <= 32, 'The album has no bounded actual artist references.')
        for credit in credits:
            require(isinstance(credit, dict) and isinstance(credit.get('Name'), str) and credit['Name'],
                    'An album artist reference is incomplete.')
            identity = numeric_id(credit.get('Id'))
            if identity not in artist_ids:
                artist_ids.append(identity)
    require(artist_ids, 'The album has no real artist ID for the exclusion query.')
    require(MISSING_ID not in {item['Id'] for item in rows} | set(artist_ids),
            'The fixed missing-ID control exists in the observed fixture catalog.')
    return movie, album, mp3, artist_ids


def catalog_projection(rows):
    # Compare all observed metadata and UserData fields. Sorting by stable item
    # identity tolerates response ordering without dropping any returned value.
    return {item['Id']: item for item in rows}


def make_recorder(support, operator, owner):
    class Recorder(support.Recorder):
        def __init__(self):
            super().__init__(operator, owner, operator.read_private(operator.BROWSER))
            self.business_count, self.sequence = 0, 0
            self.routes, self.anonymous = {}, set()
            self.configuration_preserved = self.catalog_preserved = self.media_unchanged = False
            self.catalog, self.item_ids, self.artist_ids = [], {}, []
            self.recovering = False
            self.login_reserved = False

        def approve(self, method, route, user_id, *, authenticated):
            require(user_id == self.user_id, 'A request attempted a different research user.')
            parsed = urlsplit(route)
            require(not parsed.scheme and not parsed.netloc and not parsed.fragment, 'External request routing is forbidden.')
            if method == 'POST':
                require(route in ('/emby/Users/AuthenticateByName', '/emby/Sessions/Logout') and
                        authenticated is (route == '/emby/Sessions/Logout'), 'Only the owned login and logout may use POST.')
                return
            require(method == 'GET', 'Only read-only auxiliary business requests are allowed.')
            if route == '/emby/Sessions':
                require(authenticated, 'The invalid-token control must use the exact owned token.')
                return
            require(any(route == value and authenticated is (label not in self.anonymous)
                        for label, value in self.routes.items()), 'The request is outside the fixed auxiliary capture list.')

        def request(self, label, method, route, **kwargs):
            authenticated = kwargs.get('authenticated', True)
            self.approve(method, route, self.user_id, authenticated=authenticated)
            business = method == 'GET' and route != '/emby/Sessions'
            require(self.sequence < MAX_REQUESTS, 'The complete study HTTP attempt budget is exhausted.')
            if business:
                require(label in self.routes and self.routes[label] == route and
                        authenticated is (label not in self.anonymous) and self.business_count < MAX_BUSINESS and
                        self.sequence < MAX_REQUESTS - 2, 'The business request would consume reserved exact-token cleanup capacity.')
                self.business_count += 1
            if kwargs.get('login'):
                require(not self.login_reserved and self.sequence == 0, 'Only one new recorder login is permitted.')
                self.login_reserved = True
            self.sequence += 1
            if self.recovering:
                label = 'recovery-' + label
            operator.save(PRIVATE / ('request-intent-%02d.json' % self.sequence),
                          {'marker': MARKER, 'sequence': self.sequence, 'label': label, 'method': method,
                           'path': route, 'authenticated': authenticated, 'business': business,
                           'businessRequestsReserved': self.business_count})
            try:
                # The accepted base implementation and its count < 20 guard
                # remain unchanged. It owns token acknowledgement, identity,
                # bounded response capture, and credential sanitization.
                result = super().request(label, method, route, **kwargs)
                require(self.records[label]['response']['complete'], 'A recorder response is incomplete; further capture is stopped.')
                return result
            except Exception as error:
                operator.save(PRIVATE / ('request-failure-%02d.json' % self.sequence),
                              {'marker': MARKER, 'sequence': self.sequence, 'label': label,
                               'failureType': type(error).__name__, 'responseRecordAvailable': label in self.records,
                               'responseCompletionProven': False, 'httpAttemptReserved': True})
                raise

        def export(self, path, value):
            return super().export(path, safe_urls(value))

        def get(self, label):
            status, body = self.request(label, 'GET', self.routes[label], authenticated=label not in self.anonymous)
            require(self.records[label]['response']['complete'], 'An auxiliary response exceeded its complete-read bound.')
            return status, body

        def restore_budget(self):
            paths = sorted(PRIVATE.glob('request-intent-*.json'))
            require(0 < len(paths) <= MAX_REQUESTS, 'The retained HTTP budget is missing or excessive.')
            intents = [operator.read_private(path) for path in paths]
            require([row.get('sequence') for row in intents] == list(range(1, len(intents) + 1)) and
                    all(row.get('marker') == MARKER and type(row.get('business')) is bool for row in intents),
                    'The retained HTTP attempts are incomplete or belong to another study.')
            self.sequence = self.count = len(intents)
            self.business_count = sum(row['business'] for row in intents)
            require(self.business_count <= MAX_BUSINESS, 'The retained business budget is excessive.')
            self.login_reserved = True

        def logout(self):
            proof = PRIVATE / 'recorder-revocation.json'
            if operator.present(proof):
                require(operator.read_private(proof) == {'marker': MARKER, 'deviceId': DEVICE,
                        'logoutStatus': 204, 'invalidTokenStatus': 401}, 'The retained recorder revocation proof differs.')
                self.revoked = True
                return

            def complete_record(label, method, route, status):
                path = PRIVATE / (label + '-record.json')
                if not operator.present(path):
                    return False
                saved = operator.read_private(path)
                return saved.get('case') == label and saved.get('request', {}).get('method') == method and \
                    saved['request'].get('path') == route and saved['request'].get('authenticated') is True and \
                    saved.get('response', {}).get('status') == status and saved['response'].get('complete') is True

            logged_out = any(complete_record(prefix + 'recorder-logout', 'POST', '/emby/Sessions/Logout', 204)
                             for prefix in ('', 'recovery-'))
            invalid = any(complete_record(prefix + label, 'GET', '/emby/Sessions', 401)
                          for prefix in ('', 'recovery-') for label in
                          ('recorder-token-invalid', 'recorder-token-invalid-after-acknowledged-logout'))
            if logged_out:
                if not invalid:
                    status, _ = self.request('recorder-token-invalid-after-acknowledged-logout', 'GET', '/emby/Sessions')
                    require(status == 401, 'The acknowledged logout did not invalidate the owned token.')
                # Complete retained response records can finish publication
                # without a twenty-first request after an interrupted fsync.
                operator.save(proof, {'marker': MARKER, 'deviceId': DEVICE, 'logoutStatus': 204, 'invalidTokenStatus': 401})
                self.revoked = True
                return
            return super().logout()

        def capture(self):
            self.request('recorder-login', 'POST', '/emby/Users/AuthenticateByName', authenticated=False, login=True)
            require(self.token_identity_proven and self.records['recorder-login']['response']['complete'],
                    'The new ordinary recorder credential was not completely proven.')
            user = '/emby/Users/' + self.user_id
            catalog = user + '/Items?' + urlencode({'Recursive': 'true', 'IncludeItemTypes': ITEM_TYPES,
                                                   'Fields': CATALOG_FIELDS, 'Limit': '100'})
            self.routes = {'viewer-before': user, 'catalog-before': catalog}
            status, before = self.get('viewer-before')
            require(status == 200 and isinstance(before, dict) and before.get('Id') == self.user_id and
                    isinstance(before.get('Configuration'), dict) and isinstance(before.get('Policy'), dict),
                    'The ordinary viewer configuration and policy baseline is unavailable.')
            status, catalog_before = self.get('catalog-before')
            require(status == 200, 'The fixture catalog baseline is unavailable.')
            rows = catalog_rows(catalog_before)
            movie, album, mp3, self.artist_ids = select_items(rows)
            self.catalog = rows
            self.item_ids = {'movie': movie['Id'], 'album': album['Id'], 'mp3': mp3['Id']}
            similar = {'Limit': '12', 'UserId': self.user_id, 'EnableTotalRecordCount': 'false',
                       'ImageTypeLimit': '1', 'Fields': SIMILAR_FIELDS}
            for kind, item in self.item_ids.items():
                self.routes['similar-' + kind] = '/emby/Items/' + item + '/Similar?' + urlencode(similar)
                self.routes['theme-' + kind + '-default'] = '/emby/Items/' + item + '/ThemeMedia?' + urlencode({'UserId': self.user_id})
            self.routes['similar-album-exclude-artist'] = '/emby/Items/' + album['Id'] + '/Similar?' + urlencode(
                {**similar, 'ExcludeArtistIds': ','.join(self.artist_ids)})
            theme = '/emby/Items/' + movie['Id'] + '/ThemeMedia?'
            for label, option in (('songs-false', 'EnableThemeSongs'), ('videos-false', 'EnableThemeVideos'),
                                  ('inherit-false', 'InheritFromParent')):
                self.routes['theme-movie-' + label] = theme + urlencode({'UserId': self.user_id, option: 'false'})
            self.routes.update({'anonymous-similar': self.routes['similar-movie'],
                                'anonymous-theme': self.routes['theme-movie-default'],
                                'missing-theme': '/emby/Items/' + MISSING_ID + '/ThemeMedia?' + urlencode({'UserId': self.user_id}),
                                'viewer-after': user, 'catalog-after': catalog})
            self.anonymous = {'anonymous-similar', 'anonymous-theme'}
            cases = ('similar-movie', 'similar-album', 'similar-album-exclude-artist', 'similar-mp3',
                     'theme-movie-default', 'theme-album-default', 'theme-mp3-default', 'theme-movie-songs-false',
                     'theme-movie-videos-false', 'theme-movie-inherit-false', 'anonymous-similar', 'anonymous-theme', 'missing-theme')
            require(len(cases) + 4 == MAX_BUSINESS, 'The fixed route list no longer fits its study budget.')
            for label in cases:
                self.get(label)
            status, after = self.get('viewer-after')
            self.configuration_preserved = status == 200 and isinstance(after, dict) and all(
                before.get(key) == after.get(key) for key in ('Configuration', 'Policy'))
            status, catalog_after = self.get('catalog-after')
            self.catalog_preserved = status == 200 and catalog_projection(catalog_rows(catalog_after)) == catalog_projection(rows)
            require(self.configuration_preserved and self.catalog_preserved,
                    'Viewer configuration, policy, catalog metadata, or UserData changed during read-only research.')

    recorder = Recorder()
    support.approve = recorder.approve
    return recorder


def capture(support, operator, owner, recover=False):
    recorder = make_recorder(support, operator, owner)
    failure = None
    operator.save(PRIVATE / ('recovery-worker.json' if recover else 'worker.json'), operator.process_identity(os.getpid()))
    try:
        signal.alarm(85)
        if recover:
            recorder.recovering = True
            recorder.restore_budget()
            login = operator.read_private(PRIVATE / 'recorder-login.json')
            recorder.token = login.get('AccessToken')
            require(isinstance(recorder.token, str) and recorder.token, 'No owned recorder acknowledgement is available.')
            recorder.secrets.add(recorder.token)
            recorder.prove_login(200, login)
        else:
            recorder.capture()
    except Exception as error:
        failure = type(error).__name__
    finally:
        signal.alarm(50)
        try:
            recorder.logout()
        except Exception as error:
            failure = failure or type(error).__name__
        recorder.media_unchanged = operator.verify_media() == owner['media']
        recorder.identity()
        signal.alarm(0)
    summary = {'marker': MARKER, 'classification': 'protocol research; not client acceptance',
               'sourceSha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
               'referencePID': PID, 'referenceStartTicks': TICKS, 'referenceExecutableSha256': BINARY_SHA256,
               'businessRequestsReserved': recorder.business_count, 'maximumBusinessRequests': MAX_BUSINESS,
               'httpAttemptsReserved': recorder.sequence, 'maximumHttpAttempts': MAX_REQUESTS, 'responsesRecorded': len(recorder.records),
               'baseRecorderCount': recorder.count, 'recorderDeviceId': DEVICE, 'newRecorderTokenRevoked': recorder.revoked,
               'itemIds': recorder.item_ids, 'excludedArtistIds': recorder.artist_ids, 'approvedFixtureCatalog': recorder.catalog,
               'configurationAndPolicyPreserved': recorder.configuration_preserved, 'catalogMetadataAndUserDataPreserved': recorder.catalog_preserved,
               'mediaManifestPreserved': recorder.media_unchanged, 'playbackPreferenceMetadataOrLibraryWrites': 0,
               'browserSessionUsedOrRevoked': False, 'oldReferenceRequests': 0, 'failureType': failure, 'recoveryOnly': recover,
               'caseStatuses': {key: record['response']['status'] for key, record in recorder.records.items()},
               'missingIdControl': {'id': MISSING_ID, 'syntax': 'bounded positive decimal',
                                    'absenceScope': 'Not present in the complete bounded owned-media catalog or its observed album artist references.',
                                    'malformedIdRequested': False, 'globalAbsenceClaimed': False},
               'scope': 'Empty results only describe this fixture. Missing decimal-ID behavior does not establish malformed-ID behavior. '
                        'Complete non-200 auxiliary responses are retained and do not stop later cases. '
                        'Incomplete transport responses stop capture; their intent and failure type remain private, without claiming a complete body.'}
    export_root = EXPORT
    if recover:
        export_root = ROOT / 'recovery-export'
        require(not operator.present(export_root), 'Recovery evidence already exists.')
        export_root.mkdir(mode=0o700)
    for label, record in recorder.records.items():
        recorder.export(export_root / (label + '.json'), record)
    recorder.export(export_root / 'report.json', summary)
    require(failure is None and recorder.revoked and recorder.media_unchanged,
            'Research or exact recorder cleanup is incomplete; all existing evidence is retained.')
    print(json.dumps({'result': 'complete', 'httpAttemptsReserved': recorder.sequence,
                      'newRecorderTokenRevoked': recorder.revoked, 'report': str(export_root / 'report.json')}), flush=True)


def main():
    arguments = sys.argv[1:]
    require(arguments in ([], ['_capture'], ['--recover'], ['_recover']), 'Unsupported capture invocation.')
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'), 'Run through root SSH on test-env only.')
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('The bounded capture deadline expired.')))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    operator.validate_owner(owner)
    require(owner.get('phase') == 'ready' and owner['serviceIdentity']['pid'] == PID and
            owner['serviceIdentity']['startTicks'] == TICKS and operator.service_identity(owner) == owner['serviceIdentity'],
            'The exact approved fresh reference instance changed.')
    internal = arguments in (['_capture'], ['_recover'])
    recovering = arguments in (['--recover'], ['_recover'])
    if internal:
        require(operator.read_private(ROOT / 'OWNER.json') == {'marker': MARKER, 'path': str(ROOT), 'deviceId': DEVICE},
                'The capture ownership differs.')
        signal.alarm(130)
        capture(support, operator, owner, recover=recovering)
        return
    if recovering:
        require(operator.read_private(ROOT / 'OWNER.json') == {'marker': MARKER, 'path': str(ROOT), 'deviceId': DEVICE},
                'The retained capture ownership differs.')
        for name in ('worker.json', 'recovery-worker.json'):
            if operator.present(PRIVATE / name):
                worker = operator.read_private(PRIVATE / name)
                if operator.present(Path('/proc') / str(worker['pid'])):
                    current = operator.process_identity(worker['pid'])
                    require(any(current.get(key) != worker.get(key) for key in ('pid', 'bootId', 'startTicks')),
                            'An owned capture worker is still live; recovery cannot race it.')
    else:
        require(not operator.present(ROOT), 'The capture evidence already exists and will not be replaced.')
        require(operator.verify_media() == owner['media'], 'The approved media manifest changed.')
        ROOT.mkdir(mode=0o700)
        operator.save(ROOT / 'OWNER.json', {'marker': MARKER, 'path': str(ROOT), 'deviceId': DEVICE})
        PRIVATE.mkdir(mode=0o700)
        EXPORT.mkdir(mode=0o700)
        operator.save(PRIVATE / 'intent.json', {'marker': MARKER, 'sourceSha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            'serviceIdentity': owner['serviceIdentity'], 'maximumBusinessRequests': MAX_BUSINESS, 'maximumHttpAttempts': MAX_REQUESTS,
            'newRecorderDeviceId': DEVICE, 'businessWriteRequests': 0})
        operator.save(PRIVATE / 'operator.lock', '')
    operator.canonical(PRIVATE / 'operator.lock', mode=0o600)
    lock = os.open(PRIVATE / 'operator.lock', os.O_RDWR | os.O_NOFOLLOW)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(lock)
        raise RuntimeError('Another capture or recovery supervisor is active.') from None
    namespace = os.open(f'/proc/{PID}/ns/net', os.O_RDONLY)
    try:
        require(os.fstat(namespace).st_ino == int(owner['serviceIdentity']['networkNamespace'][5:-1]) and
                operator.service_identity(owner) == owner['serviceIdentity'], 'The namespace handle changed before capture.')
        result = subprocess.run(['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-I', '-B',
                                 str(Path(__file__).absolute()), '_recover' if recovering else '_capture'],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=150, check=False, pass_fds=(namespace,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
        require(result.returncode == 0, 'The capture failed; retained private acknowledgement supports exact cleanup.')
        print(result.stdout.decode().strip())
    finally:
        os.close(namespace)
        os.close(lock)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'result': 'failed', 'failureType': type(error).__name__, 'evidenceRetained': True}), file=sys.stderr)
        sys.exit(1)
