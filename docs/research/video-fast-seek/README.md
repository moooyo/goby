# Verified fast video decoding: bounded Linux research

This report describes controlled FFmpeg 9.0.1 experiments and the subsequent M4f core implementation. It does not establish a universal startup deadline. All media commands and Go verification ran through `ssh test-env`; no local runtime verification, database work, service changes, or commits were performed by this task.

The preceding M4e production path decodes the source from the beginning, preserves its format clock with `copyts` and `itsoffset`, and applies output `ss`. This remains the correctness reference. The optimization must preserve its displayed video frames, audio position, and selected-track clock.

## Result

A supported implementation can combine **a verified H.264 IDR video restart** with **a second, independent linear audio input** from the same borrowed file. The second input uses `discard:v all` and the same format-origin offset. Both selected tracks still pass through the existing output seek and fragmented MP4 writer.

The dual-input path produced exactly the same decoded video frame hashes and complete audio PCM as the linear reference for MP4, Matroska, and MPEG-TS, with either copied or encoded AAC. The caller's input descriptor was deliberately positioned at byte 17; every conversion left it at byte 17. Each `/proc/self/fd/3` input is opened independently by the child, without seeking the caller's file description.

Fast-seeking both tracks together is **not** accepted. It produced the correct video sequence but changed decoded/encoded AAC waveforms, and Matroska introduced an eight-sample audio offset. Equal duration or equal sample count did not detect these failures.

## Measured controls

The main source is 180 seconds, 96x54, 12 FPS, H.264 with two B-frames and two-second closed GOPs, plus nonperiodic AAC chirp audio. The request starts at 120.37 seconds and the diagnostic writes a two-second output window. Three source files together occupy approximately 3.8 MiB.

| Input | Indexed IDRs | Candidate absolute PTS | Actual first IDR after seek | Index scan | Preflight |
| --- | ---: | ---: | ---: | ---: | ---: |
| MP4 | 90 | 120.0 | 120.0 | 0.016 s | 0.005 s |
| MKV | 90 | 120.0 | 118.0 | 0.017 s | 0.005 s |
| TS | 90 | 121.566667 | 121.566667 | 0.020 s | 0.006 s |

The TS format origin is 1.545333 seconds. Its video starts 21.333 milliseconds after that origin. Matroska's actual landing two seconds before the candidate demonstrates why candidate timestamps cannot be assumed to describe demuxer behavior.

For a single fast input, all video frame hashes matched. Encoded AAC had equal sample counts but normalized waveform errors of approximately 0.0953, 0.0624, and 0.0488 respectively. The best matched sample offsets were 0, 8, and 0. These are retained failure results, not accepted tolerances.

For two inputs, all six container/audio-mode combinations matched the entire reference audio PCM byte for byte, and all video frame hashes matched. This preserves decoder history and Matroska's existing audio timestamp behavior instead of trying to approximate an AAC warmup interval.

The tiny source is dominated by audio and process overhead: the dual-input encoded-AAC path took approximately 0.109–0.130 seconds, and did not improve the corresponding low-resolution linear timings. This is not hidden by the report.

A separate temporary CPU control used a 150-second, 3840x2160/24 FPS uniform-color H.264/AAC source of only 2,627,320 bytes. Seeking to 120.37 seconds and producing two seconds scaled to 96x54 took **9.784 seconds** with linear video decoding and **0.259 seconds** with fast video plus linear audio. The temporary source was removed after the measurement. Uniform color is suitable for measuring prefix decoder work, not for proving frame position; the nonperiodic source provides the content checks.

A `readrate=2` control timed out the linear single input after five seconds with zero output bytes; the single fast input completed in 1.026 seconds. This demonstrates the effect of bypassing prefix input delivery. It does **not** establish the I/O behavior of the final dual-input design: the audio input can still read the source linearly, especially in MPEG-TS. No universal 45-second startup guarantee or constant-time seek is claimed.

## Restart evidence and index contract

The existing `Keyframes` API records demuxer `K` flags. Those entries are useful candidates but are not decoder restart certificates.

The experiment's compressed-packet index uses FFmpeg's H.264 `filter_units=pass_types=5` and `framehash`. Only IDR NAL units survive. Each record retains native PTS/DTS, its rational time base, and the filtered packet SHA-256. A same-binary preflight uses the proposed input seek arguments and verifies that the actual retained IDR matches an indexed packet and occurs before the requested presentation position.

A stronger index also records decoded IDR image hashes. This addresses active SPS/PPS state, rather than merely observing a NAL type:

1. Read the source sequentially with `skip_frame=nokey`; this avoids decoding ordinary inter-predicted pictures while still processing parameter-set updates.
2. Map the selected video twice to a `framehash` output. One stream copies packets through `filter_units=pass_types=5`; the other produces raw decoded key pictures with `enc_time_base=demux` and `fps_mode=passthrough`.
3. Retain decoded picture hashes only where native presentation timestamps also have an actual IDR marker. Other decoded I/key pictures provide no restart authorization.
4. Use a short equivalent preflight at a candidate boundary and require matching packet identity, decoded IDR image identity, dimensions, and source-clock position. If the proof cannot be established within its limits, keep the existing linear plan.

The combined-index command was exercised on all three long-source containers. Every one of their 90 IDR image hashes matched the corresponding picture from a complete linear decode. A two-stream preflight limited to one marker and one decoded key picture then matched both indexed digests and the same native-rational PTS. A VFR fixture retained the correct visible frame-hash sequence as well. The combined index took approximately 25–34 milliseconds and the long-source preflight 7–9 milliseconds on these low-resolution inputs.

The open-GOP fixture produced one IDR marker at zero and four decoded key pictures. Only the first picture qualifies. This preserves correctness through the original fallback; it does not mislabel non-IDR open-GOP pictures as safe restart points.

The simpler input filter `filter_units=pass_types=5|6|7|8` followed by decoding is unsuitable as a general index builder. On the open-GOP fixture, retained recovery SEI packets without their removed VCL pictures caused decoder errors. That rejected experiment is retained in `open-gop-boundary.json`.

The implementation uses **private seek-index version 1**, separate from public Emby DTOs and the current presentation timeline:

- Source identity: existing immutable source stamp plus opened-file device/inode, size, modification time, and change time. A changed source invalidates the index and any prepared seek.
- Tool identity: FFmpeg executable/version fingerprint. Decoder or parser changes invalidate cached restart proofs.
- Stream identity: original selected stream index, source codec H.264, known raw format origin, native rational time base, dimensions/pixel representation used by the hash.
- Ordered entries: actual IDR PTS/DTS, compressed IDR digest, and decoded image digest. Nonmonotonic, duplicate, ambiguous, or mismatched records are unsupported.
- Limits: bounded line length, output bytes, packet/frame/entry counts, source duration, wall time, and process-group cancellation. A bounded index should be prepared and cached during library analysis; a full source scan must not be concealed inside a 45-second playback-start promise.

Single-picture equality cannot prove the state of an earlier PPS that the IDR does not use but a later picture references. The final scanner therefore has five bounded branches:

1. Actual IDR coded hashes.
2. Decoded key-picture hashes, joined only to the IDR markers.
3. Normalized SPS/PPS packet hashes through `h264_mp4toannexb,filter_units=pass_types=7|8`. The complete source must have one stable parameter-set hash.
4. All original coded packets, streamed without retaining their payloads, to reject packet side data other than the known one-byte MPEG-TS video stream identifier. This catches container `NEW_EXTRADATA` that filtered branches could otherwise drop.
5. A scan-only branch removing ordinary NAL types `1,5,6,7,8,9,10,11,12`. Any remaining packet rejects the fast index. It must remain empty through full source EOF.

The last check covers packet NAL scope, not a claim to parse every possible initial extradata extension. Initial extradata is provided afresh on each opening; changes and unsupported packet extensions are rejected. `PacketSideDataChecked` and `NALScopeChecked` are required index fields, established only by the complete scan.

Runtime proof uses only the first three branches, with normal input decoding and a `select=key` decoded-output filter. It deliberately does not use input `skip_frame=nokey`: that option can change stream-info decoder-delay discovery and therefore FFmpeg's internal seek target. Index construction can retain the skip optimization because runtime verifies the actual picture against its recorded evidence.

A four-branch runtime attempt was rejected by the real runner regression: limiting the passthrough branch to its first packet ended the output before a TS IDR was reached. The complete-source side-data check now remains in scanning, bound to the unchanged source identity. The runtime must actually receive and match IDR, decoded picture, and parameter-set evidence.

A missing index, unsupported codec or parameter-set state, exhausted proof budget, or unproven recovery point selects the existing linear fallback. Source mutation and cancellation remain errors rather than being hidden as ordinary cache misses.

## Private plan and command changes

The plan adds `VideoSeekCandidate`, a canonical private JSON string bounded to 64 KiB. This keeps `Plan` comparable and within its existing 128 KiB serialized limit. Its versioned object contains the requested start, initial input-seek proposal, index identity, and at most 64 preceding indexed points. Empty values preserve the original behavior. Candidates are valid only for progressive MP4 video encoding with a nonzero request; copied video, HLS, and audio-only outputs reject them.

Run must preserve the **tested input seek argument**, not replace it with the observed restart picture's timestamp. In the MKV control, candidate 120 lands at IDR 118. Using 118 as a new argument can land earlier again and no longer reproduce the proof. Runtime can test native PTS/DTS-derived arguments from the four most recent indexed points, deduplicated and limited to eight attempts under one five-second budget. Each argument must independently pass the proof. No fixed backward time margin is used. The successful argument is returned separately from the actual landing timestamp.

A pure preparation helper selects an indexed candidate without running FFmpeg. `BuildArgs` still previews the linear path. Run performs the bounded proof against its borrowed source before creating the progressive output and applies the optimization only on success. Candidate data is not accepted from URL parameters. Persisted or reconstructed requests must revalidate source and tool identity; an old boolean alone never authorizes a restart.

For a prepared seek, input zero uses the existing source-safe input setup plus:

```text
-copyts -seek_timestamp 1 -noaccurate_seek
-ss <format-origin + verified input-seek argument>
-itsoffset <-format-origin> -i /proc/self/fd/3
```

When audio is selected, input one independently opens the same inherited file:

```text
-threads <limit> -discard:v all
-itsoffset <-format-origin> -i /proc/self/fd/3
```

Both inputs need the same explicit local format/protocol allowlists. Hardware video decoding belongs only to input zero. The output keeps the current requested `ss`, common source clock, video filters, optional physical FPS conversion, and fragmented MP4 settings. Mapping becomes `0:<video-index>` and `1:<audio-index>`. No new worker pool, cache filename, shell, network input, or source-path authority is needed.

The linear audio path can remain I/O-bound. This proposal removes the expensive video prefix decoding without promising that all source bytes before the request can be skipped.

## Primary implementation evidence

- FFmpeg input seek uses `avformat_seek_file(..., INT64_MIN, target, target, 0)`, while ffprobe intervals allow a different upper bound. FFmpeg also has a decoder-delay adjustment on certain demuxers. Therefore ffprobe intervals cannot certify the production seek landing: [ffmpeg_demux.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_demux.c#L2238), [ffprobe.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffprobe.c#L1706).
- H.264 parser key flags include recovery points and heuristics; an IDR has stronger reference-reset semantics: [h264_parser.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/h264_parser.c#L354), [h264dec.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/h264dec.c#L438), [RFC 6184](https://www.rfc-editor.org/rfc/rfc6184#section-8.5.2).
- Packet positions are not generic payload offsets. Matroska positions refer to block framing, and TS payloads are reassembled across transport packets. This design deliberately uses demuxed packet/decoded-picture evidence instead of parsing arbitrary `ReadAt(pos,size)` bytes: [matroskadec.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/matroskadec.c#L4294), [mpegts.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/mpegts.c#L1076).
- TS timestamp-search index entries are not codec random-access evidence: [mpegts.c](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/mpegts.c#L3783).

## Artifacts and remaining implementation gates

- `experiment.py`: bounded Linux reproduction of the long-source IDR, single-input failure, dual-input correctness, and explicit read-rate controls.
- `single-input-controls.json`: original failure evidence and index/preflight timings.
- `dual-input-controls.json`: exact AV comparisons and borrowed-descriptor offsets for both AAC modes.
- `cpu-control.json`: measured high-resolution CPU control, with its explicit limitation.
- `open-gop-boundary.json`: compressed IDR evidence and the rejected filtered-decoder experiment.
- `restart-proof-controls.json`: combined packet/image proof, full-decode comparisons, actual landing, and VFR control.

The remote owned directory is `/opt/goby-test/exec-scratch/goby-fast-video-seek-long-uajx7orq`, with `OWNER.txt` equal to `goby-fast-video-seek-experiment`. The shorter initial diagnostics are in `/opt/goby-test/exec-scratch/goby-fast-video-seek-2saibvaa` under the same marker. Existing M4e inputs were only read.

The bounded index/parser, optional Prober analysis, source/tool invalidation, native-rational candidate checks, runtime proof, and dual-input renderer have been implemented. Analysis requires both `Prober.FFmpegPath` and `Prober.AnalyzeVideoSeek`; ordinary probing does not implicitly start full indexing. Probe version is 6.

Earlier focused Linux media tests passed after the scan/proof fixes in 0.488 seconds. The actual transcode target passed in 2.987 seconds, including recorded real converter arguments, three containers, AAC encode/copy/no-audio, complete video hashes and audio PCM equality, invalid-proof fallback, and private-plan bounds. The positive assertions explicitly reject a silent linear fallback. Subsequent application wiring, source resource gates, and matching the proof's decoder threads to Run have been implemented and compile for Linux/amd64, but these later changes still require the final Linux regression suite and deployment acceptance. The [operating contract](../../development/video-fast-seek.md) records their exact scope and the outstanding cache-rebuild and aggregate-memory limitations.
