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
