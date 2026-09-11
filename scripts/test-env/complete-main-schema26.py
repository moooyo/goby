#!/usr/bin/env python3
"""Close the one pinned schema26 post-start evidence gap without redeployment.

The old three operation trees remain immutable. This script only reads their
evidence and the already running process, then creates one new native smoke
session and revokes it. It never invokes migration, restore, publication, or
systemd mutations. Importing this module performs no external work.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import stat
import sys
import types

sys.dont_write_bytecode = True
WORK = Path('/opt/goby-test/exec-work-m3e')
ROOT = Path('/opt/goby-test/backups/main-schema26-post-start-v1')
MARKER = 'goby-main-schema26-post-start-v1'
FAILED_ROOT = Path('/opt/goby-test/backups/main-schema26-rehearsal-repair-v1')
FAILED_RUN = FAILED_ROOT / 'run-20260911T132850Z-7f17a39a6e1e08ce9639d6a1'
OBSERVATION = WORK / 'main-schema26-post-start-review-02/failure-observation.json'
OBSERVATION_SHA = '003928387a5ee8a656c34410beae89a7159ed50fac374ecc07f37ce730c12365'
UPGRADE_SHA = 'e3040771cba35ec5783b327f7df356adfb66480d2918a19356f4b4743ecad92c'
GUARD_NAME = 'test-complete-main-schema26.py'
PID, TICKS, SOCKET = 688833, '5620918', '1330903'
HASH = re.compile(r'[0-9a-f]{64}')
MAX_FILE = 512 << 20


class Failure(Exception):
    """A bounded, sanitized post-start gate failed."""


def require(value, message):
    if not value:
        raise Failure(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=True, allow_nan=False) + '\n').encode()


def decode(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, 'A private control document repeats a field.')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(Failure('A control number is nonfinite.')))


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def read_control(path, *, source=False):
    require(path.is_absolute() and '..' not in path.parts, 'A private input path is not canonical.')
    for member in reversed((path, *path.parents)):
        require(not stat.S_ISLNK(member.lstat().st_mode), 'A control path contains a symbolic link.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and before.st_nlink == 1 and
            stat.S_IMODE(before.st_mode) in ((0o600, 0o644) if source else (0o600,)) and before.st_size <= 16 << 20,
            'A control file has unexpected ownership, permissions, links, or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as handle:
        require(identity(os.fstat(handle.fileno())) == identity(before), 'A control file changed before reading.')
        raw = handle.read((16 << 20) + 1)
        require(identity(os.fstat(handle.fileno())) == identity(before) and len(raw) == before.st_size,
                'A control file changed during reading.')
    require(identity(path.lstat()) == identity(before), 'A control file was replaced after reading.')
    return raw


def load_inputs(args):
    require(args.mode in ('preflight', 'complete'), 'Only read-only preflight or one-shot completion is supported.')
    for field in ('script_sha256', 'upgrade_source_sha256', 'upgrade_arguments_sha256', 'guard_report_sha256'):
        require(HASH.fullmatch(getattr(args, field, '') or ''), 'Every operator input requires an explicit digest.')
    for path in (args.upgrade_source, args.upgrade_arguments, args.guard_report):
        require(path.is_absolute() and path.is_relative_to(WORK) and '..' not in path.parts,
                'An input escaped the private workspace.')
    own_path = Path(__file__).absolute()
    require(sha(read_control(own_path, source=True)) == args.script_sha256 and args.upgrade_source_sha256 == UPGRADE_SHA and
            args.upgrade_source.name == 'upgrade-main-schema26.py', 'The completion or frozen upgrade source changed.')
    raw = read_control(args.upgrade_source, source=True)
    require(sha(raw) == UPGRADE_SHA, 'The frozen tool04 implementation changed.')
    guards_raw = read_control(args.guard_report)
    require(sha(guards_raw) == args.guard_report_sha256, 'The independent completion guard report changed.')
    guards = decode(guards_raw)
    require(guards.get('suite') == 'main-schema26-post-start-guards' and guards.get('status') == 'passed' and
            type(guards.get('tests')) is int and guards['tests'] >= 6 and
            all(type(guards.get(key)) is int and guards[key] == 0 for key in ('errors', 'failures', 'skips')) and
            guards.get('operator_sha256') == args.script_sha256 and
            guards.get('guard_sha256') == sha(read_control(own_path.with_name(GUARD_NAME), source=True)) and
            guards.get('fixtures') == 'synthetic-memory-only', 'These exact completion bytes have no passing independent guards.')
    module = types.ModuleType('frozen_main_schema26_tool04')
    module.__file__ = str(args.upgrade_source)
    exec(compile(raw, str(args.upgrade_source), 'exec'), module.__dict__)
    arguments_raw = read_control(args.upgrade_arguments)
    require(sha(arguments_raw) == args.upgrade_arguments_sha256, 'The original upgrade arguments changed.')
    arguments = decode(arguments_raw)
    require(set(arguments) == {'schema', 'version', 'argv'} and arguments['schema'] == 'goby-main-schema26-upgrade-arguments' and
            type(arguments['version']) is int and arguments['version'] == 1 and isinstance(arguments['argv'], list) and
            20 <= len(arguments['argv']) <= 160 and all(isinstance(value, str) and 0 < len(value) <= 8192 for value in arguments['argv']),
            'The retained tool04 argument vector has an unexpected shape.')
    source_args = module.parser().parse_args(arguments['argv'])
    module.validate_arguments(source_args)
    require(source_args.mode == 'rehearsal-repair' and source_args.script_sha256 == UPGRADE_SHA,
            'Only the original fixed third-root deployment may be completed.')
    op, published = module.load_published_operator(source_args)
    return module, op, source_args, published


def failed_inventory(op):
    result, total = {}, 0
    for path in [FAILED_ROOT, *sorted(FAILED_ROOT.rglob('*'))]:
        before = op.path_info(path)
        name = '.' if path == FAILED_ROOT else op.safe_relative(path.relative_to(FAILED_ROOT).as_posix())
        value = {'device': before.st_dev, 'inode': before.st_ino, 'uid': before.st_uid, 'gid': before.st_gid,
                 'mode': stat.S_IMODE(before.st_mode), 'nlink': before.st_nlink,
                 'mtime_ns': before.st_mtime_ns, 'ctime_ns': before.st_ctime_ns}
        require(before.st_uid == before.st_gid == 0 and len(result) < 10000, 'The retained failed tree has unsafe ownership or size.')
        if stat.S_ISDIR(before.st_mode):
            op.directory(path)
            value['type'] = 'directory'
        else:
            raw = op.read_file(path, limit=MAX_FILE)
            total += len(raw)
            require(total <= 1 << 30 and identity(op.path_info(path)) == identity(before), 'The retained failed tree changed during reading.')
            value.update(type='file', bytes=len(raw), sha256=sha(raw))
        result[name] = value
    return result


def retained_evidence(module, op, source_args, inputs):
    require(module.LOCK_HELD and module.ROOT == FAILED_ROOT, 'Completion requires the existing deployment lock and tool04 read scope.')
    raw = op.read_file(OBSERVATION)
    require(sha(raw) == OBSERVATION_SHA, 'The independently observed post-start boundary changed.')
    observed = decode(raw)
    require(observed.get('root') == str(FAILED_ROOT) and observed.get('run') == str(FAILED_RUN) and
            observed.get('smoke_artifacts_absent') is True and observed.get('private_state_preserved') is True and
            observed.get('post_start_all_table_equality_claimed') is False and
            observed.get('startup_plan_sha256') == source_args.startup_plan_sha256,
            'The reviewed failure is not the sole post-start evidence gap.')
    require(failed_inventory(op) == observed['inventory'], 'The full original failed deployment changed after review.')
    require(decode(op.read_file(FAILED_ROOT / 'OWNER.json')) ==
            {'marker': module.MARKER, 'version': 1, 'path': str(FAILED_ROOT), 'run_id': FAILED_RUN.name} and
            decode(op.read_file(FAILED_RUN / 'OWNER.json')) == {'marker': module.MARKER, 'version': 1, 'run_id': FAILED_RUN.name},
            'The original deployment owner differs.')
    old_inputs_raw = op.read_file(FAILED_RUN / 'inputs.json')
    require(decode(old_inputs_raw) == inputs, 'The current accepted inputs differ from the original deployment.')
    actions = ['repair-rehearsal', 'save-materials', 'baseline-dump', 'rehearsal-role', 'rehearsal-database', 'rehearsal-mark',
               'rehearsal-restore', 'rehearsal-migrate', 'rehearsal-close', 'rehearsal-drop-database', 'rehearsal-drop-role', 'main-migrate']
    actions += ['publish-file'] * (len(inputs['asset_members']) + 1) + ['start']
    require(len(actions) == 71 and observed.get('intent_actions') == actions, 'The old run has another mutation or smoke history.')
    previous = None
    for number, action in enumerate(actions, 1):
        raw = op.read_file(FAILED_RUN / f'intent-{number:04d}.json')
        value = decode(raw)
        require(value.get('marker') == module.MARKER and value.get('run_id') == FAILED_RUN.name and value.get('sequence') == number and
                value.get('action') == action and value.get('previous_sha256') == previous and value.get('inputs_sha256') == sha(old_inputs_raw),
                'The completed deployment intent chain changed.')
        previous = sha(raw)
    terminal = decode(op.read_file(FAILED_RUN / 'terminal.json'))
    require(terminal == observed.get('terminal') and terminal.get('status') == 'failed' and
            terminal.get('last_completed_phase') == 'start-requested' and terminal.get('last_intent_sha256') == previous and
            terminal.get('reason') == 'The candidate process has an unexpected effective identity.' and
            terminal.get('automatic_recovery_executed') is False and terminal.get('main_database_restored') is False and
            terminal.get('old_binary_rollback') is False, 'The failure crossed the permitted post-start boundary.')
    prefix = FAILED_RUN.name + '/'
    require(not any(name.startswith(prefix + 'smoke-') or name == prefix + 'native-smoke.json' or
                    (name.startswith(prefix + 'intent-') and name > prefix + 'intent-0071.json') for name in observed['inventory']),
            'An earlier smoke attempt already exists; it cannot be adopted or repeated.')
    reports = {}
    for name, key in (('baseline', 'baseline_sha256'), ('main-migrated', 'migration_report_sha256'),
                      ('main-after-migration', 'post_migration_report_sha256'), ('rehearsal-disposed', 'rehearsal_disposed_sha256')):
        raw = op.read_file(FAILED_RUN / (name + '.json'))
        require(sha(raw) == observed[key], 'A retained migration or disposal proof changed.')
        reports[name] = decode(raw)
    migrated, after = reports['main-migrated'], reports['main-after-migration']
    for value, mode, status, version in ((reports['baseline'], 'inspect', 'inspected', 25),
                                       (migrated, 'migrate', 'committed', 25), (after, 'inspect', 'inspected', 26)):
        require(value.get('schema') == 'goby-main-schema26-migration' and value.get('mode') == mode and value.get('status') == status and
                value.get('source_schema_version') == version and value.get('target_schema_version') == 26 and
                value.get('repair_rehearsal') is True and value.get('repair_baseline') is False and value.get('rehearsal') is False and
                value.get('run_id') == FAILED_RUN.name and value.get('target') == module.MAIN and
                value.get('source_manifest_sha256') == inputs['source_manifest_sha256'] and
                value.get('tool_manifest_sha256') == inputs['tool_source']['manifest_sha256'] and
                value.get('candidate_sha256') == inputs['candidate']['sha256'] and value.get('helper_sha256') == inputs['helper']['sha256'],
                'A retained report lost its exact primary or accepted-source bindings.')
    require(migrated.get('before_state_sha256') == migrated.get('preserved_state_sha256') == reports['baseline'].get('state_sha256') and
            migrated.get('input_baseline_sha256') == observed['baseline_sha256'] and migrated.get('new_theme_defaults_verified') is True,
            'The committed migration did not preserve the complete original schema25 state.')
    module.compare_snapshots(migrated, after)
    disposed = reports['rehearsal-disposed']
    require(disposed.get('status') == 'disposed' and disposed.get('force_or_backend_termination_used') is False and
            disposed.get('after', {}).get('database') is None and disposed['after'].get('role') is None,
            'The owned rehearsal has no completed disposal proof.')
    before = decode(op.read_file(FAILED_RUN / 'protected-before.json'))
    prior = module.read_rehearsal_failure_evidence(op, source_args, inputs)
    require(before.get('failed_upgrade') == prior['first_failed_tree'] and
            before.get('failed_baseline_repair') == prior['failed_tree'],
            'The original two failure attestations no longer match the preserved deployment state.')
    return {'observation': observed, 'before': before, 'reports': reports, 'inputs_sha256': sha(old_inputs_raw)}


def live_checks(module, op, source_args, inputs, evidence, *, after_smoke=False):
    observed = evidence['observation']
    active = module.exact_service(op, source_args, expected_sha=inputs['candidate']['sha256'], old=False)
    require(active == observed['active_service'] and active['pid'] == PID and active['start_ticks'] == TICKS and
            active['listener']['inode'] == SOCKET, 'The independently observed new process or listener changed.')
    require(module.primary_proof(op, source_args) == observed['primary'], 'The primary PostgreSQL process changed.')
    module.preserved_private(op, evidence['before'], installed_inputs=inputs, allow_log_append=True)
    require(failed_inventory(op) == observed['inventory'], 'The original post-start failure evidence changed.')
    environment = op.database_environment(op.runtime_policy(), module.MAIN['database'], module.MAIN['role'], readonly=True)
    current = decode(module.pg(op, """BEGIN READ ONLY; SELECT json_build_object(
      'database',current_database(),'database_oid',d.oid::bigint,'owner_oid',d.datdba::bigint,'role',current_user,'role_oid',r.oid::bigint,
      'schema',(SELECT max(version) FROM public.schema_migrations),'activity_total',(SELECT count(*) FROM public.activity_entries),
      'theme_owners',(SELECT count(*) FROM public.theme_owner_ids),'reserved_paths',(SELECT count(*) FROM public.theme_reserved_paths),
      'theme_resources',(SELECT count(*) FROM public.item_theme_resources),
      'rehearsal_databases',(SELECT count(*) FROM pg_database WHERE datname ~ '^goby_upgrade26_m3e_[0-9a-f]{24}$'),
      'rehearsal_roles',(SELECT count(*) FROM pg_roles WHERE rolname ~ '^goby_upgrade26_m3e_[0-9a-f]{24}$'),
      'safe',NOT(r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls) AND r.rolcanlogin,
      'memberships',(SELECT count(*) FROM pg_auth_members m WHERE m.member=r.oid OR m.roleid=r.oid))
      FROM pg_database d JOIN pg_roles r ON r.rolname=current_user WHERE d.datname=current_database(); ROLLBACK;""",
      database=module.MAIN['database'], environment=environment))
    expected = {**module.MAIN, 'owner_oid': module.MAIN['role_oid'], 'schema': 26, 'theme_owners': 22, 'reserved_paths': 0,
                'theme_resources': 0, 'rehearsal_databases': 0, 'rehearsal_roles': 0, 'safe': True, 'memberships': 0}
    require({key: value for key, value in current.items() if key != 'activity_total'} == expected and
            type(current.get('activity_total')) is int and (current['activity_total'] >= 15 if after_smoke else current['activity_total'] == 15),
            'The live primary identity, schema26, Theme defaults, activity, or rehearsal population changed.')
    return {'active_service': active, 'database': current, 'startup_check': module.startup_checkpoint(required=True),
            'old_state_preserved_at_migration_commit': True, 'post_start_all_table_equality_claimed': False}


class SmokeJournal:
    def __init__(self, module, op, run, inputs_sha256):
        self.module, self.op, self.run, self.inputs_sha256 = module, op, run, inputs_sha256
        self.sequence, self.previous = 0, None

    def intent(self, action, binding):
        require(self.module.LOCK_HELD and action == ('smoke-login' if self.sequence == 0 else 'smoke-logout') and self.sequence < 2 and
                self.run.parent == ROOT and not os.path.lexists(self.run / 'terminal.json'), 'Smoke intent escaped its independent one-shot sequence.')
        check = self.module.startup_checkpoint(action, required=True)
        require(decode(self.op.read_file(self.run / 'OWNER.json')) == {'marker': MARKER, 'version': 1, 'run_id': self.run.name},
                'The post-start smoke run has another owner.')
        require(sha(self.op.read_file(self.run / 'inputs.json')) == self.inputs_sha256 and
                (self.sequence == 0 or sha(self.op.read_file(self.run / 'intent-0001.json')) == self.previous),
                'The independent smoke input or preceding intent changed.')
        raw = canonical({'marker': MARKER, 'run_id': self.run.name, 'sequence': self.sequence + 1, 'action': action,
            'previous_sha256': self.previous, 'inputs_sha256': self.inputs_sha256,
            'binding': binding, 'startup_plan_check': check, 'new_session_only': True})
        self.op.write_exclusive(self.run / f'intent-{self.sequence + 1:04d}.json', raw)
        self.sequence += 1
        self.previous = sha(raw)
        return self.previous


def complete(args, module, op, source_args, inputs, evidence, observed):
    require(module.LOCK_HELD and not os.path.lexists(ROOT), 'Existing completion evidence is never adopted or retried.')
    module.startup_checkpoint('smoke-login', required=True)
    op.directory(ROOT.parent)
    ROOT.mkdir(mode=0o700)
    run = ROOT / ('run-' + dt.datetime.now(dt.timezone.utc).strftime('%Y%m%dT%H%M%SZ') + '-' + secrets.token_hex(12))
    op.write_exclusive(ROOT / 'OWNER.json', canonical({'marker': MARKER, 'version': 1, 'path': str(ROOT), 'run_id': run.name}))
    run.mkdir(mode=0o700)
    op.write_exclusive(run / 'OWNER.json', canonical({'marker': MARKER, 'version': 1, 'run_id': run.name}))
    binding = {'marker': MARKER, 'source_operator_sha256': UPGRADE_SHA, 'completion_operator_sha256': args.script_sha256,
        'upgrade_arguments_sha256': args.upgrade_arguments_sha256, 'guard_report_sha256': args.guard_report_sha256,
        'observation_sha256': OBSERVATION_SHA, 'failed_run': str(FAILED_RUN), 'upgrade_inputs_sha256': evidence['inputs_sha256']}
    op.write_exclusive(run / 'inputs.json', canonical(binding))
    op.write_exclusive(run / 'preflight.json', canonical(observed))
    op.sync_directory(ROOT.parent)
    op.sync_directory(ROOT)
    journal = SmokeJournal(module, op, run, sha(canonical(binding)))
    try:
        require(live_checks(module, op, source_args, inputs, evidence)['active_service'] == observed['active_service'],
                'The new process changed before its one permitted smoke session.')
        module.native_smoke(op, source_args, inputs, journal, observed['active_service'], evidence['before']['stores']['backups'])
        final = live_checks(module, op, source_args, inputs, evidence, after_smoke=True)
        smoke = decode(op.read_file(run / 'native-smoke.json'))
        require(smoke.get('logout_verified') is True and smoke.get('new_session_only') is True and
                smoke.get('cleanup_checkpoint_error_type') is None and journal.sequence == 2,
                'The newly issued session has no complete normal logout and 401 proof.')
        terminal = {**binding, 'status': 'passed', 'schema_version': 26, 'binary_sha256': inputs['candidate']['sha256'],
            'final': final, 'native_smoke_sha256': sha(op.read_file(run / 'native-smoke.json')), 'last_intent_sha256': journal.previous,
            'old_failure_records_unchanged': True, 'service_mutations': 0, 'database_migrations': 0, 'database_restores': 0,
            'automatic_retry': False, 'new_session_only': True, 'logout_verified': True}
        op.write_exclusive(run / 'terminal.json', canonical(terminal))
        return {'status': 'passed', 'report': str(run / 'terminal.json'), 'sha256': sha(canonical(terminal))}
    except BaseException as error:
        terminal = {**binding, 'status': 'failed', 'error_type': type(error).__name__, 'reason': reason(module, op, error),
            'last_intent_sha256': journal.previous, 'evidence_retained': True, 'automatic_retry': False,
            'service_mutations': 0, 'database_migrations': 0, 'database_restores': 0}
        if not os.path.lexists(run / 'terminal.json'):
            op.write_exclusive(run / 'terminal.json', canonical(terminal))
        raise


def reason(module, op, error):
    if isinstance(error, Failure):
        return str(error)
    return module.failure_reason(op, error) if module is not None else type(error).__name__


def parser():
    value = argparse.ArgumentParser(description=__doc__)
    value.add_argument('--mode', choices=('preflight', 'complete'), required=True)
    for name in ('upgrade-source', 'upgrade-arguments', 'guard-report'):
        value.add_argument('--' + name, type=Path, required=True)
    for name in ('script', 'upgrade-source', 'upgrade-arguments', 'guard-report'):
        value.add_argument('--' + name + '-sha256', required=True)
    return value


def main(argv=None):
    args, module, op = parser().parse_args(argv), None, None
    try:
        require(sys.platform == 'linux' and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and
                os.environ.get('SSH_CONNECTION'), 'Run only through authorized root SSH on test-env.')
        os.umask(0o077)
        module, op, source_args, published = load_inputs(args)
        with module.operation_scope(source_args):
            with module.deployment_lock(op):
                with module.startup_plan_checks(op, source_args):
                    require(not os.path.lexists(ROOT), 'This independent completion already has evidence; review it instead of retrying.')
                    inputs = module.candidate_inputs(op, source_args, published)
                    evidence = retained_evidence(module, op, source_args, inputs)
                    observed = live_checks(module, op, source_args, inputs, evidence)
                    if args.mode == 'preflight':
                        result = {'status': 'preflight-passed', 'observation_sha256': OBSERVATION_SHA,
                                  'preflight_sha256': sha(canonical(observed)), 'mutations_executed': False}
                    else:
                        result = complete(args, module, op, source_args, inputs, evidence, observed)
        print(json.dumps(result))
        return 0
    except Exception as error:
        print(json.dumps({'status': 'blocked', 'reason': reason(module, op, error), 'automatic_retry': False,
                          'service_mutations': 0, 'database_migrations': 0, 'database_restores': 0}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
