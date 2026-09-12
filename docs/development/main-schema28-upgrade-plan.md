# Main service schema28 upgrade plan

Status: design only, 2026-09-12. This document does not authorize replay of an
existing execution directory and does not report a main-service deployment.
It was prepared from local source and recorded evidence; no live inspection,
test, build, SSH command, service action, or database operation was performed
for this plan. Existing authorization covers the necessary subsequent remote
verification and deployment; the remaining gates are concrete evidence gates.

## Outcome and release authority

Upgrade `goby-foundation-test.service` from source32/schema27 to the accepted
source55 binary and schema28, while preserving the main installation's data,
credentials, media, recovery generations, inactive recovery slot, and archives.
Keep the deployed candidate, its independent assets, the existing proxy, and
the PostgreSQL workspace on port15432 unchanged.

The product is already published as storage/scanner commit
`13b21d60c8bdc4caf8d59abdddc9e2b96dc52775` and Subviews commit
`16d75c38064008680fa60839c637efee2f12f2ae`. The accepted candidate upgrade tools
and evidence were subsequently published as
`212dc387581d9fffddfc7337bbfee1e77c2d04f4`.

Use these existing artifacts as product and implementation prerequisites:

- Source: `/opt/goby-test/exec-work-m3e/source-attempt-55`, manifest SHA256
  `7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a`.
- Binary: `/opt/goby-test/exec-work-m3e/client-backup-run-20260912_084241_db776aacc1a7/tmp/goby-linux-amd64`,
  29,337,989 bytes, SHA256
  `6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81`.
- [Full report](collection-folder-source55-full.json), SHA256
  `2c0b5a54b51f66ffa0b11f3160cf50325df520322ec79af1fd92fc9bff93596c`,
  and [independent terminal](collection-folder-source55-full-terminal.json),
  SHA256 `7d43a77d904bb212f9231bf427133eb0034a9f74a2187004e18775ccd334880c`:
  2,173 tests, 25 packages, no failures/skips, build and six cleanup checks.
  The terminal has integer test-count summaries and an integer package count;
  the hash-bound full report contains the complete test and package lists.
- Native frontend: `/opt/goby-test/exec-work-m3e/storage-binding-workflow-web-03/dist`,
  all 58 files from the [accepted frontend report](storage-binding-workflow-web-accessibility-verification.json),
  SHA256 `2b33e2805afb7422eeb8486db9c4f531cd074a636e4dcddc08c91c8d0808a9d7`.
- [Candidate upgrade report](client-schema28-accepted.json), SHA256
  `328bc6abbf4d2fe0559a34ed62d9e7d9cf7b77c075c583f9070d7aa9761c8832`,
  and its [passed independent attestation](client-schema28-accepted-attestation.json),
  SHA256 `ec20d286a1998f0b27667603819853129e88b91579a97a25141bd6258e450032`.
  The run report retains `awaiting_outer_attestation`; the separate attestation
  supplies final success. Neither document is a main database baseline.

Bind the [Git reconciliation](collection-folder-source55-git-reconciliation.json)
and [single-fixture EOL verification](collection-folder-source55-git-eol.json)
as recorded. Do not claim that all 849 committed inputs are byte-exact, change
the frozen source55 tree, or add the supplementary test to the full-suite count.

Before a service-affecting main run, require the separately completed source55
original-client acceptance, including its independent terminal and final owned
cleanup. That work is still open at this document's preparation boundary. Its
failed v1 setup and the [v2 navigation failure](client-library-changed-source55-v2-browser.json)
are not passing client evidence. V2 completed setup and login but did not prove
the target movie card; its native edit and automatic-refresh stages did not run.
Tool design, review, isolated builds, and read-only main input preparation may
proceed before that gate. Record the eventual accepted client scope by exact
path/hash, source, schema, binary, candidate process, and upgrade-attestation
chain; do not fill future receipt fields with placeholders.

## Main identity must be independently established

The provided current service identity and historical database anchors are:

| Fact | Expected main value | Required fresh check |
| --- | --- | --- |
| Service | `goby-foundation-test.service` | Unit, loaded properties, fragment and every drop-in |
| Process | PID762090, start ticks7637121 | UID/GID, boot, executable, cmdline, cgroup, listener ownership |
| Invocation | `bb94d74b475f4382a6ec6f6df181dd74` | Exact original invocation immediately before stop |
| Old binary | `/opt/goby-dev/goby`, SHA256 `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620` | Bytes, inode, owner, mode, live `/proc` executable |
| Public listener | `127.0.0.1:18096` | One listener owned by the same process |
| Main PostgreSQL | Port5432, database/role `goby_test` | Exact connection, ordinary role, cluster and active generation |
| Historical OIDs | Database16385, role16384, public2200, public owner6171 (`pg_database_owner`) | Actual OIDs, owners, raw/expanded ACLs and role properties |
| Historical cluster | System identifier `7683277964552005578`, data `/var/lib/postgresql/17/main` | Postmaster PID/start ticks/boot/start microseconds, version, socket and configuration paths |
| Historical deployment | `f58d5e0c8ff49fca916499e666bffd9f` | Local lifecycle state and exact database binding marker |

The OIDs and cluster/deployment values come from the
[schema27 main migration](m3e-main-schema27-migration.json). They are expected
identity anchors, not a replacement for current observations. The
[historical completion](m3e-main-schema27-completed.json) is a consumed
26-to27 execution. Its schema26 dump, startup deadline, population, old helper,
and rehearsal receipt cannot authorize a 27-to28 run.

Do not infer the active database from the unit PID or configured primary URL.
[`recovery.Runtime.ActiveConfig`](../../internal/recovery/runtime.go) can select
a different slot and key generation without changing the process identity.
Read and bind the current lifecycle deployment/revision/generation, database
slot, master source, and raw `goby.recovery.binding.v1` marker. This plan requires
that the active slot resolves to the expected main `goby_test` database. A
different active slot, pending activation, unknown generation, or changed OID
rejects this plan before stop; it must not cause automatic target selection.

All main SQL must explicitly select port5432 and the independently pinned
database/role. Reject port15432, `goby_client_m3e`, candidate URLs, and the
workspace owner/cluster as substitutes. Maintenance SQL is limited to the same
verified main cluster and the exact new rehearsal pair.

## Minimal implementation and reusable boundaries

Prepare two new runtime files, `upgrade-main-schema28.py` and
`migrate-main-schema28.go`, with matching Python and pure Go guards. Keep these
as a narrow main-specific adaptation; no separate generic deployment framework
or speculative disposal operator is needed. A later real failure may require
a separately reviewed, scope-specific recovery action.

| Existing implementation | Reuse | Replace for the main scope |
| --- | --- | --- |
| [Candidate controller](../../scripts/test-env/upgrade-client-schema28.py) | Strict input closure, durable journal, exclusive outputs, backup/rehearsal-before-stop ordering, one-shot service fence, safe error codes, five direct GETs, independent `attest` | Candidate constants, state publisher, `COUNTS`, private-file paths, port15432 SQL, workspace/HBA helpers, and candidate-only process checks |
| [Candidate Go helper](../../scripts/test-env/migrate-client-schema28.go) | `OpenSnapshot`/`Facts`/`Dump`, `RestoreOffline`, `captureState`, sequence/ACL checks, normalized role properties, complete state comparisons, transaction/lease/commit handling | Exact main5432 identity, fresh main profile, local lifecycle fence proof, main/rehearsal URLs and evidence namespace |
| [Candidate pure Go guards](../../scripts/test-env/migrate-client-schema28_test.go) | Real report roundtrip and comparisons that retain role, OID, ACL, sequence and other state differences | Main-specific identities and scope tests; keep explicit-file Go test/build invocations |
| [Historical main27 controller](../../scripts/test-env/upgrade-main-schema27.py) | Read the patterns in `primary_proof`, `exact_service`, `protected_state`, `media_state`, `save_materials`, `startup_plan_gate`, `create_rehearsal`, `dispose_rehearsal`, and asset publication | Its old26-to27 inputs, deadline, published dependency loader, candidate/client receipts and mutation entrypoints |
| [Historical main27 helper](../../scripts/test-env/migrate-main-schema27.go) | Main-cluster identity proof and full old-column/row-multiset preservation design | Hardcoded schema26, Extra backfill, old run/proof namespace and historical helper bytes |

Do not execute or import a historical upgrade entrypoint to obtain its helpers.
In particular, this work must not read, import, execute, copy, or modify
`scripts/test-env/upgrade-main-schema25.py`. Selected, reviewed source methods
may inform the new implementation without activating their dependency chains.
Do not inspect original Emby implementation, binaries, assets, or databases.

Build only in a new protected source55 copy containing the pinned product inputs
and the two new Go overlays. Preserve the original source manifest and bytes.
Use the accepted Go1.27.1 toolchain, offline module policy, and explicit
`main.go`/`main_test.go` arguments because the operational Go files retain
`//go:build ignore`. The build/test receipts bind every copied file and the
resulting helper binary; they do not claim that the candidate helper binary
accepts main5432 unchanged.

Carry forward all accepted candidate fixes: strict terminal summaries, explicit
master-state proof, direct owned `*os.File` dump output with a bounded same-fd
hash, and validated/compacted `RoleProperties`. Do not replace full comparison
with a hash that omits fields or compare unnormalized `json.RawMessage` bytes.

## Fresh scope, inputs, and private preservation

Reserve a new run identifier only after confirming that its tool, build,
controller unit, evidence paths, role, and database names are unused. Suggested
names are `main-schema28-source55-tool-01`,
`/opt/goby-test/backups/main-schema28-v1/run-<timestamp>-<nonce>`, and
`goby_main_s55_rehearsal_<nonce>` on port5432. No existing backup, accepted
candidate output, `goby_recovery_m5j`, or workspace backup pair is scratch space.

The frozen intent must bind product/full/terminal/frontend/publication/client
receipts, tool/guard/build bytes, main service and cluster pins, main baseline,
startup plan, private/material inventories, rehearsal identity policy, HBA
policy, and the exact temporary restart fence. Keep credentials only in private
0600 files; reports contain hashes, identities, counts and fixed error codes.
No secret goes in command arguments, raw error output, or the public journal.

Capture and preserve these main-owned materials, resolving effective paths
through the verified unit, both environment files and active lifecycle state:

| Material | Main location or rule |
| --- | --- |
| Runtime and administrator credentials | `/opt/goby-test/runtime.env`, `/opt/goby-test/browser.env` |
| Recovery environment | `/opt/goby-test/recovery-m5j.env` |
| Default application-key master/vault | `/var/lib/goby-test/application-key-vault/master.key` and its owned vault |
| Lifecycle and generation files | `/var/lib/goby-test/recovery-m5j` |
| Encrypted archives and operation journal | `/var/lib/goby-test/backups-m5j`, `/var/lib/goby-test/recovery-operations-m5j` |
| Inactive database/role | `goby_recovery_m5j`, independently identified on port5432 |
| Unit and permanent drop-ins | Main fragment, `20-application-keys.conf`, `30-observability.conf`, `40-backup-recovery.conf`, plus the complete actual inventory |
| Binary and native assets | `/opt/goby-dev/goby`, `/opt/goby-dev/admin` |
| Other existing owned state | Native pairing, diagnostics, transcode cache ownership, media roots, operator-held archive/passphrase pairs, and accepted historical execution trees |

Historical main evidence requires a 32-byte master and records its ownership as
UID995/GID995; other application stores use UID995/GID986. Recheck every actual
file identity and preserve it. Do not apply the candidate's absent-master
exception, its GID assumptions, or any candidate population counts to the main installation.
Also preserve any selected generation master and configuration, not just the
default path. Missing or inconsistent main key material rejects admission.

Copy necessary runtime, recovery, master/vault and archive recovery materials
into the new root-only operational backup without altering their originals.
Hash all original encrypted archives and preserve their matching operator-held
passphrases where already managed. Keep plaintext dumps and copied secrets
outside strict application stores. Media receives only bounded identity/hash
observation; the operator never creates, rescans, renames, deletes or rebinds it.

Bind historical executions through their already sealed manifests and receipts,
and keep every old scope outside the new mutation allowlist. Do not recursively
load historical tool source, or open the prohibited schema25 operator merely to
refresh a tree hash. A new byte-equality claim covers only explicitly allowed
artifact members that were actually read; an inherited sealed digest is labeled
as historical provenance.

Recheck the inactive recovery slot's exact OIDs, role flags, ACLs, marker,
catalog, rows/fingerprints, sequences and absence of active work. The historical
empty-slot observation is not current authority. An accepted retained slot must
remain byte/fact equivalent; do not reset, migrate, restore into, or dispose it.
Unknown state rejects admission. Preserve lifecycle revisions and raw binding
bytes in both slots; the operational rehearsal does not stamp a new application
generation or consume a recovery reset proof.

## Startup and ownership gates

Create a fresh finite startup plan from the effective source55 configuration,
database clock, activity ages, runnable triggers, tasks, scans, playback,
encoding, recovery operations, and pending store publications. Require no work
that startup will interrupt, resume, prune, or schedule during the approved
window. Recheck the deadline before stop, migration, publication, and start.
Source55 defaults activity retention to 30 days and starts pruning on a
one-minute ticker; read the actual overrides and data rather than reusing the
historical 30-day observation or its expired deadline. Do not disable tasks,
delete history, or edit retention settings to manufacture a passing gate.

Use a fresh main-operation lock and the existing main deployment lock with
verified ownership. The M5j lock is recorded at
`/opt/goby-test/exec-scratch/.m5j-deployment.lock`; verify its marker/inode and
any additional currently accepted main lock identity before choosing a fixed,
nonblocking acquisition order. Do not substitute `client-fixture.lock` or the
workspace15432 operator lock. Unknown/missing lock provenance is a read-only
preparation failure, not permission to recreate a historical lock.

While the service is live, observe lifecycle/generation files without invoking
an API that may initialize or repair them. After the sole stop and complete
drain, acquire the existing lifecycle directory and named-file flocks in the
same order as `lifecycle.Open`, against their already pinned identities, without
creating or changing any lifecycle file. The migration helper additionally
holds `database.AcquireLease` and transaction table locks. Release these owned
handles cleanly immediately before the controlled start, while retaining the
external main-operation lock. Never call `recovery.Open` just to inspect a live
deployment or use a recovery restore/finalizer as the in-place migration.

The main service's historical `Restart=on-failure` differs from the candidate.
Prepare a unique, receipted runtime drop-in under
`/run/systemd/system/goby-foundation-test.service.d/` that sets `Restart=no`.
Inspect restart-force settings and reject any unaccounted override. Publish the
new drop-in only in this run, reload unit configuration, and verify the effective
policy and unchanged original process. Preserve every permanent unit/drop-in
byte. This prevents a failed first launch from silently becoming another
attempt. After a healthy final readback, remove only the exact owned temporary
file, reload, and prove restoration of the original service policy without a
restart. On failure, retain the fence and its receipt for explicit forward
review; do not automatically restart or roll back.

Derive bounded stop/start waits from the freshly captured main configuration.
The historical main stop timeout is 90 seconds, so do not copy the candidate's
shorter stop budget. Preserve the permanent application's resource settings;
bound the new controller separately. A timeout, forced termination, or a new
unexpected service invocation remains failure even if a later process is ready.

## Execution sequence

1. **Read-only admission.** Verify all release/client gates, the fresh main
   identity, active generation, private stores, inactive slot, media, startup
   window and exact main cluster. Capture the complete global5432 catalog,
   HBA/configuration facts, and separate preservation witnesses for candidate,
   proxy and workspace services. Reserve no old scope and send no login request.
2. **Online consistent backup.** Keep the original main process running. Hold
   one read-only repeatable-read exported snapshot; obtain source27 facts, full
   state and the custom-format dump from that same snapshot. Pass the exclusive
   regular `*os.File` directly to `Snapshot.Dump`; sync, seek and hash the same
   fd with explicit bounds and identity checks. Couple the dump to the stable
   private/generation/material inventory. Partial files never count as backup.
3. **Independent rehearsal.** Create one new ordinary role/database on the
   verified main5432 cluster, with fresh OIDs and an exact ownership comment.
   Its URL cannot name the main database, recovery slot or any candidate pair.
   Use low-level `RestoreOffline` with the trusted main source URL, restoring
   source27 facts and migrating the new target to28. Validate all source data
   projections and neutral defaults; keep target ownership differences explicit.
4. **Ordinary rehearsal cleanup.** Verify catalog, complete restored contents,
   role flags, ACLs, public/cast identity, no unowned sessions/prepared work/slots,
   and no external role dependencies. If a new exact HBA rule was necessary,
   restore the original5432 bytes/owner/mode and reload once. Close admission to
   this owned target, then ordinary `DROP DATABASE` and `DROP ROLE`. No FORCE,
   CASCADE, backend termination, or existing-slot cleanup. Require the original
   global catalog and HBA to match before proceeding.
5. **Recheck before stop.** Re-read the live main full state and compare it with
   the actual saved backup state, including normalized role properties. Also
   recheck every private/generation/slot/material and startup proof. Any drift
   rejects before stop, without refreshing the baseline or retrying. Install
   and verify the owned restart fence, then repeat the final identity/baseline
   checks while allowing only that declared temporary unit-policy difference.
6. **One stop and transaction.** Durably reserve the stop before dispatch.
   Require the old PID/start/invocation, listener and service cgroup to terminate
   cleanly; do not accept forced shutdown as success. Require main sessions,
   prepared transactions and slots to drain without terminating other clients.
   Acquire the lifecycle fences and helper database lease, compare the stopped
   full state with the same backup again, and migrate27-to28 in one transaction.
   Validate preservation before commit. An uncertain commit retains the scope
   and stops the workflow; it is not retried or repaired with the old dump.
7. **Publish verified files while stopped.** Stage new native assets with root
   ownership, explicit directory0755/file0644 despite umask0077. Reuse the main27
   publication pattern: install verified assets first and `index.html` last,
   preserving old hashed assets outside the new manifest. Require the exact
   before/new union, with only declared replacements. Atomically replace only
   `/opt/goby-dev/goby` from a same-filesystem owned staging file. Keep the main
   runtime/web path and all other settings unchanged; the candidate's separate
   `/opt/goby-client-m3e/admin` is untouched.
8. **One start and direct proof.** Recheck private/generation/main/recovery
   state and the startup deadline; release owned lifecycle/database handles.
   Durably reserve one start. Require one fresh stable PID/start/invocation,
   correct executable/UID/GID/sandbox and sole listener18096; a restart or
   unexpected exit fails. Perform exactly five anonymous direct GETs:
   `/readyz`, `/emby/System/Info/Public`, `/admin/`, and the accepted index's
   unique module JS and stylesheet CSS. Require200, no Set-Cookie, the original
   main server ID, and exact accepted static bytes. No authenticated smoke or
   client business writes are included in this deployment transaction.
9. **Independent terminal.** Capture the final full main state, private and
   inactive-slot preservation, archive/media hashes, global5432 catalog and
   restored HBA/restart policy. Verify candidate/proxy/workspace unchanged and
   no leftover helper processes. The controller publishes only
   `awaiting_outer_attestation`. A separate invocation verifies the exact
   controller's exit0/MainPID0/original invocation/empty recursive cgroup,
   final main process and all recorded identities before publishing `passed`.
   Use a fresh bounded controller with `RemainAfterExit=yes` so its terminal
   metadata remains observable; default/not-found unit properties are not proof.

The HBA policy must be decided from fresh main configuration: preserve existing
rules if they already admit the exact new SCRAM target; otherwise the frozen
intent names one temporary database/role/loopback rule and its exact restoration.
Resolve `hba_file`, config file and socket from the verified5432 server rather
than borrowing workspace paths. Reserve restoration before its first write;
a failed restoration is not automatically attempted again.

## Preservation and completion criteria

The main migration preserves every existing column/value of all 35 schema27
tables, including generated values, metadata controls, policies, authentication,
playback, Extra/Theme state and recovery binding. It preserves all five sequence
values and in-place physical sequence facts, relation OIDs/owners, raw and
expanded ACLs, role properties, and unrelated catalog objects/constraints.
Schema27 catalog SHA256 is
`1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d`;
schema28 is `8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b`.

Allowed database changes are exactly migration28's history row, four new
`library_roots` columns and their constraints, two new `activity_entries`
columns, and the published replacements of three audit CHECKs. Every old root
starts with `binding_revision=1` and NULL `storage_binding`, `bound_at`,
`bound_by`; old audits have `previous_revision=0` and an empty observation
fingerprint. Preserve all literal main root mappings. No filesystem identity,
binding approval, administrator action, media scan, or missing-item deletion is
inferred by the upgrade.

Rehearsal comparison may account only for its newly created database/role/OIDs,
explicit owner-only database ACL and restored sequence WAL bookkeeping; it must
not erase those fields from the in-place main comparison. Preserve nullable
and typed role fields using the accepted normalization. Revalidate both current
state and archive limits; do not copy candidate row counts as main admission.

The startup gate makes full old-state preservation the expected post-start
result as well. Any task, retention, authentication, generation or store delta
outside the declared upgrade prevents a passing deployment. Diagnostics may
append only under a separately bounded, recorded log rule; existing diagnostic
files and private stores must not be silently discarded. The historical main27
smoke explicitly added authentication/audit history, so it cannot supply a
post-smoke whole-table-equality precedent for this new five-GET workflow.

Retain the new schema27 dump, matching private materials, source32 binary and
old asset inventory as recovery evidence. A rehearsal automatically upgraded to
schema28 is not a source32-startable schema27 rollback database. No automatic
restore, lifecycle rollback, old-binary restart, receipt adoption or mutation
retry is part of this operator.

After a passed main attestation, publish a new main after-state authority for
future acceptance. A later main-client scope must use its own main users,
server ID, library/item targets, credentials/session cleanup and direct18096
address or a separately owned route; it cannot use candidate snapshots or
reconfigure the existing candidate proxy. Hash-bind any legitimate new login,
device, audit or metadata changes to that later scope. Keep deployment success,
candidate original-client acceptance and any later main-client acceptance as
separate, explicitly linked results.

## Remaining preparation work

- Seal fresh main service/cluster/active-generation/private/inactive-slot/full
  database/startup/HBA/lock observations without mutation. Record actual main
  counts and material hashes; do not extrapolate historical or candidate values.
- Finish the separate source55 client gate and bind its actual passing terminal.
- Implement/review the narrow main operator and helper, then run remote pure
  guards, explicit Go guards/build and complete read-only input preflight.
  Cover wrong5432/15432 targets, recovery-slot substitution, populated keys with
  missing master, stale generation/deadline, drift before/after stop, restart
  fence/permission failures, owned-file dump output, JSON report roundtrip,
  HBA restoration failure, ordinary cleanup and independent terminal admission.
- Freeze the concrete intent and new scope only after those checks. Report any
  failure at its durable phase with fixed safe codes and retained evidence;
  never relabel a failed or historical scope as a successful main upgrade.
