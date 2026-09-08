# Linux verification environment

Goby runtime and integration verification runs on `ssh test-env`, a Debian 13
amd64 host. Local Windows execution is limited to the explicitly authorized
compilation checks. Do not run application tests, smoke tests, browser tests,
or runtime probes on Windows.

## Toolchain baseline

The official release sources were checked on 2026-09-09:

| Component | Version | Installation |
| --- | --- | --- |
| Go | 1.27.1 | `/opt/goby-toolchains/go1.27.1/bin/go` |
| FFmpeg / ffprobe | 9.0.1 | `/opt/goby-toolchains/ffmpeg-9.0.1/bin/` |
| NVIDIA codec headers | n13.1.15.0 | `/opt/goby-toolchains/nv-codec-headers-n13.1.15.0/` |
| PostgreSQL | Debian 17.11 | Dedicated `goby_test` role and database |

Go and FFmpeg are pinned to the latest stable releases observed on that date.
Update the pinned versions and provenance checks when adopting a newer stable
release; never silently replace them with an older operating-system package.
PostgreSQL follows the Debian security-maintained release.

- [Official Go release metadata](https://go.dev/dl/?mode=json) supplies the
  SHA-256 checksum for the Linux amd64 archive:
  `63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445`.
- [Official FFmpeg downloads](https://ffmpeg.org/download.html) supply the
  9.0.1 source archive and detached signature. The bootstrap verifies the
  signature against the published release-key fingerprint
  `FCF986EA15E6E293A5644F10B4322F04D67658D8` in a temporary GnuPG keyring.
- [Official NVIDIA codec headers maintained by FFmpeg](https://github.com/FFmpeg/nv-codec-headers/releases/tag/n13.1.15.0)
  provide build-time interfaces. This header release requires NVIDIA Linux
  driver 610.0 or newer according to its
  [release README](https://github.com/FFmpeg/nv-codec-headers/blob/n13.1.15.0/README).
  The runtime host must meet that requirement; merely installing the headers
  does not install a driver.

## Provisioning

Run these commands from the project directory in PowerShell:

```powershell
ssh test-env "install -d -m 700 /opt/goby-test"
scp scripts/test-env/install-toolchains.sh scripts/test-env/provision-database.sh scripts/test-env/verify-toolchains.sh test-env:/opt/goby-test/
ssh test-env "bash /opt/goby-test/provision-database.sh"
ssh test-env "GOBY_BUILD_ROOT=/dev/shm bash /opt/goby-test/install-toolchains.sh"
ssh test-env "bash /opt/goby-test/verify-toolchains.sh"
```

The bootstrap requires root and installs Debian build dependencies. It uses
dedicated paths and does not replace the host's existing Go installation.
`GOBY_BUILD_ROOT` controls scratch space; its filesystem must permit executable
files. Use `/dev/shm` only when sufficient memory-backed capacity is available.
`GOBY_BUILD_JOBS` defaults to four to limit resource use on a shared host.
Temporary directories created by the script are removed on exit.

FFmpeg includes software H.264/AAC, MP3, Opus, Vorbis, subtitle rendering with
libass, and VAAPI, Intel QSV, and NVIDIA NVDEC/NVENC interfaces. The build enables
GPL components, including libx264; its configuration and build logs are retained
under `/opt/goby-toolchains/`. Matching runtime libraries must accompany these
dynamically linked executables. They are not a portable static distribution.

The database bootstrap creates a non-superuser login role and database named
`goby_test`. PostgreSQL listens on localhost by default. The script refuses to
overwrite a pre-existing unmanaged role or database. It preserves an existing
managed environment file and does not rotate credentials on every run.

## Credentials and application verification

`/opt/goby-test/test.env` is owned by root with mode `0600`; the containing
directory is `0700`. Never copy this file into the repository or print its
contents in command output. It defines:

| Variable | Purpose |
| --- | --- |
| `GOBY_DATABASE_URL` | Application connection to the dedicated PostgreSQL database |
| `GOBY_TEST_DATABASE_URL` | Integration-test connection to the same dedicated database |
| `DATABASE_URL` | Generic command compatibility |
| `GOBY_FFMPEG` / `GOBY_FFPROBE` | Pinned media executable paths |
| `GOBY_FFMPEG_PATH` / `GOBY_FFPROBE_PATH` | Equivalent path aliases |
| `PATH` | Project Go/FFmpeg toolchains and the existing Node installation |

Application source is staged at `/opt/goby-test/repository`. Use an exported
environment in the remote shell, for example:

```powershell
ssh test-env "bash -lc 'set -a; source /opt/goby-test/test.env; set +a; cd /opt/goby-test/repository; go test ./...'"
```

Integration tests may mutate the dedicated test database. Do not aim them at
production credentials or another project's database. Concurrent database
fixtures should use isolated schemas or separate databases to prevent one test
suite from clearing another suite's state.

## Hardware verification boundary

The current virtual machine exposes neither `/dev/dri` nor NVIDIA GPU devices.
The toolchain verification script checks codec availability, real software
H.264/AAC encoding, `ffprobe` JSON extraction, HLS generation, and decoding the
generated HLS. Listing `vaapi`, `qsv`, or `cuda` in `ffmpeg -hwaccels` proves
compiled support only. It does not prove that a GPU, driver, supported media
profile, zero-copy transfer, or hardware tone mapping works.

Hardware execution needs a separate Linux runner with a suitable device and
driver. VAAPI normally needs a render node such as `/dev/dri/renderD128`; QSV
also needs an Intel media runtime; NVIDIA needs a compatible driver and
container device exposure when containerized. Verification must exercise both
decode and encode, unsupported profiles, unavailable devices, and any explicit
software fallback policy. Do not label hardware execution as passed on this
GPU-free machine.

## Shared-host capacity

The initial root filesystem was full. Preparation reclaimed regenerable npm,
Jest, and Go module/build caches after inspecting active processes. Existing
project directories, backups, containers, and volumes were retained. The
bootstrap scripts do not perform global cleanup; inspect current disk and
memory use before future builds. Do not remove another task's live workspace,
container, database, or cache files in use by an active process.

Provisioning and smoke-test results are recorded separately from application
test results. A working toolchain is not evidence of complete Emby API or
third-party-client compatibility.

## Recorded provisioning verification

The final toolchain configuration was verified remotely on 2026-09-09:

| Check | Observed result |
| --- | --- |
| Go archive checksum and executable version | Passed; Go 1.27.1 |
| FFmpeg release signature and executable versions | Passed; FFmpeg and ffprobe 9.0.1 |
| Dedicated PostgreSQL connection | Passed; user/database `goby_test`, server 17.11 |
| Database network exposure | Loopback IPv4 and IPv6 only, port 5432 |
| Environment file permissions | `root:root`, mode `0600`; parent directory `0700` |
| 160 x 90, two-second H.264/AAC sample and stream JSON | Passed |
| Software decode, HLS generation, and HLS decode | Passed |
| VAAPI, QSV, CUDA methods and H.264 encode/decode interfaces | Enumerated successfully |
| libass, MP3, Opus, and Vorbis support | Present in the final build configuration |
| Hardware execution | Not verified; this machine exposes no GPU device |
| Shell syntax for the three provisioning/verification scripts | Passed on Linux |

The final execution log is
`/opt/goby-toolchains/toolchain-verification.log`. Build configuration,
compilation, installation, and bootstrap logs remain alongside it. The initial
full disk was reduced to approximately 563 MiB available after installation;
use task-specific memory-backed Go caches when running integration tests on
this shared host, and recheck current capacity before adding large fixtures.
