# Application Keys API

This is the M5d implementation contract. [Product verification](../development/verification-m5d-application-keys.md)
passes focused and complete Linux race tests, isolated browser/restart
acceptance, and the deployed workflow. The reference studies below describe
separate, bounded Emby observations.

Application keys are independent, non-expiring, server-wide privileged bearer
credentials. They are not owned by the creating user; `CreatedBy` is audit
metadata. Possession permits the implemented compatibility key-management
operations, including revealing other active keys. Protect them as server
credentials. Native administrator endpoints require a native administrator
session and do not accept an application key as a replacement.

## Native administration

Use the `goby_session` administrator cookie. All POST requests require the
existing same-origin checks and `X-CSRF-Token`. They accept `application/json`,
one UTF-8 object of at most 4 KiB, exactly the documented fields, and no query
parameters. Unknown, repeated, missing, or incorrectly typed fields are invalid.
Key handlers send `Cache-Control: no-store` and `Pragma: no-cache`.

| Method and path | Request | Successful response |
| --- | --- | --- |
| `GET /admin/v1/api-keys` | Optional filters below | `200`, `{Items, TotalRecordCount, StartIndex, Limit}` |
| `POST /admin/v1/api-keys` | `{"AppName":"Example integration"}` | `201`, `{Key, AccessToken}` |
| `POST /admin/v1/api-keys/{id}/reveal` | `{}` | `200`, `{Id, AccessToken}` |
| `POST /admin/v1/api-keys/{id}/revoke` | `{}` | `200`, `{Id, RevokedAt}` |

Native key IDs are canonical positive decimal strings, including in path
parameters. Every list row and the creation response's `Key` contain exactly
these eight safe fields; neither includes a token hash or ciphertext:

| Field | JSON representation |
| --- | --- |
| `Id` | Positive decimal string identifying the key record |
| `AppName` | Application label |
| `CreatedAt` | UTC timestamp string |
| `LastUsedAt` | UTC timestamp string or `null` before use |
| `RevokedAt` | UTC timestamp string or `null` while active |
| `CreatedBy` | Real creator user ID string or `null` |
| `IPAddress` | Address recorded at creation |
| `Status` | `"active"` or `"revoked"` |

`AppName` is trimmed, then must contain 1–256 UTF-8 bytes and no control
characters. Duplicate labels create independent credentials. Only native
creation and explicit reveal return the full `AccessToken`.

Listing accepts case-sensitive, single-valued `StartIndex`, `Limit`,
`SearchTerm`, and `IncludeRevoked`; other parameters are invalid. `StartIndex`
defaults to `0` and accepts canonical integers from `0` through `2147483647`.
`Limit` is `1`–`200`, default `50`. `SearchTerm` is an optional case-insensitive
literal substring of the application label, limited to 256 UTF-8 bytes without
controls. `IncludeRevoked` is exactly `true` or `false`, default `false`.
Results sort by creation time descending, then numeric ID descending.
`TotalRecordCount` is the actual filtered count from the same query snapshot,
including when the requested page is empty.

Invalid input returns `400`; wrong content type returns `415`; unknown native
IDs return `404`. Revealing a revoked key returns `409` (`key_revoked`). Repeat
native revocation returns the original revocation timestamp. An unavailable
secret vault returns `503` (`key_vault_unavailable`) for creation or active-key
reveal. Native errors use `{Error: {Code, Message}, RequestId}`.

## Emby compatibility

The canonical paths below use `/emby`; the same routes also work without that
prefix. Compatibility route literals accept case-insensitive aliases, including
`Auth`, `Keys`, and `Delete`; the token path segment remains case-sensitive.
Authentication accepts the existing Emby token carriers, including
`X-Emby-Token` and `api_key`. A normal Emby
administrator login or an application key can manage keys; ordinary users
receive `403`, and absent, conflicting, or revoked credentials receive `401`.

| Method and canonical path | Successful behavior |
| --- | --- |
| `GET /emby/Auth/Keys` | `200`, `{Items, TotalRecordCount}` with every returned active key's full `AccessToken` |
| `POST /emby/Auth/Keys?App={label}` | `204`, empty body; duplicate labels remain independent |
| `DELETE /emby/Auth/Keys/{Key}` | `204`, empty body; `{Key}` is the token, not the numeric row ID |
| `POST /emby/Auth/Keys/{Key}/Delete` | Same idempotent token revocation |
| `POST /emby/Sessions/Logout` with an application key | `204`, empty body; revokes the parent credential and all its contexts |
| `GET /emby/Users` | `200`, bare user DTO array for an administrator or application key |

Unknown or already revoked tokens are successful deletion targets. Normal
login logout retains its existing `200` response. Compatibility key handlers
also disable caching. Listing requires decryption and can return `503` when
the master file is unavailable, even while native metadata listing works.

Compatibility rows have numeric `Id`, `DeviceId`, and `UserId` (`0`), plus
`AccessToken`, `ReportedDeviceId`, `AppName`, `AppVersion`, `IpAddress`,
`DeviceName`, `DateCreated`, and `IsActive: true`. `DateLastActivity` is omitted
until use. Dates are UTC strings. The key's label and reported server device
identity stay stable; its displayed version and device name reflect the most
recently used client context. These values are not reference-server constants.

Compatibility listing supports case-insensitive `StartIndex` and `Limit`,
defaulting to `0` and `200`; limits remain `1`–`200`. It returns the actual
active count and deterministic ordering described above. Goby intentionally
does not reproduce the reference's zero totals for populated unpaged results,
inconsistent ordering, negative/zero limits, or malformed-limit `500` errors.
Invalid filters return `400` with `{ResponseStatus: {ErrorCode, Message}}`;
authentication and key-management permission errors retain plain-text replies.

## Client context, catalog, and playback

One key owns persisted client contexts keyed by parent credential, client name,
and device ID. Creation adds a default context using its label and server
metadata. Missing header metadata uses defaults; changing `Device` or
`Version` updates a context without issuing another credential. A key has at
most 256 contexts, including the default. Client fields have the existing
256-byte UTF-8 bound and cannot contain NUL. `Session.Id` is the real context ID;
application sessions omit `UserId` and `UserName`. Ordinary login IDs retain
their existing meaning.

A key-only global catalog request omits `UserData`. An explicit real `UserId`
selects that user's catalog visibility and state projection, including folder
ACLs. Explicit favorite/played routes update only that target user's state.
User-state filters without a user return `400`, intentionally differing from
the captured reference `500` failures. Key playback and reports remain
userless and do not persist `user_item_data`, including when negotiation names
a target user.

Minimal PlaybackInfo and original-media requests can be userless. Key
PlaybackInfo always validates an explicit `UserId`, including minimal requests.
With no user, profile, or explicit `AudioStreamIndex`, it omits
`DefaultAudioStreamIndex`. An explicit target permits the evaluated default
audio index to appear; this does not implement every user language or audio
preference rule. A `DeviceProfile` with nonempty `TranscodingProfiles` requires
an explicit `UserId`; omission returns `400` (`playback_user_required`) rather
than the reference's observed `500`. Negotiation combines global conversion limits with
the target's `EnableAudioPlaybackTranscoding`,
`EnableVideoPlaybackTranscoding`, and `EnablePlaybackRemuxing`. For application
keys, `IsDisabled` or `EnableMediaPlayback` alone does not disable conversion.
A query `DeviceId` is a hint, never authentication. Without explicit client
headers, an owned `PlaySessionId` can recover its existing context only after
the parent token authenticates; it cannot transfer another key's playback.
This also applies to token-only playback Ping. Unknown or foreign Ping IDs
return an inert `204` without refreshing playback.

Revocation invalidates every child context, disconnects its WebSockets, and
cancels matching conversion work. Original responses revalidate before headers
and attempt revalidation every five seconds during delivery.

See the [key](../research/api-key-reference.md),
[playback](../research/api-key-playback-reference.md),
[context](../research/api-key-context-reference.md), and
[target-scope](../research/api-key-scope-reference.md) studies. Their bounded
negotiation and byte-range evidence does not establish GPU conversion,
full-client interoperability, or complete reference equivalence.
