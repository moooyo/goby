# Unchanged-image query plan reuse, 2026-10-08

Unchanged-image comparisons now reuse a prepared statement on the existing
reserved owner connection. The constructor's actual statement-cache capacity
continues to select the policy; disabled caching preserves the connection's
default execution mode. The shared field is named `scanOwnedReadMode` because
both media-fact and image-comparison reads use this owner policy.

The SQL text and all seventeen data parameters are unchanged. This caches the
execution plan, not image results. The writer, authorization checks, owner lock,
cancellation handling, final source checks and transaction semantics are unchanged.

## Remote validation

One candidate build and one focused functional process completed on `test-env`:
15 top-level tests and 32 subtests passed, with no skips. New coverage verifies
actual typed named-plan reuse, disabled-cache and connection-hook fallback,
one-entry cache eviction, custom/generic plans, array parameter changes and
freshly committed image rows. Existing no-op, concurrent-writer, cancellation,
source-change and owner-loss checks remain in the selected regression set.

Four first performance processes ran in fixed baseline/candidate/candidate/
baseline order, each retaining the original five phases and restored physical
ext4 corpus. All twenty phases qualified. The complete SQL multiset, image
results, traversal order, lookup/media argument sequence and fixture manifest
matched. The target query runs zero times in cold and 136 times in each other
phase. Only its execution mode changes from default CacheDescribe to explicit
CacheStatement. First use and nested Prepare are included in query timing.

| Contrast | Phase | Baseline image-query ms | Candidate image-query ms | Query delta ms | Job delta ms |
| --- | --- | ---: | ---: | ---: | ---: |
| First pair | warm | 61.063 | 23.716 | -37.347 | -60.386 |
| Second pair | warm | 57.587 | 25.692 | -31.895 | -30.467 |
| First pair | force | 66.835 | 26.970 | -39.865 | -109.230 |
| Second pair | force | 64.990 | 26.724 | -38.265 | -15.273 |
| First pair | image_changed | 59.431 | 23.970 | -35.462 | -36.152 |
| Second pair | image_changed | 56.605 | 23.149 | -33.456 | -43.580 |
| First pair | image_removed | 55.027 | 27.047 | -27.980 | -21.511 |
| Second pair | image_removed | 54.739 | 22.413 | -32.326 | -38.134 |

Warm target-query time decreases 61.2% and 55.4%; warm jobs decrease 16.6679%
and 8.6111%. These are two fixed paired engineering observations, not tail
estimates or a universal speedup. Callback sums include protocol and server work;
they are not exclusive planner CPU time. COMMIT occupancy and other work also
vary, so whole-job changes are not attributed entirely to this query.

Cold has no target query. Its two job contrasts are -117.875 ms and +212.322 ms
(the latter +6.3334%). Both are retained without a cold-speedup claim. The prior
NVMe investigation remains stopped. Prepared-plan retained memory was not
measured by this task; the existing cache capacity and eviction behavior remain.

## Source and closeout

The retained baseline binary is
`64703882060a9219be6409e7c81969b3f6f1a2f8c97d3c8b8e36a481731d4448`.
The candidate is
`922b09a71ae40d023915ea65f4a06444df0d0116f7885b953048e9abdabaa378`.
The baseline production/dependency inputs match main `0c81f0e7`; the two retained
performance-driver inputs are shared unchanged. Candidate source freeze is
`f3539e6cef3f9d3005c025546528f627381c927d91303de1caedebc51a83797d`.
Original-workspace WIP is outside the measured inputs.

All 57 exported files were independently checked for bytes and SHA-256.
Workers exited and the task's compiler scratch and verified RAM copies were
removed. Shared Go caches, previous source overlays and the original ext4 corpus
remain. No local product verification or replacement performance sample ran.

- [Query and COMMIT contrasts](../../.artifacts/scan-image-plan-20261008/analysis/image-plan-result.md)
- [Full phase/resource report](../../.artifacts/scan-image-plan-20261008/analysis/version-report.md)
- [Frozen candidate inputs](../../.artifacts/scan-image-plan-20261008/runtime/candidate-inputs.json)
- [Actual closeout](../../.artifacts/scan-image-plan-20261008/runtime/closeout.json)
