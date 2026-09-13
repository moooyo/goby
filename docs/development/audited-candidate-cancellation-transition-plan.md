# Audited candidate cancellation-fix transition

Status: the [single transition completed](audited-candidate-cancellation-transition.json)
after the full product verification and Linux build passed. The executed input
and historical process expectations below remain frozen. This plan implements
the separately reviewed candidate-transition option in the
[current execution plan](../planning/current-execution-plan.md).

Preparation now has [seven passing remote tool guards](audited-candidate-transition-tool-verification.json)
and a [reviewed current baseline](audited-candidate-transition-state-review.json).
The 35-table comparison attributes only one revoked native session and two
session audit rows to admission02; all earlier rows and the other 33 tables are
unchanged. All fourteen media files match the original fixture hashes and
read-only permissions. The first read-only review reader used the wrong activity
column name; its source remains retained, and the corrected reader consumed the
same captured data without repeating any SQL or business operation.
Admission v2 also passed [23 remote guards](audited-candidate-admission-v2-tool-verification.json).
These preparation results authorize neither a missing full-product result nor
a claim that the transition or live admission has already happened.

## Product and retained candidate

Use `C = /opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b` and
`R = /opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14` below.

The new product archive is
`b7120b6f323ace203fe7b49c56cd669ce5b9ff3ef8f3ceddb4359c2951aa658b`.
Its only product change is the worker-finalizer cancellation correction in
[`internal/recovery/manager.go`](../../internal/recovery/manager.go), with
corresponding regression coverage. There is no migration or frontend change.
The targeted closeout at `R/recovery-cancel-targeted-closeout.json`, SHA256
`319c56a52a4a4c424787ffd42b17c021a01878f2b2e50e5cb253e2c37edc659f`,
passed two top-level tests and two subtests, including actual CAS failure.
The full run passed in
`/opt/goby-test/audit-fixes-20260913-20260913T083107Z-fb6c70468fa3`.
Its report, worker report, source manifest and built binary are bound by the
[full closeout](restore-cancellation-full-verification.json); the new executable
has SHA256 `477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f`.

The last reported candidate uses binary SHA256
`a9b25b6b3e9f04b528ca77cd0a0dd548ae4c2715c06a1a56a6def23c6e00e2d7`,
server PID `363605` and PostgreSQL PID `363520`. These are preflight expectations,
not fresh observations made by this document. The original private provisioning
manifest remains `C/private/manifest.json`, SHA256
`022ca1e48aac6159750df72157dcddff3738e67962012f556825e26b0f0dfb09`.

The [seed closeout](audited-candidate-seed-closeout.json) binds
`R/candidate-core-seed-reconciliation-02/private/manifest.json`, SHA256
`1c4e3b5b0005281b33eaaad3e5b7819f6a3bb93db8559bf91713d73ac1bf50e1`.
Retain its eight accounts and credentials, three libraries/roots/completed scans,
13 stored item rows, ten public catalog entries, fourteen copied files and zero
playback history. Its original business executor remains seed SHA256
`365363509c9e9b71c2b5ae323ddc9c4a39605a5f6f8596a84a5693d933e1ced7`.
Keep both seed/reconciliation failures and the exact session-binding addendum.
Admission01 made no HTTP request. Admission02 added one native administrator
session and proved its logout and same-token 401; it created no backup or restore.
Fresh preflight must confirm all three current sessions are revoked, binding
the two seed sessions and the additional admission session to their own receipts.
The recovery database is expected to remain empty.

## One bounded transition

Prepare a new exclusive scope such as `R/candidate-cancellation-transition-01`.
An existing scope is a collision, not permission to resume. Proposed limits are
15 minutes overall, one stop request, one atomic executable replacement and one
start request, with at most 60 seconds for readiness and ten public HTTP requests.
The final input must fix the actual limits, helper/source hashes and receipts.
No login, bootstrap, scan, backup, restore, generation activation or rollback is
part of this operation. Complete the full verification before stopping Goby.

1. Pin the exact full result and binary bytes, new source manifest, unchanged
   schema28 catalog/migration closure, unchanged frontend inputs and the existing
   57 installed assets. Pin current unit bytes and loaded properties, environment
   descriptor, software-transcoding profile, public/direct origins, PG cluster
   identity, both database/role identities, server process/listener and deployment
   lease. Preserve secrets only in private records. Reject unexplained drift.
2. Capture the owned before-state defined below. Require no active scan, task,
   encoding or recovery worker, no pending generation transition, and no
   scheduled startup/due/catch-up work during the bounded window. Require the
   already established recovery binding marker. A different baseline requires
   a separately reviewed input; do not disable tasks or initialize missing state
   merely to force this plan through.
3. Preserve verified old executable bytes in the new private scope together
   with its original owner/mode/file-object metadata. Stage the new executable
   as an exclusive regular file on the same filesystem as `C/install/goby`;
   verify its bytes, executable mode and owner, then fsync it and its directory.
   Preserve existing units, environment, assets and every old manifest/receipt.
4. Persist the exact stop intent before requesting stop of only
   `goby-audited-20260913T073217Z-ef77f9ffcf0b-server.service`. Record completion,
   old process exit, empty recursive cgroup, closed old listener and release of
   its deployment lease. PostgreSQL must retain its invocation, PID/start ticks,
   executable object, cluster identifier and database/role ownership throughout.
5. Persist replacement intent, recheck destination/staged-file authority and
   atomically rename the staged file over `C/install/goby`; fsync the directory
   and record the installed SHA256/file object. Persist one start intent and
   request start once using the unchanged `Type=exec`, `Restart=no` unit. Never
   rewrite the immutable provisioning manifest to make old guards accept this.
6. Bind the new PID/start ticks, invocation, boot ID, executable bytes/object,
   command, UID/cgroup/network namespace and the loopback listener's inode owned
   by that PID. Prove one granted deployment lease for the same source database,
   linked to the new server's actual PostgreSQL socket. Its backend PID/client
   port may change; the postmaster and logical recovery generation must not.
   Check health/readiness and unchanged public server ID/origin using only the
   new process/listener binding. Capture after-state and publish the new runtime
   epoch plus seed binding only after all comparisons pass.

A stop timeout, forced kill, unknown replacement/start outcome, failed readiness,
lost lease or unexplained state change fails this transition. Preserve exact
stage/code, unit/process observations and stop/replace/start responsibility.
Do not automatically restart, swap back, restore or clear data. An old-binary
return would need a new reviewed action; the preserved bytes enable that review.

## Owned preservation contract

Capture logical rows and sequence values from the same 35 owned tables in the
SHA-bound [schema28/PG17 catalog](../../internal/backuppg/catalogs/schema-28-postgresql-17.json),
using one consistent read-only source snapshot. Reuse the narrow catalog reader
and `snapshot_sql` shape in
[`admit-audited-candidate.py`](../../scripts/test-env/admit-audited-candidate.py).
Compare all rows and sequence values, excluding only capture timestamps. Keep
the exact stored/public mapping: three `CollectionFolder` rows have library IDs,
null parents and empty paths; the root album omits public `Path`; Q stores eight
policy fields while ordinary users store `{}`. Do not replace these checks with
public DTO counts or remove unexplained fields.

The idle, unchanged-schema baseline should preserve those logical rows exactly.
Startup is not inherently read-only: `ServerID` performs a no-op SQL update;
task-definition reconciliation updates only missing/different definitions;
schedule initialization can record missed occurrences or admit startup work;
scan/task/encoding recovery can retire active work. Pin the existing definitions,
triggers and idle state before stop. Unexpected logical changes stop closure.
Database physical files, WAL, transaction IDs and LSNs are not equality targets.
See [task reconciliation](../../internal/tasks/store.go) and
[scheduler initialization](../../internal/tasks/scheduler.go).

Additionally bind source/recovery schema and role/owner facts; source recovery
marker and logical generation; recovery/control records and their exact file
inventory beneath `C/data/recovery`, `C/data/operations` and `C/data/backups`;
cache inventory; fourteen media bytes, paths, owners, modes and file objects;
and existing configuration/assets/unit files. Recovery remains uninitialized.
The control inventory must include existing lifecycle/generation registry and
activation journal, recovery `current.json`/CAS proof and backup catalog/markers,
plus their registered objects and lock-file identities. No pending backup
publication/deletion or cache job directory may require startup cleanup in this
minimal scope. Do not delete locks or control files to obtain ownership.

Distinguish persistent control bytes from process-owned lock acquisition/release.
Diagnostics normally close/append the old active log, create a new registered
`goby-*.jsonl` and update `.goby-diagnostics.json`; unit logs also append. Pin
these exact paths and registry transitions. Startup pruning must either have no
eligible victim within the frozen window or name the exact already registered
logs allowed by the pinned retention policy. No directory-wide deletion
allowance is acceptable. See
[diagnostics open](../../internal/diagnostics/store_linux.go). Compare
fresh identities of the primary/source55 services, candidate PostgreSQL and the
original-client host without restarting or reconfiguring them. Cite accepted
historical closures for unrelated scopes; do not rehash the previous 198 roots
or claim they were freshly reverified.

## Minimal data-contract changes before dispatch

| Existing consumer | Required narrow adaptation |
| --- | --- |
| `CandidateIO` in the seed helper | It fixes `MANIFEST`/`INSPECTION`, old binary/processes and historical `sourceUsers=0` / `bootstrapExecuted=false`. Add a separately frozen epoch-aware reader with explicit original provision, new runtime epoch and seeded-state descriptors. Keep its IO budgets/listener ownership guards. Do not weaken or execute `Seed.run`. |
| Frozen `CandidateInspection` (`inspect-candidate-02.py`) | It fixes the old manifest, binary, PIDs/invocations and `source_state()` users0. A new revision must accept only the reviewed runtime epoch and actual seeded baseline. Reuse read-only cluster/ownership/lease primitives; never call the old users0 inspection main. |
| `prepare-audited-candidate.py` and seed reconciliation | Provisioning assumes a fresh root/cluster and old archive; reconciliation binds an old process epoch and exactly two seed sessions. Neither is a transition runner. Preserve the old sources/results; the new binding references their data provenance plus the third revoked admission session. |
| `admit-audited-candidate.py` | `SOURCE_MANIFEST_SHA`, inspector SHA, `candidate.input.sourceManifest`, and `seed.source/processes == current` bind the old product/epoch. Freeze a new admission input/revision that checks current source and epoch separately from original seed execution. Reuse unchanged schema catalog, credentials and exact mappings; do not impersonate the new product through the old provision input. |
| Browser candidate input and gateway attestation | Reuse seeded actor/catalog descriptors and unchanged original-client hosting proof. Supply the new candidate source/process/listener epoch and a matching fresh gateway binding before any client run. Old gateway attestations cannot prove the new upstream process. |

Publish a new private `runtimeEpoch` descriptor containing the transition input,
original provision descriptor, old/new source and binary descriptors, unchanged
configuration/assets/database identities, before/after proofs, stop/start calls,
new process/listener/lease and a status of `running_awaiting_live_acceptance`.
Its state facts are the actual seeded state, not zero users. Publish a separate
`seedRuntimeBinding` linking the unchanged seed closeout, executor `365363...`,
original credentials/catalog/cleanup proofs, admission02 cleanup, and this new
epoch. It changes runtime attribution, not the claimed seed executor or history.
Keep all original manifests immutable; readers must verify both links explicitly.

After this transition closes, freeze a fresh bounded live-admission attempt for
the new product, including the corrected cancellation path and its retained-stage
semantics. A successful restart establishes neither that live cancellation gate
nor candidate admission, core-client acceptance, a main upgrade or M2-M6 closure.
