# Emby API implementation scope

Status: **planning only; zero implemented operations and no verified clients**.

The objective is an independent open-source Linux media backend that existing Emby-compatible clients can connect to. This includes browsing and playback APIs even though Goby's own React/MUI website is exclusively an administrator dashboard.

The full upstream inventory is [535 operations](catalog.md), with [local request/response models](models.md). That count describes the fixed SDK export, not the complete behavior of every Emby release. The newer baseline is **SDK 4.9.5.0 Release**; see [source provenance](../sources/README.md).

## Delivery definitions

| Stage | Deliverable | Compatibility claim allowed after verification |
| --- | --- | --- |
| P0 | Server setup, users/policy, library ingestion, client login/browse, direct video/audio playback, basic subtitles, progress, administrator operations | The tested direct-play workflows and media profiles only |
| P1 | Remux/transcode/HLS, full playback lifecycle, broader music/TV experience, richer user data, metadata management, client aliases/events | The exact tested client versions and feature/media profiles |
| P2 | Additional upstream features needed for broader feature coverage | Only each feature that passes its acceptance matrix |
| Deferred | Live TV/DVR, DLNA, sync/offline packages, channels, synchronized parties, optional plugin ecosystem | No support claim until scheduled and implemented |
| Excluded | Emby consumer web application, Emby Connect/cloud identity, Emby package distribution and proprietary binary plugin compatibility | Explicitly outside this project's current scope |

P0 and P1 are implementation order, not a redefinition of the final goal. A direct-play MVP does not constitute a general Emby replacement. Revisit deferred features as compatibility coverage expands. Do not return successful empty results merely to inflate endpoint coverage.

The catalog uses service-level labels such as `CORE-CANDIDATE`; a service can contain both essential and optional operations. The selections below are the actual starting scope. Unlisted operations remain in the full catalog and default to later evaluation, even if their service is a core candidate.

## P0: a complete initial media flow

Routes below are relative to `/emby`. A table cell listing multiple verbs requires each listed method; similar-looking routes are not automatically interchangeable. Read the linked service reference for exact parameter names and schemas.

| Feature | Initial route selection | Implementation obligation |
| --- | --- | --- |
| Server identity and reachability | `GET /System/Info/Public`; `GET /System/Info`; `GET`, `HEAD`, `POST /System/Ping` | Stable ID, truthful capabilities, correct public/admin field exposure; ping payload needs a fixture |
| Sign-in | `GET /Users/Public`; `POST /Users/AuthenticateByName`; `POST /Users/{Id}/Authenticate`; `GET /Users/{Id}`; `POST /Sessions/Logout` | Header parsing, password verification, token issue/revoke, rate limits, hidden/disabled user behavior |
| Administrator user operations | `GET /Users/Query`; `POST /Users/New`; `POST /Users/{Id}`; `POST /Users/{Id}/Password`; `POST /Users/{Id}/Policy`; `POST /Users/{Id}/Configuration`; `DELETE /Users/{Id}`; `POST /Users/{Id}/Delete` | Shared policy enforcement; prevent removing the last usable administrator; invalidate affected credentials |
| Application tokens | `GET`, `POST /Auth/Keys`; `DELETE /Auth/Keys/{Key}`; `POST /Auth/Keys/{Key}/Delete` | Explicit application principals, administrator control, redaction, immediate revocation |
| Client sessions | `GET /Sessions`; `POST /Sessions/Capabilities`; `POST /Sessions/Capabilities/Full` | Device identity and profile storage; filter visible sessions by user policy |
| Library roots | `GET /Library/VirtualFolders/Query`; `POST /Library/VirtualFolders`; `POST /Library/VirtualFolders/LibraryOptions`; `POST /Library/VirtualFolders/Name`; `POST /Library/VirtualFolders/Delete` | Persistent library model, safe Linux roots, options validation, clearly separated library removal and file deletion |
| Library paths | `POST /Library/VirtualFolders/Paths`; `POST /Library/VirtualFolders/Paths/Update`; `POST /Library/VirtualFolders/Paths/Delete` | Canonical path allowlist, rename/move handling, permissions; legacy DELETE contracts need additional evidence |
| Directory picker | `GET /Environment/DefaultDirectoryBrowser`; `GET /Environment/DirectoryContents`; `GET /Environment/ParentPath`; `POST /Environment/ValidatePath` | Restrict enumeration to administrator-approved Linux roots; do not emulate Windows drive semantics |
| Scan and task control | `POST /Library/Refresh`; `POST /Items/{Id}/Refresh`; `GET /ScheduledTasks`; `GET /ScheduledTasks/{Id}`; `POST`, `DELETE /ScheduledTasks/Running/{Id}`; `POST /ScheduledTasks/Running/{Id}/Delete` | Durable jobs, bounded scan/probe concurrency, cancellation, progress and errors |
| Client library navigation | `GET /Users/{UserId}/Views`; `GET /Users/{UserId}/Items/Root`; `GET /Users/{UserId}/Items`; `GET /Items`; `GET /Users/{UserId}/Items/{Id}` | Stable IDs, ACL filtering before pagination/counting, DTO field projection and correct response envelope |
| Home and continue watching | `GET /Users/{UserId}/Items/Latest`; `GET /Users/{UserId}/Items/Resume`; `GET /Shows/NextUp` | Latest-item array vs query-result distinction, user-specific progress, deterministic ordering |
| Television hierarchy | `GET /Shows/{Id}/Seasons`; `GET /Shows/{Id}/Episodes` | Series/season/episode relationships, numbering and ordering; Episodes response fixture required |
| Search | `SearchTerm` on `GET /Items` and `GET /Users/{UserId}/Items` | Defined Unicode matching and ordering; all results obey the same library access rules |
| Item and user artwork | `GET`, `HEAD /Items/{Id}/Images/{Type}` and `/Items/{Id}/Images/{Type}/{Index}`; `GET /Items/{Id}/Images`; `GET`, `HEAD /Users/{Id}/Images/{Type}` and `/Users/{Id}/Images/{Type}/{Index}` | Safe image lookup, resize/cache tags, correct content type and conditional responses |
| Playback negotiation | `GET`, `POST /Items/{Id}/PlaybackInfo` | Accurate sources/streams, requested audio/subtitle selection, user/device restrictions, truthful direct-play support |
| Direct video | `GET`, `HEAD /Videos/{Id}/stream`; `GET`, `HEAD /Videos/{Id}/stream.{Container}`; `GET`, `HEAD /Videos/{Id}/{StreamFileName}` | Authenticated streaming, byte ranges, lengths, seek behavior, literal stream-route precedence over filename wildcard |
| Direct audio | `GET`, `HEAD /Audio/{Id}/stream`; `GET`, `HEAD /Audio/{Id}/stream.{Container}`; `GET`, `HEAD /Audio/{Id}/{StreamFileName}` | Same media authorization and HTTP semantics; supported source formats only |
| External text subtitles | `GET`, `HEAD /Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` and `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | Stable stream indexes, encoding, authorized conversion to supported text formats |
| Playback reporting | `POST /Sessions/Playing`; `POST /Sessions/Playing/Progress`; `POST /Sessions/Playing/Ping`; `POST /Sessions/Playing/Stopped` | Idempotent state transitions, correct ticks, reconnects, durable resume position and stopped-session cleanup |
| Watched and favorite state | `POST`, `DELETE /Users/{UserId}/PlayedItems/{Id}`; `POST`, `DELETE /Users/{UserId}/FavoriteItems/{Id}`; corresponding `POST .../{Id}/Delete` routes | Correct per-user response DTOs and play-count behavior; do not leak another user's state |
| Server settings and logs | `GET`, `POST /System/Configuration`; `GET`, `POST /System/Configuration/{Key}`; `GET /System/Logs/Query`; `GET /System/Logs/{Name}`; `GET /System/ActivityLog/Entries` | Supported settings documented individually; no arbitrary path reads; redacted logs and audit records |

HTTP route presence is only part of P0. Seed supported movie, series, episode, music-album and audio item models; scan usable local metadata; implement an authorization service shared by every read, stream, and mutation. The [client research](../research/client-compatibility.md) details data-shape obligations.

## P1: broader playback and client compatibility

| Feature | Route selection / family | Implementation obligation |
| --- | --- | --- |
| Audio negotiation | `GET`, `HEAD /Audio/{Id}/universal`; `GET`, `HEAD /Audio/{Id}/universal.{Container}` | Capability parameters in narrative documentation exceed the generated spec; complete the contract from reference exchanges |
| Video HLS | `GET`, `HEAD /Videos/{Id}/master.m3u8`; `GET /Videos/{Id}/main.m3u8`; `GET /Videos/{Id}/live.m3u8`; `GET`, `HEAD /Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | Feasible playlists, correct MIME/types and timing, safe job reuse, seek/cancel, authorized segments |
| Audio HLS | Corresponding `/Audio/{Id}/master.m3u8`, `/main.m3u8`, `/live.m3u8`, and `/hls1/...` operations documented in [DynamicHlsService](services/DynamicHlsService.md) | Audio-only negotiation and stream lifecycle |
| HLS variants | [VideoHlsService](services/VideoHlsService.md), subtitle playlists, and generated segment URL handling | Serve the paths actually emitted by the server; the export alone does not specify every generated segment path |
| Stop transcoding | `DELETE /Videos/ActiveEncodings`; `POST /Videos/ActiveEncodings/Delete` | Validate device/session ownership, terminate jobs safely, expire caches |
| Open/close dynamic sources | `POST /LiveStreams/Open`; `POST /LiveStreams/MediaInfo`; `POST /LiveStreams/Close` | Implement when a supported source advertises `RequiresOpening`; independent of full Live TV feature support |
| Advanced subtitles | Start-position variants, `/Videos/{Id}/subtitles.m3u8`, `/live_subtitles.m3u8`, attachments, remote subtitle search/download | Track offset and language semantics; fonts and bitmap subtitles may require FFmpeg burn-in |
| Bandwidth decisions | `GET /Playback/BitrateTest`, bitrate/device-profile input fields | Bound generated output, apply user limits, avoid measuring once and assuming permanent bandwidth |
| Legacy playback reporting | `POST /Users/{UserId}/PlayingItems/{Id}`; `POST .../Progress`; `DELETE .../{Id}` and `POST .../{Id}/Delete` | Adapt into the same internal playback state machine with version-specific request decoding |
| Rich user state | `POST /Users/{UserId}/Items/{ItemId}/UserData`; `POST /Users/{UserId}/Items/{Id}/HideFromResume`; rating operations | Respect field-specific semantics and concurrent updates |
| Preferences | [DisplayPreferencesService](services/DisplayPreferencesService.md); typed settings and partial configuration routes | Isolate by user/client/key, preserve compatible values; resolve inconsistent write schemas |
| Music and facets | [ArtistsService](services/ArtistsService.md), [GenresService](services/GenresService.md), [MusicGenresService](services/MusicGenresService.md), [PersonsService](services/PersonsService.md), [StudiosService](services/StudiosService.md), supported tag/rating facets | Accurate artist/album identities, folder hierarchy, search, facet counts, artwork and permission filtering |
| Playlists and collections | [PlaylistService](services/PlaylistService.md), [CollectionService](services/CollectionService.md) | Ordered entries, owner/share rules, stable entry IDs, additions/removals/reordering |
| Extra navigation | Ancestors, counts, similar items, additional parts, special features, trailers, home sections | Return only available/supported content; do not invent recommendation semantics |
| Metadata administration | [ItemUpdateService](services/ItemUpdateService.md), selected movie/series/music [ItemLookupService](services/ItemLookupService.md), [RemoteImageService](services/RemoteImageService.md), image upload/delete | Separate technical vs provider metadata, provenance, locks, idempotent refresh and restricted fetches |
| Devices and remote session commands | [DeviceService](services/DeviceService.md), [SessionsService](services/SessionsService.md) command/viewing/queue routes | Advertised command capabilities, user ownership, administrator override policy, event delivery |
| Operations | Task triggers, partial settings, log lines, supported encoder options, system restart/shutdown | Configuration reload semantics, supervisor integration, auditable privileged actions |

P1 transcoding starts with software profiles, then adds individually tested hardware profiles. Both correctness and sustained resource limits are release requirements. See [playback and transcoding](../research/playback-and-transcoding.md).

## Protocols outside the REST inventory

| Protocol | Scope | Evidence required |
| --- | --- | --- |
| Emby WebSocket | Include session/event infrastructure early; implement the messages required by supported client workflows | Handshake path/query, authenticated session binding, `MessageType`/`Data` envelopes, subscriptions, heartbeat, reconnect and event payload captures |
| UDP discovery | Optional P0/P1 convenience for LAN clients; manual server URL remains the initial setup path | Official `who is EmbyServer?` request on UDP 7359 and JSON identity response; bind/address behavior on Linux/container networks |
| HTTP media semantics | Mandatory for any advertised playback profile | GET/HEAD, 200/206/304/416 as applicable, byte offsets, lengths, range edges, content types, cache validators, request cancellation |

These behaviors do not appear as a complete set of operations in Swagger. They must be tracked in addition to endpoint counts.

## Deferred and excluded families

| Family | Decision and rationale |
| --- | --- |
| Live TV, EPG, DVR, tuner management | Deferred: substantial scheduling/source-lifecycle domain; do not confuse with local-file transcoding or generic live-stream opening |
| DLNA profiles, discovery and SOAP services | Deferred: separate interoperability stack and Linux networking coverage |
| Sync/offline downloads | Deferred: transfer/job/package semantics exceed simple authorized file download |
| Channels and external content providers | Deferred: provider integration, availability, and security requirements |
| Party/group synchronization | Deferred: synchronized playback protocol and clock/state reconciliation |
| BackupApi | Evaluate for P2 only after confirming upstream plugin/core provenance and supported archive format; build native Goby backup first |
| PluginService | Optional future Goby extension registry; no implied compatibility with Emby binary plugins |
| Notifications, recommendations, instant mixes, BIF previews, themes, intros, game/book media | P2 or deferred according to client demand and domain coverage |
| WebAppService and consumer web player | Excluded: Goby provides only its own administrator dashboard |
| ConnectService and Emby cloud registration | Excluded: use Goby local accounts and configured server URLs |
| PackageService / Emby package installation | Excluded: Goby releases and extensions need their own distribution mechanism |

Unsupported features need an explicit capability decision and appropriate error behavior. Do not implement licensing or cloud endpoints with fabricated success responses. Do not substitute Jellyfin contracts for missing Emby evidence.

## Legacy candidates from the historical export

| Candidate | Newer baseline / decision |
| --- | --- |
| `GET /Users` | Newer `GET /Users/Query`; keep response array/envelope differences separate |
| `GET /Library/VirtualFolders` | Newer `GET /Library/VirtualFolders/Query`; add old form only with a target fixture |
| `GET /System/Logs` and `GET /System/Logs/Log` | Newer `/System/Logs/Query` and `/System/Logs/{Name}` |
| `GET /Search/Hints` | Absent from newer export; use `SearchTerm` on item queries; add an explicit Hints adapter when required |
| Root routes without `/emby` | Not implied by the narrative base URL; examine real client requests before adding aliases |
| XML and alternate authentication/header forms | Official docs describe multiple transports; JSON-first delivery must not claim XML until matching it is implemented and tested |

Maintain a versioned compatibility profile with each alias's evidence and fixtures. Never change a shared handler's response shape simply because a newer endpoint looks similar.

## Authentication and wire-contract guardrails

- Separate public bootstrap, authenticated-user, self-only, authorized-resource, and administrator access. Upstream security annotations are inputs to review, not executable policy.
- Parse the official Emby authorization metadata and token carriers; define conflict handling and redact credentials from logs. Never trust a caller-provided `UserId` as authorization.
- Preserve PascalCase JSON fields and documented enum strings. Resolve absent/null/empty behavior with fixtures before using broad `omitempty` rules.
- Preserve query list encodings, repeated keys, boolean/default semantics, field projection, sort order, pagination, counts, and errors for supported operations.
- Use UTC time representations and `int64` ticks with checked conversion. Playback ticks are 100 ns units; individual APIs may use other explicit units.
- Return accurate `MediaSourceInfo`, `MediaStream`, and `DeviceProfile` results. A matching DTO name without correct values is not sufficient for playback.
- Keep write operations consistent across DELETE and documented POST `/Delete` variants. Enforce idempotency and authorization in the shared service.

See the [full catalog](catalog.md) for all request parameters and source claims, and [delivery gates](../planning/delivery-and-verification.md) for how these obligations become test evidence.
