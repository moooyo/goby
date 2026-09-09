# Application-Key Playback Reference Contracts

This bounded capture extends the [application-key study](api-key-reference.md)
against the isolated official Emby Server `4.9.5.0` on `test-env`. It added
**59 sanitized records: 58 complete HTTP exchanges and one audit observation**
at `2026-09-09T20:23:24Z`. The retained corpus grows from 1006 to 1065 records.
Evidence uses the `keys-playback-m5d-` prefix in the
[reference fixture directory](../../tests/compatibility/fixtures/reference/emby-4.9.5.0).

An application key by itself successfully negotiated and read bounded original
media, and its own userless session accepted playback reports. Supplying a real
user ID to negotiation affected a source preference field, but did not turn the
key session into that user. Explicit favorite routes separately changed the
temporary user's favorite state. These are route-specific observations, not a
universal claim about key authorization or playback policy.

## Capture and ownership boundaries

The [recorder](../../scripts/test-env/reference-api-key-playback.py) imports the
existing hardened key recorder for secret collection, header redaction, private
file handling, and isolation checks. Its snapshot includes all **2012 preceding
raw/export files** and **240 known source paths**, rather than reusing the old
recorder's fixed 958-record snapshot. Each preceding hash matched afterward.

Before any request, the recorder revalidated the root-owned
`goby-emby-reference.service`, PID `3131777`, `PrivateNetwork=yes`, ownership
markers, and a network namespace separate from the host. Requests went to
`127.0.0.1:18097` inside that namespace. The administrator's existing credential
file was read only after root ownership and mode `0600` checks. One fresh
administrator login used its own recorder device ID.

The pinned [UserService inventory](../api/services/UserService.md) documents
`POST /Users/New`, `DELETE /Users/{Id}`, and `POST /Users/{Id}/Policy`. The
recorder used these operations to create one ordinary temporary user named
`reference-keys-playback-m5d-20260910-01`, change only that user's policy, and
delete it afterward. Creation returned `200` with a UserDto whose
`Policy.IsAdministrator` was `false`. No password or login was created for this
temporary user. The single new API key used the application label
`Goby Keys Playback M5d 20260910 01 Alpha`.

All API-key requests used only `X-Emby-Token`, without cookies or client
authorization metadata. User-state writes targeted the temporary user's ID.
Two missing-user favorite paths were explicit negative route controls. No
existing user policy, library, setting, or source was edited. Normal API use
advanced activity timestamps. No encoder or conversion endpoint was requested.

The video was the existing two-second synthetic MP4, item `"18"`, source
`"mediasource_18"`, size 90,299 bytes. The audio control reused the existing
97,098-byte synthetic MP3 from the prior audio study. Their paths had to occur
in the protected source baseline. Media reads requested `Range: bytes=0-31`;
the recorder would close after at most 4096 bytes if Range were ignored and
mark that response incomplete. Every observed media response honored Range.
Nonmedia response capture was capped at 256 KiB, and all response bodies
together were capped at 4 MiB. Actual captured bytes totaled **155,319**.

Raw evidence remains in root-only directories and mode-`0600` files under
`/opt/goby-test/exec-scratch/keys-playback-m5d/private`. Only sanitized exports
were copied into the repository. The final audit compared every raw/export
redaction pair and file mode. No credential was printed or placed in argv.

## Negotiation and original media

The pinned [MediaInfoService inventory](../api/services/MediaInfoService.md)
declares the GET/POST PlaybackInfo operations. GET used either no query
parameters or `UserId={temporaryUserId}`. POST used either `{}` or
`{"UserId":"{temporaryUserId}"}`. These were minimal requests: no DeviceProfile
or `IsPlayback` setting was supplied.

| Request | Key alone | Key with explicit temporary UserId |
| --- | --- | --- |
| `GET /Items/18/PlaybackInfo` | `200`, JSON | `200`, JSON |
| `POST /Items/18/PlaybackInfo` | `200`, JSON | `200`, JSON |
| `GET /Videos/18/stream?Static=true&MediaSourceId=mediasource_18`, Range 0-31 | `206`, `video/mp4`, 32 bytes | Same |
| `GET /Audio/{audioId}/stream?Static=true&MediaSourceId={audioSourceId}`, Range 0-31 | `206`, `audio/mpeg`, 32 bytes | Same |

Evidence suffixes: `no-user-info-{get,post}`, `explicit-user-info-{get,post}`,
`{no-user,explicit-user}-{video,audio}-range`. Media `UserId` was a query
parameter. Media requests did not carry `PlaySessionId`, so their success did
not depend on presenting the preceding negotiation's playback ID.

Every successful negotiation returned an object with `MediaSources` and a
nonempty string `PlaySessionId`, and omitted `ErrorCode`. Its source had all
three `SupportsDirectPlay`, `SupportsDirectStream`, and `SupportsTranscoding`
flags set to `true`. Neither `DirectStreamUrl` nor `TranscodingUrl` appeared.
`Content-Type` was `application/json; charset=utf-8`.

Comparing the source DTOs from the two baseline POST requests, the only
difference was `DefaultAudioStreamIndex`: it was omitted without a user and
was numeric `1` with the explicit user. This is positive evidence that
negotiation consumed the user context for a source preference. The GET pair
showed the same field distinction.

Video replies reported `Content-Range: bytes 0-31/90299`; audio replies reported
`Content-Range: bytes 0-31/97098`. All had `Content-Length: 32` and
`Accept-Ranges: bytes`. The captured video prefix hash was
`b1c35f1e6d75af66f96c70ff2572346caa6812dee411ad1134cffb50e33fba5c` in both
contexts; the audio prefix hash was
`57cf53a49c747ad37cca0554fb73df1c807b97d3ee6505c6d8e41f8d1f90042c` in both.
These hashes cover the captured prefix, not the full media payload.

## Playback report identity and user state

Two sequences used the corresponding POST negotiation's `PlaySessionId`,
the known item/source IDs, and `PlayMethod: "DirectStream"`. Neither supplied
`SessionId`. Started reported position zero with `IsPaused: true`; Progress
reported `PositionTicks: 10000000` and `EventName: "TimeUpdate"`; Stopped
reused that one-second position. No real-time playback or full decoding claim
is made. Each of the six report requests returned `204` with an empty body
and no `Content-Type`.

The explicit-user sequence also supplied the temporary `UserId` in each
report's JSON. Unlike [PlaybackInfoRequest](../api/models.md#model-playbackinforequest),
the pinned [PlaybackStartInfo](../api/models.md#model-playbackstartinfo),
[PlaybackProgressInfo](../api/models.md#model-playbackprogressinfo), and
[PlaybackStopInfo](../api/models.md#model-playbackstopinfo) do not declare
`UserId`. Its presence in those reports is therefore a compatibility probe,
not a documented user-association mechanism.

| Observation from `GET /Sessions` | Both report sequences |
| --- | --- |
| Session `Id` | Same key session before, during, and after playback |
| `UserId`, `UserName` | Both omitted throughout |
| `Client` | The newly owned application label |
| After Started | `NowPlayingItem.Id: "18"`; `PlayState.PositionTicks: 0`; paused |
| After Progress | Same playing item; `PlayState.PositionTicks: 10000000`; paused |
| After Stopped | `NowPlayingItem` and `PositionTicks` omitted; `CanSeek: false` |

Evidence: `sessions-before`, `{no-user,explicit-user}-sessions-{started,progress,stopped}`.
The normal administrator login remained a separate session with its own
`UserId`, `UserName`, device ID, and recorder client name. The key session's
displayed identity did not become the administrator or temporary user.

After each report, an explicit `GET /Users/{temporaryUserId}/Items/18` still
returned exactly these UserData values:

```json
{
  "PlaybackPositionTicks": 0,
  "PlayCount": 0,
  "IsFavorite": false,
  "Played": false
}
```

Evidence: `user-item-before` and
`{no-user,explicit-user}-item-{started,progress,stopped}`. The observed session
state changed while this user's persisted item state did not. The two-second
source and one-second progress point do not resolve general resume thresholds,
long-duration persistence, delayed writes, or all ways of associating users
with a session. No user-binding rule should be inferred solely from the empty
user-state delta.

## Favorites and current-user lookup

| Request authenticated with the key | Result |
| --- | --- |
| `GET /Users/Me` | `500`, `text/plain`, `Unrecognized Guid format.` |
| `POST /Users//FavoriteItems/18` | `404`, `text/plain`, file-not-found text |
| `POST /FavoriteItems/18` | `404`, `text/plain`, file-not-found text |
| `POST /Users/{temporaryUserId}/FavoriteItems/18` | `200`, JSON UserItemData with `IsFavorite: true` |
| `DELETE /Users/{temporaryUserId}/FavoriteItems/18` | `200`, JSON UserItemData with `IsFavorite: false` |

Evidence suffixes: `key-users-me`, `favorite-missing-user`, `favorite-global`,
`favorite-explicit-user`, `favorite-explicit-remove`, and
`item-after-favorites`. Both successful favorite responses kept zero position
and play count, and `Played: false`. The intermediate item detail also showed
`IsFavorite: true`. The two 404 controls establish only those exact paths;
they do not establish all omitted/empty user parameter behavior. `/Users/Me`
was an explicit compatibility probe, and the observed parsing error does not
establish a supported current-user endpoint.

## Temporary-user policy controls

The administrator applied each policy by posting a full copy of the new
user's initial policy with the stated single field changed. The second case
therefore restored `EnableMediaPlayback: true` while setting `IsDisabled`.
Both updates returned `204`; subsequent key-authenticated user GET responses
confirmed the exact effective flags.

| Confirmed temporary-user policy | Minimal POST PlaybackInfo, key alone / explicit UserId | Static video Range, key alone / explicit UserId |
| --- | --- | --- |
| `EnableMediaPlayback: false`, `IsDisabled: false` | `200` / `200` | `206` / `206` |
| `EnableMediaPlayback: true`, `IsDisabled: true` | `200` / `200` | `206` / `206` |

Evidence families: `playback-disabled-*` and `user-disabled-*`. All four
negotiations again advertised all three support flags and omitted ErrorCode;
explicit context still supplied `DefaultAudioStreamIndex: 1`. All four media
replies returned the same 32-byte MP4 prefix. These observations cover minimal
negotiation and original static delivery only. They do not establish behavior
for DeviceProfile negotiation, `IsPlayback: true`, transcoding, folder/rating
restrictions, disabled-user credentials, playback reports under restriction,
or an arbitrary policy bypass.

## Cleanup and remaining scope

The temporary user DELETE returned `204`. Final user membership and every
preexisting user's policy exactly matched the initial list. The owned key
DELETE returned `204`; the final key list exactly matched the initially empty
list. Administrator logout returned `204`. Both reported playback sequences
had already stopped successfully. The reference PID and isolation were
revalidated and all preceding evidence/source hashes remained unchanged.
Evidence: `user-delete`, `users-final`, `cleanup-key-0`, `keys-final`,
`admin-logout`, and `audit`.

All verification and response analysis ran through SSH on `test-env`; no local
tests, builds, validators, or runtime probes ran. The first helper launch
re-entered its shared module during namespace entry and refused to touch the
already existing old capture root before making HTTP requests. The corrected
entry path produced this complete capture; neither old captures nor the
failed helper source was overwritten.

No compatibility implementation changed. Application-key query transport,
multiple-key session interactions, full byte-stream playback, long-media user
state, actual client session attachment, and the policy cases listed above
remain outside this bounded study.
