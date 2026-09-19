# Progressive video playback

The closed [AMD media increment](amd-media-phase1-20260919.md) extends this
contract with HEVC Main/Main 10 and AV1 Main 8/10-bit MP4 encoding, supported
Dolby Vision conversion, GPU processing, progressive subtitle burn-in and
broader copy seeking. Its completed selected-profile acceptance is recorded separately.
Historical M4e evidence below describes that earlier accepted increment;
use [advanced media](advanced-media.md), [AMD processing](amd-video-processing.md),
[hardware admission](hardware-encoding-admission.md) and
[copy seeking](copy-seek-compatibility.md) for the current added source contract.

M4e originally connected Video PlaybackInfo profiles and the standard video stream
routes to bounded H.264/AAC fragmented MP4 delivery. Its separate
[acceptance report](verification-m4e-video-and-users.md) records the complete Linux
suite and deployed workflow for that historical source, verified through
`ssh test-env`. Neither these endpoints nor the bounded
[Emby 4.9.5.0 reference study](../research/video-progressive-reference.md) establish
complete compatibility with arbitrary clients, sources, or GPU configurations.

## Routes and original-file behavior

| Route | Implemented representation |
| --- | --- |
| `GET`, `POST /emby/Items/{Id}/PlaybackInfo` | Authorized original-source facts and available profile-negotiated output |
| `GET`, `HEAD /emby/Videos/{Id}/stream.mp4` | Original-compatible legacy delivery or an explicitly requested progressive MP4 conversion |
| `GET`, `HEAD /emby/Videos/{Id}/stream` and `stream.{Container}` | Original legacy delivery when unconstrained and container-compatible; supported explicit conversion otherwise |
| `GET`, `HEAD /emby/Videos/{Id}/original.{Container}` | Complete original bytes; equivalent to an explicit original-file request |

Recognized route literals also accept the existing root aliases and case variants.
Dynamic identifiers retain their values and never become filesystem paths.
Audio catalog items use the separate [audio routes](audio-playback.md).

An unconstrained legacy request can return the original file. `Static=true`
explicitly retains that behavior when the requested container matches the source.
`original.{Container}` rejects a conflicting `Static=false`. Original delivery
does not remux, resize, change tracks, or infer a byte offset from ticks. A bare
original request's `StartTimeTicks`, or its legacy nonnegative
`StartPositionTicks` hint, remains a client-side seek instruction. Conversion
does not accept `StartPositionTicks` as an unverified alias for `StartTimeTicks`.

`Static=false`, a requested output container different from the original, or
recognized output requirements enter conversion negotiation. Explicit unsupported
transformations and malformed numeric parameters cannot silently become raw
delivery. An explicit `Static=true` keeps its original-file semantics rather than
claiming to apply encoder hints to those bytes.

The conversion output is MP4. Video URL selectors normalize `m4v`, `mov`, and
`fmp4` to the MP4 family, and normalize the existing MPEG-TS/Matroska aliases for
original-container matching. They do not use the audio adapter's `mp4` to `m4a`
normalization. A filename extension or selector is not proof that arbitrary source
codecs can be copied into the requested output.

## Ordered PlaybackInfo profiles

Supply `DeviceProfile` in the POST PlaybackInfo JSON body. GET retains the
existing minimal/query request behavior; it does not introduce a nested-profile
query serialization. Video `TranscodingProfiles` preserve declared array order
across HTTP and HLS. The first authorized, constructible output satisfying the
profile is selected; an unusable earlier profile can fall through to a later one.

An omitted or empty video profile `Protocol` is treated as HTTP, and an omitted
or empty `Context` as Streaming. Goby emits `TranscodingSubProtocol=http` for a
selected progressive result. `Context=Static` does not select a streaming
conversion. A progressive profile must explicitly offer `Container=mp4` and
a supported `VideoCodec` (`h264`, `hevc`, or `av1`), plus `AudioCodec=aac` when the source has audio. Missing profile
selectors do not inherit the manual stream URL's defaults. HLS profiles use the
separate [HLS contract](hls-playback.md), including supported MPEG-TS and fMP4 output.

This example illustrates Goby's implemented profile vocabulary:

```json
{
  "EnableDirectPlay": false,
  "EnableDirectStream": false,
  "EnableTranscoding": true,
  "DeviceProfile": {
    "SupportedMediaTypes": "Video",
    "TranscodingProfiles": [
      {"Type": "Video", "Context": "Streaming", "Protocol": "http", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"},
      {"Type": "Video", "Context": "Streaming", "Protocol": "hls", "Container": "ts", "VideoCodec": "h264", "AudioCodec": "aac"}
    ]
  }
}
```

The planner intersects request, profile, and server ceilings and evaluates
applicable `ContainerProfiles` and `CodecProfiles` against projected output.
Exact equality, upper/lower bounds, and supported alternatives retain their
different meanings. Required unknown or contradictory facts reject a candidate;
optional unknown facts remain unverified. An allowed encoding can establish
facts that a copied source cannot prove. Candidate exploration is bounded rather
than an exhaustive search across every possible encoder setting.

`MediaSources` continues to describe the authorized original file: its container,
duration, size, bitrates, and stream indexes are not replaced with output guesses.
The progressive delivery fields are `TranscodingContainer=mp4`,
`TranscodingSubProtocol=http`, and the standard `TranscodingUrl`. The existing
delivery-flag contract can expose a permitted remux as `DirectStreamUrl` when
transcoding delivery is disabled and original direct streaming is unavailable.
These URL/flag names do not determine which tracks were physically copied.

PlaybackInfo prepares the canonical play session but does not start a producer
for user media or reserve a progressive job. New VAAPI output may run the bounded
synthetic encoder and capability checks described in [hardware admission](hardware-encoding-admission.md).
A supported negotiation does not promise that source
facts, permission, or capacity will remain available when the media request arrives.

## Stream query parameters

Manual conversion URLs default to MP4/H.264 and AAC for an existing audio track.
These are Goby defaults, not inferred defaults for missing profile selectors.
Explicit codec candidate lists are bounded; an unsupported legal candidate does
not block a later supported candidate. Existing audio is not silently dropped,
and a source without audio does not acquire an invented track.

| Parameters | Meaning during conversion |
| --- | --- |
| `Container`, `TranscodingContainer`, route suffix | Output container selectors; conflicting selectors are rejected |
| `Protocol`, `TranscodingProtocol` | HTTP/progressive delivery on this route; HLS uses the HLS endpoints |
| `StartTimeTicks` | Nonnegative presentation position in 100 ns ticks, before the source end |
| `VideoStreamIndex`, `AudioStreamIndex` | Actual indexed source tracks, not projected output indexes |
| `VideoCodec`, `AudioCodec` | Supported codec candidates or explicit `copy`; existing audio cannot be removed with `none` |
| `VideoProfile`, `VideoBitDepth` | Supported codec profile and exact 8/10-bit encoding; H.264 is 8-bit only |
| `VideoRange`, `VideoRangeType` | Consistent SDR or HDR10 output selectors; HDR10 encoding requires a supported 10-bit codec |
| `SubtitleStreamIndex`, `SubtitleMethod=Encode`, `BurnSubtitles=true` | Select an authorized indexed subtitle for burn-in; incompatible selectors and copied video are rejected |
| `SubtitleOffsetTicks` | Signed subtitle offset in 100 ns ticks, bounded to 24 hours in either direction; requires burn-in |
| `CopyTimestamps` | Preserve the normalized source clock for an admitted copied seek; emitted for standard aligned-copy negotiation |
| `Width`, `Height` | Exact encoded dimensions; a single supplied dimension preserves the aspect ratio with bounded even rounding |
| `MaxWidth`, `MaxHeight` | Independent output ceilings, further limited by server configuration |
| `Framerate` | Exact frame conversion, from 1 through 240 fps with at most six fractional decimal digits |
| `MaxFramerate` | Frame-rate ceiling; copied media must prove it, and encoding can construct a bounded rate |
| `VideoBitrate`, `AudioBitrate` | Exact encoded targets, or exact known source declarations when evaluating copy |
| `MaxVideoBitrate`, `MaxAudioBitrate`, `MaxStreamingBitrate` | Per-track and combined media bitrate ceilings |
| `AudioChannels`, `AudioSampleRate` | Exact output targets |
| `MaxAudioChannels`, `TranscodingMaxAudioChannels`, `MaxSampleRate` | Independent ceilings; the stricter channel ceiling applies |
| `AllowVideoStreamCopy`, `AllowAudioStreamCopy` | Copy restrictions; they do not grant encoding or remux permission |
| `EnableAutoStreamCopy=false` | Disable copying of both selected streams |
| `AllowInterlacedVideoStreamCopy=false` | Require proof that copied video is progressive |

Exact values above an effective ceiling are rejected rather than weakened into
maximums. The runner further restricts dimensions, rates, bitrates, channels, and
codec combinations. The combined bitrate budget is a media planning limit, not
instantaneous HTTP bandwidth policing. See [conversion configuration](transcoding-configuration.md)
for server limits and execution policy.

Query names are case-insensitive; conflicting values for the same normalized
name are invalid. Query size, text, lists, integers, and frame rates are bounded.
Identity hints such as `UserId`, `DeviceId`, and `PlaySessionId` do not establish
authority. Unknown source-clock and hardware query fields cannot supply trusted
facts or arbitrary FFmpeg options.

Malformed requests return 400. A structurally valid request with no supported
output returns 415 with `NoCompatibleStream`; PlaybackInfo instead reports its
normal unavailable-capability result. Token/access/source checks and resource
admission can independently return 401, 403, 404, 429, or 503 as appropriate.

## Executable URLs and seeking

Generated video URLs contain `Static=false`, the source stream indexes,
`StartTimeTicks`, concrete codecs, per-stream copy flags, canonical play/source/
device identifiers, and `api_key`. Encoded video carries its dimensions and
bitrate; an explicit frame rate is included only when the plan actually converts
frame pacing. Encoded audio carries its bitrate, channels, and sample rate.
Copied streams do not receive fabricated encoding parameters. A video-only plan
does not emit audio parameters. A burn plan publishes its selected subtitle and
offset; an output without burn-in uses `SubtitleStreamIndex=-1`. External text
delivery remains separate from MP4 composition.

A media request rebuilds the plan from current source facts and concrete output
settings. It does not accept `SourceFormatStartKnown`, `SourceFormatStartTicks`,
hardware configuration, or source duration from the URL. The URL needs no private
progressive revision identifier. Changing `StartTimeTicks` on an encoded URL can
request another position while retaining its concrete output settings; current
policy and source validation still apply.

Compatible H.264/HEVC/AV1 video and AAC audio can be copied, including bounded
mixed copy/encode combinations. Nonzero copied starts require a source-bound
random-access proof. Standard profile negotiation may align to a proven earlier
point within ten seconds and publishes the actual start with
`CopyTimestamps=true`; exact/manual requests retain their explicit contract.
See [copy seeking](copy-seek-compatibility.md). Editing a URL does not authorize
an unproven restart or grant encoding permission. An ordinary codec request may
fall back to encoding only when the current user permits it.

The fallback encoded seek path decodes from the beginning, retaining one common
source clock for audio and video. Eligible M4f software video decoding can use a
freshly verified input restart while audio independently retains its linear
history; an unproven demuxer landing does not authorize that optimization.
Explicit frame-rate conversion
changes frame pacing; without it, encoding preserves source timestamp pacing
instead of claiming an invented constant frame rate. Packet priming, encoder
padding, and output track intervals require actual media measurements; no general
sample-exact A/V or perceptual synchronization claim follows from a successful
HTTP response.

## Format-clock origin and current probe cache

Probe cache version 5 introduced FFprobe's explicitly reported format-clock origin in
`FormatStartTicks` and `FormatStartKnown`. Known zero, positive, and negative
origins are distinct from an absent origin. This fact is independent of the
first audible sample, an individual stream's first packet, and the audio-only
`PresentationOriginTicks` measurement.

Current probe version 7 retains those facts and extends optional private
restart evidence to the supported HEVC/AV1 and AAC-copy combinations, alongside
actual Dolby Vision RPU facts. The [M4f operating contract](video-fast-seek.md) describes the
verified software-decoder input seek and independent linear audio input. Missing
or unproven evidence keeps the preceding linear path; hardware decoding still
uses that path. A normal scan upgrades older probe caches. These additions do
not change the public playback URL or authorize a client-supplied proof.

The progressive video plan requires a verified bounded format origin and applies
its offset consistently to every selected track. Missing origin data cannot be
replaced by a client parameter or assumed zero. These execution facts are kept
private; the implementation does not fabricate a `ContainerStartTimeTicks` DTO
field from the reference's omission of that property.

Run a normal library rescan after upgrading older probe caches, including version
4. Existing snapshot guards block indexed media and external-subtitle delivery
until those old snapshots are refreshed. A current source whose format
origin remains unknown can still be served as an authorized original; it cannot
promise progressive video conversion. Video conversion does not reuse the
audio-only exact-sample gate as proof of its shared A/V clock.

## Authorization, HTTP delivery, and lifetime

Every media request requires a valid Emby token and current user, playback policy,
library access, and indexed-source validation. Administrator dashboard cookies
do not authenticate media routes. Explicit playback/remux/encoding denials also
apply to administrators. `EnablePlaybackRemuxing` permits all-copy conversion;
each encoded track requires its corresponding audio/video transcoding permission.
Client profiles and copy flags cannot grant those permissions.

Converted requests bind canonical playback ownership under the authenticated
user, authentication session, device, item, and media source. A permitted fresh
client `PlaySessionId` can be resolved through the existing
[nonce binding contract](client-playback-references.md). Reuse cannot change its
resource or revive a stopped play. The original-file route retains its independent
token/source-access contract. Preparation or byte delivery does not fabricate
Started/Progress/Stopped reports or mutate watched state.

| HTTP behavior | Original file | Progressive MP4 |
| --- | --- | --- |
| MIME | Probed original type | `video/mp4` |
| Successful Range request | Standard byte/suffix/multipart range behavior | Range ignored; 200 sends the complete selected representation |
| Validators and length | Indexed ETag, source length, conditional requests | No estimated Content-Length, ETag, or Content-Range |
| `Accept-Ranges` | `bytes` | `none` |
| HEAD | Original-file checks and headers | Current source/plan/ownership/policy checks, no encoder startup and no estimated output length |

Progressive GET streams the manager's private append-only output. Temporary EOF
does not mean completion. Successful EOF requires successful producer completion
and consumption of its final bytes; a failure after headers aborts HTTP/1 or
resets the HTTP/2 stream. Readers of an identical immutable plan share work within
the same canonical scope. The final departing consumer cancels unfinished work;
completed output remains eligible for bounded cache reuse.

Video uses the existing shared queue, cache quotas, stream slots, cancellation,
and shutdown supervision. Startup, response lifetime, and blocked writes have
separate deadlines. Nonzero seeks into long media may spend substantial CPU and
time decoding the prefix before output becomes ready; startup limits still apply.
See the [conversion engine](transcode-engine.md) for ownership and process bounds.

## Reference evidence and remaining scope

The [M4e reference report](../research/video-progressive-reference.md) observed
working progressive MP4 for explicit, omitted, and empty HTTP protocol values,
and preserved mixed HTTP/HLS profile order in its small matrix. Goby's emitted
`http` string intentionally normalizes the reference's differing omitted/empty
DTO values. Goby also omits the reference's estimated HEAD length.

The reference's successful mixed seek to 6.37 seconds copied video beginning at
the preceding 6.0-second keyframe without an edit list hiding that pre-roll.
Other copy retries were truncated despite a 200 status. These observations explain
M4e's historical nonzero video-copy rejection and do not justify advertising an
earlier keyframe or incomplete output as precise successful playback.

The independent [copied-video seek diagnostics](../research/video-copy-seek/README.md)
also found that a correctly written fragmented-MP4 edit list did not make the
default FFmpeg consumer suppress retained video pre-roll. Explicit presentation
filtering recovered the intended frames, but arbitrary clients cannot be assumed
to apply that filter. Buffered nonfragmented MP4 was viable in bounded controls;
it is a separate future delivery path with finalization, resource, source-clock,
and client requirements. The current source-clock copy path is separately
verified; it does not rely on consumers hiding arbitrary pre-roll with an edit list.

Current output remains MP4 with the supported video codecs and AAC, using
indexed media and explicitly supported color, deinterlace and subtitle operations.
Arbitrary filters and progressive live-stream conversion are unavailable.
Selected indexed text or bitmap subtitles can be burned while video is encoded;
text extraction and separate [subtitle delivery](external-subtitles.md) retain
their own routes. Exact source, codec, clock and permission checks still apply.
The [phase 1 record](amd-media-phase1-20260919.md) owns actual AMD and browser
evidence; it does not imply broad real-client acceptance. The React/MUI
dashboard remains an administrator interface with no
consumer player, watch page, or playback UI.
