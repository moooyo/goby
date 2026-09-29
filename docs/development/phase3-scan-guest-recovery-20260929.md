# Phase 3 blocked-scan and guest recovery - September 29, 2026

Status: **this functional recovery increment is complete**. It follows the
[media-read/CLI recovery record](phase3-read-cli-recovery-20260929.md) at `242d6d8`.
Actual blocked-scan cancellation and three isolated guest lifecycle cases passed.
No product change was required. The next increment is consolidated regression
and migration/backup/recovery composition, not another controller-generation cycle.

## Blocked scan cancellation

`TestHTTPPhase3BlockedScanCancellationRecovery` passed on `test-env`. The first
run passed in 3.50 seconds. Review then added explicit positive observations of
the retained evidence reservations; that final test passed in 3.77 seconds.

The fixture uses two libraries with one real MP4 each, production server child
processes, real HTTP, and an owned ext4/dm-linear volume in a private mount
namespace. After the initial scan and ordinary application shutdown, it
unmounts and remounts only its own volume, starts a new application process,
suspends the mapper, and starts a scan without warming the deep media directory.
The trace and two kernel observations confirm that application TID `952733`
is blocked in `newfstatat` on the fault mount.

The cancellation request returns HTTP 202 and persists `cancel_requested=true`.
While the syscall remains in D state, the job stays Running with no finish time.
Exactly one evidence pass remains active. Its configured reservations stay at
1 GiB and 4,362 descriptors before and after cancellation; these are admission
reservations, not observed resident memory or a count of open descriptors.
A different library completes a cached scan, catalog query, and exact original
media response while the cancelled worker is still blocked.

Resuming the device lets the same syscall return. Only then does the original
job become Cancelled, with zero scanned/added/updated items. Active and retiring
passes, cleanup failures, reserved bytes, and reserved descriptors return to the
zero baseline. Catalog, UserData, and root approval remain exact. A new scan of
the original root then completes successfully, and both application generations
close normally. The five-second binding-GET observation limit is not applied to
this scan syscall.

The accepted fixture is `blocked-scan-cancel-dhW2if4H`; the earlier positive run
is `blocked-scan-cancel-a9nhbyUD`. Both used ordinary unmount and released their
own mapper and loop. Neither watchdog fired.

## Dedicated guest environment

All guest work stays inside `test-env`. A new nested QEMU VM owns an 8 GiB
copy-on-write OS disk and a separate 512 MiB raw media disk, with 1 GiB RAM and
two vCPUs. Final execution uses nested KVM, QEMU 10.0.13, PostgreSQL 17.11, the
actual `cmd/goby` binary from the preceding repair, and FFmpeg/ffprobe 9.0.1.
The application runs as UID/GID 65534 without capabilities. The CLI binary hash
remains `7acd0f4add2106fec230fa56a19e01a77a2aebfd41f89e9b53a70f87b42f6f94`.

QEMU and its missing libraries were unpacked into the owned directory on
`test-env`; they were not installed into its shared package environment. Package
installation happened only inside the disposable guest. SSH and application
forwards bind host loopback ports 22089 and 58099. Existing PVE guests, including
VM106, were not restarted or repurposed.

The [Debian 13 cloud image](https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2)
was verified against its published SHA-512 manifest before use:
`95e110dfcdbd0ed8a82a75ed9579802f9950cabf51a810dcc6388e81bc778188713878b9f28d583a0ea602fbf48b35996ae9ad37f584166d8fbd6489df248f53`.
The ordinary GRUB boot path repeatedly reset in this fixture under both KVM and
TCG. Directly booting that same image's extracted kernel and initrd succeeded,
first under TCG and then under KVM. The accepted lifecycle run uses KVM and
kernel `6.12.107+deb13-cloud-amd64`. It exercises actual Linux guest reboot/reset,
not firmware or bootloader recovery, a PVE host reset, or physical power loss.
Failed bootstrap logs were retained separately from application acceptance.

PostgreSQL, recovery state, keys, caches, logs, and evidence live on the OS disk.
The media library lives below the dedicated media mount; its configured allowed
parent is an existing directory on the OS disk. A separate enabled oneshot unit
normally mounts the original ext4 UUID before Goby. Goby orders after that unit
but does not require or start it, allowing a deliberate absent-media boot.

## Actual guest results

The driver stores the exact catalog/UserData/settings checkpoint outside the
guest before each fault. It uses boot ID changes, stable machine ID, and stable
media filesystem UUID to identify recovery. Application PIDs can repeat across
boots and are not treated as independent proof of a reboot.

| Scenario | Actual result |
| --- | --- |
| Normal guest reboot during active scan/preview work | Boot ID changed; PostgreSQL and Goby started automatically. The old scan was Cancelled and the preview task Interrupted. Durable state and new work passed. |
| QMP `system_reset` during active scan/preview work | Boot ID changed; old scan and preview task became Interrupted. PostgreSQL logs confirm unclean shutdown, automatic recovery, and WAL redo. Durable state and new work passed. |
| Reboot with the media unit disabled | Goby and PostgreSQL became ready while the media volume remained unmounted. All 20 catalog IDs and user/settings state remained. The binding was unavailable, media returned 503, and a scan failed without deleting the catalog. |
| Mount the original volume after that boot | The same boot, App PID `806`, and InvocationID continued. The original filesystem and approved binding became verified without rebind. New scanning, playback, and previews succeeded. |

All three recovery paths preserved the same 20 real-media identities, nondefault
UserData, and settings. Each completed a new forced scan, exact 2,199,678-byte
original MP4 delivery, forced preview generation, and a 33,940-byte BIF response.
The BIF assertion checks successful new generation and delivery/header; it does
not independently decode every BIF frame. Real media subprocess activity was
observed before reboot/reset without assigning every subprocess to one exact
preview child.

Observed boot IDs were `2f59b07a-845e-41f4-b9c4-768b48917d18` before the campaign,
`a5740e9a-ce2e-462a-88d7-f0ee544e7330` after normal reboot,
`12811e50-b13e-42a9-b5d7-6da0ec0b4471` after reset, and
`ec613430-0169-4ed2-9ce8-cdfadaa9a97a` for the late-mount boot. The media UUID
remained `9cc207bf-7f14-4ca8-911c-c532fa357695`.

Final review moved the driver's late-mount ownership flag before the controller
call, so a lost acknowledgment after disabling the mount cannot skip cleanup.
The successful path above is unchanged. This subsequent failure-path guard
received remote Python syntax verification; no separate injected lost-acknowledgment
case is claimed.

## Closure and evidence

The guest restored its normal media boot policy and shut down normally. QEMU
is absent and both host forwards are released. Offline read-only inspection
confirmed the enabled mount unit, the exact CLI binary, PostgreSQL crash recovery,
and its final completed shutdown. The raw PostgreSQL log contains NUL gaps after
reset; the original is retained, and a separate text extract removes NUL bytes
only for reading the relevant log lines.

All inspection loops, scan-fault loops/mappers, and scan child processes are
closed. The separate PostgreSQL cluster used for the scan test is normally
stopped with its socket absent. Shared PostgreSQL and the `test-env` host were
not restarted. Guest disks, media, private configuration, checkpoints, and
success/failure logs remain inactive for reuse or inspection.

Raw evidence is under local `.git/phase3-scan-guest-20260929/`, remote
`/opt/goby-phase3-guest-20260929-01/artifacts/`, and the two scan-fault directories
under `/opt/goby-phase3-fs-20260929-01/`.

| Local evidence | SHA-256 |
| --- | --- |
| `scan-cancel.log` | `16a13b0b152309c2b1f187bc0fc5a671c288b57b0aa46f6842fbc58a7cbbe032` |
| `guest-result.jsonl` | `faa813ea254af36a1741f6eac94b99953291e4088e6e529b5e88a7f955c3ce3d` |
| `guest-postgres-recovery.txt` | `c6b56d1803940a264589ff679439bd033f90f9cdc58368a936d75958896f123d` |
| `final-closure.log` | `ac3357324f1eb31d8c694abb2f0d5001fafae14f2d852f4fcc75050d2cdaa189` |

The scan test requires `GOBY_PHASE3_BLOCKED_SCAN_CANCEL=1` and the existing owned
mount/control/host-namespace/metadata-trace contract. The guest driver is
`scripts/test-env/phase3-guest-lifecycle-recovery.py --config ABSOLUTE_JSON
--checkpoint ABSOLUTE_HOST_CHECKPOINT`; it reuses the adjacent CLI driver and
requires an explicitly provisioned fixed controller. Credentials and VM control
paths remain in private configuration, outside tracked source.

Proceed with the [final verification entry](phase3-final-verification-plan-20260929.md)
for consolidated regression and migration/backup/recovery composition.
Keep the completed fault cases at their recorded source scopes unless a relevant
change or failure justifies repeating them. These results do not claim the full
historical two-tier 28-case matrix, strict performance SLOs, release packaging,
or physical power-loss durability.
