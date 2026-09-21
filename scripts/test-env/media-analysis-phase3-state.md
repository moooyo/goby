# Phase 3 authoritative state observer

`media-analysis-phase3-state.py` is source for a Linux guest-side observer, not
an acceptance result. It has not been executed, compiled, or tested as part of
its source-only delivery. Consolidated remote verification must exercise it
against the frozen candidate, PostgreSQL 17/schema 50, and the owned VM 106.
Importing the module performs no I/O. Running it performs only the requested
production HTTP mutations, fixed read-only SQL, bounded filesystem/process
observations, and private evidence writes. It contains no reboot, mount, service,
shell-command, SQL-write, or test-environment provisioning operation.

The fault controller owns the SSH transport and external durability journal.
The compound workload owns healthy-root search/playback/scan service checks,
real media delivery, post-recovery resume playback, sorting concurrency, and
runtime performance. This observer supplies independent acknowledged state and
recovery facts; a successful state comparison does not replace those checks.

## Private binding

The binding is a pre-provisioned, owned, regular, non-symlink `0600` JSON file.
All runtime paths, credentials, mutation values, and raw observations are private.
The private evidence directory must already exist with mode `0700`; every ancestor
must be a real directory. It must be below the owned root, on storage outside the
injected-full/inaccessible media volume. The helper does not create an environment.

Required fields are:

| Field | Contract |
| --- | --- |
| `version` | Integer `1`. |
| `run_id`, `owner_id`, `machine_id`, `smbios_uuid` | Frozen run and guest ownership identities. Actual `/etc/machine-id` and DMI product UUID must match. |
| `owned_directory`, `private_directory` | Absolute owned root and private evidence child directory. |
| `owner_marker` | `{path,sha256}`; the marker is below the owned root, regular `0600`, and has the frozen SHA-256. Its object must contain matching `schema_version:1`, `owner_id`, `vmid:106`, `smbios_uuid`, `machine_id`, and `run_id`. Additional provisioning fields are permitted. |
| `database` | `{psql,psql_sha256,host,port,name,oid,role,role_oid,system_identifier,passfile}`. `psql` is `/usr/lib/postgresql/17/bin/psql`; host is loopback; port is a nonprivileged integer. Database and role names match `goby_phase3_[a-z0-9_]{1,48}`. OIDs/system identifier are exact decimal strings. Passfile is private `0600` below the owned root. |
| `http` | `{origin,server_id,admin_cookie,emby_token,emby_device_id}`. Origin is plain HTTP on an explicitly ported loopback endpoint. No redirect is followed. The native cookie and Emby user token must be valid pre-created credentials for the owned instance. |
| `scope` | Nonempty unique arrays `user_ids` (maximum 128), `item_ids` (256), `library_ids` (128), `root_ids` (128), plus nonempty `fault_root_ids`, a subset of `root_ids`. Use dedicated sentinel users/items, separate from compound workload writers. |
| `application` | `{cgroup,executable_sha256}`. Dedicated application cgroup below `/sys/fs/cgroup`; expected Goby executable SHA-256. |
| `analysis_cache` | `{path,owner}`. Actual analysis-cache child of the owned root and actual `.goby-analysis-cache` owner. |
| `mutations` | At most 1024 named operations from the finite families below, including all target IDs and intended values. No arbitrary route, command, SQL, or expected-pass field is accepted. |
| `rebind_roots` | Optional map from declared fault root ID to `{library_id,target_path,original,replacement}`. Each stage has exact `{dm_uuid,major_minor,filesystem_uuid,backing_file}` from owned fault-volume preparation. The fixed target and backing files remain below the owned root. |

The PostgreSQL observer role needs read access to the fixed application tables
and `pg_control_system()`; this is not authority to execute manifest SQL. Each
query uses a sanitized environment, `psql -X --no-password`, a private passfile,
read-only defaults, a repeatable-read transaction, UTC, 90-second statement and
two-second lock timeouts. Both ends of the transaction independently check the
database/role names and OIDs, PostgreSQL system identifier, PostgreSQL major 17,
and persisted `server_id`. It also requires `fsync`, `synchronous_commit`, and
`full_page_writes` to be `on`. These settings are prerequisites, not evidence of
an actual physical power-loss test. Schema migration versions must be exactly
the contiguous sequence 1 through 50.

The runtime requires Python 3.11 or later and its standard library, including
SQLite, and the bound PostgreSQL 17 client. It intentionally does not depend on
an import-time database driver or invoke a caller-specified executable.
Every production HTTP identity check also binds the actual listening socket to
the sole expected Goby executable lifetime in the dedicated application cgroup.
The observer needs sufficient guest privileges for those `/proc` observations.

## RPC and evidence transport

The controller launches the installed helper with a fixed, privately bound SSH
command ending in `--binding /owned/private/state-binding.json`. It sends one
UTF-8 JSON document on stdin and closes stdin. Every request contains
`{"version":1,"request_id":"safe-id","op":"..."}`. Unknown request fields are
rejected. Only one observer may run at a time; a concurrent call fails with
`observer_busy` rather than racing mutations or snapshots. The fixed invocation
deadline is 240 seconds. Stdin is capped at 64 KiB and an HTTP response/SQL row
at 8 MiB. Exceeding a bound is a failure, never an omitted tail or a pass.

| `op` | Additional request fields | Result |
| --- | --- | --- |
| `snapshot` | `snapshot_id` | `snapshot_id`, run/binding identity hash, complete table counts/canonical hashes, actual runtime observations, summary `sha256`, and `private_artifact:{path,bytes,sha256}` for the complete SQLite evidence file. |
| `snapshot_database` | `snapshot_id` | The same complete SQL snapshot with `runtime:null`; it does not issue binding/storage HTTP observations during an injected storage stall. |
| `mutate` | `mutation_id` | Production mutation, HTTP readback and authoritative DB readback; the private acknowledgement described below. |
| `observe_rebind` | `observation_id`, `stage`, `root_id` | Actual binding GET and independent identity of the declared target volume; a new private `.rebind.json` artifact. No caller fingerprint or revision is accepted. |
| `rebind` | `mutation_id`, `observation:{path,bytes,sha256}` | Recheck the actual frozen observation, volume, revision and fingerprint, issue native PUT, and return a DB/HTTP verified ACK with complete runtime before/after snapshots. |
| `ack_probe_progress` | `mutation_id`, `before_snapshot_id`, `after_snapshot_id`, `probe_receipt:{path,bytes,sha256}` | Acknowledge only a declared progress operation after the actual probe's Playing/media/Stopped flow. Verify the SQLite pre/postimages, real counted stopped session and current HTTP UserData. |
| `readiness` | None | Actual `/readyz` status, root binding observations, process lifetimes, cache inventory. It has no DB snapshot and explicitly sets cache `references_observed:false`; it cannot prove persistence. |
| `compare` | `before_id`, `after_id`, ordered `ack_ids`, `expectation`, `artifact_sha256` | Derived comparison findings, counts, safe differences and observed task transitions. No supplied `passed` value is accepted. |

`artifact_sha256` must contain exactly the two keys `<before_id>.sqlite` and
`<after_id>.sqlite`, plus `<ack_id>.ack.json` for every acknowledgement. Values
are the hashes retained by the external controller. The helper checks those
file hashes before opening the snapshots or acknowledgement records. A hash
computed only after the fault is not a baseline or an external durability proof.

All RPC responses are **private transport**, including file paths and
acknowledgement postimages. The controller must not publish them wholesale.
Success uses `{version:1,request_id,ok:true,result:{...}}`; failure uses
`{version:1,request_id,ok:false,error:"fixed_safe_code"}` with exit status 1.
Raw exception text, SQL errors, HTTP bodies, paths, and credentials never appear
in a failure response.

For a successful mutation, the result contains:

```json
{
  "mutation_id": "progress-01",
  "kind": "progress",
  "record_sha256": "SHA256_OF_EXACT_ACK_FILE",
  "before_snapshot": "progress-01.before",
  "after_snapshot": "progress-01.after",
  "postimage_sha256": "SHA256_OF_POSTIMAGES",
  "database_verified": true,
  "http_readback_verified": true,
  "external_fsync_required": true,
  "private_ack": {
    "version": 1,
    "run_id": "frozen-run",
    "mutation_id": "progress-01",
    "kind": "progress",
    "binding_sha256": "SHA256_OF_BINDING",
    "postimages": [{"table": "user_item_data", "key": ["USER_ID", "ITEM_ID"], "before_row": {}, "row": {}, "expected_fields": {}}],
    "completed_monotonic_ns": "EXACT_DECIMAL_STRING",
    "completed_unix_ns": "ACTUAL_WALL_CLOCK_DECIMAL_STRING",
    "http_responses": [{"method": "POST", "status": 204, "route_sha256": "HASH", "body_sha256": "HASH"}]
  },
  "private_artifact": {"path": "/owned/private/progress-01.ack.json", "bytes": 1, "sha256": "HASH"},
  "private_snapshots": [{"path": "/owned/private/progress-01.before.sqlite", "bytes": 1, "sha256": "HASH"}]
}
```

The example is a shape illustration, not valid evidence. The real full ACK file
also retains private HTTP records and exact before/after images. File creation
is exclusive, mode `0600`; both file contents and the containing directory are
fsynced. Snapshot SQLite commits use `synchronous=FULL`; the closed file is
fsynced and exported with its exact file hash/size. This is guest-local evidence
until the controller has durably preserved it elsewhere.

Each snapshot summary includes `transaction` from both identity queries in the
same actual SQL transaction: `isolation`, `read_only`, `database_identity`,
`synchronous_commit`, `fsync`, and `full_page_writes`. A later PostgreSQL
observation cannot substitute for those snapshot-transaction facts. Runtime
observations also retain the native resource DTO. Cache observations include
actual temporary/trash allocated bytes, unique inodes and entries; ready entries
remain separate and are not mislabeled temporary output.

Replacement and restored-original rebinds require separate declared mutations
and separately exported observations. The helper opens the fixed target and
uses its actual `fdinfo` mount ID to identify the visible covering mount. Stacked
bind mounts and restoring a nested mount to an ordinary directory on the
original filesystem are both representable. It checks device-mapper UUID,
loop backing path, actual target device and ext4 UUID before and after the binding
write. Its bounded raw-device read uses the documented ext4
[superblock fields](https://github.com/torvalds/linux/blob/v6.12/Documentation/filesystems/ext4/super.rst).
The actual HTTP fingerprint and CAS revision are observed facts; neither is
predicted in the manifest.

The resume probe stores its original successful RPC response in the same private
directory, excluding its self-referential artifact descriptor. The before SQLite
file hash must equal the probe's `snapshot_sha256`. A reused already-counted
PlaySession can legitimately leave durable UserData unchanged; its ACK then
has `readback_only:true`, with the actual successful HTTP and stopped-session
proof retained. This does not invent a play-count increment or permit unrelated
state changes. The external ACK inspector independently checks the retained
SQLite session and UserData images as well as the HTTP receipt fields.

Before any fault, the external controller must:

1. Receive and validate the helper result, its run/request/ownership binding,
   and the actual mutation family. Reject any missing database/HTTP verification.
2. Copy each named baseline SQLite and ACK artifact using an independently
   bounded transfer restricted to the owned private root, verifying its exact
   size and SHA-256. Write the copies with `0600` outside VM 106.
3. Append the complete private ACK/postimages and artifact descriptors to its
   append-only external journal, preserving request order. Flush and fsync the
   journal and its directory. Record the external durable sequence/hash.
4. Only then permit the fault. Preserve the external copies through reboot/reset.
   Recopy lost guest evidence from those verified copies before `compare`, and
   send the original externally retained hashes in `artifact_sha256`.

A guest reply, a guest-local fsync, an HTTP 200/204, a changed boot ID, and a QEMU
reset are each insufficient on their own to prove physical power-loss durability.
The controller must name the actual fault mechanism and retain its external
timing and outcome. A reset is reported as forced guest reset.

## External read-only library API

The pinned module additionally exposes these functions for the controller's
independent oracle. They read only already exported private files. They never
load the guest binding file, call SSH/HTTP, read guest `/proc`, create hardlinks,
rewrite evidence, or require credential-bearing configuration.

```python
compare_external(before_ref, after_ref, ack_refs, binding_sha256, scope, expectation)
inspect_external(snapshot_ref, binding_sha256, scope)
inspect_ack_external(ack_ref, binding_sha256, before_ref, after_ref, scope)
```

A snapshot reference has `snapshot_id`, controller-local absolute `path`,
`bytes`, and externally retained `sha256`. An ACK reference substitutes
`mutation_id` for `snapshot_id`. Export filenames may have hash prefixes; the
functions map logical identifiers to the verified original files without
creating aliases. Files must be regular `0600`, single-link, and owned by the
controller account. The frozen `scope` is the same secret-free scope described
above; `binding_sha256` is the original canonical guest binding hash.

Every external snapshot open verifies the retained whole-file hash/size, the
summary hash and binding, and **recomputes all table counts and canonical
digests from the actual SQLite rows**. The canonical hash order is
`SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key`.
For each row, append `canonical([row_key, decode(payload)])` followed by LF to
SHA-256. `row_key` is the ASCII canonical JSON primary-key array **as a string**.
This fixed byte ordering is independent of PostgreSQL text collation. Primary
keys and per-table bounds are rechecked while streaming. SQLite opens use
read-only immutable mode, so adjacent unverified WAL files are not consumed.

`inspect_external` returns verified `summary`, `controller_state`, actual
`active_jobs`, `runtime_observations`, and `private_progress_rows`. The five
controller hash names and memberships are source-owned:

| Hash | Observed data |
| --- | --- |
| `catalog_identity_sha256` | All `catalog_identity` rows. |
| `root_binding_sha256` | Frozen roots' `library_roots` rows. |
| `user_state_sha256` | Sentinel `users`, `user_item_data`, `display_preferences`, `metadata_admin`, `item_intro_state`, and `analysis_intro_decisions`. |
| `playback_progress_sha256` | Sentinel user/item IDs, exact ticks, play count, played/last-played/hide-from-resume and remembered source/stream fields. |
| `settings_sha256` | `managed_settings` and `analysis_settings`. |

Each multi-table group hashes its fixed table-name to `{count,sha256}` mapping.
The progress projection hashes the canonical per-row projected records directly.
`catalog_count` is the recomputed complete `catalog_identity` count. These
hashes are comparison facts; the oracle must still apply legitimate ACK chains
and interrupted-scan rules instead of demanding that every hash stay equal.

`inspect_ack_external` validates actual before/after SQLite rows against the
ACK images and expected fields, rejects collateral changes again, and requires
successful recorded production write/read responses. It returns actual
HTTP response hashes, mutation/postimage proof hashes and the helper-recorded
`completed_unix_ns`, `completed_monotonic_ns`, and per-HTTP
`started_unix_ns`/`completed_unix_ns`. It does not invent a wall-clock timestamp
or accept a caller-supplied verification Boolean. Wall clocks can step and are
reported observations; the external journal sequence establishes ACK order.

## Finite mutation families

Each mutation has unique `id` and `kind`, plus only the relevant fields below.
All supplied target IDs must belong to the frozen scope. Native revisions are
read fresh and sent as exact decimal strings. Progress accepts an exact positive
decimal string below `2^63`, then serializes the production API's required JSON
integer losslessly. JSON integer parsing never uses binary floating point;
noninteger database JSON is parsed with `Decimal` and encoded without rounding
or numeric/object type collisions.

| Kind | Binding fields | Production operation and readback |
| --- | --- | --- |
| `user_name` | `user_id`, ASCII `name` of 2–128 characters | GET/PUT/GET `/admin/v1/users/{id}` with complete unchanged policy/role fields and CAS revision; verify `users.name`, normalized name, exactly one management revision increment, and the production policy merge. |
| `favorite` | `user_id`, `item_id`, Boolean `value` | POST or DELETE `/emby/Users/{id}/FavoriteItems/{item}`; GET item `UserData`; verify the exact `user_item_data` row. |
| `progress` | `user_id`, `item_id`, `media_source_id`, `position_ticks` decimal string | POST item `PlaybackInfo`, then `Sessions/Playing` and `Sessions/Playing/Progress`; GET item `UserData`; verify exact persisted ticks, expected play-count behavior and durable last-played ordering. The position must actually change. |
| `preferences` | `user_id`, `configuration` patch | GET/PUT/GET native preferences; fixed language/subtitle/next-episode/remember-stream/rewind/hide-played/intro preference allowlist. Verify only fields production actually persists and exactly one revision increment. |
| `display_preferences` | `user_id`, `preferences_id`, `client`, `preferences` patch | Emby DisplayPreferences GET/POST/GET with CAS; only `SortBy`, `SortOrder`, `CustomPrefs`; verify exact preferences and revision. |
| `metadata` | `item_id`, `overrides` patch | Native metadata GET/PUT/GET CAS, preserving existing locks. Only `Name`, `SortName`, `Overview`, `Tags`, `Genres`; verify effective HTTP values and durable overrides/editor facts. |
| `analysis_configuration` | `profile` patch | Native analysis overview GET, configuration PUT with complete profile/CAS, then GET. Verify every analysis profile field and exactly one revision increment in `analysis_settings`. |
| `root_rebind` | `library_id`, `root_id`, `stage:"replacement"|"original"`, `acknowledge_missing_removal:true` | Requires `observe_rebind` then `rebind`, never generic `mutate`. Recheck the exported actual fingerprint/CAS revision and declared volume, then verify native binding and library-revision postimages. |
| `managed_configuration` | `server_name` | Read the actual managed configuration, preserve every other override, CAS-update the custom server name and verify HTTP plus the exact managed-settings row/revision. |

Fault cases use `managed_configuration` for the server-settings ACK while media
analysis is active. Updating the analysis profile intentionally invalidates its
derived references and changes in-flight analysis authority; that separate
configuration operation remains supported but is not the fault-case sentinel.

An ACK is issued only after all readbacks agree and the intended state changed.
It checks non-authorized fields of the target row against their before-image;
a successful favorite cannot silently bless concurrent progress loss, and a
name change cannot bless unrelated configuration loss. Known production side
effects have explicit checks. A failed/no-op request has no durable ACK, and
partial private files retain the failure rather than authorizing a retry under
the same ID. Failed mutation attempts require a separately named run/operation.

## Snapshots and recovery comparisons

Fixed SQL emits one JSON object per row, in fixed key order, in one read-only
repeatable-read transaction. It never uses unbounded `json_agg`/`jsonb_agg`.
Each table reads at most its published bound plus one; the extra row fails the
snapshot. Counts are authoritative counts of the complete observed table,
computed while streaming. A 2 GiB output bound and 8 MiB per-row bound fail
explicitly. SQLite stores exact canonical rows keyed by table and primary key;
comparison merges ordered cursors instead of loading the entire catalog.

The source `TABLES` constant is the exact table/column/limit inventory. It covers
schema history, users, libraries/roots, catalog items and stable item identity,
user state, preferences, administrator metadata, general/analysis settings,
tasks/scans and immutable analysis admission facts, detection/preview/cache
state, and separately classified sessions. `catalog_identity` includes item ID,
library/root/parent/type and relative path, so a large count alone never proves
identity preservation. The catalog limit is 200,000 rows, sufficient for both
required 10,000 and 100,000 tiers with explicit headroom; admission must still
record actual media counts independently.

`users`, preferences, user-item state and administrator metadata compare the
fixed sentinel scope. Full-table digests/counts remain available privately, so
compound clients can use separate users without being mislabeled as corruption.
Every catalog identity is compared. Existing IDs cannot disappear or change.
During `interrupted`, new catalog rows are allowed only in libraries with an
actually observed pre-fault active scan, and their count is reported explicitly.
This exception never permits deletion or identity replacement.

Later legitimate writes require the actual ordered ACKs. Before/after images
must form a contiguous per-row chain, and the baseline must match a point on
that chain. The latest acknowledged image is the expected result. Old or
reordered ACKs cannot overwrite a newer baseline to legitimize rollback.

Sessions, access times and play-session lifetimes are observed separately from
durable user state. `updated_at`, last scan time and cache last-use timestamps
are excluded only from the documented relevant durable projections. Durable
last-played, metadata editor, configuration revisions and root-binding facts
remain covered. Automatically derived item/source fields are reported separately
from administrator overrides; the compound acceptance must check its own
sorting/source-work expectations.

`expectation` is one of:

- `preserve`: ready service, preserved catalog/user/config state, same verified
  roots, no unsafe/missing referenced derivatives.
- `interrupted`: the above, no surviving pre-fault active task, at least one
  actually observed interrupted transition, no surviving pre-fault media-worker
  lifetime on the same boot, and no detached worker or temporary/trash remainder
  at the settled observation point. A completed racing task is recorded as such
  and is not counted as an interrupted task.
- `replacement-unbound`: affected roots must be mismatch/unavailable with no
  binding revision change; healthy roots stay verified. Catalog deletion remains
  forbidden even when storage is inaccessible.
- `restored-original`: original approved fingerprint/revision and verified
  status must return, while item IDs and durable user state remain preserved.
- `explicit-rebind`: each affected root requires an actual rebind ACK and must
  remain verified with matching approved/observed fingerprint. The comparison
  itself never grants missing-item removal.

Active-task admission/linkage fields and all previously present analysis
run-profile/work/source snapshots remain immutable. Only explicit execution
state/counter/time/error fields may transition. Already terminal rows must stay
unchanged. New tasks are listed by their actual IDs/state in the after snapshot;
their existence does not excuse an old orphan. Media PID observations include
PID/start ticks, executable hash, parent chain and application ancestry.

Cache inspection binds the actual cache owner, validates owner/key records,
manifest seal, DB preview seal, bounded artifact inventory, actual file identity,
size and SHA-256. The fixed 8 GiB hash budget fails explicitly if exceeded. It
separately counts temporary, trash, ready, unreferenced ready, missing referenced,
and unsafe entries. Unreferenced sealed entries may be valid retained cache;
their presence is reported and does not silently imply corruption or cleanup.
Take settled comparison snapshots after the controller has independently proved
worker termination and stopped admitting new background work; a request timeout
does not establish worker termination.

Public reports must project only safe operation IDs, hashes, counts, statuses,
latencies and explicitly classified failures. Keep credentials, filesystem
paths, full RPC responses, SQLite files, cache names and raw HTTP/SQL state in
private `0600` storage. No local or remote verification, environment mutation,
media generation, commit or publication is implied by this source delivery.
