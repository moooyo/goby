# Progressive Copy Seek Compatibility

Status: selected remote copy-seek media and native-browser scopes passed;
the phase 1 increment has completed its recorded verification and closeout. No local verification
was performed. See the [phase 1 record](amd-media-phase1-20260919.md).

## Playback contract

Progressive MP4 can retain H.264, HEVC, or AV1 video packets at a proven random
access point. Existing exact requests retain their zero-normalized output clock.
Clients may explicitly enable `AllowVideoSeekAlignment`; standard profile planning
can also select alignment when it publishes a matching `CopyTimestamps=true` URL.
The planner can then choose the nearest preceding proven point within ten seconds.
The plan and returned URL use the actual aligned `StartTicks`. Private preparation
data also retains the original requested position.

`CopyTimestamps` is an actual muxing contract: output-side trimming still selects
the same source window, then `-output_ts_offset` restores its normalized source
position. The logical source clock removes the probed container origin. Fresh
copy verification uses that same output offset and checks native packet PTS/DTS.
The MP4 muxer carries the timeline through its delayed initialization/edit list;
the readiness reader never rewrites it. A URL flag by itself is insufficient.

Supported copied restarts have equal native presentation and decode timestamps.
Exact requests still require the source position to be exactly representable in
playback ticks. An aligned `CopyTimestamps` request may round its public start
position down by strictly less than one 100 ns tick, while retaining the original
native packet time and hashes in its private evidence. This includes common
MPEG-TS 1/90000 boundaries. Its expected output PTS/DTS is calculated from the
original native position minus the source origin, never from the rounded public
tick value. The source origin must itself rescale exactly on every copied stream's
native clock; native-tick rounding is not part of this allowance.

Reordered starts, open-GOP access points, unknown configuration changes, or
unsupported packet side data do not authorize a copied restart. The planner may
use the permitted video/audio encoding path instead. A failed fresh
proof after an immutable copy job has been created fails before output is
published; it cannot silently change that job into an unverified copy.

## Evidence and codec boundaries

- H.264 uses the existing IDR, static SPS/PPS, full packet-side-data, and NAL-scope
  evidence. The production restart must emit the indexed IDR first.
- HEVC Main/Main 10 uses IDR NAL types 19 and 20, static VPS/SPS/PPS, full packet-side-data and
  NAL-scope evidence. Fresh verification also checks independently decoded pixels.
  CRA, BLA, and a container key flag alone do not establish this contract.
- AV1 Main at 8 or 10 bits pairs candidate key packets with decoded pictures, checks complete packet
  side data and static sequence-header OBUs across the source, and validates the
  retained packet bytes against a complete in-band sequence header followed by a
  displayed key frame. The syntax reader accepts one operating point with no OBU
  extensions. A final ISOBMFF frame or tile-group OBU may omit its size and consume
  the remaining sample; sequence/configuration OBUs still need explicit sizes.
  Runtime verification matches the complete packet and independently decoded
  pixels. External extradata alone does not authorize an AV1 restart.
- When HEVC/AV1 probing omits scalar bit-depth fields, the explicit supported
  decoded pixel format supplies the component depth (`yuv420p` or `yuv420p10le`).
  Missing pixel formats and contradictory scalar/pixel-format depths remain
  unsupported; the complete packet and decoded-picture proofs still apply.
- AAC LC can be copied when a scanned audio packet starts on the identical native
  source clock as the video restart, has no priming/padding/configuration side
  data, and still matches its packet hash, duration, and timestamp during fresh
  verification. Video and audio each receive a strict bounded proof invocation
  with identical input-seek, output-trim, and output-clock arguments. Both must
  succeed inside the same source/tool identity checks and timeout. This avoids
  a one-packet audio limit terminating FFmpeg before a video branch has flushed.
  Both copied streams then use the same preflighted production input seek.
  Otherwise permitted AAC encoding retains the independent linear audio input.

The scanner supports authorized self-contained source containers; each actual
container and clock layout must establish the same evidence. A container name
alone is never a copy-seek capability claim. Scanning is optional and bounded by
time, output, pixel, entry, and candidate-size budgets. Missing, stale, unsupported,
or budget-exhausted indexes leave the source available for ordinary playback and
encoding. Source filesystem identity and executable identity are checked again
immediately before publication.

## Acceptance evidence

The phase-one test suite includes real HEVC and AV1 media in MP4 and Matroska,
H.264/HEVC MPEG-TS input with a fractional 1/90000 boundary, source-frame comparisons
after copying, strict full-output decoding, native-clock AAC packet alignment,
and wrong/missing evidence rejection. `copy-repair03` passed its selected actual
media cases after separating the bounded video and AAC preflight processes.
`browser02` passed the native HTMLMediaElement source-clock and report journey,
and `http-media01` passed the selected modern-codec HLS source-timeline cases.
These are distinct scopes with overlapping test/subtest counts. They are not
full Emby Web, arbitrary source or full-length playback acceptance.
Source/tool identities, earlier failed attempts and remaining phase gates
belong to the phase 1 record.

## Standard browser acceptance procedure

Run this procedure on the designated remote verification environment using the
supported official Web client and its ordinary playback controls. Record the
client build, browser build, server revision, media source, and returned URL.

1. Use a deterministic changing-video/audio fixture with known closed GOPs and a
   seek target between GOPs. Log in as an ordinary test user and play through the
   normal detail page. Do not add a custom alignment query parameter manually.
2. Seek to a position between keyframes. Capture the standard PlaybackInfo result
   and media request. If the result selects aligned copying, verify that the URL
   has the real aligned `StartTimeTicks` and `CopyTimestamps=true`, and that the
   persisted job contains the matching copy candidate and output-clock contract.
3. Capture the actual media response and inspect packet PTS/DTS and decoded frame
   times with FFprobe. The first video time must equal the real aligned logical
   source point, not zero and not the arbitrary container's raw timestamp origin.
   Decode the full output and compare its pictures with the corresponding source
   window. Repeat with a source having a nonzero or negative container origin.
4. Read the real `HTMLMediaElement.currentTime` after playback settles, compare it
   with the client's visible progress and the outgoing playback-position report,
   and confirm that the client does not add the original seek position again.
   The visible picture/content position and the reported source position must agree.
5. Seek again, pause, resume, stop, and resume from the saved position. Confirm
   that the current source position survives each transition and that cancelled
   jobs retire. Also cover explicit `CopyTimestamps=false` and an unsupported
   copy boundary, which must retain their exact/encoding behavior.

The standard client offset behavior and the inspected upstream source versions
are documented in `media-client-timestamps.md`. Browser observations supplement
the independent packet/frame checks; neither substitutes for the other.
