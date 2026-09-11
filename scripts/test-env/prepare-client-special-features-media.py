#!/usr/bin/env python3
"""Create the two explicitly separated synthetic Movie-extras media stages.

Run only through ssh test-env. The mains stage requires both new roots absent.
The extras stage requires the immutable mains receipts and a separately pinned
reference-indexing receipt. Neither stage performs HTTP, login, scan, service,
or database operations. Failed stages retain their exclusive attempt fences.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import subprocess
import sys
import time
import types

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = Path('/opt/goby-fixtures/client-special-features-m3e-v1')
CONTROL = WORK / 'special-features-media-v1'
REFERENCE_CONTROL = WORK / 'reference-special-features-v1'
AUX = Path('/opt/goby-fixtures/client-aux-m3e-v1')
AUX_CONTROL = WORK / 'auxiliary-media-v1'
AUX_MANIFEST_SHA = 'dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7'
AUX_COMPLETED_SHA = 'edd0080ea5856a92279300adebea4894d7c8f2f2be3f7733ccc67c4236d6d789'
HELPER_SHA = '20b807307b785fa01e9356e87d89f4c4783a1cf86c1e8b937bb0ce2e80796a43'
REFERENCE_BINARY_SHA = 'c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2'
MARKER = 'goby-client-special-features-media-m3e-v1'
FFMPEG = Path('/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg')
FFPROBE = FFMPEG.with_name('ffprobe')
HASH = re.compile(r'[0-9a-f]{64}')
ID = re.compile(r'[A-Za-z0-9_-]{1,128}')
MAX_OUTPUT, MAX_MEDIA, MAX_TREE = 2 << 20, 16 << 20, 160 << 20
SECONDS = 12
ENV = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}
POSITIVE = 'M3e Special Features Positive (2026)'
EMPTY = 'M3e Special Features Empty (2026)'
OWNER_DIRECTORY = 'Movies/' + POSITIVE
FREQUENCIES = {'positive': 440, 'empty': 550, 'featurette-zeta': 660, 'featurette-alpha': 770,
               'deleted-middle': 880, 'trailer-delta': 990, 'nested-hidden': 1100}


class Failure(Exception):
    """A bounded media-only guard rejected this stage."""


def require(value, message):
    if not value:
        raise Failure(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def source_identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, ensure_ascii=True, allow_nan=False) + '\n').encode()


def decode(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'A control document repeats a field.')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(Failure('A control number is nonfinite.')))


def load_helper(args):
    for digest in (args.script_sha256, args.auxiliary_generator_sha256):
        require(HASH.fullmatch(digest or ''), 'Both source files require explicit digests.')
    require(args.auxiliary_generator_sha256 == HELPER_SHA and args.auxiliary_generator.is_relative_to(WORK) and
            args.auxiliary_generator.is_absolute() and '..' not in args.auxiliary_generator.parts and
            args.auxiliary_generator.name == 'prepare-client-auxiliary-media.py', 'The accepted auxiliary generator was not selected.')
    path = args.auxiliary_generator
    for member in reversed((path, *path.parents)):
        info = member.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An auxiliary source ancestor is not administratively controlled.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= MAX_OUTPUT,
            'The accepted helper is not a bounded independent source file.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC), 'rb') as handle:
        require(source_identity(os.fstat(handle.fileno())) == source_identity(before), 'The helper changed while opening.')
        raw = handle.read(MAX_OUTPUT + 1)
        require(source_identity(os.fstat(handle.fileno())) == source_identity(before) and
                source_identity(path.lstat()) == source_identity(before) and sha(raw) == HELPER_SHA,
                'The exact accepted helper source changed.')
    helper = types.ModuleType('accepted_auxiliary_media_helpers')
    helper.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), helper.__dict__)
    require(helper.ROOT == AUX and helper.CONTROL == AUX_CONTROL and helper.FFMPEG == FFMPEG and helper.FFPROBE == FFPROBE,
            'The accepted helper has different fixed fixture or tool paths.')
    require(helper.digest(Path(__file__).absolute(), limit=MAX_OUTPUT) == args.script_sha256,
            'The new generator source differs from its explicit pin.')
    if args.phase == 'mains':
        require(args.index_receipt is None and args.index_receipt_sha256 is None,
                'Mains creation cannot consume a later indexing receipt.')
    else:
        require(isinstance(args.index_receipt, Path) and args.index_receipt.is_relative_to(REFERENCE_CONTROL) and
                args.index_receipt.is_absolute() and '..' not in args.index_receipt.parts and
                args.index_receipt.name == 'mains-indexed.json' and HASH.fullmatch(args.index_receipt_sha256 or ''),
                'Extras require the explicitly pinned new reference mains-indexed receipt.')
    return helper


def anchor(helper, path, mode):
    value = helper.identity(helper.safe_path(path, directory=True, mode=mode))
    return {key: value[key] for key in ('device', 'inode', 'uid', 'gid', 'mode')}


def snapshot(helper, root, *, private=False, full_directories=False):
    mode, directory_mode = (0o600, 0o700) if private else (0o644, 0o755)
    root_info = helper.safe_path(root, directory=True, mode=directory_mode)
    files, directories, total = {}, {}, 0
    for path in sorted(root.rglob('*')):
        require(len(files) + len(directories) < 160, 'A fixture or control tree exceeded its bounded membership.')
        relative = path.relative_to(root).as_posix()
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            directories[relative] = (helper.identity(helper.safe_path(path, directory=True, mode=directory_mode))
                                     if full_directories else anchor(helper, path, directory_mode))
        else:
            info = helper.safe_path(path, mode=mode)
            total += info.st_size
            require(total <= MAX_TREE, 'A retained fixture exceeds the complete byte budget.')
            files[relative] = {'sha256': helper.digest(path, mode=mode), 'bytes': info.st_size, 'identity': helper.identity(info)}
    return {'root_identity': helper.identity(root_info) if full_directories else anchor(helper, root, directory_mode),
            'directories': directories, 'files': files}


def protected_media(helper):
    original = helper.old_snapshot()
    raw = helper.read_small(AUX / 'manifest.json', 0o644)
    require(sha(raw) == AUX_MANIFEST_SHA, 'The accepted auxiliary manifest changed.')
    manifest = decode(raw)
    complete_raw = helper.read_small(AUX_CONTROL / 'completed.json')
    require(sha(complete_raw) == AUX_COMPLETED_SHA, 'The accepted auxiliary completion receipt changed.')
    complete = decode(complete_raw)
    require(manifest.get('marker') == 'goby-client-auxiliary-media-m3e-v1' and manifest.get('root') == str(AUX) and
            len(manifest.get('files', {})) == 43 and complete.get('manifestSha256') == AUX_MANIFEST_SHA,
            'The accepted auxiliary fixture owner or member count differs.')
    current = snapshot(helper, AUX, full_directories=True)
    expected = manifest['files']
    require(set(current['files']) == set(expected) | {'manifest.json'} and set(current['directories']) == set(manifest['directories']),
            'The accepted auxiliary fixture membership changed.')
    for name, value in expected.items():
        actual = current['files'][name]
        require(actual['sha256'] == value['sha256'] and actual['bytes'] == value['bytes'] and
                all(actual['identity'][key] == value[key] for key in ('device', 'inode', 'links')),
                'An accepted auxiliary file changed bytes, inode, or links.')
    require(helper.read_small(AUX / '.goby-managed', 0o644) == b'goby-client-auxiliary-media-m3e-v1\n',
            'The existing auxiliary media marker changed.')
    tools = manifest['tools']['sha256']
    require(set(tools) == {str(FFMPEG), str(FFPROBE)} and tools[str(FFMPEG)] == original['ffmpegSha256'] and
            all(helper.digest(path, mode=0o755) == tools[str(path)] for path in (FFMPEG, FFPROBE)),
            'The accepted FFmpeg/ffprobe binary pair changed.')
    return {'original': original, 'auxiliary': current, 'auxiliary_control': snapshot(helper, AUX_CONTROL, private=True, full_directories=True)}, tools


def mkdir(helper, path, *, private=False):
    require(path == ROOT or path.is_relative_to(ROOT) or path == CONTROL or path.is_relative_to(CONTROL),
            'Creation escaped the two new owned roots.')
    helper.safe_path(path.parent, directory=True)
    path.mkdir(mode=0o700 if private else 0o755)
    helper.safe_path(path, directory=True, mode=0o700 if private else 0o755)
    helper.sync_directory(path.parent)


def write_new(helper, path, raw, *, private=False):
    require(path.is_relative_to(ROOT) or path.is_relative_to(CONTROL), 'A file write escaped the new owned roots.')
    helper.safe_path(path.parent, directory=True)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                         0o600 if private else 0o644)
    with os.fdopen(descriptor, 'wb') as target:
        require(target.write(raw) == len(raw), 'A new file was only partially written.')
        target.flush()
        os.fsync(target.fileno())
    helper.sync_directory(path.parent)


def run(helper, directory, arguments, label):
    require(arguments[0] in (FFMPEG, FFPROBE) and re.fullmatch(r'[a-z0-9-]{1,60}', label),
            'A media command selected an unapproved executable or label.')
    helper.safe_path(directory, directory=True, mode=0o700)
    descriptor = os.open(directory / (label + '.log'), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    output = bytearray()
    with os.fdopen(descriptor, 'wb') as log:
        process = subprocess.Popen([str(value) for value in arguments], stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=ENV, start_new_session=True, preexec_fn=helper.command_limits)
        try:
            deadline = time.monotonic() + 150
            with selectors.DefaultSelector() as ready:
                ready.register(process.stdout, selectors.EVENT_READ)
                while True:
                    require(time.monotonic() < deadline, 'A media command exceeded its time bound.')
                    if not ready.select(0.25):
                        continue
                    block = os.read(process.stdout.fileno(), 65536)
                    if not block:
                        break
                    require(len(output) + len(block) <= MAX_OUTPUT, 'A media command exceeded its output bound.')
                    output.extend(block)
                    log.write(block)
            require(process.wait(timeout=max(1, deadline - time.monotonic())) == 0, 'A media command failed; partial output is retained.')
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
            process.stdout.close()
            log.flush()
            os.fsync(log.fileno())
    helper.sync_directory(directory)
    return bytes(output)


def main_items():
    return [{'fixture_id': key, 'intended_type': 'Movie', 'title': title.removesuffix(' (2026)'), 'year': 2026,
             'media': f'Movies/{title}/{title}.mp4', 'nfo': f'Movies/{title}/movie.nfo'}
            for key, title in (('positive', POSITIVE), ('empty', EMPTY))]


def extra_items():
    return [{'fixture_id': key, 'owner_fixture_id': 'positive', 'intended_role': role, 'category': category,
             'media': OWNER_DIRECTORY + '/' + relative}
            for key, role, category, relative in (
                ('featurette-zeta', 'extra_candidate', 'featurettes', 'featurettes/Zeta Bonus.mp4'),
                ('featurette-alpha', 'extra_candidate', 'featurettes', 'featurettes/Alpha Bonus.mp4'),
                ('deleted-middle', 'extra_candidate', 'deleted scenes', 'deleted scenes/Middle Deleted Scene.mp4'),
                ('trailer-delta', 'local_trailer_candidate', 'trailers', 'trailers/Delta Local Trailer.mp4'),
                ('nested-hidden', 'nested_video_control', 'featurettes', 'featurettes/nested/Hidden Nested.mp4'))]


def generate_video(helper, directory, item):
    path = ROOT / item['media']
    helper.missing(path)
    started = dt.datetime.now(dt.timezone.utc).isoformat()
    run(helper, directory, [FFMPEG, '-nostdin', '-hide_banner', '-loglevel', 'error', '-n', '-protocol_whitelist', 'file,lavfi',
        '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=30', '-f', 'lavfi', '-i', f"sine=frequency={FREQUENCIES[item['fixture_id']]}:sample_rate=48000",
        '-t', str(SECONDS), '-map_metadata', '-1', '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '28', '-threads:v', '1',
        '-pix_fmt', 'yuv420p', '-g', '60', '-c:a', 'aac', '-b:a', '96k', '-threads:a', '1', '-ac', '2', '-ar', '48000',
        '-movflags', '+faststart', path], item['fixture_id'] + '-generate')
    info = helper.safe_path(path, mode=0o644)
    require(0 < info.st_size <= MAX_MEDIA, 'A new media file exceeds its bounded profile.')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    helper.sync_directory(path.parent)
    raw = run(helper, directory, [FFPROBE, '-v', 'error', '-protocol_whitelist', 'file', '-show_format', '-show_streams', '-of', 'json', path],
              item['fixture_id'] + '-probe')
    measured = decode(raw)
    streams = measured.get('streams', [])
    video = [value for value in streams if value.get('codec_type') == 'video']
    audio = [value for value in streams if value.get('codec_type') == 'audio']
    require(len(streams) == 2 and len(video) == len(audio) == 1 and video[0].get('codec_name') == 'h264' and
            video[0].get('width') == 320 and video[0].get('height') == 180 and video[0].get('avg_frame_rate') == '30/1' and
            audio[0].get('codec_name') == 'aac' and audio[0].get('channels') == 2 and audio[0].get('sample_rate') == '48000' and
            11.9 <= float(measured['format']['duration']) <= 12.2 and
            all('title' not in {key.lower() for key in row.get('tags', {})} for row in [measured['format'], *streams]),
            'A clip differs from the video/audio profile or contains an embedded title.')
    return {'fixture_id': item['fixture_id'], 'started_at': started, 'completed_at': dt.datetime.now(dt.timezone.utc).isoformat(),
            'profile': {'duration_seconds': float(measured['format']['duration']), 'video_codec': 'h264', 'audio_codec': 'aac',
                        'width': 320, 'height': 180, 'fps': '30/1', 'channels': 2, 'sample_rate': 48000,
                        'embedded_title_absent': True, 'probe_sha256': sha(raw)}}


def read_mains(helper, args):
    control_identity = anchor(helper, CONTROL, 0o700)
    owner = decode(helper.read_small(CONTROL / 'OWNER.json'))
    require(owner == {'marker': MARKER, 'version': 1, 'root': str(ROOT), 'control': str(CONTROL),
                     'generator_sha256': args.script_sha256, 'auxiliary_generator_sha256': HELPER_SHA,
                     'control_identity': control_identity},
            'The existing mains control has another owner or source version.')
    manifest_raw = helper.read_small(ROOT / 'mains-manifest.json', 0o644)
    completed_raw = helper.read_small(CONTROL / 'mains/completed.json')
    manifest, completed = decode(manifest_raw), decode(completed_raw)
    require(manifest.get('marker') == completed.get('marker') == MARKER and manifest.get('phase') == completed.get('phase') == 'mains' and
            manifest.get('version') == completed.get('version') == 1 and manifest.get('root') == completed.get('root') == str(ROOT) and
            manifest.get('control') == completed.get('control') == str(CONTROL) and
            completed.get('status') == 'completed' and manifest.get('generator_sha256') == completed.get('generator_sha256') == args.script_sha256 and
            manifest.get('items') == main_items() and completed.get('manifest', {}).get('sha256') == sha(manifest_raw) and
            completed['manifest'].get('path') == str(ROOT / 'mains-manifest.json') and
            completed.get('protected_media_preserved') is True and completed.get('index_receipt_sha256') is None,
            'Mains have no complete, matching first-stage receipt.')
    current = snapshot(helper, ROOT)
    require(current == completed.get('media_snapshot') and current['root_identity'] == manifest.get('root_identity') and
            current['directories'] == manifest.get('directories') and
            {key: value for key, value in current['files'].items() if key != 'mains-manifest.json'} == manifest.get('files'),
            'A main file, manifest, directory identity, or first-stage membership changed.')
    require(completed['manifest'].get('identity') == current['files']['mains-manifest.json']['identity'] and
            completed.get('protected_before_sha256') == completed.get('protected_after_sha256') and
            HASH.fullmatch(completed.get('protected_after_sha256', '')), 'The mains completion receipt lost its preservation bindings.')
    index_raw = helper.read_small(args.index_receipt)
    require(sha(index_raw) == args.index_receipt_sha256, 'The explicit reference-indexing receipt changed.')
    index = decode(index_raw)
    require(index.get('schema') == 'goby-client-special-features-mains-indexed' and type(index.get('version')) is int and index['version'] == 1 and
            index.get('status') == 'indexed' and index.get('media_root') == str(ROOT) and index.get('media_root_identity') == current['root_identity'] and
            index.get('generator_sha256') == args.script_sha256 and index.get('mains_manifest_sha256') == sha(manifest_raw) and
            index.get('mains_completed_sha256') == sha(completed_raw) and index.get('no_extras') is True,
            'The reference has not explicitly acknowledged these exact mains before extras.')
    reference = index.get('reference', {})
    require(reference.get('service') == 'goby-emby-client-m3e.service' and reference.get('binary_sha256') == REFERENCE_BINARY_SHA and
            reference.get('library_path') == str(ROOT / 'Movies') and ID.fullmatch(reference.get('server_id', '')) and
            ID.fullmatch(reference.get('library_id', '')), 'The indexing receipt belongs to another reference or library path.')
    indexed = index.get('items', {})
    require(set(indexed) == {'positive', 'empty'}, 'The indexing receipt does not cover both main controls.')
    for item in main_items():
        actual = indexed[item['fixture_id']]
        require(set(actual) == {'Id', 'Type', 'Path'} and ID.fullmatch(actual['Id']) and actual['Type'] == 'Movie' and
                actual['Path'] == str(ROOT / item['media']), 'A main was not indexed as its exact owned Movie path.')
    require(len({reference['library_id'], indexed['positive']['Id'], indexed['empty']['Id']}) == 3,
            'Reference library and main identities are not distinct.')
    evidence = index.get('evidence', {})
    path = Path(evidence.get('path', ''))
    require(set(evidence) == {'path', 'sha256'} and path.is_absolute() and path.is_relative_to(REFERENCE_CONTROL) and
            '..' not in path.parts and path != args.index_receipt and path.suffix == '.json' and HASH.fullmatch(evidence['sha256']) and
            sha(helper.read_small(path)) == evidence['sha256'], 'The indexing receipt lacks its separate pinned observation.')
    return {'manifest': manifest, 'manifest_sha256': sha(manifest_raw), 'completed_sha256': sha(completed_raw),
            'protected_sha256': completed['protected_after_sha256'],
            'control_identity': control_identity,
            'media_snapshot': current, 'control_snapshot': snapshot(helper, CONTROL / 'mains', private=True, full_directories=True),
            'owner_bytes': helper.read_small(CONTROL / 'OWNER.json'), 'index': index}


def preserve_mains(helper, mains):
    current = snapshot(helper, ROOT)
    expected = mains['media_snapshot']
    require(current['root_identity'] == expected['root_identity'] and
            all(current['files'].get(name) == value for name, value in expected['files'].items()) and
            all(current['directories'].get(name) == value for name, value in expected['directories'].items()) and
            snapshot(helper, CONTROL / 'mains', private=True, full_directories=True) == mains['control_snapshot'] and
            anchor(helper, CONTROL, 0o700) == mains['control_identity'] and
            helper.read_small(CONTROL / 'OWNER.json') == mains['owner_bytes'],
            'Extras modified a main file, the first manifest, or the immutable first-stage control records.')


def execute(helper, args, before, tools, mains, phase_directory):
    items = main_items() if args.phase == 'mains' else extra_items()
    planned = [item['media'] for item in items]
    write_new(helper, phase_directory / 'intent.json', encoded({'marker': MARKER, 'version': 1, 'phase': args.phase,
        'root': str(ROOT), 'generator_sha256': args.script_sha256, 'auxiliary_generator_sha256': HELPER_SHA,
        'protected_before': before, 'tool_sha256': tools, 'creation_order': planned,
        'index_receipt_sha256': args.index_receipt_sha256, 'automatic_retry': False}), private=True)
    versions = {}
    for path in (FFMPEG, FFPROBE):
        first = run(helper, phase_directory, [path, '-version'], path.name + '-version').decode().splitlines()[0]
        require(first.startswith(path.name + ' version 9.0.1 '), 'The pinned media tool version differs.')
        versions[path.name] = first
    require(protected_media(helper)[0] == before, 'Protected media changed after the durable intent.')
    if args.phase == 'mains':
        helper.missing(ROOT)
        mkdir(helper, ROOT)
        write_new(helper, ROOT / '.goby-managed', (MARKER + '\n').encode())
        mkdir(helper, ROOT / 'Movies')
        for item in items:
            mkdir(helper, (ROOT / item['media']).parent)
    else:
        preserve_mains(helper, mains)
        for relative in ('featurettes', 'deleted scenes', 'trailers', 'featurettes/nested'):
            mkdir(helper, ROOT / OWNER_DIRECTORY / relative)
    creation, profiles = [], {} if mains is None else dict(mains['manifest']['profiles'])
    for item in items:
        measured = generate_video(helper, phase_directory, item)
        creation.append({'fixture_id': item['fixture_id'], 'media': item['media'],
                         'started_at': measured['started_at'], 'completed_at': measured['completed_at']})
        profiles[item['media']] = measured['profile']
        if args.phase == 'mains':
            write_new(helper, ROOT / item['nfo'], helper.xml_bytes('movie', {'title': item['title'], 'year': item['year']}))
    if args.phase == 'extras':
        write_new(helper, ROOT / OWNER_DIRECTORY / 'featurettes/notes.txt', b'Synthetic nonvideo exclusion control.\n')
        preserve_mains(helper, mains)
    current = snapshot(helper, ROOT)
    expected_files = {'.goby-managed', *(value[key] for value in main_items() for key in ('media', 'nfo'))}
    expected_directories = {'Movies', 'Movies/' + POSITIVE, 'Movies/' + EMPTY}
    if args.phase == 'extras':
        expected_files |= {'mains-manifest.json', *(value['media'] for value in extra_items()), OWNER_DIRECTORY + '/featurettes/notes.txt'}
        expected_directories |= {OWNER_DIRECTORY + '/' + name for name in ('featurettes', 'featurettes/nested', 'deleted scenes', 'trailers')}
    require(set(current['files']) == expected_files and set(current['directories']) == expected_directories and
            set(profiles) == {name for name in current['files'] if name.endswith('.mp4')},
            'The new fixture differs from its complete fixed-stage membership.')
    old_inodes = {(row['identity']['device'], row['identity']['inode']) for old in (before['original'], before['auxiliary']) for row in old['files'].values()}
    new_inodes = [(row['identity']['device'], row['identity']['inode']) for row in current['files'].values()]
    require(len(new_inodes) == len(set(new_inodes)) and not old_inodes.intersection(new_inodes),
            'A new file aliases another new file or any protected original file.')
    after, after_tools = protected_media(helper)
    require(after == before and after_tools == tools, 'Original media or the accepted tool pair changed during generation.')
    manifest = {'marker': MARKER, 'version': 1, 'phase': args.phase, 'root': str(ROOT), 'control': str(CONTROL),
        'generator_sha256': args.script_sha256, 'auxiliary_generator_sha256': HELPER_SHA,
        **current, 'profiles': profiles, 'items': main_items(), 'extras': [] if mains is None else extra_items(),
        'creation_order': creation, 'tools': {'sha256': tools, 'versions': versions},
        'index_receipt_path': str(args.index_receipt) if mains else None, 'index_receipt_sha256': args.index_receipt_sha256,
        'mains_manifest_sha256': mains['manifest_sha256'] if mains else None,
        'mains_completed_sha256': mains['completed_sha256'] if mains else None,
        'reference_main_ids': {key: value['Id'] for key, value in mains['index']['items'].items()} if mains else None,
        'reference_contract_verified': False, 'library_scan_executed': False, 'http_requests': 0,
        'scope': 'Two main Movies, then featurettes, deleted scenes, trailers, one nested video and one nonvideo control. Other categories and flat trailers are excluded.'}
    manifest_path = ROOT / (args.phase + '-manifest.json')
    manifest_raw = encoded(manifest)
    write_new(helper, manifest_path, manifest_raw)
    final_snapshot = snapshot(helper, ROOT)
    manifest_fact = final_snapshot['files'].get(manifest_path.name, {})
    require(final_snapshot['root_identity'] == current['root_identity'] and final_snapshot['directories'] == current['directories'] and
            {name: value for name, value in final_snapshot['files'].items() if name != manifest_path.name} == current['files'] and
            manifest_fact.get('sha256') == sha(manifest_raw) and manifest_fact.get('bytes') == len(manifest_raw),
            'The final fixture does not exactly match the published phase manifest.')
    if mains is not None:
        preserve_mains(helper, mains)
        require(sha(helper.read_small(args.index_receipt)) == args.index_receipt_sha256, 'The indexing receipt changed during extras generation.')
    require(protected_media(helper)[0] == before, 'Protected media changed before completion publication.')
    receipt = {'marker': MARKER, 'version': 1, 'phase': args.phase, 'status': 'completed', 'root': str(ROOT), 'control': str(CONTROL),
        'generator_sha256': args.script_sha256, 'manifest': {'path': str(manifest_path),
            'sha256': manifest_fact['sha256'], 'identity': manifest_fact['identity']},
        'media_snapshot': final_snapshot, 'protected_before_sha256': sha(encoded(before)), 'protected_after_sha256': sha(encoded(after)),
        'protected_media_preserved': True, 'library_scan_executed': False, 'http_requests': 0,
        'index_receipt_path': str(args.index_receipt) if mains else None, 'index_receipt_sha256': args.index_receipt_sha256,
        'mains_manifest_sha256': mains['manifest_sha256'] if mains else None,
        'mains_completed_sha256': mains['completed_sha256'] if mains else None}
    write_new(helper, phase_directory / 'completed.json', encoded(receipt), private=True)
    return {'status': 'completed', 'phase': args.phase, 'manifest': str(manifest_path), 'manifest_sha256': receipt['manifest']['sha256'],
            'receipt': str(phase_directory / 'completed.json'), 'receipt_sha256': helper.digest(phase_directory / 'completed.json', mode=0o600),
            'file_count': len(final_snapshot['files']), 'library_scan_executed': False, 'http_requests': 0}


def parser():
    value = argparse.ArgumentParser(description=__doc__)
    value.add_argument('--phase', choices=('mains', 'extras'), required=True)
    value.add_argument('--script-sha256', required=True)
    value.add_argument('--auxiliary-generator', type=Path, required=True)
    value.add_argument('--auxiliary-generator-sha256', required=True)
    value.add_argument('--index-receipt', type=Path)
    value.add_argument('--index-receipt-sha256')
    return value


def main(argv=None):
    args, helper, phase_owned = parser().parse_args(argv), None, False
    try:
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and os.environ.get('SSH_CONNECTION'),
                'Run only through authorized root SSH on test-env.')
        os.umask(0o022)
        helper = load_helper(args)
        before, tools = protected_media(helper)
        if args.phase == 'mains':
            helper.missing(ROOT)
            helper.missing(CONTROL)
            mains = None
            mkdir(helper, CONTROL, private=True)
            write_new(helper, CONTROL / 'OWNER.json', encoded({'marker': MARKER, 'version': 1, 'root': str(ROOT), 'control': str(CONTROL),
                'generator_sha256': args.script_sha256, 'auxiliary_generator_sha256': HELPER_SHA,
                'control_identity': anchor(helper, CONTROL, 0o700)}), private=True)
        else:
            mains = read_mains(helper, args)
            require(mains['protected_sha256'] == sha(encoded(before)), 'The protected old media changed between the two stages.')
        phase_directory = CONTROL / args.phase
        helper.missing(phase_directory)
        mkdir(helper, phase_directory, private=True)
        phase_owned = True
        print(json.dumps(execute(helper, args, before, tools, mains, phase_directory)))
        return 0
    except BaseException as error:
        known = isinstance(error, Failure) or helper is not None and isinstance(error, helper.Failure)
        result = {'status': 'failed', 'phase': args.phase, 'reason': str(error) if known else type(error).__name__,
                  'artifacts_retained': True, 'automatic_retry': False, 'library_scan_executed': False, 'http_requests': 0}
        if phase_owned:
            try:
                write_new(helper, CONTROL / args.phase / 'failed.json', encoded(result), private=True)
            except Exception:
                pass
        print(json.dumps(result))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
