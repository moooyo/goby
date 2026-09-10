# Scheduled task read-only reference

Research date: 2026-09-10 (Asia/Shanghai).
Status: **bounded reference read study complete**. Goby's subsequent task
acceptance is recorded in the separate
[M5f verification report](../development/verification-m5f-tasks.md).

This study observes task definitions, existing schedules, list filters, and
read authorization on official Emby Server `4.9.5.0`. The server returned
**22 task definitions**, all idle, including the library task with
`Key: "RefreshLibrary"`. Task definitions are distinct from execution records:
all 18 returned historical results used the enclosing definition's ID.
No task was started, stopped, rescheduled, or otherwise mutated by this study.

The [implementation plan](scheduled-tasks-plan.md) records design and evidence obligations.
The later [fresh-instance mutation study](scheduled-tasks-mutation-reference.md)
adds separate evidence for bounded task writes and manual execution; it does
not change this study's read-only scope. The
[official list reference](https://dev.emby.media/reference/RestAPI/ScheduledTaskService/getScheduledtasks.html),
[official trigger-update reference](https://dev.emby.media/reference/RestAPI/ScheduledTaskService/postScheduledtasksByIdTriggers.html),
and [pinned SDK contract](../api/services/ScheduledTaskService.md) describe the
declared API; observations below come from the saved HTTP exchanges.

## Evidence accounting and capture boundary

The `scheduled-tasks-m5f-` extension adds **129 records: 128 complete HTTP
exchanges and one audit observation**. It preserves the preceding 1665 records
and brought the reference corpus to **1794 records** at this checkpoint. There were no incomplete
HTTP replies. The capture recorded **182,636 response bytes in 2.164 seconds**.
The [audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-audit.json)
records no capture failure, no cleanup error, and successful preservation checks.

The [recorder](../../scripts/test-env/reference-scheduled-tasks.py) ran through
authorized root SSH on `test-env`, in the original reference service's private
network namespace, using only `127.0.0.1:18097`. It pinned the reference PID,
server identity, namespace separation, ownership markers, dependency sources,
and the existing Goby service identity. Its limits were 64 task definitions,
480 total HTTP attempts with a 300-request main-phase bound, 256 KiB per
response, 32 MiB total response bytes, and 360 seconds per phase. The complete
definition set remained below the configured bound; it was not truncated.

Task traffic comprised the nine list/filter combinations before and after,
detail reads for all 22 IDs before and after, and seven denied/unknown-ID
controls. Supporting reads recorded users, device history/options, server
identity, and login-session cleanup. Two fresh ordinary credentials supplied
administrator and viewer authority. Their login and logout requests were the
only allowed mutations. Existing stored tokens were not used for HTTP.

The recorder issued **zero task mutations, device mutations, application-key
requests, media/encoder requests, or source writes**. These zeros describe the
capture boundary, not a conclusion that the server rejects those operations.

## List filters return task arrays

`GET /emby/ScheduledTasks` returned a bare JSON array, with
`Content-Type: application/json; charset=utf-8`. It did not return an
`Items/TotalRecordCount` envelope. All nine initial and final list reads returned
`200`, and the observed memberships were unchanged.

| Query | Task count | Initial fixture | Final fixture |
| --- | ---: | --- | --- |
| No filters | 22 | [All](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-all.json) | [All](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-all.json) |
| `IsHidden=true` | 14 | [Hidden](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-hidden-true.json) | [Hidden](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-hidden-true.json) |
| `IsHidden=false` | 8 | [Visible](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-hidden-false.json) | [Visible](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-hidden-false.json) |
| `IsEnabled=true` | 20 | [Enabled](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-enabled-true.json) | [Enabled](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-enabled-true.json) |
| `IsEnabled=false` | 2 | [Disabled](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-enabled-false.json) | [Disabled](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-enabled-false.json) |
| `IsHidden=true&IsEnabled=true` | 12 | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-hidden-true-enabled-true.json) | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-hidden-true-enabled-true.json) |
| `IsHidden=true&IsEnabled=false` | 2 | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-hidden-true-enabled-false.json) | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-hidden-true-enabled-false.json) |
| `IsHidden=false&IsEnabled=true` | 8 | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-hidden-false-enabled-true.json) | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-hidden-false-enabled-true.json) |
| `IsHidden=false&IsEnabled=false` | 0 | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-hidden-false-enabled-false.json) | [Both](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-final-hidden-false-enabled-false.json) |

The empty intersection was exactly `[]`. Omitting filters included both hidden
and visible definitions and both enabled and disabled memberships in this
instance. Hidden status is not an authorization boundary: administrator detail
reads succeeded for hidden definitions too.

`IsEnabled=false` returned `markers` and `EmbyServerBackup`. Both had nonempty
trigger arrays. Conversely, `RefreshIntros` and `ScanInternalMetadataFolderTask`
had empty trigger arrays and belonged to the enabled set. Therefore trigger
presence alone cannot reproduce this observed enabled filter. No response
contained an `IsEnabled` property. This study did not determine why the two
definitions were disabled or how that status can be changed.

Only canonical `true`/`false` filters were sent. Repeated, malformed, empty,
case-variant, or unknown query parameters were not tested against the server.

## Definition inventory and field presence

The following is the complete observed unfiltered inventory, in the initial
list's order. Names and categories are reference data, not Goby feature claims.
Definitions for plugins, Emby Connect, or other unimplemented features must not
be advertised by Goby solely because they occur here. Source:
[initial complete list](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-all.json).

| Key | Name | Category | IsHidden |
| --- | --- | --- | --- |
| `DeleteCacheFiles` | Cache file cleanup | Maintenance | true |
| `SystemUpdateTask` | Check for application updates | Application | true |
| `PluginUpdates` | Check for plugin updates | Application | true |
| `SyncPrepare` | Convert media | Downloads & Conversions | false |
| `markers` | Detect Episode Intros | Library | true |
| `DownloadOcr` | Download OCR Data | Subtitles | true |
| `DownloadSubtitles` | Download subtitles | Library | false |
| `EmbyServerBackup` | Emby Server Backup | Application | true |
| `HardwareDetection` | Hardware Detection | Application | true |
| `CleanLogFiles` | Log file cleanup | Maintenance | true |
| `RefreshIntros` | Refresh Custom Intros | Library | true |
| `RefreshAuthorizationsScheduledTask` | Refresh Emby Connect Data | Application | true |
| `RefreshGuide` | Refresh Guide | Live TV | true |
| `RefreshInternetChannels` | Refresh Internet Channels | Internet Channels | true |
| `RefreshUsers` | Refresh Users | Library | true |
| Omitted | Rotate log file | Application | false |
| `RefreshLibrary` | Scan media library | Library | false |
| `ScanInternalMetadataFolderTask` | Scan Metadata Folder | Library | false |
| `SyncDownloadNotification` | Send Download Notifications | Sync | true |
| `ServerSync` | Transfer media | Downloads & Conversions | false |
| `VacuumDatabase` | Vacuum Database | Database | false |
| `RefreshChapterImages` | Video preview thumbnail extraction | Library | false |

All 22 definitions contained `Name`, `State`, `Id`, `Triggers`, `Description`,
`Category`, and `IsHidden`. Every observed state was `Idle`.
`CurrentProgressPercentage` and `IsEnabled` were **omitted**, not emitted as
null or zero. There was no task runtime change between the baseline and final
reads, according to the audit's presence-aware comparisons.

Eighteen definitions contained `LastExecutionResult`, all with
`Status: "Completed"`. Each returned result's `Id` equaled its enclosing task
definition's `Id`; the result did not provide a separate execution ID. The
results included `StartTimeUtc`, `EndTimeUtc`, `Status`, `Name`, and `Id`, and
usually `Key`; `ErrorMessage` and `LongErrorMessage` were absent. The four
definitions without a result were `markers`, `EmbyServerBackup`,
`RefreshIntros`, and `ScanInternalMetadataFolderTask`. Their result properties
were omitted, not null. Existing completed results are historical records;
this capture did not observe those executions happen.

`Rotate log file` omitted `Key` both at the top level and inside its existing
result. It did not return `"Key": null`. See its
[initial detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-19.json)
and [final detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-final-detail-19.json).
Other returned task keys are public operation identifiers. The recorder
preserved them only in the successful task DTO positions while retaining
credential redaction elsewhere.

The audit contains derived fields such as `taskIdentifier` and `progress` that
can be null when the wire property is absent. Use each HTTP fixture's
`response.body`, not those derived summaries, for field-presence contracts.
Unchanged IDs across these reads establish short-window stability only; they
do not prove stability across server restart, upgrade, or another installation.

## Observed trigger representations

Four `Type` strings occurred in stored trigger arrays: `IntervalTrigger`,
`DailyTrigger`, `StartupTrigger`, and `SystemEventTrigger`. No trigger object
contained an ID or timezone. No weekly rule or `DayOfWeek` value appeared.
These are read representations, not a tested allowlist for writes.

The [library-task detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-6.json)
returned definition ID `6330ee8fb4a957f33981f89aa78b030f`,
`Key: "RefreshLibrary"`, category `Library`, `IsHidden: false`, and this stored
trigger array:

```json
[
  {
    "Type": "IntervalTrigger",
    "IntervalTicks": 432000000000
  }
]
```

At the conventional 100 ns per tick, that value represents 12 hours. It is a
stored interval value; the capture did not measure its execution cadence or
anchor. The same definition and configuration were present in the
[final detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-final-detail-6.json).
Its preexisting completed result used the definition ID and `RefreshLibrary`
key as well.

Other stored representations illustrate field combinations:

| Example | Stored trigger fields | Evidence |
| --- | --- | --- |
| Cache file cleanup | `Type: IntervalTrigger`, `IntervalTicks: 864000000000` | [Detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-3.json) |
| Emby Server Backup | `Type: DailyTrigger`, `TimeOfDayTicks: 6000000000` | [Detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-15.json) |
| Rotate log file | `Type: DailyTrigger`, `TimeOfDayTicks: 0` | [Detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-19.json) |
| Video preview thumbnail extraction | `Type: DailyTrigger`, `TimeOfDayTicks: 72000000000`, `MaxRuntimeTicks: 144000000000` | [Detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-20.json) |
| Download OCR Data | A trigger with only `Type: StartupTrigger` | [Detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-21.json) |
| Hardware Detection | One `SystemEventTrigger` with `SystemEvent: DisplayConfigurationChange`, plus a separate `StartupTrigger` | [Detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-5.json) |

The Hardware Detection task's existing result was completed, but this capture
did not change a display, emit a system event, restart Emby, or observe a new
hardware-detection run. The presence of `DisplayConfigurationChange` in a Linux
server's stored configuration does not establish a working Linux event source
or hardware acceleration capability.

Zero is a present `TimeOfDayTicks` value for Rotate log file. It must remain
distinguishable from a missing field. The observed `MaxRuntimeTicks` establishes
only a stored value, not timeout enforcement or the result of exceeding it.
Timezone interpretation, DST handling, interval overlap/misfire policy, trigger
validation, and persistence across a tested restart remain unresolved.

## Read authorization and missing IDs

The known-ID control used the observed Download subtitles definition. All
requests used `/emby` paths; authenticated reads supplied `X-Emby-Token` from a
fresh ordinary login. No application key was used.

| Caller | List | Known detail | Unknown detail |
| --- | --- | --- | --- |
| Anonymous | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-anonymous-tasks-list.json) | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-anonymous-tasks-known-detail.json) | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-anonymous-tasks-unknown-detail.json) |
| Ordinary viewer | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-viewer-tasks-list.json) | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-viewer-tasks-known-detail.json) | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-viewer-tasks-unknown-detail.json) |
| Administrator | [200](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-tasks-baseline-all.json) | [200](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-task-detail-0.json) | [404](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-admin-tasks-unknown-detail.json) |

Every one of the administrator's 22 initial and 22 final known-detail reads
returned a JSON object with the requested ID and status `200`. Denial and
unknown-ID responses used `text/plain`: anonymous replies reported an invalid
or expired access token, viewer replies identified missing `ManageServer`
access, and the administrator's unknown-ID reply was `Task not found`.

The same denial statuses for known and unknown IDs show that these anonymous
and viewer requests did not disclose task existence through the status code.
This matrix establishes the observed ordinary-login read permissions; it does
not establish application-key permissions, mutation permissions, root aliases,
namespace casing, or task-ID normalization.

## Cleanup, preservation, and pins

Both fresh logins returned `200`. Their logout requests independently returned
`204`, followed by a protected `GET /emby/Sessions` with each credential that
returned `401`. These independent protected reads establish credential
invalidity; they do not rely on a task route that could reject a valid viewer
for lack of administrator authority. Evidence:
[control logout](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-cleanup-logout-control.json),
[control invalidity](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-cleanup-invalid-control.json),
[viewer logout](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-cleanup-logout-viewer.json),
and [viewer invalidity](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-m5f-cleanup-invalid-viewer.json).

The two ordinary logins left their attributed device-history records `31`
(control) and `32` (viewer). They were retained, not deleted under a read-only
device policy. All 20 previously listed devices retained structural fields,
activity, and options; the previously known hidden server device `15` also
retained its directly read information and options. No unowned new device
appeared. User membership and fields remained unchanged except for explicitly
attributed activity/login timestamps of the two participating users.

All task-definition membership, configuration, and observed runtime fields
remained unchanged. The audit reports preservation of **3330 prior raw/export
files**, **240 known source paths**, and **2214 preexisting private files**.
These preservation sets can overlap and must not be added together. The audit
retains the earlier fresh-instance teardown's explicit exception for its
already removed ownership marker; it does not demand recreation of that
disposable instance.

The original Emby process remained PID `3131777`. The protected main Goby
service remained PID `3535438`, UID `995`, start ticks `23604510`, with the same
executable and binary hash. No main-service API requests, deployments, or
restarts were performed by the recorder. Raw evidence remains private under
`/opt/goby-test/exec-scratch/scheduled-tasks-m5f/private/raw`; only sanitized
exports were copied into the repository.

| Pin | Value |
| --- | --- |
| Reference product/version | Emby Server `4.9.5.0` |
| Reference server ID | `ec69ef1cf84140e88489c30326529308` |
| Audit captured at | `2026-09-10T01:56:58.967019Z` |
| Recorder SHA-256 | `b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1` |
| Guard-test source SHA-256 | `1546e5635765a3579d4909adcb56060198b0c6cf4f53906928b7a4dac615d9b8` |
| `reference-devices.py` dependency SHA-256 | `96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d` |
| `reference-api-keys.py` dependency SHA-256 | `ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8` |
| Protected Goby binary SHA-256 | `394430272da8ad6268534c1bcaa925ccffc92f61ed0b136a8ec6bbc4dae88541` |

The [remote recorder-guard report](../development/m5f-scheduled-task-recorder-tests.json)
records **five tests passed**, zero failures/errors, zero HTTP requests, and
zero capture writes. The [guard-test source](../../scripts/test-env/test-reference-scheduled-tasks.py)
covers read-route restrictions, mutation denial, scoped preservation of public
task keys, bounded oversized responses, and absent-versus-null comparison.
Those tests verify recorder behavior, not Goby's task implementation or the
reference server's mutation semantics. This report was authored from the
saved local evidence; no local test, validator, build, or runtime probe was run
while preparing it.

## Remaining evidence and implementation consequences

The proposed adapter can now use the observed bare array/detail shapes,
ordinary-login read authorization, canonical filter memberships, idle field
omissions, `RefreshLibrary` key, and definition-ID projection for the sampled
historical results. A durable Goby run ID must remain a separate native
identity; it should not replace the definition ID in this observed result
shape. Enabled status must remain independent of whether a trigger list is
empty.

The following were outside this read-only study: trigger replacement or clearing, accepted
write types/fields, task start and stop responses, idle-stop behavior, active
progress, cancellation races, duplicate/overlapping starts, timeouts, interval
anchor, restart behavior, calendar timezone/DST, weekly triggers, actual
startup/system-event execution, application-key authority, and full-client
workflows. No mutation or runtime claim should be inferred from the historical
results or from the existence of a stored trigger. The later
[fresh-instance study](scheduled-tasks-mutation-reference.md) supplies bounded
write/start/stop evidence and raises the corpus to 1965 records. Goby's database,
scheduler, UI, and cancellation implementation has its own
[acceptance evidence](../development/verification-m5f-tasks.md); reference
observations do not establish it.
