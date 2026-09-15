"""Render three fixed capacity units and inspect their absence without changes.

Imports, anchor_unit(), postgres_unit(anchor_pid), app_unit(anchor_pid), and
expected_properties(anchor_pid) perform no I/O. Every unit file belongs under
UNIT_ROOT; none is enabled and there is no drop-in. These custom units do not
install or verify the shipped goby.service. The caller must bind anchor_pid to
the live, owned ANCHOR before publishing either namespace consumer. Preparation
creates STATE, CACHE, and LOGS as new goby:goby 0700 directories; systemd creates
only RUNTIME through RuntimeDirectory. Fixture ancestors and BINARY must remain
traversable/readable by goby without making these private directories public.
Preparation exclusively creates APP_LOG as a root:root 0600 empty file and
saves its metadata. Both APP output streams append there. The controller binds
the actual fd 1/fd 2 paths and inodes to that file; no unobserved systemd file
property is assumed. GOBY_LOG_DIR uses LOGS/diagnostics in the generated fixture
environment, keeping application diagnostics separate from this output record.

expected_properties() returns a declaration from this frozen source, never a
projection of observed systemctl output. Its exact fields compare as strings;
tokenSets compare after splitting fixed space-free tokens, rejecting duplicates,
and sorting without resolving paths or removing '-' prefixes. This handles
systemd list ordering while preserving path flags and environment assignments.
microseconds and integers require strict typed conversion of the corresponding
systemctl fields. execStart and environmentFiles require structured parsing of
one command/file with exact path, argv and ignore-errors policy; command lifetime
fields are separately checked by the caller's ownership and startup controller.
The caller preserves its observed preparedServiceProperties independently.

The APP limit is 1500 seconds, with Restart=no. The declared upper allowances
sum to 1385 seconds: 1200 business, 15 reader join, 60 cancellation, eight
10-second credential requests, 20 normal stop, and 10 startup observation.
The remaining 115 seconds are not a proof about unlisted controller overhead.
The caller must retain one continuous deadline rather than reset phase budgets.

inspect_absence(base) runs under the caller's already-held deployment lock and
uses its bounded base.run recorder for two systemctl listings. It records into
base.report['capacityAbsence'] before any guard can fail. E may already contain
its root-owned private evidence, including environment-preflight; F, RUNTIME,
the three fixed unit files, and their registrations/aliases must remain absent.
Global, inherited-prefix, and exact-unit .conf drop-ins are rejected. Empty real
global/prefix directories carry no configuration; applicable directory symlinks
and exact-unit .d paths are rejected even when empty. Only metadata, link targets, account identities,
and applicable drop-in hashes are read. No existing environment/configuration,
unit body, password field, master key or application data is decoded. This module
never takes a lock, starts/stops a service, reloads systemd, or mounts storage.
"""
import grp
import hashlib
import os
from pathlib import Path
import pwd
import re
import stat

E = Path('/opt/goby-test/native-scan-http-capacity-20260915')
F = Path('/opt/goby-native-scan-http-capacity-20260915')
PREFIX = 'goby-native-capacity-20260915'
ANCHOR = PREFIX + '-net.service'
PGUNIT = PREFIX + '-postgres.service'
APP = PREFIX + '-app.service'
UNITS = (ANCHOR, PGUNIT, APP)
UNIT_ROOT = Path('/run/systemd/system')
UNIT_PATHS = {name: UNIT_ROOT / name for name in UNITS}
BINARY, ENVIRONMENT = F / 'bin/goby', F / 'config/goby.env'
STATE, CACHE, LOGS = F / 'state', F / 'cache', F / 'log'
APP_LOG = LOGS / 'application.jsonl'
RUNTIME = Path('/run') / PREFIX
ABSENT_PATHS = (F, RUNTIME, *UNIT_PATHS.values())
GOBY_UID, GOBY_GID = 995, 986
POSTGRES_UID, POSTGRES_GID = 103, 106
PG_DATA, PG_SOCKET = F / 'pg/data', F / 'pg/socket'
LEGACY_APPLICATION_PATHS = (
    Path('/usr/local/bin/goby'), Path('/etc/goby'), Path('/var/lib/goby'),
    Path('/var/cache/goby'), Path('/var/log/goby'), Path('/run/goby'), Path('/var/run/goby'),
)
DENIED = (
    '/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b',
    '/opt/goby-audited-candidate-20260914T083143Z-9b73ad46f2e6',
    '/opt/goby-dev', '/opt/goby-test', '/var/lib/goby-test', '/var/log/goby-test',
    '/var/lib/postgresql', '/etc/postgresql', '/run/postgresql', '/var/run/postgresql',
)
SYSTEMD_ROOTS = tuple(Path(path) for path in (
    '/etc/systemd/system.control', '/run/systemd/system.control', '/run/systemd/transient',
    '/run/systemd/generator.early', '/etc/systemd/system', '/etc/systemd/system.attached',
    '/run/systemd/system', '/run/systemd/system.attached',
    '/run/systemd/generator', '/usr/local/lib/systemd/system', '/usr/lib/systemd/system',
    '/lib/systemd/system', '/run/systemd/generator.late',
))
RELEVANT_DROPINS = {
    'service.d', *(name + '.d' for name in UNITS),
    'goby-.service.d', 'goby-native-.service.d', 'goby-native-capacity-.service.d',
    'goby-native-capacity-20260915-.service.d',
}


def _pid(value):
    if type(value) is not int or not 2 <= value <= 4194304:
        raise ValueError('anchor_pid must be the pinned live anchor MainPID')
    return str(value)


def _deny(paths):
    return 'InaccessiblePaths=' + ' '.join('-' + str(path) for path in paths)


def _render(unit, service):
    return '\n'.join(('[Unit]', *unit, '', '[Service]', *service, ''))


def anchor_unit():
    """Render the complete, independent root-owned network anchor."""
    return _render(('Description=Fixed native capacity network anchor', 'StopWhenUnneeded=no'), (
        'Type=exec', 'User=root', 'Group=root', 'ExecStart=/usr/bin/sleep infinity',
        'PrivateNetwork=yes', 'PrivateTmp=yes', 'ProtectSystem=strict', 'ProtectHome=yes',
        'NoNewPrivileges=yes', 'CapabilityBoundingSet=', 'AmbientCapabilities=',
        'ProtectKernelTunables=yes', 'ProtectKernelModules=yes', 'ProtectControlGroups=yes',
        'RestrictSUIDSGID=yes', 'UMask=0077', 'Restart=no', 'RuntimeMaxSec=3600',
        'MemoryMax=64M', 'MemorySwapMax=0', 'TasksMax=16', 'CPUQuota=10%',
        'TimeoutStartSec=30', 'TimeoutStopSec=15', 'KillMode=control-group',
        'KillSignal=SIGTERM', 'SendSIGKILL=yes', 'LimitCORE=0', 'OOMPolicy=stop',
        'StandardInput=null', _deny((*DENIED, F)),
    ))


def postgres_unit(anchor_pid):
    """Render the one new foreground PostgreSQL, with normal fast shutdown."""
    namespace = 'NetworkNamespacePath=/proc/' + _pid(anchor_pid) + '/ns/net'
    return _render(('Description=Fixed native capacity PostgreSQL', 'After=' + ANCHOR, 'BindsTo=' + ANCHOR,
        'AssertPathIsDirectory=' + str(PG_DATA), 'AssertPathIsDirectory=' + str(PG_SOCKET)), (
        'Type=exec', 'User=postgres', 'Group=postgres', 'WorkingDirectory=' + str(PG_DATA),
        'ExecStart=/usr/lib/postgresql/17/bin/postgres -D ' + str(PG_DATA) + ' -k ' + str(PG_SOCKET),
        namespace, 'PrivateTmp=yes', 'ProtectSystem=strict', 'ProtectHome=yes', 'NoNewPrivileges=yes',
        'ReadWritePaths=' + str(F / 'pg'), _deny(DENIED),
        _deny((*LEGACY_APPLICATION_PATHS, F / 'media')), 'UMask=0077',
        'CapabilityBoundingSet=', 'AmbientCapabilities=', 'RestrictSUIDSGID=yes',
        'ProtectKernelTunables=yes', 'ProtectKernelModules=yes', 'ProtectControlGroups=yes',
        'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6', 'Restart=no',
        'MemoryMax=768M', 'MemorySwapMax=0', 'TasksMax=256', 'CPUQuota=150%',
        'TimeoutStartSec=90', 'TimeoutStopSec=90', 'KillMode=mixed', 'KillSignal=SIGINT',
        'SendSIGKILL=yes', 'LimitCORE=0', 'OOMPolicy=stop', 'StandardInput=null',
    ))


def app_unit(anchor_pid):
    """Render the sole native application lifetime, using only fixture paths."""
    namespace = 'NetworkNamespacePath=/proc/' + _pid(anchor_pid) + '/ns/net'
    return _render(('Description=Fixed native scan and HTTP capacity application',
        'After=' + ANCHOR + ' ' + PGUNIT,
        'AssertPathIsDirectory=' + str(STATE), 'AssertPathIsDirectory=' + str(CACHE),
        'AssertPathIsDirectory=' + str(LOGS), 'AssertPathIsDirectory=' + str(F / 'media')), (
        'Type=simple', 'User=goby', 'Group=goby', 'ExecStart=' + str(BINARY),
        'WorkingDirectory=' + str(STATE), 'EnvironmentFile=' + str(ENVIRONMENT),
        'StateDirectory=', 'CacheDirectory=', 'LogsDirectory=',
        'RuntimeDirectory=' + PREFIX, 'RuntimeDirectoryMode=0700',
        namespace, 'ReadWritePaths=' + ' '.join(str(path) for path in (STATE, CACHE, LOGS, RUNTIME)),
        'ReadOnlyPaths=' + str(F / 'media'), 'InaccessiblePaths=' + str(F / 'pg'), _deny(DENIED),
        'Environment=GOMEMLIMIT=768MiB GOMAXPROCS=2',
        'NoNewPrivileges=yes', 'ProtectSystem=strict', 'ProtectHome=yes', 'PrivateTmp=yes',
        'CapabilityBoundingSet=', 'AmbientCapabilities=', 'RestrictSUIDSGID=yes', 'UMask=0077',
        'MemoryMax=2G', 'MemorySwapMax=0', 'TasksMax=256', 'CPUQuota=200%',
        'RuntimeMaxSec=1500', 'Restart=no', 'RestartSec=5s',
        'TimeoutStartSec=30', 'TimeoutStopSec=20', 'KillMode=control-group',
        'KillSignal=SIGTERM', 'FinalKillSignal=SIGKILL', 'SendSIGKILL=yes',
        'ProtectKernelTunables=yes', 'ProtectKernelModules=yes', 'ProtectKernelLogs=yes',
        'ProtectControlGroups=yes', 'RestrictNamespaces=yes', 'RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6',
        'StandardInput=null', 'StandardOutput=append:' + str(APP_LOG), 'StandardError=append:' + str(APP_LOG),
        'LimitCORE=0', 'OOMPolicy=stop',
    ))


def expected_properties(anchor_pid):
    """Declare APP settings independently of any live unit observation.

    Numeric/time and list categories describe semantic comparisons; the caller
    records raw values as well. Do not compare the full rendered ExecStart field
    because it also contains dynamic process timestamps and status. Parse its
    fixed command prefix and separately bind all dynamic identity fields.
    """
    namespace = '/proc/' + _pid(anchor_pid) + '/ns/net'
    return {
        'exact': {
            'Id': APP, 'LoadState': 'loaded', 'FragmentPath': str(UNIT_PATHS[APP]),
            'DropInPaths': '', 'NeedDaemonReload': 'no', 'Type': 'simple', 'User': 'goby', 'Group': 'goby',
            'WorkingDirectory': str(STATE), 'StateDirectory': '', 'CacheDirectory': '', 'LogsDirectory': '',
            'RuntimeDirectory': PREFIX, 'RuntimeDirectoryMode': '0700',
            'Restart': 'no', 'KillMode': 'control-group', 'SendSIGKILL': 'yes',
            'NoNewPrivileges': 'yes', 'ProtectSystem': 'strict', 'ProtectHome': 'yes', 'PrivateTmp': 'yes',
            'CapabilityBoundingSet': '', 'AmbientCapabilities': '', 'RestrictSUIDSGID': 'yes',
            'UMask': '0077', 'NetworkNamespacePath': namespace,
            'ProtectKernelTunables': 'yes', 'ProtectKernelModules': 'yes', 'ProtectKernelLogs': 'yes',
            'ProtectControlGroups': 'yes', 'RestrictNamespaces': 'yes', 'StandardInput': 'null',
            'StandardOutput': 'append', 'StandardError': 'append', 'OOMPolicy': 'stop',
        },
        'tokenSets': {
            'ReadOnlyPaths': (str(F / 'media'),),
            'ReadWritePaths': tuple(str(path) for path in (STATE, CACHE, LOGS, RUNTIME)),
            'InaccessiblePaths': (str(F / 'pg'), *('-' + path for path in DENIED)),
            'Environment': ('GOMEMLIMIT=768MiB', 'GOMAXPROCS=2'),
            'RestrictAddressFamilies': ('AF_UNIX', 'AF_INET', 'AF_INET6'),
        },
        'microseconds': {'RestartUSec': 5000000, 'TimeoutStartUSec': 30000000,
                         'TimeoutStopUSec': 20000000, 'RuntimeMaxUSec': 1500000000,
                         'CPUQuotaPerSecUSec': 2000000},
        'integers': {'MemoryMax': 2147483648, 'MemorySwapMax': 0, 'TasksMax': 256,
                     'KillSignal': 15, 'FinalKillSignal': 9, 'LimitCORE': 0},
        'execStart': {'property': 'ExecStart', 'commands': 1, 'path': str(BINARY),
                      'argv': (str(BINARY),), 'ignoreErrors': False},
        'environmentFiles': {'property': 'EnvironmentFiles',
                             'files': ({'path': str(ENVIRONMENT), 'ignoreErrors': False},)},
    }


def _observed(base, path):
    return base.metadata(path) if os.path.lexists(path) else None


def _hash_dropin(base, path, info):
    # Hash only; never decode or return a drop-in body that may contain secrets.
    base.need(info['type'] == stat.S_IFREG and info['bytes'] <= 1 << 20, 'global_dropin_file_boundary')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        opened = os.fstat(fd)
        base.need((opened.st_dev, opened.st_ino) == (info['device'], info['inode']), 'dropin_open_changed')
        raw = os.read(fd, (1 << 20) + 1)
        base.need(len(raw) == info['bytes'] and base.metadata(path) == info, 'dropin_changed_during_hash')
        return hashlib.sha256(raw).hexdigest()
    finally:
        os.close(fd)


def _accounts(base):
    result = {}
    for name, uid, gid in (('goby', GOBY_UID, GOBY_GID), ('postgres', POSTGRES_UID, POSTGRES_GID)):
        user, group = pwd.getpwnam(name), grp.getgrnam(name)
        base.need((user.pw_name, user.pw_uid, user.pw_gid, group.gr_name, group.gr_gid) == (name, uid, gid, name, gid) and
                  pwd.getpwuid(uid).pw_name == name and grp.getgrgid(gid).gr_name == name, 'capacity_account_identity')
        result[name] = {'name': name, 'uid': uid, 'gid': gid, 'home': user.pw_dir, 'shell': user.pw_shell,
            'supplementaryGroupIds': sorted(set(os.getgrouplist(name, gid))), 'primaryGroupMemberNames': list(group.gr_mem)}
    return result


def _systemd_inventory(base, result):
    roots, entries, aliases, dropins = {}, {}, {}, {}
    result.update(roots=roots, entries=entries, aliases=aliases, relevantDropins=dropins, uninspectedDirectoryLinks={})
    visited = set(); remaining = 50000
    def visit(directory, depth=0):
        nonlocal remaining
        base.need(depth <= 12 and remaining > 0, 'systemd_metadata_inventory_bound')
        before = base.metadata(directory)
        base.need(before['type'] == stat.S_IFDIR, 'systemd_directory_type')
        key = (before['device'], before['inode'])
        if key in visited: return
        visited.add(key)
        with os.scandir(directory) as iterator: names = sorted(item.name for item in iterator)
        for name in names:
            path = directory / name; info = base.metadata(path); remaining -= 1
            base.need(remaining >= 0, 'systemd_metadata_inventory_bound')
            row = {'metadata': info}; entries[str(path)] = row
            relevant = path.parent.name in RELEVANT_DROPINS and name.endswith('.conf')
            if info['type'] == stat.S_IFLNK:
                row['linkTarget'] = os.readlink(path)
                base.need(base.metadata(path) == info, 'systemd_link_changed')
                target = Path(row['linkTarget'])
                if not target.is_absolute(): target = path.parent / target
                # resolve(strict=False) follows path metadata only, not bodies.
                row['resolvedTarget'] = str(target.resolve(strict=False))
                aliases[str(path)] = {'name': name, 'target': row['linkTarget'], 'resolvedTarget': row['resolvedTarget']}
                # Following an applicable directory link can change its basename
                # and hide inherited .conf files from the relevance predicate.
                if relevant or name in RELEVANT_DROPINS: dropins[str(path)] = row
                resolved = Path(row['resolvedTarget']); target_info = _observed(base, resolved)
                if target_info is not None and target_info['type'] == stat.S_IFDIR:
                    if any(resolved == root or resolved.is_relative_to(root) for root in SYSTEMD_ROOTS):
                        visit(resolved, depth + 1)
                    else:
                        result['uninspectedDirectoryLinks'][str(path)] = {'target': str(resolved), 'metadata': target_info}
            elif info['type'] == stat.S_IFDIR:
                visit(path, depth + 1)
            elif info['type'] == stat.S_IFREG:
                if relevant:
                    row['sha256'] = _hash_dropin(base, path, info); dropins[str(path)] = row
            else:
                base.need(False, 'systemd_special_path')
        with os.scandir(directory) as iterator: after_names = sorted(item.name for item in iterator)
        base.need(names == after_names and base.metadata(directory) == before, 'systemd_directory_changed')
    for root in SYSTEMD_ROOTS:
        info = _observed(base, root); roots[str(root)] = {'metadata': info}
        if info is None: continue
        resolved = root.resolve(strict=True)
        roots[str(root)]['resolvedPath'] = str(resolved)
        base.need(resolved in set(SYSTEMD_ROOTS), 'systemd_root_resolution')
        visit(resolved)
    candidates = set(UNITS)
    # A link named arbitrarily can still alias a target unit, including through
    # a chain. Record it and reject rather than treating the unit file as absent.
    changed = True
    while changed:
        changed = False
        for row in aliases.values():
            if Path(row['resolvedTarget']).name in candidates and row['name'] not in candidates:
                candidates.add(row['name']); changed = True
    result['matchingAliases'] = {path: row for path, row in aliases.items() if row['name'] in candidates or
        Path(row['resolvedTarget']).name in candidates}
    result['unitAndAliasNames'] = sorted(candidates)
    result['matchingScopeEntries'] = {path: row for path, row in entries.items() if
        Path(path).name in candidates or Path(path).name in {name + '.d' for name in UNITS} or
        (Path(path).name.startswith(PREFIX) and Path(path).name.endswith('.service'))}
    return candidates


def inspect_absence(base):
    """Record then reject existing paths, unit state, aliases or global drop-ins."""
    result = {'kind': 'native-capacity-absence-inspection', 'version': 1, 'status': 'checking',
        'scope': str(E), 'fixture': str(F), 'units': list(UNITS), 'paths': {}, 'systemd': {},
        'serviceChanges': 0, 'accountChanges': 0, 'fileContentsRead': 'Only global or applicable systemd drop-in hashes; no bodies returned.'}
    base.report['capacityAbsence'] = result
    try:
        base.need(os.geteuid() == 0, 'capacity_absence_root_required')
        for path in (E, E / 'private'):
            info = _observed(base, path); result['paths'][str(path)] = info
            base.need(info is not None and info['type'] == stat.S_IFDIR and info['uid'] == info['gid'] == 0 and
                info['mode'] == 0o700 and path.resolve() == path, 'capacity_evidence_scope')
        result['scopeEntries'] = sorted(path.name for path in E.iterdir())
        base.need(result['scopeEntries'] == ['private'], 'capacity_scope_already_used')
        for path in ABSENT_PATHS: result['paths'][str(path)] = _observed(base, path)
        result['accounts'] = _accounts(base)
        names = _systemd_inventory(base, result['systemd'])
        matches = {}
        for label, operation in (('loadedUnits', 'list-units'), ('unitFiles', 'list-unit-files')):
            raw = base.run('absence-' + operation, ['/usr/bin/systemctl', operation, '--all', '--no-legend', '--no-pager', '--plain'], 30)
            text = raw.decode('utf-8', errors='strict')
            lines = [line.split() for line in text.splitlines() if line.strip()]
            base.need(len(lines) <= 20000 and all(re.fullmatch(r'[^\s\x00-\x1f]+', line[0]) for line in lines), 'systemd_list_shape')
            matches[label] = [line[0] for line in lines if line[0] in names or line[0].startswith(PREFIX)]
            result['systemd'][label] = {'rowsObserved': len(lines), 'matchingNames': matches[label]}
        base.need(all(result['paths'][str(path)] is None for path in ABSENT_PATHS), 'capacity_fixture_or_unit_path_exists')
        base.need(not result['systemd']['relevantDropins'], 'global_or_applicable_service_dropin_exists')
        base.need(not result['systemd']['uninspectedDirectoryLinks'], 'systemd_directory_link_outside_inventory')
        base.need(not result['systemd']['matchingAliases'] and not result['systemd']['matchingScopeEntries'], 'systemd_capacity_path_exists')
        base.need(not any(matches.values()), 'unit_already_loaded_or_registered')
        result.update(status='absent', noMatchingLoadedUnits=True, noMatchingUnitFiles=True,
                      noMatchingAliasesOrEnablement=True, capacityFixtureAndRuntimeAbsent=True,
                      noStandardInstallationChanges=True)
        return result
    except BaseException as error:
        result.update(status='rejected', errorCode=str(error) if isinstance(error, base.Rejected) else type(error).__name__)
        raise
