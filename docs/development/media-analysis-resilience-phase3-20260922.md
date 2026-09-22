# Phase 3: Large-library concurrency and fault/restart recovery

Status: **source implementation integrated; consolidated remote verification in progress; publication pending**.

This record covers Phase 3 of the
[approved three-phase plan](../planning/media-analysis-resilience-plan-20260920.md).
Phase 2 is verified, closed and published: delivery `feb5004` and publication
metadata `e41febbb36687d04340f5c651f4bf1bf376a4310` were merged, pushed and read
back. Those records establish the starting provenance; they are not Phase 3
verification. The overall three-phase objective remains incomplete.

Implementation uses the isolated `codex/media-analysis-resilience` checkout.
Unrelated changes in the original checkout remain outside this increment.
Complete the phase's implementation and integration before its consolidated
remote verification. No Phase 3 source candidate, workload profile, threshold
set or result is frozen by this document.

## Implementation in progress

| Workstream | Current state | Evidence boundary |
| --- | --- | --- |
| PostgreSQL temporary Seen staging and ownership | Source implementation and scan-path integration written. | Session-private staging, batch/physical limits and ownership retirement require remote verification. |
| Disk spool generation-handle identity and fallback | Source implementation and scan-path integration written. | Exact directory evidence, Linux export handles, bounded held-FD fallback and cleanup require remote verification. |
| SQL paging and configuration/store integration | Source implementation written. | Unseen candidates use keyset pages; a bounded positive deletion closure remains atomic. Explicit spool configuration, owner manifest, resource observations and failed-cleanup shutdown repair are integrated; verification is pending. |
| Consolidated workload and recovery acceptance | Compound/fixture, fault/transport, state/probe/oracle, regression and actual HTTP overload sources are integrated. Private runtime/PG/observer/export sources are also complete. | Exact source/tool/build/fixture references and resource admission remain to be bound. No Phase 3 product test, build, benchmark or fault/reboot acceptance run has started. |

These are workstream status observations, not completion claims. Source-only
design and review do not establish bounded resource use, concurrency, fault
recovery or a passing runtime result.

The core product and Go test sources are saved in branch commit `40d1f0a`.
The [scan-evidence runtime contract](scan-evidence-runtime.md) documents deployment,
fixed budgets, recovery boundaries and the native runtime-resource endpoint.
This includes the cleanup-failure shutdown repair: after actual work and owned
transactions finish, a failed evidence owner detaches its retained PostgreSQL
session from the pool while preserving its database and filesystem fences until
process exit. Pool shutdown no longer waits for an intentionally retained
checkout. Three focused PostgreSQL regression sources cover this failure path.
They have not run. This commit is not the complete Phase 3 verification freeze
and has not been merged or pushed to `main`.

Follow-up `6316e61` bounds native root-binding filesystem observations, including
their actual closes, without holding a database transaction or store mutex.
Capacity/deadline exhaustion becomes 503 after current authority and row checks;
ordinary unavailable storage retains its prior binding projection. The same
follow-up adds actual in-memory pool contention counters to the resource DTO.
Its new lifecycle, current-authority and real-pool-wait test sources remain
unexecuted.

The consolidated regression runner is now source-complete with independent
static review. It discovers every ordinary Go package, builds the frontend and
both application variants, includes embedded command tests and all Phase 3
Python test sources, and requires seven distinct owned PostgreSQL databases.
Actual missing credentials cannot turn required database coverage into a silent
skip. Runtime setup, fixture and recovery-oracle sources are integrated; exact
release values must be bound before this runner may execute.

The actual HTTP overload source complements each tier's full workload. It holds
eight same-user original-response leases, requires the ninth request's exact
429 response, observes another user's successful allowance, then checks recovery
and closure. It uses already indexed licensed media without sparse extension.
It does not substitute for decoding, throughput or the cold/cached/incremental
capacity journey.

Each fault scenario will receive a separate immutable runtime/context export
from the actual current service and PostgreSQL lifetimes. Reboots cannot reuse
old PID bindings. Individual controller successes retain `partial`/exit-2 status;
the complete declared fault matrix must be composed from independently bound
case receipts for each tier. No single-case result accepts the whole matrix.
All cases except ENOSPC use the persistent normal analysis cache. ENOSPC follows
a separately recorded native configuration withdrawal of old derived references,
normal service closure, preparation of the owned derivative volume, and startup
of its actual cache consumer. These setup changes precede that case's baseline
ACKs. The settings sentinel changes the managed server name rather than
invalidating media-analysis work during fault admission.

## Required scope retained from the approved plan

| Work | Required delivery and acceptance |
| --- | --- |
| Capacity contract | Use both 10,000 and 100,000 catalog-item tiers. Separately record actual media counts, bytes, formats and scan work. Pin CPU, RAM, PostgreSQL, storage and concurrency; establish an idle baseline and freeze service/resource thresholds before acceptance. |
| Mixed workload | Combine cold, cached and incremental scans with Unicode search, filters, exact counts, shallow/deep paging, Resume/Latest, direct playback, remux/transcode, seek, intro analysis and preview generation. Reuse one controlled environment and corpus while recording distinct workload conclusions and proving actual operation overlap. |
| Sorting and metadata concurrency | At both catalog tiers, include sorting-policy rebuilds and simultaneous metadata editing. Measure rollback and owner survival on statement timeout. These are part of the mixed workload, not optional follow-ups. |
| Resource engineering | Use actual query plans, pool waits, CPU, memory, I/O and child-process observations to address indexes, query shape, queue admission and isolation. Preserve playback priority and background fairness; overload must have bounded queues or rejection. |
| Storage faults | Inject blocked read/metadata operations, mount loss, changed root/nested mounts, permission failures, full storage and PostgreSQL disconnect/lock waits. Inaccessible storage must not be treated as missing files or cause bulk catalog removal; healthy roots remain serviceable. |
| Recovery correctness | Restore original storage while preserving item identity and user state. Require explicit rebind for replacement storage. Distinguish request timeout from actual worker termination, and verify resource return, partially published work and temporary-output cleanup. |
| Process/database/guest restart | Separately exercise process crash, PostgreSQL restart, clean guest OS reboot and forced guest reset with external observations. Cover late mounts, readiness, interrupted task state, orphaned derivatives/processes, acknowledged durable state, reconnection and resume from persisted playback progress. |
| Final composed regression and recovery | Report latency distributions, failures, playback start/seek behavior, resource peaks, supported concurrency and recovery time for each admitted profile. Complete integrated regression and migration/backup/recovery checks for every new state category, close owned resources and retain original failures and evidence. |

Catalog-item scale is not a claim that the same number of real media files was
decoded. A service restart does not establish power-loss durability, and an HTTP
timeout does not establish termination of an uninterruptible filesystem syscall.
Unavailable guest reboot/reset evidence remains pending rather than reducing
the approved scope.

## Isolated environment preparation and retained bootstrap failure

Read-only PVE preparation has completed. The first clone attempt for the new,
owned VM `106` failed after the controller's 60-second timeout; its UPID records
failure. A `qemu-img` child initially remained after that controller failure and
subsequently exited naturally. Its later exit does not turn the failed clone
into a successful VM or bootstrap result.

This is an **environment-bootstrap failure**, not a Goby product regression.
No Phase 3 product workload ran as part of that failed attempt. No existing VM
was modified, and neither shared `test-env` nor the physical PVE host was
rebooted. Keep the original controller/UPID failure and child-lifecycle evidence.

The exact partial guest and its three identity-bound volumes were subsequently
cleaned up after the original child exited. Clone02 completed successfully under
an independent systemd unit, with independent process closure. The new guest
received task-specific key-only SSH access and an independent machine identity.
Its one identity-bootstrap reboot is infrastructure preparation, not product
reboot acceptance. Original failure and cleanup records remain retained.

VM106 is now provisioned with 2 vCPU, 3 GiB fixed RAM, a 33 GiB disk and no swap.
Dependency preparation is complete: PostgreSQL 17.11, Go 1.27.1, FFmpeg/ffprobe
9.0.1, the pinned fingerprint helper, Node 24.20.0 and the fixed browser runtime
are available. No PostgreSQL workload cluster or Goby workload was started by
that preparation. The last dependency snapshot recorded 29,105,045,504 free
filesystem bytes and 2,563,784,704 available memory bytes; these are historical
observations, not future admission guarantees.

All eight pre-existing guests and the template retained their recorded
identities. Preparation workers are closed; VM106 intentionally remains running.
The external controller transport is ready, but reset dispatch remains disabled
until the exact workload, artifacts and scenario are frozen. Full workload
admission is still pending. Private records bind the guest owner, machine and
storage identities, tool hashes, failed history and independent closures.

All tests, builds, validation and runtime probes remain remote-only. Ordinary
checks use `test-env`; clean reboot and forced-reset acceptance require the
isolated owned guest. Shared `test-env`, other existing VMs and the physical PVE host
are outside reboot/reset scope. Local verification is not authorized, and
`ui-ux-pro-max` remains disabled. This documentation update performs no SSH,
environment mutation, test, build or runtime probe.

## Next work and closeout state

Freeze the integrated candidate and bind the release values for the completed
runtime/fixture/oracle sources. The fixture producer must establish real item
identities and frozen expected results, and the recovery checks must bind actual
blocked work, resource return, explicit replacement-storage rebind and durable
playback state. Record the complete candidate,
environment/corpus inventory, both scale-tier profiles, thresholds, fault
injections and protected ownership boundaries before consolidated verification.

Verification, resource closure and Phase 3 publication are pending. Preserve
each failed attempt and bind any repair to its own affected verification; do not
relabel earlier evidence as a later pass. The final record must identify exact
source/artifact identities, selectors, real workload overlap, resource peaks,
recovery outcomes and remaining limitations. Only the verified delivery may be
merged and pushed, with exact remote-ref readback; publication remains distinct
from deployment.

## First consolidated verification attempt

The complete source was frozen at `46c2747e3cc148141db13cd2fe05c42c95b899a3`.
Remote source staging verified all 6,720 canonical Git archive files before and
after compiling 27 Python sources, with bytecode written outside the frozen
tree. The staging process and cgroup closed independently. Seven isolated
PostgreSQL databases were then provisioned; the setup process closed while its
dedicated PostgreSQL remained available for regression. An initial setup-closure
collector assertion confused `INVOCATION_ID` and `_SYSTEMD_INVOCATION_ID`;
that collector failure remains preserved separately from successful provisioning.

The first full regression worker failed before any test, npm command or build
ran, at `frozen_source_inventory`. All 6,720 paths, byte counts and hashes still
matched; 28 array positions differed. The runner sorted `Path` components while
the manifest sorts complete relative POSIX path strings. For example,
`cmd/goby-notification-receiver/main.go` and `cmd/goby/dashboard_assets.go`
received different relative ordering. This is a verification-runner defect,
not a product source mismatch or a Goby regression result.

The repair uses explicit relative POSIX string ordering and adds a focused
regression source with that actual prefix/directory pattern. The failed run is
retained, with zero test/build stages accepted. Its fast exit also exposed the
independent observer's inability to bind a worker before the first polling
sample; that failed observer result remains separate. A successor must bind
the launch identity, preserve transient observation gaps, and use a new immutable
source/profile/context. The untouched seven databases can be reused only with
their original service and database identities rechecked; setup is not repeated.

## Second regression attempt and PostgreSQL resource failure

The inventory repair is `ad02b1223dc0aeef9adc27cea4fc95bbd5ca1467`; product Go
inputs are unchanged from the first candidate. Its complete source staging and
syntax checks passed. Eight Python scripts passed all 58 tests. Frontend
installation/build and both ordinary/embedded Go application builds passed.
The first six Go package scopes completed with 141 passing parents; the
notification receiver has no test files and is a package-level skip.

The next package, `internal/backuppg`, recorded 103 passing and 30 failing
parents. Its first failure was
`TestPostgreSQLPhase3MaximumManagedArtworkDumpValidatesAndRestores` at the
snapshot-fingerprint step. The kernel recorded a PostgreSQL memory-cgroup OOM
at the same time, under the 384 MiB test-database cap. Later database-dependent
failures remain recorded; they do not independently establish product defects.
All remaining 28 package scopes and the embedded command tests were unstarted.

The same maximum-size artwork case has a
[historical 1 GiB PostgreSQL result](amd-media-phase3-20260919.md), following a
512 MiB failure. Its earlier PostgreSQL peak was 756,084,736 bytes, and the
relevant fingerprint/fixture implementation remains unchanged. The current
384 MiB regression profile is therefore insufficient for that supported case.
The worker's last live counters showed memory reclaim but no worker OOM; they
are not substituted for unavailable final counters after cgroup removal.

The second worker, observer and PostgreSQL processes and cgroups are closed.
PostgreSQL stopped because of OOM, not a clean shutdown. All seven databases and
original evidence are retained, including the consumed backup pair. Source
bytes were rechecked unchanged. The
[results ledger](media-analysis-resilience-phase3-results-20260922.json) keeps
the original failure, passing scope and remaining requirements separate.

The proposed successor redistributes the same aggregate 2,304 MiB cap across
PostgreSQL (1,024 MiB), the serial test worker (1,152 MiB, `GOMEMLIMIT=800MiB`)
and the observer (128 MiB). This is a candidate, not an accepted worker profile.
It preserves the successful builds, Python and complete Go scopes, uses a fresh
pair for the entire failed backup package, then runs every previously unstarted
package and embedded command tests. The previous failure is never relabeled
successful. Actual resource admission, database recovery and this successor's
results remained pending at that checkpoint.

## Live regression successor and preparation repair

Successor03 is now running from the immutable `ad02b12` candidate. The original
PostgreSQL cluster completed WAL recovery with its system identity and seven
database identities preserved. Four unused recovery databases were checked
empty, and a separate fresh backup pair was created; all nine owned databases
remain retained. The setup process closed independently before the test worker
started. PostgreSQL has a 1 GiB cap, the serial worker has 1,152 MiB with
`GOMEMLIMIT=800MiB`, and the observer has 128 MiB. The aggregate limit is unchanged.

The retained-build receipt binds the actual ordinary and embedded binaries and
all 73 frontend assets. It remains a build-only receipt and does not relabel the
failed regression. Collector failures for two metadata file modes and one
previously undeclared generated frontend report are preserved, with their exact
repairs and independent closures. No passed build or package was replayed.

At the latest live observation, the complete backup package, including the
previously failing maximum-artwork case, and package scopes 07 through 15 have
passed. The library package is active. Six complete scopes from attempt02 plus
these ten new scopes cover 16 of the 35 ordinary package scopes so far; one of
the original six has no test files. This is live progress, not complete
regression or resource acceptance. The remaining ordinary packages and embedded
command tests must finish, and final observations and closures remain required.

The independent `34344c8` preparation repair reconciles the restored filesystem
with the accepted compound run's incremental catalog before creating fault
fixtures. It preserves the original exact-tier result, verifies the one new and
one moved item, and records a cached-only handoff. Population digests hash each
row first, then the ordered row hashes, with full count guards before aggregation.
The repair changes three preparation source/documentation/test files only;
production Go, frontend and build inputs remain unchanged. Its remote checks and
actual handoff have not run. The live regression source is not modified, and
later binary reuse must retain the real `ad02b12` build provenance explicitly.

## Library regressions found during successor03

The complete library package subsequently finished with 944 passing parents,
three failed parents and one original opt-in mount-namespace helper skip. The
failed parents are:

- `TestScanReconciliationStagedPagingExcludesLargeAcceptedPopulationBeforeLocks`:
  the final reconciliation returned `context deadline exceeded` after the
  40,000-row fixture; the full test took 50.54 seconds. The earlier accepted-row
  lock check was not the failing assertion.
- `TestScanReconciliationSpoolFallbackRejectsReplacedAndRestoredChains`:
  the restored-chain subcase incorrectly authorized absence after a rename
  round trip. The replaced-chain subcase passed.
- `TestScanReconciliationSpoolRejectsLinkedAndSpecialRecordFiles`:
  the symlink subcase accepted a linked record as private evidence. The FIFO
  subcase passed.

The original events, summary, stage receipt, skip reason and nearest resource
observations are retained. The observed worker, PostgreSQL and observer memory
events report no OOM; these failures are not attributed to attempt02's OOM.
The resource samples bracket the failure window and are not per-assertion
measurements. The original runner continues its frozen remaining package order.
At the next checkpoint, package scopes through 26 were complete, with only
library failed, and server scope27 was active.

Source repairs are in progress. Seen is a session-private temporary table and
had no explicit statistics refresh before the exclusion query. Its repair adds
`ANALYZE` at successful sealing without changing the SQL/proof deadlines, the
40,000-row population, or lock/deletion/resource assertions. The original
execution plan and individual timing contributions were not captured, so the
missing statistics are a confirmed source defect rather than proof of the sole
50.54-second cause. The affected test now records the actual owner-session
plan and operation timings. Directory identity and private-record access fixes
must preserve bounded resources and conservative deletion authority.

The previously prepared `ad02` to `34344c8` build-reuse bridge remains unexecuted.
It covers only the preparation-script change and cannot authorize binaries for
these new production Go repairs. After all repair source is integrated, freeze
a new candidate, run the affected remote verification and rebuild the actual
application artifacts. Capacity and fault/reboot acceptance remain unstarted.

## Complete successor03 and integrated repair source

The original successor subsequently completed all 29 new ordinary package
scopes and the embedded command scope, then exited with failure as required.
Its composed ordinary summary reports 4,251 passes, five failures and 18 skips;
embedded command tests report 24 passes without failures or skips. Original
per-scope evidence and skip reasons remain authoritative. Independent final
resource closure is being collected; these counts do not accept the phase.

The two additional failed parents were
`TestConfigurationCompatibilityMigrationPreservesEverySchema20Field` in
settings and `TestManagerOwnerLossFencesWritesAndRecoveryDoesNotResumeOldRun`
in tasks. The former retained a pre-schema50 table inventory and compared new
analysis admission keys as historical fields. Its fixture now includes all
eleven analysis tables, proves the six excluded new task keys did not exist in
schema20, checks their exact defaults, and retains every original field's value
comparison. Repeated migration must preserve the complete analysis-settings
row. No published migration or production settings behavior was changed.

The task fixture acknowledged a backend-termination signal before proving the
old session and its advisory locks had disappeared. It now binds the reserved
owner backend through an actual owned transaction, uses the existing bounded
backend-termination API, and separately confirms backend and lock absence. The
old probe remains blocked through this transition; all late-writer fences and
the prohibition on resuming the old run remain tested. Production ownership,
manager shutdown and failed-cleanup quarantine are unchanged.

The integrated source also refreshes sealed Seen statistics, opens private
records with kernel no-follow semantics, and records bounded directory-change
history before raw enumeration. Native and fallback paths share that history
witness. Overflow, registration failure and unsupported notification semantics
cannot restore deletion authority. The [runtime contract](scan-evidence-runtime.md)
states the filesystem and resource bounds, including the unchanged positive
scan behavior where reliable namespace history is unavailable. The fixed
descriptor reservation includes both the change queue and record-opening
scratch directory. Source review corrected those two accounting omissions.

These repairs have been formatted and statically reviewed only. Their affected
remote tests, new application builds, complete capacity profiles and fault
matrices remain pending. The original five failures are not relabeled as passes.
