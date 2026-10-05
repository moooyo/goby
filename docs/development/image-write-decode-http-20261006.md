# Image row sets, validated decode reuse and GET transactions (2026-10-06)

## Current status

Normal 1/32 Stop response paired medians are 1.151006/1.096821, with slower deltas up to +5.393277 ms; the largest lifetime/output-lease observation increase is +7.465010 ms. All selected Stop/freshness/ownership assertions pass, but that does not erase the negative timing outcomes. No independent latency SLO was selected. The user selected (1) per-item image UPSERT row sets, (2) scan-local validated image Info reuse after a fresh complete read/hash, and (3) GET transaction assessment/simplification with current external semantics preserved. Step 1 is accepted and committed as `109b1fe52e4411749340b9bedd5bd0cb2eb216ad`, parent `6250d4345f24cef56672adb24607303dceaa0008`. Step 2 is accepted and committed as `050c62b9deb37baab26a7c3dbe374f82172cc132` after Step 1; all twelve fixed products qualify. Step 3 is accepted: eleven fixed products qualify without product replay, bound to source commit `26eab41377f0b2519c229308a9cf184bc058c09e`. All eighteen GET pairs improve, but normal 1/32 Stop response/lifetime windows retain explicit regressions. The feature now has integration merge `8e5b5f44e69122ba724678d5696a640e26e0b55d` with new main6683; its three-invocation integration correctness verification and actual temporary-resource closure are accepted. All 35 unique selected Go products qualify (32 original stage products plus three integration races), with no rerun for the retained parser repair. Performance is not measured on that integrated revision.

## Step 1: accepted per-item image row sets

Nine selected invocations qualify: targeted race passes 25 top-level tests and 18 subtests with zero skips, two independent TRACE1 count runs complete, and six TRACE0 timing runs form B-C / C-B / B-C pairs. Exactly `images_scan.go` and its new integration test enter the source commit. No cache or HTTP change is included in this evidence.

The corpus has 128 generated H.264 videos, 32 FLAC tracks and 288 PNG files, initially producing 672 image rows. Real probe counts, affected rows, committed key/xmin changes and public image metadata remain correct in all five phases. Store.Close joins after every complete profile.

| Phase | Raw SQL B/C | BEGIN=COMMIT B/C | Image UPSERT commands B/C | Real probes B/C |
|---|---:|---:|---:|---:|
| cold | 7301/6765 | 382/382 | 672/136 | 160/160 |
| warm | 2426/1895 | 193/194 | 672/136 | 0/0 |
| force | 6826/6290 | 382/382 | 672/136 | 160/160 |
| image_changed | 2429/1898 | 193/194 | 672/136 | 0/0 |
| image_removed | 2428/1898 | 193/194 | 671/136 | 0/0 |

These N=1 traced pairs are counts, not timing estimates. Warm/change/remove retain one additional COMMIT in C; cold/force do not reduce COMMIT count. Raw SQL reductions are 536/531/536/531/530. Batched row sets reduce round trips, not every transaction or allocation. Removing one image source still shifts three backdrop indexes; those are not three changed source files.

| Phase | Job C/B median [min,max] | Adverse job block | Observed-terminal median | Adverse terminal blocks |
|---|---:|---|---:|---|
| cold | 0.881641 [0.686132,1.348121] | 3: +1570.249 ms | 0.894560 | 3 |
| warm | 0.930705 [0.790007,1.508531] | 3: +836.616 ms | 1.000396 | 2, 3 |
| force | 0.992290 [0.839324,1.447147] | 3: +1680.743 ms | 1.000247 | 2, 3 |
| image_changed | 0.925745 [0.813078,1.083256] | 3: +206.165 ms | 0.859055 | 3 |
| image_removed | 0.925976 [0.858511,0.931062] | None | 0.909721 | None |

All five persisted-job medians are lower, but substantial block-3 adverse samples remain. Warm/force terminal medians are slightly higher; job, observed terminal and worker retirement are distinct windows. Allocation medians remain near one, and malloc/GC/pool observations retain reverse samples in the detailed summary. Neither the common slow block nor the older force/GET p99 regressions is assigned a host/code cause. Three samples do not establish tail latency; fewer SQL calls do not imply proportional allocation or end-to-end benefit.

## Step 2: accepted scan-local validated Info reuse

B2 is Step 1 production109b plus the common real-image corpus driver; C2 adds four cache production files and three tests. All twelve selected products qualify with zero skips: artwork race 58 top-level/152 subtests, library race 26 top-level/18 subtests, two diverse TRACE1 counts, six diverse TRACE0 timing runs and one separate shared B2/C2 pair. Sixty-three exported evidence files match; complete B2/C2 archive manifests have 7558/7562 members. They are variants of one owner-managed switching directory, not two simultaneous live trees.

These are incremental C2/B2 results against Step 1 production. Step 1 and Step 2 ratios are not multiplied into a claimed final speedup versus main6250. Diverse/shared modes and TRACE1/TRACE0 remain separate.

| Diverse phase | Job C2/B2 median [min,max] | Terminal median | Allocation-byte median ratio | GC-count median ratio |
|---|---:|---:|---:|---:|
| `cold` | 0.931327 [0.858177,1.021575] | 0.914368 | 0.615830 | 0.644951 |
| `warm` | 0.806856 [0.779626,1.111650] | 0.803126 | 0.423299 | 0.439716 |
| `force` | 0.974505 [0.925181,0.986123] | 0.952717 | 0.615989 | 0.655797 |
| `image_changed` | 0.897121 [0.827305,1.215718] | 0.901886 | 0.423312 | 0.422535 |
| `image_removed` | 0.822843 [0.821911,1.178223] | 0.798546 | 0.421677 | 0.418440 |

All fifteen diverse phase-pairs reduce process allocation and GC count, saving about 379-382 MB per measured phase. Job adverse samples remain cold block 3 +118.492 ms, warm block 1 +262.445 ms, changed block 1 +354.659 ms and removed block 1 +283.156 ms. Force jobs improve in all three blocks, but force terminal block 1 remains +2.504241 ms. These samples are not dismissed as noise.

| Mode / phase | Adverse metric | Slower blocks and signed deltas |
|---|---|---|
| diverse n=3 / `cold` | `job_elapsed_ns` | block 3: +118.492000 ms |
| diverse n=3 / `cold` | `worker_retirement_wait_ns` | block 3: +4.680000 us |
| diverse n=3 / `cold` | `pool_acquire_duration_ns` | block 2: +1.159443 ms; block 3: +0.354915 ms |
| diverse n=3 / `cold` | `pool_empty_acquire_wait_ns` | block 2: +1.146274 ms; block 3: +0.357162 ms |
| diverse n=3 / `warm` | `job_elapsed_ns` | block 1: +262.445000 ms |
| diverse n=3 / `warm` | `observed_terminal_elapsed_ns` | block 1: +264.581716 ms |
| diverse n=3 / `warm` | `worker_retirement_wait_ns` | block 1: +0.381000 us; block 3: +1.170000 us |
| diverse n=3 / `warm` | `pool_acquire_count` | block 1: +1.000000 count |
| diverse n=3 / `warm` | `pool_acquire_duration_ns` | block 1: +2.628113 ms |
| diverse n=3 / `warm` | `pool_empty_acquire_count` | block 1: +1.000000 count |
| diverse n=3 / `warm` | `pool_empty_acquire_wait_ns` | block 1: +2.626515 ms |
| diverse n=3 / `force` | `observed_terminal_elapsed_ns` | block 1: +2.504241 ms |
| diverse n=3 / `force` | `pool_acquire_duration_ns` | block 1: +0.020871 ms; block 2: +0.007599 ms |
| diverse n=3 / `image_changed` | `job_elapsed_ns` | block 1: +354.659000 ms |
| diverse n=3 / `image_changed` | `observed_terminal_elapsed_ns` | block 1: +499.707755 ms |
| diverse n=3 / `image_changed` | `worker_retirement_wait_ns` | block 1: +3.040000 us; block 2: +5.349000 us |
| diverse n=3 / `image_changed` | `pool_acquire_count` | block 1: +2.000000 count |
| diverse n=3 / `image_changed` | `pool_acquire_duration_ns` | block 1: +0.007994 ms |
| diverse n=3 / `image_removed` | `job_elapsed_ns` | block 1: +283.156000 ms |
| diverse n=3 / `image_removed` | `observed_terminal_elapsed_ns` | block 1: +248.402707 ms |
| diverse n=3 / `image_removed` | `worker_retirement_wait_ns` | block 2: +3.930000 us; block 3: +2.169000 us |
| diverse n=3 / `image_removed` | `pool_acquire_count` | block 1: +1.000000 count |
| shared n=1 / `cold` | `worker_retirement_wait_ns` | block 1: +3.189000 us |
| shared n=1 / `image_removed` | `worker_retirement_wait_ns` | block 1: +37.787000 us |

| Diverse phase | Raw SQL B2/C2 | BEGIN=COMMIT B2/C2 | UPSERT commands B2/C2 | Probes B2/C2 |
|---|---:|---:|---:|---:|
| `cold` | 6765/6765 | 382/382 | 136/136 | 160/160 |
| `warm` | 1885/1890 | 192/193 | 136/136 | 0/0 |
| `force` | 6290/6290 | 382/382 | 136/136 | 160/160 |
| `image_changed` | 1888/1893 | 192/193 | 136/136 | 0/0 |
| `image_removed` | 1888/1888 | 192/192 | 136/136 | 0/0 |

Warm/image_changed have +5 raw SQL and +1 BEGIN/COMMIT in C2, with zero rollbacks. Counted image UPSERT/DELETE/snapshot populations match; three other commands plus BEGIN/COMMIT remain outside those image categories. No per-template timing identifies the transaction family or attributes it to the cache. Image rows/xmin/probes and actual Store.Close joins remain correct.

All sources freshly read/hash each image and retain validation, source proof, cancellation, independent Info, invalid-image/GIF and final publication checks. This batch has no CPU/heap/trace profile or decode-hit/miss counter. Process allocation through terminal/worker retirement includes observer/background work; reduced allocation is not measured CPU/native savings or peak live-heap reduction.

Diverse starts with 288 unique PNG hashes, which fit the 512-entry cache, and still reselects physical generic backdrops. It does not establish eviction-heavy or fully unique-per-selection behavior. The separate shared n=1 pair starts with two hashes, with cached/edit allocation ratios about 0.058; this fixture-bias check is not pooled into formal timing or called a median.

## Step 3: accepted GET consolidation with retained Stop regressions

Eleven original products qualify: identity race 4 top-level/22 subtests, library race 36/83, server race 2/0, six fixed matched processes and two normal-32 cold/image diagnostics. Actual skips and main endpoint errors are zero. Sixty-four export files and both complete archives independently match. B3 is production050c plus identical two-file telemetry; C3 adds five production files/five behavior tests. The necessary two-line priority-driver hook change is common and declared; four legacy performance drivers remain unchanged.

All six groups have lower GET client p50/p95/p99 and complete 256-request elapsed time in all eighteen pairs. This is C3/B3 on the frozen pre-integration revision, with no multiplication of earlier stage ratios. Original per-group request arrays and all reverse outcomes remain external.

| Credential mode / callers | GET p99 C3/B3 median [min,max] | Complete GET elapsed median | Stop response median / slower blocks |
|---|---:|---:|---|
| normal / 1 | 0.711847 [0.195291,0.980248] | 0.771643 | 1.151006 / 1,3 |
| normal / 8 | 0.848409 [0.189255,0.859380] | 0.782102 | 0.950084 / 2 |
| normal / 32 | 0.746920 [0.315577,0.821337] | 0.783267 | 1.096821 / 2,3 |
| application key / 1 | 0.466206 [0.224696,0.772024] | 0.613099 | 0.877135 / 1 |
| application key / 8 | 0.292754 [0.195054,0.736388] | 0.417644 | 0.707418 / None |
| application key / 32 | 0.463637 [0.328870,0.908604] | 0.431374 | 0.725947 / None |

| Normal callers / Stop window | B3 block 1/2/3 ms | C3 block 1/2/3 ms | Signed deltas ms |
|---|---|---|---|
| 1 / TCP response through body close | 17.302899 / 17.356847 / 21.786360 | 22.696176 / 16.792180 / 25.076232 | +5.393277 / -0.564667 / +3.289872 |
| 1 / handler-drain observation upper bound | 17.332619 / 17.380926 / 21.813170 | 22.721055 / 16.815420 / 25.102780 | +5.388436 / -0.565506 / +3.289610 |
| 1 / lifetime-output-lease observation upper bound | 19.390565 / 19.237551 / 23.719173 | 25.679982 / 18.552987 / 26.901556 | +6.289417 / -0.684564 / +3.182383 |
| 32 / TCP response through body close | 17.558192 / 17.419236 / 21.504008 | 16.014839 / 21.130754 / 23.586037 | -1.543353 / +3.711518 / +2.082029 |
| 32 / handler-drain observation upper bound | 17.585052 / 17.452316 / 21.530466 | 16.050078 / 21.166153 / 23.618676 | -1.534974 / +3.713837 / +2.088210 |
| 32 / lifetime-output-lease observation upper bound | 19.188621 / 19.198413 / 22.691968 | 17.483343 / 23.107686 / 30.156978 | -1.705278 / +3.909273 / +7.465010 |

These windows overlap and are not added. Drain and lifetime/output-lease fields are observation upper bounds, not native encoder exit/reap. Normal-8 block 2 retains response +0.058858 ms/lifetime +0.643435 ms. Application-key 1 retains response block 1 +0.945848 ms and lifetime blocks 1/3 +0.937976/+0.024770 ms; key-32 lifetime block 1 is +3.894980 ms. Key-1 allocation median is slightly higher at 1.000151, with blocks 1/2 +23,576/+138,984 bytes; key-32 allocation/malloc block 1 also remains adverse. Three Stop samples do not establish stable tails.

Normal-1 successful endings remain 512: 512 COMMITs become 512 acknowledged ROLLBACKs. Key-1 retains 512 COMMITs plus 512 ROLLBACKs instead of 1024 COMMITs. Other queue/refreshed-stage endings remain explicit, including normal-32 block 1's additional ending. The rollback optimization applies only to actual READ COMMITTED transactions; other isolation levels retain COMMIT. A literal COMMIT reduction does not mean transactions disappeared; successful read-fence rollback replacements are not product failures. Endpoint errors, fresh two-stage validation, payload, Stop ownership and actual resource-completion assertions remain enforced.

The cold normal-32 N=1 diagnostic has 7529 fresh-authority COMMIT endpoints in B3 and 10021 fresh-authority ROLLBACK endpoints in C3, reflecting different open-ended GET-wave counts. The role includes source/play checks, not isolated permission time. Sparse active matching-command samples find B3 WAL waits but only one short C3 rollback sample without a wait label; this cannot establish zero WAL or exact wait duration. PG-global counters and overlapping endpoint/phase sums do not assign the older p99 cause.

Qualification repair is retained honestly: the first B3 GET Go process exited successfully, but validator v1 mishandled the extra seventh cached_fixture_close marker and did not merge common flags into the per-case environment. Its original false postguard/runner-exit-1 receipt and helper remain in retained-parser-v1 evidence. Helper v2 requalified the same immutable raw input into six groups plus closure, with an appended repair record. No product was rerun, replaced or added; this is a parser correction, not a product regression.

## New-main integration: accepted separate correctness scope

Main advanced independently from 6250 to `6683ce7dba47a9a60a2ce8bbd228f1980f40bbe7`: seven commits and 460 net changed paths, with no overlap against the 21 selected source paths or these docs. Measured source history is preserved as 109b -> 050c -> 26eab; integration 8e5b has exactly ordered parents [26eab,6683]. All 21 measured source file contents remain unchanged, and main6683 -> integration has exactly those 21 net paths.

The complete Git integration archive has 43,880,756 compressed bytes, 221,419,520 tar bytes and 7,919 regular files containing 215,191,537 logical bytes; these are not filesystem allocation. The actual combined tree passes exactly three focused race invocations: identity 4 top-level/22 subtests, library 64/101 (62 image/GET tests plus two existing new-main media-revalidation cases), and server 2/0. The total is 70 top-level tests, 123 subtests and 70 required tests, with no missing requirements, failures or unexpected skips. Source/services/fixtures remain conserved. This is scoped integration correctness with no additional performance matrix.

All 32 original product results above remain bound to their original stage snapshots/revisions. New main's bitmap scanning (`scanBitmapSubtitlesAttempt`) adds an EXISTS SELECT even without a candidate; thus pre-merge absolute SQL or elapsed times are not integrated8e5b measurements. Static no-overlap/source preservation and the passed focused races do not establish integrated performance.

## Source recoverability and capacity state

Baseline Go/tests/module bytes map to main6250 through retained Step 3 D source; its two later documentation differences are declared rather than silently overwritten. Step 1 retains its complete candidate archive, all 7558 regular members verified against the manifest, and the committed two-file overlay. Forty-seven completed Step 1 evidence files match; the environment was retained at that stage; the actual final closure is recorded below.

Step 2 maps back to the recoverable Step 1 candidate, then adds common driver/cache overlays and retained complete B2/C2 archives/manifests. Step 3 B3/C3 builds on complete C2 with exact difference overlays; old immutable baseline documentation is retained with its declared main6250 differences.

| Completed source-retention group | Exact expanded source directories | Selected allocated bytes before reclamation | Persistent available before/after |
|---|---:|---:|---:|
| First | 13 | 2,890,153,984 | 1,616,592,896 / 4,065,529,856 |
| Second | 16 | 3,057,315,840 | 2,258,087,936 / 5,315,403,776 |

Only these 29 selected expanded source directories were reclaimed after full archive/member verification and per-source restore mapping. Sources are recoverable from complete local archives and manifests; this is an explicit retention exception, not a claim that every old expanded tree stayed remote. `source-retention-receipt.json` and `source-retention-round2/source-retention-receipt.json` map every removed path to its archive/manifest and exact restoration destination. Excluded reusable baseline, retained evidence/closures, shared caches/services and old root directories remain protected. The selected allocated-byte totals are not the entire df delta: other concurrent allocations can change persistent availability. No capacity admission stop is a successful product run. Actual final temporary-resource closure is accepted below; recoverable source and evidence remain retained.

## Evidence and publication plan

Detail evidence is external under `D:/Code/goby/.artifacts/image-write-decode-http-20261006`. Step 1 analysis: `analysis/performance-report.md` SHA256 `c6e0fc4eadc5a7f42c884364c5dec2a4d0bd9fc7e28b045ef4d13ffe86fa705e`; `analysis/summary.json` SHA256 `3131593591c365a1a9064e717011144e6bd319fac054f9efaee22251296c7cd0`. Raw paired values, all resource/adverse samples and immutable references remain there; old n=3 batches are not pooled.

Step 2 detail: `step2/analysis/performance-report.md` SHA256 `11540aa1d43dcb984b6d0ee0c6473f1e852abb2827f4f28235c572a335f414a5`; `step2/analysis/summary.json` SHA256 `02821f04ceeb547925c835aa0a7765339e3cfd31ca948b0dc2361636c76f438d`.

Step 3 detail: `step3/analysis/performance-report.md` SHA256 `93f28abc0c53f7cd87a84302950bed76dc5a6892bb45480fdd3cb47b20ebce1b`; `step3/analysis/summary.json` SHA256 `b2ba68ad394fa2e066f0ccdc24acf19507bb082eefc25ed5906a329dd8fc6e7d`.

The final two-doc commit directly follows accepted integration 8e5b. Publication uses ForkBase 6250 for the preserved measured chain, ExpectedOldHead 6683 for the main snapshot, explicit integration parents and main 6683-to-integration 21 net source scope. Fresh main/origin checks and all 199 original WIP byte states/56 historical identities remain protected; original WIP is not imported. No local product verification is run. No additional experiment or cleanup is implied.

## Actual final temporary-resource closure

`evidence/closure-receipt.json` is `qualified_and_closed`; `final-export-verification.json` is `all_products_exported_and_owned_temporaries_closed`. All 35 unique products have exited and their export is complete. The original B3 parser-v1 false receipt remains beside same-raw/same-process requalification; it did not create another product run. Only this task's owned Go scratch and empty fixture TMP paths were removed and are absent. Current merged source, all version/full integration archives, raw/derived/repair evidence, prior receipts, database/binaries, shared caches, PG/Goby/Docker services and the 403374-block (1%) reserve are retained. Final persistent availability is 4,201,009,152 bytes.

Closure SHA256: `68c2356472cdcc4099be293e6e4dc001930b25ca72df23ac90e1f2aaff268993`; final export verification SHA256: `b39e0696fb4bb04175c2f756fb59ed58db1a116142c53e11b03697e119843cca`. Exact source/archive/member mapping and the accepted integration receipts remain under `integration/`. The subsequent publication receipt records final docs/main/origin identities and preserved WIP without a self-referential document SHA.
