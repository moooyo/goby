# Linux transcoding configuration

`Config.Load` enables the configured conversion service by default. Set
`GOBY_TRANSCODING_ENABLED=false` to disable conversion while retaining the
configured resource policy for a later restart. A directly constructed, entirely
zero `TranscodingConfig` remains disabled. Explicit malformed settings fail
configuration loading even when conversion is disabled.

The same manager serves MPEG-TS HLS and [progressive audio](audio-playback.md).
Universal/legacy audio conversion needs no additional environment variables or
separate worker pool. Original-file delivery remains available when conversion
is disabled and current source/access checks pass. The administrator dashboard
does not add a consumer audio or video player.

The checked-in [Linux environment example](../../deploy/linux/.env.example)
contains all conversion settings. Values are read at startup; changing an
environment file requires a service restart.

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

## Audio upgrade and response limits

Migration `0012` adds scoped client playback references; probe cache version 3
requires normal rescans of existing libraries. A configured encoder does not
substitute for current source timing or authorization. Complete continuous audio
coverage and integer sample facts govern converted length, including accurate
WAV headers; unsupported timing cases can retain original delivery after rescan.

HLS and progressive consumers share the same manager quotas and reader leases.
Progressive responses also share the server's 64 original/audio response slots,
wait at most 45 seconds for startup, impose a 30-second individual write deadline,
and have a four-hour response limit. A partially transmitted failure aborts the
HTTP response. These HTTP limits are current implementation bounds rather than
additional environment variables. Detailed lifecycle and supported output formats
are in [audio playback](audio-playback.md).

Progressive video, progressive DeviceProfile negotiation through PlaybackInfo,
additional input/timing cases, packed-audio HLS and richer subtitle/output support
remain unfinished. Startup settings and an available encoder do not establish
complete third-party-client compatibility or hard resource isolation.

## Dedicated test deployment

`scripts/test-env/run-foundation.sh` provisions `/dev/shm/goby-transcodes-test`
with owner `goby:goby` and mode `0700`. Its deployment ownership record lives at
`/opt/goby-test/transcode-cache.owner`, separate from engine-owned cache contents.
An existing cache without that record, a mismatched record, or a symbolic link
causes deployment to stop without claiming the directory.

The script appends conversion settings to `runtime.env` only when the setting is
absent. Its default test policy is 128 MiB total, 32 MiB per job, and 16 MiB minimum
free space, keeping conversion output off the host's constrained root disk. The
service receives a write exception only for this dedicated cache. Existing
explicit configuration remains unchanged and may need its corresponding service
override when it names a different cache.

These packaging changes do not constitute deployment verification. Configuration
tests, shell validation, non-root cache initialization, restart recovery, and real
conversion checks run on the dedicated Linux test host.
