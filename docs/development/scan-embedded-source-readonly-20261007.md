# Single-statement embedded artwork scan evaluation

The candidate meets its correctness and SQL-count goals, but it is retained on
codex/embedded-source-readonly-20261007 and is not merged into main. The deletion
phase is slower in all three fixed C/B pairs, and the available storage evidence
cannot explain every reversal. This is not accepted as a demonstrated latency
optimization. Main retains runtime 6386; only this report and the handoff are
selected for publication.

Embedded artwork scans previously opened a repeatable-read, read-only
transaction, loaded the indexed source, queried cache metadata, and committed.
Both SELECTs needed one consistent database view. A single SELECT can provide
that view directly, removing two transaction commands and one SELECT per item.

## Implementation and preserved behavior

The new statement joins the primary item, its current root and optional embedded
artwork cache metadata. It reads the complete source facts used by the existing
snapshot checks plus source hashes, cached status and extraction version. It
does not read cached artwork bytes, catalog projections or subtitles.

Two pure snapshot helpers retain the same validation stages used by public media
opens and delivery revalidation. Public reads still attach requested subtitles
between those stages. The internal scan uses its immutable startup permission
grant. Source path, root, probe version, file identity, size, modification time and
change time checks remain, including the physical proof after SQL waits.

Cache hits remain read-only. Force extraction, actual cache writers, publication
locks, final source checks and transactional notifications keep their existing
behavior. One statement still has PostgreSQL's implicit statement transaction;
this change removes explicit read-only transaction commands, not real writes or
a fixed number of WAL flushes.

The former two-query path handled errors asymmetrically: failures in the source
read became warnings, while cache-query and transaction failures were returned.
The combined query now returns SQL, Scan and transport failures. Missing
rows and malformed or invalid indexed media snapshots remain warnings and skips.
A regression test triggers a real missing-table SQL error in its isolated schema
and checks that it cannot become a successful invalid-source warning.

## Verification design

Source commit cd22752397ff70b490b27f9b7536dfddb57d737f is a direct child of
main 934abbb4791925f8197f1eaf9895e9c720c2bf5b (runtime 6386). Its four files
match the executed source freeze; the source commit is bound by a separate
addendum without changing frozen inputs or raw evidence.

All verification runs on test-env. The frozen candidate contains two production
files and two regression files. A focused race run selects 22 top-level tests:
embedded source/cache and extraction contracts, public media revalidation, and
operation-authority lifetime checks. All 22 top-level tests and 66 subtests passed
under the race detector, with no skipped or failed test.

A is ab0282616de26f0000813d7667356e845eceaf62, before image no-op acceptance.
B is 6386c8fd88585557ee65acbc0a12be23d90a41d4, the published image no-op change.
C adds the current embedded snapshot change to B. The fixed matrix is one
candidate race invocation, two TRACE1 counts (B/C), then nine TRACE0 timings in
ABC / BCA / CAB order. Counts are not used for timing. C/B measures the current
change, B/A observes the preceding image no-op change, and C/A only observes
their combination.

All sources share one test-only logger overlay that records existing job start
and finish timestamps. It changes no work, SQL, assertion, elapsed definition or
resource window. The original five phases, diverse media, native probes,
committed image keys/xmin, worker retirement and Store.Close checks remain.
The logger overlay is evidence infrastructure and is not a product source change.

## Measured work reduction

Both TRACE1 runs qualified. Every phase saves exactly 96 SQL callbacks and 32
explicit BEGIN/COMMIT pairs. Image DML commands, affected rows, committed keys
and xmin changes are identical between B and C, and all rollback counts are zero.
Native media probe counts remain 160 / 0 / 160 / 0 / 0.

| Phase | Raw SQL B / C | Explicit BEGIN and COMMIT, each B / C |
| --- | ---: | ---: |
| cold | 6523 / 6427 | 382 / 350 |
| warm | 979 / 883 | 56 / 24 |
| force | 5096 / 5000 | 246 / 214 |
| image_changed | 990 / 894 | 57 / 25 |
| image_removed | 990 / 894 | 57 / 25 |

The fixture contains 32 no-picture audio tracks. This count reduction covers
source/cache lookup, not the throughput of extracting large embedded images.
Warm and force image DELETE/UPSERT commands remain zero; changed and removed
phases retain one of each. Cold retains 136 of each. These are explicit command
counts, not disk flushes or JSON expression evaluations. The existing time-based
progress checkpoint can affect counts in other executions.

## Timing results and publication decision

All twelve selected invocations qualified: one race run, two count runs and nine
timings. There were no skipped tests, replayed products or additional timings.
The following job times retain all three blocks. Positive changes mean slower.
Percentages are medians of the three fixed paired percentage changes, not ratios
of source medians.

| Phase | A job ms, blocks 1/2/3 | B job ms, blocks 1/2/3 | C job ms, blocks 1/2/3 | C/B median change | B/A median change |
| --- | --- | --- | --- | ---: | ---: |
| cold | 3150.726 / 5573.881 / 3260.852 | 3218.504 / 3179.040 / 3195.390 | 3197.783 / 3181.400 / 4690.947 | +0.0742% | -2.0075% |
| warm | 724.241 / 1921.047 / 744.941 | 449.680 / 397.819 / 398.953 | 407.830 / 401.040 / 403.299 | +0.8097% | -46.4450% |
| force | 2635.399 / 4433.833 / 2518.508 | 2203.191 / 2161.618 / 2185.451 | 2165.011 / 2920.685 / 2190.905 | +0.2496% | -16.4001% |
| image_changed | 722.564 / 1676.706 / 731.943 | 413.108 / 439.863 / 405.980 | 401.502 / 507.693 / 392.637 | -2.8094% | -44.5339% |
| image_removed | 768.690 / 1834.439 / 748.998 | 398.687 / 421.132 / 420.253 | 443.719 / 480.333 / 433.505 | +11.2951% | -48.1342% |

C/B job paired median absolute changes are +2.360, +3.221, +5.454, -11.606 and
+45.032 ms in table order. Removal reversals are +45.032 / +59.201 / +13.252 ms
(+11.2951 / +14.0576 / +3.1533%). Its observed-terminal differences are smaller:
+0.829 / +9.890 / +2.310 ms, with a paired median of +0.4584%. That separate
observation window does not replace the job measurement or erase its reversal.
C2 force is +35.1157% and C3 cold is +46.8036%; both remain in the main table.

Removal allocation rises by 96,144 / 115,424 / 202,216 bytes. The corresponding
GC-pause increases are only 0.017 / 0.137 / 0.040 ms. Empty-pool waits are zero,
pool acquire durations are about 0.09-0.12 ms, and retirement waits are 9-14 us.
These recorded quantities do not explain the 13-59 ms job differences. No SQL
spans or CPU profile were collected, so the evidence does not establish a query
plan, scheduling, mutex or other precise cause. Complete resource and terminal
arrays, including all reversals, remain in the machine report.

B/A improves every warm, force, changed and removed job and observed-terminal
pair. Its second A run falls in slow storage, so the whole-batch percentages are
not uniform code-only estimates. The first and third pairs also improve those
four job phases; they do not establish behavior under comparably slow storage.
Cold B/A includes a +2.1512% first-pair reversal and does not demonstrate uniform
cold acceleration.

Root and independent review found no specific correctness defect in the
single-query design, but that is insufficient to accept it as a performance
improvement. Preserve the candidate and its measurements. Investigating the
removal reversal is the next prerequisite before reconsidering its merge; this
batch does not select new instrumentation or additional performance runs.

## Comparable slow-storage observations

The rule was declared before the runs. Passive host observations are grouped
into fixed one-second bins. A qualifying bin has at least five physical NVMe
flush completions, mean completion time at least 3 ms, no counter reset or missing
coverage, and no adjacent sample gap over 400 ms. Three consecutive qualifying
bins form a plateau. A same-block, same-phase pair qualifies only when both
complete job intervals, expanded by measured clock uncertainty, lie inside the
same plateau and their equally weighted median covered-bin means have a ratio
between 0.8 and 1.25.

QMP is a cross-check. The main nine observations are retained regardless of this
label. It is an operating-context comparison, not controlled external disk
latency: the code can itself change flush demand. Missing anchors prevent a
match, and no match means the selected batch did not quantify comparable natural
slow-window behavior. No additional run is selected to obtain a match.

The completed classifier found zero matched phase pairs out of 15 B/A and zero
out of 15 C/B, with zero matched blocks. It retained 197 bins: 167 not-slow,
27 slow and three with insufficient completion counts. One plateau conservatively
covers 2026-10-06 19:23:43.944 through 19:24:10.958 UTC. Only C2 changed/removal
and all five A2 phases lie completely inside it; no B phase does. Pairing C2
against A2 would change the selected comparison and cannot isolate either change.

C2 force crosses the transition into that slow interval, while C3 cold crosses
its exit. Those observations have changing storage backgrounds. C2 removal is
slow versus a non-slow B2, but both ends of the first and third removal pairs
cover only not-slow bins. Not-slow means below the declared threshold, not
identical controlled latency. The recorded storage context cannot account for
all three removal reversals. The guest-minus-host clock bounds span approximately
-2.789 to +692.168 ms; classification uses the expanded interval, not a precise
clock offset.

The fixed batch is complete, but image no-op benefit under comparable natural
slow storage remains unquantified. No additional run, relaxed threshold or
post-hoc re-pairing was used to obtain a match.

## Evidence and lifecycle

Evidence is retained under .artifacts/embedded-source-readonly-20261007.
The environment uses the explicitly selected initially empty private RAM build
cache, shared modules, separate compiler scratch and ext4 media fixtures. It is
not pooled with older shared-cache timings. No shared PostgreSQL, VM, swap,
filesystem, cache or durability setting is changed. Device-internal diagnosis and
real-write batching remain outside this request.

The count summary SHA-256 is
f1d1cc664639cc24116a6150b9f2804ce44626f50f8193dc34d6ebdfefd2bdb4;
the timing summary is
fdefe94c7caca75db2455980f347e7f654b284573f5738fe1ecbef74e7bf9a16;
the slow-storage classification is
8616733038b2bda17a662c9b2176d3fc01f48ae8b0140b3e52ccf2bd69e23b0c.
The root independently recomputed all ten job comparison rows and the five
SQL reductions from the exported observations. One offline parser invocation
initially received the enclosing measurement-freeze hash instead of the source
freeze; it refused that binding, then rebuilt the same immutable evidence using
the correct source-freeze input. No product was replayed.
Export and authorized closeout are complete. All 70 persistent exports, nine
guest RAM exports and five host RAM exports matched their remote SHA-256 values.
Four matching binaries are independently retained. Both passive collectors
completed: 944 guest and 992 host samples, with zero QMP gaps.

After all workers and collectors exited, the private build cache was reclaimed
from 689,840,128 to 8,192 allocated bytes. Empty compiler scratch and ext4 fixture
directories, plus the independently retained temporary RAM copies, were removed.
Source backings, raw evidence, databases, shared build/module caches and reserve
403374 remain. PostgreSQL, Goby and QEMU retain their original process identities.
Final persistent availability of 289,538,048 bytes is a snapshot, not an amount
attributed to cache cleanup. No further runtime product is selected.

Guest closure SHA-256:
b6eba82846115ba5409540899b40a738eb40fe4cf895b425afb74b55ff7299ff.
Host closure:
45ed9ec1d9b139cb210e63a0de25c48df72c94c58068c937379e22b893542280.
Complete export:
adb9285b5e84c5648a995578ee86c6b3a35f0e36334199ed8ef50a22f89dab0d.