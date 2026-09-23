# Internal Linux amd64 OCI preparation

Status: **implementation merged; image build, Compose validation and container
runtime verification remain pending**. The 12 synthetic artifact-checker methods
[passed on test-env](../../docs/development/oci-artifact-checker-verification.md).
That result accepts no actual release input or container. This prepares the first software
container profile. It does not change the current Programs artifact, deploy a
candidate, select E11 for another installation, or complete M6.

The profile uses the fixed Debian amd64 manifest and package snapshots in
[source-pins.json](source-pins.json), a previously verified embedded Goby release,
FFmpeg/ffprobe 9.0.1 built together, and PostgreSQL 17 client tools. PostgreSQL
server, media, application secrets and a reverse proxy remain external.
The selected runtime is a rootful Linux Docker engine without user-namespace
remapping, using an unprivileged container UID/GID of `10001:10001`. Other
runtime/user-namespace and architecture profiles require their own verification.

## Build inputs and evidence

Select a release only after its required source, ordinary/embedded build and
asset gates have passed. Supply the independently trusted SHA256 for both
`goby` and its build `manifest.json`. The input checker binds their bytes,
manifest relationship and ELF64/amd64 header; it never executes Goby or supplies
missing source, embedded-asset or client acceptance evidence.

Prepare a fresh private context on the remote Linux build host containing
exactly these files:

```text
.dockerignore
Dockerfile
build-ffmpeg.sh
check-artifact.py
entrypoint.sh
source-pins.json
toolchain-patches/ffmpeg-progress-copyts-nopts.patch
toolchain-patches/progress-copyts-nopts/native-regression.c
toolchain-patches/progress-copyts-nopts/native-regression.mk
toolchain-patches/progress-copyts-nopts/run-native-regression.py
toolchain-patches/progress-copyts-nopts/README.md
goby
manifest.json
```

Copy the first six files from this directory, the `toolchain-patches` inputs from
[`scripts/test-env/toolchain-patches`](../../scripts/test-env/toolchain-patches),
and the last two files from the selected verified release, preserving bytes.
The patch has one canonical repository copy; do not maintain an independent OCI
variant. The scripts must have LF line endings.
Keep runtime environment files, application state, keys, browser material and
private frontend contribution reports outside this context. The included
`.dockerignore` allows only these inputs. Retain their names, lengths and hashes
with the build receipt rather than relying on an image tag.

The artifact stage checks the two hashes before installing its Python checker
dependency. Only successful complete validation produces a fixed stage-dependency
marker, which precedes the media/runtime package stages. That marker is not an
independent receipt. Its constant contents are intended to permit FFmpeg layer
reuse across changing Goby binaries; actual cache behavior remains untested.
Failed checks retain any partial binding and require a fresh context. A binding
file alone is insufficient: the checker must exit zero.

The media stage verifies the pinned FFmpeg source hash and selected signing-key
fingerprint, and the fixed-commit NVIDIA header archive hash, before compiling.
It uses the existing project's configure flags and one or two compiler jobs.
Before compiling it applies the recorded copy-timestamp progress patch with
zero fuzz, preserving an unpatched source snapshot for the deterministic
[real-reporter gate](../../scripts/test-env/toolchain-patches/progress-copyts-nopts/README.md).
Both baseline and candidate reporter contracts must pass before the matching
FFmpeg/ffprobe pair is installed. Patch bytes, source hashes, harness inputs and
native regression receipts remain in `/usr/share/goby/toolchain/`.
The runtime image checks actual FFmpeg/ffprobe 9.0.1 and PostgreSQL 17 version
commands, records executable hashes and package versions, and retains build
configuration, source archives, recipe files and available upstream legal texts.
These build-time checks do not prove nonroot execution, media correctness,
hardware use, backup/restore behavior or complete dependency/legal coverage.

The APT repositories are fixed historical snapshots. `check-valid-until=no`
permits replay of those snapshots; ordinary archive signatures and package-hash
verification remain enabled. Public APT retrieval uses HTTP so the slim base
can bootstrap its CA package. Source archives use HTTPS and their separate
hash/signature checks. Package availability and the final dependency closure
still need a real build. `SOURCE_DATE_EPOCH` is fixed, but repeat-build image
reproducibility has not been demonstrated.

Only after the coordinated heavy-work window and this profile's builder/storage
prerequisites are ready, the remote Linux invocation has this form:

```sh
docker buildx build --builder "$owned_builder" --platform linux/amd64 \
  --build-arg GOBY_BINARY_SHA256="$verified_binary_sha256" \
  --build-arg GOBY_BUILD_MANIFEST_SHA256="$verified_manifest_sha256" \
  --build-arg FFMPEG_BUILD_JOBS=2 \
  --build-arg SOURCE_DATE_EPOCH=1789430400 \
  --progress plain --tag "$private_image_tag" \
  --iidfile "$owned_run/image-id.txt" --load "$oci_context"
```

Use a separately admitted builder and allocated storage. Budget the builder,
compiler descendants, image import/export, package caches, layers, source trees,
logs and failure preservation on their actual filesystems. Limiting only the
Docker CLI process does not bound daemon or BuildKit work. A cgroup limit or
point-in-time free-memory reading is not a host reservation. Do not prune a
shared engine, alter existing candidate egress or create a default daemon/bridge
as an implicit prerequisite. Engine availability and its owned execution/
preservation boundary must be established before this command is selected.
No builder, daemon, image, network or container has been created in this preparation.

## Runtime configuration

Use a Compose implementation supporting raw environment files, `init`,
`bind.create_host_path: false` and the selected resource settings. The
[Compose specification](https://github.com/compose-spec/compose-spec/blob/main/05-services.md)
defines those fields; actual compatibility remains to be checked. The
[compose.yaml](compose.yaml) intentionally requires an already verified local
image reference, an explicitly reserved loopback port and existing host paths:

| Compose variable | Required value |
| --- | --- |
| `GOBY_OCI_IMAGE` | Verified local image ID/reference, bound to the new build receipt; pulling is disabled |
| `GOBY_HOST_PORT` | Unclaimed, explicitly reserved host port for this installation |
| `GOBY_CONFIG_FILE` | Absolute path to its private runtime `goby.env` |
| `GOBY_STATE_DIR` | Dedicated persistent state directory |
| `GOBY_CACHE_DIR` | Dedicated local cache directory |
| `GOBY_LOG_DIR_HOST` | Dedicated persistent diagnostic-log directory |
| `GOBY_MEDIA_DIR` | Approved, readable media root; mounted read-only |

Precreate the three private directories with real, non-symlink, mutually distinct
and non-overlapping paths, ownership `10001:10001` and mode 0700. They must also
be separate from media and from another installation's state. A bind mount hides
the image's directory ownership, so image-time `install -d` is not host-volume
preparation. The entrypoint never recursively chowns, repairs or deletes host
paths. Parent traversal permissions and any security labeling must permit the
selected UID. User-namespace remapping changes the host UID contract and is not
covered by this initial profile.

The state volume contains the private master key and the separate sibling
`recovery`, `recovery-operations` and `backups` stores. Preserve it with the
matching PostgreSQL state across image changes. Cache remains separately
disposable only under the application's own lifecycle/ownership rules. The
application's current defaults allow 20 GiB transcode cache, 8 GiB per job,
32 GiB backup storage and 8 GiB per backup object; both stores reserve 512 MiB
free space. These limits and the source/build/layer/log budgets must fit the
actual allocated filesystems before execution. They are limits, not reserved
capacity or accepted performance results.

Create `goby.env` privately from [goby.env.example](goby.env.example), replacing
all placeholders. Raw env-file mode preserves values rather than interpolating
`$`; do not add shell quoting or source the file. Supply a unique PostgreSQL 17
database and an ordinary migration-capable role, never a retained candidate's
credentials or a superuser. Both normal and optional restoration URLs need the
explicit TLS root-certificate setting required by the backup layer. The example
uses the image's CA bundle. A private CA needs a separately reviewed read-only
certificate mount and corresponding URL path. Full restoration additionally
needs a distinct database and role.

Compose fixes the image's tool paths, `/media`, private storage paths, listener,
embedded-web selection and software codec selection. Those values override the
runtime env file; do not reuse an old systemd environment and infer equivalent
behavior. Keep the public HTTPS origin and actual trusted-proxy CIDRs consistent
with the reverse proxy. Only TCP is exposed on a loopback host port; REST, ranges,
HLS and WebSocket need the usual proxy behavior. LAN discovery needs a separate
explicit network profile; host networking is not a universal requirement.

The container has a read-only root, empty capability set, no new privileges,
the fixed nonroot identity and a 64 MiB private noexec `/tmp`. The entrypoint
sets umask 077 and `exec`s Goby; the init process forwards signals and reaps
children. `/proc/self/fd` and the application's normal process-group/wait
operations must remain usable under the selected runtime's default security
policy. No privileged mode or disabled seccomp profile is provided.

The initial 2-CPU, 1-GiB/no-swap, 128-PID profile and two-minute stop grace are
unverified starting budgets. Fifteen seconds inside Goby is not a total shutdown
guarantee. A forced kill or merely absent container does not prove clean task,
child-process, lease and database closure. Restart is disabled for the first
evaluation so failure cannot silently turn into repeated startup attempts.
The local logging driver is capped independently from Goby's bounded JSONL logs.

## Required verification and remaining scope

Run the 12 prepared input-checker test methods on `test-env`, then validate
Compose without printing secret-bearing rendered configuration. Actual image
verification must bind the image/platform/layers and all retained input/report
hashes; confirm tool/library loading as UID 10001; complete setup/login, a small
real-media scan and software H.264/AAC path; verify native backup and isolated
restore with the selected PostgreSQL/TLS profile; and observe clean stop/start
with exact state/secret preservation and closed children. Record actual memory,
disk and capture overhead. None of these checks has run.

VAAPI/QSV/NVIDIA interfaces may compile into FFmpeg, but this software profile
contains no device admission or accepted GPU-driver path. Hardware, native
arm64, host durability, broader capacity/media/client profiles and the remaining
M2-M6 obligations remain separate open work. The three native stores, existing
application recovery and all consumed client inputs retain their original scopes.

External distribution remains blocked by the existing Goby license decision and
the complete image/application dependency-notice work. Retained FFmpeg/header
sources and Debian copyright material do not complete that review. No image has
been published, and this preparation does not request a new license decision.

Independent static reviews covered the Docker stage dependencies, selected
runtime configuration, input checker and synthetic test contracts. Corrections
added the complete-check dependency before expensive stages, explicit TLS root
paths, fixed runtime paths and retention of partial checker output. These reviews
did not run tests, syntax validators, tool versions, image builds or services.
