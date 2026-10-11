# Backend design review, 2026-10-11

Completed fourteen review rounds. Rounds 12-14 added no new independent
recommendations, satisfying the requested stop condition. The report contains
seventeen new recommendations and two current working-tree regressions;
unfinished historical recommendations remain separately identified.

Implementation and subsequent verification are tracked in the
[implementation record](backend-review-implementation-20261011.md).

## Scope and evidence

This review examines permission complexity, excessive defensive work, backend
performance, and the Linux-only server boundary. Its baseline is the current
working tree at `0b7d95f2cdac62aebe85b2e0eb43af64c8f2df57`, including the
pre-existing uncommitted scan, media, HLS, release, and research changes.
Production code, tests, and those existing changes are not modified by this
review.

Four parallel tracks inspect authority, defensive/resource behavior,
performance, and platform boundaries. The coordinating review reads callers,
compares the working tree with HEAD, and checks counterexamples. A separate
history pass distinguishes implemented findings from unfinished proposals.

R01-R30, N01-N22, L01-L08, Q01-Q09, S01-S09, F01-F13, C01-C15, T01-T08,
V01-V07, W01-W07, and X01-X05 have implementation records. U01-U07 remain
separate, unimplemented recommendations; T09 remains deferred. Existing
follow-through is recorded separately instead of being assigned another ID.

The evidence below is static source, call-path, lock-order, and Git inspection.
SQL counts and formatting costs describe executed code paths, not measured
production latency. Historical test results are identified as historical and
do not validate the current working tree. No local or remote tests, builds,
benchmarks, or validation suites were run for this review.

## Accepted findings

| ID | Priority | Area | Recommendation |
| --- | --- | --- | --- |
| Y06 | P2 | Current working-tree regression | Restore a bounded Stop-recovery opportunity within HLS maintenance. |
| Y01 | P2 | Catalog projection | Honor `EnableUserData=false` before computing response-only user data. |
| Y02 | P3 | Permission queries | Combine adjacent application-key validation and key-lock queries. |
| Y03 | P3 | Defensive probing | Coordinate the task loop's duplicate owner Pings. |
| Y04 | P3 | Task progress | Reuse the parent Run already locked in the same execution transaction. |
| Y05 | P3 | Packed-audio HLS | Skip rendering an unchanged public playlist after completing fresh validation. |
| Y07 | P3 | Account deletion | Reuse target session IDs obtained while locking the deletion set. |
| Y08 | P3 | Administrator user list | Read only the account summary fields consumed by the native list. |
| Y09 | P3 | Recovery dependencies | Check PostgreSQL tools at the operation that actually uses them. |
| Y10 | P3 | Secret witness | Limit the notification master-key witness query to its one consumed row. |
| Y11 | P3 | Notification mutation | Return the registration projection directly from its INSERT/UPDATE. |
| Y12 | P3 | Current working-tree performance regression | Restore the known-empty folder-image fast path. |
| Y13 | P3 | Recovery validation | Omit the middle resource validation only when no schema upgrade occurs. |
| Y14 | P3 | Compiled recovery metadata | Reuse validated embedded catalog baselines by supported version. |
| Y15 | P3 | Recovery transaction setup | Batch the fixed transaction-local configuration statements. |
| Y16 | P3 | Collection member queries | Reuse the authorized parent already read by dedicated collection routes. |
| Y17 | P3 | Playlist preview | Query only overlap with the proposed members instead of loading the entire existing playlist. |
| Y18 | P3 | Response construction | Prepare immutable DTO presentation options once per response. |
| Y19 | P3 | Capability projection | Skip optional capability queries when no capability field will be returned. |

Y06 and Y12 reopen previously implemented behavior removed by the current dirty
diff. The other seventeen findings are new recommendations in this review.
The recovery-configuration deadline issue is recorded as Q06 follow-through
below because the same deadline invariant already has a project fix and test.

### Y06: Preserve a recovery opportunity after slow HLS checks

`internal/server/hls_runtime.go:807-809` gives `maintainSessions` the complete
four-second cycle, then calls `recoverPlaybackStopIntents` with the same
context. `maintainSessions` runs four workers; each session check receives up
to 750 milliseconds (`hls_runtime.go:843-852,872`). Twenty-four continuously
active sessions with slow checks can consume the entire cycle. The recovery
loop then returns at `internal/server/playback_stop_recovery.go:78` before
issuing a terminal-witness query.

Active here means a live registration retaining at least one producer; it
does not require 24 simultaneously running encoder processes.

This matters when a failed Stop has retained an early-stop reservation and its
database row subsequently becomes `Stopped` or `Expired`. Repeated exhausted
cycles can leave otherwise eligible reservations uncollected. There are at
most four reservations. Production still enables correlated ownership and
early Stop in `internal/server/server.go:99-100`; an old qualification comment
does not disable that code.

The working-tree diff removes `maintainPlaybackCycle`,
`sessionMaintenanceBeforeStopRecovery`, and
`TestHTTPPlaybackStoppedRecoveryMaintenanceReservesTerminalWitnessBudget`.
The committed implementation already apportions the existing cycle before
active authorization work. Restore that bounded scheduling behavior and its
regression test while preserving the initial idle sweep, existing workers,
fresh exact terminal witness, and final entry/reference checks. Do not add a
control-pool borrow, another worker, or a TTL-based release of uncertain state.

The original regression test combines 24 controlled slow callbacks with a real
database terminal row. Its historical PASS is retained at
`.artifacts/media-stop-read-isolation-20261003/final-combined4-stop-default-final-race.log:48-50`.
This is an individual PASS within a combined run that failed another Stop
retirement test; that run did not pass its full suite or reach its full/build
phases. The retained-failures table in the implementation record states this
limitation explicitly.
The [October 3 implementation record](media-stop-read-isolation-20261003.md)
documents the original budget contract. Neither is a new execution of the
current candidate. Remote verification should restore the test and cover zero,
one, and four eligible reservations, transient authorization timeouts, idle
retirement, nonterminal/deleted rows, and actual worker completion.

This does not block every Stop request. `hls_playback_ownership.go:87-92`
retains the normal fully validated durable-Stop fallback when the early lane
is full. The demonstrated issue is recovery starvation and lost early-lane
capacity, not removal of that fallback.

### Y01: Stop computing explicitly disabled user-data attachments

`internal/server/items.go:748-754` propagates only `Browse` and
`ImagesDisabled` into `QueryProjection`. `EnableUserData=false` takes effect
later, when `applyItemSwitches` deletes the DTO field at lines 766-770.
Before that deletion, `internal/library/query.go:200` has already called
`attachUserData`.

For a nonempty supported leaf page with a selected user, the attachment path
queries `user_item_data` (`internal/library/userdata_query.go:50`). Folder
pages also derive recursive physical-folder and collection summaries
(`userdata_query.go:133`, `internal/library/collections_userdata.go:81`).
These queries are already batched; this finding is unnecessary work after an
explicit output opt-out, not an N+1 claim. Userless application-key reads
already avoid these user-state queries and have no such saving.

Add an explicit response projection option alongside `ImagesDisabled` and
thread it through the relevant browse callers. Skip only user-data output
attachment and the user-data portion of `populateEntityProjections`.
Keep entity images independently controlled. Preserve the default complete
domain result: native collection DTOs and user-data notifications are other
consumers of these values.

Retain the user-data predicates needed for `IsPlayed`, `IsFavorite`, resume
selection, NextUp, sorting, grouping, and authorization. These semantics do
not disappear when their output field is disabled. Preserve invalid-boolean
validation, empty results, selected-user versus application-key authority, and
the existing transaction snapshot.

Remote verification should compare leaf/folder/entity and collection results
with the flag absent, true, and false; combine false with state filters and
sorting. Count attachment SQL separately from filtering SQL and measure
allocations, query plans, buffers, and latency for large directory pages.
Not reading an unrequested attachment also removes its independent failure
path; do not promise identical behavior for an error in a query no longer run.
Main authorization, filter, and independently requested image errors remain.

### Y02: Combine validation with the next application-key row lock

`internal/identity/application_key_clients_activity.go:36-40` first calls
`CheckApplicationKey` and then selects the same key ID `FOR SHARE`.
`internal/identity/application_key_clients.go:60-64` repeats the pattern with
`FOR UPDATE`. `CheckApplicationKey` executes the `SELECT EXISTS` at
`internal/identity/application_keys.go:169-172`.

Both paths have already acquired their separate parent-session lock. Combine
the complete application-key predicate with the next query returning `k.id`,
using the original lock mode and `OF k`. Keep the parent session lock as a
separate first statement. The steady path omits one separately awaited SELECT;
an explicit-client request that restarts into the write path can omit one in
each transaction. Driver/protocol work determines the actual wire round trips.

Preserve `validRevalidationID`, kind, user-null, expiry-null, revocation, key
existence, and previous key-ID comparison. Map only absent rows or a mismatched
key ID to `ErrUnauthorized`; retain wrapped database errors. Keep the
credential -> key -> client -> device order, release the full shared
transaction before exclusive retry, and retain default-context existence
checks. The clock-based activity query must remain after the last row wait.

Remote verification should cover stable touches, metadata updates, first client
binding, read-to-write restart, absent/malformed IDs, missing default context,
revocation during the initial parent wait, database errors, and concurrent
binding/deletion. The recommendation changes query composition, not the
application-key authority model or client-context boundary.

### Y03: Coordinate duplicate task-loop ownership probes

`internal/tasks/manager.go:343` and `:508` independently call `checkOwnership`
when a loop reaches both schedule and reconciliation checks. The default
reconcile interval is 500 milliseconds. Each call ultimately takes the catalog owner's
mutex and Pings its reserved PostgreSQL session
(`internal/library/ownership.go:137-152`). The probe can occupy that mutex for
up to its five-second database timeout.

Coordinate one periodic idle-owner probe for the normal idle loop and retain
cheap `Available` checks around later work. Production task storage and the scan
executor use the same library owner (`internal/server/server.go:242,252`).
Actual owned writes also detect connection loss on that exact session; a
successful query on the general pool is not an ownership proof.

Keep the shutdown path's own detection and `observeFailure` behavior, real
worker joins, and the independent reconciliation opportunity after an ordinary
schedule failure. Schedule and reconciliation retain separate contexts and
budgets. The schedule backoff at `manager.go:323-325` returns before its probe,
so a successful schedule return cannot stand in for an explicit probe result.
Keep the check before execution-result reaping; the minimum idle optimization
must not silently weaken active-work or shutdown admission. Do not remove all
idle probing: without writes, a disconnected
owner still needs detection. One probe per loop can defer a disconnect occurring
between phases until the next probe; record this bounded behavioral difference.

Do not directly remove Start/Stop entry probes as part of the minimum change.
`Store.Start` validates RequestID before entering its owned transaction
(`internal/tasks/runs.go:21,33`). A disconnected but not-yet-observed owner plus
an invalid ID would otherwise change `ErrUnavailable` into `ErrInvalidInput`.
Cancellation has a similar ordering concern. A broader change needs an explicit
error-order decision and separate verification.

Remote verification should count owner Pings in idle cycles, terminate an idle
owner backend, exercise manual tasks while schedule initialization fails, and
cover shutdown, cancellation, actual write failure, and invalid requests.
The expected benefit is fewer round trips and mutex acquisitions, not a
measured latency improvement.

### Y04: Reuse the execution transaction's locked Run

`internal/tasks/execution_store.go:132` and `:156` read the Run `FOR UPDATE`
for progress and completion respectively. After changing only the child,
lines 149 and 198 call `refreshRun`, which reads that same parent again at
`internal/tasks/runs.go:397`. Each read selects the full `to_jsonb(r)`
projection and decodes it (`runs.go:554-562`).

Extract a private refresh operation accepting the already locked Run, leaving
the existing read-and-refresh entry for other callers. The two selected paths
can avoid one separately awaited SELECT and one full JSON encode/decode per progress or
completion report. They retain the parent lock throughout, and the current
child triggers do not update its state or aggregates for these child changes.

Retain parent-before-child locking, token and state predicates, fresh durable
child totals, the unchanged-aggregate guard, terminal activity, queue settlement,
and rollback. Do not share the Run across transactions. In particular,
`stopLocked` changes the parent before its refresh (`runs.go:266,313`); its
earlier object is not interchangeable with a current locked read.

Remote verification should cover monotonic progress, a stale token, stopping
and shutdown, child completion, aggregate mismatch, audit/queue rollback, and
no-op refresh. This differs from R10, which already suppresses unchanged parent
writes; Y04 removes a redundant parent read before that existing decision.

### Y05: Skip unchanged packed-audio playlist rendering

The publisher callback is driven by a 25-millisecond ticker
(`internal/transcode/runner_linux.go:304-311`). In
`internal/transcode/packed_hls_linux.go:152-166`, every call renders all
published AAC/MP3 segments into a new string before comparing it with
`lastList`. Disk writes are already deduplicated, but formatting and allocation
remain proportional to the published segment count while awaiting the next
closed segment.

After the existing private-playlist read, parse, historical-segment validation,
and next-file closure checks, compare the render inputs: published segment
count, target duration, and actual final-ended state. Skip rendering when those
inputs match the last successfully published public list. Preserve an explicit
first-publication state and update the render marker only after successful
publication. The fixed plan and validated immutable published prefix make this
a local rendering optimization.

Do not return merely because the private list bytes are unchanged: the next
`.tmp` file may have appeared and proved that a segment is now closed. Preserve
sticky errors, every historical segment comparison, playlist bounds,
discontinuities and timestamps, and final publication only after successful
process completion. This applies to packed AAC/MP3, not every HLS producer.

Remote verification should cover unchanged polls at 10/100/1000 segments,
new closure evidence with unchanged list bytes, target-duration changes,
successful final ENDLIST, failed completion/publication, and altered historical
segments. Measure formatting allocations and CPU separately from retained
parsing and filesystem work; no end-to-end speedup is established here.

### Y07: Reuse the locked target-session list during user deletion

`internal/identity/managed_users.go:451-472` locks and reads all target-user
sessions and the actor session in ID order. It retains only the actor-session
identity. `DeleteManagedUser` then queries the target IDs again at lines
333-348 to construct `RevokedSessionIDs`.

Return an internal deletion-lock result containing the target IDs. The lock
query can project `id, (user_id = $3)` to separate target sessions from a
different actor's session. The account locks at line 426 block new login
issuance, which requires an account lock in `device_registration.go:50-52`;
the existing sessions are themselves locked. The same-transaction set can
therefore supply the deletion result without another query or another O(N)
transfer of IDs.

Preserve sorted IDs, expired and revoked sessions, a non-nil empty result,
self-deletion without duplicates, and exclusion of another actor's sessions.
Keep the current actor expiry/device read at line 330 and the post-audit
clock/authorization checks at lines 391-400. External session/resource cleanup
must continue only after a successful commit. This is a low-frequency
management simplification; it is not presented as a major latency bottleneck.

Remote verification should cover another-user deletion, self-deletion with a
remaining administrator, a target with no sessions, expired/revoked sessions,
concurrent login attempts, stale revision, expiry after a wait, and rollback
without external retirement.

### Y08: Use a summary projection for the native user list

`internal/server/admin.go:163` calls `ListUsers`, whose query at
`internal/identity/store.go:380` reads `userColumns` for every account. Those
columns include full policy and configuration JSON and two local-credential
presence flags (`store.go:40`). The native list uses only ID, name,
administrator/disabled status, password presence, and creation time through
`nativeUser` (`admin.go:15-16`). Avatar attachment consumes only IDs
(`internal/server/avatars.go:260-277`).

Give this native endpoint a fixed summary query, preserving its current
`normalized_name, id` order and all six consumed fields. This avoids moving
and allocating two unused JSON values per listed account. The data is already
excluded from the HTTP response; the opportunity concerns PostgreSQL-to-Go
transfer and allocation.

Keep `ListUsers` complete for its public-user and Emby consumers, which use
policy/configuration for visibility and DTOs. Preserve the administrator route
check, disabled accounts, total count, timestamp handling, empty-list shape,
and existing avatar behavior. Do not add a general projection framework or
cross-request cache for this one fixed summary.

Remote verification should compare native responses, ordering and avatars
across disabled/admin/password states, and retain public/Emby policy behavior.
Measure allocation and database-result bytes with large policy/configuration
values if prioritizing this low-frequency optimization. R03's user-directory
pagination and N21's login projection do not close this native-summary path.

### Y09: Check only tools used by the selected recovery operation

`NewOfflineEngine` explicitly cannot create backups
(`internal/recovery/engine.go:63-65`), but the shared constructor resolves
`pg_dump` before `pg_restore` at lines 94-100. Restoration then calls
`checkToolVersions` (`internal/backuppg/restore.go:139`), which starts both
executables with `--version` (`internal/backuppg/command.go:180-190`). The
restore/decode data path only uses `pg_restore`.

The coupling also reaches SQL-only recovery facts:
`internal/recoverydb/store.go:284` opens a backuppg snapshot for `Capture`,
reads facts and identity, and never calls `Dump`. `OpenSnapshot` nevertheless
runs both tool-version subprocesses at `internal/backuppg/snapshot.go:66`.
The online `captureRetiring` path at `internal/recovery/transition.go:404`
also reads only Witness/Facts. `ValidateDump` at
`internal/backuppg/validate_dump.go:37` checks both tools while using only the
decoder. Changing only the offline constructor would leave these dependencies.

Separate database snapshot/fact admission from dump and decoder admission.
Resolve and verify a tool when the selected operation requires it. Pure
recovery needs its bounded `pg_restore` decoder; pure SQL fact capture needs
neither tool subprocess. Backup creation still needs both dump generation and
the existing `ValidateDump` decoder before publication. Move the dump check
to an actual dump boundary rather than deleting it from the system.

Keep capabilities and errors aligned with this separation:
`internal/recovery/views.go:21-42` currently shares `engine != nil` between
backup and restore availability, and `transition.go:240` also rejects a nil
engine. Restore must not remain blocked by the old combined flag, and a
restore-capable engine must not imply that a missing dump tool can create
backups. A small operation-specific helper is sufficient; a general capability
framework is unnecessary. Offline status and listing already tolerate an
unavailable engine and are not all blocked by missing tools.

Keep the required decoder's existing early path resolution at constructor or
recovery admission. `plans.go:194` can reset an explicitly replaceable target
before the decoder is invoked at line 220; removing the unused dump dependency
must not defer the first discovery of a missing decoder until after that reset.
Retain the operation-time executable safety and version checks as well.
`ErrUnsupported` also represents database version/encoding errors, so it must
not be globally relabeled as tool unavailability.

This removes unused subprocesses and permits recovery with a valid archive,
target database and decoder when the configured dump tool is missing or has
an incompatible version. The stock OCI build already installs both tools;
the missing-tool availability impact requires a degraded or separately
configured selected path. No claim is made that the released image lacks it.

Retain deployment-selected paths, executable safety, PostgreSQL 17 checks,
schema and source/target identity, TLS interpretation, lease protection,
archive authentication, bounded COPY decoding, finalizers, timeouts, and
actual subprocess retirement. Avoid a version cache that outlives executable
identity. Remote verification should remove or replace only the task-owned
dump fixture, exercise offline restore and SQL-only Capture, and independently
prove that Create still refuses a missing dump or invalid decoder.

### Y10: Limit the notification master-key witness to its consumed row

When earlier application-key/PIN witnesses do not provide a result,
`allowNotificationMasterCreation` calls `notificationSecretRows`
(`internal/identity/notification_secrets.go:165`). That helper selects the
receiver ciphertext and all registration ciphertexts with a UNION ALL and
`ORDER BY 1,2` at lines 128-130. The caller consumes only the first row and
closes the result at lines 170-180. Normal registration admission caps history
at 512 in application code;
the query can still produce many unused ciphertext rows for one witness.

Use a witness-specific query with the same projection and ordering plus
`LIMIT 1`. Preserve the chosen first row, empty-set behavior, size checks,
generation/AAD binding, corrupted-first-row rejection, management lock, and
fresh actual master-key file observation. The full recovery validator at
line 137 must continue scanning and validating every row; do not limit the
shared full-recovery helper.

Remote verification should cover receiver-first and target-only witnesses,
empty records, malformed first and later records, invalid master keys and
historical schema absence. Verify that recovery still rejects a corrupt later
secret. The concrete saving is unconsumed result production/transfer; query
plans must establish whether underlying scans or sorting also improve.

### Y11: Return notification registration fields from the mutation

`internal/notifications/store.go:305` UPSERTs a registration, then reads it
again at line 313. The revoke UPDATE at line 339 returns only its ID and is
followed by the same full projection read at line 349.

Return the existing response fields directly from these DML statements:
`id, revision::text, enabled, event_ids, true, last_outcome`. Share the
six-field scanning and retain the DTO defaults, including `Transport`. Do not
reuse the GET helper's missing-row success semantics. Both mutations hold
the registration row lock. The intervening `cancelDeliveries`
(`internal/notifications/queue.go:501`) changes delivery history and does not
change these registration fields; current migrations add no registration
trigger that invalidates this reasoning. Each successful mutation can avoid
one additional SELECT.

Keep expected-revision CAS, a revoke UPDATE affecting no row as `ErrConflict`, event ordering,
registration generation, delivery cancellation/retirement, final session
authorization, commit failure handling, and the HTTP `FenceRegistration`
barrier. A decoded RETURNING row is not an independently committed success.

Remote verification should compare create/update/re-enable/revoke results,
test stale revisions, cancellation and final-authority failure, and confirm
that rollback never publishes the candidate DTO or bypasses actual delivery
fencing. This is a bounded management-query simplification, not a throughput
claim.

### Y12: Restore the known-empty folder-image fast path

The working-tree change at `internal/library/scan.go:1061` replaces the
committed `scanImagesWithKnownAbsence` call with `scanImages`. That wrapper
always supplies false for `knownNoLocalImages`
(`internal/library/images_scan.go:30-31`), discarding absence information
already obtained by the folder's locked catalog lookup.

For a complete stable directory with no image candidates and no existing
local-image rows, the prior path completes with an owner-session Ping
(`images_scan.go:208-217`). The current path instead prepares a source-root
witness, performs the image-set CTE/FULL JOIN comparison on that same owner
session (`internal/library/image_catalog_changes.go:67-112`), and runs the
additional final root/directory proof (`images_scan.go:248-281`). Restore the
small caller-side hint calculation tied to the exact committed folder ID.

This is a heavier query replacing a Ping plus additional source-proof work.
It is not a net extra SQL round trip or owner-mutex acquisition, and it does
not imply a new DELETE: the existing unchanged-image comparison can still
return before the write transaction. No measured end-to-end slowdown is
claimed for the current edit.

Preserve the raw existence check across old roots, exact old/new folder
identity, complete stable candidate inspection, and ordinary handling of
present or invalid images. Public image visibility is insufficient to prove
raw row absence. The original behavior and selected measurements are recorded
in [scan transaction trimming](scan-transaction-trimming-20261005.md).

The retained `TestScanFolderImageAbsenceSkipsNewAndExistingImageTransactions`
at `internal/library/scan_folder_images_noop_integration_test.go:12`
does not fully detect this regression: its tracer counts the notification
snapshot projection, INSERTs and DELETEs, but not
`/* image_catalog_unchanged */`
(`internal/library/scan_new_item_image_absence_integration_test.go:25-34`).
Do not claim it necessarily fails in the current tree. Remote verification
should explicitly observe that comparison query and the extra source-proof
work for cold/cached/force empty folders, while retaining old-root and invalid
replacement controls. Historical test success does not validate this dirty
change.

### Y13: Skip the middle resource check for an unchanged schema

`internal/backuppg/restore.go:222` validates the raw restored resource state.
When `facts.SchemaVersion` equals the current compiled version, the subsequent
work reads fingerprints, server identity, sequence bounds and ownership, and
sets sequence values. It does not modify the resource-table data. Nevertheless,
line 266 repeats the complete resource validation before the optional finalizer.

The restore transaction retains target-table locks established by TRUNCATE
at line 199. The resource validator dispatches thirteen categories
(`internal/backuppg/theme_state.go:14-53`); these inspect persisted data and
relationships rather than current wall time or the live sequence values.
For this exact no-upgrade branch, omit only that middle duplicate pass.

Retain full validation after an actual migration. After every non-nil
finalizer, retain the validation at line 274 regardless of whether migration
occurred, because the finalizer may change rows. This adds no validation for
the nil-finalizer path.
Preserve the initial `ErrSchema` to `ErrArchive` mapping, fingerprints, server
ID, sequence bounds/setval, ownership checks, transaction rollback and commit.
Keep an explicit `ctx.Err()` at the omitted check's location after ownership
validation, so cancellation cannot newly enter a side-effecting finalizer.

Remote verification should distinguish current-schema restoration, historical
upgrade, no-finalizer and mutating-finalizer paths; reject invalid raw resources
and invalid finalizer output, and exercise cancellation before finalization.
Count actual validation calls/queries and measure representative large
catalogs. Thirteen categories are not necessarily thirteen SQL statements;
their internal query work varies with version and data.

### Y14: Reuse the validated embedded catalog baseline

`internal/backuppg/catalog.go:76-100` copies and strictly decodes an embedded
baseline, checks migration facts, normalizes its complete Objects JSON and
hashes it on every `loadCatalog` call. The current schema-68 file is
1,145,135 bytes. A single `recoverydb.Store.Capture` calls `OpenSnapshot` and
then `InspectRecoveryTransaction` (`recoverydb/store.go:284,292`), each loading
the same compiled version's baseline.

Lazily retain the successfully verified Catalog and migration facts per
supported embedded version. Return a deep copy with the caller's schema name
applied. The stored baseline must remain immutable; copy table/column/primary
key/sort-key slices, sequences and their consumers, constraints and migration
facts while preserving nil/empty distinctions. Do not retain the large raw
Objects value after its successful validation, and bound cache entries to
versions actually present in the executable.

Keep the exact first-load validation and errors for invalid inputs,
unsupported versions, malformed baseline contents, migration mismatch and
bad checksums. Every call, including a cache hit, still validates the version
range and schema syntax before returning a value. Embedded versions have
gaps; do not assume every integer through the latest migration has a baseline.
Do not cache actual database catalog inspection, migration
history, row/resource validation, identity, ownership or leases. These remain
fresh even when the expected compiled baseline is reused. Independent parsing
in `compiledRecoveryDropPlan` is outside this narrow recommendation; not every
baseline parse disappears.

Remote verification should cover concurrent loads, different schema names,
mutation isolation of returned nested slices, unsupported versions and invalid
baseline controls. Measure first-load cost, warm allocations/CPU and retained
memory separately. The input is immutable executable metadata, unlike R20's
live filesystem-history observations. A small dedicated cache suffices.

### Y15: Batch transaction-local recovery configuration

`internal/backuppg/snapshot.go:266-274` sets eleven fixed PostgreSQL options
using eleven separately awaited `Exec` calls. The options establish search
path, timeouts, canonical text encodings, and row-security behavior before
backup/recovery queries. `Capture` reaches this configuration through both
`OpenSnapshot` and `InspectRecoveryTransaction`, so that path executes the
eleven-statement setup twice.

Send the existing parameterized, fully qualified
`pg_catalog.set_config($1,$2,true)` statements as one bounded `pgx.Batch` per
configuration call. Consume every statement result and check batch Close
before any dependent query. On failure, close the batch before returning to
the caller that owns rollback; this helper must not take over transaction
ownership. The current map iteration already has no fixed
setting order; there is no intervening business or authority operation to
preserve between its members.

Keep all eleven settings, transaction-local scope, schema validation and
`ErrConfiguration`/`ErrDatabase` handling. Recalculate the remaining context
budget on every configuration call. Preserve cancellation, complete result
draining and rollback, including a later setting failing after earlier ones
succeeded. Do not batch subsequent identity, history, catalog or fingerprint
queries into this change, and do not skip a later configuration call merely
because this transaction was configured earlier.

A batch still executes eleven SQL statements; this recommendation reduces
serial request/response waiting, not the statement count. Driver description
and protocol work can affect actual wire round trips. Remote verification
should check every effective GUC in read-only and restore transactions,
transaction-local reversion, changing deadlines, cancellation, partial
failure and subsequent pool usability. A synchronized connection can be
reused after rollback; cancellation or a protocol failure may require the
driver to discard that connection. Do not require a broken connection to
remain open. Measure protocol exchanges and latency before claiming a
numerical gain.

### Y16: Reuse the already authorized collection parent

`internal/library/collections_query.go:24` reads and authorizes the parent
against the dedicated route's requested kind, then discards the returned
CollectionInfo. Line 31 calls the generic `queryCollectionItems`, which
queries the kind again at line 49 and reads/authorizes the same parent at
line 56. The successful path thus has two redundant parent SELECTs before
the actual count and member page.

`beginCollectionRead` uses the same read-only repeatable-read subject snapshot
(`internal/library/collections.go:96-104`); no write or clock-based authority
check occurs between these parent reads. A private member-query core can
accept the first authorized CollectionInfo. The generic `/Items` adapter
must keep its parent-kind discovery, authorization and non-collection fallback.

Retain the dedicated route's first kind/feature/ACL check and its missing or
wrong-kind error, then preserve member ACLs, order, repeated playlist entries,
entry IDs, recursive behavior, totals, attachments and commit. This reuses a
value inside one transaction; it introduces no cross-request authorization
cache. Keep existing domain `Limit=0` defaults and the HTTP zero-limit adapter
unchanged rather than combining a count-only redesign with this finding.

Remote verification should compare playlist/box-set routes, wrong kinds,
restricted and userless/targeted application subjects, duplicate members,
empty/out-of-range pages, zero-limit behavior and generic folder fallback.
Count the two parent SELECTs separately from result-page attachments. R06's
batched collection attachments do not eliminate this dedicated-entry repetition.

### Y17: Avoid loading the complete existing playlist for duplicate preview

`internal/library/collections.go:674` calls `collectionMemberSet` while
previewing an addition. That helper selects every existing `item_id` and
builds a Go map (`collections.go:692-705`). The preview only needs
`ItemCount` and `ContainsDuplicates` (`internal/server/collections.go:666-671`).
A valid one-item proposal can therefore transfer the entire existing playlist,
whose normal admission limit is 10,000 entries.

Keep the current collection authorization, edit permission and
`resolveCollectionMembers` expansion first. Detect duplicates within the
resolved proposal locally, and use an `EXISTS` restricted by collection ID
and the proposed member IDs to detect overlap with existing entries. Preserve
`ItemCount=len(resolved)`, raw membership semantics and the same read snapshot;
do not add a new member ACL predicate to the overlap check. Existing membership
does not grant visibility or editing permission.

Keep error ordering and commit, including the empty-expansion case, rather
than returning early merely because an input duplicate is already known.
The minimum change applies only to preview. Similar BoxSet append use of
`collectionMemberSet` is an extension of this finding, not another ID.

The original request accepts at most 1,000 IDs, but legitimate expansion can
produce 10,000 members. A new overlap helper must not apply the smaller input
limit to resolved members. Deduplication may narrow only the query array;
the original resolved length remains the response count.
The existing `(item_id, collection_id)` index is an available access path;
the minimum proposal needs no new index. Actual planner choice and parameter
cost remain workload-dependent.

Remote verification should cover an empty playlist, duplicates within the
proposal, overlap only with existing entries, repeated playlist entries,
folder expansion, hidden existing members, locked/shared collections and
zero expanded members. Compare a small proposal into a 10,000-entry playlist
with a large expansion into a small playlist. The expected saving is returned
membership rows and the full existing-set map, not necessarily fewer SQL
statements or lower latency for every input shape. This differs from R06's
page attachments, N16's new-member/share batches and W02's position writes.

### Y18: Prepare DTO presentation options once per response

`sendItemQuery` calls `applyItemSwitches` for each returned item
(`internal/server/items.go:305-314`). That helper calls `r.URL.Query()`
three times per item: once through field exclusions and once for each boolean
switch (`items.go:758-766`, `internal/server/item_projection.go:124`). A
1,000-item result therefore executes at least 3,000 complete query parses
in this stage alone, including when the switches are absent. Go 1.27.1's
`net/url.URL.Query` directly calls `ParseQuery` on every call; this was checked
by reading the designated host's `src/net/url/url.go:1164-1166`.

Image enrichment reparses exclusions per row and Fields for an eligible
Primary image in a non-detail response, at most once per item
(`internal/server/images_dto.go:74,81`).
`internal/server/subtitles_dto.go:221` also re-extracts the same credentials
for every item solely to construct delivery URLs. These inputs are invariant
within the response.

Prepare a small response-local presentation value after existing request
authentication and projection normalization. Reuse the parsed field lists,
switches, image options and delivery-token string in the item loop and image
enrichment. The existing normalizer can replace `r.URL` and canonicalize case
aliases (`item_projection.go:21-98`), so a cache captured in earlier middleware
would be the wrong boundary. No cross-request cache or cached authorization
decision is needed.

Retain the actual authentication and input-validation stages, their error
precedence, protected identity fields, unknown-field compatibility and nested
MediaSources exclusions. Retain the exclusion pass after capabilities/images
are added; reusing parsed options does not make that second filtering stage
redundant. Delivery URLs still need each item's identity and original escaping.
Do not retain the token beyond the response's existing lifetime.

Keep caller-supplied `fields` and `detail` choices distinct from common query
options. Other callers, including native and client-session handlers, have
their own validation order and must not acquire an extra Emby normalizer,
ASCII whitelist or new early failure. Preserve their existing query-list and
case-insensitive field matching semantics. The existing formatter ignores
credential-extraction errors; retain its rendering result and leave actual
authentication to the original entry point. Preserve all credential-carrier
conflict/selection behavior rather than reading only `api_key`.
Defaults also matter: images/user data are enabled when unspecified, image
limit defaults to 32, and an empty image-type set means all types.
`ImageTypeLimit=0` must not become `EnableImages=false` in this parsing-only
change. Preserve capability checks before the image branch and defer any
later-stage parameter error to its original point.

Remote verification should compare absent/true/false switches, case aliases,
repeated field lists, conflicting aliases, nested stream/chapter/path removal,
image limits and subtitle delivery URLs. Benchmark validated small and large
queries at 1/100/1000 items, measuring parsing calls, allocations and CPU.
The saving is output-side parsing and allocation, not SQL or filesystem I/O.
Y01 remains independent: it removes unnecessary database user-data attachments,
whereas this repeated work also occurs when UserData is enabled.

### Y19: Respect exclusions before selecting capability-query work

`internal/server/item_capabilities.go:14-16` decides whether to compute
`CanDelete` and `CanDownload` using only `detail` and Fields. It ignores
ExcludeFields. A detail response, or an explicit request for both capabilities,
still enters `ItemCapabilitiesFor` when both fields will be removed by final
projection (`internal/server/images_dto.go:81`).

The unnecessary work is a separate capability transaction with actual-actor
authorization, a batched source/eligibility query and, when applicable, a root
binding query (`internal/library/item_capabilities.go:70-101`). These are
display facts, not operation admission; the
[capability contract](../api/item-capabilities.md) explicitly requires actual
download and deletion to perform their own current authorization and source
checks.

Combine requested-field and excluded-field precedence before selecting the
optional batch. When neither capability can appear in the final response,
skip that batch. If either capability remains requested, keep the existing
full batch and its real-actor semantics. Preserve the core catalog
authentication/ACL checks, downstream image processing, final exclusions,
and actual Download/Delete authorization. Do not substitute a selected
UserId for the actor, or use displayed capability values as permission grants.
This also covers one requested capability that is excluded while the other
was never requested; two explicit exclusion names are not required.

Remote verification should cover detail and list responses with neither,
one, or both capability fields excluded, case-insensitive names, empty lists,
targeted application keys, native sessions and current policy changes.
`EnableImages=false` alone must continue to allow capability projection, as
the existing integration test requires. Prove that both-excluded requests
omit the capability batch and that real protected operations still deny
unauthorized actors. Errors belonging solely to an omitted optional batch
will no longer affect that response; main authorization and query errors remain.

This is distinct from Y01's UserData switch propagation and Y18's parsing-only
reuse: it resolves Fields/detail versus ExcludeFields precedence for an
independent capability query. No extra index or generalized authorization
framework is needed.

## Existing unfinished recommendations

The [U-series review](backend-design-review-increment-20261010.md) remains
actionable. The [V-series implementation](backend-continuation-implementation-20261010.md)
explicitly treats it as separate scope. Later W/X implementations and the
current dirty code do not close these seven recommendations.

| ID | Priority | Existing work still applicable |
| --- | --- | --- |
| U05 | P2 | Preserve a database management-lock failure as unavailable instead of converting it to an authentication failure. |
| U01 | P2 | Implement entity count-only reads and omit unused source-count projections for Emby browsing. |
| U03 | P3 | Narrow the global management lock for ordinary device renaming. |
| U02 | P3 | Detect unchanged transaction-local notification references before encoding them again. |
| U04 | P3 | Skip the notification item/entity query when that reference category is absent. |
| U06 | P3 | Validate a dynamic lease without copying its complete metadata when the caller discards it. |
| U07 | P3 | Normalize freshly decoded task selections without an immediate second copy. |

Four already-described principles also have remaining current call sites;
they do not receive new finding IDs:

- R01: a successful ordinary-Emby playback authorization observation parses the same policy in identity
  revalidation, library-policy construction, and the final clock check
  (`internal/library/playback_media_authorization.go:285,296,108`). Reuse only
  parsed facts from that locked observation; retain each fresh time decision.
- V01: `internal/library/provider_subtitle_authorization.go:59-77` still does
  a separate existence SELECT when `lock=false`, immediately before a liveness
  query with the same credential/user/kind predicate. Successful ordinary-Emby
  paths through both read-only callers perform this pair twice; native-admin
  paths have a different check. Preserve lock=true and both time/ACL boundaries.
- R20: recovery calls `ReadGeneration` and then `MasterKeyPath` at
  `internal/recovery/transition.go:768,789`, repeating verified-history work.
  A shared verified result must retain the same ownership and integrity checks.
- Q06: the already-known deadline/Err publication gap also affects
  `internal/backuppg/snapshot.go:261-263`. The local recovery call site and
  bounded correction are detailed below rather than counted as a new ID.

T09's page-level vault-key reuse remains deferred because it changes repeated
secret-path observations. Linear backup-registry accounting, notification
JSONB-size aggregation, and a broader Boolean-EXISTS rewrite of folder counts
remain profiling or design leads, not accepted new recommendations.

### Q06 follow-through: Do not report setup success after an expired deadline

`internal/backuppg/snapshot.go:261-263` returns `ctx.Err()` when
`time.Until(deadline) <= 0`. A deadline timestamp passing does not itself
guarantee that the cancellation callback has already published a non-nil Err.
The helper can therefore return nil without executing any configuration
statement, and its caller can enter the next phase believing setup succeeded.

The [Q06 implementation record](backend-followup-implementation-20261009.md)
already documents this deadline gap, and
`internal/literaldial/dial.go:144-152` implements the expected fallback.
`TestDialContextRejectsUnpublishedExpiredDeadline` at
`internal/literaldial/dial_test.go:348` is the existing controlled-context
precedent. The newly located recovery call site is actionable P3 follow-through,
not a newly discovered Go behavior or another independent finding ID.

Read-only inspection of the designated host's Go 1.27.1 source confirms the
distinction: future deadlines use a timer callback in
`src/context/context.go:653-655`, while `cancelCtx.Err` reads the stored error
at lines 464-471 rather than comparing wall time. This source inspection is
not a runtime reproduction, and no erroneous database commit is established.

In the already-expired branch, return a non-nil `ctx.Err()` unchanged when
present, otherwise return `context.DeadlineExceeded`. Preserve schema-error
precedence and the normal budget calculation. Do not change error mapping for
cancelled live-deadline/no-deadline paths as part of this narrow correction.
Batching the statements in Y15 does not correct a false-success return before
them.

Remote verification should use a controlled context whose deadline has passed
while cancellation remains pending, assert a non-nil deadline error and zero
configuration SQL, and retain already-cancelled, live-deadline and invalid-schema
controls. Creating a standard context with an already-past deadline cancels it
synchronously and does not by itself reproduce the pending-callback interval.
Do not broaden the claim to a proven authentication bypass or bad restore
commit; later context checks can still reject the operation.

## Linux boundary and necessary defenses

No additional Windows server implementation was found to delete. The Goby
media-server and native-launcher entries select Linux. Release compilation
fixes GOOS to Linux, native
helper CMake rejects non-Linux builds, and OCI build/runtime paths are Linux.
The earlier Windows server cleanup is present in the actual source.

`internal/commanddomain/launcher_other.go` means `linux && !amd64`; it rejects
an unsupported Linux architecture. `runtime.GOOS` in system status reports a
fact. Remaining test skips are maintenance leftovers already recorded by
earlier reviews, not a separate server implementation or runtime optimization.

The separate `goby-notification-receiver` protocol utility contains portable
Go code but no Windows-specific implementation. Portability of a helper is
not another supported media-server delivery.

Retain Windows development-host tooling, Windows playback-client metadata,
invalid-path test inputs, third-party upstream code, old archive interpretation,
and readable historical `unsupported_platform` values. In particular, old
drive-letter restrictions in `internal/database/extra_visibility.go` are
versioned archive rules, not current Linux path rejection.

No evidence supports deleting a complete authorization layer. Authentication,
transaction-local authorization, post-wait time checks, application-key/target
separation, collection membership, and delivery-time controller/receiver
checks protect different observations. Root-anchored opening, source identity,
actual process/reader retirement, recovery publication proofs, and notification
transaction/savepoint fences likewise retain independent correctness roles.
The accepted simplifications remove repeated work within one proven scope.

The removed theme stored-row/occupant guards were also examined. The current
group path retains row locking, active-population comparison and actual-source
witnesses; no concrete conflicting production mutation was established in
this review. That deletion alone is not accepted as another regression.

## Review-loop ledger

A completed round counts as empty only when all four tracks and the
coordinating review have no new actionable recommendation or reopened current
regression. Refining a finding or rejecting a candidate does not add an ID.

| Round | Focus | Accepted additions | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Parallel authority, defensive-work, query/playback and Linux inspection; historical deduplication. | Y01-Y05 | 0 |
| 2 | Lock/error-order counterexamples, output consumers, account deletion, dirty diff versus committed behavior, and retained historical evidence. | Y07; reopened regression Y06 | 0 |
| 3 | Cross-track review of the accepted boundaries; native user-summary consumers, recovery tool dependencies, and notification witness/mutation queries. | Y08-Y11 | 0 |
| 4 | Independent report challenge and full recovery/activation trace; no-upgrade resource checks, immutable baseline loads, and exact empty-folder image costs. | Y13-Y14; reopened optimization Y12 | 0 |
| 5 | Reverse consumer/commit audit, current-schema/finalizer and nested-copy counterexamples, precise scan evidence, and transaction configuration. | Y15 | 0 |
| 6 | Full authority/resource audit, pgx batch error and rollback behavior, Linux entry points, current Git diff and independent report/evidence review. | None | 1 |
| 7 | Revocation/self-deletion and failure-path review; dedicated collection dispatch, deadline return semantics, and required decoder admission timing. | Y16; deadline case classified as existing Q06 follow-through during historical reconciliation | 0 |
| 8 | Independent collection/deadline counterexamples, historical Q06 deduplication, remaining API consumers and duplicate-preview population. | Y17 | 0 |
| 9 | Cross-track preview equivalence, input/expanded limits, query and parameter costs, empty/error/commit paths, historical deduplication and Linux utility boundaries. | None | 1 |
| 10 | Reverse success/retirement audit, authentication and transaction boundaries, delivery packaging, and per-row response parsing. | Y18 | 0 |
| 11 | Independent DTO/authentication and late-filter review; caller defaults and credential error behavior; requested-versus-excluded capability fields. | Y19 | 0 |
| 12 | Independent capability-display versus real-operation authorization review, effective requested fields, query/error boundaries, Linux file operations and historical deduplication. | None | 1 |
| 13 | Reverse authorization/commit/retirement audit, request-local versus live-state boundaries, current WIP and source/history correspondence across all four tracks. | None | 2 |
| 14 | Final combined authority, defensive-work, performance and Linux review; independent history audit and confirmation of all retained correctness boundaries. | None | 3 - stop |

All four tracks, the independent history review and the coordinating review
completed rounds 12-14 without a new independent actionable recommendation.
The requested exit condition is satisfied after fourteen rounds. This is
convergence for the inspected scope, not proof that no future issue can exist.

## Verification and resource handling

The capacity policy was read before read-only SSH inspection of `test-env`.
At that observation, the persistent root had about 20 GiB available while
`/tmp` had about 762 MiB. The ordinary shared cache paths were
`/root/.cache/go-build` and `/root/go/pkg/mod`. The default `/usr/bin/go`
reported 1.26.7, while the repository declares 1.27.1. No verification relied
on that default executable and no toolchain setting was changed.

At review completion, no new remote environment, source copy, database, compiler
cache, scratch, fixture, or worker had been created. No shared-cache cleanup or
removal of another task's retained artifacts was performed. This report was the
only new repository file produced by the review; implementation had not yet
started. Subsequent changes and verification belong to the linked implementation
record.
