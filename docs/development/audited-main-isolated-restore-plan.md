# Isolated schema27 recovery and installation rollback

Status: **INITIAL SELECTED RESTORE AND OFFLINE ACTIVATION COMPLETE; SERVE,
ONLINE ROLLBACK AND SOURCE32 PROOF OPEN**, 2026-09-14. The first infrastructure execution,
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-isolated-restore-01/infrastructure-execution.json`, SHA256
`c0bbc8730f7548d159840ed2f19316f772a8223c3f0b8577d7f66712ab713f10`,
succeeded. All three profile isolation checks passed; one fresh PG maintenance
identity read completed and its backend exited naturally. That increment created
no application database. The
[infrastructure checkpoint](isolated-restore-infrastructure.json) records this
boundary. The subsequent [target/material checkpoint](isolated-restore-targets.json),
remote `target-preparation/execution.json` SHA256
`38391c8fe5120138dbd9dcdd3801bba2b9300c212e6c3899188d5a13f6bab395`,
created all four empty ordinary-role targets, prepared 474 installation files
and copied private archive/passphrase inputs under their required ownership.
All five SQL backends exited naturally; protected host state remained exact.
Those preparation inputs are consumed and must not be repeated. The subsequent
[selected initial activation](isolated-selected-initial-activation.json) completed
one import, one ready native plan and one offline apply. The private source405
and staged406 fingerprints matched all 35 tables, with only the declared
normalization/migration/binding changes. Apply preserved all 406 staged rows and
added exactly one `restore.applied` audit: B now has407 rows and is selected at
lifecycle revision1, generation `f20a5b87bf8e9ca873c8889db6566433`.
Its CLI, observers and owned process groups are closed. No serve invocation or
HTTP login has run. Preserve these completed steps; online A activation, real
rollback to B, restarts and the independent source32 proof remain required.

The [completed source32 backup](audited-main-native-backup-completed.json)
supplies archive `69f5e597c99f65099647cd3322d3174c`, 196,310 bytes, SHA256
`0a61cbd6c6ff23543ba873ed1a44dd33ecce2f8702f956e24096862996c7874f`.
It contains schema27, 35 tables and 405 snapshot rows. Completion checkpoint
`3a9ff1ddf45213331aa4f23d66d1beedd44da9f18df8e168e9e56badc6a925cf`
reconciles the separate 408-row post-workflow state and policy cleanup. Native
source-key authentication passed. The selected native plan now also proves
archive-passphrase decryption and restoration validation, with generation master
and configuration descriptors matching the archive. Actual source32 restoration
and restored-account login remain separate uncompleted proofs.

The original archive, its original private passphrase and the old installation
tar remain on persistent storage in their existing pinned scopes. The old tar
is 16,590,013 bytes, SHA256
`f24ad82c66d7a8a1ba2128ed547210a95804f8cd62448ba5ffdb2a14fcac66fb`.
Root stages verified working copies without moving or modifying those originals.
The phase comparisons follow the [data contract](isolated-restore-data-contract.md),
which distinguishes raw archive facts, normalization, native retained facts,
authentication, audit destinations and all five sequence values.

## Fixed topology

Use only `Root=/opt/goby-restore-69f5e597`. Root manages the infrastructure and
installation; application and PostgreSQL processes have separate identities.
There is no shared-host PostgreSQL route or inherited deployment environment.

| Resource | Fixed selection |
| --- | --- |
| Network anchor | `goby-restore-69f5e597-anchor.service`, root-owned independent `PrivateNetwork=yes` unit |
| PostgreSQL | PostgreSQL17, unit `goby-restore-69f5e597-postgres.service`, OS user/group `goby-r69pg`, UID/GID55241 |
| PostgreSQL storage | Root-managed 768 MiB tmpfs mounted at `Root/postgres-volume`; data at `postgres-volume/data`, Unix socket at `postgres-volume/socket` |
| PostgreSQL endpoint | `127.0.0.1:25441` inside the anchor namespace only |
| Application identity | OS user/group `goby-r69app`, UID/GID55242, for both serial application phases and their CLI/HTTP probes |
| Shared installation | `Root/install/goby` and `Root/install/admin`, root-managed and read-only to application processes |
| Selected application | `Root/app-selected`, unit `goby-restore-69f5e597-selected.service`, listen/public URL `127.0.0.1:18241` / `http://127.0.0.1:18241` |
| Source32 application | `Root/app-source32`, unit `goby-restore-69f5e597-source32.service`, listen/public URL `127.0.0.1:18242` / `http://127.0.0.1:18242` |
| Controller and evidence | `Root/controller`, root-owned mode0700, persistent and inaccessible to both application sandboxes |
| Media | The approved `/opt/goby-fixtures` layout, bound read-only with its archived absolute path identities |

The four physical slots are separate databases and ordinary owner roles in this
new cluster. Their names are fixed now; their actual OIDs are recorded after
creation, never invented as admission inputs.

| Slot | Database | Owner role | Application configuration |
| --- | --- | --- | --- |
| A | `goby_r69_a` | `goby_r69_a` | Selected `GOBY_DATABASE_URL` |
| B | `goby_r69_b` | `goby_r69_b` | Selected `GOBY_RECOVERY_DATABASE_URL` |
| C | `goby_r69_c` | `goby_r69_c` | Source32 `GOBY_DATABASE_URL` |
| D | `goby_r69_d` | `goby_r69_d` | Source32 `GOBY_RECOVERY_DATABASE_URL` |

Cluster system identifier is `7685171266320400886`. A/B/C/D database OIDs are
16392/16393/16394/16395 and owner-role OIDs are16388/16389/16390/16391. At construction
each had public schema2200 owned through `pg_database_owner`, zero application
relations, routines and types, and only `plpgsql`. Database ACLs grant access only to the
respective owner; roles have no administrative flags or memberships. These are
measured preparation facts, to be rebound before native access.

The root-managed mount and its UID/GID55241 data/socket directories, mode0700,
must exist before any PG process. The recorded mount must outlive every PG
stop/restart. Do not use a unit-local temporary filesystem that recreates the
database on restart. Keep normal
PostgreSQL durability settings, including fsync and full-page writes. The
768 MiB limit bounds only this PostgreSQL volume; it does not prove sufficient
space, RAM or capacity for four restored databases, indexes, WAL, temporary
files, archive/decryption scratch or evidence. Measure and budget these before
admission and at phase boundaries. Stop on insufficient capacity; do not delete
retained slots or enlarge/reset the fixture to manufacture success.

All PG, initdb, application, CLI and connecting probe processes join the same
anchor namespace. Root verifies the anchor's live PID/start/invocation and
namespace inode before every dependent start; `BindsTo` alone is insufficient.
The anchor stays alive across PG restarts. PG lifetime must not own the namespace.
Unexpected anchor loss stops the dependent scope without automatic replacement.

Use the fixed profiles in `.git/isolated-restore-units.py`, with the admitted
source bytes pinned. They separate the app roots, hide the PG volume and
controller from apps, and hide both apps, installation and media from PG. They
deny existing main, candidate, source55 and host PostgreSQL paths; private
loopback cannot reach host listeners. The root coordinator stays outside the
application sandbox, but every HTTP/SQL child it launches joins the same pinned
namespace. No fixture process uses a host PostgreSQL socket or connection URL.

Both app roots, `install/` and `controller/` must exist before dependent units so
required inaccessible paths exist. App roots and their private input copies are
owned by UID/GID55242, with app/private directories mode0700. Root-owned manager
environment files are `controller/selected.env` and `controller/source32.env`; actors cannot read
the controller directory. Archive/passphrase copies needed by a CLI belong in
that CLI's `app-*/private`, with exact bytes, owner and mode0600. No secret enters
arguments, safe reports or public logs.

Each app has fresh, nonoverlapping lifecycle, operation, backup, default-master,
cache and diagnostic paths under its own root. Configure these paths explicitly,
along with `GOBY_WEB_DIR=Root/install/admin`, tool paths, logical defaults and
backup/cache/log limits. Do not source main's environment or copy its lifecycle
stores or live master. Native restore creates the local binding and recovered
generation master. The slot URLs remain fixed throughout each phase;
`ActiveConfig` selects the active generation without URL swapping or fallback.

## Admission and construction

Admission has two stages: bind the construction scope first, then admit the
actual empty targets. Target OIDs do not exist before construction. Each
execution phase uses this recorded authority and its concrete phase receipts.

1. **Infrastructure admission — executed:** the recorded scope bound the fixed
   accounts, directories, mount, anchor, PG17 cluster, namespace/profile fences,
   source/configuration pins and finite budgets. Root started the independent
   anchor, ran initdb as UID55241 in that namespace and started its own postmaster.
   Its empty maintenance-cluster identity and three profile isolation checks
   are recorded; anchor, PG and mount remain owned infrastructure. This did not
   admit application database creation or native restore. Later work rechecks
   their recorded identities and remaining resources without recreating them.
2. **Target construction and empty-target observation — executed:** the bounded
   scope created A/B/C/D and their ordinary owner roles under the isolated
   cluster authority. Unknown target OIDs were not prerequisites for creation.
   The checkpoint captured the actual
   cluster system identifier, PG/tool versions, postmaster/namespace identity,
   database/public OIDs, owners, ACLs, role flags and empty-target observations.
   Each target is UTF8 with an owner-controlled public schema and no non-system
   relations, routines, types or extensions other than `plpgsql`; no foreign
   connection or prepared transaction may exist. Bind the native operation's
   actual input and fresh continuity before plan access. Keep C/D empty and recheck this
   boundary when the source32 phase is reached. The native target validator and lease
   remain mandatory and do not replace these external identity bindings.
   Application roles use private authentication, have no administrative
   capabilities or membership in privileged/source roles, and own only their
   declared database. PG administration uses only this cluster's bootstrap
   authority. Freeze private input handling, actual capacity and finite
   operation/disposal bounds in this target scope; there is no inherited
   permission to run a native mutation merely because PG is available.

Preparation copied the selected installation's 58 files (30,667,532 bytes) into
`install`, and staged source32's 416 files (40,869,739 bytes) privately under
`controller/old-installation`. Every copied file matched the pinned source by
size and SHA256; source originals were preserved. Both private environments and
archive/passphrase copies are ready, and both default master paths remain absent.
Actual installation replacement is still pending. At this checkpoint root free
space was379,777,024 bytes and the PG tmpfs had739,315,712 bytes free; these are
observations, not permanent reservations or a claim that every phase will fit.

Use the template ceilings: anchor150min, PG120min per invocation, app45min,
offline CLI35min and probes120s, with their stop and memory limits. The
coordinator freezes a non-resetting overall deadline within the anchor lifetime
and reserves time for app/PG shutdown and final disposal. Each command receives
only its remaining phase budget; a process restart creates no new experiment
budget. CLI/probe profiles clear inherited commands and sinks: add one fixed
reviewed command and bounded private stdout/stderr capture before publication.
Bind stop authority to each submitted start before environment/readiness checks.

## Selected proof: B revision1, A revision2, B revision3

Install selected binary
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`
and its exact 57 administrator assets at the shared installation paths. Keep
source32 stopped. Selected A/B and `app-selected` begin as the admitted fresh
resources; both slots are real databases.

1. **Offline restore into B.** As UID55242 under the selected CLI profile, run
   native `recovery status`, import the pinned encrypted archive once and use
   its returned local BackupId. Status opens stores and is an admitted write,
   not a read-only probe. Require import completion and exact archive bytes.
   Run `restore plan` with that BackupId/SHA256, one fixed request ID, the actual
   `GenerationRevision="0"` and the private passphrase file. Preserve exact UTF8
   passphrase bytes, including any newline. Do not request replacement of a retained copy.
   Require actual ready state, successful decryption/key recovery, target B
   schema28 and the data contract's normalization/migration/binding results.
   Record the returned operation ID/revision and target lease closure.
   **Completed:** local BackupId `c2aea0de5ee83bf805ed27526ad1af97`, plan
   `2e5da2c5d865ab5a2f5aa018eb25fad8`, ready revision4. Independent comparison
   matched all source/staged table fingerprints, all unlisted row fields and
   the five expected sequence observations. Its original wrapper-level
   comparison failure is retained; no import or restore plan was repeated.
2. **Offline apply and serve B.** Apply that operation with the returned
   revisions and `--accept-no-rollback`; A is still empty and is not being
   certified as a rollback image by this initial step. Require the native
   application acceptance and lifecycle revision1 selecting B. Apply itself
   initializes the app, binds its listener and starts task management before
   closing; record those effects and close every CLI child/listener. Its
   `Status=completed`, matching `OperationId`, `GenerationRevision` and
   `StartService=true` do not prove a daemon is running. Start the selected
   serve unit separately and prove B's actual generation/master, schema28,
   readiness, restored server/data identity and selected assets.
   **Offline apply completed; serve pending:** activation receipt SHA256
   `be91d5197076e0a1dc89cd4e6102a5b07aca97f1b11e45c7a12de6c552fac5fc`
   confirms B/revision1, matched generation master/config descriptors, A
   unclaimed, B active, no pending journal, 406 old rows exact and one complete
   system audit. Activity sequence advanced1; the other four remained exact.
   The initial cache marker/lock now exist; selected logs remain empty.
3. **Online plan into A.** Authenticate an owned restored administrator on B.
   Use `POST /admin/v1/restores/plans` with the already imported archive ID,
   fixed SHA256, a new fixed RequestId, `RestoreDefaults=false`,
   `ReplaceRollback=false` and the current `GenerationRevision="1"`. Transfer the
   passphrase privately through the native request, never through arguments.
   Keep A empty until its native lease is taken. Require a ready plan that
   independently restores the same archive into A and produces schema28.
   Reconcile B's plan audits separately from A's restoration changes.
4. **Online apply B to A.** Submit `POST /admin/v1/restores/{id}/apply` with the
   returned operation Revision and `GenerationRevision="1"`. Require completed
   activation at lifecycle revision2 selecting A and a usable native retained
   B image. After B ingress and writers drain, capture its complete native
   retained facts, raw marker, key/generation bindings and all five sequence
   values. These include B's actual login/plan/apply-request history. Freeze
   this image until rollback; do not log out into inactive B, run its startup
   or refresh its baseline to accept drift. Confirm the active service is A
   and authenticate a new owned A administrator for its reads and rollback.
5. **Online rollback A to B.** Submit `POST /admin/v1/restores/rollback` with one
   fixed RequestId and `GenerationRevision="2"` through A's native authentication.
   Require validation of the retained B image before mutation, the declared
   normalization/rebinding and actual accepted rollback at revision3 selecting
   B. This is native rollback, not another restore plan or an offline
   `--accept-no-rollback` substitute. Preserve retiring A and its captured facts.
   B's return generation is the native retained BeforeImage generation; it
   differs from its former active generation. Compare its recorded identity.
6. **Restart with the same mount.** Finish current-B owned authentication and
   logout/401 checks, then normally stop/drain the selected app. Shut down only
   the pinned fixture postmaster with the template's ordinary fast shutdown;
   confirm PG processes, sockets and listeners are gone. Leave the anchor and
   the exact postgres-volume mount intact. Restart PG with the same data,
   configuration and cluster identifier, then restart the selected app with
   unchanged slot URLs/stores. Prove revision3, returned B generation/master,
   schema28 and readable restored data persist; close any declared new login
   and stop/drain the app again. Record A/B before/after rows and sequences.

Every mutation uses actual preceding IDs and revisions and has one admitted
request slot. Revision and GenerationRevision are decimal strings; compare
returned values to the expected 0→1→2→3 sequence, without substituting expected
values for missing output. Poll only the recorded operation. An uncertain HTTP
result does not authorize another plan/apply or rollback. Keep each transition,
service terminal and state comparison in its own phase receipt.

## Actual installation rollback and source32 C/D proof

With selected app/CLI/probes stopped and their ownership sealed, root changes
the same `install/goby` and `install/admin` paths from the selected installation
to source32. Use the preserved tar to restore executable
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`
and all 415 old administrator files, including retained asset versions. Record
the exact selected inventory before replacement, old inventory afterward,
bytes/hash/modes/ownership and final published executable. Do not merely select
another executable path or combine the old binary with the 57 selected assets.
Original source archive, passphrase, tar and sealed selected evidence remain
on persistent storage.

Recheck C/D and their separately created owner roles against the recorded empty-
target admission, and use entirely fresh `app-source32` stores/configuration.
Keep A/B and `app-selected` retained and unchanged until final disposal.
Source32 must never connect to A/B or any schema28 database.

Run source32 offline status/import/plan/apply into D using the same original
archive SHA256 and passphrase, its returned local BackupId and new operation
request IDs. C begins as the real empty primary. Initial offline apply uses
`--accept-no-rollback` and must produce revision1 selecting D. Require source27
and actual target27, raw archive authentication, the source32 data-contract
normalization and legal generation/master binding. Close apply's temporary
app/listener and all CLI children before serving D on port18242.

Start the source32 serve unit from the replaced shared installation, prove its
old binary and all 415 assets, authenticate a restored administrator for the
declared reads, then logout and prove same-credential401. Stop/drain the app,
normally stop/restart PG while retaining the same mount and anchor, and restart
the source32 app with the same C/D URLs and native stores. Prove target schema27,
revision1, the same activated D generation/master and readable data again;
close any new owned authentication and stop/drain source32.

Successful completion proves the actual installation return to source32 plus
an independently restored compatible database. It is not a down-migration of
A/B. The selected online rollback proof remains a distinct requirement and is not replaced by
source32's initial offline apply.

## Data, credentials and phase closure

Use the [data comparison contract](isolated-restore-data-contract.md) for every
staged, active and retained boundary. Native archive/table fingerprints are not
equivalent to JSON re-encoding by a helper. Preserve unlisted fields and rows,
exact marker bytes, key/generation files and separate sequence observations.
Bind SQL clock windows and every new audit/session to its actual active database
and committed operation. Archive Source.SchemaVersion27 does not identify the
current target schema. Startup, retention and activation effects must be
declared and compared rather than removed from the data.

The A session authorizing rollback can remain unrevoked in inactive retained A.
Record its exact private row and responsibility; do not issue a SQL update or
start A merely to revoke it. A 401 for that credential at B or D establishes
only rejection by the active service, not database revocation in A. Log out
and prove rejection for all newly owned credentials accessible through the
currently active B or D service. Preserve A's retained image through the
source32 phase, with its credential disposal still explicitly pending.

At final closure, stop all app, CLI, probe and fixture PG processes and their
children; prove exact PID/invocation, cgroup and listener absence. Require PG's
normal shutdown result. Root then performs ordinary unmount and disposal of
the exclusively owned postgres-volume cluster, including A/B/C/D and roles.
This ends the inactive A credential's responsibility by disposal, not by a
fabricated revoked_at value. Service stop or a cross-slot401 alone is insufficient.
Seal comparison/phase/disposal receipts on persistent storage before removing
temporary stores; retain the original archive, passphrase, old tar and both
proofs afterward. Stop the independent namespace anchor last and restore only
the explicitly owned fixture infrastructure according to its admitted disposal.

No FORCE/CASCADE database cleanup, foreign-backend termination, lazy/forced
unmount or fixture reset is part of this route. A target, schema, key, marker,
write, deadline, exit or cleanup mismatch stops the affected phase and retains
its evidence and ownership responsibility. Do not auto-retry, create another
archive, add rollback cycles to manufacture revocation, or reclassify forced
termination as normal shutdown.

The same-mount PG/app restarts prove process-restart continuity only. They do
not prove host reboot, power-loss or persistent tmpfs data; the M2 durability
gate remains open. Separate closeouts must identify the selected native rollback,
actual installation replacement/source32 recovery, final credential/resource
disposal and protected main/candidate boundaries. The selected initial restore
and offline activation are complete; its remaining online/restart proof and the
source32 proof are uncompleted. Core video acceptance, main promotion and complete M2–M6 remain
subject to the [umbrella contract](audited-main-upgrade-plan.md) and
[current execution plan](../planning/current-execution-plan.md). All eventual
execution and verification is confined to `ssh test-env`; this document runs none.
