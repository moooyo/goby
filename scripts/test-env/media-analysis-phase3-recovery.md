# Phase 3 owned-guest recovery controller

Status: **source delivery only**. No command in these files has been executed as
part of this delivery. No fault, capacity profile, reboot, forced reset, durable
state recovery, or resource closure is accepted by the presence of this source.
Pure test source is included; it has not been run locally. Run all verification
only in the designated remote environment after the complete Phase 3 source,
private bindings, explicit run release, and independent adapters are frozen.

The initial clone/bootstrap or machine-identity repair reboot does not count as
recovery acceptance. A service restart does not count as an OS reboot. A reset
of the owned guest is not a physical PVE host power-loss experiment.

## Source ownership and components

- `media-analysis-phase3-recovery.py` runs outside the tested guest. It admits
  finite manifests, owns an immutable fsynced event journal, sequences the fault
  case, validates independent facts, and reports single-case results as
  `partial`. It never executes caller-provided shell fragments or SQL.
- `media-analysis-phase3-recovery-guest.py` implements real, finite guest fault
  primitives and read-only Linux identity observations. It never contacts PVE.
- `media-analysis-phase3-recovery-transport.py` is an importable, pinned SSH
  transport with bounded streams and full external artifact copies. It supplies
  transport only; it cannot produce a passing acceptance result.
- `media-analysis-phase3-recovery-tests.py` contains pure parser, authorization,
  preservation, overlap and state-machine test source. It injects no faults.
- The separately owned state observer produces actual HTTP-write/DB-readback
  acknowledgements, immutable SQLite snapshots and exact comparison records.
  The separately owned HTTP probe produces healthy-root requests and playback
  resume/decode evidence. The oracle combines those observations with actual
  guest and PVE records. Missing evidence must remain pending or fail.

The network owner supplies the PVE adapter. Only that adapter may dispatch a
forced reset, only for the one released VM 106. Its context independently pins
the actual owner marker, SMBIOS UUID, disk identities, PVE machine identity and
per-run release. The controller can run in an owned private directory on PVE;
neither its location nor a guest root account authorizes host fault injection.
VM 101, shared `test-env`, VM 102, other guests and the physical PVE host are
outside every fault scope.

## External controller manifest

The exact top-level fields are `schema_version: 1`, `run_id`, `source_revision`
(40 lowercase hexadecimal characters), `profile_id`, `tier` (10000 or 100000),
`owner_id`, `controller`, `guest`, `adapters`, `workload_manifest`, `volumes`,
`healthy_roots`, `budgets`, `scenarios`, and `state_validator`. All private JSON rejects duplicate
keys, non-finite numbers and unexpected fields. Manifest admission validates
shape and local pins; it does not contact the guest or establish readiness.

`controller` is `{machine_id, artifacts_root, marker: {path, sha256}}`. Its root
is a canonical existing directory owned by the executing UID with mode 0700.
The marker is exactly `{schema_version:1, owner_id, role:
"external_recovery_controller", machine_id}`. The controller machine ID must
differ from the guest machine ID. Output directories must not already exist.

`guest` contains exactly:

```text
vmid: 106
name: a goby-phase3- name
machine_id: 32 lowercase hexadecimal characters
smbios_uuid: lowercase UUID
owner_marker_sha256: SHA-256 of the guest owner marker
owned_root: canonical /var/lib/goby-phase3/... or /opt/goby-phase3/...
disks: mapping of disk label to {volume, uuid, size_bytes}
goby_uid: non-root application UID
goby_cgroup: /system.slice/goby-phase3-....service
postgres_cgroup: /system.slice/goby-phase3-....service
```

Only `local-lvm:vm-106-disk-N` and `local-lvm:vm-106-cloudinit` volume names are
admitted. The PVE adapter must observe their real LVM identities; copying a
manifest's disk inventory into a guest response is only a scope reference.

`adapters` has `executor`, `observer`, `workload`, and `hypervisor`. Each is
`{path, sha256, context:{path,sha256}}`. Source and context remain pinned for
every call. Adapters run as `python3 -I -B SOURCE --context PRIVATE_JSON`.
The first three may point at the same independently reviewed oracle source.
`workload_manifest` is `{path,sha256}` and must match run, owner, source,
profile and tier. Credentials, addresses and raw database values stay private.

`state_validator` is `{source:{path,sha256},binding_sha256,scope}`. Its source is
the pinned local `media-analysis-phase3-state.py`; the binding digest is the
observer's canonical binding hash. Scope contains finite `user_ids`, `item_ids`,
`library_ids`, `root_ids`, and `fault_root_ids`. The controller independently
calls its pure external-artifact readers to derive actual state hashes, check
ACK postimages and compare external SQLite rows. An oracle's `passed:true`
cannot replace that comparison.

Each `volumes` entry has exactly `mountpoint`, `backing_file`, `loop_device`,
`mapper_name`, `dm_uuid`, `filesystem_uuid`, `major_minor`, `size_bytes`,
`writable`, and `purpose`. Paths must be descendants of the guest's owned root.
Loop nodes are explicit `/dev/loopN` paths; mapper names begin
`goby-phase3-`; DM UUIDs begin `GOBY-PHASE3-OWNER_ID-`. Distinct volumes cannot
alias loop nodes, mapper names or backing files. There are 2 through 8 volumes,
each 16 MiB through 2 GiB. Purposes are `media`, `derivatives`, or `replacement`.
Media used for suspension must already be mounted read-only. Full storage is
injected only into the declared writable derivative volume, never PGDATA or
the guest root filesystem. The frozen major/minor pair is reserved for explicit
recreation after reboot; collision fails instead of selecting another device.

`healthy_roots` is a finite nonempty list of actual root IDs. Keep healthy roots
on ordinary persistent guest storage when all temporary loop mappings will be
absent during a late-mount observation. A healthy root that silently depends on
the suspended/lost fault volume is not an independent control.

`budgets` contains `rpc_seconds` (1–120), `recovery_seconds` (10–900),
`fault_seconds` (5–120), `poll_seconds` (0.1–10), `max_output_bytes`
(4096–8388608), `healthy_latency_ms` (1–60000), and `cleanup_seconds` (5–120).
Freeze them with the measured idle baseline before accepting any profile.
The journal caps events at 2048 and an event at 16 MiB. Full SQLite artifacts
are streamed, capped at 1 GiB and independently rehashed and fsynced externally.

Each manifest admits exactly one scenario with a fresh release/context and
current process-lifetime pins. Earlier context files are retained unchanged;
reboot does not authorize a broad in-place PID refresh. The scenario has exactly:

```json
{
  "scenario_id": "blocked-read-01",
  "fault": "blocked_read",
  "volume_id": "fault-media",
  "replacement_volume_id": null,
  "relative_path": "nested",
  "late_mount": false,
  "restart_goby_after": false,
  "require_interrupted_jobs": false
}
```

Non-storage scenarios use `volume_id:null`, except a late-mount reboot/reset.
Only replacement scenarios supply `replacement_volume_id`. Nested replacements
require a nonempty descendant path. Absolute paths and `..` are rejected by
both the external controller and guest helper. Restart scenarios require an
actual admitted population of interruptible scan and analysis jobs.

## Fault mechanisms and evidence

| Fault | Actual primitive | Required independent observation |
| --- | --- | --- |
| `blocked_read` | Suspend an exclusively owned loop-backed DM mapping with `--noflush --nolockfs`. | Goby task in `D`, raw read syscall, correct cgroup/PID/TID/start identity and fault-volume FD correlation at two times; the real GET caller returns while that task remains alive. |
| `blocked_metadata` | Same real DM suspension after an explicitly released guest cache drop. | A metadata operation actually blocked in the kernel, separately from the read case. A real GET socket deadline or bounded GET 503 can establish caller return. POST 202 is only admission; cached successful stat is not blocked-operation evidence. |
| `mount_loss` | Lazy unmount only the exact owned fault mount. | Mount disappeared and affected access failed; catalog identities and all protected user state retained. Existing detached handles are recorded rather than assumed closed. |
| `changed_root_mount` | Bind a declared replacement volume over the owned root. | Different mount identity, replacement visible, product binding rejected as changed, unchanged approved revision and no implicit rebind. |
| `changed_nested_mount` | Bind the declared replacement over one frozen descendant. | Same replacement fencing, independently for the nested mount. |
| `permission_failure` | Save the exact original device/inode/mode, then chmod only a directory on the selected fault volume. | Actual `EACCES` under the application's non-root UID, not a root-user probe. |
| `enospc` | Create one exclusively owned filler file, write with a 60-second/volume-size bound until actual `ENOSPC`. | Product derivative write failed with real ENOSPC; healthy media remains readable. |
| `postgres_disconnect` | Derive the exact library-owner advisory lock key and terminate that one database backend with matching start timestamp. | Old backend absent, ownership/readiness loss observed, durable rows retained; recovery uses the predeclared Goby restart when needed. |
| `postgres_lock_wait` | A transient, owned systemd helper holds one frozen item's row lock; PostgreSQL and systemd separately bound it. | `pg_stat_activity`/`pg_blocking_pids` show the actual wait, product statement fails with SQLSTATE 57014, complete rollback and the same healthy owner backend survives. |
| `process_crash` | SIGKILL only the process matching current PID/start/cgroup/executable identity. | Goby lifetime changes while guest boot and PG lifetime remain unchanged. |
| `postgres_restart` | Restart only the declared owned PostgreSQL service. | PG lifetime changes; boot remains unchanged; interrupted catalog ownership is repaired explicitly. |
| `guest_reboot` | A five-second owned transient timer calls guest `systemctl reboot --no-block`. | A new guest boot ID plus independently observed process identities and external PVE observations. |
| `guest_reset` | Network owner's pinned adapter dispatches the exact VM 106 PVE reset once. | Durable external PVE intent/task receipt plus a new guest boot ID. It is an abrupt guest reset, not physical host power loss. |

The cache-drop option affects caches in the exclusively owned guest. It is
disabled unless explicitly present in the frozen binding and run release. No
host cache, shared guest cache, global loop detach, unmount-all, process-name
kill, arbitrary SQL or broad filesystem deletion is used.

Blocked-fault facts record `caller_timed_out`, `bounded_http_return`,
`http_method`, `http_status`, and `task_alive_after_caller_return`. Exactly one
of the first two flags must match actual GET evidence. A bounded response must
be the real 503. Both cases still require the live kernel task and later
syscall, FD and resource-release proofs.

Suspension recovery resumes the exact DM device before resource closure is
checked. A live Go OS thread may remain after a syscall returns; its mere
existence or disappearance cannot prove a goroutine completed. The oracle must
correlate the old syscall and retained evidence FD/mount/spool lifetimes and
require actual worker/evidence release. If those facts cannot be obtained, this
cell cannot pass. An HTTP deadline alone proves only a caller deadline.

After reboot, loop and DM mappings are recreated over the exact existing files
with the expected sizes, DM identities and filesystem UUIDs. Existing conflicting
mappings fail admission. No recovery path calls mkfs or creates replacement
backing data. The original mounts are explicitly restored only after the
late-mount observation. Original identity and protected user-state comparison
must still pass. After replacement fencing is observed, the controller settles
the workload, performs the separately frozen `rebind_replacement` mutation and
externally persists its actual ACK. It restores the original mount, performs
`rebind_original`, and persists a second ACK. Both postimage chains must name
the same root/library, and the second storage document must match the original.
Binding and library revisions advance explicitly; the original digest is not
reported as though those writes had not happened.

## Guest binding and release

The private guest binding has `schema_version`, `run_id`, `owner_id`, `guest`,
`owner_marker`, `owned_root`, `services`, `volumes`, `tools`, `postgres`, and
`allow_guest_cache_drop`, and `fixture_access:{actor_uid,media_read_gid}`.
Optional `spool_directory` is required when proving actual scan-spool closure.
`guest` contains `vmid`, `name`, `machine_id`,
`smbios_uuid`, `disks`. The marker is a pinned root-owned 0600 JSON file and
contains at least `owner_id`, `vmid:106`, `machine_id`, and `smbios_uuid`.
Prefer also `schema_version:1` and `run_id` for cross-component agreement.

The root-owned `owned_root` must not be group/other writable. Its fixed path
chain to each mount parent must have `media_read_gid` and group execute access.
Runtime preparation supplies that exact chain; the helper never chmods an
entire tree. Actor and Goby both need that group; actor is non-root and distinct
from Goby. Its precreated
`recovery-operations` directory is root-owned 0700. Private control files are
not placed in a fault mount. Services map `goby` and `postgres` to exactly
`{unit,unit_file:{path,sha256},uid,executable_sha256}`. Names must match
`goby-phase3-*.service` and run
under non-root UIDs. Every live service observation checks `/proc`, cgroup and
the actual executable. Goby must report empty effective capabilities and
`NoNewPrivs=1`. The loaded unit fragment must match its pinned file;
unexpected drop-ins are rejected. Tools are `{path,sha256}` references to canonical real
root-owned executables, not symlink aliases.

`postgres` contains `service_file:{path,sha256}`, `service_name`, `database`
(`goby_phase3_*`), `schema:"public"`, `data_directory` beneath the owned root,
one frozen `lock_item_id`, and `json_log:null` or
`json_log:{path,format:"postgresql-jsonlog-v17"}`. PostgreSQL service-file credentials remain
private. Each SQL action independently verifies `current_database()` and the
server's actual `data_directory`, plus the owned postgres service identity.
The helper supplies only fixed SQL; a request cannot provide a statement.
The lock-timeout scenario additionally requires actual PG 17 JSON logging with
`logging_collector=on`, `log_destination` including `jsonlog`, and
`log_error_verbosity=verbose`. Its frozen log path is outside fault volumes.
`observe_postgres_log` accepts only `{cursor:null}` to freeze the current tail,
or `{cursor:{device,inode,offset}}` to read at most 1 MiB of complete JSONL rows.
It returns raw base64 bytes, SHA-256, exact offsets, before/after file identity,
next cursor and actual server logging settings. Rotation or truncation fails;
the oracle must correlate real SQLSTATE 57014 and backend/session identity.
An HTTP 503 or a message containing the word timeout is insufficient.

Guest RPC CLI:

```text
python3 -I -B media-analysis-phase3-recovery-guest.py --binding /private/guest.json --binding-sha256 SHA --release /private/release.json --release-sha256 SHA
```

The release is separate from the binding, avoiding a circular hash dependency.
It contains exactly `schema_version:1`, `run_id`, `owner_id`, `vmid:106`,
`smbios_uuid`, `source_revision`, `binding_sha256`, `operations`, `scenarios`,
and `expires_unix_ns`. `scenarios` maps scenario IDs to SHA-256 of their entire
canonical JSON objects. Matching an ID alone never authorizes changing its
fault, selected volume or path. Read-only observations do not require release;
every mutation verifies it before saving an exclusive durable operation intent.

Guest request:

```text
{schema_version:1, request_id, op, run_id, owner_id, source_revision,
 binding_sha256, scenario, payload}
```

Operations are `observe`, `observe_postgres`, `observe_postgres_log`, `observe_failure_errno`, `prepare_volumes`,
`arm_late_mount`, `inject`, `recover`, and `close`. Mutation responses contain
`dispatches:1`, meaning that this RPC was dispatched once, not that a recovery
consists of only one internal command. Intents are O_EXCL files keyed by the
request ID. A repeated request is never replayed. Ambiguous or failed actions
retain their evidence and resources for independent observation.

`prepare_volumes` exclusively creates bounded backing files, explicit unused
loops, DM mappings and ext4 filesystems. It requires 2 GiB guest disk headroom
beyond each allocation, refuses every preexisting target, and returns
`corpus_populated:false` and `runtime_admitted:false`. Mount roots become
root:`media_read_gid` mode 0710. Media/replacement volumes get `fixture-media`
owned by actor:`media_read_gid` mode 0750; derivative volumes get
`analysis-cache` owned by Goby:`media_read_gid` mode 0700. Actual paths, device/
inode, filesystem/DM UUIDs and permissions are returned in `fixture_directories`;
`fixture_parent_chains` records the real traversable ancestors. Goby need not
be running: prepare the derivative volume before starting it with its final
cache configuration. Fixture preparation,
permissions, read-only media remount and complete source/binding refreezing
remain distinct. `close` does not delete a shared fixture or claim closure; the
independent oracle must confirm original modes/mounts and empty fault resources.

`observe` includes real per-thread syscall state, FD/mount/inode inventory, DM
suspension state, and scenario-specific target modes, filler existence and
lock-unit cgroup membership. Optional spool inventory admits only the frozen
Goby-owned 0700 directory outside fault volumes, rejects symlinks and foreign
ownership, and caps 64 generations, 4096 entries and five seconds. It does no
cleanup. `observe_failure_errno` requires release and a durable intent because
its ENOSPC branch creates one owned temporary probe. It drops to the actual
Goby UID/group set, verifies empty capabilities and no-new-privileges, performs
a real open/read or 32 KiB write/fsync, removes only its probe and reaps its
exact child. It cannot replace the separate product-write failure evidence.

## Oracle RPC and external durability

The external adapter request is:

```text
{schema_version:1, request_id, operation, run_id, owner_id, source_revision,
 tier, profile_id, guest, volumes, scenario, payload, artifacts_root,
 context_sha256}
```

The reply is `{schema_version:1,request_id,operation,owner_id,vmid:106,status,
data}`, with status `ok`, `pending`, or `failed`. The controller's
`validate_ack`, `validate_identity`, `validate_fault_observation`,
`validate_recovery`, and `healthy_probes` functions are the exact schema.
Adapters must derive their facts from source-pinned real observations and keep
the native evidence. A matching field shape does not establish provenance.

The sequence is: independent guest and hypervisor identity; actual mutations
and authoritative readback; complete external artifact copy; fsynced durable
ack event; optional late-mount arming; one injection; independent fault facts;
late-mount evidence when selected; one declared recovery; new independent
identities; `settle_scenario`; recovery comparison/client probe; `close_scenario`;
independent closure observation. A failed or unknown mutation stops this
sequence. Only read-only `pending` observations are polled, under a fixed
deadline. Observation failures are not automatic permission to rerun mutation.

Replacement scenarios settle before their first explicit rebind. The
`recovery_observation` adapter also performs one actual playback reconnect;
despite its name, it is dispatched once as a mutation and never automatically
retried after an observation timeout.

Storage and PostgreSQL lock-wait `fault_observation` calls include actual
healthy scan/preview POSTs or metadata PUTs. They are single mutation calls
under `fault_seconds` and require `dispatches:1`; an unknown result cannot be
polled as a new invocation. `late_mount_observation` has the same rule because
its healthy-root scan admits real work. Restart fault observation remains a
read-only bounded poll for boot/process identity. None of these distinctions
changes the separately recorded one-time fault injection.

Rebind proof fields are `dispatches:1`, `stage` (`replacement` or `original`),
`expected_state`, `native_ack`, `external_ack`, `before_snapshot`,
`after_snapshot`, `comparison`, and `observed_unix_ns`. `native_ack` is the
full actual external ACK JSON. Snapshot references contain `snapshot_id`,
`path`, `sha256`, `bytes` and optional `fsynced`; the ACK reference contains
`path`, `sha256`, `bytes`, `fsynced:true`. Independent external comparison must
show that only the declared root's binding fields and its library's revision
changed. Two fsynced `durable-rebind-state` journal references form the
recovery record's `rebind_chain`.

The recovery record's `state` describes restored durable state before the new
client reports playback. Mandatory `post_resume` has the same proof fields
except `dispatches` and `stage`. The controller derives its actual after-state
from external SQLite and verifies the sole `progress` ACK targets the same
initially acknowledged user/item. Only position, a bounded play-count increment
and last-played time may change. Original recovery and subsequent legitimate
client writes remain separate durable records.

Acknowledged protected state includes catalog identities/root bindings,
metadata, fixed sentinel-user state, persisted playback progress and settings.
Keep sentinel identities separate from continuing workload users/items so
legitimate later playback writes do not make comparison ambiguous. The state
observer returns real before/after row images and HTTP response hashes. The
controller keeps those private, copies complete SQLite/ack artifacts outside
the guest, and fsyncs them before injecting the fault. Hashes pointing only at
guest-local files are insufficient.

Each journal event is exactly:

```text
{schema_version:1, sequence, previous_sha256, kind, controller_unix_ns,
 controller_monotonic_ns, data}
```

Its canonical representation is sorted, compact ASCII JSON plus one LF.
`durable-acknowledged-state` data is `{ack,snapshot,before,hypervisor_before}`.
Both `ack.snapshot` and `data.snapshot` refer to the same externally durable
`{path,sha256,bytes,fsynced:true}` artifact. `ack.checkpoint` binds run/owner/
profile/source/tier and actual overlap windows. Intro and preview share one
worker slot: each must independently overlap scanning, search and playback;
the controller does not require two analysis workers to run simultaneously.

For PVE `forced_reset`, the controller journals the adapter PID/start/cgroup
before writing its request. It never signals that adapter group after an
observation timeout: the PVE operation may already be accepted. The network
adapter's durable intent and fixed receipt path own eventual disposition.
The controller reports failure/unknown closure and forbids another dispatch.
Independent read-only observation and closure are required before any later
operator action. Ordinary SSH-child termination similarly does not prove that
a remote mutation was cancelled or that a kernel-blocked task terminated.

## Commands and result scope

```text
python3 -I -B media-analysis-phase3-recovery.py --manifest /private/run/recovery.json --manifest-sha256 SHA --output /private/run/admission --admit-only
python3 -I -B media-analysis-phase3-recovery.py --manifest /private/run/recovery.json --manifest-sha256 SHA --output /private/run/attempt-01
python3 -I -B media-analysis-phase3-recovery-tests.py
```

Use distinct new output directories. Exit code 0 means admission, 1 means
failed, and 2 means a successful single case with `partial` matrix coverage.
This controller never publishes a full-matrix pass. The independent composer
must bind the original case manifests, releases and receipts for all thirteen
fault kinds and late mounts, retaining original failures. Each catalog tier has
its own frozen matrix and composition. Capacity, named consumer behavior, migration/backup regression and
physical power-loss durability are not inferred from this result.

Public reporting should project safe source/artifact hashes, scenario names,
distributions, failures, time/resource limits and actual closure status. Raw
acknowledgements, SQLite snapshots, service paths, owner IDs, network addresses,
private requests, process FD links and credentials remain outside the repository.
