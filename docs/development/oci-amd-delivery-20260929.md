# AMD OCI archive and Compose delivery - September 29, 2026

Status: **COMPLETE within the selected Linux amd64 AMD delivery scope**.
Build 03, actual archive import, selected GPU checks, the production HTTP journey,
and owned-resource closure passed. Git integration is handled separately;
no completed merge or push is claimed.

This Linux amd64 archive/Compose extension follows the [AMD delivery plan](../planning/oci-amd-delivery-plan-20260929.md)
and [accepted software image](oci-delivery-20260929.md). Use the
[AMD guide](../../deploy/oci/README.amd.md) with the base Compose guide.
No registry publication is included. Exact receipts are bound by the
[machine-readable result manifest](oci-amd-delivery-results-20260929.json).

## Recorded build identities

| Input or artifact | Identity |
| --- | --- |
| AMD recipe revision | `82741b27661784f2adfa86f14c4e8b5e46ed4c31` |
| Build-03 image | `sha256:eb0f98427ad6535eb37d4cf744ab0d90d6c2e45f84c64f34a2355e0a7eed713f` |
| Unchanged application source | `b9bfa7ccee07e2ae81f70ab90d2121d908734636` |
| Unchanged Goby SHA-256 | `2e296bfd02fdf3f7b740000d0d87563e420d5a099fd20e0aa37b96502f78c004` |
| Rebuilt FFmpeg 9.0.1 SHA-256 | `3505587e95203e2561a5b0459134847b59aca3665557676a394cdfbca2d14908` |
| Rebuilt ffprobe SHA-256 | `5012fcac5d0da7346953312cce9f86cbfa588baf412e3d14acf6e028405f71ad` |
| Exported archive | `goby-linux-amd64-amd-image.tar`, 890,938,368 bytes |
| Archive SHA-256 | `4c5bb25118a3d73aa04273647b28a57512f232cfe9c418c01ec66307a7a5332a` |
| Actual archive import | Loaded into CT104 and executed using the recorded immutable image ID |

The verified software base is `sha256:ff9beeb782aea48dbb19630712b67824c6ef775c5886b7bac9ef856fb5c5dcab`.
Its application, embedded UI, PostgreSQL clients, and intro helper are inherited;
the AMD recipe-revision label does not identify a new Goby binary.

FFmpeg/ffprobe were rebuilt with three patches: strict supported Dolby Vision,
decoder queue wakeup, and copy-timestamp progress handling. Libplacebo 7.351.0
and the remaining captured private libraries are reused unchanged from native v3,
with matching headers and retained provenance. The new FFmpeg hashes identify
new executables; native-v3 acceptance is background evidence, not acceptance of
these rebuilt binaries. Runtime tools live under `/opt/goby-amd-ffmpeg`.

## Actual environment

| Property | Observed fixture |
| --- | --- |
| Host boundary | Unprivileged LXC CT104, private rootful Docker inside it |
| AMD adapter | `gfx1150` |
| Driver/kernel tuple | Mesa `25.0.7-2+deb13u1`, DRM 3.64, kernel `6.17.13-3-pve` |
| Selected render node | `/dev/dri/renderD128`, supplementary GID 992 |
| Application identity/storage | UID/GID `10001:10001`; read-only root filesystem and media |
| Resource profile | Two CPUs, 2 GiB; an execution profile, not a throughput/SLO result |
| Database | External PostgreSQL 17, TLS endpoint on `test-env` port 55993, ordinary owner roles |

No privileged container, added capability, unconfined seccomp, or PVE configuration
change was used. Only the selected render node is mapped; host drivers/firmware are unchanged.

## Narrow seccomp adjustment

Docker's default seccomp denial of `kcmp` produced AMD driver diagnostics on
stderr and prevented the existing strict admission checks from passing. Those
product checks remain unchanged. The delivery captures Docker 29.7.2's actual
profile with all capabilities dropped, then adds one allow rule:
`kcmp` with argument index 2 equal to `KCMP_FILE` (`0`).

| Profile | SHA-256 |
| --- | --- |
| Captured Docker baseline | `342a876f4cb0e876151af93370a33bdcb199f8a17ed33da740ffa82036ca180d` |
| Delivered `seccomp.amd.json` | `22b86c7effb4cf7189145eb12c241f0bb28f732b50270123c751d31f0766b7f0` |

The profile is [stored with the recipe](../../deploy/oci/seccomp.amd.json).
The Compose extension selects it through `GOBY_AMD_SECCOMP_PROFILE`; the builder
includes it with the delivery companions. This specific Docker/profile change
does not qualify an arbitrary seccomp baseline or a different engine version.

## Completed focused acceptance and retained failures

| Check | Result and boundary |
| --- | --- |
| Build 01 negative control | The strict Dolby Vision patch was missing. The original failure is retained; the recipe was corrected to include all three required patches. |
| Build 02 identity check | Root execution appeared successful, but UID 10001 could not traverse the captured 0700 private prefix. This was not accepted nonroot execution. |
| Build 03 | The installed runtime prefix was made readable/traversable with `a+rX`, and FFmpeg/ffprobe execution was checked after `USER 10001:10001`. The image build passed. |
| Exact admission tuples | Selected actual admission checks for five tuples passed. Capability enumeration alone is not their evidence. |
| Decode/encode axes | Three selected axis combinations passed 39 leaf output checks. |
| Output geometry guard | Actual AV1 padding was rejected; the guard passed. |
| Input depth and processing | Selected 8/10-bit decode-input, HDR, and deinterlace checks passed. |
| Seek and synchronization | Selected seek/audio-video synchronization checks passed. |
| Actual GPU cancellation | The dedicated live-producer cancellation check passed. |
| Subtitle processing | Selected Vulkan-to-VAAPI subtitle path checks passed. |
| Managed server selection | Selected managed-axis and explicit AV1 software-fallback checks passed. |
| Bounded Compose startup | Actual start, status, observer snapshot, and stop passed as UID 10001. Read-only root/media and rejected root writes were confirmed; two processes were observed and no probe error was reported. |

The selected Go result is **10 parent passes and 69 subtest-node passes**, with
zero failures/skips/unfinished tests. Grouping nodes are included; the 39 leaf output checks are a subset.

VAAPI AV1 requests for 320x180 and 318x190 produced 320x192 on this device/driver.
Admission rejected the changed geometry; software fallback preserved the exact
request. This is not general fallback for decoder or GPU-processing-graph failure.

Overlapping selectors are not summed. A GPU descriptor, decoder, encoder, and GPU
filter are distinct witnesses; none alone proves that all stages ran on hardware.

## Actual HTTP journey

`http-attempt04` passed seven production `cmd/goby` HTTP outputs:

| Outputs | Observed result |
| --- | --- |
| H.264: decode_only, encode_only, combined | Three outputs, each 1,080 frames and 45 seconds |
| HEVC: decode_only, encode_only, combined | Three outputs, each 1,080 frames and 45 seconds |
| Exact AV1 software fallback | 320x180, 96 frames, four seconds |

Each output passed independent full software decoding and frame mean-luma error
checks at or below 15. Persisted plans agreed with observed FFmpeg executable
SHA-256, arguments, UID 10001, and actual render descriptors. Explicit stop checks
confirmed no remaining scoped process, output descriptor, or job cache entry.
After switching managed selection back to software, all seven historical plans
remained unchanged. This stop evidence does not relabel completed HTTP producers
as the separate live-GPU cancellation case.

Fresh bootstrap succeeded in attempt01. A private `resume-http.py` continued
that retained database; attempt04 used an independent output directory and did
not recreate or reinitialize the database. Earlier attempts remain failures:
attempt01 hit observer `PermissionError`; attempt02 retained short-lived-PID/FD
permission diagnostics and a resume-wrapper omission of the library ID; attempt03
hit existing output filenames after retry. Original data and outputs were kept.

The observer retries the complete sample only for `PermissionError`, every 10 ms
for at most 0.5 seconds. Persistent collection errors still fail. One actual
transient short-lived-PID observation recovered under this bounded retry; missing
or partial evidence was not declared complete. No Go product code changed.

## Final closure and delivery

| Final gate | Current state |
| --- | --- |
| Actual production `cmd/goby` HTTP media journey | PASS: attempt04, seven outputs with decoded results and job/process/device evidence |
| HTTP producer stop and output cleanup | PASS, with final application and observer exit zero |
| Final archive import | PASS in CT104 using the recorded image ID; archive size/hash unchanged |
| Complete owned Docker/PostgreSQL/resource closure | PASS; retained data and protected environments are described below |
| Git integration | Handled separately; no completed merge or push is claimed |

The application stopped with exit zero, PID zero, and closed HTTP; its observer
also exited zero. The owned container was removed. The private Docker engine was
inactive/dead with MainPID zero, and no owned binary process remained. CT104 stayed
running; CT100 and VM101 also remained running, with no PVE/CT configuration change.
The PostgreSQL 55993 fixture had zero connections before normal fast shutdown;
`postmaster.pid` was absent and database data was retained. The three protected
Docker containers retained exactly their original IDs, PIDs, and start times.

`amd-runtime-evidence.tar.gz` contains 69 files and 10,450,598 bytes, with SHA-256
`c6fc1ca467dfd7c3870aea307b559924170a9c13b48c4fe26ffb1337061454be`.
The local delivery destination is `D:/Code/goby/.artifacts/oci-amd-20260929`.
Private configurations remain outside tracked source. Original build, permission,
seccomp, and HTTP-attempt failures retain their original disposition.

## Scope limits

Acceptance is limited to this Linux amd64 AMD profile and recorded device/driver/
tool tuple. Other GPUs, arm64, arbitrary codec/resolution combinations, new client
coverage, performance SLOs, and host/power-loss recovery are not claimed.
Native v3 and software-image results retain their original scope. Notices and
license texts remain included; no project-license decision, public-distribution
licensing clearance, or registry publication is implied.
