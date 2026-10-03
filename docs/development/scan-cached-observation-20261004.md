# Cached scan observation consolidation, batch 2 - October 4, 2026

## Cache improvement and retained regressions

Batch 2 has a mixed performance outcome. Descriptor cold C/B medians are 1.036
for directory_episodes (all three matched blocks slower), 1.003 for
flat_episodes and 1.006 for flat_movies (each two of three slower). Descriptor
force_probe medians are 1.027 for directory_episodes and 1.006 for flat_movies,
each slower in two of three blocks. Flat_episodes force_probe median is 0.959,
while its adverse paired observation 2.561 remains retained. Real force has
median C/B 1.041, with two of three blocks slower. These regressions remain
explicit and all adverse samples are retained. The selected acceptance is a
bounded cached-observation optimization and NFO read boundary repair, not
uniform performance improvement.

Across all three descriptor layouts, cached_1, cached_2, task_owned_cached,
incremental and cached_after_incremental improve in all three matched blocks.
Their paired C/B median ranges are respectively 0.670-0.798, 0.623-0.830,
0.716-0.846, 0.696-0.799 and 0.631-0.696. Real warm improves in all three
blocks, median C/B 0.754. The prior batch's two cached_after_incremental
regression targets now improve against the completed batch 1 baseline within
this matched matrix. Ordinary descriptor cached C/H medians remain
1.974-2.472x historical H; that baseline is not restored. Three blocks support
observed medians and ranges, not stable
tails or isolated time/allocation causation.

## Delivered source and completed evidence

Frozen v2 source commit `cac08952e0b694aa6520bbada0741b624ed642cb` is the direct
child of baseline `6b1ca87ca26319565e43936a6b1573d325be2dea`. A Git archive of
the ten committed paths matches every frozen candidate byte hash. Current v2
correctness, builds, pilots, count diagnostics and formal timing are complete,
followed by exact owned-resource closure. Accepted evidence status is
`qualified_and_closed`. The source is accepted with the mixed performance
limitations above; completed evidence does not imply uniform improvement or
completion of deferred work. Exact main/push identities are recorded separately
in the outside publication receipt after publication.

## Failed v1 and accepted v2 qualification

The v1 focused run failed at a same-tick ctime fixture precondition and remains
retained history. It is not a passed attempt and does not enter v2 qualification
or performance statistics. V2 adds an observed filesystem-clock advance barrier
in one test file while all four production files remain unchanged. The original
v1 freeze SHA-256 is
`93a54449b95fdcd9a1ef3bf8aec12f4b06329ea74861065f6843a3f2ff5b8880`;
its candidate archive SHA-256 is
`cc4896655094c3d8ebc45bac1521686a822ca39d27f8420102dd3ff6ef32baf7`.

Current v2 qualification completed four correctness cases, four Linux/Windows
builds, six pilots, six separate count diagnostics and eighteen formal timing
processes. All 216 formal phase records, failures and adverse observations
remain in the full structured evidence. Exact closure and source/protected
contracts are independent of the mixed performance outcome. Do not describe
all product attempts as having passed.

## Measured source and delivered source

| Role | Git base | Measurement-ready archive SHA-256 |
| --- | --- | --- |
| H | `912354f2a48b5043a8824246f2fa011f8f34bbf1` | `709f2c2b88f429c16f8d8d88e156078930088c43a77e46c20c19c02a4dd3cac7` |
| B | `6b1ca87ca26319565e43936a6b1573d325be2dea` | `a08d7f51bebb4f453da6db8131014f09d32fb814348ee7ca00823f3ceb0ca7cd` |
| C v2 | `6b1ca87ca26319565e43936a6b1573d325be2dea` | `4398cc771b8968773ca8aba29455f00135b2e202c14ce50c417d8c830ec7d4c4` |

H is its historical Git archive plus the compatible common-driver overlay.
B measurement-ready bytes are the exact original completed batch 1 archive;
the same archive already contains all four unchanged v2 measurement drivers.
There is no three-driver overlay on B in this batch. C is B plus the ten-path
cached-observation/NFO v2 patch, and no measurement driver is a candidate delta.
Measured archives and prepared overlays remain distinct from the later source
commit. Current freeze SHA-256 is
`50d6e30a3fcd714810d2c2ae3e6b63c3a37a33f66681b1873ca97403859a1a69`.
It binds the source/archive/member and common-driver identities separately from
the later committed path-byte receipt.

The source commit contains exactly these paths:

| Path | Role |
| --- | --- |
| `internal/library/scan.go` | Eligible cached path, deferred episode side effects and final source confirmation |
| `internal/library/nfo.go` | Admitted file/folder observation and outside-phase parse/apply |
| `internal/library/subtitles_scan.go` | Empty-subtitle owner query shared with the cached path |
| `internal/library/cached_sidecar_observation.go` | Bounded stable merged observation and source-change rejection |
| `internal/library/library_editing_test.go` | Existing behavior guard |
| `internal/library/primary_scan_routing_integration_test.go` | Updated eligible cached AUTH budget |
| `internal/library/scan_cached_checkpoint_integration_test.go` | Cached checkpoint budget and preservation guards |
| `internal/library/scan_cached_observation_integration_test.go` | Combined-path authority, source, fallback and retirement guards |
| `internal/library/nfo_observation_test.go` | NFO observation/parse semantics |
| `internal/library/nfo_authority_integration_test.go` | NFO authority and admitted-read guards |

## Selected implementation and boundaries

The eligible stable cached path retains initial source preparation and merges
publication pathname, NFO, subtitle and image-absence observation into the
existing second metadata phase. It reduces the eligible-file AUTH budget from
four to two. Actual Run grants retain fresh task/root authority, exact binding
and final owned publication proof. Immutable observations return only after
handles and leases retire; owner queries, completion checks and catalog writes
occur outside the admitted phase.

Directory reads are bounded to 4096 entries and 4 MiB. EOF-complete stable
evidence is required before concluding absence. Any payload candidate, required
old-sidecar deletion, overflow, incomplete or unstable evidence returns to the
full scanner. Truncated, extra-filtered or theme-filtered entries cannot prove
complete absence. Candidate indexes and warning/scanner state are applied only
after the admitted phase and actual resource closes succeed. A positive active
subtitle owner lookup falls back without transferring a speculative transaction.

Cached episode series/season side effects are deferred until after the merged
phase and final source confirmation. Known root or primary changes return
sourceChanged rejection rather than allowing soft fallback to publish old
metadata. An unconditional phase-end Lstat, including ctime, confirms the source
even when earlier observations requested fallback. These guards preserve early
cold-source rejection before virtual-folder writes, accepted committed prefixes,
checkpoint position, cancellation, Busy handling and actual retirement.

File and folder NFO reads occur as bounded observation within admitted metadata
phases. Parsing and warning/state application occur outside. Preserve safe
paths, the 2 MiB bound, invalid preferred-file stopping without less-specific
fallback, previous metadata on ordinary I/O/content failures, disabled-option
retention and deletion only after complete absence. Context/deadline, Close
failure and unknown retirement are fatal rather than warnings. Ordinary NFO
I/O warning semantics do not become whole-library failure semantics.

## Measurement contracts and limits

The unchanged four v2 drivers prepare the complete 400-descriptor and task-owned
160-real profiles. Six TRACE0 pilots qualify fixtures/instrumentation only and
are excluded from formal statistics. Six TRACE1 count diagnostics remain
separate from eighteen TRACE0 formal processes with H-B-C, C-B-H and B-H-C
blocks. C/B and C/H use matched blocks. Official timing has SQL/AUTH counters
unavailable, not zero; explicit/implicit authority and raw client-command counts
belong to traced diagnostic observations. Autocommit requests still have database
transactions, and counts do not isolate transaction milliseconds or cause
untraced elapsed/allocation changes.

Fixed Go/FFmpeg/PostgreSQL, CGO_ENABLED=1 and GOMAXPROCS=4, metric windows and
concurrency contracts remain. Formal timing uses no race or profiler. Keep
all sample arrays, adverse observations, process/allocation/pool scopes and
n=3 median/range limits; no stable tail or return-to-H claim follows from this
small matched comparison. There is no HTTP performance matrix in this batch.

## Exact evidence and closure

Final evidence receipt:
`D:/Code/goby/.artifacts/scan-cached-observation-20261004/final-evidence-receipt.json`,
SHA-256 `332eb7e7c1e045fcf8d1849919673a0d626c88a032251208122efec9339a22a4`.
Exact closure receipt:
`D:/Code/goby/.artifacts/scan-cached-observation-20261004/closure-receipt.json`,
SHA-256 `8aa7a533f61e0d3c91705101f4a7979e43961495ad8bab99ad35a9da6b9c24f6`.
It reports exact private closure with zero owned references, mounts or errors.
Source/data/image/cache/raw and failure evidence stay retained; closure is the
remote owner's exported receipt, not a new local runtime observation.

The [structured record](scan-cached-observation-20261004.json) is the complete
accepted analysis `summary.json`, retaining every sample, diagnostic, adverse
observation, failed-v1 history, source/configuration identity, archive/driver
manifest, statistic and separate receipt. Only CRLF line endings are normalized
to LF for the repository's text policy; all values and records are unchanged.
Published JSON SHA-256 is `4507e88d92a1dac0a0f1dba61a863075669bf443d09da481a2fe4cf586e39ce3`; the external original retains
SHA-256 `38902c3d463761723a45dde7d7df774a05fc1de8c50c4bd6a41779e41a4bd5c1`.
The full accepted analysis Markdown is appended below unchanged except for LF
normalization. Its original SHA-256 is
`7a8f40c531b8b7de2fa69db0201a00e4461c1c1be1487258642539ed079fd2c4`.

The independent seal binds 154 actual exported evidence files, all present and
hash-matched. All eighteen formal aggregate records match their immutable run
receipts and raw logs, and all 216 formal phase records are retained. Failed-v1
history is `confirmed_failed_retained`, distinct from current v2 qualification.
Full accepted source/protected-resource, parser and exact-closure references
remain in the structured record. The outside seal binds original evidence
inputs, not the hash of this publication wrapper.

## Remaining task decisions

Only the selected batch 2 cache optimization and file/folder NFO boundary repair
are qualified within the declared evidence scope. Batch 3 directory-index/
reconciliation redesign is not authorized. TTL authority caching, quota changes,
existing-client A/V, Analysis PART2, GPU/native-hard qualification, whole-service
mixed capacity, images and deployment remain unselected. This bounded delivery
does not complete the whole performance program or broaden accepted Docker
checkpoints.

## Complete accepted analysis

# Cached scan observation consolidation, batch 2

Generated only from completed exported static evidence. No product, test, build, runtime probe, SSH operation, or measurement is executed by this analysis.

Final qualification: `qualified_and_closed`.

Current candidate v2 passes the required source, runtime, sealed-evidence and exact owned-resource closure gates. Candidate v1's failed ctime fixture attempt remains separately archived and explicitly unqualified; it is not included in current v2 correctness or performance statistics.

The performance acceptance is limited to the cached-observation target and the verified NFO boundary repair, not a uniform speedup. Every descriptor layout improves cached_1, cached_2, task_owned_cached, incremental and cached_after_incremental job time in all three matched blocks; real warm also improves in all three blocks. Current paired cached_after_incremental C/B medians are 0.682 for directory_episodes, 0.696 for flat_episodes and 0.631 for flat_movies. Batch 1's earlier directory/flat-episode cached-after regressions remain in its unchanged report and are not dismissed as noise or pooled into this batch's different source comparison.

Cold and force costs remain explicit. Directory-episode cold is slower in all three blocks: C/B median 1.036 [1.009, 1.061]. Flat-episode cold is 1.003 [0.998, 1.020] and flat-movie cold is 1.006 [0.987, 1.036], each slower in two blocks. Directory force is 1.027 [0.944, 1.066], flat-movie force is 1.006 [0.388, 1.492], and real force is 1.041 [0.461, 1.111], each slower in two blocks. All adverse samples, matched C/H comparisons and full ranges remain retained. The admitted folder-NFO boundary is part of the implementation change, but its isolated time cost was not measured.

Separate n=1 traced diagnostics show flat_movies/cached_1 B/C raw SQL 1859/1539 and confirmed AUTH committed transactions 652/332. Real warm is 2952/2633 SQL and 687/368 confirmed AUTH committed transactions. Explicit raw BEGIN/COMMIT command counts remain distinct from implicit transactions; count reductions do not assign exact untraced timing causation. Single-row responses precede Go authorization validation, and no stable-tail or whole-service capacity claim follows from three formal blocks.

## Frozen source and shared measurement identities

H is historical, B is the current committed baseline, and C is the frozen candidate overlay. All sources use the same v2 measurement drivers. A candidate base Git head is not a candidate commit identity. Original Git archives and complete measurement-ready archives are separate byte identities.

Source freeze: `50d6e30a3fcd714810d2c2ae3e6b63c3a37a33f66681b1873ca97403859a1a69`; source binding status: `confirmed`.

| Source | Role | Git base | Original archive SHA256 | Measurement-ready SHA256 | Staged manifest SHA256 |
| --- | --- | --- | --- | --- | --- |
| sourceH | historical | 912354f2a48b5043a8824246f2fa011f8f34bbf1 | c54573195606653dea488cd26a48466d1aae785270118e96b450a5e472c4844b | 709f2c2b88f429c16f8d8d88e156078930088c43a77e46c20c19c02a4dd3cac7 | b80f8ab87a912fa0bbbb756857c0f0a0a37fd5097ffd190ef22e375d25a7a565 |
| sourceB | baseline | 6b1ca87ca26319565e43936a6b1573d325be2dea | a08d7f51bebb4f453da6db8131014f09d32fb814348ee7ca00823f3ceb0ca7cd | a08d7f51bebb4f453da6db8131014f09d32fb814348ee7ca00823f3ceb0ca7cd | b4d84c764fa1b7c35acbe1d7b3a4452b0929728e5e30b83fad44cebbfd7f0959 |
| sourceC | candidate | 6b1ca87ca26319565e43936a6b1573d325be2dea | a08d7f51bebb4f453da6db8131014f09d32fb814348ee7ca00823f3ceb0ca7cd | 4398cc771b8968773ca8aba29455f00135b2e202c14ce50c417d8c830ec7d4c4 | 5246703eb74bb13cfe3308bfccdb53c0fe354850371a675c9092a8c7d6ef3d0d |

| Shared driver | SHA256 |
| --- | --- |
| internal/library/scan_performance_integration_test.go | 95da8a0ac7c3a5aa7aa95f6ac71f7eadb10399e6ff055eb0b2796c88941e2dec |
| internal/library/scan_performance_measurement_integration_test.go | b753e4ae946544735abb6d5db5ee3472f7294847c3035d714eb63dbc2af386b7 |
| internal/library/scan_probe_pipeline_performance_integration_test.go | 6c85de1b388c4aa0e63b3f14e0ef6eec5a144223cdcd09d3c7d189bc5586a0b7 |
| internal/server/http_get_stop_remeasure_integration_test.go | e421241ee9079a8a139f5cd7355c18c64c7122dfd03540d767a8af09ae46f2a0 |

The actual original and ready archive regular-member hashes are compared without extraction. H/B ready changes are confined to shared drivers; C ready bytes bind the exact candidate overlay. Each complete ready archive must equal its staged manifest, and B/C staged production/test changes must equal the exact frozen non-driver paths.

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
| descriptor400 / directory_episodes / cached_1 | 251.904 [248.187, 337.777] | 638.941 [623.065, 2098.223] | 509.635 [497.229, 529.119] | 0.798 [0.237, 0.849] | 1.974 [1.509, 2.132] |
| descriptor400 / directory_episodes / cached_2 | 254.783 [247.495, 264.935] | 612.716 [609.581, 651.955] | 519.984 [508.650, 545.286] | 0.830 [0.798, 0.895] | 2.058 [1.996, 2.101] |
| descriptor400 / directory_episodes / cached_after_incremental | 258.778 [256.937, 581.473] | 730.876 [620.156, 2226.764] | 498.666 [497.988, 540.724] | 0.682 [0.224, 0.872] | 1.924 [0.930, 1.941] |
| descriptor400 / directory_episodes / cold | 855.529 [853.111, 891.307] | 1430.853 [1371.551, 1453.671] | 1466.840 [1454.886, 1482.225] | 1.036 [1.009, 1.061] | 1.715 [1.632, 1.737] |
| descriptor400 / directory_episodes / force_probe | 675.330 [673.439, 703.092] | 1263.820 [1199.514, 1368.905] | 1278.149 [1193.439, 1405.743] | 1.027 [0.944, 1.066] | 1.893 [1.772, 1.999] |
| descriptor400 / directory_episodes / incremental | 297.950 [293.408, 722.250] | 677.207 [643.820, 2510.515] | 555.523 [540.927, 579.452] | 0.799 [0.231, 0.863] | 1.815 [0.769, 1.975] |
| descriptor400 / directory_episodes / task_owned_cached | 353.077 [344.205, 414.255] | 778.975 [761.752, 841.000] | 652.728 [644.356, 672.456] | 0.846 [0.776, 0.863] | 1.872 [1.576, 1.905] |
| descriptor400 / flat_episodes / cached_1 | 573.795 [568.167, 588.310] | 1860.758 [1764.425, 3662.481] | 1182.970 [1165.465, 1330.965] | 0.670 [0.318, 0.715] | 2.082 [2.031, 2.262] |
| descriptor400 / flat_episodes / cached_2 | 569.760 [564.666, 580.999] | 1838.260 [1718.938, 4016.265] | 1241.062 [1239.614, 1315.804] | 0.716 [0.309, 0.722] | 2.178 [2.134, 2.330] |
| descriptor400 / flat_episodes / cached_after_incremental | 654.473 [574.257, 1554.945] | 1823.929 [1689.152, 5831.799] | 1298.734 [1176.157, 1347.268] | 0.696 [0.231, 0.712] | 1.797 [0.835, 2.346] |
| descriptor400 / flat_episodes / cold | 2957.090 [2932.544, 3104.786] | 5012.720 [4876.894, 5028.736] | 5004.733 [4972.355, 5043.533] | 1.003 [0.998, 1.020] | 1.696 [1.612, 1.706] |
| descriptor400 / flat_episodes / force_probe | 2301.921 [2232.552, 3511.022] | 4476.058 [4283.969, 5532.724] | 4226.303 [4107.629, 11462.046] | 0.959 [0.764, 2.561] | 1.836 [1.170, 5.134] |
| descriptor400 / flat_episodes / incremental | 647.585 [608.082, 1806.322] | 1903.270 [1686.947, 5905.360] | 1285.988 [1204.842, 1336.295] | 0.702 [0.218, 0.714] | 2.064 [0.667, 2.115] |
| descriptor400 / flat_episodes / task_owned_cached | 970.398 [945.450, 1032.695] | 2274.769 [2171.354, 2301.280] | 1709.844 [1614.603, 1820.165] | 0.787 [0.702, 0.800] | 1.808 [1.563, 1.876] |
| descriptor400 / flat_movies / cached_1 | 411.118 [400.803, 417.256] | 1332.612 [1320.738, 1476.315] | 1020.755 [1011.624, 1031.438] | 0.759 [0.691, 0.781] | 2.472 [2.461, 2.547] |
| descriptor400 / flat_movies / cached_2 | 416.077 [409.384, 425.706] | 1507.870 [1380.153, 4686.856] | 967.671 [938.875, 971.822] | 0.623 [0.207, 0.701] | 2.293 [2.273, 2.336] |
| descriptor400 / flat_movies / cached_after_incremental | 414.818 [409.473, 1179.391] | 1525.818 [1498.130, 6105.155] | 944.761 [944.756, 969.173] | 0.631 [0.155, 0.635] | 2.278 [0.822, 2.307] |
| descriptor400 / flat_movies / cold | 2404.970 [2387.816, 2442.860] | 3989.509 [3963.879, 4147.039] | 4106.252 [3939.389, 4173.085] | 1.006 [0.987, 1.036] | 1.681 [1.638, 1.748] |
| descriptor400 / flat_movies / force_probe | 1818.754 [1788.461, 3101.841] | 3363.420 [3280.591, 9977.236] | 3870.775 [3384.407, 4895.711] | 1.006 [0.388, 1.492] | 2.164 [1.091, 2.692] |
| descriptor400 / flat_movies / incremental | 466.271 [456.015, 884.473] | 1427.245 [1410.153, 3236.964] | 1003.709 [992.933, 1053.221] | 0.696 [0.325, 0.712] | 2.130 [1.135, 2.310] |
| descriptor400 / flat_movies / task_owned_cached | 717.477 [702.601, 828.945] | 1905.767 [1794.492, 1937.231] | 1364.416 [1334.738, 1402.903] | 0.716 [0.689, 0.782] | 1.942 [1.610, 1.955] |
| real160 / mixed_real_probe / cold | 3314.721 [3287.067, 3453.445] | 5219.598 [5062.348, 11686.358] | 5181.558 [5099.530, 5217.013] | 0.977 [0.446, 1.024] | 1.538 [1.511, 1.576] |
| real160 / mixed_real_probe / force | 2685.810 [2633.529, 2693.856] | 4343.011 [4329.301, 9780.161] | 4508.379 [4504.394, 4826.034] | 1.041 [0.461, 1.111] | 1.679 [1.672, 1.833] |
| real160 / mixed_real_probe / warm | 870.955 [868.660, 982.120] | 2041.527 [2001.107, 6872.584] | 1609.698 [1508.599, 2162.971] | 0.754 [0.315, 0.788] | 1.848 [1.737, 2.202] |

### Terminal and same-task retirement observations

| Profile / library / phase | H terminal ms | B terminal ms | C terminal ms | H retirement us | B retirement us | C retirement us |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 503.843 [253.555, 504.221] | 755.116 [754.383, 2257.790] | 753.123 [503.048, 754.349] | 8.351 [8.071, 9.609] | 9.690 [8.490, 12.119] | 8.810 [7.250, 17.440] |
| descriptor400 / directory_episodes / cached_2 | 504.424 [253.428, 504.460] | 753.941 [753.913, 754.729] | 753.865 [752.739, 754.348] | 8.720 [7.010, 10.580] | 9.719 [8.890, 10.720] | 9.770 [4.419, 25.210] |
| descriptor400 / directory_episodes / cached_after_incremental | 503.931 [503.613, 754.586] | 754.380 [752.846, 2254.467] | 753.917 [504.430, 754.099] | 11.720 [8.171, 12.889] | 10.029 [2.231, 19.739] | 10.479 [2.680, 12.440] |
| descriptor400 / directory_episodes / cold | 1004.076 [1003.683, 1004.424] | 1503.226 [1503.117, 1504.805] | 1504.212 [1503.222, 1504.878] | 10.000 [9.350, 11.761] | 7.619 [3.380, 9.770] | 9.521 [7.670, 10.890] |
| descriptor400 / directory_episodes / force_probe | 754.266 [753.307, 754.440] | 1503.937 [1254.001, 1504.489] | 1503.498 [1253.687, 1504.407] | 9.180 [7.020, 11.181] | 9.010 [7.960, 11.260] | 11.710 [8.850, 11.940] |
| descriptor400 / directory_episodes / incremental | 504.408 [503.939, 760.160] | 753.796 [753.486, 2755.091] | 754.780 [753.375, 754.903] | 8.840 [8.410, 10.229] | 8.789 [8.459, 10.739] | 8.910 [8.859, 25.090] |
| descriptor400 / directory_episodes / task_owned_cached | 504.740 [504.478, 506.884] | 1004.243 [1003.805, 1004.371] | 754.578 [754.219, 755.000] | 11.029 [9.440, 23.600] | 13.139 [9.850, 15.350] | 9.019 [8.720, 9.361] |
| descriptor400 / flat_episodes / cached_1 | 754.261 [753.881, 755.354] | 2003.320 [2003.269, 3754.306] | 1254.098 [1253.297, 1504.727] | 10.370 [4.230, 21.989] | 8.710 [8.460, 11.251] | 8.800 [8.639, 8.890] |
| descriptor400 / flat_episodes / cached_2 | 753.206 [753.172, 753.822] | 2004.141 [1753.578, 4255.913] | 1254.883 [1253.632, 1503.852] | 10.279 [7.830, 12.949] | 11.969 [7.960, 13.310] | 10.170 [7.399, 10.360] |
| descriptor400 / flat_episodes / cached_after_incremental | 754.158 [753.929, 1753.966] | 2003.913 [1752.984, 6009.168] | 1502.831 [1253.331, 1503.436] | 9.909 [8.269, 10.040] | 8.759 [8.670, 25.570] | 9.871 [8.590, 16.679] |
| descriptor400 / flat_episodes / cold | 3004.299 [3003.556, 3254.123] | 5253.464 [5002.994, 5254.270] | 5253.827 [5003.168, 5254.971] | 10.260 [8.251, 11.890] | 13.100 [8.211, 16.220] | 8.201 [7.950, 12.010] |
| descriptor400 / flat_episodes / force_probe | 2504.374 [2253.299, 3756.763] | 4504.309 [4504.206, 5756.838] | 4253.561 [4253.308, 11511.274] | 9.110 [7.489, 9.569] | 10.780 [9.721, 13.290] | 20.009 [8.141, 36.239] |
| descriptor400 / flat_episodes / incremental | 754.131 [753.954, 2007.642] | 2003.952 [1753.301, 6004.600] | 1503.606 [1253.070, 1504.550] | 9.729 [8.280, 13.849] | 8.379 [8.200, 9.709] | 8.850 [7.200, 9.529] |
| descriptor400 / flat_episodes / task_owned_cached | 1004.891 [1003.746, 1253.855] | 2504.414 [2253.962, 2504.666] | 1755.318 [1754.297, 2004.319] | 8.789 [7.870, 17.020] | 9.359 [8.810, 11.439] | 11.410 [8.250, 12.559] |
| descriptor400 / flat_movies / cached_1 | 503.068 [502.854, 503.105] | 1503.112 [1503.092, 1503.746] | 1253.012 [1252.881, 1253.518] | 8.571 [7.870, 9.459] | 8.469 [7.479, 27.319] | 8.850 [8.849, 10.460] |
| descriptor400 / flat_movies / cached_2 | 503.237 [503.035, 503.890] | 1753.984 [1503.728, 4759.751] | 1003.730 [1003.146, 1004.175] | 9.800 [8.370, 12.500] | 14.290 [10.880, 21.170] | 8.559 [8.210, 9.120] |
| descriptor400 / flat_movies / cached_after_incremental | 503.960 [503.322, 1262.552] | 1753.566 [1504.629, 6270.981] | 1004.163 [1003.142, 1004.881] | 8.761 [8.620, 8.989] | 8.080 [7.280, 10.449] | 8.520 [8.011, 16.890] |
| descriptor400 / flat_movies / cold | 2504.267 [2504.246, 2504.445] | 4003.961 [4003.491, 4253.904] | 4253.859 [4004.241, 4255.601] | 3.660 [3.139, 10.371] | 3.580 [3.430, 7.640] | 9.460 [9.040, 11.109] |
| descriptor400 / flat_movies / force_probe | 2004.858 [2003.644, 3254.596] | 3504.812 [3503.710, 10004.721] | 4003.012 [3506.311, 5004.264] | 10.890 [8.650, 11.339] | 12.520 [7.770, 22.880] | 8.300 [7.850, 12.280] |
| descriptor400 / flat_movies / incremental | 504.991 [503.628, 1003.441] | 1504.668 [1503.566, 3253.521] | 1253.928 [1003.689, 1255.087] | 12.090 [7.779, 23.750] | 8.230 [2.340, 18.629] | 9.630 [2.500, 12.711] |
| descriptor400 / flat_movies / task_owned_cached | 755.374 [755.214, 1004.694] | 2004.611 [2004.590, 2006.370] | 1505.308 [1504.913, 1505.335] | 10.389 [2.961, 22.250] | 9.050 [8.149, 14.830] | 9.120 [8.010, 21.640] |
| real160 / mixed_real_probe / cold | 3506.251 [3505.216, 3508.558] | 5256.662 [5255.737, 11756.857] | 5256.154 [5255.621, 5257.026] | 9.100 [5.280, 12.640] | 8.859 [2.950, 12.729] | 7.799 [3.660, 11.060] |
| real160 / mixed_real_probe / force | 2754.197 [2753.821, 2754.727] | 4504.575 [4504.556, 10011.899] | 4754.973 [4754.325, 5005.216] | 9.570 [8.810, 16.159] | 5.950 [4.550, 12.379] | 5.320 [4.530, 10.149] |
| real160 / mixed_real_probe / warm | 1004.972 [1003.733, 1005.083] | 2255.360 [2254.175, 7011.198] | 1755.637 [1754.741, 2253.405] | 7.319 [5.121, 8.459] | 10.099 [9.320, 15.340] | 3.890 [3.510, 12.659] |

### Untraced process allocation and pool observations

| Profile / library / phase | H allocated MiB | B allocated MiB | C allocated MiB | H empty wait ms | B empty wait ms | C empty wait ms |
| --- | --- | --- | --- | --- | --- | --- |
| descriptor400 / directory_episodes / cached_1 | 6.618 [6.518, 6.618] | 23.081 [22.968, 23.223] | 22.229 [22.225, 22.282] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_2 | 6.530 [6.529, 6.543] | 23.111 [23.094, 23.199] | 22.318 [22.277, 22.349] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cached_after_incremental | 6.502 [6.478, 6.503] | 23.201 [23.099, 23.350] | 22.220 [22.082, 22.341] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / cold | 43.306 [43.163, 43.344] | 89.163 [89.093, 89.194] | 89.285 [89.219, 89.305] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / force_probe | 42.430 [42.354, 42.486] | 88.176 [88.169, 88.290] | 88.124 [88.118, 88.528] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / incremental | 9.208 [9.090, 9.220] | 26.418 [26.338, 26.555] | 25.555 [25.468, 25.614] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / directory_episodes / task_owned_cached | 6.989 [6.847, 6.993] | 23.309 [23.184, 23.319] | 22.508 [22.465, 22.656] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_1 | 13.653 [13.601, 13.696] | 77.072 [76.793, 77.118] | 72.956 [72.859, 73.005] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_2 | 13.557 [13.499, 13.642] | 76.917 [76.805, 77.056] | 72.820 [72.740, 73.100] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cached_after_incremental | 13.700 [13.629, 13.746] | 76.796 [76.573, 77.048] | 73.049 [72.956, 73.222] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / cold | 158.578 [158.086, 158.638] | 338.835 [338.759, 339.064] | 339.266 [339.091, 339.326] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 1.902 [0.000, 2.139] |
| descriptor400 / flat_episodes / force_probe | 154.899 [154.803, 155.508] | 335.569 [335.506, 335.932] | 335.370 [335.239, 335.623] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / incremental | 16.410 [16.357, 16.455] | 80.322 [80.094, 80.409] | 76.349 [76.291, 76.710] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_episodes / task_owned_cached | 15.059 [14.988, 15.105] | 77.697 [77.596, 77.701] | 73.837 [73.772, 74.247] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_1 | 9.907 [9.903, 9.958] | 62.616 [62.437, 62.739] | 59.500 [59.375, 59.557] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_2 | 9.914 [9.891, 9.934] | 62.638 [62.518, 62.823] | 59.368 [59.280, 59.391] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cached_after_incremental | 10.001 [9.922, 10.104] | 62.783 [62.753, 63.015] | 59.352 [59.312, 59.382] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / cold | 131.686 [131.596, 131.792] | 282.655 [282.286, 283.006] | 282.877 [282.782, 282.925] | 2.900 [2.755, 2.932] | 4.781 [4.540, 4.806] | 4.601 [2.944, 4.781] |
| descriptor400 / flat_movies / force_probe | 128.416 [127.982, 128.498] | 278.670 [278.600, 278.719] | 278.814 [278.749, 279.215] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / incremental | 12.519 [12.467, 12.543] | 65.733 [65.682, 66.202] | 62.895 [62.651, 62.928] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| descriptor400 / flat_movies / task_owned_cached | 11.139 [11.044, 11.141] | 63.157 [63.069, 63.404] | 60.176 [60.117, 60.332] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |
| real160 / mixed_real_probe / cold | 259.833 [259.378, 260.081] | 411.424 [411.340, 411.603] | 411.445 [410.745, 411.496] | 3.417 [1.536, 3.528] | 5.284 [5.033, 5.496] | 5.042 [3.288, 5.736] |
| real160 / mixed_real_probe / force | 256.191 [255.760, 256.346] | 406.617 [406.129, 406.877] | 406.929 [406.519, 407.046] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] | 0.000 [0.000, 3.178] |
| real160 / mixed_real_probe / warm | 17.538 [17.505, 17.568] | 70.614 [70.418, 70.803] | 67.802 [67.773, 67.965] | 0.000 [0.000, 3.096] | 0.000 [0.000, 0.000] | 0.000 [0.000, 0.000] |

### Profile-level process observations

These process windows include fixtures and observers; wall additionally includes compilation. Store.Close is outside every phase window.

| Profile | H package s | B package s | C package s | H wall s | B wall s | C wall s | H Store.Close ms | B Store.Close ms | C Store.Close ms |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| descriptor400 | 21.477 [21.223, 28.245] | 52.260 [42.995, 68.262] | 37.484 [36.997, 46.747] | 22.067 [21.793, 28.803] | 52.868 [43.552, 68.810] | 38.063 [37.567, 47.300] | 0.431 [0.398, 0.482] | 0.323 [0.290, 0.349] | 0.327 [0.283, 0.409] |
| real160 | 7.563 [7.551, 7.569] | 12.302 [12.293, 29.068] | 12.298 [12.058, 12.566] | 8.137 [8.103, 8.146] | 12.882 [12.851, 29.610] | 12.857 [12.598, 13.159] | 0.363 [0.346, 0.466] | 0.420 [0.419, 0.470] | 0.381 [0.350, 0.401] |

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
| descriptor400 / flat_movies / cold | sourceB | 7619 | 492 | 492 | 1144 | 3 | 1129 | 1132 | 1129 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cold | sourceB | 9480 | 606 | 606 | 1366 | 3 | 1351 | 1354 | 1351 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cold | sourceB | 2736 | 180 | 180 | 418 | 3 | 403 | 406 | 403 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_1 | sourceB | 1859 | 172 | 172 | 664 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_1 | sourceB | 2514 | 222 | 222 | 790 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_1 | sourceB | 972 | 84 | 84 | 274 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_2 | sourceB | 1859 | 172 | 172 | 664 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_2 | sourceB | 2514 | 222 | 222 | 790 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_2 | sourceB | 972 | 84 | 84 | 274 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / task_owned_cached | sourceB | 2690 | 172 | 172 | 673 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / task_owned_cached | sourceB | 3505 | 222 | 222 | 799 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / task_owned_cached | sourceB | 1243 | 84 | 84 | 283 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / incremental | sourceB | 1941 | 175 | 175 | 667 | 3 | 652 | 655 | 652 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / incremental | sourceB | 2598 | 225 | 225 | 793 | 3 | 778 | 781 | 778 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / incremental | sourceB | 1056 | 87 | 87 | 277 | 3 | 262 | 265 | 262 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_after_incremental | sourceB | 1859 | 172 | 172 | 664 | 3 | 649 | 652 | 649 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_after_incremental | sourceB | 2514 | 222 | 222 | 790 | 3 | 775 | 778 | 775 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_after_incremental | sourceB | 972 | 84 | 84 | 274 | 3 | 259 | 262 | 259 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / force_probe | sourceB | 6659 | 332 | 332 | 1144 | 3 | 1129 | 1132 | 1129 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / force_probe | sourceB | 8274 | 414 | 414 | 1366 | 3 | 1351 | 1354 | 1351 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / force_probe | sourceB | 2412 | 132 | 132 | 418 | 3 | 403 | 406 | 403 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cold | sourceC | 7619 | 492 | 492 | 1144 | 3 | 1129 | 1132 | 1129 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cold | sourceC | 9480 | 606 | 606 | 1366 | 3 | 1351 | 1354 | 1351 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cold | sourceC | 2748 | 180 | 180 | 430 | 3 | 415 | 418 | 415 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_1 | sourceC | 1539 | 172 | 172 | 344 | 3 | 329 | 332 | 329 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_1 | sourceC | 2130 | 222 | 222 | 406 | 3 | 391 | 394 | 391 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_1 | sourceC | 888 | 84 | 84 | 190 | 3 | 175 | 178 | 175 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_2 | sourceC | 1539 | 172 | 172 | 344 | 3 | 329 | 332 | 329 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_2 | sourceC | 2130 | 222 | 222 | 406 | 3 | 391 | 394 | 391 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_2 | sourceC | 888 | 84 | 84 | 190 | 3 | 175 | 178 | 175 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / task_owned_cached | sourceC | 2370 | 172 | 172 | 353 | 3 | 329 | 332 | 329 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / task_owned_cached | sourceC | 3121 | 222 | 222 | 415 | 3 | 391 | 394 | 391 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / task_owned_cached | sourceC | 1159 | 84 | 84 | 199 | 3 | 175 | 178 | 175 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / incremental | sourceC | 1625 | 175 | 175 | 351 | 3 | 336 | 339 | 336 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / incremental | sourceC | 2218 | 225 | 225 | 413 | 3 | 398 | 401 | 398 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / incremental | sourceC | 976 | 87 | 87 | 197 | 3 | 182 | 185 | 182 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / cached_after_incremental | sourceC | 1539 | 172 | 172 | 344 | 3 | 329 | 332 | 329 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / cached_after_incremental | sourceC | 2130 | 222 | 222 | 406 | 3 | 391 | 394 | 391 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / cached_after_incremental | sourceC | 888 | 84 | 84 | 190 | 3 | 175 | 178 | 175 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_movies / force_probe | sourceC | 6659 | 332 | 332 | 1144 | 3 | 1129 | 1132 | 1129 | 0 | 0 | 0 | 0 |
| descriptor400 / flat_episodes / force_probe | sourceC | 8274 | 414 | 414 | 1366 | 3 | 1351 | 1354 | 1351 | 0 | 0 | 0 | 0 |
| descriptor400 / directory_episodes / force_probe | sourceC | 2424 | 132 | 132 | 430 | 3 | 415 | 418 | 415 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / cold | sourceH | 8062 | 559 | 559 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / warm | sourceH | 2259 | 207 | 207 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / force | sourceH | 7219 | 399 | 399 | 16 | 2 | 0 | 2 | 0 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / cold | sourceB | 9427 | 560 | 560 | 1188 | 3 | 1164 | 1167 | 1164 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / warm | sourceB | 2952 | 208 | 208 | 708 | 3 | 684 | 687 | 684 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / force | sourceB | 8424 | 400 | 400 | 1188 | 3 | 1164 | 1167 | 1164 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / cold | sourceC | 9428 | 560 | 560 | 1189 | 3 | 1165 | 1168 | 1165 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / warm | sourceC | 2633 | 208 | 208 | 389 | 3 | 365 | 368 | 365 | 0 | 0 | 0 | 0 |
| real160 / mixed_real_probe / force | sourceC | 8425 | 400 | 400 | 1189 | 3 | 1165 | 1168 | 1165 | 0 | 0 | 0 | 0 |

All attempts, rollbacks, manual/task classifications, raw records, and diagnostic count partitions remain in summary.json. Confirmed implicit completion and single-row classification are not a proof of final Go acceptance or physical resource retirement.

## Correctness, builds and exact closure

| Family | Case | Qualified | PASS top-level / subtest / package | Missing required | Unexpected skipped | Raw SHA256 |
| --- | --- | --- | --- | --- | --- | --- |
| correctness | cached-observation-nfo | True | 20 / 44 / 1 | [] | [] | b060a670d40705b117507c91ec82536b505487d1e13bae7889645ae2af59f7b4 |
| correctness | authority-sidecar-nfo-neighbors | True | 103 / 122 / 1 | [] | [] | 69dffa84d601809a41644073a6256fa96410f6504b75726094d2e7542d2a2e2c |
| correctness | library-full | True | 1297 / 2747 / 1 | [] | [] | d1ccfb51a8aa58d8824edd888398c9637e73ee8e14f10522aa3d221ab042f612 |
| correctness | actual-primary-neighbors | True | 2 / 0 / 1 | [] | [] | 3f6ea1bba8c10c9b0911edde4504d375d31e5e88f2b124754a276beba9b5221a |
| builds | linux-goby | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| builds | linux-launcher | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| builds | windows-goby | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |
| builds | windows-launcher | True | not applicable | [] | [] | e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 |

| Independent receipt | Status | SHA256 |
| --- | --- | --- |
| setup-receipt | confirmed | 9aba9aaf4469352fc06f5df5ccb02fd62341ae01971f3e9252498d119d96ef11 |
| preparation-receipt | not_applicable | pending |
| scan-pilot-observation-receipt | confirmed | 74490775d95cbc1297398cc5658b92248fb450f776b32355e4fbd19f2aef442b |
| scan-diagnostic-observation-receipt | confirmed | 3e5055252216898e2258e4a6f9fb12521f339aee83a3d0a9b653a04bceec5c9b |
| scan-formal-observation-receipt | confirmed | 6f378cae945cb306d569e56cbccf335ba31b6334ffe18bcdfb3be1fe535d41b7 |
| closure-receipt | confirmed | 8aa7a533f61e0d3c91705101f4a7979e43961495ad8bab99ad35a9da6b9c24f6 |
| final-evidence-receipt | confirmed | 332eb7e7c1e045fcf8d1849919673a0d626c88a032251208122efec9339a22a4 |

### Retained failed candidate history

Failed candidate v1 is retained history only, excluded from current v2 correctness and all performance statistics. Failfast missing required tests are not skips or v2 quality issues.

| Version | Case | Exit | Qualified | Raw SHA256 | Original freeze SHA256 |
| --- | --- | --- | --- | --- | --- |
| v1 | cached-observation-nfo | 1 | False | 3a2bea2f8fc9bc6683174681ab9b9d041f6bdfb24b485a5e7894cd11f10517aa | 93a54449b95fdcd9a1ef3bf8aec12f4b06329ea74861065f6843a3f2ff5b8880 |

Actual HLS/Analysis neighbors and the unchanged real 10,000-file library case are correctness guards, not timing remeasurements. Archive/source/driver/post/protected identities, diagnostic qualification, statistical completion, and exact owned-environment closure remain independent contracts. No HTTP performance matrix is selected.

## Retained issues

No selected exported evidence-quality issue was found. Adverse timing samples remain retained; this statement is not a claim that every candidate sample is faster.

## Limits

This batch evaluates eligible cached-observation consolidation and the admitted NFO read boundary with common v2 drivers. It does not qualify batch3 directory-index/reconciliation redesign, authority caching, quota changes, existing clients, A/V, Analysis PART2, GPU/native-hard behavior, physical cold storage, whole-service mixed capacity, deployment, or larger corpora. Trace1 counts cannot explain exact TRACE0 milliseconds. Three same-block samples establish observed central values and ranges, not stable tails or isolated causal allocation/time attribution.

