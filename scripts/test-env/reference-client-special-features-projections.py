#!/usr/bin/env python3
"""Capture twenty fixed, read-only special-feature projection/access cases.

The completed setup operator supplies immutable input/media attestation only.
Its setup, scanner and Job entry points are never called. One new ordinary
viewer token is created and revoked; anonymous reads never receive credentials.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import time
import types
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = WORK / 'reference-special-features-projections-v2'
PRIVATE, EXPORT = ROOT / 'private', ROOT / 'export'
FIXTURE_ROOT = WORK / 'reference-special-features-v1'
MARKER = 'goby-reference-special-features-projections-v2'
FAILED_REPORT = WORK / 'reference-special-features-projections-v1/export/report.json'
FAILED_REPORT_SHA = 'f835490388e569b5f99be39acf4d4ff452728edbc1c7684971d09b8f2434d41a'
FAILED_SOURCE_SHA = 'e8ad9f41b23a4fd1de38254de031045a6012a14969b671149130b6f5563556c9'
BASE_SHA = '51813c5478ac2eb10f990224564c57cd33631498fccbf7628466d17d1e3c728f'
SUPPORT_PATH = WORK / 'source-attempt-28/scripts/test-env/add-client-auxiliary-reference.py'
SUPPORT_SHA = '9af752745a5e9d9a9047491c03b9f32da2e9a70d01d78d17e1504ad760204d75'
PREPARER_SHA = '29f159a2580e178dce65b6c23ef14ad9875971240e534546eb5e8a14ce88c160'
REFERENCE_OWNER_SHA = '3bdbeee28406f9809624f3619b38915ab4445fa1a7c8aca30cbe445d3d1ddb40'
INDEX_SHA = '457a4d88f86617c448b980f31b7dd5b7d8bbeb31e3cfe807103371859f2d7998'
MAINS_REPORT_SHA = '42f94024a5780ad94fc0c0f4ad525e65faa1b02494d823574e964b1d8a0cf3c4'
MAINS_INDEX_SHA = '53e883235f6dbcbe15cfd4046ddc8f99578f64e4266bb6623775028d0d4668f7'
EXTRAS_MANIFEST_SHA = '5bfb18a68440d0144f9eba8915561647094208664a287f263ae9d0d1f83b6e38'
EXTRAS_COMPLETED_SHA = 'dae031f6fe222faa28900b65d62271823a565e9866cf758f3f45212115dcd187'
EXTRAS_REPORT_SHA = '8e4f9323495408540e6d97ecba19a6ba31f679ca0129a4c8eaf4d5277019c51e'
PID, TICKS = 332054, '357218'
PARENT, EMPTY, MISSING = '87', '88', '2147483647'
CHILDREN = {'alpha': '91', 'deleted': '90', 'zeta': '92', 'trailer': '89'}
MAX_BODY, MAX_BYTES, CLEANUP_BODY = 2 << 20, 48 << 20, 64 << 10


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def route(path, query=None):
    return path + ('?' + urlencode(query) if query else '')


def load_inputs(args):
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Run this recorder only through authorized root SSH on test-env.')
    require(re.fullmatch('[0-9a-f]{64}', args.script_sha256), 'The reviewed source digest is malformed.')
    require(args.fixture_operator_path.is_absolute() and args.fixture_operator_path.is_relative_to(WORK) and
            args.fixture_operator_path.name == 'add-client-special-features-reference.py' and
            '..' not in args.fixture_operator_path.parts, 'The frozen attestor path is outside its approved workspace.')
    # Attest the frozen Python source before executing its definitions.
    for path in (args.fixture_operator_path, Path(__file__).absolute()):
        for ancestor in reversed((path, *path.parents)):
            info = ancestor.lstat()
            require(not ancestor.is_symlink() and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                    'A source path is not protected and root-owned.')
        require(path.is_file() and path.stat().st_nlink == 1 and path.stat().st_size <= 1 << 20,
                'A source file exceeds its protected scope.')
    raw = args.fixture_operator_path.read_bytes()
    require(sha(raw) == BASE_SHA and sha(Path(__file__).read_bytes()) == args.script_sha256, 'A reviewed source changed.')
    base = types.ModuleType('frozen_special_features_attestor')
    base.__file__ = str(args.fixture_operator_path)
    exec(compile(raw, str(args.fixture_operator_path), 'exec'), base.__dict__)
    base_args = types.SimpleNamespace(phase='extras', script_sha256=BASE_SHA, support_path=SUPPORT_PATH,
        support_sha256=SUPPORT_SHA, operator_sha256=PREPARER_SHA, owner_sha256=REFERENCE_OWNER_SHA,
        auxiliary_index_sha256=INDEX_SHA, media_manifest_sha256=EXTRAS_MANIFEST_SHA,
        media_completed_sha256=EXTRAS_COMPLETED_SHA, mains_indexed_sha256=MAINS_INDEX_SHA)
    inputs = base.load_inputs(base_args)
    support, op, owner, browser, index, old_libraries = inputs
    media = base.new_media(base_args, support, op, owner, index)
    base.existing_control(base_args, inputs, media)
    require(owner['serviceIdentity']['pid'] == PID and owner['serviceIdentity']['startTicks'] == TICKS,
            'The fixed reference process lifetime changed.')
    report_raw = base.protected_source(FIXTURE_ROOT / 'extras/export/report.json')
    require(sha(report_raw) == EXTRAS_REPORT_SHA and
            sha(base.protected_source(FIXTURE_ROOT / 'mains/export/report.json')) == MAINS_REPORT_SHA,
            'A completed setup report changed.')
    report = support.decode(report_raw)
    require(report.get('result') == 'complete' and report.get('phase') == 'extras' and report.get('failureType') is None and
            report.get('scriptSha256') == BASE_SHA and report.get('libraryId') == '83' and
            report.get('allMediaPreserved') is True and report.get('oldLibrariesAndFiveUsersPreserved') is True and
            report.get('newTokensRevoked') is True and report.get('mainItems') == media[2]['items'],
            'The completed positive capture does not bind this fixture.')
    expected_auth = {'loginStatus': 200, 'ownedIdentityProven': True, 'logoutStatus': 204, 'exactTokenStatus': 401}
    require(report.get('authentication') == {'admin': expected_auth, 'viewer': expected_auth},
            'The setup recorder sessions were not proven closed.')
    failed_raw = base.protected_source(FAILED_REPORT)
    require(sha(failed_raw) == FAILED_REPORT_SHA, 'The original failed projection evidence changed.')
    failed = support.decode(failed_raw)
    require(failed.get('marker') == 'goby-reference-special-features-projections-v1' and
            failed.get('scriptSha256') == FAILED_SOURCE_SHA and failed.get('result') == 'retained_for_review' and
            failed.get('failureType') == 'RuntimeError' and failed.get('businessRequests') == 0 and failed.get('totalRequests') == 6 and
            failed.get('caseResults') == {} and failed.get('newRecorderTokenRevoked') is True and
            failed.get('authentication') == expected_auth and failed.get('allMediaPreserved') is True and
            failed.get('ordinaryUserAndExplicitItemsPreserved') is False and failed.get('childDirectProjectionsPreserved') is False,
            'The original attempt is not the acknowledged pre-business failure with its new token closed.')
    phase_owner = support.decode(base.protected_source(FIXTURE_ROOT / 'extras/OWNER.json'))
    require(phase_owner.get('inputs') == base.input_bindings(base_args) and
            phase_owner.get('phaseIdentity') == base.directory_identity(support, op, FIXTURE_ROOT / 'extras', 0o700) and
            phase_owner.get('commonOwnerSha256') == report.get('commonOwnerSha256'), 'The completed extras phase identity changed.')
    populations = report['preservationScope']['oldVisibleItemIdsByUser']
    require(set(populations) == set(index['existingUserIds']) and len(populations) == 5 and len(old_libraries | {'83'}) == 7 and
            {browser['accounts'][key]['userId'] for key in ('admin', 'viewer', 'viewer2')} <= set(populations),
            'The seven-library/five-user ownership binding changed.')
    selected = {}
    for label, expected in (('positive-specialfeatures-fields', {'91', '90', '92'}), ('positive-localtrailers-fields', {'89'})):
        saved = report['caseResults'][label]['response']
        require(saved.get('status') == 200 and saved.get('complete') is True and isinstance(saved.get('body'), list) and
                {row.get('Id') for row in saved['body']} == expected, 'A completed child membership changed.')
        for item in saved['body']:
            relative = Path(item['Path']).relative_to(base.MEDIA).as_posix()
            require(item.get('ParentId') == PARENT and relative in media[0]['files'] and
                    item.get('Type') == ('Trailer' if item['Id'] == '89' else 'Video'), 'A child is not an owned indexed media member.')
            selected[item['Id']] = item
    scope_ids = sorted(set(populations[browser['accounts']['viewer']['userId']]) | {PARENT, EMPTY} | set(selected))
    require(len(scope_ids) <= 128 and MISSING not in scope_ids and all(re.fullmatch('[0-9]{1,10}', value) for value in scope_ids),
            'The explicit preservation identity set is malformed.')
    return base, base_args, inputs, media, report, selected, scope_ids


def cases(viewer_id, foreign_id):
    own = '/emby/Users/' + viewer_id + '/Items/'
    foreign = '/emby/Users/' + foreign_id + '/Items/' + PARENT
    parent, sf, trailers = own + PARENT, own + PARENT + '/SpecialFeatures', own + PARENT + '/LocalTrailers'
    result = [
        ('parent-default', parent, True),
        ('parent-count-fields', route(parent, {'Fields': 'ItemCounts,Path,ParentId,MediaSources,MediaStreams'}), True),
        *[('child-' + key, own + item_id, True) for key, item_id in CHILDREN.items()],
        ('special-features-repeat-01', sf, True), ('special-features-repeat-02', sf, True),
        ('special-features-no-user-data', route(sf, {'EnableUserData': 'false'}), True),
        ('special-features-no-images', route(sf, {'EnableImages': 'false'}), True),
        ('special-features-media-sources', route(sf, {'Fields': 'MediaSources'}), True),
        ('local-trailers-no-user-data', route(trailers, {'EnableUserData': 'false'}), True),
        ('local-trailers-no-images', route(trailers, {'EnableImages': 'false'}), True),
        ('local-trailers-media-sources', route(trailers, {'Fields': 'MediaSources'}), True),
        ('missing-detail', own + MISSING, True), ('missing-special-features', own + MISSING + '/SpecialFeatures', True),
        ('foreign-special-features', foreign + '/SpecialFeatures', True), ('foreign-local-trailers', foreign + '/LocalTrailers', True),
        ('anonymous-special-features', sf, False), ('anonymous-local-trailers', trailers, False),
    ]
    require(len(result) == 20 and len({value[0] for value in result}) == 20, 'The fixed twenty-case plan changed.')
    return result


class Recorder:
    def __init__(self, args, loaded, control):
        self.args, self.loaded, self.control = args, loaded, control
        self.base, self.base_args, inputs, self.media, self.prior, self.selected, self.scope_ids = loaded
        self.support, self.op, self.owner, browser, self.index, self.old_libraries = inputs
        self.user, self.foreign = browser['accounts']['viewer'], browser['accounts']['viewer2']
        self.token, self.proven, self.revoked = None, False, False
        self.secrets = {account['password'] for account in browser['accounts'].values()}
        self.device = control['deviceId']
        self.sequence = self.business_count = self.received = self.revocation_bytes = 0
        self.finishing, self.deadline = False, time.monotonic() + 180
        self.reserved, self.records, self.before, self.after = set(), {}, None, None
        self.recorded_before_item_ids = None
        self.authentication = {'loginStatus': None, 'ownedIdentityProven': False, 'logoutStatus': None, 'exactTokenStatus': None}
        self.plan = cases(self.user['userId'], self.foreign['userId'])
        user_path = '/emby/Users/' + self.user['userId']
        self.state_routes = {'user': user_path, 'items': route(user_path + '/Items', {
            'Ids': ','.join(self.scope_ids), 'Recursive': 'true', 'Fields': self.base.FIELDS,
            'IncludeItemTypes': self.base.TYPES + ',Trailer',
            'Limit': '128', 'EnableTotalRecordCount': 'true', 'EnableUserData': 'true'})}
        self.approved = {label: ('GET', path, authenticated, True) for label, path, authenticated in self.plan}
        for phase in ('before', 'after'):
            self.approved.update({phase + '-' + key: ('GET', path, True, False) for key, path in self.state_routes.items()})
        self.approved.update({'after-child-' + key: ('GET', user_path + '/Items/' + value, True, False) for key, value in CHILDREN.items()})
        self.approved.update({'public-identity': ('GET', '/emby/System/Info/Public', False, False),
            'login': ('POST', '/emby/Users/AuthenticateByName', False, False),
            'logout': ('POST', '/emby/Sessions/Logout', True, False), 'exact-token': ('GET', '/emby/System/Info', True, False)})

    def save(self, name, value):
        require(re.fullmatch('[a-z0-9-]+[.]json', name), 'An evidence name is invalid.')
        self.op.save(PRIVATE / name, value)

    def check(self):
        require(sha(self.base.protected_source(Path(__file__).absolute())) == self.args.script_sha256 and
                sha(self.base.protected_source(self.args.fixture_operator_path)) == BASE_SHA and
                sha(self.base.protected_source(SUPPORT_PATH)) == SUPPORT_SHA and
                sha(self.base.protected_source(self.support.OPERATOR)) == PREPARER_SHA and
                sha(self.base.protected_source(self.op.OWNER)) == REFERENCE_OWNER_SHA and
                sha(self.base.protected_source(FIXTURE_ROOT / 'extras/export/report.json')) == EXTRAS_REPORT_SHA and
                sha(self.base.protected_source(FIXTURE_ROOT / 'mains/export/report.json')) == MAINS_REPORT_SHA and
                sha(self.base.protected_source(FAILED_REPORT)) == FAILED_REPORT_SHA and
                sha(self.base.protected_source(self.base.INDEXED)) == MAINS_INDEX_SHA and
                self.support.decode(self.base.protected_source(ROOT / 'OWNER.json')) == self.control and
                self.base.directory_identity(self.support, self.op, ROOT, 0o700) == self.control['rootIdentity'],
                'A pinned source, receipt or exclusive evidence identity changed.')
        require(self.op.digest(self.op.BROWSER) == self.owner['browserSha256'] and self.op.digest(self.op.REPORT) == self.owner['reportSha256'],
                'The existing credential/evidence files changed.')
        self.op.same_service(self.owner)
        require(os.readlink('/proc/self/ns/net') == self.owner['serviceIdentity']['networkNamespace'], 'HTTP left the fixed reference namespace.')

    def request(self, label):
        self.check()
        require(label in self.approved and label not in self.reserved and self.sequence < (42 if self.finishing else 28) and
                time.monotonic() < self.deadline, 'A request is repeated, out of scope or over budget.')
        method, path, authenticated, business = self.approved[label]
        require(label in ('public-identity', 'login') or self.proven, 'The fresh ordinary recorder identity is not proven.')
        require(not self.finishing or not business and label != 'login', 'Cleanup cannot start new business work.')
        require(label != 'exact-token' or 'logout' in self.reserved, 'The token proof cannot precede its owned logout attempt.')
        require(method == 'GET' or label in ('login', 'logout'), 'Only the new login/logout can use POST.')
        if business:
            require(self.business_count < 20, 'The twenty-case business budget is exhausted.')
            self.business_count += 1
        self.reserved.add(label)
        self.sequence += 1
        self.save(f'{self.sequence:04d}-{label}-intent.json', {'label': label, 'method': method, 'path': path,
            'authenticated': authenticated, 'business': business, 'businessCount': self.business_count})
        headers = {'Accept': 'application/json', 'Accept-Encoding': 'identity'}
        if authenticated or label == 'login':
            headers['Authorization'] = f'Emby Client="Goby Special Features Projection Recorder", Device="Linux Recorder", DeviceId="{self.device}", Version="1.0"'
        if authenticated:
            require(self.proven and self.token is not None, 'An authenticated request lacks its exact owned token.')
            headers['X-Emby-Token'] = self.token
        body = None
        if label == 'login':
            require(self.token is None, 'The recorder cannot log in twice.')
            body = self.base.encoded({'Username': self.user['username'], 'Pw': self.user['password']})
            headers['Content-Type'] = 'application/json'
        cleanup = label in ('logout', 'exact-token')
        bound = CLEANUP_BODY if cleanup else MAX_BODY
        status, mime, value, raw, complete, failure = None, None, None, b'', False, None
        connection = http.client.HTTPConnection('127.0.0.1', 18097, timeout=8)
        signal.setitimer(signal.ITIMER_REAL, min(12, max(0.01, self.deadline - time.monotonic())))
        try:
            connection.request(method, path, body, headers)
            response = connection.getresponse()
            status, mime = response.status, response.getheader('Content-Type')
            require(not 300 <= status < 400, 'Redirects cannot leave the reviewed route.')
            length = response.getheader('Content-Length')
            require(length is None or length.isdigit() and int(length) <= bound, 'A response exceeds its byte bound.')
            raw = response.read(bound + 1)
            require(len(raw) <= bound and (length is None or len(raw) == int(length)), 'A response is incomplete or oversized.')
            self.received += len(raw)
            if cleanup:
                self.revocation_bytes += len(raw)
                require(self.revocation_bytes <= 2 * CLEANUP_BODY, 'The separate revocation budget is exhausted.')
            else:
                require(self.received <= MAX_BYTES, 'The projection/preservation response budget is exhausted.')
            value = self.support.decode(raw) if raw and mime and mime.lower().split(';', 1)[0] == 'application/json' else raw.decode('utf-8') if raw else None
            complete = True
            if label == 'login' and isinstance(value, dict) and isinstance(value.get('AccessToken'), str) and 0 < len(value['AccessToken']) <= 8192:
                self.token = value['AccessToken']
                self.secrets.add(self.token)
                user, session = value.get('User', {}), value.get('SessionInfo', {})
                self.proven = (status == 200 and value.get('ServerId') == self.owner['serverId'] and
                    user.get('Id') == self.user['userId'] and user.get('Name') == self.user['username'] and
                    user.get('Policy', {}).get('IsAdministrator') is False and
                    session.get('DeviceId') == self.device and session.get('UserId') == self.user['userId'])
                self.save('login-acknowledgement.json', value)
        except Exception as error:
            failure = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
            record = {'request': {'method': method, 'path': path, 'authenticated': authenticated,
                                  'authorizationSent': authenticated or label == 'login', 'business': business},
                      'response': {'status': status, 'contentType': mime, 'bytes': len(raw), 'sha256': sha(raw),
                                   'complete': complete, 'failureType': failure, 'body': value}}
            self.records[label] = record
            self.save(f'{self.sequence:04d}-{label}-response.json', record)
        require(complete and failure is None, 'The request did not complete; no request may be retried.')
        self.check()
        if label == 'login':
            self.authentication.update({'loginStatus': status, 'ownedIdentityProven': self.proven})
            require(self.proven, 'The login acknowledgement remains unproven and quarantined.')
        return record

    def snapshot(self, phase):
        user_record, item_record = self.request(phase + '-user'), self.request(phase + '-items')
        user, body = user_record['response']['body'], item_record['response']['body']
        if phase == 'before':
            raw_items = body.get('Items') if isinstance(body, dict) else None
            if isinstance(raw_items, list) and len(raw_items) <= 128 and all(
                    isinstance(row, dict) and isinstance(row.get('Id'), str) and re.fullmatch('[0-9]{1,10}', row['Id'])
                    for row in raw_items):
                self.recorded_before_item_ids = sorted(row['Id'] for row in raw_items)
            self.save('before-item-ids-observed.json', {'diagnosticOnly': True, 'preservationGatePassed': None,
                'responseStatus': item_record['response']['status'], 'responseSha256': item_record['response']['sha256'],
                'recordedBeforeItemIds': self.recorded_before_item_ids})
        require(user_record['response']['status'] == item_record['response']['status'] == 200 and isinstance(user, dict) and
                user.get('Id') == self.user['userId'] and isinstance(user.get('Configuration'), dict) and isinstance(user.get('Policy'), dict),
                'The ordinary user preservation witness is unavailable.')
        items = self.support.rows(body, 128)
        old_visible = set(self.prior['preservationScope']['oldVisibleItemIdsByUser'][self.user['userId']])
        require(old_visible | {PARENT, EMPTY} <= set(items) <= set(self.scope_ids) and all(
            isinstance(row.get('UserData'), dict) for row in items.values()), 'The explicit user-data projection is incomplete.')
        for key, row in items.items():
            path = row.get('Path')
            require(path is None or path == '' or isinstance(path, str) and any(
                path == str(root) or path.startswith(str(root) + '/') for root in (*self.base.OLD_ROOTS, self.base.MEDIA)),
                'A selected item escaped the three owned media roots.')
        result = {'user': {key: user[key] for key in ('Id', 'Name', 'Configuration', 'Policy')}, 'items': items}
        self.save(phase + '-state.json', result)
        return result

    def logout(self):
        self.finishing, self.deadline = True, time.monotonic() + 40
        if not self.proven:
            require(self.token is None, 'An unproven acknowledgement remains quarantined; unknown credentials are not used or revoked.')
            return
        result = self.request('logout')['response']
        self.authentication['logoutStatus'] = result['status']
        require(result['status'] == 204 and result['body'] is None, 'The new token logout was not acknowledged.')
        result = self.request('exact-token')['response']
        self.authentication['exactTokenStatus'] = result['status']
        require(result['status'] == 401, 'The exact logged-out token remains accepted.')
        self.revoked = True

    def sanitize(self, value):
        if isinstance(value, dict):
            value = {key: '[redacted]' if key.lower() in ('stack', 'stacktrace', 'apikey', 'access_token', 'x-mediabrowser-token')
                     else item for key, item in value.items()}
        return self.base.Recorder.sanitize(self, value)

    def run(self):
        failure, preserved, media_preserved, children_preserved = None, False, False, False
        try:
            result = self.request('public-identity')['response']
            require(result['status'] == 200 and result['body'].get('Id') == self.owner['serverId'] and
                    result['body'].get('Version') == '4.9.5.0', 'The public reference identity changed.')
            self.request('login')
            self.before = self.snapshot('before')
            for label, _, _ in self.plan:
                self.request(label)
        except Exception as error:
            failure = type(error).__name__
        finally:
            self.finishing, self.deadline = True, time.monotonic() + 90
            try:
                if self.proven and self.before is not None:
                    self.after = self.snapshot('after')
                    require(self.base.same(self.before, self.after), 'The ordinary user configuration or explicit item projection changed.')
                    preserved = True
                    comparisons = {}
                    for key in CHILDREN:
                        if 'child-' + key in self.records:
                            after = self.request('after-child-' + key)['response']
                            before = self.records['child-' + key]['response']
                            comparisons[key] = (before['complete'] and before['status'] == after['status'] and self.base.same(before['body'], after['body']))
                    children_preserved = len(comparisons) == 4 and all(comparisons.values())
                    require(all(comparisons.values()), 'A child direct-read projection changed.')
            except Exception as error:
                failure = failure or type(error).__name__
            try:
                self.logout()
            except Exception as error:
                failure = failure or type(error).__name__
            try:
                observed = self.base.new_media(self.base_args, self.support, self.op, self.owner, self.index)
                require(self.base.same(observed, self.media), 'An old or new media receipt, identity or byte changed.')
                self.check()
                media_preserved = True
            except Exception as error:
                failure = failure or type(error).__name__
        complete = failure is None and self.business_count == 20 and preserved and children_preserved and media_preserved and self.revoked
        report = {'marker': MARKER, 'result': 'complete' if complete else 'retained_for_review', 'failureType': failure,
            'scriptSha256': self.args.script_sha256, 'fixtureOperatorSha256': BASE_SHA, 'priorExtrasReportSha256': EXTRAS_REPORT_SHA,
            'priorFailedAttempt': {'path': str(FAILED_REPORT), 'sha256': FAILED_REPORT_SHA, 'businessRequests': 0,
                                  'newRecorderTokenRevoked': True, 'result': 'retained_for_review'},
            'referenceIdentity': self.owner['serviceIdentity'], 'authentication': self.authentication,
            'businessRequests': self.business_count, 'totalRequests': self.sequence, 'responseBytes': self.received,
            'caseResults': {label: self.records[label] for label, _, _ in self.plan if label in self.records},
            'ordinaryUserAndExplicitItemsPreserved': preserved, 'childDirectProjectionsPreserved': children_preserved,
            'recordedBeforeItemIds': self.recorded_before_item_ids,
            'allMediaPreserved': media_preserved, 'newRecorderTokenRevoked': self.revoked,
            'populationBinding': {'libraryIds': sorted(self.old_libraries | {'83'}), 'userIds': sorted(self.index['existingUserIds']),
                'source': 'Completed seven-library/five-user receipts; no new administrator inventory or other-user state read.'},
            'preservationScope': {'userId': self.user['userId'], 'requestedItemIds': self.scope_ids,
                'returnedItemIds': sorted(self.before['items']) if self.before else [],
                'boundary': 'Exact accessible API projections plus immutable media, not a raw database or all-user/session-history equality claim.'},
            'missingIdBoundary': '2147483647 is a fixed missing-ID candidate, not a preasserted HTTP status.',
            'boundary': 'All actual statuses are observations. Capture completion does not prove full permissions, ordering, playback or original-client compatibility.'}
        report = self.sanitize(report)
        require(not any(secret in self.base.encoded(report).decode() for secret in self.secrets), 'A credential survived safe export.')
        self.op.save(EXPORT / 'report.json', report)
        print(json.dumps({'result': report['result'], 'report': str(EXPORT / 'report.json'), 'newRecorderTokenRevoked': self.revoked}), flush=True)
        return 0 if complete else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--script-sha256', required=True)
    parser.add_argument('--fixture-operator-path', type=Path, required=True)
    parser.add_argument('--check-only', action='store_true')
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    parser.add_argument('--lock-fd', type=int, help=argparse.SUPPRESS)
    args = parser.parse_args()
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError('A bounded request expired.')))
    require(not (args.worker and args.check_only) and (args.worker or args.lock_fd is None), 'The invocation mode is inconsistent.')
    loaded = load_inputs(args)
    base, _, inputs, media, _, _, _ = loaded
    support, op, owner, _, _, _ = inputs
    bindings = {'scriptSha256': args.script_sha256, 'fixtureOperatorPath': str(args.fixture_operator_path), 'fixtureOperatorSha256': BASE_SHA}
    if args.worker:
        control = support.decode(base.protected_source(ROOT / 'OWNER.json'))
        require(type(args.lock_fd) is int and args.lock_fd > 2 and control.get('marker') == MARKER and control.get('path') == str(ROOT) and
                control.get('inputs') == bindings and control.get('supervisor') == op.process_identity(os.getppid()) and
                support.identity(os.fstat(args.lock_fd)) == support.identity(op.canonical(op.LOCK, mode=0o600)) and
                os.readlink('/proc/self/ns/net') == owner['serviceIdentity']['networkNamespace'] and
                base.same(control.get('media'), media) and re.fullmatch('goby-m3e-special-features-projections-[0-9a-f]{32}', control.get('deviceId', '')),
                'The worker lacks its exact live supervisor, inherited lock, namespace, media or device binding.')
        recorder = Recorder(args, loaded, control)
        recorder.check()
        op.save(PRIVATE / 'worker.json', op.process_identity(os.getpid()))
        return recorder.run()
    op.canonical(op.LOCK, mode=0o600)
    lock, namespace = os.open(op.LOCK, os.O_RDONLY | os.O_NOFOLLOW), None
    try:
        require(support.identity(os.fstat(lock)) == support.identity(op.canonical(op.LOCK, mode=0o600)), 'The shared reference lock changed.')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not op.present(ROOT), 'Existing projection evidence cannot be overwritten, adopted or retried.')
        op.same_service(owner)
        if args.check_only:
            print(json.dumps({'result': 'preflight_passed', 'httpRequests': 0, 'evidenceWrites': 0, 'businessCases': 20}), flush=True)
            return 0
        ROOT.mkdir(mode=0o700)
        PRIVATE.mkdir(mode=0o700)
        EXPORT.mkdir(mode=0o700)
        control = {'marker': MARKER, 'path': str(ROOT), 'inputs': bindings, 'supervisor': op.process_identity(os.getpid()),
            'rootIdentity': base.directory_identity(support, op, ROOT, 0o700), 'media': media,
            'deviceId': 'goby-m3e-special-features-projections-' + secrets.token_hex(16)}
        op.save(ROOT / 'OWNER.json', control)
        op.sync_directory(WORK)
        namespace = os.open(f'/proc/{PID}/ns/net', os.O_RDONLY)
        require(os.fstat(namespace).st_ino == int(owner['serviceIdentity']['networkNamespace'][5:-1]), 'The reference namespace handle changed.')
        op.same_service(owner)
        op.save(PRIVATE / 'worker-dispatch-intent.json', {'inputs': bindings, 'supervisor': control['supervisor']})
        result = subprocess.run(['/usr/bin/nsenter', '--net=/proc/self/fd/' + str(namespace), '/usr/bin/python3', '-I', '-B',
            str(Path(__file__).absolute()), '--script-sha256', args.script_sha256, '--fixture-operator-path', str(args.fixture_operator_path),
            '--worker', '--lock-fd', str(lock)], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            check=False, timeout=450, pass_fds=(lock, namespace), env=dict(op.BASE_ENV, SSH_CONNECTION=os.environ['SSH_CONNECTION']))
        require(len(result.stdout) <= 8192 and len(result.stderr) <= 8192, 'The worker exceeded its terminal-output bound.')
        op.save(PRIVATE / 'worker-exit.json', {'returnCode': result.returncode, 'stdoutSha256': sha(result.stdout), 'stderrSha256': sha(result.stderr)})
        print(json.dumps({'result': 'complete' if result.returncode == 0 else 'retained_for_review', 'report': str(EXPORT / 'report.json')}), flush=True)
        return 0 if result.returncode == 0 else 1
    finally:
        if namespace is not None:
            os.close(namespace)
        os.close(lock)


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'result': 'retained_for_review', 'failureType': type(error).__name__, 'evidence': str(ROOT), 'retryPermitted': False}), file=sys.stderr)
        raise SystemExit(1)
