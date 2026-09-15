"""Copy and inventory the fixed 1,000-leaf native capacity corpus.

Import definitions only. The caller owns the deployment lock, deadlines,
protected-state checks, evidence persistence, and the already-created fixture
parent. This module starts no subprocess, generates no template, changes no
mount, and never replaces or removes an existing path.

prepare_corpus(base, template_input) creates F/media exactly once. The base
provides metadata(path) and read_regular(path, byte_limit). template_input must
have kind/version/scope/files/ancestors/provenance. files has video and audio
descriptors with path/sha256/bytes/metadata at the two fixed fresh template paths.
ancestors maps every absolute parent through '/' to its full metadata. provenance
pins E/private/template-preparation.json with path/sha256/bytes. That new receipt
must bind the exported recipe and files, with status generated, source
fresh-fixed-recipe, and generationCount 2. Other execution evidence may accompany
those required receipt fields. The caller reviews actual generator identities,
tool pins, and command completion; a matching receipt alone is not a media probe.

snapshot_corpus(base, manifest) rereads the exact files and source descriptors.
It requires unchanged full corpus metadata/hashes and stable ancestor ownership,
mode and identity. Ancestor directory timestamps and sizes can legitimately
change as the caller adds unrelated owned evidence beside the fixed paths.
All returned records are private evidence. Persist them outside the media root.
"""

import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
ROOT = F / 'media'
TEMPLATE_ROOT = E / 'private/templates'
TEMPLATE_RECEIPT = E / 'private/template-preparation.json'
LABELS = ('A', 'B')
ARTIST = 'Shared Capacity Artist'
LOGICAL_LIMIT = 16 << 20
ALLOCATED_LIMIT = 32 << 20
FILE_MODE = 0o444
DIRECTORY_MODE = 0o555
META_FIELDS = {'device', 'inode', 'uid', 'gid', 'mode', 'type', 'bytes',
               'links', 'mtimeNs', 'ctimeNs'}
STABLE_FIELDS = ('device', 'inode', 'uid', 'gid', 'mode', 'type')
COUNTS_PER_LIBRARY = {'CollectionFolder': 1, 'Folder': 15, 'MusicAlbum': 10,
                      'Series': 2, 'Season': 10, 'Movie': 200, 'Episode': 200,
                      'Audio': 100}


class CorpusError(RuntimeError):
    """A fixed safe error code without private output."""


def _need(value, code):
    if not value:
        raise CorpusError(code)


def _actor():
    _need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
          sys.flags.isolated and sys.flags.dont_write_bytecode,
          'corpus_remote_root_isolated_python_required')


def template_recipe():
    """Return the existing two generation recipes as data, without executing."""
    common = ['-hide_banner', '-loglevel', 'error', '-nostdin', '-nostats', '-n',
              '-filter_threads', '1']
    encodings = {
        'video': ['-f', 'lavfi', '-i', 'color=c=black:s=32x32:r=5', '-t', '0.4',
                  '-an', '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt',
                  'yuv420p', '-threads', '1', '-movflags', '+faststart'],
        'audio': ['-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000',
                  '-t', '0.25', '-ac', '1', '-c:a', 'flac', '-threads', '1',
                  '-metadata', 'artist=' + ARTIST, '-metadata',
                  'album_artist=' + ARTIST, '-metadata', 'title=',
                  '-metadata', 'album='],
    }
    return {
        'source': 'internal/library/catalog_scan_capacity_integration_test.go',
        'generator': 'internal/library/catalog_rescan_acl_integration_test.go:catalogRescanGenerateMedia',
        'environment': {'PATH': '/usr/bin:/bin', 'LANG': 'C', 'LC_ALL': 'C',
                        'TZ': 'UTC'},
        'generatorCommandSeconds': 5,
        'generatorWaitDelaySeconds': 2,
        'outputLimitIsEmergencyOnly': True,
        'templates': {
            kind: {'path': str(TEMPLATE_ROOT / ('video.mp4' if kind == 'video' else 'audio.flac')),
                   'arguments': common + encoding + ['-fs', '1048576',
                       str(TEMPLATE_ROOT / ('video.mp4' if kind == 'video' else 'audio.flac'))],
                   'acceptedBytesMaximum': 8 << 10 if kind == 'video' else 16 << 10}
            for kind, encoding in encodings.items()
        },
        'factsBoundary': 'Generation parameters; no fresh probe or current template availability is claimed.',
    }


def expected_layout():
    """Return physical paths and expected associations; catalog IDs stay unknown."""
    files = {}
    directories = {'.'}
    for label in LABELS:
        for movie in range(1, 201):
            relative = f'Movies/{label} Movie{movie:04d}.mp4'
            files[label + '/' + relative] = {
                'library': label, 'kind': 'Movie', 'relativePath': relative,
                'template': 'video', 'name': f'{label} Movie{movie:04d}',
                'parent': {'kind': 'Folder', 'relativePath': 'Movies'},
                'indexNumber': 0, 'parentIndexNumber': 0,
            }
        for series in range(1, 3):
            series_name = f'{label} Show{series:02d}'
            for season in range(1, 6):
                for episode in range(1, 21):
                    basename = f'{series_name}.S{season:02d}E{episode:02d}'
                    relative = f'Television/{series_name}/Season{season:02d}/{basename}.mp4'
                    files[label + '/' + relative] = {
                        'library': label, 'kind': 'Episode', 'relativePath': relative,
                        'template': 'video', 'name': basename.replace('.', ' '),
                        'indexNumber': episode, 'parentIndexNumber': season,
                        'series': {'kind': 'Series', 'name': series_name,
                                   'relativePath': '//series/' + series_name.lower(),
                                   'parent': 'library', 'physicalPath': ''},
                        'season': {'kind': 'Season', 'name': f'Season {season}',
                                   'indexNumber': season, 'parent': 'series',
                                   'relativePathPattern': '//season/{seriesId}/' + str(season),
                                   'physicalPath': ''},
                        'parent': 'season',
                    }
        for album in range(1, 11):
            name = f'{label} Album{album:03d}'
            album_relative = 'Music/' + name
            association = {'kind': 'MusicAlbum', 'name': name,
                           'relativePath': album_relative,
                           'parent': {'kind': 'Folder', 'relativePath': 'Music'}}
            files[label + '/' + album_relative + '/album.nfo'] = {
                'library': label, 'kind': 'AlbumNfo',
                'relativePath': album_relative + '/album.nfo',
                'template': None, 'catalogLeaf': False, 'album': association,
                'content': '<album><title>' + name + '</title></album>',
            }
            for track in range(1, 11):
                title = f'{name} Track{track:02d}'
                relative = f'{album_relative}/{track:02d} {title}.flac'
                files[label + '/' + relative] = {
                    'library': label, 'kind': 'Audio', 'relativePath': relative,
                    'template': 'audio', 'name': title, 'indexNumber': track,
                    'parentIndexNumber': 0, 'parent': 'album', 'album': association,
                    'embeddedTitle': '', 'embeddedAlbum': '', 'artist': ARTIST,
                    'albumArtist': ARTIST,
                }
    for relative in files:
        directories.update(parent.as_posix() for parent in Path(relative).parents)
        if files[relative]['template'] is not None:
            files[relative]['catalogLeaf'] = True
            files[relative]['isFolder'] = False
            files[relative]['sortName'] = files[relative]['name'].lower()
    return {
        'kind': 'native-scan-http-capacity-layout', 'version': 1,
        'root': str(ROOT), 'libraries': {
            label: {'name': 'Capacity ' + label, 'collectionType': 'mixed',
                    'path': str(ROOT / label), 'mediaLeaves': 500,
                    'albumNfoFiles': 10, 'catalogCounts': dict(COUNTS_PER_LIBRARY)}
            for label in LABELS},
        'files': files,
        'directories': sorted(directories, key=lambda name: (len(Path(name).parts), name)),
        'mediaLeaves': 1000, 'regularFiles': 1020, 'albumNfoFiles': 20,
        'catalogItems': 1076,
        'hierarchyBoundary': 'Mixed-library physical show and SeasonNN directories are Folder items; episode parents are separate synthetic Season items.',
    }


def _metadata(base, path):
    row = base.metadata(path)
    _need(set(row) == META_FIELDS and all(type(value) is int for value in row.values()) and
          row['device'] > 0 and row['inode'] > 0 and row['bytes'] >= 0 and row['links'] > 0,
          'corpus_metadata_contract')
    return row


def _stable(actual, expected):
    return all(actual[key] == expected[key] for key in STABLE_FIELDS)


def _ancestors(base, parent, expected=None):
    paths = [parent, *parent.parents]
    _need(parent.is_absolute() and '..' not in parent.parts and
          (expected is None or set(expected) == {str(path) for path in paths}),
          'corpus_ancestor_scope')
    records = {}
    for path in reversed(paths):
        row = _metadata(base, path)
        _need(path.resolve() == path and row['type'] == stat.S_IFDIR and
              row['uid'] == row['gid'] == 0 and not row['mode'] & 0o022,
              'corpus_ancestor_unsafe')
        if expected is not None:
            _need(isinstance(expected[str(path)], dict) and set(expected[str(path)]) == META_FIELDS and
                  all(type(value) is int for value in expected[str(path)].values()) and
                  _stable(row, expected[str(path)]), 'corpus_ancestor_replaced')
        records[str(path)] = row
    return records


def _pin(base, descriptor, path, limit, mode):
    _need(isinstance(descriptor, dict) and set(descriptor) == {'path', 'sha256', 'bytes', 'metadata'} and
          descriptor['path'] == str(path) and isinstance(descriptor['sha256'], str) and
          re.fullmatch('[0-9a-f]{64}', descriptor['sha256']) and
          type(descriptor['bytes']) is int and 0 < descriptor['bytes'] <= limit,
          'corpus_source_descriptor')
    before = _metadata(base, path)
    _need(isinstance(descriptor['metadata'], dict) and set(descriptor['metadata']) == META_FIELDS and
          all(type(value) is int for value in descriptor['metadata'].values()) and
          path.resolve() == path and before == descriptor['metadata'] and
          before['uid'] == before['gid'] == 0 and before['type'] == stat.S_IFREG and
          before['links'] == 1 and before['mode'] == mode and before['bytes'] == descriptor['bytes'],
          'corpus_source_identity')
    raw = base.read_regular(path, limit)
    _need(len(raw) == descriptor['bytes'] and hashlib.sha256(raw).hexdigest() == descriptor['sha256'] and
          _metadata(base, path) == before, 'corpus_source_changed')
    return raw


def _templates(base, supplied):
    _need(isinstance(supplied, dict) and set(supplied) ==
          {'kind', 'version', 'scope', 'files', 'ancestors', 'provenance'} and
          supplied['kind'] == 'native-scan-http-capacity-templates' and
          type(supplied['version']) is int and supplied['version'] == 1 and
          supplied['scope'] == str(E) and isinstance(supplied['files'], dict) and
          set(supplied['files']) == {'video', 'audio'} and isinstance(supplied['ancestors'], dict),
          'corpus_template_input_contract')
    ancestors = _ancestors(base, TEMPLATE_ROOT, supplied['ancestors'])
    _need(all(ancestors[str(path)]['mode'] == 0o700 for path in (E, E / 'private', TEMPLATE_ROOT)) and
          {path.name for path in TEMPLATE_ROOT.iterdir()} == {'video.mp4', 'audio.flac'},
          'corpus_private_template_scope')
    pin = supplied['provenance']
    _need(isinstance(pin, dict) and set(pin) == {'path', 'sha256', 'bytes'} and
          pin['path'] == str(TEMPLATE_RECEIPT) and isinstance(pin['sha256'], str) and
          re.fullmatch('[0-9a-f]{64}', pin['sha256']) and type(pin['bytes']) is int and
          0 < pin['bytes'] <= 2 << 20, 'corpus_template_provenance_pin')
    metadata = _metadata(base, TEMPLATE_RECEIPT)
    raw = _pin(base, dict(pin, metadata=metadata), TEMPLATE_RECEIPT, 2 << 20, 0o600)
    receipt = json.loads(raw)
    _need(isinstance(receipt, dict) and receipt.get('kind') == 'native-scan-http-capacity-template-preparation' and
          type(receipt.get('version')) is int and receipt['version'] == 1 and
          receipt.get('status') == 'generated' and receipt.get('scope') == str(E) and
          receipt.get('source') == 'fresh-fixed-recipe' and
          type(receipt.get('generationCount')) is int and receipt['generationCount'] == 2 and
          receipt.get('recipe') == template_recipe() and receipt.get('files') == supplied['files'],
          'corpus_template_provenance_contract')
    templates = {}
    for kind, row in template_recipe()['templates'].items():
        templates[kind] = _pin(base, supplied['files'][kind], Path(row['path']),
                               row['acceptedBytesMaximum'], 0o600)
    source_ids = {(pin['metadata']['device'], pin['metadata']['inode'])
                  for pin in supplied['files'].values()}
    _need(len(source_ids) == 2 and _ancestors(base, TEMPLATE_ROOT, ancestors) == ancestors,
          'corpus_templates_changed_during_read')
    return templates, source_ids, metadata


def _ext4():
    candidates = []
    for line in Path('/proc/self/mountinfo').read_text().splitlines():
        fields = line.split()
        if fields[4] == str(ROOT) or fields[4].startswith(str(ROOT) + '/'):
            raise CorpusError('corpus_nested_mount_present')
        if fields[4] in {str(path) for path in (F, *F.parents)}:
            candidates.append((len(fields[4]), fields, line))
    _need(candidates, 'corpus_filesystem_missing')
    _, fields, line = max(candidates, key=lambda row: row[0])
    _need(fields[fields.index('-') + 1] == 'ext4', 'corpus_ext4_required')
    return {'mountId': fields[0], 'device': fields[2], 'mountPoint': fields[4],
            'filesystem': 'ext4', 'mountLine': line}


def _directory(base, path):
    row = _metadata(base, path)
    _need(path.resolve() == path and row['type'] == stat.S_IFDIR and
          row['uid'] == row['gid'] == 0 and row['mode'] == DIRECTORY_MODE,
          'corpus_directory_identity')
    return row


def _mkdir(base, path):
    parent = _metadata(base, path.parent)
    _need(path.parent.resolve() == path.parent and parent['type'] == stat.S_IFDIR and
          parent['uid'] == parent['gid'] == 0 and
          parent['mode'] == (0o755 if path == ROOT else DIRECTORY_MODE),
          'corpus_creation_parent_identity')
    _need(not os.path.lexists(path), 'corpus_destination_already_exists')
    os.mkdir(path, DIRECTORY_MODE)
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        opened = os.fstat(fd)
        _need(stat.S_ISDIR(opened.st_mode) and opened.st_uid == opened.st_gid == 0 and
              opened.st_dev == parent['device'], 'corpus_created_directory_identity')
        os.fchmod(fd, DIRECTORY_MODE)
        os.fsync(fd)
        current = _directory(base, path)
        _need((current['device'], current['inode']) == (opened.st_dev, opened.st_ino) and
              _stable(_metadata(base, path.parent), parent), 'corpus_created_directory_replaced')
    finally:
        os.close(fd)


def _copy(base, path, raw):
    """Write one fixed file using ordinary writes, with no copy acceleration."""
    parent = _directory(base, path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                 FILE_MODE)
    try:
        opened = os.fstat(fd)
        _need(stat.S_ISREG(opened.st_mode) and opened.st_nlink == 1 and
              opened.st_uid == opened.st_gid == 0 and opened.st_dev == parent['device'] and
              opened.st_size == 0, 'corpus_created_file_identity')
        remaining = memoryview(raw)
        while remaining:
            written = os.write(fd, remaining)
            _need(written > 0, 'corpus_copy_short_write')
            remaining = remaining[written:]
        os.fchmod(fd, FILE_MODE)
        os.fsync(fd)
        after = os.fstat(fd)
        _need(after.st_size == len(raw) and after.st_blocks * 512 >= len(raw) and
              after.st_nlink == 1 and (after.st_dev, after.st_ino) == (opened.st_dev, opened.st_ino),
              'corpus_copy_size_or_identity')
        current = _metadata(base, path)
        _need((current['device'], current['inode']) == (opened.st_dev, opened.st_ino) and
              _stable(_metadata(base, path.parent), parent), 'corpus_copy_path_replaced')
    finally:
        os.close(fd)


def _inventory(base, template_input, source_ids, provenance_metadata):
    layout = expected_layout()
    ancestors = _ancestors(base, F)
    _need(ancestors[str(F)]['mode'] == 0o755 and
          all(row['mode'] & 0o005 == 0o005 for row in ancestors.values()),
          'corpus_application_ancestor_access')
    filesystem = _ext4()
    directories = {}
    file_rows = {}
    expected_names = (set(layout['directories']) - {'.'}) | set(layout['files'])
    allocated = 0
    device = ancestors[str(F)]['device']
    for relative in layout['directories']:
        path = ROOT if relative == '.' else ROOT / relative
        row = _directory(base, path)
        info = path.lstat()
        _need(info.st_dev == device and info.st_blocks >= 0 and
              {entry.name for entry in path.iterdir()} ==
              {Path(name).name for name in expected_names if Path(name).parent.as_posix() == relative} and
              _metadata(base, path) == row, 'corpus_directory_membership')
        directories[relative] = {'metadata': row, 'allocatedBytes': info.st_blocks * 512}
        allocated += info.st_blocks * 512
    identities = set(source_ids)
    logical = 0
    media_logical = 0
    for relative, expected in layout['files'].items():
        path = ROOT / relative
        before = _metadata(base, path)
        info = path.lstat()
        key = (before['device'], before['inode'])
        _need(path.resolve() == path and before['type'] == stat.S_IFREG and
              before['uid'] == before['gid'] == 0 and before['mode'] == FILE_MODE and
              before['links'] == 1 and before['device'] == device and key not in identities and
              info.st_blocks >= 0 and info.st_blocks * 512 >= before['bytes'] and
              len(str(path)) <= 256 and len(relative) <= 128 and len(path.name) <= 64,
              'corpus_file_identity_or_storage')
        identities.add(key)
        if expected['template'] is None:
            raw_expected = expected['content'].encode('utf-8')
            pin = {'bytes': len(raw_expected), 'sha256': hashlib.sha256(raw_expected).hexdigest()}
        else:
            pin = template_input['files'][expected['template']]
        _need(before['bytes'] == pin['bytes'], 'corpus_file_size')
        raw = base.read_regular(path, 16 << 10)
        _need(len(raw) == pin['bytes'] and hashlib.sha256(raw).hexdigest() == pin['sha256'] and
              _metadata(base, path) == before, 'corpus_file_hash_or_metadata_changed')
        allocated += info.st_blocks * 512
        logical += len(raw)
        if expected['template'] is not None:
            media_logical += len(raw)
        _need(logical <= LOGICAL_LIMIT and allocated <= ALLOCATED_LIMIT,
              'corpus_storage_cap_exceeded')
        file_rows[relative] = {'path': str(path), 'sha256': pin['sha256'],
                              'bytes': len(raw), 'allocatedBytes': info.st_blocks * 512,
                              'metadata': before, 'library': expected['library'],
                              'kind': expected['kind'], 'relativePath': expected['relativePath'],
                              'expected': expected}
    for relative, row in directories.items():
        _need(_metadata(base, ROOT if relative == '.' else ROOT / relative) == row['metadata'],
              'corpus_directory_changed_during_inventory')
    _need(_ancestors(base, F, ancestors) == ancestors and _ext4() == filesystem and
          _metadata(base, TEMPLATE_RECEIPT) == provenance_metadata and
          len(file_rows) == 1020 and len(identities) == 1022, 'corpus_inventory_boundary')
    return {'kind': 'native-scan-http-capacity-corpus', 'version': 1, 'scope': str(E),
            'root': str(ROOT), 'templateInput': template_input,
            'templateProvenanceMetadata': provenance_metadata, 'layout': layout,
            'ancestors': ancestors, 'filesystem': filesystem, 'directories': directories,
            'files': file_rows, 'totals': {'mediaLeaves': 1000, 'albumNfoFiles': 20,
                'regularFiles': 1020, 'directories': len(directories),
                'logicalBytes': logical, 'mediaLogicalBytes': media_logical,
                'allocatedBytesIncludingDirectories': allocated},
            'limits': {'logicalBytes': LOGICAL_LIMIT, 'allocatedBytes': ALLOCATED_LIMIT},
            'access': {'ownerUid': 0, 'ownerGid': 0, 'fileMode': FILE_MODE,
                'directoryMode': DIRECTORY_MODE, 'nonrootReadable': True,
                'nonrootWritable': False, 'boundary': 'POSIX owner and other permission bits; caller binds the nonroot application profile and mount restrictions.'},
            'copyMethod': 'Exclusive ordinary byte writes; no hardlinks, reflinks, sparse writes, truncation, or source replacement.'}


def prepare_corpus(base, template_input):
    """Create only the fixed corpus; leave any partial failure for owned closure."""
    _actor()
    _need(not os.path.lexists(ROOT), 'corpus_scope_already_consumed')
    ancestors = _ancestors(base, F)
    _need(ancestors[str(F)]['mode'] == 0o755, 'corpus_fixture_parent_mode')
    _ext4()
    templates, source_ids, provenance_metadata = _templates(base, template_input)
    layout = expected_layout()
    logical = sum(len(templates[row['template']]) if row['template'] else len(row['content'].encode('utf-8'))
                  for row in layout['files'].values())
    _need(logical <= LOGICAL_LIMIT, 'corpus_planned_logical_cap')
    # Reserve extra filesystem blocks for directory growth before each mutation.
    # The final inventory rereads every file; creation accounting updates only
    # the new member and its parent, avoiding a quadratic full-tree walk.
    block_size = os.statvfs(F).f_frsize
    _need(type(block_size) is int and 0 < block_size <= 65536,
          'corpus_filesystem_block_size')
    planned_allocation = len(layout['directories']) * 4 * block_size + sum(
        ((len(templates[row['template']]) if row['template'] else len(row['content'].encode('utf-8')))
         + block_size - 1) // block_size * block_size + 2 * block_size
        for row in layout['files'].values())
    _need(planned_allocation <= ALLOCATED_LIMIT, 'corpus_planned_allocated_cap')
    allocated = {}

    def account(created):
        for candidate in (created, created.parent):
            if candidate != ROOT and ROOT not in candidate.parents:
                continue
            info = candidate.lstat()
            _need(info.st_dev == ancestors[str(F)]['device'] and
                  (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)) and
                  info.st_blocks >= 0, 'corpus_creation_storage_identity')
            allocated[str(candidate)] = info.st_blocks * 512
        _need(sum(allocated.values()) <= ALLOCATED_LIMIT, 'corpus_creation_allocated_cap')

    for relative in layout['directories']:
        _need(sum(allocated.values()) + 4 * block_size <= ALLOCATED_LIMIT and
              _stable(_metadata(base, F), ancestors[str(F)]),
              'corpus_directory_allocation_reserve')
        path = ROOT if relative == '.' else ROOT / relative
        _mkdir(base, path)
        account(path)
    for relative, row in layout['files'].items():
        raw = templates[row['template']] if row['template'] else row['content'].encode('utf-8')
        reserve = ((len(raw) + block_size - 1) // block_size + 2) * block_size
        _need(sum(allocated.values()) + reserve <= ALLOCATED_LIMIT,
              'corpus_file_allocation_reserve')
        path = ROOT / relative
        _copy(base, path, raw)
        account(path)
    _ancestors(base, F, ancestors)
    # Directory fsyncs follow all file creations; this does not claim power-loss durability.
    for relative in reversed(layout['directories']):
        path = ROOT if relative == '.' else ROOT / relative
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    _, final_source_ids, final_provenance = _templates(base, template_input)
    _need(final_source_ids == source_ids and final_provenance == provenance_metadata,
          'corpus_sources_changed_during_creation')
    return _inventory(base, template_input, source_ids, provenance_metadata)


def snapshot_corpus(base, manifest):
    """Require the full saved corpus to remain unchanged across native phases."""
    _actor()
    _need(isinstance(manifest, dict) and manifest.get('kind') == 'native-scan-http-capacity-corpus' and
          type(manifest.get('version')) is int and manifest['version'] == 1 and
          manifest.get('scope') == str(E) and manifest.get('root') == str(ROOT) and
          manifest.get('layout') == expected_layout(), 'corpus_saved_manifest_contract')
    _, source_ids, provenance_metadata = _templates(base, manifest['templateInput'])
    current = _inventory(base, manifest['templateInput'], source_ids, provenance_metadata)
    _need(set(current) == set(manifest) and all(current[key] == manifest[key]
          for key in current if key != 'ancestors') and
          set(current['ancestors']) == set(manifest['ancestors']) and
          all(_stable(row, manifest['ancestors'][path]) for path, row in current['ancestors'].items()),
          'corpus_saved_snapshot_changed')
    return current
