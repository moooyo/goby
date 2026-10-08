# Direct final-main scan comparison, 2026-10-08

The final committed source `82dbcd0540017852fac39747b5b1caa80e89fe00` has now
been measured directly against the genuine old scan version
`08ed21267ab647a4a49bf818ee2d2a2d19d26e39` (runtime
`6386c8fd88585557ee65acbc0a12be23d90a41d4`). Four fixed old/final/final/old
processes and all twenty original phases qualified on their first attempts.
Nine of ten job comparisons improve; the first cold comparison is slower.

This closes the direct cumulative scan comparison left open by the image-plan
work. It compares complete revisions, rather than adding or multiplying earlier
stage percentages or isolating the last Stop/GET change. The older `6683` HTTP
comparison is a separate historical experiment and is not used here.

## Source and workload

No new binary was built for this comparison. The retained old library binary is
`7d5c231c204f7b201deeec7794dace0c90125c28406996c16c5ed7874b0e0e28`;
the final library binary is
`e9d8c89e73fae59d855f7749c270a97be8843a15eb3d3a1e48841ec80cf5fdf9`.
Original build receipts, effective input manifests, independent binary hashes
and the code-delivery receipt bind the compiled candidate to final main
`82dbcd05`. Documentation added afterward does not change these compiled inputs.

Both binaries contain the identical common scan drivers:

- `scan_real_images_performance_integration_test.go`:
  `d2654ca3e06e81a1d2ff04c3c7e237218794186fa86d09bc4f9ec66d00e7eb19`.
- `scan_version_performance_integration_test.go`:
  `50b898636141377576d9dca8831affff04b07aba5b3c1f7e7dc51fcfd5ffcd31`.

All four processes use the original physical ext4 corpus, fresh Store/schema,
the five original phases and the same native environment. The corpus contains
128 real videos, 32 real audio files and 288 distinct PNG contents. Both native
GOTMPDIR and TMPDIR select ext4 fixtures. Global CacheDescribe, cache capacities,
GOMAXPROCS=4 and jit=off remain. Each source executes its actual implementation;
no mode mutation emulates an older binary. The restored backdrop inode caveat
from the existing corpus workflow remains.

## Direct job results

Each cell is final minus old within its prescribed pair. Pair 1 is old1 followed
by final1; pair 2 uses final2 followed by old2. The two observations per phase are
correlated ordered contrasts, not independent N=20 or stable-tail estimates.

| Phase | Pair 1 job delta | Pair 1 change | Pair 2 job delta | Pair 2 change |
| --- | ---: | ---: | ---: | ---: |
| cold | +62.800 ms | +1.8691% | -39.158 ms | -1.1374% |
| warm | -61.910 ms | -14.8641% | -148.506 ms | -33.3634% |
| force | -81.190 ms | -3.6103% | -120.028 ms | -5.1925% |
| image_changed | -124.350 ms | -27.8819% | -109.958 ms | -25.4337% |
| image_removed | -143.251 ms | -32.1899% | -126.028 ms | -28.9764% |

Cold means fresh catalog and connections, not cold operating-system or disk
caches. The first cold increase is retained without claiming universal speedup.
Observed terminal time is separate: pair 2 warm and image_changed increase by
0.319015 and 1.813991 ms. The roughly 250 ms force terminal reductions include
the existing polling interval and are not substituted for persisted job savings.
Pair 1 GC counts increase by three in cold, three in force and one in
image_removed. Full allocation, malloc, GC, retirement and pool observations,
including other adverse values, remain in the detailed report.

## Work and statement checks

All ten comparisons match the observed traversal, fixture guards, committed
image outcomes and argument sequences. Probe counts stay 160/0/160/0/0 and image
comparison reads stay 0/136/136/136/136 across cold/warm/force/changed/removed.

| Phase | Old to final observed SQL statements | Old to final explicit BEGIN / COMMIT, each |
| --- | ---: | ---: |
| cold | 7417 to 7321 | 382 to 350 |
| warm | 1043 to 947 | 56 to 24 |
| force | 5727 to 5631 | 246 to 214 |
| image_changed | 1056 to 960 | 57 to 25 |
| image_removed | 1056 to 960 | 57 to 25 |

Every phase has exactly five differing SQL-template counts. The old 32 legacy
source reads, 32 source-cache reads and 32 explicit read-only BEGIN/COMMIT pairs
are replaced by 32 single source reads: 128 observed source commands become 32,
for a net reduction of 96 statements and 32 explicit BEGIN/COMMIT pairs.
ROLLBACK counts remain zero. This is a statement-count observation, not a count
of wire round trips or all implicit PostgreSQL transactions.

Query callbacks include protocol, server waits and result consumption. Nested
Prepare is not added again. Process allocation and GC windows include observer
and background work. No CPU, retained-plan-memory, host-storage cause, native/GPU,
whole-service capacity or original-player A/V claim follows from this workload.

## Verification, retention and closeout

Independent offline parsing rebuilt all twenty phase observations from raw,
verified their stored observations, and rehashed both actual retained binaries
and their original build/source/delivery chains. Old data was qualified with the
old-version contract and final data with the accepted image-plan contract;
expected source-consolidation differences were not erased by imposing a false
identical-SQL requirement. All 43 final-scan exports matched their hashes.

Across the selected follow-up, three actual builds and twenty-two native
invocations completed: four diagnostics, two functional products, twelve formal
HTTP products and these four scan products. All twenty-two native invocations
qualified with zero retries. The separately retained shared-cache maintenance
wrapper metadata gap is described in the
[Stop/GET report](stop-get-followup-20261008.md); it is not a Go product result.

Actual final closeout found no workers and removed the task's inactive RAM,
compiler scratch and empty fixture root. The original shared corpus remains on
ext4 at device 2049/inode 4227089, allocated size 3006464 bytes, with its retained
baseline checksum. Source, independent binaries and all raw evidence remain.
Shared build cache is 403750912 bytes and modules are 920547328 bytes; neither
was cleaned at closeout. Final persistent availability was 6491443200 bytes,
which is not attributed wholly to a single cleanup action.

Evidence root: `.artifacts/stop-get-followup-20261008`.

- `final-scan` contains source/selection bindings, all raw phases and receipts.
- Final-scan export manifest SHA256:
  `31afeb617dec706eee97002f75e8e63f1c475fb32fbeb998aa6bf072d9835ee9`.
- `analysis/final-scan-abba-final.md` SHA256:
  `64477aa4915ad1fe541d841bfdcf857d5d5ad06c075ad1dff1d0b132c434a78c`.
- Its complete JSON SHA256:
  `52d62439e1286505656ef883be53668c719e079f3cd7a9ed908d37d663839ee5`.
- `followup-closeout.json` SHA256:
  `3897f568e6b40b508103c579d27b03b7185cccb1da2b9b9f01547d970ee68c68`.
