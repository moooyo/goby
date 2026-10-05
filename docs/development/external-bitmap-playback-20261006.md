# External bitmap subtitle playback — October 6, 2026

## Source scope and status

The selected source increment enables server-side burn-in of indexed external
SUP/PGS and standard multilingual IDX/SUB/VobSub subtitles. It uses existing
playback negotiation and the existing player subtitle selector. The production
player UI does not need a separate bitmap track control, OCR, or browser bitmap
decoder. Database schema remains **61**; no migration or timeline regeneration
is required.

Focused source checks and all nine real backend/player acceptance phases passed
for feature commit `2bc39dbea06b0d0abe3ea1c903f7032ead91ea2f`. The later merge
with main has separate targeted integration verification, recorded below.
This document does not claim a new accepted Docker delivery. The
current `2026-10-06-main-images` catalog retains backend source
`2ba10e3613cac9da0a2e2c8ae7317bba229fdd56` and player source
`7aaaeed44526848737270089ab0227d2d410f864`. Existing release reports and build
receipts retain their exact original image identities.

## Playback contract

The scanner's existing external-bitmap catalog supplies one SUP track or the
language tracks from a uniquely paired IDX and SUB. Public stream indexes stay
stable and share the existing external/owned subtitle namespace. A language's
IDX identifier and its sidecar demux ordinal are distinct; playback uses the
validated ordinal rather than treating the public index as an FFmpeg stream
index.

Item/source and PlaybackInfo `MediaStreams` describe an external bitmap track
with:

```json
{
  "IsExternal": true,
  "IsTextSubtitleStream": false,
  "SupportsExternalStream": false,
  "DeliveryMethod": "Encode"
}
```

The current projection does not set `GobySubtitleTimelineOnly` for these tracks
and supplies no `DeliveryUrl` or private source path. The existing player can
choose the public stream index, switch language tracks, or turn subtitles off.
Selection negotiates video encoding; a user or client that cannot use the
required transcoding path receives the existing negotiation failure instead
of an apparently successful video without the selected captions. External
text and embedded bitmap tracks retain their existing delivery behavior.

The selected implementation supports bounded physical-file HLS and progressive
burn-in paths. The subtitle input retains its absolute source clock and decoder
history before the requested seek, including an active subtitle at that seek.
The selected subtitle offset remains part of the plan. The bitmap canvas is
mapped onto the primary video's canvas before the composed output is scaled,
so an SD VobSub canvas is not treated as a same-size patch in a larger video.
The focused media/transcode/server checks and the real candidate run below
verify timing, placement, track selection and clearing with actual decoded
pixels; the filter graph alone is not the acceptance evidence.

## Source authorization and private job material

`WithBitmapSubtitleFor` binds the current user, item, media-source ID, stable
stream index, catalog tag and demux ordinal to the authorized source. It borrows
the exact SUP descriptor or both IDX/SUB descriptors only for a synchronous
consumer. Root approval, user visibility/play permission, primary media
identity, component hashes and catalog/publication revisions are checked around
that read. Revocation, rescanning, changed pair assignment or component changes
reject the result. Borrowed descriptors do not escape into a durable plan or
an asynchronous consumer after the owner has retired its output.

Before FFmpeg starts, the runner writes a bounded private job snapshot using
only fixed names: `subtitle.sup`, or `subtitle.idx` plus `subtitle.sub`.
Files are created exclusively with mode `0600`. FFmpeg receives those names
and an explicit companion binding rather than a library pathname to explore.
SUP and SUB bytes are copied from the held descriptors; IDX is regenerated
from independently validated presentation facts into FFmpeg-compatible lexical
syntax. The canonical form preserves palette, canvas, language order, absolute
timestamps, delays and packet positions without emitting a source path.

Original sidecars remain read-only. The runner checks their identity around
materialization; preparation errors remove partial private assets, and the
transcode lifecycle owns subsequent cleanup. Playback snapshots are disposable
job data, unlike permanent generated GSTL timeline sidecars. No playback step
rewrites or deletes an original SUP, IDX or SUB.

## Independence from timeline generation

Burn-in works without GSTL material, enabled timeline generation or writable
media mounts. The existing [timeline contract](../api/subtitle-timelines.md)
continues to require opt-in or an explicit administrator request and stores
permanent source-side intervals. Only valid nonempty data creates a timeline
lane; its absence does not remove a playable subtitle choice. Playback does
not queue timeline generation or change existing manifests.

The schema-61 timeline-only implementation and its historical receipts remain
valid for their original sources. This increment adds playback capability to
that catalog without changing the database schema, remapping public indexes,
or weakening original-file and source-root controls.

## Verification status

All verification uses `test-env`; local Windows activity is limited to source
and documentation editing and reading retained evidence. Focused results for
the feature-source candidate are:

| Scope | Result |
| --- | --- |
| External bitmap library source/authorization cases | 14 top-level passes, 27 subtests |
| Media parsing and canonical IDX cases | 33 top-level passes, 61 subtests; one pre-existing opt-in availability fixture skipped |
| Transcode and private asset boundaries | 20 top-level passes, 47 subtests, no skips |
| Actual HTTP/source-change/server cases | Seven top-level passes, 24 subtests, no skips |
| Cancellation race checks | Two top-level passes, two subtests, no skips |
| Player type checking and browser contracts | Type check passed; 55/55 cases passed, comprising four new selection cases and 51 existing timeline cases, no skips |
| Existing embedded-bitmap HDR/seek regression | Three cases passed using the accepted software image's media tools |
| Real backend/independent-player flow | Nine phases passed on the immutable feature-source candidate, exit code 0 |

The first canonical IDX test expected the wrong canvas size. Two initial server
test issues counted old job rows across subtests and applied a pure-white
threshold to a glyph scaled close to one pixel. The checks were corrected to
use independent expected state and scale-appropriate pixel expectations while
keeping strict assertions. The host FFmpeg lacked `zscale`, so the unchanged
embedded HDR regression was run using the already delivered software image
`sha256:318a7d1294d6cdc036c0f2ee41e5165e6a643048144885980299a8c9c76646d0`.
No production guard was bypassed to satisfy these checks. Original failures
remain preserved. These counts are separate scopes, not a new combined total
with overlapping retries or historical timeline/release matrices.

## Real candidate playback

| Binding | Value |
| --- | --- |
| Feature source | `2bc39dbea06b0d0abe3ea1c903f7032ead91ea2f` |
| Candidate executable SHA-256 | `153936aac6a203b95917865be25f4ce0ad545d3b0e75bfe140cd8068990a7b71` |
| Candidate software image | `sha256:276ce680bee830905561db8d2b21820ddbca47c46760a47f5de49fa3c0be202c` |
| Unchanged standalone player image | `sha256:7140ce5c531a302b0aca595ee47ce98c52cd08ed73d5a00e5a27972d1ad94192` |
| Successful run | `live-1791231754984-73515dc3` |

The new 35-second H.264/AAC fixture has no embedded subtitles. Independent
authored PGS/VobSub bytes supply SUP and two IDX language tracks. The fixture's
glyph coordinates and timing windows, rather than the candidate decoder's
output, define the expected pixels. All nine phases passed:

1. A real scan exposed the three tracks. Explicitly selecting Subtitle Off in
   the detail UI produced original playback without caption pixels, including
   times when a subtitle would otherwise be active.
2. SUP pixels matched the upper-only, overlapping upper/lower, lower-only and
   clear states. Continuous 1x playback also observed the display transitions.
3. The IDX English track matched its positive delay and both display windows.
4. The other language track matched its negative delay and disjoint windows.
5. Switching Off, back to SUP and Off again left no previous caption residue.
6. Real player seeks entered active and empty intervals and returned to the
   active interval with the expected source clock and pixels.
7. Negotiated MP4 requests verified `SubtitleOffsetTicks` of positive and
   negative 5,000,000 ticks and a 4.5-second start inside an active IDX cue.
   This does not add a subtitle-offset UI control.
8. SUP, IDX and SUB components were separately changed. The same previously
   working negotiated URL changed from actual media output to
   `503 / video_unavailable` with no media bytes while the backend stayed ready.
   Restoring, rescanning and negotiating again restored the expected pixels.
9. Timeline availability stayed false, generation history did not increase,
   no source-side timeline directory appeared and no text-VTT request was
   invented. Media helpers returned to zero.

The run checked 27 browser native-video frames and 12 decoded API output frames.
The 130-by-14 authored glyph contains 492 foreground pixels. Thresholds fixed
before observing candidate output required at least 90 percent total foreground
coverage, 80 percent coverage for each nonempty character, and at most 3 percent
bright-pixel leakage in holes or inactive areas. The two IDX language labels
share a Latin glyph; disjoint display windows prove track selection, not Chinese
text recognition. No OCR is involved.

The first browser attempt incorrectly assumed subtitles defaulted to Off.
Existing Smart mode with a Chinese preference correctly selected the newly
playable matching track. The harness now selects Off explicitly; generation
still defaults off, and the product's Smart language-selection default was preserved. The
second attempt stopped at an ambiguous Play-button selector after the first
pixel checks passed. Selecting the real bottom-bar control fixed that harness
issue. Neither repair changed product code or relaxed the final pixel oracle.
The original failed runs and fixture-preparation corrections remain in the
evidence.

The private [real-service report](../../.artifacts/external-bitmap-playback-20261006/live/REVIEW.md)
and its raw run directory retain frames, screenshots, negotiated output and
the successful nine-phase result. These are isolated acceptance artifacts;
the candidate image is not a replacement for the official release catalog.

## Later main integration and closure

After the feature run, main advanced to
`d4678637097ce78235d883d5b1b62fa4ff516d4c`, with image scan/cache and playback
read-authorization work. Merge `d7276ee8528817bd94ba42bd90c47092e84a64f6`
has parents `2bc39dbea06b0d0abe3ea1c903f7032ead91ea2f` and that main revision.
Remote comparison confirmed the 23-file main delta and unchanged contents for
all 25 feature Go files. Targeted identity/library/server integration race
checks, including real server GET and changed-source/authorization cases,
passed 25 top-level cases and 57 subtests with no failures or skips. The result
is retained as `artifacts/integration-result.json` beneath the private evidence
root. This does not relabel the feature candidate image or rerun the nine
browser phases under the merge revision. Main's separate image performance
evidence retains its original conclusions and source boundary. Existing
uncommitted work in the primary checkout is outside this clean-source
verification scope.

The feature acceptance initially closed its backend, player and dedicated
PostgreSQL containers with exit code 0 and no OOM. Media helpers retired, and
task compiler scratch, the standalone embedded-regression test binary and
temporary test directories were reclaimed after worker exit. Shared Go caches,
source, media, database data, builds and raw evidence were retained. The
dedicated database was then reopened only for the later integration checks.
After those checks, the second closure receipt confirms no workers, all three
owned containers stopped with exit code 0 and no OOM, and reclaimed compiler
scratch and empty test-temporary directories. Shared caches, source, database
data, media, builds and evidence remain retained. This final state is recorded
separately in `artifacts/integration-closeout.json`; the earlier closure is not
used as a substitute. The final Chinese report is retained at
`.artifacts/external-bitmap-playback-20261006/REVIEW.md`.

## Boundaries

The supported source scope is standard PGS and the admitted VobSub presentation
subset. Non-default global alpha, enabled custom colors, forced-only IDX
presentation directives and other unsupported presentation syntax remain
explicitly rejected rather than producing guessed captions. Metadata indicating
a forced subtitle track does not imply support for a forced-only IDX directive.

This increment does not add browser bitmap decoding, OCR/WebVTT conversion,
original SUP/IDX/SUB download, paired-file deletion, unbounded streaming bitmap
sidecars, universal subtitle/container support or new GPU/driver acceptance.
Its source implementation and recorded isolated runtime evidence do not
replace the official Docker catalog or establish production deployment.

Implementation: [authorized source reader](../../internal/library/bitmap_subtitles_source.go),
[DTO projection](../../internal/server/subtitles_dto.go),
[server burn-in binding](../../internal/server/bitmap_subtitles_burn.go),
[private job assets](../../internal/transcode/external_bitmap_subtitle.go),
[canonical IDX](../../internal/media/bitmap_subtitle_external_canonical.go), and
[subtitle composition](../../internal/transcode/subtitle_plan.go).
