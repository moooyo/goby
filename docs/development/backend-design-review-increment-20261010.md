# Backend design review increment, 2026-10-10

## Scope and evidence

This review covers permission complexity, excessive defensive work, backend
performance, and the Linux-only server boundary. The baseline is the working
tree at `d9214417`, including its pre-existing uncommitted changes. This review
does not modify production code, tests, existing experiments, or release files.

Four parallel tracks inspect authority, defensive/resource behavior,
performance, and platform boundaries. The coordinating review reads the actual
callers and checks counterexamples. Findings are deduplicated against R01-R30,
N01-N22, L01-L08, Q01-Q09, S01-S09, F01-F13, C01-C15, T01-T09 and their
implementation records. Repeated instances of an already-recorded opportunity
are not automatically new findings.

Evidence is static source and call-path inspection. Query, allocation and lock
counts below describe code paths, not measured latency or production frequency.
The proposals require targeted remote verification before implementation is
accepted. No local or remote tests, builds, benchmarks, or validation suites
were run for this review.

## Findings

| ID | Priority | Area | Recommendation |
| --- | --- | --- | --- |
| U05 | P2 | Defensive error classification | Preserve database failures from notification management locking instead of returning an authentication error. |
| U01 | P2 | Catalog query architecture | Give entity endpoints explicit count-only and narrower browse projections. |
| U03 | P3 | Permission serialization | Remove the global management lock from ordinary device-name mutation only. |
| U02 | P3 | Defensive serialization | Detect unchanged transaction-local notification references before encoding them. |
| U04 | P3 | Notification queries | Skip the item/entity query when that reference category is empty. |
| U06 | P3 | Dynamic playback projection | Avoid a full lease copy when a caller needs only current authorization and activity refresh. |
| U07 | P3 | Defensive copying | Normalize freshly decoded task selections without copying them again. |

### U05: Do not classify a management-lock failure as invalid credentials

`internal/identity/notification_secrets.go:41-42` converts every error from
`pg_advisory_xact_lock(managedUsersLockID)` into `ErrUnauthorized`. The database
pool sets a 15-second statement timeout (`internal/database/database.go:48`).
Consequently, a valid administrator or Emby user can wait for another management
transaction, encounter a database timeout, and receive an authentication error.

The error passes unchanged through notification configuration and registration
mutations (`internal/notifications/store.go:139,256,335`).
`internal/server/notifications.go:82-85` routes `ErrUnauthorized` to the identity
handler rather than its existing `503 notification_unavailable` response. No
credential query has established an authentication failure at this point.

Return a contextual wrapped database error from the lock failure. The existing
HTTP handler can keep its redacted unavailable response; raw database errors
must not enter the response. Keep rollback, lock order, and all subsequent
administrator/session checks. This changes error classification, not whether
the mutation is permitted.

Remote verification should hold the management lock, use a short transaction
statement timeout, and assert an unavailable response with no mutation or audit
commit. Include valid native and Emby actors, actual invalid credentials,
revocation after a wait, and request cancellation as separate controls.

### U01: Match entity-query work to the requested response

`internal/server/entities.go:35-50` and
`internal/server/music_entities.go:69-82` implement HTTP `Limit=0` by requesting
one entity and then discarding it. The store still performs pagination and,
when a row exists, image and user-state reads selected by the current projection
through
`internal/library/entity_user_data.go:147-190`. A response that only needs the
authorized total therefore constructs an unused result.

Ordinary Emby entity responses also omit source-item counts explicitly
(`internal/server/entities.go:122-124`), while the entity SQL produces
`count(DISTINCT i.id)` for each projected entity
(`internal/library/entities.go:136-150`,
`internal/library/music_entities.go:80-85,141-146`). The native music-management
response does consume `entity.Count` (`internal/server/music_entities.go:170`).
Tag responses use only name and ID, providing another caller for a narrower
projection rather than an independent finding.

First introduce an explicit count-only option at the entity API boundary and
skip page/attachment work for it. Preserve the domain convention that a zero
`Query.Limit` defaults to 100 (`internal/library/query.go:493-494`); simply
forwarding the HTTP zero does not implement count-only behavior. Then let Emby
browse opt out of unused source counts while retaining the complete default
domain result and native management result. Retain entity membership through
an authorized association, entity uniqueness, filters and music-role rules.

The count and page currently execute the same `eligible_entities` population in
two statements (`entities.go:143,149`; `music_entities.go:137,141`). A later
measured step can share a narrow population within one statement. Do not assume
the count statement executes an unused aggregate: PostgreSQL may prune it.
Do not assume materialization is always faster, or build a cross-request cache.
Preserve totals for out-of-range pages instead of relying solely on a window
count attached to returned rows. The count-only improvement does not require
this broader SQL rewrite.

Keep subject/key/target separation, parent authorization, query validation,
favorite semantics, the existing snapshot, and final native-actor checks.
Full count projections still require distinct source items when multiple
credits point to the same item. AlbumArtist's effective-owner expression,
orphan exclusion, ordering, and numeric entity namespaces must remain intact.

Remote verification should cover zero/empty/out-of-range pages, all entity
families, multiple credits, parent and favorite filters, userless and targeted
application keys, restricted accounts, native counts, and disabled images.
Measure SQL count, plans, buffers and allocations on small and large catalogs;
no end-to-end speedup is established here.

### U03: Narrow ordinary device-name mutation serialization

`internal/identity/device_mutations.go:65` uses `beginDeviceMutation`, which
takes the global `managedUsersLockID` at line 30. Updating one device's
`custom_name` and `revision` therefore retains the same global lock used by
other user and device management while waiting for its target device, reading
the result and writing its audit entry.

Split the ordinary options transaction entry from the deletion entry. Ordinary
rename already locks the actor account and credential in shared mode, then one
target device in update mode (`devices.go:226-253`,
`device_mutations.go:70-89`). It does not acquire a second target device or a
later management lock. Keep its checks after the device wait and after the
audit (`device_mutations.go:88,127`), native revision comparison, no-op behavior,
tombstone/generation handling and error precedence.

Registration locks its account before the device and inserts a new credential
afterward. Credential activity and device deletion lock their existing
credentials before devices. A deletion needing the
rename actor's credential waits before reaching the device; otherwise the
device row itself serializes the competing mutation. This is the static reason
the global lock appears unnecessary for ordinary rename. Concurrent remote
tests remain necessary before changing it.

Do not change `deleteDevice` or shared application-device lifecycle locks.
Emby's compatibility route first attempts the shared application-device
operation (`internal/server/devices.go:103-108`), which can still wait for its
management lock before falling back. This proposal reduces the ordinary
mutation's lock-holding interval; it does not remove every global-lock wait
from an Emby request. Native ordinary-device editing has the direct entry.

Verify different-device edits while one target row is blocked, unrelated user
management, same-device CAS, rename against registration/deletion/revocation,
expiry after waits, aliases and audit rollback. N01 and C01 retain distinct
shared-device and deletion guarantees and are not superseded.

### U02: Skip encoding unchanged notification facts

`internal/library/notification_journal.go:19-24` marshals the complete current
reference list and hashes it before determining that the journal is unchanged.
`ownedTx.Exec`, `QueryRow` and `Commit` all call the flush path
(`internal/library/ownership.go:265,282,302,322`). Once catalog changes exist,
later statements repeat the encoding even when no reference has changed.

Production examples include artwork mutation's result read, final
administrator check and commit (`artwork_management.go:363-374`), and a bounded
provider-cache batch followed by auxiliary work and commit
(`provider_tasks.go:537-546`). N20 already reduced the provider's per-item
journal writes; the remaining no-change encodings are a separate cost.

The only production append to `notificationReferences` is
`notification_journal.go:75`. The transaction-private collection deduplicates
complete immutable string-valued references and never rewrites, reorders or
clears an existing prefix. Retain the last successful reference length and
current resync value, plus the existing recorded flag, to detect unchanged
input in constant time. Update this marker only after `RecordCatalog` succeeds.

Compare resync directly to detect a resync-only change. Auxiliary catalog
batch reconstruction does not reset the reference prefix. Preserve the mutation
ID, initial
empty guard, overflow sentinel, raw-byte limit, failure retry behavior, and all
existing flush locations. `RecordCatalog` must still perform its original
overflow conversion. Do not move writes behind final authority checks or
replace database durability with an in-memory marker.

Remote verification should retain the reference append/overflow tests,
provider-cache batch/auxiliary-merge/capacity tests and final-authorization
ordering tests. Add focused observations for repeated unchanged flushes,
resync-only changes, duplicate references and failed journal writes.

### U04: Omit queries for absent notification reference categories

`internal/library/notification_projection.go:20-38` partitions references into
item and entity IDs, but lines 40 and 58 always execute both queries. Ordinary
catalog changes produce Item references (`notification_journal.go:48-57`), so
their filtering also sends an entity query with an empty bigint array. The same
filter serves fanout and later delivery revalidation.

Guard each query with the length of its collected category. A single-category
filter then issues one fewer query without removing any applicable ACL work.
The empty-array query is not claimed to scan the entire entity table.

Keep `beginSubjectRead` even for empty references, validate every input, retain
the transaction and commit, and preserve result order and full-tuple
deduplication. Item/Library SourceID values must remain in the item category;
the final source/library relationship checks are independent of the guard.
Never route a decimal opaque item ID into the entity namespace.

Verify item-only, entity-only, mixed and empty inputs, invalid entities, source
visibility changes, identical numeric text in separate namespaces and revoked
subjects. Assert only the absent category query disappears.

### U06: Separate dynamic lease validation from returned projections

`internal/dynamicsource/manager.go:444` clones the lease while holding the global
manager mutex. `cloneInfo` copies streams, chapters and nested timing/Dolby
Vision values (`manager.go:668-686`). Three production callers discard this
value: `Acquire` at line 463, `Subtitle` in `subtitles.go:136`, and playback
heartbeat in `internal/server/dynamic_timeshift.go:381`. Acquire then performs
the required independent `Input.Lease` copy at `manager.go:510`.

Share the existing lookup, fresh authorization and activity-refresh logic with
a validation-only operation that captures just the scalar facts needed by the
authorization callback. Keep the public Info result and Input.Lease as complete
independent copies. Do not expose internal slice storage or reuse an earlier
authorization merely to avoid allocation.

Preserve owner and peer context, initial-opening rejection, readable committed
facts during reconnect, and authorization outside the mutex. Public Info keeps
its pre-authorization snapshot. On authorization failure, retain the best-effort
CloseLease call with the same context and return the original authorization
error; cancellation can prevent that close. Keep the post-authorization closed
check and accessed-time update, including before later Acquire/Subtitle checks
that may fail.
Acquire's subsequent state checks and Subtitle's generation/tag fences remain
necessary. Verify reconnect, close/revoke during authorization, stale subtitle
generations, and mutation isolation of actual returned DTOs. Measure allocations
and lock duration; media metadata is bounded and no throughput gain is claimed.

### U07: Avoid copying a task selection immediately after decoding it

`internal/tasks/store.go:289` calls `cloneAnalysisSelection` from `normalizeRun`.
The helper (`analysis_admission.go:56-60`) allocates a new selection and copies
both ID slices. Production callers normalize freshly decoded Run values,
including nested CurrentRun/LastRun values. `Run.UnmarshalJSON` explicitly
clears its destination first (`publication_fence.go:28`) before decoding new
storage, so this particular copy does not establish an additional ownership
boundary.

Normalize nil LibraryIDs/ItemIDs to empty slices directly on that freshly
decoded selection. Keep nil AnalysisInput as nil and preserve all timestamp
normalization. Do not remove clones at Start admission, executor handoff,
analysis admission callbacks, or the sealed publication capability:
`runs.go:20`, `executors.go:86`, `analysis_admission.go:218`, and
`publication_fence.go:78-80` protect separate lifetimes or mutable callers.

Verify absent/null/empty selections, reused JSON decode destinations, run lists
and nested definition runs, and independent selections across admission and
publication. This is a small allocation simplification, not a major architectural
bottleneck.

## Linux boundary and rejected changes

Windows server implementation scaffolding was already removed in `27071431`
and subsequent cleanup commits. Both server executable entries select Linux;
release compilation fixes GOOS to Linux and current OCI profiles use
linux/amd64. No additional Windows server implementation was found to delete.

Retain Windows development-host tools, Windows playback-client metadata,
historical migration/archive semantics, and explicit rejection of unsupported
network-path credentials. `commanddomain/launcher_other.go` rejects unsupported
Linux architectures; the private evaluator's non-Linux refusal is not a server
implementation. Remaining redundant platform guards in tests are already
covered by earlier findings and do not justify another cleanup claim.

Do not remove post-wait/final authorization, application-key/target separation,
root-anchored opening, real process/reader retirement or recovery proof checks.
These reviewed mechanisms have independent correctness roles. A repeated
same-snapshot Latest policy read is an extension of previously recorded policy
reuse, not a new finding; NextUp's leaf-only result does not enter that folder
path.

Two profiling leads remain unaccepted as implementation recommendations:

- Backup writes linearly locate their record and sum registry sizes per chunk
  (`backupstore/writer_linux.go:131,145`, `store_linux.go:85-97,145-146`). The
  default object count is 128 and the limit is 4096. Establish meaningful CPU or
  mutex cost before adding another derived byte counter or index with its own
  recovery/deletion/partial-write invariants.
- Notification enqueue repeatedly sums `octet_length(refs::text)` over pending
  and sending deliveries (`notifications/queue.go:245,294`). Measure JSONB
  conversion and
  lock duration before adding a generated size column and migration/recovery
  changes. Keep current queue-lock, savepoint, coalescing and exact PostgreSQL
  byte-accounting semantics; do not introduce a cross-transaction total cache.

## Review-loop ledger

The loop stops only after three consecutive completed rounds add no actionable
finding. Refinements to a recorded finding and rejected profiling hypotheses
do not count as new findings.

| Round | Focus | New findings | Consecutive rounds without new findings |
| --- | --- | --- | --- |
| 1 | Parallel permission, defensive-work, query/playback and Linux inspection; historical deduplication. | U01-U04 | 0 |
| 2 | Cross-check authority, lock order, empty results and append-only facts; inspect dynamic metadata and task decode ownership. | U05-U07 | 0 |
| 3 | Counterexample review of all seven findings: transaction snapshots, reconnects, lifecycle copies, startup failures and platform imports. | None | 1 |
| 4 | Reconcile report claims with HTTP consumers, cancellation, registration order, auxiliary resync guards, empty-category filtering and current Linux cleanup commits. | None | 2 |
| 5 | Final reverse caller/consumer audit, permission and resource lifetime checks, default domain semantics, source-to-report reconciliation and Windows-server deletion evidence. | None | 3 - stop |

All four tracks completed rounds 3-5 without an independent new actionable
finding. The coordinating review reached the same result. The requested exit
condition is satisfied after five rounds. This is convergence for the reviewed
scope, not proof that no future issue or optimization can exist.

## Verification and environment

The capacity policy was read before inspecting `test-env`. Read-only SSH
inspection found roughly 28 GiB free on the persistent root filesystem and
identified the ordinary shared caches at `/root/.cache/go-build` and
`/root/go/pkg/mod`. The default remote `/usr/bin/go` reported Go 1.26.7, while
this repository declares Go 1.27.1; no version-dependent verification was
attempted and no acceptance claim relies on that executable.

No new source copy, database, compiler cache, scratch directory or worker was
created. No cleanup of shared resources or another task's artifacts was
performed. The only repository change from this review is this report.
