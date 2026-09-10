# Managed settings persistence and operation

**M5g native settings increment accepted.** The current native implementation adds
the [three settings APIs](../api/settings.md) and an administrator Settings page.
The full native Go race suite, isolated browser/restart workflow, protected
deployment, and main-service workflow have passed. The accepted live service is
PID `3614026`, schema **20**, probe **6**. The Emby ConfigurationService adapter
remains unimplemented; native acceptance does not establish that compatibility
surface or complete the wider administrator milestone.

## Defaults and durable overrides

`internal/settings` manages exactly `ServerName`, `MaxBitrate`, `MaxWidth`,
`MaxHeight`, and `MaxAudioChannels`. Startup resolves built-in/environment
configuration and validates these values before constructing the store. The
store freezes those defaults for its lifetime. For each field, a non-null
database override wins; a null override uses the frozen deployment default.
The API reports `Defaults`, `Overrides`, `Effective`, and `Sources` separately.
An explicitly saved value equal to a default still has source `database`.

The name defaults to `Goby`; output defaults are 20,000,000 bits/second,
1920 by 1080 pixels, and eight audio channels. Their environment variables and
shared validation bounds are in the [API field table](../api/settings.md#managed-fields-and-response).
Server names preserve valid text, must be nonblank and at most 128 UTF-8 bytes,
and cannot contain NUL. Numeric zero never means automatic/default. Validation
also applies when conversion is disabled; limits can be saved for later use.

Migration [0020_managed_settings.sql](../../internal/database/migrations/0020_managed_settings.sql)
adds one table and inserts exactly one row: `id = 1`, revision `1`, five NULL
overrides, and creation/update timestamps. Constraints enforce the singleton,
positive revision, nullable overrides, and storage bounds. The existing
`server_settings` identity table is retained. No environment defaults or
deployment secrets are copied into the new table. The preceding 27 tables
retain their old rows; migration history gains only its schema-20 entry.
Schema 20 has 28 public tables. Probe cache version 6 is unchanged and this
migration requires no media rescan.

Restarting with changed environment defaults recomputes null-backed effective
values without modifying the stored row, revision, or timestamps. Explicit
overrides survive unchanged. A later reset clears selected overrides and uses
the current process's deployment defaults. Reproducing an effective setup
therefore needs both stored overrides and the intended deployment configuration.
This does not provide a product backup/restore or configuration export workflow.

## Authorization, commit, and publication

Production startup acquires catalog ownership and loads/validates the singleton
before exposing the HTTP handler. A missing or invalid stored row fails startup;
the store does not silently replace it with defaults. Direct SQL edits are not
a supported hot-reload mechanism. The store is a publisher for one server
process, not a distributed settings synchronization service.

Native reads and mutations use the existing owned-transaction boundary and
current native administrator audience. Mutation authorization is checked before
locking the settings row, again after the lock, and as the final database
operation before commit. Credential expiry, revocation, account disablement,
and authority changes cannot be bypassed by waiting on the business-row lock.
The settings domain does not receive transaction-control or raw connection
capabilities.

The writer holds a publication mutex from transaction entry through commit and
runtime publication. It locks the singleton, compares the supplied revision,
and verifies that stored overrides match the published state. A stale revision
fails with `409`; unexplained database/runtime divergence is an operational
`503`. A real change increments the revision once. A no-op still checks revision
and authority but leaves revision and timestamps untouched. A failing validation,
authorization, or transaction does not publish proposed values.

After a successful commit, publication synchronously replaces an immutable
snapshot through an atomic pointer. HTTP cancellation after commit cannot skip
that publication step. A crash in the commit/publication gap is recovered by
loading committed overrides at startup. Publication is monotonic, and returned
override pointers are copies; an older writer or caller cannot mutate a newer
published snapshot. Native settings GET waits for publication and returns the
same published state used by runtime readers, rather than a separate SQL view.

Runtime snapshot reads perform no settings I/O and do not wait for the writer
mutex. Each HTTP request captures one revision and all five effective values
at entry. That request keeps a consistent snapshot through a concurrent change;
requests beginning after publication receive the new snapshot. Mutation
responses explicitly use the committed result rather than their older
request-entry snapshot. A response lost after commit is an uncertain outcome,
not evidence that the database rolled back.

## Runtime application boundary

The server name feeds public system information and administrator overview.
New application keys snapshot that effective name for their server credential
and default client. Renaming does not rewrite older credential/client metadata
or server identity. Other historical records keep their own snapshots.

For new output planning, handlers copy startup transcoding configuration and
replace only the four effective output ceilings. PlaybackInfo, Universal/audio,
progressive video, and unregistered HLS requests use this request-local copy.
Source facts, requested codec/profile constraints, user permissions, and the
enabled execution service still determine whether a plan is constructible.
Restrictive saved limits can leave no supported output; saving them does not
prove that arbitrary media will convert. Bitrate is an encoded-output planning
ceiling, not a network bandwidth limiter.

Already registered HLS outputs retain their concrete dimensions, codecs,
bitrates, and execution settings. Lowering settings does not replan or retire
them. Their existing source, credential, playback ownership, expiry, and
conversion-permission checks still apply. Progressive negotiation is different:
its PlaybackInfo URL carries output choices but reserves no producer. A later
GET or HEAD constructs a plan using the later request's snapshot and current
source/policy, so an intervening settings update can change or reject that
request; exact old URL choices need not be automatically reduced to fit.
Once a concrete producer is registered, it retains its plan. Identical concrete
plans may reuse an existing matching scope, so a settings revision alone does
not promise a new session or encoder job.

Hardware backend/device selection, transcoder enablement, threads, concurrency,
queues, cache paths, storage budgets, and other resource configuration remain
startup-only. Settings neither reconfigure the manager nor inspect or launch
hardware. The API exposes only seven safe read-only deployment values:
`TranscodingEnabled`, `HardwareDecoder`, `HardwareEncoder`, `Threads`, `MaxJobs`,
`MaxUserJobs`, and `MaxSessionJobs`. It does not serialize the complete Config
object. See [transcoding configuration](transcoding-configuration.md) for those
deployment controls; the five managed fields now use the override precedence
described here. Actual GPU execution remains outside this increment's evidence.

## Administrator workflow

The Settings page shows each saved effective value, its source, and the current
deployment default. An override switch selects an explicit value or fallback;
Save sends the complete five-field set. Bitrate is displayed in Mbps and parsed
with at most six decimal places into exact integer bits per second, avoiding
floating-point rounding. The hardware/resource section is read-only.

Reset opens a selection dialog for saved overrides. Cancelling makes no change;
confirming clears only selected fields. Unselected unsaved drafts remain in the
form. Dirty navigation, reload, and discard have explicit confirmation flows.
After a revision conflict, network/invalid-response failure, or server failure,
the UI keeps the last confirmed values and draft but blocks further mutations
until an explicit reload. It does not blindly retry a potentially committed
write. Session expiry follows the existing native authentication flow. The page
is server administration and introduces no consumer playback UI.

## Evidence available at this checkpoint

- The [complete native race run](m5g-native-full-race.json) passed **1222 top-level
  tests across fourteen tested packages**, with zero skipped tests, no race
  findings, and Go exit 0. `cmd/goby` has no test files. The
  [first attempt](m5g-native-full-race-attempt-1.json) retains its 1219 passes and
  three missing-fixture failures; the successful second snapshot restores all
  ten internal test-data files while preserving the same 419 Go/module/SQL inputs.
  The [verification report](verification-m5g-settings.md) records the final source
  gate and its binding to the browser candidate and 50 dashboard assets.
- [Core checks](m5g-core-tests.json): 284 top-level tests passed across config,
  database, settings, and library packages, with Go exit 0 and no race warning.
- [Native HTTP checks](m5g-settings-http-tests.json): 18 top-level tests passed,
  covering DTOs, strict input, authorization/CSRF, CAS, request snapshots, and
  transaction failures. The [three subsequent checks](m5g-settings-media-tests.json)
  cover the NUL boundary and media/name integration; these are checkpoint
  reports, not additional unique tests to add to the full-suite count.
- Real FFmpeg HLS evidence retains an old registered 160 by 90 output and its
  identical media bytes after reducing limits, while a fresh scope produces
  96 by 54 output under the new ceilings. The old producer may already have
  finished; this does not claim a settings change during active encoding.
- The [browser workflow](m5g-settings-browser.json) passed its first attempt in
  **6.280795 seconds**. It exercises defaults, all five overrides, exact Mbps,
  resets, conflicts, uncertain committed responses, mobile layout, and navigation.
  Two isolated restarts preserve all 28 public tables exactly and retain the
  native login. The second changes deployment defaults for ServerName, MaxWidth,
  and MaxAudioChannels; only null-backed effective values change, while the
  saved bitrate/height overrides and revision survive. A final reset uses the
  replacement defaults. All cleanup checks pass and the shared service is unchanged.
- [Deployment](m5g-deployment-evidence.json) passed with the accepted binary,
  419 Go/module/SQL inputs, and 50 current assets. A complete protected backup
  preceded service stop; an isolated restore verified all 27 old tables exactly
  and was removed. Old business rows, server identity, task history, master,
  runtime/unit configuration, and media were preserved. The new singleton
  initially had revision 1 and five NULL overrides.
- The [main-service workflow](m5g-deployed-settings.json) passed its first attempt
  in **0.701 seconds**: 15 GETs, one POST, two PUTs, one DELETE, no transport
  retries, and 19 separate read-only verifier SQL queries. One new native cookie
  temporarily saved all five overrides, verified public-info/overview names
  and published values, restored the original nullable overrides with CAS,
  and was logged out with an independent `401` and final SQL barrier. Old rows
  in all 27 preceding tables remain exact; the new session is retained revoked.
  A separate [read-only observation](m5g-post-workflow-settings-state.json)
  confirms revision 3 and five NULL overrides; only the settings row's
  revision and update timestamp advance. This workflow performs no main restart,
  planning, playback, media, scan, key, or Emby configuration operation.

Four safe screenshots record [defaults](screenshots/settings-defaults-desktop.png),
[overrides](screenshots/settings-overrides-desktop.png),
[reset](screenshots/settings-reset-desktop.png), and
[mobile layout](screenshots/settings-mobile.png). The independent
[configuration read study](../research/configuration-reference.md) and later
[fresh mutation study](../research/configuration-mutation-reference.md) supply
bounded Emby observations. A sampled partial update can return `500` after a
visible name change; the three key-authorized writes are baseline no-ops only.
These results do not establish complete write semantics or compatibility
implementation. The ConfigurationService adapter, broader configuration,
M4, M5, M6, and the complete goal remain unfinished.
