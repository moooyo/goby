# Store-lifetime scan inspection cache, 2026-10-06

This batch extends successful digest-to-Info reuse across scans within one Store.
Warm/edit/removal process allocation falls about 87% in all three pairs, and
force allocation falls 39.91%. Warm job median is 23.71% lower; edit/removal jobs
are faster in all three pairs. However, the third pair is slower for cold
+58.23% (+2009.288ms), warm +46.56% (+539.184ms) and force +5.28% (+163.406ms).
Stable latency is not established. Cold's job median is nearly unchanged
(-0.3240%) while its terminal median rises +0.0602%; no cold improvement is
claimed. These negative samples are retained, without dismissing them as noise
or asserting a host cause. All nine selected Go products qualified and closed.

## Source and narrow change

Baseline main is `2269711fcdeb2b76c76367b144536d99615d8af8`, with runtime contents
equivalent to retained `d9cc07e1a62b3b44f18f952d00b7c7304e3b25aa`. The two later
Stop report/handoff files are the documentation-only difference. Candidate
`b100a315367d0108aa2f58447cfd71cfeea0b4f9` is a direct child of main226 and changes
exactly three production files and two new tests.

Store now owns one zero-value inspection cache instead of each scan allocating
its own. A private mutex protects lookup and successful insertion only; fresh
reads, hashing, validation/decode and joined-slot waiting remain outside it.
Insertion rechecks cancellation and concurrent insertion after acquiring the
lock. The 512-entry bound is retained; concurrent duplicate insertion does not
evict unrelated entries.
Only digest keys and value Info are retained, without pixels, input bytes,
paths, descriptors, authorization or per-item publication facts. Concurrent
misses may still decode independently. No global cache or new Close hook is added.

All fresh source reads/hashes, cancellation, source identity, final scan proof,
notification and authoritative removal behavior remain. DELETE and subtitle
optimizations are outside this selected batch. New tests cover concurrent hits,
misses/capacity, blocked readers, cancellation at insertion, duplicate admission
and separate scans with changed/invalid/removed image sources.

## Baseline evidence and denominator

One complete original real-image test was profiled on the immutable baseline,
including fixture generation, all five scan phases, checks and teardown.
The denominators are 6.17 seconds of sampled Go CPU and 2511.72 pprof MB of
sampled alloc_space, with allocation sampling rate 65536 bytes. PostgreSQL and
media subprocess CPU are excluded; cumulative nested stacks are not summed.

| Cumulative call chain | Go CPU | Whole CPU share | alloc_space | Whole allocation share |
| --- | ---: | ---: | ---: | ---: |
| artwork.decodeImageData | 1.02s | 16.53% | 1150.52MB | 45.81% |
| scanRealImagePNG fixture generation | 0.59s | 9.56% | 528.21MB | 21.03% |
| artwork.readImageData | 0.05s | 0.81% | 34.96MB | 1.39% |

The scan decode stack is separate from fixture encoding. These whole-process
shares are not warm-job wall time, exact peak heap or a predicted candidate saving.
The diagnostic's elapsed time is excluded from performance samples. Detail and
profile/binary identities remain in `analysis/baseline-profile-analysis.md`
under `.artifacts/scan-hotspots-20261006`.

Two possible allocation follow-ups are root-topology/mountinfo parsing
(`revalidateRootTopologyLockedThread`, cumulative 291.43 pprof MB) and process
identity reads (`readConventionalProcessIdentity`, cumulative 240.44 pprof MB).
These whole-profile stacks may be nested and must not be summed. They do not
establish wall-time hot spots or justify removing physical/proc fences. Neither
change is implemented in this batch.

## Finite verification and paired results

The selected work is one baseline whole-profile diagnostic, two focused race
invocations for artwork/library, and six fresh TRACE0 timing processes in
BC/CB/BC order. Both sources use the identical existing diverse real-image driver
and full cold/warm/force/image_changed/image_removed phases. Artwork race passed
19 top-level/32 subtests and library race passed 11 top-level/3 subtests, with all
30 required names and no skips/failures. Source, exit, fixture and service guards
passed. The actual receipt is `evidence/cache-focused-receipt.json`, SHA256
`9fbc7a7d034faaab45d9155dca82a878f1517af5cacff97778001cb4fdf14117`.
All six new unprofiled TRACE0 processes qualified, yielding thirty phase
observations with no supplementary samples. The baseline whole profile and
earlier task timings are excluded from these three pairs.

| Phase | B job ms, blocks1/2/3 | C job ms, blocks1/2/3 | Paired median change | Median delta ms | C/B range | Slower blocks |
| --- | --- | --- | ---: | ---: | --- | --- |
| cold | 3418.508, 3495.806, 3450.438 | 3407.432, 3419.870, 5459.726 | -0.3240% | -11.076 | 0.978278-1.582328 | 3 |
| warm | 1157.398, 1147.705, 1158.044 | 882.982, 875.394, 1697.228 | -23.7097% | -272.311 | 0.762734-1.465599 | 3 |
| force | 3038.348, 3141.272, 3093.863 | 2974.882, 2912.522, 3257.269 | -2.0888% | -63.466 | 0.927179-1.052816 | 3 |
| image_changed | 1128.544, 1122.261, 1579.616 | 1020.869, 935.926, 898.632 | -16.6035% | -186.335 | 0.568893-0.904589 | None |
| image_removed | 1133.047, 1157.880, 2043.469 | 934.832, 876.370, 893.761 | -24.3125% | -281.510 | 0.437374-0.825060 | None |

Medians are median(C_i/B_i - 1) and median(C_i - B_i), not ratios or differences
of source medians. The job, observed-terminal and worker-retirement windows are
separate; terminal polling does not replace persisted job duration.

| Phase | Terminal median change | Allocated bytes median change | Allocation slower blocks | GC count median change | GC pause median change |
| --- | ---: | ---: | --- | ---: | ---: |
| cold | +0.0602% | +0.0045% | 1, 2 | -2.0305% | +7.6392% |
| warm | -19.8728% | -87.0733% | None | -81.0345% | -85.7395% |
| force | -7.6595% | -39.9127% | None | -26.2570% | -17.5344% |
| image_changed | -19.8988% | -86.8224% | None | -80.3279% | -83.3864% |
| image_removed | -19.9851% | -87.0974% | None | -81.8182% | -86.3575% |

Cold terminal is slower in blocks2/3; warm/force terminal in block3, and changed
terminal in block1. Force retirement and pool-acquire duration have positive
medians, and changed pool-acquire duration rises in all three pairs. Their full
values and all other reverse resource/retirement samples remain in the external
report and JSON. Allocation/GC are process deltas through terminal and worker
retirement, including observer/background work, not CPU or peak heap.

C3 warm still allocates only 36.175128MB, with GC pause 0.549785ms and empty-pool
wait zero, despite its slower job. The pool window excludes the reserved owner
and ownership mutex. B3 changed/removal jobs are also higher than B1/2 at
1579.616/2043.469ms; adjacent ordering shows slow phases in both variants during
that period but does not establish their cause. SQL counts/timing and host waits
were not collected, so these observations do not attribute the time shift.
`analysis/adverse-timing-note.md` preserves the detailed block3 chronology and
the limits of these small resource windows without changing the main summary.

Each process starts a fresh Store, so cold has no cross-process cache reuse.
Force still performs 160 real probes; the five phases retain probe counts
160/0/160/0/0. The 288 distinct initial PNG digests plus one replacement fit within
512 entries and do not establish performance under eviction pressure. SQL
command/transaction counts are not measured in this TRACE0 timing selection;
unchanged SQL source
does not establish measured identical transactions. No TRACE1 pair is appended.

## Source retention and publication

The five-file overlay reuses immutable physical8e and baseline-d9cc backing
files; no expanded source or build/module cache copy is created. Source freeze
`source-freeze-store-cache-v1.json` is unchanged. The separate
`source-freeze-store-cache-commit-binding-v1.json` records candidate identity
without changing frozen runtime bytes or requesting a product replay.

The result detail is `analysis/cache-paired-report.md` SHA256
`9d019fb2bada08aeec1328ebaa74ca78b6fff05d1467c5ef1b28a9219245f0d0`
and complete JSON `e34e90047f73122e4c30a124ceeb6bf327873491c65669717fbe5b038bd629cd`.
All source/driver, corpus, tuple/xmin, probe and Store.Close guards qualified.
Rows are 672/672/672/672/671 and PNG files 288/288/288/288/287; these tuple outcomes
do not by themselves count source-file changes. Two pre-upload Python missing-file
preparation errors produced zero Go invocations or observations and remain
retained separately; they were not product retries or replacement samples.

Actual closure is `qualified_and_closed`. All workers exited. Only owned inactive
compiler scratch, the empty fixture and three verified RAM profile/binary copies
were removed (profile copies 46825472 allocated bytes); matching local originals
remain. Source backings, manifests, archives, raw, derived data and ordinary
shared caches remain; no cache cleanup occurred. PG/Goby identities and reserve
403374 (1%) are unchanged. Final persistent available capacity is 620,027,904 bytes,
a snapshot including concurrent activity rather than a cleanup-attribution claim.
`evidence/closure-receipt.json` SHA256 is
`99e432b5186f81bd942a4f9ce1869508f2fb883204cfa21c70c4ba5da7a544b7`;
`final-export-verification.json` SHA256 is
`b56ffb4730ca1cecf8a517c70c243e945d61e9f93e73ec1a0fd99aeb9d22b825`.

Original D workspace WIP is outside all source inputs. Publication preserves
its current fresh bytes, including overlapping scan.go and legitimate newer
edits. Its receipt binds delivery identity separately from measured candidate
b100. The preceding Stop/cumulative-main task and its artifacts remain immutable;
its negative observations are not claimed resolved by this cache change.
