# Toolchain, database, and hardware verification policy

Updated: **2026-09-19, Asia/Shanghai**. Status: **formal amd-media-v3 toolchain built; selected CT AMD and VM CPU/browser scopes verified; phase 2 resource/documentation closeout complete**.

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

The advanced software media profile also requires FFmpeg's `zscale`, `tonemap`,
`bwdif`, `subtitles`, and `overlay` filters, libx264 encoding, and the declared
audio encoders. HDR conversion uses a real linear-light transform; changing
color tags alone is not a substitute. Build FFmpeg with `--enable-libzimg` and
`--enable-libass` for this profile. FFmpeg documents libzimg as the dependency
of its [zscale filter](https://ffmpeg.org/ffmpeg-filters.html#zscale).

## Phase 1 private media toolchain

The phase 1 installer adds `libx265` for HEVC and `libaom-av1` for AV1, alongside
the existing H.264/audio encoders. The AV1 implementation uses libaom because
the engine requires arbitrary forced keyframe timestamps for independent HLS
segments. VAAPI codec interfaces, Vulkan, libdrm and libplacebo are compiled;
their presence does not establish device support.

FFmpeg 9.0.1 requires libplacebo at least 7.351.0. The installer builds a private
libplacebo **7.351.0**, pinned to signed tag `v7.351.0` and commit
`3188549fba13bbdf3a5a98de2a38c2e71f04e21e`. Vulkan, the Vulkan loader,
glslang and Dolby Vision reshaping are explicitly enabled. FFmpeg supplies the
parsed RPU side data, so the separate libdovi dependency is disabled. The
[private strict Dolby Vision patch](../../scripts/test-env/toolchain-patches/README.md)
adds `strict_dolbyvision` and `strict_dolbyvision_profile`: it rejects missing
per-frame RPU data and unsupported residuals, and normalizes a private metadata
copy only for the supported exact zero-residual profile 7 MEL condition. This
does not implement FEL reconstruction or new Dolby Vision encoded output.

[`install-toolchains.sh`](../../scripts/test-env/install-toolchains.sh) accepts
these build settings; they are not Goby service settings:

| Build environment variable | Default | Meaning |
| --- | --- | --- |
| `GOBY_TOOLCHAINS` | `/opt/goby-toolchains` | Installation root; retained service toolchains must remain intact |
| `GOBY_BUILD_ROOT` | `/var/tmp` | Executable scratch filesystem for a new private build directory |
| `GOBY_BUILD_JOBS` | `2` | Positive build parallelism; the constrained AMD worker build used `1` |
| `GOBY_TOOLCHAIN_REUSE_ROOT` | Empty | Optional saved source snapshot containing the FFmpeg archive/signature/keys and pinned Git repositories, including fast_float |

The installer verifies the Go archive checksum, FFmpeg release signature,
libplacebo signed tag and expected dependency commits. Reused source objects
are verified again, then development headers and libraries are rebuilt; an
earlier FFmpeg binary or object tree is not reused. Patch bytes and the
installer/helper identities enter the recipe hash. A changed recipe publishes
`ffmpeg-9.0.1-goby-<recipe hash prefix>` instead of overwriting an earlier
toolchain. Reusing an existing prefix requires its recipe and complete file
manifest to match. Failed builds retain their private scratch directory and
logs for diagnosis.

The published prefix contains matching FFmpeg/ffprobe binaries, a private
shared-library closure with relative `$ORIGIN` RPATHs, and `metadata/` records
for source signatures, patch/input/output hashes, configuration, dependency
versions, licenses and installed-file hashes. Move the entire prefix together;
no global `LD_LIBRARY_PATH` change is required. A compatible system loader and
glibc, target VAAPI drivers/Vulkan ICDs, Fontconfig configuration and fonts remain
external dependencies. This is not a self-contained driver distribution.

The historical phase 1 CT 104 build published
`/opt/goby-amd-media-20260919/toolchains/ffmpeg-9.0.1-goby-65af9bed5365`, with Go
1.27.1 in the same toolchain root. This records a successful build and its
compiled features. Selected AMD profiles have completed the separate phase 1
media gates; broader GPU acceptance is not implied. See [AMD processing](amd-video-processing.md),
[exact hardware encoding admission](hardware-encoding-admission.md) and the
[phase 1 execution record](amd-media-phase1-20260919.md) for the separate media
and device gates. Existing native service binaries are not promoted by this
build record; OCI and non-AMD GPU acceptance remain deferred.

## Phase 2 formal v3 runtime

The formal `amd-media-v3` recipe rebuilds libplacebo and FFmpeg from source and
adds the separately recorded decoder-queue receive-wakeup patch and native gate.
`phase2-toolchain-successor02` published a new CT 104 prefix without replacing the
phase 1 prefix or installing/upgrading system packages:

`/opt/goby-amd-media-20260919/toolchains/ffmpeg-9.0.1-goby-cb8b6d298456`

The exact FFmpeg SHA-256 is
`c8887f1a2b5a6777c1285fd7514049ec175947048d82e4b3700e44115da34843`;
the matching ffprobe SHA-256 is
`fddc50127245a7a836b5fb6789e1606ebe9e12bcf3c9e2fab5b32bfb7a0b703f`.
The native baseline reproduces four expected deadlocks among seven cases, while
the candidate passes eight cases with zero deadlocks. The original failed native
assertion and first formal-build monitoring stop remain recorded; this is not a
first-attempt success claim.

Actual race-enabled CT execution passed the four selected live/finite/P8.1/P7-MEL
groups with 21 pass events, zero failures and zero skips. The previous prefix,
recorded system GPU libraries and dpkg package state were preserved. On VM 101,
`software-environment-v3.json` records 146 matched installed files and successful
version probes; the v3 CPU-media regression and final native-video/HLS.js browser
scope then passed. The latter uses a verified warm browser profile and does not
establish full original Emby Web compatibility or arbitrary hardware support.

The [phase 2 record](amd-media-phase2-20260919.md) owns all source/runtime identities,
environment-specific skips, final application artifacts and retention receipts.
Final owned PostgreSQL/worker closure and documentation are complete. Existing
production services have not been promoted by these results.

## Copy-timestamp progress successor source

The `amd-media-v4` installer source adds the
[unknown-progress-timestamp repair](../../scripts/test-env/toolchain-patches/progress-copyts-nopts/README.md)
to the two existing FFmpeg 9.0.1 patches. It preserves an unknown scheduler clock
through the existing `N/A` progress representation instead of subtracting a
previous copy-timestamp origin from `AV_NOPTS_VALUE`. No Goby parser limit or
media timestamp behavior is relaxed.

Its distinct recipe includes the new patch and real-reporter native harness
hashes, keeps the original decoder queue gate, and publishes a successor prefix
only after both gates pass. The OCI source recipe includes the same progress
patch and gate. The earlier v3 binary identities and acceptance records above
remain historical evidence; these source changes are not an executed v4 build
or a replacement for actual progressive/compound media acceptance.

## Build and test boundary

Ordinary formatting, compilation, production builds, type checks, test suites,
schema validators, smoke tests, server execution, HTTP probes and software media
verification run through `ssh test-env`, PVE VM 101. Local verification requires
explicit authorization in the current task; historical permissions do not carry
forward automatically.

For phase 1 AMD work, the user explicitly approved the independent PVE CT 104
`goby-amd-worker` as an exception because VM 101 has no `/dev/dri`. Use
`ssh pve` and `pct exec 104` for its isolated toolchain builds and AMD/device/media
checks. This unprivileged Debian worker exposes the selected render node to its
non-root worker account. It does not require moving the GPU out of CT 100,
restarting VM 101, or changing host device permissions. This verification
exception does not authorize local testing or establish OCI deployment support.

Outside the approved AMD exception, run unit, integration, compatibility, browser, media, and operational checks through `ssh test-env` on Linux. The user authorizes installing and removing packages/software on that remote environment for this work. Use isolated project databases and synthetic/licensed media fixtures. If the designated environment is unavailable, record the affected checks as blocked and continue only work that does not require those results; do not execute the checks locally.

Linux is the production platform. Local compilation success is evidence of build correctness only and does not prove Linux runtime behavior, PostgreSQL persistence, FFmpeg availability, or client playback.

## PostgreSQL integration

Use `pgx/v5` and `pgxpool` with explicit pool bounds, timeouts, transaction scopes, and shutdown. Apply numbered migrations under a database advisory lock and check the published migration-content baseline. Historical database rows record version/name; do not infer their original SQL checksums from current source. Keep transactions short and enforce invariants using database constraints and row-level coordination. Bound worker concurrency without serializing all PostgreSQL writes through a single writer.

Deployment must supply a PostgreSQL service, persistent database storage, a least-privilege application role, and explicit credentials/TLS configuration. Backups use a compatible `pg_dump --format=custom`; restores use `pg_restore` into a staged database under application maintenance mode. Record source/target PostgreSQL versions, schema version, ownership/ACL handling, server identity behavior, and recovery evidence. [PostgreSQL dump/restore](https://www.postgresql.org/docs/current/backup-dump.html).

## Hardware decode and encode acceptance

The transcoding implementation must account for hardware decoding as well as encoding. Linux profiles include VAAPI, Intel QSV, and NVIDIA where the installed hardware, drivers, and FFmpeg build permit them. Phase 1 execution targets AMD VAAPI/Vulkan; Intel and NVIDIA acceptance remains deferred. The playback planner must distinguish these capabilities instead of treating an encoder list as a hardware acceleration guarantee.

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

New VAAPI plans also perform bounded [exact encoder admission](hardware-encoding-admission.md)
before registering an immutable output. A successful three-frame tuple check
is narrower than source decode, HDR, deinterlacing, subtitle, synchronization,
performance or client acceptance. Rejected tuples can select the compiled
software implementation of the same requested codec before registration;
this does not convert a failed running hardware job into a verified software
result. The [configuration contract](transcoding-configuration.md) describes
the remaining device and CPU/GPU boundaries.

If the designated environment lacks a required GPU, record that profile as
unverified or blocked with its missing prerequisite; use the explicitly approved
CT 104 exception for AMD work. Implemented command planning and CPU-only tests
do not close the hardware runtime acceptance gate.
