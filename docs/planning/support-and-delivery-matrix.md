# Support and delivery matrix

Recorded on 2026-09-15 from committed plans and saved evidence. This is a
delivery decision map, not a worker or runtime-status ledger. The
[immediate queue](current-execution-plan.md#immediate-queue) controls execution
order and admission; [current status](../development/current-status.md) records
active work, runtime state and actual artifact bindings. The
[delivery and verification](delivery-and-verification.md) retains the complete
M2-M6 requirements. M7 remains deferred.

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
| A: audited client candidate | `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`, schema 28 | [MP3](../development/audited-mp3-client01-closeout.json), [FLAC](../development/audited-flac-client01-closeout.json), [Episode01](../development/audited-episode-client01-closeout.json) and [Subtitles01](../development/audited-subtitles-client01-closeout.json) bind this binary. The selected isolated rollback also used A. Movie checkpoints retain their own earlier source records. |
| B: admitted embedded candidate | `59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312` | [Fresh provision, inspection, seed and admission](../development/fresh-embedded-candidate-checkpoint.json) passed; `clientAcceptance=false`. Its [native catalog restart](../development/native-catalog-restart-verification.json) used a separate root profile. B is neither A nor E11. |
| E11: retained build/G2 baseline | `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`, 30,691,123 bytes; amd64 package `2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a` | [Package builds](../development/systemd-package-build-verification.json), [source bridge](../development/systemd-package-source-checkpoint.json) and [composite G2 acceptance](../development/internal-amd64-installation-acceptance.json) retain their exact scopes. These bytes do not contain the new Programs route. |
| Programs successor: selected delivery direction | Actual source, binary and package bindings are recorded in [current status](../development/current-status.md#current-gates) | [Focused verification](../development/live-tv-programs-focused-verification.json) independently accepts 14 top-level and 118 subtest passes on its frozen source. Final regression, artifact and runtime admission require separate evidence. |
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
| W / recorded movie, direct video and cross-login resume / historical movie candidate | Movie05 records four unattributed errors; Movie06 failed readiness before playback. Their historical source and failure records retain their scopes. | [Movie05 closeout](../development/audited-core-movie05-owned-state-closeout.json), [Movie06 plan review](../development/audited-movie06-plan-review.md), [movie checkpoint](../development/current-status.md#movie-checkpoints) | Require a reviewed retained-state contract and actual component/runtime bindings for the declared final movie/resume journey. | Admitted complete closed state, W and exact fixture pins; original failures are not waived. | G1 movie/resume |
| W / TV browse and recorded Episode2-1 video, pause/seek/stop / A | Parent metadata and playback implemented; Episode01 retains its formal rejection. The consumed no-playback TV diagnostic does not replace it. | [Episode01 contract](../development/audited-episode-client01-plan.md), [review](../development/audited-episode-client01-review.md) | Require the typed retained-episode entry/closure contract and assess browse/playback through all declared checks and closeout. | W and retained episode state must be bound without actor reset; use the authoritative queue for entry readiness. | G1 episode/browse |
| W / recorded movie plus external SRT and VTT / A | Historical cues/seek/Off completed, but the saved result does not explain two undefined errors or resolve physical293. | [Subtitle result and response identity](../development/audited-subtitles-client01-review.md), [closeout](../development/audited-subtitles-client01-closeout.json) | Require the typed retained-subtitle entry/closure contract and response-identity/timing gates for the declared SRT/VTT journey. | Admitted latest state and exact client/media pins; use the authoritative queue for entry readiness. | G1 subtitles |

## Remaining delivery slices

These rows retain the complete milestone scope. A `Later profile` designation
keeps an independent acceptance scope; ordering belongs in the immediate queue
and no requirement is removed from M2-M6.

| Slice: milestone / media path / deployment profile | Recorded status | Evidence | Next concrete action | External prerequisite | Relation to next internal candidate |
| --- | --- | --- | --- | --- | --- |
| M2/M6 / catalog, ACL and normal restart / Linux amd64 nonroot systemd | Product catalog checks accepted in their scopes; B root restart and E11 r04 nonroot runtime passed. The internal amd64 installation gate is now accepted with the archived final-state supplement. | [B restart](../development/native-catalog-restart-verification.json), [E11 r04](../development/systemd-installation-fourth-attempt.json) | Reuse the accepted G2 result. Keep PG restart, host durability and broader storage profiles separate. | Saved r04 evidence; any new run needs fresh ownership/capacity. | G2; part of the proposed internal package slice |
| M2 / scan plus concurrent catalog HTTP / E11, 1,000 tiny files | SQL 10,000-leaf and tiny-file scan baselines accepted; native measurement unexecuted. No representative throughput or SLO accepted. | [SQL baseline](../development/catalog-capacity-isolation-verification.json), [real-file baseline](../development/catalog-real-media-capacity-verification.json), [saved preparation](../development/session-handoff-20260915-native-capacity.md) | Record the product decision this measurement will inform, minimum useful output and remaining preparation time limit; decide continue or park before completing the missing controller/metrics work. | Current remote resources and safe closure; overlap may be incomplete. | Independent capacity evidence; does not close G1/G2/G3 or full M2 |
| M2 / inaccessible or blocked storage and host restart / owned Linux filesystem profile | Scan safety implemented with partial evidence; paused full-scan mount stages and host durability remain unproved. | [Mount preparation](../development/m2-fullscan-mount-preparation.json), [storage plan](../development/storage-root-bindings-plan.md), [remaining obligations](delivery-and-verification.md#required-scenario-matrix) | Define one fault profile and its exact expected catalog/termination result using the saved failure review; list its required fixture and affected protected resources before deciding execution. Keep the paused mount isolated. | Suitable owned filesystem/fault mechanism; a host reboot needs an explicit affected-service preservation plan. | Later operational profile; required for the matching M2/durability claims |
| M3/M4 / remux, progressive/HLS software conversion and track changes / Linux amd64 CPU | Planner/pipeline paths have scoped implementation evidence; broad original-client/media matrix remains open. Nonzero video-copy seeks are explicitly unsupported. | [HLS](../development/hls-playback.md), [progressive video](../development/progressive-video-playback.md), [audio profiles](../development/audio-profile-playback.md) | After G0, choose one uncovered input/output pair from the existing contracts and bind negotiation, seek, cancellation and cleanup checks; use software H.264/AAC as the documented baseline where enabled. | Pinned FFmpeg and permitted fixture; exact selected client capabilities. | Later media profile; M4 needs M3 session/source identities, not every unrelated feature |
| M5 / administrator completeness / native dashboard | Located implementation gaps: native user deletion and an administrator entry for actual decode/encode diagnostic results. Metadata/user editing, sessions, devices, keys, tasks, settings, audit/log UI and PostgreSQL backup/recovery retain accepted evidence in their named source/profile scopes. | [M5 requirements](delivery-and-verification.md#milestones-and-dependencies), [accepted scopes](../development/progress.md), [user-management boundary](../api/admin-users.md#remaining-administrator-scope), [configuration/diagnostic boundary](../development/settings.md) | Plan bounded independent follow-up increments for the located gaps. For existing features, review successor applicability and bridge only actual source/profile/state differences; do not reimplement or automatically rerun accepted journeys. | Map policy/provider/additional-executor/configuration requirements to concrete administrator behavior before implementation. Missing hardware blocks only the corresponding hardware diagnostic profile, not ordinary UI. | Later independent M5 work; do not insert these Go/UI changes into the frozen Programs artifact or alter its current delivery queue |
| M4/M6 / hardware decode, encode and combined pipeline / each VAAPI, QSV or NVIDIA profile | Configuration/planning exists; no accepted actual GPU profile is claimed by this matrix. | [Hardware policy](../development/toolchain.md#hardware-decode-and-encode-acceptance), [required matrix](delivery-and-verification.md#required-scenario-matrix) | First inventory an available device/driver/FFmpeg/service-access profile; then define separate decode, encode and combined filter/subtitle/cancel cases for that exact profile. | Required GPU and driver availability are unknown here; missing hardware blocks that profile. | Later hardware profile; required for actual hardware support claims |
| M6 / embedded package / native Linux arm64 | Cross-build evidence accepted; native install/runtime and client behavior open. An amd64 pass is not native arm64 evidence. | [Embedded build](../development/embedded-administrator-verification.json), [package builds](../development/systemd-package-build-verification.json) | Select the retained arm64 artifact and define the minimum nonroot install/start/catalog/stop journey before remote execution. | Native arm64 environment and compatible PG/FFmpeg; availability not established here. | Later architecture profile; required for arm64 support |
| M6 / packaged service / OCI | Complete container packaging/runtime profile remains open. | [Packaging architecture](../architecture/linux-go-react.md#linux-packaging-and-operations), [current status](../development/current-status.md#remaining-release-work) | Choose an explicit image/runtime profile and bind database, media, writable state, UID and optional discovery/GPU configuration to an install/upgrade contract. | Target container runtime/image provenance and any selected hardware. | Later deployment profile; required for OCI support |
| M3/M6 / NextUp and automatic LibraryChanged refresh / recorded original client | Implemented portions and research exist; positive selector/order and automatic client-refresh evidence remain open. | [NextUp comparison contract](../development/nextup-goby-comparison-contract.md), [gate boundaries](current-execution-plan.md#gate-boundaries) | Keep parked until a positive client query/reference state or an explicit product decision identifies a new bounded question. | New discriminating public/client evidence; no egress change or vendor-source access implied. | Feature gates only; do not block unrelated core/internal work |
| M6 / actual package contents and external distribution / each selected package | Initial notices, emitted chunk graph and bounded glyph correspondence recorded; project license, remaining provenance/contribution and final legal payload open. | [Notices](../../THIRD_PARTY_NOTICES.md), [component inventory](../development/systemd-package-component-inventory.md), [JavaScript attribution](../development/systemd-package-javascript-attribution.md) | Resolve the documented remaining package-contribution and glyph-source inputs, then assemble the payload for the actual selected package. Keep the already pending project-license decision pending. | User's pending license choice and required upstream information; this matrix makes no legal conclusion. | External distribution gate; no repeated license question or external release implied |
| M7 / additional features / unselected profiles | Deferred. | [Milestone scope](delivery-and-verification.md#milestones-and-dependencies) | Wait for explicit feature selection, then define its contracts, permissions, migration and interoperability gate. | Explicit feature selection. | Outside the next internal candidate; remains deferred |

The M5 refinement preserves the complete M2-M6 obligations without turning vague
policy/provider/executor/configuration labels into an unlimited feature list.
Trace each proposed behavior to the original requirements and give it a bounded
acceptance journey. Record a later documentation correction for the Tasks API's
stale media-refresh-pending introduction; this does not reopen its accepted
implementation or authorize rewriting historical receipts.

## Updating a row

Change a row only when its cited evidence or a concrete product decision changes.
Keep active-worker plans, runtime observations and entry-readiness updates in the
authoritative queue/status instead of duplicating them here.
Record the exact artifact/profile and distinguish implementation, original-client
acceptance, installation, recovery, capacity and distribution outcomes. A
document edit closes none of those gates. All verification remains on
`ssh test-env`; this matrix was authored by manual file reading without tests,
builds, validators, service operations or runtime probes.
