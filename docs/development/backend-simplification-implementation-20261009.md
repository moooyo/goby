# Backend simplification implementation, 2026-10-09

## Scope

This implements the thirteen selected findings in the
[backend design reassessment](backend-design-reassessment-20261009.md), including
its already-reviewed Linux path cleanup. The delivery branch is
`codex/backend-simplification`, based on `main` at `7dba0880`. An isolated worktree
keeps unrelated original-workspace changes outside the tested and committed
source. The user selected remote verification followed by integration into main
and a push; deployment and image publication are not part of this change.

## Changes

| Finding | Implementation | Preserved boundary |
| --- | --- | --- |
| F01 | Retain successful embedded text-subtitle extraction bytes in the owning HLS session; 16 MiB per session and 64 MiB across the service. | Fresh source and authority checks, actual extraction-source/tool identity, final HLS revalidation and real process/input retirement. External, owned and dynamic subtitles keep their existing paths. |
| F02 | Use canonical bigint arrays for entity-page and Genre collage lookups. | Original physical text IDs, collision precedence, ACLs, authorized snapshot and page order. |
| F03 | Select at most 1,001 eligible episode IDs once, then project the ordered keys in the same transaction. | Full eligibility before the bound, exact 1,000-item limit, replay/user state and Repeatable Read snapshot. |
| F04 | Add schema 65's partial completed-run index and a PostgreSQL-17-generated recovery catalog. | Existing immutable migrations, history index, completion/ID ordering, active-run selection and recovery compatibility. |
| F05 | Map non-Season/Episode provider queries directly after checking cancellation. | Ancestor-query transactions and later source/revision/publication checks. |
| F06 | Skip an unchanged projected record clone and second serialization when all operations are terminal. | Extra reserve, payload validation and the independent transition capacity check. |
| F07 | Reuse one private parsed control record after fresh byte/hash/identity checks; seed only after successful durable CAS. | Current proof relationships, root/directory checks, uncertain-publication handling, reopening validation and independent payload copies. |
| F08 | Combine backup Verify's metadata/digest check and snapshot acquisition under one initial health inspection. | Error precedence, reader admission, lock-free archive hashing, final object check and snapshot retirement. |
| F09 | Maintain bounded queued counts by root/domain rather than rescanning waiters for capacity. | Existing fairness, composite admission, foreground reservations, active statistics and cancellation/Close behavior. |
| F10 | Remove residual Windows drive-name rejection from Linux scan evidence paths. | Root anchoring, absolute/traversal rejection, identity checks and all remaining path bounds. |
| F11 | Revalidate notification actors through the held transaction, explicitly using Read Committed. | Current-statement authority even with a different database default isolation, error handling and later send-time checks. |
| F12 | Deduplicate full notification-reference tuples with a transaction-private bounded set and stop at the 4,097 overflow sentinel. | First-insertion order, source scope, overflow/resync behavior and atomic rollback. |
| F13 | Sort backup inventory indices, select the page, then deep-copy only returned metadata. | Health checks, deleting-entry filtering, exact total, ordering and independent nested summary slices. |

F01 intentionally continues parsing and rendering per request. Its cache contains
immutable extraction bytes, not a shared mutable Document. Offset and producer
clock changes do not trigger extraction. The existing subtitle slots bound active
requests, and Stop/retirement clears retained bytes. A session cache does not
eliminate pre-session PlaybackInfo preflight work.
Extraction always uses the original configured invocation. PATH, alias and
noncanonical tool paths bypass this cache; the official software and AMD Docker
recipes use direct absolute installation paths and retain the optimization.

F07 preserves CAS's original returned payload representation. A successfully
encoded publication is parsed once for the cache, so subsequent Read and reopen
agree on the actual on-disk representation, including U+2028/U+2029 escapes.
Encoding-expanded payloads beyond the Read limit are not cached; the existing
complete-read rejection remains unchanged.

The F07 and F09 decisions followed remote baseline measurements. F07 parsing
dominated the larger control-record read fixtures. F09 capacity scans increased
with queue length, whereas the existing dispatch loop did not justify replacing
its fairness algorithm. Both changes remain local to their existing owners.

## Verification and observations

Final executable verification passed on `test-env`: 230 selected ordinary
top-level tests and 136 selected race tests, with zero skips. Race tests are a
subset of the ordinary coverage, not 136 additional distinct scenarios. The
previously failing cancellation case also passed three further race repetitions.
All `cmd` and `internal` packages built successfully, and the 46 edited/new Go
files passed remote formatting inspection. This is affected-scope testing plus
a complete production-package build, not execution of the entire repository suite.
No local tests, builds, validation suites or runtime probes were used. Ordinary
runs used pinned Go 1.27.1 with `CGO_ENABLED=0`; selected race runs explicitly
used `CGO_ENABLED=1` with the remote C toolchain.

| Package | Ordinary top-level passes | Race top-level passes |
| --- | ---: | ---: |
| primaryio | 52 | 52 |
| recoverycontrol | 22 | 22 |
| backupstore | 47 | 47 |
| database | 8 | Not selected |
| tasks | 1 | Not selected |
| backuppg | 5, including the current-catalog check | Not selected |
| library | 51 | Not selected |
| notifications | 18 | 6 |
| recovery | 9 | Not selected |
| server | 17 | 9 |

Baseline and candidate observations use the same benchmark source, flags and
selected fixture filesystem. They are controlled component observations, not
production HTTP latency or throughput claims. The task-index observation compares
schema 64 and 65 on the same bounded history fixture without forcing a planner
choice. The generated recovery catalog comes from a newly owned PostgreSQL 17.11
database, separate from the general tests and backup source/target databases.

Selected benchmark medians and query observations:

| Controlled observation | Baseline | Candidate | Scope |
| --- | ---: | ---: | --- |
| Control Read, 512 operation-shaped records | 2.519 ms; 29,837 allocations | 0.226 ms; 62 allocations | Includes fresh filesystem/hash/proof checks. |
| Control CAS, 512 operation-shaped records | 12.516 ms; 59,724 allocations | 10.306 ms; 29,962 allocations | Includes durable publication; fsync latency varies. |
| Queue capacity check, 127 mixed waiters | 2,648 ns | 44.1 ns | Mutex-protected capacity lookup, not end-to-end I/O. |
| Full queued admission/cancellation, 127 waiters | 1,156 ns | 998 ns | Includes counter maintenance. |
| Full queued admission/cancellation, no earlier waiter | 209 ns | 306 ns | Explicit low-queue maintenance cost; healthy bypass remains approximately unchanged. |
| Notification collector, 1,024 changes | 6.47 ms | 0.468 ms | Transaction-local reference construction. |
| Notification collector, 32,768 synthetic changes | 780.51 ms; 1.16 MB/op | 0.985 ms; 2.47 MB/op | Collector-only stress case; not an assertion that a scan admits this many items under its independent byte budget. |
| Backup list, one returned entry from 128 objects | 480,622 B/op; 551 allocations | 35,288 B/op; 297 allocations | Includes the existing complete health inspection. |

The notification set trades bounded extra allocation for less comparison work;
B/op is not retained-memory usage. Queue bookkeeping has a measurable cost at low
queue depth, so no universal speedup is claimed. Small-payload CAS timing also
varies with durability I/O.

On the 32,008-row task-history fixture, the completed-run lookup changed from
Seq Scan plus Sort (32.863/34.500/32.558 ms; 1,186 shared hits) to the new Index
Scan (0.040/0.047/0.031 ms; four shared hits). The index occupied 3,653,632 bytes.
Its storage and write-maintenance costs remain part of schema 65; these query
observations do not establish a production polling or HTTP speedup.

## Implementation review

Independent reviewers cross-check each other's implementation. Their scope
includes authorization time, source identity, cancellation versus actual resource
completion, memory/queue limits, ordering/snapshot semantics and schema recovery.
The review does not treat wording clarifications or synchronization of copied
evidence as new implementation defects.

| Round | Result | Consecutive clean rounds |
| --- | --- | --- |
| 1 | Cross-review found mixed notification-registration snapshots; the first remote recoverycontrol run found a Unicode seed representation mismatch. Both were repaired with regression tests. | 0 |
| 2 | Cross-review found the stale-peer disable write and symlink-wrapper invocation change. Both were repaired with regression tests. | 0 |
| 3 | Rechecked revision fences, source/tool witnesses, CAS representation, limits, snapshots, memory and retirement. No new issue. | 1 |
| 4 | Static boundary checks were clean; runtime race verification found an invalid global WaitGroup wait in the new HTTP tests. The harness now observes each exact handler's completion. | 0 |
| 5 | Independently checked the handler completion barrier, final publication fences, queue/map bounds and unchanged query/recovery contracts. No new issue. | 1 |
| 6 | Reconciled final source, test phases, catalog identities, benchmark claims and retained failed/passing evidence. No new issue. | 2 |
| 7 | Final independent source/caller/lifetime and test/report correspondence checks found no new issue. | 3 - stop |

Rounds 5-7 satisfy the three-consecutive-clean-round stopping condition. All
selected simplifications are implemented; no implementation finding remains open.

Failures and repairs remain separate from final passes:

- The first recoverycontrol run exposed a Unicode representation mismatch in
  the CAS-seeded parse cache. Seeding from actual encoded bytes fixes it while
  preserving the old CAS return value and Read limits.
- Cross-review found two notification races introduced for deployments whose
  previous default isolation was Repeatable Read: mixed registration snapshots
  could skip events, and a rejected old peer could disable a new registration.
  The first read now binds revision, and disabling a registration uses that
  revision as a compare-and-swap condition. Both have deterministic regressions
  using real registration changes.
- Cross-review found that resolving and executing a symlink target changed a
  wrapper's invocation semantics. Original invocation is restored and aliases
  bypass caching; a real shell-wrapper regression covers both failure and success.
- Race verification found that two new tests waited on the live global request
  WaitGroup without an admission fence. They now wait for completion of their
  exact real HTTP handler. Production code and retirement assertions are retained.

## Evidence and capacity

The remote task root is `/opt/goby-backend-simplification-20261009-08`. Local raw
evidence is retained under `.artifacts/backend-simplification-20261009` in the
implementation worktree. Database connection secrets remain in private remote
environment files and are excluded from source delivery.

Initial phases reused `GOCACHE=/root/.cache/go-build` and
`GOMODCACHE=/root/go/pkg/mod`. Compiler and executable scratch use task-owned tmpfs paths. Native execution
binds both `GOTMPDIR` and `TMPDIR` to the task's ext4 fixture directory. No module
or compiler cache is copied per task. Baseline hardlink overlays replace files
after unlinking so the retained baseline cannot be modified through shared
inodes. Source archives, raw failed/passing evidence and database fixtures are
retained separately from disposable compiler output.

The shared build cache reached the initial 400 MiB growth budget, then the
revised 650 MiB budget after additional package/race compilation. New admission
stopped at each guard. With persistent availability near the retained 1 GiB floor,
the final phase explicitly selected an empty private tmpfs cache at
`/tmp/goby-backend-simplification-20261009-08-final-cache`; shared modules remained
unchanged. This was a documented environment change, not a copied cache or a
shared-cache purge. Its budgets are 1.25 GiB cache, 768 MiB compiler/current
binaries, at least 512 MiB tmpfs availability and at least 2 GiB available RAM.
Native fixtures retained ext4. Already-passed unaffected packages were not rerun
merely because the compiler cache changed.

After fresh worker-exit inspection, 559 MiB of owned completed compiler/binary
scratch was reclaimed. The completed baseline and catalog source snapshots were
combined into a retained archive, checked across 5,537 entries, then their exact
duplicate expanded trees were removed. Their measured allocation changed from
about 59.04 MB expanded to 8.06 MB archived; filesystem availability is recorded
separately because unrelated activity can change it. Original archives, overlays,
raw evidence, databases and the final candidate source remain retained.

The final closeout receipt records worker liveness, exact private-cache/scratch
cleanup targets, allocated bytes and both persistent/memory availability. The
source-correspondence receipt binds the tested 2,763 Go/SQL/JSON/module/helper
files to the delivery commit's archive, without allocating another persistent
expanded checkout.

## Git delivery

The selected delivery is a fast-forward integration into main after final commit
source correspondence, followed by a push. The external Git delivery receipt
records the exact tested, integrated and pushed identities. Existing unrelated
original-workspace edits are preserved outside this commit.
