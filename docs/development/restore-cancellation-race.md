# Late restore-plan cancellation race

Status: independently confirmed by source review on 2026-09-13; deterministic
remote regression and implementation are pending. Candidate promotion is paused.
No runtime failure of this race is claimed yet.

`planJob` publishes `ready` before its deferred archive, database and lease
cleanup completes. `startLocked` keeps the worker in `m.jobs` until the entire
work callback returns. If `Cancel` arrives in that interval, it persists
`CancelAuthorized` and cancels the worker context. The successful return path
does not call `failJob`, and the worker finalizer only removes the job and closes
its done channel. The operation can consequently remain durably `ready` with
both `CanApply` and `CanCancel` false. It continues to make the manager busy;
repeating Cancel returns the same operation. Startup reconciliation can retire
it, but the running manager has no equivalent completion path.

The symmetric ordering also needs coverage: accepted cancellation before the
worker's last successful `ready` publication must not be overwritten by that
publication. Existing integration tests use `waitOperation`, which waits for
the worker's done channel after observing the requested state. That deliberately
avoids the problematic cleanup interval.

Relevant source: [plan publication](../../internal/recovery/plans.go),
[worker finalization](../../internal/recovery/manager.go),
[cancel handling](../../internal/recovery/backup_jobs.go), and
[existing integration fixture](../../internal/recovery/manager_integration_test.go).

Use the existing isolated manager fixture and channels to reproduce both
orderings through actual Cancel, persistence and worker completion. First run
the regression against unchanged product code on `test-env`; then implement a
narrow terminal-state fix and verify cancellation, busy release and the retained
inactive stage. Do not force a restart to make the regression pass.

After the final product snapshot passes affected regressions and required full
verification, preserve the seeded candidate through a separately reviewed
schema28 binary transition. Rebind its process/listener/lease and actual source
before restarting bounded live admission. Do not rerun bootstrap/scans or reuse
the consumed admission inputs. Main upgrade and core-client acceptance remain
open; this finding does not alter their completion requirements.
