# Client capabilities, presence, and player-state projection

Client sessions are Emby token sessions. Administrator browser-cookie sessions
are a separate authentication domain and are not included in this API.

## Phase 3 Sessions subscriptions

Per-WebSocket session-list subscriptions passed the selected
[phase 3 protocol coverage](amd-media-phase3-20260919.md). They use the same current snapshot
builder as `GET /emby/Sessions`, rather than a second authority/projection model.

Send `SessionsStart` with Data as the SDK string `"delayMs,intervalMs"`.
Initial delay is 0 through 60,000 ms; interval is 1,000 through 60,000 ms.
Omitted Data or an empty string selects `"0,1000"`. After the delay, the server
sends a `Sessions` envelope containing the currently authorized session array,
then obtains fresh snapshots at the chosen interval. Restarting the subscription
replaces this socket's schedule. `SessionsStop` accepts omitted/empty-string Data
and stops only this socket; another socket using the same token is independent.
Null, other Data shapes and invalid ranges close the connection with code 1008.

Each snapshot checks current identity and catalog access, and the output path
rechecks the authority snapshot before delivery, dropping a snapshot if authority
changed. Session frames contain no token, media path or UserData. Logout, revocation and socket
closure stop the existing ownership path and its subscription timers. Reads are
bounded to 64 KiB and 64 messages per second, session payloads to 128 KiB, with
bounded timer/queue behavior. No session subscription survives as an autonomous
background subscription after its socket closes.

LibraryChanged, UserDataChanged and remote commands retain their existing paths.
No unobserved RefreshProgress message is fabricated. A client must requery the
relevant current-authority API after invalidation; event receipt by itself does
not prove a visible refresh. See [library refresh evidence](library-change-notifications.md)
and [global NextUp policy](next-up.md). Original-client negative observations
remain historical facts until a later source-bound client run establishes more.

Source: [subscription parser/lifecycle](../../internal/server/session_subscriptions.go),
[WebSocket output](../../internal/server/websocket.go), and
[shared session snapshot](../../internal/server/client_sessions.go).

## Routes and ownership

| Route | Behavior |
| --- | --- |
| `GET /emby/Sessions` | Bare array of currently visible client sessions |
| `POST /emby/Sessions/Capabilities` | Comma-separated query declarations; 204 empty success |
| `POST /emby/Sessions/Capabilities/Full` | JSON ClientCapabilities declaration; 204 empty success |
| `POST /emby/Sessions/Playing/Ping?PlaySessionId=...` | 204 empty success for known or unknown nonempty keys |

Root aliases and case variants of route literals use the same authentication.
Ordinary users see their own sessions; current administrators can list enabled
users' sessions. `Id` and `DeviceId` filter before the bounded result limit.
No access token, token digest, notification push token, or administrator cookie is
returned. Remote addresses and a numeric InternalDeviceId are not yet persisted
or projected; this is not the complete upstream SessionInfo model.

Capability reports always update the authenticated session. The reference accepts
missing or stale `Id` hints, so HTTP hints do not select a different session.
The internal repository still rejects attempts to target another session.
Client-supplied identity, role, or capability claims never grant permissions.

The supported declaration contains `PlayableMediaTypes`, `SupportedCommands`,
`SupportsMediaControl`, `SupportsSync`, `DeviceProfile`, `IconUrl`, and `AppId`.
Updates replace the snapshot; an empty Full object clears previous declarations.
Known fields are type checked; unknown fields are discarded at every level.
Input is limited to 64 KiB, bounded nesting/nodes, 128 entries per array, and
2,048 bytes per string. Optional nulls are omitted, while null array entries and
duplicate JSON keys are rejected. PushToken/PushTokenType are validated then
discarded because their vendor transports are not implemented. The separate
[GobyWebhookV1 registration API](../api/notifications.md) is explicit and does
not reinterpret vendor tokens or create registrations from capabilities.

Session responses expose declared media types and commands. They do not expose
the raw capability object, stored device profile, or arbitrary remote icon URLs.
`SupportsRemoteControl` requires both `SupportsMediaControl=true` and an established
WebSocket. `ControllableByUserId` filters live, declared sessions using current
same-user or administrator authorization. Ordinary users cannot inspect another
user's controller scope. See [events and remote commands](websocket-events.md).
Stored DeviceProfile does not implicitly change a later minimal PlaybackInfo request,
matching the captured reference control. Negotiation still consumes its explicit
request profile.

## Presence and authentication lifetime

The initial Goby presence window is five minutes. Authenticated requests refresh
`last_seen_at` at most once per 15 seconds. Capability reports also refresh that
activity. Established WebSockets also refresh presence, with current authentication
rechecked every five seconds. This is separate from the 30-day login lifetime; activity never extends
token expiration. Current account disable/revocation and administrator role are
rechecked in the database.

The optional `ActiveWithinSeconds` filter accepts 1 through 2,592,000 seconds.
Zero disables the presence filter while retaining authentication expiration and
account checks. The reference captured values 0 and 3600 on a freshly active
session but did not establish whether it implements this parameter. The default
window, zero semantics, and 256-session bound are explicit Goby policies, not a
claim of complete reference presence parity.

## Current playback

An active authentication session does not imply active playback. `NowPlayingItem`
appears only for an authorized Playing/Paused session. Selection takes the newest
such record per authentication session after access checks and before the result
limit; newer Prepared records cannot hide ongoing playback. Item metadata uses
the requesting user's current library access and omits paths and UserData from
this session projection. Stopped/expired sessions return the idle PlayState.

Migration `0008` adds validated player hints to playback sessions. Started,
Progress, and the first Stopped report can include CanSeek, IsMuted, VolumeLevel,
audio/subtitle indexes, PlayMethod, RepeatMode, PlaybackRate, Shuffle, and
SubtitleOffset. Missing/null fields retain previous hints; explicit false, zero,
and supported -1 track values remain meaningful. Volume is 0..100, indexes are
-1..int32 maximum, rate is finite and greater than zero through 10, and subtitle
offset fits int32. Offset units are not reinterpreted as timeline ticks.

Position, pause state, media-source identity, ownership, and duration continue to
use the existing state-machine columns. Hints do not control decoding or authorize
media operations. Duplicate Started and terminal sessions remain immutable;
concurrent partial hints merge transactionally rather than replacing each other.

Ping requires a nonempty PlaySessionId; omission returns the reference's 400
plain-text error. An unknown or foreign key is an inert 204 response and cannot
update another owner's session. A valid owned key refreshes live-session expiry
without changing playback position. This supersedes the initial SDK-derived
200 success response used before the M3b runtime capture.

Indexed external SRT/WebVTT, user-state WebSocket events, and initial remote
commands are implemented in separate increments. Full device management,
session-list subscriptions, additional events, and the broader shared-device
policy matrix remain work. No consumer player is added to the administrator
dashboard by these APIs.
