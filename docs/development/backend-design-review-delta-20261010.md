# Backend design review delta, 2026-10-10

## Scope and evidence

This review examines permission complexity, excessive defensive work, backend
performance architecture, and the Linux-only server boundary at HEAD
`d192ec55`, including the pre-existing dirty working tree. Existing scan,
media, playback, release, and research changes are preserved. This task adds
this review report; it does not implement the optimization recommendations.

Four parallel review tracks and the coordinating review inspect production
callers, consumers, invariants, and counterexamples. Findings are deduplicated
against the R/N/L/Q/S/F/C/T/U/V/W reviews and their implementation records.
Another caller of an existing root cause is follow-through, not a new finding.
In particular, the W recommendations and the independent administrator-read
follow-through are already implemented in the current HEAD.

The evidence is static source inspection. Counts below describe executed code
paths, allocations implied by copies, or SQL input cardinality; they are not
measured latency, throughput, WAL, or end-to-end performance improvements.
Suggested remote checks are implementation acceptance criteria, not executed
test results.

## Findings

| ID | Priority | Area | Recommendation |
| --- | --- | --- | --- |
| X01 | P2 | Catalog write amplification | Reuse existing entity IDs without updating their unchanged names. |
| X04 | P2, conditional | Preview generation | Share one successful source-frame plan across the three widths in one build. |
| X02 | P3 | Permission-check projection | Read only target revisions during notification delivery checks. |
| X03 | P3 | Defensive copying | Remove the temporary event-data copy before synchronous JSON encoding. |
| X05 | P3 | Redundant SQL input | Omit the full removed-ID array from music ancestor discovery. |

No new permission model or authorization cache is needed. X02 reduces data
read by a necessary check; X03 removes a copy that creates no additional
ownership boundary. X01 addresses repeated writes even when entity
synchronization itself is necessary. X04 warrants a controlled comparison
before committing to its larger media API change.

### X01: Reuse existing catalog entity IDs without same-value UPDATE

`internal/database/migrations/0025_music_artists.sql:76-84` is the current
definition of the entity insertion part of `sync_catalog_item_entities`.
For each distinct entity, it attempts an INSERT and uses
`ON CONFLICT ... DO UPDATE SET name = catalog_entities.name` when the full
normalized name matches. This obtains `RETURNING id`, but also updates the
existing row. The attempted insert allocates an identity value, and the
UPDATE invokes the name-hash trigger defined at
`internal/database/migrations/0004_catalog_entities.sql:23`.

The production path is `syncScannedMetadata`
(`internal/library/metadata_scan.go:147`) through `writeEffectiveMetadata`
(`metadata_store.go:445`) and `syncItemEntities` (`entities.go:269`). New media,
changed effective projections, and forced entity repair reach this path.
Administrator metadata edits and provider publication also use it. The
ordinary unchanged warm-scan path already skips entity synchronization and
is not included in this finding.

An invocation with K distinct entities already present performs K same-value
entity UPDATEs. New tracks sharing artists/genres and new movies sharing
genres/studios/people therefore rewrite shared dimension rows even though
their names and IDs are unchanged. This is distinct from the earlier
unchanged-directory/item-projection optimization: these calls still need to
build or repair the item's associations.

Batch-resolve exactly matching existing entities within one synchronization
invocation in the same transaction and send only missing entities through the
insertion path. Do not enlarge the transaction across items. The original conflict
path can remain for races on the missing subset. Retain complete association
results rather than merely removing the UPDATE clause: `DO NOTHING RETURNING`
does not return existing IDs, and a same-statement fallback SELECT can miss a
concurrent conflict row outside that statement's snapshot.

Keep the `(kind, normalized_hash)` lookup and the complete normalized-name
comparison. Digest collisions must still fail. Preserve stable IDs, the first
stored spelling, `created_at`, Artist-before-AlbumArtist first-insert priority,
Person's multiple credits, credit groups, positions, roles and sort orders.
Keep PostgreSQL's `lower`, `btrim` and hash rules rather than recomputing name
normalization in Go. Empty metadata must still remove old associations.
Keep association rebuilding, entity repair, current transaction ownership,
foreign-key protection, and rollback behavior. Avoid a process-wide entity
cache or any assumption that a numeric ID remains authorized across requests.
Existing-entity lookup also needs appropriate row protection or equivalent
failure semantics for concurrent rename/delete and supported isolation levels;
the production owner is not a universal lock for direct SQL callers.

Implement this through a new migration and a reviewed manifest append.
`internal/database/migration_manifest.go:23` makes published SQL immutable;
the migration runner applies only the unapplied suffix, and backup/recovery
also depend on historical SQL and catalog identity. Do not edit migration
0004 or 0025 in place.

Remote acceptance should count attempted entity UPDATEs for shared-entity
new imports, metadata edits and forced repair. Measure WAL and owner hold time
separately. Cover existing/missing mixtures, case and whitespace equivalence,
duplicate credits, concurrent inserts, rollback, collision rejection, stable
IDs, concurrent rename/delete, transaction isolation, and upgrade/restore paths.
No measured saving is claimed here.
Include an all-new, unshared-entity workload: the preliminary lookup adds work
there, so the benefit must be evaluated against the actual entity sharing ratio.

### X02: Narrow the target query used by notification delivery checks

`internal/notifications/runtime.go:418-426` calls `currentTarget` and uses only
the returned registration and transport revisions. The full query at
`internal/notifications/queue.go:44-46` also transfers endpoint, allowed
networks, two encrypted secrets, their generations, and identity fields.

`deliver` needs the complete target once. Subsequent checks occur before
delivery, after connection/TLS work, and in the 250 ms observer at
`runtime.go:341-348`. Up to four active senders can repeat those reads while
waiting for receivers. The field limits keep the waste bounded; this is a
small projection optimization, not a proven database bottleneck.

Use a private two-revision result or a private revision-matching query for
`checkDelivery`, retaining the complete initial delivery read. A generic
projection-options framework is unnecessary.

Preserve every current JOIN and WHERE condition: registration and transport
enabled state; session, user, device and kind binding; revocation; expiry
against the database clock; and account disabled state. Preserve no-row to
`identity.ErrUnauthorized` and other database errors to `ErrUnavailable`.
Do not reduce observation frequency or remove any call site, subsequent
`RevalidateSessionAuthority`, or current reference filtering. The original
target's immutable delivery configuration is still protected by its revisions.

R23 removed a surrounding single-query transaction, and N21 narrowed identity
session data. Neither removed these notification target columns. Narrowing a
similar fanout projection would be an extension of X02, not another finding.

Remote checks should exercise registration/configuration changes, disabled
transport/account, credential revocation and expiry, device mismatch, TLS
waits, SQL failure and cancellation. Compare result bytes and allocation work
under four concurrent slow deliveries to one controlled test receiver,
distinguished by registration/token, without sending to external services.
The transport endpoint is a singleton; four bounds active attempts, not
configured endpoints or the total number of target queries per second.

### X03: Remove the temporary event-data copy before JSON encoding

`internal/events/hub.go:542-545` copies `Envelope.Data` with `append` and
immediately passes the copied envelope to synchronous `json.Marshal`. The
resulting JSON is independently retained as `string(payload)` at line 562.
The public contract at lines 96-98 already disallows input mutation during
publication and permits it after publication returns.

The temporary RawMessage copy therefore does not establish an additional
ownership boundary. Removing it avoids one allocation and one payload-sized
copy for ordinary nonempty events. `libraryNotifier.run` and
`userDataNotifier.notifyPage` hold their publication mutex through this work,
so it also reduces work inside those critical sections. Encoding occurs before
the hub's own queue mutex; this is not a claim about that mutex's hold time.

Keep synchronous encoding, final payload storage, `CatalogScopes` cloning,
`Event.Bytes()` copies, message and queue limits, authority metadata, and all
delivery-time permission checks. Keep explicit UTF-8 validation and Marshal's
JSON validation. Nil input must still encode as null, while non-nil empty or
malformed RawMessage must still be rejected. Concurrent input mutation remains
unsupported; the removed copy did not make such a race safe.

`internal/events/hub_test.go:263` already covers modifying the producer's input
after publication and modifying one recipient's byte slice without affecting
another. Reuse this and existing malformed-data, scope, and queue-limit tests
on the remote environment when implementing the deletion. No new public API
or copying framework is necessary.

### X04: Share the source-frame plan within one preview build

`internal/server/analysis_preview_generation.go:83-88` requires the three
widths 240, 320, and 400. `generateAnalysisPreview` calls the extractor once
per width at lines 208-213. Its production closure uses the same file, source
information, selected stream and bound extractor at lines 66-73.

Each `ExtractPreviews` invocation first performs a complete source decode to
create the hold plan (`internal/media/analysis_preview.go:119-125`), then
performs another complete decode to emit that width (lines 129-141). A
successful three-width build thus executes six full preview decodes. This
count excludes additional tool validation and geometry probes.

The source pass has no target-width/height scale; that filter is added only
when a proof exists (`internal/media/analysis_preview_hold.go:190-194`). Its
private proof contains source ordinals, PTS/slot selection, frame and packet
counts, and a source-metadata trace. The nominal interval, source geometry,
stream, duration and source/tool identity are common to the three variants.

A bounded helper for one build can compute this successful source plan once
and use it for three independently audited output passes: four full preview
decodes instead of six. Keep the proof private to the build's source/tool
ownership. Do not introduce a persistent cache, reuse a caller-supplied proof,
or generalize this into a multi-output FFmpeg pipeline as part of this change.

Preserve current source and tool checks at each relevant boundary, executable
capability checks, actual EOF and successful process/reader retirement, and
each output pass's full trace, PTS, packet, selected-frame and raster checks.
The trace is source metadata, not a pixel-content digest. A previous successful
pass cannot excuse a later pass's failure or change in evidence. Preserve
borrowed JPEG handling, geometry/byte limits, cross-variant timeline equality,
temporary-file ownership and all-variant publication/final authority fences.
On the native command-domain path, each command must still obtain its current
approved executable use; holding a successful proof or an unchanged file
descriptor does not replace that permission. A later native executable
approval revocation must still prevent that command from starting.
Keep the existing task-worker approval contract separately: approval is
captured per execution, while cancellation, durable worker ownership and
publication identity remain live. This recommendation does not add repeated
human-account authorization to an already approved worker.

Budgeting needs explicit design: the current frame/packet/diagnostic counters
apply cumulatively to each two-pass extraction. Blindly sharing that counter
across four passes would reject previously admitted sources; resetting it
without charging the shared audit could hide work. Preserve each variant's
logical admission limits, account for all actual passes, and keep the common
build deadline and bounded proof lifetime. Preserve the separate per-variant
timeout where it is tighter than the outer build deadline; do not substitute
one shorter batch deadline.

Proof retention and process admission have different lifetimes. Today the
single global analysis slot is released after each extraction, before BIF
assembly and temporary cleanup. Retaining a proof must not silently extend
that slot across those inter-variant stages or recursively acquire a slot
already held by the build helper. Re-admit output work at the appropriate
boundary, and retain real process/pipe/callback ownership until it finishes.

This is distinct from V02's cross-child credits fingerprint cache, S02's
scratch-file deletion batching, Q07's BIF thumbnail decode, and N22's ready
preview validation. It targets repeat source planning inside one generation.

Before implementation, compare representative short/long and variable-frame-
rate media using the pinned remote toolchain. Include held first/last frames,
nonzero start times, rotation/SAR geometry, truncated sources, tool/source
changes, cancellation, failed later variants and cleanup failures. Compare
selected timelines and complete output artifacts, actual decode counts,
CPU/I/O and build time. The structural 6-to-4 count is not a one-third latency
improvement claim.

### X05: Omit the removed-ID array from music ancestor discovery

`internal/library/scan_reconciliation_music.go:37-53` constructs the initial
frontier after excluding every ID in `removed`. Subsequent frontiers likewise
skip `removed[parentID]` at lines 105-107. Every queried frontier therefore
contains only IDs outside the removed set.

The ancestor query at lines 70-77 already restricts rows to
`i.id=ANY($1::text[])`, but also binds the complete removed-ID array and tests
`NOT (i.id=ANY($3::text[]))`. The second predicate cannot remove another row.
Its redundancy follows from the ID sets in that same statement, not from
assuming an earlier database snapshot remains current.

For example, deleting one track from each of 1,024 surviving albums produces
16 first-level ancestor queries at the existing 64-parent batch size. With
1,024 removed IDs, this binds 16,384 excluded-ID occurrences. At 32 ASCII bytes
per ID, that is 512 KiB of repeated ID content before array/protocol overhead.
The shape is below the current 32,768-item bounds. This is an illustrative
structural count, not a measured packet size or elapsed-time result.

Remove only the redundant ancestor predicate and parameter. Keep the removed
map, exclusion while constructing each frontier, identifier/oversized-parent
checks, library and ordinary-item predicates, sorting, visited-set behavior,
budgets, cancellation and exact pre-deletion validation.

The descendant-member query at
`internal/library/metadata_music_scan.go:362-365` still needs its exclusion
predicate: it discovers child IDs that have not already been filtered in Go.
Keep that query and the shared excluded array. Repeated member-page input is
only a measurement lead; a temporary table or new cache is not justified by
this small ancestor-query simplification.

Remote acceptance should compare results for surviving and removed parents,
shared ancestry, missing rows, cycles, cross-library links, invalid parent
identifiers and cancellation. Count bound argument content on the many-album
shape separately from SQL count and query duration. S06 batched nearest-album
discovery round trips; X05 removes redundant input from a different, already
batched deletion-preflight query.

## Permission and Linux boundary assessment

Current account/session/application-key distinctions, actor-versus-target
checks, shared authority locks and final checks after real waits serve
different correctness purposes. No independently justified permission-model
layer deletion was found. Known same-snapshot parsing and broader delivery
revalidation follow-through remain assigned to their existing findings.

No additional Windows server implementation was found to delete. The Goby
server entry and command launcher have Linux build tags, release compilation
fixes GOOS to Linux, and the native fingerprint build and OCI artifact checks
reject unsupported targets. `commanddomain/launcher_other.go` rejects unsupported Linux CPU
architectures; it is not a Windows implementation. The production
`runtime.GOOS` use reports actual host status.

Retain Windows client metadata, Windows development-host utilities,
historical fixtures/migrations/archive semantics, upstream source macros, and
Linux kernel/architecture capability checks. Earlier changes already removed
the Windows telemetry implementation, non-Linux stubs and obsolete diagnostic
explanations. No platform cleanup was fabricated just to produce a code diff.

## Review-loop ledger

A completed round counts as empty only when all tracks and the coordinating
review add no independent actionable recommendation. Refinements and rejected
hypotheses do not increment the finding count. A new conditional optimization
also resets the consecutive-empty-round counter.

| Round | Focus | New findings | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Current authority, resource ownership, query/write paths and platform inventory; deduplicate previous reviews and implementations. | X01-X03 | 0 |
| 2 | Concurrency/collision and revision-query counterexamples; preview source/output passes; exact removed-ID/frontier relation and historical migration identity. | X04-X05 | 0 |
| 3 | Cross-check report against callers; concurrent entity conflicts, singleton transport, per-command approval, decode-slot/deadline lifetimes, malformed event input and exclusion invariants. | None | 1 |
| 4 | Empty/maximum inputs, changing accounts/configuration, automatic disable without revision change, failed later preview variants, SQL/isolation/cancellation errors, exact source line references and historical restore formats. | None | 2 |
| 5 | Final reverse production-call audit, policy-to-delivery/commit chains, task approval versus native executable use, actual resource retirement, disabled/empty/failure paths, cost-claim calibration and Linux/source correspondence. | None | 3 - stop |

All four tracks and the coordinating review completed rounds 3-5 without an
independent new actionable recommendation. The requested exit condition is
satisfied after five rounds: 3, 2, 0, 0, 0 new findings respectively. This is
convergence for the reviewed scope, not proof that all future optimizations
or defects have been exhausted.

## Verification environment

No local tests, builds, formatting, validation suites or runtime probes were
run. The remote capacity policy was read before read-only SSH inspection of
`test-env`. That inspection found approximately 22 GiB free on persistent
storage and 4.3 GiB available memory. The pinned
`/opt/goby-toolchains/go1.27.1/bin/go` reported Go 1.27.1; the default
`/usr/bin/go` reported Go 1.26.7. Both reported the ordinary shared build/module
cache paths `/root/.cache/go-build` and `/root/go/pkg/mod`, with no explicit
GOTMPDIR/TMPDIR from `go env`.

No test or build was run for this static review. No verification source copy,
database, worker, private cache or scratch directory was created. There was
therefore no task-owned compiler output to reclaim, and no shared or unrelated
resources were cleaned. Remote reachability/toolchain inspection is not a
tested-source receipt.
