#!/usr/bin/env python3
"""Create only the missing, empty main-service transcode cache after preparation.

Run only through authorized root SSH on Linux. Inspect is read-only. Create
requires its exact inspection digest and a completed, pinned deployment prepare
receipt. The frozen deployment operator supplies read-only identity guards and
the shared lock; its mutation entry points are never called. Existing cache or
control paths, including interrupted attempts, are never adopted or removed.
"""

from __future__ import annotations

import argparse
import datetime as dt
import grp
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import stat
import sys
import types

sys.dont_write_bytecode = True

WORK = Path('/opt/goby-test/exec-work-m3e')
BACKUPS = Path('/opt/goby-test/backups/client-schema25-v1')
CACHE = Path('/dev/shm/goby-transcodes-test')
CONTROL = WORK / 'main-transcode-cache-create-v1'
OWNER = Path('/opt/goby-test/transcode-cache.owner')
OWNER_BYTES = b'goby-foundation-transcode-cache-v1\n'
OWNER_SHA = 'd28cdc05d8a25c2f067e1507e30bc4a6b0f9dbd99526c0430855fbcd2470a2e7'
MARKER = 'goby-main-transcode-cache-create-v1'
UID, GID, MODE = 995, 986, 0o700
HASH = re.compile(r'[0-9a-f]{64}')
BOOT = re.compile(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}')
RUN = re.compile(r'run-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{24}')
DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


class Failure(Exception):
    """A fixed guard failed; retain every artifact without automatic recovery."""


def require(value, message):
    if not value:
        raise Failure(message)


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def fact(info):
    return {'device': info.st_dev, 'inode': info.st_ino, 'uid': info.st_uid,
            'gid': info.st_gid, 'mode': stat.S_IMODE(info.st_mode)}


def file_identity(info):
    return {**fact(info), 'bytes': info.st_size, 'links': info.st_nlink,
            'mtime_ns': info.st_mtime_ns, 'ctime_ns': info.st_ctime_ns}


def secure_file(path, limit=2 << 20):
    require(path.is_absolute() and '..' not in path.parts, 'An input path escaped its absolute scope.')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                'A control-file ancestor is not a trusted root-owned directory.')
    before = path.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
            stat.S_IMODE(before.st_mode) in (0o600, 0o644) and before.st_nlink == 1 and before.st_size <= limit,
            'A pinned input has unsafe type, ownership, permissions, links, or size.')
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC), 'rb') as handle:
        require(file_identity(os.fstat(handle.fileno())) == file_identity(before), 'A pinned input changed while opening.')
        raw = handle.read(limit + 1)
        require(file_identity(os.fstat(handle.fileno())) == file_identity(before) and len(raw) == before.st_size,
                'A pinned input changed while reading.')
    after = path.lstat()
    # Reading may change atime; it cannot change the binding or content facts.
    require(fact(after) == fact(before) and after.st_size == before.st_size and
            after.st_mtime_ns == before.st_mtime_ns and after.st_ctime_ns == before.st_ctime_ns,
            'A pinned input was replaced during its read.')
    return raw


def load_operator(args):
    require(args.operator.name == 'deploy-client-schema25.py' and
            args.operator.parent.name == 'test-env' and args.operator.parent.parent.name == 'scripts' and
            args.operator.parents[3] == WORK and
            re.fullmatch(r'tool-build-[a-z0-9][a-z0-9_-]{1,79}', args.operator.parents[2].name),
            'The dependency is not a frozen deployment tool source.')
    raw = secure_file(args.operator)
    require(sha(raw) == args.operator_sha256, 'The frozen deployment operator digest differs.')
    module = types.ModuleType('main_cache_pinned_deployment')
    module.__file__ = str(args.operator)
    exec(compile(raw, str(args.operator), 'exec'), module.__dict__)
    require(module.WORK == WORK and module.ROOT == BACKUPS and module.MAIN == {
        'database': 'goby_test', 'database_oid': 16385, 'role': 'goby_test', 'role_oid': 16384},
        'The pinned dependency has a different fixed deployment scope.')
    return module


def prepared_state(op, args):
    run = args.prepared.parent
    require(args.prepared.name == 'prepared.json' and run.parent == BACKUPS and RUN.fullmatch(run.name),
            'The prepared receipt escaped its fixed deployment run.')
    op.directory(run)
    inputs = op.decode(op.read_file(run / 'inputs.json'))
    verified_run, receipt = op.verify_prepared(args, inputs)
    require(verified_run == run and inputs['tool_source']['path'] == str(args.operator.parents[2]) and
            inputs['tool_source']['files']['scripts/test-env/deploy-client-schema25.py'] == args.operator_sha256,
            'The prepared receipt belongs to another frozen operator.')
    proof = op.decode(op.read_file(run / 'main-after-rehearsal-cluster.json'))
    baseline = op.decode(op.read_file(run / 'protected-before.json'))
    require(proof['boot_id'] == args.boot_id and proof['system_identifier'] == op.SYSTEM_IDENTIFIER and
            proof['port'] == 5432 and proof['server_version'] == 170011 and
            proof['data_directory'] == '/var/lib/postgresql/17/main' and
            all(proof.get(k) == v for k, v in op.MAIN.items()),
            'The prepared cluster proof differs from the reviewed main cluster or boot.')
    return inputs, receipt, proof, baseline


def directory_fact_at(parent_fd, name):
    info = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
    require(stat.S_ISDIR(info.st_mode), 'An exact directory entry changed its type.')
    return fact(info)


def parent_state(args):
    chain = []
    for path, mode in ((Path('/'), 0o755), (Path('/dev'), 0o755), (CACHE.parent, 0o1777)):
        info = path.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == info.st_gid == 0 and
                stat.S_IMODE(info.st_mode) == mode, 'The cache ancestor type, owner, or mode differs.')
        chain.append({'path': str(path), **fact(info)})
    require(chain[-1]['device'] == args.parent_device and chain[-1]['inode'] == args.parent_inode,
            'The cache parent inode differs from the explicit inspection.')
    mounts = []
    for line in Path('/proc/self/mountinfo').read_text().splitlines():
        left, separator, right = line.partition(' - ')
        columns, filesystem = left.split(), right.split()
        if separator and len(columns) >= 6 and columns[4] == str(CACHE.parent):
            require(len(filesystem) == 3 and filesystem[0] == filesystem[1] == 'tmpfs' and
                    columns[3] == '/' and {'rw', 'nosuid', 'nodev'} <= set(columns[5].split(',')) and
                    'ro' not in columns[5].split(',') and 'rw' in filesystem[2].split(','),
                    'The cache parent is not the expected writable private-output tmpfs mount.')
            mounts.append(line)
    require(len(mounts) == 1 and int(mounts[0].split()[0]) == args.mount_id,
            'The cache parent mount identity differs.')
    require(mounts[0].split()[2] == f'{os.major(args.parent_device)}:{os.minor(args.parent_device)}',
            'The parent inode and mount device disagree.')
    return {'ancestors': chain, 'mountinfo': mounts[0]}


def absent(path):
    try:
        path.lstat()
    except FileNotFoundError:
        return
    raise Failure('An exact creation path already exists; no adoption or retry is permitted.')


def primary_state(op, proof):
    process = op.read_primary_process()
    require(process['boot_id'] == proof['boot_id'] and process['pid'] == proof['postmaster_pid'] and
            process['start_ticks'] == proof['postmaster_start_ticks'],
            'The primary PostgreSQL process changed since preparation.')
    root = Path('/proc') / str(process['pid'])
    postgres_uid, postgres_gid = op.postgres_identity()
    status = (root / 'status').read_text().splitlines()
    for key, wanted in (('Uid:', postgres_uid), ('Gid:', postgres_gid)):
        require(next((line.split()[1:] for line in status if line.startswith(key)), None) == [str(wanted)] * 4,
                'The primary PostgreSQL process credentials differ.')
    require(os.readlink(root / 'exe') == str(op.PG / 'postgres'),
            'The primary PostgreSQL executable path differs.')
    sql = op.decode(op.pg("""BEGIN READ ONLY; SELECT json_build_object(
      'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
      'postmaster_start_microseconds',(extract(epoch FROM pg_postmaster_start_time())*1000000)::bigint);
      ROLLBACK;"""))
    require(sql == {'system_identifier': proof['system_identifier'],
                    'postmaster_start_microseconds': proof['postmaster_start_microseconds']} and
            op.read_primary_process() == process, 'The primary SQL or OS startup identity changed.')
    # SQL startup microseconds and PID-file seconds are distinct observations.
    # Each is bound to its own exact prior value, never rounded against the other.
    return {'process': process, 'sql': sql}


def inspect(op, args, created=None):
    _, _, proof, baseline = prepared_state(op, args)
    require(Path('/proc/sys/kernel/random/boot_id').read_text().strip() == args.boot_id,
            'The current boot differs from the requested preparation boot.')
    account, group = pwd.getpwnam('goby'), grp.getgrnam('goby')
    require((account.pw_uid, account.pw_gid, group.gr_gid) == (UID, GID, GID),
            'The fixed service UID or GID differs.')
    owner = op.read_file(OWNER)
    require(owner == OWNER_BYTES and sha(owner) == OWNER_SHA, 'The legacy external cache owner differs.')
    cache_values = op.private_values(op.RUNTIME, {'GOBY_TRANSCODING_ENABLED', 'GOBY_TRANSCODE_CACHE'})
    require(cache_values == {'GOBY_TRANSCODING_ENABLED': 'true', 'GOBY_TRANSCODE_CACHE': str(CACHE)},
            'The configured cache path or enable flag differs.')
    url = op.runtime_policy()
    environment = op.database_environment(url, op.MAIN['database'], op.MAIN['role'])
    current = op.protected_state(environment)
    require(current == baseline and current['quiescent']['active_encodings'] == 0,
            'Protected deployment state changed since the completed preparation.')
    primary = primary_state(op, proof)
    parent = parent_state(args)
    if created is None:
        absent(CACHE)
    else:
        require(fact(CACHE.lstat()) == created and stat.S_ISDIR(CACHE.lstat().st_mode),
                'The new cache was replaced while its prerequisites were rechecked.')
    return {'marker': MARKER, 'version': 1, 'cache': str(CACHE), 'uid': UID, 'gid': GID, 'mode': MODE,
            'boot_id': args.boot_id, 'parent': parent, 'primary': primary,
            'prepared': str(args.prepared), 'prepared_sha256': args.prepared_sha256,
            'operator_sha256': args.operator_sha256, 'protected_state_sha256': sha(op.canonical(current)),
            'legacy_owner': {'path': str(OWNER), 'sha256': OWNER_SHA, 'identity': fact(OWNER.lstat())},
            'cache_missing': created is None, 'active_encodings': 0}


def exclusive_record(directory_fd, name, value):
    raw = canonical(value)
    descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                         0o600, dir_fd=directory_fd)
    with os.fdopen(descriptor, 'wb') as handle:
        info = os.fstat(handle.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == info.st_gid == 0 and
                stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
                'The new private record has an unexpected identity or mode.')
        handle.write(raw)
        handle.flush()
        os.fsync(handle.fileno())
    os.fsync(directory_fd)
    return sha(raw)


def create(op, args, before):
    require(sha(canonical(before)) == args.inspection_sha256, 'The exact prior inspection digest differs.')
    absent(CONTROL)
    # The control directory is outside the cache and the frozen prepared-run
    # inventory. Its mere existence fences every future automatic attempt.
    work_fd = os.open(WORK, DIR_FLAGS)
    control_fd = parent_fd = cache_fd = None
    try:
        require(fact(os.fstat(work_fd)) == fact(WORK.lstat()), 'The private workspace changed.')
        os.mkdir(CONTROL.name, 0o700, dir_fd=work_fd)
        control_fd = os.open(CONTROL.name, DIR_FLAGS, dir_fd=work_fd)
        require(fact(os.fstat(control_fd)) == fact(CONTROL.lstat()) and
                (os.fstat(control_fd).st_uid, os.fstat(control_fd).st_gid,
                 stat.S_IMODE(os.fstat(control_fd).st_mode)) == (0, 0, 0o700),
                'The newly created private control directory differs.')
        os.fsync(work_fd)
        intent_sha = exclusive_record(control_fd, 'intent.json', {
            'marker': MARKER, 'phase': 'intent', 'created_at': dt.datetime.now(dt.timezone.utc).isoformat(),
            'inspection': before, 'inspection_sha256': args.inspection_sha256,
            'script_sha256': sha(secure_file(Path(__file__))), 'automatic_retry_allowed': False})
        # All read-only guards run again after durable intent publication and
        # immediately before the only cache-directory creation attempt.
        require(inspect(op, args) == before, 'A guarded prerequisite changed after the durable intent.')
        parent_fd = os.open(CACHE.parent, DIR_FLAGS)
        require(fact(os.fstat(parent_fd)) == {k: v for k, v in before['parent']['ancestors'][-1].items() if k != 'path'},
                'The opened cache parent differs.')
        absent(CACHE)
        os.mkdir(CACHE.name, MODE, dir_fd=parent_fd)
        cache_fd = os.open(CACHE.name, DIR_FLAGS, dir_fd=parent_fd)
        initial = os.fstat(cache_fd)
        require(stat.S_ISDIR(initial.st_mode) and initial.st_uid == initial.st_gid == 0 and
                stat.S_IMODE(initial.st_mode) == MODE and initial.st_dev == args.parent_device and
                fact(initial) == directory_fact_at(parent_fd, CACHE.name) and
                not os.listdir(cache_fd), 'The newly created cache inode differs or is not empty.')
        created_identity = (initial.st_dev, initial.st_ino)
        # Only the descriptor opened for the successful mkdir above is mutable.
        # EEXIST or any failed guard exits without chown, chmod, or cleanup.
        os.fchown(cache_fd, UID, GID)
        os.fchmod(cache_fd, MODE)
        final = os.fstat(cache_fd)
        require((final.st_dev, final.st_ino) == created_identity and
                (final.st_uid, final.st_gid, stat.S_IMODE(final.st_mode)) == (UID, GID, MODE) and
                fact(final) == directory_fact_at(parent_fd, CACHE.name) and
                not os.listdir(cache_fd), 'The created cache identity, ownership, or empty state changed.')
        require(inspect(op, args, created=fact(final)) == {**before, 'cache_missing': False},
                'A guarded prerequisite changed during cache creation.')
        require(fact(os.fstat(cache_fd)) == fact(final) and
                fact(CACHE.lstat()) == fact(final) and not os.listdir(cache_fd),
                'The new cache changed before receipt publication.')
        os.fsync(cache_fd)
        os.fsync(parent_fd)
        receipt = {'marker': MARKER, 'version': 1, 'phase': 'created', 'cache': str(CACHE),
                   'identity': fact(final), 'empty': True, 'boot_id': args.boot_id,
                   'inspection_sha256': args.inspection_sha256, 'intent_sha256': intent_sha,
                   'legacy_owner_sha256': OWNER_SHA, 'service_started': False,
                   'database_mutated': False, 'old_outputs_restored': False}
        digest = exclusive_record(control_fd, 'created.json', receipt)
        return {'status': 'created', 'receipt': str(CONTROL / 'created.json'), 'sha256': digest}
    finally:
        # Closing descriptors is the only failure cleanup. No content is erased,
        # renamed, adopted, or retried, even if receipt publication failed.
        for descriptor in (cache_fd, parent_fd, control_fd, work_fd):
            if descriptor is not None:
                os.close(descriptor)


def parser():
    value = argparse.ArgumentParser(description=__doc__)
    value.add_argument('--mode', choices=('inspect', 'create'), required=True)
    value.add_argument('--operator', type=Path, required=True)
    value.add_argument('--operator-sha256', required=True)
    value.add_argument('--prepared', type=Path, required=True)
    value.add_argument('--prepared-sha256', required=True)
    value.add_argument('--boot-id', required=True)
    value.add_argument('--parent-device', type=int, required=True)
    value.add_argument('--parent-inode', type=int, required=True)
    value.add_argument('--mount-id', type=int, required=True)
    value.add_argument('--inspection-sha256')
    return value


def main(argv=None):
    try:
        args = parser().parse_args(argv)
        require(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0,
                'This operator requires the reviewed Linux root environment.')
        require(HASH.fullmatch(args.operator_sha256) and HASH.fullmatch(args.prepared_sha256) and
                BOOT.fullmatch(args.boot_id) and min(args.parent_device, args.parent_inode, args.mount_id) > 0,
                'An explicit artifact, boot, or parent identity is invalid.')
        require((args.mode == 'create' and HASH.fullmatch(args.inspection_sha256 or '')) or
                (args.mode == 'inspect' and args.inspection_sha256 is None), 'The inspection pin does not match this phase.')
        op = load_operator(args)
        with op.operator_lock(False):
            absent(CONTROL)
            before = inspect(op, args)
            if args.mode == 'inspect':
                result = {'status': 'inspection-passed', 'inspection_sha256': sha(canonical(before)),
                          'cache': str(CACHE), 'uid': UID, 'gid': GID, 'mode': MODE, 'writes_executed': False}
            else:
                result = create(op, args, before)
        print(json.dumps(result))
        return 0
    except BaseException as error:
        reason = str(error) if isinstance(error, Failure) else type(error).__name__
        print(json.dumps({'status': 'failed', 'reason': reason, 'automatic_recovery_executed': False,
                          'evidence_directory': str(CONTROL)}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
