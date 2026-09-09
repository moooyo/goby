# HLS VOD playback

Goby connects its Linux conversion engine to authenticated Emby playback
negotiation and a complete MPEG-TS HLS URL graph. The administrator dashboard
remains an administration interface without a consumer player. Configuration is
documented in [transcoding configuration](transcoding-configuration.md).

## Negotiation and routes

`POST /Items/{Id}/PlaybackInfo` evaluates the original source and the requested
Streaming/HLS/TS TranscodingProfiles. A supported HLS result includes
`SupportsTranscoding=true`, `TranscodingUrl`, `TranscodingContainer=ts`, and
`TranscodingSubProtocol=hls`. Pure remux is a physical DirectStream operation but
may use the transcoding delivery URL, as captured in the reference. Explicit
copy restrictions and current user permissions still control which streams may
be encoded or copied. If transcoding delivery is disabled, an allowed remux may
instead provide a DirectStreamUrl when original-file direct streaming is unavailable.

Negotiation opens and validates the source but does not start an encoder. It
registers an immutable output revision and a full-duration playback-session
identity. Registry capacity cannot remove an otherwise valid original-file option.
A new bitrate/track/profile decision receives its own revision when its plan
changes; an old segment URL cannot mutate that plan.

Routes below use the usual optional `/emby` base and case-normalized literals:

| Method and route | Behavior |
| --- | --- |
| GET/HEAD `/Videos/{Id}/master.m3u8` | One HLS variant with truthful bandwidth/dimensions and an authorized media URL |
| GET/HEAD `/Videos/{Id}/main.m3u8` | Complete source VOD timeline, stable global numbers, ENDLIST, optional starting hint |
| GET/HEAD `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.ts` | Authorized finalized MPEG-TS output, including ranges and conditional requests |
| The same three routes under `/Audio/{Id}` | Audio-only HLS for Audio catalog items |
| DELETE `/Videos/ActiveEncodings` | Idempotent cleanup of the caller's matching DeviceId/PlaySessionId |
| POST `/Videos/ActiveEncodings/Delete` | Equivalent cleanup alias |

The existing original-file stream endpoints retain their own contract; HLS does
not turn progressive `Static=false` requests or universal-audio routes into
implemented conversion endpoints. Live playlists, adaptive multi-variant output,
fMP4, HLS subtitles, and richer delivery formats remain separate work.

## Full timeline and seek

The media playlist describes the complete source even when playback starts in
the middle. `StartTimeTicks` adds `EXT-X-START:TIME-OFFSET=...,PRECISE=YES`; it does
not truncate the timeline or renumber segments. Generated master/media URLs
include an explicit zero when resetting a prior nonzero hint.
An omitted start in a manual request without GobyHlsId also means zero, even
when the request reuses a revision originally opened at a later position.

Encoded video uses nominal segment intervals. Audio uses the exact timing and
frame-aware tail rules in [audio playback](audio-playback.md#source-duration-and-audio-hls);
an unproducible tiny final interval is merged without losing source duration.
Copied video uses actual
FFprobe packet seekpoints and can therefore have longer, irregular segments.
Format start time is the source origin; a real audio lead-in before the first
video keyframe remains part of segment zero. Packet key flags establish demuxer
seekpoints, not an unconditional closed-GOP guarantee. The manifests deliberately
omit an independent-segments claim and omit unproven RFC 6381 codec strings.

The worker receives explicit source-time cut points and global segment numbers.
A producer covers up to 256 consecutive segments. Nearby requests, including
out-of-order prefetch, share existing production. A distant seek starts at the
requested segment boundary; obsolete unfinished production is cancelled, and
earlier finalized cache entries may be reused while retained. Up to eight producer
bindings belong to one revision. A backward seek can regenerate expired output.

VOD mode uses the FFmpeg segment muxer with a consistent Goby transport timestamp
offset. Source positions, playlist positions and decoder PTS are distinct; the
reference's observed ten-second timestamps are not copied as a protocol constant.
The implementation accounts for source-format/video lead-in and drops negative
pre-seek audio packets. Actual tests compare the same global segment across
zero-start and later-start production, including MP4, MPEG-TS, copied/encoded
video, AAC and MP3. Audio can differ within the measured packet-boundary range;
sample-exact gapless conversion is not claimed.

Each segment uses a fresh MPEG-TS muxer, including within one producer, so its
transport continuity counters restart. The muxer marks the first packet of each
stream with the MPEG-TS discontinuity indicator, and the public playlist marks
every non-first segment with `EXT-X-DISCONTINUITY`. Both layers are intentional:
the transport indicator prevents a strict TS demuxer from treating a counter
restart as corrupt data, while the playlist declares the real boundary to HLS
clients as required by [RFC 8216 section 3](https://www.rfc-editor.org/rfc/rfc8216.html#section-3).
FFmpeg documents the transport option as
[`initial_discontinuity`](https://ffmpeg.org/ffmpeg-formats.html#mpegts).
These boundaries are stable regardless of request order or producer reuse. They
do not change global segment numbers, source timestamps, duration, or seek hints.
Clients may reset their parser/decoder at these boundaries; uninterrupted
transport counters or sample-exact gapless presentation are not claimed.

The segment muxer's private list can become visible before the current file is
closed. The publisher therefore waits for the next temporary segment, or process
exit for the final segment, before renaming a complete TS file. It validates TS
packet alignment and atomically publishes a normalized internal main playlist.
Private lists and temporary files can never be downloaded through the manager.

## Authorization and cache behavior

`GobyHlsId` is an opaque output-revision identifier, not a bearer credential. Its
scope fixes user, Emby authentication session, device, play session, item, source
snapshot, and plan. Generated child URLs carry the user's API token. This
deliberately differs from the reference's sampled capability-like PlaySessionId
segment URLs. Administrator cookies and another user's or device session's token
cannot substitute for the owner, including for an administrator account.

Every manifest/segment request revalidates the current authentication and playback
session, user conversion switches, library visibility, and anchored source
snapshot. Source identity includes inode, size, mtime and ctime, not a repeated
whole-media hash. Slow preparation is followed by another authorization/source
check before returning output. HEAD and cached 304 responses go through those
same checks.

Manifest responses use `application/vnd.apple.mpegurl` and `Cache-Control:
no-store`. Segments use `video/mp2t`, private/no-transform caching, an ETag, and
the standard HTTP range/conditional implementation on a leased finalized handle.
The request URL is never interpreted as a filesystem path.

Current `EnablePlaybackRemuxing`, `EnableAudioPlaybackTranscoding`, and
`EnableVideoPlaybackTranscoding` apply independently. Missing flags default to
allowed when the configured service is enabled; malformed/null flags deny the
corresponding operation. Disabled users and denied/malformed media-playback policy
deny all conversion. Administrators obey explicit conversion denials too.

Confirmed account/permission/source changes retire the affected revision and its
producers. A distinct source-change error preserves the existing 503 media-error
contract while allowing prompt cancellation. Transient database/filesystem errors
deny the current request without turning a temporary outage into permanent
revision loss. Background checks use independent bounded per-session work; an
exhausted cycle does not cancel unverified sessions belonging to other users.

Started/progress/valid Ping reports refresh revision presence, including long
segments or paused playback. They do not refresh the encoder's media-consumption
lease indefinitely. Stopped reports and logout retire the matching revisions.
Recent media access also permits a bounded prepared-session Ping renewal without
changing user progress or watched state. Progressive writes share this presence
mechanism; a long active response is not treated as an idle registry entry.
ActiveEncodings cleanup requires the caller's device ID and only affects that
authentication session; unknown or foreign play-session IDs are inert 204s.
Cleanup itself never fabricates a watched/progress update.

## Limits and operational behavior

Initial protocol-layer bounds are 32 simultaneous HLS requests, 128 registered
revisions total, 32 per user, 16 per authentication session, two keyframe probes,
and 16,384 timeline segments. Idle revisions expire after five minutes. Timelines
and immutable plans remain in memory; after restart clients must obtain a fresh
playback result. Durable user progress and authentication remain in PostgreSQL.

Manifest preparation has a two-minute ceiling. Segment preparation has a
45-second budget and segment writes a 30-second deadline. Shutdown/cancellation
interrupt active work and writes; cancelled server-side work cannot become an
empty successful segment. The underlying engine's concurrency, byte quotas,
reader leases and time limits are documented in [the engine guide](transcode-engine.md).
Periodic storage checks are not hard filesystem/cgroup ceilings.

Manually constructed master requests without GobyHlsId require an owned active
PlaySessionId and use a bounded query-to-plan adapter. Supported fields include
TS container selectors, H.264/AAC/MP3 codec choices, segment length, selected audio,
bitrate/channel/sample-rate/dimension/frame-rate limits, and stream-copy switches.
H.264/AAC are Goby's default HLS output choices for that manual form. Registered
revisions accept position hints; changing transform parameters requires a fresh
negotiation. Unsupported explicit burn-in, HDR/tone mapping, timestamp copying,
and manual positive subtitle selection are rejected rather than silently ignored.

The service does not silently fall back from requested hardware to unbudgeted
software. Hardware configuration and implementation flags are separate from the
administrator API's `Hardware.Verified=false`. Actual device-specific execution,
additional client/profile cases and complete client acceptance remain required.
