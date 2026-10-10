# Backend design review continuation, 2026-10-10

## Scope

This is a fresh review loop requested after the
[previous increment](backend-design-review-increment-20261010.md). HEAD remains
`d9214417`; the previous U01-U07 recommendations remain unimplemented. The
current dirty working tree, rather than only the committed release, is the
review target. Existing source modifications and experiments are preserved.

Four parallel tracks cover authority, defensive/resource behavior, performance,
and Linux boundaries. New findings are deduplicated against U01-U07 and the
earlier R/N/L/Q/S/F/C/T review and implementation records. Counterexamples and
production callers are checked independently before retaining a suggestion.

All evidence is static. Counts below describe successful code paths or explicit
limits, not measured latency or production frequency. No tests, builds,
benchmarks, local runtime probes, remote verification environment, or source
changes are part of this review. The proposed changes still require appropriate
verification on `test-env` before acceptance.

## Recommendations

| ID | Priority | Area | Recommendation | Status |
| --- | --- | --- | --- | --- |
| V01 | P3 | Permission checks | Omit the duplicate session-existence SELECT in the nonlocking file-mutation check. | Actionable simplification |
| V02 | P2 | Credits analysis | Measure bounded reuse of successful tail fingerprints after a fresh full-content digest and current source/tool checks. | Conservative candidate; measure before implementation |
| V03 | P3 | Subtitle scanning | Skip stable text-subtitle UPSERTs within a scan that also contains changed tracks. | Actionable simplification |
| V04 | P3 | Music metadata | Decode the same locked accepted-music snapshot once per metadata synchronization. | Actionable simplification |
| V05 | P3 | Recovery control | Avoid copying the old payload when CAS consumes only its verified reference. | Actionable simplification |
| V06 | P2 | Task scheduling | Defer an unselected analysis run once instead of repeating the same election for its waiting children. | Actionable architecture improvement |
| V07 | P2 | Task fairness | Extend the original two-run selection policy to fair rotation across three or more eligible media runs. | Proposed scheduling-policy improvement |

### V01: Remove only the nonlocking existence query

`internal/library/media_mutations.go:96-105` selects a session ID and discards it.
The following query at lines 109-110 uses the same session ID, user ID and kind
to select liveness, device and database time, with its own missing-row and
database-error handling. When `lock=false`, the first query acquires no row
lock and establishes no additional result needed by its caller.

Execute that first SELECT only when `lock=true`, retaining `FOR SHARE` and the
separate subsequent clock-based observation. Preserve user/policy checks and
their order, all final and post-wait calls to this helper, and the independent
application-key branch. Do not merge the locked SELECT with its later fresh
authorization query or reuse authority across operation phases.
The preceding user query still flushes an owned transaction's pending journal,
so omitting the later existence query does not move journal writes after final
authorization.

Production consumers include `ItemCapabilitiesFor`
(`internal/library/item_capabilities.go:75`) and deletion inspection's initial
and final checks (`internal/library/media_mutations_target.go:107-111`). A
nonempty ordinary capability batch saves one query; successful deletion inspection can
save two. Collection and physical/subtitle mutation final checks also use the
nonlocking path. No latency percentage is established.

Remote coverage should include wrong user/kind, missing/revoked/expired
sessions, native versus Emby policy, expiry after a blocked credential lock,
application keys, final mutation authorization and SQL counts for both lock
modes. This differs from policy parsing reuse and narrow session DTOs: the
removed statement has no consumer and the complete authority query remains.

### V02: Reuse tail analysis without silently weakening content checks

Analysis admission groups at most 16 targets within up to 32 total source
members, including targets and support, per child
(`internal/library/analysis_admission.go:470-490`). For a valid
24-episode season with every episode selected, two children both contain all
24 sources. `internal/server/analysis_credits_executor.go:80-90` processes every
source in each child: line 167 computes a full-file digest and line 180 runs
tail-audio fingerprint extraction. Under successful, audio-eligible inputs this
means 48 digests and 48 tail extractions. The audio window is the final 450
seconds or the complete shorter source (`internal/media/analysis_credits_skipper.go:45-50`).

A bounded cache dedicated to credits raw fingerprints could reuse successful,
nonempty extraction results within one non-Force run. Key it by the run and
actual freshly computed full-content digest, source revision, selected audio
stream, exact tail window, algorithm/options and tool identity. Keep the
existing full-file digest on every reuse attempt. Do not reuse cohort matching,
target-specific visual results, publication decisions, Work/Fence values, or
another child's authorization context. Do not put tail features into the
prefix-only intro cache.

The original broader idea of also skipping full-file hashing is not accepted
as a behavior-preserving cleanup. `analysisSourceDigest` actually reads all
bytes, detects short reads/I/O failures, and checks before/after file identity,
size, mtime and ctime (`internal/server/analysis_source.go:60-108`). Reopening
and checking metadata does not reproduce those observations.

Cache hits also need current audio-tool proof. The execution profile is a
startup snapshot, whereas extraction normally reopens/verifies the tool and
checks it afterward (`internal/media/analysis_credits_skipper.go:83-101`).
Replacing/removing the audio executable between children must not be hidden
by an earlier successful result. The separate visual executable is not a
substitute for this proof. Preserve its current executable capability probes,
not just a digest comparison: current library-loading or execution failures can
occur without changing the executable bytes. Recheck source state and context
after those probes, keeping their actual process/I/O retirement.

Keep each child's execution authorization, root approval, I/O admission,
cohort/settings/manual-revision checks and final publication fences. Force
bypasses reuse. Do not cache empty/unavailable/error/canceled results. Publish
immutable entries only after successful source/process retirement and final
checks; never retain descriptors, process handles or an authority capability.
Publish after the extraction/read helper returns successfully, because deferred
closure can still replace its apparent success with an error.
Give the runtime cache explicit entry and total-byte limits plus bounded key
data and eviction. One raw fingerprint is capped at 5000 uint32 points, but a
run can admit 100,000 sources, so run isolation alone is not a memory bound.

Measure 16/24/32-source runs, Force, misses, repeated overlap and realistic
storage. The conservative proposal may reduce the example to 24 tail
extractions while retaining all 48 content digests; this is conditional on
eligible successful cache hits without intervening eviction, not a measured
speedup. Required tool capability probes still execute on hits. Test changed media,
mid-file I/O failure, tool replacement, source/root revocation, distinct child
authorization, cancellation and retirement failure. If these checks or cache
maintenance outweigh repeated extraction, keep the existing implementation.

### V03: Avoid SQL for unchanged tracks in a mixed subtitle scan

The scanner already has an all-unchanged fast path
(`internal/library/subtitles_scan.go:492-511`). Once any track differs, the
sorted loop at lines 528-563 sends an UPSERT for every inspected track.
`ON CONFLICT ... WHERE IS DISTINCT FROM` prevents identical row updates but
still executes the statement and conflict checks.

Use the existing `previousByPath` and `subtitleScanSnapshot.matches` at line 34
to skip retained matching entries inside that loop. Its equality covers the
stored root/path/identity, ctime, content tag, size, mtime, codec, language,
title, flags and MIME type. The map contains only active, present tracks with
indices above the embedded-stream maximum. The item is already locked and the
operation runs through catalog ownership.

With 32 accepted tracks and exactly one changed track, up to 31 statements can
be omitted. This applies only to mixed-change scans; the all-unchanged case is
already optimized. The SQL already avoids identical row updates; no additional
row-update or WAL reduction is established here.

Preserve freshly inspected bytes and all physical facts, directory completeness,
retirement of missing or colliding identities, permanent retired-index rules,
deterministic index allocation, owned/bitmap capacity, warnings, before/after
projection comparison and final physical proof. Matching only Tag or stat is
insufficient. Keep the original conflict predicate for entries still written.

Remote coverage should combine changed and stable tracks, new/deleted tracks,
embedded-index collisions, private metadata changes, incomplete directories,
owned/bitmap tracks, capacity limits and final source replacement. Count SQL
statements while checking identical public notifications and stored rows.

### V04: Decode the accepted-music snapshot once under its existing lock

`internal/library/metadata_scan.go:34` reads `music_source` while locking its
metadata row. The same byte slice is passed to `acceptedMusicSourceHash` at
line 47, `acceptedMusicName` at line 52 when extracted facts exist, and
`mergeAcceptedMusicSource` at line 85. Each helper independently calls
`decodeAcceptedMusicSource` (`metadata_music_source.go:40,90,105`). Extracted
sources therefore undergo three strict decodes in one synchronization; unread
sources still pass through the hash and merge decoders.

Decode once in `syncScannedMetadata` and use private typed helpers for these
three operations. Keep the standalone byte-oriented helper APIs self-validating
for metadata-management and provider callers. This requires no persistent
cache, new authority state, or relaxed parser.

Preserve the exact distinction between unextracted empty data and extracted
empty facts such as `{"Version":1}`. In the unextracted branch, merge returns a
byte clone of the original local data without interpreting it. Extracted empty
facts have a nonempty canonical hash and can clear accepted music fields.
Continue hashing the canonical decoded struct, not the incoming JSON text.
Retain validation limits, unknown-field rejection, array/text order, NFO name
and sort precedence, administrator overrides and all existing write fences.
Keep error evaluation order and do not incidentally share a mutable local-NFO
map between helpers.

Remote tests should preserve the accepted-music source suite's formatting,
duplicate names, exact text/number behavior, invalid and maximum input cases,
unextracted byte preservation and metadata integration results. Measure parsing
allocations before claiming a scan throughput benefit.

### V05: Keep old recovery payload copies at the public boundary

`internal/recoverycontrol/store_linux.go:379` calls `readCurrent` during CAS.
That verified read ends at line 348 with `recordSnapshot`, which copies the
complete payload for a noninitial record (`format.go:180-184`). CAS then uses
only the old revision and digest; it never consumes the copied old payload.
Startup's load path also discards the returned snapshot at line 255.

Split the private fully verified read from public snapshot construction. CAS
can use its verified `recordReference`; public Read and the successful new CAS
result must still receive independent payload storage. This removes one unused
old-payload allocation of up to 1 MiB per noninitial CAS. It does not remove
current-file reading, hashing or payload parsing required on a cache miss.

Preserve root/proof/current identity and digest checks, directory inspection,
all before/candidate relationships, cancellation, CAS comparison, publication
ordering and uncertain-write behavior. A successful CAS still advances its
revision even for identical payloads. Do not expose `parsedCurrent` payload
storage. Keep the distinction between the parsed encoded candidate and the
public CAS return: JSON escaping can change the retained representation.
N17/F07 already optimized digest/parsing work, but require independent copies
at actual public boundaries, not this unused intermediate result.

Remote coverage should retain public payload isolation across Read/CAS, stale
digests, tampering, expanded/escaped payloads, failed publication/reopen, and
maximum valid records. Measure allocations separately from filesystem cost.

### V06: Avoid repeated analysis-run election for an unselected backlog

`internal/tasks/execution_manager.go:96-102` calls `nextAnalysisRun` for each
waiting analysis child when the shared analysis group has no owned execution.
If another run is selected, it returns `false, nil`. The caller continues to
the next child and consumes another unit of its bounded pass budget
(`internal/tasks/manager.go:575-619`).

Consider an old run A with at least 200 waiting children, previous analysis run
A, an eligible run B, and the pass cursor at A. Each election selects B, but A
can consume the default 200-child budget with repeated queries before B is
visited. The five-second cycle deadline can terminate the pass earlier. The
run cursor progresses, so this is extra database work and delayed dispatch,
not permanent starvation. `nextAnalysisRun` itself is a separate pool query
(`internal/tasks/execution_store.go:15-21`).

Represent deferral of only the current run separately from global queue-full.
When a waiting child belongs to an unselected run and the analysis group is
idle, stop dispatching more waiting children of A and continue the outer run
loop. Do not reuse the existing
`queueFull` result: that also breaks the outer loop and would still postpone B.
Do not add a long-lived cached election or treat the selected ID as authority.

Keep shutdown/stopping behavior, independently reaped executions, detection of
running children without owned workers, runtime deadlines, child/run cursors,
and the single analysis group. Keep selection policy separate from this
traversal optimization; V07 addresses its multi-run limitation. The special deferral is
only for a waiting child of a nonstopping run with no owned analysis execution
and a different selected run. Final database claims, execution tokens, fresh
child authorization and publication fences remain unchanged.
Active child pages can mix waiting, queued and running entries. Before
deferring, retain the checks for nonwaiting entries in the fetched page or
handle them independently; do not skip a missing-worker invariant failure.
Keep the final RefreshRun and charge/advance cursors only for actually examined
children, not an entire page that was fetched and then deferred.

Verify the actual `Manager.reconcile` loop with a large A backlog and eligible
B, not only direct calls to `reconcileExecution`. Cover selected-run
cancellation, pending-to-running transitions, busy groups, max concurrency,
non-analysis work, stopping, deadline expiry and cursor wraparound. Assert
bounded election queries and prompt eligible dispatch without weakening claims.
R09/R10 address different trigger/aggregate work and do not resolve this path.

### V07: Rotate fairly across more than two eligible analysis runs

`internal/tasks/execution_store.go:17-21` orders candidates by
`(r.id = previousRun), r.created_at, r.id`. This moves the previously served
run behind all others but always selects the oldest remaining run. With three
eligible runs ordered A, B, C, the sequence after A is B, then A, then B again
while both older runs retain waiting children. C cannot be selected until A or
B drains or otherwise becomes ineligible.

This is a valid configuration: active-run coalescing is per task definition
(`internal/tasks/runs.go:82-84`), while six media task keys share one execution
group (`internal/tasks/analysis_admission.go:50-53`). The outer runOffset cursor
does not solve the election bias: visiting C still reaches the same global
selection and returns a mismatch. Finite admitted backlogs and cancellation can
eventually release C, so this is not a claim of inevitable permanent starvation.
It can nevertheless delay a later media task behind substantial older work.
BeginRun also starts its configured runtime clock before acquiring an execution
slot (`internal/tasks/runs.go:321-352`).

The [original analysis contract](analysis-task-contract.md) explicitly describes
two keys and avoiding the previous run with creation/ID tie-breaking. Current
SQL follows that rule; this finding proposes an explicit policy extension for
the six-task group, not a claim that the original two-key rule was implemented
incorrectly. The existing alternating-run integration test
(`internal/tasks/analysis_integration_test.go:756`) covers two analysis runs and
one unrelated provider, not three members of the same group.

For the current fixed group, rotate through its six task keys, selecting the
next key with an eligible run after the last successfully served key and
wrapping when needed. Definition keys are unique and each definition has at
most one active run (`0019_scheduled_tasks.sql:5,111-112`). This bounded rotation
does not chase newer run IDs forever when short tasks repeatedly finish and
restart. Preserve those uniqueness assumptions explicitly.

A plain `(created_at, id)` cursor is insufficient for general arrival fairness:
new B/C runs can continually appear after the cursor, preventing wrap to a
still-waiting old A. If run-order rotation is preferred, bound each round's
membership or upper watermark and defer later arrivals to the next round.
Do not present the unbounded tuple walk as a general fairness solution.

Advance scheduling state only after a successful claim and worker registration,
at the current lastAnalysisRunID update boundary (`execution_manager.go:131-133`),
not when merely considering or deferring a candidate. A later execution-time
authorization failure still consumed that claim; it must not pin selection to
the same failed actor. No durable last-served table is required for the proposed
in-process policy. Define restart reset behavior without changing RecoverRuns
or reusing old execution tokens.

Keep pending/running plus waiting-child eligibility, current selection queries,
MaxConcurrent, the one-worker analysis group, cancellation, deadlines, reaping,
transactional claims and per-child authorization/publication. Do not execute a
cached run ID without its claim. Do not filter out already-admitted work because
its definition is disabled or its executor is unavailable; keep its existing
execution/failure lifecycle. Update the scheduling contract together with
the implementation. V06 reduces repeated election work but does not alter this
ordering, so the two recommendations are independent.

Remote verification should run at least three distinct media task definitions
with multiple children and prove that C starts before A/B exhaust, both through
selection tests and the real reconcile loop. Include one eligible key, wrap,
continuous new runs of other keys, removal/cancellation of the cursor run, stopped candidates,
failed claims, restart, large backlogs and unrelated provider work. No fairness
latency bound or measured throughput improvement is established here.

## Retained boundaries and Linux cleanup

No Windows server implementation remains to remove. Server entry build
constraints and the native analysis helper build restrict targets to Linux;
release compilation fixes GOOS to Linux,
and current OCI profiles target linux/amd64. `launcher_other.go` rejects an
unsupported Linux architecture; it is not a Windows fallback.

Vendored Chromaprint/KissFFT `_WIN32` branches remain upstream source under
checksum-pinned provenance, behind a Linux-only CMake entry. Windows client
metadata, developer-host tools, independent receiver clients and historical
archive/migration contracts do not extend server support. Current root opening,
media deletion, editing, image access and restore preserve the already-fixed
Linux colon/backslash contracts. URI and loader-path delimiter checks serve
their own Linux/protocol purposes.

Settings reads through catalog ownership extend the already-recorded read-owner
opportunity; they are not counted again. Backup registry lookup/sum and
notification JSONB capacity sums remain profiling leads. Full cohort
revalidation and whole-file digest removal lack an equivalent-observation proof
in this pass. Keep actual process/descriptor retirement, revocation and final
publication checks rather than treating every repeated observation as redundant.

## Review-loop ledger

New independent recommendations reset the counter. Clarifications to existing
findings and rejected hypotheses do not count as new recommendations.

| Round | Focus | New recommendations | Consecutive rounds without new recommendations |
| --- | --- | --- | --- |
| 1 | Fresh authority, recovery/storage, scan/analysis and platform review, deduplicated against prior records. | V01-V04 | 0 |
| 2 | Cross-check credential query semantics, source/tool proofs, subtitle writes, music decoding and recovery snapshots; inspect scheduler dispatch. | V05-V06; V02 narrowed | 0 |
| 3 | Adversarial checks of mixed child states, current tool capability, source retirement, copy boundaries and scan accounting; source-to-report reconciliation. | None | 1 |
| 4 | Reverse API/data-ownership and release inspection; three-run counterexample to the inherited two-key selection policy. | V07 | 0 |
| 5 | Cross-check V07 with continuous arrivals and claim failures; refine its solution to bounded task-key rotation and verify all six earlier proposals. | None; V07 solution refined | 1 |
| 6 | Source-to-report audit of query/allocation bounds, capability batches, WAL wording, fixed-key uniqueness and all retained authority/resource boundaries. | None | 2 |
| 7 | Final independent production-caller, error, publication, resource-retirement and Linux-support audit of the complete report. | None | 3 - stop |

All four tracks and the coordinating review completed rounds 5-7 without a new
independent finding. Seven new recommendations were retained across seven
rounds; U01-U07 from the previous increment were not counted again. The requested
exit condition is satisfied. V02 remains a candidate requiring measurement and V07
requires an explicit update to the original scheduling policy; neither is
presented as an already-verified implementation.

## Verification status

This task performed source inspection only. No local verification was run, no
remote environment was prepared, and no caches or scratch were created or
cleaned. The only repository addition for this continuation is this report.
Existing uncommitted source changes and the previous report remain intact.
