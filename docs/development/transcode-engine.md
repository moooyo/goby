# Linux conversion engine

The conversion engine provides planning, PostgreSQL job records, bounded FFmpeg
execution, and an owned media output cache. Its HLS planner-to-segment path is
verified with real Linux media. The [HLS adapter](hls-playback.md) connects it to
playback negotiation, complete source timelines, authorized segment URLs, seek
production and cleanup. The [audio adapter](audio-playback.md) adds Universal and
legacy selection with progressive output on the same manager, while
[Audio PlaybackInfo](audio-profile-playback.md) selects ordered HTTP/HLS profiles.
[Startup configuration](transcoding-configuration.md) enables conversion by default and
supplies cache, concurrency, output and video-hardware policy. Progressive video,
additional audio timing/format/profile cases, richer subtitles, hard resource
isolation, actual GPU execution and real-client acceptance
remain active requirements.

## Plans and output facts

`playback.PlanConversion` preserves the existing original-file `Evaluate` result
and evaluates an additional HLS output candidate. `playback.PlanProgressiveAudio`
evaluates explicit progressive audio targets separately from Universal's choice
to serve a compatible original. `playback.PlanAudioConversion` preserves the
original evaluation and declared HTTP/HLS TranscodingProfile order for Audio
PlaybackInfo, selecting the first authorized output that passes profile checks.
These planners consume authenticated server and user limits supplied by the
caller, never authorize a user themselves, and start no process during negotiation.

Progressive profile adaptation intersects request/server/profile streaming
ceilings and constructs supported exact or bounded output targets. Applicable
CodecProfiles and ContainerProfiles, including output-dependent applicability,
are evaluated again on the projected output. Required unknown or contradictory
conditions reject a candidate; an allowed encoding can be retried when copied
facts cannot prove compatibility. Original MediaSource DTOs stay unchanged, and
the resulting standard audio URL serializes the concrete plan. PlaybackInfo
prepares its canonical play but does not reserve a progressive job or revision;
GET performs fresh admission and source/policy checks. See the [profile contract](audio-profile-playback.md).

Audio profile search shares a request-wide 2,048-unit evaluation budget across
progressive and HLS candidates, with bounded per-profile refinements and variants.
Its container aliases normalize consistently across output selectors and codec/
container conditions without changing the original evaluation. Budget exhaustion
returns an explained unsupported result rather than an unevaluated plan.

The HLS planner considers stream copy, audio conversion, video conversion, and their
combinations against a declared Streaming/HLS/MPEG-TS TranscodingProfile. Initial
encoded outputs are H.264, AAC LC, and MP3. It selects actual stream indexes and
applies copy restrictions, source duration, starting ticks, bitrate, audio channel
and sample-rate limits, dimensions, frame rate, container/codec conditions, and
supported external text-subtitle declarations. Conditions whose applicability
changes after resizing/downmixing are reevaluated through bounded refinement.

Progressive audio supports MP3, AAC/ADTS, AAC in fragmented MP4/M4A, FLAC, OGG
with Vorbis/Opus/FLAC, and WAV with signed 16-bit PCM. Copy requires compatible
source codec/framing and current remux permission. Exact audio targets remain
separate from maximum ceilings; FLAC retains an explicit 16- or 24-bit choice,
and Opus output uses its actual 48 kHz clock. Unknown timing or precision is not
converted into a guaranteed output fact. The [audio guide](audio-playback.md)
records source restrictions and deliberately rejected copy combinations.

The resulting output facts pass the same profile evaluator again. Input facts
that the output cannot promise are cleared: input time bases and codec tags, and
uncontrolled encoded profile/level/reference-frame or color facts, are not copied
into a fabricated result. Required unknown output conditions cause rejection.
MPEG-TS Annex B output does not retain MP4's AVC framing claim.

Initial numeric server defaults are 20 Mbps total, 1920x1080, and up to eight audio
channels. The planners preserve source channels when allowed; MP3 is limited to
two channels. HLS planning reserves ten percent of the bitrate budget for transport
overhead, which is a planning allowance rather than a packet-level rate limiter.
The caller can supply different bounded limits. Remux/audio/video permissions
default to false until explicitly supplied by the authorized caller.

Progressive lossy-audio budgets apply to encoded media, allowing an exact target
to equal the requested media ceiling. PCM uses its payload rate and FLAC a
conservative sample/frame bound. Container startup bytes remain within cache
quotas but are not amortized into a short clip's bitrate estimate. This policy
does not impose instantaneous HTTP bandwidth limits.

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
and no shell. The job directory starts empty. The original EVENT mode uses the
HLS muxer with `temp_file` publication and retained MPEG-TS segments. The HTTP VOD
path uses the segment muxer with explicit source-time cut points and stable global
segment numbers. Its publisher validates and atomically exposes finalized output;
private worker lists and temporary segments are never downloadable. Encoded video
forces keyframes at the configured cuts. Stream copy retains source keyframe
spacing; neither synthetic equal-length segments nor unsupported independent-
segment guarantees are invented. See the official [HLS muxer documentation](https://ffmpeg.org/ffmpeg-formats.html#hls)
and the [VOD timing and publication contract](hls-playback.md#full-timeline-and-seek).

VOD segments restart MPEG-TS transport continuity counters. Their initial packets
carry transport discontinuity indicators; the public VOD manifest also marks
every non-first segment with `EXT-X-DISCONTINUITY`. Strict continuous decoding
must pass across these declared boundaries, including output from different
producer windows. Single-segment decoding alone cannot verify this contract.

The measured playlist parser validates this closed output subset, exact segment
names, sequence, decimal durations, and completion state. URI rewriting preserves
those facts and accepts only bounded relative server URLs. It rejects external
resources, path traversal, temporary files, unsupported key/map features, and
tag injection. An unfinished event is never relabeled as a complete VOD.
The client-facing VOD manifest comes from the complete source timeline, separately
from the measured internal worker list; seeking can produce a requested range
without truncating that public manifest.

Audio-only HLS uses exact source duration and output-frame or measured packet
facts to merge an unproducible short final tail into the preceding segment before
publishing the timeline. Total duration and global numbering remain coherent,
and target duration is recomputed. This does not add packed AAC/MP3 HLS or claim
sample-exact gapless presentation.

Progressive mode sends media through nonseekable descriptor 4 into the private
append-only `stream.bin`, while descriptor 3 remains the source and stdout the
progress channel. Readiness requires actual media payload beyond container
metadata. `ProgressiveReader` waits through temporary EOF and returns successful
EOF only after durable completion and consumption of all final bytes. Failed or
cancelled production returns an error; after response headers, the HTTP adapter
preserves `http.ErrAbortHandler` so truncated output is aborted instead of ending
with a normal success terminator.

Probe cache version 3 introduced bounded packet/frame timing; version 4 extends
the current facts to supported Ogg Opus/Vorbis/modern FLAC after independent page,
header and topology checks plus complete packet/frame association. Opus pre-skip
and final discard and the defined Vorbis warm-up packet are reconciled with actual
decoded samples. Chained or changing streams, legacy Ogg FLAC mapping and
Matroska/WebM quantized-clock timing remain outside this exact subset. Current
metadata can retain original-file delivery when exact conversion is unproven.
Integer decoded sample counts survive seek/resampling plans rather than being
reconstructed from outward-rounded ticks. WAV writes an accurate RIFF
header before its PCM payload; only the defined sample-quantization deficit may
be padded. Excessive, materially short, misaligned or changed source/output fails,
and unsupported 32-bit RIFF sizes are rejected. Existing libraries, including
version 3 snapshots, need a normal rescan for version 4. The [audio guide](audio-playback.md#source-duration-and-audio-hls)
records proof bounds; additional exact-timing input profiles remain work.

Exact Ogg audio uses private `Plan.AudioSampleSeek` for non-copy conversion.
Progressive and HLS execution decode from the beginning, trim the Start/End window
in input sample coordinates, reset the decoded PTS origin, then resample and apply
the output sample limit. The normal HLS numbering and transport mapping remain
separate from this decoded-audio origin. The planner derives this policy only from
proven exact Ogg facts; request query fields cannot invent source measurements.
Non-Ogg plans retain their existing input seek behavior.

This path addresses measured Ogg cases where input `-ss` produced the correct
sample count at the wrong content position: a short Vorbis case was 128 samples
early, and an Ogg FLAC case was 11,264 samples early after timestamp seeking
failed. Content alignment against full decoding is required in addition to EOF,
sample-count and process-exit checks. Nonzero progressive Ogg copy is declined;
an ordinary codec request may choose permitted encoding, while explicit copy
cannot silently change its operation. Full-file compatible copy and original
ranges remain available.

The source prefix must actually be decoded. Existing startup, no-progress,
job-runtime and resource limits continue to apply; the policy does not provide a
high-performance random page-seek guarantee. Final execution and HTTP verification
are recorded separately from these diagnostic findings and the chosen contract.

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

Migration `0010` adds `encoding_jobs` to PostgreSQL; `0011` expands the bounded
plan JSON capacity to 128 KiB for immutable VOD source cut points. Creation
rechecks the enabled user, Emby authentication/session/device, and active playback owner under locks,
and checks expiry again at insertion. Records fix every scope dimension, source
stamp, plan, and creation time. Status progresses from queued to running and a
terminal result; completed/failed/cancelled/interrupted records cannot revive.
Cleanup may persist a terminal result after logout. Source files, tokens, output
paths, and stderr are not database fields.

Migration `0012` adds [client playback references](client-playback-references.md).
A client nonce is scoped to user, authentication session and device, then bound
to one canonical server play and one item/source. Encoding jobs and output
revisions continue using the canonical identity. Tombstones prevent a removed or
terminal playback reference from silently creating new work; binding or resolving
the reference does not update watched state or playback position.

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

All limits are bounded options. The [configuration reference](transcoding-configuration.md)
maps startup environment settings to manager limits and documents the private
systemd cache parent and test deployment policy. Periodic byte/free-space checks can overshoot
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
Identical scope/source/plan requests reuse work. For HLS, `WaitReady` requires a
published playlist and a complete segment; progressive readiness requires usable
media payload. `Open` requires the exact scope and a generated
filename; the HTTP adapter additionally revalidates token, library, playback policy,
and source before serving output, including cached and conditional requests.
`OpenProgressive` exposes a nonseekable reader with the same scoped authorization
boundary and shared reader/cache limits. The last progressive consumer cancels
unfinished production; completed output may remain cached. Retirement fences
deduplication before another consumer can register the same output key.
Cancellation, startup/no-progress/idle/runtime limits, cache
failure, and shutdown reclaim owned resources. Caller timeouts do not abandon
background cleanup. Conversion never updates watched or playback-position data.

## Playback integration and remaining work

The [reference study](../research/hls-reference.md) establishes full VOD manifests
and stable global segment numbers, with `EXT-X-START` seek hints. Its successful
segments also establish usable codec/packet evidence; its remux failures are not
a behavior to reproduce. The [HTTP adapter](hls-playback.md) uses complete source
timelines and bounded VOD producers with explicit cuts and global numbers.
Its full-duration manifests are distinct from the measured internal worker list.
Universal/legacy audio now selects original, progressive, or MPEG-TS HLS delivery
under the [audio contract](audio-playback.md) and [reference evidence](../research/audio-reference.md).
Progressive video, additional audio timing and profile
cases, packed-audio HLS, richer subtitle/codec profiles, hard worker isolation,
actual hardware execution, and real third-party-client release acceptance remain
required. The M4/M5/M6 scope stays active.
