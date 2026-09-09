# Original-file playback and durable user state

This increment implements original local-file delivery. It does not complete the
entire M3 milestone: external subtitles, client events, remuxing, transcoding,
and real consumer-client acceptance remain open. Client-session capabilities and
NextUp are described in their subsequent increment guides.
Goby's React/MUI dashboard remains an administrator application without a player.

## Client flow

1. Authenticate using the existing Emby user-token routes and client/device metadata.
2. Browse an authorized library and request item detail or `Fields=MediaSources,MediaStreams`.
3. Call `GET` or `POST /emby/Items/{Id}/PlaybackInfo`. POST can provide the official
   `DeviceProfile` structure and playback limits. Inspect the returned capabilities.
4. For a supported original-file decision, use the returned `DirectStreamUrl`, or
   the authorized `/Videos/{Id}/stream?Static=true` or `/Audio/{Id}/stream?Static=true` route.
5. Report start, progress, and stop through `/Sessions/Playing`,
   `/Sessions/Playing/Progress`, and `/Sessions/Playing/Stopped`.
6. Read per-user `UserData` and `/Users/{UserId}/Items/Resume` to restore progress.

Known route literals accept case variants and the API works with or without its
`/emby` prefix. Dynamic identifiers and encoded entity names retain their case
and segment boundaries. Administrator and health routes remain separate.

Minimal GET/POST negotiation returns source facts without a generated stream URL,
matching the captured reference behavior. A matching profile, explicit direct
delivery options, or `IsPlayback=true` can request the URL. Source IDs use
`mediasource_{ItemId}`. `PlaySessionId` is distinct from the authentication session
ID and is bound to the authenticated user, device, item, and source.

## Negotiation boundary

Both `SupportsDirectPlay` and `SupportsDirectStream` describe delivery of the
complete original file in this increment. DirectStream does not imply remuxing.
`SupportsTranscoding` is always false. The evaluator checks container/codec
profiles, stream selection, known required conditions, and explicit bitrate or
channel limits. Unknown optional facts remain unverified; unknown required facts
decline the applicable capability. Missing `IsRequired` defaults to false.

With no profile, source capabilities describe original-file availability; the
client still decides whether it can decode that source. An explicit
`EnableTranscoding=false` permits the reference-observed original fallback after
a profile-only mismatch. It does not bypass authorization, invalid track indexes,
explicit request limits, or unsupported external/burned-in subtitle delivery.
Selected audio tracks remain in the original file and require client-side track
selection. Only embedded subtitle delivery is currently evaluated.

When conversion would be necessary, no conversion URL is emitted. A request with
no supported direct method receives `ErrorCode=NoCompatibleStream`, except that
explicitly disabling all three methods returns the source with all flags false,
as observed in the reference. Detailed profile-condition combinations are Goby
design choices until broader reference/client coverage establishes exact parity.

## HTTP delivery and authorization

`GET` and `HEAD` support byte ranges, suffix/multipart ranges, `If-Range`, ETags,
and conditional requests. MIME types and container names come from probe facts
and a compatible filename extension. `stream`, `stream.{Container}`, and
`original.{Container}` are delivery aliases, never arbitrary filesystem paths.
Requests requiring container conversion are rejected. Start-position ticks do
not truncate an original container; the client seeks using its demuxer or ranges.

Every request resolves its token, current user state, library access, playback
policy, and indexed source before serving bytes or returning 304. Administrator
cookies cannot authenticate these routes. `EnableMediaPlayback=false` also
applies to administrators. An original-file URL's `PlaySessionId` is correlation
data, not a bearer credential or authorization bypass; reports enforce its owner
binding separately. A valid user token remains necessary even with that ID.

The service opens approved roots and the indexed file through anchored Linux file
descriptors. It validates inode, size, mtime, and ctime against the scan snapshot,
without hashing an entire movie before starting delivery. The quoted ETag hashes
these snapshot identifiers, not the full media contents. Changed sources return
503 until rescanned. Last-Modified includes ctime when later than mtime; ETags are
the precise validator because HTTP dates have only second precision.

Original streaming has 64 concurrent request slots and four bounded media-open
workers. File opening has a 20-second request budget; cancellation does not
release a worker slot while its underlying filesystem work remains blocked.
Disconnects close the held delivery descriptor. This is not a claim of bounded
kernel I/O completion on an unavailable network filesystem.

Goby deliberately uses standard suffix-range and 416 behavior rather than copying
the observed reference suffix-range defect. See the
[reference report](../research/reference-server.md) for the evidence and limits.

## State and session lifecycle

Migration `0006` stores `user_item_data` and `play_sessions` in PostgreSQL.
Started counts one playback; duplicate starts/stops do not count twice. Progress
can move backward after a seek. Stop is terminal and later progress cannot revive
it. Concurrent reports apply in database-lock processing order; strict HTTP
arrival order is not guaranteed and the initial contract has no reliable client
sequence number. Client-supplied duration, user identity, and role hints
never establish authority. Started, Progress, and Stopped return 204 with no body,
matching the runtime reference. Ping also returns 204 after a subsequent runtime
capture corrected the original SDK-derived 200 implementation; missing keys are
400 and unknown/foreign nonempty keys are inert. See [client sessions](client-sessions.md).

Resume initially requires a duration of at least 120 seconds and a position from
2% through less than 90%, excluding played items. Stop at or above 90% marks the
item played and clears its resume position. These are Goby's initial policy;
boundary behavior is not claimed as complete Emby parity. Resume is ordered by
last play and filtered by current library access before pagination and counting.

Watched/favorite mutations return user-data objects without `ItemId` or `Key`.
Marking a supported folder watched/unwatched applies to its descendants in the
same library in one transaction. Folder `Played` and `UnplayedItemCount` are
derived from current descendant state; empty folders report false and zero.
Folder favorites remain individual, and folder playback counts/dates stay empty.
Queries support `IsPlayed`, `IsFavorite`, and the initial `Filters` values
`IsPlayed`, `IsUnplayed`, `IsFavorite`, `IsFavoriteOrLikes`, and `IsResumable`.
Likes are not separately stored; `IsFavoriteOrLikes` currently matches favorites.

Prepared/live sessions expire after 30 minutes without activity. Creation is
limited to 32 active sessions per authentication session and 128 per user.
Cleanup targets 256 recent terminal sessions per user and prunes entries older
than seven days. Prepare and Started without an explicit play-session ID trigger
cleanup; each call deletes at most 256 terminal rows, so this is not an immediate
hard retention limit. Progress, Ping, and Stopped do not trigger cleanup. Legacy
Started reuses or creates the caller's active item/source session. Other legacy
reports prefer that active session, otherwise use the latest terminal session as
a no-op, and return NotFound when no match exists. They cannot modify another
authentication session.

## Upgrade requirement

Probe cache version 2 adds Linux ctime and additional codec facts. Existing media
must undergo a normal library scan after upgrading before it can be opened for
playback. The scanner detects the older cache and probes those files again.
Automatic scheduling of this scan is not yet implemented. Account/dashboard
availability is independent of the scan; old probe data is not silently trusted
for streaming. No additional configuration variables or frontend assets are
required for this increment.
