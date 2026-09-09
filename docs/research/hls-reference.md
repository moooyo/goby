# HLS reference capture: Emby Server 4.9.5.0

This bounded M4a study establishes successful H.264/AAC HLS segment delivery for an owned normal-frame-rate source: four requested segments returned HTTP 200 and passed independent ffprobe and full-segment decoding. It also confirms negotiation, playlist construction, seek hints, segment route shapes, and cleanup responses. Streamcopy delivery and the earlier sparse-source controls still failed with HTTP 500; those failures remain separate from the positive transcode evidence.

The 94 new `hls-m4a-*` records comprise 84 HTTP captures, four remote media probes, and six runtime observations. The initial 53 records were preserved before a separate 41-record normal-frame-rate control. They are stored alongside the earlier reference fixtures in [`tests/compatibility/fixtures/reference/emby-4.9.5.0`](../../tests/compatibility/fixtures/reference/emby-4.9.5.0). This is reference-server evidence, not a Goby or real-client compatibility result.

## Scope and provenance

The server is the same official Linux amd64 Emby 4.9.5.0 release described in [reference-server.md](reference-server.md). Its systemd service retained `PrivateNetwork=yes`, and every request ran through `ssh test-env` and `nsenter` into that namespace. The existing Goby service, other reference users, media bytes, library options, and old fixture records were not changed.

A new ordinary account, `reference-hls-m4a`, and device `goby-hls-m4a-recorder` isolate the new sessions. Its random password and token remain in the mode-0600 remote file `private/hls-m4a-credentials.env`. No playback Started/Progress/Stopped reports were sent, so this study did not change watched or resume state.

The initial source was the already owned `Reference Playback M3.mp4`, item `"28"`, source `"mediasource_28"`: 600 seconds, 56379 bytes, H.264 Constrained Baseline at 160x90 and 1 fps, mono 8 kHz AAC. Its SHA-256 remains `997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3`. Its sparse keyframes and unusually low reported audio bitrate matter to the limitations below. The later independent normal-frame-rate source is described in its own section.

The capture script is [reference-hls.py](../../scripts/test-env/reference-hls.py). It imports the existing recorder without modifying it, uses a separate ownership-marked tmpfs scratch directory, caps each HTTP body at 1 MiB, caps a capture stage at 8 MiB, and audits a 30 MiB cumulative wire budget. Actual captured HTTP bodies totalled **652822 bytes**, including 236771 bytes from the initial phase and 416051 bytes from the normal-source control. No dependency was installed.

## Published contract baseline

The pinned official [Emby.SDK Swagger file](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json) supplies these API-relative routes; the usual base is `/emby`:

| Method | Route | Documented request fields |
| --- | --- | --- |
| POST | `/Items/{Id}/PlaybackInfo` | JSON `PlaybackInfoRequest`, including one `DeviceProfile` object |
| GET / HEAD | `/Videos/{Id}/master.m3u8` | Streaming query parameters |
| GET | `/Videos/{Id}/main.m3u8` | Streaming query parameters |
| GET | `/Videos/{Id}/live.m3u8` | Streaming query parameters; not exercised here |
| GET / HEAD | `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | Four required string path parameters |
| GET | `/Videos/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}` | Four required string path parameters; not exercised here |
| DELETE | `/Videos/ActiveEncodings` | Required string query `DeviceId`, `PlaySessionId` |
| POST | `/Videos/ActiveEncodings/Delete` | The same query fields; no body declared |

The SDK therefore **does declare segment route templates**. The earlier statement in [playback-and-transcoding.md](playback-and-transcoding.md) that templates were not specified is too broad. What the SDK does not fully define is the manifest grammar, segment-number interpretation, output timeline, readiness behavior, and ownership lifetime.

PlaybackInfo and HLS use int64 `StartTimeTicks`, not the subtitle endpoint's `StartPositionTicks`. `PlaybackInfoRequest` also declares `AllowVideoStreamCopy` and `AllowAudioStreamCopy`. HLS query documentation describes `EnableAutoStreamCopy` as defaulting to true and `CopyTimestamps` as defaulting to false, although the schema does not set JSON `default` values. TranscodingProfile `SegmentLength`, `MinSegments`, `MaxWidth`, and `MaxHeight` are int32; its `MaxAudioChannels` is a string.

The generated URLs demonstrate gaps in that schema: they use `SegmentContainer=ts` without the SDK's required `Container`, and include `MediaSourceId`, `PlaySessionId`, `SegmentLength`, `MinSegments`, `BreakOnNonKeyFrames`, `TranscodingMaxAudioChannels`, and `TranscodeReasons`. Actual URL spelling and casing should be preserved when following a server-generated URL rather than reconstructed solely from the incomplete parameter list.

## Initial 600-second source: negotiation controls

All three POSTs supplied the dedicated user, the known media source, `IsPlayback=true`, direct play and direct stream disabled, transcoding enabled, `StartTimeTicks=5700000000`, `AudioStreamIndex=1`, and `SubtitleStreamIndex=-1`. Each profile used no DirectPlayProfiles and one TS/HLS streaming TranscodingProfile with SegmentLength 3, MinSegments 1, MaxAudioChannels `"2"`, and maximum streaming bitrate 200000.

| Control | Requested output and copy flags | Observed URL difference |
| --- | --- | --- |
| `remux` | H.264/AAC; video and audio copy allowed | No explicit copy prohibition |
| `audio` | H.264/MP3; video copy allowed, audio copy disabled | `AudioCodec=mp3`, `allowAudioStreamCopy=false` |
| `video` | H.264/AAC; video copy disabled, audio copy allowed; MaxWidth 80 | `MaxWidth=80`, `allowVideoStreamCopy=false` |

All returned HTTP 200, `SupportsDirectPlay=false`, `SupportsDirectStream=false`, `SupportsTranscoding=true`, a PlaySessionId, and a TranscodingUrl. No ErrorCode or entitlement denial was returned. Their common URL structure was:

```text
/videos/28/master.m3u8?
DeviceId=...&MediaSourceId=mediasource_28&StartTimeTicks=5700000000&
PlaySessionId=...&api_key=...&VideoCodec=h264&AudioCodec=...&
VideoBitrate=199747&AudioBitrate=253&AudioStreamIndex=1&
TranscodingMaxAudioChannels=2&SegmentContainer=ts&SegmentLength=3&
MinSegments=1&BreakOnNonKeyFrames=False&
TranscodeReasons=ContainerNotSupported,DirectPlayError
```

The actual fixture contains one continuous URL; the line breaks above are illustrative. The tiny AAC source caused the reference to negotiate `AudioBitrate=253` even for MP3 conversion. A later public error log warned that this value was extremely low. This synthetic result is not a recommended production bitrate.

Evidence: `hls-m4a-{remux,audio,video}-playbackinfo.json`.

## Master and media playlists

Every captured master and main GET returned HTTP 200 with `Content-Type: application/vnd.apple.mpegurl`. The master returned one variant with an `#EXT-X-INDEPENDENT-SEGMENTS` declaration and a relative `main.m3u8` URI carrying the complete query, including the API token and PlaySessionId. The remux/audio masters omitted CODECS; the video master included `CODECS="avc1.640029,mp4a.40.2"`.

All main playlists in this initial phase described the complete 600-second VOD, using 200 entries of three seconds and absolute-in-program segment indices 0 through 199. The final entry was followed by `#EXT-X-ENDLIST`. The common leading lines were:

```m3u8
#EXTM3U
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-START:TIME-OFFSET=570
#EXTINF:3.0000, nodesc
hls1/main/0.ts?PlaySessionId=<reference-play-session>
```

The emitted segment URI had **only PlaySessionId**, with no api_key. The recorder resolved the child path against the main URL without inheriting its parent query, adding a token header, or using cookies. These initial requests reached the encoder path but returned HTTP 500. That result alone is not proof of successful segment authorization. The normal-source control below later obtained 200 using the same credential-carrying shape; it still does not establish every policy combination.

The master/main responses were generated successfully even though no segment could later be delivered. They are not evidence that output files are ready. The video master advertised 80x45, while the public FFmpeg error log described actual scaling to 80x44 for the 4:2:0 encoder. That mismatch is another reason not to infer decoded output dimensions or codecs solely from the master.

Full master and main bodies, including every segment URI, remain in the corresponding fixtures. Token values are redacted without shortening the stored manifest structure or dropping lines; original Content-Length headers describe the original wire bytes, not the sanitized text's length.

Fixture notes written during exploration describe the recorder's initial intent, not additional observations. In particular, the initial "last 30 seconds" wording did not prove that StartTimeTicks truncated the manifest. Some generic master notes say only StartTimeTicks changed, while the explicitly named dimension control also changed Width/Height/MaxWidth/MaxHeight. Query reserialization encoded the comma in TranscodeReasons as `%2C`; the generated negotiation URL and the actual request are both retained, so byte identity between those two URLs is not claimed.

## StartTimeTicks and seek behavior

StartTimeTicks controlled an HLS starting-position hint, **not a shorter playlist**. For the same video play session, forward and backward controls changed StartTimeTicks from 570 seconds to 580 and then 560. The main playlist retained the same 200 entries, MediaSequence 0, and segment URI numbering; only `#EXT-X-START:TIME-OFFSET` changed accordingly.

| Requested offset | Selected manifest entries | Input seek shown by the public segment-error log |
| --- | --- | --- |
| 570 seconds | 190 and 191 | 570 and 573 seconds |
| 580 seconds | 193 and 194 | 579 and 582 seconds |
| 560 seconds | 186 and 187 | 558 and 561 seconds |

The recorder selected the entry whose accumulated EXTINF interval contains the desired position. This exercised the two adjacent entries at the desired seek point rather than presuming that entry zero represented the requested offset. The observed encoder input seeks align with the entry's three-second timeline, not necessarily the exact requested position inside that entry. Decoder PTS, A/V alignment, and precise playback seek accuracy remain unverified because the HTTP responses did not contain media.

An initial control had selected literal entries 0 and 1 from the complete playlist despite StartTimeTicks=570. The reference performed a very fast copy/copy remux across the source; its error log reported all 600 frames and no decoding/re-encoding. Those failed requests are retained as `hls-m4a-remux-segment-{0,1}.json`, not rewritten as tail-segment observations. The recorder was then corrected to use manifest offsets. Every audio/video encoding request selected a tail entry, so no full 600-second video/audio encoding was requested by those controls. Keyframe seeking during remux could retain the prior keyframe; the 570-second remux error log reported 60 copied frames from this source's long GOP.

Evidence: `hls-m4a-remux-main.json`, `hls-m4a-remux-tail-*`, `hls-m4a-seek-forward-*`, and `hls-m4a-seek-backward-*`.

## Segment failure and the limit of codec evidence

All 14 segment attempts made before cleanup returned HTTP 500 with text beginning `Error starting ffmpeg`, followed by a public FFmpeg command and its execution log. The logs showed:

- Remux: video `copy`, audio `copy`, H.264 Annex B conversion, MPEG-TS segmentation.
- Audio conversion: video `copy`, AAC decoded and encoded through `libmp3lame`.
- Video conversion: H.264 decoded, scaled to 80x44, encoded through `libx264`, with audio copied.

The reference package's reported FFmpeg version was `5.1-emby_2023_06_25_p4`, distinct from the separate FFmpeg 9.0.1 toolchain available for later independent probing. No vendor implementation was read, modified, or reverse engineered; these details came from the ordinary HTTP error response.

The logs reached normal output/segment-completion and EXIT messages without an explicit missing-dependency or paid-feature denial. The cause of the HTTP failure remains undetermined. The very small source might expose completion/readiness timing, but this is only a hypothesis. One bounded control requested 1920x1080 dimensions to try increasing encoding work; the reference retained 160x90, so that control did not actually test the hypothesis. It also returned 500, and no additional expansion was made.

No successful binary response was downloaded in the initial 53-record phase, so that phase produced no ffprobe or decode evidence. The failure logs distinguish requested conversion paths, but they do not establish that a client received usable MPEG-TS, valid decoder initialization, accurate timestamps, or uninterrupted playback. The separate normal-source control below provides a bounded positive transcode result.

For implementation context, upstream FFmpeg documents that streamcopy transfers encoded packets without decoding or encoding, while transcoding invokes codecs. Its HLS muxer normally cuts at a subsequent keyframe; claiming independent segments requires a keyframe boundary, and splitting on non-keyframes can impair seeking. These explain the implementation concerns but do not diagnose this reference failure. [FFmpeg streamcopy documentation](https://ffmpeg.org/ffmpeg-doc.html#Streamcopy), [FFmpeg HLS muxer documentation](https://ffmpeg.org/ffmpeg-formats.html#hls).

## Cleanup, isolation, and audit

Both forms were captured with the dedicated DeviceId and the exact issued PlaySessionId:

```text
DELETE /emby/Videos/ActiveEncodings?DeviceId=...&PlaySessionId=...
POST   /emby/Videos/ActiveEncodings/Delete?DeviceId=...&PlaySessionId=...
```

Every cleanup returned HTTP 204 with an empty body, including repeated final cleanup. Four bounded requests to the same previously emitted segment URLs after cleanup returned HTTP 404, with no new master/main request in between. This supports that the active segment context was removed. It does not prove cancellation latency for a long-running encoder: the failing workers had already exited.

The initial phase's final runtime observation found no direct child process of the reference service, zero files/bytes under its transcoding-temp directory, matching requester/reference network namespaces, 79654912 root filesystem bytes free, and 1284796416 tmpfs bytes free. Only the ownership-marked temporary probe-input directory was eligible for manual removal; other caches, services, and fixtures were untouched. Private wire originals remain under the owned tmpfs directory for later audit.

Before creating the new account, the script hashed exactly 342 raw/export fixture pairs. The final remote audit checked all **684** old files byte-for-byte, the source media hash, each new JSON shape/type/number, original response headers, raw text/JSON/binary reconstruction, and Content-Length versus original wire bodies. It scanned all known reference credentials from private credential files, including tokens embedded in M3U8 body URLs. Only sanitized exports were copied to the repository.

The initial script stages are `setup`, `negotiate`, the three delivery controls, the explicit remux-tail correction, one dimension control, seek, and finish/audit. It refuses to overwrite owned fixture names or reuse the existing account setup. All runtime work occurred on `test-env`; no local test, validator, build, or runtime probe was run. Neither source code nor fixture changes were committed by this research task.

## Independent normal-frame-rate control

The authorized follow-up created `/opt/goby-fixtures/hls-reference`, with its own `goby-hls-normal-owned-v1` marker and a mode-0755 root. Its single mode-0644 `Reference HLS Normal (2026).mp4` is 967651 bytes, with SHA-256 `332bce27f1e97d71d3ef7cffa59d8cf70cbcc51380a6b600e05048107432de4d`. Existing FFmpeg generated 15 seconds of synthetic test-pattern video at 320x180/24 fps, H.264 with a three-second GOP, and a synthetic 440 Hz mono AAC track at 48 kHz/64 kbit/s. No existing source changed.

A separate administrator, `reference-hls-m4a-admin`, was created and authenticated through ordinary APIs; its credentials use a separate mode-0600 private file. That administrator created and refreshed only the new `Reference HLS Normal M4a` movie library, using the existing provider-disabled options and `SampleIgnoreSize=0`. Playback remained on the earlier dedicated ordinary HLS user/device. The indexed item is `"43"`, source `"mediasource_43"`.

### Positive H.264 conversion and retained streamcopy failure

Two profiles requested TS/HLS with three-second segments and MinSegments 1. The streamcopy profile allowed both streams to copy; the video profile disabled video copy and set TranscodingProfile MaxWidth 160, while allowing AAC copy. StartTimeTicks 0 and 60000000 were tested independently for each profile, with cleanup between runs.

| Mode and position | Requested segment IDs | HTTP result |
| --- | --- | --- |
| Copy/copy, zero seconds | 0, 1 | Both 500, with public FFmpeg error logs |
| Copy/copy, six seconds | 2, 3 | Both 500, with public FFmpeg error logs |
| H.264 conversion, zero seconds | 0, 1 | Both 200; 86292 and 93436 bytes |
| H.264 conversion, six seconds | 2, 3 | Both 200; 94564 and 89300 bytes |

Each successful segment request had an empty recorded header mapping, no cookie, and a URL containing only PlaySessionId. The master/main had previously been requested with the issued query token. This establishes successful retrieval using the generated segment capability in this configuration. It does not authorize treating arbitrary unknown PlaySessionIds, foreign sessions, or every library policy as public access.

Successful responses used `Content-Type: video/mp2t`, exact Content-Length, `Cache-Control: private, no-transform`, and an ETag. They did not advertise Accept-Ranges. The captured transport bytes were saved as binary-base64 and independently probed/decoded on `test-env`.

All four probe results identified MPEG-TS containing H.264 Main at 160x90/24 fps and AAC LC at 48 kHz mono. Video stream duration was three seconds in each segment, and the first observed video packet had a keyframe flag. Each full downloaded segment decoded with FFmpeg exit code 0 and empty stderr. These checks cover individual complete segments, not a real player's continuous A/V presentation or a complete adaptive session.

The master claimed `avc1.640029`, while the encoded stream's independently detected codec string was `avc1.4d4015`. Goby should derive truthful manifest codec declarations from its own output rather than copy this mismatch.

### Full VOD and observed packet timestamps

Both zero- and six-second main playlists contained all five three-second entries, indexed 0 through 4, with MediaSequence 0, TargetDuration 4, and ENDLIST. At zero there was no EXT-X-START line. At six seconds the only timeline hint addition was `#EXT-X-START:TIME-OFFSET=6`. This reproduces the full-VOD behavior seen in the earlier long source using a normal-frame-rate source and successfully delivered media.

| Requested position / segment ID | First video PTS | First video DTS | Video duration |
| --- | --- | --- | --- |
| 0 / 0 | 10.083333 s | 10.000000 s | 3.000000 s |
| 0 / 1 | 13.083333 s | 13.000000 s | 3.000000 s |
| 6 / 2 | 16.000000 s | 15.916667 s | 3.000000 s |
| 6 / 3 | 19.000000 s | 18.916667 s | 3.000000 s |

These are measured transport timestamps, not zero-based item positions. Adjacent video start PTS values advanced by three seconds within each run. The measurements do not establish a universal ten-second offset, identical encoder delay across seek runs, or exact perceptual A/V synchronization. The ffprobe record includes full stream/format metadata and the first eight demuxed packets; the independent FFmpeg decode processes the complete downloaded segment.

### Failure-path observation and final audit

Before each streamcopy cleanup, the recorder inspected only the output directory explicitly named by that job's public error response. The normal-source remux directory existed and contained the expected `<prefix>_0.ts` through `<prefix>_4.ts` files and its M3U8, despite the HTTP 500 result. For the zero-second run, those TS files were approximately 210-226 KiB each. The public error log and observed filenames agreed. The zero- and six-second runs reused the same PlaySessionId and cache prefix; this observation does not distinguish retained/reused files from newly generated files in the second run or prove that each 204 immediately deleted all cached output. Therefore, an assertion that FFmpeg never started or that the expected output files were simply absent would contradict this evidence. The reason the reference handler rejected the streamcopy job is still unresolved; no vendor code or configuration was changed to bypass it.

All four normal-source cleanup calls, alternating DELETE and POST `/Delete`, returned 204. Repeated final cleanup also returned 204. The final runtime observation again found no direct child PIDs and zero transcoding cache files/bytes. Owned local probe inputs were removed after audit; exact private wire evidence remains. No further streamcopy failure experiments were added.

This extension's **41** records include 32 HTTP responses, four successful media-probe records, and five runtime observations. Its wire bodies total **416051 bytes**, below the eight-MiB bound. The remote audit preserved all **790** files that preceded it: the original 342 fixture pairs plus the first 53 HLS pairs. It also rechecked the original and new source hashes and the original 684-file baseline. The audit now requires every export to equal the deterministic sanitizer applied to its raw record, in addition to header/type/number and credential checks. This verifies that changes inside token-bearing manifest strings are exactly those made by the configured sanitizer.

Supporting stages are `normal_prepare`, `normal_setup`, `normal_negotiate`, `normal`, `normal_audit`, and `normal_finish`. Evidence uses the `hls-m4a-normal-*` prefix and preserves every earlier fixture unchanged.

## Remaining evidence boundaries

The positive result covers four normal-source H.264/AAC conversion segments, including a six-second start position, with [ffprobe metadata/packet inspection](https://ffmpeg.org/ffprobe.html) and full-segment decoding. It does not establish successful copy/copy or audio-only conversion delivery. The reference's HTTP 500 failures must not be reproduced as an intended Goby compatibility feature. Foreign/unknown session authorization, HEAD/Range, live playlists, fMP4 initialization segments, discontinuity, subtitle-in-HLS, cancellation latency while a long worker is active, and real third-party players remain outside this completed bounded capture.
