# Current execution plan

Reviewed on 2026-09-13 after TV browse01. Status: **audited product verified and
prior candidate admitted; core client acceptance open; client execution paused;
TV parent metadata verified and installed; affected admission pending**. This is the active work queue.
[Current status](../development/current-status.md) records accepted facts;
[delivery and verification](delivery-and-verification.md) retains the complete
M2-M6 obligations. Historical plans and consumed run inputs are evidence, not
alternative instructions to execute.

The cancellation fix passed the [full remote verification](../development/restore-cancellation-full-verification.json):
2,264 tests across 25 packages with race instrumentation and a Linux build,
without failures or skips. Its selected candidate and configuration passed
[live admission04](../development/audited-candidate-live-admission-closeout.json).
Reuse these results while the product bytes remain unchanged.

Movie05 completed the observed movie UI and two counted Stopped play chains,
but its browser result failed. Its [owned state](../development/audited-core-movie05-owned-state-closeout.json)
and saved physical/durable evidence have since been reconciled; four page-error
causes remain unknown. The [v3 integration](../development/audited-client-v3-verification.json)
passed 71 new remote checks and a read-only stdout capture. Its one permitted
movie06 run failed before playback because the test helper required a unique
title while the old Home view was still visible after the item URL changed.
The [movie06 closeout](../development/audited-core-movie06-owned-state-closeout.json)
confirms fourteen revoked sessions, both workers closed, exact userdata and
only the explained session/device/audit/preparation changes. It does not pass
client acceptance or resolve the old page errors.

The [movie06 plan review](../development/audited-movie06-plan-review.md) replaces
the earlier immediate next actions. The transition-wait correction now passes
[19 remote lifecycle tests](../development/audited-movie06-readiness-verification.json),
including a reproduction against the unchanged source. Keep movie execution
paused: its current v3 baseline is bound to movie05, and any later movie run
needs an explicitly reviewed closed-state input without per-attempt code
constants, saved-state replay and a reachable page-error observation.

TV browse01 has now consumed its separate decision. It completed the visible
cross-season navigation, but a derived `SeriesId` assertion failed and one page
error remains unclassified. Its [owned state is closed](../development/audited-core-tv-browse01-owned-state-closeout.json):
all fifteen sessions are revoked, both workers closed, and the movie history
preserved. The shared adapter correction passed
[62 remote checks](../development/audited-tv-browse01-adapter-verification.json),
including saved-response replay. A separate retained reference response shows
a real TV parent-metadata difference in the product; fixing the checker alone
does not resolve it.

Follow the [TV evidence review](../development/audited-tv-browse01-evidence-review.md).
The evidenced Series/Season identity and name projection is implemented through
existing authorized queries. Its 21 effective focused tests and the
[full verification](../development/tv-parent-metadata-full-verification.json)
passed: 2,270 tests/25 packages with race instrumentation and a Linux build,
zero failures/skips, reconciled source/artifacts and complete isolated cleanup.
The corrected HTTP fixture and its original failed run remain recorded.
The [current-epoch candidate successor](../development/tv-parent-candidate-transition-plan.md)
is installed using the existing transition tool, with exact retained state and
historical hosting provenance. Tool verification passed 103 checks; the one
stop/replacement/start and independent closeout passed. Complete a bounded
affected admission for TV projections and access before resuming clients.
Reuse admission04 for unchanged contracts without repeating backup/restore.
Do not expand this into Live TV or reopen global NextUp research on an error
timestamp alone.

MP3, FLAC, episode playback and subtitles remain independent later core
increments. They do not depend functionally on movie passing, but they do use
the shared adapter. Serialize writes and close each owned state before the
next run. Every increment freezes relevant verified source, fresh actor/state
facts, expected outcome and existing request/time/cleanup bounds. Movie and TV
actors are now consumed; future reruns need reviewed retained-state admission,
not new actor identities or another null-baseline input. Success closes only
that scenario's gate.

The [main upgrade contract](../development/audited-main-upgrade-plan.md) remains
a draft. It needs core acceptance and distinct forward-restoration and actual
old-binary restoration proofs from one fresh recovery point. Neither main nor
the older source55 service has been upgraded or restarted by this increment.

## Goal retained

Deliver an independent Linux media server using Go, PostgreSQL and FFmpeg, with
an administrator-only React/MUI dashboard and unmodified compatible clients.
Keep the complete M2-M6 scope and its evidence requirements. A scoped internal
deployment or direct-play checkpoint is not completion of the planned server.
M7 stays deferred. This review neither adds a consumer player nor removes
hardware, operational, broader media or compatibility obligations.

## Corrections retained from the initial review

| Finding | Decision |
| --- | --- |
| Initially, the verified product fixes were absent from both old installations | Completed: the selected product now has an admitted isolated candidate; keep the old installations separate |
| Both services exited unexpectedly; recovery proved current readiness, not the original cause | Prioritize bounded failure diagnosis and safe future exit classification; do not require an unknowable historical cause before all development can continue |
| Matrix07 returned ten empty global results, and two zero-history clients made no NextUp request | Park global NextUp discovery as inconclusive; require new discriminating evidence before another experiment |
| A source55-specific automatic-refresh gate became a prerequisite for main upgrades generally | Separate data/security/upgrade safety, core client regressions, and feature-specific compatibility gates; explicitly supersede the old source55 deployment design |
| Repeated preparation, identity repair and evidence reconstruction displaced product work | Reuse verified helpers and receipts, scale new checks to changed risks, and stop unproductive tooling loops |
| The master plan's initial backlog and long historical handoffs look current | Use this short queue and the current-status index; keep old facts without treating old PIDs or pending actions as current |

## Ordered delivery work

| Priority | Deliverable and scope | Entry and completion condition |
| --- | --- | --- |
| 1. Diagnose and freeze the product — complete | Audited backend and matching frontend, including R01-R21, exit diagnostics and the cancellation fix | Reuse the exact verified source/artifacts. The historical exit cause remains unresolved; do not invent causality or repeat service starts to investigate it |
| 2. Admit an audited candidate — complete | Isolated candidate with its own database/state and separately reviewed binary/configuration transitions | Admission04 passed. Preserve its source, process, lease, recovery and owned-state identity; no new admission run or primary upgrade is needed merely to continue client work |
| 3. Close the core client checkpoint | Reuse the established original-client scenarios on that candidate: login/browse, movie and TV direct play, seek/stop/resume, MP3/FLAC and supported external subtitles; check the changed access and state paths | Pin client/media versions and require actual media delivery, advancing playback, durable state, user isolation and exact owned cleanup. Cite historical controls for unchanged contracts and rerun affected journeys. Do not restart all reference research |
| 4. Prepare and perform the main upgrade | Write a new plan for the selected audited candidate and the freshly observed main deployment. The old source55-only plan is superseded | Require candidate admission, core client regression evidence, an exact migration/recovery contract, a real isolated backup/restore and rollback rehearsal, fresh authority/ownership checks, and a bounded post-upgrade workflow. A failure retains recovery responsibility and cannot be relabeled success |
| 5. Close the remaining release matrix | Complete representative catalog capacity and blocked-storage/reboot cases, wider playback/transcode/subtitle cases, actual GPU profiles, packaging, project license and dependency notices, plus unresolved feature compatibility | Select one bounded increment at a time from M2-M6. Publish explicit client/server/media/hardware support rows and limitations. Missing hardware blocks that profile, not unrelated software verification. License and notices must be resolved before an external distribution release. A limited release remains partial |

Read-only source/artifact review may run in parallel. State-changing work on
shared fixtures must remain serialized. Each next task must name its concrete
deliverable and stop condition rather than request an open-ended M2-M6 finish.

The first diagnosis pass is limited to one focused work session, with a
60-minute investigation ceiling before reassessment. If retained evidence is
insufficient, record the missing observation and implement only the diagnostic
coverage needed to distinguish a future failure. Do not repeat service starts
or claim that a readiness check fixes the original exit cause. A known unsafe
condition blocks promotion until fixed; an unknown historical cause requires
explicit risk tracking and bounded candidate stability evidence, not invented
causality. Freeze the workload, resource limits, observation window and failure
criteria before candidate execution. Keep heavy verification jobs serialized
on the shared test host in light of the recorded memory-pressure episodes.

## Gate boundaries

| Gate | What it blocks | What it does not block |
| --- | --- | --- |
| Source identity, authorization, data preservation, migration and recovery safety | Any affected candidate admission or main promotion | Read-only diagnosis and preparation |
| Core real-client regression on the selected candidate | Promotion claiming the supported playback workflow | Independent API/unit work and fresh isolated candidate preparation |
| Positive global NextUp reference and real-client behavior | Global-selection/ordering and NextUp playback-refresh compatibility claims; complete acceptance of that feature | An explicitly partial internal checkpoint for unrelated verified behavior |
| Positive LibraryChanged automatic refresh | Automatic-refresh compatibility claims | Core playback evidence and an internal upgrade with that limitation explicitly recorded |
| Complete supported release profile | M6 completion or a broad compatibility release claim | Earlier partial milestones with exact scope and known gaps |

This intentionally replaces the blanket source55 original-client prerequisite
in the [old main schema28 plan](../development/main-schema28-upgrade-plan.md).
It does not admit its old runner, artifacts or receipt fields for a new release.
The replacement upgrade plan must bind the selected audited product and all
applicable safety/core-workflow evidence. Do not substitute hashes in an old
frozen input. NextUp and automatic refresh remain open requirements for their
full feature claims; they have not passed and have not been removed from scope.

The selected executable migrates a schema27 restore to schema28. Such a restore
does not establish a usable source32/schema27 rollback database. The draft main
contract therefore requires an actual old-binary restoration/start in isolation
from the same fresh recovery point. Likewise, cancelling a ready restore plan
retains its inactive staged database; later operations must bind that retained
state explicitly instead of treating the slot as empty.

## NextUp and automatic-refresh research stop rules

The existing matrix, browser and recovery scopes are closed and consumed. Do
not repeat the same empty-global requests or zero-history Home/TV/Suggestions
journey, reset their budget, or reinterpret missing requests as empty responses.
The unused remainder of an old time allowance is not authority for another run.

Reopen a feature investigation only with a recorded product decision and new
evidence that can distinguish competing explanations: for example an actual
client-issued query, a reproducible positive public reference state, or a
specific changed prerequisite supported by an observed public exchange. A
guessed parameter, arbitrary delay or renamed copy of the same fixture is not
such evidence. Reference inspection remains limited to permitted public APIs
and visible client behavior; do not inspect vendor source or its database.

Before any new experiment, freeze one hypothesis, predicted outcomes, the
minimum controls, a wall-clock/request limit, and reserved cleanup. Permit one
bounded attempt for that hypothesis, then report pass, observed difference or
inconclusive. A revised experiment needs materially new evidence. Real-client
discovery retains a ceiling of 20 minutes and two playback attempts per target;
use a smaller bound where the hypothesis needs less. No playback attempt is
authorized merely to spend the remaining allowance.

The [Goby comparison contract](../development/nextup-goby-comparison-contract.md)
is parked. The fourteen captured SeriesId controls remain useful if a concrete
regression or compatibility decision needs them; they do not justify a default
300-request run. Select only the controls necessary for that decision. Keep
global Goby policies labeled as policy; never make all results empty to imitate
the sampled reference negatives.

## Verification and tooling proportionality

All tests, builds, validators and runtime/media/browser checks still run through
`ssh test-env`; there is no local fallback. Plan edits use source/document
inspection. Changed tools require their own focused remote verification.

For future work, map each changed behavior to its risk and the smallest useful
check. Reuse the audit's exact-source evidence when the product inputs are
unchanged. For changed product inputs, run relevant regressions and one required
full verification pass for the final frozen snapshot; repeat only after a
material change, failure or unresolved concern. Never rerun the full suite for
a documentation edit or read-only checker correction alone. High-risk migration,
recovery, credential and ownership transitions retain independent review and
closure even when the product suite already passed.

Tool changes require targeted checks for the changed failure mode and input
contract; citing an old helper receipt does not verify newly edited helper code.

Keep public projections distinct from persisted rows. The seed exposed ten
catalog entries while storage also held the three library collection rows;
stored policy retained two legacy account flags omitted from the six-field
managed-policy projection. Before dispatching another reconciliation, exercise
its complete comparison against the already captured DTOs and stored snapshot.
Bind explicit representation mappings rather than assuming equal field sets or
counts, or weakening checks to ignore unexplained extra data.

The movie03 pause permitted movie04, and the later movie05/movie06 decisions
were also consumed. None supplies retry authority. Their shared lesson remains:
the adapter must preserve the accepted asynchronous lifecycle, and evidence
collection must not block owned Stop/Logout. The movie06 review now controls the
next offline increment and any subsequent execution decision. Keep the same
candidate, hosting, media and network boundary. A real product difference needs
a focused reproduction; a tooling failure needs saved-evidence reconciliation.
Neither result grants an unlimited sequence of renamed browser attempts.

Preserve old receipts and private artifacts. Reuse their accepted manifests and
checksums; do not recursively reconstruct every historical scope before every
new action. A new preservation contract must enumerate the resources the action
can affect, the expected changes and unrelated protected boundaries. Isolate
writes and check those live boundaries freshly. Any reduction from an old
contract must be justified for a new scope, never applied retroactively. Actual
identity drift, uncertain writes or unexplained foreign-state changes still
stop the affected operation until reconciled.

Pin each historical receipt actually consumed by the new operation. Reports
must distinguish freshly checked live resources from history cited through an
accepted immutable closure; a narrower check cannot claim that all 198 previous
roots were freshly verified. Preserve raw failed identity samples privately
before rejecting them, so a later review can identify the exact failed field
without exposing credentials or sensitive runtime data in the published report.

Prefer existing tested operators with small explicit input changes. Do not
create another general framework or duplicate an entire runner to add a guard.
If two successive tooling/checker failures prevent the same planned observation,
pause the experiment and review the shared cause and its expected value before
building another version. Retain each failure; retrying a read-only checker must
not rerun business operations. Runtime dependency changes belong in preparation,
not in an active browser/upgrade execution.

Each increment needs one concise closeout linking source/artifact identity,
checks run, observed product result, cleanup, residual risk and the next action.
Keep detailed raw evidence private and link existing receipts rather than
copying their full histories into every status file. Commit and push a reviewable
checkpoint under the existing development policy; never report a document or
tooling checkpoint as product compatibility or deployment completion.
