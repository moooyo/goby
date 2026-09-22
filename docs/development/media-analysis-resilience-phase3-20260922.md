# Phase 3: Large-library concurrency and fault/restart recovery

Status: **repair regression/builds and independent closure passed; failed-capacity evidence is externally archived; fresh full capacity and fault/recovery acceptance remain pending; Phase 3 is unpublished**.

This record covers Phase 3 of the
[approved three-phase plan](../planning/media-analysis-resilience-plan-20260920.md).
Phase 2 is verified, closed and published: delivery `feb5004` and publication
metadata `e41febbb36687d04340f5c651f4bf1bf376a4310` were merged, pushed and read
back. Those records establish the starting provenance; they are not Phase 3
verification. The overall three-phase objective remains incomplete.

Implementation uses the isolated `codex/media-analysis-resilience` checkout.
Unrelated changes in the original checkout remain outside this increment.
Product/build source `1ed1d69` and fixture successor `00af9e4` have actual remote
evidence. Exact profiles, contexts and receipts remain bound in the private
checkpoint and delivery ledger; this document does not itself admit a workload.

## Current checkpoint

Scope03's temporary preparation limit was restored and its runtime closed after
diagnosis identified missing `io.stat` accounting. Scope04 uses runtime template09
with `IOAccounting=yes` and has completed actual preparation and independent
worker closure. This resolves a deployment prerequisite, not a Goby product
defect. Goby's original benchmark limit is restored and all preparation controls
are closed. The [capacity checkpoint](#capacity-preparation-checkpoint) records
the actual fixture, restoration and preserved service lifetimes.

The latest composed regression has **4,263 ordinary Go parent passes, zero
failures and 18 explicit skips**, plus **24 embedded command passes** and
**70 Python passes**. Frontend and both application builds passed. Original
failures and skips remain in their source-bound records. Preparation is not
capacity acceptance: the full 10k compound journey failed during cold, while 100k,
overload and all 28 fault/recovery cases remain unrun; `accepted_capacity` is false.

The first actual compound publisher was rejected before source publication or
business dispatch. Its minimum observed MemAvailable was 2,300,395,520 bytes;
the App/PG-only incremental estimate required 2,304,077,824 bytes, a 3,682,304-byte
shortfall. Disk, inode, CPU, total-memory and no-swap checks passed. This is an
admission failure, not a Goby workload result. The publisher exited with status
1, its original process/cgroup and control parent are closed, and the prepared
data and original App/PG lifetimes remain intact. No resampling or retry occurred.
Admission SHA-256: `29131d0adb8e4c312d7059e8c1fe36b7de49b16becf7fea453c2d5678e744806`;
independent closure: `9b26d06eeb441f00ae367038de39202cea2f23b84679a812138d6c0d01b334ae`.

The original estimate omitted resident control memory, but no simultaneous
control counters were captured, so the refusal cannot be proved incorrect or
attributed to the outer SSH/dispatcher. The independently recorded successor uses
an explicit new policy: count disjoint App, PostgreSQL and shared-control
resident candidates once, subtract one aggregate 64 MiB operational reserve,
and observe again in the final launcher after exec. This policy is not equivalent
to the old per-role rule, is not an atomic or guaranteed headroom calculation,
and cannot retroactively accept the failed attempt. Role caps, VM resources,
disk floors, performance thresholds and the full workload scope stay unchanged.

Publisher02's final three-sample admission passed after its exact caller exited
and the observer became ready. Minimum MemAvailable was 2,316,333,056 bytes,
against a 2,309,144,576-byte estimated remaining requirement. App, PostgreSQL
and shared-control minimum resident candidates were 37,773,312, 116,379,648
and 19,730,432 bytes; the aggregate reserve remained 67,108,864 bytes. All disk,
inode, CPU, memory and no-swap gates passed. This is a new-policy admission,
not retrospective acceptance of publisher01 or a claim of guaranteed headroom.
Admission SHA-256: `09896f50b7c989d314d9a797d92be570313b0f643cd5df287856a8f887d5c697`.

The unchanged full driver actually started as PID `165428`, start ticks
`4517597`, invocation `f84abce06ce5400f9af15ae441fae93e`; observer PID `165360`,
start ticks `4517486`, invocation `56607cc24a374118852182dcc569913d`. The publisher
exited successfully. The later failure and closure below supersede those live
handles; neither dispatch nor a partial stage accepts the tier.

### Actual cold failure, diagnosis and source repair

The Actor exited 1 during cold with `http_status`; cached and incremental did
not complete. Three playback preparations returned `503 playback_timeout`
after approximately 20 seconds. Analysis admission, settings and scan requests
subsequently reached the client's 90-second transport deadline. Their start
times were within approximately 6.4 ms: late completion is not late dispatch.
The editing lane never completed its first settings GET, so its sorting rebuild,
metadata edit and deliberately forced statement timeout had not started.

The failed observer also had a separate confirmed defect: its new disk-peak
loop overwrote the worker-state dictionary with an integer. Original gaps and
source were retained; the monitor was explicitly stopped after the Actor ended.
Its later generic OSError gap did not preserve enough detail to classify it.
No successful resource-observation result is claimed. The isolated source
successor changes only that local variable name and still requires remote use.

A read-only reconciliation found no current scan, storage-observation or stream
work and no matching committed analysis/playback/encoding records. That does
not recast the original unknown POST outcomes as never accepted. The original
catalog owner was backend `114170`; its PostgreSQL log records an idle-in-
transaction timeout at 09:39:56.250 UTC, thirty seconds after the concurrent
requests began. A deliberately terminating SIGQUIT then captured the original
Goby instance's blocked stacks. The analysis admission held ownership.mu and
waited for Store.mu in Available; the scan admission held Store.mu and waited
for ownership.mu. The settings request was also blocked on Store.mu. These are
actual stacks of the same Store, not solely a source-level possibility.

The repair makes Available observe the immutable ownership reference and atomic
closing/lost state without acquiring Store.mu. Close already sets closing
before closed, so the mutable closed field is unnecessary for this observation.
Owned write admission and session fencing stay in place. A deterministic
integration regression holds the admission mutex while an owned callback checks
availability, then verifies commit and ownership reuse. This new code and test
now have remote focused verification. The old ownership implementation failed
the new case after the expected five-second mutex wait; the repair passed in
0.20 seconds and with `-race` in 0.22 seconds. Both actual Go binaries built.
These completed stages have individual exit/child-closure receipts.

The following full library package was interrupted after 696 parent passes and
one skip, without a package terminal result. Its independent observer retained
a Python tuple for every inode and hit its 128 MiB memory cap. The observer was
OOM-killed; `BindsTo` then stopped the worker. The unloaded worker unit's default
exit fields are not a passing test result. Both original processes/cgroups are
closed, and the failed output is retained. A source-only observer successor uses
native `du` byte/inode accounting with the same caps, reporting failed reads as
gaps. The continuation must rerun the entire library package and complete tasks,
settings, server, command and embedded-command scopes with a fresh database.
It may retain the independently completed focused/race/build stages.

The fresh-database continuation completed all five packages and embedded command
tests: library 955 passes/one skip; tasks 92 passes; settings 55 passes; server
1,013 passes/four skips; command 24 passes; embedded command 24 passes. All new
stages exited successfully with their children closed. The observer stayed
within its 128 MiB limit with no OOM or persistent loss. Six transient allocation
samples raced PostgreSQL temporary-file removal; their failures remain recorded,
not zero-valued samples. Independent closure verified the real worker/observer
exit identities and clean PostgreSQL shutdown, retaining all twelve databases.

The composed ordinary regression is now 4,264 passing parents, zero failures and
18 explicit skips across 35 scopes; embedded 24 and Python 70 are separate.
The partial interrupted library output was replaced by a complete new run.
Frontend reuse binds unchanged tracked inputs and the full original artifact
inventory; both Go binaries were actually built at `5fb968a`.

| Repair verification | SHA-256 |
| --- | --- |
| Complete affected continuation | `e8b28b1bcabdb09829ed8e739f1affa4d38e9170edfd8771601051e6a57e8000` |
| Independent observer result | `afa5ef836a517a366bf5732315cd4ec9d07cbc07f808fc18cedaf6c3e37dc3cc` |
| Independent worker/observer/PostgreSQL closure | `e5d66d4a799df49ff5daeaf0781eaa132901be26f5c60edcd73e04a7176573ba` |
| Setup-compatible build delivery | `a152e8407e88e7ee57a6a01f2b19e4ec79a037e6e90aae051b21f4187c07f8d0` |

Failed scope04 fixture, preparation output and compound output were archived
with all 348,699 original path/stat records and 1,673,799,971 source bytes.
The independent PVE readback matched every member and all four complete file
hashes, then synced files and directory. Guest archive, transfer and readback
workers are closed. The external receipt is
`c9b123394be6b39b2b81a9b7e64ccd8f9475f8f73d81cd1afeaa451f9082a42b`;
its independent closure is
`68f2aa733037a05c36fd31ebf7eeedce65edbd07347e2a004f03dbcd25fdfb1a`.
Exact guest retirement and regenerable build-cache cleanup completed. Source
bytes were compared with the verified manifest before retirement; all hardlinks
were confined to the archived tree. Eleven exact trees and the two large guest
archive files are absent, with protected databases, credentials, diagnostics,
source snapshots, build artifacts and external originals retained. Independent
closure recorded 22,229,053,440 free bytes, above the unchanged initial10k gate.
This snapshot does not replace fresh admission for the pending scope05 profile.
Retirement receipt: `033f5f301e5b32096e1e1ac94efd90c98bf6e305a1431ac4b008cdb6ac3fc8a9`;
independent closure: `9d7bbc21c1a6df71ed685dbe0587b1039410cc11ebf50b19af5d13013330090f`.

Goby's diagnostic exit 2 is not graceful shutdown or a fault-matrix success.
After it exited, a read-only PostgreSQL snapshot found no other clients and no
surviving matching admitted work. PostgreSQL then shut down cleanly; both retired
runtime units were disabled. Original processes/cgroups and control parents are
absent, with all databases, fixture files and failure evidence retained.

| Failure and closure evidence | SHA-256 |
| --- | --- |
| Closed failed Actor/observer controllers | `1973308265b2ad2a3cf4c20440a1db3bafb14577febdd5b03315decd4be8edcd` |
| Independent helper/control-parent closure | `b9a834caefd9bf8be5915d18ede1835adf009bc4f43f423325b47b373cda9627` |
| Owner observation and matching PostgreSQL timeout | `615a1d7bf45a98a54658a4ab56fd7f4a14fc923be8742292809caee66c9fa9d8` |
| Screened actual blocked-goroutine record | `89b153b3e36d2c711ca5e73933892760592cb49cdd8d19e759f17912a4f63d2d` |
| Post-Goby read-only database state | `c6db7435e812aa747a91ae36ddca7c45dab1c4b7e46688f78dc23bfa7c5ef72d` |
| Retired Goby/PostgreSQL runtime closure | `a9141da68c25eb7cf52a0d44b0918d945f413b1f97910e79b498cabdf53f4ba1` |

## Implementation history before consolidated verification

This section records the earlier source-only checkpoint. Its unexecuted-test
statements are historical and are superseded by the current checkpoint and the
later source-bound regression results below.

| Workstream | State at that checkpoint | Evidence boundary at that checkpoint |
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

## Capacity preparation checkpoint

This section preserves the preparation checkpoint before the failed compound
run. Its service lifetimes are now closed as recorded above.

Scope04's prepared receipt records 9,342 initial
catalog items, including directories, and 658 pending media files. The frozen
cold/cached/incremental totals are each 10,000; those are workload expectations,
not observed compound results. All 14 licensed sources, 4,200 stress directories
and 336,000 distinct zero-byte nonmedia entries remain, with 79,695,000 raw name
bytes. The actual fixture has 5,780 media paths and occupies 1,800,376,320
allocated bytes. Its prepared receipt explicitly says `accepted_capacity:false`.

Same-invocation launcher, worker and observer journals establish successful
completion. Their original PIDs and cgroups, producer descendants and Actor-UID
processes are absent. The closure records the real context, inventory, owner and
manifest bindings without exporting the credential-bearing context. A prior
closure-collector metadata failure remains retained separately.

At that checkpoint, Goby PID `114162` / invocation `6831967b922841f5a188a00641b218f9`
and PostgreSQL PID `114049` / invocation `2646f886443040a1b60ceb48a3bc3efa` retained
their original running lifetimes. Preparation lowering and restoration both
passed. Goby's effective limit was restored to 1,280 MiB; PostgreSQL stayed at 512 MiB, both
with zero swap and no restart. The restoration helper exited successfully and
its original PID/cgroup are absent. The empty preparation control parent
`[27,146123]` was stopped and its cgroup is absent. All preparation controls are
closed; actual compound dispatch still requires fresh resource admission.
Closure-time free space is a dated observation, not a future admission guarantee.

| Scope04 evidence | SHA-256 |
| --- | --- |
| Actual prepared receipt | `4e3b289b479ecb6539e70cbe594a050ad4be08b45bdc1eb39946bb0c9badf6aa` |
| Independent preparation closure | `ad24681dfd23c95b110565537728010d8e83089a4bde15fc8977c7e54b7c5bca` |
| Prepared context bindings | `0ec6b644a145e1ad0e89b871376a48b48b6021a20f0a1bf773f1336229711611` |
| Benchmark-limit restoration | `be86459e7b22ab2f00c3ef935c06787a28427e1e9fb3c232451cde36a13617e4` |
| Restoration and control-parent closure | `41424adfbf70390e127855a99a2f4fc3c6349ddd02e4709539675727a889ff0d` |

### Retained scope02 failure, retirement and fixture repair

The fourteen-source licensed corpus transfer is accepted. Product, workload and
build provenance remain `1ed1d69`; fixture-only `0dacf3c` makes shared media
ancestors readable by the media group while retaining owner-only private inputs.
Deployment-only directory, diagnostic-store and PostgreSQL observer-role repairs
allowed the second isolated 10k runtime to start. They do not establish capacity
acceptance or weaken the product's database ownership checks.

The first actual fixture preparation failed before producing its prepared
context. Its 384 MiB producer cgroup was OOM-killed while creating the unchanged
directory-stress fixture. Kernel evidence recorded 24,408,064 anonymous bytes,
26,193,920 file bytes and 352,051,200 kernel bytes, including 351,860,200 bytes of
reclaimable slab. This establishes the dominant charge at failure; it does not
prove that slab can be reclaimed promptly. The partial fixture contains 220,397
entries and occupies 1,738,973,184 allocated bytes. The producer and observer
are closed; successful disk/lifetime observation does not turn the producer's
exit status 9 into preparation success.

The five licensed-library seed scans completed. Before clean runtime closure,
Goby had no active scans, task children, playback sessions, original-stream
leases or media-process descendants. Its memory snapshot contained 34,787,328
anonymous bytes and 717,717,504 file-cache bytes; this does not classify its
earlier peak. Goby then stopped successfully, PostgreSQL shut down cleanly, and
both units were disabled with their original processes and cgroups absent.
Databases, control records, the failed fixture and original evidence remain
retained. Full external archival is accepted: all 220,624 members and
1,649,703,901 source bytes were independently read back with their original
metadata, and the four external files and directory were synced. Both archive
workers are closed. Retirement of the two preserved source trees and the two
large guest archive files completed with 220,626 recorded removals. The worker
and its cgroup are closed; the exact four targets are absent. Databases,
credentials, control records, binaries, shared corpus and all external archive
originals are preserved. The independent snapshot recorded 21,962,846,208 free
bytes, satisfying the unchanged 21,676,163,072-byte admission floor for the
then-fresh scope03. This historical snapshot cannot admit scope04 or a later
operation. No capacity or fault/recovery acceptance follows from this cleanup.

Fixture successor `00af9e4` streams the exact inventory JSONL after its original
space precheck, releases the source row list after a successful synced write,
and avoids a second directory-sized list of Path objects. It preserves the full
inode identity set, fixture population, budgets and all benchmark thresholds.
The complete affected preparation suite passed remotely: 15 tests, zero
failures/skips, unchanged source hashes and an independently closed worker.
It replaces the previous ten-test scope; together with 55 unaffected retained
Python tests, the latest fixture composition has 70 passing tests. These changes
reduce retained Python objects but are not a measured resolution of the
kernel-slab OOM. Product and workload binaries remain unchanged.

The failed workspace and initialized database cannot be replayed as fresh input.
The later scope04 preparation above used a fresh runtime and an independently
recorded memory envelope. Restoration after producer closure is a prerequisite
for the original benchmark envelope; scope04 has now satisfied that requirement.

| Evidence | SHA-256 |
| --- | --- |
| Failed producer and observer closure | `8086ab394a88c6f5cc0c114a9af636652359c0ee52706283d127cdc4aae2882d` |
| Kernel OOM memory breakdown | `100b640c8112c490f5b8b45e5d73229e60c055d5c5333c49321b509b62070769` |
| Clean Goby and PostgreSQL closure | `09f407d1344cd4cb056b88462f36f2070a0675ea0280f565353a21873b2812ab` |
| Complete external archive readback | `4cf55d05f7b0244b2967ba0b6aebe1da96c2573ba42d8094097e51346518a733` |
| Fixture successor tests and closure | `c796c8f214805e1c10f03d1ac88fc98b73406fc5db001db74269e0452f014bdb` |
| Exact failed-fixture retirement | `d3732b58e3a7d9b33f8614d294d276ea0787f6d198e602cbfc707fbb8193e056` |
| Independent retirement postproof | `90bfdeca27fbfab638ad9bf54f6abfd3b303179e92a3b512dce7fe2de7e0deee` |

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

## Initial environment preparation and retained bootstrap failure

This section records the earlier environment bootstrap, not current service or
transport admission. Its free-space and memory observations are historical.

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

VM106 was provisioned with 2 vCPU, 3 GiB fixed RAM, a 33 GiB disk and no swap.
Dependency preparation is complete: PostgreSQL 17.11, Go 1.27.1, FFmpeg/ffprobe
9.0.1, the pinned fingerprint helper, Node 24.20.0 and the fixed browser runtime
are available. No PostgreSQL workload cluster or Goby workload was started by
that preparation. The last dependency snapshot recorded 29,105,045,504 free
filesystem bytes and 2,563,784,704 available memory bytes; these are historical
observations, not future admission guarantees.

All eight pre-existing guests and the template retained their recorded
identities. Preparation workers closed; VM106 intentionally remained running.
The external controller transport was ready at that checkpoint, with reset
dispatch disabled until the exact workload, artifacts and scenario were frozen.
Full workload admission was still pending. Private records bind the guest owner,
machine and storage identities, tool hashes, failed history and independent closures.

All tests, builds, validation and runtime probes remain remote-only. Ordinary
checks use `test-env`; clean reboot and forced-reset acceptance require the
isolated owned guest. Shared `test-env`, other existing VMs and the physical PVE host
are outside reboot/reset scope. Local verification is not authorized, and
`ui-ux-pro-max` remains disabled. This documentation update performs no SSH,
environment mutation, test, build or runtime probe.

## Next work and closeout state

Use the accepted repair/build delivery after completed exact retirement and fresh
storage admission to prepare a new capacity runtime. Retain all original failure
and archive evidence. Do not reuse the
retired service context. External-controller admission is required before later
external ACK, archive writes or fault operations, not for independent guest
work that performs no external write. Use the actual
prepared context for the complete 10k compound journey and overload; follow it
with the required post-compound reconciliation and the full fault/recovery matrix.
The 100k tier remains a separate required preparation and acceptance scope.
Recovery evidence must bind actual blocked work, resource return, replacement
storage rebind and durable playback state. Historical external free-space or
transport records cannot authorize a later ACK or fault operation.

Capacity/fault verification, final runtime closure and Phase 3 publication are
pending. Preserve each failed attempt and bind any repair to its own affected
verification; do not relabel earlier evidence as a later pass. The final record must identify exact
source/artifact identities, selectors, real workload overlap, resource peaks,
recovery outcomes and remaining limitations. Only the verified delivery may be
merged and pushed, with exact remote-ref readback; publication remains distinct
from deployment.

## First consolidated verification attempt

This and the following attempt-by-attempt regression sections retain their
historical checkpoints. References to live workers, pending tests or unfrozen
sources describe those earlier moments; use the current checkpoint above for
the latest results and service state.

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
closure and closure-helper postproof succeeded: the worker, observer, helper
and original PostgreSQL processes/cgroups are closed. PostgreSQL shut down
cleanly, its system identity is unchanged and nine databases remain retained.
Root reviewed the original composed result, all package scopes, failed events,
18 skip reasons and both closure records. These counts do not accept the phase.

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

These repairs are frozen at `1ed1d69b94548e5beb842906b6177664367310df`, with
6,723 canonical archive files. They have been formatted and statically reviewed only. Their affected
remote tests, new application builds, complete capacity profiles and fault
matrices remain pending. The original five failures are not relabeled as passes.

## Accepted composed regression and capacity handoff

Source-stage04 extracted the frozen repair but failed before starting tests
because its private wrapper named the preparation test incorrectly. Its failure,
source and partial external bytecode remain retained. Source-stage05 checked
all 6,723 canonical files before and after running the correct suite: ten tests
passed. Its worker, child group and cgroup closed independently. No PostgreSQL
or Goby service was started by that operation.

The subsequent PostgreSQL setup preserved the original cluster and nine database
and role identities, creating one fresh unprivileged ordinary test database.
An independent collector initially compared OID JSON strings with integer
receipt fields. All ten actual identities matched; the failed collector remains
retained. A distinct collector with two explicit bigint casts established the
same successful setup's closure without repeating setup or altering a database.

Affected regression04 executed all ten declared stages from `1ed1d69`. Frontend
installation/build and both ordinary/embedded application builds passed. Full
library, settings, tasks, server and command packages passed with respectively
954, 55, 92, 1,013 and 24 parent tests. The embedded command scope passed 24
parents. All five original failed parents now have explicit passing evidence.
The 40,000-row paging parent took 10.51 seconds overall, without relaxing its
internal SQL/proof limits; its recorded operation timings and actual owner
session plan remain distinct from whole-test duration.

The complete regression composition retains 30 unchanged package scopes from
the original source-bound result: 2,125 passing parents and 13 explicit skips.
Replacing the five affected scopes adds 2,138 passes and five explicit skips,
yielding **4,263 ordinary Go parent passes, zero failures and 18 skips** across
35 package scopes. Two packages have no tests and contribute no parent passes.
Embedded command passes remain separate. At source-stage05, 55 unchanged Python
tests plus ten preparation tests gave 65 passes across eight scripts. Fixture
successor `00af9e4` replaces that ten-test scope with 15 passes, making the current
composition 70 passes. No repeat is added to its predecessor's count, and no
retained result is described as a new run.

The unchanged opt-in skips cover mount, hardware/media fixture and HTTP binding
profiles; their original and current reasons are preserved. They do not establish
those scenarios' execution. The complete capacity/fault/reboot campaign retains
its independent acceptance requirements.

Independent closure confirmed the worker, observer and closure helper gone,
the exact PostgreSQL instance cleanly stopped, ten databases retained and the
6,723 source files unchanged. The result, observer, closure and helper-postproof
hashes are in the delivery ledger. All original failed attempts are preserved.
Seventy-two screened original files were copied with exact byte/hash checks;
the server stdout with potential credential patterns remains remote, alongside
its copied summary, receipt and full skip evidence. No secret context was copied.

The two actual new artifacts are 48,793,294-byte `goby` and 50,231,482-byte
`goby-embedded`; the frontend contains 73 files totaling 1,421,440 bytes. Both
artifacts and frontend bind directly to build source `1ed1d69`. The old
preparation-only build-reuse proposal was not executed. Runtime tier setup can
consume the actual source-matching build result after fresh admission.

Composed regression and build acceptance is complete. Neither capacity nor a
fault matrix has run yet. Proceed with one tier at a time, exact corpus/source
and runtime bindings, the complete three-phase mixed workload, post-compound
reconciliation, independent overload and all declared recovery cases. Preserve
the original thresholds, concurrency and full scope before final publication.
