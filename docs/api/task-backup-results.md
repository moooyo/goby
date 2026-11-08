# Native administrator task backup results

The native administrator task reads expose an optional `BackupResult` on
automatic backup task runs. This applies to `CurrentRun` and `LastRun` in
`GET /admin/v1/tasks` and `GET /admin/v1/tasks/{id}`, the items returned by
`GET /admin/v1/tasks/{id}/runs`, and the run returned by
`GET /admin/v1/task-runs/{id}`. Existing clients can ignore the new field;
responses from older servers may omit it.

The result comes from the durable association for the exact `backup.create`
run ID. It is never inferred from the latest backup, task name, schedule,
initiator, or task completion state. A task can fail after creating a valid
archive, and an archive can be deleted after its task has completed.

| Field | Meaning |
| --- | --- |
| `Status` | `available`, `not_recorded`, `deleted`, `deleting`, `missing`, `mismatch`, or `unavailable`. |
| `BackupId` | The exact associated archive ID when its durable association can be published. |
| `State` | The actual archive state for an available result, or the durable deletion state. |
| `SizeBytes` | The canonical nonnegative decimal byte count from archive metadata. Only present for an available result. |
| `SHA256` | The actual archive digest, or an empty string before a digest exists. Only present for an available result. |
| `Verified` | The actual boolean archive verification flag. Only present for an available result. |

`not_recorded` means the run has no durable backup association. `deleted`
requires a durable deletion record. An absent object with a retained association
is `missing`; absence alone does not prove deletion. `mismatch` withholds
measurements when the recorded deployment, archive kind, identity, or digest
does not agree. `unavailable` retains ordinary task history while explicitly
withholding an optional result that could not be observed.

Both association reads are bounded by the caller's exact run IDs. Archive
metadata is observed between those reads, and a changed association cannot
reuse the earlier measurements. Administrator authorization, active ownership,
request cancellation, and the manager lifecycle are checked before returning
the projection. Authentication or audience errors terminate the HTTP response;
they are not converted into an optional unavailable result.

The projection does not reconcile, restore, verify, create, or delete archives.
It does not expose deployment IDs, filesystem paths, credentials, secrets,
source table fingerprints, or inferred skipped-file counters. The Emby task
DTO and backup archive format remain unchanged.
