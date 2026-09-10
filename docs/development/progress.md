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
| M0 core reference capture | Baseline complete; broader coverage pending | Official Emby 4.9.5.0 evidence contains 1665 records. The [user-device study](../research/devices-reference.md) adds 348 records after the earlier 1128; the [key-device study](../research/key-devices-reference.md) adds another 189 with independent offline audit recovery and fresh-fixture teardown. [Key playback](../research/api-key-playback-reference.md), [client contexts](../research/api-key-context-reference.md), and [target scope](../research/api-key-scope-reference.md) distinguish credentials, contexts, ACLs and conversion permissions. Global NextUp selection and hidden header-device Info/deletion remain unresolved. Reference conclusions are separate from Goby's completed M5e product acceptance |
| M1 service, identity, administrator foundation | Foundation increment complete | PostgreSQL migrations, users/sessions, setup/login, CSRF, proxy-aware rate limits, React/MUI overview/user creation, non-root Linux deployment; [verification report](verification-m1.md) |
| M2a media ingestion and browse | Complete | Pushed `90b7c2e`: safe ffprobe, bounded scans, PostgreSQL catalog/ownership, library ACL queries and React/MUI Libraries/Tasks; [verification report](verification-m2a.md) |
| M2b metadata and artwork | Local NFO, entities, and local artwork increments complete | Pushed NFO increment `6011377`; persistent entity navigation/filtering and bounded image delivery pass full Linux race tests and deployed checks; [NFO verification](verification-m2b-nfo.md), [artwork/entity verification](verification-m2b-artwork-entities.md). Generated/embedded artwork, broader metadata/query coverage and reconciliation remain |
| Startup and migration time budgets | Complete | Configurable `GOBY_STARTUP_TIMEOUT`, migration-local SQL timeout override, cancellation and connection-setting restoration verified; [verification](verification-startup-timeouts.md) |
| M3 initial client playback | Original playback/state, client sessions/NextUp, external SRT/WebVTT, and user-state events/remote-control increments complete; milestone incomplete | Full Linux race tests and deployed workflows passed; [M3a](verification-m3a-original-playback.md), [M3b](verification-m3b-sessions-nextup.md), [M3c](verification-m3c-subtitles.md), [M3d](verification-m3d-events.md). Additional events/subscriptions, broader subtitle handling, global NextUp parity and real-client acceptance remain |
| M4 conversion and hardware pipeline | Engine, HLS VOD, progressive audio/video, ordered profiles and verified video restart increments complete; milestone incomplete | [M4a](verification-m4a-engine.md), [M4b](verification-m4b-hls.md), [M4c](verification-m4c-audio.md), [M4d](verification-m4d-audio-profiles.md) and [M4e](verification-m4e-video-and-users.md) establish the preceding conversion and clock behavior. [M4f](verification-m4f-video-seek.md) adds probe 6 private restart evidence, software-decoder proof with actual threads, and independent linear audio. All 940 top-level race tests and five deployed library upgrades passed with real fast producer observation. [M4g](verification-m4g-media-refresh.md) adds explicit media re-probing, schema-15 task modes, real same-version index reconstruction and five deployed forced scans; all 969 top-level race tests pass. Nonzero copied-video seeking, efficient audio I/O, additional tracks/formats, aggregate resource isolation, actual GPU execution and full client acceptance remain |
| M5 administrator completion | User, metadata, login-session, application-key and device increments complete; milestone incomplete | [M5a](verification-m4e-video-and-users.md), [M5b](verification-m5b-metadata.md), [M5c](verification-m5c-sessions.md), and [M5d](verification-m5d-application-keys.md) retain the preceding account, metadata, session and key evidence. [M5e](verification-m5e-devices.md) adds native Devices and six compatibility operations, ordinary/shared generations, grouped revocation, alias isolation, schema 18 and exact backfill preservation. All 1085 top-level race tests pass across twelve packages with zero skips or races; focused regressions, browser/restart and the main-service workflow pass. Full policies, online providers, generic tasks, settings, audit/log browsing, backup/restore and broader device/Session wire parity remain |
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

At the research checkpoint, the initial M5e Go run reported nine failures and
product acceptance was pending. Those failures and the later legacy-regression
issues were corrected; [final M5e acceptance](verification-m5e-devices.md) now
passes. Recreating a new application-key device generation remains a Goby
safety design, not an observed reference contract. M5e completes this device
increment only; M4, M5, M6, and the complete goal remain unfinished.

## Environment observations

`test-env` is reachable as a Debian 13 Linux host. Its root filesystem was initially full; disposable caches were reclaimed while preserving unrelated running workloads. Go and media build caches use dedicated scratch locations. No GPU device was present in the `/dev/dri` and `/dev/nvidia0` inspection. Hardware command generation can be tested there, but actual GPU execution requires suitable hardware and remains unverified.

The current service is active as the unprivileged `goby` user at `http://127.0.0.1:18096` on the test host: PID 3535438, UID 995, start ticks `23604510`, schema 18 through `0018_application_key_devices.sql`, and probe version 6. The [deployment audit](m5e-deployment-evidence.json) verifies executable SHA-256 `394430272da8ad6268534c1bcaa925ccffc92f61ed0b136a8ec6bbc4dae88541`, all 354 Go/module files plus 18 migrations, and all 44 current administrator assets. All old business fields across nineteen tables and the exact ordinary/shared-device backfills are preserved, along with the vault, environment and service configuration.

M5e passed [1085 top-level race tests](m5e-full-race-summary.json) across twelve tested packages, with zero skips, no race findings, and Go exit code 0. Its [device regression](m5e-device-regression.json) passes 22 tests and the [legacy regression](m5e-legacy-regression.json) passes four. The [isolated browser journey](m5e-devices-browser.json) passed in 6.930194 seconds with real registrations, conflicts, response-loss recovery, generation deletion, and 21-table restart preservation; all ten cleanup checks pass and four secret-free screenshots document the layout.

The [main-service device workflow](m5e-deployed-devices.json) passed in 1.824 seconds with 16 GETs, 17 POSTs, one DELETE, and no retries. It groups two cross-user logins, keeps the native cookie and unrelated login usable, checks `409` conflicts and old-ID idempotency, and registers a replacement generation. All five new credentials end revoked and all three new device generations end soft-deleted. Every preexisting row across 21 public tables, all eleven media sources, the vault, and historical key namespace remain unchanged.

Deployment included a brief service interruption after permissions blocked source installation and the original restore command failed. The finishing wrapper completed 342 source replacements without another stop or database write and retained the original 551-entry backup. The [read-only SQL-generation check](m5e-restore-sql-generation.json) reproduces the old command's exit 1 and verifies `--file=-` generates 203,084 SQL bytes with exit 0. It does not connect to or restore a database. This and the five original pre-install refusal simulations do not establish full database-restore or product backup/restore acceptance.

The service uses an operator-provisioned private master under `GOBY_API_KEY_MASTER_KEY_FILE`, with UID 995, mode `0600`, 32 bytes, and a mode-0700 parent. [Application-key operations](application-keys.md) require preserving the database and matching master together; secret recovery is not a shipped backup/restore or rotation workflow. Earlier video restart, explicit refresh, session and key increments remain in place. Credentials remain in private files on the test host. Generic tasks, settings, providers, audit/log browsing, backups, actual hardware execution, and full client acceptance remain open. This is a test deployment, not a public production release; M4, M5, M6, and the complete planned goal remain unfinished.

No milestone is complete solely because its build passes. Each implementation increment will append exact test/build results and its pushed revision here or in the corresponding verification report.
