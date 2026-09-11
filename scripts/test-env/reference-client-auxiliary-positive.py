#!/usr/bin/env python3
"""Capture independent positive auxiliary contracts without business writes.

Each explicit phase has its own evidence root, recorder device, ordinary-viewer
token, and twenty-attempt budget. The accepted auxiliary reader provides the
unchanged base HTTP cap, private token acknowledgement, and exact-token cleanup.
Fixture names describe controls; rankings, theme owners, and status codes are
observed values rather than assertions about reference behavior.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import sys
import types
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
MEDIA = Path('/opt/goby-fixtures/client-aux-m3e-v1')
MEDIA_MARKER = 'goby-client-auxiliary-media-m3e-v1'
MEDIA_SHA = 'dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7'
READER_SHA = '4e06ff3ea848f1e3bece6d73f0969de9480ba5a62aed23c6a322bebd92f587f9'
INDEX_REPORT = WORK / 'reference-auxiliary-libraries-v1/export/report.json'
OLD_MEDIA = Path('/opt/goby-fixtures/client-m3e')
PHASES = ('movies', 'music', 'themes', 'music-roles', 'artist-exclusion')
ITEM_TYPES = 'Movie,Series,Season,Episode,MusicAlbum,Audio'
CATALOG_FIELDS = 'Path,Genres,People,Studios,Tags,ProductionYear,ParentId,SortName,DateCreated,Overview,ProviderIds'
SIMILAR_FIELDS = CATALOG_FIELDS + ',PrimaryImageAspectRatio'
THEME_FIELDS = SIMILAR_FIELDS + ',MediaSources,MediaStreams'
MAX_RESPONSE_BYTES, MAX_TOTAL_BYTES = 512 * 1024, 10 * 1024 * 1024
SHA = re.compile(r'[0-9a-f]{64}')


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def protected_bytes(path, limit=4 << 20):
    require(path.is_absolute() and '..' not in path.parts, 'An input path escaped its absolute scope.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not protected and root-owned.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in (0o600, 0o644) and before.st_size <= limit,
            'A source or manifest has unsafe ownership, type, permissions, links, or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        opened = os.fstat(handle.fileno())
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino), 'An input changed while opening.')
        raw = handle.read(limit + 1)
    after = path.lstat()
    require(len(raw) == before.st_size and (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) ==
            (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns), 'An input changed while reading.')
    return raw


def phase_identity(phase):
    require(phase in PHASES, 'An unknown capture phase was requested.')
    return {'root': WORK / ('reference-auxiliary-positive-v1-' + phase),
            'marker': 'goby-m3e-reference-auxiliary-positive-v1-' + phase,
            'device': 'goby-m3e-auxiliary-positive-' + phase + '-v1',
            'client': 'Goby Auxiliary Positive ' + phase.title() + ' Recorder'}


def load_inputs(args):
    require(args.phase in PHASES and args.manifest_sha256 == MEDIA_SHA and SHA.fullmatch(args.script_sha256 or ''),
            'The phase or explicit source and fixture bindings are invalid.')
    require(args.reader.name == 'reference-client-auxiliary-reads.py' and args.reader.is_relative_to(Path('/opt/goby-test')),
            'Only the accepted auxiliary reader may supply recorder support.')
    require(digest(protected_bytes(Path(__file__).absolute())) == args.script_sha256, 'The positive recorder source changed.')
    raw = protected_bytes(args.reader)
    require(digest(raw) == READER_SHA, 'The accepted twenty-request auxiliary reader changed.')
    base = types.ModuleType('accepted_positive_auxiliary_reader')
    base.__file__ = str(args.reader)
    exec(compile(raw, str(args.reader), 'exec'), base.__dict__)
    identity = phase_identity(args.phase)
    base.ROOT = identity['root']
    base.PRIVATE, base.EXPORT = base.ROOT / 'private', base.ROOT / 'export'
    base.MARKER, base.DEVICE, base.CLIENT = identity['marker'], identity['device'], identity['client']
    require(base.MAX_REQUESTS == 20 and base.MAX_BUSINESS == 17, 'The accepted HTTP limits differ.')
    support, op = base.load_support()
    # This new positive-fixture contract explicitly permits 512 KiB responses.
    # The prior reader evidence retains its original 64 KiB limit; only this
    # freshly loaded in-memory support module changes, never the support file.
    support.LIMIT = MAX_RESPONSE_BYTES
    op.preconditions()
    owner = op.read_private(op.OWNER)
    op.validate_owner(owner)
    require(owner.get('phase') == 'ready' and owner['serviceIdentity']['pid'] == base.PID and
            owner['serviceIdentity']['startTicks'] == base.TICKS and op.service_identity(owner) == owner['serviceIdentity'],
            'The accepted reference lifetime changed.')
    index_raw = protected_bytes(INDEX_REPORT)
    index = json.loads(index_raw)
    require(index.get('marker') == 'goby-reference-auxiliary-libraries-m3e-v1' and index.get('result') == 'complete' and
            index.get('manifestSha256') == MEDIA_SHA and index.get('referencePID') == base.PID and
            index.get('referenceStartTicks') == base.TICKS and index.get('serverId') == owner['serverId'] and
            index.get('newRecorderTokenRevoked') is True and index.get('allMediaPreserved') is True and
            index.get('originalLibrariesMetadataUserDataConfigurationPolicyPreserved') is True and index.get('failure') is None,
            'The auxiliary libraries have no completed preserved indexing receipt.')
    return base, support, op, owner, digest(index_raw)


def media_snapshot(op, owner, manifest_sha):
    raw = protected_bytes(MEDIA / 'manifest.json')
    require(digest(raw) == manifest_sha, 'The auxiliary media manifest changed.')
    manifest = json.loads(raw)
    require(manifest.get('marker') == MEDIA_MARKER and manifest.get('version') == 1 and manifest.get('root') == str(MEDIA) and
            manifest.get('oldManifestSha256') == owner['media']['manifestSha256'] and
            len(manifest.get('files', {})) == 43 and len(manifest.get('expectedItems', [])) == 19,
            'The auxiliary fixture manifest has an unexpected identity or inventory.')
    op.canonical(MEDIA, directory=True, mode=0o755)
    require(protected_bytes(MEDIA / '.goby-managed') == (MEDIA_MARKER + '\n').encode(), 'The auxiliary media owner changed.')
    files, directories = {}, []
    for path in sorted(MEDIA.rglob('*')):
        info = path.lstat()
        name = path.relative_to(MEDIA).as_posix()
        if stat.S_ISDIR(info.st_mode):
            op.canonical(path, directory=True, mode=0o755)
            directories.append(name)
            continue
        op.canonical(path, mode=0o644)
        if name == 'manifest.json':
            continue
        expected = manifest['files'].get(name)
        require(isinstance(expected, dict) and info.st_size <= 32 << 20, 'An auxiliary media member is unknown or oversized.')
        actual = {'sha256': op.digest(path), 'bytes': info.st_size, 'device': info.st_dev, 'inode': info.st_ino, 'links': info.st_nlink}
        require(actual == expected, 'Auxiliary media bytes, identity, or link count changed.')
        files[name] = actual
    require(files == manifest['files'] and directories == manifest.get('directories'), 'Auxiliary media membership changed.')
    old = op.verify_media()
    require(old == owner['media'], 'The original fixture media changed.')
    old_identities = {}
    for name in [*old['files'], 'manifest.json']:
        info = (OLD_MEDIA / name).lstat()
        old_identities[name] = {'device': info.st_dev, 'inode': info.st_ino, 'links': info.st_nlink, 'bytes': info.st_size}
    return manifest, {'manifestSha256': manifest_sha, 'manifestDevice': (MEDIA / 'manifest.json').lstat().st_dev,
                      'manifestInode': (MEDIA / 'manifest.json').lstat().st_ino, 'files': files, 'directories': directories,
                      'old': old, 'oldFileIdentities': old_identities}


def fixture_path(relative, manifest):
    path = PurePosixPath(relative)
    require(not path.is_absolute() and '..' not in path.parts and relative in manifest['files'],
            'A selector path is not a physical member of the pinned fixture.')
    return str(MEDIA / relative)


def catalog_rows(base, body):
    require(isinstance(body, dict) and isinstance(body.get('Items'), list) and 0 < len(body['Items']) <= 100 and
            type(body.get('TotalRecordCount')) is int and body['TotalRecordCount'] == len(body['Items']),
            'The complete bounded catalog is unavailable.')
    rows = body['Items']
    ids = set()
    for row in rows:
        require(isinstance(row, dict) and row.get('Type') in ITEM_TYPES.split(',') and isinstance(row.get('UserData'), dict),
                'A catalog item lacks its type or UserData preservation witness.')
        identity = base.numeric_id(row.get('Id'))
        require(identity not in ids, 'The catalog repeats an item identity.')
        ids.add(identity)
        path = row.get('Path')
        require(path is None or path == '' or isinstance(path, str) and '..' not in PurePosixPath(path).parts and
                any(path == str(root) or path.startswith(str(root) + '/') for root in (OLD_MEDIA, MEDIA)),
                'The catalog contains a path outside the two pinned fixtures.')
    return rows


def select_fixture(base, rows, manifest):
    def unique(kind, path):
        found = [row for row in rows if row.get('Type') == kind and row.get('Path') == path]
        require(len(found) == 1, 'A manifest item or hierarchy path has no unique catalog identity.')
        return found[0]

    movies, tracks, albums = {}, {}, {}
    for expected in manifest['expectedItems']:
        item = unique(expected['kind'], fixture_path(expected['media'], manifest))
        if expected['kind'] == 'Movie':
            movies[expected['key']] = item
        elif expected['kind'] == 'Audio':
            tracks[(expected['albumKey'], expected['number'])] = item
    for definition in manifest['albums']:
        key = definition['key']
        album_ids = set()
        for track in definition['tracks']:
            audio = tracks[(key, track['number'])]
            candidates = {base.numeric_id(str(audio[field])) for field in ('AlbumId', 'ParentId')
                          if audio.get(field) is not None and str(audio[field]) in {row['Id'] for row in rows if row['Type'] == 'MusicAlbum'}}
            require(len(candidates) == 1, 'A real Audio album relationship is missing or ambiguous.')
            album_ids.update(candidates)
        require(len(album_ids) == 1, 'Tracks intended for one album have different actual album identities.')
        album = next(row for row in rows if row['Id'] in album_ids)
        require(album.get('Type') == 'MusicAlbum' and album.get('Name') == definition['title'],
                'The observed Audio album relationship resolves to another control.')
        albums[key] = album
    artists = {}
    for row in [*albums.values(), *tracks.values()]:
        for field in ('AlbumArtists', 'ArtistItems'):
            entries = row.get(field, [])
            require(isinstance(entries, list) and len(entries) <= 32, 'Artist references exceed their bounded shape.')
            for entry in entries:
                require(isinstance(entry, dict), 'An artist reference is not an object.')
                if entry.get('Name') in ('ArtistA', 'ArtistB'):
                    identity = base.numeric_id(entry.get('Id'))
                    name = entry['Name']
                    require(name not in artists or artists[name] == identity, 'One actual artist name resolves to multiple IDs.')
                    artists[name] = identity
    require(set(artists) == {'ArtistA', 'ArtistB'} and artists['ArtistA'] != artists['ArtistB'],
            'The two actual music artists are not independently identified.')
    tv_root = MEDIA / 'TV/M3e Auxiliary Series'
    hierarchy = {'Series': unique('Series', str(tv_root)),
                 'Season01': unique('Season', str(tv_root / 'Season 01')),
                 'Season02': unique('Season', str(tv_root / 'Season 02'))}
    for season in (1, 2):
        expected = next(row for row in manifest['expectedItems'] if row['kind'] == 'Episode' and row['season'] == season)
        episode = unique('Episode', fixture_path(expected['media'], manifest))
        require(episode.get('ParentId') == hierarchy[f'Season{season:02d}']['Id'] and
                hierarchy[f'Season{season:02d}'].get('ParentId') == hierarchy['Series']['Id'],
                'The actual series, season, and episode parent links are inconsistent.')
        hierarchy[f'Episode{season:02d}'] = episode
    return {'movies': movies, 'tracks': tracks, 'albums': albums, 'artists': artists,
            'hierarchy': hierarchy, 'movie_parent': None}


def phase_routes(phase, user_id, selected):
    routes, omitted = {}, []

    def similar(label, identity, **options):
        query = {'UserId': user_id, 'Limit': '12', 'Fields': SIMILAR_FIELDS, 'ImageTypeLimit': '1', **options}
        routes[label] = '/emby/Items/' + identity + '/Similar?' + urlencode(query)

    def theme(label, identity, **options):
        routes[label] = '/emby/Items/' + identity + '/ThemeMedia?' + urlencode({'UserId': user_id, 'Fields': THEME_FIELDS, **options})

    if phase == 'movies':
        movies, seed = selected['movies'], selected['movies']['Seed']['Id']
        similar('seed-baseline-first', seed)
        similar('seed-baseline-second', seed)
        similar('seed-limit3-start0', seed, Limit='3', StartIndex='0')
        similar('seed-limit3-start3', seed, Limit='3', StartIndex='3')
        similar('seed-limit0', seed, Limit='0')
        similar('seed-limit3-total-false', seed, Limit='3', EnableTotalRecordCount='false')
        similar('seed-exclude-allmatches', seed, ExcludeItemIds=movies['AllMatches']['Id'])
        if selected['movie_parent'] is not None:
            similar('seed-movies-parent', seed, ParentId=selected['movie_parent'])
        else:
            omitted.append({'case': 'seed-movies-parent', 'reason': 'The current catalog did not prove the Movies library path and parent identity.'})
        similar('seed-sortname-desc', seed, SortBy='SortName', SortOrder='Descending')
        similar('noshared-baseline', movies['NoShared']['Id'])
        similar('yearfar-baseline', movies['YearFar']['Id'])
    elif phase == 'music':
        albums, tracks, artists = selected['albums'], selected['tracks'], selected['artists']
        seed, audio = albums['SeedAlbum']['Id'], tracks[('SeedAlbum', 1)]['Id']
        similar('seedalbum-baseline', seed)
        similar('seedalbum-exclude-artist-a', seed, ExcludeArtistIds=artists['ArtistA'])
        similar('seedalbum-exclude-artist-b', seed, ExcludeArtistIds=artists['ArtistB'])
        similar('seedalbum-exclude-both', seed, ExcludeArtistIds=artists['ArtistA'] + ',' + artists['ArtistB'])
        similar('mixedalbum-baseline', albums['MixedAlbum']['Id'])
        similar('seedaudio-baseline', audio)
        similar('seedaudio-exclude-artist-a', audio, ExcludeArtistIds=artists['ArtistA'])
        similar('seedaudio-exclude-artist-b', audio, ExcludeArtistIds=artists['ArtistB'])
        similar('differentartistaudio-baseline', tracks[('DifferentArtistAlbum', 1)]['Id'])
        similar('artist-a-seed-similar', artists['ArtistA'])
        similar('seedalbum-limit1-start1', seed, Limit='1', StartIndex='1')
        similar('seedalbum-total-false', seed, EnableTotalRecordCount='false')
    elif phase == 'music-roles':
        albums, tracks = selected['albums'], selected['tracks']
        seed_album, seed_audio = albums['SeedAlbum']['Id'], tracks[('SeedAlbum', 1)]['Id']
        different_audio = tracks[('DifferentArtistAlbum', 1)]['Id']
        similar('differentalbum-b-baseline', albums['DifferentArtistAlbum']['Id'])
        similar('mixedaudio-track2-b-baseline', tracks[('MixedAlbum', 2)]['Id'])
        similar('mixedaudio-track1-a-baseline', tracks[('MixedAlbum', 1)]['Id'])
        for label, identity in (('differentaudio-b', different_audio), ('seedaudio-a', seed_audio),
                                ('seedalbum-a', seed_album), ('mixedalbum', albums['MixedAlbum']['Id'])):
            similar(label + '-artist-type-artist', identity, ArtistType='Artist')
            similar(label + '-artist-type-albumartist', identity, ArtistType='AlbumArtist')
        similar('seedalbum-limit0', seed_album, Limit='0')
        similar('seedaudio-limit0', seed_audio, Limit='0')
    elif phase == 'artist-exclusion':
        tracks, albums, artists = selected['tracks'], selected['albums'], selected['artists']
        similar('mixedaudio-a-exclude-artist-a', tracks[('MixedAlbum', 1)]['Id'], ExcludeArtistIds=artists['ArtistA'])
        similar('mixedaudio-b-exclude-artist-a', tracks[('MixedAlbum', 2)]['Id'], ExcludeArtistIds=artists['ArtistA'])
        similar('mixedaudio-b-exclude-artist-b', tracks[('MixedAlbum', 2)]['Id'], ExcludeArtistIds=artists['ArtistB'])
        similar('mixedalbum-exclude-artist-a', albums['MixedAlbum']['Id'], ExcludeArtistIds=artists['ArtistA'])
    else:
        movies, hierarchy = selected['movies'], selected['hierarchy']
        seed = movies['Seed']['Id']
        theme('movie-seed-default', seed)
        theme('movie-seed-inherit-true', seed, InheritFromParent='true')
        theme('movie-seed-songs-false', seed, EnableThemeSongs='false')
        theme('movie-seed-both-false', seed, EnableThemeSongs='false', EnableThemeVideos='false')
        theme('allmatches-default', movies['AllMatches']['Id'])
        theme('noshared-default', movies['NoShared']['Id'])
        theme('series-default', hierarchy['Series']['Id'])
        for season in ('Season01', 'Season02'):
            theme(season.lower() + '-inherit-false', hierarchy[season]['Id'], InheritFromParent='false')
            theme(season.lower() + '-inherit-true', hierarchy[season]['Id'], InheritFromParent='true')
        theme('episode01-inherit-true', hierarchy['Episode01']['Id'], InheritFromParent='true')
        theme('episode02-inherit-true', hierarchy['Episode02']['Id'], InheritFromParent='true')
    require(len(routes) <= 13, 'The positive route list exceeds its thirteen-case budget.')
    return routes, omitted


def make_recorder(base, support, op, owner, args, manifest):
    parent = type(base.make_recorder(support, op, owner))

    class PositiveRecorder(parent):
        def __init__(self):
            super().__init__()
            self.selected, self.omitted, self.observed_cases = {}, [], []

        def request(self, label, method, route, **kwargs):
            require(support.LIMIT == MAX_RESPONSE_BYTES and self.bytes_read <= MAX_TOTAL_BYTES,
                    'The explicit positive-capture response budget changed.')
            result = super().request(label, method, route, **kwargs)
            require(self.bytes_read <= MAX_TOTAL_BYTES, 'The phase exceeded its ten-MiB total response budget.')
            return result

        def export(self, path, value):
            def mask(item):
                if isinstance(item, dict):
                    return {key: mask(entry) for key, entry in item.items()}
                if isinstance(item, list):
                    return [mask(entry) for entry in item]
                return item.replace(str(MEDIA) + '/', '[auxiliary fixture]/') if isinstance(item, str) else item
            return super().export(path, mask(value))

        def capture(self):
            self.request('recorder-login', 'POST', '/emby/Users/AuthenticateByName', authenticated=False, login=True)
            require(self.token_identity_proven, 'The independent ordinary-viewer login was not proven.')
            user = '/emby/Users/' + self.user_id
            catalog = user + '/Items?' + urlencode({'Recursive': 'true', 'IncludeItemTypes': ITEM_TYPES, 'Fields': CATALOG_FIELDS, 'Limit': '100'})
            self.routes = {'viewer-before': user, 'catalog-before': catalog}
            status, before = self.get('viewer-before')
            require(status == 200 and isinstance(before, dict) and before.get('Id') == self.user_id and
                    isinstance(before.get('Configuration'), dict) and isinstance(before.get('Policy'), dict),
                    'The viewer configuration and policy baseline is unavailable.')
            status, catalog_before = self.get('catalog-before')
            require(status == 200, 'The complete old and new catalog baseline is unavailable.')
            rows = catalog_rows(base, catalog_before)
            selected = select_fixture(base, rows, manifest)
            self.selected = {'movies': {key: row['Id'] for key, row in selected['movies'].items()},
                             'albums': {key: row['Id'] for key, row in selected['albums'].items()},
                             'tracks': {key[0] + '/' + str(key[1]): row['Id'] for key, row in selected['tracks'].items()},
                             'artists': selected['artists'], 'hierarchy': {key: row['Id'] for key, row in selected['hierarchy'].items()},
                             'movieParent': selected['movie_parent']}
            self.catalog = rows
            cases, self.omitted = phase_routes(args.phase, self.user_id, selected)
            self.routes.update(cases)
            self.routes.update({'viewer-after': user, 'catalog-after': catalog})
            for label in cases:
                self.get(label)
                self.observed_cases.append(label)
            status, after = self.get('viewer-after')
            self.configuration_preserved = status == 200 and isinstance(after, dict) and all(
                before.get(key) == after.get(key) for key in ('Configuration', 'Policy'))
            status, catalog_after = self.get('catalog-after')
            self.catalog_preserved = status == 200 and base.catalog_projection(catalog_rows(base, catalog_after)) == base.catalog_projection(rows)
            require(self.configuration_preserved and self.catalog_preserved,
                    'Observed configuration, policy, catalog metadata, UserData, or item IDs changed.')

    recorder = PositiveRecorder()
    support.approve = recorder.approve
    return recorder


def run_capture(base, support, op, owner, args, manifest, media_before, index_sha):
    recorder = make_recorder(base, support, op, owner, args, manifest)
    op.save(base.PRIVATE / ('recovery-worker.json' if args.recover else 'worker.json'), op.process_identity(os.getpid()))
    failure, media_preserved = None, False
    try:
        signal.alarm(100)
        if args.recover:
            recorder.recovering = True
            recorder.restore_budget()
            login = op.read_private(base.PRIVATE / 'recorder-login.json')
            recorder.token = login.get('AccessToken')
            require(isinstance(recorder.token, str) and recorder.token, 'The owned login acknowledgement is unavailable.')
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
        try:
            _, media_after = media_snapshot(op, owner, args.manifest_sha256)
            require(media_after == media_before and digest(protected_bytes(INDEX_REPORT)) == index_sha,
                    'The old/new media or accepted indexing evidence changed.')
            recorder.identity()
            media_preserved = True
        except Exception as error:
            failure = failure or type(error).__name__
        signal.alarm(0)
    report = {'marker': base.MARKER, 'phase': args.phase, 'classification': 'protocol research; not client acceptance',
              'sourceSha256': args.script_sha256, 'acceptedReaderSha256': READER_SHA, 'auxiliaryManifestSha256': args.manifest_sha256,
              'acceptedIndexReportSha256': index_sha, 'referenceIdentity': owner['serviceIdentity'],
              'httpAttemptsReserved': recorder.sequence, 'maximumHttpAttempts': 20, 'businessRequestsReserved': recorder.business_count,
              'maximumSingleResponseBytes': MAX_RESPONSE_BYTES, 'maximumPhaseResponseBytes': MAX_TOTAL_BYTES,
              'responseBytesRead': recorder.bytes_read, 'priorReaderEvidenceResponseLimitBytes': 64 * 1024,
              'maximumBusinessCases': 13, 'baselineRequests': 4, 'observedCases': recorder.observed_cases, 'omittedCases': recorder.omitted,
              'selectedActualIds': recorder.selected, 'approvedFixtureCatalog': recorder.catalog,
              'configurationAndPolicyPreserved': recorder.configuration_preserved, 'catalogMetadataUserDataAndIdsPreserved': recorder.catalog_preserved,
              'oldAndNewMediaBytesIdsAndManifestsPreserved': media_preserved, 'newRecorderTokenRevoked': recorder.revoked,
              'businessWrites': 0, 'playbackRequests': 0, 'headRequests': 0, 'browserSessionUsedOrRevoked': False,
              'caseStatuses': {key: value['response']['status'] for key, value in recorder.records.items()},
              'failureType': failure, 'recoveryOnly': args.recover,
              'boundary': 'Status, ranking order, pagination, and theme owner IDs are observations. No expected score or unknown-owner constant is imposed. '
                          'Names and generated tags identify controls, not verified ranking behavior. Complete non-200 cases do not stop capture.'}
    export = base.EXPORT if not args.recover else base.ROOT / 'recovery-export'
    if args.recover:
        require(not op.present(export), 'Recovery export already exists and cannot be replaced.')
        export.mkdir(mode=0o700)
    for label, record in recorder.records.items():
        recorder.export(export / (label + '.json'), record)
    recorder.export(export / 'report.json', report)
    require(failure is None and recorder.revoked and media_preserved, 'Capture or exact-token cleanup is incomplete; retain the evidence.')
    print(json.dumps({'result': 'complete', 'phase': args.phase, 'requests': recorder.sequence,
                      'newRecorderTokenRevoked': recorder.revoked, 'report': str(export / 'report.json')}), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phase', choices=PHASES, required=True)
    parser.add_argument('--manifest-sha256', required=True)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--reader', type=Path, required=True)
    parser.add_argument('--recover', action='store_true')
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--lock-fd', type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'), 'Run through authorized root SSH only.')
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('The bounded phase deadline expired.')))
    base, support, op, owner, index_sha = load_inputs(args)
    manifest, media = media_snapshot(op, owner, args.manifest_sha256)
    inputs = {'phase': args.phase, 'scriptSha256': args.script_sha256, 'readerSha256': READER_SHA,
              'manifestSha256': args.manifest_sha256, 'indexReportSha256': index_sha,
              'maximumHttpAttempts': 20, 'maximumSingleResponseBytes': MAX_RESPONSE_BYTES, 'maximumPhaseResponseBytes': MAX_TOTAL_BYTES}
    if args.worker:
        require(type(args.lock_fd) is int and args.lock_fd > 2, 'The phase worker lacks its inherited exclusive lock.')
        control = op.read_private(base.ROOT / 'OWNER.json')
        supervisor = control.get('supervisor')
        if args.recover:
            recovery_supervisor = op.read_private(base.PRIVATE / 'recovery-supervisor.json')
            require(recovery_supervisor.get('inputs') == inputs, 'The cleanup supervisor belongs to different phase inputs.')
            supervisor = recovery_supervisor.get('supervisor')
        require(control.get('marker') == base.MARKER and control.get('inputs') == inputs and control.get('media') == media and
                op.process_identity(os.getppid()) == supervisor and
                (os.fstat(args.lock_fd).st_dev, os.fstat(args.lock_fd).st_ino) ==
                (op.canonical(base.PRIVATE / 'operator.lock', mode=0o600).st_dev,
                 op.canonical(base.PRIVATE / 'operator.lock', mode=0o600).st_ino),
                'The phase worker is not bound to its supervisor, lock, inputs, and media.')
        signal.alarm(155)
        run_capture(base, support, op, owner, args, manifest, media, index_sha)
        return
    require(args.lock_fd is None, 'Only an internal worker may receive a lock descriptor.')
    if args.recover:
        control = op.read_private(base.ROOT / 'OWNER.json')
        require(control.get('marker') == base.MARKER and control.get('inputs') == inputs and control.get('media') == media,
                'The retained phase belongs to different inputs or media.')
        for name in ('worker.json', 'recovery-worker.json'):
            if op.present(base.PRIVATE / name):
                worker = op.read_private(base.PRIVATE / name)
                if op.present(Path('/proc') / str(worker['pid'])):
                    current = op.process_identity(worker['pid'])
                    require(any(current.get(key) != worker.get(key) for key in ('pid', 'bootId', 'startTicks')),
                            'An owned phase worker remains alive.')
    else:
        require(not op.present(base.ROOT), 'This phase already has evidence; it cannot be rerun or adopted.')
        base.ROOT.mkdir(mode=0o700)
        base.PRIVATE.mkdir(mode=0o700)
        base.EXPORT.mkdir(mode=0o700)
        control = {'marker': base.MARKER, 'path': str(base.ROOT), 'deviceId': base.DEVICE, 'inputs': inputs,
                   'supervisor': op.process_identity(os.getpid()), 'media': media}
        op.save(base.ROOT / 'OWNER.json', control)
        op.save(base.PRIVATE / 'intent.json', {'marker': base.MARKER, 'inputs': inputs, 'phase': args.phase,
                                             'deviceId': base.DEVICE, 'businessWrites': 0,
                                             'maximumBusinessCases': 13, 'baselineRequests': 4})
        op.save(base.PRIVATE / 'operator.lock', '')
    lock = os.open(base.PRIVATE / 'operator.lock', os.O_RDWR | os.O_NOFOLLOW)
    namespace = None
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.recover:
            # A separate receipt binds cleanup to this supervisor without
            # replacing the original worker or the original phase owner.
            op.save(base.PRIVATE / 'recovery-supervisor.json', {'inputs': inputs, 'supervisor': op.process_identity(os.getpid())})
        namespace = os.open(f'/proc/{base.PID}/ns/net', os.O_RDONLY)
        require(os.fstat(namespace).st_ino == int(owner['serviceIdentity']['networkNamespace'][5:-1]) and
                op.service_identity(owner) == owner['serviceIdentity'], 'The attested reference namespace changed.')
        arguments = ['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-I', '-B',
                     str(Path(__file__).absolute()), '--phase', args.phase, '--manifest-sha256', args.manifest_sha256,
                     '--script-sha256', args.script_sha256, '--reader', str(args.reader), '--worker', '--lock-fd', str(lock)]
        if args.recover:
            arguments.append('--recover')
        result = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, stdin=subprocess.DEVNULL,
                                timeout=170, check=False, pass_fds=(namespace, lock),
                                env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
        require(result.returncode == 0, 'The phase failed; retained acknowledgement supports exact-token cleanup only.')
        print(result.stdout.decode().strip())
    finally:
        if namespace is not None:
            os.close(namespace)
        os.close(lock)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'result': 'failed', 'failureType': type(error).__name__, 'evidenceRetained': True}), file=sys.stderr)
        sys.exit(1)
