#!/usr/bin/env python3
"""Observe one source55 native Name edit through an unchanged original client.

This fresh candidate-only controller owns one B UI login and one native login,
two exact metadata PUT reservations, bounded conditional restoration, complete
schema28 preservation ledgers and the independent browser worker lifetime.
It never imports a historical observer/controller as executable authority.
Check-only mode performs no HTTP, evidence publication, or service mutation.
"""

from __future__ import annotations

import sys
sys.dont_write_bytecode = True

import argparse
from collections import Counter
import copy
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
import time
import types
import unicodedata


WORK = Path('/opt/goby-test/exec-work-m3e')
TOOL = WORK / 'client-library-changed-source55-tool-04'
ROOT = WORK / 'client-library-changed-ui-source55-v4'
BROWSER_ROOT = ROOT / 'browser'
WORKER_UNIT = 'goby-client-library-changed-ui-source55-v4.service'
CONTROLLER_UNIT = 'goby-client-library-changed-ui-source55-controller-v4.service'
CGROUP = '/system.slice/' + WORKER_UNIT
MARKER = 'goby-client-library-changed-observation-v1'
INPUT_MARKER = 'goby-client-library-changed-input-v1'
MODE = 'b-movies-name-automatic-refresh'
BASE_URL, DIRECT_URL = 'http://127.0.0.1:18196', 'http://127.0.0.1:18198'
B = 'ecbbe4cb82403879bc4b4f78894c5738'
ADMIN = '0dd576d477e8acea871cb4b06cb11153'
ITEM = '268051d3ca734aefcf94e245fb25ad55'
LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790'
SERVER = 'c7cfd76b1dee728b2bad523793a37ccb'
STATE = WORK / 'client-fixture.json'
SOURCE = WORK / 'source-attempt-55'
MANIFEST_SHA = '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a'
CATALOG = SOURCE / 'internal/backuppg/catalogs/schema-28-postgresql-17.json'
CATALOG_SHA = '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b'
UPGRADE_TOOL = WORK / 'client-schema28-source55-tool-05'
UPGRADE_MARKER = 'goby-client-schema28-source55-upgrade-v1'
BOOT = '6bdfc486-7bc8-412f-82b5-70095a09dde7'
PREVIOUS_PROCESS = {'pid': 1264063, 'start_ticks': 11104222, 'boot_id': BOOT}
PREVIOUS_BINARY_SHA = 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d'
PREVIOUS_STATE_SHA = 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4'
PREVIOUS_RUNTIME_SHA = 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df'
PREVIOUS_INVOCATION = 'c0a5244ae25646c8bd92c3e3c1636575'
CREDENTIALS = WORK / 'browser.json'
CREDENTIALS_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790'
VIEWER_FILE = ROOT / 'viewer-credentials.json'
UPGRADE_AUTHORITY_KEYS = frozenset(('upgrade_intent', 'upgrade_report', 'upgrade_attestation', 'current_snapshot'))
PRIOR_ROOT = WORK / 'client-library-changed-ui-source55-v2'
PRIOR_TOOL = WORK / 'client-library-changed-source55-tool-02'
PRIOR_WORKER = 'goby-client-library-changed-ui-source55-v2.service'
PRIOR_CONTROLLER = 'goby-client-library-changed-ui-source55-controller-v2.service'
PRIOR_KEYS = frozenset(('prior_input', 'prior_browser_report', 'prior_controller_report', 'prior_terminal',
                        'prior_before_snapshot', 'prior_after_snapshot'))
AUTHORITY_KEYS = UPGRADE_AUTHORITY_KEYS | {'history'}
HISTORY_FIELDS = {'version', 'input', 'browser_report', 'controller_report', 'terminal', 'before_snapshot', 'after_snapshot'}
HISTORY_NAMES = {'input': 'prior_input', 'browser_report': 'prior_browser_report', 'controller_report': 'prior_controller_report',
                 'terminal': 'prior_terminal', 'before_snapshot': 'prior_before_snapshot', 'after_snapshot': 'prior_after_snapshot'}
PRIOR_PINS = {
    'prior_input': {'path': str(PRIOR_ROOT / 'input.json'), 'sha256': 'c88794dbd9bdedcb6c16fea4dd8a7d8af2d09012728084b30e24701db5872c73'},
    'prior_browser_report': {'path': str(PRIOR_ROOT / 'browser/report.json'), 'sha256': 'c980edd73bbf08c6190391f2c5368abd7973c5c1381bcec70d7be93477f8c743'},
    'prior_controller_report': {'path': str(PRIOR_ROOT / 'report.json'), 'sha256': '467f473ef20b1b05bfc76c863b41f69eeb77f1f2a783f57d6549beaa46f28104'},
    'prior_before_snapshot': {'path': str(PRIOR_ROOT / 'before-full.json'), 'sha256': '36ff8634f85a58841c1c6e4558de5e4dcfea1a842bae8e40a26c1d37c12ff32f'},
    'prior_after_snapshot': {'path': str(PRIOR_ROOT / 'after-full.json'), 'sha256': 'a13f976b7097e33337527ef2cf10ad9203d755e0fd2cec43307edfa6efbfe8bc'},
    'prior_terminal': {'path': str(WORK / 'client-library-changed-source55-execution-02/failed-terminal.json'),
                       'sha256': 'b23a1a156e781c771e3bb4b1ba31bfb048e29e77504569d129397c79445265d6'},
}
PRIOR_INDEPENDENT = {'path': str(WORK / 'client-library-changed-source55-execution-02/independent-after-full.json'),
                     'sha256': '6dfe6cbbf2288b21a66466719067b2c0d063bb588e693e06479d20da4feb9cd4'}
PRIOR_SEAL_RECORDS = {
    'scope_files': {'path': str(WORK / 'client-library-changed-source55-execution-02/failed-scope-files.json'),
                    'sha256': '3ef622b6f1724c996177ab7a2673b34c9d16a41608957f8546c0ac24d2085eaf'},
    'seal_script': {'path': str(WORK / 'client-library-changed-source55-execution-02/seal-failed-terminal.py'),
                    'sha256': '3f9bcdbf83c62b5e5cd5ead3fc77d9398b9754ca255ca3e97298cf79fb8d2c8c'},
    'controller_source': {'path': str(PRIOR_TOOL / 'observe-client-library-changed-source55.py'),
                    'sha256': '57fe02b0fc32d335bb08a89e3f5c31c9121a7af44997c31e1329761b82701f25'},
}
HISTORY_V3_PINS = {
    'input': {'path': str(WORK / 'client-library-changed-ui-source55-v3/input.json'),
              'sha256': 'eab8c899d06b525271afec345d4137b3e0adec70bb5e8ecf911fdf4dae7e9f6e'},
    'browser_report': {'path': str(WORK / 'client-library-changed-ui-source55-v3/browser/report.json'),
              'sha256': '9360e3b5c60b16f714b80b2da3f1f7a31d58313e196abee99dfbb4f8e3bc9d1f'},
    'controller_report': {'path': str(WORK / 'client-library-changed-ui-source55-v3/report.json'),
              'sha256': 'e9b931c568c98e4438917d8c204922267b0931a2de4c9f6bba40f2155c196fb4'},
    'before_snapshot': {'path': str(WORK / 'client-library-changed-ui-source55-v3/before-full.json'),
              'sha256': '10563f9b12e62a321bbda67c49bfbfc1a6e3d2c2e304d3c2e1e7d64502b2c897'},
    'after_snapshot': {'path': str(WORK / 'client-library-changed-ui-source55-v3/after-full.json'),
              'sha256': 'a84e5e480a70b7d84e49001aaae791a522cd38c6b1055dcb503f0312f933c2dd'},
    'terminal': {'path': str(WORK / 'client-library-changed-source55-execution-03/failed-terminal.json'),
              'sha256': 'e53e9777337c8c0b33a0cc86dad2e22c79d5e7f2601aa64dd68c6ed91cd9ecf1'},
}
HISTORY_V3_INDEPENDENT = {'path': str(WORK / 'client-library-changed-source55-execution-03/independent-after-full.json'),
                         'sha256': '668975cd51c66888fb363ba273fe15dec70646d87e9549adf0fbe79c20b4f3f0'}
HISTORY_V3_SEAL_RECORDS = {
    'scope_files': {'path': str(WORK / 'client-library-changed-source55-execution-03/failed-scope-files.json'),
                    'sha256': 'a27ba50b166ef0752e466960d4eab94c457e699228daf65eeed366305b5a287f'},
    'seal_script': {'path': str(WORK / 'client-library-changed-source55-execution-03/seal-failed-terminal.py'),
                    'sha256': '525caf668f68d365599b067d07d8f5e635df1bd011359d8c807014697e87c41d'},
    'controller_source': {'path': str(WORK / 'client-library-changed-source55-tool-03/observe-client-library-changed-source55.py'),
                    'sha256': '68753cc567ceb138b1a2cfd693020b42556448cc5da0821c4ead1f8cf93700f2'},
}
HISTORY_V3_PREDECESSORS = {
    'predecessor_terminals': [
        {'path': str(WORK / 'client-library-changed-source55-execution-01/failed-terminal.json'),
         'sha256': 'ecdc5fdf48bf7d471676e6786d52ed61ce1be3053537ce36fde5d44a3d1552a6'}, PRIOR_PINS['prior_terminal']],
    'predecessor_inventories': [
        {'path': str(WORK / 'client-library-changed-source55-execution-01/failed-scope-files.json'),
         'sha256': '62cc954df4141a3ddb3faf9e99772a9055f7c5f864f68ee1d30728ae00144872'}, PRIOR_SEAL_RECORDS['scope_files']],
    'discovery_failure': {'path': str(WORK / 'client-library-changed-ui-source55-v3/browser/discovery-failure.json'),
         'sha256': '23b42c459572f25fd17f18b86eacc3b75c7553020521df6ea0ab6ec43bfb609f'},
}
FIXTURE = {
    'profile_receipt': (WORK / 'client-special-features-protocol-finalization-v1/completed.json', 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36'),
    'profile_report': (WORK / 'client-special-features-protocol-finalization-v1/report.json', '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d'),
    'profile_inspection': (WORK / 'client-special-features-finalization-inspection-v1/report.json', 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e'),
    'music_chain': (WORK / 'client-music-upgrade-chain-source32.json', 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a'),
    'music_scan_receipt': (WORK / 'client-music-scan-v1/receipt.json', 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b'),
}
HELPERS = {
    'op': (WORK / 'prepare-client-fixture.py', '84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c'),
    'profile_reader': (WORK / 'client-special-features-profile-tool-01/client-special-features-profile.py', '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252'),
    'media_reader': (WORK / 'client-extra-root-tool-02/extend-client-special-features-root.py', '7a9cda4a36ebbe9c2311031db1f6a57265eef8fc8807057a3d650ef6c16b2a46'),
}
JS_NAMES = ('client-browser-library-changed-source55.mjs', 'client-library-changed-source55-fixture.mjs',
    'client-browser-library-home.mjs', 'client-browser-cross-user.mjs', 'client-browser-session-proof.mjs',
    'client-browser-goby-fixture.mjs', 'client-browser-special-features-fixture.mjs')
PRIMARY_UNIT = 'goby-foundation-test.service'
PRIMARY_PROCESS = {'pid': 762090, 'start_ticks': 7637121, 'boot_id': BOOT}
PRIMARY_INVOCATION = 'bb94d74b475f4382a6ec6f6df181dd74'
PRIMARY_BINARY_SHA = 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620'
PRIMARY_FILES = {
    '/opt/goby-dev/goby': PRIMARY_BINARY_SHA,
    '/etc/systemd/system/goby-foundation-test.service': '91a9b0e55baf053db25281236d0d7c8c055f895b093d4474d9c27e888610117d',
    '/etc/systemd/system/goby-foundation-test.service.d/20-application-keys.conf': 'ed9e8b91416918ce797ab3b5507d8843f62109d033cb30a679a44e3abbea3dd9',
    '/etc/systemd/system/goby-foundation-test.service.d/30-observability.conf': 'cc8fecb588840dd49848352936747a712d8790f8f7bd25d8d31a8a41069a46e5',
    '/etc/systemd/system/goby-foundation-test.service.d/40-backup-recovery.conf': 'e62ad56f0e93c1df2b89a6b5f7dbbcb032fa98d9d4cdcb8ebb04ba1e01b5682e',
    '/opt/goby-test/runtime.env': '2043e72115338d04775485dd63702c6084d36d09331ca5cdac66819152619607',
    '/opt/goby-test/recovery-m5j.env': '6847d34cfd9af7c53a8b16406db6f341f418b6ffdacb83bfe8c1f9bfd36b4c7c',
}
HASH, ID = re.compile('[0-9a-f]{64}'), re.compile('[0-9a-f]{32}')
MAX_JSON, MAX_IPC, MAX_REPORT = 64 << 20, 512 << 10, 4 << 20
FIELDS = ('Name', 'SortName', 'Overview', 'OriginalTitle', 'OfficialRating', 'ProductionYear',
    'PremiereDate', 'CommunityRating', 'ProviderIds', 'Genres', 'Tags', 'Studios', 'People', 'IndexNumber', 'ParentIndexNumber')
ACTIVE_FIELDS = FIELDS[:-2]
ENTITY_FIELDS = ('Genres', 'Tags', 'Studios', 'People', 'Artists', 'AlbumArtists')
STAGES = ('discovery', 'armed', 'restore-armed', 'restored')
CONTROLS = ('reserved', 'forward', 'restored', 'close')
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8'}


class ObservationError(Exception):
    """A bounded observation, ownership or exact restoration guard failed."""


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
        value = {}
        for key, item in pairs:
            require(key not in value, 'A JSON object repeats a field.')
            value[key] = item
        return value
    return json.loads(raw, object_pairs_hook=unique, parse_float=Decimal,
        parse_constant=lambda _: (_ for _ in ()).throw(ObservationError('A JSON number is nonfinite.')))


def same(left, right):
    return canonical(left) == canonical(right)


def instant(value):
    require(isinstance(value, str), 'A timestamp is missing.')
    result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.tzinfo is not None and result.utcoffset() == dt.timedelta(0), 'A timestamp is not UTC.')
    return result


def revision(value):
    require(isinstance(value, str) and re.fullmatch('[1-9][0-9]{0,18}', value) and int(value) <= 9223372036854775807,
            'A metadata revision is not a bounded canonical string.')
    return int(value)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def protected(path, expected=None, *, modes=(0o600,), limit=MAX_JSON, workspace=True):
    require(path.is_absolute() and '..' not in path.parts and (not workspace or path.is_relative_to(WORK)),
            'An input path escaped its declared root.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                'An input ancestor is not protected and root owned.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in modes and before.st_size <= limit, 'An input file changed ownership or bounds.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(identity(os.fstat(handle.fileno())) == identity(before), 'An input changed before opening.')
        raw = handle.read(limit + 1)
        require(identity(os.fstat(handle.fileno())) == identity(before), 'An input changed during reading.')
    require(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and
            (expected is None or sha(raw) == expected), 'An input failed its complete byte/identity pin.')
    return raw


def read_record(record, limit=MAX_JSON):
    require(isinstance(record, dict) and set(record) == {'path', 'sha256'} and HASH.fullmatch(record.get('sha256', '')),
            'An artifact descriptor is malformed.')
    return decode(protected(Path(record['path']), record['sha256'], limit=limit))


def proc_identity(pid):
    require(type(pid) is int and pid > 1, 'A process identity is invalid.')
    raw = (Path('/proc') / str(pid) / 'stat').read_text()
    fields = raw[raw.rfind(')') + 2:].split()
    return {'pid': pid, 'start_ticks': fields[19], 'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}


def load_reader(path, expected, name):
    raw = protected(path, expected, modes=(0o600, 0o644), limit=2 << 20)
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module


def compare_fixed_snapshot(before, after):
    require(set(before) == set(after), 'The complete snapshot shape changed.')
    for key in before:
        if key != 'database':
            require(same(before[key], after[key]), 'A private, recovery or schema snapshot value changed.')
    left, right = before['database'], after['database']
    require(set(left) == set(right), 'The complete database snapshot shape changed.')
    for key in left:
        if key != 'metadata':
            require(same(left[key], right[key]), 'A complete database/catalog/sequence value changed.')
    require(set(left['metadata']) == set(right['metadata']) and
            instant(left['metadata']['captured_at']) <= instant(right['metadata']['captured_at']) and
            all(same(value, right['metadata'][key]) for key, value in left['metadata'].items() if key != 'captured_at'),
            'A database identity or capture order changed.')


def required_digest(value):
    return isinstance(value, str) and HASH.fullmatch(value) is not None and len(set(value)) > 1


def artifact_descriptor(value):
    require(isinstance(value, dict) and set(value) == {'path', 'sha256'} and required_digest(value['sha256']) and
            isinstance(value['path'], str) and Path(value['path']).is_absolute() and Path(value['path']).is_relative_to(WORK) and
            '..' not in Path(value['path']).parts and str(Path(value['path'])) == value['path'], 'An authority descriptor is not exact and owned.')
    return value


def history_scope(version):
    require(type(version) is int and version in (2, 3), 'Only the two sealed predecessor scopes may be read.')
    if version == 2:
        return {'root': PRIOR_ROOT, 'tool': PRIOR_TOOL, 'worker': PRIOR_WORKER, 'controller': PRIOR_CONTROLLER,
                'pins': {name: PRIOR_PINS[key] for name, key in HISTORY_NAMES.items()}}
    return {'root': WORK / 'client-library-changed-ui-source55-v3', 'tool': WORK / 'client-library-changed-source55-tool-03',
            'worker': 'goby-client-library-changed-ui-source55-v3.service',
            'controller': 'goby-client-library-changed-ui-source55-controller-v3.service', 'pins': HISTORY_V3_PINS}


def validate_history_entry(entry, version):
    require(isinstance(entry, dict) and set(entry) == HISTORY_FIELDS and type(entry['version']) is int and entry['version'] == version,
            'The sealed history order or exact entry shape changed.')
    pins = history_scope(version)['pins']
    require(set(pins) == set(HISTORY_NAMES), 'The predecessor has no complete independently sealed input pins.')
    for name in HISTORY_NAMES:
        artifact_descriptor(entry[name])
        require(same(entry[name], pins[name]), 'A predecessor descriptor differs from its exact sealed scope.')


def history_entry_authority(authority, entry):
    return {**{key: authority[key] for key in UPGRADE_AUTHORITY_KEYS},
            **{target: entry[name] for name, target in HISTORY_NAMES.items()}}


def history_seal(version):
    history_scope(version)
    if version == 2:
        return {'independent': PRIOR_INDEPENDENT, 'records': PRIOR_SEAL_RECORDS,
                'controller_invocation': 'd6376750d8ff4fc08c97c69ac993f1e6', 'worker_invocation': '40884584c2784e3da2a91f816f563b45'}
    return {'independent': HISTORY_V3_INDEPENDENT, 'records': HISTORY_V3_SEAL_RECORDS,
            'controller_invocation': 'bb72fff9517e4f41baa01fbd3e86bf40', 'worker_invocation': 'e1296ab3a25a41d6964e577831df4152'}


def validate_authority_input(value):
    require(isinstance(value, dict) and set(value) == {'marker', 'version', 'candidate', 'authority'} and
            value['marker'] == 'goby-client-library-changed-source55-authority-v1' and type(value['version']) is int and value['version'] == 1,
            'The source55 authority input has another shape or version.')
    candidate = value['candidate']
    require(isinstance(candidate, dict) and set(candidate) == {'binary_sha256', 'process', 'runtime_sha256', 'state_sha256',
            'server_id', 'base_url', 'direct_url', 'source', 'source_manifest_sha256', 'invocation_id', 'publication'},
            'The future candidate requires every exact source, process, invocation and publication pin.')
    require(all(required_digest(candidate[key]) for key in ('binary_sha256', 'runtime_sha256', 'state_sha256')) and
            candidate['binary_sha256'] != PREVIOUS_BINARY_SHA and candidate['runtime_sha256'] != PREVIOUS_RUNTIME_SHA and
            candidate['state_sha256'] != PREVIOUS_STATE_SHA and candidate['server_id'] == SERVER and
            candidate['base_url'] == BASE_URL and candidate['direct_url'] == DIRECT_URL and candidate['source'] == str(SOURCE) and
            candidate['source_manifest_sha256'] == MANIFEST_SHA and isinstance(candidate['invocation_id'], str) and
            ID.fullmatch(candidate['invocation_id']) and candidate['invocation_id'] != PREVIOUS_INVOCATION and
            isinstance(candidate['publication'], str) and re.fullmatch('[0-9a-f]{40}', candidate['publication']) and
            len(set(candidate['publication'])) > 1, 'The candidate reuses old authority or lacks exact future pins.')
    process = candidate['process']
    require(isinstance(process, dict) and set(process) == {'pid', 'start_ticks', 'boot_id'} and
            type(process['pid']) is int and process['pid'] > 1 and type(process['start_ticks']) is int and process['start_ticks'] > 0 and
            process['boot_id'] == BOOT and process['pid'] not in (PREVIOUS_PROCESS['pid'], PRIMARY_PROCESS['pid']) and
            not same(process, PREVIOUS_PROCESS), 'The candidate process is not an independent new source55 process.')
    authority = value['authority']
    require(isinstance(authority, dict) and set(authority) == AUTHORITY_KEYS, 'Only the exact upgrade authority chain may admit this run.')
    for key in UPGRADE_AUTHORITY_KEYS:
        artifact_descriptor(authority[key])
    require(isinstance(authority['history'], list) and len(authority['history']) == 2,
            'Exactly the ordered sealed v2 and v3 history is required.')
    for entry, version in zip(authority['history'], (2, 3)):
        validate_history_entry(entry, version)
    require(authority['upgrade_intent']['path'] == str(UPGRADE_TOOL / 'intent.json'), 'The upgrade intent is outside its frozen tool.')
    output = Path(authority['upgrade_report']['path']).parent
    require(output.parent == WORK and re.fullmatch('client-schema28-source55-upgrade-[0-9]{8}_[0-9]{6}_[0-9a-f]{12}', output.name) and
            all(authority[key]['path'] == str(output / filename) for key, filename in
                (('upgrade_report', 'report.json'), ('upgrade_attestation', 'attestation.json'), ('current_snapshot', 'after-full.json'))),
            'The authority inputs are not the three exact files under one bounded upgrade output.')
    return copy.deepcopy(candidate), copy.deepcopy(authority)


def validate_upgrade_authority(candidate, authority, intent, report, attestation, state):
    run = intent.get('run_id')
    require(isinstance(run, str) and re.fullmatch('[0-9]{8}_[0-9]{6}_[0-9a-f]{12}', run) and
            intent.get('marker') == UPGRADE_MARKER and type(intent.get('version')) is int and intent['version'] == 1 and
            intent.get('tool') == str(UPGRADE_TOOL), 'The upgrade intent is not the exact source55 deployment.')
    output = WORK / ('client-schema28-source55-upgrade-' + run)
    require(intent.get('output') == str(output) and all(authority[key]['path'] == str(output / filename) for key, filename in
            (('upgrade_report', 'report.json'), ('upgrade_attestation', 'attestation.json'), ('current_snapshot', 'after-full.json'))),
            'The upgrade report, attestation and current snapshot do not share the intent output.')
    source, binary = intent.get('source', {}), report.get('binary', {})
    require(source.get('root') == str(SOURCE) and source.get('manifest_sha256') == MANIFEST_SHA and
            same(source.get('binary'), binary) and binary.get('sha256') == candidate['binary_sha256'] and
            type(binary.get('bytes')) is int and 1 << 20 < binary['bytes'] <= 128 << 20,
            'The accepted upgrade source and replacement binary differ.')
    for document in (report, attestation):
        require(document.get('marker') == UPGRADE_MARKER and type(document.get('version')) is int and document['version'] == 1 and
                document.get('run_id') == run and document.get('intent_sha256') == authority['upgrade_intent']['sha256'] and
                type(document.get('schema')) is int and document['schema'] == 28 and
                document.get('state_sha256') == candidate['state_sha256'] and same(document.get('binary'), binary) and
                document.get('client_acceptance') is False, 'An upgrade result lacks the exact candidate and intent binding.')
    require(report.get('status') == 'awaiting_outer_attestation' and attestation.get('status') == 'passed' and
            attestation.get('report_sha256') == authority['upgrade_report']['sha256'] and
            attestation.get('recursive_cgroup_empty') is True and report.get('publication') == candidate['publication'] and
            same(report.get('new_process'), candidate['process']) and report.get('service_actions') == ['stop', 'start'] and
            type(report.get('http_requests')) is int and report['http_requests'] == 5 and
            same(report.get('evidence', {}).get('after-full.json'), authority['current_snapshot']) and
            all(report.get(key) is True for key in ('preserved_rows_sequences_credentials_recovery', 'primary_unchanged',
                'shared_web_unchanged', 'rehearsal_removed', 'hba_restored_exactly')) and
            all(report.get(key) is False for key in ('automatic_retry', 'automatic_rollback')) and
            all(attestation.get(key) is True for key in ('rows_sequences_credentials_recovery_preserved', 'primary_unchanged',
                'rehearsal_removed', 'hba_restored_exactly')), 'The report is provisional or its independent attestation does not prove success.')
    service, controller, terminal = report.get('candidate_service', {}), report.get('controller', {}), attestation.get('controller', {})
    require(service.get('MainPID') == str(candidate['process']['pid']) and service.get('InvocationID') == candidate['invocation_id'] and
            service.get('ActiveState') == 'active' and service.get('SubState') == 'running' and
            controller.get('unit') == intent.get('controller', {}).get('unit') and ID.fullmatch(controller.get('invocation_id', '')) and
            set(terminal) == {'MainPID', 'InvocationID', 'ActiveState', 'SubState', 'Result', 'ExecMainStatus', 'ControlGroup', 'Description', 'RemainAfterExit'} and
            terminal.get('MainPID') == '0' and terminal.get('InvocationID') == controller['invocation_id'] and
            terminal.get('Result') == 'success' and terminal.get('ExecMainStatus') == '0' and
            terminal.get('ActiveState') == 'active' and terminal.get('SubState') == 'exited' and terminal.get('RemainAfterExit') == 'yes' and
            terminal.get('Description') == UPGRADE_MARKER + ':' + run and terminal.get('ControlGroup') in ('', '/system.slice/' + controller['unit']),
            'The independent upgrade controller terminal or live candidate invocation differs.')
    upgrade = state.get('schema28_upgrade', {})
    require(state.get('marker') == 'goby-m3e-client-acceptance-v1' and state.get('work') == str(WORK) and
            type(state.get('schema')) is int and state['schema'] == 28 and state.get('phase') == 'ready' and state.get('stage') == 'complete' and
            same(state.get('process'), candidate['process']) and state.get('binary_sha256') == candidate['binary_sha256'] and
            state.get('runtime_sha256') == candidate['runtime_sha256'] and state.get('server_id') == SERVER and
            state.get('browser_sha256') == CREDENTIALS_SHA and state.get('viewer_id') == B and state.get('admin_id') == ADMIN and
            upgrade.get('marker') == UPGRADE_MARKER and upgrade.get('phase') == 'complete' and upgrade.get('from_schema') == 27 and
            upgrade.get('to_schema') == 28 and upgrade.get('output') == str(output) and
            upgrade.get('intent_sha256') == authority['upgrade_intent']['sha256'] and upgrade.get('to_sha256') == candidate['binary_sha256'] and
            upgrade.get('publication') == candidate['publication'] and same(upgrade.get('new_process'), candidate['process']) and
            same(upgrade.get('service'), service) and same(state.get('upgrade'), upgrade) and
            isinstance(state.get('upgrade_history'), list) and state['upgrade_history'] and same(state['upgrade_history'][-1], upgrade),
            'The current state is not the exact independently attested schema28 upgrade.')
    require(same(state.get('schema28_source'), {'marker': 'goby-client-schema28-source-v1', 'schema': 28, 'source': str(SOURCE),
            'source_manifest_sha256': MANIFEST_SHA, 'catalog_sha256': CATALOG_SHA, 'publication': candidate['publication']}),
            'The schema28 source receipt differs from the pinned catalog and publication.')


def validate_historical_state(previous, current):
    allowed = {'schema', 'phase', 'stage', 'process', 'binary_sha256', 'binary_identity', 'runtime_sha256', 'upgrade', 'upgrade_history'}
    require(previous.get('schema') == 27 and previous.get('binary_sha256') == PREVIOUS_BINARY_SHA and
            previous.get('runtime_sha256') == PREVIOUS_RUNTIME_SHA and same(previous.get('process'), PREVIOUS_PROCESS) and
            set(current) == set(previous) | {'schema28_upgrade', 'schema28_source'} and
            all(same(value, current[key]) for key, value in previous.items() if key not in allowed) and
            same(current.get('upgrade_history'), previous.get('upgrade_history', []) + [current['schema28_upgrade']]),
            'The upgrade state rewrote an unrelated historical, credential or fixture authority field.')


def validate_schema28_catalog(catalog):
    require(isinstance(catalog, dict) and set(catalog) == {'version', 'postgresql_major', 'migrations', 'catalog', 'objects'} and
            type(catalog['version']) is int and catalog['version'] == 28 and catalog['postgresql_major'] == 17 and
            isinstance(catalog['migrations'], list) and len(catalog['migrations']) == 28 and
            [row.get('version') for row in catalog['migrations']] == list(range(1, 29)) and
            all(set(row) == {'version', 'name', 'sha256'} and required_digest(row['sha256']) for row in catalog['migrations']) and
            isinstance(catalog['objects'], list), 'The exact schema28 catalog identity is malformed.')
    shape = catalog['catalog']
    require(isinstance(shape, dict) and set(shape) == {'Schema', 'Tables', 'Sequences', 'SHA256', 'Constraints'} and shape['Schema'] == '' and
            len(shape['Tables']) == 35 and len(shape['Sequences']) == 5 and
            sha(canonical(catalog['objects'])) == shape['SHA256'], 'The trusted schema28 catalog fingerprint or inventory differs.')
    columns = {}
    for table in shape['Tables']:
        name = table['Name']
        rows = sorted((row for row in catalog['objects'] if row['kind'] == 'column' and row['name'].startswith(name + '.') and
                       row['value'].get('dropped') is False), key=lambda row: row['name'])
        values = [row['value']['name'] for row in rows]
        require(name not in columns and values and len(values) == len(set(values)) and set(table['Columns']) <= set(values),
                'A trusted schema28 table lacks its full generated and stored column inventory.')
        columns[name] = values
    require(columns['library_roots'][-4:] == ['binding_revision', 'storage_binding', 'bound_at', 'bound_by'] and
            columns['activity_entries'][-2:] == ['previous_revision', 'observation_fingerprint'], 'The schema28 extension columns differ.')
    return columns


def validate_schema28_snapshot(snapshot, state, catalog, op):
    columns = validate_schema28_catalog(catalog)
    require(isinstance(snapshot, dict) and type(snapshot.get('schema')) is int and snapshot['schema'] == 28 and
            snapshot.get('runtime_sha256') == state['runtime_sha256'] and snapshot.get('browser_sha256') == state['browser_sha256'],
            'The complete schema28 snapshot lost its exact private identity.')
    database, shape = snapshot['database'], catalog['catalog']
    require(set(database) == {'metadata', 'catalog', 'unsupported', 'tables', 'sequences'} and database['unsupported'] is False and
            same(database['catalog'], catalog['objects']) and set(database['tables']) == set(columns) and
            set(database['sequences']) == {row['Name'] for row in shape['Sequences']}, 'The complete schema28 database inventory differs.')
    metadata = database['metadata']
    require(set(metadata) == {'captured_at', 'server_version_num', 'database', 'schemas', 'public_schema', 'columns', 'relations'} and
            metadata['database'] == 'goby_client_m3e' and type(metadata['server_version_num']) is int and metadata['server_version_num'] // 10000 == 17 and
            metadata['schemas'] == ['public'] and same(metadata['columns'], columns), 'The schema28 full metadata identity differs.')
    instant(metadata['captured_at'])
    relations = {row['name'] for row in catalog['objects'] if row['kind'] == 'relation'}
    require(set(metadata['relations']) == relations and all(value.get('owner') == 'goby_client_m3e' for value in metadata['relations'].values()),
            'The schema28 relation ownership or membership differs.')
    for name, rows in database['tables'].items():
        require(isinstance(rows, list) and all(isinstance(row, dict) and set(row) == set(columns[name]) for row in rows),
                'A complete schema28 row omitted or added a column: ' + name)
    history = sorted(database['tables']['schema_migrations'], key=lambda row: row['version'])
    require(same([{key: row[key] for key in ('version', 'name')} for row in history],
                 [{key: row[key] for key in ('version', 'name')} for row in catalog['migrations']]), 'The schema28 migration history differs.')
    require(all(isinstance(value, dict) and set(value) == {'last_value', 'is_called'} and type(value['last_value']) is int and
                type(value['is_called']) is bool for value in database['sequences'].values()), 'A schema28 sequence lacks exact integer and boolean facts.')
    tables = database['tables']
    expected_users = {state['admin_id'], state['viewer_id'], state['added_viewer']['user_id']}
    users = {row['id']: row for row in tables['users']}
    require(len(users) == len(tables['users']) == 3 and set(users) == expected_users and
            users[state['admin_id']]['is_administrator'] is True and users[state['admin_id']]['is_disabled'] is False and
            all(users[user]['is_administrator'] is False and users[user]['is_disabled'] is False for user in (state['viewer_id'], state['added_viewer']['user_id'])) and
            any(row['key'] == 'server_id' and row['value'] == state['server_id'] for row in tables['server_settings']),
            'The schema28 snapshot changed the three users or stable server identity.')
    op.validate_theme_state(tables)
    for definition in shape['Sequences']:
        value = database['sequences'][definition['Name']]
        require(definition['MinValue'] <= value['last_value'] <= definition['MaxValue'] and type(definition['Increment']) is int and
                definition['Increment'] > 0 and definition['Consumers'], 'A schema28 sequence lost its trusted allocation bounds.')
        next_id = value['last_value'] + (definition['Increment'] if value['is_called'] else 0)
        require(next_id <= definition['MaxValue'] and all(all(type(row[consumer['Column']]) is int and row[consumer['Column']] < next_id
                for row in tables[consumer['Table']]) for consumer in definition['Consumers']),
                'A schema28 sequence can collide with a preserved consumer identity.')
    require(same(snapshot.get('added_viewer_credentials'), op.added_viewer_credentials(state)),
            'The additional-account private receipt or credentials changed.')
    require(all(type(row['binding_revision']) is int and row['binding_revision'] == 1 and row['storage_binding'] is None and
                row['bound_at'] is None and row['bound_by'] is None for row in database['tables']['library_roots']),
            'This Name observation cannot approve, rebind or change any library root.')
    require(all(type(row['previous_revision']) is int and row['previous_revision'] == 0 and row['observation_fingerprint'] == ''
                for row in database['tables']['activity_entries']), 'A non-root audit acquired schema28 binding facts.')


def quiescent(snapshot):
    tables = snapshot['database']['tables']
    require(type(snapshot['schema']) is int and snapshot['schema'] == 28 and len(tables) == 35 and not tables['client_playback_references'] and not tables['encoding_jobs'],
            'The candidate lacks its idle complete schema28 profile.')
    for table, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
        require(not any(str(row[field]).lower() in ('waiting', 'pending', 'queued', 'running', 'stopping') for row in tables[table]),
                'A scan or task is active.')
    sessions = {row['id']: row for row in tables['sessions']}
    for row in tables['play_sessions']:
        require(row['state'] in ('Prepared', 'Stopped', 'Expired'), 'A playback row is active or unknown.')
        if row['state'] == 'Prepared':
            owner = sessions.get(row['auth_session_id'], {})
            require(row['started_at'] is None and row['counted'] is False and row['application_client_id'] is None and
                    owner.get('kind') == 'emby' and owner.get('user_id') == row['user_id'] and owner.get('revoked_at') is not None,
                    'Prepared history is not bound to an already revoked ordinary session.')


def metadata_value_projection(automatic, overrides, locked):
    require(isinstance(automatic, dict) and isinstance(overrides, dict) and isinstance(locked, dict), 'Metadata layers are not objects.')
    value = copy.deepcopy(automatic)
    for layer in (locked, overrides):
        for key, item in layer.items():
            require(key in FIELDS, 'A metadata control has an unsupported field.')
            if key in ACTIVE_FIELDS:
                value['ProviderIDs' if key == 'ProviderIds' else key] = copy.deepcopy(item)
    result = {}
    for key in FIELDS:
        item = value.get('ProviderIDs' if key == 'ProviderIds' else key)
        if key in ('Name', 'SortName', 'Overview', 'OriginalTitle', 'OfficialRating'):
            result[key] = '' if item is None else item
            require(isinstance(result[key], str), 'A metadata text value is not a string.')
        elif key in ('Genres', 'Tags', 'Studios', 'People'):
            result[key] = [] if item is None else copy.deepcopy(item)
            require(isinstance(result[key], list), 'A metadata collection is not an array.')
        elif key == 'ProviderIds':
            result[key] = {} if item is None else copy.deepcopy(item)
            require(isinstance(result[key], dict), 'Provider metadata is not an object.')
        else:
            result[key] = copy.deepcopy(item)
    return result


def metadata_db_projection(item, overrides, locked, public_effective):
    source = item['local_metadata']
    controls = {key for layer in (locked, overrides) for key in layer if key in ACTIVE_FIELDS}
    if source is None and not controls:
        return None
    require(source is None or isinstance(source, dict), 'The sparse local metadata is not an object or null.')
    result = copy.deepcopy(source) if source is not None else {}
    for key in controls:
        internal = 'ProviderIDs' if key == 'ProviderIds' else key
        result[internal] = copy.deepcopy(public_effective[key])
        if key == 'ProviderIds':
            result.pop('ProviderIds', None)
    return result


def target_profile(snapshot):
    quiescent(snapshot)
    tables = snapshot['database']['tables']
    items = [row for row in tables['items'] if row['id'] == ITEM]
    metadata = [row for row in tables['item_metadata_state'] if row['item_id'] == ITEM]
    require(len(items) == len(metadata) == 1, 'The original Movie or its existing metadata row is not unique.')
    item, row = items[0], metadata[0]
    require(item['type'] == 'Movie' and item['is_folder'] is False and item['library_id'] == LIBRARY and
            ID.fullmatch(item['parent_id']) and ID.fullmatch(item['root_id']) and isinstance(item['name'], str) and
            0 < len(item['name'].encode()) <= 220 and '\x00' not in item['name'] and
            not any(unicodedata.category(char) == 'Cc' for char in item['name']), 'The target is not the exact ordinary Movie.')
    require(type(row['revision']) is int and 1 <= row['revision'] <= 9223372036854775805 and
            isinstance(row['overrides'], dict) and isinstance(row['locked_values'], dict) and 'Name' not in row['locked_values'],
            'The target revision or Name lock is outside this scope.')
    require(not any(value.get('item_id') == ITEM for value in tables['item_entities']) and
            all(value.get('owner_item_id') != ITEM and value.get('resource_item_id') != ITEM
                for name in ('item_theme_resources', 'item_extra_resources') for value in tables[name]),
            'The ordinary target has an entity, theme or extra role.')
    public = metadata_value_projection(row['automatic'], row['overrides'], row['locked_values'])
    projection = metadata_db_projection(item, row['overrides'], row['locked_values'], public)
    require(same(projection, row['effective']) and public['Name'] == item['name'] and public['SortName'] == item['sort_name'] and
            public['Overview'] == item['overview'], 'The stored target does not match its exact source/controls projection.')
    for layer in (item['local_metadata'], row['automatic'], row['effective'], row['music_source']):
        require(layer is None or isinstance(layer, dict), 'An entity-bearing source layer changed shape.')
        for key in ENTITY_FIELDS:
            require((layer or {}).get(key) in (None, []), 'Name-only editing could consume an entity sequence.')
    require(all(public[key] == [] for key in ENTITY_FIELDS[:4]), 'The effective target has entity-bearing collections.')
    parent = [value for value in tables['items'] if value['id'] == item['parent_id']]
    require(len(parent) == 1, 'The target parent is not uniquely receipted.')
    return {'item': copy.deepcopy(item), 'metadata': copy.deepcopy(row), 'parent_name': parent[0]['name'],
            'public': public, 'target': {'id': ITEM, 'library_id': LIBRARY, 'name': item['name'], 'type': 'Movie',
                'parent_id': item['parent_id'], 'root_id': item['root_id'], 'relative_path': item['relative_path']}}


def validate_metadata_document(document, profile):
    require(isinstance(document, dict) and set(document) == {'Item', 'Revision', 'Automatic', 'Effective', 'Overrides',
        'LockedValues', 'LockedFields', 'EditableFields', 'InactiveFields', 'LastEditedBy', 'LastEditedAt'},
        'A native metadata document has another complete DTO shape.')
    revision(document['Revision'])
    item, stored = profile['item'], profile['metadata']
    expected_item = {'Id': ITEM, 'LibraryId': LIBRARY, 'ParentId': item['parent_id'], 'ParentName': profile['parent_name'],
                     'Name': document['Effective'].get('Name'), 'Type': 'Movie', 'Path': item['path'], 'IsFolder': False}
    require(same(document['Item'], expected_item) and isinstance(document['Overrides'], dict) and
            isinstance(document['LockedValues'], dict) and document['LockedFields'] == sorted(document['LockedValues']) and
            document['EditableFields'] == list(ACTIVE_FIELDS) and document['InactiveFields'] ==
                [key for key in FIELDS if key not in ACTIVE_FIELDS and (key in document['Overrides'] or key in document['LockedValues'])],
            'Native target identity or editable/lock controls changed.')
    require(same(document['Automatic'], metadata_value_projection(stored['automatic'], {}, {})) and
            same(document['Effective'], metadata_value_projection(stored['automatic'], document['Overrides'], document['LockedValues'])) and
            isinstance(document['LastEditedBy'], str), 'Native automatic/effective metadata does not match its complete layers.')
    if document['LastEditedAt'] is not None:
        instant(document['LastEditedAt'])
    return document


def reserve_edit(original, profile):
    validate_metadata_document(original, profile)
    row = profile['metadata']
    require(original['Revision'] == str(row['revision']) and same(original['Overrides'], row['overrides']) and
            same(original['LockedValues'], row['locked_values']) and original['LockedFields'] == sorted(row['locked_values']) and
            same(original['Effective'], profile['public']) and original['LastEditedBy'] == (row['last_edited_by'] or '') and
            (original['LastEditedAt'] is None if row['last_edited_at'] is None else instant(original['LastEditedAt']) == instant(row['last_edited_at'])),
            'The first public metadata read is not the complete preserved baseline.')
    require(revision(original['Revision']) <= 9223372036854775805, 'The baseline has no room for exactly two revisions.')
    marker = original['Effective']['Name'] + ' [LC source55 v1]'
    require(len(marker.encode()) <= 256 and marker != original['Effective']['Name'], 'The bounded marker name is invalid.')
    overrides = copy.deepcopy(original['Overrides'])
    overrides['Name'] = marker
    forward = {'Revision': original['Revision'], 'Overrides': overrides, 'LockedFields': copy.deepcopy(original['LockedFields'])}
    restore = {'Revision': str(revision(original['Revision']) + 1), 'Overrides': copy.deepcopy(original['Overrides']),
               'LockedFields': copy.deepcopy(original['LockedFields'])}
    public = {'target_id': ITEM, 'library_id': LIBRARY, 'revision': original['Revision'],
        'original_name': original['Effective']['Name'], 'marker_name': marker,
        'original_controls_sha256': sha(canonical({'Overrides': original['Overrides'], 'LockedFields': original['LockedFields'],
                                                 'LockedValues': original['LockedValues']})),
        'forward_body_sha256': sha(canonical(forward)), 'restore_body_sha256': sha(canonical(restore))}
    return {'original': copy.deepcopy(original), 'forward_body': forward, 'restore_body': restore,
            'marker_name': marker, 'public': public}


def validate_login(proof, expected_server=SERVER):
    keys = {'token_sha256', 'session_id', 'user_id', 'device_id', 'server_id', 'client_name', 'device_name', 'client_version',
            'created_at', 'created_at_source', 'kind', 'slot', 'frame_login_finished', 'physical_login_completed', 'request_metadata_matches'}
    require(isinstance(proof, dict) and set(proof) == keys and HASH.fullmatch(proof.get('token_sha256', '')) and
            ID.fullmatch(proof.get('session_id', '')) and proof.get('user_id') == B and proof.get('server_id') == expected_server and
            proof.get('kind') == 'emby' and proof.get('slot') == 'B' and proof.get('created_at_source') == 'SessionInfo.LastActivityDate' and
            all(proof.get(key) is True for key in ('frame_login_finished', 'physical_login_completed', 'request_metadata_matches')),
            'B lacks one complete frame/physical login proof.')
    for key in ('device_id', 'client_name', 'device_name', 'client_version'):
        require(isinstance(proof[key], str) and 0 < len(proof[key].encode()) <= 256 and '\x00' not in proof[key],
                'A browser login identity field is missing or invalid.')
    instant(proof['created_at'])


def owned_session(before, after, proof, expected_server=SERVER):
    validate_login(proof, expected_server)
    old = before['database']['tables']['sessions']
    require(all(row['id'] != proof['session_id'] and row['token_hash'] != '\\x' + proof['token_sha256'] for row in old),
            'The browser token or session belongs to an old authentication.')
    rows = [row for row in after['database']['tables']['sessions']
            if row['id'] == proof['session_id'] or row['token_hash'] == '\\x' + proof['token_sha256']]
    require(len(rows) == 1 and rows[0]['id'] == proof['session_id'] and rows[0]['token_hash'] == '\\x' + proof['token_sha256'] and
            rows[0]['user_id'] == B and rows[0]['kind'] == 'emby' and rows[0]['device_id'] == proof['device_id'] and
            all(rows[0][key] == proof[key] for key in ('client_name', 'device_name', 'client_version')) and
            instant(rows[0]['created_at']) == instant(proof['created_at']), 'The browser proof is not one new owned session row.')
    require(instant(before['database']['metadata']['captured_at']) <= instant(rows[0]['created_at']) <=
            instant(after['database']['metadata']['captured_at']), 'The new browser issuance is outside this exact capture interval.')
    return rows[0]


def validate_private_session(private, input_record, input_sha, child, before, after):
    require(isinstance(private, dict) and set(private) == {'marker', 'version', 'input_sha256', 'source_closure_sha256',
        'node_process', 'controller', 'proof', 'token'} and private.get('marker') == 'goby-client-library-home-session-v1' and
        type(private.get('version')) is int and private['version'] == 1 and private.get('input_sha256') == input_sha and
        private.get('source_closure_sha256') == sha(canonical(input_record['source_closure'])) and
        same(private.get('controller'), input_record['controller']) and same(private.get('node_process'), child),
        'A browser cleanup credential is not bound to this exact new process and input.')
    token = private.get('token')
    require(isinstance(token, str) and re.fullmatch('[A-Za-z0-9_-]{43}', token) and
            sha(token.encode()) == private['proof']['token_sha256'], 'The retained B token does not match its login fingerprint.')
    return token, owned_session(before, after, private['proof'], input_record['candidate']['server_id'])


def owned_metadata_audit(snapshot, session_id, number):
    rows = [row for row in snapshot['database']['tables']['activity_entries']
            if row['action'] == 'metadata.updated' and row['resource_kind'] == 'item' and row['resource_id'] == ITEM and row['revision'] == number]
    return len(rows) == 1 and type(rows[0].get('revision')) is int and type(rows[0].get('affected_count')) is int and all(same(rows[0].get(key), value) for key, value in {
        'severity': 'Info', 'source': 'native', 'actor_kind': 'user', 'actor_id': ADMIN, 'actor_credential_id': session_id,
        'request_id': '', 'affected_count': 0, 'state': '', 'changed_fields': ['Name', 'Overrides'],
        'previous_revision': 0, 'observation_fingerprint': ''}.items()) and type(rows[0].get('previous_revision')) is int


def target_checkpoint(snapshot, profile, reservation, phase, admin_session_id):
    require(phase in ('original', 'forward', 'restored'), 'A target checkpoint phase is unknown.')
    tables = snapshot['database']['tables']
    item = next((row for row in tables['items'] if row['id'] == ITEM), None)
    row = next((row for row in tables['item_metadata_state'] if row['item_id'] == ITEM), None)
    require(item is not None and row is not None, 'The target disappeared during metadata reconciliation.')
    expected_item, expected_row = copy.deepcopy(profile['item']), copy.deepcopy(profile['metadata'])
    steps = {'original': 0, 'forward': 1, 'restored': 2}[phase]
    if steps:
        require(reservation is not None and admin_session_id is not None, 'An edited checkpoint lacks its owned reservation.')
        overrides = reservation['forward_body']['Overrides'] if phase == 'forward' else reservation['original']['Overrides']
        public = metadata_value_projection(expected_row['automatic'], overrides, expected_row['locked_values'])
        expected_row.update(overrides=copy.deepcopy(overrides), effective=metadata_db_projection(expected_item, overrides,
            expected_row['locked_values'], public), revision=expected_row['revision'] + steps, last_edited_by=ADMIN,
            last_edited_at=row['last_edited_at'], updated_at=row['updated_at'])
        expected_item.update(name=public['Name'], sort_name=public['SortName'], updated_at=item['updated_at'])
        for value in (row['last_edited_at'], row['updated_at'], item['updated_at']):
            require(instant(profile['metadata']['updated_at']) <= instant(value) <= instant(snapshot['database']['metadata']['captured_at']),
                    'An owned metadata timestamp escaped the observed checkpoint.')
        for number in range(profile['metadata']['revision'] + 1, expected_row['revision'] + 1):
            require(owned_metadata_audit(snapshot, admin_session_id, number), 'A metadata revision lacks its exact new credential audit.')
    require(same(item, expected_item) and same(row, expected_row), 'The complete target row or sparse metadata differs from its owned phase.')
    if phase == 'restored':
        require(same(row['effective'], profile['metadata']['effective']) and same(row['overrides'], profile['metadata']['overrides']),
                'The exact original sparse projection or control layer was not restored.')
    return item, row


def classify_metadata(document, snapshot, profile, reservation, admin_session_id):
    validate_metadata_document(document, profile)
    start = profile['metadata']['revision']
    number = revision(document['Revision'])
    phase = {start: 'original', start + 1: 'forward', start + 2: 'restored'}.get(number, 'foreign')
    if phase == 'foreign':
        return phase
    try:
        item, row = target_checkpoint(snapshot, profile, reservation, phase, admin_session_id)
        require(document['Item']['Name'] == item['name'] and same(document['Overrides'], row['overrides']) and
                same(document['LockedValues'], row['locked_values']) and document['LockedFields'] == sorted(row['locked_values']) and
                document['LastEditedBy'] == (row['last_edited_by'] or '') and
                (document['LastEditedAt'] is None if row['last_edited_at'] is None else instant(document['LastEditedAt']) == instant(row['last_edited_at'])),
                'The public read and owned target ledger disagree.')
    except ObservationError:
        return 'foreign'
    return phase


def restore_decision(forward_sent, forward_ack, restore_sent, classification, owned_forward, owned_restore):
    if not forward_sent:
        return 'not_required'
    if classification == 'restored' and owned_forward and owned_restore:
        return 'restored'
    if classification == 'forward' and owned_forward and not owned_restore and not restore_sent:
        return 'restore_once'
    # Neither the HTTP socket timeout nor the 20-second owned transaction
    # context bounds the earlier body/authentication/mutex wait. R is unresolved.
    if classification == 'original' and not owned_forward and not owned_restore:
        return 'unresolved'
    return 'conflict'


def validate_ledger(before, after, profile, reservation=None, browser=None, administrator=None,
                    phase='original', capabilities=None, authenticated=None, closed=False):
    require(set(before) == set(after), 'The complete preservation snapshot shape changed.')
    for key in before:
        if key != 'database':
            require(same(before[key], after[key]), 'A private, recovery or schema binding changed.')
    left, right = before['database'], after['database']
    require(set(left) == set(right) and set(left['tables']) == set(right['tables']) and len(right['tables']) == 35,
            'The complete database or table inventory changed.')
    for key in left:
        if key not in ('tables', 'sequences', 'metadata'):
            require(same(left[key], right[key]), 'The schema catalog or unsupported-object inventory changed.')
    require(set(left['metadata']) == set(right['metadata']) and all(same(value, right['metadata'][key])
        for key, value in left['metadata'].items() if key != 'captured_at'), 'A database/catalog identity or column definition changed.')
    start, end = instant(left['metadata']['captured_at']), instant(right['metadata']['captured_at'])
    require(start <= end, 'The complete snapshot clock moved backwards.')
    additions = {}
    for name, rows in left['tables'].items():
        current = right['tables'][name]
        columns = left['metadata']['columns'][name]
        require(isinstance(rows, list) and isinstance(current, list) and
                all(isinstance(row, dict) and set(row) == set(columns) for row in rows + current),
                'A complete table row differs from its exact source55 column inventory: ' + name)
        if name in ('items', 'item_metadata_state'):
            key = 'id' if name == 'items' else 'item_id'
            require(len(rows) == len(current) and same([row for row in rows if row[key] != ITEM],
                [row for row in current if row[key] != ITEM]), 'An unrelated item or metadata row changed.')
            continue
        old, new = Counter(canonical(row) for row in rows), Counter(canonical(row) for row in current)
        require(not old - new, 'An old complete business, authentication, device or audit row changed: ' + name)
        extra = new - old
        additions[name] = [row for row in current if extra[canonical(row)] > 0]
        require(len({canonical(row) for row in additions[name]}) == len(additions[name]), 'A new row is duplicated.')
        if name not in ('sessions', 'devices', 'activity_entries'):
            require(not additions[name], 'An unapproved table received a new row: ' + name)
    steps = {'original': 0, 'forward': 1, 'restored': 2}[phase]
    count = int(browser is not None) + int(administrator is not None)
    require(len(additions['sessions']) == count and len(additions['devices']) == int(browser is not None),
            'The new authentication/device population is outside its exact owned logins.')
    expected_audits, owned = [], []
    if browser is not None:
        owned.append(owned_session(before, after, browser))
    if administrator is not None:
        native = [row for row in additions['sessions'] if row['id'] == administrator['id'] and row['token_hash'] == administrator['token_hash']]
        require(len(native) == 1 and native[0]['user_id'] == ADMIN and native[0]['kind'] == 'admin' and
                native[0]['client_capabilities'] == {} and native[0]['device_registry_id'] is None,
                'The native credential is not the independently proven new administrator session.')
        owned.append(native[0])
    require({row['id'] for row in owned} == {row['id'] for row in additions['sessions']}, 'A new session is not attributable to this run.')
    for row in owned:
        ordinary = row['kind'] == 'emby'
        require(start <= instant(row['created_at']) <= instant(row['last_seen_at']) <= end and
                instant(row['expires_at']) == instant(row['created_at']) + dt.timedelta(days=30 if ordinary else 1),
                'An owned session issuance, Touch or expiry is outside its exact lifetime.')
        if closed:
            require(row['revoked_at'] is not None, 'An owned session remains active after cleanup.')
        if row['revoked_at'] is not None:
            require(instant(row['created_at']) <= instant(row['revoked_at']) <= end, 'A revocation is outside this run.')
        if ordinary and capabilities is not None:
            require(any(same(row['client_capabilities'], value) for value in capabilities), 'B capabilities lack an exact observed successful request.')
        if authenticated is not None:
            initial = next(value for value in authenticated['database']['tables']['sessions'] if value['id'] == row['id'])
            allowed = ('last_seen_at', 'revoked_at', 'client_capabilities') if ordinary else ('revoked_at',)
            require(all(same(value, row[key]) for key, value in initial.items() if key not in allowed),
                    'An owned session changed outside its proven Touch/capability/logout fields.')
        for action in ('session.login',) + (('session.revoked',) if row['revoked_at'] is not None else ()):
            expected_audits.append((action, 'emby' if ordinary else 'native', row['user_id'], row['id'], 'session', row['id'], 0, 1, ()))
    if browser is not None:
        ordinary = next(row for row in owned if row['kind'] == 'emby')
        device = additions['devices'][0]
        require(device['id'] == ordinary['device_registry_id'] and device['reported_device_id'] == browser['device_id'] and
            all(row['reported_device_id'] != browser['device_id'] for row in left['tables']['devices']) and
            device['reported_name'] == browser['device_name'] and device['app_name'] == browser['client_name'] and
            device['app_version'] == browser['client_version'] and device['last_user_id'] == B and device['ip_address'] == '127.0.0.1' and
            device['custom_name'] is None and device['deleted_at'] is None and type(device['revision']) is int and device['revision'] == 1 and
            start <= instant(device['created_at']) <= instant(ordinary['created_at']) and instant(device['created_at']) <= instant(device['last_seen_at']) <= end,
            'The one new device differs from B\'s exact fresh browser identity.')
        if authenticated is not None:
            initial = next(row for row in authenticated['database']['tables']['devices'] if row['id'] == device['id'])
            require(all(same(value, device[key]) for key, value in initial.items() if key != 'last_seen_at'),
                    'The new browser device changed outside its observed Touch timestamp.')
    for number in range(profile['metadata']['revision'] + 1, profile['metadata']['revision'] + steps + 1):
        require(administrator is not None, 'An edit has no owned native credential.')
        expected_audits.append(('metadata.updated', 'native', ADMIN, administrator['id'], 'item', ITEM, number, 0, ('Name', 'Overrides')))
    audits = additions['activity_entries']
    require(all(type(row.get(key)) is int for row in audits for key in ('id', 'revision', 'affected_count', 'previous_revision')) and
            all(type(row.get('id')) is int for row in additions['devices']), 'A new integer column has another JSON type.')
    keys = ('action', 'source', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id', 'revision', 'affected_count')
    require(Counter(tuple(row[key] for key in keys) + (tuple(row['changed_fields']),) for row in audits) == Counter(expected_audits) and
            all(row['severity'] == 'Info' and row['actor_kind'] == 'user' and row['state'] == row['request_id'] == row['observation_fingerprint'] == '' and
                row['previous_revision'] == 0 and
                start <= instant(row['created_at']) <= end for row in audits),
            'The audit delta is not the exact owned login, metadata and revocation sequence.')
    require(set(left['sequences']) == set(right['sequences']), 'The sequence inventory changed.')
    for name, initial in left['sequences'].items():
        require(all(isinstance(value, dict) and set(value) == {'last_value', 'is_called'} and
                    type(value['last_value']) is int and type(value['is_called']) is bool
                    for value in (initial, right['sequences'][name])), 'A sequence has another exact scalar shape.')
        table = {'devices_id_seq': 'devices', 'activity_entries_id_seq': 'activity_entries'}.get(name)
        added = additions[table] if table else []
        if added:
            first = initial['last_value'] + int(initial['is_called'])
            require(same(right['sequences'][name], {'last_value': first + len(added) - 1, 'is_called': True}) and
                    sorted(row['id'] for row in added) == list(range(first, first + len(added))),
                    'A sequence increment lacks its exact contiguous new rows.')
        else:
            require(same(initial, right['sequences'][name]), 'An unrelated or unused sequence changed.')
    target_checkpoint(after, profile, reservation, phase, administrator['id'] if administrator else None)
    if steps:
        target = next(row for row in right['tables']['items'] if row['id'] == ITEM)
        metadata = next(row for row in right['tables']['item_metadata_state'] if row['item_id'] == ITEM)
        require(all(start <= instant(value) <= end for value in
                    (target['updated_at'], metadata['updated_at'], metadata['last_edited_at'])),
                'An edited timestamp predates this fresh scope.')
    quiescent(after)
    return {'new_sessions': count, 'new_devices': int(browser is not None), 'new_audits': len(audits),
            'metadata_revision_delta': steps, 'old_rows_sequences_private_preserved': True,
            'owned_sessions_closed': all(row['revoked_at'] is not None for row in owned)}

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
        document.get('marker') == 'goby-client-library-home-capabilities-v1' and type(document.get('version')) is int and document['version'] == 1 and
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


def numeric(value):
    return type(value) is int or isinstance(value, Decimal) and value.is_finite()


def safe_text(value, maximum=256):
    return isinstance(value, str) and 0 < len(value.encode('utf-8')) <= maximum and not re.search('[\x00-\x1f\x7f]', value)


def publication_state(final, pending):
    for info in (final, pending):
        if info is not None:
            require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
                0 <= info.st_size <= MAX_IPC and info.st_nlink in (1, 2), 'An IPC publication has unsafe ownership, size or links.')
    if final is None and pending is None:
        return 'missing'
    if final is None:
        require(pending.st_nlink == 1, 'An unpublished IPC temporary has another link.')
        return 'publishing'
    if pending is None:
        require(final.st_nlink == 1, 'A ready IPC record has an unexplained extra link.')
        return 'ready'
    require((final.st_dev, final.st_ino) == (pending.st_dev, pending.st_ino) and final.st_nlink == pending.st_nlink == 2,
            'A pending IPC name does not identify its exact final hard link.')
    return 'publishing'


def dom_identity(value, target, name, route=None, document=None, passed=True):
    require(isinstance(value, dict) and value.get('target_id') == target['id'] and value.get('expected_name') == name and
        value.get('media_inactive') is True and safe_text(value.get('route'), 4096) and safe_text(value.get('document_id'), 80) and
        (route is None or value['route'] == route) and (document is None or value['document_id'] == document),
        'A DOM sample lacks its exact passive target, route or document binding.')
    if passed:
        require(value.get('passed') is True and value.get('identity_proven') is True and
            type(value.get('visible_target_cards')) is int and value['visible_target_cards'] == 1 and
            type(value.get('target_title_count')) is int and value['target_title_count'] >= 1 and
            type(value.get('forbidden_title_count')) is int and value['forbidden_title_count'] == 0,
            'The visible target card is ambiguous or retains the forbidden title.')


def catalog_shape(value):
    require(isinstance(value, dict) and value.get('kind') in ('items', 'target', 'collection-folder') and isinstance(value.get('query'), list) and
        len(value['query']) <= 64 and len(canonical(value['query'])) <= 4096 and
        all(isinstance(pair, list) and len(pair) == 2 and all(isinstance(word, str) for word in pair) and
            0 < len(pair[0]) <= 128 and len(pair[1]) <= 4096 for pair in value['query']), 'A public catalog query has another bounded shape.')
    query = {}
    for key, word in value['query']:
        lower = key.lower()
        require(lower not in query and not re.search('password|credential|csrf', lower) and
            lower not in ('api_key', 'x-emby-token', 'x-mediabrowser-token', 'token', 'access_token', 'authorization') and not lower.startswith('x-emby-'),
            'A public catalog query repeats a key or exposes a credential carrier.')
        query[lower] = word
    routes = ('/Users/' + B + '/Items', '/Items')
    target = LIBRARY if value['kind'] == 'collection-folder' else ITEM
    require(query.get('userid', B) == B and query.get('ids', target) == target and
        ('limit' not in query or re.fullmatch(r'(?:[1-9][0-9]?|1[01][0-9]|12[0-8])', query['limit'])) and
        (value['kind'] in ('target', 'collection-folder') and value.get('route') in tuple(route + '/' + target for route in routes) or
         value['kind'] == 'items' and value.get('route') in routes and
         (query.get('parentid') == LIBRARY and query.get('ids', ITEM) == ITEM or query.get('ids') == ITEM and query.get('parentid', LIBRARY) == LIBRARY)) and
        value.get('shape_sha256') == sha(canonical([value['route'], value['query']])),
        'A public catalog request is outside the one target and original Movies library.')


def pair_reads(physical, frames):
    require(isinstance(physical, list) and isinstance(frames, list) and len(physical) <= 64 and len(frames) <= 128 and
        all(type(value.get('id')) is int and value['id'] > 0 for value in physical) and
        all(type(value.get('index')) is int and value['index'] >= 0 for value in frames) and
        len({value['id'] for value in physical}) == len(physical) and len({value['index'] for value in frames}) == len(frames),
        'A physical or browser transfer inventory is repeated or outside its bound.')
    eligible = [frame for frame in frames if all(isinstance(frame.get(key), str) and HASH.fullmatch(frame[key])
        for key in ('token_sha256', 'request_sha256', 'shape_sha256')) and frame.get('finished') is True and
        frame.get('failed') is False and type(frame.get('status')) is int and frame['status'] == 200 and
        frame.get('content_type') == 'application/json' and frame.get('from_service_worker') is False and
        (frame.get('source') == 'page' and frame.get('main_frame') is True or frame.get('source') == 'worker' and
         frame.get('main_frame') is False and safe_text(frame.get('worker_id'), 80))]
    for transfer in physical:
        catalog_shape(transfer)
        projection = transfer.get('projection')
        if projection is not None:
            collection_folder = transfer['kind'] == 'collection-folder'
            field = 'collection_folder' if collection_folder else 'target'
            require(isinstance(projection, dict) and set(projection) == {field, 'count', 'body_sha256', 'body_bytes'} and
                type(projection['count']) is int and 1 <= projection['count'] <= 128 and type(projection['body_bytes']) is int and
                0 < projection['body_bytes'] <= (2 << 20) and isinstance(projection['body_sha256'], str) and HASH.fullmatch(projection['body_sha256']) and
                isinstance(projection[field], dict) and set(projection[field]) == ({'Id', 'Name', 'Type', 'Subviews'} if collection_folder else {'Id', 'Name', 'Type'}) and
                projection[field]['Id'] == (LIBRARY if collection_folder else ITEM) and
                projection[field]['Type'] == ('CollectionFolder' if collection_folder else 'Movie') and safe_text(projection[field]['Name']) and
                (not collection_folder or projection['count'] == 1 and projection[field]['Name'] == 'M3e Client Movies' and
                 same(projection[field]['Subviews'], ['movies', 'movies', 'folders'])),
                'A successful physical response lacks its exact bounded public target projection.')
    def fits(frame, transfer):
        times = [frame.get('request_elapsed_ms'), frame.get('finished_elapsed_ms'),
                 transfer.get('request_elapsed_ms'), transfer.get('finished_elapsed_ms')]
        return all(numeric(value) and value >= 0 for value in times) and transfer.get('completed') is True and \
            type(transfer.get('status')) is int and type(transfer.get('terminal_status')) is int and \
            transfer['status'] == transfer['terminal_status'] == 200 and isinstance(transfer.get('projection'), dict) and \
            all(frame[key] == transfer.get(key) for key in ('token_sha256', 'request_sha256', 'shape_sha256')) and \
            times[0] <= times[2] <= times[3] <= times[1] + 1000 and times[2] <= times[1]
    result = []
    for frame in eligible:
        matches = [value for value in physical if fits(frame, value)]
        match = matches[0] if len(matches) == 1 else None
        unique = match is not None and sum(fits(value, match) for value in eligible) == 1
        result.append({'frame': frame, 'physical': match if unique else None, 'unambiguous': unique, 'complete': unique})
    return result


def navigation_evidence(discovery, input_record, token):
    reads = discovery.get('collection_folder_reads')
    proof = discovery.get('collection_folder')
    navigation = discovery.get('navigation')
    require(isinstance(navigation, dict) and set(navigation) == {'before_route', 'after_route', 'before_sequence'} and
            safe_text(navigation['before_route'], 4096) and safe_text(navigation['after_route'], 4096) and
            navigation['before_route'] != navigation['after_route'] and navigation['after_route'] == discovery['dom'].get('route') and
            type(navigation['before_sequence']) is int and navigation['before_sequence'] >= 0,
            'CollectionFolder navigation lacks its actual click boundary and route transition.')
    require(isinstance(reads, list) and 0 < len(reads) <= 8 and isinstance(proof, dict) and
            set(proof) == {'id', 'type', 'subviews', 'physical_exchange_id', 'frame_request_index', 'passed'},
            'The exact CollectionFolder navigation evidence is absent.')
    require(all(isinstance(pair, dict) and set(pair) == {'frame', 'physical', 'complete', 'unambiguous'} and
                isinstance(pair['physical'], dict) and isinstance(pair['frame'], dict) for pair in reads),
            'The CollectionFolder transfer list is malformed.')
    paired = pair_reads([pair['physical'] for pair in reads], [pair['frame'] for pair in reads])
    require(same(paired, reads), 'The CollectionFolder transfers are ambiguous as a complete list.')
    for pair in reads:
        require(isinstance(pair, dict) and set(pair) == {'frame', 'physical', 'complete', 'unambiguous'} and
                isinstance(pair['physical'], dict) and same(pair_reads([pair['physical']], [pair['frame']]), [pair]) and
                pair['complete'] is True and pair['unambiguous'] is True, 'The CollectionFolder read is not an actual uniquely paired completed transfer.')
        physical, frame = pair['physical'], pair['frame']
        require(physical['kind'] == frame.get('kind') == 'collection-folder' and physical.get('method') == 'GET' and
                physical.get('terminal') == 'completed' and frame.get('route') == physical['route'] and
                frame.get('token_sha256') == token and physical.get('token_sha256') == token and
                type(frame.get('request_sequence')) is int and type(physical.get('request_sequence')) is int and
                min(frame['request_sequence'], physical['request_sequence']) > navigation['before_sequence'] and
                frame.get('page_route') in (navigation['before_route'], navigation['after_route']) and
                frame.get('document_id') == discovery['dom'].get('document_id'),
                'The CollectionFolder detail was not read in the same B document and route.')
    selected = min(reads, key=lambda pair: pair['frame']['finished_elapsed_ms'])
    expected = {'id': LIBRARY, 'type': 'CollectionFolder', 'subviews': ['movies', 'movies', 'folders'],
                'physical_exchange_id': selected['physical']['id'], 'frame_request_index': selected['frame']['index'], 'passed': True}
    require(same(proof, expected) and input_record['target']['library_id'] == LIBRARY,
            'The CollectionFolder proof differs from its exact public response.')
    return expected


def validate_boundary(boundary, name, token):
    require(isinstance(boundary, dict) and boundary.get('name') == name and
        all(safe_text(boundary.get(key), 4096 if key == 'route' else 80) for key in ('route', 'document_id', 'connection_id')) and
        boundary.get('token_sha256') == token and isinstance(token, str) and HASH.fullmatch(token) and
        type(boundary.get('started_sequence')) is int and boundary['started_sequence'] >= 0 and
        numeric(boundary.get('started_elapsed_ms')) and boundary['started_elapsed_ms'] >= 0 and
        type(boundary.get('websocket_seen')) is int and type(boundary.get('websocket_opened')) is int and
        1 <= boundary['websocket_opened'] <= boundary['websocket_seen'] <= 2, 'An observation boundary is not bound to one open socket.')


def window_evidence(window, input_record, reservation):
    require(isinstance(window, dict) and window.get('name') in ('forward', 'restored') and
            isinstance(window.get('control_sha256'), str) and HASH.fullmatch(window['control_sha256']), 'A window has no exact control receipt.')
    name, boundary = window['name'], window['boundary']
    expected = reservation['marker_name'] if name == 'forward' else reservation['original_name']
    require(reservation['target_id'] == input_record['target']['id'] == ITEM and reservation['library_id'] == LIBRARY and
        reservation['original_name'] == input_record['target']['name'], 'A window reservation targets another item.')
    validate_boundary(boundary, name, boundary.get('token_sha256'))
    b = boundary
    require(all(numeric(b.get(key)) and b[key] >= 0 for key in ('response_completed_elapsed_ms', 'end_elapsed_ms', 'completed_elapsed_ms')) and
        type(b.get('duration_ms')) is int and b['duration_ms'] == 120000 and b['response_completed_elapsed_ms'] >= b['started_elapsed_ms'] - 1000 and
        b['end_elapsed_ms'] == b['response_completed_elapsed_ms'] + 120000 and
        b['end_elapsed_ms'] <= b['completed_elapsed_ms'] <= b['end_elapsed_ms'] + 5000, 'A window does not cover the complete acknowledged interval.')
    commit = window['commit']
    require(isinstance(commit, dict) and set(commit) == {'revision', 'write_completed_at', 'native_result_sha256', 'readback_sha256'} and
        revision(commit['revision']) == revision(reservation['revision']) + (1 if name == 'forward' else 2) and
        all(isinstance(commit[key], str) and HASH.fullmatch(commit[key]) for key in ('native_result_sha256', 'readback_sha256')),
        'A window commit differs from its exact native write and complete readback.')
    instant(commit['write_completed_at'])
    require(isinstance(window['events'], dict) and set(window['events']) == {'physical', 'browser'} and
        all(isinstance(window['events'][key], list) and len(window['events'][key]) <= 64 for key in ('physical', 'browser')) and
        isinstance(window['http'], dict) and len(window['http']['physical']) <= 20 and len(window['http']['frames']) <= 40 and
        isinstance(window['dom'], list) and len(window['dom']) <= 600 and window.get('actions') == [] and window.get('lifecycle') == [],
        'The armed window was intervened in or exceeded its evidence bound.')
    def inside(value):
        return type(value.get('sequence')) is int and value['sequence'] > b['started_sequence'] and \
            numeric(value.get('elapsed_ms')) and b['started_elapsed_ms'] <= value['elapsed_ms'] <= b['end_elapsed_ms']
    arrays = ('ItemsAdded', 'ItemsRemoved', 'ItemsUpdated', 'FoldersAddedTo', 'FoldersRemovedFrom', 'CollectionFolders')
    for event in window['events']['physical'] + window['events']['browser']:
        message = event.get('message', {})
        require(inside(event) and event.get('connection_id') == b['connection_id'] and event.get('token_sha256') == b['token_sha256'] and
            event.get('complete') is True and event.get('received') is True and message.get('MessageType') == 'LibraryChanged' and
            safe_text(message.get('MessageId')) and all(isinstance(message.get(key), str) and HASH.fullmatch(message[key])
                for key in ('body_sha256', 'projection_sha256')) and type(message.get('body_bytes')) is int and 0 < message['body_bytes'] <= 65536 and
            isinstance(message.get('Data'), dict) and set(message['Data']) == {*arrays, 'IsEmpty'} and message['Data']['IsEmpty'] is False and
            all(same(message['Data'][key], [ITEM] if key == 'ItemsUpdated' else []) for key in arrays) and
            message['projection_sha256'] == sha(canonical({key: message[key] for key in ('MessageType', 'MessageId', 'Data')})),
            'A LibraryChanged delivery lacks its exact public payload, socket, token or ordered boundary.')
    for index, sample in enumerate(window['dom']):
        require(inside(sample) and numeric(sample.get('started_elapsed_ms')) and sample['started_elapsed_ms'] <= sample['elapsed_ms'],
                'A DOM sample is outside its observation interval.')
        dom_identity(sample['observation'], input_record['target'], expected, b['route'], b['document_id'], passed=False)
        if index:
            prior = window['dom'][index - 1]
            require(sample['sequence'] > prior['sequence'] and 0 <= sample['started_elapsed_ms'] - prior['elapsed_ms'] <= 4000,
                    'The passive DOM sample sequence has an unobserved gap.')
        else:
            require(sample['started_elapsed_ms'] <= b['started_elapsed_ms'] + 4000, 'The passive sampling started too late.')
    def fail(outcome):
        return {'result': 'not_observed_within_window', 'outcome': outcome, 'proof': None}
    if not window['events']['physical'] or not window['events']['browser']:
        return fail('websocket_not_observed_within_window')
    require(len(window['events']['physical']) == len(window['events']['browser']) == 1, 'Multiple notifications make the exact causal window ambiguous.')
    upstream, received = window['events']['physical'][0], window['events']['browser'][0]
    require(upstream.get('forwarded') is True and same(upstream['message'], received['message']) and upstream['elapsed_ms'] <= received['elapsed_ms'] and
        received.get('document_id') == b['document_id'] and received.get('route') == b['route'], 'The physical notification was not bound to this document delivery.')
    pairs = pair_reads(window['http']['physical'], window['http']['frames'])
    require(same(pairs, window['http'].get('pairs')), 'The reported HTTP pairs differ from independent unambiguous pairing.')
    accepted = [pair for pair in pairs if pair['complete'] and pair['physical']['kind'] in ('items', 'target') and
        pair['frame'].get('request_sequence', -1) > received['sequence'] and
        pair['frame']['request_elapsed_ms'] >= received['elapsed_ms'] and pair['frame']['finished_elapsed_ms'] <= b['end_elapsed_ms'] and
        pair['physical']['finished_elapsed_ms'] <= b['end_elapsed_ms'] and pair['frame']['token_sha256'] == b['token_sha256'] and
        pair['frame'].get('document_id') == b['document_id'] and pair['frame'].get('page_route') == b['route'] and
        same(pair['physical']['projection'].get('target'), {'Id': ITEM, 'Name': expected, 'Type': 'Movie'})]
    if not accepted:
        return fail('automatic_http_not_observed_within_window')
    selected = min(accepted, key=lambda pair: pair['frame']['finished_elapsed_ms'])
    def target_dom(sample):
        try:
            dom_identity(sample['observation'], input_record['target'], expected, b['route'], b['document_id'])
            return True
        except ObservationError:
            return False
    first = next((sample for index, sample in enumerate(window['dom'][:-1]) if
        sample['started_elapsed_ms'] >= selected['frame']['finished_elapsed_ms'] and target_dom(sample) and
        target_dom(window['dom'][index + 1]) and window['dom'][index + 1]['started_elapsed_ms'] - sample['elapsed_ms'] >= 400), None)
    last = window['dom'][-1] if window['dom'] else None
    if first is None or last is None or not target_dom(last) or last['elapsed_ms'] < b['end_elapsed_ms'] - 600:
        return fail('dom_update_not_observed_within_window')
    return {'result': 'passed', 'outcome': 'automatic_http_and_visible_title_observed', 'proof': {
        'message_id': received['message']['MessageId'], 'physical_exchange_id': selected['physical']['id'],
        'frame_request_index': selected['frame']['index'], 'first_dom_sequence': first['sequence'],
        'last_dom_sequence': last['sequence'], 'identity_bound': True, 'ordered': True}}


def validate_stage(value, input_record, input_sha, child, name, previous, proof):
    require(isinstance(value, dict) and set(value) == {'marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller',
        'node_process', 'name', 'token_sha256', 'session_private', 'previous_control_sha256', 'observation'} and
        value['marker'] == 'goby-client-library-changed-stage-v1' and type(value['version']) is int and value['version'] == 1 and
        value['name'] == name and name in STAGES and value['input_sha256'] == input_sha and
        value['source_closure_sha256'] == sha(canonical(input_record['source_closure'])) and same(value['controller'], input_record['controller']) and
        same(value['node_process'], child) and value['previous_control_sha256'] == previous and
        value['token_sha256'] == proof['token_sha256'] and isinstance(value['session_private'], dict) and
        set(value['session_private']) == {'path', 'sha256'} and value['session_private']['path'] == str(BROWSER_ROOT / 'session-private.json') and
        HASH.fullmatch(value['session_private']['sha256']), 'A browser stage is out of order or belongs to another actor, process or control.')
    observation = value['observation']
    keys = {'discovery': {'home', 'dom', 'reads', 'query_allowlist', 'socket', 'collection_folder_reads', 'collection_folder', 'navigation'}, 'armed': {'quiet', 'boundary'},
            'restore-armed': {'forward', 'boundary'}, 'restored': {'restored'}}[name]
    require(isinstance(observation, dict) and set(observation) == keys, 'A stage contains another authorization payload.')
    if name == 'discovery':
        dom_identity(observation['dom'], input_record['target'], input_record['target']['name'])
        navigation_evidence(observation, input_record, proof['token_sha256'])
        require(observation['home'].get('passed') is True and isinstance(observation['reads'], list) and 0 < len(observation['reads']) <= 8 and
            isinstance(observation['query_allowlist'], list) and 0 < len(observation['query_allowlist']) <= 8,
            'The prerequisite Home or target public list observation is missing.')
        for pair in observation['reads']:
            require(pair.get('complete') is True and pair.get('unambiguous') is True and
                same(pair_reads([pair['physical']], [pair['frame']]), [pair]) and pair['frame']['token_sha256'] == proof['token_sha256'] and
                pair['physical'].get('kind') == 'items' and
                pair['frame'].get('document_id') == observation['dom']['document_id'] and
                pair['frame'].get('page_route') == observation['dom']['route'] and
                same(pair['physical']['projection'].get('target'), {'Id': ITEM, 'Name': input_record['target']['name'], 'Type': 'Movie'}),
                'The initial list response is not attributable to B and the exact target.')
        for query in observation['query_allowlist']:
            require(set(query) == {'kind', 'route', 'query', 'shape_sha256'}, 'The query allowlist has another shape.')
            catalog_shape(query)
        expected_queries = {canonical({key: pair['physical'][key] for key in ('kind', 'route', 'query', 'shape_sha256')}) for pair in observation['reads']}
        require({canonical(query) for query in observation['query_allowlist']} == expected_queries and
                len(expected_queries) == len(observation['query_allowlist']), 'The public query allowlist is not exactly the observed list reads.')
        socket = observation['socket']
        require(socket.get('token_sha256') == proof['token_sha256'] and safe_text(socket.get('connection_id'), 80) and
            type(socket.get('seen')) is int and type(socket.get('opened')) is int and 1 <= socket['opened'] <= socket['seen'] <= 2,
            'The discovery socket does not belong to B.')
    elif name in ('armed', 'restore-armed'):
        validate_boundary(observation['boundary'], 'forward' if name == 'armed' else 'restored', proof['token_sha256'])
        if name == 'armed':
            quiet = observation['quiet']
            require(isinstance(quiet, dict) and quiet.get('passed') is True and type(quiet.get('duration_ms')) is int and quiet['duration_ms'] == 20000 and
                type(quiet.get('catalog_requests')) is int and quiet['catalog_requests'] == 0 and type(quiet.get('library_changed_messages')) is int and
                quiet['library_changed_messages'] == 0 and numeric(quiet.get('started_elapsed_ms')) and numeric(quiet.get('completed_elapsed_ms')) and
                20000 <= quiet['completed_elapsed_ms'] - quiet['started_elapsed_ms'] <= 25000 and
                isinstance(quiet.get('samples'), list) and 2 <= len(quiet['samples']) <= 100, 'The complete quiet baseline is missing.')
            prior = None
            for sample in quiet['samples']:
                dom_identity(sample['observation'], input_record['target'], input_record['target']['name'],
                    observation['boundary']['route'], observation['boundary']['document_id'])
                require(type(sample.get('sequence')) is int and numeric(sample.get('started_elapsed_ms')) and numeric(sample.get('elapsed_ms')) and
                    quiet['started_elapsed_ms'] <= sample['started_elapsed_ms'] <= sample['elapsed_ms'] <= quiet['completed_elapsed_ms'] and
                    (prior is None or sample['sequence'] > prior['sequence'] and 0 <= sample['started_elapsed_ms'] - prior['elapsed_ms'] <= 4000),
                    'The quiet sampling has a gap or unordered sample.')
                prior = sample
            require(quiet['samples'][0]['started_elapsed_ms'] <= quiet['started_elapsed_ms'] + 4000 and
                quiet['samples'][-1]['elapsed_ms'] >= quiet['started_elapsed_ms'] + 19400 and
                observation['boundary']['started_elapsed_ms'] >= quiet['completed_elapsed_ms'], 'The quiet interval was truncated.')
    return observation


class NativeFence:
    """Consume exact intents before dispatch, including socket failures."""

    PATH = '/admin/v1/items/' + ITEM + '/metadata'
    REQUESTS = {'login': ('POST', '/admin/v1/session'), 'original': ('GET', PATH),
        'forward': ('PUT', PATH), 'forward-readback': ('GET', PATH), 'pre-restore': ('GET', PATH),
        'restore': ('PUT', PATH), 'restore-readback': ('GET', PATH),
        'reconcile-one': ('GET', PATH), 'reconcile-two': ('GET', PATH),
        'logout': ('DELETE', '/admin/v1/session'), 'exact401': ('GET', '/admin/v1/session')}

    def __init__(self):
        self.reserved = []

    def reserve(self, label, method, path):
        require(label in self.REQUESTS and self.REQUESTS[label] == (method, path) and label not in self.reserved and
            len(self.reserved) < 12 and (label == 'login' if not self.reserved else label != 'login'),
            'A native request is outside its exact one-shot method, route, count or intent.')
        require(label == 'login' or 'login' in self.reserved, 'Native authentication was not reserved.')
        require(label != 'forward' or 'original' in self.reserved, 'The forward write lacks its original read.')
        require(label != 'restore' or 'forward' in self.reserved, 'A restoration has no dispatched forward intent.')
        require(label != 'exact401' or 'logout' in self.reserved, 'The native exact401 precedes its one logout.')
        require('logout' not in self.reserved or label == 'exact401', 'A native edit or read followed logout.')
        self.reserved.append(label)


def validate_source_manifest(value, driver):
    require(isinstance(value, dict) and set(value) == {'marker', 'files'} and
        value['marker'] == 'goby-client-library-changed-sources-v1' and isinstance(value['files'], dict) and
        driver == TOOL / JS_NAMES[0] and set(value['files']) == {str(TOOL / name) for name in JS_NAMES} and
        all(isinstance(digest, str) and HASH.fullmatch(digest) for digest in value['files'].values()),
        'The new source closure is not exactly the seven reviewed JavaScript imports.')
    return dict(value['files'])


def ui_logout_proven(report, proof):
    actor = report.get('actor', {})
    logout, physical, check = actor.get('logout', {}), actor.get('proxy_logout', {}), actor.get('session_proof', {})
    rows = check.get('entries', [])
    return (logout.get('status') == 204 and logout.get('login_view_visible') is True and physical.get('status') == 204 and
        physical.get('completed') is True and physical.get('token_fingerprint') == proof['token_sha256'] and
        check.get('outcome') == 'all_observed_logout_tokens_rejected' and len(rows) == 1 and
        rows[0].get('result') == 'logout_token_rejected' and rows[0].get('token_fingerprint') == proof['token_sha256'])


def validate_prior_ledger(baseline, before, after, proof):
    """Retain the failed attempt as a closed one-login delta, never UI success."""
    compare_fixed_snapshot(baseline, before)
    validate_login(proof)
    session = owned_session(before, after, proof)
    require(same(session['client_capabilities'], normalize_capabilities(canonical(session['client_capabilities']))) and
            session['revoked_at'] is not None and session['device_registry_id'] is not None,
            'The prior B session is not closed with bounded stored capabilities and its own device.')
    result = validate_ledger(before, after, target_profile(before), browser=proof, phase='original', closed=True)
    require(same(result, {'new_sessions': 1, 'new_devices': 1, 'new_audits': 2, 'metadata_revision_delta': 0,
                         'old_rows_sequences_private_preserved': True, 'owned_sessions_closed': True}),
            'The predecessor changed more than its single closed B login.')
    # The general ledger already protects every old row and exact sequence
    # consumption. The predecessor additionally permits no new session fields.
    expected = {'id': proof['session_id'], 'user_id': B, 'token_hash': '\\x' + proof['token_sha256'], 'kind': 'emby',
        'client_name': proof['client_name'], 'device_id': proof['device_id'], 'device_name': proof['device_name'],
        'client_version': proof['client_version'], 'created_at': session['created_at'], 'expires_at': session['expires_at'],
        'last_seen_at': session['last_seen_at'], 'revoked_at': session['revoked_at'],
        'client_capabilities': session['client_capabilities'], 'device_registry_id': session['device_registry_id']}
    require(same(session, expected), 'The predecessor session has an unreviewed complete-row field.')
    return result


def validate_prior_documents(candidate, authority, prior_input, controller, browser, baseline, before, after, version=2):
    scope = history_scope(version)
    root, tool, worker_unit, controller_unit = (scope[key] for key in ('root', 'tool', 'worker', 'controller'))
    expected_authority = {key: authority[key] for key in UPGRADE_AUTHORITY_KEYS}
    if version == 3:
        expected_authority.update(PRIOR_PINS)
    expected_browser_authority = {**expected_authority, 'before_snapshot': authority['prior_before_snapshot']}
    require(isinstance(prior_input, dict) and set(prior_input) == {'marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate',
                'fixture', 'expected_libraries', 'target', 'source_closure', 'authority', 'controller'} and
            prior_input['marker'] == INPUT_MARKER and type(prior_input['version']) is int and prior_input['version'] == 1 and
            prior_input['mode'] == MODE and prior_input['root'] == str(root) and prior_input['output'] == str(root / 'browser') and
            same(prior_input['candidate'], candidate) and same(prior_input['authority'], expected_browser_authority) and
            same(prior_input['fixture'], {key: {'path': str(path), 'sha256': digest} for key, (path, digest) in FIXTURE.items()}),
            'The predecessor input changed its original candidate, fixture or historical authority format.')
    source_closure = prior_input['source_closure']
    require(isinstance(source_closure, dict) and set(source_closure) == {str(tool / name) for name in JS_NAMES} and
            all(required_digest(value) for value in source_closure.values()), 'The predecessor has another JavaScript closure.')
    input_sha, closure_sha = authority['prior_input']['sha256'], sha(canonical(source_closure))
    actor, outer = prior_input['actor'], prior_input['controller']
    require(isinstance(actor, dict) and set(actor) == {'slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256'} and
            actor['slot'] == 'B' and actor['user_id'] == B and actor['account_key'] == 'viewer' and actor['source_credentials_sha256'] == CREDENTIALS_SHA and
            actor['credentials'].get('path') == str(root / 'viewer-credentials.json') and required_digest(actor['credentials'].get('sha256')) and
            isinstance(outer, dict) and set(outer) == {'pid', 'start_ticks', 'boot_id', 'unit'} and outer['unit'] == controller_unit and
            type(outer['pid']) is int and outer['pid'] > 1 and outer['boot_id'] == BOOT and
            isinstance(outer['start_ticks'], str) and re.fullmatch('[1-9][0-9]*', outer['start_ticks']),
            'The predecessor does not bind its one B actor and failed controller process.')
    require(controller.get('marker') == MARKER and type(controller.get('version')) is int and controller['version'] == 1 and
            controller.get('mode') == MODE and controller.get('status') == 'failed' and controller.get('phase') == 'discovery' and
            controller.get('input_sha256') == input_sha and controller.get('source_closure_sha256') == closure_sha and
            same(controller.get('authority'), expected_authority) and same(controller.get('controller'), outer) and
            same(controller.get('candidate_process'), candidate['process']) and controller.get('candidate_invocation') == candidate['invocation_id'] and
            controller.get('state_sha256') == candidate['state_sha256'] and controller.get('reserved_native_intents') == [] and
            controller.get('dispatched_native_intents') == [] and controller.get('restoration') == 'not_required' and
            all(controller.get(key) is False for key in ('restoration_required', 'browser_fallback_used', 'automatic_retry', 'sql_business_writes',
                'candidate_or_primary_service_writes', 'worker_chain_ledger_passed', 'acceptance_ready_for_outer_terminal',
                'library_changed_client_acceptance', 'client_acceptance', 'full_m3_complete')) and
            isinstance(controller.get('errors'), list) and controller['errors'], 'The predecessor controller is not the retained pre-native failure.')
    for key, name in (('prior_input', 'input.json'), ('prior_browser_report', 'browser-report.json'),
                      ('prior_before_snapshot', 'before-full.json'), ('prior_after_snapshot', 'after-full.json')):
        require(same(controller.get('evidence', {}).get(name), authority[key]), 'A predecessor evidence descriptor changed.')
    worker = controller.get('node_process', {})
    require(browser.get('marker') == 'goby-client-library-changed-report-v1' and type(browser.get('version')) is int and browser['version'] == 1 and
            browser.get('mode') == MODE and browser.get('result') == browser.get('outcome') == 'failed' and
            browser.get('failure') == 'library_changed_target_card_not_observed' and browser.get('input_sha256') == input_sha and
            browser.get('source_closure_sha256') == closure_sha and same(browser.get('controller'), outer) and
            same(browser.get('node_process'), worker) and worker.get('cgroup') == '/system.slice/' + worker_unit and
            same(browser.get('candidate'), candidate) and same(browser.get('authority'), expected_browser_authority) and
            same(browser.get('target'), prior_input['target']) and browser.get('stages') == browser.get('controls') == [] and
            all(browser.get(key) is None for key in ('discovery', 'armed', 'forward', 'restore_armed', 'restored')) and
            all(browser.get(key) is False for key in ('library_changed_client_acceptance', 'client_acceptance', 'full_m3_complete')) and
            browser.get('restoration') == 'not_required', 'The predecessor browser failure was changed into an accepted UI stage.')
    profile = target_profile(before)
    require(same(prior_input['target'], profile['target']) and same(prior_input['expected_libraries'],
            sorted([{'id': row['id'], 'name': row['name']} for row in before['database']['tables']['libraries']], key=lambda row: row['id'])),
            'The predecessor target or four-library inventory differs from its complete before snapshot.')
    proof, actor = browser.get('login_proof'), browser.get('actor', {})
    validate_login(proof)
    require(actor.get('slot') == 'B' and actor.get('id') == B and actor.get('ordinary_authority_confirmed') is True and
            actor.get('closed') is True and actor.get('cleanup_failures') == [] and actor.get('token_fingerprint') == proof['token_sha256'] and
            actor.get('proxy_login_status') == 200 and actor.get('login', {}).get('status') == 200 and
            type(actor.get('login', {}).get('request_count')) is int and actor['login']['request_count'] == 1 and ui_logout_proven(browser, proof),
            'The predecessor does not prove one B login and its exact UI logout.')
    rejection = actor['session_proof']['entries'][0].get('verification', {})
    require(rejection.get('status') == 401 and rejection.get('method') == 'GET' and rejection.get('route') == '/emby/System/Info' and
            rejection.get('is_ui_request') is False and rejection.get('eligible_at_request_start') is True and rejection.get('result') == 'token_rejected',
            'The predecessor does not independently reject the exact original token.')
    closure = browser.get('closure', {})
    require(all(closure.get(key) is True for key in ('context_closed', 'browser_closed', 'proxy_closed')) and
            all(type(closure.get(key)) is int and closure[key] == 0 for key in ('http_pending', 'websocket_pending', 'websocket_active', 'sockets_remaining')) and
            type(closure.get('websocket_opened')) is int and closure['websocket_opened'] == closure.get('websocket_closed') == 1 and
            closure.get('cleanup_failures') == [], 'The failed predecessor retains a browser, proxy or socket lifetime.')
    capabilities = browser.get('capabilities_private', {})
    require(set(capabilities) == {'path', 'sha256', 'request_count', 'last_successful_body_sha256'} and
            capabilities['path'] == str(root / 'browser/capabilities-private.json') and required_digest(capabilities['sha256']) and
            capabilities['request_count'] == 1 and required_digest(capabilities['last_successful_body_sha256']) and
            same({key: capabilities[key] for key in ('path', 'sha256')}, controller['evidence'].get('browser-capabilities-private.json')),
            'The predecessor capability receipt changed its observed single request.')
    result = validate_prior_ledger(baseline, before, after, proof)
    require(same(controller.get('ledger'), result), 'The predecessor summary disagrees with the complete independently recomputed delta.')
    if version == 3:
        require(same(controller.get('prior_failure_preservation'), result), 'The v3 report lost its separately validated v2 predecessor result.')
    return result


def validate_prior_terminal(candidate, authority, controller, browser, terminal, after, independent, version=2):
    scope, seal = history_scope(version), history_seal(version)
    root, tool, worker_unit, controller_unit = (scope[key] for key in ('root', 'tool', 'worker', 'controller'))
    keys = {'automatic_retry', 'before_snapshot', 'browser', 'browser_report', 'candidate', 'candidate_preserved', 'captured_at',
        'cleanup', 'cleanup_needed', 'cleanup_performed', 'client_acceptance', 'controller_source', 'current_matches_prior_after',
        'dispatched_native_intents', 'exact_owned_additions_retained', 'failed_units', 'full_m3_complete', 'http_requests',
        'independent_snapshot', 'input', 'ledger', 'library_changed_client_acceptance', 'marker', 'media_fact_sha256', 'media_preserved',
        'observed_run_status', 'old_rows_sequences_private_preserved', 'old_v1_scope_preserved', 'owned_session_closed', 'phase',
        'primary_fact_sha256', 'primary_invocation_id', 'primary_preserved', 'primary_process', 'prior_after_snapshot', 'report',
        'reserved_native_intents', 'restoration', 'schema', 'scope', 'scope_files', 'scope_files_unchanged', 'seal_script',
        'service_writes', 'sql_business_writes', 'status', 'tool', 'upgrade_authority_snapshot', 'version'}
    if version == 3:
        keys |= {'baseline_chain_verified', 'cumulative_totals', 'discovery_failure', 'old_v2_scope_preserved',
                 'predecessor_inventories', 'predecessor_terminals', 'prior_baseline', 'prior_failure_preservation'}
    require(isinstance(terminal, dict) and set(terminal) == keys and terminal['marker'] == 'goby-source55-failed-ui-terminal-v' + str(version) and
            type(terminal['version']) is int and terminal['version'] == 1 and type(terminal['schema']) is int and terminal['schema'] == 28 and
            terminal['status'] == 'failed_scope_sealed' and terminal['observed_run_status'] == 'failed' and terminal['phase'] == 'discovery' and
            terminal['scope'] == str(root) and terminal['tool'] == str(tool) and terminal['cleanup'] == terminal['restoration'] == 'not_required' and
            terminal['reserved_native_intents'] == terminal['dispatched_native_intents'] == [],
            'The predecessor terminal is not the exact sealed failed scope.')
    require(all(terminal[key] is True for key in ('candidate_preserved', 'current_matches_prior_after', 'exact_owned_additions_retained',
                'media_preserved', 'old_rows_sequences_private_preserved', 'old_v1_scope_preserved', 'owned_session_closed',
                'primary_preserved', 'scope_files_unchanged')) and
            all(terminal[key] is False for key in ('automatic_retry', 'cleanup_needed', 'cleanup_performed', 'client_acceptance',
                'full_m3_complete', 'library_changed_client_acceptance', 'sql_business_writes')) and
            all(type(terminal[key]) is int and terminal[key] == 0 for key in ('http_requests', 'service_writes')) and
            same(terminal['ledger'], controller['ledger']), 'The failure seal performs cleanup or claims UI acceptance.')
    for key, name in (('before_snapshot', 'prior_before_snapshot'), ('prior_after_snapshot', 'prior_after_snapshot'),
                      ('input', 'prior_input'), ('browser_report', 'prior_browser_report'), ('report', 'prior_controller_report'),
                      ('upgrade_authority_snapshot', 'current_snapshot')):
        require(same(terminal[key], authority[name]), 'The failure seal points to another predecessor artifact.')
    require(same(terminal['independent_snapshot'], seal['independent']) and
            all(same(terminal[key], value) for key, value in seal['records'].items()) and
            terminal['media_fact_sha256'] == '0f21473c43a050ad54f8985ee57e98addc6420e0cf33d6ee115db8cf8c0eff7d' and
            terminal['primary_fact_sha256'] == '0882d96f8b61c5586ce514a4c320a9bc933c2610cf55f24bfbec80237e77da3a' and
            same(terminal['primary_process'], PRIMARY_PROCESS) and terminal['primary_invocation_id'] == PRIMARY_INVOCATION,
            'The failure seal lost its independent snapshot, file seal or primary identity.')
    properties = {'ActiveState', 'ControlGroup', 'DropInPaths', 'ExecMainCode', 'ExecMainStatus', 'FragmentPath', 'Group', 'Id',
                  'InvocationID', 'LoadState', 'MainPID', 'Restart', 'Result', 'SubState', 'Transient', 'User', 'WorkingDirectory'}
    live = terminal['candidate']
    require(isinstance(live, dict) and set(live) == {'binary_sha256', 'invocation_id', 'process', 'properties'} and
            all(same(live[key], candidate[key]) for key in ('binary_sha256', 'invocation_id', 'process')),
            'The failure seal names another running candidate.')
    expected_live = {'ActiveState': 'active', 'ControlGroup': '/system.slice/goby-client-m3e.service', 'DropInPaths': '',
        'ExecMainCode': '0', 'ExecMainStatus': '0', 'FragmentPath': '/etc/systemd/system/goby-client-m3e.service', 'Group': 'goby',
        'Id': 'goby-client-m3e.service', 'InvocationID': candidate['invocation_id'], 'LoadState': 'loaded', 'MainPID': str(candidate['process']['pid']),
        'Restart': 'no', 'Result': 'success', 'SubState': 'running', 'Transient': 'no', 'User': 'goby', 'WorkingDirectory': '/var/lib/goby-test/client-m3e'}
    require(same(live['properties'], expected_live), 'The failure seal candidate is not the exact unchanged live service.')
    require(isinstance(terminal['failed_units'], dict) and set(terminal['failed_units']) == {controller_unit, worker_unit},
            'The failure seal lacks the two exact independent failed service lifetimes.')
    for unit, source, invocation, working in ((controller_unit, controller['controller'], seal['controller_invocation'], str(tool)),
            (worker_unit, controller['node_process'], seal['worker_invocation'], str(root))):
        value = terminal['failed_units'][unit]
        process = {'pid': source['pid'], 'start_ticks': int(source['start_ticks']), 'boot_id': source['boot_id']}
        require(isinstance(value, dict) and set(value) == {'old_process', 'old_process_gone', 'properties', 'recursive_cgroup'} and
                same(value['old_process'], process) and value['old_process_gone'] is True and
                same(value['recursive_cgroup'], {'exists': False, 'files_checked': 0, 'path': '/sys/fs/cgroup/system.slice/' + unit, 'processes': 0}),
                'A failed predecessor process or recursive cgroup is still present.')
        expected = {'ActiveState': 'failed', 'ControlGroup': '', 'DropInPaths': '', 'ExecMainCode': '1', 'ExecMainStatus': '1',
            'FragmentPath': '/run/systemd/transient/' + unit, 'Group': 'root', 'Id': unit, 'InvocationID': invocation,
            'LoadState': 'loaded', 'MainPID': '0', 'Restart': 'no', 'Result': 'exit-code', 'SubState': 'failed', 'Transient': 'yes',
            'User': 'root', 'WorkingDirectory': working}
        require(set(value['properties']) == properties and same(value['properties'], expected), 'A prior failed service was replaced, restarted or adopted.')
        if unit == worker_unit:
            require(same(controller.get('worker_terminal'), {**expected, 'cgroup_empty': True}), 'The controller and independent worker terminal disagree.')
    expected_browser = {'browser_closed': True, 'capabilities_verified': True, 'capability_requests': 1, 'context_closed': True,
        'failure': 'library_changed_target_card_not_observed', 'failure_counters': {'observer_errors': 0, 'page_errors': 0, 'proxy_failed': 0,
            'proxy_rejected': 0, 'websocket_failed': 0}, 'http_pending': 0, 'login_proven': True, 'owned_session_revoked': True,
        'proxy_closed': True, 'result': 'failed', 'sockets_remaining': 0, 'ui_logout_and_token_rejection_proven': True,
        'websocket_active': 0, 'websocket_closed': 1, 'websocket_opened': 1, 'websocket_pending': 0}
    require(same(terminal['browser'], expected_browser) and browser['result'] == terminal['browser']['result'],
            'The independent browser closure does not retain the actual failed observation.')
    if version == 3:
        require(terminal['baseline_chain_verified'] is True and terminal['old_v2_scope_preserved'] is True and
                same(terminal['prior_baseline'], PRIOR_INDEPENDENT) and
                all(same(terminal[key], descriptor) for key, descriptor in HISTORY_V3_PREDECESSORS.items()) and
                same(terminal['prior_failure_preservation'], controller['prior_failure_preservation']) and
                same(terminal['cumulative_totals'], {'sessions': 77, 'devices': 66, 'activity_entries': 171}) and
                same(terminal['cumulative_totals'], {key: len(after['database']['tables'][key]) for key in ('sessions', 'devices', 'activity_entries')}),
                'The v3 failure seal lost its ordered predecessor chain or exact cumulative population.')
    compare_fixed_snapshot(after, independent)
    require(instant(independent['database']['metadata']['captured_at']) <= instant(terminal['captured_at']),
            'The independent failure snapshot postdates its seal.')


def validate_history_documents(candidate, authority, documents, upgraded):
    """Validate only the two retained original formats in their fixed order."""
    require(isinstance(documents, list) and len(documents) == 2 and isinstance(authority.get('history'), list) and
            len(authority['history']) == 2, 'The complete two-entry history is required.')
    baseline, latest, results = upgraded, upgraded, []
    for entry, document, version in zip(authority['history'], documents, (2, 3)):
        validate_history_entry(entry, version)
        require(isinstance(document, dict) and set(document) == {*HISTORY_NAMES, 'independent_snapshot'},
                'A history document bundle omitted an original artifact.')
        flat = history_entry_authority(authority, entry)
        compare_fixed_snapshot(latest, document['before_snapshot'])
        result = validate_prior_documents(candidate, flat, document['input'], document['controller_report'], document['browser_report'],
            baseline, document['before_snapshot'], document['after_snapshot'], version)
        validate_prior_terminal(candidate, flat, document['controller_report'], document['browser_report'], document['terminal'],
            document['after_snapshot'], document['independent_snapshot'], version)
        baseline, latest = document['after_snapshot'], document['independent_snapshot']
        results.append({'version': version, 'ledger': result, 'after_snapshot': entry['after_snapshot'],
                        'independent_snapshot': history_seal(version)['independent']})
    return baseline, latest, results


class Run:
    """One new controller; every mutation is separately reserved and attributable."""

    def __init__(self, args):
        self.args = args
        self.candidate = self.authority = self.catalog = self.current_authority = None
        self.prior_after = self.prior_ledger = None
        self.prior_independent = None
        self.op = self.profile_reader = self.media_reader = None
        self.root_fd = self.lock = None
        self.state = self.before = self.after = self.profile = self.media = self.primary = None
        self.input = self.input_sha = self.child = self.invocation = self.terminal = None
        self.sources, self.js_sources, self.artifacts, self.records = {}, {}, {}, {}
        self.errors, self.stages, self.controls, self.native_results = [], [], [], {}
        self.phase, self.restoration = 'preflight', 'not_required'
        self.fence = NativeFence()
        self.dispatched = []
        self.reservation = self.browser = self.browser_private = self.browser_token = self.administrator = None
        self.cookie = self.csrf = self.authenticated = self.node_report = self.capabilities = self.delta = None
        self.native_login_validated = False
        self.forward_ack = self.restore_ack = False
        self.launched = self.closed = self.fallback_attempted = self.release_attempted = False
        self.deadline = time.monotonic() + 650
        self.publications = {}

    def error(self, stage, error):
        row = {'stage': stage, 'failure_type': type(error).__name__}
        if type(error) is ObservationError:
            row['reason'] = str(error)
        self.errors.append(row)

    def command(self, arguments, text=None, environment=None, timeout=25):
        values = [str(value) for value in arguments]
        allowed = values == ['/usr/bin/ss', '-H', '-ltnp', 'sport = :18198'] or (
            len(values) == 5 and values[:3] == ['/usr/bin/systemctl', 'show', 'goby-client-m3e.service'] and
            values[3] == '--no-pager' and values[4].startswith('--property='))
        pg = ['/usr/sbin/runuser', '-u', 'postgres', '--', '/usr/lib/postgresql/17/bin/psql', '-X', '--no-password',
            '-h', '/var/lib/postgresql/goby-workspace-v1/socket', '-p', '15432', '-U', 'postgres', '-d']
        is_pg = len(values) == 20 and values[:14] == pg and values[14] in ('postgres', 'goby_client_m3e') and \
            values[15:] == ['-v', 'ON_ERROR_STOP=1', '-A', '-t', '-q']
        if is_pg:
            require(isinstance(text, str) and text.lstrip().startswith(('SELECT ', 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;')),
                    'A snapshot helper requested a non-read-only SQL entry point.')
        media = len(values) == 7 and values[:6] == ['/usr/sbin/runuser', '-u', 'goby', '--', '/usr/bin/test', '-r'] and \
            Path(values[6]).is_relative_to(Path('/opt/goby-fixtures/client-m3e')) and '..' not in Path(values[6]).parts
        require(allowed or is_pg or media, 'A helper requested an unapproved command.')
        require(environment is None, 'A snapshot helper supplied another environment.')
        env = dict(ENV)
        if is_pg:
            env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=20000 -c lock_timeout=5000'
        result = subprocess.run(values, input=text, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=min(timeout, 25), check=False, env=env)
        require(result.returncode == 0 and len(result.stdout.encode()) <= MAX_JSON,
                'A bounded read-only helper command failed; no replacement command was attempted.')
        return result.stdout.strip()

    def properties(self, unit=WORKER_UNIT):
        require(unit in (WORKER_UNIT, CONTROLLER_UNIT, PRIMARY_UNIT, 'goby-client-m3e.service'), 'Another unit was requested.')
        fields = 'Id,LoadState,ActiveState,SubState,MainPID,ExecMainCode,ExecMainStatus,Result,ControlGroup,InvocationID,Transient,User,Group,FragmentPath,DropInPaths,WorkingDirectory,Restart'
        result = subprocess.run(['/usr/bin/systemctl', 'show', unit, '--no-pager', '--property=' + fields],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10, check=False, env=ENV)
        require(result.returncode in (0, 1, 4) and len(result.stdout) <= 32768, 'A scoped unit state cannot be read.')
        return dict(line.split('=', 1) for line in result.stdout.splitlines() if '=' in line)

    def primary_fact(self):
        props = self.properties(PRIMARY_UNIT)
        require(props.get('MainPID') == str(PRIMARY_PROCESS['pid']) and props.get('InvocationID') == PRIMARY_INVOCATION and
            props.get('ActiveState') == 'active' and props.get('SubState') == 'running' and
            props.get('ControlGroup') == '/system.slice/' + PRIMARY_UNIT and props.get('User') == props.get('Group') == 'goby',
            'The primary process or invocation changed.')
        process = Path('/proc') / str(PRIMARY_PROCESS['pid'])
        require(self.op.process_identity(PRIMARY_PROCESS['pid']) == PRIMARY_PROCESS and process.stat().st_uid == 995 and
            os.readlink(process / 'exe') == '/opt/goby-dev/goby' and sha((process / 'exe').read_bytes()) == PRIMARY_BINARY_SHA and
            (process / 'cgroup').read_text().strip() == '0::/system.slice/' + PRIMARY_UNIT, 'The primary executable membership changed.')
        files = {}
        for filename, digest in PRIMARY_FILES.items():
            path = Path(filename)
            protected(path, digest, modes=(0o600, 0o644, 0o755), limit=64 << 20, workspace=False)
            files[filename] = {'sha256': digest, 'identity': list(identity(path.lstat()))}
        require(sha((process / 'cmdline').read_bytes()) == 'c2c8b1f234839029e4dcd80ada8d765fa6d4ebcb21be2f4b72665945d9458d6e' and
            sha((process / 'environ').read_bytes()) == 'cfa0a59530f5e302b8ea23bb011823d7e68126be05e8b19c6360e450cbee9186' and
            self.op.process_identity(PRIMARY_PROCESS['pid']) == PRIMARY_PROCESS, 'The primary command or private environment changed.')
        return {'properties': props, 'process': PRIMARY_PROCESS, 'files': files}

    def check_root(self):
        require(self.root_fd is not None, 'No new evidence root is owned.')
        named, opened = ROOT.lstat(), os.fstat(self.root_fd)
        require((named.st_dev, named.st_ino) == (opened.st_dev, opened.st_ino) and stat.S_ISDIR(named.st_mode) and
            named.st_uid == named.st_gid == 0 and stat.S_IMODE(named.st_mode) == 0o700, 'The fresh evidence directory changed identity.')

    def check(self):
        for filename, digest in self.sources.items():
            protected(Path(filename), digest, modes=(0o600, 0o644), limit=2 << 20)
        for filename, digest in self.artifacts.items():
            protected(Path(filename), digest, modes=(0o600, 0o644), limit=MAX_JSON)
        protected(STATE, self.candidate['state_sha256'])
        protected(self.args.node, self.args.node_sha256, modes=(0o755,), limit=512 << 20, workspace=False)
        self.op.verify_fixture_directories(self.state)
        require(self.op.verify_service(self.state) == self.candidate['process'], 'The running source55 candidate changed.')
        props = self.properties('goby-client-m3e.service')
        require(props.get('InvocationID') == self.candidate['invocation_id'] and props.get('ActiveState') == 'active' and props.get('SubState') == 'running',
                'The source55 candidate invocation changed.')
        require(same(self.primary_fact(), self.primary), 'The complete primary identity changed during this scope.')
        if self.root_fd is not None:
            self.check_root()

    def save(self, name, value, ipc=False):
        self.check_root()
        require(isinstance(name, str) and re.fullmatch(r'[a-z][a-z0-9-]*\.(json|stdout|stderr)', name), 'An evidence filename escaped its new root.')
        raw = value if isinstance(value, bytes) else canonical(value) + b'\n'
        require(len(raw) <= (MAX_IPC if ipc else MAX_JSON), 'A scoped evidence record exceeded its byte bound.')
        temporary = name + '.pending' if ipc else name
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=self.root_fd)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        if ipc:
            # A hard link publishes complete bytes without replacing a prior
            # record. Failure retains either the pending or complete evidence.
            os.link(temporary, name, src_dir_fd=self.root_fd, dst_dir_fd=self.root_fd, follow_symlinks=False)
            os.fsync(self.root_fd)
            os.unlink(temporary, dir_fd=self.root_fd)
        os.fsync(self.root_fd)
        require(protected(ROOT / name, limit=MAX_IPC if ipc else MAX_JSON) == raw, 'A newly published evidence record changed on readback.')
        row = {'path': str(ROOT / name), 'sha256': sha(raw)}
        self.records[name] = row
        return row

    def snapshot(self, name=None):
        value = self.op.preservation_snapshot(self.state, 28)
        if name is not None:
            self.save(name, value)
        validate_schema28_snapshot(value, self.state, self.catalog, self.op)
        quiescent(value)
        return value

    def load(self):
        sys.dont_write_bytecode = True
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
            os.environ.get('SSH_CONNECTION') and os.readlink('/proc/self/ns/net') == os.readlink('/proc/1/ns/net'),
            'Use the authorized root host-network SSH environment only.')
        require(Path(__file__).absolute() == TOOL / 'observe-client-library-changed-source55.py', 'The controller is outside its new frozen tool directory.')
        self.candidate, self.authority = validate_authority_input(decode(protected(self.args.authority, self.args.authority_sha256)))
        self.js_sources = validate_source_manifest(decode(protected(self.args.source_closure, self.args.source_closure_sha256)), self.args.driver)
        self.sources = {**self.js_sources, str(Path(__file__).absolute()): self.args.script_sha256}
        for name, (path, digest) in HELPERS.items():
            self.sources[str(path)] = digest
            setattr(self, name, load_reader(path, digest, 'library_changed_' + name))
        self.op.command = self.command
        self.state = decode(protected(STATE, self.candidate['state_sha256']))
        self.artifacts = {str(self.args.source_closure): self.args.source_closure_sha256, str(CREDENTIALS): CREDENTIALS_SHA,
            str(self.args.authority): self.args.authority_sha256, str(CATALOG): CATALOG_SHA,
            str(SOURCE / 'backup-source-inputs.json'): MANIFEST_SHA,
            **{self.authority[key]['path']: self.authority[key]['sha256'] for key in UPGRADE_AUTHORITY_KEYS},
            **{entry[name]['path']: entry[name]['sha256'] for entry in self.authority['history'] for name in HISTORY_NAMES},
            **{str(path): digest for path, digest in FIXTURE.values()}}
        intent, report, attestation = (read_record(self.authority[key]) for key in ('upgrade_intent', 'upgrade_report', 'upgrade_attestation'))
        validate_upgrade_authority(self.candidate, self.authority, intent, report, attestation, self.state)
        previous_state = artifact_descriptor(report.get('evidence', {}).get('before-state.json'))
        require(previous_state['path'] == str(Path(intent['output']) / 'before-state.json') and previous_state['sha256'] == PREVIOUS_STATE_SHA,
                'The upgrade lacks its exact retained prior state authority.')
        self.artifacts[previous_state['path']] = previous_state['sha256']
        validate_historical_state(decode(protected(Path(previous_state['path']), previous_state['sha256'])), self.state)
        self.catalog = decode(protected(CATALOG, CATALOG_SHA, modes=(0o600, 0o644)))
        validate_schema28_catalog(self.catalog)
        self.current_authority = read_record(self.authority['current_snapshot'])
        validate_schema28_snapshot(self.current_authority, self.state, self.catalog, self.op)
        require(all(len(self.current_authority['database']['tables'][name]) == count for name, count in
                (('sessions', 75), ('devices', 64), ('activity_entries', 167), ('items', 22), ('libraries', 4), ('library_roots', 4))),
                'The independently attested source55 snapshot is not the sealed upgraded population.')
        history = []
        for entry in self.authority['history']:
            document = {name: read_record(entry[name]) for name in HISTORY_NAMES}
            independent = history_seal(entry['version'])['independent']
            require(same(document['terminal'].get('independent_snapshot'), independent), 'A history seal requested another independent snapshot.')
            self.artifacts[independent['path']] = independent['sha256']
            document['independent_snapshot'] = read_record(independent)
            for name in ('before_snapshot', 'after_snapshot', 'independent_snapshot'):
                validate_schema28_snapshot(document[name], self.state, self.catalog, self.op)
            history.append(document)
        self.prior_after, self.prior_independent, self.prior_ledger = validate_history_documents(
            self.candidate, self.authority, history, self.current_authority)
        self.lock = os.open(self.op.LOCK, os.O_RDONLY | os.O_NOFOLLOW)
        require(self.op.identity(os.fstat(self.lock)) == self.op.identity(self.op.regular(self.op.LOCK)), 'The existing fixture lock changed identity.')
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not os.path.lexists(ROOT) and self.properties().get('LoadState') == 'not-found' and
            not Path('/sys/fs/cgroup' + CGROUP).exists(), 'The fresh scope or worker is occupied; no replay, adoption or reset is permitted.')
        self.primary = self.primary_fact()
        self.check()
        self.media = self.media_reader.media_witness(self.op, self.state)
        self.before = self.snapshot()
        compare_fixed_snapshot(self.prior_after, self.before)
        compare_fixed_snapshot(self.prior_independent, self.before)
        self.profile = target_profile(self.before)
        self.credentials = decode(protected(CREDENTIALS, CREDENTIALS_SHA))
        require(self.credentials.get('base_url') == BASE_URL and self.credentials.get('direct_url') == DIRECT_URL and
            self.credentials.get('viewer', {}).get('username') == 'm3e-client-viewer' and
            all(isinstance(self.credentials.get(slot), dict) and set(self.credentials[slot]) == {'username', 'password'} and
                safe_text(self.credentials[slot]['username'], 256) and re.fullmatch('[0-9a-f]{48}', self.credentials[slot]['password'])
                for slot in ('admin', 'viewer')), 'The pinned private account document has another identity or shape.')
        admin = next(row for row in self.before['database']['tables']['users'] if row['id'] == ADMIN)
        require(admin['name'] == self.credentials['admin']['username'] and admin['is_administrator'] is True and admin['is_disabled'] is False,
                'The native account is not the accepted enabled administrator.')

    def prepare(self):
        controller = {**proc_identity(os.getpid()), 'unit': CONTROLLER_UNIT}
        props = self.properties(CONTROLLER_UNIT)
        require(props.get('MainPID') == str(controller['pid']) and props.get('ControlGroup') == '/system.slice/' + CONTROLLER_UNIT and
            props.get('User') == props.get('Group') == 'root' and ID.fullmatch(props.get('InvocationID', '')) and
            (Path('/proc/self/cgroup').read_text().strip() == '0::/system.slice/' + CONTROLLER_UNIT),
            'Execution requires its exact new outer controller unit.')
        os.umask(0o077)
        ROOT.mkdir(mode=0o700)
        self.root_fd = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        parent = os.open(WORK, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
        self.save('intent.json', {'marker': MARKER, 'version': 1, 'mode': MODE, 'root': str(ROOT), 'controller': controller,
            'controller_invocation': props['InvocationID'], 'worker_unit': WORKER_UNIT, 'sources': self.sources, 'authority': self.authority,
            'native_maximum': 12, 'metadata_put_maximum': 2, 'before_authority': self.authority['history'][-1]['after_snapshot'],
            'history_preservation': self.prior_ledger,
            'no_automatic_replay': True, 'no_service_or_sql_business_mutation': True})
        before = self.save('before-full.json', self.before)
        self.save('media-before.json', self.media)
        self.save('primary-before.json', self.primary)
        viewer = {'marker': 'goby-client-library-changed-viewer-v1', 'slot': 'B', 'user_id': B,
            'base_url': BASE_URL, 'direct_url': DIRECT_URL, 'viewer': self.credentials['viewer']}
        viewer_record = self.save('viewer-credentials.json', viewer)
        self.input = {'marker': INPUT_MARKER, 'version': 1, 'mode': MODE, 'root': str(ROOT), 'output': str(BROWSER_ROOT),
            'actor': {'slot': 'B', 'user_id': B, 'credentials': viewer_record, 'account_key': 'viewer', 'source_credentials_sha256': CREDENTIALS_SHA},
            'candidate': copy.deepcopy(self.candidate),
            'fixture': {key: {'path': str(path), 'sha256': digest} for key, (path, digest) in FIXTURE.items()},
            'expected_libraries': sorted([{'id': row['id'], 'name': row['name']} for row in self.before['database']['tables']['libraries']], key=lambda row: row['id']),
            'target': self.profile['target'], 'source_closure': self.js_sources, 'authority': {**self.authority, 'before_snapshot': before}, 'controller': controller}
        self.input_sha = self.save('input.json', self.input)['sha256']
        self.save('cleanup-reservation.json', {'marker': MARKER, 'input_sha256': self.input_sha, 'controller': controller,
            'scope': 'Only independently proven new B and native sessions; one native restoration with exact revision, controls and credential audit.',
            'browser_fallback_maximum': 2, 'native_maximum': 12, 'metadata_put_maximum': 2, 'old_credentials_revocation': False})
        self.save('node.stdout', b'')
        self.save('node.stderr', b'')

    def node_arguments(self):
        return [str(self.args.node), str(self.args.driver), '--input', str(ROOT / 'input.json'), '--input-sha256', self.input_sha,
            '--output', str(BROWSER_ROOT)]

    def capture_child(self, status):
        require(status.get('Id') == WORKER_UNIT and status.get('Transient') == 'yes' and status.get('ControlGroup') == CGROUP and
            status.get('User') == status.get('Group') == 'root' and ID.fullmatch(status.get('InvocationID', '')),
            'The worker lacks its exact new transient identity.')
        pid = int(status.get('MainPID', '0'))
        current, process = proc_identity(pid), Path('/proc') / str(pid)
        require(current['boot_id'] == self.candidate['process']['boot_id'] and process.stat().st_uid == process.stat().st_gid == 0 and
            os.readlink(process / 'exe') == str(self.args.node) and sha((process / 'exe').read_bytes()) == self.args.node_sha256 and
            (process / 'cmdline').read_bytes() == b'\0'.join(word.encode() for word in self.node_arguments()) + b'\0' and
            (process / 'cgroup').read_text().strip() == '0::' + CGROUP and proc_identity(pid) == current,
            'The worker PID, Node executable, command or cgroup is not owned.')
        self.invocation = status['InvocationID']
        self.child = {**current, 'uid': 0, 'gid': 0, 'executable_path': str(self.args.node),
            'executable_sha256': self.args.node_sha256, 'cgroup': CGROUP}
        self.save('node-process.json', {'process': self.child, 'invocation_id': self.invocation})

    def worker_live(self):
        status = self.properties()
        require(self.child is not None and status.get('InvocationID') == self.invocation and status.get('MainPID') == str(self.child['pid']) and
            status.get('ActiveState') == 'active' and status.get('SubState') == 'running' and
            same(proc_identity(self.child['pid']), {key: self.child[key] for key in ('pid', 'start_ticks', 'boot_id')}),
            'The exact browser worker no longer owns a live observation.')

    def launch(self):
        self.check()
        require(self.properties().get('LoadState') == 'not-found' and not Path('/sys/fs/cgroup' + CGROUP).exists(), 'The new worker name became occupied.')
        require(instant(self.before['database']['metadata']['captured_at']) <= dt.datetime.now(dt.timezone.utc) <=
            instant(self.before['database']['metadata']['captured_at']) + dt.timedelta(seconds=150), 'The pre-login snapshot is stale.')
        arguments = ['/usr/bin/systemd-run', '--unit=' + WORKER_UNIT, '--service-type=exec', '--quiet',
            '--property=User=root', '--property=Group=root', '--property=WorkingDirectory=' + str(ROOT),
            '--property=Restart=no', '--property=RemainAfterExit=yes', '--property=CPUQuota=150%', '--property=MemoryMax=1G', '--property=TasksMax=128',
            '--property=RuntimeMaxSec=600', '--property=TimeoutStopSec=15', '--property=KillMode=control-group', '--property=UMask=0077',
            '--property=PrivateTmp=yes', '--property=NoNewPrivileges=yes', '--property=ProtectSystem=strict', '--property=ReadWritePaths=' + str(ROOT),
            '--property=IPAddressDeny=any', '--property=IPAddressAllow=localhost', '--property=UnsetEnvironment=DEBUG PWDEBUG NODE_OPTIONS NODE_PATH',
            '--setenv=HOME=/root', '--setenv=LANG=C.UTF-8', '--property=StandardOutput=append:' + str(ROOT / 'node.stdout'),
            '--property=StandardError=append:' + str(ROOT / 'node.stderr'), '--', *self.node_arguments()]
        self.save('launch-intent.json', {'unit': WORKER_UNIT, 'arguments': arguments, 'input_sha256': self.input_sha, 'node_sha256': self.args.node_sha256})
        self.launched = True
        result = subprocess.run(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False, env=ENV)
        self.save('launch-result.json', {'returncode': result.returncode, 'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)})
        require(result.returncode == 0, 'The one worker launch failed; no retry is allowed.')
        until = min(self.deadline, time.monotonic() + 15)
        while time.monotonic() < until:
            status = self.properties()
            if int(status.get('MainPID', '0')) > 1:
                self.capture_child(status)
                return
            require(status.get('ActiveState') not in ('failed', 'inactive'), 'The worker exited before live ownership was captured.')
            time.sleep(0.1)
        raise ObservationError('The live worker process could not be captured within its bound.')

    def ipc_read(self, path):
        require(path.parent in (ROOT, BROWSER_ROOT) and re.fullmatch(r'(stage-[a-z-]+|control-[a-z-]+|abort)\.json', path.name),
                'An IPC read escaped its exact directories.')
        pending = path.with_name(path.name + '.pending')
        def optional(filename):
            try:
                return filename.lstat()
            except FileNotFoundError:
                return None
        first, temporary, last = optional(path), optional(pending), optional(path)
        # Non-overwrite publication can legitimately add a link or remove the
        # pending link between lstat calls. A changing snapshot waits once; a
        # stable incompatible pair is rejected rather than normalized.
        changed = (identity(first) if first is not None else None) != (identity(last) if last is not None else None)
        if changed:
            for info in (first, temporary, last):
                if info is not None:
                    require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
                        0 <= info.st_size <= MAX_IPC and info.st_nlink in (1, 2), 'An IPC publication changed into an unsafe file.')
            state = 'publishing'
        else:
            state = publication_state(last, temporary)
        if state == 'missing':
            return None
        if state == 'publishing':
            started = self.publications.setdefault(str(path), time.monotonic())
            require(time.monotonic() - started <= 5, 'An IPC publication did not complete within its fixed bound.')
            return None
        raw = protected(path, limit=MAX_IPC)
        require(identity(path.lstat()) == identity(last), 'A ready IPC changed after the stable publication snapshot.')
        self.publications.pop(str(path), None)
        value = decode(raw)
        return {'path': str(path), 'sha256': sha(raw), 'value': value}

    def own_browser(self, current):
        require(self.child is not None, 'No captured browser process can own a session.')
        path = BROWSER_ROOT / 'session-private.json'
        raw = protected(path, limit=32768)
        private = decode(raw)
        token, row = validate_private_session(private, self.input, self.input_sha, self.child, self.before, current)
        if self.browser_private is not None:
            require(same(private, self.browser_private), 'The private browser ownership proof changed.')
        self.browser_private, self.browser_token, self.browser = private, token, private['proof']
        self.records['browser-session-private.json'] = {'path': str(path), 'sha256': sha(raw)}
        return row

    def stage(self, name, seconds):
        require(name == STAGES[len(self.stages)], 'A stage was replayed or requested out of order.')
        until = min(self.deadline - 60, time.monotonic() + seconds)
        while time.monotonic() < until:
            self.worker_live()
            aborted = self.ipc_read(BROWSER_ROOT / 'abort.json')
            require(aborted is None, 'The browser published a failure before its expected stage.')
            record = self.ipc_read(BROWSER_ROOT / ('stage-' + name + '.json'))
            if record is not None:
                current = self.snapshot('stage-' + name + '-full.json')
                self.own_browser(current)
                active_ids = {self.browser['session_id']} | ({self.administrator['id']} if self.administrator else set())
                require(all(row['revoked_at'] is None for row in current['database']['tables']['sessions'] if row['id'] in active_ids),
                        'An observation stage lost one of its newly owned active credentials.')
                previous = self.controls[-1]['sha256'] if self.controls else None
                observation = validate_stage(record['value'], self.input, self.input_sha, self.child, name, previous, self.browser)
                require(record['value']['session_private'] == self.records['browser-session-private.json'], 'The stage names another private session record.')
                phase = {'discovery': 'original', 'armed': 'original', 'restore-armed': 'forward', 'restored': 'restored'}[name]
                validate_ledger(self.before, current, self.profile, self.reservation, self.browser, self.administrator,
                    phase=phase, authenticated=self.authenticated)
                if self.stages:
                    discovery = self.stages[0]['value']['observation']
                    boundary = observation.get('boundary')
                    if boundary:
                        require(boundary['route'] == discovery['dom']['route'] and boundary['document_id'] == discovery['dom']['document_id'] and
                            boundary['connection_id'] == discovery['socket']['connection_id'] and
                            boundary['websocket_seen'] == discovery['socket']['seen'] and boundary['websocket_opened'] == discovery['socket']['opened'],
                            'The armed route, document or socket changed since discovery.')
                if name in ('restore-armed', 'restored'):
                    window_name = 'forward' if name == 'restore-armed' else 'restored'
                    window, control = observation[window_name], self.controls[-1]
                    original_boundary = self.stages[-1]['value']['observation']['boundary']
                    require(set(window['boundary']) == set(original_boundary) | {'response_completed_elapsed_ms', 'end_elapsed_ms',
                        'completed_elapsed_ms', 'duration_ms'} and all(same(window['boundary'][key], value) for key, value in original_boundary.items()),
                        'The completed window does not extend its exact previously accepted armed boundary.')
                    require(window['control_sha256'] == control['sha256'] and same(window['commit'], control['value']['commit']) and
                        same(window_evidence(window, self.input, self.reservation['public']),
                             {key: window.get(key) for key in ('result', 'outcome', 'proof')}) and window['result'] == 'passed',
                        'Independent event, automatic HTTP and visible DOM evidence did not pass.')
                    if name == 'restored':
                        earlier = self.stages[-1]['value']['observation']['forward']
                        require(window['proof']['message_id'] != earlier['proof']['message_id'] and all(window['boundary'][key] == earlier['boundary'][key]
                            for key in ('route', 'document_id', 'connection_id', 'token_sha256')), 'The two windows do not prove distinct changes on the same document.')
                self.check()
                self.stages.append({'name': name, **record})
                self.save('accepted-stage-' + name + '.json', record)
                return observation
            time.sleep(0.1)
        raise ObservationError('The browser did not publish its bounded expected stage.')

    def publish_control(self, name, commit=None, restoration='pending', previous=None):
        require(name in CONTROLS and not any(value['name'] == name for value in self.controls), 'A control cannot be replayed.')
        if name != 'close':
            expected = {'reserved': 'discovery', 'forward': 'armed', 'restored': 'restore-armed'}[name]
            require(self.stages and self.stages[-1]['name'] == expected and name == CONTROLS[len(self.controls)], 'A control crossed its stage barrier.')
            previous = self.stages[-1]['sha256']
        require(name != 'close' or restoration in ('confirmed', 'not_required'), 'An unresolved restoration cannot release the browser as complete.')
        value = {'marker': 'goby-client-library-changed-control-v1', 'version': 1, 'input_sha256': self.input_sha,
            'source_closure_sha256': sha(canonical(self.js_sources)), 'controller': self.input['controller'], 'node_process': self.child,
            'name': name, 'previous_stage_sha256': previous, 'reservation': self.reservation['public'] if self.reservation else None,
            'commit': commit, 'restoration': restoration}
        row = self.save('control-' + name + '.json', value, ipc=True)
        self.controls.append({'name': name, **row, 'value': value})
        return row

    def capture_native_cookie(self, cookie):
        require(isinstance(cookie, str) and len(cookie) <= 4096 and
            re.fullmatch(r'goby_session=[A-Za-z0-9_-]{43};[^\r\n]*', cookie), 'Native login has no unique bounded received cookie.')
        candidate = cookie.split(';', 1)[0]
        require(self.cookie is None or self.cookie == candidate, 'Native login attempted to replace its already received cookie.')
        # Header receipt is not a successful login. Retain this exact token so
        # an incomplete body can still be matched to one new owned session for
        # DELETE/exact401 cleanup. Never discover tokens from database rows.
        self.cookie = candidate
        self.csrf = sha(('goby:admin:csrf:' + candidate.split('=', 1)[1]).encode())

    def capture_native_credentials(self, data, cookie):
        require(isinstance(data, dict) and set(data) == {'User', 'CSRFToken'} and isinstance(cookie, str) and
            re.fullmatch(r'goby_session=[A-Za-z0-9_-]{43};[^\r\n]*', cookie) and safe_text(data.get('CSRFToken'), 256),
            'The native login omitted its bounded cookie or CSRF proof.')
        user = next(row for row in self.before['database']['tables']['users'] if row['id'] == ADMIN)
        public = data['User']
        require(isinstance(public, dict) and set(public) == {'Id', 'Name', 'IsAdministrator', 'IsDisabled', 'HasPassword', 'CreatedAt'} and
            public['Id'] == ADMIN and public['Name'] == user['name'] and public['IsAdministrator'] is True and public['IsDisabled'] is False and
            public['HasPassword'] is True and instant(public['CreatedAt']) == instant(user['created_at']), 'The native login belongs to another account.')
        self.capture_native_cookie(cookie)
        require(data['CSRFToken'] == self.csrf,
                'The native CSRF value does not derive from the exact received cookie.')
        self.native_login_validated = True

    def native(self, label, body=None, expected=200):
        self.check()
        method, path = self.fence.REQUESTS[label]
        payload = None if body is None else canonical(body)
        require(payload is None or len(payload) <= (1 << 20), 'A native request body exceeds its bound.')
        if label != 'login':
            require(self.cookie is not None and self.csrf is not None and self.administrator is not None, 'A native request lacks independently owned authentication.')
            require(label in ('logout', 'exact401') or self.native_login_validated,
                    'An incomplete native login authorizes only independently owned session cleanup.')
        if method == 'PUT':
            require(self.reservation is not None and same(body, self.reservation['forward_body' if label == 'forward' else 'restore_body']),
                    'A metadata PUT differs from the exact privately retained reservation.')
        self.fence.reserve(label, method, path)
        self.save('native-' + label + '-intent.json', {'label': label, 'method': method, 'path': path,
            'body_sha256': sha(payload) if payload is not None else None, 'body_bytes': len(payload) if payload else 0,
            'session_id': self.administrator['id'] if self.administrator else None, 'candidate_process': self.candidate['process'],
            'input_sha256': self.input_sha})
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Origin': BASE_URL, 'Connection': 'close'}
        if payload is not None:
            headers['Content-Type'] = 'application/json'
        if self.cookie is not None:
            headers.update({'Cookie': self.cookie, 'X-CSRF-Token': self.csrf})
        connection, response, raw, complete = None, None, b'', False
        result = {'label': label, 'method': method, 'path': path, 'status': None, 'complete': False}
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(ObservationError('A native exchange exceeded its total deadline.')))
        signal.setitimer(signal.ITIMER_REAL, 12)
        error = None
        try:
            connection = http.client.HTTPConnection('127.0.0.1', 18198, timeout=8)
            self.dispatched.append(label)
            if label == 'forward':
                self.restoration = 'restoration_required'
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            result['status'] = response.status
            if label == 'login' and response.status == 200:
                cookies = [value for key, value in response.getheaders() if key.lower() == 'set-cookie']
                require(len(cookies) == 1, 'Native login has ambiguous received cookies.')
                self.capture_native_cookie(cookies[0])
                self.save('native-cookie-received-private.json', {'marker': MARKER, 'input_sha256': self.input_sha,
                    'controller': self.input['controller'], 'cookie': self.cookie, 'csrf': self.csrf,
                    'token_sha256': sha(self.cookie.split('=', 1)[1].encode()), 'header_status': 200,
                    'successful_login': False, 'requires_independent_new_session_ownership': True})
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= (2 << 20), 'A native response exceeds its announced byte limit.')
            raw = response.read((2 << 20) + 1)
            require(len(raw) <= (2 << 20) and (length is None or len(raw) == int(length)), 'A native response is oversized or incomplete.')
            complete = True
            result.update(complete=True, body_sha256=sha(raw), body_bytes=len(raw), write_completed_at=dt.datetime.now(dt.timezone.utc).isoformat())
            require(response.status == expected and (expected != 204 or raw == b''), 'The native response did not return the exact expected status.')
            data = decode(raw) if raw else None
            if label == 'login':
                require(len([value for key, value in response.getheaders() if key.lower() == 'set-cookie']) == 1, 'Native login has ambiguous cookies.')
                self.capture_native_credentials(data, response.getheader('Set-Cookie'))
            elif label != 'logout':
                require(response.getheader('Set-Cookie') is None, 'A metadata read/write unexpectedly replaced authentication.')
            if path == NativeFence.PATH:
                require(response.getheader('Content-Type', '').split(';', 1)[0].strip().lower() == 'application/json',
                        'A successful native metadata response has another content type.')
                validate_metadata_document(data, self.profile)
            if label in ('forward', 'restore'):
                setattr(self, label + '_ack', True)
        except Exception as caught:
            error = caught
            result['failure_type'] = type(caught).__name__
            partial = getattr(caught, 'partial', None)
            if isinstance(partial, bytes) and len(partial) <= (2 << 20):
                raw = partial
                result.update(body_sha256=sha(raw), body_bytes=len(raw))
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous)
            if connection is not None:
                connection.close()
            # Authentication secrets have a separate private ownership record.
            # Other bounded raw bodies are retained only inside this 0700 root.
            if label != 'login':
                result['raw_body_utf8'] = raw.decode('utf-8', errors='replace')
            result['complete'] = complete
            self.native_results[label] = result
        record = self.save('native-' + label + '-result.json', result)
        result['record'] = record
        if error is not None:
            raise error
        self.check()
        return data

    def own_administrator(self, current):
        require(self.cookie is not None, 'No received native cookie can own a new session.')
        token = self.cookie.split('=', 1)[1]
        token_hash = '\\x' + sha(token.encode())
        old = self.before['database']['tables']['sessions']
        matches = [row for row in current['database']['tables']['sessions'] if row['token_hash'] == token_hash and
            row['id'] not in {value['id'] for value in old} and token_hash not in {value['token_hash'] for value in old}]
        require(len(matches) == 1, 'The received native cookie does not identify one fresh session.')
        row = matches[0]
        require(row['user_id'] == ADMIN and row['kind'] == 'admin' and row['client_name'] == 'Goby Dashboard' and
            row['device_id'] == 'goby-dashboard' and row['device_name'] == 'Web browser' and
            row['device_registry_id'] is None and row['client_capabilities'] == {} and row['revoked_at'] is None and
            row['created_at'] == row['last_seen_at'], 'The received native session lacks its exact new client issuance shape.')
        require(isinstance(row['id'], str) and ID.fullmatch(row['id']) and
            set(row) == set(current['database']['metadata']['columns']['sessions']) and
            instant(self.before['database']['metadata']['captured_at']) <= instant(row['created_at']) <= instant(current['database']['metadata']['captured_at']) and
            instant(row['expires_at']) == instant(row['created_at']) + dt.timedelta(days=1), 'The provisional native issuance is outside this exact fresh lifetime.')
        login_audits = [value for value in current['database']['tables']['activity_entries'] if value['action'] == 'session.login' and
            value['resource_id'] == row['id']]
        require(len(login_audits) == 1 and all(same(login_audits[0].get(key), value) for key, value in {
            'severity': 'Info', 'source': 'native', 'actor_kind': 'user', 'actor_id': ADMIN, 'actor_credential_id': row['id'],
            'resource_kind': 'session', 'resource_id': row['id'], 'request_id': '', 'revision': 0, 'affected_count': 1,
            'state': '', 'changed_fields': [], 'previous_revision': 0, 'observation_fingerprint': ''}.items()) and
            type(login_audits[0].get('previous_revision')) is int and type(login_audits[0]['revision']) is int and
            type(login_audits[0]['affected_count']) is int and instant(row['created_at']) <= instant(login_audits[0]['created_at']) <=
            instant(current['database']['metadata']['captured_at']), 'The provisional native token lacks its exact new login audit.')
        if self.administrator is not None:
            require(same(self.administrator, row), 'The native issuance row changed before it was established.')
        self.administrator = copy.deepcopy(row)
        validate_ledger(self.before, current, self.profile, self.reservation, self.browser, self.administrator)
        self.authenticated = copy.deepcopy(current)
        if 'native-session-private.json' not in self.records:
            self.save('native-session-private.json', {'marker': MARKER, 'input_sha256': self.input_sha, 'controller': self.input['controller'],
                'session_id': row['id'], 'user_id': ADMIN, 'token_sha256': sha(token.encode()), 'cookie': self.cookie, 'csrf': self.csrf})

    def metadata_checkpoint(self, label, expected_phase):
        document = self.native(label)
        current = self.snapshot(label + '-full.json')
        require(classify_metadata(document, current, self.profile, self.reservation, self.administrator['id']) == expected_phase,
                'The native complete read and exact owned target ledger disagree.')
        validate_ledger(self.before, current, self.profile, self.reservation, self.browser, self.administrator,
            phase=expected_phase, authenticated=self.authenticated)
        return document, current

    def commit(self, label, current):
        result = self.native_results[label]
        require(result.get('status') == 200 and result.get('complete') is True and result.get('record') is not None,
                'A metadata commit lacks its durable successful response.')
        record_name = ('forward-readback' if label == 'forward' else 'restore-readback') + '-full.json'
        row = next(value for value in current['database']['tables']['item_metadata_state'] if value['item_id'] == ITEM)
        return {'revision': str(row['revision']), 'write_completed_at': result['write_completed_at'],
            'native_result_sha256': result['record']['sha256'], 'readback_sha256': self.records[record_name]['sha256']}

    def execute(self):
        self.phase = 'launch'
        self.launch()
        self.phase = 'discovery'
        self.stage('discovery', 90)
        self.native('login', {'Name': self.credentials['admin']['username'], 'Password': self.credentials['admin']['password']})
        authenticated = self.snapshot('authenticated-full.json')
        self.own_administrator(authenticated)
        original = self.native('original')
        original_snapshot = self.snapshot('original-full.json')
        validate_ledger(self.before, original_snapshot, self.profile, browser=self.browser, administrator=self.administrator,
            authenticated=self.authenticated)
        self.reservation = reserve_edit(original, self.profile)
        self.save('metadata-reservation-private.json', self.reservation)
        self.publish_control('reserved')
        self.phase = 'armed'
        self.stage('armed', 60)
        self.native('forward', self.reservation['forward_body'])
        _, forward = self.metadata_checkpoint('forward-readback', 'forward')
        forward_commit = self.commit('forward', forward)
        self.publish_control('forward', forward_commit)
        self.phase = 'restore-armed'
        self.stage('restore-armed', 145)
        self.metadata_checkpoint('pre-restore', 'forward')
        self.native('restore', self.reservation['restore_body'])
        _, restored = self.metadata_checkpoint('restore-readback', 'restored')
        restored_commit = self.commit('restore', restored)
        self.restoration = 'confirmed'
        self.publish_control('restored', restored_commit, 'confirmed')
        self.phase = 'restored'
        self.stage('restored', 145)
        self.publish_control('close', restored_commit, 'confirmed', self.stages[-1]['sha256'])
        self.phase = 'cleanup'

    def failure_snapshot(self, name):
        # Retain the actual complete read before applying semantic validators.
        value = self.op.preservation_snapshot(self.state, 28)
        self.save(name, value)
        return value

    def publish_abort(self):
        if self.child is None or self.input is None or 'abort.json' in self.records:
            return
        self.save('abort.json', {'marker': 'goby-client-library-changed-abort-v1', 'version': 1,
            'input_sha256': self.input_sha, 'source_closure_sha256': sha(canonical(self.js_sources)),
            'controller': self.input['controller'], 'node_process': self.child, 'name': self.phase,
            'failure': 'library_changed_controller_failed', 'previous_stage_sha256': self.stages[-1]['sha256'] if self.stages else None,
            'previous_control_sha256': self.controls[-1]['sha256'] if self.controls else None,
            'token_sha256': self.browser['token_sha256'] if self.browser else None,
            'session_private': self.records.get('browser-session-private.json')}, ipc=True)

    def reconcile(self, label):
        document = self.native(label)
        current = self.snapshot(label + '-full.json')
        classification = classify_metadata(document, current, self.profile, self.reservation, self.administrator['id'])
        if classification != 'foreign':
            validate_ledger(self.before, current, self.profile, self.reservation, self.browser, self.administrator,
                phase=classification, authenticated=self.authenticated)
        number = self.profile['metadata']['revision']
        owned_forward = owned_metadata_audit(current, self.administrator['id'], number + 1)
        owned_restore = owned_metadata_audit(current, self.administrator['id'], number + 2)
        decision = restore_decision('forward' in self.dispatched, self.forward_ack, 'restore' in self.dispatched,
            classification, owned_forward, owned_restore)
        record = self.save(label + '-decision.json', {'classification': classification, 'decision': decision,
            'owned_forward': owned_forward, 'owned_restore': owned_restore, 'forward_dispatched': 'forward' in self.dispatched,
            'restore_dispatched': 'restore' in self.dispatched, 'missing_ack_is_not_a_completion_barrier': True,
            'snapshot': self.records[label + '-full.json']})
        return decision, current, record

    def reconciled_commit(self, current, decision_record, snapshot_record):
        target_checkpoint(current, self.profile, self.reservation, 'restored', self.administrator['id'])
        self.restoration = 'confirmed'
        return {'revision': str(self.profile['metadata']['revision'] + 2),
            'write_completed_at': dt.datetime.now(dt.timezone.utc).isoformat(),
            'native_result_sha256': decision_record['sha256'], 'readback_sha256': snapshot_record['sha256']}

    def abort_predecessor(self):
        # A stage may have reached the worker's durable file while controller
        # validation failed. Its binding, not its passed flag, establishes the
        # close linkage. Invalid IPC never authorizes a close control.
        previous = None
        for name in STAGES:
            record = self.ipc_read(BROWSER_ROOT / ('stage-' + name + '.json'))
            if record is None:
                break
            value = record['value']
            require(value.get('marker') == 'goby-client-library-changed-stage-v1' and type(value.get('version')) is int and value['version'] == 1 and
                value.get('name') == name and value.get('input_sha256') == self.input_sha and
                value.get('source_closure_sha256') == sha(canonical(self.js_sources)) and same(value.get('controller'), self.input['controller']) and
                same(value.get('node_process'), self.child), 'The abort close predecessor has another process or input binding.')
            previous = record['sha256']
        aborted = self.ipc_read(BROWSER_ROOT / 'abort.json')
        if aborted is not None:
            value = aborted['value']
            require(isinstance(value, dict) and set(value) == {'marker', 'version', 'input_sha256', 'source_closure_sha256', 'controller',
                'node_process', 'name', 'failure', 'previous_stage_sha256', 'previous_control_sha256', 'token_sha256', 'session_private'} and
                value['marker'] == 'goby-client-library-changed-abort-v1' and type(value['version']) is int and value['version'] == 1 and
                value['input_sha256'] == self.input_sha and value['source_closure_sha256'] == sha(canonical(self.js_sources)) and
                same(value['controller'], self.input['controller']) and same(value['node_process'], self.child) and
                (value['previous_stage_sha256'] is None or isinstance(value['previous_stage_sha256'], str) and HASH.fullmatch(value['previous_stage_sha256'])) and
                (self.browser is None and value['token_sha256'] is None or self.browser is not None and value['token_sha256'] == self.browser['token_sha256']),
                'The worker abort has another input, actor or process binding.')
            # The driver's signed-by-process attempt hash covers an interrupted
            # publication, while its private binding still proves this actor.
            previous = value['previous_stage_sha256']
            self.records['browser-abort.json'] = {'path': aborted['path'], 'sha256': aborted['sha256']}
        return previous

    def recover(self):
        try:
            self.publish_abort()
        except Exception as error:
            self.error('controller_abort_publication', error)
        current = None
        try:
            self.check()
            current = self.failure_snapshot('failure-before-cleanup-full.json')
            if self.child is not None and os.path.lexists(BROWSER_ROOT / 'session-private.json'):
                self.own_browser(current)
            if self.cookie is not None and self.administrator is None:
                self.own_administrator(current)
        except Exception as error:
            self.error('failure_ownership', error)
        commit = None
        if 'forward' in self.dispatched:
            self.restoration = 'restoration_required'
            try:
                require(self.reservation is not None and self.administrator is not None, 'A dispatched edit lacks an exact owned restoration reservation.')
                decision, observed, proof = self.reconcile('reconcile-one')
                if decision == 'restore_once':
                    # The complete public controls, immutable source fields,
                    # full row delta and new credential audit were all checked.
                    # A sent or merely reserved restore intent is never replayed.
                    require('restore' not in self.fence.reserved, 'The one restoration intent was already consumed.')
                    try:
                        self.native('restore', self.reservation['restore_body'])
                    except Exception as error:
                        self.error('conditional_restore_response', error)
                    decision, observed, proof = self.reconcile('reconcile-two')
                    if decision == 'restored':
                        commit = self.reconciled_commit(observed, proof, self.records['reconcile-two-full.json'])
                elif decision == 'restored':
                    commit = self.reconciled_commit(observed, proof, self.records['reconcile-one-full.json'])
                if commit is None:
                    self.error('restoration_required', ObservationError('The public revision and owned audit do not prove completed restoration; no further write is authorized.'))
            except Exception as error:
                self.error('conditional_restoration', error)
        else:
            self.restoration = 'not_required'
        if self.child is not None and self.restoration in ('confirmed', 'not_required') and not any(value['name'] == 'close' for value in self.controls):
            try:
                self.check()
                self.publish_control('close', commit, self.restoration, self.abort_predecessor())
            except Exception as error:
                self.error('failure_close_publication', error)

    def close_native(self):
        if self.administrator is None:
            return
        # A failed exchange consumes its intent. Retain missing ACK and never
        # retry DELETE; a following exact-token read is independently bounded.
        if 'logout' not in self.fence.reserved:
            try:
                self.native('logout', expected=204)
            except Exception as error:
                self.error('native_logout', error)
        if 'logout' in self.dispatched and 'exact401' not in self.fence.reserved:
            try:
                self.native('exact401', expected=401)
            except Exception as error:
                self.error('native_exact401', error)

    def cgroup_empty(self):
        group = Path('/sys/fs/cgroup' + CGROUP)
        if not group.exists():
            return True
        paths = [group, *group.rglob('*')]
        require(len(paths) <= 256 and all(not value.is_symlink() for value in paths), 'The owned worker cgroup exceeds its membership bound.')
        return all(not value.read_text().strip() for value in paths if value.name == 'cgroup.procs')

    def await_worker(self):
        while time.monotonic() < self.deadline - 25:
            status = self.properties()
            if self.child is None and int(status.get('MainPID', '0')) > 1:
                self.capture_child(status)
            if self.child is not None:
                require(status.get('InvocationID') == self.invocation, 'The worker unit invocation changed before terminal collection.')
                if int(status.get('MainPID', '0')) > 1:
                    try:
                        current_process = proc_identity(int(status['MainPID']))
                    except FileNotFoundError:
                        # systemd may still expose the old MainPID during the
                        # kernel exit transition. Re-read its state within the
                        # existing deadline; never infer terminal ownership.
                        time.sleep(0.1)
                        continue
                    require(same(current_process, {key: self.child[key] for key in ('pid', 'start_ticks', 'boot_id')}),
                            'The worker restarted or changed process before exit.')
            if self.child is not None and status.get('MainPID') == '0' and self.cgroup_empty():
                require(status.get('ActiveState') in ('inactive', 'failed') or
                    status.get('ActiveState') == 'active' and status.get('SubState') == 'exited',
                    'The zero-PID worker has not reached an exited or inactive state.')
                # The process tree is already closed independently of its
                # optional transient-unit release journal. A later release or
                # journal failure must not discard safe owned-token cleanup.
                self.closed = True
                self.terminal = {**status, 'cgroup_empty': True}
                if status.get('ActiveState') == 'active' and status.get('SubState') == 'exited':
                    self.save('worker-exited.json', status)
                    require(not self.release_attempted, 'The already-exited worker release intent was already consumed.')
                    self.save('worker-release-intent.json', {'unit': WORKER_UNIT, 'invocation_id': self.invocation,
                        'process': self.child, 'MainPID': '0', 'cgroup_empty': True})
                    self.release_attempted = True
                    result = subprocess.run(['/usr/bin/systemctl', 'stop', WORKER_UNIT], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                        timeout=15, check=False, env=ENV)
                    self.save('worker-release-result.json', {'returncode': result.returncode, 'stdout_sha256': sha(result.stdout), 'stderr_sha256': sha(result.stderr)})
                    require(result.returncode == 0, 'The one already-exited worker release failed.')
                    ended = self.properties()
                    require(ended.get('MainPID') == '0' and ended.get('ActiveState') in ('inactive', 'failed') and self.cgroup_empty(),
                            'The released worker does not have a terminal empty cgroup.')
                    self.terminal = {**status, 'ActiveState': ended['ActiveState'], 'SubState': ended['SubState'],
                        'cgroup_empty': True, 'release_returncode': result.returncode}
                elif status.get('ActiveState') in ('inactive', 'failed'):
                    self.terminal = {**status, 'cgroup_empty': True}
                else:
                    raise ObservationError('The worker has an ambiguous zero-PID state.')
                self.closed = True
                self.save('worker-terminal.json', self.terminal)
                return
            time.sleep(0.5)
        raise ObservationError('The worker did not reach an independently observed terminal empty cgroup within its deadline.')

    def read_browser(self, current):
        require(self.closed and self.child is not None, 'The worker can still act; final browser evidence is not stable.')
        if os.path.lexists(BROWSER_ROOT / 'session-private.json'):
            self.own_browser(current)
        raw = protected(BROWSER_ROOT / 'report.json', limit=MAX_REPORT)
        report = decode(raw)
        require(report.get('marker') == 'goby-client-library-changed-report-v1' and type(report.get('version')) is int and report['version'] == 1 and
            report.get('mode') == MODE and report.get('input_sha256') == self.input_sha and
            report.get('source_closure_sha256') == sha(canonical(self.js_sources)) and same(report.get('controller'), self.input['controller']) and
            same(report.get('node_process'), self.child) and same(report.get('candidate'), self.input['candidate']) and
            same(report.get('authority'), self.input['authority']) and same(report.get('target'), self.input['target']) and
            report.get('client_acceptance') is False and report.get('full_m3_complete') is False and
            same(report.get('login_proof'), self.browser) and report.get('session_private') == self.records.get('browser-session-private.json'),
            'The final browser report is outside this exact actor, input, worker or acceptance scope.')
        self.node_report = report
        self.records['browser-report.json'] = {'path': str(BROWSER_ROOT / 'report.json'), 'sha256': sha(raw)}
        cap = report.get('capabilities_private')
        require(isinstance(cap, dict) and set(cap) == {'path', 'sha256', 'request_count', 'last_successful_body_sha256'} and
            cap['path'] == str(BROWSER_ROOT / 'capabilities-private.json') and HASH.fullmatch(cap['sha256']), 'The final capabilities descriptor is unowned.')
        document = read_record({key: cap[key] for key in ('path', 'sha256')}, limit=MAX_IPC)
        self.capabilities = capability_values(document, self.browser, self.input, self.input_sha, self.child)
        entries = document['entries']
        require(type(cap['request_count']) is int and cap['request_count'] == len(entries) and
            all(value.get('completed') is True and value.get('status') == 204 for value in entries) and
            cap['last_successful_body_sha256'] == (entries[-1]['body_sha256'] if entries else None) and
            report.get('actor', {}).get('proxy', {}).get('capabilities') == len(entries), 'A capability effect lacks a complete matching observed request.')
        self.records['browser-capabilities-private.json'] = {key: cap[key] for key in ('path', 'sha256')}

    def fallback(self, current):
        require(self.closed and self.browser is not None and self.browser_token is not None and not self.fallback_attempted,
                'The B fallback has no closed worker and independently owned credential.')
        row = owned_session(self.before, current, self.browser)
        self.check()
        status = self.properties()
        require(status.get('MainPID') == '0' and self.cgroup_empty() and
            (status.get('LoadState') == 'not-found' or status.get('InvocationID') == self.invocation and
             (status.get('ActiveState') in ('inactive', 'failed') or status.get('ActiveState') == 'active' and status.get('SubState') == 'exited')),
            'The exact worker is no longer independently closed before B fallback.')
        require(read_record(self.records['cleanup-reservation.json'])['input_sha256'] == self.input_sha, 'The original cleanup reservation changed.')
        self.fallback_attempted = True
        self.save('browser-fallback-intent.json', {'session_id': row['id'], 'token_sha256': self.browser['token_sha256'],
            'already_revoked': row['revoked_at'] is not None, 'maximum_requests': 2, 'acceptance': False})
        previous = signal.getsignal(signal.SIGALRM)
        signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(ObservationError('The owned B cleanup exceeded its total deadline.')))
        signal.setitimer(signal.ITIMER_REAL, 20)
        results = []
        until = time.monotonic() + 20
        try:
            for method, path, expected in ([] if row['revoked_at'] is not None else [('POST', '/emby/Sessions/Logout', 204)]) + [('GET', '/emby/System/Info', 401)]:
                require(time.monotonic() < until, 'The B cleanup total deadline expired before its next reserved exchange.')
                result = {'method': method, 'path': path, 'status': None, 'complete': False}
                connection = http.client.HTTPConnection('127.0.0.1', 18198, timeout=min(8, max(0.1, until - time.monotonic())))
                try:
                    connection.request(method, path, None, {'X-Emby-Token': self.browser_token, 'Origin': BASE_URL,
                        'Accept': 'application/json', 'Accept-Encoding': 'identity', 'Connection': 'close'})
                    response = connection.getresponse()
                    result['status'] = response.status
                    length = response.getheader('Content-Length')
                    require(length is None or length.isdigit() and int(length) <= 8192, 'The owned cleanup response exceeds its announced limit.')
                    raw = response.read(8193)
                    require(len(raw) <= 8192 and (length is None or len(raw) == int(length)), 'The owned cleanup response is incomplete.')
                    result.update(complete=True, body_sha256=sha(raw), body_bytes=len(raw), raw_body_utf8=raw.decode('utf-8', errors='replace'))
                    require(response.status == expected and (expected != 204 or not raw), 'The owned B cleanup did not return its exact status.')
                except Exception as error:
                    result['failure_type'] = type(error).__name__
                    # A missing logout ACK consumes POST. Still use the one
                    # independently reserved exact-token GET while time remains.
                    # Its result cannot erase the retained POST failure.
                finally:
                    connection.close()
                    results.append(result)
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, previous)
            self.save('browser-fallback-result.json', {'requests': results, 'acceptance': False})
        require(results and results[-1]['status'] == 401 and results[-1]['complete'] is True, 'The exact B token was not proven rejected.')
        require(all('failure_type' not in value for value in results), 'A B cleanup exchange failed despite the later exact-token observation.')

    def validate_observation(self):
        report = self.node_report
        require(report is not None and self.browser is not None and not self.fallback_attempted and report.get('result') == 'passed' and
            report.get('failure') is None and not report.get('abort') and not report.get('controller_abort') and report.get('restoration') == 'confirmed' and
            self.restoration == 'confirmed' and len(self.stages) == 4 and len(self.controls) == 4 and ui_logout_proven(report, self.browser),
            'The original client did not complete the exact automatic refresh and UI cleanup scope.')
        require(same(report.get('stages'), [{key: value[key] for key in ('name', 'path', 'sha256')} for value in self.stages]) and
            same(report.get('controls'), self.controls[:-1]) and
            same(report.get('control_close'), {key: self.controls[-1][key] for key in ('path', 'sha256', 'value')}) and
            same(report.get('discovery'), self.stages[0]['value']['observation']) and same(report.get('armed'), self.stages[1]['value']['observation']) and
            same(report.get('forward'), self.stages[2]['value']['observation']['forward']) and
            same(report.get('restore_armed'), self.stages[2]['value']['observation']['boundary']) and
            same(report.get('restored'), self.stages[3]['value']['observation']['restored']), 'The final report differs from the independently consumed stage/control records.')
        for window in (report['forward'], report['restored']):
            require(same(window_evidence(window, self.input, self.reservation['public']), {key: window[key] for key in ('result', 'outcome', 'proof')}) and
                window['result'] == 'passed', 'The final automatic refresh chain does not independently recompute.')
        actor, closure = report.get('actor', {}), report.get('closure', {})
        observation = report.get('observation', {})
        frames = [value for value in observation.get('frames', []) if value.get('kind') == 'login']
        physical = [value for value in observation.get('physical', []) if value.get('kind') == 'login']
        require(len(frames) == len(physical) == 1 and frames[0].get('finished') is True and not frames[0].get('failed') and
            frames[0].get('status') == 200 and frames[0].get('from_service_worker') is False and frames[0].get('content_type') == 'application/json' and
            physical[0].get('completed') is True and physical[0].get('terminal_status') == 200 and
            frames[0].get('request_sha256') == physical[0].get('request_sha256') and actor.get('login', {}).get('request_count') == 1 and
            actor['login'].get('status') == 200 and actor.get('ordinary_authority_confirmed') is True and actor.get('page_error_count') == 0 and
            actor.get('closed') is True and actor.get('cleanup_failures') == [], 'The browser authentication/cleanup observation is incomplete or repeated.')
        proxy, network, websocket = actor.get('proxy', {}), actor.get('network', {}), actor.get('websocket', {})
        require(proxy.get('login') == proxy.get('logout') == 1 and proxy.get('completed') == proxy.get('admitted') and
            all(type(proxy.get(key)) is int and proxy[key] == 0 for key in ('preparation', 'failed', 'rejected', 'active')) and
            all(type(network.get(key)) is int and network[key] == 0 for key in ('forbidden_mutations', 'playback_attempts', 'observer_errors', 'overflow', 'guard_errors')) and
            type(network.get('external_blocked')) is int and 0 <= network['external_blocked'] <= 2000 and
            websocket.get('failed') == websocket.get('control_attempts') == 0 and report.get('catalog_observation', {}).get('observer_failure') is None,
            'A transport mutation, playback, overflow or observer failure escaped the permitted client scope.')
        require(all(closure.get(key) is True for key in ('context_closed', 'browser_closed', 'proxy_closed')) and
            all(type(closure.get(key)) is int and closure[key] == 0 for key in ('http_pending', 'websocket_pending', 'websocket_active', 'sockets_remaining')) and
            type(closure.get('websocket_opened')) is int and 1 <= closure['websocket_opened'] <= 2 and
            closure.get('websocket_closed') == closure['websocket_opened'] and closure.get('cleanup_failures') == [] and
            self.terminal is not None and self.terminal.get('MainPID') == '0' and self.terminal.get('ExecMainStatus') == '0' and
            self.terminal.get('cgroup_empty') is True, 'The browser, socket or independent worker lifetime remains open.')
        require(all(self.native_results.get(key, {}).get('complete') is True and self.native_results[key].get('status') == code
                    for key, code in (('logout', 204), ('exact401', 401))), 'The new native credential did not complete logout and exact401.')

    def cleanup(self):
        self.close_native()
        if self.launched:
            try:
                self.await_worker()
            except Exception as error:
                self.error('worker_terminal', error)
        current = None
        try:
            current = self.failure_snapshot('closed-worker-full.json')
            if self.closed:
                self.read_browser(current)
        except Exception as error:
            self.error('browser_final_evidence', error)
        if self.closed and current is not None and self.browser is not None and not (self.node_report and ui_logout_proven(self.node_report, self.browser)):
            try:
                self.fallback(current)
            except Exception as error:
                self.error('browser_fallback', error)
        if self.closed:
            for name in ('node.stdout', 'node.stderr'):
                try:
                    raw = protected(ROOT / name, limit=MAX_REPORT)
                    self.records[name] = {'path': str(ROOT / name), 'sha256': sha(raw)}
                except Exception as error:
                    self.error('worker_log_retention', error)
        try:
            self.check()
            self.after = self.failure_snapshot('after-full.json')
            validate_schema28_snapshot(self.after, self.state, self.catalog, self.op)
            phase = 'restored' if self.restoration == 'confirmed' else 'original'
            require(self.restoration != 'restoration_required', 'The unresolved request may still commit; final restoration cannot be claimed.')
            self.delta = validate_ledger(self.before, self.after, self.profile, self.reservation, self.browser, self.administrator,
                phase=phase, capabilities=self.capabilities, authenticated=self.authenticated, closed=True)
            if self.browser is not None:
                require(self.capabilities is not None, 'The new B capabilities lack retained exact successful wire evidence.')
            after_media = self.media_reader.media_witness(self.op, self.state)
            self.save('media-after.json', after_media)
            require(same(after_media, self.media), 'A member of the three preserved media groups changed.')
            self.save('primary-after.json', self.primary_fact())
            require(same(self.primary_fact(), self.primary), 'The primary identity changed at final collection.')
        except Exception as error:
            self.error('final_complete_ledger', error)
        if not self.errors:
            try:
                self.validate_observation()
                require(self.delta == {'new_sessions': 2, 'new_devices': 1, 'new_audits': 6, 'metadata_revision_delta': 2,
                    'old_rows_sequences_private_preserved': True, 'owned_sessions_closed': True}, 'The normal exact 2/1/6/+2 persistent allowance did not hold.')
            except Exception as error:
                self.error('automatic_refresh_acceptance', error)

    def report(self):
        success = not self.errors and self.delta is not None and self.closed and self.restoration == 'confirmed'
        value = {'marker': MARKER, 'version': 1, 'mode': MODE, 'status': 'passed' if success else 'failed', 'phase': self.phase,
            'worker_chain_ledger_passed': success, 'acceptance_ready_for_outer_terminal': success, 'outer_controller_terminal_required': True,
            'library_changed_client_acceptance': False, 'client_acceptance': False, 'full_m3_complete': False,
            'candidate_process': self.candidate['process'], 'candidate_invocation': self.candidate['invocation_id'], 'state_sha256': self.candidate['state_sha256'],
            'input_sha256': self.input_sha, 'source_closure_sha256': sha(canonical(self.js_sources)), 'authority': self.authority,
            'history_preservation': self.prior_ledger,
            'controller': self.input['controller'] if self.input else None, 'node_process': self.child, 'worker_terminal': self.terminal,
            'restoration': self.restoration, 'restoration_required': self.restoration == 'restoration_required',
            'reserved_native_intents': self.fence.reserved, 'dispatched_native_intents': self.dispatched,
            'browser_fallback_used': self.fallback_attempted, 'ledger': self.delta, 'errors': self.errors,
            'evidence': dict(self.records), 'automatic_retry': False, 'sql_business_writes': False, 'candidate_or_primary_service_writes': False,
            'completed_at': dt.datetime.now(dt.timezone.utc).isoformat()}
        self.save('report.json', value)
        return value

    def run(self):
        try:
            self.load()
            if self.args.check_only:
                return {'marker': MARKER, 'status': 'preflight_passed', 'http_requests': 0, 'evidence_writes': 0, 'service_writes': 0,
                    'candidate_process': self.candidate['process'], 'state_sha256': self.candidate['state_sha256'], 'target': self.profile['target'],
                    'current_authority': self.authority['history'][-1]['after_snapshot'], 'upgrade_authority': self.authority['current_snapshot'],
                    'history_preservation': self.prior_ledger, 'library_changed_client_acceptance': False}
            self.prepare()
            try:
                self.execute()
            except Exception as error:
                self.error('execution_' + self.phase, error)
                self.recover()
            self.cleanup()
            return self.report()
        except Exception as error:
            self.error('controller_' + self.phase, error)
            if self.root_fd is not None:
                try:
                    return self.report()
                except Exception as report_error:
                    self.error('failure_report_publication', report_error)
            return {'marker': MARKER, 'status': 'failed', 'errors': self.errors, 'library_changed_client_acceptance': False,
                'evidence_retained': str(ROOT) if self.root_fd is not None else None, 'automatic_retry': False}
        finally:
            if self.root_fd is not None:
                os.close(self.root_fd)
            if self.lock is not None:
                os.close(self.lock)


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--driver', type=Path, required=True)
    parser.add_argument('--source-closure', type=Path, required=True)
    parser.add_argument('--source-closure-sha256', required=True)
    parser.add_argument('--authority', type=Path, required=True)
    parser.add_argument('--authority-sha256', required=True)
    parser.add_argument('--node', type=Path, required=True)
    parser.add_argument('--node-sha256', required=True)
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args(argv)
    require(all(required_digest(value) for value in (args.script_sha256, args.source_closure_sha256, args.node_sha256, args.authority_sha256)) and
        args.authority == TOOL / 'authority.json' and
        args.driver == TOOL / JS_NAMES[0] and args.source_closure.parent == TOOL and args.node.is_absolute() and '..' not in args.node.parts,
        'Explicit reviewed source and Node pins are required; no fallback paths are permitted.')
    return args


def main():
    try:
        result = Run(arguments()).run()
        # Print only a redacted terminal summary. Tokens, passwords, native
        # bodies and complete row snapshots stay in their private files.
        print(canonical({key: result[key] for key in ('marker', 'status', 'library_changed_client_acceptance')}).decode())
        return 0 if result['status'] in ('passed', 'preflight_passed') else 1
    except Exception as error:
        print(canonical({'marker': MARKER, 'status': 'failed', 'failure_type': type(error).__name__,
            'library_changed_client_acceptance': False}).decode())
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
