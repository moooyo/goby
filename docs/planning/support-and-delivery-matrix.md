# Support and delivery matrix

The completed [AMD media and compatibility plan](amd-media-compatibility-plan-20260919.md)
is a separate three-phase increment with recorded scope boundaries. Phase 1 source adds HEVC/AV1 output,
supported Dolby Vision conversion, AMD processing and broader copy seeking;
its [execution record](../development/amd-media-phase1-20260919.md) tracks
completed selected-profile verification, builds and closeout. CT 104 is the user-approved AMD worker;
ordinary checks remain on `test-env`. No old client, deployment, non-AMD or
OCI acceptance is inherited from this work. Phase 2 subtitles/time shifting has
completed its selected v3 CPU/GPU/browser verification, final builds and owned
PostgreSQL/worker/documentation closeout. It is verified and closed within those
boundaries. Phase 3 library/client/management verification and closeout are also complete. The
[phase 2 record](../development/amd-media-phase2-20260919.md) owns exact results.
The [phase 3 record](../development/amd-media-phase3-20260919.md) binds final
source16 to actual execution sources, composed server coverage, selected
package/media scopes, the real twelve-stage administrator journey, application
builds and closed resources. Its final source contains 1,625 selected files.
Original failures remain retained rather than relabeled as passing runs.

The current branch is `main`. Product commit
`80198b6aa8a163b696ceaff64da831847d82e496` was fast-forward merged and pushed to
`origin/main` on September 20, 2026, after development on
`codex/amd-media-compatibility`. This follow-up documentation commit is separate
from the verified product snapshot and does not establish production deployment.

Phase 3 completes selected library editing/directory browsing, persistent
preferences and state, protected artwork/avatars, music tags and core entity APIs,
navigation/events, management fields and native task adapters. The ordinary
server inventory has 812 unique parent passes and one explicit AMD skip, not
a single full-pass run. The maximum-size artwork backup profile uses 1 GiB
PostgreSQL; the earlier 512 MiB OOM remains a failed profile. Source16/ui09 passed
10 mocked checks, 12/12 real stages and final desktop/mobile visual review.
Both application artifacts actually use source14 and are accepted through
unchanged production inputs. All 42 workers and the owned PostgreSQL runtime
closed with twenty protected services unchanged and a 172-file hashed evidence
handoff. This does not accept full original Emby Web, provider-online, OCI,
non-AMD, broader capacity/platform or deployment scope.

Phase 2 source distinguishes the internal live-media clock from external caption
protocols: cumulative internal clocks use nonnegative signed 64-bit ticks, while
the explicitly declared external clock/watermark contract remains limited to
30 days within one generation. Neither limit is the replay window. Default replay
has a 600-second upper horizon with 512 MiB per-window and 2 GiB global budgets;
actual visible duration may be shorter because advertised grace and readers stay
charged. The recorded media and native-video/HLS.js harness scopes passed; they
do not claim full original Emby Web compatibility. Phase 2 resource closeout is
recorded separately. The existing mount-helper skip remains an explicitly separate opt-in
profile, not a passing test.

Initially recorded on 2026-09-15 from committed plans and saved evidence. This is a
delivery decision map, not a worker or runtime-status ledger. The user selected
the [media, collections, and management wave](../development/feature-wave-20260919.md),
whose scoped functional acceptance, builds and owned-resource closure are
complete. Code commit `39893aa195553d501ba0ffb56ce618066f2cb5e1` was fast-forward
merged into `main` and pushed to `origin/main`. OCI work and provider-specific
acceptance remain deferred by the user's decisions. The
[current handoff](../development/handoff.md) governs current disposition;
older queue entries are historical. The
[delivery and verification](delivery-and-verification.md) retains the complete
M2-M6 requirements. The M2-M6 scope remains incomplete, M7 remains deferred, and the
project license and final distribution payload remain pending.

## Historical September 19 feature-wave scope

The [wave verification record](../development/feature-wave-verification-20260919.md)
owns its historical results. The server scope covers 692 tests: 676 original passes
and 127 targeted rerun passes overlap and must not be added together. The core
10-package run recorded 5136 passes and one existing mount-helper skip,
`TestRootBindingFullScanMountNamespaceHelper`. Separate
completed scopes recorded 181 identity passes, 497 backuppg passes and 177
recoverydb passes. The real browser journey passed seven stages, and five new
mock checks passed. The full recovery-manager package run `recovery-accept04`
recorded 73 passes, zero failures and zero skips; all four existing mock checks
also passed.

Ordinary and embedded builds passed, and all 1178 source files matched the
accepted source. All 33 recorded worker invocations closed, owned PostgreSQL stopped, and the
five protected processes retained their PID, start time and executable
identity. The declared functional, build, resource-closure and code-integration
scope for this wave is complete. The rows below share those results; the
counts above are not independent per-row totals. This does not complete the
full M2-M6 goal or accept deferred provider and OCI work.
Use the [current status](../development/current-status.md) and
[handoff](../development/handoff.md) for current disposition. No result here
changes the ownership or acceptance of the historical evidence below.

| Group | Implemented source scope | Current acceptance evidence | Remaining boundary |
| --- | --- | --- | --- |
| Media and subtitles | Advanced text subtitle delivery, font attachments and HLS burn-in; generated TS/fMP4/packed-audio and encoded multi-variant HLS; software HDR/deinterlacing; configured dynamic sources; narrowly admitted nonzero H.264 video-copy seeks. | The declared wave scope passed its shared acceptance, build and closure gates; code is merged and pushed to main. The scoped server/core results above apply. See the [source contract](../development/advanced-media.md) and consolidated verification record for exact output and profile limits. | No full third-party-client matrix, arbitrary source/profile support, actual GPU acceptance or Live TV business subsystem is claimed. |
| Collections and library state | Persisted Playlist/BoxSet containers, ordered duplicate playlist entries, unique BoxSet membership, current access checks and per-user state; bounded original downloads and recoverable physical media deletion. | The declared wave scope passed its shared acceptance, build and closure gates; code is merged and pushed to main. The scoped server/core and browser results above apply. The [API inventory](../api/implemented.md) records the container, membership, download and deletion boundaries. | Container deletion retains media; physical deletion and download retain their separate authority and source restrictions. These results do not close complete M2 storage or delivery coverage. |
| Administration and management | Expanded user policy and account operations, feature discovery, closed management configuration, metadata/subtitle/cache task executors, and configured provider adapters. | The declared wave scope passed its shared acceptance, build and closure gates; code is merged and pushed to main. The scoped identity/server, recovery-manager, real-browser and new/existing mock results above apply. Provider-specific acceptance remains user-deferred. | OCI work remains user-deferred; no complete M5, M6, provider or distribution acceptance is inferred. |

## Historical checkpoints

The Programs, M2 native-concurrency, M5 administrator, OCI and M6 packaging rows were updated from
the September 18 checkpoint.
Other rows retain their historical evidence and acceptance boundaries.

The [later final-regression failure and incident](../development/programs-final-regression-incident.json)
introduced incident-recovery prerequisites. Follow their resolution in the
authoritative queue/status before applying the transition or verification rules below.

`Accepted` below applies only to the named result, artifact and profile.
`Open` means the stated gate is incomplete; it does not by itself establish a
product defect. `Unknown` identifies a fact that the cited record does not
establish. Historical evidence is reusable within its scope, without replaying
consumed inputs or silently transferring acceptance to another binary.

## Artifact and client identities

| Identity | Bound artifact or build | Established scope and evidence |
| --- | --- | --- |
| A: historical client candidate | `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`, schema 28 | [MP3](../development/audited-mp3-client01-closeout.json), [FLAC](../development/audited-flac-client01-closeout.json), [Episode01](../development/audited-episode-client01-closeout.json) and [Subtitles01](../development/audited-subtitles-client01-closeout.json) bind these historical bytes. The selected isolated rollback also used them. Their acceptance is not transferred to the current Programs runtime. |
| A: current audited Programs runtime | Source `3d6b79b36b6a5050e174f7a152f214b9356608c8`, binary `ead67c8faaf4cde88f7fe1bf57ffed43705259aba7b29f59c3473747afd732fb`, schema 28; epoch/binding format 4 | Programs admission r02 accepted its 16+4 requests and closure. The later movie remains failed with client/media/G3 false. The accepted 4c build and 41 executor components have not changed this runtime. |
| B: admitted embedded candidate | `59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312` | [Fresh provision, inspection, seed and admission](../development/fresh-embedded-candidate-checkpoint.json) passed; `clientAcceptance=false`. Its [native catalog restart](../development/native-catalog-restart-verification.json) used a separate root profile. B is neither A nor E11. |
| E11: retained build/G2 baseline | `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`, 30,691,123 bytes; amd64 package `2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a` | [Package builds](../development/systemd-package-build-verification.json), [source bridge](../development/systemd-package-source-checkpoint.json) and [composite G2 acceptance](../development/internal-amd64-installation-acceptance.json) retain their exact scopes. These bytes do not contain the new Programs route. |
| Programs 4c correction: accepted build, pending runtime update | Source `4c06893d63e52cdb43ef5a6751b289ee7b0611c7`, binary `791bf3b91f579bfc95addfc7947fee68fa4fa22f064bac6753dcf830b6da767e`, schema 28 and 57 assets | Full8f1d independently accepted 25 packages, 2310 top-level tests and both builds, retaining the declared mount-profile gaps. Executor r02 and separate capture accepted 41 component groups and the actual current-state capture with closure; the original empty-stderr reader failure is retained. Candidate update, fresh admission and new client acceptance remain unexecuted. The earlier [focused 14/118 result](../development/live-tv-programs-focused-verification.json) retains its original source scope. |
| Canonical 116 AAC-preset correction: accepted full/build | Source `1162808afafacdc9ab9a4d6c037263764b1afd7a`, binary `7a504ce0db8d0c0e96e55772f77328e33fb2f6cbf0395c3f4585db8fbfc4ef81`, 31640005 bytes; schema 29 and 59 assets | Original full and independent review accepted 25 packages, 2442 top-level passes, 28 required regressions, two builds and 5582 source files with closure. The finite native r09 complete/cancel/delete-owner software profile is now separately accepted on these 116/e6 bytes, with owned closure and its causality limitations retained. Explicit mount/inert-helper gaps and prior native failures remain. This does not change A or establish W, other hardware/codec profiles, completion of M5, OCI or distribution acceptance. |
| R32: old recovery binary | `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`, schema 27 | [Actual source32 restoration and restart](../development/isolated-source32-recovery.json) passed. This proves old-binary recovery, not a Programs-successor restore or upgrade. |
| W: pinned original Web Client | Web Client distributed in official Emby `4.9.5.0`; package SHA256 `1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843` | [Original-client hosting record](../development/audited-original-client-hosting.json) establishes package/version provenance; the individual client closeouts establish each journey's outcome. Their public summaries do not restate the browser build. The saved Subtitles01 readback below supplies its browser version; other runs retain their own missing metadata and do not inherit older source12 values. |

**Recorded artifact decision:** the [core resolution](../development/core-client-acceptance-resolution.md)
selects one direct A-to-Programs-successor transition with its existing actor
lineage and external administrator asset override. E11 remains the accepted
historical build/G2 baseline, not the identity of changed source. A transition
requires verified actual artifacts and reviewed recovery/journey contracts;
selection alone is not admission. Active jobs and permitted parallelism belong
only in the authoritative queue/status.

The earlier [user-interrupted full run](../development/live-tv-programs-full-interruption.json)
safely closed after 10 packages/341 passes, zero failures/skips and no build in
that attempt. Its 109 partial identity passes are excluded. This is historical
partial evidence, not the latest regression outcome or a passing full suite.

## Internal candidate gate definitions

| Gate | Required result | Retained evidence | Admission or reuse rule | Required binding |
| --- | --- | --- | --- | --- |
| G0: artifact and inputs | One verified successor binary/package, source/build bridge, exact client/browser/media pins and reviewed retained-state inputs | The [A transition decision](../development/e11-candidate-transition-decision.md) retains A's actor lineage and explicit external administrator asset override. Actual artifact/admission results are tracked in current status. | Admit one direct transition only with actual source/build evidence, the declared startup state delta, recovery and final journey contracts. | Admitted current state/runtime, typed episode/subtitle contracts and bounded transition/recovery evidence; readiness is not inferred from this matrix. |
| G1: supported core client | Complete declared movie, episode and subtitle journeys on the successor, plus the explicit reviewed audio reuse bridge or affected new audio evidence | A's historical audio passed within its recorded profile. [Accepted baseline](current-execution-plan.md#accepted-baseline) and individual closeouts retain their scopes. | Each admitted final journey uses its preceding complete closeout. Do not repeat completed diagnostic/reference work without an invalidating change. | Pinned original client/media, current capacity and retained-state admission; a movie or browse contract does not automatically cover episode/subtitle execution. |
| G2: internal installation | Complete nonroot package installation, bootstrap/catalog journey, normal stop/start and final sealing | E11 internal amd64 installation is accepted through original runtime plus independently reviewed HTTP/archive evidence. [Composite acceptance](../development/internal-amd64-installation-acceptance.json) | Retain this historical G2 slice and failed sealer. Review successor applicability only for assertions invalidated by the actual change; do not automatically queue another installer run. | No remaining prerequisite for the original accepted slice; the successor's source/build identity remains a G0 requirement. |
| G3: promotion and recovery | Current-state preservation, applicable recovery evidence, bounded upgrade and post-upgrade workflow for the actual successor | R32 backup/recovery and the historical A rollback passed in their recorded scopes. [Recovery proofs](../development/isolated-source32-recovery.json), [upgrade contract](../development/audited-main-upgrade-plan.md) | After G1, bind the actual main workflow and refresh only prerequisites invalidated by changes. Explicitly handle old-binary task reconciliation; do not assume automatic rollback. | Current main state and capacity; G1 and applicable safety conditions must pass before promotion. |

The [saved Subtitles01 metadata](../development/core-response-identity-verification.json)
confirms Chromium `153.0.8010.12` and Playwright `1.63.0` for that execution.
That readback does not establish the browser versions of other historical runs
or the currently installed executable; recover their own pins before reuse.

G2 closes the claimed installation slice. G1 and the applicable G3 safety
prerequisites remain mandatory for main promotion. Capacity, hardware and broader
support rows below do not become automatic dependencies of every internal
increment. They remain required for their own claims and complete M2-M6 delivery.

## Original-client slices

All proposed final-client rows target the G0-selected Linux amd64 artifact;
historical results remain on their recorded candidates. W identifies the client
package, not every released Emby client. Exact fixture hashes and media details
must come from each retained input before execution; a filename or extension
does not establish a codec profile.

| Slice: client / media path / deployment | Implementation and original-client status | Evidence | Next concrete action | External prerequisite | Gate |
| --- | --- | --- | --- | --- | --- |
| W / 180-second MP3 through universal audio / A native candidate | Implemented; scoped original-client journey accepted on A, including physical delivery, seeks, stopped state and closure. No audible-output claim. | [MP3 plan and outcome](../development/audited-mp3-client01-plan.md), [closeout](../development/audited-mp3-client01-closeout.json) | Any successor reuse requires the explicit source/configuration/media/transition bridge or an actually affected audio journey. | W browser pin and original fixture/input metadata. | G1 audio |
| W / 180-second FLAC through universal audio / A native candidate | Implemented; scoped original-client journey accepted on A. No independent output-codec or audible-output claim. | [FLAC plan and outcome](../development/audited-flac-client01-plan.md), [closeout](../development/audited-flac-client01-closeout.json) | Apply the explicit successor reuse bridge to FLAC; preserve the accepted A result without relabeling it as a successor execution. | W browser pin and original fixture/input metadata. | G1 audio |
| W / recorded movie, direct video and cross-login resume / historical and current Programs candidates | Movie05/06 retain their historical failures. Current `movie-programs-01` also remains failed: four page errors, unmatched partial-media 341 and complete HTTP 500 at 338. The 6965 persistent-state receipt accepts only an update/admission baseline, with client/media/G3 false. | [Movie05 closeout](../development/audited-core-movie05-owned-state-closeout.json), [Movie06 plan review](../development/audited-movie06-plan-review.md), current failed movie state | Bind a new admitted artifact/runtime and fresh baseline before a new movie/resume journey; preserve the strict UI and physical-delivery rules. | Complete owned closure, current admission, W and exact fixture pins; original failures are not waived. | G1 movie/resume |
| W / TV browse and recorded Episode2-1 video, pause/seek/stop / A | Parent metadata and playback implemented; Episode01 retains its formal rejection. The consumed no-playback TV diagnostic does not replace it. | [Episode01 contract](../development/audited-episode-client01-plan.md), [review](../development/audited-episode-client01-review.md) | Require the typed retained-episode entry/closure contract and assess browse/playback through all declared checks and closeout. | W and retained episode state must be bound without actor reset; use the authoritative queue for entry readiness. | G1 episode/browse |
| W / recorded movie plus external SRT and VTT / A | Historical cues/seek/Off completed, but the saved result does not explain two undefined errors or resolve physical293. | [Subtitle result and response identity](../development/audited-subtitles-client01-review.md), [closeout](../development/audited-subtitles-client01-closeout.json) | Require the typed retained-subtitle entry/closure contract and response-identity/timing gates for the declared SRT/VTT journey. | Admitted latest state and exact client/media pins; use the authoritative queue for entry readiness. | G1 subtitles |

## Remaining delivery slices

These rows retain the complete milestone scope. A `Later profile` designation
keeps an independent acceptance scope; ordering belongs in the immediate queue
and no requirement is removed from M2-M6.

| Slice: milestone / media path / deployment profile | Recorded status | Evidence | Next concrete action | External prerequisite | Relation to next internal candidate |
| --- | --- | --- | --- | --- | --- |
| M2/M6 / catalog, ACL and normal restart / Linux amd64 nonroot systemd | Product catalog checks accepted in their scopes; B root restart and E11 r04 nonroot runtime passed. The internal amd64 installation gate is now accepted with the archived final-state supplement. | [B restart](../development/native-catalog-restart-verification.json), [E11 r04](../development/systemd-installation-fourth-attempt.json) | Reuse the accepted G2 result. Keep PG restart, host durability and broader storage profiles separate. | Saved r04 evidence; any new run needs fresh ownership/capacity. | G2; part of the proposed internal package slice |
| M2 / scan plus concurrent catalog HTTP / E11 baseline and full137 bridge, 1,000 tiny files | SQL 10,000-leaf and tiny-file baselines accepted. Earlier 61 methods and separate 12 reader, 8 transport and 6 pool groups retain acceptance. An additional 14 bridge/interface checks (12+2) actually passed and closed. The additive schema-29 amendment is published source-only; capture, source binding, fixture and native workload remain unexecuted. Throughput/SLO remains unaccepted. | [SQL baseline](../development/catalog-capacity-isolation-verification.json), [real-file baseline](../development/catalog-real-media-capacity-verification.json), [running-observation components](../development/native-capacity-running-observation-verification-20260916.md), 14-check and schema-29 checkpoint | Obtain fresh admission under the unchanged 6 GiB memory, 4 GiB root-free and 30000-inode gates before consuming the first capture, then bind the amended source and actual owned workload. Require actual HTTP/Running overlap and closure in both phases; do not replay unchanged groups. | Fresh read 8ef605 at 2026-09-18T16:21:59.553728Z has 9398964224 root-free bytes, 5465229 free inodes and 5510918144 bytes MemAvailable, still below the unchanged 6442450944-byte memory floor. Disk/inode floors pass; the observation is not a reservation or M2 admission. Completed raw-file releases cannot be reclaimed again. Persisted Running state does not prove continuous filesystem or ffprobe activity. | Independent capacity evidence; source publication and component checks do not close G1/G2/G3 or full M2 |
| M2 / inaccessible or blocked storage and host restart / owned Linux filesystem profile | Canonical3060's three-media/three-scan permission-loss profile is accepted with actual nonroot EACCES, catalog/UserData/xmin/root-approval preservation and recovery. The original outer failed a terminal-state guard; separate close-02 closed all owned resources and the combined independent review preserves both outcomes. Other storage faults, paused full-scan mount stages and host durability remain unproved. | Actual profile and separate closeout, [storage plan](../development/storage-root-bindings-plan.md), [remaining obligations](delivery-and-verification.md#required-scenario-matrix) | Preserve the accepted permission-loss scope. Select the next distinct required storage-fault or durability profile with its own fixture and preservation plan. Keep the paused mount isolated. | Suitable owned filesystem/fault mechanism; a host reboot needs an explicit affected-service preservation plan. | Only the recorded permission-loss profile is accepted; it does not establish capacity, NFS/SMB, host restart or complete M2 |
| M3/M4 / remux, progressive/HLS software conversion and track changes / Linux amd64 CPU | Planner/pipeline paths have scoped implementation evidence; broad original-client/media matrix remains open. Nonzero video-copy seeks were unsupported in this historical profile; the September 19 source separately implements the bounded copy-seek path described above, without extending this row's historical acceptance. | [HLS](../development/hls-playback.md), [progressive video](../development/progressive-video-playback.md), [audio profiles](../development/audio-profile-playback.md) | After G0, choose one uncovered input/output pair from the existing contracts and bind negotiation, seek, cancellation and cleanup checks; use software H.264/AAC as the documented baseline where enabled. | Pinned FFmpeg and permitted fixture; exact selected client capabilities. | Later media profile; M4 needs M3 session/source identities, not every unrelated feature |
| M3/M4 / A1 AAC original, mismatch and PCM source-window / native 7fec CPU | A1 r03 is scoped accepted on 7fec/f091; W b7/4f remain failed and consumed. Later reference r06 bf7c completed independently reviewed metadata/package capture and actual file binding, then its sole live dispatch failed before the expected application executable was observed. The application child returned -25 with empty streams; no bootstrap/browser result exists. Independent review accepted the failure and physical closure, with its 2 GiB backing retained. | Latest reference and W state, [r06 failure/closure review](D:/Code/goby/.git/m3-m4-a1-artistless-reference-instance-preparation-20260918-r06/source-publication-bf7c20e8a4d9/saved-controller-evidence-all-r01/independent-failure-closure-review.md), [bounded signal diagnosis](D:/Code/goby/.git/m3-m4-a1-artistless-reference-instance-preparation-20260918-r06/new-startup-signal-diagnosis.md) | Complete the separately owned strace tool acquisition, then one bounded fresh startup diagnosis with the original application budgets and no bootstrap/browser replay. R04 signed Release and its original host graph are accepted. R05 third-range timeout remains failed. The complete public index was retrieved on Windows without local verification. R06 upload then failed before stage creation; a later read found only ld.so.cache changed among 28 fixed inputs. The cache-amendment and r07 consumer are incomplete, non-executable drafts. No complete index/package/extraction or trace is accepted; see the current handoff before selecting further work. Keep original AAC bytes and all original-client acceptance requirements. | Returncode -25 supports the recorded SIGXFSZ interpretation on this host but does not identify the executable, file or syscall cause. Do not raise the 128 MiB application file-size budget without causal evidence. Tool acquisition source/public prerequisites do not prove a startup trace or application readiness. All prior scopes remain consumed. | Native AAC protocol and finite command/capture evidence retain their exact acceptance. Reference startup/browser, W client/media and broad M3/M4 acceptance remain open. |
| M5 / administrator completeness / native dashboard | Canonical 116 full/build and the finite native r09 complete/cancel/delete-owner software profile are independently accepted within their scopes. Original 7fa950 exited 0; complete passed six stages, three references and 17 closed command records. Actual live-candidate cancel and owner DELETE requests returned 200 with matching cancelled/authority-loss terminals. All 40 outer commands, 30 resource commands, 29 HTTP handles and owned resources closed. Specific-command binding and PID-exit causality remain not-established; natural exit in the request window is possible. Raw pending flags, r07/r08 failures and earlier browser/targeted scopes are preserved. | [M5 requirements](delivery-and-verification.md#milestones-and-dependencies), accepted browser phases, [r09 independent success and closure](D:/Code/goby/.git/m5-native-outer-preparation-20260918-r09/source-publication-d27c9b10e5a6/success-review.md), scope checkpoint | Preserve the completed d27 receipts and reuse this finite result. Continue other still-required M5/deployment/hardware profiles according to their contracts; do not rerun r09 or add proof of PID-exit causality as a new gate for this accepted profile. | This acceptance is limited to actual 116/e6 software behavior and closure. Other clients, codec/hardware profiles and broader delivery claims require their own evidence. | Finite complete/cancel/delete-owner native acceptance is established; this does not close every M5 requirement or the overall M2-M6 goal |
| M4/M6 / hardware decode, encode and combined pipeline / each VAAPI, QSV or NVIDIA profile | Configuration/planning exists; no accepted actual GPU profile is claimed by this matrix. | [Hardware policy](../development/toolchain.md#hardware-decode-and-encode-acceptance), [required matrix](delivery-and-verification.md#required-scenario-matrix) | First inventory an available device/driver/FFmpeg/service-access profile; then define separate decode, encode and combined filter/subtitle/cancel cases for that exact profile. | Required GPU and driver availability are unknown here; missing hardware blocks that profile. | Later hardware profile; required for actual hardware support claims |
| M6 / embedded package / native Linux arm64 | Cross-build evidence accepted; native install/runtime and client behavior open. An amd64 pass is not native arm64 evidence. | [Embedded build](../development/embedded-administrator-verification.json), [package builds](../development/systemd-package-build-verification.json) | Select the retained arm64 artifact and define the minimum nonroot install/start/catalog/stop journey before remote execution. | Native arm64 environment and compatible PG/FFmpeg; availability not established here. | Later architecture profile; required for arm64 support |
| M6 / packaged service / OCI | R08 build, retained payload and r02 two-image import are accepted. R04 predecessor firstboot, normal Goby stop and physical closure are accepted. R09 completed the fixed catalog/ACL/UserData journey and same-image restart; two independent safe HTTP/SQL reviews and the root review now accept that business scope. Its original outer still failed final storage-evidence collection: command 68 exited 0 with its group closed, but original EOF/complete and acceptance remain false. A separate locked read-only observation accepted finite inactive-resource and exact retained-backing facts. H1 r02 subsequently failed at its client HEAD reader before any media GET; independent review accepted its owned physical closure while preserving the failed business result. The HEAD-only correction passed five real socketpair methods with independent review. The saved H1 initialization projection is also accepted within its finite scope: 35 tables, five sequences, 146 unchanged prior row/xmin digests and 22 additions with no changed or removed rows. Actual playback and the complete post-cleanup state remain pending. | Latest original state, [root r09 business review](D:/Code/goby/.git/oci-catalog-restart-preparation-20260918-r09/saved-business-projection-preparation-r01/actual-projection-01/root-business-review.md), [separate physical review](D:/Code/goby/.git/oci-catalog-restart-preparation-20260918-r09/actual-physical-observation-r01/independent-physical-observation-review.md) | The new retained-state continuation is published, composed, bound and independently reviewed as file-only work; ten/eighteen new branch checks have scoped acceptance while their supervisor warning-policy failure remains. After a user decision to resume, use that actual binding and the checked HEAD operator without repeating completed file-only stages. Reuse the existing media, library, viewer, policy and scan. Capture the missing full post-cleanup database baseline in the new PG-only stage and verify exact cleanup/startup changes before new auth/Prepared/output work. Preserve current locked protection, backing identity and PG alias checks. Upgrade and backup/restore consume actual accepted preceding stages afterward. | The H1 original HEAD response remains incomplete; its missing after-playback/database result is not manufactured. Component success and physical closure do not establish media playback. Old r03 command 65 and r09 command 68 flags remain unchanged; anchor exit 137 is not called graceful. Retained backings are not silently reclaimed; historical capacity is not reservation. | Firstboot and the fixed catalog/same-image restart business scope are accepted separately. Software playback, upgrade/restore, public HTTPS, complete OCI and full M6 remain open. |
| M3/M6 / NextUp and automatic LibraryChanged refresh / recorded original client | Implemented portions and research exist; positive selector/order and automatic client-refresh evidence remain open. | [NextUp comparison contract](../development/nextup-goby-comparison-contract.md), [gate boundaries](current-execution-plan.md#gate-boundaries) | Keep parked until a positive client query/reference state or an explicit product decision identifies a new bounded question. | New discriminating public/client evidence; no egress change or vendor-source access implied. | Feature gates only; do not block unrelated core/internal work |
| M6 / actual package contents and external distribution / each selected package | The new actual116 internal package at 62f41a7d9e30 is scoped accepted: five original member bodies/modes plus 51 unchanged legal texts, two unchanged historical indexes and the updated total-index target paragraph; 59 files, 64 directories and 54 notices in total. Actual integration and complete independent readback closed with 148/148 reader FDs, and private provenance bodies remain outside the archive. The original 116 input archive and separate accepted old 7fec package are unchanged. The reusable checks passed 32/32 and the exact source is in the uncommitted working tree. No installation/upgrade/runtime or external-distribution acceptance follows. | Actual package and scope, [independent saved review](D:/Code/goby/.git/m6-116-notice-package-preparation-20260918-r01/actual-readback-62f41a7d9e30/saved-package-result-review.md), [local actual116 archive](D:/Code/goby/.git/m6-116-notice-package-preparation-20260918-r01/actual-artifacts-62f41a7d9e30/goby-linux-amd64-systemd-notices-internal-candidate.tar.gz), [component inventory](../development/systemd-package-component-inventory.md) | Preserve this accepted package and its exact 116 artifact/material association. Continue the separately required installation, upgrade, runtime and delivery slices, retaining the unresolved license and final distribution selection. Do not replay unchanged packaging checks or relabel the old 7fec archive. | Pending project-license decision and final distribution selection; this matrix makes no legal-completeness conclusion. New runtime claims need their own actual journey. | Internal package assembly/readback and bounded reusable checks are accepted within scope; source commit, licensing, installation/upgrade, external distribution and complete M6 remain pending |
| M7 / additional features / unselected profiles | Deferred. | [Milestone scope](delivery-and-verification.md#milestones-and-dependencies) | Wait for explicit feature selection, then define its contracts, permissions, migration and interoperability gate. | Explicit feature selection. | Outside the next internal candidate; remains deferred |

The independent [first-eleven preservation review](D:/Code/goby/.git/native-image-preservation-preparation-20260918-r01/actual-preservation-review.md)
and [d27/b7 addendum](D:/Code/goby/.git/native-image-preservation-preparation-20260918-r02/actual-preservation-review.md)
accept thirteen complete byte representations: **26 GiB raw**, **834378306 gzip
bytes**, and preservation-time allocations **27917488128 raw / 834404352 gzip**.
The subsequent [independent release review](D:/Code/goby/.git/native-image-single-release-preparation-20260918-r02/actual-thirteen-release-review.md)
accepts all thirteen separately decided single-file releases, each with fresh
same-process ownership checks and gzip/non-raw preservation. **38410/38410 read
FDs**, **3346/3346 pidfds** and **52 queries** closed; errors/unknowns are empty.
All complete gzip representations and non-raw evidence remain. The original
thirteen-release review excludes 4f. Its separate fourteenth complete backup
now exists at [actual-readback-529df3a6b18c](D:/Code/goby/.git/native-image-preservation-preparation-20260918-r03/actual-readback-529df3a6b18c):
publication 94db8a / archive 3242a8 / verify f859a4 / copy 11f91c all exited 0, preserving
the original 2 GiB SHA in a 65977188-byte gzip. Its subsequent
[separate 4f release](D:/Code/goby/.git/native-image-w4f-single-release-preparation-20260918-r01/actual-readback-a037d8e512cf/actual-release-review.md)
also completed: d0e592 / ba903e / 474aa3 exited 0, with 5346/5346 read FDs,
258/258 pidfds and four queries closed. The gzip and full non-raw tree remain,
including nine future-reference source dependencies and four evidence pins.
Both original forensic SQL snapshots were already saved. The 2048-entry
observer used the known 1056-row lower bound; its 1024-entry r01 was unexecuted.
All fourteen selected old raws are now individually released. Original
preservation/observer flags and
product outcomes are unchanged, including the a6 authentication failure and
separate successful saved-only read. Root closed only 5e's optional old-payload
follow-up; four SQL bodies remain unread and 5e remains failed. The historical 4f
post-close root-free sample **39962927104 bytes** does not grant a reservation
or new admission. Earlier samples are historical; do not add the fourteen
raw releases or OCI's backing deletion again. Actual OCI setup/build/closure
is recorded separately in its row.
The later actual OCI import post-operation sample is **39544225792 root-free
bytes / 5151657984 bytes MemAvailable**, still below the separate 6 GiB M2
memory gate. This remains a sample, not a reservation. Keep fresh admission gates;
neither byte recovery nor release recreates original inode/ctime or accepts a
broader product profile.

The M5 refinement preserves the complete M2-M6 obligations without turning vague
policy/provider/executor/configuration labels into an unlimited feature list.
Trace each proposed behavior to the original requirements and give it a bounded
acceptance journey. The [Tasks API](../api/tasks.md) distinguishes the accepted
normal-scan and media-refresh source/profile evidence from deployment status;
that documentation correction does not reopen either implementation or rewrite
historical receipts.

## Updating a row

Change a row only when its cited evidence or a concrete product decision changes.
Keep active-worker plans, runtime observations and entry-readiness updates in the
authoritative queue/status instead of duplicating them here.
Record the exact artifact/profile and distinguish implementation, original-client
acceptance, installation, recovery, capacity and distribution outcomes. A
document edit closes none of those gates. All verification remains on
`ssh test-env`; this matrix was authored by manual file reading without tests,
builds, validators, service operations or runtime probes.
