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
| Research baseline and PostgreSQL/toolchain decisions | Complete as a documentation increment | Pinned upstream catalog, implementation scope, amended architecture and toolchain provenance |
| M0 runtime reference contract capture | Pending | Need an isolated reference server and recorded public/auth/DTO/media behavior; the static SDK does not prove this |
| M1 service, identity, administrator foundation | In progress | Go/PostgreSQL migrations, users/sessions, administrator setup/login, React/MUI shell, remote tests and packaging |
| M2 media catalog | Pending | Scanner, metadata, images, queries, permissions and administrative library workflow |
| M3 initial client playback | Pending | Direct playback, negotiation, subtitles, progress, session/events |
| M4 conversion and hardware pipeline | Pending | Remux/transcode/HLS, tracks, hardware decode/encode, limits and recovery |
| M5 administrator completion | Pending | Metadata, full policies, tasks/devices/keys/settings, backup and restore |
| M6 compatibility release | Pending | Client/reference comparisons, Linux distribution/architecture/GPU matrix, operations and upgrade evidence |
| M7 additional features | Deferred per scope | Explicit feature decisions and their own acceptance gates |

## Environment observations

`test-env` is reachable as a Debian 13 Linux host. Its root filesystem was initially full; only disposable caches are being reclaimed while preserving unrelated running workloads. No GPU device was present in the initial `/dev/dri` and `/dev/nvidia0` inspection. Hardware command generation can be tested there, but actual GPU execution requires suitable hardware and must remain unverified until exercised.

No milestone is complete solely because its build passes. Each implementation increment will append exact test/build results and its pushed revision here or in the corresponding verification report.
