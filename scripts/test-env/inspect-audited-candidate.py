#!/usr/bin/env python3
"""Inspect the fresh embedded candidate without bootstrap, authentication or service changes.

The initial workflow reads only its new PostgreSQL cluster and public HTTP
endpoint. Runtime consumers may reuse the bounded metadata/SELECT primitives
with an explicit 900-second context; they never reopen initial output evidence.
SELECT strings are reviewed caller code, not untrusted input or a SQL sandbox.
"""
from __future__ import annotations
import argparse
import base64
from datetime import datetime, timezone
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import selectors
import signal
import stat
import subprocess
import sys
import time

E3 = Path('/opt/goby-test/embedded-candidate-20260914')
OLD_CANDIDATE = Path('/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b')
PG = Path('/usr/lib/postgresql/17/bin')
PROVISION_SHA = 'cecd04aa13f76a389ae7f42e2215b5c127eaf60c4a3d2c82702fea70352884d2'
SOURCE_ARCHIVE = {'path': '/opt/goby-test/full-regression-20260914/retained/source.tar.gz', 'sha256': '418f9803237e02e0859f9bef8f6ed646730acb590a3482867a288cd64e8a0fad'}
SOURCE_MANIFEST = {'path': '/opt/goby-test/full-regression-continuation-20260914/retained/source-manifest.json', 'sha256': '95fe6a40ecabdfe6b48260dcf200cf8a49d0cf9664c407ca91cd9eaba539d6f6'}
EMBEDDED_MANIFEST = {'path': '/opt/goby-test/m6-embedded-20260914/artifacts/linux-amd64/manifest.json', 'sha256': 'a732425002323e0e29a4ca7cf9e0fd517d773c1de027d810488173b75957e683'}
BINARY_SHA = '59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312'
BINARY_BYTES = 30678868
INDEX_SHA = '60f1aa03d54480eecddf21ed5458b9f7e06a212413601f421e47e4846130d358'
PRODUCT = {'sourceArchive': SOURCE_ARCHIVE, 'sourceManifest': SOURCE_MANIFEST, 'embeddedBuildManifest': EMBEDDED_MANIFEST,
    'productionSourceBinding': {'path': '/opt/goby-test/full-regression-20260914/production-source-binding.json', 'sha256': '823870bb9bcf9e2ca4b35e483d617de34e1c541b273cd6bf02e647a066e920dc'},
    'regressionReview': {'path': '/opt/goby-test/full-regression-continuation-20260914/independent-review.json', 'sha256': '5b946d4b4bfee2b177b53861c89fad690c08c406b832a26f778d563ed4174b72'},
    'regressionClosure': {'path': '/opt/goby-test/full-regression-continuation-20260914/closure.json', 'sha256': '6cd0b024834315db240d6268c94b578e4beb99977fd1740d51c5494cf4508b1c'}}
BACKUP_ENV = {'GOBY_BACKUP_MAX_OBJECT_BYTES': '67108864', 'GOBY_BACKUP_MAX_TOTAL_BYTES': '268435456'}
BUDGETS = {'maximumSeconds': 180, 'maximumSqlSessions': 16, 'maximumHttpRequests': 64, 'maximumHttpBodyBytes': 4194304}
RUNTIME_BUDGETS = {'maximumSeconds': 900, 'maximumSqlSessions': 64}
UNIT_FIELDS = {'Id', 'LoadState', 'ActiveState', 'SubState', 'MainPID', 'InvocationID', 'Result', 'ExecMainStatus', 'ControlGroup'}
IDENTITY_FIELDS = {'pid', 'startTicks', 'bootId', 'exe', 'uid', 'cgroup', 'executableDevice', 'executableInode', 'invocationId'}
ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LANG': 'C.UTF-8', 'LC_ALL': 'C.UTF-8', 'SYSTEMD_COLORS': '0'}
CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
FORBIDDEN = {'upgrade-main-schema25.py', 'dispose-source41-resource-full-failed-pair.py', 'test-dispose-source41-resource-full-failed-pair.py'}
PRIOR_METADATA_FAILURE = {'path': str(E3 / 'initial-runtime-inspection-01/report.json'), 'sha256': '7622127b9f288bc274566f1f05af80883120258dc55641d5d71618a40a26605d'}
PRIOR_INPUT = {'path': str(E3 / 'private/inspection-input.json'), 'sha256': '5ed2b16a20cc9659a06d49f043c03ad4e1e261c418cc23f096c210b1d8de6a28'}
PRIOR_HELPER = {'path': str(E3 / 'private/operators/inspect-audited-candidate.py'), 'sha256': '998c9e6ee7d8998291661c075e244bcb221a191edb8871956b081d3ae31a5e0a'}
REVIEWED_HELPER = E3 / 'private/operators-reviewed/inspect-audited-candidate.py'
METADATA_CORRECTION_REVIEW = {'path': str(E3 / 'empty-environment-files-review.json'), 'sha256': '1b518abf9491027366936e7b88e4738d22c0a609d87c029e2f5d54b11c8c9ce8'}
PRIOR_SQL_FAILURE = {'path': str(E3 / 'initial-runtime-inspection-02/report.json'), 'sha256': '1aaad33511366fd3b9eb54923460314f0bdd63c4160deb6fa3710ba4816f83db'}
SQL_FAILURE_DIAGNOSTIC = {'path': str(E3 / 'private/first-sql-diagnostic/report.json'), 'sha256': '65370c3558444a5e7503e8709ea30ceee4e1cf11c61a468699cc6d2ac882c47d'}
FAILED_SQL_HELPER = {'path': str(REVIEWED_HELPER), 'sha256': 'fb69aba3168ae896423f17dcd125aeab63ae43d3710e1e0cb6bebc0a81c96bde'}
SQL_CORRECTED_HELPER = E3 / 'private/operators-sql-corrected/inspect-audited-candidate.py'
SQL_WARNING_SHA = '88cb46494b3249cb7cc354338357ec6718eae891e2a2bbf86715ce129869ecff'
SQL_CORRECTION = {'priorFailurePreserved': True, 'priorSqlResultRecovered': False, 'diagnosticReadProcessesClosed': True,
                  'passfilePolicy': 'owned_socket_path_required_absent', 'stderrPolicy': 'empty_required'}


class InspectionError(ValueError):
    """Only fixed nonsecret diagnostic codes leave the inspector."""


def need(ok, code):
    if not ok:
        raise InspectionError(code)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def parse(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            need(key not in result, 'duplicate_json_key')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda unused: (_ for _ in ()).throw(InspectionError('nonfinite_json')))


def descriptor(value):
    need(isinstance(value, dict) and set(value) == {'path', 'sha256'} and isinstance(value['path'], str) and
         Path(value['path']).is_absolute() and '..' not in Path(value['path']).parts and Path(value['path']).name not in FORBIDDEN and
         isinstance(value['sha256'], str) and re.fullmatch(r'[0-9a-f]{64}', value['sha256']), 'descriptor_invalid')
    return value


def signature(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def read_checked(path, checksum=None, maximum=8 << 20, owner=0):
    path = Path(path)
    need(path.is_absolute() and '..' not in path.parts and path.name not in FORBIDDEN and path.resolve() == path, 'read_path_invalid')
    for parent in path.parents:
        info = parent.lstat()
        need(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022, 'read_ancestry_unsafe')
    before = path.lstat()
    need(stat.S_ISREG(before.st_mode) and before.st_uid == owner and before.st_nlink == 1 and
         not before.st_mode & 0o022 and before.st_size <= maximum, 'read_file_unsafe')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as stream:
        need(signature(os.fstat(stream.fileno())) == signature(before), 'read_open_changed')
        raw = stream.read(maximum + 1)
        need(signature(os.fstat(stream.fileno())) == signature(before), 'read_file_changed')
    need(signature(path.lstat()) == signature(before) and len(raw) == before.st_size and
         (checksum is None or sha(raw) == checksum), 'read_digest_changed')
    return raw


def read_descriptor(value, maximum=8 << 20):
    descriptor(value)
    return parse(read_checked(value['path'], value['sha256'], maximum))


def validate_input(value):
    common = {'kind', 'version', 'output', 'provisionManifest', 'provisionInput', 'provisionHelper', 'sourceArchive', 'sourceManifest', 'budgets'}
    need(isinstance(value, dict) and type(value.get('version')) is int and value['version'] in (1, 2, 3), 'inspection_input_version')
    keys = common | ({'priorMetadataFailure'} if value['version'] >= 2 else set())
    if value['version'] == 3:
        keys |= {'priorSqlFailure', 'sqlFailureDiagnostic'}
    outputs = {1: 'initial-runtime-inspection-01', 2: 'initial-runtime-inspection-02', 3: 'initial-runtime-inspection-sql-corrected'}
    need(isinstance(value, dict) and set(value) == keys and value['kind'] == 'audited-candidate-inspection-input' and
         value['budgets'] == BUDGETS and
         all(type(number) is int for number in value['budgets'].values()) and
         value['output'] == str(E3 / outputs[value['version']]),
         'inspection_input_contract')
    for key in keys - {'kind', 'version', 'output', 'budgets'}:
        descriptor(value[key])
    need(value['sourceArchive'] == SOURCE_ARCHIVE and value['sourceManifest'] == SOURCE_MANIFEST and
         Path(value['provisionInput']['path']).is_relative_to(E3) and Path(value['provisionHelper']['path']).is_relative_to(E3) and
         Path(value['provisionHelper']['path']).name == 'prepare-audited-candidate.py' and
         value['provisionHelper']['sha256'] == PROVISION_SHA, 'inspection_source_contract')
    candidate = Path(value['provisionManifest']['path'])
    need(re.fullmatch(r'/opt/goby-audited-candidate-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}/private/manifest\.json', str(candidate)) and
         candidate != OLD_CANDIDATE / 'private/manifest.json', 'old_or_foreign_candidate_refused')
    if value['version'] >= 2:
        need(value['priorMetadataFailure'] == PRIOR_METADATA_FAILURE, 'metadata_correction_failure_pin')
    if value['version'] == 3:
        need(value['priorSqlFailure'] == PRIOR_SQL_FAILURE and value['sqlFailureDiagnostic'] == SQL_FAILURE_DIAGNOSTIC,
             'sql_correction_failure_or_diagnostic_pin')
    return value


def validate_metadata_correction(value, prior, old_input):
    """Bind only the one closed metadata failure which sent no SQL or HTTP."""
    validate_input(value); validate_input(old_input)
    restored_input = {key: item for key, item in value.items() if key != 'priorMetadataFailure'}
    restored_input.update(version=1, output=str(E3 / 'initial-runtime-inspection-01'))
    need(value['version'] == 2 and old_input['version'] == 1 and restored_input == old_input, 'metadata_correction_inputs_changed')
    need(prior.get('kind') == 'audited-candidate-runtime-inspection' and type(prior.get('version')) is int and prior['version'] == 3 and
         prior.get('status') == 'inspection_failed' and prior.get('stage') == 'runtime_configuration' and
         prior.get('failure') == {'type': 'InspectionError', 'code': 'unit_process_changed'} and
         prior.get('input') == PRIOR_INPUT and prior.get('helper') == PRIOR_HELPER and
         all(prior.get(key) == value[key] for key in ('provisionManifest', 'provisionInput', 'provisionHelper')) and
         prior.get('sourceEvidence') == PRODUCT and prior.get('readProcessesClosed') is True and
         prior.get('sqlReadResults') == [] and prior.get('httpResponses') == [], 'metadata_failure_not_expected_and_closed')
    counters = prior.get('counters', {})
    need(set(counters) == {'readOnlySqlSessions', 'publicHttpRequests', 'httpBodyBytes', 'unitMetadataCalls'} and
         all(type(counters.get(key)) is int and counters[key] == 0 for key in ('readOnlySqlSessions', 'publicHttpRequests', 'httpBodyBytes')) and
         type(counters['unitMetadataCalls']) is int and 0 < counters['unitMetadataCalls'] < 4096 and
         type(prior.get('databaseWrites')) is int and prior['databaseWrites'] == 0 and
         all(prior.get(key) is False for key in ('bootstrapPerformed', 'restorePerformed', 'serviceChangesPerformed',
                                               'candidateAdmissionComplete', 'clientAcceptance')), 'metadata_failure_has_business_or_unclosed_work')


def validate_sql_correction(value, prior, old_input, diagnostic):
    """Authorize only the diagnosed second failure, without recovering its lost result."""
    validate_input(value); validate_input(old_input)
    restored_input = {key: item for key, item in value.items() if key not in ('priorSqlFailure', 'sqlFailureDiagnostic')}
    restored_input.update(version=2, output=str(E3 / 'initial-runtime-inspection-02'))
    need(value['version'] == 3 and old_input['version'] == 2 and restored_input == old_input, 'sql_correction_inputs_changed')
    input_pin = descriptor(prior.get('input'))
    need(Path(input_pin['path']).is_relative_to(E3 / 'private') and
         prior.get('kind') == 'audited-candidate-runtime-inspection' and type(prior.get('version')) is int and prior['version'] == 3 and
         prior.get('status') == 'inspection_failed' and prior.get('stage') == 'database_identity_and_lease' and
         prior.get('failure') == {'type': 'InspectionError', 'code': 'read_command_failed'} and
         prior.get('helper') == FAILED_SQL_HELPER and prior.get('priorMetadataFailure') == PRIOR_METADATA_FAILURE and
         prior.get('metadataCorrectionReview') == METADATA_CORRECTION_REVIEW and
         all(prior.get(key) == value[key] for key in ('provisionManifest', 'provisionInput', 'provisionHelper')) and
         prior.get('sourceEvidence') == PRODUCT and prior.get('readProcessesClosed') is False and
         prior.get('sqlReadResults') == [] and prior.get('httpResponses') == [], 'sql_failure_not_expected')
    counters = prior.get('counters', {})
    need(set(counters) == {'readOnlySqlSessions', 'publicHttpRequests', 'httpBodyBytes', 'unitMetadataCalls'} and
         all(type(counters.get(key)) is int and counters[key] == expected for key, expected in
             (('readOnlySqlSessions', 1), ('publicHttpRequests', 0), ('httpBodyBytes', 0))) and
         type(counters['unitMetadataCalls']) is int and 0 < counters['unitMetadataCalls'] < 4096 and
         type(prior.get('databaseWrites')) is int and prior['databaseWrites'] == 0 and
         all(prior.get(key) is False for key in ('bootstrapPerformed', 'restorePerformed', 'serviceChangesPerformed',
                                               'candidateAdmissionComplete', 'clientAcceptance')), 'sql_failure_scope_changed')
    need(diagnostic.get('kind') == 'first-candidate-sql-diagnostic' and type(diagnostic.get('version')) is int and diagnostic['version'] == 1 and
         diagnostic.get('status') == 'diagnostic_complete' and diagnostic.get('classification') == 'libpq_nonregular_password_file_warning' and
         all(diagnostic.get(key) is True for key in ('readOnly', 'originalInspectorWouldReject', 'frontendGroupClosed', 'backendGone', 'postmasterUnchanged')) and
         all(type(diagnostic.get(key)) is int and diagnostic[key] == expected for key, expected in
             (('sqlCommands', 1), ('httpRequests', 0), ('serviceChanges', 0), ('frontendExitCode', 0),
              ('stderrBytes', 55), ('otherPostgresPsqlBackendCount', 0))) and
         diagnostic.get('otherPostgresPsqlBackends') == [] and
         type(diagnostic.get('backendPid')) is int and diagnostic['backendPid'] > 1 and
         diagnostic.get('stderr', {}).get('sha256') == SQL_WARNING_SHA and diagnostic['stderr'].get('bytes') == 55,
         'sql_diagnostic_not_expected_and_closed')
    commands = diagnostic.get('commands')
    need(isinstance(commands, list) and [row.get('label') for row in commands] == ['pg-before', 'psql-once', 'pg-after'] and
         all(row.get('groupClosed') is True and type(row.get('exitCode')) is int and row['exitCode'] == 0 and
             row.get('timeoutOrFailure') is None for row in commands) and
         commands[1].get('stderr') == diagnostic['stderr'] and commands[1].get('stdout') == diagnostic.get('stdout'),
         'sql_diagnostic_command_closure')


def postgres_unit_object(unit):
    need(re.fullmatch(r'goby-audited-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}-postgres\.service', unit), 'dbus_unit_scope')
    label = ''.join(character if character.isascii() and character.isalnum() else '_%02x' % ord(character) for character in unit)
    return '/org/freedesktop/systemd1/unit/' + label


def validate_manifest(value, manifest, provision_input, helper):
    helper.validate_input(provision_input)
    need(provision_input['version'] == 2 and manifest['kind'] == 'audited-candidate-private-manifest' and
         manifest['status'] == 'running_awaiting_live_acceptance' and manifest['provisionVersion'] == 2 and
         manifest['input'] == value['provisionInput'] and manifest['runId'] == provision_input['runId'] and
         manifest['productEvidence'] == PRODUCT and all(provision_input[key] == pin for key, pin in PRODUCT.items()), 'provision_v2_binding')
    root = Path('/opt') / ('goby-audited-candidate-' + manifest['runId'])
    suffix = manifest['runId'].split('-')[-1]
    need(root != OLD_CANDIDATE and value['provisionManifest']['path'] == str(root / 'private/manifest.json') and
         manifest['binary'] == {'path': str(root / 'install/goby'), 'sha256': BINARY_SHA} and
         manifest['ports'] == provision_input['ports'] and manifest['publicUrl'] == provision_input['public_url'] and
         manifest['directUrl'] == 'http://127.0.0.1:' + str(manifest['ports']['http']) and
         manifest['database'] == 'goby_candidate_' + suffix and manifest['recoveryDatabase'] == 'goby_recovery_' + suffix and
         manifest['dataDirectory'] == str(root / 'data') and manifest['runtime']['path'] == str(root / 'private/runtime.env'), 'candidate_layout_binding')
    need(manifest['ordinaryRegressionStatus'] == 'passed_with_explicit_profile_gap' and manifest['ordinaryRegressionPhases'] == 2 and
         manifest['taggedFullRegressionClaimed'] is False and manifest['dashboard'] == {'mode': 'embedded', 'buildManifest': EMBEDDED_MANIFEST,
             'assetCount': 57, 'externalDirectoryInstalled': False, 'webDirectoryOverridePresent': False} and
         manifest['backupProfile'] == {'name': 'bounded-fixture-backup-v1', 'environment': BACKUP_ENV} and
         all(manifest[key] is False for key in ('bootstrapExecuted', 'recoveryRestoreExecuted', 'clientAcceptance', 'candidateAdmissionComplete')) and
         manifest['sourceState'] == {'users': 0, 'schema': 28, 'migrations': 28}, 'initial_candidate_contract')
    need(set(manifest['processes']) == set(manifest['units']) == {'server', 'postgres'}, 'candidate_unit_roles')
    identities = {}
    for role in ('server', 'postgres'):
        unit = 'goby-audited-' + manifest['runId'] + '-' + role + '.service'
        row = manifest['processes'][role]
        identity = manifest['serverIdentity' if role == 'server' else 'postgresIdentity']
        descriptor(manifest['units'][role])
        need(set(row) == UNIT_FIELDS and row['Id'] == unit and row['MainPID'].isdigit() and int(row['MainPID']) > 1 and
             row['LoadState'] == 'loaded' and row['ActiveState'] == 'active' and row['SubState'] == 'running' and
             row['Result'] == 'success' and row['ExecMainStatus'] == '0' and row['ControlGroup'] == '/system.slice/' + unit and
             re.fullmatch(r'[0-9a-f]{32}', row['InvocationID']) and manifest['units'][role]['path'] == '/run/systemd/system/' + unit,
             'candidate_unit_identity')
        need(set(identity) == IDENTITY_FIELDS and identity['pid'] == int(row['MainPID']) and
             type(identity['pid']) is int and type(identity['uid']) is int and identity['uid'] > 0 and
             identity['invocationId'] == row['InvocationID'] and identity['cgroup'] == row['ControlGroup'] and
             identity['exe'] == str(root / 'install/goby' if role == 'server' else PG / 'postgres') and
             identity['startTicks'].isdigit() and type(identity['executableDevice']) is int and type(identity['executableInode']) is int and
             identity['executableInode'] > 0, 'candidate_process_binding')
        identities[role] = identity
    need(identities['server']['pid'] != identities['postgres']['pid'] and identities['server']['uid'] != identities['postgres']['uid'] and
         identities['server']['bootId'] == identities['postgres']['bootId'], 'candidate_process_separation')
    protected = manifest['protectedBaseline']
    protected_names = helper.protected_units(provision_input)
    need(set(protected) == {'units', 'bootId'} and protected['bootId'] == identities['server']['bootId'] and
         set(protected['units']) == set(protected_names) and len(protected_names) == 5 and
         all(set(row) == UNIT_FIELDS and row['Id'] == name for name, row in protected['units'].items()), 'protected_baseline_binding')
    need(manifest['listener']['pid'] == identities['server']['pid'] and manifest['listener']['address'] == '127.0.0.1' and
         manifest['listener']['port'] == manifest['ports']['http'] and re.fullmatch(r'[0-9]+', manifest['listener']['socketInode']), 'candidate_listener_binding')
    expected_hidden = list(helper.inaccessible_paths(provision_input))
    need(str(OLD_CANDIDATE) in expected_hidden and manifest['inaccessiblePaths'] == expected_hidden and
         manifest['loadedInaccessiblePaths'] == {role: sorted(expected_hidden) for role in ('server', 'postgres')}, 'candidate_hiding_binding')
    return root


def parse_environment(raw):
    need(len(raw) <= 1 << 20, 'environment_limit')
    result = {}
    for row in raw.split(b'\0'):
        if not row:
            continue
        need(b'=' in row, 'environment_shape')
        key, value = row.split(b'=', 1)
        need(key not in result, 'duplicate_environment_key')
        result[key] = value
    return result


def validate_environment(runtime_raw, process_raw):
    lines = runtime_raw.splitlines()
    need(all(b'=' in line for line in lines), 'runtime_environment_shape')
    declared = parse_environment(b'\0'.join(lines))
    actual = parse_environment(process_raw)
    need(b'GOBY_WEB_DIR' not in declared and b'GOBY_WEB_DIR' not in actual and
         all(actual.get(key) == value for key, value in declared.items()) and
         not {key for key in actual if key.startswith(b'GOBY_')} - set(declared) and
         all(declared.get(key.encode()) == value.encode() for key, value in BACKUP_ENV.items()) and
         declared.get(b'GOBY_BACKUP_MIN_FREE_BYTES') == b'67108864', 'runtime_environment_changed')
    return True


def validate_context(runtime_context, caller_deadline, runtime_budgets):
    need(type(runtime_context) is bool, 'runtime_context_type')
    if runtime_context:
        need(runtime_budgets == RUNTIME_BUDGETS and all(type(number) is int for number in runtime_budgets.values()) and
             callable(caller_deadline), 'runtime_context_budget')
    else:
        need(runtime_budgets is None and caller_deadline is None, 'initial_context_budget')


def response_headers(pairs):
    result = {}
    for name, value in pairs:
        name = name.lower()
        need(name not in result and '\r' not in value and '\n' not in value, 'response_header_shape')
        result[name] = value
    need('set-cookie' not in result and 'location' not in result and result.get('content-encoding', 'identity').lower() == 'identity', 'response_cookie_redirect_or_encoding')
    need(not ('content-length' in result and 'transfer-encoding' in result) and result.get('transfer-encoding', 'chunked').lower() == 'chunked', 'response_framing')
    if 'content-length' in result:
        need(re.fullmatch(r'0|[1-9][0-9]*', result['content-length']), 'response_length_shape')
    return result


class CandidateInspection:
    def __init__(self, value, input_pin, source_pin, provision=None, *, runtime_context=False, caller_deadline=None, runtime_budgets=None):
        need(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION') and sys.flags.isolated and sys.flags.dont_write_bytecode,
             'authorized_remote_python_required')
        self.started = time.monotonic()
        self.value, self.input_pin, self.source_pin = validate_input(value), descriptor(input_pin), descriptor(source_pin)
        need(Path(input_pin['path']).is_relative_to(E3) and Path(source_pin['path']).is_relative_to(E3) and
             Path(source_pin['path']).name == 'inspect-audited-candidate.py' and read_descriptor(input_pin) == value,
             'inspection_input_source_paths')
        read_checked(source_pin['path'], source_pin['sha256'], 1 << 20)
        if value['version'] == 3:
            need(Path(source_pin['path']) == SQL_CORRECTED_HELPER, 'sql_correction_helper_path')
            prior = read_descriptor(PRIOR_SQL_FAILURE)
            old_input = read_descriptor(descriptor(prior.get('input')))
            validate_sql_correction(value, prior, old_input, read_descriptor(SQL_FAILURE_DIAGNOSTIC))
            validate_metadata_correction(old_input, read_descriptor(PRIOR_METADATA_FAILURE), read_descriptor(PRIOR_INPUT))
            read_checked(METADATA_CORRECTION_REVIEW['path'], METADATA_CORRECTION_REVIEW['sha256'], 1 << 20)
        elif value['version'] == 2:
            need(Path(source_pin['path']) == REVIEWED_HELPER, 'metadata_correction_helper_path')
            validate_metadata_correction(value, read_descriptor(PRIOR_METADATA_FAILURE), read_descriptor(PRIOR_INPUT))
            read_checked(METADATA_CORRECTION_REVIEW['path'], METADATA_CORRECTION_REVIEW['sha256'], 1 << 20)
        else:
            need(source_pin == PRIOR_HELPER, 'original_inspection_helper_identity')
        helper_pin = value['provisionHelper']
        raw = read_checked(helper_pin['path'], helper_pin['sha256'], 1 << 20)
        spec = importlib.util.spec_from_loader('embedded_inspection_provision', loader=None)
        self.helper = importlib.util.module_from_spec(spec)
        self.helper.__file__ = helper_pin['path']
        exec(compile(raw, helper_pin['path'], 'exec'), self.helper.__dict__)
        self.provision_input = read_descriptor(value['provisionInput'])
        self.manifest = read_descriptor(value['provisionManifest'])
        self.root = validate_manifest(value, self.manifest, self.provision_input, self.helper)
        self.output = Path(value['output']); self.private = None
        self.pgport, self.httpport = self.manifest['ports']['postgres'], self.manifest['ports']['http']
        self.socket = self.root / 'postgres/socket'
        self.units = {role: self.manifest['processes'][role]['Id'] for role in ('server', 'postgres')}
        self.argv = {'server': [str(self.root / 'install/goby')], 'postgres': [str(PG / 'postgres'), '-D', str(self.root / 'postgres/data'), '-c', 'config_file=' + str(self.root / 'postgres/server.conf')]}
        self.provision = provision
        if provision is not None:
            need(provision.root == self.root and provision.units == self.units and provision.argv == self.argv and
                 provision.value['ports'] == self.manifest['ports'], 'consumer_provision_context_mismatch')
        validate_context(runtime_context, caller_deadline, runtime_budgets)
        self.runtime_context, self.caller_deadline = runtime_context, caller_deadline
        self.limits = RUNTIME_BUDGETS if runtime_context else BUDGETS
        self.sql_sessions = self.public_requests = self.unit_observations = self.http_body_bytes = 0
        self.sql_records, self.sql_raw, self.http_records = [], [], []
        self.command_records, self.command_failure_raw = [], []
        self.dbus_metadata_calls = 0
        self.typed_unit_evidence = []
        self.active_command = None

    def remaining(self):
        if self.caller_deadline is not None:
            self.caller_deadline()
        left = self.limits['maximumSeconds'] - (time.monotonic() - self.started)
        need(left > 0, 'inspection_deadline')
        return left

    def save(self, name, value, raw=False):
        if self.private is None:
            return None
        need(re.fullmatch(r'[a-z0-9][a-z0-9.-]{0,100}', name), 'evidence_name')
        data = value if raw else encoded(value)
        path = self.private / name
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
            stream.write(data); stream.flush(); os.fsync(stream.fileno())
        return {'path': str(path), 'sha256': sha(data), 'bytes': len(data)}

    def command(self, argv, payload=None, maximum=65536, timeout=10, environment=None):
        record = {'ordinal': len(self.command_records) + 1, 'argvSha256': sha(encoded(argv)),
                  'processCreated': False, 'pid': None, 'processGroup': None, 'exitCode': None,
                  'frontendGroupClosed': True, 'failure': None, 'cleanupFailure': None, 'retentionErrors': []}
        self.command_records.append(record)
        process = selector = failure = None
        stdout, stderr = bytearray(), bytearray()
        ended = {'stdout': False, 'stderr': False}
        pending = memoryview(payload or b'')
        try:
            deadline = time.monotonic() + min(timeout, self.remaining())
            process = subprocess.Popen(argv, stdin=subprocess.PIPE if payload is not None else subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=ENV if environment is None else environment, start_new_session=True)
            self.active_command = process
            record.update(processCreated=True, pid=process.pid, processGroup=process.pid, frontendGroupClosed=False)
            selector = selectors.DefaultSelector()
            for stream, target in ((process.stdout, stdout), (process.stderr, stderr)):
                os.set_blocking(stream.fileno(), False)
                selector.register(stream, selectors.EVENT_READ, target)
            if process.stdin is not None:
                os.set_blocking(process.stdin.fileno(), False)
                if pending:
                    selector.register(process.stdin, selectors.EVENT_WRITE, None)
                else:
                    process.stdin.close()
            while selector.get_map():
                left = min(deadline - time.monotonic(), self.remaining())
                need(left > 0, 'read_command_timeout')
                for key, events in selector.select(min(0.2, left)):
                    if events & selectors.EVENT_WRITE:
                        try:
                            sent = os.write(key.fd, pending[:65536])
                        except BlockingIOError:
                            continue
                        pending = pending[sent:]
                        if not pending:
                            selector.unregister(key.fileobj); process.stdin.close()
                        continue
                    try:
                        limit = maximum if key.data is stdout else 65536
                        block = os.read(key.fd, min(65536, limit - len(key.data) + 1))
                    except BlockingIOError:
                        continue
                    if not block:
                        ended['stdout' if key.data is stdout else 'stderr'] = True
                        selector.unregister(key.fileobj)
                    else:
                        key.data.extend(block)
                        need(len(stdout) <= maximum and len(stderr) <= 65536, 'read_command_output_limit')
            process.wait(timeout=max(0.01, min(deadline - time.monotonic(), self.remaining())))
            if self.helper.group_alive(process.pid):
                raise InspectionError('read_command_descendant')
            need(process.returncode == 0 and not stderr and len(stdout) <= maximum, 'read_command_failed')
        except BaseException as error:
            failure = error
        finally:
            if process is not None:
                try:
                    if process.poll() is None or self.helper.group_alive(process.pid):
                        self.stop_command(process)
                    record['exitCode'] = process.poll()
                    record['frontendGroupClosed'] = record['exitCode'] is not None and not self.helper.group_alive(process.pid)
                    need(record['frontendGroupClosed'], 'read_command_group_unclosed')
                except BaseException as error:
                    record['exitCode'] = process.returncode
                    record['cleanupFailure'] = {'type': type(error).__name__, 'code': 'read_command_cleanup_failed'}
                    if failure is None:
                        failure = error
                if record['frontendGroupClosed']:
                    self.active_command = None
            closables = ([selector] if selector is not None else []) + ([process.stdin, process.stdout, process.stderr] if process is not None else [])
            for stream in closables:
                if stream is not None:
                    try:
                        stream.close()
                    except BaseException as error:
                        record['cleanupFailure'] = {'type': type(error).__name__, 'code': 'read_command_stream_close_failed'}
                        if failure is None:
                            failure = error
            record['stdinComplete'] = process is not None and not pending
            for name, raw in (('stdout', stdout), ('stderr', stderr)):
                record[name] = {'sha256': sha(raw), 'bytes': len(raw), 'complete': ended[name]}
            if failure is not None:
                record['failure'] = {'type': type(failure).__name__,
                    'code': str(failure) if type(failure) is InspectionError else 'read_command_failed'}
                self.retain_command_failure(record, bytes(stdout), bytes(stderr))
        if failure is not None:
            raise failure
        return bytes(stdout)

    def retain_command_failure(self, record, stdout, stderr):
        """Keep bounded raw evidence private, even when a caller's sink is unavailable."""
        self.command_failure_raw.append({'ordinal': record['ordinal'], 'stdout': stdout, 'stderr': stderr})
        if self.private is None:
            return
        prefix = 'command-%04d-' % record['ordinal']
        for name, raw in (('stdout', stdout), ('stderr', stderr)):
            try:
                pin = self.save(prefix + name + '.raw', raw, raw=True)
                record[name]['path'] = pin['path']
            except BaseException as error:
                record['retentionErrors'].append({'part': name, 'type': type(error).__name__, 'code': 'command_evidence_unavailable'})
        try:
            self.save(prefix + 'result.json', record)
        except BaseException as error:
            record['retentionErrors'].append({'part': 'result', 'type': type(error).__name__, 'code': 'command_evidence_unavailable'})

    def stop_command(self, process):
        for sig in (signal.SIGTERM, signal.SIGKILL):
            if not self.helper.group_alive(process.pid):
                break
            try:
                os.killpg(process.pid, sig)
            except ProcessLookupError:
                pass
            until = time.monotonic() + 4
            while self.helper.group_alive(process.pid) and time.monotonic() < until:
                process.poll(); time.sleep(0.05)
        process.wait(timeout=1)
        need(not self.helper.group_alive(process.pid), 'read_command_group_unclosed')

    def process(self, role, extra=()):
        self.remaining()
        need(role in self.units and all(re.fullmatch(r'[A-Za-z][A-Za-z0-9]*', key) for key in extra), 'unit_observation_scope')
        need(self.unit_observations < 4096, 'unit_observation_budget')
        fields = UNIT_FIELDS | set(extra)
        self.unit_observations += 1
        raw = self.command(['/usr/bin/systemctl', 'show', self.units[role], '--property=' + ','.join(sorted(fields))])
        unit = dict(line.split('=', 1) for line in raw.decode().splitlines() if '=' in line)
        unit = self.complete_unit_metadata(role, fields, unit)
        need(set(unit) == fields and all(unit[key] == value for key, value in self.manifest['processes'][role].items()), 'unit_process_changed')
        expected = self.manifest['serverIdentity' if role == 'server' else 'postgresIdentity']
        path = Path('/proc') / str(expected['pid'])
        before = (path / 'stat').read_text().rsplit(') ', 1)[1].split()
        info = (path / 'exe').stat()
        observed = {'pid': expected['pid'], 'startTicks': before[19], 'bootId': Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            'exe': os.readlink(path / 'exe'), 'uid': path.stat().st_uid, 'cgroup': unit['ControlGroup'],
            'executableDevice': info.st_dev, 'executableInode': info.st_ino, 'invocationId': unit['InvocationID']}
        need(observed == expected and before[0] not in ('Z', 'X') and observed['uid'] == pwd.getpwnam('goby' if role == 'server' else 'postgres').pw_uid and
             (path / 'cgroup').read_text() == '0::' + unit['ControlGroup'] + '\n' and
             (path / 'cmdline').read_bytes().split(b'\0') == [value.encode() for value in self.argv[role]] + [b''] and
             (path / 'stat').read_text().rsplit(') ', 1)[1].split()[19] == before[19], 'process_identity_changed')
        need(os.readlink(path / 'ns/net') == os.readlink('/proc/self/ns/net'), 'reader_network_namespace_differs')
        return observed, unit

    def complete_unit_metadata(self, role, fields, unit):
        missing = fields - set(unit)
        if not missing:
            if role == 'postgres' and 'EnvironmentFiles' in fields:
                need(unit['EnvironmentFiles'] == '', 'postgres_environment_files_nonempty')
            return unit
        need(role == 'postgres' and missing == {'EnvironmentFiles'} and set(unit) == fields - {'EnvironmentFiles'} and
             all(unit.get(key) == value for key, value in self.manifest['processes']['postgres'].items()), 'unit_process_changed')
        name = self.units['postgres']
        object_path = postgres_unit_object(name)
        need(self.dbus_metadata_calls + 3 <= 128, 'dbus_metadata_budget')
        self.dbus_metadata_calls += 1
        resolved = self.command(['/usr/bin/busctl', '--system', '--no-pager', 'call', 'org.freedesktop.systemd1',
            '/org/freedesktop/systemd1', 'org.freedesktop.systemd1.Manager', 'GetUnit', 's', name])
        number = len(self.typed_unit_evidence) + 1
        self.save('postgres-envfiles-%02d-object.raw' % number, resolved, raw=True)
        need(resolved.decode('ascii').strip() == 'o ' + json.dumps(object_path), 'dbus_foreign_or_untyped_unit_object')
        self.dbus_metadata_calls += 1
        unit_id = self.command(['/usr/bin/busctl', '--system', '--no-pager', 'get-property', 'org.freedesktop.systemd1',
            object_path, 'org.freedesktop.systemd1.Unit', 'Id'])
        self.save('postgres-envfiles-%02d-id.raw' % number, unit_id, raw=True)
        need(unit_id.decode('ascii').strip() == 's ' + json.dumps(name), 'dbus_unit_id_changed')
        self.dbus_metadata_calls += 1
        raw = self.command(['/usr/bin/busctl', '--system', '--no-pager', 'get-property', 'org.freedesktop.systemd1',
            object_path, 'org.freedesktop.systemd1.Service', 'EnvironmentFiles'])
        self.save('postgres-envfiles-%02d-property.raw' % number, raw, raw=True)
        need(raw.strip() == b'a(sb) 0', 'postgres_environment_files_not_typed_empty')
        # Recheck the exact process-bearing properties after the typed lookup.
        need(self.unit_observations < 4096, 'unit_observation_budget')
        self.unit_observations += 1
        observed = self.command(['/usr/bin/systemctl', 'show', name, '--property=' + ','.join(sorted(UNIT_FIELDS))])
        fresh = dict(line.split('=', 1) for line in observed.decode().splitlines() if '=' in line)
        need(fresh == self.manifest['processes']['postgres'], 'dbus_unit_process_changed')
        proof = {'unit': name, 'objectPath': object_path, 'property': 'EnvironmentFiles', 'interface': 'org.freedesktop.systemd1.Service',
                 'signature': 'a(sb)', 'elementCount': 0, 'typedEmptyVerified': True, 'invocationId': fresh['InvocationID'],
                 'getUnitStdoutSha256': sha(resolved), 'unitIdStdoutSha256': sha(unit_id), 'propertyStdoutSha256': sha(raw)}
        self.typed_unit_evidence.append(proof)
        self.save('postgres-envfiles-%02d-proof.json' % number, proof)
        return {**unit, 'EnvironmentFiles': ''}

    def descriptor_links(self, role):
        root = Path('/proc') / str(self.manifest['processes'][role]['MainPID'])
        files = list((root / 'fd').iterdir()); need(len(files) <= 4096, 'process_fd_bound')
        result = set()
        for entry in files:
            try:
                result.add(os.readlink(entry))
            except FileNotFoundError:
                pass
        return root, result

    def owned_tcp(self, role, local_port, remote_port=None):
        self.process(role)
        need(type(local_port) is int and 1024 < local_port <= 65535 and (remote_port is None or type(remote_port) is int and remote_port == self.pgport), 'tcp_scope')
        need((remote_port is None and local_port == (self.httpport if role == 'server' else self.pgport)) or
             (remote_port is not None and role == 'server'), 'tcp_role_scope')
        root, links = self.descriptor_links(role)
        raw = (root / 'net/tcp').read_bytes(); need(len(raw) <= 1 << 20, 'tcp_metadata_limit')
        rows = [line.split() for line in raw.decode().splitlines()[1:]]
        matches = [row for row in rows if len(row) > 9 and row[1] == '0100007F:%04X' % local_port and
            row[3] == ('0A' if remote_port is None else '01') and (remote_port is None or row[2] == '0100007F:%04X' % remote_port)]
        need(len(matches) == 1 and 'socket:[' + matches[0][9] + ']' in links, 'socket_not_owned_by_pinned_process')
        self.process(role)
        result = {'pid': int(self.manifest['processes'][role]['MainPID']), 'localPort': local_port, 'remotePort': remote_port, 'socketInode': matches[0][9]}
        if role == 'server' and remote_port is None:
            need(result['socketInode'] == self.manifest['listener']['socketInode'], 'listener_changed_since_provision')
        return result

    def owned_unix(self):
        self.process('postgres')
        path = self.socket / ('.s.PGSQL.' + str(self.pgport))
        info = path.lstat(); pgid = self.manifest['postgresIdentity']['uid']
        need(stat.S_ISSOCK(info.st_mode) and info.st_uid == pgid and self.socket.resolve() == self.socket and
             self.socket.stat().st_uid == pgid and stat.S_IMODE(self.socket.stat().st_mode) == 0o700, 'private_postgres_socket')
        root, links = self.descriptor_links('postgres')
        raw = (root / 'net/unix').read_bytes(); need(len(raw) <= 1 << 20, 'unix_metadata_limit')
        rows = [row.split() for row in raw.decode().splitlines()[1:]]
        matches = [row for row in rows if len(row) == 8 and row[7] == str(path) and int(row[3], 16) & 0x10000]
        need(len(matches) == 1 and 'socket:[' + matches[0][6] + ']' in links, 'unix_socket_not_owned')
        # PostgreSQL periodically touches socket timestamps; they are observations,
        # not permanent listener identity.
        return {'path': str(path), 'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid,
                'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode), 'listenerInode': matches[0][6]}

    def sql_json(self, database, single_select):
        self.remaining()
        query = single_select.strip().removesuffix(';').strip()
        need(database in ('postgres', self.manifest['database'], self.manifest['recoveryDatabase']) and
             query.startswith('SELECT ') and ';' not in query and '\0' not in query and len(query.encode()) <= 65536,
             'single_reviewed_select_required')
        need(self.sql_sessions < self.limits['maximumSqlSessions'], 'sql_budget')
        socket_before = self.owned_unix()
        passfile = self.socket / '.goby-inspection-no-password'
        need(not os.path.lexists(passfile), 'sql_password_file_present')
        lines = (self.root / 'postgres/data/postmaster.pid').read_text().splitlines()
        need(lines[0] == self.manifest['processes']['postgres']['MainPID'] and lines[1] == str(self.root / 'postgres/data') and lines[3] == str(self.pgport), 'postmaster_pidfile_changed')
        self.sql_sessions += 1
        number = self.sql_sessions
        self.save('sql-%02d-intent.json' % number, {'database': database, 'querySha256': sha(query.encode()), 'socket': socket_before})
        identity_query = "SELECT json_build_object('readOnly',current_setting('transaction_read_only'),'backendPid',pg_backend_pid(),'database',current_database(),'systemIdentifier',(pg_control_system()).system_identifier::text)"
        payload = ("BEGIN READ ONLY;\nSET LOCAL statement_timeout='5s';\nSET LOCAL lock_timeout='1s';\n" + identity_query + ';\n' + query + ';\nCOMMIT;\n').encode()
        argv = ['/usr/sbin/runuser', '-u', 'postgres', '--', str(PG / 'psql'), '-X', '--no-password', '-h', str(self.socket),
                '-p', str(self.pgport), '-U', 'postgres', '-d', database, '-v', 'ON_ERROR_STOP=1', '-Atq']
        sql_environment = {**ENV, 'PGOPTIONS': '-c default_transaction_read_only=on -c statement_timeout=5000 -c lock_timeout=1000',
                           'PGCONNECT_TIMEOUT': '3', 'PGPASSFILE': str(passfile), 'PGSERVICEFILE': '/dev/null'}
        raw = self.command(argv, payload, maximum=8 << 20, timeout=12, environment=sql_environment)
        self.sql_raw.append(raw)
        self.save('sql-%02d-stdout.raw' % number, raw, raw=True)
        output = raw.strip().splitlines(); need(len(output) == 2, 'sql_output_shape')
        binding, value = parse(output[0]), parse(output[1])
        need(binding['readOnly'] == 'on' and binding['database'] == database and binding['systemIdentifier'] == self.manifest['clusterSystemIdentifier'] and
             type(binding['backendPid']) is int and binding['backendPid'] > 1, 'sql_cluster_binding')
        until = time.monotonic() + min(3, self.remaining())
        while Path('/proc/' + str(binding['backendPid'])).exists() and time.monotonic() < until:
            time.sleep(0.05)
        need(not Path('/proc/' + str(binding['backendPid'])).exists() and self.owned_unix() == socket_before, 'sql_backend_or_socket_unclosed')
        need(not os.path.lexists(passfile), 'sql_password_file_present')
        result = {'database': database, 'querySha256': sha(query.encode()), 'stdoutSha256': sha(raw), 'backendPid': binding['backendPid'],
                  'readOnly': True, 'frontendGroupClosed': True, 'backendGone': True, 'commitAcknowledged': True}
        self.sql_records.append(result); self.save('sql-%02d-result.json' % number, result)
        return value

    def assert_target_cluster(self):
        self.owned_tcp('postgres', self.pgport)
        value = self.sql_json('postgres', "SELECT json_build_object('dataDirectory',current_setting('data_directory'),'port',current_setting('port'),'systemIdentifier',(pg_control_system()).system_identifier::text,'version',current_setting('server_version_num'))")
        expected = {'dataDirectory': str(self.root / 'postgres/data'), 'port': str(self.pgport),
                    'systemIdentifier': self.manifest['clusterSystemIdentifier'], 'version': str(self.manifest['postgresVersionNum'])}
        need(value == expected and int(value['version']) // 10000 == 17, 'wrong_postgres_cluster')
        return value

    def database_facts(self, slot):
        """Read the provisioned catalog identity; recovery must still be empty."""
        need(slot in ('source', 'recovery'), 'database_slot_invalid')
        name = self.manifest['database'] if slot == 'source' else self.manifest['recoveryDatabase']
        query = "SELECT json_build_object('identity',(SELECT json_build_object('database',d.datname,'databaseOid',d.oid::bigint,'roleOid',r.oid::bigint,'owner',pg_get_userbyid(d.datdba),'superuser',r.rolsuper,'createDb',r.rolcreatedb,'createRole',r.rolcreaterole,'replication',r.rolreplication,'bypassRls',r.rolbypassrls,'inherit',r.rolinherit,'login',r.rolcanlogin,'memberships',(SELECT count(*) FROM pg_auth_members WHERE member=r.oid),'publicDatabasePrivileges',(SELECT count(*) FROM aclexplode(d.datacl) WHERE grantee=0)) FROM pg_database d JOIN pg_roles r ON r.rolname='" + name + "' WHERE d.datname='" + name + "'),'objects',json_build_object('relations',(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace),'functions',(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace),'types',(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace),'schemas',(SELECT json_agg(nspname ORDER BY nspname) FROM pg_namespace WHERE nspname!~'^pg_' AND nspname<>'information_schema'),'schemaOwner',(SELECT pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname='public'),'foreignRelations',(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND pg_get_userbyid(relowner)<>'" + name + "'),'foreignFunctions',(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND pg_get_userbyid(proowner)<>'" + name + "')))"
        value = self.sql_json(name, query)
        expected = self.manifest['databases'][slot]['afterStart']
        need(all(value['identity'][key] == item for key, item in expected['identity'].items()) and
             value['identity']['memberships'] == value['identity']['publicDatabasePrivileges'] == 0, 'database_role_or_owner_changed')
        need(all(value['objects'][key] == item for key, item in expected['objects'].items()) and
             value['objects']['schemaOwner'] in (name, 'pg_database_owner') and
             value['objects']['foreignRelations'] == value['objects']['foreignFunctions'] == 0, 'database_objects_changed')
        if slot == 'recovery':
            need(all(value['objects'][key] == 0 for key in ('relations', 'functions', 'types')) and
                 value['objects']['schemas'] == ['public'], 'recovery_database_not_empty')
        return value

    def deployment_lease(self):
        lock = 4919415424202458201
        database = self.manifest['database']
        query = "SELECT COALESCE(json_agg(json_build_object('backendPid',a.pid,'user',a.usename,'database',a.datname,'application',a.application_name,'clientHost',host(a.client_addr),'clientPort',a.client_port,'backendStart',a.backend_start,'mode',l.mode,'granted',l.granted) ORDER BY a.pid),'[]'::json) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.locktype='advisory' AND l.classid=" + str(lock >> 32) + '::oid AND l.objid=' + str(lock & 0xffffffff) + "::oid AND l.objsubid=1 AND l.database=(SELECT oid FROM pg_database WHERE datname='" + database + "')"
        rows = self.sql_json(database, query)
        need(isinstance(rows, list) and len(rows) == 1, 'deployment_lease_not_single')
        row = rows[0]
        need(row['user'] == row['database'] == database and row['application'] == 'goby' and row['clientHost'] == '127.0.0.1' and
             row['mode'] == 'ExclusiveLock' and row['granted'] is True and type(row['clientPort']) is int, 'deployment_lease_not_candidate_owned')
        row['candidateConnection'] = self.owned_tcp('server', row['clientPort'], self.pgport)
        return row

    def source_state(self):
        value = self.sql_json(self.manifest['database'], "SELECT json_build_object('users',(SELECT count(*) FROM users),'versions',(SELECT json_agg(version ORDER BY version) FROM schema_migrations),'serverId',(SELECT value FROM server_settings WHERE key='server_id'))")
        need(value['users'] == 0 and value['versions'] == list(range(1, 29)) and
             re.fullmatch(r'[0-9a-f]{32}', value['serverId'] or ''), 'source_not_initial_schema28')
        return value

    def protected_baseline(self):
        names = sorted(self.manifest['protectedBaseline']['units'])
        raw = self.command(['/usr/bin/systemctl', 'show', '--no-pager', *names, '--property=' + ','.join(sorted(UNIT_FIELDS))])
        units = {}
        for block in raw.decode().strip().split('\n\n'):
            row = dict(line.split('=', 1) for line in block.splitlines() if '=' in line)
            need(set(row) == UNIT_FIELDS and row['Id'] not in units, 'protected_unit_output')
            units[row['Id']] = row
        value = {'units': units, 'bootId': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}
        need(value == self.manifest['protectedBaseline'], 'protected_baseline_changed')
        return value

    def runtime_configuration(self):
        extras = {'Type', 'User', 'Group', 'Restart', 'ProtectSystem', 'ProtectHome', 'PrivateTmp', 'NoNewPrivileges',
                  'ReadWritePaths', 'InaccessiblePaths', 'ExecStart', 'MemoryMax', 'MemorySwapMax', 'TasksMax',
                  'EnvironmentFiles', 'UnsetEnvironment', 'FragmentPath', 'DropInPaths'}
        units = {}
        for role in ('server', 'postgres'):
            original = self.manifest['units'][role]
            read_checked(original['path'], original['sha256'], 65536)
            identity, observed = self.process(role, extras)
            user = 'goby' if role == 'server' else 'postgres'
            need(all(observed[key] == val for key, val in {'Type': 'exec', 'User': user, 'Group': user, 'Restart': 'no',
                 'ProtectSystem': 'strict', 'ProtectHome': 'yes', 'PrivateTmp': 'yes', 'NoNewPrivileges': 'yes',
                 'ReadWritePaths': str(self.root / ('data' if role == 'server' else 'postgres')),
                 'MemoryMax': str((768 if role == 'server' else 256) << 20), 'MemorySwapMax': '0', 'TasksMax': '128',
                 'FragmentPath': original['path'], 'DropInPaths': '',
                 'EnvironmentFiles': str(self.root / 'private/runtime.env') + ' (ignore_errors=no)' if role == 'server' else '',
                 'UnsetEnvironment': 'GOBY_WEB_DIR' if role == 'server' else ''}.items()), 'loaded_unit_configuration')
            self.helper.verify_inaccessible_paths(observed['InaccessiblePaths'].split(), self.provision_input)
            import shlex
            command = observed['ExecStart']
            need(command.count('argv[]=') == 1 and shlex.split(command.split('argv[]=', 1)[1].split(';', 1)[0]) == self.argv[role], 'loaded_unit_command')
            view = Path('/proc') / str(identity['pid']) / 'root' / str(OLD_CANDIDATE).lstrip('/')
            try:
                mask = view.stat()
                need(stat.S_ISDIR(mask.st_mode) and stat.S_IMODE(mask.st_mode) == 0, 'old_candidate_directory_visible')
                hidden = 'mode000_directory_mask'
            except FileNotFoundError:
                hidden = 'absent_in_unit_view'
            units[role] = {'process': identity, 'isolationProperties': {key: observed[key] for key in sorted(extras)},
                           'oldCandidateDirectory': str(OLD_CANDIDATE), 'oldCandidateVisibility': hidden}
        need(not os.path.lexists(self.root / 'install/admin'), 'external_admin_directory_present')
        runtime_raw = read_checked(self.manifest['runtime']['path'], self.manifest['runtime']['sha256'], 65536)
        with (Path('/proc') / str(self.manifest['serverIdentity']['pid']) / 'environ').open('rb') as stream:
            actual = stream.read((1 << 20) + 1)
        validate_environment(runtime_raw, actual)
        self.process('server')
        raw = read_checked(self.root / 'install/goby', BINARY_SHA, BINARY_BYTES)
        need(len(raw) == BINARY_BYTES, 'installed_binary_size')
        return {'units': units, 'runtimeEnvironmentSha256': sha(runtime_raw), 'runtimeEnvironmentMatches': True,
                'externalAdminAbsent': True, 'webDirectoryOverrideAbsent': True, 'binarySha256': BINARY_SHA,
                'backupProfile': self.manifest['backupProfile']}

    def public_get(self, route, expected, maximum):
        need(not self.runtime_context and self.private is not None, 'initial_http_workflow_only')
        self.remaining()
        need(route in self.http_routes and type(maximum) is int and 0 < maximum <= 1 << 20 and
             self.public_requests < BUDGETS['maximumHttpRequests'], 'public_request_scope_or_budget')
        before = self.owned_tcp('server', self.httpport)
        self.public_requests += 1; number = self.public_requests
        record = {'ordinal': number, 'route': route, 'method': 'GET', 'expectedStatus': expected, 'complete': False,
                  'listenerBefore': before, 'noCredentialsSent': True}
        self.http_records.append(record); self.save('http-%02d-intent.json' % number, record)
        connection = http.client.HTTPConnection('127.0.0.1', self.httpport, timeout=min(4, self.remaining()))
        chunks = bytearray(); headers = None
        try:
            connection.request('GET', route, headers={'Accept': '*/*', 'Accept-Encoding': 'identity', 'Connection': 'close'})
            response = connection.getresponse()
            pairs = response.getheaders(); record['status'] = response.status
            self.save('http-%02d-headers.json' % number, {'status': response.status, 'headers': pairs})
            headers = response_headers(pairs)
            if 'content-length' in headers:
                need(int(headers['content-length']) <= maximum, 'http_body_limit')
            while True:
                self.remaining()
                block = response.read1(min(65536, maximum - len(chunks) + 1))
                if not block:
                    break
                chunks.extend(block); self.http_body_bytes += len(block)
                need(len(chunks) <= maximum and self.http_body_bytes <= BUDGETS['maximumHttpBodyBytes'], 'http_body_limit')
            need(response.status == expected and (response.isclosed() or response.length == 0) and
                 ('content-length' not in headers or int(headers['content-length']) == len(chunks)), 'http_response_incomplete_or_status')
            record.update(complete=True, noCookieIssued=True, noRedirect=True, bytes=len(chunks), bodySha256=sha(chunks))
        finally:
            connection.close()
            self.save('http-%02d-body.bin' % number, bytes(chunks), raw=True)
            self.save('http-%02d-result.json' % number, record)
        need(self.owned_tcp('server', self.httpport) == before, 'public_listener_changed')
        return bytes(chunks), headers, record

    def dashboard_and_public(self, server_id):
        build = read_descriptor(EMBEDDED_MANIFEST)
        need(build['binary'] == {'name': 'goby', 'sha256': BINARY_SHA, 'bytes': BINARY_BYTES} and
             build['target'] == {'os': 'linux', 'arch': 'amd64', 'cgoEnabled': False}, 'embedded_build_identity')
        files = build['administratorAssets']
        assets = {row['name']: row for row in files}
        refs = build['administratorEntryReferences']
        need(len(assets) == len(files) == 57 and build['administratorAssetBytes'] == sum(row['bytes'] for row in files) == 1106130 and
             assets['index.html'] == {'name': 'index.html', 'sha256': INDEX_SHA, 'bytes': 800} and len(refs) == 5, 'embedded_asset_inventory')
        for row in files:
            need(set(row) == {'name', 'sha256', 'bytes'} and (row['name'] == 'index.html' or
                 re.fullmatch(r'assets/[A-Za-z0-9_./-]+', row['name'])) and '..' not in Path(row['name']).parts and
                 type(row['bytes']) is int and 0 < row['bytes'] <= 1 << 20 and re.fullmatch(r'[0-9a-f]{64}', row['sha256']), 'embedded_asset_member')
        routes = {'/healthz', '/readyz', '/emby/System/Info/Public', '/admin/libraries', '/admin/v1/inspection-missing', '/admin/v1/session'}
        self.http_routes = routes | {'/admin/' if name == 'index.html' else '/admin/' + name for name in assets}
        public = []
        for route, status in (('/healthz', 'ok'), ('/readyz', 'ready'), ('/emby/System/Info/Public', None)):
            raw, headers, record = self.public_get(route, 200, 65536)
            need(headers.get('content-type', '').split(';')[0] == 'application/json', 'public_json_content_type')
            value = parse(raw)
            if status:
                need(value == {'Status': status}, 'public_health_not_ready')
            else:
                need(value['Id'] == server_id and value['ProductName'] == 'Goby' and value['StartupWizardCompleted'] is False and
                     value['LocalAddress'] == self.manifest['publicUrl'], 'public_server_identity')
            public.append(dict(record))
        matched = []
        for name in sorted(assets):
            row = assets[name]
            raw, headers, unused = self.public_get('/admin/' if name == 'index.html' else '/admin/' + name, 200, row['bytes'])
            need(len(raw) == row['bytes'] and sha(raw) == row['sha256'] and headers.get('content-security-policy') == CSP and
                 headers.get('cache-control') == ('no-store' if name == 'index.html' else 'public, max-age=31536000, immutable'), 'embedded_served_asset_changed')
            matched.append(dict(row))
        for reference in refs:
            need(reference['name'] in assets and reference['url'] == '/admin/' + reference['name'] and
                 all(reference[key] == assets[reference['name']][key] for key in ('bytes', 'sha256')), 'embedded_entry_reference_changed')
        raw, headers, unused = self.public_get('/admin/libraries', 200, 800)
        need(len(raw) == 800 and sha(raw) == INDEX_SHA and headers.get('cache-control') == 'no-store' and
             headers.get('content-security-policy') == CSP, 'spa_asset_selection_changed')
        for route, status, code in (('/admin/v1/inspection-missing', 404, 'not_found'), ('/admin/v1/session', 401, 'authentication_required')):
            raw, headers, unused = self.public_get(route, status, 65536)
            value = parse(raw)
            need(headers.get('content-type', '').split(';')[0] == 'application/json' and headers.get('cache-control') == 'no-store' and
                 value['Error']['Code'] == code and re.fullmatch(r'[0-9a-f]{32}', value.get('RequestId', '')), 'administrator_api_separation')
        return public, {'assetCount': 57, 'assetBytes': 1106130, 'allAssetsMatched': True, 'assets': matched,
                        'indexSha256': INDEX_SHA, 'indexBytes': 800, 'entryReferencesMatched': 5, 'entryReferences': refs,
                        'spaMatched': True, 'administratorApiSeparation': True, 'missingCookieRejected': True,
                        'buildManifest': EMBEDDED_MANIFEST, 'directOrigin': self.manifest['directUrl'],
                        'reservedPublicOrigin': self.manifest['publicUrl'], 'reservedPublicOriginRequested': False}

    def close(self):
        if self.active_command is not None:
            self.stop_command(self.active_command)
            self.active_command = None

    def run_initial(self):
        need(not self.runtime_context and self.private is None and not os.path.lexists(self.output), 'initial_inspection_consumed')
        self.output.mkdir(mode=0o700)
        self.private = self.output / 'private'; self.private.mkdir(mode=0o700)
        result = {'kind': 'audited-candidate-runtime-inspection', 'version': 3, 'status': 'inspection_failed', 'stage': 'runtime_configuration',
                  'input': self.input_pin, 'helper': self.source_pin, 'provisionManifest': self.value['provisionManifest'],
                  'provisionInput': self.value['provisionInput'], 'provisionHelper': self.value['provisionHelper'],
                  'sourceEvidence': self.manifest['productEvidence'], 'binary': self.manifest['binary'],
                  'bootstrapPerformed': False, 'restorePerformed': False, 'serviceChangesPerformed': False, 'databaseWrites': 0,
                  'candidateAdmissionComplete': False, 'clientAcceptance': False, 'failure': None}
        if self.value['version'] >= 2:
            result['priorMetadataFailure'] = self.value['priorMetadataFailure']
            result['metadataCorrectionReview'] = METADATA_CORRECTION_REVIEW
        if self.value['version'] == 3:
            result.update(priorSqlFailure=self.value['priorSqlFailure'], sqlFailureDiagnostic=self.value['sqlFailureDiagnostic'],
                          sqlCorrection=dict(SQL_CORRECTION))
        self.save('inspection-started.json', {'input': self.input_pin, 'helper': self.source_pin, 'budgets': BUDGETS})
        try:
            result['protectedBefore'] = self.protected_baseline()
            result['configuration'] = self.runtime_configuration()
            result['stage'] = 'database_identity_and_lease'
            cluster = self.assert_target_cluster()
            databases = {slot: self.database_facts(slot) for slot in ('source', 'recovery')}
            state = self.source_state(); lease = self.deployment_lease()
            result['database'] = {'cluster': cluster, 'slots': databases, 'sourceSchemaVersion': 28, 'sourceMigrationCount': 28,
                                  'sourceUsers': 0, 'recoveryTargetEmpty': True, 'allSqlTransactionsReadOnly': True, 'deploymentLease': lease}
            result['stage'] = 'embedded_assets_and_public_contract'
            result['publicResponses'], result['dashboard'] = self.dashboard_and_public(state['serverId'])
            result['stage'] = 'readonly_state_and_process_closeout'
            need(self.source_state() == state and self.deployment_lease() == lease and
                 self.database_facts('recovery') == databases['recovery'] and self.database_facts('source') == databases['source'], 'state_or_lease_changed')
            result['processes'] = {role: self.process(role)[0] for role in ('server', 'postgres')}
            result['httpListener'] = self.owned_tcp('server', self.httpport)
            result['protectedAfter'] = self.protected_baseline()
            need(read_descriptor(self.value['provisionManifest']) == self.manifest and len(self.sql_records) == self.sql_sessions and
                 all(row['complete'] for row in self.http_records) and self.public_requests == 63, 'inspection_evidence_incomplete')
            result['status'], result['stage'] = 'ready_pending_seed', 'complete'
        except BaseException as error:
            result['failure'] = {'type': type(error).__name__, 'code': str(error) if isinstance(error, InspectionError) else 'inspection_operation_failed'}
        finally:
            try:
                self.close()
            except BaseException as error:
                result['status'] = 'inspection_failed'
                result['readCommandCloseFailure'] = {'type': type(error).__name__, 'code': 'inspection_read_command_unclosed'}
                if result['failure'] is None:
                    result['failure'] = result['readCommandCloseFailure']
            result['capturedAt'] = datetime.now(timezone.utc).isoformat()
            result['counters'] = {'readOnlySqlSessions': self.sql_sessions, 'publicHttpRequests': self.public_requests,
                                  'httpBodyBytes': self.http_body_bytes, 'unitMetadataCalls': self.unit_observations}
            result['sqlReadResults'] = self.sql_records
            result['commandResults'] = self.command_records
            result['httpResponses'] = self.http_records
            result['typedUnitEvidence'] = self.typed_unit_evidence
            result['dbusMetadataCalls'] = self.dbus_metadata_calls
            result['readProcessesClosed'] = self.active_command is None and len(self.sql_records) == self.sql_sessions
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('input', 'input-sha256', 'source-sha256'):
        parser.add_argument('--' + name, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    source_pin = {'path': str(Path(__file__).absolute()), 'sha256': args.source_sha256}
    input_pin = {'path': args.input, 'sha256': args.input_sha256}
    inspector = None
    def interrupted(unused_number, unused_frame):
        raise InspectionError('inspection_interrupted')
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(number, interrupted)
    signal.alarm(BUDGETS['maximumSeconds'])
    try:
        inspector = CandidateInspection(read_descriptor(input_pin), input_pin, source_pin)
        result = inspector.run_initial()
        signal.alarm(0)
        path = inspector.output / 'report.json'
        raw = encoded(result)
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        for directory in (inspector.private, inspector.output, inspector.output.parent):
            fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        print(json.dumps({'status': result['status'], 'report': {'path': str(path), 'sha256': sha(raw)}, 'failure': result['failure'],
                          'counters': result['counters'], 'candidateAdmissionComplete': False, 'clientAcceptance': False}), flush=True)
        return 0 if result['status'] == 'ready_pending_seed' else 2
    except BaseException as error:
        if inspector is not None:
            try:
                inspector.close()
            except BaseException:
                pass
        print(json.dumps({'status': 'inspection_receipt_unavailable' if inspector is not None and inspector.private is not None else 'inspection_admission_failed',
                          'type': type(error).__name__, 'code': str(error) if isinstance(error, InspectionError) else 'inspection_operation_failed',
                          'output': str(inspector.output) if inspector is not None else None,
                          'serviceChangesPerformed': False, 'candidateAdmissionComplete': False}), flush=True)
        return 2
    finally:
        signal.alarm(0)


if __name__ == '__main__':
    raise SystemExit(main())
