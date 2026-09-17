# Retained Programs transition finalization

`finalize-programs-transition.py` is a fixed, read-only producer for the retained
`candidate-programs-transition-01` failure. It creates only
`candidate-programs-finalization-01` under the existing resumed-delivery root.
It never calls the original transition `run`, `open`, `products`, public HTTP,
stop, replace, start, or recovery methods. The original CLI, caller, and SSH
exit codes remain 2; the original PowerShell exit remains 1. Original failure,
before/after, source snapshots, and original closure receipts remain unchanged.

The input binds the original D000 transition input, 72c177 transition operator,
2bbf5e original runtime, the fixed failure and before/after snapshots, original
source snapshots, the fixed independent failure review r02, and a separately
published UTC rollover review pinned to SHA-256 `67057f9bb30bf15185bf21bf8c0222eeec186d158be518ce32d1a6821fc82f42`.
The source snapshots and three original public
GET intent/response pairs are read through the original outer closure's saved
output Pin3 inventory. The same inventory supplies the original stop intent,
stopped state, replacement intent/result, and start intent; their process,
unit, lease-release, binary, staging, and old-copy bindings are checked.
Original command responsibility records must show all
40 commands acknowledged and closed. The original actions remain 1/1/1, with
three completed GETs. No original success is inferred from resource closure.

The rollover review has exactly these fields:
`kind`, `version`, `status`, `transitionInput`, `failure`, `before`, `after`,
`shutdownLog`, `startupLog`, `oldSourceManifest`, `oldMainSource`, `oldStoreSource`.
Its kind is `audited-programs-shutdown-rollover-review`, version is 1, and status
is `exact_saved_rollover_reviewed`. Ordinary references are Pin2 objects
(`path`, `sha256`). The two log references are Pin3 objects (`path`, `bytes`,
`sha256`) for root-owned immutable copies outside the finalization output.
The validator reads and hashes the referenced source and log bytes. It accepts
only the exact 143-byte shutdown log and original 1101-byte startup prefix,
their fixed names and inode identities, the saved UTC time window, nine
unchanged old files, one closed shutdown rollover, and one new active log.
This is not a general two-file exception. Detailed attribution and original
reader/SSH closure evidence belong in separate review receipts.

The finalizer takes one fresh capture through the existing native reader.
Its fixed budget is 300 seconds, including an internal 15-second cleanup
reserve, with exactly 16 read-only SQL commands, at most 64 selected
`systemctl show` commands, zero public requests, and zero service actions.
SQL is restricted to the existing capture queue, read-only transactions, and
read-only session settings. Fresh state must retain the actual new candidate
PID 1907978, start ticks 34901535, executable identity, listener, lease,
protected units, hosting identity, logical rows, controls, trees, and fixed
file facts. The ten closed diagnostic files remain exact; the current active
file may only preserve its original prefix under the existing bounded rule.
Key files remain stat-only, and environment/configuration evidence remains
hashes and metadata. The historical runtime chain reads only its selected
JSON records and observation source; its existing 4 MiB per-record limits fit
the finalizer's reader limits without broadening their scope.

The producer writes `antecedent.json` first, followed by `preservation.json`,
`runtime-epoch.json`, `seed-runtime-binding.json`, and `result.json`. The
antecedent references no future epoch, binding, or result, avoiding a digest
cycle. Epoch version 4 gains one optional Pin2, `finalization`, pointing to
the fixed antecedent. `transitionHelper` remains the original 72c177 source;
`runtimeHelper` names the actual new validator. The antecedent binds the new
producer and validator, original failure/actions, rollover review, independent
closure, fresh capture, and all fresh command responsibilities. The four
original snapshot paths and pins stay original. Only the finalized branch may
place preservation at the fixed finalization publication root. Consumers read
and validate all referenced evidence before enabling the diagnostic exception.
Legacy V4 without `finalization` keeps its existing rules and reads.

Execution requires a separately reviewed outer executor holding the existing
deployment flock for the entire operation. The outer executor owns the memory,
CPU, task, unit, deadline, deployment-lock, cgroup, descriptor, original caller,
and original SSH closure evidence. `SSH_CONNECTION` is only a launch-context
check; it is not proof of the original SSH process exit. The finalizer records
that its own process exit and outer unit/SSH closure are still pending. Its
result cannot substitute for that independently observed closure. An outer
failure or an incomplete publication retains the new output for review and
does not authorize an automatic retry, rollback, or recovery.

Published state remains `running_awaiting_live_acceptance`, with
`candidateAdmissionComplete` false. Fresh validation is not client acceptance.
Concrete publication source pins and input/review pins must be reviewed before
an actual remote execution; draft or null pins are never accepted by readers.

The new Python and JavaScript component suites are pending source freeze and
a remote execution receipt from the designated test environment. They extend
the narrow finalization contract without rerunning historical transition,
recovery, or product acceptance suites.
