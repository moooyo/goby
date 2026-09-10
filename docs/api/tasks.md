# Tasks API

**M5f task increment accepted.** This describes the current source contract.
The [complete Go race run](../development/m5f-full-race-summary.json)
and [isolated browser/restart workflow](../development/m5f-tasks-browser.json)
have passed, as have [deployment](../development/m5f-deployment-evidence.json)
and the [deployed workflow](../development/m5f-deployed-tasks.json). See
[implementation and scheduling](../development/tasks.md) for persistence,
ownership, recovery, and timing rules.

The only registered executor is `library.scan`, named `Scan media library`,
with compatibility key `RefreshLibrary`. It performs normal scans of every
library present when a new run is admitted. Definitions are stable operations;
runs are executions; children are the run's snapshotted libraries. Existing
per-library scan jobs and forced media refresh remain separate operations.

## Native administrator routes

All eight routes require the native `goby_session` administrator cookie.
POST and PUT also require same-origin checks and `X-CSRF-Token`. An Emby login
or application key cannot replace the native cookie. Responses use JSON and
`Cache-Control: no-store`.

| Method and path | Input | Success |
| --- | --- | --- |
| `GET /admin/v1/tasks` | No query | `200 {Items, TotalRecordCount}` |
| `GET /admin/v1/tasks/{id}` | No query | `200 {Task}` |
| `POST /admin/v1/tasks/{id}/runs` | `{}` or `{"RequestId":"client-request-001"}` | `202 {Run, Admitted}` |
| `GET /admin/v1/tasks/{id}/runs` | Optional `StartIndex`, `Limit` | `200 {Items, TotalRecordCount, StartIndex, Limit}` |
| `GET /admin/v1/task-runs/{id}` | Optional child `StartIndex`, `Limit` | `200 {Run, Children: {Items, TotalRecordCount, StartIndex, Limit}}` |
| `POST /admin/v1/task-runs/{id}/cancel` | `{}` | `202 {Run}` |
| `PUT /admin/v1/tasks/{id}/triggers` | `{Revision, ScheduleTimezone, Triggers}` | `200 {Task}` |
| `POST /admin/v1/tasks/{id}/triggers/preview` | `{ScheduleTimezone, Triggers}` | `200 {ServerTime, Items}` |

Task and run path IDs are lowercase 32-character hexadecimal strings, not
decimal IDs or definition keys. Trigger and child IDs use the same opaque
format. Read the returned ID; do not derive it from a name or reference server.

Each native mutation/preview body is exactly one UTF-8 JSON object, at most
32 KiB, with one `application/json` Content-Type header and no query. Field
names are case-sensitive. Unknown, duplicate, missing required, or wrongly
typed fields fail; null is accepted only where specified below. Empty bodies
are not `{}`. Pagination accepts only single-valued canonical decimal
`StartIndex` from `0` through `2147483647` and `Limit` from `1` through `200`,
defaulting to `0` and `50`; the raw page query is limited to 4 KiB. Definition
listing is unpaged. Run history sorts by creation time then ID descending;
children sort by snapshotted ordinal then ID. Counts remain accurate for empty
pages beyond the end.

## Native representations

All native timestamps are UTC strings; optional timestamps are explicit nulls.
Revisions and tick values are canonical decimal **strings** to preserve integer
precision. Counters, ordinals, page values, and weekdays are JSON numbers.

| Object | Fields |
| --- | --- |
| Task | `Id`, `Key`, `Name`, `Description`, `Category`, `IsHidden`, `Enabled`, `Revision`, `ScheduleTimezone`, `Triggers`, `CurrentRun`, `LastRun`, `NextRunAt` |
| Trigger | `Id`, `Kind`, nullable `IntervalTicks`, `TimeOfDayTicks`, `MaxRuntimeTicks`, `DayOfWeek`, `NextFireAt`, and string `CalculationError` |
| Run identity/state | `Id`, `TaskId`, snapshotted `TaskName`, `State`, `Source`, nullable originating `RequestId`, `ScheduledFor`, `MaxRuntimeTicks` |
| Run lifecycle | `CreatedAt`, nullable `StartedAt`, `DeadlineAt`, `StopRequestedAt`, `FinishedAt`; strings `StopReason`, `ErrorCode`, `ErrorMessage` |
| Run counters | `TotalChildren`, `TerminalChildren`, `CompletedChildren`, `FailedChildren`, `CancelledChildren`, `InterruptedChildren`, `UnavailableChildren`, `Scanned`, `Added`, `Updated` |
| Child | `Id`, `RunId`, `LibraryId`, snapshotted `LibraryName`, `Ordinal`, `State`, nullable `ScanJobId`, `Scanned`, `Added`, `Updated`, `ErrorCode`, `ErrorMessage`, `CreatedAt`, nullable `StartedAt`, `FinishedAt` |

`CurrentRun` and `LastRun` are run objects or null. `NextRunAt` is the earliest
calculable timed trigger for an enabled definition, or null. A startup event
has no timed next occurrence. `Enabled` is separate from having schedules;
there is no public endpoint here for changing it. DTOs omit bearer credentials,
request fingerprints, and internal actor audit identities.

Native run states are `pending`, `running`, `stopping`, `completed`, `failed`,
`cancelled`, and `interrupted`. Child states are `waiting`, `queued`, `running`,
`completed`, `failed`, `cancelled`, `unavailable`, and `interrupted`.
Run sources are `manual`, `compatibility`, `schedule`, and `startup`.
`StopReason` is empty until set, then `administrator`, `max_runtime`, or
`shutdown`; the first reason is retained. Empty error strings mean no reported
error. A completed run can carry `scan_warnings` without becoming failed.

### Admission, receipts, and cancellation

`202` confirms durable admission or an existing matching run, not completion.
There is at most one active run per definition. Another start while it is
pending, running, or stopping returns that run with `Admitted: false`; it does
not queue an additional execution. A newly admitted empty-library run can
already be `completed` in the response.

An optional `RequestId` contains at most 128 visible ASCII bytes, without
spaces or controls. Omitted or empty means no receipt. A nonempty ID is scoped
to the definition and bound to the request fingerprint. Repeating it returns
the recorded run even after completion or restart. Each coalesced request gets
its own durable receipt; `Run.RequestId` still describes the originating run
request. Reusing a receipt for different input returns `409 request_conflict`.
Without a receipt, a retry after the previous run finishes can start new work.

Native cancellation is by run ID and is idempotent for a known terminal run.
It persists stop flags before returning; the coordinator then cancels only
that run's owned child scans. Poll the run for its terminal result. Stopping
does not roll back catalog work already committed, cancel an unrelated scan,
or delete execution history. Current administrator authority is rechecked
inside every mutation transaction before commit.

## Schedule replacement and preview

Replacement requires the current positive decimal-string `Revision`, one
explicit `ScheduleTimezone`, and the complete `Triggers` array. For example:

```json
{
  "Revision": "3",
  "ScheduleTimezone": "Europe/London",
  "Triggers": [
    {"Kind": "daily", "TimeOfDayTicks": "108000000000", "MaxRuntimeTicks": "36000000000"}
  ]
}
```

Use UTC or a loadable explicit IANA timezone, at most 128 bytes. `Local`,
filesystem paths, `posix/` and `right/` names are rejected. At most 32 rules
are accepted. Every rule requires `Kind`; the only optional fields are
`IntervalTicks`, `TimeOfDayTicks`, `DayOfWeek`, and `MaxRuntimeTicks`.
Irrelevant fields must be absent or null, not populated.

| Native `Kind` | Required values |
| --- | --- |
| `interval` | `IntervalTicks`, at least `"10000000"` (one second); anchor is assigned from the save transaction's clock |
| `daily` | `TimeOfDayTicks`, `"0"` through `"863999999999"`, interpreted in `ScheduleTimezone` |
| `weekly` | Daily time plus integer `DayOfWeek`, Sunday `0` through Saturday `6` |
| `startup` | No interval, time of day, or weekday; fires at a later owner startup, not at save time |

One tick is 100 ns. Duration fields are limited to `"92233720368547758"`.
`MaxRuntimeTicks` may be omitted, null, or `"0"` for no limit; positive values
must be at least one second. It applies to scheduled/startup runs admitted
from that rule, including their wait for scan capacity. Newly admitted manual
or compatibility runs have no trigger runtime limit; coalescing does not alter
an existing run's limit.

Every accepted replacement advances the definition revision, retires old rules,
creates new trigger IDs, and reanchors intervals, even for equivalent input.
A stale revision returns `409`. An empty array removes automatic schedules
while leaving manual starts and existing runs available. A nonempty
`CalculationError` pauses only that rule; saving a corrected replacement
explicitly resets its calculation state.

Preview accepts the same fields without `Revision` and writes nothing. Each
item has `Index`, `Occurrences`, and `Event`: timed rules return three future
UTC timestamps with `Event: null`; startup returns `Occurrences: []` and
`Event: "startup"`. Preview is advisory, not an admission or reservation.
Saving later uses its own database clock. Due times are rounded upward to
PostgreSQL microseconds so storage cannot make a 100 ns occurrence early.
See the development guide for DST, missed occurrences, and overlap policy.

Native errors use `{Error: {Code, Message, Fields?}, RequestId}`. Input errors
return `400`, content-type errors `415`, absent resources `404`, and disabled
new admissions `409 task_disabled`. Revision/request conflicts also return
`409`; unavailable task execution returns `503 task_unavailable`.

## Emby ScheduledTaskService

The six operations accept current Emby administrator logins or application keys
through the existing token carriers. Ordinary users receive `403`; absent or
revoked credentials receive `401`. Native cookies do not substitute for Emby
credentials. Key authorization is a Goby implementation contract; the reference
task studies did not test application-key requests. Mutations revalidate live
authority in the owned database transaction.

| Method and canonical path | Success |
| --- | --- |
| `GET /emby/ScheduledTasks` | `200`, bare TaskInfo array |
| `GET /emby/ScheduledTasks/{id}` | `200`, one TaskInfo |
| `POST /emby/ScheduledTasks/{id}/Triggers` | `204`, replace the complete trigger array |
| `POST /emby/ScheduledTasks/Running/{id}` | `204`, durable start/coalescing |
| `DELETE /emby/ScheduledTasks/Running/{id}` | `204`, stop the active run selected atomically by definition ID |
| `POST /emby/ScheduledTasks/Running/{id}/Delete` | Same stop contract |

All path IDs above are definition IDs, including `LastExecutionResult.Id`;
native run IDs are not compatibility task IDs. Paths support the existing
root aliases and case-insensitive route literals. Listing accepts optional
case-insensitive `IsHidden` and `IsEnabled` names with exact `true`/`false`
values. Duplicate aliases and unsupported queries are rejected. Other task
routes accept only the `api_key` authentication query. Queries are at most
4 KiB. No paging envelope or placeholder reference tasks are returned.

TaskInfo contains `Id`, `Name`, `Key`, `Description`, `Category`, `IsHidden`,
`State`, and `Triggers`. The current executor emits `Key: "RefreshLibrary"`.
Pending/running native runs project as `Running`, stopping as `Cancelling`,
and no active run as `Idle`. Active progress is the percentage of terminal
library children, not estimated per-file progress. `CurrentProgressPercentage`
is omitted while idle. `LastExecutionResult` is omitted until a terminal run
exists and keeps describing the previous terminal run while another is active.
It includes definition `Id`, `Name`, `Key`, `StartTimeUtc`, `EndTimeUtc`, and
`Status`; native interrupted maps to `Aborted`. Error text is included for
failed/interrupted results when present. `IsEnabled` and native run/receipt
fields are omitted from the wire DTO.

Compatibility trigger writes use one UTF-8 JSON **array**, at most 32 KiB and
32 entries, with a single `application/json` header. Accepted types are
`IntervalTrigger`, `DailyTrigger`, `WeeklyTrigger`, and `StartupTrigger`.
Tick fields are JSON int64 numbers here, not native decimal strings; null tick
values are rejected. Weekly `DayOfWeek` accepts exact English weekday names
or the Goby numeric tolerance `0`–`6`; responses use names. Field names are
case-insensitive, duplicates/unknown fields fail, and irrelevant fields are
invalid. Rules use the same duration/shape limits as native input and retain
the definition's current timezone. Empty arrays clear schedules; duplicate
rules are retained. A concurrent schedule change can return `409` rather than
silently overwriting it.

Known starts coalesce while a run is active; compatibility supplies no durable
client receipt. Stop accepts native pending/running states. An absent active
run or an already stopping run returns the captured `500 text/plain` message
`Cannot cancel a Task unless it is in the Running state.` Unknown definitions
return `404 text/plain` with `Task not found`. Other validation failures use
`{ResponseStatus: {ErrorCode, Message}}`.

`SystemEventTrigger` and unknown types return `400 unsupported_trigger` because
there is no supported event executor. This deliberately differs from the
reference's accepted system-event configuration. No valid prefix is installed
when a replacement is invalid. Weekly/numeric-weekday support, scheduling
timing, and the native cancellation contract are Goby choices; the
[read](../research/scheduled-tasks-reference.md) and
[mutation](../research/scheduled-tasks-mutation-reference.md) studies establish
only their recorded cases. The API does not provide arbitrary commands,
definition creation/deletion, run deletion, or an end-user player.
