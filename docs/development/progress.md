# Implementation progress

The goal remains the complete planned Linux backend and administrator dashboard. A completed engineering increment does not establish full Emby compatibility.

## Decisions

- Develop directly on `main`; commit and push each completed increment.
- Use PostgreSQL exclusively; the earlier SQLite proposal is superseded.
- Pin Go **1.27.1** and FFmpeg **9.0.1**, confirmed from official stable-release sources on 2026-09-09.
- Include explicit hardware **decode** and **encode** selection and evidence; software-only success is not GPU verification.
- Local compilation/build checks are authorized. Execute functional tests, runtime probes, media tests, and browser tests through `ssh test-env`.

## Increment status

| Increment | Status | Evidence / remaining work |
| --- | --- | --- |
| Research baseline and PostgreSQL/toolchain decisions | Complete as a documentation increment | Pushed `baa3731`: pinned upstream catalog, scope, PostgreSQL architecture and toolchain provenance |
| Linux toolchain and database provisioning | Complete | Pushed `79745ce`: Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11; software media verification passed |
| M0 core reference capture | Baseline complete; broader coverage pending | Official Emby 4.9.5.0 isolated on test-env, 958 audited records including HTTP/WebSocket/media observations; the latest metadata study adds 130 records with 106 complete HTTP exchanges. Global NextUp selection remains unresolved; [HTTP](../research/reference-server.md), [WebSocket](../research/websocket-reference.md), [HLS](../research/hls-reference.md), [audio](../research/audio-reference.md), [audio profiles](../research/audio-profile-reference.md), [video profiles](../research/video-progressive-reference.md), [metadata](../research/metadata-reference.md) |
| M1 service, identity, administrator foundation | Foundation increment complete | PostgreSQL migrations, users/sessions, setup/login, CSRF, proxy-aware rate limits, React/MUI overview/user creation, non-root Linux deployment; [verification report](verification-m1.md) |
| M2a media ingestion and browse | Complete | Pushed `90b7c2e`: safe ffprobe, bounded scans, PostgreSQL catalog/ownership, library ACL queries and React/MUI Libraries/Tasks; [verification report](verification-m2a.md) |
| M2b metadata and artwork | Local NFO, entities, and local artwork increments complete | Pushed NFO increment `6011377`; persistent entity navigation/filtering and bounded image delivery pass full Linux race tests and deployed checks; [NFO verification](verification-m2b-nfo.md), [artwork/entity verification](verification-m2b-artwork-entities.md). Generated/embedded artwork, broader metadata/query coverage and reconciliation remain |
| Startup and migration time budgets | Complete | Configurable `GOBY_STARTUP_TIMEOUT`, migration-local SQL timeout override, cancellation and connection-setting restoration verified; [verification](verification-startup-timeouts.md) |
| M3 initial client playback | Original playback/state, client sessions/NextUp, external SRT/WebVTT, and user-state events/remote-control increments complete; milestone incomplete | Full Linux race tests and deployed workflows passed; [M3a](verification-m3a-original-playback.md), [M3b](verification-m3b-sessions-nextup.md), [M3c](verification-m3c-subtitles.md), [M3d](verification-m3d-events.md). Additional events/subscriptions, broader subtitle handling, global NextUp parity and real-client acceptance remain |
| M4 conversion and hardware pipeline | Engine, HLS VOD, progressive audio/video and ordered profile increments complete; milestone incomplete | [M4a](verification-m4a-engine.md), [M4b](verification-m4b-hls.md), [M4c](verification-m4c-audio.md) and [M4d](verification-m4d-audio-profiles.md) established conversion, VOD, exact audio timing and profiles. [M4e](verification-m4e-video-and-users.md) adds progressive H.264/AAC MP4, ordered video profiles, probe 5 format clocks and encoded nonzero seeking. The complete Linux race suite, deployed video workflow and all five library upgrades passed. Nonzero copied-video seeking, efficient long-source seeking, additional tracks/formats, hard resource controls, actual GPU execution and full client acceptance remain |
| M5 administrator completion | User and metadata-management increments complete; milestone incomplete | [M5a](verification-m4e-video-and-users.md) covers accounts, policies and revocation. [M5b](verification-m5b-metadata.md) adds schema 14, library item management, persistent automatic/manual/locked metadata, source revisions, inactive-setting cleanup, and browser/restart/migration evidence. Full policies, online providers, tasks/devices/keys/settings, audit/log browsing, backup and restore remain |
| M6 compatibility release | Pending | Client/reference comparisons, Linux distribution/architecture/GPU matrix, operations, representative large-catalog upgrade timing, and recovery evidence |
| M7 additional features | Deferred per scope | Explicit feature decisions and their own acceptance gates |

## Environment observations

`test-env` is reachable as a Debian 13 Linux host. Its root filesystem was initially full; disposable caches were reclaimed while preserving unrelated running workloads. Go and media build caches use dedicated scratch locations. No GPU device was present in the `/dev/dri` and `/dev/nvidia0` inspection. Hardware command generation can be tested there, but actual GPU execution requires suitable hardware and remains unverified.

The current service is active as the unprivileged `goby` user at `http://127.0.0.1:18096` on the test host, with schema 14 and probe version 5. M5b passed 888 top-level tests across all twelve tested packages with zero skips, metadata and user browser workflows, and migration checks preserving every existing row in ten stable tables. The M5b checkpoint retains the previously verified media pipeline; ongoing fast-seek work is not part of that checkpoint. Credentials remain in root-only environment files on that host. It is a test deployment, not a public production release.

No milestone is complete solely because its build passes. Each implementation increment will append exact test/build results and its pushed revision here or in the corresponding verification report.
