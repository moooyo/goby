# Continuous Live Publication

Status: the selected phase-two media, formal AMD and v3 browser scopes passed.
The original TS/fMP4 and bitmap failures remain preserved alongside their repairs.
Owned PostgreSQL/worker/documentation closeout is complete. The
[phase 2 execution record](amd-media-phase2-20260919.md) owns frozen sources,
individual results and final closeout boundary. Acceptance remains limited to
those recorded profiles; no local execution or broader client support is implied.

## Source and ownership

`Manager.EnsureStreamInputs` consumes a primary pipe and, for bitmap burn-in, an
independent bitmap pipe from the same authorized source generation. Admission,
reuse, cancellation, and shutdown release every transferred descriptor. One FFmpeg
process continues consuming that generation throughout its lifetime.

Live publication requires `Options.LivePublish`. Each selected internal text track
also requires `Options.LiveCaption`. `TouchStream` renews consumer activity only
for an active stream with an exact ownership scope. Authorized media requests and
client heartbeats drive that lease. Stream jobs retain idle, startup, progress,
space, and concurrency limits; finite conversion runtime limits do not terminate
an otherwise active stream periodically.

## Closed media publication

Each rendition uses the segment muxer's streaming CSV journal on a dedicated
bounded pipe. The journal is emitted after media/trailer flushing. The publisher
checks complete regular-file contents and stable source identity, joins exactly
one matching sequence from every fixed rendition, and calls `LivePublish`
synchronously. Rendition start and end times must agree within one millisecond.
Successful publication is followed by scratch-file retirement.

The CSV journal's first start value is a placeholder. An independent first-packet
clock supplies the actual first start and corrects the first duration. Later CSV
boundaries come from actual reference packets. `LiveSegment.StartTicks` and each
`LiveRendition.PreMuxClockTicks` use the common pre-container clock. All primary,
bitmap, and subtitle inputs share `LiveSourceClockBiasTicks`: one second of encoder
headroom minus the known probed origin. The receiver measures the published
container's actual first packet to establish its mux clock delta.

Live segment cuts allow one microsecond of timestamp comparison tolerance.
FFmpeg converts the first reference PTS to `AV_TIME_BASE` before calculating
subsequent cut times; a rational clock such as 24 fps can otherwise put an exact
keyframe just before the rounded boundary and skip an entire GOP. This tolerance
does not change packet timestamps, measured durations, or the store's fixed target
duration limit.

MPEG-TS files require complete packet framing. Each new segment muxer declares its
continuity-counter reset with `initial_discontinuity`, preserving the continuous
presentation timestamps while allowing strict decoding across joined segments.
Fragmented MP4 is first produced as
a complete self-contained file, checked for complete box boundaries and usable
media, and split into independent initialization and media files using
movie-fragment-relative addressing. Initialization bytes must remain stable in one
producer. Every fMP4 callback borrows an initialization file for verification; the
time-shift store retains it once per generation. The caller separately enforces
codec restart evidence before advertising independently decodable copied media.
Child MP4 muxers explicitly use `movie_timescale=1000` and
`avoid_negative_ts=disabled`: segment muxers do not propagate the outer timestamp
setting, and independently selected defaults would change initialization bytes or
rebase each segment to zero. Full initialization hashes remain mandatory.

## Caption completeness

Internal text tracks use private companion containers holding copied reference
and original subtitle packets from the same primary demuxer. A closed companion
is extracted completely with preserved timestamps before its callback advances
the corresponding subtitle watermark. Long cues retain their original end times,
while the completed reference interval determines coverage. Audio is the preferred
reference; a video-only companion uses its decode clock as a conservative marker.
Companion cut comparisons use the same one-microsecond rounding tolerance as
media segments; only actual copied reference packets advance the watermark.

After FFmpeg exits successfully and all closed-companion callbacks succeed, each
internal track receives `LiveCaptionSegment{Complete:true}` with its completed
sequence count. Cancellation and failed producers never emit that completion.
Source shutdown must cancel the job before intentionally closing its source pipes.

## Bounds and acceptance

The engine limits each media segment to 64 MiB, total scratch to at most 256 MiB
and the configured job quota, and directory entries to 1,024. Per-rendition queues
hold one record and kernel journal pipes provide backpressure. A separate budget
watch checks growing files, including a source GOP that fails to close. Publication
callbacks receive a deadline and borrow files only until they return.

Live timeline values use nonnegative signed 64-bit ticks. Finite-media duration
caps do not apply to cumulative live clocks, including FFmpeg's internal progress
records. Stream progress converts integer microseconds to ticks only when the
product fits in a signed 64-bit integer; negative encoder-start values retain the
existing clamp to zero. Finite progress retains its 30-day limit and 10-second
tolerance. This internal clock handling does not extend any external protocol's
30-day admission boundary. The segment muxer's signed 32-bit sequence range
remains an explicit integer bound.

Regression sources cover ordered journals, first-clock correction, complete
fMP4 splitting, file retirement, ladder misalignment rejection, scoped consumer
renewal, and long-running integer clocks. Real-media coverage generates continuous
TS input and checks single/dual-rendition TS and fMP4 publication, each fragment's
actual mux clock, full decoded frame counts, and scratch cleanup. Phase-two
completion records must supply the remote execution results.
