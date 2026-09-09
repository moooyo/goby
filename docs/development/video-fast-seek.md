# Verified fast video seeking

M4f adds an optional way to skip expensive prefix video decoding for eligible
progressive MP4 conversions. It preserves the existing linear conversion as the
fallback and correctness reference. This document describes the running contract
and operational behavior; it is not an M4f acceptance or deployment report.

The bounded FFmpeg experiments, rejected alternatives, and restart evidence are
in [the research report](../research/video-fast-seek/README.md). The preceding
[M4e video and user-management acceptance](verification-m4e-video-and-users.md)
and [M5b metadata acceptance](verification-m5b-metadata.md) describe their own
verified snapshots. M5b explicitly excluded the unfinished fast-seek work; those
reports do not certify M4f or real GPU execution.

## Scope and observable behavior

The optimization applies to a nonzero presentation start when converting a
supported H.264 source through the software video decoder into progressive
H.264 MP4. A source must have a known format-clock origin and a usable private
restart index. Current source facts, output constraints, user permissions, and
resource admission still determine whether the conversion itself is allowed.

Original-file delivery, audio-only conversion, HLS, and copied video retain their
existing paths. A nonzero copied-video request is still subject to the existing
unsupported-copy-seek rule; an index does not authorize an implicit switch from
copying to encoding. A request at the beginning needs no fast restart.

Hardware decoding remains available through the configured hardware policy but
uses the linear path. A software-decoder proof is not reused as authorization
for VAAPI, QSV, or CUDA decoding. This fallback does not disable hardware support
or silently substitute a software decoder. Software decoding can still supply
a candidate when the selected encoder is hardware-based. Hardware encoding and
decoding are separate policy choices; available configuration is not evidence
that a device has been exercised. Real GPU execution remains unverified.

There is no new public HTTP route, seek-proof endpoint, or proof query parameter.
Clients continue to use the existing PlaybackInfo and video stream URLs,
including `StartTimeTicks` and concrete output settings described in
[progressive video playback](progressive-video-playback.md). Indexes, decoded
hashes, source identity, format-clock authority, and the private candidate are
not published in `MediaSources` or serialized into a stream URL.

Changing the start on a supported encoded URL rebuilds the plan under current
authorization and source facts. A URL value cannot attest to a restart point.
PlaybackInfo and HEAD prepare/check the existing playback contract without
starting index analysis, a runtime proof, or an encoder. A GET that actually
starts a conversion lets the runner decide whether the private candidate can
be used.

## Index production during library scans

Probe cache version **6** carries optional `VideoSeekIndexes` alongside the
ordinary private media facts. This is a probe-cache change, not a new public
metadata DTO. A demuxer keyframe flag alone is not a restart certificate.

`Server.New` enables optional seek analysis on the normal scanner's Prober and
supplies the configured FFmpeg path. This does not depend on
`GOBY_TRANSCODING_ENABLED`: disabling playback conversion does not prevent a
normal library scan from preparing eligible private indexes. An explicitly
constructed Prober still needs `AnalyzeVideoSeek=true` and a nonempty
`FFmpegPath`; an empty path skips analysis. There is no separate administrator
HTTP switch or dedicated seek-index configuration flag.

The existing `GOBY_FFMPEG` and `GOBY_FFPROBE` settings select the server's tool
paths. Configuration is read at startup; restart the service after changing
those settings. Supplying a tool path enables no client-controlled arguments
and does not alter the configured media roots.

The application builds eligible indexes as part of normal library analysis,
using the configured FFmpeg and FFprobe executables and a held local source
descriptor. Index construction belongs to scanning, not to playback startup.
Ordinary media probing remains usable without optional restart evidence.

For a supported stream, the index pairs actual compressed H.264 IDR evidence
with the corresponding decoded image hash at the exact native rational
timestamp. The complete scan also checks parameter-set stability and rejects
unsupported packet side data and NAL scope. Only matching IDR pictures are
retained as candidates; other decoded key pictures do not acquire restart
authority.

Each index is bound to the original stream index, source clock and dimensions,
the held file's identity and mutation stamps, and the identified FFmpeg tool.
The index is stored privately with the source facts. Its retained entries and
serialized size are bounded independently of how many packets the scan visits.

Unsupported codecs or evidence, unavailable optional tooling, exceeded analysis
budgets, and optional decoder errors produce no usable index. They do not turn
an otherwise accepted source into an unplayable item. This does not waive the
ordinary probe, source-access, or conversion-support requirements. Cancellation
of the scan and mutation of the held source during analysis remain errors; they
are not reported as a successfully completed optional cache miss.

After upgrading a library whose cached probe facts predate version 6, run the
normal administrator library scan. Existing cache-version and source-snapshot
guards still apply while those old facts await refresh. New or changed sources
are analyzed through the normal scanner. A successful scan need not produce an
index for every video, and an absent index alone is not an incomplete scan.

An unchanged source with a current version-6 probe snapshot retains the existing
scanner cache behavior. A missing index or an FFmpeg change alone does not force
that snapshot to be probed again. In particular, installing a different FFmpeg
can invalidate an old index at runtime, but an ordinary rescan does not rebuild
it solely for that reason. Sources remain eligible for linear playback under
the ordinary checks.

This is a known operational limit: there is currently no force-reprobe endpoint
or dedicated index-rebuild task. A naturally changed source is analyzed through
the normal scanner; explicit rebuilding of unchanged sources remains future
work. Do not treat changing media timestamps, deleting libraries, or modifying
private catalog state as the normal way to request fresh indexes.

## Candidate selection and execution

Preparation selects a bounded window of preceding indexed points using only
stored facts and the requested start. This is a pure selection step: it starts
no process, reads no media, and does not reserve a second worker. The result is
a canonical private `VideoSeekCandidate` inside the comparable conversion plan.
Plan validation binds its stream, format origin, duration, and requested start
to that plan. Argument previews remain linear because a candidate is not proof.

Every new `Run` with a candidate performs a bounded preflight against its
borrowed source before creating progressive output. It rechecks source and tool
identity, tests a bounded set of indexed timestamp proposals, and compares the
actual retained IDR packet, decoded picture, parameter sets, and native clock
position against the stored evidence. The accepted picture must not follow the
requested presentation position. A cached success boolean cannot authorize a
later run.

Runtime proof uses the actual conversion's configured input thread count, rather
than assuming the index scanner's thread setting. Hardware-decoded plans skip
this software proof and retain their original linear input. The index's decoded
evidence still has to match the actual preflight; a different thread setting is
not permission to ignore a mismatch.

The successful **input seek argument** is retained separately from the observed
landing picture. A demuxer may land earlier than the proposed timestamp. That
earlier picture proves what happened for the tested argument; replacing the
argument with its timestamp would require another proof and can land earlier
again.

On success, input zero opens the held file with the verified input seek and
the existing format-clock offset. The output still applies the requested
presentation seek, selected video stream, supported filters, and concrete
encoding settings. A failed or exhausted optional proof leaves the existing
linear arguments in use. Unsupported optimization is not a new playback error.

When audio is selected, input one independently opens the same inherited file
and reads audio linearly, with video discarded. Both inputs use the same source
clock and explicit local format/protocol restrictions. The selected audio index
is mapped from input one; its copy or encode policy remains unchanged. A
video-only source does not introduce an unused second input.

The separate audio input preserves decoder history and the existing priming and
timestamp behavior. Seeking audio together with video was rejected by the
research because equal duration and sample count did not preserve the actual
waveform. No fixed audio warmup interval is substituted for the linear path.
Opening `/proc/self/fd/3` independently also preserves the caller's borrowed
descriptor offset and avoids resolving a replaced source pathname.

## Resources and performance expectations

The optional analysis domain is deliberately narrower than ordinary media
probing or playback. Sources outside these limits remain eligible for their
existing playback paths; they do not receive fast-seek evidence.

| Eligibility or execution bound | Current value |
| --- | --- |
| Source display dimensions | Each dimension is positive and at most 32,768; their product is at most 8,847,360 pixels (`4096 * 2160`). |
| Source pixel representation | `yuv420p` with known bit depth 8, or `yuv420p10le` with known bit depth 10. Unknown or mismatched facts are unsupported. |
| Source duration | Positive and at most 25,920,000,000,000 ticks, or 30 days. |
| Analysis input decoder | One thread. |
| Runtime proof input decoder | The actual conversion's configured thread count, from 1 through 64. |
| Analysis wall time | One shared two-minute budget across the source's candidate streams. |
| Runtime proof wall time | One shared five-second budget, including tool identification and at most eight timestamp attempts. |
| FFmpeg allocation option | `-max_alloc 67108864`, limiting a single allocation block to 64 MiB. |
| FFmpeg input decoder pixel option | `-max_pixels 8847360`. |

The source eligibility check happens before launching optional decoding, and
the FFmpeg options also constrain the child. These are **not a whole-process
memory or RSS cap**. In FFmpeg 9, the pixel check applies to aligned display
dimensions; a large H.264 coded canvas with a small crop and some earlier table
allocations are not fully bounded by that display-pixel check. Multiple allowed
allocation blocks can also exceed the single-block limit in aggregate. The
options reduce supported resource demand but do not establish complete memory
isolation against arbitrary media. Worker-wide isolation is a separate concern.

Parsing independently limits a line to 8 KiB, streamed scan output to 256 MiB,
records to 8,000,000, and pending joins to 4,096 records. The source's retained
indexes share an 8,192-entry and 2 MiB serialized budget. A private candidate
retains at most 64 points and fits within 64 KiB. Runtime proof output is limited
to 64 KiB. Child diagnostics are bounded and process-group cancellation retires
the parser processes. Index scans do not retain packet payloads or decoded
pictures in the Go process; hashes are the retained evidence.

The optimization runs within existing scan and conversion lifecycles. It does
not introduce a second encoding job, worker pool, output cache, or source-path
authority. A conversion proof happens before the same job's output becomes
ready, so it consumes part of the existing startup window. Manager admission,
thread policy, output byte quotas, free-space checks, reader leases, and HTTP
deadlines continue to apply. See [conversion configuration](transcoding-configuration.md)
and [the engine contract](transcode-engine.md) for those controls.

Fast video decoding does not imply constant-time seeking. In particular, the
independent audio input can still read the container prefix linearly and become
I/O-bound. Low-resolution sources may gain little because audio, hashing,
process startup, or output work dominates. The research's high-resolution CPU
control illustrates reduced video prefix work; it is not a universal latency
promise or a production startup deadline.

## Invalidation, cancellation, and HTTP lifetime

| Condition | Behavior |
| --- | --- |
| No eligible index or no preceding supported candidate | Use linear conversion if ordinary conversion is supported and authorized. |
| Stale cached index identity, incompatible tool identity, or unproven restart | Do not authorize fast input seek; use the linear path after ordinary current-source checks. |
| Optional proof reaches its own budget | Fall back to linear conversion while the parent job remains active. |
| Parent request/job is canceled | Cancel the active proof or conversion and retire its process group; do not convert cancellation into successful fallback. |
| Held source changes during analysis, proof, or conversion | Fail the affected operation through the existing source-mutation handling. |
| Current account, token, library, or playback permission is revoked | Apply the existing playback authorization and cancellation rules, independent of index availability. |
| The last consumer departs, playback is stopped, or the service closes | Use the shared reader/job cleanup and shutdown paths. A private candidate does not keep work alive. |

An index does not extend a source grant, authentication session, or playback
session. Reading media and preparing a seek do not fabricate playback reports
or edit watched state. Completed outputs remain subject to existing scoped
cache ownership and source checks; a new job must prove its candidate again.

Progressive HTTP behavior is unchanged: Range is ignored for the generated
representation, HEAD starts no encoder, and no estimated Content-Length or
ETag is introduced. Temporary producer EOF is not successful completion. A
failure or cancellation after headers must abort the response or reset the
stream through the existing transport path, rather than appear as a successful
truncated body. Original-file Range, HEAD, and validators retain their own
contract.

## Operational interpretation

Use the normal administrator scan and playback operations. The lack of a public
fast-seek flag means clients cannot force an unproven restart, and a successful
200 response does not tell an operator whether the runner used a fast or linear
input. Inspection of private test recordings or controlled diagnostics must
remain separate from the public playback contract.

When a seek remains slow, distinguish an unsupported or stale index, proof
fallback, linear audio input, and ordinary encoder/resource contention. Do not
increase client `StartTimeTicks` margins or invent proof fields to compensate.
For troubleshooting, retain request/job identifiers and bounded diagnostics;
avoid exporting authentication tokens, full stream URLs, private source paths,
or raw source metadata.

Evidence for new changes must cover the actual runner and emitted media, not
only a candidate, command preview, first decoded picture, or HTTP status. The
research documents controls comparing complete video frame sequences and audio
PCM, including positive assertions that the fast path really ran. Broad source,
client, GPU, and final application acceptance remain separate verification
work; this document does not claim that work has completed.
