# Backend design review and Linux-only cleanup, 2026-10-09

Implementation follow-up: `backend-design-implementation-20261009.md` records
the authorized fixes, implementation reviews, remote verification and delivery.
The findings below retain the source and status observed during this review.

## Scope and evidence

This review starts from `4509e3be` and the existing working-tree changes in
`D:\Code\goby`. It examines permission complexity, excessive defensive work,
architectural performance, and the Linux-only backend platform boundary.
The previous 30 recommendations and their implementation record in
`backend-design-review-20261008.md` and `backend-design-fixes-20261008.md` were
checked before accepting additional findings. They are not counted again.

Findings below are supported by static call-path, query, and synchronization
analysis. Operation counts are derived from source, not measured latency or
throughput. Only the Linux-only cleanup is implemented in this task. Other
findings are recommendations, not claimed fixes. Pre-existing edits are
preserved, including the scan, native-media, and HLS work.

Review rounds are sequential: each round incorporates the preceding round's
accepted findings, searches additional paths, and challenges proposed changes.
Additional examples or safety constraints for the same change do not count as
new recommendations. The stopping rule is three consecutive complete rounds
without a new accepted recommendation. Twelve rounds were completed; rounds
10, 11, and 12 added none across all four review axes, satisfying that rule.
This is convergence within the inspected scope, not proof that no further
optimization exists.

## Assessment and priority

The remaining opportunities are local: reduce the lock scope for an operation
that does not revoke credentials, batch data already belonging to one request
or transaction, avoid private payloads in explicitly narrow reads, and eliminate
repeated work on the same verified observation. The evidence does not call for
a new permissions framework, cross-request authority cache, or service split.
There are 21 pending recommendations (ten P2 and eleven P3), plus the completed
Linux-only cleanup N09. The recommendations have not been implemented or
performance-benchmarked in this task.

| Requested area | Accepted recommendations |
| --- | --- |
| Permission complexity | N01, N18, N21; preserve fresh authority and actual wait boundaries |
| Excessive defensive work | N03-N05, N11, N14-N15, N17, N19; N10 also removes repeated decoding |
| Architectural performance | N02, N06-N08, N10, N12-N13, N16, N20, N22 |
| Linux-only backend | N09, implemented and remotely verified as recorded below |

Prioritize the page/input amplification and repeated image work in N02, N10,
N12, N16, and N20, plus cancellation propagation in N04. N06 has a clear
head-of-line problem but needs more concurrency/retirement validation than the
local batching changes. Lower-priority allocation, syscall, digest, and
failure-path refinements should follow observed workload needs. These priorities
are based on structural cost and implementation scope, not measured speedups.

## Findings

### N01 — P3: Separate shared-device lookup, rename, and credential retirement

Evidence: `internal/identity/application_key_devices.go:98-169,206-251`;
`internal/identity/application_key_clients_activity.go:25`;
`internal/server/devices.go:96-100`.

The shared application-device helper always takes the global management lock
and locks every credential and key sidecar in the selected device generation.
Renaming only `custom_name` passes `mutate=true`, so it acquires `FOR UPDATE`
on all those credentials. Authentication needs a conflicting credential SHARE
lock. A slow rename or its audit work can therefore block unrelated keys in
that shared generation. Even an ordinary device lookup enters this helper
before falling back to the ordinary device registry.

Use explicit lookup, options, and delete modes. Lookup needs actor locks and
the target projection, retaining the target-device SHARE lock unless generation
and tombstone are resolved in one equivalent protected observation; options
needs management serialization, actor SHARE
locks, and target-device UPDATE; only deletion needs the full-generation
credential retirement locks. Preserve account/credential/sidecar/device lock
ordering, current authorization after waits and before commit, and the
`ErrApplicationKeyDeviceRemoved` barrier preventing alias fallback into a
different generation. This is a remaining device-helper issue, not a repeat
of the already-fixed API-key metadata list path.

Validation: concurrent rename and authentication of another key in the same
generation; self-delete and cross-key delete; actor revocation while waiting;
deleted-generation aliases and ordinary-device fallback.

### N02 — P2: Batch the analysis administration inventory projection

Evidence: `internal/library/analysis_admin.go:125-168`;
`internal/library/analysis_detections.go:399-519`.

After selecting a page of IDs, `ListAnalysisItems` calls
`readAnalysisAdminItem` serially for each item. Source, library kind, name,
intro decisions/detection, and preview queries amount to seven SELECTs for a
Movie/Episode with no detection row, excluding page/count/authorization.
Current detection records can add settings, support, season population, and
library-policy reads, reaching eleven SELECTs on the described Episode path.
Shared settings and season facts are read repeatedly in the same transaction.

Build a page projection and batch related records, deduplicating settings,
library, and season facts. Keep the repeatable-read snapshot, deterministic
page order, stored-result validation, support/cohort semantics, and the final
fresh administrator check. An inventory must still not present a detected
interval as physically validated playback evidence.

Validation: mixed source types and detection states, shared seasons, corrupt
stored results, revocation during a page read, and SQL count per page.

### N03 — P3: Avoid full backup inventory audits while deletion is busy

Evidence: `internal/recovery/backup_jobs.go:326-334`;
`internal/backupstore/store_linux.go:211-230,411-428`.

Deletion retries `ErrBusy` every 250 ms. Each retry performs `healthy()` and
enumerates the backup directory under `Store.mu` before checking whether an
owned reader, writer, or protection reference still blocks deletion. A slow
download can cause roughly four unnecessary inventory passes per second for
the duration of the wait.

Keep context/closed/degraded checks, `healthyIdentity` checks of the root and
fixed marker/lock/catalog roles, and the expected-digest comparison before a
cheap busy result. Do not add a new payload-open check to that no-op branch.
Perform the complete
inventory audit under the same mutex when deletion is actually ready to
persist its deleting marker. Keep exact unlink identity, journal ordering,
fsync, and real resource retirement. This deliberately allows discovery of an
unknown directory entry during a busy wait to move to the next complete audit;
it does not relax the audit immediately before mutation.

Validation: long-lived readers/protection, cancellation, digest mismatch,
directory replacement, unexpected entries at the eventual deletion boundary,
and metadata syscall counts while busy.

### N04 — P2: Preserve cancellation through lifecycle health reads

Evidence: `internal/lifecycle/store_linux.go:296-338,404-413`;
`internal/lifecycle/generations_linux.go:35-94`;
`internal/recovery/manager.go:127-135`.

`Current()` replaces the caller's budget with `context.Background()`. Recovery
health checks call it while holding the manager mutex. A cancelled management
request can consequently continue waiting for lifecycle admission and reading
and hashing every historical generation, holding up other manager operations.
The format allows 4096 generations and 1 MiB configurations; this is a
theoretical bound, not a measured workload.

Add a context-aware State-only reader and propagate the request or owned-job
context through health checks. Preserve full verification and CAS semantics;
do not substitute a configuration/master snapshot that reads more secret
material. Propagate cancellation and deadline errors distinctly from state
conflicts, including temporary-file verification. Cancellation is not proof
of storage corruption. Actual descriptor close and post-publication recovery
obligations must still complete under their appropriate ownership lifetime.
Cancellation can bound gate waits and checks between reads; it cannot interrupt
an already executing kernel call or make ordinary mutex acquisition cancellable.

Validation: cancellation while queued and during a historical-generation read;
no false conflict/degraded state; unrelated management progress afterward;
unchanged journal and generation-tamper detection.

### N05 — P3: Allocate I/O admission channels only for queued requests

Evidence: `internal/primaryio/admission.go:361-398,417-437`.

Every `acquirePrepared` allocates a ready channel, even when the request is
granted immediately; the fast path closes it and selects from it immediately.
`tryAcquire` also allocates and closes a channel that it never receives from.
This occurs per actual I/O admission, including 32 KiB streaming chunks.

Allocate `ready` only before appending to `waiters`. Use a lock-local queued
decision for the fast return, retain the post-unlock cancellation check and
release compensation, and leave the queued select unchanged. Do not read
`r.granted` after unlocking to choose a path: another dispatcher can change it.
All remaining ready-channel closes must address queued requests. Preserve
capacity accounting, compound-route fairness, FIFO fences, and per-chunk
admission; this is only an allocation simplification.

Validation: uncontended allocations, queued cancellation/grant races, governor
close, and existing fairness/resource-accounting regressions.

### N06 — P2: Let notification destinations progress independently

Evidence: `internal/notifications/runtime.go:134-171,185-192`;
`internal/notifications/queue.go:361,407-408`;
`internal/server/userdata_notifier.go:150-180,184-250`.

The webhook loop calls `deliver` synchronously, with a ten-second request
deadline, before it can claim another destination or return to maintenance and
fanout. A slow destination therefore blocks unrelated registrations. The
separate user-data notifier has the same scheduling limitation: one recursive
marker consumes every 64-item page, up to its fifteen-second deadline, before
another user's marker is served. These are separate queues with the same
destination-fairness recommendation, counted once.

First separate webhook maintenance/fanout from its blocking sender. If load
justifies it, use small bounded cross-registration concurrency; user-data work
can yield between pages while retaining per-user order and dirty-marker
coalescing. An unfinished marker remains active across a page yield so a new
commit marks it dirty even if its item has already passed the page cursor.
Avoid a general worker framework or unbounded goroutine fanout.

Database ordering and lease state alone are insufficient. Maintain one real
unretired sender per registration, and never overwrite an existing delivery's
active-attempt entry when maintenance expires its database lease. The existing
transport can still be joining workers after cancellation. Close and fencing
must wait for all actual workers, preserve pre-body and periodic authorization,
generation checks, retry limits, overflow resync, and publication order.

Validation: one stalled destination alongside a fast one; expiry while a
transport is still retiring; concurrent revocation/config edits; Close;
recursive user updates alongside another user's progress and overflow.

### N07 — P3: Separate history retention from fast notification maintenance

Evidence: `internal/notifications/runtime.go:134-143`;
`internal/notifications/queue.go:404-411`.

Every 500 ms maintenance pass executes seven SQL statements, including a
terminal-history `row_number()` partition/sort and seven-day registration
cleanup, even when history has not changed. History retention has a different
freshness requirement from lease recovery and authorization cancellation.

Prune a registration's terminal history when it changes, with a lower-frequency
global/startup fallback. Keep prompt lease recovery, revoked-generation cleanup,
and source-journal capacity reclamation on the required fast path. Do not lower
the frequency of the whole maintenance batch indiscriminately.

Validation: unchanged-history SQL/plan cost, bounded history after bursts,
source capacity recovery, revoked registrations, expired leases, and restart.

### N08 — P2: Batch dynamic-subtitle artifact-retention checks

Evidence: `internal/server/dynamic_publication.go:178`;
`internal/server/dynamic_subtitles.go:335-363,819-829`;
`internal/timeshift/store.go:131,598-609,751-758`.

After segment publication, subtitle pruning checks each old media coordinate
through `RetainsArtifact`. Every call locks the global timeshift store and
calls `checkedWindow`, whose expiry pass scans all artifacts and may remove
expired files. G old coordinates and A retained artifacts can therefore cause
G passes over A artifacts for one prune operation.

Add a batch retention read for one scope/window: perform admission and expiry
once, then test the requested artifact IDs against fresh visibility/grace
facts. Preserve the non-renewing consumer lease, scope, cancellation, closed
and idle-window checks, open-reader/quota rules, and conservative retention
on inconclusive storage errors. Do not substitute a stale window snapshot for
the fresh retention decision or independently remove expiry work.

Validation: grace-boundary timing, retained readers, concurrent publication,
abandoned windows, errors/cancellation, and expiry-pass count per prune.

### N09 — Implemented: Remove Windows and other non-Linux backend scaffolding

The cleanup modifies 57 previously clean paths and removes 70, with 73 inserted
and 1357 deleted lines before this review document. The removed files contain
non-Linux backend substitutes, Windows telemetry, and tests specific to those
unsupported implementations. Linux command entry points now have explicit
build constraints; the command-domain fallback is only `linux && !amd64`.
Dead platform branches were removed from otherwise unchanged Linux paths.
Three Linux-constant platform predicates and their unreachable fallback paths
were also removed. The explicit capability-failure injection used by process
retirement tests remains, along with all actual kernel/filesystem checks.

The private intro-fingerprint helper now declares Linux as its CMake target
boundary and drops its own Windows stdio/MSVC branches. Vendored third-party
code is preserved. Current development documentation and the dashboard's
synthetic server fixture now describe a Linux backend. Unused platform-only
error sentinels and their HTTP mappings/test skips were removed; current API
and package documentation no longer promises the deleted substitutes.

Windows playback-client recognition, host development/orchestration scripts,
the independent notification receiver, and rejection of unsafe Windows-style
input paths remain. These are not Windows server support. No Linux architecture
or CGO capability was newly promised. Historical verification records are not
rewritten.

Pre-existing dirty files `internal/media/analysis_process.go:62` and
`internal/media/video_seek_process.go:122,302` still have redundant non-Linux
guards. They have no effect in the Linux build and do not supply a Windows
implementation; they were preserved to avoid modifying another work item's
source.

### N10 — P2: Reuse successful embedded-artwork inspection by content digest

Evidence: `internal/library/embedded_artwork.go:90-116,166`;
`internal/server/images.go:358,365,387`;
`internal/artwork/inspection_cache.go:25-46`.

Each embedded-artwork open hashes the database bytes and then performs a full
`InspectContext` decode. The HTTP handler opens the source before its variant
cache lookup and again before the response, so a cached response or 304 still
performs two complete image decodes. Embedded collage members use the same
reader. Scan publication already validates the image, but current database
bytes and their metadata still need verification when read.

Reuse the existing bounded successful-inspection cache keyed by the digest of
the actual current bytes. Retain size, digest, width, height, and MIME comparisons
on every call, along with source revision/publication/physical-media checks and
both authorization boundaries. Never cache permission decisions or trust the
stored tag without hashing the bytes. Variant rendering checks remain separate.

Validation: cached and conditional requests, modified/corrupt bytea and metadata,
GIF limits, collage members, source replacement, revocation, and decode counts.

### N11 — P3: Reuse the file metadata already returned by cache opening

Evidence: `internal/transcode/cache_linux.go:615-624,757-787`;
`internal/transcode/cache_maintenance_linux.go:380-387`;
`internal/transcode/live_observer_linux.go:394-401`.

`cacheOpenRegular` calls `cacheOpenRegularInfo` and discards its `FileInfo`.
The three cited inspection/budget paths immediately call `file.Stat()` again
on the same descriptor. The helper already obtained that metadata and checked
the regular-file/link constraints; only its initial seek lies between the
observations.

Use `cacheOpenRegularInfo` at these immediate call sites and reuse the returned
metadata. Preserve no-follow/nonblocking opening, type/link checks, quota/fact
calculation, close, and subsequent audits. This intentionally uses the helper's
slightly earlier observation; it must not replace a Stat after an actual read,
write, wait, or other meaningful state boundary.

Validation: inventory rename/unlink races, live scratch limits, and syscall
counts in the affected observers.

### N12 — P2: Use explicit summary and polling projections for media operations

Evidence: `internal/library/media_operations_store.go:25-30,461,502,707`;
`internal/server/media_operations_runtime.go:322,383,412-415,483-487`;
`internal/server/admin_media_operations_dtos.go:154-165`.

List, candidate, and coordinator status reads select complete operation records,
including `source_snapshot`, `execution_snapshot`, and `journal`. HTTP summaries
do not expose or consume those private documents. The active coordinator polls
status every 500 ms, while pending-operation results are used only for their
IDs before claim reads the complete operation again. Each document permits up
to 256 KiB; this is a schema limit, not a measured transfer size.

Add explicit summary, status, and candidate-ID projections. Preserve parameters,
public result summaries, worker-presence-dependent capabilities, snapshot and
administrator checks, and the default full-record contract. In particular,
remove-subtitle cancellation compensation replaces its work record with the
current record and then discards using its journal; that caller must keep a
full read. Idempotency/application receipt paths also need their private
fingerprints. Pending-page reads currently validate every row's parameters and
state before returning a candidate page. Keep those lightweight validation
fields and whole-page failure semantics instead of silently skipping a corrupt
candidate when reducing the projection to IDs.

Validation: DTO and capability equivalence, cancellation/worker-token races,
application receipts, compensation with a retained journal, and transferred
bytes per list/poll.

### N13 — P2: Batch OCR cue edits within the existing transaction

Evidence: `internal/library/media_operations_review.go:150-224`.

One review update accepts up to 200 cue edits. After reading and validating the
complete cue set and calculating its hash, the code issues one sequential
UPDATE per edit while retaining the catalog owner transaction. The edits have
already passed duplicate-ordinal and existence checks.

Use one parameterized UPDATE from a typed input relation, such as UNNEST, and
check the affected-row count. Retain the operation lock, expected revision,
whole-result validation/hash, ordinal and text limits, final administrator
check, and single commit. This changes transport/query amplification, not the
review semantics or authorization model.

Validation: 1/200 edits, invalid/duplicate ordinals, concurrent revisions,
cancelled operations, unchanged result hashes, and statement count.

### N14 — P3: Bound error bookkeeping during indefinite process cleanup

Evidence: `internal/media/process.go:296-326`, especially line 314.

When actual process retirement remains unproven, cleanup correctly retains the
owner and retries. However, each one-second retry assigns
`errors.Join(cancelErr, process.conventional.cancel())` back to `cancelErr`.
A persistent kernel identity/signal failure therefore grows a nested error
chain without a bound. Once an error exists, even later nil cancellation
results continue wrapping it. Retained memory and later error traversal/logging
grow with the cleanup duration.

Keep a bounded first/latest-error summary and retry count, joining only those
fixed slots at return or observation. Preserve the original callback/wait errors
and final join error, including their sticky repeated-Wait/Close behavior.
Preserve the unknown-retirement sentinel,
pidfd/identity, all permits, and the actual cleanup wait. Do not use a timeout
to claim retirement or release capacity while the process is still unproven.

Validation: repeated injected retirement failures, bounded retained error size,
eventual successful retirement, and unchanged ownership/capacity behavior.

### N15 — P3: Avoid poisoning lifecycle state on a proven unpublished cancellation

Evidence: `internal/lifecycle/generations_linux.go:129-139`;
`internal/lifecycle/store_linux.go:283-293`;
`internal/lifecycle/files_linux.go:196-249`.

The first StageGeneration step appends an in-memory registry entry and persists
it before creating the generation directory. If its atomic write returns
cancellation before rename, the old registry is still authoritative and no
generation directory exists, but `persistRegistry` unconditionally marks the
store degraded. A routine cancellation at this point blocks later operations
until reopen.

For this specific first, proven-unpublished step, restore the previous
in-memory registry and propagate cancellation without degrading a healthy
store. Carry an explicit publication outcome if needed to make that proof
robust. Do not globally suppress `persistRegistry` failure handling: the later
directory-identity persistence already follows mkdir and must retain its
recovery barrier. Post-rename/fsync uncertainty also remains degraded.
Manager control writes can follow a committed authorization grant and likewise
must not be indiscriminately made retryable.

Validation: cancellation before the initial registry rename, cancellation after
mkdir at the later write, post-rename fsync failure, reopen, and recovery state
equivalence. Accept only the first case as safe to continue without reopen.

### N16 — P2: Batch collection member and sharing inputs

Evidence: `internal/library/collections_members.go:15-22`;
`internal/library/collections.go:23,418,531-553,607,646`.

Collection create/add/preview accepts up to 1000 requested item IDs and queries
the type/folder facts of each input separately. Create/add retain the catalog
owner transaction while resolving them. Sharing edits likewise validate each
recipient and INSERT each share separately, although admission already locks
the participating accounts in deterministic order. The existing member INSERT
is already batched; the remaining amplification is input resolution and shares.

Fetch authorized member facts for deduplicated IDs once, then reconstruct the
result in original input order. Batch validation of already-locked recipient
accounts and the sharing INSERT. Preserve explicit playlist duplicates, folder
expansion order and limits, BoxSet rules, owner/self-share rejection, the
original first-error behavior, all ACL checks, account-lock ordering, and final
authorization. Member decisions must replay the original requested order,
including folder expansion and cumulative limits; sharing checks must retain
their existing normalized UserID order. Keep `itemPolicySQL` plus
`ordinaryItemSQL` rather than substituting a different direct-play predicate.
This complements the previously fixed collection list metadata;
it does not replace or recount that old read-path recommendation.

Validation: 1000 inputs, duplicate playlist members, folder expansion, mixed
visible/invisible or incompatible items, disabled/deleted sharing recipients,
concurrent account deletion, and query count during collection edits.

### N17 — P3: Reuse a verified recovery-control record digest

Evidence: `internal/recoverycontrol/format.go:180-185`;
`internal/recoverycontrol/files_linux.go:136,206,228`;
`internal/recoverycontrol/store_linux.go:332,398`.

`recordSnapshot` hashes its complete record bytes even though the same digest
has already been computed and verified. The Read path has `readFile`'s digest,
used in the current/proof identity comparison. CAS has the candidate digest
from temporary creation and the verified file returned by publication. Snapshot
construction consequently adds one full-record hash per Read and two per CAS
(its current record and candidate), on records bounded to 1 MiB plus framing.

Pass the already verified digest into snapshot construction. Keep the independent
payload copy and every actual file read, proof comparison, identity check, CAS,
fsync, and post-publication check. This removes hashing of the same private
bytes, not a fresh filesystem observation. It is a low-priority CPU reduction,
with no measured production latency claim.

Validation: exact digest and payload equivalence on Read/CAS, defensive payload
copying, and unchanged tamper/publication-fault regressions.

### N18 — P3: Skip unused display preferences when sorting is fully explicit

Evidence: `internal/server/user_preferences.go:268-327`, especially line 306;
`internal/identity/user_preferences_store.go:356-381`;
`internal/server/items.go:288`.

When both SortBy and SortOrder are explicit, the defaults helper still reads
display preferences, taking account/credential locks, reading and validating
the stored JSON, and rechecking preference authority. Neither returned sorting
field is used. This also lets corrupt, unused preferences fail an otherwise
fully specified catalog request.

On the ordinary-user branch that would otherwise read preferences, retain
duplicate-parameter, scope, and explicit-sort validation, then skip the
preference read if both sorting fields are present. Keep the existing bypasses
for missing client/scope, application keys, and missing query users; do not move
scope checks ahead of those bypasses. Requests missing a default
continue through the existing preference path, and catalog authentication/ACL
checks remain unchanged. The deliberate behavior change is that invalid unused
stored preferences no longer block a request that supplies both sort fields.
Do not let the shortcut accept malformed scopes or ambiguous query keys.
Detect explicit fields by their presence in the URL, not by nonempty defaults
already stored in the query struct. Keep the separate user-configuration read
that controls result-set behavior such as missing episodes.

Validation: both/one/neither explicit field, invalid scopes, duplicate keys,
invalid sorts, corrupt unused preferences, forbidden preference subjects, and
unchanged item ACLs.

### N19 — P3: Inspect inert lifecycle debris without reading its payload

Evidence: `internal/lifecycle/store_linux.go:327-330`;
`internal/lifecycle/files_linux.go:89-128`;
`internal/lifecycle/README.md` (inert temporary-file contract).

Every lifecycle verification fully reads and hashes each leftover `.next-*`
file, up to 8 MiB per file, but consumes only its Present flag. These files are
explicitly never adopted or automatically deleted. With crash debris, every
health read repeats allocations, payload I/O, and hashing unrelated to current
authority; with no debris there is no saving.

Use a dedicated metadata-only inspection for those inert leftovers, retaining
safe nonblocking/no-follow opening, ownership/mode/type/link checks, size bounds,
descriptor and named identity, root checks, per-file cancellation checks, and
bounded enumeration. Keep full
verification for registry, manifest, journal, registered generations, and the
temporary file currently being published. The deliberate tradeoff is that
unreadable data blocks inside unused debris no longer make verification fail;
this is not claimed to be identical fault-detection behavior.

Validation: safe inert leftovers, symlinks/FIFOs/hard links, oversized or foreign
files, replacement races, unchanged authority-file checks, and I/O/allocation
counts with debris.

### N20 — P2: Record provider-cache catalog changes as one batch

Evidence: `internal/library/provider_tasks.go:523-533`;
`internal/library/catalog_changes.go:128-135`;
`internal/library/notification_journal.go:14-28`.

Provider-image cache pruning already deletes a batch, but then calls
`recordCatalogChanges` once per unique item. Each call immediately refreshes
the transaction's journal: it serializes and hashes the accumulated references
and calls the notification SQL function. A legal batch of 1000 unique items
can therefore issue 1000 serial calls. Disabled transport still incurs these
round trips despite its already-fixed early return inside the SQL function.

Collect changes in the existing unique-ID order and invoke the existing
variadic helper once. Preserve non-ordinary parent handling, the later
`before.record` auxiliary changes, the same transaction/mutation identity,
reference limits, resync behavior, capacity failures, and commit/rollback.
The sequence values and gaps from intermediate uncommitted refreshes may
change. Preserve committed notification content, ordering, and cursor coverage;
consumers use actual sequence values rather than requiring contiguous numbers.
This is a local batching change, not a global change to journal durability or
notification timing.

Validation: multi-item prune, ordinary and auxiliary items, disabled transport,
reference/resync limits, capacity rollback, equivalent committed notifications,
and SQL count for one prune batch.

### N21 — P3: Give proven authority-only consumers a narrow session projection

Evidence: `internal/identity/session_revalidation.go:20,57-76`;
`internal/notifications/runtime.go:237-244,322-329`;
`internal/server/websocket.go:400-402`;
`internal/server/analysis_preview_provider.go:215-247`.

Ordinary-user RevalidateSession always selects the full user configuration and
account display fields. An active webhook sender periodically revalidates at a
250 ms interval, while websocket maintenance and published-preview credential
checks also call it. The inspected consumers use authority, identity, activity,
or expiry facts; none consumes the returned Configuration. Configuration write
paths permit relatively large documents, but no typical size or performance
benefit was measured here. This is not a global idle 250 ms polling cost;
the current webhook runtime has one sender.

Add an explicit narrow authority projection for these proven call groups.
Preserve the default RevalidateSession contract, which explicitly returns
current configuration. Keep current role/policy, local-auth and peer checks,
credential/device bindings, expiry/revocation, activity facts where needed,
and the same database/time observations. Downstream notification and preview
catalog reads still acquire their own current subject ACLs. Do not introduce
an authorization cache, reuse an old principal after a wait, or migrate other
callers without auditing the fields they actually consume.

Validation: ordinary/app-key parity for the selected consumers, large stored
configuration payloads, policy changes and expiry, local-auth peer restrictions,
client touches, preview cancellation, and SQL result bytes/allocations.

### N22 — P2: Combine ready-preview source and reference revalidation

Evidence: `internal/server/analysis_preview_provider.go:177-187`;
`internal/library/analysis_sources.go:395-439`;
`internal/library/analysis_previews.go:437-492`.

A ready published preview first calls GetCurrentAnalysisSourceFor and then
GetAnalysisPreviewsFor. The second method calls the same source verifier again.
That verifier includes two surrounding subject-read transactions and an actual
OpenMediaFor/Close chain with its own source/I/O ownership checks. Between the
two full verifiers, Revalidate only compares source identity and passes its IDs
to the next call. This repeats the full source proof during ready-preview
checks, in addition to the separate credential projection cost in N21.

Provide a local combined operation returning a physically verified current
source and its preview references. In the ready path, verify the lease's
expected source before querying references to preserve failure ordering. Keep
one complete before/open/close/after source proof, the later current-source
comparison in the reference-reading transaction, canPlay/ACL checks, settings
revision/publication epoch, cache inode/size/time checks, and final fresh
credential authorization. The not-ready path still needs its source-only
check. Do not merge OpenPreview's checks across actual source registration or
resource acquisition, and do not turn this into a cross-request source cache.
Do not hold one old database snapshot across the physical I/O. A not-ready
lease stays not-ready; this read consolidation does not upgrade its metadata.
The saved work is query/descriptor/metadata/admission work, not a full-media
payload read or hash.

Validation: ready versus not-ready previews, source/revision replacement at
each retained boundary, invalid references/settings changes, cancellation and
revocation during physical I/O, close failures, and actual source-open/query
counts per revalidation.

## Deduplication and rejected simplifications

- Do not remove authorization after real waits or before publication simply
  because another check occurred earlier. Internal scans retain their selected
  startup operation grant; external requests retain fresh authority checks.
- Do not return 304 solely from a predicted image ETag. Rendering also checks
  orientation-dependent crop bounds and GIF/output budgets. A wildcard or
  predictable validator must not turn an invalid representation into success.
- Repeated private-file `flock` calls were not accepted for removal without a
  complete filesystem/lock-recovery argument; the likely saving is small.
- Additional single-SELECT transaction and duplicate-policy parsing examples
  were not counted as new roots for the already-reviewed recommendations.

The same rule applies to remaining complete-Item projections in
`GetItemsByIDFor` (`internal/library/query.go:298`), used by NowPlaying
(`internal/server/client_sessions.go:209`) and visibility-only LibraryChanged
work (`internal/server/library_events.go:140`). These are follow-through for
the earlier R05/R21 projection recommendations, not additional root causes.
Their individual field requirements still need auditing before selecting an
existing narrow projection.

## Review-loop ledger

| Round | Focus and accepted additions | Consecutive empty rounds |
| --- | --- | --- |
| 1 | Independent permission, defensive-work, performance, and platform inventories; N01-N09 | 0 |
| 2 | Cross-review of lock/retirement safety, artwork and media-operation paths, and independent platform diff audit; N10-N15 | 0 |
| 3 | End-to-end request and mutation paths, cancellation/publication/restart review, collection inputs, and platform documentation; N16. Corrected the current transcode platform guide as part of N09. | 0 |
| 4 | Adversarial review of proposal invariants, streaming and recovery paths; N17. Platform constant-predicate cleanup remains part of N09. | 0 |
| 5 | Workload bounds, provider/default-preference paths, inert recovery debris, and current platform contracts; N18-N20. Unsupported-platform sentinel/docs cleanup remains part of N09. | 0 |
| 6 | Cross-check of all minimal changes, input/error-order semantics, notification sequence/cursor behavior, lifecycle debris, and final platform delta; no new recommendation | 1 |
| 7 | Rotated cross-review of permissions/recovery, state/configuration/startup/shutdown costs, lookup tombstones, paged notification activity, and platform entry points; no new recommendation | 2 |
| 8 | Final counterexample review discovered the independent session-authority projection N21; other axes added nothing. The global empty-round count resets. | 0 |
| 9 | Independent cross-check of N21 and adjacent source lifetimes discovered N22's duplicated ready-preview physical proof. No other new recommendation. | 0 |
| 10 | Full cross-review of the combined preview read, fresh snapshots, ready/not-ready state, prior recommendations, and frozen platform manifest; no new recommendation | 1 |
| 11 | Combined-change review against callers and existing regression contracts, including notification batching/scheduling and authority/source projections; no new recommendation | 2 |
| 12 | Final independent source/report/contract review on every axis, including retained test evidence and all proposal counterexamples; no new recommendation | 3 |

The requested stopping condition is met. No additional review round was started.

## Verification and closeout

All verification runs on `ssh test-env`; no local tests, builds, smoke tests,
or runtime probes are authorized or performed. The final Linux/amd64 backend
and command test compilation passed for all 41 packages (38 packages with
tests, three without). Empty test selection establishes compilation, not test
suite execution.

| Source snapshot | Executed checks | Result |
| --- | --- | --- |
| 1: initial platform cleanup | Complete systemstatus and config packages; managed-hardware selection | 22, 798, and 19 passing tests/subtests respectively, no skips |
| 1 | Private intro-fingerprint helper Release build and protocol tests | Build and all seven protocol tests passed |
| 2: constant platform predicates removed | Media retirement/background/cohort selection | 44 passing tests/subtests, no skips, including the explicit unsupported-capability injection regression |
| 2 | Library file-deletion/media-edit/scan-evidence selection | 62 passing tests/subtests; 16 database-dependent records skipped because no PostgreSQL test DSN was configured |
| 3: final sentinel/documentation cleanup | Application-key vault selection | 41 passing tests/subtests, no skips |
| 3 | Dynamic/application-key HTTP/local-credential selection | 129 passing tests/subtests; four real HLS HTTP cases skipped because FFmpeg/FFprobe were not configured |

Each snapshot also passed all 41-package Linux test compilation. The eleven
files added in snapshot 2 and twelve paths added in snapshot 3 have separate
manifests; unchanged-path test results retain their original snapshot label.
This is not one complete backend execution matrix. The final correspondence
check matched all 2,842 files in the retained verification-source manifest to
the final snapshot. No Windows, frontend, complete database/media, or performance
benchmark result is claimed.

Toolchains were Go 1.27.1 with CGO disabled, CMake 3.31.6, GCC/G++ 14.2.0,
and Python 3.13.5. Native filesystem fixtures remained on task-owned ext4 with
both GOTMPDIR and TMPDIR set to that fixture root.

The initial run used the shared Go build/module caches. Subsequent admission
paused when further shared-cache growth would cross the selected 1 GiB
persistent headroom floor. The task then explicitly selected an empty, bounded
tmpfs private build cache and reused shared modules without copying a cache.
Compiler scratch stayed separate from ext4 fixtures. A capacity sampler raced
normal Go temporary-directory removal; its interruption was retained and the
affected package without a completion receipt was rerun after correcting that
identified monitor condition. The initial admission stop and rejected cleanup
layout predicate are also retained, rather than presented as successful runs.

Closeout confirmed no task worker and empty fixtures. The exact private cache
held 959,176,704 allocated bytes before the pinned Go clean operation. Cleanup
reclaimed 1,256,017,920 bytes of task tmpfs including cache and scratch; shared
cache allocation stayed unchanged during closeout. Source, raw evidence, and
verified binary archives remain retained.

Evidence locations:

- Local: `.artifacts/backend-linux-review-20261009/VERIFICATION.md` and
  `retained-evidence/verification-summary.json` in the same artifact directory.
- Per-snapshot commands, manifests, passing results, explicit skips, admission
  records, and interruptions: that directory's `retained-evidence` tree.
- Exact cleanup receipt: `retained-evidence/closeout-final.json`.
- Remote source/evidence: `/opt/goby-linux-review-20261009-01`.
