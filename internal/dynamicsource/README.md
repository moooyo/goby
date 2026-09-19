# Dynamic source ownership

`Manager.Acquire` exclusively transfers one authorized upstream connection to an
`Input`. A reconnect uses `Acquire` again after the previous input is closed. Its
`Generation` increases and its `Stamp` changes; no connection is spliced into an
existing reader. Publication must fence every producer and subtitle fetch by that
generation and explicitly establish a new timestamp epoch on reconnect.

Dynamic HLS planning prefers video encoding with forced closed GOP boundaries,
while retaining compatible copied audio. This preference never grants encoding
permission: remux-only users and requests disabling transcoding can still choose
copy output. Each copied video segment must then pass a bounded first-packet
restart check before publication. A container key flag, HEVC CRA picture, or
open-GOP access point alone does not establish independent replay. Copied output
does not promise universal restart support for every live source.

`Input.OpenPipeSet(1)` provides the media reader. `OpenPipeSet(2)` provides the
media reader at `Readers[0]` and an independent bitmap subtitle reader at
`Readers[1]`. Both start at byte zero of that generation, including the probed
prefix. They consume one ingress connection through separate anonymous pipes.
Readers cannot be added after consumption starts. Reopening a live pipe descriptor
does not create an independent input and must not be used instead of this API.

The two-reader journal defaults to 8 MiB per group and a 64 MiB aggregate manager
quota, excluding small fixed pump buffers and OS pipe buffers. It stores decoder
lag only, not media history. A full journal applies backpressure. A reader that
cannot complete a bounded write within 10 seconds fails the whole group with
`ErrFanoutStalled`; another reader is never advanced by dropping bytes. Transport
or consumer errors fail the group with `ErrUnavailable`. `Input.Close` cancels and
joins ingress and egress, `PipeSet.Close` additionally closes recipient-owned read
descriptors, and `PipeSet.Wait` reports completion. `Manager.Close` includes the
same pump lifetime in its shutdown accounting. The single-reader `OpenPipe` API
remains available and has no shared-journal allocation.

These pipes do not create subtitle events or advance sparse bitmap events. A live
bitmap decoder still requires a media-clock heartbeat, an initial transparent
state, and correct clear/EOF handling in the transcoder. In particular, a second
demuxer that discards all audio/video packets may lack the sub2video heartbeats
needed between subtitle cues. Source fanout alone is not proof of working live
bitmap composition.

## Declared external text subtitles

`Definition.Subtitles` contains at most eight trusted operator declarations.
The closed formats are `webvtt`, `subrip`, and `ass`. `document` identifies a
bounded finite resource. `webvtt-stream` and `webvtt-hls` require WebVTT and identify
an incremental response or a rolling subtitle media playlist respectively.
`Clock` is mandatory: `media` uses source media timestamps; `mpegts` requires an
explicit WebVTT `X-TIMESTAMP-MAP`. `OffsetTicks` declares source alignment in 100 ns
ticks, before any viewer offset. No wall-clock or URL-based timing is inferred.

Finite `document` declarations must omit `segmentClock` and `streamWatermarks`.
The server parses the complete response within an 8 MiB input limit. An endless
response must use one of the explicit completeness protocols below instead.

### WebVTT HLS segment-start contract

`webvtt-hls` requires `clock: "mpegts"` and
`segmentClock: "timestamp-map"`. This is an operator assertion that the LOCAL
anchor in **each segment's own** `X-TIMESTAMP-MAP` is the beginning of that
segment's complete interval. `EXTINF` supplies its duration. A global
`EXT-X-MAP` alone cannot establish a new segment's beginning; headerless segments
and segments without their own timestamp map are rejected.

```json
{
  "id": "english-hls",
  "format": "webvtt",
  "mode": "webvtt-hls",
  "url": "https://captions.example/live/english.m3u8",
  "clock": "mpegts",
  "segmentClock": "timestamp-map"
}
```

For a two-second segment whose LOCAL interval starts at ten seconds, its body
can begin as follows. Cue intervals can cross the segment boundaries and must
remain complete, as required by HLS.

```text
WEBVTT
X-TIMESTAMP-MAP=LOCAL:00:00:10.000,MPEGTS:990000

00:09.000 --> 00:12.000
A caption crossing the segment boundary.
```

The fetcher reports the LOCAL start and actual `EXTINF` duration only after the
whole segment has been parsed. The bridge maps both endpoints against a measured
media anchor in the current generation and checks continuity with an explicit
tolerance. A discontinuity requires a new generation or rejection. Sequence
numbers, an ordinary LOCAL zero, and wall-clock passage cannot establish a
source-media interval without this declared contract.

### Incremental WebVTT watermark contract

`webvtt-stream` requires `streamWatermarks: "goby-note-v1"`. This Goby operator
protocol adds a single-line NOTE block declaring that every cue before the stated
LOCAL or media timestamp has already been delivered:

```json
{
  "id": "english-stream",
  "format": "webvtt",
  "mode": "webvtt-stream",
  "url": "https://captions.example/live/english.vtt",
  "clock": "media",
  "streamWatermarks": "goby-note-v1"
}
```

```text
WEBVTT

00:00:01.000 --> 00:00:02.000
A complete caption.

NOTE GOBY-WATERMARK 00:00:03.000

```

Watermarks use `HH:MM:SS.mmm` with at least two hour digits, are nondecreasing,
and are bounded to 30 days within a source generation. Identical repeated
watermarks are inert; backward, multiline, malformed, and out-of-range markers
are rejected. Generic NOTE blocks and marker-like caption text do not advance
completeness. EOF on an incremental stream does not turn it into a complete
finite document. A stream without valid barriers cannot establish empty subtitle
intervals merely because media continued or a read timed out.

`mpegts` mode still requires its own WebVTT map and a matching measured media
anchor; the raw 33-bit MPEGTS value is not automatically assigned an epoch.
For `media` clocks, the bridge uses the encoder's measured source-origin policy,
including its one-second output headroom, before applying the declared offset.

Requests disallow redirects, proxies, and compressed representations. HLS child
resources must be safe same-origin relative references; nested master playlists,
encryption, and byte ranges are not followed. Playlist count, response bytes,
poll downloads, startup time, idle reads, and cancellation are bounded. These
protocols do not add provider-account integration or online provider acceptance.

Public lease information contains the subtitle codec, language, title,
default/forced flags, a reserved external stream index, and an opaque tag. It
never contains the private URL or headers. `SubtitleTag` binds the random lease
identity and the complete immutable declaration, including its clock and request
settings. It is an operator-declaration identity, not a digest of fetched subtitle
bytes. The tag stays stable across reconnects of the same lease so a fixed HLS
track set can retain its buffered window.

The server resolves a declaration with
`Manager.Subtitle(ctx, owner, leaseID, generation, streamIndex, tag)`. This repeats
current catalog authorization and returns a defensive copy only if the owner,
track binding, and current generation match. A stale generation returns
`ErrStaleGeneration`. The caller must keep that fence while fetching and before
publishing cues; obtaining a declaration once does not authorize publishing data
into a later generation. Fetching, cue parsing, epoch alignment, bounded retention,
and HLS publication belong to their respective server/transcode components.

The journal does not retain raw program history for rerendering. A retained
bitmap-burned presentation remains immutable when a viewer starts another
presentation with a different bitmap selection or with subtitles disabled.
