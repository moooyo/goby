# Audio DeviceProfile PlaybackInfo reference: Emby 4.9.5.0

This bounded M4d study adds **126 independent `audio-profile-m4d-*` records** from the official Emby Server 4.9.5.0 reference on `test-env`. It covers 20 small PlaybackInfo controls, including one response preserved only as an incomplete body observation, and **87 complete HTTP captures**. The preceding **610 records** and all four existing audio-source hashes remained unchanged. Captured HTTP and incomplete-response bodies totaled **1,999,142 bytes**.

The central result is that Audio profiles with `Protocol: "http"`, an omitted Protocol, or `Protocol: ""` all selected progressive delivery. Their returned `TranscodingSubProtocol` values differed: `"http"`, an omitted property, and `""`, respectively. A supported HTTP profile produced a `stream.mp3`, `stream.aac`, or `stream.m4a` endpoint; the original `MediaSource.Container` continued to describe FLAC. These observations do not justify normalizing every reference DTO to `"http"`.

The captures were made on 2026-09-09 between approximately 05:55 and 05:57 UTC. The recorder is [reference-audio-profiles.py](../../scripts/test-env/reference-audio-profiles.py); evidence lives in the [reference fixture directory](../../tests/compatibility/fixtures/reference/emby-4.9.5.0). No Goby source, deployment, database, user state, existing library configuration, or media content was changed.

## Method and boundaries

The study reused the owned account, library, and immutable sources described in [the universal-audio study](audio-reference.md). The active inputs were the six-second FLAC source, at 96 kHz/stereo/24-bit, and the six-second MP3 source, at 44.1 kHz/stereo. AAC and WAV hashes were protected, but no additional PlaybackInfo claims about those input formats are made. No new media, library, or account was created. Ogg, ALAC, and Matroska input behavior was outside this matrix.

GET controls used the ordinary authenticated `Items/{Id}/PlaybackInfo` operation with only `UserId`. The profile controls used POST JSON because the pinned operation declares `DeviceProfile` in its request model; the recorder did not invent a nested-profile query serialization. Model names, properties, and the `Streaming`/`Static` context values come from the [pinned model inventory](../api/models.md#model-transcodingprofile), including [EncodingContext](../api/models.md#model-encodingcontext) and [ProfileConditionValue](../api/models.md#model-profileconditionvalue).

Unless a row says otherwise, the POST request used:

```json
{
  "UserId": "<owned-user>",
  "MediaSourceId": "<owned-source>",
  "IsPlayback": true,
  "EnableDirectPlay": false,
  "EnableDirectStream": false,
  "EnableTranscoding": true,
  "AllowAudioStreamCopy": false,
  "MaxStreamingBitrate": 128000,
  "DeviceProfile": {
    "Name": "Goby audio profile reference",
    "MaxStreamingBitrate": 128000,
    "DirectPlayProfiles": [],
    "TranscodingProfiles": [{
      "Type": "Audio",
      "Context": "Streaming",
      "Container": "mp3",
      "AudioCodec": "mp3",
      "MaxAudioChannels": "2",
      "Protocol": "http"
    }]
  }
}
```

The recorder followed each actual returned delivery URL within the reference origin, first HEAD and then GET, without altering parameters or injecting an additional authentication header. A GET 500 permitted one identical-URL retry after 200 ms; the initial failure remained a separate record. No local-media response redirected. Successful bodies were probed and decoded to their actual end with FFmpeg/ffprobe 9.0.1 and strict `-xerror`. HLS verification followed the real emitted HTTP graph from a private local master. No credential-bearing URL was placed in process arguments or console output.

Each HTTP body was bounded at 2 MiB, total retained wire bodies at 8 MiB, redirects at three same-origin hops, and HLS manifests at six segments. HTTP timeouts were 15 seconds; probe and full-decode timeouts were 20 and 25 seconds. This was a deliberately small matrix, not a Cartesian product of codecs and settings.

## Protocol and context selection

All following PlaybackInfo requests returned HTTP 200. In the first five rows, the native source had `Container: "flac"`, `SupportsDirectPlay: false`, `SupportsDirectStream: false`, `SupportsTranscoding: true`, `DefaultAudioStreamIndex: 0`, and `TranscodingContainer: "mp3"`. `DefaultSubtitleStreamIndex` was omitted.

| Profile difference | Returned endpoint leaf | `TranscodingSubProtocol` | Actual delivery |
| --- | --- | --- | --- |
| `Protocol: "http"`, `Context: "Streaming"` | `stream.mp3` | `"http"` | Progressive MP3; initial GET 500, retry 200 |
| Protocol omitted | `stream.mp3` | property omitted | Progressive MP3; initial GET 500, retry 200 |
| `Protocol: ""` | `stream.mp3` | `""` | Progressive MP3; first GET 200, complete six-second decode |
| Context omitted, `Protocol: "http"` | `stream.mp3` | `"http"` | Same selected output as Streaming |
| `Context: ""`, `Protocol: "http"` | `stream.mp3` | `"http"` | Same selected output as Streaming |
| `Context: "Static"`, `Protocol: "http"` | `stream` | property omitted | No usable selected output; HEAD 200, GET 500, retry 500 |

The Static-only case also omitted `TranscodingContainer`, yet retained `SupportsTranscoding: true` and a `TranscodingUrl`. Its URL lacked the audio codec, bitrate, stream index, and channel parameters found in the successful Streaming profiles. This is an observed unusable reference result, not evidence that a Static-only profile supports streaming conversion. Empty Context was accepted by this JSON endpoint; it is not an additional declared EncodingContext enum value.

Every selected progressive URL contained `api_key`. The source DTO nevertheless had `AddApiKeyToDirectStreamUrl: false`; that field was not evidence that the returned URL lacked authentication. Common emitted parameter names and values included `AudioCodec=mp3`, `AudioBitrate=128000`, `AudioStreamIndex=0`, `TranscodingMaxAudioChannels=2`, `TranscodeReasons=DirectPlayError`, and lower-camel-case `allowAudioStreamCopy=false`. No `AudioSampleRate` parameter appeared in the baseline profile.

Evidence: the `flac-http-mp3`, `flac-protocol-omitted`, `flac-protocol-empty`, and `flac-context-*` families, plus the [derived URL contract summary](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-url-contract-summary.json). The summary preserves original query-key casing while excluding credential and identity values.

## Progressive containers and actual media

All three explicit HTTP output profiles used the same 128 kbit/s ceiling and a two-channel maximum. Their native `MediaStreams` still described the original FLAC. Output codec, sample rate, and duration below come from independent decoding of the returned media, not from those native source fields.

| Output profile | DTO output container / protocol | GET bytes | Probed format and decoded audio | Complete decoded response duration |
| --- | --- | ---: | --- | ---: |
| MP3/MP3 with empty Protocol | `mp3` / `""` | 97,031 | MP3, 48 kHz, stereo | 6.000000 s |
| AAC/AAC with HTTP | `aac` / `http` | 100,406 | ADTS AAC, 96 kHz, stereo | 6.016000 s |
| M4A/AAC with HTTP | `m4a` / `http` | 99,647 | MP4/M4A AAC, 96 kHz, stereo | 6.000000 s |
| MP3/MP3 with HTTP, Start2 | `mp3` / `http` | 65,159 | MP3, 48 kHz, stereo | 4.000000 s |

Each successful media probe and strict full-body decode exited 0. The ADTS response declared `audio/mp4`, even though ffprobe identified `aac` framing; this repeats the reference MIME mismatch observed in M4c. The M4A response was genuinely an MP4/M4A container and also declared `audio/mp4`.

The explicit-HTTP baseline, omitted Protocol, omitted/empty Context, one-channel maximum, exact-condition control, and HTTP-first mixed-profile case each first returned GET 500. Their one allowed retry returned HTTP 200 with **81,920 bytes**. Those returned MP3 bodies decoded without an error but contained only **5.064979 seconds** of audio. ffprobe's format duration was still 6.000000 seconds. These are incomplete source-duration deliveries, not successful six-second playback. The capture retains both the first 500 and the shortened retry; no extra retry was used to hide them. This study does not establish the cause of the vendor startup or truncation behavior.

Evidence: [AAC probe](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-http-aac-media-probe.json), [M4A probe](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-http-m4a-media-probe.json), [complete MP3 observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-protocol-empty-observation.json), and [shortened HTTP retry probe](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-http-mp3-media-probe.json).

Successful progressive GET responses were chunked, omitted Content-Length and ETag, and declared `Accept-Ranges: none`. HEAD returned 200 without a body, with the corresponding MIME, `Accept-Ranges: none`, and an estimated Content-Length of 96,000 for 128 kbit/s requests or 72,000 for the 96 kbit/s condition case. Start2 still had the 96,000-byte HEAD estimate. Those estimates were not actual final lengths. This matrix did not repeat Range behavior already established by the separate [universal-audio study](audio-reference.md#progressive-conversion-headers-and-seeking).

## Audio conditions and selected streams

Codec condition controls used `CodecProfiles` with `Type: "Audio"`, `Codec: "mp3"`, `Container: "mp3"`, and required conditions. Property values were strings, as declared by the profile model.

| Profile control | Emitted audio-specific change | Actual returned MP3 |
| --- | --- | --- |
| `MaxAudioChannels: "1"` | `TranscodingMaxAudioChannels=1`; no separate channel condition parameter | 48 kHz, mono |
| `AudioChannels Equals 1` and `AudioSampleRate Equals 44100` | **`audiochannels=1`**; `TranscodingMaxAudioChannels=2` remained; no sample-rate query parameter | 48 kHz, mono, not the requested exact 44.1 kHz |
| `AudioChannels <= 1`, `AudioSampleRate <= 48000`, `AudioBitrate <= 96000` | **`audiochannels=1`**, `AudioBitrate=96000`; maximum-channel parameter remained 2; no sample-rate query parameter | 48 kHz, mono, complete six-second response |

The lower-case `audiochannels` spelling is present in the captured URL, not a documentation normalization. The exact sample-rate condition did not constrain this reference output to 44.1 kHz. The limit case's 48 kHz result satisfied its ceiling but, alone, does not prove that the condition was enforced. Goby's supported capability claims should be evaluated against actual produced media rather than reproducing an observed reference violation.

The selected-audio/seek control supplied `AudioStreamIndex: 0`, `StartTimeTicks: 20000000`, and the previous response's `CurrentPlaySessionId` in a second POST. The returned URL carried `AudioStreamIndex=0` and `StartTimeTicks=20000000`; the returned `PlaySessionId` was different from the supplied current ID. Its original GET returned 500, and the single same-URL retry decoded to exactly four seconds. This proves the observed POST-body behavior only; no alternate placement or spelling of the reuse field was probed.

Evidence: [exact conditions](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-codec-exact-info.json), [limit conditions](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-codec-limits-info.json), [URL field summary](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-url-contract-summary.json), and [selected stream / Start2 observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-http-mp3-start-reuse-observation.json).

## Direct flags, minimal requests, and profile order

| Control | DirectPlay / DirectStream / Transcoding | Returned delivery |
| --- | --- | --- |
| MP3 GET with UserId only | true / true / true | No DirectStreamUrl or TranscodingUrl; output container/protocol fields omitted |
| FLAC POST with UserId only | true / true / true | Same absence of delivery URLs and output fields |
| MP3 matching direct profile, all methods allowed, 2 Mbit/s ceiling | true / true / true | DirectStreamUrl returned exact original 97,098-byte MP3 |
| FLAC DirectStream enabled, DirectPlay and Transcoding disabled | false / true / false | DirectStreamUrl returned exact original 716,925-byte FLAC |
| All three methods disabled | false / false / false | No delivery URL; no ErrorCode was present |

The ordinary complete minimal requests returned a PlaySessionId and default audio index 0. Their lack of URLs must not be replaced in the reference fixtures with an inferred playable URL. The all-disabled result also returned a PlaySessionId. The initial FLAC GET minimal response is covered only by the incomplete-response caveat below, so its HTTP status and headers are not claimed.

The two direct-media GET and HEAD pairs returned 200, exact original Content-Length values, `Accept-Ranges: bytes`, ETags, and truthful MP3/FLAC MIME types. Full decoding yielded six seconds. DirectStreamUrl included an `api_key` despite `AddApiKeyToDirectStreamUrl: false`.

For the mixed-profile controls, the first eligible entry selected the output. `[HLS/TS/AAC, HTTP/MP3/MP3]` returned `TranscodingContainer: "ts"`, `TranscodingSubProtocol: "hls"`, and the HLS master. Reversing those two entries returned MP3/HTTP. The HLS profile used `SegmentLength: 3` and `MinSegments: 1`; both advertised TS segments returned 200. A real HLS demuxer followed the emitted graph and decoded AAC/96 kHz/stereo for **6.016000 seconds**, exit 0. The playlist described a six-second VOD. This establishes ordering for these two eligible audio profiles, not all mixed-codec combinations.

Evidence: the `mp3-get-minimal`, `flac-post-minimal`, `mp3-direct-match`, `flac-direct-stream-only`, and `flac-all-disabled` families; [HLS-first observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-hls-first-observation.json); and [complete HLS graph decode](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-hls-first-hls-probe.json).

## Integrity, cleanup, and reproduction

The existing official service remained PID **3131777** with its private network namespace. All capture processes entered that namespace, which differed from the host namespace. Goby's separate service retained PID **3247967** before and after this study. Root free space remained above 52 MB; new credentials, raw JSON, exact base64 wire bodies, exports, and temporary decode inputs lived in the existing marked tmpfs runtime tree:

```text
/dev/shm/goby-emby-reference/runtime/audio-profile-m4d
```

The new ownership marker is `goby-audio-profile-m4d-owned-v1`. Private files use mode 0600 and directories mode 0700. The normal login used a new recorder device for the existing owned ordinary account. No administrator login was needed. Each case cleaned up only returned/emitted play-session IDs on that device. Final device-only ActiveEncodings deletion and logout both returned 204. The final runtime observation found no reference child process and zero transcode-cache files or bytes. Only this capture's marked probe-input directory was removed; private evidence and immutable source files were retained.

The remote audit verified **1,220 preceding raw/export files**, corresponding to the old 610 records, and all four original audio hashes. It checked each new export against deterministic sanitization of its private raw record, exact header structure and non-secret values, JSON types and values, and wire reconstruction/hashes. Non-HEAD Content-Length values were compared with body bytes. Binary exports were checked for credentials. Only this new prefix's 126 sanitized JSON files were copied to the repository, bringing the reference corpus from 610 to 736 records for this extension.

There was one recorder defect before the matrix continued: inherited export auditing did not yet allow redaction of the reused source directory, which now lived outside the new capture root. The first FLAC minimal GET had already persisted its exact wire body when that audit stopped. Its status and headers had not been persisted. The fix was limited to this new script and explicitly audited that source-path replacement. The body was retained in [an incomplete observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-profile-m4d-flac-get-minimal-incomplete.json), including its wire hash and limitation; the request was not repeated and no synthetic headers/status were added. This record is excluded from the 87 complete HTTP captures but included in the wire-byte budget and audit.

Completed capture stages refuse to overwrite existing evidence. For a newly allocated capture, stage order is `setup`, `capture`, `summarize`, `finish`; `summarize` derives query-field facts without network traffic. The completed `audit` may be repeated through SSH:

```powershell
ssh test-env 'pid=$(systemctl show goby-emby-reference.service -p MainPID --value); nsenter -t "$pid" -n python3 /opt/goby-test/repository/scripts/test-env/reference-audio-profiles.py audit'
```

The final audit summary remains in the new root's `private/audit-summary.json`. Private originals and reused sources are in tmpfs and are volatile across reboot. No local tests, validators, builds, runtime probes, or git operations were performed. Authentication attacks, output Range semantics, multi-consumer cancellation, and arbitrary input-container support were not repeated in this focused PlaybackInfo matrix.
