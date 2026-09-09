# Indexed external text subtitles

This increment serves standalone SRT and WebVTT sidecars for indexed local media.
Embedded-track extraction, ASS/SSA conversion, bitmap burn-in, HLS subtitle
playlists, attachments/fonts, downloads from providers, and unrestricted legacy
encoding detection remain separate media-pipeline work.

## Discovery and stable track identity

A normal media-library scan inspects matching sidecars even when ffprobe data is
cached. Supported names use the media basename and optional language/default/
forced/hearing-impaired suffixes before `.srt` or `.vtt`. Discovery stays in the
same directory and does not follow symbolic links or interpret metadata URLs.
Directory names are indexed once per directory during a scan.

Migration `0009` stores subtitle records separately from the primary probe JSON.
External stream indexes begin above embedded indexes. An existing active filename
retains its index when another sidecar is added. Removed entries leave tombstones;
reappearance or an embedded-index collision allocates a new index, so an old URL
cannot silently identify a different track. Goby allows 32 active sidecars and
4,096 historical identities per item. This deliberately differs from the sampled
reference's reordering after a newly added VTT file.

The scanner validates source bytes and records SHA-256, inode identity, size,
mtime, and Linux ctime. An invalid existing candidate retains its prior record
with a warning; absence is retired only after a complete, stable directory view.
A changed or unavailable source cannot be served using its old snapshot. Existing
libraries need a normal rescan after this upgrade to discover their sidecars;
the upgrade does not automatically schedule one.

## DTOs and negotiation

Authorized item detail and Fields=MediaStreams/MediaSources include external
tracks in both the top-level and source-level stream arrays. Indexes refer to the
complete stream array, not a subtitle-only list. Native wire codecs are `srt` and
`vtt`; the planner internally maps WebVTT to its media-codec spelling.

DeliveryMethod is External and DeliveryUrl is a relative route containing the
current request's API token. Selecting a text sidecar with an External subtitle
profile can choose its native format or SRT/WebVTT conversion, preferring native
delivery when the profile permits both. A converted SRT track still reports
Codec=srt while its URL ends in Stream.vtt, matching the reference. The sampled
external selection omits DefaultSubtitleStreamIndex, which Goby also omits.

No profile leaves device support unverified and permits native external delivery
for an explicitly selected track. Missing or -1 selection does not automatically
enable a sidecar. Embed-only, burn-in, HLS, bitmap, or extraction requirements
cannot silently become external text delivery, including when transcoding is
explicitly disabled. Server conversion capability remains false for video/audio;
text subtitle conversion does not imply a media-transcoding pipeline.

## Download routes and time behavior

GET and HEAD accept both `/Videos` and `/Items`, with or without `/emby`:

```text
/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}
/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}
```

Format is srt or vtt. StartPositionTicks, EndPositionTicks, and CopyTimestamps are
accepted in the query; an explicit query start overrides a valid path start.
Ticks are 100 nanoseconds. Inputs must fit nonnegative int64 values, and duplicate
query values that conflict are rejected.

The reference's window behavior is unusual and is preserved: a cue whose original
start is before StartPositionTicks is discarded, even if it overlaps that point.
CopyTimestamps=false, the default, subtracts StartPositionTicks from retained cue
times; true preserves their times. EndPositionTicks excludes cue starts at or
above that bound **after** any offset. Cue ends are not clipped. Consequently a
start larger than the end parameter is meaningful when timestamps are shifted.

For Start=10 seconds and End=20 seconds:

| Original cue | CopyTimestamps=false | CopyTimestamps=true |
| --- | --- | --- |
| 9..12 seconds | Discarded | Discarded |
| 10..13 seconds | 0..3 seconds | 10..13 seconds |
| 18..22 seconds | 8..12 seconds | 18..22 seconds |
| 20..21 seconds | 10..11 seconds | Discarded |

Complete same-format UTF-8 responses preserve source layout, including BOM and
CRLF. Supported BOM-marked UTF-16 is decoded strictly and emitted as UTF-8.
Unmarked non-UTF-8 inputs are rejected rather than guessed. Windowed SRT output
renumbers cues and preserves their original internal line endings. SRT-to-VTT
uses the observed WEBVTT header, no SRT numbers, short timestamp notation where
appropriate, and final newline spelling. MIME types are text/plain and text/vtt.
Native VTT header/NOTE/STYLE/REGION data is kept separate from caption text; full
style conversion and MPEGTS timestamp-map interpretation are not claimed.

## Access and resource boundaries

Goby requires a valid user token, current playback permission, library access,
and a current primary-media snapshot for subtitle downloads. Every generated URL
includes api_key for clients that fetch subtitles separately. This is an explicit
access-policy difference from the reference, which returned the sampled external
SRT anonymously and with an invalid token. Goby also returns 404 for an unknown
source/index instead of the reference's empty 200 response. These differences
remain part of the published compatibility boundary.

Before any 304 response, the service checks the current account/source and reads
the bounded subtitle through anchored file descriptors, verifying its hash and
snapshot. The ETag hashes the rendered bytes; Last-Modified includes ctime when
later than mtime. Responses require private cache revalidation. Changes require a
rescan, and deleted/tombstoned track URLs remain unavailable.

Source input is capped at 8 MiB, output at 16 MiB, cues at 50,000, and lines at
250,000, with additional line/cue text bounds. Four HTTP slots and four storage
workers bound concurrent work; a canceled storage call retains its worker slot
until blocking work actually finishes. Requests have a 20-second budget. No whole
video hash, arbitrary filename lookup, or FFmpeg process is needed for this text
delivery path.

See [the reference report](../research/reference-server.md) for the recorded
subtitle matrices. WebSocket work in this increment is [research only](../research/websocket-reference.md).
