# Stop recovery and actual media-root read isolation - October 3, 2026

## Selected scope and source

The selected Stop recovery and actual media-root read isolation increment is
qualified. Source commit `8df2dd226e955343eb8376cdeff3e487a6d29d41` is based on
`912354f2a48b5043a8824246f2fa011f8f34bbf1` and is followed by this documentation
commit. The final delivery HEAD and exact main/origin readback are recorded in
the private publication receipt after push; they are distinct from the source
commit.

The 130 changed paths are backend Go source or tests: 91 existing files and 39
new files. No frontend, SQL schema, catalog or file deletion entered this
increment. The earlier metadata display-overlay comparator and idempotent
analysis stderr cleanup repairs remain; see the
[earlier October 3 integration](media-performance-integration-20261003.md).

| Qualified identity | Value |
| --- | --- |
| Complete worktree manifest, 7505 files | `c83d4c2eac37da9c62e35b92a961924b0d700299b92c4f6df249f3104b28ad8a` |
| Go/SQL/catalog manifest, 2447 files | `b11fafe4bbd9820dfa3419f0f16d81a76a3d0be740fddab57acc2e51dbc37d19` |
| Final frozen source archive | `8a6f0531ce3ca8645596bc62db3f49350c86cebda034b957566e20cadb5ad987` |
| Final composed evidence archive | `b57af031d23062e9018099a969c086ed6ec664b7188744193d3cc39022bcb548` |
| Full library component evidence | `4f3c84fabd02ce76431a3e46c6ac42a7bdc4f6fb560723944ca6d1cba4a13033` |

Before adding this record, all 7505 archive files matched the managed worktree
byte-for-byte. All 2447 committed Go/SQL/catalog files were independently read
from the source commit and matched the qualified code manifest; every intended
Go change also matched its staged blob. Existing checkout line-ending rules
remain unchanged. Private source, test and database artifacts are not committed.
Missing historical app archive snapshots are not recovery sources; use the
verified archive and published Git source.

## Bounded Stop recovery

An ordinary validated Stop remains available when the four-slot early lane is
full. A successful durable report fences the current exact lifetime, including
late inputs and registrations, without minting a new lifetime or reservation.
Failed reports retain restrictive intent. Recovery uses a fresh exact terminal
witness and rechecks actual owner references before collection. The existing
maintenance cycle reserves bounded terminal-witness windows before active
session checks can spend the cycle. Data12, Control4 and Stop reservation bounds
remain unchanged.

Production correlated ownership and early Stop are default-enabled only for the
existing file-HLS path. Progressive and dynamic admissions retain their no-op
contract. Generated-window and native experimental features remain disabled;
the dedicated raw A/V diagnostic keeps its earlier baseline.

Retirement still requires the same pinned pidfd exit, actual `stat ENOENT`, and
original process-group `ESRCH`. A test-only retry uses the existing five-second
deadline and repeats a fresh complete observation only for exact pidfd
exit-ready plus `stat ESRCH` while retirement remains unknown. It never accepts
ESRCH as reap proof. Other unknown observations, PID reuse and deadline
exhaustion fail. First/last numeric facts and retry counts remain evidence.

## Actual media-root read isolation

The common opaque root I/O capability prepares trusted routing from committed
catalog authority before filesystem work. It supplies admission, not playback,
task or publication authorization. Consumers include scans and ordinary cold
final publication, directory/root proofs, sidecars, artwork, provider local
filesystem work, HLS/transcode, embedded media reads, Analysis digest/child
reads and source-open metadata work.

An owned database transaction tries admission immediately. Pure Busy rolls back,
waits outside the transaction and repeats fresh authority and source proofs.
Mixed authority, SQL, Close or unknown-retirement errors cannot be reclassified
as retryable capacity. Ordinary source changes retain business-rejection
semantics only when their exact private rejection pointer is the sole error
leaf. Publication terminal and deleted-terminal cancellation preserve the
existing oracle.

Complete 256-source populations use serial source phases and bounded immutable
facts. Body owners and canonical claims both remain 64. The four-owner metadata
runtime shares the actual governor and borrows reference-counted exact existing
domain claims. Each active atomic claim remains limited to eight roots and
sixteen domains. Existing global/root/domain and queue capacities are retained.

Actual workers, children, copiers, inherited descriptors and resource closes
remain charged until independently proved complete. Caller cancellation alone
does not release them. Unknown retirement and the first actual Close failure
retain ownership. Successful Close of the same `*os.File` is idempotent; a later
`os.ErrClosed` must not create a permanent Store pin. SQL ownership loss is
separate from actual filesystem retirement failure.

Normal metadata owners observe Store shutdown. The cleanup-only exception uses
fixed `warm.releaseChecked` on an already-held anchor and a prepared immutable
route after Store cancellation. It performs no SQL, Stat, new source open or
payload read. Busy or unknown failure retains exact resources and reservation
references.

## Final verification and exact-input reuse

All execution verification ran on `ssh test-env`. Local work was editing,
static review, Git operations, packaging and byte/evidence processing.

| Component | PASS | FAIL | SKIP | Evidence scope |
| --- | ---: | ---: | ---: | --- |
| Full library race package | 1275 | 0 | 11 | final-library-component1, 1459.487 seconds |
| Full media race package | 641 | 0 | 8 | final-combined3, exact-input reuse |
| Full transcode race package | 709 | 0 | 19 | final-combined3, exact-input reuse |
| Final affected and pending server scope | 302 | 0 | 3 | final-combined5, 543.268 seconds |
| ForeignBodies, count 5 | 5 | 0 | 0 | final-combined5 |
| DelayedInput, count 5 | 5 | 0 | 0 | final-combined5 |
| Original Stop and default scope | 16 | 0 | 0 | final-combined5 |
| Actual consumed HLS input Close and Store join | 1 | 0 | 0 | final-combined5 |
| Actual disabled-HLS/progressive Noop | 1 | 0 | 0 | final-combined5 |
| Actual production-constructor HLS and encoder | 1 | 0 | 0 | final-combined5, primary_io_measure |
| Actual Analysis executor and decoder | 1 | 0 | 0 | final-combined5, primary_io_measure |

The library suite ran on the final production freeze before the last two
server-test-only observation changes. Its 1535 actual race dependency input
files, including test data and module files, were re-enumerated after staging
and matched exactly. Dependency manifest:
`6e26626ad8fc6e3bada0df13c38e4624f50b5446063533a385de73f9e07e3489`.
Media/transcode dependency inventories contain no library, server or primaryio
package. Their 837 input files remained exact through the final stages:
`b427de739ce07d11102b3d0a825f13a391a91ecd377530de3f3d720c9a8bb110`.
These are per-package input proofs, not a claim that every run used one globally
identical tree.

The server plan reran 305 affected or previously pending tests. It retains 154
explicitly unaffected prior passing authorization/DTO/cache/URL cases, and
records overlaps with separate focused checks. It includes directory adapters,
generated cached-map current authority and actual four-codec progressive
encoding. Its three skips are the recorder helper and existing opt-in actual
restart and IPv6 network fixtures. It is not one complete server-package run.
The unchanged 83-test focused component covers metadata4, common factories,
Original/download64, cancellation, warm anchors and mixed-root256. Actual BG2
publication success, queued source change and unknown joined-Busy cases passed.
Repeated and overlapping counts must not be summed as unique coverage.

All four `CGO_ENABLED=0`, amd64 Goby/launcher builds passed for Linux and Windows.
Source hash checks, full re-enumeration and protected-resource checks exited
zero; the final composed runner exited zero. Windows cross-builds do not
qualify Windows runtime, native, GPU or full client A/V behavior. Existing
optional package skips remain absent-profile gaps.

The complete 10000-file cold/reopen/cached/replacement/ACL scan fixture passed
again, including 10001 actual probes, four completed jobs and Store closure.
This is functional compatibility. Cached checkpoint counts remain 1/2/3/3/2;
new authority transaction counts are explicitly 8/6/8/8/4 with raw totals
retained. These additional locking transactions are a cost, not warm-scan
performance or whole-service capacity acceptance.

## Retained failures

Later passing evidence does not erase these original results.

| Retained record | Original outcome and limit |
| --- | --- |
| Historical generic retirement observation | `known=false` remains unexplained; no errno or cause is inferred from later diagnostics. |
| Initial Stop matrix | One runner setup omitted its private artifact environment variable. The same-source correction passed; the setup failure remains. |
| Early scan/Analysis/transcode checks | Post-commit scan cancellation, four nil-context guards and the initial uninstrumented Analysis witness failed. Strict repairs passed later; the paced Analysis diagnostic does not prove the old cause. |
| final-combined1 | Library compilation failed for a missing `errors` import; overall exit 1. Evidence `fdd87241cc54da644ebc7b076480c7af8678450f1cb2967d8bb24c9d7e6a7f94`. |
| final-combined2 | Focused 78 PASS/1 FAIL at owned source projection; overall failed. Evidence `f28fb47b7c0e9973ad1ff42b8125754720e85574f2c272d173c0c0cb6d5610b4`. |
| final-combined3 | Library 1251 PASS/15 FAIL/11 SKIP; server 350 PASS/1 SKIP then a natural 30-minute cleanup timeout, leaving Noop and later checks incomplete. Builds passed but overall exit 1. Evidence `c5b666b863590f4624d7621409393af014f7b0fa929652c246be4b827091ce47`. |
| repair-focus1 | Original 15 failed selectors and 10k compatibility passed; terminal/deleted-terminal cancellation still failed, overall exit 1. Evidence `eb07cb1a2259e40dcaa7511e296df8013357ddca8142ad5e091fb69861740183`. |
| final-combined4 | StopForeignBodies saw pidfd-ready/stat-ESRCH unknown without a group fact; overall exit 1 and full/build phases did not execute. Evidence `f0fcdf9f3854e8217a4ec4bd5e933221ef9a8dffc96117ffbbe2740a5d000912`. |

The source fixes preserve actual-close/unknown failures, separate SQL ownership
loss, retry only pure Busy outside locks, and use real task/backend/window
fixtures. The final retirement retry changes tests only and retains the strict
acceptance and original deadline.

## Publication and resource disposition

Original workspace WIP was copied before publication: 208 dirty path states (206 files and two intentional absences), its index and
binary patch, including all 56 previously distinct WIP identities. Main advances conditionally with an index lock and compare-and-swap
reference update. Initial dirty paths are never refreshed; only exact old
committed bytes or safe absent paths may receive the new source. The private
publication receipt records post-update byte preservation, source/documentation
commits, the delivery HEAD and exact origin/tracking readback. No force push is
used.

Private PostgreSQL PID2831755/start53792627 received only pidfd-bound SIGTERM
for smart shutdown after zero other client backends and fresh exact identity
checks. Its pidfd exited; all six recorded family processes disappeared; the
private PID file, socket and lock were absent. Protected shared PostgreSQL and
three recorded Docker identities remained unchanged. Visible recorded audits
found zero blocking task process references, task mounts, task loop devices or
task-reference cgroup memberships. This is the recorded visible scope, not
absence of every global kernel resource. Source, data, fixtures, cache, logs
and failed evidence are retained.

Closure receipt SHA256:
`b6ffff608bff311955966d3d5c3860426d311d56e9b6d3faccb9014a2f1ff38e`.
Closure evidence archive:
`81589e96f782a11f47ed4257c9c6a202132b71df4c27ee3dbc044318b019a7b2`.

## Paused work

Full existing-client A/V, native/hard resource qualification, Analysis PART2,
new reconciliation hints and whole-service mixed capacity remain paused.
Analysis digest and child-source wiring is read-lifetime work only. This
increment does not expand edit/rewrite/deletion mutation publication, qualify a
replacement Docker image or deployment, or complete the full performance program.
