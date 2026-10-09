# Backend convergence implementation, 2026-10-10

## Scope

This change implements the accepted findings in
[the convergence review](backend-design-review-convergence-20261009.md).
The work starts from `8ae660bf` on `codex/backend-convergence-fixes`, in an
isolated checkout. Only the previously verified C13 changes are carried from
the original working tree. Its unrelated scan, native-media, playback, release
and research changes are not part of this delivery.

The user authorized implementation, verification on `test-env`, integration
into `main` and push. All executable verification was performed remotely.

## Implementation map

| Finding | Implementation | Retained boundary |
| --- | --- | --- |
| C01 | Ordinary device deletion shares account locks while exclusively locking the affected credentials and device. | Registration/management serialization, lock order, post-wait authority, revision and self-revocation rules. |
| C02 | Session listing uses control flags returned by its existing locked policy read. | Both authority checks, current database time, exact credential kinds and key/client checks. |
| C03 | A settings-owned value snapshot projects one atomic load without cloning unused overrides or slices. | Full management snapshots and publication copies remain independent; one revision supplies every value. |
| C04 | Maintenance collects inspection candidates before copying/sorting jobs. | Fair cursor/budget, subsequent identity checks and the full quota/failure/expiration/reclamation pass. |
| C05 | Descendant counts use nullable-leaf counts without a redundant DISTINCT. | Recursive UNION, unique roots, empty=false, ACLs and collection set semantics. |
| C06 | Direct expected-episode lookups filter the base `e.id` before virtual-row conversion. | General browse population, outer ID/ACL predicates, active roster, season ambiguity and physical-presence rules. |
| C07 | Hardware inspection occurs at planning/status/diagnostic/preview consumers of the captured selection. | Device identity, software-plus-Vulkan selection and concrete-plan checks at actual execution. |
| C08 | A progressive observer reuses a bounded private prefix buffer until readiness or owner exit. | Fresh Stat/ReadAt/parse, short-read slicing, prefix limits, joined finish and real retirement. |
| C09 | Bounded completed-discovery hints reuse an unchanged committed catalog generation. | Every child still enters its fenced owned transaction; any committed catalog change/resync invalidates reuse. |
| C10 | Remote delivery independently revalidates the original controller after receiver/item waits. Play also compares the actual item-query authority. | Receiver checks, key/client binding, denial-as-event-suppression and no database lock across transport writes. |
| C11 | The private diagnostic encoder handoff omits duplicate JSON/newline scans. | Sanitization, length/final-newline bounds, queue-owned clone, backpressure and durable completion. |
| C12 | Schema 67 adds the partial pending-operation priority index and a PostgreSQL-generated recovery catalog. | Old queued-admission index, cancellation-only restrictions, complete candidate validation and claim fences. |
| C13 | Schema 66 and current classifiers/read predicates accept literal Linux colon paths. | Old archive semantics, root/owner/role relationships, anchored opening and every other canonical-path restriction. |
| C14 | Auxiliary sort names are derived in one ordinal-preserving query after each probe retires. | Duplicate names, case behavior, error/coverage checks and authoritative sorting at publication. |
| C15 | Within-group facts are measured incrementally with the original accounting rules. | Exact header/inline/capacity/time/repeated-pointer charges, early limits and final full audits. |

C05 implements the proven redundant-deduplication removal. A separate Boolean
EXISTS query is an optional redesign from the review and is not introduced
without evidence that its additional query shape improves the workload.
C15 keeps cross-root full audits; the implementation does not claim that all
auxiliary accounting across roots is linear.

## Automatic discovery and authority

C09 uses the existing successful `ownedTx.Commit` notification boundary rather
than relying on completed-scan task signals. A scan may commit useful catalog
rows before cancellation or failure, and those commits must invalidate discovery
immediately. Bitmap subtitle changes, media replacements, library options and
resyncs use the same boundary. Invalidation occurs even when no external catalog
listener is installed.

The Store retains at most 512 FIFO keys, each identifying a library/task and the
catalog generation of successful discovery. It is a process-local enumeration
hint, not authority or a durable grant. Reopening the Store or evicting a key
causes discovery again. Generation changes are conservative across libraries;
an unrelated catalog change may cause extra discovery rather than hide work.
Queue-only completion and continuation do not change catalog generation, so an
unchanged backlog need not be enumerated after each eight-item turn.

Hints are recorded only after the complete fenced transaction commits, and a
late record is rejected if a newer catalog commit intervened. The lookup and
notification paths use owner-before-hint lock order; recording a completed hint
does not acquire catalog ownership. All Claim/source/publication checks and
existing task fairness, continuation, manual/automatic and Force behavior remain.
No new schema counter, checkpoint table, timer or worker is introduced.

C10's final authority lookup is a new pool observation outside the earlier
repeatable-read item transactions. An opaque library value captures the policy
and role that actually authorized controller item selection. It is compared with
the final principal so an A-to-B-to-A change cannot be hidden by comparing only
the first and last controller reads. Keys retain their original parent/client
binding and ordinary principals retain their trusted peer context. Revocation
suppresses the event; database/cancellation errors retain their previous path.
This does not make database revocation atomic with network delivery.

Review also found an existing preview-specific hardware gap: initial device
resolution preceded mutex, source-read and process-capacity waits. The Vulkan
encoder and GPU capability probe now carry a narrow callback for the captured
device/inventory generation. Conventional and native process admission invoke
it after capacity acquisition. Software and subsequent CPU validation use the
original context; callbacks are not retained in the options cache.

The new no-start path is explicit about resources. Native admission owns a
prelaunch cleanup defer until it transfers the permit to a process owner, so
callback errors, panic and Goexit cannot lose capacity. Analysis-owned stdout
and stderr pipe writers are explicitly closed even when Start never runs;
borrowed input/tool descriptors remain with their existing owner. CPU-only
tests exercise refusal after waits, abnormal exits and repeated refusal with GC
disabled. These tests do not establish actual AMD execution.

## Implementation review

Five independent review tracks cover authority, request settings/hardware,
resource lifetimes, SQL/schema behavior and scan preparation. Reviews are
sequential rounds over the combined candidate; new defects reset the consecutive
empty-round count. Rounds 4, 5 and 6 completed without a new implementation
defect, satisfying the three-round stopping condition.

| Round | Outcome | Consecutive empty rounds |
| --- | --- | --- |
| 1 | Cross-reviewed actual callers, locked/fresh authority, candidate filtering, buffer/log ownership, catalog invalidation, query equivalence, migration history and exact facts accounting. No new defect. | 1 |
| 2 | Found the preview's missing execution-time hardware check after waits; added the captured-device callback and admission tests. The C08 cancellation suggestion was rejected against the selected contract: a still-active observer may retain bounded scratch until readiness/error/exit/join. | 0 |
| 3 | Found two no-start cleanup gaps exposed by the new callback: a native pre-owner permit on abnormal exit and analysis-owned pipe writers when Start is not called. Added explicit cleanup and regressions. | 0 |
| 4 | All five tracks reviewed the combined repaired candidate, including cancellation, ownership transfer, catalog commit invalidation, authority snapshots, query equivalence and exact group budgets. No new defect. | 1 |
| 5 | All five tracks challenged the production callers and failure paths again. No new defect. Recorded the single-file C15 benchmark tradeoff and the limits of component measurements. | 2 |
| 6 | All five tracks completed another full pass over production callers, failure/retirement paths, actual authority observations, partial catalog commits, Linux path roles and complete-group publication. No new defect. | 3 |

## Verification

The selected verification completed on Linux `test-env` with Go 1.27.1,
PostgreSQL 17.11 and `GOMAXPROCS=2`. Ordinary runs used `CGO_ENABLED=0`; race
runs used `CGO_ENABLED=1`. Native media fixtures remained on ext4. No local
test, build or formatting verification was performed.

| Package | Ordinary passed | Race passed | SQL profile passed |
| --- | ---: | ---: | ---: |
| backuppg | 18 | 0 | 0 |
| database | 20 | 0 | 1 |
| diagnostics | 57 | 57 | 0 |
| identity | 25 | 4 | 0 |
| library | 75 | 5 | 1 |
| media | 66 | 26 | 0 |
| server | 51 | 15 | 0 |
| settings | 15 | 5 | 0 |
| tasks | 9 | 0 | 0 |
| transcode | 41 | 41 | 0 |
| Total | 377 | 153 | 2 |

Counts are deduplicated by package and top-level test name within each mode.
Race cases overlap the ordinary cases; they are not 153 additional unique
behaviors. There are no unresolved failures or skipped selected cases. The
final Linux `cmd/goby` production build passed, and remote gofmt reported no
changes for the 72 changed Go files. This is selected regression coverage,
not a claim that every repository test or actual AMD GPU execution ran.

Two existing settings schema-19 migration tests initially failed because the
standalone test binary ran from the repository root rather than its package
directory. The harness was repaired and those two cases passed on rerun. An
exit-wrapper quoting error after a successful library run was also repaired.
Raw failed attempts are retained alongside the successful results.

Source receipts distinguish r1 (initial implementation), r2 (hardware admission
callback) and r3 (native permit and owned-pipe cleanup). Unchanged scopes retain
their earlier passing results; affected media scopes were retested on r3.
The final production build used r3. The final source manifest has 2,818 entries;
comparison with the selected isolated-worktree archive found no archive
differences or candidate drift.

Raw results, source manifests, capacity records and archives are retained under
`.artifacts/backend-convergence-implementation-20261010/`, with receipts in
`remote-evidence/`. The principal records are `verification-totals.json`,
`final-case-summary.json`, `benchmark-summary.json`, `query-profile-records.json`,
`final-source-retention.json` and `workspace-source-correspondence.json`.

| Retained artifact | SHA-256 |
| --- | --- |
| Final r3 manifest | `18469b7b415c8669be1704762d18a60b3d2cb19b63369a86b48eacdf03444982` |
| `source-final-tested.tar.gz` | `902c056d8f4daae5f69accfeeae912319768c9cba61043793523fcf8f14354de` |
| Linux production binary, 72,986,676 bytes | `ec289a838b160bad3018424c1cba3ee21c1a69260c5e8a85564ca92279b87d5f` |

## Bounded performance observations

These are same-host component observations, not HTTP latency or end-to-end scan
throughput claims. SQL observations used warm shared buffers (`shared_reads=0`).

| Finding and fixture | Previous median | Candidate median | Supporting observation |
| --- | ---: | ---: | --- |
| C12, 12,000 terminal operations and 40 mixed pending, ordinary claim | 1.945 ms | 0.013 ms | Shared hits 574 to 4; partial index 16,384 bytes. |
| C12, same fixture, cancellation-only claim | 0.984 ms | 0.013 ms | Both custom and generic plans use the new index for both flags. |
| C06, known expected-episode ID | 37.573 ms | 0.310 ms | Shared hits 16,592 to 52; base primary-key lookup. |
| C06, known-ID batch | 37.823 ms | 0.320 ms | Shared hits 16,592 to 54. |
| C05, two roots and 2,000 leaves | 50.734 ms | 41.929 ms | Shared hits unchanged at 62,217. |
| C04, 128 retained jobs | 10,013 ns | 2,932 ns | 1,152 B / 1 allocation to 0 B / 0 allocations. |
| C04, 4,096 retained jobs | 564,137 ns | 118,860 ns | 32,768 B / 1 allocation to 0 B / 0 allocations. |
| C08, incomplete 1 MiB prefix | 82,169 ns | 20,964 ns | 1,048,784 B / 2 allocations to 208 B / 1 allocation. |
| C15, one file | 2,511 ns | 3,778 ns | 1,264 B / 8 allocations to 1,872 B / 11 allocations. |
| C15, 64 files | 2,630,976 ns | 235,523 ns | 1,324,760 B to 124,976 B. |
| C15, 256 files | 42,103,039 ns | 979,559 ns | 19,967,192 B to 450,608 B. |

C04 uses warm retained jobs without filesystem work. C08 repeatedly observes a
fixed incomplete prefix while retaining fresh ReadAt and parse operations.
C15 compares the previous prefix algorithm plus publication audit with the
incremental algorithm plus post-sort and publication audits in the same process
and fixture. Its additional post-sort audit costs about 1.27 microseconds and
608 bytes for one file; its measured benefit is concentrated in larger groups.
Cross-root full audits remain in production.

C03's selected allocation test observes zero allocations for ValueSnapshot
versus four for the full snapshot. C14 tests observe zero sort-name queries for
an empty group and one per nonempty owner at three and 256 files. C09's 17-item,
three-turn fixture performs one discovery per family, and actual partial
cancelled-scan catalog commits invalidate that reuse. C02's ordinary session
listing drops from ten to nine SQL operations including BEGIN/COMMIT; the key
path remains eleven.

## Capacity and retention

The capacity policy was read before preparation. Initial persistent availability
was 657,776,640 bytes, insufficient headroom for the selected multi-package
verification with the existing 2,046,578,688-byte shared build cache. One explicit
maintenance operation confirmed idle workers, canonical path, ownership and the
pinned Go executable-cache layout, then ran `go clean -cache` with the exact
shared `GOCACHE`. Module dependencies, source, databases and evidence were not
removed. The cache dropped to 12,288 allocated bytes and persistent availability
rose to 2,704,334,848 bytes.

Ordinary verification reuses that shared cache and the existing module cache.
Source, compiler scratch, binaries and live evidence use task-owned tmpfs paths;
native execution binds both temporary-directory variables to ext4 fixtures.
The admitted plan permits 1.75 GiB of shared-cache growth, retains a 512 MiB
persistent-space floor, and bounds task tmpfs use at 1.5 GiB with 512 MiB free
tmpfs and 2 GiB available memory.

After the library phase, temporary PostgreSQL catalog occupancy briefly exceeded
the original 100 MiB database/fixture budget. Admission paused. A fresh read
showed automatic PostgreSQL reclamation to 81,845,472 bytes with no task database
connections and no retained application schemas in the general test database.
The phase budget was explicitly increased to 220 MiB before continuing. No
database was recreated and no second cache cleanup was performed for that event.

For the final production build, an empty private tmpfs build cache was explicitly
selected instead of growing the persistent shared cache further. The revised
plan budgeted 1 GiB for that private cache, 512 MiB for compiler scratch and the
binary, and 2 GiB total task tmpfs, while retaining the same persistent, free
tmpfs and memory floors. Test and baseline binaries were archived and copied
locally before reclaiming expanded copies.

After actual worker exit, a fresh liveness check and exact-path validation
preceded pinned `go clean -cache` for the private cache. It shrank from
288,854,016 to 8,192 allocated bytes; compiler scratch was zero. The shared build
cache (1,493,168,128 bytes) and module cache (920,563,712 bytes) were unchanged by
closeout. Final observed availability was 689,610,752 bytes on the persistent
root, 2,614,697,984 bytes on tmpfs and 6,144,811,008 bytes of memory. Availability
may reflect concurrent activity independently of measured cache allocation.

All source revisions, raw logs, four owned databases, the archived test binaries
and final production binary are retained. The remote task root is
`/tmp/goby-backend-convergence-implementation-20261010`; native fixtures remain
under `/opt/goby-backend-convergence-implementation-20261010`. Exact retention
and cleanup paths are recorded in `final-source-retention.json` and
`private-cache-closeout.json`.

## Git delivery

Delivery is limited to the reviewed isolated candidate. Existing unrelated
working-tree changes are preserved separately from the implementation commit.
The selected source is compared remotely with the committed archive before
integration. Main advances by fast-forward and is pushed without force.

The final commit identity, upstream equality and working-tree preservation
receipt are recorded during integration in the local
`.artifacts/backend-convergence-implementation-20261010/git-delivery.json`.
That receipt stays outside repository history to avoid a self-referential
commit hash; the remote committed-source correspondence is retained with the
other `remote-evidence/` records.
