# Phase 3: Large-library concurrency and fault/restart recovery

Status: **the Goby session JIT repair at 89b6670 passed all 50 remote regression stages, both new builds and independent closure. Scope10 compound02's original OOM failure remains closed and retained; its four selected failed-state trees have complete external archival/readback. Exact-copy retirement, external preservation of the clean 33-database regression cluster and a fresh capacity journey remain pending. Full capacity/fault acceptance and Phase 3 publication remain pending**.

This record covers Phase 3 of the
[approved three-phase plan](../planning/media-analysis-resilience-plan-20260920.md).
Phase 2 is verified, closed and published: delivery `feb5004` and publication
metadata `e41febbb36687d04340f5c651f4bf1bf376a4310` were merged, pushed and read
back. Those records establish the starting provenance; they are not Phase 3
verification. The overall three-phase objective remains incomplete.

Implementation uses the isolated `codex/media-analysis-resilience` checkout.
Unrelated changes in the original checkout remain outside this increment.
The current accepted regression/build source is
`89b667083a1c5608b9d7e554df27de721ac23c51`. Earlier product/build source `1ed1d69`
and fixture successor `00af9e4` retain their original remote evidence.
Exact profiles, contexts and receipts remain bound in the private
checkpoint and delivery ledger; this document does not itself admit a workload.

## Current checkpoint

The session JIT repair at `89b6670` passed the complete 50-stage remote
regression and independent closure detailed below. Its restored regression
cluster shut down cleanly with 33 custom databases retained. That cluster is
separate from the failed scope10 capacity cluster, whose original OOM and
unrecovered database state remain preserved. No fresh capacity journey is yet
accepted. The four selected failed-state trees have complete census and external
archive/readback; their original guest copies have not been retired.

Scope10 preparation, memory restoration and execution rebind passed. The first
compound generation failed admission because the PostgreSQL version banner and
SQL `server_version` strings differed. Its controls are closed and the original
failure is retained. Generation `phase3-tier10k-10-compound-02` corrected the
execution manifest without changing the original preparation references,
passed actual admission, and entered cold business concurrency.

At **2026-09-23 02:41:37 UTC**, the **512 MiB PostgreSQL cgroup exhausted memory**.
The kernel killed catalog-owner backend `586572`; PostgreSQL, Goby and the Actor
subsequently exited. The driver result is `accepted=false` and
`execution_complete=false`, with failure codes `observer_failure` and
`http_status`. Partial remux-start p95 was approximately **8,023.7 ms** against
the unchanged **5,000 ms** threshold; this is a failed partial measurement.
Cached and incremental acceptance did not complete. No full 10k or 100k
journey, overload profile, or any of the 28 fault/recovery cases is accepted.

The observer did not exit after the original application/PostgreSQL PIDs
disappeared: its gap handler kept it active. Independent failure collection
preserved the evidence and externally withdrew that exact observer. The empty
publisher and control parent were stopped, and root independently confirmed the
successful collector's exit and closure. No scoped native, Actor or broker
process remains. Original service failures, including PostgreSQL `oom-kill`,
were preserved. Driver cleanup recorded seven `cleanup_operation` errors and
`filesystem_restore_held_for_active_worker`. PostgreSQL data, WAL and control
files remain in place, with the control state `in production`. The database has
not restarted; no clean shutdown, durable job-state result or fixture rollback
is claimed. Preserve this state and bind any recovery to its own evidence.

The last PostgreSQL sample contained **447,766,528 anonymous bytes** and
**76,296,192 file bytes**, including **61,272,064 shmem bytes**. It does not support
a page-cache-only explanation. Read-only analysis of the existing 77,032,176-byte
PostgreSQL log found 30 recursive roots/user data query plans across five
backends, with up to 182 JIT functions. `QueryItems` and `QueryLatest` already use
`SET LOCAL jit = off`. These retained plans are diagnostic evidence, not a
verified repair or proof that one SQL statement alone caused the OOM.

| Scope10 compound02 retained evidence | SHA256 |
| --- | --- |
| Failed compound result | `01e735b67dfe9b7aa70897007ed38601f144206ff6e13ebcd30b0efc59eacf02` |
| Compound launch intent | `f23ce3a047f99c28bdb31c211d832554ca80b6e745c68a3b63182da529c9ae84` |
| Actor entry | `f4d01ab70b64a20dc1b53bc78689b82e1b1843d7a6b216ab412edd978e320115` |
| Existing PostgreSQL diagnostic log | `4a22e9317bcaf6f55ff8a45f2a6fbd0f5e9edfde6cfa0cbadcf76d0b8a11b273` |
| Failed-run closure receipt | `41ee8cfde07a0e6ee11f89fba930b3e99599b1af501df849a6a2f60c07e50391` |
| Independent runtime closure | `a45ea2368e666f72d692192cb3a096648c1a8a912090821407bcca9b32728beb` |

The earlier accepted 49-stage regression at `8b6cb21` is retained below.
Scope09's failed fixtures and the closed 26-database regression cluster were
completely archived, externally read back, and only then retired as exact
redundant guest copies.
That storage work and scope10's physical process closure do not establish
capacity acceptance or durable job completion. The verified source repair sets
`jit=off` at startup for all Goby database connections, covering the observed
userdata query path and connections retained for catalog ownership or deployment
leases. Existing transaction guards, pool size and resource limits remain.
Two new real-connection integration tests also cover replacement connections,
Hijack and independent PostgreSQL sessions. The successful regression does not
establish that JIT was the only OOM cause. The prior clean regression archive
was restored for that separate regression; the failed capacity database remains
unmodified. Census and external preservation of the four selected scope10
failed-state trees are complete: 350,821 entries and 1,788,288,743 logical bytes
passed full member readback. Archive, transfer and external readback controls
are independently closed. The separate clean 33-database regression cluster
has a completed guest archive containing 13,747 entries and 3,044,606,975 logical
bytes, with its archive worker independently closed. Its external copy and
readback remain pending; no original tree from either archive has been retired.
Fresh disk admission must use measured physical free space after retirement.
Phase 3 remains unpublished; phases 1 and 2 remain published at
`e41febbb36687d04340f5c651f4bf1bf376a4310`.

### Accepted scope10 session JIT repair regression

Source `89b667083a1c5608b9d7e554df27de721ac23c51` completed all **50 stages**.
All **35 ordinary Go packages** completed with **4,279 parent passes, zero
failures and 18 explicit parent skips**. The separate Vulkan subcase skip is
not another parent skip. All 18 parent skip identities and that subcase identity
exactly match the preceding accepted `8b6cb21` evidence. The embedded command
suite passed **24** parents; eight Python scripts passed **91** tests. Focused
library **10**, focused database **2** and race **15** passes are separate
repeats and are not added to the ordinary Go total.

Both Go binaries were freshly built from this source. All tracked frontend
inputs and all **73 artifact files** were compared by hash before reusing the
accepted `8b6cb21` frontend output.

| New artifact | Bytes | SHA256 |
| --- | ---: | --- |
| Goby | 48,796,956 | `1709d063369278ad0690e1dac3df6f839aef513290b489cafeb38c8c591792cf` |
| Embedded Goby | 50,239,960 | `69f46e1e2fe9aaab4689d5003014199d2b96d2de7737ddbbd185acae9f392b59` |

The observer passed with **zero OOM events**. Four transient `du` measurements
failed when PostgreSQL files disappeared during measurement; observation
resumed without persistent loss, and missing values were not filled with zero.
Worker `646051`, observer `646042`, PostgreSQL `645634` and collector `697473`
are independently closed. PostgreSQL shut down cleanly, retaining **33 custom
databases**, comprising the restored original 26 and seven new regression
databases. This does not describe or recover the separate scope10 OOM cluster,
which remains unrestarted with control state `in production`, unknown durable
job state and no fixture rollback.

The private evidence directory is
`/opt/goby-phase3-campaign-20260922-01/private/regression-scope10repair01`.

| Scope10 repair evidence | Bytes | SHA256 |
| --- | ---: | --- |
| Complete regression | 650,194 | `87848ec48d5a926219eead3150399bd4be2d768c6d55640f04ca28eb6f1c2eee` |
| Resource observer | 10,115 | `21827b5bea429fb147c9bbd08b54c00686232ed20ecebf328ea8def60c068bb8` |
| Closure collector | 39,960 | `224e4fcb179dbe19b80257f017babeee0ba645a77f6714beedbd1c665aff01c6` |
| Independent closure | 5,330 | `dbbc033d4dca327e5812db093bf748f16686424c989c0e9fea4c5ecbc758d9c1` |
| Build delivery | 11,804 | `bc30ec01e6a88afd5a638908e82b14a777dccba77b4722d87b9011f6e4646bf2` |
| Explicit skip evidence | 8,002 | `90a8d40cfb0b011a2bdd50666c6caa6bff95f88109925c10439e86749c71f80b` |

### Observer terminal-handling candidate checks

A separate observer candidate with source SHA256
`250a9fa1b1276f4410a941d1063370d157d745d7ed7f8ec56d6badbdbbfcb3a6`
passed **15 isolated Python tests** on VM106 under DynamicUser UID `64360`.
The test worker closed. These tests are additional to the eight-script,
91-test regression total. The 2,121-byte receipt is retained at
`/opt/goby-phase3-campaign-20260922-01/private/observer-terminal-tests01-result.json`,
SHA256 `485ec98ee207f20232cad8fe48ce5b8a15c8b22937638439fe112f95d0e87cc9`.
The candidate has not been deployed as the real observer, and actual systemd
terminal-state propagation has not been tested. These isolated checks do not
accept a capacity journey or close those remaining runtime requirements.

## Historical scope09 failure and accepted repair regression

The following scope09 and earlier records retain their original failures and
verification scope. Their service and storage states describe those checkpoints;
the current resumption state is the scope10 checkpoint above.

Scope09 passed preparation and final resource admission, then ran the complete
compound driver until a cold-phase failure. The metadata edit returned a real
`409 revision_conflict`: the concurrent sorting rebuild legitimately changed
the same item's automatic sort name and metadata revision. The driver had
incorrectly required its old revision to succeed. The repair retains the first
concurrent request and its overlap evidence, accepts only that specific conflict,
checks that the rejected write did not change manual metadata, and permits one
fresh CAS followed by independent persistence readback. A repeated conflict or
unexpected manual/source change still fails.

Two Latest requests took 10,172-10,469 ms. Their existing PostgreSQL plans showed
9,718-10,268 ms execution and 1,012 JIT functions with optimization and inlining.
`QueryLatest` had not applied the transaction-local JIT policy already used by
ordinary catalog queries. Its repair covers the main query and subsequent user
data/subtitle projections without changing pooled-session settings. New tests
cover grouped and ungrouped results, user/application subjects, and restoration
after both successful reads and an actual SQL failure. These source changes and
the workload metadata-CAS repair passed the complete remote regression below.

Remux first-byte samples were 4,718 and 5,170 ms against the unchanged 5,000 ms
target. Their overlap with the expensive Latest queries suggests contention,
but does not establish its cause. No additional remux change or threshold
increase is justified by these two samples. Seek checks passed; the existing
joint video/audio boundary repair remains. Cached and incremental phases did
not run, so no full capacity tier is accepted.

The external observer completed with no OOM or service-generation change. It
retained one transient observation gap without persistent loss. Workload cleanup
reported no errors. Actor, observer, publisher, closure collector, control parent,
application and PostgreSQL are independently closed; PostgreSQL shut down cleanly
and both application service units have autostart disabled. The database, fixture
and original failed evidence remain intact.

| Scope09 evidence | SHA256 |
| --- | --- |
| Failed compound result | `c56f73146db8e3a8f563d3d84100bde1406234ff9454590dafa5498e0e84cd80` |
| Resource observer | `008f783411907bb7a1619dd70d6414c919551b2c89efb59ebb06694e49011fb3` |
| Compound independent closure | `b09c747c8c1bc6c30db0e277ee4fb395129f64af40e761bb3497197badb54cea` |
| Runtime independent closure | `814708aa562629847364c3c3bfca6357e5ac9a44865822eaea667abe86e2e1a4` |

### Accepted scope09 repair regression

Source `8b6cb210f0ecc679c8067e19da142dfe963dfb8c` completed all **49 original
stages**. All 35 ordinary Go packages completed with **4,277 parent passes,
zero failures and 18 explicit parent skips**. One additional Vulkan subcase skip
is retained separately and is not another parent skip. The embedded command
suite passed **24** parents; the eight Python scripts passed **91** tests,
including 35 workload tests. Focused checks passed **10** parents and the race
stage passed **15**; these repeated cases are reported separately and are not
added to the ordinary Go total.

Both ordinary and embedded Go binaries were freshly built from this revision.
All tracked frontend inputs and all **73** retained frontend artifact files were
compared before reuse. The successful collector rechecked the complete stage
inventory, original log hashes, package results, new binaries and source
inventories before closing the proved service lifetimes.

The independent resource observer passed. Four transient `du` observations
failed when PostgreSQL files disappeared during measurement; monitoring resumed
without persistent observation loss. Missing observations were not credited as
zero. Worker, observer, setup and closure controllers are independently closed,
and the regression PostgreSQL cluster shut down cleanly. At closure, all **26**
databases were retained, comprising the original 19 and seven fresh regression
databases. The subsequent full archive and exact guest retirement are recorded
below.
The actual build delivery is bound to that complete result and independent closure:

| Scope09 repair evidence | SHA256 |
| --- | --- |
| Complete regression | `d23198ff7128e6b5c902fbf3d39fe542e9b248dddd01a0376c4c7972f9c60612` |
| Resource observer | `cec40c94a29450b64eb48209c3fe837c03e8a82cec754853c3bc885d044cf41c` |
| Independent closure | `3d89ecbf1d0b6750c83eecf2da60996fafa87f8ef97bd9d52c21abd651788461` |
| Build delivery | `e9c28c29aaaf2de8a754319b44172d0012f16680b78559d392b452397e1134fd` |
| Explicit skip evidence | `69908004d04dbafa539243189634463a92aefba4b8a565abdcf8efefeedc6a9f` |

Scope09's failed fixture, preparation and compound output were archived with
all **349,155** members read back externally. After independent worker closure,
the exact three guest trees and two redundant guest archive files were retired;
the external four-file archive and all unselected data remain. The external
readback receipt is `2e07a8f95f01ffdfcaaf66d1e90364fe3883cebf78b8477036ab5c5f013aeb3c`,
and final retirement closure is
`464e8aaf0bad2be669da6f016742f6d31703470b6433c602fef39b0ed93c6c08`.
The closed regression PostgreSQL cluster, including all 26 databases, also
completed full external archival and readback before its exact redundant guest
copy was retired. The external archive preserves those databases; they are no
longer retained as that guest cluster. This completed storage step enabled the
later scope10 preparation and does not accept its workload.
No complete 10k or 100k journey, or any of the 28 fault/recovery cases, is accepted.
Phase 3 remains in progress and unpublished.

### Accepted regression before the scope09 repairs

The query JIT, thirteen ownership-admission paths, remux joint-boundary selector,
and driver packet-proof repairs are frozen at
`55d50696e4590d719cad1540cac42c90c028d139`. All 35 ordinary Go packages completed:
**4,275 parent passes, zero failures and 18 explicit skips**. The embedded command
suite passed **24** parents; eight Python scripts passed **84** tests. Focused
checks passed eight parents and the race run passed fifteen; these repeated
cases are not added to the ordinary total. Both ordinary and embedded binaries
were freshly built. Frontend assets were reused after comparing every tracked
input and all 73 artifact files.

The independent observer passed its declared resource checks. It retained four
`du` observation failures caused by PostgreSQL files disappearing during
measurement; monitoring resumed without persistent loss. Missing measurements
were not zero-filled. Eighteen hardware, fixture and explicit-profile parent
skips, plus one Vulkan subcase skip, remain explicit in the retained evidence.

The successful collector rechecked all 49 original stage receipts and log
hashes, complete package results, artifacts and source inventories. Worker,
observer and the exact PostgreSQL lifetime closed normally; all 19 databases
remain, including the original twelve. Root then independently closed the
collector and published the actual build delivery. Key receipt hashes are:

| Evidence | SHA256 |
| --- | --- |
| Complete regression | `28ca253c776c5e5cbf225a283cdd182d06492431bc096892cb11df546a918ed7` |
| Resource observer | `e3db6cb6d84db0f47d413190c4a03c442d86e5d9cb23178ef2e4fcde26bdbfcd` |
| Independent closure | `bbee5da585b504f8e198ffb70a75aebc9cbf809679729640eb8f10e922d24d2c` |
| Build delivery | `ee20450db2d81b581da95dc409745687df0491ba886566e95a1743a0d70f0708` |

Scope07's failed fixture, preparation and compound output were externally
archived with all 349,360 members read back. After independent consumer closure,
only those three guest trees and two redundant guest archive files were retired.
All databases, original diagnostics, source/build artifacts and the complete
external archive remain. Scope08 subsequently failed when the preparation
observer reached its control cgroup limit while accounting directory metadata.
Its failed trees were externally archived and verified before exact retirement.
The control parent now uses a 64 MiB reclaim threshold within the original
128 MiB hard limit; scope09 preparation and compound observation completed
without OOM. No latency, concurrency or failure threshold is relaxed.
No complete 10k/100k compound journey, overload profile or fault matrix is accepted.

### Earlier failures and their repairs

The scope04 compound failure exposed an actual product lock inversion, repaired
at `5fb968a`. Its affected regression and both Go builds passed. Scope05 then
failed during fixture preparation because the observer broker rejected the
temporary systemd memory override. The corrected broker passed twelve remote
guard checks. Neither failed attempt establishes capacity acceptance.

All scope05 processes are closed. Its 347,968 archived members were independently
read back on the external controller before the exact guest fixture and
preparation trees were retired. Databases, diagnostics, builds and external
originals remain retained. Independent retirement closure measured 21,989,072,896
free bytes, above the unchanged initial 10k gate of 21,676,163,072 bytes.

Scope06 passed provisioning, managed memory lowering and actual Actor broker
access, including all fourteen corpus read opens and application private-path
isolation. Its full preparation later failed because the workload manifest still
pinned scope04's scan configuration hash. The final broker itself exited zero
and matched the actual scope06 configuration; the producer correctly refused the
stale manifest. Six seed scans completed with 9,342 catalog rows, but no accepted
context or successful preparation was published. Resource observation passed
without gaps; it does not convert the producer's exit one into success.

The preparation tool now checks the real broker against the manifest before any
fixture directory creation, bootstrap or media generation, while retaining its
final check. All eighteen preparation tests passed remotely, including refusal
without side effects and the valid configuration path. Product and workload
driver bytes are unchanged. Scope06's memory limit was restored, all its workers
and application/database lifetimes closed, and its original failed evidence and
database remain intact. A fresh full preparation must bind its own configuration
hash before dispatch. Complete capacity and fault acceptance remain pending.

Scope06's 347,966 members have now passed full guest and external readback;
the exact archived fixture and preparation trees were retired after independent
worker closure. All databases, diagnostics, source, binaries and external
originals remain retained. Scope07 provisioned successfully with its manifest,
predicted configuration and actual deployed configuration in agreement. Its
actual Actor broker check also validates the complete scan configuration against
the pinned manifest before producer dispatch.

Scope07's first launcher was refused before creating a fixture or dispatching a
worker: free space was 30,105,600 bytes below the unchanged preparation gate.
Only three closed, unreferenced generated compiler-cache directories were then
retired, freeing 31,465,472 bytes. Sources, binaries, frontend and private results
were preserved. A separately recorded successor passed the original gates and
started the full producer and resource observer. The producer's new real broker
check passed before any fixture mutation. Preparation completed with 9,342 seed
catalog rows and 658 pending additions for the 10,000-item tier. Resource
observation passed, all preparation workers closed, and the application returned
to its original 1,280 MiB limit without changing its lifetime.

The actual scope07 compound workload then failed during cold. Concurrent shallow
and deep catalog requests took approximately 6.2-7.9 seconds. Three playback
preparations took approximately 6.2-6.4 seconds, and remux seek returned 415.
Cached and incremental phases did not execute. External resource observation
passed without gaps or service-lifetime changes; that does not make the workload
successful. Driver cleanup completed. Independent closure confirmed the worker,
observer, publisher, closure collector and control parent were closed. The
original application/database subsequently closed cleanly. Its failed data is
retained in the complete external archive described above.

A read-only diagnostic replayed the original count and page SELECTs inside one
repeatable-read snapshot in JIT on/off/off/on order. Counts and ordered IDs
matched. With JIT enabled, count took 587-630 ms and pages 1,784-1,854 ms;
transaction-local JIT disabling reduced those to 22-23 ms and 26-79 ms. This was
an isolated diagnostic, not a repeated compound acceptance run. The original
collector failed at a CSV field-size limit after SQL completion; a separate
collection of existing output succeeded without repeating SQL. Both records
remain retained. The query repair and semantic/connection-restoration tests
passed the later 55d5069 regression described above.

Playback admission separately exposes head-of-line blocking: a request waiting
for the catalog owner holds the shared store mutex, delaying unrelated source
opens and user-state writes. All thirteen affected admission paths now wait
outside that mutex while preserving queue, root-anchor and shutdown semantics.
New tests observe the actual owner-mutex wait before checking independent work,
cancellation and shutdown. Independent source review found no remaining reversed
admission path; the later focused, race and full regression passed. This differs from the
earlier repaired lock inversion.

The remux fixture's requested 30-second video point lacks a matching AAC packet
boundary; a proved joint boundary exists at 24 seconds within the allowed
window. The selector repair searches earlier joint candidates after validating
the entire index. The driver explicitly permits alignment while retaining the
original 30-second request, both copy codecs, source timestamps and target-frame
verification. Independent review also identified an output AAC packet-hash
verification gap; the driver now compares the first output AAC payload with the
source proof and validates the proof's joint-boundary binding. These changes
passed regression; their real compound repeat remains required. No latency or
failure threshold was relaxed.

The previous composed regression had **4,264 ordinary Go parent passes, zero
failures and 18 explicit skips**, plus **24 embedded command passes** and
**73 Python passes**. This is the accepted baseline before the new scope07
repairs. Frontend and both application builds passed. Original
failures and skips remain in their source-bound records. Preparation is not
capacity acceptance: the full 10k compound journey failed during cold, while 100k,
overload and all 28 fault/recovery cases remain unrun; `accepted_capacity` is false.

### Historical scope04 admission and failure

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

### Scope05 preparation failure and broker correction

Scope05 deployed the accepted repaired binary with fresh user, database and
process identities. Initial cross-UID media access, private-path isolation and
I/O accounting passed. Direct cgroup lowering initially read back successfully,
but a later observation found the application back at its declared limit before
the producer started; the cause of that reversion was not established. The
replacement used one explicitly pinned systemd runtime MemoryMax dropin, with
the same application lifetime and the unchanged preparation resource envelope.

The full producer reached the final observation after creating the pressure
tree and indexing 9,342 seed catalog rows. It then exited 1 with `child_exit`:
the fixed read-only broker returned `app_service_binding_changed` because its
original contract rejected every service dropin. This is a preparation tooling
failure, not a mixed-workload result or a new product deadlock. The prepared
receipt remains false and no compound context was published. Removing the exact
temporary dropin through the restoration operation made the same broker return
complete on the same App/PG lifetimes. All native work was idle; the one retained
playback row was the intended terminal `Stopped` seed. Goby and PostgreSQL then
closed cleanly, with all failed data retained. Runtime closure:
`0b84771fc9be9e0372718fb8b00f0dd8cacdf628f7eba46b0dd21b57879e3fb8`.

The private setup10/broker successor binds only the exact initial normal-service
preparation dropin path, hash and 512 MiB limit. It still checks original unit,
process, executable, effective memory and zero swap, and rejects extra overrides.
Case/fault bindings carry no preparation override. Twelve remote guard tests
passed, including wrong hash, additional dropin, foreign unit, limit, PID and
fragment rejection; test/worker closure:
`003604e4fb6ed237626e65d566bce5d938ab0bea1ff6521715edb9b61b624e31`.
The old broker bytes were preserved before deploying the new helper at its fixed
argv path, after every old application lifetime had closed. The next fresh scope
must perform actual Actor broker access after managed lowering and before the
heavy producer; unit tests are not that runtime check. Scope05's archive and
external readback completed for all 347,968 members and 1,673,278,695 source
bytes, with exact hashes and file/directory fsync. External receipt:
`9257d2be4e9956ca0d33b73acb62065f18eb334dffec1505de0c2bc29b4776c5`;
independent closure:
`59382d72a4cbdd8a476c57bd3dfc344c83c62967c9576c5f206f08673e5d86ca`.
Only the two externally preserved source trees and two large guest archive
files are selected for current retirement. The fresh scope06 profile remains
unreleased until current resource and namespace admission.

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

Scope10 compound02 failure preservation and physical runtime closure are
complete. Database control state remains `in production`, durable job state is
unknown, and fixture restoration is held. Record any later database recovery
separately. Do not reinterpret the involuntary OOM exit as a planned recovery
test or replay the failed prepared state as fresh.

The earlier 26-database regression-cluster archive/readback and exact guest
retirement are complete. Its restored successor passed the `89b6670` regression,
shut down cleanly and retains 33 custom databases. Its new guest archive and
archive-worker closure are complete; external preservation remains pending.
The failed capacity cluster is separate. The four selected scope10 failed-state
trees have complete external preservation and closed archival controls, but
their original guest trees remain. Complete exact-copy retirement and fresh
space admission before a new capacity journey. Preserve all original failures,
the unrecovered capacity database and external archives.

Use the accepted `89b6670` repair/build delivery and preserve the original
limits for a newly admitted complete 10k compound journey and overload. The
observer candidate's 15 isolated tests do not establish deployed observer or
actual systemd propagation behavior. Follow a successful journey with the
required post-compound reconciliation and full
fault/recovery matrix. External-controller admission is required before later
external ACK, archive writes or fault operations, not for independent guest
work that performs no external write.
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
