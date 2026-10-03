# Media performance remeasurement - October 3, 2026

## Outcome

The selected remeasurement is complete and shows a scan regression. In the
400-descriptor profiles, median paired current/baseline job-time ratios are
2.16-2.24x for catalog-cold scans, 4.10-5.91x for cached_1/cached_2, and
2.50-2.62x for forced probes. The 160-file real-probe profile has the same
direction: 2.13x cold, 4.15x warm, and 2.35x forced. These are observed paired
ratios from three formal pairs, not ratios of source medians. Adverse rows and
large current-source observations remain in the complete record. Scan
optimization is not complete. A future optimization decision can target repeated
strict authority work while preserving every required permission and freshness
check; no repair is implemented or accepted by this record.

For flat_movies cached_1, all three pairs contain exactly 6460 additional raw
SQL statements and 1292 additional BEGINs and COMMITs. The identical deltas are
fully classified in the strict authority subset for that case. This assigns
work counts; it does not establish elapsed-time or allocation causation or
isolated transaction milliseconds.

The six cached HTTP cases have median paired p50 ratios of 0.987-1.058.
Their p95/p99 observations vary by case and retain baseline adverse waves;
three waves do not establish stable tails or broad capacity. A running encoder's
Stop response median is 15.302ms on baseline and 16.122ms on current. Actual
exit/reap observation upper-bound ranges overlap: exit is
379.170-515.510ms versus 217.326-526.941ms; reap/group absence is
379.181-515.523ms versus 221.239-530.316ms. With n=3, neither these observations
nor cached completed-producer Stop can support a stable native retirement tail.

All 12 formal scan processes and six formal HTTP processes passed with zero
skips. Pilot qualification passed separately and contributes no statistics.
The private environment is closed, evidence is retained, and protected services
are unchanged. Only the selected measurement task is complete. The other
performance tasks remain paused; this record changes no production behavior,
feature default, Docker image or deployment.

## Measured source and published instrumentation

The measured production sources are baseline
`912354f2a48b5043a8824246f2fa011f8f34bbf1` and current
`66680f2beb366e0d32f1b44b3a873afcbcb05b21`. The later measurement-driver commit
`9f76c28f0c57913cf6e5449835f7375175b7a8a4` is not a measured production revision. It contains only the four
Linux test files below. Both measured trees received identical driver bytes.
Their production-preserved manifests matched after the formal runs.

| Measurement driver | SHA-256 |
| --- | --- |
| `internal/library/scan_performance_integration_test.go` | `e0959141052e4bd9ef5685ec2511c72ea3c9681679df1330d268485a2b53d62c` |
| `internal/library/scan_probe_pipeline_performance_integration_test.go` | `0ddce3357ca36d1eefe015bf9608c79df917236391330e1efb28e0dcdfbb0fcc` |
| `internal/library/scan_performance_measurement_integration_test.go` | `e0a111271325454947ce1613252913cae3a275116312b07a3487f4b788d658f8` |
| `internal/server/http_get_stop_remeasure_integration_test.go` | `e421241ee9079a8a139f5cd7355c18c64c7122dfd03540d767a8af09ae46f2a0` |

The complete [structured measurement record](media-performance-remeasure-20261003.json)
is a complete copy of the accepted static analysis `summary.json`, including raw
request arrays, individual observations, source/configuration identities,
pilot records, complete statistics and closure receipt. Only CRLF line endings
are normalized to LF for the repository's text policy; values and records are
unchanged. Its published SHA-256 is
`126f6405634922cf4d0e64b1f123dc7e86bb2854d02918ba4489f1aacce9c3c6`. The exact external original retains SHA-256
`93df304f3d88ddfe33d5a5ddb331006582adc6305488df2df1d5ea28073ab4f4`.
The original analysis report SHA-256 is
`4e8c0bed18e9f4f3b59659368fbe39b25112400be3ce03efa78147d9d932b6b9`.
The external evidence seal is retained at
`D:/Code/goby/.artifacts/performance-remeasure-20261003/analysis/evidence-seal.json`,
SHA-256 `a19970f3182ec8f9f39c1823ef6484147d2f791791d91c7e3f6d7c78b2999c6f`.
The closed-environment receipt SHA-256 is
`12cce2924e93246c71e4509ce54f687f7973e59b59b8dc78c6311e9fcedf69a7`.
The external seal binds the original analysis inputs; it does not claim the
SHA-256 of this publication wrapper.
The retained remote evidence archive is
`D:/Code/goby/.artifacts/performance-remeasure-20261003/performance-remeasure-evidence.tgz`,
SHA-256 `823c8ca7585b24c48f1fed68d1b5c30472aba4789c55518fc2ab07423d8df8dc`.
The final remote evidence receipt is
`D:/Code/goby/.artifacts/performance-remeasure-20261003/final-evidence-receipt.json`,
SHA-256 `9242d5338c73711035b8727a618b0344d0838a62378a077b0f8c2298f6b38aac`.
It records the full-source, production-preserved, driver and formal evidence
identities. The accompanying `remote-verification-receipt.md` in that artifact
root summarizes the remote-only execution and exact resource closure.

## Complete accepted measurement evidence

This document is generated from exported static evidence. It executes no product, test, build, runtime probe, SSH operation, or new measurement.

Baseline: `912354f2a48b5043a8824246f2fa011f8f34bbf1`. Current: `66680f2beb366e0d32f1b44b3a873afcbcb05b21`.

Three fresh formal pairs use B-C / C-B / B-C order. Pilot evidence is retained solely for instrument and fixture qualification and is excluded from all formal statistics. Formal incomplete, failed, skipped, and adverse rows remain in [structured measurement record](media-performance-remeasure-20261003.json); incomplete matrices cannot support a final performance claim. Non-JSON transport/build log lines are retained with their original line numbers; ordinary module-download text does not invalidate complete hashed Go event streams.

## Evidence availability

| Family | Mode | Exported processes | Expected processes | Status |
| --- | --- | --- | --- | --- |
| scan | pilot | 4 | 4 | complete |
| scan | formal | 12 | 12 | complete |
| http | pilot | 2 | 2 | complete |
| http | formal | 6 | 6 | complete |

## Metric contracts

- Scan job elapsed, terminal-observation elapsed, same-task Store.active retirement wait, profile/package elapsed, and process wall including compilation are separate windows. Catalog-cold is not OS-cache-cold. Store.Close is a separate profile-level join contract.
- Raw SQL and raw BEGIN/COMMIT/rollback counts are distinct from strictly attributed authority statements and transactions. Authority transaction counts are a subset classification and must not be added to raw counts. SQL count differences do not isolate SQL time.
- Scan MemStats are process deltas through terminal observation and scan worker retirement, including tracer, observer, Go runtime, and background work. Heap before/after is a snapshot, not peak memory. Pool counters include observer acquires and resource construction while excluding reserved-owner and ownership-mutex waits.
- Real-probe cohort peak counts overlapping ProbeFile callbacks, including parsing and sequential children; it is not active native-process peak. RUSAGE_CHILDREN is for reaped child CPU and kernel block I/O, not logical source-read bytes or page-cache misses.
- At three scan samples per phase, nearest-rank p95 is the maximum and provides no stable tail evidence. cached_1 and cached_2 stay separate; this report does not pool their six observations.
- Each cached HTTP wave has 256 complete TCP Do/body-read/body-close spans. Its p50 averages the middle two integer nanosecond durations; p95/p99 use nearest rank. Reported three-wave percentile summaries are medians and ranges of wave percentiles, not percentiles pooled across requests.
- HTTP cases use production Data12/usable11 plus Control4, total16, 32 prewarmed connections, and unchanged feature defaults. Only source-admission v5 span contracts belong to this comparison. Admission stage sums overlap and are not additive or isolated filesystem/SQL execution time.
- Cached Stop producers are already complete. Cached Stop response, handler drain, and exact lifetime/output/lease completion cannot establish native exit/reap. Drain/completion values are observation upper bounds including joins and assertions.
- Each live Stop uses one actually running paced software encoder. Response, pinned pidfd exit, actual stat ENOENT and original group ESRCH, consumer join, and Store/dependency close stay separate. Retirement observations include 1ms polling and recorder cost as upper bounds. Three samples support min/median/max, not stable p99.

## Formal scan results

Values are median [minimum, maximum], n=3 per source and phase. C/B values summarize paired ratios. Timing windows retain their own columns.

| Profile / library / phase | B job ms | C job ms | Paired C/B | B terminal ms | C terminal ms | B retirement us | C retirement us |
| --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 261.236 [258.117, 262.567] | 1076.720 [1021.538, 1085.915] | 4.101 [3.958, 4.157] | 504.572 [503.012, 505.695] | 1253.819 [1253.517, 1254.212] | 17.420 [8.149, 27.728] | 16.429 [11.630, 22.910] |
| descriptor400 / directory_episodes / cached_2 | 269.569 [255.925, 271.035] | 1082.724 [1053.140, 1174.303] | 4.115 [3.995, 4.356] | 504.225 [504.147, 504.696] | 1254.400 [1253.906, 1254.530] | 11.890 [9.659, 13.520] | 9.700 [9.289, 13.059] |
| descriptor400 / directory_episodes / cached_after_incremental | 271.930 [255.433, 308.381] | 1037.399 [1025.320, 1048.651] | 3.856 [3.364, 4.014] | 504.053 [503.776, 504.346] | 1254.795 [1253.005, 1255.244] | 10.370 [10.351, 37.059] | 11.090 [11.010, 13.709] |
| descriptor400 / directory_episodes / cold | 893.779 [888.446, 916.165] | 2052.529 [1893.665, 2328.294] | 2.240 [2.119, 2.621] | 1004.036 [1003.589, 1004.336] | 2253.682 [2003.622, 2503.642] | 9.170 [9.050, 9.679] | 9.540 [8.729, 24.639] |
| descriptor400 / directory_episodes / force_probe | 705.027 [692.510, 726.818] | 1811.258 [1810.163, 1825.375] | 2.569 [2.511, 2.614] | 754.127 [753.331, 754.165] | 2004.223 [2004.099, 2006.702] | 8.850 [8.430, 9.909] | 9.619 [9.179, 11.830] |
| descriptor400 / directory_episodes / incremental | 301.384 [291.414, 308.556] | 1192.863 [1092.758, 3724.408] | 3.866 [3.750, 12.358] | 503.569 [503.390, 503.810] | 1253.949 [1253.895, 3768.416] | 8.979 [8.319, 11.859] | 7.880 [7.711, 26.119] |
| descriptor400 / directory_episodes / task_owned_cached | 379.141 [363.553, 541.113] | 1395.454 [1383.010, 1400.237] | 3.693 [2.579, 3.804] | 504.762 [504.185, 756.771] | 1504.553 [1504.387, 1505.293] | 10.269 [3.590, 21.609] | 22.329 [8.610, 27.638] |
| descriptor400 / flat_episodes / cached_1 | 593.541 [593.442, 596.302] | 3140.043 [3122.201, 3168.286] | 5.290 [5.236, 5.339] | 754.034 [753.348, 754.211] | 3253.452 [3253.371, 3254.050] | 8.360 [5.670, 9.250] | 10.371 [8.390, 12.680] |
| descriptor400 / flat_episodes / cached_2 | 607.925 [581.105, 614.771] | 3335.952 [3216.903, 3461.370] | 5.694 [5.233, 5.741] | 755.606 [754.037, 756.573] | 3504.339 [3254.002, 3504.695] | 9.759 [8.521, 13.289] | 10.699 [9.740, 13.239] |
| descriptor400 / flat_episodes / cached_after_incremental | 610.005 [576.508, 669.437] | 3124.261 [3087.647, 3243.561] | 5.317 [4.612, 5.419] | 754.073 [753.804, 754.081] | 3254.586 [3252.922, 3255.123] | 11.069 [10.169, 12.689] | 9.649 [2.640, 13.119] |
| descriptor400 / flat_episodes / cold | 3121.573 [3074.719, 3180.914] | 6742.130 [6582.842, 6808.900] | 2.160 [2.069, 2.214] | 3254.196 [3253.820, 3255.476] | 6755.609 [6753.578, 7006.721] | 9.559 [9.531, 10.340] | 8.579 [2.580, 10.529] |
| descriptor400 / flat_episodes / force_probe | 2398.990 [2339.268, 2403.167] | 5947.722 [5714.935, 5998.521] | 2.500 [2.378, 2.543] | 2503.849 [2503.846, 2504.423] | 6003.356 [5753.717, 6254.435] | 8.609 [3.920, 8.811] | 12.559 [11.370, 16.589] |
| descriptor400 / flat_episodes / incremental | 658.406 [653.000, 662.212] | 3705.621 [3167.901, 11788.668] | 5.596 [4.811, 18.053] | 753.647 [753.381, 756.034] | 3753.536 [3253.444, 12021.300] | 8.210 [8.180, 9.629] | 7.840 [3.540, 8.170] |
| descriptor400 / flat_episodes / task_owned_cached | 998.130 [985.283, 1001.700] | 4243.945 [4125.431, 4300.386] | 4.237 [4.187, 4.308] | 1254.008 [1004.070, 1254.099] | 4255.148 [4254.134, 4504.046] | 8.680 [7.410, 22.729] | 17.701 [2.530, 20.100] |
| descriptor400 / flat_movies / cached_1 | 428.263 [407.248, 433.200] | 2561.515 [2487.044, 2646.515] | 5.913 [5.807, 6.499] | 503.553 [502.997, 503.558] | 2752.593 [2503.175, 2753.428] | 8.490 [8.050, 11.090] | 7.899 [7.470, 9.260] |
| descriptor400 / flat_movies / cached_2 | 419.021 [408.541, 514.041] | 2634.874 [2394.407, 2647.676] | 5.714 [5.126, 6.481] | 505.159 [504.169, 753.676] | 2753.845 [2504.130, 2755.260] | 11.719 [9.090, 11.989] | 15.149 [9.270, 21.180] |
| descriptor400 / flat_movies / cached_after_incremental | 415.672 [400.468, 430.268] | 2631.914 [2535.502, 6195.138] | 6.331 [6.117, 14.904] | 504.310 [503.988, 505.327] | 2753.450 [2753.328, 6260.117] | 11.190 [10.099, 22.418] | 9.950 [9.520, 12.349] |
| descriptor400 / flat_movies / cold | 2519.953 [2445.124, 2590.150] | 5587.855 [5581.873, 5603.155] | 2.217 [2.163, 2.283] | 2753.894 [2504.222, 2754.185] | 5754.426 [5753.720, 5754.489] | 10.950 [7.209, 11.040] | 9.240 [4.280, 13.329] |
| descriptor400 / flat_movies / force_probe | 1844.071 [1838.807, 1954.552] | 4819.138 [4643.065, 4917.666] | 2.621 [2.376, 2.667] | 2004.383 [2004.380, 2006.102] | 5004.846 [4754.246, 5005.704] | 8.050 [6.220, 21.410] | 8.860 [5.970, 9.250] |
| descriptor400 / flat_movies / incremental | 477.941 [476.660, 534.788] | 2662.827 [2606.629, 5832.759] | 5.586 [4.874, 12.204] | 504.261 [503.287, 754.386] | 2754.774 [2753.082, 6003.750] | 8.320 [2.480, 11.870] | 8.399 [7.971, 29.670] |
| descriptor400 / flat_movies / task_owned_cached | 799.585 [760.527, 834.526] | 3478.073 [3468.549, 3644.885] | 4.368 [4.338, 4.573] | 1005.233 [1005.006, 1005.415] | 3505.073 [3505.026, 3754.686] | 9.069 [8.720, 11.059] | 9.059 [8.659, 9.330] |
| real160 / mixed_real_probe / cold | 3386.596 [3377.778, 3473.082] | 7180.506 [7083.098, 7455.375] | 2.126 [2.092, 2.147] | 3505.879 [3505.182, 3506.242] | 7256.694 [7256.422, 7506.223] | 3.340 [2.529, 3.920] | 7.349 [4.219, 11.190] |
| real160 / mixed_real_probe / force | 2780.583 [2699.820, 2793.493] | 6544.028 [6377.902, 6609.132] | 2.353 [2.283, 2.448] | 3005.070 [2754.248, 3005.639] | 6753.953 [6504.204, 6753.998] | 10.129 [9.839, 13.289] | 11.039 [9.520, 11.659] |
| real160 / mixed_real_probe / warm | 920.124 [898.205, 977.307] | 3998.797 [3711.249, 4055.005] | 4.149 [4.132, 4.346] | 1004.143 [1003.991, 1004.654] | 4254.099 [3754.697, 4254.319] | 5.160 [3.570, 8.150] | 10.790 [7.880, 16.750] |

### SQL and authority classification

Each B / C entry is the median count; exact samples, ranges, rollback and manual/task classifications remain in [structured measurement record](media-performance-remeasure-20261003.json).

| Profile / library / phase | Raw SQL B / C | Raw BEGIN B / C | Raw COMMIT B / C | Authority SQL B / C | Authority BEGIN B / C | Authority COMMIT B / C |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 696 / 3158 | 83 / 573 | 83 / 573 | 10 / 2460 | 2 / 492 | 2 / 492 |
| descriptor400 / directory_episodes / cached_2 | 696 / 3158 | 83 / 573 | 83 / 573 | 10 / 2460 | 2 / 492 | 2 / 492 |
| descriptor400 / directory_episodes / cached_after_incremental | 696 / 3158 | 83 / 573 | 83 / 573 | 10 / 2460 | 2 / 492 | 2 / 492 |
| descriptor400 / directory_episodes / cold | 2268 / 5498 | 179 / 813 | 179 / 813 | 10 / 3180 | 2 / 636 | 2 / 636 |
| descriptor400 / directory_episodes / force_probe | 1992 / 5174 | 131 / 765 | 131 / 765 | 10 / 3180 | 2 / 636 | 2 / 636 |
| descriptor400 / directory_episodes / incremental | 776 / 3254 | 86 / 579 | 86 / 579 | 10 / 2475 | 2 / 495 | 2 / 495 |
| descriptor400 / directory_episodes / task_owned_cached | 964 / 4896 | 83 / 573 | 83 / 573 | 16 / 3936 | 2 / 492 | 2 / 492 |
| descriptor400 / flat_episodes / cached_1 | 1734 / 9464 | 221 / 1767 | 221 / 1767 | 10 / 7740 | 2 / 1548 | 2 / 1548 |
| descriptor400 / flat_episodes / cached_2 | 1734 / 9464 | 221 / 1767 | 221 / 1767 | 10 / 7740 | 2 / 1548 | 2 / 1548 |
| descriptor400 / flat_episodes / cached_after_incremental | 1734 / 9464 | 221 / 1767 | 221 / 1767 | 10 / 7740 | 2 / 1548 | 2 / 1548 |
| descriptor400 / flat_episodes / cold | 7932 / 18734 | 605 / 2727 | 605 / 2727 | 10 / 10620 | 2 / 2124 | 2 / 2124 |
| descriptor400 / flat_episodes / force_probe | 6918 / 17528 | 413 / 2535 | 413 / 2535 | 10 / 10620 | 2 / 2124 | 2 / 2124 |
| descriptor400 / flat_episodes / incremental | 1814 / 9560 | 224 / 1773 | 224 / 1773 | 10 / 7755 | 2 / 1551 | 2 / 1551 |
| descriptor400 / flat_episodes / task_owned_cached | 2722 / 15090 | 221 / 1767 | 221 / 1767 | 16 / 12384 | 2 / 1548 | 2 / 1548 |
| descriptor400 / flat_movies / cached_1 | 1205 / 7665 | 171 / 1463 | 171 / 1463 | 10 / 6470 | 2 / 1294 | 2 / 1294 |
| descriptor400 / flat_movies / cached_2 | 1205 / 7665 | 171 / 1463 | 171 / 1463 | 10 / 6470 | 2 / 1294 | 2 / 1294 |
| descriptor400 / flat_movies / cached_after_incremental | 1205 / 7665 | 171 / 1463 | 171 / 1463 | 10 / 6470 | 2 / 1294 | 2 / 1294 |
| descriptor400 / flat_movies / cold | 6325 / 15345 | 491 / 2263 | 491 / 2263 | 10 / 8870 | 2 / 1774 | 2 / 1774 |
| descriptor400 / flat_movies / force_probe | 5525 / 14385 | 331 / 2103 | 331 / 2103 | 10 / 8870 | 2 / 1774 | 2 / 1774 |
| descriptor400 / flat_movies / incremental | 1283 / 7759 | 174 / 1469 | 174 / 1469 | 10 / 6485 | 2 / 1297 | 2 / 1297 |
| descriptor400 / flat_movies / task_owned_cached | 2033 / 12369 | 171 / 1463 | 171 / 1463 | 16 / 10352 | 2 / 1294 | 2 / 1294 |
| real160 / mixed_real_probe / cold | 8062 / 22991 | 559 / 2401 | 559 / 2401 | 16 / 14752 | 2 / 1844 | 2 / 1844 |
| real160 / mixed_real_probe / force | 7219 / 21988 | 399 / 2241 | 399 / 2241 | 16 / 14752 | 2 / 1844 | 2 / 1844 |
| real160 / mixed_real_probe / warm | 2259 / 13156 | 207 / 1569 | 207 / 1569 | 16 / 10912 | 2 / 1364 | 2 / 1364 |

In flat_movies cached_1, each of all three pairs has exactly 6460 additional raw SQL statements, 1292 additional BEGINs, and 1292 additional COMMITs, with identical deltas in the strict authority subset. Thus the additional work counts in this one case are fully classified as authority work. This observation is not generalized to other cases, does not assign elapsed/allocation causation, and does not isolate transaction milliseconds.

### Process allocation and pool deltas

Allocation is total allocated bytes in the declared process window, not peak memory. Wait columns are empty-acquire wait, including resource construction.

| Profile / library / phase | B allocated MiB | C allocated MiB | Paired C/B | B pool acquires | C pool acquires | B empty wait ms | C empty wait ms |
| --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 9.183 [9.136, 9.209] | 29.830 [29.694, 29.975] | 3.264 [3.224, 3.265] | 69 [69, 69] | 561 [561, 561] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_2 | 9.244 [9.178, 9.260] | 29.794 [29.699, 29.963] | 3.236 [3.223, 3.236] | 69 [69, 69] | 561 [561, 561] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_after_incremental | 9.183 [9.150, 9.322] | 29.825 [29.599, 29.960] | 3.235 [3.214, 3.248] | 69 [69, 69] | 561 [561, 561] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cold | 53.484 [53.437, 53.703] | 104.071 [103.920, 104.112] | 1.943 [1.938, 1.948] | 119 [119, 119] | 757 [756, 758] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / force_probe | 49.138 [49.131, 49.353] | 99.568 [99.232, 99.791] | 2.022 [2.020, 2.026] | 70 [70, 70] | 708 [708, 708] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / incremental | 12.535 [12.515, 12.601] | 33.947 [33.784, 34.159] | 2.708 [2.681, 2.729] | 71 [71, 71] | 566 [566, 576] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / task_owned_cached | 10.077 [9.904, 10.229] | 33.127 [33.057, 33.233] | 3.280 [3.249, 3.345] | 69 [69, 70] | 562 [562, 562] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_1 | 17.902 [17.900, 17.999] | 94.349 [94.194, 94.542] | 5.262 [5.253, 5.270] | 202 [202, 202] | 1757 [1757, 1757] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_2 | 17.832 [17.783, 17.966] | 94.482 [94.146, 94.530] | 5.294 [5.259, 5.301] | 202 [202, 202] | 1758 [1757, 1758] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_after_incremental | 17.937 [17.870, 18.007] | 94.154 [94.010, 94.253] | 5.255 [5.229, 5.261] | 202 [202, 202] | 1757 [1757, 1757] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cold | 192.812 [192.701, 192.940] | 389.563 [389.386, 390.008] | 2.021 [2.020, 2.021] | 404 [404, 404] | 2539 [2539, 2540] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / force_probe | 176.147 [175.563, 176.398] | 371.514 [371.453, 371.972] | 2.109 [2.106, 2.119] | 209 [209, 209] | 2344 [2343, 2345] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / incremental | 21.393 [21.335, 21.454] | 98.577 [98.449, 98.722] | 4.602 [4.602, 4.620] | 204 [204, 204] | 1764 [1762, 1797] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / task_owned_cached | 21.105 [21.068, 21.254] | 105.674 [105.631, 105.970] | 5.005 [4.972, 5.030] | 204 [203, 204] | 1761 [1761, 1762] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_1 | 12.504 [12.459, 12.606] | 76.189 [76.039, 76.356] | 6.081 [6.044, 6.129] | 170 [170, 170] | 1470 [1469, 1470] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_2 | 12.466 [12.427, 12.594] | 76.101 [75.825, 76.283] | 6.105 [6.021, 6.139] | 170 [170, 171] | 1470 [1469, 1470] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_after_incremental | 12.477 [12.455, 12.500] | 76.184 [76.084, 76.355] | 6.098 [6.095, 6.131] | 170 [170, 170] | 1470 [1470, 1484] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cold | 159.813 [159.305, 159.935] | 323.809 [323.684, 323.851] | 2.025 [2.025, 2.033] | 339 [338, 339] | 2122 [2122, 2122] | 2.965 [2.832, 3.340] | 5.569 [4.756, 5.726] |
| descriptor400 / flat_movies / force_probe | 144.974 [144.751, 145.188] | 307.449 [307.444, 307.868] | 2.121 [2.120, 2.124] | 176 [176, 176] | 1959 [1958, 1959] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / incremental | 15.771 [15.608, 15.849] | 80.246 [80.144, 80.422] | 5.099 [5.063, 5.135] | 172 [172, 173] | 1475 [1475, 1488] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / task_owned_cached | 15.226 [15.111, 15.295] | 85.385 [85.300, 86.024] | 5.602 [5.583, 5.693] | 172 [172, 172] | 1473 [1473, 1474] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| real160 / mixed_real_probe / cold | 293.283 [293.139, 293.454] | 469.178 [468.726, 469.274] | 1.600 [1.597, 1.601] | 375 [375, 375] | 2231 [2231, 2232] | 3.591 [1.527, 3.627] | 5.195 [2.988, 5.236] |
| real160 / mixed_real_probe / force | 278.386 [278.094, 278.607] | 453.028 [452.875, 453.332] | 1.627 [1.626, 1.630] | 213 [212, 213] | 2069 [2068, 2069] | 0.000 [0.000, 0.000] | 0.000 [0.000, 2.021] |
| real160 / mixed_real_probe / warm | 24.931 [24.819, 24.957] | 97.775 [97.679, 97.798] | 3.923 [3.914, 3.939] | 205 [205, 205] | 1579 [1577, 1579] | 0.000 [0.000, 2.409] | 0.000 [0.000, 0.000] |

### Real-probe child resource observations

| Phase | B cohorts | C cohorts | B child CPU ms | C child CPU ms | B in/out blocks | C in/out blocks |
| --- | --- | --- | --- | --- | --- | --- |
| cold | 2 [2, 2] | 2 [2, 2] | 1149.956 [1148.482, 1151.968] | 1196.377 [1190.208, 1204.846] | 0 [0, 0] / 0 [0, 0] | 0 [0, 0] / 0 [0, 0] |
| force | 2 [2, 2] | 2 [2, 2] | 1102.281 [1091.740, 1104.849] | 1182.378 [1159.899, 1188.037] | 0 [0, 0] / 0 [0, 0] | 0 [0, 0] / 0 [0, 0] |
| warm | 0 [0, 0] | 0 [0, 0] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0 [0, 0] / 0 [0, 0] | 0 [0, 0] / 0 [0, 0] |

### Profile-level process observations

These process values include fixture and observer work; wall additionally includes compilation. They must not substitute for individual phase job times.

| Profile | B package seconds | C package seconds | B wall seconds | C wall seconds | B Store.Close ms | C Store.Close ms |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 | 22.498 [22.476, 22.753] | 67.066 [66.773, 84.793] | 23.124 [23.069, 23.412] | 67.677 [67.394, 85.480] | 0.314 [0.299, 0.332] | 0.305 [0.234, 0.345] |
| real160 | 7.818 [7.547, 7.820] | 18.550 [18.054, 18.587] | 8.445 [8.194, 8.445] | 19.153 [18.619, 19.234] | 0.420 [0.377, 0.453] | 0.338 [0.320, 0.596] |

## Formal cached HTTP results

Values are medians [minimum, maximum] across three wave percentiles, in milliseconds. Each wave has 256 raw request durations; samples are not pooled across waves.

| Credentials / callers | B p50 ms | C p50 ms | C/B p50 | B p95 ms | C p95 ms | C/B p95 | B p99 ms | C p99 ms | C/B p99 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| normal-login / 1 | 9.470 [9.447, 18.127] | 9.658 [9.625, 10.111] | 1.016 [0.533, 1.070] | 12.672 [11.805, 30.863] | 12.469 [12.179, 13.232] | 0.961 [0.404, 1.121] | 14.929 [13.103, 41.121] | 14.864 [14.738, 15.100] | 0.987 [0.367, 1.134] |
| normal-login / 8 | 15.506 [15.028, 39.817] | 15.723 [15.684, 16.288] | 1.011 [0.409, 1.046] | 18.001 [17.536, 74.291] | 18.555 [18.410, 20.838] | 1.023 [0.280, 1.058] | 22.575 [18.605, 97.654] | 21.321 [21.260, 26.986] | 0.944 [0.276, 1.143] |
| normal-login / 32 | 64.490 [62.664, 124.543] | 62.314 [60.741, 63.842] | 0.990 [0.488, 0.994] | 92.679 [76.832, 211.610] | 67.812 [64.123, 70.986] | 0.766 [0.303, 0.883] | 98.552 [80.149, 231.605] | 71.943 [67.489, 72.848] | 0.730 [0.291, 0.909] |
| application-key / 1 | 12.063 [12.009, 12.182] | 12.119 [12.091, 12.129] | 1.005 [0.996, 1.007] | 14.237 [14.151, 15.099] | 14.994 [14.002, 15.709] | 1.040 [0.983, 1.060] | 16.096 [15.531, 19.656] | 17.444 [17.158, 18.959] | 1.084 [0.965, 1.105] |
| application-key / 8 | 18.968 [18.893, 20.461] | 19.115 [18.639, 19.349] | 0.987 [0.946, 1.008] | 21.585 [21.314, 24.667] | 21.604 [21.540, 24.536] | 1.001 [0.995, 1.011] | 22.864 [22.240, 28.594] | 23.690 [22.775, 36.682] | 1.065 [0.996, 1.283] |
| application-key / 32 | 63.955 [63.314, 66.225] | 67.009 [63.398, 68.563] | 1.058 [0.957, 1.072] | 81.341 [74.567, 81.360] | 78.552 [72.476, 83.924] | 0.966 [0.891, 1.125] | 92.650 [85.403, 94.813] | 84.654 [84.286, 87.508] | 0.910 [0.893, 1.025] |

### Cached same-scope Stop

Values are median [minimum, maximum], n=3 per case; native reap is not measured by these completed producers.

| Credentials / callers | B response ms | C response ms | B handler drain upper bound ms | C handler drain upper bound ms | B scope completion upper bound ms | C scope completion upper bound ms |
| --- | --- | --- | --- | --- | --- | --- |
| normal-login / 1 | 16.324 [14.683, 17.263] | 15.678 [15.568, 15.756] | 16.354 [14.711, 17.294] | 15.699 [15.617, 15.779] | 18.061 [16.303, 18.902] | 17.449 [17.195, 18.413] |
| normal-login / 8 | 13.961 [13.150, 18.383] | 13.732 [13.596, 13.969] | 14.126 [13.519, 18.565] | 13.774 [13.635, 13.999] | 15.680 [15.015, 20.299] | 15.303 [14.980, 15.349] |
| normal-login / 32 | 15.302 [14.215, 16.217] | 12.739 [11.618, 16.535] | 26.627 [24.474, 27.171] | 12.761 [11.656, 16.568] | 28.230 [25.712, 28.638] | 17.656 [14.054, 18.174] |
| application-key / 1 | 11.869 [11.794, 12.178] | 13.051 [12.504, 14.101] | 11.896 [11.819, 12.206] | 13.067 [12.522, 14.118] | 16.462 [16.279, 16.902] | 17.524 [16.891, 19.722] |
| application-key / 8 | 14.457 [14.306, 16.262] | 16.864 [15.057, 23.406] | 14.539 [14.341, 16.303] | 16.896 [15.083, 23.432] | 20.918 [19.410, 21.257] | 21.811 [20.445, 28.073] |
| application-key / 32 | 18.439 [18.202, 19.935] | 18.810 [15.994, 19.320] | 24.104 [22.848, 24.875] | 18.831 [16.023, 19.351] | 30.262 [27.403, 30.491] | 26.130 [24.313, 28.177] |

### HTTP SQL, allocation, pools and admission

Each B / C entry below is median [minimum, maximum] across three waves. Allocation is a process delta in the declared window. Data/Control empty wait includes acquisition/resource construction and is not total server contention.

| Credentials / callers | SQL B / C | BEGIN B / C | COMMIT B / C | Allocated MiB B / C | Data empty wait ms B / C | Control empty wait ms B / C |
| --- | --- | --- | --- | --- | --- | --- |
| normal-login / 1 | 6400 [6400, 6400] / 6400 [6400, 6400] | 512 [512, 512] / 512 [512, 512] | 512 [512, 512] / 512 [512, 512] | 205.255 [205.124, 205.517] / 208.600 [208.397, 208.711] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] |
| normal-login / 8 | 6450 [6400, 8370] / 6420 [6400, 6450] | 517 [512, 709] / 514 [512, 517] | 517 [512, 709] / 514 [512, 517] | 205.758 [204.628, 252.922] / 208.313 [207.979, 209.266] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] |
| normal-login / 32 | 6540 [6440, 7460] / 6430 [6420, 6440] | 526 [516, 618] / 515 [514, 516] | 526 [516, 618] / 515 [514, 516] | 208.131 [205.752, 230.463] / 208.927 [208.721, 209.161] | 22.501 [21.609, 24.303] / 23.508 [20.070, 42.547] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] |
| application-key / 1 | 10496 [10496, 10496] / 10496 [10496, 10496] | 1024 [1024, 1024] / 1024 [1024, 1024] | 1024 [1024, 1024] / 1024 [1024, 1024] | 145.549 [145.419, 145.597] / 149.192 [148.888, 149.201] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] |
| application-key / 8 | 10507 [10496, 10518] / 10507 [10507, 10529] | 1025 [1024, 1026] / 1025 [1025, 1027] | 1025 [1024, 1026] / 1025 [1025, 1027] | 145.200 [145.108, 145.403] / 148.681 [148.493, 148.810] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] |
| application-key / 32 | 10518 [10507, 10639] / 10529 [10507, 10573] | 1026 [1025, 1037] / 1027 [1025, 1031] | 1026 [1025, 1037] / 1027 [1025, 1031] | 145.602 [145.468, 147.225] / 149.415 [148.941, 149.802] | 972.112 [935.096, 1014.216] / 923.197 [912.030, 1033.767] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] |

Source-admission v5 entries are aggregated stage sums per wave. Their spans overlap and must not be added or treated as isolated SQL/filesystem execution. Extra successful reauthorization is permitted and counted; authority stage counts are not constrained to exactly 2N.

| Credentials / callers | Authorizations B / C | Reauthorizations B / C | Authorization wait ms B / C | Authorization span ms B / C | I/O queue ms B / C | I/O held ms B / C |
| --- | --- | --- | --- | --- | --- | --- |
| normal-login / 1 | 512 [512, 513] / 512 [512, 512] | 0 [0, 0] / 0 [0, 0] | 0.250 [0.249, 0.261] / 0.259 [0.235, 0.288] | 1898.389 [1875.924, 4418.076] / 1953.350 [1950.203, 2020.345] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] | 50.943 [47.521, 68.253] / 61.724 [59.357, 66.526] |
| normal-login / 8 | 517 [512, 709] / 514 [512, 517] | 5 [0, 197] / 2 [0, 5] | 1.198 [1.142, 1.748] / 1.220 [1.217, 1.246] | 2920.315 [2829.593, 9051.301] / 2969.307 [2949.817, 3160.027] | 0.883 [0.000, 21.350] / 0.181 [0.000, 0.522] | 72.844 [69.179, 665.608] / 93.169 [88.980, 93.411] |
| normal-login / 32 | 526 [516, 618] / 515 [514, 516] | 14 [4, 106] / 3 [2, 4] | 1.396 [1.291, 1.837] / 1.564 [1.382, 1.692] | 3350.243 [3206.299, 6607.883] / 3079.146 [2964.126, 3121.993] | 2.169 [0.549, 20.307] / 0.405 [0.227, 0.529] | 100.149 [73.294, 423.667] / 93.984 [89.688, 95.274] |
| application-key / 1 | 513 [513, 513] / 513 [513, 513] | 0 [0, 0] / 0 [0, 0] | 0.267 [0.260, 0.275] / 0.270 [0.251, 0.382] | 1736.961 [1724.332, 1744.953] / 1756.759 [1736.670, 1774.054] | 0.000 [0.000, 0.000] / 0.000 [0.000, 0.000] | 24.397 [24.031, 24.435] / 37.779 [36.907, 37.788] |
| application-key / 8 | 513 [512, 514] / 513 [513, 515] | 1 [0, 2] / 1 [1, 3] | 1.024 [0.954, 1.122] / 1.111 [1.106, 1.147] | 2688.775 [2636.517, 2990.875] / 2650.597 [2612.924, 2741.812] | 0.052 [0.000, 0.131] / 0.085 [0.057, 0.255] | 34.769 [33.663, 37.153] / 55.778 [55.501, 57.145] |
| application-key / 32 | 514 [513, 525] / 516 [513, 519] | 2 [1, 13] / 3 [1, 7] | 1.383 [1.292, 1.452] / 1.310 [1.281, 1.430] | 3149.093 [3061.900, 3189.897] / 3253.530 [2977.601, 3325.210] | 0.155 [0.084, 1.152] / 0.259 [0.106, 0.546] | 40.635 [38.074, 51.435] / 60.831 [57.373, 61.488] |

All raw duration arrays, recomputed per-wave percentiles, SQL/transaction samples, allocation/pool samples, the full v5 file_open/publication/ack/handoff/cleanup stages, fresh successful authority counts, Stop burst outcomes and exact resource facts remain in [structured measurement record](media-performance-remeasure-20261003.json).

## Formal running-encoder Stop

Values are median [minimum, maximum], n=3; response and observations are separate metrics. Every retirement row requires pinned pidfd exit, actual stat ENOENT and original group ESRCH.

| Metric | B ms | C ms |
| --- | --- | --- |
| stop_response_ns | 15.302 [14.878, 15.672] | 16.122 [14.876, 16.491] |
| stop_response_observed_upper_bound_ns | 15.333 [14.914, 15.731] | 16.164 [14.908, 16.523] |
| retirement.exit_observed_upper_bound_ns | 494.932 [379.170, 515.510] | 240.129 [217.326, 526.941] |
| retirement.reap_group_observed_upper_bound_ns | 494.951 [379.181, 515.523] | 241.216 [221.239, 530.316] |
| consumer_join_observed_upper_bound_ns | 494.965 [379.187, 515.533] | 241.221 [221.248, 530.345] |
| store_and_dependency_join_ns | 26.574 [21.075, 1014.459] | 16.203 [14.857, 24.778] |
| stop_to_final_close_observed_upper_bound_ns | 521.879 [400.619, 1531.088] | 266.409 [236.620, 547.045] |

## Retained issues

No evidence-quality issue was found in the exported formal or pilot matrices.

## Statistical and scope limits

This is a bounded source comparison on the declared fixture and loopback environment. It does not qualify existing clients, A/V correctness, GPU/native-hard behavior, physical cold-storage, full mixed-service capacity, or larger corpora. Pairing reduces order ambiguity but does not establish a causal attribution for latency changes. Three pairs establish observed central values and ranges, not stable tails or broad capacity.

Raw run metadata, source/configuration identities, recomputed HTTP wave percentiles, numeric sample arrays and paired ratios are in [structured measurement record](media-performance-remeasure-20261003.json). Pilot marker records are preserved separately and never enter statistics.

## Environment closure

The exported owned-environment closure receipt reports closed. The designated remote owner confirmed only saved private PostgreSQL PID3006542/start60394174 was smart-stopped through its pinned pidfd, with no other clients and with its process family, socket, PID and lock files absent. Owned mount366 was unmounted without force or lazy mode; device7:0 had no namespace mounts before exact loop0 detachment, and the backing-image association count is zero. The bounded visible audit reports zero task references, mounts and errors. Shared PostgreSQL PID898/start689, the three protected Docker identities and the old closed task root remain unchanged.

Source trees, test overlays, frozen source/fixture images, PostgreSQL data, caches and logs are retained. This closure receipt is retained in [structured measurement record](media-performance-remeasure-20261003.json) and the evidence seal. Statistical completion and environment closure are separate contracts.
