# Universal and progressive audio reference: Emby 4.9.5.0

This bounded M4c study captured **174 new `audio-m4c-*` records**, including 117 HTTP responses, from the existing official Emby Server 4.9.5.0 reference on `test-env`. The initial 165 records were followed by nine independent `audio-m4c-defaults-*` records. It establishes original audio delivery, successful progressive MP3 conversion and seeking, and one complete AAC/TS HLS graph. It also preserves several reference failures and inconsistent metadata as observations, not behavior Goby should reproduce.

The [sanitized fixtures](../../tests/compatibility/fixtures/reference/emby-4.9.5.0) were captured on 2026-09-09, initially from approximately 04:25:48 through 04:32:39 UTC, with the minimal default-format follow-up at 04:56:57 UTC. The recorder is [reference-audio.py](../../scripts/test-env/reference-audio.py). No Goby production code, deployment, user state, or existing reference media was changed by this study.

## Sources and bounded method

The primary parameter source was the official [Audio Streaming guide](https://dev.emby.media/doc/restapi/Audio-Streaming.html), retrieved on 2026-09-09. The guide describes comma-separated `Container` capabilities, `MaxStreamingBitrate`, `MaxSampleRate`, client-generated unique `PlaySessionId` values, and output selection through `TranscodingProtocol`, `TranscodingContainer`, and `AudioCodec`. It identifies an omitted protocol as progressive delivery and `hls` as the HLS choice. Redirect settings concern external and cloud media. The [pinned SDK inventory](../api/services/UniversalAudioService.md) separately declares GET/HEAD on both `/Audio/{Id}/universal` and `/Audio/{Id}/universal.{Container}`, but omits most guide parameters.

The initial matrix contained 27 controls. It concentrated on MP3 and FLAC rather than taking a Cartesian product: omitted capabilities; matching, ordered, uppercase, and conflicting containers; bitrate and sample-rate controls; progressive and HLS choices; start offsets; authentication; a static byte range; and four successive uses of one deliberately reused play-session ID. AAC and WAV each added one original-delivery control. Two subsequent controls addressed a fresh progressive seek with one identical-URL retry and a precise-duration FLAC HLS source. Successful audio bodies were independently probed and completely decoded with the installed FFmpeg/ffprobe 9.0.1. HLS decoding used a private master containing absolute same-origin media references, then followed the real reference HTTP graph. URLs were not placed in process arguments.

`MaxBitDepth=16` was one explicitly exploratory control. Neither the guide nor the pinned universal-audio operation documents this field. Its lack of effect is not evidence of a supported bit-depth-limit contract. `AudioSampleRate` is documented for the legacy stream operation; its universal-audio behavior was observed separately from guide-supported `MaxSampleRate`.

Each HTTP body was limited to 2 MiB, total captured wire bodies to 12 MiB, redirects to three same-origin hops, and HLS manifests to six segments. Initial captured HTTP bodies totaled **6,808,869 bytes**; the minimal follow-up added **1,346,425 bytes**, for **8,155,294 bytes** combined. No redirects were returned in this local-media matrix, so no remote- or cloud-media redirect behavior was tested.

## Environment and media provenance

The [existing official reference deployment](reference-server.md) remained `active/running`, PID **3131777**, with `PrivateNetwork=yes`. Recorder processes entered only that service's network namespace; it differed from the host namespace. The vendor package and server configuration were unchanged. Goby's separately running service retained PID **3176023** throughout.

Before capture, the root filesystem had approximately 49 MB free. New sources, raw JSON, exact base64 wire bodies, credentials, and sanitized exports therefore stayed under:

```text
/dev/shm/goby-emby-reference/runtime/audio-m4c
```

Its ownership marker contains `goby-audio-m4c-owned-v1`. Private files use mode 0600 and private directories mode 0700. The source subdirectory uses mode 0755 and generated media mode 0644. This directory is inside the reference's already-authorized runtime tree; it was not a new host-facing mount or listener. Metadata-writing and online-provider options were inherited from the owned, provider-disabled reference music library. Hashes verified that source bytes stayed unchanged. These tmpfs sources and private originals are volatile across host reboot; only sanitized copies are stored in the repository.

A new ordinary account, `reference-audio-m4c`, and an independent administrator-device login were created through ordinary APIs. The administrator added and refreshed only `Reference Audio M4c`, a music library pointing at the new source directory. Credentials were never written to the repository. The new source audio consists of six-second synthetic 440 Hz tones:

| Source | Bytes | Independently probed audio | Complete decoded duration |
| --- | ---: | --- | ---: |
| MP3 | 97,098 | MP3, 44.1 kHz, stereo, 128 kbit/s | 6.000000 s |
| FLAC | 716,925 | FLAC, 96 kHz, stereo, 24-bit | 6.000000 s |
| AAC/ADTS | 50,440 | AAC, 48 kHz, mono | 6.037333 s |
| WAV | 576,160 | PCM signed 16-bit little-endian, 48 kHz, mono | 6.000000 s |

Source SHA-256 values:

- MP3: `75697f6abbda787e48de57462fef4c75c2decaa07576ef95d2105fd8151ed61b`
- FLAC: `843ce1829f3ef1807233e4a0ed874d9132fa87f581b07298576ac18ea4216c81`
- AAC: `ae7f1c88684f05ec883b327dd2b1fae17445b3dfaba446c02613c2f70ac46160`
- WAV: `37fc3c2a29ff734981c1217003888b8aa7207fcf4c9f498c19fe9535ce36c8b5`

Evidence: [source provenance](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-source-provenance.json), [indexed items](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-source-items-01.json), and [initial runtime](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-runtime-before.json). Raw ADTS probe duration estimation was 5.947500 seconds while complete decoding produced 6.037333 seconds; these different measurements are retained.

## Original delivery and capability selection

Unless a row says otherwise, ordinary requests supplied the owned user/device/token, a fresh client-generated play-session ID, and `MaxStreamingBitrate=2000000`.

| Control | Observed GET/HEAD result |
| --- | --- |
| MP3 with only `DeviceId` and `api_key` | Both 200. Omitted `UserId`, `PlaySessionId`, bitrate, container, and output fields did not prevent original delivery. |
| MP3 `Container=mp3` | Both 200, exact original MP3 bytes. |
| MP3 `Container=mp3&AudioCodec=aac` | Both 200, exact original MP3 bytes; the alternate conversion codec did not force conversion. |
| FLAC `Container=flac` | Both 200, exact original FLAC bytes. |
| FLAC `Container=mp3,flac`, `flac,mp3`, or `FLAC,MP3` | Each returned original FLAC bytes. These list orders and case variants accepted the available original container. |
| FLAC match plus `MaxSampleRate=48000` | Still original 96 kHz/24-bit FLAC. |
| FLAC match plus `MaxSampleRate=48000&AudioSampleRate=22050` | Still original 96 kHz/24-bit FLAC. This did not establish target-rate precedence for conversion. |
| FLAC match plus exploratory `MaxBitDepth=16` | Still original 24-bit FLAC; field support remains unestablished. |
| MP3 original match plus `StartTimeTicks=20000000` | Original six-second MP3 bytes unchanged. |
| AAC `Container=aac` | Original ADTS bytes, but the server declared `audio/mp4`. Goby should use truthful format/MIME facts rather than copy this mismatch. |
| WAV `Container=wav` | Original WAV bytes, `audio/wav`, both 200. |
| `/universal.mp3?Container=flac` on MP3 | HEAD 200, GET 500. The failure does not establish a useful suffix/query precedence rule. |

Successful original MP3 GET and HEAD had `Content-Type: audio/mpeg`, `Content-Length: 97098`, `Accept-Ranges: bytes`, an ETag, and `Cache-Control: private, no-transform`. Neither had Transfer-Encoding or Location. Original FLAC, AAC, and WAV bodies also matched their source hashes and passed independent full decoding.

Evidence: the `audio-m4c-mp3-minimal-*`, `mp3-match-*`, `mp3-match-other-codec-*`, `flac-match-*`, `flac-list-*`, `flac-max-sample-*`, `flac-sample-precedence-*`, `flac-bit-depth-exploratory-*`, `mp3-original-start-*`, `aac-match-*`, and `wav-match-*` families. The direct-compatible sample-rate and exploratory bit-depth observations do not justify Goby violating a capability limit that it claims to support.

### Minimal follow-up: omitted Container across source formats

A separate one-shot follow-up reused only the existing FLAC, AAC, and WAV sources. Each GET and HEAD used `/Audio/{Id}/universal` with **only `DeviceId` and `api_key`**, exactly the parameter-name set of the earlier MP3 minimal control. `UserId`, `PlaySessionId`, `MaxStreamingBitrate`, `Container`, and all output-selection fields were omitted. A new ordinary login was issued normally and logged out afterward; no source, library, or user policy was changed.

| Source | GET / HEAD | Received bytes and matching Content-Length | Declared MIME | Exact source-byte match |
| --- | --- | ---: | --- | --- |
| FLAC | 200 / 200 | 716,925 | `audio/flac` | yes |
| AAC/ADTS | 200 / 200 | 50,440 | `audio/mp4` | yes; the previously observed MIME mismatch remains |
| WAV | 200 / 200 | 576,160 | `audio/wav` | yes |

All six responses advertised `Accept-Ranges: bytes` and omitted Location. GET hashes matched the previously captured source hashes exactly. Runtime observations before and after found zero reference child processes and zero transcode-cache files/bytes. No repeat media decoding was needed: these are the same bytes whose full decoding had already been verified.

Together with the earlier MP3 control, this supports treating an omitted `Container` as unconstrained original-format acceptance for Goby's adapter, rather than inventing an MP3-only default and converting lossless sources unnecessarily. ALAC was not included in this follow-up; no format-specific ALAC compatibility claim is made.

The nine new records contain eight HTTP captures: one login, the three GET/HEAD pairs, and logout. A separate marked directory, `/dev/shm/goby-emby-reference/runtime/audio-m4c-defaults`, holds their private originals and audit summary. Its audit preserved **1,202 preceding raw/export files**, including all **330** files belonging to the initial **165** audio records, and all four source hashes. The host root filesystem reported zero available bytes during this follow-up; the new evidence remained in tmpfs and no host cleanup or Goby deployment was attempted.

Evidence: [combined minimal observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-defaults-observation.json), [FLAC GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-defaults-flac-minimal-get.json), [AAC GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-defaults-aac-minimal-get.json), and [WAV GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-defaults-wav-minimal-get.json).

## Progressive conversion, headers, and seeking

A successful FLAC-to-MP3 request used these output parameters, in addition to the ordinary identity fields:

```text
Container=mp3
MaxStreamingBitrate=2000000
TranscodingProtocol=
TranscodingContainer=mp3
AudioCodec=mp3
```

GET returned 242,183 bytes of MP3 at 48 kHz stereo. Independent probing and complete decoding both succeeded, yielding six seconds. It returned HTTP 200 directly, not a redirect. Its empty `TranscodingProtocol` value is an observed explicit-empty case; the guide independently documents omitted protocol as progressive.

| Header | GET for the successful conversion | HEAD before GET |
| --- | --- | --- |
| Content-Type | `audio/mpeg` | `audio/mpeg` |
| Content-Length | absent | `1500000` |
| Transfer-Encoding | `chunked` | absent |
| Accept-Ranges | `none` | `none` |
| ETag / Location | absent / absent | absent / absent |
| Cache-Control | absent | `no-cache, no-store, no-transform, must-revalidate` |

The HEAD length was not the final byte count. A fresh play-session control requesting FLAC-to-MP3 with `MaxStreamingBitrate=128000` and `StartTimeTicks=20000000` first returned HTTP 500. One identical-URL retry after 200 ms, before cleanup, returned HTTP 200 with **65,159 bytes**, independently verified as MP3/48 kHz/stereo and **4.000000 seconds** of complete decoded audio. The first 500 remains separate evidence; the retry does not erase it or establish why initial startup failed.

Both initial and cached HEAD for that seek URL returned `Content-Length: 96000`, although the real result was 65,159 bytes. These two HEAD estimates are consistent with full source duration multiplied by requested bitrate, even when seeking; that is an inference from the captured numbers, not a general declared contract. The successful seek GET was chunked with no Content-Length or ETag and `Accept-Ranges: none`.

Adding `Range: bytes=0-1023` to the successful progressive URL still returned HTTP **200** and the **complete 65,159-byte stream**, without Content-Range. The byte-range request did not select an encoded-output prefix. In contrast, the legacy MP3 static control combined `Static=true`, conflicting codec/sample-rate hints, `StartTimeTicks=20000000`, and the same Range. GET and HEAD returned **206** with `Content-Range: bytes 0-1023/97098`; the GET bytes matched the original MP3 prefix exactly.

Evidence: [successful progressive GET](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-flac-progressive-empty-get.json), [its full decode](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-flac-progressive-empty-media-probe.json), [fresh seek/retry observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-control-progressive-start-fresh-retry-observation.json), [progressive Range response](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-control-progressive-start-fresh-range.json), and [static Range observation](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-mp3-static-range-start-observation.json).

Other failures are preserved without being treated as intended playback behavior:

- FLAC with only `Container=mp3` and the usual bitrate/identity fields returned HEAD 200 but GET 500. The public error showed a copy operation without a usable output extension/format. It does not support inventing defaults for omitted `TranscodingContainer` and `AudioCodec`.
- `TranscodingProtocol=http&TranscodingContainer=mp3&AudioCodec=aac` returned GET 500; the public FFmpeg error identified an AAC/MP3 output mismatch. This combined control is not an isolated proof about the `http` protocol value.
- Fresh MP3 low-bitrate and FLAC seek cases also returned initial GET 500 responses, while HEAD returned 200. A successful HEAD was not evidence that conversion had successfully begun or would complete.

## HLS audio

The first HLS control used MP3 with `Container=mp3`, a 64 kbit/s ceiling, `TranscodingProtocol=hls`, `TranscodingContainer=ts`, and `AudioCodec=aac`. Universal GET returned a master M3U8 directly with HTTP 200; HEAD returned 200 with Content-Length 0. The master and main playlists were both accessible. The main advertised **three full three-second entries**, numbers 0 through 2, for nine advertised seconds. Entries 0 and 1 returned HTTP 200; entry 2 returned HTTP 500 when the server attempted to start at six seconds. This was an overlong final advertised entry, not an accurately declared short tail.

The first two downloaded segments independently contained AAC/44.1 kHz/stereo and fully decoded without errors. Their observed audio start timestamps were 10.000000 and 13.041811 seconds. The segment probes estimated 2.809611-second spans, while full segment decoding produced 3.041814 and 2.995374 seconds. These measurements are not interchangeable, and the failed third entry prevents claiming successful complete playback of this MP3 HLS graph.

A separate control used the exact six-second FLAC source with:

```text
Container=mp3
MaxStreamingBitrate=128000
TranscodingProtocol=hls
TranscodingContainer=ts
AudioCodec=aac
MaxSampleRate=48000
AudioSampleRate=48000
```

Its master and main returned 200, and the VOD main contained **two three-second entries**, sequence 0, target duration 4, and ENDLIST. Both TS segments returned 200. A real FFmpeg HLS demuxer followed the emitted HTTP graph and completely decoded **AAC/44.1 kHz/stereo, 6.037188 seconds**, with exit code 0 and empty stderr. ffprobe reported six seconds and start time 10 seconds. The 44.1 kHz result was below the requested 48 kHz ceiling; it did not equal the separate `AudioSampleRate` value.

The captured child segment URLs contained PlaySessionId and omitted api_key; requests followed them without adding an authentication header or inherited token. This is evidence about this established reference stream graph, not permission to accept arbitrary unknown or foreign stream IDs. Goby's stricter ownership and token checks remain an intentional design boundary.

Evidence: [initial HLS main](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-mp3-low-hls-hls-main.json), [failed third entry](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-mp3-low-hls-hls-segment-2.json), the `control-existing-hls-segment-*-probe` records, [exact-duration main](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-control-flac-hls-exact-duration-hls-main.json), and [complete HLS graph probe/decode](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-control-flac-hls-exact-duration-hls-probe.json).

## Session identity, authentication, HEAD observations, and cleanup

The deliberate play-session-ID reuse sequence first requested a fresh low-bitrate MP3 stream and received 500. Without cleanup, changing quality, then seek position, then source to FLAC while retaining that same ID returned the **same 48,744-byte MP3/44.1 kHz/stereo/64 kbit/s output** each time. Every successful reused body had SHA-256 `2e8cd229325f4281a34b42cc6178580ea5349af7c728c663d5a51f3536090d52` and decoded to six seconds. This control violates the guide's requirement for a unique ID per stream URL. It documents unsafe reuse behavior, not a supported way to change source or quality, and does not establish a cross-user authorization issue. Goby should not reuse another plan's bytes when identifiers or source facts differ.

Universal GET and HEAD with a missing or invalid token returned 401. The GET body was `Access token is invalid or expired.`. No local-media case returned a Location header or redirect status. External redirects and credential forwarding were outside the matrix.

For each ordinary case, the recorder sampled direct reference child processes and transcode-cache file counts before HEAD, immediately afterward, and after 150 ms. It observed no direct child process in these windows. Cache counts were generally unchanged across each HEAD, with occasional decreases consistent with outstanding cleanup. This sampling cannot prove a short-lived encoder never ran, and HEAD Content-Length values demonstrably did not predict final encoded byte counts.

Every captured ownership-scoped `DELETE /Videos/ActiveEncodings` cleanup returned 204. Final device-only cleanup targeted only the new recorder device and also returned 204. Final user and administrator-device logouts returned 204. The final runtime observation found **no direct child process and zero files/bytes under the reference transcoding cache**. The recorder removed only its temporary probe-input directory; marked sources, exact private originals, and wire evidence were retained.

Evidence: the `audio-m4c-reuse-*`, `no-token-*`, `bad-token-*`, and `*-cleanup-*` records, [final runtime](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-runtime-final.json), [user logout](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-user-logout.json), and [administrator logout](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/audio-m4c-admin-logout.json).

## Integrity and reproduction

The final remote audit verified every new export against deterministic sanitization of its private original. It preserved JSON keys, types, numbers, response-header structure, non-secret header values, and exact raw wire reconstruction. Credential-bearing header values may be redacted without changing their names or order. Original Content-Length values were retained and compared with wire bodies where applicable; HEAD representation lengths were not incorrectly compared with their empty bodies. Binary bodies were checked for credentials before export.

The initial audit preserved all **872** preceding raw/export files, representing the previous **436** JSON records, and hashes for **216** pre-existing synthetic media/subtitle files. All four new source hashes also matched. The separate default-format follow-up subsequently protected the initial 165 audio records as well, auditing 1,202 preceding raw/export files. Only new `audio-m4c-*` exports were copied into the repository. No local test, validator, runtime probe, Goby deployment, or git operation was performed for this task.

For a newly allocated capture, stage order is `prepare`, `setup`, `capture`, `controls`, `finish`. The `defaults` stage is the separate one-shot minimal follow-up and refuses an existing follow-up directory. Completed stages intentionally refuse to overwrite existing evidence. `audit` may be repeated read-only for the initial capture. Use the existing service namespace from PowerShell:

```powershell
ssh test-env 'pid=$(systemctl show goby-emby-reference.service -p MainPID --value); nsenter -t "$pid" -n python3 /opt/goby-test/repository/scripts/test-env/reference-audio.py audit'
```

The completed audit summaries remain at `audio-m4c/private/audit-summary.json` and `audio-m4c-defaults/private/audit-summary.json` under the marked reference runtime tree. The study does not establish all output-codec combinations, remote/cloud audio redirects, arbitrary client players, sample-accurate cross-codec seeking, user-policy mutation behavior, or a general fix for the recorded vendor HTTP 500 responses.
