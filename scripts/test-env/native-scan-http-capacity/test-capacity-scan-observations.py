"""Pure scan-observation parsing, request-budget, and clock contracts.

Kernel clock reads are mocked. These tests perform no HTTP, SQL, process, service,
or native scan operation and do not establish measurement acceptance.
"""

import copy
import importlib.util
import io
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


workload = load('capacity_scan_observation_workload', 'capacity-workload.py')
reader = load('capacity_scan_observation_reader', 'capacity-reader.py')
controller = load('capacity_scan_observation_controller', 'capacity-controller.py')
transport = load('capacity_scan_observation_transport', 'capacity-transport.py')
LIBRARIES = {'1' * 32, '2' * 32}
BOOT = '11111111-2222-3333-4444-555555555555'


def job(identifier='a' * 32, library='1' * 32, status='pending'):
    return {'Id': identifier, 'LibraryId': library, 'ForceProbe': False, 'Status': status,
            'Error': '', 'Scanned': 0, 'Added': 0, 'Updated': 0,
            'CreatedAt': '2026-09-16T00:00:00Z',
            'StartedAt': None if status == 'pending' else '2026-09-16T00:00:01Z',
            'FinishedAt': '2026-09-16T00:00:02Z' if status == 'completed' else None}


def envelope(*jobs):
    return {'Items': list(jobs), 'TotalRecordCount': len(jobs)}


class ScanObservationContracts(unittest.TestCase):
    def test_two_observations_replace_detail_calls_without_borrowing_other_budgets(self):
        selected = [slot for slot in range(1, 92) if workload.detail_poll_requested(slot)]
        self.assertEqual(len(selected), 89)
        self.assertEqual(set(range(1, 92)) - set(selected), {30, 60})
        self.assertIn(1, selected)
        self.assertIn(91, selected)
        self.assertEqual(2 * (len(selected) + 2 + 1), workload.COUNTER_LIMITS['task-poll'])
        self.assertEqual(112 + 184 + 64 + 240, 600)
        for invalid in (0, 92, True):
            with self.subTest(slot=invalid), self.assertRaises(workload.WorkloadError):
                workload.detail_poll_requested(invalid)

    def test_observations_use_first_two_periodic_details_without_delaying_reader_start(self):
        self.assertEqual(workload.scan_observation_positions(1, 'running', 0), ())
        self.assertEqual(workload.scan_observation_positions(2, 'running', 0), ('first',))
        self.assertEqual(workload.scan_observation_positions(3, 'running', 1), ('second',))
        self.assertEqual(workload.scan_observation_positions(4, 'running', 2), ())

    def test_completed_detail_uses_no_extra_wait_and_failure_adds_no_business(self):
        self.assertEqual(workload.scan_observation_positions(1, 'completed', 0), ('first', 'second'))
        self.assertEqual(workload.scan_observation_positions(2, 'completed', 0), ('first', 'second'))
        self.assertEqual(workload.scan_observation_positions(3, 'completed', 1), ('second',))
        for state in ('failed', 'cancelled', 'interrupted', 'stopping'):
            with self.subTest(state=state):
                self.assertEqual(workload.scan_observation_positions(2, state, 0), ())
                self.assertEqual(workload.scan_observation_positions(3, state, 1), ())

    def test_empty_and_partially_created_current_jobs_are_legal(self):
        self.assertEqual(workload.scan_observation_jobs(envelope(), 'cold', LIBRARIES, {}), {})
        pending = job()
        self.assertEqual(workload.scan_observation_jobs(envelope(pending), 'cold', LIBRARIES, {}), {'a' * 32: pending})
        retained = {value['Id']: value for value in
                    (job('c' * 32, status='completed'), job('d' * 32, '2' * 32, 'completed'))}
        self.assertEqual(len(workload.scan_observation_jobs(envelope(*retained.values()), 'cached', LIBRARIES, retained)), 2)
        self.assertEqual(len(workload.scan_observation_jobs(envelope(*retained.values(), pending), 'cached', LIBRARIES, retained)), 3)

    def test_foreign_duplicate_and_malformed_rows_are_rejected(self):
        values = [envelope(job(library='f' * 32)), envelope(job(), job()),
                  envelope(job(), job('b' * 32)), {'Items': [], 'TotalRecordCount': True}]
        for value in values:
            with self.subTest(value=value), self.assertRaises(workload.WorkloadError):
                workload.scan_observation_jobs(value, 'cold', LIBRARIES, {})
        for key, value in (('ForceProbe', True), ('Scanned', True), ('Status', 'Running')):
            row = job()
            row[key] = value
            with self.subTest(key=key), self.assertRaises(workload.WorkloadError):
                workload.scan_observation_jobs(envelope(row), 'cold', LIBRARIES, {})

    def test_cached_observation_cannot_drop_or_change_old_terminal_jobs(self):
        retained = {value['Id']: value for value in
                    (job('c' * 32, status='completed'), job('d' * 32, '2' * 32, 'completed'))}
        changed = copy.deepcopy(list(retained.values()))
        changed[0]['Scanned'] += 1
        for value in (envelope(), envelope(*changed)):
            with self.subTest(value=value), self.assertRaisesRegex(workload.WorkloadError, 'retained_changed'):
                workload.scan_observation_jobs(value, 'cached', LIBRARIES, retained)

    def test_running_requires_started_time_and_no_terminal_timestamp(self):
        for key, value in (('StartedAt', None), ('FinishedAt', '2026-09-16T00:00:02Z')):
            row = job(status='running')
            row[key] = value
            with self.subTest(key=key), self.assertRaises(workload.WorkloadError):
                workload.scan_observation_jobs(envelope(row), 'cold', LIBRARIES, {})

    def test_observation_event_binds_only_the_completed_fixed_http_receipt(self):
        source = {'path': '/synthetic/control-http/001-receipt.json', 'sha256': 'a' * 64, 'bytes': 1}
        saved = {'path': '/synthetic/controller-events/0001.json', 'sha256': 'b' * 64, 'bytes': 1}
        http = transport.ControlTransport.__new__(transport.ControlTransport)
        http.active = None
        http.scan_observation_receipts = {'scan-state-cold-first': source}
        current = controller.Controller.__new__(controller.Controller)
        current._sample_observation = None
        current.event_serial = 0
        current.phase = 'business'
        current.transport = http
        current.check_storage = Mock()
        current.write = Mock(return_value=saved)
        observation = {'label': 'scan-state-cold-first'}
        result = current.event({'event': 'scan-job-state-observation', 'observation': observation})
        self.assertEqual(result, {'observation': dict(observation, controlReceipt=source), 'record': saved})
        self.assertNotIn('controlReceipt', observation)
        self.assertEqual(current.write.call_args.args[1]['observation']['controlReceipt'], source)
        with self.assertRaises(transport.Rejected):
            http.scan_observation_receipt('unreviewed-label')
        http.active = {'notClosed': True}
        with self.assertRaises(transport.Rejected):
            http.scan_observation_receipt('scan-state-cold-first')

    def domain(self, *, after_inode=123, second_boot=BOOT, implementation='clock_gettime(CLOCK_MONOTONIC)'):
        path = Mock()
        path.open.side_effect = [io.BytesIO((BOOT + '\n').encode()), io.BytesIO((second_boot + '\n').encode())]
        with patch.object(reader.sys, 'platform', 'linux'), patch.object(reader, 'Path', return_value=path), \
             patch.object(reader.os, 'open', return_value=99), \
             patch.object(reader.os, 'fstat', return_value=SimpleNamespace(st_dev=4, st_ino=123)), \
             patch.object(reader.os, 'stat', return_value=SimpleNamespace(st_dev=4, st_ino=after_inode)), \
             patch.object(reader.os, 'close') as close, \
             patch.object(reader.time, 'get_clock_info', return_value=SimpleNamespace(
                 implementation=implementation, monotonic=True, adjustable=False)):
            result = reader.clock_domain()
            close.assert_called_once_with(99)
            return result

    def test_clock_identity_comes_from_actual_selected_clock_and_namespace(self):
        value = self.domain()
        self.assertTrue(value['available'])
        self.assertEqual(value['bootId'], BOOT)
        self.assertEqual(value['timeNamespace'], {'device': 4, 'inode': 123})
        self.assertEqual(reader.check_clock_domain(value), value)

    def test_changed_or_unknown_clock_is_unavailable_and_fd_is_closed(self):
        for changes in ({'after_inode': 124}, {'second_boot': 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'},
                        {'implementation': 'unknown-clock'}):
            with self.subTest(changes=changes):
                self.assertEqual(self.domain(**changes),
                                 {'version': 1, 'available': False, 'code': 'clock_domain_unavailable'})

    def test_missing_time_namespace_does_not_claim_availability_or_close_an_unknown_fd(self):
        path = Mock()
        path.open.return_value = io.BytesIO((BOOT + '\n').encode())
        with patch.object(reader.sys, 'platform', 'linux'), patch.object(reader, 'Path', return_value=path), \
             patch.object(reader.os, 'open', side_effect=FileNotFoundError), patch.object(reader.os, 'close') as close:
            self.assertFalse(reader.clock_domain()['available'])
            close.assert_not_called()


if __name__ == '__main__':
    unittest.main()
