# Backend design review continuation, 2026-10-09

Implementation follow-up:
[backend-continuation-implementation-20261009.md](backend-continuation-implementation-20261009.md).
The review below retains its original findings and review-time status.

## Scope and method

This review starts from `2d12bdb5` and the current working tree in `D:\Code\goby`.
It covers permission complexity, excessive defensive work, architectural
performance, and the Linux-only backend boundary. Existing unrelated changes
remain untouched. Prior R01-R30, N01-N22, L01-L08 and Q01-Q09 findings and their
implementation records are the deduplication baseline. Another instance of an
existing recommendation does not count as a new finding.

Four reviewers inspect the four areas in parallel within sequential rounds.
The primary reviewer checks source evidence, production callers and proposal
constraints. A round is complete only after every area reports. A new accepted
recommendation resets the empty-round counter; stopping requires three
consecutive complete rounds with no new recommendation. This is convergence
within the inspected scope, not a proof that the code has no other defects.
Seven complete rounds were performed. Rounds 5, 6 and 7 added no new accepted
recommendation across all four areas, satisfying the requested stopping condition.

Findings below are recommendations, not implemented changes. Structural costs
follow from the source; no measured latency, throughput or natural-load speedup
is claimed. No tests, builds, validation suites or runtime probes have been run
locally or remotely for this review. Only source/documentation inspection and
read-only Git inspection have been used. No verification environment was created.

## Assessment

Nine new recommendations remain: five P2 and four P3, including one ancillary
credential-state consistency issue. All are unimplemented review findings.

No new permission-bypass defect or removable production Windows server support
was found. The concrete opportunities are avoidable database round trips,
repeated durable scratch cleanup, filesystem work under a shared lock, and
overfrequent limiter maintenance. Current authority, source identity, durability
and actual resource-retirement checks retain concrete purposes.

| Finding | Priority | Area | Structural cost |
| --- | --- | --- | --- |
| S01 | P3 | Authentication infrastructure | Full limiter-map scan on every attempt once more than 10,000 addresses are retained. |
| S02 | P2 | Defensive I/O / preview generation | Up to 12,288 directory sync calls for temporary JPEG deletion per source. |
| S03 | P2 | Playback cache architecture | One window's expiration filesystem I/O blocks other windows behind the global mutex. |
| S04 | P2 | Public-account visibility | One historical-device query for each account using the unused-device visibility policy. |
| S05 | P3 | Preference authorization composition | Two transactions read the owner's configuration, and the second replaces the first result. |
| S06 | P2 | Music scan finalization | One recursive ancestor statement per parent while holding the catalog write-owner mutex. |
| S07 | P3 | Ancillary credential consistency | A PIN-only administrator edit also clears the local-password lockout. |
| S08 | P3 | Lock-only database projection | User-state copying transfers up to 100,001 IDs that Go only counts. |
| S09 | P2 | Catalog deletion / FK lookup | Item deletion has no selective leading item-ID index for several referencing relations. |

S04 and S02 are relatively narrow batching changes. S03 has broader concurrency
and resource-accounting implications and needs separate slow-storage and failure
validation. S01 primarily matters under high address cardinality. None calls for
removing the underlying permission or abuse-prevention policy.

## Findings

### S01 - P3: Amortize login-limiter cleanup

Evidence: `internal/server/auth.go:241-261`, particularly lines 245-251.

When `loginLimiter.windows` contains more than 10,000 addresses, every call to
`allow` scans the complete map for expired entries while holding the limiter's
single mutex. The scan precedes the requested address's limit decision, so
already-rate-limited attempts incur it too. With many still-live entries, the
scan repeatedly removes nothing. The retained table is bounded at 20,000, but
high-cardinality traffic still multiplies linear scans and serializes login
attempts through this work.

Use a cleanup time gate or a bounded incremental cleanup budget. Preserve the
ten-attempt, one-minute address window, the 20,000-entry capacity limit, and
trusted-proxy address handling. Capacity pressure must not discard live limits
or restart a caller's window. Expired entries must become reusable within a
defined bound; an optimization must not indefinitely deny a new address because
maintenance never progresses. No separate cleanup service is necessary.

Validate high-cardinality repeated denials, expired-entry reclamation, full-map
admission, clock boundaries and concurrency. Compare scanned entries and mutex
hold time separately from login/password-verification time.

### S02 - P2: Batch durable removal of preview JPEG scratch files

Evidence: `internal/server/analysis_preview_generation.go:28,172,207,303-306`
and `internal/analysiscache/builder.go:389-443,480-485`.

Preview generation requires three widths and permits up to 4,096 frames per
width. Once a width's BIF is complete, it calls `DeleteTemporary` separately for
every JPEG. Each call validates the file, unlinks it, then calls
`syncDirectory(b.root)` before updating accounting. At the allowed maximum,
scratch deletion alone performs 12,288 directory sync calls per source.

Add one batch deletion operation under the builder's existing serialization.
Keep each frame's reference, deleting-state and identity checks and actual
unlink. Sync the directory once after the batch, then finalize its bookkeeping.
One completed batch per width reduces these directory sync calls to three for
a successful maximum-size build, while retaining durable deletion on successful
return. The same operation can support Publish's residual scratch cleanup.

Cancellation, partial unlink failure or directory-sync failure must abort the
build without reusing affected frame names or prematurely returning reservation
capacity. The builder remains responsible for actual cleanup. Preserve all
final BIF file syncs, owner/manifest/seal writes, the pre-publication directory
sync and the post-rename parent-directory sync.

`WriteTemporary` also uses the ordinary successful file-sync path
(`builder.go:186,217,283`). Scratch-file sync removal is not part of this accepted
recommendation: that would require a separate decision about its error-observation
contract. The primary proposal only batches directory syncs after deletion.

Validate open temporary leases, cancellation, partial unlink and sync failures,
name reuse across widths, restart cleanup, final-publication durability and sync
call counts. The existing streaming-temporary and restart-recovery tests provide
baseline contracts. No performance result is inferred from the call-count bound.

### S03 - P2: Move timeshift expiration I/O outside the global mutex

Evidence: `internal/timeshift/store.go:556-569,943-963`,
`internal/timeshift/storage_linux.go:375-395`, and
`internal/timeshift/reader.go:54-112`. Production callers create the store and
windows in `internal/server/dynamic_sources.go:85,246`.
The scope is dynamic-source playback with transcoding and timeshift enabled.

`removeExpiredLocked` calls `window.storage.remove` while the shared Store mutex
is held. This performs open/stat/close/unlink operations and checks the possible
partial artifact. The periodic maintenance pass holds that same mutex across
all windows' expiration. Snapshot, artifact opening and reader close/cancellation
also need it. A slow cache filesystem during one window's cleanup therefore
delays unrelated windows' control and media-open work. Default bounds are 32
windows and 65,536 artifacts, with a one-second maintenance interval.

First split only expiration retirement: under the lock, make the artifact
unavailable and mark one pending cleanup with retained window resources; perform
bounded cleanup outside the lock; finalize accounting under the lock after
actual deletion. Reuse existing maintenance ownership where practical instead
of immediately introducing global lock sharding or an unbounded worker pool.

Pending deletion must still consume artifact/byte capacity. Preserve readers,
advertisement grace, source/window identity, duplicate-cleanup exclusion and
directory lifetime. Failed deletion remains charged and records the storage
failure. Window destruction, publication-error cleanup and Store.Close must
join the same real resource retirement. Merely moving a function call outside
the lock without these transitions is unsafe.

Readers/resolvers must reject the retiring state. Cleanup already admitted at
shutdown must drain and join rather than vanish when store.ctx is cancelled.
Where publication currently reclaims expired capacity before succeeding, it
may need to wait for bounded cleanup outside the mutex, then recheck cancellation,
window state and capacity; do not introduce a new immediate quota failure simply
because reclaimed bytes are temporarily still charged.

Preserve existing synchronous observations too: the last expired reader's Close,
observable Done completion, ClosePresentation and post-publication expiration
currently let a subsequent Usage/Snapshot observe completed cleanup. Relevant
callers must wait outside the global mutex for their cleanup completion instead
of merely enqueueing it and returning. Existing tests include
`store_linux_test.go:127-190`, `retention_batch_linux_test.go:90-94`,
`grace_linux_test.go:132-137` and `publication_result_linux_test.go:108-112`.
Bounded cleanup means bounded concurrency and bookkeeping, not pretending an
uninterruptible filesystem operation has retired when a timer expires.

Creation and artifact opening also perform filesystem operations under this
mutex (`store.go:155-178`, `reader.go:95-105`); the narrow expiration change does
not claim to eliminate every storage stall. It should first demonstrate that
slow deletion no longer stops another window's existing-artifact open/snapshot.
Test quota retention, concurrent publication/close, grace/readers, deletion
failure and final Store.Close with deterministic remote storage hooks.

This differs from N08's batched subtitle retention queries and Q05's compact
publication result. Those improvements are present; the shared-lock I/O
dependency remains.

### S04 - P2: Batch public-account historical-device visibility queries

Evidence: `internal/server/emby.go:128-147`,
`internal/server/user_management.go:565-577`, and
`internal/identity/user_password.go:121-135`.

The unauthenticated public user picker loads users, then calls
`publicUserVisible` once per account. With a valid, nonempty DeviceID, every account
whose policy sets `IsHiddenFromUnusedDevices` triggers its own
`UserHasUsedDevice` database EXISTS statement. This also happens before other
visibility predicates reject a disabled, administrator, hidden or remote-hidden
account. With H relevant accounts, there are H sequential history round trips
in addition to the account list and other response work.

Within this request, identify accounts for which device history can affect the
result, then query their historical user/device relationship in one statement
or bounded batches and use the resulting user-ID set. Retain account ordering,
Go policy semantics and fail-closed malformed policies, disabled/admin/hidden
checks, remote access and allowed-device restrictions. History must still select
ordinary `kind='emby'` sessions and include previously revoked logins; prior use
is distinct from a currently live credential. Invalid or empty device IDs retain
their existing no-history behavior.

Do not introduce a cross-request permission cache or reuse this display-only
result to authorize an avatar/resource read. Public avatar serving retains its
own current locked policy checks. This is independent of R03's authenticated
directory pagination.

Validate equivalence for local/remote callers, all visibility-policy combinations,
malformed policies, disabled/admin accounts, missing or invalid device IDs and
revoked historical logins. Count statements for a many-account picker request;
do not attribute all public-picker cost to this one history projection.

### S05 - P3: Combine the owner's Configuration endpoint reads

Evidence: `internal/server/user_preferences.go:145-150`,
`internal/server/local_credentials.go:114-121`,
`internal/identity/user_preferences_store.go:103-140`, and
`internal/identity/local_credentials_vault.go:157-190`.

For an ordinary user reading their own `GET /emby/Users/{Id}/Configuration`, the
handler first calls GetUserPreferences. It then attaches the profile PIN through
GetOwnProfileConfiguration, which reads the configuration again and completely
replaces the first projection. The two successful paths issue thirteen SELECTs
in two transactions, excluding middleware and BEGIN/COMMIT. The first read is
still doing necessary preference-feature authorization, but its configuration
value is discarded.

Provide an endpoint-specific combined read: acquire the preference account and
credential locks once, read the configuration and sealed PIN together under the
same account lock, decrypt, then perform fresh final authority checks. Preserve
configuration projection, mixed-update consistency, peer/device policy, expiry
and revocation, and the ordinary-owner-only plaintext PIN boundary.
The current final owner projection is server.projectUserConfiguration, including
its Unicode and omitted-field behavior; the typed identity preference DTO must
not be assumed to be an identical wire replacement.

Do not simply skip GetUserPreferences: GetOwnProfileConfiguration does not
enforce EnableUserPreferenceAccess / FeaturePreferences. Conversely, do not add
those gates globally to GetOwnProfileConfiguration: existing login/Users/Me
behavior deliberately permits an owner to read an existing PIN when preference
editing is disabled (`local_credentials_vault_integration_test.go:99-105`).
Other users, native administrator projections and application keys must never
receive the PIN. No generic User DTO should acquire this private field.

Validate the Configuration endpoint's denied feature/permission cases, successful
owner reads with/without PIN, other-user/admin/application requests, concurrent
mixed configuration/PIN patches, revocation/expiry after waits, vault failures,
and unchanged login/Users/Me behavior. Count endpoint statements to establish
the reduction without assuming all authorization checks are redundant.

### S06 - P2: Batch nearest-album discovery at music scan completion

Evidence: `internal/library/metadata_music_scan.go:130-175`, especially 145-156;
`internal/library/ownership.go:151-179`; and `internal/library/scan.go:194-195`.

refreshScannedMusicAlbums opens one owned transaction, sorts deduplicated parent
IDs, and issues a separate recursive ancestor query for every parent.
Only after all queries does it commit and release the catalog write-owner mutex.
P parents therefore produce P sequential SQL round trips while unrelated catalog
writes wait. The real media.Prober implements MusicMetadataVersion, so the scan
calls this path; cached audio can also contribute old and new parents.

Batch discovery through a bounded parent-ID input relation with an explicit
origin per traversal. This reduces discovery statements to ceil(P / batch-size).
Keep each origin's visited set, same-library and ordinary-item restrictions,
nearest folder MusicAlbum stopping rule, completeRoots filtering, deterministic
ordering and final album deduplication. A single global visited set would not
preserve independent origin paths.

Keep protected owned-transaction calls and the subsequent separate
refreshOwnedMusicAlbum transactions. An incomplete root must retain its old
accepted album source; cancellation may preserve already-committed album
publications as today. Do not turn discovery batching into a new giant atomic
publication or bypass owner-session failure handling.

Validate nested albums, shared ancestors, cycles, missing parents, cross-library
links, incomplete roots, cached audio moves and cancellation. Measure discovery
statement count and owner-lock hold time on many-parent scans. This is distinct
from R15's repeated ancestor evaluation inside one content-policy statement.

### S07 - P3, ancillary: Preserve local-password lockout on PIN-only edits

Evidence: `internal/identity/local_credentials.go:173-204,248-260`,
`internal/server/local_credentials.go:14-17`, and
`internal/identity/local_credentials_preferences.go:62-64`.

An administrator can update only ProfilePin through the native local-credentials
PUT, omitting LocalPassword and retaining EnableLocalPassword. The common UPDATE
unconditionally sets local_password_failures to zero and blocked_until to NULL.
The later localChanged calculation is false for this request: it preserves
sessions and emits UserUpdated rather than PasswordReset. Thus a PIN edit also
ends the separate five-minute local-password block created after five failures,
even though that credential did not change. The compatibility PIN update does
not reset these fields.

Compute whether local-password material/enablement changes before the UPDATE
and use that fact to decide the failure-counter/block reset. Keep the existing
reset, session revocation and audit semantics for explicit local-password or
enablement changes. Keep PIN revision, validation, normal-password prerequisite,
vault encryption and final authority checks.

This is an administrator-only unintended state coupling, not an ordinary-user
privilege escalation. The route already requires an administrator cookie and
CSRF protection, and administrators can intentionally reset credentials. No new
authentication requirement or broader security redesign is recommended.

Validate a live block surviving PIN-only set/clear, same-PIN input and unrelated
configuration edits; explicit local-password/enablement changes must retain
their reset/revoke/audit behavior. The documented five-failure/five-minute rule
is in `docs/api/playback-accounts.md:57-58`.

### S08 - P3: Return counts from lock-only user-copy queries

Evidence: `internal/identity/user_copy.go:180-226` and the 100,000-row combined
copy budget at line 20.

lockCopiedUserData first locks referenced media items, then catalog entities,
using ordered FOR KEY SHARE queries. It streams every ID to Go, where each ID
is discarded after incrementing a counter. A full copy therefore transfers and
scans 100,000 unused identifiers; an over-limit input reads up to 100,001 before
rejecting. Management/account locks remain held during this work.

Retain each ordered, bounded row-lock subquery, but return an outer count of its
results instead of every ID. Keep the item-before-entity lock order, the first
max-plus-one limit, the second query's remaining combined budget, source-account
write barrier, FK-target locks, error propagation and atomic copy rollback.
Do not perform an unlocked count followed by a separate locking statement.

This removes unused result transport and decoding, not the database's necessary
row traversal or locking. Before implementation acceptance, verify the nested
query's actual lock behavior and plan on the designated PostgreSQL environment,
including concurrent deletion, source-state writers, exactly-at-limit and
over-limit mixed item/entity inputs. This is separate from directory pagination
and duplicate count/page computation in the prior reviews.

### S09 - P2: Index the actual item foreign-key lookup paths for deletion

Evidence: `internal/database/migrations/0006_playback_state.sql:3,22,37-42`,
`internal/database/migrations/0044_media_operations.sql:7,47-55`, all subsequent
published index definitions, `internal/library/scan_reconciliation_commit.go:554`,
and `internal/library/store.go:411`.

user_item_data and play_sessions reference items with ON DELETE CASCADE;
media_operations.item_id uses ON DELETE SET NULL. Their current indexes have no
item_id-leading lookup for all referencing rows. user_item_data's primary key
starts with user_id, and play_sessions' composite source index is both
non-leading and partial. media_operations has no index on item_id at all:
source_item_id is a separate historical field, so its indexes cannot serve the
actual foreign-key lookup merely because the values commonly coincide.

Reconciliation deletes batches of items, and library removal cascades through
the catalog while holding its owned transaction. Each deleted parent requires
referencing-row work. With substantial playback/user-state/operation history,
unselective lookups can repeat this work over large child relations. The source
establishes the missing access paths, not the observed execution plan or elapsed
cost. A suffix index can still be scanned; this finding does not claim every
existing query necessarily performs a sequential heap scan.

Evaluate a new migration adding item_id-leading indexes to these specific
relations. media_operations can use a non-NULL partial index if the measured
plans support it. Preserve database CASCADE/SET NULL behavior and current
publication barriers. Do not rewrite published migrations or indiscriminately
index every foreign key; update the repository's schema publication and restore
expectations when implementing the selected migration.

Use realistic remote PostgreSQL data to inspect deletion/trigger plans and
buffers, including retained terminal operations, mixed user histories and large
reconciliation batches. Compare deletion owner-lock duration, index size and
write-maintenance cost before settling the final index set. Do not report an
end-to-end scan speedup without measurement.

## Linux-only boundary

Production Windows/non-Linux server support was removed by preceding commits;
this review does not attribute those earlier changes to the current task. The
server, recovery CLI, process domain, native helpers and release targets already
require Linux. `launcher_other.go` is a `linux && !amd64` capability rejection,
not a Windows implementation.

Remaining Windows references describe playback clients, Windows development
hosts, unsafe-input fixtures, upstream third-party code or historical records.
Persisted `unsupported_platform` values remain for reading historical analysis
records. Some test-only runtime Linux guards repeat earlier N09/L06 cleanup
coverage; they do not restore a working Windows backend and do not justify a
new architecture finding. No new platform source change is necessary here.

## Review-loop ledger

| Round | Completed scope | New accepted recommendations | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Current credential and user visibility paths; cache/backup/lifecycle durability; query, event and playback-cache scaling; Linux source and delivery boundaries. | S01-S04 | 0 |
| 2 | Cross-review of temporary cleanup, historical-device visibility and timeshift retirement; configuration/PIN projection; music scan finalization; provider, subtitle, task and native-build boundaries. | S05-S06 | 0 |
| 3 | Configuration-read policy boundaries; HLS/transcode/scan ownership; backup import/export validation; native capabilities and reverse platform dependencies; separate credential-state semantics. | S07 | 0 |
| 4 | Maximum inputs, visibility and expiry counterexamples; partial scratch cleanup and timeshift close; account-copy lock projection and reverse-FK indexes; startup/status/lease lifetimes; platform and report consistency. | S08-S09 | 0 |
| 5 | Independent report/source reconciliation; downstream private/public configuration projections; copy lock and deletion barriers; preview and timeshift accounting; query-cost bounds, migration contracts and external native build inputs. | None | 1 |
| 6 | End-to-end login/configuration/copy routes and wire projections; static failure/lease/grace/Close test contracts; exact call-count bounds, scan cancellation/publication behavior and retained platform evidence. | None | 2 |
| 7 | Final independent adversarial permission, durability, query/resource and platform passes; production caller reachability, maximum/empty inputs, concurrent-state boundaries and final evidence correspondence. | None | 3 - stop |

The review is complete. The only task-owned workspace change is this report;
production source, existing tests and unrelated working-tree changes were left
unchanged. No commit, push, deployment or remote verification resource was created.
Implementation and measurement requirements are listed with each recommendation;
reading existing tests is not reported as executing them.
