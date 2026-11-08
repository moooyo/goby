# Managed transcoding options

The administrator settings surface resolves these options for each new playback
admission. Existing sessions and durable transcode records retain the execution
choices captured when they were admitted. The preceding eleven-group increment is
locally verified in [its closeout](../development/admin-native-transcoding-20261107.md).
The [November 8 selections](../development/admin-native-transcoding-decisions-20261108.md)
extend the closed runtime contract to fifteen groups; their current verification
is recorded separately from the preceding source and assets.

`Runtime` updates use the existing native administrator settings routes and their
revision and CSRF checks. A missing field preserves its saved override. An explicit
`null` removes that override. Non-null codec, policy, tone-map, and codec-selection
values are complete groups; arbitrary object properties are rejected.

## Software video encoders

`Runtime.H264` and `Runtime.HEVC` have `Preset`, `RateControl`, `CRF`,
`ThreadCount`, and `X264Options` fields in the management representation.

| Field | Supported value and effect |
| --- | --- |
| H.264 `Preset` | `superfast`, `veryfast`, `faster`, `fast`, `medium`, `slow`, `slower`, `veryslow` |
| HEVC `Preset` | The H.264 values plus `ultrafast` and `placebo` |
| `RateControl` | `bitrate` uses the planned bitrate; `capped_crf` uses CRF while retaining the planned bitrate cap |
| `CRF` | Integer `1..51`; dormant in bitrate mode |
| `ThreadCount` | `null` inherits the existing runtime thread limit, `0` delegates selection to the encoder, and `1..16` sets a per-codec limit |
| H.264 `X264Options` | Empty or the bounded analysis and quality parameters below |
| HEVC `X264Options` | Empty; x264 options cannot affect x265 |

The historical defaults remain `veryfast` with bitrate control, H.264 CRF 23,
HEVC CRF 28, and inherited runtime threads. The observed native Emby editor uses
CRF by default; changing that existing Goby default requires an explicit settings
change. Emby's obsolete global `EncodingThreadCount` is not presented as a current
native encoder control. Goby's existing `Runtime.Threads` remains available for
its historical execution policy.

`X264Options` accepts colon-separated `key=value` pairs for `subme` (`0..9`),
`me-range` (`4..16`), `rc-lookahead` (`0..250`), `aq-mode` (`0..3`),
`aq-strength` (`0..3`), `deblock` (two comma-separated integers, each `-6..6`),
and `partitions` (`none` or `all`). Underscore aliases are normalized, and
`merange` is accepted as an alias for `me-range`. Duplicate, unknown, file-access,
GOP, thread, profile, and rate-control parameters are rejected before admission.
The supported set includes the observed native x264 factory analysis parameters.
These bounds are Goby's validated subset rather than claims about Emby's complete
parameter grammar.

## Encoding and extraction policy

`Runtime.Transcoding` contains the following complete policy group.

| Field | Effect |
| --- | --- |
| `EnableHevcEncoding` | Gates new HEVC encoding candidates; original HEVC playback and video copy remain valid |
| `EnableHardwareEncoding` | Gates hardware encoding while retaining hardware decoding and filter device dependencies |
| `DownMixAudioBoost` | Multiplier `0.1..10` applied only when a known surround source with more than two channels is encoded to stereo |
| `EnableSubtitleExtraction` | Gates embedded subtitle extraction offered and delivered by playback; sidecar subtitles and subtitle burn processing have their own paths |
| `EnableOnTheFlyAttachmentExtraction` | Gates extraction of embedded font attachments for subtitle rendering |

The historical Goby policy enables both encoding flags and both extraction flags,
with downmix multiplier 1. The inspected native Emby factory enables hardware
encoding and extraction, disables HEVC encoding, and uses downmix multiplier 2.
Goby retains its existing defaults so this increment does not silently alter
existing playback output. The administrator can explicitly select the native
reference values. The downmix multiplier range is a Goby validation boundary.

`Runtime.CodecConfigurations` is a complete two-entry array. Each entry has
`CodecId`, `IsEnabled`, and `Priority`. The supported native identities are
`V-E-libx264` and `V-E-libx265`, explicitly mapped to the implemented software
encoders. Each identity must appear exactly once. Priority is an integer
`-1000..1000`; larger values are preferred among otherwise eligible client-offered
software encoding candidates. Equal priorities preserve the client's candidate
order. Disabled software encoders cannot be selected through a hardware fallback.
The array is normalized by identity for persistence. The default enables both
entries at priority zero.

## Software tone mapping

`Runtime.SoftwareToneMapOptions` contains `SoftwareToneMapAlgorithm` and
`ToneMapSizeLimit`. Algorithms are `Hable`, `Reinhard`, and `Mobius`; the default
is `Hable`. Resolution values are `None`, `Limit420`, `Limit720`, and `Limit1080`;
the default is `None`. A resolution limit lowers the encoded output height while
preserving aspect ratio and the existing server and client limits. It applies
only to the selected CPU tone-mapping path.

The existing software and Vulkan enable switches remain separate. The output
plan captures the actual tone-mapping algorithm and dimensions. An inactive
algorithm or resolution preference cannot create a different output identity.
Goby retains its historical enabled switch defaults; the inspected native
factory has both tone-mapping switches disabled.

## Transcoding cache path

`Runtime.TranscodingTempPath` is a string override. Empty selects the deployment
cache location; `null` removes the override. A non-empty override must be a
canonical absolute Linux directory dedicated to transcoding and separate from
media, other caches, scratch, logs, web assets, and recovery stores. Saving the
value persists a desired location without creating or opening a cache manager.

The management response reports desired and active locations with a restart
requirement. A subsequent process startup validates the selected path through the
existing cache ownership marker, lock, and no-follow protections. It refuses an
unowned non-empty directory. Saving a path does not migrate a previous cache or
delete user directories.

## Selected HLS buffer policy

`Runtime.EnableThrottling` is an independent nullable boolean preference, default
false. It applies to new file-backed HLS video encoding admissions; progressive
and live transports retain their existing resource/backpressure behavior.
Existing admissions retain their captured policy.

The implementation adapts the automatic behavior to Goby's HLS architecture.
Continuous producers can pause and resume their owned process without changing
its identity. Published finite windows delay unnecessary subsequent admissions
while retaining requested delivery and current source/permission checks. The
internal high/low buffer thresholds and bounded request credit are implementation
policy, not editable native-method or threshold fields. Unknown or initial demand
and required unpublished output cannot enter a wait that prevents first delivery.

Stock Emby 4.9.5.0 uses a different mechanism: its default 120-second buffer and
8-second hysteresis select a 100 ms wait per input cycle through Emby FFmpeg's
stdin control. The inspected native code also handles unknown position, small
ahead time and the end-of-source separately. The native behavior and Goby's
continuous-process/window-admission implementation must not be called identical.

## Independent image extraction preference

`Runtime.ExtractionThreadCount` is an independent nullable integer `0..16`, with
saved default preference 1. It affects the next preview-thumbnail image
extraction build. A positive value smaller than the available logical processor
count emits the decoder thread argument; zero or a value at least that count lets
the decoder select automatically. Each multi-pass build freezes one preference.
The saved preference and actual argument selection are different facts.

The inspected native consumer is image extraction, rather than subtitle or font
extraction. Goby's existing subtitle/font extraction and unrelated background
clip execution retain their own controls. Native image HTTP requests in the
isolated reference returned 500 before an extraction command, so its successful
image parameter application is not claimed; the consumer rule is supported by
the inspected native implementation and verified against actual Goby previews.

## Existing-codec profile ceilings

`Runtime.CodecProfileLimits` is a complete nullable object with `H264` and `HEVC`.
H.264 values are `baseline`, `main`, and `high`; HEVC values are `main` and
`main10`. Defaults `high` and `main10` allow the full existing executable range.
The limit filters new encoding candidates against the client's allowed formats
and profiles. It does not add profiles, force a particular output, or restrict
unchanged source playback/copy. Already admitted encoding tuples remain fixed.

## ASS text negotiation

`Runtime.SubtitleOptions` is a complete nullable object containing
`AllowAssToText`. The Goby default is true to preserve its existing negotiation;
the inspected stock Emby default is false. When false, new ASS/SSA playback
negotiation avoids style-losing SRT/VTT candidates and uses eligible native-style
or burn delivery instead. It is a preference, not an authorization gate for
explicit SRT/VTT conversion requests. Existing admitted subtitle delivery remains
fixed, and source ASS styling is retained on native delivery.

The native observation proves the known-duration VTT candidate preference and
successful explicit conversion GETs under both values. Goby's existing SRT/VTT
and dynamic negotiation coverage is its own bounded service contract; this is not
a claim of complete native subtitle-editor equivalence. Appearance, OCR,
high-frame-rate burn and FFmpeg tool/environment editors are outside the selected
increment.

## Native reference boundary

The native reference is the official Emby SDK 4.10.2.0 declaration plus observed
stock Emby Server 4.9.5.0 encoding editor responses. The relevant static native
properties agree between the inspected SDK snapshots. Dynamic editor values and
factory values are attributed to the observed server version.

The product owner selected the existing CPU tone algorithms, validated x264
subset, automatic deinterlacing, and current encoder/hardware paths. Their broader
native alternatives are excluded from this increment rather than repeated pending
requirements. HLS buffer scheduling and ASS negotiation retain the implementation
boundaries described above. Unsupported options are rejected rather than stored
as inactive placeholders. Goby's administrator runtime contract does not implement
Emby's generic `EditObjectContainer` framework.

Native references:

- [EncodingOptions declaration](https://github.com/MediaBrowser/Emby.SDK/blob/45e73012eddf985dcfe878ca031b500085e16496/Documentation/reference/pluginapi/MediaBrowser.Model.Configuration.EncodingOptions.html)
- [CodecConfiguration declaration](https://github.com/MediaBrowser/Emby.SDK/blob/45e73012eddf985dcfe878ca031b500085e16496/SampleCode/RestApi/Emby.ApiClient/Emby.ApiClient/Model/CodecConfiguration.cs)
- [Official transcoding guide](https://emby.media/support/articles/Transcoding.html)
- [FFmpeg 9.0.2 software tone-map implementation](https://github.com/FFmpeg/FFmpeg/blob/n9.0.2/libavfilter/vf_tonemap.c)
