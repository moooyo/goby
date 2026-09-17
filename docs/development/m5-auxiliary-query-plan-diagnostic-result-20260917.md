# M5 auxiliary query-plan diagnostic

The third attempt, `8d749eaa6326`, passed one parent test and both variants once,
with independent result and saved-resource closure review. Its 15 markers cover
four snapshots and four `EXPLAIN` calls; full snapshots and property digests
match between variants for each selection, and settings restoration was observed. SSH session
`73065` exited 0. Exact pins for all three attempts are in the [checkpoint](m5-auxiliary-query-plan-diagnostic-result-20260917.json).

| Selection | Snapshot default / JIT disabled | EXPLAIN execution default / disabled | Default JIT / planning |
| --- | ---: | ---: | ---: |
| `ordinary` | 2034.891 / 3.537 ms | 2008.324 / 0.418 ms | 2006.839 / 2.166 ms |
| `removal_retained` | 2029.386 / 3.319 ms | 2022.768 / 0.391 ms | 2021.429 / 1.943 ms |

JIT accounts for about 99.93% of the reported default execution time, with 662
functions per plan. The disabled arm reports no JIT. `Generation.Total` includes
`Deform`, which is not counted twice. This supports JIT compilation as the dominant
measured cost for this small fixture under fresh plans. The fixed default-then-
disabled order and one sample per selection/arm do not establish a performance
distribution, cached product-path behavior or the full original failure cause.

Both earlier attempts remain failed. `7dbac2df9dde` ran the parent/default test
but failed the `jit_provider` settings read with SQLSTATE `42501`; a separate
`OutputType` parser rejection does not mean the test never ran. It reached no
snapshot or `EXPLAIN`. `564e1bbfbc4d` completed one 2092.821 ms snapshot and
executed `EXPLAIN`, then failed numeric decoding. Subsequent source review found
incompatible handling of the nested `Generation` value. Zero completed
explain markers do not mean zero SQL execution. Neither reached the disabled arm.
Their original SSH sessions `76693` and `45514` exited 1 and remain preserved.

All three attempts have reviewed owned-resource closure. The first two retain
`loaded/failed/MainPID=0` unit records; the third records `not-found/inactive`.
The successful scope closed 64 adapter/23 worker commands, owned PostgreSQL,
the unit/cgroup, three volumes and lock, with protected state and source exact.

The original M5 full run remains failed; earlier diagnostic scopes are unchanged.
The narrow product correction is prepared in an isolated worktree, retaining the
complete auxiliary query and five-second proof budget. It still needs frozen-source
verification through actual reconciliation, full regression and builds. No product fix, complete M5 result
or Programs transition is accepted here. This documentation pass only read saved evidence.
