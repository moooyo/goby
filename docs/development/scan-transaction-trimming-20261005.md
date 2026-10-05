# Scan transaction trimming - October 5, 2026

The user selected evaluation of four scan transaction categories and implementation
of worthwhile simplifications. This change removes avoidable writes and empty
image transactions while preserving atomic accepted media publication. It adds no
lock, worker, schema, cross-media batch or database durability change.

The baseline is `8017567beabab649e657bc204c78e23a6a73aafc`, whose production source
is d993454. The final candidate is an exact twelve-file overlay: six production
files and six test files. Four performance drivers remain byte-identical.
The selected verification is complete. Final v3 performance is mixed: fourteen
of twenty-one paired job medians improve, including the two episode-force
targets, while seven medians regress. This is a bounded optimization with
retained adverse measurements, not uniform improvement.
The v2 source commit is `341caeeabadf8982e46cb90515dfb7972feb7e84`, directly after the
baseline. Its twelve Git blobs match the frozen v2 LF files. Measurement receipts
retain candidate commit=null because the archive was frozen before this Git
commit; source-commit-receipt-v2.json records that exact later mapping.
The final source commit is `450c3364d2db86ec260e8483926a546e17dde2fa`, directly
after 341caee, containing only the replacement-count placement change. Its
cumulative twelve-file delta from 8017567 matches the v3 archive; the final
source mapping is retained in source-commit-receipt-v3.json.

## Four-category assessment

| Category | Decision | Implemented reduction | Preserved behavior |
| --- | --- | --- | --- |
| Primary media, metadata, associations, progress and events | Keep one atomic transaction | Compare all assigned business columns before rewriting items; reuse locked sort provenance to omit an unchanged UPDATE command | Fresh probe, explicit force notification, entity repair, accepted counters, final source proof and commit |
| Actual image-set replacement | Keep atomic replacement | Delete only missing indexes; conditionally upsert nine stored fields; omit after snapshot when no rows change | Invalid types are retained, old-root keys repaired/removed, complete image checks, final physical proof and notification projection |
| No old images and no candidates | Omit the image write transaction | Reuse the existing safe absence path for physical folders | Raw old images from any root, exact committed item identity, complete stable listing and existing ownership completion |
| Pure scan progress | Retain the previous batching policy | No new code change: standalone per-file force progress transactions were already removed | 64-item/500-ms cooperative checks, database-stop observation, failure handling and final flush |

The primary UPSERT uses a null-safe comparison of all eighteen assigned business
fields. A successful identical force probe still increments Updated, repairs
derived associations and sends its explicit CatalogUpdated notification. It no
longer changes items.updated_at/xmin merely to repeat the same stored facts.
An unchanged collage member revision consequently stays stable; changed media or
images continue to invalidate their corresponding facts. Manual metadata overlays
still participate in the same transaction and can require a real item update.

The initial metadata SELECT already locks item_metadata_state. It now returns
automatic_sort_name_explicit with that snapshot. Matching provenance skips its
previous conditional UPDATE roundtrip; a changed flag uses the unchanged SQL.
Provider calls supply no observed flag and retain their old behavior. Current
sorting rules are still read, and no CTE hides mutations from direct-write counts.

Image candidate indexes are contiguous within each replaceable type. The new
DELETE removes indexes beyond the selected count, then upserts each remaining
candidate only when root, path, identity, hash, size, mtime, dimensions or MIME
differs. The SQL does not restrict deletion to the current root. The before
snapshot/item lock and final RunImmediate physical proof remain; only a provably
unchanged after snapshot is omitted. Notification equality alone is insufficient
because it omits file identity, mtime and MIME needed for later reads.

Folder lookup adds raw item_images existence to its existing locked SELECT, with
no additional roundtrip. Absence applies to the exact committed old identity or
a confirmed new ID. Existing rows from obsolete roots cannot be mistaken for
absence. The complete directory/candidate inspection still runs, and only known
no rows plus no candidates avoids the image transaction. The previous diagnostic
had twelve empty folder image commits; their old elapsed time is not a promise
of equal savings under different persistence conditions.

The remaining progress due boundary checks database-only cancellation even when
counters do not change. Removing it would change the chosen stop semantics and
would not address the zero standalone force progress count. The existing policy
already supplies the intended batching, so it is retained.

## Verification contract

Independent static review covered all six production and the initial five test files. One
new image integration test needed its Linux build constraint; this was fixed
before the first freeze. The initial targeted race had forty required tests.
The final ordinary full Library run retains those requirements and adds the
repaired test, with declared environment/opt-in skips. Two TRACE1 count runs
and six TRACE0 timing
runs in B-C/C-B/B-C order. Full Library includes its current capacity test; no
additional standalone 10k, full-server, historical diagnosis or profiler run is
selected. A failed product stops later admission and keeps its raw evidence.

New behavior tests cover primary tuple/timestamp retention with force counters,
notifications and association repair, changed probe facts, sorting provenance
transitions, identical image tuple versions, one changed image without collateral
rewrites, mtime-only changes without content notifications, obsolete-root keys,
missing images/indexes, and invalid candidate retention. Existing final-source,
rollback, manual data, ownership and progress tests remain in the selected scope.

All checks run on test-env. Local work is limited to code edits, formatting, Git,
source archives and offline evidence analysis. Shared build/module caches are
reused, compiler scratch is separate from ext4 media TMPDIR, and the 403,374
reserve setting is retained. Four external timeline containers observed during
preflight are background context; this task does not control their workloads.

## Retained v1 failure and fixture-only v2

The initial race process exited 1, with 105 top-level and 134 subtest passes,
all forty required tests passed and no skips. Its sole failing scenario was
TestScanCatalogChangesCommittedFileSurvivesLaterFailureOrCancellation/Failed.
The test depended on an AFTER UPDATE trigger on a second file whose facts never
changed. The no-op guard correctly skipped that redundant UPDATE, so the injected
failure never occurred. Subsequent full and performance processes were stopped.

v2 changes only this test's Failed branch: after capturing the old database
snapshot and before starting the scan, it changes the second file from 21 to
35 bytes. The original deferred trigger now targets a real change. The Cancelled
branch, first-file committed visibility, second-file exact rollback, single
notification and Updated=1/Added=0 assertions are unchanged. No production code
was changed after v1. Complete failed source, raw output and receipts are retained.

Only this affected top-level race test and its two subtests are requalified on
v2 before full Library. The 105 previous race passes are evidence for identical
production code, not a claim that the complete race selection ran again on v2.
The selected total is eleven products: one retained v1 failure and ten v2 runs;
none of the eight performance processes is a replacement timing sample.

| Completed correctness check | Actual result |
| --- | --- |
| v1 targeted race | Overall FAIL from the obsolete fixture; 105 top-level/134 subtests passed, all 40 required, no skips |
| v2 repaired-case race | PASS: one top-level/two subtests, all three required parent/subtest names, no skips |
| v2 ordinary full Library | PASS: 1,335 top-level/2,806 subtests, all 41 required, 11 declared skips and no unexpected skips |
| Capacity test within full Library | PASS: 10,688 items/10,000 leaves, 10,001 real probes, four jobs, eight UserData witnesses and Store.Close |

The repaired-case and full-process wall times including compilation are
27.223225 and 833.601613 seconds. The capacity case reports 221.41 seconds and
its cached assertions 1.5088 seconds; these are functional observations, not
matched throughput comparisons. Overlapping correctness evidence is not summed
into a larger distinct-test total.

## Performance interpretation

The first completed v2 comparison improved eighteen of twenty-one job paired
medians. Its three adverse medians are movie cold 1.0035903333 (all three slower),
movie cached-after-incremental 1.0491757360 (all three slower), and flat-episode
cached-after-incremental 1.0458542067 (blocks 1/3 slower). The three force medians
are movie 0.9479040298, flat episodes 0.9784278394 and directory 0.9067372422;
flat-episode force still has one adverse pair. All v2 data stays independently
retained in the parent evidence/analysis directory.

Review then found that new replacement-count preparation also ran before the
known-empty-image fast return. The final v3 moves only this pure calculation
after that return and before writer admission. Types, candidate counts, mutation,
I/O, locking, notification and physical-proof behavior are unchanged. This
removes unnecessary preparation but is not a proven explanation of the cached
regressions. Existing allocation/GC observations do not establish a common cause.

v3 receives one related image/absence/source-proof race process with seventeen
required tests, plus a new fixed two-count/six-timing comparison. The completed
v2 full Library run is not replayed for this placement-only change. No v2 timing
is pooled with v3, no old sample is replaced, and no further profiling, source
contrast or timing chase is selected. Total product history is twenty processes:
one retained v1 failure, ten qualified v2 runs, and nine qualified v3 runs.

The v3 related race process passed seventeen top-level tests and fifteen subtests,
with all seventeen required tests and no skips. It took 39.55 seconds including
compilation. This final placement-only change does not claim another full-package
race or another full Library run.

Each new performance process preserves all twenty-one descriptor phases and
803 probes. TRACE1 supplies work counts only. Three same-block C/B comparisons
per group use TRACE0 without profiler or SQL timing. Persisted job time,
observed-terminal polling, retirement and process/compile wall time remain
separate. The descriptor workload has no real image-heavy load: actual-image
diff acceptance is functional and row-version evidence, not an image-throughput
claim. The earlier 3585/d993 three-pair force regressions and later instrumented
diagnosis remain separate historical evidence, not pooled baselines.

## Final v3 results and limitations

The paired C/B median for flat-episode force is 0.9527091835, a 4.7291% elapsed
reduction, with block 2 still slower by 60.110 ms. Directory force is
0.9443837306, a 5.5616% reduction, and all three pairs are faster. Directory
cached/incremental job medians fall by 17.25%-32.65%, each with all three pairs
faster. These contrasts use current baseline 8017567; they do not demonstrate
that the older regression relative to 3585 has disappeared.

Seven adverse medians remain: movie cold 1.0011805264, cached_2 1.0021886090,
incremental 1.0277818470, cached-after-incremental 1.0028183610, and force
1.0165776058; flat-episode cached_1 1.0088022568 and incremental 1.0107412021.
None is slower in all three final pairs, but every adverse sample is retained.
The v2-to-v3 difference is not used as causal proof about count-array preparation.

TRACE1 confirms force direct item-write rows 160/192/48 to zero, with the same
160/192/48 probes and accepted scan-write counts 163/195/51. Movie/flat-episode
force COMMIT counts stay 171/221; directory force falls from 83 to 71, eliminating
the twelve empty folder image transactions. Force SQL counts fall
4723 to 4563, 6010 to 5800 and 1822 to 1666, respectively. These are full-phase
direct command counts, not all trigger/function mutations or job-clipped counts.
They do not convert absent row rewrites into a claim of zero WAL or zero I/O.

An independent root audit exactly matches all sixty-three final job pairs. The
complete final table follows. All terminal-window samples remain separately in
the external performance report. The unchanged 250 ms test observer can add a
poll interval: movie incremental block 2 has only +7.547 ms of persisted job time
but about +251.236 ms in observed terminal time. The observer window is not a
direct client UI measurement, nor is it interchangeable with job duration.

| Scenario | B samples ms | C samples ms | C/B three ratios | Median [min,max] | Reverse blocks | Signed deltas ms |
| --- | --- | --- | --- | --- | --- | --- |
| flat_movies/cold | 1933.883, 1945.521, 1942.278 | 1936.166, 1973.253, 1904.736 | 1.0012, 1.0143, 0.9807 | 1.0012 [0.9807,1.0143] | [1, 2] | +2.283, +27.732, -37.542 |
| flat_movies/cached_1 | 190.762, 193.585, 197.431 | 212.130, 189.769, 195.138 | 1.1120, 0.9803, 0.9884 | 0.9884 [0.9803,1.1120] | [1] | +21.368, -3.816, -2.293 |
| flat_movies/cached_2 | 195.558, 198.645, 201.991 | 195.986, 205.301, 195.623 | 1.0022, 1.0335, 0.9685 | 1.0022 [0.9685,1.0335] | [1, 2] | +0.428, +6.656, -6.368 |
| flat_movies/task_owned_cached | 190.571, 193.073, 196.566 | 189.075, 184.991, 188.574 | 0.9921, 0.9581, 0.9593 | 0.9593 [0.9581,0.9921] | [] | -1.496, -8.082, -7.992 |
| flat_movies/incremental | 232.094, 244.070, 235.377 | 238.542, 251.617, 233.799 | 1.0278, 1.0309, 0.9933 | 1.0278 [0.9933,1.0309] | [1, 2] | +6.448, +7.547, -1.578 |
| flat_movies/cached_after_incremental | 206.503, 192.302, 201.504 | 207.085, 197.379, 192.766 | 1.0028, 1.0264, 0.9566 | 1.0028 [0.9566,1.0264] | [1, 2] | +0.582, +5.077, -8.738 |
| flat_movies/force_probe | 1823.226, 1801.163, 1823.846 | 1876.687, 1775.310, 1854.081 | 1.0293, 0.9856, 1.0166 | 1.0166 [0.9856,1.0293] | [1, 3] | +53.461, -25.853, +30.235 |
| flat_episodes/cold | 2434.821, 2383.624, 2465.541 | 2426.426, 2422.022, 2435.746 | 0.9966, 1.0161, 0.9879 | 0.9966 [0.9879,1.0161] | [2] | -8.395, +38.398, -29.795 |
| flat_episodes/cached_1 | 330.638, 333.210, 344.927 | 351.568, 336.143, 332.689 | 1.0633, 1.0088, 0.9645 | 1.0088 [0.9645,1.0633] | [1, 2] | +20.930, +2.933, -12.238 |
| flat_episodes/cached_2 | 353.757, 353.609, 335.479 | 332.385, 330.363, 341.923 | 0.9396, 0.9343, 1.0192 | 0.9396 [0.9343,1.0192] | [3] | -21.372, -23.246, +6.444 |
| flat_episodes/task_owned_cached | 326.677, 346.344, 342.218 | 347.999, 343.848, 336.797 | 1.0653, 0.9928, 0.9842 | 0.9928 [0.9842,1.0653] | [1] | +21.322, -2.496, -5.421 |
| flat_episodes/incremental | 386.401, 387.444, 371.653 | 394.365, 383.175, 375.645 | 1.0206, 0.9890, 1.0107 | 1.0107 [0.9890,1.0206] | [1, 3] | +7.964, -4.269, +3.992 |
| flat_episodes/cached_after_incremental | 358.107, 331.409, 346.730 | 354.391, 323.078, 360.373 | 0.9896, 0.9749, 1.0393 | 0.9896 [0.9749,1.0393] | [3] | -3.716, -8.331, +13.643 |
| flat_episodes/force_probe | 2349.970, 2227.643, 2409.045 | 2238.838, 2287.753, 2200.307 | 0.9527, 1.0270, 0.9134 | 0.9527 [0.9134,1.0270] | [2] | -111.132, +60.110, -208.738 |
| directory_episodes/cold | 704.183, 745.115, 761.612 | 721.736, 667.503, 679.488 | 1.0249, 0.8958, 0.8922 | 0.8958 [0.8922,1.0249] | [1] | +17.553, -77.612, -82.124 |
| directory_episodes/cached_1 | 218.870, 207.678, 219.408 | 173.182, 169.147, 168.915 | 0.7913, 0.8145, 0.7699 | 0.7913 [0.7699,0.8145] | [] | -45.688, -38.531, -50.493 |
| directory_episodes/cached_2 | 201.066, 220.174, 220.295 | 181.125, 160.888, 166.219 | 0.9008, 0.7307, 0.7545 | 0.7545 [0.7307,0.9008] | [] | -19.941, -59.286, -54.076 |
| directory_episodes/task_owned_cached | 262.121, 214.924, 265.449 | 176.526, 182.131, 166.321 | 0.6735, 0.8474, 0.6266 | 0.6735 [0.6266,0.8474] | [] | -85.595, -32.793, -99.128 |
| directory_episodes/incremental | 254.373, 247.727, 255.554 | 210.494, 229.436, 206.107 | 0.8275, 0.9262, 0.8065 | 0.8275 [0.8065,0.9262] | [] | -43.879, -18.291, -49.447 |
| directory_episodes/cached_after_incremental | 220.633, 206.512, 202.674 | 171.616, 172.461, 165.176 | 0.7778, 0.8351, 0.8150 | 0.8150 [0.7778,0.8351] | [] | -49.017, -34.051, -37.498 |
| directory_episodes/force_probe | 690.433, 697.260, 682.148 | 653.344, 658.481, 638.410 | 0.9463, 0.9444, 0.9359 | 0.9444 [0.9359,0.9463] | [] | -37.089, -38.779, -43.738 |

## Closed resources and delivery

Both batches are closed. v2 has 61 sealed current evidence files and 16 retained
v1 records; v3 has 50 sealed files. All exported hashes match. Every selected
invocation exited, including the retained v1 failure. Final manifests match the
frozen sources. Owned empty scratch and ext4 fixture TMPDIR were removed; sources,
archives, raw results, shared caches and old environments remain. PostgreSQL/Goby
identities and reserve 403,374 are unchanged. Final v3 persistent availability is
4,945,543,168 bytes. No local product verification or database setting change ran.

Delivery includes the two source commits and this report/handoff only. The
separate publication receipt records final main/origin identity and preservation
of the original 199 WIP byte states and 56 historical hashes. Original scan.go WIP
overlaps this delivery and is preserved separately from accepted main code.
No additional measurement or optimization is selected by this handoff.

Evidence root: `D:/Code/goby/.artifacts/scan-transaction-trimming-20261005`.
Initial frozen source SHA-256:
bee113fbadef7586d7958fb746a563133abdc077a1929cd65aca7e1ce4de606d.
Baseline archive:
b9d1296181ff35b6067dea79dbb56dccda97db7cc4afd41366771bc22d06202c.
Candidate v1 archive:
c32f79219c5eba33f8be720e0c02d5e5dccc3a83ab5b09ed321653788c1cd118.
Final source-freeze-v2.json SHA-256:
00122856683dd47eb1f7fd6a7592454d28c957995ff146019ab51f72a3f98449.
Candidate v2 archive:
537dde2651e929e891c07d9cb703fbcd30e705e11007ae78f6f0417937c2a7ae.
Final v3 source freeze:
a2bde269fa696274b9323dddc4c7cb384600f8c45d2e78844a70dbb100a8eac8.
Candidate v3 archive:
875a6088cfbd75f3b3c147623099eeb60423f8b5ba75e0ff0bc54b59f4d04fdd.


| Record | SHA-256 |
| --- | --- |
| source-freeze-v3.json | a2bde269fa696274b9323dddc4c7cb384600f8c45d2e78844a70dbb100a8eac8 |
| source-commit-receipt-v3.json | d7475c002e129644f8ccea3ea2114b39c1385fc04422bf2a03459aca349136db |
| evidence/correctness-v2-receipt.json | 69c0b1c0d75f823307fdf12bf45a6af9c3d4e269ce5d2206ba6f71f1f7ebe159 |
| prior-attempts-retention.json | b0eff6dbe8e9a2ef20904ba79307709e954b2e3ad5ef8a78cbee88ff0f5aae33 |
| analysis/summary.json | 22929e377e069959e50cbc1d368403d396e1377caddd497e8260f2305c31e431 |
| v3/evidence/correctness-receipt.json | c495732cf38ed907632f97af8b12970b9e3ddbb72ab14619a4fb4dfdb8eeb8be |
| v3/analysis/summary.json | a3257f47254223d6c054332cb16c7f7d0ba56de55a4c6065f705acbd0a429b9c |
| v3/analysis/root-paired-audit.json | c6a09242e44c4709b101a1107cefc25081ac61932d0d4eecae4bfa0967bfc14f |
| v3/evidence/closure-receipt.json | 56d23ccbd0f83dcec63ea5bf3a5a05f929b921c078229451caf788351c3096cf |
| v3/final-export-verification.json | 06321b68b9fd51ffe91a05c10646218a558cf86a9810f59d1d565c14c0c5c4f7 |
