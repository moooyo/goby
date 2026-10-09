# Backend follow-up implementation, 2026-10-09

## Scope

This implements Q01-Q09 from the
[follow-up review](backend-design-review-followup-20261009.md), including its
completed Q03 platform cleanup. The branch `codex/backend-followup-fixes`
starts from `d2952a3f` in an isolated worktree. Existing unrelated edits in
`D:\Code\goby` remain outside this delivery. The user authorized implementation,
verification on `test-env`, and merge/push to `main`.

## Implementation

| Finding | Result |
| --- | --- |
| Q01 | Recognize exact static help aliases before loading service configuration, preserving safe errors if writing help fails. Actual operations retain full configuration and deployment validation. |
| Q02 | Retain successful request-local playback routing and use the existing narrow authority revalidation on reuse. The repeated five-statement routing transaction becomes one current-authority SELECT, preserving peer/client facts and denial semantics. |
| Q03 | Remove the obsolete non-Linux executable-capability contract test, retaining Linux trust and unsafe-input coverage. |
| Q04 | Upsert validated episode-roster entries through one typed relation using protected `ownedTx.Exec`, with explicit nullable dates and the original atomic history/retirement/revision behavior. |
| Q05 | Add a compact post-expiration publication result, sharing the original atomic write path. Stable subtitle plans still select the full publication snapshot. |
| Q06 | Share a bounded staggered literal-address dialer between providers and notifications. All addresses are validated before dialing, deadlines bound connection work, and losing attempts are joined and closed. |
| Q07 | Reuse the fully validated BIF JPEG directly when no size/quality change is requested; retain rendering for transformations. |
| Q08 | Return the first validated, owned waveform arrays after final source/access checks and descriptor closure; remove the HTTP layer's second read and parse. |
| Q09 | Publish snapshot close failure and reader retirement under one store lock, then fold all observed degradation into the terminal Store.Close result. |

Authority, integrity and actual-resource retirement checks remain in place.
The implementation does not add a cross-request permission cache, replay HTTP
operations, split roster commits, or use a later snapshot to approximate an
atomic publication result.

Provider DNS/TCP work has one ten-second deadline; notification DNS/TCP work
has one three-second deadline. The preferred address keeps that full deadline.
Fallbacks have a two-second minimum where the remaining total budget permits
it; insufficient time does not promise traversal of every DNS address. TLS
budgets, hostname checks, provider rate limits and post-TLS notification
authorization remain unchanged.

## Implementation review

| Round | Outcome | Consecutive clean rounds |
| --- | --- | --- |
| 1 | Corrected help-output error reporting. Network review required progress beyond two stalled addresses and an additional short-remaining-budget scheduling correction. Other authorization, SQL, publication, waveform and diagnostic changes passed static review. | 0 |
| 2 | Identified overly small fractional dial timeouts for healthy addresses in a long DNS answer; selected a conservative preferred-address budget and minimum fallback budget. Remote verification also found a pointer-valued test fixture error and a dial deadline boundary failure. | 0 |
| 3 | Rechecked final dial scheduling/deadline semantics, snapshot failure aggregation, request-local identity, protected roster transactions, publication/data handoffs, CLI failures and platform boundaries. No new issue. | 1 |
| 4 | Challenged complete success, empty-result, failure, cancellation and late-completion paths, plus source identity and staging scope. No new issue. | 2 |
| 5 | Reconciled final production call chains, error semantics, ownership, budget tradeoffs and test outcomes. No new issue. | 3 - stop |

Five implementation review rounds completed. Rounds 3-5 consecutively found
no new issue after the corrections above. Remote execution and its limits are
recorded separately below.

## Verification results

Verification is complete. Production builds of all packages under
`internal/...` and `cmd/...` passed, as did compilation of the ten selected
package test binaries. There are 302 distinct passing top-level tests across
the selected ordinary executions, including all 52 newly added tests. Selected
race executions passed 171 tests, a subset of the ordinary test set. Counts
exclude subtests and repeated executions.

| Selected package | Ordinary passed | Race passed |
| --- | ---: | ---: |
| cmd/goby | 14 | Not selected |
| commanddomain | 2 | Not selected |
| literaldial | 11 | 11 |
| providers | 22 | 22 |
| notifications | 12 | 12 |
| diagnostics | 56 | 56 |
| timeshift | 30 | 30 |
| identity | 5 | Not selected |
| library | 34 | 23 |
| server | 116 | 17 |

These are affected-package regressions, not execution of the entire backend
suite. Two pre-existing, opt-in authentication performance fixtures were
selected by the broad HTTP name pattern but skipped because
`GOBY_AUTH_PERFORMANCE=1` was not enabled. No new test was skipped or failed.
The exact executable also passed fifteen CLI process checks: all help aliases
with absent/bad configuration, invalid actual operations, and a failed help
write through `/dev/full` with a safe stderr diagnostic.

The first snapshot retained three failed phase exit records: ordinary and
race server compilation found a pointer-valued `SegmentLength` test fixture
initialized with an integer, and the ordinary dial suite exposed a reached
deadline whose cancellation state was not yet published. The server execution
was blocked by that compile error. The fixture uses the correct pointer now;
dialing checks the absolute deadline as well as `ctx.Err()`. Source round 2
rebuilt production and the affected network/server binaries; all supplemental
phases passed. Unchanged package results remain bound to their original
executions rather than a claimed full rerun.

All 32 changed/new Go files were inspected with pinned remote `gofmt`; four
test files needed formatting, and those exact results were returned to the
worktree. The second overlay required no formatting change. Source manifests
and per-phase exits remain in the evidence directory. The final summary binds
each new test to its passing raw-log line.

## Controlled work and allocation observations

- The full-size roster test writes 2,000 entries with one entry UPSERT
  statement (22 SQL statements for the entire observed operation). It also
  verifies nullable/extreme dates, stable IDs, rollback and cancellation.
- Matching request-local playback routing uses one fresh-authority SELECT on
  reuse, replacing the five-statement/three-SELECT routing transaction. HTTP
  tests separately preserve explicit clients, missing/foreign plays, trusted
  peers and revocation across body/activity waits.
- In the result-copy microbenchmark, 512 segments with one epoch require
  125,217 B and 525 allocations for a full snapshot versus 64 B and one
  allocation for the compact result. This measures locked result construction,
  excluding file copy, expiration and the rest of publication.
- In the 64-track waveform helper comparison, the owned-data path uses about
  6.24 MB and 985,066 allocations per operation versus 14.93 MB and 1,970,017
  for rereading/reparsing. The observed single-run times were 15.68 ms and
  32.00 ms respectively. This is a local helper/fixture comparison, excluding
  database authority, worker admission and HTTP; it is not a natural-load
  latency or throughput claim.

Raw benchmarks, query-count assertions and regression results remain separate.
No aggregate end-to-end speedup is inferred from these measurements.

## Verification environment

All executable checks run on `test-env` using pinned Go 1.27.1 and the existing
shared build/module caches. One compiler/test phase runs at a time. Compilation
uses task-owned tmpfs scratch; both native `GOTMPDIR` and `TMPDIR` bind to the
task's persistent fixture directory. Database fixtures own random schemas and
remove only their own schemas.

The remote owner root is `/opt/goby-followup-implementation-20261009-05`.
The local plan, scripts, source receipts and raw evidence belong under
`.artifacts/backend-followup-implementation-20261009/` in the implementation
worktree. The capacity plan preserves at least 1 GiB persistent free space and
uses no private compiler cache or cache copy. A fresh process inspection found
no owned workers before cleanup. Seventeen explicitly listed test executables
were hashed and reclaimed, freeing 449,421,312 allocated bytes; the selected
production executable, source, raw evidence and persistent fixture directory
remain. The empty task compiler-scratch directory was removed. Shared caches
were preserved.

At closeout, persistent availability was 1,749,712,896 bytes; retained task
resources occupied 134,737,920 allocated bytes, shared build cache
1,856,073,728 bytes and module cache 920,563,712 bytes. Concurrent host activity
can affect filesystem deltas, so only the measured test-binary occupancy is
attributed to this cleanup.

`evidence/results-summary.json` records final selected outcomes, historical
phase exits and all new-test coverage. `evidence/source-files-round01.json`
and `source-files-round02.json` bind the two snapshots. `closeout.json` and
`test-binary-retirement.json` preserve resource checks and removed-binary hashes.

## Integration

The selected target is `main`, reached from the isolated delivery branch after
verification. The commit/source comparison and Git delivery receipt are kept
with the local evidence. Unrelated original-worktree changes are excluded.
