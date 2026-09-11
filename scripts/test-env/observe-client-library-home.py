#!/usr/bin/env python3
"""Own one B-only original-client Home/Views observation and normal reload.

Only the browser performs login. The controller captures complete read-only
snapshots and may revoke only its child's independently proven new credential.
No administrator, policy update, playback, scan, retry, or old-session cleanup
is available. Existing evidence and the current candidate remain immutable.
"""

from __future__ import annotations

import argparse
from collections import Counter
import datetime as dt
from decimal import Decimal
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
import types
import unicodedata

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = WORK / 'client-library-ui-baseline-v1'
BROWSER_ROOT = ROOT / 'browser'
MARKER = 'goby-client-library-home-observation-v1'
INPUT_MARKER = 'goby-client-library-home-input-v1'
UNIT = 'goby-client-library-ui-baseline-v1.service'
CGROUP = '/system.slice/' + UNIT
B = 'ecbbe4cb82403879bc4b4f78894c5738'
A = '34b4c24f6568659af7ce17938fae7f81'
STATE = WORK / 'client-fixture.json'
STATE_SHA = '5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1'
BROWSER = WORK / 'browser.json'
BROWSER_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790'
BINARY_SHA = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'
SOURCE = WORK / 'source-attempt-32'
MANIFEST_SHA = 'a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65'
PROCESS = {'pid': 748513, 'start_ticks': 6996875, 'boot_id': '6bdfc486-7bc8-412f-82b5-70095a09dde7'}
BASE_URL, DIRECT_URL = 'http://127.0.0.1:18196', 'http://127.0.0.1:18198'
HELPERS = {
    'core': (WORK / 'prepare-client-fixture.py', '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'),
    'restriction': (WORK / 'client-library-restriction-tool-01/verify-client-library-restriction.py', '99cb5b89f0da94940bed34a86607a545278da914750ae09ac7b52d458810004e'),
    'extension': (WORK / 'client-extra-root-tool-02/extend-client-special-features-root.py', '7a9cda4a36ebbe9c2311031db1f6a57265eef8fc8807057a3d650ef6c16b2a46'),
}
AUTHORITY = {
    'api_report': {'path': str(WORK / 'client-library-restriction-v1/report.json'), 'sha256': '193a5cc5ddaa806575d630a03de1268abed2418347b4e57eb5cc57610a04e8eb'},
    'api_after': {'path': str(WORK / 'client-library-restriction-v1/after-full.json'), 'sha256': 'a09e42a404b9aa0251e2341e7ffa85c93528b7b141d81e11df6791802d6e92fd'},
    'inspection': {'path': str(WORK / 'client-library-restriction-inspection-01/report.json'), 'sha256': 'e3dcbc0c21edbc484baab89762e11857a7cbc46889b78701af8dd309f12c3441'},
    'current_snapshot': {'path': str(WORK / 'client-library-restriction-inspection-01/current-full.json'), 'sha256': '1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3'},
}
LIBRARIES = {'a9993591e72f0f2e7babcbf8b9c50790', '6383d20008836e137559698c29b10395',
             'a34ce665fb75421ef7551570f353d705', '57a85c1ca5b6c7ae602c587755250b2f'}
BASE_COUNTS = {'sessions': 67, 'devices': 58, 'activity_entries': 149, 'play_sessions': 26,
               'user_item_data': 7, 'libraries': 4, 'items': 22, 'client_playback_references': 0, 'encoding_jobs': 0}
JS_NAMES = {'client-browser-library-home.mjs', 'client-browser-cross-user.mjs',
    'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs', 'client-browser-session-proof.mjs'}
FIXTURE_KEYS = {'profile_receipt', 'profile_report', 'profile_inspection', 'music_chain', 'music_scan_receipt'}
PROFILE_PINS = {
    'profile_receipt': (WORK / 'client-special-features-protocol-finalization-v1/completed.json', 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36'),
    'profile_report': (WORK / 'client-special-features-protocol-finalization-v1/report.json', '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d'),
    'profile_inspection': (WORK / 'client-special-features-finalization-inspection-v1/report.json', 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e'),
    'music_chain': (WORK / 'client-music-upgrade-chain-source32.json', 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a'),
    'music_scan_receipt': (WORK / 'client-music-scan-v1/receipt.json', 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b'),
}
HASH, ID = re.compile(r'[0-9a-f]{64}'), re.compile(r'[0-9a-f]{32}')
MAX_JSON, MAX_BINARY = 32 << 20, 256 << 20


class ObservationError(Exception):
    """A bounded, sanitized observation or ownership condition failed."""


def require(value, message):
    if not value:
        raise ObservationError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def exact(value):
    if value is None:
        return 'null'
    if type(value) is bool:
        return 'true' if value else 'false'
    if type(value) is int or isinstance(value, Decimal):
        require(not isinstance(value, Decimal) or value.is_finite(), 'A JSON number is nonfinite.')
        return str(value)
    if isinstance(value, str):
        return json.dumps(value, ensure_ascii=False)
    if isinstance(value, list):
        return '[' + ','.join(exact(item) for item in value) + ']'
    require(isinstance(value, dict) and all(isinstance(key, str) for key in value), 'A JSON value is unsupported.')
    return '{' + ','.join(exact(key) + ':' + exact(value[key]) for key in sorted(value)) + '}'


def canonical(value):
    return exact(value).encode('utf-8')


def decode(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'A private JSON field is repeated.')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique, parse_float=Decimal,
        parse_constant=lambda _: (_ for _ in ()).throw(ObservationError('A JSON number is nonfinite.')))


def instant(value):
    require(isinstance(value, str), 'A timestamp is missing.')
    result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.tzinfo is not None, 'A timestamp lacks its timezone.')
    return result


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def protected(path, expected=None, *, modes=(0o600,), limit=MAX_JSON, workspace=True):
    require(path.is_absolute() and '..' not in path.parts and (not workspace or path.is_relative_to(WORK)),
            'An explicit file path escaped its allowed root.')
    for parent in reversed(path.parents):
        observed = parent.lstat()
        require(stat.S_ISDIR(observed.st_mode) and observed.st_uid == observed.st_gid == 0 and not observed.st_mode & 0o022,
                'An input ancestor is not root controlled.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in modes and before.st_size <= limit, 'A private file identity or bound differs.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(identity(os.fstat(handle.fileno())) == identity(before), 'A file changed before opening.')
        raw = handle.read(limit + 1)
        require(identity(os.fstat(handle.fileno())) == identity(before), 'A file changed while reading.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and
            (expected is None or sha(raw) == expected), 'A file changed or lost its exact digest.')
    return raw


def load_source(path, digest, name):
    raw = protected(path, digest, modes=(0o600, 0o644), limit=2 << 20)
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def descriptor(row):
    require(isinstance(row, dict) and set(row) == {'path', 'sha256'} and HASH.fullmatch(row.get('sha256', '')),
            'An artifact descriptor is incomplete.')
    return Path(row['path']), row['sha256']


def read_record(row):
    return decode(protected(*descriptor(row)))


def proc_identity(pid):
    require(type(pid) is int and pid > 1, 'A process identity is invalid.')
    fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(') ', 1)[1].split()
    require(len(fields) > 19 and re.fullmatch(r'[1-9][0-9]*', fields[19]), 'A process start time is invalid.')
    return {'pid': pid, 'start_ticks': fields[19], 'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}


def validate_baseline(snapshot):
    require(snapshot.get('schema') == 27 and isinstance(snapshot.get('database'), dict), 'The baseline is not schema27.')
    tables = snapshot['database']['tables']
    require(len(tables) == 35 and all(isinstance(tables.get(name), list) and len(tables[name]) == count
        for name, count in BASE_COUNTS.items()), 'The latest complete baseline population differs.')
    require({row['id'] for row in tables['libraries']} == LIBRARIES and
            sum(row.get('user_id') in (A, B) for row in tables['sessions']) == 59,
            'The current libraries or two-user authentication baseline differs.')
    rows = [row for row in tables['users'] if row['id'] == B]
    policy = {'EnableAllFolders': True, 'EnabledFolders': [], 'EnableMediaPlayback': True, 'EnablePlaybackRemuxing': True,
        'EnableAudioPlaybackTranscoding': True, 'EnableVideoPlaybackTranscoding': True, 'IsAdministrator': False, 'IsDisabled': False}
    require(len(rows) == 1 and rows[0]['management_revision'] == 3 and rows[0]['is_administrator'] is False and
            rows[0]['is_disabled'] is False and canonical(rows[0]['policy']) == canonical(policy),
            'B is not the latest revision3, materialized restored Policy baseline.')


def validate_authority(api, inspection, state, baseline):
    expected_candidate = {'binary_sha256': BINARY_SHA, 'process': PROCESS, 'state_sha256': STATE_SHA}
    require(api.get('marker') == 'goby-client-library-restriction-v1' and api.get('result') == 'passed' and
            api.get('phase') == 'complete' and api.get('restoration') == 'confirmed' and api.get('candidate') == expected_candidate and
            api.get('snapshots', {}).get('after') == AUTHORITY['api_after'] and api.get('proof', {}).get('b_revision_after') == '3' and
            api['proof'].get('raw_policy_matches_exact_merge') is True, 'The accepted restriction/restore authority differs.')
    require(inspection.get('marker') == 'goby-client-library-restriction-inspection-v1' and inspection.get('status') == 'passed' and
            inspection.get('candidate') == expected_candidate and inspection.get('report_sha256') == AUTHORITY['api_report']['sha256'] and
            inspection.get('current_snapshot_path') == AUTHORITY['current_snapshot']['path'] and
            inspection.get('current_snapshot_sha256') == AUTHORITY['current_snapshot']['sha256'] and inspection.get('b_revision_after') == 3 and
            all(inspection.get(key) is True for key in ('stored_after_matches_current', 'raw_policy_exact_merge', 'media_unchanged', 'all_three_owned_sessions_revoked')),
            'The independent persisted-state inspection differs.')
    require(state.get('schema') == 27 and state.get('phase') == 'ready' and state.get('stage') == 'complete' and
            state.get('process') == PROCESS and state.get('binary_sha256') == BINARY_SHA and state.get('viewer_id') == B and
            state.get('added_viewer', {}).get('user_id') == A, 'The selected candidate is not the current ordinary fixture.')
    validate_baseline(baseline)


def validate_login(proof, expected_server):
    keys = {'token_sha256', 'session_id', 'user_id', 'device_id', 'server_id', 'client_name', 'device_name', 'client_version',
        'created_at', 'created_at_source', 'kind', 'slot', 'frame_login_finished', 'physical_login_completed', 'request_metadata_matches'}
    require(isinstance(proof, dict) and set(proof) == keys and HASH.fullmatch(proof.get('token_sha256', '')) and
            ID.fullmatch(proof.get('session_id', '')) and proof.get('user_id') == B and proof.get('server_id') == expected_server and
            proof.get('kind') == 'emby' and proof.get('slot') == 'B' and proof.get('created_at_source') == 'SessionInfo.LastActivityDate' and
            all(proof.get(key) is True for key in ('frame_login_finished', 'physical_login_completed', 'request_metadata_matches')),
            'The browser has no complete B-only physical and frame login proof.')
    for key in ('device_id', 'client_name', 'device_name', 'client_version'):
        require(isinstance(proof[key], str) and 0 < len(proof[key].encode()) <= 256 and '\0' not in proof[key],
                'The actual login client metadata is incomplete or invalid.')
    instant(proof['created_at'])


def owned_session(before, after, proof, expected_server):
    validate_login(proof, expected_server)
    old = before['database']['tables']['sessions']
    require(all(row['id'] != proof['session_id'] and row['token_hash'] != '\\x' + proof['token_sha256'] for row in old),
            'The browser credential was already present before this observation.')
    rows = [row for row in after['database']['tables']['sessions'] if row['id'] == proof['session_id'] or row['token_hash'] == '\\x' + proof['token_sha256']]
    require(len(rows) == 1 and rows[0]['id'] == proof['session_id'] and rows[0]['token_hash'] == '\\x' + proof['token_sha256'] and
            rows[0]['user_id'] == B and rows[0]['kind'] == 'emby' and rows[0]['device_id'] == proof['device_id'] and
            rows[0]['client_name'] == proof['client_name'] and rows[0]['device_name'] == proof['device_name'] and
            rows[0]['client_version'] == proof['client_version'] and instant(rows[0]['created_at']) == instant(proof['created_at']),
            'The actual login proof does not identify one new ordinary B authentication row.')
    return rows[0]


def validate_private_session(private, input_record, input_sha, child, before, after):
    require(isinstance(private, dict) and set(private) == {'marker', 'version', 'input_sha256', 'source_closure_sha256',
        'node_process', 'controller', 'proof', 'token'} and private.get('marker') == 'goby-client-library-home-session-v1' and
        private.get('version') == 1 and private.get('input_sha256') == input_sha and
        private.get('source_closure_sha256') == sha(canonical(input_record['source_closure'])) and
        private.get('controller') == input_record['controller'] and private.get('node_process') == child,
        'A fallback credential is not bound to this exact child, controller and input.')
    token = private.get('token')
    require(isinstance(token, str) and re.fullmatch(r'[A-Za-z0-9_-]{43}', token) and sha(token.encode('utf-8')) == private['proof']['token_sha256'],
            'The private token does not match the original UTF-8 AccessToken fingerprint.')
    row = owned_session(before, after, private['proof'], input_record['candidate']['server_id'])
    return token, row


# This bounded schema is the frozen source32 capability sanitizer. It is used
# only to derive the value allowed by the exact observed successful requests.
TEXT, BOOL, INT32, INT64 = 'text', 'bool', 'int32', 'int64'
KIND = ('Audio', 'Video', 'Photo')
CONDITION = {'Condition': ('Equals', 'NotEquals', 'LessThanEqual', 'GreaterThanEqual', 'EqualsAny'),
    'Property': tuple('AudioChannels AudioBitrate AudioProfile Width Height Has64BitOffsets PacketLength VideoBitDepth VideoBitrate VideoFramerate VideoLevel VideoProfile VideoTimestamp IsAnamorphic RefFrames NumAudioStreams NumVideoStreams IsSecondaryAudio VideoCodecTag IsAvc IsInterlaced AudioSampleRate AudioBitDepth VideoRange VideoRotation IsExternalAudio'.split()),
    'Value': TEXT, 'IsRequired': BOOL}
DEVICE_PROFILE = {'Name': TEXT, 'Id': TEXT, 'SupportedMediaTypes': TEXT, 'MaxStreamingBitrate': INT64,
    'MusicStreamingTranscodingBitrate': INT32, 'MaxStaticMusicBitrate': INT32, 'DeclaredFeatures': [TEXT],
    'DirectPlayProfiles': [{'Container': TEXT, 'AudioCodec': TEXT, 'VideoCodec': TEXT, 'Type': KIND}],
    'TranscodingProfiles': [{'Container': TEXT, 'Type': KIND, 'VideoCodec': TEXT, 'AudioCodec': TEXT, 'Protocol': TEXT,
        'EstimateContentLength': BOOL, 'EnableMpegtsM2TsMode': BOOL, 'TranscodeSeekInfo': ('Auto', 'Bytes'), 'CopyTimestamps': BOOL,
        'Context': ('Streaming', 'Static'), 'MaxAudioChannels': TEXT, 'MinSegments': INT32, 'SegmentLength': INT32,
        'BreakOnNonKeyFrames': BOOL, 'AllowInterlacedVideoStreamCopy': BOOL, 'ManifestSubtitles': TEXT,
        'MaxManifestSubtitles': INT32, 'MaxWidth': INT32, 'MaxHeight': INT32, 'FillEmptySubtitleSegments': BOOL}],
    'ContainerProfiles': [{'Type': KIND, 'Conditions': [CONDITION], 'Container': TEXT}],
    'CodecProfiles': [{'Type': ('Video', 'VideoAudio', 'Audio'), 'Conditions': [CONDITION], 'ApplyConditions': [CONDITION], 'Codec': TEXT, 'Container': TEXT}],
    'ResponseProfiles': [{'Container': TEXT, 'AudioCodec': TEXT, 'VideoCodec': TEXT, 'Type': KIND, 'OrgPn': TEXT, 'MimeType': TEXT, 'Conditions': [CONDITION]}],
    'SubtitleProfiles': [{'Format': TEXT, 'Method': ('Encode', 'Embed', 'External', 'Hls', 'VideoSideData'), 'DidlMode': TEXT,
        'Language': TEXT, 'Container': TEXT, 'AllowChunkedResponse': BOOL, 'Protocol': TEXT}]}
CAPABILITIES = {'PlayableMediaTypes': [TEXT], 'SupportedCommands': [TEXT], 'SupportsMediaControl': BOOL,
    'PushToken': TEXT, 'PushTokenType': TEXT, 'SupportsSync': BOOL, 'DeviceProfile': DEVICE_PROFILE, 'IconUrl': TEXT, 'AppId': TEXT}


def normalize_capabilities(raw):
    require(isinstance(raw, bytes) and 0 < len(raw) <= 65536, 'A capability body exceeds its exact wire bound.')
    value = decode(raw.decode('utf-8', errors='strict'))
    nodes = [0]
    def text_bound(text, maximum=2048):
        require(len(text.encode('utf-8')) <= maximum and not any(unicodedata.category(char) == 'Cc' for char in text), 'Capability text exceeds its permitted bound.')
    def walk(value, depth=0):
        nodes[0] += 1
        require(depth <= 8 and nodes[0] <= 4096, 'Capability JSON exceeds depth or node limits.')
        if isinstance(value, dict):
            require(len(value) <= 64, 'A capability object is too large.')
            for key, child in value.items():
                text_bound(key, 256); walk(child, depth + 1)
        elif isinstance(value, list):
            require(len(value) <= 128, 'A capability array is too large.')
            for child in value: walk(child, depth + 1)
        elif isinstance(value, str): text_bound(value)
    def clean(value, shape):
        if isinstance(shape, dict):
            require(isinstance(value, dict), 'A known capability object has another type.')
            return {key: clean(child, shape[key]) for key, child in value.items() if key in shape and child is not None}
        if isinstance(shape, list):
            require(isinstance(value, list) and all(child is not None for child in value), 'A known capability array is invalid.')
            return [clean(child, shape[0]) for child in value]
        if isinstance(shape, tuple):
            require(isinstance(value, str) and value in shape, 'A known capability enum is invalid.'); return value
        require((shape == TEXT and isinstance(value, str)) or (shape == BOOL and type(value) is bool) or
            (shape in (INT32, INT64) and type(value) is int and 0 <= value <= (2147483647 if shape == INT32 else 9223372036854775807)),
            'A known capability scalar has another type or value.')
        return value
    walk(value)
    result = clean(value, CAPABILITIES)
    result.pop('PushToken', None); result.pop('PushTokenType', None)
    return {key: value for key, value in result.items() if key == 'DeviceProfile' or value not in (False, '', [])}


def capability_entry_value(entry):
    raw = entry['body_utf8'].encode('utf-8')
    require(len(raw) <= 65536 and sha(raw) == entry['body_sha256'], 'A captured capability body digest changed.')
    if entry['kind'] == 'full':
        return normalize_capabilities(raw)
    require(entry['kind'] == 'query' and isinstance(entry['query'], dict) and set(entry['query']) <=
            {'playablemediatypes', 'supportedcommands', 'supportsmediacontrol', 'supportssync'}, 'A query capability snapshot is unsupported.')
    value = {}
    for key, field in (('playablemediatypes', 'PlayableMediaTypes'), ('supportedcommands', 'SupportedCommands')):
        if key in entry['query']:
            value[field] = [word.strip() for word in entry['query'][key].split(',') if word.strip()]
    for key, field in (('supportsmediacontrol', 'SupportsMediaControl'), ('supportssync', 'SupportsSync')):
        if key in entry['query']:
            source = entry['query'][key]
            require(source in ('1', 't', 'T', 'TRUE', 'true', 'True', '0', 'f', 'F', 'FALSE', 'false', 'False'), 'A query capability boolean is invalid.')
            value[field] = source in ('1', 't', 'T', 'TRUE', 'true', 'True')
    return normalize_capabilities(canonical(value))


def capability_values(document, proof, input_record, input_sha, child):
    require(isinstance(document, dict) and set(document) == {'marker', 'version', 'input_sha256', 'source_closure_sha256',
        'node_process', 'session_id', 'token_sha256', 'entries'} and
        document.get('marker') == 'goby-client-library-home-capabilities-v1' and document.get('version') == 1 and
        document.get('input_sha256') == input_sha and document.get('source_closure_sha256') == sha(canonical(input_record['source_closure'])) and
        document.get('node_process') == child and document.get('session_id') == proof['session_id'] and
        document.get('token_sha256') == proof['token_sha256'], 'The capability journal belongs to another session or child.')
    entries = document.get('entries')
    require(isinstance(entries, list) and len(entries) <= 16, 'The capability request inventory exceeds its scope.')
    observed, successful = set(), []
    for entry in entries:
        require(isinstance(entry, dict) and set(entry) == {'physical_id', 'kind', 'request_elapsed_ms', 'response_elapsed_ms',
            'finished_elapsed_ms', 'token_matches_session', 'status', 'completed', 'body_utf8', 'body_sha256', 'query'} and
            type(entry.get('physical_id')) is int and entry['physical_id'] > 0 and entry.get('kind') in ('full', 'query') and
            isinstance(entry.get('body_utf8'), str) and HASH.fullmatch(entry.get('body_sha256', '')) and
            isinstance(entry.get('query'), dict) and entry.get('token_matches_session') is True and
            canonical(entry.get('physical_id')) not in observed, 'A capability request is unowned or repeated.')
        observed.add(canonical(entry['physical_id']))
        if entry.get('status') != 204 or entry.get('completed') is not True:
            continue
        times = [entry.get(key) for key in ('request_elapsed_ms', 'response_elapsed_ms', 'finished_elapsed_ms')]
        require(all(type(value) is int or isinstance(value, Decimal) for value in times) and
            0 <= times[0] <= times[1] <= times[2] <= 600000, 'A successful capability request lacks ordered terminal observations.')
        successful.append((times[0], times[1], capability_entry_value(entry), entry['body_sha256']))
    if not successful:
        return [{}]
    # A response completed before a later request started establishes commit
    # order. Differing unordered final writes cannot establish one exact value.
    last = [value for value in successful if not any(value[1] < other[0] for other in successful)]
    require(len({canonical(value[2]) for value in last}) == 1, 'Concurrent differing capability writes have no proven final order.')
    return [value[2] for value in last]


def validate_delta(op, restriction, before, after, proof, server_id, capabilities):
    additions = restriction.unchanged_rows(op, before, after)
    require(all(len(rows) == {'sessions': 1, 'devices': 1, 'activity_entries': 2}.get(name, 0) for name, rows in additions.items()),
            'Home observation changed state outside one new B session/device and two session audits.')
    row = owned_session(before, after, proof, server_id)
    start, end = instant(before['database']['metadata']['captured_at']), instant(after['database']['metadata']['captured_at'])
    created = instant(row['created_at'])
    require(start <= created <= instant(row['last_seen_at']) <= end and row['revoked_at'] is not None and
        created <= instant(row['revoked_at']) <= end and instant(row['expires_at']) == created + dt.timedelta(days=30) and
        any(op.equal_json(row['client_capabilities'], value) for value in capabilities),
        'The new authentication changed outside its actual login, observed capabilities, Touch and revocation.')
    device = additions['devices'][0]
    require(device['id'] == row['device_registry_id'] and device['reported_device_id'] == proof['device_id'] and
        all(old['reported_device_id'] != proof['device_id'] for old in before['database']['tables']['devices']) and
        device['reported_name'] == proof['device_name'] and device['app_name'] == proof['client_name'] and
        device['app_version'] == proof['client_version'] and device['last_user_id'] == B and device['ip_address'] == '127.0.0.1' and
        device['custom_name'] is None and device['deleted_at'] is None and device['revision'] == 1 and
        start <= instant(device['created_at']) <= created and instant(device['created_at']) <= instant(device['last_seen_at']) <= end,
        'The new device does not preserve its exact fresh login metadata.')
    expected = Counter((action, 'emby', 'user', B, proof['session_id'], 'session', proof['session_id'])
                       for action in ('session.login', 'session.revoked'))
    keys = ('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id')
    require(Counter(tuple(event[key] for key in keys) for event in additions['activity_entries']) == expected and
        all(event['severity'] == 'Info' and event['affected_count'] == 1 and event['revision'] == 0 and
            event['changed_fields'] == [] and event['state'] == '' and start <= instant(event['created_at']) <= end for event in additions['activity_entries']),
        'The audit delta is not exactly the new B login and revocation.')
    left, right = before['database']['sequences'], after['database']['sequences']
    require(set(left) == set(right), 'The sequence population changed.')
    for name, original in left.items():
        if name in ('devices_id_seq', 'activity_entries_id_seq'):
            table, count = ('devices', 1) if name == 'devices_id_seq' else ('activity_entries', 2)
            first = original['last_value'] + int(original['is_called'])
            require(right[name] == {'last_value': first + count - 1, 'is_called': True} and
                sorted(item['id'] for item in additions[table]) == list(range(first, first + count)), 'A sequence increment lacks its exact new rows.')
        else:
            require(op.equal_json(original, right[name]), 'An unrelated sequence changed.')
    return {'old_rows_sequences_private_unchanged': True, 'users_and_policies_unchanged': True,
        'new_b_authentication': 1, 'new_devices': 1, 'new_session_audits': 2, 'new_play_userdata_references_encodings': 0,
        'session_id': proof['session_id'], 'token_sha256': proof['token_sha256'], 'observed_capabilities_bound': True}


def validate_source_manifest(value, driver):
    require(isinstance(value, dict) and set(value) == {'marker', 'files'} and
        value['marker'] == 'goby-client-library-home-sources-v1' and isinstance(value['files'], dict) and
        len(value['files']) == 5 and driver.name == 'client-browser-library-home.mjs' and
        set(value['files']) == {str(driver.parent / name) for name in JS_NAMES},
        'The explicit browser source closure is not exactly the five reviewed imports.')
    for filename, digest in value['files'].items():
        descriptor({'path': filename, 'sha256': digest})
        require(Path(filename).is_absolute() and Path(filename).is_relative_to(WORK) and
            '..' not in Path(filename).parts, 'A browser source left the private workspace.')
    return dict(value['files'])


def validate_node_report(report, input_record, input_sha, child):
    require(report.get('marker') == 'goby-client-library-home-report-v1' and report.get('version') == 1 and
        report.get('mode') == input_record['mode'] and report.get('input_sha256') == input_sha and
        report.get('source_closure_sha256') == sha(canonical(input_record['source_closure'])) and
        report.get('candidate') == input_record['candidate'] and report.get('controller') == input_record['controller'] and
        report.get('node_process') == child and report.get('client_acceptance') is False and
        report.get('permission_ui_acceptance') is False, 'The browser report belongs to another input, process or acceptance scope.')


def ui_logout_proven(report, proof):
    actor = report.get('actor', {})
    logout, physical, check = actor.get('logout', {}), actor.get('proxy_logout', {}), actor.get('session_proof', {})
    rows = check.get('entries', [])
    return (logout.get('status') == 204 and logout.get('login_view_visible') is True and
        physical.get('status') == 204 and physical.get('completed') is True and physical.get('token_fingerprint') == proof['token_sha256'] and
        check.get('outcome') == 'all_observed_logout_tokens_rejected' and len(rows) == 1 and
        rows[0].get('result') == 'logout_token_rejected' and rows[0].get('token_fingerprint') == proof['token_sha256'])


def validate_observation(report, proof, terminal):
    closure, actor = report.get('closure', {}), report.get('actor', {})
    require(report.get('result') == 'passed' and report.get('outcome') == 'baseline_observation' and not report.get('failure') and
        report.get('login_proof') == proof and report.get('session_private') and report.get('capabilities_private') and
        all(report.get(stage, {}).get('views', {}).get('result') == 'passed' and
            report[stage].get('dom', {}).get('passed') is True for stage in ('initial', 'reload')) and
        report['reload'].get('action', {}).get('kind') == 'page.reload' and report['reload']['action'].get('count') == 1 and
        report['reload']['action'].get('completed') is True and actor.get('login', {}).get('request_count') == 1 and
        actor['login'].get('status') == 200 and actor.get('ordinary_authority_confirmed') is True and actor.get('page_error_count') == 0 and
        ui_logout_proven(report, proof), 'The original client did not complete the exact B Home/Views and reload observation.')
    require(all(closure.get(key) is True for key in ('context_closed', 'browser_closed', 'proxy_closed')) and
        all(closure.get(key) == 0 for key in ('http_pending', 'websocket_pending', 'websocket_active', 'sockets_remaining')) and
        type(closure.get('websocket_opened')) is int and closure['websocket_opened'] > 0 and
        closure.get('websocket_closed') == closure['websocket_opened'] and closure.get('cleanup_failures') == [] and
        terminal.get('cgroup_empty') is True and terminal.get('MainPID') == '0' and terminal.get('ExecMainStatus') == '0',
        'The browser, WebSocket or owned worker lifecycle did not close completely.')


def fallback_http(token, revoked, connection_factory=http.client.HTTPConnection):
    """Only the caller's already proven new token is accepted; never authenticate."""
    require(isinstance(token, str) and re.fullmatch(r'[A-Za-z0-9_-]{43}', token), 'Cleanup requires an exact proven AccessToken.')
    records = []
    for method, path, expected in ([] if revoked else [('POST', '/emby/Sessions/Logout', 204)]) + [('GET', '/emby/System/Info', 401)]:
        connection = connection_factory('127.0.0.1', 18198, timeout=8)
        record = {'method': method, 'path': path, 'status': None, 'complete': False}
        try:
            # The caller installs a total cleanup alarm, not only a socket timeout.
            connection.request(method, path, None, {'X-Emby-Token': token, 'Origin': BASE_URL,
                'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close'})
            response = connection.getresponse()
            record['status'] = response.status
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= 8192, 'An owned cleanup response exceeds its byte limit.')
            raw = response.read(8193)
            require(len(raw) <= 8192 and (length is None or len(raw) == int(length)), 'An owned cleanup response is incomplete.')
            record.update(complete=True, bytes=len(raw), sha256=sha(raw))
            require(response.status == expected and (expected != 204 or not raw), 'Owned cleanup did not return the exact required status.')
        except Exception as error:
            record['failure_type'] = type(error).__name__
        finally:
            connection.close()
            records.append(record)
    return records


class Run:
    def __init__(self, args):
        self.args, self.op, self.restriction, self.ext = args, None, None, None
        self.lock = self.root_fd = None
        self.state = self.before = self.after = self.media = self.input = self.child = self.terminal = None
        self.input_sha = None
        self.sources, self.artifacts, self.records, self.errors = {}, {}, {}, []
        self.launched, self.fallback_attempted, self.closed = False, False, False
        self.node_report = self.session = self.capabilities = self.delta = None
        self.phase = 'preflight'

    def error(self, stage, error):
        # Exception messages can contain credentials or raw database/HTTP data.
        row = {'stage': stage, 'failure_type': type(error).__name__}
        if type(error) is ObservationError:
            row['reason'] = str(error)
        self.errors.append(row)

    def command(self, arguments, text=None, environment=None, timeout=30):
        values = [str(value) for value in arguments]
        allowed = values == ['/usr/bin/ss', '-H', '-ltnp', 'sport = :18198'] or (
            len(values) == 5 and values[:3] == ['/usr/bin/systemctl', 'show', 'goby-client-m3e.service'] and
            values[3] == '--no-pager' and values[4].startswith('--property='))
        pg = ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/psql', '-X', '--no-password',
              '-h', '/var/lib/postgresql/goby-workspace-v1/socket', '-p', '15432', '-U', 'postgres', '-d']
        is_pg = (len(values) == 20 and values[:14] == pg and values[14] in ('postgres', 'goby_client_m3e') and
            values[15:] == ['-v', 'ON_ERROR_STOP=1', '-A', '-t', '-q'])
        if is_pg:
            require(isinstance(text, str) and text.lstrip().startswith(('SELECT ', 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')),
                'A helper attempted a non-read-only SQL entry point.')
        media = (len(values) == 7 and values[:6] == ['/usr/sbin/runuser', '-u', 'goby', '--', '/usr/bin/test', '-r'] and
            Path(values[6]).is_relative_to(Path('/opt/goby-fixtures/client-m3e')) and '..' not in Path(values[6]).parts)
        require(allowed or is_pg or media, 'The frozen helper attempted an unapproved command or service mutation.')
        env = dict(self.op.ENV)
        if is_pg:
            env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=20000 -c lock_timeout=5000'
        require(environment is None, 'A read-only helper supplied an unexpected environment.')
        result = subprocess.run(values, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=min(timeout, 25), check=False, env=env)
        require(result.returncode == 0 and len(result.stdout.encode()) <= MAX_JSON,
            'A bounded read-only helper command failed; no alternate command was attempted.')
        return result.stdout.strip()

    def properties(self):
        fields = 'Id,LoadState,ActiveState,SubState,MainPID,ExecMainCode,ExecMainStatus,ControlGroup,InvocationID,Transient,User,Group'
        result = subprocess.run(['/usr/bin/systemctl', 'show', UNIT, '--no-pager', '--property=' + fields],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10, env=self.op.ENV, check=False)
        require(result.returncode in (0, 1, 4) and len(result.stdout) <= 32768, 'The owned worker state cannot be read.')
        return dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)

    def check(self):
        for filename, digest in self.sources.items():
            protected(Path(filename), digest, modes=(0o600, 0o644), limit=2 << 20)
        for filename, digest in self.artifacts.items():
            protected(Path(filename), digest, modes=(0o600, 0o644), limit=MAX_JSON)
        protected(STATE, STATE_SHA)
        protected(self.args.node, self.args.node_sha256, modes=(0o755,), limit=MAX_BINARY, workspace=False)
        self.op.verify_fixture_directories(self.state)
        require(self.op.verify_service(self.state) == PROCESS, 'The exact current candidate process changed.')
        if self.root_fd is not None:
            named, opened = ROOT.lstat(), os.fstat(self.root_fd)
            require((named.st_dev, named.st_ino) == (opened.st_dev, opened.st_ino) and stat.S_ISDIR(named.st_mode) and
                named.st_uid == named.st_gid == 0 and stat.S_IMODE(named.st_mode) == 0o700, 'The evidence root changed identity.')

    def save(self, name, value):
        require(self.root_fd is not None and re.fullmatch(r'[a-z][a-z0-9-]*\.(json|stdout|stderr)', name), 'An evidence write escaped its new root.')
        raw = value if isinstance(value, bytes) else canonical(value) + b'\n'
        require(len(raw) <= MAX_JSON, 'A private evidence record exceeds its byte bound.')
        descriptor_fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=self.root_fd)
        with os.fdopen(descriptor_fd, 'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        os.fsync(self.root_fd)
        row = {'path': str(ROOT / name), 'sha256': sha(raw)}
        self.records[name] = row
        return row

    def snapshot(self, name):
        # Snapshot failure is independent of browser cleanup, and never repaired by
        # invoking a writer or an old fixture validator with an empty profile.
        value = self.op.preservation_snapshot(self.state, 27)
        self.save(name, value)
        return value

    def load(self):
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
            os.environ.get('SSH_CONNECTION'), 'Use the authorized root SSH environment only.')
        os.umask(0o077)
        self.sources = validate_source_manifest(decode(protected(self.args.source_closure, self.args.source_closure_sha256)), self.args.driver)
        self.sources[str(Path(__file__).absolute())] = self.args.script_sha256
        for name, (path, digest) in HELPERS.items():
            self.sources[str(path)] = digest
            module = load_source(path, digest, 'home_' + name)
            setattr(self, {'core': 'op', 'restriction': 'restriction', 'extension': 'ext'}[name], module)
        require(len(self.sources) == 9, 'A source path collides with an independent helper.')
        self.op.command = self.command
        self.op.host_inputs(initial=False)
        self.lock = os.open(self.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(self.op.identity(os.fstat(self.lock)) == self.op.identity(self.op.regular(self.op.LOCK)), 'The existing fixture lock changed identity.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(ROOT), 'The observation root already exists; do not retry, overwrite or adopt it.')
        unit = self.properties()
        require(unit.get('LoadState') == 'not-found' and unit.get('MainPID') == '0', 'The fixed browser worker name is already occupied.')
        require(not Path('/sys/fs/cgroup' + CGROUP).exists(), 'The fixed browser worker cgroup is already occupied.')
        self.state = decode(protected(STATE, STATE_SHA))
        self.artifacts[str(self.args.source_closure)] = self.args.source_closure_sha256
        for row in AUTHORITY.values():
            self.artifacts[row['path']] = row['sha256']
        self.artifacts[str(BROWSER)] = BROWSER_SHA
        for path, digest in PROFILE_PINS.values():
            self.artifacts[str(path)] = digest
        baseline, api, inspection = (read_record(AUTHORITY[key]) for key in ('current_snapshot', 'api_report', 'inspection'))
        validate_authority(api, inspection, self.state, baseline)
        require(self.state.get('runtime_sha256') == 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df' and
            self.state.get('server_id') == 'c7cfd76b1dee728b2bad523793a37ccb' and
            self.state.get('media_root_extension', {}).get('script_sha256') == HELPERS['extension'][1], 'The runtime, server or media extension changed.')
        self.restriction.compare_fixed_snapshot(self.op, read_record(AUTHORITY['api_after']), baseline)
        product = self.state['upgrade']['schema_artifacts']['product_verification']
        require(self.op.equal_json(self.op.verify_product_upgrade(Path(self.state['upgrade']['source']), BINARY_SHA, SOURCE,
            MANIFEST_SHA, Path(product['report_path']), product['report_sha256'], 27), product), 'The accepted source32 product proof changed.')
        self.check()
        self.media = self.ext.media_witness(self.op, self.state)
        self.before = self.op.preservation_snapshot(self.state, 27)
        self.restriction.compare_fixed_snapshot(self.op, baseline, self.before)
        self.restriction.quiescent(self.before)
        validate_baseline(self.before)

    def prepare(self):
        ROOT.mkdir(mode=0o700)
        self.root_fd = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        parent = os.open(WORK, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try: os.fsync(parent)
        finally: os.close(parent)
        self.save('intent.json', {'marker': MARKER, 'version': 1, 'mode': 'b-home-views-reload', 'root': str(ROOT),
            'controller': proc_identity(os.getpid()), 'unit': UNIT, 'source_closure': self.sources,
            'authority': AUTHORITY, 'requests': 'One browser B login, Home/Views, one reload, owned UI logout and exact401 only.'})
        before_record = self.save('before-full.json', self.before)
        self.save('media-before.json', self.media)
        controller = {**proc_identity(os.getpid()), 'unit': UNIT}
        self.input = {'marker': INPUT_MARKER, 'version': 1, 'mode': 'b-home-views-reload', 'root': str(ROOT), 'output': str(BROWSER_ROOT),
            'actor': {'slot': 'B', 'user_id': B, 'credentials': {'path': str(BROWSER), 'sha256': BROWSER_SHA}, 'account_key': 'viewer'},
            'candidate': {'binary_sha256': BINARY_SHA, 'process': PROCESS, 'runtime_sha256': self.state['runtime_sha256'],
                'state_sha256': STATE_SHA, 'server_id': self.state['server_id'], 'base_url': BASE_URL, 'direct_url': DIRECT_URL,
                'source': str(SOURCE), 'source_manifest_sha256': MANIFEST_SHA},
            'fixture': {key: {'path': str(path), 'sha256': digest} for key, (path, digest) in PROFILE_PINS.items()},
            'expected_libraries': sorted([{'id': row['id'], 'name': row['name']} for row in self.before['database']['tables']['libraries']], key=lambda row: row['id']),
            'source_closure': self.sources, 'authority': {**{key: AUTHORITY[key] for key in ('api_report', 'inspection', 'current_snapshot')},
                'before_snapshot': before_record}, 'controller': controller}
        self.input_sha = self.save('input.json', self.input)['sha256']
        self.save('cleanup-reservation.json', {'marker': MARKER, 'input_sha256': self.input_sha, 'controller': controller,
            'session_path': str(BROWSER_ROOT / 'session-private.json'), 'scope': 'Only new B authentication proven by this child; at most one logout and one exact401.',
            'maximum_requests': 2, 'no_login': True, 'no_existing_credentials': True})
        self.save('node.stdout', b''); self.save('node.stderr', b'')

    def node_arguments(self):
        return [str(self.args.node), str(self.args.driver), '--input', str(ROOT / 'input.json'), '--input-sha256', self.input_sha,
                '--output', str(BROWSER_ROOT)]

    def capture_child(self, status):
        require(status.get('Id') == UNIT and status.get('Transient') == 'yes' and status.get('ControlGroup') == CGROUP and
            status.get('User') == 'root' and status.get('Group') == 'root' and ID.fullmatch(status.get('InvocationID', '')),
            'The worker does not have the exact new transient-unit identity.')
        pid = int(status.get('MainPID', '0'))
        current = proc_identity(pid)
        process = Path('/proc') / str(pid)
        require(current['boot_id'] == PROCESS['boot_id'] and process.stat().st_uid == process.stat().st_gid == 0 and
            os.readlink(process / 'exe') == str(self.args.node) and
            (process / 'cmdline').read_bytes() == b'\0'.join(word.encode() for word in self.node_arguments()) + b'\0' and
            any(line.split(':', 2)[-1] == CGROUP for line in (process / 'cgroup').read_text().splitlines()) and
            sha((process / 'exe').read_bytes()) == self.args.node_sha256 and proc_identity(pid) == current,
            'The worker PID, executable, invocation or kernel cgroup is not owned.')
        self.invocation = status['InvocationID']
        self.child = {**current, 'uid': 0, 'gid': 0, 'executable_path': str(self.args.node),
            'executable_sha256': self.args.node_sha256, 'cgroup': CGROUP}
        try: self.save('node-process.json', {'process': self.child, 'invocation_id': self.invocation})
        except Exception as error: self.error('child_process_journal', error)

    def cgroup_empty(self):
        group = Path('/sys/fs/cgroup' + CGROUP)
        if not group.exists(): return True
        paths = [group, *group.rglob('*')]
        require(len(paths) <= 256 and all(not path.is_symlink() for path in paths), 'The owned worker cgroup tree exceeds its bound.')
        return all(not path.read_text().strip() for path in paths if path.name == 'cgroup.procs')

    def await_worker(self):
        deadline = time.monotonic() + 395
        while time.monotonic() < deadline:
            status = self.properties()
            if self.child is None and int(status.get('MainPID', '0')) > 1:
                try: self.capture_child(status)
                except FileNotFoundError:
                    time.sleep(0.5); continue
            if self.child is not None:
                require(status.get('LoadState') == 'not-found' or status.get('InvocationID') == self.invocation,
                    'The owned worker unit was replaced.')
                if int(status.get('MainPID', '0')) > 1:
                    try: live = proc_identity(int(status['MainPID']))
                    except FileNotFoundError:
                        time.sleep(0.5); continue
                    require(live == {key: self.child[key] for key in ('pid', 'start_ticks', 'boot_id')},
                        'The owned worker restarted or changed process.')
            # RemainAfterExit retains the exact exit result until the controller
            # observes it. Only this owned, already exited worker is released.
            if (self.child is not None and status.get('MainPID') == '0' and status.get('ActiveState') == 'active' and
                    status.get('SubState') == 'exited' and self.cgroup_empty()):
                self.save('worker-exited.json', status)
                self.save('worker-release-intent.json', {'unit': UNIT, 'invocation_id': self.invocation,
                    'child': self.child, 'main_pid': 0, 'cgroup_empty': True})
                released = subprocess.run(['/usr/bin/systemctl', 'stop', UNIT], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                    timeout=20, check=False, env=self.op.ENV)
                require(released.returncode == 0, 'The exact exited worker did not release its transient unit.')
                ended = self.properties()
                require(ended.get('MainPID') == '0' and ended.get('ActiveState') in ('inactive', 'failed') and self.cgroup_empty(),
                    'The released worker is not terminal with an empty cgroup.')
                self.terminal = {**status, 'ActiveState': ended.get('ActiveState'), 'SubState': ended.get('SubState'),
                    'cgroup_empty': True, 'release_returncode': released.returncode}
                self.closed = True
                self.save('worker-terminal.json', self.terminal)
                return
            if status.get('MainPID') == '0' and status.get('ActiveState') in ('inactive', 'failed') and self.cgroup_empty():
                self.closed = True
                self.terminal = {**status, 'cgroup_empty': True}
                self.save('worker-terminal.json', self.terminal)
                require(self.child is not None, 'The worker exited before its live process identity was captured.')
                return
            time.sleep(0.5)
        raise ObservationError('The strictly bounded worker did not reach a terminal empty cgroup.')

    def launch(self):
        self.check()
        require(self.properties().get('LoadState') == 'not-found', 'The worker name became occupied before launch.')
        require(instant(self.before['database']['metadata']['captured_at']) <= dt.datetime.now(dt.timezone.utc) <=
            instant(self.before['database']['metadata']['captured_at']) + dt.timedelta(seconds=150), 'The immediate before snapshot is no longer fresh.')
        self.save('launch-intent.json', {'unit': UNIT, 'input_sha256': self.input_sha, 'node_sha256': self.args.node_sha256,
            'arguments': self.node_arguments(), 'limits': {'cpu_percent': 150, 'memory_bytes': 1073741824, 'tasks': 128, 'seconds': 360}})
        self.launched = True
        arguments = ['/usr/bin/systemd-run', '--unit=' + UNIT, '--service-type=exec', '--quiet',
            '--property=User=root', '--property=Group=root', '--property=WorkingDirectory=' + str(ROOT),
            '--property=Restart=no', '--property=RemainAfterExit=yes', '--property=CPUQuota=150%', '--property=MemoryMax=1G', '--property=TasksMax=128',
            '--property=RuntimeMaxSec=360', '--property=TimeoutStopSec=15', '--property=KillMode=control-group',
            '--property=UMask=0077', '--property=PrivateTmp=yes', '--property=NoNewPrivileges=yes',
            '--property=ProtectSystem=strict', '--property=ReadWritePaths=' + str(ROOT),
            '--property=IPAddressDeny=any', '--property=IPAddressAllow=localhost',
            '--property=UnsetEnvironment=DEBUG PWDEBUG NODE_OPTIONS NODE_PATH', '--setenv=HOME=/root', '--setenv=LANG=C.UTF-8',
            '--property=StandardOutput=append:' + str(ROOT / 'node.stdout'), '--property=StandardError=append:' + str(ROOT / 'node.stderr'),
            '--', *self.node_arguments()]
        result = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False, env=self.op.ENV)
        self.save('launch-result.json', {'returncode': result.returncode, 'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)})
        require(result.returncode == 0, 'The one worker start returned failure; its exact state must still be collected.')

    def read_browser(self, current):
        require(self.closed and self.child is not None, 'No child evidence is accepted while its process tree can still act.')
        path = BROWSER_ROOT / 'report.json'
        if path.exists():
            try:
                raw = protected(path, limit=4 << 20)
                report = decode(raw)
                validate_node_report(report, self.input, self.input_sha, self.child)
                self.node_report = report
                self.records['browser-report.json'] = {'path': str(path), 'sha256': sha(raw)}
            except Exception as error: self.error('browser_report', error)
        private_path = BROWSER_ROOT / 'session-private.json'
        if private_path.exists():
            raw = protected(private_path, limit=32768)
            private = decode(raw)
            token, row = validate_private_session(private, self.input, self.input_sha, self.child, self.before, current)
            expected = {'path': str(private_path), 'sha256': sha(raw)}
            if self.node_report is not None:
                if self.node_report.get('session_private') != expected or self.node_report.get('login_proof') != private['proof']:
                    self.error('browser_private_proof_disagreement', ObservationError())
                    self.node_report = None
            self.session = private
            if not (self.node_report and ui_logout_proven(self.node_report, private['proof'])):
                self.fallback(token, row)
        elif self.node_report and self.node_report.get('login_proof'):
            raise ObservationError('The acknowledged login lacks its private durable ownership evidence.')

    def fallback(self, token, row):
        require(self.closed and not self.fallback_attempted and self.session is not None, 'The exact fallback scope is unavailable or consumed.')
        self.check()
        require(decode(protected(ROOT / 'cleanup-reservation.json', self.records['cleanup-reservation.json']['sha256']))['input_sha256'] == self.input_sha,
            'The original conditional cleanup reservation changed.')
        self.fallback_attempted = True
        self.save('fallback-intent.json', {'token_sha256': self.session['proof']['token_sha256'], 'session_id': row['id'],
            'user_id': B, 'already_revoked': row['revoked_at'] is not None, 'ui_result_remains_failed': True})
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(ObservationError('Owned cleanup exceeded its total deadline.')))
        signal.setitimer(signal.ITIMER_REAL, 20)
        try:
            self.cleanup_result = fallback_http(token, row['revoked_at'] is not None)
            exact = bool(self.cleanup_result and self.cleanup_result[-1].get('status') == 401 and self.cleanup_result[-1].get('complete') is True)
            self.save('fallback-result.json', {'requests': self.cleanup_result, 'exact_token_rejected': exact, 'ui_result_remains_failed': True})
            require(exact and all('failure_type' not in result for result in self.cleanup_result), 'The exact fallback did not fully close its credential.')
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous)

    def collect(self):
        # Each evidence branch has an independent opportunity to complete. A bad
        # UI report cannot skip the final database or media witness.
        current = None
        try: current = self.snapshot('after-browser-full.json')
        except Exception as error: self.error('after_browser_snapshot', error)
        if current is not None:
            try: self.read_browser(current)
            except Exception as error: self.error('browser_evidence_or_owned_cleanup', error)
        try: self.after = self.snapshot('after-full.json')
        except Exception as error: self.error('final_snapshot', error)
        try:
            after_media = self.ext.media_witness(self.op, self.state)
            self.save('media-after.json', after_media)
            require(self.op.equal_json(self.media, after_media), 'A protected media byte, inode or inventory changed.')
            self.check()
        except Exception as error: self.error('private_media_and_process', error)
        if self.after is not None and self.session is not None:
            try:
                proof = self.session['proof']
                caps_path = BROWSER_ROOT / 'capabilities-private.json'
                raw = protected(caps_path, limit=2 << 20)
                self.capabilities = decode(raw)
                values = capability_values(self.capabilities, proof, self.input, self.input_sha, self.child)
                require(self.node_report is not None, 'A terminal browser report is missing.')
                successes = [entry for entry in self.capabilities['entries'] if entry['completed'] and entry['status'] == 204]
                expected = {'path': str(caps_path), 'sha256': sha(raw), 'request_count': len(self.capabilities['entries']),
                    'last_successful_body_sha256': successes[-1]['body_sha256'] if successes else None}
                require(self.node_report.get('capabilities_private') == expected, 'The complete capability journal descriptor changed.')
                self.delta = validate_delta(self.op, self.restriction, self.before, self.after, proof, self.state['server_id'], values)
                self.restriction.quiescent(self.after)
                require(not self.fallback_attempted, 'Fallback cleanup cannot promote a failed UI observation.')
                validate_observation(self.node_report, proof, self.terminal or {})
            except Exception as error: self.error('final_delta_or_ui_observation', error)
        elif self.after is not None:
            try: self.restriction.compare_fixed_snapshot(self.op, self.before, self.after)
            except Exception as error: self.error('unacknowledged_state_delta', error)

    def execute(self):
        try:
            self.load()
            if self.args.check_only:
                return {'marker': MARKER, 'result': 'checked', 'output_created': False, 'http_requests': 0,
                    'candidate': PROCESS, 'baseline_sha256': AUTHORITY['current_snapshot']['sha256'],
                    'client_acceptance': False, 'permission_ui_acceptance': False}
            self.prepare()
            self.phase = 'browser'
            try: self.launch()
            except Exception as error: self.error('worker_launch', error)
            if self.launched:
                try: self.await_worker()
                except Exception as error: self.error('worker_lifecycle', error)
            self.phase = 'collection'
            self.collect()
            passed = not self.errors and self.delta is not None and self.closed and not self.fallback_attempted
            report = {'marker': MARKER, 'version': 1, 'result': 'passed' if passed else 'failed', 'phase': 'complete',
                'outcome': 'baseline_observation' if passed else 'not_observed' if (self.node_report or {}).get('outcome') == 'not_observed' else 'failed',
                'browser_outcome': (self.node_report or {}).get('outcome'),
                'client_acceptance': False, 'permission_ui_acceptance': False,
                'boundary': 'B-only original-client Home/Views and one reload. No administrator, policy, playback or scan action.',
                'candidate': self.input['candidate'], 'input_sha256': self.input_sha, 'source_closure': self.sources,
                'authority': AUTHORITY, 'browser_process': self.child, 'worker_closed': self.closed,
                'fallback_attempted': self.fallback_attempted, 'proof': self.delta, 'errors': self.errors, 'evidence': dict(self.records)}
            self.save('report.json', report)
            return report
        except Exception as error:
            self.error(self.phase, error)
            if self.root_fd is not None and 'report.json' not in self.records:
                try: self.save('failure.json', {'marker': MARKER, 'result': 'failed', 'phase': self.phase, 'errors': self.errors,
                    'worker_started': self.launched, 'worker_closed': self.closed, 'evidence': dict(self.records),
                    'client_acceptance': False, 'permission_ui_acceptance': False})
                except Exception: pass
            raise ObservationError('The bounded observation failed; retain its private evidence and do not retry.') from None
        finally:
            if self.session is not None: self.session['token'] = None
            if self.root_fd is not None: os.close(self.root_fd)
            if self.lock is not None: os.close(self.lock)


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--driver', required=True, type=Path)
    parser.add_argument('--source-closure', required=True, type=Path)
    parser.add_argument('--source-closure-sha256', required=True)
    parser.add_argument('--node', required=True, type=Path)
    parser.add_argument('--node-sha256', required=True)
    parser.add_argument('--check-only', action='store_true')
    value = parser.parse_args(argv)
    require(all(HASH.fullmatch(getattr(value, key)) for key in ('script_sha256', 'source_closure_sha256', 'node_sha256')) and
        value.node.is_absolute() and value.driver.is_absolute() and value.source_closure.is_absolute(), 'Explicit argument pins are incomplete.')
    return value


if __name__ == '__main__':
    try:
        result = Run(arguments()).execute()
        print(exact({'marker': result['marker'], 'result': result['result'], 'client_acceptance': False,
            'permission_ui_acceptance': False, 'report': str(ROOT / 'report.json') if result['result'] != 'checked' else None}))
        sys.exit(0 if result['result'] in ('passed', 'checked') else 1)
    except Exception as failure:
        print(exact({'marker': MARKER, 'result': 'failed', 'failure_type': type(failure).__name__,
            'client_acceptance': False, 'permission_ui_acceptance': False}), file=sys.stderr)
        sys.exit(1)
