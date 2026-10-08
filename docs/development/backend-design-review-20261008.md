# Backend design review, 2026-10-08

## Scope and method

This review examines permission complexity, excessive defensive work, and
architectural performance opportunities in the current working tree at
`D:\Code\goby`, based on HEAD
`8d9a63b1af90bebf907ed26b0b34d1b6ec8df764` plus existing tracked and untracked
changes. HEAD alone does not reproduce this reviewed source. Existing work was
preserved; this review changes no application code.

Findings are supported by static call-path and synchronization analysis. No
tests, builds, runtime probes, or performance measurements were run, locally or
remotely. Operation counts below describe code paths, not measured latency or
throughput. Implementation validation belongs on `ssh test-env`, subject to
`test-env-capacity-policy.md`.

Each loop round searches for additional actionable suggestions, challenges the
safety of earlier proposals, and deduplicates by root cause. A narrower example,
wording correction, or additional call site for an existing recommendation does
not count as new. The requested stopping condition is three consecutive rounds
with no new accepted recommendation. Ten rounds were completed; rounds 8, 9,
and 10 added no new recommendation, satisfying that condition. This is review
convergence within the inspected scope, not a proof that no other issue exists.

## Assessment

The subsequent implementation review corrected one important interpretation:
internal scans use the operation-scoped startup grant selected in
`scan-operation-authorization-20261004.md`; they must not re-read approval or
authorization configuration at each phase. Live cancellation, physical mapping,
source identity, and publication fences remain. Statements below about fresh
request authorization apply to external requests unless explicitly stated.
See `backend-design-fixes-20261008.md` for fixes, rechecks, and current evidence.

There are concrete opportunities to simplify repeated checks within the same
authority snapshot, reduce lock scope on read-only operations, and move expensive
work out of streaming and polling loops. The current evidence does not justify
a replacement permission framework or a service decomposition.

Keep authorization after real waits, current credential and source checks,
filesystem identity validation, publication fencing, and actual resource
retirement. Their repeated appearance often corresponds to a different trust or
lifetime boundary. Simplification should remove duplicate work within those
boundaries.

P1 identifies a correctness/forward-progress risk needing prompt verification
and repair. P2 means a substantive optimization to plan; P3 means a smaller or
lower-volume simplification. Priorities do not imply measured production
incidents. Confidence is high in the identified extra work; realized benefit is
unmeasured. R15 additionally requires query-plan evidence before selecting an
implementation.

## Decision guide

| Requested area | Concrete opportunities | Finding IDs |
| --- | --- | --- |
| Permission overengineering and simplification boundaries | Repeated parsing/checks within one snapshot, overly broad read/revocation locks, repeated ancestor predicates, per-item remote-play authorization, preserving trusted peer context | R01, R02, R14, R15, R21, R25, R28 |
| Excessive defense | Goroutine-per-close for known internal resources, per-chunk directory audits and route preparation, capacity handling without forward progress, repeated whole-store verification and unused source opening | R04, R08, R16, R19, R20, R26 |
| Architectural performance | Narrow and batched browse projections, query reuse, read/write separation, bounded polling/checkpoint work, and consistent quota/lifetime sizing | R03, R05-R07, R09-R13, R17-R18, R22-R24, R27, R29-R30 |

Categories overlap; the report contains 30 distinct recommendations: one
P1, 21 P2, and eight P3. A separate working-tree integration observation appears
below. Suggested execution order:

1. Resolve the working-tree integration prerequisite, then reproduce and fix
   R19's legal notification-batch stall, R28's application-key peer regression,
   R29's auxiliary-resource group capacity mismatch, and R30's local-transcode
   I/O lifetime regression.
2. Take small, bounded simplifications: R01, R02, R12, R23, followed by the
   narrowly scoped R14 read path.
3. Address known amplification in browse/remote-play queries (R05-R07, R21),
   backup streams (R08), direct-read route preparation (R16), and unused download
   preparation (R26).
4. Reduce task polling and read/write coupling (R09-R11, R22), then account
   directory and NextUp work (R03, R17). Restore the intended scan checkpoint
   batching after reconciling the current work-in-progress source (R27).
5. Measure before undertaking larger query/revision changes (R15, R18).
   Schedule the remaining P3 ownership/lifetime simplifications according to
   observed load and implementation cost.

This order weighs correctness, scope, and structural work amplification. It is
not a ranking of measured speedups.

## Findings

### R01 — P3: Parse one authorization policy snapshot once

Evidence: `internal/identity/store.go:304-306`,
`internal/identity/policy.go:337-345`.

`ResolveWithPeer` parses the current policy, then immediately calls
`loginPolicyAllows`, which parses exactly the same bytes again. Parsing includes
bounded JSON traversal and normalization. Similar patterns exist in devices,
client sessions, avatars, user settings, preferences, managed users, device
registration, and password changes.

Use the existing `parsedLoginPolicyAllows` with the already parsed value, as
`session_revalidation_transaction.go:26-28` already does. Reuse only within one
database observation; retain fresh time, device, remote-access, and revocation
checks. Do not introduce a cross-request authorization cache for this change.

Validation: compare malformed-policy rejection and access-time boundaries, and
measure parse count and allocations per authentication request.

### R02 — P3: Give diagnostic read authorization a pure-check path

Evidence: `internal/server/observability.go:76-100`,
`internal/identity/administrator_authorization.go:55-93`.

The diagnostic helper calls `CheckAdministrator(true)` immediately followed by
`CheckAdministrator(false)`. The first call already authorizes, locks the actor,
and reauthorizes. No business operation occurs between the two calls. An ordinary
administrator therefore incurs five SELECTs plus BEGIN/COMMIT per helper call.
The locks are released before the subsequent filesystem read. Log list and line
handlers invoke the helper before and after file access; downloads also perform
periodic checks.

Remove the immediately redundant check, then consider a fresh read-only
authorization operation without actor locks for this helper. Preserve the
separate checks around file access, periodic download revocation checks, and
deadlines. Transactional business mutations still need their existing locking
authorization boundary.

Validation: revocation during file access/download, expiry, malformed policy,
native versus Emby audience separation, and SQL count per helper.

### R03 — P2: Push ordinary user-directory pagination into SQL

Evidence: `internal/identity/user_query.go:63-84`,
`internal/server/emby.go:245-257`.

`QueryUsers` transfers and scans every matching user's full policy and
configuration bytes before counting and slicing the requested page in Go. This also
happens for `Limit=0` and when `IsHidden` is absent. The actor locks remain held
while all these rows are processed. Full policy projection is performed for
`IsHidden` filtering; ordinary scans retain JSON as raw bytes.

Implement count plus page selection in one SQL statement for `IsHidden == nil`,
returning only the requested full records; the zero-limit path needs only a count.
Two default read-committed statements do not preserve the original single-statement
snapshot. Preserve ordering, totals, and fresh authorization/time checks.
The hidden-user filter needs a separately proven projection: malformed policies
currently receive conservative visibility treatment, so a simple JSON boolean
predicate is not an equivalent replacement.

Validation: page boundaries, zero limits, tied sorting, hidden/malformed policies,
and transfer volume at large account counts.

### R04 — P3: Close trusted internal resources without a goroutine per close

Evidence: `internal/library/storage_observation.go:54-74`,
`internal/library/primary_directory_read.go:48`,
`internal/library/media_source_warm_root.go:115-201`.

`closeStorageObservationResources` starts a goroutine and channel, then immediately
joins it, to isolate panic and `runtime.Goexit` in a callback. A successful
`openMediaSourceAndRelease` follows six such close calls: four `os.Root.Close`
operations and two internal root-pin releases. These concrete callbacks are
trusted implementation code. The extra goroutines provide no timeout.

Use a typed synchronous close path for those internal resources. Preserve error
propagation, `MarkUnknown`, phase ownership, and the fact that a blocked close has
not retired. Keep abnormal-callback isolation only where a callback can actually
execute independently supplied code.

Validation: descriptor ownership on cancellation and close failure, abnormal
worker exit at the outer boundary, goroutine creation and allocations per open.

### R05 — P2: Separate browse projections from playback-private media data

Evidence: `internal/library/query.go:27,769-772`,
`internal/server/items.go:296-301,608-618`, `internal/media/media.go:40-41`,
`internal/media/video_seek.go:17-18`.

Ordinary lists select and decode complete `i.media` before response Fields are
considered. That JSON includes private `VideoSeekIndexes`, which are never public
DTOs; each index can contain up to 8192 entries and 2 MiB. Production probing
enables these indexes (`internal/server/server.go:123`). Browse pages therefore
pay for playback-specific transfer and decoding even when requesting a small
summary.

Introduce an explicit browse projection, initially excluding private seek
indexes, then selecting optional expensive fields according to internal projection
options. Do not globally narrow `itemColumns`: planning, filtering, source
validation, and other callers still need their respective facts. The existing
movie ancestry projection optimization does not remove this media JSON cost.
Existing default domain queries promise a complete Item; add an explicit browse
mode rather than silently changing that default contract.

Validation: response equivalence for supported Fields and filters, SQL result
bytes, allocations, and page latency with populated seek indexes.

### R06 — P2: Batch collection metadata by page

Evidence: `internal/library/query.go:225`,
`internal/library/collections_query.go:159-168`,
`internal/library/collections.go:291,323,331`.

For each Playlist or BoxSet, `attachCollectionInfo` reads the collection and
counts visible members; an owner also loads sharing data. A page with N
collections performs an additional `2N + S` queries, where S is the number whose
sharing roster is visible to the owner or administrator. If all qualify, this
is 3N queries, serially on the transaction connection. Repeated collection IDs
can repeat work.
The CollectionFolder response branch also calls `GetLibrary` per item
(`internal/server/items.go:306-307`).

Deduplicate page IDs and batch collection records, ACL-filtered grouped counts,
and management-authorized sharing records; map results back to page order. Keep
the same authorization snapshot and limit sharing rosters to owners and
administrators. Batch the related CollectionFolder lookup where practical.
Deduplicate lookup keys only; retain distinct playlist entry IDs and page order
when the same media appears multiple times.

Validation: mixed ownership/sharing pages, inaccessible children, duplicate IDs,
stable page order, and SQL count versus collection count.

### R07 — P2: Make genre artwork projection optional and batched

Evidence: `internal/library/entities.go:165`,
`internal/library/entity_user_data.go:181-186`,
`internal/library/collage.go:119-149`, `internal/server/entities.go:43-56`.

Every genre without a primary image can invoke `readCollageManifest`, which
re-reads target kind, visibility, managed artwork state, and representative
images. Without managed artwork rows this already requires four queries per
genre. Image selection deduplicates candidates before `LIMIT 4`; the limit does
not bound earlier candidate processing. `EnableImages=false` is applied after
these reads, including the related search-hints path.

Pass an explicit image projection option into the domain operation. Skip collage
work when images are disabled. For image-enabled pages, batch target facts and
select representatives grouped by target under the current subject ACL. A shared
final-collage cache must not bypass per-subject visibility.
Preserve managed Primary tombstones even when no image row exists, source
precedence, stable representative ordering, and manifest revision/tag semantics.

Validation: images disabled, mixed managed/generated artwork, different library
grants, genres with overlapping sources, and query count per page.

### R08 — P2: Move full backup-directory auditing out of each stream chunk

Evidence: `internal/backupstore/writer_linux.go:117-152`,
`internal/backupstore/store_linux.go:95-108,178-195`,
`internal/recovery/backup_jobs.go:218`.

Each Writer chunk, at most 256 KiB, invokes `healthy` while holding the store's
global mutex. It checks root, marker, lock, and catalog identities, enumerates
the entire directory, and rebuilds an expected-name map. Import uses a 64 KiB
buffer: an 8 GiB stream entails approximately 131072 such directory passes when
reads fill that buffer. Smaller reads can cause more passes. Other store
operations share this lock.

Split full inventory enumeration from the per-chunk health operation. Retain
ownership checks and complete inventory audits at lifecycle/publication and
management boundaries, and use bounded periodic inventory auditing if required
during long streams. Per-chunk work must retain cancellation, degraded/closed
state, writer registration, pinned object identity and size, quota, scratch
reservations, and free-space checks. Detection of an externally added unknown
file would move to the next full inventory audit; that timing change is explicit.
Preserve durable publication, full archive verification, and guarded deletion.
The package does not promise isolation from a malicious process with the same
Unix UID.

Validation: rename/replacement and unexpected-file cases at required boundaries,
quota and cancellation, interrupted publication/restart, metadata syscall counts,
and status latency during large transfers.

### R09 — P2: Avoid locking every task definition on idle dispatch

Evidence: `internal/tasks/scheduler.go:191-225`,
`internal/tasks/system_events.go:62-90`, `internal/tasks/manager.go:15,387-390`.

Both schedule and system-event dispatch first iterate supported executor keys and
lock each definition separately, then select a single trigger. The work repeats
for every dispatched item, and idle polling still performs the definition reads
and locks. The catalog owned session is occupied during this work.

Use a non-authoritative candidate/no-work query before admission to the owned
write transaction. When work exists, acquire required definition/trigger locks in
the established order and revalidate current facts. A smaller first step is one
ordered batch query for the definition locks. Preserve fairness, database-clock
due checks, and ordering shared with trigger edits and reconciliation.

Validation: no-work SQL count, concurrent trigger edits/manual starts, competing
due definitions, and lock waits with a backlog.

### R10 — P2: Suppress unchanged task aggregate writes

Evidence: `internal/tasks/manager.go:551,623`,
`internal/tasks/runs.go:378-387,434-444`.

A reconciliation pass refreshes a run before and after child processing. Each
refresh aggregates all children and updates the run even when the persisted
values are unchanged. At the default 500 ms interval, a run visited each pass
can incur about four unchanged updates per second while making no progress.

Compare the locked run with computed aggregates and skip unchanged updates.
Avoid the second aggregate if the pass made no relevant change, subject to
concurrent worker semantics. Existing wake signals can later coalesce progress
work; periodic reconciliation should remain as a recovery fallback. Preserve
terminal transitions and their transactionally guarded side effects.

Validation: idle versus advancing children, worker completion races,
cancellation/restart, terminal side-effect uniqueness, and rows/WAL per minute.

### R11 — P2: Skip the notification journal lock when transport is disabled

Evidence: `internal/database/migrations/0048_notifications.sql:77-82`,
`internal/library/play_sessions.go:900`,
`internal/library/userdata_update.go:101`.

The source-recording function takes the singleton journal row `FOR UPDATE`
before deciding that transport is disabled. Ordinary user-state and playback
progress writes can therefore request this exclusive row lock for no notification
work. This adds a shared lock dependency across callers; its realized contention
also depends on the surrounding catalog owner serialization.

After acquiring the existing transport SHARE lock, return immediately when
transport is disabled, before taking the journal lock. Keep sequence allocation
and the no-eligible-subscriber check under their required registration/cursor
ordering. Moving that subscriber check outside the journal lock needs a separate
concurrency proof. Implement any SQL function change in a new migration; do not
edit the published migration in place.

Validation: disabled transport, concurrent enable/register/source commits,
initial cursor ordering, and journal lock acquisition counts.

### R12 — P2: Skip the second fanout transaction for an empty source page

Evidence: `internal/notifications/queue.go:121-148,185-209`,
`internal/notifications/runtime.go:134`.

Even when no sources are returned, fanout opens a second transaction, locks and
re-reads the registration, and updates its cursor to the same value. At 64
eligible registrations this means 128 explicit transactions and 64 unchanged
updates per pass, excluding other revalidation work. The loop ticks every 500 ms;
actual pass frequency depends on work duration.

Return after the first transaction when `sources` is empty. A later optimization
can select only registrations behind a journal high-water mark while preserving
periodic revoked-registration maintenance and send-time revalidation. Nonempty
pages still need atomic cursor advancement with delivery creation, even when all
sources are filtered out.

Validation: empty pages, filtered-only pages, concurrent registration revision
changes, revocation, and transactions/updates per idle pass.

### R13 — P3: Index media-policy leases by owner

Evidence: `internal/server/media_policy.go:105-149,222-233,318-328`,
`internal/server/playback_media_authorization.go:20`.

Acquire, check, and heartbeat invoke `reconcileLocked`, which traverses every
lease under one global mutex to expire old entries and gather one owner's plays.
Creating a lease then performs another global owner-count pass. The configured
ceiling is 4096 leases, so each user's hot-path policy work scales with unrelated
users' retained plays. Per-lease expiry timers already exist.

Maintain owner membership/counts so policy reconciliation visits only that
owner's leases. Keep exact expiry and deterministic oldest-play retention when
limits tighten; retain bounded global-capacity cleanup where required. Start
with shorter work under the existing mutex rather than adding a distributed
quota system or complex lock sharding.

Validation: simultaneous acquisition, tightened limits, expired but delayed
timers, application-key owner grouping, and mutex time versus unrelated owners.

### R14 — P2: Avoid the global management lock for API-key metadata lists

Evidence: `internal/identity/application_keys.go:234,486-488`.

The native API-key list has `RevealTokens=false`, but its shared operation helper
still takes the exclusive `managedUsersLockID` advisory lock. User changes,
session revocations, and device mutations also use that lock. A metadata-only
list neither decrypts secrets nor records a reveal audit, and its count/page
already come from one SQL statement.

Separate the non-revealing read path from mutation/reveal admission. Preserve
fresh actor and credential authorization, appropriate actor SHARE locks, final
authorization, and the count/page snapshot. Keep the stronger lock ordering for
creation, revocation, reveal, and operations affecting other principals.

Validation: simultaneous account/device changes, revocation, audiences,
secret omission, and lock acquisition/wait counts for metadata lists.

### R15 — P2: Reuse ancestor facts inside content-policy SQL

Evidence: `internal/library/policy_access.go:68-112`.

The same nearest inherited-rating recursive scalar expression is interpolated
twice for `MaxParentalRating` and twice for `BlockUnratedItems`. Enabling both
creates four textual copies in one item predicate; excluded folders and tag
rules construct additional ancestor traversals. This establishes repeated query
structure, not a guaranteed fourfold runtime cost.

Investigate one statement-local ancestor/rating/tag projection reused by the
conditions. Keep authorization before count/pagination, cycle termination,
same-library ancestry, unknown-rating fail-closed behavior, organization-folder
exceptions, and separate extra/theme-owner authorization. Confirm actual plans
before choosing a CTE, lateral relation, or other shared projection; avoid a
process-wide authorization cache.

Validation: EXPLAIN with both rating restrictions and representative folder/tag
policies, plus result equivalence for cycles and inherited/unknown ratings.

### R16 — P2: Prepare the immutable I/O route once per reader

Evidence: `internal/primaryio/read_seeker.go:57-68,144`,
`internal/primaryio/admission.go:334`,
`internal/library/primary_read.go:16,306`.

The reader constructor already copies and validates its route into private
state. Every subsequent Read calls Owner/ Governor admission, which copies,
deduplicates, and validates that same route again. The production reader uses
32 KiB chunks: a 1 GiB read entails at least 32768 repetitions, constructing map
and slice state without receiving new caller-owned route input.

Add an internal immutable prepared-route representation. Construct it once and
reuse it in the trusted reader admission path; public entry points accepting
arbitrary mutable Route values must continue to copy and validate them. Keep
per-chunk admission, queue fairness, cancellation, and retirement accounting.

Validation: mutation of original caller slices, conflicting queued routes,
cancellation/retirement, and route-processing allocations per streamed GiB.

### R17 — P2: Compute NextUp candidates once and support actual count-only reads

Evidence: `internal/library/nextup.go:58-62,121-182`,
`internal/server/nextup.go:26-43`.

NextUp executes its recursive scope and windowed candidate query once for total
count and again for the result page. An HTTP zero limit is changed to one, so it
also reads a full item and attachments before discarding them. An empty candidate
set still reaches the second statement.

First add explicit count-only and empty-result paths. Consider one materialized
candidate relation serving both count and page, preserving total count for empty
or out-of-range pages. Starting from eligible series history may further reduce
work, but ParentID currently filters candidates rather than the complete history;
moving it earlier changes semantics. Preserve ACLs, nested series, and special
episode ordering.

Validation: new users, zero limit, out-of-range offsets, parent filters, nested
series and specials, and recursive/window work in actual query plans.

### R18 — P2: Maintain source-fact digests at publication time

Evidence: `internal/library/media_operations.go:32-34`,
`internal/library/playback_media_authorization.go:175-178`,
`internal/library/media_publication_barrier.go:35,74`,
`internal/server/hls_http.go:436`.

`MediaOperationSourceRevisionSQL` constructs JSON from source facts including all
of `i.media`, converts it to text, and hashes it. HLS authorization computes it,
the source-open publication barrier computes it again, and delivery has another
fresh authorization phase. Large private seek indexes thus cost database CPU
even if the SQL result projection excludes them. This is distinct from R05's
transfer and application-decoding cost.

Measure this path, then consider a persisted source-fact digest or dedicated
source revision maintained atomically by every relevant writer. Continue reading
and combining current root-binding facts and preserve both authorization phases,
named-path checks, and the publication barrier. A metadata `updated_at` is not
an equivalent revision. This change needs a migration and an inventory of every
source mutation path, making it more involved than straightforward batching.
The existing revision also appears in persisted edit/derived references. Either
materialize the current expression equivalently or version and migrate those
references and in-flight operations explicitly; replacing JSON with a digest
inside the expression changes the revision string.

Validation: probe/edit/root/path changes, old-data migration, concurrent
publication/open races, and database CPU with populated seek indexes.

### R19 — P1: Prevent notification capacity checks from trapping a whole batch

Evidence: `internal/notifications/queue.go:121,185-209,219-255`,
`internal/notifications/runtime.go:142-153`,
`internal/library/metadata_music_notifications.go:73-99`.

Fanout reads up to 16 source events and enqueues the page in one transaction.
When the ninth delivery encounters eight pending deliveries, coalescing merges
their references. More than 4096 unique references returns `ErrLimit`, rolling
back the entire page and its cursor. The next pass selects the same page and
fails again. This can occur with an initially empty delivery queue.

A legal burst can contain nine disjoint album updates with 256 tracks each:
track and parent/source references can contribute at least 512 references per
event, individually within source limits, but at least 4608 when coalesced.
This requires the burst to accumulate before successful fanout. In addition,
the runtime skips delivery consumption when fanout returns an error. Existing
queued deliveries cannot then free space; accumulated source events can
eventually hit journal limits and fail the surrounding business write.

Commit a capacity-fitting source prefix with its corresponding cursor and
deliveries, and let existing deliveries drain independently of fanout capacity
errors. Merely moving drain earlier does not fix the empty-queue whole-page
rollback. Preserve authorization filtering, ordering, atomic cursor advancement,
and all size limits. Do not simply increase the limits.
Use a savepoint or separate transaction boundary for each attempted prefix step:
enqueue can cancel prior pending rows during coalescing before a later size or
global-capacity check fails. Catching that error and committing the transaction
without rolling back the failed step would incorrectly retain those cancellations.

Validation: a remote regression case with nine individually legal disjoint
events and an initially empty queue; repeat with existing pending deliveries,
permission changes, restart, and near-full journal state. This failure is a
static control-flow finding; it has not been reproduced at runtime in this review.

### R20 — P3: Consolidate repeated lifecycle verification into one startup read

Evidence: `internal/lifecycle/store_linux.go:336-339,404-413`,
`internal/lifecycle/generations_linux.go:84-89`,
`internal/recovery/runtime.go:93,114,130`.

`ActiveConfig` reads Current, the selected generation, and, when selected, its
generation master-key path through separate public methods. Each method invokes
verification that reads and
hashes registered generations again. The registry admits up to 4096 generations;
the repeated work grows with retained history.

Provide one coherent operation that performs the existing complete verification
once and captures the current manifest, selected generation, and master-key
reference under the same store synchronization. Preserve full integrity checks,
root ownership, activation proof, and CAS boundaries. Making damage to unreferenced
history nonfatal would change the retention/integrity contract and is not part
of this recommendation.

Validation: current-generation changes during startup reads, corrupted referenced
and historical files, rollback/journal state, and bytes hashed per ActiveConfig.

### R21 — P2: Batch remote-Play permission projections for each participant

Evidence: `internal/server/remote_commands.go:28,500-504,530-550,637`,
`internal/library/query.go:246-289`, `internal/server/websocket.go:313-332`.

Remote Play permits up to 128 item IDs. For every distinct ID it calls
`GetItemFor` separately for controller and target, opening a complete item-read
transaction and loading media, metadata, user state, subtitles, and related
projections. The caller needs visibility and target play permission. HTTP
acceptance and eventual socket delivery each perform this stage, giving up to
512 full item transactions for 128 distinct IDs and one receiving socket. The
socket delivery authorization has a two-second budget; overload can fail the
command and end that send loop.

Use a minimal batch authorization projection separately for controller and
target at each stage. Preserve rejection of any missing/invisible/non-playable
ID and required error ordering. A helper that silently drops invisible IDs is
insufficient without checking complete request coverage. Do not reuse HTTP-stage
authority after the asynchronous queue; keep send-time authorization.
Preserve the existing target `Item.CanPlay` semantics; controller items require
visibility only. Do not introduce new `media != nil` or `!IsFolder` requirements.

Validation: 128-item commands, mixed visibility, virtual non-playable IDs,
controller/target policy changes while queued, and transactions per command.

### R22 — P2: Read activity pages outside the catalog write-owner gate

Evidence: `internal/server/observability.go:266-286`,
`internal/server/observability_emby.go:156`,
`internal/library/ownership.go:179,235,309`,
`internal/activity/query.go:17-36`.

Activity pages execute pure count/page selection and DTO construction through
`library.WithOwnedTx`, holding the catalog owner's mutex and reserved connection.
The query already computes count and page in one SQL snapshot. Slow activity
reads therefore hold the same gate needed by scan checkpoints and catalog
publication. The owned transaction also uses a cancellation-detached, bounded
context, extending unnecessary work after a reader disconnects.

Use an independent bounded pool transaction for this read, preserving current
generation/availability admission, administrator SHARE locks, and final authority
checks. Honor request cancellation and retain the single-statement count/page
snapshot. The transaction must still permit `FOR SHARE`; setting PostgreSQL
`READ ONLY` blindly would reject that lock operation. This is separate from
R02's redundant diagnostic authentication calls.

Validation: activity reads during scans, authorization/revocation and generation
changes, disconnected clients, catalog owner hold time, and count/page consistency.

### R23 — P3: Remove the single-query transaction around notification targets

Evidence: `internal/notifications/queue.go:38-58`,
`internal/notifications/runtime.go:171,235`.

`currentTarget` opens a repeatable-read transaction, executes exactly one SELECT,
and commits before further identity and resource checks. It does not extend a
snapshot across those later operations. Delivery initialization and the periodic
250 ms delivery check repeatedly use this wrapper.

Let `readTarget` accept a narrow query interface so this caller can execute the
unchanged SELECT directly on the pool. Preserve every predicate, error mapping,
and revalidation interval. This removes explicit BEGIN/COMMIT work without
replacing delivery authorization with cached state.

Validation: configuration/registration/session changes, error mapping, and
database statement/round-trip counts during an active delivery.

### R24 — P3: Release the sidecar retry I/O lease before database-only refresh

Evidence: `internal/library/primary_sidecar_read.go:99-109`,
`internal/library/provider_subtitles.go:327-328,335-430,446-451`.

The capacity-only retry wait executes its fresh-authority callback inside
`operation.Run`, holding actual global/root/domain I/O quota while the callback
performs only SQL reads. It then releases the operation and restarts preparation.
The real file-open attempt checks authority again immediately before source I/O.
Under admission and database contention, SQL wait time is charged as disk work.

Keep the fresh check but complete the memory-only admitted callback and release
its actual I/O lease first, then run the database refresh while retaining the
operation owner. The Run callback context is canceled when Run returns; create
a fresh context observing both request and Store-owner cancellation instead of
reusing that context or dropping the owner signal. A capability-only context
wrapper does not merge cancellation. Close the owner after this refresh. Do not
remove authority refresh, move the final file-access check, or relax full
retirement before retry.

Validation: saturated root/domain admission, slow SQL, cancellation and source
change during retry, and actual I/O lease hold time before real file access.

### R25 — P3: Narrow account locks during single-session revocation

Evidence: `internal/identity/managed_sessions.go:265-293,324-348`,
`internal/library/playback_media_authorization_batch.go:18`.

`RevokeManagedSession` takes account rows `FOR UPDATE`, although it only changes
one session's revocation state and writes audit data. If a later session lock or
audit write waits, these account locks conflict with user SHARE locks used by
otherwise unrelated sessions' playback authorization. Revoking one session can
therefore delay sibling sessions of the actor/target accounts.

For this method only, use account SHARE locks while retaining the global
management-mutation advisory lock, deterministic account-before-session ordering,
session UPDATE locks, fresh authority checks, and the exact self-revocation
exception. SHARE still prevents concurrent account disable/demotion/deletion.
Do not apply this to operations that actually modify accounts or passwords.
Existing application-key mutation admission already uses account SHARE locks.

Validation: hold the target session row so revocation waits, and confirm sibling
playback authorization can proceed; also retain reciprocal revocation, expiry
after audit/update, self-revocation, and account mutation coverage. This is a
low-frequency administrative path, so its realized impact is unmeasured.

### R26 — P2: Remove the unused file-open stage from download preparation

Evidence: `internal/server/media_download.go:63`,
`internal/library/media_download.go:33`,
`internal/server/original_primary_download.go:25-30`.

The Download/File handler calls `OpenDownloadFor`, completing source admission,
database authorization, and a filesystem-proven open. Its returned planning
descriptor is immediately closed without reading bytes or making a media
decision. `OpenOriginalDownloadFor` then performs the actual registered delivery
open using the returned item/source and expected ETag. GET and HEAD pay for this
unused first open and its cleanup.

Introduce a download-specific database-only planning snapshot, followed by the
existing actual-delivery open. Preserve fresh download authority after waiting,
expected ETag and item/source/type checks, full path/file identity validation,
reader registration, and periodic source/authority checks. Download permission
remains independent of playback permission, including before HTTP cache validators.
Preserve a bounded preparation deadline for actual source opening/queueing,
separate from the successfully handed-off reader's response lifetime. Do not
create a long download reader with a 20-second lifetime, and do not drop the
initial open timeout when removing the planning open. A timed-out open worker
retains its resources until actual cleanup.

Validation: GET/HEAD/Range and cache validators, disabled playback but permitted
download, queued revocation/source replacement, Store shutdown, and filesystem
open/close counts per request.

### R27 — P2: Reconnect bounded scan-progress checkpointing

Evidence: `internal/library/scan_probe_pipeline.go:306-307`,
`internal/library/scan.go:502,515,686`,
`internal/library/scan_progress.go:6-7,39`, `internal/library/jobs.go:439-453`.

The current uncommitted pipeline increments Scanned and immediately calls
`persistProgress` for every prepared file. The existing 64-item/500 ms batching
helper remains defined, but no production call to `maybePersistProgress` remains.
Cached and changed completion paths also issue individual checkpoint operations.
The no-change guard cannot remove the entry write because Scanned just changed.
N files therefore cause at least N entry progress transactions.

Reconnect the existing bounded checkpoint scheduler on ordinary entry/completion
paths, after resolving the working-tree integration issues below. Keep current
task/root authorization on each applicable source admission, final publication
fencing, forced error-repair checkpoints, cancellation handling, and terminal
flush. This is distinct from R10's aggregate task-run polling.

Validation: cached/changed/mixed scans, cancelled/retired task authority,
checkpoint durability and final counters, transaction counts per file. Historical
batching receipts do not establish this current working tree's behavior.

### R28 — P2: Preserve trusted peer context when revalidating application keys

Evidence: `internal/identity/session_revalidation.go:38-44`,
`internal/identity/application_keys.go:91-94`,
`internal/server/dynamic_sources.go:83,131-145,323-328`,
`internal/server/live_streams_http.go:26-28`.

The current uncommitted application-key revalidation branch returns the scanned
principal directly, removing the previous assignment of the trusted PeerIP. The
database principal scanner does not set that field. Dynamic-source revalidation
compares the full refreshed owner with the original HTTP owner, including PeerIP.
An application-key request with a nonempty peer therefore fails the owner check
and returns Unauthorized during dynamic-source playback negotiation, even before
the no-DeviceProfile response path.

Restore the trusted peer from the previously authenticated principal after
successful database revalidation, while retaining all credential/key/client
checks. Restore the removed peer-preservation assertion and add a dynamic-source
HTTP regression. This is a boundary that simplification must preserve, not an
argument to remove the dynamic owner check.

Validation: dynamic playback negotiation with an application key and nonempty
local/remote peer, key/client revocation, and revalidation round trips. Do not
generalize this finding to all file HLS: its authorization batch separately
restores the peer in `playback_media_authorization.go:277`.

### R29 — P2: Align auxiliary-group preparation with retained-owner capacity

Evidence: `internal/library/themes_scan.go:574-587,657-659,1096-1102`,
`internal/library/primary_scan_read.go:91,326-332`,
`internal/library/primary_read.go:145`, `internal/library/themes.go:22`,
`internal/library/extras.go:21`.

Theme/extra groups allow 256 resources per owner. The current dirty preparation
loop retains each entire input until the whole group finishes; each input
registers a process-wide retained owner, whose limit is 64. A legal 65-resource
group can exhaust that limit without any concurrent request. Publication-witness
preparation also makes new input copies and registers another owner for each,
while the originals remain held. An ordinary group with 33 resources can therefore
exceed the limit during this second phase. Concurrent playback reduces headroom.

Retire each completed probe's actual resources, retaining bounded immutable facts
across phases. Give atomic group publication one explicit group lifetime with
separate descriptor/fact budgets, rather than two sets of per-file registrations.
Preserve source identity/named-path checks, complete-group publication, and the
old group's integrity on failure. Do not simply raise the global limit. This is
a retained-owner capacity mismatch, not an actual-I/O-phase deadlock: receipt
completion already releases each probe's actual phase.

Validation: 32/33/64/65/256-resource groups after source integration, with and
without concurrent playback, cancellation and source changes, and retained-owner
counts throughout both preparation and publication.

### R30 — P2: Restore job-lifetime source-I/O admission for local transcodes

Evidence: `internal/server/hls_http.go:395`,
`internal/server/hls_admission.go:199`,
`internal/transcode/manager.go:480,1363`,
`internal/transcode/runner_linux.go:105`,
`internal/media/source_read_phase.go:52-55`.

The current dirty changes remove the server SourceRead adapter, the manager's
SourceRead option/preparation, retained job source read, and runner-context phase
injection. The manager creates job context from its own lifetime, so request and
pending-admission contexts do not supply the missing phase. The runner still calls
`RunSourceReadPhase`, which executes work directly when no phase exists.

Local HLS/transcode jobs still have transcode job limits, but no longer participate
in the primaryio global/root/domain admission shared with original playback,
downloads, and scans. Request loans and pending forks end after Ensure and cannot
cover queued work, FFmpeg execution, and final descriptor closure.

Restore the complete job-lifetime adapter: retained source preparation, context
injection, and actual final retirement, including unknown-retirement propagation.
Restoring a struct field or server assignment alone is insufficient. Preserve
the distinction between local files and `SourceMode == "stream"`, which the
previous hook excluded. This is a production path, unlike the deferred native
command-scope capability path discussed below.
The retained owner must belong to the job's lifetime, independently of the HTTP
loan; actual-I/O leases still cover real source work rather than all queued time.

Validation: queued/running/cancelled local jobs under concurrent playback and
scans, root/domain limits and foreground headroom, exact reader retirement and
failure accounting; dynamic network input should keep its separate behavior.

## Working-tree integration prerequisite

Static source inspection found incomplete scan/transcode-state migration,
separate from the 30 recommendations. `scanState` in
`internal/library/scan.go:31-53` no longer declares `imageInspection` or `walkIO`
and has no embedded type providing them. Remaining code still accesses
`state.imageInspection` at `internal/library/images_scan.go:389,392` and
`state.walkIO` at `internal/library/primary_sidecar_read.go:222-223`.
`internal/transcode/manager_finalization.go:74` also accesses `j.sourceRead`,
although that field was removed from the current managedJob definition.

Reconcile the intended replacement or restoration of those fields and their
ownership/lifetime initialization before remote build or runtime verification.
No compiler was executed in this review, and no claim is made that the current
dirty source builds. The existing changes were preserved for their owner.

## Rejected or deferred observations

- Historical exclusive application-key steady-state locking, duplicate activity
  touch, full playback media projection, root-wide cache work, and unconditional
  full periodic transcode cache scans have current improvements. They are not
  reissued as unchanged findings.
- The command-domain capability path repeats full executable hashing, but its
  command-scope integration was not found on the current production path.
  Reconsider at integration time; no current service performance claim is made.
- A close-error retirement concern lacks a complete platform/error-class proof
  and is not an accepted finding.
- Source root/parent/file before-and-after identity checks, authority rechecks
  after admission waits, and keeping ownership until actual worker retirement
  protect concrete races.
- Recovery control, coordinator records, and backup object publication address
  distinct crash/failure boundaries. Their package count does not establish
  overengineering. Full rehash before restore and publication recovery must stay.
- The bounded event hub already shares encoded messages and limits subscribers;
  a more elaborate dispatch index has no established priority here.

## Loop ledger

| Round | Focus | New accepted recommendations | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Parallel baseline review of authorization, filesystem defense, browse queries, playback, tasks, backup, and notifications | 13 (R01-R13) | 0 |
| 2 | Cross-check trust boundaries and examine read admission, recursive queries, per-chunk work, notification backpressure, and startup | 7 (R14-R20) | 0 |
| 3 | Adversarial permission and lifecycle review, queue-failure proof, remote commands, and read/write isolation | 4 (R21-R24) | 0 |
| 4 | Existing test-contract cross-checks, public-route coverage, projection semantics, recovery and notification failure boundaries | 0 | 1 |
| 5 | Cross-feature empty-page, credential, cancellation, runtime-capacity and restart scenarios | 1 (R25) | 0 |
| 6 | Reconcile historical findings with current dirty source; inspect download preparation, scan checkpoints, and peer-context propagation | 3 (R26-R28), plus a separate static integration prerequisite | 0 |
| 7 | Audit source lifetimes and legal resource-group sizes; challenge dynamic-key and download boundaries | 2 (R29-R30) | 0 |
| 8 | Consolidated authority, retained-owner, actual-I/O, durable-order, and batching compatibility audit | 0 | 1 |
| 9 | Scenario review across small/large catalogs, multiple users, slow storage, saturated capacity, optional notifications, shutdown, and restore | 0 | 2 |
| 10 | Final independent challenge of triggers, current-source evidence, calculations, safe replacements, and failure/recovery contracts | 0 | 3 |

The loop ended after round 10. All parallel review work was complete before
closeout. No application changes or verification commands were performed.
