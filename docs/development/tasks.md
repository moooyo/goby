# Task Execution and Scheduling

**M5f task increment accepted.** The
[complete Go race summary](m5f-full-race-summary.json) records 1190 passing
top-level tests across thirteen tested packages with no skipped tests or race
findings. The [isolated browser/restart workflow](m5f-tasks-browser.json) has
also passed, as have [deployment](m5f-deployment-evidence.json) and the
[deployed workflow](m5f-deployed-tasks.json). This document describes source behavior, not a full
client release.
The [API contract](../api/tasks.md) specifies request and response formats.

## Definitions, runs, and schema 19

`internal/tasks` currently registers only `library.scan` / `RefreshLibrary`,
which performs a normal full-library scan. Registry reconciliation allocates
its definition ID once, maintains code-owned labels, preserves schedules and
history, and disables unavailable definitions. It creates neither a run nor
an automatic schedule. Newly initialized task configuration is manual-only.

Each admitted execution receives a new run ID and a snapshot of all current
library IDs/names, ordered by library ID. Each library gets a separate child
identity. Libraries added afterward are not retroactively included; a removed
library becomes an unavailable child when admission reaches it. Definition,
run, child, library, and scan-job IDs have distinct roles. Native task IDs are
opaque 32-character hexadecimal strings; revisions and 100 ns values are
decimal strings on the native wire.

[Migration 0019](../../internal/database/migrations/0019_scheduled_tasks.sql)
adds six tables and the nullable `scan_jobs.task_child_id` association:

| Table | Durable responsibility |
| --- | --- |
| `task_definitions` | Stable operation identity, labels, enabled state, revision, schedule timezone |
| `task_triggers` | Ordered, revisioned schedule rules; replaced rules are retired, not erased |
| `task_runs` | Execution identity, source, actor audit snapshot, lifecycle, trigger snapshot, aggregate counters |
| `task_run_requests` | A receipt for each nonempty client request ID, including coalesced requests |
| `task_run_children` | Snapshotted library identity, exact owned scan association, counters and terminal outcome |
| `task_occurrences` | Admitted, overlapping, or missed occurrence evidence with revision/due identity |

Old schema-18 rows remain unchanged apart from the explicit nullable scan
column. Historical scan jobs are not fabricated into generic runs. The schema
enforces one active run per definition, one receipt per definition/request ID,
one child per run/library, one scan per child, and occurrence uniqueness by
trigger, revision, and due instant. No task/run deletion or automatic task-history
retention policy is exposed by this increment.

## Owned transactions and authority

Every task write uses the scanner's existing reserved PostgreSQL owner session
through `library.OwnedTransactions`. Ordinary pool reads do not grant write
ownership. The callback receives a restricted query/exec view, with no exposed
connection or transaction controls; this is a trusted internal capability,
not a SQL sandbox. Callbacks perform bounded database work, never nested
public repository calls, network requests, subprocesses, or waits for workers.

Once an owner transaction begins, its independent 20-second context protects
commit/rollback from a cancelled HTTP context. A caller can lose the response
after a successful commit, which is why native starts use durable receipts.
Ownership loss fences subsequent writes and makes execution unavailable;
restarting is required to obtain a new catalog owner.

Manual writes check the current actor before locking task records, after
waiting, and as the final database operation before commit. Native audiences
accept native administrator credentials; compatibility audiences accept
administrator Emby logins or complete application-key principals. Account and
credential locks precede task-definition/run locks. Stored role, expiry, or
client labels in a previously resolved principal cannot replace the fresh
database check. Scheduled/system operations use their own owner capability,
not an invented administrator or user.

After admission, execution belongs to the server. The initiating actor fields
are audit data, not a continuously borrowed login session. Later credential
changes restrict new management requests; they do not automatically undo an
already admitted run or its committed catalog work.

## Admission and request receipts

Manual admission locks the definition, rechecks authorization, resolves a
receipt if supplied, then either returns the active run or creates a run and
its complete library snapshot in the same transaction. Empty snapshots finish
immediately. Active means pending, running, or stopping; another manual start
does not queue a later execution behind it.

Receipts are scoped by definition and visible-ASCII request ID, at most 128
bytes. Their fingerprints bind the definition, executor, and native/compatibility
source. A matching retry returns the original run after completion or restart;
different input is a conflict. Coalescing preserves a receipt for every accepted
request without replacing the originating run's `RequestId`. IDs are not bearer
credentials, and current management authorization is still required on replay.
Omitted/empty IDs have no replay protection after a run finishes.

`Library/Refresh` now uses the same durable full-library admission. Existing
per-library scans and forced refreshes remain independent scanner jobs. The
generic executor does not turn a normal scan into `ForceProbe` or accept
client-supplied executables or arbitrary task parameters.

## Scanner bridge, progress, and cancellation

The [scan bridge](../../internal/library/task_scans.go) admits by child ID.
It checks both `child.scan_job_id` and `scan_jobs.task_child_id`, with parent,
child, then scan locks. It never adopts or cancels a job merely because it
belongs to the same library.

| Scanner outcome | Coordinator behavior |
| --- | --- |
| New owned scan | Persist job and child association atomically; enqueue after commit |
| Already-owned association | Return the same scan after validating identity and snapshot |
| Independent scan already active | Leave the child waiting; retry after it finishes |
| Queue full | Leave the child waiting; stop this admission pass and retry |
| Library no longer exists | Persist child `unavailable`; the parent eventually fails |

The existing scanner's two workers, 128-slot queue, safe probing, and normal
scan semantics remain in use. The task coordinator adds bounded reconciliation,
not another worker pool. Its default poll is 500 ms; scan hints and explicit
wakes are coalesced. It examines a bounded batch of up to 200 children/runs per
pass and advances cursors, so active prefixes cannot hide later children.
Dropped hints or an HTTP disconnect cannot erase durable admission.

Scanner progress and terminal state are copied to the linked child in the
same transaction as the job update. A retained terminal child snapshot survives
scan-history removal; a missing active association is an error, not fabricated
completion. Parent counters are recomputed from children, avoiding double
increments after retries. Compatibility progress counts terminal libraries,
not an estimate of how many files remain.

Stopping persists the parent state/reason, cancels unadmitted waiting children,
and marks only linked queued/running scans for cancellation before returning.
The coordinator repeatedly signals those exact jobs. The first stop reason
wins; repeated native cancellation returns current durable state. Queued jobs
can finish cancellation immediately; running work reports its terminal state
through the scanner. A run remains stopping until all its children are terminal.
Previously committed scan results are retained.

A parent fails if any child failed or became unavailable, otherwise reflects
interrupted/cancelled children or completes. Explicit administrator/runtime stop
produces cancellation; a recorded shutdown stop produces interruption. Completed
children with warnings can yield a completed run with `scan_warnings`.

## Clock model, timezone, and precision

The same calculation functions power preview and dispatch. The database clock
is authoritative for save, startup, and due selection. Process timers are only
wakeup hints; a due transaction checks the database time again. Embedded Go
timezone data supports explicit IANA names in minimal Linux installations.
There is no dependency on an implicit host `Local` timezone.

| Rule | Calculation policy |
| --- | --- |
| Interval | Exact period from an immutable database-clock anchor set at replacement; first occurrence is one interval later, not measured from prior completion |
| Daily | Local civil time in the definition's named timezone |
| Weekly | Local civil time on Sunday `0` through Saturday `6` |
| Startup | One event per rule at the coordinator's fixed startup instant; no future timestamp preview |

Tick values retain 100 ns units. Intervals are at least one second; duration
values cannot exceed `92233720368547758` ticks. A positive maximum runtime is
also at least one second; zero/absence means no limit. Time-of-day is within
one civil day. Persisted timezone names are at most 128 bytes. Supported
schedule instants have UTC years 1 through 9999, with bounded calendar search.

PostgreSQL due timestamps have microsecond precision. The service rounds
calculated due/preview occurrences upward, never early, while retaining the
exact ticks and immutable anchor for future calculation. Native tick strings
and the UI's BigInt conversion avoid JavaScript floating-point loss.
`DeadlineAt` is an audit timestamp, not the runtime stopwatch.

For DST gaps, a nonexistent local time is skipped. For folds, only the earliest
matching UTC instant fires; the second repeated local time is not another run.
Calendar calculation advances civil dates rather than adding fixed UTC days.
These are explicit Goby policies, not behavior established by the reference
trigger-readback captures.

## Missed work, overlap, and paused rules

At startup, the coordinator fixes one database instant and reuses it through
initialization retries. Timed occurrences through that instant become exact
missed-range summaries and are not replayed. Existing startup rules each get
one durable event receipt; retries cannot duplicate a run after the first
attempt completes. A startup rule saved later in that process waits for the
next startup. No restart is implied by saving a schedule.

During normal delayed dispatch, all older due occurrences are recorded as
missed and only the latest is offered for execution. An active run produces
an `overlap` occurrence pointing to it, without starting or queuing more work.
Its runtime limit is not replaced by the overlapping rule. Due receipt,
admission, library snapshot, and trigger advancement commit together.

Intervals use exact arithmetic for long missed ranges. Calendar catch-up counts
at most 4096 occurrences; a longer range pauses the rule rather than approximating
or silently losing its recovery position. Calculation failures retain
`next_fire_at`/`last_due_at` and expose `CalculationError`, including
`calendar_misfire_limit`, `schedule_range`, or `calendar_search_limit`.
Paused rules are excluded from timed wakeups and `NextRunAt`. Replace the
schedule to correct and resume them; there is no silent rule repair.

Saving a schedule retires old rules and increments the definition revision,
even when values are equivalent. Runs and occurrence receipts retain their
old trigger/revision references. An empty trigger array pauses automatic
execution by removing all current rules, but does not cancel a running task.
Use explicit run cancellation when that is intended.

## Maximum runtime, recovery, and shutdown

Scheduled/startup runs snapshot the admitting trigger's maximum runtime. On
transition to running, the coordinator establishes a monotonic deadline once,
before waiting for scan slots. Queue waits and owned scan work count toward
the limit. Expiry requests cooperative cancellation with `max_runtime`; wall
clock adjustments cannot extend the active stopwatch. This is not an exact
process-kill deadline or a CPU/resource-isolation guarantee.

Startup ordering is migration, scanner ownership/recovery, registry reconciliation,
run recovery, then scheduling. The scanner first marks abandoned queued/running
jobs interrupted and updates linked child snapshots. Run recovery refuses
inconsistent active links, marks unadmitted children interrupted, and closes
previous active runs as interrupted. It never resumes an old execution or
guesses that unfinished work completed.

Shutdown fences new admissions, waits for operations already admitted, persists
shutdown cancellation, and drains owned children while retaining scanner and
catalog ownership. The server closes the task manager before closing the
library. A caller's Close deadline can end its wait while background cleanup
continues. Confirmed owner loss stops further task writes; the next owner must
recover retained active history rather than treating it as success.

Scheduler initialization/dispatch failures make readiness unavailable while
admitted manual work still gets a reconciliation pass. `/readyz` reports
`tasks_not_ready` when the manager is unhealthy. Individually paused calculation
rules remain visible and do not fabricate a future occurrence. Ordinary database
errors are retried with bounded polling and rate-limited diagnostics.

## Administrator UI and operating boundary

The Tasks page separates `Available tasks` from existing `Scan history`.
Administrators can start, view paged runs/library children, request cancellation,
edit typed schedules, and preview three upcoming times. Saving requires a
preview matching the current draft. Conflicts and uncertain saves require an
explicit reload; mutations are not blindly replayed. Draft navigation is guarded.

Start receipts use a random UUID kept in memory and sessionStorage under the
current user/task. A failed or interrupted response retains it for explicit
recovery; it is cleared only after the acknowledged run is rendered. This
storage contains a request identity, not an access token. Status polling is
five seconds, pauses while the tab is hidden, and stops after read failures
until refresh. The UI manages server work and contains no consumer media player.

No new database engine, secret, or independent task-process service is required.
The existing PostgreSQL owner and scanner configuration are reused. Backups
must retain the database's definitions, receipts, rules, occurrence history,
and the existing application-key master file together. A product backup/restore
workflow and arbitrary task plug-in execution are not provided here.

The [reference read](../research/scheduled-tasks-reference.md) and
[fresh mutation](../research/scheduled-tasks-mutation-reference.md) studies
cover their stated definitions, manual operations, and trigger payloads.
Goby deliberately rejects unsupported `SystemEventTrigger` despite the
reference accepting its JSON. Weekly/numeric-weekday tolerance, DST, missed
work, monotonic runtime, and durable native receipts require Goby's own evidence;
reference readback does not prove timer execution. Real-client and broader
release acceptance remain pending; see the
[M5f verification report](verification-m5f-tasks.md) for the current gates.
