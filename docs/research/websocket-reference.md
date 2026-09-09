# Emby 4.9.5.0 WebSocket reference

## Scope and evidence

This investigation recorded 46 additive `websocket-m3c-*.json` fixtures: 19 WebSocket connection transcripts and 27 supporting HTTP requests. It used the isolated official Emby Server 4.9.5.0 on `test-env`, reached at `127.0.0.1:18097` from the `goby-emby-reference.service` network namespace. All execution and verification were remote. The service retained `PrivateNetwork=yes`; no package, service configuration, Goby backend, or existing media file was changed.

The new ordinary user `reference-websocket-m3c` used device `goby-websocket-m3c-recorder`. Playback and favorite changes targeted only this user's data for the existing 600-second synthetic movie, item `"28"`, media source `"mediasource_28"`. Separate credentials, raw transcripts, and the baseline hash file remain mode 0600 under the protected reference directory. The source is [reference-websocket.py](../../scripts/test-env/reference-websocket.py), a standard-library extension that reads the existing recorder without editing it. Captures span `2026-09-09T01:35:37Z` through `01:37:24Z`, according to the remote recorder clock.

The exported [fixtures](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/) preserve HTTP status lines, ordered headers and their wire representation, WebSocket frame direction/opcode/masking/finality, text payloads, parsed JSON, and receipt/send timestamps. Credentials and private reference paths are sanitized; JSON primitive types and field presence are preserved. RFC Ping/Pong payloads are retained as base64. These are application-frame transcripts rather than packet captures: TCP segmentation and random outgoing masking keys are not retained. Each observation wait was at most ten seconds.

## Published protocol and SDK context

The official [Web Socket guide](https://dev.emby.media/doc/restapi/Web-Socket.html) describes changing a server HTTP URL to WS/WSS, passing `api_key` and `deviceId`, and using envelopes with `MessageType` and `Data`. It documents `UserDataChanged` with `Data.UserId` and `Data.UserDataList`, plus receiving `Play`, `Playstate`, and `GeneralCommand` remote-control events. Those remote commands were not generated in this investigation.

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
