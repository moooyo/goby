# Isolated restore data comparison contract

Status: **DRAFT / NOT ADMITTED**, 2026-09-14. This is a comparison specification,
not an executor or a passed restoration. It adds no SQL, service, authentication,
fixture, or resource action. Admission and actual phase receipts remain required.

## Fixed recovery point and independent targets

Use the [completed source32 archive](audited-main-native-backup-completed.json):
SHA256 `0a61cbd6c6ff23543ba873ed1a44dd33ecce2f8702f956e24096862996c7874f`,
196,310 bytes, schema27, 35 tables, 405 rows. The completion checkpoint is
`3a9ff1ddf45213331aa4f23d66d1beedd44da9f18df8e168e9e56badc6a925cf`.
Keep credentials, archive plaintext and full database rows on the remote host.

For the selected fixture, call its physical primary slot **A** and recovery slot
**B**: offline restore into B and apply; serve B; online plan the same archive
into empty A and apply while retaining B; request native rollback from A to B;
restart B. The old-compatible proof uses a separate fresh target and stores.
Its restore implementation must have an embedded migration ceiling of27; the
selected `b0d6769c...` implementation would upgrade to28. After the selected
service is stopped and drained, replace the shared isolated installation's
selected binary and 57 assets with the retained `af46a82e...` binary and all 415
old assets for the old-compatible authentication/stop/restart proof. Keep A/B
and their stores unchanged until their admitted final disposal. Never run
source32 against selected schema28 data. Installation replacement is not a
database down-migration.

The saved private `controller-before-snapshot.json` (SHA256
`fe0065a8cde1da2895e2c2dee9d848cc70ce32d1d1f11249a1d827f18d430f28`) contains 402
rows; `controller-after-snapshot.json` (SHA256
`a570b96daa57b9b9cabf83ef55c70e4fe896355924cc62c03d9f09ea60244286`) contains 408.
Both are under the checkpoint's `private` directory. Saved-only inspection
returned counters, not row dictionaries. The archive includes the new session,
login activity and backup-request activity; completion, download and logout
happened later. Neither operational snapshot is the archive's raw row image.

## Raw validation, migration and normalization

The [native restore](../../internal/backuppg/restore.go) first authenticates
source migration/catalog, complete table fingerprints, server ID and dump
sequence bounds. It then migrates to the executable's current schema and runs
the [identity finalizer](../../internal/recovery/restore.go) plus local marker
stamping before the same target commit. Scanner/task reconciliation follows in
separate transactions. Require each boundary's actual successful closeout;
`RestoredDatabase` counters alone do not prove all later writes completed.

For each row below, preserve every unlisted field and every nonmatching row.
New timestamps must satisfy the named SQL expression and the recorded database
clock interval for that phase; do not globally remove timestamp fields.

| Table | Actual predicate and permitted change | Expected for this archive before application startup |
| --- | --- | --- |
| `sessions` | `revoked_at IS NULL`, without kind/expiry filtering: set only `revoked_at=transaction_timestamp()`. Previously revoked timestamps remain exact. No per-row revocation audit is emitted. | 17 updates: the 16 previously unrevoked admin rows plus the backup's in-snapshot admin session. All 103 archived sessions remain; all become revoked. |
| `play_sessions` | `state IN ('Prepared','Playing','Paused')`: `state='Expired'`; `stopped_at=COALESCE(old,clock_timestamp())`; `updated_at=GREATEST(old,clock_timestamp())`. No `expires_at` predicate. | 1 update, with initially null `stopped_at`; final states 23 `Expired` and 7 `Stopped`. `user_item_data` remains exact. |
| `encoding_jobs` | `state IN ('queued','running')`: set `state='interrupted'`, `error_code='backup_restored'`, `updated_at=GREATEST(old,clock_timestamp())`. | 0; all 29 completed rows exact. |
| `scan_jobs` | Scanner selection is active scan status **or** linked child state `queued/running`. Only scans with `status IN ('Queued','Running')` become `Interrupted`, with fixed error `Server stopped before the scan finished` and new `finished_at`; each changed scan emits `scan.finished`. | 0; all 46 completed rows exact. |
| `task_run_children` | For selected linked `queued/running` children, copy scan `state` lowercased, `scanned/added/updated`, `error_message`, `started_at/finished_at`; map Failed/Cancelled/Interrupted to `scan_failed/scan_cancelled/scan_interrupted`, otherwise empty `error_code`. A terminal child must already match. Then `waiting` children of active runs become `interrupted`, with new `finished_at`, `error_code='server_interrupted'`, `error_message='The server stopped before this library scan was admitted.'`. | 0; all 5 completed children exact. |
| `task_runs` | `state IN ('pending','running','stopping')`: `state='interrupted'`, `error_code='server_interrupted'`, `error_message='The server stopped before this task finished.'`; recompute all child-count fields and `scanned/added/updated`; preserve `started_at`; coalesce `finished_at`. Each transitioned parent emits `task.finished`. | 0; the completed run exact. |
| `task_definitions` | Reconcile `library.scan`: insert if missing; otherwise update only differing `emby_key/name/description/category` and `updated_at`. For other enabled keys, set `enabled=false`, increment `revision`, update `updated_at`. No schedules or runs are created. | 0; the one existing definition already has the exact compiled values. Both existing triggers are retired. |
| `server_settings` | Only `goby.recovery.binding.v1`: compare its complete old raw value, then replace `value` with the new local deployment/generation/slot marker and update `updated_at` if different. Existing `created_at`, `server_id` and all other settings remain exact. | 1 existing row changes for initial staging; no added row. Each later changed generation marker is a separately recorded delta. |
| `schema_migrations` | Preserve source rows 1..27, including `applied_at`. Selected restore adds exactly migration28 with its trusted name/checksum and actual application timestamp. | Selected staged total 406 rows; source32 staged total 405. These totals exclude application-startup and authentication effects. |
| `library_roots` | Selected migration28 adds `binding_revision=1`, `storage_binding=NULL`, `bound_at=NULL`, `bound_by=NULL`; all preexisting fields remain exact. Source32 adds nothing. | 5 registered roots, still explicitly unbound on selected. No path remapping or storage approval. |
| `activity_entries` | Preserve all 21 archived rows. Selected migration28 adds only `previous_revision=0` and `observation_fingerprint=''` to them. Additional events require the phase/actor/resource rules below. | No restore-normalizer events for the zero-work predicates above. |

The scanner/child predicates come from
[task_scans.go](../../internal/library/task_scans.go); definition and run rules
come from [store.go](../../internal/tasks/store.go) and
[runs.go](../../internal/tasks/runs.go). Task aggregate fields are
`total_children`, `terminal_children`, `completed_children`, `failed_children`,
`cancelled_children`, `interrupted_children`, `unavailable_children`, `scanned`,
`added`, `updated`. Source32 and selected use the inspected same normalization
predicates; migration28 is the explicit schema difference.
The compiled definition tuple is `RefreshLibrary`, `Scan media library`,
`Scan all registered media libraries.`, `Library`; no trigger is created by
reconciliation.

All other 24 tables are exact at this boundary: `application_key_clients`,
`application_key_devices`, `application_keys`, `catalog_entities`,
`client_playback_references`, `devices`, `extra_reserved_paths`, `item_entities`,
`item_extra_resources`, `item_images`, `item_metadata_state`, `item_subtitles`,
`item_theme_resources`, `items`, `libraries`, `managed_settings`,
`task_occurrences`, `task_run_requests`, `task_triggers`, `theme_owner_ids`,
`theme_reserved_paths`, `user_item_data`, `user_settings`, `users`.
In particular, all 10 users, 4 enabled administrators and 4 sealed application-key
rows survive. Key recovery validates all sealed history, including revoked keys;
do not print keys, tokens, password hashes or ciphertext.

## Audit and authentication ownership by phase

Use actual committed HTTP/CLI operation IDs and the database active at that
phase. Never infer the audit database from an operation's target slot alone.

| Phase | Database writes and audit destination |
| --- | --- |
| Offline import and offline plan into B | Operator authorization is in the private journal; no native login and no SQL `restore.requested`/`restore.planned` audit. The staged B receives the normalization and binding changes above. |
| Offline apply/accept B | The newly constructed application manager runs on B and `AcceptSwitch` adds one system `restore.applied`, `state='completed'`, for the offline restore operation. CLI success is not evidence that a daemon remains running. |
| Native authentication while B serves | Each successful admin login adds one B `sessions` row and one B `session.login`. Native authentication does not register a device. Each actual first logout updates that token's `revoked_at` and adds one B `session.revoked`; repeated/unknown-token revocation adds nothing. |
| Online plan from B into A | B receives native `restore.requested` and system `restore.planned`; A is restored independently from the common archive and normalized/migrated as above. |
| Online apply B to A | B receives native `restore.apply_requested` before it is drained and captured. A receives system `restore.applied` at acceptance. B receives no target-acceptance audit. |
| Native authentication while A serves | New owned sessions/login/logout audits belong to A. A rejection of a former B token does not prove its retained B row was revoked. |
| Online native rollback A to retained B | A receives native `restore.rollback_requested` and is then captured as the retiring generation. B is checked against its retained facts, normalized again by `normalizeRestoredIdentity`, and rebound before activation; B receives system `restore.applied` for the rollback operation. This is not a second archive restore. |
| Restart B, or old-compatible start/restart | Normal startup reconciliation/retention must be accounted for. A completed switch does not add another `restore.applied`. Newly declared login/logout chains write only the currently active target. |

In the planned serial rollback, A's authorizing session remains unrevoked in
retained A; bind its actual row to that captured image. Do not update A to make
cleanup counters look complete. After returning to B, log out every newly owned
session in current B and prove a 401 response for the same token. A 401 for A's
token at B proves rejection by
the current service, not `revoked_at` in inactive A. Native session endpoints
use the active identity store; the inspected API provides no separate inactive-
slot revocation path. Retain the A credential privately as an explicit temporary
responsibility. Close it by the admitted ordinary disposal of A's database/role,
or by verified shutdown and ordinary unmount/disposal of the exclusively owned
temporary PostgreSQL cluster containing the fixture. Keep private comparison
evidence; report the credential as disposed, not as database-revoked. Neither a
401 from another slot nor service stop alone completes this disposal. Do not add another
rollback cycle solely to manufacture a revocation timestamp.

Recovery admission events have `source='native'`, the actual admin user/session
actor and `resource_kind='restore'`, `resource_id=operation.Id`; system events
have empty actor IDs. Their `severity='Info'`, `revision=0`, `affected_count=0`,
`request_id=''` and `changed_fields=[]`; selected schema28 also requires neutral
`previous_revision=0` and `observation_fingerprint=''` (absent in schema27).
Authentication events instead have `affected_count=1` and the session as the
resource. Native `requireAdmin`/`CheckAdministrator` reads do not touch session
activity timestamps; Emby touch rules are outside this native-only phase.
[grantLocked/recordSystem](../../internal/recovery/manager.go),
[AcceptSwitch](../../internal/recovery/transition.go),
[CLI activation](../../cmd/goby/recovery_cli.go) and
[login/revocation](../../internal/identity/device_registration.go) define these
destinations. Keep each new session and audit row individually correlated;
an aggregate row-count match is insufficient.

## Retained B boundary and comparison implementation

`nativeRetainedFacts` means the exact protected `recoverydb.Retained` recorded
by `captureRetiring`: database/role, parsed marker, complete raw marker and native
`SourceFacts` (schema/migration/probe/PostgreSQL/server identity and all 35 table
counts/fingerprints). Capture occurs **after B ingress and all application writers
are drained**, after B's apply grant. It therefore includes B's admitted
authentication/plan/apply history, not merely the earlier restored 405-row image.

From that capture until explicit rollback mutation, B's full rows, marker and
native facts must remain exact. Do not log out into B, run startup/retention on
B, or refresh the retained baseline after drift. Keep a separate exact before/
after observation of all five sequence `last_value/is_called` pairs:
`SourceFacts` does **not** contain sequence current values. Native `sameRetained`
relaxes only PostgreSQL minor-version formatting; it does not relax table data,
marker bytes or schema/migration facts.
The captured key witness and generation/master/configuration file bindings
remain separate evidence; the retained table-facts object does not contain
those file bytes.

Rollback checks this B image before mutation, revokes every then-unrevoked B
credential, expires any then-matching playback, interrupts any then-matching
encoding and stamps the return generation. Compute these counts from frozen B,
not from 17/1 in the original archive. The return generation is the retained
`BeforeImage` allocated when B retired; it differs from B's former active
generation. Lifecycle revision advances. The predicted post-mutation facts are
persisted before SQL commit, then acceptance/startup add their separately allowed
changes. A is also retained during this rollback. Restart must use the new B
generation and the recovered generation master.

Reuse native `Snapshot.Facts`/`RecoveryInspection.Facts` and the existing bounded
private row observer. Native fingerprints use PostgreSQL `to_jsonb(row)::text`
UTF8 bytes, trusted sort keys cast to text with `COLLATE "C" NULLS FIRST`, and an
8-byte big-endian byte-length prefix per row. Python/JavaScript JSON re-encoding
is not an equivalent fingerprint. For semantic row comparison, use complete
trusted-key row sets, explicit field overlays above, separately checked new-row
sets and sequence facts; reject every unexplained change. Do not mask whole
tables or discard timestamps, tokens, ciphertext or audit fields from private
comparisons. Publish only receipt hashes, counters and safe identity/status.

Still to measure/admit: actual archive-decoded rows and sequence values; phase
database-clock bounds; native B/A retained records and generation IDs; exact
owned authentication count; startup/cache and task/retention effects under the
frozen configuration. Original activities begin 2026-09-10; admission must choose
and record a retention window that preserves them, or explicitly account for
the real bounded deletion predicate. No active scans/runs/unretired schedules
exist in the saved input, but unexpected matches or writes stop the rehearsal.
Neither these expectations nor a native retained fingerprint proves execution,
media availability, old-installation rollback, core acceptance or main promotion.
