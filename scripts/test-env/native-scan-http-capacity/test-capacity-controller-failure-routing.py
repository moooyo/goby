"""Pure failure-routing contracts for Controller.run and Controller.cleanup.

Future execution requires an admitted remote test scope. External ownership,
credential cleanup, reader joins, application stops, infrastructure closure,
storage, files, descriptors and signals are finite leaf substitutes here. No
process, database, token or native preservation result is measured. The real
controller routes every case to a failed attempt; no case can accept capacity.
"""

from contextlib import ExitStack, redirect_stdout
import copy
import importlib.util
import io
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location(
    'capacity_controller_failure_routing_tests', Path(__file__).with_name('capacity-controller.py'))
controller = importlib.util.module_from_spec(spec)
spec.loader.exec_module(controller)


class ControllerFailureRoutingTests(unittest.TestCase):
    def fixture(self):
        """Build only the interfaces consumed by the real routing methods."""
        f = SimpleNamespace(events=[], saved={}, failed_saves=set(), joined_errors=set(), unjoined_errors=set(),
                            reserve_changed=False, constructor_error=False)
        c = controller.Controller.__new__(controller.Controller)
        f.current = c
        f.lock_metadata = {'fixture': 'deployment-lock'}
        f.reserve_metadata = {'fixture': 'cleanup-reserve'}
        protected = {'fixture': 'protected-state'}
        c.base = SimpleNamespace(LOCK=Path('/synthetic/deployment.lock'))
        c.original_run = Mock(side_effect=AssertionError('unexpected real command routing'))
        c.support = SimpleNamespace(protected=Mock(return_value=protected))
        c.ctx = {'fixtureOnly': True}
        c.prepared = {'commands': [], 'ownedUnits': {}, 'createdPaths': []}
        c.runtime = {'commands': [], 'ownedUnits': {}, 'createdPaths': []}
        c.close_commands = {'commands': [], 'ownedUnits': {}, 'createdPaths': []}
        c.record = {'kind': 'synthetic-controller-routing-test', 'failures': [], 'componentReceipts': {},
                    'protectedBefore': protected, 'lockMetadata': f.lock_metadata,
                    'lockReleased': False, 'capacityAccepted': False, 'wholeM2M6Accepted': False,
                    'deadlines': {'originalSequenceCeiling': 1000}}
        c.started = 100.0
        c.phase, c.phase_deadline, c.business_deadline = 'admission', 220.0, 400.0
        c.closure_ceiling, c.closure_deadline = 700.0, None
        c.lock, c.host = 100001, {'net': {'fd': 100002}, 'mnt': {'fd': 100003}}
        c.reserve_identity = f.reserve_metadata
        c.mutation_admitted = True
        c._sample_observation = None
        c._cost_operations = c.new_cost_operations()
        c.allocations, c.reader_starts = {}, {}
        c.raw_before_closure = None
        c.metric_samples_stopped = False
        c.workload_result = c.workload_cleanup = c.final_validation = None
        c.closer = None

        def enter(phase):
            c.phase = phase
            f.events.append(phase)

        c.acquire = Mock(side_effect=lambda: enter('admission'))
        c.prepare = Mock(side_effect=lambda: enter('preparation'))

        def business_failure():
            enter('business')
            raise controller.Rejected('workload_failed')

        c.business = Mock(side_effect=business_failure)
        c.restore_host = Mock(side_effect=lambda: f.events.append('restore-host'))
        c.set_commands = Mock(side_effect=lambda *args: f.events.append('closure-command-route'))
        c.check_storage = Mock(return_value={})
        c.raw_bytes = Mock(return_value=0)
        c.checkpoint_costs = Mock(side_effect=lambda *args: f.events.append('cost-boundary'))
        c.actor = SimpleNamespace(cleanup_owned=Mock(side_effect=lambda *args: f.events.append('fallback')))

        def cleanup_credentials(**kwargs):
            f.events.append('credential-cleanup-entry')
            return {'status': 'closed', 'fixtureOnly': True}

        c.workload = SimpleNamespace(cleanup=Mock(side_effect=cleanup_credentials), observations={},
                                    reader_joins={}, budget={}, trace_persisted=False)
        c.transport = SimpleNamespace(
            set_phase_deadline=Mock(side_effect=lambda *args, **kwargs: f.events.append('cleanup-transport-deadline')),
            summary=Mock(return_value={'fixtureOnly': True}))
        c.pool = SimpleNamespace(record={'childrenCreated': 0, 'phases': {}},
                                 mark_controller_failure=Mock(side_effect=lambda *args: f.events.append('pool-failure')),
                                 start=Mock(side_effect=controller.Rejected('reader_start_failed')))

        def finish(phase, deadline):
            f.events.append('join-' + phase)
            if phase in f.unjoined_errors:
                raise controller.Rejected('reader_join_incomplete')
            result = {'phase': phase, 'joined': True, 'status': 'synthetic-join',
                      'unusedAllocationMayBeReleased': False}
            c.pool.record['phases'][phase].update(joined=True, result=result)
            if phase in f.joined_errors:
                raise controller.Rejected('reader_failed_after_join')
            return result

        c.pool.finish = Mock(side_effect=finish)
        c.app = SimpleNamespace(record={'configurationAccepted': True, 'stopDispatched': False,
                                        'physicalClosed': False, 'normalExit': False},
                                 check_owned=Mock(return_value={'fixtureOnly': True}))

        def stop(configured, deadline):
            f.events.append('application-stop')
            c.app.record.update(stopDispatched=True, physicalClosed=True)

        c.app.stop = Mock(side_effect=stop)
        # Incomplete synthetic slots deliberately prohibit the final SQL route.
        c.observers = SimpleNamespace(record={'sql': [], 'failed': False},
                                       observe=Mock(side_effect=AssertionError('unexpected SQL route')))
        f.closer = SimpleNamespace(record={'status': 'owned_resources_preserved_and_closed',
                                          'infrastructureStopMethodsEntered': ['synthetic-stop']})

        def infrastructure_close():
            f.events.append('infrastructure-close')
            return copy.deepcopy(f.closer.record)

        f.closer.close = Mock(side_effect=infrastructure_close)

        def construct(*args, **kwargs):
            f.events.append('infrastructure-constructor')
            if f.constructor_error:
                raise controller.Rejected('closure_constructor_failed')
            return f.closer

        f.construct = Mock(side_effect=construct)
        c.m = {'closure': SimpleNamespace(Closure=f.construct), 'units': object(), 'corpus': object()}
        return f

    def run_fixture(self, f):
        """Replace only external leaves; keep run, cleanup, writes and joins routed."""
        c = f.current

        def metadata(path):
            if path == controller.RESERVE_FILE:
                return {'fixture': 'replacement'} if f.reserve_changed else f.reserve_metadata
            if path == c.base.LOCK:
                return f.lock_metadata
            raise AssertionError('unexpected metadata path')

        def save(path, value):
            f.events.append('write-' + path.name)
            if path.name in f.failed_saves:
                raise OSError('synthetic publication failure')
            f.saved[path.name] = copy.deepcopy(value)
            return {'path': str(path), 'sha256': '0' * 64, 'bytes': 1}

        def unlink(path):
            self.assertEqual(path, controller.RESERVE_FILE)
            f.events.append('reserve-unlink')

        def close(fd):
            self.assertIn(fd, (100001, 100002, 100003))
            f.events.append('close-lock' if fd == c.lock else 'close-host-descriptor')

        output = io.StringIO()
        with ExitStack() as stack:
            stack.enter_context(patch.object(controller, 'metadata', side_effect=metadata))
            stack.enter_context(patch.object(controller, 'save', side_effect=save))
            stack.enter_context(patch.object(controller.os, 'unlink', side_effect=unlink))
            stack.enter_context(patch.object(controller.os, 'close', side_effect=close))
            stack.enter_context(patch.object(controller.time, 'monotonic', return_value=100.0))
            stack.enter_context(patch.object(controller.time, 'monotonic_ns', return_value=100_000_000_000))
            f.signal = stack.enter_context(patch.object(controller.signal, 'signal'))
            f.timer = stack.enter_context(patch.object(controller.signal, 'setitimer'))
            stack.enter_context(redirect_stdout(output))
            f.exit_code = c.run()
            # The consumed reserve must not be unlinked a second time.
            if c.reserve_identity is None:
                c.release_reserve()
        f.summary = json.loads(output.getvalue())
        self.assertEqual(f.exit_code, 1)
        self.assertEqual(f.summary['status'], 'native_capacity_attempt_failed')
        self.assertFalse(f.summary['capacityAccepted'])
        self.assertFalse(f.summary['wholeM2M6Accepted'])
        self.assertTrue(f.summary['lockReleased'])
        c.observers.observe.assert_not_called()
        c.original_run.assert_not_called()

    def test_first_failure_survives_cleanup_and_join_errors_in_required_order(self):
        for joined in (True, False):
            with self.subTest(join_completed=joined):
                f = self.fixture()
                c = f.current
                c.allocations = {phase: {'settled': False} for phase in ('cold', 'cached')}
                c.pool.record.update(childrenCreated=4, phases={phase: {} for phase in ('cold', 'cached')})
                (f.joined_errors if joined else f.unjoined_errors).add('cold')

                def credentials_fail(**kwargs):
                    f.events.append('credential-cleanup-entry')
                    raise controller.Rejected('credential_cleanup_failed')

                c.workload.cleanup.side_effect = credentials_fail
                self.run_fixture(f)
                self.assertEqual(c.record['firstFailure']['code'], 'workload_failed')
                self.assertIn('workload_cleanup', {row['stage'] for row in c.record['failures']})
                self.assertIn('reader_finish_cold', {row['stage'] for row in c.record['failures']})
                self.assertEqual(c.allocations['cold'].get('physicallyJoined', False), joined)
                self.assertTrue(c.allocations['cached']['physicallyJoined'])
                self.assertTrue(all(row['settled'] is False for row in c.allocations.values()))
                self.assertEqual('reader_failure_without_completed_join' in
                                 {row['code'] for row in c.record['failures']}, not joined)
                order = ('cleanup-transport-deadline', 'credential-cleanup-entry', 'join-cold',
                         'join-cached', 'application-stop', 'infrastructure-close', 'close-lock')
                self.assertEqual([f.events.index(name) for name in order], sorted(f.events.index(name) for name in order))
                c.actor.cleanup_owned.assert_not_called()

    def test_zero_child_start_failure_requires_unchanged_pool_state(self):
        for drift in (False, True):
            with self.subTest(drift=drift):
                f = self.fixture()
                c = f.current

                def start_failure():
                    c.phase = 'business'
                    c.allocations['cold'] = {'settled': False}
                    try:
                        c.start_readers('cold', {'Id': 'synthetic-run'}, {})
                    finally:
                        if drift:
                            c.pool.record['childrenCreated'] = 1

                c.business.side_effect = start_failure
                self.run_fixture(f)
                self.assertEqual(c.record['firstFailure']['code'], 'reader_start_failed')
                self.assertTrue(c.reader_starts['cold']['noChildrenForThisPhaseEstablished'])
                failures = {row['code'] for row in c.record['failures']}
                self.assertEqual('reader_phase_without_child_absence_authority' in failures, drift)
                c.pool.finish.assert_not_called()
                f.closer.close.assert_called_once_with()

    def test_component_publication_failure_still_reaches_infrastructure_close(self):
        f = self.fixture()
        f.failed_saves.add('application-before-preservation.json')
        self.run_fixture(f)
        stages = {row['stage'] for row in f.current.record['failures']}
        self.assertIn('save_application', stages)
        self.assertIn('observers-before-preservation.json', f.saved)
        self.assertIn('reader-pool-before-preservation.json', f.saved)
        self.assertLess(f.events.index('write-application-before-preservation.json'),
                        f.events.index('infrastructure-close'))
        self.assertEqual(f.current.record['firstFailure']['code'], 'workload_failed')

    def test_final_publication_failure_keeps_closed_routes_and_uses_only_fallback(self):
        for fallback_fails in (False, True):
            with self.subTest(fallback_fails=fallback_fails):
                f = self.fixture()
                f.failed_saves.add(controller.RESULT.name)
                if fallback_fails:
                    f.failed_saves.add('controller-result-fallback.json')
                self.run_fixture(f)
                f.closer.close.assert_called_once_with()
                self.assertLess(f.events.index('infrastructure-close'), f.events.index('write-' + controller.RESULT.name))
                self.assertLess(f.events.index('close-lock'), f.events.index('write-' + controller.RESULT.name))
                self.assertEqual(f.events.count('write-' + controller.RESULT.name), 1)
                self.assertEqual(f.events.count('write-controller-result-fallback.json'), 1)
                self.assertEqual(f.summary['receiptPersisted'], not fallback_fails)
                self.assertEqual(f.summary['firstFailure']['code'], 'workload_failed')

    def test_signal_and_deadline_exceptions_route_through_cleanup_reserve(self):
        for number in (controller.signal.SIGTERM, controller.signal.SIGALRM):
            with self.subTest(signal=number):
                f = self.fixture()

                def interrupted():
                    f.current.phase = 'business'
                    raise controller.Deadline('controller_signal_' + str(number))

                f.current.business.side_effect = interrupted
                self.run_fixture(f)
                self.assertEqual(f.current.record['firstFailure']['type'], 'Deadline')
                self.assertEqual(f.current.record['firstFailure']['code'], 'controller_signal_' + str(number))
                self.assertTrue(f.current.metric_samples_stopped)
                self.assertEqual(f.current.closure_deadline, 700.0)
                f.current.workload.cleanup.assert_called_once_with(deadline=700.0)
                f.closer.close.assert_called_once_with()
                for signal_number in (controller.signal.SIGTERM, controller.signal.SIGINT, controller.signal.SIGHUP):
                    f.signal.assert_any_call(signal_number, controller.signal.SIG_IGN)
                f.timer.assert_any_call(controller.signal.ITIMER_REAL, 0)

    def test_fallback_requires_zero_infrastructure_stop_entry(self):
        for branch in ('constructor-error', 'zero-stop-entry', 'stop-entered'):
            with self.subTest(branch=branch):
                f = self.fixture()
                f.constructor_error = branch == 'constructor-error'
                f.closer.record = {'status': 'closure_authority_unavailable_resources_retained',
                                   'infrastructureStopMethodsEntered': [] if branch == 'zero-stop-entry' else ['synthetic-stop']}
                self.run_fixture(f)
                expected = branch != 'stop-entered'
                self.assertEqual(f.current.actor.cleanup_owned.call_count, int(expected))
                self.assertEqual('preparationEmergencyClose' in f.current.record, expected)
                if expected:
                    f.current.actor.cleanup_owned.assert_called_once_with('controller_zero_stop_entry_fallback')
                    self.assertEqual(f.current.actor.cleanup_deadline, f.current.closure_deadline)

    def test_reserve_is_consumed_once_or_retained_on_identity_change(self):
        for changed in (False, True):
            with self.subTest(changed=changed):
                f = self.fixture()
                f.reserve_changed = changed
                self.run_fixture(f)
                self.assertEqual(f.events.count('reserve-unlink'), int(not changed))
                self.assertEqual(f.current.record.get('cleanupCaptureReserveReleased', False), not changed)
                self.assertEqual(f.current.reserve_identity is None, not changed)
                if not changed:
                    self.assertLess(f.events.index('reserve-unlink'), f.events.index('credential-cleanup-entry'))
                else:
                    self.assertIn('cleanup_reserve_identity_changed', {row['code'] for row in f.current.record['failures']})
                f.closer.close.assert_called_once_with()
                self.assertEqual(f.current.record['firstFailure']['code'], 'workload_failed')

    def test_unadmitted_failure_releases_reserve_without_entering_mutation_cleanup(self):
        f = self.fixture()
        c = f.current
        c.mutation_admitted = False
        c.acquire.side_effect = controller.Rejected('admission_failed')
        self.run_fixture(f)
        self.assertEqual(c.record['firstFailure']['code'], 'admission_failed')
        self.assertEqual(f.events.count('reserve-unlink'), 1)
        c.prepare.assert_not_called()
        c.business.assert_not_called()
        c.workload.cleanup.assert_not_called()
        c.app.stop.assert_not_called()
        f.construct.assert_not_called()
        c.actor.cleanup_owned.assert_not_called()


if __name__ == '__main__':
    unittest.main()
