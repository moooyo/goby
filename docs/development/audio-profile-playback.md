# Audio PlaybackInfo profiles

Audio PlaybackInfo supports the existing original-file evaluation and ordered
HTTP progressive and HLS conversion profiles. This guide describes the implemented
planning and URL contract; it does not record a completed test run or establish
compatibility with every third-party client. The administrator dashboard remains
separate from client playback.

## Profile selection and output conditions

Both `GET` and `POST /emby/Items/{Id}/PlaybackInfo` serve Audio catalog items.
Send `DeviceProfile` in the POST JSON body for profile negotiation; GET retains
the existing minimal/query request behavior and does not define a nested-profile
query serialization. `TranscodingProfiles` retain their declared array order across
`http` and `hls`: the first authorized, constructible candidate whose output
satisfies the profile is selected. An unusable leading candidate can fall through
to a later candidate; a global HLS preference does not reorder the list.

For audio profiles, omitted or empty `Protocol` selects progressive HTTP, and
omitted or empty `Context` is treated as Streaming. `Context=Static`, unsupported
protocols, and unimplemented output behavior do not produce a usable conversion.
The progressive planner requires a supported container and codec selector; it
does not apply Universal URL defaults to missing profile output selectors.

Progressive container aliases normalize `mp4`/`m4b` to `m4a`, `adts` to `aac`,
`wave` to `wav`, and `oga` to `ogg`. The same normalization applies to container
selectors in TranscodingProfiles, ContainerProfiles and CodecProfiles, while the
original evaluation remains unchanged. Composite codec/container labels such as
`opus` are not blindly treated as generic container aliases. Profiles requiring
timestamp copying, estimated content lengths, byte-based seek information,
manifest subtitles or a selected subtitle are outside this progressive contract.

For example, this is an illustrative Goby profile list, not a copied SDK example:

```json
{
  "DeviceProfile": {
    "SupportedMediaTypes": "Audio",
    "TranscodingProfiles": [
      {"Type": "Audio", "Protocol": "http", "Context": "Streaming", "Container": "mp3", "AudioCodec": "mp3"},
      {"Type": "Audio", "Protocol": "hls", "Context": "Streaming", "Container": "ts", "AudioCodec": "aac"}
    ]
  }
}
```

The planner intersects current server/user limits with request and profile
streaming bitrate/channel ceilings, including `MusicStreamingTranscodingBitrate`.
`MaxStaticMusicBitrate` remains part of original-file evaluation; it is not reused
as a streaming encoder ceiling. Capability declarations do not grant remux or
encoding permission. A permitted copy is considered against its real facts;
when it fails a required condition, an allowed encoding may still satisfy that
same profile before a later profile is considered.

Applicable `CodecProfiles` and `ContainerProfiles` are reevaluated against the
projected output. Numeric equality, lower/upper bounds and supported alternatives
remain distinct constraints; an exact sample rate is not silently reduced to a
maximum. Channel/rate/bitrate choices and explicit FLAC precision must be
constructible by the selected codec. Output-dependent applicability is checked
again after those choices. Required unknown or contradictory conditions reject
the candidate; optional unknown conditions remain unverified, not fabricated
output facts.

With a FLAC bit-depth condition, candidate selection prefers the source precision
then considers permitted 16-/24-bit output. A permitted lower precision may satisfy
a bitrate budget that excludes 24-bit output; it still requires encoding permission
and becomes an explicit output setting. Without that condition, the existing
source-precision preference is retained.

One request has a budget of 2,048 candidate evaluation units. Each progressive
plan/output evaluation consumes one; each HLS profile reserves its bounded
four-evaluation allowance from the same budget. A profile also has at most eight
condition-refinement states and 64 parameter variants. Exhaustion records the
planner reason `audio_profile_search_limit` and does not advertise an unverified
plan. These are implementation bounds, not additional client-controlled settings.

Projection describes one output audio stream at index zero and the remaining
output duration. It clears original packet timing, source tags and other facts
that encoding cannot establish. The immutable execution plan independently
retains the selected original stream index, source duration and exact integer
sample counts. Validation therefore neither selects the wrong source track nor
applies the seek twice.

## Source facts and delivery facts

Audio PlaybackInfo keeps the authorized original media source in `MediaSources`.
Its `Container`, `RunTimeTicks`, `Bitrate`, `Size` and `MediaStreams` describe that
source, including original stream indexes and precision. A converted stream is
not substituted into the original source DTO.

When a progressive candidate is available, the delivery fields identify it:
`SupportsTranscoding=true`, `TranscodingContainer` is the selected output
container, `TranscodingSubProtocol=http`, and `TranscodingUrl` uses the standard
`/emby/Audio/{Id}/stream.{Container}` route. An allowed copy/remux is physically a
DirectStream operation; when transcoding delivery is disabled and original direct
streaming is unavailable, the existing response contract can expose that route
as `DirectStreamUrl` instead. These delivery fields do not relabel the original
codec, bitrate, sample rate or bit depth.

## Executable progressive URLs

The generated URL carries concrete plan settings rather than unresolved client
preferences:

| URL field | Meaning |
| --- | --- |
| `Static=false` | Use the converted stream representation |
| `StartTimeTicks` | Requested source position, including an explicit zero |
| `AudioStreamIndex` | Selected original stream index |
| `AudioCodec` | Actual selected encoder, or `copy` for an approved remux |
| `AllowAudioStreamCopy` | `false` for encoding; `true` for an approved copy |
| `AudioChannels`, `AudioSampleRate` | Concrete encoded output settings |
| `AudioBitrate` | Concrete encoded bitrate when the plan has a bitrate target |
| `AudioBitDepth` | Explicit FLAC output precision when FLAC is encoded |
| `DeviceId`, `MediaSourceId`, `PlaySessionId`, `api_key` | Current owner, source, canonical playback identity and token |

A copy URL does not attach invented encoding parameters. The selected codec and
framing must still be compatible with the actual source when the media request
is evaluated. FLAC without a fixed encoded bitrate does not receive a fabricated
bitrate target; its output budget uses the existing conservative frame/sample
bound.

`AudioChannels` is an exact output target. `MaxAudioChannels` and
`TranscodingMaxAudioChannels` are independent ceilings, so the stricter value
constrains conversion without treating the two names as duplicate query fields.
An exact value above the effective ceiling is rejected. On Universal/legacy
selection, the transcoding-only ceiling is applied after compatible original
delivery has been considered; it cannot by itself force an accepted original
file through an encoder. These rules also apply to the HLS audio query adapter.

The client can change `StartTimeTicks` to request a later or earlier position
through the ordinary audio route. Source positions stay separate from projected
output coordinates: the plan retains the original stream index and source sample
counts, while profile evaluation uses the resulting single output audio stream.
This URL does not require a private progressive revision parameter.

Seek changes remain subject to codec/source restrictions. A nonzero seek on an
explicit Ogg copy URL is declined; a client needs an encoding-capable alternative
when permitted. Compatible full-file copy and original-file range requests keep
their existing behavior.

## Request lifecycle

PlaybackInfo opens and validates the indexed source and prepares the canonical
playback session. Selecting a progressive output does not start FFmpeg, create an
encoding job, or reserve progressive manager/registry capacity. A successful
negotiation therefore does not promise that resources or permissions will still
be available when GET arrives.

The media request rechecks the current token, account, device/playback ownership,
library access, source snapshot and conversion permissions. Its query parameters
are evaluated as a normal audio request; prior negotiation cannot authorize a
stale source or a denied encoder. Progressive HEAD performs the corresponding
checks without starting an encoder or estimating output length.

GET uses the shared progressive manager and its append-only output reader.
Temporary EOF does not mean success, and failure after headers aborts HTTP/1 or
resets HTTP/2. Shared quotas, last-consumer cancellation, exact WAV lengths and
sample quantization follow the [audio playback contract](audio-playback.md).
Canonical identifiers and nonce tombstones follow [client playback references](client-playback-references.md).

## Source timing and upgrade

Profile acceptance does not replace source-timing proof. Current probe cache
version **4** includes the strict Ogg Opus/Vorbis/modern FLAC subset alongside the
previous continuous-audio formats. Physical pages, stream topology, headers and
decoded packet/frame intervals must establish the presented sample timeline.
Matroska/WebM's quantized packet clock remains unproven for conversion; compatible
original-file playback can remain available. See the [timing contract](audio-playback.md#source-duration-and-audio-hls)
for exact bounds and unsupported cases.

For proven exact Ogg inputs, encoded progressive/HLS plans select private
`AudioSampleSeek`: decode from the beginning, trim the source-sample window, rebase
timestamps, then resample and limit output. The flag and source sample measurements
are not serialized into client URLs or accepted as client authority. This prevents
an apparently correct output length from concealing the wrong source position.
Prefix decoding remains subject to existing startup/job/resource budgets and does
not promise high-performance random page seeking. See the [audio seek contract](audio-playback.md#source-duration-and-audio-hls)
for the motivating diagnostic cases and copy restrictions.

Rescan existing libraries, including version 3 snapshots, before relying on the
current media/subtitle access and duration facts. This increment uses the existing
schema 12 and conversion configuration; it adds no consumer playback page or
separate progressive worker pool.

## Evidence and remaining scope

Universal's `TranscodingProtocol` query and `TranscodingProfile.Protocol` are
different contract fields. A documented default for the former does not prove a
default for the latter. The pinned SDK declares profile fields without defining
all missing/empty-value behavior or mixed-profile ordering. The bounded
[Emby 4.9.5.0 profile study](../research/audio-profile-reference.md) separately
observed the following:

| Profile input | Observed reference behavior |
| --- | --- |
| `Protocol=http` | Progressive `stream.mp3`; `TranscodingSubProtocol=http` |
| Omitted `Protocol` | Progressive `stream.mp3`; `TranscodingSubProtocol` omitted |
| Empty `Protocol` | Progressive `stream.mp3`; empty `TranscodingSubProtocol` |
| Omitted or empty `Context` | Selected the streaming profile in the sampled requests |
| `Context=Static` as the only candidate | No usable match; an advertised bare stream URL failed when fetched |
| HLS before HTTP, with both usable | HLS selected |
| HTTP before HLS, with both usable | Progressive selected |

Goby normalizes a selected progressive result to the truthful protocol string
`http`; its string model does not preserve omitted versus empty profile fields.
An unsupported profile does not become an advertised stream that cannot execute.
The reference's sampled `AudioSampleRate=44100` equality condition still produced
48 kHz output, so that result does not establish correct constraint handling.
Output-condition verification must use the constructible output's facts. The
sampled exact mono condition and a separate maximum of two channels likewise
remain distinct targets and ceilings.

Some reference HTTP controls first returned 500 and then a shortened successful
retry. Those MP3 bodies decoded without an error but contained only 5.064979
seconds, despite an estimated six-second format duration. Other controls returned
complete measured outputs. The [reference report](../research/audio-profile-reference.md#progressive-containers-and-actual-media)
preserves that distinction; a 200 response or decoder exit code alone does not
prove complete source-duration playback.

The dashboard remains an administrator interface without a consumer player.
Progressive video, additional audio timing/container cases, packed AAC/MP3 HLS,
adaptive variants, richer subtitle delivery, actual GPU execution and full client
acceptance remain required work. Verification evidence
belongs to the versioned reports linked from [implementation progress](progress.md).
