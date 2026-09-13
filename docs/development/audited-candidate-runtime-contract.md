# Candidate runtime epoch contracts

Status: implementation frozen and seven remote pure guards passed; the current
candidate baseline was captured and reviewed without HTTP or business writes.
The actual transition and product gate remain pending. See the
[tool verification](audited-candidate-transition-tool-verification.json) and
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
