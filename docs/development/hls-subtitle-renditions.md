# HLS subtitle renditions and immutable presentation views

Status: selected phase 2 HTTP/media and v3 native-video/HLS.js browser scopes passed;
owned PostgreSQL/worker/documentation closeout is complete within the recorded scope.
The [phase 2 record](amd-media-phase2-20260919.md) retains exact scope and original failures.
This document describes the finite-source HTTP path. Dynamic-source ownership,
time-shift retention, and reconnect epochs have separate phase 2 contracts.

## Bound tracks and producer identity

An HLS plan binds up to eight client-compatible text subtitle tracks, ordered by
their source stream indexes. Each track records its codec and, for an external
sidecar, its indexed content fingerprint. Bitmap subtitles remain a burn-in
choice and do not become WebVTT renditions. The native burn-in plan and the HLS
text-rendition set are mutually exclusive.

The complete track set belongs to the immutable source revision. The selected
track, subtitle-off state, and caption offset do not belong to the A/V encoder
plan. Changing these presentation choices reuses the same A/V revision and
producer. A different bound track set or changed external fingerprint requires
a new revision.

Every generated manifest child explicitly carries `SubtitleStreamIndex` and
`SubtitleOffsetTicks`. Index `-1` means off; offsets are bounded to plus or minus
24 hours. Existing URLs retain their original values after another request
selects a different track or offset. There is no mutable shared default that
can silently retarget an earlier URL.

The master advertises every bound track in one `SUBTITLES` group with distinct,
escaped names and available language metadata. Only the explicitly selected
track has `DEFAULT=YES,AUTOSELECT=YES`. An off view retains the available tracks
with both attributes set to `NO`, allowing a client to select a track later.
Tracks outside the bound, authorized set cannot be selected through a query.

## Actual media windows and clocks

Canonical resources are:

- `hls2/{RevisionId}/subtitles-{Slot}.m3u8`
- `hls2/{RevisionId}/subtitles-{Slot}-segment-{Sequence}.vtt`, with the sequence
  represented by at least six decimal digits using the generated canonical form.

Slots identify the fixed track set; they are not source stream indexes. The
subtitle playlist mirrors the first actual media rendition's `EXTINF` values,
media sequence, discontinuity markers, and completion state. It does not predict
segment boundaries from a nominal segment duration or advertise `ENDLIST` before
the corresponding media producer completes.

The first pre-mux reference packet and its corresponding published packet
establish the producer epoch's transport-clock difference. This source anchor
and observed segment boundaries remain associated with that producer. Later
rolling windows reuse observed overlapping or contiguous sequence anchors;
they do not reopen segment zero for every request or guess an unobserved gap.
A new producer cannot inherit the old producer's epoch. The retained boundary
map is bounded by the media playlist's existing segment limit.

Each WebVTT segment includes every cue overlapping its half-open source window
after applying the requested caption delay. A cue spanning multiple segments
appears in each with its complete original start and end, rather than clipped
timestamps. A media window without cues produces a valid empty WebVTT segment.
Each segment receives the measured transport timestamp map; an imported source
VTT timestamp map cannot override the generated transport clock.

The selected-track aliases `subtitles.m3u8` and `subtitles.vtt` inside an existing
`hls2` revision remain available. The latter is a bounded full-source document
for older consumers; new masters advertise the segmented slot resources.

## Authorization and lifetime

Every lookup, including `HEAD`, conditional requests and completed artifacts,
revalidates the principal, playback reference, source stamp, conversion policy,
current track identities, and every bound external subtitle fingerprint. A
matching WebVTT ETag is considered only after this authorization. Changed or
deleted source bytes retire the affected revision. Explicit subtitle deletion
immediately retires every revision containing that track, including when a
different track was selected in the requesting client's view.

Confirmed sidecar identity, metadata, or content-hash changes carry
`ErrSourceChanged` as well as the existing `ErrUnavailable` read classification.
This lets immutable conversions retire immediately without misclassifying an
ordinary transient storage or database read failure as a confirmed mutation.

`HEAD` checks the bound resource without starting a media producer, extracting
an embedded subtitle, or measuring a new mux clock. It does not report whether
subtitle bytes are ready. Subtitle GETs share the existing media-policy and
request limits. Pending or failed subtitle preparation does not retire the A/V
producer or count as media delivery.

## Background preparation for embedded text

An authorized subtitle body GET starts or joins a whole-track background read
if there is no completed extraction. `PlaybackInfo`, the master, and the media
and subtitle playlists do not wait for this read. HLS VTT and standalone
`Stream.*` share complete extraction bytes when the source version, absolute
track index, format and executable identity match. ASS is retained in its native
representation before rendering. Rendering still applies the current request's
window, timestamp mode and offsets, not those of the first caller.

Standalone requests and HLS requests carrying `Prefer: respond-async` wait up
to 500 ms for extraction. HLS requests without that preference can wait up to
20 seconds for the shared result, preserving the previous wait for players
that cannot poll explicitly. This does not change A/V or playlist waits.
Native clients without retries may need a new request after a longer wait.
An expired wait returns `503 subtitle_preparing`; admission to a full queue
returns `429 subtitle_extraction_busy`. Both include
`Retry-After: 2`, `X-Goby-Subtitle-Preparing: 1` and `Cache-Control: no-store`;
neither contains a subtitle representation or an ETag. Retry the same URL with
current authority. A standalone cold embedded `HEAD` may return
`503 subtitle_preparing` but does not enqueue a read. Sidecar delivery and
normal ready responses, including ranges and conditional GETs, keep their
existing behavior. A terminal extraction failure remains an unmarked error; a
genuinely empty HLS window returns valid WebVTT only after the complete track is
known.

One source reader and eight queued requests are admitted per server, with a
three-minute budget per preparation including queue time. Successful bytes have
a 30-minute lifetime, a limit of 128 retained entries and a 64 MiB total byte
budget. Per-track output remains capped at 8 MiB. Failed or incomplete bytes are
never published. The extractor runs in the background source and process lanes,
with authorization and source checks before reading and again before publication.
It can finish after a requesting connection or HLS play stops; a stopped or
revoked URL cannot use the result. Source publication retires matching work, and
server shutdown cancels and joins the worker. The cache is in memory only and
starts empty after restart. Configured executable aliases whose identity cannot
witness their invocation dependencies retain uncached, bounded extraction.
This does not change font attachments, embedded burn-in, dynamic-source
subtitles, or video encoding quotas.

## Standard playlist entry points

`GET` and `HEAD` on `/Videos/{Id}/subtitles.m3u8` and
`/Videos/{Id}/live_subtitles.m3u8` can reuse an authorized finite-source Goby HLS
revision. Supply its `GobyHlsId`, playback/source/device scope, and an explicit
`SubtitleStreamIndex`; `SubtitleOffsetTicks` has the same view semantics as the
canonical resources. For finite sources the two names return the same actual
media window. The route name alone does not make a finite source live.

If `SubtitleSegmentLength` is supplied, it must match the registered nominal
media segment length; actual subtitle boundaries still come from media output.
The SDK snapshot declares `ManifestSubtitles` only as an opaque string and does
not define its serialization. These standard adapters reject a nonempty value
rather than inventing a stream-index, JSON, or URL interpretation. The existing
manual master request's `ManifestSubtitles=vtt/webvtt` remains Goby's explicit
output-format hint; it is not a claim about the opaque standard-route encoding.
See the checked-in [DynamicHlsService contract](../api/services/DynamicHlsService.md).

Read-only inspection of the retained official client at
`/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/dashboard-ui/modules/browserdeviceprofile.js`
found `TranscodingProfiles[].ManifestSubtitles` set to `vtt` when supported and
`MaxManifestSubtitles=10` for its native Tizen profile. This establishes the
profile's format hint and maximum-count semantics, not the standard route's
opaque string serialization. The server caps a client's maximum at its own
eight-track bound instead of rejecting a client capable of more tracks. The
inspected file's SHA-256 is
`2ae6855eb523420c16257975233e0006c0bdf7added644ab7a4449fca33c50c5`.
No request was sent to the retained reference server.

## Acceptance coverage

- `TestHLSSubtitleViewsPreserveProducerAndEveryPublishedURL` checks independent
  view defaults, escaped labels, complete scoped URLs, and unchanged plan state.
- `TestHLSSubtitleWindowsRetainMeasuredRollingEpoch` checks unequal measured
  durations, overlapping rolling windows, changed-boundary rejection, and gaps.
- `TestHTTPHLSMultipleSubtitleWindowsViewsAndCurrentSourceAuthority` exercises
  actual media-aligned VTT segments, repeated complete cross-segment cues, empty
  segments, switching/off/offset without a new producer, standard entry points,
  `HEAD`, conditional authority, changed sidecars, and deletion retirement.
- `TestHTTPHLSSubtitleRenditionsMatchActualMediaPresentationClock` retains real
  decoded TS/fMP4 and adaptive-rendition clock comparisons across selected tracks
  and offsets.

The [phase 2 record](amd-media-phase2-20260919.md) identifies the executed remote
source/runtime scopes, original failures, repairs and completed closeout. Test
presence alone does not extend that acceptance to other clients or profiles.
