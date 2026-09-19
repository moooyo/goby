# Decoder queue wakeup regression

The `amd-media-v3` installer requires this gate before publishing a new FFmpeg
prefix. It builds the same harness against two complete source trees: the
baseline retains the existing strict Dolby Vision patch, and the candidate adds
the independent decoder queue wakeup patch. Both use the same built AV libraries.
Source includes take priority over build includes, preventing candidate headers
from silently replacing the baseline implementation.

The harness includes the real n9.0.1 scheduler and thread queue. Test hooks only
establish ordering with semaphores at existing receive/wait boundaries. It checks
buffered overflow, notification before a receive starts, exact A/B/C packet
payload and clock order, normal EOF, and stop from both wait domains. The explicit
EOF-waiter cancellation branch may retain undelivered A/B until teardown; buffer
reference checks prove their release. Ordinary wakeup and normal EOF still require
delivery of every packet. No codec, GPU, or long-running source is simulated.

Each case has a three-second child-process watchdog. The baseline must reproduce
four specific deadlocks and pass three controls; the candidate must pass eight
cases without a deadlock. The watchdog bounds failure and never orders threads.
Expected summaries are:

```text
contract=PASS mode=baseline cases=7 reproduced_deadlocks=4
contract=PASS mode=candidate cases=8 reproduced_deadlocks=0
```

`run-native-regression.py` compiles each variant, enforces every result line,
rejects unexpected diagnostics, and records command logs, binary hashes, source
and object hashes, and an accepted receipt. It checks that inputs remain unchanged.
The installer retains those records in `metadata/native-regression/`, plus this
harness and both patches. It preserves the full baseline before applying the
wakeup patch and uses an out-of-tree build for generated files and objects.

The corrected harness and private candidate passed `native-evidence02` on the
authorized CT. The original `native-evidence01` failure remains recorded: its
stop-waiter assertion incorrectly required normal draining after cancellation.
The private candidate then passed the unchanged dual-input AMD media path with
identical saved source prefixes, original stderr, source clocks, subtitle pixels,
and cancellation checks. These are the selected candidate results in the
[phase 2 record](../../../../docs/development/amd-media-phase2-20260919.md#decoder-queue-wakeup-candidate-acceptance),
not acceptance of an untested successor installation or of the complete phase.

For a manual authorized remote invocation, provide separate source/build roots
and new output directories:

```sh
python3 run-native-regression.py \
  --baseline /private/baseline-src --candidate /private/candidate-src \
  --build /private/candidate-build --binaries /private/native-binaries \
  --evidence /private/native-evidence
```

Do not add stubs for missing scheduler or queue functions to repair a link failure.
The sole CLI-only `print_sdp()` guard aborts if unexpectedly reached; the test does
not initialize media mux tasks. Published toolchain prefixes remain immutable.
