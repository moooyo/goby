# Advanced media source contract

Status on September 19, 2026: implemented, accepted within the recorded software
profile, and integrated into `main` by the media/library/management wave.
Provider-specific acceptance is **deferred by the user**. The
[feature-wave record](feature-wave-20260919.md) and its verification record
define this acceptance; older playback reports do not accept these additions.

## Implemented output paths

| Area | Source behavior | Important limits and source anchors |
| --- | --- | --- |
| Text subtitles | External SRT/WebVTT/ASS/SSA and extraction of indexed ASS/SSA, SubRip/text, mov_text and WebVTT streams; supported SRT, WebVTT and ASS/SSA representations | Absolute catalog stream identities, current source/access checks, bounded extraction; ASS-to-text conversion loses styling. [Extractor](../../internal/media/subtitle_extract.go#L20), [delivery](../../internal/server/subtitles.go#L100) |
| Fonts and burn-in | Indexed TrueType/OpenType attachments and software H.264 HLS burn-in of supported text or PGS/DVD/DVB/XSUB bitmap subtitles | Font content is checked; at most 16 selected font attachments. Burn-in is an HLS encoding path, not progressive MP4 or stream copy; bitmap-to-text/OCR is absent. [Selection](../../internal/playback/hls_options.go#L35), [closed subtitle plan](../../internal/transcode/subtitle_plan.go#L41) |
| HLS packaging | Existing source-timeline MPEG-TS plus generated MPEG-TS, fMP4 initialization/media segments, and packed AAC/MP3 audio | fMP4 rejects MP3 audio. Packed audio is finite-source, audio-only, without an adaptive ladder. [Plan](../../internal/transcode/hls_plan.go#L30), [HTTP routes](../../internal/server/hls_http.go#L21) |
| Adaptive HLS | Two to four independently encoded H.264 renditions, with shared input clock and aligned forced GOP cuts | Each rendition must satisfy the client profile; at least two distinct permitted sizes/bitrates are required. This is not a copied-video ladder. [Planner](../../internal/playback/hls_options.go#L107), [producer](../../internal/transcode/hls_command.go#L25) |
| Video filters | Software deinterlacing and PQ/HLG-to-BT.709 SDR tone mapping, composed with scaling and HLS subtitle burning | Known BT.2020 primaries, matrix and range are required for HDR; contradictory/Dolby Vision declarations are rejected. The filter graph requires software decode/encode; temporal deinterlacing retains linear history. [Source facts](../../internal/playback/progressive_video_filters.go#L12), [filter contract](../../internal/transcode/video_filters.go#L24) |
| Video-copy seek | Nonzero-start progressive H.264 MP4 packet copying at an exact indexed IDR boundary without decode pre-roll | Catalog evidence, source/tool identity and runtime packet-copy proof must agree; audio is absent or encoded AAC, never copied audio. Arbitrary keyframe-near seeking is rejected or falls back to permitted encoding. [Admission](../../internal/transcode/copy_seek.go#L13), [candidate proof](../../internal/media/video_copy_seek.go#L104) |

Conversion still uses the shared job manager, current ownership checks, bounded
queue/cache/reader resources, and cancellation lifecycle. A constructible
PlaybackInfo response is not a reservation or proof of successful client
playback. Hardware interface enumeration does not establish GPU execution.

## Subtitle and HLS HTTP boundary

Both `/emby/Videos/{Id}/{MediaSourceId}` and the `/emby/Items/...` alias expose
`Subtitles/{Index}/Stream.{Format}`, the start-position path variant, and
`Attachments/{Index}/Stream`. GET registrations also provide authenticated
HEAD behavior. Subtitle options include start/end ticks, `CopyTimestamps`, and
bounded signed `SubtitleOffsetTicks`; an explicit query start overrides the
path start. This does not assign a guessed unit to upstream `SubtitleOffset`.

Generated HLS uses `/emby/Videos/{Id}/hls2/{PlaylistId}/{Artifact}` and the Audio
equivalent. The master lists actual media renditions and, when selected, one
WebVTT subtitle rendition. Its `subtitles.m3u8` references a bounded whole-source
`subtitles.vtt`; a measured mux-clock translation aligns text with output media.
Only plan-owned names are served, with token and current source/policy checks.
See [master generation](../../internal/server/hls_generated.go#L23),
[subtitle artifacts](../../internal/server/hls_generated.go#L277), and
[clock alignment](../../internal/server/hls_subtitle_clock.go#L20).

Standalone upstream `/Videos/{Id}/subtitles.m3u8` and
`/Videos/{Id}/live_subtitles.m3u8` are not registered. No rolling subtitle
window, arbitrary multi-track subtitle manifest, or complete client matrix is
claimed. Universal/legacy audio's `TranscodingProtocol=hls` selector retains
its MPEG-TS-only contract; advanced audio containers are selected through the
supported Audio PlaybackInfo/HLS profile paths. See the
[legacy selector](../../internal/server/audio_request.go#L443).

External subtitle deletion is available through
`DELETE /emby/Videos/{Id}/Subtitles/{Index}` and its POST `/Delete` alias, with
the Items equivalents. It requires current subtitle-management authority,
uses recoverable file/catalog deletion, and retires affected HLS output.
Embedded subtitles cannot be removed from their media container. See
[deletion handler](../../internal/server/subtitles_management.go#L14).

## Generic dynamic sources

`GOBY_DYNAMIC_SOURCES_FILE` supplies bounded server-side HTTP(S) source
definitions linked to catalog items. Clients cannot submit arbitrary input
URLs. `/emby/LiveStreams/Open`, `/MediaInfo`, and `/Close` operate owned source
leases; `/emby/LiveStreams/{LiveStreamId}/hls/{Artifact}` serves their output.
Opening/probing, bounded reconnects, idle expiry, cancellation and credential
revalidation belong to that lifecycle. HTTP redirects are not followed.
Source anchors: [configuration](../../internal/config/dynamic_sources.go#L31),
[routes](../../internal/server/live_streams_http.go#L19),
[lease manager](../../internal/dynamicsource/manager.go#L249), and
[HTTP connector](../../internal/dynamicsource/http.go#L77).

Dynamic conversion is nonseekable TS/fMP4 HLS. Unknown duration stays unknown;
nonzero start positions, selected subtitles and packed-audio output are
rejected. It does not provide a DVR/rewind window or seamless reconnect
guarantee. These are generic media sources, independent of the deferred Live
TV channel, tuner, EPG and recording subsystem. See
[dynamic planner](../../internal/playback/dynamic_conversion.go#L8).

## Provider and acceptance boundary

Remote subtitle search/download and native metadata/image provider operations
are integrated with server-side credentials and managed enablement. Search
results used by the Emby subtitle adapter are source/session-bound; ordinary
subtitle permissions apply and application keys are rejected. TMDB,
MusicBrainz and OpenSubtitles provider-specific acceptance remains deferred by
the user. See [routes](../../internal/server/providers.go#L22) and
[subtitle authority](../../internal/server/providers_subtitles.go#L86).

The [implemented API inventory](../api/implemented.md) separately records the
new collection, policy, account, settings, task, download and deletion APIs.
Consolidated remote media/browser/regression acceptance and both Linux builds
completed in the [recorded scope](feature-wave-verification-20260919.md).
OCI delivery work remains deferred. Actual GPU execution, full Emby parity,
arbitrary codecs/containers and every third-party client remain outside the
supported claim.
