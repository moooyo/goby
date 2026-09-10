# Implementation progress

The goal remains the complete planned Linux backend and administrator dashboard. A completed engineering increment does not establish full Emby compatibility.

## Decisions

- Develop directly on `main`; commit and push each completed increment.
- Use PostgreSQL exclusively; the earlier SQLite proposal is superseded.
- Pin Go **1.27.1** and FFmpeg **9.0.1**, reconfirmed from official stable-release sources on 2026-09-10.
- Include explicit hardware **decode** and **encode** selection and evidence; software-only success is not GPU verification.
- Local compilation/build checks are authorized. Execute functional tests, runtime probes, media tests, and browser tests through `ssh test-env`.

## Increment status

| Increment | Status | Evidence / remaining work |
| --- | --- | --- |
| Research baseline and PostgreSQL/toolchain decisions | Complete as a documentation increment | Pushed `baa3731`: pinned upstream catalog, scope, PostgreSQL architecture and toolchain provenance |
| Linux toolchain and database provisioning | Complete | Pushed `79745ce`: Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11; software media verification passed |
| M0 core reference capture | Baseline complete; broader coverage pending | Official Emby 4.9.5.0 evidence contains 2051 records. The [configuration read study](../research/configuration-reference.md) adds 86 records to the 1965 checkpoint reached by the [fresh task mutation study](../research/scheduled-tasks-mutation-reference.md). Earlier device, key, catalog, and media evidence is preserved. Configuration writes/key authority, task timer/key-auth behavior, weekly/system-event execution, DST/maximum-runtime enforcement, global NextUp selection, and hidden header-device Info/deletion remain unresolved. Reference records are separate from product acceptance |
| M1 service, identity, administrator foundation | Foundation increment complete | PostgreSQL migrations, users/sessions, setup/login, CSRF, proxy-aware rate limits, React/MUI overview/user creation, non-root Linux deployment; [verification report](verification-m1.md) |
| M2a media ingestion and browse | Complete | Pushed `90b7c2e`: safe ffprobe, bounded scans, PostgreSQL catalog/ownership, library ACL queries and React/MUI Libraries/Tasks; [verification report](verification-m2a.md) |
| M2b metadata and artwork | Local NFO, entities, and local artwork increments complete | Pushed NFO increment `6011377`; persistent entity navigation/filtering and bounded image delivery pass full Linux race tests and deployed checks; [NFO verification](verification-m2b-nfo.md), [artwork/entity verification](verification-m2b-artwork-entities.md). Generated/embedded artwork, broader metadata/query coverage and reconciliation remain |
| Startup and migration time budgets | Complete | Configurable `GOBY_STARTUP_TIMEOUT`, migration-local SQL timeout override, cancellation and connection-setting restoration verified; [verification](verification-startup-timeouts.md) |
| M3 initial client playback | Original playback/state, client sessions/NextUp, external SRT/WebVTT, and user-state events/remote-control increments complete; milestone incomplete | Full Linux race tests and deployed workflows passed; [M3a](verification-m3a-original-playback.md), [M3b](verification-m3b-sessions-nextup.md), [M3c](verification-m3c-subtitles.md), [M3d](verification-m3d-events.md). Additional events/subscriptions, broader subtitle handling, global NextUp parity and real-client acceptance remain |
| M4 conversion and hardware pipeline | Engine, HLS VOD, progressive audio/video, ordered profiles and verified video restart increments complete; milestone incomplete | [M4a](verification-m4a-engine.md), [M4b](verification-m4b-hls.md), [M4c](verification-m4c-audio.md), [M4d](verification-m4d-audio-profiles.md) and [M4e](verification-m4e-video-and-users.md) establish the preceding conversion and clock behavior. [M4f](verification-m4f-video-seek.md) adds probe 6 private restart evidence, software-decoder proof with actual threads, and independent linear audio. All 940 top-level race tests and five deployed library upgrades passed with real fast producer observation. [M4g](verification-m4g-media-refresh.md) adds explicit media re-probing, schema-15 task modes, real same-version index reconstruction and five deployed forced scans; all 969 top-level race tests pass. Nonzero copied-video seeking, efficient audio I/O, additional tracks/formats, aggregate resource isolation, actual GPU execution and full client acceptance remain |
| M5 administrator completion | User, metadata, login-session, application-key, device and initial scheduled-task increments complete; milestone incomplete | [M5a](verification-m4e-video-and-users.md), [M5b](verification-m5b-metadata.md), [M5c](verification-m5c-sessions.md), [M5d](verification-m5d-application-keys.md), and [M5e](verification-m5e-devices.md) retain preceding acceptance evidence. [M5f](verification-m5f-tasks.md) adds durable library-wide execution, schedules, receipts and an administrator task UI. All 1190 top-level race tests pass across thirteen packages with zero skips/races; browser, deployment and the main-service workflow pass. Additional task executors, full policies, providers, broader settings, audit/log browsing, product backup/restore and broader wire/client parity remain |
| M5f scheduled tasks | Initial library executor and scheduling increment complete | [Verification](verification-m5f-tasks.md): 1190 full-race tests, isolated browser acceptance with two restarts, schema-19 deployment, and a successful main-service run/replay/schedule workflow. Eight [native task routes](../api/tasks.md) and six compatibility operations use durable receipts, owned children, typed schedules and recovery. [Reference read](../research/scheduled-tasks-reference.md) and [mutation](../research/scheduled-tasks-mutation-reference.md) studies retain their narrower evidence; timer/reference parity, more executors, and complete client acceptance remain broader work |
| M5g configuration | Read-only reference increment complete; native implementation in development | [Study](../research/configuration-reference.md): 85 complete HTTP exchanges plus one audit, permission/field observations, preserved prior evidence, and [18 pure recorder guard tests](m5g-configuration-recorder-tests.json). No configuration writes or key-authority requests were sampled. Product acceptance is pending; M5f remains deployed at schema 19/probe 6 |
| M6 compatibility release | Incomplete; acceptance pending | Client/reference comparisons, Linux distribution/architecture/GPU matrix, operations, representative large-catalog upgrade timing, and recovery evidence |
| M7 additional features | Deferred per scope | Explicit feature decisions and their own acceptance gates |

## User-device reference increment

The [device study](../research/devices-reference.md) adds 346 complete HTTP
exchanges and two audits in two preserved datasets. The first contains 153
records and is partial: its post-delete Info guard expected `404` but observed
`204`, so the recorder stopped before the next mutation. The second contains
195 records and completed in 7.207 seconds. Both cleanup audits pass. The
recorder's synthetic safety suite now contains 19 passing tests.

The observations distinguish a global reported-device registry, decimal-string
wire IDs, custom-name clearing, device-wide revocation of the tested ordinary
logins, and re-registration with a new ID and cleared options. All 240 known
source paths, preceding raw/export and private files, old device options, and
user policies are preserved. Control devices `21` and `25` remain as documented
history with their login credentials revoked. The reference's zero totals and
ignored sort requests remain separate observations. These captures are research
evidence; product acceptance is recorded separately. The key-device study
below supplies bounded shared-server deletion evidence. At that research
checkpoint, the deployed product was M5d `563cd0e`, schema 16, with its separate
1046-test acceptance; current M5e results are recorded below.

## Application-key/device reference increment

The [completed reference study](../research/key-devices-reference.md) adds 189
records: guarded original-instance capture with 61 HTTP exchanges and one audit,
fresh setup with 16 complete HTTP exchanges and one non-HTTP readiness failure,
109 complete fresh-study HTTP exchanges, and one independent recovery audit.
The corpus reached 1665 at that checkpoint; the derived operator teardown is
not an extra record.

The original-instance gate preserved a hidden old server device and created no
key. A wholly owned fresh instance then showed two keys sharing server-device
`5`; deleting it emptied the key list and made all five tested key scopes
unauthorized while the ordinary control remained usable. Three metadata
Session DTOs persisted as stale projections. A key separately renamed and
deleted the owned ordinary viewer's device, revoking that login while both
keys still authenticated. Header-specific hidden Info/deletion and key-device
recreation were not sampled.

The original final-audit serialization failed on logical labels classified as
secrets. The offline recovery preserves that failure, verifies all 109 exports
unchanged and secret-free, acknowledges ten owned mutations, and proves final
credential invalidity. It makes no HTTP request or process launch. Old evidence,
240 sources, credentials, prior failure artifacts, and unrelated services remain
preserved; separate operator teardown removes only the fresh program data and
process. Synthetic suites pass 16 guarded-recorder tests, 28 preparation tests,
24 fresh-recorder tests, and five offline-analyzer tests.

At the research checkpoint, the initial M5e Go run reported nine failures and
product acceptance was pending. Those failures and the later legacy-regression
issues were corrected; [final M5e acceptance](verification-m5e-devices.md) now
passes. Recreating a new application-key device generation remains a Goby
safety design, not an observed reference contract. M5e completes this device
increment only; M4, M5, M6, and the complete goal remain unfinished.

## ScheduledTasks read-only reference increment

The [M5f read-only study](../research/scheduled-tasks-reference.md) adds 129
records under `scheduled-tasks-m5f-`: 128 complete HTTP exchanges and one audit.
The corpus reached 1794 records at that checkpoint. It observes 22 task definitions, with every
recorded state `Idle`, and interval, daily, startup, and system-event trigger
declarations. Tested anonymous requests return `401`, ordinary-viewer reads
return `403`, and administrator details return `200` for known IDs or `404`
for unknown IDs. No task start/stop, trigger update, restart, or key request ran.

The [audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-audit.json)
preserves all 1665 preceding records and 3330 raw/export files, 240 known
source paths, and 2214 private files. The old twenty listed devices, hidden
server device `15`, options, task definitions/configuration, and recorded
runtime state are unchanged. Both new logins are logged out and independently
denied with `401`; device rows `31` and `32` remain as revoked-login history.
The original Emby PID 3131777 and Goby PID 3535438 remain unchanged.

The [five pure recorder guard tests](m5f-scheduled-task-recorder-tests.json)
pass with zero HTTP requests or capture writes. They verify recorder guards,
not product task execution. At that checkpoint, the separate [transaction foundations](verification-m5f-task-foundations.md)
passed 21 targeted Go race tests on the isolated Linux database. They add
restricted owner transactions and live administrator authorization without
registering task routes or schedules at that stage. M5e `d5d696f`, schema 18/probe 6,
was the deployed checkpoint then; the completed M5f acceptance is recorded below.

## ScheduledTasks fresh mutation reference increment

The [fresh-instance study](../research/scheduled-tasks-mutation-reference.md)
adds 171 records: 169 complete HTTP exchanges, one audit, and one non-HTTP
readiness failure. The corpus reached 1965 at that checkpoint. Both stop forms were exercised
against observed `Running` tasks, returned `204`, and produced new `Cancelled`
results. Idle-stop returned `500`; administrator operations on the reserved
unknown ID returned `404`. Sampled legal trigger arrays returned `204`, while
unknown and mixed-invalid arrays returned `400` without installing a valid
prefix from the empty baseline.

The original 1794 records and 240 known sources are preserved. The disposable
instance used 512 independent media copies across two fixture libraries;
those copies are not additions to the permanent 240-source baseline. The
[operator cleanup](m5f-fresh-task-cleanup.json) removed the exact owned source
and program-data paths, stopped the fresh process, and retained evidence.
The old Emby and main Goby process identities remain unchanged.

[Nineteen operator safety tests](m5f-fresh-task-operator-tests.json) and
[eight recorder guard tests](m5f-fresh-task-recorder-tests.json) pass with
synthetic fixtures. These are research-safety checks. Weekly/system-event
execution, scheduled firing, DST, maximum-runtime enforcement, and reference
key authority remain unverified. Goby's first integration checks passed
[35 core tests](m5f-core-targeted-tests.json) and [44 task tests](m5f-tasks-initial-tests.json).
Those early checks were followed by the [complete M5f acceptance](verification-m5f-tasks.md):
full regression, browser/restarts, deployment, and the main-service workflow.
The reference increment remains separate evidence; its unsampled behaviors
are not promoted to observed Emby contracts by Goby's successful implementation.

## Configuration read-only reference increment

The [M5g configuration study](../research/configuration-reference.md) adds 86
records: 85 complete HTTP exchanges and one audit, recorded in 2.438 seconds
with 104,719 response-body bytes. The corpus now contains 2051 records. Administrator
total configuration has 60 fields; the viewer's `200` body is exactly `{}`.
Named encoding/devices/DLNA objects have 17, two, and three fields respectively,
with administrator `200`, viewer `403`, and anonymous `401`. An unknown name
returns administrator `500` with `Sequence contains no matching element`.

The [audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/configuration-m5g-audit.json)
preserves all preceding 1965 records and 3930 raw/export files, 240 known source
files, 2621 private files, 22 old listed devices and hidden server device `15`,
their options, and old task/user structure. The original Emby PID 3131777 and
M5f Goby PID 3570491 remain unchanged. Only the two owned ordinary login/logout
flows mutate state; new device rows `33` and `34` remain as history, and both
credentials finish with independent `401` proofs. All cleanup checks pass.

All 85 full private wire records are preserved. Export redaction covers sensitive
fields, known credentials, dictionary keys and nested headers; URL recognition
uses at most two percent-decode layers and masks values that exceed that budget.
`AllowLegacyLocalNetworkPassword` is conservatively masked, so its actual value
is not claimed. The sampled deprecated `EncodingThreadCount: -1` and
`TranscodingMaxWidth: 0` do not establish encoder execution semantics. The
[18 pure guard tests](m5g-configuration-recorder-tests.json) pass without HTTP
or capture writes. Total/partial/named POST behavior and application-key
authority remain unobserved. Native M5g implementation remains in development,
without product acceptance; the accepted M5f deployment is unchanged.

## Environment observations

`test-env` is a Debian 13 Linux host. The earlier root-capacity pressure has been resolved: after the user expanded its virtual disk to 97 GiB, online `growpart` and `resize2fs` grew the root partition and ext4 filesystem. At the [2026-09-10 observation](test-env-disk-growth.json), root reported roughly 96G total and 60G available. Root identity/start and boot partitions were preserved; the original Emby PID 3131777 and main Goby PID 3535438 were unchanged. Go and media build caches retain dedicated scratch locations. No GPU device was present in the recorded `/dev/dri` and `/dev/nvidia0` inspection, so actual GPU execution remains unverified.

The current deployed increment is M5f. Its service is active as the unprivileged `goby` user at `http://127.0.0.1:18096`: PID 3570491, UID 995, start ticks `24600634`, schema 19 through `0019_scheduled_tasks.sql`, and probe version 6. The [deployment audit](m5f-deployment-evidence.json) verifies executable SHA-256 `2993870cce6f4e0ae2c3630b645985cab664ce5e5e18123fdc920fb241bb2abf`, all 386 Go/module inputs plus 19 migrations, and 47 current administrator assets. Old business fields, media, vault and service configuration are preserved. Old scans retain null task-child associations; deployment registers only `library.scan`, with no initial triggers or executions.

M5f passed [1190 top-level race tests](m5f-full-race-summary.json) across thirteen tested packages, with zero skips/race findings and Go exit code 0. The [isolated browser workflow](m5f-tasks-browser.json) passed in 19.078232 seconds with eight check groups, two restarts, 27-table history preservation, and one actual startup execution with two children/scans. Its temporary resources were cleaned up, and four safe screenshots record the administrator UI. Earlier 21/35/44-test foundation and integration reports remain historical checkpoints rather than extra unique tests added to 1190.

The [main-service task workflow](m5f-deployed-tasks.json) passed its first attempt in 1.618 seconds with 14 GETs, seven POSTs, three PUTs, one DELETE, and no transport retries. One manual run snapshots five libraries, creates five owned child scans, and scans eleven cached media items. Receipt replay creates no second run. Native and Emby views share the definition ID, including `LastExecutionResult.Id`. Two future daily/weekly rules, a stale `409`, and restoration of the original empty rules/timezone are checked. Both new credentials are logged out and independently denied with `401`.

A separate [calendar execution check](m5f-calendar-firing.json) passed in 6.966 seconds. Daily and weekly rules each produced a real completed run with two owned library scans, an exact occurrence/revision association, and the correctly advanced next time. It used a fresh isolated fixture without a browser, restart, clock modification, or reference-server operation; cleanup passed. Together with startup/interval execution, every supported native trigger kind has real Linux firing evidence. This does not establish equivalent calendar behavior in Emby.

The live workflow preserves old media, NFO, user, key, device, policy, metadata, scan, and task-history fields. Its allowed old-row changes are normal `last_scan_at` updates for completed libraries, `updated_at` on five eligible catalog directories, and acknowledged revision/timestamp changes on the selected task definition. New run, child, scan, receipt, trigger, login and device history is retained. No automatic timer firing or service restart is claimed by this main workflow; isolated browser acceptance covers the two restart phases.

The M5f deployment creates and verifies its backup before stopping the service, restores it into an isolated temporary database, confirms raw equality across the 21 pre-upgrade tables, and removes that temporary database. It does not restore the live database. The preceding [M5e interruption and SQL-generation limits](verification-m5e-devices.md) remain documented historically; these operational checks do not constitute a shipped product backup/restore workflow.

The service uses an operator-provisioned private master under `GOBY_API_KEY_MASTER_KEY_FILE`, with UID 995, mode `0600`, 32 bytes, and a mode-0700 parent. [Application-key operations](application-keys.md) require preserving the database and matching master together; secret recovery is not a shipped backup/restore or rotation workflow. Earlier video restart, refresh, session, key and device increments remain in place. Additional task executors, broader settings, providers, audit/log browsing, product backups, actual hardware execution, and full client acceptance remain open. This is a test deployment, not a public production release; M5f is a completed increment while M4, M5, M6, and the full goal remain unfinished.

No milestone is complete solely because its build passes. Each implementation increment will append exact test/build results and its pushed revision here or in the corresponding verification report.
