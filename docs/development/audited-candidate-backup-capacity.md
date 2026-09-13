# Candidate backup capacity correction

Status: the failed attempt is closed; the environment revision and a new live
admission are pending. The verified product binary is unchanged.

The [single binary transition](audited-candidate-cancellation-transition.json)
installed the cancellation fix and preserved all 35 source tables, sequences,
recovery/control/media/assets and the uninterrupted candidate PostgreSQL process.
Admission03 then completed authentication and storage checks but its first
backup failed with `capacity_exceeded`. It issued 36 normal and six cleanup
requests, revoked all three controller sessions, and admitted no restore.
The [attempt](audited-candidate-admission-03.json) and
[independent failure closeout](audited-candidate-admission03-failure-closeout.json)
remain retained. This is not a passing live admission or client result.

## Cause and scope

The live status response reported the default 8 GiB object and 32 GiB total
backup limits. Only the 64 MiB minimum free-space setting had been set for the
candidate. `Engine.Create` first reserves scratch space for
`min(MaxDatabaseBytes, MaxObjectBytes)` before opening the source snapshot.
The 8 GiB scratch reservation, 64 MiB free-space floor and metadata allowance
require at least 8,690,663,424 available bytes. The captured filesystem had
3,885,617,152 available bytes.

The retained generated object has `phase=empty`, `state=failed`,
`errorCode=quota_exceeded` and zero stored bytes; the operation is
`failed/finished/capacity_exceeded` with no source summary. Together with the
effective limits and available space, these facts locate the refusal at scratch
reservation. A zero object size alone would not identify the failure stage.
See [engine reservation](../../internal/recovery/engine.go),
[quota accounting](../../internal/backupstore/store_linux.go) and
[recovery configuration](../../internal/config/recovery.go).

## Revised fixture profile

Append exactly these previously absent settings to this candidate's private
environment through one separately recorded, atomic configuration revision:

```text
GOBY_BACKUP_MAX_OBJECT_BYTES=67108864
GOBY_BACKUP_MAX_TOTAL_BYTES=268435456
```

Retain `GOBY_BACKUP_MIN_FREE_BYTES=67108864`. The 64 MiB object limit matches the
existing bounded admission download; the 256 MiB total limit covers its scratch
and archive coexistence. Require at least 234,946,560 free bytes before dispatch
as a conservative allowance for two maximum-size objects, the free-space floor
and metadata. Check the effective limits through the admission's existing status
request. This is a small-fixture profile, not a change to product defaults or a
large-catalog capacity claim.

Preserve all six revoked sessions, three devices, the failed backup/operation,
and all original data. The failure added exactly three sessions, two devices and
eight audit rows; all earlier rows and the other 32 tables remained exact.
The recovery target is still empty. Preserve the existing binary and PostgreSQL
invocation; record one candidate stop, one environment replacement and one start
with a new runtime epoch explicitly linked to its predecessor. Do not relabel
an environment replacement as a binary replacement or change old manifests.

After the revision passes its preservation and runtime checks, freeze a new
admission scope. Admission03 cannot be resumed. Reuse the unchanged product's
2,264-test race run and Linux build; verify only the changed configuration
operator/readers and the new live workflow. Future capacity planning must account
for declared scratch reservations as well as expected database/archive size.
