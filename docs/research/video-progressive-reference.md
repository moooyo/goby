# Progressive video PlaybackInfo reference: Emby 4.9.5.0

This bounded M4e study adds **92 independent `video-profile-m4e-*` records**, including **61 complete HTTP captures**, to the official Emby Server 4.9.5.0 reference corpus. It covers 12 PlaybackInfo requests and one progressive Range control. Captured wire bodies total **5,437,154 bytes**. The preceding **736 records** and the existing source SHA-256 remained unchanged; the corpus becomes **828 records** after this extension.

The main positive result is actual progressive fragmented MP4 from a Video/Streaming profile: explicit HTTP, omitted Protocol, and empty Protocol all produced a working `stream.mp4`. The main timing finding is equally significant: a successful copy-video/encode-audio seek to **6.37 seconds** retained video from the previous keyframe at **6.0 seconds**, with no MP4 edit list to remove that preroll. The returned video and audio ended at different presentation times. That reference behavior is evidence to understand, not a precise A/V seeking contract Goby should reproduce.

Captures were made on 2026-09-09, approximately 06:52 through 06:56 UTC. The recorder is [reference-video-profiles.py](../../scripts/test-env/reference-video-profiles.py); only the new prefix's sanitized exports were copied into the [reference fixture directory](../../tests/compatibility/fixtures/reference/emby-4.9.5.0). No Goby code, deployment, database, state, or media was changed.

## Existing source and bounded method

The study reused the owned normal-frame-rate source established by the [HLS reference study](hls-reference.md#independent-normal-frame-rate-control):

```text
/opt/goby-fixtures/hls-reference/Reference HLS Normal (2026).mp4
```

Its parent marker remains `goby-hls-normal-owned-v1`. The file has **967,651 bytes** and SHA-256 **`332bce27f1e97d71d3ef7cffa59d8cf70cbcc51380a6b600e05048107432de4d`**. It contains 15 seconds of 320x180 H.264 at 24 fps, a three-second GOP, and mono 48 kHz AAC. Independent decoding of the original yielded **360 video frames and 720,000 audio samples**, with both presentations starting at zero and ending at 15 seconds. No MKV copy, new media, library, or account was needed.

The original non-fragmented MP4 has movie timescale 48,000, video media timescale 12,288, and audio media timescale 48,000. Its video edit-list media time is 0; its audio edit-list media time is **1,024**. Both edit-list segment durations are **720,000 movie-timescale units**. The first demuxed AAC packet has source PTS -0.021333 seconds, while the first decoded audio presentation sample begins at zero. These are distinct packet and decoded-presentation measurements.

The existing ordinary HLS reference account logged in normally on an independent recorder device. The existing item/source identity was read from the prior captured normal-source item, not guessed. All 12 PlaybackInfo responses issued nonempty, distinct PlaySessionIds. No administrator session or scan was necessary.

The [pinned VideoService inventory](../api/services/VideoService.md) declares progressive GET/HEAD routes, and the [PlaybackInfo inventory](../api/services/MediaInfoService.md) and [TranscodingProfile model](../api/models.md#model-transcodingprofile) supply the profile vocabulary. Every delivery request followed an actual returned same-origin URL. The only explicit URL changes were adding `StartTimeTicks=63700000` to fresh negotiated URLs and adding the HTTP Range header. No manual `Videos/stream.mp4` output-option request was introduced.

Unless a control says otherwise, POST PlaybackInfo used these settings:

| Scope | Settings |
| --- | --- |
| Playback request | Owned user/source, IsPlayback true; DirectPlay and DirectStream false; Transcoding true |
| Copy controls | AllowVideoStreamCopy false, AllowAudioStreamCopy false |
| Selection | VideoStreamIndex 0, AudioStreamIndex 1, SubtitleStreamIndex -1, StartTimeTicks 0 |
| Bitrate | MaxStreamingBitrate 500,000 in both request and DeviceProfile |
| Output profile | Type Video, Context Streaming, Protocol http, Container mp4, VideoCodec h264, AudioCodec aac |
| Size/channel limits | MaxWidth 160, MaxHeight 90, MaxAudioChannels `"2"` |

Original-compatible and video-copy controls used 2,000,000 bit/s and omitted the size constraints. The original-compatible case supplied a matching MP4/H.264/AAC DirectPlayProfile and enabled all delivery methods. HLS profiles substituted TS/HLS, SegmentLength 3, and MinSegments 1.

Each body was limited to 3 MiB, all captured wire bodies to 12 MiB, redirects to three same-origin hops, and HLS manifests to six segments. HTTP timeout was 15 seconds. Media-process output was privately capped at 3 MiB per process and runtime at 40-45 seconds. A first GET 500 permitted exactly one same-URL retry after 200 ms. No failed case was retried indefinitely.

Independent FFmpeg/ffprobe 9.0.1 observations included complete stream/packet metadata, decoded-frame timestamps and sample counts, a strict `-xerror` A/V decode, and strict decoded-video frame hashes. HLS tools read the emitted graph through a private local master. Their graph reads are additional to the retained HTTP-body byte count. No token-bearing URL entered command arguments or console output.

## HTTP, default protocol, and container facts

All four core controls returned PlaybackInfo 200, HEAD 200, and an immediate GET 200. The returned progressive bodies were byte-for-byte identical, each **345,200 bytes**.

| Profile control | Returned endpoint leaf | TranscodingContainer | TranscodingSubProtocol |
| --- | --- | --- | --- |
| Protocol `http`, Context `Streaming` | `stream.mp4` | `mp4` | `http` |
| Protocol omitted | `stream.mp4` | `mp4` | property omitted |
| Protocol empty string | `stream.mp4` | `mp4` | empty string |
| Protocol `http`, Context empty string | `stream.mp4` | `mp4` | `http` |

The native source Container remained `mp4`; SupportsDirectPlay and SupportsDirectStream were false, and SupportsTranscoding true. DefaultAudioStreamIndex was 1. DefaultVideoStreamIndex and DefaultSubtitleStreamIndex were omitted. The returned URL included AudioStreamIndex 1 but did not echo VideoStreamIndex 0. Because this source has only one track of each kind, the matrix does not establish selection behavior among multiple video or audio tracks.

An offline check of all 12 saved PlaybackInfo DTOs and the earlier normal-source native item found that **ContainerStartTimeTicks was omitted everywhere**, not explicitly set to zero. No extra request was made for this check. This does not establish how a nonzero-start source or another container would populate that field, and it must not conflate container start time with decoded audio presentation origin.

The baseline URL contained `VideoCodec=h264`, `AudioCodec=aac`, `VideoBitrate=435707`, `AudioBitrate=64293`, `MaxWidth=160`, `MaxHeight=90`, `TranscodingMaxAudioChannels=2`, and the lower-camel-case flags `allowVideoStreamCopy=false` and `allowAudioStreamCopy=false`. Its TranscodeReasons were `ContainerBitrateExceedsLimit,DirectPlayError`. The issued bitrate values sum to the requested 500,000 ceiling, but are negotiation settings rather than a measured output average.

Returned delivery URLs contained their own `api_key`. The DTO still said `AddApiKeyToDirectStreamUrl: false`; that boolean is not evidence that emitted URLs omit authentication. Media requests added no token header or cookie beyond what the URL already carried.

| Header | Successful progressive GET | HEAD before GET |
| --- | --- | --- |
| Content-Type | `video/mp4` | `video/mp4` |
| Content-Length | absent | `937500` |
| Transfer-Encoding | `chunked` | absent |
| Accept-Ranges | `none` | `none` |
| ETag / Location | absent / absent | absent / absent |
| Cache-Control | absent | `no-cache, no-store, no-transform, must-revalidate` |

HEAD's 937,500-byte estimate equals 15 seconds times the 500,000 bit/s request divided by eight. It was not the actual 345,200-byte result. `Range: bytes=0-1023` returned HTTP **200**, no Content-Range, and the **entire identical 345,200-byte body**. This progressive Range control did not produce a 206 byte prefix. No local-media case redirected.

Evidence: [HTTP baseline PlaybackInfo](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-mp4-info.json), [GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-mp4-get.json), [HEAD](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-mp4-head.json), [Range](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-mp4-range.json), and the `protocol-omitted`, `protocol-empty`, and `context-empty` families.

## Actual output, original delivery, and copy behavior

The normal forced output is fragmented MP4 with three fragments, H.264 at **160x90/24 fps**, and mono 48 kHz AAC. It completely decoded to **360 video frames and 721,920 audio samples**, with strict A/V and frame-hash decodes both exiting 0 and empty error output. There are no video or audio edit lists. The output includes 1,920 more decoded audio samples than the original presentation; a clean decoder exit alone does not establish preservation of priming/padding or original sample count.

Decoded output video begins at PTS **0.083333** and ends at **15.083334**; audio begins at **0** and ends at **15.040000**. These measured track intervals do not have identical boundaries. They are not a perceptual synchronization test, and the format duration of 15.083333 seconds must not replace either track's actual presentation interval.

| Control | Received result | Decoded media / copy evidence |
| --- | --- | --- |
| Original-compatible | DirectStreamUrl `original.mp4`; GET/HEAD 200, exact original 967,651 bytes | 360 frames, 720,000 samples, original edit lists preserved; complete video and audio packet hashes match source |
| Both streams copy allowed, forced output method | TranscodingUrl `stream.mp4`; first GET 500, one retry 200 with 81,920 bytes | Truncated MP4; 25 decoded video frames, zero audio frames; not successful remux delivery |
| Video copy allowed, audio copy disabled | TranscodingUrl `stream.mp4`; immediate GET 200, 965,959 bytes | 360 frames; complete video packet-payload hash sequence matches original; AAC differs and has 721,920 decoded samples |

The original-compatible DTO had all three Supports flags true, a DirectStreamUrl, and no TranscodingUrl. Its original GET/HEAD had matching Content-Length, byte-range support, and an ETag. The copy and mixed controls retained DirectPlay=false, DirectStream=false, Transcoding=true, even when the physical video operation was streamcopy. The URI field and high-level delivery flags must therefore be distinguished from whether an encoder actually changed each track.

The successful video-copy/audio-encode result has five MP4 fragments and no edit lists. Its video presentation is 0 through 15.0 seconds, while audio is 0 through 15.040000 seconds. Every video packet payload matched the original sequence, demonstrating copy behavior without creating an MKV fixture or relying solely on the selected codec's name.

Evidence: [original-compatible observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-original-compatible-observation.json), [mixed copy/encode observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-video-copy-audio-encode-observation.json), [baseline decoded media](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-mp4-media-probe.json), and [source probe](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-source-probe.json).

## Non-keyframe seeking and edit lists

Three fresh PlaybackInfo responses were used for the 6.37-second controls. Their returned media URLs were changed only in StartTimeTicks. This source has keyframes every three seconds, so **6.0 seconds is the preceding keyframe**, while the first 24 fps source frame at or after 6.37 seconds is frame 153 at 6.375 seconds.

| Mode at requested 6.37 s | Wire result | Decoded video | Decoded audio | Timing/edit-list observation |
| --- | --- | --- | --- | --- |
| Copy video and audio allowed | 500, then one 200 retry; 81,920 bytes | Only 25 received frames; first hash exactly matches source frame 144 at 6.0 s | No decodable received audio | Truncated mdat; complete initialization has no edit lists |
| Encode video and audio | Immediate 200; 199,524 bytes | 207 frames at 160x90; output PTS 0.083333 through 8.708334 | 416,768 samples; PTS 0 through 8.682666 | Two fragments, no edit lists; no exact source-pixel match is claimed after lossy scaling/encoding |
| Copy video, encode audio | Immediate 200; 578,311 bytes | 216 frames at 320x180; first hash exactly matches source frame 144 at 6.0 s; PTS 0 through 9.0 | 416,768 samples; PTS 0 through 8.682666 | Three fragments, no edit lists; audio ends **317.334 ms before video** |

The successful mixed case provides direct source-position evidence for the video: the decoded first-frame hash equals the original frame at 6.0 seconds, and the first copied video packet also matches source PTS 6.0. The 0.37-second preroll was retained and rebased to output zero, not trimmed or hidden by an edit list. The full encoded-audio packet sequence exactly matches the independently negotiated encode-both 6.37-second control, while the mixed video retains the earlier keyframe.

The source audio is a periodic 440 Hz tone. It cannot independently establish a unique audible source position after lossy re-encoding by waveform similarity alone. The report therefore preserves sample counts, packet equality between the two encoded-audio outputs, decoded presentation times, and the requested seek value, without inventing sample-accurate audible-source alignment. Likewise, frame count and format duration alone are not proof that an encoded first video frame corresponds to the requested source time.

The two copy/copy responses were incomplete, despite HTTP 200 on retry. Their top-level box size exceeded the captured response; only three complete top-level boxes before the invalid media-data box were available. Their initialization moov still showed movie timescale 1,000, video timescale 12,288, audio timescale 48,000, and no edit lists. Strict video frame hashing exited **183**. The combined A/V process returned exit 0 but nonempty stderr and no decoded audio, so it was explicitly **not** accepted as a successful full decode. This demonstrates why exit code alone is insufficient evidence for these vendor responses.

The first 500 responses expose an ordinary public FFmpeg command: `-copyts`, `-start_at_zero`, copy/copy output, `-avoid_negative_ts disabled`, and `-movflags +empty_moov+frag_keyframe`. The seek control places `-ss 00:00:06.370` before input. These are captured error-response facts, not a conclusion from reading vendor implementation. The study does not diagnose why the HTTP handler first returned 500 or why its retry was truncated.

Evidence: [copy non-keyframe observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-copy-start-nonkeyframe-observation.json), [encode-both observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-start-nonkeyframe-observation.json), [successful mixed non-keyframe observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-video-copy-audio-encode-start-observation.json), [its complete packet/frame evidence](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-video-copy-audio-encode-start-media-probe.json), and [final MP4 initialization summary](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-mp4-initialization-summary-final.json).

## Mixed HTTP/HLS profile ordering

With two eligible video output profiles, `[HTTP/MP4, HLS/TS]` selected progressive MP4 and `[HLS/TS, HTTP/MP4]` selected TS/HLS. The HTTP-first response matched the core 345,200-byte output. The HLS-first DTO returned TranscodingContainer `ts` and TranscodingSubProtocol `hls`.

The HLS master and main returned 200, advertised five three-second entries, and all five TS segments returned 200. A real HLS demuxer followed the emitted graph and completed strict A/V and video frame-hash decoding: **360 frames at 160x90/24 fps and 721,920 mono 48 kHz AAC samples**. Its decoded video presentation ran from 10.083333 through 25.083334 seconds; audio ran from 10.062000 through 25.102000. The playlist's 15-second duration and those transport timestamps are different measurements. This is an ordering and playable-graph control, not a new general HLS timeline contract.

Evidence: [HTTP-first observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-http-first-observation.json), [HLS-first observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-hls-first-observation.json), and [complete HLS graph probe](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/video-profile-m4e-hls-first-hls-probe.json).

## Preservation, cleanup, and recorder limitations

The official reference remained PID **3131777** in its existing private network namespace. The recorder entered only that namespace. Goby's separate service retained PID **3288722** before and after the study. No Goby HTTP endpoint or PostgreSQL connection was used. Root filesystem free space was approximately 48 MB; all new capture and probe files were in:

```text
/dev/shm/goby-emby-reference/runtime/video-profile-m4e
```

Its ownership marker is `goby-video-profile-m4e-owned-v1`. Private files have mode 0600 and directories mode 0700. This capture's probe-input directory was removed after cleanup and audit; raw JSON, exact base64 wire bodies, credentials, and sanitized exports remain private in the marked tmpfs tree. They are volatile across a host reboot.

Every scoped ActiveEncodings deletion and the final device-only deletion returned an empty 204, as did logout. Final runtime observation found **no reference child process and zero transcoding-cache files/bytes**. Cleanup was limited to issued IDs and the independent recorder device; no media, library, account, or unrelated service was removed.

The final remote audit verified all **1,472 preceding raw/export files**, preserving the 736 old records, and rehashed the immutable source. Every new export matched deterministic sanitization of its raw original. Header structure and non-secret values, JSON types, exact wire reconstruction and hashes, non-HEAD Content-Length, binary credential absence, and the wire budget were checked. Raw URLs and credentials were not copied to the repository.

Two recorder-postprocessing corrections are retained transparently:

- The first copy/copy retry contained a truncated MP4. The initial strict box observer raised after the complete HTTP body had already been captured and the case had been cleaned up. Only the retained bytes were reprocessed; no HTTP request was repeated. The observer now records parseError while retaining complete initialization boxes before the invalid mdat. Both the failure and resulting media limitations remain visible.
- An initial derived initialization summary counted `System/Info/Public` because its filename ended with `-info`. The final summary selects the actual PlaybackInfo request route and correctly reports **12 nonempty unique PlaySessionIds**. The earlier derived record is retained but superseded for identity counts; no captured HTTP response was altered.

For a new capture, normal stage order is `setup`, `core`, `remaining`, `seek_mixed`, `initialization`, `finish`. The one-off `recover_copy` stage exists only to finish the documented retained-byte postprocessing correction. Completed cases are skipped without requests; incomplete captures refuse automatic repetition. The completed audit can be repeated through SSH:

```powershell
ssh test-env 'pid=$(systemctl show goby-emby-reference.service -p MainPID --value); nsenter -t "$pid" -n python3 /opt/goby-test/repository/scripts/test-env/reference-video-profiles.py audit'
```

The private audit summary remains at `video-profile-m4e/private/audit-summary.json`. No local test, build, validator, runtime probe, commit, or push was performed. Alternate video containers/codecs, multiple-track selection, visual/audio source identification after lossy encoding, arbitrary client players, and a successful full copy/copy progressive delivery remain outside the established positive contract.
