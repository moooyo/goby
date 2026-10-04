# Internal operation cleanup - October 4, 2026

Nine mechanical simplifications retain the existing authorization and lifecycle
contracts. The accepted targeted remote race matrix passed: 132 top-level and
166 subtest PASS events, all 35 required names, zero skips. Source commit
`975d256ef7d8dbae365803caffbc4faff5ae029c` has exactly fifteen changed paths
from `f5b09298b21cd47f7c6e79344f9c501898c948a8`, and all committed file bytes
match the v2 freeze. Performance was not remeasured. V1's failed fixture run
remains retained; v2 adds only its test repair to the original fourteen paths.

## Nine mechanical simplifications

| Change | Preserved contract |
| --- | --- |
| Reuse the same primary-read row/grant check for its route copy. | The exact operation/root is checked before using the immutable grant. |
| Check task state at immutable batch entry/exit instead of locking once per root in root-I/O preparation. | Every root/revision still matches the grant; cancellation/owner rejection releases admission resources. |
| Remove cached-sidecar validImageScanPath after the stronger validMediaSourceRelativePath. | Safe relative-path validation remains. |
| Remove the later same-value locked missing-receipt write. | Unknown retirement stays quarantined; an unreleased owner cannot acquire a second read. |
| Resolve a hint's route/domain once; clone route slices at primaryRootRouteContext escape, avoiding scalar-lane copies. | Escaping consumers cannot mutate the grant; final PrimaryRootIO retains its independent copy. |
| Cache sorted immutable root IDs at startup before borrowing any descriptor. | Close and final mapping SQL reuse the complete list, including partial-startup cleanup. |
| Drop unused grant.scopeID and its SQL projection. | Work.Fence still validates the real scope and execution identity. |
| Compare Analysis selection through pointer-nil, Force and slices.Equal. | Omitempty nil/empty and ordered slice identity retain JSON wire semantics; four Marshal calls per Fence are removed. |
| Decode/validate current Analysis admission once and return typed profile/execution values for the worker. | Public historical v1-v5 validation stays unchanged; an added v5 guard rejects old admission as current worker authority. |

The two lease checks are deliberately unchanged. A real mutex wait separates
them; grant context, closed and Store state can change during that wait. Timely
rejection and cancellation/error priority are part of the lifecycle contract,
and the small possible saving does not justify altering them. Live physical
source, manual/cohort, owner/token, mapping, deletion and retirement guards also
remain. External authorization and internal startup authorization policy are
unchanged.

## Source and targeted verification

The accepted v2 verification source has fifteen source/test paths, archive SHA-256
`6e8a0b170a3c13578873775a3ce1e8c2b11f3a2492ecd01f051c1e282be61249`,
freeze SHA-256
`05ff0ab058e6fe5d5cad5b77fb7bfb30abb90b9eb1d16be084c5cd450f5c6e82`.
The original fourteen file hashes are unchanged. The fifteenth path is only
scan_probe_pipeline_integration_test.go (+11 fixture lines), SHA-256
`9d75b85c26e8f91c2d57f088765a234830f54b99b0e8d180373be0a3b442999c`.
It repairs a native directory-order precondition before StartScan: Remove/Mkdir
did not guarantee the assumed order. This adds no production cleanup or
performance optimization; the nine mechanical changes above are unchanged.

| Retained v1 case | Top-level/subtest PASS events | Required passed | Skips | Outcome |
| --- | --- | --- | --- | --- |
| tasks-identity | 25 / 32 | 5 | 0 | PASS, approved reuse for v2 because source/dependencies are unchanged |
| scan-grant | 36 / 66 | 8 | 0 | FAIL: one fixture prerequisite before StartScan |

V1 source/process/fixture cleanup/protected facts were normal, but its scan
package is not qualified. Failed raw and receipts remain retained. The other
two groups did not start. The final accepted matrix combines that approved v1
tasks reuse with the three v2 runs:

| Accepted case | Top-level/subtest PASS events | Required passed | Skips | Outcome |
| --- | --- | --- | --- | --- |
| tasks-identity v1 reuse | 25 / 32 | 5 | 0 | PASS; source/dependencies unchanged |
| scan-grant v2 | 37 / 66 | 8 | 0 | PASS; the original FlushesBeforeFolderHierarchy fixture now passes |
| analysis-admission v2 | 25 / 45 | 13 | 0 | PASS |
| source-lifetime-routing v2 | 45 / 23 | 9 | 0 | PASS |
| Total | 132 / 166 | 35 | 0 | All four selected cases qualified |

The three v2 processes elapsed 125.5795, 16.8632 and 12.1953 seconds respectively,
including compilation; these are verification durations, not performance
results. Every case used race/count1/p1/15m, passed its package and required
names, and exited without failures or skips. Source/protected identities were
unchanged, fixture-owned schemas/pools/processes were cleaned up, and TMP
remaining was empty. The failed v1 scan run is not included in this PASS total.

Immutable exports remain outside the repository at
`D:/Code/goby/.artifacts/internal-operation-cleanup-20261004`.
Tasks reuse references `retained-v1/tasks-identity.jsonl` and
`retained-v1/tasks-identity-run.json`; each other row references
`evidence-v2/<case>.jsonl` and `evidence-v2/<case>-run.json`:

| Case | Raw SHA-256 | Process receipt SHA-256 |
| --- | --- | --- |
| tasks-identity | d4fc6ed651b67c3e22cf455b13e1dcd6cf4bb008c15f1a40521af9e0f26a76af | 3059076907b580268b3268ac3476c760944d6018cea113029fa54b2ba90d0bb1 |
| scan-grant-v2 | 72b442669b6b3ea703d892f0ee6caad0f4e68bc7f091b68043a819636513c14a | 829d23762d766ed52c23a1680676fe0d2752e76988c21e87eeab6c66d0197a03 |
| analysis-admission-v2 | 6a1aa24086c1e4921f230c0d29302655d299610a49308398e04e0ade7a5ee9a7 | 25c11a01ec5f52a245de7df53c76b85a8d48dd56a1df19ebb51e4655e58cc286 |
| source-lifetime-routing-v2 | 7ba706814806b957f34f19319441c2a334229fd89bfdd2c8ea66e763fd3a2a4e | 62d44009836d9377ea5612826a2acbd3e4cc167b20afb57f7b894c35d894644c |

Independent root acceptance is recorded in `root-verification-acceptance.json`,
SHA-256 `c5c026735eaeee821bdfc7f7aec8fb97f20421e22aa897326203a67a3696e210`.
The unchanged staged/final source manifest SHA-256 is
`310d866e29023921af7d7eac697a6fd5b340c78c0492bec77372c7257bea9ee5`.

Tests use standard goby_test random-schema/search_path fixtures, bounded
fixture/pool/process cleanup. After the capacity gate, user-approved cleanup of
only the shared Go build cache reduced it from 2,873,466,880 to 12,288 bytes and
restored about 3.2 GB available space. V2 uses a task-owned memory-backed GOCACHE;
the module cache and old environments were not changed. No private cluster,
image, old-root removal, local product test/build/probe, 10k suite or new
performance measurement was selected. The final publication receipt separately
records exact main/origin readback and preservation of the original 199 dirty
byte states and 56 prior distinct WIP identities.

## Performance and historical scope

Performance is not remeasured. The prior operation-authorization report delivered
at `f5b09298b21cd47f7c6e79344f9c501898c948a8` binds its measured archive to
source commit `46dec288998bd9dab5898ba974979c4758989943`. Those results remain
valid only for that previously measured source and its declared profiles; they
are not measurements of the new cleanup source or later HEAD. This cleanup
reruns neither the real 10,000-file suite, full library, builds nor H/B/C/HTTP
performance matrices. No latency, memory, throughput or stable-tail improvement
is claimed. Full historical handoff and prior comparison limits remain retained.
