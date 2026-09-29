# Phase 3 media-read and CLI recovery - September 29, 2026

Status: **this recovery increment is complete**. It follows the
[filesystem recovery record](phase3-filesystem-recovery-20260929.md) at `9a092e7`.
An actual blocked media read passed. Running the real `cmd/goby` entry point
then reproduced a PostgreSQL crash-recovery startup defect that the earlier
`Server.New` process fixture did not exercise. A focused repair and the complete
CLI/supervisor recovery journey passed against the retained deployment.

## Product repair

A private PostgreSQL immediate restart during an active scan left nine catalog
relations belonging to the old scan session: temporary scan tables, their
indexes, and temporary TOAST tables/indexes. PostgreSQL eventually removes these
crash remnants. Before that cleanup, `CheckDatabase` rejected them as foreign
database scope, so the real CLI repeatedly exited before starting HTTP.

The failure was reproduced a second time with immediate catalog capture. All
nine relations had `relpersistence='t'` and PostgreSQL's
`pg_is_other_temp_schema(...)` returned true, including the temporary TOAST
namespace. The configured role had no administrative capabilities. A direct
binding diagnostic returned `recovery database binding or retained data changed`.
An earlier, delayed catalog inspection saw zero relations after cleanup and
was insufficient to identify the cause; the immediate reproduction resolved it.

`validateRecoveryScope` now excludes a relation only when both temporary
persistence and PostgreSQL's native other-session temporary-namespace predicate
are true. It performs no DROP or repair DDL. Namespace-name prefixes alone do
not grant an exception, and foreign permanent/unlogged objects remain rejected.
Ownership, deployment-marker, schema, and persistent-data checks remain in place.

The new integration case fails on the original code and passes after this
change. It holds a second physical session with a real temporary heap, primary
index, TOAST table, and TOAST index; verifies their native identities; then
checks acceptance and unchanged payload. Its permanent/unlogged foreign-schema
negative cases remain rejected. The existing real backup/restore round trip
and six schema-drift rejection cases also pass: three parent tests, no skips.

## Actual media payload stall

`TestHTTPPhase3BlockedReadRecovery` passed in 3.14 seconds on `test-env`.
Its private ext4/dm-linear volume contains a real MP4. A metadata-only HEAD warms
the source path, then Sync plus range-scoped `FADV_DONTNEED` removes four interior
pages. `mincore` confirms all four target pages are nonresident. No global cache
drop is used.

An HTTP Range request entered a real `read` syscall on the exact media descriptor
while the device was suspended. The test observed the same kernel D-state wait
before and after client cancellation. The client returned while the actual HTTP
handler and its original-stream lease remained active; healthy catalog queries
and exact healthy-media bytes continued. This does not apply the metadata
five-second or media-open twenty-second deadline to payload reads.

After resume, the same syscall returned 16,384 bytes. The handler returned, its
exact lease moved to completed history, and no descriptor to that source remained.
A new Range request returned the precise expected bytes and Content-Range.
Catalog and UserData snapshots remained unchanged.

The first attempt exceeded the trace reader's 16 MiB bound because tracing all
reads also traced the test reading its own trace. Restricting strace with `-P`
to the single owned source fixed the launcher. That attempt is retained as a
fixture failure, not a passing storage result. The accepted run is
`blocked-read-pXWTsfRp`; both attempts ordinarily unmounted and released their
own mapper/loop devices, and neither watchdog fired.

## Actual CLI and supervisor journey

The production CLI was built normally from `./cmd/goby`, without a test-process
entry point. A temporary runtime systemd unit ran it as UID/GID 65534 with no
capabilities, `Restart=on-failure`, `RestartSec=2s`, and `KillMode=control-group`.
A small ExecStopPost observer retained systemd's exit facts before inactive-unit
unloading; it did not restart the application. This is API/process acceptance,
not release-installer or complete dashboard-package acceptance.

The fixture uses 20 real MP4 files and a dedicated `public` database owned by
the non-administrative `goby_cli_recovery` role. Its private PostgreSQL 17.11
temporarily listened only on loopback port 55989, with matching database/role
access rules. Production Config.Load also enables transcoding by default, so
its cache explicitly points into the owned runtime directory. Initial attempts
using the cluster-bootstrap superuser and an omitted transcode-cache directory
were setup failures; neither was counted as product acceptance.

The original full journey passed normal stop and SIGKILL recovery but encountered
the PostgreSQL startup defect. Repeated failed automatic starts were stopped;
the driver records `passed=false` with `driver_interrupted`. The database, media,
cache, recovery state, and failed evidence were retained. A saved checkpoint
from before the focused reproduction supplied the exact 20 catalog IDs, complete
UserData, and settings for the repaired run. Its first step verified that same
state without reinitializing the database or rewriting the checkpoint values.

| Final case | Observed result |
| --- | --- |
| SIGTERM | Exit code 0; no automatic restart during the observation window; explicit start preserved durable state. |
| SIGKILL during active work | Actual scan and preview child were running with real media subprocesses. Signal 9 termination was recorded; systemd started a new PID/invocation and incremented NRestarts. Abandoned work became interrupted. |
| Private PostgreSQL immediate restart | The HTTP outage was observed, the old application exited with code 1, and only systemd started the successor after PostgreSQL returned. No manual application start was used for this transition. |
| Work after each abnormal restart | All 20 IDs, UserData, and settings matched; a new forced scan, exact 2,199,678-byte original-media response, forced preview generation, and 33,940-byte BIF delivery passed. |

The successful database restart immediately exposed nine old temporary relations
in the catalog before the automatic successor became ready. The focused test
independently proves acceptance while the other session retains its temporary
objects; the real journey verifies automatic crash recovery from that condition.
The media
subprocess evidence establishes real activity but does not assign every PID to
one specific scan or preview child. Completed preview children may stay completed;
the assertions require abandoned active work to become interrupted.
The BIF check verifies actual delivery and its header after a successful new
preview task; it does not separately decode every frame in that response.

## Verification and closure

Local operations were compilation only. The blocked-read test, before/after
regression, actual CLI faults, Python compilation, and runtime checks all ran on
`test-env`. Go 1.27.1 and FFmpeg/ffprobe 9.0.1 were used. The repaired CLI binary
SHA-256 is `7acd0f4add2106fec230fa56a19e01a77a2aebfd41f89e9b53a70f87b42f6f94`.

Final closure confirmed an inactive application, empty service cgroup, released
HTTP port, absent read-fault mappers and backing loops, zero other database client
sessions, and all 20 movies retained. The temporary socket ACL was revoked. The
private PostgreSQL cluster shut down normally; its socket and TCP listener were
absent. The temporary runtime unit was removed and the private cluster's original
socket-only network configuration restored. No shared PostgreSQL or host was
restarted. Dedicated databases, runtime files, backing images, and logs remain as
inactive evidence.

Raw evidence is retained under local `.git/phase3-read-cli-20260929/`, remote
`/opt/goby-phase3-cli-20260929-01/artifacts/`, and the two read-fault directories
under `/opt/goby-phase3-fs-20260929-01/`.

| Local evidence | SHA-256 |
| --- | --- |
| `blocked-read.log` | `145ec6287f90cbd5ff409b3f0c86b27212a18b24404693256762a59b8f6dc620` |
| `temp-scope-before.log` | `e4941ace018daa68a585b2f98f68afe4eb6a9a603803dda7c25ab347d16579a9` |
| `temp-scope-after.log` | `3c887abd58829b6900cb4c59f3dc7f0e4d62680716497c8f74e1d38ad25b33f5` |
| `cli-before.jsonl` | `e8efebce5554d7646e74f55048a429561d2c80a38daceceb0198dcdbc305d2b4` |
| `cli-after.jsonl` | `b3ab37f6f28efc94733020ff85ef2cb16599d55a67a70679b26e68731de88713` |
| `orphan-temp-after.jsonl` | `2bef0051c046c79c7c4ace66872598a616834bf314f1c29f5fb65d3820835e96` |
| `final-closure.log` | `8f00f261b355c6082f47ae2679208a5cbff6d6b7776028c30f018b419137c391` |

## Reproduction and remaining work

The blocked-read selector is opt-in through `GOBY_PHASE3_BLOCKED_READ=1` and
the same owned mount/control/host-namespace/trace variables as the preceding
metadata fixture. Trace only the owned `media/Faulty.mp4` with
`strace -f -yy -s 0 -P PATH -e trace=read,pread64,readv,sendfile,splice,close`.
The controller must resume the device before handler/descriptor cleanup.

`scripts/test-env/phase3-cli-supervisor-recovery.py --config ABSOLUTE_JSON`
drives an explicitly provisioned service through a fixed control executable.
Its private configuration and controller contract are documented in the script.
An optional 0600 `baseline_path` permits exact-state verification and complete
repetition on a retained failed fixture. Source/target databases for backup
regression must be independent disposable databases with non-administrative
owners; never point that fixture at the retained CLI database.

Remaining work includes cancellation while a scan is blocked in the filesystem,
isolated guest reboot/reset and late-mounted storage, and final integrated
regression with migration/backup/recovery composition. This increment does not
complete the historical two-tier 28-case matrix or establish a new strict SLO.
Continue with bounded functional cases under the relaxed performance agreement.
