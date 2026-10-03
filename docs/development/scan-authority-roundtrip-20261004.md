# Scan authority round-trip optimization, batch 1 - October 4, 2026

## Mixed performance outcome

Batch 1 has a mixed performance outcome. In cached_after_incremental,
flat_episodes has same-block C/B median 1.351 [0.890, 1.678] (+35.1%), and
directory_episodes has median 1.044 [0.922, 3.329] (+4.4%); each is slower in
two of three matched blocks. These regressions remain explicit. The accepted
scope is a bounded batch 1 delivery, not uniform improvement.

Descriptor cold and task_owned_cached improve in all three blocks for each of
the three layouts. Their C/B medians are respectively 0.892-0.938 and
0.838-0.861. Real force also improves in all three blocks, with median C/B
0.897. These results do not establish restoration of historical H performance.
Three blocks establish observed central values and ranges, not stable tails.
All adverse samples and complete same-block C/B and C/H comparisons remain
in the full accepted analysis and structured record.

## Delivered source and accepted evidence

The frozen nine-path source commit is
`616766abbfc386e1167b61caca2fd6d1cc852eb2`, an immediate child of baseline
`948373ac64af8646ac6288e9294ce3c7f2882df6`. Every committed source-path byte
identity matches the final freeze. The selected batch 1 source is accepted with
the mixed performance limitations above. Correctness, builds, pilots, separate
count diagnostics, formal timing and exact private-resource closure are
complete. Evidence status is `qualified_and_closed`; it does not imply uniform
performance improvement or completion of the other performance tasks.

## Measured source and delivered source

| Role | Git base | Measurement-ready archive SHA-256 |
| --- | --- | --- |
| H | `912354f2a48b5043a8824246f2fa011f8f34bbf1` | `709f2c2b88f429c16f8d8d88e156078930088c43a77e46c20c19c02a4dd3cac7` |
| B | `948373ac64af8646ac6288e9294ce3c7f2882df6` | `6cc06c4da1e9491ea6ec4a5cb0f2cbdda262cc3fd623a3c7b37379ae146b81b7` |
| C | `948373ac64af8646ac6288e9294ce3c7f2882df6` | `586b4151509502a2008d99753f1dfc9b7f75de52a409d2b9e917b7e077e1e0e8` |

H and B are their Git archives plus the common measurement-driver overlay. C
is B's Git base plus the nine-path candidate overlay and common drivers. The
original H/B Git archive hashes are
`c54573195606653dea488cd26a48466d1aae785270118e96b450a5e472c4844b` and
`5ca6a4e89c92bd9000d08d6d21f46631cb324242c8cc768492f4254411c0fc81`.
The measured archives and prepared overlays remain distinct from the later
source commit. `source-freeze.json`, SHA-256
`d549825fd5c26d64b6cd2fa635c2a34ec306a77405fd6246ca5e749b1f7ef128`, binds
all source/archive/member and shared-driver identities. The later source
commit matches all nine frozen candidate working-tree changed-file hashes
without relabeling the measured candidate as a commit.

The actual prepared B/C production-and-test delta contains six paths:

- `internal/library/primary_scan_read.go`
- `internal/library/primary_scan_authority.go`
- `internal/library/primary_scan_authority_integration_test.go`
- `internal/library/primary_scan_routing_integration_test.go`
- `internal/library/primary_sidecar_authority_queue_integration_test.go`
- `internal/library/scan_cached_checkpoint_integration_test.go`

Three updated library measurement drivers bring the source commit to nine
paths. Four common driver bytes are identical across H/B/C:

| Shared driver | SHA-256 |
| --- | --- |
| `internal/library/scan_performance_integration_test.go` | `95da8a0ac7c3a5aa7aa95f6ac71f7eadb10399e6ff055eb0b2796c88941e2dec` |
| `internal/library/scan_performance_measurement_integration_test.go` | `b753e4ae946544735abb6d5db5ee3472f7294847c3035d714eb63dbc2af386b7` |
| `internal/library/scan_probe_pipeline_performance_integration_test.go` | `6c85de1b388c4aa0e63b3f14e0ef6eec5a144223cdcd09d3c7d189bc5586a0b7` |
| `internal/server/http_get_stop_remeasure_integration_test.go` | `e421241ee9079a8a139f5cd7355c18c64c7122dfd03540d767a8af09ae46f2a0` |

## Selected implementation and measurement boundaries

Batch 1 implements a success-only one-request normal scan-authority path using
dependent materialized CTEs. Initial snapshots route to rows; acceptance uses
locked tuples with the existing lock strength and order. The task-owned chain
is run, child, job, then root. Manual scans cannot borrow task-owned relations.
Complete task/child/parent and root binding fields are checked, and full query
consumption and implicit transaction completion precede returned authority or
filesystem work.

Missing, changed or malformed authority releases the first query's connection
and locks before the unchanged original transaction handles terminal/history/
error semantics. Post-grant freshness, final owned publication proof, Busy
rollback and retry, committed prefixes, quotas and actual retirement remain.
Real PostgreSQL row-lock wait/mutation guards and committed-change queue cases
qualify these selected correctness boundaries; no broader product behavior is
claimed by this increment.

All four correctness cases, four builds, six pilots, six count diagnostics and
eighteen formal timing processes completed. Official timing uses TRACE0 and
has no SQL tracer: SQL/AUTH counters are unavailable, not zero. Separate n=1
traced flat_movies/cached_1 diagnostics reduce B/C client SQL from 4455 to 1859,
while confirmed AUTH committed transactions remain 652/652. C records three
explicit and 649 implicit AUTH commits. Raw BEGIN/COMMIT count client commands;
an implicit autocommit SELECT still has a database transaction. Count evidence
does not assign untraced timing changes to transaction milliseconds or causal
allocation differences. Zero-row, failure and unconfirmed completion records
remain separately retained.

Timing uses the complete 400-descriptor and task-owned 160-real profiles with
fixed Go/FFmpeg/PostgreSQL, CGO_ENABLED=1 and GOMAXPROCS=4, without race or
profiler instrumentation. Formal source blocks are H-B-C, C-B-H and B-H-C;
C/B and C/H ratios come from matched blocks. Pilot data qualifies only the
instrumentation/fixtures and is excluded from formal statistics. There is no
HTTP performance matrix in this batch.

## Evidence and exact closure

The accepted final evidence receipt is
`D:/Code/goby/.artifacts/scan-authority-roundtrip-20261004/final-evidence-receipt.json`,
SHA-256 `ccff91d80b2ca0700d7168e23e912bf4a6cf2cc75caa9ad33e4ee0e06303af76`.
The exact closure receipt is
`D:/Code/goby/.artifacts/scan-authority-roundtrip-20261004/closure-receipt.json`,
SHA-256 `99a33b6acdcef09f3c06d755d6713e4418d86df0cf5940062fdeeb0d0fb804dd`.
Final evidence qualification is `qualified_and_closed`. Evidence qualification
and exact closure are separate from the mixed performance outcome. Source,
data, images, caches and raw/failure evidence remain retained; no new local
runtime probe establishes those exported closure facts.

The [structured record](scan-authority-roundtrip-20261004.json) is the complete
accepted analysis `summary.json`, including every sample, diagnostic, adverse
observation, source/configuration identity, archive/driver manifest, statistics
and separate receipt. Only CRLF line endings are normalized to LF for the
repository's text policy; all values and records are unchanged. Published JSON
SHA-256 is `8c0a0b109b7ee3c1e5b547c2656787159bb90c681c5d4f55ac2b80c50d00fc40`. The external original retains SHA-256
`2798f075a4fcd8b790dd619506c0501793e4391437c69632059e3f862a2c9d56`.

The original accepted analysis Markdown SHA-256 is
`76e1c3f67f2b7ea2bcd2b6331a54674c9bba6f97c4ab4815ea484c9859e593a3`;
the final static parser SHA-256 is
`d9762781108bd155208895fd2c30b20c418a7021dd3ad2cbbe3522ed5d81b747`.
The independent final seal binds 143 actual exported evidence files with zero
missing or mismatched hashes, and all eighteen formal aggregate records match
their immutable receipts and raw logs. This seal binds the original evidence
inputs, not the hash of this publication wrapper. The old optional preparation
receipt is documented as not applicable rather than inferred as missing
required evidence. Full accepted metric/source/protected-resource facts are
retained below and in the structured record.

## Remaining task decisions

This batch does not select phase merging, the known raw local-NFO boundary,
directory-index or reconciliation redesign, existing-client A/V, Analysis
PART2, GPU/native-hard qualification, whole-service mixed capacity, images or
deployment. The selected bounded batch does not resolve those tasks or complete
the whole performance program. Feature defaults and accepted Docker delivery
checkpoints retain their prior scope.

## Complete accepted analysis

Generated only from completed exported static evidence. No product, test, build, runtime probe, SSH operation, or measurement is executed by this analysis.

Final qualification: `qualified_and_closed`.

Runtime, source, configuration, sealed-evidence and exact owned-resource closure gates are qualified. This quality status is separate from the performance outcome.

The performance result supports a bounded batch 1 delivery, not uniform improvement across phases. Descriptor cold and task_owned_cached job times improve in all three blocks for each layout; real force also improves in all three blocks. The observed regressions remain explicit: flat_episodes/cached_after_incremental has same-block C/B median 1.351 [0.890, 1.678], with two of three blocks slower; directory_episodes/cached_after_incremental has median 1.044 [0.922, 3.329], also with two of three blocks slower. All other adverse samples and full ranges remain retained. These observations are not dismissed as noise, and n=3 does not establish stable tails.

The separate n=1 traced flat_movies/cached_1 diagnostics show B/C client SQL 4455/1859, a reduction of 2596 commands. Confirmed AUTH committed transactions remain 652/652: C has 3 explicit and 649 implicit AUTH commits. Raw BEGIN/COMMIT dropping from 821 to 172 does not mean the implicit transactions disappeared. AUTH attribution is a statement-family classification, not an exact helper or fallback profile; single-row results precede Go validation. Diagnostic counts do not assign the observed untraced timing changes to transaction milliseconds or a specific query-planning cause.

## Frozen source and shared measurement identities

H is historical, B is the current committed baseline, and C is the frozen candidate overlay. All sources use the same v2 measurement drivers. A candidate base Git head is not a candidate commit identity. Original Git archives and complete measurement-ready archives are separate byte identities.

Source freeze: `d549825fd5c26d64b6cd2fa635c2a34ec306a77405fd6246ca5e749b1f7ef128`; source binding status: `confirmed`.

| Source | Role | Git base | Original archive SHA256 | Measurement-ready SHA256 | Staged manifest SHA256 |
| --- | --- | --- | --- | --- | --- |
| sourceH | historical | 912354f2a48b5043a8824246f2fa011f8f34bbf1 | c54573195606653dea488cd26a48466d1aae785270118e96b450a5e472c4844b | 709f2c2b88f429c16f8d8d88e156078930088c43a77e46c20c19c02a4dd3cac7 | b80f8ab87a912fa0bbbb756857c0f0a0a37fd5097ffd190ef22e375d25a7a565 |
| sourceB | baseline | 948373ac64af8646ac6288e9294ce3c7f2882df6 | 5ca6a4e89c92bd9000d08d6d21f46631cb324242c8cc768492f4254411c0fc81 | 6cc06c4da1e9491ea6ec4a5cb0f2cbdda262cc3fd623a3c7b37379ae146b81b7 | 117227442b839d97d16af70fbcb2e91b90694883129121fb277cf1ba534354ac |
| sourceC | candidate | 948373ac64af8646ac6288e9294ce3c7f2882df6 | 5ca6a4e89c92bd9000d08d6d21f46631cb324242c8cc768492f4254411c0fc81 | 586b4151509502a2008d99753f1dfc9b7f75de52a409d2b9e917b7e077e1e0e8 | f09bd1941b551ab62a8da6dba71c26011ca5cfc763e53d638b38140453a9ab1e |

| Shared driver | SHA256 |
| --- | --- |
| internal/library/scan_performance_integration_test.go | 95da8a0ac7c3a5aa7aa95f6ac71f7eadb10399e6ff055eb0b2796c88941e2dec |
| internal/library/scan_performance_measurement_integration_test.go | b753e4ae946544735abb6d5db5ee3472f7294847c3035d714eb63dbc2af386b7 |
| internal/library/scan_probe_pipeline_performance_integration_test.go | 6c85de1b388c4aa0e63b3f14e0ef6eec5a144223cdcd09d3c7d189bc5586a0b7 |
| internal/server/http_get_stop_remeasure_integration_test.go | e421241ee9079a8a139f5cd7355c18c64c7122dfd03540d767a8af09ae46f2a0 |

The actual original and ready archive regular-member hashes are compared without extraction. H/B ready changes are confined to shared drivers; C ready bytes bind the exact candidate overlay. Each complete ready archive must equal its staged manifest, and B/C staged production/test changes must equal the six frozen non-driver paths.

## Independent evidence contracts

| Contract | Mode / scope | Completed exported processes | Expected | Status |
| --- | --- | --- | --- | --- |
| Scan | pilot | 6 | 6 | complete |
| Scan | diagnostic | 6 | 6 | complete |
| Scan | formal | 18 | 18 | complete |
| Correctness | selected race cases | 4 | 4 | complete |
| Build | Linux/Windows applications | 4 | 4 | complete |
| HTTP | not selected | not remeasured | none | not_remeasured |

Formal timing comprises eighteen TRACE0 processes and 216 phase records. For each profile, the three serial blocks are H-B-C / C-B-H / B-H-C. C/B and C/H use the same blocks. Six TRACE0 pilot processes qualify fixtures and instrumentation and are excluded from statistics. Six TRACE1 diagnostic processes produce 72 count observations, one per source/phase, and never enter timing statistics.

## Measurement and transaction semantics

| Field / cohort | Meaning and limit |
| --- | --- |
| measurement_version=2, TRACE0 | The pgx SQL tracer is not installed. Every SQL/AUTH counter is present as JSON null. It is unavailable, not zero; legacy text sql=0 is not used to replace null. |
| Formal allocation / pools | Process/pool deltas through observed terminal state and same-task retirement; includes observer and runtime work. No SQL-tracer overhead is included. Heap snapshots are not peaks. Empty-pool wait excludes reserved-owner and ownership-mutex waits. |
| Phase vs whole process | Persisted job elapsed, terminal observation, same-task worker retirement, separate Store.Close, package elapsed, and wall including compilation remain distinct. |
| TRACE1 diagnostic | One sample per source/phase, work counts only. Raw timing/allocation fields are retained but do not support timing, allocation, pool-wait or child-CPU claims. |
| Raw BEGIN / COMMIT / ROLLBACK | Actual client command attempts, including failures; these do not count standalone implicit database transactions. |
| Strict AUTH SQL | Subset of raw SQL, including classified explicit sequences and matching implicit attempts. Never add the subset to raw totals. |
| Confirmed AUTH committed transactions | Explicit attributed AUTH COMMIT plus implicit AUTH commits. Zero-row implicit SELECT results still count as committed database transactions. |
| Implicit attempts | Errors, unconfirmed completion, confirmed single-row, empty, and unexpected-row results remain separate. Errors do not prove rollback or physical retirement. |
| Manual / task classification | Classified explicit transactions plus implicit single-row responses; Go mapping validation follows. These counts are not final successful authorization grants. |
| Real-probe resources | Peak cohorts count overlapping ProbeFile callbacks, not native-process peaks. RUSAGE_CHILDREN counts reaped child CPU/kernel block I/O, not source-read bytes or cache misses. |

Every official phase has n=3 per source. Values are median [minimum, maximum]. Nearest-rank p95 at n=3 is the maximum, not stable tail evidence. cached_1 and cached_2 remain separate; catalog-cold is not physical-storage cold. SQL transaction counts do not assign elapsed or allocation causation.

## Formal untraced timing

| Profile / library / phase | H job ms | B job ms | C job ms | Same-block C/B | Same-block C/H |
| --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 261.347 [247.844, 312.160] | 686.132 [679.210, 731.801] | 710.778 [663.959, 1789.478] | 0.971 [0.968, 2.635] | 2.679 [2.277, 6.847] |
| descriptor400 / directory_episodes / cached_2 | 254.488 [250.286, 272.918] | 691.163 [682.924, 705.496] | 628.562 [591.870, 704.686] | 0.909 [0.867, 0.999] | 2.511 [2.326, 2.582] |
| descriptor400 / directory_episodes / cached_after_incremental | 264.478 [249.633, 279.745] | 705.048 [682.215, 727.366] | 736.235 [628.968, 2421.539] | 1.044 [0.922, 3.329] | 2.949 [2.248, 9.156] |
| descriptor400 / directory_episodes / cold | 901.458 [873.721, 913.745] | 1546.296 [1495.255, 1629.557] | 1479.402 [1437.754, 1527.847] | 0.938 [0.930, 0.989] | 1.619 [1.595, 1.749] |
| descriptor400 / directory_episodes / force_probe | 671.372 [665.663, 728.846] | 1445.639 [1337.268, 1446.192] | 1270.342 [1219.255, 1281.819] | 0.886 [0.843, 0.950] | 1.816 [1.743, 1.926] |
| descriptor400 / directory_episodes / incremental | 292.637 [288.727, 332.367] | 2282.316 [735.198, 2282.825] | 674.147 [647.000, 2568.649] | 0.880 [0.295, 1.125] | 2.335 [1.947, 8.778] |
| descriptor400 / directory_episodes / task_owned_cached | 359.208 [343.120, 360.782] | 953.198 [872.092, 2579.644] | 819.391 [789.254, 829.310] | 0.860 [0.306, 0.951] | 2.299 [2.197, 2.388] |
| descriptor400 / flat_episodes / cached_1 | 643.187 [600.922, 655.591] | 2023.759 [1961.575, 2032.777] | 1740.397 [1701.643, 1826.927] | 0.867 [0.860, 0.899] | 2.832 [2.655, 2.840] |
| descriptor400 / flat_episodes / cached_2 | 579.878 [553.878, 588.847] | 2060.984 [1962.173, 2131.903] | 1832.174 [1750.139, 6348.621] | 0.934 [0.821, 3.080] | 3.308 [2.972, 10.948] |
| descriptor400 / flat_episodes / cached_after_incremental | 575.272 [567.801, 589.169] | 1932.162 [1897.985, 1962.807] | 2650.911 [1719.278, 3185.735] | 1.351 [0.890, 1.678] | 4.669 [2.989, 5.407] |
| descriptor400 / flat_episodes / cold | 3097.976 [3017.171, 3135.649] | 5470.793 [5401.034, 5478.767] | 4997.350 [4919.996, 5065.760] | 0.913 [0.911, 0.925] | 1.613 [1.569, 1.679] |
| descriptor400 / flat_episodes / force_probe | 2297.598 [2282.853, 2347.326] | 4527.869 [4496.896, 4560.769] | 4172.235 [4035.106, 4479.639] | 0.928 [0.885, 0.989] | 1.816 [1.719, 1.962] |
| descriptor400 / flat_episodes / incremental | 614.528 [608.795, 617.721] | 6365.521 [2016.480, 6712.214] | 1864.352 [1760.370, 4124.978] | 0.615 [0.293, 0.873] | 3.018 [2.865, 6.776] |
| descriptor400 / flat_episodes / task_owned_cached | 988.763 [915.392, 1052.605] | 2634.521 [2634.030, 5658.950] | 2208.585 [2204.789, 2236.065] | 0.838 [0.390, 0.849] | 2.234 [2.095, 2.443] |
| descriptor400 / flat_movies / cached_1 | 424.470 [423.028, 427.955] | 1506.327 [1491.082, 1644.632] | 1377.858 [1300.554, 1522.579] | 0.863 [0.838, 1.021] | 3.257 [3.064, 3.558] |
| descriptor400 / flat_movies / cached_2 | 409.644 [407.407, 413.064] | 1622.845 [1557.502, 1665.855] | 1375.563 [1307.678, 4992.406] | 0.848 [0.785, 3.205] | 3.376 [3.166, 12.187] |
| descriptor400 / flat_movies / cached_after_incremental | 406.801 [400.972, 420.893] | 1658.059 [1508.343, 4024.207] | 1443.421 [1324.230, 5429.859] | 0.799 [0.359, 3.600] | 3.429 [3.255, 13.542] |
| descriptor400 / flat_movies / cold | 2429.905 [2399.085, 2451.752] | 4500.708 [4478.435, 4641.453] | 4085.361 [4013.543, 4125.484] | 0.892 [0.889, 0.912] | 1.683 [1.652, 1.703] |
| descriptor400 / flat_movies / force_probe | 1873.426 [1798.169, 1936.603] | 3801.488 [3795.917, 4112.068] | 3405.262 [3342.993, 9111.513] | 0.897 [0.879, 2.216] | 1.859 [1.758, 4.864] |
| descriptor400 / flat_movies / incremental | 509.615 [471.673, 535.926] | 4789.464 [1628.178, 5916.029] | 1392.596 [1380.398, 1478.407] | 0.288 [0.250, 0.855] | 2.709 [2.598, 3.134] |
| descriptor400 / flat_movies / task_owned_cached | 715.010 [712.869, 730.512] | 2187.740 [2106.303, 2263.468] | 1882.917 [1846.647, 1897.680] | 0.861 [0.838, 0.877] | 2.598 [2.583, 2.641] |
| real160 / mixed_real_probe / cold | 3349.208 [3310.304, 3413.539] | 5817.063 [5646.446, 5993.106] | 5333.561 [5064.525, 14216.269] | 0.890 [0.871, 2.518] | 1.592 [1.484, 4.295] |
| real160 / mixed_real_probe / force | 2619.328 [2600.563, 2765.992] | 5312.527 [5013.714, 13262.340] | 4560.315 [4474.672, 4767.797] | 0.897 [0.337, 0.910] | 1.724 [1.721, 1.741] |
| real160 / mixed_real_probe / warm | 917.234 [846.291, 956.256] | 2454.404 [2445.533, 3607.568] | 2288.476 [2080.898, 6551.683] | 0.932 [0.851, 1.816] | 2.704 [2.269, 6.851] |

### Terminal and same-task retirement observations

| Profile / library / phase | H terminal ms | B terminal ms | C terminal ms | H retirement us | B retirement us | C retirement us |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 504.756 [252.982, 507.785] | 753.938 [753.723, 755.725] | 753.535 [753.329, 2003.269] | 10.490 [2.490, 31.329] | 8.590 [2.670, 11.620] | 11.260 [10.939, 16.440] |
| descriptor400 / directory_episodes / cached_2 | 503.978 [503.938, 506.079] | 753.631 [753.618, 754.416] | 755.127 [753.360, 755.134] | 9.729 [9.690, 11.320] | 9.190 [7.849, 10.999] | 8.730 [8.690, 11.809] |
| descriptor400 / directory_episodes / cached_after_incremental | 503.786 [503.764, 503.855] | 753.763 [752.981, 754.427] | 753.827 [752.976, 2504.420] | 10.470 [9.831, 11.320] | 8.320 [8.160, 8.460] | 7.420 [7.030, 9.221] |
| descriptor400 / directory_episodes / cold | 1004.010 [1003.758, 1004.279] | 1753.931 [1503.215, 1754.175] | 1503.363 [1503.098, 1755.142] | 9.189 [5.439, 9.940] | 12.950 [5.030, 14.189] | 8.420 [7.279, 13.661] |
| descriptor400 / directory_episodes / force_probe | 753.887 [753.765, 755.958] | 1503.745 [1503.502, 1505.392] | 1503.807 [1254.613, 1504.076] | 14.380 [7.980, 22.669] | 9.869 [8.320, 12.659] | 11.760 [10.700, 11.780] |
| descriptor400 / directory_episodes / incremental | 504.072 [503.345, 504.776] | 2507.620 [754.458, 2507.651] | 755.279 [754.491, 2762.578] | 8.579 [8.520, 9.240] | 11.830 [7.510, 15.219] | 9.970 [8.360, 10.111] |
| descriptor400 / directory_episodes / task_owned_cached | 504.556 [504.025, 504.969] | 1004.102 [1004.025, 2762.238] | 1004.358 [1004.146, 1004.530] | 9.360 [9.300, 20.049] | 10.080 [8.730, 20.960] | 9.670 [9.640, 12.509] |
| descriptor400 / flat_episodes / cached_1 | 754.043 [753.532, 754.160] | 2253.949 [2002.981, 2254.327] | 1754.611 [1753.556, 2005.701] | 11.639 [8.880, 21.339] | 12.569 [8.019, 15.410] | 11.501 [7.441, 24.979] |
| descriptor400 / flat_episodes / cached_2 | 753.702 [753.484, 753.757] | 2254.014 [2003.863, 2254.511] | 2004.096 [2003.377, 6509.318] | 12.080 [9.300, 18.789] | 9.310 [8.060, 9.840] | 9.839 [9.679, 21.619] |
| descriptor400 / flat_episodes / cached_after_incremental | 754.311 [754.085, 755.145] | 2004.032 [2003.737, 2004.393] | 2756.585 [1754.596, 3253.808] | 10.250 [3.010, 11.269] | 7.980 [7.650, 9.119] | 13.959 [9.760, 17.920] |
| descriptor400 / flat_episodes / cold | 3253.367 [3253.162, 3254.237] | 5503.381 [5503.136, 5504.086] | 5012.482 [5004.242, 5253.408] | 5.330 [3.300, 22.019] | 8.769 [3.160, 11.859] | 9.421 [2.870, 9.740] |
| descriptor400 / flat_episodes / force_probe | 2504.215 [2503.951, 2504.410] | 4753.478 [4503.337, 4754.562] | 4255.049 [4254.361, 4504.736] | 11.380 [10.119, 11.860] | 8.769 [7.331, 9.960] | 9.210 [9.040, 10.499] |
| descriptor400 / flat_episodes / incremental | 753.891 [753.367, 754.222] | 6508.495 [2254.290, 6798.342] | 2003.893 [2002.806, 4254.161] | 10.180 [9.880, 10.940] | 12.839 [11.310, 36.869] | 9.981 [9.859, 11.009] |
| descriptor400 / flat_episodes / task_owned_cached | 1004.038 [1003.851, 1254.055] | 2754.776 [2753.894, 5756.159] | 2253.884 [2253.668, 2254.023] | 8.360 [2.230, 15.540] | 11.329 [10.631, 11.400] | 7.571 [7.541, 8.779] |
| descriptor400 / flat_movies / cached_1 | 503.011 [502.840, 504.546] | 1753.527 [1503.032, 1754.378] | 1503.659 [1502.995, 1754.893] | 7.950 [4.070, 8.799] | 11.381 [6.790, 22.009] | 9.929 [4.900, 12.120] |
| descriptor400 / flat_movies / cached_2 | 503.319 [503.231, 505.449] | 1753.313 [1753.194, 1753.377] | 1504.439 [1503.322, 5258.266] | 9.469 [8.959, 12.019] | 11.230 [10.999, 13.889] | 8.110 [5.630, 10.049] |
| descriptor400 / flat_movies / cached_after_incremental | 504.616 [503.218, 505.718] | 1756.525 [1753.792, 4255.502] | 1504.034 [1503.501, 5509.672] | 8.530 [8.140, 8.840] | 9.839 [8.630, 10.830] | 8.300 [8.289, 28.879] |
| descriptor400 / flat_movies / cold | 2504.098 [2503.708, 2504.594] | 4754.662 [4503.786, 4754.675] | 4254.174 [4254.123, 4254.417] | 8.099 [7.359, 8.450] | 10.370 [8.541, 23.239] | 7.639 [5.660, 15.059] |
| descriptor400 / flat_movies / force_probe | 2003.760 [2003.420, 2005.275] | 4003.748 [4003.675, 4253.899] | 3503.615 [3503.114, 9268.670] | 9.850 [8.300, 13.639] | 10.590 [5.181, 10.720] | 14.300 [10.309, 21.830] |
| descriptor400 / flat_movies / incremental | 753.852 [503.622, 754.159] | 5003.438 [1754.233, 6009.148] | 1503.852 [1503.346, 1504.691] | 9.839 [9.460, 11.890] | 8.679 [8.340, 9.150] | 8.449 [8.069, 9.119] |
| descriptor400 / flat_movies / task_owned_cached | 755.192 [754.227, 756.048] | 2255.433 [2255.140, 2504.608] | 2005.624 [2005.235, 2005.941] | 8.741 [8.371, 9.079] | 12.700 [10.320, 13.619] | 39.450 [8.951, 85.898] |
| real160 / mixed_real_probe / cold | 3506.117 [3505.242, 3507.779] | 6007.139 [5755.501, 6010.300] | 5508.842 [5257.048, 14264.370] | 5.920 [4.480, 9.740] | 8.560 [8.550, 9.800] | 16.021 [8.020, 22.249] |
| real160 / mixed_real_probe / force | 2754.399 [2754.247, 3004.170] | 5504.694 [5253.809, 13505.862] | 4754.745 [4504.123, 5005.130] | 10.100 [9.021, 10.190] | 12.050 [7.730, 12.520] | 11.381 [7.450, 14.899] |
| real160 / mixed_real_probe / warm | 1004.974 [1004.323, 1005.439] | 2504.461 [2503.715, 3754.530] | 2504.997 [2253.500, 6758.749] | 8.500 [4.730, 8.700] | 8.490 [8.109, 11.199] | 10.699 [4.360, 10.820] |

### Untraced process allocation and pool observations

| Profile / library / phase | H allocated MiB | B allocated MiB | C allocated MiB | H empty wait ms | B empty wait ms | C empty wait ms |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 6.662 [6.610, 6.686] | 22.628 [22.612, 22.703] | 23.058 [23.032, 23.159] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_2 | 6.565 [6.536, 6.629] | 22.716 [22.677, 22.730] | 23.249 [23.091, 23.250] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_after_incremental | 6.618 [6.513, 6.639] | 22.764 [22.656, 22.824] | 23.258 [23.240, 23.280] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cold | 43.148 [42.959, 43.434] | 88.126 [88.107, 88.419] | 89.283 [89.264, 89.403] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / force_probe | 42.463 [42.461, 42.551] | 87.323 [87.322, 87.349] | 88.103 [87.882, 88.254] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / incremental | 9.215 [9.108, 9.250] | 25.987 [25.966, 26.072] | 26.478 [26.304, 26.669] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / task_owned_cached | 6.907 [6.892, 6.959] | 23.957 [23.743, 23.962] | 23.212 [23.081, 23.499] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_1 | 13.676 [13.585, 13.714] | 75.725 [75.609, 75.864] | 76.751 [76.651, 76.959] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_2 | 13.587 [13.502, 13.663] | 75.880 [75.421, 75.918] | 77.087 [76.669, 77.098] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_after_incremental | 13.587 [13.552, 13.719] | 75.767 [75.197, 75.791] | 77.147 [77.024, 77.154] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cold | 158.424 [157.955, 158.747] | 336.641 [335.654, 336.849] | 339.272 [338.978, 339.551] | 0.000 [0.000, 0.000] | 0.000 [0.000, 1.829] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / force_probe | 155.283 [154.625, 155.394] | 333.025 [332.806, 333.074] | 335.512 [335.481, 335.565] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / incremental | 16.431 [16.332, 16.455] | 79.281 [78.952, 79.303] | 80.423 [80.191, 80.440] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / task_owned_cached | 15.057 [15.025, 15.211] | 79.410 [79.081, 79.503] | 77.509 [77.341, 77.520] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_1 | 9.943 [9.901, 9.945] | 61.701 [61.622, 61.790] | 62.548 [62.520, 62.597] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_2 | 9.975 [9.897, 10.012] | 61.531 [61.517, 61.535] | 62.916 [62.798, 63.078] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_after_incremental | 9.902 [9.871, 10.025] | 61.645 [61.597, 61.712] | 62.661 [62.594, 62.681] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cold | 132.117 [131.814, 132.165] | 280.724 [280.294, 280.803] | 282.570 [282.517, 282.907] | 3.050 [2.893, 3.196] | 4.799 [3.112, 5.269] | 3.017 [2.859, 5.129] |
| descriptor400 / flat_movies / force_probe | 128.358 [128.099, 128.700] | 276.440 [276.258, 276.566] | 278.574 [278.354, 278.990] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 2.857 [0.000, 4.124] |
| descriptor400 / flat_movies / incremental | 12.529 [12.513, 12.544] | 64.913 [64.899, 64.940] | 65.960 [65.911, 66.014] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / task_owned_cached | 11.078 [11.000, 11.128] | 64.787 [64.727, 64.885] | 63.320 [63.045, 63.361] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| real160 / mixed_real_probe / cold | 260.071 [259.517, 260.370] | 413.817 [413.631, 414.148] | 411.114 [410.928, 411.468] | 3.383 [3.295, 3.532] | 5.728 [3.233, 6.816] | 5.389 [4.732, 5.940] |
| real160 / mixed_real_probe / force | 256.747 [255.846, 256.812] | 409.388 [409.358, 410.186] | 406.934 [406.141, 407.418] | 0.000 [0.000, 0.000] | 0.000 [0.000, 1.496] | 0.000 [0.000, 0.000] |
| real160 / mixed_real_probe / warm | 17.246 [17.185, 17.412] | 72.681 [72.561, 72.805] | 70.851 [70.050, 71.228] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |

### Profile-level process observations

These process windows include fixtures and observers; wall additionally includes compilation. Store.Close is outside every phase window.

| Profile | H package s | B package s | C package s | H wall s | B wall s | C wall s | H Store.Close ms | B Store.Close ms | C Store.Close ms |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 | 21.977 [21.720, 21.997] | 56.544 [47.240, 64.792] | 52.019 [51.506, 52.027] | 22.525 [22.277, 22.549] | 57.149 [47.828, 65.355] | 52.565 [52.055, 52.600] | 0.309 [0.301, 0.388] | 0.313 [0.281, 0.526] | 0.327 [0.282, 0.353] |
| real160 | 7.546 [7.543, 7.801] | 14.351 [14.042, 23.321] | 13.053 [12.813, 25.902] | 8.093 [8.071, 8.331] | 14.902 [14.663, 23.873] | 13.650 [13.379, 26.482] | 0.372 [0.343, 0.406] | 0.546 [0.423, 0.650] | 0.347 [0.328, 0.603] |

## Separate traced count diagnostics

Each row is a single traced diagnostic observation, n=1. No fabricated n=3 median or timing ratio is calculated. Implicit transactions remain transactions even when explicit BEGIN/COMMIT command counts fall.

| Profile / library / phase | Source | Raw SQL | Raw BEGIN | Raw COMMIT | AUTH SQL | Explicit AUTH commits | Implicit AUTH commits | Confirmed AUTH TX | Implicit single rows | Implicit empty | Implicit errors | Implicit unconfirmed | Implicit unexpected |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / flat_movies / cold | sourceH | 6325 | 491 | 491 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cold | sourceH | 7932 | 605 | 605 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cold | sourceH | 2268 | 179 | 179 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_1 | sourceH | 1205 | 171 | 171 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_1 | sourceH | 1734 | 221 | 221 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_1 | sourceH | 696 | 83 | 83 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_2 | sourceH | 1205 | 171 | 171 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_2 | sourceH | 1734 | 221 | 221 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_2 | sourceH | 696 | 83 | 83 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / task_owned_cached | sourceH | 2033 | 171 | 171 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / task_owned_cached | sourceH | 2722 | 221 | 221 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / task_owned_cached | sourceH | 964 | 83 | 83 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / incremental | sourceH | 1283 | 174 | 174 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / incremental | sourceH | 1814 | 224 | 224 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / incremental | sourceH | 776 | 86 | 86 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_after_incremental | sourceH | 1205 | 171 | 171 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_after_incremental | sourceH | 1734 | 221 | 221 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_after_incremental | sourceH | 696 | 83 | 83 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / force_probe | sourceH | 5525 | 331 | 331 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / force_probe | sourceH | 6918 | 413 | 413 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / force_probe | sourceH | 1992 | 131 | 131 | 10 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cold | sourceB | 12135 | 1621 | 1621 | 5660 | 1132 | 0 | 1132 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cold | sourceB | 14884 | 1957 | 1957 | 6770 | 1354 | 0 | 1354 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cold | sourceB | 4348 | 583 | 583 | 2030 | 406 | 0 | 406 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_1 | sourceB | 4455 | 821 | 821 | 3260 | 652 | 0 | 652 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_1 | sourceB | 5614 | 997 | 997 | 3890 | 778 | 0 | 778 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_1 | sourceB | 2008 | 343 | 343 | 1310 | 262 | 0 | 262 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_2 | sourceB | 4455 | 821 | 821 | 3260 | 652 | 0 | 652 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_2 | sourceB | 5614 | 997 | 997 | 3890 | 778 | 0 | 778 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_2 | sourceB | 2008 | 343 | 343 | 1310 | 262 | 0 | 262 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / task_owned_cached | sourceB | 7233 | 821 | 821 | 5216 | 652 | 0 | 652 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / task_owned_cached | sourceB | 8930 | 997 | 997 | 6224 | 778 | 0 | 778 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / task_owned_cached | sourceB | 3056 | 343 | 343 | 2096 | 262 | 0 | 262 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / incremental | sourceB | 4549 | 827 | 827 | 3275 | 655 | 0 | 655 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / incremental | sourceB | 5710 | 1003 | 1003 | 3905 | 781 | 0 | 781 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / incremental | sourceB | 2104 | 349 | 349 | 1325 | 265 | 0 | 265 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_after_incremental | sourceB | 4455 | 821 | 821 | 3260 | 652 | 0 | 652 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_after_incremental | sourceB | 5614 | 997 | 997 | 3890 | 778 | 0 | 778 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_after_incremental | sourceB | 2008 | 343 | 343 | 1310 | 262 | 0 | 262 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / force_probe | sourceB | 11175 | 1461 | 1461 | 5660 | 1132 | 0 | 1132 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / force_probe | sourceB | 13678 | 1765 | 1765 | 6770 | 1354 | 0 | 1354 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / force_probe | sourceB | 4024 | 535 | 535 | 2030 | 406 | 0 | 406 | 0 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cold | sourceC | 7619 | 492 | 492 | 1144 | 3 | 1129 | 1132 | 1129 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cold | sourceC | 9480 | 606 | 606 | 1366 | 3 | 1351 | 1354 | 1351 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cold | sourceC | 2736 | 180 | 180 | 418 | 3 | 403 | 406 | 403 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_1 | sourceC | 1859 | 172 | 172 | 664 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_1 | sourceC | 2514 | 222 | 222 | 790 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_1 | sourceC | 972 | 84 | 84 | 274 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_2 | sourceC | 1859 | 172 | 172 | 664 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_2 | sourceC | 2514 | 222 | 222 | 790 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_2 | sourceC | 972 | 84 | 84 | 274 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / task_owned_cached | sourceC | 2690 | 172 | 172 | 673 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / task_owned_cached | sourceC | 3505 | 222 | 222 | 799 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / task_owned_cached | sourceC | 1243 | 84 | 84 | 283 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / incremental | sourceC | 1941 | 175 | 175 | 667 | 3 | 652 | 655 | 652 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / incremental | sourceC | 2598 | 225 | 225 | 793 | 3 | 778 | 781 | 778 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / incremental | sourceC | 1056 | 87 | 87 | 277 | 3 | 262 | 265 | 262 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_after_incremental | sourceC | 1859 | 172 | 172 | 664 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_after_incremental | sourceC | 2514 | 222 | 222 | 790 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_after_incremental | sourceC | 972 | 84 | 84 | 274 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / force_probe | sourceC | 6659 | 332 | 332 | 1144 | 3 | 1129 | 1132 | 1129 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / force_probe | sourceC | 8274 | 414 | 414 | 1366 | 3 | 1351 | 1354 | 1351 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / force_probe | sourceC | 2412 | 132 | 132 | 418 | 3 | 403 | 406 | 403 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / cold | sourceH | 8062 | 559 | 559 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / warm | sourceH | 2259 | 207 | 207 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / force | sourceH | 7219 | 399 | 399 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / cold | sourceB | 17575 | 1724 | 1724 | 9336 | 1167 | 0 | 1167 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / warm | sourceB | 7740 | 892 | 892 | 5496 | 687 | 0 | 687 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / force | sourceB | 16572 | 1564 | 1564 | 9336 | 1167 | 0 | 1167 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / cold | sourceC | 9427 | 560 | 560 | 1188 | 3 | 1164 | 1167 | 1164 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / warm | sourceC | 2952 | 208 | 208 | 708 | 3 | 684 | 687 | 684 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / force | sourceC | 8424 | 400 | 400 | 1188 | 3 | 1164 | 1167 | 1164 | 0 | 0 | 0 | 0 |

All attempts, rollbacks, manual/task classifications, raw records, and diagnostic count partitions remain in summary.json. Confirmed implicit completion and single-row classification are not a proof of final Go acceptance or physical resource retirement.

## Correctness, builds and exact closure

| Family | Case | Qualified | PASS top-level / subtest / package | Missing required | Unexpected skipped | Raw SHA256 |
| --- | --- | --- | --- | --- | --- | --- |
| correctness | authority-one-request | True | 9 / 32 / 1 | [] | [] | 7f424158c3f67200f1dd544e649fbe3b571b3c0ababea82233e55406d164e84f |
| correctness | authority-lifetime-neighbors | True | 83 / 91 / 1 | [] | [] | 4ad3ddfa979a4702b80af4363aff755b550434401f8c42a8dc229de986c65335 |
| correctness | library-full | True | 1284 / 2722 / 1 | [] | [] | c61b4f517031a658be3ea77a2f9f7bd57fb2102e0c0760669fb43ce0fee957a7 |
| correctness | actual-primary-neighbors | True | 2 / 0 / 1 | [] | [] | 0f3134f38966deedb14f47fd90c0a461967e7c41edb1e912a7ba49b545ff3628 |
| builds | linux-goby | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| builds | linux-launcher | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| builds | windows-goby | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| builds | windows-launcher | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |

| Independent receipt | Status | SHA256 |
| --- | --- | --- |
| setup-receipt | confirmed | 729f56bd2aeb5aa791260b779e9e80d7d20cc150068a8b0665655b44204d9991 |
| preparation-receipt | not_applicable | pending |
| scan-pilot-observation-receipt | confirmed | b0a6fd09f97648ead3c47730e83be046a5f5dfed799169920dc51ef716e1dd74 |
| scan-diagnostic-observation-receipt | confirmed | 0e266fdc7b7d3726b0cfb7c0ed7914add98b3f2a7a4f3dc53621d30740f8cb34 |
| scan-formal-observation-receipt | confirmed | 15197dd75dbe417d2c5cfad516ddcf5391f4e431e7dcbf688af915edcaa1cde0 |
| closure-receipt | confirmed | 99a33b6acdcef09f3c06d755d6713e4418d86df0cf5940062fdeeb0d0fb804dd |
| final-evidence-receipt | confirmed | ccff91d80b2ca0700d7168e23e912bf4a6cf2cc75caa9ad33e4ee0e06303af76 |

Actual HLS/Analysis neighbors and the unchanged real 10,000-file library case are correctness guards, not timing remeasurements. Archive/source/driver/post/protected identities, diagnostic qualification, statistical completion, and exact owned-environment closure remain independent contracts. No HTTP performance matrix is selected.

## Retained issues

No selected exported evidence-quality issue was found. Adverse timing samples remain retained; this statement is not a claim that every candidate sample is faster.

## Limits

This batch evaluates a bounded authority round-trip change with common v2 drivers. It does not qualify batch2 phase merging, NFO refactoring, directory-index/reconciliation redesign, authority caching, quota changes, existing clients, A/V, Analysis PART2, GPU/native-hard behavior, physical cold storage, whole-service mixed capacity, deployment, or larger corpora. Trace1 counts cannot explain exact TRACE0 milliseconds. Three same-block samples establish observed central values and ranges, not stable tails or isolated causal allocation/time attribution.

