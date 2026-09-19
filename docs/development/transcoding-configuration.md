# Linux transcoding configuration

Updated: **2026-09-19**. Phase 1 codec and AMD configuration are implemented;
selected-profile acceptance, builds and closeout are complete in the phase 1 record.
Phase 2's formal v3 runtime, selected CPU/GPU/browser scopes and final builds
also passed; owned PostgreSQL/worker/documentation closeout is complete.

`Config.Load` enables the configured conversion service by default. Set
`GOBY_TRANSCODING_ENABLED=false` to disable conversion while retaining the
configured resource policy for a later restart. A directly constructed, entirely
zero `TranscodingConfig` remains disabled. Explicit malformed settings fail
configuration loading even when conversion is disabled.

The same manager serves MPEG-TS/fMP4 HLS, [progressive audio](audio-playback.md), and
[progressive MP4 video](progressive-video-playback.md). Universal/legacy audio
and progressive video need no separate worker pool. Original-file delivery
remains available when conversion is disabled and current source/access checks
pass. The administrator dashboard does not add a consumer audio or video player.

The checked-in [Linux environment example](../../deploy/linux/.env.example)
contains all conversion settings. Values are read at startup; changing an
environment file requires a service restart.

The accepted [managed settings](settings.md) provide four native output-planning
overrides over environment defaults, together with explicit server-name modes.
Resetting a native numeric override resumes its deployment default. M5h adds
the independent `Encoding.TranscodingMaxWidth` ceiling through the
[native API](../api/settings.md) and supported
[Emby configuration routes](../api/configuration.md). A positive additional
width combines with native width by taking the smaller value; zero removes only
the additional ceiling. Native width, height, bitrate, and resource budgets still
apply. This value has no additional environment variable. Hardware and resource
controls remain startup-only, and previously registered plans retain their
concrete settings.

| Environment variable | Default | Accepted range or meaning |
| --- | --- | --- |
| `GOBY_FFMPEG` | `ffmpeg` | FFmpeg executable; select the private phase 1 binary for its required encoders and strict Dolby Vision filter |
| `GOBY_FFPROBE` | `ffprobe` | ffprobe executable from the same FFmpeg build |
| `GOBY_TRANSCODING_ENABLED` | `true` | Boolean |
| `GOBY_TRANSCODE_CACHE` | `/var/cache/goby/transcodes` | Canonical absolute Linux directory, excluding `/` |
| `GOBY_TRANSCODE_THREADS` | `2` | 1–64 per FFmpeg thread setting |
| `GOBY_TRANSCODE_MAX_JOBS` | `2` | 1–64 running jobs |
| `GOBY_TRANSCODE_MAX_USER_JOBS` | `1` | 1 through the total running-job limit |
| `GOBY_TRANSCODE_MAX_SESSION_JOBS` | `1` | 1 through the total running-job limit |
| `GOBY_TRANSCODE_MAX_QUEUE_JOBS` | `16` | 1–1,024 waiting jobs |
| `GOBY_TRANSCODE_MAX_RETAINED_JOBS` | `128` | Running-job limit through 4,096 cached job records |
| `GOBY_TRANSCODE_MAX_CACHE_BYTES` | `21474836480` | 1 byte through 1 PiB; default 20 GiB |
| `GOBY_TRANSCODE_MAX_JOB_BYTES` | `8589934592` | 1 byte through the total cache limit; default 8 GiB |
| `GOBY_TRANSCODE_MIN_FREE_BYTES` | `536870912` | 1 byte through 1 PiB; default 512 MiB |
| `GOBY_TRANSCODE_MAX_BITRATE` | `20000000` | 1–1,000,000,000 bits per second; HLS/output-media planning limit, not HTTP rate policing |
| `GOBY_TRANSCODE_MAX_WIDTH` | `1920` | 1–8,192 pixels |
| `GOBY_TRANSCODE_MAX_HEIGHT` | `1080` | 1–8,192 pixels |
| `GOBY_TRANSCODE_MAX_AUDIO_CHANNELS` | `8` | 1–8 channels |
| `GOBY_HW_DECODER` | `software` | `software`, `vaapi`, `qsv`, or `cuda` |
| `GOBY_HW_ENCODER` | `software` | `software`, `vaapi`, `qsv`, or `nvenc` |
| `GOBY_HW_DEVICE` | Empty | See hardware selection below |

Numeric environment values are decimal integers without unit suffixes. An empty
variable selects its documented default. An explicit zero is rejected for the
numeric settings above, avoiding implicit replacement by engine defaults.
Accepted limits do not promise a usable output for every source: a restrictive
bitrate, dimension, or channel policy can leave no compatible conversion.

For progressive audio, exact request targets remain separate from maximum
bitrate/channel/sample-rate ceilings. Lossy codecs use an encoded-media bitrate
target; PCM uses its payload rate, and FLAC uses a conservative frame/sample
bound. Progressive container startup bytes do not become an estimated bitrate
penalty for a short source, but still count toward the shared byte quotas. WAV
selects 16-bit PCM; FLAC supports explicit 16- or 24-bit output. These request
options and supported source-timing requirements are documented in the
[audio contract](audio-playback.md), not extra startup configuration.

The cache must be dedicated to Goby, owned by the service user, and not writable
by other users. Its Linux initialization rejects symbolic links, unfamiliar
contents, and a conflicting owner lock. Only the final directory component can
be created by the engine, so its parent must exist and be writable by the service
user. Do not select a media directory or share the cache with another server.
Periodic cache monitoring is not a filesystem quota; it can overshoot between
observations. The [engine documentation](transcode-engine.md) describes process,
reader, timeout, and storage behavior.

The supplied systemd unit uses `User=goby`, `CacheDirectory=goby`, and
`CacheDirectoryMode=0700`. systemd prepares the parent `/var/cache/goby` with the
service's ownership and permits writes there under `ProtectSystem=strict`; Goby
creates its final `transcodes` directory with private permissions. A custom cache
outside this tree needs an administrator-provisioned parent and an explicit
`ReadWritePaths` exception in a service override. This does not change media
directory permissions.

## Hardware selection

Decoding and encoding can be selected independently. Software decode can feed
any supported encoder; hardware decode can feed the corresponding hardware
encoder or software. VAAPI, QSV, and CUDA backends cannot be mixed in one plan.
VAAPI/QSV devices accept `/dev/dri/renderD128` through `/dev/dri/renderD255` and
default to `/dev/dri/renderD128`; CUDA/NVENC accepts decimal device indexes 0–31
and defaults to `0`. Startup configuration rejects a device when both codec
selections use software. A subsequently planned Vulkan operation may retain
its render node when only the encoder falls back to software.

Startup configuration validation checks selections without probing a GPU or
launching FFmpeg. Hardware settings require the matching driver, FFmpeg support,
and actual Linux device access by the service account. Grant only the required
render/video group or device permissions in the deployment; the service does
not make devices writable or silently change user groups.

Before registering a new authorized VAAPI output, the server performs bounded
[exact hardware encoding admission](hardware-encoding-admission.md) for its
codec, profile, bit depth, dimensions, frame rate and bitrate, including every
adaptive rendition. Rejection selects the same codec's software encoder only
when that implementation is compiled into the selected toolchain; otherwise
the output is declined. The requested format and geometry remain unchanged.
This is planning-time admission, not automatic retry of an FFmpeg job that
fails after starting. Decoder, filter and full client acceptance are separate.

These hardware selections concern video decoding and encoding. Audio-only
progressive output uses software audio codecs; selecting a video GPU does not
make that audio path hardware-accelerated. Ordinary `test-env` remains GPU-free;
the user-approved AMD worker is described below. Non-AMD GPU execution remains
deferred.

## Phase 1 codecs and AMD processing

The current output implementation maps codecs as follows. These are executable
planning paths, not a claim that every device accepts every listed format.

| Codec | Software encoder | AMD encoder | Output depth/profile |
| --- | --- | --- | --- |
| H.264 | `libx264` | `h264_vaapi` | 8-bit Baseline, Main or High |
| HEVC | `libx265` | `hevc_vaapi` | 8-bit Main or 10-bit Main 10 |
| AV1 | `libaom-av1` | `av1_vaapi` | 8-bit or 10-bit Main |

Client requests and playback profiles select the codec, profile and bit depth
within current source facts and limits. There is no separate startup variable
that selects HEVC, AV1, HDR output or a Vulkan filter. An omitted encoded-video
codec retains the H.264 planning default. HEVC supports TS and MP4 output; AV1
requires MP4, including fMP4 HLS. Intel QSV and NVIDIA NVENC retain their existing H.264 engine
paths and do not gain HEVC/AV1 acceptance from the AMD implementation.

For the isolated AMD worker, the verified phase 2 v3 toolchain can be selected with
the existing service variables:

```dotenv
GOBY_FFMPEG=/opt/goby-amd-media-20260919/toolchains/ffmpeg-9.0.1-goby-cb8b6d298456/bin/ffmpeg
GOBY_FFPROBE=/opt/goby-amd-media-20260919/toolchains/ffmpeg-9.0.1-goby-cb8b6d298456/bin/ffprobe
GOBY_HW_DECODER=software
GOBY_HW_ENCODER=vaapi
GOBY_HW_DEVICE=/dev/dri/renderD128
```

Use the actual selected render node and both binaries from the same installed
prefix. `GOBY_HW_DECODER=vaapi` independently requests hardware video decoding
for eligible ordinary sources; Dolby Vision processing explicitly retains
software HEVC decoding so per-frame RPU metadata reaches the renderer. This
example selects a pipeline and does not assert completed device acceptance or
change any existing service configuration.

The planner selects Vulkan/libplacebo for AMD HDR conversion, deinterlacing and
subtitle composition. VAAPI and Vulkan descend from the same DRM device.
Ordinary VAAPI decoding downloads NV12/P010 frames for exact CPU resizing;
processing/composition can use Vulkan, and an explicit upload supplies VAAPI
encoding. Text subtitles are rasterized by CPU libass into an alpha plane;
Vulkan composites that plane. Bitmap decoding and the final resize also remain
distinct from GPU composition. These mixed paths include CPU/GPU transfers and
are not zero-copy or entirely GPU-resident. Deinterlacing uses Vulkan YADIF on
the selected AMD path; enumerated VAAPI VPP modes are not proof of execution.

Dolby Vision conversion requires the private `strict_dolbyvision` patch and
verified RPU-bearing source facts. Supported profile 5/8 inputs and the strict
zero-residual profile 7 MEL subset can target SDR or 10-bit HEVC/AV1 HDR10.
FEL residual reconstruction and newly authored Dolby Vision output are outside
the implementation. Software codec selection alone does not imply CPU-only
processing when Vulkan is required. See [AMD video processing](amd-video-processing.md)
for source admission, metadata preservation, color processing, subtitle clocks
and the remaining acceptance gates, and [the toolchain policy](toolchain.md)
for build identities and portable runtime dependencies.

## Dynamic time-shift configuration

Phase 2 wires a separate bounded retained-output Store for configured dynamic
sources. These settings are read at startup and require a restart to change.
Their selected remote CPU/GPU/browser contracts and final builds have passed;
original failed attempts and completed PostgreSQL/worker/documentation closeout
are recorded in the
[phase 2 record](amd-media-phase2-20260919.md).

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `GOBY_TIMESHIFT_ENABLED` | `true` | Enable bounded retention when conversion and configured dynamic sources are also available |
| `GOBY_TIMESHIFT_CACHE` | `/var/cache/goby/timeshift` | Private canonical Linux directory, separate from and not overlapping conversion scratch storage |
| `GOBY_TIMESHIFT_WINDOW_SECONDS` | `600` | Configured upper retention horizon; byte pressure can shorten the actual window |
| `GOBY_TIMESHIFT_MAX_WINDOW_BYTES` | `536870912` | 512 MiB hard per-presentation budget, including advertised grace, temporary copies and retained readers |
| `GOBY_TIMESHIFT_MAX_CACHE_BYTES` | `2147483648` | 2 GiB aggregate Store budget |
| `GOBY_TIMESHIFT_MAX_WINDOWS` | `32` | Concurrent retained presentations |
| `GOBY_TIMESHIFT_MAX_USER_WINDOWS` | `4` | Per-user or application-credential presentation limit |

After advertisement, current visible media and required live initialization use
at most one third of the per-window byte budget; the remainder provides room for
promised grace and publication work. The Store continues charging removed but
advertised media until its promise expires, and cannot evict that grace to satisfy
new admission. A fixed target duration and actual segment durations govern the
playlist. The default 600-second horizon is not a guaranteed ten minutes of
replay. See [retention and lifecycle](../../internal/timeshift/README.md).

Only already published output is replayable. Restart discards the temporary
history and old presentation identities. A directly constructed zero-valued
configuration remains disabled; disabling retention does not authorize an
unbounded dynamic cache. Deployment must provision access to this separate cache
directory; this source change does not assert installed service/OCI acceptance.

## Media upgrade and response limits

Migration `0012` introduced scoped client playback references. The current
source uses probe cache version **7**; run a normal library scan to upgrade
older cached probe facts. [M4f acceptance](verification-m4f-video-seek.md)
records its earlier deployed probe-5-to-6 upgrade, preserved metadata/user
state, and verified video seeking. The historical
[M5g deployment](m5g-deployment-evidence.json) records probe 6 at database schema
20; it does not establish deployment of the phase 1 source. These versions
describe different stores; a database migration alone does not refresh old
media facts.

An unchanged source with a current probe-version snapshot is not re-probed merely
because its optional seek index is absent or FFmpeg changed. Use
[Refresh media details](../api/admin-scans.md) to request fresh probing of such
sources; the [video-seeking contract](video-fast-seek.md) describes eligibility
and linear fallback. The earlier audio profile/Ogg increment itself added no
database migration or environment variable. A configured encoder does not
substitute for current source timing or authorization. Complete continuous audio
coverage and integer sample facts govern converted length, including accurate
WAV headers. The supported Ogg Opus/Vorbis/modern FLAC subset requires physical-page
and codec/header integrity plus complete packet/frame evidence. Chaining and
Matroska/WebM quantized-clock timing remain unproven; supported original-file
delivery is retained after rescan when conversion cannot establish exact timing.

HLS and progressive consumers share the same manager quotas and reader leases.
Progressive audio/video responses share the server's 64 original/progressive media response slots,
wait at most 45 seconds for startup, impose a 30-second individual write deadline,
and have a four-hour response limit. A partially transmitted failure aborts the
HTTP response. These HTTP limits are current implementation bounds rather than
additional environment variables. Detailed lifecycle and supported output formats
are in [audio playback](audio-playback.md) and
[progressive video playback](progressive-video-playback.md).

[Audio PlaybackInfo](audio-profile-playback.md) uses these same limits for ordered
HTTP/HLS profiles and serializes constructible progressive settings into the
standard stream URL. It reserves no progressive encoding capacity; GET performs
fresh admission, and HEAD starts no encoder. Exact audio targets and independent
channel ceilings remain distinct, including `TranscodingMaxAudioChannels`, which
only constrains Universal/legacy selection after an original file has been ruled
out.

[Progressive video](progressive-video-playback.md) implements bounded
H.264/HEVC/AV1 fragmented MP4, ordered HTTP/HLS profile selection, compatible
stream copy, subtitle burn-in and supported encoded seeks using the verified
source format clock. Source-bound copy seeking has additional packet and
random-access requirements in the [copy-seeking contract](copy-seek-compatibility.md).
Eligible H.264/HEVC software-decoded seeks can use verified private restart
evidence to skip
prefix video decoding while selected audio retains its independent linear
history. Unsupported or stale optional evidence retains the linear path;
hardware decoding does not borrow the software proof. The
[M4f verification](verification-m4f-video-seek.md) retains its earlier H.264
acceptance; current additions belong to the phase 1 record.

The phase 1 record binds codec, copied-video seek and AMD processing results
to their final source. Efficient long-source audio I/O,
additional input/timing/profile cases and aggregate resource
isolation remain separate work. Startup settings and an available encoder do
not establish complete third-party-client compatibility or universal
constant-time seeking. Bounded finite-source packed AAC/MP3 HLS is already
implemented under the [advanced-media contract](advanced-media.md); it is not
an unbounded dynamic or adaptive audio path.

## Dedicated test deployment

Ordinary verification remains on `ssh test-env`, PVE VM 101. Because that VM
has no render device, the user approved the separate unprivileged PVE CT 104
`goby-amd-worker` for isolated AMD/toolchain verification, reached through
`ssh pve` and `pct exec 104`. It exposes the selected AMD render node to the
non-root `goby-worker` account. This is a task-specific remote exception, not
local verification authorization or OCI playback acceptance. See the
[phase 1 execution record](amd-media-phase1-20260919.md) for worker and result
identities; the successful private toolchain build does not close the latest
GPU media regression gates by itself; the phase 1 record supplies the results.

`scripts/test-env/run-foundation.sh` provisions `/dev/shm/goby-transcodes-test`
with owner `goby:goby` and mode `0700`. Its deployment ownership record lives at
`/opt/goby-test/transcode-cache.owner`, separate from engine-owned cache contents.
An existing cache without that record, a mismatched record, or a symbolic link
causes deployment to stop without claiming the directory.

The script appends conversion settings to `runtime.env` only when the setting is
absent. Its default test policy is 128 MiB total, 32 MiB per job, and 16 MiB minimum
free space, keeping conversion output in the dedicated memory-backed cache. The
service's transcode-cache write exception names this dedicated directory. Existing
explicit configuration remains unchanged and may need its corresponding service
override when it names a different cache.

The accepted [M5g deployment evidence](m5g-deployment-evidence.json) records
PID 3614026, UID 995, schema 20/probe 6, matching installed artifacts, and
preserved runtime/service configuration. Its [verification report](verification-m5g-settings.md)
records the full Linux regression, isolated browser/restarts, and main-service
settings workflow. That live workflow updates and restores settings without
launching media or planning requests.
Actual converted-video and fast-seek evidence remains separately recorded in
[M4f's deployed workflow](m4f-deployed-video-fast-seek.json). These reports establish
their own bounded workflows. Selected AMD execution was subsequently accepted
in phase 1; full client acceptance remains separate.
