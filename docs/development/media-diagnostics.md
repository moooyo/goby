# Native media diagnostics increment

This independent M5 increment adds an administrator entry for actual decode,
encode and combined execution results. It is being prepared on
`codex/m5-media-diagnostics`, based on `d77b988`, outside the frozen Programs
artifact and the separate user-deletion branch. No runtime or M5 acceptance is
claimed by this preparation.

## Complete intended behavior

Settings will place an explicit diagnostic action beside Deployment configuration.
The administrator selects the software baseline or the configured hardware
profile, starts one bounded run, observes each stage, and can cancel it. Completed
stage facts remain visible after cancellation or a later-stage failure. Missing
devices, unavailable tools, failed execution, unverified output, cancellation
and stages not attempted must remain distinct.

The software baseline includes H.264 video and AAC audio. Hardware video paths
cover the configured VAAPI, QSV or CUDA decoder and VAAPI, QSV or NVENC encoder.
Each selected path has separate decode, encode and combined stages. Raw samples
are fixed, small, deterministically generated data; compressed references must
be produced and checked by the actual selected software toolchain. No arbitrary
media path, URL or FFmpeg argument is accepted from an API caller.

Decode uses a compressed reference and checks actual decoded media. Encode uses
raw media and validates its encoded output. Combined must execute the selected
decode/encode chain and validate the output. A software output verifier does not
prove hardware decoding. AAC encoder delay and padding must use an observed
prepared reference, not a guessed byte count. Hardware failure cannot silently
become software success.

Results must bind sample identity, actual executable identity/version, selected
profile/device, stage timing, measured output/frame or sample facts, and owned
process closure. A pass is scoped to that codec/profile/sample and configuration;
it does not establish every hardware/media combination or change broad support
claims without their own acceptance. Configuration and capability enumeration
remain separate from these execution results.

## Implementation boundaries

The first code increment is the fixed sample and command-plan layer with pure
contract tests. It does not execute FFmpeg, allocate a playback identity or create
an administrator operation. The subsequent executor, admission/cancellation,
result retention and Settings UI must still be implemented and verified before
this feature is complete.

Reuse the existing process-group and borrowed-file-descriptor primitives. Do not
create a second Manager against the live transcode repository: its recovery path
would interrupt existing jobs. Do not fabricate user/auth/play/item records or
library task children for diagnostics. Existing task UI interactions may inform
polling and cancellation, but their scan-specific data model is not the result
contract for media stages.

Execution must use isolated environment/TMP/cache locations, bounded concurrency,
input/output and stderr bytes, a total deadline, cancellation and shutdown joins.
Thread counts alone are not a memory or storage bound. Admission must account
for other conversion work and use current administrator authority; unknown
submission outcomes require lookup before another decision. Public errors expose
safe codes, not raw child stderr or host environment content. Runtime-result
retention and request-identity lifetime must be fixed before exposing a run API.

The fixed raw-sample generator and closed three-stage command plans are now
implemented in `internal/media/diagnostic_sample.go` and `diagnostic_plan.go`,
with twelve pure test cases in their paired test files. Compressed specifications
use `RequiresPreparation`; their declared properties never mean that compressed
bytes or decoded-reference evidence already exist. Plan validation rebuilds the
closed contract and rejects changed arguments, bounds or verification requirements.

Independent static review found no blocking issue in this component. The four
Go files were formatted on `ssh test-env`; no Go test, build, FFmpeg or media
execution ran. The source-preparation record is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/media-diagnostic-core-20260916-01/source-preparation.json`
(4,029 bytes, SHA-256
`dbfa4453f4fe10dcc78000ee4acca307038208819f7a2ea02aa4fafc817b1626`).
The five-file snapshot precedes this evidence note and is not an execution input.
Executor, resource/authority enforcement, actual result validation, API and UI
work remain open; this component does not satisfy those requirements by itself.

## Available evidence and remaining work

Read-only SSH metadata found no `/dev/dri`, NVIDIA device node, or loaded
`i915`, `xe`, `nvidia` or `amdgpu` module in the observed host namespace. This
does not establish a hardware acceptance profile or service access. The result
is retained at
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/media-diagnostic-host-prerequisites-20260916-01.json`
(725 bytes, SHA-256
`b909c249073c2aade9cc470c4e29dce50f5770023ea6def7eb13bdf9a3856724`).

The tracked candidate generator identifies the explicit FFmpeg/ffprobe paths
under `/opt/goby-toolchains/ffmpeg-9.0.1/bin`. Both files exist; neither was
executed, and their actual version/service-access checks remain pending. The
path supplement is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/media-diagnostic-tool-paths-20260916-01.json`
(1,123 bytes, SHA-256
`f8ad566486e19e35431711cee89b247b6e121b9776823d17f9233fe0906dad5a`).

The coordinated heavy-work window remains pending. Software checks still need
actual execution. Hardware checks require a suitable visible device/driver and
the service's access to it; absent hardware blocks that profile rather than
justifying simulated success. Go/HTTP/browser validation, resource closure and
the complete M5 feature remain open.

Command semantics are checked against the existing playback implementation and
the official [FFmpeg options](https://ffmpeg.org/ffmpeg-doc.html),
[raw media formats](https://ffmpeg.org/ffmpeg-formats.html), and
[protocol restrictions](https://ffmpeg.org/ffmpeg-protocols.html). Documentation
describes options, not runtime support on a particular device.
