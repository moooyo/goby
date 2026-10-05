# Scan progress batching - October 5, 2026

This implements the requested batched-progress semantics and passes the selected
race/full functional checks. Progress transaction counts fall substantially and
19 of 21 job paired medians are lower. It is an optimization with known measured
tradeoffs, not an all-scenario speedup or a confirmed database-cause repair.

Two force regressions remain prominent:

- Flat-episodes force C/B median is 1.049313307342171, slower in all three blocks.
  B samples are [2565.032,3209.296,2569.098] ms; C [3351.579,3367.557,2586.560].
- Directory force C/B median is 1.3174699239223628, slower in blocks 1 and 2.
  B samples are [773.289,764.820,736.240] ms; C [1018.785,1136.604,698.751].

These adverse samples are retained, not dismissed as noise or treated as resolved regressions. Movie
incremental median is 0.5105771518 with all three blocks faster; directory
incremental is 0.7763186032 with block 1 slower. Movie cached_1/cached_2/task-owned
cached medians are 0.21904/0.2542/0.22479, all three blocks faster. Common cached
and incremental work improves while the two force medians do not.

Committed source is `d993454fef258301d3ff967c083fa2b2173be802`, directly after
baseline `3585a84d612c01d8eead53a3b593d7ddd2557285`. Source/archive measurement
identity is separate from the following documentation and main publication.

## Behavior and retained safety

The previous diagnosis found many per-file progress COMMIT waits. This change
coalesces progress persistence without changing accepted media-counter atomicity.
A checkpoint is due after 64 Scanned entries or 500 ms elapsed at a cooperative
progress call. Every call still checks ctx and Available, even when its SQL write
is coalesced. A blocked probe does not trigger a background wall-clock checkpoint;
500 ms is not a promise that the UI updates while the worker is blocked.

Successful primary/theme transactions mark their already-saved Scanned/Added/
Updated triplet, allowing the following progress path to reuse that checkpoint.
Successful cached/primary completion no longer executes the per-file CTE fallback
merely to repeat progress publication. Explicit forced checkpoints, error handling and terminal checks remain.
Deleted-terminal recognition compares the last saved triplet against exact retained
history, avoiding false rejection from a newer pending Scanned count. It still
requires the existing execution/deletion evidence rather than inventing history.

Cancellation, token/owner fencing, source/mapping/data checks, accepted media
publication, terminal state and actual retirement remain required. This adds no
background goroutine, lock or configuration option. Owner/safety locks are not
removed to avoid COMMIT cost.

The explicit tradeoff is UI progress lag and possible loss of the most recent
uncheckpointed Scanned count after a crash. Accepted media counters remain atomic
with their catalog transactions. Final flush and forced/error/terminal behavior
are retained; coalescing is not permission to lose accepted catalog state.

## Frozen source and completed verification

Exactly 15 changed paths contain seven production files and eight tests. Four
measurement drivers have identical archive-LF bytes; no diagnostic overlay is
used. Freeze SHA-256 is
`2d2e1ade0ef7973c5cd09bab903c0b9e11215c673cc71e3be56cc10dfd86bde4`.
Baseline archive SHA-256 is
`ed4d1736ce5daba198706d6bb76361d0e9256f31e89a50bbed01ea3a67d6442c`;
candidate archive SHA-256 is
`36dc3ad73ba16ef7c9d6b011b9ab12c1cde7aa7648d462aa7d950db9dd8b9f53`.

| Check | Actual result |
| --- | --- |
| Targeted race v1 | PASS: 68 top-level/114 subtests, all 16 required, zero skips |
| Ordinary full Library | PASS: 1,328 top-level/2,801 subtests, all 16 required; 11 predeclared skips |
| Existing real-media functional case | PASS: 10,688 catalog items/10,000 leaves, 10,001 probes, four jobs, eight UserData witnesses and Store.Close |
| TRACE1 counts | Two B/C processes PASS; 42 phase records |
| TRACE0 timing | Six processes PASS in B-C/C-B/B-C order; 126 formal records, no added samples |
| Owned close/export | Complete; all 53 sealed files local and hash-matched |

Targeted and full wall times including compilation are 121.6151 and 831.65 seconds,
not phase-performance results. The existing real-media case took 214.40 seconds
as functional evidence, not a matched capacity speedup. The native-handle capability
check did not skip. The eleven declared opt-in/environment skips stay recorded
in the immutable full result; no all-opt-in acceptance is claimed. Overlapping
correctness selections are not summed into a larger pass total.

## Complete paired job results

Each descriptor400 process retains all 21 phases/803 probes. The two TRACE1 n=1
count runs are excluded from timing; the six TRACE0 runs supply three same-block
C/B comparisons per phase. All reverse blocks and ranges are reported below.
No pilot, historical H, real160 performance, profiler or extra matrix was added.
Three samples do not establish tail latency. The root's
independent paired audit matched all 63 per-source job comparisons to the analyst.

| Scenario | B samples ms | C samples ms | C/B three ratios | Median [min,max] | Reverse blocks | Signed deltas ms |
| --- | --- | --- | --- | --- | --- | --- |
| flat_movies/cold | 2923.970, 3793.348, 2273.647 | 1887.666, 2839.627, 1836.188 | 0.6456, 0.7486, 0.8076 | 0.7486 [0.6456,0.8076] | [] | -1036.304, -953.721, -437.459 |
| flat_movies/cached_1 | 1143.828, 1254.813, 437.419 | 250.547, 269.080, 192.132 | 0.2190, 0.2144, 0.4392 | 0.2190 [0.2144,0.4392] | [] | -893.281, -985.733, -245.287 |
| flat_movies/cached_2 | 1150.803, 1168.002, 432.304 | 292.529, 238.624, 209.522 | 0.2542, 0.2043, 0.4847 | 0.2542 [0.2043,0.4847] | [] | -858.274, -929.378, -222.782 |
| flat_movies/task_owned_cached | 788.291, 2980.017, 843.273 | 249.537, 248.974, 189.562 | 0.3166, 0.0835, 0.2248 | 0.2248 [0.0835,0.3166] | [] | -538.754, -2731.043, -653.711 |
| flat_movies/incremental | 504.153, 1442.925, 479.146 | 277.612, 345.772, 244.641 | 0.5507, 0.2396, 0.5106 | 0.5106 [0.2396,0.5507] | [] | -226.541, -1097.153, -234.505 |
| flat_movies/cached_after_incremental | 427.393, 1158.082, 436.448 | 248.687, 253.715, 189.928 | 0.5819, 0.2191, 0.4352 | 0.4352 [0.2191,0.5819] | [] | -178.706, -904.367, -246.520 |
| flat_movies/force_probe | 2063.485, 3492.858, 2013.248 | 2599.643, 2607.704, 1719.805 | 1.2598, 0.7466, 0.8542 | 0.8542 [0.7466,1.2598] | [1] | +536.158, -885.154, -293.443 |
| flat_episodes/cold | 4342.264, 4867.061, 2681.690 | 3208.897, 4416.532, 2415.165 | 0.7390, 0.9074, 0.9006 | 0.9006 [0.7390,0.9074] | [] | -1133.367, -450.529, -266.525 |
| flat_episodes/cached_1 | 1578.339, 1738.836, 633.168 | 557.286, 536.838, 335.640 | 0.3531, 0.3087, 0.5301 | 0.3531 [0.3087,0.5301] | [] | -1021.053, -1201.998, -297.528 |
| flat_episodes/cached_2 | 1499.749, 1646.346, 617.715 | 507.168, 497.550, 371.155 | 0.3382, 0.3022, 0.6009 | 0.3382 [0.3022,0.6009] | [] | -992.581, -1148.796, -246.560 |
| flat_episodes/task_owned_cached | 1021.146, 3214.185, 1000.527 | 564.699, 551.842, 334.946 | 0.5530, 0.1717, 0.3348 | 0.3348 [0.1717,0.5530] | [] | -456.447, -2662.343, -665.581 |
| flat_episodes/incremental | 641.573, 1902.711, 666.548 | 539.925, 542.043, 429.810 | 0.8416, 0.2849, 0.6448 | 0.6448 [0.2849,0.8416] | [] | -101.648, -1360.668, -236.738 |
| flat_episodes/cached_after_incremental | 643.350, 1915.211, 649.998 | 540.148, 471.035, 329.966 | 0.8396, 0.2459, 0.5076 | 0.5076 [0.2459,0.8396] | [] | -103.202, -1444.176, -320.032 |
| flat_episodes/force_probe | 2565.032, 3209.296, 2569.098 | 3351.579, 3367.557, 2586.560 | 1.3066, 1.0493, 1.0068 | 1.0493 [1.0068,1.3066] | [1, 2, 3] | +786.547, +158.261, +17.462 |
| directory_episodes/cold | 1095.211, 1547.322, 806.939 | 1198.790, 1157.708, 728.620 | 1.0946, 0.7482, 0.9029 | 0.9029 [0.7482,1.0946] | [1] | +103.579, -389.614, -78.319 |
| directory_episodes/cached_1 | 594.807, 652.692, 277.244 | 402.384, 376.151, 203.179 | 0.6765, 0.5763, 0.7329 | 0.6765 [0.5763,0.7329] | [] | -192.423, -276.541, -74.065 |
| directory_episodes/cached_2 | 297.373, 789.022, 282.409 | 413.247, 329.157, 201.525 | 1.3897, 0.4172, 0.7136 | 0.7136 [0.4172,1.3897] | [1] | +115.874, -459.865, -80.884 |
| directory_episodes/task_owned_cached | 439.492, 1047.627, 383.006 | 370.844, 397.447, 223.138 | 0.8438, 0.3794, 0.5826 | 0.5826 [0.3794,0.8438] | [] | -68.648, -650.180, -159.868 |
| directory_episodes/incremental | 313.332, 756.073, 317.988 | 443.150, 433.723, 246.860 | 1.4143, 0.5737, 0.7763 | 0.7763 [0.5737,1.4143] | [1] | +129.818, -322.350, -71.128 |
| directory_episodes/cached_after_incremental | 307.481, 725.186, 288.408 | 435.041, 342.215, 197.085 | 1.4149, 0.4719, 0.6834 | 0.6834 [0.4719,1.4149] | [1] | +127.560, -382.971, -91.323 |
| directory_episodes/force_probe | 773.289, 764.820, 736.240 | 1018.785, 1136.604, 698.751 | 1.3175, 1.4861, 0.9491 | 1.3175 [0.9491,1.4861] | [1, 2] | +245.496, +371.784, -37.489 |

## Progress work counts and interpretation limits

The separate TRACE1 count runs show:

| Scenario | COMMIT B to C | SQL B to C |
| --- | ---: | ---: |
| Movie cached_1 | 171 to 13 | 1203 to 414 |
| Movie incremental | 173 to 15 | 1277 to 488 |
| Directory incremental | 85 to 37 | 842 to 603 |
| Directory force | 131 to 83 | 2062 to 1822 |

These are explicit command counts over the phase including admission/tail work;
movie incremental's 173 is not the older diagnosis's job-clipped 171. Full counts
for all 21 scenarios remain external. They support reduced progress-transaction
work, not allocation of untraced milliseconds or proof of PG WAL/fsync/host causes.
Formal SQL fields are unavailable/null, not zero. Persisted job elapsed time stays
separate from terminal polling, package/wall time, Store.Close and retirement.
The old n=1 diagnosis and previous three-source batches are not pooled here.
Mixed temporal regimes and every adverse observation remain; none is discarded.

The first performance-admission SSH attempt failed in the local signing agent
before any remote command. Its original error is retained; one identical bounded
retry succeeded without credential/configuration changes. This is not a product
failure or an added/replacement performance sample.

## Closed resources and retained evidence

Final B/C source manifests match staged bytes. All selected invocations exited;
empty owned compiler scratch and physical-fixture TMPDIR were removed. Persistent
sources, full archives, raw/failed records, observations and receipts remain.
Shared build/module caches, standard PostgreSQL, Docker and old environments are
retained. PG/Goby identities and reserve 403,374 are unchanged. Closing persistent
availability is 10,812,764,160 bytes. No local product verification or publication-
owned SSH ran. Verification setup created no private PG, mount, loop, cache copy
or reserve change; this does not assert that full-test helpers never use namespaces
or mounts.

Evidence is external at `D:/Code/goby/.artifacts/scan-progress-batching-20261005`.
Only this report and the complete historical handoff follow the accepted source.

| Record | SHA-256 |
| --- | --- |
| evidence/correctness-receipt.json | 1403e718a9a1a4ad4d3ffcb2f1e8df9a3bb577ee5e8d03768eaa44f1d6d50ad0 |
| evidence/formal-receipt.json | 8a2be5f4eedf0703853990bb3e51427b6eff6b40e0873670be89b42304cafccb |
| analysis/performance-report.md | 79d178834ba5f60e3cebd2ad5784ef185d11888c059f0206587da6a2de742673 |
| analysis/summary.json | 03b3615349560902adfd3a8216889b4eb1396a6884a1a16937efe568a44429b6 |
| root-paired-audit.json | 9363a91fabec185b1d7239a2ab4e8a00502d9750234c2f3b2e0b4aa1a2ebd7da |
| evidence/closure-receipt.json | a37c9d8b9fcae7f95e778ad8b6dcb2ac14411f6c10c03496b3518662387fd36b |

The external publication receipt records exact subsequent main/origin readback
and all preserved 199 original byte states plus 56 prior distinct WIP hashes.
