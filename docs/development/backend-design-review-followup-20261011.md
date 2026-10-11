# Backend design follow-up review, 2026-10-11

Completed fourteen review rounds. Rounds 12-14 produced no new recommendation
or actionable call-site extension, satisfying the requested stop condition.
The report contains twenty-one new recommendations: three P2 and eighteen P3,
plus additional call sites for six existing principles.
The review has modified only this report, preserving pre-existing working-tree changes.

This is the historical review-only record. Its selected findings now have a
[follow-up implementation](backend-followup-implementation-20261011.md);
that record tracks the implementation scope and pending verification separately.

The main opportunities are safer known-expiry transaction completion, query
composition, and narrower response projections.
The review does not justify removing an authorization layer or introducing a
cross-request authorization cache. Address Z21's owner-availability boundary
and the two P2 query paths, then apply the smaller transaction-local and
immutable-data reductions. Necessary
post-wait authorization, filesystem identity, and real retirement boundaries
remain. No Windows server implementation remains to remove.

## Scope and method

The baseline is the current working tree at
`05a955c56f9ea886f2b579f295d61acc72e77249`, including its pre-existing scan,
media, HLS, release-tooling, and documentation changes. The review examines
permission complexity, unnecessary defensive work, architectural performance,
and the Linux-only server boundary.

Parallel reviewers inspect authority, defensive/resource behavior, performance,
and platform support. Subsequent rounds rotate reviewers, trace consumers and
failure paths, and challenge proposed simplifications. The coordinating review
checks current source and historical findings. A round is empty only after all
tracks and coordination finish without a new actionable recommendation or an
additional actionable call site. Refining an existing finding or rejecting a
candidate does not add an ID; expanding its implementation scope still resets
the empty-round count.

The [earlier review](backend-design-review-20261011.md) predates the
[latest implementation](backend-review-implementation-20261011.md). That
implementation covers Y01-Y19, U01-U07, and the selected R01/V01/R20/Q06
follow-through. Those completed changes are not counted again here.

Evidence is static source, SQL, call-path, schema, and existing-test inspection.
Complexity, query counts, and payload estimates below are not measured latency
or production throughput. No local or remote tests, builds, benchmarks, or
validation suites have been run for this review.

## Recommendations

| ID | Priority | Area | Recommendation |
| --- | --- | --- | --- |
| Z21 | P2 | Owned transaction completion | Roll back a known-expired protected transaction before calling driver Commit. |
| Z01 | P2 | Client session projection | Read only the three capability fields used by session summaries. |
| Z03 | P2 | Similar movies | Find candidates through the seed's entity associations before expanding candidate credits. |
| Z02 | P3 | Defensive graph traversal | Validate the notification parent graph once and reuse the result. |
| Z04 | P3 | Administrator session history | Filter status earlier and materialize narrow page/count keys before fetching details. |
| Z05 | P3 | Creation authority locks | Keep management serialization but use shared actor locks when creating a new account. |
| Z06 | P3 | Task query projection | Omit unused execution payload and nested state from display or target queries. |
| Z07 | P3 | Task state persistence | Reuse updated Run values where later transaction checks preserve failure handling. |
| Z08 | P3 | Viewing statistics | Filter user-state rows whose contribution is necessarily zero. |
| Z09 | P3 | System-event admission | Return the occurrence receipt instead of querying it again. |
| Z10 | P3 | Deferred analysis | Skip the unused full Run read when a system event must wait. |
| Z11 | P3 | Scan reconciliation | Reuse validated ancestor suffixes within the deletion transaction. |
| Z12 | P3 | Managed-user credentials | Combine adjacent Emby observations and read self-deletion facts only for self-deletion. |
| Z13 | P3 | Native provider queries | Remove the first of two complete metadata reads in three handlers. |
| Z14 | P3 | Provider provenance | Select source-field names without transferring their unused values. |
| Z15 | P3 | Subtitle provenance | Return a newly inserted provider binding, retaining conflict lookup. |
| Z16 | P3 | Music source refresh | Combine source comparison and changed-only UPDATE. |
| Z17 | P3 | Provider metadata targets | Narrow image-selection and provenance targets without dropping source validation. |
| Z18 | P3 | Metadata decoding | Remove the second copy of a freshly decoded RawMessage. |
| Z19 | P3 | User settings encoding | Reuse canonical JSON already produced while validating the same document. |
| Z20 | P3 | Target-account reads | Select only required account facts at five callers that do not consume a complete User. |

### Z21: Preserve the owner when expiration is known before Commit

`internal/library/ownership.go:299-309` calls the driver's Commit with the
protected transaction context after flushing pending events. A task transaction
with no pending journal/catalog event can perform no SQL during that flush.
If its 20-second context is already done, Commit is nevertheless attempted.

The pinned pgx v5.11.0 implementation has an important distinction:
`pgconn/pgconn.go:1219-1227,1353-1362` returns an already-done-context error
before submitting a query without closing the connection. But
`tx.go:201-207` closes the connection after a Commit error when its transaction
status is not idle. Goby's ownershipErrorLocked at
`internal/library/ownership.go:360-364` then marks the reserved owner lost and
cancels the Store. A known budget exhaustion can therefore invalidate the
catalog owner even though COMMIT was never sent and independent rollback was
still possible. This also occurs in the existing code, independent of any
proposed SELECT reduction.

After a successful flush and immediately before invoking driver Commit, check
the protected tx.ctx.Err(). If it is already non-nil, use the existing
Rollback path, which creates its own bounded five-second cleanup context.
Return the context error and, only after confirmed successful rollback, the
existing pgx.ErrTxCommitRollback marker. If rollback fails, preserve its real
classified error without a success marker. Use tx.ctx, not
the deliberately ignored method argument or the worker/request context:
ordinary caller cancellation must retain the existing protected-write contract.
Let Rollback own finalization and the single ownership-mutex release; do not
fall through into Commit's later cleanup or notify committed changes.

Keep actual attempted-Commit errors and unknown outcomes on the original
driver/owner-loss path. Do not retry the transaction, extend its write budget,
or claim that rollback failure preserves a healthy owner. A cancellation can
still race the final check and driver call; this proposal handles expiration
already reported at that boundary, not every possible timeout interleaving.
It does not automatically reinstate the final-read optimizations withdrawn in
round 10.

Round 11 adds the other direct reserved-owner transaction entry:
`internal/library/scan_reconciliation_staging_session.go:57,112` begins and
commits a raw transaction without ownedTx.Commit. Check its writeCtx.Err()
after the existing caller-cancellation check and before raw.Commit. A known
failure returns with finished and committed still false, so its existing
deferred independent rollback executes, with physical high-water auditing under
its existing guards. Keep DDL
uncertainty, cleanup=true failure discard, capacity-driven discard, and all
current error classification. Do not run afterCommit or publish staging
pass/seal bookkeeping. An actually attempted failed Commit still marks the
session for discard. Source inspection found only these two Begin/Commit
entry points on this reserved catalog connection.

Rollback-result consumers also require an explicit contract.
`internal/library/primary_sidecar_read.go:162-169` treats ErrTxClosed after
pgx.ErrTxCommitRollback as an already confirmed rollback. Image/embedded-image/
subtitle scans use that cleanup path. Returning only the context error after
successful cleanup would falsely produce an unknown-rollback error; returning
the success marker after failed cleanup would conceal that uncertainty.
Apply the same conditional marker and error preservation to the existing
flush-failure branch at ownership.go:303-305, which currently discards its
rollback result. Keep the original flush error alongside the classified
cleanup failure; tasks.Manager.observeFailure at manager.go:229 reacts to
ErrUnavailable and should not have to rediscover lost ownership later. This
does not promise a uniform HTTP status change across differently prioritized
error mappers.

Remote verification should use the pinned driver and a real owned connection:
expire only the protected context after the last successful statement, retain
a live caller context, and prove database rollback, no committed catalog
notification, the same retained
owner connection, and a subsequent successful owned transaction. Also cover
caller cancellation with a healthy protected context, pending-event flush,
rollback failure, double-finalization prevention, and an actual Commit/network
failure without converting an unknown result into success. This finding is
based on code inspection; these cases have not been executed in this review.
Include staging noncleanup/cleanup/DDL and physical-budget failure branches,
known-versus-unknown sidecar rollback classification, and immediate task-manager
owner-loss observation when cleanup itself fails.
Confirmed database rollback is not evidence that published files should be
removed or source workers have exited; preserve their existing independent
retention and retirement contracts.

### Z01: Use a bounded capability summary for session display and control

`internal/identity/client_sessions.go:259,268,276` selects complete capability
JSON for ordinary and application sessions. Lines 299-309 compact and parse
every returned object. `ParseClientCapabilities` at lines 81-115 walks and
sanitizes the object, then marshals and decodes it again. A list can contain
256 sessions. The complete input is limited to 64 KiB per capability object;
this is not a separate 64 KiB allowance for DeviceProfile.

The four production callers are `internal/server/client_sessions.go:139`,
`session_subscriptions.go:104`, and `remote_commands.go:484,640`. Their consumers
use only PlayableMediaTypes, SupportedCommands, and SupportsMediaControl.
`session_subscriptions.go:40` defaults an active Sessions subscription to a
one-second refresh. DeviceProfile, SupportsSync, IconURL, and AppID are not
used for these results or their control decisions. PlaybackInfo uses its
request's profile, as documented in `client-sessions.md`.

Select and validate the three consumed fields in an explicit summary path.
Retain full write validation and the complete read contract wherever it is
still requested. One named summary operation or a private projection choice
is sufficient; a generic projection framework is unnecessary. Keep identity,
visibility, presence filtering, expiry, final authorization, and array/type/
string limits. Preserve omitted/null field behavior when constructing the
summary; adding artificial null keys must not push a valid near-limit object
past the existing byte bound. A malformed omitted field will no longer independently fail
the summary; this is an intentional error-boundary change, not exact error
equivalence. Invalid consumed fields must still fail.

Remote implementation checks should cover full and summary reads, all four
callers, ordinary/application principals, large profiles, malformed included
and omitted fields, and revocation/expiry waits. Compare transferred bytes and
allocations before claiming a latency benefit.

### Z02: Validate each notification parent edge once

`internal/server/library_notifier.go:231-249` starts a fresh ancestor walk for
every change. Lines 277-293 repeat the walk for Removed changes. The batch's
parent graph is already immutable. A legitimate approximately 100-level tree
deleted before a completed scan can produce one batch containing its removed
folders: `internal/library/scan_reconciliation_commit.go:193-200,532-546`
retains and records their old parent facts. Such a batch fits the current
depth, 512-change, and 64 KiB bounds. This establishes repeated O(n * depth)
work in the single notification worker, not a measured notification delay.

Validate the unique graph once using visitation/completion marks keyed by
(ItemID, LibraryID). Preserve duplicate conflicts, cross-library kind/role
consistency, PreviousParent validation, missing-parent termination, non-folder
rejection, and cycle-triggered resynchronization. Construct output in original
Changes order, not map iteration order.

After full graph validation, Removed suppression needs only the same-library
direct parent and its Removed kind. The existing boolean becomes true at that
first matching parent and is never reset by a longer walk. Do not search past
a non-Removed direct parent; a later non-Removed ancestor does not undo
suppression already established by a Removed direct parent. Keep encoded size accounting,
Hub validation, queue budgets, generation fencing, and publication order.
The repeated input-size validation can be consolidated within this private
validated path, but is not a separate finding.

Remote checks should extend the existing ordered-field, scope, cycle, and
ancestor tests with long shared chains, duplicates, cross-library names, and
non-Removed intermediates. Count graph visits and allocations independently
of end-to-end latency.

### Z03: Reverse the candidate search for Movie Similar

`internal/library/similar.go:177-206` materializes the complete authorized
candidate scope and its qualifying credits before lines 225-237 intersect
with the seed's entities and score candidates. A small Limit does not bound
this work. With many unrelated movies and a sparsely associated seed, most of
that expanded population cannot qualify.

For the Movie branch, first read the seed's complete qualifying entities and
find candidate IDs using the existing entity-to-item index
(`internal/database/migrations/0004_catalog_entities.sql:37`). Then apply the
original candidate predicates and complete scoring. Movie has no Audio album
bonus, so a candidate sharing no seed entity necessarily scores zero and
cannot pass the threshold of two at `similar.go:109`.

Keep the seed independent of candidate-only Ids, parent, search, state, and
exclusion filters. Preserve the actual Artist credit branch as well as Genre,
Tag, and Studio; the current nonmusic SQL includes Artist. Retain distinct
(entity_id, credit) scoring, all filters and ACLs, the repeatable-read snapshot,
random ties or explicit sort, Movie Limit=0 behavior, offsets, and returned-page
count. Do not apply this recommendation to InstantMix's zero-score fallback
or replace the music scoring model.

This differs from L02's InstantMix aggregate rewrite. Compare EXPLAIN plans,
buffers, intermediate rows, and latency on sparse and dense overlap catalogs;
an index-driven plan is not guaranteed to win for a dense seed.

### Z04: Keep administrator session count/page intermediates narrow

`internal/identity/managed_sessions.go:175-195` materializes wide session rows,
including user and client text, then filters status into another materialized
relation before applying LIMIT. The native API defaults to 50 rows and permits
200, while authentication history can continue accumulating. Even an active
filter first materializes matching revoked and expired rows.

Push the selected status predicate toward the base relation and retain only
IDs, sorting keys, and required classification in count/page intermediates.
Fetch user and client details after page selection, within the same statement.
Exact count and literal substring search may still scan a matching population;
this is a row-width and unnecessary-state optimization, not O(page) counting.

Preserve one captured observed_at and revoked > disabled > expired > active
precedence. Keep sessions JOIN users before counting so userless application
keys cannot fall through NULL comparisons into active. Reuse the selected
status rather than reclassifying it during detail projection. Complete the
page's detail joins before the anchor/count LEFT JOIN; a later inner join must
not erase the total for an empty or out-of-range page. Keep the independent
final actor/clock check before commit.

Remote checks should cover every status, current account changes, application
keys, empty pages, search, stable ordering, and concurrent activity. Measure
work-table width, buffers, and sorting work with a large retained history.

### Z05: Narrow actor locks for ordinary account creation

`internal/identity/managed_users.go:116` reuses the account-mutation admission
helper. That helper takes the global management lock at line 416, reads the
actor's complete ManagedUser under FOR UPDATE at line 421, and takes the
actor credential FOR UPDATE at line 475. The creation body only inserts a
new account and an activity record. It does not modify its actor or reduce
the administrator population. An insert/audit wait unnecessarily retains
exclusive actor locks that conflict with ordinary authorization readers.

Use creation-specific admission with the same global management lock followed
by account and credential FOR SHARE, reading only required authority. Keep
current role, login policy, peer, device, revocation, expiry, final recheck,
name uniqueness, activity, and rollback behavior. Reject application keys as
today. Reusing a general administrator helper must preserve the current
ErrUnauthorized result for a demoted Emby actor, rather than changing it to
ErrClientSessionForbidden. Do not weaken the shared update/delete helper or
the separate copy-user source lock.

This scoped recommendation retains management serialization. It does not
establish that this global lock is necessary for every creation; removing it
would require a separate audit of the complete mutation protocol. No claim is
made that the selected change stops waiting for unrelated management operations.

Remote checks should hold ordinary actor SHARE locks during creation and
exercise account deletion/demotion, expired actors, username conflicts,
bootstrap, copying, and activity failure without changing final-admin rules.

### Z06: Match task projections to display and target consumers

`internal/tasks/store.go:102-106,139-169` serializes complete current/last Runs,
individual Runs, and history pages. `internal/server/task_dtos.go:32-40` and
`scheduled_tasks.go:167-189` do not consume AnalysisInput or
AnalysisConfigFingerprint. A selection permits 64 library IDs and 256 item
IDs. With ordinary 32-character identifiers, a full selection alone is about
11 KiB; repeated across a 200-run page this is roughly 2.2 MiB before other
fields. These are estimates from bounds and encoding, not measured traffic.

Use explicit columns for HTTP display projections. Constructing to_jsonb(r)
and then subtracting fields still performs the initial full conversion.
Keep complete reads for execution, admission/coalescing, restart recovery,
and publication fencing; `publication_fence.go:62-95` consumes the hidden
authority and input fields. Use a distinct summary type or an equally explicit
boundary so display results cannot enter those execution paths. Missing input
is legitimate for some system tasks, so a later fence cannot be assumed to
detect every accidental use of a summary. Keep errors, counters, nullable UTC times, ordering,
definition/current/last/trigger snapshot consistency, existence, and empty-page
totals.

An invalid omitted execution payload will no longer independently break a
display read. Record this error-boundary change and retain full validation in
execution and recovery. U07 removed an additional copy after decoding; this
recommendation removes unnecessary upstream serialization and decoding.

Remote checks should compare native and compatibility DTOs, maximum selections,
no-run and empty pages, malformed included/omitted fields, and independent
execution/fence rejection. Measure bytes and allocations, not just query count.

Round 7 adds eight preliminary task-target lookups that consume no nested Run
or trigger projection at all. GetByKey is used by admin_media_analysis.go:163
and libraries.go:296 for ID alone, and audio_waveforms.go:101,
background_previews.go:156, and subtitle_timelines.go:102 for ID and Enabled.
These file names are under internal/server. Preserve the three Enabled checks
before their separate queue writes.

The compatibility mutations at internal/server/scheduled_tasks.go:380,425,440
also load complete definitions through line 203. Start/Stop only need the ID
and key fields for supported-task admission; trigger replacement additionally
needs revision and timezone, including empty-timezone-to-UTC behavior. A small
base-definition target lookup can omit the three nested projection subqueries
and their JSON without removing a SQL statement. Keep current request/error
ordering and all authoritative reads in the later Start/Stop/ReplaceTriggers
transactions. Full GET/display paths still need their summaries; the target
is never an execution Run. Omitted nested decoding errors no longer block a
target lookup, while the mutation retains its required live validation.
Keep both Key and stored EmbyKey in compatibility targets, with the existing
CompatibilityKey mapping check. A later StopByDefinition does not replace that
specific front-end support check.
For the three queue-first handlers, omitting nested decoding can allow a
durable queue request before Start rejects a corrupt current Run. Their queue
contracts intentionally retain requests independently of immediate admission;
record this error/side-effect boundary instead of combining or reordering the
queue and Start transactions to conceal it.

### Z07: Return persisted task state directly from UPDATE

`internal/tasks/runs.go:455-468` updates aggregate fields and immediately issues
a separate readRun. Both production callers hold the parent lock in the same
owned transaction. No selected schema trigger modifies task_runs after UPDATE.
For terminal transitions, UPDATE ... RETURNING can return the same complete
Run while the subsequent queue/activity operations retain protected-context
error handling before commit. Preserve the existing readback for nonterminal
progress: it can be the final SQL before an owned transaction commits.

Round 3 proposed the same optimization for BeginRun at
`internal/tasks/runs.go:345-349`. Round 10 withdrew that unconditional extension:
the readback is also the final SQL before commit. Removing a query that fails
before submission with an already-expired context can replace a safe rollback
with driver Commit failure, connection closure, and catalog-owner loss. Keep
the existing BeginRun readback, start clock, runtime-limit validation, and
deadline calculation. Reconsider these final-read paths only with an explicit
completion strategy and remote failure-path evidence.

Round 4 completed the adjacent Run-query inventory and extends this scope:

- `runs.go:126-133` and `scheduler.go:478-485`: return the new Run after
  updating total_children. Preserve the intervening admission activity and
  call refreshLockedRun afterward, including fresh child totals and the
  zero-child terminal transition.
- `runs.go:266-313`: retain the old Run for first-stop activity and shutdown
  signal decisions; pass the complete updated Run to refreshLockedRun after
  child changes. The old Y04 warning against reusing the pre-update Run still
  applies. Coordinate this with the R10 no-op stop extension below.
- `runs.go:82-91`: read the active ID and its raw complete Run in the same
  locking query. Keep the current actor recheck before decoding the raw Run;
  do not move malformed-payload handling ahead of authorization.
- `scheduler.go:430-442`: for a non-system-event analysis overlap, the active
  row lock can also return the Run subsequently consumed for input comparison.
  Keep an ID-only read for branches that do not need the payload, including
  the known-deferred system-event branch in Z10.

These are transaction-local reuse opportunities. Request-receipt matching
followed by a later current Run read, external coordinator reads, and
publication-time authority reads retain their distinct observations.

Keep the zero-update read behavior, database-generated times, full private
execution authority, decoding/UTC normalization, and all error propagation.
Account for decodeRow translating pgx.ErrNoRows to ErrNotFound when preserving
the aggregate path's no-row fallback. Verify private authority fields as well
as public JSON; those fields are deliberately excluded from marshaled output.
Only an actual terminal update may settle preview, waveform, and subtitle
queues or record completion activity. RETURNING is not a committed result;
the surrounding transaction still owns commit and rollback.

R10 already avoids unchanged updates, and Y04 reuses the parent before fresh
child aggregation. This finding concerns the read after a real update.
`execution_refresh_integration_test.go:38-58,252-299` already observes query
counts and readback/queue/activity failures. Adapt those checks to RETURNING
decoding and retained rollback behavior; do not discard the failure cases.

### Z08: Exclude zero-contribution state before viewing-statistics joins

`internal/library/viewing_statistics.go:33-43` leaves zero-contribution user
state eligible for item-dependent joins and predicates. Favorites and other
preferences create rows with played=false and playback_position_ticks=0
(`userdata.go:154`, `migrations/0006_playback_state.sql:4-7`). Such rows always
contribute zero to the current formula, irrespective of runtime or visibility.

Add the state-side predicate state.played OR state.playback_position_ticks > 0
so the planner can discard these rows before item-dependent work. Predicate
text order does not guarantee physical evaluation order. Retain initial subject authorization even
when no rows contribute, all current visibility predicates for contributing
rows, duration validation, clamping, numeric accumulation, rounding, zero
result semantics, and the read transaction's commit.

This removes unnecessary eligible join inputs for preference-heavy accounts.
It does not promise a new index plan, a lower SQL count, or constant-time
aggregation. Remote checks should include favorite-only rows, unmarking played
items, zero/nonzero progress, invalid durations, large totals, hidden items,
and disabled or revoked principals, then compare actual query plans and buffers.

### Z09: Return the system-event occurrence receipt

`internal/tasks/system_events.go:130-137` calls admitScheduledOccurrence and
immediately queries the receipt it just found or inserted. The helper's initial
existence query is at `scheduler.go:410-420`; its successful insertion is at
lines 493-499. Return a private receipt containing nullable run_id and
disposition from the initial lookup or successful insertion. The system-event
caller can then omit one separately awaited SELECT.

Preserve existing-receipt idempotency, definition/trigger/Run lock order,
analysis deferral, and the common transaction for admission, event receipt,
and cursor advancement. task_occurrences permits missed receipts with NULL
run_id; startup/time consumers currently accept their existence. The
system-event consumer must still reject NULL/missed receipts and roll back,
as its own receipt table requires an admitted/overlap Run. Do not coalesce
NULL to an invented empty or successful result. Returned receipt data is not
a commit acknowledgment.

### Z10: Do not decode an analysis Run that must be deferred

`internal/tasks/scheduler.go:441-452` reads a complete active analysis Run and
then defers unconditionally when source is system_event. Move that source
decision ahead of readRun for this branch. Preserve prepareAnalysisAdmission,
the active-ID FOR UPDATE query, all current preparation/lock errors, and
unchanged deferral without consuming an event or writing an occurrence.
Other sources still need the full Run for input/fingerprint comparison.

This removes one unused read and its decoding on these retries. A malformed
unused Run payload no longer independently replaces deferral with a decode
error. Execution and recovery continue using complete validated reads.
Moving the decision before prepareAnalysisAdmission is a different design
that changes configuration/library error timing and is not selected here.

### Z11: Reuse valid deletion-ancestor suffixes

`internal/library/scan_reconciliation_commit.go:952-979` walks the entire
ancestor chain independently for each deletion member, including non-Audio
items. The caller at line 465 already holds the complete locked relation.
A valid 100-level directory tree containing 1,000 leaf media items creates
substantial shared paths within the current 32 MiB, item, and depth budgets.
This repeated CPU/allocation work occurs while the catalog owner is held.

Memoize only successfully validated suffixes, including their length and
nearest surviving ordinary MusicAlbum. Use the existing globally unique item
ID, not the notification layer's composite key. Keep each origin's depth
check, missing-node/cycle errors, complete validation above a discovered album,
and non-Audio validation. Do not inspect unrelated nodes in known or use one
global visited set to skip origin-specific limits. Only Audio contributes an
album, and results remain sorted and unique. Charge new memo storage against
the existing byte budget; fall back to the original traversal if the optional
memo cannot fit rather than introducing a new capacity rejection. This is an in-memory optimization, not fewer SQL
queries, and differs from S06's batched SQL album lookup and Z02's notification
graph.

Round 5 extends the reuse opportunity to the scan's collection invalidation
at `scan_reconciliation_commit.go:543`. The query in
`internal/library/collections_notifications.go:36-42` recursively rereads
parent edges from the same locked, validated relation. A private scan-only
entry can accept the exact closure reachable from visible Removed item IDs.
Follow known.parentID, not CatalogChange.ParentID, which can be cleared for
auxiliary resources; do not supply the whole known map or add owner edges.
Keep the collection membership query at its original point, all resync/empty/
1024-fact/4097-result behavior, private notification scopes, and byte accounting.
Other deletion callers retain the ordinary SQL path. This removes repeated
database traversal, not a SQL statement; measure the larger ancestor argument
and memory costs as well as query work before selecting the implementation.
The closure includes its seed IDs and all same-library ancestors, including
ancestors not independently visible. The 1024 check still applies to original
facts, not closure length. Scan deletion excludes CollectionFolder members,
so do not populate the existing libraryIDs branch with the current library.
Budget the extra map, array, and parameter scratch before allocation; use the
existing query as a fallback when this optional reuse cannot fit its budget.
Retain the 4097 overflow sentinel and original journal failure/rollback behavior.

### Z12: Narrow managed-user credential reads

After all actor/target/credential locks, the Emby branch in
`internal/identity/managed_users.go:486-503` separately reads credential
liveness and device/time. Return the complete identity/kind/revocation/expiry
decision, device_id, and current database clock in one observation. Preserve
the preceding account-role check, boolean denial before policy/device/peer
evaluation, missing-row/error classification, and final mutation authorization.

`managed_users.go:338` reads actor expiry and device for every deletion, but
only the self-deletion branch at lines 382-390 consumes them. Keep this read
before self-deletion, with its final fresh clock/policy check after audit;
omit it when deleting another account, retaining the independent final check
at line 393. This deliberately narrows Y07's earlier retained-read boundary.

The extra DeleteManagedUser check at line 319 is not selected for removal in
the minimum recommendation. Dropping it can change an immediately expired
actor plus bad-revision/last-admin case from an authorization error to a
business error. Final authorization would still prevent a commit, but that
separate error-order choice is not required for the two reductions above.

### Z13: Eliminate three duplicate native provider reads

`internal/server/providers.go:139,305,329` invokes providerItem and then
OnlineMetadataQuery at lines 143,311,337. The first result contributes only
ItemID. providerItem performs GetItemMetadata, while OnlineMetadataQuery
(`internal/library/provider_metadata.go:354-375`) already repeats administrator
authorization, readMetadataRecord, metadataDetail, and transaction completion.

Keep request parsing, adminMetadataID, and the trusted actor; call the latter
domain query directly. This omits one complete metadata-read transaction per
metadata search, native subtitle search, or native subtitle download. Retain
the latter query's complete validation and Season/Episode ancestor lookup,
timeouts, and all later source/publication checks. Removed reads no longer
have their own independent failure opportunity; retain appropriate HTTP error
mapping without claiming identical fault timing.

Do not generalize this to apply/images/preview, which consume Type, or refresh,
which also consumes effective ProviderIDs. Provenance alone does not repeat
metadataDetail validation. The post-network image-preview metadata check is
a new authorization observation and remains.

### Z14: Project provider field names instead of values

`internal/library/provider_metadata.go:78-102` transfers complete fields JSON
and decodes every value solely to enumerate top-level keys for ProviderSource.
Select a key array in SQL and retain the existing Go name mapping and sort.
Preserve each source row when fields is an empty object; a correlated array
works where a dropping cross join would not. Keep non-null empty Fields and
duplicate output names after the ProviderIDs-to-ProviderIds mapping.

Retain the preceding target read and its authorization/source checks; Z17's
additional nonlocking projection can supply the same required checks.
This saves unused value transfer and decoding; it is not an N+1 correction or
proof of a source-size bound. The initial provider input limit does not prove
an identical hard bound after later source merges. If omitted deep/corrupt
values would fail Go decoding, a keys-only projection no longer observes that
independent error; retain database object-shape and selected-field semantics.

### Z15: Return a newly inserted subtitle provider binding

`internal/library/provider_subtitles.go:978-988` inserts provider provenance
with ON CONFLICT DO NOTHING and then always reads provider/provider_id.
Use INSERT ... RETURNING for a newly inserted binding. Only the no-row conflict
case needs the existing SELECT, followed by the same exact provider/ID check.
Do not turn conflict handling into an UPDATE or invent success for mismatches.

Preserve before/after catalog snapshots, task and actor checks, final file and
source observations, retention on uncertain commit, and transaction rollback.
The first-registration path saves one SELECT; existing-provenance paths retain
their required lookup. Test both branches and failure after insertion.

### Z16: Compare and update accepted music source in one statement

`internal/library/metadata_music_scan.go:270-280` compares music_source and
then updates it when changed. The item and metadata-state rows have already
been locked at lines 248-252. Use a conditional UPDATE with the same IS DISTINCT
FROM predicate and inspect RowsAffected. A changed source needs one statement
instead of two; an unchanged source still avoids downstream synchronization.

Keep the already acquired locks, ready/incomplete source behavior, source
composition, metadata overrides, effective catalog comparison, notifications,
and commit. Do not replace this with an unconditional write or report a changed
source as unchanged after a database error.

### Z17: Use smaller metadata targets for image selection and provenance

`internal/library/provider_images.go:54-79` consumes metadata revision, type,
library, parent, and folder facts after calling the full readMetadataRecord.
Its unrelated display/editor fields, automatic projection, overrides, and
locked-values payload are not used by this operation. Add a narrow private
target projection retaining the same item/metadata-state locks and supported
type checks.

Keep the local/music/online source composition that readMetadataRecord itself
performs; removing those validations is a different error-boundary change.
Preserve revision checks, image inspection/capacity, catalog notifications,
final actor authorization, and commit. Do not apply this recommendation to
the post-network preview check by deleting its full metadata validation.

Round 5 adds `internal/library/provider_metadata.go:75`, where ProviderSources
discards the entire metadataRecord. Reuse the smaller target in its existing
nonlocking repeatable-read transaction. Keep items/metadata-state existence,
supported-type checks, and source composition before querying provenance;
do not add an earlier identifier validator that changes error ordering. The
HTTP provenance handler's separate metadataDetail check still remains.

Keep the original online_type match: an old online source that the current
type intentionally ignores must not become a new validation error. Omitting
editor/display columns also omits their own scan errors, such as a manually
stored infinite editing timestamp. This is an explicit projection error-boundary
change; local/music/applicable-online composition errors remain observable.

### Z18: Reuse each newly decoded metadata RawMessage

`internal/library/metadata_values.go:415-419` creates a fresh RawMessage,
decodes into it, then copies the bytes again before storing the map entry.
RawMessage.UnmarshalJSON already owns a copy, and the destination is new on
each iteration; see the [Go implementation](https://go.dev/src/encoding/json/stream.go).
Store that value directly. The source-read, metadata-scan,
provider, and input-validation consumers avoid one allocation/copy per field.

Retain UTF-8, duplicate-key, root-object, trailing-data, number precision, and
all later field-validation behavior. Do not generalize this to shared buffers
or repeated decoding into a reused destination. Remote checks should mutate
the original input and individual returned fields to confirm independent
ownership, then measure allocations on representative metadata objects.

### Z19: Retain already validated user-settings JSON

`internal/identity/user_settings.go:229` encodes the validated settings map
and discards the bytes. PATCH then marshals the same unchanged map at line 87
for its before-image; the HTTP GET at `internal/server/user_settings.go:53`
also marshals the validated result again. Retain canonical bytes in a private
validated document for the PATCH baseline and an explicit HTTP JSON read.
This avoids one repeated serialization of a document bounded to 128 KiB.

Keep GetUserSettings' independent map contract, all key/value/count/byte limits,
null and case-alias handling, missing-row empty defaults, request parsing and
authorization order, no-op timestamps, and transaction completion. PATCH must
still validate and encode its changed map. Use Go's validated canonical bytes,
not PostgreSQL's differently formatted JSON text, so byte equality continues
to identify no-op patches. Preserve GET/HEAD Content-Length and the existing
body behavior. Do not cache this document across requests or mutable states.

Round 6 adds a related decoding reduction at
`internal/identity/user_settings.go:215-227`: null rejection currently requires
one full decode into RawMessage values and a second full decode into strings.
A single decode into map[string]*string can reject a nil root or nil member,
then create the independent string map before the existing full validation and
canonical encoding. Nonstring members still fail; an empty string remains
different from null. This removes a repeated parse and RawMessage payload
copies, but pointer/map allocation tradeoffs still need measurement.

Preserve ErrStoredUserSettings for malformed stored documents instead of
exposing raw validation errors. Empty PATCH and byte-identical PATCH still
return after final authorization and use deferred rollback. In particular,
a deletion-only no-op against a missing document must not commit the temporary
empty row inserted earlier. HTTP writes the same canonical bytes directly,
without an Encoder newline or []byte-to-base64 conversion.
The domain's empty-PATCH shortcut does not remove the HTTP preflight settings
read or change its position before request-body validation.

### Z20: Read only the account facts each lookup consumes

Five calls to the complete GetUser have narrower consumers:

| File in internal/server | Line | Required facts |
| --- | --- | --- |
| dynamic_sources.go | 101 | Raw target Policy for application-key conversion limits. |
| playback_info.go | 160 | Raw target Policy and real target existence. |
| client_sessions.go | 149 | Controller ID, disabled/admin flags, and raw Policy. |
| emby.go | 165 | Login target Name before normal authentication. |
| user_preferences.go | 374 | Complete Configuration document for its existing projection. |

Use small explicit identity projections, leaving full GetUser unchanged for
user DTO consumers. The fifth call site was added in round 7: it still needs
the complete Configuration document and original ProjectUserConfiguration
behavior, but not the rest of User. Preserve each query's original placement,
missing-row/database error mapping, and subsequent complete authentication or
authorization. An application key's selected target is not its authenticated
principal: do not add a disabled-user or administrator predicate. PlaybackInfo
must still check an explicitly supplied UserId even without a DeviceProfile.
Keep the existing raw-policy interpretation for conversion flags rather than
introducing a stricter ParseRuntimePolicy validation. Login-by-ID still maps
missing targets to the existing unauthorized response and performs ordinary
password/client/policy authentication afterward.

This removes unused Configuration, display, password-presence, and timestamp
columns where applicable; it does not cache users or reduce the number of
authority observations. An omitted column's independent decoding error is no
longer part of that narrow read, while errors in requested facts remain.

## Existing-principle follow-through

R01 has one additional response-local call site:
`internal/server/client_sessions.go:149,159-160` reads a controller once and
calls CanControlClientSession for each session. `internal/identity/policy.go:296`
parses the same policy on each call. Prepare the controller's decision facts
once for this list. Preserve RemoteControl feature denial before the admin
shortcut, target kind/user rules, and independent delivery-time authorization.
Malformed controller policy still means not controllable, not a new HTTP error.
Do not cache live authority across requests. This is not another new root cause.

Round 7 adds internal/server/dynamic_sources.go:107, which applies the same
remote-bitrate limit already applied by hlsPrincipalLimits at line 96 through
hls_runtime.go:514. The ordinary-login branch has no intervening limit change
or live observation; application-key limits are unaffected by this helper.
Return the existing limits after the target branch. Keep the initial policy
calculation, current target lookup, all dynamic-principal refreshes, local-peer
exemption, invalid-policy denial, and default remote ceiling. Do not combine
the separate raw application-target policy semantics into this simplification.

### N21: Complete authority-only Principal adoption

Ten additional production call sites consume only identity, policy,
client metadata, trusted peer, activity, and expiry. Use the existing
RevalidateSessionAuthority where applicable and a matching transaction-owned
entry for fanout, without removing or moving a fresh check:

| File relative to internal | Line | Consumer |
| --- | --- | --- |
| server/remote_commands.go | 478 | Remote command acceptance and trusted controller metadata. |
| server/media_download.go | 91 | Download identity/source authorization. |
| server/original_runtime.go | 153 | Original-stream policy, bitrate, owner scope, and expiry tightening. |
| server/analysis_preview_provider.go | 60 | Initial preview lease admission; its later credential refresh is already narrow. |
| server/audio_http.go | 132 | Universal-audio original source authorization. |
| server/websocket.go | 314, 320 | Event and Sessions delivery checks and snapshot-authority comparison. |
| notifications/queue.go | 79 | Fresh notification-reference filtering. |
| notifications/queue.go | 136 | Same-transaction fanout authority; the returned Principal is unused. |
| server/dynamic_sources.go | 83 | The revalidation function value used by six dynamic-principal refresh paths. |

Keep all existing deadlines, local-auth/peer behavior, revocation, source
checks, and independent final observations. Ordinary-user queries omit unused
display/password-presence/Configuration fields. The application-key branch
already has its separate projection and gains no corresponding field saving.
Full-user consumers retain the original RevalidateSession contract.
Omitted display columns also lose their independent scanning errors, for
example an out-of-range stored creation timestamp. This changes projection
failure behavior without removing a current-authority predicate.

The last three call sites were added in round 5. For queue.go:136, add an
authority-only transaction entry using the same connection and explicit
ReadCommitted transaction; preserve F11's revocation update and commit
behavior. Do not fall back to a pool query. Dynamic preference selection at
`server/dynamic_sources.go:323` still consumes the complete initial principal
before refresh; keep that input and all later fresh authorization checks.
The full observation API used by `library/playback_media_authorization.go:285`
retains its documented complete Principal result and is not changed by this
adoption of smaller getters.

### C03: Complete settings value projections at five callers

| File in internal/server | Line | Selected projection |
| --- | --- | --- |
| server.go | 253 | Task child admission's MaxConcurrent scalar. |
| provider_configuration.go | 20 | Metadata enabled, language, and country values. |
| task_executors.go | 26 | The current enabled value at the existing availability check. |
| task_executors.go | 48-52 | Captured Management options with an independent subtitle-language slice; build provider configuration from this capture instead of rereading and overwriting it. |
| runtime_http_binding.go | 66 | Existing ValueSnapshot for DesiredNetwork and revision. |

Expose small settings-owned projections from one atomic load. Preserve
per-admission freshness and worker-start snapshot consistency. Execute also
needs task cache limits and subtitle options, so it cannot use a scalar-only
projection. Keep required slice/pointer copies and full Get/GetConfiguration
responses. Do not collapse the two availability observations as an unrelated
behavior change. The HTTP-binding saving is once per startup, not a hot-loop
throughput improvement.

### Y04: Reuse already locked Run facts

`internal/tasks/runs.go:250` passes only its Run ID to lockRunChildrenAndScans,
whose line 579 query rereads TaskKey. Pass the TaskKey from that already locked
Run for this call. Preserve child/scan locks, force-probe consistency, and
validation. RecoverRuns initially holds only IDs and must retain a real TaskKey
read or obtain it in its original locking query. Do not substitute the current
definition or a request-provided key.

Round 5 adds RecoverRuns at `internal/tasks/runs.go:499-513`: after locking
and collecting every active parent ID, it rereads the same rows to compute
three automatic-analysis restart flags. Return the required small facts or
booleans in the original ordered locking query and aggregate them after
scanning. Keep the exact task-key/source/system-actor/stop-reason predicates,
empty-set false results, row ordering, and complete result closure before
child locks. This can share the initial TaskKey acquisition described above;
it must not change restart admission, cancellation, or rollback behavior.
Round 10 narrows this reuse to nonempty active-parent sets. When the first
query returns no parents, retain the existing aggregate query: it is the last
SQL before commit, and its failure can preserve the independent rollback path.
An empty result is not by itself proof that removing this final observation
has identical owner-lifetime behavior.

### N18: Skip unused self-user configuration defaults

For an ordinary or administrator login querying its own user ID, four paths
can avoid requestUserConfiguration's pure projection when none of its default
values can be consumed:

| File in internal/server | Line | Condition |
| --- | --- | --- |
| suggestions.go | 28 | Parsed query.IsPlayed is non-nil. |
| similar.go | 62 | Parsed query.IsPlayed is non-nil. |
| music_discovery.go | 130 | Parsed query.IsPlayed is non-nil. |
| items.go | 404 | Parsed IsPlayed is non-nil, and either normalized ParentID is nonempty or Ids has members. |

ProjectUserConfiguration parses the full legacy document, validates known
values, and copies lists, but has no I/O or error result. Skipping that unused
self projection saves CPU/allocation only. Keep initial query/Filter conflict
validation, neutral application-key behavior, and cross-user reads with their
current missing-user error ordering. Views and ordinary Items still consume
preferences and are not included. This is N18's additional caller coverage,
not a new preference or authorization cache.
Use normalized query values rather than raw parameter presence: a virtual root
can normalize to an empty ParentID, and an explicitly empty Ids list does not
disable global Latest exclusions. Played-state Filters can also establish the
non-nil IsPlayed value after conflict validation.
The Latest condition is `query.IsPlayed != nil && (query.ParentID != "" || len(query.Ids) > 0)`.

### R10: Avoid repeated cancellation writes and reuse transition IDs

Shutdown repeatedly invokes Stop while work is retiring. At
`internal/tasks/runs.go:266-269`, write the parent only on its first transition
to Stopping; the schema already requires stop time and reason for that state.
Keep the old Run for first-stop activity and shutdown decisions, and coordinate
the changed-state result with Z07.

At lines 291-293, limit the bulk scan flag update to NOT cancel_requested.
It can return the actual changed scan IDs, ordered in SQL, instead of the
separate taskScanCancellationIDs query (`internal/tasks/activity.go:64`).
Preserve the exact joined membership, active statuses, zero-change empty list,
query/row errors, and the original later audit position. Parent, child, and
scan locks, waiting-child transitions, fresh totals, actual cancel signals,
worker joining, and all rollback boundaries remain.

Do not apply this to `internal/library/task_scans.go:477`: its status UPDATE
also triggers pending deletion/publication guards. A superficially identical
row value is not proof that this different UPDATE has no required effects.
When changing the bulk UPDATE to a CTE, retain the actual-write fault hooks
and reached assertions in compatibility_stop_integration_test.go:156 and
store_integration_test.go:317; their existing SQL-prefix match needs updating.

T09's page-level vault-key reuse and the previously deferred backup-registry,
notification-capacity JSONB-size, and folder Boolean-EXISTS profiling leads
remain separate. They are not presented as new findings or silently accepted.

## Rejected simplifications and retained defenses

- The round-9 nil-deadline publication-fence candidate is withdrawn. Keep the
  final parent SELECT in internal/tasks/publication_fence.go:112 even when the
  parent fields are already locked. If the protected transaction context has
  expired after the child read while the Work context remains live, a query
  can fail before submission and let the caller roll back with the existing
  fresh cleanup context. A direct Commit with that expired context instead
  can close the reserved pgx connection and mark the catalog owner lost.
  This changes availability, not merely error timing. Reading the pinned pgx
  v5.11.0 sources established the distinction; no runtime reproduction was
  performed. The original implementation still has a later query-to-commit
  race, so it is not described as universally immune to owner loss.
- A zero-subscriber LibraryChanged shortcut changes semantics. A subscriber
  arriving after enqueue but before publication currently receives the batch;
  an invalid queued batch can also require that subscriber to reconnect.
  Preserve publication-time recipients and generation/resync behavior.
- NewManager and NewOfflineManager call CurrentContext on either side of an
  independent control.Read wait and file operation. The first State also binds
  the constructor's generation. Removing the second observation or substituting
  its State is not established as equivalent. The old R20 implementation does
  not prove that this additional merger is safe.
- Keep separate application credentials, client contexts, and device-generation
  barriers; they represent different revocation and ownership facts.
- Keep checks after lock waits and final database-clock checks, exact
  self-revocation handling, and last-administrator protection.
- Keep filesystem identity/containment checks, generation fencing, real worker
  joins, recovery history validation, and completion-capacity reservations.
  Cancellation alone does not prove resource retirement.
- Keep prepareAnalysisAdmission before the selected known-busy analysis
  deferral. Moving configuration/source admission itself later changes another
  set of failure and lock boundaries and was not selected.
- Do not move backup hashing into pg_dump output solely to remove a later
  read. It moves hashing CPU inside the database snapshot lifetime; no measured
  tradeoff supports that separate pipeline change here.

## Linux server support

No remaining Windows server implementation was found, so this review does not
claim a new Windows-code deletion. Goby and the command launcher select Linux;
native filesystem, process, and analysis implementations already use Linux.
Release compilation fixes GOOS=linux, and Docker delivery validates a Linux
ELF artifact. Current accepted Docker profiles are Linux amd64; a development
arm64 build option does not expand delivery acceptance. The native fingerprint
helper rejects other operating systems.

Keep Windows development-host tooling, client declarations, invalid-path test
fixtures, upstream third-party conditional code, and historical archive rules.
`internal/commanddomain/launcher_other.go` selects linux && !amd64, rejecting
unsupported Linux architectures; it is not a Windows fallback. The standalone
notification receiver and a research CLI do not form additional server targets.
Old test GOOS guards do not provide Windows server support.

## Review-loop ledger

| Round | Completed focus | New recommendations | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Four-track source review, historical reconciliation, consumer and concurrency counterexamples. | Z01-Z05; R01 additional call site identified separately. | 0 |
| 2 | Rotated projection/authority review; SQL NULL/empty-page checks; task execution consumers; viewing-statistics formula; complete delivery chain. | Z06-Z08. | 0 |
| 3 | Reverse execution/fence audit, statistics equivalence, graph and SQL claims, current dirty scan/media/HLS diff, platform negative paths, and independent report/history review. | Z07 extended to BeginRun; removed an unsubstantiated lock rationale and clarified existing boundaries. | 0 |
| 4 | Complete adjacent Run-query and settings-call inventories; independent provider review; deletion-graph and cancellation analysis; ordinary-user authority consumers; cross-track lock/error/null/trigger checks and historical reconciliation. | Z09-Z18; expanded Z07; N21/C03/Y04/R10 additional call sites. | 0 |
| 5 | Full metadata-read and direct/transaction/function-value revalidation consumers; source-composition and projection errors; cancellation combinations; user-settings canonical bytes; locked recovery facts and scan collection closure. | Z19; expanded Z11/Z17/N21/Y04. | 0 |
| 6 | Cross-check complete target-user consumers, self-preference defaults, canonical/no-op JSON handling, dynamic/notification authority, collection closure scope/budgets, and recovery flags. | Z20; expanded Z19 and N18 caller coverage. | 0 |
| 7 | Final authority/getter inventories, normalized preference conditions, task-definition target consumers, canonical JSON and full report/SQL/cost checks. | Expanded Z06/Z20 and R01 dynamic-limit call site. | 0 |
| 8 | Complete actor/target and task-display/target/execution consumer checks; independent combined error/commit/NULL/overflow audit; Linux delivery and historical-scope challenge. | None. Clarified existing compatibility-key, queue-error, and normalized-filter boundaries. | 1 |
| 9 | Reverse rejection/rollback/retirement audit, extreme inputs and memory accounting, independent history/source reconciliation, and publication-fence deadline cases. | Proposed a Y04 nil-deadline fence extension; round 10 later withdrew it. | 0 |
| 10 | Pinned pgx canceled-query versus failed-Commit paths, reserved-owner lifetime, sidecar retention, and all adjacent last-SQL reuse branches. | Z21; withdrew the fence candidate and retained BeginRun/nonterminal-progress final reads and empty-Recover final aggregation. Other later-checked reductions remain selected. | 0 |
| 11 | All reserved-owner transaction entry points, rollback result markers, sidecar cleanup consumers, task-manager failure classification, and remaining query reductions. | Z21 expanded to raw scan staging and existing flush-failure cleanup; conditional successful-rollback markers clarified. | 0 |
| 12 | Independent final audit of both owner entry points, all rollback-marker consumers, public failure classification, staging/sidecar lifetimes, remaining SQL reductions, and report/source/history consistency. | None. Clarified existing conditional auditing and database-versus-file publication boundaries. | 1 |
| 13 | Reverse public-result, cancellation, rollback/discard and actual-retirement checks; extreme query/JSON/graph inputs; pinned-driver and current-source evidence boundaries. | None. | 2 |
| 14 | Final complete authority, defensive/resource, performance and Linux reviews; independent consumer/error/source/history reconciliation of the frozen recommendation set. | None. | 3 - stop |

All four primary tracks, the independent review, the history audit, and the
coordinating review completed rounds 12-14 without a new recommendation or
actionable call site. The requested exit condition is satisfied after fourteen
rounds. The findings remain recommendations; implementation has not begun in
this review.

That statement describes completion of this review. The subsequent
[implementation record](backend-followup-implementation-20261011.md) documents
the changes made afterward; its verification and delivery status remain pending.

## Verification and resource handling

The test-environment capacity policy was read. Read-only SSH inspection found
test-env reachable, approximately 16 GiB available on persistent root, and
762 MiB in /tmp. No verification environment, worker, database, cache copy,
compiler scratch, or fixture was created. Nothing requires task-owned cache
cleanup. Historical successful verification is not current-source verification.
The round-10 driver comparison read the pinned module's source files over SSH;
it did not run a reproduction or modify the module, database, or remote owner.
