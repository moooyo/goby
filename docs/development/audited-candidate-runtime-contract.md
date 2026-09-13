# Candidate runtime epoch contracts

Status: the product gate, binary transition and backup-limit environment
revision are complete. Live admission04 is running against runtime epoch v2.
The revised readers passed five new and seven compatibility guards. See the
[environment closeout](audited-candidate-backup-limits-revision.json),
[revision checks](audited-candidate-backup-limits-tool-verification.json),
[binary-transition tool verification](audited-candidate-transition-tool-verification.json) and
[state review](audited-candidate-transition-state-review.json).
The schemas below are enforced by
[`audited-candidate-runtime.py`](../../scripts/test-env/audited-candidate-runtime.py).
They supplement the [transition plan](audited-candidate-cancellation-transition-plan.md).
`Descriptor` means exactly `{"path": "/absolute/path", "sha256": "64 lowercase hex digits"}`.
All records, source snapshots and credentials remain private. These contracts
are internal review artifacts, not requests for another user approval.

## Transition input, version 1

The exact fields are:

| Field | Contract |
| --- | --- |
| `kind`, `version` | `audited-candidate-transition-input`, integer `1` |
| `output` | A fresh `R/candidate-cancellation-transition-NN` directory |
| `originalProvision`, `seedManifest`, `seedSessionAddendum`, `admission02` | The four fixed historical descriptors in the runtime module |
| `newFullReport`, `newSourceManifest`, `newBinary`, `compiledCatalog` | Descriptors inside the selected `fb6c70468fa3` full-verification scope |
| `frontendReport` | The unchanged provisioned frontend build descriptor |
| `helpers` | Exactly `seed`, `provision`, `gateway`, `admission`, `reconcile`, each a frozen source descriptor |
| `reviewedStateContract` | Descriptor of the internally reviewed state contract below |
| `budgets` | Exactly `maximumSeconds:900`, `stopSeconds:60`, `readySeconds:60`, `maximumPublicRequests:10`, `stopCalls:1`, `replaceCalls:1`, `startCalls:1` |

The complete passing full report, worker report, actual source file closure,
archive and new binary are verified before creating the transition output or
staging executable bytes. The source manifest's entire migration/frontend/catalog
membership must match the old corresponding closure. A descriptor alone does
not substitute for a passing product report or actual artifact bytes.

`helpers.admission` must equal the frozen `helper` descriptor in admission02's
retained report (source SHA256
`9f3d92cf374c2a6ad72637e8c00f5ec34d8cfbf39d32b1446224861ff79b941d`).
It is imported solely for pure snapshot/projection functions;
its admission main is never called. A future v2 admission controller has its
own independent source pin and consumes the completed epoch. This avoids a
runtime-manifest/code-pin cycle and does not relabel the old executed source.
The metadata/listener helper remains frozen gateway03, SHA256
`8c8e962b8d55e1143972e720e55aa99a89dcb49610b729040bbc773b4bab0c2a`.
A later browser-recording gateway revision is a separate input and does not
replace this transition dependency.

## Reviewed state contract, version 1

The exact fields are `kind` (`audited-candidate-reviewed-state-contract`),
`version` (`1`), `sourceSnapshot`, `controlDocuments`, `diagnosticsSnapshot`,
`preserved`, `loadedUnits`, and `diagnosticPolicy`.

`sourceSnapshot` is a descriptor of the captured 35-table/sequence snapshot in
the existing `admission.snapshot_sql` format. The complete comparison requires
eight known users, three libraries/roots/completed scans, 13 stored/ten public
items, no playback/task/trigger state, and exactly the three revoked credentials
bound by `(kind, token hash)`. The third credential ID is additionally fixed by
admission02. Q's eight raw policy fields and ordinary `{}` policies remain exact.

`controlDocuments` describes a saved JSON object mapping relative paths beneath
`C/data` to parsed control documents. It proves the existing deployment marker,
primary/default generation, no activation journal, idle recovery control and no
backup entries/pending publication. `diagnosticsSnapshot` describes the captured
registry, registered log facts, lock identity and directory identity.

`preserved` contains exactly `recovery`, `trees`, `fixedFiles`, `protected`,
`postgresProcess`, and `generation`, in the same form as `Transition.capture`.
`recovery` holds both source and recovery database/role/schema identity facts;
the latter must still match its original empty-database inventory.
`trees` has exactly the six roots `C/data/recovery`, `operations`, `backups`,
`cache`, `media`, and `C/install/admin`. Directory facts retain device/inode,
owner/group/mode and modification/change times. File facts additionally retain
size and SHA256. `fixedFiles` covers the original provision manifest, environment,
both unit files and every regular file immediately beneath `C/data`. `master.key`
has either full byte/identity facts or an explicit `absent:true` entry. The data
root's complete immediate inventory/identity is retained; an additional
uncovered directory is rejected. `protected` covers the four named unaffected services.

`loadedUnits` records both candidate units' normalized static properties, including
the actual command argument arrays instead of timestamp-bearing `ExecStart`
formatting. `diagnosticPolicy` is exactly `maximumFilesBefore:14`,
`retentionDays:7`, `maximumLogBytes:4194304`, `maximumAppendBytes:1048576`,
`pruningAllowed:false`. This small transition refuses a baseline needing pruning.
It accepts precisely the old active log closing, one new active log, stable old
registered file identities/prefixes and the corresponding registry update.

Before stop, the operator captures current SQL, controls, files and process state
again. The reviewed snapshot is never labeled fresh. A mismatch stops the action
before requesting stop; an after-state mismatch retains transition responsibility.

## Runtime epoch and seed runtime binding

`audited-candidate-runtime-epoch`, version `1`, has exactly `kind`, `version`,
`status`, `transitionInput`, `transitionHelper`, `runtimeHelper`,
`originalProvision`, `seedProvenance`, `currentSource`, `candidate`,
`candidateProcess`, `postgresProcess`, `lease`, `before`, `after`,
`preservation`, `calls`, `helpers`, and `candidateAdmissionComplete`.

`currentSource` contains `archiveSha256`, `sourceManifest`, installed `binary`,
`fullReport`, and `schema:28`. `candidate` is an explicit runtime projection of
unchanged deployment configuration/database identities and the new executable,
unit invocation and listener. It records `bootstrapExecuted:true` and
`sourceState:{users:8,schema:28,migrations:28}`, omits the setup token, and points
to `originalProvision` and `seedProvenance`. Its `input` is the transition input;
readers use `currentSourceManifest`, not the old provision input's source field.
The status is only `running_awaiting_live_acceptance`.

`audited-candidate-seed-runtime-binding`, version `1`, has exactly `kind`,
`version`, `runtimeEpoch`, `originalSeed`, `seedExecutor`, `seedInput`,
`seedSessionAddendum`, `admission02`, `serverId`, `admin`, `actors`, `controlQ`,
`catalog`, `catalogFile`, `actualCatalogDtos`, `libraries`, `roots`, `resources`,
`seedCleanup`, `currentSessions`, and `candidateAdmissionComplete`.
All business mappings and original executor/input remain equal to the original
seed closeout. `currentSessions` adds the separately attributed admission02
credential; it never changes the original two-session seed cleanup claim.

`EpochReader` supplies `pin`, `process`, `assert_target_cluster`, `sql_json`,
`database_facts`, and `deployment_lease`. `EpochIO` reuses the bounded original
recorder through explicit epoch/binding descriptors and a fresh output. Neither
calls provisioning, bootstrap or users0 inspection. Future admission revisions
must check current epoch/source and original seed provenance as separate links.

## Commands

Read-only preparation does not require a future product input. The exported
`capture_reviewed_state(runtime, helper_pins, output, input_pin, source_pin,
runtime_pin)` function uses only existing provision/seed/admission evidence and
the old compiled catalog, then obtains a fresh read-only candidate snapshot.
Its output must be `R/candidate-transition-state-capture-NN`. It returns a
contract descriptor for internal review, with `productGateEvaluated:false`.
It never fabricates passing full/build fields or stops/starts a service.
The capture input contains exactly `kind:audited-candidate-state-capture-input`,
`version:1`, `output`, and the five frozen `helpers` descriptors. It contains
no future full report, binary or previously captured state-contract field.

Use root SSH with `python3 -I -B`. Both modes take `--input`, `--input-sha256`,
`--source-sha256`, `--runtime-helper`, and `--runtime-helper-sha256`.
`--check-captured` only replays saved source/DTO/control artifacts, creates no
output and issues no SQL/HTTP/service calls. Its result explicitly says that no
product was admitted. Omit that flag only for the separately frozen single
transition after full verification and operator review have completed.

## Candidate backup-limit configuration revision

`revise-audited-candidate-backup-limits.py` performs one environment revision.
Its version 1 input has exactly `kind` (`audited-candidate-environment-revision-input`),
`version`, `output`, `previousEpoch`, `previousSeedBinding`, `admission03`,
`failureCloseout`, `closedState`, `runtimeHelper`, `additions`, and `budgets`.
The output is a fresh `R/candidate-backup-limits-revision-NN`. The previous epoch
is the completed binary transition `7bcdbc...`; admission03, its `090d044...`
failure closeout and the `9c2a666...` complete state remain immutable authorities.

`additions` is exactly `GOBY_BACKUP_MAX_OBJECT_BYTES=67108864` and
`GOBY_BACKUP_MAX_TOTAL_BYTES=268435456`. Both keys must be absent from the old
environment. Preserve its complete byte prefix and explicit
`GOBY_BACKUP_MIN_FREE_BYTES=67108864`; append the two assignments only. The
existing file was generated as bare `key=value` assignments and independently
checked to contain no quoted, escaped or edge-whitespace values. Retain a
root-owned 0600 byte copy, stage on the same filesystem, and perform one stop,
one atomic environment replacement and one start. The binary never changes.
Free space must cover a conservative two 64 MiB payloads, 64 MiB minimum free
space and 32 MiB plus 64 KiB metadata reserve: `234946560` bytes.

The old closed state uses `runtimeEpoch`; the new operation's before/after
captures explicitly use `previousEpoch` as their anchor. Neither after capture
claims that its new PID is the old runtime. Both forms validate all six revoked
sessions: the original three plus admission03's three identified credentials.
Preserve all 35 tables, sequences, old rows, the one failed create operation,
the one generated/failed/empty/zero-size quota object, control/media/secret files,
and the empty recovery database. Only the declared environment file and normal
diagnostic rotation/unit-log appends may differ. Keep their complete evidence
descriptors in `epoch.preservation`.

The resulting runtime epoch is version `2`: all version 1 fields plus
`previousEpoch`, `productInput`, `operationKind`, and `configurationChange`.
`operationKind` is `environment_revision`; `calls` is exactly
`{stop:1,replaceEnvironment:1,start:1}`. `productInput` is the previous binary
epoch's `transitionInput`; the current `transitionInput` remains this environment
revision input. `currentSource`, binary and static deployment fields are inherited
exactly. `candidate.runtime`, current process/listener/lease, and current input
are updated without rewriting the original manifests.

`configurationChange` has exactly `before`, `after`, `preservedCopy`, `additions`,
`requiredFreeBytes`, and `observedFreeBytesBefore`. Readers verify the actual old
copy and new environment bytes through the same pure append validator, then
verify the process and loaded values. `resolve_epoch_lineage(epoch, reader)`
returns `productEpoch`, `productInput`, and `configurationInput`; only one v2
environment epoch pointing to its v1 binary predecessor is supported.

The seed runtime binding is also version `2`, adding `previousBinding`,
`admission03`, `failureCloseout`, and `closedState` to the version 1 field set.
Original seed executor/input, business mappings and two-session seed cleanup
remain unchanged. `currentSessions` contains the six fully attributed revoked
credentials. A new successful admission for this current epoch is still required
before browser work; failed admission03 cannot be reused as a success gate.
