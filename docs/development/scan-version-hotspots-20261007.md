# Real scan versions and remaining candidates, 2026-10-07

Stage1 completes the genuine previous/current binary comparison. Media and bitmap
callbacks each improve in all ten fixed contrasts, but whole jobs improve in
only four and are slower in six. Every current2 phase is slower than pre2;
force adds 1300.342 ms. The targeted benefit does not establish stable overall
version acceleration. The source-reader candidate passed its fixed gate and is
committed. The ordinary lookup candidate also passed its fixed gate. Both are accepted
as a bounded engineering result; the large COMMIT swings remain unexplained.

## Method and source identities

Previous is `08ed21267ab647a4a49bf818ee2d2a2d19d26e39` (runtime 6386); current is
`5f992b6d8b9081fae9e3c22e9949d230fcf6a31e` (runtime e2f262d8). Each was built
once with the identical two-file test-only driver/observer. Four actual native
processes ran pre1/current1/current2/pre2, each with a fresh Store/schema and the
original five phases. No mode mutation emulates the previous binary.

All cases reuse one restored physical ext4 corpus. Close precedes restoration,
outside measurement. The removed backdrop's new inode is disclosed; directory,
media and baseline file identities/bytes otherwise agree. Actual lookup/media
traversal matches all ten contrasts: 160 ordered unique paths, 128 mp4 and 32 flac.
Item-key hashes associate source queries with same-phase audio records; random
schema keys are not compared across processes. Two builds and four native processes qualified;
all twenty phases and 44 exported files are independently size/hash matched.

There are two predetermined, correlated contrast orders per phase, not independent
N=20. Cold means fresh catalog/connections, not cold OS/disk caches. Global
CacheDescribe, 512/512 capacities and jit=off remain. Pre media/bitmap reads use
implicit CacheDescribe; current reads use explicit CacheStatement. Query options
are observed rather than changed. First uses/prepares and all adverse data remain.

## All ten current-minus-previous contrasts

Target is the recorded monotonic callback sum for media+bitmap. Query durations
already include nested Prepare; separate Prepare records are not added. Four-read
sums include lookup, media, bitmap and image-noop. These
callback sums are not CPU time or the job-clipped wall unions used below.

| Contrast | Phase | Job delta ms | Job change | Terminal delta ms | Target callback delta ms | Four-read callback delta ms | Allocation delta bytes | GC delta |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| current1-pre1 | cold | +14.534 | +0.4370% | +0.210070 | -37.846142 | -38.302718 | -300952 | -2 |
| current1-pre1 | warm | -21.782 | -5.1237% | -0.065994 | -28.752841 | -28.274400 | +399816 | +1 |
| current1-pre1 | force | -11.487 | -0.5015% | -0.745504 | -29.174289 | -27.288988 | +107296 | 0 |
| current1-pre1 | image_changed | -37.388 | -8.4883% | +0.314696 | -27.363910 | -29.916516 | -67912 | +1 |
| current1-pre1 | image_removed | -36.300 | -8.1070% | +2.427241 | -30.438169 | -34.681507 | -156296 | 0 |
| current2-pre2 | cold | +463.914 | +10.1215% | +495.225214 | -35.905547 | -31.622880 | -94400 | -1 |
| current2-pre2 | warm | +34.328 | +7.7296% | +0.921725 | -31.593750 | -36.751595 | +25096 | +1 |
| current2-pre2 | force | +1300.342 | +58.1137% | +1506.540679 | -15.313832 | +5.987777 | -126704 | -1 |
| current2-pre2 | image_changed | +59.864 | +14.0531% | +0.159892 | -27.922339 | -28.812075 | +83992 | 0 |
| current2-pre2 | image_removed | +71.346 | +16.0395% | +251.124508 | -27.993347 | -20.313484 | +113840 | -1 |

Job, terminal and resources remain separate. Terminal includes the existing 250 ms
observer. MemStats/data-pool deltas are broad process windows, not phase CPU or
owner-mutex measurements. Selected callback families are not all application SQL.
Prepare nests within query callbacks and is never added again; absent Prepare
records do not exclude server planning. No CPU/perf/PMU/PG sampler was selected.

## Location of the second force increase

Retained client wall intervals, cropped to persisted job start/end, locate much
of the increase in COMMIT callbacks. All COMMIT union increases 1172.784257 ms;
only 0.095534 ms is in the 32 independently matched source COMMITs. Remaining COMMIT
union increases 1172.688723 ms. The matched source four-command callback UNION
increases 1.155245 ms, not 1300 ms. Matching uses PID, command order and actual
audio association. Matched transaction envelopes separately include helper gaps
and are not exclusive source CPU/wire time.

Four-read, matched-source and remaining-boundary unions are mutually disjoint in
these two force windows. All retained query union increases 1181.525352 ms; uncovered
job time increases 118.816648 ms. Target reads and remaining COMMITs are subsets
and must not be added again. Uncovered time may include unrecorded SQL and other
work; it is not labelled non-SQL, CPU or waiting time. Remaining transactions
are not all classified as writers or WAL waits. No WAL, storage, CPU or scheduler
evidence establishes cause or makes the expansion automatically code-independent.

## Source-reader candidate: accepted

S is the exact four-file patch extracted from cd227 and evaluated on current
main with accepted media/bitmap reuse enabled. No old branch, report or T reader
was imported. Its accepted source commit is
`d869b96d4e0cc18b34734f9684d63f79f6af4901`, directly after `5f992b6d`.
The four committed file contents match the immutable overlay selection.

One incremental build and 23 focused functional tests passed (72 subtests, zero
skips). All four current/source/source/current native cases and twenty phases
passed the business, work, traversal and cleanup guards. Both predetermined
orders improved warm jobs by 5.7991%/9.6118% and removed jobs by 3.4024%/19.6508%.
The only adverse job contrast is image_changed +2.2074%; it is retained. Both
orders satisfy the predeclared warm/removed gate and no-other-phase threshold.
Source-local callbacks improve in all ten contrasts, but the second baseline's
large COMMIT expansion cannot be credited to S as a general speedup.

Source commands fall from 128 to 32 and read-only transaction pairs fall by 32;
every phase has 96 fewer SQL commands. After removing the source command family,
the complete remaining SQL-template multisets match in all ten contrasts.
Writes, probes, catalog state and actual paths remain equal. The snapshot combines source-state reads; SQL/Scan failures
now propagate instead of the legacy first-query warning policy. Missing or
invalid snapshots still warn and skip. That deliberate error-policy difference
is tested and is part of the accepted behavior.

The original declined n=3 evidence remains unchanged. This is a new decision
against the current plan-reuse baseline with two fixed correlated contrasts;
it does not overwrite the earlier removal regression or establish tail latency.
Raw native evidence remains in `stage3/evidence/native`; the independent
commit binding is `stage3/source-commit-binding.json`. The independent review
is `stage3/analysis/independent-review.md`, SHA256
`99a4bb5a31cfbd3e2418d208b1e5821f0ea3c3f435b5c3991d6fe424a52afd5e`.
The runtime candidate was an immutable overlay; the commit binding establishes
the same accepted four file contents, rather than relabelling the measured input.
The accepted S gate retains additional adverse observations: first-order cold
Audio next-media enclosure +13.201174 ms; warm allocations +234248/+259504 bytes
and removal +32184/+243176 bytes; warm GC +2 in both orders. Second-order warm
and removal four-read sums are +17.366531/+3.506546 ms. These enclosing/broad
windows do not invalidate the gate, but prevent claiming uniform resource savings.
Second-order force saves 1153.211 ms in the job while its source-local callback
saving is 6.776850 ms and remaining-COMMIT union falls 1081.533685 ms; the latter
cause is not established and is not credited to the source rewrite.

The final Stage3 analysis is `analysis/stage3/version-report.md`, SHA256
`e6f4dc665fe445839ca2d343a7114c0967b5cf8ce8f25c54327923e357e512a3`;
its `acceptance-gate.json` is
`edaea6041477f8ea2bd07adfd96413ba68d6b3a9cb5a2e857f6273cbfc2c2561`.

## Lookup candidate: accepted

L reuses a named typed plan for the ordinary unique-path lookup, using the existing
startup/disabled/hook pool policy. Other-role lookups, rename/claim fallback, SQL,
current row decoding and freshness remain. There is no result cache, new lock,
retry or query registry. Its four production and two test files are committed as
`515d2ba88e33ebb14a41413b66b2026592539a3d`, directly after S `d869b96d`.
Both runtime variants retain accepted media/bitmap reuse and S's single source read.

One incremental build and the completed 30-test functional invocation passed
(91 subtests, zero skips). All four first performance invocations qualified, with
matching SQL-template multisets, 32 source reads, probes, writes, catalog state,
actual path order and fixture identities. All ten job and lookup contrasts improve.
Warm job changes are -5.0415%/-35.4549%, with lookup callback changes
-34.766060/-44.904403 ms; removed changes are -10.6919%/-32.7511%, with lookup
-40.072359/-39.461932 ms. Across all phases lookup callbacks fall 34.707901 to
54.276448 ms. These are two fixed correlated contrasts per phase, not tail estimates.

| Contrast | Phase | Job delta ms | Job change | Terminal delta ms | Allocation delta bytes | GC delta |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| lookup1-baseline1 | cold | -138.485 | -4.0911% | -0.147911 | -1310568 | +8 |
| lookup1-baseline1 | warm | -20.980 | -5.0415% | +1.328089 | -908456 | 0 |
| lookup1-baseline1 | force | -17.138 | -0.7689% | -0.646032 | -475840 | -2 |
| lookup1-baseline1 | image_changed | -30.687 | -7.8455% | +0.991599 | -1100424 | 0 |
| lookup1-baseline1 | image_removed | -40.495 | -10.6919% | +0.716015 | -1293448 | -1 |
| lookup2-baseline2 | cold | -2011.805 | -37.9601% | -1999.485663 | -1269208 | -1 |
| lookup2-baseline2 | warm | -185.246 | -35.4549% | -260.454469 | -1011376 | -1 |
| lookup2-baseline2 | force | -820.885 | -26.0856% | -750.747248 | -732032 | 0 |
| lookup2-baseline2 | image_changed | -166.014 | -31.7792% | -257.131840 | -995224 | +1 |
| lookup2-baseline2 | image_removed | -165.498 | -32.7511% | -252.795683 | -1069336 | 0 |

The positive job directions do not erase terminal/GC reversals in this table.
The second order includes a large remaining-COMMIT swing. Its full job saving
cannot be assigned to lookup, and Stage3 and Stage4 percentages are not multiplied
into a cumulative speedup. Broad allocation measurements are not exclusive CPU,
peak heap or owner-mutex observations. All signed raw/resource contrasts remain.
Task 2 retained-plan-memory work and an image-noop rewrite remain unselected.

## Retained harness failure and actual closeout

The unique lookup build succeeded: 47,166,020 bytes, binary SHA256
`64703882060a9219be6409e7c81969b3f6f1a2f8c97d3c8b8e36a481731d4448`.
Attempt-01 was interrupted during its selected 30-test functional invocation
with exit -15: the capacity monitor's `du` encountered ENOENT for a descendant
TempDir removed by normal cleanup. This is a retained harness failure, not an
observed completed product-test PASS/FAIL. No performance case had started.
The repair ignores only vanished descendants; missing root/other errors remain
fatal. Independent attempt-02 retained the same source, binary, budget and
selection without rebuilding. Its four performance cases are the first selected
samples, not favorable replacements. Original raw/receipts remain in `stage4`.

Across the selected stages, four builds and fifteen native invocations completed:
fourteen native invocations qualified and one retained harness interruption;
twelve were performance cases. No phase CPU/PG/storage sampler was selected.
Stage4 attempt-02 exported 43/43 files with size/SHA verification. Closeout removed
111,276,032 allocated bytes of independently retained inactive RAM duplicates and
owned compiler scratch; remaining task RAM (761,856 bytes), shared ext4 corpus
(3,006,464 bytes), local original binaries/raw, persistent source/data/evidence and
shared cache remain. Workers exited. PG/Goby/QEMU and reserve 403374 blocks remain
unchanged; final persistent available space was 725,893,120 bytes.

## Evidence and delivery boundary

The external evidence root is `.artifacts/scan-version-hotspots-20261007`.
Full JSON, native raw, binary copies, SQL/work/traversal and fixture records stay
there; no large JSON is added to the repository. Detail reports cover all signed
observations and are linked below.

| Evidence | SHA256 |
| --- | --- |
| [Stage1 real-version report](../../.artifacts/scan-version-hotspots-20261007/analysis/version-report.md) | `55540592780cae34aef94a8c0a39e9894dba0e5c4de4c0c66418c20077f55965` |
| [Stage1 independent review](../../.artifacts/scan-version-hotspots-20261007/analysis/stage1-independent-review.md) | `2b3d3cf93f7daa6f63c151a6aa8cd8e48e270edea40efd57936d725e3cd13c50` |
| [Stage3 report](../../.artifacts/scan-version-hotspots-20261007/analysis/stage3/version-report.md) | `e6f4dc665fe445839ca2d343a7114c0967b5cf8ce8f25c54327923e357e512a3` |
| [Stage3 gate](../../.artifacts/scan-version-hotspots-20261007/analysis/stage3/acceptance-gate.json) | `edaea6041477f8ea2bd07adfd96413ba68d6b3a9cb5a2e857f6273cbfc2c2561` |
| [Stage4 independent review](../../.artifacts/scan-version-hotspots-20261007/stage4/attempt-02/analysis/independent-review.md) | `d2f0d0b04ca90a494a0a7d49ab8d47e8be3854c55bbbbb17fe65a6cadbdc037f` |
| [Stage4 report](../../.artifacts/scan-version-hotspots-20261007/analysis/stage4/version-report.md) | `90a7b048329b452a163cbd37d63de0f02e62486220b7baae23aec5b303ba4b03` |
| [Stage4 gate](../../.artifacts/scan-version-hotspots-20261007/analysis/stage4/acceptance-gate.json) | `1e0d04d4d07c2b2149667c243d756e1acd17eb3ebd1af6d3b91f26d07f988f80` |
| [Stage4 43-file export manifest](../../.artifacts/scan-version-hotspots-20261007/stage4/attempt-02/completed-export-manifest.json) | `a0e1ededd995d68cd3f135748c4e5100f09547a9139389d84cc1f64c98e2d0b6` |
| [Actual closure](../../.artifacts/scan-version-hotspots-20261007/actual-closure.json) | `a22136c613199567b87c1764157688b5a2ebf730fc8c5de591f9049c2e24aacf` |
| [Closure export verification](../../.artifacts/scan-version-hotspots-20261007/closure-export-verification.json) | `6581cc02a178cd4879a837451fcd2494b68656af40aa88409156df2b5644a9da` |

[Stage4 full report](../../.artifacts/scan-version-hotspots-20261007/analysis/stage4/version-report.md)
retains all callback/resource contrasts. Stage1 fills the true-binary gap in
[the earlier controlled query-plan experiment](scan-query-plan-reuse-20261007.md).
[The earlier source-reader decline](scan-embedded-source-readonly-20261007.md)
remains unchanged: this accepted S decision uses the newer plan-reuse baseline
and its own predetermined gate, not a retrospective rewrite of the old n=3 result.

Accepted source chain is `5f992b6d` -> `d869b96d` (S4) -> `515d2ba8` (L6).
Only these ten selected source/test file contents are included; no old cd/T branch
or diagnostics are imported. Runtime overlays stay immutable, with separate Git
commit bindings. Exact final main/push identity and fresh original-workspace WIP
preservation are recorded by the external publication receipt; historical WIP
bytes are not restored over newer legitimate changes.
