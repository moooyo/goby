# Administrator playback management

The native playback interface uses administrator cookies and CSRF. Playback
client tokens retain their separate Emby-compatible authentication boundary.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | /admin/v1/playback/sessions | Reads a current, bounded session snapshot with media, state, connectivity, and permitted controls. |
| POST | /admin/v1/playback/sessions/{id}/command | Requests an operation on the selected client. |
| POST | /admin/v1/playback/message | Requests a message on current playing clients and reports individual outcomes. |

The session snapshot includes Items, TotalRecordCount, Limit, AtCapacity, and
ObservedAt. Individual records include TransportConnected and Controls. A
connected client must also advertise the relevant capabilities and pass current
account, session, and library authorization.

Supported native commands are Stop, Pause, Unpause, Seek, DisplayMessage,
PlayNow, PlayNext, and PlayLast. Seek supplies an exact nonnegative
SeekPositionTicks value. Play supplies ItemIds and optional StartPositionTicks.
Native PlayNow accepts one item; queue insertion requires an explicit target
capability. The existing Emby-compatible multiple-item play contract is retained.

State commands can supply PlaySessionId to bind the request to the selected
playback instance. Queue delivery revalidates the current source, caller,
target, and capability state before the client receives an instruction.

DisplayMessage supplies Text and optional Header and TimeoutMs. Broadcast
results distinguish queued, offline, unsupported, and unavailable clients.
Messages are rendered as text by the Goby player.

## Acceptance and observation

A 202 response acknowledges server acceptance. A queued receipt includes
DeliveryCount and ExecutionConfirmed=false. These fields do not establish that
the player displayed a message or changed playback state.

The player changes its actual media element and submits authenticated playback
reports. The administrator refreshes those reports to observe pause, position,
resume, and stop. Unconfirmed HTTP results require explicit snapshot readback
before another write, without automatically replaying the old instruction.

## Playback observations

The existing activity log records playback.started and playback.stopped within
the same transaction as the authenticated player report. The first accepted
Started or Progress observation that enters Playing or Paused emits the start.
A committed stop emits the stop; stopping a prepared play does not invent an
earlier start. Preparation, expiry, presence-only Ping, and remote command
acceptance do not emit either observation.

Each observation identifies the canonical media item. A digest of the canonical
play identity and transition prevents duplicate facts from repeated reports or
client correlation aliases. The existing final account, credential, policy, and
media authorization check remains after the activity insertion. Failed writes
or final checks roll back the playback, user data, and activity together.

GET /admin/v1/activity?Action=playback.stopped reads retained stop observations
with current Actor.Name and optional Resource.Name enrichment. These names
reflect currently available users and items; they are not historical snapshots.
Missing or deleted resources retain their public identifiers without inventing
names. Application-key observations retain their actual key identity and have
no user name.

The session page displays six recent stop observations with their event times.
They use the activity retention policy, which defaults to 30 days. This bounded
log is not a permanent per-play reporting system and does not infer a device,
watch duration, or completed viewing from a stop report.

The Emby-compatible activity projection exposes AudioPlayback and
AudioPlaybackStopped for a currently available audio item, or VideoPlayback and
VideoPlaybackStopped for a currently available video item. Missing or
unclassified items retain the neutral playback.started or playback.stopped
type and their real ItemId. UserId is present only for an associated user.
The projection does not expose playback, authentication, or client correlation
identifiers. The media classification is current enrichment, not a stored
historical media-type claim.
