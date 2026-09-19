# AMD media phase 1 execution record

Status: **phase 1 complete within the recorded profiles: implementation, remote verification, builds and documentation closed**.
This record belongs to phase 1 of the approved
[three-phase plan](../planning/amd-media-compatibility-plan-20260919.md).
At this phase closeout, phases 2 and 3 had not started. OCI and non-AMD GPU
work remain deferred. The [compact result projection](amd-media-phase1-results-20260919.json)
mirrors the retained terminal receipts and keeps original failed scopes visible.

## Source and preservation

- Starting committed source: `b15de9a` on `main`.
- Working branch: `codex/amd-media-compatibility`.
- Existing tracked differences and the starting status were recorded in the
  Git-private `amd-media-compatibility-20260919` directory before implementation.
  Pre-existing untracked files remain in place. They are not part of this
  increment merely because they share the working directory.
- Pre-existing native services, PostgreSQL instances and retained verification
  evidence remain outside this phase's ownership. The temporary PostgreSQL
  instance created for this phase was separate and is now stopped. Its final
  process and listener closure is recorded below.
- Passing receipts belong to their recorded source and toolchain snapshots.
  Later repairs are not covered merely because an earlier scope passed.
- The `source06` pre-format Git tree is
  `5a3771132ca5e1272dc662f02ece1898116a6f5e`; remote `format06` was applied
  afterward. This tree identifier is not a claim that the formatted worktree
  has the same bytes. `gpu-binaries03` successfully compiled the race-enabled
  `internal/media`, `internal/transcode` and `internal/server` test binaries
  from that prepared source. Test-binary compilation is not a final release
  build or a passing full-repository test run.
- The final selected source snapshot is `source11`, Git tree
  `276caa836657e743609e31a81b73c41c03e55a13`. Remote `format11` made no
  changes. All 1,244 selected source files were individually matched against
  their Git blobs on the remote worker.
- The last production changes were the four bitmap-input/filter-graph files.
  Their affected GPU, software-combination and transcode-package paths have
  source-bound coverage recorded below. Later `source11` changes affect only
  the test invocation recorder. Earlier browser and copied-stream branches
  have no related intervening production change, so their passing scopes were
  retained without redundant reruns.
- This phase's owned `source01.tar` copy is now retained as
  `source01.tar.gz`. Lossless compression and a complete round-trip comparison
  preserved the original tar SHA-256
  `e3351ea9555270469ba01f22bf926c3f3f64a1bf6ee50a8b82b7e8baa8273c22`
  and released approximately 95 MB. All original failed-test logs remain
  intact.

## Verification environments

The first remote inventory found no `/dev/dri` in `test-env`, PVE VM 101.
The AMD Strix device (`1002:150e`, PCI `0000:65:00.0`) was using `amdgpu` on
the `pve` host, and its render node was already mapped to `homelab`, CT 100.
The user explicitly approved creating an independent GPU-test LXC instead of
moving that device away from the existing container or restarting VM 101.

The dedicated worker is PVE CT 104, `goby-amd-worker`, with Debian 13.6,
two CPU cores, 2048 MiB RAM and a 16 GiB `local-lvm` root disk. It is unprivileged
and maps only `/dev/dri/renderD128` through the PVE device interface. The
non-root `goby-worker` account has render-group access. Worker setup preserved
host device ownership and permissions and left CT 100 and VM 101 running.
Manage this worker through `ssh pve` and `pct exec 104`; it is not a public
application deployment. Ordinary verification remains on `ssh test-env`.

Initial enumeration records, retained as inventory rather than media results:

| Item | Observation | Acceptance boundary |
| --- | --- | --- |
| VAAPI | H.264, HEVC Main/Main10 and AV1 Profile 0 expose decode and encode entry points to the non-root worker | Subsequent actual GPU execution is recorded below; enumeration alone does not establish any combination |
| Vulkan | AMD Radeon Graphics, RADV GFX1150, API 1.4.305 | Enumeration does not establish filter correctness or interoperability |
| Initial userland | Mesa 25.0.7, libva 2.22.0, FFmpeg 7.1.5, libplacebo 7.349 | This is the original inventory, not the private toolchain subsequently used for product checks |

The dedicated worker and patched toolchain preparation have succeeded. The
`goby-amd-toolchain02` build exited successfully and installed FFmpeg
`9.0.1-goby-65af9bed5365` in the independent prefix:

```text
/opt/goby-amd-media-20260919/toolchains/ffmpeg-9.0.1-goby-65af9bed5365
```

The toolchain archive SHA-256 is
`102ca119c64d20c042a05a6860a2d8a4f5e29321cf6f7659dd3e8662234e6ce4`.
The recipe includes HEVC/AV1 encoders, Vulkan/libplacebo, and the
[strict Dolby Vision patch](../../scripts/test-env/toolchain-patches/README.md).
Existing FFmpeg installations remain separate; no existing service toolchain
was replaced. Building this private toolchain does not establish completion of
the repository's release build or the entire AMD media matrix.

A temporary database proxy admitted only the designated CT worker peer for
the real HTTP GPU checks. That proxy is stopped, with `MainPID=0`; PostgreSQL
listeners and HBA configuration were not changed. The owned PostgreSQL worker
itself is now stopped. Pre-existing protected services were not modified.

## Implementation workstreams

| Workstream | Verified result | Retained acceptance boundary |
| --- | --- | --- |
| HEVC/AV1 engine and client negotiation | `gpu03` passed the repaired 8/10-bit HEVC MP4/fMP4 paths, actual HEVC hardware decode/encode, HEVC/AV1 seek A/V checks, and the real HTTP AV1 software-fallback case | Final builds and closeout passed; the historical aggregate `gpu03` failure remains recorded |
| Dolby Vision and GPU processing | `gpu04` passed P8.1 and complete P7 MEL SDR/HDR10 conversion and chart checks; the source bridge and affected final-source regressions are recorded | Retain profile/layer, color-test and CPU/GPU boundaries without expanding these results into Dolby color certification |
| Subtitle composition and progressive MP4 burn-in | `gpu04` passed cancellation/file closure; `gpu10` passed repaired strict bitmap gates; `bitmap-software02` passed HDR, two-rendition ladder and audio/bitmap combinations | Final source/artifact identities retain these exact clocks, pixels and mixed CPU/GPU stages |
| Broader copy seeking | Source-bound restart/packet proofs implemented; `copy-repair03`, the H.264 native-browser journey and repaired recorder checks passed | Preserve the tested alignment contract and retained source bridge in the final artifact record |
| Progressive HEVC/AV1 startup detection | Exercised by selected HTTP/GPU checks and composed server/transcode coverage; AV1 also played in the browser harness | Malformed/truncated-prefix checks do not imply every browser or codec profile can play |
| Toolchain and worker preparation | CT 104 and patched toolchain build 02 succeeded; actual GPU media execution and owned-process closure passed | The worker and artifacts remain available; other hardware/driver profiles remain independent |

The current execution and transfer boundaries are described in
[AMD video processing](amd-video-processing.md). In particular, Dolby Vision
uses software HEVC decoding to preserve per-frame RPU data, text subtitles use
CPU libass rendering before Vulkan composition, and VAAPI/Vulkan paths include
explicit downloads/uploads. These mixed paths are not described as entirely
GPU-resident or zero-copy pipelines. The accepted bitmap repair additionally
uses CPU transparent-plane synchronization before the final Vulkan blend.

## Recorded verification checkpoints

Ordinary scopes ran on `test-env`; actual AMD scopes ran as the non-root worker
in the approved CT 104. The ordinary owned evidence root is
`/opt/goby-amd-media-20260919-50f45177f297`. Commands, source overlays, private
environment manifests and raw receipts remain in the owned verification
records; database credentials are not copied into repository documentation.

| Scope | Recorded outcome | What the outcome establishes |
| --- | --- | --- |
| `copy-repair03` | 201 passed, 0 failed, 0 skipped | Selected `VideoCopySeek`/`VideoCopyAudio` tests in `internal/media` and `internal/transcode`, including actual copy/seek media paths and source/packet proof cases |
| `browser02` | Driver `Complete=true`; AV1 status `played` | Actual native `HTMLMediaElement` H.264 copied-seek and AV1 progressive-output journeys, with browser/frame clocks, UI/report mapping, persisted state and resource retirement |
| `http-media01` | 9 passed, 0 failed, 0 skipped | Selected HEVC/AV1 progressive encoding and exact-seek output, modern-codec HLS copying, and progressive subtitle-burn negotiation/HEAD reauthorization |
| `hardware-unit02` | 28 passed, 0 failed, 2 GPU-dependent cases skipped | Selected hardware-configuration and HTTP authorization behavior; the two skips are not hardware execution passes |
| `gpu02` | Most selected actual-device cases passed; scope did not pass completely | Exposed corrupt VAAPI HEVC MP4 output on the `hvc1` path; retained as the failing evidence preceding the `hev1` correction |
| `gpu-binaries03` | Race-enabled media/transcode/server test binaries compiled successfully | Establishes compilation of the prepared `source06` plus remote formatting; it is not product runtime acceptance or the release build |
| `gpu03` | Terminal, `MainPID=0`, exit status 1 | The original HEVC corruption cases and new A/V/fallback gates passed; new bitmap/cancel fixture admission and an SDR test assertion failed, so the aggregate scope remains failed |
| `gpu04` | Actual cancellation/file closure and P8.1/complete P7 MEL SDR/HDR10/chart checks passed; bitmap cases failed | Corrected fixture and color assertions reached the real checks; bitmap output was delayed by about 0.44/0.5 seconds and HLS contained excess frames |
| `gpu05` through `gpu09` | Isolated bitmap diagnostics retained | Established correct PGS cue times and identified single-demux subtitle heartbeat scheduling and timestamp-precision loss; diagnostic iterations are not independent product acceptances |
| `gpu10` | Strict bitmap output and related command unit checks passed; successful process with `MainPID=0` | Independent authorized subtitle input, AVTB precision and synchronized transparent-plane composition retained exactly 40 output frames, with the cue visible only at frames 16 through 31 and cleared afterward |
| `regression01` | Closed; 10,458 pass, 78 fail and 14 skip events | Retains the original whole-repository run, including fixture/environment failures and the server package's aggregate deadline; event counts overlap and this is not a clean whole-suite pass |
| Server tail scope | 334 top-level tests: 332 passed, 2 failed; 1,189 pass and 2 fail events | Completed the original server inventory after the aggregate timeout; the two failures were subsequently repaired in the test recorder |
| `source11` recorder repair | 11 passed, 0 skipped | Eight invocation-classification subcases, their new parent, and the two affected integration tests passed after a test-only observation repair |
| `cache-repair01` | 3 passed, 0 skipped | Resolved the three fixture/current-probe-version failures without changing historical version-6 fixtures |
| `transcode-repair01` | 1,050 pass, 1 fail and 9 GPU skip events | Whole-package execution covered the final production change; the sole failure was the new combination fixture's reference-stream helper and was resolved by the following test-only repair |
| `bitmap-software02` | 4 passed, 0 skipped | Software HDR bitmap output, two-rendition fragmented-MP4 ladder output, and verified seek with independent audio/bitmap and chirp A/V checks passed |
| `archive-repair01` | 497 passed, 0 skipped | `backuppg` verification passed with its admitted isolated database environment |
| `recovery-store02` | 177 passed, 0 skipped | Recovery-store verification passed after correcting the owned test environment |
| `recovery-manager02` | 73 passed, 0 failed, 0 skipped | Complete recovery-manager scope on its independently owned database pair |
| `build-final01` | Both Linux builds passed | Ordinary and embedded-dashboard binaries from the reconciled source, CGO disabled and trimpath enabled |
| VM closure | 26 owned worker invocations terminal; PostgreSQL stopped | No owned verification worker PID; database PID 2796884 retired and port 54919 no longer responds |
| GPU closure 02 | Passed; no live owned process | Includes retirement of three completed diagnostic units' lingering systemd-run waiters; the first failed closure receipt is retained |

The numeric results are per-scope reported outcomes. Filters, repaired reruns,
and test/subtest reporting overlap; they must not be added into an independent
test total or a count of distinct accepted features. These selected scopes are
not interchangeable with distinct top-level test coverage or a final build
result. The composed server coverage is detailed below; no single successful
whole-server invocation is claimed.

### Native browser scope and startup-clock repair

The browser test uses a private, real HTTP Goby instance, an independently
scanned 132-second H.264/AAC source, and a native `HTMLMediaElement` harness.
It is not the original Emby Web application and does not execute that
application's playback modules. The retained
[client timestamp research](media-client-timestamps.md) remains separate
static consumer evidence, not an original-client runtime result.

The journey exercised default copied starts at requested positions 6.37, 3.41
and 7.25 seconds, with actual keyframe origins 6.0, 3.0 and 6.0 seconds.
`CopyTimestamps=true` preserves the source clock rather than adding the original
request again. It checked actual presented frames and color markers, pause and
resume, backward/forward stream changes, persisted reports and resume positions,
explicit stop, and the retirement of old conversion resources. The source's
duration crosses the real resume-storage threshold; the journey does not play
the entire 132-second tail or claim full-length playback acceptance.

The first attempt, `browser01`, failed because the first video-frame callback
reported media time 6.0 while the element's initial `currentTime` was still 0.
Later observed frames and element time agreed. That raw first observation was
preserved rather than overwritten or accepted by widening the tolerance.
The repaired harness waits at most three seconds after `play()` resolves for
three consecutive advancing frame/element-clock samples, each within 0.1
seconds, and requires the `playing` event. It generates the displayed time and
`Started.PositionTicks` from one actual `currentTime` snapshot, then reads the
persisted Started state before a Progress report can overwrite it. `browser02`
passed this repaired gate. The first attempt had no independent Started
snapshot, so its record does not prove that an initial zero was actually
persisted.

AV1 capability detection and actual AV1 playback are recorded separately;
`played` in `browser02` means the browser actually rendered the progressive
output. HEVC browser playback remains unverified. Neither a software browser
decode nor `mediaCapabilities` establishes AMD hardware decoding. The browser
journey also does not claim independent lip-sync measurement, subtitle playback,
or completion of the full Emby client workflow.

### AMD HEVC repair and expanded actual-media results

`gpu02` exposed decoder errors in VAAPI HEVC MP4 output tagged `hvc1`.
The focused device-bound diagnosis found different initialization and in-band
PPS values; the `hvc1` muxing path removed parameter sets needed by the encoded
pictures. Changing only the sample entry to `hev1` retained those parameter
sets and produced a strictly decodable diagnostic output. Bitrate ceilings,
dimensions, bit depth and media checks were not weakened to make it pass.

The engine now selects `hev1` for VAAPI HEVC progressive MP4 and fragmented MP4
HLS, and for copied HEVC. Software `libx265` output retains `hvc1` with parameter
set repetition disabled. Signaling follows the final plan, including fallback.
See [the HEVC parameter-set diagnosis](hevc-vaapi-parameter-sets-20260919.md)
for exact tool/device identities and retained control results. The subsequent
`gpu03` product execution passed the original HEVC progressive MP4 and
fragmented MP4 cases at both 8 and 10 bits, and the actual HEVC hardware
decode/encode cases. The earlier diagnostic controls are therefore accompanied
by product-path evidence for this repair; they are not the sole acceptance
basis.

`gpu03` also passed the new actual HEVC/AV1 8/10-bit seek A/V synchronization
checks. A real `PlaybackInfo` to media `GET` journey exercised AV1 320x180
hardware-output padding rejection, preserved the same requested output
specification through software fallback, and verified cleanup. This result
establishes a truthful fallback for that device limitation, not successful
hardware AV1 encoding at the rejected geometry.

The full `gpu03` process nevertheless exited with status 1 and is closed
(`MainPID=0`). Its new bitmap/cancellation cases stopped at a fixture
precondition that expected exact Matroska duration; the observed duration was
1 ms longer. The guard has been corrected to account for that fixture
representation. Those failed attempts did not reach the intended new behavior
checks and cannot be counted as their passes. After correcting the guard,
`gpu04` passed actual cancellation and file closure, and reached the bitmap
checks that exposed the separate timing defect described below.

### Bitmap timing repair and retained GPU10 acceptance

`gpu04` found actual bitmap-subtitle output defects: progressive/HLS cue timing
was delayed by approximately 0.44/0.5 seconds, and HLS output included extra
frames. The original strict output checks were retained. Isolated `gpu05`
through `gpu09` diagnostics established that the source PGS start/clear cues
were correctly timed at 2.5/3.5 seconds. The delay arose in single-demux
`sub2video` heartbeat scheduling. A 1 ms timestamp base also collapsed the
heartbeat placed 1 microsecond before a cue, losing the boundary needed for
correct display/clear behavior.

The product repair supplies subtitles through an independent, authorized
file-descriptor input, preserves timestamp precision with AVTB, synchronizes
a transparent plane on the CPU, and performs the final blend in Vulkan.
It does not repair timing by shifting the expected cue or accepting additional
output frames. In `gpu10`, the original strict 40-frame requirement and all
visibility/clear checks for frames 16 through 31 passed, as did the related
command unit cases.

The retained unit is `goby-amd-gpu10-bitmap-input-fix.service`, recorded as
`active/exited`, `MainPID=0`, with successful exit. Its evidence directory is:

```text
/opt/goby-amd-media-20260919/gpu10-bitmap-input-fix/_evidence
```

The tested binary SHA-256 is
`c1542153c06adda9d93b3930a4807a52f4f6889ec987b0fbc530c0e5e576a78e`.
These results bind that GPU execution and its patch. The four-file product
change is now included in the final source bridge. The whole transcode package
and the focused software HDR, adaptive-ladder and audio-bearing bitmap
combinations have also run against that production change.

The combination fixture initially selected the wrong reference streams.
`source09` corrected the test helper to select SDR video `v:1` and PCM audio
`a:0`, and to keep PGS events out of video/audio frame observations. This was
a test-only repair. `bitmap-software02` then recorded four passing outcomes,
including two-rendition fragmented-MP4 output and a real verified seek with
independent audio input, bitmap rendering and chirp-based A/V measurements.
The strict bitmap timing and output-frame requirements were not weakened.

### Complete profile 7 MEL material and color checks

The complete synthetic profile 7 MEL fixture is now available at:

```text
/opt/goby-amd-media-20260919/profile7-fixture02/artifacts/profile7-mel-single-track.mp4
```

Its SHA-256 is
`51f1f2f68791950c747b3cc7f9aaedd5c8e99db4a015c2e06429d22bd9d3eace`.
Both layers contain 96 frames and independently decode. Official-tool
multiplexing, demultiplexing and complete RPU export checks passed. This is
separate from the earlier deliberately incomplete BL-plus-RPU controls; their
historical limits remain intact. Fixture provenance and the generation-only
acceptance boundary are recorded in
[the Dolby Vision fixture record](../../scripts/test-env/amd-media-fixtures/dolby-vision/README.md#recorded-generation-checkpoint).

With this complete input, `gpu03` passed profile 7 MEL and profile 8.1 HDR10
conversion, including neutral output, monotonic ordering and the expected PQ
luma direction. The SDR outputs already showed neutral, monotonic pixels, but
a newly added assertion incorrectly required bright samples to become darker
after tone mapping into a different output range. That assertion caused the
SDR cases to fail; the record does not relabel those runs as successful.

The corrected gate confines the PQ luma-direction assertion to HDR10. SDR
continues to require neutrality, monotonic ordering, lifted dark samples and
visible RPU-driven differences. `gpu04`, using the `source06` production code
and corrected test guards, passed P8.1 and complete P7 MEL conversion to both
SDR and HDR10, including the chart checks.

The production libplacebo path explicitly sets `peak_detect=0`; dynamic peak
detection was not the cause of the failed assertion. The mistake was applying
the PQ-domain curve-direction requirement directly to SDR results that had
passed through BT.2390 tone mapping and different RPU-dependent HDR color
interpretations. The corrected checks keep the requested output-range
boundaries explicit. These synthetic chart and conversion results do not
establish Dolby color certification, FEL residual reconstruction, licensed
encoder certification or arbitrary commercial-title compatibility.

### Repository regression and resource closure

`regression01`, the original full `./...` race run on `source06`, is closed.
Its retained event totals are 10,458 passes, 78 failures and 14 skips. These
include overlapping parent/subtest outcomes and are not counts of independent
defects or accepted features. The 10,000-item media scan and rescan passed,
as did the full playback package.

Two library failures and one media failure were traced to stale assertions
that expected current probe version 6 after the current version changed to 7.
The fresh-fixture assertions were corrected to 7, while intentionally
historical version-6 fixtures remain unchanged. `cache-repair01` passed all
three repaired cases with no skips. The original failed outcomes are retained.

`backuppg`, `recovery` and `recoverydb` cases were rejected by test-environment
database-name-prefix or port protection. The backup fixture requires the
`goby_backup_` prefix. With the independent database pair configured,
`archive-repair01` recorded 497 passes without skips and `recovery-store02`
recorded 177 without skips. `recovery-manager02` subsequently recorded 73
passes, zero failures and zero skips. The environment repair did not change production
behavior or protected services. Original admission failures remain in their
receipts alongside the successor results.

#### Composed server coverage

The first whole-server invocation reached its aggregate 12-minute deadline
while `TestHTTPLiveTVProgramsInputAndStorageFailuresRemainErrors` had been
running for only about one second. This is not evidence that the named test
itself consumed twelve minutes. Before that package timeout, 390 top-level
tests passed and one GPU-only test was skipped in the ordinary environment.
The original compiled inventory contained 725 top-level names.

The remaining 334 top-level tests were executed in the tail scope: 332 passed
and two failed, with 1,189 pass and two fail events including subtests. Both
failures came from the old test recorder classifying a full-source AAC
`framehash` scan as a seek proof. `source11` changes only that test observer.
Its repair scope passed eleven outcomes without skips: eight classification
subcases, the newly added parent test, and the two affected integration tests.

After deduplicating by compiled top-level test name, server coverage is now
725 top-level passes and one GPU-only ordinary-environment skip across 726
compiled names, including the new observer test. This is composed coverage
from the initial run, tail execution and affected repair scope. It is not a
claim that a single whole-server invocation exited successfully.

#### Final source bridge

The last production patch consists of the four bitmap input/filter-graph
files. Its affected behavior is covered by `gpu10`, the whole-package
`transcode-repair01` run and the repaired `bitmap-software02` combinations.
The transcode package reported 1,050 passes, one failure and nine GPU skips;
its sole failure was the test helper corrected in `source09`, not an
additional production repair. These outcomes are retained separately rather
than rewriting the package receipt as a clean run.

Subsequent `source11` changes only the invocation recorder used by tests.
The passing browser and copy scopes have no related production change on
their exercised branches, so their original receipts remain applicable and
were not rerun solely to obtain later timestamps. Remote `format11` was a
no-op, and all 1,244 selected source files matched their Git blobs for tree
`276caa836657e743609e31a81b73c41c03e55a13`. This source bridge supports the
composed evidence; the final builds below independently establish compilation.

## Final artifacts and closure

`build-final01` completed both Go 1.27.1 Linux builds with `CGO_ENABLED=0` and
`-trimpath`. The embedded build reuses the preceding accepted administrator
asset build; all 56 production frontend source/configuration files were checked
unchanged before that reuse. The new browser test file is not a production asset.

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| `goby-amd-phase1-ordinary` | 34,525,334 | `984bf934ec267bdef5a25f220399dfa3a2de3f01a5deed1cc502baaa35c99745` |
| `goby-amd-phase1-embedded` | 35,755,346 | `e4b3ebf490b591c6fa466edccdd42b04c67d23c3e87a7e802f37cdbea5c96b5a` |

The VM's `phase1-vm-closure.json` records 26 terminal owned worker invocations,
the stopped temporary PostgreSQL instance and a nonresponsive owned database
port. Source, tools, binaries, original failures and stopped database data remain
available as evidence; no pre-existing service was stopped or reconfigured.

The first GPU process closure found three passive `systemd-run --wait` callers
for GPU07/08/09. Their diagnostic workers had already succeeded with
`MainPID=0`, but `RemainAfterExit=yes` kept the callers waiting. The parent
stopped those three completed units and retained the original failed closure.
`phase1-gpu-closure02.json` then recorded no live owned process. CT 104 remains
available with its tools and evidence; CT 100 and VM 101 were observed running.

Phase 1 implementation, selected-profile verification, builds, resource
closeout and API/support/status documentation are complete. Phase 2 is the
next authorized work; phase 3 remains required by the same approved plan.

## Scope retained after closeout

Actual Dolby Vision chart acceptance covers profile 8.1 and the complete
single-track profile 7 MEL fixture. Profile 5 and other profile 8 compatibility
variants have implementation/admission paths but are not promoted to verified
profiles by these fixtures. HEVC playback in the tested Chromium browser is
unverified; AV1 and H.264 passed their described native-player journeys.
The device-bound encoding and processing results do not certify every AMD
adapter, driver, resolution, color grade or third-party client.

Dolby Vision MEL/FEL behavior must follow actual residual-processing support.
A base-layer conversion must not be reported as full enhancement-layer
reconstruction. Missing fixture or profile evidence remains explicit.

No full Emby client acceptance, production deployment, commit or push is
claimed by this execution record. Phases 2 and 3 remain not started at this
phase closeout; OCI and other GPU vendors remain deferred.
