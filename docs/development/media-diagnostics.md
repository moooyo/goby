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
contract tests. The subsequent source increments add an internal Linux process
session, decoded-content validators and a stage pipeline with independent
preparation/reference records and codec-log evidence. None is wired to an
administrator operation. Execution ownership/admission, deployment prerequisites,
result retention and Settings UI remain to be connected and verified before
this feature is complete.

Reuse the existing process-group and borrowed-file-descriptor primitives. Do not
create a second Manager against the live transcode repository: its recovery path
would interrupt existing jobs. Do not fabricate user/auth/play/item records or
library task children for diagnostics. Existing task UI interactions may inform
polling and cancellation, but their scan-specific data model is not the result
contract for media stages.

Execution must use an isolated environment and controlled TMP/cache locations, bounded concurrency,
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

The new internal process session places each child directly into one newly owned
cgroup v2 leaf, using Go's `UseCgroupFD` before exec. The existing process-group
helper now preserves preconfigured attributes. The leaf has fixed, as-yet
unmeasured limits of 512 MiB memory, zero swap, group OOM termination and 64 tasks;
all version/preparation/stage/verification commands share a two-minute context
and a 24-command ceiling. Each command also retains the 15-second plan deadline.
These limits cover the descendant cgroup, not the Go server's own bounded
buffers or GPU VRAM. A shared-host execution window is still required for tests.

The service must supply an already delegated memory/pids parent and an empty
scratch directory that it cannot make writable: a read-only mount or a non-owned
directory with no write permission bits. The process session borrows a directory
descriptor and never creates, chmods or removes that directory. The child inherits
only the explicit loader/hardware environment and disabled driver-cache settings.
No deployment delegation, scratch provisioning or public configuration is wired
yet. Missing prerequisites reject execution; they do not authorize changing the
service's parent cgroup or running without bounds.

Raw and compressed command inputs use sealed read-only memfds. Raw input hashes
must also match the fixed generator; a compressed hash still requires a validated
preparation record from the future stage orchestrator. The ELF descriptor, its
content and filesystem identity stay bound across execution. Stream byte limits,
memory/OOM/pids events, child joins and cgroup emptiness are separate observations.
Failures during initialization retain a partial owner when cleanup fails; a
pending writer is joined before reading buffers or releasing its resources.
No command observation is itself a successful diagnostic stage.

Decoded-content checks compare directly with the fixed original YUV420P/PCM
fixtures. Video checks eight complete frames, frame order, each plane and local
tiles. Audio searches bounded alignment candidates, compares the complete
waveform and bounds both padding regions. Its reported offset is an observed
waveform match, not a codec-header delay claim. The fixed lossy-error thresholds
still require real software and configured-hardware codec calibration. They do
not prove sample-exact completeness, actual decoder/encoder selection, or replace
stage-specific AAC preparation/reference evidence. Small differences within the
content budget, including low-energy fade changes, are permitted by this policy.

This increment adds ten pure content cases and eight Linux component cases;
none has run. Seven Go files were formatted remotely without Go tests, builds,
FFmpeg, SQL, HTTP or service operations. The formatting/source record is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/media-diagnostic-executor-20260916-01/source-preparation.json`
(3,416 bytes, SHA-256
`7f3024c5bebe7e0896f69d7e2e6933979e0d89b045b0b8e55d2e5bd94ff4b862`).
Its snapshot precedes this evidence note and the final comment-only clarification
of the audio content policy. Static review identified partial-owner
loss and a same-UID scratch-permission weakness; both were corrected and reviewed
again before formatting. No actual cgroup/fork/cancellation/media result or
supported deployment profile has been verified by this source preparation.

The stage pipeline now prepares H.264, first-generation AAC and second-generation
AAC in separate software commands. The second AAC reference uses the actual
combined plan over the first compressed input. Every preparation is decoded and
compared with the original generated sample before dependent stages can use it.
Formal decode, encode and combined stages use new commands; encode/combined
outputs receive an additional software decode. Actual decoded bytes, waveform
alignment and physical ADTS packet counts are compared with the applicable
independent reference. Video encoding can still be checked against the raw
fixture when an unrelated preparation or stage fails.

The fixed graph has 17 commands for the software video/audio baseline and 22
when configured hardware video is included, counting the version command and
all preparation/verification work. These are graph counts, not observed runs or
a promise that the total deadline can accommodate every worst-case command.
Cancellation interrupts the active session even when its parent context differs
from the graph's parent. Completed stages remain preserved; remaining stages
are explicitly not run. Resource/identity/closure failures stop further dispatch,
while independent codec stages may continue after an ordinary codec failure.

The bounded FFmpeg parser follows the n9.0.1 source grammar for actual stream
mapping, input/output shape and decoder format selection. Plans now request
debug-level logging, retaining the same 64 KiB stderr limit. Hardware format
selection must follow mapping, belong to one mapped decoder context and remain
consistent. Missing/conflicting evidence is unverified; planned arguments,
probe-only formats or a successful exit cannot replace it. ADTS parsing only
establishes complete physical packets and the declared fixed AAC-LC/48k/stereo
header profile. Actual decoding and content/reference checks remain mandatory.

The pipeline returns `SessionClosureRequired=true` for a supplied session,
including when every stage is complete. The future administrator run owner must
retain partial initialization/close failures, account for active conversion work,
close the session, and publish the final outcome only after those obligations
are satisfied. Progress snapshots copy their nested facts, so an observer cannot
mutate private references. There is still no public run API, configured resource
delegation, retained run manager or Settings action. The new source-derived log
fixtures and simulated-session cases do not establish actual FFmpeg, hardware,
deadline or administrator behavior on test-env.

This stage increment adds 12 log-evidence, six ADTS and nine orchestration
test functions, all unexecuted. Static review corrected cancellation forwarding
to a differently parented session and the codec-tag suffix on a rawvideo
reference-frame descriptor. The eight Go files were formatted on test-env;
the source record is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/media-diagnostic-stages-20260916-01/source-preparation.json`
(3,775 bytes, SHA-256
`faa236010eeb66d50c7df01a9257054927eb630c36be5382394ed5720e989614`).
The snapshot precedes this evidence note. No Go tests, builds, FFmpeg, ffprobe,
SQL, HTTP or service operations ran; real log compatibility, software/hardware
stage results and the enclosing administrator run remain unverified.

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
The process layer additionally follows the official
[Go Linux process implementation](https://go.dev/src/syscall/exec_linux.go) and
[kernel cgroup v2 documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html).
Their API descriptions do not prove that the test host or shipped service has
the required delegation, kernel features or scratch-directory setup.
