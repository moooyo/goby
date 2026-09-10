# Scheduled tasks implementation plan

Status: **M5f implemented; evidence recorded separately**. Prepared from the
schema-18 baseline after M5e. Schema 19, the task repository, scanner bridge,
scheduler, coordinator, native/compatibility handlers, and administrator UI are
implemented. The [verification report](../development/verification-m5f-tasks.md)
records the full Linux regression, isolated browser/restarts, deployment, and
main-service workflow. This plan retains the design and broader evidence
obligations; the [current API](../api/tasks.md) describes the shipped contract.
The official
[list reference](https://dev.emby.media/reference/RestAPI/ScheduledTaskService/getScheduledtasks.html)
and [trigger-update reference](https://dev.emby.media/reference/RestAPI/ScheduledTaskService/postScheduledtasksByIdTriggers.html)
were retrieved successfully on 2026-09-10 and confirm the declared DTO and
administrator requirement. These declarations do not establish actual server
behavior. The [read-only reference study](scheduled-tasks-reference.md) is now
complete: 128 complete HTTP exchanges plus one audit, with five remote recorder
guard tests passing. It observed no task mutation or execution transition.
The later [fresh-instance mutation study](scheduled-tasks-mutation-reference.md)
adds 171 records, including real manual starts/stops and bounded trigger writes,
with its own completed teardown. The corpus is now 1965 records. Full Goby
regression, browser, deployment, timer, and client acceptance are separate
requirements; neither reference study proves them.

The objective is a persistent task service used by both the administrator
dashboard and the Emby compatibility adapter. A task is an available operation;
a run is one execution of that operation. Existing per-library scan jobs remain
execution records and must not become the list of available scheduled tasks.

## Existing contracts and reusable code

- [ScheduledTaskService](../api/services/ScheduledTaskService.md) declares six
  operations: list, detail, replace triggers, start, DELETE stop, and POST stop.
  These declarations do not establish actual success codes, no-op behavior,
  casing aliases, or undocumented validation rules.
- [TaskInfo](../api/models.md#model-taskinfo) separates `Id`, `Key`, current
  `State`, optional progress, `LastExecutionResult`, and `Triggers`.
  `TaskState` declares `Idle`, `Running`, and `Cancelling`; completion status
  declares `Completed`, `Failed`, `Cancelled`, and `Aborted`.
- [TaskTriggerInfo](../api/models.md#model-tasktriggerinfo) declares `Type`,
  `TimeOfDayTicks`, `IntervalTicks`, `SystemEvent`, `DayOfWeek`, and
  `MaxRuntimeTicks`. It declares neither a trigger ID nor a timezone, and `Type`
  has no enum. The list route accepts `IsEnabled`, although `TaskInfo` has no
  declared `IsEnabled` property. Reference reads confirm 22 definitions, 14
  hidden/8 visible, 20 enabled/2 disabled, and no emitted `IsEnabled` property.
  Enabled membership differs from whether a task has any stored trigger.
- [The library queue](../../internal/library/jobs.go) admits durable jobs,
  limits each library to one active scan, records progress, and supports
  cooperative cancellation. The store has two workers and a queue of 128.
- [Catalog ownership](../../internal/library/ownership.go) reserves a PostgreSQL
  session and fences writes through the session holding its advisory lock. A
  replacement process cannot race an old owner into catalog writes.
- [Startup recovery](../../internal/library/store.go) marks abandoned queued or
  running scans `Interrupted`; it does not resume them. Scan cancellation retains
  committed catalog changes. The scan finalizer retries transient persistence
  failures instead of releasing an active library slot prematurely.
- [TasksPage](../../web/admin/src/TasksPage.tsx) presents scan history, warnings,
  counters, timestamps, and cancellation. Its visibility-aware polling and
  request cancellation can be reused, but its data model is not `TaskInfo`.
- Before M5f, [Library/Refresh](../../internal/server/libraries.go) started each
  library independently and ignored `ErrBusy`, conflating an existing scan
  with a full queue. M5f routes this operation through durable full-library
  admission and distinguishes those scanner outcomes.

The first registered executor should perform a normal scan of every library
present at admission. Existing per-library scans and explicit media detail
refreshes remain available. The registry must support additional real task
executors without advertising placeholder cleanup, backup, or provider tasks.

## Stable definitions and execution identity

Use a native definition key such as `library.scan`, owned by the code registry.
The key identifies the operation, not its display name, schedule, or latest run.
Allocate an opaque definition ID once and persist it. Repeated startup, task
execution, renaming, and schedule changes must preserve the ID. Do not copy an
ID from the reference server or compute it from mutable display text.

The observed library task has Emby `Key: "RefreshLibrary"`; map the proposed
native key `library.scan` to that wire identifier. Its observed definition ID
is an opaque string. Goby should preserve its own persisted definition ID
across both surfaces rather than copy the reference server's value. Do not add
a second ID namespace without an actual compatibility need. The reference's
Rotate log file task omits `Key`, so adapter models must not assume this field
is universally present or synthesize null for an omission.

Every execution receives a different run ID. Every library selected for that
execution receives a child record. The stable definition ID, run ID, child ID,
library ID, and existing scan-job ID are separate identities. All 18 historical
results in the read study used the enclosing definition ID for
`LastExecutionResult.Id`. Preserve that observed projection; a Goby run ID
belongs in the native API. The fresh-instance study also observed that ID in
new completed/cancelled results. Its task ID matched the original instance's
ID, which is evidence about two instances rather than an ID-generation
algorithm or a universal restart/upgrade guarantee.

Registry reconciliation inserts missing definitions and updates code-owned
labels/capabilities without replacing administrator schedules or history. A
definition without an available executor is disabled with an explicit reason;
its schedules must not fire successfully. Registry reconciliation performs no
scan by itself. Seed no automatic schedule until its behavior is selected and
documented.

## Schema 19 design

The [0019_scheduled_tasks.sql migration](../../internal/database/migrations/0019_scheduled_tasks.sql)
adds six tables and a nullable scan-child association. Keep all historical
schema-18 rows unchanged apart from that explicit nullable column. Do not infer
or backfill generic runs from old scan history. The sixth table normalizes
request receipts instead of accumulating bounded JSON aliases inside a run.

| Table | Columns and purpose |
| --- | --- |
| `task_definitions` | `id text PRIMARY KEY`, `key text UNIQUE NOT NULL`, `emby_key`, code-owned `name/description/category/is_hidden`, `revision bigint NOT NULL`, `enabled boolean NOT NULL`, `schedule_timezone text NOT NULL DEFAULT 'UTC'`, `created_at`, `updated_at`. Registry reconciliation maintains labels; historical labels are snapshotted on runs. |
| `task_triggers` | `id text PRIMARY KEY`, `task_id` FK, `schedule_revision bigint`, `position integer`, native `kind`, nullable `interval_ticks/anchor_at/time_of_day_ticks/day_of_week/timezone/max_runtime_ticks`, `next_fire_at`, `last_due_at`, `calculation_error text NOT NULL DEFAULT ''` bounded to 128 bytes, `retired_at`, and creation/update timestamps. Native kinds are interval/daily/weekly/startup; no SystemEvent executor is claimed. |
| `task_runs` | `id text PRIMARY KEY`, `task_id` FK, `state text`, `source text`, optional `request_id`, `request_fingerprint`, optional immutable trigger ID/revision/scheduled-instant/limit snapshots, actor audit snapshot, task label snapshot, `created_at`, `started_at`, `deadline_at`, `stop_requested_at`, `stop_reason`, `finished_at`, bounded `error_code/error_message`, and aggregate child/file counters. Occurrences link to runs through the table below; there is no circular run-to-occurrence FK. |
| `task_run_requests` | `(task_id, request_id) PRIMARY KEY`, `run_id`, 32-byte `fingerprint`, `created_at`; composite `(run_id, task_id)` FK to the referenced run. Every accepted native RequestId has a receipt, including requests coalesced into an existing active run. The run's own RequestID remains its initial display/audit snapshot. |
| `task_run_children` | `id text PRIMARY KEY`, `run_id` FK, `library_id text`, `library_name text`, `ordinal integer`, `state text`, optional immutable `scan_job_id text`, progress/result snapshot, `created_at`, `started_at`, `finished_at`, and bounded error fields. The library identity and name are snapshots; deleting a library must not cascade-delete generic task history. |
| `task_occurrences` | `id text PRIMARY KEY`, `task_id` FK, `trigger_id` FK, `schedule_revision bigint`, `due_at timestamptz`, `last_due_at timestamptz NULL`, `occurrence_count bigint NOT NULL DEFAULT 1`, `disposition text`, optional `run_id` FK, observation timestamp. Records whether a due occurrence admitted a run, overlapped an active run, or summarizes a consecutive missed range while the service was unavailable. |

Add nullable `scan_jobs.task_child_id`, unique when non-null, referencing the
child record with `ON DELETE RESTRICT`. Old and independently requested scans
retain null. A child's `scan_job_id` is a historical identifier rather than a
cascading reference: current library deletion also deletes that library's scan
jobs. The scanner must copy terminal counters/results into the child in the
same transaction that finalizes or interrupts the scan, so deletion after scan
completion cannot erase an outcome before the coordinator reads it.
This includes early terminal paths such as native cancellation of a queued
scan, not only the normal worker finalizer.

Enforce these database invariants:

1. A partial unique index on `task_runs(task_id)` permits only one
   `pending`, `running`, or `stopping` run for a definition.
2. `UNIQUE (run_id, library_id)` and `UNIQUE (run_id, ordinal)` preserve one
   snapshot entry per selected library and deterministic iteration.
3. A partial unique index on non-null `task_run_children(scan_job_id)` and the
   unique scan-side child reference enforce one owned scan per child.
4. `task_run_requests` provides durable retry identity through its
   `(task_id, request_id)` primary key and normalized fingerprint. Under the
   definition lock, read a receipt before selecting an active run, and insert a
   receipt for every accepted request. A coalesced request retried after the run
   finishes must return that run instead of starting another. Reject a reused
   RequestId with different input. Its composite FK prevents cross-task links.
5. `UNIQUE (trigger_id, schedule_revision, due_at)` makes trigger admission
   idempotent across a commit whose response or scheduler wake-up was lost.
6. State checks and timestamp checks distinguish active and terminal records;
   counters are nonnegative. Terminal rows cannot revert to active states.
7. Active trigger positions are unique within a task/revision. Schedule
   replacement retires previous rows and increments the definition revision
   in one transaction. Referenced retired triggers remain available to history.
8. Trigger shape checks permit only fields applicable to its internal kind:
   interval plus anchor, calendar time plus zone and optional weekday, or
   startup without calendar fields. Weekdays use a documented native 0-6 convention and
   are converted at the compatibility boundary. Do not use zero to represent
   an omitted nullable tick value.
9. A trigger's `(id, task_id, schedule_revision)` is unique. Composite FKs bind
   occurrences and scheduled runs to that exact task/revision, and occurrences
   to a run of the same task. A run's trigger ID/revision/scheduled instant are
   all absent for manual/compatibility sources and all present for
   schedule/startup sources. Retired referenced rules remain protected by
   `ON DELETE RESTRICT`.

Use native states with precise meanings. Proposed run states are `pending`,
`running`, `stopping`, `completed`, `failed`, `cancelled`, and `interrupted`.
Proposed child states are `waiting`, `queued`, `running`, `completed`, `failed`,
`cancelled`, `unavailable`, and `interrupted`. A child waiting behind an unrelated
scan remains `waiting`; it is not owned, started, or completed by this run.
These are Goby states. Their Emby projection is a separate evidence-driven
decision.

No scan starts inside the migration. Initialization should register definitions
after migration and owner acquisition. Migration acceptance must compare every
old business row and prove that the added child reference is null for old jobs.

## Shared ownership and transaction boundaries

Use the existing catalog owner for generic task writes and scan admission. Do
not introduce a task scheduler that writes through an ordinary pool connection
while the scanner is fenced through its reserved owner session. Checking owner
availability before a write is insufficient: ownership can be lost immediately
after the check.

The smallest suitable integration is a narrow owned-transaction callback on
the library store, or a mechanical extraction of the existing owner into a
shared internal package. The callback approach can be implemented first:

```go
type OwnedTransactions interface {
    WithOwnedTx(context.Context, func(OwnedTx) error) error
}

type OwnedTx interface {
    Exec(string, ...any) (pgconn.CommandTag, error)
    QueryRow(string, ...any) OwnedRow
    Query(string, ...any) (OwnedRows, error)
}

type OwnedRow interface {
    Scan(...any) error
}

type OwnedRows interface {
    Next() bool
    Scan(...any) error
    Err() error
    Close()
}
```

`library.Store.WithOwnedTx` should delegate to the existing `beginOwnedTx`,
and exclusively own commit, rollback, row cleanup, and owner-mutex release.
Its callback receives a restricted wrapper, not `pgx.Tx`. The wrapper must not
embed or expose the raw transaction or provide `Conn`, `Begin`, `Commit`,
`Rollback`, `CopyFrom`, or `SendBatch`. Restricted rows must not expose their
underlying `pgx.Rows` or connection either. The existing `ownedTx` embeds
`pgx.Tx` and overrides only a subset of its methods; passing it directly would
allow an unshielded `Query` to use a cancelled HTTP context and potentially
close the session holding the ownership lock.

The methods above deliberately accept no caller context. `Exec`, `QueryRow`,
and `Query` execute with the transaction's bounded protected context, created
independently of caller cancellation once the owned transaction begins.
`OwnedRow.Scan` and the whole `OwnedRows` lifecycle remain under that context.
The wrappers classify connection loss from query creation, `Scan`, `Next`,
`Close`, and `Err`, including delayed stream errors; a connection failure
permanently fences the owner just as an existing owned write failure does.
`Next` and `Close` must inspect the underlying terminal error when appropriate,
even if the callback never explicitly calls `Err`.

Before commit, `WithOwnedTx` closes any outstanding rows and observes their
terminal errors; no row iterator or transaction may escape the callback's
lifetime. Callback failure triggers rollback through the protected cleanup
path. Connection loss can never be ignored into a successful commit. Preserve
ordinary handled query outcomes such as `pgx.ErrNoRows` without confusing them
with ownership loss.

Callbacks perform only bounded database operations; they cannot call scanner
public methods, wait for workers, or start processes. The task repository uses
this interface. Scan admission and finalization use the same owner directly.
This preserves one write fence without requiring an immediate redesign of all
existing library-store constructors and tests.

Keep lock order explicit: scanner admission takes `Store.mu` before entering an
owned transaction; an owned-transaction callback never takes `Store.mu`. Task
operations must leave their database transaction before asking the scanner to
cancel or admit work. Do not hold a task-manager mutex while waiting for scanner
shutdown or database finalization.

For linked jobs, use database row-lock order `task_run`, `task_run_child`, then
`scan_job` in admission, start, cancellation, and finalization. Adapt the
existing scan update helpers accordingly; adding a child update after the
existing scan-row lock without a common order can deadlock with parent stop.

Start the task manager after the library store has acquired ownership and
recovered its old jobs. Recovery must also make each affected child terminal
inside that scan recovery transaction. Then recover abandoned generic runs
before enabling new admissions or trigger dispatch. Stop the task scheduler
and all admissions first during shutdown; drain owned scan work before closing
the library store and releasing ownership. Shutdown timeout may end an HTTP or
supervisor wait, but must not make the old process resume admissions.
Cooperative shutdown uses stop reason `shutdown` and a native `interrupted`
result once work has drained, preserving the distinction from a user stop.

## Full-library execution and queue admission

The start transaction revalidates the current administrator actor, resolves the
definition, and admits or identifies one run. For a new full-library run it
uses `INSERT ... SELECT` to snapshot all current library IDs/names into child
records in the same transaction. Empty-library runs have a real zero-child
execution and an explicit completed result. Libraries created later belong to
the next execution.

The coordinator considers waiting children in ordinal order and fills the
existing bounded scanner queue. It does not insert all scan jobs into memory at
once and does not increase the two-worker media-probe limit. The persistent
child list is the backlog; queue capacity controls how many can enter scanning.

Refactor the scanner's internal admission result to distinguish these outcomes:

```go
type ScanAdmissionKind string

const (
    ScanAdmitted      ScanAdmissionKind = "admitted"
    ScanAlreadyOwned  ScanAdmissionKind = "already_owned"
    ScanDuplicate     ScanAdmissionKind = "duplicate_active"
    ScanQueueFull     ScanAdmissionKind = "queue_full"
)

type ScanAdmission struct {
    Kind ScanAdmissionKind
    Job  Job
}

func (s *Store) AdmitTaskScan(ctx context.Context, childID string) (ScanAdmission, error)
```

The proposed implementation locks the parent run and selected child through the
owned transaction, checks that admission is still allowed, and inserts the
`scan_jobs` row plus both ownership links atomically. It then publishes work to
the in-memory queue while retaining the scanner admission mutex. A request
disconnect after the commit must not undo the admitted operation. A crash
between commit and publication is recovered as interrupted, not silently
restarted. A retry for a child already linked to a scan returns that same job.

Check for an existing scan before treating capacity exhaustion as a duplicate.
`ScanDuplicate` refers to a real active scan for that library that was admitted
independently. `ScanQueueFull` means the child remains waiting for capacity. Do
not convert either result into a fictitious successful scan. Existing native
per-library callers may keep their documented `409 scan_busy` response while
the internal result becomes more precise.

The proposed native full-library policy is to wait for an independently active
scan to finish, then admit this run's own normal scan. This keeps ownership
unambiguous and guarantees each snapshot entry receives this execution's scan.
It may repeat a cached scan after an existing one. Whether Emby full-library
start joins, skips, or waits for independently active scans remains a reference
question; isolate any observed behavior in the full-library executor policy.

If a library disappears before admission, persist `unavailable` with its
snapshot identity. If a child fails, continue other eligible children and
produce a failed aggregate result with bounded per-child details. Warnings
from completed scans remain warnings and do not automatically make them failed.
An aggregate `completed` result requires every selected child to complete.

Repeated native start requests with the same `RequestId` return the original
run, including after it becomes terminal. A different request while a run is
active returns that active run with `Admitted: false` and its own durable
request receipt. It creates no second child list. The fresh reference returns
`204` for a running duplicate start; that observation does not establish an
unlimited future no-replay guarantee. Goby's native retry contract is explicit.

Route `POST /emby/Library/Refresh` through this same full-library admission once
its observed behavior is captured. It must no longer lose libraries because a
queue-full error happened to share the duplicate-scan error value.

## Cancellation, maximum runtime, and results

A cancellation transaction locks the selected run, its children, and their
owned scan jobs in `run -> child -> job` order, with deterministic ID ordering
within each row group. It marks the active run `stopping`, records the reason,
marks all unadmitted children `cancelled`, and sets
`scan_jobs.cancel_requested = true` on every linked active scan in that same
transaction. It verifies each scan's child/run association before updating it.
The parent-row lock shared with admission prevents new children from entering
the queue after the stop transition. The committed parent state and committed
scan cancellation flags must never be separated by a later HTTP-dependent
operation.

After commit, wake the application-owned cancellation coordinator. The wake-up
is an optimization, not a correctness dependency: the coordinator rereads
persistent `stopping` runs and repeatedly signals cancellation to their linked
active scans until they are terminal. It runs in the application's work/drain
lifecycle, independently of the HTTP request context, with bounded contexts
for each attempt. A dropped response, cancelled request, lost wake-up, or
transient signal/persistence failure does not abandon cancellation. Repeated
signals are harmless. A running scan can also observe its durable cancellation
flag at its existing progress/finalization checks, while memory cancellation
interrupts an active probe without waiting for the next file boundary.

Add a scanner cancellation entry point that accepts a child ID and verifies the
scan's child/run association inside the owned transaction. A stale child ID,
scan ID from another run, or independent scan must never widen cancellation.
Keep the existing native per-scan cancellation route for administrators who
explicitly select that scan.

The compatibility `StopByDefinition` path locks the definition and its current
run in that same owned transaction before entering the shared child/scan stop
logic. It accepts pending/running runs, which project as Running, and reports
`ErrNotRunning` for an absent active run or an already stopping run. A missing
definition is `ErrNotFound`. The observed idle error maps to compatibility
`500`, and an unknown definition maps to `404`; native Stop by run ID retains
its idempotent contract. The stopping-state decision is Goby's explicit
concurrency policy; the reference sample did not capture Cancelling.

The existing `library.CancelJob` queued fast path immediately writes
`Cancelled` and `finished_at`. For a linked job, route that path through the
same `run -> child -> scan` bridge and persist the child's terminal status,
counters, timestamps, and message in that transaction. This applies when the
administrator cancels a queued scan from the existing scan-history page too.
Do not wait for the queue worker to copy the result later: the current
`finishTask` update finds no active row after that fast path and returns nil.

Change the linked-job terminal branch of `finishTask` so an already-terminal
scan verifies that its matching child already contains the persisted terminal
result. It must not treat `pgx.ErrNoRows` as sufficient proof of finalization.
Read and compare the matching terminal scan/child under the same ordered
transaction; if repair is deliberately supported, copy the actual stored scan
result atomically, otherwise report the inconsistency. Never invent a child
result from an absent or differently owned scan. The standalone, unlinked job
path can retain its established idempotent semantics. Every fast terminal path
must leave generic parent aggregation and subsequent library deletion safe.

Shutdown first stops new admissions and trigger dispatch while keeping the
cancellation/drain coordinator alive for existing stopping work. Do not cancel
the coordinator's lifetime context before the scanner has drained. If database
ownership is lost, fence the old process and let successor recovery classify
abandoned work; the old coordinator cannot reacquire ownership or resume writes.

A run remains `stopping` until all owned scans have actually stopped and their
terminal state has been persisted. Do not free the definition's active-run
slot merely because cancellation was requested or a context was cancelled.
The scanner must retain its resource slots until the probe/process has ended.
Committed file updates, metadata overrides, and user playback data remain.
Before changing a linked queued scan to running, the worker must lock and check
the parent state too. It cannot begin a probe in the interval between the
parent stop commit and delivery of the in-memory cancellation signal.

The [coordinator](../../internal/tasks/manager.go) establishes each active limit
once using a local `time.Now().Add(limit)` value with Go's monotonic component,
before waiting for scan capacity. Persisted `started_at/deadline_at` are UTC
audit data; a wall-clock jump must not redefine elapsed runtime or create a
new origin on each poll. Native omitted/zero runtime means no limit, positive
runtime is at least one second, and checked conversion rejects Go duration
overflow. These are Goby policies, not observed Emby timeout semantics. Startup
recovery interrupts old runs instead of reconstructing elapsed time from wall
timestamps. Do not impose `finished_at >= started_at` as a database invariant
that could reject a legitimate wall-clock rollback.

On expiry, use the same owned cancellation path with reason `max_runtime`.
Keep the native reason distinct from an administrator stop. The task remains
stopping until its children are terminal. Proposed native outcome is
`cancelled` with the timeout reason; do not assume whether the reference uses
`Cancelled`, `Failed`, or `Aborted`.

Restart policy: preserve definitions, schedules, and all past results; mark
abandoned active runs `interrupted`, copy recovered child outcomes, and release
their active-run slots. Do not automatically resume an old run or retry its
remaining libraries. Trigger misfire handling decides future work separately.
Any later resumable executor needs an explicit checkpoint contract rather than
an implicit change to all task types.

Derive native progress from completed/terminal child counts and expose actual
scan counters separately. The current scanner does not know the total number
of files. A library-level fraction can be labeled as such in the dashboard;
it must not be presented as measured file completion. Emby progress omission,
zero values, and update cadence need observation. Do not invent a smooth
percentage to satisfy a DTO property. All 22 sampled idle definitions omitted
`CurrentProgressPercentage`; none emitted null or zero. The fresh study later
observed running values including 0, 9, and 11.25 while the old terminal result
remained visible. It did not capture the Cancelling transition or establish
the reference's percentage denominator.

## Persistent schedules and time

Use typed internal schedule kinds independent of Emby `Type` strings. Current
native kinds are interval, daily calendar, weekly calendar, and startup.
Stored reference rules and the fresh write study establish the spellings
`IntervalTrigger`, `DailyTrigger`, `StartupTrigger`, and `SystemEventTrigger`.
Weekly rules were not observed. The library task stores
`IntervalTicks: 432000000000`; Hardware Detection stores a
`SystemEventTrigger` for `DisplayConfigurationChange` and a separate
`StartupTrigger`. The fresh study accepted and read back all four sampled wire
types, but did not exercise their timer/event execution. Goby does not support
the DisplayConfigurationChange system event. Startup is a distinct native
lifecycle event and cannot stand in for an unimplemented system event.

Store instants as `timestamptz` and serialize timestamps in UTC. Store each
calendar rule's named IANA timezone separately; use an explicit persisted UTC
default for newly created native schedules. Do not silently depend on the
browser timezone or on a changed Linux `/etc/localtime`. The server must supply
timezone data consistently, for example via Go's embedded `time/tzdata`.

The Emby trigger model and sampled trigger objects have no timezone field.
Determine whether their clock ticks use server-local wall time, UTC, or another
convention before implementing the adapter. `TimeOfDayTicks` must not be
interpreted as a timestamp since an epoch. The read sample includes present
time-of-day values 0, 6000000000, and 72000000000, and a maximum-runtime value
144000000000; these stored values do not prove timer interpretation or
enforcement. Day-of-week numbering/names and accepted tick precision still need
evidence.

Native scheduling policy, with its verification recorded separately:

| Concern | Implementation policy |
| --- | --- |
| Interval anchor | Persist an explicit UTC anchor; calculate due instants from the anchor instead of delaying the next interval from scan completion. |
| Calendar rules | Calculate the next local calendar date/time in the persisted zone, then resolve it to a UTC instant. Do not add 24 hours to the preceding UTC instant. |
| Missing DST time | Skip that nonexistent local occurrence; show the next real occurrence in the preview. |
| Repeated DST time | Fire once for that local date/time, using the earlier matching UTC instant. |
| Overlap | Admit at most one run per definition. Record the occurrence as overlapping an existing run; do not create an unbounded catch-up queue. |
| Downtime/misfire | Startup summarizes and skips every historical timed occurrence through one fixed startup instant. Delayed ordinary dispatch summarizes older due instants, then admits or coalesces only the latest due occurrence. Do not replay an unbounded backlog. |
| Schedule replacement | Validate the full replacement before changing any row. Commit a new revision, retired old rules, and next-fire values atomically. Already admitted runs retain their schedule/limit snapshots. |
| Empty schedule | Remove automatic rules while keeping manual execution available. The reference accepted clearing with `204`; this does not equate an empty array with its separate IsEnabled filter. |
| Clock changes | Recompute due work against a fresh database/UTC clock after wake-up; never assume a timer alone establishes the current instant. Persisted occurrence identity prevents duplicate admissions after a backward clock step. |
| Calculation failure | Set CalculationError and pause that rule while retaining its original next_fire_at and last_due_at; other rules continue. A replacement creates validated new rows with the error cleared. |
| System events | DisplayConfigurationChange is currently unsupported by Goby despite accepted reference JSON. Do not map an unknown event to startup. |

The scheduler selects due rules in a short owned transaction, inserts their
unique occurrence, admits or associates a run, and advances `next_fire_at` in
that transaction. A lost response after commit cannot admit the occurrence
again. Do not claim exactly-once execution: a process can crash after admission;
the proposed guarantee is durable, at-most-once admission per occurrence plus
explicit interrupted outcomes.

Use one scheduler loop with a wake signal for schedule changes and a bounded
sleep until the nearest due rule. Do not create an unbounded goroutine per
trigger. The native limit is 32 active rules per definition, at least one second
for a positive interval/runtime, and at most ten preview occurrences. Preview
uses the same pure schedule function as dispatch.

Native tick fields preserve 100 ns units. PostgreSQL timestamps have microsecond
precision, so [stored due instants](../../internal/tasks/triggers.go) use an
upward microsecond ceiling, never rounding an occurrence earlier. Interval
anchors come from the database clock; future calculation retains the immutable
anchor and exact tick interval rather than adding rounded persisted intervals.
Due-range reconstruction accounts for the ceiling before counting occurrences.

The [scheduler](../../internal/tasks/scheduler.go) counts interval misfires
arithmetically and calendar misfires exactly up to 4096 occurrences. Exceeding
that calendar bound, exhausting a bounded search, or finding no representable
future instant pauses the affected rule with a bounded CalculationError. Its
next_fire_at/last_due_at are not cleared or advanced. NextDue and DispatchDue
exclude paused rules, preventing repeated immediate wake-ups while other rules
continue. Native DTOs/UI must display the error and retained recovery position;
a successful atomic replacement clears the condition through new rule rows.

Prune terminal occurrence/run history only under a documented retention policy,
never active runs or records required by live foreign keys. Retention must not
silently invalidate native retry keys during their documented retention window.
This design does not require advertising a separate cleanup task.

## Module interfaces and implementation boundaries

Add `internal/tasks` for definitions, runs, triggers, scheduling, and result
aggregation. It owns its SQL and normalized DTOs. Keep scan-file traversal,
media probing, the two-worker queue, and scan ownership checks in
`internal/library`. Put only their explicit child-admission/finalization bridge
in `internal/library/task_scans.go`.

```go
type Service interface {
    List(context.Context, ListOptions) ([]DefinitionView, error)
    Get(context.Context, string) (DefinitionView, error)
    ListRuns(context.Context, string, Page) (RunPage, error)
    GetRun(context.Context, string) (RunView, error)
    Start(context.Context, Actor, StartRequest) (Admission, error)
    Stop(context.Context, Actor, string) (RunView, error)
    ReplaceTriggers(context.Context, Actor, ReplaceTriggersRequest) (DefinitionView, error)
    Preview(context.Context, PreviewRequest) ([]time.Time, error)
    Close(context.Context) error
}

type ScanExecutor interface {
    AdmitTaskScan(context.Context, string) (library.ScanAdmission, error)
    CancelTaskScan(context.Context, string) error
}

type ScheduleCalculator interface {
    Next(Trigger, time.Time) (time.Time, error)
}
```

`StartRequest` contains definition ID, native optional request ID, normalized
executor input, and source. `ReplaceTriggersRequest` contains the definition
ID, expected revision, and typed replacement rules. `Admission` distinguishes
new admission from an existing active/retried run. `Actor` is server-created
identity information, never client-supplied administrator flags. Triggered runs
have explicit system provenance rather than a fake user.

Revalidate an administrator's live credential and authority inside privileged
mutation transactions, following the existing management-service pattern.
Native cookies and application keys retain their current authentication
boundaries. A legitimately admitted persistent scan is application work; an
ordinary logout does not silently cancel it. Future executions of saved
schedules run as the configured server service, with the schedule edit's actor
kept as audit history.

`Clock` and timer creation should be injectable for deterministic scheduler
tests. Repository methods own admission and state-transition transactions;
the scheduler and HTTP handlers cannot issue ad hoc state SQL. Use paginated
child/run reads, bounded error strings, and a coordinator wake channel to avoid
per-file polling. The database remains authoritative if an in-memory wake-up
is missed.

## Native administrator routes and React/MUI page

These routes are proposals for Goby, not upstream Emby routes:

| Route | Proposed result |
| --- | --- |
| `GET /admin/v1/tasks` | Available definitions with active run, last result, schedule summary, timezone, next due instant, supported actions, and revision. |
| `GET /admin/v1/tasks/{id}` | One definition and its editable schedule details. |
| `POST /admin/v1/tasks/{id}/runs` | `202` with `Run` and `Admitted`; optional native `RequestId` permits a safe network retry. |
| `GET /admin/v1/tasks/{id}/runs` | Paginated run history with a truthful total. |
| `GET /admin/v1/task-runs/{id}` | One run and a bounded child page; expose the continuation needed for larger runs. |
| `POST /admin/v1/task-runs/{id}/cancel` | `202` with the observed run after requesting owned cancellation; a terminal run stays terminal. |
| `PUT /admin/v1/tasks/{id}/triggers` | Full typed replacement with expected revision; `409` for a conflicting revision, without partial changes. |
| `POST /admin/v1/tasks/{id}/triggers/preview` | Read-only calculation of a small bounded set of UTC instants, shown with the selected zone. |

All native operations require the native administrator session. Apply the
existing origin/CSRF discipline to mutations, and strict bounded JSON decoding
with field-specific errors. Application keys continue to use the compatibility
surface. Never include credentials, media root paths, SQL errors, or process
arguments in task messages.

Evolve TasksPage into two clearly named views: available tasks and execution
history. The existing scan/refresh history remains reachable and retains its
mode, counters, warnings, and per-scan cancellation. Generic run details link to
owned scans without implying ownership of a preexisting independent scan.

Use MUI cards/table rows for task definitions, a run detail panel for child
outcomes, and a dialog for editing supported trigger fields. Show the actual
timezone and next occurrences before save. Changing a schedule does not require
an Emby token or exposing wire ticks in the form. Use duration/time controls and
perform checked conversion in the API client/server as appropriate.

Show `Stopping` until work has ended. A failed request to start, stop, or save
does not prove whether the server committed it; reload by request/run/revision
identity before offering an unsafe duplicate retry. Preserve the existing
abortable polling, hidden-tab pause, responsive layout, accessible dialog
labels, field errors, and keyboard actions. Stop polling on a request failure
and provide an explicit retry. Keep all product copy in English.

## Emby adapter and reference gates

Place compatibility handlers in `internal/server/scheduled_tasks.go`; they call
the same task service and translate only HTTP and DTO behavior. Capture the
remaining behaviors below before freezing the adapter. The completed
[read study](scheduled-tasks-reference.md) covers the nine canonical list/filter
combinations, all 22 known details before and after, ordinary-login read
authorization, stored trigger shapes, and preservation. It contains no task
mutation, application-key request, restart, or observed execution transition.
The separate [fresh mutation study](scheduled-tasks-mutation-reference.md)
establishes the bounded write/manual-run observations in the table below.

| Reference question | Observed baseline and fresh study | Remaining observation |
| --- | --- | --- |
| Stable task identity | Historical and newly completed/cancelled result IDs equal the definition ID; Rotate log file omits Key. The selected ID matches in two instances. | ID-generation algorithm, broader cross-installation behavior, restart and upgrade. |
| List filters | Canonical absent/true/false filters and all four intersections; counts 22, 14/8, 20/2 and 12/2/8/0; no IsEnabled property. | Repeated/malformed/empty/case-variant filters and changes in enabled state. |
| Authorization and routes | Anonymous reads and four mutation forms return 401; viewer returns 403 for known and unknown IDs. Administrator known reads return 200 and unknown reads/mutations return 404, under `/emby`. | Application-key authority, root aliases, namespace casing and opaque ID normalization. |
| Start | Known empty-library/media starts return 204 with observed new results/running state; running duplicate returns 204. | Arbitrary payload validation, later replay outside the observation window, and the independent Library/Refresh route relationship. |
| Stop | Both running-stop forms return 204 followed by a new Cancelled result. Both idle-stop forms return 500 with the captured text; unknown IDs return 404. | Cancelling-state transitions, prolonged cancellation, failures and broader concurrency patterns. |
| Trigger vocabulary | IntervalTrigger, DailyTrigger, StartupTrigger and SystemEventTrigger writes return 204 and read back for the tested payloads. | Real timer/event execution, additional payload shapes and weekly rules. Goby does not implement DisplayConfigurationChange. |
| Trigger identity/update | Clearing returns 204/[]; duplicate intervals remain duplicated; unknown and mixed-invalid arrays return 400 and stay [] from an empty baseline. | Null/omitted/extra-field behavior, invalid replacement over an arbitrary nonempty baseline, and broader normalization. |
| Calendar meaning | Stored daily time values, including explicit zero; no timezone or DayOfWeek field in this sample. | Server timezone, UTC offset, tick interpretation, local midnight/weekly boundaries and DST behavior. |
| Interval/runtime meaning | Stored library interval 432000000000 and thumbnail maximum runtime 144000000000. | Anchor, accepted zero/omitted/negative/overflow bounds, overlap, misfires, restart persistence and actual maximum-runtime outcome. |
| Progress/result shape | Idle progress omitted; fresh running progress includes 0/9/11.25 while LastExecutionResult retains the old terminal result; new results retain the definition ID. | Progress denominator, Cancelling observations, warning/failure results and retention across restart. |

The completed mutation study used a separately owned disposable instance,
restored its original rule, revoked capture logins, and retained evidence after
operator teardown. Further mutations still require a specific bounded recorder,
before/after snapshots, restoration, and isolation from unrelated work. The
original reference instance remains a preserved baseline.

The adapter may preserve an observed harmless quirk when useful, but it must
not lie about an unsupported trigger, accepted scan, or completed cancellation.
If Goby's scheduling safety policy intentionally differs, document that exact
difference and cover it with compatibility fixtures. Unresolved evidence
remains an open issue, not a guessed implementation requirement.

## Implementation order and acceptance evidence

The ordering below describes delivery work and its acceptance obligations.
The [M5f verification report](../development/verification-m5f-tasks.md) records
the implemented increment. This document does not mark the full release gate
complete or convert unresolved reference behavior into a native requirement.

1. Retain both completed reference studies and their exact bounded claims.
   Expand unresolved timer/client contracts through separately owned evidence,
   distinct from native implementation choices.
2. Add schema 19, registry reconciliation, run/child repositories, and the
   shared-owner transaction entry point. Preserve all schema-18 data.
3. Split duplicate/capacity results, add atomic child links and terminal
   snapshots, then implement manual full-library runs and owned cancellation.
4. Add typed schedules, persistent occurrence admission, injected-clock
   calculations, limits, recovery, and real Linux scheduler execution.
5. Add native and Emby handlers, then extend the React/MUI Tasks page with task
   definitions, run history/details, and schedule editing.
6. Run focused and full regression tests on `test-env`, then isolated browser,
   restart, and deployed acceptance. Record failures and cleanup evidence
   without replacing earlier attempts.

Required tests include protected `Query` and row iteration after HTTP context
cancellation without losing the owner; connection loss discovered during
`Rows.Next`, `Close`, or `Err` fencing the old writer; concurrent starts producing
one active run; retry IDs
after uncertain responses; more libraries than queue capacity without lost
children; an unrelated active scan that cannot be cancelled by the parent;
parent stop racing child admission/completion; stop commit followed by an
aborted HTTP request or lost wake-up still cancelling a real running probe;
durable scan cancellation flags present in that stop transaction; cancellation
retry after a transient failure; native scan-history cancellation of a queued
owned job followed by the worker's already-terminal finalizer; child-result
preservation when that library is then deleted; finalization persistence
failure; library deletion around terminal
snapshotting; exact old-owner fencing after PostgreSQL ownership loss; restart
with queued/running/stopping runs; schedule replacement racing due admission;
overlap, missed intervals, DST gaps/folds, clock changes, and bounded integer
conversion. Each claimed trigger must also cause a real run on Linux.

Browser acceptance should exercise create/edit/remove schedule rules, preview
and timezone display, optimistic-revision conflict, manual start, stopping and
terminal results, child failure, pagination, hidden-tab polling, mobile layout,
and persistence across restart. Deployment evidence must preserve preexisting
libraries/items/user state, old scan history, devices, credentials and the
application-key master file while proving only the expected new task data and
nullable scan-child associations were added.

This increment does not complete all M5 administration, M4 hardware execution,
or M6 release/client acceptance. It establishes a real generic execution and
scheduling service that later implemented operations can reuse.
