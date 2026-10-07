# Narrow scan query-plan reuse, 2026-10-07

The seven-file change is accepted for the measured warm-scan improvement. Both
target query families and persisted job duration improve in all four fixed C/D
contrasts. The corrected focused run passes 18 top-level tests and 11 subtests
with no skips, and the first performance product qualifies all thirteen phases.
Observed-terminal time increases slightly in every pair, and retained PG plan
bytes are unavailable; neither terminal nor memory acceleration is claimed.

Accepted source commit `e2f262d846e20bc058e95f71abe6f2bf1699c691` is a direct child
of main `08ed21267ab647a4a49bf818ee2d2a2d19d26e39`, with exactly the seven bound
source/test files. Actual closeout is complete. The publication commit identity
is recorded separately; no unobserved final main identity is inferred here.

## Implementation boundary

The seven-file candidate is based on main
`08ed21267ab647a4a49bf818ee2d2a2d19d26e39`, runtime
`6386c8fd88585557ee65acbc0a12be23d90a41d4`, in
`codex/scan-query-plan-reuse`. It excludes the withheld embedded-source candidate
and acquired-row diagnostic. Four production files and three test files are bound
by `source-selection.json`; their hashes remain unchanged across all attempts.

Only media facts (`6bf1992ebd49`) and bitmap existence (`2b0fe30f9764`) opt into
per-query CacheStatement when the connection configuration supports it. The
constructor stores the selected modes once. Zero retains the existing connection
default, including disabled-cache configurations. The owner uses its actual
physical configuration; the data-pool opt-in also falls back when BeforeConnect
or ConnectTracer hooks can alter future physical configuration.

The global CacheDescribe/DescribeExec policy, SQL text, parameter typing, source
and freshness observation points, authorization, cancellation and real writers
remain unchanged. There is no new result cache, lock, Acquire path, retry or
per-query configuration copy. Production and verification static reviews passed;
no local product build or test was used.

## Selected comparison and evidence boundary

The performance driver retains five original phases in candidate mode C, followed
by D,C,C,D,C,D,D,C in one Store/schema/fixture. Mode switches follow complete
worker retirement. D uses the prior default query mode through the same candidate
call sites and argument handling; it is not an independently built old-source
binary. Shared setup costs are not subtracted. C here names this plan-reuse mode,
not the older withheld embedded-source candidate.

The warm controls preserve actual lookup/target path and item-digest sequences,
full SQL multiset, 160 media-facts reads, 128 bitmap reads, 160 lookup records,
671 image rows, no probes/extractions or tuple changes, and Store.Close. The
original five phases retain their own cold/force probes and image-write
expectations. Records are collected during the windows; digesting and
serialization take place after measurement/Close. No production traversal sort,
perf/PMU, forced warmup, cache flush or statistics intervention was added.
All fixed contrasts, first uses, Prepare events and physical PID routes remain.

The frozen analysis retains all eight observations in four fixed C-minus-D
contrasts. Times are milliseconds; target duration sums the two client query
callback families, not exclusive server planning or CPU.

| Pair | C job | D job | C-D job | C-D target callbacks | C-D observed terminal |
| --- | ---: | ---: | ---: | ---: | ---: |
| C2-D1 | 390.186 | 453.247 | -63.061 | -33.454964 | +0.262213 |
| C3-D4 | 405.408 | 426.163 | -20.755 | -27.735643 | +0.685844 |
| C5-D6 | 399.843 | 424.726 | -24.883 | -26.954334 | +2.452615 |
| C8-D7 | 394.972 | 413.772 | -18.800 | -27.688569 | +0.592315 |

Job changes are -13.9132%, -4.8702%, -5.8586% and -4.5436%. Each target family
improves in every fixed pair. Unchanged lookup sums have two negative and two
positive contrasts; allocation and GC changes are also mixed. All adverse
terminal observations remain retained.

The existing long-test wait loop polls every 250 ms. Jobs of 390-453 ms finish
before the second tick, while terminal observations are approximately 504-507 ms.
These are different measurement boundaries: the terminal result describes that
250 ms polling window and does not establish UI or terminal acceleration. No CPU,
cold/force or retained-memory benefit is inferred.

Every control has 1043 application callbacks, 56 BEGIN and 56 COMMIT, no rollback,
160 lookup/media and 128 bitmap reads. Full SQL multisets and actual path-order
hashes agree; each window has 160 unique paths, 128 mp4 and 32 flac. All 288 target
records use their phase's selected mode and remain inside the job. Recorder
capacity stays bounded with no dropped/error record, and Close completes.

Original C cold contains three named Prepares. C controls have none; D1 has two
unnamed description Prepares and D4 one, all retained. D6/D7 have no Prepare and
the corresponding target contrasts still improve. Prepare is nested in query
duration and is not added. First observed mode use is not automatically a cache
miss. The original phases populate caches, and D retains existing C named plans.

These are correlated warm post-removal observations in one process, not
independent N=8, an old-binary comparison or a memory comparison. Only the three
read families and Prepare callbacks are timed; full SQL union and CPU are absent.
Do not assign the entire job change to server planning or use this result to
explain the historical embedded-source regression. All adverse resource and
terminal observations remain in the frozen report.

## Correctness and memory observations

The corrected focused run passes all 18 selected top-level tests and 11 subtests,
with no skips. Six new capacity/hook cases cover named-plan reuse or typed
fallback, parameter types, eviction, private invalidation, pooled/owner session
replacement, nullable facts and fresh presence data. Existing source/root approval,
late replacement, cancellation, no-write and publication contracts also pass.

The focused raw output reports cached-plan memory unavailable twice in every
configuration, twelve observations in total. This is an explicit observation gap,
not zero bytes or a skipped functional assertion. The test's available memory
query would sum CachedPlan contexts for the backend; it would not measure a
single query's exact memory cost or saving. Plan-count/custom/generic assertions
are distinct from that unavailable memory observation. Any additional aggregate
plan evidence from the performance analysis must keep its backend/session scope.

## Actual attempt history and environment correction

| Attempt | Actual result | Interpretation |
| --- | --- | --- |
| Build 1 | RSS guard stop, exit -15; no binary | Owned RSS 836,993,024 exceeded its 805,306,368-byte allowance; not a compile diagnostic or functional test failure |
| Build 2 | Success, exit 0 | Same seven source hashes; one explicit capacity recovery within the unchanged total RAM envelope |
| Native 1 | 18 fixture-setup failures, exit 1 | Inherited GOTMPDIR pointed to reclaimed compiler scratch; no functional assertion or performance product completed |
| Native 2 | Focused PASS, 18 top / 11 sub, no skips | Same retained binary with native GOTMPDIR and TMPDIR both bound to the ext4 fixture root |
| Native 3 | Performance PASS, 13 phases qualified | First execution of the selected performance product; all fixed contrasts retained |

There are two build attempts and three native invocations. Failed build and
native-configuration evidence remain retained; neither is a performance sample.
No third build or replacement performance run occurred. Successful binary SHA-256:
`5d77a7769fef00a09fc9860e1d8ca04afdcc90dcb3b1b53339ddf75ee2d24a3a`.

The build used an explicitly selected initially empty private RAM cache with
shared modules. Build-only GOMAXPROCS=2 and GOGC=50 reduced recovery pressure;
native settings remained unchanged. After compiler exit and independent binary
preservation, the private cache was reclaimed from 264,454,144 to 8,192 bytes
and compiler scratch removed. Shared caches were not cleaned.

Pinned Go 1.27.1 testing.TempDir prefers GOTMPDIR. Setting only native TMPDIR was
therefore insufficient after compiler-scratch removal. The corrected launcher
sets both variables to the selected canonical ext4 fixture root and guards that
binding. The capacity-policy update changes only the compiler-scratch/media-
fixture section to make this phase-specific rule explicit.

## Acceptance and actual closeout

Acceptance is limited to this two-query policy and its verified configuration,
freshness and warm comparison. The seven source hashes match the executed freeze;
no old embedded-source or acquired-row candidate is imported. PG retained bytes
remain unmeasured, so the result is not a memory comparison. It does not establish
cold/force speedup, old-binary total improvement or the cause of the earlier C/B
regression. No further product is selected.

The 68-file corrected native export is independently hash matched. All workers
exited; no external sampler was run. Private compiler cache cleanup occurred
before native execution, from 264,454,144 to 8,192 bytes, with scratch removed;
no second cache clean or shared-cache cleanup occurred. Actual final closeout
removes 49,590,272 allocated bytes of independently preserved temporary binary/raw
copies and the empty ext4 fixture. Task RAM falls from 50,012,160 to 421,888 bytes,
retaining source overlays and the empty private cache root. Independent source,
binary, raw failures and both valid runs remain. PG/Goby/QEMU identities, shared
settings and reserve 403374 are unchanged.

Final persistent availability is 288,096,256 bytes and guest available memory is
6,648,561,664 bytes. These are separate snapshots, not a total cleanup attribution.
Original workspace WIP remains outside publication and must be protected using
fresh identities. The accompanying capacity-policy change is confined to the
compiler-scratch/media-fixture section, including combined go-test execution.

## Evidence

Evidence root: `.artifacts/scan-query-plan-reuse-20261007`. The initial single-build
plan is superseded by the preserved recovery history: two builds, three native
invocations and exactly one performance product.

| Artifact or binding | SHA-256 |
| --- | --- |
| `source-selection.json` | `8d37930ba762fa7713725eaa500f8f413522b07e897df8ba8b0f0cfa0fb223f1` |
| Executed source freeze | `1532ff3d810f1f129fb9948b61472d6aab5c3b76fad4e56b03cf74c38e2acd02` |
| Successful native binary | `5d77a7769fef00a09fc9860e1d8ca04afdcc90dcb3b1b53339ddf75ee2d24a3a` |
| `analysis/performance-report.md` | `66c6afffecf355dd7faefcc1c155eade0441997754d26d66bcab5cba06a7cd9a` |
| `analysis/performance-summary.json` | `c85a84d5c4efd1322ffab28d563e8fea8b7250ac508c6e2a9fd625e403f1f052` |
| Completed 68-file manifest | `4c672acfb9a9641741a874a62b52b8c6016cbf7d791c78ddf7150059abaf82a6` |
| `actual-closure.json` | `13968ab49e938848d99a745f6064191a60818d0a0bcfce4fa322048fd18eacde` |
| `actual-host-closure.json` | `a2b9b8bc7fc56859198f7dfe56a1023d92099c2c8d4b208aea7ad462acb3b011` |
