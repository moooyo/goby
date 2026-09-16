"""Read the fixed native capacity profile after recorded resource closure.

This module has no process, network, database, service, or evidence-writing API.
It reports saved checks and measurement limits separately. In particular, sampled
wall/monotonic anchors do not prove clock continuity or actual scan overlap.
"""

import argparse
import datetime
import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys


SCOPE = Path('/opt/goby-test/native-scan-http-capacity-20260915')
PHASES = ('cold', 'cached')
READERS = ('left', 'right')
OVERLAPS = ('demonstrated', 'none', 'indeterminate')
SHAPES = ('page64', 'count')
SQL_SLOTS = ('prestart-identity', 'empty-catalog', 'cold-catalog',
             'post-favorite-baseline', 'cached-catalog', 'final-after-stop')
UNITS = ('goby-native-capacity-20260915-postgres.service',
         'goby-native-capacity-20260915-net.service')
COST_OPERATIONS = ('ownership', 'metrics', 'pool', 'storageWalk', 'evidenceWrite', 'commandWait')
OVERLAP_GAP = 'Missing a record that binds actual scan-job active state to a client monotonic interval.'
RUNNING_JOB_SCOPE = 'Persisted scan-job Running state only; continuous filesystem or ffprobe work is not established.'
MAX_INPUT_BYTES = 384 << 20


class EvidenceError(Exception):
    """Only fixed, non-sensitive codes may leave the reader."""


def need(condition, code):
    if not condition:
        raise EvidenceError(code)


def integer(value, minimum=0):
    return type(value) is int and value >= minimum


def object_value(value):
    return value if type(value) is dict else {}


def parse_json(raw):
    def pairs(values):
        result = {}
        for key, value in values:
            need(key not in result, 'duplicate_json_key')
            result[key] = value
        return result

    def constant(_value):
        raise EvidenceError('nonfinite_json_number')

    def decimal(value):
        number = float(value)
        need(math.isfinite(number), 'nonfinite_json_number')
        return number

    try:
        return json.loads(raw.decode('utf-8', 'strict'), object_pairs_hook=pairs,
                          parse_constant=constant, parse_float=decimal)
    except (ValueError, UnicodeError, RecursionError):
        raise EvidenceError('invalid_json') from None


def stable(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class SavedScope:
    """Read exact expected paths through no-follow directory descriptors.

    The CLI fixes the root. Tests may supply an owned temporary root; this is
    not an input-file field. No descriptor chooses its own traversal path.
    """

    def __init__(self, root, owner=0):
        self.root = Path(root)
        need(self.root.is_absolute(), 'scope_not_absolute')
        self.owner = owner
        self.total_bytes = 0
        self.pins = []
        self._seen = {}

    def _directory(self, fd):
        info = os.fstat(fd)
        need(stat.S_ISDIR(info.st_mode) and info.st_uid == self.owner and
             stat.S_IMODE(info.st_mode) == 0o700, 'private_directory_contract')

    def read(self, pin, relative, maximum):
        relative_path = PurePosixPath(relative)
        need(not relative_path.is_absolute() and relative_path.parts and
             all(part not in ('', '.', '..') for part in relative_path.parts) and
             str(relative_path) == relative, 'unexpected_relative_path')
        expected = self.root.joinpath(*relative_path.parts)
        need(type(pin) is dict and pin.get('path') == str(expected) and
             type(pin.get('sha256')) is str and
             re.fullmatch('[0-9a-f]{64}', pin['sha256']) is not None and
             ('bytes' not in pin or integer(pin['bytes'])), 'evidence_pin_contract')
        need(integer(maximum, 1), 'evidence_bound_contract')
        # Walk absolute ancestors without following a symlink. Shared ancestors
        # need not be private; the fixed scope and its descendants must be.
        handles, directories = [], []
        try:
            fd = os.open(self.root.anchor, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
            handles.append(fd)
            for part in self.root.parts[1:]:
                parent = fd
                fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                             dir_fd=fd)
                handles.append(fd)
                directories.append((parent, part, fd))
            self._directory(fd)
            for part in relative_path.parts[:-1]:
                parent = fd
                fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                             dir_fd=fd)
                handles.append(fd)
                directories.append((parent, part, fd))
                self._directory(fd)
            parent = fd
            fd = os.open(relative_path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK,
                         dir_fd=parent)
            handles.append(fd)
            before = os.fstat(fd)
            need(stat.S_ISREG(before.st_mode) and before.st_uid == self.owner and
                 before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o600 and
                 before.st_size <= maximum, 'private_file_contract')
            need(self.total_bytes + before.st_size <= MAX_INPUT_BYTES, 'total_input_byte_limit')
            chunks, count = [], 0
            while True:
                part = os.read(fd, min(65536, maximum + 1 - count))
                if not part:
                    break
                chunks.append(part)
                count += len(part)
                need(count <= maximum, 'evidence_byte_limit')
            raw = b''.join(chunks)
            named = os.stat(relative_path.name, dir_fd=parent, follow_symlinks=False)
            need(stable(before) == stable(os.fstat(fd)) == stable(named) and
                 count == before.st_size, 'evidence_changed_during_read')
            for parent_fd, name, directory_fd in directories:
                held = os.fstat(directory_fd)
                named_directory = os.stat(name, dir_fd=parent_fd, follow_symlinks=False)
                need(stat.S_ISDIR(named_directory.st_mode) and
                     (held.st_dev, held.st_ino) == (named_directory.st_dev, named_directory.st_ino),
                     'evidence_directory_replaced')
            digest = hashlib.sha256(raw).hexdigest()
            need(digest == pin['sha256'] and ('bytes' not in pin or pin['bytes'] == count),
                 'evidence_hash_or_size_mismatch')
            observed = {'path': str(expected), 'sha256': digest, 'bytes': count}
            if relative in self._seen:
                need(self._seen[relative] == observed, 'conflicting_evidence_pin')
            else:
                self._seen[relative] = observed
                self.pins.append(observed)
            self.total_bytes += count
            return raw
        except FileNotFoundError:
            raise
        except OSError:
            raise EvidenceError('evidence_open_or_read_failed') from None
        finally:
            for fd in reversed(handles):
                os.close(fd)


def quantiles(values):
    """Quantiles describe valid observations, never an acceptance threshold."""
    need(all(integer(value) for value in values), 'latency_value_contract')
    ordered = sorted(values)
    count = len(ordered)
    if not count:
        return {'n': 0, 'min': None, 'median': None, 'p95': None, 'max': None}
    middle = count // 2
    median = ordered[middle] if count % 2 else (ordered[middle - 1] + ordered[middle]) / 2
    return {'n': count, 'min': ordered[0], 'median': median,
            'p95': ordered[math.ceil(count * .95) - 1], 'max': ordered[-1]}


def anchor_interval(value):
    need(type(value) is dict and set(value) ==
         {'monotonicBeforeNs', 'wallTimeNs', 'monotonicAfterNs'}, 'clock_anchor_contract')
    before, wall, after = (value[key] for key in
                          ('monotonicBeforeNs', 'wallTimeNs', 'monotonicAfterNs'))
    need(all(integer(number, 1) for number in (before, wall, after)) and
         before <= after <= before + 1_000_000_000, 'clock_anchor_bound')
    return before, wall, after


def clock_summary(anchors):
    observations = sorted(set(anchor_interval(row) for row in anchors))
    offsets = [(wall - after, wall - before) for before, wall, after in observations]
    envelope = [min(row[0] for row in offsets), max(row[1] for row in offsets)] if offsets else None
    intersection = [max(row[0] for row in offsets), min(row[1] for row in offsets)] if offsets else None
    if intersection and intersection[0] > intersection[1]:
        intersection = None
    return {'anchorCount': len(observations), 'clockContinuityProven': False,
            'observedOffsetEnvelopeNs': envelope,
            'constantOffsetIntersectionAtObservedAnchorsNs': intersection,
            'wallRegressionsAtOrderedAnchors': sum(right[1] < left[1] for left, right in
                                                  zip(observations, observations[1:])),
            'anchorReadWidthNs': quantiles([after - before for before, _, after in observations]),
            'observations': [{'monotonicBeforeNs': before, 'wallTimeNs': wall,
                              'monotonicAfterNs': after, 'offsetLowerNs': wall - after,
                              'offsetUpperNs': wall - before} for before, wall, after in observations],
            'interpretation': 'Offsets are sampled mappings, not proof of scan overlap or clock continuity.'}


def timestamp_ns(value):
    need(type(value) is str and re.fullmatch(
        r'\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d{1,6})?(?:Z|[+-]\d{2}:\d{2})', value),
        'scan_timestamp_contract')
    try:
        instant = datetime.datetime.fromisoformat(value.replace('Z', '+00:00'))
        delta = instant - datetime.datetime(1970, 1, 1, tzinfo=datetime.timezone.utc)
        return ((delta.days * 86400 + delta.seconds) * 1_000_000 + delta.microseconds) * 1000
    except ValueError:
        raise EvidenceError('scan_timestamp_contract') from None


def same_clock_domains(*values):
    """Absent or different time namespaces cannot authorize clock comparisons."""
    if not values:
        return False
    for value in values:
        if value is None or object_value(value).get('available') is False:
            return False
        need(type(value) is dict and set(value) == {'version', 'available', 'clock', 'implementation',
             'monotonic', 'adjustable', 'bootId', 'timeNamespace'} and
             type(value['version']) is int and value['version'] == 1 and value['available'] is True and
             value['clock'] == 'CLOCK_MONOTONIC' and value['implementation'] == 'clock_gettime(CLOCK_MONOTONIC)' and
             value['monotonic'] is True and value['adjustable'] is False and
             type(value['bootId']) is str and
             re.fullmatch('[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', value['bootId']) and
             type(value['timeNamespace']) is dict and set(value['timeNamespace']) == {'device', 'inode'} and
             integer(value['timeNamespace']['device']) and integer(value['timeNamespace']['inode'], 1),
             'running_job_clock_domain_contract')
    return all(value == values[0] for value in values)


def running_intersection(interval, guaranteed):
    if interval is None or interval[0] >= interval[1]:
        return 'unproven'
    start, end = interval
    if any(lower <= start and end <= upper for lower, upper in guaranteed):
        return 'full'
    if any(max(start, lower) < min(end, upper) for lower, upper in guaranteed):
        return 'partial'
    return 'unproven'


def classify_overlap(interval, guaranteed=(), possible=None):
    """Classify intervals only from supplied bounds, not sampled clock guesses.

    Only paired, clock-bound Running observations may supply guaranteed active
    intervals. Sampled wall-clock offsets never supply these bounds.
    """
    if interval is None:
        return 'indeterminate'
    start, end = interval
    need(integer(start, 1) and integer(end, 1) and start <= end, 'request_interval_contract')
    for lower, upper in guaranteed:
        need(integer(lower, 1) and integer(upper, 1) and lower < upper, 'active_interval_contract')
        if max(start, lower) < min(end, upper):
            return 'demonstrated'
    if possible is not None:
        for lower, upper in possible:
            need((lower is None or integer(lower)) and (upper is None or integer(upper)) and
                 (lower is None or upper is None or lower <= upper), 'possible_interval_contract')
        if all((upper is not None and start >= upper) or (lower is not None and end <= lower)
               for lower, upper in possible):
            return 'none'
    return 'indeterminate'


def request_metrics(row, evidence_complete):
    """Do not discard failures or use incomplete exchanges in latency quantiles."""
    dispatched = row.get('dispatched') is True
    times = [row.get(key) for key in ('dispatchMonotonicNs', 'headersMonotonicNs',
                                    'bodyCompleteMonotonicNs', 'connectionCloseMonotonicNs')]
    timed = all(integer(number, 1) for number in times) and times == sorted(times)
    complete = row.get('bodyComplete') is True and row.get('connectionClosed') is True
    error = row.get('error')
    correct = object_value(row.get('correctness')).get('passed') is True
    success = (dispatched and complete and timed and evidence_complete and correct and
               row.get('status') == 200 and error is None)
    if not dispatched:
        outcome = 'notDispatched'
    elif not evidence_complete:
        outcome = 'evidenceUnavailable'
    elif type(error) is str and ('timeout' in error.lower() or 'deadline' in error.lower()):
        outcome = 'timeout'
    elif error is not None or not complete or row.get('status') != 200:
        outcome = 'transportOrProtocolFailure'
    elif not correct:
        outcome = 'correctnessFailure'
    elif not timed:
        outcome = 'timingUnavailable'
    else:
        outcome = 'validSuccess'
    interval = ((times[0], times[-1]) if dispatched and integer(times[0], 1) and
                integer(times[-1], 1) and times[0] <= times[-1] else None)
    return {'dispatched': dispatched, 'complete': complete, 'outcome': outcome, 'interval': interval,
            'headerNs': times[1] - times[0] if success else None,
            'bodyNs': times[2] - times[0] if success else None,
            'closeNs': times[3] - times[0] if success else None}


def summarize_groups(samples):
    result = []
    outcomes = ('validSuccess', 'notDispatched', 'evidenceUnavailable', 'timeout',
                'transportOrProtocolFailure', 'correctnessFailure', 'timingUnavailable')
    for phase in PHASES:
        for shape in SHAPES:
            for overlap in OVERLAPS:
                selected = [row for row in samples if (row['phase'], row['shape'], row['overlap']) ==
                            (phase, shape, overlap)]
                result.append({'phase': phase, 'shape': shape, 'overlap': overlap,
                    'intentRecords': len(selected),
                    'dispatched': sum(row['dispatched'] for row in selected),
                    'bodyAndConnectionComplete': sum(row['complete'] for row in selected),
                    'outcomes': {name: sum(row['outcome'] == name for row in selected) for name in outcomes},
                    'byReader': {side: sum(row['reader'] == side for row in selected) for side in READERS},
                    'runningJobIntersection': {kind: sum(row.get('runningIntersection') == kind for row in selected)
                                               for kind in ('full', 'partial', 'unproven')},
                    'validSuccessLatencyNs': {key: quantiles([row[field] for row in selected
                        if row[field] is not None]) for key, field in
                        (('dispatchToHeaders', 'headerNs'), ('dispatchToBody', 'bodyNs'),
                         ('dispatchToConnectionClose', 'closeNs'))}})
    return result


def coverage(samples):
    result = []
    for phase in PHASES:
        per_reader = {}
        intervals = {}
        for side in READERS:
            selected = [row for row in samples if row['phase'] == phase and row['reader'] == side]
            intervals[side] = [row['interval'] for row in selected if row['interval'] is not None]
            dispatches = sorted(row['interval'][0] for row in selected if row['interval'] is not None)
            per_reader[side] = {'dispatchesWithClosedIntervals': len(dispatches),
                'firstDispatchMonotonicNs': dispatches[0] if dispatches else None,
                'lastDispatchMonotonicNs': dispatches[-1] if dispatches else None,
                'dispatchSpacingNs': quantiles([right - left for left, right in zip(dispatches, dispatches[1:])]),
                'shapesObserved': sorted({row['shape'] for row in selected if row['dispatched']})}
        intersections = [(max(left[0], right[0]), min(left[1], right[1]))
                         for left in intervals['left'] for right in intervals['right']
                         if max(left[0], right[0]) < min(left[1], right[1])]
        proven_pairs = 0
        for left in [row for row in samples if row['phase'] == phase and row['reader'] == 'left' and row['interval'] is not None]:
            for right in [row for row in samples if row['phase'] == phase and row['reader'] == 'right' and row['interval'] is not None]:
                if any(max(left['interval'][0], right['interval'][0], first[0], second[0]) <
                       min(left['interval'][1], right['interval'][1], first[1], second[1])
                       for first in left.get('provenRunningIntervals', [])
                       for second in right.get('provenRunningIntervals', [])):
                    proven_pairs += 1
        result.append({'phase': phase, 'readers': per_reader,
            'simultaneousClosedHttpIntervalPairs': len(intersections),
            'simultaneousHttpDuringProvenScan': None,
            'simultaneousHttpDuringProvenRunningJobIntervalPairs': proven_pairs,
            'uncoveredActualScanTimeNs': None,
            'interpretation': 'Reader lifetimes and simultaneous HTTP intervals do not prove overlap with active scan jobs.'})
    return result


def cost_summary(value):
    if value is None:
        return {'status': 'unavailable', 'reason': 'measurement_costs_not_recorded'}
    need(type(value) is dict and set(value) == {'version', 'clock', 'accounting', 'stages'} and
         type(value['version']) is int and value['version'] == 1 and value['clock'] == 'time.monotonic_ns' and
         value['accounting'] == 'inclusive_non_additive' and type(value['stages']) is list and
         1 <= len(value['stages']) <= 8, 'measurement_cost_contract')
    previous_end = None
    for stage in value['stages']:
        need(type(stage) is dict and set(stage) ==
             {'phase', 'startedMonotonicNs', 'finishedMonotonicNs', 'operations'} and
             stage['phase'] in ('admission', 'preparation', 'business', 'readiness', 'cleanup', 'closure') and
             integer(stage['startedMonotonicNs'], 1) and
             integer(stage['finishedMonotonicNs'], stage['startedMonotonicNs']) and
             type(stage['operations']) is dict and set(stage['operations']) == set(COST_OPERATIONS),
             'measurement_cost_stage_contract')
        need(previous_end is None or stage['startedMonotonicNs'] == previous_end,
             'measurement_cost_stage_order')
        previous_end = stage['finishedMonotonicNs']
        for operation in stage['operations'].values():
            need(type(operation) is dict and set(operation) == {'calls', 'elapsedNs', 'failures'} and
                 all(integer(number) for number in operation.values()) and
                 operation['failures'] <= operation['calls'], 'measurement_cost_operation_contract')
    return dict(value, status='recorded', coverage='controller_wrappers_and_observer_save_only',
                unwrappedHelperWorkIncluded=False, processCpuMeasurement=False,
                finalCostAndResultPublicationIncluded=False,
                interpretation='Nested elapsed times overlap. Do not sum them or subtract them from HTTP latency.')


def resource_summary(samples):
    starts = [sample['beforeMonotonicNs'] for sample in samples]
    need(starts == sorted(starts) and len(set(starts)) == len(starts), 'resource_sample_order')
    roles = {}
    for role in ('application', 'postgres'):
        rss, current, peak, cpu = [], [], [], []
        for sample in samples:
            process = object_value(object_value(sample.get('processes')).get(role))
            if process.get('status') == 'sampled' and integer(process.get('rssBytes')):
                rss.append(process['rssBytes'])
            files = object_value(object_value(object_value(sample.get('cgroups')).get(role)).get('files'))
            for key, values in (('memory.current', current), ('memory.peak', peak)):
                entry = object_value(files.get(key))
                if entry.get('status') == 'available' and integer(entry.get('value')):
                    values.append(entry['value'])
            entry = object_value(files.get('cpu.stat'))
            counters = object_value(entry.get('value'))
            if entry.get('status') == 'available' and integer(counters.get('usage_usec')):
                cpu.append({'sample': sample['sequence'], 'usageUsec': counters['usage_usec']})
        roles[role] = {'mainProcessRssSamples': len(rss), 'maxSampledMainProcessRssBytes': max(rss) if rss else None,
                       'memoryCurrentSamples': len(current), 'maxSampledCgroupMemoryCurrentBytes': max(current) if current else None,
                       'memoryPeakSamples': len(peak), 'maxRecordedCgroupMemoryPeakBytes': max(peak) if peak else None,
                       'cgroupCpuUsageObservations': cpu}
    return {'status': 'recorded' if samples else 'unavailable', 'samples': len(samples),
            'completeSamples': sum(row.get('status') == 'complete' for row in samples),
            'sampleStartSpacingNs': quantiles([right - left for left, right in zip(starts, starts[1:])]),
            'gapsBeyondTwoSecondTargetNs': [max(0, right - left - 2_000_000_000)
                                          for left, right in zip(starts, starts[1:])],
            'roles': roles, 'wholeProcessTreeRssMeasured': False,
            'interpretation': 'Samples and cgroup counters are observations; main PostgreSQL RSS is not total PostgreSQL RSS.'}


class ResultReader:
    def __init__(self, saved):
        self.saved = saved
        self.gaps = set()
        self.anchors = []

    def load(self, pin, path, maximum=2 << 20, raw=False):
        if pin is None:
            self.gaps.add('missing:' + path)
            return None
        try:
            value = self.saved.read(pin, path, maximum)
        except FileNotFoundError:
            self.gaps.add('missing:' + path)
            return None
        return value if raw else parse_json(value)

    def anchors_from(self, value):
        for name in ('createdAnchor', 'startObservedAnchor', 'finishedAnchor', 'intentAnchor',
                     'dispatchAnchor', 'headersAnchor', 'bodyCompleteAnchor'):
            if value.get(name) is not None:
                anchor_interval(value[name])
                self.anchors.append(value[name])

    def component(self, execution, role):
        return object_value(self.load(object_value(execution.get('componentReceipts')).get(role),
                                      'private/' + role + '.json', 16 << 20))

    def closure(self, execution, pool, commands):
        closed = object_value(execution.get('closure'))
        units = object_value(closed.get('closedUnits'))
        phases = object_value(pool.get('phases'))
        workers = [worker for phase in phases.values() for worker in object_value(phase).get('workers', [])]
        children = pool.get('childrenCreated')
        joined = (integer(children) and children <= 4 and
                  sum(row.get('created') is True for row in workers) == children and
                  all(row.get('joined') is True and row.get('groupEmptyAfterWait') is True and
                      object_value(row.get('wait')).get('exited') is True for row in workers if row.get('created') is True))
        command_groups = [object_value(commands.get(name)) for name in ('admissionAndRuntime', 'preparation', 'closure')]
        command_rows = [row for group in command_groups for row in group.get('commands', [])]
        commands_closed = (bool(command_rows) and all(group.get('allOwnedCommandsClosed') is True and
                           type(group.get('commands')) is list for group in command_groups) and
                           all(object_value(row).get('closed') is True for row in command_rows))
        facts = {'applicationClosed': object_value(closed.get('application')).get('physicalClosed') is True,
                 'infrastructureClosed': set(units) == set(UNITS) and
                    all(object_value(units[name]).get('physicalClosed') is True for name in UNITS),
                 'readerChildrenJoined': joined, 'commandsClosed': commands_closed,
                 'controllerCommandsClosed': execution.get('allOwnedCommandsClosed') is True,
                 'lockReleased': execution.get('lockReleased') is True,
                 'privateNamespaceGone': closed.get('privateNamespaceGone') is True}
        physical = all(facts.values())
        archive_flags = [object_value(object_value(closed.get('archives')).get(name)).get('readbackMatched') is True
                         for name in ('postgres', 'evidence')]
        preserved = (physical and closed.get('status') == 'owned_resources_preserved_and_closed' and
                     closed.get('errors') == [] and closed.get('missing') == [] and all(archive_flags) and
                     all(closed.get(key) is True for key in ('preservationComplete', 'postgresUnmounted',
                         'unitRegistrationsAbsent', 'normalInfrastructureStops', 'fixtureApplicationPathsAbsent',
                         'evidenceSourcePostflightMatched', 'sharedAccountsUnchanged')) and
                     execution.get('protectedBefore') is not None and
                     execution.get('protectedBefore') == execution.get('protectedAfter'))
        return {'status': 'recorded_closed_and_preserved' if preserved else 'incomplete',
                'physicalClosureRecorded': physical, 'checks': facts,
                'archiveReadbackRecorded': all(archive_flags),
                'liveClosureReobserved': False, 'archivesRehashedByThisReader': False}

    def sql(self, observers):
        entries = observers.get('sql', [])
        need(type(entries) is list and len(entries) <= 6, 'sql_slot_bound')
        names = [object_value(row).get('slot') for row in entries]
        need(names == list(SQL_SLOTS[:len(names)]), 'sql_slot_order')
        payloads, accepted = {}, []
        for entry in entries:
            slot = entry['slot']
            receipt = self.load(entry.get('receipt'), 'private/observers/sql/' + slot + '-receipt.json')
            if receipt is not None and entry.get('status') == 'passed':
                need(receipt == {key: value for key, value in entry.items() if key != 'receipt'},
                     'sql_receipt_readback_mismatch')
            payload = self.load(entry.get('parsed'), 'private/observers/sql/' + slot + '-parsed.json')
            if payload is not None:
                need(type(payload) is dict and payload.get('kind') == 'capacity-observation' and
                     payload.get('slot') == slot, 'sql_payload_binding')
                payloads[slot] = payload
            accepted.append(receipt is not None and payload is not None and entry.get('status') == 'passed' and
                            entry.get('payloadAccepted') is True and entry.get('frontendClosed') is True and
                            entry.get('backendGone') is True and entry.get('commandExitCode') == 0)
        if len(entries) != 6:
            self.gaps.add('six_sql_slots_not_available')
        return payloads, len(entries) == 6 and all(accepted) and observers.get('failed') is False

    def scan_lifecycle(self, workload, payloads):
        final = object_value(object_value(payloads.get('final-after-stop')).get('tables'))
        if not final:
            self.gaps.add('final_scan_lifecycle_not_available')
            return {}, []
        indexed = {}
        for table, maximum in (('task_runs', 2), ('task_run_children', 4), ('scan_jobs', 4)):
            rows = final.get(table)
            need(type(rows) is list and len(rows) <= maximum and all(type(row) is dict for row in rows),
                 'scan_table_contract')
            ids = [row.get('id') for row in rows]
            need(all(type(key) is str and re.fullmatch('[0-9a-f]{32}', key) for key in ids) and
                 len(set(ids)) == len(ids), 'scan_identity_contract')
            indexed[table] = {row['id']: row for row in rows}
        phases, summaries, seen = {}, [], set()
        runs = object_value(object_value(workload.get('result')).get('runs'))
        for phase in PHASES:
            detail = object_value(runs.get(phase))
            wire_run = object_value(detail.get('Run'))
            run_id = wire_run.get('Id')
            if run_id not in indexed['task_runs']:
                self.gaps.add('scan_run_unavailable:' + phase)
                continue
            run = indexed['task_runs'][run_id]
            need(run_id not in seen and run.get('task_key') == 'library.scan' and
                 run.get('state') == wire_run.get('State'), 'scan_run_binding')
            seen.add(run_id)
            if run.get('state') != 'completed':
                self.gaps.add('scan_run_not_completed:' + phase)
                continue
            children = [row for row in indexed['task_run_children'].values() if row.get('run_id') == run_id]
            wire_children = object_value(detail.get('Children')).get('Items')
            need(type(wire_children) is list and len(wire_children) == len(children) == 2 and
                 {row.get('id') for row in children} == {object_value(row).get('Id') for row in wire_children},
                 'scan_child_binding')
            jobs = []
            for child in children:
                job = object_value(indexed['scan_jobs'].get(child.get('scan_job_id')))
                wire = next(row for row in wire_children if row['Id'] == child['id'])
                need(job.get('task_child_id') == child['id'] and
                     job.get('library_id') == child.get('library_id') == wire.get('LibraryId') and
                     job.get('id') == wire.get('ScanJobId') and child.get('state') == 'completed' and
                     job.get('status') == 'Completed' and job.get('scanned') == 500 and
                     job.get('added') == (500 if phase == 'cold' else 0) and job.get('updated') == 0,
                     'scan_job_binding')
                created, started, finished = [timestamp_ns(job.get(key)) for key in
                                              ('created_at', 'started_at', 'finished_at')]
                need(created <= started <= finished, 'scan_job_timestamp_order')
                jobs.append((started, finished))
            phases[phase] = {'runId': run_id, 'jobs': jobs, 'run': run,
                             'children': {child['id']: child for child in children},
                             'scanJobs': {child['scan_job_id']: indexed['scan_jobs'][child['scan_job_id']]
                                          for child in children}}
            summaries.append({'phase': phase, 'jobs': len(jobs), 'recordedJobWallIntervalsNs': [list(row) for row in jobs],
                              'recordedJobDurationNs': [end - start for start, end in jobs],
                              'clockDomain': 'database_wall_clock', 'provenMonotonicActiveIntervals': []})
        if len(phases) != 2 or len(indexed['scan_jobs']) != 4:
            self.gaps.add('two_scan_phases_not_reconciled')
        return phases, summaries

    def numbered_record(self, pin, directory, width, maximum, suffix):
        path = object_value(pin).get('path')
        prefix = str(self.saved.root / directory) + '/'
        need(type(path) is str and path.startswith(prefix), 'running_job_record_path')
        match = re.fullmatch(r'([0-9]{%d})%s' % (width, re.escape(suffix)), path[len(prefix):])
        need(match is not None and 1 <= int(match[1]) <= maximum, 'running_job_record_path')
        return directory + '/' + ('%0*d' % (width, int(match[1]))) + suffix, int(match[1])

    def running_observations(self, execution, workload, pool, lifecycle):
        observations = workload.get('scanObservations')
        domains = [execution.get('clockDomainBefore'), execution.get('clockDomainAfter'),
                   pool.get('clockDomainBefore'), pool.get('clockDomainAfter')]
        shared = same_clock_domains(*domains)
        result = {'scope': RUNNING_JOB_SCOPE, 'clockDomainEstablished': False,
                  'controllerPoolClockDomainEstablished': shared, 'status': 'incomplete',
                  'observationCount': 0, 'intervals': [], 'pairedPhases': 0}
        if observations is None:
            self.gaps.add(OVERLAP_GAP)
            return {}, result, domains
        need(type(observations) is list and len(observations) <= 4, 'running_job_observation_bound')
        order = [(phase, position) for phase in PHASES for position in ('first', 'second')]
        need([(object_value(row).get('phase'), object_value(row).get('position')) for row in observations] ==
             order[:len(observations)], 'running_job_observation_order')
        transport = self.component(execution, 'transport-before-preservation')
        receipts = transport.get('receipts', [])
        need(type(receipts) is list and len(receipts) <= 360, 'running_job_transport_receipt_bound')
        reader_source = transport.get('readerSource')
        context = self.load(pool.get('context'), 'private/reader-context.json', 32 << 10)
        context_bound = (type(context) is dict and context.get('kind') == 'native-scan-http-capacity-reader-context' and
                         context.get('version') == 1 and context.get('scope') == str(self.saved.root) and
                         context.get('controller') == pool.get('controller') and
                         object_value(context.get('controller')).get('pid') == execution.get('sourceProcess') and
                         shared and context.get('bootId') == domains[0]['bootId'])
        source_pins_bound = (reader_source is not None and reader_source == pool.get('source') and
                            context_bound and
                            self.load(reader_source, 'private/capacity-reader.py', 256 << 10, raw=True) is not None and
                            self.load(transport.get('source'), 'private/capacity-transport.py', 256 << 10, raw=True) is not None)
        decoded, previous_sequence = {}, 0
        for observation in observations:
            phase, position = observation['phase'], observation['position']
            label = 'scan-state-' + phase + '-' + position
            need(observation.get('label') == label, 'running_job_observation_label')
            detail_state = observation.get('detailState')
            need(type(observation.get('detailPollSlot')) is int and
                 observation['detailPollSlot'] in (1, 2, 3) and detail_state in ('pending', 'running', 'completed') and
                 observation.get('collectionPoint') == ('terminal-detail' if detail_state == 'completed' else 'periodic-detail'),
                 'running_job_collection_point')
            event_path, _ = self.numbered_record(observation.get('record'), 'private/controller-events', 4, 4096, '.json')
            event = self.load(observation.get('record'), event_path, 64 << 10)
            control_pin = observation.get('controlReceipt')
            need(control_pin in receipts, 'running_job_transport_pin_not_registered')
            control_path, sequence = self.numbered_record(control_pin, 'private/control-http', 3, 360, '-receipt.json')
            need(sequence > previous_sequence, 'running_job_transport_sequence')
            previous_sequence = sequence
            control = self.load(control_pin, control_path, 32 << 10)
            if event is None or control is None:
                continue
            need(event.get('event') == 'scan-job-state-observation' and event.get('scope') == str(self.saved.root) and
                 event.get('phase') == phase and event.get('observation') ==
                 {key: value for key, value in observation.items() if key != 'record'}, 'running_job_event_binding')
            need(control.get('kind') == 'native-scan-http-capacity-control-http' and control.get('version') == 1 and
                 control.get('scope') == str(self.saved.root) and control.get('sequence') == sequence and
                 control.get('label') == label and control.get('phase') == phase and control.get('bucket') == 'task-poll' and
                 control.get('method') == 'GET' and control.get('path') == '/admin/v1/jobs' and control.get('route') == 'jobs' and
                 control.get('source') == transport.get('source') and control.get('readerSource') == reader_source,
                 'running_job_http_binding')
            completed = (control.get('error') is None and control.get('status') == 200 and
                         all(control.get(key) is True for key in ('dispatched', 'requestFullySent', 'bodyComplete',
                             'connectionClosed', 'responseClosed', 'ownershipBefore', 'ownershipAfter')) and
                         all(control.get(key) == [] for key in ('postOwnershipErrors', 'persistenceErrors',
                             'connectionCloseErrors', 'responseCloseErrors')) and
                         object_value(control.get('rawFraming')).get('passed') is True)
            need(completed, 'running_job_http_not_complete')
            before, after = observation.get('beforeMonotonicNs'), observation.get('afterMonotonicNs')
            start, body, closed = (control.get(key) for key in
                                  ('dispatchMonotonicNs', 'bodyCompleteMonotonicNs', 'connectionCloseMonotonicNs'))
            need(all(integer(number, 1) for number in (before, after, start, body, closed)) and
                 before <= start <= body <= closed <= after, 'running_job_observation_times')
            raw_complete = True
            payload = None
            for field, suffix, bound in (('rawHeader', '-response-header.raw', 65536),
                                        ('rawWireBody', '-response-wire-body.raw', (1 << 20) + 65536),
                                        ('body', '-response-body.raw', 1 << 20)):
                raw = self.load(control.get(field), 'private/control-http/%03d' % sequence + suffix, bound, raw=True)
                raw_complete = raw_complete and raw is not None
                if field == 'body' and raw is not None:
                    payload = parse_json(raw)
            if not raw_complete:
                continue
            jobs = observation.get('jobs')
            need(type(jobs) is dict and len(jobs) <= (2 if phase == 'cold' else 4) and
                 type(payload) is dict and set(payload) == {'Items', 'TotalRecordCount'} and
                 type(payload['Items']) is list and type(payload['TotalRecordCount']) is int and
                 payload['TotalRecordCount'] == len(payload['Items']) == len(jobs) and
                 all(type(row) is dict and row.get('Id') in jobs and jobs[row['Id']] == row for row in payload['Items']) and
                 len({row['Id'] for row in payload['Items']}) == len(jobs), 'running_job_body_binding')
            current = lifecycle.get(phase)
            if current is None:
                self.gaps.add('running_job_final_lifecycle_missing:' + phase)
                continue
            need(observation.get('runId') == current['runId'] and
                 observation.get('taskId') == current['run'].get('task_id') and
                 observation.get('requestId') == current['run'].get('request_id'), 'running_job_run_binding')
            children = observation.get('children')
            need(type(children) is list and len(children) == 2 and
                 {object_value(row).get('Id') for row in children} == set(current['children']),
                 'running_job_child_inventory')
            for child in children:
                final = current['children'][child['Id']]
                need(child.get('RunId') == current['runId'] and child.get('LibraryId') == final.get('library_id') and
                     child.get('Ordinal') == final.get('ordinal') and
                     (child.get('ScanJobId') is None or child['ScanJobId'] == final.get('scan_job_id')),
                     'running_job_child_binding')
            allowed = dict(current['scanJobs'])
            if phase == 'cached' and 'cold' in lifecycle:
                allowed.update(lifecycle['cold']['scanJobs'])
                need(set(lifecycle['cold']['scanJobs']) <= set(jobs), 'running_job_retained_history_missing')
            need(set(jobs) <= set(allowed), 'running_job_foreign_job')
            for job_id, job in jobs.items():
                final = allowed[job_id]
                need(type(job) is dict and set(job) == {'Id', 'LibraryId', 'ForceProbe', 'Status', 'Error',
                     'Scanned', 'Added', 'Updated', 'CreatedAt', 'StartedAt', 'FinishedAt'} and job.get('Id') == job_id and
                     job.get('LibraryId') == final.get('library_id') and job.get('ForceProbe') is False and
                     timestamp_ns(job.get('CreatedAt')) == timestamp_ns(final.get('created_at')) and
                     job.get('Status') in ('pending', 'running', 'completed', 'failed', 'cancelled', 'interrupted') and
                     all(integer(job.get(key)) and job[key] <= final.get(column, -1) for key, column in
                         (('Scanned', 'scanned'), ('Added', 'added'), ('Updated', 'updated'))),
                     'running_job_identity_or_progress')
                status = job['Status']
                if status == 'pending':
                    need(job.get('StartedAt') is None and job.get('FinishedAt') is None, 'running_job_pending_times')
                else:
                    need(timestamp_ns(job.get('StartedAt')) == timestamp_ns(final.get('started_at')),
                         'running_job_started_identity')
                    if status == 'running':
                        need(job.get('FinishedAt') is None and job.get('Error') == '', 'running_job_active_shape')
                    else:
                        need(status == final.get('status', '').lower() and
                             timestamp_ns(job.get('FinishedAt')) == timestamp_ns(final.get('finished_at')) and
                             job.get('Error') == final.get('error') and
                             all(job[key] == final[column] for key, column in
                                 (('Scanned', 'scanned'), ('Added', 'added'), ('Updated', 'updated'))),
                             'running_job_terminal_changed')
            domain_bound = (shared and source_pins_bound and
                            same_clock_domains(domains[0], control.get('clockDomainBefore'), control.get('clockDomainAfter')))
            decoded[(phase, position)] = {'observation': observation, 'control': control,
                                         'domainBound': domain_bound, 'jobs': jobs}
            result['observationCount'] += 1
        intervals = {}
        for phase in PHASES:
            first, second = (decoded.get((phase, position)) for position in ('first', 'second'))
            if first is None or second is None:
                self.gaps.add('running_job_observation_pair_missing:' + phase)
                continue
            start_record = object_value(object_value(pool.get('phases')).get(phase)).get('start')
            start_record = object_value(start_record)
            event = self.load(start_record.get('source'), 'private/readers/' + phase + '/start.json', 4096)
            if event is None:
                continue
            need(event == start_record.get('event') and event.get('kind') == 'native-scan-http-capacity-reader-start' and
                 event.get('version') == 1 and event.get('scope') == str(self.saved.root) and
                 event.get('phase') == phase and event.get('runId') == lifecycle[phase]['runId'] and
                 event.get('inputs') == {side: object_value(object_value(pool['phases'][phase].get('inputs')).get(side)).get('sha256')
                                        for side in READERS}, 'running_job_start_binding')
            start_before, _, start_after = anchor_interval(event.get('anchor'))
            need(start_before <= start_after <= first['observation']['beforeMonotonicNs'] and
                 first['observation']['afterMonotonicNs'] <= second['observation']['beforeMonotonicNs'],
                 'running_job_observation_after_start')
            first_observation, second_observation = first['observation'], second['observation']
            if first_observation['detailState'] == 'completed':
                need(first_observation['detailPollSlot'] in (1, 2) and
                     second_observation['detailState'] == 'completed' and
                     second_observation['detailPollSlot'] == first_observation['detailPollSlot'],
                     'running_job_terminal_collection_slots')
            else:
                need(first_observation['detailPollSlot'] == 2 and second_observation['detailPollSlot'] == 3,
                     'running_job_periodic_collection_slots')
            need(set(first['jobs']) <= set(second['jobs']), 'running_job_disappeared')
            for job_id, earlier in first['jobs'].items():
                later = second['jobs'][job_id]
                status = earlier['Status']
                need(status == 'pending' or status == 'running' and later['Status'] != 'pending' or
                     status not in ('pending', 'running') and earlier == later, 'running_job_state_regression')
                need(all(earlier[key] <= later[key] for key in ('Scanned', 'Added', 'Updated')),
                     'running_job_progress_regression')
            result['pairedPhases'] += 1
            if not first['domainBound'] or not second['domainBound']:
                self.gaps.add('running_job_clock_or_source_binding_missing:' + phase)
                continue
            if first_observation['detailState'] == 'completed' or second_observation['detailState'] == 'completed':
                self.gaps.add('terminal_scan_observation_without_running_proof:' + phase)
                continue
            lower = first['control']['bodyCompleteMonotonicNs']
            upper = second['control']['dispatchMonotonicNs']
            need(lower <= upper, 'running_job_interval_order')
            for job_id, job in first['jobs'].items():
                if job_id not in lifecycle[phase]['scanJobs']:
                    continue
                later = second['jobs'][job_id]
                if job['Status'] == later['Status'] == 'running' and lower < upper:
                    intervals[(phase, job['LibraryId'])] = [(lower, upper)]
                    result['intervals'].append({'phase': phase, 'jobId': job_id, 'libraryId': job['LibraryId'],
                                               'guaranteedMonotonicIntervalNs': [lower, upper]})
            if not any(key[0] == phase for key in intervals):
                self.gaps.add('no_paired_running_job:' + phase)
        if len(observations) != 4:
            self.gaps.add('four_scan_job_observations_not_available')
        if not intervals:
            self.gaps.add(OVERLAP_GAP)
        result['clockDomainEstablished'] = (shared and result['observationCount'] == 4 and
                                             all(value['domainBound'] for value in decoded.values()))
        result['status'] = ('recorded_running_intervals' if intervals else 'incomplete')
        return intervals, result, domains

    def terminal_bound(self, phase, record, lifecycle):
        cancel = object_value(object_value(record.get('result')).get('cancel'))
        if not cancel:
            return None
        event = self.load(cancel.get('source'), 'private/readers/' + phase + '/cancel.json', 4096)
        if event is None:
            return None
        need(event == cancel.get('event') and event.get('kind') == 'native-scan-http-capacity-reader-cancel' and
             event.get('version') == 1 and event.get('scope') == str(self.saved.root) and event.get('phase') == phase and
             event.get('runId') == record.get('runId') and
             type(event.get('inputs')) is dict and set(event['inputs']) == set(READERS) and
             all(type(digest) is str and re.fullmatch('[0-9a-f]{64}', digest) for digest in event['inputs'].values()) and
             event.get('inputs') == {side: object_value(object_value(record.get('inputs')).get(side)).get('sha256')
                                     for side in READERS}, 'reader_cancel_binding')
        _, _, after = anchor_interval(event.get('anchor'))
        self.anchors.append(event['anchor'])
        if event.get('reason') == 'task_terminal' and lifecycle.get('runId') == record.get('runId'):
            return after
        return None

    def readers(self, pool, lifecycle, running=None, domains=()):
        running = {} if running is None else running
        phases = object_value(pool.get('phases'))
        need(set(phases) <= set(PHASES), 'reader_phase_contract')
        samples, receipts = [], 0
        for phase in PHASES:
            record = object_value(phases.get(phase))
            if phase in lifecycle:
                need(record.get('runId') == lifecycle[phase]['runId'], 'reader_scan_run_binding')
            workers = record.get('workers', [])
            need(type(workers) is list and len(workers) <= 2, 'reader_worker_bound')
            sides = [object_value(row).get('reader') for row in workers]
            need(set(sides) <= set(READERS) and len(set(sides)) == len(sides), 'reader_worker_identity')
            terminal = self.terminal_bound(phase, record, lifecycle.get(phase, {}))
            for side in READERS:
                worker = next((row for row in workers if row['reader'] == side), {})
                root = 'private/readers/' + phase + '/' + side + '/'
                report = self.load(worker.get('readerReceipt'), root + 'receipt.json', 8 << 20)
                if report is None:
                    continue
                need(type(report) is dict and report.get('kind') == 'native-scan-http-capacity-reader-receipt' and
                     report.get('version') == 1 and report.get('scope') == str(self.saved.root) and
                     report.get('phase') == phase and report.get('reader') == side and
                     report.get('runId') == record.get('runId') and
                     report.get('userId') == worker.get('userId') and report.get('libraryId') == worker.get('libraryId'),
                     'reader_receipt_binding')
                if worker.get('readerReport') is not None:
                    need(report == worker['readerReport'], 'reader_report_readback_mismatch')
                requests = report.get('requests')
                need(type(requests) is list and len(requests) <= 60 and
                     [object_value(row).get('sequence') for row in requests] == list(range(1, len(requests) + 1)),
                     'reader_request_sequence')
                receipts += 1
                self.anchors_from(report)
                current_run = object_value(lifecycle.get(phase)).get('run', {})
                provenance_bound = (report.get('source') is not None and report.get('source') == pool.get('source') and
                    report.get('context') is not None and report.get('context') == pool.get('context') and
                    report.get('input') == object_value(record.get('inputs')).get(side) and
                    report.get('start') is not None and report.get('start') == record.get('start') and
                    report.get('taskId') == current_run.get('task_id') and
                    report.get('requestId') == current_run.get('request_id') and
                    report.get('parentPid') == object_value(pool.get('controller')).get('pid'))
                domain_bound = (provenance_bound and
                    same_clock_domains(*domains, report.get('clockDomainBefore'), report.get('clockDomainAfter')))
                if not domain_bound:
                    self.gaps.add('reader_clock_domain_not_established:' + phase + ':' + side)
                if not requests:
                    self.gaps.add('empty_reader:' + phase + ':' + side)
                for row in requests:
                    sequence = row['sequence']
                    need(row.get('phase') == phase and row.get('reader') == side and row.get('runId') == report['runId'] and
                         row.get('shape') == ('page' if sequence % 2 else 'count') and row.get('method') == 'GET',
                         'request_scope_or_shape')
                    saved = self.load(row.get('receipt'), root + '%03d-response.json' % sequence)
                    complete = saved is not None
                    if saved is not None:
                        need(saved == {key: value for key, value in row.items() if key != 'receipt'},
                             'request_receipt_readback_mismatch')
                    for field, suffix, bound in (('intent', 'intent.json', 32768),
                                                ('rawHeader', 'response-header.raw', 32768),
                                                ('rawWireBody', 'response-wire-body.raw', 576 << 10),
                                                ('body', 'response-body.raw', 512 << 10)):
                        raw = self.load(row.get(field), root + '%03d-' % sequence + suffix, bound, raw=True)
                        complete = complete and raw is not None
                    self.anchors_from(row)
                    metrics = request_metrics(row, complete)
                    guaranteed = running.get((phase, report['libraryId']), []) if domain_bound and complete else []
                    metrics.update(phase=phase, reader=side, shape='page64' if row['shape'] == 'page' else 'count',
                        overlap=classify_overlap(metrics['interval'], guaranteed=guaranteed,
                            possible=[(None, terminal)] if terminal and domain_bound and complete else None),
                        runningIntersection=running_intersection(metrics['interval'], guaranteed),
                        provenRunningIntervals=guaranteed)
                    samples.append(metrics)
                need(report.get('dispatchedRequests') == sum(row.get('dispatched') is True for row in requests),
                     'reader_dispatch_count_mismatch')
        if receipts != 4:
            self.gaps.add('four_reader_receipts_not_available')
        return samples, receipts

    def resources(self, observers):
        refs = observers.get('samples', [])
        need(type(refs) is list and len(refs) <= 600, 'resource_sample_bound')
        samples = []
        for sequence, reference in enumerate(refs, 1):
            need(reference.get('sequence') == sequence, 'resource_sequence')
            sample = self.load(reference.get('receipt'),
                               'private/observers/metrics/sample-%03d.json' % sequence, 48 << 10)
            if sample is None:
                continue
            need(sample.get('kind') == 'native-capacity-resource-sample' and sample.get('sequence') == sequence and
                 sample.get('status') == reference.get('status') and
                 integer(sample.get('beforeMonotonicNs'), 1) and
                 integer(sample.get('afterMonotonicNs'), sample['beforeMonotonicNs']) and
                 all(sample.get(key) == reference.get(key) for key in ('beforeMonotonicNs', 'afterMonotonicNs')),
                 'resource_sample_binding')
            samples.append(sample)
        if not samples:
            self.gaps.add('resource_samples_not_available')
        result = resource_summary(samples)
        if samples and result['completeSamples'] != len(samples):
            self.gaps.add('resource_samples_partial')
        for role, values in result['roles'].items():
            if (not values['mainProcessRssSamples'] or not values['memoryCurrentSamples'] or
                    not values['memoryPeakSamples'] or not values['cgroupCpuUsageObservations']):
                self.gaps.add('resource_fields_unavailable:' + role)
        return result

    def costs(self, execution):
        result = cost_summary(execution.get('measurementCosts'))
        if result['status'] == 'unavailable':
            self.gaps.add('measurement_costs_not_recorded')
            return result
        stages = execution['measurementCosts']['stages']
        pins = execution.get('measurementCostReceipts', [])
        need(type(pins) is list and len(pins) <= len(stages), 'measurement_cost_receipt_bound')
        expected = {str(self.saved.root / ('private/controller-costs-%02d.json' % index)): index
                    for index in range(1, len(stages) + 1)}
        paths = [object_value(pin).get('path') for pin in pins]
        need(all(type(path) is str and path in expected for path in paths) and len(set(paths)) == len(paths),
             'measurement_cost_receipt_paths')
        for index in range(1, len(stages) + 1):
            pin = next((pin for pin in pins if expected[pin['path']] == index), None)
            value = self.load(pin, 'private/controller-costs-%02d.json' % index, 32 << 10)
            if value is not None:
                need(value == dict(execution['measurementCosts'], stages=stages[:index]),
                     'measurement_cost_receipt_mismatch')
        if len(pins) != len(stages):
            self.gaps.add('measurement_cost_snapshots_incomplete')
        return result

    def analyze(self, pin, relative='execution.json'):
        need(relative in ('execution.json', 'private/controller-result-fallback.json'), 'execution_path_contract')
        execution = self.load(pin, relative, 16 << 20)
        need(type(execution) is dict and execution.get('kind') == 'native-scan-http-capacity-execution' and
             execution.get('version') == 1 and execution.get('scope') == str(self.saved.root), 'execution_contract')
        pool = self.component(execution, 'reader-pool-before-preservation')
        if pool:
            need(pool.get('kind') == 'native-capacity-reader-pool' and pool.get('version') == 1 and
                 pool.get('scope') == str(self.saved.root), 'reader_pool_record_binding')
        commands = self.component(execution, 'final-command-reports')
        closure = self.closure(execution, pool, commands)
        report = {'kind': 'native-scan-http-capacity-result-analysis', 'version': 1,
                  'execution': dict(self.saved._seen[relative]), 'resourceClosure': closure,
                  'productChecks': {'status': 'not_analyzed'},
                  'measurement': {'status': 'incomplete'}, 'capacityAccepted': False, 'wholeM2Accepted': False,
                  'limits': ['Saved evidence only; no live closure re-observation.',
                             'No idle control: no slowdown, SLO, saturation, or throughput acceptance inference.',
                             'Cold means initial catalog state; it does not establish cold operating-system caches.',
                             'Private raw HTTP data is hash checked and never included in this report.']}
        if not closure['physicalClosureRecorded']:
            self.gaps.add('physical_resource_closure_not_established')
        else:
            workload = self.component(execution, 'workload-before-preservation')
            observers = self.component(execution, 'observers-before-preservation')
            if observers:
                need(observers.get('kind') == 'native-scan-http-capacity-observers' and
                     observers.get('version') == 1 and observers.get('scope') == str(self.saved.root),
                     'observer_record_binding')
            payloads, sql_pass = self.sql(observers)
            lifecycle, scans = self.scan_lifecycle(workload, payloads)
            running, running_summary, domains = self.running_observations(execution, workload, pool, lifecycle)
            samples, receipt_count = self.readers(pool, lifecycle, running, domains)
            clocks = clock_summary(self.anchors)
            offset = clocks['observedOffsetEnvelopeNs']
            for scan in scans:
                scan['conditionalMappingUsingObservedOffsetEnvelopeNs'] = (
                    [[start - offset[1], end - offset[0]] for start, end in scan['recordedJobWallIntervalsNs']]
                    if offset else None)
                scan['conditionalMappingIsOverlapProof'] = False
            result = object_value(workload.get('result'))
            cleanup = object_value(workload.get('cleanup'))
            final = object_value(workload.get('finalValidation'))
            credentials = cleanup.get('credentials', [])
            checks = {'workloadRecordedPass': result.get('status') == 'workload-passed-pending-closure',
                      'credentialClosureRecorded': cleanup.get('status') == 'closed' and cleanup.get('failures') == [] and
                          type(credentials) is list and len(credentials) == 4 and
                          all(row.get('logout204') is True and row.get('rejected401') is True for row in credentials),
                      'finalValidationRecorded': final.get('status') == 'normal-workload-and-credential-closure-checked' and
                          all(final.get(key) == value for key, value in (('mediaLeaves', 1000), ('favoriteRows', 2),
                              ('sessionsRevoked', 4), ('runCount', 2), ('childCount', 4), ('scanJobCount', 4))),
                      'sixSqlObservationsAccepted': sql_pass,
                      'twoScanLifecyclesReconciled': len(lifecycle) == 2,
                      'readerPoolRecordedPass': len(pool.get('phases', {})) == 2 and
                          all(row.get('passed') is True for row in pool.get('phases', {}).values()),
                      'readerEvidenceAvailable': receipt_count == 4 and bool(samples) and
                          all(any(row['phase'] == phase and row['reader'] == side for row in samples)
                              for phase in PHASES for side in READERS) and
                          not any(row['outcome'] == 'evidenceUnavailable' for row in samples),
                      'readerResponsesRecordedValid': any(row['dispatched'] for row in samples) and
                          all(row['outcome'] in ('validSuccess', 'notDispatched') for row in samples),
                      'controllerRecordedNoFailures': execution.get('failures') == []}
            report['productChecks'] = {'status': 'recorded_pass' if all(checks.values()) else 'incomplete_or_recorded_failure',
                                       'checks': checks, 'fullProductContractIndependentlyReexecuted': False}
            for phase in PHASES:
                for side in READERS:
                    for shape in SHAPES:
                        if not any(row['phase'] == phase and row['reader'] == side and row['shape'] == shape and
                                   row['dispatched'] for row in samples):
                            self.gaps.add('missing_reader_shape:' + phase + ':' + side + ':' + shape)
                        if not any(row['phase'] == phase and row['reader'] == side and row['shape'] == shape and
                                   row['overlap'] == 'demonstrated' for row in samples):
                            self.gaps.add('no_demonstrated_running_overlap:' + phase + ':' + side + ':' + shape)
            report['measurement'] = {'status': 'incomplete', 'readerReceipts': receipt_count,
                'fixedReaderRequestCeiling': 240, 'unusedAllowanceCountedAsRequests': False,
                'intentRecords': len(samples), 'dispatchedRequests': sum(row['dispatched'] for row in samples),
                'groups': summarize_groups(samples), 'readerCoverage': coverage(samples), 'scanLifecycles': scans,
                'runningJobOverlap': running_summary,
                'clockAnchors': clocks, 'resources': self.resources(observers), 'observerCosts': self.costs(execution),
                'latencyDefinition': 'Reader dispatch begins before connect; quantiles require complete, successful, correctly recorded responses.',
                'quantileMethod': 'Median of ordered observations; p95 uses nearest rank ceil(0.95*n).',
                'controlHttpIncludedInReaderLatency': False, 'guaranteedScanActiveIntervalsAvailable': bool(running),
                'overlapBasis': 'same_reader_library_persisted_running_job', 'filesystemOrFfprobeOverlapProven': False}
        report['measurement']['coverageGaps'] = sorted(self.gaps)
        if report['productChecks']['status'] != 'not_analyzed' and not self.gaps:
            report['measurement']['status'] = 'recorded_with_scope_limits'
        report['evidenceReadback'] = {'files': self.saved.pins, 'bytesRead': self.saved.total_bytes,
                                      'maximumBytes': MAX_INPUT_BYTES}
        return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execution', required=True)
    parser.add_argument('--execution-sha256', required=True)
    args = parser.parse_args()
    try:
        need(sys.platform == 'linux' and os.geteuid() == 0 and sys.flags.isolated == 1 and
             sys.flags.dont_write_bytecode == 1, 'linux_root_isolated_reader_required')
        relative = ('execution.json' if args.execution == str(SCOPE / 'execution.json') else
                    'private/controller-result-fallback.json')
        need(args.execution == str(SCOPE / relative), 'fixed_execution_path_required')
        report = ResultReader(SavedScope(SCOPE)).analyze(
            {'path': args.execution, 'sha256': args.execution_sha256}, relative)
        print(json.dumps(report, sort_keys=True, separators=(',', ':'), allow_nan=False))
        return 0
    except EvidenceError as error:
        print(json.dumps({'kind': 'native-scan-http-capacity-result-analysis', 'version': 1,
                          'status': 'evidence_rejected', 'code': str(error), 'capacityAccepted': False}))
        return 1
    except (KeyError, TypeError, ValueError, AttributeError, OverflowError, RecursionError):
        print(json.dumps({'kind': 'native-scan-http-capacity-result-analysis', 'version': 1,
                          'status': 'evidence_rejected', 'code': 'unsupported_record_shape', 'capacityAccepted': False}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
