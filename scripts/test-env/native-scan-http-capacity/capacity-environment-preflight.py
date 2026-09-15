"""Observe fixed capacity prerequisites remotely without admitting any workload.

This entry only writes its new private observation files. It never starts or
stops services, mounts storage, decodes existing configuration, or queries SQL
or HTTP. Historical helpers are imported as definitions with exact source pins.
The observation is not a capacity reservation or an executable scenario input.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import pwd
import grp
import re
import signal
import stat
import sys
import types


E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
PREFIX = 'goby-native-capacity-20260915'
UNITS = tuple(PREFIX + suffix for suffix in ('-app.service', '-postgres.service', '-net.service'))
SELF = E / 'private/capacity-environment-preflight.py'
OUT = E / 'private/environment-preflight'
OLD = Path('/opt/goby-test/m6-systemd-install-20260915-r04/private')
DEPENDENCIES = {
    'support': ('m6-systemd-install-support.py', '8796a8ea8dac42004aae9df92fd56c204b543aa964f246a2eae343460f079c37', 47122),
    'prepare': ('m6-systemd-install-prepare.py', '8cb9e46efdbfd76a3a2b05ecc5d40c43f74020ff66843cd63a94956a4a72f1c8', 61473),
}
OLD_INPUT_SHA = 'e3f7b37dabe4433df78403c7bf8984a14a1848227a42510c9e594b7d22852e89'


def need(value, code):
    if not value:
        raise ValueError(code)


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + '\n').encode()


def read_pin(path, checksum=None, size=None, maximum=2 << 20):
    need(path.is_absolute() and path.resolve() == path, 'source_path')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == before.st_gid == 0 and
             before.st_nlink == 1 and not stat.S_IMODE(before.st_mode) & 0o022 and
             0 < before.st_size <= maximum, 'source_metadata')
        chunks = []
        remaining = before.st_size
        while remaining:
            chunk = os.read(fd, min(remaining, 1 << 20))
            need(chunk, 'source_read_incomplete')
            chunks.append(chunk)
            remaining -= len(chunk)
        after = os.fstat(fd)
        need(all(getattr(before, key) == getattr(after, key) for key in
                 ('st_dev', 'st_ino', 'st_size', 'st_uid', 'st_gid', 'st_mode', 'st_nlink', 'st_mtime_ns', 'st_ctime_ns')),
             'source_changed')
    finally:
        os.close(fd)
    raw = b''.join(chunks)
    digest = hashlib.sha256(raw).hexdigest()
    need((checksum is None or digest == checksum) and (size is None or len(raw) == size), 'source_pin')
    return raw, {'path': str(path), 'sha256': digest, 'bytes': len(raw)}


def load(role):
    name, checksum, size = DEPENDENCIES[role]
    path = OLD / name
    raw, descriptor = read_pin(path, checksum, size)
    module = types.ModuleType('capacity_readonly_' + role)
    module.__file__ = str(path)
    exec(compile(raw, str(path), 'exec'), module.__dict__)
    return module, descriptor


def save(path, value):
    raw = encoded(value)
    need(len(raw) <= 4 << 20, 'receipt_limit')
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    return read_pin(path, maximum=4 << 20)[1]


def interrupted(_signal, _frame):
    raise ValueError('readonly_observation_deadline')


def error_code(error):
    code = str(error) if type(error).__name__ in ('ValueError', 'Rejected') else type(error).__name__
    return code if re.fullmatch('[A-Za-z0-9_.-]{1,160}', code) else type(error).__name__


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-sha256', required=True)
    args = parser.parse_args()
    need(sys.platform == 'linux' and os.geteuid() == os.getegid() == 0 and
         sys.flags.isolated and sys.flags.dont_write_bytecode and 'SSH_CONNECTION' in os.environ and
         Path(__file__) == SELF and re.fullmatch('[0-9a-f]{64}', args.source_sha256), 'fixed_remote_entry')
    source = read_pin(SELF, args.source_sha256)[1]
    os.umask(0o077)
    for path in (E, E / 'private'):
        row = path.lstat()
        need(path.resolve() == path and stat.S_ISDIR(row.st_mode) and row.st_uid == row.st_gid == 0 and
             stat.S_IMODE(row.st_mode) == 0o700, 'private_evidence_root')
    need(not os.path.lexists(OUT), 'observation_consumed')
    OUT.mkdir(mode=0o700)
    commands = OUT / 'commands'
    commands.mkdir(mode=0o700)
    report = {'kind': 'native-scan-http-capacity-environment-preflight', 'version': 1,
              'scope': str(E), 'fixture': str(F), 'status': 'observing', 'operator': source,
              'commands': [], 'ownedUnits': {}, 'createdPaths': [], 'serviceActions': 0,
              'mountActions': 0, 'sqlConnections': 0, 'httpRequests': 0,
              'oldActorMainInvocations': 0, 'executionAdmitted': False, 'lockReleased': False, 'finalErrors': []}
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(60)
    lock = support = prepare = base = None
    try:
        support, support_pin = load('support')
        prepare, prepare_pin = load('prepare')
        report['sourceDefinitions'] = {'support': support_pin, 'prepare': prepare_pin}
        base = support.load_base()
        base.E, base.P, base.serial, base.ENV, base.report = E, commands, 0, dict(support.BASE_ENV), report
        original_run = base.run
        def readonly_command(label, argv, timeout=15):
            need(argv[0] == '/usr/bin/systemctl' and argv[1] in ('show', 'list-units', 'list-unit-files'),
                 'readonly_command_allowlist')
            return original_run(label, argv, min(timeout, 15))
        base.run = readonly_command
        prepare.base, prepare.support = base, support
        lock, metadata = support.acquire_lock(base)
        report['lockMetadata'] = metadata
        report['protectedBefore'] = support.protected(base)
        raw, old_input = read_pin(OLD / 'prepare-input.json', OLD_INPUT_SHA, 2844)
        value = json.loads(raw)
        tools, tool_receipt = prepare.verify_tools(value)
        need(tool_receipt['protectedAfter'] == report['protectedBefore'], 'protected_tool_baseline')
        payloads, _manifest = prepare.package_payloads(value)
        report.update(sourceDefinitions={'support': support_pin, 'prepare': prepare_pin},
                      historicalInputReadOnly=old_input, toolCount=len(tools),
                      package=value['package'],
                      packagePayloads={name: {'bytes': len(body), 'sha256': hashlib.sha256(body).hexdigest()}
                                       for name, body in payloads.items()})
        report['accounts'] = {}
        for name, expected_uid, expected_gid in (('goby', 995, 986), ('postgres', 103, 106)):
            user, group = pwd.getpwnam(name), grp.getgrnam(name)
            need((user.pw_uid, user.pw_gid, group.gr_gid) == (expected_uid, expected_gid, expected_gid), 'account_identity')
            report['accounts'][name] = {'uid': user.pw_uid, 'gid': user.pw_gid, 'group': group.gr_name}
        paths = [F, Path('/run') / PREFIX]
        paths += [Path(root) / name for root in ('/run/systemd/system', '/etc/systemd/system') for name in UNITS]
        report['targetPathsAbsent'] = {str(path): not os.path.lexists(path) for path in paths}
        need(all(report['targetPathsAbsent'].values()), 'fresh_target_path_present')
        report['targetUnitLists'] = {}
        for operation in ('list-units', 'list-unit-files'):
            raw = base.run('capacity-' + operation,
                           ['/usr/bin/systemctl', operation, '--all', '--no-legend', '--no-pager', '--plain'], 15)
            rows = [line.split() for line in raw.decode('utf-8', 'strict').splitlines() if line.strip()]
            need(len(rows) <= 20000, 'unit_list_bound')
            matches = [row[0] for row in rows if row[0] in UNITS or row[0].startswith(PREFIX)]
            need(not matches, 'fresh_unit_present')
            report['targetUnitLists'][operation] = {'rowsObserved': len(rows), 'matchingNames': matches}
        fs = os.statvfs(E)
        memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
        cgroup = Path('/sys/fs/cgroup')
        report['environment'] = {
            'machine': os.uname().machine, 'cpuCount': os.cpu_count(),
            'allowedCpuCount': len(os.sched_getaffinity(0)),
            'rootFreeBytes': fs.f_bavail * fs.f_frsize, 'rootFreeInodes': fs.f_favail,
            'memAvailableBytes': int(memory['MemAvailable'].split()[0]) * 1024,
            'cgroupControllers': (cgroup / 'cgroup.controllers').read_text().split(),
            'rootMetricFilesPresent': {name: (cgroup / name).is_file() for name in
                ('memory.current', 'memory.peak', 'memory.events', 'cpu.stat', 'cgroup.events')},
            'evidenceDevice': E.stat().st_dev, 'fixtureParentDevice': F.parent.stat().st_dev,
            'mountInfo': [line for line in Path('/proc/self/mountinfo').read_text().splitlines()
                          if line.split()[4] in ('/', '/opt', '/opt/goby-test', '/sys/fs/cgroup')],
        }
        env = report['environment']
        report['capacityChecks'] = {
            'rootAtLeast4GiB': env['rootFreeBytes'] >= 4 << 30,
            'inodesAtLeast30000': env['rootFreeInodes'] >= 30000,
            'availableMemoryAtLeast6GiB': env['memAvailableBytes'] >= 6 << 30,
            'amd64Machine': env['machine'] == 'x86_64',
            'cpuAndMemoryControllers': {'cpu', 'memory'} <= set(env['cgroupControllers']),
        }
        report['templateSourceDecision'] = 'Old transient corpus did not retain template files; generate two fresh fixed-recipe templates after source admission.'
        report['referenceApiImportedWithoutBus'] = support.reference_api(base) is not None
        report['status'] = ('readonly_prerequisites_observed' if all(report['capacityChecks'].values())
                            else 'readonly_prerequisite_constraints_not_met')
    except BaseException as error:
        report.update(status='observation_failed', errorCode=error_code(error))
    finally:
        signal.alarm(20)
        try:
            if lock is not None and 'protectedBefore' in report:
                report['protectedAfter'] = support.protected(base)
                need(report['protectedAfter'] == report['protectedBefore'], 'protected_state_changed')
                need(base.metadata(base.LOCK) == report['lockMetadata'], 'lock_metadata_changed')
        except BaseException as error:
            report['finalErrors'].append({'phase': 'protected_after', 'errorCode': error_code(error)})
            report['status'] = 'observation_failed'
        finally:
            if lock is not None:
                try:
                    os.close(lock)
                    report['lockReleased'] = True
                except BaseException as error:
                    report['finalErrors'].append({'phase': 'lock_release', 'errorCode': error_code(error)})
                    report['status'] = 'observation_failed'
            signal.alarm(0)
        report['allOwnedCommandsClosed'] = all(row.get('closed') is True for row in report['commands'])
        if not report['allOwnedCommandsClosed']:
            report['status'] = 'observation_failed'
            report['finalErrors'].append({'phase': 'command_closure', 'errorCode': 'observation_command_open'})
        receipt = save(OUT / 'result.json', report)
        print(json.dumps({'status': report['status'], 'receipt': receipt, 'capacityChecks': report.get('capacityChecks'),
                          'environment': {key: report.get('environment', {}).get(key) for key in
                            ('machine', 'cpuCount', 'allowedCpuCount', 'rootFreeBytes', 'rootFreeInodes', 'memAvailableBytes')},
                          'commandCount': len(report['commands']), 'lockReleased': report['lockReleased'],
                          'executionAdmitted': False}), flush=True)
    return 0 if report['status'] == 'readonly_prerequisites_observed' else 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except Exception as error:
        print(json.dumps({'status': 'observation_entry_rejected', 'errorCode': type(error).__name__, 'executionAdmitted': False}), flush=True)
        raise SystemExit(2) from None
