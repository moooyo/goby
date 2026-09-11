#!/usr/bin/env python3
"""Generate a new synthetic Similar/ThemeMedia fixture only on ssh test-env.

Both fixed output roots must be absent. A private intent precedes all media
creation. Existing M3e bytes, inode identities, and hard-link counts are checked
before and after; they are never used as writable inputs or hard-link sources.
This operator performs no networking, library scan, reference login, or database
operation. Failed attempts retain all artifacts and cannot be resumed implicitly.
"""

from __future__ import annotations

from collections import Counter
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import resource
import selectors
import signal
import stat
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

sys.dont_write_bytecode = True

WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = Path('/opt/goby-fixtures/client-aux-m3e-v1')
CONTROL = WORK / 'auxiliary-media-v1'
OLD = Path('/opt/goby-fixtures/client-m3e')
OLD_MANIFEST_SHA = 'd071081ea17decbc07d3191ddddd4ab5415717ac907f7e86c5c8c302878bc064'
MARKER = 'goby-client-auxiliary-media-m3e-v1'
FFMPEG = Path('/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg')
FFPROBE = FFMPEG.with_name('ffprobe')
ENV = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
MAX_OUTPUT = 2 << 20
MAX_MEDIA = 16 << 20
MAX_TREE_BYTES = 160 << 20
MAX_FILES = 128
SECONDS = 12
THEME_DOCUMENT = 'https://emby.media/support/articles/Theme-Songs-Videos.html'
PROFILE = {'durationSeconds': SECONDS, 'videoCodec': 'h264', 'audioCodec': 'aac',
           'width': 320, 'height': 180, 'fps': '30/1', 'channels': 2, 'sampleRate': 48000}


class Failure(Exception):
    """A bounded preparation guard failed; retain every artifact."""


def require(value, message):
    if not value:
        raise Failure(message)


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, ensure_ascii=True) + '\n').encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid, 'gid': info.st_gid,
            'mode': stat.S_IMODE(info.st_mode), 'links': info.st_nlink, 'bytes': info.st_size,
            'mtimeNs': info.st_mtime_ns, 'ctimeNs': info.st_ctime_ns}


def safe_path(path, *, directory=False, mode=None, links=False):
    require(path.is_absolute() and '..' not in path.parts, 'A path escaped its absolute fixed scope.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'A path has an unsafe administrative ancestor.')
    info = path.lstat()
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)) and
            info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022 and
            (mode is None or stat.S_IMODE(info.st_mode) == mode) and
            (directory or links or info.st_nlink == 1), 'A path has an unexpected type, owner, mode, or link count.')
    return info


def digest(path, *, mode=None, links=False, limit=512 << 20):
    before = safe_path(path, mode=mode, links=links)
    require(before.st_size <= limit, 'A file exceeds its fixed read bound.')
    result = hashlib.sha256()
    size = 0
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC), 'rb') as source:
        require(identity(os.fstat(source.fileno())) == identity(before), 'A file changed while opening.')
        while block := source.read(65536):
            size += len(block)
            require(size <= limit, 'A file grew beyond its bounded read.')
            result.update(block)
        require(identity(os.fstat(source.fileno())) == identity(before), 'A file changed while hashing.')
    require(identity(safe_path(path, mode=mode, links=links)) == identity(before), 'A file changed during hashing.')
    return result.hexdigest()


def read_small(path, mode=0o600):
    before = safe_path(path, mode=mode)
    require(before.st_size <= MAX_OUTPUT, 'A control file exceeds its bounded size.')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(descriptor, 'rb') as source:
        require(identity(os.fstat(source.fileno())) == identity(before), 'A control file changed while opening.')
        raw = source.read(MAX_OUTPUT + 1)
        require(len(raw) == before.st_size and identity(os.fstat(source.fileno())) == identity(before),
                'A control file changed while reading.')
    return raw


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def missing(path):
    try:
        path.lstat()
    except FileNotFoundError:
        return
    raise Failure('A fixed output path already exists; no adoption, replacement, or retry is permitted.')


def new_directory(path, private=False):
    require(path == ROOT or path.is_relative_to(ROOT) or path == CONTROL or path.is_relative_to(CONTROL),
            'Directory creation escaped the two new roots.')
    safe_path(path.parent, directory=True)
    path.mkdir(mode=0o700 if private else 0o755)
    safe_path(path, directory=True, mode=0o700 if private else 0o755)
    sync_directory(path.parent)


def write_new(path, raw, private=False):
    require(path.is_relative_to(ROOT) or path.is_relative_to(CONTROL), 'File creation escaped the two new roots.')
    safe_path(path.parent, directory=True)
    mode = 0o600 if private else 0o644
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    with os.fdopen(descriptor, 'wb') as target:
        target.write(raw)
        target.flush()
        os.fsync(target.fileno())
    safe_path(path, mode=mode)
    sync_directory(path.parent)


def old_snapshot():
    safe_path(WORK, directory=True, mode=0o700)
    require(json.loads(read_small(WORK / 'OWNER.json')) == {
        'marker': 'goby-m3e-client-acceptance-v1', 'path': str(WORK)}, 'The existing work owner differs.')
    safe_path(ROOT.parent, directory=True, mode=0o755)
    require(read_small(ROOT.parent / '.goby-managed', 0o644) == b'goby-generated-media-fixtures\n',
            'The existing media-parent owner differs.')
    root_info = safe_path(OLD, directory=True, mode=0o755)
    manifest_raw = read_small(OLD / 'manifest.json', 0o644)
    require(sha(manifest_raw) == OLD_MANIFEST_SHA, 'The original fourteen-file manifest changed.')
    manifest = json.loads(manifest_raw)
    require(manifest['marker'] == 'goby-client-media-m3e-v1' and len(manifest['files']) == 14,
            'The original media manifest is not the accepted fixture.')
    files, directories, link_counts = {}, {}, Counter()
    for path in sorted(OLD.rglob('*')):
        require(len(files) + len(directories) < 64, 'The old fixture exceeded its bounded inventory.')
        info = path.lstat()
        relative = path.relative_to(OLD).as_posix()
        if stat.S_ISDIR(info.st_mode):
            directories[relative] = identity(safe_path(path, directory=True, mode=0o755))
        else:
            info = safe_path(path, mode=0o644, links=True)
            files[relative] = {'identity': identity(info), 'sha256': digest(path, mode=0o644, links=True)}
            link_counts[(info.st_dev, info.st_ino)] += 1
    require({name: value['sha256'] for name, value in files.items() if name != 'manifest.json'} == manifest['files'],
            'The old media membership or bytes changed.')
    require(all(value['identity']['links'] == link_counts[(value['identity']['device'], value['identity']['inode'])]
                for value in files.values()), 'The original hard-link population is not closed.')
    return {'root': identity(root_info), 'files': files, 'directories': directories,
            'manifestSha256': OLD_MANIFEST_SHA, 'ffmpegSha256': manifest['ffmpegSha256']}


def movie_matrix():
    names = ('Seed', 'AllMatches', 'GenreBoth', 'GenreOne', 'TagBoth', 'TagOne', 'Studio',
             'Actor', 'Director', 'YearNear', 'YearFar', 'NoShared')
    result = []
    for name in names:
        all_features = name in ('Seed', 'AllMatches')
        result.append({'key': name, 'title': 'M3e Auxiliary ' + name,
                       'year': 2001 if name == 'YearNear' else 2025 if name == 'YearFar' else 2000,
                       'genres': ['Drama', 'Adventure'] if all_features or name == 'GenreBoth' else ['Drama'] if name == 'GenreOne' else [],
                       'tags': ['SharedOne', 'SharedTwo'] if all_features or name == 'TagBoth' else ['SharedOne'] if name == 'TagOne' else [],
                       'studios': ['SharedStudio'] if all_features or name == 'Studio' else [],
                       'actors': ['CommonActor'] if all_features or name == 'Actor' else [],
                       'directors': ['CommonDirector'] if all_features or name == 'Director' else []})
    return result


def music_matrix():
    return [{'key': album, 'title': 'M3e Auxiliary ' + album,
             'albumArtist': 'ArtistB' if album == 'DifferentArtistAlbum' else 'ArtistA',
             'tracks': [{'number': index, 'title': f'M3e Auxiliary {album} Track {index}', 'artist': artist,
                         'albumArtist': 'ArtistB' if album == 'DifferentArtistAlbum' else 'ArtistA'}
                        for index, artist in enumerate(artists, 1)]}
            for album, artists in (('SeedAlbum', ['ArtistA']), ('SameArtistAlbum', ['ArtistA']),
                                   ('DifferentArtistAlbum', ['ArtistB']), ('MixedAlbum', ['ArtistA', 'ArtistB']))]


def expected_paths(movies, albums):
    files = {'.goby-managed'}
    directories = {'Movies', 'TV', 'Music'}
    for item in movies:
        folder = 'Movies/' + item['key']
        directories.add(folder)
        files.update({folder + '/movie.mp4', folder + '/movie.nfo'})
    files.update({'Movies/Seed/theme.mp3', 'Movies/AllMatches/theme-music/one.mp3',
                  'Movies/AllMatches/theme-music/two.flac', 'Movies/NoShared/backdrops/video.mp4'})
    directories.update({'Movies/AllMatches/theme-music', 'Movies/NoShared/backdrops'})
    series = 'TV/M3e Auxiliary Series'
    directories.add(series)
    files.update({series + '/tvshow.nfo', series + '/theme.mp3'})
    for season in (1, 2):
        folder = series + f'/Season {season:02d}'
        directories.add(folder)
        stem = folder + f'/M3e Auxiliary Series S{season:02d}E01'
        files.update({folder + '/season.nfo', stem + '.mp4', stem + '.nfo'})
    directories.add(series + '/Season 02/theme-music')
    files.add(series + '/Season 02/theme-music/season.mp3')
    for album in albums:
        folder = 'Music/' + album['key']
        directories.add(folder)
        files.update(folder + f"/{track['number']:02d}.mp3" for track in album['tracks'])
    return files, directories


def xml_bytes(kind, fields):
    root = ET.Element(kind)
    for name, values in fields.items():
        for value in values if isinstance(values, list) else [values]:
            element = ET.SubElement(root, name)
            if name == 'actor':
                ET.SubElement(element, 'name').text = value
            else:
                element.text = str(value)
    return ET.tostring(root, encoding='utf-8', xml_declaration=True) + b'\n'


def command_limits():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_CPU, (120, 120))
    resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_MEDIA, MAX_MEDIA))


def run(arguments, label):
    require(arguments[0] in (FFMPEG, FFPROBE) and label.replace('-', '').isalnum(),
            'An unapproved executable or log label was selected.')
    log = CONTROL / (label + '.log')
    safe_path(CONTROL, directory=True, mode=0o700)
    descriptor = os.open(log, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    output = bytearray()
    with os.fdopen(descriptor, 'wb') as evidence:
        process = subprocess.Popen([str(value) for value in arguments], stdin=subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=ENV,
                                   start_new_session=True, preexec_fn=command_limits)
        try:
            deadline = time.monotonic() + 150
            with selectors.DefaultSelector() as ready:
                ready.register(process.stdout, selectors.EVENT_READ)
                finished_output = False
                while not finished_output:
                    require(time.monotonic() < deadline, 'A media command exceeded its time bound.')
                    if not ready.select(0.25):
                        continue
                    block = os.read(process.stdout.fileno(), 65536)
                    if not block:
                        finished_output = True
                        continue
                    require(len(output) + len(block) <= MAX_OUTPUT, 'A media command exceeded its output bound.')
                    output.extend(block)
                    evidence.write(block)
                require(process.wait(timeout=max(1, deadline - time.monotonic())) == 0,
                        'A media command failed; retain its private log and partial output.')
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
            process.stdout.close()
            evidence.flush()
            os.fsync(evidence.fileno())
    sync_directory(CONTROL)
    return bytes(output)


def generated_file(path):
    info = safe_path(path, mode=0o644)
    require(0 < info.st_size <= MAX_MEDIA, 'A generated media file exceeds its nonempty size bound.')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    sync_directory(path.parent)


def copy_new(source, target):
    require(source.is_relative_to(ROOT) and target.is_relative_to(ROOT), 'Media copies must stay in the new root.')
    before = safe_path(source, mode=0o644)
    require(0 < before.st_size <= MAX_MEDIA, 'A media copy input exceeds its bound.')
    safe_path(target.parent, directory=True, mode=0o755)
    source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    target_fd = None
    try:
        require(identity(os.fstat(source_fd)) == identity(before), 'A generated copy source changed.')
        target_fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o644)
        with os.fdopen(target_fd, 'wb', closefd=False) as output:
            while block := os.read(source_fd, 65536):
                output.write(block)
            output.flush()
            os.fsync(target_fd)
        require(identity(os.fstat(source_fd)) == identity(before), 'A generated source changed during copying.')
        require((os.fstat(target_fd).st_dev, os.fstat(target_fd).st_ino) != (before.st_dev, before.st_ino),
                'A media copy unexpectedly shares its source inode.')
    finally:
        if target_fd is not None:
            os.close(target_fd)
        os.close(source_fd)
    generated_file(target)
    require(digest(target) == digest(source), 'An independent media copy differs from its generated source.')


def probe(path, label, codec, *, video=False, tags=None):
    raw = run([FFPROBE, '-v', 'error', '-protocol_whitelist', 'file', '-show_format', '-show_streams',
               '-of', 'json', path], label)
    value = json.loads(raw)
    streams = value['streams']
    audio = [stream for stream in streams if stream.get('codec_type') == 'audio']
    videos = [stream for stream in streams if stream.get('codec_type') == 'video']
    require(len(streams) == len(audio) + len(videos) and len(audio) == 1 and
            audio[0].get('codec_name') == codec and audio[0].get('channels') == 2 and
            audio[0].get('sample_rate') == '48000' and 11.9 <= float(value['format']['duration']) <= 12.2,
            'Generated audio or duration differs from its declared profile.')
    if video:
        require(len(videos) == 1 and videos[0].get('codec_name') == 'h264' and
                videos[0].get('width') == 320 and videos[0].get('height') == 180 and
                videos[0].get('avg_frame_rate') == '30/1', 'Generated video differs from its declared profile.')
    else:
        require(not videos, 'An audio fixture unexpectedly contains video.')
    if tags:
        actual = {key.lower(): val for key, val in value['format'].get('tags', {}).items()}
        require(all(actual.get(key) == val for key, val in tags.items()), 'Generated music tags differ from their declared metadata.')
    return {'durationSeconds': float(value['format']['duration']), 'audioCodec': codec,
            'videoCodec': 'h264' if video else None, 'audioChannels': 2, 'sampleRate': 48000,
            'width': 320 if video else None, 'height': 180 if video else None,
            'fps': '30/1' if video else None, 'tags': tags or {}, 'probeSha256': sha(raw)}


def generate_audio(path, codec, label, tags=None):
    missing(path)
    command = [FFMPEG, '-nostdin', '-hide_banner', '-loglevel', 'error', '-n', '-protocol_whitelist', 'file,lavfi',
               '-f', 'lavfi', '-i', 'sine=frequency=660:sample_rate=48000', '-t', str(SECONDS),
               '-vn', '-c:a', codec, '-ac', '2', '-ar', '48000', '-threads:a', '1']
    for name, value in (tags or {}).items():
        command.extend(['-metadata', f'{name}={value}'])
    command.append(path)
    run(command, label + '-generate')
    generated_file(path)
    return probe(path, label + '-probe', 'mp3' if codec == 'libmp3lame' else 'flac', tags=tags)


def generate(before, tools):
    movies, albums = movie_matrix(), music_matrix()
    planned_files, planned_directories = expected_paths(movies, albums)
    write_new(CONTROL / 'intent.json', encoded({'marker': MARKER, 'phase': 'intent',
              'createdAt': dt.datetime.now(dt.timezone.utc).isoformat(), 'root': str(ROOT), 'control': str(CONTROL),
              'scriptSha256': digest(Path(__file__), limit=MAX_OUTPUT),
              'oldSnapshot': before, 'tools': tools, 'movies': movies, 'albums': albums,
              'videoProfile': PROFILE, 'themeLayoutSource': THEME_DOCUMENT,
              'expectedFiles': sorted(planned_files), 'expectedDirectories': sorted(planned_directories),
              'defaultMovieYear': 2000, 'automaticRetryAllowed': False}), private=True)
    for path in (FFMPEG, FFPROBE):
        version = run([path, '-version'], path.name + '-version').decode().splitlines()[0]
        require(version.startswith(path.name + ' version 9.0.1 '), 'The fixed toolchain version differs.')
        tools[path.name + 'Version'] = version
    require(old_snapshot() == before, 'The original fixture changed after intent publication.')
    missing(ROOT)
    new_directory(ROOT)
    write_new(ROOT / '.goby-managed', (MARKER + '\n').encode())
    for kind in ('Movies', 'TV', 'Music'):
        new_directory(ROOT / kind)
    for item in movies:
        new_directory(ROOT / 'Movies' / item['key'])
    first_video = ROOT / 'Movies/Seed/movie.mp4'
    run([FFMPEG, '-nostdin', '-hide_banner', '-loglevel', 'error', '-n', '-protocol_whitelist', 'file,lavfi',
         '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=30', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000',
         '-t', str(SECONDS), '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '28', '-threads:v', '1',
         '-pix_fmt', 'yuv420p', '-g', '60', '-c:a', 'aac', '-b:a', '96k', '-threads:a', '1',
         '-ac', '2', '-ar', '48000', '-movflags', '+faststart', first_video], 'video-generate')
    generated_file(first_video)
    video_profile = probe(first_video, 'video-probe', 'aac', video=True)
    profiles, expected = {}, []
    for item in movies:
        directory = ROOT / 'Movies' / item['key']
        video_path = directory / 'movie.mp4'
        if video_path != first_video:
            copy_new(first_video, video_path)
        write_new(directory / 'movie.nfo', xml_bytes('movie', {'title': item['title'], 'year': item['year'],
                  'genre': item['genres'], 'tag': item['tags'], 'studio': item['studios'],
                  'actor': item['actors'], 'director': item['directors']}))
        relative = video_path.relative_to(ROOT).as_posix()
        profiles[relative] = video_profile
        expected.append({'kind': 'Movie', 'media': relative, 'nfo': str(directory.relative_to(ROOT) / 'movie.nfo'), **item})
    theme_mp3 = ROOT / 'Movies/Seed/theme.mp3'
    mp3_profile = generate_audio(theme_mp3, 'libmp3lame', 'theme-mp3')
    profiles[theme_mp3.relative_to(ROOT).as_posix()] = mp3_profile
    theme_music = ROOT / 'Movies/AllMatches/theme-music'
    new_directory(theme_music)
    copy_new(theme_mp3, theme_music / 'one.mp3')
    profiles[(theme_music / 'one.mp3').relative_to(ROOT).as_posix()] = mp3_profile
    profiles[(theme_music / 'two.flac').relative_to(ROOT).as_posix()] = generate_audio(theme_music / 'two.flac', 'flac', 'theme-flac')
    backdrops = ROOT / 'Movies/NoShared/backdrops'
    new_directory(backdrops)
    copy_new(first_video, backdrops / 'video.mp4')
    profiles[(backdrops / 'video.mp4').relative_to(ROOT).as_posix()] = video_profile
    series = ROOT / 'TV/M3e Auxiliary Series'
    new_directory(series)
    write_new(series / 'tvshow.nfo', xml_bytes('tvshow', {'title': 'M3e Auxiliary Series'}))
    copy_new(theme_mp3, series / 'theme.mp3')
    profiles[(series / 'theme.mp3').relative_to(ROOT).as_posix()] = mp3_profile
    for season in (1, 2):
        directory = series / f'Season {season:02d}'
        new_directory(directory)
        write_new(directory / 'season.nfo', xml_bytes('season', {'title': f'Season {season}', 'seasonnumber': season}))
        stem = f'M3e Auxiliary Series S{season:02d}E01'
        copy_new(first_video, directory / (stem + '.mp4'))
        write_new(directory / (stem + '.nfo'), xml_bytes('episodedetails', {'title': f'Auxiliary Episode {season}-1',
                  'season': season, 'episode': 1}))
        relative = (directory / (stem + '.mp4')).relative_to(ROOT).as_posix()
        profiles[relative] = video_profile
        expected.append({'kind': 'Episode', 'media': relative, 'series': 'M3e Auxiliary Series', 'season': season, 'episode': 1})
        if season == 2:
            new_directory(directory / 'theme-music')
            copy_new(theme_mp3, directory / 'theme-music/season.mp3')
            profiles[(directory / 'theme-music/season.mp3').relative_to(ROOT).as_posix()] = mp3_profile
    for album in albums:
        directory = ROOT / 'Music' / album['key']
        new_directory(directory)
        for track in album['tracks']:
            path = directory / f"{track['number']:02d}.mp3"
            tags = {'album': album['title'], 'title': track['title'], 'artist': track['artist'],
                    'album_artist': track['albumArtist'], 'track': str(track['number'])}
            relative = path.relative_to(ROOT).as_posix()
            profiles[relative] = generate_audio(path, 'libmp3lame', f"music-{album['key']}-{track['number']}", tags)
            expected.append({'kind': 'Audio', 'media': relative, 'albumKey': album['key'], 'album': album['title'], **track})
    files, directories, total = {}, set(), 0
    for path in sorted(ROOT.rglob('*')):
        if stat.S_ISDIR(path.lstat().st_mode):
            safe_path(path, directory=True, mode=0o755)
            directories.add(path.relative_to(ROOT).as_posix())
            continue
        info = safe_path(path, mode=0o644)
        total += info.st_size
        require(len(files) < MAX_FILES and total <= MAX_TREE_BYTES, 'The generated fixture exceeds its inventory budget.')
        files[path.relative_to(ROOT).as_posix()] = {'sha256': digest(path), 'bytes': info.st_size,
                                                 'device': info.st_dev, 'inode': info.st_ino, 'links': info.st_nlink}
    require(set(files) == planned_files and directories == planned_directories and len(files) == 43,
            'The generated fixture differs from its exact planned membership.')
    require(set(profiles) == {name for name in files if Path(name).suffix in ('.mp4', '.mp3', '.flac')},
            'A generated media member lacks its measured or byte-identical copied profile.')
    require(old_snapshot() == before, 'The original fixture changed during auxiliary generation.')
    require({str(path): digest(path, mode=0o755) for path in (FFMPEG, FFPROBE)} == tools['sha256'],
            'The fixed media executables changed during generation.')
    manifest = {'marker': MARKER, 'version': 1, 'root': str(ROOT), 'files': files,
                'directories': sorted(directories), 'profiles': profiles,
                'expectedItems': expected, 'movies': movies, 'albums': albums, 'tools': tools,
                'themeLayouts': {'Seed': ['theme.mp3'], 'AllMatches': ['theme-music/one.mp3', 'theme-music/two.flac'],
                                'NoShared': ['backdrops/video.mp4'], 'Series': ['theme.mp3'],
                                'Season02': ['theme-music/season.mp3']},
                'themeLayoutSource': THEME_DOCUMENT, 'defaultMovieYear': 2000,
                'featureBoundary': 'Names identify intended metadata controls, not verified reference ranking. NoShared still has the shared default year.',
                'musicGroupingControl': 'MixedAlbum has track artists ArtistA and ArtistB but album_artist ArtistA on both tracks. Album artist and track artist are separate experimental relationships.',
                'oldManifestSha256': OLD_MANIFEST_SHA, 'oldBytesInodesAndLinksPreserved': True,
                'libraryScanExecuted': False, 'referenceContractVerified': False}
    write_new(ROOT / 'manifest.json', encoded(manifest))
    require(old_snapshot() == before, 'The original fixture changed before completion publication.')
    write_new(CONTROL / 'completed.json', encoded({'marker': MARKER, 'phase': 'completed',
              'manifest': str(ROOT / 'manifest.json'), 'manifestSha256': digest(ROOT / 'manifest.json'),
              'fileCountExcludingManifest': len(files), 'generatedBytesExcludingManifest': total,
              'oldSnapshotSha256': sha(encoded(before)), 'oldBytesInodesAndLinksPreserved': True,
              'libraryScanExecuted': False}), private=True)
    return {'status': 'created', 'manifest': str(ROOT / 'manifest.json'), 'sha256': digest(ROOT / 'manifest.json'),
            'files': len(files), 'movieCount': 12, 'episodeCount': 2, 'albumCount': 4, 'musicTrackCount': 5}


def main():
    try:
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Run this operator only through authorized root SSH on test-env.')
        # Use known creation modes without altering any preexisting permissions.
        os.umask(0o022)
        missing(ROOT)
        missing(CONTROL)
        before = old_snapshot()
        tools = {'sha256': {str(path): digest(path, mode=0o755) for path in (FFMPEG, FFPROBE)}}
        require(tools['sha256'][str(FFMPEG)] == before['ffmpegSha256'], 'FFmpeg differs from the accepted 9.0.1 toolchain.')
        new_directory(CONTROL, private=True)
        # The control directory itself is an exclusive attempt fence. Generate
        # publishes its durable intent before even running version commands.
        print(json.dumps(generate(before, tools)))
        return 0
    except BaseException as error:
        print(json.dumps({'status': 'failed', 'error': str(error) if isinstance(error, Failure) else type(error).__name__,
                          'artifactsRetained': True, 'automaticRetryAllowed': False, 'control': str(CONTROL)}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
