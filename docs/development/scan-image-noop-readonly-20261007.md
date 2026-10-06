# Read-only acceptance for unchanged scan images

The previous image optimization skipped redundant image DML but still opened
an owned transaction, locked job/root/item rows and committed for every image
set. A subsequent same-binary diagnostic reproduced longer WAL acknowledgement
and host NVMe flush completion times. This change removes the write-transaction
boundary when the complete local image rowset is already correct.

## Implementation

The read-only check uses one statement on the existing owner connection. Its
mutex is held only for the query and complete result consumption. The query
checks the current item/library/root association and granted physical root
mapping, and compares all replaceable raw fields, including obsolete-root rows,
private metadata and missing or extra indexes. The existing writer shares this
comparison expression, avoiding two definitions of equality.

An equal snapshot is the database acceptance point for a no-op. The caller
releases the connection mutex, then performs the original final directory/file
proof and operation-lifetime checks. A file replacement during the SQL wait is
therefore still rejected. A writer after the snapshot is later work and cannot
be overwritten by this no-op. This retains the existing filesystem predicate;
it does not introduce a global filesystem/database atomicity guarantee.

A mismatch enters the original owned transaction and re-reads current state
under its original task/root/item locks. Actual image mutations and catalog
notifications still commit or roll back together. Invalid image types retain
their previous populations. The existing known-absence hint only routes newly
populated sets directly to the ordinary writer; it cannot prove equality and
does not add a precheck to every cold image set.

Operation permissions continue to use the immutable startup grant. The no-op
does not write progress or repeat per-image task-relation locking queries.
Database-only stop and association changes remain governed by the existing
64-item/500ms cooperative checkpoint, error repair and finalization policy.
In-memory cancellation, Close and owner loss are still checked. Actual writers
retain their fresh publication checks and durable atomicity.

## Verification and work counts

Source commit `6386c8fd88585557ee65acbc0a12be23d90a41d4` is a direct child of
base main `ab6753831fb20117fc0332984b9e82ccd83f2432` (runtime
`ab0282616de26f0000813d7667356e845eceaf62`). Its two production and four test
files match the frozen and executed overlay. Root and independent static review
passed. All nine remote invocations qualified: focused race with forty required
top-level tests and fifty subtests, zero skips; two separate TRACE1 count runs;
and six TRACE0 timing runs in fixed BC/CB/BC order. No product was replayed.

Coverage includes snapshot acceptance, the post-query file proof, released-owner
concurrency, uncommitted and later writers, mismatch re-read, commit rollback,
operation lifetime, cooperative cancellation, private-field/old-root/index
repair, invalid-type retention, notifications and cold hint routing. The original
four performance drivers remain unchanged. SQL_TIMING is disabled throughout.

| Phase | Raw SQL B / C | Explicit BEGIN and COMMIT, each B / C | Image DELETE / UPSERT commands, each B / C |
| --- | ---: | ---: | ---: |
| cold | 6523 / 6523 | 382 / 382 | 136 / 136 |
| warm | 1659 / 979 | 192 / 56 | 0 / 0 |
| force | 5776 / 5096 | 382 / 246 | 0 / 0 |
| image_changed | 1664 / 990 | 192 / 57 | 1 / 1 |
| image_removed | 1664 / 990 | 192 / 57 | 1 / 1 |

All ROLLBACK counts are zero. Warm/force remove 136 explicit image transactions
and 680 commands. Changed/removal remove 135 such transactions and 674 commands:
the one genuinely changed image set adds a precheck before the original writer.
Cold adds no prechecks. Actual image changes remain conserved: warm/force retain
all 672 row versions; changed updates one row; removal deletes one key and updates
three shifted rows, leaving 671. The observed count runs have no extra progress
checkpoint difference. Other executions can vary with the existing time boundary.

Projection-containing query counts fall from 136 to 0 in warm/force and 137 to 2 in
changed/removal. They count SQL callbacks containing the expression, not JSON
evaluations; the previous exact-match CASE already avoided building the JSON.
Explicit COMMIT counts are not physical flush counts, and a pure SELECT still
has PostgreSQL's implicit statement transaction. Ownership Ping is outside the
raw QueryTracer count. Count-run elapsed values are excluded from performance.

## Performance and storage conditions

Every timing retains the original diverse corpus, task-owned=0, image keys/xmin,
retirement and Store.Close checks. Probe calls remain 160/0/160/0/0 with peak
cohorts 2 for cold/force. The 288 initial digests and one replacement fit in the
existing 512-entry cache; this does not exercise eviction pressure. Native media
work remains. No new profiler, full SQL span collector or HTTP benchmark ran.

| Phase | B job ms, blocks 1/2/3 | C job ms, blocks 1/2/3 | Median paired change | Median delta ms | Slower job pairs |
| --- | --- | --- | ---: | ---: | --- |
| cold | 3202.916 / 5186.821 / 5508.545 | 3222.535 / 3200.286 / 3219.606 | -38.2997% | -1986.535 | 1 |
| warm | 776.599 / 1689.159 / 1459.515 | 451.632 / 414.231 / 404.712 | -72.2708% | -1054.803 | None |
| force | 2517.664 / 4540.209 / 3048.644 | 2179.173 / 2251.455 / 2153.334 | -29.3675% | -895.310 | None |
| image_changed | 719.102 / 1516.025 / 741.851 | 401.776 / 396.079 / 445.501 | -44.1281% | -317.326 | None |
| image_removed | 712.900 / 1649.614 / 741.179 | 409.902 / 412.222 / 400.571 | -45.9549% | -340.608 | None |

Percentages are median(C_i/B_i-1), not ratios of medians. All observations,
including slower results, remain in their original order. Storage conditions
are materially different in pairs 2/3, so these medians are not uniform code-only
speedup estimates. Cold work counts are unchanged; no cold acceleration is
established by its large median difference.

Whole-invocation QEMU/NVMe flush means in pair 1 are B 0.770/0.706ms versus
C 0.819/0.741ms. With these similar storage averages, observed changes are warm
-41.8449%, force -13.4446%, changed -44.1281% and removed -42.5022%. Cold is
19.619ms/+0.6125% slower. This is one pair, not a precise population estimate.

In pair 2, C runs with QEMU/NVMe means 0.787/0.714ms before B encounters
5.712/5.460ms. In pair 3, B is recovering at 3.641/3.425ms before C runs at
0.809/0.726ms. These are conservative whole-invocation storage observations,
including preparation/cleanup, not exact job or SQL attribution. The collector
does not contain SQL spans. Candidate behavior under a comparably slow flush
window has not been measured, so its tail improvement is not quantified here.
There was no warmup, quiet-period selection, artificial checkpoint, re-pairing
or extra run to obtain a favorable storage window.

Warm/edit/removal allocation falls about 11% and force about 0.99%; cold allocation
median rises 0.0312%. Allocation is not peak memory or CPU. Cold GC/pool values and
several worker-retirement metrics also have reverse samples. Job, observed
terminal and worker retirement are separate windows; complete arrays remain in
the machine report rather than being hidden behind the job summary.

## Evidence and closeout

Both variants use the same explicitly selected, initially empty task-owned RAM
build cache with shared modules. Compiler scratch is separate tmpfs storage and
media fixtures remain ext4. This avoids the ordinary-cache persistent shortfall
without deleting retained evidence or changing database/VM/storage settings.
Previous shared-cache runs are not pooled with these results.

Evidence is retained under `.artifacts/image-noop-readonly-20261007`.
`analysis/counts-summary.json` SHA256 is
`f01cecb3f611aba37322ab24a86e40d66aa538a7cf50c25e6d976ee61c667bf4`;
`analysis/timing-summary.json` SHA256 is
`eeedc243ab978ed9a4fb7eceeda73a8992a0fa20b4f6bfa24a94c7e820b125ac`.
Root independently recomputed the five first-pair changes, paired medians and
negative-pair counts from the observations and rechecked all six source hashes.
The source-commit binding is an addendum; original pre-commit null fields remain.

SSH authentication interrupted export after the race invocation. The user
restored the agent; the original result was exported and only the remaining
eight products ran. Original failures are retained. An export-manifest correction
excluded interpreter bytecode while retaining the original manifest and copies;
it changed no product, source or observation.

All workers and both observers exited. The private compiler cache was reclaimed
with the pinned Go tool from 627486720 to 8192 allocated bytes. Only owned inactive
scratch, empty ext4 fixture directories and independently retained, hash-verified
RAM copies were removed. Source/backings, binaries, raw evidence, databases and
shared caches remain. Protected service/VM identities and reserve 403374 are
unchanged; final persistent availability is 293621760 bytes, a snapshot rather
than a future reservation. Guest closure SHA256 is
`d939e9e41ef33ac7bcb60617e8d245b68260dd2c57d0efec115bf05fa6ec4756`;
host closure SHA256 is
`aef979fa9e44155030c4f63ecd55eeb59049d714c767f8c2a46df4bc512f3b17`;
final export SHA256 is
`5e80ae669e0a52198f5e5cde22fd4e34a86349d3c16c5bb112d3c5833c343cf3`.

Real-write batching, the 32 pure read-only embedded source snapshots and the
internal cause of NVMe flush variability remain separate, unselected work.
