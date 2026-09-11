# Library permission UI execution contract

This contract defines the gate in
[the permission UI plan](m3e-library-permission-ui-plan.md). Its consumed run
passed the declared behavior after explicit reloads; see
[the execution result](verification-m3e-library-permission-ui.md). The contract
itself is not an execution result. The accepted prerequisite was Home v3, report SHA-256
`a8117846d80eeed8e714b8951103632acfc05105d8a14c362e99f0f9d9e62270`,
browser report `a5e4cd3a16c625c85b459c20c2349375a22bf63d889d7ac2e89f0e9237a91e28`,
and private after-snapshot
`8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a`.
The frozen input fixture had 71 authentication rows, 61 devices, 157 audit rows,
26 play rows, seven UserData rows and B revision 3.

## Ownership and input

Let `W=/opt/goby-test/exec-work-m3e` and
`R=W/client-library-permission-ui-v1`. The browser owns `R/browser`;
the controller owns the remaining records. The dedicated Node unit is
`goby-client-library-permission-ui-v1.service`. No existing output, failed
scope, browser storage, administrator token or ordinary token is adopted.

Publish each live coordination record atomically: write its fixed adjacent
`.pending` file with exclusive creation, flush and fsync complete bytes, create
the final name with a non-overwriting hard link, fsync the directory, then
remove only the matching owned pending inode and fsync again. A reader may
briefly wait for a final file with two links and its matching pending inode;
it parses only a complete final file with one link and no pending name. A
foreign file, duplicate publication or invalid completed record is rejected.
The pending state has a short deadline and never authorizes a policy action.

The browser has a 350-second work deadline and an independent 170-second abort
coordination window followed by its existing bounded cleanup. The Node worker
has a 660-second runtime limit; the controller observes its termination within
700 seconds. Only this fixed permission actor allows three WebSocket handshakes
for the initial page and two reloads. Existing Home/cross-user limits stay at
two; authentication, active-connection and message limits are unchanged.

The controller creates `R/input.json`. Its exact top-level keys are
`marker`, `version`, `mode`, `root`, `output`, `actor`, `candidate`, `fixture`,
`expected_libraries`, `source_closure`, `authority` and `controller`.
Use marker `goby-client-library-permission-input-v1`, version 1, mode
`b-home-permission-reload`, root R and output `R/browser`. Actor, candidate,
fixture, library records and process identities retain the Home v3 field
shapes. Actor is B only. Administrator credentials never enter this input.

Authority has exactly `home_report`, `home_browser`, `current_snapshot` and
`before_snapshot` descriptors. The first three reference the accepted v3
artifacts above; the last references the new complete `R/before-full.json`.
Source closure includes the permission driver, its Home observation helpers,
shared browser guard, three fixture/session modules, the new controller and
every imported frozen Python helper. Pin every source before execution.

Derive the restricted library list by removing only original Movies
`a9993591e72f0f2e7babcbf8b9c50790` from the exact four input libraries.
Extras Movies, Music and TV remain. No client PlaybackInfo, media playback,
Movie navigation, scan, preference, UserData or Policy mutation is allowed.

## Stage records

The Node process writes these exclusive, private files in order:

1. `browser/stage-baseline.json`
2. `browser/stage-restricted.json`
3. `browser/stage-restored.json`

Each stage record has marker `goby-client-library-permission-stage-v1`,
version 1 and exactly these additional keys: `input_sha256`,
`source_closure_sha256`, `controller`, `node_process`, `name`,
`token_sha256`, `session_private`, `previous_control_sha256`, and
`observation`. Names are `baseline`, `restricted` and `restored`.
The baseline previous-control digest is null; subsequent digests identify
the actual consumed controller record. `session_private` is the immutable
descriptor of the new B login receipt, using the proven Home session format.
The token digest is constant across all stages.

Baseline observation contains `dom` and `views`. Later observations contain
`spontaneous` and `reload`. Reload contains the exact action, DOM and Views
evidence used by Home v3. Spontaneous observation contains the actual bounded
window, its DOM observations, actual Views evidence and an outcome such as
`not_observed_within_window`. An absent request or cached state is recorded,
not promoted to a fresh response. Correct expected membership is required
after each explicit reload. The original Movies card must be absent while
restricted. Record other visible text without confusing it with a card.

The controller writes these exclusive, private files in order:

1. `control-restricted.json`
2. `control-restored.json`
3. `control-close.json`

Each control record has marker `goby-client-library-permission-control-v1`,
version 1 and exactly these additional keys: `input_sha256`,
`source_closure_sha256`, `controller`, `node_process`, `name`,
`previous_stage_sha256`, `revision`, `write_completed_at`,
`expected_libraries` and `restoration`. Names are `restricted`, `restored`
and `close`. Revision is the fresh native revision string, never a fabricated
number. For restricted/restored controls, `write_completed_at` records the
native write acknowledgement time. Restoration is `pending`, `confirmed`
or `not_required`. A normal close requires `confirmed`; closure before any
restriction can use `not_required` with revision and write time null.

If restoration loses its HTTP acknowledgement but a fresh readback and the
owned audit prove revision r+2 and restored values, a failure-only close may
use `confirmed`, that real fresh revision and a null write time. It cannot
publish a successful restored stage or claim an acknowledgement timestamp.
An early valid close is a controller-failure signal: the browser enters abort
cleanup, validates its process/input/last-stage bindings and consumes it only
for closure. It must not continue waiting for a successful restored control
that the failed controller will not publish. Normal controls retain their
real acknowledgement-time requirement; the overall UI result remains failed.

The normal previous-stage links are baseline to restricted, restricted to
restored, and restored to close. On failure, close may link to the last
validated stage, or null if no stage completed. Other stage ordering cannot
authorize a new write. An exclusive `browser/abort.json` carries marker
`goby-client-library-permission-abort-v1`, version 1, the same input/source/
process bindings, `stage` and a sanitized `failure` code. It requests cleanup
only and never authorizes restriction or a retry.

Arm passive observation before publishing each ready stage, so requests
arriving while the controller is writing are retained. Delimit the declared
post-acknowledgement observation window using the actual write time; preserve
earlier observations separately. Observe ten seconds without a UI action,
then perform exactly one ordinary `page.reload()` per changed-policy phase.
The request, physical response, token and visible-state checks remain separate.

## Controller and failure behavior

Before launching Node, acquire the existing fixture lock, confirm the accepted
v3 authority, capture a fresh complete database/media baseline and verify the
candidate process/source/private inputs. Do not invoke a consumed Home entry
point or an old API operator. Reuse only reviewed pure comparison functions
and read-only snapshot helpers.

Before restriction, require the valid baseline stage, proven new B credential
and a separately proven new native administrator session. Read the managed B
account with a fresh revision, save the original supported values and a durable
conditional restoration reservation, then issue at most one restriction PUT.
Read back the revision/account and bind the owned `user.updated` audit before
publishing the restricted control. Before restoration, reconcile the current
account and owned audit; restore only the acknowledged restricted state using
its fresh revision. Read back and prove restoration before publishing control.
No unowned intervening change may be overwritten.

After an observation error or abort, reconciliation/restoration and both owned
session cleanups have independent bounded opportunities. Normal restriction
and restoration are each attempted at most once. Keep the browser alive for
restoration before releasing it for UI logout. If coordination becomes
unavailable or restoration cannot be proved, retain an explicit failed result;
never infer restoration, successful UI logout or a completed permission gate.
No unbounded waits or surviving polling timers are allowed. Final worker
termination and empty cgroup must be observed before any fallback B cleanup.

Only a proven new B private receipt can authorize its exact fallback logout;
fallback leaves UI acceptance failed. Native administrator login ownership
must be retained before response journaling, and its pre-reserved cleanup
must remain available after journal errors. Raw credentials and response
snapshots remain private on test-env.

Take complete after snapshots and media witnesses even on failure. For the
normal successful run, expect two new sessions, one new browser device, four
authentication audits and two owned account-update audits. Preserve every
other old row, allowing B only its declared Policy/revision/update-time
changes and the new session/device only their proven capabilities and Touch.
Require restored supported account values and original Policy, no new play/
UserData/reference/encoding rows, exact owned cleanup, and correct terminal
log digests. This gate can establish observed permission UI behavior, not
completion of all M3/M4/M5/M6 work or an unobserved automatic update guarantee.

The browser report marker is `goby-client-library-permission-report-v1` and the
controller marker is `goby-client-library-permission-observation-v1`. The
browser retains `baseline`, `restricted` and `restored` stage observations and
one B `actor`. A completely successful defined flow uses outcome
`permission_observation_after_explicit_reload`, `permission_ui_acceptance=true`
and `client_acceptance=false`. Passive-window findings remain independent;
manual reload success must not turn an absent automatic update into success.
