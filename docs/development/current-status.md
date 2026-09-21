# Current implementation and delivery status

## Active increment: compatibility, media analysis and resilience

The user authorized the [three-phase plan](../planning/media-analysis-resilience-plan-20260920.md)
on September 20, 2026, including consolidated tests after each phase's complete
implementation and verified merge/push to `main`. Development starts at
`2fd9182` on `codex/media-analysis-resilience` in an isolated checkout. Phase 1
is verified, its resources are closed, and delivery commit
`59ce0747a5b6947a088fe9eec22a8fa72d77a183` was fast-forwarded into `main` and
pushed to `origin/main`, with exact remote readback on September 21, 2026.
Phase 2 implementation is complete and final acceptance remains open. Product
source `49fc4ec67de30d3d5dbe51a40be257ccac3f3e57` fixes recursive visual cropping
and unequal source windows by remeasuring frozen correspondences over a shared
clock interval. Four affected artifacts built successfully. All 105 current
intro parent tests passed; 337 unaffected parent results retain their original
source provenance, yielding 442 composed passes. The test wrapper's erroneous
post-run count assertion is preserved separately from the successful suite and
its accepted inventory proof.

The fixed five-call calibration qualified all six original episodes within the
unchanged safe bounds and five-second tolerance; all four body controls passed.
The three controlled analyses passed cold-open qualification, recap safety and
competing-interval branch coverage, and no-intro body controls. These analyses
reused previously verified extraction bundles: they are eight new Analyze calls,
not fresh media extractions or independent new originals. Options, labels and
quality gates remain unchanged, and all earlier failures remain retained.
Fresh holdout sources are still sealed. Independent source review and label
freeze, fresh holdout accuracy, and the actual 14-source consumer/lifecycle
journey remain required before phase 2 publication. All affected verification
workers are closed; the owned PostgreSQL and required evidence remain available.
The [phase 2 record](media-analysis-resilience-phase2-20260921.md) tracks those
source boundaries and retained failures. Phase 3 has not started product changes. The phase 1
[execution record](media-analysis-resilience-phase1-20260920.md) and
[delivery results](media-analysis-resilience-phase1-results-20260921.json)
bind 2,328 unique Go passes, three explicit skips, 55 composed mocked UI cases,
seven frontend source tests, the complete 24-stage actual journey and both
application builds. Original failures and source reuse remain explicit. All 17
owned workers and PostgreSQL are closed, nine databases/evidence are preserved,
and three unrelated services retain their identities. Tests, builds and runtime checks
use remote environments; earlier local-test authorization is historical.
Unrelated original-checkout changes are preserved. The completed delivery
records below describe the previous increment and do not close this new scope.

## Completed selected compatibility increment

On September 20, 2026, the user selected the next account/playback, subtitle,
artwork, music, management configuration, search and client-protocol work.
Its [four-phase execution plan](../planning/selected-compatibility-plan-20260920.md)
is approved; phase 1 is closed on `codex/selected-client-compatibility` under
the user's third-party-client adapter boundary. [Phase 2](selected-compatibility-phase2-20260920.md)
is closed after composed regression, scoped repairs and the complete native
browser journey at `bd20b23`. [Phase 3 music/search/discovery](selected-compatibility-phase3-20260920.md)
is closed using the recorded `460c33a` backend/recovery/native browser scopes and
affected test-only repairs. All 24 administrator cases have composed passing
evidence. Phase 3 PostgreSQL and workers are stopped, with data and failures
preserved. [Phase 4](selected-compatibility-phase4-20260920.md) management
configuration, protocols and notifications are closed using composed backend
and recovery checks, the 18-stage actual browser journey, 18 selected mocked
administrator cases and two selected AMD cases. Its owned PostgreSQL, bridge
and workers are closed, with evidence and original failed results retained.
The [phase 1 execution record](selected-compatibility-phase1-20260920.md) separates
implementation from scoped acceptance. Backend repair scopes, administration,
original-client local-password/PIN, enabled/disabled autoplay, runtime restart,
credential revocation and cleanup passed. The original Web client's ShowButton
and AutoSkip intro modes remain blocked by external entitlement, with page errors
confined to those phases. The user accepted adapter delivery and removed that
original-client restriction as a blocking gate; no third-party seek result or
full client parity is inferred. Phase 1 owned test workers and PostgreSQL are
stopped, with evidence and database files preserved. Phase 2 workers and its
PostgreSQL runtime are also stopped; the unrelated original-client service is unchanged.
Live TV/EPG/DVR/tuners, DLNA, external
channels and group playback are now explicitly excluded. Offline sync and other
unselected work remain deferred. Local compilation and unit tests are authorized;
actual integration/E2E runs on `test-env`. The user authorized final merge and
push after all four phases complete. Integration was fast-forwarded and pushed
to `origin/main` at `d3d043e19b9233e5d0b373ae42145f5189bda08d`, with the remote ref
read back at that exact commit. This documentation follow-up records publication
without adding a runtime acceptance claim. No deployment occurred.
The completed historical baseline below is unchanged.

The approved [AMD media and client compatibility increment](../planning/amd-media-compatibility-plan-20260919.md)
is complete within its recorded boundaries. Implementation used
`codex/amd-media-compatibility` from `b15de9a`; the current branch is `main`.
Product commit `80198b6aa8a163b696ceaff64da831847d82e496` was fast-forward merged
and pushed to `origin/main` on September 20, 2026. Phase 1 has
completed its selected-profile verification, both builds and resource/documentation
closeout at `source11`. Phase 2 is verified and closed through the recorded
`source18`/`source19` and v3 runtime scopes, final builds, owned PostgreSQL/worker
closure and documentation. Phase 3 verification, both application builds,
resource closure and documentation are complete. Ordinary verification ran
on `test-env`; the user-approved AMD exception uses PVE CT 104. The
[phase 1 record](amd-media-phase1-20260919.md) retains its closed source-bound
results. The [phase 2 record](amd-media-phase2-20260919.md) tracks the active
subtitle/time-shift contracts, preserved failures and the completed closeout boundary.
The [phase 3 record](amd-media-phase3-20260919.md) tracks selected sources,
preserved failures, scoped repair results and final closeout.
This follow-up documentation update is separate from the published product
commit. No production deployment occurred, and repository integration does not
add runtime acceptance or inherit the historical wave's acceptance.

Final phase 3 source16 is `f9b57d3d99d5b9c22fea0a4b511298a914804e06`, with
schema41 and 1,625 selected files. Composed ordinary server coverage accounts for
812 unique passing parent cases and one explicit AMD skip, with zero remaining;
it is not a single full passing run. Latest UI build/mock/real scopes passed,
including 10 mocked checks and 12/12 real stages with one restart and natural
cleanup. Maximum-image backup and recovery passed at the recorded 1 GiB
PostgreSQL profile; the original 512 MiB OOM is not erased. Both CGO-disabled
application builds actually used source14 and remain applicable through
unchanged production inputs. The 42 workers and owned PostgreSQL runtime are
closed, all selected files/receipts unchanged, and twenty protected services
preserved. PGDATA/six database directories and the hashed 172-file evidence
handoff are retained. See the
[phase 3 results ledger](amd-media-phase3-results-20260919.json).
The prior wave's missing Configuration-write correction below remains historical;
the later phase 3 adapter has its separate accepted scope. Full original Emby Web,
OCI, non-AMD GPU, provider-online and broader capacity/platform delivery remain
outside this completed increment.

## Previous completed wave

The implemented functionality in the
[media, collections, and management implementation wave](feature-wave-20260919.md)
has completed its recorded functional acceptance, builds and resource closeout.
Code commit `39893aa195553d501ba0ffb56ce618066f2cb5e1` was fast-forward merged
into `main` and pushed to `origin/main`; that was the branch at its closeout.
Documentation-only changes are separate from the tested product snapshot. Provider-specific
acceptance and OCI work remain user-deferred.
Use the [current handoff](handoff.md) for earlier evidence and retained stop
points. Canonical 116 full/build and native software diagnostics retain their
historical scoped acceptance; M2-M6 delivery is still incomplete. No new H1 live
continuation has run.

Scope correction: the wave's original P3 row incorrectly marked user-configuration
writes as complete. User creation, update, password, policy and deletion remain
implemented with their recorded acceptance, but at that baseline
`POST /emby/Users/{Id}/Configuration` was not implemented. The corrected P3b was
assigned to phase 3 of the
[AMD media and compatibility plan](../planning/amd-media-compatibility-plan-20260919.md).
The original verification records remain unchanged and do not establish support
for the later API implementation.

## Historical feature-wave checkpoint

| Area | Current result | Scope boundary |
| --- | --- | --- |
| Source | Implemented advanced subtitles/media, playlists/collections, and supported policy/management APIs and UI completed their recorded scoped acceptance | P3b user-configuration writes were absent at this baseline; provider-specific acceptance remains deferred; unsupported profiles and fields are not implied |
| User configuration | Creation/update/password/policy/deletion compatibility was implemented; `POST /emby/Users/{Id}/Configuration` was absent at this baseline | Original P3 is corrected to P3a/P3b; the later phase 3 implementation has the separate acceptance above |
| Server | 692-test scope covered through 676 original passes and 127 targeted rerun passes, including new, failed and previously unfinished cases | These overlapping counts are not additive |
| Other packages | Ten core packages: 5,136 passes and one existing `TestRootBindingFullScanMountNamespaceHelper` skip; identity: 181 passes; backuppg: 497; recoverydb: 177; full recovery-manager `recovery-accept04`: 73 passes, zero failures/skips | The old opt-in mount profile remains unexecuted; no zero-skip claim is made for the core run |
| Browser | Seven real browser phases, five new mocked checks and four existing mocked checks passed | These named scenarios do not establish every third-party-client journey |
| Build and closure | Ordinary/embedded builds passed; 1,178 source files matched; 33 recorded worker invocations closed; owned PostgreSQL stopped; five protected PID/start/executable identities unchanged | Closure covers the owned wave resources, not unrelated retained historical resources |
| Integration | Code commit `39893aa195553d501ba0ffb56ce618066f2cb5e1` fast-forward merged into main and pushed to origin/main | Documentation changes are separate from the tested product snapshot; OCI and provider-specific acceptance remain deferred |

See the [wave verification record](feature-wave-verification-20260919.md) for
source identities and terminal evidence, and the [implemented API](../api/implemented.md)
and [advanced-media contract](advanced-media.md) for exact behavior and limits.
No current result automatically accepts an old Programs/W or H1 journey, a
production promotion, OCI delivery, or the full M2-M6 objective.

## Historical checkpoints

The dated statements below retain their original context. They do not override
the current handoff or authorize replay of completed or failed scopes.

The September 17 priority was to prepare Programs affected admission and the
M5 full-run guard from the independently reviewed V4 finalization.
[S2 actually stopped, replaced and started A once](programs-transition-first-execution-failure-20260917.md),
with three successful GETs, then failed at `after_preservation` with
`diagnostic_file_membership_changed`. A is now PID `1907978`, start `34901535`,
invocation `2d8403319f3943dbb3d1da638f33093e`, running `ead67c8f...` at inode
`1580898`. S1's earlier zero-launch preflight failure is historical. Do not replay
`d0006145...`, use old A/runtime/guard as current authority, or repeat stop/replace/start.
The original closer remains resources true/evidence false; independent failure/
closure supplementation is complete without upgrading that original result.
The [UTC rollover review and reader closure](programs-transition-utc-rollover-review-20260917.md)
are now complete; writer-PID attribution remains a source/time/order inference,
without a captured write syscall. The finalization changes from
`24ad7c1dfd49455f2dd723fca4c9940edca78121` now passed
[21 component methods and one separate saved-only check](programs-finalization-component-result-20260917.md).
Independent component/reader closure review is complete, and the source changes
are merged at `3f609b6`. The [actual finalizer](programs-finalization-result-20260917.md)
then completed once with PowerShell/SSH/caller/CLI exit 0 and an accepting original
closer. It generated V4 epoch `ae728a2a...` and binding `5d687e18...`, using 16
read-only SQL/32 metadata commands with zero stop/replace/start/HTTP. A/backend
remain `1907978`/`1907986`. Independent actual-finalization/reader-closure review
and the admission-facing closeout are complete; affected live admission and
client acceptance remain false. Preserve all original S2 failures.
The [M5 header correction](m5-http-header-targeted-result-20260917.md)
has separately passed two targeted tests and independent closure; complete M5
full/build and overall M2-M6 delivery remain unfinished.
The user confirmed that `test-env` is available on September 16 and then requested
root filesystem cleanup. The [capacity review and cleanup](test-env-root-cleanup-20260916.md)
confirm that the September 10 disk expansion is already in use; there is no
unallocated extension or separate unused data disk at that observation. The
subsequent [user-selected cleanup and additional 25 GiB expansion](test-env-root-maintenance-20260916.md)
are complete and independently reviewed: 138 selected directories were removed,
the root filesystem grew online, and approximately 44G is available. The user
requested resuming the task after maintenance. No product verification ran
during that maintenance. The subsequent [original theme diagnostic](library-theme-diagnostic-result.md)
passed once with one parent and one subtest, unchanged business budgets and full
owned-resource closure. Its recovery scan finished in about 0.832 seconds with
no observed ownership loss. This is non-reproduction, not resolution of the
retained full failure. The subsequent [complete Library package](library-package-diagnostic-result.md)
passed 606 top-level tests and 1584 subtests, with only the existing M2 skip.
The [fresh full attempt](programs-successor-memory-limit-result.md) subsequently
reached its worker memory limit during recovery. Sixteen complete packages and
1405 top-level passes are retained; the interrupted package is excluded and
neither build ran. Owned resources closed and protected state stayed unchanged.
The [disk compiler-volume check](compiler-disk-volume-verification-20260916.md)
now passes real mount execution, cache/temporary writes and owned closure on the
expanded root filesystem. Its first attempt exposed overlapping systemd mounts;
the corrected reader selects the visible mount by the opened directory's mount
ID. The full adapter uses a 2 GiB disk compiler volume, 2 GiB RAM scratch and the
original 3 GiB worker limit, with unchanged test scope and timeouts. The subsequent
[full run](programs-disk-full-result.md) passed 18 complete packages and 1447
top-level tests, including recovery, then failed server tests because its compact
source omitted nine versioned compatibility fixtures. All resources closed;
both builds remain unexecuted. A [complete-source replacement](programs-complete-source-preparation.json)
now contains all 5348 tracked files plus the original 57 assets. Independent
archive review and actual source preflight passed, but the fixed 5 GiB memory
floor was unavailable throughout its 120-second admission window. No worker or
volume was created, and protection/closure passed. Both execution scopes are
consumed. Capacity subsequently recovered and a
[fresh complete-source run](programs-complete-source-full-result-20260916.md) passed
all 25 packages with race instrumentation: 2309 top-level and 7577 subtest passes,
zero failures, and the one original mount-profile skip. Both ordinary and embedded
systemd builds succeeded. Independent result/source/build and resource/materialization
reviews passed; all owned resources closed and protected state matched exactly.
The actual embedded successor is now retained outside the closed workspace.
The separate [schema-29 catalog bootstrap](m5-user-deletion-catalog-generation-20260916.json)
has now generated its real artifact once and passed independent content and
resource-closure reviews. Only the two expected activity CHECK objects changed;
all 29 migration hashes, 35 tables and five sequences were checked. Its exact
bytes were transferred to the M5 branch. The owned PG, worker, disk volume and
lock are closed; no product tests ran in that generator scope. The
[combined source](m5-combined-source-preparation-20260916.json) is now frozen at
`138522b`, including the immediate cancellation hook and deterministic
reservation/peer-isolation regression. Focused static review and independent
5400-file archive reconciliation passed. The [actual frontend build](m5-final-frontend-verification-20260916.md)
then passed one `tsc --noEmit && vite build` invocation with separate execution
and artifact reviews. Its complete S1 archive preserves all 5400 source rows
and adds only 59 real dist files; the original contribution sidecar and successful
command are retained. The build processes/unit/cgroup closed, while source,
dependencies and artifacts remain in the workspace. A 345-byte future Vite
config-loader warning remains recorded. Combined Go/HTTP/browser behavior and
final builds are still pending and must consume S1 with `--frontend-contributions`.
The [M5 storage prerequisite](m5-full-storage-verification-20260916.md) has now
passed in the third scope, with independent review. The first missing-directory
failure created no volume/payload; the second payload `PermissionError` has no
retained errno/syscall proof. Both failures remain preserved. Creating the owned
directories before validation and binding host namespace identities for the
capability-free payload resolved the identified source conflicts without widening
budgets or permissions. The final run closed 73 controller/three payload commands
and 152 streams, verified four sentinels on three filesystems, and read back all
11 archived files/six directories. The recorded 79 PIDs, two cgroups, mounts and
backing resources closed. Its 256 MiB controller peak alone is not an OOM finding
or full 3 GiB workload proof. The subsequent [actual combined full attempt](m5-combined-full-capacity-failure-20260916.md)
failed capacity admission and is independently closed. All 60 internal samples
over 118,022 milliseconds were below the unchanged 4 GiB memory floor; disk space
was sufficient. The adapter exited naturally with `insufficient_capacity`/exit 1,
before any worker, Go package, build, volume or archive. Its 27 commands, 54 streams
and 29 recorded PIDs closed with protected state exact and the lock released.
Adapter RSS/peak and the cause of changing host availability were not recorded.
The scope is consumed and unchanged. The later [four-directory `/tmp` cleanup](test-env-tmp-memory-cleanup-20260916.md)
removed only the approved PowerToys agentic review-related directories, with no
Goby ownership claim. Observed tmpfs use fell by 1,436,913,664 bytes; the after-sample
reported 5,829,570,560 available memory bytes. Concurrent workload effects prevent
exact attribution of the memory increase. The new full scope
`/opt/goby-test/m5-combined-full-fcf3c8f4a474` has now [failed Library reconciliation
and closed](m5-combined-full-reconciliation-failure-20260917.md), with independent
failure-evidence/resource review. Eleven complete packages passed: 492 top-level
and 1993 subtests. Library's raw 604/1583 passes remain separate from that total;
two top-level tests and one child failed, with the original M2 skip retained.
Seventeen selected new passes (deletion store/activity/archive/config) are a subset
of the completed counts. Later media/server groups and both builds did not run.
The 68 recorded PIDs, PG, cgroup, three volumes and lock are closed with protected
state exact. The saved logs have no race or Go-timeout marker; later PG errors
and the recorded memory peak establish no cause. The later [focused diagnostic](m5-reconciliation-focused-diagnostic-result-20260917.md)
passed the original two top-level tests and two music subtests once in 16.501
seconds, with independent evidence/resource closure. Removal callback proofs
took 4588/4968/4431 ms with active, error-free contexts; the slowest had only
about 32 ms of the unchanged five-second budget left. This is non-reproduction,
not a cause or full-run correction. The earlier 255 source-status rejection and
its original false closure fields remain separate from the physical-closure
supplement. The later [phase-timing diagnostic](m5-reconciliation-phase-timing-result-20260917.md)
also passed two top-level/two child tests once and independently closed. Its two
auxiliary snapshots consumed 99.2-99.4% of removal proof time, about 2.2 seconds
each; deletion and final revalidation took about 1-2 ms and 3 ms respectively.
The later [query-plan diagnostic](m5-auxiliary-query-plan-diagnostic-result-20260917.md)
is independently accepted in its third scope: one parent/two variants passed,
with four snapshots, four `EXPLAIN` calls, equal per-selection results/digests and restored
settings. JIT accounts for about 99.93% of reported default execution time;
the disabled arm's snapshots took 3-4 ms versus about 2 seconds by default. This is
a fresh-plan, fixed-order small-fixture result, not direct proof of the original
full failure or a product fix. The two earlier permission/parser and numeric-
decoder failures retain their original SSH results and owned closure. The
[narrow product correction](m5-auxiliary-jit-fix-targeted-result-20260917.md) on
`9e70e4b` now passed eight top-level tests and sixteen children once, including
the original reconciliation cases, with independent result/closure review.
Its full query and five-second proof budget are unchanged. Cached-statement
checks establish `jit=on` creation/execution, statement/counter reuse and equal
results; they do not directly observe JIT generation. The separate full scope
`/opt/goby-test/m5-combined-full-0add3118b99f` exited 1 in original SSH session
`43043`. Its [independent review](m5-combined-full-http-header-failure-20260917.md)
accepts 18 complete passing packages and the failed server invocation: raw totals
are 2,129 top-level passes, 6,614 child passes, one top-level failure and the
original skip. The totals include server's 590/1,751 passing cases. All 24
JIT/reconciliation identities passed in this full run. Six packages, both builds
and materialization did not run. Owned resources closed with protection unchanged.
Subsequent static inspection identified a raw test-header key that bypassed
canonicalization. The separate [137b41b targeted result](m5-http-header-targeted-result-20260917.md)
now passes both selected top-level tests once (no children), in 3.937 seconds,
with original SSH `30627` exit 0 and independent closure. Its fake executor owner
does not establish real FFmpeg, the remaining full suite or builds; `0add` stays failed.
Programs transition admission has its own prerequisites and does not require M5 full success.
The other prepared increments remain short of full acceptance. Complete M2-M6
delivery remains unfinished and in scope. The previous unanswered-window
blocker no longer applies.

The [native-capacity operator correction](native-capacity-component-verification-20260916.md)
is integrated after 47 synthetic/subprocess methods passed. It fixes duplicate
observations, records inclusive measurement costs, enforces cleanup child-output
limits and preserves a terminal failure status. The subsequent
[Running-job observation increment](native-capacity-running-observation-verification-20260916.md)
is also integrated: 61 unittest methods and the affected reader 12, transport
eight and pool six groups passed with independent source/result/closure review.
The first pool attempt's namespace rejection remains preserved. The result
reader now binds request intervals to same-library persisted Running jobs and
reports full, partial or unproven overlap without changing the 600-request cap.
Actual native workload evidence and current runtime/artifact admission remain
pending; these component scopes do not close capacity acceptance.

The same fresh host observation found both candidate applications failed at
10:22:08 CST on September 16, alongside a global OOM event in the same second.
Their original invocations are retained; the three postmaster identities,
protected files and configuration hashes still match the September 15 recovery
record. The [new read-only preservation checkpoint](candidate-oom-exit-preservation-20260916.md)
subsequently matched all 35 tables and five sequences in each of four databases
against the latest September 15 after-restart baselines. Ten read-only SQL
sessions closed, protected state matched, and independent saved-record review
passed. The subsequent [native preservation checkpoint](candidate-oom-exit-native-preservation-20260916.md)
and independent review passed with zero new SQL or native-store writes. Both
applications were still failed at that preservation checkpoint. The subsequent
[actual application recovery](candidate-oom-exit-recovery-completed-20260916.md)
and separate post-start diagnostics observation have now passed independent
review. Both original applications started once, with four health/readiness
responses, two owned leases and eight closed read-only SQL sessions. All four
databases remained exact; PostgreSQL, configuration and protected resources were
preserved. Thirteen old logs remained exact, and each candidate added one active
log whose body was not read. The observers and locks are closed; these startup
scopes must not be replayed. Cause and physical integrity remain unproved.
The new diagnostic
used an explicitly stopped-candidate protection descriptor with no existing
service action or application database connection. The prior ready envelope is
historical and cannot authorize a live transition after the application
invocations changed. A subsequent real observation has now published the new
`db22e3d...` binding described below; recovery alone did not admit the Programs
successor.
Historically, after the [September 15 incident](programs-final-regression-incident.json), the
[recovery checkpoint](candidate-lease-loss-recovery.json) independently accepts
both once-only application starts with unchanged binaries/configuration,
health/readiness and exact four-database preservation across startup. Scoped
native and diagnostics/log/cache checks also passed: nine old logs retain their
bytes/hashes/inodes, each application has one new active log whose body was not
read, and the cache remained empty. A/B recovery completed within that historical scope;
physical integrity and Programs/client acceptance are not inferred.

The [v2 component scopes](programs-transition-component-verification.json) passed,
and the actual observation completed one cluster-identity query and one lease
query. The ready consumer envelope `aed2914...` is published and validated against
34 records before publication and after file readback. Historical `38b906...`
remains its predecessor; product epoch, seed, admission and hosting are unchanged.
Those frozen Programs components retained preparatory authority constants.
The later binding and actual product acceptance below provide their own evidence;
the historical ready envelope alone is not an executable Programs input.
The later [binding component scope](programs-binding-components-20260916.json)
passed runtime 42, successor 28 and closer 68 checks once with independent
review and full closure. It covers the fixed producer source, second recovery
ancestry and complete-source profile. The [current runtime checkpoint](programs-current-runtime-preparation-20260916.json)
has now completed its unique observer invocation: two acknowledged read-only SQL
commands, one lease-method call, about 1.129 seconds and a 56,352,768-byte memory
peak. A's identity and metadata/hosting projections remained unchanged. Observer
and query frontend processes closed, their recorded frontend groups were absent,
the owned unit cgroup was empty, and unlock/FD close succeeded. Query backend
disappearance, observer/outer numeric PGIDs and a post-close lock-inode check were
not separately measured; the application-owned lease backend is expected to persist.
The new `db22e3d...` envelope passed frozen-producer validation in memory and from
the published file against 40 unique saved records, with no new SQL/HTTP/service
actions during publication. Its predecessor `aed2914...` and historical `38b906...`
remain unchanged. The canonical artifact receipt is also copied byte-for-byte
into the actual full scope. The [final binding components](programs-final-binding-components-20260916.json)
have now passed runtime 44, successor 28 and closer 70 checks once, with independent
source/result/closure review. The 256 MiB unit recorded a 65,093,632-byte peak.
Tested tooling is integrated on main at `f45d73c`; its constants bind `db22e3d...`,
and both CLI sources were published with identical bytes. The [first actual fresh
capture](programs-state-capture-first-attempt-20260916.md) has now failed with
`programs_unreviewed_key_path`/CLI exit 2 and passed independent closure review.
Fourteen read-only SQL frontends were acknowledged and closed, leaving 71 partial
files but no reviewed/source-reviewed state or successful capture. The guard
rejected before key-content opening/hashing; the failure recorded no specific
filename. The original numeric outer SSH exit remains unknown/null. The consumed
scope must not be retried. The [generation-key metadata correction](programs-generation-key-metadata-verification-20260916.md)
has now passed a separate 151-check run (44/36/71), independent review and closure,
and is integrated on main at `a5af0b6`. It permits only the reviewed stat-only key
metadata; no real key body/hash or new capture was exercised by those components.
The [second actual capture](programs-state-capture-second-attempt-20260917.md)
has now passed independent state and owned-resource closure review. All 16
read-only SQL frontends closed, both 35-table/five-sequence snapshots match their
history, and stat-only key metadata matched without a key-body read/hash.
The exact 12-field startup summary supports retention and transition contracts
at the captured database time only. The [first saved-file consumer](programs-saved-product-first-attempt-20260917.md)
then passed `--check-captured` once on the real 23-field input, but its one actual
`runtime.load_programs_product` call failed with `programs_artifact_independent_review`:
the canonical review says `passed`, while complete-profile consumers expect
`verified`. The original failure is retained and owned resources are closed.
The [profile-specific correction](programs-review-status-verification-20260917.md)
then passed 152 component checks and was integrated at `8b1dead`. The
[second actual product readback](programs-saved-product-second-attempt-20260917.md)
completed the loader once with real byte reads and has independent composite
product/closure acceptance. Its caller remains failed with
`resource_properties_changed`/SSH exit 2: the successful main exit and required
resource properties were saved before the owned stop and subsequent unit
garbage collection. The failed caller was not relabeled or the loader replayed.
The [transition/recovery entry components](programs-transition-admission-verification-20260917.md)
then passed 46 methods once with independent review. Integration at `8477421`
removed the three-line unconditional Programs dispatch hold from main; strict
fresh-before checks, explicit predecessor recovery, helper pins and budgets remain.
The separate [caller-fault components](programs-transition-caller-fault-components-20260917.md)
passed eight mocked methods once and closed. PowerShell was not executed;
the real final GC census and closer `inspect_attempt` are outside that coverage.
The earlier operator `9b8d5e8a...` is unchanged. S1's caller preflight failed with
zero launches; its hosting correction then passed [three mocked methods](programs-hosting-namespace-r02-components-20260917.md).
The later S2 publication retained the S1-path `72c...` operation source and actually
performed the transition once. It failed after preservation because UTC rollover
at old-A shutdown produced a closed 143-byte log before new-A startup/GETs added
an active 1101-byte log. All nine old logs remain; other saved differences match
the expected transition, definition and unit-log changes. Original failures and
snapshots stay unchanged. The metadata supplement matches the running new A and
protected state and records 78 owned PIDs gone; it supplies no new SQL lease-grant
proof or V4 admission. Final independent failure/closure supplementation is
complete. Its initial prefix-field comparison rejection remains preserved;
the corrected saved-field review confirms the PostgreSQL log was physically unchanged.
An isolated finalization worktree is preparing strict UTC log authorization,
an optional V4 finalization descriptor and a continuation/publication program.
Its actual calls remain zero. Preserve new A; do not handwrite V4 or repeat transition.
The [execution plan](../planning/current-execution-plan.md)
is the active queue. Complete M2-M6 delivery remains in scope; M7 is deferred.
The [isolated preparation checkpoints](../planning/current-execution-plan.md#isolated-preparation-checkpoints)
now retain a reviewed test-diagnostic branch, a native user-deletion draft, and
the media-diagnostic implementation draft, now connected through native
administrator APIs, bounded run ownership, conversion-slot admission and a
Settings panel. Diagnostics remain disabled by default; no deployment was
activated. Static review and remote formatting are complete. The combined-source
frontend typecheck/build and real schema-29 catalog export have since passed in
their separate scopes. The combined full has failed and closed, retaining the
completed deletion/activity/archive/config coverage. The focused reconciliation
diagnostic and phase-timing scope each passed once without reproduction. The later
query-plan result isolates dominant JIT cost within its fresh-plan small fixture.
The narrow correction passed its targeted 8/16 scope and its 24 identities also
passed in the new full run. That full failed the HTTP diagnostic test and is
independently closed. The test-header correction then passed its two-test scope
with independent closure; full/build and browser
acceptance remain pending. Main's Programs product
source is unchanged.
Historical handoffs, PIDs, experiment inputs and verification receipts retain
their original meanings and are not fresh deployment observations.

The [Programs focused verification](live-tv-programs-focused-verification.json)
has 14 top-level and 118 subtest passes with independent result/resource review.
The earlier [ordinary full run](live-tv-programs-full-interruption.json) was interrupted
for the user's plan review and safely closed after 10 of 25 packages: 341
passes, zero failures and zero skips. The identity package's additional 109 raw
passes are incomplete package evidence and are excluded from that total.
No build ran and no product assertion failure was observed in that attempt.
Its user interruption is not a product failure or passing full suite.

The later final worker failed the Library package. Only its 11 complete passing
packages and 475 passes count as completed package evidence; Library's 605 raw
passes are excluded. Raw totals are 1,080 pass events, two related parent/theme
failure events and the one declared M2 skip. Neither ordinary nor embedded build
ran. That failed run used frozen source `74a69ab` / `3c0e7e0d...`. The initial
hypothesis that the failing theme snapshots bypassed the ownership context was
withdrawn after call-site review; the frozen source already selects `owned.ctx`
at both Query sites. No root cause or product correction is established; retain
the [recorded hypothesis and correction](programs-final-regression-incident.json).

All resources owned by this failed verification scope are now closed through
the supplemental RAM closure and root's independent review. Root also reread all
five incident files and matched their sizes/hashes. The original
`resourcesClosed=false` and `recovered_protected_state_changed` guard remain
unchanged. The global OOM killed `tsc` PID 1209025 in session 6413; its trigger
was `MainThread` PID 1208225. Their launching job and causal relationship to the
Library failure or A/B lease losses are unproved. Concurrent workload requires
coordination but is not attribution evidence.

The subsequent [retained Library timeline](library-theme-retained-timeline.md)
records the recovery-scan observer timeout about 19 seconds before the incident
summary's OOM time. The expected auxiliary rejection is present in the archived
worker-owned PostgreSQL log. Cleanup reports database finalization and ownership
loss; it is not a filesystem-root observation. The exact loss time and any
earlier resource pressure remain unproved. Independent source review found no
specific transaction or fixture leak. The existing phase/read/ownership
diagnostics were the next bounded step at that checkpoint and have since run
in the recorded diagnostic and full-verification scopes. No root-cause claim
or product fix follows from their non-reproduction.

The user has supplied the previously requested `test-env` availability. The
original theme subcase and complete Library package have each passed once.
The subsequent full attempt stopped at its worker memory cap. The bounded disk
compiler profile and later complete-source verification have since passed,
including both builds and independent closure review. No `ownedTx.Query`
fix is scheduled, and non-reproduction cannot clear the original full failure.
[Client components](programs-client-component-verification.json) passed within
their frozen scopes, including the typed episode/subtitle framework. The actual
artifact's saved-file gate and the separate 46-method transition/recovery component
scope are accepted. S2 has now changed A and failed transition acceptance;
strict finalization/V4 and client-admission inputs remain pending. No live journey has run. Reuse completed
recovery, component and observation evidence without repeating starts or consumed
inputs. Keep other tool versions frozen and bind admitted inputs to the verified
artifact and runtime authority, with no framework expansion.
Do not replay old inputs or bypass the original guard. The Programs
successor is running at the recorded new identity. Preserve that process while
preparing finalization; old runtime/guard pins are predecessor evidence only.
E11/G2 and the completed September 14 recovery retain their
historical scopes. Final client acceptance and main promotion remain open.

The earlier resumed work on 2026-09-15 completed the following scoped results.
The [core acceptance resolution](core-client-acceptance-resolution.md) remains
the delivery path after the current runtime-binding prerequisite. The [internal amd64 systemd installation gate](internal-amd64-installation-acceptance.json)
is accepted from the original runtime, corrected saved HTTP review and the
independently reviewed archived final state. The original failed sealer remains
unchanged; its final SQL transaction was not retroactively recreated.
The response-ID matcher correction passed
[35 remote guards and the exact saved subtitle replay](core-response-identity-verification.json)
with independent review; this closes a tool defect, not client acceptance.
The [reviewed movie-baseline contract](reviewed-movie-baseline-verification.json)
has now passed Python 47, JavaScript 46 and 12 log/history checks. Both languages
read actual movie05, movie06 and latest Subtitles01 states; all 35 tables, five
sequences and two foreign references remain protected within those saved cases.
The [TV/current-runtime component increment](reviewed-tv-baseline-verification.json)
subsequently passed Python 61, JavaScript 53, log/history 12 and runtime 18 checks,
actual saved-state integrations and independent review. The [single TV browse02
diagnostic](audited-tv-browse02-diagnostic.json) completed before its investigation
ceiling and passed independent owned-state/resource review. Its native Response
uniquely identifies GET `/emby/LiveTv/Programs` HTTP 404; the browser context's
completed flag is false and the one pageerror remains unaccepted. Client
acceptance is still false. The input is consumed and diagnostic tooling is frozen.

The latest accepted pre-incident A client snapshot is browse02's 22 sessions, 14 plays and six
UserData rows, with two retained foreign audio references. Only the declared
authentication/device/activity effects and uncounted preparation/expiry changed;
activity and device sequences advanced accordingly. Do not use the prior
Subtitles01 snapshot as a fresh full-state baseline. Browse02 is now retained
preservation evidence. The completed recovery checks do not turn a historical
client or runtime input into fresh execution authority.
The [transition decision](e11-candidate-transition-decision.md) selects existing A
for one direct Programs-successor transition and establishes its external administrator asset override from
saved configuration provenance and pre-incident hashes. At that historical checkpoint,
no transition or deployment had occurred. The [reference response question](reference-programs-verification.json),
diagnostic and focused product scope are complete and consumed; they are not
queued for repetition. The [core resolution](core-client-acceptance-resolution.md)
remains subject to the incident-preservation and recovery hold above.
Native capacity execution is held under its
[measurement decision](native-capacity-measurement-decision.md). The
historical pause, integrated capacity controller and remaining work are preserved
in the [current handoff](handoff.md) and
[tracked source manifest](native-scan-http-capacity-source-snapshot.json). Eight control-transport
groups and six child-lifecycle fixture groups passed remotely, separately from
the earlier twelve reader groups. Independent full review of the two new
results, the one-byte transport warning correction and controller orchestration
checks remain pending. No native capacity fixture or execution input exists.

The historical September 14 [M5 preflight resource incident](m5-refresh-preflight-incident.json)
required candidate recovery before verification continued. Disk exhaustion caused PostgreSQL PANIC/recovery and
both candidate applications exited after losing their database leases. One
audited build-cache clean restored 7,582,253,056 available root bytes. A later
read-only checkpoint matched every row in 35 tables and all five sequences
in each of the four retained databases against its saved baseline. Native
backup, lifecycle, control and staged-generation checks also passed independent
review. Physical database integrity is not established by those observations.
Both candidate applications have subsequently started once with their original
binaries/configuration and passed health/readiness, new lease ownership and
retained-state checks. Independent review of the [application recovery](candidate-disk-full-recovery.json)
and old diagnostic-log preservation passed. The subsequent [first M5 focused run](m5-refresh-first-verification.json)
failed one library test after 89 top-level passes. Its failed result, source
and closed resources passed independent review; server/browser checks were not
executed. The [corrected run](m5-refresh-corrected-verification.json) subsequently
passed all 109 ordinary focused tests but failed the browser interval scenario.
Its result and closed resources passed independent review. The subsequent
[fresh browser run](task-media-refresh-verification.json) passed the full
scenario and independent review/closure, with unchanged ordinary inputs and
embedded assets. Accepted focused coverage is 109 retained tests plus one
browser Go test. The subsequent
[final-source ordinary regression](m5-final-regression-verification.json)
passed all 25 packages from the beginning: 2,295 top-level passes, zero failures
and one declared mount-profile skip, followed by the Linux amd64 build.
Independent result and closure reviews passed. The 109 focused ordinary
results are not added to the full-suite count. Preserve the separate capacity
rejection, the final run's zero-dispatch loop-metadata rejection and its first
reader rejection; the actual product suite ran once. Four private archives
passed readback, owned processes/PG/cgroups closed, and ext4, loop and RAM were
released with protected runtime metadata unchanged. The verified M5 increment
is committed and pushed as `5faf854`. The subsequent partial increment is an
[internal amd64 embedded systemd package](systemd-package-plan.md) and actual
installation acceptance. Its build-script, environment-template and manual
installation changes are committed and pushed in `beaea34`. The
[package build verification](systemd-package-build-verification.json) passed
three actual builds, seven focused top-level tests (26 including subtests),
26 guards, independent review and build resource closure. The archive and
private evidence are retained. The first actual installation preparation passed,
but its runtime controller failed at entry because of a missing `sys` import.
Goby never started. The owned PostgreSQL and network anchor stopped; the first
preservation checker then rejected unavailable process-exit fields. Its failure
is retained. Corrected preservation passed actual read-only prechecks, independent
evidence review, archive readback and final resource checks. Configuration/media
and five installation-file copies remain private; the original installation
paths, units, namespace and PG tmpfs are closed. The import-only correction passed remote
symbol and invalid-input entry checks. See the
[first installation attempt](systemd-installation-first-attempt.json).
Installation acceptance remained open at that historical checkpoint. The separately reviewed
[r02 attempt](systemd-installation-second-attempt.json) also failed: Type=simple
returned before the final exec/configuration was complete, and the controller
froze the transient root systemd-executor observation. Later evidence bound the
same PID/start/invocation to the exact UID 995 / GID 986 Goby process. Failure
cleanup originally rejected that transition; a separate exact-owned closure
stopped all three services and retained the actual APP cleanup exit result.
There were zero HTTP requests and no second application start. Failure
preservation retained five directory trees and five installation copies,
archived PG/evidence and closed the installation, namespace and PG tmpfs.
Independent readback passed with protected state and shared accounts unchanged.
The [startup correction](systemd-startup-transition-verification.json) then passed
its remote controller cases and independent review. The
[third attempt](systemd-installation-third-attempt.json) recorded one startup
transition, accepted the final nonroot process, performed 147 HTTP requests and
closed four credentials with logout 204 / rejection 401. Its first APP stop
retained a normal exit and shutdown-completed event without ERROR events.
The runtime still failed: both SQL observer markers returned databaseOid as an
OID JSON string, while the controller expected an integer. All other compared
identity fields matched. The sealer closed the remaining infrastructure;
independent failure-preservation readback confirmed two archives, five retained
trees and five copies, absent installation paths and PG mount, and unchanged
protected state. The original observer failure records are unchanged; a separate
readback confirms their reported backend PIDs are now gone. No second APP start
occurred. The explicit numeric OID/shared-reader correction subsequently passed
twelve remote synthetic cases and independent review. The
[actual r04 attempt](systemd-installation-fourth-attempt.json) then passed the
real prestart SQL contract, two normal nonroot starts/stops, 272 HTTP requests,
six closed observers and three snapshots. The sealer completed its application
and database review, then incorrectly required JSON for a valid Emby
revoked-credential 401 plain-text response. Independent saved-evidence diagnosis
confirmed the exact product response contract. The final sealer observer did
not run. Failure preservation and independent readback passed for both archives,
five directory trees, five installation copies and all process/unit/mount/
namespace boundaries, with protected state unchanged. The runtime success and original
seal failure remain separate. No fifth attempt is admitted. The shipped unit
and product package are unchanged. The later
[archived final-state comparison](r04-archived-catalog-verification.md) and
independent review close the internal amd64 installation gate; the original r04
receipt still records its failed seal.
The [source checkpoint](systemd-package-source-checkpoint.json) now matches
864 tracked backend, embed-wrapper, module and package inputs to the verified
E11 source. A supplement checks `web/admin/embedded.go` from the same Git tree;
the original bridge had grouped that tracked source with 57 generated assets.
Those assets retain their separate build evidence. This checkpoint does not
complete M6.

The [bounded unit-reference experiment](systemd-stop-evidence-verification.json)
passed both real exit-zero and exit-seven control cases. It retained the
original execution PID, start timestamp, invocation and actual exit result
until the private D-Bus reference was released. Both fixtures and connections
closed with protected state unchanged. The
[prospective runtime/sealer integration](systemd-stop-integration-verification.json)
passed remote syntax/global checks, two invalid-input entry checks and 25
synthetic contract/fault checks. These cover retained-record types and causality,
bounded cleanup after file capture failure, no repeated stop, and preservation
of failure classification. That integration checkpoint ran no installation or
actual Goby stop. The later r02 failure above retains its separate real-process
and closure evidence; installation acceptance still requires the full successful
nonroot journey and independent sealing.
Historical candidate admissions retain their original scope.

The saved recovery scope is
`/opt/goby-test/candidate-disk-full-recovery-20260914` on `test-env`.
Its read-only execution receipt has SHA256
`591da28976ec05c7898a7aafd50e6d754f8fdc75d611a12d11cc5e11013cb25f`;
its native execution receipt has SHA256
`d2721d724669395eb6be4c981f222700b5f43e8a9086153d95c89e01e08d2f3d`.
Both released the deployment lock. Raw database and native snapshots remain
private on the remote host. The earlier incident report retains its original
pre-observation timestamp and has not been relabeled as application recovery.
The subsequent restart execution receipt has SHA256
`3dfb3639001f63a1b7545022e648480db7f777f8d81222e26d49dd8b340c2f8f`.
It records two application starts, four health/readiness requests, eight
read-only SQL sessions, unchanged data/sequences across all four databases,
closed readers and a released lock. PostgreSQL processes were not restarted.
The separate M5 recovered input has SHA256
`80f51f90acb7bc076a3f49ffdbc81ba3ea2e8a8ae4bef108ffaaf61b06b29d32`;
its preparation receipt has SHA256
`f3297d25cfbf68d32ef0701760d698cd6459b96f04cbb66fe6949c646c37bdee`.
The original product-source archive, worker, failed preflight and historical
candidate metadata remain unchanged. The revised input binds both new
application identities and all unchanged protected resources explicitly.

## Current gates

This table retains the historical Programs checkpoint's gate wording and
results. Its "current" identities and next actions are scoped to that
checkpoint, not the active feature-wave queue above; they do not authorize a
replay or override later recorded outcomes.

| Gate | Accepted result | Next required result |
| --- | --- | --- |
| Current candidate and runtime binding | S2 left [A running as PID 1907978 on ead67c8f](programs-transition-first-execution-failure-20260917.md); saved current metadata matches that identity and protected state. Original transition and closer evidence acceptance remain failed | Retain the running successor and all original evidence. Old PID 1648477, `db22e3d...` and the old guard are historical, not current entry authority. Prepare validated finalization/V4 without replaying the transition |
| Earlier client-candidate product | R01-R21, diagnostics, cancellation fixes and TV parent metadata passed 2,270 tests/25 packages and a Linux build on that earlier audited source/binary | Keep this historical proof with its original client candidate; it is distinct from the intended embedded artifact and its ordinary regression below |
| Earlier ordinary regression | Same badf396 source completed 25 ordinary packages across two phases: 2,276 passes/0 failures/1 explicit mount opt-in skip, ordinary amd64 build and independent review/closure | Retain that source and exclude its partial Library counts; it is not the new media-refresh source |
| Accepted media-refresh baseline | Frozen `f5b70c00...` passed one complete 25-package ordinary run: 2,295 passes/0 failures/1 explicit skip, Linux amd64 build, independent result/closure reviews and resource disposal | Retain its exact scope; Programs changes product source and cannot inherit this as its final full regression or artifact identity |
| Programs successor | [Actual finalization](programs-finalization-result-20260917.md), its original closer, independent review/reader closure and admission-facing closeout are complete. V4 epoch/binding exist; A/backend remain `1907978`/`1907986`. Original S2 failures stay unchanged | Prepare affected live admission and the M5 full-run guard using the reviewed V4/closeout. Candidate admission/client acceptance remain false. No D000 replay or further transition; complete M5/M2-M6 delivery remains open |
| Internal amd64 systemd package | Three actual builds, seven focused top-level tests (26 including subtests), 26 guards and independent closure passed. After the OID correction, r04 passed two nonroot starts/stops, 272 requests, six observers and three snapshots; its seal rejected a valid Emby plain-text 401. All four attempts have independently verified preservation/resource closure | The original final observer did not run; the independently reviewed archived final state now closes G2 through the composite acceptance. Native arm64, upgrade and whole-M6 remain open; no further installer run is queued |
| Fresh embedded candidate | Embedded `59096592...` was provisioned once; initial inspection, seed and native admission passed independent review. Admission used 88 actual requests with no cleanup failures. Recorded operator guards passed 213 checks | B recovered once with its existing binary/configuration. Preserve its inactive cancelled stage, consumed inputs and historical admission; no repeat inspection/seed/admission |
| Earlier audited candidate | TV successor, admission05 and September 16 recovery retain their original accepted scopes | S2 has replaced A with the Programs binary. Earlier process/runtime/guard bindings are historical; finalization and client admission must bind the retained new A |
| Core original client | MP3/FLAC retain their historical acceptance; video/subtitle failures remain unchanged. The typed movie/episode/subtitle framework passed its component scope | After validated finalization/V4 and client admission, bind inputs to the retained successor and preceding closeouts. Execute the three journeys and audio reuse bridge without replaying the transition or old client attempts |
| M2 catalog | Real-media rescan/ACL and native amd64 stop/start proofs passed. A separate SQL-seeded 10,000-leaf/442-folder handler test passes ACL/UserData isolation during an owned transaction block; those earlier scopes have independent review and closure | The SQL baseline is not physical scan throughput, native TCP/service performance, RSS/SLO or actual kernel filesystem blocking. Representative real-file capacity, host durability and the paused full-scan mount proof remain open |
| Real small-media capacity | The new 10,000-leaf scan/rescan test and affected seven-file shared-fixture regression passed independent review and resource closure; production is unchanged | This tiny valid-media and in-process Store-reopen profile does not prove HTTP/service performance, service/PG/host restart, actual filesystem stalls or a throughput SLO |
| Main recovery and deployment | Native archive/key witness, both distinct restore/restart proofs, actual old-installation return and final cluster/credential disposal passed. Original failures and private evidence remain preserved; fixture processes/namespace/runtime unit files are closed, and main is inactive | Core video acceptance still blocks new-binary main promotion. M2 host-reboot/power-loss durability and the remaining complete M2-M6 release requirements stay open |
| Complete release | Implemented foundations and historical scoped controls | Remaining M2-M6 capacity, operations, media, hardware, packaging, license and feature evidence |

The [support and delivery matrix](../planning/support-and-delivery-matrix.md)
maps those obligations to artifact/client/media/deployment slices and concrete
next actions. The current target is the verified Programs successor. Its
selected A transition already executed and failed acceptance after preservation.
Keep the new A running while preparing strict finalization and concrete client
admission; M5 success is not a prerequisite and a second transition is not planned.
September 16 recovery, the runtime
envelope, product verification/build, saved product gate and 46 component methods
retain their historical scopes. No validated V4 or client result has been produced.

The [catalog capacity/isolation increment](catalog-capacity-isolation-verification.json)
passed one race test with zero failures/skips on base `892c536` plus one new
server test. That base changes no Go or module files relative to `badf396`;
the new test was not in the earlier 2,276-test ordinary suite. SQL created
10,000 leaves and 442 folders across two libraries, one shared artist, 4,400
credits and ten UserData rows. No physical media, scanner or native TCP service
was exercised.

Twenty timed page/count-only checks covered eight before, four during and eight
after the owned transaction block: fifteen 64-item pages and five `Limit=0`
queries. Their cumulative 1,315 ms includes concurrent overlap; the maximum
sample was 234 ms. The 245 ms interval runs from first confirmed PostgreSQL
blocking to the gate rollback's return, not an exact PostgreSQL wait duration.
All ten UserData rows retained every field, timestamp and `xmin` before, during,
after cancellation/rollback and through closure. Schema allocation was
25,198,592 bytes and SQL seeding took 3,937 ms. The test took 6.16 seconds,
the package 7.174 seconds and the cold-compile Go command 49.816 seconds.
These are bounded observations, not an SLO, RSS measurement, scan-throughput
result, complete raw-catalog preservation proof or actual filesystem stall.

The first independent reviewer rejected a PostgreSQL-owned log under an
incorrect root-only assumption; the product test did not fail. Corrected review
used the fixed UID 103/GID 106, mode 0600, single-link guard and passed without
rerunning the test. It verified the exact 5,250-file source, twelve tool pins,
nineteen commands, thirteen Go JSON events, cleanup and protected state.
Closure preserved and reread the evidence and private closed-PG archives, stopped
PG normally, removed its private bind/cgroup and ordinarily unmounted the new
3 GiB RAM scope. The empty fixtures/underlying directories remain; no loop or
account was created. Six JSON records retain identical bytes under `retained/`.
No independent test binary was retained. Root free space at closure was
1,190,330,368 bytes; future work requires a fresh resource budget. That SQL
baseline remains complete and consumed.

The [real small-media capacity increment](catalog-real-media-capacity-verification.json)
passed both focused tests, independent review and evidence/resource closure.
Production is unchanged; the seven-file regression used a new environment and
does not add another pass to the historical 2,276-test suite.

The 10,000 tiny media files passed cold/cached scans, a replacement and in-process
Store reopen. Eight UserData rows, seeded after cold scan, retained all fields,
timestamps and `xmin` at three checkpoints; no final post-Close snapshot exists.
Catalog preservation covers
selected identity/hierarchy fields, not every raw column.

All processes closed, both private archives were reread and RAM was unmounted;
empty directories remain and closure observed 1,124,679,680 free root bytes.
This is not an HTTP/service benchmark, service/PG/host restart, storage-stall or
throughput-SLO proof. Next, prioritize core video only with new discriminating grounds.

The [partial full-regression checkpoint](full-regression-partial-verification.json)
records a 570-second package timeout and natural Go exit 1 after 576.293 seconds.
Historical library/server/total durations of 782.265/992.994/2755.104 seconds
exceeded the old 600-second command/2400-second business limits; the active
test had run 9.497893 seconds, not the entire package budget. The continuation
used TEST=1500/COMMAND=1560/BUSINESS=4200/RUNTIME=4800 seconds, the same frozen
source, 25-package inventory and 12 tool pins. It has now passed its 14 complete
packages/1,801 top-level tests and ordinary Linux build. The [independent
completion checkpoint](full-regression-continuation-verification.json) combines
those with only the first run's 11 complete packages/475 passes: 25 packages,
2,276 passes, zero failures and one explicit mount-helper opt-in skip. The old
537 partial Library passes are not counted. All older unexecuted mount-profile
and original timeout boundaries remain explicit.

The ordinary amd64 binary is 29,560,432 bytes, SHA256
`d6fc493d5664f85b6c7258c48d05081050457c1eac4965414528291695c66e53`.
It differs from the embedded M6 binary `59096592...`. A separately pinned
comparison proved all 374 production Go/embed/module files equal; it does not
claim whole-archive or binary identity, or a tagged full-suite run. The saved
embedded asset/build and root-profile runtime proofs retain their own scope.

Continuation records and four private archives were reread and sealed; all
owned processes/PG/cgroups, ext4 mount/loop, RAM and the lock closed. The source
archive remains stored once under the first scope; raw database archives and
logs stay remote/private. Two closed older build caches were reclaimed without
source/module/log/database changes, preserving the earlier zero-command lock
rejection. Root free space was 1,561,907,200 bytes after sealing. The regression
receipt's 61 preparation/legacy-consumer guards and pre-provision status remain
historical facts. The original Environment versus UnsetEnvironment assertion
failure is retained.

The subsequent [fresh-candidate checkpoint](fresh-embedded-candidate-checkpoint.json)
records one actual independent nonroot provision of embedded `59096592...`.
The app uid 995 and PostgreSQL uid 103 use separate owned directories; the old
candidate and protected paths are hidden from both services. All 57 embedded
administrator assets are selected with no external administrator directory.
Provisioning observed schema 28/28 migrations/zero users and 1,481,416,704 free
root bytes. Direct HTTP uses 28698; 28696 is only a reserved browser origin,
not a verified gateway. Private credentials and the raw manifest stay remote.
These process and capacity facts retain their observation time and must be
rechecked before dependent work.

Fresh seed and admission adapters are implemented in the working tree. Current
remote guards passed 42 preparation, 31 SQL-corrected inspector, 83 seed,
39 admission and 18 legacy TV tests: 213 guards. Superseded revisions are not
counted again; all operator counts are separate from the Go 2,276 results.
The final seed revision only adds private failure text bounded to 4,096
characters; the admission reader adds a private runtime-inspection failure sink.

The first two initial-inspection attempts failed before any HTTP, bootstrap,
seed or admission at those checkpoints.
Attempt 01 stopped before SQL because `systemctl show` omitted the empty PG
`EnvironmentFiles` property; typed D-Bus reads established its exact empty
value and unchanged unit identity. Attempt 02 passed that correction and made
one SQL call, but rejected its unrecorded output and retained
`readProcessesClosed=false`. A separate one-command read-only diagnostic
confirmed psql exit 0 with a 55-byte warning caused by `PGPASSFILE=/dev/null`.
That diagnostic's frontend and backend closed, no other postgres/psql backend
remained, and the postmaster identity was unchanged. Neither this diagnosis
nor the successful diagnostic SQL retroactively passes attempt 02.

The reviewed SQL correction uses an explicitly absent password-file path in
the owned scope, retains strict stderr checking and captures failed-command
outputs and exit facts. Corrected initial inspection then passed and was
independently reviewed: nine SQL calls, 63 HTTP requests, 1,107,438 response
bytes, 298 commands and 284 unit calls. All 57 embedded assets and five HTML
references matched. It made zero business writes, preserved process and
protected identities and closed its readers. The original two failures remain
unchanged.

One fresh seed also passed independent review. It used 49 normal and four
cleanup requests over 19,096 milliseconds, copied fourteen independent media
files totaling 201,156,949 bytes and created eight users, three libraries and
three scans. Ten public catalog DTOs correspond to thirteen stored items.
Both owned credentials passed logout 204 / exact-credential 401 and stored
revocation checks; no playback ran. The review confirms seven closed SQL
frontend commands, four cluster-identity and 109 server/listener checks. It
does not claim per-SQL backend-absence receipts or a final complete PostgreSQL
process-identity receipt that the seed did not record.

Native admission against this same candidate exited 0 after 601,357 milliseconds.
Its final report records `admitted_for_core_client`,
`candidateAdmissionComplete=true` and `clientAcceptance=false`. Actual traffic
was 82 normal plus six cleanup requests, 88 in total, with no failure and no
cleanup failures. The inactive restore stage remains retained. The fixed
600-second health window and 129-request cap were not expanded; the cap is
not an observed request count. Independent saved-evidence review passed:
eleven health samples span exactly 600,000 milliseconds, all nine TV checks
match, and three new sessions completed logout 204 / same-token 401 with
stored revocation. Source changes are limited to those sessions, two devices
and fourteen activity rows; the other 32 tables, preserved old rows and all
five bounded sequence expectations matched. Backup HEAD, range and full
download agree on the 290,038-byte archive. The ready-to-cancelled inactive
stage preserves all 35 tables and five sequences, excluding only `capturedAt`
from snapshot comparison.

The review also checked 23 runtime SQL samples with their raw outputs, records
and intents, 101 complete and closed reader commands, fourteen consistent
lease checks, fourteen fixtures and 190 server/listener checks. It bound the
separate current metadata checkpoint below without new SQL, HTTP, service
changes, runtime probes or business replay. Initial inspection, seed and
admission are consumed; do not repeat them, reprovision or reset the candidate.

An independent post-admission metadata check matched five protected identities
and the candidate/PostgreSQL process/listener identities, including PID, boot,
start ticks, UID, executable inode and cgroup. Both owned libpq password-file
paths remained absent; metadata commands closed with zero SQL, HTTP, service
changes or initial-inspection replay. Its later capacity observation was
1,233,956,864 free root bytes and 8,227,144 KiB MemAvailable. These values are
not a reservation for another heavy build. This check does not supply missing
historical per-SQL backend evidence from the seed.

All four first-regression-run worker cleanup checks passed, PG stopped normally and the
unit/cgroup closed. The reviewed copy-only archive closure subsequently closed
the ext4 mount, exact loop and RAM mount, retaining original failed artifacts
and complete private archives. Rebuildable Go-cache release preserved source,
modules, logs and artifacts. No profile, client or deployment gate closed here.

The [M2 full-scan mount checkpoint](m2-fullscan-mount-preparation.json) is paused
after two launcher failures. The first assumed a nonexistent module-cache path;
the reviewed second attempt compiled the helper with race instrumentation, then
`runuser` failed to change UID before `initdb`. No PostgreSQL daemon or helper test
started, and none of the seven planned scan stages ran. The failed unit has no
owned process, while its RAM workspace, compiled helper and evidence remain
retained. The UID failure's cause remains unassigned; no third renamed attempt
is queued. This result does not accept full-scan deletion protection or complete M2.

An independent [catalog rescan/ACL target](catalog-rescan-acl-verification.json)
subsequently passed one race test, zero failures/skips, across four state
checkpoints. Seven real media files in two ext4 libraries comprised one movie,
one episode and five FLAC tracks. T2 moved from album A to B within the mixed
library with the same inode and item ID; direct album counts changed from 2/1
to 1/2 and derived Played values reversed with membership. Audio ACL counts were
3/2/5, while artist entity counts were 5/3/8 including visible albums. All ten original UserData rows retained
every field and `xmin`. Queries after `Store.Close/New` required no scan, and
the following cached rescan preserved the same results.

The initial test failed before the move by using `ListEntities` for `MusicArtist`.
Correcting the test to `GetEntityByID` and `ArtistIds`/`AlbumArtistIds` queries
produced the passing second attempt; no production fix was made. Both attempts'
sources/logs and their private closed-PG archives were retained. The catalog scope
closed PostgreSQL normally and ordinarily unmounted its 3 GiB tmpfs, with an
empty cgroup and the underlying/fixture directories retained. This is Store
reopen evidence, not an OS service/PG restart, representative capacity or host
durability. It does not supply the paused mount experiment's seven unrun stages.

The separate [native catalog restart run](native-catalog-restart-verification.json)
passed once in `/opt/goby-test/native-catalog-20260914`, with independent
evidence review and sealing complete. It used the saved embedded
amd64 binary `59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312`
under a root/private-network profile, with an owned tmpfs PostgreSQL cluster and
seven ext4 media files. `GOBY_WEB_DIR` was absent. The first process scanned five
mixed-library and two hidden-library files, managed three users and their ACLs,
and made six favorite/played writes resulting in five UserData rows. Four owned
logout 204 / same-credential 401 pairs closed before its normal SIGTERM shutdown.

The second real process started against the same PG without another bootstrap
POST, scan or UserData mutation. Catalog/ACL/count/UserData checks matched;
four more logout pairs closed. Request counts were 89 after the first phase and
156 cumulatively. All rows and PostgreSQL `xmin` matched in nine core tables:
users 3, libraries 2, roots 2, items 14, catalog entities 1, item-entity links 16,
metadata-state rows 14, UserData 5 and scan jobs 2. Both schema28 checkpoints had
zero unrevoked sessions; all seven media files retained their metadata and hash.
The 800-byte embedded index matched the actual M6 manifest.

PIDs 557934 and 557978 had distinct start ticks. Both exited 0 with shutdown
complete, no forced termination or residual processes and no ERROR-level
application log events. All 31 worker commands exited 0; PostgreSQL stopped
normally and all owned cgroups emptied.
Independent review SHA256
`3cb9b9e2b7e789cf0dde23585fb8707f65baf590f069ef96661f6c3f2e51fbee`
checked all 156 HTTP pairs and their request-ID/log bindings, both SQL snapshots
byte-for-byte and all seven media files totaling 66,189 bytes. Closure SHA256
`b4d4993692477d448a67d8ac7acbf0c4ee637013b2d8e2c781db5f979ea9eb0f`
binds both reread private archives, complete app/PG/worker/cgroup closure and
ordinary unmount of the independent 3 GiB RAM mount. The seven ext4 media files
and empty underlying RAM directory remain retained.
The run is consumed and must not be replayed. It proves
neither PG restart, nonzero playback resume, playback/browser behavior, nonroot
operation, native arm64/GPU, capacity, host durability nor full regression or
main promotion.

The [M6 embedded administrator increment](embedded-administrator-verification.json)
now has actual remote results. All 22 handler tests/subtests and two ordinary
plus two embedded provider tests passed with race instrumentation, without
failures or skips. The actual embedded amd64 binary is 30,678,868 bytes, SHA256
`59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312`.
All 57 production assets and five HTML references were independently checked;
its manifest binds 848 source files. Eleven negative manifest cases passed,
including seven pre-Go rejections and four synthetic Go mutation guards. The
two ordinary provider race tests also passed without `dist`, while an actual
tagged compile without `dist` rejected as expected.

An actual arm64 cross-build produced 28,598,324 bytes, SHA256
`11e6e4c0bfdfb1cb4ed9c6debf6f2b7cdf272e2e4abefdd6a35b32ee8870aa3d`,
with ELF machine 183 confirmed. Both artifacts/manifests, the verified 911-file
source archive and command logs were retained, and ordinary unmount closed the
owned M6 build tmpfs. Protected state and the frozen module cache stayed exact.
The [build document](embedded-administrator-build.md) preserves the detailed
receipts and scope. That immutable M6 build checkpoint records no native runtime
execution at its own boundary. The later native amd64 test above is separate
evidence; no main/candidate deployment occurred at that historical checkpoint.
The fresh nonroot admission and its independent review have now passed
separately. Ordinary full
regression and the 374-file source bridge are now complete as recorded above;
they neither establish tagged full-suite acceptance nor inherit the old
2,270-test proof. Native arm64, OCI, GPU, licensing and full M6 remain open.
Both catalog baselines and their evidence/resource closures are complete.
Prioritize core video when new discriminating evidence or a justified correction
supports a bounded journey; no replay is admitted by these catalog results.
Actual filesystem blocking, representative throughput and host durability
remain independent obligations. The paused M2 launcher and consumed client/recovery
scopes retain their existing limits.

The [subtitle response-identity review](audited-subtitles-client01-review.md)
has also completed once using saved evidence. Context request 291's response ID
identifies physical exchange 294, not 293; matching URL/Range alone did not identify the
earlier physical response. Neither its cancellation cause nor the two original
page errors is resolved. The original timing failure and formal rejection
remain. No duplicate read-only review, new browser run or additional observation
framework is queued without new discriminating evidence.

The [material checkpoint](audited-main-recovery-materials.md) preserves the old
executable and 415 administrator files in a verified 16,590,013-byte archive.
At that checkpoint private credential metadata was reconciled, while key and
passphrase authentication remained unproved. The standalone key observer stopped at a helper-mode mismatch
before any database connection or master read. The native backup's same-snapshot
`WitnessBackup` was the required authentication step; no repeat observer is queued.
The later native create passed that witness. The isolated selected native plan
has now authenticated the archive/passphrase and restored generation master;
restored-account login has since passed on selected B and A, including B after
rollback and process restart. The separate actual source32 restoration and
authentication/restart have also passed, as recorded below.

The [native-backup scope](audited-main-native-backup-plan.md) is consumed. Its
first controller rejected a retained preparation filename before locking. The
corrected controller started source32 once, then rejected systemd's two memory
pressure environment variables before login or backup. Full environment checking
preceded assignment of stop authority, so separate, reviewed ownership recovery
was required to stop the exact invocation. That input paused after two
controller failures and was not executed again.

The [post-stop closeout](audited-main-native-backup-closeout.json) confirms main
and source55 inactive, 35 tables/402 rows and all five sequences exact, zero new
sessions/activity/backups, preserved lifecycle/control/old archive/master
metadata and unchanged external boundaries. Two cache marker files now exist;
the old 413-byte diagnostic entry and one new 451-byte log are closed. The
installed capacity and temporary fence were exact. These became the retained
inputs for the reviewed correction; the old empty-cache/ten-log baseline is stale.

The [ownership correction](audited-main-backup-ownership-correction.md) establishes
stop authority before configuration/readiness checks and precisely admits the
observed pressure variables. Its 12 service, 10 retained-file and 11 controller
guard groups passed remotely. A separately admitted child phase then completed
one create, one full download, 13 HTTP calls and the same-cookie logout rejection.
The [completed checkpoint](audited-main-native-backup-completed.json) records
archive `69f5e597c99f65099647cd3322d3174c`, 196,310 bytes, SHA256
`0a61cbd6c6ff23543ba873ed1a44dd33ecce2f8702f956e24096862996c7874f`.
Its native source-key witness covers four application-key rows. The archive
contains 405 rows; the final operational database has 408, with all 402 old rows
preserved, one new revoked administrator session and five ordered activity rows.
The other four sequences remain exact. Control revision is9, one new ready object
exists, the two cache markers and eleven old logs are exact, and a twelfth log
closed. Main and source55 are inactive.

The original execution receipt retains a final shared-parent metadata rejection
during empty-directory cleanup. Its recovery restored the fence; a separate
reviewed cleanup then removed only that exact fence and owned empty directory.
Capacity remains installed and the original restart policy is restored. No
HTTP, backup create or service start was repeated for cleanup. The final archive
bytes and candidate/PostgreSQL boundaries were checked again.
The [revised restoration plan](audited-main-isolated-restore-plan.md) requires
offline recovery into B, online application into A, actual native rollback to B,
and same-mount PG/application restart. Two offline applications do not retain a
usable rollback image. The separate source32 proof also requires replacing the
same isolated installation with the preserved old binary and 415 assets.

The [isolated infrastructure](isolated-restore-infrastructure.json) checkpoint records
an independent PostgreSQL17 cluster, dedicated PG/application identities, a
private network namespace and a capped 768 MiB PostgreSQL tmpfs. All 158 checks
across three sandbox profiles passed; its maintenance identity backend exited
naturally and protected main/control/candidate state remained exact. Five remote
transport guards covered process-group closure before provisioning. The subsequent
[target/material preparation](isolated-restore-targets.json) created four empty
ordinary-role databases, copied all 474 selected/old installation files and prepared
separate private configuration/archive/passphrase inputs. All five preparation SQL
backends closed naturally. This completed preparation was followed by the
[selected initial restore and activation](isolated-selected-initial-activation.json).
One native import/plan and one offline apply completed, with all source/staged
table fingerprints matched. At that checkpoint B was active at lifecycle revision1 with407 rows:
all406 staged rows remained exact and one complete system restore audit was
added. Its activity sequence advanced1; the other four stayed exact. Native
master/config descriptors match the archive, the default master remains absent,
and all CLI/observer processes were closed.

The [online rollback closeout](isolated-selected-online-rollback.json) now records
B serve/login, a second native plan into A, online apply at revision2 and A
login. After the sole rollback request returned202, the selected process was
OOM-killed at its 512 MiB unit limit. Its original failed execution remains
preserved. Independent post-OOM comparison proved A410 with only the new
rollback-request audit and all B412 rows exact. The control store retained the same
authorized rollback in `retiring`, with A/revision2 still current.

A reviewed unit revision changed only `MemoryMax=2G` and added
`GOMEMLIMIT=768MiB`. One new serve invocation resumed operation
`6957d09764e1d0dbfe98339a5f00685d`, without HTTP or new plan/apply/rollback
requests, then stopped normally. The independent result proves A410 exact and
B413: one session revoked, one binding changed and one `restore.applied` audit
added, with the activity sequence advancing1 and the other four exact. That
checkpoint selected B/revision3, generation `2a66c343e7265878629a21c7ea0336f0`, with no
transition or lifecycle journal. At that closeout the app was inactive and the
original PG516649 and anchor516461 were still live.

The subsequent restart completion passed once, with 16 complete HTTP responses
and no HTTP errors or unknown commits. Both old credentials returned401 at B;
this does not revoke the retained A row. Two new administrator logins each
closed with DELETE204 and same-cookie401. B advanced413-to416-to419, each time
adding only one revoked session and two audits; all other table fields and all
five sequence expectations matched. A410 remained exact and ten read-only
observer backends closed naturally. The app started and stopped normally twice.
One normal SIGINT shutdown/restart of the isolated PG retained the exact mount,
system identifier and B/revision3 generation. At that checkpoint PG was PID529062, start ticks
7006822, invocation `d820096e54ef47ebae60300554e8824e`; anchor516461 is unchanged.
Main/source55 and protected resources remained exact, and the complete source32
tree was unchanged. This selected checkpoint remains historical and unchanged.

A saved-input preflight failure is retained: default Python import resolution
selected `/root/inspect.py` instead of the standard library, before HTTP, SQL
or product execution. The input check passed under `-I -B`; the actual restart
workflow was executed once. Both this failure and the earlier OOM remain in the
[combined checkpoint](isolated-selected-online-rollback.json).

The [actual installation return and source32 closeout](isolated-source32-recovery.json)
now records two successful, single executions. Two exclusive renames returned
the shared installation to all 416 old files (40,869,739 bytes), retaining all
58 selected files (30,667,532 bytes); this installation step ran no product,
HTTP or SQL. The source32 workflow then restored the original archive into
independent D while C remained empty. Source405 and staged405 matched all 35
source/target fingerprints at schema27, with 17 credential revocations, one
expired play and one new binding; migration28 was not applied. Apply added one
restore audit to406 rows. Two login/logout workflows advanced D to409 and412,
preserving all other fields and all five sequence expectations.

All 14 HTTP responses completed without error or unknown commit; both cookies
closed with logout204 and same-cookie401. Five native CLI commands and ten
read-only observer backends closed, two app starts/stops were normal, and one
normal PG SIGINT shutdown/restart retained the mount and cluster identity.
At its closeout source32 was inactive at D/revision1, generation
`5f41cda3df2b444bf83236099819c74d`, deployment
`d4d1469c3a03a1b8a398521ebf19b032`. PG532226 has start ticks7112069 and invocation
`450ca1fbec084033ad38ddac2afd5030`; original anchor516461 was unchanged at that
source32 checkpoint. The selected tree and protected main/candidate state stayed exact.

The [final fixture disposal](isolated-restore-disposal.json) then completed once.
Final A410/B419 observations matched all35 tables and five sequences per database,
with both observer backends naturally closed. PG stopped normally by SIGINT;
ordinary unmount removed the exact mount319/dev55/768 MiB tmpfs, and the anchor
stopped normally last. All owned PIDs, cgroups and the private network namespace
are gone. All61 exact runtime unit files were preserved in private evidence and
removed, followed by reload; every unit is inactive with no FragmentPath and no
runtime unit file remains. The temporary cluster, A/B/C/D and their roles are
disposed, closing inactive A's credential responsibility without fabricating or
updating revoked_at. Protected main/source55/candidate state and the original
backup, passphrase and old installation archive remain unchanged.

The fixture working/evidence files and dedicated `goby-r69pg`/`goby-r69app` OS
accounts remain explicitly retained; no recursive deletion or account deletion
was performed. Both recovery/restart proofs and their declared disposal are
complete, while core video acceptance, main promotion and full M2-M6 remain open.

Persistent originals remain outside tmpfs; same-mount process restarts will not establish host-reboot
or power-loss durability, so the M2 durability gate remains open.

[Third-party notices](../../THIRD_PARTY_NOTICES.md) now retain 182 original legal
texts and one toolchain VERSION file, with exact-version records for all 11
current Go requirements, all 87 production npm entries and nine built font
assets. [Remote source/copy/font checks](third-party-notices-verification.json)
passed. The inventory remains incomplete:
historical/optional package gaps, upstream icon scope and the project license
remain open. The [actual package inventory](systemd-package-component-inventory.md)
has since bound the five E11 members, eleven binary build-info module records,
57 embedded assets and nine fonts to the retained legal collection. The package
has no notices/legal payload; the exact JavaScript package contribution graph
and upstream glyph evidence remain open. This does not close the M6 release gate.
The [staged Git artifact check](third-party-notices-git-verification.json) also
confirmed that all 183 legal/version files and the inventory retained their
verified bytes through Git; targeted attributes prevent newline conversion.

The dependency review separates pre-backup operational inputs from archive and
post-workflow outputs. Independent M2-M6 work can advance under its own gates;
promotion is not a blanket prerequisite. None of these changes waives the
original client errors, timing gap or full release requirements.

## Selected product and candidate

The [audit remediation](audit-remediation-20260913.md) is published as `a623375`.
The [exit diagnostic increment](exit-diagnostics-20260913.md) preserves known
lease, panic and listener failure classes; it does not identify the historical
unexpected exits. The later [restore cancellation fix](restore-cancellation-race.md)
is included in the selected binary. Its [full remote verification](restore-cancellation-full-verification.json)
passed 2,264 tests across 25 packages with race instrumentation and a Linux amd64
build, with zero failures or skips. The 57 frontend assets reuse 41 mocked-API
cases from 64 unchanged source files.

The isolated candidate is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`, binary SHA-256
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`.
Its earlier [binary transition](audited-candidate-cancellation-transition.json) and
[backup-limit correction](audited-candidate-backup-capacity.md) preserved all
35 source tables. The [seed closeout](audited-candidate-seed-closeout.json)
binds eight accounts, three libraries, fourteen media files, thirteen stored
items and ten public catalog entries. Seed, scans and hosting were not rebuilt
for later checker failures.

[Live admission04](audited-candidate-live-admission-closeout.json) passed a full
ten-minute window, 79 complete responses, a 290,550-byte backup download,
ready-plan cancellation and exact owned cleanup. The inactive staged recovery
database remains retained; it is not an empty slot. This is admission evidence
for the preceding `477d26ad...` binary. The [original-client host initialization](audited-original-client-host-startup-closeout.json)
is also closed, with an empty hosting library and its setup credential revoked.

The [TV successor transition](tv-parent-candidate-transition-closeout.json)
completed exactly one stop, replacement and start after 103 remote tool checks.
New epoch `76d7cc71...` runs PID 486706.
All source and inactive tables/sequences, fifteen revoked sessions, seven plays,
two userdata rows, controls and media remained exact. The PostgreSQL process,
configuration and original-client hosting remained unchanged. Old admission04
does not independently admit the new binary.

[Affected admission05](tv-parent-affected-admission-closeout.json) now passes
the new TV list/detail projections and restricted-user access. Its 24 complete
responses include a 60-second health/readiness window and both same-token
logout rejections. Exact reconciliation found two new sessions, two devices
and four audit rows; all seventeen sessions are revoked, all prior playback
and userdata remain exact, and the inactive stage/control files are preserved.
The selected controller passed thirty guards and three actual saved-receipt
integrations. The final [actual lineage reader review](tv-parent-client-lineage-review.md)
also passes, including the fixed public report and two unknown historical
credential IDs. The [first MP3 input](audited-mp3-client01-plan.md) is reviewed
against the exact seventeen-session state. This is API/runtime admission, with
core client acceptance open.

The [MP3 core scenario](audited-mp3-client01-closeout.json) now passes: 287
physical exchanges, 2,880,702 bytes of completed audio delivery, one counted
Stopped lifecycle, count1/55.346884-second userdata and no page errors. All
eighteen sessions are revoked; both browser/gateway workers are closed. The
single retained universal-audio reference is bound to the stopped play and
revoked credential. Old state is exact. The separately reviewed
[first FLAC input](audited-flac-client01-plan.md) preserved this full state.
FLAC also [passed](audited-flac-client01-closeout.json):287 exchanges,
3,127,772 bytes of completed media delivery, count1/55.342315-second userdata,
zero page errors and fully closed workers. At that closeout nineteen sessions were revoked,
with nine plays, four userdata rows and two retained audio references.

The [episode entry](audited-episode-client01-plan.md) was consumed against that
state. Its full TV browse and playback controls completed, but three page errors
correctly failed formal acceptance. The [owned closeout](audited-episode-client01-closeout.json)
reconciles 349 physical exchanges, one counted Stopped chain with count 1 and
59.157962-second userdata, and one unstarted Prepared row from later detail
PlaybackInfo. That closeout recorded twenty revoked sessions, eleven plays, five userdata rows,
two audio references and no encoding jobs remain. All prior rows are preserved.
Both workers exited with code 0 and are absent; candidate/PostgreSQL continuity is checked.
This does not pass episode or overlapping TV browse acceptance. The
[bounded rejection review](audited-episode-client01-review.md) led to the completed
diagnostic and subtitle increments below. Existing product verification and audio acceptance
remain reusable; no episode replay or new diagnostic framework is planned.

The [native rejection increment](native-rejection-observer.md) is now verified:
83 component checks, 35 controller guards and three actual saved integrations
passed. A first synthetic redirect fixture failure is preserved; the corrected
fixture uses its own private loopback server. Both synthetic browser scopes and
the temporary server are closed, with zero original-client runs. The same
increment reused the established movie readiness helper for the subtitle entry.
Original pageerror rejection remains unchanged; metadata association does not
resolve the old movie/episode errors. The [first subtitle entry](audited-subtitles-client01-entry.json)
passed with all35 tables/sequences matching the Episode01 closeout and an
unused actor. That input has now been consumed.

Subtitles01 completed SRT/VTT selection, cue visibility, seek and return to Off,
stop and logout. [Owned closure](audited-subtitles-client01-closeout.json) preserves
all old rows and records twenty-one revoked sessions, thirteen plays, six userdata
rows, two retained audio references and no encoding jobs. Both workers exited
and candidate/PostgreSQL continuity remains exact. Formal acceptance stays open:
two native primitive-undefined errors remain unattributed, and physical media
exchange 293 lacks matching browser response/cancellation evidence. The later
[response-identity review](audited-subtitles-client01-review.md#read-only-response-identity-review-2026-09-14)
binds context291 to physical294, not physical293. The original 3507.211510 ms
comparison against the 2000 ms limit remains a failed historical checker result,
not proof of a product cancellation delay. Follow the
[core acceptance resolution](core-client-acceptance-resolution.md), preserving
all consumed inputs, original failures and the unchanged acceptance boundary.

## Movie checkpoints

The [v3 input contract](audited-client-v3-input.md) integrates occupied movie05
history and request-ID-bound server cancellation evidence into the existing
controller/closeout. [Verification](audited-client-v3-verification.json) passed
71 new remote checks plus an actual read-only stdout capture. The unchanged
adapter and movie-lifecycle tests were reused for that exact revision. The
saved movie05 replay now reconciles physical media and durable state, including
an explicitly cancelled zero-byte response that contributes no media delivery.
Its original failed result and four errors with unrecoverable causes remain.

Movie06 consumed its [one diagnostic execution decision](audited-movie06-execution-decision.md).
It failed before playback at `movie_detail_title`: the item URL had changed,
but two movie titles from Continue Watching and Latest Movies remained visible
on Home. The helper incorrectly required a unique title. The new stdout capture
worked even with browser exit 1. No Playing or media request occurred, so zero
page errors in this run does not resolve movie05's errors.

The [movie06 owned-state closeout](audited-core-movie06-owned-state-closeout.json)
passed using saved evidence only. All fourteen sessions are revoked and both
workers are closed. Of 35 tables, 31 are exact; the four changed tables contain
one new session/device, two audit rows, one new unstarted Prepared play and the
explained expiration of the previous unstarted play. All older plays remain,
including two exact counted Stopped rows. The complete userdata row is unchanged:
play count 2 and resume position 1,217,878,390 ticks. No playback reference or
encoding job remains. The 263-row physical ledger includes 261 complete observed
HTTP exchanges, one CONNECT handshake and one rejection before upstream.

The transition readiness correction now [passes 19 remote tests](audited-movie06-readiness-verification.json),
including a regression that reproduces the original failure against unchanged
source. It preserves real action uniqueness, delayed readiness, separate play
lifecycles and owned logout. At that historical checkpoint the controller's
source selection had not changed. Later integration is recorded above; the
readiness result alone was a tooling checkpoint.

Movie execution stays paused under the [plan review](audited-movie06-plan-review.md).
Before another movie attempt, its per-attempt baseline constants must become an
explicitly reviewed closed-state input with matching saved-state regression.
The current v3 controller remains bound to movie05 and cannot admit movie06's
changed state. No movie07 input or run is claimed. The independent existing
actors do not depend on that movie-only adjustment. TV browse subsequently
consumed its own decision, as recorded below. MP3 and FLAC have since passed;
episode is consumed with closed owned state and failed formal acceptance.
Native rejection diagnostics have since passed and Subtitles01 is now consumed
with the explicit gaps above. Each future business run needs a frozen decision,
serialized execution and complete closure. Passing an independent scenario
does not resolve movie errors or authorize main promotion.

Earlier failures remain consumed and linked through their closeouts:
[movie01](audited-core-movie01-prelogin-closeout.json),
[movie02](audited-core-movie02-prelogin-closeout.json),
[movie03](audited-core-movie03-failure-closeout.json),
[movie04 alignment](audited-movie-offline-alignment.json),
[movie05 state](audited-core-movie05-owned-state-closeout.json) and
[movie05 evidence correction](audited-movie05-evidence-contract.md).
They establish neither passing client acceptance nor a reason for automatic
retries. Product verification remains reusable because the binary is unchanged.

## Historical TV browse checkpoint and product correction

[TV browse01](audited-tv-browse01-execution-decision.md) completed the visible
library-to-series, two season selections, Episode 2-1 detail, Home return and
logout workflow in the original client. Its observed cross-season list does not
prove strict season filtering. The browser then failed the adapter's
`candidate_item_detail_binding_changed` assertion; one page error remains
unclassified. The [evidence review](audited-tv-browse01-evidence-review.md)
identifies a missing `SeriesId`: seed mapping derived it from parent edges,
while the checker treated it as a required observed response field. Other
identity, parent, index and duration fields matched.

The [owned-state closeout](audited-core-tv-browse01-owned-state-closeout.json)
passed from saved evidence. Of 35 tables, 30 are exact. The new TV session/device,
two audit rows, one unstarted Prepared play and one zero-history userdata row
are fully explained. All fifteen sessions are revoked; both workers are closed;
all six movie plays and its complete userdata are exact. There are no playback
references, encoding jobs, Playing reports or media requests. Candidate,
PostgreSQL, lease and log prefix remain exact as recorded.

The [adapter correction](audited-tv-browse01-adapter-verification.json) passed
55 remote adapter tests and seven saved-response/failure checks. It preserves
requested-item and parent identity, rejects conflicting SeriesId when present,
and permits redacted diagnostics only for the precisely classified
non-credential assertion. The original failed result and unknown page error
remain. At that checkpoint the controller still pinned the prior adapter and
the correction had not been used by a new browser run. The later verified
integration and MP3/FLAC/Episode01 results above supersede that pending action.

An existing reference Episode detail also proves a distinct product difference:
SeriesId, SeasonId and their names were absent from Goby's mapper. The
[authorized parent metadata increment](tv-parent-metadata.md) now implements
the observed default Episode and Season fields through the existing read
transaction, with same-library ordinary-parent constraints. Its 21 effective
[focused checks](tv-parent-metadata-targeted-verification.json) passed: twenty
unchanged checks plus a corrected HTTP fixture rerun. The first fixture failure
remains retained. [Full verification](tv-parent-metadata-full-verification.json)
passed 2,270 tests/25 packages with race instrumentation and a Linux build,
without failures or skips. Raw test events, source/archive/manifest and binary
were independently reconciled; the isolated worker/PG are closed. Binary
`b0d6769c...` from archive `b363afdc...` is installed through the completed
[current-state transition](tv-parent-candidate-transition-plan.md) and passed
[affected live admission](tv-parent-affected-admission-closeout.json). This is not a Live TV feature expansion;
the nearby `/LiveTv/Programs` 404 is only a timing lead for the unresolved error.
Admission05 and the first audio/episode inputs have since passed entry review;
episode's remaining page errors keep video acceptance open.
The consumed TV actor now needs retained-state admission before any future
rerun, just as movie does. No main service change or complete core acceptance
is claimed.

## Main and parked investigations

The host rebooted at `2026-09-13T06:18:45Z`. The [reboot baseline](resumed-delivery-reboot-baseline.json)
found source55 and source32 main inactive with their installed bytes retained,
and PostgreSQL 17 main active. It made no database preservation attestation.
No main/source55 restart or upgrade occurred in this client increment. Old PIDs
and the [pre-reboot source55 recovery](candidate-source55-restart-closeout.md)
are historical, not current live identities.

The [fresh main identity record](audited-main-readonly-identity.json), captured at
2026-09-13T17:54:09Z, again finds main and source55 inactive. Main's source32
executable matches its historical SHA and 28,172,723-byte size; PostgreSQL's unit
remains active with PID 893. Units, ordered environment-file references and fixed
file metadata are stable during the read. Environment/master contents and
database/recovery state were not read; no lock was acquired and no service,
migration or restore action occurred. This record supplies the initial baseline
for the following private review, not upgrade admission or data preservation proof.

The later [read-only preparation checkpoint](audited-main-readonly-preparation.json)
resolves configured paths and captured primary/revision0/default lifecycle
selection. One settled create corresponds to the retained schema23 backup;
that historical archive is not a new schema27 recovery point. Three fixed
PostgreSQL read-only connections confirm the main5432 cluster/database/role
identities, exact main migration prefix 1..27, 35 tables and matching recovery
marker. The recovery database has no observed non-system relations or settings
table; complete empty-target admission is not claimed. All three transactions
committed, their owned frontends/backends exited, and the existing deployment
lock was released unchanged. Main and source55 remain inactive.

The [preparation review](audited-main-readonly-preparation.md) also records an
actual startup obstacle: backup storage had 520,519,680 available bytes, below
source32's 570,490,880-byte open threshold with current defaults and metadata
reserve. Capacity, the complete startup/configuration chain, pending work,
private recovery materials and key witness remained open at that checkpoint.
No policy change, deletion or startup was attempted. Even settled stores and complete migration
history cannot establish a zero-write normal startup because `ServerID()`
performs an upsert and subsystems can reconcile work and retention.

The subsequent [startup/capacity checkpoint](audited-main-startup-preparation.json)
passed the actual configuration loader for current defaults and a prepared
64 MiB object / 256 MiB total / 128 MiB minimum-free profile. Its conservative
creation floor is 302,055,424 bytes; final free space was 512,126,976 bytes.
At that checkpoint the profile was staged and installed settings were unchanged. The default
8 GiB scratch reservation explains why fixing only the earlier open threshold
would not make native backup creation fit.

The bounded main read found no scan, task-run, child or encoding recovery
candidates, no eligible triggers and no thirty-day-expired activity. Setup is
complete, the managed singleton exists and the task definition matches compiled
values. The first read stopped before connecting because only socket timestamps
differed from the earlier observation. The equality check was too strict for
fields PostgreSQL normally refreshes. Its narrow correction passed seventeen
remote saved-snapshot checks, then the
single actual transaction committed and its owned connection/lock closed.

The [startup review](audited-main-startup-preparation.md) records the explicit
filesystem prerequisite: an absent tmpfs cache path was exclusively created
empty as `goby:goby`, `0700`, without application markers or a service start.
At that checkpoint one unclosed diagnostic file contained 413 complete bytes and
required a registry closure update on startup; no truncation or current-age/count
pruning was expected. The then-installed `Restart=on-failure` required a finite
one-invocation policy before starting.
The later [material checkpoint](audited-main-recovery-materials.md) preserves
the old installation and closes the standalone key observer's preflight failure
without executing it. The later [startup-only closeout](audited-main-native-backup-closeout.json)
superseded those runtime prerequisites at that checkpoint: one source32 start
and owned stop were closed; capacity and the fence were installed, both cache
markers existed and all eleven diagnostic entries were closed. That input was paused
after two controller failures. The later
[reviewed correction](audited-main-backup-ownership-correction.md) used this exact
retained state and completed the native backup/source-key witness. Selected
online rollback and same-mount restart have subsequently closed through the
[A410/B419 checkpoint](isolated-selected-online-rollback.json). Actual source32
installation return/recovery/restart has since passed its [separate checkpoint](isolated-source32-recovery.json).
Final cluster/credential disposal has also [completed](isolated-restore-disposal.json).
Main promotion remains blocked by core video acceptance. The backup's
removed-fence state is retained in its original completed checkpoint.

The [replacement main upgrade contract](audited-main-upgrade-plan.md) requires
one fresh native archive and two distinct isolated restorations: new binary to
schema28, and actual source32 binary to schema27. Its source review identifies
an existing native HTTP backup-creation path; fresh authority, startup effects
and bounded ownership must be reviewed before using it. No main operation is
admitted by this draft or by candidate admission alone.

Global NextUp and automatic-refresh research remain parked. The
[matrix07 record](nextup-global-reference-matrix-07.md) contains 158 requests and
ten empty global results. Two [original-client discoveries](nextup-client-discovery-closeout.md)
issued no physical NextUp request. Existing preservation closures remain valid
for their original scopes; another run does not freshly reverify all 198 roots.
The [Goby comparison contract](nextup-goby-comparison-contract.md) requires new
discriminating evidence or a concrete product decision before reopening.

## Remaining release work

The selected feature implementations and scoped functional closeout are
complete. This table keeps the wider release obligations separate from that
completed wave.

| Area | Remaining acceptance obligation |
| --- | --- |
| Foundation and recovery | Main migration and bounded post-upgrade workflow against the admitted artifact/current state; isolated selected rollback, actual old-binary restoration and restart/disposal proofs are complete |
| Catalog and operations | Native root-profile restart, r04 nonroot two-start runtime, SQL catalog/isolation and tiny real-file scan/rescan baselines passed with closure; internal amd64 installation sealing is now accepted through supplemental final-state evidence; representative throughput, actual blocked storage, host reboot and filesystem measurements remain open |
| Playback | Complete pinned original-client journeys and profiles outside the accepted selected advanced-media contract; broader format, client and hardware claims retain their own gates |
| Administration | Native user deletion, diagnostics and the selected policy/settings/task source have their recorded acceptance. Provider code is integrated but provider-specific acceptance remains deferred; unsupported upstream fields and wider M5/deployment obligations remain open. Earlier frozen Programs scopes are not retroactively expanded |
| NextUp and refresh | Positive selector/ordering/client behavior and automatic-refresh evidence for those feature claims |
| Hardware | Actual GPU decode, encode and combined-path profiles |
| Packaging | Native arm64/OCI and deployed embedded-bundle/profile acceptance, support rows, project license and dependency notices; focused embedded tests and amd64/arm64 build artifacts are already recorded |
| M7 | Deferred until explicit feature selection and separate acceptance |

A partial internal deployment does not close M2-M6 or claim broad compatibility.
Missing hardware blocks that profile; it does not block unrelated software work.
License and notices must be resolved before external distribution.

The [native scan/HTTP preparation](native-scan-http-capacity-preparation.json)
has recorded the current remote environment and initial source checks. Its
reader's first synthetic protocol run failed four of ten groups because closing
an incomplete HTTP response required a missing file-like flush method. The
corrected reader passed twelve groups, including error preservation and
cancellation/context-exit boundaries; independent saved-evidence review passed.
Those checks used socketpairs and explicit namespace/context substitutes. The
integrated native controller, real workload and resource closure remain open.

The [JavaScript/glyph readback](systemd-package-javascript-attribution.md)
records the 46 emitted chunks' import graph and 73 exact single-path glyph
correspondences to identified MUI package files. The subsequent
[fixed-commit source lookup](systemd-package-mui-source-correspondence.md)
matches all 73 recorded path literals and labels; its 56 public modules also
match the cached modules byte for byte. The 56 raw SVG candidates retain only
a naming/source-layout association, with no exact path-literal match or claimed
historical transformation. Two exact saved build reports supplied no source
graph reference; this does not establish that the entire host lacks one.
The [initial legal payload draft](systemd-package-legal-payload-draft.md)
selects 19 existing Go module/runtime and font texts with explicit destinations.
Both drafts received independent static review. Complete source-module
contribution, historical Google glyph inputs and final package assembly remain
open. The subsequent required M5 frontend build captured its own bundler
observations; no unchanged E11 rebuild or external distribution occurred, and
the new sidecar does not supply historical E11 contribution evidence.

The [separate frontend capture preparation](https://github.com/moooyo/goby/blob/43a9b76a93615b7f872f48dae4e9b3a73ca050b0/docs/development/frontend-contribution-capture.md)
is now retained on `codex/m6-frontend-contributions` at `43a9b76`, in
`D:/Code/goby-frontend-provenance`. It adds a Vite build-only private report and
an explicit release-script input with complete asset equality and final
source/copy checks. JavaScript assets must partition into recorded chunk names
or an explicit unattributed list. Its [seven producer top-level tests/eight
subcases and five synthetic CLI tests/nine subcases](frontend-contribution-verification.md)
passed once per group in bounded remote units. Those synthetic scopes did not
typecheck or build the application. The CLI cases use fake Go
and deterministic source/copy mutations, not product builds. Static review findings
were corrected in source. The plugin records a bundler observation, requiring a
separate successful command result; it does not claim final per-module byte
shares or legal completeness. The implementation and test sources are now
cherry-picked into `codex/m5-user-deletion` at `4796aa1`; its documentation
references the actual synthetic results. The [later real M5 frontend build](m5-final-frontend-verification-20260916.md)
has now passed typechecking and Vite execution once, with 59 assets and an original
sidecar covering 48 chunks/48 JavaScript assets. Its source and artifact mapping
passed independent review. Actual release-script consumption remains pending and
must use `--frontend-contributions`; static saved-data compatibility does not
execute that consumer. The integration itself ran no build, and the later M5
build does not rebuild or change the frozen Programs increment.

Static review of the separate deletion and media-diagnostic branches identified
one required integration hook: after a successful user deletion commits, notify
the diagnostic owner with the deleted user's ID. Its existing authority watcher
eventually detects deletion, but does not replace immediate cancellation at the
committed DELETE boundary. The [combined source](m5-combined-source-preparation-20260916.json)
at `138522b` now implements that hook and a regression that freezes watcher
authorization, observes immediate cancellation, retains the reservation through
failed closure and preserves peer jobs, processes, caches and sockets. Focused
independent static review passed; actual combined behavior remains unverified.

The [initial OCI source profile](https://github.com/moooyo/goby/blob/18cd4efd117f3314cf5cb46b4736f65795bbdf59/deploy/oci/README.md)
is committed and pushed at `18cd4ef` on `codex/m6-oci-package`, in
`D:/Code/goby-oci-package`. It defines a fixed Linux amd64 software recipe,
FFmpeg/ffprobe 9.0.1, PostgreSQL 17 clients, a selected-release file checker and
the nonroot/read-only Compose/storage/signal contract. The [12 checker methods](oci-artifact-checker-verification.md)
passed once in a bounded remote unit, with unchanged source and closed resources.
Registry/snapshot metadata and the small header-source archive were read; that
archive was hashed remotely. No OCI image was built, pulled or started, and no
Compose or deployed profile was verified. The synthetic checker result does not prove package resolution, runtime
dependency closure, reproducibility, media/backup/stop behavior or legal completeness.

## Verification policy

All compilation, formatting tools, tests, browser checks, media probes and runtime
verification use `ssh test-env`. Local verification requires explicit permission
in the current task. An unavailable test environment blocks verification; it
does not authorize a local fallback. Reuse unchanged verification and limit new
checks to the changed risk. Future production changes require relevant focused
checks, including browser checks where applicable, followed by full regression
of the final intended source.
The fixed media-refresh increment has completed these gates within its recorded
scope; it does not waive them for later production changes.

## Supported capacity boundaries

Missing-item reconciliation currently requires a complete proof within 4096
directory handles, 262144 entries, 131072 seen IDs and 64 MiB of observation
storage. Its SQL candidate/closure budget is separately 32768 rows and 32 MiB,
with per-row overhead that can reach the byte limit first. These budgets cover
the library, not only the deleted files. Exhaustion retains missing records and
reports a skipped cleanup while additions and updates may still complete.

Global NextUp query cost and concurrent scan/homepage latency have not been
accepted at a representative large-catalog scale. Neither a small API page size
nor the passing correctness suite establishes that capacity. Existing events
are not durable; supported clients must refetch state after reconnect.
