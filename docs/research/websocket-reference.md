# Emby 4.9.5.0 WebSocket reference

## Scope and evidence

The initial M3c investigation recorded 46 additive `websocket-m3c-*.json` fixtures: 19 WebSocket connection transcripts and 27 supporting HTTP requests. It used the isolated official Emby Server 4.9.5.0 on `test-env`, reached at `127.0.0.1:18097` from the `goby-emby-reference.service` network namespace. All execution and verification were remote. The service retained `PrivateNetwork=yes`; no package, service configuration, Goby backend, or existing media file was changed. The later M3d administrator and remote-control follow-up is described separately at the end of this report.

The new ordinary user `reference-websocket-m3c` used device `goby-websocket-m3c-recorder`. Playback and favorite changes targeted only this user's data for the existing 600-second synthetic movie, item `"28"`, media source `"mediasource_28"`. Separate credentials, raw transcripts, and the baseline hash file remain mode 0600 under the protected reference directory. The source is [reference-websocket.py](../../scripts/test-env/reference-websocket.py), a standard-library extension that reads the existing recorder without editing it. Captures span `2026-09-09T01:35:37Z` through `01:37:24Z`, according to the remote recorder clock.

The exported [fixtures](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/) preserve HTTP status lines, ordered headers and their wire representation, WebSocket frame direction/opcode/masking/finality, text payloads, parsed JSON, and receipt/send timestamps. Credentials and private reference paths are sanitized; JSON primitive types and field presence are preserved. RFC Ping/Pong payloads are retained as base64. These are application-frame transcripts rather than packet captures: TCP segmentation and random outgoing masking keys are not retained. Each observation wait was at most ten seconds.

## Published protocol and SDK context

The official [Web Socket guide](https://dev.emby.media/doc/restapi/Web-Socket.html) describes changing a server HTTP URL to WS/WSS, passing `api_key` and `deviceId`, and using envelopes with `MessageType` and `Data`. It documents `UserDataChanged` with `Data.UserId` and `Data.UserDataList`, plus receiving `Play`, `Playstate`, and `GeneralCommand` remote-control events. Those remote commands were not generated in the initial M3c investigation.

The current official [C# ApiWebSocket source](https://github.com/MediaBrowser/Emby.ApiClient/blob/b2e124f789b6f086b7b5b7e88e07c902e946f5b2/Emby.ApiClient/ApiWebSocket.cs) appends `/embywebsocket` and sends `SessionsStart` with an interval pair such as `"1000,1000"`, followed by `SessionsStop` with an empty string. Its general sender uses binary WebSocket messages. The guide's older C# link returned 404, so the current repository path was used instead.

The official [JavaScript client](https://github.com/MediaBrowser/Emby.ApiClient.Javascript/blob/fdd0939ac596406ab92274f30fd83d226a677424/apiclient.js) also constructs an `/embywebsocket` URL. Its current `reportPlaybackProgress` implementation posts JSON to `Sessions/Playing/Progress`. The name `ReportPlaybackProgress` is therefore treated below as an explicit compatibility probe, not as a currently documented supported WebSocket operation. Sources were read on September 9, 2026; SDK links are pinned to the retrieved commits.

## Upgrade paths and authentication

All handshake requests used RFC 6455 version 13 with a fresh key. No HTTP authorization header, cookie, compression extension, or subprotocol was sent. Every recorded `101` response's `Sec-WebSocket-Accept` matched its request key.

| Request path, with valid token and the owned device ID | Upgrade result |
| --- | --- |
| `/` | `101 Switching Protocols` |
| `/emby` | `101 Switching Protocols` |
| `/emby/` | `101 Switching Protocols` |
| `/embywebsocket` | `101 Switching Protocols` |
| `/emby/socket` | `101 Switching Protocols` |

The corresponding five `websocket-m3c-handshake-*.json` fixtures contain approximately one second of observation per path. These results establish accepted upgrades, not equivalent event delivery on every alias. Subsequent message tests used `/embywebsocket`.

The observed response header order was `Connection`, `Date`, `Upgrade`, `Sec-WebSocket-Accept`; the connection and upgrade values were `Upgrade` and `websocket`. No initial JSON message or authentication error was received during the one-second handshake windows.

| `/embywebsocket` credential/device case | Upgrade | Own favorite events in the later concurrent control |
| --- | --- | --- |
| Valid token, original device ID | 101 | Two `UserDataChanged` messages |
| Valid token, no `deviceId` | 101 | The same two messages |
| Valid token, a new unmatched device ID | 101 | The same two messages |
| No token, original device ID | 101 | No messages |
| Fabricated invalid token, original device ID | 101 | No messages |
| Neither token nor device ID | 101 | Not included in the favorite control |

The five concurrent connections remained open across one favorite POST and its restoring DELETE, with three-second event windows after each request. The three valid-token connections received matching `MessageId` values and payloads. Their device variants therefore did not prevent this user-scoped notification. This does not establish device-to-session binding, remote-control routing, or whether arbitrary device IDs are interchangeable for other operations.

Missing and invalid tokens did not receive the controlled user event despite completing the upgrade. In these bounded windows the server did not send an authentication error or initiate a close. Consequently, **HTTP 101 alone is not proof of an authenticated notification channel**. Long-term behavior and other events on unauthenticated connections remain unverified. The sanitizer replaces all `api_key` values, including the deliberately invalid value; fixture names and the capture source identify each case.

Evidence: `websocket-m3c-auth-*.json`, `websocket-m3c-event-*.json`, and `websocket-m3c-favorite-*.json`.

## Initial traffic, ping, and close

The dedicated `websocket-m3c-initial-and-ping.json` transcript observed ten seconds of silence before sending an RFC Ping (opcode 9). A server Pong (opcode 10) arrived approximately 2.6 ms later with the identical 23-byte payload. There was no initial application message, server-initiated Ping, or application-level keepalive in this short window. This establishes Ping/Pong handling; it does not establish the server's longer heartbeat interval or an application-level keepalive contract.

At teardown the client sent a masked Close with code 1000. The captured connections then reached TCP EOF without an observed server Close frame. No reconnection, idle timeout, malformed frame, TLS, or proxy behavior was tested.

## UserDataChanged envelope

The favorite POST returned HTTP 200. The subsequent server text frame contained the following exact JSON structure; the DELETE produced a second event with `IsFavorite: false` and another `MessageId`:

```json
{
  "MessageType": "UserDataChanged",
  "MessageId": "1f185ce1331e4c1e941f5e467c7ff55a",
  "Data": {
    "UserId": "5760844f98b54d16b1c13073e5c9d73f",
    "UserDataList": [
      {
        "PlaybackPositionTicks": 0,
        "PlayCount": 0,
        "IsFavorite": true,
        "Played": false,
        "ItemId": "28"
      }
    ]
  }
}
```

`MessageId`, `UserId`, and `ItemId` were strings. The envelope included `MessageId` in addition to the two fields illustrated by the guide. Favorite notifications contained one entry, rather than a full item DTO. Each valid-token connection received both changes in order. The approximately 0.5-second receipt delay on the first connection is a capture observation, not a guaranteed delivery latency; the recorder polled the five sockets sequentially.

HTTP Started and Stopped requests also produced `UserDataChanged` messages on the owned connection. In those entries, `LastPlayedDate` was present with seven fractional second digits. Favorite events for the previously unused account omitted it. One playback transcript also contains an incidental `LibraryChanged` event (`ItemsAdded` 37-40) while the independent subtitle work was creating its library. That event was retained unchanged and is not attributed to a playback report.

## Playback progress and subscriptions

After normal PlaybackInfo negotiation and HTTP Started, an initial text-frame `ReportPlaybackProgress` requested `PositionTicks: 1200000000`. Two seconds later the HTTP session DTO had advanced only from approximately one second to three seconds, consistent with its running playback clock; item user data remained at zero. No acknowledgement or error frame was received. This alone would be insufficient to distinguish rejected progress from clock behavior.

The second control started a fresh play session paused at zero, then compared identical progress data over three transports:

| Step | Observed session position | Other evidence |
| --- | --- | --- |
| HTTP Started with `IsPaused: true`, position zero | 0 | Paused session confirmed by GET |
| WebSocket text `ReportPlaybackProgress`, then one-second wait | 0 | No acknowledgement/error |
| WebSocket binary `ReportPlaybackProgress`, then one-second wait | 0 | No acknowledgement/error |
| HTTP `POST /emby/Sessions/Playing/Progress` with the same Data object | 1200000000 | HTTP 204; item user data also 1200000000, `PlayedPercentage: 20` |

The WebSocket payload was not observed to apply in this account/session/version, while the same data succeeded over HTTP. The evidence supports using the verified HTTP route for this operation. It does not prove that every historical WebSocket format, account policy, or additional identification sequence would fail. Both probes used the original device ID associated with the login and a real negotiated PlaySessionId; no media was decoded or played in real time.

The optional SDK-derived `SessionsStart`/`SessionsStop` sequence was sent once as text and once as binary, with three to 3.5 seconds of observation before stopping. Neither produced a `Sessions` response for this ordinary account. Its session DTO had `SupportsRemoteControl: false`. No capability or policy change was made to obtain a different result. These are bounded negative observations, not proof that the documented subscription names are unsupported for every client or account.

Evidence: `websocket-m3c-sessions-subscription.json`, `websocket-m3c-playback-report-progress.json`, `websocket-m3c-session-*-ws-progress.json`, `websocket-m3c-playback-paused-control.json`, and `websocket-m3c-control-*.json`.

## Restoration and integrity

Each playback sequence ended with HTTP Stopped at zero and DELETE PlayedItems for the new user's movie. Final detail returned the original user state: `PlaybackPositionTicks: 0`, `PlayCount: 0`, `IsFavorite: false`, and `Played: false`, with `LastPlayedDate` absent. Favorite state was independently restored before playback began. The new ordinary account and its credentials remain available for reference reuse. No remote command targeted another client, and no policy, library, media, or other user's data was changed by this script.

The final remote audit compared all 464 raw/export files existing before setup (232 fixture pairs) by SHA-256 and found them unchanged. Concurrent new subtitle fixtures were allowed as additions and were not mistaken for baseline modifications. It also audited all 46 new sanitized fixtures against their private originals, preserving structure and primitive values while checking that known credentials were absent. Only these sanitized new exports were copied into the repository. The final audit output was:

```text
Audited 46 WebSocket fixtures; preserved all 464 original raw/export files.
```

The recorder's stages are `setup`, `handshakes`, `events`, `subscriptions`, `playback`, `playback_control`, and `audit`. Setup and capture stages refuse existing owned filenames; they are not rerun commands for the completed dataset. To repeat the experiment, allocate a new prefix, credential file, baseline file, and ordinary account. The audit stage is read-only and repeatable on `test-env`.

## M3d administrator and remote-control follow-up

This follow-up adds 25 `websocket-admin-m3d-*.json` fixtures: three WebSocket transcripts and 22 HTTP records. Its independent baseline covers the 317 fixture pairs present at repository baseline `9f83f5c`, including the complete M3c collection. The remote capture clock spans `2026-09-09T02:09:52Z` through `02:11:17Z`. Each observation wait was at most three seconds, within the five-second bound for this follow-up.

The existing reference administrator authenticated on a new dedicated device, `goby-websocket-admin-m3d-recorder`. Its login response confirmed `Policy.IsAdministrator: true` and returned a dedicated session and token. The original administrator credential file was not overwritten. The only command target was the existing ordinary recorder device `goby-websocket-m3c-recorder`, session `3051be5bda6e822e1f0a9c80cb6a5100`, belonging to `reference-websocket-m3c`. No user policy was changed and no real client received a command.

### Administrator subscription control

The administrator's `/embywebsocket` upgrade returned 101. The recorder sent `SessionsStart` with `Data: "1000,1000"` as a text frame, observed three seconds, sent `SessionsStop`, and observed another two seconds. It repeated that sequence with binary frames. No server message, including `Sessions`, appeared in the entire transcript.

Administrator status therefore did not make the earlier ordinary-account subscription probe positive. Because no subscription result appeared, the experiment cannot establish effective subscription stopping. It does not distinguish unsupported message handling, additional required client state, or a different contract in this server version. Evidence: `websocket-admin-m3d-admin-login.json` and `websocket-admin-m3d-sessions-subscription.json`.

### Remote capability and connection state

The ordinary target's HTTP session ID and device/user identity were checked before commands. Its initial `PlayableMediaTypes` and `SupportedCommands` were empty arrays. No playback report was sent during M3d.

| Target state | `SupportsRemoteControl` | Advertised arrays |
| --- | --- | --- |
| Before opening its WebSocket | false | Empty |
| WebSocket connected, no capability declaration | false | Empty |
| Connected; Simple capabilities declared `SupportsMediaControl=true` | true | `PlayableMediaTypes: ["Video","Audio"]`, `SupportedCommands: ["SetVolume"]` |
| WebSocket closed, same capability declaration still present | false | Arrays still populated |
| Original capabilities restored | false | Empty |

The declaration returned HTTP 204. This sequence establishes that capability declaration and an active WebSocket together made this particular target report remote-control support; opening its socket alone did not. The response DTO did not expose a `SupportsMediaControl` property, so its input value should not be invented in returned session DTOs.

GET Sessions constrained by the target `Id` plus `ControllableByUserId` returned the target for its owner and for the administrator. It also returned the target when the administrator queried with the other ordinary test user's ID. This is an observed accepted combination; it does not fully establish independent semantics or precedence for every session-filter parameter. The official [remote-control guide](https://dev.emby.media/doc/restapi/Remote-Control.html) describes using `ControllableByUserId` to find sessions a user can control.

Evidence: `websocket-admin-m3d-target-*.json`, `websocket-admin-m3d-capabilities-*.json`, and `websocket-admin-m3d-controllable-*.json`.

### Delivered remote-control messages

With the target connected and declared, the administrator posted `/Sessions/{Id}/Playing/Pause` with `{"Command":"Pause"}`. HTTP 204 was followed by one server text frame with this envelope:

```json
{
  "MessageType": "Playstate",
  "MessageId": "451c076929034ff1b129c579d58593dc",
  "Data": {
    "Id": "3051be5bda6e822e1f0a9c80cb6a5100",
    "Command": "Pause",
    "ControllingUserId": "fa0ec9ecc77b4fb487eceb8233c9a1fb"
  }
}
```

The recorder only receives frames. It does not perform the command or report playback state. The subsequent session DTO retained its default play state, including `IsPaused: false`, and had no playback item or position. HTTP command acceptance and delivery therefore must not be equated with confirmed execution by a client.

The administrator also posted `/Sessions/{Id}/Command/SetVolume` with `{"Arguments":{"Volume":"37"}}`. This returned 204 and delivered `GeneralCommand` with `Data.Name: "SetVolume"`, the administrator's `ControllingUserId`, and an **empty** `Arguments` object. Its Data did not contain `Id`.

A single complete-body control then posted `/Sessions/{Id}/Command` with `{"Name":"SetVolume","Arguments":{"Volume":"37"}}`. It returned 204 and delivered:

```json
{
  "MessageType": "GeneralCommand",
  "MessageId": "99993779527d4fa2b8dc4eaa300bfba7",
  "Data": {
    "Id": "3051be5bda6e822e1f0a9c80cb6a5100",
    "Name": "SetVolume",
    "ControllingUserId": "fa0ec9ecc77b4fb487eceb8233c9a1fb",
    "Arguments": {
      "Volume": "37"
    }
  }
}
```

The `Volume` value remained a string. The complete-body endpoint preserved this argument and included the target ID; the named endpoint did not preserve the supplied JSON argument in this test. The guide suggests arguments can accompany the named route, while its current [named-command REST reference](https://dev.emby.media/reference/RestAPI/SessionsService/postSessionsByIdCommandByCommand.html) lists only path `Id` and `Command`, without a body parameter. The [general-command REST reference](https://dev.emby.media/reference/RestAPI/SessionsService/postSessionsByIdCommand.html) explicitly declares a body containing `Name`, `ControllingUserId`, and `Arguments`. The runtime captures support using the complete-body endpoint when arguments must be delivered. No broader body/query precedence inference was made.

Evidence: `websocket-admin-m3d-pause-admin.json`, `websocket-admin-m3d-volume-admin.json`, `websocket-admin-m3d-remote-target.json`, `websocket-admin-m3d-volume-general-body.json`, and `websocket-admin-m3d-command-body-target.json`.

### Three bounded additional command cases

| Case targeting only the recorder | HTTP result | Observed delivery |
| --- | --- | --- |
| Administrator sends `VolumeUp`, which was absent from the target's `SupportedCommands: ["SetVolume"]` | 204 | `GeneralCommand`, `Name: "VolumeUp"`, empty `Arguments` |
| A different ordinary test user sends named-route `SetVolume` | 204 | `GeneralCommand` with that ordinary user's `ControllingUserId`, empty `Arguments` |
| Administrator sends Pause after the target socket closes, before capability restoration | 204 | No connected target was available; delivery was not established |

The distinct ordinary caller was the existing `reference-session-m3b` test account, user ID `2118172027b94fd18c3ca6e55478d6d3`. A GET of its user record confirmed `IsAdministrator: false`, `EnableRemoteControlOfOtherUsers: false`, and `EnableSharedDeviceControl: true`. Its command still reached the dedicated target, whose `UserId` belonged to the other ordinary account. Both policy flags and the exact target/session setup are necessary context: this observation does not establish a general cross-user authorization model or isolate shared-device behavior, and no policy was altered to probe further.

The undeclared-command case shows that the advertised command list did not act as a rejection list for this request. The disconnected case shows that HTTP 204 alone did not prove an available delivery channel. These findings should remain separate from capability-driven UI filtering and client execution acknowledgement.

Evidence: `websocket-admin-m3d-undeclared-command.json`, `websocket-admin-m3d-other-user-policy.json`, `websocket-admin-m3d-volume-other-user.json`, `websocket-admin-m3d-pause-disconnected.json`, and the corresponding target transcript.

### M3d restoration and integrity

Both command sequences restored the target's original empty media-type and command arrays with `SupportsMediaControl=false` and `SupportsSync=false`. Final session GETs confirmed `SupportsRemoteControl: false`, empty arrays, the original idle play state, and no current item or position. No media, library configuration, existing user policy, or other device capability changed. The administrator's new dedicated session and protected credentials remain for reference reuse; authenticated requests also advance the participating test accounts' ordinary session activity.

The final remote audit validated all 25 sanitized records against their private originals, checked credential absence and JSON structure/types, and preserved all 634 pre-existing raw/export files by SHA-256. Concurrent additions are permitted without weakening checks on the recorded baseline. The immutable M3c fixtures were included in this baseline.

```text
Audited 25 administrator WebSocket fixtures; preserved all 634 original raw/export files.
```

The additive source stages are `admin_setup`, `admin_subscriptions`, `remote_controls`, `admin_command_body`, and `admin_audit`. Captured stages reject existing names; only the audit is intended for repeat execution against this completed collection.
