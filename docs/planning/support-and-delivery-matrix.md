# Support and delivery matrix

Recorded on 2026-09-15 from committed plans and saved evidence. This is a
delivery decision map, not new runtime verification. The
[current execution plan](current-execution-plan.md) controls execution order;
[delivery and verification](delivery-and-verification.md) retains the complete
M2-M6 requirements. M7 remains deferred.

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
| Programs successor: current delivery target | Binary and package identities not assigned; build has not run | [Focused verification](../development/live-tv-programs-focused-verification.json) independently accepts 14 top-level and 118 subtest passes. [Full regression](../development/live-tv-programs-full-interruption.json) is user-interrupted partial evidence, not a passing full suite. |
| R32: old recovery binary | `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`, schema 27 | [Actual source32 restoration and restart](../development/isolated-source32-recovery.json) passed. This proves old-binary recovery, not a Programs-successor restore or upgrade. |
| W: pinned original Web Client | Web Client distributed in official Emby `4.9.5.0`; package SHA256 `1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843` | [Original-client hosting record](../development/audited-original-client-hosting.json) establishes package/version provenance; the individual client closeouts establish each journey's outcome. Their public summaries do not restate the browser build. The saved Subtitles01 readback below supplies its browser version; other runs retain their own missing metadata and do not inherit older source12 values. |

**Artifact selection:** the [core resolution](../development/core-client-acceptance-resolution.md)
now targets the Programs successor. Existing A will switch directly once to its
verified final artifact; no intermediate original-E11 deployment is queued.
E11 remains the accepted build/G2 baseline, not the identity of changed product
source. Recovery and all three final journey entry contracts must complete before
A changes; pure-tool preparation may run in parallel with verification/build of
frozen Go source. Then make one A transition, complete three client journeys and
the audio reuse bridge, and enter G3. Selection is not transition admission.

The planned single worker will verify all 25 ordinary packages, build the ordinary
binary, then build one embedded amd64 systemd package. It has not run. Changes to
Python/JavaScript tools alone do not require another Go full run or build launcher.

The ordinary full run was interrupted for user review and safely closed after
10 of 25 packages: 341 passes, zero failures and zero skips. The identity
package's additional 109 raw passes are incomplete and excluded. No build ran;
full verification is false and no product assertion failure was observed.

## Next internal candidate gates

| Gate | Required result | Current evidence | Next concrete action | External prerequisite |
| --- | --- | --- | --- | --- |
| G0: artifact and inputs | One verified successor binary/package, source/build bridge, exact client/browser/media pins and reviewed retained-state inputs | Programs source is the target and [existing A is selected](../development/e11-candidate-transition-decision.md); no successor binary/package exists. A retains its explicit external administrator asset override. | Prepare recovery/journey entry contracts in parallel with frozen-Go-source verification/build; complete both before one direct A transition with the declared startup task-definition delta. | Episode/subtitle contracts and recovery decision remain incomplete; current candidate state and bounded transition evidence are required. |
| G1: supported core client | Complete declared movie, episode and subtitle journeys on the successor, plus the explicit reviewed audio reuse bridge or affected new audio evidence | A's historical audio passed; final successor client acceptance remains open. [Accepted baseline](current-execution-plan.md#accepted-baseline) | Prepare the contracts before transition; execute the three final journeys afterward using successive complete closeouts. Do not repeat completed diagnostic/reference work. | Pinned original client/media, current capacity and retained-state admission; v4 movie/v5 TV do not supply episode/subtitle contracts automatically. |
| G2: internal installation | Complete nonroot package installation, bootstrap/catalog journey, normal stop/start and final sealing | E11 internal amd64 installation is accepted through original runtime plus independently reviewed HTTP/archive evidence. [Composite acceptance](../development/internal-amd64-installation-acceptance.json) | Retain this historical G2 slice and failed sealer. Review successor applicability only for assertions invalidated by the actual change; do not automatically queue another installer run. | No remaining prerequisite for the original accepted slice; the successor's source/build identity remains a G0 requirement. |
| G3: promotion and recovery | Current-state preservation, applicable recovery evidence, bounded upgrade and post-upgrade workflow for the actual successor | R32 backup/recovery and the historical A rollback passed; successor applicability and main promotion remain open. [Recovery proofs](../development/isolated-source32-recovery.json), [upgrade contract](../development/audited-main-upgrade-plan.md) | Prepare the transition recovery decision now; after G1, bind the main workflow and refresh only prerequisites invalidated by changes. Old-binary return can disable the refresh definition and is not an implemented automatic rollback plan. | Current main state and capacity; G1 and applicable safety conditions must pass before promotion. |

G0's candidate choice and successor scope are selected; its binary/package,
entry contracts and transition checks remain open. The [saved Subtitles01 metadata](../development/core-response-identity-verification.json)
now confirms Chromium `153.0.8010.12` and Playwright `1.63.0` for that execution.
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
| W / 180-second MP3 through universal audio / A native candidate | Implemented; scoped original-client journey accepted on A, including physical delivery, seeks, stopped state and closure. Successor reuse bridge open; no audible-output claim. | [MP3 plan and outcome](../development/audited-mp3-client01-plan.md), [closeout](../development/audited-mp3-client01-closeout.json) | Reuse the established A/E11 source comparison and review only the Programs-successor delta; close the source/configuration/media/transition bridge or name an actually affected audio journey. | W browser pin and original fixture/input metadata. | G1 audio |
| W / 180-second FLAC through universal audio / A native candidate | Implemented; scoped original-client journey accepted on A. Successor reuse bridge open; no independent output-codec or audible-output claim. | [FLAC plan and outcome](../development/audited-flac-client01-plan.md), [closeout](../development/audited-flac-client01-closeout.json) | Apply the same explicit successor reuse bridge to FLAC; preserve the accepted A result without relabeling it as a successor execution. | W browser pin and original fixture/input metadata. | G1 audio |
| W / recorded movie, direct video and cross-login resume / historical movie candidate | Implemented; formal acceptance open. Movie05 has four unattributed errors; Movie06 failed readiness before playback. Historical binary/state remains unchanged. | [Movie05 closeout](../development/audited-core-movie05-owned-state-closeout.json), [Movie06 plan review](../development/audited-movie06-plan-review.md), [current movie checkpoint](../development/current-status.md#movie-checkpoints) | Prepare the successor entry using the verified v4 state contract and current component/runtime bindings; after transition, run the declared final movie/resume journey. | Latest complete closed state, W and exact fixture pins; original failures are not waived. | G1 movie/resume |
| W / TV browse and recorded Episode2-1 video, pause/seek/stop / A | Parent metadata and playback implemented; historical episode acceptance remains open. The consumed no-playback TV diagnostic does not replace it. | [Episode01 contract](../development/audited-episode-client01-plan.md), [review](../development/audited-episode-client01-review.md) | Complete the missing retained-episode entry/closure contract before A changes, then run the final successor episode journey. Assess browse only through its complete declared checks and closeout. | Episode contract not complete; W and retained episode state must be bound without actor reset. | G1 episode/browse |
| W / recorded movie plus external SRT and VTT / A | Implemented; historical cues/seek/Off completed, but two undefined errors and physical293 interpretation remain open. | [Subtitle result and response identity](../development/audited-subtitles-client01-review.md), [closeout](../development/audited-subtitles-client01-closeout.json) | Complete the missing retained-subtitle entry/closure contract before transition; then run the final successor SRT/VTT journey with current response-identity and timing gates. | Subtitle contract not complete; latest state and exact client/media pins are required. | G1 subtitles |

## Remaining delivery slices

These rows retain the complete milestone scope. A `Later profile` designation
orders work for the next internal candidate; it is not removal from M2-M6.

| Slice: milestone / media path / deployment profile | Recorded status | Evidence | Next concrete action | External prerequisite | Relation to next internal candidate |
| --- | --- | --- | --- | --- | --- |
| M2/M6 / catalog, ACL and normal restart / Linux amd64 nonroot systemd | Product catalog checks accepted in their scopes; B root restart and E11 r04 nonroot runtime passed. The internal amd64 installation gate is now accepted with the archived final-state supplement. | [B restart](../development/native-catalog-restart-verification.json), [E11 r04](../development/systemd-installation-fourth-attempt.json) | Reuse the accepted G2 result. Keep PG restart, host durability and broader storage profiles separate. | Saved r04 evidence; any new run needs fresh ownership/capacity. | G2; part of the proposed internal package slice |
| M2 / scan plus concurrent catalog HTTP / E11, 1,000 tiny files | SQL 10,000-leaf and tiny-file scan baselines accepted; native measurement unexecuted. No representative throughput or SLO accepted. | [SQL baseline](../development/catalog-capacity-isolation-verification.json), [real-file baseline](../development/catalog-real-media-capacity-verification.json), [saved preparation](../development/session-handoff-20260915-native-capacity.md) | Record the product decision this measurement will inform, minimum useful output and remaining preparation time limit; decide continue or park before completing the missing controller/metrics work. | Current remote resources and safe closure; overlap may be incomplete. | Independent capacity evidence; does not close G1/G2/G3 or full M2 |
| M2 / inaccessible or blocked storage and host restart / owned Linux filesystem profile | Scan safety implemented with partial evidence; paused full-scan mount stages and host durability remain unproved. | [Mount preparation](../development/m2-fullscan-mount-preparation.json), [storage plan](../development/storage-root-bindings-plan.md), [remaining obligations](delivery-and-verification.md#required-scenario-matrix) | Define one fault profile and its exact expected catalog/termination result using the saved failure review; list its required fixture and affected protected resources before deciding execution. Keep the paused mount isolated. | Suitable owned filesystem/fault mechanism; a host reboot needs an explicit affected-service preservation plan. | Later operational profile; required for the matching M2/durability claims |
| M3/M4 / remux, progressive/HLS software conversion and track changes / Linux amd64 CPU | Planner/pipeline paths have scoped implementation evidence; broad original-client/media matrix remains open. Nonzero video-copy seeks are explicitly unsupported. | [HLS](../development/hls-playback.md), [progressive video](../development/progressive-video-playback.md), [audio profiles](../development/audio-profile-playback.md) | After G0, choose one uncovered input/output pair from the existing contracts and bind negotiation, seek, cancellation and cleanup checks; use software H.264/AAC as the documented baseline where enabled. | Pinned FFmpeg and permitted fixture; exact selected client capabilities. | Later media profile; M4 needs M3 session/source identities, not every unrelated feature |
| M5 / administrator tasks, users, metadata, settings and recovery / embedded dashboard | Named historical increments and fixed media refresh accepted; complete M5 still open. Full reference-object parity is not claimed. | [M5 refresh result](../development/task-media-refresh-verification.json), [final ordinary regression](../development/m5-final-regression-verification.json), [current status](../development/current-status.md) | Map the remaining M5 requirement list to named missing administrator behavior, then select one API/UI journey with success, failure and permission outcomes. Reuse accepted unchanged increments. | Actual missing behavior must be selected from the existing scope; no consumer player. | Accepted refresh/source evidence supports G0; remaining M5 slices follow independently |
| M4/M6 / hardware decode, encode and combined pipeline / each VAAPI, QSV or NVIDIA profile | Configuration/planning exists; no accepted actual GPU profile is claimed by this matrix. | [Hardware policy](../development/toolchain.md#hardware-decode-and-encode-acceptance), [required matrix](delivery-and-verification.md#required-scenario-matrix) | First inventory an available device/driver/FFmpeg/service-access profile; then define separate decode, encode and combined filter/subtitle/cancel cases for that exact profile. | Required GPU and driver availability are unknown here; missing hardware blocks that profile. | Later hardware profile; required for actual hardware support claims |
| M6 / embedded package / native Linux arm64 | Cross-build evidence accepted; native install/runtime and client behavior open. An amd64 pass is not native arm64 evidence. | [Embedded build](../development/embedded-administrator-verification.json), [package builds](../development/systemd-package-build-verification.json) | Select the retained arm64 artifact and define the minimum nonroot install/start/catalog/stop journey before remote execution. | Native arm64 environment and compatible PG/FFmpeg; availability not established here. | Later architecture profile; required for arm64 support |
| M6 / packaged service / OCI | Complete container packaging/runtime profile remains open. | [Packaging architecture](../architecture/linux-go-react.md#linux-packaging-and-operations), [current status](../development/current-status.md#remaining-release-work) | Choose an explicit image/runtime profile and bind database, media, writable state, UID and optional discovery/GPU configuration to an install/upgrade contract. | Target container runtime/image provenance and any selected hardware. | Later deployment profile; required for OCI support |
| M3/M6 / NextUp and automatic LibraryChanged refresh / recorded original client | Implemented portions and research exist; positive selector/order and automatic client-refresh evidence remain open. | [NextUp comparison contract](../development/nextup-goby-comparison-contract.md), [gate boundaries](current-execution-plan.md#gate-boundaries) | Keep parked until a positive client query/reference state or an explicit product decision identifies a new bounded question. | New discriminating public/client evidence; no egress change or vendor-source access implied. | Feature gates only; do not block unrelated core/internal work |
| M6 / actual package contents and external distribution / each selected package | Initial notices, emitted chunk graph and bounded glyph correspondence recorded; project license, remaining provenance/contribution and final legal payload open. | [Notices](../../THIRD_PARTY_NOTICES.md), [component inventory](../development/systemd-package-component-inventory.md), [JavaScript attribution](../development/systemd-package-javascript-attribution.md) | Resolve the documented remaining package-contribution and glyph-source inputs, then assemble the payload for the actual selected package. Keep the already pending project-license decision pending. | User's pending license choice and required upstream information; this matrix makes no legal conclusion. | External distribution gate; no repeated license question or external release implied |
| M7 / additional features / unselected profiles | Deferred. | [Milestone scope](delivery-and-verification.md#milestones-and-dependencies) | Wait for explicit feature selection, then define its contracts, permissions, migration and interoperability gate. | Explicit feature selection. | Outside the next internal candidate; remains deferred |

## Updating a row

Change a row only when its cited evidence or a concrete product decision changes.
Record the exact artifact/profile and distinguish implementation, original-client
acceptance, installation, recovery, capacity and distribution outcomes. A
document edit closes none of those gates. All verification remains on
`ssh test-env`; this matrix was authored by manual file reading without tests,
builds, validators, service operations or runtime probes.
