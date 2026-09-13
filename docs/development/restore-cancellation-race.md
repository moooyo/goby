# Late restore-plan cancellation race

Status: independently reviewed, reproduced remotely in both timing windows, and
fixed. [Focused verification](restore-cancellation-targeted-verification.json)
passes, including a real journal CAS conflict. The
[single full run](restore-cancellation-full-verification.json) passed 2,264 tests
across 25 packages with race instrumentation, zero failures/skips and a Linux
amd64 build for the [frozen snapshot](restore-cancellation-source-freeze.json).
Candidate promotion still requires the reviewed transition and live admission.

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

The regression uses the existing isolated manager fixture and channels through
actual Cancel, persistence and worker completion. Unchanged product code failed
both orderings with `ready`, committed cancellation, no remaining worker and a
busy manager. The test-only archive is
`2195e8ef4a8b5aaa871f049ab35d10a43f2958fbba7ca55038305a9290242185`.

The fix settles an explicitly cancelled ready restore after the worker returns,
before its done signal is closed. It uses an independent five-second persistence
context and retains the last acknowledged control state on any persistence
failure, marking the manager unavailable. This also prevents online operation
queries from reporting an uncommitted cancelled terminal state. It does not
change completed backups, uncancelled plans or application transitions.

Two focused top-level tests passed with race instrumentation: the two ordering
subcases, and a real same-payload CAS that advances the journal digest before
worker completion. That conflict leaves the committed payload intact, prevents
a false terminal-state response and rejects subsequent mutation. Both red and
green verification scopes stopped their owned PostgreSQL instances, removed
private mounts and left empty cgroups. No candidate or main restart was used.

The final archive is
`b7120b6f323ace203fe7b49c56cd669ce5b9ff3ef8f3ceddb4359c2951aa658b`.
Its one full run is
`/opt/goby-test/audit-fixes-20260913-20260913T083107Z-fb6c70468fa3`.
The built binary SHA256 is
`477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f`.
Raw test events, package coverage, source archive/manifest/actual file closure,
binary bytes and owned PostgreSQL/mount/cgroup cleanup were reconciled. The
read-only closeout reader was corrected to the actual frozen Go timeout; no
test or build was repeated. Unchanged frontend evidence remains reusable.

Preserve the seeded candidate through the separately reviewed
schema28 binary transition. Rebind its process/listener/lease and actual source
before restarting bounded live admission. Do not rerun bootstrap/scans or reuse
the consumed admission inputs. Main upgrade and core-client acceptance remain
open; this finding does not alter their completion requirements.
