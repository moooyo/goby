# Linux transcoding configuration

`Config.Load` enables the configured conversion service by default. Set
`GOBY_TRANSCODING_ENABLED=false` to disable conversion while retaining the
configured resource policy for a later restart. A directly constructed, entirely
zero `TranscodingConfig` remains disabled. Explicit malformed settings fail
configuration loading even when conversion is disabled.

The same manager serves MPEG-TS HLS, [progressive audio](audio-playback.md), and
[progressive MP4 video](progressive-video-playback.md). Universal/legacy audio
and progressive video need no separate worker pool. Original-file delivery
remains available when conversion is disabled and current source/access checks
pass. The administrator dashboard does not add a consumer audio or video player.

The checked-in [Linux environment example](../../deploy/linux/.env.example)
contains all conversion settings. Values are read at startup; changing an
environment file requires a service restart.

The accepted [M5g managed-settings increment](settings.md) adds database overrides
for server name and four output-planning ceilings. Environment values supply
startup defaults; non-null database overrides take precedence. Reset resumes
the current deployment default. Hardware and resource controls remain
startup-only. The [native API](../api/settings.md) does not expose an Emby
ConfigurationService adapter.

| Environment variable | Default | Accepted range or meaning |
| --- | --- | --- |
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
and defaults to `0`. A device is invalid when both decode and encode use software.

Configuration validation checks selections without probing a GPU or launching
FFmpeg. Hardware settings require the matching driver, FFmpeg support, and actual
Linux device access by `goby`. Grant only the required render/video group or
device permissions in the deployment; the service does not make devices writable
or silently change user groups. An unavailable hardware backend is not treated
as successful conversion, and software fallback is not automatic.

These hardware selections concern video decoding and encoding. Audio-only
progressive output uses software audio codecs; selecting a video GPU does not
make that audio path hardware-accelerated. Actual device-specific video execution
remains unverified on the current GPU-free test host.

## Media upgrade and response limits

Migration `0012` introduced scoped client playback references. The current
probe cache version is **6**; run a normal library scan to upgrade older cached
probe facts. [M4f acceptance](verification-m4f-video-seek.md) includes the deployed
probe-5-to-6 upgrade, preserved metadata/user state, and actual verified video
seeking. The current [M5g deployment](m5g-deployment-evidence.json) retains probe 6
at database schema 20. These versions describe different stores; a database
migration alone does not refresh old media facts.

An unchanged source with a current probe-6 snapshot is not re-probed merely
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

[Progressive video](progressive-video-playback.md) supports bounded H.264/AAC
fragmented MP4, ordered HTTP/HLS profile selection, compatible stream copy at
zero start, and supported encoded seeks using the verified source format clock.
Eligible H.264 software-decoded seeks can use
[verified private restart evidence](verification-m4f-video-seek.md) to skip
prefix video decoding while selected audio retains its independent linear
history. Unsupported or stale optional evidence retains the linear path;
hardware decoding does not borrow the software proof.

Nonzero copied-video seeks, efficient long-source audio I/O, additional
input/timing/profile cases, packed-audio HLS, richer subtitle/output support,
actual GPU execution, and aggregate resource isolation remain unfinished.
Startup settings and an available encoder do not establish complete
third-party-client compatibility or universal constant-time seeking.

## Dedicated test deployment

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
their own bounded workflows; GPU execution and full client acceptance remain pending.
