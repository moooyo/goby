# Backend review implementation, 2026-10-09

## Scope

This task implements the recommendations in
`backend-design-review-20261009.md`, reviews the resulting changes, verifies
them on `test-env`, and publishes the verified result to `main` as authorized
by the user. It starts from `4509e3be` in an isolated worktree on
`codex/backend-review-fixes-20261009`.

The earlier Linux-only cleanup is included. The original checkout's unrelated
scan, native-media, HLS, measurement, and historical-documentation changes are
excluded and preserved. The implementation and delivery candidate must not be
reconstructed from that dirty checkout or confused with its earlier test run.

## Implementation map

| Finding | Change | Retained contract |
| --- | --- | --- |
| N01 | Explicit shared-device lookup/options/delete lock modes | Target device SHARE/tombstone barrier, actor locks and final authority; deletion still locks the complete credential generation |
| N02 | Page-level analysis inventory facts, settings, support, previews and deduplicated season reads | Repeatable-read consistency, exact detection/cohort semantics, corrupt-record rejection, and fresh final administrator authorization |
| N03 | No full directory audit for no-op backup deletion results | Fixed root/marker/lock/catalog identity and digest checks; complete inventory audit before any deleting-marker publication |
| N04 | Context-aware lifecycle State read and recovery callers | Complete authority/history verification, explicit cancellation errors and real cleanup ownership |
| N05 | Ready channels only for queued I/O admission | Per-chunk accounting, ordered fairness and cancellation compensation |
| N06 | Bounded independent webhook senders and paged user-data scheduling | Per-registration real worker ownership, claim/fence handoff, transport join, per-user ordering, dirty markers and overflow resync |
| N07 | Targeted terminal-history pruning plus infrequent fallback | Fast lease recovery, revocation and source-journal reclamation |
| N08 | Batch old dynamic-subtitle artifact retention checks | Fresh grace, no lease renewal, successor filtering, reader/quota accounting and conservative errors |
| N09 | Linux-only backend cleanup | Windows clients/development host, effective feature errors, historical data and Linux kernel capability checks |
| N10 | Content-keyed successful embedded-artwork inspection cache | Actual-byte hashing, metadata, publication, physical source and permission checks on every access |
| N11 | Reuse immediate open-time FileInfo | No-follow/nonblocking opening, subsequent audits and meaningful later observations |
| N12 | Explicit media-operation summary/status/candidate projections | Full default reads, candidate-page validation, capabilities, private receipts and complete compensation journals |
| N13 | Typed batched OCR cue UPDATE with row-count verification | Full review validation/hash, revision lock, actor checks and atomic rollback/commit |
| N14 | Bounded first/latest cancellation-error bookkeeping | Sticky original callback/wait/final-join errors, unknown-retirement status and actual capacity retention |
| N15 | Restore the initial in-memory generation intent on a proven unpublished cancellation | Post-mkdir and post-rename uncertainty retain their recovery barriers |
| N16 | Batch member facts and sharing validation/inserts | Input/error order, playlist duplicates, folder expansion, ACL predicates and account-lock order |
| N17 | Reuse the verified recovery-control record digest | Independent payload copy and every filesystem/publication/CAS observation |
| N18 | Skip unused preferences for fully explicit sort requests | Existing bypasses, scope/sort validation, query-field presence and separate user configuration |
| N19 | Metadata-only inspection of inert lifecycle temporary debris | Ownership, mode/link/type/size/identity/cancellation bounds; authority files still fully verified |
| N20 | One provider-cache catalog-change batch | Reference order, mutation identity, auxiliary changes, resync, limits and whole-transaction rollback |
| N21 | Explicit authority-only session revalidation | Original full default contract, policy, observed clock, role, peer, credential/device binding, expiry and activity facts |
| N22 | Combined ready-preview source/reference revalidation | One complete physical proof, expected-source error order, a fresh reference transaction, cache checks and final credentials |

The report's existing-projection follow-through is also implemented. NowPlaying
uses an explicit direct-item projection without private seek indexes or unused
user data; its remaining presentation fields and complete default API survive.
LibraryChanged uses a stored-item permission projection. It does not expand
virtual expected episodes or reinterpret a stored expected-looking ID.

## Deliberate behavior boundaries

- Shared-device rename still holds the target device UPDATE lock. Authentication
  of the same device can wait at its device SHARE boundary; this change does not
  promise that the entire rename is nonblocking.
- Backup Delete now skips inventory enumeration for NotFound, digest Conflict,
  and Busy outcomes. Fixed-role identity checks remain first, and every mutation
  still performs the full audit. Unknown directory entries may be detected by
  the next complete audit instead of a no-op request.
- Fully explicit sorting no longer depends on unused stored preference JSON.
  Inert `.next-*` debris no longer tests its unused payload blocks for readability.
  These are selected behavior changes, not claims of identical failure timing.
- Notification history written through its store is pruned on terminal change.
  External terminal mutations, such as identity/recovery writes, converge via
  startup and hourly fallback. Fast authority/lease/source-capacity work remains
  at the existing polling cadence.
- Provider batching can change gaps and absolute values caused by intermediate
  uncommitted journal refreshes. Committed content, order and cursor coverage
  remain the contract; contiguous sequence values are not required.

## Implementation review

Review rounds search the implementation and its callers, challenge proposed
fixes, and deduplicate the same issue across call sites. The exit condition
remains three consecutive complete rounds without a new actionable finding.

| Round | Outcome | Consecutive empty rounds |
| --- | --- | --- |
| 1 | Replaced the initial N02 cross-season outer sort with pipelined single-season statements consumed one result set at a time. Added batch-tracer, error-drain and population-bound regressions. Clarified N03's no-op audit timing. Other completed groups had no blocking finding. | 0 |
| 2 | Restored the original sharing-recipient SHARE lock on the fresh batched validation query. The initial ordered account-lock observation must not be assumed to cover every later visible recipient. Add a deterministic PostgreSQL concurrency regression; query count and normalized error order remain unchanged. | 0 |
| 3 | Cross-reviewed the fixed sharing lock, the real pgx pipeline lifetime, source/authority projections and runtime shutdown contracts; no new actionable issue. | 1 |
| 4 | Restore one transaction around notification maintenance state changes and their targeted history pruning. A pruning failure after an implicit batch commit otherwise loses the changed-registration set until a later change or hourly fallback. Add a PostgreSQL fault/retry regression. | 0 |
| 5 | Checked the maintenance transaction against real pgx close/rollback behavior, concurrent authorization and sender ownership, and the new failure observer; no new actionable issue. | 1 |
| 6 | Rotated review of field consumers, closure/exception behavior, corruption, batching, retention and shutdown ownership; no new actionable issue. | 2 |
| 7 | Final independent source/caller/test-contract review across permissions, defensive work, performance and platform integration; no new actionable issue. | 3 |
| 8 | The complete remote matrix exposed an existing source-range mutation fixture that did not establish an observable ctime transition. Add a bounded helper loop preserving inode, size and restored mtime; production checks and the expected rejection remain unchanged. | 0 |
| 9 | Independent review of the fixture's metadata premise, bounded retry, process result and descriptor ownership found no new issue. | 1 |
| 10 | Require the parent test to observe the isolated ctime transition unconditionally. An early descriptor-duplication failure can also return ErrInvalidInput and otherwise pass without executing the mutation. | 0 |
| 11 | Checked the unconditional parent premise against early input errors and a missing production ctime check; no new actionable issue. | 1 |
| 12 | Rotated review of payload/path stability, real descriptor closure, process/parse error order, workload bounds and independent assertions; no new actionable issue. | 2 |
| 13 | Final source-equivalence and contract review found no unresolved issue. The fixture's actual-mutation premise and original proof/offset assertions remain independent. | 3 |
| 14 | The next package exposed a pre-existing generation-close fixture that equated closed-pool counters with live backends. Replace that assertion with the borrowed connection's closure/cleanup and exact closed-pool admission rejection, preserving the lease/drain barriers. | 0 |
| 15 | Give each final closed-pool admission check a fresh bounded context; a spent cleanup context can mask ErrClosedPool. Apply the same corrected metric to five existing startup-test sites while preserving their independent lease, error and PostgreSQL backend observations. | 0 |
| 16 | Checked fresh context isolation, exact closed-pool errors, returned unexpected borrowers and preserved startup barriers; no new actionable issue. | 1 |
| 17 | Rechecked all five call sites, active-pool capacity assertions, source/target ownership and separate client/server closure observations; no new actionable issue. | 2 |
| 18 | Final independent review confirmed only three test files changed after candidate three, with all production contracts and resource ownership intact; no unresolved issue. | 3 |
| 19 | Supplemental startup execution exposed an immediate readiness assertion racing asynchronous scheduler initialization. A diagnostic-only candidate reproduced the exact tasks_not_ready response. Bound the fixture's wait and retry only that declared startup state; production readiness remains unchanged. | 0 |
| 20 | Verified the single readiness budget, exact retry classification, actual HTTP success and per-attempt resource cleanup; no new actionable issue. | 1 |
| 21 | Challenged persistent and unrelated failures, all helper call sites and unchanged ownership/identity observations; no new actionable issue. | 2 |
| 22 | Final independent review across the four test-only corrections found no unresolved issue or weakened production contract. | 3 |

The initial implementation review reached three complete empty rounds in
rounds 5, 6 and 7. Review resumed when complete-matrix execution exposed the
source-range fixture issue, reached three empty rounds again in 11 through 13,
and reopened for the generation-close fixture in round 14. Rounds 16, 17
and 18 were three consecutive complete rounds with no new actionable issue.
Supplemental opt-in startup execution reopened review in round 19. Final rounds
20, 21 and 22 had no new actionable finding, satisfying the requested stopping
rule. Test source inspection remains distinct from the executed verification
below.

## Remote verification

All compilation, tests, runtime checks, query observations and race checks were
performed on `test-env`. No local tests, builds or runtime probes were run.
The first candidate received a centralized remote formatting pass before its
verification manifest was selected.

Persistent capacity initially had only about 1.1 GiB free. A specifically
selected idle maintenance pass checked current worker liveness, completed owner
records, canonical path, ownership and the shared Go cache layout, then used
the pinned Go executable with the exact shared GOCACHE to remove regenerable
compiler output. Shared modules, source, databases, media and raw evidence were
preserved. The pass increased persistent availability to about 6.04 GB.

Verification used an initially empty, budgeted private tmpfs build cache and
the shared module cache; no cache was copied. Compiler scratch was separate
from actual ext4 test fixtures. Database and FFmpeg/FFprobe fixtures were enabled
for the relevant regressions. Package, test, skip, failure and source identities
are retained with the actual results.

The first candidate (`d5def4754efc9357a4bd597b775a15f22452a0ce`) compiled all
41 packages and three commands. All 83 newly selected top-level regressions
executed without skips: 82 passed and one N02 test failed. Its small first
result could remain in PostgreSQL's output buffer while a later injected
`pg_sleep` ran, so the deadline expired before the intended Scan error was
reached. The fixture now returns 4096 real first-rowset records and asserts
that Scan ran exactly once. Error-preservation, child-cancellation, healthy
caller-context and pool-reuse assertions are unchanged; production code did
not change for this fixture repair.

The second candidate (`6601125c01ea9005506f49fee649d88a6128f33f`) includes that
fixture repair and the sharing-lock regression, for 84 new top-level tests.
Its prioritized pipeline-failure test passed all three subcases and the
parent, without skips. The original failure and both candidate identities are
retained separately from subsequent full-suite results.

The second candidate then compiled all 41 packages and three commands and
passed all 84 selected new top-level regressions across eleven packages, with
241 passing test/subtest records, no skips and no missing selected tests.
Full-suite and race admission paused for the round-4 maintenance transaction
repair; these focused results are not presented as a complete matrix.

The selected primary-I/O allocation benchmark ran three times on this
candidate. The prepared path consistently reported 112 B/op and one allocation;
the try path reported 160 B/op and three allocations. No old-source benchmark
was run, and these figures do not establish a latency or throughput improvement.

The third candidate (`55b916f555aa7f34bca0197a7073475968327100`) includes the
maintenance transaction repair and its new regression, for 85 selected new
top-level tests. The prioritized statement-error and context-deadline cases
both passed on PostgreSQL, including their parent result, without skips.
It then compiled all 41 packages and three commands and passed all 85 new
top-level regressions across eleven packages: 244 passing test/subtest records,
zero skips and zero missing selected tests. Full verification started from this
frozen source; later test-only corrections and their package-scoped reruns are
recorded below. Production source remained unchanged after this candidate.

The first complete-matrix attempt stopped at the backup fixture admission guard:
its newly provisioned database names did not use the required `goby_backup_`
prefix. No restore ran in that attempt. The verification environment was
corrected with fresh, independently owned low-privilege database/role pairs,
and the complete matrix restarted as `full-r3-retry1` on the unchanged third
candidate. The original guard failure and fixture-disposal evidence are retained
separately; this was an environment correction, not a product-code repair.

The complete third-candidate run passed 35 packages, then its transcode package
reported one failing mutation subtest and its parent. The source-range helper
wrote once and restored mtime without proving that ctime had advanced. The
production source-range function, identity fence, descriptor duplication and
original fixture were unchanged from the base commit. Neighboring existing
fixtures already wait for an observable ctime transition. A same-clock-quantum
mutation is the supported static explanation; the original failure did not
record the before/after timestamp values, so it is not a measured timestamp
trace.

The fourth candidate (`7ad450809a838e1ced8eccd337ab4afa3c27d46b`) changes only
that existing test relative to the frozen third candidate. Its 7,981-file
manifest SHA256 is
`b2dabe43ac2b0b803e796aefc32384e4b8f367350f32589ef4fa54dd49c14b72`.
Remote formatting made no further change. Its helper retries within a two-second
budget until inode,
size and restored mtime remain equal while ctime differs, and closes its writer
before publishing evidence. The parent independently requires that transition,
excluding early ErrInvalidInput failures that never executed the mutation. The
zero-result, error-class and borrowed-offset assertions remain unchanged.
Production source is identical, so successful third-candidate package results
remain applicable. The transcode package is rebuilt and rerun in full, followed
by the packages not reached in the stopped run and the planned race/helper work.

The original third-candidate binary repeated the mutation case 100 times:
73 passed and 27 failed, with no skips. This measures fixture instability,
not the exact timestamp mechanism. After the fourth-candidate rebuild, the
same 100-run selection passed 100 times with zero failures or skips, including
the new unconditional parent premise. Both runs and the earlier complete-matrix
failure remain separately retained.

The complete fourth-candidate transcode package passed with 2,690 passing
test/subtest records, 20 conditional skips and no failures. The previously
unreached `cmd/goby` package then reported one failure in its generation-close
fixture, alongside 61 passes and six conditional skips. The failed assertion
read local `pgxpool.Stat().TotalConns()` values, not PostgreSQL backend state.
Repeating the original single test 50 times passed all 50 without skips; this
does not supersede the original complete-package failure.

Independent dependency inspection found that pinned puddle v2.2.2 can finish a
resource constructor after pool closure, destroy its value, and retain its idle
bookkeeping record. A separate, task-owned remote diagnostic reproduced that
branch deterministically: two constructors and two destructors completed, Close
returned, but TotalResources and IdleResources remained one. The module cache
was not modified. This establishes the invalidity of the counter assumption;
the original failure did not record enough detail to identify its particular
pool or close-time interleaving.

The fifth candidate (`e32406f35412aa44a11b80bb38d243c2f9ac2279`) changes only
`cmd/goby/generation_playback_control_integration_test.go` relative to the fourth
candidate. It preserves the timeout, deployment-lease and borrowed-capacity
barriers, then checks the captured physical control connection's IsClosed and
CleanupDone, and requires both pools to reject Acquire with ErrClosedPool.
Post-close counters remain diagnostic output only. Production source and the
previously verified package sources remain unchanged.

The fifth-candidate command regression passed 50 repeated runs, then its full
package passed with 62 passing records, six conditional skips and no failures.
The independently owned recovery database suite passed all 206 records without
skips. The fifth-candidate manifest SHA256 is
`bd1631b14a5322b5c98b2aea476c974b43fb31d866041287f20a8c90345d04d9`.

The sixth candidate (`5ce703f4ba5927fbd4937c695ee74d789142d0aa`) adds a small
shared test assertion with its own five-second context for each closed-pool
admission check. It also replaces five occurrences of the same invalid counter
assumption in `generation_startup_playback_control_linux_test.go`. The original
expected errors, lease and drain ordering, and PID/database-OID backend checks
remain intact. Only these two command test files differ from candidate five;
production and all other package test sources remain byte-identical.

The sixth candidate's 7,981-file manifest SHA256 is
`234ed3ad81cdc61413669d468e12816e560d8fbb814a0d86029733ae60d8be9f`.
Remote formatting made no change. Its command regression passed 50 repeated
runs, and its complete command package passed with 62 passing records and six
conditional skips. The combined complete matrix covers 38 packages with tests
and three packages without tests: 21,839 passing test/subtest records, 131
conditional skips and no final failures. Its package evidence combines the
unchanged candidate-three packages, candidate-four transcode, candidate-five
recoverydb and the final candidate-six command package. Equivalence was checked
using actual source hashes, not assumed from package names.

All five planned race groups passed: primaryio (83 records), lifecycle (64),
notifications (12), selected media retirement cases (46), and selected server
notification/user-data cases (26). The total is 231 passes, zero skips, zero
failures and no reported data race. The Linux intro-fingerprint helper compiled
with GCC 14/CMake and passed all seven protocol tests; no install or publication
was performed.

The five modified startup scenarios require a disposable PostgreSQL administrator
over a Unix socket. The existing system cluster's authentication did not meet
that fixture contract. A separate task-owned PostgreSQL 17 cluster therefore
used a private 0700 socket directory, local trust, rejected host authentication,
and no TCP listener. The system cluster configuration remained unchanged.

The first supplemental run passed four scenarios and failed the returned-source
readiness assertion in the fifth, with no skips. All ten temporary databases
and ten temporary roles were removed by the fixture, and post-run connection
and resource inventories were empty. A diagnostic-only seventh candidate
(`a55f2e24292684cb52fc068ff3f34c66de413d8f`) retained the original one-request
assertion and added bounded, allowlisted Error.Code reporting. Its first exact
reproduction returned HTTP 503 with tasks_not_ready; no additional diagnostic
repetitions were attempted after that failure.

The scheduler's existing NewManager contract initializes asynchronously, and
the preceding pool-budget test can temporarily occupy all normal Data slots.
The listener can therefore serve before scheduleReady is published. Candidate
eight (`db26f32365b765e6f56ca55b27217b0dd50234b7`) changes only the fixture to
wait within one five-second context, retrying HTTP 503/tasks_not_ready at 25 ms
intervals and requiring eventual HTTP 200. Other errors, unknown codes, lost
leases, server exits and budget expiry still fail. Each response body closes
immediately; the production readiness contract is unchanged.

Candidate eight's 7,981-file manifest SHA256 is
`bf7c5ca87a611e378e32be734a9275c369c373ccefb74b47828c42990330aee6`.
Remote formatting made no change. Its complete ordinary command package passed
with 62 passing records and six conditional skips. The five exact startup
scenarios then all passed with zero skips in the isolated cluster. The formerly
failing returned-source scenario also passed five additional repetitions, for
six successful executions on the final binary. These supplemental runs cover
five of the original matrix's 131 skips; 126 other conditional skips remain.

The final startup runs created and removed 20 databases and 20 roles with
matching DDL audit records. Post-run inventories contained no fixture database,
role or connection, and the private cluster was stopped. Source, independent
binaries, cluster data and raw success/failure evidence are retained separately.
After actual worker exit, final closeout reduced the private compiler cache
from 306,212,864 bytes to 8,192 bytes; compiler scratch and ext4 runtime fixtures
were empty. Persistent availability was about 4.360 GB and tmpfs availability
about 3.069 GB. Earlier cleanup receipts remain intact rather than being
overwritten by this supplemental closeout.

## Delivery

The commit containing this report is the delivery candidate. Its production and
test sources match the final verified candidate; only this implementation
record is updated after verification. The original checkout's 28 unrelated
tracked changes and 131 unrelated untracked files are excluded from the commit.

Delivery uses a fast-forward merge to `main` and a normal, non-forced push. The
exact commit and remote ref, task-owned cleanup stash identity, and before/after
preservation results are recorded in the local task artifact directory at
`.artifacts/backend-review-fixes-20261009/delivery/`. Complete verification
counts, conditional skip reasons, historical failures, manifests and closeout
receipts are retained under the same task artifact root, separately from Git.
