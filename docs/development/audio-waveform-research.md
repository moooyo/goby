# Per-track audio waveform research

Status: the original 2026-10-04 source/documentation study is complete; the user
subsequently approved implementation with persistent source-side files. The
original investigation ran no builds, probes, or encoding jobs. Performance
statements in that historical research remain estimates unless superseded by
the separate implementation evidence below.

## Implemented increment and verification

Goby now generates one source-aligned peak/RMS envelope per original audio stream
and serves it through the [audio waveform API](../api/audio-waveforms.md).
Files live in `backdrops/goby-waveforms/<source-filename-sha256>/` beside the
source. They are independent of the background-video namespace and disposable
analysis cache. Existing files persist until explicit Force replacement; stale
source axes are not served. Generation is opt-in per library and uses the task
system. Opening the media-information page reads existing data without starting
analysis.

The player renders actual envelopes and treats missing-data intervals as gaps.
Focused browser/HTTP/domain/task checks and actual FFmpeg cases passed, including
multiple tracks, delayed audio, opposing channel polarity, AAC, AC3, EAC3, FLAC,
Opus, PCM, and cancellation. Schema-58 PostgreSQL backup/restore covers the
queue/receipt state while retaining source-side files. The separate live Docker
journey passed all eight phases with `failures=[]`, covering generation/reuse,
real player rendering, stale-axis rejection, failure retention, and explicit
replacement. Its result is recorded in
[player acceptance](../../web/player/ACCEPTANCE.md).

The following sections retain the original research-time observations and
proposals. They do not supersede the implemented API or its approved persistent
storage policy.

## Research-time product purpose and state

The player handoff's media-information page contains a whole-title timeline:
V1 video thumbnails, one A lane per audio track, and one S lane per subtitle
track. The selected audio lane receives the accent color. The handoff itself
generates decorative audio shapes through `wavePath(seed, 180)` and a
pseudorandom generator in `goby-core.js`; these are not captured audio data.

The production player deliberately keeps real audio metadata and an unavailable
waveform state in `web/player/src/pages/DetailPage.tsx`. Existing thumbnail and
subtitle lanes consume actual BIF frames and subtitle cue intervals. They do
not supply audio amplitudes.

A real waveform would show the distribution of silence and audio activity
across the title and complete that visual design. It is not required to select
an audio track, play the movie, synchronize subtitles, or resume playback. The
current design has no waveform editing or detailed zoom workflow. It does not
call for a spectrogram, sound recognition, or certified loudness measurements.

The existing Emby comparison found no waveform-envelope endpoint in the pinned
core contract. This is a Goby analysis extension; it is not an omitted field
that can be filled by returning existing stream metadata. The finding does not
exclude third-party Emby plugins.

Repository reuse points:

- `internal/media/probe.go` and `media.go` preserve original stream indexes,
  codec, channel count/layout, sample rate, and timing facts. Ordinary FFprobe
  metadata does not contain a waveform. Exact `AudioTiming` is not universally
  available for every video-contained audio track, so extraction must establish
  its own presentation alignment.
- `internal/media/analysis_audio.go` contains bounded descriptor-based PCM and
  timestamp handling. Its legacy intro window and mono extraction are not an
  all-track, whole-title waveform implementation.
- Current intro execution in `analysis_intro_skipper.go` extracts a selected
  audio track's Chromaprint fingerprint over the configured intro horizon.
  Fingerprints cannot be converted into amplitude envelopes, and their
  selected-track policy does not satisfy one lane per audio track.
- `internal/tasks` already supplies durable scheduling, progress, cancellation,
  recovery, and publication fences. Background previews demonstrate a durable
  item queue with request receipts. Intro, BIF, and background work already
  share a bounded analysis execution slot.
- `internal/analysiscache` and `internal/server/analysis_runtime.go` manage
  disposable, private BIF/feature derivatives. Source-side background MP4s use
  a separate persistent lifecycle. Choosing between these policies for waveform
  data remains a product decision.

## Research recommendation for the first version

Generate a compact amplitude envelope on the server, persist only the envelope
and its provenance, and render it with Canvas or SVG in the existing audio
lane. Use one lane per original audio stream, aggregating decoded channels into
that lane. Keep sample-rate audio, separate channel lanes, frequency analysis,
and zooming out of the first version.

Decode every presentation sample of each selected audio track. Do not sparsely
seek to occasional samples: a short impulse or a quiet interval can otherwise
be missed. Selecting audio prevents video decoding, but does not establish how
many bytes the demuxer reads from an interleaved container.

For a time bucket with decoded normalized PCM values `x[c,n]`:

```text
peak = max(abs(x[c,n]))
rms = sqrt(sum(x[c,n]^2) / number_of_channel_samples)
```

Use channel maxima and channel energy, not an arithmetic mono downmix. Opposite
polarity left/right content could cancel in a mono downmix while both channels
contain substantial sound. The first version should use all decoded channels
with explicit equal weighting. A track lane represents decoded channel energy;
it is not a measurement of subjective loudness or an Atmos object renderer.
Optional separate channel views can be a future profile and API extension.

Derive bucket positions from decoded timestamps on the item's presentation
timeline. Respect track delay, codec priming/padding, late starts, and early
ends. Represent intervals with no presentation data separately from known
digital silence. Unexpected discontinuities or unproved timing must yield an
explicit unavailable/partial result, not silently shifted audio. Persist a
complete ready result only after validating its coverage and output bounds.

Keep the data's linear amplitude interpretation stable and perform visual
compression, such as a square-root display scale, in the renderer. Do not
normalize every track independently, which would make a quiet commentary track
look as strong as a louder mix. Quantization must define clipping for decoded
values outside the nominal unit interval.

## FFmpeg implementation options

The official [FFmpeg stream-selection documentation](https://ffmpeg.org/ffmpeg.html#Stream-selection)
supports explicit mapping of source stream indexes. Use those original indexes,
not positions in a filtered audio list. Reuse Goby's held source descriptors,
admitted executable identity, protocol restrictions, cancellation/join behavior,
and finite output limits. No external service, machine-learning model, or GPU is
required for this design.

Two implementations deserve a small bounded prototype:

1. Decode to PCM and consume it incrementally in a Go accumulator, with a
   validated timestamp/frame channel. This gives explicit bucket and alignment
   semantics and constant working memory, but PCM crosses the process boundary.
2. Aggregate inside FFmpeg with `astats` and export only selected metadata.
   The official [astats documentation](https://ffmpeg.org/ffmpeg-filters.html#astats-1)
   describes per-channel and overall statistics, metadata export, frame-based
   resets, peak values, and RMS. The
   [asetnsamples filter](https://ffmpeg.org/ffmpeg-filters.html#asetnsamples)
   can control frame sample counts. This reduces pipe traffic but requires a
   strictly bounded metadata/timestamp parser. Fixed FFmpeg frames alone do not
   prove alignment to an item-relative bucket grid or handle discontinuities.

The first approach is the simpler correctness reference. Keep full audio sample
rate for the initial profile: downsampling after decode does not avoid decoding
the compressed stream and may change brief peaks. If lower sample rates later
prove useful, they need a distinct profile and error measurements.

FFmpeg's [showwavespic filter](https://ffmpeg.org/ffmpeg-filters.html#showwavespic)
can produce a single waveform picture and has peak/average and channel-split
options. A bitmap is a quick visual experiment, but it fixes resolution and
colors and cannot supply the numeric data, coverage, and multiresolution
semantics proposed here. It is not the preferred production artifact.

Reuse of the existing FFmpeg executable is likely; the configured build must
still be tested for required decoders, filters, and output formats. Explicitly
test AAC, AC-3, E-AC-3, DTS, TrueHD, FLAC, Opus, and PCM coverage instead of
claiming support from a codec name alone. A waveform from an Atmos-bearing
stream describes the decoder's channel output; it does not prove Atmos object
rendering.

## Data shape, resolution, and size estimates

The present nonzooming timeline needs screen-sized data, not a point for every
audio sample. A proposed profile stores four resolutions of 512, 1024, 2048,
and 4096 equal-duration buckets over the item. At two hours, the finest bucket
spans about 1.76 seconds; a 1024-bucket view spans about 7.03 seconds per bucket.
Shorter titles naturally have finer time resolution. These figures are a
product tradeoff, not a technical limit.

Accumulate finest-bucket peak, energy sum, and valid sample count. Construct
coarser RMS values from summed energy/count, never by averaging RMS directly;
coarser peaks take the maximum. Quantize only after each level is complete.
Missing-data coverage must also aggregate correctly.

Proposed wire contract, not an implemented endpoint:

```text
GET /emby/Items/{itemId}/AudioWaveforms
  descriptor: Available, State, MediaSourceId, SourceVersion, ProfileVersion,
              DurationTicks, Streams[{StreamIndex, Channels, ChannelLayout,
              Coverage, Levels[{BucketCount, Url, ETag}]}]

GET /emby/Items/{itemId}/AudioWaveforms/{streamIndex}?buckets=1024&tag=...
  versioned binary payload: header + interleaved uint16 peak/rms + validity map
```

The final API must define byte order, normalization, endpoint status codes,
coverage, timeline origin, schema/version limits, and exact ETag scope. Existing
credentials and item ACLs apply to both descriptor and payload. Reads never
start analysis or create playback history. Avoid embedding envelope arrays into
ordinary item-list responses. The client requests the smallest suitable level,
memoizes by source/profile/generation, and aborts on navigation.

Uncompressed payload estimates using exactly two unsigned 16-bit values per
bucket, excluding headers and protocol overhead:

| Case | Amplitude payload |
| --- | ---: |
| One track, 1024-bucket response | 4 KiB |
| One track, 4096-bucket level | 16 KiB |
| One track, all four levels | 30 KiB |
| Six tracks, all four levels | 180 KiB |
| 1000 titles, six tracks each, all levels | About 176 MiB |

A one-bit validity map adds 960 bytes per track for all four levels. Together
with small headers, a six-track title would be roughly 186-190 KiB under these
assumptions. Compression ratio is content-dependent and is not assumed here.
Base64 adds approximately one third to binary size; plain numeric JSON is larger
and should not be used to estimate persisted storage.

If detailed zoom later requires 100 ms buckets, a two-hour track needs 72,000
buckets: about 281 KiB for the finest peak/RMS level, or less than 563 KiB for a
complete power-of-two pyramid before validity/header overhead. That is a
different profile; the current handoff does not require it.

## Resource costs and client/server choice

The small result does not imply a cheap extraction. Full-title envelopes require
full-duration audio decoding. CPU demand depends on codec, channel count, sample
rate, track count, and decoder speed. Original-file bytes read depend on container
interleaving/indexing, demuxer seeking, filesystem caching, and local/NAS storage.
Neither audio bitrate alone nor an assumption that every file byte is read is a
valid IO estimate. No measured runtime or IO figure is available from this
research.

Streaming aggregation avoids retaining PCM. For scale, two hours of 48 kHz
float32 decoded audio is about 2.57 GiB for stereo or 7.72 GiB for six channels,
before decoder and container buffers. Those are raw-buffer size calculations,
not persisted waveform sizes. A bounded streaming worker needs only decode
buffers and accumulators.

Prefer one demux pass for a bounded batch of audio tracks where it is reliable;
separate per-track processes can reread the source. The tradeoff is decoder
concurrency and implementation complexity. Cap active tracks/channels and
working memory; process remaining tracks in additional batches rather than
silently omitting them. Measure both strategies before choosing the final
batch size.

Client-side analysis has poor economics for this whole-title view. Every device
would acquire and decode audio, including tracks the user never plays, and
browser container/codec support varies. The
[Web Audio specification](https://www.w3.org/TR/webaudio/#AudioBuffer)
defines `AudioBuffer` as memory-resident float32 PCM and recommends streaming for
long material. Its [AnalyserNode contract](https://www.w3.org/TR/webaudio/#AnalyserNode)
describes a recent sample window, not a precomputed whole-film timeline. An
on-device live analyzer therefore does not fill the design before playback.

A server artifact is generated once, shared across clients, and only a few KiB
per visible lane are sent to the browser. Initial generation remains background
work. No result should be fabricated while pending or unavailable.

## Task and persistence proposal

Add a dedicated waveform task to the generic task center, with bounded item/
track work, progress, request receipts, cancellation, deadlines, and restart
recovery. Share the existing analysis resource group initially so a library scan
does not create unrestricted decode work. An optional automatic library policy
should default off; administrators can run explicit selected work. Task identity
and database schema changes require a separate implementation decision.

Bind results to source identity, original stream index and stream signature,
timing basis, extraction profile, and format version. A changed source or stream
mapping must not cause old data to be shown as current. A failed replacement
must not publish a partial result. Existing auth/publication fences and source
retirement semantics remain mandatory.

The user's persistent source-side rule for generated background MP4s does not
automatically decide the waveform lifecycle. The options are:

| Policy | Benefits | Costs and decisions |
| --- | --- | --- |
| Private derivative cache, similar to BIF | Works with read-only media; reuses cache leases/budgets | Eviction can cause repeated full-audio decoding; regeneration policy must be explicit |
| Persistent application data directory | Works with read-only media; survives normal cache cleanup | Requires durable volume, backup/restore policy, and explicit orphan retention/removal rules |
| Persistent source-side metadata | Travels with source and can survive catalog rebuilds | Requires source write access and namespacing; must define associations and stale-data handling |

For this small reusable artifact, persistent application storage with explicit
maintenance is a reasonable starting recommendation, but remains unapproved.
Do not place it in the existing background MP4 directory or extend that
directory's retention contract by implication. Stale-result visibility and
physical-file deletion are separate decisions: an old file may be retained
without being presented for a replaced source.

## Original research acceptance proposal

This list preserves the broader research proposal. The completed selected
implementation and its actual receipts are defined by the API and acceptance
links above; additional storage/performance matrices below are not outstanding
commitments created by this historical list.

1. Known silence, impulses, stepped amplitudes, and phase-inverted stereo prove
   peak/RMS aggregation, channel handling, and multiresolution consistency.
2. Delayed tracks, priming/padding, negative timestamps, early ends, discontinuity,
   and truncated media prove timeline/coverage behavior and reject invented data.
3. Multiple original stream indexes, layouts, languages, and the selected-track
   accent prove correct lane association without changing playback selection.
4. Real codec fixtures establish which configured FFmpeg decoders work. Corrupt,
   unsupported, and missing tracks remain truthful per-track unavailable states.
5. Source replacement, revoked ACL, cancellation, restart, duplicate requests,
   and failed publication prove artifact identity and lifecycle behavior.
6. Desktop/mobile visuals verify responsive resolution, theme color, selected
   track, hidden page cancellation, and absence of fake progress/history writes.
7. Remote measurements compare codec/layout/track counts and representative
   MKV/MP4 plus local/NAS storage, recording elapsed time, CPU, peak RSS, source
   bytes read, and final artifact size. They determine default limits and batch
   size; no current estimate substitutes for that evidence.

The algorithm is straightforward and uses existing dependencies. Robust
timeline binding, full-file analysis resource control, and durable task/artifact
integration are the main engineering work. The feature should remain optional
because its current product value is primarily the media-information display.
