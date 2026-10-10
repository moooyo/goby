# Backend post-continuation implementation, 2026-10-10

## Scope

This implements the actionable recommendations and R02 follow-through from
[the post-continuation review](backend-design-review-post-continuation-20261010.md).
The user authorized implementation, verification on `test-env`, integration
into `main`, and push. Work starts from `737c382d` in an isolated checkout on
`codex/backend-post-continuation-fixes`. The original checkout's unrelated
uncommitted scan, media, playback, release and research changes are preserved.

Only the review report is copied from the original dirty checkout. Production
changes are based on the committed source, not on the unrelated experiments.
All executable verification and Go formatting take place on Linux `test-env`.
Local work is limited to source editing, reading and static Git inspection;
no local tests, builds or runtime probes are run.

## Change map

| Finding | Implementation | Retained boundary |
| --- | --- | --- |
| W01 | Recover a missing initial process identity from the original exec-owned process handle. | No arbitrary PID recapture, exact identity/group proof, unreaped leader, copier join and actual capacity retirement. |
| W02 | Normalize only changed positions and move only the affected interval; omit same-position updates. | Full ordinal calculation, gap repair, hidden entries, duplicates, deferred uniqueness, collection touch, Resync and final authority. |
| W03 | Build the private canonical policy-field schema once. | Every raw policy parse, canonical keys/types, runtime fallback and independent mutable results. |
| W04 | Describe unsupported transcoding operations without a false Linux requirement. | The same error sentinel and unsupported output-plan behavior. |
| W05 | Project configured playback from both existing deny sources and explain the blocking rule. | Separate persisted fields and independent edits; dynamic playback conditions remain separate. |
| W06 | Use configured-anchor I/O admission and the existing finite directory-worker lifetime for root readability. | One-entry read, configuration order, current authority, named identity, cancellation, actual syscall ownership and shutdown drain. |
| W07 | Transfer rendered provider-preview buffers to the existing count/byte budget before releasing image processing. | Bounded download/render, final metadata authorization, nonwaiting handoff, retained capacity including HEAD, idle-write deadline and exact release. |
| R02 | Use an explicit independent single-statement administrator read in five standalone observation helpers. | Transaction-local mutation authorization, audience, peer/key/client policy, fresh database time and precise denials. |

W07 was selected after the original-source controlled backpressure observation
confirmed the shared-processing-slot interference. The private test download
callback supplies fixture bytes; no provider token, live TMDB request or broader
online-provider acceptance is involved.

W01 deliberately retains the conservative result where Go has no original
process handle. It does not recover an arbitrary numeric PID or broaden the
supported kernel/handle contract. On the selected Go 1.27.1/Linux profile, the
original exec-owned handle permits recovery after descriptor pressure ends.
Tests separately reject an unowned bare-PID process object.

R02 keeps authority denials, SQL errors and query-time context errors distinct.
Connection admission failures and actual lost connections carry an explicit
unavailable marker; a cancellation observed after a successful query also
prevents success. This deliberately makes infrastructure failure classification
consistent rather than preserving an incidental BEGIN-versus-SELECT distinction.

W06 preserves unavailable configured directories becoming available later,
including dangling aliases. At startup, only ENOENT permits resolving an
existing ancestor and a missing suffix into a fixed canonical anchor. Relative
and absolute symlink targets retain their components until symlinks preceding
`..` are resolved. The explicit fallback symlink budget is shared across the
recursive work. Actual availability still requires the original configured
path to resolve successfully and match that anchor; an intended future anchor
alone cannot establish readability. Alias changes do not rewrite the I/O route.

W07's native preview consumer now displays a local failure and offers manual
retry. Retry remounts the image with its original URL and never applies an image,
changes the draft index, appends query parameters or starts automatic retries.

## Verification environment

The remote task root is `/opt/goby-backend-post-continuation-20261010` on ext4.
The selected executable is `/opt/goby-toolchains/go1.27.1/bin/go`; FFmpeg and
ffprobe use `/opt/goby-toolchains/ffmpeg-9.0.1/bin`. PostgreSQL is 17.11 and
Node.js is 20.19.2. Tests use separate task-owned non-superuser databases;
credentials stay in mode-0600 environment files on the remote host.

Ordinary Go runs reuse `/root/.cache/go-build` and `/root/go/pkg/mod` with
`GOMAXPROCS=2` and package parallelism one. Compilation uses task-owned
`compiler` scratch. Test execution binds both `GOTMPDIR` and `TMPDIR` to
task-owned ext4 `fixtures`, preserving filesystem semantics. Source, binaries,
raw logs and receipts are retained separately.

The admitted persistent budget is 8 GiB with a 10 GiB availability floor.
Initial available persistent storage was approximately 25.6 GiB; tmpfs was
not selected because its remaining capacity was small. The capacity guard
checks fresh observations between phases. There is no private compiler-cache
copy, shared-cache cleanup, dependency pruning or unrelated environment cleanup.
The source/archive/frontend-dependency allowance was increased from 1 GiB to
2 GiB using the plan's reserve; the total and availability floor did not change.

## Confirmed component observations

W02's original-source negative control and candidate use the same isolated
collection fixture and update audit. The audit counts actual attempted entry
updates, including same-value updates, without treating a rollback as zero work.

| Fixture | Original source | Candidate |
| --- | ---: | ---: |
| 10,000 entries, one-position move: position UPDATE rows | 20,000 | 2 |
| Same fixture: unchanged rows updated | 19,998 | 0 |

The original-source regression fails on those expected counts; it is a
successful negative-control observation, not an unresolved candidate failure.
The candidate's five new position regressions and existing selected collection
regressions passed. These counts do not measure end-to-end HTTP latency or
eliminate the full ordinal scan.

The identity package passed 246 top-level tests, and its selected race profile
passed 21 tests, with no skips. The independent administrator-read tests observe
three SQL statements for the transaction-wrapped form versus one for the new
read, across native, Emby and application principals. Current authority and
failure-path tests remain part of those results.

The same parser benchmark is run against the original and changed policy
implementation with three representative inputs. Each candidate case removes
35 allocations and approximately 3 KiB per parse. Allocation counts are
51 to 16 for the legacy input, 408 to 373 for the populated input, and 94 to 59
for runtime playback fallback. This is a parser component measurement, not
an authentication latency result.

W01's ordinary and race profiles each passed 26 top-level tests, including
helper entries, with no skips. The six capture-recovery scenarios execute in
isolated child processes, including actual file-descriptor exhaustion and
retained-copy-writer/descendant cases. Race and ordinary counts overlap; they
are not separate unique behaviors.

W07's baseline holds four real preview handlers at an unread `net.Pipe` write.
Only the download call is replaced with fixed fixture bytes; rendering,
authorization, output and image budgets retain their production code. All four
processing slots remain occupied, and a warmed ordinary-image request does not
complete during the 250 ms observation window. Releasing the blocked writes
allows it to complete. This demonstrates the coupling under controlled
backpressure, not a production throughput or network-latency result.

The candidate observes zero occupied processing slots, four active transfers
and 5,046,272 retained bytes while those preview writes remain blocked. Its
ordinary-image request completes in approximately 10.3 ms during the blocked
window. The original completes only after release, approximately 267.4 ms in
that run. The byte budget, not the nominal image dimensions, bounds retained
output. Five new preview regression groups and the selected existing
image/provider regressions also passed.

The final frontend build, including TypeScript checking, passed. The complete
management-wave browser file passed on desktop, with an additional 390 px
configured-playback case: 14 passing cases and no skips, including manual
preview retry after a 429 response. Retained screenshots
confirm that the configured-permission explanation wraps without obscuring
account controls or fixed actions. The first dependency installation failed
on a TLS connection reset; its cached retry succeeded. No code test ran in
that failed dependency attempt.

## Implementation review and final verification

The combined candidate is reviewed again after implementation. New defects
reset the consecutive clean-round count. Rounds 3, 4 and 5 completed without a
new implementation defect, satisfying the three-round stopping condition.

| Round | Outcome | Consecutive empty rounds |
| --- | --- | --- |
| 1 | Found missing configured directories below existing/dangling aliases that would not recover, and corrected the fixed startup mapping. Hardened FD-pressure test synchronization and bounded descendant cleanup. | 0 |
| 2 | Found premature lexical cleaning of symlink targets containing `..`, which could select the wrong allowed root. Preserved raw components and added wrong-target rejection cases. Added the missing manual UI recovery path for preview transfer rejection. | 0 |
| 3 | Rechecked actual authority consumers, root mapping, process retirement, result ownership, collection writes and provider output. A missing-before-`..` candidate was rejected against r2's existing preservation of the original ENOENT. No new defect. | 1 |
| 4 | Cross-checked current response/error contracts, five independent authorization consumers, fixed routes, cancellation, transfer cleanup and manual retry. No new defect. | 2 |
| 5 | Final pass over original-handle recovery, actual worker/descriptor retirement, immutable schema, independent policy edits, selected measurements and UI consumers. No new defect. | 3 - stop |

The final selected results are deduplicated by package, top-level test name and
mode. Counts include the recorded helper invocations; they are not claims of
that many independent product scenarios.

| Package | Ordinary passed | Race passed |
| --- | ---: | ---: |
| media | 26 | 26 |
| library | 33 | 11 |
| server | 95 | 17 |
| identity | 246 | 21 |
| recovery | 3 | 0 |
| transcode | 5 | 0 |
| Total | 408 | 75 |

There are 483 passing package/name/mode combinations. The race cases overlap
ordinary cases. The W07 measurement intentionally skipped its ordinary
invocation without the measurement flag, then passed explicitly on both the
baseline and candidate. There are no unresolved selected failures or skips.
The W02 original-source count assertion failed as expected and is retained
separately as a negative control.

The recovery profile includes real backup, planning and cancellation work.
The W06 HTTP regressions include six missing/alias cases, including deliberately
created wrong lexical targets that must never be accepted as library roots.
The final `goby` build with `goby_embed_admin` and the command launcher build
passed. The embedded dashboard comes from the verified final frontend output;
166 UI source files were compared before importing its 197 generated files.

Raw records include `final-verification-summary.json`,
`r2-prior-verification-source-correspondence.json`, phase-specific environment
and capacity records, ordinary/race logs, parser measurements and browser
screenshots. Earlier unchanged-source scopes retain their passing results;
source correspondence was checked before accepting them for r2.

## Retention and Git delivery

Raw evidence and administrative source-preservation records are retained under
`.artifacts/backend-post-continuation-implementation-20261010/` outside Git.
The original checkout's file-state snapshot was taken before implementation.
Only task-owned compiler scratch is reclaimed after real worker exit; source,
databases, independent binaries and raw failed/passing evidence are retained.

Final closeout observed no task workers or database sessions. Its exact compiler
scratch target was `/opt/goby-backend-post-continuation-20261010/compiler`;
the empty directory occupied 4 KiB and was removed. Shared Go caches and modules,
npm cache, source, database contents, binaries and fixtures were retained.
Approximately 22.99 GB of persistent storage and 4.59 GB of available memory
remained. Availability may also reflect unrelated activity; the exact scratch
reclamation is recorded independently.

Before integration, the committed archive is compared with the final tested
source. `main` is advanced without force, and push is authorized for that
integrated result. Final commit/remote equality and workspace preservation
receipts are stored outside Git to avoid a self-referential commit hash.
