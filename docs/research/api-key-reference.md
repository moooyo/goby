# API Key Reference Contracts

Verified against the existing isolated official Emby Server `4.9.5.0` on
`test-env` at `2026-09-09T19:58:51Z`. This bounded study records reference
behavior; it does not establish that Goby implements these routes.

The capture added **48 sanitized records: 47 complete HTTP exchanges and one
audit observation**. The preserved corpus contained 958 records beforehand and
contains 1006 after this extension. Evidence uses the `keys-m5d-` prefix under
[the reference fixture directory](../../tests/compatibility/fixtures/reference/emby-4.9.5.0).

## Source and capture boundaries

The [pinned SessionsService inventory](../api/services/SessionsService.md)
declares `GET /Auth/Keys`, `POST /Auth/Keys?App=...`,
`DELETE /Auth/Keys/{Key}`, and `POST /Auth/Keys/{Key}/Delete`. It identifies
these operations as administrator operations, declares optional integer
`StartIndex` and `Limit` on listing, and leaves successful response schemas
unspecified. Those declarations are distinct from the observations below.
The live official documentation site could not be fetched through the available
web provider during this study; no additional web-only claim is marked verified.

The [reference deployment](reference-server.md) remained active as
`goby-emby-reference.service`, PID `3131777`, with `PrivateNetwork=yes`.
The recorder rechecked its root ownership, both ownership markers, and the
separate network namespace before entering that namespace. HTTP requests used
the previously established `127.0.0.1:18097` listener inside the namespace.
No host-facing listener or port forwarding was created.

The existing administrator and ordinary synthetic account authenticated normally
on two new dedicated recorder device IDs. Their original credential files were
read only after checking root ownership and mode `0600`; neither was overwritten.
All attempted key creation used the unique application prefix
`Goby Keys M5d 20260910 01 `. Four keys were issued successfully. No existing
user, policy, library, setting, or source was edited. Normal authentication and
API use advanced the participating accounts' and sessions' activity timestamps.

The dedicated [recorder](../../scripts/test-env/reference-api-keys.py) restricts
mutations to its fresh logins and application keys discovered as a difference
from the initial key list. Before exporting a response, it registers all returned
credentials and redacts token fields, token headers, `api_key` query values, and
the credential segment in `/Auth/Keys/{Key}` paths. Raw records and login results
remain in `/opt/goby-test/exec-scratch/keys-m5d/private` on `test-env`, with
root-only directories and mode `0600` files. Only sanitized exports entered the
repository. No token or password was placed in a command argument or stdout.

## Creation and returned representation

| Request | Verified result | Evidence suffix |
| --- | --- | --- |
| Administrator `POST /Auth/Keys?App={owned label}` | HTTP `204`, empty body; the key is obtained by listing afterward | `create-alpha`, `create-beta` |
| The same administrator creates the same `App` label again | HTTP `204`; two separate rows with the same `AppName` exist afterward | `create-alpha-duplicate`, `list-created` |
| Ordinary user creates an owned label | HTTP `403`, `text/plain` | `create-viewer` |
| A newly issued API key creates another owned label | HTTP `204`; the new row is visible afterward | `create-by-key`, `list-after-key-use` |

Creation did not return JSON or the new credential in its response body. The
successful `204` captures did not include `Content-Type` or `Content-Length`.
The declared SDK success status `200` is therefore different from the observed
status. Missing, empty, whitespace-only, oversized, and non-ASCII `App` values
were not attempted: every potentially successful creation had to retain this
study's unique ownership label.

`GET /Auth/Keys` returned HTTP `200` with
`Content-Type: application/json; charset=utf-8` and an object containing
`Items` and `TotalRecordCount`. It exposed each active key's full credential;
the following example replaces only the credential with a redaction marker:

```json
{
  "Id": 18,
  "AccessToken": "[REDACTED_SECRET]",
  "ReportedDeviceId": "ec69ef1cf84140e88489c30326529308",
  "DeviceId": 15,
  "AppName": "Goby Keys M5d 20260910 01 Alpha",
  "AppVersion": "4.9.5.0",
  "IpAddress": "127.0.0.1",
  "DeviceName": "Goby Emby Reference",
  "UserId": 0,
  "DateCreated": "2026-09-09T19:58:51.0000000Z",
  "IsActive": true
}
```

`Id`, `DeviceId`, and `UserId` were JSON numbers. `Id` identified the key row;
it was different from the string `AccessToken` used for authentication and
deletion. Newly created rows omitted `DateLastActivity`. After the first key
was used, its row included a string `DateLastActivity` with a UTC timestamp.
No `UserName` appeared in these key rows. All four rows had `UserId: 0` and
used the server ID, server name, and server version as their reported device
metadata. These are observations for this server and request metadata, not
portable constants. Evidence: `list-created`, `list-after-key-use`.

## Listing and pagination observations

All rows in the initial three-key pagination sample had the same
`DateCreated` timestamp to the server's displayed precision. Numeric row IDs
identify the sample without exposing credentials.

| Query | `Items` row IDs | `TotalRecordCount` | HTTP |
| --- | --- | --- | --- |
| No query parameters | `[18, 17, 16]` | `0` | `200` |
| `StartIndex=0&Limit=1` | `[16]` | `3` | `200` |
| `StartIndex=1&Limit=1` | `[17]` | `3` | `200` |
| `StartIndex=13&Limit=2` | `[]` | `0` | `200` |
| `Limit=0` | `[]` | `3` | `200` |
| `StartIndex=-1&Limit=1` | `[16]` | `3` | `200` |
| `Limit=-1` | `[]` | `3` | `200` |
| `Limit=nope` | Text error | Not present | `500` |

Evidence: `list-created`, `page-zero`, `page-one`, `page-empty`, `limit-zero`,
`start-negative`, `limit-negative`, and `limit-malformed`. The malformed limit
body was exactly `The input string 'nope' was not in a correct format.`

These captures establish that offset/limit affect the result, but do not
establish a reliable universal ordering or total-count rule. In particular, the
unpaged and paged requests returned different first rows without a mutation
between them, and an unpaged result containing rows still reported a zero total.
The later four-row unpaged result also reported zero. Do not present the
observed count or same-timestamp order as a corrected or stable contract.
Large limits, integer overflow, malformed `StartIndex`, and ordering across
distinct creation/activity timestamps remain unverified.

## Identity and permission observations

The API key was sent by itself, without `Authorization` client metadata or
cookies, through each of these carriers in separate requests:

- `X-Emby-Token: {key}`.
- `?api_key={key}` appended to the request query.

Both carriers returned HTTP `200` for all of the following routes:

| Route | What the response establishes |
| --- | --- |
| `GET /Auth/Keys` | The key can read the administrator key list, including credentials. |
| `GET /Users` | The key can read the user collection. |
| `GET /Users/{administratorId}` | The key can read the existing administrator's user DTO. |
| `GET /Users/{ordinaryUserId}` | The key can read a different, ordinary user's DTO. |
| `GET /Items?Limit=1` | A user ID is not required for this catalog request; the returned item omitted `UserData`. |
| `GET /Users/{ordinaryUserId}/Items?Limit=1` | An explicit ordinary-user context is accepted; the returned item included `UserData`. |
| `GET /Sessions` | The key can read the session collection. |

Evidence: the fourteen `key-{header,query}-{keys,users,admin-user,viewer-user,items,viewer-items,sessions}`
captures. The two catalog requests returned different first item types, so
their different `UserData` shapes are not a comparison of the same item.
No media bytes, user-state mutation, playback start, or remote command was
requested with a key.

The key's own session appeared in both session captures. Its `Client` was
the application label, while `DeviceId` and `DeviceName` described the server.
It omitted both `UserId` and `UserName`; the two ordinary login sessions
included those fields. Combined with `UserId: 0` in the key DTO, this is direct
evidence of a credential without a user binding in this reference behavior.
The successful key-creation request additionally establishes administrative
key-management authority. It does not prove that every administrative
operation or every possible user-policy combination accepts a key.

Anonymous `GET /Auth/Keys` returned HTTP `401`, `text/plain`, with
`Access token is invalid or expired.`. The ordinary account's list, create,
and DELETE attempts returned HTTP `403`, `text/plain`, with
`User reference-session-m3b does not have access to ManageServer feature.`
Evidence: `list-anonymous`, `list-viewer`, `create-viewer`,
`delete-viewer-denied`. The denied DELETE targeted only a newly owned key;
the subsequent administrator DELETE revoked it normally.

## Revocation and logout

| Operation using an owned key as the target | Verified result | Evidence suffix |
| --- | --- | --- |
| Administrator `DELETE /Auth/Keys/{key}` | `204`, empty body | `delete-owned` |
| Protected request with that deleted key | `401`, standard invalid-token text | `deleted-key-protected` |
| Repeat the DELETE for that same already deleted owned key | `204`, empty body | `delete-owned-repeat` |
| Administrator `POST /Auth/Keys/{key}/Delete` | `204`, empty body | `delete-alias-owned` |
| Protected request with the alias-deleted key in `api_key` | `401`, standard invalid-token text | `alias-deleted-key-protected` |
| `POST /Sessions/Logout` authenticated by a newly issued key in `X-Emby-Token` | `204`, empty body | `logout-key` |
| Protected key-list request using the logged-out key | `401`, standard invalid-token text | `key-after-logout` |

The key used for `Sessions/Logout` disappeared from the administrator's list;
the other three keys remained. Thus this logout revoked the API credential,
not merely its transient session view. Evidence: `list-after-key-use`,
`list-after-key-logout`. Logout with the query carrier, repeated logout, and
cross-key session identity interactions were not tested.

## Final audit and implementation implications

The final key list exactly matched the initial list; both were empty. All four
issued keys were removed through owned logout or the documented key deletion
routes. Both dedicated normal login tokens were logged out. The recorder
rechecked the same reference PID, compared all **1916 preceding raw/export
files** and **240 known source paths** with their pre-capture SHA-256 values,
and audited each new raw/export redaction pair and file permissions. All checks
passed. Known source paths came from the preceding metadata baseline plus its
three retained source-directory entries, including its ownership marker;
unrelated source trees were not enumerated. Total HTTP response bytes were
`67347`. Evidence: `keys-m5d-audit.json`.

### Recorder hardening after capture

A subsequent source review found that the original recorder traversed JSON
lists but not the tuple pairs returned by `HTTPResponse.getheaders()`. It also
reported the number of owned logins without checking the two logout statuses.
Neither gap changed the recorded observations: the original HTTP responses
contained no `Set-Cookie`, authorization, or credential-token response headers,
and `keys-m5d-viewer-logout.json` and `keys-m5d-admin-logout.json` each record
HTTP `204` with an empty body. Existing captures were not regenerated or edited.

The recorder was hardened after the capture. Header dictionaries, live tuple
pairs, and serialized list pairs now receive structural validation and
name-based credential collection/redaction, including cookie, authorization,
API-key, and previously unknown token header names. Duplicate response headers
retain their order. Unsupported header representations fail before export.
Owned login cleanup now counts only confirmed `200` or `204` responses, records
each observed status, continues to the other login after an error, and prevents
a successful audit if any logout is unconfirmed.

The [synthetic regression suite](../../scripts/test-env/test-reference-api-keys.py)
passed **13 tests with zero failures and zero errors**, only through root SSH
on `test-env`. It imports the recorder without initialization and uses fake
HTTP plus in-memory outputs; network access and process probes are blocked.
No reference login, API request, capture rewrite, or source-media access occurred
during these tests. The hardened recorder and test source were staged as two
new mode-`0600` files with exclusive creation directly under the existing owned
execution scratch directory, outside the retained capture directory.

There were no preexisting API keys in this environment. The audit therefore
proves that no new key remained and that the empty old key set was preserved;
it does not exercise preservation alongside a nonempty historical key set.

For Goby design, the observed API key is a separate, userless administrative
credential. Automatically binding it to the administrator who issued it would
be an intentional policy choice with different observable identity semantics.
Likewise, storing only a one-way token hash and showing a secret once cannot
reproduce a reference list that returns every full `AccessToken`; a native
dashboard can choose a different contract, but that choice should be explicit.
The observed total-count and ordering anomalies should be considered separately
from a coherent native pagination contract. No compatibility implementation or
native administration policy was changed by this study.
