# Phase 3 external recovery oracle

Status: source integration only. No tests, imports, builds, SSH, runtime probes,
faults, reboots, or resets were executed while authoring these files. Source
availability does not authorize execution.

`media-analysis-phase3-oracle.py --context <private-controller-context>` serves
as the recovery controller's executor, observer and workload adapter. The PVE
adapter remains independent. The controller also independently opens the
external SQLite/ACK originals through its pinned state validator; an oracle
comparison flag cannot replace actual preserved bytes.

`media-analysis-phase3-oracle-workload.py --binding <private-guest-binding>`
wraps the existing compound Actor. Its separately owned systemd unit retains
real HTTP/SQL samples, media helper CPU/FD intervals, and source identities.
The oracle recomputes actual scan/search/playback/analysis overlap from those
records. It does not claim completion of the separate capacity campaign.

## Exact controller context

Top-level fields: `schema_version:1,released:true,controller_machine_id,run_id,
owner_id,source_revision,profile_id,tier,contract,modules,transport,state_scope,
state_binding_canonical_sha256,workload_manifest,scenarios,budgets`.
Private files are current-owner mode 0600, single-link regular files. Directories
are mode 0700. All paths are absolute; the controller must be outside VM 106.

- `contract`: recovery manifest `guest,volumes,healthy_roots,budgets`; omit
  adapter references to avoid a self-hash cycle. Incoming declarations must
  exactly match these frozen guest and volume objects.
- `modules`: exactly `controller,transport,state,evidence,workload`, each
  `{path,sha256}`. Workload is the original Actor source; evidence is
  `media-analysis-phase3-oracle-evidence.py`.
- `transport`: the recovery transport's exact configuration, with all four
  native roles and their shared root-owned guest private directory.
- `state_scope`: `{user_ids,item_ids,library_ids,root_ids,fault_root_ids}`.
  The state binding hash is the state module's canonical complete private
  binding hash, not the raw file hash. The recovery manifest's
  `state_validator` pins this same source, binding hash, and scope.
- `workload_manifest`: actual external `{path,sha256}` reference.
- `budgets`: finite integer `operation_seconds,external_bytes,external_files,
  free_floor_bytes`, admitted against actual storage. Full materialized SQLite
  files, ACKs, raw records and partial failures count toward storage admission.
- `scenarios`: ID to `scenario,root_id,workload_binding,caller_timeout_seconds,
  ack_mutations,resume`. The complete scenario must equal the controller
  scenario. Workload binding is `{path,sha256}`; caller timeout is an integer
  1..30. ACK mutations map exactly `metadata,user_state,playback_progress,
  settings` to unique declared native IDs. Resume is
  `{user_id,item_id,item_id_ref,mutation_id}`, with a separate unused progress
  mutation ID. Replacement cases also declare
  `rebind_mutations:{replacement,original}`.

Fault cases use the native `managed_configuration` mutation for their settings
ACK. It changes only the managed custom server name through the real settings
CAS API, preserving the active analysis profile and its preview/feature
references. The `analysis_configuration` ACK mapping remains supported for
other declared callers; it is not the fault-case settings mutation.

## Guest wrapper and actual fixture inputs

Exact wrapper binding: `version:1,released:true,run_id,owner_id,source_revision,profile_id,tier,
scenario_id,guest,private_directory,actor_uid,actor_gid,actor_groups,unit,
output_directory,phase,actor,manifest,context,scan_configuration,systemd_run,systemctl,python,
self_sha256,unit_memory_bytes,unit_runtime_seconds,settle_seconds`.
Tool/file references are `{path,sha256}`. Guest identity includes actual VM
106 machine ID and SMBIOS UUID. Supplementary groups explicitly include the
approved media read group. The non-root Actor is outside Goby and PG cgroups.
Its context preserves the separate PG lifetime and bounded process observer.
The immutable wrapper binding SHA is passed as the Actor's request scope, so
another case cannot reuse completed analysis admissions from an earlier phase.
`manifest`, `context`, and `scan_configuration` are Actor-owned mode 0600
files. The scan-configuration reference is an exact-byte readable copy whose
SHA must equal `context.scan_evidence_sha256`. The context retains the actual
deployed `scan_evidence_path` for independent broker verification; the copy
does not replace that deployment identity or widen its permissions.

Setup consumes actual `fault-fixture-private.json`, runtime service/DB receipts
and native prepared-volume identities. Root records provide actual library,
root, item and source IDs, source bytes/hash/identity and media clocks.
`state_refs` selects the independent sentinel user and metadata/progress/lock
items. Declared mutation IDs are not ACK evidence. No future PID, fingerprint,
item ID or database identifier may be invented.

Probe `artifacts_directory` must equal the shared state private directory.
The successful resume response is written before adding its self-reference;
the actual original bytes are exported and later consumed by state.

Each scenario has an independent controller release/context. Before starting
the next case, runtime export reads actual fixed-unit/executable/PG identities
and pins fresh Actor context/owner marker/bindings. It never bypasses
`Actor.assert_owned` with an old PID after restart. Single-case controller
reports retain their partial status and exit 2. The outer runner composes the
frozen 13 fault kinds plus late mount for both tiers; any absent case leaves the
matrix incomplete. The wrapper grants no general context-refresh privilege.

Fault wrappers accept only `phase:"cached"`. For each tier, the complete
independent compound journey still runs cold, cached, and incremental phases
before fault-fixture population, overload, and the cached fault cases. The
partially seeded catalog is never reported as the final tier workload.

## Evidence and lifetime rules

1. Recompute productive overlap and finish Actor settings/metadata edits before
   the four sentinel writes. Export and fsync real ACKs and SQLite bytes outside
   the guest. Recheck every final ACK postimage against the durable checkpoint.
2. Fault mutations have durable once-only intents. Unknown disposition retains
   artifacts and fails without automatic retry or recovery. Healthy roots use
   actual search, decoded source-bound media and native scan admissions.
3. Blocked I/O requires sustained owned D state, actual syscall ABI, FD, device,
   mount, inode and source path after a completed GET boundary. Socket timeout
   and bounded HTTP 503 are distinct. POST 202 is only admission. An expected
   source identity is never an observed source descriptor.
4. PG lock proof joins actual owner wait/blockers, bounded JSON log bytes with
   SQLSTATE 57014 and matching query hash, the same surviving idle owner, and
   unchanged durable rows. HTTP failure does not invent SQLSTATE or rollback.
   ENOSPC also needs a real failed preview task/child plus independent
   unprivileged errno evidence; the errno probe is not the product failure.
5. Replacement cases prove rejection, settle new admissions, then observe and
   explicitly CAS rebind to actual replacement identity. Restore original
   storage and explicitly rebind again. Both restricted ACKs and each SQLite
   pre/postimage remain external; no predicted fingerprint is accepted.
6. Settle actual Actor clients/jobs/encoders without undoing acknowledged
   metadata/settings. Restart termination is distinct from a successful join.
   Old job transitions, source/FD lifetimes and resource counters establish
   product cleanup.
7. Compare restored durable state before playback. Resume at actual persisted
   ticks using current PlaybackInfo, a checked original-stream URL, downloaded
   source SHA, actual ffprobe clocks, decoded frame hashes and four joined
   helper receipts. Export the original response and acknowledge only lawful
   progress/PlayCount/LastPlayedAt changes in a new actual snapshot. An unchanged
   already-counted session is a real readback-only ACK, not an invented write.
8. Closure observes workload unit termination, no active product jobs/media
   children, no temporary cache/spool generations, released native reservations,
   restored mount/mode identity, no owned filler and no owned PG blocker.
   Ready cache payloads and approved persistent spool control files are not
   temporary leaks.

Original streams require the same lease's actual leave record. Metadata uses
stopped admissions plus actual all-worker storage/scan quiescence, the original
request, FD and syscall exit; an unrelated scan completion cannot stand in for
the GET. Thread disappearance alone is insufficient. A pre-injection root
anchor is exempt only if its full identity persists in both blocked samples
and the final observation. The blocked syscall's own FD is never exempt.
Historical completion-ring drops do not invalidate a still-retained target;
a missing/overwritten target or incomplete current projection fails.

`media-analysis-phase3-oracle-tests.py` contains pure negative evidence tests
for future authorized remote execution. It has not been run. Actual fixture
bytes, module pins, per-case fresh identities, resource admission and the
independent PVE adapter must be bound by the complete release before execution.
