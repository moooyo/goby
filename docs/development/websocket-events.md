# WebSocket events and remote control

Goby provides a token-authenticated event channel for Emby clients. The React/MUI
dashboard remains an administrator interface without a consumer player. The
[reference investigation](../research/websocket-reference.md) distinguishes
observed Emby 4.9.5.0 behavior from the implementation policies below.

## Connection and lifetime

Use a standard RFC 6455 GET upgrade at `/embywebsocket`. The observed aliases `/`,
`/emby`, `/emby/`, and `/emby/socket` are also accepted, including case variants of
literal path names. Ordinary requests to `/` still redirect to the dashboard.
Normal HTTP authorization headers or query `api_key` must contain an issued Emby
user token. Administrator cookies do not authenticate this channel. A supplied
`deviceId` is a hint; the authenticated database session fixes the user, session,
and device scope. A hint cannot subscribe to another session's commands.

Goby rejects missing, invalid, expired, or revoked tokens before upgrading, with
HTTP 401. This deliberately differs from the reference's observed silent 101
for unauthenticated connections. A successful Goby upgrade therefore establishes
an initially authenticated channel. It does not guarantee continued permission.

Explicit token authentication permits cross-origin browser clients. An Origin,
when supplied, must be a valid HTTP(S) origin or `null`. TLS remains the reverse
proxy's responsibility when that proxy terminates HTTPS. WebSocket compression
is disabled. No token, digest, or request URL is stored in the event hub.

Accounts and authentication sessions are revalidated every five seconds and
before each outgoing event. Logout immediately closes every socket for that
authentication session; other sessions remain independent. Connection activity
refreshes presence at most once per 15 seconds without extending the 30-day
token lifetime. RFC Ping is sent every 30 seconds with a three-second response
budget. There is no invented initial application message or application ping.

Incoming text and binary messages accept UTF-8 JSON envelopes with a nonempty
`MessageType`. Unknown application messages are inert. In particular,
`ReportPlaybackProgress` does not replace the verified HTTP playback-report
route. `SessionsStart` and `SessionsStop` are accepted as unknown messages but
do not currently produce a `Sessions` subscription. Neither ordinary nor admin
subscription probes produced that event in the bounded reference observations.

## User state notifications

Successful favorite/watched changes and HTTP Started, Progress, and Stopped
reports enqueue a marker after the database transaction commits. Ping does not
produce a state notification. A background worker reads the latest committed
user state, including supported ancestors and descendants of recursive watched
changes, rather than retaining an HTTP response snapshot. The message shape is:

```json
{
  "MessageType": "UserDataChanged",
  "MessageId": "generated-once-per-publication",
  "Data": {
    "UserId": "authenticated-user-id",
    "UserDataList": [
      {
        "ItemId": "catalog-item-id",
        "PlaybackPositionTicks": 0,
        "PlayCount": 0,
        "IsFavorite": true,
        "Played": false
      }
    ]
  }
}
```

`LastPlayedDate` and folder `UnplayedItemCount` appear when applicable. The event
contains user-data entries, not full item DTOs or local media paths. It is sent
only to that user's connections. A publication shares one `MessageId` and one
immutable state snapshot across recipients. Current library visibility is
checked again before writing; inaccessible entries are removed and empty
messages are dropped.

Pending markers coalesce by user and item. An update during an active read
schedules another read, so the final change is not discarded. Delivery may
combine intermediate changes and is not a durable event log. Each database page
contains at most 64 entries and uses its own consistent snapshot; a multi-page
recursive update is not an atomic notification transaction. Reconnecting clients
must reload the relevant item/user state through HTTP. No replay cursor or
exactly-once delivery is advertised.

## Remote-control routing

| Route | Message |
| --- | --- |
| `POST /emby/Sessions/{Id}/Playing` | `Play` with a validated PlayRequest |
| `POST /emby/Sessions/{Id}/Playing/{Command}` | `Playstate` |
| `POST /emby/Sessions/{Id}/Command` | `GeneralCommand` with complete JSON arguments |
| `POST /emby/Sessions/{Id}/Command/{Command}` | `GeneralCommand` with the named command and empty arguments |

These routes also accept the normal root aliases. The target is an Emby
authentication session ID, not an ItemId or PlaySessionId. A user can control
their own sessions; a current administrator can control other enabled users'
sessions. Other ordinary users' targets remain invisible. The broader shared
device and cross-user policy matrix is not claimed from the limited reference
observation, which involved `EnableSharedDeviceControl=true`.

`SupportsRemoteControl` is true only while the session declares
`SupportsMediaControl=true` and has an established socket. `ControllableByUserId`
filters session lists using the same current user/administrator rule and live
transport. Capabilities alone do not create a transport. `SupportedCommands`
remains a declaration, not an authorization allowlist.

For argument-bearing commands, use the complete JSON route:

```json
{"Name":"SetVolume","Arguments":{"Volume":"37"}}
```

Argument values remain strings. The server supplies `ControllingUserId`, replaces
any full-command `Id` with the trusted target, and generates the envelope's
`MessageId`. As observed in the reference, the named GeneralCommand route ignores
the JSON body and has empty arguments and no Data.Id. Playstate includes the
trusted target Id. Client-provided controller claims never establish authority.

Goby forwards `SeekRelative.SeekPositionTicks` as a signed 64-bit offset, including
negative and zero values. This follows the pinned field type; no reference
capture currently establishes behavior for a negative relative offset. Absolute
`Seek.SeekPositionTicks` and PlayRequest `StartPositionTicks` remain nonnegative.
Body and query values must agree when both are supplied.

`POST /emby/Sessions/{Id}/Playing` currently requires an `application/json` object
body even when all playback fields are supplied in the query. Send `{}` in that
case. Requests without a body are not supported; the pinned schema declares the
PlayRequest body as required.

Play requests authorize every requested item for both controller and recipient
and check the recipient's playback policy. Outbound control messages recheck
current account role, target capability, and applicable item access before
delivery. Remote commands only notify the client. They never run a host command,
start server playback, or fabricate a playback-state update. HTTP 204 means the
request was accepted, not that a client executed it. An offline target can return
204 with no delivery; commands are not held for a future reconnect.

## Resource limits and shutdown

These are explicit initial Goby policies, not inferred Emby limits:

| Resource | Bound / behavior |
| --- | --- |
| Connections | 128 total, 8 per user, 4 per authentication session; reserve before upgrade |
| Upgrade | Five-second write deadline, interrupted by shutdown; Origin limited to 2,048 bytes |
| Input | 64 KiB per message; 64 application messages per one-second window |
| Output queue | 16 messages and 256 KiB per connection; 128 KiB per encoded message |
| Notification work | 256 outstanding user/item markers total, 64 per user, including active work |
| Notification reads | 64 entries per page, 15 seconds per marker |
| Delivery | Two seconds for authorization/resource reads and five seconds for a socket write |

Input violations close the connection. Queue overflow disconnects the slow
consumer. Notification work overflow or failure disconnects that user's sockets
to require resynchronization; other users remain isolated. Publishing never
waits for a socket writer. No unbounded goroutine or durable offline queue is
created per HTTP state update.

Server shutdown rejects new subscriptions, cancels notifier and socket work,
waits for hijacked connections, and then releases catalog resources. Cleanup
continues if an individual Close caller reaches its deadline. This explicit
lifecycle is required because net/http.Shutdown does not close WebSockets.

The transport uses `github.com/coder/websocket` v1.8.15. Linux integration and
deployed verification are recorded in the [M3d verification report](verification-m3d-events.md).
Session-list subscriptions, library-change broadcasts, additional command
families, complete policy semantics, and real third-party-client acceptance remain
scheduled work.
