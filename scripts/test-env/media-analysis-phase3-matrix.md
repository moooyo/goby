# Phase 3 external fault-matrix composer

Status: **source delivery; execution and actual matrix acceptance pending**.
The composer and its contract tests have not been run as part of this source
delivery. A test fixture is not a fault result. Run verification only on the
authorized remote environment. Actual composition runs on the external Linux
controller that retains the original case artifacts, outside VM 106.

The CLI reads evidence and writes one new composition result. It never opens an
SSH connection, starts a service, invokes an adapter, injects a fault, reboots a
guest, or retries a mutation. It rehashes and fsyncs retained external artifacts
through the existing controller readers. Original case files remain unchanged.

## Required coverage and result scope

The input must contain exactly the 10,000 and 100,000 catalog tiers. Each tier
has one frozen `profile_id` and exactly fourteen independently bound cases:

- One ordinary case for each of `blocked_read`, `blocked_metadata`,
  `mount_loss`, `changed_root_mount`, `changed_nested_mount`,
  `permission_failure`, `enospc`, `postgres_disconnect`, `postgres_lock_wait`,
  `process_crash`, `postgres_restart`, `guest_reboot`, and `guest_reset`.
- A separate `guest_reboot` with `late_mount:true`. It cannot replace the
  ordinary reboot or reset case.

Each ordinary case has `late_mount:false`. Both tiers must be complete before
the composer can return exit code 0 and `complete_fault_matrix:true`. A missing
case, admission-only journal, failed/unknown mutation, reused receipt, changed
scope, or missing native artifact fails closed with exit code 1. There is no
option to skip a tier, reduce coverage, trust a pass flag, or accept one case as
the complete matrix.

The existing recovery controller deliberately produces `status:partial`, exit
code 2, and `matrix_composition_required:true` after one successful case. The
composer requires this exact original result and keeps it unchanged. The new
composition result alone records full coverage.

The matrix concerns the owned VM 106 fault cases. It does not establish physical
host power loss, capacity/overload acceptance, named-consumer coverage, final
regression, publication, or closure of all external controller descendants.
Those remain independent acceptance requirements.

## CLI

```text
python3 -I -B media-analysis-phase3-matrix.py --manifest /private/matrix.json --manifest-sha256 SHA256 --output /private/matrix-result-01.json
python3 -I -B test-media-analysis-phase3-matrix.py
```

The output must not exist. Successful output is created exclusively with mode
0600 and fsynced with its parent directory. Failure prints a bounded reason to
stdout and does not create a passing result. Retain that failure output with the
other original attempts. Do not overwrite an earlier composition or case.

All references below are `{path,sha256}` objects with canonical absolute Linux
paths and a lowercase SHA-256 of the original bytes. Private artifacts must be
regular files owned by the executing UID, not symlinks or hard links, without
group/other permissions. A digest is a binding to retained bytes; it is not an
execution receipt by itself.

## Composition manifest

The top-level fields are exactly:

| Field | Contract |
| --- | --- |
| `schema_version` | `1` |
| `matrix_id` | A finite identifier for this new composition |
| `source_revision` | One exact 40-character lowercase product revision |
| `owner_id` | The exact owner shared by every case |
| `guest` | `{vmid:106,name,machine_id,smbios_uuid,disks}`; disk entries are the existing `{volume,uuid,size_bytes}` records |
| `modules` | References for `controller`, `state`, `oracle`, `evidence`, and `workload` |
| `tiers` | Exactly two objects, each `{tier,profile_id,cases}` |
| `retained_failures` | References to original failed-attempt records; use an empty list only when there are none in the declared campaign |

The modules are the existing sibling files `media-analysis-phase3-recovery.py`,
`media-analysis-phase3-state.py`, `media-analysis-phase3-oracle.py`,
`media-analysis-phase3-oracle-evidence.py`, and
`media-analysis-phase3-workload.py`. The paths must name these installed sibling
sources. Their hashes must match the sources pinned by every original case.
No manifest-provided arbitrary verifier is executed. Pinned transport and PVE
adapter sources are rehashed but never imported or dispatched.

Each case has exactly these fields:

```text
scenario_id, run_id, fault, late_mount, manifest,
journal: {directory, terminal: {path,sha256}},
oracle_state: {path,sha256},
inputs: [{original: {path,sha256}, retained: {path,sha256}}],
reset_evidence: null | {intent: {path,sha256}, receipt: {path,sha256}}
```

`manifest` is the original single-case recovery manifest. `journal.terminal`
pins its actual final `NNNN-run-result.json`. The directory must contain the
complete original journal and no unrelated files. `oracle_state` pins the final
original `oracle-state-<scenario SHA prefix>.json` under that case's external
artifacts root. Freeze this pin after the case is terminal; its mutable filename
alone is not evidence.

Existing exporters can share `run_id` across a tier. The composer allows this
and binds each execution with its scenario, exact manifest, and independent
artifact root. The private exporter derives the workload unit from both run and
case ID and places native artifacts under case-specific directories. Request
strings such as `run:1` may therefore recur in separate namespaces. Reusing a
case, artifact root, terminal journal pin, oracle-state pin, or native-record
path for another matrix cell is rejected.

`inputs` retains exact private guest bindings and releases outside the guest.
At minimum it must contain the original transport references for the guest
binding and release, state binding, probe binding, and workload binding. The
`original` path remains the exact guest path referenced by the frozen context;
`retained` names its externally preserved copy. Both SHA values must match.
Never rewrite a binding's guest paths to make a copy appear original.

The guest release is checked against its original binding, full scenario hash,
run, owner, source, VM identity, authorized operations, controller mutation
intent times, and actual injection time. Expired-at-dispatch releases fail; an
audit performed after a correctly released run is allowed. The state binding
hash uses the state observer's canonicalization contract.

Only `guest_reset` supplies `reset_evidence`. These are the actual private PVE
adapter's `reset-intent.json` and `reset-receipt.json`, using the existing
`goby-phase3-pve106-reset-intent-v1` and
`goby-phase3-pve106-reset-receipt-v1` schemas. The composer binds their original
request/context/ACK, one dispatch, VM identity and UPID, reads the retained
`pvesh` stdout/stderr bytes, and requires a stopped task with exit status `OK`.
A generic reset boolean or a successful reboot cannot replace these artifacts.

## Evidence checks

The composer first verifies the canonical journal hash chain, continuous
sequence numbers, event filenames, monotonic order, and terminal pin. It then
replays the existing controller's state machine with a receipt-only RPC reader.
Every original intent, adapter process owner, receipt, payload, durable ACK,
rebind stage, resume proof and closure transition must be consumed in order.
Only the newly measured replay duration is excluded from original-result
equality. The original measured duration remains in the original journal.

The replay calls the existing native-baseline and write-proof validators against
the actual external SQLite and ACK files. The composer additionally reads the
final oracle's immutable `{kind,request_id,data}` records and binds them to the
matching controller request in that case. Guest/external transfer references
must have matching lengths and hashes, and the complete external bytes are
rehashed. The saved oracle projection cannot substitute for a collector record.

The independent raw checks cover:

- Actual foreground/scan/intro/preview overlap recomputed from the retained
  workload artifact, including source-process work intervals.
- Original and recovered guest boot/process identities, fault observations,
  non-root errno probes, replacement fencing, and original mount geometry.
- Both sustained blocked syscall/FD observations and concrete completion after
  recovery, including the original stream lease or drained metadata worker,
  original PID/TID/start identities, retained root-anchor exceptions, and the
  actual source FD's disappearance.
- PostgreSQL lock wait, matching raw JSON-log SQLSTATE 57014, query/backend
  identity, rollback, and independent before/after SQLite inspection.
- Preserved acknowledged catalog/user/settings/progress state, interrupted job
  transitions, explicit replacement/original rebind ACKs, and actual resumed
  playback decoding with a matching post-resume durable ACK.
- Final immutable SQLite job/cache/process observations, native scan/storage/
  stream counters, spool inventory, fault backend release, restored volume and
  permission identity, filler absence and workload/lock unit observations.

Full external controller/adapter process-tree closure still requires the
separate infrastructure observation. The existing controller journal records
adapter PID/start/cgroup ownership but has no durable descendant-empty record.
The existing Actor and lock-unit collectors record their top-level cgroup PID
lists, while the product snapshot recursively records the Goby cgroup. The
composition result explicitly leaves
`external_controller_tree_closure_established:false`; it must not be used to
close an external worker or collector without the separate evidence.

Collector records contain decoded HTTP data and response hashes. They are not
raw wire-body archives. Guest mutation intents remain guest-owned originals;
the composer does not claim to archive them merely because an oracle reply was
retained. Source pins, external release copies and bound native replies provide
the existing protocol's provenance; missing additional archival evidence must
remain visible in the final campaign record.

The earlier `case-inputs-01/*.pending.json` drafts are schema references only.
They contain neither current execution nor accepted recovery evidence. Do not
pass them, fabricate receipts, alter original `partial` results, or omit failed
attempts to obtain a matrix pass.
