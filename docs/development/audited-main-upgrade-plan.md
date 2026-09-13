# Audited main upgrade contract

Status: **DRAFT, 2026-09-13; preparation only**. This document records a source
and evidence review. No remote observation, backup, rehearsal, service change,
runner implementation, test or build was performed for this draft.

The [current execution plan](../planning/current-execution-plan.md) controls
execution. The [source55 main plan](main-schema28-upgrade-plan.md), its prepared
inputs and its runners remain superseded execution designs. Replacing their
hashes, PIDs or deadlines does not admit this release. Freeze a new concrete
input and independently review the selected implementation before execution.

## Selected product and open gates

The selected candidate is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`. Its product includes
R01-R21 through `a623375` and diagnostic commit `a9c541a`, with schema28 binary
SHA256 `a9b25b6b3e9f04b528ca77cd0a0dd548ae4c2715c06a1a56a6def23c6e00e2d7`.
Bind the exact source manifest, executable and all 57 frontend assets from the
accepted receipts, rather than assuming a commit name describes installed bytes.

At this draft's boundary, candidate provisioning has started the new units;
live admission and core original-client acceptance remain open. **No main
upgrade is admitted until both pass for this exact product.** Require the
[candidate admission](audited-candidate-admission-plan.md) terminal and core
login/browse, movie/TV, seek/stop/resume, MP3/FLAC, supported external subtitle,
durable-state and user-isolation evidence, with exact owned cleanup.

Positive global NextUp and LibraryChanged automatic refresh remain open feature
claims. They do not become blanket prerequisites for this partial internal
upgrade. Main deployment success will not establish those features or M2-M6
completion.

## Evidence that can be reused

| Accepted record | Reusable conclusion | Boundary |
| --- | --- | --- |
| [Final diagnostic verification](exit-diagnostics-full-verification.json), [source freeze](exit-diagnostics-source-freeze.json) and [publication](exit-diagnostics-publication.json) | The selected snapshot passed 2,262 tests in 25 packages with race instrumentation and a Linux build | Reuse unchanged product verification; it is not live candidate or main admission |
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

The following fields are required execution inputs, not facts established by
this draft. Acquire private reads and database access under the freshly verified
main operation/deployment authority. The historical shared deployment lock is
`/opt/goby-test/exec-work-m3e/main-deployment-schema25.lock`; verify its existing
identity and ownership before use, and do not recreate an absent lock.

| Fact group | Required fresh evidence |
| --- | --- |
| Service and installation | Boot, loaded unit and every permanent/runtime drop-in, ordered environment files, effective paths, UID/GID, sandbox, restart/stop policy, binary/asset bytes and file identities. Record current inactive/PID0/cgroup/listener absence or the actual live PID/start/invocation if independently changed |
| Active recovery identity | Deployment ID, revision, active slot, generation, selected configuration/master, pending operation state and raw `goby.recovery.binding.v1` bytes. Resolve the effective database using the lifecycle state; the primary URL alone is insufficient |
| Main database and cluster | Independently bound port5432 cluster/system identifier, PostgreSQL/tool versions, postmaster boot/start, configuration/HBA/socket paths; database/role/public OIDs, owners, raw and expanded ACLs, normalized role properties, complete schema27 migration/catalog and server ID. Historical anchors are `goby_test`, database16385, role16384 and system identifier `7683277964552005578`; drift requires review, not automatic target selection |
| Data and startup effects | Same-snapshot full old-column row multisets for all 35 tables, all five sequence states and physical facts, root mappings and application-key witness. Fresh database clock, pending scans/tasks/playback/encoding/recovery work, runnable triggers, retention and finite startup deadline must explain every allowed startup write |
| Recovery slot and private materials | Identify `goby_recovery_m5j` independently, including actual catalog/data/marker, OIDs, ACLs, role properties and work/session absence. Resolve and inventory runtime/admin/recovery credentials, default and generation masters, vault, lifecycle files, encrypted archives with available passphrases, operation journal, native pairing and diagnostic paths |
| Operational isolation | Exact new evidence/staging/rehearsal paths and ownership; free resources and bounded controller budget; candidate, source55 control, proxy, workspace15432 and media boundaries that this action could affect. Record fresh witnesses for those resources and permitted concurrent changes |

If main remains inactive, preserve that baseline through preparation and backup.
Do not start the old service solely to satisfy the old runner's running-PID
template. A future execution input must explicitly support an already stopped
service. If main is live instead, bind and drain that exact invocation once.
Either path requires no unowned database sessions, prepared work or writers
before migration. Missing/pending lifecycle state, an unexpected active slot,
missing master material, or uncertain ownership rejects admission.

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

1. Freeze one fresh schema27 recovery point: custom dump, source facts and full
   state from the same read-only exported snapshot, plus matching private
   materials and old installation bytes. Use an exclusive owned regular file,
   sync it and hash the same bounded descriptor; retain failure fragments as
   failures. Bind the native archive envelope, configuration/master and
   passphrase needed by the selected recovery path to this same source snapshot.
   Preserve the inactive slot and all originals. Keep secrets and
   plaintext recovery materials in a new root-private operational bundle,
   outside application stores; do not expose them in arguments or public logs.
2. Reserve a disposable isolated deployment and distinct empty database/role
   targets. Freeze their exact names, cluster policy, paths, ownership, network
   fences and cleanup before creation. Define legal lifecycle creation or
   recovery, including both slot identities, generations and master selection;
   copied main lifecycle files cannot supply fresh fixture ownership.
   Main/candidate URLs, listeners, locks and writable stores must be unreachable
   from rehearsal processes; approved media copies/mounts remain read-only.
3. Restore the actual dump with selected schema28 code and prove all schema27
   data projections, sequences, schema28 defaults and trusted catalog. Exercise
   the selected recovery/activation/rollback contract in that isolated
   deployment and record its real terminal states. Plan/cancel alone and a
   low-level restore result do not prove activation or rollback. Keep native
   credential/generation changes explicit in its separate comparison. Validate
   raw table fingerprints before the finalizer; account separately for revoked
   credentials, expired playback and interrupted/reconciled work afterward.
4. Prove the distinct operational return to source32: use the same recovery
   point with the actual source32 restore/finalizer or another independently
   reviewed schema27 restoration path. Require `SourceVersion=27` and
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
with that same saved point; unexpected drift stops the operation before writes.
Do not silently refresh the backup baseline to accept drift or edit tasks and
retention to manufacture admission.

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
