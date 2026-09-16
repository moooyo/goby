# Test environment capacity review and cleanup, September 16

The user confirmed that `test-env` was available, requested root filesystem
cleanup before further verification, and asked why the previously expanded
space was not being used. The expansion was already active. No product test or
build ran during this maintenance.

## Expansion and current allocation

The [September 10 growth receipt](test-env-disk-growth.json) records successful
online partition and ext4 growth. The September 16 SSH inspection of host
`test-env` found exactly the same disk and filesystem capacities:

| Measurement | Bytes |
| --- | ---: |
| `/dev/sda` virtual disk | 104152956928 |
| `/dev/sda1` root partition | 104018722304 |
| Root capacity reported by `df` | 102297296896 |
| Unpartitioned space reported by `sfdisk --list-free` | 0 |
| Root available on September 10 | 63770054656 |
| Root available immediately before this cleanup | 1458827264 |

The virtual disk is 97 GiB. The root filesystem block count, 25395195 at 4096
bytes per block, fills its partition to the final partial block. The other two
partitions are the existing boot partitions. There is no separate unused data
disk or ext4/xfs/btrfs data mount in this observation. `/tmp`, `/dev/shm` and the
paused M2 mount are tmpfs; their capacity is memory, not additional disk space.

The earlier [Go cache relocation](m5h-go-cache-relocation.json) and
[dependency relocation](m5h-dependency-relocation.json) did use persistent disk
space. On September 16, `/opt/goby-test/go-caches-m5h/build`, its `modules`
directory and `/opt/goby-test/exec-work-m5i` still existed on the root device.
The old `/dev/shm/goby-go-cache` and `/dev/shm/goby-go-mod` symlink paths were
absent. Their historical relocation must not be replayed or assumed to describe
current path resolution.

The disk-usage inventory found accumulated Goby workspaces, other projects'
working trees, toolchains and caches. The September 10 free-space observation
does not describe current availability. No additional partition or filesystem
growth is indicated by the current device geometry.

## Cleanup completed

The first operation removed only the `build-cache` directories from 14 closed
`audit-fixes-20260913-*` Goby verification workspaces. Saved closure reports,
current inactive/failed unit state, absent process references, standard Go cache
layout and exact directory identities were checked before removal. Saved reports
and sibling metadata were unchanged afterward.

| First operation | Bytes |
| --- | ---: |
| Root available before | 1458827264 |
| Root available after | 4993187840 |
| Increase | 3534360576 |

The private receipt remains on `test-env` at
`/opt/goby-test/root-cleanup-20260916-yubtzaau/result.json` with SHA-256
`56a27960cd5ec24a6f5c650a4ba0a0adcae632b3f2cbe7981fe372bfe7e4f34e`.

The second operation removed only `go-cache` beneath
`/opt/goby-test/exec-work-m3e/main-schema27-build-01` through
`main-schema27-build-04`, after the same directory, cache-layout, inactive-unit
and process-reference checks. These four compiler caches occupied 563818496
allocated bytes. All four targets are absent; the 118 regular sibling evidence
files retain their hashes and metadata, and sibling directories are unchanged.
The first historical build remains failed and the other three remain passed.

| Second operation | Bytes |
| --- | ---: |
| Root available before | 4993101824 |
| Root available after deletion | 5556862976 |
| Increase during this operation | 563761152 |
| Root available at independent final observation | 5556793344 |

The second private receipt is
`/opt/goby-test/root-cleanup-20260916-axgvs6xj/result.json`, 50219 bytes, SHA-256
`6884538635f281cddf2913b43321c141e4b0b175f3f0489e036a8490913389eb`.
Root independently read this receipt, matched its hash, confirmed all four
targets were absent and observed final free space. Across both operations,
18 closed compiler caches were removed and root free space increased by
approximately 4.10 GB to 5.56 GB. Exact free-space differences also include
receipt allocation and concurrent filesystem activity.

No application, PostgreSQL, Docker or service operation was performed. Source,
module dependencies, binaries, database directories, logs, reports and the
paused M2 RAM workspace were retained. In particular, the frozen module cache
required by the existing verification worker remains at
`/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356/modules`.

## Execution implications

The user's availability message resolves the previous unanswered-window blocker.
The subsequent [original Library theme diagnostic](library-theme-diagnostic-result.md)
passed once with unchanged business budgets; follow the active execution plan
for the broader package diagnosis. Current free
space, memory and protected process identity must be checked before admission;
historical recovery receipts are not fresh liveness observations.

Use explicit persistent paths for new large build inputs and include temporary
files, retained failure evidence and final archives in the disk budget. Reuse
compatible dependency caches and retire disposable compiler caches after their
owning task is closed. Preserve the selected worker's isolation and resource
limits; a successful disk cleanup does not justify an unbounded RAM allocation.

The subsequent [disk compiler-volume check](compiler-disk-volume-verification-20260916.md)
uses the expanded root filesystem explicitly. It preallocated and formatted a
2 GiB image, executed a small program and verified cache/temporary writes inside
the isolated service, then unmounted, detached and removed its owned backing
file. Root free space was 5474127872 bytes before allocation and 3326570496 bytes
after allocation. The full adapter now points compiler `GOCACHE`, `TMPDIR` and
`GOTMPDIR` at its own root-backed compiler volume; actual test temporary
directories retain their existing fixture behavior. This corrects the prior
full worker's RAM-backed compiler storage. A [fresh full run](programs-disk-full-preparation.json)
has started; its final result and closure remain pending.
