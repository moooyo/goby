# Scan SQL reduction, 2026-10-06

This first batch removes redundant database work while retaining the existing
publication transaction and final source proof. Baseline is main
`1a9ec5f1ff06b91efdc951ac864e5c56c3286cbb`, whose runtime equals b100. Candidate
`ab0282616de26f0000813d7667356e845eceaf62` is its direct child and matches the
ten frozen source files used remotely: five production files, two adjusted
existing tests and three new tests. The four performance drivers are unchanged.

## Implementation

Embedded artwork scans reuse the existing narrow media-source reader with
subtitle attachment disabled. Full source facts and the original validation,
extraction, publication and notification boundaries remain.

Theme and extra invalidation skip their UPDATE when the current transaction's
locked active-resource population is empty. An empty theme population still
continues to the extra check. Nonempty populations retain the original repair.

Image publication compares the complete replacement rowset on its first owned
item read. The comparison includes type/index keys, obsolete roots, paths,
identity, hash, size, normalized timestamp, dimensions and MIME. Preserved
types are excluded from replacement. The same query conditionally constructs
the original public notification projection only when raw rows differ.
An exact no-op skips DELETE and UPSERT; changes retain the previous writes and
before/after notification comparison. Owner/item locks, transaction completion,
final RunImmediate source proof and resource retirement remain in place.

No cross-scan catalog cache, new authorization policy, batching framework or
transaction-free path is introduced. Explicit refresh still performs real
probes, derived-state repair and refresh notification work.

## Verification and work counts

All nine selected remote Go invocations qualified: one focused library race
run, two TRACE1 work-count runs and six unprofiled TRACE0 timing runs. The race
run passed all 41 required top-level tests and 39 subtests, with zero skips.
Coverage includes same-hash private-field repair, old-root and extra-index
repair, invalid-type retention, source replacement, cancellation, final proof,
notification/tuple retention, and transactional auxiliary repair/rollback.

The count pair uses one complete five-phase invocation per source, with
SQL_TIMING disabled. Its elapsed values are excluded from the timing comparison.

| Phase | Raw SQL B / C | COMMIT B / C | Image DELETE B / C | Image UPSERT B / C |
| --- | ---: | ---: | ---: | ---: |
| cold | 6957 / 6523 | 382 / 382 | 136 / 136 | 136 / 136 |
| warm | 2045 / 1659 | 192 / 192 | 136 / 0 | 136 / 0 |
| force | 6482 / 5776 | 382 / 382 | 136 / 0 | 136 / 0 |
| image_changed | 2048 / 1664 | 192 / 192 | 136 / 1 | 136 / 1 |
| image_removed | 2048 / 1664 | 192 / 192 | 136 / 1 | 136 / 1 |

BEGIN counts also remain unchanged. Image DML counters directly establish the
removed commands; subtitle/auxiliary savings are consistent with the code and
focused tests but are not separate category counters in the performance driver.
Snapshot counters still count queries containing the projection expression,
including conditional queries. They do not count actual JSON evaluations.

All processes preserve the diverse corpus, image keys/rows/xmin, Store.Close,
and media ProbeFile call counts 160/0/160/0/0 with peak call cohorts 2 for
cold/force. Native FFmpeg/FFprobe work remains; these are not counts of every
native command invocation.
The 288 initial image digests plus one replacement fit within the existing
512-entry inspection cache; this does not measure eviction pressure.

## Unprofiled performance

Three fresh pairs ran in fixed BC/CB/BC order. Percentage changes are
median(C_i / B_i - 1); absolute changes are median(C_i - B_i). These are not
ratios of source medians. Earlier samples and the count runs are not pooled.

| Phase | B job ms, blocks 1/2/3 | C job ms, blocks 1/2/3 | Paired median change | Median delta ms | Slower blocks |
| --- | --- | --- | ---: | ---: | --- |
| cold | 4376.695 / 3420.634 / 3405.476 | 5409.228 / 3140.055 / 3226.206 | -5.2642% | -179.270 | 1 |
| warm | 1861.999 / 881.279 / 876.008 | 1482.873 / 690.905 / 721.667 | -20.3612% | -190.374 | None |
| force | 4743.490 / 2817.057 / 2805.462 | 4075.383 / 2537.684 / 2425.548 | -13.5419% | -379.914 | None |
| image_changed | 1854.182 / 866.056 / 901.476 | 784.704 / 754.588 / 705.751 | -21.7116% | -195.725 | None |
| image_removed | 1513.071 / 857.872 / 879.750 | 700.908 / 737.711 / 705.436 | -19.8140% | -174.314 | None |

Cold block 1 is slower by 1032.533ms / 23.5916%. Slow phases occur in both
variants around that block, but timing proximity does not establish their
cause. This negative result remains included; uniform or stable acceleration
is not established. The prior cache batch's third-pair regression is also still
unresolved and is not explained by the present improvements.

Process allocation falls in every pair, with paired medians of -1.4975%,
-13.4291%, -2.9644%, -11.8200% and -12.7199% in phase order. Allocation is not
CPU or peak heap. Job, observed terminal and worker retirement remain separate
windows: changed terminal block 2 is +0.581262ms, and several resource/retirement
metrics have reverse samples. The complete arrays and limits remain in the
external timing report, not just these selected job summaries.

## Evidence and closeout

Evidence is retained under `.artifacts/scan-sql-reduction-20261006`.
`analysis/counts-summary.json` SHA256 is
`9ca38e3b120b88c01bb52351300591bdf717ae1f546c5c239169177272d4ac3d`;
`analysis/timing-summary.json` SHA256 is
`9a7e756bb419dd4dbcff364a0dcf3c647228d6ba1962fdd3f26ddf3f6571be50`.
Independent review rebuilt 1165 aggregate values without discrepancies and
confirmed source, raw, common-driver and BC/CB/BC bindings.

After the race run, a capacity guard stopped before the first count invocation.
Unused build allowance was then revised using actual race consumption, retaining
the 128MiB exit reserve and the original cache-accounting origin. Only the
remaining eight products resumed; no product or adverse sample was replayed.
The shared cache grew 136888320 bytes in total. An observed non-cache capacity
change remains unattributed. Two export-preparation tool errors launched no Go
process. Original logs are retained; an addendum corrects the export namespace
and a stdout footer that labels eight remaining products as eight timing runs.
Actual receipts contain two count and six timing products.

All workers and owned observers exited. Only inactive owned compiler scratch
and the empty ext4 fixture directory were reclaimed. Source backings, archives,
raw/derived evidence and shared caches remain. PostgreSQL/Goby identities and
reserve 403374 are unchanged; closure availability was 363065344 bytes.
`evidence/closure-receipt.json` SHA256 is
`2ebcc0f5ca4c141a2a87d0e0f0df8d0ae43bc7776eb571b4ef9787af86c8aba9`;
`final-export-verification.json` SHA256 is
`b992ba5f0f14615ce2499edf241ef8567e387038476c63b60e3914bdd8ae9d96`.

## Remaining work

Removing explicit write transactions from unchanged image publication remains
a separate change. A fresh read-only snapshot could define the no-op acceptance
point under existing owner serialization. That must explicitly cover current
task/root/item identity and preserve source proof and cancellation behavior.
It is not equivalent to waiting for another connection's uncommitted changes.
Combining FOR SHARE clauses into one query neither removes all tuple-lock WAL
nor guarantees the same post-wait rowset freshness as multiple statements.
The present release retains the existing transaction semantics.

Original workspace WIP is outside every source input and must retain its fresh
bytes during publication, including the overlapping scan.go. Do not restore
historical hashes over legitimate later edits.
