# Media performance session handoff - October 2, 2026

## October 3 selected follow-up completed

The user resumed source integration and the metadata concurrency/analysis cleanup
repairs. Read the [October3 integration record](media-performance-integration-20261003.md)
for source6d681bc, exact provenance, targeted race checks, builds and closure.
The formerly interrupted candidate was reconstructed from retained bytes and
the two production failures are repaired and verified. This supersedes only
the first work item in the final closeout. Remaining Stop, full A/V, native,
scan-read batching, reconciliation and mixed-capacity work is not resumed here.

## Final user-requested closeout and publication

Further development and verification were explicitly stopped. Read the
[final closeout and Git delivery record](media-performance-closeout-20261002.md)
first. The ready scan commit e37b04f was pushed to main with exact remote
readback; later working-tree code remains unpublished. Source provenance gaps,
integration failures, proposed unverified fixes and outstanding work are
preserved in that record and the local artifacts.

The attempted full integration check was interrupted during server tests.
Database76 and identity201 passed; library1217 passed with one metadata
concurrency failure; media636 passed with four analysis-close failures.
The additional fixes were prepared and retained, then further checks were
canceled. The scan-read adapter also remains unpublished because retained64
ownership conflicts with the existing256 auxiliary resource contract.

The restarted private PostgreSQL PID2592733/start50794543 is now stopped,
with zero owned workers or native resources and protected services unchanged.
Remote data and all original source are retained. Temporary publication
worktrees are recoverable archives. No test/build or worker remains active,
and no continuation is authorized by this handoff.

## Checkpoint status

The interrupted media performance chat
(`01a0f4ca-72ba-7772-b0c5-28852d794584`) has been recovered and closed at the
user's request.
The original performance program is **incomplete**. This is a recoverable source
and evidence checkpoint, not seven-priority acceptance or a release.

The chat was idle when recovered, and its last turn was interrupted. Its last
recorded scan retry stopped at source-hash preflight before compilation or tests.
The final analysis PART1 v2 results had been exported but not written back to
the running state document. Both gaps are corrected below.

The workspace is `D:/Code/goby`, branch `main`, at
`e37b04f6cfeac989e21483780f21c8b21fc2c487` (`Optimize media library scan overhead`).
Extensive tracked changes and untracked implementation/test files remain in the
working tree. No commit, push, PR, image build, production deployment or feature
enablement was performed during closeout. Preserve unrelated work and all earlier
failed evidence. Do not use `git reset`, `git clean`, or a complete live-tree
overlay as a shortcut to reconstruct an accepted candidate.

## Execution constraints

- User conversation is Chinese; source, comments and documentation are English.
- Local commands use Windows PowerShell. Local testing, builds and runtime
  verification are not authorized. Verification remains on `ssh test-env`.
- The selected client target is existing Emby-compatible clients. HTTP fixtures
  alone do not qualify an actual Emby player or complete A/V playback.
- Preserve Data12 + Control4, total application16 plus the catalog lease, and
  existing process, root/domain, physical-storage and inode budgets.
- Correlated HLS ownership, early Stop, generated-window A/V qualification and
  native backend readiness remain disabled/unqualified. Failed observation is
  not process retirement, and callback completion is not child/copier join.

## Current seven-priority scope

These are the current roadmap numbers from the retained session state, rather
than the original recommendation order in the October 1 architecture review.

| Priority | Verified checkpoint | Remaining acceptance |
| --- | --- | --- |
| P1: Incremental cache accounting | Bounded maintenance, completion observer/FD retirement, unknown-accounting fences and scoped race evidence. Median maintenance ticks were 200.246 to 35.928 ms and 824.708 to 30.441 ms. | These ticks perform different bounded work from baseline full scans. Small-cache parallel reads cost 13.6% more and allocate more. Do not generalize to complete scan speed or HTTP latency. |
| P2: HLS authorization/publication | Fresh authority, final DB-clock checks, post-commit IO, publication barriers and separately matched HTTP/mixed-load increments. | Some p95/p99 pairs regress. No uniform concurrent-client tail-latency or complete service capacity claim. Preserve matched source and workload scopes. |
| P3: Pause/demand/windows and Stop | Legacy finite TS windows, real natural TTL case, default admission fix on three genuine transport paths, real row-lock early encoder retirement and retained-owner fences. | Latest recovery matrix still fails the delayed-input observer. Complete existing-client A/V, fMP4, subtitles and packed/native clocks remain unqualified. |
| P4: Process/finalization/hard storage | Runner/Manager subsets, earlier native custody/gateway, 48 native/cohort regressions and remote Windows cross-compilation. PART1 v2 result is recovered below. | Actual new collector kernel positive, RLIMIT set/readback, PART2 dual streams, writer/broker/finalizer/Manager/storage integration and full resource acceptance remain pending. |
| P5: Control capacity and actual read ownership | Data12 + Control4, aliases and earlier original/download read/projection evidence; scan adapter now compiles and has a failed focused matrix recorded below. | Two scan tests fail; publication/neighbor/mixed tests are not accepted. HLS/transcode, subtitle/provider/artwork and directory/walk readers remain outside complete read-path acceptance. |
| P6: Scan/reconciliation/short owner phase | Independent complete cold/warm/force scans, historical complete cold-scan/HLS matched run, UNION plans, staged membership and unused ID-slice reductions. | Latest single-page hint is prepared only; larger pages and final filesystem work remain inside catalog ownership. Artificially delayed phase-split results do not establish normal-throughput improvement. |
| P7: Dolby Vision streaming parser | Fixed state, 256 MiB total cap, AU/CRC checks, strict second validation and actual fixture/resource evidence. | Accepted scope is parser resource behavior, not GPU/video encoding throughput or whole-service capacity. |

Useful local evidence entry points (later reports may remain uncommitted):

- [Architecture review](performance-architecture-review-20261001.md),
  [initial priority increment](performance-priority-increment-20261001.md), and
  [follow-up](performance-followup-20261001.md).
- [Cache costs and tradeoffs](performance-cache-maintenance-20261002.md),
  [HLS authorization](performance-hls-authorization-20261002.md),
  [publication/HLS results](scan-publication-progress-hls-results-20261002.md), and
  [queue-fact/HLS results](immediate-fallback-hls-matched-results-20261002.md).
- [Stop/cohort checkpoint](stopped-hls-and-probe-retirement-checkpoint-20261002.md),
  [default admission and PCM results](playback-admission-default-and-pcm-results-20261002.md),
  [continuation results](performance-continuation-results-20261002.md), and
  [scan adapter contract](primary-scan-read-adapter-contract-20261002.md).
- Retained detailed working history:
  `D:/Code/goby/.artifacts/performance-roadmap-20261002/session-state.md`.
  The closeout section supersedes its historical live-session and goal text.

## Exact recovered execution state

### P3: Stop recovery remains a failing candidate

The earlier positive Stop matrix has source manifest
`0c60d9d0a187e555b683f500b3847dce258dd558fc6e4d3baebd67319b3c6d26`
and evidence archive
`89f39c9d99aaa0eaa7b7a3ac3e9ee0f3ef2eddc5c94108c607dbecff7961256f`.
It demonstrates exit, reap and group closure before the held user-data row was
released, then HTTP204 and scoped resource closure. Its four-failed/fifth429
case is an availability failure boundary, not recovery.

The newer recovery candidate passed three pure tests, four actual recovery
tests and the old 14 pure tests. Six of seven old actual tests passed; the
delayed-input/registration test failed because its exact encoder retirement
observer returned `known=false`. The complete run returned exit1. Source:
`25eabd868422cb709848cb3427a0dc8814cc03c3655adf932cfbb774b06c6200`.
Evidence:
`be0191406e4cdb1c8e91aa29edfaed50e0839ba0c39ffe279b91cf07bb1fbd65`.

The separately frozen observation-only helper is prepared under
`.artifacts/performance-roadmap-20261002/correlated-hls-stop-retirement-observation1`.
It was not staged or executed during closeout. It records pidfd poll facts,
stat error class/start identity and absent-group probe facts while retaining
the original strict acceptance. No actual captured errno currently explains
the failure; do not guess ESRCH or accept child exit as reap/group closure.

### P4: Analysis PART1 v2 actually ran

The latest retained archive is
`.artifacts/performance-roadmap-20261002/p4-analysis-part1-v2-evidence.tgz`.
Its SHA256 is
`2cd98f00e3397ace16f5e53f296685346a1d983863625e12dfa77f012302fee5`.
Its complete source manifest is
`5d4a89acbe6be26d3ebec7fea0c71113e2db94aaef97e48a67626a0aec240898`.

The focused race selector returned exit0 with 49 top-level PASS and 9 SKIP in
1.139 seconds. The commanddomain package race returned exit0 with 70 top-level
PASS and 9 SKIP in 2.199 seconds. Both counts include repeated tests and must
not be added as distinct coverage. The 13 formatted source checks passed.
Actual executable-capability/root-ancestor cases were skipped, so these results
do not qualify actual immutable ELF execution or kernel RLIMIT readback.

PART2 has a source freeze at
`.artifacts/performance-roadmap-20261002/p4-analysis-dual-stream5-v1.sha256`
and `p4-analysis-dual-stream-source-v1-frozen`. It has no executed acceptance.
The real new ffprobe collector still has a physical/inode budget gap under the
unchanged complete fixture budget. Preserve creator cost and the single budget;
earlier kernel gateway acceptance does not qualify this new collector.

### P5: Compile fix recovered; focused matrix now fails explicitly

The original adapter run failed compilation at
`scan_publication_progress_primary_integration_test.go:38` because
`t.Cleanup` cannot accept the new `func() error` Close method. Its evidence is
`4c6a63f8c7d8c974d4a0d37db1a98ac331c7f5698bc022fce2a8209dc9176ef8`.
The independent existing-test wrapper preserves and reports the Close error;
its raw source SHA256 is
`f07d4092f48b4d3edb07120bfc55db88312bb500f46a31ec72ce288c89078e44`.

The last original-chat retry, `run-p5-primary-scan-read-cleanup2.sh`, failed
preflight on `internal/media/process_native.go`, with no compilation or tests.
The raw-source manifest expected
`6bd4bf495a44cecfbb08aebbb16d6f2ace17a2cd2ca54bfd4147687c5d7f149f`.
The remote file was
`62a45062f2dbb5730570c715f6db3b807a5acc7b731332588d324964583213d8`:
the exact gofmt bytes already archived by both the earlier passing native-hook
batch and the first scan compile attempt. Static comparison found formatting
changes only. Neither old manifest nor failed attempt was overwritten.

Closeout created an independent source manifest adopting those already
archived native bytes, then ran `^TestPrimaryScanRead` on `test-env`. The library
compiled. Four top-level tests passed, including one helper and three substantive
integration tests; two failed; zero skipped. The package returned exit1 in
3.350 seconds. The two failed assertions are:

1. `TestPrimaryScanReadStoreCloseRetainsActualFD`, line382: the deeply nested
   `reader.sock` pathname was 116 UTF8 bytes and failed Unix socket bind with
   `invalid argument` before creating the scan fixture or starting the child.
2. `TestPrimaryScanReadWarmNoopAndJoinedOverride`, line658: scan preparation
   rejected a mismatch between task child and scan ownership/terminal snapshot.
   The intended false-marker dispatch assertion was not reached.

Static follow-up identified both fixture setup errors. The false-marker segment
changes the in-memory and database job to `ForceProbe=true`, but its immutable
parent run remains `library.scan`, which requires false. The production
association check correctly rejects that mismatch. Direct `prepare`/`probe`
needs no ForceProbe override. The earlier cold/warm calls, xmin, IO and owner
assertions in that test already passed. Neither failed case establishes a
production defect; their intended final assertions still need actual reruns.

No production or fixture source was changed during closeout. The publication
selector was gated on a successful focused matrix and therefore did not run.
The complete scan adapter and its mixed workload remain unaccepted.

New complete remote source manifest:
`26417d21031f76791e9a7e4ce5d7fab64951e7d0665a9e3c14e821830380efc8`.
New evidence archive:
`8a71cbc0c5354afa3610e6947b19bdbb59fd93800f821bf323d1910a5db42daa`.
The source manifest is retained at
`.artifacts/performance-closeout-20261002-session/session-closeout-p5-cleanup3/source.sha256`;
the archive is at
`.artifacts/performance-closeout-20261002-session/session-closeout-p5-cleanup3-evidence.tgz`.
The local working tree includes additional unstaged-to-remote WIP; this source
manifest does not certify the complete local working tree.

### P6: Single-page candidate hint remains prepared

The five-file freeze under
`.artifacts/performance-roadmap-20261002/p6-single-page-candidate-hint1`
has not been staged or tested. Its intended selector is
`^TestScanReconciliationCandidateHint` (eight top-level tests). Preserve the
fresh complete-page equality, common proof deadline, physical budget and final
cascade/Seen/filesystem proofs. A single-page fast path does not complete P6.

## Environment closure and retained data

All closeout verification used `test-env`. After the failed focused matrix
joined and its evidence was exported, the exact private PostgreSQL17 instance
was stopped with `pg_ctl -m smart -t30 -w stop`, exit0. Before stopping, its
identity was checked against PID2120236, start tick44304841, UID103, executable,
data directory and postmaster receipt. It had zero other client backends.

Final observation found zero task-owned process references through cwd,
arguments, descriptors or maps; zero reserved-UID65534 processes; and zero
owned mounts, loop devices or native cgroups. The private postmaster, pid file
and socket were absent. Shared PostgreSQL PID898/start689 and Docker
PID1452/1455/1467 retained their original identities and start times.
The additional `historical-native-inventory.json` covers the earlier
`/var/lib/goby-fixed-pool-fixtures` prefix as well as the task/native prefixes;
it independently records zero matching mounts, loops or cgroups without
changing resources.

The entire remote task root remains at
`/opt/goby-performance-roadmap-20261002-01`: source, database data, fixtures,
cache, logs, frozen snapshots and failed evidence were retained. No resource
directory was deleted. Do not rerun initialization, old resource setup or retired
kernel cleanup scripts simply to resume.

The exported closure receipt is
`.artifacts/performance-closeout-20261002-session/session-closeout-20261002/closure.json`,
SHA256 `54ef75236b91e9ba6e0647ae7d1f3c085e14ce118bf9602ab826ee0078ad8b6b`.
The local `closeout-manifest.json` in that same closeout root records the
workspace snapshot, Git status,
binary diff, evidence archive identities and recovered last-turn receipts.

No matching recurring automation was found. A read-only check of local app goal
storage found no goal/continuation-deferral record for the original chat. Its
historical `GoalACTIVE` text is not a current app-state receipt and was not used
to mark the unfinished program complete.

## Resume in this order

1. Read this handoff and
   `.artifacts/performance-closeout-20261002-session/closeout-manifest.json`.
   Reobserve protected and owned
   identities before starting anything. Restart only the retained private
   `root/pg` cluster on socket `root/socket`, port25464, no TCP listener, using
   the original startup parameters in `prepare-remote.sh`; do not rerun initdb
   or recreate the database. Source the retained `root/env.sh` remotely.
2. Repair the two P5 fixture preconditions without weakening actual FD/root/
   callback/child joins or task authority. Use `os.MkdirTemp` with a short prefix
   under the owned remote temporary root for the socket and retain cleanup.
   Remove the unnecessary false-marker segment's in-memory/SQL ForceProbe
   overrides so the immutable `library.scan` parent and job remain consistent.
   Freeze a new test-only delta and keep all failed runs.
   Rerun the six focused tests, the five publication tests and original/download,
   scan/theme/extra, binding/task/publication neighbors before mixed acceptance.
3. Overlay only the P3 diagnostic freeze on a recorded candidate. Reproduce
   DelayedInput within a bounded run and inspect actual errno/revents/identity.
   Then rerun complete recovery and default transport regressions. Keep feature
   switches false until the complete required matrix passes.
4. Independently integrate and freeze P4 PART2; run dual-stream and related
   regressions, then reviewed actual RLIMIT/kernel cases under unchanged whole
   physical/inode limits. Do not promote interfaces or skipped tests to readiness.
5. Test the prepared P6 hint and larger-page behavior, then normal, complete
   cold/warm/force scans together with actual concurrent GET and Stop workloads.
   Preserve adverse pairs and report latency, throughput and resource tradeoffs.
6. Complete existing Emby-client A/V and whole-service resource qualification,
   perform source integration separately, and only then consider enablement or
   delivery within newly authorized scope.

The user-requested recovery, resource closeout and handoff are complete. The
performance program remains an explicit continuation backlog.
