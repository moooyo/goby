# Embedded-source removal diagnostics, 2026-10-07

Seven qualified remote Go processes narrow the removal-regression question but
do not establish its historical cause. The first two diagnostic pairs reproduce
a slower candidate; the final CPU pair reverses direction. The candidate remains
withheld from main. No production source was changed in this follow-up, and its
final publication contains only this report and the handoff.

Baseline B is `6386c8fd88585557ee65acbc0a12be23d90a41d4`; candidate C is
`cd22752397ff70b490b27f9b7536dfddb57d737f`. Publication starts from main
`de83ff4b50a90f2573b3fe7a86710ca8e91387b9`, whose runtime remains B. The earlier
[acceptance report](scan-embedded-source-readonly-20261007.md) and its three
adverse C/B removal pairs remain unchanged. New diagnostic timings do not replace,
re-pair or pool those samples.

## Selected work and qualification

| Stage | Fixed selection | Purpose | Qualified Go processes |
| --- | --- | --- | ---: |
| 1 | B then C | SQL callback attribution | 2 |
| 2 | C then B | Prepare and connection-description attribution | 2 |
| 3 | C only | Private-data statistics and plan sensitivity | 1 |
| 4 | B then C | Removal CPU endpoints and backend coverage | 2 |

All seven retained the complete original five-phase workload, correctness,
probe/tuple, retirement and Store.Close guards. Stage 3 then replayed reads after
Store.Close. All selected products qualified, with no extra product or replay.
Stage 4's scheduler metadata remains partial; that limitation is separate from
product qualification. No shared PG, VM, mount, swap, cache or durability setting
was changed. Only stage 3 changed autovacuum options and statistics on its own
new fixture, which was subsequently removed.

Each B phase uses 32 BEGIN/source SELECT/cache SELECT/COMMIT groups. C replaces
them with 32 single SELECTs, removing 96 application callbacks and 32 explicit
read-only transaction-boundary pairs per phase. These are not 32 avoided WAL
flushes. Startup permission grants and actual writer semantics remain unchanged.

## Stage 1: the replacement callbacks save time; other SQL grows

The SQL-only pair has complete query lifecycle captures, with no incomplete or
dropped records. Its removal measurements are:

| Measurement | B, ms | C, ms | C minus B, ms |
| --- | ---: | ---: | ---: |
| Persisted job | 421.522000 | 447.276000 | +25.754000 |
| Application SQL union, clipped to the job | 346.926488 | 371.302717 | +24.376229 |
| Job outside that SQL union | 74.595512 | 75.973283 | +1.377771 |
| Old embedded commands / new embedded SELECT union | 50.596160 | 43.332010 | -7.264150 |
| Other application SQL union | 296.330328 | 327.970707 | +31.640379 |
| COMMIT union, included in SQL union | 27.765743 | 24.033305 | -3.732438 |

The 7.264150 ms saving measures callbacks, not the entire embedded-source path.
Old JSON/helper work occurs between the two SELECT callbacks; new JSON/helper
work follows the single callback. The old BEGIN-to-COMMIT envelope and new
single-query interval therefore cover different work. Uncovered wall time is
not Go CPU.

Four unchanged templates retain equal counts: stored-item lookup, 160 calls,
grows 12.648726 ms; unchanged-image comparison, 136 calls, grows 8.252015 ms;
media facts/subtitles, 160 calls, grows 4.779149 ms; bitmap existence, 128 calls,
grows 1.937579 ms. Their individual unions can overlap and are not additive job
components. This localizes the observed increase without explaining its cause.

Common-read PG samples are active with no reported wait, which does not isolate
CPU, planning, execution, transport or row consumption. Removal has no sampled
Lock/LWLock/data-I/O explanation in this pair, and its broad C device-flush context
is not slower overall. A separate force WalSync tail cannot explain removal.
C-owned items/item_images auto-ANALYZE was sampled in warm; its causal relevance
was an open hypothesis, rather than a demonstrated statistics or plan change.

## Stage 2: no description miss in the four main common reads

Removal is again slower for C: 427.404 versus 411.274 ms, a +16.130 ms difference.
SQL union grows 6.928745 ms and the uncovered remainder grows 9.201255 ms.
All 12 observed connections report `cache describe` and description/statement
capacities of 512/512. C's 125 and B's 121 Prepare records are unnamed and have
valid parents, with no errors, missing/incomplete/dropped records or unknown
parents.

C has no removal application Prepare callback. B has two theme-resource
callbacks totaling 0.292023 ms of job-clipped union, unrelated to the four common
reads. Those four templates have zero removal Prepare callbacks on both sides.
This does not support description-cache misses as their slowdown mechanism.

Prepare is nested inside its parent query, not additional time. Its absence
does not mean no server Parse/planning or reuse of a named generic plan.
`cache describe` retains client descriptions; query-minus-Prepare still includes
transport and scheduling. The measured connection settings apply to stage 2,
not retroactively to stage 1.

Early C cold/warm/force COMMIT tails have associated WalSync samples and elevated
device-flush context. Removal had returned to a different flush regime, so those
early tails do not explain its common-read increase. No private auto-ANALYZE was
sampled in stage 2; short activity could occur between samples. Original explicit
ANALYZE of the reconciliation temporary table is a separate operation.

## Stage 3: visible statistics changes, without a sampled bad-plan switch

A new C fixture disabled only its own tables' autovacuum before load. After the
five original phases and Store.Close, one dedicated backend replayed the same
456 common SELECTs per window in fixed order: N1/N2, ANALYZE items, I1/I2,
ANALYZE item_images, IA1/IA2. Every first query was retained. Six fixed parameter
slots had two EXPLAIN rounds per state, yielding 36 sanitized plan samples.

Statistics visibility changed as intended. N has no relevant column statistics,
I has items statistics and IA also has item_images statistics. Schema/table
identities and all private ctid/xmin rowsets remain unchanged, and replay results,
ordinals, templates and row counts match. All six sampled slots retain the same
compared node-tree shape across states and rounds. Estimated widths change
(lookup 506 to 1646; media 81 to 1305), without a demonstrated poor-plan switch.

| State | First sample sum, ms | Second sample sum, ms | Second minus first, ms |
| --- | ---: | ---: | ---: |
| N | 175.689081 | 170.710833 | -4.978248 |
| I | 165.127657 | 161.633631 | -3.494026 |
| IA | 164.770419 | 158.100193 | -6.670226 |

N2 to I2 is -9.077202 ms and I2 to IA2 is -3.533438 ms, but I2 to IA1 rises
3.136788 ms. Fixed order, first use and same-state drift prevent assigning the
full reduction to ANALYZE. Replay timing includes client query/row-consumption
work, and N1 constructs a baseline whereas later windows compare against it.
The experimental five-job timings are not B/C acceptance samples. These results
do not establish a statistics-induced cause for earlier reversals or justify
tuning shared autovacuum, cache capacity or durability.

## Stage 4: lower measured removal CPU, with the timing direction reversed

The final B-to-C pair restores the natural fixture. Only removal adds resource
endpoints before admission and after the original terminal/resource measurement;
there is no statistics intervention, extra SQL, profile or runtime trace.
All five phase results are retained:

| Phase | B job, ms | C job, ms | C minus B, ms |
| --- | ---: | ---: | ---: |
| cold | 3256.795 | 3291.161 | +34.366 |
| warm | 414.795 | 407.218 | -7.577 |
| force | 2176.638 | 2280.314 | +103.676 |
| image_changed | 419.992 | 407.962 | -12.030 |
| image_removed | 442.302 | 405.168 | -37.134 |

Removal SQL union falls 366.814110 to 334.235320 ms (-32.578790), and its uncovered
remainder falls 75.487890 to 70.932680 ms (-4.555210). The old embedded callbacks
occupy 49.540564 ms versus 38.686927 ms for the new SELECTs, a 10.853637 ms callback
saving with the same scope limitation as stage 1. This pair does not reproduce
the earlier removal regression; cold and force are still slower.

| CPU counter | B, ms | C, ms | C minus B, ms |
| --- | ---: | ---: | ---: |
| Go process user | 117.640000 | 123.923000 | +6.283000 |
| Go process system | 59.429000 | 42.579000 | -16.850000 |
| Go process total | 177.069000 | 166.502000 | -10.567000 |
| Actual removal-query backend runtime sum | 291.562103 | 266.189307 | -25.372796 |

Go rusage brackets approximately 504.26/504.30 ms, wider than the persisted jobs,
and includes all Go threads and diagnostic/observer work. Each backend counter
has its own read bracket and can include untraced Ping and observer work.
QueryTracer does not identify PostgreSQL parallel worker PIDs. These CPU counters
are not exclusive job CPU and cannot be added to SQL wall time or to each other
as a wall-time partition.

All actual phase-query PIDs have stable identities and CPU coverage. Historical
PIDs are preserved but excluded from totals; mixed application/observer PIDs
cannot be split into CPU buckets by query count. Both endpoints report scheduler
statistics disabled, so runqueue wait and timeslices remain null. Resource
status is partial despite complete query-PID coverage; null wait is not zero
wait. Runtime GC total estimates are 8.589241/9.748357 ms for B/C and include
mark-assist estimates of 0.173720/0.256522 ms. They are not subtracted from rusage
or added to each other and do not attribute CPU to a specific decode/allocation
function.

Backend count alone also fails to explain the direction:

| Diagnostic pair | B/C application backends | C minus B removal job, ms |
| --- | --- | ---: |
| Stage 1 | 2 / 3 | +25.754 |
| Stage 2 | 3 / 2 | +16.130 |
| Stage 4 | 3 / 2 | -37.134 |

The owner backend consistently carries 136 image-noop and 160 media-facts reads;
item/bitmap reads can spread across one or two data backends. The same backend
count pattern has opposite latency directions. The old acceptance batch did
not capture this distribution, so later runs cannot reconstruct it.

## Decision and actual closeout

The diagnostics support direct SQL callback savings and lower measured CPU in
the final removal pair, but not a stable end-to-end gain, an explanation of the
old adverse pairs, or a fixed regression. C remains unmerged. No further product
was selected or started; do not repeat the completed matrix or tune shared
settings to obtain a preferred result. Further work requires a bounded mechanism
question and fresh acceptance evidence before reconsidering source publication.

All seven products, workers, observers and host samplers exited. Actual closeout
reclaimed the inactive private compiler cache from 640,585,728 to 8,192 bytes,
owned compiler scratch, empty ext4 media fixtures and independently hash-retained
temporary RAM copies. Sources/common overlays, archives/manifests, raw evidence,
matching binaries, receipts and shared caches remain. PG, Goby, QEMU and reserve
403374 identities/settings are unchanged. Final persistent availability is
279,609,344 bytes and guest available memory is 6,651,854,848 bytes; these are
separate observations, not a measured total cleanup gain. No runtime work remains
active. Original workspace WIP remains outside publication and must be protected
using fresh identities.

## Evidence

Local evidence root: `.artifacts/embedded-source-attribution-20261007`.
The reports and machine summaries bind original runs, SQL/Prepare/resource
captures, plan samples and passive context receipts; earlier raw evidence is
unchanged.

| Artifact | SHA-256 |
| --- | --- |
| `analysis/attribution-report.md` | `6eb6b53557bed45bfde816d24afb5aba76a47a1b3c076e58ca5d13572cdd2414` |
| `analysis/pg-storage-context.md` | `a59248b6241253ef4535680706ef9def44f186a06a6b10adae3dbf29ed22337a` |
| `stage2/analysis/prepare-report.md` | `8e71c69b34ae955f64f3f9c0bfa7ab9615266ce6c84a7b51338346c5d4d561f5` |
| `stage2/analysis/pg-storage-context-stage2.md` | `9049f99f947fb25ee139d397ec8cbdb6a8ee01639bbe79333fb2d2ffa6cf8d3d` |
| `stage3/analysis/statistics-report.md` | `5fd4bae1058c9a84d3284c9d98e661cc14264f0957bf3f3a636bf815545e1cef` |
| `stage3/analysis/statistics-summary.json` | `d5bb77815b278b5a588da503a33035d41e37b7c915af13398793421cf0d4c7f2` |
| `stage4/analysis/cpu-report.md` | `563c2e9f02b2ac24983063ce6599faca9b6190b32ec09eecbe76395e6c468d0d` |
| `stage4/analysis/cpu-summary.json` | `accc6b621de6fb2a03b9b5b4013043268b1541e41b00e8f1cd51875b9fe0c0e9` |
| `actual-closure-receipt.json` | `763b0af62d2ac5b93f67019b24ba051e3ab6c19892952cfa76b42061b9a9b74c` |
| `actual-close-export-verification.json` | `01402fdf1c596a60bc59127e1f6e77353ea57a46d1a590d87baa7bfadb0170c4` |
