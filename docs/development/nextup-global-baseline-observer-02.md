# Reference baseline observer 02 independent acceptance

Run `nextup-global-reference-baseline-20260913-02` completed the full bounded
public baseline observation and received independent acceptance at
`2026-09-12T19:20:56.107835+00:00`. The independent terminal records
`status=complete_public_baseline_independently_attested` and
`baselineUsableForFreshPreparation=true`. It retains
`matrixFixtureReleased=false`.

The observer used source
`5436668b824c3432fe52b6f250faea6626f4fa6dc13fc33955fa152337487228`
from the frozen `nextup-global-reference-baseline-observer-tool-04` scope.
The source and its 149 passing remote guards were published in `c2291d9`.
The [observer contract](nextup-global-baseline-observer.md) describes the
header normalization, closed predecessor chain, authority checks, and
bounded dispatch used by this run.

## Complete observed baseline

The actual run recorded 34 requests: 32 normal requests and two cleanup
requests. These comprise one administrator login, all 31 snapshot GETs,
logout, and the same-token rejection proof. The GETs include the full
System/Configuration response. The baseline retains six users and eight
libraries.

Independent reconstruction from the actual raw responses matched the entire
private baseline output. All retained public server, configuration, library,
catalog, subject projection, preference, and detail documents remained
equal, with only the permitted administrator authentication-date changes
in the account profiles.

The actual Devices response contained 88 rows:

- 85 original device rows preserved completely.
- Two acknowledged, independently closed predecessor devices preserved
  under their own authentication windows.
- One new device owned by this observer's actual login.

Both controller closure adapters were independently checked against their
original wire requests and responses. They prove logout HTTP 204 followed
by HTTP 401 using the same observer token. The private authentication
closure remains available for a fresh preparation's release input.

The runner's original terminal retains
`status=awaiting_independent_attestation`, `cleanupComplete=true`,
`completeSnapshotObserved=true`, `uncertain=false`, and
`baselineReleased=false`. The subsequent independent terminal establishes
baseline usability without rewriting that runner record.

## Process closure and preservation

The controlled unit was `goby-nextup-global-reference-baseline-02.service`,
invocation `9073208f89904e56bd12e7da2a3f5c00`. Its final properties were
active/exited, successful exit status 0, and MainPID 0. Independent closure
confirmed the original PID 1506833 absent and the cgroup recursively empty.

The complete preservation comparison retained all 129 old owned roots.
The Goby fixture remained equal with 83 sessions, 70 devices, and 185
activity entries. The observer and independent audit record no original
implementation-byte or reference-database reads. The independent audit
itself issued no business HTTP requests.

The earlier [observer 01 failure and independent recovery](nextup-global-baseline-observer-01-recovery.md)
remain unchanged. The original tuple-header failure and earlier failed
assertions remain recorded in their consumed scopes. This accepted result
belongs to the new observer-02 scope.

## Private handoff evidence

The complete baseline, authentication closure, raw responses, and private
state remain on `test-env`; their contents are not copied into the repository.
The safe independent terminal carries the exact descriptors needed for the
next operator.

| Private evidence under `/opt/goby-test/exec-work-m3e` | SHA-256 |
| --- | --- |
| `reference-nextup-global-baseline-observation-02/private/public-baseline.json` | `92e7d5be48111d73e63a4afd1fbd97b528a9985f7c65f0f636fe112fbbde468c` |
| `reference-nextup-global-baseline-observation-02/private/closed-authentication.json` | `9de2bda283f84b429cdb1247efb9b53be1f551058f18a0d7b887a855d34ffd87` |
| `reference-nextup-global-baseline-execution-02/completed-scope-files.json` | `7b308ad6f9491db05db0de50c12a234526bea36a4f049c30cce4bc19fc1d607c` |
| `reference-nextup-global-baseline-execution-02/wire-index.json` | `8651ac98cfb8761ab976a75af512d4bdf49a8a360650f38ca6ea144cb3d7f3cb` |

The next planned operation at this acceptance checkpoint is fresh
preparation 03, using the separately verified 140-guard preparation source
and the independent matrix evidence parent
`/opt/goby-test/exec-work-m3e/nextup-global-reference-runs-03`. Its execution,
owned fixture creation, calibration, cleanup, and independent release must
be established in that new scope. These observer receipts do not establish
a released matrix fixture or a completed NextUp matrix.

## Byte-exact safe report copies

The eight reports below were copied only from the designated safe outputs,
and each local SHA-256 matches the remote source bytes. The preservation
stdout files contain JSON and retain their exact bytes under `.json` names.
The independent terminal contains descriptors, metadata, counts, and
attestation results; private baseline and closure contents are excluded.
Archiving these reports did not run tests or send business HTTP requests.

| Safe local report | SHA-256 |
| --- | --- |
| [Input assembly](nextup-global-baseline-observer-02-input-assembly.json) | `280c624e3713b70988c16f4f1dcdad6d887804b949829cd7a6f616444e9ce400` |
| [Frozen plan](nextup-global-baseline-observer-02-plan.json) | `3715c70a9def1d6d123158b97adcd5c489a72bf8c2359b11deaec3818f4a901f` |
| [Read-only admission](nextup-global-baseline-observer-02-admission.json) | `fbbb14e4769b855be7ecaf1cfb61a8e373b1d2b3cd101bba6f18a331039642f8` |
| [Before-preservation summary](nextup-global-baseline-observer-02-preservation-before.json) | `5cf01a628ebb9f56ff01dbef07fcc7bbca5e59cdc1b18c4afdf6cddbefea80f7` |
| [After-preservation summary](nextup-global-baseline-observer-02-preservation-after.json) | `4b14f70dc88bed7fa5d9427dd640d60bde22ed18d945458982a6a1c5461dbd4b` |
| [After-preservation command result](nextup-global-baseline-observer-02-preservation-after-run.json) | `74044fc248aa88c0de9907b69eaa334ad22d428275b586ec148925dd52586ad2` |
| [Observer terminal](nextup-global-baseline-observer-02-terminal.json) | `090ec380b09c98a8ec7e3335622af93e7059b19c3a87dc2f2c1766f05e86aa4f` |
| [Independent terminal](nextup-global-baseline-observer-02-independent-terminal.json) | `96dd725262f89cb788ee1258280e668f1d375b044688692ec122ae3450654901` |
