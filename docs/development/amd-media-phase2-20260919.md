# AMD media phase 2 implementation record

Status: **phase 2 verified and closed within the recorded source, runtime and profile boundaries; implementation, final builds, owned-resource closure and documentation are complete**.
This is the current checkpoint for phase 2 of the approved
[three-phase plan](../planning/amd-media-compatibility-plan-20260919.md).
Selected package, media, HTTP and native-video/HLS.js browser scopes have passed.
The formal successor is now accepted within its recorded CT 104 build/native/GPU
scope. The v3 runtime has also passed the recorded VM 101 CPU-media and browser
scopes. Final resource/documentation closeout is complete; full original Emby
Web behavior and deployment are not claimed.
At this closeout checkpoint, phase 3 is next and has not started. The
[compact result projection](amd-media-phase2-results-20260919.json)
records the confirmed event counts and retains the original failed scopes.

## Entry boundary

Phase 1 completed its implementation, selected-profile remote verification,
ordinary/embedded builds, owned-resource closure and documentation. Its final
selected snapshot was `source11`, Git tree
`276caa836657e743609e31a81b73c41c03e55a13`. Preserve the
[phase 1 record](amd-media-phase1-20260919.md) and its original successful and
failed receipts unchanged. Those results accept their recorded source and
profiles; they do not automatically accept phase 2 changes.

Work remains on `codex/amd-media-compatibility`. This record accepts the selected
phase 2 snapshots and scopes below, without a commit, push or production promotion claim. Existing
unrelated work and retained services/evidence remain outside this phase's
ownership.

## Frozen sources and remote results

| Snapshot or scope | Recorded result | Boundary |
| --- | --- | --- |
| `source01` | Raw selected Git tree `40b0fee24ea256a56ed4ec35869a7278874dce58` | Preparation identity, not acceptance |
| Remote formatting / `source02` | `gofmt` changed 69 files; resulting tree `19be81741a52a0644bd6523ed7aa9b2bd9e208e4` | Formatting does not execute the product |
| `compile01` | Seven packages compiled; server compilation found a function/type name collision | The failed server result remains preserved; no runtime acceptance follows from compilation |
| `source03` | Renamed the renderer to `dynamicRenderedSubtitlePlaylist`; tree `4ee63f5c3c965edb5f7a1e8de53175ac1d4b740d`, 1,314 selected source files; remote `gofmt` made no changes | This historical frozen source and its failed receipts remain unchanged |
| `compile02` | Server and browser-build-tag compilation passed | Together with the other seven `compile01` packages, this closes the reported compile defect; these are not final production builds or browser execution |
| `phase2-unit01` | Race-enabled timeshift, subtitle, dynamicsource, config and playback scope: 1,469 pass records, zero failures and zero skips | Passing scope is bound to its frozen source; it is not a complete repository or client result |
| `phase2-media01` | Race-enabled media and transcode scope: 2,142 pass records, seven failures and 12 skips | The aggregate scope failed; parent/subtest records overlap and are not distinct feature counts |
| `media-repair02` / `source05` | 48 pass events, zero failures and one AMD-dependent skip | Selected media repair scope passed; the skipped AMD case is not hardware acceptance |
| `server02` / `source07` | 84 pass events, three failures and zero skips | Aggregate failed on dynamic-metadata fixtures; original failures remain preserved |
| `server03` / `source08` | One pass event, two failures and zero skips | Aggregate failed; later diagnosis and repair results have separate source identities |
| `source11` | Git tree `172b45b48c06cff370e49acc84ccfdcacc76015b` | Adds the common one-microsecond AV/caption segmentation tolerance described below; this phase 2 snapshot is distinct from phase 1's `source11` |
| `clock-repair03` / `source11` | Four pass events, one failure and zero skips | Actual 24 fps TS/fMP4 strict decoding and dynamic pause/reconnect/seek/live/ownership passed; the remaining failure was the positive subtitle-offset boundary fixture |
| `source12` | Git tree `697b970957e25ef68a3f791d6d18f68c4fbfc43a`, 1,316 selected files; VM formatting made no changes | Subtitle-offset fixture contract repair from `source11`; retained source for the scopes below |
| `phase2-subtitle-repair04` / `source12` | Nine pass events, zero failures and zero skips; transcode and server passed | Corrected caption-prefix regex scope and dynamic eight-track switching/off/offset/retained-epoch HTTP checks passed; the unit is inactive/dead with `MainPID=0` and `Result=success` |
| `phase2-hls-regression01` / `source12` | 73 pass events, zero failures and zero skips; unit closed | Selected HLS regression scope passed; no complete phase or browser acceptance follows |
| `source13` / `source14` | Raw tree `3421672ccf651107b245ab4ea36a086061e007a1`; remote formatting changed one test file, producing `source14` tree `36af019a8ec6e3cc92b09a4e36d7c4a8518cb666` with 1,316 selected files | Live progress parser and its regression source; the raw and formatted identities remain distinct |
| `phase2-progress-repair01` / `source14` | 24 pass events, zero failures and zero skips; unit closed | Selected live-progress range/conversion repair scope passed |
| `phase2-browser01` / `source12` | Actual browser scope failed; owned processes closed, source unchanged and remaining PIDs zero; recorded `minfree=749748224` | Finite track/off/offset and dynamic play/pause/resume/replay reached their observations, but the final live-frame comparison had stale frame state; this is not a complete browser pass |
| `source15` | Git tree `4287b12736f690bd7755df6d37511760224165d6`; remote formatting made no changes | Browser harness adds a `seeked` plus three-new-frame barrier while retaining the 0.3-second threshold; replay now requests an actual backward seek |
| `phase2-browser02` / `source15` | Client journey passed in 31.6 seconds, but strict fixture resource closure failed | The aggregate scope failed; the original resource failure remains preserved |
| `source16` / `source17` | Raw tree `da2c7792289ea948bd3c72923833420bda1b2e54`; remote formatting changed two files, producing `source17` tree `5aead247d22261a73658e56d3363d7fce0ff723f` with 1,317 selected files | Reconnect cleanup cancels the terminal old job before replacing its identity; retained Store copies preserve old epochs |
| `phase2-reconnect-cleanup01` / `source17` | Five pass events, zero failures and zero skips; unit closed | Focused real-HTTP cleanup and affected dynamic playback/subtitle scope passed |
| `phase2-browser03` / `source17` | `Accepted=true`; browser exit 0, fixture exit 0; Go one pass event, zero failures and zero skips; recorded duration 51.781 seconds and `minfree=675704832` | Native video plus HLS.js 1.6.0 beta 2 harness passed with strict resource closure; not full original Emby Web acceptance |
| `phase2-library-regression01` / `source17` | 2,417 pass events, zero failures and one skip; unit closed | Full library-package scope passed with the existing `TestRootBindingFullScanMountNamespaceHelper` skip, which requires a separately reviewed opt-in mount scope |
| `source18` | Git tree `e416723dcdeee9fe8512e23a94ca8c74b5d8f9af`, 1,322 selected files; VM formatting made no changes | Only toolchain/scripts changed from `source17`; Go source, including the library code, is unchanged |
| `phase2-toolchain-successor02` / `source18` | Formal `amd-media-v3` source rebuild and native gate accepted; unit closed | Rebuilds libplacebo and FFmpeg, skips package installation, publishes a separate prefix and preserves the previous runtime/package state |
| `phase2-gpu-formal01` / `source18` | Four groups, 21 pass events, zero failures and zero skips; all units closed and scratch empty | Actual race-enabled media/transcode tests use the formal successor without an overlay; selected live, finite and Dolby Vision profiles passed |
| VM v3 runtime admission | 146 installed files matched and version probes passed; recorded in `software-environment-v3.json` | Establishes the selected runtime on VM 101, separately from the CT build and GPU results |
| `phase2-media-v3-regression01` / `source18` | 2,169 pass events, zero failures and 13 environment-specific skips; unit closed | Selected CPU media/transcode regression passed with v3; GPU/Dolby Vision environment scopes retain their separate CT evidence |
| `phase2-final-build01` / `source18` | Ordinary and embedded builds passed with `CGO_ENABLED=0`; source unchanged | Exact artifact hashes and sizes are recorded below |
| `phase2-browser04` / v3 | Failed after the client retained resume position 4.98 while the server window was `[6,18)` | The attempted rewind target was forward of the actual client position; the original harness failure remains preserved |
| `source19` | Git tree `8f1de3f4509b36b06a5e1a331166d1e1c6a64fd7`, 1,322 selected files; remote formatting made no changes | Only the browser spec and `scripts/test-env/phase2-hls-browser.md` changed from `source18`; all Go, production frontend and toolchain inputs are unchanged |
| `phase2-browser05` / v3 / `source19` | `Accepted=true`; browser and fixture exits zero; Go one pass event, zero failures and zero skips; 41.201 seconds, `minfree=714891264` | The corrected native-video/HLS.js harness and strict before-global-close resource predicate passed; this is the final recorded browser scope, not full original Emby Web acceptance |
| `phase2-vm-closure.json` | `closed=true`; 27 workers terminal with `MainPID=0` and empty cgroups; source19 matched 1,322/1,322 entries before and after closure, with `failed=[]` | Owned PostgreSQL stopped after identity/connection checks; its PID, port, socket/lock and pidfile are absent; original receipt hashes are unchanged |

All counts in this record are overlapping test events. Parent/subtest records
and repair scopes must not be added into a total of independent tests or accepted
features. Later successes retain their exact source and selected-case boundary;
they do not erase original failures or imply a successful complete repository run.

The original media failures included continuous MPEG-TS with single and dual
renditions, where strict concatenated-output decoding reports a corrupt packet,
and single/dual-rendition fMP4 initialization changes within one producer. Their
failed receipts remain preserved alongside the subsequent selected repair scopes.

The `bitmapcue_true` failure exposed a new fixture's missing `-copyts`: its SUP
cue at six to seven seconds was rebased to zero to one second. The fixture repair
preserves those timestamps and checks original source packet PTS before the
product path. The original failure remains part of the source-bound history;
finite GPU bitmap results below are separate from the later quiet-live repair
and formal-successor acceptance scopes.

`diagnostic05` and `diagnostic06` established that the Store correctly rejected a
6.0000003-second segment against the fixed target. At 24 fps, the first reference
clock is approximately 1.041666... seconds; microsecond rounding missed the
three-second GOP boundary. Phase 2 `source11` supplies
`segment_time_delta=0.000001` to both AV and caption segmentation. Its
`clock-repair03` strict media and dynamic playback cases passed without weakening
the fixed-target Store contract. The one remaining positive-offset fixture
failure was corrected in `source12` and passed `phase2-subtitle-repair04`.

### Live progress and browser boundary

The new live progress parser does not inherit the finite-media 30-day limit or
the erroneous ten-second ceiling. It bounds microsecond values by
`MaxInt64 / 10` before multiplying by ten to obtain 100 ns ticks. Finite-media
limits, negative-start clamping to zero, and the external caption protocol's
30-day-per-generation rule remain unchanged. `phase2-progress-repair01` passed
its selected scope on formatted `source14`.

In `phase2-browser01`, the final Live observation compared
`currentTime=9.738708` with stale `presented=11`; the frame counter remained at
19 without a newly presented frame. The harness sampled the two clocks across
a seek/frame race, so the completed earlier actions do not make the full browser
scope pass. Its original failure and clean source/process closure remain recorded.

`source15` waits for `seeked` and three newly presented frames before that clock
comparison, preserving the 0.3-second tolerance. The earlier Replay target was
actually forward of the current position; the harness now requests a real
backward seek. `phase2-browser02` passed that client journey in 31.6 seconds but
failed the unchanged strict fixture resource-close predicate. It is a preserved
failed aggregate scope, not an accepted browser result.

The failure exposed reconnect scratch ownership: replacing `session.jobID`
without cancelling the terminal old job left its cache until the ordinary idle
TTL. `source17` cancels that old job before replacing the identity. The time-shift
Store already owns independent media copies, so old epochs and their advertised
grace remain readable while encoder scratch is reclaimed. The focused
`phase2-reconnect-cleanup01` HTTP scope passed five events with zero failures or
skips and a closed unit. See [reconnect scratch ownership](dynamic-reconnect-cleanup.md).

`phase2-browser03` then returned `Accepted=true` on `source17`: browser and fixture
exit codes were both zero, and its Go fixture recorded one pass event with zero
failures or skips. The reported duration was 51.781 seconds and minimum-free
metric was `675704832`. Source bytes were unchanged, the private context was
removed, process groups closed, `MainPID=0`, and the cgroup PID list was empty.

Crucially, cleanup was observed **before global close**: all three observed cache
directories were absent; finite/dynamic sessions, active jobs, every Store usage
counter, stream slots and active upstreams were zero; the manager was ready.
Global shutdown was not used to satisfy that runtime cleanup predicate. Evidence
is retained under `phase2-browser03/` in the evidence root. This is an accepted
native-video plus HLS.js 1.6.0 beta 2 harness scope, not a complete original Emby
Web or arbitrary-client acceptance claim.

After admitting v3 on VM 101, `phase2-browser04` exposed another harness state
assumption: the client retained resume position 4.98 while the server's retained
window had advanced to `[6,18)`. The nominal rewind target was consequently a
forward seek. `source19` explicitly returns to Live, records `ReplayOrigin`, then
requests a real one-second backward seek. The server's retention boundary and
the existing acceptance tolerances are unchanged; expired cached client state
is not relabeled as valid history.

`phase2-browser05` passed this final v3 journey: `Accepted=true`, browser/fixture
exits zero and Go one pass event with zero failures/skips, in a reported 41.201
seconds with `minfree=714891264`. Source stayed unchanged, the private context was
removed, process groups closed and the cgroup PID list was empty. The same strict
resource predicate passed before global close. Its warm browser profile used
704 MiB after confirming accepted cache/input hashes; the 576 MiB low-water
threshold and behavioral/cleanup assertions were unchanged. This is a measured
warm-profile boundary, not a cold-start or general capacity claim. The earlier
browser04 result remains failed in its original scope.

### Actual AMD scope

`phase2-gpu01` ran on CT 104 using the `source08` binary with SHA-256
`04a8abe7cc9164bcd52acb7d1ee0f6d97bc5daf0fea3c43ceb664245fa44f784`.
It recorded **five pass events, three failures and zero skips**. Finite bitmap
output in both formats, GPU cancellation and empty-live cancellation passed.
Quiet live input with a later PGS cue stalled at its first gate in both formats
at that checkpoint. The aggregate GPU scope did not pass. Those earlier GPU
diagnostic processes exited; process termination is not a passing media
result and does not close this feature gate.

The later `topology01` one-input/no-null diagnosis retained the same first-gate
stall in all three subcases: one published segment ending at two seconds. That
topology was not adopted. The observation does not establish `null` as the root
cause, and no additional parameter sweep is scheduled by this record.

Subsequent static review identified a decoder-queue wake-up gap in FFmpeg. The
private candidate and its selected runtime gates are recorded below. The formal
successor has now been rebuilt and verified in its own prefix; the previous
published toolchain remains unchanged.

### Decoder queue wakeup candidate acceptance

The affected decoder can hold packets in its overflow FIFO while waiting on the
incoming queue's condition variable. Scheduler unchoke signaled a different
waiter condition; without another packet or EOF, it could fail to revisit the
buffered packets. The independent patch uses a retained, coalesced receive
interruption to retry that FIFO without modifying packets, timestamps, or EOF.
The [toolchain patch contract](../../scripts/test-env/toolchain-patches/README.md#decoder-queue-receive-wakeup)
describes the lock and publication boundaries.

The first native scope, `native-evidence01`, correctly reproduced the ordinary
wakeup failures but failed its stop-waiter assertion: that exact cancellation
branch returns EOF without delivering retained overflow. The failure is preserved.
The revised harness requires that cancellation result and checks release of the
undelivered buffers; ordinary wakeup and normal EOF delivery remain strict.

`native-evidence02` passed both controls: the original source reproduced four
bounded deadlocks among seven cases, and the candidate passed eight cases with
zero deadlocks. Semaphores and mutex boundaries establish the interleavings;
three-second child watchdogs only bound failed cases. Native candidate binary
SHA-256 is `bed8249d1367d1743239e508f50efbc78a3bc27067c52841cbbdb83b3c0ce0f0`.

`gpu-validation01` then passed the original dual-input plus null-output path,
original stderr pipe, and unchanged strict AMD assertions in 2.02 seconds. TS,
fMP4, and empty-stream cancellation passed. The complete run retained 192 frames
at 16 fps, visible subtitle pixels only at frames 96 through 111, the 12-second
source interval, stable initialization/mux clocks, both pre-EOF progress gates,
and owned process/GPU descriptor cleanup. No debugger or ptrace trampoline was
used in this acceptance run.

The saved first prefix was 3,653 bytes with SHA-256
`1b4bdb6113e33971ac7866c3b0f4cc6259a9124c4132fd4f61c33e66f3d30df3`, identical
to the failed control. The full 7,022-byte source SHA-256 was
`14fa030ebae926e9f7a88f02dc66d90f7fdb269e8176b3c5863ece56ed1820ca`.
Evidence remains on CT 104 under
`/opt/goby-amd-media-20260919/phase2-ffmpeg-wakeup-candidate01/`, in
`native-evidence01`, `native-evidence02`, and `gpu-validation01`.

The accepted candidate patch SHA-256 is
`82ae1c9db24fedc232993f9638abd41284b00c9c46e3908344c80d8b8e196ea9`.
Its unchanged bytes are now a separate formal patch. The `amd-media-v3` installer
adds an out-of-tree build, complete strict-only baseline source, source-first
native includes, and a pre-publication native gate with immutable-input receipts.
The patch and harness build inputs affect the successor identity. Formal recipe
execution and actual GPU results now follow below, with the prior published
prefix retained.

### Formal v3 successor and GPU verification

The first formal attempt, `phase2-toolchain-successor01`, was stopped after the
monitor treated a transient `du` file disappearance as a failure. That original
stopped scope remains preserved, as does `native-evidence01` with its incorrect
cancellation expectation. The later success is not a claim that the first native
or formal build attempt passed unchanged.

`phase2-toolchain-successor02` rebuilt libplacebo and FFmpeg from the formal
`source18` recipe with `skip-package-install=1`. Its native control reproduced
four expected bounded deadlocks among seven baseline cases; the candidate passed
eight cases with zero deadlocks. The build exited zero and published:

`/opt/goby-amd-media-20260919/toolchains/ffmpeg-9.0.1-goby-cb8b6d298456`

| Artifact | SHA-256 |
| --- | --- |
| `ffmpeg` | `c8887f1a2b5a6777c1285fd7514049ec175947048d82e4b3700e44115da34843` |
| `ffprobe` | `fddc50127245a7a836b5fb6789e1606ebe9e12bcf3c9e2fab5b32bfb7a0b703f` |
| Published runtime archive | `c49fe996ace971f9b8ce2c7f9b784ed5d9497f3aed8d8bbaf6c8306f6affd2b2` |

`phase2-gpu-formal01` compiled the actual `source18` media/transcode tests with
race instrumentation and no overlay. Its four formal-runtime groups recorded
21 pass events, zero failures and zero skips: live TS/fMP4 bitmap publication and
empty-stream cancellation; existing HDR/deinterlacing, finite bitmap, text-burn
and cancellation paths; P8.1 and complete P7 MEL SDR/HDR10 conversion; and rejection
of the tested nonzero residual. This closes those selected GPU gates, including
the quiet late-PGS scenario, rather than every AMD codec, source or client profile.

The build and all four GPU units have `MainPID=0` and are inactive. GPU scratch
directories are empty and no owned processes remain. The previous prefix and
the 27 recorded system GPU libraries retained their bytes; the installed dpkg
package inventory and versions were unchanged. The formal prefix checksums also
matched after execution. These are this scope's closure facts, not complete
phase 2 resource closure.

The formal runtime archive and evidence are retained locally in the Git-private
`.git/amd-media-compatibility-20260919/phase2-formal-successor01/` directory.
The evidence archive retains `phase2-toolchain-successor02/`,
`phase2-gpu-formal01/`, and the published prefix's native gate receipts. The
directory's historical `successor01` label does not change the successful
`successor02` receipt identity.

### VM v3 regression and final builds

`software-environment-v3.json` records the VM installation's 146 matched runtime
files and successful version probes. `phase2-media-v3-regression01` on `source18`
then passed 2,169 events with zero failures and 13 environment-specific skips;
the relevant actual GPU/Dolby Vision profiles have separate CT 104 acceptance.
Those skips are not converted into passes or added to the CT counts.

`phase2-final-build01` produced both final artifacts with `CGO_ENABLED=0` and
unchanged `source18`:

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| Ordinary server | 35,393,841 | `68801cc8a156e88b16fac480c48fda9ee1067dbf1f3d56535762b6e69f6b157f` |
| Embedded administrator dashboard | 36,620,733 | `207ef8f84b7bdd1674fd11929dae4bd467794c1c5c3e1415adcbd9ab42e13074` |

`source19` changes only browser verification material. Its Go, production frontend
and toolchain inputs are identical to `source18`, so these artifacts retain their
recorded source/build identity while the corrected browser scope is separately
bound to `source19`. No production service promotion follows from these builds.

## Goals and current contracts

| Area | Contract being implemented | Current boundary |
| --- | --- | --- |
| HLS text subtitles | A fixed set of at most eight authorized text renditions; selected track, off and signed offset are subtitle-view choices | View changes preserve the audio/video plan; route and integration source exists, but complete client and media acceptance remains open |
| Rolling subtitle output | Render each subtitle segment against the actual corresponding media interval | A cue crossing segment boundaries appears complete in every overlapping segment; do not clip it into invented fragments or derive intervals from nominal duration alone |
| Dynamic playback | Receive and retain complete produced output for bounded replay, pause/resume, seeking and return to the live edge | No history exists before the server actually stores it; upstream catch-up and recording are separate features |
| Time-shift storage | The `internal/timeshift` Store bounds retained duration, storage, readers, publication and presentation ownership | Service configuration and publication wiring are present; the actual retained window also depends on byte limits and advertised-resource grace |

Subtitle rendition identities are fixed within the presentation. Selecting a
different text track, turning subtitles off or changing its offset must not
replace the audio/video plan or rewrite already published audio/video artifacts.
The standard `subtitles.m3u8` and `live_subtitles.m3u8` adapters and rolling views
are registered in source. They require a bound presentation/revision and explicit
supported subtitle view; registration is not original-client acceptance.

The initial source anchors are the [HLS subtitle planner](../../internal/playback/hls_subtitles.go),
[closed rendition plan](../../internal/transcode/hls_subtitle_plan.go),
[server rendition adapter](../../internal/server/hls_subtitle_renditions.go), and
[time-shift storage contract](../../internal/timeshift/README.md). These files may
change during repair; their presence alone is not execution evidence.

Internal text captions use closed companion containers containing copied
reference and original subtitle packets from the same primary demuxer. A complete
companion is extracted before advancing that track's observed watermark. Final
completion requires successful producer EOF and all companion callbacks; a timer,
an empty pipe, cancellation or a failed producer cannot prove an empty interval.
See [continuous live publication](live-publication.md) for the producer contract.

External captions require trusted operator declarations. A finite `document`
must be fully parsed within its byte bound. `webvtt-hls` requires
`clock: "mpegts"` and `segmentClock: "timestamp-map"`: each segment's own map
explicitly anchors its complete `EXTINF` interval. `webvtt-stream` requires
`streamWatermarks: "goby-note-v1"` and monotonic explicit NOTE barriers.
Neither protocol guesses completeness from media progress, URL names, an ordinary
LOCAL zero or wall-clock time. MPEGTS wrap resolution requires a measured anchor
in the same generation. The [dynamic-source contract](../../internal/dynamicsource/README.md)
owns declaration examples and fetch restrictions. Its external declared clock
and watermark protocol remains bounded to 30 days within one generation; the
internal live-media clock's signed 64-bit range does not remove that separate
external-protocol limit.

## Retention, clocks and presentation changes

The configured default is a **600-second upper retention horizon**, **512 MiB per
window** and **2 GiB globally**. The service now wires the bounded startup settings
described in [transcoding configuration](transcoding-configuration.md#dynamic-time-shift-configuration).
Six hundred seconds is not a guaranteed replay duration: byte pressure can shorten
the actual visible window.

Each presentation fixes its HLS target duration before advertisement, while
`EXTINF`, media clocks and retained intervals use actual published durations.
Advertisement keeps removed segment URIs and their initialization resources
readable for the promised grace interval. Grace resources, temporary copies and
open retired media remain charged; quota exhaustion cannot erase those promises.
After the first advertisement, the current visible media and required live
initialization use at most **one third of the window byte budget**, leaving
headroom for grace and in-flight work. See the
[Store contract](../../internal/timeshift/README.md) for exact admission and
retirement behavior. These limits are not a measured throughput or capacity claim.

Subtitle offsets are bounded by retained subtitle completeness. A positive delay
can require input before that retained source boundary; requesting such a segment
returns HTTP 410 `subtitle_window_expired`, even while its unshifted URL remains
readable under advertised-media grace. A surviving crossing cue does not prove
that the discarded prefix is complete. Clients must request the offset subtitle
playlist again and use its advertised children: the view may omit unavailable
leading segments while leaving the audio/video window unchanged. The remaining
live subtitle interval must still cover three fixed target durations; a window
that is too short after those omissions remains not ready.

Only complete output intervals that have actually been published into the Store
are replayable. A paused client does not pause upstream reception: authorized
production continues within the duration, byte and lifecycle limits, so old
paused positions may expire. The intended server contract returns
HTTP 410 `window_expired` when a requested position has left the retained window; it must
not fabricate earlier media or silently substitute a different historical seek.

Restart does not retain playback history or restore old presentation IDs.
Recognized owned temporary artifacts are handled by the Store's guarded cleanup
contract. Unrecognized files or failed cleanup must not be treated as recovered
history or free storage.

A source reconnection starts a new generation with its own initialization and
clock/discontinuity metadata. Segment sequence numbers remain continuous within
the presentation. The presentation clock accumulates actual published media
durations; network downtime does not become synthetic playable media. Old
generation initialization remains associated with its retained segments.

Dynamic bitmap subtitles are part of burned video output. Switching that burned
representation requires a new presentation; already retained burned history
cannot be rewritten as a different track or an off state. This is separate from
selecting a text-subtitle view over an unchanged audio/video presentation.

The Store is not an authentication service. Server integration must recheck
current token, playback ownership, catalog and media permissions for requests,
and retire owned production/readers on revocation, stop or close. Package scope
checks and storage quotas supplement those server checks.

Dynamic video planning prefers encoding with closed GOP boundaries while
retaining compatible copied audio. This does not grant a remux-only user encoding
permission. Copied video requires actual restart evidence for each published
segment; a container key flag alone does not establish independently replayable
media. The selected strict TS/fMP4 repair results above retain their recorded
scope. The formal successor's selected live bitmap cases now pass, but these
results do not imply every dynamic source or hardware/client profile is accepted.

## Verification and handoff state

The selected phase 2 implementation, verification, final builds and closeout are complete.
Recorded unit, library, media, HLS, HTTP, progress, browser and formal AMD/toolchain
passes retain their exact source/runtime boundaries, including the new v3 VM
regression and `phase2-browser05`. The final VM closure receipt and this
documentation complete phase 2. At this closeout checkpoint, phase 3 is the next
approved scope and has not started. All original failed attempts remain
unchanged; no deployment is claimed.
Ordinary verification remains on `ssh test-env`; the existing user-approved AMD
exception remains PVE CT 104 through `ssh pve` when an affected check requires
the device. No local verification is authorized. The phase-owned PostgreSQL unit
`goby-amd-media-phase2-postgres` on port `54919` is closed. The read-only
`amd_admin` check used the passwordless owned Unix socket with `-U amd_admin`,
matched its owned cluster identity and found `other_clients=0`
and `noninfra_backends=0` before stopping only that unit. The final state is
inactive/dead/not-found with `MainPID=0`; the original process, port listener,
owned socket/lock and `postmaster.pid` are absent. The stopped cluster remains
retained. Unrelated historical services and evidence were outside this cleanup.
An earlier auxiliary check using role `postgres` failed; the successful
`amd_admin` check and final closure receipt retain their separate outcome. No
environment-password fallback was used for the successful connection.

`phase2-vm-closure.json` records all 27 phase-owned VM workers as terminal, with
`MainPID=0` and empty cgroups, and preserves each original success or failure.
Source19's 1,322 entries matched the Git blob/mode inventory before and after
closure, with no failures. Every preceding receipt hash remained unchanged. The
separate CT formal-toolchain/GPU closure also passed; retained files and stopped
database storage are intentional evidence, not live worker resources.
The final closure receipt SHA-256 is
`c73ccd190165d1e57db3c7c374236ccba606b05727d9b9c2fcd1bf1bb82b95ed`;
its recorded intent SHA-256 is
`c9b5d44b0a99c19703a02b3666b56bd2103bc339fd275bf1e57b76bc60dd282e`.
The retained evidence root is
`/opt/goby-amd-media-20260919-50f45177f297/evidence`. Seven older test binaries were
preserved with gzip and their original hashes, saving **109,516,600 bytes**.
This storage action does not discard their evidence or change any test result.

Two older transfer archives, `source01.tar.gz` and `ffmpeg02.tar.gz`, were also
relocated with matching hashes to the local Git-private
`.git/amd-media-compatibility-20260919/retained-remote-archives` directory. Their
remote copies were removed, releasing **78,400,403 bytes**; the retained receipt
is `phase2-archive-relocation.json` in the evidence root. Phase 1 source and formal
artifacts remain unchanged. Relocating transport archives is not deletion of
their retained evidence or a new source/build result.

All accepted raw binaries are now retained with matching hashes in the local
Git-private `.git/amd-media-compatibility-20260919/retained-remote-artifacts`
directory; their remote copies were removed. The complete phase 1 source was
also archived: 6,149 entries and 125,925,229 source bytes, retained as
`retained-remote-archives/phase1-source-retained-20260919T114335Z-eebe171d.tar.gz`
under the same Git-private root, SHA-256
`6adf0a32971a9c131b31e166e62ee8bdda599c53737724a2ee1b1dbe01032204`.
The old remote phase 1 source directory was removed after preservation; the
phase 2 source remains retained. These relocations preserve historical source
and artifact identities rather than changing their verification results.

Disk expansion from 122 GiB to 154 GiB remains awaiting user permission; no resize
was performed. Reversible archive/artifact relocation supplied the capacity for
the recorded final verification, so that request no longer blocks these completed
scopes.

At this closeout checkpoint, the handoff is the approved phase 3
library/client-management scope, which has not started. The library helper remains explicitly skipped; running its separate
mount profile would require a reviewed opt-in scope. It is not converted to a
pass. Existing phase 1 and phase 2 passes are retained
within their unchanged scopes. Failed receipts remain visible; this record does
not request a new acceptance matrix or replay of completed checks.

OCI and non-AMD GPU work remain deferred. Dynamic replay does not add Live TV
channels, tuners, EPG, scheduled recording or provider-specific historical
catch-up. User Configuration writes and the remaining library/client-management
work remain phase 3 obligations.
