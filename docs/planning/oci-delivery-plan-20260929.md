# OCI container delivery - September 29, 2026

Status: **COMPLETE within the selected delivery scope**. The importable image
archive and Compose profile passed actual loading, container execution, native
recovery, upgrade/rollback, and owned-resource closure. The target is Linux amd64
software processing with external PostgreSQL 17. Registry publication remains
outside this request. Git integration is handled separately; no merge or push is
asserted by this status.

The baseline is accepted application revision
`48d461a063622576810402bdfe4ffead2a1c59ba`. The final delivery includes verified
restore repair/source revision `b9bfa7ccee07e2ae81f70ab90d2121d908734636`.
Development uses the isolated
`codex/oci-delivery` worktree. Existing unrelated changes in the main checkout
remain preserved. The two existing Dockerfile transport/permission improvements
are incorporated deliberately; unrelated release-notice drafts are not inputs.

Final identities, results, retained failures, and limits are recorded in the
[delivery result](../development/oci-delivery-20260929.md) and its
[machine-readable manifest](../development/oci-delivery-results-20260929.json).
The [operator guide](../../deploy/oci/README.md) describes importing and using
the archive, configuring external databases, and preserving state during updates.

## Completed finite delivery sequence

1. Completed the software recipe, both FFmpeg correctness patches/native
   harnesses, fingerprint helper/protocol tests, notices, and analysis config.
2. Built the embedded application and final image on `test-env`; exported the
   338,467,840-byte hash-bound archive and successfully loaded it with Docker.
3. Passed final Compose bootstrap/media execution, direct/Range bytes, decoded
   remux/transcode and BIF samples, and actual intro feature extraction. The
   synthetic intro result remains an execution result, not an accuracy claim.
4. Passed normal stop/recreation and persistence, native encrypted restore and
   rollback, including intended credential revocation. Reproduced and repaired
   registry-less restore normalization that disabled retained task policy.
   Complete recovery/tasks regression and the embedded command suite passed.
5. Passed the actual schema 29 to 50 image upgrade, one probe-version 6 to 8
   forced scan, and restoration of the old snapshot into a distinct database
   for schema-29 rollback. Original IDs, user state, keys, and media bytes held.
6. Closed and removed seven owned containers, stopped the private PostgreSQL,
   removed the owned network, and released eight ports. Preserved 17 custom
   databases, failures, archives, and receipts; three existing running containers
   were unchanged. Operator documentation and evidence are ready for Git integration.

Tests and runtime probes ran on `test-env` with Docker 29.7.2 and Compose 5.5;
local work was compilation. The selected container runs as UID/GID 10001 with a
read-only root filesystem and media mount. No shared engine pruning, service
replacement, historical one-shot controller chain, or strict Phase 3 performance
profile was needed for this finite container acceptance.

The image is a software profile. GPU device admission, native arm64, a new
client matrix, host/physical-power-loss recovery, registry publication, and a
project-license decision remain separate. Existing source/license notices are
retained; this work does not silently select a project license.
