# M4e copied-video seek and fragmented MP4 edit-list research

This is a bounded Linux diagnostic, not a production implementation or a client-compatibility claim. No local runtime verification, database work, services, or production edits were performed during the diagnostic. Existing MP4/MKV/TS inputs were opened read-only from `/opt/goby-test/exec-scratch/goby-m4e-video-engine-8juf45nd` after checking its `OWNER.txt`. Diagnostic outputs are confined to `/opt/goby-test/exec-scratch/m4e-copy-elst-control` on the test host; this directory contains the retained report and JSON evidence. FFmpeg/FFprobe 9.0.1 were used. The generated open-GOP source and all outputs together occupy approximately 4 MiB.

## Conclusions

1. FFmpeg can write the appropriate nonzero `elst.media_time` in fragmented MP4 when `delay_moov` and `use_editlist=1` are enabled. Missing edit metadata is not the remaining fundamental issue.
2. With the edit list, retained preroll, `copyts`, and an explicit decoder-side presentation filter at zero, every displayed video-frame hash matches the intended source sequence. However, default FFmpeg fragmented-MP4 playback does not mark or suppress those negative-PTS video frames; the copied or encoded audio is already trimmed to the requested position. This is a real default-consumer A/V mismatch, not a successful generic seek.
3. Editing an existing `elst` duration from zero to a finite known duration does not change that FFmpeg behavior. Fabricating different init metadata is therefore not justified as a generic fix.
4. Nonfragmented MP4 provides a viable buffered-remux direction: default FFmpeg demux honors the edit and discards preroll video. Correct MP4 and TS-prefix examples are proven below. Demuxer seek behavior, coarse source timestamps, and client behavior still require explicit per-path verification.
5. Do not remove preroll or label a non-RAP sample as a random-access point. A missing-preroll control exits successfully but jumps to a later keyframe, losing requested content.

## Source facts

The reused source has 150 H.264 frames at 24 fps, 160x90, 6.25 seconds, closed GOP length 48 and two B-frames. Its audio is a nonperiodic chirp, `0.15*sin(2*PI*(173*t+97*t*t))`, encoded as AAC 64 kbit/s at 48 kHz.

- MP4: format origin 0; first video PTS 0 and DTS -0.083333; source video keyframes 0, 2, 4, 6 seconds.
- Current MKV file: format origin 0; actual first video packet PTS 0, initial DTS unavailable; video stream time base 1/1000. The earlier 21 ms MKV recollection was corrected after probing this exact file. Audio timestamps have millisecond quantization.
- TS: format origin 1.462000; first video PTS 1.483333 and DTS 1.400000. The first video presentation is therefore 21.333 ms after the format origin. A source-relative 2.37-second request maps to transport origin 3.832 seconds.

The principal request is 2.37 seconds. Source frame 57, at 2.375 seconds in MP4, is the first source video frame at or after this request. The required decoder preroll begins with frame 48 at 2 seconds. Video checks compare decoded frame hashes, not exit codes or packet counts. Audio checks compare a warmed chirp window with independently decoded full-source PCM at the requested sample position.

## Fragmented MP4 controls

The baseline flags were `frag_keyframe+empty_moov+default_base_moof+skip_trailer`, with `use_editlist=1` and `avoid_negative_ts=disabled`.

| Variant | Edit list | Default first decoded source frame | Explicit nonnegative presentation |
| --- | --- | --- | --- |
| `empty_moov` alone, input seek 2.37 | Absent | 48 | No correctly normalized edit domain |
| Add `delay_moov` | Present | 48 | 57 through 149, all hashes match |
| Add `copyts`, input seek 2.37, `itsoffset=-2.37` | Same video edit | 48 | Correct retained video dependency set |
| Seek input to 2.0 and offset by -2.37 | Same video edit | 48 | Correct retained video dependency set |
| Read from zero and offset by -2.37 | Larger edit, whole prefix retained | 0 | 57 through 149 after presentation trimming |
| Nonfragmented MP4, input seek 2.37 | Finite edit | 57 | Same correct frame sequence by default |

For `delay.mp4`, movie timescale is 1536000. Video timescale is 12288, first `tfdt=0`, first composition offset 1024, and video `elst.media_time=5571`, giving first PTS approximately -0.370036 seconds. Audio timescale is 48000, `tfdt=0`, and `elst.media_time=22624`, giving first packet PTS -0.471333 seconds and decoder `Skip Samples=22624`. The fragment edits use one entry, rate 1, duration 0. `mvhd`, `tkhd`, and `mdhd` durations are zero.

The default fragmented demuxer emits the preroll video packets without discard flags. `-copyts` preserves their negative decoded timestamps; explicitly selecting presentation timestamps at or after zero yields all 93 intended frames, each matching source frame 57 through 149. This explicit filter is diagnostic: an existing third-party client cannot be assumed to apply it.

The nonfragmented control marks preroll packets with discard flags and begins default video output at source frame 57. Its reported duration is 3.88 seconds. Changing the fragmented edit's duration from 0 to 5959680 movie ticks (3.88 seconds), without changing any offsets or box sizes, leaves default video output beginning at frame 48. The finite-duration edit therefore does not repair the fragmented demuxer behavior.

For the MP4 fractional request, copied audio's normalized MSE versus the correct source sample position is approximately `3.04e-10`; encoded AAC is approximately `1.08e-6`. Audio is already correct while the default video consumer still starts 370 ms early.

`copyts` plus `itsoffset` is not interchangeable with ordinary accurate input seek for mixed audio encoding. The naive mixed combination moved the encoded audio start to approximately 2.348667 seconds on the output timeline and shortened it. An explicitly controlled `noaccurate_seek` variant preserves audio preroll and lets the edit clip it correctly. These are measured argument interactions, not a recommended universal command.

## Container boundaries

- MP4 at an exact 2-second video keyframe works in this bounded `delay_moov` copy control: default first video frame is 48, and copied audio MSE is approximately `7.32e-10`. This is not enough to generalize exact-keyframe seeks to other demuxers.
- MKV input seek 2.0 can return the previous GOP at source frame 0. At 2.37 it retains source frame 48. Its copied and re-encoded audio can differ by small sample counts because of its coarse timestamps: measured best alignment deltas were -16 samples at the aligned copy seek, +8 for fractional AAC encode, and -8 for the buffered-prefix copy. Those offsets are recorded rather than called exact. Fractional copy at 2.37 happened to align at zero samples.
- TS copy requires `aac_adtstoasc`; without it the MP4 muxer rejects the AAC packet framing.
- Even with that filter, ordinary TS input seek 2.37 starts copied video at frame 96 (4 seconds), losing the requested region. This also occurs in the mixed AAC control.
- A controlled earlier TS seek at 1.9 seconds with `copyts` and offset -3.832 retains source frame 48; reading TS from zero with the same offset retains all preroll. Both yield the complete expected frame 57..149 sequence when presentation is explicitly clipped at zero. The earlier-seek amount is fixture-specific evidence, not a hardcoded safe margin.

## Open GOP and missing preroll

A separate 6.25-second source was encoded with x264 `open-gop=1`, three B-frames, `b-adapt=0`, and `b-pyramid=none`. Actual AVCC NAL parsing confirms an IDR NAL type 5 at time zero and non-IDR type 1 keyframe packets at 2, 4, and 6 seconds.

Both retained-preroll seek and full-prefix offset controls decode every displayed frame 57..149 identically to the complete source, with no fatal decoder errors. This proves this particular open-GOP request is usable when its dependencies are retained; it does not certify all non-IDR/key-flagged packets as random-access points.

The `copyinkf` / `copypriorss=0` control removes needed preroll. It exits zero and decodes without a fatal error, but begins at source frame 96, not 57. Frame-hash sequence checking detects the missing requested region. Dropping negative packets or changing sample flags cannot substitute for preserving real decoder dependencies.

## Buffered nonfragmented alternative

Waiting for a normal MP4 trailer and publishing a complete seekable file is a technically supported path for clients that need default edit handling. The MP4 input-seek control already demonstrates it. Additional full-prefix `copyts`/offset controls using nonfragmented `faststart` output establish:

- TS offset -3.832 with AAC framing conversion: default first video frame 57, all 93 video hashes match, audio MSE approximately `4.86e-10`.
- Open-GOP MP4 offset -2.37: default first video frame 57, all 93 hashes match, audio MSE approximately `2.84e-10`.
- MKV offset -2.37: video is correct, but audio is 8 samples early relative to linear source PCM; it is not considered an exact sample-aligned result.

This approach trades streaming startup for a complete private output and final sample tables. Reading the entire prefix preserves correctness but is not a bounded-preroll policy for long seeks. Production adoption needs an authorized-input cache, cancellation, disk quotas, validated source timestamp mapping, and a deliberate response policy while finalization is pending. An indexed, proven decoder dependency boundary can reduce the prefix; an approximate demuxer seek alone is not sufficient evidence.

## Standards and implementation evidence

- W3C requires support for a single rate-1 edit in the ISO BMFF initialization segment. Duration zero extends to subsequent media. The init sample tables must be empty: https://www.w3.org/TR/mse-byte-stream-format-isobmff/#iso-init-segments
- HLS fragmented MP4 requires zero `mvhd`/`tkhd` durations and movie-fragment-relative addressing: https://www.rfc-editor.org/rfc/rfc8216.html#section-3.3
- FFmpeg 9.0.1 writes fragmented `elst` duration zero and derives it from actual `start_dts/start_cts`: https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/movenc.c#L4133
- `delay_moov` obtains the timestamps that plain `empty_moov` lacks: https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/movenc.c#L8152
- `tfdt` is derived relative to the track's `start_dts`, not copied blindly from input DTS: https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/movenc.c#L5856
- FFmpeg fragmented demux disables advanced edit-list handling: https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/mov.c#L5355
- Chromium applies the first nonnegative edit media time through composition offsets; its code does not establish generic hidden-preroll support: https://github.com/chromium/chromium/blob/main/media/formats/mp4/track_run_iterator.cc#L315
- MSE coded-frame processing can drop pre-window frames and then require another random-access point: https://www.w3.org/TR/media-source-2/#sourcebuffer-coded-frame-processing
- ISO BMFF byte-stream random-access support is expressed using SAP type 1 or 2, not an arbitrary key flag: https://www.w3.org/TR/mse-byte-stream-format-isobmff/#iso-random-access-points

A bounded pre-publication edit would have standards support only when it computes the correct media-time mapping, preserves dependent samples, updates all affected box sizes/offsets, and keeps init metadata immutable after publication. The experiment already has the needed edit and proves that simply changing its duration does not fix consumer preroll handling. No custom init writer is proposed or implemented on the strength of these tests.

## Suggested next steps

Keep encoded seek as the reliable current route for arbitrary nonzero positions. Continue copy-seek development as two explicit paths: buffered normal MP4 with proven input/timestamp mapping, and fragmented edit-list output for concrete clients whose preroll presentation behavior has been tested. A precise-keyframe MP4 subset may be useful after additional content and boundary tests, but the MKV and TS counterexamples prohibit a container-agnostic claim. Do not advertise generic nonzero copied-video seek based only on muxing success or the existence of an edit list.

## Machine-readable artifacts

- `report.json`: initial eight muxer/edit controls and parsed atoms.
- `edit-interpretation.json`: default/copyts/explicit-presentation decoding and finite edit patch.
- `av-boundaries.json`: MP4/MKV/TS copy/mixed and aligned/fractional controls.
- `ts-offsets.json`: AAC framing and TS format-origin controls.
- `open-gop.json`: retained and missing preroll output sequences.
- `final-frame-verification.json`: every expected visible frame hash and open-GOP key NAL types.
- `buffered-copy.json`: nonfragmented prefix controls.
- `mkv-audio-quantization.json`: measured sample alignment deltas.
- `source-framehash.json`, `open-gop-source-framehash.json`, `reused-source-hashes.json`: input provenance.
