# AMD OCI archive and Compose delivery plan - September 29, 2026

Status: **COMPLETE within the selected Linux amd64 AMD delivery scope**.
The selected scope is a Linux amd64 AMD image archive plus Compose extension.
No registry publication, other-GPU qualification, or performance claim is included.

Development uses the existing `codex/oci-amd` checkout. The extension recipe is
`82741b27661784f2adfa86f14c4e8b5e46ed4c31`; application source remains
`b9bfa7ccee07e2ae81f70ab90d2121d908734636`, with the unchanged embedded binary from
the [accepted software OCI delivery](../development/oci-delivery-20260929.md).
The [AMD result record](../development/oci-amd-delivery-20260929.md) and its
[result manifest](../development/oci-amd-delivery-results-20260929.json) bind
focused checks, actual archive import, the HTTP journey, and final closure.
Git integration is handled separately; no completed merge or push is claimed.

## Selected environment and boundaries

Use the existing AMD `gfx1150` device in unprivileged LXC CT104, with a private
rootful Docker engine inside that container. Goby stays UID/GID `10001:10001`,
with a read-only root filesystem and media mount, dropped capabilities, and
separate writable state/cache/log paths. The selected render node is
`/dev/dri/renderD128`, supplementary GID 992. These values identify this fixture;
other installations must select their actual device and numeric group.

The observed driver stack is Mesa `25.0.7-2+deb13u1`, DRM 3.64, and kernel
`6.17.13-3-pve`. The selected execution allocation is two CPUs and 2 GiB.
External PostgreSQL uses TLS on the owned `test-env` endpoint at port 55993
and ordinary database owner roles. Do not change PVE configuration or shared
device permissions to admit this profile.

The runtime seccomp file starts from Docker 29.7.2's actual `cap_drop: ALL`
profile and adds only `kcmp` with argument index 2 equal to `KCMP_FILE` (`0`).
Keep the product's strict AMD admission checks. Privileged mode, added
capabilities, and `seccomp=unconfined` are not substitutes for this bounded rule.

## Finite execution sequence

1. **Completed focused acceptance:** rebuild FFmpeg 9.0.1 with strict Dolby Vision,
   decoder-wakeup, and copy-timestamp-progress fixes; preserve the verified v3
   private libraries/libplacebo ABI. Check executable access as UID 10001.
   Run the selected actual admission, output-axis, padding, decoding,
   processing, synchronization, cancellation, subtitle, and fallback tests.
2. **Completed actual HTTP acceptance:** production `cmd/goby` passed seven
   outputs with both Compose files and the recorded seccomp profile. Managed
   axis selections, independently decoded media, captured plans, and actual
   executable/arguments/UID/render descriptors agreed. Per-play stop checks
   closed process, output-FD, and cache ownership. Attempt04 reused the database
   bootstrapped in attempt01; it was not a fresh-database run.
3. **Completed artifact verification:** the recorded archive was loaded into
   CT104 and the immutable image ID executed. Application, media, archive,
   seccomp, and the 69-file runtime-evidence archive retain exact identities.
   The local delivery destination is `D:/Code/goby/.artifacts/oci-amd-20260929`.
4. **Completed owned-resource closure:** the application/observer exited zero,
   its container was removed, and the private Docker engine and PostgreSQL
   stopped. Database data and failure evidence were retained. Existing guests,
   PVE/CT configuration, and protected Docker containers remained unchanged.
5. **Separate Git integration:** the verified result and operator documentation
   are ready for the authorized merge/push and actual-ref readback. This plan
   does not claim that integration has already occurred.

## Correctness requirements

Decoder, encoder, and GPU processing are separate axes. A render-node descriptor
or capability listing does not prove that every axis ran on the GPU. Compare
actual output dimensions, codec/depth, frames, seeking, and audio synchronization
with the captured plan. On the observed AV1 padded tuples, reject altered geometry
and preserve the exact request through explicit software fallback.

Build 01's missing strict patch, build 02's UID-access failure, and the original
default-seccomp rejection remain failures at their original scope. Do not suppress
stderr, relax output checks, or relabel an earlier native/software result as a
new AMD-image run. Actual HTTP verification and final owned-service closure passed.

The [AMD operator guide](../../deploy/oci/README.amd.md) complements the base
Compose guide. Other adapters/drivers, Intel/NVIDIA, arm64, arbitrary resolution
or codec matrices, host power-loss recovery, and strict performance SLOs are
outside this increment. Existing notices are retained; no project-license
decision or public-distribution licensing clearance is implied.
