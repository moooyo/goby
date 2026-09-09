# Universal and progressive audio

Goby serves audio through PlaybackInfo, Universal and legacy stream routes,
alongside the existing original-file and MPEG-TS HLS delivery paths. The website
remains an administrator dashboard. The [Universal reference study](../research/audio-reference.md)
and [profile reference study](../research/audio-profile-reference.md) separate
observed Emby 4.9.5.0 behavior from Goby's implementation choices.

## Routes and negotiation

All routes below also accept the existing root aliases and case-normalized
literals. Authentication uses the normal Emby token boundary.

| Method and route | Meaning |
| --- | --- |
| GET/POST `/emby/Items/{Id}/PlaybackInfo` for Audio items | Original-file evaluation and ordered HTTP/HLS audio TranscodingProfiles |
| GET/HEAD `/emby/Audio/{Id}/universal` | Choose an original or converted audio representation |
| GET/HEAD `/emby/Audio/{Id}/universal.{Container}` | Universal input-container capability suffix |
| GET/HEAD `/emby/Audio/{Id}/stream[.{Container}]` | Original or explicitly selected progressive conversion |
| GET/HEAD `/emby/Audio/{Id}/original.{Container}` | Explicit original-file delivery |
| Existing `/Audio/{Id}/master.m3u8`, `main.m3u8`, `hls1/...` | Authenticated MPEG-TS HLS output |

[Audio PlaybackInfo profiles](audio-profile-playback.md) preserve mixed HTTP/HLS
profile order and recheck applicable codec/container conditions against actual
constructible output facts. The response keeps original source DTO facts and
provides a standard progressive `stream.{Container}` URL containing the concrete
output settings and canonical owner identity. Progressive negotiation does not
start or reserve an encoding job; the following media request rechecks current
source and permission state.

For Universal, `Container` is a comma-separated list of original formats the
client accepts. `TranscodingContainer` selects a conversion output. The parser
does not reinterpret one as the other. Container aliases include MP4/M4A/M4B,
AAC/ADTS, WAV/WAVE, and OGG/OGA. A container-only declaration does not acquire
the encoder's more restrictive codec list: compatible original ALAC or other
audio can still be served without encoding.

Omitted Universal `Container` means no original-format restriction, as observed
for the four reference inputs. A compatible original takes priority over
conversion fallback settings. Explicit bitrate, sample-rate and channel ceilings
still apply in Goby. A nondefault selected audio track needs a converted output
so a raw container cannot silently select a different track in the player.

`TranscodingProtocol=hls` selects the implemented MPEG-TS HLS path when conversion
is needed. Omission or an empty protocol selects progressive conversion. Goby's
explicit fallback for a required progressive conversion is MP3/MP3; the HLS
fallback is TS/AAC. These are controlled implementation defaults, not a claim
that the sampled reference's missing-output-format failures are a useful default.

`Static=true` requests the complete original file. Original-file starts remain
client-side seeks using ranges or the container's index; `StartTimeTicks` never
becomes an estimated byte offset. Contradictory container selectors, malformed
numbers and conflicting duplicate query fields are rejected.

The parser distinguishes exact `AudioBitrate`, `AudioChannels`, `AudioSampleRate`
and `AudioBitDepth` from the independent `MaxStreamingBitrate`,
`MaxAudioChannels`, `TranscodingMaxAudioChannels` and `MaxSampleRate` ceilings.
The two channel ceilings are separate inputs; conversion uses the stricter
constraint and rejects an exact channel target that exceeds it. They are not
conflicting aliases. `TranscodingMaxAudioChannels` applies only after compatible
original-file selection, so a conversion-only limit does not by itself reject
an otherwise acceptable original. The HLS audio query adapter preserves the same
exact-target versus ceiling distinction. A valid but unconstructible output
returns a compatibility error. Unknown source facts do not prove a capability.
`AudioBitDepth` is an explicit Goby output selection; the reference's exploratory
`MaxBitDepth` result is not advertised as an established upstream contract.

## Progressive outputs

| Output container | Implemented encoded audio |
| --- | --- |
| MP3 | MP3 |
| AAC/ADTS | AAC LC |
| M4A/fragmented MP4 | AAC LC |
| FLAC | FLAC with an explicit 16- or 24-bit target |
| OGG | Vorbis, Opus, or FLAC |
| WAV | PCM signed 16-bit little-endian |

The planner can retain a compatible encoded payload when copying is permitted
and the necessary source/framing facts are known. Explicit copy does not permit
a different codec, unsupported container, changed channel/rate target, or missing
remux permission. AAC from an arbitrary container is not promised to be valid
ADTS merely because its codec name is AAC. Nonzero FLAC copy starts, nonzero
progressive copy from Ogg sources and other unproven copy combinations are
declined. An ordinary codec request can select encoding when permission allows;
an explicit `AudioCodec=copy` request cannot silently become encoding. Compatible
full-file progressive copy and original-file range delivery remain available.
Native WAV copy validates the input WAVE format and data layout.

Without an output precision constraint, FLAC preserves known source precision
through a 16- or 24-bit choice. When a profile includes a bit-depth condition,
the planner prefers the source precision and can try other permitted 16-/24-bit
outputs to satisfy the complete constraints and budget; the selected encoded
precision is explicit in the URL. An unknown source precision
does not become a bit-exact claim. WAV's selected PCM codec explicitly means
16-bit output. Opus output uses its actual 48 kHz clock. Output projections clear
source timing measurements, codec tags and other facts they cannot establish.

For lossy output, the requested maximum is an encoded-media bitrate planning
ceiling. An exact 128 kbit/s target can fit a 128 kbit/s media budget. Container
startup bytes are not amortized over a short clip to make that clip unplayable.
They remain subject to the manager's byte quotas. PCM uses its payload rate;
FLAC uses a conservative sample/frame bound without inventing a compression
ratio. None of these settings implements instantaneous HTTP bandwidth policing.

## HTTP streaming and failure behavior

Original delivery shares the existing authorized file handle, MIME, byte ranges,
ETag and conditional-request implementation. A second source open is not used
to substitute different bytes after Universal negotiation.

Progressive GET streams an append-only output, with truthful MIME,
`Accept-Ranges: none`, and `Cache-Control: no-cache, no-store, no-transform,
must-revalidate`. It does not fabricate Content-Length, ETag or Content-Range.
A Range header is ignored for this representation and the full selected output
is sent with 200, matching the sampled progressive behavior. Seeking uses a new
request with `StartTimeTicks`, subject to the same source and session ownership.

HEAD validates the source, plan, session and current permissions, but starts no
encoder and invents no estimated output length. This deliberately avoids copying
the reference's inaccurate HEAD Content-Length estimates. The response remains
body-free, including for a cached output.

FFmpeg writes media through nonseekable descriptor 4 to the private `stream.bin`;
descriptor 3 still identifies the already opened source, and stdout remains the
progress channel. Input manifests, network fetches and arbitrary output paths
are not enabled. The runner checks for actual initial media payload beyond
container metadata before declaring readiness. A temporary file EOF does not
mean conversion has completed.

The reader waits for more output or a terminal job state. Successful EOF requires
durable completion and consumption of the final bytes. A failed or cancelled job
returns an error. If HTTP headers have already been sent, the handler aborts the
HTTP/1 response or resets the HTTP/2 stream instead of writing a normal chunk
terminator over truncated media. The panic boundary preserves `http.ErrAbortHandler`.

WAV uses a correct prewritten RIFF header followed by raw PCM, rather than a
placeholder length that strict readers can misinterpret at EOF. Exact source
sample counts control output length. Only the defined source/output sample
quantization deficit may be padded; a substantially short source, misaligned
output, excessive output, or a source change fails. RIFF output beyond the
supported 32-bit size is rejected. Headers are not rewritten after transmission.

## Source duration and audio HLS

Probe cache version 3 introduced bounded audio packet/frame scanning; the current
version **4** extends exact timing to supported Ogg Opus, Vorbis and modern Ogg
FLAC streams. For supported continuous audio, decoded sample counts account for
decoder-applied priming, discard and edit semantics. This fixes raw ADTS duration
estimates that would otherwise truncate an encoded output. The scan also records
source-relative effective packet boundaries used by copied-audio HLS.

Source `AudioDurationExact` and selected-track timing must prove continuous
coverage from the source origin to its end before conversion. A delayed, shorter,
gapped or unproven track is not silently treated as exact. Unsupported exact-timing
cases retain ordinary metadata and can still use original-file playback. The
current timing implementation covers MP3, AAC/ADTS, native FLAC/WAV, supported
audio MP4 and the strict Ogg subset described below. Additional formats and timing
cases remain work; Matroska/WebM audio keeps ordinary original-file delivery while
its quantized packet clock remains unproven for this conversion contract.

Ogg timing combines decoded packet/frame evidence with an independent physical
page scan: version, CRC, sequence and continuation must be valid, BOS/EOS complete,
and logical streams cannot restart or chain. Codec headers must match the indexed
rate/channel facts. Supported headers are Opus mapping family 0 or standard family
1, the three Vorbis headers, and modern Ogg FLAC mapping; legacy `fLaC` mapping is
not included. FLAC frame headers also reject rate, channel or precision changes.

Packets can share an Ogg page position, so matching also uses packet ordinal and
the complete decoded frame interval. Opus TOC sample counts, cumulative header
pre-skip and final discard must agree with effective decoded samples. Only the
defined first Vorbis data packet may contribute internal warm-up without a frame;
subsequent coverage must be complete. Timelines are relative to the earliest
effective decoded frame. Exactness therefore describes presented audio, not a
promise to recover encoder warm-up samples from an earlier PCM input.

The Ogg proof is bounded at 32 streams, one million data packets, a 1 GiB physical
scan and 512 MiB of incremental ffprobe output, with at most 4,096 pending packet
associations. At most two topology readers retain independent source descriptors,
and cancellation remains bounded. These checks do not introduce a timing epsilon
to make Matroska/WebM's coarser packet clock appear sample-exact. Exceeding the
supported proof boundaries does not establish an exact conversion timeline.

Integer source sample counts are retained through seek and resampling plans.
They are not reconstructed by rounding an already rounded tick duration again.
Nonzero seeks retain the documented source/output sample quantization boundary;
this is not a claim that every 100 ns request selects an independently representable
audio sample.

Ogg conversion seeking needs more than a correct output count. In bounded Linux
diagnostics, a 100 ms Vorbis source decoded to 4,282 presented samples, but a
nonzero FFmpeg input seek placed output content 128 samples early even when its
length was correct. A 2.013-second Ogg FLAC source sought to 1.2378912 seconds
likewise started 11,264 samples early after demuxer timestamp seeking failed.
These cases require content-position comparisons against full decoding as well
as duration/sample-count checks.

For exact Ogg audio conversion, the private `Plan.AudioSampleSeek` policy selects
decoding from the beginning and trims the requested Start/End window in the
source sample domain. Timestamps are rebased before resampling and final output
sample limiting. This policy applies to encoded progressive and HLS audio, is
derived from proven server-side source facts, and is not a client query option.
Other input formats retain their existing seek path. Ogg HLS requires encoding;
nonzero progressive Ogg copy remains declined as described above.

Decoding the source prefix consumes real time and resources. Existing startup,
job-runtime, concurrency and storage bounds still apply, so this is not a promise
of fast random Ogg page seeking. A distant seek can exceed those budgets instead
of bypassing them. Final runner/HLS/HTTP coverage belongs to the versioned
verification report; the diagnostic findings alone do not establish that every
Ogg conversion path has passed end-to-end acceptance.

Later AAC seeks can rebuild decoder overlap or PNS state differently from a
decoder started at zero. Exact output counts are verified independently of a
claim that all later lossy-codec waveforms are byte-identical to a full-decode slice.

Audio HLS still advertises a complete source timeline with stable global numbers.
An encoded final remainder of at most one output frame is merged into the
preceding segment. Copy uses measured last-packet facts with a conservative
packet-phase allowance. Total duration is preserved, target duration is recomputed,
and an unproducible extra URL is never published. The existing MPEG-TS transport
and playlist discontinuity declarations remain in force. Packed AAC/MP3 HLS,
gapless presentation, adaptive variants and unverified timing profiles are not
implied by this implementation.

## Ownership, cancellation and limits

[Client playback references](client-playback-references.md) allow a fresh
client-generated PlaySessionId without a preceding PlaybackInfo call. Internally,
the manager and HLS registry continue using canonical server-generated identities.
Another authentication session's token cannot use an existing output revision,
even for the same user or device name. Current token, account, library, source
snapshot and conversion permissions are checked before returning media.

Progressive consumers share the existing conversion manager, queue, source
descriptors, byte budgets and reader limits. They share the server's 64 original/
audio response slots; there is no second unbounded encoder pool. Startup waiting
is limited to 45 seconds, individual writes to 30 seconds, and a progressive
response to four hours. Runtime cancellation interrupts blocked readers and writes.

Closing one of several consumers does not cancel the others. The last consumer
cancels unfinished production; a successfully completed file may remain cached.
Retirement fences manager deduplication before the same output key can be registered
again, preventing a reconnect from inheriting a producer being cancelled.

Successful media writes refresh registry activity. Recent media access can renew
the prepared playback lease through the existing Ping semantics without changing
position, play count or watched state. Stopped, logout, ActiveEncodings cleanup
and confirmed permission/source changes retire the owned work. A read-only media
request never substitutes for a client playback-progress report.

## Upgrade and remaining work

Migration `0012` adds scoped playback references. Run normal scans of existing
libraries after upgrading to probe version **4**, including libraries last scanned
with version 3. Older snapshots block indexed media/subtitle reads until refreshed;
they do not establish the current source/duration contract. The profile/Ogg
increment adds no database migration beyond the existing schema 12 baseline.
Existing local metadata, artwork,
identities and user state remain in PostgreSQL.

The current work does not complete all Emby playback or administrator APIs.
Additional input/timing profiles, progressive video, richer HLS/profile/subtitle
behavior, real hardware execution, long-form/client acceptance and the remaining
administrator/recovery workflows stay in the [delivery plan](../planning/delivery-and-verification.md).
