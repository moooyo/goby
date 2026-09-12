#!/usr/bin/env python3
"""Guard the new LibraryChanged controller using only memory fixtures.

The sibling controller and this guard are the only project sources read. The
controller contributes definitions only; no historical controller is imported.
Private JSON callbacks are prepared before the effect fence. Every test keeps
filesystem, process, network, import, and dynamic-code operations fenced unless
an explicit memory adapter owns that boundary.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime
from decimal import Decimal
import fcntl
import hashlib
import http.client
import importlib.util
import io
import json
import json.decoder
import json.scanner
import linecache
import math
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unicodedata
import urllib.parse
import unittest
from unittest.mock import Mock, call, patch


sys.dont_write_bytecode = True


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("Duplicate JSON object member.")
        result[key] = value
    return result


def invalid_constant(_value):
    raise ValueError("Nonfinite JSON numbers are forbidden.")


def make_memory_json_decoder():
    decoder = json.JSONDecoder(object_pairs_hook=unique_object, parse_float=Decimal,
                               parse_constant=invalid_constant)
    decoder.parse_string = json.decoder.py_scanstring
    object_globals = dict(json.decoder.JSONObject.__globals__)
    object_globals["scanstring"] = json.decoder.py_scanstring
    decoder.parse_object = types.FunctionType(json.decoder.JSONObject.__code__, object_globals,
        "memory_json_object", json.decoder.JSONObject.__defaults__, json.decoder.JSONObject.__closure__)
    decoder.scan_once = json.scanner.py_make_scanner(decoder)
    return decoder


MEMORY_JSON_DECODER = make_memory_json_decoder()


def memory_json_decode(raw):
    if isinstance(raw, bytes):
        raw = raw.decode("utf-8")
    return MEMORY_JSON_DECODER.decode(raw)


class MemoryPath(PurePosixPath):
    """Keep path composition independent from runtime platform imports."""

    def __init__(self, *segments):
        paths = []
        for segment in segments:
            if isinstance(segment, PurePosixPath):
                paths.extend(segment._raw_paths)
            elif type(segment) is str:
                paths.append(segment)
            else:
                raise TypeError("Memory paths accept only strings or trusted POSIX paths.")
        self._raw_paths = paths

    def with_segments(self, *segments):
        return type(self)(*segments)


OPERATOR_PATH = Path(__file__).with_name("observe-client-library-changed-reference-ui.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
ACTIVE_FENCES = []
ORIGINAL_HELP_FORMATTER_INIT = argparse.HelpFormatter.__init__


def deny_effect(label):
    for fence in ACTIVE_FENCES:
        fence.violations.append(label)
    raise AssertionError("An unfaked external effect was attempted: " + label)


def denied(*_args, **_kwargs):
    deny_effect("external API")


def audit_effect(event, arguments):
    if not ACTIVE_FENCES:
        return
    fence = ACTIVE_FENCES[-1]
    if event == "exec" and fence.allowed_code is not None and arguments and arguments[0] is fence.allowed_code:
        return
    if event == "import" and fence.allowed_code is not None and arguments and arguments[0] in sys.modules:
        return
    if event in {"open", "import", "compile", "exec", "builtins.input", "builtins.breakpoint"} or event.startswith(
            ("os.", "subprocess.", "socket.", "ctypes.", "fcntl.", "mmap.", "shutil.", "tempfile.", "pty.")):
        deny_effect("audit:" + event)


sys.addaudithook(audit_effect)


class EffectFence(contextlib.ExitStack):
    def __init__(self, allowed_code=None):
        super().__init__()
        self.allowed_code = allowed_code

    def __enter__(self):
        super().__enter__()
        self.violations = []
        ACTIVE_FENCES.append(self)
        surfaces = [
            (builtins, ("open", "input", "breakpoint")),
            (io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "socketpair", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (signal, ("signal", "setitimer", "alarm", "pthread_kill")),
            (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "read", "write", "stat", "lstat", "fstat", "readlink",
                  "listdir", "scandir", "mkdir", "makedirs", "remove", "unlink", "rename", "replace",
                  "rmdir", "chmod", "chown", "link", "symlink", "truncate", "kill", "killpg",
                  "system", "popen", "fsync", "fdatasync", "fchmod", "fchown", "ftruncate",
                  "fork", "execv", "execve", "posix_spawn", "umask", "urandom")),
            (Path, ("open", "read_bytes", "read_text", "write_bytes", "write_text", "stat", "lstat",
                    "resolve", "exists", "is_file", "is_dir", "is_symlink", "mkdir", "touch", "unlink",
                    "rename", "replace", "rmdir", "chmod", "iterdir", "glob", "rglob")),
        ]
        if self.allowed_code is None:
            surfaces.append((builtins, ("__import__", "compile", "exec", "eval")))
            surfaces.append((importlib, ("import_module", "reload")))
        for owner, names in surfaces:
            for name in names:
                if hasattr(owner, name):
                    self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments):
        try:
            result = super().__exit__(*arguments)
        finally:
            if ACTIVE_FENCES and ACTIVE_FENCES[-1] is self:
                ACTIVE_FENCES.pop()
        if self.violations:
            raise AssertionError("The guard attempted external effects: " + ", ".join(self.violations))
        return result


SPEC = importlib.util.spec_from_file_location("library_changed_reference_ui_under_test", OPERATOR_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("The sibling LibraryChanged controller cannot be loaded.")
CONTROLLER = importlib.util.module_from_spec(SPEC)
OPERATOR_CODE = SPEC.loader.source_to_code(OPERATOR_RAW, str(OPERATOR_PATH))
sys.modules[SPEC.name] = CONTROLLER
with EffectFence(allowed_code=OPERATOR_CODE):
    exec(OPERATOR_CODE, CONTROLLER.__dict__)


JSON_LOAD_OPTIONS = {}


def dispatch_object_pairs(pairs):
    callback = JSON_LOAD_OPTIONS.get("object_pairs_hook")
    return callback(pairs) if callback is not None else dict(pairs)


def dispatch_integer(value):
    return (JSON_LOAD_OPTIONS.get("parse_int") or int)(value)


def dispatch_float(value):
    return (JSON_LOAD_OPTIONS.get("parse_float") or float)(value)


def dispatch_constant(value):
    callback = JSON_LOAD_OPTIONS.get("parse_constant")
    if callback is not None:
        return callback(value)
    return {"NaN": float("nan"), "Infinity": float("inf"), "-Infinity": float("-inf")}[value]


def make_controller_json_decoder():
    decoder = json.JSONDecoder(object_pairs_hook=dispatch_object_pairs, parse_int=dispatch_integer,
                               parse_float=dispatch_float, parse_constant=dispatch_constant)
    decoder.parse_string = json.decoder.py_scanstring
    object_globals = dict(json.decoder.JSONObject.__globals__)
    object_globals["scanstring"] = json.decoder.py_scanstring
    decoder.parse_object = types.FunctionType(json.decoder.JSONObject.__code__, object_globals,
        "controller_memory_json_object", json.decoder.JSONObject.__defaults__, json.decoder.JSONObject.__closure__)
    decoder.scan_once = json.scanner.py_make_scanner(decoder)
    return decoder


CONTROLLER_JSON_DECODER = make_controller_json_decoder()


def controller_json_loads(raw, **options):
    if set(options) - {"object_pairs_hook", "parse_int", "parse_float", "parse_constant"}:
        raise AssertionError("An unmodeled JSON decoder option was requested.")
    if JSON_LOAD_OPTIONS:
        raise AssertionError("The memory JSON decoder was entered recursively.")
    JSON_LOAD_OPTIONS.update(options)
    try:
        return CONTROLLER_JSON_DECODER.decode(raw.decode("utf-8") if isinstance(raw, bytes) else raw)
    finally:
        JSON_LOAD_OPTIONS.clear()


CONTROLLER_JSON = types.SimpleNamespace(**vars(json))
CONTROLLER_JSON.loads = controller_json_loads


def file_information(*, inode=100, links=1, mode=0o600, directory=False, size=1, device=7):
    return types.SimpleNamespace(st_dev=device, st_ino=inode, st_uid=0, st_gid=0, st_nlink=links,
        st_mode=(stat.S_IFDIR if directory else stat.S_IFREG) | mode, st_size=size,
        st_mtime_ns=1_700_000_000_000_000_000, st_ctime_ns=1_700_000_000_000_000_000)


def memory_translation(message):
    return message


def memory_plural_translation(singular, plural, count):
    return singular if count == 1 else plural


def memory_help_formatter_init(self, prog, indent_increment=2, max_help_position=24, width=None):
    return ORIGINAL_HELP_FORMATTER_INIT(self, prog, indent_increment=indent_increment,
        max_help_position=max_help_position, width=78 if width is None else width)


class MemoryTextTestResult(unittest.TextTestResult):
    """Format failures from exception and code objects without source lookup."""

    def _exc_info_to_string(self, error, test):
        exception_type, exception, trace = error
        name = exception_type.__module__ + "." + exception_type.__name__
        arguments = exception.args
        message = arguments[0][:2048] if arguments and type(arguments[0]) is str else ""
        lines = [name + (": " + message if message else ""), "Traceback locations (no source reads):"]
        for _index in range(32):
            if trace is None:
                break
            code = trace.tb_frame.f_code
            filename = code.co_filename.replace("\\", "/").rsplit("/", 1)[-1][:256]
            lines.append("  " + filename + ":" + str(trace.tb_lineno) + " in " + code.co_name[:128])
            trace = trace.tb_next
        if trace is not None:
            lines.append("  [remaining frames omitted]")
        return "\n".join(lines) + "\n"


class GuardTestCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.object(CONTROLLER, "Path", MemoryPath))
        for name, value in list(vars(CONTROLLER).items()):
            if isinstance(value, PurePosixPath):
                self.enterContext(patch.object(CONTROLLER, name, MemoryPath(str(value))))
        # Keep the actual parser and formatter. Fixed width avoids the original
        # formatter's local import and terminal lookup; translations stay local.
        self.enterContext(patch.object(argparse, "_", memory_translation))
        self.enterContext(patch.object(argparse, "ngettext", memory_plural_translation))
        self.enterContext(patch.object(argparse.HelpFormatter, "__init__", memory_help_formatter_init))
        self.enterContext(EffectFence())
        self.enterContext(patch.object(CONTROLLER, "json", CONTROLLER_JSON))

    def reject(self, callback):
        with self.assertRaises(CONTROLLER.ObservationError):
            callback()


class MemoryJSONGuards(GuardTestCase):
    def test_failure_reporting_never_looks_up_source_files(self):
        result = MemoryTextTestResult(io.StringIO(), True, 0)
        with patch.object(linecache, "getline", denied), patch.object(linecache, "getlines", denied), \
                patch.object(linecache, "checkcache", denied):
            for exception in (AssertionError("synthetic assertion failure"), RuntimeError("synthetic unexpected error")):
                try:
                    raise exception
                except (AssertionError, RuntimeError):
                    error = sys.exc_info()
                    if type(exception) is AssertionError:
                        result.addFailure(self, error)
                    else:
                        result.addError(self, error)
        self.assertEqual(len(result.failures), 1)
        self.assertEqual(len(result.errors), 1)
        self.assertIn("synthetic assertion failure", result.failures[0][1])
        self.assertIn("synthetic unexpected error", result.errors[0][1])
        self.assertIn("test_failure_reporting_never_looks_up_source_files", result.errors[0][1])

    def test_private_python_callbacks_decode_nested_values_without_imports(self):
        self.assertIsInstance(MEMORY_JSON_DECODER.scan_once, types.FunctionType)
        self.assertIs(MEMORY_JSON_DECODER.parse_string, json.decoder.py_scanstring)
        self.assertIs(MEMORY_JSON_DECODER.parse_object.__globals__["scanstring"], json.decoder.py_scanstring)
        self.assertEqual(memory_json_decode(b'{"nested":{"values":[1,true,null,"text",0.125]}}'),
                         {"nested": {"values": [1, True, None, "text", Decimal("0.125")]}})

    def test_malformed_keys_values_arrays_duplicates_and_nonfinite_values_are_rejected(self):
        for raw in (b'{malformed', b'{"unterminated}', b'{"key":"bad\\q"}', b'{"key":}',
                    b'{"key":[1,]}', b'{"key":1,"key":2}', b'{"key":NaN}'):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                memory_json_decode(raw)


TOKEN = 'synthetic-viewer-token'
TOKEN_SHA = hashlib.sha256(TOKEN.encode()).hexdigest()
ADMIN_TOKEN = 'synthetic-administrator-token'
ROUTE = '/web/index.html#!/videos?serverId=f56dec8ff7414847873064c4be9fba74&parentId=93'
DOCUMENT = 'document-2'
CHILD = {'pid': 7002, 'start_ticks': '12346', 'boot_id': CONTROLLER.BOOT, 'uid': 0, 'gid': 0,
    'executable_path': str(CONTROLLER.NODE), 'executable_sha256': CONTROLLER.NODE_SHA,
    'cgroup': '/system.slice/' + CONTROLLER.WORKER_UNIT}


def at(seconds=0):
    return (datetime.datetime(2026, 9, 12, 12, tzinfo=datetime.timezone.utc) + datetime.timedelta(seconds=seconds)).isoformat().replace('+00:00', 'Z')


def descriptor(path, value='a'):
    return {'path': str(path), 'sha256': value * 64}


def movie(name='Observed', identifier='100'):
    value = {'Id': identifier, 'Name': name, 'Type': 'Movie', 'IsFolder': False,
        'ParentId': '99' if identifier == '100' else '95',
        'Path': CONTROLLER.TARGET_PATH if identifier == '100' else CONTROLLER.ANCHOR_PATH,
        'SortName': name, 'ForcedSortName': name, 'Overview': 'Original overview.', 'ProviderIds': {'opaque': 'retained'},
        'Genres': [], 'LockedFields': [], 'LockData': False, 'UserData': {'Played': False, 'PlayCount': 0, 'PlaybackPositionTicks': 0},
        'Opaque': {'LargeInteger': 9007199254740993, 'SmallNumber': Decimal('1.000000000000000001')}}
    if identifier == '100':
        value['MediaSources'] = [{'Id': 'mediasource_100', 'ItemId': '100', 'Path': CONTROLLER.TARGET_PATH, 'Name': name,
            'RunTimeTicks': 9007199254740993, 'Container': 'mp4', 'MediaStreams': [{'Index': 0, 'Type': 'Video', 'Codec': 'h264', 'Width': 1920}],
            'UnknownTechnical': {'Retained': Decimal('1.000000000000000001')}}]
    return value


def movie_with_source(name='Observed'):
    return movie(name)


def snapshot():
    target, anchor = movie(), movie('Anchor', '96')
    folder = {'Id': '99', 'Name': 'Observed folder', 'Type': 'Folder', 'IsFolder': True, 'Path': str(MemoryPath(CONTROLLER.TARGET_PATH).parent)}
    library = {'Id': '93', 'Name': 'M3e Controlled LibraryChanged Movies', 'Type': 'CollectionFolder'}
    users = {key: {'Id': key, 'Name': name, 'Configuration': {'OrderedViews': ['93']},
        'Policy': {'IsAdministrator': administrator, 'IsDisabled': False}, 'LastLoginDate': at(-10), 'LastActivityDate': at(-10)}
        for key, name, administrator in ((CONTROLLER.ADMIN, CONTROLLER.ADMIN_NAME, True), (CONTROLLER.VIEWER, CONTROLLER.VIEWER_NAME, False))}
    return {'marker': CONTROLLER.SNAPSHOT_MARKER, 'version': 1, 'captured_at': at(),
        'credential_context': CONTROLLER.credential_context(CONTROLLER.ADMIN, hashlib.sha256(ADMIN_TOKEN.encode()).hexdigest()),
        'server': {'Id': CONTROLLER.SERVER, 'Version': '4.9.5.0'}, 'roster': users, 'configuration': {'EnableRemoteAccess': False},
        'libraries': {'93': {'ItemId': '93', 'Name': library['Name'], 'Locations': [str(CONTROLLER.MOVIES)], 'CollectionType': 'movies',
            'LibraryOptions': {'SaveLocalMetadata': False, 'MetadataSavers': []}}},
        'catalog_by_library': {'93': {'99': folder, '100': copy.deepcopy(target), '96': copy.deepcopy(anchor)}},
        'items_by_user': {key: {'100': copy.deepcopy(target), '96': copy.deepcopy(anchor)} for key in users},
        'preferences': {key: {'theme': 'dark'} for key in users},
        'details': {'admin': {'93': library, '99': copy.deepcopy(folder), '100': copy.deepcopy(target), '96': copy.deepcopy(anchor)},
                    'viewer': {'100': copy.deepcopy(target), '96': copy.deepcopy(anchor)}},
        'devices': {'legacy-device': {'Id': 'legacy-device', 'ReportedDeviceId': 'legacy-reported', 'Name': 'Legacy browser',
            'LastUserName': CONTROLLER.VIEWER_NAME, 'AppName': 'Legacy Client', 'AppVersion': '1.0', 'LastUserId': CONTROLLER.VIEWER,
            'DateLastActivity': at(-10), 'IpAddress': '127.0.0.1'}}}


def input_record():
    before = snapshot()
    return {'marker': CONTROLLER.INPUT_MARKER, 'version': 1, 'mode': CONTROLLER.MODE, 'root': str(CONTROLLER.ROOT),
        'output': str(CONTROLLER.ROOT / 'browser'), 'actor': {'slot': 'B', 'user_id': CONTROLLER.VIEWER, 'username': CONTROLLER.VIEWER_NAME,
            'credentials': descriptor(CONTROLLER.ROOT / 'viewer-credentials.json'), 'account_key': 'viewer'},
        'reference': {'server_id': CONTROLLER.SERVER, 'base_url': CONTROLLER.BASE_URL,
            'service_identity': {'pid': CONTROLLER.PID, 'startTicks': CONTROLLER.TICKS, 'bootId': CONTROLLER.BOOT, 'uid': 0,
                'exe': '/synthetic/reference', 'cmdline': ['/synthetic/reference'], 'networkNamespace': CONTROLLER.NAMESPACE,
                'cgroup': '0::/system.slice/reference.service\n', 'invocationId': CONTROLLER.REFERENCE_INVOCATION}},
        'expected_libraries': [{'id': '93', 'name': before['libraries']['93']['Name']}],
        'target': {'id': '100', 'library_id': '93', 'parent_id': '99', 'name': 'Observed', 'path': CONTROLLER.TARGET_PATH, 'type': 'Movie'},
        'anchor': {'id': '96', 'name': 'Anchor', 'path': CONTROLLER.ANCHOR_PATH, 'type': 'Movie'},
        'source_closure': {str(CONTROLLER.TOOL / name): hashlib.sha256(name.encode()).hexdigest() for name in CONTROLLER.JS_NAMES},
        'authority': {'owner': {'path': str(CONTROLLER.OWNER), 'sha256': CONTROLLER.OWNER_SHA},
            'prior_recovery': copy.deepcopy(CONTROLLER.PRIOR_RECOVERY),
            'preflight': descriptor(CONTROLLER.PREFLIGHT_ROOT / 'report.json'), 'before_snapshot': descriptor(CONTROLLER.ROOT / 'before-public.json')},
        'controller': {'pid': 7001, 'start_ticks': '12345', 'boot_id': CONTROLLER.BOOT, 'unit': CONTROLLER.CONTROLLER_UNIT, 'invocation_id': 'd' * 32}}


def private_session(value=None):
    value = value or input_record()
    proof = {'token_sha256': TOKEN_SHA, 'session_id': 'observed-session', 'user_id': CONTROLLER.VIEWER,
        'client_name': 'Emby Web', 'device_id': 'fresh-ui-device', 'device_name': 'Observed browser', 'client_version': '4.9.5.0',
        'server_id': CONTROLLER.SERVER, 'created_at': at(1), 'created_at_source': 'SessionInfo.LastActivityDate', 'kind': 'emby', 'slot': 'B',
        'frame_login_finished': True, 'physical_login_completed': True, 'request_metadata_matches': True}
    return {'marker': 'goby-reference-library-changed-session-private-v1', 'version': 1, 'input_sha256': 'a' * 64,
        'source_closure_sha256': CONTROLLER.sha(CONTROLLER.canonical(value['source_closure'])), 'controller': value['controller'],
        'node_process': CHILD, 'token': TOKEN, 'proof': proof}


def viewer_baseline_fixture(value=None, private=None):
    value = value or input_record(); private = private or private_session(value)
    target = dict(movie(), CanDelete=False, CanDownload=False)
    anchor = dict(movie('Anchor', '96'), CanDelete=False, CanDownload=False)
    bodies = {'identity': {'Id': CONTROLLER.VIEWER, 'Name': CONTROLLER.VIEWER_NAME, 'Policy': {'IsAdministrator': False, 'IsDisabled': False}},
        'target': target, 'anchor': anchor}
    results = {role: {'channel': 'controller_api', 'status': 200, 'complete': True, 'body': body,
        'body_bytes': len(CONTROLLER.canonical(body)), 'body_sha256': CONTROLLER.sha(CONTROLLER.canonical(body)),
        'token_sha256': TOKEN_SHA, 'operation': None, 'completed_at': at(10)} for role, body in bodies.items()}
    names = {'identity': 'viewer-fresh-identity-result.json', 'target': 'viewer-baseline-target-result.json', 'anchor': 'viewer-baseline-anchor-result.json'}
    baseline = {'marker': 'goby-reference-library-changed-viewer-baseline-v1', 'version': 1, 'captured_at': at(10),
        'input_sha256': 'a' * 64, 'session_private': descriptor(CONTROLLER.ROOT / 'browser/session-private.json'),
        'credential_context': CONTROLLER.credential_context(CONTROLLER.VIEWER, TOKEN_SHA),
        'details': {'100': copy.deepcopy(target), '96': copy.deepcopy(anchor)}}
    for role, result in results.items():
        baseline[role + '_result'] = {'path': str(CONTROLLER.ROOT / names[role]), 'sha256': CONTROLLER.sha(CONTROLLER.canonical(result) + b'\n')}
    return baseline, results


def prior_recovery_report():
    report = {'administrator_closed': True, 'captured_at': '2026-09-12T16:03:07.659965+00:00', 'errors': [], 'http_requests': 9,
        'library_changed_client_acceptance': False, 'marker': 'goby-reference-ui-name-recovery-v3', 'media_unchanged': True,
        'original_scope_replayed': False, 'protocol_observation_complete': False, 'restoration_confirmed': True,
        'restore_acknowledged': True, 'restore_posts': 1, 'status': 'restored',
        'viewer_projection_channel': 'administrator_token_with_viewer_UserId', 'viewer_ui_login_performed': False, 'results': {}, 'evidence': {}}
    for name, (status, size, body_sha) in CONTROLLER.PRIOR_RECOVERY_RESULTS.items():
        report['results'][name] = {'status': status, 'body_bytes': size, 'body_sha256': body_sha, 'complete': True,
            'token_sha256': None if name == 'login' else '8c1f5d1d11d471242c8d00be3c073ecba5ed6e162a9f3e2b20578e3d62e18f3e'}
        for suffix in ('-intent', '-result'):
            key = name + suffix; filename = 'login-result-private.json' if key == 'login-result' else key + '.json'
            report['evidence'][key] = descriptor(CONTROLLER.PRIOR_RECOVERY_ROOT / filename)
    report['evidence']['ownership'] = descriptor(CONTROLLER.PRIOR_RECOVERY_ROOT / 'ownership.json')
    return report


def query_digest(route, query, hidden=()):
    return hashlib.sha256((route + '\n' + json.dumps(sorted(query.items()), separators=(',', ':'), ensure_ascii=False) + '\n' +
        json.dumps(list(hidden), sort_keys=True, separators=(',', ':'), ensure_ascii=False)).encode()).hexdigest()


def read_pair(name='Observed', phase='discovery', start=100, sequence=2, index=0, target=False):
    route = '/Users/' + CONTROLLER.VIEWER + '/Items' + ('/100' if target else '')
    query = {} if target else {'IncludeItemTypes': 'Movie', 'Limit': '50', 'ParentId': '93', 'Recursive': 'true', 'StartIndex': '0'}
    items = [{'Id': '100', 'Name': name, 'Type': 'Movie', 'IsFolder': False, 'ParentId': '99'}]
    if not target: items.append({'Id': '96', 'Name': 'Anchor', 'Type': 'Movie', 'IsFolder': False, 'ParentId': '95'})
    body = CONTROLLER.canonical(items[0] if target else {'Items': items, 'TotalRecordCount': len(items)})
    base = {'index': index, 'method': 'GET', 'kind': 'target' if target else 'items', 'route': route, 'query': query, 'hidden_query': [],
        'shape_sha256': query_digest(route, query), 'request_sha256': 'b' * 64, 'token_sha256': TOKEN_SHA,
        'request_sequence': sequence, 'start_elapsed_ms': start, 'response_elapsed_ms': start + 50, 'finished_elapsed_ms': start + 100,
        'phase': phase, 'document_id': DOCUMENT, 'page_route': ROUTE, 'sourceworker': False, 'main_frame': True,
        'from_service_worker': False, 'content_type': 'application/json', 'status': 200, 'completed': True, 'failed': False,
        'projection': None, 'outcome': 'completed', 'reason': None, 'request_bytes': None, 'response_bytes': None}
    base.update({key: None for key in CONTROLLER.HTTP_TRANSPORT_FIELDS})
    frame = dict(base, id='frame-' + str(index))
    physical = dict(base, id='physical-' + str(index), request_sequence=sequence + 1, start_elapsed_ms=start + 10,
        finished_elapsed_ms=start + 80, sourceworker=None, main_frame=None, request_bytes=0, response_bytes=len(body),
        projection={'items': items, 'count': len(items), 'body_sha256': CONTROLLER.sha(body), 'body_bytes': len(body)})
    physical.update(transport_phase='completed', request_body_sha256=hashlib.sha256(b'').hexdigest(),
        upstream_create_attempted=True, upstream_created=True, upstream_end_attempted=True, upstream_end_returned=True, upstream_response_received=True)
    return {'physical': physical, 'frame': frame, 'complete': True, 'unambiguous': True}


def dom(expected='Observed', observed=None, pair=None, phase='discovery', message_id=None, started=250, passed=True):
    observed = expected if observed is None else observed
    pair = pair or read_pair(expected)
    physical, frame = pair['physical'], pair['frame']
    return {'route': ROUTE, 'document_id': DOCUMENT, 'started_elapsed_ms': started, 'expected_name': expected, 'anchor_name': 'Anchor',
        'observed_title': observed, 'observed_anchor_title': 'Anchor', 'visible_items_containers': 3, 'visible_card_containers': 1,
        'visible_cards': 2, 'visible_title_buttons': 2, 'target_title_count': int(observed == expected), 'anchor_title_count': 1,
        'forbidden_title_count': int(observed != expected), 'explicit_identity_consistent': True, 'media_inactive': True, 'structural_match': True,
        'target_id': '100' if passed else None, 'anchor_id': '96' if passed else None,
        'identity_mode': 'reference-two-movie-wire-and-cards' if passed else 'unbound', 'identity_proven': passed, 'passed': passed,
        'wire_identity': {'phase': phase, 'physical_exchange_id': physical['id'], 'frame_request_index': frame['index'],
            'body_sha256': physical['projection']['body_sha256'], 'shape_sha256': physical['shape_sha256'],
            'request_sha256': physical['request_sha256'], 'token_sha256': TOKEN_SHA, 'message_id': message_id} if passed else None}


def discovery():
    pair = read_pair()
    return {'home': {'route': '/web/index.html#!/home', 'document_id': 'document-1', 'library_id': '93',
            'library_name': 'M3e Controlled LibraryChanged Movies', 'control_count': 1, 'identity_proven': True},
        'dom': dom(pair=pair), 'reads': [pair], 'query_allowlist': [{key: pair['physical'][key] for key in ('kind', 'route', 'query', 'hidden_query', 'shape_sha256')}],
        'socket': {'connection_id': 'socket-1', 'token_sha256': TOKEN_SHA, 'seen': 1, 'opened': 1},
        'navigation': {'before_route': '/web/index.html#!/home', 'after_route': ROUTE, 'before_sequence': 0}}


def message(phase='forward', elapsed=100100, sequence=102, identifier='f' * 32):
    value = {'MessageType': 'LibraryChanged', 'MessageId': identifier, 'Data': {key: ['100'] if key == 'ItemsUpdated' else [] for key in CONTROLLER.MESSAGE_ARRAYS}}
    value['Data']['IsEmpty'] = False
    raw = CONTROLLER.canonical(value).decode()
    return {'sequence': sequence, 'elapsed_ms': elapsed, 'phase': phase, 'direction': 'server', 'connection_id': 'socket-1',
        'token_sha256': TOKEN_SHA, 'original_message': True, 'payload_retained': True, 'json': value, 'json_text': raw,
        'body_sha256': CONTROLLER.sha(raw.encode()), 'bytes': len(raw.encode()), 'forwarded': True, 'document_id': DOCUMENT, 'page_route': ROUTE}


def window(name='forward', transition=True, notifications=True, http=True):
    reservation = CONTROLLER.reserve_edit(movie())['public']
    expected = reservation['marker_name'] if name == 'forward' else reservation['original_name']
    old = reservation['original_name'] if name == 'forward' else reservation['marker_name']
    start, sequence = (100000, 100) if name == 'forward' else (230000, 1000)
    wire = message(name, start + 100, sequence + 2, 'f' * 32 if name == 'forward' else 'e' * 32)
    browser = dict(wire, elapsed_ms=start + 110, sequence=sequence + 3)
    pair = read_pair(expected, name, start + 120, sequence + 4)
    samples = [{'sequence': sequence + 1, 'started_elapsed_ms': start, 'elapsed_ms': start + 10,
                'observation': dom(expected, old if transition else expected, pair, name, wire['json']['MessageId'], start, False)}]
    samples += [{'sequence': sequence + 20 + index, 'started_elapsed_ms': moment, 'elapsed_ms': moment + 10,
        'observation': dom(expected, expected, pair, name, wire['json']['MessageId'], moment, bool(notifications and http))}
        for index, moment in enumerate(range(start + 500, start + 120001, 500))]
    changed = transition and notifications and http
    return {'name': name, 'control_sha256': 'c' * 64,
        'commit': {'write_completed_at': at((start + 500) / 1000), 'native_result_sha256': 'd' * 64,
                   'admin_readback_sha256': 'e' * 64, 'viewer_readback_sha256': 'f' * 64},
        'boundary': {'name': name, 'route': ROUTE, 'document_id': DOCUMENT, 'connection_id': 'socket-1', 'token_sha256': TOKEN_SHA,
            'websocket_seen': 1, 'websocket_opened': 1, 'started_sequence': sequence, 'started_elapsed_ms': start,
            'response_completed_elapsed_ms': start + 500, 'end_elapsed_ms': start + 120500, 'completed_elapsed_ms': start + 120600, 'duration_ms': 120000},
        'events': {'physical': [wire] if notifications else [], 'browser': [browser] if notifications else []},
        'http': {'physical': [pair['physical']] if http else [], 'frames': [pair['frame']] if http else [],
            'pairs': [{'frame_request_index': 0, 'physical_exchange_id': 'physical-0', 'complete': True, 'unambiguous': True}] if http else []},
        'dom': samples, 'actions': [], 'lifecycle': [], 'result': 'changed' if changed else 'not_observed',
        'outcome': 'automatic_http_and_visible_transition_observed' if changed else 'matches_expected_without_proven_transition',
        'status': {'message_observed': notifications, 'http_observed': notifications and http, 'dom_matches_expected': True, 'visible_transition_observed': changed},
        'proof': {**samples[1]['observation']['wire_identity'], 'first_dom_sequence': sequence + 20, 'last_dom_sequence': samples[-1]['sequence']} if changed else None,
        'notification_context': {'additional_items': False, 'parent_context': False},
        'message_evidence': {'received_count': int(notifications), 'transport_paired_count': int(notifications),
            'qualified_count': int(notifications), 'unsupported_schema_count': 0, 'reasons': []}}


def stage(name, observation, value=None):
    value = value or input_record()
    return {'marker': 'goby-reference-library-changed-stage-v1', 'version': 1, 'input_sha256': 'a' * 64,
        'source_closure_sha256': CONTROLLER.sha(CONTROLLER.canonical(value['source_closure'])), 'controller': value['controller'],
        'node_process': CHILD, 'name': name, 'token_sha256': TOKEN_SHA,
        'session_private': descriptor(CONTROLLER.ROOT / 'browser/session-private.json'), 'previous_control_sha256': 'c' * 64,
        'observation': observation}


def closed_failed_report(value, private, child):
    return {'marker': 'goby-reference-library-changed-browser-v1', 'version': 1, 'input_sha256': 'a' * 64,
        'source_closure_sha256': CONTROLLER.sha(CONTROLLER.canonical(value['source_closure'])),
        'controller': copy.deepcopy(value['controller']), 'node_process': copy.deepcopy(child),
        'reference': copy.deepcopy(value['reference']), 'target': copy.deepcopy(value['target']), 'anchor': copy.deepcopy(value['anchor']),
        'library_changed_client_acceptance': False, 'main_acceptance': False, 'login_proof': copy.deepcopy(private['proof']),
        'protocol_observation_complete': False, 'result': 'failed', 'failure': 'reference_synthetic_failure',
        'actor': {'closed': True, 'cleanup_failures': [], 'session_proof_owner': 'runtime_viewer_only',
            'user_id': CONTROLLER.VIEWER, 'server_id': CONTROLLER.SERVER, 'token_sha256': TOKEN_SHA,
            'http': {'login': 1, 'logout': 1, 'active': 0}, 'websocket': {'active': 0, 'opened': 1, 'closed': 1},
            'logout': {'status': 204, 'physical_completed': True, 'login_view_visible': True, 'post_logout_status': 401,
                'token_sha256': TOKEN_SHA, 'frame_token_sha256': TOKEN_SHA},
            'session_proof': {'outcome': 'all_observed_logout_tokens_rejected', 'entries': [
                {'token_fingerprint': TOKEN_SHA, 'result': 'logout_token_rejected', 'ui_request': {'response_status': 204},
                    'verification': {'status': 401, 'is_ui_request': False, 'method': 'GET',
                        'route': '/emby/System/Info', 'result': 'token_rejected'}}]}}}


class MetadataAndPublicGuards(GuardTestCase):
    def test_name_reservation_deepcopies_every_present_edit_field(self):
        original = movie()
        saved = copy.deepcopy(original)
        value = CONTROLLER.reserve_edit(original)
        self.assertEqual(original, saved)
        self.assertEqual(value['restore_body'], {'Id': '100', **{key: original[key] for key in CONTROLLER.EDIT_FIELDS if key in original}})
        self.assertEqual({key for key in value['forward_body'] if value['forward_body'][key] != value['restore_body'][key]}, {'Name'})
        value['forward_body']['ProviderIds']['opaque'] = 'changed copy'
        self.assertEqual(original['ProviderIds'], {'opaque': 'retained'})

    def test_removed_ids_wrong_path_folder_and_title_collision_are_rejected(self):
        for key, value in (('Id', '98'), ('ParentId', '97'), ('Path', '/foreign'), ('IsFolder', True), ('Type', 'Audio')):
            current = movie(); current[key] = value
            with self.subTest(key=key): self.reject(lambda: CONTROLLER.reserve_edit(current))
        self.reject(lambda: CONTROLLER.reserve_edit(movie(CONTROLLER.MARKER_NAME)))

    def test_only_exact_owned_name_state_permits_restoration(self):
        original = movie(); reservation = CONTROLLER.reserve_edit(original)
        changed = copy.deepcopy(original); changed['Name'] = reservation['public']['marker_name']
        changed['SortName'] = changed['ForcedSortName'] = changed['Name']
        changed['MediaSources'][0]['Name'] = changed['Name']
        self.assertEqual(CONTROLLER.classify_metadata(original, changed, reservation, at(), at(10), True)[0], 'mutated')
        changed['Overview'] = 'Foreign metadata'
        self.assertEqual(CONTROLLER.classify_metadata(original, changed, reservation, at(), at(10), True)[0], 'foreign-or-unknown')
        self.assertEqual(CONTROLLER.classify_metadata(original, original, reservation, at(), at(10), False)[0], 'original')

    def test_only_the_proven_unlocked_sorting_round_trip_is_admitted(self):
        for defect in ('lock', 'lockdata', 'different-sort', 'different-forced', 'missing-forced', 'missing-sort'):
            value = movie()
            if defect == 'lock': value['LockedFields'] = ['SortName']
            elif defect == 'lockdata': value['LockData'] = True
            elif defect == 'different-sort': value['SortName'] = 'custom'
            elif defect == 'different-forced': value['ForcedSortName'] = 'custom'
            elif defect == 'missing-forced': del value['ForcedSortName']
            else: del value['SortName']
            with self.subTest(defect=defect): self.reject(lambda: CONTROLLER.reserve_edit(value))

    def test_sorting_is_derived_in_the_response_but_never_a_separate_write(self):
        original = movie(); reservation = CONTROLLER.reserve_edit(original)
        self.assertEqual(reservation['public']['marker_name'], 'reference library changed ui four')
        self.assertEqual(reservation['forward_body']['SortName'], original['SortName'])
        self.assertEqual(reservation['forward_body']['ForcedSortName'], original['ForcedSortName'])
        forward = copy.deepcopy(original)
        forward.update(Name=CONTROLLER.MARKER_NAME, SortName=CONTROLLER.MARKER_NAME, ForcedSortName=CONTROLLER.MARKER_NAME)
        forward['MediaSources'][0]['Name'] = CONTROLLER.MARKER_NAME
        self.assertEqual(CONTROLLER.classify_metadata(original, forward, reservation, at(), at(10), True)[0], 'mutated')
        forward['Name'] = original['Name']
        self.assertEqual(CONTROLLER.classify_metadata(original, forward, reservation, at(), at(10), True)[0], 'foreign-or-unknown')
        self.assertEqual(CONTROLLER.classify_metadata(original, original, reservation, at(), at(10), True)[0], 'original')

    def test_the_unique_media_source_name_is_a_projection_and_restores_exactly(self):
        original = movie_with_source(); untouched = copy.deepcopy(original); reservation = CONTROLLER.reserve_edit(original)
        forward = copy.deepcopy(original)
        forward['Name'] = forward['SortName'] = forward['ForcedSortName'] = CONTROLLER.MARKER_NAME
        forward['MediaSources'][0]['Name'] = CONTROLLER.MARKER_NAME
        self.assertEqual(CONTROLLER.name_projection(original, CONTROLLER.MARKER_NAME), forward)
        self.assertEqual(CONTROLLER.classify_metadata(original, forward, reservation, at(), at(10), True)[0], 'mutated')
        self.assertEqual(original, untouched); self.assertNotIn('MediaSources', reservation['forward_body'])
        before = snapshot(); before['details']['admin']['100'] = copy.deepcopy(original)
        del before['catalog_by_library']['93']['100']['MediaSources']
        after = copy.deepcopy(before); after['captured_at'] = at(10)
        for family in ('catalog_by_library', 'items_by_user', 'details'):
            for rows in after[family].values():
                if '100' not in rows: continue
                target = rows['100']; target['Name'] = target['SortName'] = target['ForcedSortName'] = CONTROLLER.MARKER_NAME
                if 'MediaSources' in target: target['MediaSources'][0]['Name'] = CONTROLLER.MARKER_NAME
        compared = CONTROLLER.compare_public(before, after, expected_name=CONTROLLER.MARKER_NAME, metadata_sent=True)
        self.assertTrue(compared['passed'])
        self.assertNotIn('MediaSources', after['catalog_by_library']['93']['100'])
        self.assertTrue(any(row['kind'] == 'name-derived-media-source' for row in compared['automatic_changes']))
        self.assertEqual(CONTROLLER.classify_metadata(original, original, reservation, at(), at(20), True)[0], 'original')
        forward['Name'] = forward['SortName'] = forward['ForcedSortName'] = original['Name']
        self.assertEqual(CONTROLLER.classify_metadata(original, forward, reservation, at(), at(20), True)[0], 'foreign-or-unknown')

    def test_media_source_identity_types_count_technical_and_unknown_values_are_preserved(self):
        for field, replacement in (('Id', 'other'), ('ItemId', 100), ('Path', '/foreign'), ('Name', 'Independent source title')):
            value = movie_with_source(); value['MediaSources'][0][field] = replacement
            with self.subTest(field=field): self.reject(lambda: CONTROLLER.reserve_edit(value))
        value = movie_with_source(); value['MediaSources'] *= 2
        self.reject(lambda: CONTROLLER.reserve_edit(value))
        missing = movie(); del missing['MediaSources']
        self.reject(lambda: CONTROLLER.reserve_edit(missing))
        original = movie_with_source(); reservation = CONTROLLER.reserve_edit(original)
        for defect in ('path', 'runtime', 'stream', 'unknown', 'missing', 'added'):
            current = copy.deepcopy(original)
            current['Name'] = current['SortName'] = current['ForcedSortName'] = CONTROLLER.MARKER_NAME
            source = current['MediaSources'][0]; source['Name'] = CONTROLLER.MARKER_NAME
            if defect == 'path': source['Path'] = '/foreign'
            elif defect == 'runtime': source['RunTimeTicks'] -= 1
            elif defect == 'stream': source['MediaStreams'][0]['Width'] = 1280
            elif defect == 'unknown': source['UnknownTechnical']['Retained'] = Decimal('1')
            elif defect == 'missing': del current['MediaSources']
            else: source['Unexpected'] = None
            with self.subTest(defect=defect):
                self.assertEqual(CONTROLLER.classify_metadata(original, current, reservation, at(), at(10), True)[0], 'foreign-or-unknown')

    def test_snapshot_context_identifies_admin_projection_without_equating_fresh_tokens(self):
        before = snapshot(); after = copy.deepcopy(before); after['captured_at'] = at(10)
        after['credential_context']['token_sha256'] = 'c' * 64
        self.assertTrue(CONTROLLER.compare_public(before, after)['passed'])
        for field, replacement in (('authenticated_user_id', CONTROLLER.VIEWER), ('user_id_semantics', 'authenticated_viewer'),
                                   ('channel', 'browser_ui'), ('token_sha256', None)):
            changed = copy.deepcopy(after); changed['credential_context'][field] = replacement
            with self.subTest(field=field): self.reject(lambda: CONTROLLER.validate_public_snapshot(changed))

    def test_prior_recovery_remains_an_exact_failed_scope_restoration_receipt(self):
        terminal = copy.deepcopy(CONTROLLER.PRIOR_RECOVERY_TERMINAL); report = prior_recovery_report()
        CONTROLLER.validate_prior_recovery(terminal, report)
        self.assertNotIn('version', terminal); self.assertNotIn('version', report)
        for defect in ('viewer_auth', 'replay', 'posts', 'logout', 'token', 'terminal', 'old_report', 'evidence_path'):
            changed, changed_terminal = copy.deepcopy(report), copy.deepcopy(terminal)
            if defect == 'viewer_auth': changed['viewer_ui_login_performed'] = True
            elif defect == 'replay': changed['original_scope_replayed'] = True
            elif defect == 'posts': changed['restore_posts'] = 2
            elif defect == 'logout': changed['results']['logout']['complete'] = False
            elif defect == 'token': changed['results']['exact401']['token_sha256'] = 'b' * 64
            elif defect == 'terminal': changed_terminal['systemd']['MainPID'] = '123'
            elif defect == 'old_report': changed_terminal['original_failed_report']['sha256'] = 'a' * 64
            else: changed['evidence']['login-result']['path'] = str(CONTROLLER.ROOT / 'foreign.json')
            with self.subTest(defect=defect): self.reject(lambda: CONTROLLER.validate_prior_recovery(changed_terminal, changed))

    def test_viewer_baseline_binds_real_results_session_token_and_unfiltered_details(self):
        value = input_record(); private = private_session(value); baseline, results = viewer_baseline_fixture(value, private)
        CONTROLLER.validate_viewer_baseline(baseline, value, 'a' * 64, CHILD, private, baseline['session_private'], snapshot(), results)
        self.assertFalse(baseline['details']['100']['CanDelete']); self.assertNotIn('SupportsSync', baseline['details']['100'])
        for defect in ('principal', 'token', 'result_descriptor', 'result_token', 'permission', 'anchor', 'ordinary_identity', 'missing_source'):
            changed, records = copy.deepcopy(baseline), copy.deepcopy(results)
            if defect == 'principal': changed['credential_context']['authenticated_user_id'] = CONTROLLER.ADMIN
            elif defect == 'token': changed['credential_context']['token_sha256'] = 'c' * 64
            elif defect == 'result_descriptor': changed['target_result']['sha256'] = 'f' * 64
            elif defect == 'result_token': records['target']['token_sha256'] = 'c' * 64
            elif defect == 'permission': changed['details']['100']['CanDelete'] = True
            elif defect == 'anchor': changed['details']['96']['Overview'] = 'Changed anchor'
            elif defect == 'ordinary_identity':
                records['identity']['body']['Policy']['IsAdministrator'] = True
                changed['identity_result']['sha256'] = CONTROLLER.sha(CONTROLLER.canonical(records['identity']) + b'\n')
            else:
                del changed['details']['100']['MediaSources']; del records['target']['body']['MediaSources']
                changed['target_result']['sha256'] = CONTROLLER.sha(CONTROLLER.canonical(records['target']) + b'\n')
            with self.subTest(defect=defect):
                self.reject(lambda: CONTROLLER.validate_viewer_baseline(changed, value, 'a' * 64, CHILD, private,
                    baseline['session_private'], snapshot(), records))

    def test_automatic_fields_are_recorded_and_time_bounded(self):
        original = movie(); changed = dict(original, Etag='new-etag', DateLastSaved=at(5))
        changes = CONTROLLER.automatic_changes(original, changed, at(), at(10), True)
        self.assertEqual(set(changes), {'Etag', 'DateLastSaved'})
        self.reject(lambda: CONTROLLER.automatic_changes(original, changed, at(), at(10), False))
        changed['DateLastSaved'] = at(11)
        self.reject(lambda: CONTROLLER.automatic_changes(original, changed, at(), at(10), True))

    def test_complete_public_snapshot_and_preservation_keep_unknown_values(self):
        before = snapshot(); after = copy.deepcopy(before); after['captured_at'] = at(10)
        self.assertIs(CONTROLLER.validate_public_snapshot(before), before)
        self.assertTrue(CONTROLLER.compare_public(before, after)['passed'])
        self.assertIn(b'9007199254740993', CONTROLLER.canonical(before))
        self.assertIn(b'1.000000000000000001', CONTROLLER.canonical(before))

    def test_every_legacy_policy_preference_userdata_anchor_and_catalog_is_preserved(self):
        for family in ('roster', 'preferences', 'items_by_user', 'details', 'catalog_by_library', 'configuration', 'devices'):
            before = snapshot(); after = copy.deepcopy(before); after['captured_at'] = at(10)
            if family == 'roster': after[family][CONTROLLER.VIEWER]['Policy']['IsAdministrator'] = True
            elif family == 'preferences': after[family][CONTROLLER.VIEWER]['theme'] = 'changed'
            elif family == 'items_by_user': after[family][CONTROLLER.VIEWER]['100']['UserData']['Played'] = True
            elif family == 'details': after[family]['admin']['96']['Overview'] = 'changed'
            elif family == 'catalog_by_library': del after[family]['93']['96']
            elif family == 'configuration': after[family]['EnableRemoteAccess'] = True
            else: after[family]['legacy-device']['Name'] = 'changed'
            with self.subTest(family=family): self.reject(lambda: CONTROLLER.compare_public(before, after))

    def test_only_owned_authentication_dates_may_advance(self):
        before = snapshot(); after = copy.deepcopy(before); after['captured_at'] = at(10)
        after['roster'][CONTROLLER.ADMIN]['LastActivityDate'] = at(5)
        result = CONTROLLER.compare_public(before, after, active_users=[CONTROLLER.ADMIN])
        self.assertEqual(result['automatic_changes'][0]['kind'], 'owned-authentication-time')
        self.reject(lambda: CONTROLLER.compare_public(before, after))
        after['roster'][CONTROLLER.ADMIN]['LastActivityDate'] = at(11)
        self.reject(lambda: CONTROLLER.compare_public(before, after, active_users=[CONTROLLER.ADMIN]))

    def test_closed_preflight_device_exception_is_narrow_and_receipted(self):
        before = snapshot(); after = copy.deepcopy(before); after['captured_at'] = at(10)
        after['devices']['legacy-device']['DateLastActivity'] = at(2)
        witness = {'client': {'device_id': 'legacy-reported', 'client_name': 'Legacy Client', 'user_id': CONTROLLER.VIEWER},
                   'last_observed_at': at(), 'logout_completed_at': at(3)}
        self.assertTrue(CONTROLLER.compare_public(before, after, prior_closed_devices=[witness])['passed'])
        self.reject(lambda: CONTROLLER.compare_public(before, after))
        after['devices']['legacy-device']['DateLastActivity'] = at(4)
        self.reject(lambda: CONTROLLER.compare_public(before, after, prior_closed_devices=[witness]))

    def test_local_metadata_savers_active_refresh_and_incomplete_pages_are_rejected(self):
        current = snapshot(); current['libraries']['93']['LibraryOptions']['SaveLocalMetadata'] = True
        self.reject(lambda: CONTROLLER.validate_public_snapshot(current))
        current = snapshot(); current['libraries']['93']['RefreshStatus'] = 'unknown'
        self.reject(lambda: CONTROLLER.validate_public_snapshot(current))
        self.reject(lambda: CONTROLLER.indexed_page({'Items': [{'Id': '100'}], 'TotalRecordCount': 2}))

    def test_utc_bounds_preserve_seventh_fractional_digit(self):
        self.assertEqual(CONTROLLER.instant('2026-09-12T12:00:00.0000001Z') - CONTROLLER.instant(at()), 1)
        for value in ('2026-09-12T12:00:00', '2026-09-12T20:00:00+08:00', '2026-02-30T12:00:00Z'):
            with self.subTest(value=value): self.reject(lambda: CONTROLLER.instant(value))


class BrowserEvidenceGuards(GuardTestCase):
    def test_discovery_binds_both_real_movies_and_the_frozen_query(self):
        value = discovery(); original = copy.deepcopy(value)
        CONTROLLER.discovery_evidence(value, input_record(), TOKEN_SHA)
        self.assertEqual(value, original)

    def test_discovery_rejects_id_subset_route_token_and_wrong_anchor(self):
        for defect in ('id', 'query', 'route', 'token', 'anchor'):
            value = discovery(); pair = value['reads'][0]
            if defect == 'id': pair['physical']['projection']['items'][0]['Id'] = '98'
            elif defect == 'query': pair['physical']['query']['Ids'] = '100'
            elif defect == 'route': value['dom']['route'] = '/web/index.html#!/item?id=100'
            elif defect == 'token': pair['frame']['token_sha256'] = '0' * 64
            else: pair['physical']['projection']['items'][1]['Name'] = 'Wrong anchor'
            with self.subTest(defect=defect): self.reject(lambda: CONTROLLER.discovery_evidence(value, input_record(), TOKEN_SHA))

    def test_two_passive_windows_have_independent_real_transitions(self):
        reservation = CONTROLLER.reserve_edit(movie())['public']
        for name in ('forward', 'restored'):
            value = window(name); original = copy.deepcopy(value)
            actual = CONTROLLER.window_evidence(value, input_record(), reservation, discovery())
            self.assertEqual(actual, {key: value[key] for key in actual})
            self.assertEqual(value, original)

    def test_already_original_after_no_forward_transition_is_only_a_match(self):
        value = window('restored', transition=False)
        actual = CONTROLLER.window_evidence(value, input_record(), CONTROLLER.reserve_edit(movie())['public'], discovery())
        self.assertEqual(actual['result'], 'not_observed')
        self.assertTrue(actual['status']['dom_matches_expected'])
        self.assertFalse(actual['status']['visible_transition_observed'])

    def test_missing_notification_or_automatic_http_remains_not_observed(self):
        for notifications, http in ((False, True), (True, False)):
            value = window(notifications=notifications, http=http)
            actual = CONTROLLER.window_evidence(value, input_record(), CONTROLLER.reserve_edit(movie())['public'], discovery())
            self.assertEqual(actual['result'], 'not_observed')

    def test_compact_references_are_recomputed_and_cannot_hide_ambiguity(self):
        reservation = CONTROLLER.reserve_edit(movie())['public']
        for defect in ('missing', 'wrong', 'duplicate', 'ambiguous', 'failed'):
            value = window()
            if defect == 'missing': value['http']['pairs'] = []
            elif defect == 'wrong': value['http']['pairs'][0]['physical_exchange_id'] = 'physical-9'
            elif defect == 'duplicate': value['http']['pairs'] *= 2
            elif defect == 'ambiguous': value['http']['frames'].append(dict(value['http']['frames'][0], id='frame-1', index=1))
            else: value['http']['frames'][0]['failed'] = True
            with self.subTest(defect=defect): self.reject(lambda: CONTROLLER.window_evidence(value, input_record(), reservation, discovery()))

    def test_transport_facts_preserve_possible_dispatch_without_a_response(self):
        pair = read_pair(); physical = pair['physical']
        physical.update(transport_phase='upstream_end', completed=False, failed=False, outcome='pending',
            status=None, response_elapsed_ms=None, finished_elapsed_ms=None, response_bytes=0, projection=None,
            upstream_response_received=False)
        original = copy.deepcopy(physical)
        pairs = CONTROLLER.pair_reads([physical], [pair['frame']])
        self.assertFalse(pairs[0]['complete']); self.assertEqual(physical, original)
        self.assertTrue(physical['upstream_created']); self.assertTrue(physical['upstream_end_returned'])
        physical.update(failed=True, outcome='failed', error_code='upstream_transport_error', reason='http_upstream_error', finished_elapsed_ms=180)
        original = copy.deepcopy(physical)
        self.assertFalse(CONTROLLER.pair_reads([physical], [pair['frame']])[0]['complete'])
        self.assertEqual(physical, original); self.assertIsNone(physical['status']); self.assertEqual(physical['response_bytes'], 0)
        admission = copy.deepcopy(physical)
        admission.update(transport_phase='admission', failed=False, outcome='pending', error_code=None, reason=None,
            finished_elapsed_ms=None, request_body_sha256=None, **{key: False for key in CONTROLLER.HTTP_UPSTREAM_FLAGS})
        CONTROLLER.http_shape(admission, True)
        response = copy.deepcopy(physical)
        response.update(transport_phase='upstream_response', error_code='upstream_response_rejected', upstream_created=False,
            upstream_end_attempted=False, upstream_end_returned=False, upstream_response_received=True)
        CONTROLLER.http_shape(response, True)

    def test_transport_stages_flags_and_frame_unknowns_cannot_be_forged(self):
        for defect in ('missing', 'unknown_phase', 'unsafe_error', 'unsafe_content_type', 'nonboolean', 'created_without_attempt',
                       'end_without_create', 'returned_without_end', 'response_without_attempt', 'completed_without_body',
                       'completed_wrong_phase', 'completed_without_response', 'failed_without_error', 'pending_completed_phase', 'frame_false'):
            pair = read_pair(); physical, frame = pair['physical'], pair['frame']
            if defect == 'missing': del physical['upstream_created']
            elif defect == 'unknown_phase': physical['transport_phase'] = 'dispatch_not_proven'
            elif defect == 'unsafe_error': physical.update(completed=False, failed=True, transport_phase='upstream_end', error_code='untrusted exception text')
            elif defect == 'unsafe_content_type': physical['request_content_type'] = 'application/x-www-form-urlencoded; password=forbidden'
            elif defect == 'nonboolean': physical['upstream_created'] = 1
            elif defect == 'created_without_attempt': physical['upstream_create_attempted'] = False
            elif defect == 'end_without_create': physical['upstream_created'] = False
            elif defect == 'returned_without_end': physical['upstream_end_attempted'] = False
            elif defect == 'response_without_attempt':
                physical.update(completed=False, transport_phase='upstream_response', **{key: False for key in CONTROLLER.HTTP_UPSTREAM_FLAGS})
                physical['upstream_response_received'] = True
            elif defect == 'completed_without_body': physical['request_body_sha256'] = None
            elif defect == 'completed_wrong_phase': physical['transport_phase'] = 'downstream_write'
            elif defect == 'completed_without_response': physical['upstream_response_received'] = False
            elif defect == 'failed_without_error': physical.update(completed=False, failed=True, outcome='failed', transport_phase='upstream_end')
            elif defect == 'pending_completed_phase': physical.update(completed=False, outcome='pending')
            else: frame['upstream_created'] = False
            with self.subTest(defect=defect): self.reject(lambda: CONTROLLER.pair_reads([physical], [frame]))

    def test_full_raw_sessions_replies_do_not_consume_librarychanged_budget(self):
        value = window()
        for number in range(100):
            event = {'sequence': 500 + number, 'elapsed_ms': 110000 + number, 'phase': 'forward', 'direction': 'server',
                'json': {'MessageType': 'Sessions', 'Data': []}, 'connection_id': 'socket-1', 'token_sha256': TOKEN_SHA}
            value['events']['browser'].append(event); value['events']['physical'].append(dict(event))
        actual = CONTROLLER.window_evidence(value, input_record(), CONTROLLER.reserve_edit(movie())['public'], discovery())
        self.assertEqual(actual['result'], 'changed'); self.assertEqual(actual['message_evidence']['received_count'], 1)

    def test_missing_message_id_is_received_but_never_a_proven_update(self):
        value = window()
        for row in value['events']['physical'] + value['events']['browser']:
            row['json'].pop('MessageId', None)
            row['json_text'] = CONTROLLER.canonical(row['json']).decode()
            row['bytes'] = len(row['json_text'].encode()); row['body_sha256'] = CONTROLLER.sha(row['json_text'].encode())
        actual = CONTROLLER.window_evidence(value, input_record(), CONTROLLER.reserve_edit(movie())['public'], discovery())
        self.assertEqual(actual['result'], 'not_observed'); self.assertTrue(actual['status']['message_observed'])
        self.assertEqual(actual['message_evidence']['reasons'], ['message_id_not_proven'])

    def test_parent_arrays_remain_actual_context_and_do_not_authorize_writes(self):
        value = window()
        for row in value['events']['physical'] + value['events']['browser']:
            row['json']['Data']['FoldersAddedTo'] = ['93', '99']
            row['json_text'] = CONTROLLER.canonical(row['json']).decode()
            row['bytes'] = len(row['json_text'].encode()); row['body_sha256'] = CONTROLLER.sha(row['json_text'].encode())
        actual = CONTROLLER.window_evidence(value, input_record(), CONTROLLER.reserve_edit(movie())['public'], discovery())
        self.assertTrue(actual['notification_context']['parent_context']); self.assertEqual(actual['result'], 'changed')

    def test_digest_intervention_sampling_and_duration_corruption_are_rejected(self):
        for defect in ('digest', 'action', 'gap', 'duration', 'bound-false'):
            value = window()
            if defect == 'digest': value['events']['physical'][0]['body_sha256'] = '0' * 64
            elif defect == 'action': value['actions'] = [{'kind': 'reload'}]
            elif defect == 'gap': del value['dom'][5:20]
            elif defect == 'duration': value['boundary']['duration_ms'] = 119999
            else: value['dom'][-1]['observation']['passed'] = False
            with self.subTest(defect=defect): self.reject(lambda: CONTROLLER.window_evidence(value, input_record(), CONTROLLER.reserve_edit(movie())['public'], discovery()))

    def test_private_acknowledgement_binds_actual_new_device_and_both_logins(self):
        value = input_record(); private = private_session(value)
        CONTROLLER.validate_private_session(private, value, 'a' * 64, CHILD, snapshot())
        for field, replacement in (('device_id', 'legacy-reported'), ('user_id', CONTROLLER.ADMIN), ('server_id', '0' * 32),
                                   ('physical_login_completed', False), ('token_sha256', '0' * 64)):
            changed = copy.deepcopy(private); changed['proof'][field] = replacement
            with self.subTest(field=field): self.reject(lambda: CONTROLLER.validate_private_session(changed, value, 'a' * 64, CHILD, snapshot()))

    def test_real_proc_cgroup_projects_identically_into_private_stage_and_report(self):
        expected = '/system.slice/goby-reference-library-changed-ui-v4.service'
        for raw in ('0::/system.slice/goby-reference-library-changed-ui-v4.service',
                    '0::/system.slice/goby-reference-library-changed-ui-v4.service\n'):
            with self.subTest(raw=raw):
                child = copy.deepcopy(CHILD)
                child['cgroup'] = CONTROLLER.worker_cgroup_path(raw)
                value = input_record(); private = private_session(value)
                private['node_process'] = copy.deepcopy(child)
                current_stage = stage('discovery', discovery(), value)
                current_stage['node_process'] = copy.deepcopy(child)
                current_stage['previous_control_sha256'] = None
                report = closed_failed_report(value, private, child)
                proof = CONTROLLER.validate_private_session(private, value, 'a' * 64, child, snapshot())
                CONTROLLER.validate_stage(current_stage, value, 'a' * 64, child, 'discovery', None, proof, None, None)
                CONTROLLER.validate_browser_report(report, value, 'a' * 64, child, private, [], [])
                for record in (private, current_stage, report): self.assertEqual(record['node_process']['cgroup'], expected)
                self.assertEqual(report['reference']['service_identity']['cgroup'], '0::/system.slice/reference.service\n')

    def test_cgroup_rejects_malformed_proc_rows_and_coherently_wrong_publication(self):
        expected = '/system.slice/goby-reference-library-changed-ui-v4.service'
        old = '/system.slice/goby-reference-library-changed-ui-v1.service'
        previous = '/system.slice/goby-reference-library-changed-ui-v2.service'
        latest = '/system.slice/goby-reference-library-changed-ui-v3.service'
        controller = '/system.slice/goby-reference-library-changed-ui-controller-v4.service'
        for raw in (expected, '0::' + old, '0::' + previous, '0::' + latest, '0::' + controller, '0::' + expected + '\r\n',
                    '0::' + expected + '\n\n', '0::' + expected + '\n0::/other', ' 0::' + expected):
            with self.subTest(raw=raw): self.reject(lambda: CONTROLLER.worker_cgroup_path(raw))
        for published in ('0::' + expected + '\n', old, previous, latest, controller):
            child = copy.deepcopy(CHILD); child['cgroup'] = published
            value = input_record(); private = private_session(value); private['node_process'] = copy.deepcopy(child)
            current_stage = stage('discovery', discovery(), value); current_stage['node_process'] = copy.deepcopy(child)
            report = closed_failed_report(value, private, child)
            with self.subTest(published=published):
                self.reject(lambda: CONTROLLER.validate_private_session(private, value, 'a' * 64, child, snapshot()))
                self.reject(lambda: CONTROLLER.validate_stage(current_stage, value, 'a' * 64, child,
                    'discovery', 'c' * 64, private['proof'], None, None))
                self.reject(lambda: CONTROLLER.validate_browser_report(report, value, 'a' * 64, child, private, [], []))


def quiet_observation():
    samples = [{'sequence': 10 + index, 'started_elapsed_ms': moment, 'elapsed_ms': moment + 10,
                'observation': dom(started=moment)} for index, moment in enumerate(range(1000, 75501, 500))]
    ordinary = read_pair('unused', 'discovery', 2000, 20, 10)
    for row in ordinary.values():
        if isinstance(row, dict):
            row.update(kind='read', route='/System/Info/Public', query={}, hidden_query=[], projection=None)
            row['shape_sha256'] = query_digest(row['route'], {})
    sessions = [{'sequence': 50 + index, 'elapsed_ms': 2000 + index * 500, 'direction': 'server',
                 'json': {'MessageType': 'Sessions', 'Data': []}} for index in range(70)]
    return {'quiet': {'started_elapsed_ms': 1000, 'completed_elapsed_ms': 76000, 'started_sequence': 9, 'duration_ms': 75000,
            'catalog_requests': 0, 'library_changed_messages': 0, 'http': {'physical': [ordinary['physical']], 'frames': [ordinary['frame']]},
            'events': {'physical': copy.deepcopy(sessions), 'browser': sessions}, 'samples': samples, 'passed': True},
        'boundary': {'name': 'forward', 'started_sequence': 1000, 'started_elapsed_ms': 77000, 'route': ROUTE, 'document_id': DOCUMENT,
            'connection_id': 'socket-1', 'token_sha256': TOKEN_SHA, 'websocket_seen': 1, 'websocket_opened': 1}}


class QuietAndCLIGuards(GuardTestCase):
    def test_complete_quiet_allows_normal_background_http_and_sessions_replies(self):
        value = stage('armed', quiet_observation())
        CONTROLLER.validate_stage(value, input_record(), 'a' * 64, CHILD, 'armed', 'c' * 64,
            private_session()['proof'], discovery(), CONTROLLER.reserve_edit(movie())['public'])

    def test_quiet_recomputes_catalog_and_librarychanged_semantic_counts(self):
        for defect in ('catalog', 'message', 'shortened', 'gap'):
            value = stage('armed', quiet_observation()); quiet = value['observation']['quiet']
            if defect == 'catalog':
                pair = read_pair(start=3000, sequence=30, index=11)
                quiet['http']['physical'].append(pair['physical']); quiet['http']['frames'].append(pair['frame'])
            elif defect == 'message': quiet['events']['browser'].append(message('discovery', 3000, 200))
            elif defect == 'shortened': quiet['duration_ms'] = 20000
            else: del quiet['samples'][5:20]
            with self.subTest(defect=defect):
                self.reject(lambda: CONTROLLER.validate_stage(value, input_record(), 'a' * 64, CHILD, 'armed', 'c' * 64,
                    private_session()['proof'], discovery(), CONTROLLER.reserve_edit(movie())['public']))

    def test_business_read_only_cli_needs_no_browser_inputs(self):
        args = CONTROLLER.arguments(['--mode', 'preflight', '--script-sha256', 'a' * 64])
        self.assertEqual(args.mode, 'preflight'); self.assertIsNone(args.node)
        self.reject(lambda: CONTROLLER.arguments(['--mode', 'preflight', '--script-sha256', 'a' * 64, '--node', str(CONTROLLER.NODE)]))

    def test_observation_cli_requires_fixed_source_node_and_actual_preflight_pins(self):
        values = ['--mode', 'observe', '--script-sha256', 'a' * 64, '--preflight-sha256', 'b' * 64,
            '--source-closure', str(CONTROLLER.TOOL / 'source-closure.json'), '--source-closure-sha256', 'c' * 64,
            '--node', str(CONTROLLER.NODE), '--node-sha256', CONTROLLER.NODE_SHA]
        self.assertEqual(CONTROLLER.arguments(values).mode, 'observe')
        for flag, replacement in (('--node', '/opt/reference/original-executable'), ('--preflight-sha256', 'pending'),
                                  ('--source-closure', str(CONTROLLER.ROOT / 'sources.json')),
                                  ('--source-closure', str(CONTROLLER.WORK / 'reference-library-changed-ui-tool-01/source-closure.json')),
                                  ('--source-closure', str(CONTROLLER.WORK / 'reference-library-changed-ui-tool-02/source-closure.json')),
                                  ('--source-closure', str(CONTROLLER.WORK / 'reference-library-changed-ui-tool-03/source-closure.json'))):
            changed = list(values); changed[changed.index(flag) + 1] = replacement
            with self.subTest(flag=flag): self.reject(lambda: CONTROLLER.arguments(changed))


class MemoryResponse:
    def __init__(self, body, status=200):
        self.raw = b'' if body is None else CONTROLLER.canonical(body)
        self.status = status

    def getheader(self, name):
        return str(len(self.raw)) if name == 'Content-Length' else 'application/json' if name == 'Content-Type' else None

    def read(self, bound):
        return self.raw[:bound]


def transport_run(case, mode='observe'):
    clock = {'now': 0}
    case.enterContext(patch.object(CONTROLLER.time, 'monotonic', lambda: clock['now']))
    case.enterContext(patch.object(CONTROLLER, 'utc_now', lambda: at(10)))
    case.enterContext(patch.object(CONTROLLER.secrets, 'token_hex', lambda _count: 'c' * 32))
    for name in ('getsignal', 'signal', 'setitimer'):
        case.enterContext(patch.object(CONTROLLER.signal, name, lambda *_args: 0))
    run = CONTROLLER.Run(types.SimpleNamespace(mode=mode, script_sha256='a' * 64))
    run.before = snapshot(); run.reservation = CONTROLLER.reserve_edit(movie()); run.media = {'synthetic': 'unchanged'}
    run.admin = CONTROLLER.Administrator(run, 'p' * 64)
    run.admin.token = ADMIN_TOKEN; run.admin.owned = run.admin.proven = run.admin.acknowledged = True
    run.admin.session_id = 'admin-session'; run.browser = private_session(); run.viewer_token = TOKEN
    run.input = input_record(); run.input_sha = 'a' * 64
    run.check = lambda cleanup=False: None
    run.documents, run.exchanges, run.connections = {}, [], []
    environment = {'state': 'original', 'fault': None, 'clock': clock, 'run': run}
    def save(name, value, ipc=False):
        if name in run.documents: raise CONTROLLER.ObservationError('A memory publication was repeated.')
        if environment['fault'] == 'ack-publication' and name == 'admin-acknowledgement-private.json': raise OSError('Synthetic private publication failure.')
        run.documents[name] = copy.deepcopy(value)
        record = {'path': str(run.root / name), 'sha256': CONTROLLER.sha(CONTROLLER.canonical(value) + b'\n')}
        run.records[name] = record
        return record
    run.save = save
    baseline, baseline_results = viewer_baseline_fixture(run.input, run.browser)
    environment['viewer_target'] = copy.deepcopy(baseline['details']['100'])
    environment['viewer_anchor'] = copy.deepcopy(baseline['details']['96'])
    environment['viewer_identity'] = copy.deepcopy(baseline_results['identity']['body'])
    if mode == 'observe':
        run.viewer_baseline = copy.deepcopy(baseline)
        saved = run.save('viewer-baseline.json', baseline)
        run.source_pins[saved['path']] = saved['sha256']
    case.enterContext(patch.object(CONTROLLER, 'media_snapshot', lambda: copy.deepcopy(run.media)))
    class Connection:
        def __init__(self, host, port, timeout):
            case.assertEqual((host, port, timeout), ('127.0.0.1', 18197, 8)); self.closed = False; run.connections.append(self)
        def request(self, method, path, body, headers):
            self.method, self.path, self.headers = method, path, dict(headers)
            run.exchanges.append({'method': method, 'path': path, 'body': memory_json_decode(body) if body else None, 'headers': dict(headers)})
            if method == 'POST' and path == '/emby/Items/100':
                environment['state'] = 'mutated' if memory_json_decode(body)['Name'] == run.reservation['public']['marker_name'] else 'original'
        def getresponse(self):
            if environment['fault'] == 'restore-ack-lost' and self.method == 'POST' and self.path == '/emby/Items/100':
                raise OSError('Synthetic restoration ACK loss.')
            if self.path == '/emby/Users/AuthenticateByName':
                return MemoryResponse({'AccessToken': ADMIN_TOKEN, 'ServerId': CONTROLLER.SERVER,
                    'User': {'Id': CONTROLLER.ADMIN, 'Name': CONTROLLER.ADMIN_NAME, 'Policy': {'IsAdministrator': True}},
                    'SessionInfo': {'Id': 'admin-session', 'UserId': CONTROLLER.ADMIN, 'DeviceId': run.admin.device}})
            if self.path == '/emby/Sessions/Logout': return MemoryResponse(None, 204)
            if self.path == '/emby/System/Info': return MemoryResponse({'error': 'unauthorized'}, 401)
            if self.method == 'POST': return MemoryResponse(None, 204)
            if self.path == '/emby/Users/' + CONTROLLER.VIEWER: return MemoryResponse(environment['viewer_identity'])
            if self.path.endswith('/Items/96'): return MemoryResponse(copy.deepcopy(environment['viewer_anchor']))
            current = copy.deepcopy(environment['viewer_target'] if self.headers.get('X-Emby-Token') == TOKEN else run.reservation['detail'])
            if environment['state'] == 'mutated':
                current['Name'] = run.reservation['public']['marker_name']
                current['SortName'] = current['ForcedSortName'] = current['Name']
                if 'MediaSources' in current: current['MediaSources'][0]['Name'] = current['Name']
            return MemoryResponse(current)
        def close(self): self.closed = True
    case.enterContext(patch.object(CONTROLLER.http.client, 'HTTPConnection', Connection))
    return run, environment


class TransportAndRecoveryGuards(GuardTestCase):
    def test_actual_authentication_transport_has_private_ack_before_proof(self):
        run, _environment = transport_run(self, 'preflight')
        run.admin.token = None; run.admin.owned = run.admin.proven = run.admin.acknowledged = False
        run.admin.login()
        self.assertTrue(run.admin.proven)
        self.assertEqual(list(run.documents)[:4], ['admin-login-intent.json', 'admin-acknowledgement-private.json', 'admin-login-result.json', 'admin-proven.json'])
        self.assertIsNone(run.documents['admin-login-result.json']['body'])
        self.assertFalse(run.documents['admin-acknowledgement-private.json']['normal_use_authorized'])
        run.admin.logout()
        self.assertTrue(run.admin.closed); self.assertEqual(run.http_requests, 3)
        self.assertTrue(all(connection.closed for connection in run.connections))

    def test_failed_private_ack_never_authorizes_metadata_but_keeps_cleanup_identity(self):
        run, environment = transport_run(self)
        run.admin.token = None; run.admin.owned = run.admin.proven = run.admin.acknowledged = False
        environment['fault'] = 'ack-publication'
        self.reject(run.admin.login)
        self.assertFalse(run.admin.proven); self.assertTrue(run.admin.owned)
        run.admin.logout(); self.assertTrue(run.admin.closed)
        self.assertEqual(run.dispatched, ['admin-login', 'admin-logout'])

    def test_work_request_and_byte_exhaustion_preserves_unique_logout_and_401(self):
        run, _environment = transport_run(self)
        run.work_requests = run.http_requests = CONTROLLER.WORK_HTTP
        run.work_received = run.bytes_received = CONTROLLER.WORK_BYTES
        self.reject(lambda: run.request('another-work-read', 'GET', '/emby/System/Info/Public'))
        run.admin.logout()
        self.assertTrue(run.admin.closed); self.assertEqual(run.cleanup_requests, 2)
        self.assertEqual(run.http_requests, CONTROLLER.WORK_HTTP + 2)
        self.reject(run.admin.logout)

    def test_administrator_logout_keeps_a_final_budget_inside_the_hard_deadline(self):
        run, environment = transport_run(self)
        environment['clock']['now'] = 880
        run.cleanup_deadline = 875
        run.admin.logout()
        self.assertEqual(run.cleanup_deadline, run.hard_deadline)
        self.assertTrue(run.admin.closed)

    def test_cleanup_reserve_can_restore_once_after_work_is_exhausted(self):
        run, environment = transport_run(self)
        run.forward_sent = True; run.dispatched = ['forward']; environment['state'] = 'mutated'
        run.work_requests = run.http_requests = CONTROLLER.WORK_HTTP
        run.work_received = run.bytes_received = CONTROLLER.WORK_BYTES
        run.metadata_post(True, cleanup=True)
        self.assertEqual(run.restoration, 'confirmed'); self.assertEqual(run.cleanup_requests, 5)
        self.assertEqual([row['method'] for row in run.exchanges], ['GET', 'POST', 'GET', 'GET', 'GET'])
        self.reject(lambda: run.metadata_post(True, cleanup=True))
        run.admin.logout()
        self.assertTrue(run.admin.closed); self.assertEqual(run.cleanup_requests, 7)
        self.assertLessEqual(run.cleanup_requests, CONTROLLER.CLEANUP_HTTP)

    def test_expired_work_clock_still_has_bounded_cleanup_time(self):
        run, environment = transport_run(self)
        environment['clock']['now'] = 781
        run.op = types.SimpleNamespace(same_service=lambda _owner: None,
            process_identity=lambda _pid: {'startTicks': '378464', 'bootId': CONTROLLER.BOOT, 'networkNamespace': 'net:[1]'})
        self.enterContext(patch.object(CONTROLLER.os, 'readlink', lambda _path: 'net:[1]'))
        baseline = copy.deepcopy(run.records['viewer-baseline.json']); reads = []
        def memory_protected(path, expected=None, limit=CONTROLLER.MAX_JSON, modes=(0o600,)):
            CONTROLLER.require(str(path) == baseline['path'] and expected == baseline['sha256'] and modes == (0o600, 0o644),
                'The deadline fixture requested an unowned memory artifact.')
            reads.append(str(path))
            raw = CONTROLLER.canonical(run.documents['viewer-baseline.json']) + b'\n'
            CONTROLLER.require(len(raw) <= limit and CONTROLLER.sha(raw) == expected, 'The owned memory artifact changed its size or digest.')
            return raw
        self.enterContext(patch.object(CONTROLLER, 'protected', memory_protected))
        run.check = types.MethodType(CONTROLLER.Run.check, run)
        self.reject(run.check)
        self.assertEqual(reads, [])
        run.recover()
        self.assertEqual(run.cleanup_deadline, 891)
        run.check(cleanup=True)
        self.assertEqual(reads, [baseline['path']])
        original = copy.deepcopy(run.documents['viewer-baseline.json'])
        run.documents['viewer-baseline.json']['input_sha256'] = 'f' * 64
        self.reject(lambda: run.check(cleanup=True))
        run.documents['viewer-baseline.json'] = original
        reads_before_expiry = len(reads)
        environment['clock']['now'] = 901
        self.reject(lambda: run.check(cleanup=True))
        self.assertEqual(len(reads), reads_before_expiry)

    def test_unknown_forward_at_original_is_not_treated_as_resolved(self):
        run, _environment = transport_run(self)
        run.forward_sent = True; run.dispatched = ['forward']; run.responses['forward'] = {'complete': False, 'status': None}
        run.recover()
        self.assertEqual(run.restoration, 'restoration_required')
        self.assertTrue(run.documents['restore-original-observation.json']['outcome_unknown'])
        self.assertFalse(any(row['method'] == 'POST' for row in run.exchanges))

    def test_restore_ack_loss_reconciles_original_without_second_post(self):
        run, environment = transport_run(self)
        run.forward_sent = run.forward_ack = True; run.dispatched = ['forward']; environment['state'] = 'mutated'
        environment['fault'] = 'restore-ack-lost'
        self.reject(lambda: run.metadata_post(True))
        self.assertTrue(run.restore_sent); self.assertFalse(run.restore_ack)
        run.recover()
        self.assertEqual(run.restoration, 'confirmed')
        self.assertEqual(sum(row['method'] == 'POST' for row in run.exchanges), 1)
        self.assertFalse(run.documents['restore-reconciliation.json']['second_restore_permitted'])

    def test_restore_sent_inside_recovery_with_lost_ack_gets_one_readonly_reconciliation(self):
        run, environment = transport_run(self)
        run.forward_sent = run.forward_ack = True
        run.dispatched = ['forward']; environment['state'] = 'mutated'; environment['fault'] = 'restore-ack-lost'
        run.recover()
        self.assertTrue(run.restore_sent); self.assertFalse(run.restore_ack)
        self.assertEqual(run.restoration, 'confirmed')
        self.assertEqual([row['method'] for row in run.exchanges], ['GET', 'POST', 'GET'])
        self.assertEqual(run.dispatched.count('restore'), 1)
        self.assertEqual(run.documents['restore-reconciliation.json']['state'], 'original')
        self.assertFalse(run.documents['restore-reconciliation.json']['second_restore_permitted'])
        self.assertTrue(any(row['stage'] == 'exact-restoration' for row in run.errors))

    def test_not_dispatched_and_foreign_metadata_never_trigger_a_blind_restore(self):
        run, _environment = transport_run(self)
        run.forward_sent = True
        run.recover(); self.assertEqual(run.restoration, 'not_required'); self.assertEqual(run.exchanges, [])
        run.dispatched = ['forward']; run.restore_sent = False
        foreign = movie(); foreign['Overview'] = 'Foreign edit'
        run.detail_readback = lambda *_args, **_kwargs: foreign
        run.recover()
        self.assertFalse(any(row['method'] == 'POST' for row in run.exchanges))
        self.assertTrue(run.errors)

    def test_cleanup_scope_cannot_login_forward_or_probe_an_unowned_route(self):
        run, _environment = transport_run(self)
        for label, method, route, body, operation in (
            ('extra-login', 'POST', '/emby/Users/AuthenticateByName', {}, 'admin-login'),
            ('extra-forward', 'POST', '/emby/Items/100', run.reservation['forward_body'], 'forward'),
            ('extra-sessions', 'GET', '/emby/Sessions', None, None),
            ('extra-proof', 'GET', '/emby/System/Info', None, None)):
            with self.subTest(label=label): self.reject(lambda: run.request(label, method, route, body, operation, cleanup=True))
        self.assertEqual(run.exchanges, [])

    def test_viewer_api_readback_uses_actual_ui_metadata_and_a_separate_channel(self):
        run, _environment = transport_run(self)
        run.detail_readback('viewer-readback', viewer=True)
        headers = run.exchanges[0]['headers']
        self.assertEqual(headers['X-Emby-Token'], TOKEN)
        self.assertIn('fresh-ui-device', headers['Authorization'])
        self.assertNotIn(run.admin.device, headers['Authorization'])
        self.assertEqual(run.documents['viewer-readback-result.json']['channel'], 'controller_api')

    def test_real_ui_baseline_precedes_name_writes_and_preserves_its_own_permission_projection(self):
        run, environment = transport_run(self)
        original = movie_with_source()
        run.reservation = CONTROLLER.reserve_edit(original); run.before['details']['admin']['100'] = copy.deepcopy(original)
        run.before['details']['viewer']['100'].update(CanDelete=True, CanDownload=True, SupportsSync=True)
        environment['viewer_target'] = dict(copy.deepcopy(original), CanDelete=False, CanDownload=False)
        run.viewer_baseline = None; run.documents.pop('viewer-baseline.json'); run.records.pop('viewer-baseline.json')
        run.child = copy.deepcopy(CHILD)
        run.stages = [{'name': 'discovery', 'value': {'session_private': descriptor(CONTROLLER.ROOT / 'browser/session-private.json')}}]
        run.capture_viewer_baseline(); run.require_viewer_baseline()
        frozen = copy.deepcopy(run.viewer_baseline)
        self.assertEqual([row['path'] for row in run.exchanges], ['/emby/Users/' + CONTROLLER.VIEWER,
            '/emby/Users/' + CONTROLLER.VIEWER + '/Items/100', '/emby/Users/' + CONTROLLER.VIEWER + '/Items/96'])
        self.assertTrue(all(row['headers']['X-Emby-Token'] == TOKEN and 'fresh-ui-device' in row['headers']['Authorization'] for row in run.exchanges))
        self.assertTrue(all(run.source_pins[run.records[name]['path']] == run.records[name]['sha256'] for name in
            ('viewer-baseline.json', 'viewer-fresh-identity-result.json', 'viewer-baseline-target-result.json', 'viewer-baseline-anchor-result.json')))
        run.metadata_post(); run.metadata_post(True)
        self.assertEqual(run.restoration, 'confirmed'); self.assertEqual(run.viewer_baseline, frozen)
        self.assertFalse(run.documents['forward-viewer-confirmed.json']['detail']['CanDelete'])
        self.assertNotIn('SupportsSync', run.documents['restore-viewer-confirmed.json']['detail'])
        self.assertEqual(run.documents['restore-viewer-confirmed.json']['detail']['MediaSources'], original['MediaSources'])
        self.assertEqual(sum(row['method'] == 'POST' for row in run.exchanges), 2)

    def test_real_viewer_permission_presence_or_anchor_drift_is_never_filtered_out(self):
        for defect in ('delete', 'download', 'missing', 'added', 'anchor', 'baseline_mutation'):
            run, environment = transport_run(self)
            if defect == 'delete': environment['viewer_target']['CanDelete'] = True
            elif defect == 'download': environment['viewer_target']['CanDownload'] = True
            elif defect == 'missing': del environment['viewer_target']['CanDelete']
            elif defect == 'added': environment['viewer_target']['SupportsSync'] = False
            elif defect == 'anchor': environment['viewer_anchor']['Overview'] = 'Foreign anchor'
            else: run.viewer_baseline['details']['100']['CanDelete'] = True
            with self.subTest(defect=defect): self.reject(lambda: run.metadata_post())
            self.assertEqual(sum(row['method'] == 'POST' for row in run.exchanges), 0 if defect == 'baseline_mutation' else 1)

    def test_invalid_ui_identity_stops_before_baseline_movie_reads_or_reservation(self):
        run, environment = transport_run(self)
        run.viewer_baseline = None; run.documents.pop('viewer-baseline.json'); run.records.pop('viewer-baseline.json')
        run.child = copy.deepcopy(CHILD)
        run.stages = [{'name': 'discovery', 'value': {'session_private': descriptor(CONTROLLER.ROOT / 'browser/session-private.json')}}]
        environment['viewer_identity']['Policy']['IsAdministrator'] = True
        self.reject(run.capture_viewer_baseline)
        self.assertEqual([row['path'] for row in run.exchanges], ['/emby/Users/' + CONTROLLER.VIEWER])
        self.assertIsNone(run.viewer_baseline); self.assertEqual(run.controls, [])
        self.reject(lambda: run.control('reserved'))

    def test_preflight_transport_rejects_any_name_write(self):
        run, _environment = transport_run(self, 'preflight')
        self.reject(lambda: run.request('forward', 'POST', '/emby/Items/100', run.reservation['forward_body'], 'forward'))
        self.assertEqual(run.exchanges, [])

    def test_production_finally_revokes_administrator_even_when_recovery_raises(self):
        run, _environment = transport_run(self)
        run.load = lambda: None
        run.execute_observation = lambda: (_ for _ in ()).throw(CONTROLLER.ObservationError('Synthetic observation failure.'))
        run.recover = lambda: (_ for _ in ()).throw(RuntimeError('Synthetic recovery failure.'))
        run.wait_browser = lambda: None
        run.final_public_state = lambda: None
        result = run.run()
        self.assertEqual(result['status'], 'failed')
        self.assertTrue(run.admin.closed)
        self.assertEqual([row['path'] for row in run.exchanges], ['/emby/Sessions/Logout', '/emby/System/Info'])

    def test_quota_failure_consumes_no_unjournaled_physical_request(self):
        run, _environment = transport_run(self)
        run.cleanup_requests = CONTROLLER.CLEANUP_HTTP
        self.reject(lambda: run.request('admin-logout', 'POST', '/emby/Sessions/Logout', operation='admin-logout', cleanup=True))
        self.assertEqual(run.exchanges, [])


def main():
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2, resultclass=MemoryTextTestResult).run(suite)
    print(json.dumps({'marker': 'goby-reference-library-changed-ui-guards-v1', 'tests': result.testsRun,
        'failures': len(result.failures), 'errors': len(result.errors), 'status': 'passed' if result.wasSuccessful() else 'failed',
        'controller_sha256': OPERATOR_SHA256, 'guard_sha256': GUARD_SHA256,
        'external_effects_permitted': False}, sort_keys=True))
    return 0 if result.wasSuccessful() else 1


if __name__ == '__main__':
    raise SystemExit(main())
