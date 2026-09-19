# Advanced media source contract

Status on September 19, 2026: phase 1 completed its selected-profile verification,
builds and documentation closeout. Its
[phase 1 execution record](amd-media-phase1-20260919.md) retains that closed source
and acceptance boundary. [Phase 2](amd-media-phase2-20260919.md) has completed its
selected v3 CPU/GPU/browser verification, final builds and owned PostgreSQL/
worker/documentation closeout within its recorded boundaries.
The preceding media/library/management wave
was integrated into `main` and accepted within its recorded software profile.
Provider-specific acceptance is **deferred by the user**. The
[feature-wave record](feature-wave-20260919.md) and its verification record
define this acceptance; older playback reports do not accept these additions.

The subsequent AMD media phase has a focused
[HEVC VAAPI parameter-set compatibility record](hevc-vaapi-parameter-sets-20260919.md).
That record documents the verified output correction and does not extend
the historical acceptance scope below.

## Current phase 1 additions

| Area | Implemented source contract | Runtime and compatibility boundary |
| --- | --- | --- |
| Encoded video | H.264 8-bit Baseline/Main/High, HEVC Main 8-bit/Main 10, AV1 Main 8/10-bit; software and selected VAAPI encoding | HEVC supports MPEG-TS and MP4; AV1 requires MP4, including HLS fMP4. Software uses libx265/libaom-av1. Codec, profile and precision must satisfy the client. |
| HEVC MP4 framing | Copied and VAAPI-encoded HEVC use `hev1`; software x265 uses `hvc1` | Preserve authoritative in-band parameter sets. Re-evaluate output constraints after an encoder fallback. See the [parameter-set record](hevc-vaapi-parameter-sets-20260919.md). |
| Hardware admission | Validate actual output for the exact codec/profile/depth/geometry/rate tuple, with bounded cached probes | An unsupported or padded output can fall back to an available authorized software encoder without changing the requested format. This does not mark the entire GPU verified. See [admission](hardware-encoding-admission.md). |
| Dolby Vision | Probe actual RPU coverage, CRC and native mapping syntax; supported conversion applies metadata through the private strict libplacebo filter | SDR and HDR10 conversion are separate from authoring Dolby Vision output. Profile 7 zero-residual MEL and nonzero-residual FEL are distinguished by NLQ, not one flag. Full profile acceptance requires its own fixture and output evidence. |
| AMD filters | Vulkan/libplacebo HDR processing and deinterlacing, with VAAPI decode/encode where selected | The sampled AMD VAAPI VPP path is unusable. Ordinary hardware decoding/scaling uses download, CPU scale and optional upload. Text shaping is CPU work; GPU composition does not mean an all-GPU pipeline. See [AMD processing](amd-video-processing.md). |
| Progressive subtitles | MP4 encoding can burn selected text and bitmap subtitles, including signed offsets and nonzero seek | Current source/attachment authorization and stream identity apply. Copy output cannot burn subtitles. Actual text and bitmap acceptance are recorded separately. |
| Copy seeking | H.264/HEVC/AV1 restart points, source/tool-bound packet and decoded-frame proof, supported AAC-LC copy, exact or explicitly declared alignment | `CopyTimestamps=true` retains the source clock; arbitrary frame-accurate packet copying is not promised. See [copy seeking](copy-seek-compatibility.md) and [client timestamps](media-client-timestamps.md). |

The probe cache version is now 7. Optional raw bit-depth facts remain unchanged;
effective HEVC/AV1 precision may be derived from an unambiguous pixel format.
See [bit-depth projection](video-bit-depth.md). Existing cached probe data must
be refreshed under the new version before it supplies new processing facts.

The following table retains the preceding accepted software increment's
scope. Its narrower restrictions do not describe the new phase 1 source above,
and its acceptance does not automatically extend to that source.

## Preceding accepted software output paths

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
equivalent. Phase 2 source adds a fixed set of at most eight text subtitle
renditions and segment windows derived from actual media intervals. Crossing cues
retain their complete timestamps in each overlapping segment. Track selection,
off and signed offsets are views over the same audio/video plan; a measured
mux-clock translation remains independent of the viewer offset. Only plan-owned
names are served, with token and current source/policy checks. These additions
retain the recorded phase 2 profile and final-closeout boundary.
See [master generation](../../internal/server/hls_generated.go#L23),
[subtitle artifacts](../../internal/server/hls_generated.go#L277), and
[clock alignment](../../internal/server/hls_subtitle_clock.go#L20).

The `/Videos/{Id}/subtitles.m3u8` and `/Videos/{Id}/live_subtitles.m3u8` adapters
are now registered for a bound revision/presentation and a supported explicit
track view. Their source implementation does not establish complete client
acceptance or an arbitrary multi-track manifest contract. See the
[phase 2 record](amd-media-phase2-20260919.md) and
[rendition adapter](../../internal/server/hls_subtitle_renditions.go).
Universal/legacy audio's `TranscodingProtocol=hls` selector retains
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

Phase 2 source retains complete produced TS/fMP4 output for bounded replay,
pause/resume, in-window seeking and live-edge return. It adds internal and
explicitly declared external text subtitles; bitmap changes require a new burned
presentation and cannot rewrite retained history. Default retention is at most
600 seconds subject to 512 MiB per-window and 2 GiB global budgets, including
advertised grace. Dynamic video prefers encoding; remux-only output must prove
each segment's restart boundary. Unknown upstream duration remains unknown,
restart discards history, and unavailable earlier content is not manufactured.
Selected strict media, formal AMD and v3 browser scopes now pass; original
failures remain preserved and PostgreSQL/worker/documentation closeout is complete.
See [continuous publication](live-publication.md),
[dynamic source ownership](../../internal/dynamicsource/README.md), and
[phase 2 results](amd-media-phase2-20260919.md). Channels, tuners, EPG, scheduled
recording and provider-specific catch-up remain separate deferred work.

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
OCI delivery work remains deferred. The new AMD execution results belong only
to the phase 1 record, rather than this preceding wave. Full Emby parity,
arbitrary codecs/containers and every third-party client remain outside the
supported claim.
