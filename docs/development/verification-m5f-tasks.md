# Scheduled-task verification

M5f adds durable library-wide tasks, administrator scheduling, and the six
Emby scheduled-task operations. The full Linux Go regression suite and isolated
browser acceptance, deployment, and deployed task workflow have passed. This is an
engineering increment, not complete Emby-client or release acceptance.

## Accepted Go source

The [full race report](m5f-full-race-summary.json) records **1190 passing
top-level tests across 13 tested packages**, no skipped tests, and no race
warning. `cmd/goby` has no test files. The command ran only through
`ssh test-env`:

```sh
go test -race -count=1 -json -p 2 -timeout 20m ./...
```

The accepted snapshot is `/opt/goby-test/verify-m5f-core-attempt-5`, with
386 Go/module inputs and all 19 migrations. Its complete input manifest was
compared against the current workspace before the candidate was accepted.
Go 1.27.1 built the Linux/amd64 candidate with `CGO_ENABLED=0` and `-trimpath`.
The final source gate and full raw test log remain on the test host.

The [35-test core check](m5f-core-targeted-tests.json) and
[44-test initial task check](m5f-tasks-initial-tests.json) are earlier focused
checkpoints. The final full suite supersedes them for source acceptance.

Coverage includes schema-18-to-19 preservation, scoped foreign keys,
transactional administrator revalidation, durable request replay, coalesced
admissions, owner loss, scanner queue capacity, independent scan isolation,
shutdown and recovery, actual startup/interval dispatch, maximum runtime,
calendar previews, DST gaps and folds, strict HTTP inputs, and both protocol
surfaces. Tests use the isolated PostgreSQL cluster on port 15432.

## Retained failures

The first targeted HTTP attempt exposed conflicting Go `ServeMux` POST
patterns. Its fixture had registered application cleanup after constructing
the handler, so the panic also left the database pool waiting on the catalog
owner. The route dispatcher was corrected, and fixture cleanup now registers
immediately after application construction. The exact owned test process was
interrupted to retain its stack trace; the separately identified empty test
schema was privately backed up and removed. The main service was unchanged.

The next targeted attempt passed 20 tests and failed one DTO test because it
compared a typed nil inside an interface rather than the serialized JSON.
The test now checks the wire representation. The DTO also excludes disabled
tasks and paused calculation-error rules when choosing the next run time.
Both corrections are included in the passing full suite.

Browser attempt 1 was rejected before creating a database because the
executable-evidence tmpfs had less than the required 128 MiB free. Its
[report](m5f-tasks-browser-attempt-1.json) is retained. The owned tmpfs was
expanded from 512 to 768 MiB without deleting files, changing its directory
identity, or affecting the shared service; see the
[capacity report](m5f-scratch-growth.json).

Browser attempt 2 reached administrator setup but failed during fixture
initialization. Its [report](m5f-tasks-browser-attempt-2.json) proves that the
temporary session, database, role, processes, and HBA modification were cleaned
up. The fixture's two media subdirectories had inherited mode `0700` from the
operator's `umask 077`; explicitly restoring their intended root-owned `0755`
mode fixed access for UID 995. No product change was needed for this failure.

## Browser and restart acceptance

The [third browser attempt](m5f-tasks-browser.json) passed with the same product
binary and assets as the accepted Go source. Chromium completed the journey
in 19.078 seconds with no unexpected, flaky, or skipped tests. It exercised
real manual and interval runs, library counters, legacy scan history,
calendar precision, optimistic-revision conflicts, committed requests whose
responses were lost, explicit reload/retry, mobile layout, and the unsaved
schedule guard.

The isolated fixture contained two real MP4 files in two libraries, created
through native APIs without an initial scan. The first restart preserved all
27 public tables exactly before any authenticated HTTP request; the original
browser cookie, CSRF identity, run history, and request receipt remained valid.
The second restart executed exactly one newly installed startup trigger,
producing one run, two owned children, two linked scans, and one occurrence.
Only their documented dispatch and scan watermarks advanced. Existing history,
request receipts, catalog content, account state, and source media were preserved.

The browser observed the active stop dialog but did not execute an active
cancellation. Real active cancellation is covered by the Go/HTTP tests; this
report does not attribute that coverage to the browser.

Both issued native sessions were revoked, task rules were cleared, work was
drained, and the temporary processes, database, role, and credential-bearing
runtime files were removed. The exact prior HBA and database inventory were
restored. The shared service retained its original M5e PID throughout this
isolated workflow.

The four accepted screenshots were inspected for desktop and mobile layout:

- [Run detail](screenshots/tasks-desktop.png)
- [Schedule editor](screenshots/tasks-schedule-desktop.png)
- [Mobile schedule editor](screenshots/tasks-mobile.png)
- [Existing scan history](screenshots/tasks-scan-history.png)

| Candidate | Accepted value |
| --- | --- |
| Linux/amd64 executable SHA-256 | `2993870cce6f4e0ae2c3630b645985cab664ce5e5e18123fdc920fb241bb2abf` |
| Administrator archive SHA-256 | `b432bbaa8363016ab7a6de2e73b82765c7e01a913ae1f2788d7eccf0595cff76` |
| Administrator asset files | 47 |
| Browser verifier SHA-256 | `c9e62cb4aa022eb7f28da973881916459ba3466b5f3e8e97dc8f2f6c47538f27` |

## Actual daily and weekly firing

A separate [native calendar workflow](m5f-calendar-firing.json) passed in
6.966 seconds with the same accepted binary. The
[calendar verifier](../../scripts/test-env/verify-task-calendars.py) used a fresh
isolated two-library fixture and one native administrator login. It configured
daily and weekly UTC rules sequentially, each approximately three seconds
ahead of the database clock, without changing that clock or writing task rows
directly. Each rule caused its own real completed scheduled run and two owned
library scans.

Read-only database checks bound each run to its saved trigger, revision, and
single occurrence. The recorded next daily time advanced by one day; the next
weekly time advanced by seven days, both matching the second previewed
occurrence. Both rules were cleared, all work settled, and the credential,
database, role, temporary runtime, and HBA change were cleaned up. The main
service remained unchanged. This additional workflow neither opened a browser
nor restarted a service and adds no official-reference records. It supplies
real daily/weekly execution evidence alongside the existing startup/interval
checks, without claiming Emby calendar or DST parity.

## Deployment and recovery preparation

The [deployment evidence](m5f-deployment-evidence.json) records the accepted
service at PID **3570491**, UID **995**, schema **19**, and probe version **6**.
All 405 installed Go/module/migration inputs and 47 current administrator assets
match the accepted candidate. Older immutable assets are retained separately;
they are not included in the current asset count.

Before stopping the old service, the operator captured all 21 old public tables
and a PostgreSQL dump from the same exported snapshot. It imported the complete
recovery SQL into a new disposable database as the application role, compared
every restored row exactly, and removed that temporary database. The protected
backup was complete and verified before the service stopped. The database was
checked again after stopping, before installation, to prevent discarding any
write accepted while the backup was being prepared.

Migration and startup preserved all old business fields, all 11 source media
files, and the matching application-key master, runtime environment, and systemd
configuration. Old scans have a null new task-child association. Startup created
the single real task definition with empty trigger and execution tables; the
deployment operator did not request a scan.

The successful recovery bundle is root-private at
`/opt/goby-test/backups/m5f-20260910-attempt-2`. The completed deployment marker
prevents later automatic restoration of its older data. Recovery before a
proven installation start can only preserve and restart the unchanged old
service. This operator rehearsal is not a user-facing backup/restore feature,
and no recovery SQL was executed against the main application database.

The [eight pure operator tests](m5f-deployment-operator-tests.json) separately
cover contract and failure-path behavior. Their report explicitly distinguishes
synthetic checks from the real database restoration above. Earlier operator
source and failure evidence remain on the test host. The first live attempt
refused an incorrect assumption that the existing systemd unit was mode `0644`;
it actually remained root-owned `0600`. The next attempt exposed PostgreSQL's
string JSON representation of `oid`; explicitly casting it to `bigint` corrected
the rehearsal identity assertion. Both failures occurred before the old service
stopped. The temporary rehearsal database was removed, and the incomplete first
backup was retained without overwriting it.

## Deployed task workflow

The [main-service workflow](m5f-deployed-tasks.json) passed on its first attempt
in 1.618 seconds. It created one native administrator login and one ordinary
Emby administrator login on a new reported device. One native request admitted
one normal task with five owned library children and five reciprocal scan-job
links. All 11 existing media files passed the scanner's cache preconditions
and completed without new or updated media items. Replaying the original
request receipt returned the same terminal run without admitting new work.

Native and Emby reads identified the same real definition, and the Emby last
execution result used the definition ID. Two distant daily/weekly rules were
previewed, stored, read back, and then conditionally restored to the original
empty schedule and timezone. A stale revision returned `409`. This main-service
workflow did not exercise automatic firing or restart; the isolated workflow
above provides that separate evidence.

Old media items, user data, metadata layers, keys, devices, prior credentials,
scan history, and playback rows retained their fields. The only old item change
was `updated_at` on five preexisting directory items processed by their completed
owned scans, consistent with the existing scanner's directory upsert behavior.
Each eligible directory's type, root/library/parent/path identity and every
other field remained exact; each timestamp advanced only inside its scan's
recorded time window. The five libraries' scan watermarks and the selected
definition's acknowledged revision/update timestamps also advanced as expected.

Both new credentials were logged out and independently received `401` from
their protected endpoints. Their two session rows, the one new device record,
one task run, one request receipt, five children, five scans, and two retired
trigger rows remain as history. No history was physically deleted. Source
media, the master, and the accepted service identity remained unchanged.

## Reference evidence and limits

The [fresh reference study](../research/scheduled-tasks-mutation-reference.md)
adds 171 records to the existing 1794-record corpus: 169 complete HTTP
exchanges, one audit, and one initial connection-refused readiness observation.
These are reference observations, separate from Goby verification. The fresh
Emby instance and its synthetic media copies were removed after trigger
restoration and credential revocation; private evidence was retained.

Goby supports interval, daily, weekly, and startup schedules with explicitly
documented native time semantics. The study does not establish reference
parity for weekly execution, timezone/DST interpretation, misfires, or maximum
runtime. `SystemEventTrigger` is rejected because its Linux event execution
is not implemented. Task progress measures completed library children and
does not estimate remaining file-processing time. Only the real library scan
definition is advertised.

No web player is introduced. The React/MUI dashboard provides administrator
task controls and the existing scan history. Broader clients, task types,
settings, providers, audit/log browsing, and product backup/restore remain
outside this increment.
