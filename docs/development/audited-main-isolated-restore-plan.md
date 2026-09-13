# Audited main isolated restore plan

Status: **DRAFT / NOT ADMITTED**, prepared on 2026-09-14. This document records
source-reviewed execution contracts. It creates no fixture, runs no command,
and reports no successful restoration, activation or restart. The
[completed source32 backup](audited-main-native-backup-completed.json) now supplies
the common archive and main-workflow closeout. Fixture resource, ownership and
execution admission remain pending.

The common archive is `69f5e597c99f65099647cd3322d3174c`, 196,310 bytes, SHA256
`0a61cbd6c6ff23543ba873ed1a44dd33ecce2f8702f956e24096862996c7874f`,
schema27 with 405 snapshot rows across 35 tables. It is retained privately under
the backup's `after-ownership-review/private` directory. Its original private
passphrase is retained in the parent experiment; no value is copied here.
Native source-key authentication is proved, while decryption/restore validation
of that archive remains part of these forthcoming proofs.

The [resource observation](audited-main-restore-resources.json) found two unused
database/role identities and ports18241/18242, with no fixture creation or
reservation. The root/PostgreSQL volume had 464,678,912 free bytes, while tmpfs
had about 5.83 GiB available. Sharing the existing PostgreSQL17 cluster is valid
for distinct fresh target databases, but a provisional 528 MiB combined allowance
exceeds the observed root free space. That allowance is not an established
growth bound; target/index/WAL sizing and the final layout must be resolved
before admission. `max_wal_size=128MB` is a soft target, not a hard storage cap.

This is the isolated-recovery step in the
[current execution plan](../planning/current-execution-plan.md), under the
[main recovery contract](audited-main-upgrade-plan.md). It does not admit main
promotion or change the open video-client acceptance gate. Reuse the selected
product verification; no additional full product suite is required merely to
prepare these fixtures.

## Two independent proofs from one archive

| Fixture | Executable | Required restored schema | Purpose |
| --- | --- | --- | --- |
| Source32 | Actual source32 executable, SHA256 `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`, source commit `b9bb7b1cf11e07e011a6e1726ddb9d53ef8e1fe9` | Source 27, target 27 | Prove that the fresh recovery point can run under the actual old executable and matching old administrator assets |
| Selected | Selected executable, SHA256 `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42` | Source 27, target 28 | Prove authenticated restoration followed by the selected executable's embedded migration28 and normal activation |

Both fixtures consume the identical encrypted archive SHA256 and passphrase.
Neither consumes the other's restored database or stores. The schema28 result
must never be presented as a source32-compatible database. There is no supported
down-migration, and replacing its executable with source32 would not prove
rollback. Follow the umbrella plan's selected-fixture proof, then its separate
source32 proof. Serialize the resource-intensive rehearsals and close each
owned scope before the next admitted phase.

## Evidence required before fixture admission

| Required input | Concrete evidence still to bind |
| --- | --- |
| New archive | Completed source32 backup operation/catalog publication, full downloaded file path, SHA256, byte length and file identity; archive SourceFacts for schema27 and all 35 tables; matching native key witness and configuration/master provenance from that backup snapshot |
| Main backup closure | Exact post-snapshot session/activity/sequence and private-file reconciliation; completed download and owned logout; main invocation stopped and drained; temporary policy and protected external state closed. An accepted request or a partial file is insufficient |
| Private recovery material | Exact private passphrase descriptor and custody, readable archive copy, and verified old installation bundle. No passphrase or master bytes enter arguments, safe reports or public logs. The archive's recovered generation master is established through the native plan, not by copying the live master file |
| Capacity | Fresh filesystem free bytes/inodes, memory and process budget for the selected isolated PostgreSQL layout; bounds for target rows/indexes, WAL, temporary space, encrypted import and decryption scratch, evidence and retained first-fixture output. Check the configured store admission/reservation limits as well as archive size. The earlier main free-space observation is not fixture admission |
| Database and role identities | Exact fresh target names, private URI descriptors, cluster/system identity, PostgreSQL/tool versions, port/socket, database/public OIDs and owners, ACLs and ordinary-role flags; empty-target and foreign-session checks. Existing main, candidate, workspace and recovery databases are excluded |
| Paths and ports | Canonical new scope, PostgreSQL data/socket/log paths, application stores/cache/log/master paths, archive/passphrase copies, unit names and network namespace; owner UID/GID, modes, no symlink/overlap, initial absence, and unused listeners. Assign actual values in the frozen input only after these checks |
| Executable and assets | Exact consumed binary, source/manifest and administrator asset pins for each fixture, with existing verification receipts. Pin `pg_dump`, `pg_restore`, FFmpeg and FFprobe paths and versions; do not resolve them through inherited deployment defaults |
| Finite execution | One import and one plan request ID per fixture, phase/total deadlines, request and disk-output bounds, restart policy, exact owned stop/cleanup responsibility and independent closeout. No automatic business retry, restore retry or restart loop |

Freeze these inputs and obtain independent review before provisioning or
executing either fixture. Do not fill unknown hashes, free-space values, ports
or successful-state fields with placeholders that an executor could accept.

## Isolation and configuration

Each fixture needs two explicit, different configuration slot identities:
`GOBY_DATABASE_URL` for primary and `GOBY_RECOVERY_DATABASE_URL` for recovery.
Database names and role names must differ, even across hosts. The offline CLI
only parses the configured original identity; it does not connect to it or
open its original application-key vault. Thus the minimum is one actual fresh
recovery database and ordinary role per fixture, plus a separately declared
isolated primary identity. An unreachable primary is valid for this offline
proof. Creating a second empty database/role is optional when the admitted
proof explicitly observes both slots; it must not reuse main as the primary.

The target must be PostgreSQL 17 with UTF8, an existing owner-controlled public
schema, and no non-system relations, routines or types or extensions other
than `plpgsql`. Its role must not have administrative capabilities or inherit
the source role or an administrative role. The native target validator and
lease remain mandatory after external admission.

Use a clean, explicitly constructed environment for every CLI invocation and
subsequent service start. Preserve the same two slot URLs throughout the
fixture; do not swap environment values after activation. Freeze at least:

- `GOBY_DATABASE_URL`, `GOBY_RECOVERY_DATABASE_URL`, `GOBY_LISTEN` and
  `GOBY_PUBLIC_URL`, with fixture-only loopback/network boundaries.
- Separate `GOBY_RECOVERY_STATE_DIR`, `GOBY_RECOVERY_OPERATIONS_DIR`,
  `GOBY_BACKUP_DIR`, `GOBY_API_KEY_MASTER_KEY_FILE`, `GOBY_TRANSCODE_CACHE`
  and `GOBY_LOG_DIR`. Use fresh native stores and their own locks/markers;
  private store paths must not overlap one another or media/cache/log paths.
- `GOBY_WEB_DIR`, tool paths, backup/cache/log capacity settings, software
  hardware profile, startup/operation timeouts, cookie/public-origin policy
  and declared logical defaults. No environment file from main is sourced.
- Exact `GOBY_MEDIA_ROOTS` approved for this archive and the fixture's read-only
  media namespace. The archived allowed/full/relative path identities are
  validated; restoration does not remap them to arbitrary new paths.

Media files are absent from the archive. Preserve original approved absolute
path identities through an isolated read-only mount layout, or declare an
unavailable mount where the restoration contract allows it. Neither option
permits scans or writes against main's media. New schema28 root fields do not
constitute storage-binding approval.

Do not copy main's lifecycle/control directory into a fresh path and treat it
as valid ownership. Native store formats also bind device/inode identities.
Fresh `recovery.Open` creates local ownership; the plan stages a generation
and atomically stamps its new deployment/generation/slot marker into the
restored database. The archive's old database marker is not authority for a
new fixture. `ActiveConfig` later selects the activated recovery slot and
generation master without falling back to primary.

## Actual CLI sequence

The following is an invocation template, not an admitted command file. `BIN`
means the fixture's pinned executable, always run as its recorded deployment
owner under the frozen fixture environment. The service remains stopped until
the explicit serve phase.

```text
BIN recovery status
BIN backup import --file ARCHIVE --request-id IMPORT_REQUEST
BIN backup list
BIN restore plan --backup-id IMPORTED_BACKUP_ID --sha256 ARCHIVE_SHA256
    --request-id PLAN_REQUEST --generation-revision GENERATION_REVISION
    --passphrase-file PRIVATE_PASSPHRASE_FILE
BIN restore apply --id PLAN_ID --revision PLAN_REVISION
    --generation-revision GENERATION_REVISION --accept-no-rollback
BIN serve
```

1. `recovery status` opens native stores and is part of the admitted write
   scope, not a read-only preflight. Record the actual generation revision.
   Initial fresh state is expected to select primary/revision0/default, but
   derive the next command's revision from the actual accepted output.
2. Import creates a new local `BackupId`; use that returned ID rather than the
   embedded archive ID or main's store ID. Require operation `State=completed`
   and the local ready object's exact SHA256/size. Import alone does not prove
   decrypted archive validity; `Verified` is established by successful plan
   validation. Exit code zero alone is insufficient for the operation state.
3. Plan waits for a ready operation. Require `Kind=restore`, `State=ready`,
   `Phase=ready`, empty error code and matching input identities. Save the
   returned `Id`, `Revision` and `GenerationRevision`. The passphrase file is
   an owner-readable regular file, mode0600, one link, with no final symlink;
   its exact UTF8 bytes are used and trailing newlines are not stripped.
4. Before apply, independently close staging: actual target schema, migrations,
   normalized data, lease release and local database/generation binding must
   match the declared fixture. Use no `--replace-rollback` on these fresh
   targets, and do not alter the staged target to make activation pass.
5. Apply includes lifecycle publication, target application initialization and
   acceptance. There is no separate `activate` command. It calls the normal
   application constructor, temporarily binds `GOBY_LISTEN`, starts the task
   manager and activity retention, then closes that generation before CLI
   exit. Account for these writers and finite cleanup even without HTTP use.
6. Require the apply JSON's actual capitalized fields: `Status=completed`,
   matching `OperationId`, the new `GenerationRevision` and `StartService=true`.
   The latter is an instruction to start a service, not evidence of a running
   daemon. Require the CLI process and all children/listeners closed first.
7. Start exactly the fixture's own finite service with `serve`; verify binary,
   process/invocation, listener, readiness, retained server/data identity and
   the matching administrator assets. Authenticate a restored account using
   a new owned native session for the declared reads, then logout and prove
   rejection of that same credential. Bound and reconcile its session/audit
   deltas separately from restoration.
8. Stop and drain that unit, then perform the one declared restart with the
   identical environment and stores. Prove the same activated generation,
   recovery slot, recovered master selection, schema and readable data. Close
   any separately declared authenticated session and stop/drain the unit again.

`recovery jobs` and `recovery status` inspect persisted operation state after
an uncertain outcome. `recovery resume` exists for an actual pending switch;
it is not an automatic retry instruction. `restore rollback` requires a
separately proved retained generation. Offline `--accept-no-rollback` preserves
the original slot without certifying a rollback image, so these fresh fixtures
must not claim that native rollback or installation downgrade was exercised.
Any additional rollback exercise required by the umbrella contract remains a
separate explicit obligation; it is not waived by this narrower increment.

## Independent preservation and completion

The native restore pipeline authenticates the schema27 migration/catalog,
table fingerprints, sequence bounds and server ID before upgrading or running
its trusted finalizer. It rebuilds trusted embedded DDL; archive SQL is not
executed directly. The source32 executable must finish at schema27. The selected
executable must finish at schema28, including exactly migration28's neutral
root/audit additions and the expected trusted catalog.

The CLI operation's `Source.SchemaVersion` describes the archive, so it remains
27 in both proofs. It is not evidence of the target's current schema. Obtain
the target migration/catalog and durable-state evidence independently, using
only the newly owned target and bounded read-only SQL after each writer closes.

Separate raw archive equality from the legitimate finalizer changes: validate
the recovered application-key witness, preserve user/password/policy/key and
device history, revoke previously unrevoked credentials of every kind, expire
Prepared/Playing/Paused sessions, interrupt queued/running encoding work, and
account for scan/task reconciliation. The finalizer stamps the new local
binding in the same restore transaction. Later task normalization and apply or
serve startup may add their own declared changes; do not discard those fields
from comparison or assert whole-database equality with raw archive rows.

Each proof needs its own safe closeout binding the common archive, executable,
fixture input, import/plan/apply outputs, target schema/data, generation/master
selection, service starts and restart, owned authentication cleanup, process
and listener closure, and protected external boundaries. Preserve private raw
evidence and failed receipts. The two completed proofs and usable original
schema27 bundle are distinct deliverables; neither is main promotion, complete
core-client acceptance, root-binding approval or full M2-M6 completion.

Stop on an unexpected target, hash, schema, write, deadline, exit, uncertain
commit/publication or incomplete cleanup. Retain the exact scope and determine
its durable phase before another action. Never reset a failed stage, terminate
foreign backends, use FORCE/CASCADE disposal or silently refresh its baseline.
Cleanup may remove only resources explicitly owned and covered by the admitted
disposal step; retain required evidence and the usable source archive. All
eventual execution and verification run through `ssh test-env`.

## Source-reviewed entry points

- [CLI dispatch and activation](../../cmd/goby/recovery_cli.go), with
  [Linux private-file handling](../../cmd/goby/recovery_cli_linux.go).
- [Offline manager authority](../../internal/recovery/operator.go),
  [plan/staging](../../internal/recovery/plans.go),
  [activation journal](../../internal/recovery/transition.go) and
  [active slot configuration](../../internal/recovery/runtime.go).
- [Offline source-identity boundary](../../internal/backuppg/offline.go),
  [target restore/migration](../../internal/backuppg/restore.go),
  [trusted finalizer](../../internal/recovery/restore.go) and
  [pre-migration database binding](../../internal/recovery/binding.go).

The inspected source32 versions expose the same CLI and offline ownership
flow. Its embedded migrations end at27; current migrations include28. Relative
source links above show current code, not a claim that the old binary contains
later engine validation, diagnostics or cancellation fixes.
