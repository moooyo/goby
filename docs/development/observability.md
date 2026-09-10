# Activity and diagnostic log implementation

**M5i increment complete and deployed: schema 22/probe 6.** The
[complete remote race suite](m5i-full-race-summary.json) passed 1380 top-level
tests across 17 tested packages with zero skips or race findings.
[Browser/restarts](m5i-observability-browser.json),
[source/build reconciliation](m5i-final-go-source-gate.json),
[protected deployment](m5i-deployment-evidence.json), and the
[main-service workflow](m5i-deployed-observability.json) also passed. The separate
[Emby reference study](m5i-observability-reference.json) remains research
evidence. See the [API contract](../api/observability.md). This completion covers
the declared activity/log increment; M4, M5, M6, broader administration and
complete Emby compatibility remain unfinished.

## Storage responsibilities

[`internal/activity`](../../internal/activity) records committed administrative
facts in PostgreSQL. [`internal/diagnostics`](../../internal/diagnostics) writes
sanitized operational JSONL files to a dedicated Linux directory. Activity
records do not come from HTTP success logs, and diagnostic files are not the
transactional audit store.

Migration [0022_activity_entries.sql](../../internal/database/migrations/0022_activity_entries.sql)
adds `activity_entries`, the 29th application table at schema 22. It starts
empty on an existing installation: it does not invent history from business
rows. The media probe cache version remains 6. The table uses a generated
bigint identity and a `clock_timestamp()` timestamp. Checks constrain action,
severity, source, actor/resource kinds, identifiers, counts, revisions, terminal
states, and permitted changed-field names. Date/ID, actor/date, action/date,
and severity/date indexes support the implemented read paths.

Actor and resource IDs are retained values rather than foreign keys. Deleting
a business resource cannot cascade-delete its audit history. A user's display
name is resolved from the current user row when reading; stored facts do not
contain historical display names. The native projection omits the internal
credential and correlation fields. The table is append-only in normal business
code until retention deletes expired entries; it is not a cryptographically
tamper-evident archive.

## Transactional activity

[`activity.Record` and `RecordOwned`](../../internal/activity/store.go) require
the caller's existing transaction. They neither acquire authority nor begin,
commit, or publish a separate transaction. Callers return insertion failures
so a failed activity insert rolls back the business change as well. Settings,
catalog, task, device, session-management, and application-key paths retain
their transaction-specific current-authorization checks; where a final check
exists, the activity insertion precedes it.

Managed-user operations retain their established locked authorization and
self-mutation semantics. An authorized self-demotion, self-disable, password
reset, or self-revocation must not be rolled back merely because its own
intended effect removes the actor's authority. Audit integration does not add
a generic post-mutation authorization rule over these paths.

The [API action table](../api/observability.md#native-activity-query) lists the
21 implemented actions. Their owners are:

| Owner | Integration |
| --- | --- |
| [`identity`](../../internal/identity/activity.go) | Bootstrap/account changes, successful login issuance, explicit session revocation, application-key create/reveal/revoke, and device changes |
| [`library`](../../internal/library/activity.go) | Library changes, scan admission/cancellation/terminal transitions, and metadata changes |
| [`settings`](../../internal/settings/activity.go) | Native and compatibility changes to supported raw settings |
| [`tasks`](../../internal/tasks/activity.go) | New manual/scheduled admissions, cancellation, first terminal transitions/recovery, and schedule replacement |

An authenticated user actor stores its user ID and existing session-row ID.
An application-key actor stores the decimal key-sidecar ID and credential-row
ID, never its token or a fabricated user identity. Explicit background and
scheduler operations use a system actor; bootstrap records a system actor with
native source. Other sources distinguish native, Emby, and background work.

Changed fields are sorted, unique names from a finite schema. Settings compare
raw presence/value, name mode, and compatibility encoding width; resetting an
override can be recorded even when the effective value stays the same.
Metadata compares override and lock layers and includes their affected field
names. Account and schedule revisions retain their original semantics:
successful equal-value account updates and schedule replacements are still
real revision changes. Settings/metadata no-ops and repeated terminal,
cancellation, revocation, or admission-replay paths do not duplicate those
facts. Bulk credential revocation is represented by its owning account/device
operation; clients must not infer a separate activity entry for every changed
session row.

Application-key reveal entries mean that decryption succeeded and the secret
was prepared for authorized display in the transaction. They do not claim
network delivery. A compatibility key-list read that reveals active tokens
records each actual reveal; native metadata-only listing does not imply a
reveal. Repeated legitimate reveal requests remain distinct operations.

Failed terminal scan/task transitions use `Error`; interrupted recovery uses
`Warn`; successful/cancelled terminal transitions use the default `Info`.
`Name` and `Overview` are fixed descriptions generated from the action/state,
without request values or arbitrary error messages. Server process start/stop
events belong to diagnostics and do not manufacture activity entries.

[`QueryOwned`](../../internal/activity/query.go) obtains the filtered total and
page through one SQL statement snapshot, ordered by database date and ID
descending. Its inclusive `MinDate` lower bound rounds upward to microseconds.
The HTTP activity read authenticates before interpreting filters and rechecks
current authorization inside the catalog-owned transaction before returning.

The [retention worker](../../internal/server/activity_retention.go) waits for a
one-minute ticker, then attempts one batch of at most 1000 expired rows. It
uses a separate pool transaction, database time, a five-second operation
context, and `FOR UPDATE SKIP LOCKED`; it does not hold the catalog owner or
business-row locks. There is no immediate startup purge or hard total-row
ceiling. Errors emit a fixed safe retry diagnostic. Shutdown cancels the
worker and waits within the caller's close context. The startup retention
default is 30 days, configurable from 1 through 365 days.

## Safe diagnostic records

[`diagnostics.NewHandler`](../../internal/diagnostics/handler.go) sanitizes the
record before both file persistence and fallback output. A configured store
enables Info and higher levels; other levels follow the fallback handler's
policy. Unknown messages become `unclassified event` with an `unclassified`
event code and without arbitrary attributes.

The handler uses fixed message/event templates and an attribute allowlist per
event. Allowed values are further constrained: internal IDs, bounded integers,
finite enums, version syntax, and registered route templates. Unknown route
strings become `unmatched`. Raw URLs, query strings, headers, cookies, bodies,
filesystem paths, client names, and arbitrary strings are not retained merely
because they appear in a log call. Errors are reduced to fixed classes without
calling arbitrary formatting methods. `Stringer`, `LogValuer`, arbitrary `Any`
payloads, and caller program counters are not serialized. Attribute count,
group depth, and inspected values are bounded; final JSONL records cannot
exceed 8192 bytes including their newline.

[`cmd/goby/main.go`](../../cmd/goby/main.go) starts with the same safe handler
for bootstrap output, opens diagnostic storage before connecting to the
database, and passes that store into the server. The persistent and fallback
handlers share the raw fallback instead of nesting sanitized handlers. The
global slog logger and HTTP server error logger use this pipeline. Listener
creation precedes `server listening`; final lifecycle diagnostics follow
HTTP/application/database cleanup, and the diagnostic store closes last.

[`request_logging.go`](../../internal/server/request_logging.go) records
`request.completed` with a server-generated request ID, allowed method,
registered route pattern, status, duration, byte count, and outcome. It does
not capture URL path values or request data. The response wrapper preserves
the underlying HTTP interfaces, including `Unwrap`, hijacking, and
`FlushError`, so streaming and download cancellation retain their behavior.
Byte counts include bytes accepted by the HTTP response writer; bytes written
directly after a protocol hijack are outside that count. An abort before a
response commits can omit status, and unrecognized HTTP methods are omitted.
These request diagnostics report HTTP handling, not business-transaction
commit evidence.

## Linux file ownership, rotation, and quotas

[`store_linux.go`](../../internal/diagnostics/store_linux.go) pins the configured
directory descriptor and traverses path components without following symlinks.
The final directory must belong to the effective service UID with mode `0700`.
Only the final path component can be created by the store; its parent must
exist and be accessible. A new store accepts an empty directory, while reuse
requires its valid ownership marker and registry. It does not import existing
Emby files, arbitrary text files, or journald history.

An exclusive nonblocking process lock prevents concurrent writers. Registered
files must be service-owned, single-link regular files with mode `0600` and
the expected device/inode identity. The store opens and deletes relative to
its directory descriptor, rejects substituted symlinks/hardlinks/FIFOs, and
never treats an API filename as an arbitrary filesystem path. Marker and lock
identity are checked during normal operations. Opaque generated filenames are
listed through the API; they should not be guessed or renamed externally.

The active file rotates before a write would exceed the configured byte limit,
or on the first write after the UTC date changes. Rotation is not a timer that
creates an empty file at midnight. A process open creates a new active file.
Closed files are immutable; retention deletes closed registered files by age
from their creation time or to reserve a slot within `MaxFiles`. The active
file counts toward that limit. Open, append, and list operations can perform
retention; an idle process does not have a separate log-retention timer.

Defaults are 4 MiB per file, 16 named files, seven days, and a 32 MiB free-space
reserve. The reserve check includes the upcoming write and manifest headroom.
The maximum supported file/snapshot size is 64 MiB, and at most eight snapshots
can be open. Retention can unlink a file while an existing descriptor still
serves its snapshot; that storage remains allocated until the reader closes.
Therefore `MaxFiles * MaxFileBytes` bounds named file payload, not every byte
held by metadata or open descriptors. Allow space for those readers as well.

The registry persists deletion intent before unlinking. Startup recovers an
interrupted owned deletion and can trim an incomplete trailing record from
the previous active file before closing it. This is recovery of registered
state, not adoption of arbitrary orphan files. Complete records are synced;
a partial-write failure attempts to restore the previous line boundary.

Write, sync, space, registration, or identity failures mark the store degraded.
The degraded state is sticky until an explicit close/reopen; it does not
silently claim recovery after the next successful write. File reads then fail
closed with `503`, while sanitized fallback logging can continue. When a
diagnostic store is configured, its unhealthy state also makes `/readyz`
return `503 diagnostics_not_ready`. Failure to open the configured store at
startup prevents the service from starting.

## Reads, transfer lifetime, and dashboard

The [native handlers](../../internal/server/observability.go) use independent
short pool transactions for authorization before and after filesystem work.
They do not retain catalog ownership or account row locks while reading files.
The store's [snapshot](../../internal/diagnostics/snapshot.go) pins a registered
descriptor and byte length. Lines scan at most 64 MiB while retaining no more
than 500 records in the response. A malformed or incomplete line fails the
read and degrades the store rather than returning partial JSONL as successful
data.

The [download handler](../../internal/server/observability_download.go) supports
HEAD, conditional responses, and byte ranges over that fixed snapshot. It
enforces a 60-second maximum transfer lifetime or the earlier credential expiry,
rechecks authority once per second, and uses cancellation to close the reader
and interrupt a blocked network write. Authorization checks have their own
short contexts. The final buffered bytes are flushed while the watcher and
write deadline are still active; watcher/cancellation callbacks finish before
the deadline is reset. An unsupported write deadline produces `503`, and a
failure after response start aborts the stream. Concurrent snapshot closes
wait until descriptor cleanup and reader-slot release have completed.

The [React/MUI page](../../web/admin/src/ObservabilityPage.tsx) offers Activity
and Server logs tabs, with filters, pagination, current policy, expandable
activity details, escaped log text, and native-cookie attachment links.
Activity/file pages offer 25, 50, 100, or 200 items. Line preview offers 10, 50,
or 200 lines. The UI cancels replaced requests and ignores stale results,
handles a rotated-away file as `404`, and uses the existing API request layer
for expired-session handling. Native attachment navigation does not itself
run that SPA request layer, so a failed download alone does not promise an
immediate in-page sign-out transition. There is no playback page or arbitrary
log-file editor.

## Compatibility adapter boundaries

[`observability_emby.go`](../../internal/server/observability_emby.go) registers
four GET adapters over the same stores. It preserves the studied administrator
and application-key GET access, viewer `ManageServer` denial, compact JSON
envelopes, numeric int64 DTO fields, and seven-fractional-digit UTC timestamps.
Its documented paging deliberately differs from native paging: Activity's
omitted Limit reports a zero total, file-list totals survive beyond-end pages,
and Lines defaults to empty items with a real snapshot line count. The
[API compatibility section](../api/observability.md#compatibility-surface)
contains the complete bounds, projection, error text, and default rules.

The adapter's fixed activity descriptions do not interpolate users' names or
request values. It exposes real Goby user associations, not fabricated Emby
internal numeric user identities. `session.login` and `user.password_reset`
map to their observed compatibility Type spellings; other Types preserve
their actual stored action. File creation timestamps retain registered Goby
creation time, even though the reference's active-file dates changed together.

Malformed inputs receive fixed `400` errors and missing registered basenames
receive `404`, rather than reproducing the reference's reflected parsing and
missing-file `500` errors. Queries remain bounded and closed to unestablished
filter fields. `MinDate` uses Goby's explicit inclusive microsecond rule; the
reference's unresolved 100 ns boundary behavior is not an inferred algorithm.
Every `Sanitize` mode serves the same safe registered JSONL content. Private
cache policy, attachment framing, fixed byte snapshots, ranges, and bounded
revocation handling are product guarantees requiring their own verification.

Compatibility HEAD returns fixed `404` before GET authentication for all four
routes; native HEAD retains its GET representation behavior. Only the reference
log-download HEAD case was sampled, and its unavailable body is not claimed
as evidence for Goby's fixed text. Canonical route paths also pass through the
existing root namespace alias; no unmeasured case-variant compatibility is
added by this increment.

## Linux deployment configuration

The [startup configuration](../../internal/config/observability.go) is separate
from database-managed server settings. These values are read at process start
and require restart to change; neither Settings nor the observability page
persists them.

| Environment variable | Default | Valid value |
| --- | --- | --- |
| `GOBY_LOG_DIR` | `/var/log/goby` | Clean absolute dedicated Linux path, not `/`, at most 4096 bytes, no NUL |
| `GOBY_LOG_MAX_FILE_BYTES` | `4194304` | Decimal integer `8192..67108864` |
| `GOBY_LOG_MAX_FILES` | `16` | Decimal integer `1..256` |
| `GOBY_LOG_RETENTION_DAYS` | `7` | Decimal integer `1..365` |
| `GOBY_LOG_MIN_FREE_BYTES` | `33554432` | Decimal integer `1..1099511627776` |
| `GOBY_ACTIVITY_RETENTION_DAYS` | `30` | Decimal integer `1..365` |

An absent or empty environment variable uses the default. Explicit numeric
zero is invalid at environment load. Configuration validation does not probe
the filesystem; opening the store performs the live ownership checks.

The [systemd unit](../../deploy/linux/goby.service) provisions the default log
directory for its service user, including under `ProtectSystem=strict`:

```ini
[Service]
User=goby
Group=goby
LogsDirectory=goby
LogsDirectoryMode=0700
UMask=0077
```

The [environment example](../../deploy/linux/.env.example) declares the same
defaults. For a custom `GOBY_LOG_DIR`, provision that dedicated directory with
the actual service UID and mode `0700`, and grant the path write access in the
service sandbox, for example with a scoped `ReadWritePaths=` entry. Do not use
a shared log directory or run two Goby processes against one registry. Existing
markers, locks, and files must retain their ownership, permissions, names, and
identities. External rotation or file replacement is incompatible with this
store's registry; Goby performs its own rotation and retention.

Schema migration and operational deployment must retain current business
state, add the empty audit table, and provide the private writable log directory
before starting the new binary. The [accepted deployment](m5i-deployment-evidence.json)
verified the actual service UID, permissions, sandbox, readiness, retained
settings/media, and safe diagnostic output. The new table was empty immediately
after upgrade; the [live workflow](m5i-deployed-observability.json) subsequently
verified six real committed activity records.

## Reference evidence and verification status

The [M5i reference report](m5i-observability-reference.json) describes Emby Server
4.9.5.0 using an isolated owned instance: 15 setup records, 76 capture HTTP
attempts, one correspondence audit, and four operator-cleanup records, totaling
96 new safe exports and a corpus of 2462 records. This comprises 94 complete
HTTP exchanges, one initial connection-refused readiness record, and one audit;
the readiness failure is not counted as a completed HTTP exchange. It records byte correspondence,
complete bodies, a caused user-creation activity, credential invalidation,
owned-process cleanup, and preservation of prior services/evidence. Its
23 reference guards are reported separately in the same evidence. These are
research and recorder results, not product API, browser, upgrade, or deployment
results.

Current source includes meaningful tests for activity validation/rollback,
query consistency and retention; identity/catalog/settings/task transaction
integration; diagnostic redaction, Linux ownership, rotation, recovery and
reader cleanup; native and compatibility authorization, DTO precision, and interrupted downloads;
configuration bounds; and request logging. Relevant starting points are
[`activity`](../../internal/activity), [`diagnostics`](../../internal/diagnostics),
and the [native](../../internal/server/observability_native_test.go),
[authorization](../../internal/server/observability_authorization_test.go),
and [download](../../internal/server/observability_download_test.go) test files,
plus the [compatibility contract](../../internal/server/observability_emby_test.go)
and [compatibility authorization](../../internal/server/observability_emby_authorization_test.go) tests.
The complete remote result is linked below; the existence of test files alone
is not acceptance evidence.

The [complete M5i race suite](m5i-full-race-summary.json) passed **1380 top-level
tests across 17 tested packages**, with Go exit 0, zero skips, and no race
findings. It ran against `/opt/goby-test/verify-m5i-full-attempt-2`, retaining
the same candidate source and binary. The complete log SHA-256 is
`decfdd6344bd8d16d0a06c88f1cc411497d9d6b22d9096b482211dbce9431b53`.
The [source gate](m5i-final-go-source-gate.json) reconciles 535 inputs: 470
Go/module/SQL files, ten test-data files, and 55 browser inputs. It binds the
same tested Linux Go 1.27.1 `CGO_ENABLED=0` executable and 54 current assets to
the passing full-suite and browser reports. The executable SHA-256 is
`1ead2fcaa22df227d3d8b6b607978887ccfee7868139fc38f40523dbe24752ef`, and the
asset archive SHA-256 is
`20ed0e16b721515f1e56dddeef818e3102a7c92f25f2a1d8d08aa1d2b756f85b`.

The [real browser acceptance](m5i-observability-browser.json) passed in
12.400443 seconds, with 11 explicit scenario checks and no skips or retries.
It exercised activity filters/pagination/details, log list and line paging,
cookie-based download, stale-response cancellation, mobile layout, and
self-revocation. Missing-file/unavailable-response UI cases used controlled
injection; they are not a claim that the browser physically damaged storage.
The independent runner verified real file rotation, immediate absence of each
body/header/path/query sentinel before later rotation, 16 completed downloads
reusing bounded reader slots, and two restarts. Each restart retained all 29
public tables exactly, including 66 activity entries, and recovered registered
logs for download. Both native credentials were revoked with independent HTTP
401 and SQL proofs; the temporary database, role, processes, and HBA rule were
cleaned up. The four reviewed screenshots show
[desktop activity](screenshots/m5i/observability-activity-desktop.png),
[activity details](screenshots/m5i/observability-details-desktop.png),
[desktop logs](screenshots/m5i/observability-logs-desktop.png), and
[mobile administration](screenshots/m5i/observability-mobile.png).

The [protected deployment](m5i-deployment-evidence.json) upgraded schema 21 to
22 after a complete backup and a real isolated restore rehearsal proved all
28 old tables raw-exact. The live database was not restored. Migration added
the 29th table empty, preserved all old business fields and settings revision
6, and retained media, the master file, prior runtime/unit settings, and the
original reference process. The owned `30-observability.conf` drop-in supplies
the private `/var/log/goby-test` directory, mode `0700`, UID 995. The current
service is PID **3668655**, start ticks `26912384`, schema **22**/probe **6**.
Its candidate manifest SHA-256 is
`bf3902d5744fed5136296197f08c8e009ab4a2b496c00e19dea2eaa865dfa5af`.
The completed backup at `/opt/goby-test/backups/m5i-20260910` must not be
restored over the newer accepted business state.

The [main-service workflow](m5i-deployed-observability.json) passed in 0.776
seconds with 17 GETs, two HEADs, three POSTs, two PUTs, one DELETE, zero transport
retries, and 20 read-only verifier SQL queries. One new native and one new
ordinary Emby credential read the two API surfaces. Two native CAS writes
changed the name and restored the complete original settings; revision advanced
from 6 to 8 while every original name/override/encoding value was retained.
Native HEAD/range and compatibility HEAD 404 were verified, then both credentials
were logged out and independently rejected with 401. Old rows were preserved
apart from the acknowledged settings revision/timestamp advances. Two revoked
sessions, one device, and six activity entries remain as new history. The
workflow made no media/planning, scan, task, or history-deletion request and
did not restart the service. It does not claim rotation, retention deletion,
or mid-download revocation on the main service; those boundaries have separate
core/browser coverage.

The [first browser attempt](m5i-observability-browser-attempt-1.json) remains
retained: its test instrumentation was corrected before repeating the same
Go binary and production UI. The [first deployment attempt](m5i-deployment-attempt-1.json)
rejected an asset-archive filename contract before stopping the service. The
operator's `.tar`/`.tar.gz` literal and its fixture were corrected, all 33 pure
operator guards passed again, and the final deployment operator SHA-256 is
`46760ebc18e11a87e32a1b9d098b98cd98dbd6ee77b5ebbea431d14de08a52fb`.
These failed attempts remain historical evidence; neither required a Go or
production UI change.

The following intermediate checks have passed on their captured source
snapshots. They are retained progress evidence and must not be relabeled as
the complete acceptance result for a later final source tree:

| Stage evidence | Recorded result | Scope limit |
| --- | --- | --- |
| [Core race attempt 2](m5i-core-race.json) | 79 top-level tests passed | Activity, diagnostics, database, configuration, and command packages on that snapshot |
| [Domain/native HTTP attempt 2](m5i-domain-http-race.json) | 68 selected top-level tests passed | Targeted domain and native HTTP coverage; not the complete server suite or final compatibility acceptance |
| [Browser-runner guards](m5i-browser-guards.json) | 29 tests passed | Synthetic in-memory guards; zero HTTP, subprocess, database, or capture effects; real browser acceptance is false |
| [Deployment-operator guards](m5i-deployment-operator-tests.json) | 33 tests passed | Synthetic in-memory contracts; real database restore and deployment acceptance are false |

The intermediate build checks remain historical steps; the source gate and
runtime evidence above establish the accepted executable and 54 current assets.

| Acceptance area | Current documentation status |
| --- | --- |
| Official reference capture and recorder guards | Passed reference research; see the linked report |
| Go and React/MUI source/build-evidence reconciliation | Passed: [535 source/test/browser inputs, tested executable, and 54 assets](m5i-final-go-source-gate.json) |
| Complete integrated remote Go tests and race checks | Passed: [1380 top-level tests, 17 tested packages, zero skips/race findings](m5i-full-race-summary.json) |
| Native/Emby HTTP acceptance | Passed: full Go suite and [main-service API workflow](m5i-deployed-observability.json) within their recorded scopes |
| Real browser behavior, pagination, downloads, revocation, and restarts | Passed: [11 scenario checks and independent runner/restart evidence](m5i-observability-browser.json) |
| Schema-21 upgrade, Linux log provisioning, and protected deployment | Passed: [28-table restore rehearsal and schema-22 deployment](m5i-deployment-evidence.json) |
| Main-service settings/activity/log workflow and credential cleanup | Passed: [0.776-second workflow](m5i-deployed-observability.json) |

Product tests, validators, runtime/media probes, and browser acceptance run
only in the authorized remote `test-env`. Historical M5h product evidence remains
separate from the accepted M5i evidence. Product backup/restore, more task and
configuration features, broader wire equivalence, actual GPU execution, and
full client acceptance remain outside this completed increment.
