# Linux amd64 AMD OCI extension

The [September 30 online-provider increment](../../docs/development/online-providers-20260930.md)
provides a newer application layer on this accepted AMD image. Its receipt binds
the new image and application identities; the FFmpeg, drivers and earlier GPU
evidence below retain their original boundaries. See the
[provider guide](README.providers.md) for configuration and subtitle write access.

Status: **COMPLETE within the selected Linux amd64 AMD archive/Compose scope**.
The recipe extends the separately verified software image with AMD VAAPI
drivers, Mesa Vulkan drivers and a rebuilt FFmpeg that
retains all three required media fixes. The previously accepted native
`amd-media-v3` prefix supplies unchanged private libraries and matching
libplacebo headers. Actual container encoding, decoding, processing and cleanup
checks have passed on the recorded AMD worker. Seven actual server HTTP playback
cases and final owned-resource closure also passed. Exact results, retained
failures and accepted boundaries are in
[the delivery record](../../docs/development/oci-amd-delivery-20260929.md) and
[its result manifest](../../docs/development/oci-amd-delivery-results-20260929.json).

Effective **2026-09-30**, the [Docker delivery policy](../../docs/planning/docker-delivery-policy.md)
supports Docker Engine images as the sole Goby deployment form. AMD and software
are two profiles of that form, delivered through image archives and Compose.
A registry is an optional distribution channel. The native toolchain reused by
this image is a build input; it does not create a supported standalone binary or
systemd installation package.

Use this guide with [the base deployment guide](README.md). The same external
PostgreSQL 17 service, private state/cache/log directories, read-only media mount,
embedded administrator UI and archive-based delivery apply. The selected runtime
uses a rootful Docker engine without user namespace remapping, Linux amd64, and
container UID/GID `10001:10001`. No registry publication is included.
PostgreSQL and a reverse proxy may remain external services; they are not
required to run in Docker.

The selected image is
`sha256:eb0f98427ad6535eb37d4cf744ab0d90d6c2e45f84c64f34a2355e0a7eed713f`.
Its FFmpeg SHA-256 is
`3505587e95203e2561a5b0459134847b59aca3665557676a394cdfbca2d14908`;
the matching ffprobe SHA-256 is
`5012fcac5d0da7346953312cce9f86cbfa588baf412e3d14acf6e028405f71ad`;
the unchanged Goby binary SHA-256 is
`2e296bfd02fdf3f7b740000d0d87563e420d5a099fd20e0aa37b96502f78c004`.
The delivery consists of `goby-linux-amd64-amd-image.tar`, its build receipt and
hashes, both Compose files, `seccomp.amd.json`, the application environment
example and operator guides. The selected archive is **890,938,368 bytes** with
SHA-256 `4c5bb25118a3d73aa04273647b28a57512f232cfe9c418c01ec66307a7a5332a`.
The local delivery directory is `D:/Code/goby/.artifacts/oci-amd-20260929`.
Both `README.md` and `README.amd.md` are included in the companion files.

## Image inputs and build boundary

The exact base image ID is
`sha256:ff9beeb782aea48dbb19630712b67824c6ef775c5886b7bac9ef856fb5c5dcab`.
Its embedded application is source
`b9bfa7ccee07e2ae81f70ab90d2121d908734636`; the extension does not replace that
binary. The inherited `org.opencontainers.image.revision` identifies the
application. `io.goby.oci.amd-recipe-revision` identifies this extension's recipe.

Prepare a private Linux build context containing `Dockerfile.amd`,
`build-amd-ffmpeg.sh`, `source-pins.amd.json`, `amd-toolchain/`,
`libplacebo-headers/` and `toolchain-patches/`. `amd-toolchain/` must be the complete
unchanged verified `ffmpeg-9.0.1-goby-cb8b6d298456` prefix, including its private
libraries, metadata and notices. Capture the matching installed libplacebo 7.351.0
headers, including generated `config.h`, from the recorded native build; their
bytes and source provenance belong to the build receipt. `toolchain-patches/`
contains the strict Dolby Vision, decoder-wakeup and copy-timestamp-progress
patches and both existing native regression harnesses. The builder must resolve
and inspect the exact base image before assigning its private local alias to
`GOBY_AMD_BASE_IMAGE`.
It must retain the build log, source/input digests, final image inspection and
archive digest. Do not substitute a mutable remote image tag for the base ID.

The recipe checks the application and native-input FFmpeg/ffprobe hashes plus
the native toolchain's complete installed-file manifest before installing build
dependencies. A private stage rebuilds only FFmpeg/ffprobe from the verified
9.0.1 source, applies all three patches and runs the decoder-wakeup and
copy-timestamp-progress native regressions. It uses the captured libplacebo ABI
and leaves the private library files unchanged. Compiler and development packages
stay outside the final runtime image.

The final stage installs `mesa-va-drivers`, `mesa-vulkan-drivers`, `libvulkan1`
and `vainfo` from the base image's fixed `20260915T000000Z` Debian snapshots. It
records actual runtime packages and compiled VAAPI/Vulkan/libplacebo features
without requiring a GPU during the build. The new binary digests and full prefix
manifest describe the rebuilt output; the old v3 binary digests identify only
the captured build input. The selected image resolves Mesa to
`25.0.7-2+deb13u1`; the complete runtime package record remains authoritative for
all dependency versions.

The relocated media prefix is `/opt/goby-amd-ffmpeg`. Its relative `$ORIGIN`
library search paths require the entire prefix to move together; no global
`LD_LIBRARY_PATH` is required. Both the image and Compose override select its
matching `bin/ffmpeg` and `bin/ffprobe`. The original software-image tools remain
at `/opt/ffmpeg/9.0.1` with their original evidence.

The rebuilt media runtime retains the strict Dolby Vision and decoder-wakeup
fixes from the native AMD toolchain and the copy-timestamp-progress fix from the
software OCI recipe. Its FFmpeg binaries are new artifacts, so earlier native or
software-image media results must not be relabeled as AMD-image results. The
exact input identities and retained source records are in
[source-pins.amd.json](source-pins.amd.json).

The source-pins file is the frozen build input and retains its build-time pending
status. Likewise, `runtimeAccepted: false` in the build receipt means the builder
does not run or accept the application. Later runtime results belong to the
separate acceptance record; they do not require rewriting the image or its
original build receipt.

## Select one render node

The Docker host must already have an operational `amdgpu` kernel driver and its
required firmware. This image supplies user-space drivers only. Identify the
intended AMD render node on that host and record its numeric group ID:

```sh
render_node=/dev/dri/renderD128
stat -c 'node=%n owner=%u group=%g mode=%a device=%t:%T' "$render_node"
cat /sys/class/drm/"$(basename "$render_node")"/device/vendor
```

The vendor must be `0x1002`. Goby checks the real character device and its sysfs
association at startup and when admitting hardware work. The corresponding
`/sys/dev/char/<major>:<minor>/device/vendor` must be readable in the container.
An inaccessible, foreign or replaced device remains unavailable.

Use the same absolute render-node path on both sides of the container mapping.
For example, do not map a host `renderD129` to container `renderD128`: the basename
must agree with the character device's minor number. Only the selected render
node is required; a `card` node, the whole `/dev/dri` directory, privileged mode
and a root application process are not required. Do not change shared host
device ownership or permissions to configure this delivery.

## Container syscall profile

Keep the supplied `seccomp.amd.json` alongside the deployment files. It derives
from Docker Engine **29.7.2**'s actual resolved default profile with
`cap_drop: [ALL]`. It preserves that profile and adds one allowance:
`kcmp` with argument index 2 equal to `0` (`KCMP_FILE`). The other comparison
types and all other syscall rules remain as captured.

The supplied profile SHA-256 is
`22b86c7effb4cf7189145eb12c241f0bb28f732b50270123c751d31f0766b7f0`.

The AMD user-space driver uses this comparison to determine whether two DRM
descriptors refer to the same open file. The default container profile blocked
it in the recorded run. Encoding returned valid bytes, but the driver emitted
an `os_same_file_description` warning. Goby's strict encoding admission rejected
that stderr. The narrow syscall allowance resolved the warning while retaining
the product's output and stderr checks. No `SYS_PTRACE` capability or unconfined
seccomp policy is used.

The supplied profile is an explicit deployment file, not an automatic merge
with another engine's defaults. Its accepted engine/profile combination is
Docker 29.7.2 on the recorded Linux amd64 worker. Treat an engine or profile
change as a separate deployment configuration to verify.

## Configure and start

Create the private `/etc/goby` directory and application environment as described
in the base guide. Then verify and load the supplied AMD archive and install its
syscall profile:

```sh
sha256sum --check SHA256SUMS
docker image load --input goby-linux-amd64-amd-image.tar
image_id="$(cat image-id.txt)"
docker image inspect "$image_id" --format '{{.Id}} {{.Os}}/{{.Architecture}} {{.Config.User}}'
sudo install -m 0644 seccomp.amd.json /etc/goby/seccomp.amd.json
```

Use the recorded AMD image ID for `GOBY_OCI_IMAGE` in
`deployment.env`. Retain all the base deployment variables and add the actual
host values:

```dotenv
GOBY_AMD_RENDER_NODE=/dev/dri/renderD128
GOBY_AMD_RENDER_GID=REPLACE_WITH_NUMERIC_RENDER_NODE_GROUP
GOBY_AMD_SECCOMP_PROFILE=/etc/goby/seccomp.amd.json
GOBY_AMD_DECODER=vaapi
GOBY_AMD_ENCODER=vaapi
```

The group number is host-specific; it is not the container's primary GID 10001.
The override adds it as a supplementary group and maps the selected render node
with read/write access. Both decoder and encoder default to `vaapi`.
`GOBY_AMD_SECCOMP_PROFILE` is required and must name the supplied profile at an
absolute path accessible to the account running Compose. Keep that file with
the deployment; container recreation uses it again.

Run every Compose operation with both files, in this order:

```sh
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml config --quiet
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml up -d
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml ps
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml logs --tail 100 goby
```

The override preserves the base UID, read-only root filesystem, dropped
capabilities, `no-new-privileges`, resource bounds, loopback port and persistent
paths. It adds the narrowly extended seccomp profile described above and sets
`LIBVA_DRIVER_NAME=radeonsi`. Mesa's installed Vulkan ICD discovery is used;
no guessed `VK_DRIVER_FILES` path or GPU index is forced.

For a bounded device inventory check under the actual container identity:

```sh
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml exec -T goby \
  vainfo --display drm --device /dev/dri/renderD128
```

Use the actual selected node in that command. This checks access and enumerated
profiles, not real decoding, encoding or processing. Play representative media
through Goby and inspect the recorded job/backend and decoded output before
claiming that a path works.

## Managed settings and fallback

The startup environment defines deployment defaults and authorizes the selected
AMD node. A persisted administrator hardware override takes precedence over
those defaults. After replacing an existing software deployment, inspect the
administrator execution settings or `GET /admin/v1/settings`:

- `Runtime.Defaults.Hardware` shows the deployment selection.
- `Runtime.Hardware.Devices` shows authorized opaque device IDs and availability.
- `Runtime.Effective.Hardware` shows the selection used for new work.

Select the available AMD device and both desired axes, or reset only
`Runtime.Hardware` to use the deployment defaults. An API update must preserve
the current `Overrides` object and use the current `Revision`; hardware selection
uses the returned `DeviceId`, not a device path. Existing accepted jobs retain
their captured settings. See [managed execution settings](../../docs/api/managed-execution-settings.md).

| Decode | Encode | Intended codec path |
| --- | --- | --- |
| `vaapi` | `software` | Hardware decode with software encode |
| `software` | `vaapi` | Software decode with hardware encode |
| `vaapi` | `vaapi` | Hardware decode and hardware encode |
| `software` | `software` | Software codecs; a retained device can still serve GPU filters |

For an entirely CPU processing selection, choose both software axes and clear
the managed `DeviceId`. Merely changing one encoder field does not remove GPU
decoding or processing from an existing plan. Environment changes require a
container recreation and still do not erase persisted overrides.

Goby checks the exact hardware encoding tuple before registering a new output.
If that tuple is rejected, it can select the same codec's software encoder while
preserving dimensions, bit depth and client constraints. This is not a general
fallback for a missing GPU, decoder failure or unsupported processing graph.
On this image and driver, a direct AV1 hardware request for 320x180 independently
produced 320x192 output. The real padding-rejection check passed: Goby must not
accept that output as the requested size. Its automatic software-encoder fallback
preserves the requested dimensions and codec where the client contract permits
that fallback. This is a driver geometry limit, not a claim that the rejected
320x180 tuple works in hardware. The actual server HTTP fallback case passed:
the software AV1 output preserved 320x180 and contained all 96 expected frames
over four seconds.

## Media processing scope

The media toolchain includes VAAPI H.264/HEVC/AV1 interfaces and software
equivalents, Vulkan/libplacebo, subtitle rendering, 10-bit conversion, HDR tone
mapping and strict supported Dolby Vision processing. Actual hardware support
depends on the device and driver. GPU decode, GPU encode and GPU filters are
separate stages; none implies that all three executed.

AMD plans can select Vulkan for 10-bit inputs, HDR, deinterlacing and subtitle
composition. CPU/GPU transfers, CPU subtitle rasterization and CPU resizing can
remain part of those paths. Dolby Vision retains software HEVC decoding so its
per-frame metadata reaches the renderer. The supported profile 5/8 and strict
zero-residual profile 7 MEL subset do not include FEL reconstruction or newly
authored Dolby Vision output. Disabling Vulkan tone mapping rejects a graph that
needs it; it does not create an equivalent CPU substitute.

The selected container verification uses PVE CT 104 with AMD GFX1150,
`radeonsi`, Mesa `25.0.7-2+deb13u1`, DRM `3.64`, kernel `6.17.13-3-pve`, and
`/dev/dri/renderD128` with numeric group `992`. Group 992 is an observation of
that worker, not a portable deployment default. The following selected checks
have passed with the non-root container profile and supplied seccomp policy:

| Check | Recorded scope |
| --- | --- |
| Exact hardware encoding admission | Five tuples: H.264 8-bit and HEVC/AV1 8/10-bit |
| Codec output matrix | 39 leaf cases across decode-only, encode-only and combined VAAPI selections |
| Hardware input decoding | Selected H.264 and HEVC/AV1 8/10-bit input matrix |
| GPU processing | Representative HDR, deinterlacing and Vulkan-to-VAAPI subtitle paths |
| Cancellation | Actual GPU cancellation and resource cleanup |
| Rejected AV1 geometry | Independently reproduced non-aligned output padding and exact rejection |

Seven playback cases also passed through the actual container `cmd/goby` HTTP
workflow. H.264 and HEVC each exercised hardware decode only, hardware encode
only, and combined hardware decode/encode: all six outputs contained 1,080
expected frames over 45 seconds. The seventh case verified the four-second,
96-frame AV1 software fallback above. Process evidence recorded the actual FFmpeg
executable digest, UID and arguments; the GPU paths also established an open
render descriptor. Every complete output underwent software decoding and image
validation.

Settings changes left all seven historical admitted plans unchanged. Each
playback stopped with its owned descriptors, processes and cache resources
closed. The application, observer, private Docker infrastructure and independent
PostgreSQL fixture were also closed; the image archive and evidence were retained.

These results retain their individual case boundaries; they are not a count of
independent features or full third-party-client compatibility. The private Dolby
Vision implementation and historical native results do not establish a new
container Dolby Vision acceptance result here. The selected checks do not claim
every resolution, profile, AMD adapter, browser or client.
Intel/NVIDIA devices, arm64, performance SLOs and public distribution licensing
remain outside this AMD extension.

Stop with both Compose files:

```sh
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml stop
```

Retain the selected image archive, deployment configuration and matching
database/state backups. The base guide's schema-aware upgrade/rollback procedure
still applies. Switching back to software also requires reviewing persisted
hardware settings; changing only the image cannot clear an old GPU selection.

Runtime package/driver/feature records are under `/usr/share/goby/amd-runtime`.
The original native media evidence and notices are retained under
`/opt/goby-amd-ffmpeg/metadata/native-v3-input`. New FFmpeg source archives, patch
records, native regressions, header hashes, notices and actual executable digests
are under `/opt/goby-amd-ffmpeg/metadata/amd-oci-build`; the parent metadata
directory contains the new complete installed-file manifest. Inherited
application notices remain under
`/usr/share/doc/goby`. This internal delivery does not claim complete public
distribution licensing.
