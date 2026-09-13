# Current execution plan

Reviewed on 2026-09-13 against product commit `a623375` and evidence checkpoint
`8bc7b76`. Execution resumed on 2026-09-13 at the user's request. Status:
**priority 1 complete; priority 2 candidate running, live admission pending**. The
[diagnostic increment](../development/exit-diagnostics-20260913.md) passed 60
targeted tests and a full 2,262-test/25-package race run with a Linux build.
The review itself performed no runtime verification or service operation;
subsequent execution must supply its own evidence and meet the gates below.

The new candidate has passed independent process, lease, database and public
health [inspection](../development/audited-candidate-runtime-inspection.json).
The [bounded live admission](../development/audited-candidate-admission-plan.md)
and core client gates are still open. The
[new main upgrade contract](../development/audited-main-upgrade-plan.md) is a
draft; it requires distinct forward-restoration and old-binary rollback proof.

This is the current work queue. [Current status](../development/current-status.md)
records accepted facts; [delivery and verification](delivery-and-verification.md)
defines the long-term milestones. Historical handoffs and experiment plans are
evidence and reusable designs, not an alternative next-action queue.

## Goal retained

Deliver an independent Linux media server using Go, PostgreSQL and FFmpeg, with
an administrator-only React/MUI dashboard and unmodified compatible clients.
Keep the complete M2-M6 scope and its evidence requirements. A scoped internal
deployment or direct-play checkpoint is not completion of the planned server.
M7 stays deferred. This review neither adds a consumer player nor removes
hardware, operational, broader media or compatibility obligations.

## Corrections to the preceding plan

| Finding | Decision |
| --- | --- |
| The latest product fixes are verified but neither installed service contains them | Establish a candidate for the audited product before spending more effort comparing an older candidate |
| Both services exited unexpectedly; recovery proved current readiness, not the original cause | Prioritize bounded failure diagnosis and safe future exit classification; do not require an unknowable historical cause before all development can continue |
| Matrix07 returned ten empty global results, and two zero-history clients made no NextUp request | Park global NextUp discovery as inconclusive; require new discriminating evidence before another experiment |
| A source55-specific automatic-refresh gate became a prerequisite for main upgrades generally | Separate data/security/upgrade safety, core client regressions, and feature-specific compatibility gates; explicitly supersede the old source55 deployment design |
| Repeated preparation, identity repair and evidence reconstruction displaced product work | Reuse verified helpers and receipts, scale new checks to changed risks, and stop unproductive tooling loops |
| The master plan's initial backlog and long historical handoffs look current | Use this short queue and the current-status index; keep old facts without treating old PIDs or pending actions as current |

## Ordered work after execution resumes

| Priority | Deliverable and scope | Entry and completion condition |
| --- | --- | --- |
| 1. Diagnose and freeze the product | Review the retained exit timeline and shutdown/lease/error-classification paths. Add a narrow safe diagnostic fix only if the code cannot distinguish the relevant causes. Select the audited backend and matching frontend, including R01-R21 | Start with read-only evidence and source review. Complete a source/artifact manifest, a bounded diagnosis record, targeted regressions for any change, and required verification for the selected product snapshot. Keep the historical cause unresolved if the evidence cannot identify it |
| 2. Admit an audited candidate | Use a fresh isolated candidate scope with its own database/state, or a separately reviewed candidate transition. Preserve the source55 control until the successor is admitted | Bind actual source, binary, assets, schema, process and recovery identity. Verify health/readiness, startup/recovery, changed authorization/storage/backup paths and owned-state preservation. No primary upgrade is needed for this step |
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
`ssh test-env`; there is no local fallback. This document-only review uses manual
source/document inspection and requires no runtime check or full test suite.

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
