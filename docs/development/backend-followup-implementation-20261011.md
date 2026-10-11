# Backend follow-up implementation, 2026-10-11

This change implements the selected scope from the
[follow-up review](backend-design-review-followup-20261011.md). Remote Linux
verification is complete. The implemented changes, retained contracts,
measured performance limits, and exact verification scope are recorded below.

## Baseline and scope

Implementation started in a clean, isolated worktree at
`05a955c56f9ea886f2b579f295d61acc72e77249`. The original checkout and its
pre-existing uncommitted changes were preserved. The review's description of
its earlier working-tree snapshot remains historical evidence, not a claim
that unrelated local changes were incorporated into this candidate.

The selected scope comprises Z01-Z21 and the additional R01, N21, C03, Y04,
N18, and R10 call sites. Previously withdrawn or deferred proposals remain
outside this implementation. No authorization layer or cross-request
authorization cache was introduced or removed.

## Implemented recommendations

| ID | Implemented change | Retained boundary |
| --- | --- | --- |
| Z01 | `ListClientSessionSummaries` selects only PlayableMediaTypes, SupportedCommands, and SupportsMediaControl for HTTP lists, Sessions subscriptions, command acceptance, and command delivery. | Complete capability reads and write validation remain. Selected fields retain their limits and null/omission semantics; identity, presence, visibility, expiry, and final authorization remain current. |
| Z02 | Notification batches validate each unique parent edge once. Removed-child suppression uses the validated direct parent's Removed state. | Original output order, conflicting duplicates, library/type/role checks, PreviousParent validation, cycle resynchronization, encoding budgets, and publication generation remain. |
| Z03 | Movie Similar finds candidates through the seed's qualifying entities before expanding candidate credits. | The seed is independent of candidate filters. Artist, Genre, Tag, and Studio scoring, distinct credits, ACLs, repeatable-read scope, ordering, offsets, Limit=0, and returned-page counts remain. Music and InstantMix fallback semantics are unchanged. |
| Z04 | Administrator session history filters status earlier, keeps count/page intermediates narrow, and fetches detail columns after page selection. | One observed database clock, revoked > disabled > expired > active precedence, user-backed counts, search, stable ordering, empty-page totals, and the independent final actor/clock check remain. |
| Z05 | Account creation uses dedicated admission with shared actor-account and credential locks. | The management lock, current role/policy/device/peer checks, final authorization, name uniqueness, activity, and rollback remain. Update/delete and copy-source locking are unchanged; a demoted Emby actor retains ErrUnauthorized. |
| Z06 | Separate RunSummary, DefinitionSummary, and DefinitionTarget types feed six display reads and eight preliminary target reads. SQL builds explicit display projections. Mutation results convert to summaries only at the HTTP boundary. | Execution, coalescing, recovery, and publication retain complete Runs. Snapshot consistency, errors, nullable UTC times, totals, stored compatibility-key checks, target Enabled checks, revision/timezone handling, and later mutation authorization remain. |
| Z07 | Selected task UPDATEs return complete persisted Runs. Admission and stop reuse those values before fresh aggregation; active-run locking queries also return payloads when subsequently consumed. | Actor revalidation precedes decoding at manual admission. Only real terminal transitions settle queues or record completion. Zero-update read behavior, private authority, database times, and later queue/activity errors remain. Final-read exceptions are listed below. |
| Z08 | Viewing statistics exclude rows with neither played state nor positive playback progress before item-dependent aggregation. | Initial subject authorization, visibility, duration validation, clamping, numeric accumulation, rounding, zero results, and transaction completion remain. |
| Z09 | Scheduled occurrence admission returns its existing or newly inserted private receipt to the system-event consumer. | Nullable run IDs retain their meaning. System events still reject missed/null receipts; occurrence admission, event receipt, and cursor advancement remain one transaction. |
| Z10 | A known-busy system-event analysis branch defers after its active-ID lock without decoding an unused Run. | Analysis preparation stays before deferral. No event or occurrence is consumed, and other overlap branches still decode and compare their execution inputs. |
| Z11 | Scan reconciliation memoizes successfully validated ancestor suffixes within its byte budget. Its collection invalidation can reuse the exact same-library closure from the locked relation. | Each origin keeps depth/cycle/missing-parent validation, Audio-only album contribution, sorted unique results, and complete validation above a surviving album. Optional scratch is charged; the original traversal/query remains the fallback. Fact limits, overflow sentinel, private scopes, and journal rollback remain. |
| Z12 | Managed-user authorization combines adjacent Emby credential observations. Deletion reads self-deletion expiry/device facts only when deleting the actor's own account. | Account-role ordering, credential identity/kind/revocation/expiry, database clock, local/device/peer policy, self-deletion's final check, and other-user deletion's independent final authorization remain. |
| Z13 | Native metadata search, subtitle search, and subtitle download call OnlineMetadataQuery directly after their existing request and ID parsing. | The domain query retains administrator authorization, complete metadata validation, Season/Episode ancestry, and transaction completion. Timeouts and later source/publication checks remain. Other provider handlers retain their required metadata observations. |
| Z14 | Provider provenance projects top-level source-field names instead of transferring and decoding unused values. | Empty source rows, non-null empty Fields, name mapping, duplicate mapped names, sorting, and preceding target/source validation remain. |
| Z15 | A newly inserted subtitle-provider binding is returned by INSERT. The conflict branch retains its lookup and exact provider/ID comparison. | No conflict UPDATE is introduced. Catalog snapshots, actor/task checks, source/file observations, rollback, and uncertain-commit retention remain. |
| Z16 | Music source refresh uses a conditional UPDATE with IS DISTINCT FROM and checks RowsAffected. | Existing locks, unchanged-source early behavior, source composition, overrides, catalog comparison, notifications, and error propagation remain. |
| Z17 | Image selection and provenance use a smaller metadata-target projection. | Item/metadata-state existence, supported types, relevant locks, revision checks, and local/music/applicable-online source composition remain. The original online_type match and the separate HTTP/post-network complete metadata checks remain. |
| Z18 | Metadata object decoding stores each freshly decoded RawMessage directly. | Each destination already owns its bytes. UTF-8, duplicate keys, root shape, trailing-data rejection, number precision, and subsequent field validation remain. |
| Z19 | A private validated settings document retains canonical JSON for the PATCH baseline and explicit GetUserSettingsJSON read. Stored values are decoded once through nullable string pointers. HTTP writes canonical bytes directly. | GetUserSettings still returns an independent map. Limits, null/alias handling, malformed-storage errors, missing-row defaults, no-op timestamps, preflight reads, authorization, Content-Length, and HEAD behavior remain. |
| Z20 | Five consumers use explicit account projections: raw policy for dynamic/playback conversion, controller facts for session filtering, name for login-by-ID, and complete Configuration for cross-user preference projection. | Existing query placement and missing-user handling remain. An application key's selected target does not become its principal or acquire a new disabled/admin predicate. PlaybackInfo still checks a supplied UserId without a DeviceProfile. |
| Z21 | Owned transaction completion checks the protected context before driver Commit and rolls back known failures independently. Scan staging checks writeCtx before its raw Commit. Flush failures preserve cleanup results. | Only confirmed rollback adds pgx.ErrTxCommitRollback. Cleanup failures retain their classified error, finalization has one owner, and actually attempted Commit failures retain the original owner-loss/unknown-outcome path. |

## Existing-principle follow-through

| Principle | Implemented scope | Retained boundary |
| --- | --- | --- |
| R01 | Session filtering prepares controller policy once per response. Dynamic conversion returns the limits already processed by hlsPrincipalLimits. | RemoteControl feature denial precedes administrator control. Invalid controller policy denies control without a new HTTP error. Delivery-time authorization, dynamic refreshes, local-peer exemption, and raw application-target conversion policy remain. |
| N21 | Ten continuation callers use authority-only session reads, including the dynamic revalidation function value. Notification fanout uses RevalidateSessionAuthorityInTransaction. | No fresh check was removed or moved. Fanout retains its same connection and explicit ReadCommitted transaction, conditional revocation update, and commit. Full-principal consumers and initial dynamic preference selection remain complete. |
| C03 | Five consumers use TaskMaxConcurrent, InternetProvidersEnabled, MetadataValues, ManagementSnapshot, or the existing ValueSnapshot. Provider execution builds configuration from its captured management options. | Each projection reads one published state. Admission freshness, the two separate availability observations, worker-start consistency, independent subtitle-language slices, and full settings responses remain. |
| Y04 | Stop reuses TaskKey from its locked Run. Recovery obtains task keys and restart predicates in its original ordered parent-lock query and aggregates them after closing the result. | Parent/child/scan lock order, force-probe consistency, exact restart predicates, and rollback remain. Empty recovery retains its final aggregate SQL observation. |
| N18 | Suggestions, Similar, Music Similar, and Latest skip self-user configuration projection when parsed query values already determine every consumed default. | Cross-user existence reads and their error ordering remain. Application targets stay neutral. Latest uses normalized ParentID/Ids and still applies global exclusions when needed; Views and ordinary Items retain their preferences. |
| R10 | Repeated Stop does not rewrite an already Stopping parent. Scan cancellation updates only newly changed flags and returns ordered changed IDs for the later audit. | First-stop activity uses the original Run. Child/scan locks, fresh totals, cancel signals, worker joins, and transaction boundaries remain. The separate task_scans status UPDATE and its deletion/publication triggers remain. |

## Completion, authorization, and projection contracts

Known protected-context expiration is handled before driver Commit. The
independent Rollback path owns cancellation, finalization, and the single
ownership-mutex release. Successful rollback returns the original cause with
the confirmed-rollback marker; failed cleanup returns the original cause with
the real cleanup error and no success marker. Flush failure follows the same
contract. Caller cancellation does not replace the protected write context.

Scan staging still honors caller cancellation before its new protected-context
check. An early return does not publish afterCommit, pass, or seal state.
Independent cleanup, conditional physical high-water auditing, cleanup/DDL
uncertainty, and capacity-driven discard retain their existing rules. A
cancellation can still race the final check and driver call; the change does
not turn an attempted or uncertain Commit into a known rollback.

The following final SQL observations remain despite Z21:

- BeginRun's readback and its start-clock/runtime-limit calculation.
- Nonterminal task aggregate readback and the zero-update read result.
- The aggregate observation when RecoverRuns locks no active parents.
- Publication fencing's final parent SELECT, including the nil-deadline case.

Database rollback does not prove that published files can be removed or that
source workers have exited. Filesystem identity and containment checks,
generation barriers, private ownership scopes, completion-capacity
reservations, and actual worker retirement remain independent obligations.

Narrow projections intentionally stop observing errors in omitted fields.
Examples include unused capability profile fields, hidden execution selections,
and unrelated account or metadata display columns. Invalid selected fields
still fail. Full capability writes, complete User/settings contracts,
execution/recovery validation, publication authority, and post-wait/final
authorization retain their existing responsibilities. No result is cached
across requests or mutable authority states.

The waveform, background-preview, and subtitle-timeline handlers keep their
Enabled checks and durable queue-first writes before separate Start admission.
Omitting nested target decoding can allow a request to remain queued when
Start subsequently rejects a corrupt current Run. This is the selected
projection boundary; the queue is not folded into the Start transaction.

Settings PATCH still validates and encodes changed maps. Empty and byte-identical
patches retain final authorization and deferred rollback, including a
deletion-only no-op against a missing document. Its temporary empty row is not
committed. HTTP preflight remains before body validation; JSON is emitted
without an Encoder newline or byte-slice base64 conversion.

## Linux-only server boundary

The review found no remaining Windows server implementation to delete, and
this follow-up makes no Windows-code-removal claim. Linux server and launcher
selection, Linux filesystem/process implementations, Linux release compilation,
and Linux ELF delivery checks remain in place.

Windows development-host tooling, client declarations, invalid-path fixtures,
third-party conditionals, and historical archive rules remain because they do
not add Windows server support. The linux && !amd64 launcher rejection is an
unsupported-Linux-architecture boundary, not a Windows server fallback.

## Performance observations

The fixed shared-ancestry benchmarks ran remotely without race instrumentation,
with Go 1.27.1, Linux/amd64, GOMAXPROCS=2 and a 200 ms benchmark duration. They
compare the retained independent-walk reference with the selected implementation
in the same compiled source. They are microbenchmarks, not whole-server
throughput or end-to-end latency measurements.

| Fixture | Reference ns/op | Selected ns/op | Reference allocations/op | Selected allocations/op |
| --- | ---: | ---: | ---: | ---: |
| 100 ancestors and 1,000 Audio leaves | 6,552,816 | 164,876 | 9,001 | 24 |
| Notification chain of 16 folders | 7,510 | 1,835 | 33 | 4 |
| Notification chain of 100 folders | 328,792 | 10,489 | 689 | 4 |
| 100 notification ancestors and 400 removed leaves | 2,856,921 | 54,824 | 4,289 | 4 |

The last notification fixture visits 500 unique nodes instead of 45,450 nodes
across repeated walks. Its complete selected envelope takes 151,516 ns/op;
there is no matched historical envelope benchmark, so no envelope speedup is
claimed. Both ordinary benchmark processes and package terminal events passed.
Go does not emit individual Test-pass events for these benchmark names; the
runner's raw incomplete benchmark labels are retained and are not counted as
passing or unfinished ordinary tests.

Movie Similar was measured with 1,200 generated movies and two shared seed
genres. Three alternating warm-cache EXPLAIN pairs use ANALYZE, BUFFERS,
TIMING OFF and jit=off. The reference removes only the candidate-ID restriction
from the current relation; it is an exhaustive authorized scoring reference,
not a separately built historical server.

| Fixture | Reference execution ms | Selected execution ms | Expanded credits |
| --- | --- | --- | --- |
| Three matching movies | 5.064, 5.140, 5.137 | 1.803, 1.714, 2.533 | 4,802 to 14 |
| All 1,200 movies match | 6.681, 6.701, 6.810 | 8.328, 8.305, 8.341 | 4,802 to 4,802 |

The dense fixture is 22.5%-24.7% slower in these samples. Repeated theme and
extra-resource exclusion index probes account for most of the extra shared
buffer hits. This is a measured tradeoff, consistent with the review's warning
that an index-driven candidate plan need not win for dense seeds. There is no
unconditional Similar latency claim.

Two temporary Go overlays compared moving the candidate join after narrow
scope materialization and placing an IN predicate inside the candidate branch.
Both preserved complete qualifying scores, but neither stably removed the
dense-fixture cost. They remain diagnostic evidence only; the selected source
retains the direct entity-index candidate search. No adaptive threshold, live
authorization cache, or global planner setting was added. Original and
diagnostic plans are retained separately.

## Review and verification status

The historical review completed fourteen rounds. Rounds 12-14 found no new
recommendation or actionable call-site extension, satisfying that review's
three-consecutive-empty-round condition. Three static implementation
cross-review rounds also found no new recommendation. Selected regressions,
all six complete package suites, performance experiments, and Linux builds
finished successfully. Runtime verification found no defect requiring a
source change or reopening the completed implementation review.

| Implementation round | Completed focus | New actionable recommendation | Consecutive empty rounds |
| --- | --- | --- | ---: |
| 1 | Rotated review of production changes, authority/error boundaries, graph budgets, task state transitions, and HTTP consumers. | None. | 1 |
| 2 | Independent reverse-path review of projections, missing/disabled targets, protected completion, cancellation, and mutable snapshot ownership. | None. | 2 |
| 3 | Final complete caller and rollback-marker audit, scope/documentation reconciliation, and measured performance limits. | None. | 3 |

The two diagnostic Similar alternatives did not change the selected source.
Any later runtime failure reopens the relevant review instead of inheriting
this static closeout decision.

| Stage | Recorded status |
| --- | --- |
| Implementation candidate | `candidate-01`; all 6,480 non-documentation inputs match the formatted Git export exactly. |
| Formatting | Remote gofmt completed for all 99 changed Go files. |
| Selected package verification | All six packages passed: 122 parent tests and 262 subtests, with zero skips, failures, or incomplete tests. |
| Complete notifications/settings/tasks suites | Passed: 254 parent tests and 382 subtests, with zero skips, failures, or incomplete tests. |
| Complete identity suite | Passed: 283 parent tests and 706 subtests, with zero skips, failures, or incomplete tests. |
| Complete library suite | Passed: 1,708 parent tests and 3,645 subtests, with 17 skipped parents and zero failed or incomplete tests. The real-media capacity test processed 10,000 files and passed its cached-rescan and catalog checks. |
| Complete server suite | Passed: 1,374 parent tests and 3,013 subtests, with 50 skipped parents and zero failed or incomplete tests. |
| Linux builds | Server and command launcher passed with CGO_ENABLED=0, GOOS=linux and GOARCH=amd64. The optional embedded administrator build is outside this change's build evidence. |
| Performance evidence | Fixed graph/ancestor benchmarks and sparse/dense Similar profiles are recorded above; diagnostic alternatives do not change the candidate. |
| Verification closeout | All workers exited. Final source comparison found no differences in the 6,480 non-documentation inputs. Owned compiler scratch is empty; shared caches, source, data, and raw evidence are retained. |

The combined full-suite inventory contains each test once across identity,
library, notifications, server, settings, and tasks. Selected regressions and
diagnostic reruns are separate evidence and are not added to these totals.

| Full-suite scope | Passed | Skipped | Failed | Incomplete |
| --- | ---: | ---: | ---: | ---: |
| Unique parent tests | 3,619 | 67 | 0 | 0 |
| Unique subtests | 7,746 | 0 | 0 | 0 |

All six workers returned exit code zero and a real package-pass event. There
are no unstarted packages or incomplete test families. The 67 parent skips
retain their original environment and opt-in reasons; skips do not count as
passes. Per-package results and every raw skip output are retained in
`evidence/final-verification.json` and the immutable phase manifests.
The skips comprise 58 unselected performance, diagnostic, or workload opt-ins,
two unselected AMD/AV1 configurations, two helper entry points, and five
individual nonroot, read-only mount, audited mount-namespace, owned diagnostic
directory, or provider-preview overlay conditions.

The source archive SHA-256 is
`6435b5e5ac620dde7c3ea3a049be25d68d3231c5131898029492b8e22c64ba3b`.
The remotely formatted Go archive SHA-256 is
`97a1c7c906e1063f5a72c18ee145aae857f577071c3166360d2a98fa05ef6a0e`.
The canonical formatted Git export, including the declared CRLF conversion
for the unchanged PowerShell export tool, matches all 6,480 non-documentation
inputs in the retained remote candidate. Its archive SHA-256 is
`703cfced8142ce0a9d4daa69c0a22f175da19433338e7e70d8c7396f4deb78bf`.

| Linux amd64 binary | SHA-256 |
| --- | --- |
| goby | `caa5c97425b8b104838d55e59196955d0c3aeb215e0ca7844dd68cb6d7671558` |
| goby-command-launcher | `88932f7beeda5628cf8d8f39dabc52f72d25aefd01d0fdc9f90d7a6fe3f88fa0` |

Raw manifests, JSONL logs, query plans, diagnostic overlays, source archives,
and built binaries remain under
`/opt/goby-backend-followup-fixes-20261011`. The selected media fixture root is
`/opt/gbff11` on ext4. The effective shared caches are
`/root/.cache/go-build` and `/root/go/pkg/mod`, with the pinned executable at
`/opt/goby-toolchains/go1.27.1/bin/go`. No local test, build, runtime probe, or
formatting invocation was used.

Added or extended coverage targets capability summaries and control policy,
session-history projections, managed-user locks, transaction-owned authority,
canonical user settings, metadata ownership and provider projections, both
owned completion paths, scan graphs and collection invalidation, Similar
candidate scoring, viewing statistics, task projections/persistence/receipts,
notification fanout, HTTP contracts, and settings snapshot ownership. The
phase results above distinguish executed coverage from the recorded skips.

Verification remains remote-only under the project instructions and
[test-env capacity policy](test-env-capacity-policy.md). Ordinary runs reuse
shared Go caches. Selected tests, benchmarks, and builds ran serially. After a
fresh capacity and fixture-isolation audit, complete identity/library and
server suites use two bounded lanes with independent PostgreSQL databases.
Each lane retains GOMAXPROCS=2, GOMEMLIMIT=1GiB, -p=1 and -parallel=2. A shared
global lock excludes serial work and closeout, and a lane lock prevents a
second worker in either lane. Admission retains the 9 GiB growth budget and
6 GiB disk floor and requires at least 3 GiB of MemAvailable. Both GOTMPDIR and
TMPDIR remain on the short ext4 fixture root; no private cache copy is made.

Closeout completed at 2026-10-11 04:05:32 UTC after the exclusive task lock and
fresh process inspection confirmed that no owned worker remained. The exact
compiler-scratch target contained zero allocated bytes before and after
closeout, so there is no reclaimed-space claim. Root filesystem availability
was 15,323,607,040 bytes at both observations. No private compiler cache was
created, and the shared build/module caches were not cleaned. Source snapshots,
archives, binaries, raw evidence, database resources, and the selected ext4
fixture root remain separately retained. The receipt is
`evidence/closeout.json`.
