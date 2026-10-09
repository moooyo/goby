# Backend review loop implementation, 2026-10-09

## Scope

This implements the L01-L08 findings in
[the review record](backend-design-review-loop-20261009.md). Work starts from
`27071431` on `codex/backend-review-loop-fixes` in an isolated checkout. The
unrelated scan, native-process and HLS edits in `D:\Code\goby` are outside this
change. The previously reviewed Linux cleanup is applied independently to
the clean base so those unrelated edits do not enter the delivery.

## Implementation map

| Finding | Implementation |
| --- | --- |
| L01 | Resolve the globally unique token hash once and project either an ordinary Emby login or an application key in one SQL statement. Exact-kind login APIs and current principal policy checks remain. |
| L02 | Aggregate matching InstantMix credits by item before joining candidates. Preserve separate seed/candidate scopes, weights, bonuses, zero-score fallback and sorting. |
| L03 | Filter item names and entity names independently. Prove an entity's visibility with one valid association to a current authorized source. |
| L04 | Move legacy synchronous finalization, raw-image worker, snapshot lookup and the unintegrated ownership prototype into package test files. Keep all actual production resource lifetimes and the production ownership error sentinel. |
| L05 | Retain 512 successful image inspection records with fixed FIFO single-entry eviction. Hits, duplicate insertion and cancellation do not advance the insertion order. |
| L06 | Remove obsolete Windows branches from five library tests and three dead non-Linux media conditions. Preserve Linux assertions, client compatibility and unsafe-input rejection. |
| L07 | Use shared account/session locks for personal notification authority checks. Actual registration/journal mutations and final clock-based authority checks retain their locks and order. |
| L08 | Use one bounded diagnostic writer to group queued records without a batching timer. A caller succeeds only after its record's file has been synced. |

## Preserved behavior

Authentication retains disabled, revoked, expired, local-login, device and
trusted-peer restrictions. Application keys retain their userless, non-expiring
credential and matching default-client requirements. Native administrator
tokens remain outside the Emby resolver. No cross-request authority cache is
introduced.

Catalog optimizations preserve snapshot, typed-identity, association and ACL
semantics. Entity names need not match source titles. Candidate filters must
not shrink InstantMix seed evidence. Cache reuse still reads and hashes the
complete source and retains existing decoder limits and cancellation behavior.

Diagnostic batching preserves complete records, per-file durability across
rotation, deletion-intent ordering, sticky failure, safe fallback and actual
worker/snapshot shutdown before releasing the process lock. Queue and batch
memory are bounded. Cancellation can reject an unstarted write but cannot
retract an already started write or acknowledge it before sync.

## Implementation review

Early design review preserved error precedence for unstarted cancelled writes
without masking actual health/write failures. It also retained the original
time-scan behavior for non-finite ordinary-login timestamps. These corrections
were made before selecting the first verification source.

The first remote formatting pass found a missing loop brace in the new writer
tests. Independent review found the same syntax issue and strengthened the
pending-result assertions with `synctest.Wait`, so an unscheduled result
forwarder cannot hide an early durability acknowledgment. Both test fixes are
included in the subsequent source; the failed formatting evidence is retained.

| Round | Outcome | Consecutive clean rounds |
| --- | --- | --- |
| 1 | Fixed the writer-test loop brace and made pending-result assertions wait for runnable test goroutines. No additional production defect was found. | 0 |
| 2 | Rechecked nullable credential projections, notification lock/expiry semantics, query equivalence, fixture dependencies and writer cancellation/close cases; no new issue. | 1 |
| 3 | Traced HTTP/service/transaction/DTO and handler/fallback/snapshot paths, plus resource retirement and failure boundaries; no new issue. | 2 |
| 4 | Challenged final diffs with scope, ordering, zero-result, rotation, partial-failure and shutdown counterexamples; no unresolved issue. | 3 - stop |

Four implementation review rounds completed. Rounds 2-4 were consecutively
clean after the two test corrections. These are static review results; the
remote execution outcomes are recorded separately below.

The first identity-suite execution exposed one additional fixture error:
the new alternate-client setup omitted Device and Version while using an
existing helper that asserts exact metadata equality. Normal binding fills
those omitted fields from the default client. The fixture now supplies its
own complete metadata so all four malformed-key cases reach their intended
sidecar/default-client/revocation assertions. Production code is unchanged.
The original failing run is retained separately from the repaired resolver
group's rerun. The other 224 test outcomes remain tied to their unchanged
code and original execution rather than a claimed full-suite rerun.

## Remote verification

Verification is complete. All formatting, builds, test execution, race checks
and query-plan measurements ran through `ssh test-env`; no local verification
was performed.

All 41 packages under `internal/...` and `cmd/...` passed production build and
test compilation. Across the selected executions, 564 distinct top-level tests
have a final passing outcome, deduplicated by package and test name. All 23
new top-level tests ran and passed: artwork 3, diagnostics 8, identity 9 and
library 3. No selected test remains skipped or failed. This does not claim
execution of every test in every backend package.

| Execution | Top-level result |
| --- | --- |
| Artwork complete suite | 64 passed |
| Identity complete suite, first execution | 224 passed; 1 fixture failure retained |
| Rebuilt identity ResolveEmby group after fixture repair | 5 passed; complete identity suite not repeated |
| Notifications complete suite | 8 passed |
| Diagnostics complete suite | 53 passed |
| Library focused regressions | 66 passed |
| Transcode focused regressions | 51 passed |
| Server focused and supplemental observability routes | 35 plus 25 passed |
| Media focused Linux regressions | 36 passed |
| Artwork focused race / diagnostics complete race / notification-authority race | 5 / 53 / 4 passed |
| Discovery query equivalence and plan profile | 1 passed; 48 plans retained |

Only two files required remote formatting; their exact formatted output was
returned to the worktree. The failed initial formatting attempt and the first
identity-suite failure remain separate from their successful follow-ups.

The selected environment uses pinned Go 1.27.1, FFmpeg/ffprobe 9.0.1, the shared
Go build/module caches, one compiler/test worker at a time, and a task-owned
ext4 native-fixture directory with both `GOTMPDIR` and `TMPDIR` bound to it.
Compiler scratch is separate. The verification plan budgets cache growth,
source/binaries, fixtures/database growth and raw evidence while preserving
at least 1 GiB persistent free space.

Closeout found no owned worker. The exact empty compiler scratch directory was
removed; shared caches, source, binaries, fixture root and raw evidence remain.
Final persistent availability was 2,604,699,648 bytes. Allocated shared build
cache was 1,153,617,920 bytes, modules 920,563,712 bytes and the retained task
root 349,847,552 bytes. No unrelated resource or shared cache was cleared.

Local evidence belongs under
`.artifacts/backend-review-loop-implementation-20261009/`; remote source and
evidence belong under `/opt/goby-review-loop-fixes-20261009-03`. Previous review
and verification records remain separate.

`verification-summary.md` records the complete phase table and environment.
`evidence/results-summary.json` binds every new test to its raw-log lines and
retains exact per-phase exit codes. Source manifests are
`evidence/source-files-round01.sha256` and `evidence/source-files-final.sha256`.

## Controlled query observations

The profile compares frozen pre-change SQL and the new SQL in the same
deterministic authorized catalog: 3,000 tracks, 5,000 movies, 120 albums and
25,120 associations, with 20% of sources in a hidden library. It verifies the
complete InstantMix score relation plus count/page results, and captures three
alternating samples of each shape with JIT disabled. Times below are median
PostgreSQL execution milliseconds for this fixture.

| Query selection | Original | Current |
| --- | ---: | ---: |
| InstantMix count | 32.204 | 32.305 |
| InstantMix default-score page | 891.467 | 63.542 |
| Selective mixed SearchHints count | 7.436 | 1.904 |
| Selective mixed SearchHints page | 7.552 | 1.892 |
| Broad mixed SearchHints count | 13.752 | 13.990 |
| Broad mixed SearchHints page | 18.794 | 19.231 |
| Video-entity SearchHints count | 12.442 | 0.673 |

The broad-search samples are approximately neutral, not evidence of universal
acceleration. All root plans reported zero shared reads and zero temporary
reads/writes, so these are warm-buffer observations. The InstantMix page
measurement covers candidate selection and ordinal projection, not the full
Item DTO or HTTP response. The video-entity profile also executes an empty
OFFSET 2 page over two matches; real SearchHints skips that query when its
count is exhausted, so its synthetic page timing is not presented as a
production benefit. No natural-load throughput, cold-cache or HTTP latency
claim is made.

The deterministic diagnostic test separately confirms that 49 queued records
use three JSONL syncs while every result remains pending until its file's sync
finishes. Rotation and fault tests cover their separate boundaries; this
controlled grouping ratio is not a universal traffic ratio.

## Integration

The delivery branch is `codex/backend-review-loop-fixes`, based on `27071431`,
with `main` as the authorized merge and push target. The original checkout's
unrelated changes remain outside the commit. Git history and the task's
completion record identify the delivered revision; retained source manifests
identify the verification inputs independently of the documentation commit.
