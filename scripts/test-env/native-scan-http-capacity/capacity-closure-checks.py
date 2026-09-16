"""Future test-env checks for the closure's child-only output limit.

Run only when remote verification is authorized: python3 -I -B this-file.py.
TMPDIR must point to the admitted check's private writable scratch directory.
These checks load the pinned capture's definitions and run small Python children
in new temporary directories. They never run systemctl, SQL, HTTP, or a capacity
actor. The mandatory-stop case substitutes a Python writer before capture.
"""

import errno
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import resource
import signal
import subprocess
import sys
import tempfile
import time
import types
import unittest


BASE_PATH = Path('/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-isolated-restore-01/private/prepare-isolated-infrastructure.py')
BASE_SHA = 'ce20e4dbe21add70f90370b84e1debcf136313db6b98e8fb62ebe47433e6a9d0'
STREAM_LIMIT = 64 << 10


def overflow_program(limit):
    return """import errno, json, os, resource, signal, sys
signal.alarm(3)
signal.signal(signal.SIGXFSZ, signal.SIG_IGN)
limit = %d
first = os.write(1, b'x' * (limit + 1))
try:
    os.write(1, b'y')
except OSError as error:
    os.write(2, json.dumps({'errno': error.errno, 'firstWriteBytes': first,
        'limits': resource.getrlimit(resource.RLIMIT_FSIZE)}).encode('ascii'))
    sys.exit(23 if error.errno == errno.EFBIG else 24)
sys.exit(25)
""" % limit


class ClosureCaptureChecks(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if sys.platform != 'linux' or os.geteuid() != 0:
            raise RuntimeError('These checks require the authorized root test-env context.')
        cls.base_source = BASE_PATH.read_bytes()
        if hashlib.sha256(cls.base_source).hexdigest() != BASE_SHA:
            raise RuntimeError('Pinned inherited capture source changed.')
        path = Path(__file__).with_name('capacity-closure.py')
        spec = importlib.util.spec_from_file_location('capacity_closure_under_test', path)
        cls.closure = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.closure)

    def setUp(self):
        self.parent_limit = resource.getrlimit(resource.RLIMIT_FSIZE)
        self.assertTrue(all(value == resource.RLIM_INFINITY or value >= 256 << 10
                            for value in self.parent_limit), 'The test process needs a 256 KiB output allowance.')
        self.temporary = tempfile.TemporaryDirectory(prefix='capacity-closure-capture-', dir=os.environ['TMPDIR'])
        self.addCleanup(self.temporary.cleanup)
        self.base = types.ModuleType('pinned_capture_' + self.id().rsplit('.', 1)[-1])
        self.base.__file__ = str(BASE_PATH)
        exec(compile(self.base_source, str(BASE_PATH), 'exec'), self.base.__dict__)
        self.base.P = Path(self.temporary.name)
        self.base.serial = 0
        self.base.report = {'commands': []}
        self.original_capture = self.base.run
        self.original_subprocess = self.base.subprocess
        self.original_popen = subprocess.Popen

    def tearDown(self):
        self.assertIs(self.base.subprocess, self.original_subprocess)
        self.assertIs(subprocess.Popen, self.original_popen)
        self.assertEqual(resource.getrlimit(resource.RLIMIT_FSIZE), self.parent_limit)

    def inherited_deadline_wrapper(self, label, argv, timeout):
        # The actual controller also retains a reference to the original capture.
        return self.original_capture(label, argv, timeout)

    def capture(self, label, program, *, limit=STREAM_LIMIT, timeout=2):
        return self.closure._capture_with_output_limit(
            self.base, self.inherited_deadline_wrapper, label,
            [sys.executable, '-I', '-B', '-c', program], timeout, limit)

    def assert_command_closed(self):
        self.assertEqual(len(self.base.report['commands']), 1)
        row = self.base.report['commands'][0]
        self.assertTrue(row['closed'])
        self.assertEqual(row['pid'], row['processGroup'])
        with self.assertRaises(ProcessLookupError):
            os.killpg(row['processGroup'], 0)
        return row

    def test_kernel_rejects_overflow_before_file_can_exceed_limit(self):
        with self.assertRaisesRegex(self.base.Rejected, '^overflow_exit$'):
            self.capture('overflow', overflow_program(STREAM_LIMIT))
        row = self.assert_command_closed()
        self.assertEqual(row['exitCode'], 23)
        self.assertEqual(row['stdout']['bytes'], STREAM_LIMIT)
        self.assertEqual(Path(row['stdout']['path']).stat().st_size, STREAM_LIMIT)
        details = json.loads(Path(row['stderr']['path']).read_bytes())
        self.assertEqual(details['errno'], errno.EFBIG)
        self.assertEqual(details['firstWriteBytes'], STREAM_LIMIT)
        self.assertEqual(details['limits'], [STREAM_LIMIT, STREAM_LIMIT])

    def test_each_stream_has_its_own_bound(self):
        program = "import os, signal; signal.alarm(3); os.write(1, b'o' * %d); os.write(2, b'e' * %d)" % (STREAM_LIMIT, STREAM_LIMIT)
        returned = self.capture('two-streams', program)
        row = self.assert_command_closed()
        self.assertEqual(returned, b'o' * STREAM_LIMIT)
        self.assertEqual(Path(row['stderr']['path']).read_bytes(), b'e' * STREAM_LIMIT)
        self.assertEqual((row['stdout']['bytes'], row['stderr']['bytes']), (STREAM_LIMIT, STREAM_LIMIT))

    def test_original_timeout_still_closes_the_owned_group(self):
        program = 'import signal, time; signal.alarm(3); time.sleep(10)'
        with self.assertRaisesRegex(self.base.Rejected, '^waiting_timeout$'):
            self.capture('waiting', program, timeout=0.05)
        row = self.assert_command_closed()
        self.assertTrue(row['neededGroupClosure'])

    def test_capture_failure_is_not_replaced_and_module_is_restored(self):
        original_error = ValueError('original_capture_failure')

        def failing_capture(label, argv, timeout):
            self.assertIsNot(self.base.subprocess, self.original_subprocess)
            self.assertIs(subprocess.Popen, self.original_popen)
            raise original_error

        with self.assertRaises(ValueError) as observed:
            self.closure._capture_with_output_limit(self.base, failing_capture, 'failed', [], 1, STREAM_LIMIT)
        self.assertIs(observed.exception, original_error)
        self.assertEqual(self.base.report['commands'], [])

    def test_invalid_budget_keeps_mandatory_stop_bounded_and_never_replays_it(self):
        closer = self.closure.Closure(self.base, None, None, None, None, {}, None,
                                      deadline=time.monotonic() + 30, budget=None)
        closer._check_tool = lambda name: '/usr/bin/systemctl'
        label = 'capacity-closure-stop-' + self.closure.PGUNIT
        arguments = ['/usr/bin/systemctl', 'stop', self.closure.PGUNIT]
        closer.record['closedUnits'][self.closure.PGUNIT] = {'ownershipEstablished': True}

        def synthetic_stop(received_label, received_argv, timeout):
            self.assertEqual((received_label, received_argv), (label, arguments))
            return self.original_capture(received_label,
                [sys.executable, '-I', '-B', '-c', overflow_program(256 << 10)], timeout)

        closer._original_run = synthetic_stop
        with self.assertRaises(self.base.Rejected):
            closer._transport(label, arguments, 2)
        row = self.assert_command_closed()
        self.assertEqual(row['stdout']['bytes'], 256 << 10)
        self.assertEqual(closer.record['firstError']['code'], 'closure_budget_contract_missing_or_changed')
        self.assertTrue(closer.record['closedUnits'][self.closure.PGUNIT]['stopDispatched'])
        self.assertTrue(closer.record['commandReferences'][0]['dispatched'])
        with self.assertRaisesRegex(self.closure.ClosureError, '^closure_stop_already_dispatched$'):
            closer._transport(label, arguments, 2)
        self.assertEqual(len(self.base.report['commands']), 1)


if __name__ == '__main__':
    unittest.main()
