# Managed settings persistence and operation

**M5h increment complete and deployed: schema 21/probe 6.**
The [three native settings APIs](../api/settings.md) now share schema-21 state
with the [ConfigurationService adapter](../api/configuration.md).
Four name modes, an independent encoding width, and the native UI extension
are described here and in [configuration compatibility](configuration-compatibility.md).
The complete remote suite, browser/restarts, reference execution study,
protected deployment, and main workflow have passed for this increment.

The [accepted live deployment](m5h-deployment-evidence.json) is PID `3641418`,
UID `995`, start ticks `26048863`, with binary SHA-256
`62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54`.
It replaces the earlier M5g/schema-20 PID `3614026`. The historical M5g evidence
below remains tied to that earlier candidate. M4, M5, M6, broader configuration,
and full Emby compatibility are not complete.

## Defaults and durable overrides

`internal/settings` retains five native override fields: `ServerName`,
`MaxBitrate`, `MaxWidth`, `MaxHeight`, and `MaxAudioChannels`. Schema 21 adds
`ServerNameMode` and separate `Encoding.TranscodingMaxWidth` state. Startup
resolves and validates built-in/environment defaults, reads the actual host
name through `os.Hostname()`, validates it, and freezes both for the store's
lifetime. The API reports defaults, raw overrides, effective native values,
sources, name mode, and encoding state separately.

For native numeric fields, a non-null override wins and null uses the deployment
default. For names, deployment mode requires raw null and uses the deployment
name; custom requires a valid nonempty string; empty stores `""` and unset stores
null, both using the host name publicly. The compatibility projection retains
empty versus omission. `Sources.ServerName` is `database` for custom, empty,
and unset, even when the raw value is null. It is `deployment` only in deployment
mode. An explicit custom value equal to the deployment name remains custom.

The name defaults to `Goby`; output defaults are 20,000,000 bits/second,
1920 by 1080 pixels, and eight audio channels. Their environment variables and
shared validation bounds are in the [API field table](../api/settings.md#managed-fields-and-response).
Custom names preserve valid text, must be nonblank and at most 128 UTF-8 bytes,
and cannot contain NUL. Empty/unset are explicit states rather than invalid
custom names. Zero is invalid for the four native numeric overrides, while
zero encoding width means no additional width ceiling. Validation also applies
when conversion is disabled; saved limits do not enable the transcoder.

Migration [0020_managed_settings.sql](../../internal/database/migrations/0020_managed_settings.sql)
introduced one table and exactly one row: `id = 1`, revision `1`, five NULL
overrides, and creation/update timestamps. Constraints enforce the singleton,
positive revision, nullable overrides, and storage bounds. The existing
`server_settings` identity table is retained. No environment defaults or
deployment secrets are copied into the new table. The preceding 27 tables
retain their old rows; migration history gains only its schema-20 entry.
Schema 20 has 28 public tables. Migration
[0021_configuration_compatibility.sql](../../internal/database/migrations/0021_configuration_compatibility.sql)
extends that singleton without adding a table: old null names become deployment
mode, old non-null names become custom, and `compatibility_max_width` starts at
zero. It preserves the old five values, revisions, and timestamps, and enforces
mode/raw-name consistency plus the `0..8192` extra-width range. Probe cache
version 6 remains unchanged; these settings migrations require no media rescan.
The [M5h deployment](m5h-deployment-evidence.json) verified this exact backfill,
including preservation of the old singleton values, revision, and timestamps.

Restarting with changed environment defaults can change inherited numeric
values and deployment-mode names. A changed startup host name can change the
effective empty/unset name. Neither event writes the row or advances revision
or timestamps. Explicit modes, raw names, numeric overrides, and extra width
survive. A name reset chooses deployment mode; a numeric reset resumes its
deployment default; extra-width reset sets only that value to zero. Reproducing
an effective setup therefore needs the managed state and intended deployment/
host configuration.
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
and verifies that stored overrides, name mode, and encoding state match the
published snapshot. A stale native revision fails with `409`; unexplained
database/runtime divergence is an operational
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
mutex. Each HTTP request captures one revision, all five effective native values,
and the independent compatibility width at entry. That request keeps a consistent
snapshot through a concurrent change; requests beginning after publication
receive the new snapshot. Mutation
responses explicitly use the committed result rather than their older
request-entry snapshot. A response lost after commit is an uncertain outcome,
not evidence that the database rolled back.

The compatibility adapter shares this store and revision. Its writes carry no
client revision and transform only the selected section of the latest locked
row: Full name omission means unset, Partial name omission preserves it, and
named encoding omission sets only the extra width to zero. Other native fields
are retained. A real mode/encoding change advances the revision even when raw
native overrides or the public name look unchanged. Native PUT remains CAS
and replaces all five native override choices, optionally changing mode and
encoding atomically. See the [compatibility contract](configuration-compatibility.md)
for transactional read-only initialization validation and error precedence.

## Runtime application boundary

The server name feeds public system information and administrator overview.
New application keys snapshot that effective name for their server credential
and default client. Renaming does not rewrite older credential/client metadata
or server identity. Other historical records keep their own snapshots.

For new output planning, handlers copy startup transcoding configuration and
replace the four native effective ceilings. A positive compatibility width then
limits width to `min(Effective.MaxWidth, Encoding.TranscodingMaxWidth)`; zero
leaves native width in force. Native `Effective.MaxWidth` still reports the
native value, not the combined runtime ceiling. PlaybackInfo, Universal/audio,
progressive video, and unregistered HLS requests use this request-local copy.
Source facts, requested codec/profile constraints, user permissions, and the
enabled execution service still determine whether a plan is constructible.
Restrictive saved limits can leave no supported output; saving them does not
prove that arbitrary media will convert. Bitrate is an encoded-output planning
ceiling, not a network bandwidth limiter. The extra-width composition is an
explicit Goby policy. The [Emby 4.9.5.0 4K study](m5h-encoding-width-reference.json)
produced 1280 by 720 output for width 1280 and 3840 by 2160 for zero, each with
eight fully decoded software-encoded frames. This is bounded execution evidence,
not a universal unlimited-width rule or proof of missing-field reset behavior.

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
hardware. The API exposes eight safe read-only deployment values: startup
`HostName` plus `TranscodingEnabled`, `HardwareDecoder`, `HardwareEncoder`, `Threads`, `MaxJobs`,
`MaxUserJobs`, and `MaxSessionJobs`. It does not serialize the complete Config
object. `HostName` is read from the operating system, not written through this
API. See [transcoding configuration](transcoding-configuration.md) for execution
controls. Actual GPU execution remains outside this increment's evidence.

The response has nine top-level fields: `Revision`, `ServerNameMode`, `Defaults`,
`Overrides`, `Effective`, `Sources`, `Encoding`, `UpdatedAt`, and `Deployment`.
The four five-field maps retain their native meaning; `Encoding` is separate.
Native PUT still requires a canonical decimal revision and every native override.
Optional `ServerNameMode` must match the raw name. Omission retains schema-20
inference (null to deployment, non-null to custom), so an old-style update can
replace unset with deployment and cannot save an empty string. Optional Encoding
must contain exactly one integer `TranscodingMaxWidth`; omission preserves it,
while null or `{}` is invalid. Native requests retain their exact field casing,
16 KiB lossless JSON, no-query, and cookie/CSRF rules.

## Administrator workflow

The Settings page shows each saved effective value, its source, and the current
deployment default. Name choices are Deployment default, Host name, and Custom
name. Host name presents empty/unset together; loading and editing another field
preserves the exact saved mode, while explicitly choosing Host name selects
empty. Numeric override switches select explicit values or deployment fallback.
The additional-width field shows its current value and combined width without
changing the native width. Save sends all five overrides plus explicit mode
and Encoding state. Bitrate is displayed in Mbps and parsed
with at most six decimal places into exact integer bits per second, avoiding
floating-point rounding. The hardware/resource section is read-only.

Reset accepts one to six unique names: `ServerName`, `MaxBitrate`, `MaxWidth`,
`MaxHeight`, `MaxAudioChannels`, and `TranscodingMaxWidth`. The last selector is
not `Encoding.TranscodingMaxWidth`; `ServerNameMode` is not a reset selector.
Name reset returns to deployment; width reset affects only the selected native
or extra ceiling. The dialog offers saved changes; cancelling makes no change.
Confirming resets only selected fields. Unselected unsaved drafts remain in the
form. Dirty navigation, reload, and discard have explicit confirmation flows.
After a revision conflict, network/invalid-response failure, or server failure,
the UI keeps the last confirmed values and draft but blocks further mutations
until an explicit reload. It does not blindly retry a potentially committed
write. Session expiry follows the existing native authentication flow. The page
is server administration and introduces no consumer playback UI.

## Historical M5g acceptance: schema 20

The following runs belong to the accepted M5g candidate before four-state names,
the extra width, and ConfigurationService were added. They remain evidence for
that snapshot; their passing results must not be reused as M5h acceptance.

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

Four historical M5g screenshots record [defaults](screenshots/settings-defaults-desktop.png),
[overrides](screenshots/settings-overrides-desktop.png),
[reset](screenshots/settings-reset-desktop.png), and
[mobile layout](screenshots/settings-mobile.png).

## Current M5h acceptance and remaining scope

The [final source gate](m5h-final-go-source-gate.json) binds the accepted binary
to 429 Go/module/SQL inputs, ten test-data files, and 50 current dashboard assets.
The [complete remote race run](m5h-full-race.json) passed **1252 top-level tests
across fourteen tested packages**, with no failed or skipped top-level tests
and no race warnings. `cmd/goby` has no test files. Targeted checkpoint counts are subsets
of this full result, not additional unique tests.

The [browser journey](m5h-configuration-browser.json) passed 16 browser checks
in **7.808112 seconds**, with no skips or retries. It covers mode-aware drafts,
independent encoding width, reset, conflicts and uncertain responses. Each of
two isolated restarts preserved all 28 public tables exactly. Unset mode kept
host-name fallback when deployment defaults changed; saved bitrate, height and
encoding width survived, and null numeric overrides used the new defaults.
Both issued native credentials were revoked during cleanup and the then-current
shared M5g service was unchanged. These were isolated restarts before deployment.
The M5h screenshots show [defaults](screenshots/m5h/settings-defaults-desktop.png),
[saved overrides](screenshots/m5h/settings-overrides-desktop.png),
[reset](screenshots/m5h/settings-reset-desktop.png), and
[mobile layout](screenshots/m5h/settings-mobile.png); the M5g images above remain
separate historical artifacts.

[Deployment](m5h-deployment-evidence.json) and the main workflow passed their
first attempts. The complete backup at `/opt/goby-test/backups/m5h-20260910`
preceded service stop. An actual isolated restore verified all **28 old tables**
as complete raw rows, then removed its temporary database. No live database
restore was executed. Deployment preserved old business fields, server identity,
task history, the master, runtime/unit configuration, and all eleven media
files; only the migration's new fields and history were added.

The [main workflow](m5h-deployed-configuration.json) passed in **0.901 seconds**
using 23 GETs, five POSTs, one PUT, one DELETE and 25 separate forced-read-only
verifier queries. One fresh native cookie and one fresh ordinary Emby token
verified a Partial name change and named encoding change, each advancing the
shared revision. After revoking the non-CAS Emby writer, native CAS restored all
original overrides, deployment name mode and zero encoding width. Both logouts
have independent `401` and SQL revocation barriers. The two new revoked sessions
and one ordinary device remain as history; no Touch timestamp changed in this
run. All old rows in the other 27 tables are exact. Only the settings revision
and update timestamp remain advanced. The main process, master, private inputs,
media and NFO evidence stayed unchanged, with no planning, playback, scan, task
execution or additional main restart.

The [reference execution report](m5h-encoding-width-reference.json) adds 61
sanitized records to the earlier 2305, bringing the reference corpus to **2366**.
It retains the bounded 4K software-output evidence described above and confirms
producer stops, credential invalidation, baseline restoration and fixture cleanup.

The independent
[configuration read study](../research/configuration-reference.md) and later
[fresh mutation study](../research/configuration-mutation-reference.md) supply
bounded Emby observations. A sampled partial update can return `500` after a
visible name change; the three key-authorized writes are baseline no-ops only.
Those earlier observations do not establish complete upstream write semantics.
The [accepted compatibility increment](configuration-compatibility.md)
documents its closed fields, deliberate atomic rejection, and remaining
consumers. M5h is complete within that scope. Broader configuration, M4, M5, M6,
all Emby APIs, and the complete goal remain unfinished.
