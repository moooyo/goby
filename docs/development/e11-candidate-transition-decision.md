# E11 candidate transition decision

Status: **candidate selected; transition and execution are not admitted**.
Recorded on 2026-09-15 from the retained source comparison and A/B seed/admission
summaries. This document makes the delivery choice; it does not observe current
processes, authorize a deployment, or establish E11 client acceptance.

## Decision and evidence

Use existing **candidate A**, rooted at
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`, for the final E11
transition. Its recorded audited binary is
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`.
Preserve candidate B and its independent state; do not provision a third
candidate, move actors between databases, or reset A's consumed actors.

The selected destination binary remains E11,
`7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`
(30,691,123 bytes), from package SHA256
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`.
The [package/source bridge](systemd-package-source-checkpoint.json) and accepted
internal installation result keep their original scopes.

| Recorded fact | Delivery implication |
| --- | --- |
| A's [original seed](audited-candidate-seed-closeout.json) records eight users, three libraries/roots, three completed scans, thirteen stored items and fourteen media files. It belongs to an earlier source in A's preserved lineage, not directly to E11. | Keep the existing actors, media and identities; no new seed or scan is needed merely to select E11. |
| A's [affected admission05](tv-parent-affected-admission-closeout.json) binds the audited `b0d6769...` binary, schema 28, seed/runtime lineage, retained recovery stage and nonroot UID 995. | A already has a reviewed original-client fixture lineage and a nonroot application profile. Historical PIDs are not current entry authority. |
| A carries the [accepted MP3](audited-mp3-client01-closeout.json)/[FLAC](audited-flac-client01-closeout.json) results and the recorded movie, episode and subtitle history tracked in the [support matrix](../planning/support-and-delivery-matrix.md). | The final transition can retain the same media/actor identities and directly preserve the evidence the current retained-state contracts address. |
| B's [seed/admission summary](fresh-embedded-candidate-checkpoint.json) records eight users, three libraries/scans, fourteen independent files and successful native admission, but `clientAcceptance=false` and `browserGatewayEstablished=false`. | B is valid retained work, but switching the final-client path to B would require a separate client origin, actor mapping and input-lineage integration that its summary does not establish. |
| The [actual source comparison](core-artifact-source-comparison.md) finds A-to-E11 has eleven changed and four added production Go files; B-to-E11 has eight changed files and identical embedded assets. | B has the smaller source delta. A has the stronger existing original-client/state integration. The asset-selection differences in A-to-E11 must remain explicit. |

**Engineering judgment:** A minimizes integration uncertainty and repeated
client preparation using the presently established evidence. The choice is not
based on treating the larger A history as disposable or on claiming B is unsafe.
It retains one catalog/actor lineage for diagnosis, transition and final client
acceptance while preserving B as independent evidence. A fresh entry that
reveals incompatible state stops the transition for review; it does not
silently switch to B or create another fixture.

A's relevant historical binding pins are admission05
`b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535`,
runtime epoch
`76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac`,
and seed-runtime binding
`94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43`.
They identify lineage. They are not permission to reuse an old process identity
or a consumed input.

## Transition state contract

Freeze the actual latest complete closed state after the bounded TV branch has
closed, whether or not it obtained a useful Response binding. Preserve that
branch's result independently. Do not make complete explanation of every old
pageerror an indefinite prerequisite for designing this transition.

The following is the bounded state allowance to turn into a reviewed execution
input. Values and current ownership must be observed before dispatch; this
document does not assert that those observations have occurred.

| Area | Allowed change and required preservation |
| --- | --- |
| Executable and process | Select the exact E11 bytes through one reviewed A transition, retaining the old executable and a concrete recovery/closure route. New application PID/start/invocation/listener and database lease identities must be recorded. Main, B and their PostgreSQL instances are outside this change. |
| Startup task registration | If absent at entry, permit exactly one new `task_definitions` row keyed `library.refresh_media`, with the compiled E11 definition/default fields. If already present, require it to match the reviewed expected state. Preserve the existing `library.scan` identity, administrator settings, rules and history. This allowance does not admit a task run, scan, refresh or trigger creation. |
| Existing database state | Preserve all existing logical rows/fields across all 35 tables and the five sequences, except the precisely admitted task-definition delta and any separately enumerated admission authentication effects. The new definition's identifier is not authority for a sequence change. No schema migration is expected: all 28 migration files and six backup catalogs are identical in the compared sources. |
| Playback and users | Preserve every old movie/episode/subtitle/audio session, play, UserData row and reference. Keep all closed credentials revoked and both foreign audio references exact. Do not expire, delete or reset history merely to make entry fresh. Each later client journey gets its own explicit Prepared-state exception and declared new state effects. |
| Administrator assets/configuration | The [saved configuration provenance](#administrator-asset-selection-from-saved-provenance) establishes A's nonempty `GOBY_WEB_DIR` as its existing `install/admin` directory. With that configuration preserved, E11 selects the explicit external override. Keep the directory and its accepted asset inventory; do not remove the override or claim embedded serving merely from the binary tag. No environment/configuration rewrite is authorized here. |
| Native state and logs | Record the exact startup/reconciliation and shutdown effects on owned lease, lifecycle and diagnostic state. Do not broaden a logical-row preservation result into physical database integrity or host durability. Any additional row, sequence, task, retention or file change needs an explained, reviewed allowance before it can pass. |
| Admission requests | Health/authentication or affected task/catalog checks, if needed, must have a fixed request list and declared credential/audit/device effects in the transition input. They are separate from the startup-only allowance; old admission counts and inputs are not replayed. |

Entry must establish that no active/recoverable task or eligible trigger would
silently broaden this startup. Do not disable schedules or cancel unrelated
work to force the intended baseline. If normal E11 startup would make another
state change, resolve that concrete mismatch before execution.

## Administrator asset selection from saved provenance

The configured value is established as:

`GOBY_WEB_DIR=/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/install/admin`

This conclusion uses the pinned generator, its actual input/intent and the
recorded append-only configuration lineage. No current or historical
`runtime.env`, `/proc/.../environ`, master/key content or reference-server source
was opened or decoded for this follow-up. The current runtime digest below is
the root task's independently verified hash-only observation, not a second
environment read by this document task.

Let `R` be `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14` and `A` be
the selected candidate root stated above. The following source/nonsecret-record
pins were read; raw configuration paths referenced inside them were not followed.

| Source or record | SHA256 | Relevant fact |
| --- | --- | --- |
| `R/provision-tool-verification-01/prepare-audited-candidate.py` | `9b83f402ce539155dc6d1eafc99786c87b593ca47c7834127aeaacd46335ab6a` | Lines 214-215 derive `self.install` from the input run ID; line 430 sets `GOBY_WEB_DIR` to `self.install / "admin"`; line 438 writes the generated environment. This is the historical generator, not the later embedded-provision variant. |
| `A/private/provision-intent.json` | `2d6313b5e8cc4a9482d27bda6697d29ff2273a2fe3af37892abec70e6754f037` | Binds that executed source, the input below and the exact A scope. |
| `R/candidate-provision-input.json` | `f18038529b5a4ada9c2444f60c399d1a4d0aeca8e7a6c9c3a9bb169a77620dad` | Version-1 input, run ID `20260913T073217Z-ef77f9ffcf0b`; no generated credential or environment body was needed. |
| `R/candidate-runtime-inspection-01/report.json` | `931ffefb5983d98eef144b3b963950c7c1a74223d57800813b13245b5b6444ce` | Its recorded input hashes bind the provision input, original manifest and initial runtime digest `7a38009e...`; it records effective-environment agreement at that historical inspection. |
| `R/candidate-cancellation-transition-01/private/runtime-epoch.json` | `7bcdbc529fd1ba3f6a62f66585e6788cc9efa1aac22a4accc8d339d69ccf6ae2` | The first binary transition retains the same initial runtime digest. |
| `R/backup-limits-tool-verification-01/revision-input-01.json` | `d1f09c5dd52974eb7e46e221ce916204df57e0f34a5a221c1f1cc206d3b06b14` | Binds the preceding epoch and exactly two backup-limit additions. |
| `R/backup-limits-tool-verification-01/audited-candidate-runtime.py` | `1650d6267ab78a07f8c5b77130ad009eeb7212a0bb16251322936792a58dbe66` | Lines 346-354 require the result to equal the original bytes plus only the two fixed backup-limit lines. No dashboard key can be replaced by that operation. |
| `R/backup-limits-tool-verification-01/revise-audited-candidate-backup-limits.py` | `960eb8d7ff06912ad5d6a59f9814d75b72efa31b80bb7529ec958e19665cca61` | Records the before/after digests, fixed additions and preservation in the published configuration epoch. It was read as source, never executed here. |
| `R/candidate-backup-limits-revision-01/private/runtime-epoch.json` | `72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e` | Records the exact append from the initial digest to `877ce946...`, with all other fixed files preserved. |
| `R/candidate-tv-parent-transition-01/private/runtime-epoch.json` | `76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac` | Its binary successor still selects the same `877ce946...` runtime file; the [transition closeout](tv-parent-candidate-transition-closeout.json) records configuration preservation. |

The full initial runtime digest was
`7a38009eac18f6e9e4adcfd39182d3d58575359be6ca5906456aaa54aee07644`.
The [accepted configuration revision](audited-candidate-backup-limits-revision.json)
appended only `GOBY_BACKUP_MAX_OBJECT_BYTES=67108864` and
`GOBY_BACKUP_MAX_TOTAL_BYTES=268435456`. Its full resulting digest is
`877ce946814fef63a240171b7a60c4ad6b4505bf2051be815266ed9744627d3d`,
matching the current hash-only observation supplied for this decision.
The nonempty dashboard assignment therefore remains part of those unchanged
configuration bytes.

For the A-to-E11 input, reuse this conclusion only while the exact runtime
digest, server-unit configuration/hash and expected environment-file selection
remain bound to their reviewed records, and the retained `install/admin` asset
inventory passes its permitted hash/metadata checks. Do not read an environment
body merely to repeat the deduction. An unexpected hash, additional override or
unit-selection change stops reuse and needs a concrete review.

The [E11 source comparison](core-artifact-source-comparison.md) shows that a
nonempty explicit `GOBY_WEB_DIR` selects the external directory even in an
embedded build. Thus this transition retains A's external administrator asset
mode unless a separate reviewed configuration change is deliberately selected.
The bundled E11 assets and the accepted G2 embedded-installation evidence remain
valid in their own scopes; this follow-up does not claim a new embedded-serving
observation or authorize removing A's override.

## Audio evidence reuse and required final client work

Retain A's MP3 and FLAC journeys as accepted historical regression evidence for
their exact client/media/source profiles. The source comparison records unchanged
audio, authentication, playback, media and transcode implementation files;
this is a concrete basis for avoiding an automatic repeat of both consumed
audio inputs.

The intended reuse bridge still requires a reviewed successful A-to-E11
transition, preserved relevant configuration/media/actor state, and matching
client/profile inputs. Record the resulting audio support as **reused A journey
evidence with an E11 transition/source bridge**, never as an E11 audio execution.
Until that bridge closes, the E11 audio row remains open. A changed input or
discovered affected behavior requires only the corresponding focused new audio
journey; source equality alone does not grant the final support claim.

These three final acceptance rows require new journeys on the admitted E11
artifact and current A state:

| Final row | Required evidence |
| --- | --- |
| Movie | Original-client login/browse, advancing decoded video, pause, forward/backward seeks, stop and a second login with nonzero resume; physical media delivery and complete owned state/credential closure. Bind each partial response to the corrected response-identity rules. |
| Episode | Full declared TV browse through the exact Episode 2-1 identity, advancing decoded playback, pause/seeks/resume/stop/logout, and matching durable state. A no-playback TV diagnostic cannot replace this row. |
| External subtitles | SRT and VTT selection, visible opening/seek cues, return to Off, stop/logout, actual authorized subtitle/media delivery, and complete timing/cancellation interpretation and owned-state closure. |

Use each actor's then-current retained-state input and appropriate typed
contract. The movie v4 and TV v5 contracts are not automatically episode or
subtitle contracts. No new journey may inherit a zero-history assumption.
Recover the exact original-client build, browser/Playwright pins and media
fixture metadata for the selected journey; do not infer them from another run.

The unchanged pageerror, authorization, media/state and cleanup gates still
apply. Old failures stay failed or unresolved in their historical records;
successful E11 journeys establish only the new declared profile. If a product
correction is required, bind its successor artifact and relevant verification
before accepting changed-source results.

## Next concrete handoff

Produce one A-to-E11 transition input from the selected A root, exact artifact,
latest full state, permitted configuration facts, explicit task-definition delta,
current process/lease/protection facts and bounded recovery/cleanup plan. No
ready input is produced by this document. Complete that admission before the
three final journeys, and require core acceptance plus the separate G3 safety
prerequisites before main promotion. The already accepted G2 installation slice
does not need another installer run for this selection.

The initial candidate choice used local document/source-summary reading. The
asset-selection follow-up additionally read only pinned owned source and
nonsecret saved records over SSH. No test, build, application, browser, SQL,
HTTP, service action, configuration edit or deployment ran.
