# Backend design review loop, 2026-10-10

## Scope and evidence

This review covers permission complexity, excessive defensive work, backend
performance architecture, and the Linux-only server boundary. It reviews the
current working tree at `cf00e7c3`, including pre-existing uncommitted changes;
it does not claim to review only the committed release. Existing source,
experiments, release-script changes and deleted historical notes are preserved.
No production code is changed by this review.

Four parallel tracks inspect authority, defensive/resource lifetimes,
performance, and Linux/platform boundaries. The coordinating review checks
the actual callers, rejected alternatives, and overlap between tracks.
Recommendations are deduplicated against R01-R30, N01-N22, L01-L08, Q01-Q09,
S01-S09, F01-F13, C01-C15 and their implementation records. A prior finding is
not new merely because another caller has the same already-recorded problem.
Implementation records are context, not a substitute for reading current code.

All evidence below is static call-path evidence. Counts describe the successful
code path, not measured latency, throughput or production frequency. No local
tests, builds, formatting checks or runtime probes were run. The authorized
remote `test-env` was reachable, but its ext4 root had approximately 658 MiB
available on a 152 GiB filesystem. No build, new verification environment,
database mutation or cache cleanup was admitted. The capacity policy was read;
the observed shared caches were `/root/.cache/go-build` and `/root/go/pkg/mod`.
The remote inspection used `/usr/bin/go`; no version-dependent acceptance is
claimed. No task-owned compiler cache or scratch was created.

## Assessment

The main permission-design opportunity is to consolidate duplicated authority
implementations and parameterize policy data. There is no evidence here for
removing revocation, actor/target separation, post-wait checks, anchored file
opening, authenticated backup completion, or actual process/I/O retirement.

Eight recommendations have concrete current-code evidence. One further
optimization is conditional on an explicit change to the vault observation
contract and should not be implemented as a semantics-preserving cleanup.

| ID | Priority | Area | Recommendation | Status |
| --- | --- | --- | --- | --- |
| T05 | P2 | Permission correctness and duplicated authority | Apply current Emby login policy to application-key management after waits. | Actionable; regression verification needed |
| T08 | P2 | Linux path correctness | Align root registration/update/restore with media-read path acceptance. | Actionable; regression verification needed |
| T03 | P2 | HLS architecture | Share repeated primary-media work within one bounded external-subtitle validation group. | Measure before implementation |
| T01 | P2 | Permission SQL architecture | Parameterize user and policy values to reduce query-text/cache cardinality. | Measure query plans before implementation |
| T04 | P3 | Redundant database work | Remove request-local JIT settings already guaranteed by the production pool. | Small scoped simplification |
| T02 | P3 | Defensive serialization | Canonicalize storage-binding snapshots once per encoding. | Small scoped simplification |
| T06 | P3 | Defensive parsing | Reuse the strict notification-reference decoder's constructed values. | Small scoped simplification |
| T07 | P3 | Subtitle projection | Reuse the highest embedded stream index within one item projection. | Small optimization; avoid work on the no-subtitle path |
| T09 | P3 | Vault I/O | Consider request-bounded master-key reads for a reveal page. | Conditional; not an equivalent cleanup |

Fix T05 and T08 first. T03 and T01 need targeted remote measurements and
correctness checks before a broad refactor. T02/T04/T06 are small opportunities;
T07 should remain local and lazy. T09 needs a contract decision and evidence
that the low-frequency management cost warrants the change.

## T05: Consolidate ordinary administrator policy checks for key management

`internal/identity/application_keys.go:600-619` implements
`authorizeApplicationKeyActor` separately from ordinary administrator
authorization. Its `admin`/`emby` SQL branch checks role, disabled status,
credential revocation and expiry, but does not read policy, the credential's
device, or the observation time used for policy evaluation.

In contrast, `internal/identity/devices.go:190-219` checks current Emby
`AccessSchedules`, enabled devices, lockout and remote-access policy. The Emby
key routes enter through `requireEmby` (`internal/server/application_keys.go:23-26`),
but that authentication precedes the management/advisory, account, credential
and vault waits. The checks after those waits all call the incomplete helper.
Create, List, Get and Revoke finish with that same helper at lines 250, 365,
418 and 498, then commit; the HTTP handlers perform no later policy check.

A request authenticated before its access schedule closes can wait and still
create or reveal keys afterwards. A policy change before the operation acquires
its account lock has the same problem: ordinary policy updates do not
necessarily revoke the session (`internal/identity/managed_users.go:204`). This
is a current authority omission, not a suggestion to remove a security check.

Delegate only the ordinary `admin`/`emby` branch to the existing transaction-local
administrator policy logic. Keep the application credential's parent/key/client
binding, lock order, error mapping and exact self-revocation timestamp exception.
Do not replace the whole helper with `CheckAdministrator`, which intentionally
has no self-revocation exception. The existing ordinary branch can delegate to
`authorizeDeviceActor` without recursion; the reverse call is key-only.

Remote regressions should hold a management/credential wait after authentication,
change policy or cross a schedule boundary, and assert no token, mutation or
reveal audit commits. Cover native administrators, application principals,
demotion, disabled accounts and precise self-revocation as controls.

## T08: Make root path acceptance consistent on Linux

With `/media` as an approved anchor, a real Linux directory `/media/a\b` can pass
registration. `internal/library/paths.go:79-129` uses Linux path resolution and
retains `a\b` as the root-relative name. `root_binding_registration.go:163-172`
and `internal/storagebinding/topology.go` accept the mapping. The create path
persists it at `internal/library/store.go:249`; library updates use the same
registration chain and persist the value in `library_editing.go:426,435`.
There is no corresponding `library_roots.relative_path` backslash constraint.

`OpenMediaFor` eventually reaches `validateMediaSource`
(`internal/library/media_source.go:284-312`). Line 295 applies
`validMediaSourceRelativePath` to the root-relative path; that predicate rejects
every backslash, causing the surrounding validation to return `ErrUnavailable`.
Thus a root can be accepted and
stored while ordinary media reads under it are rejected. Making that directory
the approved anchor itself yields `.` and avoids this particular rejection,
which further demonstrates the inconsistent acceptance sets.

Restore accepts the same simple name: `internal/recovery/restore.go:262` rejects
traversal components but not `a\b`, and the database binding model uses the same
storage-binding validator. The inconsistency is distinct from F10/C13's earlier
colon-path cleanup.

Choose one root-path contract and apply it at create, update, restore and read.
A narrow fix may reject unsupported roots before persistence; a Linux-support
change may use a distinct root-relative validator after auditing all consumers.
Do not remove all backslash checks from media item paths, URI components or old
archive schemas. Those are separate accepted contracts. Remote regressions
should cover nested and exact-anchor backslash names, normal paths, symlinks,
traversal, update and restore, and actual media opening.

## T03: Batch repeated primary-media work for HLS subtitle validation

`internal/server/hls_subtitle_renditions.go:169-184` checks every planned
external-tagged subtitle with `readPlannedExternalSubtitle`. That delegates to
`ReadSubtitleFor` (`internal/server/subtitles_burn.go:18`). Each invocation
performs two `readSubtitleSnapshotFor` transactions, before and after root I/O
admission (`internal/library/subtitles_source.go:85,100`), and two primary-media
opens around subtitle storage work (lines 119 and 137).

The HLS plan permits eight tracks (`internal/transcode/hls_subtitle_plan.go:11`).
A successful eight-external-track validation therefore performs sixteen
primary-media snapshot transactions and sixteen primary-media opens, excluding
the surrounding HLS source authorization. The returned subtitle content is
discarded in this validation loop. Resolve and response-time revalidation can
each invoke the loop. This is separate from F01's embedded-subtitle extraction
cache and from the already-recorded narrow-item-projection opportunity.

Add a bounded group operation for the same item/source that shares the primary
snapshot, routing/admission work and descriptor lifetime within each validation
stage. Keep both the pre-admission routing observation and the fresh authority
observation after waiting. Retain each track's actual bytes, hash, codec,
descriptor/name checks, and the final primary-source/publication fence. Do not
substitute an earlier HLS source observation or cross-request permission cache.

`ExternalTag` also covers owned derivatives. Do not materialize eight maximum
payloads together; consume tracks under a total byte budget, or initially scope
the optimization to filesystem sidecars. Do not turn eight tracks into eight
parallel `ReadSubtitleFor` calls: the existing worker bound is four. A longer
batch lease can affect fairness, so measure transactions/opens, peak memory,
first-response latency, multi-user p95/p99 and cancellation for one/eight tracks,
including slow storage and authority/source changes. No speedup is claimed here.

## T01: Parameterize policy data instead of embedding it in SQL text

`internal/library/collections.go:138-144` embeds the user ID in collection ACL
SQL. `policy_access.go:60,76,83,114,120` similarly embeds folders, tags, exclusions
and rating values. Different ordinary users therefore generate different SQL
text even with otherwise equal policy; policy edits generate more variants.
The escaping in `policySQLString` is explicit: this is not an SQL injection
finding.

The production default is `CacheDescribe`
(`internal/database/database.go:53`), so variants consume separate description
cache entries. The source hot path additionally opts into `CacheStatement`
(`internal/library/playback_media_authorization.go:226-238`), where they also
produce separate named statements. Both caches are bounded; the concern is
prepare/describe work and churn, not unbounded cache growth.

Retain necessary structural branches and bind user IDs, arrays and rating values
as query arguments. Start with the source hot path rather than redesigning all
policy SQL at once. Preserve collection/owner restrictions, actor/target
semantics, the post-SHARE-wait fresh snapshot, and cache opt-outs. Description
cache capacity zero selects `DescribeExec`; statement cache capacity zero skips
the source named-statement branch. Administrator/userless-key paths may already
share text and should not be used to exaggerate the affected population.

Measure rotating users with equal/different policy, prepare/describe counts,
query CPU and actual plans. Parameterization can change custom/generic plan
selection; fewer SQL variants do not automatically imply faster execution.

## T04: Keep JIT configuration at the pool boundary

`internal/database/database.go:59` sets `RuntimeParams["jit"] = "off"` for every
ordinary production pool. Nevertheless `internal/library/query.go:115`,
`latest.go:37` and `viewing_statistics.go:31` each execute another
`SET LOCAL jit = off` before their actual query work.

The production chain is closed: startup uses `database.Open`
(`cmd/goby/generation.go:135`); restore and transition slots also use it
(`internal/recovery/plans.go:123`, `transition.go:799`); playback control copies
the configured data pool (`internal/database/playback_control.go:32`). No
production consumer was found that enables or resets JIT afterwards.

Remove those three redundant settings and their outdated local-policy comments.
Directly constructed test pools should explicitly adopt the production policy.
This removes one request-local database round trip in each affected operation;
it does not establish an end-to-end latency percentage. Remote verification
should confirm settings on startup/restored/control connections and preserve
Items, count, Latest and statistics results.

## T02: Canonicalize each storage-binding encoding once

`internal/storagebinding/document.go:64-77` calls `snapshot.Validate()` and
then `snapshot.canonicalBytes()`. `topology.go:54-56` shows that `Validate`
already calls exactly that canonicalizer and discards the result. Canonicalizing
validates version, identities, mapping, paths, duplicates, boundary count and
bytes, then copies/sorts boundaries and marshals JSON.

Drop the first redundant call. Registration (`root_binding_registration.go:98`)
then avoids one complete sort/marshal. Rebinding (`root_bindings_write.go:115-119`)
can additionally derive its fingerprint from the canonical bytes it just
encoded instead of canonicalizing a third time. Do not hash PostgreSQL-spaced
or arbitrary transport JSON as a substitute.

The successful canonical output is already limited to 2 MiB; the encoder's
subsequent 4 MiB check is unreachable with the current constants. The 4 MiB
raw-input limit in DecodeSnapshot has an independent transport/jsonb purpose
and must remain. Keep private clones, deterministic empty-array encoding,
live topology revalidation and all publication/ownership boundaries. This is
low-frequency management work, not a claimed major throughput bottleneck.

## T06: Return the references already constructed by strict validation

Notification fanout and delivery claim each call `ValidateReferences` and then
`decodeReferences` on the same bytes (`internal/notifications/queue.go:176-179`
and `377-380`). The strict validator already decodes each object and constructs
the complete `Reference` (`internal/notificationjournal/validation.go:90-129`),
but returns only an error. The second decoder (`notifications/store.go:362-372`)
repeats JSON parsing with weaker checks. Input is bounded at 512 KiB/4096 refs.

Return the ordered references from the existing strict decoder and retain a
validation-only wrapper for backup consumers. Reuse the result only on these
already-strict paths; `queue.go:266` has an independent decoder use. Keep exact
field-name checks, null-array rejection, tuple uniqueness, canonical entity IDs and
the distinct source/delivery/Test/Resync owner and empty-array rules. A struct
decoder alone is not an equivalent replacement. Return no partial results on
error. Dynamic authorization, registration revision, claim/commit boundaries
and send-time checks remain independent.

## T07: Reuse one embedded-index result within an item projection

`attachSubtitles` checks `highestEmbeddedStreamIndex(items[index].Media)` for
each subtitle (`internal/library/subtitles.go:104`). Owned and bitmap projection
repeat the same full stream-list scan (`owned_subtitles.go:58`,
`bitmap_subtitles.go:189`). These branches append subtitle fields without
changing `Media.Streams`.

Lazily compute the maximum when the first external subtitle reaches an item
position and reuse it only for that projection. The repeated work changes from
subtitle-count times stream-count to their sum. Use item positions, not just
IDs: repeated IDs can have different supplied projections. Preserve bitmap
private facts before collision filtering, ordering, trimming and the authorized
transaction. Avoid eagerly scanning every no-subtitle item or introducing a
cross-request cache for this small, bounded optimization.

## T09: Conditional batch opening of the application-key vault

`ListApplicationKeys` calls `vault.Open` for each active reveal row
(`internal/identity/application_keys.go:347-364`). Each Open safely loads the
same master (`application_key_vault.go:98`), including opening all ancestors,
directory flock, file reads and identity/digest checks
(`application_key_vault_linux.go:24-90,122-185`). Emby's reveal list defaults to up to
200 rows. All of this occurs while management/actor database locks are held.
`WitnessBackup` demonstrates reuse of one safely loaded key, but it
serves a different observation contract.

A request-local batch could reduce those filesystem operations to a constant
number of complete path observations. It must keep each ciphertext's AAD/tag,
context checks, original decrypt/audit order, final authority and all-or-nothing
result. Empty, metadata-only and all-revoked pages must not touch the vault.
Final observation must reopen the current absolute path chain and check the
current name/identity/digest, not merely stat the original descriptor. Do not
hold directory flock across audit SQL or create a process-wide key cache.

This is not observationally equivalent to checking before every secret: a
temporary external replacement between audit waits might be seen by the
current implementation and missed by batch endpoint observations. The current
database locks do not exclude such filesystem changes. If the existing
per-secret contract is required, retain it and reject this optimization.
The operation is low-frequency; measure before accepting this added API and
contract change. Clearing an original key array also does not establish that
Go cipher internals have been zeroized. This conditional opportunity is
separate from the eight actionable recommendations.

## Linux-only cleanup and retained boundaries

The server's non-Linux implementation scaffolding was already removed in
`27071431`. Both executable entries are Linux-tagged; host telemetry has only
its Linux implementation. `internal/commanddomain/launcher_other.go` is
`linux && !amd64`, an unsupported Linux-architecture rejection, not a Windows
server implementation. Release compilation is already Linux-specific.
No new safe Windows server deletion was found, so no production file was
deleted in this pass.

Retain Windows development-host helpers, Windows playback-client fields,
explicit rejection of unsupported Emby network-path credentials, and the
private evaluator's non-Linux refusal. Historical migrations and archive
schemas retain their original path semantics. `filepath.ToSlash/FromSlash`
are identity operations on Linux but still document stored-path boundaries;
mass removal offers insufficient benefit. T08 concerns inconsistent root
acceptance, not a claim that every backslash restriction is Windows support.

Also retain fresh authorization after real waits, descriptor/path replacement
checks, source publication fences, backup authenticated EOF/hash/canonical
format validation, bounded queues and actual cleanup before resource release.
The existing scan/task idle hints, bounded progress checkpoints, source query
batching and transcode inspection budgets are not new findings.

## Previously reported issue still visible in this tree

Q09 is not counted as a new finding. The follow-up implementation record says
snapshot failure and reader retirement were consolidated, but the current
`internal/backupstore/snapshot.go:125-130` still calls `release` before `failure`;
`reader_linux.go:55` removes the reader independently under the store mutex.
`Store.Close` can therefore miss a concurrently retiring reader's close error,
the same shutdown ordering issue described by Q09. Reconcile that historical
implementation claim with the actual branch before treating Q09 as closed.
This review does not modify or rerun that earlier change.

## Review-loop ledger

The stopping condition is three consecutive completed rounds without a new
supported recommendation. Cross-checks that refine an existing recommendation
do not create another ID. The conditional T09 candidate conservatively reset
the counter even though it is not an equivalent cleanup.

| Round | Focus and outcome | New IDs | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Authority SQL, storage canonicalization, HLS subtitle call chain, pool configuration and platform inventory. | T01-T04 | 0 |
| 2 | Route/store authority composition, strict decoder consumers, subtitle projections and Linux root acceptance. | T05-T08 | 0 |
| 3 | Policy changes and self-revocation; decoder and projection counterexamples; HLS final checks; root create/update/restore/read closure. | None | 1 |
| 4 | Reverse tracing from token/HTTP results, final authority, failure ordering, scan/task/cache consumers and platform re-scan. Found repeated vault opening. | T09, conditional | 0 |
| 5 | Vault observation granularity and locking, mixed/empty inputs, cache opt-outs, multi-user/subtitle saturation and retained platform semantics. | None | 1 |
| 6 | Cross-reviewed the report against current source; rechecked final authority, root acceptance, canonical bytes, notification semantics, subtitle counts, scan/task finalization and the known Q09 discrepancy. Corrected references and wording only. | None | 2 |
| 7 | All four tracks rechecked production consumers, actor/target boundaries, resource ownership and actual retirement, HLS response fences, SQL cache modes, scheduler/scan/cache limits, Linux entries, root paths and historical archive contracts. No new supported recommendation or counterexample. | None | 3 - stop |

Rounds 5, 6 and 7 satisfy the requested exit condition. This is convergence
within the inspected backend scope, not proof that no other issue exists.
No runtime or performance acceptance is implied by static review convergence.
The only artifact added by this task is this report; production code, prior
working-tree changes, remote source/data and existing caches remain untouched.
