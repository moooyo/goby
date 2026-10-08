# Backend design fixes, 2026-10-08

## Scope

This work rechecks and implements the findings in
`backend-design-review-20261008.md`. It starts from
`8d9a63b1af90bebf907ed26b0b34d1b6ec8df764` and preserves the pre-existing dirty
work. Diff totals against HEAD include that earlier work and are not a measure
of changes introduced by this task.

The implementation loop consists of focused fixes, remote verification, and
independent review. It is complete: accepted findings have been handled,
applicable final checks passed, and review rounds 10-12 found no new actionable
issue. The evidence below distinguishes complete package runs, targeted
regressions, compilation, historical failures, and skipped scenarios.

## Verification environment

All builds, tests, query plans, and runtime checks execute through `ssh test-env`.
No local verification is authorized or performed.

- Task root: `/opt/goby-backend-fixes-20261008-01`.
- Go: `/opt/goby-toolchains/go1.27.1/bin/go`, observed version 1.27.1.
- PostgreSQL: observed 17.11; ordinary integration tests create unique owned
  schemas in the existing test database. Recovery-database tests use a separate
  task-owned database pair with port 5432 specified explicitly.
- Shared `GOCACHE`: `/root/.cache/go-build`.
- Shared `GOMODCACHE`: `/root/go/pkg/mod`.
- Initial compile/test `GOTMPDIR` and `TMPDIR`: the task's `fixtures` directory
  on ext4. Later fixture runs also use the short physical path `/opt/gf8` on
  the same ext4 filesystem to keep Unix socket paths within their length limit.
  This path correction does not change the selected fixture filesystem.
  Compiler scratch and retained evidence remain separate.
- The main media-test FFmpeg remains the full 9.0.1 build. Intro-skipper tests
  additionally select `/usr/bin/ffmpeg` through `GOBY_INTRO_SKIPPER_FFMPEG`;
  its configuration and hash were checked against the release image's recipe.
  This is an explicit component-specific fixture configuration, not a change
  to the main FFmpeg selection.
- Initial persistent availability: 6,399,229,952 bytes. Initial available memory:
  6,665,125,888 bytes. These observations are not reservations.
- Budget: source/archive up to 0.4 GiB, shared-cache growth up to 2 GiB,
  temporary fixtures/compiler output up to 1.2 GiB, retained logs/binaries up to
  0.5 GiB. Package concurrency is two, with `GOMAXPROCS=2`.
- Before final verification, persistent availability was 3,936,534,528 bytes
  and the shared build cache occupied 2,404,237,312 allocated bytes. The allowed
  cache-growth budget was revised to 3 GiB for focused race and compatibility
  builds, preserving the fixture budget and at least 1 GiB of planned headroom.
  The task record retains that reassessment; no shared cache was cleared.

Raw evidence is retained under the remote task's `evidence` directory; local
archives and retrieved evidence are under
`.artifacts/backend-design-fixes-20261008`.

## Outcome

All 30 findings and the subsequent accepted review findings have implementations.
Production source is frozen at verification snapshot 15. Its focused anchor and
reference-retirement selection passed 104 tests, and `library-race-final1`
passed 125 tests plus the package result. The complete `library-full-final2`
run passed 4,690 tests/subtests, with 14 skipped, in 903.489 seconds. Final Linux
backend and command test compilation and Windows/amd64 command builds passed.
Environment closeout confirmed worker exit and empty temporary directories.

Review rounds 10, 11, and 12 reported no new finding across authorization,
performance, and lifecycle. They are three consecutive complete empty reviews
after the round-9 P3 reference-retirement repair. Verification snapshot/run
numbers and review-round numbers are separate sequences. The implementation
table below does not imply that all historical checks ran on one final snapshot.

| Finding | Implemented change | Retained boundary or deliberate limit |
| --- | --- | --- |
| R01 | Reuse a parsed login policy within the same observed account snapshot. | Keep malformed-policy rejection, fresh clocks, device/peer checks, and revocation checks; no cross-request policy cache. |
| R02 | Diagnostic reads use the pure administrator check without the redundant lock-and-recheck sequence. | Keep credential audiences, checks around file access, periodic download checks, and deadlines. |
| R03 | Ordinary user-directory count and page share one SQL statement; zero limit reads only the count. | Keep deterministic ordering and conservative Go visibility projection for hidden/malformed policies. |
| R04 | Concrete root closes, internal pin releases, and borrowed witness references retire synchronously. | Actual origin cleanup and arbitrary callbacks retain the required abnormal-exit isolation; close errors and unknown retirement still propagate. |
| R05 | Explicit browse projections omit private seek indexes, including dedicated collection and episode-queue routes. | Default domain reads remain complete; playback and source validation keep their required media facts. |
| R06 | Batch collection records, ACL-filtered counts, sharing rosters, and CollectionFolder facts by page. | Restrict rosters to authorized managers and preserve duplicate playlist entries and page order. |
| R07 | Skip disabled image projections and batch genre collage facts/representatives. | Keep subject ACLs, managed Primary tombstones, source precedence, ordering, and revision/tag semantics. |
| R08 | Keep fixed identity checks per backup chunk; audit inventory at boundaries and bounded byte/time checkpoints. | Publication, management, and lifecycle audits remain complete; unknown-file detection may wait until the next checkpoint. |
| R09 | Use a non-authoritative pool query to avoid owned dispatch admission when no schedule or system event is due. | Keep authoritative ordered locks, current due checks, and selection inside the owned transaction. |
| R10 | Skip parent run writes when locked aggregates and persisted state are unchanged. | Retain the second aggregate because concurrent child completion can change its result; terminal transitions and side effects remain guarded. |
| R11 | Migration 62 returns early for disabled transport before the notification journal lock. | Keep transport SHARE admission and enabled sequence/subscriber/cursor ordering. |
| R12 | Empty notification source pages return before the second transaction. | Nonempty and filtered-only pages still advance their cursor atomically with enqueue work. |
| R13 | Maintain media-policy lease membership by owner. | Keep expiry, deterministic retention under tightened limits, and bounded global-capacity cleanup. |
| R14 | API-key metadata List/Get retain shared actor locks without the global management lock. | Reveal and mutation operations retain management admission, lock ordering, auditing, and final authorization. |
| R15 | Share one ancestry scope and the nearest-rating projection across content predicates. | Preserve true/false/NULL behavior, cycles, tag/folder restrictions, and collection exceptions; remote equivalence and plans were checked. |
| R16 | Prepare an immutable route once for trusted readers. | Mutable public routes still copy/validate; every chunk still participates in admission, fairness, cancellation, and retirement accounting. |
| R17 | Compute NextUp candidate keys once for count/page selection, with explicit count-only and empty-page paths. | Keep the repeatable-read authority snapshot, ParentID history semantics, nested series, and special-episode ordering. |
| R18 | Migration 63 caches the exact existing source-revision expression at source writes. | Read current root bindings and fall back on a stale binding; preserve v1 values, publication fences, and backup/restore validation. |
| R19 | Enqueue each source under a savepoint and commit a successful prefix before capacity exhaustion. | Roll back failed coalescing before cursor advancement; existing pending delivery can continue and no unseen source is skipped. |
| R20 | Read one verified lifecycle snapshot for active configuration, generation files, and the selected master-key path. | Verify all registered history once and preserve ownership, activation proof, CAS, and master-byte cleanup. |
| R21 | Batch minimal remote-Play permission facts separately for controller and target. | Require complete ID coverage, retain target CanPlay semantics and error ordering, and repeat authorization at delivery. |
| R22 | Activity pages use a bounded pool transaction outside the catalog write-owner gate. | Retain generation admission, administrator SHARE locks, final authority checks, request cancellation, and one-statement count/page consistency. |
| R23 | Read the notification target with its unchanged single SELECT directly on the pool. | Keep all predicates, error mapping, and delivery-time revalidation; no target authorization cache. |
| R24 | Release the actual I/O lease before database-only sidecar retry refresh while retaining the owner. | Merge request/owner cancellation; external requests retain fresh authorization and internal scans retain their startup grant. |
| R25 | Single-session revocation uses account SHARE locks before session UPDATE locks. | Keep management serialization, deterministic ordering, account-mutation exclusion, fresh authority, and the exact self-revocation exception. |
| R26 | Plan downloads from a database snapshot before the one actual registered source open. | Preserve independent download authority, ETag/source/path checks, bounded opening time, long response lifetime, and actual cleanup after timeout. |
| R27 | Restore bounded ordinary scan checkpoints and terminal/repair flushes. | Preserve the operation startup grant, cancellation, current mapping/source checks, and publication fences without per-phase approval reads. |
| R28 | Application-key revalidation restores the trusted authenticated PeerIP. | Keep key/client revocation and full dynamic-owner comparison; HTTP regressions exercise negotiation without DeviceProfile. |
| R29 | Retire completed per-file probes and retain one explicit auxiliary-group publication lifetime. | Keep complete-group atomicity and source witnesses; capacity retry rolls back and retires before waiting/rebuilding, without raising the global owner limit. |
| R30 | Restore retained job-lifetime source-I/O ownership and runner phase injection for local transcodes. | Keep ownership through queued/running/final cleanup, propagate unknown retirement, and preserve the separate network-stream path. |

The first compile sweep exposed additional incomplete media API migration;
profile/lifetime and seek-retirement adapters were repaired. The second sweep
passed media/transcode compilation and exposed a missing scan NFO adapter.
These failed logs remain retained and are not replaced with successful results.
The fourth compile sweep passed every backend and command package, including
test compilation. Later regression runs and review repairs supersede that
compile-only milestone. The final current-source library and backend compilation
results are recorded below.

## Independent repair review, round 1

Four issues were identified and repaired. This was a nonempty repair round and
did not start the consecutive-empty-round count:

1. Internal scan authority must obey the selected October 4 operation policy.
   The first implementation incorrectly treated the dirty per-phase permission
   reads as the intended contract and changed tests to require them. That change
   was rejected. The captured startup grant and its original tests were restored
   while keeping cancellation, current mapping/data/source identity, publication
   and actual resource-lifetime checks. This also corrects the earlier review's
   overly broad language about fresh authorization after every internal wait.
   External playback/download/provider requests retain their fresh checks.
2. Auxiliary final publication now preserves capacity-error identity, fully
   rolls back and retires the old witness, waits outside the owned transaction,
   and rebuilds/retries. Temporary saturation must not fail or silently skip
   a legal group. Subsequent scan regression repairs are recorded below.
3. Explicit browse projection now reaches the dedicated CollectionItems and
   EpisodePlaybackQueue HTTP routes, retaining complete default domain reads.
4. The sorting journal-budget test explicitly enables transport; disabled
   transport now intentionally skips that journal lock.

## Repair review, round 2, and regression follow-up

The first full backend run, `backend-full-round1.jsonl`, ended with 29 package
passes and nine package failures. Its raw JSONL and exit record are retained.
Repair run 6 reran the failed selectors in the eight non-library packages and
all eight returned exit zero. These targeted passes do not replace the failed
full run or establish a full-suite pass. The focused round-7 library run,
`repair-round7-library.jsonl`, returned exit one with two then-remaining failures:
the historical metadata-migration fixture had a stale table inventory, and
the Store-close actual-file-descriptor fixture lacked its required root witness.
Both were corrected and passed in subsequent focused runs. The other original
scan failures and the new source, progress, capacity, mapping, and candidate
regressions passed in that focused
run. The failed run remains retained as historical evidence.

Regression investigation included real implementation/adapter repairs and
fixture/environment corrections. Historical tests were corrected only when
their setup or observation contradicted the established contract:

- The HTTP session-revocation observer now recognizes the actual credential
  UPDATE wait behind the activity writer, without requiring the removed account
  UPDATE lock or a fixed SELECT projection. It still proves no socket/HLS
  disconnect before commit and exact-owner disconnection afterward.
- The notification journal-budget fixture explicitly enables the transport
  whose journal lock it intends to test.
- The transcode same-tick fixture was corrected without weakening source-lifetime
  and retirement assertions; the focused case passed with `-count=50`.
- Unix-socket fixtures use the short physical ext4 path, intro-skipper selects
  its release-matched FFmpeg configuration, and recovery-database fixtures use
  their explicit owned database pair and port. These corrections do not justify
  relaxing product authorization, filesystem, or lifecycle behavior.

Expected outcomes must continue to assert public behavior, durable commit
ordering, current source identity, and actual resource retirement. A test must
not be rewritten to approve a new policy interpretation. In particular,
internal scan approval is the captured operation-scoped startup grant from
`scan-operation-authorization-20261004.md`; it must not become fresh approval or
authorization-configuration reads at every phase. Cancellation, physical
mapping, source identity, and publication fences remain live checks.

| Round-2 review area | Result at that review checkpoint |
| --- | --- |
| Identity and server permission/read paths | Zero new findings in the reviewed stable changes. |
| Query projections and source-revision cache | Zero new findings in the reviewed stable changes. |
| Notifications, tasks, storage, and lifecycle | Zero new findings in the reviewed stable changes. |
| Scan checkpoints, source witnesses, and auxiliary publication | Review was incomplete; later findings and repairs are recorded below. |

These partial zero-finding reviews did not constitute a completed empty round.

## Repair review, round 3: actual I/O and cleanup boundaries

An independent scan-lifetime review found four additional implementation issues:

1. Acceptance repeated a descriptor `Stat` after the probe's admitted phase had
   retired. The probe already performs the same first-observation proof inside
   its phase, and final publication repeats the live source proof. Remove the
   redundant read rather than admitting another phase for it.
2. Theme deferred/input-warning branches could absorb rollback or witness-close
   errors joined with an otherwise recoverable source error. Cleanup failures
   must remain fatal; only a pure, known source-instability result may warn.
3. Auxiliary directory and absence preflights bypassed actual I/O admission and
   discarded some close errors. Those bounded reads must use the retained
   startup grant and actual root I/O phases, independently of SQL publication.
4. A retained auxiliary witness could continue through an old configured anchor
   after the ancestor path was renamed and replaced. Its final named-anchor
   proof now checks the configured pathname as well as unchanged child facts.

The review also identified removed late-group atomicity coverage. Dedicated
tests now cover 65-item theme/extra groups with a final probe failure, mutation
of an already retired first source, or actual cancellation; 256-item warm scans
retain their no-reprobe/no-update expectations. A separate real-filesystem
barrier test keeps the original registered child inode and ctime unchanged while
replacing its configured ancestor.

The same admission audit also restored per-directory walker phases, directory
evidence context, inode-rename candidate observations after SQL rows close, and
ordinary reads of legacy/unavailable roots. These reuse the complete startup
grant and do not acquire hidden fresh authorization at each phase.

Remote round 8 passed all 81 selected tests/subtests, including late-group
failures, 256-item warm scans, ancestor replacement, metadata migration, and
Store-close descriptor retention. Round 9 passed 540 selected tests/subtests;
four optional performance profiles were skipped. Its exercised cases include
the restored walker, cross-root rename candidate, legacy-root admission, and
reconciliation proofs. Both raw JSONL files are retained.

## Repair review, round 4: root handoff and primary publication

The next independent round found two additional boundary defects, so it cannot
count as an empty round:

1. A verified-root callback could create a clone successfully, then return a
   cancellation error added by `PrimaryRootIO.Run`. Its caller discarded the
   non-nil clone on error. Individual capture retirement errors were also lost.
   Root opening now shares one failure-handoff helper that closes the clone,
   returns nil, and propagates cancellation and retirement errors. Deterministic
   real-clone cancellation and retirement tests passed remotely in
   `root-handoff-round10.log`.
2. Cached/synchronous primary publication without pipeline authority could
   read from a retained old root after metadata admission waited. Sidecar
   candidates could bypass the narrower no-payload proof, and the final fallback
   transaction checked only the database mapping. The repair proves the current
   named root before metadata-derived writes and before final commit, with
   actual I/O admission and transaction-safe immediate retry behavior.

Both defects were repaired and received focused remote coverage. This review
round reset the empty-round counter rather than advancing it.

## Review rounds 5-7: caller audit and shared source-root witness

Primary named-root regressions passed five repeats after correcting one test
precondition: renaming a file and changing its mtime deliberately creates a new
scan candidate, so that specific cold fixture must not require the prior item
ID. All source, hierarchy, rollback, counter, and notification assertions remain.
Cleanup is registered before fixture precondition assertions.

Subsequent reverse call-path audits found the same original-anchor gap in
source-backed folder NFO publication, images, text and bitmap subtitles,
embedded artwork, and auxiliary witness creation before publication. A later
pathname sample cannot become a replacement for the original physical anchor.
The combined record for rounds 5-7 contains new findings and repairs, including
the root agent's folder finding after parallel reviewers had initially reported
no new issue. It is not split into invented per-round finding counts, and it
does not supply consecutive-empty-round credit.

The final implementation uses a private `scanSourceRootWitness` shared by these
callers. It provides no authorization or I/O admission of its own. It borrows or
retains the original anchor and registered-root identity, checks them only inside
actual I/O phases, and gives workers independent borrowed lifetimes. Ordinary
verified roots reuse existing captures. Individual roots retain only lightweight
identity handles, not their complete topology captures. Legacy roots preserve
the original anchor at their first admitted open rather than resampling it later.

Source-backed folders check metadata observations and use an immediate final
proof before commit; pure virtual folders retain their no-filesystem path.
Sidecar cache/no-write returns remain fast. Auxiliary groups retain the original
proof before retiring each input, including completed empty roots needed by a
collection. Collection ownership transfers explicitly, retains the existing
256-root publication bound, and shares one owner rather than registering one per
file or root. No fresh per-phase permission query or full topology check is added.

The earlier frozen library snapshot passed its complete package run in
`library-full-final1.jsonl`: 4,650 tests/subtests passed and 14 were skipped.
This pass precedes the shared-witness caller repairs and is not represented as
their final integrated validation. The complete `primaryio` race run also passed.
The shared-witness source was then checked in `anchor-round13.jsonl`. That run
exposed an invalid capacity-test setup: the fixture exceeded CreateLibrary's
existing 32-directory contract. The corrected fixture uses 31 resource roots
with three resources each plus one empty root. It still exceeds the 64 retained
owner limit while staying within the real directory contract; the production
limit and the complete-group assertions were not relaxed. The corrected case
also received repeated coverage in `collection-owner-repeat-round14.log`.

## Review round 9: borrowed-reference retirement

Review round 8 was a complete static review with no new finding. Round 9 then
identified one P3 regression in the shared witness: closing a borrowed handle
used the generic callback-isolation path and created a goroutine even though
its only immediate work was releasing an internal reference. That new finding
reset the empty-round counter.

The fix gives `storageObservationLifetime` a typed reference-retirement path.
Borrowed witness Close performs synchronous reference accounting and keeps
active workers attached to their origin. The final origin still closes real
resources through the required isolated callback path, preserving errors,
panic/Goexit handling, and unknown-retirement propagation. Regression coverage
checks zero allocation for an ordinary borrowed Close, late-worker ownership
after origin Close, idempotent releases, and abnormal final-origin cleanup.

Verification snapshot 15 includes that repair and the corrected directory
fixture. `anchor-round15.jsonl` passed all 104 selected tests/subtests. Production
source was then frozen for the final integrated and compatibility runs.

## Review-loop ledger

Only a completed review of all assigned areas can advance the empty-round
counter. A partial reviewer result, an in-progress verification run, or a new
accepted repair does not qualify. Verification run names such as round 13 or
round 15 are evidence identifiers, not review-round numbers.

| Review round | Outcome | Consecutive complete empty rounds |
| --- | --- | --- |
| 1 | Four accepted repairs: startup-grant semantics, auxiliary capacity retry, dedicated browse routes, and notification fixture setup. | 0 |
| 2 | Stable permission/query/notification/task/storage/lifecycle reviews had no new findings, but scan review was incomplete. | 0 |
| 3 | Actual-I/O, cleanup, and original-anchor findings required repairs and additional group-failure coverage. | 0 |
| 4 | Clone failure handoff and primary named-root publication findings required repairs. | 0 |
| 5-7, combined record | Reverse caller audits found additional original-anchor gaps; shared source-root witnesses and their callers were repaired. No per-round counts are inferred. | 0 |
| 8 | Complete static review reported no new finding. | 1 |
| 9 | Borrowed Close created an unnecessary retirement goroutine; typed reference retirement repaired the P3 finding. | 0 |
| 10 | Authorization, performance, and lifecycle reviews all reported no new finding after the last production repair. | 1 |
| 11 | Authorization, performance, and lifecycle reviews all reported no new finding on the frozen production source. | 2 |
| 12 | Final authorization, performance, and cross-module lifetime reviews reported no new finding or independently removable redundant defense. | 3 |

The three-consecutive-empty-review condition is satisfied by rounds 10-12.
Final applicable verification and environment closeout also completed. No
accepted finding remains open within the reviewed scope.

## Measured query evidence

The remote query fixtures use PostgreSQL 17.11 with JIT disabled. These are SQL
fixture results, not complete HTTP throughput or capacity measurements.

- The combined content-policy fixture preserved all visible IDs and reduced
  recursive plans from seven to one (755 to 177 recursive loops); observed
  EXPLAIN execution time was 153.429 ms versus 39.487 ms. Other selected policies
  and exact true/false/NULL results also passed equivalence checks.
- For 1,604,243-byte media JSON, three samples of 128 source-revision reads
  reported 2043.272 ms total for the original expression and 0.605 ms for cache
  hits. The same fixture reported 5.714 ms for UPDATE and 10.697 ms for UPSERT.
  Source writes now maintain the cache; a root rebind safely falls back until
  the next source write. No end-to-end speedup is inferred from these numbers.

Raw query evidence: `query-measure-round1.log` and
`query-library-round2.log` in the remote evidence directory.

## Verification results and limits

- `internal/identity`: full package tests passed on the first source snapshot.
- `internal/primaryio`: full package tests passed on the first source snapshot.
- `internal/backupstore`: full package tests passed on the first source snapshot.
- `internal/lifecycle`: full package tests passed on the first source snapshot.
- Focused transcode source-lifetime/playback admission tests passed.
- Focused media tests passed with the existing full FFmpeg 9.0.1 recipe. The
  original environment's minimal binary lacked libx265 required by existing
  HDR fixtures; that failed run is retained as `media-round2.log`.
- Database migration tests passed, including schema 62 to 63 and rollback.
- Real PostgreSQL 17 schema-62/63 catalogs were generated in newly created
  task-owned databases. Cache snapshot/restore tests passed, including valid
  stale bindings, forged-cache rejection, and schema-62 archive migration.
- Focused library query, policy, NextUp, download planning, batch permission,
  and sorting journal-budget regression tests passed.
- The first full backend run recorded 29 passing packages and nine failing
  packages. `backend-full-round1.jsonl` and `backend-full-round1.exit` preserve
  that result, including failures subsequently diagnosed or repaired.
- Targeted repair-run-6 selectors passed in `backuppg`, `database`, `media`,
  `recovery`, `recoverydb`, `server`, `settings`, and `transcode`. The evidence
  files are `repair-round6-{package}.jsonl`; `repair-round6-exits.json` records
  zero for all eight. The transcode same-tick regression also passed 50 repeats.
- `repair-round7-library.jsonl` records a focused library run with exit one and
  two historical failures, described above and resolved in later runs. Its other
  original scan regressions and new source/progress/capacity/mapping/candidate
  cases passed. This is not a
  library package or integrated-suite pass.
- `anchor-round13.jsonl` retains the original directory-contract fixture failure.
  The corrected legal fixture received repeated coverage in
  `collection-owner-repeat-round14.log`; `anchor-round15.jsonl` then passed
  all 104 selected tests/subtests, including typed reference retirement.
- `library-race-final1.jsonl` passed the focused race selection: 125 tests/subtests
  and one package pass, giving 126 pass records. This is distinct from a complete
  library package run.
- The complete `library-full-final2.jsonl` run passed on frozen snapshot 15:
  4,690 tests/subtests passed, 14 skipped, and the package passed in 903.489
  seconds. The 14 skips include opt-in performance/memory/high-fanout
  experiments, a mount-namespace scenario, a nonroot permission-denial case,
  and an explicitly mounted read-only fixture. They are not counted as passes.
  The earlier `library-full-final1.jsonl` pass remains separate historical
  evidence.
- `primaryio-race-final.jsonl` passed all 79 tests/subtests under the race
  detector. The focused current-source library race selection is recorded above.
- `backend-compile-final2.log` passed test compilation for all `internal/...`
  and `cmd/...` packages on Linux. Its `-run '^$'` selection does not execute
  the backend test suites.
- `windows-build-final2.log` passed builds of all `cmd/...` packages for
  Windows/amd64 with CGO disabled. This is a cross-compilation result, not a
  Windows runtime test.

The initial remote delivery comparison checked 2,829 paths: 2,767 were identical,
61 differed only by gofmt, and one difference was documentation in
`internal/backuppg/README.md`. The confirmed formatting changes were imported
without overwriting documentation. A second remote comparison found 2,828 paths
byte-identical, zero missing files, zero formatting differences, and only that
document difference. A reverse inventory also found no additional Go or SQL
file in the tested tree. This comparison establishes source correspondence,
not an additional test pass. After verification finished, the updated README
was copied to the retained remote source. `delivery-final.json` then confirmed
all 2,829 delivered paths byte-identical, with no missing or additional Go/SQL
source. The comparison receipts remain in remote evidence.

Earlier focused and full-package passes describe their recorded source
snapshots. The full backend run with nine failed packages remains a failed
historical run; the eight non-library package repair runs were targeted
selections, not another complete backend run. No end-to-end HTTP performance or
Windows runtime claim is made. All verification ran on `test-env`.

## Environment and delivery closeout

The final process inspection found no active task Go, compiler, linker, test,
or media worker. Compiler scratch, the original fixture root, and `/opt/gf8`
were empty; their contents had already been reclaimed by their owners. Empty
roots were retained. No private build cache existed, and closeout itself
deleted no bytes.

The shared build cache occupied 3,821,428,736 allocated bytes and remained
unchanged by closeout. Observed persistent availability before and after
closeout was 2,328,444,928 bytes. Shared modules, tested source, archives,
databases, raw evidence, and Windows binaries remain retained. The remote
task record and `evidence/closeout-final.json` record these observations.

The final summary and retained raw logs, including failed attempts, are also
exported to `.artifacts/backend-design-fixes-20261008/retained-evidence` in the
working tree. The evidence export excludes separately retained Windows binaries
and never includes private database environment files.

At that closeout, the source base remained
`8d9a63b1af90bebf907ed26b0b34d1b6ec8df764` and changes were left in the working
tree. No commit, push, pull request, or deployment was performed. Existing
unrelated working-tree content was preserved.

## Delivery continuation, 2026-10-09

The user subsequently authorized a remote commit and the remaining verification.
The commit candidate excludes pre-existing native-media, HLS-maintenance,
candidate-hint, historical-documentation, and measurement-tool changes. Mixed
scan files retain HEAD's source/occupant fences, existing large-group tests,
checkpoint assertions, and known-absence image optimization alongside the
review-owned fixes. Those excluded working-tree changes remain with their owner.

The first selected candidate contains 165 paths on base
`8d9a63b1af90bebf907ed26b0b34d1b6ec8df764`; its Git tree is
`aa442d7091f28e632ffb889d02299309853cbc97`. It passed Linux compilation of all
backend and command tests. Independent static inspection found no dangling
dependency on the excluded changes. Because this candidate differs from the
earlier dirty snapshot, the earlier passing results are historical evidence,
not substitutes for the candidate's complete test run.

The remaining checks run on the designated Linux verification environment and
the user-selected Windows worker. Windows scope follows the supported platform
contract: native applicable unit tests, native command builds, help, and explicit
unsupported behavior. Linux-only server, storage, lifecycle, and transcoding
features are not represented as Windows production functionality.

Delivery verification completed with these results:

| Check | Result | Evidence |
| --- | --- | --- |
| Complete Linux backend matrix, excluding the independently provisioned recovery database package | 37 test packages passed; 21,376 tests/subtests passed; 136 conditional skips; three packages had no tests | `full-r1.jsonl` |
| Complete recovery database package with a new exclusive disposable pair | One package and 206 tests/subtests passed | `recoverydb-r1.jsonl` |
| Linux recheck of all tests in the Windows portability fixture files | Six packages and 698 tests/subtests passed; no skips | `portable-r1.jsonl` |
| Fourteen specified library opt-in tests | All executed and passed with no skips; prepared-plan memory used four independent groups | Nine `r1-*/phase-result.json` records and three special-test result records |
| Native Windows backend matrix | 36 test packages passed; 3,271 top-level tests passed; 914 top-level skips; five packages had no tests | `windows-portable-backend-round2` |
| Native Windows commands | All command builds and both help invocations passed; the non-Linux launcher returned its defined exit 126 | Retained native command logs |

The Linux default matrix therefore covers all 38 packages with tests, with
21,582 passing tests/subtests across the full run and the separate recovery
database run. Its default conditional skips remain explicit. The fourteen
specified library cases were subsequently executed separately: media/playback
revalidation, ordinary/real-probe/image/query-plan scans, descendant/query
reconciliation, high directory fanout, short proof, prepared-plan memory,
nonroot permission denial, a real read-only mount, and the private mount
namespace full-scan scenario. The latter completed all seven scans and its
own ordinary-unmount cleanup. The prepared-plan observer was removed.

Windows initially exposed test portability defects, including common helpers
hidden by a Linux build tag, platform-specific closed-file errors, invalid
UTF-8 environment conversion, and incompatible path fixtures. The final
27-file overlay changes tests only. Repeated-Close assertions still fail when
the production owner omitted its first Close; direct malformed-UTF-8 validation
retains the rejection checks. Thirty narrowly scoped top-level platform skips
cover actual Linux-native requirements; mixed files and portable tests remain
enabled. PostgreSQL-dependent Windows tests keep their explicit environment
skips. The complete native command was rerun successfully, rather than selecting
only previously passing tests. Its subtest-inclusive counts are 13,178 passes
and 1,357 skips. Original failures and an aborted setup attempt remain retained.

The tested fixture overlay produces tree
`7744e09529973fe535f4b6244c1868dd71a93e05`. Remote comparison confirmed that its
only differences from the complete Linux run are the 27 test files; production
Go, SQL, migrations, and catalogs are unchanged. Every overlay file matches its
Windows-tested bytes, and the affected Linux tests passed independently. Final
report edits do not change production source.

The Windows `serve` attempt exited 1 with an unclassified error and no detailed
diagnostic. It is not reported as a successful service startup or proof of a
particular rejection boundary. The repository's Linux-only production platform
contract remains unchanged; this work does not port the server/media runtime
to Windows.

Final inspection found no active task test, compiler, linker, or media workers.
Empty temporary fixtures and the redundant nonroot test-binary copy were
reclaimed after actual exit. Shared caches, source/Git objects, databases,
pinned media, binaries, and raw evidence remain retained. Linux evidence is in
the task's `delivery-20261009/evidence`; the local Windows record is under
`.artifacts/private/windows-backend-delivery-20261009`. Infrastructure details
and private database environment files are excluded from the commit.

Delivery branch: `codex/backend-design-fixes-20261009`. The commit is created
on the Linux verification environment and transferred back with a Git bundle.
Its identity and the preserved working-tree state are recorded in the delivery
receipt. Existing unrelated edits are not part of this commit.

For capacity, 305,092,929 bytes of this task's inactive input archives and prior
Windows cross-build outputs were relocated to a hash-verified local retention
archive. Current source, databases, raw logs, and shared caches remain remote.
The transfer manifest and receipt are retained separately from test results.
