# Backend design review loop, 2026-10-09

Implementation follow-up: [backend-review-loop-implementation-20261009.md](backend-review-loop-implementation-20261009.md)
records the authorized changes, verification and delivery. The review below
retains its original findings and review-time status.

## Scope and method

This review starts from `27071431` and the existing working tree in
`D:\Code\goby`. It covers permission complexity, excessive defensive work,
architectural performance, and the Linux-only server boundary. The previous
R01-R30 and N01-N22 findings and their implementation records are the
deduplication baseline; additional examples of those changes do not count as
new recommendations.

Four parallel reviewers contribute to each sequential round. The next round
receives the preceding round's findings and examines other paths or challenges
the proposed simplifications. The stopping condition is three consecutive
complete rounds with no new accepted recommendation across all four areas.
The ledger below records completed rounds, not an assumed absence of defects.
Five rounds completed. Rounds 3, 4 and 5 each added no new recommendation,
satisfying the requested stopping condition across all four review areas.
This is convergence within the inspected scope, not proof that every future
optimization has been exhausted.

Only Linux platform cleanup is implemented here. Other findings are review
recommendations. Costs are derived from source and call graphs, not measured
latency, throughput, or database query plans. Existing unrelated working-tree
changes are preserved. The two already-modified media files received only the
three obsolete platform-condition removals and their unused imports; their
pre-cleanup copies are retained under
`.artifacts/backend-review-loop-20261009/before/`.

## Assessment and implementation priority

Seven new recommendations remain: three P2 query/persistence changes and four
P3 permission, maintenance or cache refinements. L06 is the completed platform
cleanup. The opportunities are specific duplicated work and unnecessarily
strong locks; current authorization freshness and resource-retirement checks
have concrete reasons to remain.

| Requested area | Result |
| --- | --- |
| Permission complexity | L01 removes an avoidable failed credential query; L07 narrows a notification-only actor lock. |
| Excessive defensive implementation | L04 separates test-only and unintegrated ownership code; retain real source, durability and retirement barriers. |
| Architectural performance | L02 and L03 improve query shape; L08 can amortize durable log syncs; L05 avoids clearing an entire bounded metadata cache. |
| Linux-only backend | L06 removes remaining server-test scaffolding and the three previously documented dead media guards. |

Start with L02/L03 on representative catalogs, measuring plans before and after.
L01/L07 are narrower permission-path changes. L08 deserves separate concurrency
and storage-failure validation; a single bounded writer is sufficient, and
the existing durable-record contract remains. L04/L05 are lower-priority
maintenance and cache changes. These priorities reflect structural impact and
implementation risk, not measured end-to-end speedups.

## Findings

### L01 - P3: Resolve Emby credential kinds in one database round trip

Evidence: `internal/identity/application_keys.go:77-98`,
`internal/identity/store.go:273-315`, `internal/server/auth.go:97`, and
`internal/database/migrations/0001_identity.sql:25`.

`resolveEmbyWithPeer` first calls the exact-kind ordinary-login resolver with
`emby`. Every valid application key fails that query, then repeats token
digest calculation and queries the same globally unique token hash for its
application-key projection. This adds a predictable database round trip to
each application-key request before normal client binding and activity work.

Use one credential lookup statement with the appropriate kind-specific
projection. Keep the existing exact-kind `ResolveWithPeer` API. Preserve all
ordinary-user fields, disabled/revoked/expired checks, database observation
time, local-login and trusted-peer policy, and the application's userless,
non-expiring credential and default-client constraints. Native administrator
tokens must remain unacceptable at the Emby boundary. Database errors must
not be converted into attempts to authenticate as another kind.

This is distinct from N21's narrow background revalidation projection and
R02's adjacent administrator checks. Verify login/key/invalid/admin token
cases, missing key sidecars or clients, remote policy, and actual query counts.
Do not introduce a cross-request permission cache.

### L02 - P2: Aggregate InstantMix credit scores before joining candidates

Evidence: `internal/library/music_discovery_mix.go:256-301` and its page
construction at lines 101-122.

`mix_credits` materializes credits for the authorized music scope. Each
candidate then has a correlated aggregate over that materialized relation,
filtered by `credit.item_id=candidate.id`. Default score ordering requires
scoring before page limiting, so a small result page does not bound this
work. Indexes on the underlying association table do not index this CTE's
materialized result.

Join the credit relation to seed entities once, aggregate by item ID, and
left-join those scores to candidates. Preserve credit deduplication, weights,
seed and album bonuses, zero-score fallback, explicit sorting, stable page
order, and independently authorized seed/candidate scopes. Do not broaden
the ACL or merge candidate filtering into seed eligibility.

Use the existing typed-seed, hidden-source, playlist, sorting, and pagination
tests, then compare plans on a representative larger music catalog. Record
subplan loops, actual rows, buffers and temporary I/O before claiming a
speedup. The count/page duplication is not counted again as another finding.

### L03 - P2: Filter SearchHints names in their respective branches

Evidence: `internal/library/search_hints.go:158-168` and
`internal/database/migrations/0004_catalog_entities.sql:37`.

The shared `eligible_sources` CTE starts with the entire authorized source
catalog and is referenced by both the physical-item and entity branches.
The search term appears only outside their union. In the default mixed search,
the source set and its authorization work therefore start from the whole
catalog even when only a few names match.

Let the physical-item branch filter item names first. Let the entity branch
filter entity name and kind first, then prove membership through an existing
valid association to any currently authorized source. The existing
`(entity_id,item_id)` association index supports that direction of lookup.
Do not put an item-title predicate into the shared source set: an actor or
genre may match even when none of its media titles match.

Preserve MediaTypes restrictions on sources, valid association and music-artist
rules, typed identities, the transaction snapshot, literal escaping, ranking,
total count and pagination. Verify the entity/source-name independence and
ACL regressions, then inspect representative query plans. No new search
service, permission cache or extension is required by this recommendation.

### L04 - P3: Keep test-only lifecycle implementations out of production files

Evidence: `internal/transcode/manager.go:1501`,
`internal/transcode/manager_finalization.go:285`,
`internal/transcode/job_resource_owner.go:36,151`, and
`internal/library/images_store.go:31`.

The legacy synchronous finish path explicitly exists only for manually built
test jobs. `runPublicImageWorker` likewise has only test callers. The alternate
execution-slot/job-resource-owner constructors are exercised only by tests;
the production manager maintains its own execution counters and completion
tickets. Keeping these parallel implementations in production files adds
maintenance and ownership ambiguity without exercising them in production.

Move proven fixture helpers into `_test.go`. Keep an intentionally staged
ownership prototype explicitly separated as test/experimental work until it
is integrated. This is a code-organization recommendation, not a claim of
runtime savings or permission to delete planned native behavior.

Preserve `publicImageWorkers`, which real image reads use, and move the
production-used `errJobResourceOwnership` sentinel if necessary. Preserve the
actual completion tickets, finalization executor, resource lifecycle context,
runner integration, and unknown-retirement handling. Compile production and
package tests separately to verify the boundary after any future move.

### L05 - P3: Evict one image inspection entry instead of the complete cache

Evidence: `internal/artwork/inspection_cache.go:69-70` and
`internal/library/embedded_artwork.go:107`.

At 512 successful digests, the next insertion clears every entry. A newly
seen cold image therefore invalidates the other 512 metadata records,
including a cover just used by another request. Subsequent requests decode
those images again. N10 expanded use of this cache to embedded artwork but
did not change its eviction behavior.

Keep the same fixed capacity and use single-entry FIFO/clock eviction; use
LRU only if measured locality warrants its additional bookkeeping. Preserve
full fresh input hashing, shared decode slots, cancellation, successful-only
metadata storage, and all independent authorization/source checks.

Verify capacity, duplicate concurrent inserts and mixed hot/cold accesses.
This does not promise cache hits for every cyclic scan larger than capacity.

### L06 - Implemented: Finish removal of Windows server test scaffolding

Five library test files retained Windows skips, alternate fixtures or
platform-specific expectations after the previous server cleanup:
`root_binding_paths_test.go`, `root_binding_registration_test.go`,
`server_directories_integration_test.go`, `subtitle_timeline_external_test.go`,
and `theme_paths_test.go`. Those branches and unused imports are removed;
Linux assertions remain unchanged, including symlink escape and open-directory
replacement cases.

The three obsolete non-Linux guards in `internal/media/analysis_process.go`
and `internal/media/video_seek_process.go` are also removed. They were already
documented as N09 carryover, so they do not count as another new finding.

The `linux && !amd64` command launcher remains a Linux architecture boundary.
OS reporting, Windows playback clients, host-side development scripts,
independent research tools and unsafe-input path rejection are outside the
obsolete Windows server implementation and remain intact.

### L07 - P3: Use shared credential locks for notification mutations

Evidence: `internal/identity/notification_secrets.go:49-54`,
`internal/identity/client_sessions.go:390-397`,
`internal/notifications/queue.go:318-325`, and
`internal/library/playback_media_authorization_batch.go:20-21`.

The notification helper passes its `lock` boolean into a client-session
helper's `mutate` parameter. Registration creation/update, deletion and test
delivery therefore acquire `FOR UPDATE` on the actor's login credential even
though they write only notification tables. Test delivery can then wait for
the global journal lock while retaining that credential lock. The same
session's media authorization needs `FOR SHARE` and is unnecessarily blocked.

Use shared account and credential locks for this notification authorization
path and remove or clarify the misleading boolean. Retain registration and
journal write locks, the management lock's vault/deletion coordination,
account-before-session ordering, and fresh expiry/policy checks after waits
and before commit. Shared locks still exclude actual credential revocation
and account modification. Do not apply this change to real session mutation.

Verify that blocking a registration or journal update does not block the same
session's media authority read, while revocation and concurrent registration
Put/Delete/Test preserve authorization, version and queue semantics. This
helper's lock/mutate conflation is separate from R25's account lock during
session revocation and N01's shared-device credential locks.

### L08 - P2: Group durable diagnostic writes behind a bounded writer

Evidence: `internal/server/request_logging.go:118,135`,
`internal/diagnostics/handler.go:121-123`,
`internal/diagnostics/store_linux.go:711-762`, and the complete-record sync
contract in `docs/development/observability.md`.

Each HTTP completion callback synchronously logs its final record. With the
diagnostic store configured, one store mutex covers health and retention
checks, space reservation, a complete JSONL write and one file sync per
record. Concurrent requests therefore wait for separate serial syncs.
`duration_ms` is captured before logging, so it does not expose this final
handler wait. The client-visible effect depends on response buffering and
streaming; not every response waits to receive its first byte.

Use a single bounded writer to drain already queued records and group their
durable commit. Acknowledge each successful record only after the applicable
sync completes. Prefer draining without an added timer; any deliberate
aggregation delay must be bounded and weighed against low-load latency.
Retention work in the same batch can be consolidated without creating a
separate maintenance framework.

Bound both queued and batch bytes. Preserve record order, complete-line
boundaries, rotation and deletion-marker sync ordering, partial-write rollback,
sticky degradation, fallback logging, and actual writer termination during
Close. Sync each affected file when a batch crosses rotation. Rollback must
never remove an acknowledged record. Queue saturation needs explicit
backpressure or failure, never silent loss. This does not recommend removing
the documented durability guarantee or making logging fire-and-forget.
Keep final request records independent of the cancelled HTTP context, as the
current callback does. Drain accepted records, including the final shutdown
event, before releasing the diagnostic store's process lock.

Verify concurrent request completion, actual sync counts, rotation, partial
write/sync failures, queue saturation and close. Compare end-to-end timing
outside the existing pre-log duration field before claiming a speedup.

## Review-loop ledger

| Round | Inspection and outcome | New recommendations | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Credential resolution, music/search SQL, test-only ownership paths, artwork cache and residual platform branches. | L01-L06 | 0 |
| 2 | Notification mutation locks; task/cache failure paths; provider/event/status bounds; diagnostic persistence; build and delivery platform boundaries. | L07-L08 | 0 |
| 3 | Grant/revoke and asynchronous delivery authority; backup/recovery failure phases; transaction and scan ownership; Range/timeshift lifetimes; cleanup call graphs and Linux assertions. | None | 1 |
| 4 | Cross-review of query/ACL equivalence, credential/notification lock counterexamples, cache/log bounds, startup/settings/activity edges, and configured/build-time platform targets. | None | 2 |
| 5 | End-to-end request/principal/transaction/delivery cost and authority chains; cancellation-to-close ownership; report counterexamples and final platform/evidence correspondence. | None | 3 - stop |

## Verification

All verification ran on `test-env` from the current source snapshot retained
at `/opt/goby-backend-review-loop-20261009-02/source`. No local test, build,
validation suite or runtime probe was performed.

- All seven cleanup files passed remote `gofmt -l` inspection.
- The media and library test binaries compiled, and `cmd/goby` built with
  Go 1.27.1, Linux amd64, `CGO_ENABLED=0`, package concurrency one and
  `GOMAXPROCS=2`.
- 82 distinct top-level tests passed: 37 media and 45 library. Subtests are
  excluded from this count. The directory-authority test ran against the
  designated test database with its isolated schema, rather than skipping.
- The initial media run passed 35 and skipped two tests for missing tool-path
  configuration. Both were subsequently run successfully with pinned
  FFmpeg/ffprobe 9.0.1. An earlier supplementary 7.1.5 run is preserved
  separately and is not substituted for the pinned result.

This is targeted correctness/build verification of the cleanup. It does not
establish a full backend-suite pass or performance gains for L01-L05/L07-L08,
which are not implemented here.

Ordinary runs reused `/root/.cache/go-build` and `/root/go/pkg/mod`; no cache
was copied or cleared. The budget allowed at most 2 GiB shared build cache,
1 GiB owned compiler scratch, 700 MiB source/binary/evidence resources and
kept at least 1 GiB persistent storage available. Compiler scratch used
`/tmp/goby-backend-review-loop-20261009-02-compiler`. Native execution bound
both `GOTMPDIR` and `TMPDIR` to the task's ext4 fixture directory.

At closeout, a fresh process inspection found no owned workers. The exact
empty compiler scratch directory was removed. Source, binaries, fixture root
and raw evidence remain retained; shared caches remain intact. Persistent
availability was 3,717,005,312 bytes and the shared build cache occupied
389,988,352 allocated bytes. The cleanup removed an already empty scratch
directory, so no disk-capacity gain is attributed to it.

The local verification summary is
`.artifacts/backend-review-loop-20261009/verification-summary.md`. Raw logs are
under its `evidence/` directory, including `verification.log`,
`test-library.log`, `test-media.log`, `test-media-pinned.log`,
`test-media-real.log`, and `closeout.log`. The same evidence remains on the
remote host under `/opt/goby-backend-review-loop-20261009-02/evidence`.

## Deduplication and retained boundaries

- Adjacent administrator rechecks in additional callers are R02 coverage,
  and additional full-principal projections are N21 coverage.
- Additional count/page reuse, early unused file opening and test-only helper
  examples are covered by R17, R26 and L04 respectively.
- Retain CSRF, exact credential-kind rules, live revocation/expiry checks,
  source identity and path containment, publication-time proofs, and actual
  process/descriptor retirement. They guard different state transitions.
- Preserve bounded provider concurrency, event queues, scan-result ownership,
  filesystem worker limits, and copied settings snapshots. Their bounds and
  publication semantics are explicit; no new scheduler or cache is justified
  solely by their presence.
