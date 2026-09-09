# Toolchain, database, and hardware verification policy

Updated: **2026-09-10, Asia/Shanghai**. Status: **implementation baseline; installation and runtime capability evidence must be recorded separately**.

## Stable release baseline

| Component | Required baseline | Official evidence |
| --- | --- | --- |
| Go | `1.27.1` | The [Go download JSON](https://go.dev/dl/?mode=json) lists `go1.27.1` with `stable: true`, reconfirmed on 2026-09-10. |
| FFmpeg and ffprobe | `9.0.1` from the same build | The [FFmpeg download page](https://ffmpeg.org/download.html) identifies `9.0.1` as the latest stable release, released on 2026-08-12 and reconfirmed on 2026-09-10. |
| PostgreSQL | Required independent database service | Pin the selected supported PostgreSQL server major/patch and container digest or package source in deployment artifacts. SQLite is not an implementation option. [PostgreSQL versioning policy](https://www.postgresql.org/support/versioning/). |
| PostgreSQL Go driver | `github.com/jackc/pgx/v5`, including `pgxpool` | Pin the exact compatible module version in `go.mod` and `go.sum`. [pgx documentation](https://pkg.go.dev/github.com/jackc/pgx/v5). |
| Administrator frontend | React, TypeScript, Material UI | Pin compatible stable dependency versions in the package manifest and lockfile. Use Material Design for administration; do not add a web playback page. |

Resolve release changes through official sources when updating the toolchain. Commit exact versions and dependency locks so builds remain reproducible; do not use an unpinned `latest` container tag as a release identity. Download Go from the official distribution with its published checksum. For FFmpeg, record the source archive/version, signature or checksum verification method, configure flags, enabled libraries, and license implications of the selected build. [Go downloads](https://go.dev/dl/), [FFmpeg release archives](https://ffmpeg.org/releases/), [FFmpeg legal information](https://ffmpeg.org/legal.html).

These version pins describe the required target. They do not establish that Go, FFmpeg, PostgreSQL, drivers, or codecs are already installed or operational on `test-env`. Record the actual installed versions and build configuration in remote verification evidence before claiming them.

## Build and test boundary

The current user authorization permits local builds to detect compilation/build errors, including Go compilation and the administrator frontend production build. It does not authorize local test suites, schema validators, smoke tests, server execution, HTTP probes, FFmpeg probing, media conversion, or GPU capability checks.

Run unit, integration, compatibility, browser, media, and operational checks through `ssh test-env` on Linux. The user authorizes installing and removing packages/software on that remote environment for this work. Use isolated project databases and synthetic/licensed media fixtures. If the environment is unavailable, record the affected checks as blocked and continue only work that does not require those results; do not execute the checks locally.

Linux is the production platform. Local compilation success is evidence of build correctness only and does not prove Linux runtime behavior, PostgreSQL persistence, FFmpeg availability, or client playback.

## PostgreSQL integration

Use `pgx/v5` and `pgxpool` with explicit pool bounds, timeouts, transaction scopes, and shutdown. Apply numbered migrations under a database advisory lock, retaining version/checksum records. Keep transactions short and enforce invariants using database constraints and row-level coordination. Bound worker concurrency without serializing all PostgreSQL writes through a single writer.

Deployment must supply a PostgreSQL service, persistent database storage, a least-privilege application role, and explicit credentials/TLS configuration. Backups use a compatible `pg_dump --format=custom`; restores use `pg_restore` into a staged database under application maintenance mode. Record source/target PostgreSQL versions, schema version, ownership/ACL handling, server identity behavior, and recovery evidence. [PostgreSQL dump/restore](https://www.postgresql.org/docs/current/backup-dump.html).

## Hardware decode and encode acceptance

The transcoding implementation must account for hardware decoding as well as encoding. Linux profiles include VAAPI, Intel QSV, and NVIDIA where the installed hardware, drivers, and FFmpeg build permit them. The playback planner must distinguish these capabilities instead of treating an encoder list as a hardware acceleration guarantee.

| Capability | Required remote evidence |
| --- | --- |
| Build availability | Actual `ffmpeg`/`ffprobe` versions, configure flags, compiled hardware interfaces, decoders, encoders, and filters. Enumeration alone is insufficient for runtime support. |
| Device access | GPU identity, Linux kernel/driver/runtime versions, service UID/GID permissions, and container/systemd device policy. |
| Hardware decode | Decode representative input codec/profile/bit-depth samples on the selected hardware path, including unsupported inputs and controlled fallback. Record the decoder and hardware device used. |
| Hardware encode | Encode representative output codec/profile/bit-depth samples on the selected encoder independently of input hardware decoding. Verify output playback and requested limits. |
| Combined pipeline | Decode, hardware-frame transfer, scale/format conversion, filtering/tone mapping where supported, and encode together. Verify timing, output integrity, seeking, and resource bounds. |
| Subtitles and filters | Exercise selected text/bitmap subtitles and filter paths that can require downloading frames to system memory and uploading them again. An unsupported combination must not be advertised as available. |
| Failure and cancellation | Test unavailable devices, unsupported formats, permission failure, process cancellation, cache cleanup, and the configured software fallback policy. |

Software H.264/AAC remains a portable correctness baseline when enabled by the selected FFmpeg build. Configuration must state whether a failed hardware plan may fall back to software and under which resource/policy limits. Do not silently claim hardware decoding when a software decoder was selected, and do not label all VAAPI/QSV/NVIDIA hardware supported after checking one device. [FFmpeg hardware device and acceleration options](https://ffmpeg.org/ffmpeg-doc.html), [FFmpeg codecs](https://ffmpeg.org/ffmpeg-codecs.html), [FFmpeg filters](https://ffmpeg.org/ffmpeg-filters.html).

If `test-env` lacks a required GPU, record that profile as unverified or blocked with its missing prerequisite. Implemented command planning and CPU-only tests do not close the hardware runtime acceptance gate.
