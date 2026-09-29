# Phase 3 process, PostgreSQL, and ENOSPC recovery - September 29, 2026

Status: **this local recovery milestone is complete**. It follows the
[compound/catalog continuation](phase3-concurrency-recovery-20260929.md) at
`5a87436`. Real application-process termination, private PostgreSQL crash/restart,
and derivative-cache ENOSPC were exercised. A reproduced control-file cleanup
defect was repaired and verified with actual filesystem exhaustion.

## Product repair

When an exclusively created cache control file could not be written because
the filesystem was full, its incomplete `.owner.json` remained. Normal abort
then classified the workspace as unsafe, retained one builder and 16 KiB of
reservation, and rejected a same-key retry even after space was restored.
The original failure was observed on a private 16 MiB tmpfs, not through errno
injection. The previously published entry survived, but retry and Close failed.

`writeControl` now removes its own incomplete file after a Write or Sync error
only when the current name, reopened regular file, and still-open creating
descriptor have matching identities. It retains the original error, including
ENOSPC. Replacement files, symlinks, hardlinks, and unverifiable identities are
retained and rejected. Successful writes, existing-file rejection, startup
validation, and handling of unknown ownership records are unchanged. This
uses the existing private-directory/exclusive-writer model; it does not claim
atomic protection against an external writer continuously renaming files.
The repaired run used a fresh isolated fixture and proved retry within that
run. It does not automatically repair malformed markers left by an older build.

## Actual recovery results

| Scenario | Final result |
| --- | --- |
| Cache owner initialization on full storage | Passed: actual `.owner.json` write returned ENOSPC, without ErrUnsafe; builder/reserved bytes returned to zero; old entry remained exact; same-key publication and Close/Open recovery succeeded. |
| Preview payload on full storage | Passed: strace confirmed a real write to `frame-000000.jpg` returned ENOSPC, with no owner-write ENOSPC. The task failed with `executor_failed`, retained all catalog/UserData/preview rows and original HTTP BIF bytes, then completed a new forced run after only its filler was removed. |
| Application SIGKILL | Passed while a real force-probe scan, preview task, and media child were active. The parent verified signal termination, started a new process, and checked 20 catalog identities, confirmed UserData/settings, interrupted old work, fresh scanning, original-media bytes and new preview delivery. |
| PostgreSQL immediate stop/restart | Passed against a dedicated PostgreSQL 17.11 cluster. Catalog HTTP returned 500 during the outage. Postmaster start time changed and PostgreSQL logs confirmed WAL recovery. After explicit application restart, the same durable-state and new-work checks passed. |
| Cache package race suite | 30 parent tests passed with no failures or race report. The one opt-in real-ENOSPC case was skipped in this ordinary package run and separately passed in its actual mount fixture. |

The final process/database journey took 10.44 seconds. All three application
generations were joined, and each reported `orphan_cleanup_count=0`. Both faults
left 20 preview children interrupted; recovery terminated the old work rather
than silently replaying it. The final preview ENOSPC journey took 8.80 seconds
and closed with zero builders, pending publications, readers, busy entries,
reserved bytes, active operations, and acquired database connections.

The child process runs production `Server.New`, real HTTP, real FFmpeg/ffprobe,
and production workers. It is launched as a separate test-binary process and
receives an actual SIGKILL. It does not cover `cmd/goby`, a service supervisor,
automatic same-PID reconnect, guest reboot, or power-loss durability. Database
recovery explicitly starts a new application process.

## Isolation and closure

All tests ran on `test-env`; local work was compilation only. FFmpeg/ffprobe
9.0.1 and Go 1.27.1 were used. The ENOSPC fixtures used separate private mount
namespaces with bounded 16 MiB tmpfs mounts. Media, PostgreSQL, logs, build output,
and syscall traces stayed outside the fault filesystem. Fillers were outside
the cache root. No shared filesystem was filled.

The dedicated PostgreSQL cluster was under
`/opt/goby-phase3-recovery-20260929-01/pgdata`, used only its private Unix socket,
and retained `fsync=on`, `synchronous_commit=on`, and `full_page_writes=on`.
Its fixed control script operated only that PGDATA. After verification it was
stopped with a normal fast shutdown; `pg_ctl` reported no server, its socket
was absent, and the log recorded a completed shutdown checkpoint. Shared
PostgreSQL and the host were not restarted. Cluster files and logs remain as
inactive evidence.

The first initdb attempt under `/opt/goby-test` never started PostgreSQL because
the postgres OS user could not traverse that parent. Moving the new cluster
location to its own `/opt` prefix fixed setup without changing shared directory
permissions. Three new cleanup tests initially omitted the required 0700 mode
on their temporary directory; correcting that fixture produced the passing
race run. Neither setup failure is counted as product recovery acceptance.

The ENOSPC launcher archived the bounded fault filesystem before ordinary
unmount. The failed owner-initialization fixture was preserved for inspection;
no deletion of that failed cache was used to establish a successful retry.
The preview test also retained its cache until the launcher archived/unmounted
it, after checking recovery and closed resources.

## Reproduction and evidence

Opt-in entry points:

- `TestCacheRealENOSPCDuringOwnerInitializationRecovers`: set
  `GOBY_PHASE3_CACHE_ENOSPC=1` in an empty private tmpfs namespace and provide
  `GOBY_PHASE3_ENOSPC_MOUNT` plus `GOBY_PHASE3_ENOSPC_HOST_MOUNT_NAMESPACE`.
- `TestHTTPPhase3PreviewENOSPCRecovery`: set `GOBY_PHASE3_ENOSPC=1`, the same
  mount variables, and an external `GOBY_PHASE3_ENOSPC_TRACE`. Trace the compiled
  test with `strace -f -yy -s 0`; the test requires the real payload errno.
- `TestHTTPPhase3ProcessAndPostgresRestartRecovery`: set
  `GOBY_PHASE3_PROCESS_RECOVERY=1`, `GOBY_PHASE3_RECOVERY_DATABASE_URL`, and a
  `GOBY_PHASE3_RECOVERY_PG_CONTROL` script accepting only owned-cluster
  `stop` (immediate) and `start` operations. Never point it at shared PostgreSQL.

All server cases also require configured media tools and owned temporary paths.
The ENOSPC and process cases were repeated after the product repair. The syscall
tracer was unpacked into the private test directory; no shared package was
installed. Its traces omit write-buffer contents.

Raw results are under local `.git/phase3-recovery-20260929/` and remote
`/opt/goby-test/codex-recovery-20260929-01/artifacts/`.

| Local evidence | SHA-256 |
| --- | --- |
| `cache-enospc-before.log` | `d5d68167dad7e11b7c8f519bd9e80e21b96933cb97b7bfe6bdc58b2c1ecaff30` |
| `cache-enospc-after.log` | `2269ec23a149748f95b237b9b7ee37b457acf27434c8fbd7290c3e0a695f8f42` |
| `preview-enospc-after.log` | `342217b74800b6a6ad9035e4a539ae420624c230a8ef81d68a6e73b56024aae9` |
| `process-20260929-184249.jsonl` | `c5963cf48eafd81c08a16909146afe5cf4d03ce7c2ed97b35cad542a01294982` |
| `cache-race-20260929-184441.jsonl` | `6cc7fcb5274746a7b098af17428c76a30ae5339d53e5d6bbaf7ed8556ad5f2a3` |

## Remaining work

Actual blocked filesystem operations, non-root permission recovery, packaged
CLI/supervisor behavior, and isolated guest reboot/reset/late-mount cases remain
open. These small cases do not fill the historical two-tier 28-case matrix.
Whole-phase final regression and migration/backup/recovery composition also
remain open. Continue with small owned filesystem cases before expanding the
fault campaign, preserving the relaxed performance and simplified execution
agreement.
