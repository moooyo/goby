# Bounded movie execution decision

Decision recorded on 2026-09-13 after the complete offline lifecycle review.
Execute exactly one original-client movie scenario, `movie-05`, with the existing
candidate, original client host, movie actor and retained data. This decision
follows the user's instruction to continue the revised plan. It does not reuse
the consumed movie04 attempt or authorize a retry sequence.

The changed prerequisite is the verified full movie lifecycle: controls are
awaited before use, Playing/Stopped/Logout observations are installed before UI
actions, response completion is bounded, and cleanup remains independently
attempted. The captured movie04 controls reproduce the asynchronous empty-to-Play
transition. The [offline alignment](audited-movie-offline-alignment.json) records
16 lifecycle, 20 closeout and 16 controller checks, with 39 unchanged adapter
checks reused. No product or dependency change is included.

## Frozen execution

- Input: `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/offline-movie-controller-verification-03/movie-input-05.json`,
  SHA-256 `5df04aeefdf461162ce2572c6f93487c79dd34f5b6cfe7a517da4379b0338d01`.
- Controller SHA-256: `291a58ded1d519adc923cd2afb4206c173b3a8695592d753150ac814c6d1576e`.
- Fresh entry review: `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-movie05-entry-review-01/review.json`,
  SHA-256 `54a122d595648f46025c80ba1e8a2587342dd6a99e98c76c4d5fe7829d794e28`.
- Output: `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-core-client-movie-05`.

The entry review captured state at `2026-09-13T12:09:24.583298Z`. All 35 tables
and sequences match the retained movie04 snapshot, with JSON types preserved.
The candidate, PostgreSQL, deployment lease and hosting identity remain exact;
the output and both new worker units are absent. The pruning deadline guard
passes. The review issued no business HTTP, browser run or service action.
The controller repeats these live entry checks immediately before execution.

## Expected result and limits

The unchanged original client must select the pinned movie, start and stop
playback, preserve its expected history, log out, log in again and prove a new
playback chain. Actual completed API exchanges, media responses, UI observations
and durable state must agree. The old unstarted preparation may become Expired
only under the [retained baseline contract](audited-movie-retained-baseline.md);
it cannot count as a new play or be deleted to simplify comparison.

Keep the existing limits: 1,200 seconds overall with 240 seconds reserved for
cleanup, 600 seconds for the browser with 120 seconds reserved for client cleanup,
and 1,000 gateway requests with 64 reserved for cleanup. No additional smoke
request, wider network access, new account, seed reset or service restart is
part of this decision. Only the two owned movie05 worker units may be started
and stopped. Retain their terminal status and prove absent PIDs and empty cgroups.

If the scenario fails, close owned state and reconcile the saved evidence before
another execution decision. A checker failure is investigated offline against
the existing artifacts; it does not rerun business operations. A product failure
requires a focused reproduction and fix. A preparation or observer failure
stops browser iterations for another complete value-and-cause review. Unexplained
identity or foreign-state drift blocks the affected operation. Playback
acceptance remains open until the strict closeout passes.
