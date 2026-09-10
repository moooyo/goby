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
| M0 core reference capture | Baseline complete; broader coverage pending | Official Emby 4.9.5.0 evidence now contains 1665 records. The [user-device study](../research/devices-reference.md) adds 348 records after the earlier 1128; the [key-device study](../research/key-devices-reference.md) adds another 189 with independent offline audit recovery and fresh-fixture teardown. [Key playback](../research/api-key-playback-reference.md), [client contexts](../research/api-key-context-reference.md), and [target scope](../research/api-key-scope-reference.md) distinguish credentials, client contexts, ACLs and conversion permissions. Global NextUp selection and hidden header-device Info/deletion remain unresolved. Goby's M5d acceptance is separate; M5e device implementation is not yet accepted |
| M1 service, identity, administrator foundation | Foundation increment complete | PostgreSQL migrations, users/sessions, setup/login, CSRF, proxy-aware rate limits, React/MUI overview/user creation, non-root Linux deployment; [verification report](verification-m1.md) |
| M2a media ingestion and browse | Complete | Pushed `90b7c2e`: safe ffprobe, bounded scans, PostgreSQL catalog/ownership, library ACL queries and React/MUI Libraries/Tasks; [verification report](verification-m2a.md) |
| M2b metadata and artwork | Local NFO, entities, and local artwork increments complete | Pushed NFO increment `6011377`; persistent entity navigation/filtering and bounded image delivery pass full Linux race tests and deployed checks; [NFO verification](verification-m2b-nfo.md), [artwork/entity verification](verification-m2b-artwork-entities.md). Generated/embedded artwork, broader metadata/query coverage and reconciliation remain |
| Startup and migration time budgets | Complete | Configurable `GOBY_STARTUP_TIMEOUT`, migration-local SQL timeout override, cancellation and connection-setting restoration verified; [verification](verification-startup-timeouts.md) |
| M3 initial client playback | Original playback/state, client sessions/NextUp, external SRT/WebVTT, and user-state events/remote-control increments complete; milestone incomplete | Full Linux race tests and deployed workflows passed; [M3a](verification-m3a-original-playback.md), [M3b](verification-m3b-sessions-nextup.md), [M3c](verification-m3c-subtitles.md), [M3d](verification-m3d-events.md). Additional events/subscriptions, broader subtitle handling, global NextUp parity and real-client acceptance remain |
| M4 conversion and hardware pipeline | Engine, HLS VOD, progressive audio/video, ordered profiles and verified video restart increments complete; milestone incomplete | [M4a](verification-m4a-engine.md), [M4b](verification-m4b-hls.md), [M4c](verification-m4c-audio.md), [M4d](verification-m4d-audio-profiles.md) and [M4e](verification-m4e-video-and-users.md) establish the preceding conversion and clock behavior. [M4f](verification-m4f-video-seek.md) adds probe 6 private restart evidence, software-decoder proof with actual threads, and independent linear audio. All 940 top-level race tests and five deployed library upgrades passed with real fast producer observation. [M4g](verification-m4g-media-refresh.md) adds explicit media re-probing, schema-15 task modes, real same-version index reconstruction and five deployed forced scans; all 969 top-level race tests pass. Nonzero copied-video seeking, efficient audio I/O, additional tracks/formats, aggregate resource isolation, actual GPU execution and full client acceptance remain |
| M5 administrator completion | User, metadata, login-session and application-key management increments complete; milestone incomplete | [M5a](verification-m4e-video-and-users.md) covers accounts and policies. [M5b](verification-m5b-metadata.md) adds metadata edits/locks, and [M5c](verification-m5c-sessions.md) adds login-session administration. [M5d](verification-m5d-application-keys.md) adds schema 16, native and compatibility key management, encrypted secret recovery, independent userless client contexts, target-user catalog ACLs, and parent-wide runtime retirement. All 1046 top-level race tests pass across twelve packages with zero skips or races; 75 focused tests, browser/restart acceptance and the deployed key workflow also pass. Full policies, online providers, generic tasks, complete devices/settings, audit/log browsing, backup and restore remain |
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
ignored sort requests remain separate observations. This is research only:
Goby device-management acceptance remains pending; the separate key-device
study below now supplies bounded shared-server deletion evidence. The deployed
M5d `563cd0e` product, schema 16, and its 1046-test acceptance are unchanged.

## Application-key/device reference increment

The [completed reference study](../research/key-devices-reference.md) adds 189
records: guarded original-instance capture with 61 HTTP exchanges and one audit,
fresh setup with 16 complete HTTP exchanges and one non-HTTP readiness failure,
109 complete fresh-study HTTP exchanges, and one independent recovery audit.
The corpus is now 1665; the derived operator teardown is not an extra record.

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

M5e device implementation remains uncommitted and undeployed at this research
checkpoint. Its initial Go run reported nine failures requiring correction;
no M5e acceptance is claimed. Recreating a new application-key device generation
is a Goby safety design, not an observed reference contract. The deployed M5d
schema-16/probe-6 service and its 1046-test evidence remain current. M4, M5, M6,
and the complete goal remain unfinished.

## Environment observations

`test-env` is reachable as a Debian 13 Linux host. Its root filesystem was initially full; disposable caches were reclaimed while preserving unrelated running workloads. Go and media build caches use dedicated scratch locations. No GPU device was present in the `/dev/dri` and `/dev/nvidia0` inspection. Hardware command generation can be tested there, but actual GPU execution requires suitable hardware and remains unverified.

The current service is active as the unprivileged `goby` user at `http://127.0.0.1:18096` on the test host: PID 3494032, UID 995, schema 16 through `0016_application_keys.sql`, and probe version 6. The [deployment audit](m5d-deployment-evidence.json) verifies executable SHA-256 `13857f9c33312bc13aef56d5d881c4a0fad23399ceb8a9dafa43e24c0fb2bad5`, all 40 current administrator assets, and preservation of every preexisting business field across migration. Existing playback rows receive null application-client columns; migration creates no application keys or clients.

M5d passed [1046 top-level race tests](m5d-full-race-summary.json) across twelve packages, with zero skips, no race findings, and zero Go/wrapper exit codes. Its [75 focused tests](m5d-targeted-summary.json) cover seven packages. The [isolated browser journey](m5d-application-keys-browser.json) passed in 6.603685 seconds, using 33 real issued keys and two target client contexts, with secret handling, revocation, restart persistence, and all ten cleanup checks passing.

The final [deployed key workflow](m5d-deployed-application-keys.json) passed its second attempt in 1.064 seconds, issuing 30 GETs, 22 POSTs, and one DELETE. It creates two keys, six client contexts, and two playback sessions; real WebSockets close on parent revocation, and token-only Progress, Ping, and Stopped recover the owned context. Source files, the master vault file, and all preexisting rows across nineteen tables remain unchanged. All three new credentials end revoked and both playbacks end stopped. The retained history now contains four key rows and eight client contexts, including the first attempt's records; these are not newly active credentials.

The service uses an operator-provisioned private master under `GOBY_API_KEY_MASTER_KEY_FILE`, with UID 995, mode `0600`, 32 bytes, and a mode-0700 parent. [Application-key operations](application-keys.md) require preserving the database and matching master together; secret recovery is not a shipped backup/restore or rotation workflow. M4f video restart, M4g explicit refresh, and M5c session management remain in place. Credentials remain in private files on the test host. This is a test deployment, not a public production release; M4, M5, M6, and the complete planned goal remain unfinished.

No milestone is complete solely because its build passes. Each implementation increment will append exact test/build results and its pushed revision here or in the corresponding verification report.
