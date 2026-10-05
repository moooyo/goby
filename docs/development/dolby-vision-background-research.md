# Dolby Vision background clip research

Original research and first acceptance: 2026-10-04.
Profile extension: 2026-10-05.

Status: the original source/documentation study is complete, followed by an
approved implementation and bounded runtime acceptance. The original research
used repository reads and upstream documentation/source without executing jobs;
its findings below retain that historical scope. The web search gateway was
unavailable, so primary-source URLs were retrieved directly. Upstream source is
design evidence, not proof that deployed binaries changed.

## October 4 implemented and verified increment

The background generator now reuses Goby's strict software HEVC decode plus
Vulkan/libplacebo path, while keeping H.264 encoding on the CPU. Runtime policy
requires enabled Vulkan tone mapping, an admitted DRM render device, and matching
pinned tool identities. Profile 8.1 and complete Profile 7 MEL were the accepted
subset for this increment. Profile 5 was disabled; Profile 8.2/8.4 and FEL
reconstruction were not advertised. Failure does not silently fall back to
ignoring DV metadata.

The user explicitly authorized CT104. Eight profile/fixture scenarios completed
in 11 executions, including three output-retaining reruns, with no failures or
skips. Coverage includes P8.1/P7 MEL baseline output, neutral RGB, RPU-negative
controls, cancellation, and a default 25-second clip from an original 28-second
source. The final P8 bad-CRC and valid-CRC invalid-mapping controls produced no
output. The residual P7 BL+RPU control without EL was rejected at admission and
also produced no output; this is not full EL/FEL runtime-reconstruction evidence.
The [closure record](../../.artifacts/player-dv-20261004/evidence/closure.json)
binds cases, tool/source hashes, output locations, and cleanup; it records no
worker processes or render-device handles remaining. This is bounded media
generation evidence, not Dolby certification or every-profile/device acceptance.

## October 5 profile extension: completed native AMD media acceptance

The user selected Profile 5 first, then Profile 8.4 and Profile 8.2. Only Profile
7 FEL reconstruction is deferred. The background policy now has independent
admission switches for the three new profiles and enables them only after the
existing strict-toolchain and admitted-device dependency check. Source-bound
scan evidence and strict per-frame RPU validation remain mandatory. The prior
Profile 8.1 and complete Profile 7 MEL paths retain their existing checks.

The transform continues to use software HEVC decoding, Vulkan/libplacebo for
color processing, and CPU H.264 encoding. It receives the native Profile 5
IPTPQc2, Profile 8.4 HLG, or Profile 8.2 BT.709 SDR base pixels. RPU reshaping and
color conversion precede geometry and cadence changes. The internal reshaped
PQ working space does not mean that the source HLG/SDR pixels are relabeled as
PQ. This increment does not change the private renderer patch or introduce a
base-layer-only fallback.

The accepted corpus combines legally usable Dolby/Netflix Sol Levante Profile 5
and Profile 8.4 samples with original analytic sequences. The official
Sol Levante material is distributed under CC BY 4.0; its source revision,
attribution, license, and hashes are retained in the
[public source record](../../scripts/test-env/amd-media-fixtures/dolby-vision/extended_profiles/PUBLIC_SOURCES.md).
Original analytic sequences use each profile's actual native base encoding,
non-identity RPU processing, and an independent scalar conversion oracle.
The final BT.2390 SDR display mapping is shared with the product; the independent
oracle covers the DV signal conversion, not a separate end-to-end Dolby
reference renderer.
Profile 8.2 currently has only the original SDR-base/RPU analytic corpus; it
does not have a public commercial-source reference video. Analytic agreement
must not be represented as commercial-corpus coverage or Dolby certification.

Actual sources exposed three implementation defects beyond profile admission:

| Finding | Final correction |
| --- | --- |
| The background decoder could ignore an RPU with only CRC corruption. | DV inputs now explicitly set `-err_detect crccheck+explode`; ordinary SDR/HDR options are unchanged. Runtime corrupt-RPU controls produce no output. |
| Valid official `dvhe` MP4s have empty `hvcC` parameter arrays and carry parameter sets in video packets. The scanner previously treated their structural warning as `decoder_error`. | Only the exact `hevc_mp4toannexb` context and `No parameter sets in the extradata` message are permitted during syntax validation, after the complete access-unit and CRC scan succeeds. Other diagnostics, mixed messages, incomplete scans, and invalid RPU evidence remain failures. |
| Input seeking could skip the in-band VPS/SPS/PPS required by that packaging. | A successful scan persists optional `InBandParameterSets` evidence. These sources decode from the beginning, trim to the requested interval, then enter strict DV processing. Ordinary sources retain fast seeking. The additive field is omitted when false and does not invent evidence for older records. |

No private renderer patch or toolchain rebuild was needed. The final focused
suite on `test-env` passed 41 top-level tests containing 153 subtests, with no
failures or skips; these are hierarchy counts, not 194 distinct tests. The Go
backend build also passed.

The final CT104 matrix passed all 21 required cases with no skips:

| Coverage group | Cases | Result |
| --- | ---: | --- |
| Profile 5, 8.4, and 8.2 analytic colors, default 25-second window, runtime bad-frame rejection, and cancellation | 12 | Passed for each profile, using native base encoding and independent scalar color oracles. |
| Official Profile 5 and 8.4 sources, 25-second windows beginning at 60 seconds | 2 | Passed after full source admission and in-band-parameter handling. |
| Same-scene consistency between the official Profile 5 and 8.4 versions | 1 | Passed the bounded cross-profile comparison. |
| Legacy Profile 8.1, complete Profile 7 MEL, 25-second window, bad CRC, invalid mapping, and residual-input rejection | 6 | Passed without expanding FEL reconstruction support. |

The complete retained history has 33 executions: 29 passes and four failures
from before the fixes. Those failures remain part of the record. The final
[case summary](../../.artifacts/player-dv-profiles-20261005/final/evidence/case-summary.json)
binds results to source/oracle/tool/test identities. The
[closure record](../../.artifacts/player-dv-profiles-20261005/final/closure.json)
confirms all expected cases passed, no changed evidence files or integrity
errors, and no remaining worker processes, render-device handles, or temporary
entries. The detailed [review](../../.artifacts/player-dv-profiles-20261005/REVIEW.md)
retains failure history and acceptance limits. The October 4 evidence is not
recounted as new Profile 5/8.4/8.2 coverage.

The same-scene official Profile 5/8.4 comparison measured RGB mean absolute
error 4.7441 on a 0–255 scale and spatial luma correlation 0.996525. It checks
consistency between independently published versions, not an independent SDR
master. The evidence archive SHA-256 is
`53e86376e58b3a162bdf2a08878b29a6d4f0d8bcd94236302b015ebf2d915bd3`.

Final binary identities are:

| Artifact | SHA-256 |
| --- | --- |
| Media test binary | `a90202cf8816a56ff2ed101784d570f60301c9c130c1526afb98d68c60e5f88f` |
| Go backend binary | `01b3433430d82f202ae7e8d837adadf04bf1f27401f7b828477ca14bfb678017` |

The official 25-second Profile 5 output is 4,666,702 bytes and its accepted
run took 28.095 seconds. Profile 8.4 produced 4,659,991 bytes in 28.013 seconds.
These elapsed times include setup and output validation; they are not isolated
encoder benchmarks or estimates for arbitrary sources and devices.

The output remains a silent H.264, 8-bit BT.709 SDR MP4 with a 25-second default.
Generation remains disabled by default. Completed source-adjacent artifacts
remain permanent and are replaced only by an explicit Force operation. This
work neither adds browser playback of the original DV file nor preserves DV in
the generated output. This increment verifies the native AMD media path, not a
new Docker deployment. It makes no claim for all devices, Windows, CPU Vulkan,
or universal GPU/container deployment.

The three profile switches are internal runtime policy, not new administrator
UI settings. Previously rejected `failed` queue entries are not automatically
requeued by an upgrade. If RPU evidence was already verified and only the old
profile gate rejected generation, the existing generate-missing-clips action
or a new background request with `Force: false` can retry it. Sources whose
saved RPU validation reason is `decoder_error` must first finish **Libraries >
Refresh media details** (`ForceProbe: true`), then retry generation. Existing
artifacts remain reusable. No cache clearing or sidecar migration is needed;
Force is reserved for deliberately replacing a completed clip.

The current contract is [background previews](../api/background-previews.md).
The remainder of this document records the preceding research and recommendations;
statements about the repository below describe that research-time snapshot.

## Research recommendation before implementation

Generating a silent, 25-second, 720p H.264 SDR background from supported Dolby
Vision input is feasible. Goby already has most of the difficult color-processing
and source-admission work in its playback transcoder. The background generator
does not use that work yet. This is principally a bounded integration and media
acceptance task, rather than a new Dolby Vision implementation.

Reuse the existing strict software-HEVC-decode plus Vulkan/libplacebo processing
path. Keep H.264 encoding on the CPU initially, matching the existing background
output contract and avoiding an additional hardware-encoder matrix. Treat
Profile 8.1 and the already tested complete Profile 7 MEL subset as the first
acceptance baseline. Include Profile 5 only after a genuine Profile 5 color and
nonzero-seek corpus passes the new background and container gates. Profile 8.2
and 8.4 also need their own material before being advertised. Do not claim FEL
residual reconstruction.

A separate, explicitly selected compatible-base-layer fallback could later
cover more CPU-only installations. It would intentionally generate an SDR
derivative from the compatible HDR10, HLG, or SDR image while discarding Dolby
Vision enhancements. This is a product quality policy, not the equivalent of
the strict Dolby Vision renderer. It must never be silently applied to Profile
5 or inferred from the filename or a generic `DOVI` label.

## Research-time repository boundary

- [`backgroundClipColorFilter`](../../internal/media/background_clip.go) rejects
  any stream with Dolby Vision metadata or `VideoRange == "DOVI"`. Its HDR10 and
  HLG path uses software `zscale` and `tonemap`; removing the rejection alone
  would not implement Dolby Vision.
- [`BackgroundClipAvailability`](../../internal/media/background_clip_availability.go)
  proves the ordinary SDR software pipeline using an actual finite synthetic
  encode and decode at startup. It explicitly makes no hardware or Dolby Vision
  claim. Its single `Available` result cannot serve as the DV capability gate.
- [`videoProcessingPlanForRange`](../../internal/playback/progressive_video_filters.go)
  admits HEVC profiles 5, 7, and 8 only with consistent profile, bit depth,
  compatibility, layer flags, and verified per-frame RPU evidence. Profile 8
  compatibility IDs are limited to 1, 2, and 4. Profile 7 is further constrained
  by the strict renderer at frame processing time.
- [`video_gpu_filters.go`](../../internal/transcode/video_gpu_filters.go) applies
  the RPU with `apply_dolbyvision=1`, the private strict options, explicit output
  color, and `tonemapping=bt.2390:peak_detect=0`. It removes obsolete source HDR
  metadata after the pixels have been transformed. Dolby Vision uses software
  HEVC decoding so per-frame metadata reaches the renderer.
- The [private patch contract](../../scripts/test-env/toolchain-patches/README.md)
  requires raw and parsed RPU data on every frame. It checks a bounded metadata
  representation and rejects missing, mixed, malformed, or unsupported data.
  It accepts Profile 7 only when all three components satisfy the exact
  zero-residual MEL predicate. Nonzero residuals and FEL reconstruction fail.
- The [native AMD acceptance record](amd-media-phase1-20260919.md) includes real
  Profile 8.1 and complete Profile 7 MEL SDR/HDR10 chart checks. That record does
  not cover real Profile 5, arbitrary Profile 8, or Dolby color certification.
- The [AMD OCI profile](../../deploy/oci/README.amd.md) includes the private
  FFmpeg, matching libplacebo ABI, Vulkan/Mesa libraries, non-root render-device
  access, and cleanup controls. It explicitly does not claim a new Dolby Vision
  corpus acceptance for that container. Existing background Docker acceptance
  likewise establishes SDR/HDR10/HLG, not DV.

## Input differences that matter

`BL` is the base video layer, `RPU` is the per-frame metadata used for reshaping
and display management, and `EL` is an enhancement layer. A decodable HEVC base
layer and the presence of DV tags are insufficient evidence of correct DV pixels.

| Input | Compatible image without full DV processing | Background implication |
| --- | --- | --- |
| Profile 5 | No ordinary HDR10-compatible base; proprietary IPT-PQ representation | Apply the actual RPU/color transform. Relabeling the stream as BT.2020/PQ can produce severe color errors. |
| Profile 7 MEL | HDR10-compatible BL; enhancement residual is trivial for the admitted MEL subset | Reuse Goby's verified strict MEL path. A BL-only image is a deliberate loss of DV processing. |
| Profile 7 FEL | HDR10-compatible BL; enhancement residual contributes to the full DV image | Goby does not reconstruct the residual. Reject strict rendering, or use a separately approved HDR10-base fallback. |
| Profile 8.1 | HDR10-compatible BL plus RPU | Natural first target for the existing strict renderer; possible explicit CPU HDR10 fallback. |
| Profile 8.2 | SDR-compatible BL plus RPU | Check compatibility and real material separately; never assume all Profile 8 is PQ. |
| Profile 8.4 | HLG-compatible BL plus RPU | Check separately; an explicit BL fallback requires the HLG conversion path. |

The compatibility distinctions are supported by FFmpeg's
[configuration implementation](https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/dovi_rpuenc.c):
Profile 5 uses proprietary IPTPQc2 with compatibility ID 0, while Profile 8
selects ID 1 for BT.2020/PQ, ID 4 for BT.2020/HLG, and ID 2 for BT.709 SDR.
The primary [dovi_tool generator documentation](https://github.com/quietvoid/dovi_tool/blob/main/docs/generator.md)
also identifies 8.1 as an HDR10 base layer and 8.4 as an HLG base layer.
Profile 7 layer and MEL/FEL restrictions above follow Goby's inspected strict
contract and its linked upstream predicates; they do not imply full dual-layer
support in general FFmpeg builds.

## Implementation approaches

| Approach | Dependencies and expected effort | Result and limits |
| --- | --- | --- |
| Reuse strict DV rendering | Existing private FFmpeg/libplacebo, an admitted Vulkan device, and background integration; medium effort | Uses supported RPU data and produces SDR. Best fit with the existing safety and playback contracts. |
| Compatible-base-layer fallback | Software HEVC decode plus current SDR/HDR10/HLG filters; lower processing integration effort, but a new admission and quality policy | Can work without a physical GPU for correctly identified compatible sources. Discards DV-specific processing and cannot cover Profile 5. |
| Full Profile 7 FEL reconstruction | Additional EL decoding, precise BL/EL synchronization, residual processing, and independent color validation; high effort | Outside the current renderer and unnecessary for a small decorative background. Do not include in the first increment. |
| Software Vulkan device | A compatible software Vulkan implementation, additional deployment and performance validation | A potential experiment, not an accepted CPU-only replacement or a performance promise. |

Upstream [libplacebo](https://github.com/haasn/libplacebo#readme) advertises Profile
5 conversion to SDR or HDR/PQ, DV side-data handling, and reshaping. Its renderer
uses GPU APIs; the FFmpeg integration is Vulkan based. This demonstrates that
the transformation is available, but does not validate a particular Goby input,
device, image, or pinned patch.

The [FFmpeg filter documentation](https://ffmpeg.org/ffmpeg-filters.html#libplacebo)
says `apply_dolbyvision` consumes source-frame RPU metadata. The reshaped signal
is BT.2020/PQ, so the background graph must explicitly request BT.709 SDR rather
than relying on automatic output color selection. Upstream's permissive default
does not replace Goby's strict admission and publication rules.

The [dovi_tool options](https://github.com/quietvoid/dovi_tool#all-options) include
RPU conversions and removal of the enhancement layer; one conversion removes
Profile 7 FEL luma/chroma mapping. Such metadata conversion is not proof that
the lost enhancement residual was reconstructed. Adding a preprocess command
that relabels a file as Profile 8.1 would therefore not implement full FEL support.

## Proposed integration boundary

1. Derive a closed, reusable video-color plan from existing media facts and RPU
   evidence. Share its source-admission and strict rendering rules with playback
   without moving HTTP playback, watch-history, or session behavior into jobs.
2. Add a distinct background DV capability that binds the selected toolchain,
   required strict filter options, device identity, and actual bounded color
   processing result. An unavailable DV capability must leave ordinary SDR
   generation usable and explain why a DV source was skipped.
3. Build the graph in this order: authorized source and nonzero seek; software
   HEVC decode retaining RPU; strict DV reshaping and explicit SDR conversion;
   display geometry and final resize; 24 fps; H.264/yuv420p encoding with no audio.
   Do not apply the current generic background resize or side-data deletion
   before the DV transform. Geometry and metadata propagation need explicit tests.
4. Reuse existing process limits, the serialized background work slot, source
   ownership checks, cancellation/join behavior, finite full-decode validation,
   and source-adjacent publication. Integrate the established GPU process/device
   lifecycle rather than creating an unbounded side process.
5. Preserve source selection, the default-off library switch, 25-second default,
   persistent artifact policy, and playback priority. Existing finished clips
   remain unchanged until explicit regeneration. Unsupported DV must fall back
   to the next configured background source, not publish a miscolored video.

The proposed graph and effort ratings are engineering inferences from the
inspected implementation. They have not been implemented or benchmarked here.
The Vulkan filter itself is not inherently AMD-only, but Goby's documented
device and deployment acceptance currently covers its AMD profile. Do not
extend that acceptance to Intel, NVIDIA, Windows, or CPU Vulkan automatically.

The output storage budget remains approximately 4.7 MB (4.5 MiB) for 25 seconds
at 1.5 Mbps, plus container overhead; Dolby Vision input does not require Dolby
Vision output or a larger output bitrate. Decode and color-processing memory,
elapsed time, and GPU contention depend on source resolution, GOP structure,
driver, and device. No timing estimate is justified without a remote benchmark.

## Original proposed acceptance matrix

This was the broader research matrix before the bounded implementation was
selected. The completed October 4 scope was the Profile 8.1/complete Profile 7
MEL increment documented above. Profile 5, Profile 8.4, and Profile 8.2 are now
explicitly selected for the separate October 5 extension. FEL reconstruction
and additional deployment/performance matrices below are not implied remaining
obligations of either increment.

Before enabling any profile, use the designated remote verification environment
and a source-controlled test plan with identified, legally usable media:

- Genuine Profile 5; Profile 8.1; complete Profile 7 MEL with actual BL/EL/RPU;
  FEL with a nonzero residual; and separate 8.2/8.4 inputs if offered. Container
  tags alone or a Profile 8 RPU relabeled as Profile 7 are not sufficient.
- Test normal and nonzero seeks, including the actual 25-second default window,
  scene boundaries, RPU reuse, and non-identity RPU transforms. Compare against
  independent/reference pixels and an RPU-disabled negative control. Inspect
  skin tones, neutrals, highlights, and dark areas; metadata tags alone cannot
  establish color correctness.
- Reject missing middle-frame RPU, CRC damage, valid-CRC malformed mapping,
  profile/layer mismatch, unsupported coefficient representations, and FEL
  residuals before any artifact publication. Retain the existing strict corpus.
- Inspect complete output: H.264, yuv420p, BT.709 SDR with matching pixel
  interpretation, correct aspect ratio and dimensions, expected 24 fps duration,
  no audio, no stale DV/HDR side data, and complete software decoding.
- Exercise the actual non-root AMD container and selected render node, plus
  unavailable-device/driver/filter cases. Record executable and image identity,
  actual GPU participation, peak memory, wall time, and playback contention.
- Cancel during decode and GPU work, fail before publication, and explicitly
  regenerate an existing clip. Verify child processes and device descriptors
  retire, old artifacts survive failure/cancellation, and the newly published
  generation gets its distinct versioned URL.

Historical playback and container evidence reduces the implementation risk;
none of it substitutes for this new background-specific acceptance.
