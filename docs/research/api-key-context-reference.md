# Application-Key Client and User Context Reference

This independent bounded study follows the
[application-key playback capture](api-key-playback-reference.md) against
official Emby Server `4.9.5.0`. At `2026-09-09T20:35:18Z`, it added **36
sanitized records: 35 complete HTTP exchanges and one audit observation**.
The preceding 1065 records remain unchanged; the corpus now contains 1101.
Evidence uses the `keys-context-m5d-` prefix in the
[reference fixture directory](../../tests/compatibility/fixtures/reference/emby-4.9.5.0).

The results refine the previous study in three ways. An application key can
create distinct userless session DTOs through client authorization metadata.
The tested user-state filters fail when no user context is supplied. A real
target user's policy affects profile-based negotiation, even though explicit
favorite writes continue to succeed for that restricted or disabled user.
HTTP success alone therefore does not establish a usable negotiated stream.

## Boundaries and cleanup

The [dedicated recorder](../../scripts/test-env/reference-api-key-context.py)
revalidated the root-owned reference service, PID `3131777`,
`PrivateNetwork=yes`, ownership markers, private credential mode `0600`, and
a network namespace separate from the host. Requests used only the existing
`127.0.0.1:18097` listener inside that namespace through root SSH on `test-env`.

The recorder created one ordinary user,
`reference-keys-context-m5d-20260910-01`, and one application key labeled
`Goby Keys Context M5d 20260910 01 Alpha`. It used the already documented and
previously observed user/key creation and deletion routes. No temporary-user
password or login was created. One fresh administrator login used a dedicated
device ID. Only the new user's policy and favorites, the new key, and the
fresh request sessions could be mutated. No existing user policy, library,
setting, or media source was edited.

All **2130 preceding raw/export files**, **240 known source paths**, and
**1460 files in their existing private directories** matched their initial
SHA-256 hashes afterward. The private-file set overlaps the raw-record set;
these counts are not additive. Initial and final user membership and all
preexisting user policies matched. The owned user and key DELETEs and the
administrator logout each returned `204`. Final API-key records exactly
matched the initially empty list. Session-cache eviction after key deletion
was not separately observed.

Every HTTP exchange was complete. Actual captured response bodies totaled
**72,623 bytes**, below the 2 MiB total and 256 KiB per-response bounds. The
two video reads requested only bytes 0-31 of the known 90,299-byte synthetic
MP4. Returned conversion URLs were captured as data and never requested.
No encoder or conversion-delivery request was issued. This is a negotiation
study, not an encoder execution or full-playback check.

Raw evidence remains in root-only directories with mode-`0600` files under
`/opt/goby-test/exec-scratch/keys-context-m5d/private`; only sanitized exports
entered the repository. Response headers, credential fields, and URL tokens
were redacted, and all new raw/export pairs passed the shared redaction audit.
Tests, validation, and response analysis ran only on `test-env`; no local
verification or git operation ran.

## Authorization metadata creates separate userless sessions

The baseline key request supplied only `X-Emby-Token`. Two subsequent variants
added `Authorization: Emby ...` with the following nonsecret metadata. The
Authorization header is redacted as a sensitive header in exports; its
individual client metadata fields are retained separately in each request record.

| Field | Alpha | Beta |
| --- | --- | --- |
| `Client` | `GobyContext-alpha` | `GobyContext-beta` |
| `DeviceId` | `goby-context-client-alpha` | `goby-context-client-beta` |
| `Device` | `ContextDevice-alpha` | `ContextDevice-beta` |
| `Version` | `9.8.7` | `1.2.3` |

`GET /Sessions` returned `200`, `application/json; charset=utf-8` for all
three cases. The baseline key session used the application label as `Client`
and the server's device ID/name/version. Alpha created another session with a
different `Id`; Beta created a third. The Beta snapshot still contained the
Alpha and baseline sessions. The two new sessions used the supplied Client,
DeviceId, DeviceName, and ApplicationVersion exactly. All three omitted both
`UserId` and `UserName` and had empty `AdditionalUsers`.

Evidence: `sessions-baseline`, `metadata-alpha-sessions`, and
`metadata-beta-sessions`. The administrator recorder appeared separately with
its ordinary user identity. The observation establishes distinct session
representations for these client/device pairs; it does not establish the
complete session-key algorithm, collision behavior, or whether every metadata
field independently participates in session identity.

The key-list row also changed selected display metadata. Comparing
`keys-created` with `cleanup-keys`, row `Id: 23` retained its original AppName,
`ReportedDeviceId` equal to the server ID, numeric `DeviceId: 15`, and numeric
`UserId: 0`. Its `AppVersion` changed from `4.9.5.0` to Beta's `1.2.3`, and
`DeviceName` changed from the server name to `ContextDevice-beta`.
`DateLastActivity` appeared. These row fields must not be conflated with the
client/device fields of the additional session DTOs.

## Query DeviceId and bounded original delivery

Each metadata variant used a deliberately different `DeviceId` query value:
`goby-context-query-alpha` or `goby-context-query-beta`. No user ID was
supplied. Alpha used GET PlaybackInfo; Beta used POST with an empty JSON body.

| Request with differing header/query DeviceId | Result |
| --- | --- |
| Alpha `GET /Items/18/PlaybackInfo?DeviceId=...` | `200`, JSON |
| Beta `POST /Items/18/PlaybackInfo?DeviceId=...` | `200`, JSON |
| Alpha/Beta `GET /Videos/18/stream?Static=true&MediaSourceId=mediasource_18&DeviceId=...`, Range 0-31 | Both `206`, `video/mp4`, 32 bytes |

Evidence: `device-{alpha,beta}-{info,range}`. Both negotiation DTOs contained
MediaSources and nonempty PlaySessionIds, omitted ErrorCode and
DefaultAudioStreamIndex, and advertised all three support flags. Neither
static delivery request supplied a PlaySessionId.

Both media replies had `Content-Range: bytes 0-31/90299`, `Content-Length: 32`,
and `Accept-Ranges: bytes`; their prefix SHA-256 values matched the prior
playback study. The subsequent Beta session snapshot still represented Alpha
with its header DeviceId; no session with Alpha's query DeviceId appeared.
This establishes that these mismatched query values did not prevent the
tested original-media requests. It does not establish strict identity rules
for negotiated conversion URLs or all playback-report/session combinations.

## User-state filters without a user

These key-only requests had no UserId, cookies, or client authorization
metadata. Each used `/Items?Recursive=true&Limit=5` plus the specified filter.

| Filter | HTTP | Content-Type | Exact response body |
| --- | --- | --- | --- |
| `IsFavorite=true` | `500` | `text/plain` | `Exception of type 'SQLitePCL.pretty.SQLiteException' was thrown.` |
| `IsPlayed=true` | `500` | `text/plain` | `Object reference not set to an instance of an object.` |
| `IsPlayed=false` | `500` | `text/plain` | `Object reference not set to an instance of an object.` |
| `Filters=IsResumable` | `500` | `text/plain` | `Exception of type 'SQLitePCL.pretty.SQLiteException' was thrown.` |

Evidence: `filter-favorites`, `filter-played`, `filter-unplayed`, and
`filter-resumable`. None returned an Items collection, count, or UserData.
The observed reference does not return an empty successful result, silently
ignore these filters, or reject them with `400` for this request shape.
Other filters, combinations, omitted recursion, and explicit-user variants
were not part of this matrix. A native API may choose a coherent alternative,
but should not describe it as this observed reference behavior.

## Profile negotiation uses target-user policy

The baseline used a legal video DeviceProfile and request fields from the
pinned [PlaybackInfo model](../api/models.md#model-playbackinforequest) and
[TranscodingProfile model](../api/models.md#model-transcodingprofile):

```json
{
  "IsPlayback": true,
  "AutoOpenLiveStream": false,
  "EnableDirectPlay": false,
  "EnableDirectStream": false,
  "EnableTranscoding": true,
  "AllowVideoStreamCopy": false,
  "AllowAudioStreamCopy": false,
  "DeviceProfile": {
    "Name": "Goby-Key-Context-HLS",
    "MaxStreamingBitrate": 2000000,
    "DirectPlayProfiles": [],
    "TranscodingProfiles": [{
      "Type": "Video", "Container": "ts", "VideoCodec": "h264",
      "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming",
      "MaxAudioChannels": "2", "MinSegments": 1, "SegmentLength": 3
    }]
  }
}
```

The baseline also supplied the fresh user's `UserId`. It returned `200` JSON,
`SupportsTranscoding: true`, both direct support flags false, and a relative
HLS TranscodingUrl. The URL declared H.264/AAC, TS segments, both copy flags
false, and transcode reasons `ContainerNotSupported,DirectPlayError`.
`TranscodingSubProtocol` was `hls` and `TranscodingContainer` was `ts`.
That successful baseline demonstrates that the same profile could negotiate
a conversion before the policy changes. The URL was not followed.

Two administrator policy updates reused the fresh user's full initial policy
and applied these independent combinations. Both posted policies set
`EnableAudioPlaybackTranscoding`, `EnableVideoPlaybackTranscoding`, and
`EnablePlaybackRemuxing` to `false`.

| Case | Additional policy flags | Confirmed policy update/read | Explicit-user profile result |
| --- | --- | --- | --- |
| Playback disabled | `EnableMediaPlayback: false`, `IsDisabled: false` | `204` / `200` | `200`, all three Supports flags false, no TranscodingUrl |
| User disabled | `EnableMediaPlayback: true`, `IsDisabled: true` | `204` / `200` | `200`, all three Supports flags false, no TranscodingUrl |

Evidence: `profile-initial-explicit-user`, `playback-disabled-{set-policy,user,profile-explicit-user}`,
and `user-disabled-{set-policy,user,profile-explicit-user}`. The second policy
restored the initial playback flag while disabling the user; restrictions were
not accidentally accumulated. Both restricted responses still returned a
MediaSources array, a PlaySessionId, and DefaultAudioStreamIndex `1`. They
omitted ErrorCode as well as TranscodingUrl, TranscodingSubProtocol, and
TranscodingContainer.

This shows target-user policy affecting negotiated capabilities. It does not
isolate which of the combined restrictions caused each change, or establish
the independent effect of IsDisabled when transcoding remains allowed.
The previous study's successful minimal requests and static byte reads remain
valid observations, but do not justify treating targetUser as merely a
projection/preference context for every playback operation.

One otherwise identical profile request omitted UserId and returned `500`,
`text/plain`, with `Object reference not set to an instance of an object.`
Evidence: `user-disabled-profile-no-user`. This request ran after the second
policy update but did not name that user; the evidence does not establish that
the unrelated policy update caused the error. A no-user profile control before
the policy updates was not sampled.

## Favorites remain separately accepted

Under both policy combinations, the key's explicit
`POST /Users/{freshUserId}/FavoriteItems/18` returned `200` JSON with
`IsFavorite: true`, and the matching DELETE returned `200` with
`IsFavorite: false`. Both maintained `PlaybackPositionTicks: 0`,
`PlayCount: 0`, and `Played: false`. Evidence:
`{playback-disabled,user-disabled}-favorite-{add,remove}`.

These responses establish successful favorite toggles for the new restricted
and disabled target user. They do not turn the application key into that user
or prove that every explicit-user write bypasses every policy. Collection
visibility restrictions, user-login behavior, conversion execution, and full
session attachment remain outside this study.
