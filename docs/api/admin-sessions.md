# Administrator login sessions

Status: implemented and accepted in [M5c](../development/verification-m5c-sessions.md),
including complete Linux race tests, browser/restart checks and deployed API
verification. The broader administrator milestone remains incomplete.

These native administrator endpoints manage existing authentication records.
They do not implement Emby's device registry, device options, application keys,
or a device-wide sign-in ban. The dashboard contains no consumer player.

## Authentication and authorization

Use the administrator cookie and the existing same-origin/CSRF contract. An
Emby API token does not authorize these native routes, including a token owned
by an administrator account. Mutation requests require `X-CSRF-Token`.

Both operations recheck the actor's current account, administrator role,
administrator-kind authentication, revocation and expiration in the database.
The list and revocation also recheck authorization immediately before commit.
Expired or revoked actors receive 401; the initial middleware rejects an
authenticated non-administrator with its existing 403 behavior.

## List login sessions

`GET /admin/v1/sessions`

Parameter names and enum values are case-sensitive. Unknown or repeated
parameters and malformed UTF-8/query encoding are rejected.

| Parameter | Meaning | Default / bounds |
| --- | --- | --- |
| `UserId` | Exact account ID | Empty includes all accounts; at most 256 UTF-8 bytes without surrounding whitespace or controls. |
| `Kind` | `admin` or `emby` | Empty includes both kinds. |
| `Status` | `active`, `revoked`, `disabled`, `expired`, or `all` | Empty means `active`. |
| `DeviceId` | Exact client-declared device ID | Empty includes every device ID; at most 256 UTF-8 bytes without NUL. |
| `SearchTerm` | Case-insensitive literal substring in user name, client name, device ID/name or application version | Empty disables search; at most 256 UTF-8 bytes without NUL. Whitespace is significant, and SQL wildcard characters are ordinary text. |
| `StartIndex` | Zero-based record offset | Default 0; canonical decimal integer from 0 to 2147483647. |
| `Limit` | Page size | Default 50; canonical decimal integer from 1 to 200. |

The response has exactly `Items`, `TotalRecordCount`, `StartIndex`, and `Limit`.
`Items` is always an array, including for an empty page. The total and the page
come from one SQL statement snapshot, with one captured time for status
classification. Records sort by creation time descending, then ID descending.
Later requests observe a new snapshot; offset pagination does not freeze the
history while concurrent users sign in.

Each item contains these fields:

| Field | Type / meaning |
| --- | --- |
| `Id` | Immutable authentication-session ID. This is not the token. |
| `UserId`, `UserName` | Current owning account identity and display name. |
| `UserIsAdministrator`, `UserIsDisabled` | Current account flags. |
| `Kind` | `admin` or `emby`. |
| `Client` | Client-declared application name. |
| `DeviceId`, `DeviceName` | Client-declared device identity and name. |
| `ApplicationVersion` | Client-declared application version. |
| `CreatedAt`, `LastSeenAt`, `ExpiresAt` | UTC RFC 3339 timestamps. |
| `RevokedAt` | UTC RFC 3339 timestamp or null. |
| `Status` | Effective classification described below. |
| `IsCurrent` | Whether this is the authenticated administrator's current login. |

Token material, token hashes and raw client capability objects are never
included. IP addresses and User-Agent strings are not invented from device
labels; the current authentication table does not retain those observations.

Status precedence is:

1. `revoked` when the authentication row has a revocation timestamp.
2. `disabled` when its account is disabled, or when an `admin` login's account
   no longer has the administrator role.
3. `expired` when its expiration is at or before the statement's captured time.
4. `active` otherwise.

Active means the login remains authorized. It does not mean the device is
online or playing. `LastSeenAt` is the last recorded token activity; ordinary
administrator requests currently do not refresh it. A client-declared device
ID is not a hardware identity, and the same ID across users is not a shared
revocation scope.

## Revoke one login

`POST /admin/v1/sessions/{id}/revoke`

Use `Content-Type: application/json` and body `{}`. No query parameters or body
fields are accepted. The body must be valid UTF-8 and at most 4 KiB. Missing,
null, array, trailing, or unknown-field bodies are invalid. The ID must contain
1 to 256 UTF-8 bytes without surrounding whitespace or control characters.

A successful response is:

```json
{
  "SessionId": "session-id",
  "UserId": "user-id",
  "Kind": "emby",
  "RevokedAt": "2026-09-09T00:00:00Z",
  "CurrentSessionRevoked": false
}
```

Revoking an already revoked record is idempotent: it returns 200 with the
original revocation time. Unknown records return 404 after current actor
authorization. No authentication history is deleted and no account management
revision changes. Other logins owned by the same account or declared device
remain valid. A later sign-in can create a new login, subject to normal account
and password policy.

Self-revocation commits successfully, returns `CurrentSessionRevoked: true`, and
expires the administrator cookie. The frontend clears its authenticated state
and returns to login. The final database check permits only this transaction's
exact self-revocation timestamp; actor expiration still rolls back the mutation.

The existing administrator mutation lock serializes account changes and mutual
administrator revocations. All account rows are locked before authentication
rows, with deterministic ID order inside each group. Runtime cleanup happens
only after durable revocation commits: existing WebSocket subscriptions for the
selected session are disconnected and its conversion sessions are retired.
Original-file responses retain their existing periodic authorization watcher
and blocked-write interruption. Revocation does not fabricate playback reports
or edit watched, favorite, resume, or metadata state.

## Errors and dashboard behavior

The existing native error envelope contains `Error` and `RequestId`. Validation
errors use 400 / `invalid_input` with safe field messages. Unsupported request
media types use 415 / `unsupported_media_type`. Missing sessions use
404 / `not_found`; stale actor credentials use the existing native 401 error.

The Sessions page supports filtering, literal search, pagination, refresh and
confirmation before a single-session revocation. It identifies the current
login and the account/application/device involved. It distinguishes login
validity from online presence. Device grouping, bulk revocation, IP history,
application-key management and persistent device settings remain separate work.
