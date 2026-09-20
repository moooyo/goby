# Descriptor-bound media analysis

The Go extraction entry points are `media.AnalysisExtractor.ExtractAudio`,
`ExtractVisual`, `ExtractIntro`, and `ExtractPreviews`. They take an authorized
open `*os.File` and its indexed `media.Info`, never an HTTP pathname. The caller
owns current authorization, source revision and content identity, job admission,
cache publication and expiration. No GET handler should call an extractor.

`Availability` checks the independently installed helper protocol and the fixed
FFmpeg 9.0.1 version, required filters and output codecs. It reports separate
audio/video dependency availability. Preview planning additionally requires the
`wrapped_avframe` encoder for its bounded null-output planning pass. It does not install a binary or assume the
existing FFmpeg has Chromaprint support. The helper build and licensing recipe
is in [README.md](README.md). Configure its installed path as `FingerprintPath`,
the existing fixed FFmpeg as `FFmpegPath`, and its paired FFprobe as
`FFprobePath`. Production jobs must copy the admission snapshot's
`ExpectedFFmpegSHA256`, `ExpectedFFprobeSHA256`, and
`ExpectedFingerprintSHA256` into the extractor. Empty expected hashes are
available for independent fixtures; they do not replace job admission.
Tool SHA-256 and file identities
are checked, and execution uses the held executable descriptor. Changing a
tool invalidates its extraction identity; replacing a pathname cannot select
different executable bytes between hashing and execution.
`ExtractIntro` freezes all three observed digests before either serial phase,
and both audio and visual extraction recheck the same admitted FFmpeg bytes.
The pure `IntroAlgorithmProfile(availability, intervalTicks)` derives the exact
cache/matcher profile from availability facts without executing a tool. The
returned `IntroFeatures.ToolFacts` records the three admitted digests.

## Time and content evidence

All public times are 100 ns ticks relative to `Info.FormatStartTicks`. Unknown
format origins are rejected. FFmpeg consumes `/proc/self/fd/3` with `-copyts`,
no seek rebasing and a self-contained format/protocol allowlist. Original
selected packet PTS must exist before FFmpeg timestamp repair. The bounded
`-debug_ts` parser uses the fixed 9.0.1 original demuxer record, not a later
repaired timestamp or frame number multiplied by an assumed rate.

Audio is trimmed to the first `min(DurationTicks, 600 s)` and decoded to native
11,025 Hz mono signed 16-bit little-endian PCM with one decoder/filter thread.
`asettb` explicitly sets sample units; `ashowinfo` supplies each output frame's
actual PTS, sample count, format and checksum. Frames must be continuous at that
time base. Every PCM extent must match its frame's zero-seed AVAdler checksum;
extra, missing, corrupt or discontinuous samples decline the extraction.
Original packet PTS auditing rejects missing original packet times. FFmpeg's
documented audio `av_rescale_delta` path
maps existing packet clocks to the decoder sample time base; the extractor does
not invent an independent clock or fill a gap. This evidence does not provide
decoder-before-repair frame identity or claim to exclude every internal
best-effort timestamp choice.

The separate helper receives only these bounded PCM bytes. Its actual API
sample rate, item step and delay determine the raw fingerprint inventory.
Fingerprint item `i` has PCM support `[i*step, i*step+step+delay)`. The emitted
non-overlapping audio bin is the support's left anchor, `[i*step,(i+1)*step)`,
mapped through the proven PCM PTS origin. It does not claim that the bin is the
whole acoustic support. `AudioBoundaryUncertaintyTicks` is the full support
length rounded upward to ticks. The matcher must conservatively account for
this guard and visual timing before publishing a boundary. The profile includes
algorithm/normalization versions, timing policy and the admitted tool byte identities;
it excludes per-source stream indexes, names and timestamp origins.

Visual extraction uses the same decoded frames as its PTS records. Each frame
is matched to an original packet PTS candidate using rational time bases. This
candidate-set match is not a packet-position-to-frame identity proof and cannot
exclude all decoder-internal DTS/best-effort choices. Source
and selected-frame inventories, dimensions and order must match the streamed
raw raster. The fixed `gray32-dhash9x8-rms-v1` profile scales to 32x32 grayscale,
forms 9x8 cell means and 64 adjacent comparisons, and reports grayscale RMS
contrast as standard deviation divided by 255 and multiplied by 1000. Default
sampling is 500 ms over the first 600 seconds; a bounded refinement window may
use intervals down to 100 ms. Returned ticks are actual selected-frame PTS,
including for VFR sources. Missing, duplicate, backward or unbound timestamps
are rejected instead of interpolated.
The private geometry probe admits actual canonical 0/90/180/270-degree matrices
and positive SAR, then applies explicit rotation and square-pixel output
normalization. Preview dimensions follow the exact displayed aspect ratio,
including anamorphic inputs. See [GEOMETRY.md](GEOMETRY.md) for the independent
matrix/SAR proof, admitted transforms and malformed-input boundaries.

Previews use the separate `source-pts-display-preceding-hold-jpeg-v3` sampling
policy. The complete profile also binds `;geometry=orthogonal-display-sar-v2`
so a changed display policy cannot reuse an earlier derivative profile.
They cover the admitted full duration with uniform nominal slots and retain
each selected frame's original actual ticks separately. At a nominal slot, the
selected frame is the latest source frame whose PTS is at or before that slot.
Its presentation interval extends up to the next source PTS. A slot before the
first video PTS displays the first frame, retaining that frame's positive actual
ticks. After verified video EOF, the last frame is held through the admitted
container duration, including an audio-only tail. This is an explicit preview
display policy; it does not extend the video's timestamps or declare the video
stream longer. Several slots can therefore have the same actual ticks. No slot
is removed and no actual timestamp is replaced with its nominal timestamp.

The preview decoder performs two bounded passes through the same authorized
descriptor and held FFmpeg binary. The first audits original packet and decoded
frame PTS while retaining only the previous frame and the bounded slot plan.
After actual EOF establishes the final hold, the second selects only the unique
planned source frame ordinals and verifies their timestamps against that plan.
The complete source metadata inventory is hashed incrementally in each pass and
must match, including frames after the final selected ordinal. The fixed
[`select` filter](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavfilter/f_select.c)
uses the consumed input-frame ordinal and forwards the selected `AVFrame`
without rewriting its PTS. The source
[`showinfo` filter](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavfilter/vf_showinfo.c)
reports that real frame's ordinal, PTS and geometry before selection. This is a
source ordinal/timestamp and fixed-filter selection proof, not a claim that a
metadata hash is a decoded-image content digest. A single JPEG can serve several
adjacent slots. Both passes share one extraction
deadline and execution slot, with cumulative source-frame and diagnostic work
budgets. The intro visual sampler retains its separate original policy.

Allowed widths are 240, 320 and 400. The default interval is 10 seconds. FFmpeg
streams one selected raw RGB frame at a time; Go's JPEG encoder uses the admitted `PreviewAnalysisOptions.Quality`
(40..95, with zero selecting 80) and a bounded writer. The summary records the
actual quality and both FFmpeg/FFprobe digests. The callback borrows one read-only
JPEG with its nominal/actual ticks, dimensions and SHA-256; it must persist the
bytes before returning. Mutation invalidates the extraction. JPEGs are not
accumulated into a whole-file RAM buffer. Callback outputs are provisional:
the caller must discard its owned staging directory unless the whole method
succeeds and its own source/authority CAS still holds. The callback must return
promptly and cooperate with its caller's cancellation.

## Resource and cancellation boundaries

`AnalysisLimits` exposes lower configurable budgets, with hard maxima. Defaults
are one global background extraction slot; 10 minutes wall time (maximum two
hours); 13,230,000 PCM bytes; 65,536 PCM frames; 2,400 visual samples; 8,192
preview frames; 2,000,000 decoded source frames; 16 Mi source pixels; 1 Mi output
pixels per frame; 2 MiB per JPEG; 512 MiB JPEG output; 4 GiB streamed raw bytes;
and 512 MiB cumulative operation diagnostics, with a separate 256 MiB hard cap
for each child process. Source duration is at most 12 hours. Larger plans are
rejected, not silently truncated. Individual diagnostic lines and pending PTS
inventories are bounded separately. Long media may exceed the diagnostic budget
because source packet/frame evidence is retained as streaming records; that
case returns a resource error and never publishes partial previews. Diagnostics
are parsed incrementally with bounded lines and queues; the cumulative byte cap
does not authorize retaining the entire log in memory.

The two preview passes share source-frame and diagnostic counters. As a static
representative estimate for the pinned FFmpeg 9.0.1 log format, ordinary H.264
1080p24 records at PTS 86400/time 3600 total 974 bytes per source frame across
demuxer, timestamp-fixup, FFmpeg, decoder, source-showinfo and color records,
even with addresses shortened to `0x1`. Two hours at 24 fps decoded twice is
345,600 source-frame visits, or about 321.02 MiB before other records and longer
addresses. This estimate motivates the explicit 512 MiB operation cap; it is
neither a runtime measurement nor a universal lower bound. Each pass remains
limited to 256 MiB. Ordinary two-hour media still require actual workload
verification; a budget error must remain visible rather than publish a partial
timeline or silently increase the sampling interval.

Linux's trusted `/usr/bin/prlimit` limits each process to 2 GiB address space,
64 descriptors and zero regular-file output bytes. Media output uses pipes.
FFmpeg's allocation, source-pixel, decoder and filter-thread limits also apply.
The shared runner cancels on stdout/stderr/parser/consumer budget failure,
signals the complete process group, waits for retirement, joins pipe readers
and calls `Wait` on the real child. A context deadline is not treated as proof
that a child terminated. Source descriptor identity, extent, modification time
and change time are checked after work. Borrowed descriptor offsets are retained.

## Verification status

This source delivery has not been built or executed. Unit sources cover strict
protocol parsing, PCM checksums and real time mapping, malformed/missing PTS,
resource limits, VFR frame binding, callback cancellation, descriptor ownership,
and actual process-group retirement. Opt-in preview mechanics fixtures include
VFR frames at 0/3/7 seconds, video ending at 10 seconds with audio ending at
10.1 seconds, and a video stream starting after audio. A separate bounded
FFprobe decoded-frame inventory establishes the original PTS, while distinct
frame colors and repeated JPEG hashes establish the held image. These assertions
do not substitute invented uniform source timestamps. Opt-in generated-media cases require
`GOBY_FFMPEG`, `GOBY_FFPROBE`, and, for audio, `GOBY_INTRO_FINGERPRINT`. The tests
are mechanics evidence only. They cannot replace labeled independent real
episodes, calibration/holdout data, negative categories or boundary review.

Primary source references: the pinned FFmpeg
[audio timing path](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/fftools/ffmpeg_dec.c#L248),
[PCM frame checksums](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavfilter/af_ashowinfo.c#L260),
[trim EOF handling](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavfilter/trim.c#L143),
and the Chromaprint [helper protocol and support derivation](PROTOCOL.md).
