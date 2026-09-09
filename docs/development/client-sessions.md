# Client capabilities, presence, and player-state projection

Client sessions are Emby token sessions. Administrator browser-cookie sessions
are a separate authentication domain and are not included in this API.

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
discarded because external push-notification services are not implemented.

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
