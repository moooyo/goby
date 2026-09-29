# Phase 3 filesystem recovery - September 29, 2026

Status: **this local recovery milestone is complete**. It follows the
[process/storage recovery record](phase3-process-storage-recovery-20260929.md)
at `518307c`. It exercises actual nonroot permission denial and actual blocked
filesystem metadata using small, isolated fixtures on `test-env`.

## Nonroot permission recovery

`TestHTTPPhase3NonrootPermissionRecovery` passed in 1.45 seconds as UID/GID
65534, with empty effective, permitted, and bounding capability sets. The
production server, real HTTP, PostgreSQL, and FFmpeg/ffprobe 9.0.1 were used.

The fixture indexed four real MP4 files across two roots and saved catalog
identities, nondefault UserData, server settings, and verified root bindings.
It removed one media file, then changed its containing root to mode 000.
Both opening the directory and opening a surviving media file returned actual
EACCES in that same nonroot process.

The inaccessible scan failed and retained all four catalog records, including
the genuinely missing file. Healthy-root catalog queries and original-media
HTTP bytes still succeeded. Restoring directory permissions allowed a complete
scan to remove only the genuinely missing file. The three surviving IDs,
UserData, settings, and binding revision/fingerprint remained unchanged; no
rebind was needed. A settled rescan also passed.

## Blocked metadata recovery

`TestHTTPPhase3BlockedMetadataRecovery` passed its first remote run in 7.57
seconds. The fixture indexed two real MP4 files in separate libraries, verified
the fault root binding, and saved the catalog, UserData, and persisted roots.
It closed the original application process, ordinarily unmounted its own ext4
volume, flushed only the owned mapper device, and mounted it read-only. A new
application process started without scanning or observing the deep fault root.

After dm suspend, a real binding GET returned HTTP 503 through the production
five-second storage-observation deadline. Exactly one observation remained
active after the caller returned. The external trace identified application
PID `943795`, TID `943802`, blocked in `newfstatat` for `mounted/deep`. Two kernel
observations found the same thread in state D with unchanged syscall state.
Healthy-library catalog queries and exact original-media HTTP bytes continued
while that call remained blocked. The durable snapshot stayed unchanged.

After resume, the same TID's first matching syscall return was successful
(`newfstatat = 0`), the observation count returned to zero, and a fresh binding
GET verified the original fingerprint. Catalog, UserData, and persisted roots
remained exact. Both application processes shut down normally and were joined.
The trace proves an actual blocked metadata lookup; this is not inferred from
device suspend state or HTTP latency alone.

Static review corrected response-header propagation and constrained syscall
return matching before execution. Both additions compile locally for Linux;
no product change was needed in this increment. Passing earlier regressions
were not rerun without a new product change or unresolved concern.

## Execution boundaries

Local work is compilation only; all tests and runtime probes execute on
`test-env`. The permission fixture uses its own temporary directories. A
temporary UID-specific ACL grants access only to the private PostgreSQL socket;
shared directory permissions and shared PostgreSQL are unchanged.

The metadata fixture owns a 512 MiB backing file, one loop device, one
dm-linear mapping, and an ext4 mount inside a private mount namespace. Its
controller accepts only remount, suspend, and resume for that owned mapping.
An independent 50-second watchdog resumes the device on a stuck attempt and
invalidates that attempt. It does not load a kernel module, drop global caches,
freeze shared storage, or restart the shared host.

The watchdog did not fire. Final closure confirmed ordinary unmount, removal
of the owned mapper, detachment of its backing loop, and absence of both
application PIDs. The private database had zero other sessions and zero test
schemas. Its temporary UID ACL was revoked, the socket directory returned to
0700, and the dedicated PostgreSQL cluster stopped with normal fast shutdown.
`pg_ctl` reported no server and its socket was absent. The unmounted backing
file, controller, traces, and logs remain as inactive evidence.

The child server runs production `Server.New` as a separate test-binary
process. This increment does not cover `cmd/goby`, a service supervisor,
blocked media payload reads, scan cancellation during a blocked syscall,
guest reboot/reset, or late-mounted storage. These cases do not complete the
historical two-tier 28-case matrix or final integrated regression.

## Evidence and next steps

Private raw evidence and launchers are retained under local
`.git/phase3-filesystem-20260929/` and remote
`/opt/goby-phase3-fs-20260929-01/`.

| Evidence | SHA-256 |
| --- | --- |
| `permission.log` | `483278cba74b2e363e561ccc2defa6ea29e1e05b74475b385367b2e3e02fbf7b` |
| `blocked-metadata.log` | `32aead4793c1a63cb1c2666545d3429cc85c57ff6cfb491b9904e46d92846139` |
| `metadata.strace` | `0831091b79d0855dd41f1cdc19694dac906eccd01226bd5a54ae88835b08437e` |
| `final-closure.log` | `4faff4c99e839fbee9e5befe8479da7efe8bbe38cae708c629227f1411d090fa` |

The accepted mapper run is `blocked-metadata-Z3noRGyC`. Its trace uses
`strace -f -yy -s 256 -e trace=openat,openat2,newfstatat,statx,getdents64`; only
metadata calls are recorded, without HTTP, database, or media buffer contents.
The private `run-blocked-metadata.sh` and `run-permission.sh` launchers retain
the concrete environment and resource setup.

Both tests are opt-in. The permission selector requires
`GOBY_PHASE3_PERMISSION_RECOVERY=1` and a genuinely unprivileged process.
The metadata selector requires `GOBY_PHASE3_BLOCKED_METADATA=1`,
`GOBY_PHASE3_BLOCKED_CONTROL`, `GOBY_PHASE3_BLOCKED_MOUNT`,
`GOBY_PHASE3_BLOCKED_HOST_MOUNT_NAMESPACE`, and an existing external
`GOBY_PHASE3_BLOCKED_TRACE`. Both require `GOBY_TEST_DATABASE_URL`, executable
real `GOBY_FFMPEG`/`GOBY_FFPROBE`, and an owned `TMPDIR`/`GOTMPDIR` outside the
fault mount. The metadata case uses the existing production server process
helper; normal package runs skip these explicit fault fixtures.

Continue with small actual media-read and packaged-process recovery cases,
then isolated guest lifecycle cases and consolidated regression. Preserve the
relaxed performance gates and simplified execution agreement; do not restart
the historical controller chain as a prerequisite for these local acceptances.

For actual `cmd/goby` plus supervisor verification, use a dedicated database
with its `public` schema: production recovery binding and deployment ownership
cannot be represented by the random-schema `Server.New` fixture. A minimal
temporary service can exercise normal stop, SIGKILL restart, and private-PG
outage with the existing media/state assertions. Guest reset and whole-phase
regression remain separate follow-up work.
