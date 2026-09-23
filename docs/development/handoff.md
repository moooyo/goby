# Goby handoff — September 23, 2026

## Resume here

The active objective is the approved [three-phase plan](../planning/media-analysis-resilience-plan-20260920.md):
complete each phase's code, run consolidated verification remotely, then merge
and push the accepted delivery. The full objective remains active for Phase 3.
Use the isolated `codex/media-analysis-resilience` checkout at
`C:/Users/moooyo/.codex/worktrees/media-analysis-resilience/goby`. Preserve the
unrelated changes in the original `D:/Code/goby` checkout.

Current Phase 3 checkpoint: scope10 generation
`phase3-tier10k-10-compound-02` passed actual admission and entered cold business
concurrency, then failed at **2026-09-23 02:41:37 UTC** when the **512 MiB
PostgreSQL cgroup exhausted memory**. The kernel killed catalog-owner backend
`586572`; PostgreSQL, Goby and the Actor subsequently exited. The driver records
`accepted=false`, `execution_complete=false`, and failure codes `observer_failure`
and `http_status`. Its partial remux-start p95 was approximately **8,023.7 ms**
against the unchanged **5,000 ms** threshold. No full 10k journey, 100k journey,
overload profile or any of the 28 fault/recovery cases is accepted.

Scope10 preparation, memory restoration and execution rebind passed. Its first
compound generation was refused at admission because the PostgreSQL version
banner differed from SQL `server_version`; its controls are closed and its
failure retained. Compound02 corrected the execution manifest while preserving
the original preparation references. Scope09's failed fixtures and the closed
26-database regression cluster have complete external archives and readback;
only their exact redundant guest copies were retired.

Source `8b6cb210f0ecc679c8067e19da142dfe963dfb8c` passed all 49 remote regression
stages: 35 ordinary Go packages, 4,277 parent passes, zero failures and 18 explicit
parent skips; 24 embedded parents; and 91 tests across eight Python scripts.
The 10 focused and 15 race passes are separate repeats. Both Go binaries built,
and frontend reuse was proved from unchanged inputs and all 73 artifacts.
These accepted checks precede the failed compound run and do not accept capacity.

Failure closure is complete and independently confirmed. The observer remained
active in its gap handler after the original application/PostgreSQL PIDs
disappeared; it was externally withdrawn after evidence preservation. The empty
publisher, control parent and successful closure collector are also closed.
Original application, PostgreSQL and Actor failure states remain unchanged.
Driver cleanup
recorded seven `cleanup_operation` errors and
`filesystem_restore_held_for_active_worker`. PostgreSQL data, WAL and control
files remain in place with the control state `in production`; no clean database
shutdown is claimed. The database has not restarted, durable job state is
unknown, and fixture rollback has not run. Preserve the failed state before any
recovery or successor workload.

Read-only analysis of the existing PostgreSQL log found 30 recursive roots/user
data query plans across five backends, with up to 182 JIT functions. `QueryItems`
and `QueryLatest` already use `SET LOCAL jit = off`. The last PostgreSQL sample
included 447,766,528 anonymous bytes and 76,296,192 file bytes, including
61,272,064 shmem bytes. These observations are diagnostic evidence, not a repair,
proof of a single causal SQL statement, or evidence of a page-cache-only OOM.

The next repair sets `jit=off` on every Goby database session at connection
startup, while retaining existing transaction guards and resource limits.
Real-connection tests cover replacement, Hijack, catalog ownership, deployment
leases and unchanged independent PostgreSQL sessions. This source change is
written but not yet verified. The prior clean regression cluster archive has
been transferred back for restoration and a new complete regression; the OOM
cluster remains untouched. Resolve the demonstrated memory/observation failures
before admitting a new complete capacity journey. Read the
[Phase 3 record](media-analysis-resilience-phase3-20260922.md) and the private
checkpoint for exact evidence. Phase 3 remains unpublished. Phases 1 and 2 are
merged and pushed on `main` at `e41febbb36687d04340f5c651f4bf1bf376a4310`.

## Historical checkpoints before scope10

The following records preserve earlier failures, repairs and publication
milestones. References to then-current workers, retained guest copies, pending
retirement or future scopes describe those checkpoints; the current resumption
state is above and supersedes their next-step instructions.

At the scope07 checkpoint, preparation passed, but its actual 10k cold
compound workload failed. Catalog requests took approximately 6.2-7.9 seconds;
playback preparation took 6.2-6.4 seconds, and remux seek returned 415. Cached and
incremental phases did not run. The original application/database and all
workload/control processes are closed. All 349,360 failed-data archive members
passed external readback before exact guest copy retirement. The database,
diagnostics and full external archive remain; do not replay this scope as fresh.

Read-only JIT comparison of the original SQL preserved counts and ordered IDs.
Transaction-local disabling reduced isolated page execution from approximately
1.8 seconds to 26-79 ms. This is diagnostic evidence, not compound acceptance.
The query, remux, thirteen admission paths and driver AAC packet-proof repairs
are verified at `55d5069`: all 35 ordinary packages completed with 4,275 parent
passes, zero failures and 18 explicit skips; embedded command tests passed 24,
and eight Python scripts passed 84 tests. Focused eight and race fifteen passed
separately and are not double-counted. Both new binaries built; frontend reuse
was proven from unchanged inputs and all 73 artifacts. Resource observation
passed with four retained transient PostgreSQL-file `du` gaps and no persistent
loss. Independent closure passed; all 19 databases remain. Build delivery SHA:
`ee20450db2d81b581da95dc409745687df0491ba886566e95a1743a0d70f0708`.

At that historical checkpoint, scope08 was source-only and unreleased. Its later
preparation failure and scope09's cold failure are retained in the
[Phase 3 record](media-analysis-resilience-phase3-20260922.md). Neither is the
current runtime or an instruction to replay a retired scope.

Historical accepted baseline: scope04's full 10k workload failed during cold;
actual goroutine stacks confirm a catalog ownership/admission lock inversion.
The failed Actor, observer and controls are closed. Goby was diagnostically
terminated to preserve its blocked stacks; PostgreSQL then shut down cleanly.
All data and original failures remain retained. Repair `5fb968a` passed its
remote focused case and race check, while the old implementation failed for the
expected lock wait. Both new binaries built successfully. After preserving the
observer OOM interruption, a fresh-database continuation passed all five affected
packages: 2,139 parent passes, zero failures and five explicit skips, plus 24
embedded passes. Combined with unchanged complete scopes, that regression
had 4,264 passes, zero failures and 18 skips, with 73 retained Python passes.
The later scope07 repairs are separately verified at `55d5069` above.
Worker, observer and regression PostgreSQL are closed; all twelve databases remain.
The 100k tier, overload and fault/recovery matrix remain unrun. The first compound publisher failed its memory admission by
3,682,304 bytes before publishing sources or dispatching business work; its
process and control parent are closed. Phase 3 remains unpublished; Phase 1/2
publication is recorded on `main` at `e41febbb`.

Phase 1 is published: delivery `59ce074` and publication follow-up `2b284c3`
were fast-forwarded into `main`, pushed and read back. Its
[record](media-analysis-resilience-phase1-20260920.md) and
[results](media-analysis-resilience-phase1-results-20260921.json) preserve the
actual test/build sources and original failures. Its resources are closed.

Phase 2 automatic intro analysis and BIF previews are implemented, verified and
operationally closed within their declared scope. Delivery `feb5004` was merged
into `main`, pushed and read back on September 22, 2026.
Read the [execution record](media-analysis-resilience-phase2-20260921.md)
and [delivery results](media-analysis-resilience-phase2-results-20260922.json).
The frozen product is `49fc4ec67de30d3d5dbe51a40be257ccac3f3e57`; the complete
client/lifecycle repeat uses only the browser assertion fix at
`e94173f592f1ef000e94673dfc1c29742d526dcd`. Later documentation commits are not
new compiled product sources. The physical remote source remains immutable
`49acf4d` plus the accepted Go overlay, with a separately proven runtime view.

The first frozen-product fourteen-source run passed accuracy, four required
previews and actual skip but failed a cancellation assertion expecting 200
instead of the endpoint's existing 202. Preserve that failed attempt. The
second complete run passed all fourteen cases, the four required previews,
actual skip, configuration/CAS, native decisions, real terminal cancellation,
prune, durable restart and normal cleanup. It is a repeat of the frozen corpus,
not another unseen holdout trial. The current 105 intro parents plus 337 reused
parents are 442 affected passes, not the entire Phase 2 Go suite. Historical
failures and six original hardware/mount-profile skips remain explicit.

Independent worker and observer closure succeeded. The phase-owned PostgreSQL
shut down cleanly; all 22 database directories (including defaults), evidence
and retained media remain. Three unrelated service identities were unchanged.
There are no active Phase 2 units to resume or restart. Final resource closure
SHA-256: `a40e8f0eed56d5377bb67c528d7aa81bf28d7b30cc227749c595c64adfa54b38`.
The private checkpoint under `D:/Code/goby/.git/media-analysis-resilience-20260920/`
binds exact units, receipt hashes, screened copies and preserved failures.
Four credential-bearing preview HTTP originals remain remote; secret runtime
contexts were not copied. Do not publish them.

Phase 3 source implementation is integrated; consolidated verification is active and publication is pending.
The [execution record](media-analysis-resilience-phase3-20260922.md) retains the
complete 10k/100k mixed-workload, sorting/metadata concurrency, storage-fault,
process/database recovery and isolated-guest clean reboot/forced-reset scope.

Scope04 preparation produced an actual private handoff from product/build
`1ed1d69` and fixture `00af9e4`: 9,342 seed catalog items including directories,
658 pending media files, 14 licensed sources, 4,200 stress directories and
336,000 nonmedia entries. The fixture occupies 1,800,376,320 allocated bytes.
Cold, cached and incremental counts of 10,000 are frozen targets, not accepted
results; `accepted_capacity` remains false. Same-invocation journals establish
successful launcher, worker and observer exit, and their original processes,
cgroups and Actor-UID processes are absent. The prepared/context/closure hashes
are in the execution record; the credential-bearing context remains private.

Publisher02's final admission passed and dispatched the complete driver under
the original 1,280/512 MiB Goby/PostgreSQL limits. Actor `165428` failed during
cold before cached or incremental acceptance. Observer `165360` also had a
confirmed unit-state variable shadow defect; its observations were not accepted.
Root preserved and closed that monitor, then used SIGQUIT on the failed Goby
instance to capture the actual blocked goroutines. Original Goby `114162` and
PostgreSQL `114049` are now closed and their units disabled. Do not resume their
old contexts or present the diagnostic exit as a planned recovery test.
The original publisher refusal is retained, not reclassified as a passing
capacity result. The explicit aggregate-reserve successor sampled in the final
launcher after exec and observer readiness; a passing admission is not workload
acceptance. The failed workload and complete runtime closure are recorded in
the execution record. The complete repair regression and independent closure
are accepted. Failed scope04 data has a complete external archive, verified by
all 348,699 members. Exact guest retirement and regenerable-cache cleanup passed,
with independent worker closure and 22,229,053,440 free bytes afterward. Scope05
then deployed and built its seed fixture, but preparation failed at the final process
broker sample: the original broker rejected the exact temporary systemd memory
dropin. Its failed receipt is preserved, no compound ran, and the runtime was
restored then cleanly closed. The setup/broker successor passed twelve remote
guard tests; scope06 must verify the real broker after lowering before generating
the next fixture. Scope05 external archival and complete readback passed; exact
guest retirement is running. Scope06 remains pending. The full workload scope is
unchanged.
Scope03's access failure was traced to missing `io.stat` accounting, after which
its preparation limit was restored and the scope closed. Scope04 uses runtime
template09 with `IOAccounting=yes`; this was a deployment issue, not a product
defect or a capacity result.

The following regression and scope02 paragraphs preserve earlier attempts;
their failures remain evidence and do not describe the current preparation.
PostgreSQL temporary Seen staging, bounded disk spool with generation identity,
SQL candidate paging, configuration/store integration and runtime-resource
observation sources are written. Fixture/runtime/oracle, regression and actual
HTTP overload sources are integrated. Candidate `ad02b12` passed 58 Python tests,
the frontend and both application builds, plus 141 Go parents in six complete
package scopes (one contains no tests). Its full regression then failed during
the maximum-artwork backup case when the 384 MiB PostgreSQL cgroup exhausted
memory. The worker, observer and PG are closed; seven database directories and
the failed backup pair are retained. Use the [results ledger](media-analysis-resilience-phase3-results-20260922.json)
and private checkpoint for exact receipts and any later live successor handle.
Do not restart the closed attempt or repeat successful setup.

VM106 and its dependencies are provisioned. The successful second clone,
task-specific SSH and identity-bootstrap reboot have independent records;
the failed first clone and exact partial cleanup remain preserved. Infrastructure
readiness is separate from workload admission, which is still pending. The guest
has 2 vCPU, 3 GiB fixed RAM and a 33 GiB disk. Only the sole infrastructure owner
operates it; source implementation owners must not start independent workloads.
Regression successor03 completed with a separately admitted PG/worker budget
within the same guest envelope, a fresh backup pair, the entire failed package
and all unstarted packages/embedded command tests. The whole backup and recovery
scopes have passed, while library finished with three failed parents and one
original opt-in mount helper skip; server tests passed. The library failures cover
staged reconciliation latency, restored directory identity and symlink records.
Settings and task fixtures contributed two additional failed parents. The
original worker exited with failure after completing the full scope. Repair
source `1ed1d69b94548e5beb842906b6177664367310df` is frozen in private
`phase3/source-freeze-04`. Successor04 passed all ten stages: fresh frontend and
both Go builds, five complete ordinary Go packages and embedded command tests.
Source-stage05 also passed ten preparation tests. Root composed 2,138 new
ordinary parents with 2,125 retained unchanged parents: 4,263 passes, zero
failures and 18 explicit skips across 35 package scopes, plus 24 embedded
passes. The latest fixture composition is 70 Python passes, replacing the older
65-test composition as described below. The five original failures remain failed
historical evidence. The affected worker, observer, PostgreSQL and closure
helper are independently closed; PostgreSQL shut down cleanly and all ten
databases remain retained. Follow the private checkpoint for subsequent
capacity preparation rather than restarting either closed regression run.

The retained scope02 capacity attempt is closed. The fourteen-source transfer passed,
and fixture-only `0dacf3c` corrected media-ancestor read permission. Product,
workload and binary provenance remain `1ed1d69`. The first actual 10k preparer
was OOM-killed at 384 MiB during directory-stress creation, before any prepared
context or compound run. Kernel slab dominated the failed producer's memory;
Goby's separate idle snapshot was dominated by file cache. The five licensed
seed scans completed. Goby and PostgreSQL then stopped cleanly, their units
were disabled and their processes/cgroups are absent. Original data and failures
are retained; complete external archival and archive-worker closure passed.
Exact retirement of the two archived source trees and two large guest archive
files passed, with 220,626 removals and independent worker closure. All databases,
credentials, control records, binaries and external originals are preserved.
That retirement snapshot passed the unchanged free-space gate for the then-fresh
scope03; it is not a current admission measurement. Do not replay this
initialized database/workspace or restart either closed capacity scope. A fresh
preparation requires the revised observer, an observed preparation memory
envelope and restoration of the original benchmark limits before workload
admission. Both tiers, overload and all 28 fault/recovery cases remain required.
The [execution record](media-analysis-resilience-phase3-20260922.md#capacity-preparation-checkpoint)
binds the failed preparation and clean shutdown evidence.
Fixture successor `00af9e4` removes duplicate inventory buffers and eager Path
lists without changing the data or thresholds. Its complete 15-test preparation
suite passed remotely, with unchanged source hashes and actual worker closure.
Replace the previous ten preparation tests with this scope: the latest Python
composition is 70 passes, including 55 unchanged tests from seven other scripts.

The post-compound fault-fixture handoff repair is committed at `34344c8`:
successful capacity cleanup restores files while the catalog still reflects the
incremental pass, so a proven reconciliation must precede fault-fixture expansion.
This does not change the original exact-tier capacity requirements. Its pure
Python checks passed in source-stage05; the actual post-compound handoff remains
pending. The production Go repairs were genuinely rebuilt at `1ed1d69`, so the
old preparation-only `ad02` to `34344c8` bridge remains unused. Future tier setup
binds both source and artifacts directly to the accepted `1ed1d69` result.
Complete both workload profiles and the full fault matrices;
individual scenario partial results never accept the whole phase.
Do not reboot shared `test-env`, other existing VMs or the physical PVE host.

All tests, builds and runtime probes remain remote-only. `ui-ux-pro-max` remains
disabled. Live TV, EPG, DVR, tuners, DLNA, external channels and group playback
remain excluded; other unselected work remains deferred. Original Emby Web
commercial gating is outside the selected adapter acceptance. Earlier completed
increments below retain their historical scope and authorization.

## Previous selected-compatibility publication

The selected four-phase increment was fast-forward merged into `main` and pushed
to `origin/main` on September 20, 2026, at
`d3d043e19b9233e5d0b373ae42145f5189bda08d`. The remote ref was read back at that
exact commit. This documentation follow-up records that publication separately
from the tested product and fixture sources. No production deployment occurred.
Unrelated local OCI, packaging, helper and historical drafts remain outside the
published increment. Earlier selected-scope planning drafts are superseded by
the completed records, with their original bytes retained in a private backup.

On September 20, after the completed publication below, the user selected the
next account/playback, subtitle, artwork, music, management configuration,
search and client-protocol increment and requested its execution table. The
[new four-phase plan](../planning/selected-compatibility-plan-20260920.md) is the
selected increment, now closed on `codex/selected-client-compatibility`. Phase 1 is
closed under the user's explicit third-party-client adapter boundary. Backend,
administration, original-client PIN/local-password, next-episode, restart and
cleanup journeys passed in their recorded scopes. The user accepted delivery of
the compatibility adapter without treating original Web commercial licensing
as a blocking gate. Its two blocked intro modes and four client errors remain
recorded failures, not successful third-party-client evidence. Phase 2 is closed
after composed regression, scoped media repairs and the complete 16-stage native
browser journey at `bd20b23`. Both phases' owned PostgreSQL services and workers
are stopped, with data and evidence preserved. [Phase 3 music/search/discovery](selected-compatibility-phase3-20260920.md)
is also closed: the recorded backend/recovery scopes, 16-stage actual browser
journey and 24 composed administrator cases passed, with test-only repairs and
all earlier failures retained. Its workers and PostgreSQL are stopped. [Phase 4](selected-compatibility-phase4-20260920.md)
management configuration, protocols and external notifications are also closed:
the composed backend/recovery scopes, 18-stage actual browser journey, 18 selected
mocked administrator cases and two selected AMD cases passed. Original failures,
the unrelated live-device UI skip and all source boundaries remain explicit.
Its PostgreSQL, bridge and verification workers are closed; data and evidence
are retained. The unrelated original-client service is unchanged.
See the [phase 1 execution record](selected-compatibility-phase1-20260920.md).
The [phase 2 execution record](selected-compatibility-phase2-20260920.md) tracks
the accepted contracts, retained failures, final repair results and resource closure.
Each phase implements all delivery code before consolidated verification.
Local compilation and unit tests are now authorized; actual integration/E2E
runs on `test-env`, never locally. All four phases are complete within their
recorded boundaries, and final `main` integration and push are recorded above.
No verification environment needs resuming and no selected implementation task
remains pending. Unselected work retains its explicit deferred/excluded status.

The user now explicitly excludes Live TV, EPG, DVR/scheduled recording, tuners,
DLNA, external channels and group/synchronized playback. These are no longer a
deferred queue. Offline sync and all other unselected work remain deferred as
specified in the new plan. Existing generic dynamic sources and time shifting
remain supported; no removal was requested. This decision supersedes older
M7/P2 scope statements below without changing historical acceptance records.

The approved three-phase AMD/media and library/client-management increment is
complete, verified, and merged into `main`. On September 20, 2026, the user
authorized publication. Code commit
`80198b6aa8a163b696ceaff64da831847d82e496` was fast-forward merged from
`codex/amd-media-compatibility` and pushed to `origin/main`; the remote ref was
read back at that exact commit. Its complete Git tree is the accepted source16
tree `f9b57d3d99d5b9c22fea0a4b511298a914804e06`, including all 1,625 selected
source entries. This documentation follow-up records the publication separately
from the tested product snapshot. No deployment was performed.

For a new session:

1. Read this section, [current status](current-status.md), and the
   [final selected-compatibility record](selected-compatibility-phase4-20260920.md).
   The prior [AMD phase 3 execution record](amd-media-phase3-20260919.md) and
   [results ledger](amd-media-phase3-results-20260919.json) describe a completed
   increment, not the current four-phase work.
2. Inspect `git status --short`, `git log -5 --oneline`, and
   `git log -1 -- docs/development/handoff.md`. The last command identifies
   the documentation follow-up without embedding a self-referential commit ID.
   Fetch `origin` before starting new integration work. The current workspace
   original checkout remains on `main`; the completed isolated checkout is
   `codex/selected-client-compatibility`. Preserve unrelated changes in the
   original checkout. Do not reopen closed verification scopes merely to create
   fresh receipts.
3. Preserve the unrelated local drafts listed below. Their presence does not
   mean the accepted increment is uncommitted or requires another test run.
4. Continue only the next requested scope. OCI, non-AMD hardware, provider-online
   acceptance, full original-client parity, and broader capacity/platform work
   retain their separate boundaries; old failed scopes are not a work queue.

The latest verification includes 812 unique passing ordinary-server parent cases
and one explicit AMD skip, a real twelve-stage administrator journey with one
restart, maximum-size 20 MiB artwork backup/recovery at the recorded 1 GiB
PostgreSQL profile, and both application builds. Original failures and repair
runs remain separate. Ordinary execution used `ssh test-env`; AMD execution used
the approved non-root worker in PVE CT 104 through `ssh pve`. VM 101 is now
154 GiB. All 42 phase 3 workers and its dedicated PostgreSQL runtime are closed;
six database directories and the evidence remain retained. No live command
session needs resuming. This merge reused the accepted source and did not rerun
tests or reopen either verification environment.

Private evidence and the current machine-readable checkpoint remain under
`D:/Code/goby/.git/amd-media-compatibility-20260919/`; publication records are in
its `publish-main-20260920` directory. The retained remote root is
`/opt/goby-amd-media-20260919-50f45177f297` on `test-env`. These private artifacts
are not part of a fresh clone. A new verification run needs fresh environment
admission and must account for the stopped cluster and populated retained
databases; do not reuse consumed run names or erase earlier results.

The publication deliberately preserves pre-existing local changes to the OCI
recipe, release/notice packaging, audited-client helpers, native-capacity notes,
historical handoff deletions, Programs/session-resumption notes, and related
test-environment utilities/testdata. They remain outside the published increment;
inspect the actual working tree before selecting any of them for future work.

## Completed AMD/media execution decision

The user prioritizes advanced media and AMD GPU support and also selects the
remaining library, preference, user-state, artwork, music, navigation/event and
management compatibility work. The
[three-phase plan](../planning/amd-media-compatibility-plan-20260919.md) records the
approved order and completion gates. Phase 1 is verified and closed within its
recorded `source11` profiles. Phase 2 is verified and closed, including selected
remote verification, final builds, owned PostgreSQL/worker closure and
documentation. Phase 3 is also verified and closed within its recorded boundaries.
The approved three-phase increment is complete:

1. **Media processing and AMD:** HEVC/AV1 output, Dolby Vision input conversion,
   GPU filters and subtitle burn-in, progressive MP4 burn-in, and broader
   video-copy seek support.
2. **Subtitles and dynamic playback:** multiple and rolling HLS subtitle tracks,
   standard subtitle playlist routes, and dynamic-source subtitles, replay and
   time shifting.
3. **Library and client/management compatibility:** library editing, user
   preferences and state, images and avatars, music, client navigation and
   events, and management protocols.

Each phase completes implementation, remote verification through `ssh test-env`,
and documentation closeout before the next phase starts. Phase 3 also includes
the final cross-phase regression; there is no separate fourth acceptance phase.
Documentation closeout updates the API and support contracts, records the exact
verification scope and limitations, and leaves a current handoff.

OCI and other GPU profiles remain deferred. Actual inventory found the AMD GPU
on `pve`, with no render device in VM 101 (`test-env`). The user approved a
dedicated AMD-verification LXC, CT 104 (`goby-amd-worker`). Non-root VAAPI/Vulkan
enumeration and selected actual-media scopes passed there. HEVC framing,
8/10-bit HEVC/AV1 A/V seek timing, exact-format software fallback and selected
Dolby Vision conversions have actual-device evidence. Bitmap subtitle timing
is repaired and verified. Composed regression coverage, both builds and owned
process/database closure are complete in the phase 1 record.
Ordinary verification remains on `test-env`. See the
[phase 1 execution record](amd-media-phase1-20260919.md) for its closed scope.
The [phase 2 implementation record](amd-media-phase2-20260919.md) is the current
subtitle/time-shift checkpoint. It records a fixed maximum of eight HLS text
tracks, independent subtitle views, and bounded retention of actual published
output. Final v3 CT GPU and VM CPU-media/browser scopes passed, and both ordinary
and embedded artifacts were built from `source18`; `source19` changes only browser
verification material. The latest browser acceptance is the native-video/HLS.js
harness scope, not full original Emby Web compatibility. The record preserves
all earlier failures and exact artifact/runtime identities.

The final phase 2 closure receipt is accepted: 27 workers are terminal with no
remaining worker PIDs, source19 matches all 1,322 entries, and the owned PostgreSQL
unit, process, listener and socket/pidfile are closed. Its stopped cluster is
retained in the verified private local cold archive, and its original evidence
remains retained. The approved phase 3 scope has completed
source-bound verification and closeout. Its [execution record](amd-media-phase3-20260919.md)
and [results ledger](amd-media-phase3-results-20260919.json) preserve every failed
run and separate repair scopes. Final source16 is
`f9b57d3d99d5b9c22fea0a4b511298a914804e06`, with 1,625 selected files and schema41.
Composed ordinary server coverage is 812 unique parent passes plus one explicit
AMD skip, remaining 0; it is not a single full passing run. Source16/ui09 passed
its build, ten mocked checks, all twelve real stages and desktop/mobile review.
Source10's eight-package cross-media scope passed 3,690 events with thirteen
skips. Maximum-image backup/recovery passed at 1 GiB; the failed 512 MiB profile
and every UI/aggregate failure remain retained. Actual source14 ordinary and
embedded artifacts are accepted only through unchanged production inputs.
No overlapping counts are added. Final closeout stopped the 42 workers and
owned PostgreSQL runtime, removed listener/socket/pidfile and private transient
contexts, preserved twenty protected services and all selected source/receipts,
and retained PGDATA/six database directories. The 172-file local evidence handoff
is hash-checked. Final closure SHA-256:
`c81400f7dea32562549e293515ec8e8f78c4329cf76945064b9874b5af1dbb59`.
Reversible source/artifact archiving supplied capacity for the final checks.
The user subsequently approved expanding VM 101 to 154 GiB online. That
maintenance is complete, with unchanged boot/device identity and all 21 running
service process identities preserved. Phase 3 records its source-bound
maintenance receipts; the phase 2 closeout did not include a resize.

The prior P3 completion wording overstated user configuration support:
`POST /Users/{Id}/Configuration` was not implemented in that wave. The historical
correction and successful records retain their exact scope. Phase 3 subsequently
adds the selected persistent adapter and consumers with its own acceptance.
This does not retroactively correct the older implementation
or change an original result. Phase 2 remains closed within its recorded
boundaries; phase 3 is complete within its selected scope and deployment is not
claimed. Full original Emby Web, provider-online, OCI, non-AMD and broader
capacity/platform delivery remain separate obligations.

## Previous implementation checkpoint

The [media, collections, and management wave](feature-wave-20260919.md) has
completed its recorded implementation, functional acceptance, builds and
resource closeout. Code commit `39893aa195553d501ba0ffb56ce618066f2cb5e1` was
fast-forward merged into `main` and pushed to `origin/main`. Provider-specific
acceptance and OCI work remain deferred by the user. This is completion of the
selected wave, not the full delivery objective. Do not resume older scopes or
pending rows. The original M2–M6 scope is still incomplete; M7 remains deferred.
License selection and external distribution remain undecided.

This is the single current handoff. It replaces the September 15 native-capacity
and September 17 wrap-up handoffs. The September 17
and September 18 resumption files are retained local historical
execution logs, not an instruction to repeat their old queues. Original results,
failed attempts and independent reviews remain preserved.

## Repository and closeout

- Workspace: `D:/Code/goby`; current branch `main`.
- Current integrated code: `80198b6aa8a163b696ceaff64da831847d82e496`,
  fast-forward merged and pushed to `origin/main` on September 20, 2026.
  Development provenance: `codex/amd-media-compatibility`.
- Previous wave's integrated code commit: `39893aa195553d501ba0ffb56ce618066f2cb5e1`,
  fast-forward merged and pushed to `origin/main`.
- Remote: `git@github.com:moooyo/goby.git`.
- The historical accepted canonical full/build source is
  `1162808afafacdc9ab9a4d6c037263764b1afd7a`. Do not describe the current dirty
  checkout as that exact tested source without an explicit source comparison.
- The current three-phase code is integrated. Documentation changes are recorded
  separately from the tested product snapshot. Unrelated OCI, packaging,
  client/helper and historical-draft changes remain outside the selected work.
- The pre-wave uncommitted product/tooling inventory included diagnostic
  plan/cgroup/process changes, user-configuration DTO/tests, a refresh browser
  test, release/notice packaging, the OCI recipe and client-execution helpers.
  Pre-existing untracked utility files were retained. Inspect `git status`
  before any integration; do not blanket-stage unrelated work.
- The pre-wave closeout made no commit, push, merge, PR or issue write.
- That historical closeout retained no live command/session handle. Its
  file-only publications, composition, binding and readbacks terminated.
  No new H1 live outer, PG-only baseline or playback request was started.
- Historical preparation agents stopped after saving their work. Incomplete
  cache-amendment/r07 drafts are explicitly non-executable.
- No local test, build, candidate execution or verification suite was run.
  A public index was downloaded on Windows as unverified transfer data only.

## Previous feature-wave verification closeout

- The 692-test server scope is covered by 676 original passes and 127 targeted
  rerun passes, including new, failed and previously unfinished cases. The
  reruns overlap the original scope; these counts must not be added together.
- Ten core packages recorded 5,136 passes and one existing mount-helper skip,
  `TestRootBindingFullScanMountNamespaceHelper`.
  Identity recorded 181 passes, backuppg 497, and recoverydb 177.
- The full recovery-manager scope `recovery-accept04` recorded 73 passes,
  zero failures and zero skips. Seven real browser phases, five new mocked
  checks and all four existing mocked checks passed.
- Ordinary and embedded builds passed; all 1,178 source files matched the
  checked source inventory. All 33 recorded worker invocations closed, owned PostgreSQL stopped,
  and the five protected PID/start/executable identities remained unchanged.
- Selected functional acceptance, owned-resource closure, code merge and push
  are complete. Provider-specific acceptance and OCI remain excluded by the
  user's deferral, not silently accepted.

The [wave verification record](feature-wave-verification-20260919.md) owns the
source-bound results and final closeout. These checks do not upgrade old
Programs/W, H1, production-promotion or complete M2–M6 acceptance.

## Product functionality available

Implementation availability and acceptance scope are different. The following
capabilities exist, with per-increment evidence; this is not a claim of full
Emby compatibility or completion of every milestone.

| Area | Implemented functionality | Remaining boundary |
| --- | --- | --- |
| Service and authentication | Linux Go service, PostgreSQL migrations/persistence, first-admin setup, users, tokens, sessions, application keys and library policies | Full upstream policy/configuration and client-wire behavior are not complete |
| Media catalog | Movie/TV/music scanning, library/path editing, approved-directory browsing, stable identities, local metadata, managed artwork/avatars, music credits and core entity APIs, hierarchy and library ACLs | Broader native scan/HTTP capacity, storage faults, durability and complete upstream music APIs retain separate gates |
| Client state and navigation | Persistent Configuration/DisplayPreferences with real consumers, progress/resume, ratings/likes/HideFromResume, session subscriptions, ancestors/counts/additional parts, selected global NextUp and event-driven refresh | Selected phase 3 contracts passed; complete original-client journeys and upstream preference/query/event parity remain outside that acceptance |
| Basic client playback | Login/browse, PlaybackInfo, original-file HTTP/ranges, external SRT/WebVTT and current-authority playback state | Exact media/profile/client boundaries remain in the phase 1 and 2 records |
| Software and AMD media pipeline | H.264/HEVC/AV1 output, remux/progressive audio/video, text/ASS/bitmap subtitle delivery and supported burn-in, TS/fMP4/packed-audio and adaptive HLS, bounded dynamic replay, multiple text renditions, supported HDR/Dolby Vision/deinterlacing and proven copy seeks | Recorded phase 1 and 2 media, selected AMD, native-video/HLS.js browser, build and closure scopes passed; arbitrary format/timing combinations, other hardware and full original-client profiles remain open |
| Playlists and collections | Persistent Playlist/BoxSet containers, membership, ordered duplicate playlist entries, sharing/ownership, catalog queries and user-state behavior | Current member authorization and the documented playlist/BoxSet distinctions apply; full upstream parity is not claimed |
| Administrator dashboard | Library editing, user preferences/state, artwork/avatars, music metadata/locks, sessions, keys, devices, tasks and supported system-event triggers, typed settings, activity/logs and diagnostics | The twelve-stage phase 3 journey passed; provider-specific acceptance and unsupported upstream fields remain separate |
| Native backup/recovery | Encrypted backup, import/restore planning, activation/rollback, administrator UI and offline CLI, including schema41 state and a valid 20 MiB managed-image round trip | Large-image acceptance used a 1 GiB PostgreSQL limit; the failed 512 MiB profile and OCI upgrade/restore boundaries remain explicit |
| Packaging | Embedded assets, native systemd package work, OCI recipe/build/import and internal notices assembly | Final architecture/hardware/deployment matrix, licensing and external release are not complete |

See [implemented API surface](../api/implemented.md),
[scope](../api/implementation-scope.md), and the
[support matrix](../planning/support-and-delivery-matrix.md).

## Functionality still missing or intentionally limited

1. Text extraction, fonts, supported HLS/progressive burn-in, fixed multiple text
   renditions, standard subtitle-playlist adapters and rolling windows are
   implemented and accepted within the [recorded media contracts](advanced-media.md).
   Embedded subtitle deletion and bitmap OCR remain unsupported. Provider code
   is integrated, with provider-specific acceptance deferred.
2. Modern-codec output, selected AMD processing and bounded dynamic replay are
   implemented. Replay covers only actual retained output under duration/byte
   limits; bitmap changes cannot rewrite burned history. Copy seeking requires
   its codec-specific source/packet proof, including separate AAC-copy admission.
   Arbitrary timing/codec combinations, other hardware profiles and full original
   client compatibility remain outside the accepted scope.
3. Playlist/BoxSet membership, mutation, ordering and sharing are implemented;
   their bounded access, entry identity and user-state rules do not establish
   every upstream collection behavior.
4. Supported subfolder/parental policies, user mutations, management settings
   and task executors are implemented. Complete upstream policy/configuration
   coverage is not claimed; online provider-specific acceptance is deferred.
5. Broader Emby preference, event, query, alias and client-behavior coverage
   outside the completed three-phase increment's declared functional scope.

The September 20 next-scope decision excludes Live TV/EPG/DVR/tuners, DLNA,
external channels and group playback. Offline sync and optional extensions
remain deferred. The selected next implementation is tracked in the
[four-phase plan](../planning/selected-compatibility-plan-20260920.md).
An Emby consumer web application, Emby Connect/cloud identity and proprietary
binary-plugin compatibility are outside the current scope. Goby's website is
an administrator dashboard, not a missing consumer-player implementation.

## Accepted verification that must be reused

The following results belong to earlier artifacts and scopes. They are not the
current wave's final acceptance or an instruction to replay historical work.

- Canonical `1162808` full/build: 25 packages, 2442 top-level passes, 28 required
  regressions, two builds, 59 assets and 5582 source files. The explicit mount
  profile gap and two inert helpers remain unexercised.
- Native r09 on the actual 116/e6 artifact: software diagnostic completion,
  cancel and owner deletion are scoped accepted, with resource closure. This
  does not establish which exact process exit was caused by each intervention.
- Administrator browser phases and native backup/recovery have their recorded
  scoped acceptances; they are not all claims about one identical current binary.
- M2: SQL/tiny-file baselines, bridge components and the finite permission-loss
  recovery profile are accepted. Native concurrent scan/HTTP capacity, other
  faults and host durability remain open.
- M6: the actual 116 internal notices package is scoped accepted: 59 files,
  64 directories and 54 notices; reusable packaging checks passed 32/32.
  This is not licensing clearance or external-distribution acceptance.
- OCI: r08 image build, r02 two-image import, r04 firstboot/normal stop and
  r09 catalog/same-image-restart business are accepted in their finite scopes.
  The original r09 outer remains failed; its separate business and physical
  reviews do not rewrite the missing original EOF/acceptance flags.

## Exact current H1 stop point

This is the retained, deferred H1 checkpoint. The current feature-wave checks
do not resume its live continuation or change any original failure flag.

The original H1 r02 failed at HEAD request 13 before any media GET. It had
already created the test media/library/viewer/policy/scan and a Prepared play.
Both retained 4-GiB volumes and owned processes closed. Do not recreate or
rescan that prefix, reuse revoked credentials, or rewrite the original failure.

The HEAD reader correction passed five real socketpair methods, original
`14d3cc`, with independent review. The original product HEAD remains incomplete.

The safe initialization projection `bd3971` is independently accepted only for
before-to-initialized facts: 35 tables, 365 columns, five sequences; 146 prior
row/xmin digests unchanged; 22 additions in twelve tables; no changed/removed
rows. Eight SQL wrappers support two full snapshot joins and three scoped
observations. All 253 explicit read/output descriptors closed. The projection
is 3760557 bytes / `6abc7a9fd17a5655b0b37d6267a64ec2d8935c252dac10592ffa53d0b7b25959`.
Its [root review](D:/Code/goby/.git/oci-playback-failure-projection-preparation-20260919-r01/actual-projection-01/root-initialization-review.md)
and two adjacent independent reviews preserve missing windows, credential
receipts and after-cleanup snapshots as unknown.

The new continuation preparation is:

`D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01`

| Completed file-only operation | Actual result |
| --- | --- |
| Source publication `2d8482` | Manifest 9234 / `ce334a4445f2d72cc30d3b362124520930a81f346c888ac366fa5bafb565add5`; publication 4393 / `b6032d1c873894f4ca9ab075d8293f20831901d0ca32470275f10256f9d874c0` |
| Composition `1c1f09` | Request 7105 / `650af9df4645cb862182b1f92f90cd0d5ffd086d6fea589925d1d614611e8eb8` |
| Binding `528777` | Bound input 19220 / `38811aadcd068fd4e5f0fa66a9c104d31e57a21236fd407c134b759bd0cc2847`; derivation 4363 / `027884793d1dcdaa92d2c320b0f1d638cd1a00dc4edd5f33251071805e325c12` |
| Readbacks `dc9bcd`, `2f204a`, `8e0ead` | Eight exact files; all eight explicit file descriptors closed |
| Derived runtime/worker | 68923 / `ae9586b22defc1323c90f223ca02ff564859c8e0e947c5c655f8117b982b7514`; 14304 / `642443d3c76a55db9b799eaca3ed7fe88247abf7f50e7a5a6e8cd9de437c3df0` |

The [independent binding review](D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01/actual-bound-read-01/independent-binding-review.md)
is 7312 bytes / `486b524110220a77d554480c4b7a6eac216d629b7996e9d011800c4a7eccc075`.
It verifies seven exact runtime literal replacements and one worker replacement;
installation root, PG alias, roles, execution owner and original private-input
Pin remain unchanged. Compose/bind each retain 724 bytes of three known public
SyntaxWarnings; their stderr is not empty.

The additional continuation checks have a split outcome:

- Check publication r01 `210a62` failed before tests because PowerShell changed
  UTC strings to an equivalent local-offset representation. Its original
  nonce `27c6d9a4f183` is consumed.
- The r02 publisher preserves the original strings; `53015e` succeeded for
  nonce `58b9e6c201df`. Checker/runner/subjects were unchanged.
- Original check `8908b2` retains tool/SSH exit 1 and supervisor status
  `failed_closed_or_inspection_required`. The child exited 0 and all ten
  methods/eighteen scenarios passed. The supervisor rejected exactly one
  248-byte public-source SyntaxWarning under its empty-stderr rule.
- The [independent scoped review](D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01/targeted-check-preparation-r02/actual-check-58b9e6c201df/independent-scoped-check-review.md),
  11306 / `a246595ac92fdffbf1ab394f39b71d11ca1a555de6e49a79fddeb99137d2eca1`,
  accepts the actual ten/eighteen branches, privacy boundary and 36/36 FD/
  process/stream closure, separately from the original supervisor failure.
  Do not rerun passed scenarios just to manufacture a green original result.

No live outer has run. After an explicit user decision to continue, the
[prepared entry](D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01/ENTRY.md)
must first check the current eight-file media layout and complete PG-only
post-cleanup state, then start Goby and check its exact startup delta before
fresh authentication and media delivery. Those future snapshots are still null.
The live admission may reject changed state; file binding is not live acceptance.

## Reference startup and trace-tool stop point

The reference instance's original r06 startup failed before its expected
application executable was observed; the child returned -25. SIGXFSZ is the
supported signal interpretation on that host, but the file/syscall cause is
unproved. Its processes/storage closed and its backing remains retained.
Do not increase the original 128-MiB application file-size limit without cause.

Trace-tool acquisition history must remain separate:

1. R04 signed Release and the 49-node host graph are accepted. Its index
   download timed out with a retained unauthenticated prefix.
2. R05 copied that prefix and completed two ranges. Range 03 timed out and
   closed; later ranges and assembly did not run.
3. Windows transport `870e00` received the complete public 9678380-byte index
   at `.git/artistless-index-transfer-preparation-20260919-r01/received-public-index.xz`.
   It was not parsed or authenticated locally.
4. R06 publication `4b0eed` succeeded, nonce `71f3b8d4a206`. Upload `8ea907`
   failed before stage creation; its original remote envelope has no result
   receipt or exception code. Local completed writes are not proof of remote
   received bytes. No index verification, package or extraction followed.
5. Current read `6d3dd1` matched 27 of 28 fixed files and both owner directories.
   `/etc/ld.so.cache` changed from 35207 / `b0089c2f2437d4f969e24e41bb76371ef58b9c756dd5775d3e6fa9ee774b2cb7`
   to 42483 / `da3e9ab42a4f25f5dba2804053e9311145466a4a3d4d29e5d5f2947834f483b0`.
   This currently fails the fixed-input gate before stage creation; it does not
   recover the unrecorded original exception or identify who changed the cache.

The [R06 failure/current-observation review](D:/Code/goby/.git/artistless-startup-trace-tool-preparation-20260918-r06/actual-upload-admission-read-01/independent-failure-observation-review.md)
is 9799 / `39a2ea9bdf4687b71eab32b37ae347fb93208c11d4b11ff1f0174a30383bef0e`.
No strace candidate has been accepted or executed by this acquisition.

These saved drafts are **not executable**:

| Directory under `D:/Code/goby/.git` | Stop condition |
| --- | --- |
| `artistless-loader-cache-amendment-preparation-20260919-r01` | Partial producer/PS sources only; missing core cache adapter, antecedent, manifest, entry and seal. Read `DRAFT-STATUS.md`. No actual amendment exists |
| `artistless-startup-trace-tool-preparation-20260918-r07` | Partial version-2 consumer hooks; implementation and PS interface unfinished. Inherited r06 manifest/ENTRY/seal do not match r07. Read `DRAFT-STATUS.md`; all actual amendment slots are null |

The contemplated amendment permits one pinned static `ldconfig -p`, current
cache/file/alias checks, and reuse of original ELF observations only if all
49 nodes, 42 resolution names, 125 edges and 96 version groups still match.
Changed/ambiguous dependencies must remain Pending. This plan has not executed.

Separate source-only trace preparations are saved at
`.git/m3-m4-a1-reference-startup-trace-preparation-20260919-r02`
(manifest 4116 / `084c165751f0d26f0278c6439d2494923c7f8e2736ceb683900292611e7f158d`)
and `.git/m3-m4-a1-reference-startup-trace-checks-preparation-20260919-r01`
(3077 / `003a87477c099cb987f15da911d29e51481d1877f31c9bdafc2347bf456a5e64`).
They preserve the original r01 source and correct a cleanup poll exception.
Eighteen current pure checks, one historical counterexample and three actual
tool cases are prepared but unexecuted; tool Pins remain unresolved. The final
whole package still requires root review. Do not mistake source preparation
for a completed startup trace or client acceptance.

## Capacity and retained resources

- Full capacity observation `8ef605`, `2026-09-18T16:21:59.553728Z`:
  MemAvailable 5510918144 bytes, root-free 9398964224 bytes, free inodes 5465229.
  The unchanged M2 floors are 6442450944 bytes available memory, 4294967296
  root-free bytes and 30000 inodes. The memory floor failed; this was not a reservation.
- Separate memory-only observation `5a166a`, `2026-09-18T16:47:15.008841Z`:
  total 12526919680, available 5490810880, swap zero. Do not combine these two
  timestamps into a fictitious single capacity sample.
- First M2 E75e capture/controller remain uninvoked. Do not lower the floor.
- The OCI backup/restore preparation's separate 35-GiB root-free requirement
  has not been met by the recorded root-space samples.
- Preserve intentional services A/B, the expected PostgreSQL instances and
  hosting/reference service. A's last recorded identity is PID 1907978/start
  34901535, invocation `2d8403319f3943dbb3d1da638f33093e`, 3d6/schema28; this
  closeout did not make a fresh live-service observation. Old-A-dependent work
  must finish before a new Programs transition is selected.
- Do not touch `/dev/shm/goby-m2-fullscan-20260914`. The paused mount profile
  and retained helpers require their own explicitly planned closure.
- Retained OCI, native and reference backings are evidence, not disposable
  temporary files. Fourteen earlier raw releases are already complete and
  cannot be reclaimed again. No new backing deletion was performed here.

## Work remaining before complete delivery

These are full-delivery obligations outside the completed feature wave.
They do not reopen its closed checks or authorize a deferred historical queue.

- Current Programs update/admission, then movie/resume, episode/browse,
  SRT/VTT/Off, audio bridge and G3 promotion after their prerequisites.
- Reference startup/W client acceptance and broader real-client/media profiles.
- H1 actual playback, OCI upgrade/backup-restore/public HTTPS and remaining
  packaging/deployment profiles.
- Native scan/HTTP overlap and throughput, broader storage faults and host
  durability, additional GPU profiles and native arm64 acceptance.
- The bounded limitations listed above, future source integration, support profile,
  license decision and external-release selection.

No completion percentage is assigned: these obligations have different scopes,
and test counts cannot be converted into a reliable feature-completion ratio.

## Decisions for the user

The alternatives below record the earlier decision point. The user already
selected the feature wave; this table does not request another decision or
restart its deferred OCI/H1 alternatives.

| Direction | Next concrete outcome | Tradeoff |
| --- | --- | --- |
| Finish current acceptance first | Continue from the reviewed H1 binding, complete reference diagnosis and the chosen client/deployment gates | Produces a better-supported existing feature set; capacity/hardware prerequisites remain |
| Prioritize user-facing functionality | Select a bounded feature such as playlist membership or advanced subtitles, implement it and verify its exact scope | Expands capability while the existing delivery gaps remain open |
| Consolidate the release first | Review/integrate the dirty source, decide the first supported profiles and license/distribution boundary | Clarifies what can ship; it does not retroactively pass missing runtime evidence |

The user selected functionality first, then completed the approved three-phase
increment and authorized its merge and push. The earlier decision table is
historical. An internal release with limited supported
profiles must not be renamed completion of the full M2–M6 goal.

## Rules for the next session

Use Chinese for conversation and English for code/comments/documentation.
Use Windows PowerShell locally. Ordinary tests, builds and runtime/browser probes
remain serialized through `ssh test-env`; approved AMD checks use CT 104 through
`ssh pve`. Local verification is not authorized. No live command handle remains
from this increment. Preserve original evidence before starting a new run; a
consumed failed scope is not an automatic retry opportunity.

Keep private credentials, request bodies, raw database snapshots, environment
contents, keys and backing bodies out of local/public output. Use the scoped
remote safe projections. Preserve original false/null fields and keep component,
business, physical-closure and complete-delivery acceptance separate.

The current mutable machine-readable checkpoint is
`D:/Code/goby/.git/amd-media-compatibility-20260919/checkpoint.json`.
The older `D:/Code/goby/.git/resumption-runtime-checkpoint-20260917.json` belongs
to historical work and does not override this completed increment. Original evidence
and preparation directories are under `.git`; they are not all committed or
portable with a plain source checkout. This handoff governs current disposition;
old queue statements in historical logs do not authorize new execution.
