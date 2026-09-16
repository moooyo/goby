"""Synthetic saved-record contracts; these tests run no native workload."""

import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location('capacity_result_reader',
                                             Path(__file__).with_name('capacity-result-reader.py'))
READER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(READER)


def anchor(before, wall=1_800_000_000_000_000_000, width=5):
    return {'monotonicBeforeNs': before, 'wallTimeNs': wall + before,
            'monotonicAfterNs': before + width}


def request(sequence=1, start=1000):
    return {'sequence': sequence, 'phase': 'cold', 'reader': 'left', 'runId': '1' * 32,
            'shape': 'page' if sequence % 2 else 'count', 'method': 'GET',
            'dispatched': True, 'dispatchMonotonicNs': start, 'headersMonotonicNs': start + 20,
            'bodyCompleteMonotonicNs': start + 30, 'connectionCloseMonotonicNs': start + 40,
            'status': 200, 'error': None, 'bodyComplete': True, 'connectionClosed': True,
            'correctness': {'passed': True}, 'dispatchAnchor': anchor(start),
            'bodyCompleteAnchor': anchor(start + 30)}


def costs(phases=('business', 'readiness', 'business')):
    return {'version': 1, 'clock': 'time.monotonic_ns', 'accounting': 'inclusive_non_additive',
            'stages': [{'phase': phase, 'startedMonotonicNs': 100 + index * 100,
                        'finishedMonotonicNs': 200 + index * 100,
                        'operations': {name: {'calls': 2, 'elapsedNs': 80, 'failures': 0}
                                       for name in READER.COST_OPERATIONS}}
                       for index, phase in enumerate(phases)]}


class SavedFixture:
    """Small synthetic records, deliberately not a product acceptance fixture."""

    def __init__(self, root):
        self.root = root
        self.execution = {'kind': 'native-scan-http-capacity-execution', 'version': 1,
            'scope': str(root), 'componentReceipts': {}, 'allOwnedCommandsClosed': True,
            'lockReleased': True, 'protectedBefore': {'synthetic': True},
            'protectedAfter': {'synthetic': True}, 'failures': [],
            'closure': {'application': {'physicalClosed': True},
                'closedUnits': {name: {'physicalClosed': True} for name in READER.UNITS},
                'privateNamespaceGone': True, 'status': 'owned_resources_preserved_and_closed',
                'errors': [], 'missing': [],
                'archives': {name: {'readbackMatched': True} for name in ('postgres', 'evidence')},
                **{key: True for key in ('preservationComplete', 'postgresUnmounted',
                    'unitRegistrationsAbsent', 'normalInfrastructureStops', 'fixtureApplicationPathsAbsent',
                    'evidenceSourcePostflightMatched', 'sharedAccountsUnchanged')}}}
        self.pool = {'kind': 'native-capacity-reader-pool', 'version': 1, 'scope': str(root),
                     'childrenCreated': 4, 'phases': {}}
        self.observers = {'kind': 'native-scan-http-capacity-observers', 'version': 1, 'scope': str(root),
                          'failed': False, 'sql': [], 'samples': []}
        self.workload = {'result': {'status': 'workload-passed-pending-closure', 'runs': {}},
            'cleanup': {'status': 'closed', 'failures': [],
                        'credentials': [{'logout204': True, 'rejected401': True} for _ in range(4)]},
            'finalValidation': {'status': 'normal-workload-and-credential-closure-checked',
                'mediaLeaves': 1000, 'favoriteRows': 2, 'sessionsRevoked': 4,
                'runCount': 2, 'childCount': 4, 'scanJobCount': 4}}
        self.tables = {'task_runs': [], 'task_run_children': [], 'scan_jobs': []}
        for phase_index, phase in enumerate(READER.PHASES, 1):
            run_id = '%032x' % phase_index
            inputs = {side: {'path': str(root / 'private/readers' / phase / (side + '-input.json')),
                             'sha256': str(side_index) * 64}
                      for side_index, side in enumerate(READER.READERS, 1)}
            event = {'kind': 'native-scan-http-capacity-reader-cancel', 'version': 1,
                     'scope': str(root), 'phase': phase, 'runId': run_id, 'reason': 'task_terminal',
                     'inputs': {side: pin['sha256'] for side, pin in inputs.items()},
                     'anchor': anchor(2000 + phase_index * 10000)}
            cancel = {'event': event, 'source': self.save('private/readers/' + phase + '/cancel.json', event)}
            phase_row = {'runId': run_id, 'passed': True, 'inputs': inputs,
                         'result': {'cancel': cancel}, 'workers': []}
            self.pool['phases'][phase] = phase_row
            self.tables['task_runs'].append({'id': run_id, 'task_key': 'library.scan', 'state': 'completed'})
            detail = {'Run': {'Id': run_id, 'State': 'completed'}, 'Children': {'Items': []}}
            self.workload['result']['runs'][phase] = detail
            for side_index, side in enumerate(READER.READERS, 1):
                child_id = '%032x' % (phase_index * 10 + side_index)
                job_id = '%032x' % (phase_index * 100 + side_index)
                library_id = '%032x' % (1000 + side_index)
                user_id = '%032x' % (2000 + side_index)
                self.tables['task_run_children'].append({'id': child_id, 'run_id': run_id,
                    'scan_job_id': job_id, 'library_id': library_id, 'state': 'completed'})
                detail['Children']['Items'].append({'Id': child_id, 'ScanJobId': job_id, 'LibraryId': library_id})
                self.tables['scan_jobs'].append({'id': job_id, 'task_child_id': child_id, 'library_id': library_id,
                    'status': 'Completed', 'scanned': 500, 'added': 500 if phase == 'cold' else 0, 'updated': 0,
                    'created_at': '2026-09-15T00:00:00+00:00', 'started_at': '2026-09-15T00:00:01+00:00',
                    'finished_at': '2026-09-15T00:00:02+00:00'})
                prefix = 'private/readers/' + phase + '/' + side + '/'
                requests = []
                for sequence in (1, 2):
                    # The first request precedes cancellation; the second is
                    # after terminal publication, so it has a causal exclusion.
                    start = phase_index * 10000 + (1000 if sequence == 1 else 3000)
                    row = request(sequence, start)
                    row.update(phase=phase, reader=side, runId=run_id)
                    row['intent'] = self.save(prefix + '%03d-intent.json' % sequence, copy.deepcopy(row))
                    for field, suffix, raw in (('rawHeader', 'response-header.raw', b'HTTP/1.1 200 OK\r\n\r\n'),
                                              ('rawWireBody', 'response-wire-body.raw', b'{}'),
                                              ('body', 'response-body.raw', b'{}')):
                        row[field] = self.save(prefix + '%03d-' % sequence + suffix, raw)
                    row['receipt'] = self.save(prefix + '%03d-response.json' % sequence, copy.deepcopy(row))
                    requests.append(row)
                report = {'kind': 'native-scan-http-capacity-reader-receipt', 'version': 1,
                    'scope': str(root), 'phase': phase, 'reader': side, 'runId': run_id,
                    'userId': user_id, 'libraryId': library_id, 'requests': requests, 'dispatchedRequests': 2}
                phase_row['workers'].append({'reader': side, 'userId': user_id, 'libraryId': library_id,
                    'created': True, 'joined': True, 'groupEmptyAfterWait': True, 'wait': {'exited': True},
                    'readerReport': report, 'readerReceipt': self.save(prefix + 'receipt.json', report)})
        for slot in READER.SQL_SLOTS:
            payload = {'kind': 'capacity-observation', 'slot': slot}
            if slot != 'prestart-identity':
                payload['tables'] = self.tables if slot == 'final-after-stop' else {}
            self.observers['sql'].append({'slot': slot, 'status': 'passed', 'payloadAccepted': True,
                'frontendClosed': True, 'backendGone': True, 'commandExitCode': 0,
                'parsed': self.save('private/observers/sql/' + slot + '-parsed.json', payload)})
            self.observers['sql'][-1]['receipt'] = self.save(
                'private/observers/sql/' + slot + '-receipt.json', copy.deepcopy(self.observers['sql'][-1]))
        for sequence, start in enumerate((100, 2_000_000_100, 7_000_000_100), 1):
            sample = {'kind': 'native-capacity-resource-sample', 'sequence': sequence, 'status': 'complete',
                'beforeMonotonicNs': start, 'afterMonotonicNs': start + 100,
                'processes': {'application': {'status': 'sampled', 'rssBytes': 4096}}, 'cgroups': {}}
            self.observers['samples'].append({key: sample[key] for key in
                                              ('sequence', 'status', 'beforeMonotonicNs', 'afterMonotonicNs')})
            self.observers['samples'][-1]['receipt'] = self.save(
                'private/observers/metrics/sample-%03d.json' % sequence, sample)
        self.execution['measurementCosts'] = costs()
        self.execution['measurementCostReceipts'] = [self.save('private/controller-costs-%02d.json' % index,
            dict(self.execution['measurementCosts'], stages=self.execution['measurementCosts']['stages'][:index]))
            for index in range(1, 4)]
        self.component('final-command-reports', {name: {'allOwnedCommandsClosed': True, 'commands': [{'closed': True}]}
                                                for name in ('admissionAndRuntime', 'preparation', 'closure')})

    def save(self, relative, value):
        path = self.root / relative
        path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        current = path.parent
        while current != self.root:
            current.chmod(0o700)
            current = current.parent
        raw = value if type(value) is bytes else json.dumps(value, sort_keys=True).encode('utf-8')
        path.write_bytes(raw)
        path.chmod(0o600)
        return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}

    def component(self, name, value):
        self.execution['componentReceipts'][name] = self.save('private/' + name + '.json', value)

    def analyze(self):
        self.component('reader-pool-before-preservation', self.pool)
        self.component('workload-before-preservation', self.workload)
        self.component('observers-before-preservation', self.observers)
        pin = self.save('execution.json', self.execution)
        return READER.ResultReader(READER.SavedScope(self.root, owner=os.getuid())).analyze(pin)


class ResultReaderContracts(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='capacity-result-contract-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.root.chmod(0o700)

    def test_layered_saved_result_never_promotes_sampled_clocks_to_overlap(self):
        fixture = SavedFixture(self.root)
        result = fixture.analyze()
        self.assertEqual(result['resourceClosure']['status'], 'recorded_closed_and_preserved')
        self.assertEqual(result['productChecks']['status'], 'recorded_pass')
        measurement = result['measurement']
        self.assertEqual(measurement['status'], 'incomplete')
        self.assertIn(READER.OVERLAP_GAP, measurement['coverageGaps'])
        self.assertEqual(measurement['intentRecords'], 8)
        self.assertEqual(len(measurement['groups']), 12)
        totals = {name: sum(row['intentRecords'] for row in measurement['groups'] if row['overlap'] == name)
                  for name in READER.OVERLAPS}
        self.assertEqual(totals, {'demonstrated': 0, 'none': 4, 'indeterminate': 4})
        self.assertEqual(measurement['readerCoverage'][0]['simultaneousClosedHttpIntervalPairs'], 2)
        self.assertIsNone(measurement['readerCoverage'][0]['simultaneousHttpDuringProvenScan'])
        self.assertTrue(measurement['scanLifecycles'][0]['conditionalMappingUsingObservedOffsetEnvelopeNs'])
        self.assertFalse(measurement['scanLifecycles'][0]['conditionalMappingIsOverlapProof'])
        self.assertFalse(result['capacityAccepted'])
        self.assertFalse(result['wholeM2Accepted'])
        self.assertNotIn('HTTP/1.1', json.dumps(result))

    def test_incomplete_physical_closure_does_not_read_request_or_sql_files(self):
        fixture = SavedFixture(self.root)
        fixture.execution['lockReleased'] = False
        result = fixture.analyze()
        self.assertEqual(result['productChecks']['status'], 'not_analyzed')
        paths = [pin['path'] for pin in result['evidenceReadback']['files']]
        self.assertFalse(any('/readers/' in path or '/observers/' in path for path in paths))

    def test_preservation_failure_is_separate_from_measurement(self):
        fixture = SavedFixture(self.root)
        fixture.execution['closure']['archives']['evidence']['readbackMatched'] = False
        result = fixture.analyze()
        self.assertTrue(result['resourceClosure']['physicalClosureRecorded'])
        self.assertEqual(result['resourceClosure']['status'], 'incomplete')
        self.assertEqual(result['measurement']['dispatchedRequests'], 8)

    def test_empty_reader_is_a_coverage_gap_and_not_a_product_pass(self):
        fixture = SavedFixture(self.root)
        worker = fixture.pool['phases']['cold']['workers'][0]
        report = worker['readerReport']
        report.update(requests=[], dispatchedRequests=0)
        worker['readerReceipt'] = fixture.save('private/readers/cold/left/receipt.json', report)
        result = fixture.analyze()
        self.assertIn('empty_reader:cold:left', result['measurement']['coverageGaps'])
        self.assertNotEqual(result['productChecks']['status'], 'recorded_pass')
        self.assertEqual(result['measurement']['intentRecords'], 6)

    def test_missing_raw_evidence_keeps_the_failed_denominator(self):
        fixture = SavedFixture(self.root)
        (self.root / 'private/readers/cold/left/001-response-body.raw').unlink()
        result = fixture.analyze()
        self.assertEqual(result['measurement']['intentRecords'], 8)
        self.assertEqual(sum(row['outcomes']['evidenceUnavailable'] for row in result['measurement']['groups']), 1)
        self.assertEqual(sum(row['validSuccessLatencyNs']['dispatchToBody']['n']
                             for row in result['measurement']['groups']), 7)

    def test_terminal_cancel_must_bind_the_same_run_and_input_digests(self):
        for mutation in ('run', 'inputs'):
            with self.subTest(mutation=mutation):
                fixture = SavedFixture(self.root)
                cancel = fixture.pool['phases']['cold']['result']['cancel']
                if mutation == 'run':
                    cancel['event']['runId'] = 'f' * 32
                else:
                    cancel['event']['inputs']['left'] = 'f' * 64
                cancel['source'] = fixture.save('private/readers/cold/cancel.json', cancel['event'])
                with self.assertRaisesRegex(READER.EvidenceError, '^reader_cancel_binding$'):
                    fixture.analyze()

    def test_window_end_is_not_proof_that_scan_stopped(self):
        fixture = SavedFixture(self.root)
        for phase in READER.PHASES:
            cancel = fixture.pool['phases'][phase]['result']['cancel']
            cancel['event']['reason'] = 'window_end'
            cancel['source'] = fixture.save('private/readers/' + phase + '/cancel.json', cancel['event'])
        groups = fixture.analyze()['measurement']['groups']
        self.assertEqual(sum(row['intentRecords'] for row in groups if row['overlap'] == 'none'), 0)

    def test_duplicate_or_cross_run_scan_jobs_are_rejected(self):
        fixture = SavedFixture(self.root)
        fixture.tables['scan_jobs'][0]['task_child_id'] = 'e' * 32
        entry = fixture.observers['sql'][-1]
        entry['parsed'] = fixture.save('private/observers/sql/final-after-stop-parsed.json',
                                      {'kind': 'capacity-observation', 'slot': 'final-after-stop', 'tables': fixture.tables})
        entry['receipt'] = fixture.save('private/observers/sql/final-after-stop-receipt.json',
                                       {key: value for key, value in entry.items() if key != 'receipt'})
        with self.assertRaisesRegex(READER.EvidenceError, '^scan_job_binding$'):
            fixture.analyze()

    def test_missing_sql_slots_and_resource_samples_are_not_success(self):
        fixture = SavedFixture(self.root)
        fixture.observers['sql'] = []
        fixture.observers['samples'] = []
        result = fixture.analyze()
        self.assertFalse(result['productChecks']['checks']['sixSqlObservationsAccepted'])
        self.assertEqual(result['measurement']['resources']['status'], 'unavailable')
        self.assertIn('six_sql_slots_not_available', result['measurement']['coverageGaps'])

    def test_overlap_requires_actual_interval_evidence(self):
        self.assertEqual(READER.classify_overlap((10, 20), guaranteed=[(15, 25)]), 'demonstrated')
        self.assertEqual(READER.classify_overlap((10, 20), possible=[(21, 30)]), 'none')
        self.assertEqual(READER.classify_overlap((10, 20), possible=[(15, 25)]), 'indeterminate')
        self.assertEqual(READER.classify_overlap((10, 20), guaranteed=[(20, 30)]), 'indeterminate')
        self.assertEqual(READER.classify_overlap(None, possible=[]), 'indeterminate')

    def test_wall_jump_and_constant_offset_both_remain_unproven(self):
        for anchors in ([anchor(100), anchor(200)], [anchor(100), anchor(200, wall=100)]):
            with self.subTest(anchors=anchors):
                result = READER.clock_summary(anchors)
                self.assertFalse(result['clockContinuityProven'])
                self.assertEqual(result['anchorCount'], 2)
        jumped = READER.clock_summary([anchor(100), anchor(200, wall=100)])
        self.assertEqual(jumped['wallRegressionsAtOrderedAnchors'], 1)
        self.assertIsNone(jumped['constantOffsetIntersectionAtObservedAnchorsNs'])

    def test_failed_and_incomplete_requests_are_not_latency_successes(self):
        modifications = [({'error': 'request_deadline', 'bodyComplete': False}, 'timeout'),
                         ({'error': 'bad_chunk'}, 'transportOrProtocolFailure'),
                         ({'correctness': {'passed': False}}, 'correctnessFailure'),
                         ({'bodyCompleteMonotonicNs': None}, 'timingUnavailable'),
                         ({'dispatched': False}, 'notDispatched')]
        for changes, outcome in modifications:
            with self.subTest(outcome=outcome):
                value = request()
                value.update(changes)
                result = READER.request_metrics(value, True)
                self.assertEqual(result['outcome'], outcome)
                self.assertIsNone(result['bodyNs'])
        self.assertEqual(READER.request_metrics(request(), False)['outcome'], 'evidenceUnavailable')

    def test_quantiles_have_explicit_empty_singleton_and_nearest_rank_semantics(self):
        self.assertEqual(READER.quantiles([]), {'n': 0, 'min': None, 'median': None, 'p95': None, 'max': None})
        self.assertEqual(READER.quantiles([9]), {'n': 1, 'min': 9, 'median': 9, 'p95': 9, 'max': 9})
        self.assertEqual(READER.quantiles(list(range(1, 21)))['p95'], 19)
        self.assertEqual(READER.quantiles([10, 20])['median'], 15)

    def test_costs_preserve_repeated_phases_and_are_never_added(self):
        value = costs()
        summary = READER.cost_summary(value)
        self.assertEqual([row['phase'] for row in summary['stages']], ['business', 'readiness', 'business'])
        self.assertEqual(summary['accounting'], 'inclusive_non_additive')
        self.assertNotIn('totalElapsedNs', summary)
        self.assertFalse(summary['processCpuMeasurement'])
        self.assertEqual(READER.cost_summary(None)['status'], 'unavailable')
        value['stages'][0]['operations']['metrics']['calls'] = True
        with self.assertRaisesRegex(READER.EvidenceError, '^measurement_cost_operation_contract$'):
            READER.cost_summary(value)

    def test_resource_gaps_and_missing_postgres_rss_are_reported(self):
        result = SavedFixture(self.root).analyze()['measurement']['resources']
        self.assertEqual(result['gapsBeyondTwoSecondTargetNs'], [0, 3_000_000_000])
        self.assertEqual(result['roles']['application']['maxSampledMainProcessRssBytes'], 4096)
        self.assertIsNone(result['roles']['postgres']['maxSampledMainProcessRssBytes'])
        self.assertFalse(result['wholeProcessTreeRssMeasured'])

    def test_missing_cost_snapshot_does_not_discard_later_saved_stages(self):
        fixture = SavedFixture(self.root)
        fixture.execution['measurementCostReceipts'].pop(0)
        result = fixture.analyze()['measurement']
        self.assertEqual(len(result['observerCosts']['stages']), 3)
        self.assertIn('missing:private/controller-costs-01.json', result['coverageGaps'])
        self.assertIn('measurement_cost_snapshots_incomplete', result['coverageGaps'])

    def test_scope_path_hash_byte_limit_and_single_link_are_enforced(self):
        fixture = SavedFixture(self.root)
        pin = fixture.save('private/small.json', b'{}')
        reader = READER.SavedScope(self.root, owner=os.getuid())
        for changed, relative, maximum in ((dict(pin, path='/outside/small.json'), 'private/small.json', 10),
                                           (dict(pin, sha256='0' * 64), 'private/small.json', 10),
                                           (dict(pin, bytes=3), 'private/small.json', 10),
                                           (pin, 'private/../small.json', 10),
                                           (pin, 'private/small.json', 1)):
            with self.subTest(relative=relative, maximum=maximum, pin=changed), self.assertRaises(READER.EvidenceError):
                reader.read(changed, relative, maximum)
        os.link(pin['path'], self.root / 'private/second-link.json')
        with self.assertRaisesRegex(READER.EvidenceError, '^private_file_contract$'):
            reader.read(pin, 'private/small.json', 10)

    def test_symlink_file_and_directory_are_not_followed(self):
        fixture = SavedFixture(self.root)
        pin = fixture.save('private/small.json', b'{}')
        link = self.root / 'private/link.json'
        link.symlink_to(Path(pin['path']))
        reader = READER.SavedScope(self.root, owner=os.getuid())
        with self.assertRaises(READER.EvidenceError):
            reader.read(dict(pin, path=str(link)), 'private/link.json', 10)
        directory = self.root / 'linked'
        directory.symlink_to(self.root / 'private', target_is_directory=True)
        with self.assertRaises(READER.EvidenceError):
            reader.read(dict(pin, path=str(directory / 'small.json')), 'linked/small.json', 10)
        fifo = self.root / 'private/not-regular.json'
        os.mkfifo(fifo, mode=0o600)
        with self.assertRaisesRegex(READER.EvidenceError, '^private_file_contract$'):
            reader.read(dict(pin, path=str(fifo)), 'private/not-regular.json', 10)

    def test_file_replacement_during_read_is_rejected_without_writing_evidence(self):
        fixture = SavedFixture(self.root)
        pin = fixture.save('private/small.json', b'{}')
        path = Path(pin['path'])
        original_read = os.read
        replaced = False

        def replace_after_read(fd, size):
            nonlocal replaced
            raw = original_read(fd, size)
            if raw and not replaced:
                replaced = True
                replacement = path.with_name('replacement.json')
                replacement.write_bytes(b'[]')
                replacement.chmod(0o600)
                os.replace(replacement, path)
            return raw

        with mock.patch.object(READER.os, 'read', side_effect=replace_after_read):
            with self.assertRaisesRegex(READER.EvidenceError, '^evidence_changed_during_read$'):
                READER.SavedScope(self.root, owner=os.getuid()).read(pin, 'private/small.json', 10)

    def test_duplicate_keys_and_nonfinite_json_are_rejected(self):
        for raw in (b'{"slot":1,"slot":2}', b'{"value":NaN}', b'{"value":1e309}', b'\xff'):
            with self.subTest(raw=raw), self.assertRaises(READER.EvidenceError):
                READER.parse_json(raw)


if __name__ == '__main__':
    unittest.main()
