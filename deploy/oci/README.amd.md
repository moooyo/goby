# Linux amd64 AMD OCI extension

Status: **IN PROGRESS**. The recipe extends the separately verified software
image with AMD VAAPI drivers, Mesa Vulkan drivers and a rebuilt FFmpeg that
retains all three required media fixes. The previously accepted native
`amd-media-v3` prefix supplies unchanged private libraries and matching
libplacebo headers. Container GPU/media acceptance is still required. A successful
build or `vainfo` listing alone is not that acceptance.

Use this guide with [the base deployment guide](README.md). The same external
PostgreSQL 17 service, private state/cache/log directories, read-only media mount,
embedded administrator UI and archive-based delivery apply. The selected runtime
uses a rootful Docker engine without user namespace remapping, Linux amd64, and
container UID/GID `10001:10001`. No registry publication is included.

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
the captured build input.

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

## Configure and start

Verify and load the supplied AMD archive using its own `SHA256SUMS` and image ID,
as described in the base guide. Use that AMD image ID for `GOBY_OCI_IMAGE` in
`deployment.env`. Retain all the base deployment variables and add the actual
host values:

```dotenv
GOBY_AMD_RENDER_NODE=/dev/dri/renderD128
GOBY_AMD_RENDER_GID=REPLACE_WITH_NUMERIC_RENDER_NODE_GROUP
GOBY_AMD_DECODER=vaapi
GOBY_AMD_ENCODER=vaapi
```

The group number is host-specific; it is not the container's primary GID 10001.
The override adds it as a supplementary group and maps the selected render node
with read/write access. Both decoder and encoder default to `vaapi`.

Run every Compose operation with both files, in this order:

```sh
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml config --quiet
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml up -d
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml ps
docker compose --env-file deployment.env -f compose.yaml -f compose.amd.yaml logs --tail 100 goby
```

The override preserves the base UID, read-only root filesystem, dropped
capabilities, resource bounds, loopback port and persistent paths. It sets
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
The historically observed AV1 320x180 padding case requires actual verification
on this image/driver combination; an encoder listing cannot establish it.

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

Until the separate delivery result records completed container checks, these
are toolchain/implementation capabilities and intended verification targets.
They do not claim every resolution, profile, AMD adapter, browser or client.
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
directory contains the new complete installed-file manifest. Inherited application notices remain under
`/usr/share/doc/goby`. This internal delivery does not claim complete public
distribution licensing.
