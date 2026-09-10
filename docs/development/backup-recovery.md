# Native backup and recovery

Status: final source30 passed all 1605 race tests across 24 packages, UI a6
passed, and the complete real-process/browser/offline-CLI journey passed.
The feature is deployed on `test-env` at schema 23/probe 6. Protected deployment
and the main-service backup/download workflow passed.
This is a native Goby feature; Emby
BackupRestore plugin routes and the upstream archive format remain unsupported.

This round closes with M5j acceptance, deployment, handoff/progress and
publication. Further work is paused; other administrator, playback and
compatibility milestones remain open.

## Product boundary

The administrator page at `/admin/backups` creates, imports, downloads and
deletes encrypted backups, tracks durable operations, prepares restores,
applies a ready plan, and activates an explicitly retained rollback copy.
[The native API contract](../api/backups.md) documents its route
patterns and authentication, pagination, revision and idempotency rules.
The dashboard contains no end-user playback page.

The archive contains a PostgreSQL custom dump, five allowlisted logical
configuration defaults and the matching application-key master when present.
It excludes media files, NFO/artwork sidecars, database passwords, logs,
caches, executable files and arbitrary deployment directories. Restore
therefore requires the original media to remain mounted at approved paths.
Users, password hashes, policies, library/catalog records, settings and
history are retained. Imported login credentials are revoked, including
credentials underlying application keys; users must sign in again.

The service uses two preconfigured PostgreSQL databases with different
ordinary roles. It does not require application superuser or CREATEDB
privileges. Restoring an archive first builds and validates the inactive
slot. The active database remains intact. A later restore may reuse an older
inactive copy only after explicit `ReplaceRollback` consent and an exact
local ownership/data check. An arbitrary populated database is never adopted
or silently cleared.

## Architecture and failure handling

`backupformat` uses age 1.3.2 with production scrypt work factor 18. Fixed tar
entry names, bounded lengths, version checks, complete-stream authentication
and SHA-256 descriptors prevent partial or ambiguous imports. An upload is
stored as unverified until a complete restore-plan validation succeeds.

`backuppg` uses a PostgreSQL exported repeatable-read snapshot for the custom
dump and table fingerprints. The application-key witness validates every
sealed key, including revoked history, in the same snapshot. It does not
create a missing master when there are no sealed keys. Restoration executes
compiled migrations and strictly decoded COPY/sequence data; no uploaded
SQL, DDL or executable archive content is executed. Trusted schema, raw data,
key/administrator/root checks, credential revocation and the local target
marker commit atomically. A failed finalizer leaves the target empty.

Schema 23 has 29 tables. Table ordering uses real primary/unique keys,
including the NULLS NOT DISTINCT key of `client_playback_references`.
Shared sequences are checked against every trusted consumer, including
`devices` and `application_key_devices`. Sequence positions from pg_dump
are not MVCC table facts. Historical migration records contain version,
filename and timestamp; compiled SQL checksums are separate archive facts.

`backupstore` owns encrypted objects, quotas, prepared publication, stable
download snapshots and deletion recovery. Plaintext scratch uses anonymous
Linux files that are never linked or downloadable. Its reservation shares
the storage quota. Owned children and file consumers are joined before
scratch is closed.

`recoverycontrol` is a private, bounded compare-and-swap operation journal.
It stores non-secret request identity, source generation, immutable archive
digest, target ownership and transition proofs. Idempotency records remain
for at least seven days. Recent records are not evicted to admit more work.
The journal never stores a passphrase or a passphrase hash.

Native admission rechecks the live administrator inside the transaction
that writes its activity receipt. A worker may publish, delete or replace
only after committed authorization. A lost commit response is reconciled
against the exact receipt. Prepared ciphertext is resumed from its original
digest. Incomplete encryption or decryption cannot be reconstructed without
a new user-supplied passphrase. Committed cancellation is recovered even
when its following journal update did not finish. Backup/restore activity
resource IDs identify operations; download activity identifies the backup.

`recoverydb` binds each slot to the local deployment and generation through
`server_settings.goby.recovery.binding.v1`. Initial migration adds one
binding row to the original database. Restoration overwrites an imported
binding only through the locally authorized finalizer. Reusing a retained
slot requires its saved marker and every trusted table fingerprint;
unknown objects, grants, schema drift and unrelated data are rejected.
Destructive selectors come from the compiled catalog and use RESTRICT.

`lifecycle` owns the exclusive local deployment lock and immutable
configuration/key generations. A dedicated PostgreSQL session lease fences
each open database generation. Lease loss cancels its work. Shutdown joins
HTTP, application work and recovery workers, drains pool borrowers, and
releases the database lease last. These are cooperative single-host
ownership guarantees, not distributed HA or per-write fencing.

Apply drains ingress and application writers before capturing the retired
database and matching configuration/master. The target is validated under
its lease before startup components may write. Its listener is reserved
without serving. The coordinator records `accepting`, commits the target
completion receipt, finishes lifecycle publication and only then serves.
A lost acceptance response always resumes the selected target; it never
causes speculative automatic rollback after potential acceptance.

An unaccepted target whose startup fails can return to the exactly retained
source after all target handles are closed and ownership is reacquired.
Automatic return preserves the old credentials; explicit administrator
rollback revokes them. Return publishes a new revision rather than rewinding
a CAS token. It is refused when either slot's ownership/data cannot be
verified. In particular, an unreachable failed target cannot prove its
writers have stopped. A promised online retirement is not silently weakened
by a later offline invocation without its source database.

## Linux configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `GOBY_RECOVERY_STATE_DIR` | `/var/lib/goby/recovery` | Lifecycle and immutable generations |
| `GOBY_RECOVERY_OPERATIONS_DIR` | Final state directory plus `-operations` | Private operation journal |
| `GOBY_BACKUP_DIR` | `/var/lib/goby/backups` | Encrypted objects and anonymous scratch |
| `GOBY_RECOVERY_DATABASE_URL` | Unset | Independent, preprovisioned inactive database/role |
| `GOBY_PG_DUMP` | `pg_dump` | PostgreSQL 17 dump executable |
| `GOBY_PG_RESTORE` | `pg_restore` | PostgreSQL 17 archive decoder |
| `GOBY_BACKUP_TIMEOUT` | `30m` | Operation timeout, one to thirty minutes |
| `GOBY_BACKUP_MAX_OBJECT_BYTES` | `8589934592` | Encrypted object size limit |
| `GOBY_BACKUP_MAX_TOTAL_BYTES` | `34359738368` | Objects plus reserved scratch capacity |
| `GOBY_BACKUP_MAX_OBJECTS` | `128` | Stored object count limit |
| `GOBY_BACKUP_MIN_FREE_BYTES` | `536870912` | Filesystem free-space reserve |

Use three separate private directories owned by the service UID. They must
not overlap each other or approved media, cache or log paths. The service's
systemd sandbox must permit writes to them and execution of its selected
PostgreSQL tools. Use the same deployment environment and UID for offline
commands. Neither HTTP requests nor archives select database URLs or local
filesystem destinations.

Only `ServerName`, `MaxBitrate`, `MaxWidth`, `MaxHeight` and
`MaxAudioChannels` deployment defaults are optionally restored. Network
binding, public origin, credentials, approved media roots, tool paths,
hardware selection and cache/log policy remain local deployment settings.
Database-managed overrides are part of the database snapshot regardless
of the `RestoreDefaults` switch.

## Offline recovery commands

Stop the service and run the installed binary as its deployment owner with
its original environment. The CLI holds the same private deployment lock.
It can import and plan without connecting to the original database or
opening the original application-key vault. It uses only the configured
inactive database. Passphrases are exact UTF-8 bytes, 12..1024 bytes;
newlines are not stripped. No passphrase is accepted in command arguments.

```text
goby recovery help
goby recovery status
goby backup list
goby recovery jobs
goby backup import --file ARCHIVE --request-id REQUEST
goby restore plan --backup-id BACKUP --sha256 DIGEST --request-id REQUEST \
    --generation-revision GEN --passphrase-file PRIVATE_FILE \
    [--restore-defaults] [--replace-rollback]
goby restore apply --id PLAN --revision REV --generation-revision GEN \
    --accept-no-rollback
goby restore rollback --request-id REQUEST --generation-revision GEN \
    --accept-no-rollback
goby recovery cancel --id OPERATION --revision REV
goby recovery resume
```

Replace uppercase placeholders with values from JSON output. REQUEST is a
new caller-generated 32-character lowercase hexadecimal ID. Reuse it only
to inspect/replay that exact attempt. Plans wait for staging to finish;
status and job output remain safe to retain. Use `--passphrase-stdin` instead
of a private file when needed. A passphrase file must be a regular file,
owned by the current UID, mode 0600, with one link and no final symlink.

Offline activation preserves the original database but cannot certify it
as a rollback copy. `--accept-no-rollback` acknowledges that boundary. The
candidate must initialize and reserve its listener before it is accepted;
the command then closes it without serving. Start the normal service after
successful output and sign in with a restored account. `recovery resume`
continues an already authorized transition; it does not waive unavailable
source/target ownership checks or recover a forgotten archive passphrase.

## Verification record

Every test, browser run and runtime probe executes through `ssh test-env`.
Local compilation is authorized; local runtime verification is not.
Intermediate counts overlap and do not add to a unique full-suite total.

| Scope and snapshot | Result | Evidence |
| --- | --- | --- |
| Archive/lifecycle foundation | 31 race tests passed | [Foundation](m5j-foundation-race.json) |
| Historical PostgreSQL module, before offline extension | 42 race tests passed | [Module](m5j-backuppg-race.json) |
| PostgreSQL offline extension | 54 race tests including seven real PG groups passed | [Offline module](m5j-backuppg-offline-race.json) |
| Earlier storage/configuration/identity/lease/activity integration | 79 race tests passed | [Integration](m5j-integration-race.json) |
| Historical root online/offline staging pipeline | 36 race tests passed; predates atomic finalizer | [Pipeline](m5j-pipeline-race.json) |
| Current schema23 catalog and recovery DB/finalizer bindings | 70 race tests passed, no skips | [Bindings](m5j-recoverydb-bindings-race.json), [catalog](m5j-recoverydb-catalog.json) |
| Native HTTP and durable control | 32 race tests passed, no skips | [HTTP/control](m5j-http-control-race.json) |
| Native manager, publication/cancellation, operator staging, transitions/configuration/lease, source23 | 46 race tests passed, no skips | [Manager/transition](m5j-manager-transition-race.json) |
| Administrator UI snapshot a6 | TypeScript, 27 browser cases with mocked API responses and production build passed | [UI report](m5j-backup-ui-mock.md), [results](m5j-backup-ui-mock.json), [57 assets](m5j-admin-assets.json) |
| Diagnostic test-fixture permissions | Full 36-test package and 200 repetitions of the affected case passed | [Fixture verification](m5j-diagnostics-fixture-race.json) |
| Historical command delta, source26 | All 16 cmd race tests passed, including completed replay and refusal of a different pending operation | [Command package](m5j-final-cmd-race.json) |
| Historical complete source25 repository | 1599 top-level race tests passed, zero skips/failures/races across 24 packages; predates the final command and HTTP changes | [Earlier full regression](m5j-full-race-summary.json); [source24 failure](m5j-full-failed-attempt-1.json) retained |
| HTTP body completion, source30 | Old cleanup reproduces the failure; three fixed completion, cancellation/idle and byte-limit cases pass | [Focused regression](m5j-http-body-lifetime.json) |
| Complete final source30 repository and executable | 1605 top-level race tests passed across 24 packages, with no skips, failures or races; Linux amd64 binary built with Go 1.27.1 and CGO disabled | [Final full suite](m5j-final-full-race.json), [build](m5j-final-build.json) |
| Earlier real browser/process attempts | Creation, restore, old-cookie 401 and restored-account login observed in source28; later source29 exposed the request-body race | [Source28 failure](m5j-runtime-failed-attempt-4.json), [source29 failure](m5j-runtime-failed-attempt-5.json); neither is complete acceptance |
| Complete runtime/offline CLI, transition/restart and cleanup acceptance | Passed: three browser journeys, restore/rollback/offline activation, six generation checks and three restarts | [Real runtime acceptance](m5j-runtime-acceptance.json) |
| Final candidate/source/evidence gate | Passed: complete source30 regression/build, UI a6, actual runtime, source and artifact identities | [Final source gate](m5j-final-source-gate.json), [workspace](m5j-workspace-source-reconciliation.json), [staged source](m5j-git-source-reconciliation.json) |
| Protected deployment and main-service workflow | Passed: fresh 29-table restore rehearsal, schema-23 deployment and retained native backup/download/logout | [Deployment](m5j-deployment-evidence.json), [main workflow](m5j-deployed-backup-workflow.json) |
| Deployment parser and forward-finalization remediation | All 54 guard tests passed, then actual read-only finalization passed | [Guards](m5j-deployment-remediation-tests.json); [initial failed attempt](m5j-deployment-failed-attempt-1.json) remains retained |

Source30 includes the command replay fixes and three HTTP completion
regressions. Its passed complete run replaces the earlier source25/source26 split as
the final Go acceptance gate. Earlier module counts remain historical and
overlap the full suite.

The real-process attempts exposed both harness and product issues. The
harness now excludes PostgreSQL controller environment variables from Go
processes, handles the exact Emby text `401` contract, and preserves durable
request identity when Chromium evicts a response body. Separately, normal
request-body EOF cleanup could expire net/http's background read deadline and
cancel an in-flight plan or the next keep-alive request. A shared body owner
now closes completed bodies without expiring that deadline. Unread uploads
retain bounded interruption and close their connection. The old implementation
reproduces the focused regression; real TCP tests cover fixed/chunked JSON,
delayed authenticated planning, a subsequent request on the same connection,
and an unauthorized client that withholds its advertised body.

The initial deployment completed installation but its final service check
misread repeated `EnvironmentFiles` properties from systemd. The corrected
parser preserves every value and rejects repeated singleton properties. A
separately pinned forward-finalization path rechecked the original backup,
restore rehearsal, all installed bytes, unchanged old rows, service properties,
recovery ownership, master, media and logs before publishing acceptance. It
did not restart the candidate or restore the live database. The original
operator/candidate evidence and new 54-test remediation evidence remain separate.
The production Go and UI inputs remain the accepted source30/build inputs.
Only the deployment operator and its Python guards changed after that snapshot;
the remediation gate covers those changes. [Final Git reconciliation](m5j-final-git-reconciliation.json) records
this explicit delta and any byte-exact line-ending normalization separately.

The accepted main process is PID 3750313, UID 995, start ticks `28911319`.
It runs the final 25,715,909-byte executable and 57 current assets. The native
workflow passed in 2.373 seconds: a 177,366-byte encrypted archive was created
and downloaded, all preexisting rows remained exact, and seven audit records
plus one revoked native session were retained. The exact main archive was not
decrypted or restored by that workflow; actual product restore, rollback and
offline recovery are established by the isolated runtime acceptance. The
archive and its protected passphrase copy are retained as described in the
[handoff](handoff.md).

The current compiled schema23 catalog file SHA-256 is
`de85f4917dd7409e7e7bed20c7cbe63f0d7afe68f6ed72faff6b5bbb598ed00b`;
its structural signature is
`ff1fac5cd528f0cb72454a0e290bc5574e0679c979aedcfd0a948bcdb2b884ac`.
Earlier catalog hashes describe an unpublished earlier migration23 revision.

Source23 is the 591-input snapshot at
`/opt/goby-test/exec-work-m5j/backuppg-source-attempt-23`; its 46-test log
SHA-256 is `1dd56bdea29ea861f14a9cbbb4f820e04635b0ffabdd0565297cc4b53c740582`.
It exercised real pg_dump/age/restore, application authorization, published
and unpublished receipt recovery, actual generation activation, acceptance
recovery, rollback, safe failed-startup return and changed-target refusal.
It does not establish the later main-process/CLI/browser wiring or deployment.

Retained failures include the initial boundedness/KDF preparation issues,
command/source packaging mistakes, a PostgreSQL lease-release timing fix,
HTTP fixture header canonicalization, and cancellation returned through a
catalog-error classification. The latter is fixed by honoring committed
cancellation authority. The first full run passed 1587 top-level tests and
failed one existing diagnostic fixture: systemd's default umask made its
temporary subdirectory 0755. Explicit 0700 setup fixes the fixture while
preserving every production permission check and concurrent-close assertion.
The actual mode/UID observation and repeated test evidence are retained.
See the linked attempt reports; failed runs are not
relabelled as passed. Successful PostgreSQL runs restored exact HBA bytes,
removed owned databases/roles/cgroups, and preserved the main/reference
service identities. The standard KDF verification bound is 1536 MiB,
swap disabled, CPU quota 150 percent and two Go processors.

