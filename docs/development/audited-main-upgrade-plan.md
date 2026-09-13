# Audited main upgrade contract

Status: **DRAFT, updated 2026-09-14; main preparation only**. The TV successor's
affected admission05 has passed and reuses the preceding admission04 contracts.
Core original-client acceptance and full main safety facts remain open. The
[read-only preparation review](audited-main-readonly-preparation.md) extends the
initial service/file observation with private configured-path selection,
captured lifecycle/control/backup metadata and actual PostgreSQL/schema27 binding.
Main remains inactive. These observations do not authorize startup, backup
creation, migration, restore or promotion.

The [current execution plan](../planning/current-execution-plan.md) controls
execution. The [source55 main plan](main-schema28-upgrade-plan.md), its prepared
inputs and its runners remain superseded execution designs. Replacing their
hashes, PIDs or deadlines does not admit this release. Freeze a new concrete
input and independently review the selected implementation before execution.

## Selected product and open gates

The selected candidate is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`. Its product includes
R01-R21 through `a623375`, diagnostic commit `a9c541a` and the ready-plan
cancellation fix `d03db2e`. The preceding admitted schema28 binary SHA256 was
`477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f`;
its source manifest SHA256 is
`65e11ed30ffc769eddde99a2b9ce2387707f085f85e813c70f7b7d84c9519732`.
The [full verification](restore-cancellation-full-verification.json) passed
2,264 tests across 25 packages with race instrumentation and a Linux build.
Retain these historical identities and all 57 unchanged frontend assets.

The later [TV parent metadata increment](tv-parent-metadata.md) has also passed
full verification: 2,270 tests/25 packages and a Linux build. Its selected
successor binary is `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`,
installed through the [candidate transition](tv-parent-candidate-transition-closeout.json)
and admitted by [affected admission05](tv-parent-affected-admission-closeout.json).
The `477d26ad...` identity above records the
admitted predecessor. Freeze main against the final admitted successor
and its actual core-client evidence, rather than promoting that predecessor
through stale product pins.

[Live admission04](audited-candidate-live-admission-closeout.json) passed under
configuration epoch SHA256
`72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e`.
The [capacity configuration revision](audited-candidate-backup-limits-revision.json)
preserved the selected binary/source and preceded that admission. Its candidate
capacity settings do not become main deployment defaults automatically.

Core original-client acceptance remains open. MP3/FLAC pass, while movie,
episode and subtitle acceptance retain their recorded failures. Subtitles01
completed the visible journey with two unattributed native undefined errors
and an unresolved partial-media timing difference; its owned state is closed.
These client increments made no main-service change. **No main upgrade is
admitted until the core
gate and fresh main safety/recovery gates pass for the selected product.**
Require login/browse, movie/TV, seek/stop/resume, MP3/FLAC, supported external
subtitle, durable-state and user-isolation evidence, with exact owned cleanup.

Positive global NextUp and LibraryChanged automatic refresh remain open feature
claims. They do not become blanket prerequisites for this partial internal
upgrade. Main deployment success will not establish those features or M2-M6
completion.

## Evidence that can be reused

| Accepted record | Reusable conclusion | Boundary |
| --- | --- | --- |
| [TV successor full verification](tv-parent-metadata-full-verification.json), [transition](tv-parent-candidate-transition-closeout.json) and [admission05](tv-parent-affected-admission-closeout.json) | The currently selected snapshot passed 2,270 tests in 25 packages with race instrumentation and a Linux build; the installed successor passed affected admission | Reuse the selected product proof; core-client acceptance and main safety/admission remain separate |
| [Cancellation-fix full verification](restore-cancellation-full-verification.json), [source freeze](restore-cancellation-source-freeze.json) and [binary transition](audited-candidate-cancellation-transition.json) | The predecessor snapshot passed 2,264 tests in 25 packages with race instrumentation and a Linux build; that predecessor was installed in the isolated candidate | Historical predecessor verification, not the current successor's final product proof or core/main admission |
| [Live admission04](audited-candidate-live-admission-closeout.json) and [configuration revision](audited-candidate-backup-limits-revision.json) | Admission passed with 79 complete responses, eleven healthy/ready samples over ten minutes, backup download, ready-plan cancellation and owned-session cleanup under epoch `72e25f9...` | The inactive stage is retained; no restore application, rollback, core-client acceptance or main upgrade was performed |
| [Earlier diagnostic verification](exit-diagnostics-full-verification.json), [source freeze](exit-diagnostics-source-freeze.json) and [publication](exit-diagnostics-publication.json) | Historical verification of the 2,262-test predecessor and diagnostic change | It predates the cancellation fix and is not the selected snapshot's final verification |
| [Frontend build](audited-candidate-frontend-build.json) and [source/evidence reuse](audited-candidate-frontend-reuse.json) | 57 built assets and 41 earlier mocked API cases from 64 unchanged source files | No original-client acceptance or new browser-test execution is implied |
| [Source32 publication](source32-product-publication.json), [main schema27 completion](m3e-main-schema27-completed.json), [migration](m3e-main-schema27-migration.json) and [rehearsal disposal](m3e-main-schema27-rehearsal-disposal.json) | Provenance for the old executable, main identity, preservation method and consumed 26-to27 deployment | Its dump is schema26; `main_database_restored` and `old_binary_rollback` are false. It cannot supply this run's schema27 backup or rollback proof |
| [Accepted source55 migration](client-schema28-accepted-migration.json), [rehearsal](client-schema28-accepted-rehearsal.json) and [attestation](client-schema28-accepted-attestation.json) | Historical 27-to28 preservation and restore mechanics on their exact candidate | Different product, database and private state; no main baseline or fresh rollback authority |
| [Old main preparation](main-schema28-prepare-passed.json) and [service addendum](main-schema28-readiness-service-addendum.json) | Reviewed field coverage, including repeated `EnvironmentFiles` values | Preparation performed no upgrade and did not check the client gate; its process/startup authority has expired |
| [Reboot baseline](resumed-delivery-reboot-baseline.json) | At 2026-09-13T06:21Z, the main service was inactive with PID0; PostgreSQL 17 main was active after the 06:18:45Z reboot; installed old binary matched its accepted hash | It made no database reads and did not re-attest data preservation or every historical root |

The retained old executable is `/opt/goby-dev/goby`, SHA256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
Historical process IDs are invalid after the reboot. The previous unexplained
service exits remain a tracked limitation; readiness does not establish their
cause. Pin each receipt actually consumed by the new input. Preserve old scopes
through their sealed receipts and manifests; do not recursively reverify all
198 historical roots or claim that a narrower observation did so.

## Minimum fresh main facts

The first metadata observation at 2026-09-13T17:54:09Z confirms the loaded main
unit, ordered environment-file references, fixed file identities and installed
source32 bytes. The subsequent private preparation resolves configured paths
and the captured lifecycle's primary/revision0/default selection. The control
store has one completed create, no transition and a declared unclaimed recovery
slot. Its single retained backup records schema23, not a fresh schema27 recovery
point. Actual cluster and database/role identities match the recorded main5432
anchors. Main has the exact 1..27 migration version/name prefix, 35 tables and
a marker matching the independently selected initial primary. Recovery has no
observed non-system relations or binding table; this does not admit it as an
empty native restore target. Full catalog and data preservation remain open.
The [preparation review](audited-main-readonly-preparation.md) records
the exact evidence boundaries and remaining checks.

Neither settled control metadata nor a complete schema27 migration prefix
would establish a zero-write startup. Source32 still calls `ServerID` with an
upsert, and subsystem startup may reconcile pending work and retention. Freeze
those allowed writes from actual data and clock observations before any start.
The main startup chain, complete effective Go configuration, private key
witness, operational baseline and recovery proofs remain separate requirements.
The fresh capacity observation also falls below source32's backup-store open
threshold: 520,519,680 bytes available versus 570,490,880 required with current
defaults and metadata reserve. Resolve this through an explicit capacity
decision before admitting startup; do not test it by starting main or silently
inherit the candidate's lower limits.

The following fields define the required execution input. Reuse the particular
facts already captured above and acquire the remaining private reads under verified
main operation/deployment authority. The historical shared deployment lock is
`/opt/goby-test/exec-work-m3e/main-deployment-schema25.lock`; verify its existing
identity and ownership before use, and do not recreate an absent lock.

| Fact group | Required fresh evidence |
| --- | --- |
| Service and installation | Boot, loaded unit and every permanent/runtime drop-in, ordered environment files, effective paths, UID/GID, sandbox, restart/stop policy, binary/asset bytes and file identities. Record current inactive/PID0/cgroup/listener absence or the actual live PID/start/invocation if independently changed |
| Active recovery identity | Deployment ID, revision, active slot, generation, selected configuration/master, pending operation state and raw `goby.recovery.binding.v1` bytes. Resolve the effective database using the lifecycle state; the primary URL alone is insufficient |
| Main database and cluster | Independently bound port5432 cluster/system identifier, PostgreSQL/tool versions, postmaster boot/start, configuration/HBA/socket paths; database/role/public OIDs, owners, raw and expanded ACLs, normalized role properties, complete schema27 migration/catalog and server ID. Historical anchors are `goby_test`, database16385, role16384 and system identifier `7683277964552005578`; drift requires review, not automatic target selection |
| Data and startup effects | Native archive dump and complete 35-table SourceFacts from one exported snapshot, plus the operational full old-column baseline, root mappings, catalog, all five sequence states/physical facts and application-key witness. Distinguish non-MVCC sequence observations and before/after operational observations from archive snapshot facts. Fresh database clock, pending scans/tasks/playback/encoding/recovery work, runnable triggers, retention and finite startup deadline must explain every allowed startup write |
| Recovery slot and private materials | Identify `goby_recovery_m5j` independently, including actual catalog/data/marker, OIDs, ACLs, role properties and work/session absence. Resolve and inventory runtime/admin/recovery credentials, default and generation masters, vault, lifecycle files, encrypted archives with available passphrases, operation journal, native pairing and diagnostic paths |
| Operational isolation | Exact new evidence/staging/rehearsal paths and ownership; free resources and bounded controller budget; candidate, source55 control, proxy, workspace15432 and media boundaries that this action could affect. Record fresh witnesses for those resources and permitted concurrent changes |

If main remains inactive, preserve that baseline through read-only preparation.
The preferred backup scope below may start source32 once after fresh ownership,
startup and write-scope review; its purpose is the existing native creation
entrypoint, never the old runner's running-PID template. Return main to inactive
after that bounded scope. If main is independently live instead, bind that exact
invocation and review its drain boundary. Before migration, require no unowned
database sessions, prepared work or writers. Missing/pending lifecycle state, an
unexpected active slot, missing master material or uncertain ownership rejects
admission.

Main SQL must name the freshly pinned main5432 target explicitly. Candidate
25498, workspace15432, `goby_client_m3e` and either existing recovery slot are
not rehearsal targets. Do not call `recovery.Open`, a restore finalizer or a
business API merely to inspect main state: opening stores can initialize or
reconcile files.

## Migration and rollback constraints

[Migration28](../../internal/database/migrations/0028_storage_root_bindings.sql)
adds four root columns and two audit columns, replaces three audit CHECKs and
adds the root-binding constraints. Existing roots have `binding_revision=1`
with NULL binding/time/actor; old audits have `previous_revision=0` and an empty
observation fingerprint, while their existing `revision` values remain intact.
No media observation, binding approval, scan or deletion follows
from the migration. Preserve all existing values, mappings, relation identities,
owners, ACLs and sequence facts; the migration history addition and the exact
declared schema28 changes are the only migration deltas.

[Database migration](../../internal/database/database.go) rejects history newer
than the executable's embedded migrations; source32's published code at
`b9bb7b1cf11e07e011a6e1726ddb9d53ef8e1fe9` explicitly rejects migration28.
There is no supported down-migration.
Restoring the old executable over a schema28 database is therefore not a
rollback. Deleting migration28's history row or dropping new columns is not an
accepted recovery method.

[RestoreOffline](../../internal/backuppg/offline.go) shares the
[restore pipeline](../../internal/backuppg/restore.go), which authenticates the
archived schema/data and then migrates to the restoring executable's current
schema. The current implementation restores a schema27 archive to schema28.
The old main schema28 helper explicitly expects that result. A successful
forward restore consequently does not leave a source32-startable schema27
database. A separate, reviewed source32-compatible restoration path and actual
old-executable start are required to establish binary rollback.

[ActiveConfig](../../internal/recovery/runtime.go) selects the database slot,
configuration and master from lifecycle state and never silently falls back to
the primary. [Native recovery](backup-recovery.md) restores logical data and
selected defaults; its archives exclude executables, arbitrary installation
files, media, deployment credentials, logs and caches. Native rollback switches
verified retained generations; it is not a binary downgrade. Explicit rollback
revokes recovered credentials and advances lifecycle revision. Recovery therefore
needs the database, matching key/configuration generation, private layout,
binary, old asset inventory, unit/environment and filesystem ownership together.
Preserving only the dump or default `master.key` is insufficient.

In-place migration does not retire the schema27 database into another slot.
An existing retained slot is neither a new backup nor an automatic rollback
target. Source32 and the selected source have the same lifecycle format and
runtime slot-selection implementation, but the [lifecycle registry](../../internal/lifecycle/format.go)
and [generation reader](../../internal/lifecycle/generations_linux.go) bind
directory/file device-inode identities as well as content. Ordinary directory
copies cannot be treated as a usable restored lifecycle. Use the supported
restore/finalizer to establish legal local binding and generations, or rehearse
the exact identity-preserving restoration method; never edit markers to make a
copied layout appear accepted.

## Required backup and isolated rehearsal

Prefer the existing source32 HTTP backup workflow once the core gate and fresh
main safety review pass. Source32's [offline CLI](../../cmd/goby/recovery_cli.go)
has `backup list/import` and restore commands, but no backup create/export.
[NewOfflineEngine](../../internal/recovery/engine.go) deliberately does not
connect to the source database or open its vault and cannot create backups.
The existing creation entrypoint is `POST /admin/v1/backups` through
[Manager.Create](../../internal/recovery/backup_jobs.go) and `Engine.Create`.

Freeze one bounded source32 service invocation, restart policy, finite startup
and operation budgets, one owned administrator session and one backup request
ID. After reviewing all expected startup writes, start the pinned old binary,
create one native backup, await its actual terminal, download the complete
object and verify its size/SHA256. Revoke the owned session and cleanly stop and
drain that invocation. Preserve the backup/passphrase and operation history;
do not scan, rebind roots, apply a restore or repeat a failed creation in this
main backup scope. An unsafe startup or uncertain write outcome stops this
route for review; this draft does not authorize its execution.

`Engine.Create` obtains dump, SourceFacts and the master witness from one
exported snapshot. Backup completion, download and logout occur later and add
their own audit/session and private-operation changes. Record those exact
post-snapshot deltas separately, including associated sequence changes. Preserve
the archive snapshot as the recovery point and seal a reconciled post-workflow
operational baseline for migration. Do not assert whole-table equality between
those two points or repeatedly create backups to chase equality.

Use a cold export only when a recorded need requires main to stay inactive.
The necessary library calls already exist: `OpenSnapshot`,
`WitnessBackup(snapshot.Tx)`, `Facts`, `Dump`, `ValidateDump`,
`EncodeBackupDefaults` and `backupformat.CreateWithLimits`. They can form a small
reviewed composition entrypoint with private exclusive output; there is no
existing offline CLI that combines them. `WitnessBackup` does not create a
master. `Engine.Create` opens its own snapshot and cannot be used to wrap a
previous dump while claiming the same snapshot. Any such addition needs scoped
verification; it is not the default reason to build another upgrade framework
or execute a source55 helper with substituted pins.

1. Freeze one fresh native schema27 archive using the selected route, with its
   SourceFacts, configuration/master descriptors, archive size/SHA256 and
   private passphrase. Bind the matching operational observations, declared
   post-snapshot deltas, private materials and old installation bytes. If a
   custom dump is extracted for comparison, verify it against this archive's
   manifest rather than creating another recovery point. Preserve all originals
   and the inactive slot. Keep copied secrets/plaintext in a new private
   operational bundle outside application stores; expose none in arguments or
   public logs. Partial output never counts as a completed backup.
2. Reserve a disposable isolated deployment and distinct empty database/role
   targets. Freeze their exact names, cluster policy, paths, ownership, network
   fences and cleanup before creation. Define legal lifecycle creation or
   recovery, including both slot identities, generations and master selection;
   copied main lifecycle files cannot supply fresh fixture ownership.
   Main/candidate URLs, listeners, locks and writable stores must be unreachable
   from rehearsal processes; approved media copies/mounts remain read-only.
3. Import and restore this exact archive with the selected schema28 binary,
   proving `SourceVersion=27`, `CurrentVersion=28`, all schema27 data projections,
   sequences, schema28 defaults and trusted catalog. Exercise the selected
   recovery/activation/rollback contract in that isolated deployment and record
   its real terminal states. Plan/cancel alone and a low-level restore result
   do not prove activation or rollback. Keep native
   credential/generation changes explicit in its separate comparison. Validate
   raw table fingerprints before the finalizer; account separately for revoked
   credentials, expired playback and interrupted/reconciled work afterward.
4. Prove the distinct operational return to source32 in another isolated target
   using the same new archive SHA256 and the actual source32 binary's native
   restore/finalizer. Require `SourceVersion=27` and
   `CurrentVersion=27`, matching configuration/master, legal slot/generation
   binding and old assets/binary. Start that exact old executable in isolation;
   verify schema27, original server/data identity, a restored-account login and
   required reads, then cleanly stop/restart and prove the same legal generation.
   Scope resulting authentication/audit changes and revoke owned sessions.
   Exercise the proposed installation rollback after an isolated upgrade so
   copied files alone cannot count as a rollback test.
5. Retain both results and the usable schema27 recovery bundle. Before native
   normalization, allow only explicit target OID/owner/ACL and restore
   sequence-bookkeeping differences in the restored schema27 projection.
   Account separately for the step3/4 normalization and login/audit changes;
   never remove these fields from in-place main comparisons. Dispose only exact
   owned rehearsal resources with ordinary operations, without FORCE, CASCADE,
   foreign-backend termination or existing-slot resets. Restore any declared
   temporary HBA rule and prove affected shared boundaries unchanged.

The frozen input must select the real restore/rollback implementation and bind
its code, targeted guard/build evidence and receipts. This draft neither adds
a runner nor admits a source55 runner by substitution. Reuse reviewed operators
where their contracts fit; verify any changed helper only for its actual changed
failure modes. All future verification runs through `ssh test-env`.

## Main execution and failure boundary

After the gates and rehearsal pass, freeze the exact main backup point,
preservation allowlist, startup-effect window, restart fence, install/rollback
steps, budgets and independent terminal. Compare current and stopped main state
with the sealed operational baseline, reconciled to the immutable archive
snapshot through only the declared backup-workflow deltas. Unexpected drift
stops the operation before writes. Do not silently refresh either baseline to
accept drift or edit tasks and retention to manufacture admission.

Hold verified external ownership plus lifecycle/database fences in their required
order; migrate27-to28 transactionally and validate preservation before commit.
Install verified assets with explicit modes and index last, then atomically
publish the selected executable while stopped. Preserve old assets and bytes.
Permit one controlled start with automatic retries fenced. Prove the selected
executable, one invocation/listener, startup result, `/readyz`, original public
server identity and exact admin index/module/CSS bytes in a finite direct-GET
workflow. Any authenticated main journey needs its own bounded state/cleanup
contract; candidate accounts and snapshots are not main authority.

A timeout, unexpected exit, uncertain commit, changed foreign state or failed
cleanup retains recovery responsibility. Keep the failed scope and restart
fence, identify the durable phase, and review the already rehearsed forward or
rollback action before dispatch; do not automatically restore, repeat a start,
or relabel a later ready process as the failed run's success. Returning to the
pre-upgrade backup after accepting new writes requires an explicit data-loss
decision; the dump does not contain those later writes.

Completion requires an independent terminal binding controller exit/cgroup,
main process/schema/artifacts, database/private/inactive-slot preservation,
restored temporary policies and scoped external boundaries. Publish separate
candidate admission, core client, recovery rehearsal and main deployment
results. Until those receipts exist, this remains a draft and main is not
reported as upgraded.
