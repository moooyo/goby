# Activity and diagnostic log API

**M5i increment complete and deployed: schema 22/probe 6.** The
[complete remote race suite](../development/m5i-full-race-summary.json) passed
1380 top-level tests across 17 tested packages with zero skips or race findings.
[Browser/restarts](../development/m5i-observability-browser.json),
[source/build reconciliation](../development/m5i-final-go-source-gate.json),
[protected deployment](../development/m5i-deployment-evidence.json), and the
[main-service workflow](../development/m5i-deployed-observability.json) passed.
The [official reference study](../development/m5i-observability-reference.json)
remains separate Emby research. This completion covers the declared activity/log
increment; M4, M5, M6, and complete Emby compatibility remain unfinished. See the
[implementation notes](../development/observability.md) for storage, deployment,
and acceptance boundaries.

Activity records describe committed business operations. Diagnostic files
contain bounded, sanitized service events. They have independent storage and
retention policies. The React/MUI administrator page at `/admin/observability`
reads both surfaces; it provides no media player.

## Native routes and authority

| Method and route | Result |
| --- | --- |
| `GET /admin/v1/activity` | A filtered page of committed activity |
| `GET /admin/v1/logs` | A page of registered diagnostic files and effective storage policy |
| `GET /admin/v1/logs/{name}/lines` | A page of complete JSONL lines from one byte snapshot |
| `GET /admin/v1/logs/{name}/download` | An attachment from one byte snapshot |

These routes require a current administrator session in the `goby_session`
cookie. An Emby token or application key does not substitute for the native
administrator audience. Authentication precedes query and filename validation.
GET and HEAD need no native CSRF header. Download additionally rejects
`Sec-Fetch-Site: cross-site` and any supplied `Origin` that differs from the
configured public URL.

GET registration also supports HEAD with the same native authorization and
representation headers but no response body. Every native route sets
`Cache-Control: no-store` and `Pragma: no-cache`, including errors. JSON responses
use `application/json; charset=utf-8`. Send no request body: these handlers do
not consume bodies, and an advertised body causes the HTTP/1 connection to be
closed rather than awaited during response flushing.

Query names and enum values are case-sensitive. List and line queries accept
at most 4096 raw bytes, valid UTF-8, exactly one value for each recognized field,
and no decoded control characters. Unknown names, duplicate names, malformed
encoding, and empty supplied filter values fail with `400 invalid_input`.
Pagination integers use canonical unsigned decimal spelling: `0` is valid for
`StartIndex`; `+1`, `01`, whitespace, and an empty string are invalid.

## Native activity query

`GET /admin/v1/activity`

| Query field | Default | Accepted value |
| --- | --- | --- |
| `StartIndex` | `0` | Integer from `0` through `2147483647` |
| `Limit` | `50` | Integer from `1` through `200` |
| `MinDate` | No date filter | RFC 3339 timestamp with an explicit offset; fractional seconds are accepted |
| `Severity` | All | `Debug`, `Info`, `Warn`, `Error`, or `Fatal` |
| `Action` | All | One action from the table below |
| `ActorId` | All | Actor identifier, at most 256 ASCII bytes; first character alphanumeric, remaining characters alphanumeric or `._:-` |

All supplied filters are combined with AND. A well-formed unknown actor ID
simply matches no records. The result sorts by `Date`
descending, then numeric activity ID descending. The page and matching total
come from one SQL statement snapshot. `MinDate` is inclusive. A submicrosecond
lower bound is rounded upward to PostgreSQL microsecond precision, so a record
earlier than the requested instant is not accidentally included. UTC years
outside `1..9999` are invalid. A start beyond the result returns an empty
`Items` array with the filtered total intact.

The following is an illustrative response, not a captured verification result:

```json
{
  "Items": [
    {
      "Id": "42",
      "Date": "2026-09-10T09:00:00.123456Z",
      "Action": "settings.updated",
      "Severity": "Info",
      "Source": "native",
      "Actor": {"Kind": "user", "Id": "example-admin", "Name": "Administrator"},
      "Resource": {"Kind": "settings", "Id": "1"},
      "Revision": "7",
      "Count": "1",
      "State": null,
      "ChangedFields": ["ServerName", "ServerNameMode"],
      "Name": "Server settings updated",
      "Overview": "Supported server settings were updated."
    }
  ],
  "TotalRecordCount": 1,
  "StartIndex": 0,
  "Limit": 50,
  "RetentionDays": 30
}
```

| Response field | Meaning |
| --- | --- |
| `Id` | Positive activity ID encoded as a decimal string |
| `Date` | UTC database timestamp |
| `Action` | Stored operation identifier |
| `Severity` | Stored severity using the query enum |
| `Source` | `native`, `emby`, or `system` |
| `Actor.Kind` | `user`, `application_key`, or `system` |
| `Actor.Id` | User ID or decimal application-key record ID; null for a system actor |
| `Actor.Name` | Current user display name when available and valid; null for a missing user, system actor, or application key |
| `Resource.Kind` | `user`, `session`, `application_key`, `device`, `library`, `scan`, `item`, `settings`, `task`, or `task_run` |
| `Resource.Id` | Persisted public resource identifier as a string |
| `Revision` | Positive resource revision as a decimal string, or null when not applicable |
| `Count` | Nonnegative action-specific count as a decimal string; it is not a universal count of all affected database rows |
| `State` | `completed`, `failed`, `cancelled`, or `interrupted` for terminal scan/task entries; otherwise null |
| `ChangedFields` | Sorted unique field names from the implemented edit schema; an empty array when none applies |
| `Name`, `Overview` | Fixed application descriptions of the action and terminal state |
| `TotalRecordCount` | JSON number containing the filtered count before pagination |
| `StartIndex`, `Limit` | Effective requested pagination |
| `RetentionDays` | Effective activity retention policy |

Native int64 identities, counts, and revisions remain strings to preserve
precision in JavaScript. A total greater than `9007199254740991` fails with
`503 activity_unavailable`. Actor names are current enrichment, not historical
name snapshots. Credential identifiers and optional internal correlation IDs
are not exposed in this DTO. Names, settings values, passwords, keys, file
paths, request bodies, and before/after values are not copied into the stored
activity description.

| Action | Resource | Committed fact |
| --- | --- | --- |
| `user.created` | `user` | Initial administrator setup or managed account creation |
| `user.updated` | `user` | A managed account update and new management revision |
| `user.password_reset` | `user` | Password replacement and its credential revocations |
| `session.login` | `session` | Successful native or Emby login session issuance |
| `session.revoked` | `session` | First explicit session revocation |
| `application_key.created` | `application_key` | Application-key creation |
| `application_key.revealed` | `application_key` | Successful decryption and preparation for authorized display, including compatibility list operations that reveal tokens |
| `application_key.revoked` | `application_key` | First application-key revocation |
| `device.updated` | `device` | A changed device display option |
| `device.removed` | `device` | Device removal and associated session revocation |
| `library.created` | `library` | Library registration |
| `library.removed` | `library` | Catalog library removal; media files are retained |
| `scan.requested` | `scan` | Durable scan admission |
| `scan.cancel_requested` | `scan` | First persisted scan cancellation request |
| `scan.finished` | `scan` | First terminal scan transition, including interrupted-work recovery |
| `metadata.updated` | `item` | Changed metadata overrides or locks and their revision |
| `settings.updated` | `settings` | Changed raw supported settings and their revision |
| `task.admitted` | `task_run` | A newly admitted manual or scheduled execution |
| `task.cancel_requested` | `task_run` | First persisted task cancellation request |
| `task.finished` | `task_run` | First terminal task transition, including recovery |
| `task.schedule_updated` | `task` | Schedule replacement and new schedule revision |

Records are inserted inside the business transaction. Failed or rolled-back
operations do not leave a committed success record. Replayed task admission,
repeated cancellation/revocation, and unchanged settings or metadata do not
create duplicate transition records. Successful account updates and schedule
replacements still advance their existing revisions and are recorded even when
submitted values match; an empty `ChangedFields` array does not imply failure.
An application-key reveal describes preparation inside the transaction, not
proof that a client received the secret.

Activity retention defaults to 30 days. The worker runs once per minute and
deletes at most 1000 expired rows per run using database time. Expired rows may
remain while a backlog is drained; this is not a hard row-count limit.

## Native diagnostic file list

`GET /admin/v1/logs` accepts only `StartIndex` and `Limit`, with the same default
and bounds as the native activity page. The list sorts by registered creation
time descending, then filename descending. A start beyond the list returns an
empty array and the full retained file count.

```json
{
  "Items": [
    {
      "Name": "goby-0123456789abcdef0123456789abcdef-abcdef0123456789abcdef0123456789.jsonl",
      "DateCreated": "2026-09-10T09:00:00Z",
      "DateModified": "2026-09-10T09:01:00Z",
      "Size": "1024"
    }
  ],
  "TotalRecordCount": 1,
  "StartIndex": 0,
  "Limit": 50,
  "Status": {
    "Healthy": true,
    "Degraded": false,
    "Closed": false,
    "MaxFileBytes": "4194304",
    "MaxFiles": 16,
    "RetentionDays": 7,
    "MinFreeBytes": "33554432",
    "Format": "jsonl"
  }
}
```

`Name` is an opaque basename returned by this list, not a path to construct.
`DateCreated` is the store's registered creation timestamp; `DateModified` is
the current file modification timestamp. Both are UTC. `Size`, `MaxFileBytes`,
and `MinFreeBytes` are decimal strings measured in bytes. `MaxFiles` includes
the active file. The default named-file payload budget is 16 files of at most
4 MiB each; filesystem metadata and open snapshots are additional storage.

`Status` reports configured limits and the store state at the time it was
sampled. A closed, degraded, or unavailable store normally fails the list with
`503 diagnostics_unavailable`; the endpoint does not promise a successful
status-only response during an outage. It imports neither arbitrary existing
files nor journald output. Listing can perform normal closed-file retention.

## Native diagnostic lines

`GET /admin/v1/logs/{name}/lines`

| Query field | Default | Accepted value |
| --- | --- | --- |
| `StartIndex` | `0` | Integer from `0` through `2147483647` |
| `Limit` | `200` | Integer from `1` through `500` |

```json
{
  "Items": [],
  "StartIndex": 0,
  "NextIndex": 0,
  "TotalRecordCount": 0,
  "SnapshotSize": "0"
}
```

The example shows an empty file. `SnapshotSize` is the byte length of the
captured file, not a value clients should infer from decoded strings. `Items`
contains complete JSONL records in file order, oldest first, with each
trailing newline removed. Each item is a string, not a nested JSON object.
`TotalRecordCount` counts every complete line in the same snapshot, independent
of pagination. `NextIndex` always equals `StartIndex + Items.length`; it stays
at the requested start for an empty page. `SnapshotSize` is a decimal byte-count
string.

Each request captures its own fixed byte boundary, so later appends do not
extend that response. Separate page requests can see a larger active file.
Closed files remain immutable until retention removes them. A file listed
earlier may subsequently return `404`, at which point the client should refresh
the list. There is no tail stream, cursor subscription, text search, or
cross-file line pagination.

Both line and download paths accept only a nonempty UTF-8 basename of at most
160 bytes, excluding `.`, `..`, separators, NUL, and control characters. A
syntactically valid unregistered name returns `404` when storage and reader
capacity are available. The store verifies the
registered file identity before opening it.

## Native diagnostic download

`GET /admin/v1/logs/{name}/download` accepts no query string, including an empty
trailing `?`. Use the native cookie; do not put credentials in a download URL.
At most one `Range` header is accepted, with at most 4096 bytes. The attachment
uses `application/x-ndjson` and a safely encoded `Content-Disposition` filename.

The handler preserves HTTP HEAD, modification-time conditional requests, and
byte ranges through `http.ServeContent`. Normal responses are `200`, valid
partial responses are `206`, and unsatisfiable ranges can produce `416`.
Range/conditional responses follow that HTTP implementation rather than the
JSON error envelope. A successful transfer's Content-Length describes the fixed
snapshot or selected range; later log writes do not enlarge it.

Authority is checked before opening and again afterward. While a download is
active, a watcher rechecks current authority once per second using short
database transactions. The transfer ends at the earlier of the credential's
expiry or a 60-second lifetime. Revocation, demotion, expiry, cancellation,
storage failure, or a failed network write can interrupt it. Once response
bytes have started, failure aborts the HTTP stream instead of appending a JSON
error or reporting normal EOF. Clients must discard an incomplete transfer.
The response writer must support write deadlines; otherwise the request fails
with `503` before serving the file.

The store permits at most eight simultaneous snapshots across lines and
downloads, each at most 64 MiB. Exhaustion returns `503`, not an unbounded queue.
Reader slots and file descriptors are released on completion, cancellation,
or store shutdown.

## Native errors

The usual native error shape is:

```json
{"Error":{"Code":"invalid_input","Message":"Supply valid activity or diagnostic query fields."},"RequestId":"server-generated-request-id"}
```

| Status and code | Condition |
| --- | --- |
| `401 authentication_required` | Missing native administrator cookie |
| `401 invalid_credentials` | Invalid, expired, revoked, or otherwise inactive credential |
| `403 administrator_required` | Current native principal lacks administrator access |
| `403 origin_denied` | Download origin check fails |
| `400 invalid_input` | Invalid native query, filename, or download request fields |
| `404 not_found` | Requested diagnostic basename is not registered |
| `503 activity_unavailable` | Activity storage or representable-count failure |
| `503 diagnostics_unavailable` | Diagnostic storage unavailable, closed, degraded, busy, or unable to enforce transfer deadlines |
| `500 internal_error` | Unexpected request failure |

Client cancellation may end without a further response. `416` and other
conditional-download results follow the download contract above. Diagnostic
errors do not disclose filesystem paths or raw underlying errors.

## Compatibility surface

The [compatibility adapter](../../internal/server/observability_emby.go) exposes
the same underlying activity and registered diagnostic files through four
Emby GET operations. The canonical paths below also work without the `/emby`
prefix through the existing namespace adapter. No additional case-variant
guarantee is made for these route literals; filenames remain case-sensitive.

| GET route | Allowed query fields besides `api_key` | Result |
| --- | --- | --- |
| `/emby/System/ActivityLog/Entries` | `StartIndex`, `Limit`, `MinDate` | Activity query result |
| `/emby/System/Logs/Query` | `StartIndex`, `Limit` | Registered file query result |
| `/emby/System/Logs/{Name}/Lines` | `StartIndex`, `Limit` | String-line query result |
| `/emby/System/Logs/{Name}` | `Sanitize` | Sanitized JSONL attachment |

GET requires a current ordinary Emby administrator token or a complete
application-key principal. The native administrator cookie is a separate
audience. Existing Emby token transports, including `X-Emby-Token` and the
single lowercase `api_key` query field, remain supported. Missing/invalid
credentials receive `401` with `Access token is invalid or expired.` A valid
viewer receives `403` with
`User {Name} does not have access to ManageServer feature.` These text errors
have no trailing newline. Authority is checked before operation inputs and
again transactionally around the read.

All four compatibility routes deliberately return `404` for HEAD before GET
authentication, with no body and headers for the fixed text
`The requested operation was not found.` This is not native HEAD support. The
reference measured administrator and anonymous HEAD on the log-download route
as `404`; its absent response body does not establish that fixed error text.

Every compatibility route uses `Cache-Control: no-store` and `Pragma: no-cache`.
Successful JSON is `application/json; charset=utf-8`, has a measured
Content-Length, and has no trailing newline. The only common response fields
are `Items` and numeric `TotalRecordCount`; native pagination echoes, policy,
actor details, revisions, and snapshot byte counts are not added.

### Compatibility pagination

Query names are exact and single-valued, with the same 4096-byte, UTF-8, and
control-character limits as native queries. Numeric values must be canonical
signed int32 decimal strings: `+1`, `01`, and `-0` are invalid. The table below
defines the implemented differences from the native API.

| Surface | Omitted `Limit` | Explicit `Limit` | `StartIndex` | `TotalRecordCount` |
| --- | --- | --- | --- | --- |
| Activity | Up to 200 items | `1..200`; zero or negative returns empty items | Default `0`; negative values become `0` | `0` when Limit is omitted or start is beyond/equal to the matching count; otherwise the matching count, including a zero/negative Limit |
| File list | Up to 200 files | `1..200`; zero or negative returns empty items | Default `0`; negative values become `0` | Full retained file count, including empty/beyond-end pages |
| Lines | Empty items | `0..500`; zero returns empty items; negatives fail | Default `0`; `0..2147483647`; negatives fail | Complete line count of the snapshot, including empty/beyond-end pages |

Limits above the stated maxima fail with `400`; omission never requests an
unbounded result. Activity sorting and log sorting follow Goby's underlying
stores. Activity supports only the inclusive `MinDate` filter described above;
native `Severity`, `Action`, and `ActorId` are not compatibility parameters.
Lines does not accept `StartPosition`, `SearchTerm`, or a cursor. The reference
requests using those extra fields also omitted Limit, so their empty responses
did not prove usable filtering or cursor semantics.

### Compatibility projections

Activity entries contain exactly these fields, with `UserId` conditional:

```json
{
  "Items": [
    {
      "Id": 42,
      "Name": "Session issued",
      "Overview": "A login completed and its session was issued.",
      "Type": "user.authenticated",
      "Date": "2026-09-10T09:00:00.1234560Z",
      "UserId": "example-user",
      "Severity": "Info"
    }
  ],
  "TotalRecordCount": 0
}
```

`Id` and `TotalRecordCount` use JSON integer numbers, preserving the declared
Emby int64 wire form rather than the native string form. Clients must handle
int64 precision appropriately. `Type` is the real stored action, except
`session.login` maps to `user.authenticated` and `user.password_reset` maps to
`user.passwordchanged`. Goby does not invent separate policy/password events
for an unrelated operation. `Name` and `Overview` retain Goby's fixed safe
templates. `Date` is UTC with exactly seven fractional digits.

For a user resource, `UserId` is that resource's actual public user ID;
otherwise it is the actor's user ID when the actor is a user. It is omitted
when no real user association applies. The reference exposes a different
internal numeric-string identity, which Goby does not fabricate. Optional
SDK fields `ShortOverview`, `ItemId`, and `UserPrimaryImageTag` are omitted.

Compatibility file entries contain `DateCreated`, `DateModified`, `Size`, and
`Name`. Dates use UTC with seven fractional digits, and `Size` is a JSON
integer byte count. Creation time is Goby's registered creation time; it does
not change merely because the active file was appended. The Lines projection
is exactly `{"Items":["complete line without newline"],"TotalRecordCount":1}`
for a one-line snapshot and nonzero limit. It has no native `NextIndex` or
`SnapshotSize` field.

### Compatibility download and bounded deviations

`Sanitize` may be omitted or supplied as exactly `true` or `false`. Every mode
serves already sanitized JSONL; `false` does not expose raw errors, request
values, credentials, or arbitrary files. Download uses
`text/plain; charset=UTF-8`, an attachment filename, fixed snapshot length,
and the same GET range/conditional handling, reader quota, current-authority
watcher, cancellation, and transfer lifetime as the native download. These
bounded transfer guarantees are product behavior; the reference did not test
Range requests. Compatibility GET does not apply the native-cookie download
origin check.

The reference's malformed-input and missing-file `500` responses are not
copied. After authentication, these adapter errors use fixed `text/plain`
messages without a trailing newline or reflected input:

| Status | Text |
| --- | --- |
| `400` | `Supply valid activity or diagnostic query fields.` |
| `404` for an unknown registered-file name | `The requested diagnostic file was not found.` |
| `503` for activity | `Activity history is currently unavailable.` |
| `503` for diagnostics | `Diagnostic logs are currently unavailable.` |
| `500` for an unexpected handler failure | `The activity or diagnostic request could not be completed.` |

Existing authentication-layer client-metadata errors retain their established
contract. HTTP range/conditional results and failures after a transfer begins
follow the download behavior above.

The [reference observations](../research/observability-reference.md) retain
the original behavior and unresolved questions. In particular, its activity
date experiment returned the same record at a bound 100 ns after the displayed
timestamp; this does not establish the server's rounding algorithm. Goby uses
its explicit inclusive PostgreSQL precision rule. Fixed descriptions, actual
user IDs, registered creation timestamps, bounded queries, safe error statuses,
and always-sanitized private downloads are declared implementation boundaries,
not claims of byte-for-byte identity with every Emby response.
