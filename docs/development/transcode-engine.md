# Linux conversion engine

The M4a engine provides conversion planning, PostgreSQL job records, bounded
FFmpeg execution, and an owned HLS output cache. Its planner-to-segment path is
verified with real Linux media. Connecting this engine to the Emby HLS HTTP graph
and implementing the complete VOD seek strategy remain required work. The current
server therefore continues to advertise its existing original-file playback
surface, not a completed HLS transcoding API.

## Plans and output facts

`playback.PlanConversion` preserves the existing original-file `Evaluate` result
and evaluates an additional output candidate. It consumes authenticated server
and user limits supplied by its caller, never authorizes a user itself, and starts
no process during negotiation.

The planner considers stream copy, audio conversion, video conversion, and their
combinations against a declared Streaming/HLS/MPEG-TS TranscodingProfile. Initial
encoded outputs are H.264, AAC LC, and MP3. It selects actual stream indexes and
applies copy restrictions, source duration, starting ticks, bitrate, audio channel
and sample-rate limits, dimensions, frame rate, container/codec conditions, and
supported external text-subtitle declarations. Conditions whose applicability
changes after resizing/downmixing are reevaluated through bounded refinement.

The resulting output facts pass the same profile evaluator again. Input facts
that the output cannot promise are cleared: input time bases and codec tags, and
uncontrolled encoded profile/level/reference-frame or color facts, are not copied
into a fabricated result. Required unknown output conditions cause rejection.
MPEG-TS Annex B output does not retain MP4's AVC framing claim.

Initial numeric server defaults are 20 Mbps total, 1920x1080, and up to eight audio
channels. The planner preserves source channels when allowed; MP3 is limited to
two channels. It reserves ten percent of the bitrate budget for transport
overhead, which is a planning allowance rather than a packet-level rate limiter.
The caller can supply different bounded limits. Remux/audio/video permissions
default to false until explicitly supplied by the authorized caller.

Explicit HDR conversion, unknown high-bit-depth HDR risk, unsupported interlace,
embedded/bitmap subtitle rendering, unsupported output conditions, and unavailable
codec/container settings are declined with reasons. Pixel-format conversion is
not presented as HDR tone mapping. Original `Evaluate` behavior is unchanged.

## FFmpeg execution

Plans contain identifiers, indexes, timing, output settings, and hardware policy.
They contain no bearer token, input path, shell command, arbitrary filter text,
or output path. The runner borrows an already opened regular source and makes it
available as descriptor 3. FFmpeg reopens `/proc/self/fd/3` without changing the
caller's file offset. The manager owns and eventually closes the accepted source.

Arguments use explicit stream mapping, disabled subtitle/data/metadata mapping,
a local protocol/demuxer allowlist, bounded thread settings, fixed output names,
and no shell. The job directory starts empty. HLS uses `temp_file` publication,
an append-only EVENT playlist, and retained completed MPEG-TS segments. Encoded
video forces keyframes at the selected segment interval. Stream copy retains
source keyframe spacing; neither synthetic equal-length segments nor unsupported
independent-segment guarantees are invented. See the official [HLS muxer
documentation](https://ffmpeg.org/ffmpeg-formats.html#hls).

The measured playlist parser validates this closed output subset, exact segment
names, sequence, decimal durations, and completion state. URI rewriting preserves
those facts and accepts only bounded relative server URLs. It rejects external
resources, path traversal, temporary files, unsupported key/map features, and
tag injection. An unfinished event is never relabeled as a complete VOD.

The runner processes `-progress pipe:1` incrementally with a bounded line size;
normal progress can continue for hours without accumulating memory. Stderr keeps
only a 64 KiB tail for internal diagnostics. Public job errors use server-owned
codes. An explicit subprocess environment allowlist excludes database/setup
credentials, arbitrary reporting paths, and unrelated environment settings.

Each invocation has its own Linux process group. Cancellation sends SIGTERM,
then SIGKILL after two seconds. On exit, `waitid(WEXITED|WNOWAIT)` retains the
leader's PID while the group and kill timer are retired, before `cmd.Wait` reaps
the leader and drains pipes. This also reclaims surviving children after a
successful parent exit. Non-Linux execution returns an unsupported error; the
Windows build remains useful only for compilation checks.

## Hardware decode and encode

`transcode.Hardware` selects decoding and encoding separately:

| Decoder | Encoder options in the initial command builder |
| --- | --- |
| Software | Software, VAAPI, QSV, NVENC |
| VAAPI | Software or VAAPI |
| QSV | Software or QSV |
| CUDA | Software or NVENC |

VAAPI/QSV device selectors are bounded `/dev/dri/renderD*` paths; CUDA uses a
bounded device number. Same-backend hardware paths retain device frames through
the supported scaler. Hardware decode to software encode explicitly downloads
frames; software decode to hardware encode explicitly uploads them. Mixed
hardware backends are rejected. Software fallback after a hardware failure is
not automatic and would need separate resource admission.

QSV uses a VAAPI-derived device and the pinned FFmpeg version's automatic matching
decoder selection. QSV `forced_idr` and NVENC `forced-idr` are distinct options.
The choices were checked against the official [hardware-device guidance](https://ffmpeg.org/ffmpeg.html#Advanced-Video-options),
[QSV encoder options](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/qsvenc.h),
and [decoder selection](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_demux.c).
Command construction and compiled interfaces are not GPU execution evidence.
The current test host has no GPU; actual hardware decode/encode, drivers, device
permissions, filters, quality, and concurrent workloads remain unverified.

## Persistence and resource ownership

Migration `0010` adds `encoding_jobs` to PostgreSQL. Creation rechecks the enabled
user, Emby authentication/session/device, and active playback owner under locks,
and checks expiry again at insertion. Records fix every scope dimension, source
stamp, plan, and creation time. Status progresses from queued to running and a
terminal result; completed/failed/cancelled/interrupted records cannot revive.
Cleanup may persist a terminal result after logout. Source files, tokens, output
paths, and stderr are not database fields.

Startup recovery marks abandoned queued/running rows interrupted while retaining
history. The caller must already hold the existing exclusive catalog ownership;
a cache-directory lock alone cannot coordinate two roots using one database.
Historical rows follow parent account/session/item retention. A separate bounded
administrator retention policy remains to be added.

The manager's initial defaults are:

| Resource | Default |
| --- | --- |
| Running processes | 2 total, 1 per user and authentication session |
| Waiting jobs | 16 |
| Retained in-memory/cache jobs | 128 |
| Threads | 2 per configured FFmpeg thread setting |
| Open output readers | 256 total, 32 per job |
| Cache storage | 20 GiB total, 8 GiB per job, 512 MiB minimum free space |
| Time budgets | 30 s startup, 30 s without progress, 2 min idle, 4 h total runtime |
| Monitoring interval | 250 ms |

All limits are bounded options. Periodic byte/free-space checks can overshoot
between observations; filesystem quotas or delegated cgroups are required for
hard operating-system ceilings. Thread settings alone do not isolate CPU,
memory, process count, or GPU resources.

The cache requires an absolute, dedicated, process-owned Linux directory with
an ownership marker and exclusive lock. Descriptor-relative operations reject
symlinks and unfamiliar contents. FFmpeg's working directory is addressed through
the retained parent directory descriptor, so renaming the configured directory
does not redirect output into a replacement path. Recovery removes only verified
owned output. Reader leases defer deletion until readers close.

`Ensure` takes ownership of every supplied input, including duplicate/error paths.
Identical scope/source/plan requests reuse work. `WaitReady` requires a published
playlist and a complete segment. `Open` requires the exact scope and a generated
filename; the API must still revalidate token, library, playback policy, and source
before each call. Cancellation, startup/no-progress/idle/runtime limits, cache
failure, and shutdown reclaim owned resources. Caller timeouts do not abandon
background cleanup. Conversion never updates watched or playback-position data.

## Next integration work

The [reference study](../research/hls-reference.md) establishes full VOD manifests
and stable global segment numbers, with `EXT-X-START` seek hints. Its successful
segments also establish usable codec/packet evidence; its remux failures are not
a behavior to reproduce. Before advertising HLS, the HTTP layer still needs the
complete master/media/segment graph, full-duration seek scheduling, authenticated
child URLs, per-request authorization/source checks, current transcode policy,
ActiveEncodings cleanup, configuration/deployment integration, and real-client
acceptance. The measured engine playlist is an internal artifact, not a shortcut
around those requirements. The remaining M4/M5/M6 scope stays active.
