# M5e: device administration

Verification date: 2026-09-10 (Asia/Shanghai).
Status: **passed for this increment**, including the complete race suite,
focused regressions, isolated browser/restart acceptance, final deployment,
and main-service device workflow.

This increment adds the [device API](../api/devices.md) and native Devices
page, including ordinary device generations and a separate shared
application-key server-device family. The [implementation guide](devices.md)
describes registration, revisions, deletion receipts, alias isolation, and
migrations 17 and 18. This report does not mark the larger M5 administrator
milestone or compatibility release complete.

## Tested artifacts and environment

Functional, database, and browser checks ran through `ssh test-env`. Go
integration tests and the disposable browser database used the isolated
PostgreSQL cluster on port 15432. The browser application ran as UID 995 with
its own database and low-privilege role, using Node 20.19.2 and Playwright
1.63.0. The shared application service was unchanged during that browser run.

The final source contains 354 Go/module files and 18 migrations. The tested
administrator asset archive contains 44 files. Artifact identities are:

| Artifact | SHA-256 |
| --- | --- |
| Accepted Linux executable | `394430272da8ad6268534c1bcaa925ccffc92f61ed0b136a8ec6bbc4dae88541` |
| Administrator asset archive | `b1e632731756592db638ad4d3fd0ef61ea7c4fdedcf717b33d882c976dd5885e` |
| Final Go/module source manifest | `6b6054675e286143c32e86b3400d68a0693f7ae1bba443dcdb178fb11d088f95` |
| Eighteen-migration manifest | `a5ae9d724bea52867ef81f05cd5b24439c46f4208a9eecde6bf1d5d76f1a2d59` |

These identify the accepted test/browser build and final installed artifacts.
The deployment evidence below verifies all 372 Go/module and migration files
against the corresponding manifests.

## Complete race suite and focused regressions

The [final complete race summary](m5e-full-race-summary.json) records
**1085 top-level tests passed across all twelve tested packages**, zero skipped
tests, zero failures, no race findings, and Go exit code 0. The command package
has no tests; its package-level skip entry is not a skipped test. The final log
hash is retained in the summary, alongside the final source manifest.

The [device regression](m5e-device-regression.json) passed 22 top-level tests
across database and identity with zero skips. The [legacy regression](m5e-legacy-regression.json)
passed four top-level tests across identity, server, and subtitle with zero
skips on the final source. These focused results supplement the complete suite;
they are not added to 1085 as extra unique tests.

The exercised device contracts include:

- Schema-16 history preservation through both migrations. Ordinary logins gain
  registry associations; native administrator and application credentials keep
  their distinct scopes. Existing key rows retain their shared generation and
  recoverable ciphertext.
- Native cookie/CSRF isolation, strict 4 KiB JSON and query parsing, decimal
  IDs/revisions, nullable safe DTOs, UTC timestamps, literal search, actual
  snapshot counts, and pagination.
- Custom-name updates and clearing, unchanged-name no-ops, revision conflicts,
  authenticated activity preserving manual overrides, and current-authority
  login counts.
- Ordinary-generation deletion, idempotent receipts, revocation of grouped
  logins, and later registration without reusing the removed generation or its
  custom name. A colliding reported dashboard ID does not revoke a native
  administrator login.
- Shared application-key generations, hidden/native route separation, numeric
  versus reported-alias lookup, self-removal, all-parent key revocation, and
  protection of a newly created generation from retries against old history.
- Real database lock barriers for login/removal and concurrent name changes,
  actor revalidation after waiting, and rollback when an actor expires after
  the credential update. HTTP tests cover grouped WebSocket/HLS retirement
  without retiring an unrelated device's credentials.

## Preserved earlier failures and corrections

The [first focused attempt](m5e-targeted-attempt-1.json) passed 97 top-level
tests and reported nine failed tests, with Go exit code 1. The failure set
included new-device fixture/expectation issues and a real UTC projection
inconsistency. The product now normalizes native device `CreatedAt` and
`LastSeenAt` to UTC for both database rows and JSON-backed list projections;
the regression explicitly checks UTC locations as well as equal instants.
The corrected fixtures exercise valid credential kinds and the actual
registration/management lock ordering. The 22-test device regression and final
complete suite pass after those corrections.

The [first complete attempt](m5e-full-race-attempt-1.json) passed 1081 top-level
tests and failed four top-level groups. Its JSON also lists failing subtests,
so those entries are not seven independent top-level failures. The affected
groups were administrator-session runtime retirement, concurrent managed-user
login invalidation, remote commands targeting a native administrator session,
and observed SRT delivery fixtures.

The concurrency regressions now observe the actual blocked PostgreSQL
connections and account/credential lock sequence. The native-target test uses
a legitimate native login instead of changing an ordinary Emby row's kind,
which would violate the new registry scope constraint. The remote test
snapshot was completed with its missing subtitle reference captures. These
test/environment corrections do not establish a new subtitle or remote-control
feature. The four-test legacy regression and final complete suite both pass;
the failed attempts remain available rather than being relabeled successful.

## Browser journey and restart preservation

The [isolated browser workflow](m5e-devices-browser.json) passed one scenario
in **6.930194 seconds**, with zero unexpected failures, skips, or flaky tests.
Setup used two users, 32 real devices, and 33 real Emby logins, including two
independent target logins. The reported dashboard-ID collision is deliberate;
no credentials were fabricated and the login-limit configuration was unchanged.

The native journey verifies 25-row pagination, literal and UTF-8 search limits,
rename/clear behavior, real revision conflicts, missing-device recovery, and
removal that clamps an empty final page. Both target logins are revoked while
unrelated and native administrator credentials remain authorized. A later
login registers a new generation. A deliberately lost response after a
committed mutation requires explicit refresh; the UI does not automatically
repeat a change with an uncertain outcome.

The restart comparison preserves all rows across **21 public tables**, device
names and revisions, deletion receipts, and native administrator logins.
Removed-device credentials remain denied; retained and replacement credentials
remain authorized. All ten cleanup checks pass, including application and
tagged-process shutdown, private-file/runtime removal, temporary database and
role removal, exact HBA restoration, unchanged preexisting database catalog,
and an unchanged shared application service.

Four secret-free screenshots document the accepted views:
[desktop](screenshots/devices-desktop.png),
[rename dialog](screenshots/devices-rename-desktop.png),
[mobile](screenshots/devices-mobile.png), and
[removal dialog](screenshots/devices-remove-mobile.png). The captured layout
was reviewed without overflow. Private logs exclude raw credentials.

## Main deployment, interruption, and completed recovery

The first deployment preflight refused an existing administrator directory
with mode `0777` and 59 assets with mode `0666`. Permission hardening was
limited to the verified scope; the checked set of 185 file contents remained
byte-identical. This was a deployment-permission correction, not an asset or
product-code change. The [permission audit](m5e-static-permissions.json)
records the exact directory/file scope and unchanged original service PID.

The [foundation staging helper](../../scripts/test-env/run-foundation.sh) now
sets directories to `0755` and files to `0644` after `cp -a`, replacing the
earlier `chmod -R a+rX` that retained source write bits. Remote `bash -n`
passed. The install helper was not rerun for this check, and the syntax check
did not change the shared service.

The next deployment applied schema 18 and installed the candidate executable
and assets, then stopped during source installation because 16 source
directories had modes `0777` or `0775`. The fallback `pg_restore` invocation
failed because it omitted the required `-f` option, leaving the service stopped.
This attempt is not a successful automated rollback or database-restore test.

Recovery independently checked the complete 551-entry backup inventory,
preexisting business fields across the old nineteen tables, the exact ordinary
and shared-device backfills, the master file, environment, and candidate
artifacts. The sixteen verified source directories were set to `0755` without
changing file bytes. Restarting the candidate then reached schema 18 at
PID 3535438. Recovery retained the already migrated database; it did not rely
on a successfully executed database restore. The service was briefly stopped
during this recovery; the deployment was not uninterrupted.

The [final deployment audit](m5e-deployment-evidence.json) passes. Its finishing
wrapper replaces 342 source files, preserves the original complete 551-entry
backup, and verifies all **354 Go/module files plus 18 migrations** byte for
byte. The finishing phase issues no further service start/stop or database
write. The final service is PID **3535438**, UID **995**, start ticks
`23604510`, schema **18**, and probe version **6**. All 44 current administrator
assets and the actual HTTP index/entry bytes match the accepted build.

The migration preserves every preexisting business field across the old
nineteen tables. `sessions.device_registry_id` and both ordinary/shared device
registries match their exact expected backfills. Old application-key numeric
IDs, ciphertext, client contexts, and playback history are preserved, as are
the master file, environment, and service-unit configuration. No media rescan
is issued. Retained old administrator assets remain available to loaded clients.

## Restore-path evidence boundary

The [read-only restore-script check](m5e-restore-sql-generation.json) reproduces
the original `pg_restore` invocation failure with exit code 1. Adding
`--file=-` generates **203,084 bytes** of SQL with exit code 0 and SHA-256
`34d15f69bdaf1c83212d9ca100c1268f011e38525d0371da13e18568f28e7c5a`.
No database connection is requested, no restore SQL is executed, and the
original backup is unchanged.

The corrected operator hash is
`6d96dfeaf2023a668b9a70389313cce1c1345fd74989d081f24532c6eac608ba`.
The five earlier deployment-helper simulations belong to original operator
`ded292de2db8c2fb232d875f38afa25d142c7b61925cd0c93981f965db8d2107`
and exercise refusal/interruption before installation. They do not establish
successful real database restoration. The completed deployment, intact backup,
and successful SQL generation are not full product backup/restore acceptance.

## Deployed device workflow

The [main-service workflow](m5e-deployed-devices.json) passes in **1.824 seconds**,
with 16 GETs, 17 POSTs, one DELETE, and zero HTTP retries. Two real Emby logins
from different users join one reported-device generation. The workflow verifies
the fourteen-field native projection, literal names and clearing, exact search
and pagination counts, `409` for stale updates/deletion, and revocation of both
target logins while an unrelated ordinary login and native cookie remain usable.

A later login receives a new generation and authenticates successfully. A
repeat against the old numeric ID remains idempotent and does not remove the
replacement. Cleanup revokes all **five new credentials** and soft-deletes all
**three new generations**, with no physical row deletion.

All preexisting rows across 21 public tables remain raw-identical, including
users/policies and expired prepared playback history. All eleven known media
sources, the vault, and the historical application-key namespace are unchanged.
PID, start ticks, schema, UID, and executable identity remain unchanged during
this workflow. Shared-key mutations and reported-ID collision cases are covered
by disposable acceptance; this main workflow preserves the historical shared
key rows.

## Evidence boundaries and remaining work

The [ordinary-device reference](../research/devices-reference.md) and
[key-device reference](../research/key-devices-reference.md) are independent
upstream evidence in the 1665-record corpus. Goby's coherent counts, immediate
custom-name projection, and shared-generation recreation policy are explicit
implementation choices. The reference did not sample new-key generation after
shared-device deletion or hidden header-specific device Info/deletion.

Camera upload/history, broader device and Session wire parity, complete client
interoperability, actual GPU execution, and a shipped backup/restore product
remain outside this acceptance. Generic tasks, system settings, online
providers, and audit/log browsing also remain open. M5e is a completed
increment; M4, M5, M6, and the complete planned server remain unfinished.
