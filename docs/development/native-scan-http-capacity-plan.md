# Native scan and HTTP capacity observation

Status: **implementation held under the [measurement-value decision](native-capacity-measurement-decision.md);
no native input, fixture or execution is admitted**. The [preparation checkpoint](native-scan-http-capacity-preparation.json)
records the actual read-only environment observation, candidate source checks
and twelve passing synthetic reader groups after a preserved first failure.
Native transport, lifecycle and complete controller integration remain unverified.
The [r04 installation scope](systemd-installation-fourth-attempt.json) has closed
with independent resource readback. Its runtime passed and its sealer failed;
that result remains unchanged. Any unresolved defect in a reused ownership or
closure primitive must be resolved before using that primitive here.

This is the next independent M2 measurement proposed by the
[current execution plan](../planning/current-execution-plan.md). It does not
depend on core-video acceptance or admit main promotion. All implementation
verification and execution use `ssh test-env`; this document was prepared by
static reading only.

## Question and evidence boundary

Measure what happens to bounded catalog HTTP reads while one native Goby
process executes a normal cold scan and then a normal cached rescan of the same
owned corpus. Record actual scan duration, HTTP latency and overlap, application
and PostgreSQL resource samples, task completion, and catalog/ACL preservation.
There is no existing product latency or throughput SLO for this profile. Report
measurements and operational limit failures without inventing a release target.

The first profile uses **1,000 tiny valid media files** across two libraries,
taken from the existing 10,000-file recipe, and the existing 64-item page shape.
It changes the execution boundary to native TCP HTTP, task admission, and process
resource observation. This smaller fixed corpus bounds the first measurement;
it does not increase media duration, resolution, reader count, or scan concurrency
to manufacture a representative-media or saturation claim.

| Existing result | Reusable evidence | New evidence required here |
| --- | --- | --- |
| [SQL capacity/isolation](catalog-capacity-isolation-verification.json) | 10,000 SQL-created leaves, ACL-safe page/count behavior and UserData preservation during a transaction block | Real native HTTP exchanges during scanner work; no SQL-created catalog and no transaction-block injection |
| [Tiny real-media capacity](catalog-real-media-capacity-verification.json) | Two libraries, 10,000 media leaves, cold/cached scan correctness and measured tiny-file cost | A running native process, task/child/job identities, concurrent TCP reads and process/cgroup measurements |
| [Native catalog restart](native-catalog-restart-verification.json) | Small-catalog bootstrap/auth, HTTP recording and normal process shutdown primitives | This larger fixed corpus and its scan/HTTP measurements; no second application start is needed |
| [Retained systemd package](systemd-package-build-verification.json) | Existing embedded amd64 artifact and source/build bridge | A new isolated runtime input; installation acceptance remains in its own r04 record |

The earlier 174.018-second cold scan and 82.768-second cached scan are historical
observations on another profile, not predictions, reservations, or pass limits.
The native [Server.New](../../internal/server/server.go) enables video seek
analysis in addition to ordinary probing. It can run ffprobe and FFmpeg even
with transcoding disabled; its separate
[seek-analysis deadline](../../internal/media/video_seek_process.go) is not
covered by treating the ordinary 30-second probe timeout as a whole-scan limit.
That difference is a reason for the smaller initial corpus. Keep production
probing unchanged and include its children in resource and shutdown accounting.
This increment will not prove representative storage throughput, large-media
probing, kernel I/O stalls, network filesystems, host reboot/power-loss durability,
playback, browser behavior, GPU/arm64/OCI, or complete M2-M6 delivery.

## Concrete reuse and necessary adaptation

Use definitions from the existing operators in a fresh scoped copy; never run a
consumed historical `main()` or pass an old input to a new workload. Freeze each
selected source and its actual dependency descriptors before remote checks.

| Entry point | Intended reuse and limit |
| --- | --- |
| [cmd/goby/main.go](../../cmd/goby/main.go), `run` and `runGenerationLoop` | One direct invocation of the retained E11 Goby binary, matching the verified unit; no invented subcommand, embedded test server or source rebuild |
| Retained `native-catalog-journey.py`, SHA256 `63125715ad415349f3ae95a008f56783800c7334d90efcbcb394a14215e695d4` | `CatalogJourney._request`, `_login_admin`, `_create_users`, `_login_emby`, `_logout`, and `cleanup` supply bounded transport, credential capture and closure patterns. Replace the fixed seven-file orchestration with this one scenario; its current 500-request cap and small-catalog assertions do not fit automatically |
| Reviewed r04 `Runtime`, `CapturedConnection`, `CapturedResponse`, and support definitions | Reuse owned start/stop, bounded raw HTTP capture, typed SQL observer identity, protected-state checks, and retained D-Bus stop evidence after binding their reviewed r04 outcome. The fixed installer paths, two-start inventory, and sealer contract require explicit scope adaptation |
| [scripts/test-env/verify-tasks.py](../../scripts/test-env/verify-tasks.py), `completed_run` and `wait_no_work` | Reuse task-to-child-to-scan-job checks and cancellation-before-credential-closure order. Replace its two-file counts and old database/browser assumptions. Do not run its old allocation, browser, schedule, or restart workflow |
| [internal/server/admin_tasks.go](../../internal/server/admin_tasks.go), `registerAdminTaskRoutes`, `startAdminTask`, `adminTaskRun`, `cancelAdminTaskRun` | Bind the actual native task routes, request schema, stable returned IDs, terminal state, and cancellation behavior |
| [internal/library/store.go](../../internal/library/store.go), `New`; [task_scans.go](../../internal/library/task_scans.go); [scan.go](../../internal/library/scan.go) | Preserve the existing two scan workers and normal cached scan behavior. Do not insert delays, change the scanner, wrap ffprobe, or add a production observation endpoint |
| [catalog_scan_capacity_integration_test.go](../../internal/library/catalog_scan_capacity_integration_test.go), corpus construction, `catalogCapacityTree`, `catalogCapacityReadCatalog`, and `catalogCapacityUserData` | Reuse the file layout, exclusive-copy identity checks and selected catalog/UserData snapshot definitions. These are Go test helpers, not a callable native server or a new test execution |
| [catalog_capacity_isolation_integration_test.go](../../internal/server/catalog_capacity_isolation_integration_test.go), page/count query and ACL assertions | Reuse bounded query shapes and correctness rules; replace `httptest` with actual loopback HTTP. Do not import its SQL seed or locking experiment |
| Retained `catalog-real-media-capacity-seal.py`, `tree`, `pack`, and `capacity` | Reuse bounded private archive/readback concepts. Its consumed roots, source archive, old protected identities and ownership assumptions are not current authority |

The retained native journey and runtime sources are inventoried by
[native-catalog-restart-verification.json](native-catalog-restart-verification.json).
The r04 sources remain private scope material, with their outcome and source
bridge recorded in the fourth-attempt result and decision. This proposal adds only a fixed workload mode and
two bounded read workers to those existing primitives; it does not introduce a
general launcher, browser runner, persistent sampler, or observation framework.

## Fresh parameters that must be bound

The execution input remains incomplete until the following facts are obtained
and reviewed for the new scope. Historical values are not substituted for them.

- **Artifact:** use the unchanged E11 binary, 30,691,123 bytes, SHA256
  `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`,
  bound to its retained package manifest and source bridge. A changed artifact
  needs its own relevant verification before this measurement can describe it.
- **Ownership:** fresh exclusive evidence/fixture paths, dedicated unit names,
  the existing application/PostgreSQL account identities, empty independent
  database, namespace/port bindings, current protected resources and the
  existing deployment lock. Keep main, both retained candidates, all three
  protected PostgreSQL instances, and the paused M2 mount unchanged.
- **Profile:** actual CPU count and quota, cgroup version/available metric files,
  memory/swap limits, PostgreSQL settings and connection limit, filesystem type
  and mount identity, available bytes/inodes, tool pins, and the exact native
  configuration. Use one isolated custom unit and fixture paths, not another
  installation into `/usr/local/bin/goby` or the standard application directories.
- **Corpus:** establish that the two saved video/audio templates are available
  from an explicitly retained owned source. Bind their path, hash, bytes and
  media facts before copying. If unavailable, separately freeze the existing
  two-template synthetic generation recipe in the new preparation; do not run
  the old fixture creator or silently replace the media. The first profile uses
  200 movies, 200 episodes (two series, five seasons each, twenty episodes per
  season), and 100 audio leaves (ten albums with ten tracks) per library. Include
  twenty album NFO files overall, retaining the earlier naming/hierarchy recipe.
- **Actual storage cost:** verify independent regular copies, single links,
  path/ancestor ownership, logical bytes, allocated blocks and required inodes.
  Cap the new corpus at 16 MiB logical and 32 MiB allocated, with video/audio
  templates bounded by the existing 8 KiB/16 KiB limits respectively.
  Copies have new device/inode identities; do not hardlink, reflink, truncate,
  create sparse substitutes, or modify the retained source. New media remains
  readable and nonwritable to the application on an owned ext4 location.
- **Authority and expected state:** new private bootstrap/password inputs,
  three new users, two restricted library policies, the actual returned task
  ID, two distinct RequestIds, run/child/job IDs, and the expected catalog map
  derived from the physical manifest. Existing environment/configuration files
  remain hash-only and master/key files stat-only. Only the new fixture context
  may be privately decoded; secrets never enter CLI arguments or public reports.
- **Sampling implementation:** a frozen fixed query schedule, private worker
  inputs, per-worker request/byte/time limits, workload-specific worker start/join
  receipts, cancellation channel, and cross-process monotonic timestamps. Verify
  this narrow adaptation remotely before any fixture is kept live for it.

The later recorded environment observation found the fixed targets absent,
twelve available CPUs, 5,073,956,864 free root bytes and 6,626,668,544 available
memory bytes, with protected state unchanged. These are observations, not
reservations. The old corpus did not retain its templates; the new preparation
must generate and bind the two fixed-recipe files. Absence, ownership, final
inputs and capacity must still be checked at the actual execution boundary.

## Bounded sequence

1. Complete source review and the smallest remote checks for the changed
   controller/reader contract, then perform fresh read-only admission. Prepare
   one owned PostgreSQL and private loopback namespace with read-only media.
   Run the real typed PostgreSQL observer identity check before one nonroot APP
   start. Freeze the remaining lifetime from the anchor's original start;
   never reset its allowance during handoff.
2. Bootstrap once, create the two libraries with `Scan: false`, create the two
   restricted users, set each policy to its own library, and log them in once.
   Retain the administrator cookie/CSRF, an administrator Emby credential for
   settled catalog checks, and both restricted user Emby credentials. These are
   four credentials for three users. The Emby catalog routes do not accept the
   native administrator cookie; the extra login stays within the setup quota.
   Discover `library.scan` by `Key` using `GET /admin/v1/tasks`; use its returned
   ID. Confirm empty catalog/jobs, no active task/scan and no installed triggers.
   Record empty-state HTTP and the first private SQL baseline.
3. Submit exactly one cold run with
   `POST /admin/v1/tasks/{id}/runs` and a new `{RequestId}` body. Require
   `Admitted: true`, the returned run ID, manual source, and `TotalChildren: 2`.
   Bind the two child library identities from the bounded run-detail GET;
   waiting children may not yet have ScanJobIds. The request carries no
   `ForceProbe` or executor field. Start the
   two bounded read workers for this run and poll its real state independently.
   A response loss is an unknown admission outcome: stop new work, reconcile
   the owned returned/recorded identity for closure, and do not retry the POST.
4. Join both readers before advancing the phase. Require the cold run and both
   children/jobs to complete normally with `ForceProbe: false`, exact file counts
   and no warnings/errors. Record a terminal SQL catalog map and bounded settled
   HTTP checks. Seed exactly two favorite states through the normal user API,
   one owned leaf per restricted user, and save all fields/timestamps/`xmin` of
   the resulting UserData rows. No playback or nonzero resume state is created.
5. Once the first run is terminal and no scan is active, submit exactly one
   cached run using a distinct RequestId. Require a new run ID and
   `Admitted: true`; coalescing with the first run is a failed boundary, not a
   second measurement. Use another pair of bounded readers with this immutable
   phase/run identity. Do not change any media between runs. Require terminal
   task/child/job ownership and `Added == Updated == 0` for the cached run.
   Report normal cached-rescan behavior; those counters alone do not prove an
   exact ffprobe call count.
6. Join the final readers, record settled HTTP and SQL, compare selected catalog
   identity/hierarchy and all seeded UserData fields/`xmin`, and verify every
   source file is unchanged. Close all four credentials with logout 204 and
   same-credential 401 plus stored revocation. Stop APP normally, take the final
   read-only snapshot while the owned PG remains available, then close PG and
   the anchor with retained exit evidence. Preserve and reread the private
   evidence/closed-PG archives and close the owned mount/units/lock.

Only the two declared task admissions and their four child scans are allowed.
Task scheduling, forced media refresh, scan replay, another application start,
PG restart, media replacement, storage faults, and client playback are outside
this input. The existing `CatalogJourney._scan` is a per-library cold-scan
helper; it is not the task admission or two-phase completion checker here.

## Concurrent HTTP observation

The controller stays single-threaded. `support.app_network` currently rejects
multithreaded namespace transitions; `Runtime.active_capture` and the journey's
serial/phase/trace state are also single-request state. Do not add threads around
the current `_request`, share its capture object, or patch global `http.client`.

Use at most **two independent single-threaded read worker processes at a time**,
one restricted user per worker, and at most four worker processes across the two
phases. Each receives its own private frozen input, immutable phase/run/user
identity, distinct trace/body directory and local request sequence. Each enters
the pinned private namespace through the existing guarded mechanism before I/O.
The controller owns process-group birth, cancellation, exit and join receipts.
Readers receive no administrator credential. Their fixed entry accepts only the
two GET shapes below for their own user ID; it rejects arbitrary URLs, methods
and bodies. The normal restricted-user token is not claimed to be a server-side
read-only credential; the worker's fixed code constrains its use.

Each worker alternates these real HTTP queries, with one request outstanding
and at least two seconds between dispatches:

```text
GET /emby/Users/{own-user-id}/Items?Recursive=true&IncludeItemTypes=Movie,Episode,Audio&SortBy=SortName&StartIndex=0&Limit=64
GET /emby/Users/{own-user-id}/Items?Recursive=true&IncludeItemTypes=Movie,Episode,Audio&Limit=0
```

The controller ends readers when the task is terminal or their fixed observation
window ends. Each worker has at most 60 GETs and 120 seconds per phase, whichever
ends first. There is no catch-up burst, automatic retry, or transfer of unused
quota to another worker. An early task completion may yield fewer observations.

For each sample retain process/phase/run identity, request intent and connection
close, monotonic dispatch/header/body-complete times, status, response request
ID, raw private response evidence and its hash, bytes, query shape and correctness
result. HTTP timing starts immediately before dispatch and excludes preceding
evidence fsync; record evidence-write and wall phase costs separately. Reuse the
existing `Connection: close`/identity-encoding policy and state it in the report;
this is not keepalive, browser, or WAN performance.

During cold ingestion, results can grow between requests. Check status/types,
bounded response size, unique item IDs within a page and permitted item kinds;
do not require equality to a completed catalog or compare successive pages as
one database snapshot. Reconcile every observed ID with the terminal map for its
authorized library. Settled count results must be exactly 500 leaves per
restricted user and 1,000 for the administrator. Before/after boundary checks
retain cross-user 403 and hidden-item 404 where an actual hidden ID is known.
Cached-phase pages/counts use the frozen completed catalog and selected UserData.

Classify overlap from sample intervals and actual run/child `StartedAt` and
`FinishedAt`, with controller monotonic observations of pending/running/terminal
state. Save the wall-to-monotonic anchors and their uncertainty. An in-flight
HTTP interval and a task POST alone do not prove active scanning. Report exact
sample counts and observed simultaneous-reader intervals. If active-scan or
two-reader overlap is not established, mark that metric incomplete; do not slow
the scanner, add files, or start another run to obtain it.

## Proposed operational limits

These ceilings bound one execution; they are not performance acceptance targets.
The frozen input must reconcile every nested timeout and resource allocation
before dispatch. A budget change needs a revised input before work starts.

| Resource | Proposed bound |
| --- | --- |
| Application | One invocation; `GOMAXPROCS=2`, `GOMEMLIMIT=768MiB`, `MemoryMax=2G`, `MemorySwapMax=0`, `CPUQuota=200%`; its existing two scan workers and any owned ffprobe/FFmpeg children share the application cgroup limits |
| PostgreSQL | One owned cluster; at most 1 GiB tmpfs, `MemoryMax=768M`, no swap, `CPUQuota=150%`; no build cache in this volume |
| Controller/readers | Controller at most 256 MiB; each of two concurrent reader processes at most 128 MiB; bounded process groups, no persistent helper service |
| Initial host capacity | At least 6 GiB `MemAvailable`, 4 GiB available root bytes and 30,000 available root inodes before new fixture allocation; account for all new cgroups, PG RAM and retained files without borrowing the paused mount |
| Disk preservation | Keep at least 1 GiB available root bytes; at most 2.5 GiB cumulative new root allocation including working evidence, corpus, installation copy, retained trees and archive copies |
| Evidence | At most 384 MiB raw HTTP/SQL/command/metric evidence, including a 64 MiB cleanup/terminal-record reserve and per-stream caps; PG archive source at most its 1 GiB volume. Include archive overhead and simultaneous raw/archive copies in the cumulative disk bound |
| HTTP | At most 600 requests overall: 240 worker observations, 184 task-state polls, 112 setup/settled/readiness checks and 64 reserved cleanup requests; quotas remain distinct and every actual dispatch counts |
| Per HTTP request | 10-second absolute deadline; 512 KiB response body cap for catalog reads; 1 MiB for other existing bounded control responses; overflow is a failed sample, not truncated acceptance |
| Task phases | At most 450 seconds per admitted scan run; task-state polling no more often than every five seconds and never beyond its fixed total quota |
| SQL observers | At most six sequential read-only connections, each with 10-second command/statement limits and a bounded payload; frontend/backend closure recorded. No periodic SQL sampling during timed HTTP windows |
| Resource samples | Target a sample at the next controller opportunity after two seconds, at most 600 samples over business work; sample only bound APP/PG cgroups and owned process identities. At most 32 MiB of the raw evidence quota |
| Lifetimes | Preparation at most 300 seconds, business work at most 1,200 seconds, preservation/closure at most 900 seconds, handoffs at most 120 seconds, plus 180 seconds reserve: at most 2,700 seconds from the first anchor start. Retain the existing 3,600-second final anchor ceiling; no live fixture waits for operator development or independent review |

Freeze the actual APP lifetime and stop reserves so they cover its business work
and owned stop before the shared lifetime expires; do not inherit the installer
runtime's 900-second APP ceiling without reconciling this workload. Keep its
normal 20-second APP stop and the reviewed PG/anchor shutdown behavior. Any OOM,
automatic restart, deadline or quota exhaustion fails the affected execution;
do not raise limits while it runs.

The six SQL slots are fixed: prestart identity, empty catalog, completed cold
catalog, post-favorite baseline, completed cached catalog, and final post-APP-stop
state including credential revocation. Do not append an unbudgeted seventh
observer by inheriting the installer's final-sealer workflow.

Resource reporting should include cgroup `memory.current`, available
`memory.peak`, `memory.events`, CPU counters, and bound process RSS observations.
Report sampled RSS maxima as sampled maxima. Cgroup memory includes more than
RSS, and CPU quota constrains the result. Record available PG/media/evidence
space at admission, phase boundaries and closure. Missing metric files must be
resolved or explicitly marked unavailable before execution, never filled with
zero. Do not flush page caches or claim a cold filesystem cache: "cold scan"
means an empty Goby catalog/probe cache only.

The single-threaded controller may be inside a bounded HTTP/SQL call when a
resource sample would be due. Record each actual sample time, interval and gap;
do not promise continuous two-second coverage, fabricate missing samples, or
perform catch-up bursts. This needs no additional sampling process. Keep the
sampled RSS maximum distinct from an available cgroup lifetime peak.

## Stop, closure and result

Stop new read dispatches and phase transitions on identity/protection drift,
unknown admission outcome, unexpected task/scan ownership, ACL failure, malformed
or incomplete HTTP evidence, worker failure, application error/restart/OOM, or
time/space limits. Preserve the first failure. Independently bounded cleanup
still owns both reader groups, any identified task and its children, issued
credentials, APP, PG, anchor, mount and lock.

Join or terminate only the owned reader groups first. While administrator
authority and APP remain available, cancel the identified active run at most
once and poll its terminal state within cleanup quota; cancellation does not
roll back catalog additions. Never cancel an unrelated job or issue a second
task admission. Then close credentials before normal application stop. If a
normal prerequisite is unavailable, use the already reviewed owned failure
closure, retain the incomplete responsibility explicitly, and do not fabricate
HTTP revocation, normal exit, archive readback or successful unmount.

Within the 900-second closure phase, allow at most 15 seconds to join both active
readers, 60 seconds for owned task cancellation/drain, 20 seconds for normal APP
stop, 90 seconds for PG stop and 15 seconds for the anchor. Credential requests,
the final SQL slot, archive/readback and unit/mount/lock closure share the
remaining bounded phase time; no step resets the overall deadline. Reserve these
windows before every new task or reader dispatch. A forced process-group stop
is retained as a failed normal shutdown even when physical closure succeeds.

Archives remain remote root-private (directories 0700, files 0600). Preserve
master-bearing state by the existing whole-tree private retention route without
reading/hashing the key. Preserve file contents and use ordinary owned unmount
only after process/namespace closure; no recursive deletion or account removal
is required. Independent result review happens after owned runtime resources
have closed, against immutable receipts and the permitted current readback.

Publish one concise result with the bound artifact/profile/corpus; actual task,
job, request and worker counts; cold/cached durations; HTTP minimum/median/p95/
maximum with sample counts and the stated empirical quantile method; timeout,
error and overlap counts; resource samples; catalog/ACL/UserData outcomes; and
evidence/resource closure. Do not treat sums of concurrent latencies as elapsed
time or file bytes as actual storage throughput. Keep observed incomplete metrics
separate from product correctness failures and passing metrics.

The next executable deliverable is the small frozen workload/reader adaptation
and its input, not another general-purpose controller. Reuse unchanged package
and ordinary-suite evidence; no full product regression is required for a pure
operator/measurement change. A discovered production defect gets its own reviewed
fix and relevant verification before changed-product acceptance.
