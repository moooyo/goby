# Backend design review convergence, 2026-10-09

Implementation follow-up: [backend-convergence-implementation-20261010.md](backend-convergence-implementation-20261010.md)
records the authorized changes, verification and delivery. The review below
retains its original findings and review-time status.

## Scope and evidence

This review examines the working tree based on `8ae660bf`, including its
pre-existing uncommitted changes. It covers permission complexity, unnecessary
defensive work, architectural performance, and the Linux-only server boundary.
R01-R30, N01-N22, L01-L08, Q01-Q09, S01-S09 and F01-F13, together with their
implementation records, form the deduplication baseline. A previously reported
recommendation is not new merely because its implementation has an optional
extension. A previously unreported call path with a concrete remaining cost is
identified explicitly below.

Four parallel review tracks feed sequential rounds. Subsequent rounds challenge
the preceding findings and inspect additional paths. The stopping condition is
three consecutive complete rounds with no new accepted recommendation across
all four tracks. An incomplete track does not count as an empty round.

Ten rounds completed. Rounds 8, 9 and 10 each added no new accepted
recommendation across all four tracks, satisfying the stopping condition.
There are fifteen new findings: C13 is implemented and remotely verified;
the other fourteen comprise five P2 and nine P3 recommendations.

All costs below are source-level observations. There are no measured latency,
throughput, allocation, database-plan or hardware-performance improvements in
this report. C01-C12 and C14-C15 remain recommendations. C13 is the separately authorized
Linux path cleanup; its implementation and remote verification are tracked
below. Existing unrelated source changes are preserved.

## Recommendations

| ID | Priority | Area | Recommendation |
| --- | --- | --- | --- |
| C10 | P2 | Permission correctness | Revalidate a remote controller after receiver and item-query waits. |
| C09 | P2 | Background discovery | Separate automatic discovery from unchanged batch continuations. |
| C07 | P2 | Defensive I/O / request architecture | Inspect the configured AMD device only at consumers that need hardware facts. |
| C06 | P2 | Catalog queries | Apply known expected-episode IDs before constructing virtual item rows. |
| C12 | P2 | Coordinator polling | Index the full pending-media-operation predicate and priority order. |
| C01 | P3 | Permission locking | Use shared account locks during ordinary device deletion. |
| C02 | P3 | Permission composition | Reuse the policy already read and locked for session listing. |
| C03 | P3 | Request allocation | Read a narrow settings value snapshot for ordinary requests. |
| C04 | P3 | Transcode maintenance | Filter inspection candidates before copying and sorting retained jobs. |
| C05 | P3 | Defensive SQL / aggregation | Remove redundant descendant-count deduplication; measure a dedicated played predicate. |
| C08 | P3 | Progressive startup allocation | Reuse an observer-owned prefix buffer while retaining every fresh read and parse. |
| C11 | P3 | Defensive logging | Avoid reparsing a privately encoded JSONL record before queue admission. |
| C14 | P3 | Scan preparation | Batch auxiliary sort-name derivation without extending probe lifetimes. |
| C15 | P3 | Defensive accounting | Avoid recounting every retained auxiliary fact after each appended file. |
| C13 | Implemented | Linux-only cleanup | Remove Windows drive-prefix assumptions from auxiliary media classification and current-schema constraints. |

C10 deserves a targeted authorization regression first. C09, C07 and C06
deserve performance investigation because their work can exceed the scope of
the actual request. C12 matters when media operations are enabled and history
has accumulated. C01-C04 are narrower changes. C05 should start
with its proven uniqueness simplification and use representative plans to
decide whether the larger Boolean-query rewrite is worthwhile. These are
priorities for implementation and measurement, not measured speedup rankings.

### C01: Share account locks during ordinary device deletion

Evidence: `internal/identity/device_mutations.go:159-181,230-264`,
`internal/identity/device_registration.go:36-41`, and
`internal/library/playback_media_authorization_batch.go:18`.

`lockDeviceDeletion` takes `FOR UPDATE` on the actor account and every ordinary
account with a historical login on the device. It retains those locks while
waiting for session and device locks and recording the deletion. The operation
updates `devices` and `sessions`, not `users`. It can therefore block shared
account authorization on those users' other devices even though those other
credentials are not being revoked.

Change only the account-lock query to `FOR SHARE`. Keep the management advisory
lock, the device-registration advisory lock, deterministic account/session/
device ordering, exclusive session and device locks, complete credential
generation selection, fresh post-wait authorization, and the exact self-revoke
exception. Account mutation and deletion still conflict with shared locks.
Same-device registration remains excluded before selecting the owner set.

R25 changed single-session revocation; N01 changed application-key device
operations. This ordinary-device deletion path is a remaining, distinct caller.

Verify a deletion blocked on its target session while another device of the
same user performs a shared authority read. Also cover registration/deletion
races, account disable/delete, password rotation, self-deletion, expiry and
revision conflicts. Do not claim that all same-session contention disappears:
the credential locks intentionally remain exclusive.

### C02: Reuse the locked policy in client-session lists

Evidence: `internal/identity/client_sessions.go:252-266,328,382-419` and
`internal/server/session_subscriptions.go:40,104`.

`ListClientSessions` calls `lockClientSession`, which already reads, shared-locks
and parses the ordinary user's current policy. The list then issues another
`SELECT policy FROM users` and parses the same policy to obtain its remote and
shared-device control flags. The account row remains locked in the same
transaction throughout. Both HTTP session listing and the default one-second
WebSocket Sessions subscription use this path.

Return the successfully parsed policy or the two required control flags through
a private helper result. Remove the additional policy query and parse. Keep
the original account/session locks, the application-key branch, post-lock
database-clock observation, and the final `lockClientSession` call. Never
substitute the potentially older HTTP principal policy or a cross-request
permission cache.

This removes one SELECT and one policy parse per ordinary-user list invocation.
It is distinct from R01's duplicate parsing within one policy observation and
N21's narrower background authority projection. End-to-end gains are unmeasured.

Verify ordinary/admin/application-key visibility, both control permissions,
policy updates before lock acquisition, expiry during list construction, and
the actual query count. Keep the final time-dependent check even when account
and credential fields are fixed by locks.

### C03: Project settings values before making defensive copies

Evidence: `internal/server/server.go:384`,
`internal/server/settings_context.go:26-34,49-56`,
`internal/settings/store.go:69-70`, `internal/settings/models.go:173-178`,
`internal/settings/management.go:82-84`, and
`internal/settings/runtime_models.go:113-126`.

Every request passes through the outer settings middleware. It calls the full
`Store.Snapshot`, cloning optional overrides, subtitle-language and sorting
slices, and runtime override pointers. It immediately discards those fields
and retains only revision, effective limits, desired network values, encoding,
hardware selection and execution values. Those retained fields have no mutable
references. Management permits up to eight subtitle languages and 32 sorting
words; configured overrides add further pointer copies.

Add a small settings-owned value projection built from one atomic `current.Load`.
Keep full `Snapshot` and publication-time defensive copies for callers that
receive mutable slices or pointers. Do not return `RuntimeSnapshot` wholesale,
because it includes mutable override pointers. Keep one coherent revision and
the existing request-local snapshot semantics; separate accessor loads could
mix revisions. C07 independently addresses when hardware inspection occurs.

This is a small allocation refinement. Empty slices and absent overrides make
its benefit smaller. Verify concurrent publication does not mix revisions,
management results remain independent copies, and configured request allocation
counts change before quantifying savings.

### C04: Select maintenance candidates before sorting

Evidence: `internal/transcode/manager.go:1120-1126,1263-1280,1532-1561,1639-1674`.

Each artifact reader release signals maintenance. A non-periodic wake copies
and sorts all retained jobs, then skips jobs that do not need an initial
readiness inspection. Retention defaults to 128 jobs and permits up to 4,096.
An already warm cache can therefore repeatedly sort its retained history even
when there is no inspection candidate. Wake signals are coalesced, so this is
per processed maintenance wake, not necessarily once per released handle.

Under the initial manager lock, collect only jobs satisfying the current
inspection predicate, using a lazy slice. Sort that subset and retain the
existing ID cursor, wrap behavior and finite fair inspection budget. Preserve
the subsequent locked identity/reclamation check before filesystem work.
Do not allocate an empty slice with capacity equal to all retained jobs.

The full final quota, timeout, failure and reclamation pass must still execute
when the inspection set is empty. Keep readable sealed producers eligible and
keep periodic audits. State changes continue to signal work; periodic ticks
cover subsequent candidates. No new persistent candidate index is necessary.

Verify warm-reader behavior, zero and mixed candidate sets, cursor fairness,
state changes between snapshot and inspection, cancellation and reclamation.
Use the existing manager maintenance/fairness tests and measure allocations
with many retained jobs. The complete maintenance operation still has a
necessary O(N) final pass; only the avoidable full-set sorting is removed.

### C05: Avoid redundant descendant aggregation work

Evidence: `internal/library/userdata_query.go:163-202,222`,
`internal/library/collections_userdata.go:42-72`,
`internal/database/migrations/0002_library.sql:19`, and
`internal/database/migrations/0006_playback_state.sql:11`.

Physical and collection descendant queries use recursive `UNION` on stable
root/item/library tuples. The later joins to `items.id` and to one user's
`user_item_data` are unique. Nevertheless, both total and unplayed counts use
`COUNT(DISTINCT leaf.id)`. The second deduplication has no additional semantic
effect for the current unique root builders and joins.

Replace those aggregates with `COUNT(leaf.id)` and its existing filtered form,
retaining the recursive `UNION`. Count the nullable leaf rather than `*`, so
empty and non-playable folders still return zero. Keep distinct roots separate,
cycle termination, ACL predicates, root exclusion, and collection set semantics.
Do not add a parent-is-folder condition: existing cycle coverage deliberately
includes a path through an Episode.

Separately, `itemPlayedSQL` uses the entire count-producing query merely to
decide whether there is a playable leaf and no unplayed leaf, before pagination.
A dedicated EXISTS/NOT EXISTS predicate is worth comparing on representative
catalogs, but it must preserve empty=false and may still require substantial
recursive work. Do not assume PostgreSQL will short-circuit the recursive
population or that two existence checks are automatically cheaper.

Check empty/all-played/mixed state, overlapping roots, duplicate playlist
entries, cycles and denied descendants. Compare plans, loops, buffers and
sort/hash work before claiming a performance benefit. Exact descendant counts
remain necessary for returned UnplayedItemCount projections.

### C06: Push explicit expected-episode IDs into the base relation

Evidence: `internal/library/expected_episodes_query.go:18-43,46-52`,
`internal/library/item_permissions.go:80-82`, and
`internal/database/migrations/0046_expected_episodes.sql:37`.

The virtual expected-episode relation constructs an `items` composite through
`jsonb_build_object` and `jsonb_populate_record`. Direct item reads and batched
permission checks then filter the resulting `i.id` with a known ID or ID array.
The ID predicate is expressed on the reconstructed field rather than directly
on the primary-key column `expected_episodes.id`. This puts roster joins,
season selection, physical-presence checks and JSON construction behind a
needlessly indirect selection. A per-series roster limit is not a global
population bound.

For these two direct lookup paths, add a private builder variant that applies
the parameterized `e.id = $1` or `e.id = ANY($1::text[])` before virtual row
construction. Retain the outer selection and authorization predicates, active
roster/source-key checks, series/season visibility, ambiguous-season rejection,
physical-episode exclusion, expected-ID semantics and `CanPlay=false`.

Leave the general `queryCatalogItems` discovery population unchanged in the
first implementation. Parent, recursive, missing, unaired and name filters
cannot simply be treated as ID predicates, and an unfiltered browse must retain
its full population. Keep values parameterized and the same authorized
transaction snapshot.

Verify direct reads and remote-Play permission coverage for present/absent,
inactive, source-replaced, denied and ambiguous expected episodes. Compare
plans on many series with a tiny requested ID set. This report does not claim
an observed sequential scan or measured index-scan improvement.

### C07: Move physical hardware inspection out of global middleware

Evidence: `internal/server/server.go:384`,
`internal/server/settings_context.go:26-34,49-56`,
`internal/server/managed_hardware.go:169-203,233-245`, and
`internal/server/managed_hardware_linux.go:42-97,101-131`.

With a valid configured AMD DeviceID, the global request settings snapshot
resolves the hardware selection. Resolution calls `checkEntry`, which performs
the complete `/dev` and `/sys` identity observation on every HTTP request,
including requests that only need a server name, an image or a static asset.
The successful inspection contains eight explicit Lstat calls, two Stat calls,
two EvalSymlinks calls and a vendor-file open/read/close; symlink resolution can
itself involve more filesystem calls. This is structural work, not a syscall
trace or timing measurement. Software-only selections without a DeviceID do
not incur this inspection.

Capture immutable settings values and the selected hardware identity at request
entry. Resolve that captured selection explicitly at hardware planning,
hardware availability/status, media diagnostics and background preview
consumers. Scalar-only consumers should not observe device files. Reuse the
result within the same consumer operation where semantics allow it; do not add
a cross-request availability cache or silently convert an unavailable selected
device to software.

Preserve `plannedHardwareAvailable`, concrete-plan `checkHardware` and HLS
admission checks. They validate actual execution after waits and against the
accepted inventory generation. A later settings change must not retarget an
existing job. Preserve all node, parent, sysfs/vendor and replacement checks
at the remaining hardware-dependent boundaries.

Verify zero hardware-inspector calls for scalar-only HTTP routes, current
availability for hardware capability endpoints, replacement/removal between
snapshot and planning/admission, diagnostics and preview paths, and the
software/AMD/Vulkan selection combinations. The GPU-free test environment can
exercise injected inspector behavior; actual AMD execution needs the existing
designated GPU verification environment.

### C08: Reuse the progressive readiness scratch buffer

Evidence: `internal/transcode/progressive_linux.go:87-101,138-158,172-192`,
`internal/transcode/progressive_ready.go:12`, and
`internal/transcode/runner_linux.go:180-198`.

Before first readiness, the observer inspects output on a 25 ms ticker. Every
positive-size incomplete prefix allocates a new byte slice, reads from offset
zero and parses it. The maximum prefix is 4 MiB. A valid but incomplete first
fragment that grows slowly or temporarily stops below the cap can cause many
similar allocations. A still-incomplete prefix at the cap is rejected, so the
cap must not be described as indefinitely repeated successful work.

Retain an observer-private scratch buffer, grow it only as needed within the
existing cap, and release it on readiness and on every terminal path. Continue
the same fresh Stat, complete ReadAt and parser invocation, including same-size
byte changes. The parser returns only readiness/error and does not retain the
input slice. Do not cache a readiness proof from size or metadata alone.

Production inspection belongs to the single ticker; finish joins that ticker
before its final inspection. The buffer needs no new shared pool, global cache,
worker or synchronization regime. Its capacity must remain bounded. The
inspection owner should release it on readiness, inspection failure and ticker
exit, and finish should release it after its joined final inspection. Cancellation
alone is not observer retirement: if the owner is still active, retain bounded
ownership rather than clearing its buffer from a concurrent callback. Preserve
WAV finalization, invalid-prefix errors and callback sequencing.

Verify slowly growing and unchanged incomplete prefixes, same-size corruption,
short reads, prefix-cap rejection, readiness, cancelled/failed finish and final
inspection. Compare allocation counts rather than claiming reduced I/O. This
is a low-priority startup-only refinement; quickly ready small prefixes have
little opportunity to benefit.

Short reads must pass only the newly read `[:n]` to the parser. Clear scratch
on inspection failure or ticker exit as well as in finish: failed process
retirement can delay finish, and must not unnecessarily prolong buffer retention.

### C09: Avoid whole-library discovery on each sidecar continuation

Evidence: `internal/server/background_preview_executor.go:84,122-134`,
`internal/server/audio_waveform_executor.go:79,115-127`,
`internal/server/subtitle_timeline_executor.go:77,113-125`, and
`internal/library/background_preview_queue.go:137-181` with the corresponding
audio-waveform and subtitle-timeline queue methods.

Each executor invocation begins with `PrepareAutomatic*`: an owned transaction
performs whole-library `INSERT ... SELECT ... ORDER BY i.id ON CONFLICT DO
NOTHING`. When a matching system-event trigger exists, a child can yield after
eight processed items and request another child. That child repeats the same
whole-library discovery, including conflict checks for already queued or
completed items. For a stable eligible population of N items and eight-item
turns, the logical candidate enumeration is repeated about ceil(N/8) times.
This is a source-level amplification argument, not an observed PostgreSQL loop
count. Each discovery also occupies the shared catalog transaction owner.

Separate a continuation over an already discovered, unchanged population from
a request that requires new automatic discovery. Existing claimable work can
then be drained without repeating full discovery on every continuation. A
queue-empty discovery gate alone is insufficient: continuously replenished
pending work could otherwise starve automatic items introduced by a new scan
or enablement change. Use a current discovery revision/trigger distinction or
another explicitly bounded rediscovery rule, and prove its behavior before
selecting the implementation. Do not infer freshness from a child-local flag.

When an empty queue needs discovery, perform it at most once in that child and
retry the claim. Before yielding or closing an exactly exhausted batch, ensure discovery
and continuation decisions cannot miss automatic items that have not yet been
queued. In particular, eight pre-existing manual requests must not hide an
otherwise undiscovered automatic population. Existing scan, enablement and
late-request events must continue to cause fresh discovery after changes.

Keep eight-item fairness when a continuation trigger exists, and the existing
drain behavior when no such trigger exists. Preserve the fence, current library
options, claim ownership, Force/revision semantics and `ON CONFLICT` behavior:
discovery must not reset completed, failed or cancelled records. A cross-run
in-memory "already discovered" flag is insufficient for restart and concurrent
catalog changes. Adding only `NOT EXISTS` does not remove repeated catalog
enumeration.

Verify 1/8/9/17-item boundaries, an exact manual batch plus unseen automatic
work, trigger removal, source scans, enable/disable, late manual requests,
restart and cancellation. Count actual discovery statements and catalog-owner
hold time. Preserve task/progress/continuation semantics while reducing repeated
discovery; do not increase batch size to hide the repeated work.

### C10: Revalidate the controller after delivery-time database waits

Evidence: `internal/server/remote_commands.go:594-652`,
`internal/identity/client_sessions.go:382-406`, and
`internal/server/websocket.go:316-345`.

`authorizeRemoteSocketEvent` first revalidates the original controller and
checks ordinary remote-control permission. It then lists the receiving client's
session, which can wait for that receiver's account or credential locks. During
the wait, an independent controller can expire, be revoked or lose control
permission. For Playstate/GeneralCommand, the function subsequently returns
true without another controller check. Play adds media queries, creating further
waits. Receiver socket maintenance and disconnecting the controller's own
socket do not revoke an event already being delivered on the receiver socket.

The socket's authorization phase has a two-second timeout. This bounds the
window but does not close it: the receiver wait can finish after controller
revocation and before the timeout. This is an ancillary permission-correctness
finding, not a proposal to remove a defensive check.

Keep the initial check. After receiver and Play item authorization finish,
revalidate the controller from the original trusted event authority and repeat
the control-permission decision. Preserve application-key/client binding.
For Play, conservatively discard if identity, role or policy changes invalidate
the actual item-authorization snapshot. Comparing only the earliest and final
controller policy is insufficient if an intermediate item query used a different
policy that subsequently changed back. Capture the relevant authorization
revision/snapshot or compose those reads with an explicit consistent authority
boundary. Re-reading an unlocked authority in the same old Repeatable Read
snapshot is not a fresh revocation observation. Keep database and transport deadlines and
do not hold database locks across socket writes. This restores a final authority
observation; it does not claim revocation and network delivery are atomic.
Revoked/expired/denied controllers should suppress their event as the initial
check already does, rather than closing an otherwise valid receiver socket.
Actual database and cancellation failures retain the existing error path.

The minimal demonstrated call-order counterexample uses independent ordinary
controller/receiver credentials and Playstate or GeneralCommand. A shared
credential's revocation can be caught by receiver checks; application keys do
not expire; and Play performs additional controller item authorization after
receiver listing. Those distinctions do not justify dropping a final controller
observation after all subsequent waits. Test each case separately rather than
claiming every command type follows an identical gap.

Existing queued-command tests change authority before calling the delivery
helper. Add a deterministic test that blocks receiver session listing after
the initial controller check, changes only the controller, then releases the
receiver before the authorization deadline. Cover revocation, expiration,
remote-control policy removal and application-key/client removal, plus Play's
item-scope checks. The finding is based on the call/lock order; the race has not
been executed in this review.

### C12: Index pending media-operation polling

Evidence: `internal/server/media_operations_runtime.go:318-324,389-403`,
`internal/library/media_operations_projections.go:29-30,96-102`, and
`internal/database/migrations/0044_media_operations.sql:47-55`.

When media operations are explicitly enabled and execution is available, the
coordinator polls every 500 ms while worker capacity remains. Its pending query
selects unowned queued/applying operations and cancelled ready/interrupted
operations, ordered with cancellations first and then by creation time and ID.
The existing runnable index covers only `state='queued'`; history and publication
indexes do not cover this full selection and order. Terminal history is retained,
whereas MaxQueued limits queued work rather than total history. A small LIMIT
therefore does not itself bound the work required to find eligible candidates.

Evaluate one matching partial index in a new migration:

```sql
CREATE INDEX media_operations_pending_order_idx
ON media_operations ((cancel_requested_at IS NOT NULL) DESC, created_at, id)
WHERE worker_token = ''
  AND (state IN ('queued', 'applying')
       OR (state IN ('ready', 'interrupted')
           AND cancel_requested_at IS NOT NULL));
```

Keep the cancellation-only query's extra `publication_phase='none'` restriction,
whole-page parameter/state validation, claim-time locks and fences. Retain the
old queued index: the admission count in `media_operations_store.go` queries
queued state without the new worker-token predicate. Append a migration and
update its manifest and PostgreSQL 17 recovery catalog; do not rewrite an old
migration. The current transactional migration runner cannot execute a bare
`CREATE INDEX CONCURRENTLY` migration.

The feature defaults to disabled; a disabled runtime has no periodic idle
polling and wakes only for explicit work or its bounded retry path. This is
not a universal cost on every deployment. Compare enabled and cancellation-only
plans with substantial terminal history and mixed pending states, including
custom/generic plans, buffers, ordering, and index maintenance costs. No actual
query plan or polling speedup was measured in this review.

### C11: Avoid duplicate validation of privately encoded log lines

Evidence: `internal/diagnostics/handler.go:101-121` and
`internal/diagnostics/writer_linux.go:74-98`.

The diagnostic handler sanitizes a record and uses `slog.NewJSONHandler` to
encode one complete JSONL record into its private buffer. It is the only
production caller of the private append method. Queue admission then scans
the line again with `bytes.Count` and `json.Valid`, even though that same trusted
encoder just established the representation. Records are bounded at 8 KiB.

Document the private encoded-line contract and omit these two repeated scans
on that path. Keep the cheap nonempty/length/final-newline checks, sanitization,
the existing byte clone, queue and batch limits, cancellation, backpressure,
fallback and durable completion. Do not add a new generic raw-byte logging API
or a second production path used only by tests. If an external byte producer is
introduced later, establish validation at that producer's boundary.

The clone should remain: queue accounting charges line length, and this review
has not established an exact backing-array capacity bound for the encoder
buffer. A full slice expression does not release a larger backing array.
Removing a clone would require a separate ownership and capacity proof that
is not justified by this small refinement.

Retain handler escaping/control-character and bounded-record tests, plus
concurrent queue, rotation, cancellation and failure coverage. Adapt direct
invalid-raw-input fixtures to test the actual encoding boundary if the private
contract changes. This is the lowest-priority suggestion: it removes two short
scans and does not claim allocation or durable-write savings.

### C13: Remove remaining Windows drive-prefix assumptions

The fourth round found a distinct remaining platform assumption after the
initial executable and delivery audit. `classifyThemePath` and `classifyExtraPath`
reject any leading ASCII letter followed by a colon. `ExtraResourceItemSQL`
and the theme/extra reservation constraints repeat that rule. Under Linux,
`C:film/theme.mp3` and `C:/featurettes/clip.mp4` are relative paths whose first
component contains a colon. They are not Windows drive references.

This can affect classification, not just validation errors: without an existing
reservation, a failed auxiliary classification can leave those files in the
ordinary-media scan. F10 removed the separate reconciliation/evidence-path
restriction; it did not cover these classifiers and database contracts.

The cleanup removes only the current drive-prefix assumption, adds schema 66
to replace the two canonical reservation checks, and preserves schema 65 and
earlier extra-state validation. Historical migrations and recovery catalogs are
not rewritten. Schema 66 needs its own PostgreSQL-generated recovery catalog.
No old item ID, owner association or history is rewritten by this migration;
normal subsequent scans establish the newly recognized reservations.

Keep canonical slash-separated relative paths, nonempty components, rejection
of dot/traversal/backslash/NUL/absolute paths, non-symlink regular-file/directory
classification, anchored opening and identity checks. SQL must still enforce
the same root, library, owner/resource relationship, reserved boundary and
permanent cross-role exclusions. Those checks have Linux semantics and are
not obsolete Windows compatibility.

The classifiers, current extra visibility predicate, migration manifest and
schema 66 recovery catalog are updated. Tests cover classifier decisions,
normal/recovery migration and rollback, repeated colon-directory scans and
ordinary/direct/owner visibility, and schema 65/66 archive restoration. The
existing schema 65 migration test now lets the normal migration runner reach
the latest schema while its explicitly targeted recovery case still requires
65. Published migration files and earlier catalogs are unchanged.

Targeted remote verification passed as recorded below. The two-scan fixture
checks classification, ownership and visibility; it does not itself compare
resource IDs between the two scans. Migration and restore fixtures separately
check retained rows/identities and historical behavior.

### C14: Batch auxiliary sort-name derivation

Evidence: `internal/library/themes_scan.go:622-700,966-1003,2075-2089`,
`internal/library/extras_scan.go:526`, and
`internal/library/metadata_scan.go:25`.

`prepareAuxiliaryFiles`, shared by themes and extras, queries
`goby_generated_sort_name($1, sort_remove_words, true)` from the same settings
row once per resource. It uses the result for the preparation-time changed
decision. An owner group already has a bounded candidate/facts lifetime, so
these independent names can be derived in one parameterized query instead of
one round trip per file.

Complete each file's existing probe, preserve its source row/shared source
facts, and retire its descriptor before proceeding to the next candidate.
Batch only the retained names, using typed input and ordinals to map results
back to files. Preserve duplicate names, candidate order, full result coverage
and the existing `true` case-preservation argument. Empty groups need no query;
a missing settings row remains an error.

Only sort derivation and the dependent changed decision move. Recheck the
existing retained-facts byte budget after filling all sort names, because
that budget includes them. Query, scan, rows-error, incomplete-result and
cancellation failures must use the existing group cleanup and publish no
partial prefix. Do not keep descriptors alive to assemble a SQL batch or add a
cross-owner settings cache.

Preparation is not the authoritative final sort. Keep the publication path's
`syncScannedMetadata`/`applyAutomaticSorting`, current settings, metadata controls
and catalog-owner serialization. A settings update during probing must not be
overwritten by the earlier preparation snapshot, nor should a settings lock span
filesystem work.

Verify duplicate titles, mixed embedded/filename names, prefix configuration
changes during preparation/publication, empty and maximum groups, result/query
failures and the retained-facts limit. Measure query count on cached owner
groups. This is a P3 round-trip reduction, not an implemented change; the
pre-existing edits to `themes_scan.go` were left untouched in this task.

### C15: Avoid repeated full-prefix accounting of auxiliary facts

Evidence: `internal/library/themes_scan.go:686,935,2075-2089` and
`internal/library/scan_probe_pipeline.go:444-511`.

After each file is appended, `auxiliaryProbeFactsFit(files)` allocates a new
facts slice for all files retained so far and recursively measures the complete
prefix again. A valid 256-file group therefore represents 1+2+...+256 = 32,896
file-fact visits across these checks, including previously inspected nested
media/probe data. The 16 MiB bound limits accepted retained facts, not the number
of times their structure is revisited. Cross-root collection accumulation
also repeats the full-set check.

Evaluate group-local composable accounting for fixed prepared facts: measure
each appended member against the remaining budget and retain its charge,
including the correct top-level slice storage. Preserve exact overflow and
capacity handling, time-value treatment, conservative repeated-pointer charges,
the same rejection boundary, and a complete final budget check. Do not introduce
a global pointer cache or deduplicate shared references to enlarge accepted
populations. Start with within-group accounting before extending it across roots.
The current temporary facts slice has capacity equal to `len(files)`; charge
its header once and its member storage accordingly, rather than using the
persistent prepared-files slice's capacity. Nested slices retain their actual
capacity charges. Repeated single-member wrapper headers must not change the
acceptance boundary.

Charges cannot outlive the facts they describe. Any later name/sort or probe
mutation must update the charge or force remeasurement; C14's delayed sort-name
assignment is an explicit example. Keep early oversized-group rejection,
previous-population retention, complete cleanup and the final publication proof.
Do not remove the memory bound or defer all checks until an oversized group
has already accumulated.

Compare incremental decisions with the current full measurement for empty,
maximum, just-under/over-budget, shared-pointer and mutated-sort cases. Measure
visited facts, temporary allocations and CPU before prioritizing implementation.
This is a bounded but quadratic preparation cost, not a demonstrated leak or
an implemented optimization.

## Linux-only assessment

The general Windows server implementation was already removed by N09/L06/Q03.
C13 addresses the remaining drive-prefix assumptions found in this review.
`cmd/goby/main.go:1` and the command launcher require Linux.
`scripts/build-release.mjs:447,471` rejects non-Linux builds and selects Linux;
OCI scripts and Compose select Linux amd64. The uncommitted notices-only
packaging path independently enforces Linux and verifies its Linux artifact.

Retain the following distinctions:

- `linux && !amd64` launcher rejection, ELF architecture checks and Linux ioctl
  layouts are architecture/capability boundaries within Linux.
- `runtime.GOOS` in system status reports the host; it is not an alternate
  server implementation.
- Remaining Linux test guards are fixture restrictions already noted in prior
  cleanup records. They do not implement a Windows server fallback.
- Historical `unsupported_platform` values remain part of archived analysis
  record validation. Deleting them would reject previously valid data.
- Windows playback clients, the Windows development host, unsafe-input tests
  and upstream vendored sources are outside Windows server support.

## Review-loop ledger

| Round | Completed scope and outcome | New recommendations | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Identity/session/device authority; recovery and transcode maintenance; library user-data aggregation; request settings; production and delivery platform boundaries. | C01-C05 | 0 |
| 2 | Lock-order and immutable-publication counterexamples; cancellation/finalization; virtual expected-episode lookup; hardware-resolution consumers; progressive readiness buffers; Linux/client/archive distinctions. | C06-C08 | 0 |
| 3 | End-to-end socket delivery authority; sidecar continuation and catalog-owner work; pending-operation history queries; encoded logging boundary; Linux runtime and artifact call graphs. | C09-C12 | 0 |
| 4 | Counterexamples for all accepted findings; queue discovery starvation, authorization snapshots, buffer ownership, and auxiliary path/constraint/archive semantics. | C13 | 0 |
| 5 | Independent C13 migration/catalog/classifier/restore review and retained resource lifetimes; auxiliary preparation's per-file sorting query. | C14 | 0 |
| 6 | End-to-end authority, request/SQL/queue/I/O costs, sorting publication, retained-facts budgets, and final Linux/source/evidence correspondence. | None | 1 |
| 7 | Revocation/cancellation, empty/maximum/duplicate/cycle inputs, failed queries, and legacy path counterexamples. Maximum auxiliary groups exposed repeated full-prefix accounting. | C15 | 0 |
| 8 | Cross-review of composable accounting, sort-name mutation and final authority/source checks; cost-claim calibration and verified Linux cleanup correspondence. | None | 1 |
| 9 | Database isolation and final authority observations; cost/scale limits, dynamic settings, cancellation-to-retirement ownership, and legacy/current platform contracts. | None | 2 |
| 10 | Final actual-caller, transaction/clock, private-ownership, failure-to-close and Linux migration/source/test/closeout correspondence review. | None | 3 - stop |

## Rejected simplifications and verification boundary

Retain fresh permission/expiry checks after waits, publication/source identity
proofs, bounded worker/queue accounting, and actual descriptor/process
retirement. Similar-looking checks protect different state transitions.
Session list policy reuse does not authorize removing its final clock check.
The settings projection does not authorize exposing mutable store references.

The two Dolby Vision probe passes provide different proofs. A single input with
multiple FFmpeg outputs might share demuxing, but equivalent error classification,
stderr interpretation, EOF and retirement behavior has not been established.
It is not accepted as an actionable recommendation in this review. Parsed HLS
subtitle reuse is already an optional part of F01 and is not counted again.

## C13 verification and resource closeout

No tests, builds, validation suites, smoke checks or runtime probes ran locally.
All executable verification used `ssh test-env`, Go 1.27.1, `CGO_ENABLED=0` and
`GOMAXPROCS=2`. The schema 66 recovery catalog was generated on PostgreSQL 17.11.

| Remote scope | Top-level passed | Failed | Skipped |
| --- | --- | --- | --- |
| Selected database migration/manifest tests | 17 | 0 | 1 optional observation |
| Selected backup/catalog/restore tests | 17 | 0 | 0 |
| Selected library classifier, theme/extra scan and anchored-path tests | 33 | 0 | 0 |
| Linux `cmd/goby` build | Passed | 0 | Not applicable |

The 67 passing tests exclude subtest counts. The one skip is
`TestTaskRunsCompletedIndexPostgreSQL17Observation`, whose explicit performance
observation switch was not selected; it is not a skipped migration or cleanup
regression. This is targeted C13 verification, not a full backend-suite pass.
The checks listed under C01-C12/C14-C15 remain proposed validation for future
implementation; no performance gain or AMD execution is established here.

Ordinary runs reused `/root/.cache/go-build` and `/root/go/pkg/mod` without
copying or clearing either cache. Compiler scratch and expanded source were
under `/tmp/goby-backend-review-convergence-20261009`; native execution bound
both `GOTMPDIR` and `TMPDIR` to the task's ext4 fixtures under `/opt`.
The initial 256 MiB shared-cache growth budget was explicitly increased to
384 MiB after fresh capacity observations, before admitting the library build.
Actual growth was 359,759,872 bytes. The 500 MiB persistent-space floor was
maintained; compiler/source scratch stayed below its 1 GiB tmpfs budget.

At closeout, fresh liveness inspection found no task workers. The exact owned
compiler directory contained 206,782,464 allocated bytes and was reclaimed.
Expanded source trees were reclaimed only after archiving and verifying the
actual tested source against its manifest. The source archive, four owned
databases and raw evidence remain retained; shared caches and other tasks were
not cleaned. Final persistent availability was 659,279,872 bytes; tmpfs
availability was 3,061,137,408 bytes and available memory was 6,306,689,024 bytes.
Those observations are separate from measured reclaimed allocation and can
include concurrent activity.

Retained tested source:
`/opt/goby-backend-review-convergence-20261009/source-final-tested.tar.gz`.
The corresponding local archive is under
`.artifacts/backend-review-convergence-20261009/`. Its SHA-256 is
`52a871de28e09ce8ee79c62ff0c13b26e54989539e2218a8f021a87e03a7a58e`.
The final workspace export matched all 2,791 manifested Go/SQL/JSON/module/helper
entries, with no differences. This manifest describes the selected verification
source, not unrelated documentation or frontend artifacts.

Raw evidence and receipts are under
`.artifacts/backend-review-convergence-20261009/remote-evidence/`, including
`test-summary.json`, per-package test logs, `build-production-result.txt`,
`final-source-manifest.json`, `workspace-source-correspondence.json`,
`capacity-budget-revision.txt` and `closeout.json`.

