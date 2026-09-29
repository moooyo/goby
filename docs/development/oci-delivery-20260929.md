# OCI archive and Compose delivery - September 29, 2026

Status: **COMPLETE within the selected Linux amd64 software profile**.
The final image and archive passed actual loading, Compose runtime acceptance,
native encrypted recovery, schema upgrade/rollback, and owned-resource closure.
Git integration is handled separately; this record does not claim a merge or push.

The previously verified application baseline was
`48d461a063622576810402bdfe4ffead2a1c59ba`. The final delivery includes restore
repair/source commit `b9bfa7ccee07e2ae81f70ab90d2121d908734636`.
See the [delivery plan](../planning/oci-delivery-plan-20260929.md),
[operator guide](../../deploy/oci/README.md), and
[machine-readable result manifest](oci-delivery-results-20260929.json).
Unrelated main-checkout changes remain outside this delivery.

## Delivered identities

| Artifact | Accepted identity |
| --- | --- |
| Final image, build 04 | `sha256:ff9beeb782aea48dbb19630712b67824c6ef775c5886b7bac9ef856fb5c5dcab` |
| Embedded application SHA-256 | `2e296bfd02fdf3f7b740000d0d87563e420d5a099fd20e0aa37b96502f78c004` |
| Archive | `goby-linux-amd64-image.tar`, 338,467,840 bytes |
| Archive SHA-256 | `1c3b2f5194c3799b83bac948352dcbfc5ea5411eb8f9dac69894b95fff9fe720` |
| Actual `docker image load` | PASS for the final archive |

The archive's local delivery destination is
`D:/Code/goby/.artifacts/oci-20260929/goby-linux-amd64-image.tar`.
Its companion receipt, image inspection/identity, `SHA256SUMS`, `compose.yaml`,
and `goby.env.example` identify and configure the same delivery. Use the immutable
image ID after loading; Compose disables pulls. No registry publication occurred.

## Selected runtime profile

| Property | Accepted scope |
| --- | --- |
| Host tools | Docker 29.7.2 and Compose 5.5 on `test-env` |
| Architecture and engine | Linux amd64; rootful Docker without user namespace remapping |
| Application | Embedded administrator UI and production `cmd/goby` |
| Media tools | Software FFmpeg/ffprobe 9.0.1 and the native intro fingerprint helper |
| Database | External PostgreSQL 17; actual fixture 17.11; independently provisioned ordinary owner roles |
| Container | UID/GID `10001:10001`, no capabilities, no new privileges, read-only root filesystem |
| Storage | Read-only media; separate persistent state, cache, and logs; bounded temporary storage |
| Startup | Loopback host port and `restart: "no"`; operator-controlled startup |

The image contains PostgreSQL 17 dump/restore clients and an analysis configuration
bound to the installed helper digest. Analysis scheduling remains normal library/
task policy. PostgreSQL and the separate native-restore target are not created by
Compose. The operator guide explains their configuration and persistence needs.

## Build and regression evidence

| Check | Result and boundary |
| --- | --- |
| Build 01 | Retained registry TLS certificate failure; no image acceptance from this attempt. |
| Base fetch correction | `mirror.gcr.io/library/debian` fetched the same pinned Debian manifest. |
| Build 02 toolchain | FFmpeg x264/x265/libaom/zscale support, decoder-wakeup and copy-timestamp-progress native harnesses, and seven native-helper protocol tests passed. |
| Build 03 | Added collected application notices; image ID beginning `6136f9` remains a pre-repair candidate. |
| Build 04 | Final repaired application/image built and exported successfully. |
| Artifact checker and OCI builder | 12 and four remote tests passed, respectively. |
| Recovery and tasks packages | 131 parent and 170 subtest passes across the two complete packages; zero failures or skips. |
| Embedded-admin command suite | 24 parent and 37 subtest passes. |
| Application builds | Ordinary application and embedded release builds passed. |
| Additional software-codec/filter execution | Four actual HEVC/AV1/zscale-tonemap/subtitle fixture encodes and independent complete three-frame decodes passed. |

Build 04 retains exactly the build-02 hashes for all five non-Go executables:
FFmpeg, ffprobe, pg_dump, pg_restore, and the fingerprint helper. The build-02
native harness/protocol results are reused on that explicit byte-equivalence
basis; they are not described as new build-04 executions. Parent/subtest counts
and repeated runtime stages are reported separately rather than added as unique
coverage. Local work was compilation; tests and runtime checks ran on `test-env`.

The Debian manifest is
`sha256:abc9cb88a5587630d7f915f47b23b0668fe250fbfc6457aa4d52b534c1bbf73f`;
APT uses the `20260915T000000Z` snapshots. Tool sources, local patches, build
records, package versions, helper notices, and application notices are retained
in the image at the locations documented by the operator guide.

## Product repair and original failures

Native encrypted restore exposed a real product defect: normalization called
`Reconcile` on a temporary task store without the application executor registry.
This persisted disabled analysis definitions and changed their revisions, so
analysis remained disabled after startup. The new regression reproduced the
original retained-definition/state mismatch before the fix.

The repair removes that registry-less reconciliation. `RecoverRuns` still
interrupts retained executable work; startup reconciles against the real registry.
Regression compares retained definitions, schedules and revisions, including an
explicitly disabled definition that must remain disabled. The complete affected
packages and final actual container recovery passed after the repair.

Other retained failures were fixture/setup assumptions: expecting a master-key
file before the first application key, expecting imported credentials to survive
native restore, expecting schema 49 instead of 50, pg_dump connection setup,
and omitting the required old-probe refresh. A filter fixture also supplied an
invalid colorspace vector; the corrected vector passed without changing the
product filters. These failures remain recorded and are not relabeled as passes.

## Final Compose runtime journey

`runtime-attempt04` completed bootstrap/media verification, normal container
recreation, a 387,334-byte age-encrypted backup, native restore to generation 1,
new 23-frame BIF generation, restart, rollback to generation 2, final persistence,
and clean stop. Direct and Range bytes matched; remux/transcode outputs were
decoded. HTTP BIF verification decoded three sampled frames from 23.

Each of three real episode analyses produced 342 audio and 90 visual samples
and returned `review`. This proves extraction and truthful result publication,
not a new intro-accuracy claim. Container recreation retained active application
keys and their master key. Native restore intentionally revoked imported keys:
reveal returned 409 for them, while newly issued active keys remained revealable.

## Actual upgrade and rollback

`upgrade-attempt05` passed the complete old-image/schema-29 to final-image/schema-50
transition and rollback. The old image ID begins `f387` (historical baseline
`1162808`). Exactly one explicit `ForceProbe` scan refreshed probe version 6 to 8.
Both original Movie IDs and their UserData, the application key, and the sample
movie's 4,019,585 original bytes remained correct.

Rollback restored the pre-upgrade database snapshot into a distinct owned database
and selected the original image, returning to schema 29 with matching state/master
key. It did not run the old binary against the migrated database or erase failed
state. This external snapshot rollback is distinct from application-managed
native restore, whose imported-credential revocation is intentional.

## Closure, evidence, and limits

Seven owned containers stopped with exit code 0 and were removed. PostgreSQL
17.11 had zero other clients before normal shutdown. Its 17 custom databases and
failure states remain retained. The owned network was removed and eight reserved
ports were released. The three pre-existing running container IDs were unchanged.
No shared engine pruning, shared PostgreSQL replacement, or registry push occurred.

Raw success/failure evidence remains under
`/opt/goby-oci-delivery-20260929-01`, including build-04 receipts and the final
runtime/upgrade attempts. The result manifest binds final artifacts, verification
receipts, and closure evidence; earlier candidates retain their own identities.

GPU admission, Vulkan/libplacebo processing, arm64, other runtimes, OCR/source-media
rewriting, new client matrices, host/power-loss recovery, and historical strict
capacity/SLO claims are outside this profile. Existing dependency notices and
original license texts are included. Goby's project license remains undecided;
this internal delivery neither selects a license nor claims complete public-
distribution licensing.
