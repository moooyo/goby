# Scan routing-preparation authority optimization - October 3, 2026

## Measured source and delivered source

Measured baseline is `8f2e7e459b33ffc025d3a9c68675c36576eb18c8`, archive
SHA-256 `f1b52e9cd34b389107d09b2e02650224b5495de122118e196ae2bb3c1b1877a6`.
The measured candidate is that complete base archive plus an exact six-file
working-tree overlay, archive SHA-256
`148182310beceb3fe9cf93215fb4b5fc18a316a34b289990d36a28a8cfc1e33d`.
These are the measured input identities. Later source commit
`d7d047243febbb038c2c30556858ddbe171ff652` contains exactly the six frozen paths and
matches every changed-file SHA-256 in `source-freeze.json`, whose SHA-256 is
`726db129cb8266ae720484757128de1e822bc2d8fd910b47919a089c54f2c13e`.
The delivery commit is bound to the measured overlay bytes separately; it does
not replace the measured candidate archive identity.

## Outcome

The selected routing-preparation optimization is qualified and closed. Paired
candidate/baseline scan-job medians across the 400-descriptor libraries are
0.588-0.709 for cached_1, 0.609-0.623 for cached_2, 0.801-0.877 for catalog-cold,
and 0.792-0.810 for force_probe. The 160-file real-probe medians are 0.824 cold,
0.627 warm and 0.787 force. These are medians of the three matched ratios,
with complete sample ranges below. Seven individual paired phase observations
have C/B greater than 1 and remain retained. Three pairs support observed
central values and ranges, not stable tails. This comparison does not establish
return to the historical pre-isolation `912354f` baseline.

For flat_movies cached_1, every pair reduces raw SQL from 7665 to 4455 and raw
BEGIN/COMMIT from 1463 to 821 each. Strict AUTH SQL falls from 6470 to 3260,
and strict AUTH BEGIN/COMMIT from 1294 to 652 each. Every pair therefore removes
exactly 3210 SQL statements, 642 BEGINs and 642 COMMITs; raw deltas equal the
strict AUTH subset deltas in this case. This assigns work counts, not isolated
transaction milliseconds or elapsed/allocation causation.

All four selected correctness processes, four Linux/Windows amd64 CGO0
Goby/launcher builds, four pilot processes and twelve formal scan processes
qualified. The unchanged real 10,000-file functional corpus passed in the full
library race package, with exactly eleven approved opt-in skips. Actual HLS
encoder and Analysis decoder tagged race neighbors passed without skips as
correctness checks. Pilot samples are excluded from formal statistics. All 144
formal phase records, adverse observations and original raw evidence remain
in the complete structured record. The private environment is closed and
protected services are unchanged. This selected increment is complete; the
other performance scopes remain unselected and feature defaults, Docker images
and deployment have no new delivery in this increment.

## Selected implementation and correctness boundaries

The change removes routing-only authority transactions before scan queue
admission. A valid retained walk operation and its exact committed root row
supply fixed admission routing in primary preparation, primary metadata
preparation and sidecar preparation. When that trusted route is unavailable,
the existing SQL preparation path remains. A same-call standalone preparation
may pass its just-committed input row only as routing.

Every actual operation.Run grant still performs complete fresh task/root SQL
and exact row.same checks before filesystem I/O. Detached walk-row binding
checks, final owned-transaction proof, pure Busy rollback, actual close before
outside-queue full fresh retry, unknown-outcome retirement and committed-prefix
protection remain. Body64, canonical64, metadata4 and the existing
root/domain/queue/8-root/16-domain limits remain. The implementation introduces
no TTL/global authority cache, token registry, CTE fast path, long phase held
through an owned transaction or merged sidecar payload/catalog/retry work.

The source commit contains exactly these paths:

| Path | Role |
| --- | --- |
| `internal/library/primary_scan_read.go` | Primary routing preparation |
| `internal/library/scan_probe_pipeline.go` | Same-preparation committed input-row routing |
| `internal/library/primary_sidecar_read.go` | Retained walk operation fork for sidecar routing |
| `internal/library/scan_cached_checkpoint_integration_test.go` | Updated standalone cached AUTH budgets |
| `internal/library/primary_scan_routing_integration_test.go` | Primary routing and actual queued-authority guards |
| `internal/library/primary_sidecar_authority_queue_integration_test.go` | Sidecar actual queued-authority guard |

The new guards force actual BG2 queue occupancy and durable task/root changes,
then verify no callback, FD delivery or publication after an invalid grant and
actual operation/phase/owner retirement. The primary cases also watch kernel
IN_OPEN/IN_ACCESS events; the sidecar cases do not independently kernel-watch
brief directory/FP accesses. Static review confirms the sidecar callback's
first fresh authority check precedes payload I/O. Independent exact-delta review
found no confirmed blocking issue.

The full library command was
`go test -p 1 -count=1 -failfast -timeout=30m -json -race ./internal/library`.
Its log has 1281 top-level test PASS events, 2707 subtest PASS events and one
package PASS event. These event counts do not count distinct top-level tests.
The required `TestCatalogRealMediaCapacityScanAndCachedRescan` passed in 621.45
seconds. No required test was missing and no unexpected skip was recorded.
The actual HLS/Analysis checks used `-race -tags primary_io_measure` against
`./internal/server`. Correctness runs used Linux/amd64, CGO_ENABLED=1 and
GOMAXPROCS=4. Compilation, race and build times are separate from formal scan
performance samples.

The known existing raw local-NFO read boundary remains a separate unresolved
task. This routing-only increment does not repair it. Reconciliation algorithms,
existing-client A/V, Analysis PART2, GPU/native-hard qualification, whole-service
mixed capacity, images and deployment remain outside the selected work. The
HTTP 36-wave matrix was not remeasured; actual HLS/Analysis output supports
correctness only.

## Complete structured evidence and closure

The [structured record](scan-authority-optimization-20261003.json) is a complete
copy of the accepted static analysis `summary.json`, including all raw records,
individual observations, configuration/source identities, pilot records,
statistics, manifests and separate receipts. Only CRLF line endings are
normalized to LF for the repository's text policy; values and records are
unchanged. Published JSON SHA-256 is `a580b9caa769f896dbd5f21ef3fc3a4ccbf610f2912460c7b796ccffdeab7ab3`.
The external original retains SHA-256
`e165cd59bef966a4726c121ab082a1b921229d7b84096fe6a59e8def5961d9bd`.
The original analysis report SHA-256 is
`6b11918b0fd0ab6b159bc7f12d227c9216fe56f714ef5803c80e6f17770001fc`;
its frozen parser SHA-256 is
`7160c87709ee587e82404e6b46cda3d3f0e3c2159db509aefffab0a59234caf4`.

The retained final evidence receipt is
`D:/Code/goby/.artifacts/scan-authority-optimization-20261003/final-evidence-receipt.json`,
SHA-256 `b9658df6c360100f12f02a54839367207251749fbf51b85cbbcc7a51b3d252ac`.
It binds 86 exported evidence files with zero missing or mismatched hashes;
the twelve formal aggregate records match their immutable run receipts and raw
logs. The final analysis retains no evidence-quality issue. This external seal
binds the original evidence inputs and does not claim the hash of this wrapper.

The separate exact owned-resource closure receipt is
`D:/Code/goby/.artifacts/scan-authority-optimization-20261003/closure-receipt.json`,
SHA-256 `e87d4506a237916a28a9ca7aca7c2c790a35396446b149b0d32ab7ba1470fdbb`.
It reports saved private PostgreSQL PID 3023673/start 60867312 closed, mount 366
unmounted and `/dev/loop0` device 7:0 detached, with zero owned references,
mounts or closure errors and unchanged protected services. Frozen archives,
source trees, fixture image, PostgreSQL data, caches and raw/failure evidence
remain retained. Closure is the designated remote owner's exported observation,
not a new local runtime probe.

## Retained adverse paired job observations

| Profile / library / phase | Pair | C/B |
| --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 3 | 1.851262 |
| descriptor400 / flat_episodes / cached_1 | 3 | 1.927243 |
| descriptor400 / flat_movies / cached_2 | 3 | 1.845464 |
| descriptor400 / flat_movies / cold | 2 | 2.256952 |
| real160 / mixed_real_probe / cold | 2 | 2.018595 |
| real160 / mixed_real_probe / force | 2 | 1.152064 |
| real160 / mixed_real_probe / warm | 2 | 1.871123 |

## Complete accepted analysis

Generated from exported static evidence only. This analysis executes no product, test, build, runtime probe, SSH operation, or measurement.

## Frozen source identities

Source-freeze SHA256: `726db129cb8266ae720484757128de1e822bc2d8fd910b47919a089c54f2c13e`. Identity status: `confirmed`.

| Source | Role | Git base | Measured byte identity | Archive SHA256 | Archive hash confirmed |
| --- | --- | --- | --- | --- | --- |
| sourceB | baseline | `8f2e7e459b33ffc025d3a9c68675c36576eb18c8` | git-archive | `f1b52e9cd34b389107d09b2e02650224b5495de122118e196ae2bb3c1b1877a6` | True |
| sourceC | candidate | `8f2e7e459b33ffc025d3a9c68675c36576eb18c8` | exact-six-file-working-tree-overlay | `148182310beceb3fe9cf93215fb4b5fc18a316a34b289990d36a28a8cfc1e33d` | True |

The candidate is the frozen complete base archive plus the exact six-file working-tree overlay. The Git base is not a candidate commit identity. Any later delivery commit must be bound to these measured archive and changed-file bytes separately.

Four measurement-driver hashes are frozen and common to both staged sources:

| Driver | SHA256 |
| --- | --- |
| internal/library/scan_performance_measurement_integration_test.go | `e0a111271325454947ce1613252913cae3a275116312b07a3487f4b788d658f8` |
| internal/library/scan_performance_integration_test.go | `e0959141052e4bd9ef5685ec2511c72ea3c9681679df1330d268485a2b53d62c` |
| internal/server/http_get_stop_remeasure_integration_test.go | `e421241ee9079a8a139f5cd7355c18c64c7122dfd03540d767a8af09ae46f2a0` |
| internal/library/scan_probe_pipeline_performance_integration_test.go | `0ddce3357ca36d1eefe015bf9608c79df917236391330e1efb28e0dcdfbb0fcc` |

## Evidence availability

Final evidence qualification: `qualified_and_closed`. Each contract below retains its independent completion status.

| Contract | Status | Exported / expected processes | Evidence |
| --- | --- | --- | --- |
| Scan pilot | complete | 4 / 4 | scan-pilot-runs.json |
| Scan formal | complete | 12 / 12 | scan-formal-runs.json |
| Selected correctness | complete | 4 / 4 | correctness-runs.json |
| Linux/Windows builds | complete | 4 / 4 | build-runs.json |
| HTTP matrix | not_remeasured | not selected | The HTTP 36-wave performance matrix was not selected. Actual HLS/Analysis neighbors are correctness only. |

Formal scan uses three serial pairs in B-C / C-B / B-C order on the same selected private ext4/toolchain/configuration, without race or profiler instrumentation, with CGO_ENABLED=1 and GOMAXPROCS=4. Pilot observations qualify instrumentation and fixtures only and never enter formal statistics. Incomplete, failed, skipped, malformed, adverse, and unlisted records are retained in summary.json and their raw logs.

## Selected correctness and builds

Go event counts below separate top-level test, subtest, and package events. They are event counts, not a count of distinct top-level tests.

| Family | Case | Qualified | Pass events: top-level / subtest / package | Skip events: top-level / subtest / package | Missing required / unexpected skip | Raw-log SHA256 |
| --- | --- | --- | --- | --- | --- | --- |
| correctness | routing-new | True | 6 / 17 / 1 | 0 / 0 / 0 | [] / [] | `b31949535c91e24b08a5e3dc001fe56c3247cbec011423d62c115fad5bc3dc45` |
| correctness | authority-lifetime-neighbors | True | 83 / 91 / 1 | 0 / 0 / 0 | [] / [] | `aac27cee4910d603cccc1836ae39888bdfae28fafd0bd519834975adcbae8ae6` |
| correctness | library-full | True | 1281 / 2707 / 1 | 11 / 0 / 0 | [] / [] | `d165042ae57b54a36515fb2000dc58218a35f2d1431540bcd671bba2b3ceef51` |
| correctness | actual-primary-neighbors | True | 2 / 0 / 1 | 0 / 0 / 0 | [] / [] | `8cf5134b6e7d60a418bbe1354c5a80ea2d9a8d8a664fc19b0df9bad81368bee6` |
| build | linux-goby | True | not applicable | not applicable | [] / [] | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| build | linux-launcher | True | not applicable | not applicable | [] / [] | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| build | windows-goby | True | not applicable | not applicable | [] / [] | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| build | windows-launcher | True | not applicable | not applicable | [] / [] | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

Actual HLS/Analysis encoder/decoder neighbors are correctness evidence only. Their outputs do not constitute an HTTP performance remeasurement. The full library qualification requires the unchanged real 10,000-file functional corpus and only the eleven explicitly selected opt-in skips.

## Measurement scopes

| Metric | Scope and limit |
| --- | --- |
| job_elapsed_ns | Persisted scan job StartedAt to FinishedAt; distinct from terminal observation, package elapsed, and whole process wall. |
| observed_terminal_elapsed_ns | Observer interval through observed persisted terminal state; includes observation cadence. |
| worker_retirement_wait_ns | Same scan task leaving Store.active after terminal state; does not certify unrelated callback or native-process retirement. |
| Raw SQL / BEGIN / COMMIT / ROLLBACK | Tracer counts across the declared phase window; observer-context reads are excluded, while non-authority scanner work remains included. |
| Attributed AUTH counts | Strict classified subset of raw SQL and transactions; never add these to raw counts. Counts do not isolate SQL execution time or cause allocation/latency changes. |
| process_allocated_bytes / mallocs / GC | Process deltas through terminal observation and scan worker retirement, including tracer, observer, runtime and background work; not per-item costs or peak memory. |
| Heap before / after | Snapshots; not memory peaks. |
| Pool acquires / empty wait | Data-pool deltas including observer acquires; empty wait includes resource construction and excludes reserved-owner and ownership-mutex waits. |
| peak_probe_cohorts | Overlapping ProbeFile callbacks, including parsing and sequential children; not active native-process peak. |
| child_cpu_ns / inblock / outblock | RUSAGE_CHILDREN for reaped children; CPU and kernel block I/O, not logical source-read bytes or page-cache misses. |
| Store.Close | Outside phase windows; joined Store contract only. Per-store receipt values remain distinct in summary.json; process table totals are sums across that process's stores. |
| Package / whole process wall | Includes fixtures and observer work; whole wall additionally includes compilation. Neither substitutes for phase job times. |

Each phase has three formal samples per source. Tables report median [minimum, maximum]; paired C/B reports the median and range of the three matched ratios. At n=3 nearest-rank p95 equals the maximum and is not stable tail evidence. cached_1 and cached_2 remain separate. Cold means catalog-cold, not physically cold storage.

## Formal scan results

| Profile / library / phase | B job ms | C job ms | Paired C/B | B terminal ms | C terminal ms | B retirement us | C retirement us |
| --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 1067.771 [1008.728, 1133.088] | 714.936 [675.987, 2097.643] | 0.709 [0.633, 1.851] | 1254.502 [1253.787, 1255.354] | 754.218 [753.945, 2255.013] | 24.679 [13.901, 26.838] | 8.880 [8.330, 10.670] |
| descriptor400 / directory_episodes / cached_2 | 1210.682 [1069.738, 2631.579] | 687.508 [662.797, 754.082] | 0.623 [0.252, 0.643] | 1253.386 [1253.212, 2756.201] | 754.767 [753.406, 1004.711] | 8.639 [8.149, 10.250] | 13.839 [8.281, 20.069] |
| descriptor400 / directory_episodes / cached_after_incremental | 1063.358 [1023.611, 1118.079] | 708.920 [704.302, 732.894] | 0.667 [0.630, 0.716] | 1253.733 [1252.995, 1254.093] | 753.709 [753.591, 753.829] | 10.790 [9.549, 11.770] | 7.890 [7.290, 8.810] |
| descriptor400 / directory_episodes / cold | 1996.581 [1961.169, 5801.970] | 1600.095 [1593.306, 1624.068] | 0.801 [0.275, 0.828] | 2004.755 [2004.658, 6004.684] | 1754.038 [1753.860, 1754.849] | 8.879 [8.770, 14.151] | 11.150 [10.351, 22.889] |
| descriptor400 / directory_episodes / force_probe | 1755.514 [1741.575, 1853.799] | 1411.548 [1410.337, 1452.139] | 0.810 [0.761, 0.827] | 2004.610 [1752.786, 2005.676] | 1504.468 [1503.827, 1505.532] | 9.280 [8.710, 10.889] | 8.880 [2.800, 9.611] |
| descriptor400 / directory_episodes / incremental | 1056.274 [1051.816, 1108.822] | 755.095 [739.393, 778.802] | 0.700 [0.681, 0.740] | 1254.079 [1253.438, 1254.818] | 1005.203 [753.318, 1005.804] | 10.130 [8.789, 11.230] | 10.361 [7.080, 10.949] |
| descriptor400 / directory_episodes / task_owned_cached | 1491.509 [1358.263, 4105.154] | 969.712 [920.719, 994.893] | 0.667 [0.224, 0.714] | 1504.354 [1504.044, 4261.262] | 1004.343 [1003.923, 1006.424] | 7.089 [5.730, 9.790] | 8.549 [7.949, 9.579] |
| descriptor400 / flat_episodes / cached_1 | 3269.124 [3202.168, 3276.107] | 1921.955 [1820.779, 6171.355] | 0.588 [0.556, 1.927] | 3504.370 [3254.315, 3504.661] | 2005.412 [2003.608, 6254.154] | 11.530 [10.659, 15.129] | 9.379 [8.379, 10.120] |
| descriptor400 / flat_episodes / cached_2 | 3205.234 [3159.225, 11317.790] | 1947.936 [1924.801, 2002.005] | 0.609 [0.172, 0.625] | 3253.846 [3253.321, 11504.669] | 2004.064 [2003.685, 2254.480] | 10.089 [8.579, 10.859] | 10.280 [9.930, 14.830] |
| descriptor400 / flat_episodes / cached_after_incremental | 3114.531 [3029.510, 3476.429] | 2017.033 [1834.957, 2162.525] | 0.648 [0.528, 0.714] | 3254.296 [3253.971, 3503.391] | 2253.653 [2004.195, 2254.210] | 10.371 [9.339, 21.749] | 11.190 [10.690, 14.519] |
| descriptor400 / flat_episodes / cold | 6697.572 [6512.762, 6842.233] | 5725.302 [5457.602, 5871.381] | 0.877 [0.798, 0.879] | 6754.572 [6754.089, 7005.849] | 5754.054 [5503.763, 6012.995] | 11.539 [5.130, 15.491] | 10.860 [4.390, 25.600] |
| descriptor400 / flat_episodes / force_probe | 5846.542 [5691.975, 6004.633] | 4743.315 [4593.193, 4754.508] | 0.792 [0.786, 0.833] | 6004.814 [5755.085, 6254.666] | 4754.303 [4753.734, 5004.630] | 11.220 [8.490, 12.269] | 10.949 [9.809, 12.179] |
| descriptor400 / flat_episodes / incremental | 3286.009 [3088.259, 3402.849] | 2094.813 [2094.662, 2144.217] | 0.653 [0.616, 0.678] | 3503.437 [3253.471, 3503.679] | 2253.966 [2253.855, 2255.797] | 12.471 [9.580, 21.479] | 9.379 [9.269, 16.849] |
| descriptor400 / flat_episodes / task_owned_cached | 4205.811 [4184.838, 13819.434] | 2697.589 [2695.191, 2712.323] | 0.641 [0.196, 0.644] | 4254.196 [4253.515, 14005.542] | 2754.407 [2754.367, 2755.603] | 9.160 [8.259, 11.940] | 8.110 [7.950, 8.820] |
| descriptor400 / flat_movies / cached_1 | 2557.296 [2396.460, 7530.538] | 1510.239 [1506.433, 1580.646] | 0.618 [0.200, 0.630] | 2753.696 [2503.183, 7767.283] | 1753.273 [1752.871, 1754.676] | 10.401 [10.290, 10.870] | 9.350 [9.079, 9.820] |
| descriptor400 / flat_movies / cached_2 | 2612.097 [2607.963, 2641.628] | 1626.222 [1468.799, 4812.901] | 0.616 [0.562, 1.845] | 2754.096 [2753.599, 2754.710] | 1753.486 [1503.529, 5010.694] | 10.450 [8.890, 10.779] | 9.310 [9.250, 11.920] |
| descriptor400 / flat_movies / cached_after_incremental | 2584.297 [2501.248, 2700.187] | 1618.406 [1534.209, 1713.965] | 0.599 [0.594, 0.685] | 2754.054 [2754.036, 2755.095] | 1754.048 [1753.425, 1757.524] | 10.710 [9.650, 11.439] | 9.069 [8.870, 20.969] |
| descriptor400 / flat_movies / cold | 5537.033 [5509.537, 5617.307] | 4600.091 [4435.948, 12496.816] | 0.819 [0.805, 2.257] | 5754.706 [5754.385, 5754.906] | 4754.901 [4503.655, 12754.387] | 11.250 [10.179, 14.290] | 10.800 [9.269, 12.590] |
| descriptor400 / flat_movies / force_probe | 4779.537 [4635.345, 4870.908] | 3826.185 [3823.641, 3831.590] | 0.802 [0.786, 0.825] | 5004.811 [4754.327, 5005.839] | 4003.344 [4003.330, 4004.053] | 10.989 [10.369, 12.339] | 12.189 [9.740, 14.519] |
| descriptor400 / flat_movies / incremental | 2590.206 [2479.910, 2633.109] | 1574.883 [1510.776, 1634.145] | 0.621 [0.583, 0.635] | 2754.249 [2502.951, 2754.379] | 1753.573 [1753.504, 1754.096] | 10.720 [3.690, 10.989] | 9.381 [9.140, 11.479] |
| descriptor400 / flat_movies / task_owned_cached | 3457.837 [3450.609, 8310.479] | 2089.353 [2088.501, 2261.245] | 0.604 [0.272, 0.606] | 3505.339 [3505.193, 8506.970] | 2255.300 [2254.782, 2505.814] | 9.551 [7.820, 25.319] | 10.840 [8.819, 13.950] |
| real160 / mixed_real_probe / cold | 7221.593 [7220.927, 7488.222] | 6007.663 [5951.150, 14577.475] | 0.824 [0.802, 2.019] | 7255.716 [7255.468, 7506.585] | 6255.597 [6005.508, 14756.310] | 7.489 [7.450, 20.929] | 10.450 [9.380, 11.300] |
| real160 / mixed_real_probe / force | 6589.964 [6557.495, 6868.793] | 5278.183 [5185.220, 7554.654] | 0.787 [0.768, 1.152] | 6754.990 [6754.579, 7007.220] | 5505.086 [5254.517, 7759.339] | 7.389 [4.630, 24.869] | 10.529 [8.291, 10.699] |
| real160 / mixed_real_probe / warm | 3975.225 [3698.255, 4325.279] | 2525.460 [2491.075, 6919.891] | 0.627 [0.584, 1.871] | 4003.657 [3753.977, 4503.793] | 2755.541 [2506.000, 7012.922] | 10.740 [7.480, 13.041] | 8.790 [3.200, 15.729] |

### Raw SQL and strict AUTH subset

All entries are B / C medians [minimum, maximum], n=3. Per-pair deltas, rollback, manual/task classifications, and all numeric arrays remain in summary.json.

| Profile / library / phase | Raw SQL B / C | Raw BEGIN B / C | Raw COMMIT B / C | AUTH SQL B / C | AUTH BEGIN B / C | AUTH COMMIT B / C |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 3158 [3158, 3158] / 2008 [2008, 2008] | 573 [573, 573] / 343 [343, 343] | 573 [573, 573] / 343 [343, 343] | 2460 [2460, 2460] / 1310 [1310, 1310] | 492 [492, 492] / 262 [262, 262] | 492 [492, 492] / 262 [262, 262] |
| descriptor400 / directory_episodes / cached_2 | 3158 [3158, 3158] / 2008 [2008, 2008] | 573 [573, 573] / 343 [343, 343] | 573 [573, 573] / 343 [343, 343] | 2460 [2460, 2460] / 1310 [1310, 1310] | 492 [492, 492] / 262 [262, 262] | 492 [492, 492] / 262 [262, 262] |
| descriptor400 / directory_episodes / cached_after_incremental | 3158 [3158, 3158] / 2008 [2008, 2008] | 573 [573, 573] / 343 [343, 343] | 573 [573, 573] / 343 [343, 343] | 2460 [2460, 2460] / 1310 [1310, 1310] | 492 [492, 492] / 262 [262, 262] | 492 [492, 492] / 262 [262, 262] |
| descriptor400 / directory_episodes / cold | 5498 [5498, 5498] / 4348 [4348, 4348] | 813 [813, 813] / 583 [583, 583] | 813 [813, 813] / 583 [583, 583] | 3180 [3180, 3180] / 2030 [2030, 2030] | 636 [636, 636] / 406 [406, 406] | 636 [636, 636] / 406 [406, 406] |
| descriptor400 / directory_episodes / force_probe | 5174 [5174, 5174] / 4024 [4024, 4024] | 765 [765, 765] / 535 [535, 535] | 765 [765, 765] / 535 [535, 535] | 3180 [3180, 3180] / 2030 [2030, 2030] | 636 [636, 636] / 406 [406, 406] | 636 [636, 636] / 406 [406, 406] |
| descriptor400 / directory_episodes / incremental | 3254 [3254, 3254] / 2104 [2104, 2104] | 579 [579, 579] / 349 [349, 349] | 579 [579, 579] / 349 [349, 349] | 2475 [2475, 2475] / 1325 [1325, 1325] | 495 [495, 495] / 265 [265, 265] | 495 [495, 495] / 265 [265, 265] |
| descriptor400 / directory_episodes / task_owned_cached | 4896 [4896, 4896] / 3056 [3056, 3056] | 573 [573, 573] / 343 [343, 343] | 573 [573, 573] / 343 [343, 343] | 3936 [3936, 3936] / 2096 [2096, 2096] | 492 [492, 492] / 262 [262, 262] | 492 [492, 492] / 262 [262, 262] |
| descriptor400 / flat_episodes / cached_1 | 9464 [9464, 9464] / 5614 [5614, 5614] | 1767 [1767, 1767] / 997 [997, 997] | 1767 [1767, 1767] / 997 [997, 997] | 7740 [7740, 7740] / 3890 [3890, 3890] | 1548 [1548, 1548] / 778 [778, 778] | 1548 [1548, 1548] / 778 [778, 778] |
| descriptor400 / flat_episodes / cached_2 | 9464 [9464, 9464] / 5614 [5614, 5614] | 1767 [1767, 1767] / 997 [997, 997] | 1767 [1767, 1767] / 997 [997, 997] | 7740 [7740, 7740] / 3890 [3890, 3890] | 1548 [1548, 1548] / 778 [778, 778] | 1548 [1548, 1548] / 778 [778, 778] |
| descriptor400 / flat_episodes / cached_after_incremental | 9464 [9464, 9464] / 5614 [5614, 5614] | 1767 [1767, 1767] / 997 [997, 997] | 1767 [1767, 1767] / 997 [997, 997] | 7740 [7740, 7740] / 3890 [3890, 3890] | 1548 [1548, 1548] / 778 [778, 778] | 1548 [1548, 1548] / 778 [778, 778] |
| descriptor400 / flat_episodes / cold | 18734 [18734, 18734] / 14884 [14884, 14884] | 2727 [2727, 2727] / 1957 [1957, 1957] | 2727 [2727, 2727] / 1957 [1957, 1957] | 10620 [10620, 10620] / 6770 [6770, 6770] | 2124 [2124, 2124] / 1354 [1354, 1354] | 2124 [2124, 2124] / 1354 [1354, 1354] |
| descriptor400 / flat_episodes / force_probe | 17528 [17528, 17528] / 13678 [13678, 13678] | 2535 [2535, 2535] / 1765 [1765, 1765] | 2535 [2535, 2535] / 1765 [1765, 1765] | 10620 [10620, 10620] / 6770 [6770, 6770] | 2124 [2124, 2124] / 1354 [1354, 1354] | 2124 [2124, 2124] / 1354 [1354, 1354] |
| descriptor400 / flat_episodes / incremental | 9560 [9560, 9560] / 5710 [5710, 5710] | 1773 [1773, 1773] / 1003 [1003, 1003] | 1773 [1773, 1773] / 1003 [1003, 1003] | 7755 [7755, 7755] / 3905 [3905, 3905] | 1551 [1551, 1551] / 781 [781, 781] | 1551 [1551, 1551] / 781 [781, 781] |
| descriptor400 / flat_episodes / task_owned_cached | 15090 [15090, 15090] / 8930 [8930, 8930] | 1767 [1767, 1767] / 997 [997, 997] | 1767 [1767, 1767] / 997 [997, 997] | 12384 [12384, 12384] / 6224 [6224, 6224] | 1548 [1548, 1548] / 778 [778, 778] | 1548 [1548, 1548] / 778 [778, 778] |
| descriptor400 / flat_movies / cached_1 | 7665 [7665, 7665] / 4455 [4455, 4455] | 1463 [1463, 1463] / 821 [821, 821] | 1463 [1463, 1463] / 821 [821, 821] | 6470 [6470, 6470] / 3260 [3260, 3260] | 1294 [1294, 1294] / 652 [652, 652] | 1294 [1294, 1294] / 652 [652, 652] |
| descriptor400 / flat_movies / cached_2 | 7665 [7665, 7665] / 4455 [4455, 4455] | 1463 [1463, 1463] / 821 [821, 821] | 1463 [1463, 1463] / 821 [821, 821] | 6470 [6470, 6470] / 3260 [3260, 3260] | 1294 [1294, 1294] / 652 [652, 652] | 1294 [1294, 1294] / 652 [652, 652] |
| descriptor400 / flat_movies / cached_after_incremental | 7665 [7665, 7665] / 4455 [4455, 4455] | 1463 [1463, 1463] / 821 [821, 821] | 1463 [1463, 1463] / 821 [821, 821] | 6470 [6470, 6470] / 3260 [3260, 3260] | 1294 [1294, 1294] / 652 [652, 652] | 1294 [1294, 1294] / 652 [652, 652] |
| descriptor400 / flat_movies / cold | 15345 [15345, 15345] / 12135 [12135, 12135] | 2263 [2263, 2263] / 1621 [1621, 1621] | 2263 [2263, 2263] / 1621 [1621, 1621] | 8870 [8870, 8870] / 5660 [5660, 5660] | 1774 [1774, 1774] / 1132 [1132, 1132] | 1774 [1774, 1774] / 1132 [1132, 1132] |
| descriptor400 / flat_movies / force_probe | 14385 [14385, 14385] / 11175 [11175, 11175] | 2103 [2103, 2103] / 1461 [1461, 1461] | 2103 [2103, 2103] / 1461 [1461, 1461] | 8870 [8870, 8870] / 5660 [5660, 5660] | 1774 [1774, 1774] / 1132 [1132, 1132] | 1774 [1774, 1774] / 1132 [1132, 1132] |
| descriptor400 / flat_movies / incremental | 7759 [7759, 7759] / 4549 [4549, 4549] | 1469 [1469, 1469] / 827 [827, 827] | 1469 [1469, 1469] / 827 [827, 827] | 6485 [6485, 6485] / 3275 [3275, 3275] | 1297 [1297, 1297] / 655 [655, 655] | 1297 [1297, 1297] / 655 [655, 655] |
| descriptor400 / flat_movies / task_owned_cached | 12369 [12369, 12369] / 7233 [7233, 7233] | 1463 [1463, 1463] / 821 [821, 821] | 1463 [1463, 1463] / 821 [821, 821] | 10352 [10352, 10352] / 5216 [5216, 5216] | 1294 [1294, 1294] / 652 [652, 652] | 1294 [1294, 1294] / 652 [652, 652] |
| real160 / mixed_real_probe / cold | 22991 [22991, 22991] / 17575 [17575, 17575] | 2401 [2401, 2401] / 1724 [1724, 1724] | 2401 [2401, 2401] / 1724 [1724, 1724] | 14752 [14752, 14752] / 9336 [9336, 9336] | 1844 [1844, 1844] / 1167 [1167, 1167] | 1844 [1844, 1844] / 1167 [1167, 1167] |
| real160 / mixed_real_probe / force | 21988 [21988, 21988] / 16572 [16572, 16572] | 2241 [2241, 2241] / 1564 [1564, 1564] | 2241 [2241, 2241] / 1564 [1564, 1564] | 14752 [14752, 14752] / 9336 [9336, 9336] | 1844 [1844, 1844] / 1167 [1167, 1167] | 1844 [1844, 1844] / 1167 [1167, 1167] |
| real160 / mixed_real_probe / warm | 13156 [13156, 13156] / 7740 [7740, 7740] | 1569 [1569, 1569] / 892 [892, 892] | 1569 [1569, 1569] / 892 [892, 892] | 10912 [10912, 10912] / 5496 [5496, 5496] | 1364 [1364, 1364] / 687 [687, 687] | 1364 [1364, 1364] / 687 [687, 687] |

The following flat_movies cached_1 counts compare each matched pair. An equal raw/AUTH delta classifies count changes within that case; it does not isolate transaction milliseconds or establish elapsed/allocation causation.

| Pair | C minus B SQL | C minus B BEGIN | C minus B COMMIT | Raw deltas equal AUTH subset deltas |
| --- | --- | --- | --- | --- |
| 1 | -3210 | -642 | -642 | True |
| 2 | -3210 | -642 | -642 | True |
| 3 | -3210 | -642 | -642 | True |

### Allocation and pool observations

| Profile / library / phase | B allocated MiB | C allocated MiB | Paired C/B | B pool acquires | C pool acquires | B empty wait ms | C empty wait ms |
| --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 29.958 [29.863, 29.972] | 26.927 [26.782, 27.002] | 0.898 [0.894, 0.904] | 561 [561, 561] | 329 [329, 335] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_2 | 29.944 [29.931, 29.995] | 26.943 [26.781, 27.007] | 0.898 [0.895, 0.902] | 561 [561, 567] | 329 [329, 330] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_after_incremental | 29.769 [29.705, 29.784] | 27.005 [26.861, 27.054] | 0.907 [0.902, 0.911] | 561 [561, 561] | 329 [329, 329] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cold | 104.052 [104.022, 104.281] | 101.112 [101.106, 101.116] | 0.972 [0.970, 0.972] | 756 [756, 772] | 525 [525, 525] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / force_probe | 99.515 [99.480, 99.651] | 96.811 [96.560, 96.827] | 0.972 [0.970, 0.973] | 708 [707, 708] | 476 [476, 476] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / incremental | 33.910 [33.891, 33.926] | 30.952 [30.919, 30.985] | 0.913 [0.912, 0.913] | 566 [566, 566] | 335 [334, 335] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / task_owned_cached | 33.290 [33.155, 33.466] | 29.066 [29.050, 29.125] | 0.873 [0.870, 0.877] | 562 [562, 573] | 330 [330, 330] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_1 | 94.511 [94.242, 94.622] | 84.504 [84.251, 84.769] | 0.894 [0.893, 0.897] | 1758 [1757, 1758] | 982 [982, 999] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_2 | 94.137 [94.088, 94.462] | 84.593 [84.554, 84.625] | 0.899 [0.895, 0.899] | 1757 [1757, 1790] | 982 [982, 983] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_after_incremental | 94.220 [94.052, 94.405] | 84.847 [84.818, 84.850] | 0.900 [0.899, 0.902] | 1757 [1757, 1758] | 983 [982, 983] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cold | 389.370 [389.105, 389.674] | 379.465 [379.267, 379.810] | 0.975 [0.975, 0.975] | 2539 [2539, 2540] | 1765 [1764, 1766] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / force_probe | 371.790 [371.281, 371.860] | 361.662 [361.556, 361.892] | 0.973 [0.972, 0.975] | 2344 [2343, 2345] | 1569 [1569, 1570] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / incremental | 98.408 [98.278, 98.562] | 88.480 [88.391, 88.998] | 0.900 [0.897, 0.904] | 1763 [1762, 1763] | 988 [988, 988] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / task_owned_cached | 105.680 [105.203, 105.788] | 91.629 [91.532, 91.968] | 0.870 [0.865, 0.871] | 1761 [1761, 1800] | 985 [985, 985] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_1 | 76.159 [75.878, 76.594] | 68.173 [68.171, 68.177] | 0.895 [0.890, 0.898] | 1470 [1469, 1490] | 824 [824, 824] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_2 | 76.381 [76.104, 76.512] | 68.017 [67.748, 68.164] | 0.891 [0.885, 0.896] | 1470 [1470, 1470] | 824 [823, 837] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_after_incremental | 76.289 [76.124, 76.432] | 68.232 [68.095, 68.291] | 0.895 [0.891, 0.896] | 1470 [1470, 1470] | 824 [824, 824] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cold | 323.709 [323.596, 323.717] | 315.595 [315.584, 315.948] | 0.975 [0.975, 0.976] | 2122 [2122, 2122] | 1476 [1475, 1508] | 5.274 [4.980, 5.927] | 5.232 [4.369, 5.656] |
| descriptor400 / flat_movies / force_probe | 307.826 [307.314, 308.062] | 299.663 [299.648, 299.924] | 0.974 [0.973, 0.975] | 1959 [1958, 1959] | 1313 [1313, 1313] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / incremental | 80.273 [80.207, 80.282] | 72.040 [71.925, 72.051] | 0.897 [0.897, 0.897] | 1475 [1474, 1475] | 829 [829, 829] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / task_owned_cached | 85.816 [85.587, 85.830] | 74.235 [74.227, 74.372] | 0.867 [0.865, 0.867] | 1473 [1473, 1493] | 826 [826, 827] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| real160 / mixed_real_probe / cold | 469.322 [469.128, 469.385] | 457.388 [456.463, 458.302] | 0.975 [0.973, 0.976] | 2231 [2231, 2232] | 1550 [1549, 1584] | 4.942 [4.832, 5.326] | 5.514 [5.404, 5.618] |
| real160 / mixed_real_probe / force | 453.378 [452.643, 453.512] | 441.006 [440.632, 442.031] | 0.973 [0.973, 0.975] | 2069 [2069, 2070] | 1387 [1386, 1396] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| real160 / mixed_real_probe / warm | 97.686 [97.338, 97.958] | 85.622 [85.036, 86.073] | 0.880 [0.868, 0.881] | 1578 [1577, 1580] | 896 [895, 913] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |

### Real-probe child resource observations

| Phase | B probe cohorts | C probe cohorts | B child CPU ms | C child CPU ms | B child in / out blocks | C child in / out blocks |
| --- | --- | --- | --- | --- | --- | --- |
| cold | 2 [2, 2] | 2 [2, 2] | 1186.774 [1182.976, 1188.129] | 1155.909 [1143.954, 1227.554] | 0 [0, 0] / 0 [0, 0] | 0 [0, 0] / 0 [0, 0] |
| force | 2 [2, 2] | 2 [2, 2] | 1180.530 [1172.934, 1199.825] | 1159.405 [1153.996, 1179.718] | 0 [0, 0] / 0 [0, 0] | 0 [0, 0] / 0 [0, 0] |
| warm | 0 [0, 0] | 0 [0, 0] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0 [0, 0] / 0 [0, 0] | 0 [0, 0] / 0 [0, 0] |

### Profile-level process observations

Store.Close total sums the separately retained per-store receipts within each process. These process values include fixtures and observers; wall includes compilation.

| Profile | B package s | C package s | B whole wall s | C whole wall s | B Store.Close total ms | C Store.Close total ms |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 | 76.260 [75.794, 83.516] | 55.768 [47.518, 57.016] | 76.858 [76.369, 84.078] | 56.329 [48.151, 57.581] | 0.327 [0.314, 0.344] | 0.370 [0.309, 0.530] |
| real160 | 18.545 [18.045, 19.057] | 14.568 [14.307, 29.836] | 19.142 [18.597, 19.625] | 15.157 [14.860, 30.397] | 0.382 [0.371, 0.589] | 0.468 [0.412, 1.177] |

## Independent source, archive, protected and closure references

Source archives establish measured input bytes. Staged and post manifests establish enumerated source/driver bytes. Per-run source/protected before-and-after receipts establish those run contracts. The remote observation receipt and final evidence receipt are separate from this local calculation. Closure requires its own exact owned-resource receipt.

| Evidence | Exported | SHA256 | Expected hash match |
| --- | --- | --- | --- |
| source-freeze.json | True | 726db129cb8266ae720484757128de1e822bc2d8fd910b47919a089c54f2c13e | True |
| sourceB-scan-pilot | True | a945b38b554c46cac18e3940fb8be28570f0c52d4bdc0354bab28e6171a45f65 | True |
| sourceB-scan-formal | True | a945b38b554c46cac18e3940fb8be28570f0c52d4bdc0354bab28e6171a45f65 | True |
| sourceB-final | True | a945b38b554c46cac18e3940fb8be28570f0c52d4bdc0354bab28e6171a45f65 | True |
| sourceB-driver | True | 6c75ee751f0c3a08806ee7ec8d75f7b12b3331efa128196f3636abc9c699040b | not a source-manifest comparison |
| sourceC-scan-pilot | True | 1bb49823a115d9b4d7e048bb669a45e786288ebd6d6bc8943c5e200613f41ef9 | True |
| sourceC-scan-formal | True | 1bb49823a115d9b4d7e048bb669a45e786288ebd6d6bc8943c5e200613f41ef9 | True |
| sourceC-final | True | 1bb49823a115d9b4d7e048bb669a45e786288ebd6d6bc8943c5e200613f41ef9 | True |
| sourceC-driver | True | 6c75ee751f0c3a08806ee7ec8d75f7b12b3331efa128196f3636abc9c699040b | not a source-manifest comparison |
| setup-receipt.json | True | 0a7766f57f02fc50aa2f05592c4b5834676a645c33388f507330d3a1b09ca257 | separate receipt |
| preparation-receipt.json | True | 483402443e19f33c0fe46a176add9f103a50379542d730bcfc5a752a82ab294a | separate receipt |
| scan-pilot-observation-receipt.json | True | 36a51c5e6f5904035f6510e30e761d6b8217f049fb716d17169df973d7566705 | separate receipt |
| scan-formal-observation-receipt.json | True | c6346634422cdb441381c4567e13ad61533e7237aa0e6d6584a11a20a7fb855c | separate receipt |
| closure-receipt.json | True | e87d4506a237916a28a9ca7aca7c2c790a35396446b149b0d32ab7ba1470fdbb | separate receipt |
| final-evidence-receipt.json | True | b9658df6c360100f12f02a54839367207251749fbf51b85cbbcc7a51b3d252ac | separate receipt |

The exported closure receipt reports closed for saved private PostgreSQL PID `3023673` / start `60867312`, mount `366`, and loop `/dev/loop0`. The full exact process, mount, loop, protected-resource and bounded audit facts remain in summary.json. This is the designated owner's exported closure receipt, not a new local runtime observation.

## Retained issues

No evidence-quality issue was found in the selected exported contracts.

## Scope limits

This is a bounded routing-preparation source comparison. It does not qualify existing clients, A/V behavior, Analysis PART2, GPU/native-hard behavior, reconciliation algorithms, whole-service mixed capacity, physical cold storage, deployment, or larger corpora. Actual HLS/Analysis neighbors support correctness only. The HTTP 36-wave matrix was not selected for this change and no HTTP performance improvement is claimed. Pairing reduces order ambiguity; three pairs support observed central values and ranges, not stable tails or isolated causal attribution.

The implementation design retains fresh SQL after each actual Run grant, final owned-transaction proof, quotas and actual retirement. This report evaluates exported evidence for that routing-only change; it performs no source optimization.

