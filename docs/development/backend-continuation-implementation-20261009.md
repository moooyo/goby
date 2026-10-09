# Backend continuation implementation, 2026-10-09

## Scope

This implements S01-S09 from the
[continuation review](backend-design-review-continuation-20261009.md).
The user authorized implementation, verification on test-env, integration into
main and push. Work starts from 2d12bdb5 in the isolated
codex/backend-continuation-fixes worktree. Unrelated changes in the original
workspace are outside this delivery.

## Implementation map

| Finding | Implementation |
| --- | --- |
| S01 | Amortize high-cardinality login-limiter maintenance with a one-second cleanup cadence, preserving live limits and bounded capacity. |
| S02 | Preflight and unlink a batch of closed preview JPEGs, sync the directory once, then finalize accounting and permit name reuse. Failed batches remain owned through Abort. |
| S03 | Retire timeshift artifacts and directories through one bounded worker outside the global mutex, preserving actual allocation charges, synchronous completion and shutdown ownership. |
| S04 | Query ordinary historical device usage in bounded 512-account batches only where visibility depends on it. |
| S05 | Read the Configuration endpoint's owner configuration and PIN in one authorized transaction, retaining the final server projection and separate login/Users/Me behavior. |
| S06 | Discover nearest music albums through 128-parent recursive batches with independent paths and the existing per-album publication transactions. |
| S07 | Preserve local-password failure/block state on PIN-only updates; explicit local-password or enablement changes keep reset/revoke/audit behavior. |
| S08 | Return aggregate counts from the existing bounded, ordered item/entity locking queries, preserving real FK locks and the combined 100,000-row budget. |
| S09 | Add schema 64 with three item-reference indexes and a PostgreSQL-17-generated recovery catalog, preserving historical rows, deletion actions and publication barriers. |

No cross-request authorization cache, relaxed resource identity, early quota
release or unsupported platform implementation is introduced. Windows server
support was already absent from the starting source.

## Implementation review

| Round | Result | Consecutive clean rounds |
| --- | --- | --- |
| 1 | Fixed timeshift reader completion waiting for unrelated same-window deletion: completion now belongs to the artifact, with final-directory completion handled separately. Other reviewed areas had no new issue. | 0 |
| 2 | Corrected timeshift publication waiting on unrelated cleanup despite irreclaimable local quota. Pending cleanup forecasts now distinguish local/global shortages; request/window cancellation releases an admission that owns no new retirement. Added deterministic quota/cancellation coverage. | 0 |
| 3 | Rechecked cleanup forecasts, actual charges, publisher ownership, private/public data handoffs, locking counts and schema publication. No new issue. | 1 |
| 4 | Challenged partial deletion/destroy failures, concurrent observers, no-op credential changes, rollback, maximum/empty inputs and test assertions. No new issue. | 2 |
| 5 | Final independent source/caller/lifetime/query and platform correspondence review. No new issue. | 3 - stop |

The three-consecutive-clean-round condition is satisfied by rounds 3-5.
No accepted implementation finding remains open.

## Verification results

All executable checks run on test-env with pinned Go 1.27.1, PostgreSQL 17.11
and shared build/module caches. No local build, test suite or runtime probe is
used. The final production build of all internal/cmd packages passed after the
timeshift repairs. All affected package test binaries compiled. The selected
ordinary runs passed 689 distinct top-level tests, including all 41 new tests;
selected race runs passed 127 top-level tests, a subset of the ordinary set.
Subtests, repeated attempts and skipped cases are excluded from those counts.

| Package/scope | Ordinary passes | Race passes |
| --- | ---: | ---: |
| analysiscache, complete default suite | 34 | 34 |
| database, complete default suite plus enabled index observation | 84 | Not selected |
| backuppg, complete suite with independent disposable databases | 187 | Not selected |
| identity, complete suite | 235 | Not selected |
| timeshift, complete suite after both repairs | 41 | 41 |
| library, affected music discovery/publication/reconciliation paths | 21 | Not selected |
| server, affected authentication, previews and dynamic playback paths | 87 | 52 |

The existing opt-in real-ENOSPC namespace scenario was skipped in both
analysiscache runs. The database observation's default skip was subsequently
executed with its opt-in flag. No new test was skipped. This is affected-scope
verification, not execution of the entire backend or a Docker delivery test.

The retained earlier failures are separate from final passes:

- The first catalog generator invocation failed before database work because
  its private environment was not ready. The corrected invocation exported
  schema 64 from an actually migrated, newly owned PostgreSQL database.
- The first complete backuppg run passed 186 top-level tests and failed one
  existing test requiring an empty default public schema. Catalog generation
  had populated that schema. A separate newly owned restore-fixture source
  database preserved the export database and satisfied the existing contract;
  the complete second run passed all 187 tests.
- The first identity run was stopped by the capacity guard after 135 passing
  top-level tests, with no assertion failure. After actual worker exit and the
  selected shared-cache maintenance described below, the complete second run
  passed all 235 tests. The interrupted run remains interrupted evidence.

The final source comparison checked 2,745 Go/SQL/module/catalog paths and found
zero missing, additional or different files between the delivery worktree and
the tested remote source. Pinned remote gofmt reported no outstanding formatting.
The delivery receipt binds each of the 41 new tests to its successful raw-log
line. Earlier unaffected-package passes retain their original snapshots; final
timeshift, HTTP and production-build phases cover the repaired source.

## Controlled index observations

The remote fixture retains 6,000 items, 24,000 user-state rows, 30,000 playback
sessions and 30,000 media operations. Three 128-parent DELETE trials reported
PostgreSQL execution times of 762.965 / 747.450 / 757.042 ms on schema 63 and
32.690 / 32.193 / 33.253 ms on schema 64. These are controlled SQL observations,
not natural-load scan or HTTP measurements.

The direct child-lookup statements selected their new indexes without planner
forcing. Shared buffer hits changed from 381 to 14 for user state, 914 to 20
for playback, and 1,420 to 88 for media operations; the schema-64 playback
lookup also read two shared blocks. These are equivalent FK action statements,
not captured internal trigger plans. Parent DELETE logs separately retain real
trigger observations.

Indexes have maintenance cost: 512-row insert observations increased shared
buffer work for all three tables. Exact plans, WAL, relation/index sizes and
all samples are retained in observe-indexes-r1.log; the fixture and interpretation
limits are documented in
[item-reference-index-observation-20261009.md](item-reference-index-observation-20261009.md).

## Evidence and capacity

The remote task root is /opt/goby-continuation-implementation-20261009-06.
Source snapshots, raw failed/passing logs, phase exits and source manifests are
retained separately from compiler output. The local task artifacts are under
.artifacts/backend-continuation-implementation-20261009 in the implementation
worktree, with an exported copy retained in the original workspace for closeout.
Private database environment files are excluded from source delivery.

Compilation uses task-owned tmpfs scratch and test executables. Native GOTMPDIR
and TMPDIR both point to the task's ext4 fixture directory. Shared GOCACHE and
GOMODCACHE remain /root/.cache/go-build and /root/go/pkg/mod; no private cache or
cache copy is created. Full source expansion exceeded the initial document
allowance; 1,703 unchanged documentation files were hash-matched against retained
base/overlay archives before removing only their duplicate extracted directory.
Source, documentation and raw evidence remain recoverable from the archives and
the local worktree. No media fixture filesystem was changed.

The first identity run reached the original persistent-space floor and stopped
new admission. Fresh inspection found no active test/compiler/linker worker.
After checking the exact root-owned canonical shared-cache layout, one pinned
go clean -cache pass reclaimed 2,171,465,728 allocated bytes. Modules, databases,
source, binaries and raw evidence were retained. The resulting 3,020,705,792
available persistent bytes supported a revised workload budget preserving at
least 1 GiB free. No repeated under-load cache purge was used.

Final liveness inspection found no owned worker. Ten inactive test executables
were hashed and retired, reclaiming 305,393,664 allocated bytes on tmpfs; empty
compiler/bin scratch directories were removed. No private cache existed.
Source, input archives, failed/passing logs, databases and shared caches remain.
The ext4 fixture directory was empty at closeout and retained. Persistent
availability was 2,252,722,176 bytes; shared compiler cache occupied 832,950,272
bytes and modules occupied 920,563,712 bytes. Removing empty scratch is not
attributed a persistent-disk gain.

## Git delivery

The selected delivery branch is codex/backend-continuation-fixes, based on
2d12bdb5. Integration targets main after the final source correspondence check;
the retained Git delivery receipt records the exact tested commit and pushed
remote identity. Existing unrelated original-workspace edits are excluded.
No deployment or image publication is part of this request.
