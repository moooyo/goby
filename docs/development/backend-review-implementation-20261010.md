# Backend review implementation, 2026-10-10

## Scope

This implements the eight actionable recommendations from the
[backend design review](backend-design-review-20261010.md), plus the previously
reported Q09 snapshot-retirement issue that was still present in this branch.
The user authorized implementation, verification on `test-env`, integration
into `main`, and push. Work starts at `cf00e7c3` in an isolated checkout on
`codex/backend-review-fixes-20261010`. The original checkout's unrelated
uncommitted changes are excluded and retained.

T09 remains deferred. Request-bounded vault reads would change per-secret
filesystem observation semantics, so this delivery does not introduce that
contract change or cache master keys across requests. The existing Linux-only
server support boundary remains unchanged; T08 fixes inconsistent acceptance
of a Linux root name, not a Windows implementation.

## Changes

| Finding | Implemented behavior | Retained boundary |
| --- | --- | --- |
| T01 | Playback source SQL binds user IDs and policy values, reusing each semantic parameter across item and owner predicates. | Other catalog SQL shapes, ACL branches, typed empty arrays, SHARE-wait observation order and both cache opt-outs. |
| T02 | Storage snapshots encode once; root rebinding hashes those same canonical bytes. | Canonical output limits, larger raw-document input limit, private copies and live topology/publication checks. |
| T03 | HLS validates a bounded external-subtitle group with shared primary observations and descriptors. | Per-track content/hash/codec checks, post-admission and final authority, owned state, primary publication, four workers and actual resource retirement. |
| T04 | Items, Latest and ViewingStatistics inherit the production pool's JIT policy without another setting command. | Startup, restored and playback-control pool configuration, read snapshots and cancellation. |
| T05 | Ordinary key-management actors use existing administrator policy authorization after waits. | Native/Emby audiences, exact error mapping, application parent/key/client binding and precise self-revocation. |
| T06 | Notification consumers reuse references returned by the existing strict decoder. | Exact fields, null-array rejection, existing optional-null semantics, tuple uniqueness, ownership and delivery-time authority. |
| T07 | Existing subtitle projection positions retain their own lazy embedded-stream maximum. | Repeated IDs with different projections, collision filtering, private bitmap facts and no extra stream scan when there are no subtitle rows. |
| T08 | Media open and file-deletion root fields accept Linux backslash names already accepted by registration and restore. | Media-item and deletion-payload restrictions, anchored opening, exact root mapping, traversal and historical archive rules. |
| Q09 | Snapshot close failure and reader retirement publish under the same store lock; Store.Close includes degradation in its result. | Read failures publish before retirement, real descriptor closure, cancellation, repeated Close and dependency lifetime. |

## Authority and resource decisions

T05 delegates only ordinary `admin` and `emby` principals to
`authorizeDeviceActor`. Its forbidden result maps back to the key-management
API's existing unauthorized result. The application branch retains its own
binding and self-revocation checks; it never inherits its creator's policy.
Tests exercise policy tightening during management/account waits and schedule
expiry during audit waits for create, list/reveal, get/reveal and revoke.

T03 keeps the existing two-snapshot path for one track. A multi-track group uses
three snapshots: routing, fresh selection after I/O admission, and a final
authority/track observation after storage work. That final observation was
added during implementation review because the primary-file publication fence
alone cannot detect a disabled user, revoked key or retired owned subtitle.
The final primary descriptor/name/publication check follows the last database
wait. The eight-track successful path uses three primary snapshots and two
primary opens, compared with sixteen of each in the individual-read path.

Owned payloads are consumed and discarded one at a time. The additional final
snapshot means a multi-track owned payload is read three times rather than two;
the performance profile includes zero, four and eight owned tracks. Neither
this group API nor its tests promise that every combination has equal speedup.
Cancellation may return to the caller before blocking storage retires; the
worker and I/O accounting remain charged until actual cleanup.

T07 initially used a second map. Remote measurement showed a material relative
regression on tiny stream lists, so that version was replaced. The final code
stores the lazy maximum in the already-required position entry. The `-2`
sentinel is distinct from the valid empty-stream maximum `-1`. Each entry gains
one integer; there is no additional cache map. Allocation counts were unchanged
in the measured 64-item cases. No-subtitle
pages avoid stream scans but still pay for the larger existing entries.

Q09 also closes the related read-failure publication window: a read publishes
its integrity failure before releasing the snapshot mutex. Store.Close never
waits for a snapshot while retaining the store mutex. A close failure and
removal from the reader map become one observation, and the final store result
includes degradation even when the reader has already left that map.

## Verification

All executable verification and formatting run on Linux `test-env`, using
`/opt/goby-toolchains/go1.27.1/bin/go`, PostgreSQL 17.11 and the pinned FFmpeg
9.0.1 tools where selected cases require them. No local test, build, formatting
check or runtime probe is performed. Ordinary compilation uses `CGO_ENABLED=0`;
race compilation uses `CGO_ENABLED=1`. Native execution binds both `GOTMPDIR`
and `TMPDIR` to the task's ext4 fixture directory. Compiler scratch is separate.

The selected verification completed with the following deduplicated top-level
test counts. Race cases overlap ordinary cases; they are not additional unique
behaviors.

| Package | Ordinary passed | Race passed |
| --- | ---: | ---: |
| backupstore | 50 | 50 |
| database | 4 | 0 |
| identity | 242 | 24 |
| library | 173 | 16 |
| notificationjournal | 6 | 0 |
| notifications | 18 | 18 |
| recovery | 2 | 0 |
| server | 60 | 0 |
| storagebinding | 11 | 0 |
| Total | 566 | 108 |

The library's separately opted-in `TestRootBindingFullScanMountNamespaceHelper`
was skipped when the broad root-binding pattern encountered it. Its external
private-mount/full-scan scenario was not selected or claimed. All selected
product regressions passed, with no unresolved failed or incomplete cases.
This is not a full repository, GPU, NAS or Docker-release acceptance.

Both Linux executable builds (`cmd/goby` and `cmd/goby-command-launcher`)
passed, and final remote gofmt reported no changes. An initial Q09 read-failure
test stalled because `synctest.Wait` cannot establish durable blocking of a
mutex waiter. The exact test worker was stopped with SIGQUIT to preserve its
stack. The test was repaired with a retirement-lock assertion and explicit
channels; the complete backupstore package subsequently passed in ordinary
and race modes. That raw attempt remains retained.

Source receipts distinguish r1, r2 and r3. Unchanged packages retain their
earlier passing results; library changes and failure-cleanup repairs were
verified on r3. Final verification confirmed that all 2,827 files in the
selected `cmd`, `internal`, `go.mod` and `go.sum` source scope still matched the
r3 manifest. The retained source manifest SHA-256 is
`f36fe80ac22c2d241026a8599150e829420c9ba2a5ff313580286670748b758f`.

| Artifact | SHA-256 |
| --- | --- |
| `source-final-tested.tar.gz` | `8b8378a0bd4271ba7dc74fc2d1db24e2b8973e4c9c64f8c21097ee077ac22621` |
| Linux Goby binary, 73,016,030 bytes | `409f01227b73e3fe161be337b755c31ce330a38ce0743afdb3c7ed67425c9ee3` |
| Linux command launcher, 4,479,954 bytes | `2043266e1e82be742b4775c1a096db131821e5ff8a876570b27dd315d026f543` |

## Implementation review loop

Four independent review tracks repeatedly inspect the combined candidate.
New implementation or test-lifetime problems reset the empty-round count.

| Round | Outcome | Consecutive empty rounds |
| --- | --- | ---: |
| 1 | Restored missing-track error precedence; added the final group authority/owned-state observation; completed the source-SQL test migration and nil-array equivalence checks. | 0 |
| 2 | Rechecked actor/target, parameter, source, canonicalization and actual retirement paths. No new defect. | 1 |
| 3 | Remote testing exposed the synctest mutex-wait problem and the map-based T07 regression. Repaired test coordination, reused existing position entries, and joined actual subtitle workers in failure cleanup. | 0 |
| 4 | All tracks cross-reviewed r3, including sentinel/duplicate positions, final authority, unknown cleanup and Linux path consumers. No new defect. | 1 |
| 5 | Reverse-reviewed response/commit consumers and persistence/retirement boundaries, and checked measured-performance limitations. No new defect. | 2 |
| 6 | All tracks completed a final combined review against the selected verification evidence. No new defect. | 3 |

Rounds 4-6 satisfy the three-consecutive-empty-round condition. Review
convergence is limited to the inspected scope, not a proof of defect absence.

## Bounded performance observations

The SQL comparison uses eight equal-shape policy scopes. Both forced custom and
forced generic modes retain eight literal source statements versus one bound
statement, while returning equivalent source snapshots. Real owner, sharing,
denial and post-SHARE-wait classification tests separately exercise the affected
authorization semantics. The EXPLAIN output is explicitly a one-shot statement
observation; it is not a timing benchmark of a reused prepared plan.

The final T07 benchmark includes construction of the existing position map and
its backing slices in both variants. Three runs of each 64-item case produced
these median component costs:

| Streams/item | Subtitle tracks/item | Repeated scan | Final cached position | Allocations in either arm |
| ---: | ---: | ---: | ---: | ---: |
| 2 | 1 | 2,454 ns | 2,841 ns | 67 |
| 32 | 8 | 17,447 ns | 7,588 ns | 67 |
| 128 | 8 | 48,678 ns | 11,289 ns | 67 |
| 128 | 32 | 183,765 ns | 23,188 ns | 67 |

The cached entries add 512 bytes per 64 distinct positions (5,928 to 6,440 bytes
for this benchmark). The small one-track case has a measured fixed cost; larger
stream/track groups benefit. These are in-process component observations,
not HTTP latency claims. The discarded map version and its measurements are
retained in the earlier source/evidence revision.

The subtitle-group profile runs 30 operations per user after three warmups, on
one and four users with one and eight tracks. The paired arms run in a fixed
individual-then-batch order, once per selected combination. These are warm local
fixture observations, including the observer overhead, not an HTTP or NAS test.
The eight-track observations were:

| Owned tracks | Users | Individual p95 | Batch p95 | Individual p99 | Batch p99 |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 0 | 1 | 33.75 ms | 12.00 ms | 39.26 ms | 12.70 ms |
| 0 | 4 | 52.19 ms | 15.41 ms | 55.22 ms | 17.70 ms |
| 4 | 1 | 37.81 ms | 11.73 ms | 45.34 ms | 11.77 ms |
| 4 | 4 | 52.27 ms | 19.42 ms | 53.29 ms | 21.24 ms |
| 8 | 1 | 38.22 ms | 15.06 ms | 38.41 ms | 15.30 ms |
| 8 | 4 | 55.60 ms | 20.50 ms | 59.44 ms | 22.78 ms |

Snapshot counts include warmups and independently establish sixteen versus
three per eight-track operation. Filesystem-event tests establish sixteen
versus two primary opens. Whole-owned groups still perform 24 owned-payload
queries versus 16; the measured source-work reduction outweighed that cost for
these small fixtures. Allocation bytes per eight-track group fall from about
4.42 MB to 0.91-0.92 MB with one user, and 5.85 MB to 1.18-1.19 MB with four
users. Heap peaks are sampled process observations, not proven memory limits.
No claim is made about unmeasured large owned payloads or remote storage.

## Capacity and retained evidence

The capacity policy was read before preparing the environment. Initial root
availability was about 658 MiB. One selected idle maintenance window verified
the canonical root-owned shared build-cache layout and current liveness, then
used the pinned Go executable with exact `GOCACHE=/root/.cache/go-build` to run
`go clean -cache`. Allocated cache bytes fell from 1,493,168,128 to 12,288, and
observed root availability rose from 689,659,904 to 2,182,815,744 bytes. Modules,
source, databases, old evidence and unrelated services were retained.

Ordinary runs reuse that shared cache and `/root/go/pkg/mod`. The task uses
`/tmp/goby-backend-review-implementation-20261010` for source, compiler scratch,
binaries and logs, and `/opt/goby-backend-review-implementation-20261010/fixtures`
for ext4 fixtures. Three task-owned databases use independent non-superuser
roles on the existing PostgreSQL cluster. Secret environment files stay 0600
on the remote host and are excluded from evidence exports.

After the identity run, observed catalog occupancy approached the original
220 MiB database/fixture allowance. Before further admission, the budget was
revised to 384 MiB for databases/fixtures and reduced to 896 MiB of shared-cache
growth. The task tmpfs budget remains 1.5 GiB, with 512 MiB persistent/tmpfs
availability floors and 2 GiB available memory. There was no second cache
cleanup.

The library regression tests all passed, but their post-run capacity guard
rejected 409,049,809 database bytes plus 24,576 fixture bytes against the
384 MiB allowance. New work paused. Read-only inspection found no application
schemas, business relations or application connections. The growth was system
catalog/index allocation after repeated create/migrate/drop test fixtures;
the catalog statistics recorded over two million inserted and deleted
`pg_depend` entries. After PostgreSQL's own maintenance, the databases occupied
about 162 MB and the unchanged guard passed. No task command deleted or vacuumed
data. The product-test PASS and the failed post-run guard are retained as
separate facts, along with the fresh admission result.

Race verification explicitly selects an empty task-owned tmpfs cache at
`/tmp/goby-backend-review-implementation-20261010/race-build-cache`, with a
768 MiB private-cache limit. No cache contents are copied. Ordinary runs keep
the shared cache. The revised task tmpfs limit is 1.875 GiB; the persistent,
free-tmpfs and available-memory floors remain unchanged. This avoids charging
race compiler objects to the constrained persistent filesystem. The selected
cache and effective compile/runtime variables are recorded for every phase.

Local records and copied evidence are retained under
`.artifacts/backend-review-implementation-20261010/` in the original checkout.
Selected source archives, manifests, raw test logs, failed attempts, component
measurements and final binaries are retained separately from disposable scratch.

After all actual workers exited, fresh liveness and exact-path/layout checks
preceded pinned `go clean -cache` for the selected private race cache. Its
allocated bytes fell from 395,841,536 to 8,192; compiler scratch was zero.
Shared build cache (657,793,024 bytes) and modules (920,563,712 bytes) were
unchanged by closeout. The nonempty 24,576-byte failed fixture was retained.
Observed tmpfs availability increased from 1,575,714,816 to 1,971,548,160 bytes;
persistent availability stayed at 1,211,568,128 bytes for these observations.
Availability may also reflect unrelated activity; the exact reclaimed cache
allocation is recorded independently.

## Git delivery

Only this isolated candidate and its review/implementation records are selected
for delivery. The committed code archive is compared remotely with the tested
source manifest before integration. Main advances by fast-forward and is
pushed without force. The original checkout's unrelated uncommitted byte states
are checked for preservation.

The final commit, upstream equality and working-tree preservation receipt are
recorded outside Git in
`.artifacts/backend-review-implementation-20261010/git-delivery.json`, avoiding a
self-referential commit hash. The committed-source correspondence receipt is
retained with the other remote evidence.
