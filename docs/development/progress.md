# Implementation progress

The goal remains the complete planned Linux backend and administrator dashboard. A completed engineering increment does not establish full Emby compatibility.

The M5j round is complete: native backup/recovery, deployment and the
[handoff](handoff.md) are recorded at `4a840fb`. Development resumed on 2026-09-11
at the user's request. [M3e real-client acceptance](client-acceptance-m3e.md) is
in progress. The partial [source18/schema25 checkpoint](verification-m3e-source18-checkpoint.md)
is deployed on the primary service; M5j remains a preserved historical baseline.
This source18 checkpoint records the deployed product and its verification evidence together.

## Decisions

- Develop directly on `main`; commit and push each completed increment.
- Use PostgreSQL exclusively; the earlier SQLite proposal is superseded.
- Pin Go **1.27.1** and FFmpeg **9.0.1**, reconfirmed from official stable-release sources on 2026-09-10.
- Include explicit hardware **decode** and **encode** selection and evidence; software-only success is not GPU verification.
- The resumed task requires all builds, tests, validation and runtime/browser probes through `ssh test-env`; local verification is not authorized.

## Increment status

| Increment | Status | Evidence / remaining work |
| --- | --- | --- |
| Research baseline and PostgreSQL/toolchain decisions | Complete as a documentation increment | Pushed `baa3731`: pinned upstream catalog, scope, PostgreSQL architecture and toolchain provenance |
| M3e real-client acceptance | In progress; partial checkpoint deployed | [Active record](client-acceptance-m3e.md): source18/schema25 passed 19 targeted regressions, build, protected replacement, and the full race suite: 1,741 top-level tests across 24 packages, zero failures and zero skips. Both original-client audio core journeys passed with successful state reports, persisted history and logout. FLAC completed the full workflow including Home; MP3's original Home harness uniqueness failure remains preserved. Similar/ThemeMedia 404 responses and page errors remain unresolved. Source16's 1,739-test suite, source15 SRT/VTT, ordinary-TV and controlled Music scan evidence, and source11 movie/preferences evidence are historical checkpoints. Tool03 passed 47 remote guards and build; fresh backup preparation, independent schema23-to-25 restore rehearsal, cleanup, preservation of all old business columns and sequences through the 30-table result, and the primary schema25 deployment passed. Empty transcode-cache preparation passed seven guards and actual creation. [Source18 checkpoint](verification-m3e-source18-checkpoint.md) records the deployed product and its verification evidence together; broader compatibility and complete milestones remain open |
| Linux toolchain and database provisioning | Complete | Pushed `79745ce`: Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11; software media verification passed |
| M0 core reference capture | Baseline complete; broader coverage pending | Official Emby 4.9.5.0 evidence contains 2462 records. The [activity/log study](../research/observability-reference.md) adds 96 to the preceding 2366: 94 complete HTTP exchanges, one readiness connection refusal, and one audit. The [4K encoding-width study](../research/encoding-width-reference.md) added 61 to the preceding 2305; the [fresh configuration mutation study](../research/configuration-mutation-reference.md) added 254 after the [read study](../research/configuration-reference.md) reached 2051. Older evidence remains preserved. Broader configuration writes, changed-value key writes and restart persistence, task timer/key-auth behavior, weekly/system-event execution, DST/maximum-runtime enforcement, global NextUp selection, and hidden header-device Info/deletion remain unresolved. Reference records are separate from product acceptance |
| M1 service, identity, administrator foundation | Foundation increment complete | PostgreSQL migrations, users/sessions, setup/login, CSRF, proxy-aware rate limits, React/MUI overview/user creation, non-root Linux deployment; [verification report](verification-m1.md) |
| M2a media ingestion and browse | Complete | Pushed `90b7c2e`: safe ffprobe, bounded scans, PostgreSQL catalog/ownership, library ACL queries and React/MUI Libraries/Tasks; [verification report](verification-m2a.md) |
| M2b metadata and artwork | Local NFO, entities, and local artwork increments complete | Pushed NFO increment `6011377`; persistent entity navigation/filtering and bounded image delivery pass full Linux race tests and deployed checks; [NFO verification](verification-m2b-nfo.md), [artwork/entity verification](verification-m2b-artwork-entities.md). Generated/embedded artwork, broader metadata/query coverage and reconciliation remain |
| Startup and migration time budgets | Complete | Configurable `GOBY_STARTUP_TIMEOUT`, migration-local SQL timeout override, cancellation and connection-setting restoration verified; [verification](verification-startup-timeouts.md) |
| M3 initial client playback | Original playback/state, client sessions/NextUp, external SRT/WebVTT, and user-state events/remote-control increments complete; milestone incomplete | Full Linux race tests and deployed workflows passed; [M3a](verification-m3a-original-playback.md), [M3b](verification-m3b-sessions-nextup.md), [M3c](verification-m3c-subtitles.md), [M3d](verification-m3d-events.md). Additional events/subscriptions, broader subtitle handling, global NextUp parity and real-client acceptance remain |
| M4 conversion and hardware pipeline | Engine, HLS VOD, progressive audio/video, ordered profiles and verified video restart increments complete; milestone incomplete | [M4a](verification-m4a-engine.md), [M4b](verification-m4b-hls.md), [M4c](verification-m4c-audio.md), [M4d](verification-m4d-audio-profiles.md) and [M4e](verification-m4e-video-and-users.md) establish the preceding conversion and clock behavior. [M4f](verification-m4f-video-seek.md) adds probe 6 private restart evidence, software-decoder proof with actual threads, and independent linear audio. All 940 top-level race tests and five deployed library upgrades passed with real fast producer observation. [M4g](verification-m4g-media-refresh.md) adds explicit media re-probing, schema-15 task modes, real same-version index reconstruction and five deployed forced scans; all 969 top-level race tests pass. Nonzero copied-video seeking, efficient audio I/O, additional tracks/formats, aggregate resource isolation, actual GPU execution and full client acceptance remain |
| M5 administrator completion | User, metadata, login-session, application-key, device, scheduled-task, native settings, bounded configuration compatibility, activity/log, and native backup/recovery increments complete; milestone incomplete | [M5a](verification-m4e-video-and-users.md), [M5b](verification-m5b-metadata.md), [M5c](verification-m5c-sessions.md), [M5d](verification-m5d-application-keys.md), [M5e](verification-m5e-devices.md), [M5f](verification-m5f-tasks.md), and [M5g](verification-m5g-settings.md) retain prior acceptance. [M5h](verification-m5h-configuration.md) passes its complete configuration acceptance. [M5i activity/log acceptance](observability.md) adds 1380 race tests, real browser/restarts, protected schema-22 deployment, and the main-service workflow. Additional task executors, full policies, providers, broader configuration fields/sections, and broader wire/client parity remain |
| M5f scheduled tasks | Initial library executor and scheduling increment complete | [Verification](verification-m5f-tasks.md): 1190 full-race tests, isolated browser acceptance with two restarts, schema-19 deployment, and a successful main-service run/replay/schedule workflow. Eight [native task routes](../api/tasks.md) and six compatibility operations use durable receipts, owned children, typed schedules and recovery. [Reference read](../research/scheduled-tasks-reference.md) and [mutation](../research/scheduled-tasks-mutation-reference.md) studies retain their narrower evidence; timer/reference parity, more executors, and complete client acceptance remain broader work |
| M5g native settings | Native increment complete | [Verification](verification-m5g-settings.md): 1222 full-race tests across fourteen packages, browser with two exact 28-table restarts, protected schema-20 deployment, and first-pass settings/restore workflow on the main service. Its three native routes managed five nullable overrides with CAS and atomic publication. The later M5h increment adds the bounded adapter; the [read](../research/configuration-reference.md) and [fresh mutation](../research/configuration-mutation-reference.md) studies retain their historical scope |
| M5h configuration compatibility | Bounded configuration increment complete | [Verification](verification-m5h-configuration.md): 1252 race tests across fourteen packages, 16 browser checks/two 28-table restarts, protected schema-21 deployment with actual 28-table isolated restore, and first-pass main-service writes/restoration/revocation barriers. Five [compatibility routes](../api/configuration.md) expose only name, read-only setup status and encoding width; native settings adds four name modes and six reset selectors. The [4K width study](../research/encoding-width-reference.md) proves the sampled software execution boundary; broader fields/sections, GPU execution and full client acceptance remain |
| M5i activity and diagnostic logs | Increment complete and deployed | Four [native and four Emby GET routes](../api/observability.md), schema-22 transactional activity, private bounded Linux JSONL storage, and the React/MUI Activity and Server logs page passed acceptance. The [complete race suite](m5i-full-race-summary.json) passed 1380 tests across 17 packages without skips/races; [browser/restarts](m5i-observability-browser.json), [protected deployment](m5i-deployment-evidence.json), and [main workflow](m5i-deployed-observability.json) passed. Historical deployed checkpoint: M5i/schema 22/probe 6/PID 3668655 |
| M6 compatibility release | Incomplete; acceptance pending | Client/reference comparisons, Linux distribution/architecture/GPU matrix, operations, representative large-catalog upgrade timing, and recovery evidence |
| M5j native backup and recovery | Complete and deployed | [Implementation and evidence](backup-recovery.md): final source30 passed 1605 race tests across 24 packages, with zero skips/failures/races. UI a6 passed type checking, 27 mocked-API browser cases and its 57-asset build. Actual browser/process acceptance passed create/import/restore/rollback, six generation checks, three restarts and offline CLI with an unavailable primary. Protected schema-23 deployment and the main backup/download/logout workflow passed. The initial deployment parser failure and its 54-test read-only finalization are recorded separately |
| M7 additional features | Deferred per scope | Explicit feature decisions and their own acceptance gates |

## M5j closeout

M5j remains an accepted historical product baseline. The deployment and closeout
in this section precede the current partial source18/schema25 checkpoint.
The final Go source and executable are covered by one complete source30 run;
earlier source25/source26 counts are historical and are not added to the final
1605-test result.

| Final closeout gate | Result |
| --- | --- |
| Complete repository and Linux executable | Passed: [1605 tests / 24 packages](m5j-final-full-race.json), [build identity](m5j-final-build.json) |
| Browser/process/offline CLI, transition/restart and cleanup | Passed: [complete runtime acceptance](m5j-runtime-acceptance.json) |
| Candidate/source/evidence reconciliation | Passed: [final source gate](m5j-final-source-gate.json) |
| Protected deployment | Passed: [deployment](m5j-deployment-evidence.json); the [initial failure](m5j-deployment-failed-attempt-1.json) and [54 remediation guards](m5j-deployment-remediation-tests.json) remain separate |
| Main-service backup/download/logout | Passed in 2.373 seconds: [workflow](m5j-deployed-backup-workflow.json) |
| Publication and handoff | Recorded on `origin/main`; [handoff](handoff.md) includes operating paths and the next-round scope |

At M5j closeout, the main service ran at schema 23/probe 6, PID 3750313, UID 995, start ticks
`28911319`, with 576 installed source inputs and 57 current assets. The real
workflow retained a 177,366-byte encrypted backup, its separate passphrase,
seven audit records and one revoked native session; all old table rows and
original media/master/configuration were preserved. The exact main archive
was not decrypted or restored by this workflow. Product restore/rollback and
offline recovery were verified in the isolated runtime environment.

The first deployment's repeated-systemd-property parsing failure was corrected
and verified with 54 guards. Read-only finalization rechecked the installed
candidate without a service restart or database write. The original backup,
candidate and operator evidence remain intact. No further milestone starts in
this round; M3/M4/M5/M6 gaps below remain open and the full project is unfinished.

## Earlier M5i acceptance

The [activity/log implementation](observability.md) adds 21 transactional
activity actions, bounded retention, safe per-event JSONL diagnostics, fixed
file snapshots with download authorization rechecks, and eight read routes.
The React/MUI administrator page at `/admin/observability` has Activity and
Server logs tabs. Migration 22 adds the initially empty
`activity_entries` table; it does not infer old history or require media
re-probing. This increment has passed product acceptance and replaced the
completed M5h deployment, whose historical evidence is retained below.

The [complete remote race suite](m5i-full-race-summary.json) **PASSED** with
1380 top-level tests across 17 tested packages, zero skips, no race findings,
and Go exit 0 against `/opt/goby-test/verify-m5i-full-attempt-2`. The complete
log SHA-256 is `decfdd6344bd8d16d0a06c88f1cc411497d9d6b22d9096b482211dbce9431b53`.
Its captured inputs include 470
Go/module/SQL files, ten test-data files, 2462 reference records, and 55 web
source inputs. These inventory counts identify this candidate snapshot; they
are not counts of successful tests. The accepted Go
1.27.1 Linux build uses `CGO_ENABLED=0`, is 23,156,729 bytes, and has SHA-256
`1ead2fcaa22df227d3d8b6b607978887ccfee7868139fc38f40523dbe24752ef`.
The React/MUI build produced 54 current assets, with archive SHA-256
`20ed0e16b721515f1e56dddeef818e3102a7c92f25f2a1d8d08aa1d2b756f85b`.
The [source/build gate](m5i-final-go-source-gate.json) reconciled 535
Go/module/SQL, test-data, and browser inputs with the tested executable and
assets. This is product source/evidence reconciliation; it does not represent
a Git commit or push result.

| Intermediate evidence | Result | Boundary |
| --- | --- | --- |
| [Core race attempt 2](m5i-core-race.json) | 79 top-level tests passed | Captured core snapshot; not the complete final suite |
| [Selected domain/native HTTP attempt 2](m5i-domain-http-race.json) | 68 top-level tests passed | Historical selected coverage; the subsequent full suite provides complete Go coverage |
| [Browser-runner guards](m5i-browser-guards.json) | 29 tests passed | Synthetic memory-only guards, not actual browser acceptance |
| [Deployment-operator guards](m5i-deployment-operator-tests.json) | 33 tests passed | Synthetic memory-only contracts, not a real restore or deployment |

These intermediate counts are not added to the complete 1380-test total.
The [real browser acceptance](m5i-observability-browser.json) passed 11 scenario
checks in **12.400443 seconds**, without skips or retries. It covers the native
activity/log journey, paging, details, downloads, mobile layout, stale-response
cancellation, and self-revocation; selected UI failure states use controlled
response injection. The independent runner confirmed real rotation, immediate
redaction of four request-channel sentinels before later rotation, and 16
completed downloads reusing bounded reader slots. Two restarts each preserved
all 29 public tables exactly, including 66 activity entries, and recovered
registered logs for download. Both native sessions were revoked with 401/SQL
proofs, and temporary database/role/HBA/process cleanup passed. Four reviewed
screenshots are retained under [screenshots/m5i](screenshots/m5i).

The [deployment](m5i-deployment-evidence.json) passed a real isolated restore
rehearsal of all 28 old tables before stopping the service. The live database
was not restored. Schema 21 became 22 with an initially empty activity table,
all old business fields and settings revision 6 intact, and media, master,
runtime/unit settings, and the original Emby reference process preserved. A
new owned `30-observability.conf` drop-in provisions `/var/log/goby-test`, mode
`0700`, UID 995. That earlier deployment was **M5i/schema 22/probe 6**, PID **3668655**,
start ticks `26912384`; its manifest SHA-256 is
`bf3902d5744fed5136296197f08c8e009ab4a2b496c00e19dea2eaa865dfa5af`.

The [main-service workflow](m5i-deployed-observability.json) passed in **0.776
seconds** with 17 GETs, two HEADs, three POSTs, two PUTs, one DELETE, no transport
retries, and 20 read-only verifier SQL queries. One native and one ordinary
Emby credential read both API projections. Native CAS changed the name and
restored every original name/override/encoding value, advancing revision from
6 to 8. Native HEAD/range and compatibility HEAD 404 were checked. Both logouts
were independently confirmed by 401 responses; two revoked sessions, one
device, and six activity entries remain as new history. Old rows remain exact
apart from the acknowledged settings revision/timestamp advances. No main
restart, media/planning, scan, task, or history-deletion operation was issued.
Main-service rotation, retention deletion, and mid-download revocation are
not claimed by this workflow; separate core/browser evidence covers its own
recorded scenarios.

The first [browser](m5i-observability-browser-attempt-1.json) and
[deployment](m5i-deployment-attempt-1.json) failures remain retained. The browser
instrumentation was corrected without changing production UI or Go code. The
deployment attempt rejected a `.tar`/`.tar.gz` asset-name contract before
service stop; the operator literal/fixture was corrected and all 33 pure guards
passed again before the accepted deployment. The reference study below remains
separate research, and M4, M5, M6, and the complete planned server remain open.

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
with 104,719 response-body bytes. The corpus reached 2051 at that checkpoint. Administrator
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
authority were unobserved in this read-only increment. The fresh study below
adds separate bounded evidence; the successful native acceptance is independent.

## Configuration mutation reference increment

The [fresh configuration study](../research/configuration-mutation-reference.md)
adds 254 records: 17 setup records and 237 capture records, bringing the
corpus to 2305 at that checkpoint. Its 236 complete HTTP exchanges and audit record 145,347
response-body bytes in 3.238 seconds. Complete clone and selected partial/named
writes return `204`. A mixed invalid partial update returns `500` after a name
change remains visible in the same process; no restart persistence is claimed.
The key controls comprise two reads and three complete baseline no-op writes,
not changed-value mutation coverage.

Configuration restoration and credential cleanup pass. The
[operator cleanup](m5g-fresh-configuration-cleanup.json) stops fresh PID 3613232
and removes its attested program data while retaining evidence and preserving
old services, files, and 240 source paths. The [37 recorder guards](m5g-fresh-configuration-recorder-tests.json)
and [nine operator guards](m5g-fresh-operator-tests.json) pass with memory-only
fixtures. Earlier synthetic fixture-expectation failures remain documented;
their correction does not relax redaction and is not a product regression.
This study preceded the accepted M5h adapter, whose supported field set remains narrower than the captured reference objects.

## Encoding-width execution reference increment

The [M5h width study](../research/encoding-width-reference.md) adds 61 records:
12 setup HTTP records, two operator-cleanup HTTP records, 44 capture HTTP
records, two output probes, and one audit. The corpus reached 2366 from 2305
at that checkpoint.
Using the same 4K source and software `libx264`, configured width 1280 produces
1280x720 and zero produces 3840x2160. Both outputs contain eight frames and
pass complete decoding. The [execution evidence](m5h-encoding-width-reference.json)
supports zero removing the extra width cap in that sampled flow, not all other
limits or all-client equivalence.

The second fresh fixture is stopped; its exact data and source paths are gone.
All owned producers are stopped, the capture credential is revoked with a
`401` proof, and the original encoding configuration is restored. Older records,
240 original media paths, and 39 files from the failed first fixture are preserved.
Those preserved private failure files and derived cleanup reports are not
additional sanitized corpus records.

## Activity and diagnostic log reference increment

The [M5i activity/log study](../research/observability-reference.md) adds 96
safe records to the preceding 2366, bringing the reference corpus to **2462**.
The groups are 15 setup records, 76 capture HTTP exchanges, one correspondence
audit, and four independent operator-cleanup exchanges. This is **94 complete
HTTP exchanges**, one initial connection-refused readiness record, and one
audit; all 76 capture HTTP bodies are complete. The initial readiness failure
is preserved as evidence rather than counted as a successful exchange.

The [reference report](m5i-observability-reference.json) records the GET
administrator/key permission matrix, viewer `ManageServer` denial, activity
count anomalies, observed Lines paging/default-empty behavior, download
framing, and administrator/anonymous log HEAD `404`. The newly created user's
activity is tied to its actual acknowledged cause. All raw-byte/safe-export
correspondence and cleanup checks pass, ordinary and application credentials
are invalidated, and the owned process/data are removed. Earlier services,
records, and 240 media sources remain preserved.

This study does not establish the complete upstream sanitization algorithm,
submicrosecond date rounding, retention/rotation execution, Range behavior,
restart persistence, or client UI acceptance. Goby's
[declared adapter boundaries](../api/observability.md#compatibility-surface)
use fixed safe descriptions and error text, actual user identities, bounded
queries, registered files, and always-sanitized snapshots. The separately
reported product full suite, browser, and deployment acceptance have passed
within their recorded scopes; they are not results of this reference study.

## Environment observations

`test-env` is a Debian 13 Linux host. The earlier root-capacity pressure has been resolved: after the user expanded its virtual disk to 97 GiB, online `growpart` and `resize2fs` grew the root partition and ext4 filesystem. At the [2026-09-10 observation](test-env-disk-growth.json), root reported roughly 96G total and 60G available. Root identity/start and boot partitions were preserved; the original Emby PID 3131777 and main Goby PID 3535438 were unchanged. Go caches now use persistent root-disk storage under `/opt/goby-test/go-caches-m5h` through their original `/dev/shm` path symlinks; media scratch remains separate. No GPU device was present in the recorded `/dev/dri` and `/dev/nvidia0` inspection, so actual GPU execution remains unverified.

The current primary service has **source18/schema25 deployed**, PID 539535,
start ticks `3115871`, executable SHA-256
`665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b`.
The isolated client candidate remains source18/schema25 with the same binary,
PID 506532, start ticks `2681316`. The
[deployment evidence](m3e-source18-main-deployment.json), SHA-256
`02ed027b353488ab31cb9e4ac3e7cfc4547422bb1a57e7f9cfdfdd945aad0bf3`,
records preservation of all old business columns and sequences through the
29-to-30-table schema25 migration, health/readiness, administrator login
and seven reads, logout 204, and rejection of the exact token with 401. Old
archives were preserved; no primary restore or old-binary rollback occurred.
This is the partial [M3e checkpoint](verification-m3e-source18-checkpoint.md),
with its product and verification evidence recorded together. Similar/ThemeMedia 404 responses, page
errors, broader compatibility, and complete milestones remain open.

The two earlier prepare attempts failed safely before the Go helper, dump,
restore rehearsal, or migration began and remain preserved as historical failures.

Tool03's independent SQL and OS timestamp checks passed 47 remote guards and
the build. The new [fresh preparation](m3e-schema25-preparation-source18.json)
passed: a schema23 dump from the same snapshot, an independent schema23-to-25
restore rehearsal, cleanup, and exact preservation of the primary's old state.
Its prepared record is
`/opt/goby-test/backups/client-schema25-v1/run-20260911T062405Z-975ef4c2e0c2b3078d517dc5/prepared.json`,
SHA-256 `b1d686b1c8aed83aa545dd02618c22793cc8b371f6659a0adc1f40710ddfd0ce`.
The empty transcode cache passed seven independent guards and actual creation
before the successful deployment recorded above.

At the historical M5j deployment checkpoint, the service ran as unprivileged
`goby` at `http://127.0.0.1:18096`, PID 3750313, UID 995, start ticks `28911319`,
schema 23/probe 6. [Deployment evidence](m5j-deployment-evidence.json) binds 576
installed source inputs and 57 assets from that checkpoint. Its complete
pre-upgrade backup at `/opt/goby-test/backups/m5j-20260910` passed an actual
isolated 29-table restore before that deployment's old service stopped. The
subsequent native backup workflow and private operator copy are recorded in the
[handoff](handoff.md). These historical results remain separate from the fresh
preparation recorded above.
Neither this operational backup nor the older M5i backup may overwrite newer
accepted business state. The original Emby reference PID remains 3131777.

## Earlier M5h acceptance

The preceding [M5h configuration compatibility](verification-m5h-configuration.md) service ran as unprivileged `goby` at `http://127.0.0.1:18096`: PID 3641418, UID 995, start ticks `26048863`, schema 21 through `0021_configuration_compatibility.sql`, and probe 6. Its [deployment audit](m5h-deployment-evidence.json) binds executable SHA-256 `62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54`, 429 installed Go/module/SQL inputs and 50 assets at that checkpoint. The migration retained all 28 tables and their old fields, including the managed row's revision 3 and timestamps; only the two new fields were backfilled. Old media, task history, server settings, vault, runtime and unit configuration were preserved.

The complete private backup at `/opt/goby-test/backups/m5h-20260910` was verified before stop. A real import into an isolated database proved all 28 old tables exact, and that database was removed. The live database was not restored. Successful deployment evidence is published: this backup must never be restored over the newer accepted workflow state. These operational checks do not implement shipped product backup/restore.

M5h passed [1252 top-level race tests](m5h-full-race.json) across fourteen tested packages with zero test skips or race findings. The [browser workflow](m5h-configuration-browser.json) passed 16 checks in 7.808 seconds, followed by two exact 28-table restart checks. The [main-service workflow](m5h-deployed-configuration.json) passed its first attempt in 0.901 seconds: compatibility writes, Emby credential revocation and independent `401` barrier, native CAS restoration, then native revocation and its final `401` barrier. All five overrides return to NULL, name mode to `deployment`, and extra width to zero; only settings revision/update time remain advanced. All pre-existing rows in the other 27 tables remain exact. Two new revoked sessions and one ordinary device remain as history, for totals of 96 sessions and 13 devices. This workflow performs no main restart, media/planning request, conversion, scan, or task execution.

## Earlier M5g acceptance

At the earlier M5g native settings checkpoint, the service ran as the unprivileged `goby` user at `http://127.0.0.1:18096`: PID 3614026, UID 995, start ticks `25289276`, schema 20 through `0020_managed_settings.sql`, and probe version 6. The [deployment audit](m5g-deployment-evidence.json) verifies executable SHA-256 `e1f6b723eb963ad855f465d0798958480615b8b000455fcf26bc7b088b8d2f2f`, 419 installed Go/module/SQL inputs, and 50 current administrator assets. Old business rows, `server_settings`, task history, media, vault, and runtime/unit configuration are preserved. The new singleton began at revision 1 with five NULL overrides.

The protected backup at `/opt/goby-test/backups/m5g-20260910` was complete before service stop. An actual isolated restore proved all 27 old tables raw-exact and was removed; the live database was not restored. The migration adds only the new settings table and its migration-history entry. These deployment operations do not implement a shipped product backup/restore workflow.

M5g passed [1222 top-level race tests](m5g-native-full-race.json) across fourteen tested packages, with zero test skips/race findings and exit 0. The [final source gate](m5g-final-go-source-gate.json) binds 419 source, ten test-data, and 38 browser inputs to the same accepted binary and assets. The first full attempt retains 1219 passes and three missing-fixture failures; the second restores the omitted test data without changing those source inputs. The [browser workflow](m5g-settings-browser.json) passed in 6.280795 seconds with two exact 28-table restarts, changed deployment defaults, preserved overrides/revision, and complete cleanup.

The [main-service settings workflow](m5g-deployed-settings.json) passed its first attempt in 0.701 seconds with 15 GETs, one POST, two PUTs, one DELETE, no transport retries, and 19 read-only verifier SQL queries. Its new native cookie sets all five overrides temporarily, verifies published values and public-info/overview names, and restores the original nullable set with a fixed-revision CAS. Logout, independent `401`, and the final SQL barrier preserve all pre-existing rows in the 27 old tables; the new session remains revoked history. A separate [read-only observation](m5g-post-workflow-settings-state.json) confirms revision 3 and five NULL overrides. Only the managed row's revision/update time remains advanced. The workflow makes no main restart, planning, media/playback, scan, application-key, or Emby configuration/credential operation; anonymous public information uses `/emby/System/Info/Public`.

## Earlier M5f acceptance

The preceding M5f deployment ran as PID 3570491, schema 19/probe 6. Its [deployment audit](m5f-deployment-evidence.json) records 386 Go/module inputs plus 19 migrations and 47 current assets at that checkpoint, preserving old business fields and introducing empty initial task execution/schedule state.

M5f passed [1190 top-level race tests](m5f-full-race-summary.json) across thirteen tested packages, with zero skips/race findings and Go exit code 0. The [isolated browser workflow](m5f-tasks-browser.json) passed in 19.078232 seconds with eight check groups, two restarts, 27-table history preservation, and one actual startup execution with two children/scans. Its temporary resources were cleaned up, and four safe screenshots record the administrator UI. Earlier 21/35/44-test foundation and integration reports remain historical checkpoints rather than extra unique tests added to 1190.

The [main-service task workflow](m5f-deployed-tasks.json) passed its first attempt in 1.618 seconds with 14 GETs, seven POSTs, three PUTs, one DELETE, and no transport retries. One manual run snapshots five libraries, creates five owned child scans, and scans eleven cached media items. Receipt replay creates no second run. Native and Emby views share the definition ID, including `LastExecutionResult.Id`. Two future daily/weekly rules, a stale `409`, and restoration of the original empty rules/timezone are checked. Both new credentials are logged out and independently denied with `401`.

A separate [calendar execution check](m5f-calendar-firing.json) passed in 6.966 seconds. Daily and weekly rules each produced a real completed run with two owned library scans, an exact occurrence/revision association, and the correctly advanced next time. It used a fresh isolated fixture without a browser, restart, clock modification, or reference-server operation; cleanup passed. Together with startup/interval execution, every supported native trigger kind has real Linux firing evidence. This does not establish equivalent calendar behavior in Emby.

The live workflow preserves old media, NFO, user, key, device, policy, metadata, scan, and task-history fields. Its allowed old-row changes are normal `last_scan_at` updates for completed libraries, `updated_at` on five eligible catalog directories, and acknowledged revision/timestamp changes on the selected task definition. New run, child, scan, receipt, trigger, login and device history is retained. No automatic timer firing or service restart is claimed by this main workflow; isolated browser acceptance covers the two restart phases.

The M5f deployment creates and verifies its backup before stopping the service, restores it into an isolated temporary database, confirms raw equality across the 21 pre-upgrade tables, and removes that temporary database. It does not restore the live database. The preceding [M5e interruption and SQL-generation limits](verification-m5e-devices.md) remain documented historically; these operational checks do not constitute a shipped product backup/restore workflow.

The service uses an operator-provisioned private master under `GOBY_API_KEY_MASTER_KEY_FILE`, with UID 995, mode `0600`, 32 bytes, and a mode-0700 parent. [Application-key operations](application-keys.md) require preserving the database and matching master together; secret recovery is not a shipped backup/restore or rotation workflow. Earlier video restart, refresh, session, key, device and task increments remain in place. Broader ConfigurationService fields/sections, additional task executors and settings, providers, product backups, actual hardware execution, and full client acceptance remain open. M5i activity/log APIs and administration have passed their declared product acceptance and are deployed. This is a test deployment, not a public production release; M5g native settings, M5h bounded configuration compatibility, and M5i activity/log administration are completed increments while M4, M5, M6, and the full goal remain unfinished.

No milestone is complete solely because its build passes. Each implementation increment will append exact test/build results and its pushed revision here or in the corresponding verification report.
