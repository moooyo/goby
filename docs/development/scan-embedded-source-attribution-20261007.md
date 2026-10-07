# Embedded-source execution-profile follow-up, 2026-10-07

Profiling identifies recurring PostgreSQL parsing and planning work on the
existing CacheDescribe execution path. Non-overlapping classifications place
20.9% of the application-PID PG samples in planning/plan acquisition and 10.5%
in parse/analysis. This is measured work under a policy also present in baseline,
not a cost newly introduced by C or a predicted wall-time saving. Go samples instead mainly
show kernel/scheduler execution under the profiling condition; they do not
establish a business-lock or JSON hotspot. Instruction bounds do not prove that
the candidate executes more business instructions.

The historical C/B regression's unique cause remains unproven, and neither C nor
T is accepted for source publication. A narrow per-query CacheStatement exception
is a new optimization lead, separate from that attribution question. No such
change has been implemented or verified. Both selected native products, their
exports and actual cleanup are complete; the failed first acquisition remains
part of the record. This documentation-only update is based on main
`7346bba8f3033858ccf76053f75f0776f8291a5f`, with runtime unchanged at
`6386c8fd88585557ee65acbc0a12be23d90a41d4`. The prior report is preserved below.

## Selected workload, acquisition failure and repair

Both products execute the unchanged retained S/T native binary, SHA-256
`e5c4050a7246d7745511fd0eef678c282edb14b685b843f676afdaaf361dd02e`:
five original S phases, STTS/STTS controls, the 32-Audio equality contract and
measured Store.Close. Each passes all thirteen functional observations, contract
and Close. Accounting is two native products, three bounded own-process tool
trials and zero Go builds. No implicit test2json build or fabricated GoJSON is
used. Earlier source, samples and acceptance decisions are unchanged.

Task-local perf 6.12.111 and required libraries were extracted privately without
system installation. Perf-only library paths never enter the test's child tree.
No shared PG/VM/CPU, scheduler, affinity, swap or durability setting was changed.
Attachment is restricted to the owned test and exact-tag dedicated PG backends.

Attempt 1 is a functional pass with failed acquisition: its six profile targets
stop around initial enable acknowledgement, leaving no usable control attribution
coverage. The matching perf ELF writes five bytes, `ack\n\0` / `61636b0a00`.
The v1 newline parser consumes four, leaving NUL before the next ACK and causing
its own shutdown. This collector fault does not explain the product regression.
All failed outputs and native stop codes remain retained.

The separately authorized attempt-02 collector consumes the exact five-byte
frame, retaining raw bytes and separate identity/protocol errors. One bounded
own-Python protocol trial passes with four ACKs, empty residual buffers, 143
samples, 13 stat intervals and 150,006,480 ns helper CPU. Requested-SIGINT native
profiler exit codes of -2 are preserved. This is the third tool trial, not an
extra product or shared-service attachment.

V2 then functionally passes using the same binary, workload, validator, rate and
settings. Main Go and PG profiles enable before and span the controls. Early/late
fixture targets, native stop codes and partial-lifetime metadata remain explicit;
functional PASS is separate from acquisition coverage. V1 is neither replaced
nor filled with V2 data.

## V2 control results and scope

All eight controls have 883 application callbacks, 67 fingerprints, 32 single
snapshot queries, 24 full-phase COMMIT callbacks, identical template/final-row-tag
sequences and stable query/Prepare capacities 32768/150. No application Prepare
occurs. Original media/image, no-DML, key/xmin and no-probe/extraction guards
remain. Equal observed order does not prove equal arguments, plans or physical
execution work.

All four fixed T-minus-S jobs are lower in this process, but source and CPU
changes are mixed. All eight jobs and both contrast orders remain below.

| Pair | T/S windows | T job, ms | S job, ms | Job delta, ms | Source callback delta, ms | Go endpoint CPU delta, ms | PG endpoint CPU delta, ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 2 / 1 | 483.955 | 490.744 | -6.789 | +0.303912 | -5.578 | -0.087135 |
| 2 | 3 / 4 | 499.854 | 519.387 | -19.533 | -7.049209 | +7.440 | -29.739709 |
| 3 | 6 / 5 | 552.111 | 559.065 | -6.954 | +2.325935 | -23.553 | +6.253502 |
| 4 | 7 / 8 | 517.653 | 547.598 | -29.945 | +4.208069 | -11.699 | -29.518657 |

Source callbacks increase in pairs 1/3/4, Go CPU in pair 2, PG CPU in pair 3,
terminal time in pair 3 and allocation in pair 2. These correlated windows are
not independent N=8 or a replacement for preceding mixed acquisitions. Resource
endpoints exceed the job interval and include runtime/observer participation;
SQL union excludes ownership Ping and pre-tracer waits. Source callback and
approximate Audio enclosures are not exclusive source-helper CPU.

## Cross-acquisition order and region audit

A separate bounded offline audit corrects the interpretation of region labels;
original captures and frozen derived reports are unchanged. V2's eight controls
share one application-template sequence, and the earlier acquired-row and CPU-
observation products each share the earlier sequence. The cross-acquisition
sequences differ even though each control has the same 883-callback multiset.
All 32 V2 source snapshots occur at one-based application ordinals
475, 478, ..., 568;
in the earlier two products they occur at 55, 58, ..., 148.

| Read family | Earlier before first source | V2 before first source | Earlier after last source | V2 after last source |
| --- | ---: | ---: | ---: | ---: |
| Stored-item lookup | 1 | 81 | 128 | 48 |
| Media facts | 1 | 81 | 128 | 48 |
| Image comparison | 0 | 85 | 136 | 51 |
| Bitmap existence | 0 | 80 | 128 | 48 |

The historical Audio approximation begins at the whole phase's first common
lookup, not the first Audio-specific lookup. In V2 its approximately 301-352 ms
envelope includes substantial non-source reading before the 32 source snapshots;
it must not be interpreted as Audio-only or source-exclusive work. Static
fixture/call-family structure supports prior non-source item work, but raw SQL
has no arguments and cannot prove specific movie paths or per-ordinal item
identity. In V2 window 1, roughly 48%-66% describes the first/last source
callbacks' positions inside the job, not their fraction of CPU or execution cost.

The first/last source timestamps and the after_source/post_audio cutoff arithmetic
match the raw callbacks. The boundaries are correct, but their remaining work
populations differ across acquisitions: V2 has 48 later lookup/media/bitmap reads
and 51 image comparisons, versus 128/128/128 and 136 earlier. Cross-acquisition
region CPU, instructions or sample totals therefore do not represent identical
work cohorts and must not be interpreted as isolated source/profiler effects.
Within-acquisition observations and their unchanged raw values remain retained.

Production enumeration uses File.ReadDir(64), appends entries in returned order
and recurses without explicit filename sorting. The fixture manifest's WalkDir
hashing does not establish the production traversal order. Identical template
order within one process still does not prove identical path/parameter ordering.
The new appendix records this distinction rather than rewriting the earlier
reports or inventing a mapping for unrecorded arguments.

## Function samples: recurring PG work and a changed Go observation condition

The eight controls contain 3,063 samples on the actual Go TGID and 2,774 on each
window's application-participating PG backends. Every recorded period is
1,003,009 ns, a sample weight rather than exact CPU time. The same-guest
mono/wall/mono hull is 4,600 ns wide; samples are included only when their entire
possible time lies inside a window. This is observed clock coverage, not proof
against every unobserved clock change.

Go exclusive kernel leaves are 2,289/3,063 (74.7%). The `finish_task_switch.isra.0`
leaf is 1,704/3,063 (55.6%), a subset of the kernel category rather than the whole
74.7%. Runtime leaves are 430 (14.0%); no application leaf dominates. Cumulative
sysmon, scheduling, worker and scan ancestors overlap and cannot be added as
separate CPU owners. Frequent nanosleep/sysmon/scheduler stacks show execution
around transitions, not business computation or measured sleep duration.

Go system CPU spans 189.787-261.987 ms per wider endpoint window, substantially
above preceding observations; user CPU overlaps earlier ranges. The acquisitions
also differ in traversal order, region work cohorts and resource-endpoint scope.
There is no single-factor profiling-overhead control. These are changed
profiling/execution conditions, without isolating perf, VM, runtime or ordering
causality. Absence of a named perf_event frame cannot exclude inlined
context-switch profiling work. It does not justify changing business locks,
JSON handling or Go scheduling. Sample weights, rusage, task-clock and estimated
observer cost are not subtracted to reconstruct the old unprofiled workload.

PG categories below are mutually exclusive. Unknown leaves are retained first,
then kernel and allocator leaves; the nearest visible parse/plan/execute ancestor
classifies the remaining known samples. This is callchain classification, not
complete server-stage accounting.

| PG category | Samples | Share of 2,774 application-PID samples |
| --- | ---: | ---: |
| Kernel | 685 | 24.7% |
| Planning / plan acquisition | 579 | 20.9% |
| Unknown leaf | 576 | 20.8% |
| Parse / analysis | 292 | 10.5% |
| Allocator | 281 | 10.1% |
| Other known | 183 | 6.6% |
| Execution | 178 | 6.4% |

Visible standard_planner, subquery_planner, parser and analysis frames establish
actual recurring work. Plan acquisition does not mean every sample compiled a
new plan. Overlapping cumulative planning/parse/execution counts of 1052/460/328
include descendants and are not added to this table or to allocator samples.
The percentages are not recoverable wall-time fractions or a forecast of 31%
acceleration. No major category consistently falls with the four lower T jobs.

Parsing/planning also appears in common reads, not just source snapshots. The
stored-item, image-noop, media-facts and bitmap families have 195/168/122/35
non-overlapping named planning-plus-parse samples, respectively. Four PG samples
associate with observer callbacks and 28 remain ambiguous/between callbacks;
application-PID membership is not an exclusive business CPU partition.

Unknown PG leaves remain 576, including 449 in the matching postgres ELF's .text
with no exact exported-function match. They are not renamed by nearest symbol or
redistributed. At least one unknown frame appears in 2773/2774 PG callchains,
limiting FP unwinding completeness despite visible named anchors. Five nonempty
records decode with no reported lost/throttle events; that does not prove unbiased
sampling or complete attachment/unwinding. The empty early/late PG record is
unavailable, not zero cost. All losses, unknowns and lifecycle limits remain.

## Instructions and cycles do not resolve the old difference

Counters retain native task-clock and unscaled grouped cycles/instructions,
event runtime, rounded running percentage and unsupported/not-counted nulls.
100.00% does not reconstruct exact enabled time. Conditional on matching stat
source provenance, origin/read support uses launch-to-disabled-ACK uncertainty
and serial interval read bounds; the wider first-result model is also retained.
Crossing intervals are never divided proportionally. Contained sums and enclosing
sums describe whole observed support, not all unscheduled physical execution.

The two fixed pairs with closed Go instruction bounds overlap:

| Pair | S observed instruction range, million | T observed instruction range, million |
| --- | ---: | ---: |
| 1 | 472.020-577.937 | 446.789-543.694 |
| 2 | 442.164-519.306 | 434.403-521.708 |

Later enclosing sets include not-counted rows, preventing a closed bound and a
signed conclusion. Full aggregate PG enclosing bounds are unavailable in every
job because participating backends can idle or exchange routes and emit null
rows. Known subtotals remain visible but are not substituted for a complete upper
bound; missing values are not zero. The evidence does not establish more business
instructions, physical frequency, or a specific function-cost difference.

The Go record contains 3,930 samples on one TGID, with six sampled TIDs, nine
COMM records and no FORK record. Controls independently retain zero native probes.
This does not prove every possible child lifetime is observed or make inherited
stat counters inherently Go-self-only. PG parallel workers not actually captured
are not implied covered. Counter/sample/CPU scopes remain distinct.

## Existing query-mode policy and the narrow optimization lead

Static source history establishes that global CacheDescribe predates B and C.
The same database.go blob is present in main, baseline and candidate. The policy
preserves typed execution without retaining named server plans, following earlier
backend-memory observations. JIT is already disabled. Neither JIT-off nor a
cached client description removes repeated unnamed Parse/analysis; cache_describe
uses ExecParams/SendParse. Zero PrepareTracer events therefore do not imply no
server parsing/planning. This is measured recurring work under a policy also present in B, not a new C-only
change or proof of the historical reversal's cause.

CacheStatement also uses statement descriptions and parameter OIDs. It can reuse
named parse state while still planning custom executions or responding to
invalidation. It caches execution plans, not query results. The review finds no
basis to globally switch modes or reuse an earlier freshness observation.

First control the comparison's workload ordering: reuse one fixture, Store and
schema in fixed interleaved windows, recording actual item/path digest order,
query order and before/after-source family counts on the test side. Retain first
uses, Prepare events and all windows. Do not change production directory sorting
to manufacture a stable benchmark order. This addresses the observed ordering
confound without claiming to remove cache/plan first-use or time variation.

Then evaluate the smallest local CacheStatement candidate: the media-facts read
`6bf1992ebd49` and bitmap-existence read `2b0fe30f9764`, leaving the global default
intact. These are stable parameterized shapes on the owner
and data-pool paths. The larger item-lookup and image-comparison queries are
later, separate candidates with more plan-state/array-estimate risk.

Before accepting such a selected change, preserve parameter type/null semantics,
source/root/freshness observation points and real writer/permission contracts.
Provide a declared cache-disabled fallback, and verify cold/replacement/reconnected
sessions, eviction, schema/statistics invalidation, custom/generic plan behavior
across varying parameters and retained named-plan memory. Keep the existing
zero-named-statement default tests intact for ordinary callers. No implementation,
new benchmark or production release of this proposal has occurred here.

## Actual closeout

Both native products and all owned profilers/observers exited. Final decoded
exports and closeout receipts are hash verified. Task tool RAM falls from
41,168,896 to 0 bytes and output RAM from 113,569,792 to 0; two empty ext4 fixture
directories are removed. No private compiler cache was used or Go build selected;
the unused prior 8,192-byte cache and shared caches remain. Source, both failed
and corrected raw attempts, matching Go/PG ELFs, receipts and the private tool
snapshot are retained. PG/Goby/QEMU identities and reserve 403374 are unchanged.

Final persistent availability is 328,396,800 bytes and guest available memory is
6,646,124,544 bytes; these are separate snapshots, not a total cleanup attribution.
No diagnostic runtime remains active. No additional product, production edit or
source publication is selected. Workspace WIP remains outside documentation
publication and must be protected using fresh current identities.

## Evidence

Evidence root: `.artifacts/embedded-source-profiling-20261007`.
Private tool snapshot SHA-256:
`80407b2c62d200156cfa2413342b8d8f8d63ef44914b211ae34e1f00706a5289`.

| Artifact | SHA-256 |
| --- | --- |
| `diagnostic-plan.md` | `4dfca06190172d28045eae89e7dca9c94d6e0c67aaaeb622c5088e250dfdac9c` |
| `evidence/diagnostic-receipt.json` | `8646a7c161af9b1d183f40a6c731732fa7dffed9a75215793fefb74d82f10385` |
| `attempt-02/protocol-check/protocol-final-receipt.json` | `2728c60a36ea67a09ac3c405e552548eda5bb43623b02dc03db41a6920e15426` |
| `attempt-02/evidence/diagnostic-receipt.json` | `9129543ca8eec3832dd9c69e918686c6b9ee6915fc63a64bc2a408e6c6f4521d` |
| `attempt-02/analysis/counter-query-report.md` | `df495dcec717323a9ed0414d1cec54251460b65ef1ee45fc25a98b6a50a58420` |
| `attempt-02/analysis/counter-summary.json` | `b990adb582e33be76d800993470eafa173add6eb567ac498a203ee508eea2066` |
| `attempt-02/analysis/query-summary.json` | `c227ba99687006b9730a84cc6145d8d05b37dd7c34edd22472c9544730de6dbc` |
| `attempt-02/analysis/query-order-audit.md` | `7107b9b3cfdd5b4cae6e9153f9b62914c2f522df63880cfe385af738fa5ecaaa` |
| `attempt-02/analysis/query-order-audit.json` | `ca25431fa8a17b6997898a86b28128a4a97844300714adcaca50f457dd5df465` |
| `attempt-02/analysis/function-samples-report.md` | `0dc253746628ec27c09df8497721fbe3f2790a2961e4145eb1886bd88e85cd58` |
| `attempt-02/analysis/function-samples-report.json` | `d6b85f649acd2b08ce1ff716f696c7a873f191331c437428a4e4d68ae50c6923` |
| `analysis/query-mode-policy-review.md` | `d0c92e99c20be724109ba714759e85b6ccf16e187b5d60e370ab6f1ce3d28613` |
| `attempt-02/closure-receipt.json` | `fc058aac3cda7941ae1f1b386d8edbb787eef77d3d199e50a0f82dd8ab959399` |
| `attempt-02/actual-close-export-verification.json` | `f0873215864cfa613c12f9c8e4dce0a8c68c051ddfff754f9084373d9473ff68` |

The current-main report is preserved byte-for-byte below, original SHA-256
`ff5c81a4d1c1d1f3afa8c0ebc1e0b1f3cf0adbdc6779274044c38b75e0dcc453`.
Its historical source decisions and completed closures remain unchanged.

---

# Embedded-source mechanism follow-up, 2026-10-07

Source-read savings are measured, but a stable end-to-end scan improvement and
the historical regression's root cause remain unproven. The first same-process
L/S control retains its early Audio-region saving while later work offsets it.
The acquired-row S/T comparison has mixed directions; a third process with added
CPU observation favors T, without resolving that inconsistency. Neither C nor T
is published as a performance fix.

All three selected Go products, their exports and actual resource closeout are
complete. Each retained the original five phases, eight fixed controls, the
32-Audio equality contract and measured Store.Close. No extra product or replay
was started. This documentation-only update is based on main
`e27093860472f4c8dd8b77cb44b57bc91215066b`; published runtime remains
`6386c8fd88585557ee65acbc0a12be23d90a41d4`. Candidate
`cd22752397ff70b490b27f9b7536dfddb57d737f` stays withheld, and T exists only in
artifacts. No production source was edited in this follow-up. The prior
four-stage report is preserved in full below.

## Offline localization and static limits

The earlier captures show distributed common-read differences, including shifts
in medians, rather than only a few long outliers. Their direction reverses in
stage 4, and neither a fixed candidate surcharge nor a universal server-speed
factor fits all families and time positions. These descriptive distribution views
retain every original sample and do not trim or correct acceptance results.

Recorded reader blocks occur early, approximately 4.7%-26.1% through the earlier
removal jobs. Only one lookup and one media-facts read precede the first reader
SQL; their signs differ between stages. Most common reads follow the reader
blocks. This establishes order without excluding prior-phase carryover or
assigning later work to the reader implementation.

Static inspection does not support permanent twenty-column scan-plan pollution:
the pgconn field-description array stays fixed at sixteen and later queries
rebuild their reader/scan plans. The measured S source callbacks, with maxima
2.50/1.64/1.57/2.84 ms in its four controls, do not support the fifteen-millisecond
slow-write background-reader trigger. Existing matching-binary disassembly was
inspected without another execution or build. DWARF reports 840-byte
indexedMediaSource and 912-byte embedded snapshot types; S/L attempt frames
differ by 136 bytes. Caller snapshot/closure addresses are on the stack, rather
than a new 912-byte heap closure. Copy/write-barrier instructions exist, but sizes
and instructions do not establish milliseconds of cost or per-item stack growth.

Pool.QueryRow advances a per-physical-connection poolRow batch cursor, while the
retained transaction reader does not; a subsequent batch is 5,120 raw bytes.
The second product directly examined the acquired-row alternative and found
mixed directions. The third found a favorable association under its added
observer, without establishing a stable benefit. This code difference has been
checked and does not justify prioritizing explicit Acquire as a proven fix.

## First mechanism product: complete L/S paths in one process

Exactly one Go product qualified. It retained the original five S phases, then
eight warm scans of the fixed post-removal state in the predeclared order
S L L S / S L L S, followed by a 32-Audio L/S full-value equality contract and
the unique measured Store.Close. L preserves the complete legacy attempt,
locals, closure, rollback defer and validation helpers; S preserves the candidate
attempt. The same Store, schema and pools remain, without forcing backend routes.
These are eight correlated controls, not eight independent replicates or a
replacement for the historical removal acceptance samples.

All controls retain 160 media items and 671 images, unchanged image keys/xmin,
no image DML and no native probes/extractions. L/S use 979/883 application
callbacks: 32 complete legacy four-command groups versus 32 single SELECTs.
The post-control contract validates 64 logical reads with equal values.

All deltas below are S minus L in the four fixed adjacent contrasts. The Audio
envelope runs from the first lookup QueryStart to the first following image
QueryStart; it contains interleaved source/owner/media/Go work and initial
post-source work, rather than exclusively timing the reader helper.

| Pair | S/L windows | Job delta, ms | Source callback union delta, ms | Approximate Audio envelope delta, ms | Rest-of-job delta, ms | Go endpoint CPU delta, ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 1 / 2 | +1.455 | -9.132501 | -9.137783 | +10.592783 | +3.542 |
| 2 | 4 / 3 | +8.163 | -9.392784 | -8.759162 | +16.922162 | +15.765 |
| 3 | 5 / 6 | -6.017 | -11.570032 | -10.403306 | +4.386306 | +4.153 |
| 4 | 8 / 7 | +5.092 | -10.405396 | -9.420175 | +14.512175 | +10.196 |

The early envelope remains faster in all four comparisons, including an
alternative endpoint after the first image callback. Thus immediate work inside
that envelope does not consume all source callback savings. The offset lies
outside the envelope, but this is not a function-level attribution. Lookup and
bitmap callback medians and p95, and Go endpoint CPU, rise in every S contrast;
images/media have mixed signs. No stable end-to-end acceleration is established.

Lookup/bitmap differences persist within the same physical PG PID. Pairs 1/3
retain the same PID and equal counts; pairs 2/4 split S across two PIDs, so their
same-PID subsets also change ordinal/work cohorts. No query arguments were
captured for parameter matching. Each adjacent pair uses the same PID for all
32 source blocks. Two S windows switch data PID after the middle observer poll,
but other windows poll the other PID without switching. Routing share or polling
alone is therefore insufficient to explain the result.

The query recorder grows its slice during L2 (16,164 to 20,406 capacity) and L7
(20,406 to 25,746). Their roughly 30 MB allocation versus roughly 28 MB elsewhere
is confounded by instrumentation growth; those bytes and later GC work must not
be assigned to production code. All windows remain included. L2/L6 each have two
first-use legacy Prepare callbacks on separate connections, with no common-read
Prepare in any control. This does not exclude server parsing/planning.

CPU endpoints include wider terminal/retirement/observer work. Actual-query PID
CPU coverage is complete, but disabled scheduler statistics leave wait/timeslices
null and resource status partial. CPU, SQL wall coverage and GC estimates are
separate scopes. Passive PG context has 239 active/no-wait common-read points and
one ClientRead in a 0.163035-ms callback; no common-read long lock/WAL/data-I/O
wait was sampled. Host clock-expanded windows overlap adjacent controls; aggregate
steal/vCPU runtime cannot identify critical-thread delay, and dynamic frequency
or host-core placement was not measured. No specific storage, CPU-frequency,
scheduling, JSON or copying cause is established.

## Second mechanism product: no stable acquired-row benefit

One separately frozen Go product qualified all thirteen phases, the 32-Audio
S/T equality contract and measured Store.Close. It retains five original S
phases, then S T T S / S T T S. Both paths use the same twenty-column statement,
arguments, destinations, JSON work and validations. S keeps Pool.QueryRow; T uses
explicit Acquire, Conn.QueryRow.Scan and immediate Release, with deferred
error/panic cleanup and the original statement/argument evaluation order. This
compares the complete acquired-row path, not the poolRow cursor alone.

Every control actually has 883 application callbacks, the same application
fingerprint/count multiset, 32 single-source SELECTs, zero application Prepare
and unchanged 32,768-query collector capacity. The total of 883 is an observed
result, not a gate that rejected natural bookkeeping. All controls retain 160
items, 671 unchanged image rows and no image/embedded-cache DML or native
probes/extractions. The post-control contract verifies equal full values for
64 logical reads.

All eight job observations appear in the four fixed adjacent contrasts below.
Deltas are T minus S; the controls remain correlated within one process.

| Pair | T/S windows | T job, ms | S job, ms | Job delta, ms | Source callback delta, ms | Go endpoint CPU delta, ms | PG endpoint CPU delta, ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 2 / 1 | 414.967 | 412.512 | +2.455 | +1.042129 | -5.236 | +7.477655 |
| 2 | 3 / 4 | 412.901 | 425.833 | -12.932 | +5.215129 | -5.788 | -2.574654 |
| 3 | 6 / 5 | 458.435 | 416.867 | +41.568 | -0.523525 | +15.492 | +27.363258 |
| 4 | 7 / 8 | 394.682 | 404.263 | -9.581 | -4.292201 | -0.602 | -7.757486 |

There is no stable T advantage. In slow T6, the source callback and approximate
Audio envelope are slightly faster than S5 (-0.523525/-0.579255 ms), while four
common-read sums grow 28.833825 ms, SQL wall union grows 31.176790 ms and the
coverage remainder grows 10.391210 ms. COMMIT union contributes only +1.072418 ms.
The unfavorable window remains included. T6 is slow despite one stable data PID;
route stability alone does not establish performance. Source routes differ in
pair 3, further limiting a per-source client/server comparison.

The preallocated collector removes the first experiment's measured slice-growth
confound for both paths, but changes their common observation environment.
Absolute time/allocation differences between the two products are not isolated
code gains. CPU endpoints remain wider than the job and include observer work;
query-PID identity/runtime coverage is complete, but scheduler wait/timeslices
are still null. CPU totals, GC estimates and SQL wall coverage remain separate
views. This product does not confirm a poolRow-cursor cause or qualify T as a fix.

## T6 to T7: equal observed command work, different execution cost

The adjacent controls execute the same T path on the same Store/schema/pools.
Both have 883 application callbacks and 67 fingerprints; every fingerprint has
the same count and final RowsAffected distribution, with no query errors. Each
full-phase histogram is `{0:87, 1:794, 32:2}`; the two 32s are SELECT results.
Direct items/metadata no-op DML affects zero rows. No image or embedded-cache DML
occurs. Expected job/library/activity bookkeeping changes rows equally in count;
both create two temporary tables, ANALYZE once and drop two tables.

| Metric | T6 | T7 | T7 minus T6 |
| --- | ---: | ---: | ---: |
| Job, ms | 458.435000 | 394.682000 | -63.753000 |
| Application SQL union, ms | 376.648353 | 328.001410 | -48.646943 |
| COMMIT union, ms | 26.030706 | 28.033447 | +2.002741 |
| Go user/system CPU total, ms | 181.034000 | 157.411000 | -23.623000 |
| Actual query-backend CPU, ms | 303.262250 | 257.729811 | -45.532439 |

Lookup/bitmap use the same data PID throughout both windows; media/images use
the same owner PID. All four common-read medians and sums fall in T7, largely
after the source blocks. The approximate Audio envelope falls only 2.621414 ms,
compared with the job's 63.753 ms (13.9067%) decline. COMMIT coverage increases,
so smaller COMMIT coverage is not the explanation. The independent terminal
observation increases 0.100878 ms within the original polling boundary.

Both windows have four GCs; allocation differs by only 238,352 bytes. Query
capacity stays 32,768 and there is no Prepare. Those facts, small pool/GC pause
differences and CPU totals are not additive explanations of job wall time.

Equal SQL counts and final command-row distributions exclude observed extra
commands or final affected rows. They do not establish identical parameters,
plans, planning/executor cost, physical page activity or temporary-CTE insertion
work: that CTE's outer SELECT returns one while its inner inserted count was not
captured. Same PIDs do not establish identical scheduling or CPU conditions.
The data narrow the question but prove neither a hardware cause nor the cause
of the historical candidate regression.

## T6/T7 sequence proof and remaining workload limits

A separate frozen appendix matches all 883 application callbacks ordinal by
ordinal in template, physical PID, final RowsAffected and error flag. Record
index and QueryStart ordering agree. The 869 callbacks intersecting each job
also have identical selected template sequences. All 32 source queries occur
at ordinals 55, 58, ..., 148; first lookup is 53 and first following image
comparison 167 in both windows. A changed observed callback order/population,
PID sequence or row-tag sequence therefore does not explain this T6/T7 change.

The scanner calls File.ReadDir(64) without an explicit filename sort. Identical
recorded templates and unchanged directory membership do not prove the same
file/argument at each ordinal; the capture contains no such arguments. The
later equality contract's ORDER BY does not govern scan traversal. The middle
observer query also interleaves after 431 application starts in T6 versus 552
in T7. Each scan creates fresh job/generation and temporary-table state. No
argument, plan or physical-cost equivalence is inferred from the sequence proof.

## Third mechanism product: a favorable association under a new observer

One final independently frozen product reused the second product's effective
Go/source bytes and complete STTS/STTS workload. The new external 20 ms CPU
observer changes the measurement environment. All thirteen phase observations,
the equality contract and Store.Close qualified and were exported.

All eight controls have 883 application callbacks, 67 fingerprints, 32 single
snapshot SELECTs, zero application Prepare and fixed query/Prepare capacities
32768/150. Fingerprint counts, final RowsAffected distributions and application
template order agree across controls. Image/cache keys/xmin and the original
no-probe/no-DML guards remain. Equal template order still does not establish
identical arguments, temporary-relation state or plans.

All deltas below are T minus S. The eight original jobs and all four fixed
contrasts are retained; the prior product's mixed directions remain unchanged.

| Pair | T/S windows | T job, ms | S job, ms | Job delta, ms | Go endpoint CPU delta, ms | Actual-query PG CPU delta, ms |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 2 / 1 | 405.117 | 452.296 | -47.179 | -25.264 | -29.331505 |
| 2 | 3 / 4 | 399.957 | 464.952 | -64.995 | -54.440 | -29.275962 |
| 3 | 6 / 5 | 402.666 | 424.690 | -22.024 | -12.423 | -14.373959 |
| 4 | 7 / 8 | 392.137 | 411.026 | -18.889 | -18.532 | -3.131441 |

Source callback and approximate Audio envelope durations also fall in all four
contrasts. Much of the job difference remains outside Audio. Reverse observations
are retained: pair 4 terminal rises 1.086496 ms and image-read sum rises
0.682280 ms; pair 2/4 allocation rises 16,024/46,128 bytes. CPU endpoints remain
wider than jobs and include observer work. T2 has an observer-only backend in
its actual-query CPU total; that PID is excluded from application placement.
CPU, GC estimates and wall coverage are not combined into an exclusive partition.

This is a consistent association inside this observed process, not a portable
optimization estimate, poolRow-cursor isolation or replacement for either the
preceding mixed process or historical N=3 acceptance. No eight-window causal fit,
cross-case speed subtraction or favorable re-pairing is used.

## CPU conditions: observed variability, limited placement attribution

The actual before/after clock anchors give a conservative guest-minus-host hull
[+11.005488, +75.686960] ms, width 64.681472 ms. Analysis retains possible host
envelopes and guaranteed interiors rather than using a precise midpoint. A stable
observed backend-to-host link requires all bracketed placement observations across
its full possible alignment range to agree. That rule succeeds for only 3 of
423 complete application-backend observations, and 3 of 317 after-source/post-Audio
observations. Even these are sampled positions, not continuous residency.

All 423 application-PID reads inside the controls succeed. The 177 not_present
records begin 203.077354 ms after the final control ended, so they are not gaps
inside the application control windows. Physical identities remain stable;
first registration still does not prove an earlier atomic identity. Both
scheduler settings remain disabled, leaving wait/timeslices null.

Observed VM placement varies. Broad reported-frequency medians are all about
5.05 GHz, and low values also occur in fast T windows. Static maximum-frequency
groups are not newly measured core types, and the cpufreq driver's semantics
were not recorded. Sampled lastCPU and sysfs frequency cannot establish effective
execution frequency, task residency, cycles or a slow-core explanation.

Fast T2 has 13 aggregate steal ticks, 12 on guest CPU 9. Its runtime-advanced
application-PG observations show other CPUs, while Go-thread placement was not
sampled. This prevents assigning that aggregate stolen CPU-time to the observed
SQL critical path, but does not exclude unobserved Go execution on CPU 9. Slow
S1/S4 have lower steal totals; aggregate steal does not consistently explain the
ordering. Neither these facts nor sparse placement links prove a hardware cause
or exclude code-induced pacing and changing execution cost.

The adjunct itself uses approximately 3.81% of one guest CPU and 4.30% of one
host CPU over different sampler lifetimes. These measured observer costs can
perturb execution; they are not deducted from application CPU or used to normalize
old acceptance times. All controls, ambiguous mappings and null fields remain.
The new observation improves context but leaves the main mechanism unresolved.

## Decision and bounded next step

C and T remain withheld. Direct source savings and a mode association in some
conditions are insufficient to establish stable end-to-end benefit or the cause
of the old removal regressions. No shared setting was changed and no further
product is selected. Additional unrestricted benchmark rounds would not resolve
the missing attribution.

If work continues, select a bounded function/server-execution profile or
instructions/cycles observation with explicit intervals, to distinguish extra
work from changed execution conditions per unit of work. Per-parameter plans,
PMU evidence and Go-worker thread coverage are absent here. Do not introduce a
production fix or assign CPU/hardware causality before that distinction is made.

## Actual closeout and evidence

All three products and observers exited. The final source/export/closure receipts
are hash verified, with no extra products or replay. Actual cleanup reclaimed
the inactive private compiler cache from 330,342,400 to 8,192 bytes, owned scratch,
three empty ext4 fixture directories and 12 guest/14 host temporary copies whose
independent evidence was retained. Source/common backings, raw captures, matching
binaries, receipts, shared Go build/module caches and protected service/VM
identities remain. Reserve 403374 is unchanged. Final persistent availability is
330,231,808 bytes and guest available memory is 6,639,820,800 bytes; these separate
snapshots are not attributed wholly to cleanup. No runtime work remains active.

Evidence root: `.artifacts/embedded-source-mechanism-20261007`. The three
independent product export SHA-256 values are:

- First: `ed8deeefd2647073a27784ff1cf935ede9d23593613dbf0ab358015823ef0fb5`.
- Second: `85ebb8a6f8476e6c41e7fd1d9abc4de5d22d81ec67fecb498195bf5a273a1eb6`.
- Third: `f9914d04b94f9b859d319ff2446d089ef44c8cca3e3c90fb925a80fc4465cda7`.

| Frozen artifact | SHA-256 |
| --- | --- |
| `mechanism-report.md` | `916d653606a2d655433537c882da505556a04a1c56151b1ef279936d653a54be` |
| `reader-boundaries.md` | `2ad04c9c33cd04930a4b483d80bf57425e99df0b78dc1e5f392fa1c21773aec0` |
| `analysis/crossover-report.md` | `7f2207ec19b0a9f2cb1f3a78d54624d4dc7c26b2527dd5717dec778f323c0b22` |
| `analysis/crossover-summary.json` | `5615e4414976b5574662c14fe5215c84e0c1f0200c9dbea366c1d94ea33b8336` |
| `analysis/pid-audio-appendix.md` | `00300826df2bed48147183230a11b010294adc7fc66433582e061c372ea7f03d` |
| `analysis/control-pg-storage-context.md` | `8d5979732e2b09299164121de147d68afb8752bcb99c79010541693cb63f448a` |
| `evidence/binary-inspection/inspection-receipt.json` | `ac100966c25712b0a95e88fe472531532cd117e93ef69a9a7064df0ee6153725` |
| `pool-row-control/diagnostic-plan.md` | `20e3ff471dc6a1f6cf1a7f5b83537eb812fdd065e7850d8e0127a2f5c55dbf4e` |
| `pool-row-control/analysis/pool-row-report.md` | `f84f9a7027e8064b0d1bf170f13c695314d37fa6f825fcfcc5522b49a4e0ee70` |
| `pool-row-control/analysis/crossover-summary.json` | `5d0809529f077a3499ce2f62113cefaaa4698a2cd86b919ef5476906b6fc788a` |
| `pool-row-control/analysis/t6-t7-workload-appendix.md` | `4e66e00bf816c84f7b4a8f11a051892f5c4fdee21a6ea0592d2ec51366b200ac` |
| `pool-row-control/analysis/t6-t7-workload-appendix.json` | `fab158c9806db12c1997940a895ce517b2ccef04f7b1abfaf83f9542d2f6c1ed` |
| `cpu-placement-observer/diagnostic-plan.md` | `8edc1a7ebaa0c447100a237e0c0b6a6afa48e64642b461af54c7727d0cb6f5e5` |
| `pool-row-control/analysis/sequence-appendix.md` | `ef4d225ca156bc747ca7a05f2f402ee857e8df3d3ffaf39dad66583b95687c83` |
| `pool-row-control/analysis/sequence-appendix.json` | `f4466fbe79562fb71fd09601be53b027006140bd24d04a5e976a57b15831f4d7` |
| `cpu-placement-observer/analysis/cpu-observer-query-report.md` | `db3fc572893229764e8c5e82f31bea005e1e27ee151d715586a55f9e238f2c5c` |
| `cpu-placement-observer/analysis/cpu-observer-query-report.json` | `1d435b0f1a7eefa9a9faba4540f0f42754e2cd64f96d1d928396ca26259308fb` |
| `cpu-placement-observer/analysis/cpu-condition-report.md` | `7a38ccaffa8b1a7edb2f944d5e15f9662d2ebfc01cf55524c98adfcd990e2ef4` |
| `cpu-placement-observer/analysis/cpu-condition-report.json` | `b60bda91ae284624c121b5c85381a95c1ee61438b5cbe58658ba4eb9c045df86` |
| `cpu-placement-observer/actual-close-export-verification.json` | `5c78a9ac7ab31a5ee539a64a64e3c05027ce204168e0a41ee3c9066652084e6f` |
| `cpu-placement-observer/closure-guest.json` | `4ab6d5a99c28ef3122d0d900a895d7a547bce62d3f9242907f9ea974fbc91123` |
| `cpu-placement-observer/closure-host.json` | `c9f54f32df9d0b00b5b53600265910aa806df4e5b7c8692caf4278e4159948c1` |

The earlier report is preserved below from main e2709386, original SHA-256
`95107962f95a1e7a309422f6442c73305e7d76ab712843157ed10b601dffb2b5`.
Its original stage 1-4 numbers, adverse samples, source decision and completed
historical closure have not been rewritten. This report and the handoff are the
only publication changes; original workspace WIP remains outside publication.

---

# Embedded-source removal diagnostics, 2026-10-07

Seven qualified remote Go processes narrow the removal-regression question but
do not establish its historical cause. The first two diagnostic pairs reproduce
a slower candidate; the final CPU pair reverses direction. The candidate remains
withheld from main. No production source was changed in this follow-up, and its
final publication contains only this report and the handoff.

Baseline B is `6386c8fd88585557ee65acbc0a12be23d90a41d4`; candidate C is
`cd22752397ff70b490b27f9b7536dfddb57d737f`. Publication starts from main
`de83ff4b50a90f2573b3fe7a86710ca8e91387b9`, whose runtime remains B. The earlier
[acceptance report](scan-embedded-source-readonly-20261007.md) and its three
adverse C/B removal pairs remain unchanged. New diagnostic timings do not replace,
re-pair or pool those samples.

## Selected work and qualification

| Stage | Fixed selection | Purpose | Qualified Go processes |
| --- | --- | --- | ---: |
| 1 | B then C | SQL callback attribution | 2 |
| 2 | C then B | Prepare and connection-description attribution | 2 |
| 3 | C only | Private-data statistics and plan sensitivity | 1 |
| 4 | B then C | Removal CPU endpoints and backend coverage | 2 |

All seven retained the complete original five-phase workload, correctness,
probe/tuple, retirement and Store.Close guards. Stage 3 then replayed reads after
Store.Close. All selected products qualified, with no extra product or replay.
Stage 4's scheduler metadata remains partial; that limitation is separate from
product qualification. No shared PG, VM, mount, swap, cache or durability setting
was changed. Only stage 3 changed autovacuum options and statistics on its own
new fixture, which was subsequently removed.

Each B phase uses 32 BEGIN/source SELECT/cache SELECT/COMMIT groups. C replaces
them with 32 single SELECTs, removing 96 application callbacks and 32 explicit
read-only transaction-boundary pairs per phase. These are not 32 avoided WAL
flushes. Startup permission grants and actual writer semantics remain unchanged.

## Stage 1: the replacement callbacks save time; other SQL grows

The SQL-only pair has complete query lifecycle captures, with no incomplete or
dropped records. Its removal measurements are:

| Measurement | B, ms | C, ms | C minus B, ms |
| --- | ---: | ---: | ---: |
| Persisted job | 421.522000 | 447.276000 | +25.754000 |
| Application SQL union, clipped to the job | 346.926488 | 371.302717 | +24.376229 |
| Job outside that SQL union | 74.595512 | 75.973283 | +1.377771 |
| Old embedded commands / new embedded SELECT union | 50.596160 | 43.332010 | -7.264150 |
| Other application SQL union | 296.330328 | 327.970707 | +31.640379 |
| COMMIT union, included in SQL union | 27.765743 | 24.033305 | -3.732438 |

The 7.264150 ms saving measures callbacks, not the entire embedded-source path.
Old JSON/helper work occurs between the two SELECT callbacks; new JSON/helper
work follows the single callback. The old BEGIN-to-COMMIT envelope and new
single-query interval therefore cover different work. Uncovered wall time is
not Go CPU.

Four unchanged templates retain equal counts: stored-item lookup, 160 calls,
grows 12.648726 ms; unchanged-image comparison, 136 calls, grows 8.252015 ms;
media facts/subtitles, 160 calls, grows 4.779149 ms; bitmap existence, 128 calls,
grows 1.937579 ms. Their individual unions can overlap and are not additive job
components. This localizes the observed increase without explaining its cause.

Common-read PG samples are active with no reported wait, which does not isolate
CPU, planning, execution, transport or row consumption. Removal has no sampled
Lock/LWLock/data-I/O explanation in this pair, and its broad C device-flush context
is not slower overall. A separate force WalSync tail cannot explain removal.
C-owned items/item_images auto-ANALYZE was sampled in warm; its causal relevance
was an open hypothesis, rather than a demonstrated statistics or plan change.

## Stage 2: no description miss in the four main common reads

Removal is again slower for C: 427.404 versus 411.274 ms, a +16.130 ms difference.
SQL union grows 6.928745 ms and the uncovered remainder grows 9.201255 ms.
All 12 observed connections report `cache describe` and description/statement
capacities of 512/512. C's 125 and B's 121 Prepare records are unnamed and have
valid parents, with no errors, missing/incomplete/dropped records or unknown
parents.

C has no removal application Prepare callback. B has two theme-resource
callbacks totaling 0.292023 ms of job-clipped union, unrelated to the four common
reads. Those four templates have zero removal Prepare callbacks on both sides.
This does not support description-cache misses as their slowdown mechanism.

Prepare is nested inside its parent query, not additional time. Its absence
does not mean no server Parse/planning or reuse of a named generic plan.
`cache describe` retains client descriptions; query-minus-Prepare still includes
transport and scheduling. The measured connection settings apply to stage 2,
not retroactively to stage 1.

Early C cold/warm/force COMMIT tails have associated WalSync samples and elevated
device-flush context. Removal had returned to a different flush regime, so those
early tails do not explain its common-read increase. No private auto-ANALYZE was
sampled in stage 2; short activity could occur between samples. Original explicit
ANALYZE of the reconciliation temporary table is a separate operation.

## Stage 3: visible statistics changes, without a sampled bad-plan switch

A new C fixture disabled only its own tables' autovacuum before load. After the
five original phases and Store.Close, one dedicated backend replayed the same
456 common SELECTs per window in fixed order: N1/N2, ANALYZE items, I1/I2,
ANALYZE item_images, IA1/IA2. Every first query was retained. Six fixed parameter
slots had two EXPLAIN rounds per state, yielding 36 sanitized plan samples.

Statistics visibility changed as intended. N has no relevant column statistics,
I has items statistics and IA also has item_images statistics. Schema/table
identities and all private ctid/xmin rowsets remain unchanged, and replay results,
ordinals, templates and row counts match. All six sampled slots retain the same
compared node-tree shape across states and rounds. Estimated widths change
(lookup 506 to 1646; media 81 to 1305), without a demonstrated poor-plan switch.

| State | First sample sum, ms | Second sample sum, ms | Second minus first, ms |
| --- | ---: | ---: | ---: |
| N | 175.689081 | 170.710833 | -4.978248 |
| I | 165.127657 | 161.633631 | -3.494026 |
| IA | 164.770419 | 158.100193 | -6.670226 |

N2 to I2 is -9.077202 ms and I2 to IA2 is -3.533438 ms, but I2 to IA1 rises
3.136788 ms. Fixed order, first use and same-state drift prevent assigning the
full reduction to ANALYZE. Replay timing includes client query/row-consumption
work, and N1 constructs a baseline whereas later windows compare against it.
The experimental five-job timings are not B/C acceptance samples. These results
do not establish a statistics-induced cause for earlier reversals or justify
tuning shared autovacuum, cache capacity or durability.

## Stage 4: lower measured removal CPU, with the timing direction reversed

The final B-to-C pair restores the natural fixture. Only removal adds resource
endpoints before admission and after the original terminal/resource measurement;
there is no statistics intervention, extra SQL, profile or runtime trace.
All five phase results are retained:

| Phase | B job, ms | C job, ms | C minus B, ms |
| --- | ---: | ---: | ---: |
| cold | 3256.795 | 3291.161 | +34.366 |
| warm | 414.795 | 407.218 | -7.577 |
| force | 2176.638 | 2280.314 | +103.676 |
| image_changed | 419.992 | 407.962 | -12.030 |
| image_removed | 442.302 | 405.168 | -37.134 |

Removal SQL union falls 366.814110 to 334.235320 ms (-32.578790), and its uncovered
remainder falls 75.487890 to 70.932680 ms (-4.555210). The old embedded callbacks
occupy 49.540564 ms versus 38.686927 ms for the new SELECTs, a 10.853637 ms callback
saving with the same scope limitation as stage 1. This pair does not reproduce
the earlier removal regression; cold and force are still slower.

| CPU counter | B, ms | C, ms | C minus B, ms |
| --- | ---: | ---: | ---: |
| Go process user | 117.640000 | 123.923000 | +6.283000 |
| Go process system | 59.429000 | 42.579000 | -16.850000 |
| Go process total | 177.069000 | 166.502000 | -10.567000 |
| Actual removal-query backend runtime sum | 291.562103 | 266.189307 | -25.372796 |

Go rusage brackets approximately 504.26/504.30 ms, wider than the persisted jobs,
and includes all Go threads and diagnostic/observer work. Each backend counter
has its own read bracket and can include untraced Ping and observer work.
QueryTracer does not identify PostgreSQL parallel worker PIDs. These CPU counters
are not exclusive job CPU and cannot be added to SQL wall time or to each other
as a wall-time partition.

All actual phase-query PIDs have stable identities and CPU coverage. Historical
PIDs are preserved but excluded from totals; mixed application/observer PIDs
cannot be split into CPU buckets by query count. Both endpoints report scheduler
statistics disabled, so runqueue wait and timeslices remain null. Resource
status is partial despite complete query-PID coverage; null wait is not zero
wait. Runtime GC total estimates are 8.589241/9.748357 ms for B/C and include
mark-assist estimates of 0.173720/0.256522 ms. They are not subtracted from rusage
or added to each other and do not attribute CPU to a specific decode/allocation
function.

Backend count alone also fails to explain the direction:

| Diagnostic pair | B/C application backends | C minus B removal job, ms |
| --- | --- | ---: |
| Stage 1 | 2 / 3 | +25.754 |
| Stage 2 | 3 / 2 | +16.130 |
| Stage 4 | 3 / 2 | -37.134 |

The owner backend consistently carries 136 image-noop and 160 media-facts reads;
item/bitmap reads can spread across one or two data backends. The same backend
count pattern has opposite latency directions. The old acceptance batch did
not capture this distribution, so later runs cannot reconstruct it.

## Decision and actual closeout

The diagnostics support direct SQL callback savings and lower measured CPU in
the final removal pair, but not a stable end-to-end gain, an explanation of the
old adverse pairs, or a fixed regression. C remains unmerged. No further product
was selected or started; do not repeat the completed matrix or tune shared
settings to obtain a preferred result. Further work requires a bounded mechanism
question and fresh acceptance evidence before reconsidering source publication.

All seven products, workers, observers and host samplers exited. Actual closeout
reclaimed the inactive private compiler cache from 640,585,728 to 8,192 bytes,
owned compiler scratch, empty ext4 media fixtures and independently hash-retained
temporary RAM copies. Sources/common overlays, archives/manifests, raw evidence,
matching binaries, receipts and shared caches remain. PG, Goby, QEMU and reserve
403374 identities/settings are unchanged. Final persistent availability is
279,609,344 bytes and guest available memory is 6,651,854,848 bytes; these are
separate observations, not a measured total cleanup gain. No runtime work remains
active. Original workspace WIP remains outside publication and must be protected
using fresh identities.

## Evidence

Local evidence root: `.artifacts/embedded-source-attribution-20261007`.
The reports and machine summaries bind original runs, SQL/Prepare/resource
captures, plan samples and passive context receipts; earlier raw evidence is
unchanged.

| Artifact | SHA-256 |
| --- | --- |
| `analysis/attribution-report.md` | `6eb6b53557bed45bfde816d24afb5aba76a47a1b3c076e58ca5d13572cdd2414` |
| `analysis/pg-storage-context.md` | `a59248b6241253ef4535680706ef9def44f186a06a6b10adae3dbf29ed22337a` |
| `stage2/analysis/prepare-report.md` | `8e71c69b34ae955f64f3f9c0bfa7ab9615266ce6c84a7b51338346c5d4d561f5` |
| `stage2/analysis/pg-storage-context-stage2.md` | `9049f99f947fb25ee139d397ec8cbdb6a8ee01639bbe79333fb2d2ffa6cf8d3d` |
| `stage3/analysis/statistics-report.md` | `5fd4bae1058c9a84d3284c9d98e661cc14264f0957bf3f3a636bf815545e1cef` |
| `stage3/analysis/statistics-summary.json` | `d5bb77815b278b5a588da503a33035d41e37b7c915af13398793421cf0d4c7f2` |
| `stage4/analysis/cpu-report.md` | `563c2e9f02b2ac24983063ce6599faca9b6190b32ec09eecbe76395e6c468d0d` |
| `stage4/analysis/cpu-summary.json` | `accc6b621de6fb2a03b9b5b4013043268b1541e41b00e8f1cd51875b9fe0c0e9` |
| `actual-closure-receipt.json` | `763b0af62d2ac5b93f67019b24ba051e3ab6c19892952cfa76b42061b9a9b74c` |
| `actual-close-export-verification.json` | `01402fdf1c596a60bc59127e1f6e77353ea57a46d1a590d87baa7bfadb0170c4` |
