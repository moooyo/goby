# M5i log and activity reference observations

Status: the first controlled capture completed on 2026-09-10 against an
official Emby Server **4.9.5.0** disposable instance. All eight capture checks
passed, followed by independent operator teardown. This is reference research,
not Goby product acceptance or a claim of complete Emby compatibility.

The [execution summary](../development/m5i-observability-reference.json),
[capture audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-audit.json),
and [executed plan](observability-execution-plan.md) bind the source versions,
private-byte correspondence, scope and cleanup. The endpoint declarations
come from the [pinned SDK baseline](../sources/README.md); actual responses
below take precedence over assumptions based on its incomplete schemas.

## Evidence and fixture

| Record group | Records | Meaning |
| --- | ---: | --- |
| Fresh setup | 15 | Fourteen complete HTTP exchanges and one retained connection-refused readiness attempt. |
| Controlled capture | 76 | Complete HTTP exchanges: 69 GET, four POST, two HEAD and one DELETE. |
| Capture audit | 1 | Byte, export, credential and preservation evidence; not an HTTP exchange. |
| Independent operator cleanup | 4 | Complete HTTP exchanges using the two already retired ordinary tokens. |
| New corpus records | **96** | Added to the preceding 2366, producing **2462** records. |

The [first readiness attempt](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-setup-m5i-001-readiness-public.json)
has no HTTP status or headers and records `ConnectionRefusedError`. The next
bounded readiness attempt succeeded on the same fixture. It must not be
counted as a complete HTTP exchange or a second fixture. The extension
therefore contains 94 complete HTTP exchanges, one non-HTTP readiness failure
and one audit.

The new reference used its own private network, HTTP port `18101`, no library
or media source, and two newly issued ordinary credentials. The recorder did
not log in again. It created one additional named user without authenticating
that user, and one application key used for the four studied GET routes and
one protected-route invalidity probe during cleanup.
The selected log name, `embyserver.txt`, came from that fresh instance's Query
response. The only unknown name was the fixed nonexistent basename
`goby-observability-m5i-01-not-present.log`.

All 76 capture bodies were complete and UTF-8 exportable. Recorded response
bytes total 37,054 in the JSON budget and 295,705 in the log budget. These are
private response-body measurements, not the lengths of redacted text. The
23 passing guard tests are synthetic, with zero HTTP requests, subprocesses
and capture writes; they are separate from the live reference evidence.

## Authentication and common response forms

All requests use the `/emby` base. The four GET routes have the same observed
permission matrix:

| Principal | Activity Entries | Logs Query | Log download | Log Lines |
| --- | ---: | ---: | ---: | ---: |
| Anonymous | 401 | 401 | 401 | 401 |
| Invalid token | 401 | 401 | 401 | 401 |
| Ordinary viewer | 403 | 403 | 403 | 403 |
| Ordinary administrator | 200 | 200 | 200 | 200 |
| Owned application key | 200 | 200 | 200 | 200 |

The twenty files use `observability-fresh-m5i-auth-{principal}-{endpoint}.json`,
where endpoints are `activity`, `log-query`, `log-download` and `log-lines`.
Representative complete records are [anonymous activity](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-anonymous-activity.json),
[invalid-token download](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-invalid-log-download.json),
[viewer Lines](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-viewer-log-lines.json),
[administrator Query](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-admin-log-query.json)
and [application-key activity](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-application-activity.json).

The 401 body is `Access token is invalid or expired.` with `text/plain` and
`Content-Length: 35`. Viewer denial is `text/plain`, length 94, with
`User reference-observability-fresh-viewer-m5i-01 does not have access to ManageServer feature.`
No sampled response includes `WWW-Authenticate`. Successful query/Lines
responses use `application/json; charset=utf-8`.

Tokens were carried by `X-Emby-Token`; ordinary requests also supplied their
acknowledged client/device metadata. This study does not add evidence for
query-token carriers, alternative authentication headers or key mutation
permissions.

## Activity entries

The [administrator default response](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-admin-activity.json)
contains exactly `Items` and `TotalRecordCount`. Each of its seven entries has
these properties:

| Property | Observed JSON type and value |
| --- | --- |
| `Id` | Integer number, sampled values 7 through 1. |
| `Name` | String describing the activity, account and server. |
| `Overview` | String containing display prose and newlines. |
| `Type` | String: `user.created`, `user.authenticated`, `user.policyupdated` or `user.passwordchanged`. |
| `Date` | UTC string with seven fractional digits, such as `2026-09-10T09:41:52.8040000Z`. |
| `UserId` | String, sampled values `"3"`, `"2"` and `"1"`. |
| `Severity` | String `Info` in every sampled entry. |

`ShortOverview`, `ItemId` and `UserPrimaryImageTag` are omitted, not explicit
nulls. The wider SDK severity enum is a declaration; this capture does not
produce Debug, Warn, Error or Fatal activity. Overview display times are
localized prose and must not replace the `Date` timestamp.

The new user's [creation DTO](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-create-owned-cause-user.json)
has public `Id: "4590f879e7824eeb8246a6d9f4a7a963"`, while its activity has
`UserId: "3"`. These values are not interchangeable. The new activity is
attributable through its unique reserved username, not an assumed equality
between the two identifier forms.

The [baseline](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-before-user.json)
and [first post-creation read](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-after-user-0.json)
contain IDs 6 through 1. The [second read](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-after-user-1.json)
adds ID 7, `Type: "user.created"`, for the acknowledged new user. It proves
this activity cause and a delayed visible read in this sequence, not a general
delivery-latency guarantee. No subsequent login was made for that user.

### Paging and count anomalies

The seven-entry sequence is ordered 7 through 1, with descending sampled
dates. No equal-date tie or stable cursor was tested. `TotalRecordCount` is
not consistently the number of matching entries:

| Query | Status | Returned IDs | `TotalRecordCount` | Evidence |
| --- | ---: | --- | ---: | --- |
| Omitted | 200 | 7, 6, 5, 4, 3, 2, 1 | **0** | [default](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-admin-activity.json) |
| `Limit=1` | 200 | 7 | 7 | [one](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-limit-one.json) |
| `Limit=0` | 200 | Empty | 7 | [zero](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-limit-zero.json) |
| `Limit=-1` | 200 | Empty | 7 | [negative limit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-negative-limit.json) |
| `StartIndex=1&Limit=1` | 200 | 6 | 7 | [offset one](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-offset-one.json) |
| `StartIndex=-1&Limit=1` | 200 | 7 | 7 | [negative offset](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-negative-offset.json) |
| `StartIndex=2147483647&Limit=1` | 200 | Empty | **0** | [beyond end](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-offset-beyond.json) |
| `Limit=invalid` | 500 | Plain-text integer parse error | Not a query result | [invalid](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-invalid-limit.json) |

These cases must not be collapsed into a conventional always-filtered,
pre-pagination count rule. They do not identify the server's internal count
query or its behavior for every possible offset/limit combination.

### MinDate

The boundary experiment uses the exact received ID-7 date. Every row below
returns status 200, ID 7 alone and `TotalRecordCount: 0`:

| Supplied `MinDate` | Evidence |
| --- | --- |
| `2026-09-10T09:41:52.8040000Z` | [exact](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-min-date-exact.json) |
| `2026-09-10T09:41:52.8039999Z` | [one tick before](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-min-date-before-tick.json) |
| `2026-09-10T09:41:52.8040001Z` | [one tick after](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-min-date-after-tick.json) |
| `2026-09-10T17:41:52.8040000+08:00` | [equivalent offset](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-min-date-equivalent-offset.json) |

The six older entries are absent. Inclusion even one 100 ns tick after the
displayed date prevents inferring an exact `Date >= MinDate` comparison at
that precision. Rounding, storage precision and timestamp projection remain
unresolved. The offset case demonstrates that one supplied equivalent offset
works; it is not exhaustive timezone parsing evidence.

[Exact MinDate with `StartIndex=1&Limit=1`](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-min-date-page.json)
returns empty Items and total 0. An [invalid date](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-activity-min-date-invalid.json)
returns 500 with a plain-text DateTime parse error. No severity, user, type or
free-text Activity filter was sampled.

## Log listing

The [default listing](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-before.json)
has `Items` and integer `TotalRecordCount: 2`. Each item has exactly
`DateCreated`, `DateModified`, `Size` and `Name`: dates and name are strings,
and size is an integer number. The observed order is `embyserver.txt`, then
`hardware_detection-63924658902.txt`.

| Query | Observed Items | Total | Evidence |
| --- | --- | ---: | --- |
| `Limit=1` | First file | 2 | [one](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-limit-one.json) |
| `Limit=0` | Empty | 2 | [zero](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-limit-zero.json) |
| `Limit=-1` | Empty | 2 | [negative limit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-negative-limit.json) |
| `StartIndex=1&Limit=1` | Second file | 2 | [offset](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-offset-one.json) |
| `StartIndex=-1` | Both files in the same order | 2 | [negative offset](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-negative-offset.json) |
| `StartIndex=2147483647&Limit=1` | Empty | 2 | [beyond end](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-offset-beyond.json) |

All table cases return 200. [`Limit=invalid`](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-query-invalid-limit.json)
returns 500, `text/plain`, with `The input string 'invalid' was not in a correct format.`

The active file's listed Size grows from 63,143 to 87,278 between the
[initial](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-before.json)
and [final](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-logs-final-before-cleanup.json)
listing. Both date fields also change and are equal within each response.
This does not establish a stable file birth time, an ordering algorithm,
rotation policy or exact size relationship to a later download.

## Log Lines: observed undeclared paging

The fixed SDK and generated client declare only `Name`. The live responses
demonstrate additional behavior for the tested `Limit` and `StartIndex`
values; these parameters are observed extensions, not SDK declarations.

| Query on `embyserver.txt/Lines` | Items | Total | Evidence |
| --- | --- | ---: | --- |
| Omitted, administrator | Empty | 530 | [administrator default](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-admin-log-lines.json) |
| Omitted, application key | Empty | 530 | [application default](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-application-log-lines.json) |
| `Limit=1` | First downloaded line | 633 | [first line](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-lines-limit-one.json) |
| `StartIndex=1&Limit=1` | Second downloaded line | 633 | [second line](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-lines-offset-one.json) |
| `Limit=0` | Empty | 633 | [zero](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-lines-limit-zero.json) |
| `StartIndex=2147483647&Limit=1` | Empty | 633 | [beyond end](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-lines-offset-beyond.json) |
| `StartPosition=1`, without Limit | Empty | 633 | [position](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-lines-start-position-one.json) |
| Unmatched `SearchTerm`, without Limit | Empty | 633 | [search](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-lines-search-unmatched.json) |

Every case returns 200 with only `Items` and integer `TotalRecordCount`.
Returned Items are strings without their terminating newline. The first
sample is the application-path line; the second is a NetworkManager line.
Both match the corresponding prefix of the retained download.

The default empty Items array must not be interpreted as an empty log. The
unmatched SearchTerm and StartPosition requests also omit Limit, so their
empty result does not establish whether either parameter is recognized,
ignored or applied before an empty default page. No cursor, tail, negative
Lines limit, malformed Lines integer or blank-line boundary was tested.
The change from 530 to 633 occurs while the file grows; it is not evidence of
a counting transformation caused by those query parameters.

## Download, Sanitize, HEAD and unknown names

All successful downloads return `text/plain; charset=UTF-8`:

| Request | Status | Private body bytes | Observed transport metadata |
| --- | ---: | ---: | --- |
| [Sanitize omitted, admin](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-admin-log-download.json) | 200 | 69,153 | Chunked; no Content-Length, Accept-Ranges, ETag or Cache-Control. |
| [Sanitize omitted, key](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-auth-application-log-download.json) | 200 | 69,153 | Same sampled transport metadata. |
| [Sanitize=false](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-download-sanitize-false.json) | 200 | 79,225 | Content-Length 79225; Accept-Ranges `bytes`; Cache-Control `public, no-transform`; ETag present. |
| [Sanitize=true](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-download-sanitize-true.json) | 200 | 78,174 | Chunked; no Content-Length, Accept-Ranges, ETag or Cache-Control. |

None of these responses includes Content-Disposition, Content-Range or
Last-Modified. No Range request was sent, so advertised Accept-Ranges is not
proof of 206 behavior. Omission and true share the sampled framing; neither
framing nor changing body length proves the default value or the complete
sanitization algorithm. All text in the exported fixtures additionally passed
the recorder's own sanitizer. Its placeholders must not be attributed to
Emby's server-side Sanitize implementation.

[`Sanitize=invalid`](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-download-sanitize-invalid.json)
returns 500, `text/plain`, length 55, with
`String 'invalid' was not recognized as a valid Boolean.`

Both [administrator HEAD](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-head-admin.json)
and [anonymous HEAD](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-head-anonymous.json)
for the known filename return **404**, `text/plain`, Content-Length 63 and
zero actual body bytes. This differs from GET authentication and must not be
reported as a successful HEAD download. The empty HEAD body does not reveal
the corresponding error text or internal route-dispatch reason.

The reserved missing filename returns **500** for both
[download](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-unknown-download.json)
and [Lines](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/observability-fresh-m5i-log-unknown-lines.json):
`text/plain`, length 37, body `Sequence contains no matching element`.
Filename case variations, encoded traversal, filesystem paths and legacy log
route aliases were not sampled.

## Cleanup and remaining limits

The application key was revoked with 204 and independently rejected with
401; its subsequent administrator listing was empty. Viewer and administrator
logout each returned 204, followed by independent Sessions 401 responses.
Independent operator cleanup then received 401 for both repeated logout
requests and both invalidity probes, consistent with the already retired
credentials. The final owned history comprised three accounts, two ordinary
login credentials and one revoked application key before DATA removal.

The execution summary records `dataRemoved`, `ownedProcessGone`,
`ownedCgroupEmpty` and preservation as true. The original Emby PID 3131777 and
Goby PID 3641418 were protected; earlier 2366 records, 240 media sources and
private historical evidence remain unchanged. Raw log bytes, credential
acknowledgments and wire evidence remain private on the remote host. Only
the 96 audited sanitized JSON records were added to this repository.

No retention expiry, scheduled rotation, restart persistence, concurrent
writer boundary, non-Info activity, equal-date ordering, broad filter syntax,
Range/conditional download, alternate MIME negotiation or client UI workflow
was executed. Native retention, redaction and audit durability require their
own product validation; this study does not provide it.
