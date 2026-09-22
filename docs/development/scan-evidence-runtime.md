# Bounded scan evidence and runtime observations

Implementation contract for the active Phase 3 source. Verification and main
publication are pending; see the
[execution record](media-analysis-resilience-phase3-20260922.md). Limits below
describe admission and failure behavior, not accepted throughput or scale.

## Deployment

`GOBY_SCAN_EVIDENCE_FILE` names one bounded regular JSON file. Omitting it keeps
the existing in-memory directory-evidence mode. An enabled Linux deployment can
use a separately provisioned private spool:

```json
{
  "enabled": true,
  "directory": "/var/lib/goby/scan-evidence",
  "maxBytes": 1073741824,
  "maxDirectories": 131072,
  "maxEntries": 1048576,
  "maxFallbackHandles": 4096
}
```

The numeric values shown are the defaults and fixed ceilings. They may be
lowered: bytes must be at least 65,536 and the other limits at least one.
Unknown/duplicate keys, nulls, invalid UTF-8, invalid types and trailing JSON
are rejected. The file limit is 64 KiB. The minimal enabled object needs only
`enabled` and `directory`. Disabled configuration retains no nonzero inventory.
This is startup deployment configuration, not an HTTP-editable media path.

The directory must already exist, be canonical and private with Linux mode
0700, and belong to the Goby service user. It must not overlap media roots,
transcode/time-shift/analysis caches, operation/diagnostic scratch, diagnostics
logs, web assets, or recovery/backup directories. Runtime admission checks held
filesystem identities and aliases in addition to lexical configuration checks.
Do not place the spool on the media mount whose availability it is observing.

The store holds an exclusive filesystem lock and a manifest bound to the
persistent server identity and actual catalog endpoint/database/schema identity.
The manifest contains no database password. It records only the exact owned
pass directories that may be retired. Recovery never removes files merely
because their names share a prefix. Unknown files, changed ownership or a
changed filesystem identity prevent adoption.

## Scan behavior and bounds

Eligible Linux scans stage accepted item identities in private PostgreSQL
temporary tables on the existing catalog-owner session. Each batch is bounded
to 512 identities and 128 KiB of serialized array data; each pass permits at
most 262,144 unique identities and 64 MiB of serialized identities. There are
at most two staged passes. The owner-wide physical temporary-relation ceiling
is 128 MiB, including indexes and TOAST. It is checked after bounded batches,
so a failing batch may temporarily exceed the ceiling. A session that touched
this private state is closed rather than returned to the general pool.

Disk evidence records exact raw directory membership and source identity.
Linux export handles retain generation identity without keeping every walked
directory descriptor open. Filesystems lacking that support use a bounded
held-descriptor fallback; exhaustion disables deletion authority. It never
silently substitutes inode-number equality for generation identity.

Each spooled pass additionally owns one nonblocking inotify descriptor. Root
attachment starts the root watch; each walked directory registers its held
descriptor before the first raw `ReadDir`. At most one unrecorded directory
observation is retained. The queue applies to both exported-handle and
held-descriptor paths: restoring the original directory after a rename must
not recover deletion authority merely because its inode and timestamps match.
No watcher goroutine or directory-to-watch map is retained.

Registration attempts, including aliases of an existing kernel watch, are
bounded by `maxDirectories + 256` (131,328 at the maximum). The four-pass limit
also covers retiring passes and their watches. Kernel per-user instance/watch
and queue limits can reject a smaller population. Watch memory is kernel
resource usage, not part of the spool's logical file-byte reservation; measure
it with the admitted runtime profile. Registration failure, any queued change,
queue overflow, `IN_IGNORED`, unmount or a closed queue permanently invalidates
that pass. The queue is checked at the named-chain, absence and final
revalidation boundaries; draining or rearming never restores authority.

This namespace-history witness is admitted only for the declared local ext
family, XFS, Btrfs and tmpfs filesystem types. Successful watch registration is
not proof that remote filesystem changes are observable. NFS, CIFS, FUSE,
overlay and unknown filesystem types retain missing catalog records while
ordinary readable scan additions/updates continue. Neither an empty queue nor
these repeated observations is a filesystem lock shared with the SQL commit.
Private spool records are opened as one leaf with kernel `openat` no-follow
semantics and checked for regular type, private owner/mode and a single link;
symlinks, hard links and special files cannot supply recorded evidence.

The manager admits at most four passes including unfinished retirements, with
at most 4 GiB of reserved evidence capacity and 17,448 evidence-owned descriptor
reservations. These are admission reservations, not eager allocation or a
process-wide FD ceiling. Root-binding capture and other service work have their
own budgets. Filesystem allocation and metadata need independent free-space
margin beyond logical byte limits. Each pass reserves up to 4,096 fallback
handles, 256 root anchors and ten fixed/scratch descriptors, including the
change queue and the directory/file pair used by private record opening.

Final reconciliation excludes staged identities in SQL before retaining or
locking candidate rows. It reads exact-key pages of at most 256 rows/4 MiB,
inspects at most 262,144 candidates and retains a positive deletion closure
bounded by 32,768 items and 32 MiB. All deletions in that closure remain one
transaction. This supports a large accepted catalog with bounded deletion work;
it does not promise deleting an arbitrarily large missing catalog in one pass.
No missing candidates means no unnecessary final filesystem deletion proof.

Unreadable roots, incomplete evidence, changed identity, exhausted evidence
capacity and unfinished filesystem operations cannot authorize removal of
catalog records. Ordinary readable additions/updates can continue when evidence
admission is unavailable. Database/staging failures remain actual scan failures,
not silently successful deletion skips. A scan completion message records when
missing records were retained because the storage proof was incomplete.

## Cancellation, shutdown and recovery

A returned request or cancellation does not release an operation that is still
inside a filesystem syscall. Its evidence slot, descriptors, reservation and
filesystem owner remain until actual work and retirement finish. Successful
quota release requires actual cleanup, durable manifest update and absence of
the exact owned directory. The pass's one change-queue descriptor and all its
kernel watches close only after the final retained filesystem worker returns;
a request timeout does not close or release them early.

Native root-binding reads use the same bounded observation mechanism. They copy
configured paths under the short store lock, then perform filesystem work and
all closes without a database transaction or store lock. Capacity/deadline
exhaustion returns 503 after current administrator and row checks; an ordinary
unreachable root still returns its existing `unavailable` binding projection.
The observation count remains occupied until the actual worker and its closes
finish, even after the request returns.

If retirement finishes with an error, the store joins in-flight owned
transactions and detaches its catalog-owner connection from the pool. The raw
session and failed filesystem manager remain strongly referenced until process
exit, preserving their fences and failed reservations. Pool shutdown can finish
without returning the quarantined session to another borrower. A later database
disconnect does not release the separate filesystem lock. This failure is still
reported; detachment is not successful cleanup.

The spool and PostgreSQL temporary staging are ephemeral work, not backup data.
They add no permanent schema migration. Persistent catalog/user state retains
the existing PostgreSQL backup/recovery contract. A restored or replaced catalog
with different database/schema identity cannot silently adopt another catalog's
spool manifest; provide an independently owned directory for that new scope.
Retain unexplained failed files for diagnosis instead of deleting by prefix.

## Administrator resource API

`GET /admin/v1/runtime/resources` requires a current native administrator and
returns `Cache-Control: no-store`. Query parameters are rejected. The response
contains no credentials, user/session identities, source paths or file handles.

| Object | Fields and meaning |
| --- | --- |
| `ScanEvidence` | `Enabled`, `ActivePasses`, `RetiringPasses`, `CleanupFailures`, `ReservedBytes`, `ReservedFileDescriptors`, `MaxPasses`, `MaxReservedBytes`, `MaxReservedFileDescriptors`. Unfinished or failed retirement remains charged. |
| `StorageObservations` | Actual process-wide `Active` and `Capacity`, including canceled callers whose filesystem workers still run. |
| `DatabasePool` | Actual `MaxConns`, `TotalConns`, `IdleConns`, `AcquiredConns`, `ConstructingConns`, plus decimal-string `AcquireCount`, `AcquireDurationNanoseconds`, `EmptyAcquireCount`, `EmptyAcquireWaitNanoseconds` and `CanceledAcquireCount`. The snapshot performs no connection acquisition or SQL. |
| `OriginalStreams` | `InstanceId`, actual `ActiveCount`, fixed `CurrentLimit`/`CompletionLimit`, `CurrentCapacityDropped`, `CompletionCapacityDropped`, `NextLeaseSequence`, `NextCompletionSequence`, `OldestCompletionSequence`, `Current` and `Completed`. |

Original-stream records contain `LeaseId`, `Sequence`, `CompletionSequence`,
`ItemId`, `MediaSourceId`, `StartedUnixNano`, `CompletedUnixNano`, and `Active`.
Sequences and Unix-nanosecond timestamps are decimal strings. The current list
contains the oldest 64 active leases; omitted records are counted. The completion
ring retains the latest 128 actual once-only leave callbacks. A drop count is
evidence of bounded projection, not evidence that the omitted operation ended.
Instance identity prevents combining records from different server generations.

Pool duration/count fields are cumulative for that pool generation. Acquisition
duration includes all successful acquires; empty-pool wait includes waiting for
a connection to be returned or constructed. Report deltas within the same
generation rather than calling these individual query latency measurements.

A completed lease proves that the original-stream lifetime callback ran. It
does not independently prove source FD closure, process exit, successful media
delivery or persistence. Recovery acceptance combines these actual counters and
records with the corresponding HTTP, database, FD/syscall and process evidence.
