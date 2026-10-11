# Backend review implementation, 2026-10-11

Implementation follows the [fourteen-round review](backend-design-review-20261011.md).
The accepted scope includes Y01-Y19, U01-U07, and the remaining R01, V01, R20,
and Q06 call sites. T09 and the report's explicitly deferred profiling leads
remain outside this implementation.

The implementation starts from `0b7d95f2cdac62aebe85b2e0eb43af64c8f2df57` in
a separate worktree. The original working tree contains unrelated development
work. Its tracked and untracked files were saved before editing; only the
specific Y06 and Y12 regressions were restored there.

## Implemented changes

| Findings | Result and retained boundary |
| --- | --- |
| Y01, U01 | Domain projections can omit response-only user data and entity source counts. Dedicated entity count methods skip page materialization. Native/default reads remain complete; state filters, sorting, family membership and subject authorization retain their existing queries. |
| Y02 | Application-key validation and the immediately following key-row lock use one complete predicate. The separate parent-session lock, key-ID comparison, lock mode and final clock observation remain. |
| Y03 | An explicit successful schedule ownership probe can cover idle reconciliation. Backoff is not a probe; active executions, durable active work, shutdown and manual admission retain their fresh checks and independent budgets. |
| Y04, U07 | Progress and completion reuse their transaction's already locked, unchanged Run before reading fresh child totals. Freshly decoded task selections normalize in place; copies across lifecycle boundaries remain. |
| Y05 | Packed AAC/MP3 HLS skips unchanged manifest rendering after fresh parsing, prefix validation and segment-closure evidence. First publication and final ENDLIST remain explicit; only successful publication advances the marker. |
| Y07, Y08 | User deletion reuses the target session IDs already locked in order. The native user list selects its six summary fields; full ListUsers consumers retain the original projection. |
| Y09 | SQL-only capture has no PostgreSQL command dependency. Dumping checks pg_dump; decoding/restoring checks pg_restore. Creation still performs both dump and independent archive validation. Required decoder resolution remains before destructive target preparation. |
| Y10, Y11, U05 | Notification master admission reads one ordered witness, while recovery validates every secret. Registration mutations return their six fields directly. Database management-lock errors remain unavailable rather than unauthorized. |
| Y13 | A same-schema restore omits only the unchanged middle resource pass. Raw archive validation, post-migration validation, finalizer validation and cancellation boundaries remain. |
| Y14 | Successfully validated embedded catalog baselines and migration facts are cached by version. Callers receive independent nested slices; large Objects documents and live database observations are not cached. |
| Y15, Q06 | Each recovery transaction configuration batches all eleven transaction-local settings, consumes every result and closes the batch. An elapsed deadline cannot return success while ctx.Err is still unpublished. |
| Y16, Y17 | Dedicated collection reads reuse their authorized parent in the same snapshot. Playlist previews check candidate overlap with EXISTS rather than transfer every existing entry; expansion limits, duplicates and failure/commit semantics remain. |
| Y18, Y19 | DTO presentation options are prepared once per response. Optional capability queries run only when a capability field can survive final projection. Actual download/deletion authorization remains separate. |
| U02, U04 | Unchanged append-only transaction references skip re-encoding after a successful journal write. Notification projection skips the query for an absent reference category while retaining input and subject checks. |
| U03 | Ordinary device renaming uses account/session/device coordination without the global management lock; removal and application-key device operations retain their original protocol. |
| U06 | Dynamic lease Validate shares fresh authority and activity checks with Info without copying metadata. Info and returned Input.Lease values still own independent copies. |
| R01, V01 | Playback authorization reuses parsed policy facts only inside one locked observation and repeats final expiry/schedule decisions. Read-only subtitle-provider checks omit only the redundant credential existence query. |
| R20 | Generation files and their optional master path come from one complete retained-history verification under the lifecycle store gate. Consumers retain sensitive-byte cleanup and path-loader requirements. |

Y03 intentionally allows an owner disconnect occurring between two otherwise
idle phases to remain unobserved until the next explicit probe. This does not
remove active-work, shutdown, manual Start/Stop, or owned-write detection.

Omitting an unrequested attachment or an absent reference-category query also
omits that query's independent error path. Main-query, filtering, authorization
and transaction-completion errors remain visible.

## Existing correct behavior restored in the original worktree

Y06 and Y12 were already correct in the committed baseline. The implementation
branch does not transplant unrelated dirty changes to manufacture those fixes.

The original working tree's HLS maintenance now again reserves a bounded Stop
recovery opportunity before slow active-session checks consume the cycle. The
original recovery integration test was restored. An added test covers zero,
one and four eligible reservations without cancelling the enclosing cycle.

The original scan change now again uses the exact folder's known local-image
absence. Its other image-inspection edits remain intact. The image trace now
counts the heavy `image_catalog_unchanged` comparison as well as publication
work, so the no-image regression cannot pass merely because it avoids writes.

The Linux-only server boundary is already present: both Goby and its command
launcher select Linux, release compilation fixes GOOS to Linux, and the native
fingerprint helper rejects other operating systems. No additional Windows
server implementation was found. Windows development-host tools, client
metadata, invalid-path fixtures and historical archive interpretation remain.

## Recovery readiness

The existing status fields now distinguish backup creation from new restore
planning. A missing dump tool no longer prevents decoding an existing archive.
Imports, downloads and deletion retain their own admission checks without
requiring either command.

An already verified ready plan and a retained rollback generation can switch
without decoding again. The administrator page preserves target configuration,
service/storage health, exact generation, candidate capability, busy state,
unknown-result and confirmation guards. The
[native API contract](../api/backups.md) documents the resulting field meanings.

## Review and verification

All tests, formatting, builds and runtime observations ran on `ssh test-env`.
No local verification was used.

The first independent implementation round identified a remaining UI gate
that blocked verified Apply/Rollback when the decoder was missing. It was
corrected together with positive and negative browser cases. Later rounds
rotated reviewers across modules and checked callers, reverse failure paths,
the final formatted source, and documentation against implementation.

| Implementation round | Result | Consecutive rounds without a new recommendation |
| --- | --- | --- |
| 1 | Correct the missing-decoder gate for already verified Apply/Rollback. | 0 |
| 2 | Correct two newly added test assumptions exposed by Linux execution; no production change. | 0 |
| 3 | Full caller, authorization, transaction and resource-boundary review; no new defect. | 1 |
| 4 | Rotate reviewers across modules and challenge error/cleanup paths; no new defect. | 2 |
| 5 | Final implementation/documentation and caller closure; no new defect. | 3 |
| 6 | Repair an existing browse-route SQL tracer that still matched the retired episode queue CTE. | 0 |
| 7 | Review the current projection query and all HTTP/domain consumers; no new defect. | 1 |
| 8 | Challenge projection, duplicate-ID, order and privacy assertions; no new defect. | 2 |
| 9 | Close the formatted tracer correction and its complete parent-test scope; no new defect. | 3 |
| 10 | Isolate an existing subtitle test's asynchronous system-event consumer from its read-only row-count snapshots. | 0 |
| 11 | Verify manager shutdown, worker joining, catalog lifetime and repeated cleanup; no new defect. | 1 |
| 12 | Independently check HTTP side effects, authorization and neighboring task-admission coverage; no new defect. | 2 |
| 13 | Review the final formatted test and its complete retained boundaries; no new defect. | 3 |

The final three consecutive implementation review rounds produced no new
actionable recommendation. Verification discoveries reopened the relevant
review rather than inheriting an earlier stop decision.

The first selected checks exposed two new test assumptions: an already observed
master-file replacement returns the established Unsafe error, while a fresh
vault rejects mismatching ciphertext; the generic backup fixture's extra
public schema is rejected by whole-database recovery inspection. Both tests
were corrected without weakening production behavior or its error boundaries.

The later subtitle failure was reproduced five times with a retained diagnostic
Go overlay. The six counters changed from `[1 2 0 0 0 0]` to `[1 2 0 0 0 1]`.
The extra row was a completed `media.subtitle_timeline_generation` run admitted
from the system event emitted by the test's explicit publication helper. The
read-only parent test now closes and joins its task manager before publication
and observation. Its real catalog, HTTP authorization, source/manifest checks
and all six row-count assertions remain. The corrected complete parent passed
under the race detector.

The final composed race evidence covers the exact 43-package Linux inventory:

| Scope | Passed | Skipped | Failed | Incomplete |
| --- | ---: | ---: | ---: | ---: |
| Unique parent tests | 6,690 | 126 | 0 | 0 |
| Unique subtests | 16,590 | 3 | 0 | 0 |

Three compiled packages have no test functions. Skips retain their original
reasons, including separately selected performance experiments, native helper
or oracle fixtures, hardware/media profiles, disposable startup environments,
mount namespaces and nonroot permission scenarios. Skips do not count as passes
or resolve an earlier failure.

The initial full command returned failure. Its 42-package log is retained,
including the server package's 30-minute timeout. The compiled server inventory
contains 1,415 parents: 1,066 initial passes and 34 skips were retained, and all
315 failed, unfinished or unstarted parents were selected as complete families
for continuation. That continuation completed with 297 passes, 17 skips and
the subtitle fixture failure described above. The final correction supplies
the remaining parent pass. The resulting inventory has no missing or unexpected
parents, and every previously observed member of an unfinished family has a
real terminal result.

The initially selected minimal FFmpeg 9.0.1 lacked raw Chromaprint, libx265,
AV1 encoder options and zscale required by existing media fixtures. A complete
media-package repeat and all eight failed transcode parent families passed
with the recorded complete FFmpeg/ffprobe 9.0.1 prefix and the separate
Chromaprint executable. Tool hashes are retained in `full-media-tools.json`.
The complete recovery database package ran last against the idle owned pair
and passed in 125.922 seconds.

All 51 initially failed parent/subtest records have explicit subsequent pass
evidence. The later subtitle failure is also resolved. The evidence merger
preserves original package failures, timeout observations and skip reasons;
it rejects missing members, unfinished commands and unaccounted package-level
runtime failures. This is composed verification from recorded commands.

The administrator production build and 44 Chromium backup-page interaction
cases have passed on the Linux candidate. Those browser tests intercept
administrator API calls and do not claim an additional live deployment journey.
Selected race checks cover all ten changed Go modules, including the complete
ready-plan apply, restart acceptance and retained rollback path with both
PostgreSQL command paths missing.

The normal Linux amd64 server, the server with the production administrator
bundle embedded, and the Linux command launcher have built successfully with
CGO disabled. Their SHA-256 values are recorded below.

| Binary | SHA-256 |
| --- | --- |
| `goby-linux-amd64` | `0e6b1b3ec98630b40ceb8c369ccaef41f08ca4ab55701db042a4392c70438a71` |
| `goby-embedded-linux-amd64` | `9ce282f4f64f7aa2f0963d738eecb5e1ff77857e03db76338622ced5dc9264b3` |
| `goby-command-launcher-linux-amd64` | `88932f7beeda5628cf8d8f39dabc52f72d25aefd01d0fdc9f90d7a6fe3f88fa0` |

All 6,451 inputs outside `docs/` match the final formatted `candidate-04`
exactly. Its only differences from the built `candidate-02` are the browse SQL
tracer and subtitle fixture test corrections; production and frontend inputs
are identical. The final source archive SHA-256 is
`45f147775ff7f1769fafa70cab66af46f0c0dd2c8cbc018c1750b9c7856938af`.
The final input-manifest SHA-256 is
`8873bedea79628be57c3cdeca15d73b398579e2a6d35e2c4ddf6368aa6622038`.

## Allocation measurements

After the verification workers exited, 22 in-memory benchmark cases ran
serially with Go 1.27.1, `GOMAXPROCS=2`, `-benchtime=200ms` and three samples
per case on the Linux AMD Ryzen AI 9 H 365 worker. The table reports median
bytes and allocations per operation.

| Comparison | Bytes/op | Allocations/op |
| --- | ---: | ---: |
| Lease `Info` to `Validate`, 2 streams | 1,376 to 0 | 5 to 0 |
| Lease `Info` to `Validate`, 256 streams | 167,936 to 0 | 513 to 0 |
| Packed HLS render to unchanged guard, 1,000 segments | 209,165 to 0 | 4,017 to 0 |
| Per-item to per-response parsing, 1,000 items, small query | 5,040,198 to 4,145,093 | 66,001 to 42,025 |
| Per-item to per-response parsing, 1,000 items, large query | 14,648,209 to 4,186,668 | 123,001 to 45,079 |

Both DTO modes construct the same response data. A one-item response has the
same allocation count in both modes. Per-item parsing is a conservative
comparison and does not reconstruct every redundant parse in the old code.
The lease benchmark uses a stub authorizer; the HLS benchmark measures only
formatting and the unchanged-state guard. These observations establish local
allocation savings, without measuring endpoint latency, database work, source
validation, media parsing or filesystem publication.

## Remote capacity and retained evidence

The task owns `/opt/goby-backend-review-fixes-20261011`. It uses Go 1.27.1
from `/opt/goby-toolchains/go1.27.1/bin/go`, the pinned FFmpeg/ffprobe tools,
and independently owned disposable databases and nonprivileged roles. The
server continuation used a third database to avoid the original worker's
locks. Recovery verification subsequently used that idle source and the owned
target, with the actual loopback PostgreSQL port selected explicitly.

Ordinary runs reuse `/root/.cache/go-build` and `/root/go/pkg/mod`. No cache
was copied. Both GOTMPDIR and TMPDIR point to the task's ext4 fixtures for
combined compile/test execution; standalone builds use separate compiler
scratch. Source archives, logs and binaries have separate directories.
The server continuation and final recovery tests used the recorded short
ext4 fixture root `/opt/gbrf11` on the same filesystem.

Admission observed about 19.2 GiB of persistent free space and budgeted 11 GiB
for source/archive copies, cache growth, compiler output, media fixtures,
databases and raw evidence. The existing nearly full /tmp tmpfs was not used.
No other task's cache, source, fixtures, database or service was removed.
The final budget reserved 2 GiB for source/archive snapshots, 4 GiB for shared
cache growth, 1 GiB for compiler output and binaries, 3 GiB for media fixtures,
and 1 GiB for databases and evidence.

Raw logs, source archives, formatting receipts and worker records are retained
under the task root. Final receipts are `evidence/verification-receipt.json`,
`evidence/verification-summary.json`, `evidence/benchmark-summary.json`,
`evidence/candidate-04-final-inputs-match.json` and `evidence/closeout.json`.
The verification summary SHA-256 is
`3a8fcb63c7790ff0ff8517d664bf09aeb232487e2aee3d372dead1a06a6c0321`.

Fresh closeout inspection found no remaining task worker. The confirmed empty
compiler scratch directory was removed, reclaiming its 4,096-byte directory
allocation. No private compiler cache existed. The shared Go cache remained
unchanged at 6,148,419,584 allocated bytes, and persistent free space was about
15.79 GiB afterward. Source snapshots, binaries, databases, media fixtures and
all successful and failed raw evidence remain available separately.

