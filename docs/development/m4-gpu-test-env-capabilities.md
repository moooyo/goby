# M4 GPU capability inventory on test-env

Observation date: 2026-09-12 (Asia/Shanghai).

This is a read-only capability inventory collected through `ssh test-env`, not
hardware decode or encode acceptance. No media decoding, encoding, scanning, or
application workload was started. Services, databases, HBA, and application
configuration were unchanged. Private application environment files were not read.

## Observed host and device exposure

| Item | Observed result |
| --- | --- |
| Distribution | Debian GNU/Linux 13.6 (trixie) |
| Architecture | `x86_64` |
| Kernel | `6.12.107+deb13-cloud-amd64` |
| `/dev/dri` | Directory absent |
| DRM card device entries | No matching `/sys/class/drm/card*/device` entries |
| NVIDIA device nodes | No matching `/dev/nvidia*` entries |
| NVIDIA query utility | `nvidia-smi` not found |
| PCI display-class entry | `/sys/bus/pci/devices/0000:00:01.0`: `class=0x030000`, `vendor=0x1234`, `device=0x1111`; no driver symlink was reported |

The exposed devices do not provide the prerequisites for actual VAAPI, QSV, or
CUDA/NVENC execution on this runner. This observation does not identify hardware
that might exist outside the virtual machine's exposed devices.

## Observed FFmpeg interfaces

The project-owned executable
`/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg` reports FFmpeg 9.0.1, built with
GCC 14 (`Debian 14.2.0-19`). Its `-hwaccels` output lists `cuda`, `vaapi`, `qsv`,
and `drm`.

Its encoder list includes H.264, HEVC, and AV1 interfaces for NVENC, QSV, and
VAAPI; MJPEG and MPEG-2 interfaces for QSV and VAAPI; VP8 for VAAPI; and VP9 for
QSV and VAAPI. These are compiled interfaces, not evidence of an accessible
device, a working driver, or successful GPU execution.

## Traceable read-only queries

The following commands were included in the remote inventory. Device inspection
also read the `class`, `vendor`, and `device` files of PCI display-class entries
under `/sys/bus/pci/devices/` and checked their `driver` symlinks.

```powershell
ssh test-env 'uname -srm; cat /etc/os-release'
ssh test-env 'if [ -d /dev/dri ]; then ls -ld /dev/dri; ls -l /dev/dri; else printf "NO_DRI_DIRECTORY\n"; fi'
ssh test-env 'if command -v nvidia-smi >/dev/null 2>&1; then nvidia-smi --query-gpu=name,driver_version --format=csv,noheader; else printf "NVIDIA_SMI_NOT_FOUND\n"; fi'
ssh test-env '/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg -hide_banner -version | head -n 2'
ssh test-env '/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg -hide_banner -hwaccels'
ssh test-env '/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg -hide_banner -encoders 2>/dev/null | grep -E "vaapi|qsv|nvenc"'
```

These excerpts preserve the queries used within the batched SSH commands; they
were not rerun to prepare this document.

## Acceptance boundary and next prerequisite

[M4f verification](verification-m4f-video-seek.md) and
[M4g verification](verification-m4g-media-refresh.md) retain actual GPU execution
as outstanding scope. The [transcode engine contract](transcode-engine.md#hardware-decode-and-encode)
separates command construction and compiled interfaces from hardware execution
evidence. This inventory does not change that status or establish completion of
the overall M4 milestone.

The next hardware verification requires a remote Linux runner exposing a
supported GPU, its matching driver/runtime, and device access for the application
account. Isolated, bounded media samples must then exercise hardware decoding
and hardware encoding independently, followed by the combined pipeline. Record
the actual selected device and arguments, output integrity, and relevant failure
behavior. Any successful result applies only to the tested device, driver, and
media combinations.
