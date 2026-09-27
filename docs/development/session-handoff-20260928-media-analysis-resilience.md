# Media analysis Phase 3 handoff - September 28, 2026

## Closeout and source revision

The user stopped new Phase 3 dispatches, requested that the completed code be
merged into `main`, and asked for this handoff. Local `main` was fast-forwarded
from `c34fbdb` to `c5d08bec44a8c3bf58045b51fba92ce82a43a22b`. The merge
includes these three commits:

| Commit | Change |
| --- | --- |
| `4d436f9` | Skip subtitle projections when a scan cannot change them, with a focused regression. |
| `7e8fed1` | Avoid empty subtitle/image transactions and duplicate independent-scan progress writes. |
| `c5d08be` | Check the existing capacity scan deadline while concurrent work is still pending, preserving the primary failure. |

The separate `codex/media-analysis-capacity-repair` worktree still has four
uncommitted Movie pagination/count SQL experiment files under
`internal/library/query*`. They change query behavior and were not selected or
verified for this checkpoint. Preserve them in that worktree; they were not
part of the fast-forward merge. Pre-existing unrelated dirty files in the
primary `D:/Code/goby` working tree were also preserved and excluded from this
handoff commit.

The user changed the acceptance priority to functional completeness first.
For the proposed new 10,000-item HTTP functional tier, the historical
900-second scan and p95/p99 latency targets are observations, not gates for
`functional_accepted`. Exact scan/query/playback results, real overlap,
resource/error boundaries, and independent cleanup remain required.
`capacity_accepted` remains false until a separate capacity replay meets its
own contract. The strict historical capacity workload in commit `c5d08be` is
unchanged; its result must not be relabeled as a functional-tier pass.

## Verification that exists

- The `4d436f9` subtitle implementation and its new test matched the focused
  VM106 candidate bytes. Baseline and candidate each passed 18 library parent
  tests / 24 test cases, with zero skips or failures and independent closures.
  The test tree was the preceding revision plus exact overlays, not a full
  `4d436f9` archive suite.
- The `7e8fed1` changed Go files were pinned in a VM106 targeted runner. Its
  34 selected library parent tests and package passed with no skip or failure;
  the worker closed independently. A separate 500-path real-media A/B
  diagnostic passed in both variants. That comparison is development evidence,
  not 10,000-item capacity acceptance.
- The `c5d08be` workload control change passed five actual VM106
  `Actor.run_phase` control-flow tests. A first harness error remains recorded
  separately; it was repaired in a fresh generation, not retried in place.
- The selected `c5d08be` revision has fresh, independently closed media03,
  full_media02, and preview-replay03 functional proofs. The media proof reader
  verified all three against current source. The full_media02 suite executed
  the unfiltered media package. These results do not cover the current library
  and server suites.
- The suite02 inventory03 executed `go test -list` for library, server, and
  media, listing 981, 1,018, and 558 parent names. Its receipt explicitly says
  `tests_executed=false`; it is compilation/inventory evidence only.

There is no completed current-target nine-stage library/server/media suite,
15-stage Product delivery, Reader qualification, or 10,000-item HTTP
functional result for `c5d08be`. Phase 3 acceptance remains incomplete.
Static review found no definite blocker in the merged diff, but the focused
tests do not directly exercise the new no-image-row and no-active-subtitle
shortcuts in every error path. The strict capacity fault wrapper also reuses
`run_phase`; its early scan-deadline check may end a still-running actor before
that wrapper's longer phase deadline. This path needs actual verification
before fault acceptance.

## VM106 stopping point

The original Product PostgreSQL PG03 provision was dispatched once and failed
before `initdb`. Its published controller passed
`goby-phase3-product-7e8fed-pg01.service` to the shared PostgreSQL helper,
which requires `goby-phase3-<scope>-postgres.service`. The provision and
storage-guard units are terminal failed with no live PID or cgroup. The static
PostgreSQL unit is inactive; no PostgreSQL data/WAL/log directory or port
55975 listener exists. Preserve the original failed units, scope, and receipts.
The generic failure receipt's `postgres_may_be_running=true` is conservative;
the separate terminal and filesystem observations establish that this
generation never started PostgreSQL. It is not a clean-shutdown or successful
provision result.

A separate early-failure negative-closure candidate and independent readback
passed source review, but their source transport did not pass final review:
it still lacks the complete C08 authority-API and SSH tool Pin checks. No
source stage, actual negative-closure receipt, or PG02 successor has been
published or dispatched. The existing PG03 `close-failed` mode must not be
used for this prebirth failure because it consumes a one-shot intent before
checking prebirth/data files that do not exist.

The C08 user-approved window is finite: 2026-09-27 08:15:00 through
23:43:22 UTC, with a no-new-dispatch final hour. It has no automatic renewal.
The user closeout instruction stopped new dispatches even within that window.
On a later resume, obtain the appropriate authority, recheck boot and live
state, and use fresh names and receipts. Do not reuse the failed PG03 unit or
its consumed output namespace.

The detailed private execution record is retained on the original host at
`D:/Code/goby/.git/media-analysis-resilience-20260920/phase3/resume-20260926-01/C08-EXECUTION.md`.
It contains exact evidence Pins and source-only drafts and is not part of Git
history. No private database URL or credential belongs in this document.

## Next work when Phase 3 resumes

1. Repair and independently review the PG03 early-failure source transport,
   then write and read back one negative-closure receipt without stopping,
   resetting, deleting, or retrying the original units.
2. Prepare fresh PG02 capacity and reader-resource contracts and a current
   native storage-reader qualification. The old PG01 qualification is
   historical ancestry because it pins PG01-specific paths and contracts.
   The proposed PG02 scope is `product-7e8fed-pg02`, UID/GID 62976, port
   55976, and unit
   `goby-phase3-product-7e8fed-pg02-postgres.service`; reobserve vacancy and
   bind actual evidence before use.
3. Finish the fresh suite runtime source generation and PG02 binding, then
   provision PostgreSQL, run the nine-stage current-target suite, and close
   both independently. Preserve old source-only suite releases as ancestry.
   The current storage accounting draft requires 33 distinct roots and a
   1,000,000-inode limit; remeasure before publication.
4. Complete current `c5d08be` Product delivery and Reader qualification with
   independent closures. Their PG02/suite drafts are not frozen or runnable.
5. Finish and review the separate functional-tier Actor, root-owned observer
   seals, native admission/collector/evaluator, and retirement chain. Its
   current private sources have missing dynamic Pins and unreviewed bindings;
   none are functional acceptance. Measure performance after functionality is
   proven and run a separate capacity replay only when desired.

Private source-only drafts in the `resume-20260926-01` directory contain stale
freeze Pins and explicit release guards. Do not promote them by merely flipping
`RELEASED` or `ready` flags. Reconcile producer/reader schemas and perform
independent source review before any new VM write.
