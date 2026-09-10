# Device Management Reference

Research date: 2026-09-10 (Asia/Shanghai).

The two user-device captures add **348 records: 346 complete HTTP exchanges
and two audits**, bringing the retained reference corpus to **1476**. This is
reference evidence; Goby's device-management API and administrator page are
not implemented by this research increment.

This study establishes the device-management contracts needed by Goby's
administrator dashboard and compatibility API. Device records, authentication
credentials, and current player sessions have distinct identities. The pinned
SDK does not explain how they are related or what deleting a device revokes.
Those behaviors require reference-server observations before implementation.

## Documentation baseline

The primary source is the fixed [Emby SDK 4.9.5.0 export](../sources/emby-sdk-openapi.snapshot.json),
revision `bdd0dd7c0801f6e069dff2795d80cddae6f91791`. All six non-camera
[DeviceService operations](../api/services/DeviceService.md) declare
administrator authentication. Their current official reference pages were
retrieved successfully on the research date:

| Operation | Documented request | Documented successful response |
| --- | --- | --- |
| [GET /Devices](https://dev.emby.media/reference/RestAPI/DeviceService/getDevices.html) | Optional `SortOrder` string | `200`, `{Items, TotalRecordCount}` |
| [GET /Devices/Info](https://dev.emby.media/reference/RestAPI/DeviceService/getDevicesInfo.html) | Required query `Id` string | `200`, `DeviceInfo` |
| [GET /Devices/Options](https://dev.emby.media/reference/RestAPI/DeviceService/getDevicesOptions.html) | Required query `Id` string | `200`, `DeviceOptions` |
| [POST /Devices/Options](https://dev.emby.media/reference/RestAPI/DeviceService/postDevicesOptions.html) | Required query `Id`; options body | `200`, empty body |
| [DELETE /Devices](https://dev.emby.media/reference/RestAPI/DeviceService/deleteDevices.html) | Required query `Id` string | `200`, body schema unspecified |
| [POST /Devices/Delete](https://dev.emby.media/reference/RestAPI/DeviceService/postDevicesDelete.html) | Required query `Id` string | `200`, body schema unspecified |

The response model distinguishes string `Id`, string `ReportedDeviceId`, and
integer `InternalId`. Other declared fields are `Name`, `LastUserName`,
`AppName`, `AppVersion`, `LastUserId`, `DateLastActivity`, `IconUrl`, and
`IpAddress`. `DeviceOptions` declares only an optional string `CustomName`.
The declaration does not establish field omission, empty/null semantics,
length limits, stable sorting, registration identity, or deletion effects.
See the [local models](../api/models.md#model-devices-deviceinfo).

Camera upload and its history are separate operations and are not exercised by
this administrator-device study. The earlier application-key studies establish
real client-session contexts; they do not establish device-registry behavior.
Their numeric `Auth/Keys.DeviceId` field must not be confused with the string
`DeviceInfo.ReportedDeviceId`.

## First bounded capture

The first invocation of [reference-devices.py](../../scripts/test-env/reference-devices.py)
recorded **152 complete HTTP exchanges and one audit**, with no truncated
responses, at `2026-09-09T22:14:10Z`. These 153 records extend the previous
1128-record corpus to 1281. This invocation is **partial**, with its original
output retained: the guard expected a missing previously deleted device to
return 404, but the reference returned 204 with an empty body. The guard stopped
before the repeated mutation; cleanup completed successfully.

The study used only the root-owned Emby 4.9.5.0 reference process, PID 3131777,
in its existing private network namespace. Six fresh login responses used
reserved reported device IDs. No preexisting token was sent in an HTTP request;
only uniquely identified new device rows could be changed. Each write first
matched the routed Info response to its observed `Id`, `ReportedDeviceId`, and
`AppName`, and rejected aliases belonging to an older or control device.

All 2256 earlier raw/export files, 240 known source paths, prior private files,
17 existing device records and their options were preserved. Existing user
membership and policy/configuration fields were unchanged; only the two
participating users' login/activity dates could change through the fresh
logins. Every acknowledged login token was subsequently proved invalid by a
separate authenticated `GET /Sessions` request returning 401. The final list
retained only the new control device, `Id = "21"`, as explicitly documented
history; its token was then logged out and independently denied. The capture
wrote 275,575 response bytes and took 0.695 seconds.

See the [first audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-audit.json).
Raw responses and credential material remain root-private on the test host;
only sanitized exports are stored in this repository. The exact executed
recorder, SHA-256
`f606a5375fdfb2cf996548dc15a7cbaec8726a890847919fb729dd5b6958e7b7`,
is retained in the first capture's private directory. Its preceding remote
synthetic safety suite passed all 11 tests with no HTTP, process, or capture
writes. No local tests or runtime probes were used.

## Observed user-device behavior

| Case | First observed result | Evidence |
| --- | --- | --- |
| Same reported ID, different users and clients | One device row keeps `Id = "22"`; `Name` follows the most recently reported device name | [Admin registration](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-devices-after-alpha-admin.json), [second user](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-devices-after-alpha-viewer.json), [different client](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-devices-after-alpha-other-client.json) |
| Device identity fields | `Id` is a decimal string; `ReportedDeviceId` is the submitted string; `InternalId` and `IconUrl` are omitted | [Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-lookup-canonical-info.json) |
| Reported-ID lookup | The reported string resolves to the same device as its decimal ID | [Reported lookup](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-lookup-reported-info.json) |
| Unknown reported ID or missing Id | Info and Options return 404 | [Unknown Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-lookup-unknown-info.json), [missing Options](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-lookup-missing-options.json) |
| Default options | `{}` | [Options](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-lookup-canonical-options.json) |
| Nonempty CustomName | POST returns 204; Info and current session names show the custom value | [Write](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-options-rename.json), [Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-options-rename-observed-info.json), [sessions](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-options-rename-sessions.json) |
| Empty, omitted, or null CustomName | POST returns 204; Options becomes `{}` and Info returns the reported name | [Empty](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-options-clear-empty-observed-options.json), [omitted](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-options-clear-missing-observed-options.json), [null](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-options-clear-null-observed-options.json) |
| Anonymous access | Tested device reads and mutations return 401 | [List](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-anonymous-devices.json), [delete](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-anonymous-delete.json) |
| Ordinary user access | Returns 403, including own-device Info, Options, and rename | [Own Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-viewer-self-info.json), [own rename](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-viewer-self-rename.json) |
| Delete by decimal ID | DELETE returns 204; all observed login credentials on that reported device become unauthorized | [Delete](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-delete-canonical.json), [cleanup proofs](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-audit.json) |
| Info after deleting that decimal ID | Returns 204 with an empty body; this differs from the unknown reported-ID result | [Post-delete lookup](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-delete-canonical-repeat-owned-proof.json) |

The first list has `TotalRecordCount = 0` despite nonempty `Items`. Ascending,
descending, lowercase, and invalid `SortOrder` values all return the same list
ordered by descending activity in this sample. The zero count must not be used
as an empty-list or deletion proof. See [ascending](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-devices-sort-ascending.json)
and [invalid sort](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-devices-sort-invalid.json).

Two distinctions prevent overinterpreting this very short capture. First,
all registrations share the same timestamp second; `AppName`, `AppVersion`,
and last-user fields still show the first administrator's values. Their
longer-term selection rule is not established. Second, clearing CustomName
restores the device Info name while existing Session DTOs retain the prior
custom name. This records a cache/projection difference, not a proposed Goby
requirement to keep stale session names.

The login responses also distinguish credentials from wire sessions: the
administrator and viewer using the same client/reported ID receive different
tokens but the same `SessionInfo.Id`; the viewer's different client receives
the same owned token but a different `SessionInfo.Id`. These [equalities](devices-credential-equalities.json)
were computed privately without exporting secrets; the derived report records
raw-source hashes and is not an additional HTTP fixture. They are additional session
compatibility evidence, not permission to share authorization across users.

## Completed second capture

The explicit second invocation, `GOBY_DEVICE_REFERENCE_RUN=2`, writes a new
`devices-m5e-2` directory and uses separate `02` client/device identities. It
requires the first capture's partial result and successful cleanup audit;
neither invocation overwrites an existing output directory. The second study
completes **194 HTTP exchanges and one audit** in **7.207 seconds**, with no
incomplete responses and 352,709 response bytes. Its [audit](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-audit.json)
passes every cleanup and preservation check.

All 2562 preceding raw/export files, 240 source paths, 1720 preexisting private
files, and the 18 baseline device records/options are unchanged. The first
control history remains intact. The second control row, `Id = "25"`, is the
only new device retained at the final list observation; its login is then
logged out and independently denied. All seven acknowledged login responses
are covered by final 401 proofs, including shared-token identities. Other
device rows created during this invocation are removed through owned-device
operations. Existing users retain membership, policies and configuration;
attributed participant login/activity timestamps are the only user projection
differences allowed.

The revised recorder passes **19 synthetic safety tests** on `test-env`, with
zero live HTTP, process calls, or capture writes. Recorder SHA-256 is
`96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d`;
test source SHA-256 is
`4b36878cf1f61716b159c860326dd10563488199663c988836a8ecc5f8d0e4bf`.
The executed recorder is retained in this capture's private directory too.

For an empty 204 after deletion, the guard requires prior positive ownership,
an acknowledged owned delete, and a complete bounded unpaginated list that
contains all baseline/control and nondeleted known-owned members, contains no
unexplained IDs, and excludes the target. The defective zero total is recorded
but never treated as evidence of absence. This proof permits only repeated
deletion; it cannot authorize creating options for a missing device. See the
[absence-list exchange](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-delete-canonical-repeat-owned-proof-absence-list.json).

The second observations refine the first as follows:

| Case | Observed result | Evidence |
| --- | --- | --- |
| Same reported ID, later different user | `Id = "26"` remains; the last user and app version change to the later viewer login | [Administrator](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-devices-after-alpha-admin.json), [viewer](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-devices-after-alpha-viewer.json) |
| Same viewer, later different client | Name changes, while AppName/AppVersion and activity still reflect the earlier same-user login in this sample | [Different client](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-devices-after-alpha-other-client.json) |
| Unknown numeric ID | Info returns empty 204; Options returns `200 {}` | [Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-lookup-unknown-numeric-info.json), [Options](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-lookup-unknown-numeric-options.json) |
| Repeated DELETE | Both calls return empty 204 | [First](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-delete-canonical.json), [repeat](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-delete-canonical-repeat.json) |
| Administrator POST delete alias | First and repeated calls return empty 204 | [First](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-delete-alias.json), [repeat](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-delete-alias-repeat.json) |
| Login again after deleting the device | Same reported ID now has `Id = "29"`; its prior nonempty CustomName is gone and Options returns `{}` | [New Info](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-alpha-relogin-info.json), [new Options](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-alpha-relogin-options.json) |
| Earlier deleted-device credentials after relogin | Old Alpha and Beta credentials remain 401; the unrelated Permission device remains 200 until cleanup | [Old Alpha](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-alpha-relogin-old-protected-alpha-admin.json), [unrelated device](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/devices-m5e-2-alpha-relogin-old-protected-permission-viewer.json) |

Six measured 1.1-second login intervals separate activity timestamps. This
resolves the first run's same-second last-user ambiguity, while showing that
the newest device name and newest client metadata are not necessarily selected
from the same update. The studies do not establish a general long-term cache
refresh or tie-breaking rule.

## Implementation implications and remaining evidence

A device registry needs a persistent identity separate from reported client
IDs and authentication-session IDs. The observed ordinary-device grouping is
across users and clients by reported ID; deleting a record retires the device's
ordinary credentials, and later registration starts a new record without the
old custom option. Preserve authentication/playback history separately from
the registry rather than cascading deletion into media state.

Goby should document coherent counts, deterministic ordering, input limits, and
name refresh behavior individually instead of treating reference count and
cache defects as implementation requirements. The native dashboard can expose
revision-checked management through its own cookie/CSRF contract while the
compatibility adapter keeps the recorded method, ID, status, and option shapes.
These are implementation recommendations, not delivered endpoints.

The separate [application-key/device study](key-devices-reference.md) now
records key-authorized ordinary-device management and shared server-device
deletion that revokes two keys, with stale session projections distinguished
from failed authentication. Header-specific hidden Info/deletion and key
recreation after shared-device deletion remain unsampled. The ordinary-device
captures above do not execute media, inspect WebSocket termination, perform
camera uploads, test Unicode/length limits, or establish full client
interoperability. Goby's device implementation is in progress; acceptance and
deployment remain pending.
