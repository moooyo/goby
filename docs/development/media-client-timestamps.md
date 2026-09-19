# Media client timestamp mapping

Research date: 2026-09-19. Status: **static evidence and implementation design;
browser acceptance is not complete**. This note supports phase 1 of the
[approved media plan](../planning/amd-media-compatibility-plan-20260919.md).
No client JavaScript was executed and no media, browser, build, or runtime test
was run for this research.

## Current hosted client evidence

The official [Web application entry point](https://app.emby.media/index.html)
declared `data-appversion="26.0.26"` when read. These are current hosted client
sources, not the retained Emby Server 4.9.5.0 package. Each JavaScript resource
is minified onto line 1, so the function names below identify the relevant
sections more precisely than a line range.

| Resource | Relevant functions | SHA-256 of retrieved UTF-8 text |
| --- | --- | --- |
| [playbackmanager.js](https://app.emby.media/modules/common/playback/playbackmanager.js) | `createStreamInfo`, `changeStream`, `getCurrentTicks`, `reportPlayback` | `8edefd911195489e32c13ea94b8c5640c6df534f512fe85d2176c27753e5a7c9` |
| [basehtmlplayer.js](https://app.emby.media/modules/htmlvideoplayer/basehtmlplayer.js) | `currentTime`, `getRanges`, `onStartedPlaying` | `8c61400307b6a13a668731bbc086b5673538f9aaee261e7b8d2bb49403a6e531` |
| [htmlvideoplayer/plugin.js](https://app.emby.media/modules/htmlvideoplayer/plugin.js) | `setCurrentSrc`, `onTimeUpdate` | `40e5725943fc6daff0318b9b76b481d0cc91213039c4cb56aded3da1582c2f30` |

The playback manager passes the original requested position into
`createStreamInfo`, both for initial playback and for a stream change. For
progressive transcoding it clears `playerStartPositionTicks` and normally uses
that requested position as its internal `transcodingOffsetTicks`. It does not
derive this offset from the returned URL's `StartTimeTicks`. When the returned
URL contains `CopyTimestamps=true`, it leaves the offset at zero instead.
`getCurrentTicks` adds this internal offset to the local player's time before
the ordinary playback report is built. This establishes an actual reader for
the standard URL flag, not for a `TranscodingOffsetTicks` response property.

The base HTML player obtains its updated position from the media element and
returns milliseconds; buffered and seekable ranges use the same internal
offset. The video plugin uses `playerStartPositionTicks` for initial seeking.
These readers do not use `ContainerStartTimeTicks`, `StreamStartTimeTicks`, or
the Goby-specific actual-start response headers. Static code inspection cannot
establish how a particular browser presents a nonzero fragmented-MP4 timeline.

## Pinned 4.9.5.0 package client evidence

The retained package was inspected separately through `ssh test-env`, with
read-only access limited to static files under:

```text
/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/dashboard-ui
```

Its package version and original package/binary hashes are recorded by
[prepare-core-av-original-client.py](../../scripts/test-env/prepare-core-av-original-client.py).
No product HTTP request, application process, source JavaScript, test, or
service command was executed; `programdata` was not read. The package entry
point does not contain the hosted client's literal `data-appversion` value.

| Static package file | SHA-256 of file bytes |
| --- | --- |
| `index.html` | `24e6f8713b7e00af9dade4a5eeff707f9dd0ea9d472549327a119a3bea98c9ff` |
| `modules/common/playback/playbackmanager.js` | `d54d4ca1a88382a600980373012e6b1ecddb5bb325af037d4a37a1c5370c0d79` |
| `modules/htmlvideoplayer/basehtmlplayer.js` | `b3864a5dbebd14ed6d5e858fded2ff87f5c66119af4d3ca3475f82219482f848` |

These minified JavaScript files are also single-line resources. Independent
inspection found the same relevant behavior in this older client:
`createStreamInfo` assigns the requested position to the progressive offset
unless the returned URL contains `CopyTimestamps=true`; `getCurrentTicks` adds
the offset to player time; `BaseHtmlPlayer.currentTime` obtains updated time
from the media element. Initial playback and `changeStream` pass the original
requested position, not a parsed returned URL position. Neither inspected file
reads the three proposed response metadata fields above. This is separate
static evidence for the pinned client, not an assumption transferred from
26.0.26 and not browser acceptance of a nonzero fMP4 timeline.

## Retained API and reference evidence

- The pinned [SDK snapshot](../sources/emby-sdk-openapi.snapshot.json), line
  33515, declares the `CopyTimestamps` query flag and describes its relationship
  to an offset; its documented default is false.
- The same snapshot declares `MediaSourceInfo.ContainerStartTimeTicks` at line
  72162 without a behavioral description. `MediaStream.StreamStartTimeTicks`
  at line 72368 describes the probed `start_time` value. Neither declaration
  defines a response channel for a newly aligned conversion origin.
- Neither retained OpenAPI snapshot nor the 4.9.5.0 reference fixture corpus
  declares or captures `TranscodingOffsetTicks` as a response field.
- The [progressive reference study](../research/video-progressive-reference.md)
  records that all 12 saved PlaybackInfo responses omitted
  `ContainerStartTimeTicks`. Its 6.37-second mixed copy/encode control retained
  source video from 6.0 seconds and reset output video PTS to zero, without an
  edit list. That observation proves preroll retention; it does not prove that
  a client corrected the resulting 0.37-second offset. The study explicitly
  does not claim arbitrary client-player acceptance.

## Consequences for implementation

Changing only `TranscodingUrl.StartTimeTicks` to an earlier keyframe does not
correct the inspected client's offset. For example, a request for 6.37 seconds
with output zero representing source 6.0 seconds would still initially be
reported near 6.37 seconds. A private alignment opt-in or an exposed HTTP
response header does not establish consumption by an ordinary Emby client.
The server must not invent a response property or reinterpret source probe
metadata to disguise that difference.

The standard `CopyTimestamps=true` flag provides a supported design direction:
the media output must preserve the logical source timeline, and the returned
URL must tell the client to avoid adding the requested position again. This is
an inference from the documented flag and the inspected consumer, not a passed
end-to-end result. Logical source time must account for the verified container
origin; arbitrary raw input timestamps are not automatically API positions.

The implementation and acceptance gates for that direction are:

1. Preserve both tracks on the same logical source clock after a copied seek.
   The selected random-access origin, output timestamps, URL, and registered
   plan must agree. Reauthorization and a later URL request must preserve the
   chosen timestamp mode.
2. Emit `CopyTimestamps=true` only for output that actually has that contract.
   Keep rebased precise encoding and timestamp-preserving copying distinct.
   A query flag alone must never claim a transform the runner did not perform.
3. Verify a real client at a non-keyframe forward and backward seek: compare
   decoded source content, media-element time, displayed position, and reported
   `PositionTicks`. Include audio synchronization, subtitles, track changes,
   pause/resume, and persisted resume position.
4. Confirm each supported client's behavior. The current Web source does not
   prove that every native Emby client implements the same timestamp mapping.

Until these gates pass, automatic aligned output has not established client
acceptance. Existing `X-Goby-Start-Time-Ticks` and `X-Goby-Seek-Aligned` headers are
diagnostic signals for clients that explicitly consume them; they are not a
substitute for the standard client timeline contract.
