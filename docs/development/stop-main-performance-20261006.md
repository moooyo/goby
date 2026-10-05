# Stop handling and current-main performance, 2026-10-06

Stage1's combined-green races and six formal products are qualified. Normal8/32
Stop responses improved in all three pairs, but normal1 remains slower by a
paired median +5.1337% (+0.862019ms). GET p99 medians regress for normal32, key1
and key8; key8's lifecycle observation bound rises +8.8105% despite its faster
Stop response. This is a mixed outcome, not a uniform speedup or a claim that all
Stop regressions are resolved. The user's second selected step, the cumulative
comparison with historical main6683, completed all eleven selected corrected
products. Diverse image scans have lower job medians in all five phases, with
one slower cold block. Cumulative GET p99 is lower in five groups but higher for
key8 (+10.2928%). That GET comparison is noncontemporary and unpaired; it does
not turn Stage1's negative paired observations into resolved regressions.

## Source identities and scope

| Stage | Runtime source | Common test inputs | Status |
| --- | --- | --- | --- |
| Original diagnostics | d467 current runtime; Bdiag restores four production files from050c | Four identical diagnostic files | Exactly two products completed; Cdiag has an actual HTTP500 failure |
| Deterministic red | Original Cdiag plus the v2 cancellation regression | Original diagnostic files | Expected legacy HTTP500/seeker failure reproduced |
| Combined green | main0cf plus three production fixes and two new tests | Four diagnostic files | Both selected race products qualified; 19 top-level and 28 subtest PASS, zero skip |
| Formal Stop | main0cf baseline versus candidate d9cc | Six identical formal files | All six BC/CB/BC products qualified |
| Cumulative comparison | Historical main6683 versus candidate d9cc | The same six formal files | All eleven corrected products qualified; final numbers independently reproduced |

Candidate `d9cc07e1a62b3b44f18f952d00b7c7304e3b25aa` is a direct child of
`0cf4550c9433f583c1bb2273078868b15a2555b0`. It contains the accepted nine-path
change. New main's external bitmap playback validation is preserved. The three
production changes address cancellation during cached HLS response handling,
same-account policy parsing reuse and the existing userdata initialization/lock
batch. Authorization, time, ownership, error-order and business commit checks
remain. This combined candidate cannot isolate each change's timing contribution.

Historical baseline is `6683ce7dba47a9a60a2ce8bbd228f1980f40bbe7`; it predates
later image/cache/HTTP and external bitmap playback changes. Its comparison is
cumulative revision evidence, not isolated attribution or a claim of equivalent
feature behavior. Original diagnostics are measurements of d467, not main0cf.

## Completed diagnostic and red evidence

The fixed diagnostic order was Bdiag then Cdiag, with no product rerun. Bdiag's
original Go PASS was requalified from the same immutable raw after clock-model
parser repair; the original false receipt and intermediate parser result remain.
Cdiag exited one after normal1 passed and the normal8 cancellation burst received
HTTP500. Source, service, fixture and process guards remained satisfied. The
diagnostic pair is recorded as partial with a product failure, not fully qualified.

The deterministic red used one non-race invocation and the accepted v2 Header
barrier regression. It reproduced precisely the old HTTP500/plain seeker failure;
its expected-red qualification is distinct from a product PASS. Legacy GET/HEAD
Range bytes0-31 assertions share the same fixture. This confirms the old response
failure under controlled cancellation. The original Cdiag500 did not capture a
response body, so the deterministic red does not establish that original500's
first cause. It also does not prove that cancellation alone caused the earlier
Stop timing regressions.

Stage2's first historical GET attempt failed at driver entry because the runner
set variant `historical6683` instead of accepted `baseline`. No observations were
produced. The exit-one raw and receipt remain; root authorized a corrected new
attempt with a distinct runID. The runtime source is still historical6683 and the
comparison remains unpaired. Only qualified new runIDs enter the final samples;
this configuration failure is not erased or counted as performance evidence.

Across this task, 23 actual Go invocations comprise two diagnostics, one expected
red, two green races, six formal Stop runs, one configuration failure and eleven
corrected Stage2 runs. Twenty exited with Go PASS; the other three are the actual
Cdiag failure, expected red failure and retained configuration failure. No
performance sample was added to replace an adverse result. Corrected Stage2's
receipt is `evidence/stage2-v2-receipt.json`, SHA256
`f84fa48d790cfaa2677419faec24a88728b5cb53f34feec5f90ad071a8120453`.

## Selected comparisons and interpretation

Stage1 completed two combined-green race invocations and six formal GET/Stop
products in BC/CB/BC blocks across all six existing groups. Library race passed
12 top-level/26 subtests and server race passed 7 top-level/2 subtests; required
names are present, failures and skips are empty. Green's production
and new-test bytes match the candidate. The formal driver differs only in bounded
failure observability: sample ordinal, body prefix and URL redaction. No green
replay is selected for this test-only change.

| Group | Stop response median change | Median delta ms | Slower Stop blocks | GET p99 median change | Slower p99 blocks | Lifetime bound median change |
| --- | ---: | ---: | --- | ---: | --- | ---: |
| normal1 | +5.133688% | +0.862019 | 1, 3 | -5.553415% | None | +5.795100% |
| normal8 | -20.845764% | -2.932141 | None | -2.980866% | 3 | -20.369487% |
| normal32 | -21.514461% | -4.410244 | None | +2.102869% | 2, 3 | -21.015770% |
| key1 | -11.159654% | -1.700790 | 3 | +1.451840% | 1, 3 | -11.039712% |
| key8 | -27.780585% | -7.039593 | 1 | +11.371249% | 1, 3 | +8.810521% |
| key32 | -36.365904% | -9.530192 | 3 | -13.248304% | 1 | -20.483817% |

These are medians of the three within-block relative changes and signed
differences, not differences of source medians. Stop response, handler drain and
lifetime/output-lease completion are separate windows. Lifetime is an observed
upper bound, not encoder-reap latency. Key8 lifetime is slower in blocks1/3,
with median +1.872924ms; its Stop response improvement cannot replace that result.
Normal1's Stop ratios range 0.602630-1.338702, and the normal8/32 improvement does
not erase its two slower blocks. Three runs do not establish stable tail latency;
GET p99 is a per-run empirical percentile, not a pooled cross-run p99.

GET allocated-byte and malloc medians also rise in all six groups, respectively
+0.1257-0.2911% and +0.2912-0.5375%. Full per-run values, all reverse samples,
GET p50/p95/elapsed and drain windows remain in the external formal summary.
Normal Stop retains 24 SQL statements/one COMMIT; C uses 22 direct statements and
two batched statements. Key Stop retains 31 SQL statements/three COMMITs and does
not use the A/B userdata batch. These statement counts do not establish the cause
of response-time differences or imply that authorization checks were removed.

`analysis/formal-stop-negative-decomposition.md` and its JSON retain the existing
normal1 adverse-block decomposition. Most measured handler increase lies within
client DB spans. B's two userdata spans versus C's whole batch differ by only
+0.023242ms in block1 and -0.215945ms in block3; that batch is not the main measured
increase. Existing token/item spans account for much of it. Client spans are not
pure SQL execution or row-lock time, and PrepareTracer does not cover batch
cache-miss description work. Nested preparation intervals are not added again
to the DB union; these observations do not establish a PostgreSQL or host cause.

Stage2 completed three historical-baseline GET products, two image TRACE1 count
products B/C, and six image TRACE0 products in BC/CB/BC order. Candidate GET uses
the three samples already collected in Stage1; that cumulative GET comparison is
explicitly unpaired. TRACE1 counts do not substitute for TRACE0 job timing. No
ratios are multiplied across stages and no diagnostic N=1 replaces formal samples.

### Historical6683 cumulative GET: noncontemporary, unpaired

The three baseline runs were collected later than the three reused d9cc runs.
Every value in this table is a ratio or difference of independent source medians;
there are no contemporary paired ratios or reverse-block claims in this design.

| Group | Design | GET p99 C/B | GET p99 median change | GET p99 delta ms | Stop response C/B | Lifetime bound C/B |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| normal1 | Noncontemporary, unpaired | 0.736002 | -26.3998% | -4.224638 | 1.007141 | 1.008134 |
| normal8 | Noncontemporary, unpaired | 0.789997 | -21.0003% | -4.669866 | 0.941828 | 0.959263 |
| normal32 | Noncontemporary, unpaired | 0.793848 | -20.6152% | -15.189184 | 0.742233 | 0.624602 |
| key1 | Noncontemporary, unpaired | 0.852417 | -14.7583% | -2.856721 | 0.910928 | 0.926680 |
| key8 | Noncontemporary, unpaired | 1.102928 | +10.2928% | +2.545012 | 0.964791 | 0.969692 |
| key32 | Noncontemporary, unpaired | 0.899106 | -10.0894% | -9.337351 | 0.849756 | 1.209687 |

Cumulative normal1 Stop is +0.125168ms and key32 lifetime is +5.756723ms;
the latter remains an observed output-lease-completion upper bound. Normal GET
allocated bytes/mallocs fall, while all three key groups rise in both measures.
Independent source medians and collection times do not isolate host drift,
individual optimizations or feature changes. Full request and run distributions
remain in the external cumulative GET summary.

### Historical6683 diverse real-image scans: three paired blocks

The fixed corpus contains 128 real videos, 32 real audio items and 288 distinct PNG
contents. Image tuples, probe counts, committed key/xmin differences and fixture
identities were checked in both sources. The baseline and candidate share the
same diverse driver; the inspection cache remains bounded at 512 digests.

| Phase | Job paired median change | Median delta ms | Job C/B range | Slower job blocks | Allocated bytes median change |
| --- | ---: | ---: | --- | --- | ---: |
| cold | -10.333266% | -408.126 | 0.879891-1.130076 | 1 | -38.419501% |
| warm | -27.983205% | -459.740 | 0.700772-0.729622 | None | -57.744432% |
| force | -11.749714% | -428.284 | 0.629232-0.889227 | None | -38.425080% |
| image_changed | -30.357335% | -492.774 | 0.431048-0.696929 | None | -57.776764% |
| image_removed | -31.494205% | -522.224 | 0.479168-0.688296 | None | -57.795840% |

Cold block1 is +13.0076%/+519.323ms slower for persisted job time, and its
observed terminal window is +18.9839%/+760.173899ms. The image_removed retirement
wait is slower in all three blocks, median +72.3614%/+0.006671ms; it is a separate
small Store-worker retirement window. Other retirement/pool-acquire reverse
samples remain in the external table. Process allocation deltas include observer
and background work through retirement; they do not measure CPU or peak heap.
Job, terminal polling and retirement windows are not substituted for each other.

The two separate TRACE1 products provide command counts, not latency samples:

| Phase | Raw SQL B to C | BEGIN/COMMIT each B to C | Image UPSERT commands B to C | Image rows B=C | Probe calls B=C |
| --- | --- | --- | --- | ---: | ---: |
| cold | 7493 to 6957 | 382 to 382 | 672 to 136 | 672 | 160 |
| warm | 2586 to 2045 | 193 to 192 | 672 to 136 | 672 | 0 |
| force | 7018 to 6482 | 382 to 382 | 672 to 136 | 672 | 160 |
| image_changed | 2589 to 2048 | 193 to 192 | 672 to 136 | 672 | 0 |
| image_removed | 2588 to 2048 | 193 to 192 | 671 to 136 | 671 | 0 |

Both sources issue 136 image DELETE commands per phase, affecting only one row
in image_removed and zero otherwise. UPSERT affected rows are672/0/0/1/3, and
committed changed-xmin counts are0/0/0/1/3, with the same key/row outcomes in both
sources. Raw rollback count is zero in both count products. Command-tag affected
rows and tuple xmin are separate measures; changed xmin does not by itself imply
different public content. The old AUTH classifier is partial for current SQL,
so its zero classification is not a claim of zero authorization work. Counts
include admission/tail and cannot be compared as job-clipped transaction timing.
Warm, changed and removed total COMMIT counts each fall by one; that reduction
cannot be attributed to image batching or this task's three production fixes.

Earlier accepted normal Stop
negative samples of about +2-5ms response time and a maximum +7.465ms lifecycle
observation bound remain historical facts, not proof of this candidate's behavior.
The earlier n=3 image-force +0.55% and GET p99 +1.3% observations remain unresolved;
a new faster sample will not establish that their old cause was repaired.
Further diagnosis of retained normal1 Stop and key8 GET negatives requires a
separate selection. No new profiling, authorization removal or performance batch
is implied by this report.

## Evidence and resource retention

Evidence stays under `.artifacts/stop-main-performance-20261006`. The 23 actual
raw/run exports and seven Go-overlay mappings were verified locally after export.
Stage1 detail is `analysis/formal-stop-summary.md` (SHA256
`b1a93f5f5925a88e919f7e2ce03f16fb117cbccb3d0bd9ceb1373c57711175cb`)
and its complete JSON (`942e67ccdc6cd8d01f8804fee1a66a554d427fd2f735ff42b8bd2ab3ffb1a50e`).
The cumulative GET MD/JSON hashes are
`2a9b54a7fa804aec93c711eda42cc520be6d15f6ff4213c99428aff6f3c7c461` /
`272d65a236cae4b4b17c1e6c1673b75d96c76a78e66669e6714c9727766dd204`;
cumulative image MD/JSON hashes are
`910364c8730fdd424d7680e94c526894acecd0297be201c426b05f8f6c7db238` /
`9a265813b687c55a46134949447966a07994f58f7cfbde8a32c77cc806c23091`.
The source packages use small backing-file Go overlays over retained read-only8e.
Historical6683 removes seven later Go/test inputs through overlay exclusions;
the physical files are retained. Both comparison sources use the exact same six
formal files, including observer v2 and the diverse real-image driver.

Closure status is `closed_with_retained_failure_attempts`. All selected workers
exited and no live compiler/test process or owned scratch/fixture reference
remained. Only the task's inactive compiler scratch and empty fixture directory
were removed. Physical source, RAM source backings, archives, raw/failed attempts,
database/media objects and ordinary shared build/module caches are retained;
no cache cleanup was performed. PG/Goby identities and reserve 403374 (1%) are
unchanged. Final persistent available capacity is 818,348,032 bytes, a snapshot
that includes concurrent activity rather than a cleanup-attribution claim.

`evidence/closure-receipt.json` SHA256 is
`d315492b24692009abc831151332b09a995f49ce174a12b23caf814494a46419`;
`final-export-verification.json` SHA256 is
`d4b9de1a6a0e873378b5a21eab4e10c6e448bd810fedaae54905dc4ce6ee6551`.
No expanded source copy or copied build/module cache was created for these
variants. Original workspace WIP is outside every source freeze and candidate
commit. Publication readback will bind the final documentation/main revision
separately from the measured source commit.
