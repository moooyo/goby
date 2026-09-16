# Compiler disk-volume verification, September 16

The r03 probe verified the compiler disk volume and closed its owned resources. This is a functional infrastructure check, not a Go, Node, full-input, or product-test result. Exact pins are in [the checkpoint metadata](compiler-disk-volume-verification-20260916.json).

| Attempt | Scope suffix | Result | Actual volume closure |
|---|---|---|---|
| r02 | `8feb54270744` | Failed: `compiler_worker_mount_missing_or_ambiguous` | Complete |
| r03 | `07dec6bdc80a` | One successful synthetic probe | Complete |

The original r02 failure remains preserved. In the new r03 scope, the worker saw two mount records for the same path, IDs `616` and `583`. The directory descriptor selected mount `583`. Actual cache and temporary-file writes, direct program execution from that ext4 volume, and readback succeeded. Compiler temporary paths remain separate from the test temporary directory.

The probe required **3.5 GiB root free space** before allocating the **2 GiB compiler image**. Its payload unit had a **64 MiB memory limit, zero swap allowance, and 25% CPU quota**, with private mounts/network and a 120-second runtime limit. These are probe boundaries; they do not establish full-suite memory admission or continuous free-space availability.

Both allocation-boundary cases behaved as intended: an injected open failure closed as `not_created`; a replaced pathname failed descriptor binding, refused deletion, and preserved the replacement and originally opened inode. That deliberately retained fixture is distinguished from closure of the actual compiler volume.

Each attempt closed all 38 outer commands and its payload, unmounted the actual volume, detached the loop device, and deleted only the identified owned backing file. Protected metadata and tool pins matched before and after, and the shared lock was released.

Independent capacity review passed for the saved r03 reports, raw output, mount identity, failure boundaries, budgets, and owned closure. It was read-only and produced no separate review receipt. No Go/full suite, product tests, HTTP, or SQL ran in this probe. The complete 25-package suite and the ordinary and embedded package builds still require a new run under this profile.
