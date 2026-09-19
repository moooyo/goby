# Tasks API

**Phase 3 task compatibility and system-event additions passed their selected
remote scope and closeout.** The [phase 3 record](../development/amd-media-phase3-20260919.md)
owns that evidence; earlier acceptance below does not cover the new adapters,
event persistence or consumer workflows by itself.

**Normal-scan and fixed native media-refresh increments are accepted in their
recorded verification scopes.**
The [complete Go race run](../development/m5f-full-race-summary.json),
[isolated browser/restart workflow](../development/m5f-tasks-browser.json),
[deployment](../development/m5f-deployment-evidence.json) and
[deployed workflow](../development/m5f-deployed-tasks.json) passed for the
original `library.scan` contract. The then-native-only `library.refresh_media` executor
has separate [focused and browser acceptance](../development/task-media-refresh-verification.json)
and [final ordinary regression](../development/m5-final-regression-verification.json)
evidence. These records retain their source/profile scopes; deployment progress
is recorded in [current status](../development/current-status.md). See
[implementation and scheduling](../development/tasks.md) for ownership and
timing rules, and the [media-refresh plan](../development/task-media-refresh-plan.md)
for the media-refresh acceptance scope.

The current registry exposes five fixed executors through both supported
administrative surfaces. Compatibility keys prefixed with `Goby` are explicit
Goby extensions, not claims of equivalence to a different upstream task.

| Native key | Name | Work | Compatibility key |
| --- | --- | --- | --- |
| `library.scan` | `Scan media library` | Normal cached scan | Existing `RefreshLibrary` key |
| `library.refresh_media` | `Refresh media details` | Forced media probing for each admitted library | `GobyRefreshMediaDetails` |
| `metadata.refresh` | `Refresh online metadata` | Configured provider metadata work | `GobyRefreshOnlineMetadata` |
| `subtitle.download` | `Download missing subtitles` | Configured provider subtitle work | `DownloadSubtitles` |
| `cache.maintain` | `Maintain provider cache` | Bounded persisted provider-cache pruning | `GobyMaintainProviderCache` |

Per-library executors snapshot every library present at admission. Cache
maintenance uses a single global work child with an empty LibraryID instead.
Definitions are stable operations; runs are executions; children are their
snapshotted work units.
New definitions start as manual-only without automatic schedules. Existing
IDs, enabled state, revisions, schedules and history of supported
definitions are preserved.
Per-library scan/refresh jobs remain separate operations. Neither the existing
`RefreshLibrary` task nor `Library/Refresh` becomes a forced refresh.

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
| Trigger | `Id`, `Kind`, nullable `IntervalTicks`, `TimeOfDayTicks`, `MaxRuntimeTicks`, `DayOfWeek`, `SystemEvent`, `NextFireAt`, and string `CalculationError` |
| Run identity/state | `Id`, `TaskId`, snapshotted `TaskName`, `State`, `Source`, nullable originating `RequestId`, `ScheduledFor`, `MaxRuntimeTicks` |
| Run lifecycle | `CreatedAt`, nullable `StartedAt`, `DeadlineAt`, `StopRequestedAt`, `FinishedAt`; strings `StopReason`, `ErrorCode`, `ErrorMessage` |
| Run counters | `TotalChildren`, `TerminalChildren`, `CompletedChildren`, `FailedChildren`, `CancelledChildren`, `InterruptedChildren`, `UnavailableChildren`, `Scanned`, `Added`, `Updated` |
| Child | `Id`, `RunId`, `LibraryId`, snapshotted `LibraryName`, `Ordinal`, `State`, nullable `ScanJobId`, `Scanned`, `Added`, `Updated`, `ErrorCode`, `ErrorMessage`, `CreatedAt`, nullable `StartedAt`, `FinishedAt` |

`CurrentRun` and `LastRun` are run objects or null. `NextRunAt` is the earliest
calculable timed trigger for an enabled definition, or null. A startup event
has no timed next occurrence. `Enabled` is separate from having schedules;
there is no public endpoint here for changing it. DTOs omit bearer credentials,
request fingerprints, and internal actor audit identities.

Discover `library.refresh_media` by the native Task's `Key` and use its returned
`Id`. The start body remains `{}` or `{RequestId}`: do not send `ForceProbe`,
an executor name or arbitrary options. The persisted run's `task_key` fixes
the mode. Native Run/Child DTOs gain no `ForceProbe` field; a child links to
the existing scan-job DTO by `ScanJobId`, where the job's `ForceProbe` is visible.

Native run states are `pending`, `running`, `stopping`, `completed`, `failed`,
`cancelled`, and `interrupted`. Child states are `waiting`, `queued`, `running`,
`completed`, `failed`, `cancelled`, `unavailable`, and `interrupted`.
Run sources include `manual`, `compatibility`, `schedule`, `startup`, and
`system_event`.
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

The fingerprint uses the actual executor key. Existing `library.scan` requests
keep their previous fingerprint bytes and replay semantics. A refresh run
cannot be rebound to a normal scan by later definition reads. Normal and forced
definitions have separate active-run/receipt identities, while both use the
same bounded scanner. If an independent scan already owns a library, a task's
child waits; it must not adopt or cancel that independent job.

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
are accepted per definition, not 64 rules in one request.
Timed dispatch selects due work fairly across definitions; a small batch
must not starve one behind another. Every rule requires `Kind`; the only
optional fields are
`IntervalTicks`, `TimeOfDayTicks`, `DayOfWeek`, `SystemEvent`, and `MaxRuntimeTicks`.
Irrelevant fields must be absent or null, not populated.

| Native `Kind` | Required values |
| --- | --- |
| `interval` | `IntervalTicks`, at least `"10000000"` (one second); anchor is assigned from the save transaction's clock |
| `daily` | `TimeOfDayTicks`, `"0"` through `"863999999999"`, interpreted in `ScheduleTimezone` |
| `weekly` | Daily time plus integer `DayOfWeek`, Sunday `0` through Saturday `6` |
| `startup` | No interval, time of day, or weekday; fires at a later owner startup, not at save time |
| `system_event` | Exact `SystemEvent`: `ServerStarted`, `LibraryChanged`, or `ConfigurationChanged`; no interval, time of day or weekday |

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
`Event: "startup"`; a system-event rule returns an empty occurrence array and
its exact event name. Preview is advisory, not an admission or reservation.
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

Only definitions with an approved compatibility key are exposed here, using
the five mappings above and their existing native IDs. `RefreshLibrary`
continues to perform only normal scans. Provider tasks without their required
deployment/runtime configuration reject a new manual start with `503`; they
do not return a successful placeholder run. Provider-specific online acceptance
remains deferred.

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
values. Duplicate business aliases and unsupported queries are rejected. Other
task routes accept only the declared
[compatibility transport carriers](compatibility-transport.md). Queries are at most
4 KiB. No paging envelope or placeholder reference tasks are returned.

TaskInfo contains `Id`, `Name`, `Key`, `Description`, `Category`, `IsHidden`,
`State`, and `Triggers`. `Key` is the exact mapping above.
Pending/running native runs project as `Running`, stopping as `Cancelling`,
and no active run as `Idle`. Active progress is the percentage of terminal
work children, not estimated per-file progress. `CurrentProgressPercentage`
is omitted while idle. `LastExecutionResult` is omitted until a terminal run
exists and keeps describing the previous terminal run while another is active.
It includes definition `Id`, `Name`, `Key`, `StartTimeUtc`, `EndTimeUtc`, and
`Status`; native interrupted maps to `Aborted`. Error text is included for
failed/interrupted results when present. `IsEnabled` and native run/receipt
fields are omitted from the wire DTO.

Compatibility trigger writes use one UTF-8 JSON **array**, at most 32 KiB and
32 entries, with a single `application/json` header. Accepted types are
`IntervalTrigger`, `DailyTrigger`, `WeeklyTrigger`, `StartupTrigger`, and
`SystemEventTrigger`.
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

`SystemEventTrigger` requires an exact supported `SystemEvent` value from the
closed native list above. `LibraryChanged` and `ConfigurationChanged` are Goby
extensions. `DisplayConfigurationChange`, unknown event names and unknown
trigger types are rejected; no valid prefix is installed when a replacement
is invalid. Weekly/numeric-weekday support, scheduling
timing, and the native cancellation contract are Goby choices; the
[read](../research/scheduled-tasks-reference.md) and
[mutation](../research/scheduled-tasks-mutation-reference.md) studies establish
only their recorded cases. The API does not provide arbitrary commands,
definition creation/deletion, run deletion, or an end-user player.

## Durable system-event behavior

Source mutations and their event signal commit together. Rollbacks and no-ops
produce no signal. Each event has a durable sequence; a trigger cursor and a
receipt record the consumed sequence range, run and disposition. A newly saved
or replaced trigger starts at the current sequence and does not replay old
events. Pending committed ranges survive restart; consumed or cancelled ranges
are not retried. `ServerStarted` is emitted once per manager startup lifecycle.

Dispatch coalesces a pending range and overlapping signals into an active run
without restarting its runtime deadline. Existing owned-run cancellation and
shutdown fencing/draining still apply. Changes made by task-owned scans or
provider work suppress recursive task signals while retaining client
`LibraryChanged` delivery. There is no public event-emission endpoint. The
[development contract](../development/tasks.md) identifies the persistence and
ownership implementation and the recorded phase 3 scope; complete upstream
scheduling parity and provider-online acceptance are not implied.
