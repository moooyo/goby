# Backend design reassessment, 2026-10-09

The subsequent [implementation record](backend-simplification-implementation-20261009.md)
tracks the selected simplifications, remote verification and Git delivery. The
findings below retain their review-time status and evidence boundaries.

## Scope and method

This review covers authorization complexity, defensive checks, backend
performance architecture, and the Linux-only server boundary. It reviews the
current working tree, including pre-existing uncommitted work. Existing changes
are preserved. Only the residual platform cleanup described in F10 is implemented
by this review; the other findings are recommendations.

The review is deduplicated against R01-R30, N01-N22, L01-L08, Q01-Q09 and S01-S09
in the earlier backend review and implementation records. An existing unfinished
recommendation is not counted again. Each subsequent round changes its focus and
challenges the previous findings. The stopping condition is three consecutive
complete rounds with no new actionable recommendation.

Findings below establish source-level work and access paths. They do not claim
measured latency, throughput gains, database plans or a complete backend test pass.
All executable verification for the actual cleanup runs on `test-env`.
The review completed six rounds. Rounds 4-6 introduced no new recommendations,
satisfying the stopping condition. There are 13 accepted findings: one implemented
cleanup and 12 proposed improvements, including two explicitly measurement-first
changes. No measured performance benefit is claimed for the proposals.

## Assessment

No new authorization mechanism was found that can simply be deleted without
changing current revocation, actor-isolation or publication semantics. The new
authorization-related simplification is to perform a current check through an
already-held transaction instead of borrowing a second connection (F11).

The strongest performance candidates concern repeated full-track subtitle work,
database access paths and duplicate candidate construction. The smaller recovery
findings reduce repeated work while retaining filesystem integrity checks and
uncertain-publication handling. The I/O scheduler proposal should start with
measurement and a small counter change, not a replacement scheduling framework.

| ID | Priority | Recommendation | Status |
| --- | --- | --- | --- |
| F01 | P2 | Reuse successful text-subtitle extraction across HLS segments. | Proposed |
| F02 | P2 | Keep bigint entity lookups indexable after page selection. | Proposed |
| F03 | P2 | Construct episode-playback queue eligibility once. | Proposed |
| F04 | P2 | Index the completed-run ordering used by task polling. | Proposed |
| F05 | P3 | Skip provider-query transactions that execute no SQL. | Proposed |
| F06 | P3 | Skip identical recovery capacity serialization for terminal operations. | Proposed |
| F07 | P3 | Reuse the parsed recovery record after fresh byte and identity verification. | Proposed; measure first |
| F08 | P3 | Combine backup Verify's metadata lookup and snapshot acquisition. | Proposed |
| F09 | P3 | Reduce repeated queue scans under the primary-I/O mutex. | Proposed; measure first |
| F10 | P2 | Remove residual Windows drive rejection from Linux scan paths. | Implemented; targeted remote checks passed |
| F11 | P3 | Revalidate notification actors through the transaction already holding a connection. | Proposed |
| F12 | P2 | Stop saturated notification-reference collection and use bounded tuple deduplication. | Proposed |
| F13 | P3 | Deep-copy only the requested backup inventory page. | Proposed |

## Findings

### F01: Reuse successful text-subtitle extraction across HLS segments

`internal/server/hls_generated.go:411-414` reaches
`serveGeneratedHLSSubtitle`, whose `hls_subtitle_clock.go:46-60` reads and parses
the entire subtitle document before rendering one segment window.
`internal/server/subtitles.go:133-142` extracts embedded text through
`internal/media/subtitle_extract.go:54-57`; that FFmpeg command has no window
selection. N segment GETs therefore repeat N full-track extractions and parses.
Conditional GETs also construct the result before comparing its ETag. Generated
HLS HEAD exits earlier and is not affected. PlaybackInfo preflight also extracts
content that later requests extract again. A session-owned cache alone does not
remove preflight work performed before that session exists.

Start with embedded text tracks only (`ExternalTag == ""`), retaining current
external/owned-sidecar reads. Use a bounded, session-owned successful result keyed
by physical source identity/revision, subtitle stream, actual extraction format
and tool settings.
Optionally retain an immutable parsed document. Keep fresh authorization/source
proof, producer/window clock selection and the final HLS revalidation. Bound
bytes both per session and across the service, plus concurrent producers; the
per-extraction 8 MiB bound is not an aggregate cache budget. Failed, cancelled,
closed-session or replaced-source work must not publish reusable results. Process
retirement must complete before releasing its budget. Offset/window/producer-clock
changes affect rendering, not extracted document identity. This is not a reason
to create a general cross-feature cache.
Bind the result to the actual extraction source witness, not merely the earlier
HLS session stamp, and match it at publication. Initially the existing extraction
slots can permit a small amount of concurrent cold duplication; a new shared
producer/singleflight framework is not required to reuse completed results.

Verify FFmpeg invocation counts across segments, track changes, offset changes,
conditional GET, source replacement, revocation and concurrent cancellation/Stop.

### F02: Keep bigint entity page lookups indexable

`internal/library/search_hints.go:251-253` compares
`entity.id::text=ANY($1::text[])` after choosing a page. The underlying entity
primary key is bigint, so the predicate cannot directly use its bigint equality
access path. `internal/library/collage.go:254-258` has the same expression in its
Genre fallback. A small output page does not bound the entity-table work.

Parse already-identified entity IDs into `[]int64` and compare
`entity.id=ANY($1::bigint[])`. Keep mixed physical IDs separate; do not cast
arbitrary input strings in SQL. Preserve exact textual-ID matching, physical
collision precedence, inaccessible-item handling, Genre restrictions and ACLs.
This is separate from L03's earlier name-filter pushdown.

Use large entity tables and small pages to compare actual remote plans and buffer
reads. Include noncanonical numeric strings, mixed IDs and collision cases.

### F03: Construct episode-playback queue eligibility once

`internal/library/episode_playback_queue.go:47-65` builds a recursive series
scope and eligibility filter for a count, then executes the same scope for the
queue projection. Empty queues still issue the second statement. The actual
entry point is `internal/server/episode_playback_queue.go`.

Within the existing transaction, read at most 1,001 ordered lightweight IDs once.
Keep the current error above the 1,000-item maximum; otherwise derive the count
from that list and project only those IDs in their selected order. Apply the
1,001 bound after complete eligibility, within the original transaction. Preserve series/library isolation,
ACLs, specials ordering, probe eligibility and the transaction snapshot. This is
the remaining episode-queue caller, distinct from the NextUp entry optimized for
R17. The count optimizer may prune unused window work; only duplicate recursive
scope and eligibility evaluation are asserted here.
The 1,001 limit bounds returned IDs, not necessarily the recursive traversal or
sorting work needed to choose them.

Verify empty, exact-limit and over-limit series, nested/cross-library series,
specials, revoked access and concurrent catalog changes.

### F04: Index completed-run ordering for task polling

`internal/tasks/store.go:104-106` selects LastRun by
`task_id, finished_at DESC, id DESC`, filtering out unfinished runs. The existing
history index in `internal/database/migrations/0019_scheduled_tasks.sql:115`
orders by `created_at`; later migrations do not provide the completed-time
ordering. The admin Tasks panel polls every five seconds
(`web/admin/src/ScheduledTasksPanel.tsx:86` and `useTaskResource.ts:45`), while task
history is retained. The latest-result lookup consequently grows with historical
candidates rather than having a matching first-row access path.

Consider a new partial index on `(task_id, finished_at DESC, id DESC)` with
`WHERE finished_at IS NOT NULL`. Retain the created-time index for the history
API. The visible UI schedules a new read about five seconds after a successful
request; it does not overlap requests and pauses on failure or when hidden.
Measure plan/buffer changes and index space/write overhead, and preserve the
ID tie-breaker, empty-history behavior and existing schema/recovery catalog rules.
Append the migration and publication manifest entry, and refresh the PostgreSQL
17 recovery catalog. The current migration runner is transactional, so a
`CREATE INDEX CONCURRENTLY` statement cannot be inserted into it unchanged.

### F05: Skip provider-query transactions that execute no SQL

`internal/library/provider_tasks.go:293-306` always starts and commits a read
transaction. `provider_metadata.go:310-312` immediately returns an in-memory
mapping for every type other than Season/Episode. Applicable background items
thus each pay a connection acquisition and two transaction-control commands
without a database read in between.

Check cancellation first and map those types directly. Retain the existing
ancestor lookup/snapshot for Season/Episode, or combine it with an existing
appropriate read transaction. Preserve metadata revisions, remote-call
cancellation and final write authorization. Network work can dominate total
task duration, so eliminating control commands does not imply a comparable
end-to-end speedup.

### F06: Skip identical recovery capacity serialization

`internal/recovery/control.go:154-175` detects nonterminal operations to reserve
completion space, then unconditionally clones Operations and marshals a projected
record. When all operations are terminal, `budgetOperationScalars` changes
nothing and the second serialization repeats the first. The operation history
is bounded at 512 records and the payload at 1 MiB.

Reuse a `hasNonterminal` fact to skip only the unchanged projected clone/marshal.
Keep initial validation, `extraReserve`, payload limits and the separate
`checkTransitionCapacity`: terminal operations do not prove Transition is absent.
Retain the existing completion-reserve and transition-return-path tests.

### F07: Reuse parsed recovery records after fresh integrity verification

`internal/recoverycontrol/store_linux.go:276-301` reads and hashes the current
file, validates its before/after identity and compares it to `s.currentFile`.
Even when those facts match, each Read/CAS repeats JSON decoding, canonical
re-encoding, duplicate-key traversal and payload compaction. N17 removed a
duplicate digest computation; it did not address this parsing/allocation work.
Ordinary Operations-list polling reads the manager's in-memory state, so this
proposal does not accelerate that polling path; keep it a measured P3 change.

If profiling justifies it, retain one private immutable validated record for the
current exact file identity and digest. Continue fresh full-byte reading/hash,
root/proof checks, directory validation and CAS checks. Fully validate first
open, new records and reopen; never advance cached authority after an uncertain
publication. Return independent payload copies. This is a bounded one-record
optimization, not a replacement for integrity verification.

Validate malformed/duplicate keys, noncanonical metadata, payload-copy isolation,
changed bytes/identity, all publication failures and reopening behavior.
The current record/proof relation must be checked every time, even on a parsing
cache hit: a failed current-file rename can leave a new proof beside the old
current record. Only parsed JSON is reusable, never that authority relation.

### F08: Combine backup Verify metadata lookup and snapshot acquisition

`internal/backupstore/reader_linux.go:55-62` calls Get and then Snapshot. Both
independently take `Store.mu` and perform a complete `healthy` inspection. The
restore-plan path reaches this via `internal/recovery/plans.go:242`.

Combine one initial health check, catalog lookup, expected-digest comparison and
snapshot FD acquisition in a single lock scope, using a private Snapshot helper.
This can remove one bounded directory inspection, fixed metadata checks and an
unused deep copy. It does not eliminate archive hashing or the post-read health
check at `reader_linux.go:77-89`.

Preserve wrong-digest `ErrConflict` precedence, readiness/reader limits,
cancellation, file identity and permissions, reader registration and the final
publication checks. Do not skip a check across an unlocked state-change boundary.
The fixed savings may be small relative to hashing a large archive.
Do not hold `Store.mu` during hashing or snapshot close; the snapshot release
callback also needs that mutex. Recheck cancellation before snapshot acquisition.

### F09: Reduce queue scans under the primary-I/O mutex

`internal/primaryio/admission.go:248-279` scans waiters for every route root and
domain during admission. `dispatchLocked` at line 320 scans candidate requests
and their earlier conflicts, allowing quadratic work in queue length under the
shared mutex. Chunk-based reads repeatedly enter admission/release. Production
currently admits at most 128 queued requests and 32 per root/domain
(`internal/library/primary_read.go:135-140`); the configurable 4,096 ceiling is not
the current deployment size.

First measure contention and maintain exact per-root/domain queued counts to
remove repeated capacity counting. Consider a conflict index only if the remaining
dispatch traversal is significant under realistic mixed-root load. Preserve
composite atomic acquisition, older-request fencing, foreground reservations and
rescheduling after cancellation. Avoid trading a small bounded queue for a more
complex scheduler without measured benefit.

### F10: Remove residual Windows drive rejection from Linux scan paths

`internal/library/scan_reconciliation_evidence.go:679` rejected every relative
path whose second byte was a colon, including valid Linux names `C:movie.mp4`
and `C:/movie.mp4`. That caused scan reconciliation evidence to become unavailable
for legitimate paths. The Windows volume rejection was also inapplicable.

The cleanup removes both drive-specific checks and simplifies the root-directory
guard in `internal/library/scan_evidence_store.go:78`. Physical access remains
relative to registered `os.Root` handles. Absolute paths, traversal, backslashes,
NULs, path lengths and depth limits remain checked. The existing physical-path
test now accepts those colon names and rejects colon-containing traversal cases.

No Windows server implementation remains to delete in cmd/internal. Server
entry points already select Linux. `launcher_other.go` is Linux non-amd64
rejection logic, not Windows support. Client platform fields, developer-host
tools, media time windows and historical evidence are retained.

### F11: Reuse the notification fanout transaction for revalidation

`internal/notifications/queue.go:99` begins a transaction, then line 113 invokes
`s.users.RevalidateSession`, which queries through the identity store pool
(`internal/identity/session_revalidation.go:25-26`). Server construction injects
the same Data pool. One connection is therefore retained while a second is
acquired, including during empty-source polling. The production Data budget is
12 connections; this adds occupancy and a pool-wait dependency, not a proven
deadlock.

Use the existing `identity.RevalidateSessionInTransaction` through the held
transaction and explicitly select `pgx.ReadCommitted`. Current code uses
`pool.Begin`, and DSN/server settings can change the default isolation level;
blindly reusing a Repeatable Read transaction would reuse an earlier snapshot.
The independent authorization SELECT must retain a current database observation.
Keep all expiry, peer, device, policy and
revocation checks, as well as later reference filtering and send-time checks.
This changes connection ownership, not the number or strength of authorization
boundaries. Verify small-pool concurrency, revoked/expired sessions, disabled
users, disallowed peers, rollback paths and a nondefault server isolation setting.

### F12: Stop saturated notification-reference collection

`internal/library/notification_journal.go:41-52` scans all accumulated references
for every new Item/parent tuple. At 4,097 references the collector stops appending
but continues the same full scan for each remaining input.
`catalog_changes.go:133` collects notification references before its separate
1,024-change limit. `scan_reconciliation_commit.go:20,536-549` passes a batch's
visible facts into this code while the owned transaction/catalog gate is held.
Its item ceiling is 32,768, additionally constrained by a 32 MiB memory budget;
that item ceiling is not an independently attainable batch-size promise.
The bounded work is O(K * min(K, 4097)). Even 1,024 changes producing 2,048 distinct
item/parent tuples incur millions of comparisons. These are structural counts,
not measured durations.

First stop collection once the 4,097-reference overflow sentinel is reached.
For unsaturated batches, use a transaction-owned bounded set keyed by the complete
`notificationjournal.Reference`, while retaining the insertion-ordered slice.
Unify the similar append path in `metadata_sorting.go:143-149` and account for the
direct append in `analysis_sources.go:554`; otherwise an auxiliary set can become
inconsistent. Preserve exact Kind/ID/LibraryID/SourceID tuples, source-bound
visibility, first-insertion order, overflow/resync behavior and rollback isolation.

Verify repeated IDs with different source scopes, shared parents, direct append
callers, the exact 4,096/4,097 boundary and the largest budget-admitted reconciliation batch.
Measure owner-gate hold time as well as allocation and comparison cost.

### F13: Deep-copy only the requested backup inventory page

`internal/backupstore/store_linux.go:337-355` deep-copies every nondeleting catalog
entry while holding `Store.mu`, then sorts and slices the requested page.
`copyMetadata` also clones `Summary.Tables`. The production path is
`adminBackups` to `Manager.ListBackups` (`internal/recovery/views.go:70`); a page
contains at most 100 entries. Even an out-of-range empty page duplicates the
complete visible inventory first.

After the same health inspection, collect entry indices or shallow private
projections, preserve the current CreatedAt-descending/ID-descending order, page
them, and deep-copy only the selected metadata before unlocking. Keep the exact
total count, deleting-entry filter, nonnil empty result and independent returned
Summary slices. The default object limit is 128, the configurable maximum is
4,096, and the catalog has a 16 MiB bound, so this is a fixed bounded-copy
improvement, not evidence of unbounded memory growth. A top-K framework or new
persistent index is unnecessary.

Verify empty/out-of-range pages, ordering ties, deleting entries and mutation of
returned nested slices; measure allocation and lock time on a large valid catalog.

## Deduplication and retained boundaries

- Application-key display-metadata churn is already documented in the earlier
  performance fixtures; it is an existing follow-up, not a new finding.
- Selecting only notification registrations behind the durable journal tail is
  explicitly retained as a later stage of R12, not a new recommendation here.
- R03's IsHidden projection retains conservative handling of malformed policy;
  a plain JSON Boolean SQL filter is not equivalent.
- Timeshift creation/open I/O under the store mutex is already identified as
  remaining scope in S03. It is not counted again.
- Whole-BIF verification, final authority checks, filesystem identity checks and
  real process/reader retirement have actual correctness roles. No recommendation
  here removes them merely because they are expensive.
- Executable hashing and uncertain close failures lack the additional production
  reachability/platform proof needed for a new simplification recommendation.
- Ordinary subtitle/font responses do not reauthorize after extraction. Existing
  contracts do not clearly promise cancellation of these already-started requests
  on revocation. Unifying their authorization time with long-lived HLS would be a
  product-semantics enhancement; it is not counted as a confirmed defect here.

## Review-loop ledger

| Round | Focus | New findings | Consecutive clean rounds |
| --- | --- | --- | --- |
| 1 | Parallel authorization, storage/recovery, catalog/query, playback/media and Linux boundary review, with historical deduplication. | F01-F10 | 0 |
| 2 | Cross-caller ownership, publication/cancellation counterexamples, SQL type/snapshot boundaries and path-consumer review. | F11-F12 | 0 |
| 3 | Independent report-to-source reconciliation, isolation defaults, bounded collection limits, cache-publication lifetimes and backup read/copy costs. | F13 | 0 |
| 4 | Empty/maximum inputs, cancellation and partial failures, nondefault isolation, source replacement, query frequency, response variants and source/evidence correspondence. | None | 1 |
| 5 | Reverse production-caller and response-consumer tracing, bounded memory costs, ordering and snapshot preservation, native reachability and retained platform boundaries. | None | 2 |
| 6 | Final independent authority/source/resource-lifetime checks, SQL/test-contract correspondence, migration/recovery costs and Linux cleanup/evidence boundaries. | None | 3 - stop |

All five review directions completed rounds 4-6 without an independent new
finding. Clarifications to existing proposals are not counted as new findings.
The rejected and deferred observations above remain explicit; a clean round is
not a claim that no future issue or optimization can exist.

## Verification

F10 passed targeted verification on `test-env` using the current cmd/internal
source snapshot at `/opt/goby-backend-reassessment-20261009-07/source`.

- Go 1.27.1, Linux amd64, `CGO_ENABLED=0`, `GOMAXPROCS=2`, compile concurrency one.
- Remote `gofmt -l` returned no files for the three edited files.
- The library test binary compiled once. All 19 selected top-level tests passed;
  no selected test was skipped. The selection covered scan-reconciliation evidence,
  exact raw spool names, and store alias/foreign-owner/unknown-file rejection.
- Both `GOTMPDIR` and `TMPDIR` used the task-owned ext4 fixture directory during
  test execution. Compiler scratch was separate.
- Shared caches were reused at `/root/.cache/go-build` and `/root/go/pkg/mod`.
  No private cache was copied or created. Shared build-cache growth was about
  101.6 MiB and the task-root peak was about 111.2 MiB, within its recorded budget.
- Fresh closeout inspection found no Go/compiler/linker/library-test workers.
  Only owned compiler scratch, the disposable test binary and empty fixtures were
  removed. Source and raw evidence remain retained. Persistent availability at
  closeout was 2,077,102,080 bytes; shared caches and other tasks were not cleaned.

Local retained raw evidence is under
`.artifacts/backend-reassessment-20261009/remote-evidence/`, including `tests.txt`,
`commands.txt`, `environment.txt` and `closeout.txt`. No local build, test suite,
validation suite or runtime probe was performed. These checks establish targeted
cleanup correctness, not the performance of the unimplemented recommendations.
