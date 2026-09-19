# AMD video processing implementation

This document describes the phase 1 implementation and its acceptance gates.
The presence of a plan, filter, encoder, or compiled interface is not evidence
that a particular AMD adapter has passed the gate. Record remote execution
results in the phase acceptance report before marking a combination verified.

## Execution paths

| Operation | Pixel processing | Encoding and transfers |
| --- | --- | --- |
| HDR10/HLG to SDR | Vulkan `libplacebo` with explicit source color metadata | Software or VAAPI; VAAPI input is downloaded before Vulkan, and VAAPI output is explicitly uploaded |
| Dolby Vision to SDR or HDR10 | Software HEVC decoding retains per-frame RPU metadata; the strict private `libplacebo` path applies reshaping and color conversion | Software or VAAPI; HDR10 output requires a 10-bit HEVC/AV1 plan |
| Interlaced SDR | Vulkan `libplacebo` YADIF with explicit field parity and one output frame per field pair | Source-sized frames are downloaded before final resizing and optional encoding upload |
| Interlaced HDR | Vulkan `libplacebo` YADIF and color conversion | Preserves input frame cadence instead of silently doubling it |
| Text subtitle burn | CPU libass renders a transparent plane with source timestamps; Vulkan `libplacebo` composites the plane | Includes explicit CPU/GPU transfers; it is not an entirely GPU-resident path |
| Bitmap subtitle burn | An independent subtitle demuxer reads the same authorized source; CPU framesync samples the transparent bitmap plane on the video clock, then Vulkan composites it after source processing | Video and subtitle clocks retain microsecond event boundaries; the final resize applies to both layers together |
| Progressive MP4 subtitle burn | The same software or Vulkan composition paths as HLS | Subtitle timestamps use the source clock before the progressive output trim |

VAAPI and Vulkan are derived from the same selected DRM render node. The
filter device is Vulkan; a final `hwupload=derive_device=vaapi` resolves through
the DRM ancestor for VAAPI encoding. No unverified zero-copy interop is assumed.
NV12 and P010 downloads are selected from probed source bit depth, independently
of the requested output bit depth. Ordinary VAAPI decoding also downloads
frames for exact CPU resizing before an optional VAAPI encoding upload. This
avoids the worker's VAAPI VPP geometry and picture-submission failures while
preserving hardware decoding and encoding as separate, explicit stages.

Software HDR conversion retains the existing linear-light `zscale`/`tonemap`
path. Software subtitle rendering and bitmap overlay remain available when
AMD processing is not selected. Backend selection does not claim that an
unavailable driver, unsupported codec, or failed device will run successfully.

The initial gfx1150/Mesa 25.0.7 worker diagnosis found that both VAAPI
MotionAdaptive and Bob deinterlacing failed at picture submission, including
a legal 320x192 canvas. Vulkan YADIF followed by VAAPI encoding completed that
same bounded workload. Automatic AMD selection therefore uses Vulkan for SDR
deinterlacing too; a listed VAAPI VPP mode is not advertised as a working path.

Bitmap events use an independent demuxer because sharing the video input can
let `sub2video` heartbeats overtake caption updates. The demuxer reopens the
existing authorized descriptor and discards video/audio. It reads subtitles
linearly, retaining a bitmap that was already active at the requested seek.
Progressive input uses the probed format origin; HLS uses the conversion start
origin. The requested subtitle offset is applied once after that normalization.
The independent subtitle pass can read source data preceding the seek; it does
not claim indexed subtitle seeking.

A transparent copy of the primary video's timeline supplies the bitmap plane's
cadence. Both plane inputs use `AVTB` so a heartbeat one microsecond before a
caption is distinct from that caption, even when video has a millisecond time
base. CPU overlay affects only this transparent plane, with no repeated final
subtitle after EOF; the final video/subtitle blend remains Vulkan. Sparse
caption/clear events therefore do not create extra video frames or change the
primary video pixels before composition.

## Dolby Vision admission

The scanner preserves the DOVI configuration record and separately establishes
RPU evidence. It first stream-copies the selected HEVC stream through
`hevc_mp4toannexb,hevc_metadata=aud=insert,filter_units=pass_types=35|62`.
Every access unit must have exactly one supported layer-zero, temporal-zero
RPU, a complete bounded header, and a valid CRC envelope. Known configuration
compression must agree with each RPU's reuse and metadata-compression flags.
In a second pass, the same AUD/RPU filtering precedes FFmpeg's native
`dovi_rpu=compression=none` bitstream filter. That makes the RPU the final NAL
of every packet, ensuring the filter parses each full mapping. Its rewritten
packets go to the null muxer; no pixels are decoded. Any warning, error, or
unexpected stdout invalidates the syntax evidence. CRC and configuration
consistency are checked independently because the native bitstream filter does
not enable the decoder's optional CRC/CAREFUL flags.

The earlier `skip_frame=all` approach is not used: the pinned HEVC decoder can
retain a raw RPU attachment without producing a frame and incorrectly warn that
the next access unit contains duplicate RPUs. The replacement does not suppress
that warning or modify the decoder.

Evidence is cached with the scanned source's media information and file
identity/change facts. PlaybackInfo does not repeat the scan. The two passes
share a source-sized budget of two minutes plus one second per 32 MiB, capped
at fifteen minutes, with a 256 MiB RPU output bound. A timeout, output limit,
missing RPU, or parser failure keeps the media importable but does not establish
a Dolby Vision conversion path. Cancellation still propagates.

| Input | Admission and render behavior |
| --- | --- |
| HEVC profile 5 | Configuration and complete RPU header profile must agree; residuals must be disabled |
| HEVC profile 8 | Configuration and complete RPU header profile must agree; supported compatibility IDs are 1, 2, and 4, with no enhancement layer and disabled residuals |
| HEVC profile 7 | Complete profile 7 RPU syntax is required; the strict renderer checks all three NLQ components and accepts only the exact zero-residual MEL predicate |
| Profile 7 FEL or other nonzero residual | Rejected by the strict renderer; reconstruction of enhancement-layer residuals is not implemented |
| Mixed profiles, mixed residual flags, incomplete/contradictory metadata | Rejected before conversion selection |
| Newly encoded Dolby Vision output | Not implemented; output is SDR or 10-bit BT.2020 PQ HDR10 |

Real profile 7 MEL has `disable_residual_flag=0`. A profile 7 container with
profile 8 RPU headers is not evidence of MEL support. The private FFmpeg patch
checks the exact NLQ zero-residual condition, copies the complete metadata
buffer, and normalizes only that copy for libplacebo. Original curves, color
matrices, extensions, and raw RPU bytes are preserved. Every decoded frame
must also carry its own raw RPU, so inherited decoder metadata cannot hide a
missing access-unit RPU.

See [the private patch audit](../../scripts/test-env/toolchain-patches/README.md)
for the pinned upstream sources, safety bounds, normalization details, and
unsupported coefficient representations. The installer binds patch bytes to
the recipe and publishes a separate toolchain prefix; an existing toolchain
binary is not replaced in place.

## Acceptance gates

All commands below belong on the designated remote environment. They have not
been run merely by adding this document or the tests.

- Unit coverage checks closed enums and source facts, profile/RPU mismatches,
  device derivation, filter ordering, real output bit depth, caption alpha
  planes, and the distinct HLS/progressive subtitle clocks.
- `TestProgressiveSubtitleBurnActualSeekAndOffsetPixels` checks caption pixels
  and frame count across a nonzero seek and subtitle offset. Its software case
  uses `GOBY_FFMPEG` and `GOBY_FFPROBE`; its GPU case additionally requires
  `GOBY_TEST_VAAPI_DEVICE`.
- `TestAMDVideoProcessingActualHDRAndDeinterlace` checks changed pixels,
  progressive frame flags, and output color metadata on the selected AMD
  device. Actual codec/decode combinations still need their encoding gates.
- `TestBitmapSubtitleActualGPUClockCanvasAndClear` authors a small PGS stream
  and checks actual progressive/HLS GPU output: 40 frames at 16 fps after a
  two-second seek, exact canvas placement, a positive subtitle offset, and
  explicit display/clear pixel windows. Its strict gate passed in the separate
  GPU10 scope after independent subtitle input and microsecond framesync were
  added. Earlier GPU04/GPU05/GPU06 failures and diagnostics remain separate.
- `TestGPUProducerCancellationRetiresActualFFmpegAndClosesOutput` observes a
  real, paced Vulkan/VAAPI producer after media is ready, binds its process
  identity with a pidfd, checks its executable/output directory/render-device
  descriptor, then verifies cancellation, process exit, immutable output prefix,
  and closed output descriptors. This gate passed in GPU04.
- `TestDolbyVisionActualRPUChangesPixelsAndProducesSDRAndHDR10` requires
  `GOBY_TEST_DOLBY_VISION_FILE`, a complete clip of at most ten seconds with
  non-identity real RPU data. It compares RPU-enabled and disabled pixels and
  inspects the encoded output. A source with only DV tags cannot pass.
- `TestDolbyVisionStrictRendererRejectsActualNonzeroResidual` requires
  `GOBY_TEST_DOLBY_VISION_FEL_FILE` with a nonzero residual at its first frame.
  It must fail before publishing a playable result.
- Independently compare the fast RPU evidence with full decoded-frame metadata
  for valid and damaged MP4/MKV fixtures. Verify missing middle RPUs, CRC damage,
  profile mismatches, and CRC-valid malformed mapping syntax. Verify that the
  native bitstream filter parses every RPU after AUD/RPU filtering.
- `TestDolbyVisionActualRPUCoverageCRCAndNativeSyntax` exercises those production
  gates with the complete fixture and its missing-RPU/CRC controls. Set
  `GOBY_TEST_DOLBY_VISION_BAD_SYNTAX_FILE` to a separate output from
  `mutate_syntax.py` to prove that an intact CRC and complete per-frame RPU
  coverage cannot bypass native mapping-syntax validation. The mutator changes
  a middle frame's pivot count, recomputes its CRC, and preserves MP4 sample and
  NAL lengths. It creates a new file and never replaces its input.
- Confirm that patched and original metadata references remain independent,
  and compare zero-residual MEL output with an independently normalized
  reference. Deliberately incomplete BL-plus-RPU synthetic fixtures validate
  the filter boundary only; they do not establish complete dual-layer playback.

Fixture generation and provenance are documented in
[the Dolby Vision fixture directory](../../scripts/test-env/amd-media-fixtures/dolby-vision/README.md).
