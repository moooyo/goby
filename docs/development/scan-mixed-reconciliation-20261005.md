# Real images, mixed playback, reconciliation and regression follow-up (2026-10-05)

## Current status

Step 1 is complete: all fourteen selected invocations qualify, and all 76 sealed evidence files match the local export. The outcome is mixed. Four of five Library job paired medians are lower in C, but force is slower at median C/B 1.005458, including blocks 2 (+1453.896 ms) and 3 (+20.875 ms). Mixed scan job (1.017075), Ping (1.100435) and scan-overlap GET p99 (1.012876) also have adverse medians. Cached Stop is 0.998818 with a slower block retained. No universal speedup or cause is established.

The user selected **1: real media/images and concurrent playback verification, 3: reconciliation optimization, 2: remaining regression diagnosis**, in that order. Step 3 is complete and accepted: all seven selected invocations qualify. Step 2 completes the selected finite mechanism diagnosis; the exact cause of the earlier three-pair force and GET p99 regressions remains unassigned. All 25 selected product invocations qualify, ten offline derivation commands complete, and unified temporary-resource closure is verified. No additional optimization is implemented from the diagnostic candidates.

## Compared sources

Baseline B is `8017567beabab649e657bc204c78e23a6a73aafc`; current C is `b755e7558895d1e0277af7461bcbbfd55c0d69a2`. Both effective measurement archives apply the same three reviewed test-only LF overlays. Effective archives are committed sources plus overlays, not commit archives. The original commit/archive identities remain separate.

| Source | Original archive SHA256 | Effective archive SHA256 |
|---|---|---|
| sourceB | `b9d1296181ff35b6067dea79dbb56dccda97db7cc4afd41366771bc22d06202c` | `c393488f755fbfcdccd18f58232aa09803a9a103b4572c8d4e1a84eff9766ef1` |
| sourceC | `145d5b7a8cb577e8364b6362efcdb076e181a05db0093ec6dfac018aef736831` | `9a79de39004c7790720bcf57fa82d5c524115956a42df47d49f182f9a448e5d6` |

The common freeze is `source-freeze-v1.json`, SHA256 `6be8ac868e1fd141ef4480e12d1e0de9a5e2f46acdc774beb388c364d6076708`, under `D:/Code/goby/.artifacts/scan-mixed-reconciliation-20261005`. B/C production differences are limited to the six previously accepted transaction-trimming files. The four existing performance drivers and the server control addon/fixture hooks have identical bytes in both sources. Their historical candidate-only comments do not mean that B lacks the control hook.

| Common test overlay | SHA256 |
|---|---|
| `internal/library/scan_real_images_performance_integration_test.go` | `28653ec47d248b6aa7ead471c5d0185e5a2c920bc44f354a43f04f60cfc554c2` |
| `internal/server/hls_cold_images_performance_integration_test.go` | `0ff74210f8ce828c2af8791aa90ca8d29bdc52be96144488b1ee96c85ab06935` |
| `internal/server/hls_cold_scan_mixed_performance_integration_test.go` | `86838853876437763542387ac29706410fcf5e1e016b921a427ca3ece278e6ea` |

Only the managed checkout supplies these overlays. Original-root WIP is excluded. Step 1 adds test overlays only; the uncommitted Step 3 candidate is separate. Prior transaction-trimming measurements remain separate and are not pooled with this comparison.

## Step 1: completed Library count evidence

The bounded generated corpus contains 128 real H.264 videos, 32 FLAC tracks and 288 independent encoded PNG files in eight artwork groups. The initial accepted catalog contains 160 media items and 672 image rows. FFmpeg generates the media templates; PNGs are 320x480 posters and 640x360 backdrops. The corpus SHA256 is `e9b6a2ab7f30876672877ca7f9002b14a3918702fdf700c7e40c075e541098e9`, identical in B and C. This is a generated bounded corpus, not broad image-capacity qualification.

Each count run completes five ordered phases. Image changes/removal are prepared outside the phase timer. Public type/index/path/hash/dimension/size/root results are checked independently of SQL counts. Persisted job, terminal observation, same-job worker retirement and Store.Close windows remain separate.

| Phase | Probes B/C | Image rows B/C | Raw SQL B/C | COMMIT B/C |
|---|---:|---:|---:|---:|
| `cold` | 160/160 | 672/672 | 7479/7301 | 383/382 |
| `warm` | 0/0 | 672/672 | 2579/2426 | 194/193 |
| `force` | 160/160 | 672/672 | 7139/6826 | 383/382 |
| `image_changed` | 0/0 | 672/672 | 2581/2429 | 194/193 |
| `image_removed` | 0/0 | 671/671 | 2580/2428 | 194/193 |

| Phase | Upsert commands B/C | Upsert affected rows B/C | Delete commands B/C | Delete affected rows B/C | Changed committed xmin B/C |
|---|---:|---:|---:|---:|---:|
| `cold` | 672/672 | 672/672 | 137/136 | 0/0 | 0/0 |
| `warm` | 672/672 | 672/0 | 137/136 | 672/0 | 672/0 |
| `force` | 672/672 | 672/0 | 137/136 | 672/0 | 672/0 |
| `image_changed` | 672/672 | 672/1 | 137/136 | 672/0 | 672/1 |
| `image_removed` | 671/671 | 671/3 | 137/136 | 672/1 | 671/3 |

Current C retains 672 upsert commands in cold, warm, force and image_changed, and 671 after removal. The optimization avoids unnecessary affected rows and committed tuple rewriting; it does not eliminate all image SQL. Warm/force retain all 672 tuple identities in C. One changed poster affects one C tuple. Removing one source backdrop deletes one C row and shifts three retained backdrop indexes, so three changed tuples do not mean three changed source files.

These are TRACE1 counts with one run per source/phase, not timing evidence. Command-tag affected rows combine INSERT/UPDATE and may include rolled-back attempts; these selected runs recorded zero rollbacks. Committed key/xmin comparisons occur after measurement, and a changed xmin does not imply changed public content. Process allocations include tracer/observer work and do not establish formal timing or causality.

## Step 1: completed paired timing

The fixed sequence contains two TRACE1 Library counts, six TRACE0 Library timing runs in B-C / C-B / B-C order, then six instrumented server mixed runs in the same paired order. All six Library timing runs are qualified without resampling. The following ratios use persisted job duration; TRACE1 timings and terminal-observation duration are not substituted for that metric.

| Phase | Paired job C/B median | Minimum / maximum | Adverse job blocks: C minus B |
|---|---:|---:|---|
| `cold` | 0.859525 | 0.782027 / 0.979507 | None |
| `warm` | 0.922276 | 0.887542 / 1.425427 | block 2: +770.712 ms |
| `force` | 1.005458 | 0.907001 / 1.374532 | block 2: +1453.896 ms; block 3: +20.875 ms |
| `image_changed` | 0.956698 | 0.904369 / 1.303132 | block 2: +552.118 ms |
| `image_removed` | 0.976483 | 0.940105 / 1.494028 | block 2: +862.327 ms |

Cold is faster in all three blocks. The other four phases each retain at least one slower sample; force is slower in two of three. These observations are preserved and are not dismissed as noise. SQL/tuple reductions do not establish their time causality. Three samples do not establish tail latency. Full paired B/C values, terminal windows and resource observations remain in the external timing summary.

Terminal windows remain separate: image_changed has terminal median C/B 1.001553 and all three terminal samples are slower, despite its lower job median. Terminal observation does not override persisted job timing. The six server mixed invocations also qualify; their instrumented results remain distinct from the Library TRACE0 profile.

The server case uses normal credentials and eight cached HLS GET callers, 128 new real-media copies and 256 prefixed PNG sidecars. It completes a normal cold scan while GET continues on playback A; real HTTP Ping/Stopped target independent playback B. GET includes loopback TCP, complete body read and Body.Close with exact status/length/SHA256 checks. Preserve original request arrays and per-run overlap percentiles. Do not pool requests across runs.

The existing playback encoder is already completed. Cached Stop verifies its HTTP/lifetime/output/lease scope; it does not measure active encoder exit/reap. The case is instrumented and is neither an isolated production probe benchmark nor an alone-versus-mixed comparison. It does not qualify forced/cached scans under playback load, full existing-client audiovisual behavior, native/GPU retirement or Docker delivery.

| Mixed metric | Paired C/B median | Minimum / maximum | Adverse blocks: C minus B |
|---|---:|---:|---|
| Scan job | 1.017075 | 0.994451 / 1.034165 | block 2: +153.439 ms; block 3: +306.598 ms |
| Ping | 1.100435 | 0.919831 / 1.129060 | block 1: +2.905 ms; block 2: +2.524 ms |
| Cached Stop | 0.998818 | 0.744020 / 1.455141 | block 2: +7.186 ms |
| All GET p50 | 0.996862 | 0.989394 / 1.020679 | block 3: +0.373 ms |
| All GET p95 | 0.996700 | 0.978502 / 1.061106 | block 3: +1.402 ms |
| All GET p99 | 1.012876 | 0.950336 / 1.026413 | block 2: +0.720 ms; block 3: +0.348 ms |
| Scan-overlap GET p50 | 0.996530 | 0.989061 / 1.020649 | block 3: +0.372 ms |
| Scan-overlap GET p95 | 0.995918 | 0.979195 / 1.061106 | block 3: +1.402 ms |
| Scan-overlap GET p99 | 1.012876 | 0.950351 / 1.026413 | block 2: +0.720 ms; block 3: +0.348 ms |

The full GET sets contain 3868-3952 requests per run, with 3858-3942 scan-overlap requests; each retains eight boundary-partial requests. Percentiles are empirical within each original run, not pooled across runs. Three paired scan/Stop observations do not establish stable tails. Force allocations are lower in all three Library blocks despite two slower jobs; fewer image tuple writes do not prove proportional allocation or latency improvement. The real-image profile does not collect child RUSAGE CPU/block-I/O counters.

## Step 3: accepted directory-index and reconciliation change

One targeted race invocation passed 56 top-level tests and 65 subtests; all six fixed matched benchmark/short-proof invocations qualify. All 38 sealed-file hashes match. The eight frozen source/test paths are bound to source commit `115f23f6875feb90987eec391e041f9dd5e87dd5`, directly after b755; committed Git bytes match the measured Step 3 D overlays. This commit contains three production files and five tests, with no report, handoff or Step 2 diagnostic overlay.

`images_index.go` filters non-image extensions before building sparse maps; `subtitles_index.go` sorts only subtitle candidates while preserving the admitted-media stem set; `scan_reconciliation_commit.go` reuses expansion membership within the same owner transaction. The final sealed-state control check remains before deletion. The passed Close-after-expansion test requires rollback, unchanged catalog/no notification and actual staging-pass cleanup/join after the owner transaction rolls back. The reserved owner remains healthy; the test does not claim owner retirement.

Frozen B is b755 plus the three Step 1 tests and common directory-index benchmark; D adds exactly three production files and the reconciliation test. The short-proof profile and four original drivers retain their original bytes. Effective archives remain distinct from commit identities.

| Constructor case | D/B ns/op median [min,max] | B/op median ratio | Allocs/op median ratio | Slower samples |
|---|---:|---:|---:|---|
| `images/4096_no_sidecars` | 0.386342 [0.374784, 0.411181] | 0.000388 | 0.020270 | None |
| `images/4096_sparse_sidecars` | 0.472447 [0.462520, 0.487499] | 0.038736 | 0.606232 | None |
| `images/small` | 0.961341 [0.875333, 1.010572] | 0.811175 | 0.944000 | block 1: +271 ns/op |
| `subtitles/4096_no_sidecars` | 0.570694 [0.539692, 0.581437] | 0.205492 | 0.676692 | None |
| `subtitles/4096_sparse_sidecars` | 0.580620 [0.554434, 0.599456] | 0.264686 | 0.745968 | None |
| `subtitles/small` | 0.850568 [0.811460, 0.899011] | 0.896089 | 0.909091 | None |

All six constructor medians improve. The sole constructor reverse sample is images/small block 1, +271 ns/op. These pure in-memory constructor benchmarks exclude input preparation; they do not measure whole-scan throughput, filesystem or SQL time. Zero-baseline allocation ratios are undefined.

| Short-proof window | D/B median [min,max] | Slower samples |
|---|---:|---|
| `total_elapsed_ms` | 0.961388 [0.944609, 1.039695] | block 3: +3.467078 ms |
| `owner_transaction_ms` | 0.960403 [0.936371, 1.058307] | block 3: +4.347731 ms |

The short-proof profile removes 32 missing members, preserves 32 present members and includes 4096 ignored files with zero injected delay. Total and owner-transaction medians improve, but block 3 remains slower by 3.467078 and 4.347731 ms. Three complete root proofs retain their final owner-held stages; controlled topology is a fixture adapter while actual directory/membership/absence checks remain real. These results establish neither universal improvement nor stable tails.

## Step 2: completed finite diagnosis, historical cause unresolved

Four fixed N=1 instrumented invocations compare B801 with final D115f plus the declared common diagnostic test overlay; ten offline profile/trace derivation commands add no product invocations. These effective sources and windows differ from Step 1 C/B formal timing. The earlier n=3 force median +0.55% and overlap GET p99 +1.3%, including their slower blocks, remain retained and unassigned. A faster diagnostic pair does not show that either regression was fixed.

| Force diagnostic window | B ms | D ms | D minus B ms |
|---|---:|---:|---:|
| Persisted job | 3738.215 | 3668.607 | -69.608 |
| Observed terminal | 3754.662155 | 3753.794003 | -0.868152 |
| Job-clipped SQL interval union | 2525.090379 | 2446.687159 | -78.403220 |
| Job-clipped COMMIT union | 444.111777 | 463.296875 | +19.185098 |

SQL callbacks are clipped to persisted StartedAt/FinishedAt; overlapping intervals use union coverage. COMMIT/template/family sums and runtime-trace categories are overlapping views and are not added to job wall time. Post-FinishedAt capture tails and final transaction completion remain separate. The force worker has less non-COMMIT SQL network waiting and more COMMIT waiting in this pair; this does not establish the old regression's PostgreSQL or host cause.

The server diagnostic pair has scan job D/B 1.009019 and Ping 1.172234, while overlap GET p99 is 0.747374 and cached Stop 0.785381. Handler phases differ from client GET/body-consumption windows. Valid active-COMMIT samples actually observe IO/WalSync, IO/WalWrite and LWLock/WALWrite in fresh_authority_transaction GET spans. That role includes later source/play checks, rather than isolated permission time. Ten-millisecond sampling favors long spans: counts are not durations, causal percentages or evidence for the earlier p99 difference.

Whole-Library sampled alloc_space attributes image.Decode about 74.57%/74.42% in B/D. Whole-process profiles include setup, all phases and observer work; cumulative call-path nodes overlap, and Go CPU excludes native FFmpeg CPU. This identifies expensive existing decoding, not added decode activity or an established regression cause. The initial PNG corpus has only two payload hashes, so future reuse benefits must distinguish that fixture from varied artwork.

The bounded Step 1 C2 log window contains no selected checkpoint/autovacuum event. The checkpoint starting at 14:48:48.581 follows the run end at 14:48:47.526 in the captured log clock and is not overlap evidence. Absence in that selected log window is not proof that no PostgreSQL wait occurred.

Next candidates require a separate selection: reuse decoded image information only after a fresh complete read/hash, or batch a per-item image row-set UPSERT while retaining owner atomicity and final source proof; separately evaluate the two fresh HTTP authorization/source transactions per successful GET. None is implemented here. Do not remove fresh token/source/play checks, owner/cancel controls or durability based on these observations.

## Evidence and publication state

Count input: `analysis/library-counts-summary.json`, SHA256 `e3d0a7a0d61ddcd5a2fe0ed9551f674d141bb001cb1d60c38ab3534ce794ae20`. Its immutable run/raw/observation references and full corpus/DML fields remain outside the repository under the task artifact.

Library timing input: `analysis/library-timing-summary.json`, SHA256 `0c0860177a6e4ebed5b2e17fc1c0b58eb2ceea0496fb418ef69d65622b912929`. The complete Step 1 analysis is `analysis/performance-report.md` (SHA256 `e1fa22e230e74af31d4f6a78ff3b7b25d8fb68cb217f51aad7e6b9bfa7b0cafd`) and `analysis/summary.json` (SHA256 `c784c7abe2df3a2ca90790abf1fdfd6b1b37a1a13abfce0d97a354e054a1ae5f`). All fourteen immutable run/raw references and full request arrays remain outside the repository. The Step 1 76-file export and Step 3 38-file export matched at their retained checkpoints; the later unified closure below is a separate final state. Step 3 inputs: `step3/analysis/performance-report.md` (SHA256 `b73188fb8ebfec2a2222a218503d9fb57f08b6f2d445545da47855ea2423e5a5`) and `step3/analysis/summary.json` (SHA256 `872be83b4b4f2267f68f2c1449982058a95762529043176dff269182ee584574`). Step 2 detail: `step2/analysis/diagnostic-report.md` (SHA256 `787c32b3d8bf41b308ad32ada6e1f9503d3aecca646389cebb6675d0011e73a0`) and `step2/analysis/summary.json` (SHA256 `f268d0f4df2f20fe2b1d4c327a138fcdea2018b990c387926acd4a810ca28b40`). Source identity is bound to `115f23f6875feb90987eec391e041f9dd5e87dd5`; the publication receipt records the subsequent documentation/main/origin identities and WIP-preservation outcome without a self-referential document SHA. No local product verification is performed. The original workspace's 199 dirty byte states and 56 historical identities remain protected by the established publication process.

## Final temporary-resource closure

All 185 sealed files match the local export; all 25 product invocations and ten offline tool commands have exited. Fourteen RAM profiles were exported and verified before reclamation, and only owned empty scratch/fixture paths were removed. Six source trees, four binaries, raw/derived evidence, shared caches, PostgreSQL data and old roots remain retained. Reserved blocks remain 403374 (1%). PG PID/start 898/689 and Goby PID/start 426773/72128478 are unchanged. Final persistent availability is 3,634,929,664 bytes.

Closure: `evidence/closure-receipt.json`, SHA256 `f7e5700d9a4894003249dd4f6b1e03b7c960c01ed93b012e93efa154d03f028f`; final manifest: `evidence/final-export-manifest.json`, SHA256 `1722139bd671e38797e321cda1e3ab05e413e1df16ca8e534679d6ab8d6f86f8`. `final-export-verification.json` reports `verified_and_closed`. Retained source/evidence does not mean live owned temporary references remain.
