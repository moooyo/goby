# Scheduled task mutation reference

Research date: 2026-09-10 (Asia/Shanghai).
Status: **bounded fresh-instance study and teardown complete**. Goby's
subsequent implementation has a separate
[M5f verification report](../development/verification-m5f-tasks.md).

This extends the [read-only task study](scheduled-tasks-reference.md) with
controlled writes and manual scans on a separate official Emby `4.9.5.0`
instance. Starting the known task and stopping a running task returned `204`.
Both idle-stop forms returned `500`. Trigger updates accepted the sampled
known types, while unknown and mixed-invalid arrays returned `400` without
installing the valid prefix. These observations concern the selected library
task and payloads, not every task implementation or timer behavior.

## Scope and evidence accounting

The extension adds **171 records** to the preceding 1794, bringing the corpus
to **1965**:

| Stage | Complete HTTP exchanges | Other records | Total |
| --- | ---: | --- | ---: |
| Fresh-instance setup | 16 | One connection-refused readiness observation without an HTTP response | 17 |
| Controlled task capture | 153 | One audit | 154 |

The [initial readiness failure](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-setup-m5f-001-readiness-public.json)
remains a non-HTTP observation, not a successful response. The
[capture audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-audit.json)
reports 153 complete HTTP exchanges, no incomplete capture replies, 433,969
response bytes, and 8.089 seconds. Operator/test reports below are derived
evidence and do not add corpus records.

The [fixture operator](../../scripts/test-env/prepare-scheduled-tasks-fresh.py)
created a separate service, private network namespace, program-data directory,
and accounts. The [mutation recorder](../../scripts/test-env/reference-scheduled-tasks-fresh.py)
used the attested namespace's loopback port `18099`, fresh PID `3546904`, and
server ID `56fecd8ca14a4ff5a823d70d614145be`. It issued no request to the original
reference or main Goby service. Only the uniquely identified `RefreshLibrary`
task could be mutated; a reserved missing ID supplied negative controls.

Two separately registered libraries used two batches of 256 independent
synthetic-media copies. The original media stayed outside the writable fixture.
The recorder allowed at most two library creations, 256 main/480 total HTTP
attempts, 256 KiB per response, 32 MiB total response bytes, 360 seconds per
phase, and 30 polls within a 45-second stop-observation window. Fresh program
data and the 512 copies were subsequently removed by the operator; evidence
was retained.

## Manual start and stop

Routes below are relative to `/emby`; `{id}` is the observed library-task
definition `6330ee8fb4a957f33981f89aa78b030f`.

| Operation and precondition | Response | Evidence |
| --- | --- | --- |
| `POST /ScheduledTasks/Running/{id}`, no libraries | `204`, then a new `Completed` result | [Start](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-manual-empty-start.json), [result](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-manual-empty-result-0.json) |
| Known manual start with batch A | `204`; subsequent read shows `Running` | [Start](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-manual-start.json), [running read](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-manual-start-after.json) |
| Another start while batch A is running | `204`; task remains `Running` | [Duplicate start](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-duplicate-start.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-duplicate-start-after.json) |
| `DELETE /ScheduledTasks/Running/{id}`, observed `Running` immediately before | `204`, then `Idle` with a new `Cancelled` result | [Precondition](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-stop-precondition.json), [stop](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-stop.json), [result](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-a-stop-result-0.json) |
| `POST /ScheduledTasks/Running/{id}/Delete`, batch B observed `Running` immediately before | `204`, then `Idle` with another new `Cancelled` result | [Precondition](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-b-stop-precondition.json), [stop alias](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-b-stop-alias.json), [result](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-batch-b-stop-result-0.json) |
| DELETE stop while idle | `500`, `text/plain` | [Idle stop](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-idle-stop.json) |
| POST Delete stop while idle | `500`, `text/plain` | [Idle stop alias](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-idle-stop-alias.json) |

Both idle-stop bodies were exactly:

```text
Cannot cancel a Task unless it is in the Running state.
```

The running read after batch A's first start included
`CurrentProgressPercentage: 0`; the read after its duplicate start included
`9`, and the immediate DELETE-stop precondition included `11.25`. Batch B's
stop precondition included `19.6875`. While running, `LastExecutionResult`
continued to describe the preceding terminal execution. It changed to the
new result after the observed cancellation finished. The sampled reads did
**not** capture a `Cancelling` transition; do not invent one from the SDK enum.

All sampled results, including the new completed and cancelled executions,
kept `Id` equal to the task definition ID and `Key: "RefreshLibrary"`. The fresh
instance's task ID happened to equal the original instance's ID. This is an
observation about two instances, not proof of the ID-generation algorithm or
universal cross-installation stability.

The duplicate-start observation establishes its immediate `204` and continuing
running state. No subsequent automatic replay was observed in this bounded
window. It does not prove that duplicate starts can never queue or replay work
at any later time.

## Trigger writes and readback

Each case posted a JSON array to `POST /ScheduledTasks/{id}/Triggers`, from a
controlled empty-trigger baseline, then read the task back. Clearing between
cases and restoring the original array were also recorded. Successful writes
returned empty `204` responses, not the SDK's declared empty `200`.

| Submitted array | Response and readback | Evidence |
| --- | --- | --- |
| Original `IntervalTrigger`, `IntervalTicks: 432000000000` | `204`; same single rule | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-original.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-original-after.json) |
| `[]` | `204`; empty array | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-clear.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-clear-after.json) |
| Two identical 432000000000-tick interval rules | `204`; both entries retained | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-duplicate-interval.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-duplicate-interval-after.json) |
| `DailyTrigger`, `TimeOfDayTicks: 72000000000`, `MaxRuntimeTicks: 144000000000` | `204`; fields retained | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-daily.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-daily-after.json) |
| `StartupTrigger` | `204`; same rule | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-startup.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-startup-after.json) |
| `SystemEventTrigger`, `SystemEvent: DisplayConfigurationChange` | `204`; same rule | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-system-event.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-system-event-after.json) |
| `GobyOwnedUnknownTrigger` | `400`; still `[]` | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-unknown.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-unknown-after.json) |
| Valid interval followed by `GobyOwnedUnknownTrigger` | `400`; still `[]`, without installing the valid prefix | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-mixed.json), [readback](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-trigger-mixed-after.json) |

Both invalid-array bodies were `Unrecognized trigger type:
GobyOwnedUnknownTrigger` on one line. The unknown/mixed results establish
rejection without partial installation from the sampled empty baseline; they
do not establish every malformed-input case or preservation of an arbitrary
nonempty baseline after an invalid update.

Accepted writes and matching readback do not establish calendar, interval,
startup, or system-event execution. No weekly rule, DST transition, timer
overlap/misfire, or maximum-runtime expiry was exercised. In particular,
accepting `DisplayConfigurationChange` does not establish a Linux event source;
Goby currently does not implement that event. It must not advertise successful
support solely to match the reference's accepted JSON.

## Mutation authorization and unknown IDs

For all four mutation forms, anonymous requests returned `401` and ordinary
viewer requests returned `403`, for both the known task and the reserved
unknown ID. Administrator mutations of the unknown ID returned `404`, with
the exact text body `Task not found`.

| Operation | Anonymous known / unknown | Viewer known / unknown | Administrator unknown |
| --- | --- | --- | --- |
| Start | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-known-start.json) / [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-unknown-start.json) | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-known-start.json) / [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-unknown-start.json) | [404](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-control-unknown-start.json) |
| DELETE stop | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-known-stop.json) / [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-unknown-stop.json) | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-known-stop.json) / [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-unknown-stop.json) | [404](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-control-unknown-stop.json) |
| POST Delete stop | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-known-stop-alias.json) / [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-unknown-stop-alias.json) | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-known-stop-alias.json) / [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-unknown-stop-alias.json) | [404](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-control-unknown-stop-alias.json) |
| Trigger update | [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-known-triggers.json) / [401](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-anonymous-unknown-triggers.json) | [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-known-triggers.json) / [403](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-viewer-unknown-triggers.json) | [404](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-permission-control-unknown-triggers.json) |

The audit records task readbacks after these controls. No application-key
requests were made; ordinary-login permission evidence does not establish
application-key authority. Root aliases and case-variant routes were not
tested here.

## Restoration, teardown, and evidence preservation

Before handing the fixture back to the operator, the recorder restored the
original 12-hour interval array and observed the task idle. See the
[restore request](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-cleanup-restore-original-triggers.json)
and [restored detail](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-cleanup-restore-original-triggers-after.json).
Other task configuration remained unchanged. Both capture credentials were
logged out and independently returned `401` from protected Sessions reads:
[control](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-cleanup-invalid-control.json)
and [viewer](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/scheduled-tasks-fresh-m5f-cleanup-invalid-viewer.json).

The capture audit preserved 1794 prior record pairs plus 17 setup record pairs:
**3622 preceding raw/export files** at that phase. It also preserved 240 known
original source paths and all 512 owned copies during capture. The copies are
not an addition to the long-lived 240-source corpus. Two fixture libraries,
IDs `3` and `291`, and login device history were intentionally left inside the
disposable program data for operator teardown.

The later [cleanup report](../development/m5f-fresh-task-cleanup.json) records
that the fresh service stopped, PID `3546904` exited, the unit became inactive
with MainPID `0`, and the exact owned source and program-data paths were
removed. Raw/export records, manifests, and other evidence were retained under
`/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01`.
The audit's earlier `freshServiceLeftAliveForOperatorTeardown: true` is an
intermediate state, superseded by this cleanup report. The original reference
PID `3131777` and main Goby PID `3535438` remained unchanged.

The [operator safety report](../development/m5f-fresh-task-operator-tests.json)
records **19 tests passed**, with zero failures/errors/skips and synthetic
memory-only fixtures. The [recorder guard report](../development/m5f-fresh-task-recorder-tests.json)
records **8 tests passed**, zero failures/errors, zero HTTP requests, and zero
capture writes. These validate the operator/recorder guards, not Goby M5f.

| Source pin | SHA-256 |
| --- | --- |
| Fixture operator | `bca453513b40c157ecffceb51133d2790c24f13e7f66fb75bdccbab84ec6a50b` |
| Operator safety suite | `2490e4ede069e747a8dc7e385193d7ab442c71eda8cf1e1f49262976ec358922` |
| Mutation recorder | `811d14911cb1b54d27693457d3bd4191120eaec9a1b0e9dd1ad5d883078954ac` |
| Recorder guard suite | `63c3a2e63770cd334cfea4a51edf6cdc8a38697947219bda0e687ae4fb76a402` |
| Read-recorder dependency | `b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1` |

The [Goby implementation plan](scheduled-tasks-plan.md) uses these observations
at the compatibility boundary while retaining separate native run identities,
request receipts, cancellation ownership, and explicit scheduling policies.
Full regression, browser, deployment, real timer, and client acceptance must
provide their own evidence. This report was written from saved local fixtures;
no verification, SSH, or Git operation ran while preparing it.
