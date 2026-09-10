# Development handoff

This round is closed after M5j native backup/recovery, deployment, documentation
and publication to `origin/main`. Further development is paused at the user's
request. Do not start another milestone until the user resumes work. The complete
planned server and full Emby compatibility remain unfinished.

## Project and accepted baseline

Goby is an open-source, independently implemented Emby-compatible backend for
Linux, using Go and PostgreSQL exclusively. React/MUI provides a Material Design
administrator dashboard; an end-user web playback client is outside scope.
Use Chinese for user-facing communication and English for code and documentation.
Work directly on `main` and commit/push accepted increments. Toolchain pins are
Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11.

Before M5j, the accepted deployment is M5i `58f98a6`, schema 22/probe 6,
PID 3668655, UID 995, start ticks `26912384`; original Emby PID 3131777.
These recorded identities must be rechecked before acting on a service.
[Progress](progress.md) and the linked per-increment reports establish:

- M0: local API inventory, 2,462 Emby 4.9.5.0 reference records and 240 preserved
  media sources. Research captures are not product/client acceptance.
- M1/M2: identity/setup, non-root service, safe ingestion, catalog/ACL browsing,
  local NFO, entities and indexed local artwork within their documented scopes.
- M3/M4: original playback/state, sessions/NextUp, external SRT/WebVTT, selected
  events/control, HLS and progressive conversion, profiles, restart and refresh.
- M5a–M5i: managed users/metadata/sessions/keys/devices, library task scheduling,
  settings, bounded configuration compatibility, and transactional activity/logs.
  M5i passed 1,380 race tests plus its separate browser, restart and deployment gates.

## M5j final acceptance record

The following results belong to the final source and accepted deployment.
Historical and narrower test counts are not added to the full-suite count.

| Gate | Final result and durable evidence |
| --- | --- |
| Final source30 regression and executable | Passed: 1605 race tests / 24 packages; Linux amd64, CGO disabled — [full suite](m5j-final-full-race.json), [build](m5j-final-build.json) |
| Complete browser/process/offline CLI, transitions/restarts and cleanup | Passed: three browser journeys, six generation checks and three restarts — [runtime acceptance](m5j-runtime-acceptance.json) |
| Joined source, executable, assets and acceptance inputs | Passed: 576 installable source inputs, 40 production web inputs and 57 assets — [final source gate](m5j-final-source-gate.json) |
| Protected deployment and native create/download workflow | Passed; 29-table restore rehearsal before upgrade, then a retained encrypted backup and verified logout — [deployment](m5j-deployment-evidence.json), [deployed workflow](m5j-deployed-backup-workflow.json) |
| Final binary/asset identity, schema/probe and PID/start ticks | Schema 23/probe 6; PID 3750313, UID 995, ticks `28911319`; binary SHA-256 `a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81`; 57 current assets |
| Publication and resource cleanup | Published on `origin/main`; owned verification databases/roles/cgroups removed and HBA restored. [Final source reconciliation](m5j-final-git-reconciliation.json) and [service/backup state](m5j-final-state.json) preserve the final checks. See Git history for this closeout commit. |

M5j product code is implemented: encrypted archives, durable operations, native
HTTP/UI, inactive-slot ownership/reuse, generation activation/rollback and offline
CLI. A real HTTP cleanup race was found: normal-EOF deadline cleanup cancelled a
keep-alive request. The fix and focused regression are recorded in
[HTTP body lifetime evidence](m5j-http-body-lifetime.json). The new whole source30
suite and complete real-runtime journey passed. Source25 and
command26 results are now historical scoped evidence, not final-source acceptance.
UI a6 remains the same 27 mocked-API browser cases and 57 production assets.
The first deployment installed the candidate but could not finalize its report:
systemd emitted repeated `EnvironmentFiles` lines and the original parser kept
only the last one. [That failed attempt](m5j-deployment-failed-attempt-1.json)
is retained. [54 deployment guards](m5j-deployment-remediation-tests.json) passed
before read-only forward finalization. That finalization rechecked all installed
artifacts, original data, master/media/logs, recovery ownership and process
identity; it did not restart or restore the service. The main workflow then
passed in 2.373 seconds, preserving all old rows and retaining seven audit records
and one new revoked administrator session. Its downloaded archive was checked
by size, SHA-256 and age header; that main-service archive was not restored.

## Native operator workflow and locations

The [backup/recovery guide](backup-recovery.md) and [native API](../api/backups.md)
are the operational contract. Emby BackupRestore/plugin routes and its archive
format remain unsupported. The archive includes the database, five allowed
logical defaults and the matching application-key master; media and deployment
credentials are excluded. Preserve original media mounts and the database/master pair.

The accepted M5j deployment uses these paths. Recheck current ownership and
generation before an operator action; these paths are not scratch space.

| Purpose | Linux path |
| --- | --- |
| Lifecycle/generations (`GOBY_RECOVERY_STATE_DIR`) | `/var/lib/goby-test/recovery-m5j` |
| Encrypted store (`GOBY_BACKUP_DIR`) | `/var/lib/goby-test/backups-m5j` |
| Operation journal (`GOBY_RECOVERY_OPERATIONS_DIR`) | `/var/lib/goby-test/recovery-operations-m5j` |
| Private recovery database environment | `/opt/goby-test/recovery-m5j.env` |
| Inactive recovery database / role | `goby_recovery_m5j` on the primary PostgreSQL cluster at port 5432 |
| Main runtime / existing administrator credentials | `/opt/goby-test/runtime.env` / `/opt/goby-test/browser.env` |
| Private verification evidence | `/opt/goby-test/exec-work-m5j/` |
| Operational pre-upgrade backup | `/opt/goby-test/backups/m5j-20260910/` |

`verify-backup-recovery-deployed.py` checks the accepted candidate/deployment,
logs in with the existing native administrator, creates one backup, waits for its
completed job, verifies HEAD/range/full attachment bytes and SHA, then proves
logout by HTTP 401 and SQL. It does not restore, delete, change settings or touch media.
A successful run retains the encrypted object in the native store and a copy plus
its passphrase outside all three strict stores:

- `/var/lib/goby-test/operator-secrets-m5j/attempt-01/`: UID 995, mode 0700;
  `<RequestId>.passphrase`, `<BackupId>.age`, `request.json` and `backup.json`: mode 0600.
- `backup.json` pairs the accepted file, digest and passphrase filename. Preserve
  `request.json` and the sole passphrase even when admission is uncertain.
- Safe before/after hashes and the report are under
  `/opt/goby-test/exec-work-m5j/deployed-backup-workflow-01/`, root-only mode 0700.
  Later explicitly selected attempts use their own numbered directories.

For offline recovery, stop the service and use the installed binary with the
same deployment UID/environment. Follow the documented import → plan → apply
sequence; obtain IDs/revisions from actual output. Passphrases use a protected
0600 file or stdin, never command arguments or logs. Start the normal service
after accepted CLI output and sign in with a restored account. The CLI's
`--accept-no-rollback` acknowledges its documented offline boundary, not permission
to erase an unrelated database. Do not add arbitrary files inside the strict stores.

`deploy-backup-recovery.py` is a **one-shot M5i-to-M5j deployment operator** pinned
to that baseline. Its recovery mode is for an unpublished interrupted attempt,
not a general rollback tool. After publication, do not rerun it or restore a
historical M5i/M5j operational backup over newer accepted business state.
Future upgrades need newly reviewed ownership, baseline and preservation gates.

## Resume and later-round priorities

Start with this final record, [progress](progress.md), the backup guide and the
tracked reports above. They are the durable authority for a cloned repository;
no ignored checkpoint file or old live-session handle is required. If a final
M5j record conflicts with current state, reconcile the exact evidence before
editing or operating the service. Wait for the next round's scope before continuing.

| Suggested later priority | Still open |
| --- | --- |
| P1 — M3 / client acceptance | More events/subscriptions, broader subtitles, global NextUp parity and reproducible real-client flows. |
| P2 — M4 | Nonzero copied-video seeking, efficient audio I/O, more tracks/formats, aggregate isolation and actual GPU decode **and** encode. |
| P3 — remaining M5 / metadata | More task executors, full policies, providers, broader configuration fields/sections and metadata/artwork reconciliation. |
| P4 — M6 | Differential client/reference coverage, Linux distribution/architecture/GPU matrix, large-catalog upgrades, operations and recovery coverage. |

M7 remains deferred. Software success is not GPU verification; matching routes
alone is not third-party playback compatibility. These priorities are not work
for the current closing round.

All tests, validation, runtime/media probes and browser checks run through
`ssh test-env`; local Windows/PowerShell work permits builds only. If SSH is
unavailable, report verification blocked. Keep main PostgreSQL `5432` protected;
use owned fixtures on isolated `15432`, serialize HBA and memory-heavy work, and
give every fixture three unique private recovery directories. Never weaken KDF
strength, clear an arbitrary populated slot, or delete retained evidence to pass a gate.
