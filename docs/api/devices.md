# Devices API

**Implemented and accepted in M5e.** The
[verification report](../development/verification-m5e-devices.md) records the
complete Linux race suite, isolated browser/restart acceptance, deployment,
and main-service device workflow. These results cover this increment; full
client compatibility and the broader project remain unfinished. Independent
Emby reference captures establish only their recorded HTTP behavior. See
[implementation and operations](../development/devices.md).

Goby separates ordinary Emby devices, native administrator sessions, and
application-key client contexts. A device ID identifies a registry generation;
it is not a user ID, authentication token, or `Session.Id`. Several ordinary
logins can belong to one reported device without sharing user authority.

## Native administrator API

These routes require the `goby_session` native administrator cookie. Every
POST requires the existing same-origin checks and `X-CSRF-Token`, accepts
`application/json`, and consumes exactly one UTF-8 JSON object of at most
4096 bytes. Fields are case-sensitive and must exactly match the documented
object; missing, additional, duplicate, null, and incorrectly typed fields are
invalid. POST routes accept no query parameters. Device handlers send
`Cache-Control: no-store` and `Pragma: no-cache`.

The native page lists current ordinary device records only. Native dashboard
logins stay on Sessions, and application credentials stay on API keys. The
shared application-key server device is excluded and cannot be renamed or
removed through these native routes, including by guessing its numeric ID.
An Emby administrator token or application key cannot replace the native cookie.

| Method and path | Request | Successful response |
| --- | --- | --- |
| `GET /admin/v1/devices` | Optional filters below | `200`, `{Items, TotalRecordCount, StartIndex, Limit}` |
| `POST /admin/v1/devices/{id}/options` | `{"Revision":"1","CustomName":"Living room"}` | `200`, the updated Device object directly |
| `POST /admin/v1/devices/{id}/delete` | `{"Revision":"1"}` | `200`, `{Id, DeletedAt, RevokedLoginCount}` |

Native `{id}` and `Revision` are canonical positive decimal strings within
the signed 64-bit range. JSON numbers, signs, leading zeros, and whitespace
are not accepted in their place.

The Device object contains exactly these safe fields:

| Field | JSON representation and meaning |
| --- | --- |
| `Id` | Positive decimal string for this ordinary registry generation |
| `Revision` | Positive decimal string used for administrator edit conflicts |
| `ReportedDeviceId` | Nonempty client-reported identifier |
| `Name` | Effective name: custom override, usable reported name, application name, then reported identifier |
| `ReportedName` | Raw reported name, possibly empty |
| `CustomName` | Custom name string or `null` |
| `AppName` | Recorded application name string |
| `AppVersion` | Recorded application version string |
| `LastUserId` | Last referenced user ID string or `null` |
| `LastUserName` | That user's current name when available, otherwise `null` |
| `CreatedAt` | UTC timestamp string |
| `LastSeenAt` | UTC timestamp string |
| `IpAddress` | Recorded address string, possibly empty |
| `ActiveLoginCount` | Nonnegative integer counting currently authorized ordinary Emby login credentials in this generation |

No device response exposes a token, token hash, recoverable secret, or vault
ciphertext. `ActiveLoginCount` excludes revoked or expired credentials and
disabled accounts. It is an authorization count, not an online, connected,
or currently playing indicator. A device remains listed with zero authorized
logins until explicitly removed.

### Listing and names

GET accepts only single-valued, case-sensitive `SearchTerm`, `StartIndex`, and
`Limit`. Unknown query fields and duplicate values return `400`.

| Parameter | Accepted value |
| --- | --- |
| `SearchTerm` | Case-insensitive literal substring, at most 256 UTF-8 bytes without control characters; searches custom/reported name, reported ID, application name, and last-user name |
| `StartIndex` | Canonical decimal integer `0` through `2147483647`; default `0` |
| `Limit` | Canonical decimal integer `1` through `200`; default `50` |

Results sort by `LastSeenAt` descending, then numeric `Id` descending. The page
and actual filtered `TotalRecordCount` come from one query snapshot, including
an empty page beyond the final item. Search treats `%` and `_` literally.
The administrator UI initially requests 25 rows.

`CustomName` is checked for controls, trimmed, and limited to 256 UTF-8 bytes
after trimming. An empty string clears the override; native JSON `null` is
invalid. Renaming changes only the manual override and its revision. Ordinary
activity, a later login, or changing recorded client metadata does not advance
a live ordinary record's management revision. Submitting the unchanged name
with the current revision is a no-op. A stale revision returns `409`
(`revision_conflict`), including an otherwise unchanged name.

An override appears in the device DTO and associated ordinary session name
projections. Clearing it restores each session's own stored raw device name;
it does not copy the most recent device-wide report into every session.
The registry's `Name` independently follows its documented fallback order.

### Removal and errors

Removal marks the selected generation deleted and revokes its ordinary Emby
credentials while retaining authentication and playback history. `DeletedAt`
is a UTC string. `RevokedLoginCount` counts credentials newly marked revoked,
which can include an expired credential that was not previously revoked; it
need not equal the list's authorization count.

A repeated removal of a known ordinary generation returns its original
`DeletedAt` and `RevokedLoginCount: 0`, even if the submitted positive revision
predates removal. An active record still requires its current revision.
Unknown IDs and non-native-visible records return `404`; renaming a removed
record also returns `404`. A later Emby login with the same reported ID creates
a new numeric device generation with no inherited custom name. Retrying the
old numeric ID cannot remove the replacement.

Invalid input returns `400`; unsupported content type returns `415`. Missing
or no-longer-authorized native credentials return `401` through the existing
administrator authentication contract. Native errors use
`{Error: {Code, Message}, RequestId}`; device field-validation responses also
include `Error.Fields`. After `404`, `409`, or an uncertain network result,
refresh the record before another mutation. The UI does not automatically
retry a potentially completed change.

## Emby DeviceService compatibility

The following six non-camera operations accept the existing Emby bearer-token
carriers. An ordinary Emby administrator login or an application key can manage
devices. Ordinary users receive `403`, including requests for their own
device; missing, revoked, or expired credentials receive `401`. The store
revalidates the actor's current credential and administrator authority rather
than relying only on an earlier middleware principal.

Paths work with or without `/emby`. Route literals such as `Devices`, `Info`,
`Options`, and `Delete` accept case-insensitive aliases. Query names are
case-insensitive; duplicate aliases are rejected. Identifier values retain
their spelling and case.

| Method and canonical path | Successful behavior |
| --- | --- |
| `GET /emby/Devices` | `200`, `{Items, TotalRecordCount}` for all current ordinary devices |
| `GET /emby/Devices/Info?Id={lookup}` | `200`, DeviceInfo for a current addressed record |
| `GET /emby/Devices/Options?Id={lookup}` | `200`, `{CustomName}` when overridden, otherwise `{}` |
| `POST /emby/Devices/Options?Id={lookup}` | `204`, empty body after updating or clearing the name |
| `DELETE /emby/Devices?Id={lookup}` | `204`, empty body after removal or an applicable idempotent numeric retry |
| `POST /emby/Devices/Delete?Id={lookup}` | Same removal behavior as DELETE |

DeviceInfo contains string `Id`, `ReportedDeviceId`, `Name`, `AppName`,
`AppVersion`, `IpAddress`, and UTC `DateLastActivity`. `LastUserId` and
`LastUserName` appear only when available. `InternalId`, `IconUrl`, native
revision fields, and login counts are omitted.

Listing accepts optional `SortOrder`, bounded to 256 UTF-8 bytes without
controls. Its value does not change the deterministic activity-descending,
numeric-ID-descending order. Compatibility listing is unpaged and returns an
actual count; Goby does not reproduce the observed Emby zero total for
nonempty `Items`. Pagination and native search fields are not accepted here.

Other operations require a single `Id` of 1–256 UTF-8 bytes without controls
or surrounding whitespace. `api_key` remains an accepted authentication query
parameter. Unknown query fields return `400`.

A canonical positive decimal identifier within the signed 64-bit range
addresses exactly that numeric generation. It never falls back to another
device's reported identifier when the numeric row is absent. Other valid
strings address reported-ID aliases. Keep the returned numeric ID when an
operation must continue to refer to one particular generation.

| Lookup state | GET Info | GET Options | POST Options | DELETE / POST Delete |
| --- | --- | --- | --- | --- |
| Current addressed record | `200` DeviceInfo | `200` options | `204` | `204` |
| Unknown or removed numeric generation | Empty `204` | `200 {}` | `404` | Empty `204` |
| Unknown reported identifier | `404` | `404` | `404` | `404` |
| Missing or empty `Id` | `404` | `404` | `404` | `404` |

Compatibility options accept one UTF-8 JSON object of at most 4096 bytes with
`application/json`. `CustomName` is case-insensitive; missing, `null`, or an
empty trimmed string clears it. A non-null value must be a string, without
controls and at most 256 UTF-8 bytes after trimming. Duplicate property aliases
are invalid. Other bounded JSON properties are discarded. These routes have
no revision parameter. They do not create options for a missing numeric record.

Device not-found and administrator-permission failures retain the captured
plain-text compatibility style. Validation and content-type errors use
`{ResponseStatus: {ErrorCode, Message}}`. Successful handlers disable caching
with the same headers as native device administration.

## Shared application-key server device

Application keys attach to a separate shared server-device generation. Client
authorization headers can create distinct key session contexts without
registering those header device IDs as ordinary devices. Neither native nor
compatibility device listing includes the shared server record. Its numeric
ID is available as `Auth/Keys.DeviceId`; its reported server identity is
available as `Auth/Keys.ReportedDeviceId`. Privileged compatibility Info,
Options, and deletion can address it directly.

The first Goby shared generation uses numeric ID `1`, preserving existing
schema-16 key metadata. After that generation is removed, a subsequently
created key gets a new shared generation from the same numeric allocator used
by ordinary devices. Old key rows retain their old generation. This retained
generation design is Goby's implementation choice; the reference capture did
not establish its post-removal key-creation behavior or numeric allocation.

Removing the shared generation revokes every attached parent application
credential and therefore all its client contexts. Removing an ordinary device
does not revoke an application key merely because a key context reports the
same device string. Native administrator credentials remain separately managed.
An application key that removes its own shared generation can receive the
successful mutation response, then fails subsequent authentication.

Shared aliases take precedence within the compatibility namespace. A known
removed shared reported alias remains in that family: an idempotent deletion
must not fall through to an ordinary device reporting the same string. If a
new shared generation exists, the reported alias addresses that active shared
generation. The old numeric ID still addresses only old history. For a known
removed shared reported alias with no active generation, Info and Options
return `404`, name updates return `404`, and deletion is idempotent `204`.

After committed deletion, the HTTP layer disconnects credential-scoped
WebSockets and cancels associated HLS/conversion consumers for every retired
ordinary login or parent key. Existing bounded authorization watchers cover
long original-media responses. HTTP reference credential probes do not by
themselves verify these Goby transport-retirement paths.

See the [ordinary-device reference](../research/devices-reference.md), the
[109-exchange fresh key/device audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/key-devices-fresh-m5e-2-attempt-2-recovery-audit.json),
and the [application-key contract](application-keys.md). Camera upload,
camera-upload history, and an end-user web player are outside this increment.
