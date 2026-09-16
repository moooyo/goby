"""Pure checks for one-shot observations and inclusive controller costs.

Run only in an explicitly admitted remote check scope. No test invokes a real
controller, ownership probe, storage walk, command, HTTP request or SQL query.
"""

import copy
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


controller = load('capacity_controller_measurement_tests', 'capacity-controller.py')
observers = load('capacity_observers_measurement_tests', 'capacity-observers.py')


class Clock:
    def __init__(self):
        self.value = 10_000_000_000

    def now(self):
        return self.value

    def advance(self, value):
        self.value += value


class MeasurementTests(unittest.TestCase):
    def setUp(self):
        self.clock = Clock()
        self.clock_patch = patch.object(controller.time, 'monotonic_ns', self.clock.now)
        self.clock_patch.start()
        self.addCleanup(self.clock_patch.stop)
        self.current = controller.Controller.__new__(controller.Controller)
        current = self.current
        current.phase = current._cost_phase = 'business'
        current.phase_deadline = current.business_deadline = 10 ** 20
        current._cost_started_ns = self.clock.now()
        current._cost_operations = current.new_cost_operations()
        current.record = {'failures': [], 'measurementCosts': {
            'version': 1, 'clock': 'time.monotonic_ns',
            'accounting': 'inclusive_non_additive', 'stages': []}, 'measurementCostReceipts': []}
        current._sample_observation = None
        current.metric_samples_stopped = False
        current.ctx, current.base = {}, object()
        current.support = SimpleNamespace(check_infrastructure=Mock())
        current.app = SimpleNamespace(check_owned=Mock(side_effect=lambda: {
            'observedMonotonicNs': self.clock.now(), 'generation': current.app.check_owned.call_count}))
        current.pool = SimpleNamespace(poll=Mock(return_value={'observed': True}))
        current.observers = SimpleNamespace(metrics_due=Mock(return_value=True),
                                            capture_metrics=Mock(), observe=Mock(return_value={}))
        current.check_storage = Mock(return_value={})
        current.original_run = Mock(return_value=b'')

    def assert_fresh_after_boundary(self, operation):
        current = self.current
        current.assert_owned()
        operation()
        self.assertIsNone(current._sample_observation)
        current.app.check_owned.reset_mock()
        current.pool.poll.reset_mock()
        current.sample()
        current.app.check_owned.assert_called_once_with()
        current.pool.poll.assert_called_once_with()

    def test_adjacent_sample_shares_ownership_and_pool_once(self):
        current = self.current
        current.assert_owned()
        observation = current._sample_observation['application']
        current.sample()
        current.support.check_infrastructure.assert_called_once_with(current.base, current.ctx)
        current.app.check_owned.assert_called_once_with()
        current.pool.poll.assert_called_once_with()
        current.observers.capture_metrics.assert_called_once_with(observation)
        current.check_storage.assert_called_once_with(0, 64 << 20)
        self.assertIsNone(current._sample_observation)

    def test_not_due_discards_observation_and_keeps_storage_check(self):
        current = self.current
        current.observers.metrics_due.return_value = False
        current.assert_owned()
        current.sample()
        self.assertIsNone(current._sample_observation)
        current.app.check_owned.assert_called_once_with()
        current.pool.poll.assert_called_once_with()
        current.observers.capture_metrics.assert_not_called()
        current.check_storage.assert_called_once_with(0, 64 << 20)
        current.observers.metrics_due.return_value = True
        current.sample()
        self.assertEqual(current.app.check_owned.call_count, 2)
        self.assertEqual(current.pool.poll.call_count, 2)
        self.assertEqual(current.check_storage.call_count, 2)

    def test_due_decision_precedes_fresh_ownership_and_pool(self):
        current, order = self.current, []
        current.observers.metrics_due.side_effect = lambda: order.append('due') or False
        current.app.check_owned.side_effect = AssertionError('not-due ownership probe')
        current.pool.poll.side_effect = lambda: order.append('pool')
        current.check_storage.side_effect = lambda *args: order.append('storage')
        current.sample()
        self.assertEqual(order, ['due', 'pool', 'storage'])

    def test_http_reservation_and_command_clear_pending_observation(self):
        current = self.current
        with self.subTest(boundary='http-reservation'):
            self.assert_fresh_after_boundary(lambda: current.reserve({
                'event': 'capacity-http-reserve', 'maximumAdditionalBytes': 1,
                'phase': 'setup', 'cleanup': False}))
        with self.subTest(boundary='command'):
            self.assert_fresh_after_boundary(lambda: current.command('fixed', ['not-executed']))
            current.original_run.assert_called_once()

    def test_sql_and_phase_boundaries_clear_pending_observation(self):
        current = self.current
        with self.subTest(boundary='sql-snapshot'):
            self.assert_fresh_after_boundary(lambda: current.snapshot('empty-catalog'))
            current.observers.observe.assert_called_once()
        with self.subTest(boundary='phase'):
            with patch.object(controller.signal, 'setitimer'), patch.object(current, 'checkpoint_costs'):
                self.assert_fresh_after_boundary(lambda: current.set_deadline('readiness', 10 ** 20))

    def test_different_app_or_pool_cannot_reuse_observation(self):
        current = self.current
        for component in ('app', 'pool'):
            with self.subTest(component=component):
                current.assert_owned()
                if component == 'app':
                    current.app = SimpleNamespace(check_owned=Mock(return_value={
                        'observedMonotonicNs': self.clock.now()}))
                else:
                    current.pool = SimpleNamespace(poll=Mock())
                current.app.check_owned.reset_mock()
                current.pool.poll.reset_mock()
                current.sample()
                current.app.check_owned.assert_called_once_with()
                current.pool.poll.assert_called_once_with()

    def test_old_same_round_observation_is_refreshed(self):
        current = self.current
        current.assert_owned()
        self.clock.advance(5_000_000_001)
        current.sample()
        self.assertEqual(current.app.check_owned.call_count, 2)
        self.assertEqual(current.pool.poll.call_count, 2)

    def test_failed_observation_or_metric_leaves_no_reusable_value(self):
        current = self.current
        current.app.check_owned.side_effect = RuntimeError('ownership_failed')
        with self.assertRaisesRegex(RuntimeError, 'ownership_failed'):
            current.assert_owned()
        self.assertIsNone(current._sample_observation)
        self.assertEqual(current._cost_operations['ownership']['failures'], 1)
        current.app.check_owned.side_effect = lambda: {'observedMonotonicNs': self.clock.now()}
        current.pool.poll.side_effect = RuntimeError('pool_failed')
        with self.assertRaisesRegex(RuntimeError, 'pool_failed'):
            current.assert_owned()
        self.assertIsNone(current._sample_observation)
        self.assertEqual(current._cost_operations['pool']['failures'], 1)
        current.pool.poll.side_effect = None
        current.assert_owned()
        current.observers.capture_metrics.side_effect = RuntimeError('metrics_failed')
        with self.assertRaisesRegex(RuntimeError, 'metrics_failed'):
            current.sample()
        self.assertIsNone(current._sample_observation)
        self.assertEqual(current._cost_operations['metrics']['failures'], 1)

    def test_stopped_sampling_discards_pending_value(self):
        current = self.current
        current.assert_owned()
        current.metric_samples_stopped = True
        current.sample()
        self.assertIsNone(current._sample_observation)
        current.observers.metrics_due.assert_not_called()
        current.check_storage.assert_not_called()

    def test_inclusive_costs_record_nested_work_and_failures(self):
        current = self.current

        def inner():
            self.clock.advance(7)
            raise RuntimeError('command_failed')

        def outer():
            self.clock.advance(3)
            return current.measure('commandWait', inner)

        with self.assertRaisesRegex(RuntimeError, 'command_failed'):
            current.measure('ownership', outer)
        self.assertEqual(current._cost_operations['ownership'], {'calls': 1, 'elapsedNs': 10, 'failures': 1})
        self.assertEqual(current._cost_operations['commandWait'], {'calls': 1, 'elapsedNs': 7, 'failures': 1})
        self.assertEqual(set(current._cost_operations), set(controller.COST_NAMES))

    def test_full_storage_check_keeps_all_four_existing_walks(self):
        current = self.current
        del current.check_storage
        with patch.object(controller, 'usage', return_value={'bytes': 0, 'allocatedBytes': 0}) as walk:
            with patch.object(controller.os, 'statvfs', return_value=SimpleNamespace(f_bavail=10 ** 9, f_frsize=4096)):
                current.check_storage(1, 64 << 20)
        self.assertEqual(walk.call_count, 4)
        self.assertEqual([call.args[0] for call in walk.call_args_list], [
            controller.P, controller.F / 'log', controller.E, controller.F])
        self.assertEqual(current._cost_operations['storageWalk']['calls'], 4)

    def test_phase_snapshot_is_bounded_independent_and_adds_no_walk(self):
        current, saved = self.current, []
        current.measure('ownership', lambda: self.clock.advance(19))

        def write(path, value):
            saved.append(copy.deepcopy(value))
            self.clock.advance(3)
            return {'path': str(path), 'bytes': 1, 'sha256': '0' * 64}

        with patch.object(controller, 'save', side_effect=write):
            with patch.object(controller, 'usage', side_effect=AssertionError('unexpected cost storage walk')):
                current.checkpoint_costs('closure')
        current.check_storage.assert_not_called()
        stage = saved[0]['stages'][0]
        self.assertEqual(stage['phase'], 'business')
        self.assertEqual(stage['operations']['ownership'], {'calls': 1, 'elapsedNs': 19, 'failures': 0})
        self.assertEqual(current._cost_operations['evidenceWrite'], {'calls': 1, 'elapsedNs': 3, 'failures': 0})
        current._cost_operations['ownership']['calls'] += 1
        self.assertEqual(stage['operations']['ownership']['calls'], 1)
        self.assertEqual(len(current.record['measurementCostReceipts']), 1)

    def test_cost_publication_failure_preserves_first_failure(self):
        current = self.current
        current.fail('business', RuntimeError('original_failure'))
        first = copy.deepcopy(current.record['firstFailure'])
        with patch.object(controller, 'save', side_effect=OSError('synthetic write failure')):
            current.checkpoint_costs('closure')
        self.assertEqual(current.record['firstFailure'], first)
        self.assertEqual(current.record['failures'][-1]['stage'], 'measurement_cost_receipt')
        self.assertEqual(current.record['measurementCostReceipts'], [])
        self.assertEqual(current._cost_operations['evidenceWrite']['failures'], 1)
        current.check_storage.assert_not_called()

    def test_due_check_and_observer_write_callback_are_side_effect_free(self):
        item = observers.Observers.__new__(observers.Observers)
        item._last_sample_ns = self.clock.now()
        item._metric_group = Mock(side_effect=AssertionError('unexpected metrics read'))
        self.assertFalse(item.metrics_due())
        self.assertEqual(item.capture_metrics(None)['status'], 'not-due')
        self.clock.advance(2_000_000_000)
        self.assertTrue(item.metrics_due())
        item._metric_group.assert_not_called()
        item._measure = self.current.measure
        item._write = Mock(return_value={'retained': True})
        self.assertEqual(item._save(Path('not-created'), b'fixed'), {'retained': True})
        item._write.assert_called_once_with(Path('not-created'), b'fixed')
        self.assertEqual(self.current._cost_operations['evidenceWrite']['calls'], 1)


if __name__ == '__main__':
    unittest.main()
