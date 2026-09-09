# Implemented API surface: catalog, metadata, artwork, and original playback

This file tracks implementation separately from the immutable upstream research inventory. The [full catalog](catalog.md) contains upstream contracts and initial scope labels; its generated `planned-unimplemented` field records the research baseline, not the current implementation tracker.

The routes below exist in source. Authentication, permission and ingestion workflows have passed Goby's own PostgreSQL-backed HTTP tests on Linux. Selected behavior has also been corrected using [real Emby 4.9.5.0 captures](../research/reference-server.md). This is not yet a complete differential compatibility run or a real third-party-player pass.

## Administrator API

All names are Goby-owned. JSON bodies and responses use the field names shown here.

| Method and route | Request | Response / access |
| --- | --- | --- |
| `GET /admin/v1/bootstrap` | None | `{Initialized: boolean}`; minimal anonymous setup status |
| `POST /admin/v1/bootstrap` | `{SetupToken, Name, Password}` | `201 {User}`; one-time deployment secret, atomic first administrator creation |
| `POST /admin/v1/session` | `{Name, Password}` | `{User, CSRFToken}` and opaque HttpOnly cookie; administrator credentials required |
| `GET /admin/v1/session` | Session cookie | `{User, CSRFToken}` |
| `DELETE /admin/v1/session` | Cookie and `X-CSRF-Token` | `204`; revoke session and clear cookie |
| `GET /admin/v1/overview` | Administrator cookie | Server identity, database status, real account/session counts, current feature flags |
| `GET /admin/v1/capabilities` | Administrator cookie | Implementation flags and pinned toolchain targets; unavailable media/hardware features report false |
| `GET /admin/v1/users` | Administrator cookie | `{Items: User[], TotalRecordCount}` |
| `POST /admin/v1/users` | Cookie, CSRF header, `{Name, Password, IsAdministrator}` | `201 {User}` |
| `GET /admin/v1/libraries` | Administrator cookie | `{Items: Library[], TotalRecordCount}` |
| `POST /admin/v1/libraries` | Cookie, CSRF, `{Name, CollectionType, Paths, Scan}` | `201 {Library, Job?}`; optional `ScanError` if catalog creation succeeded but initial scan admission failed |
| `DELETE /admin/v1/libraries/{id}` | Cookie and CSRF | `204`; remove catalog records, retain every media file; active scans prevent removal |
| `POST /admin/v1/libraries/{id}/scan` | Cookie and CSRF | `202 {Job}`; durable scan admission with per-library deduplication |
| `GET /admin/v1/jobs` | Administrator cookie | Recent scan jobs, real progress and outcomes |
| `POST /admin/v1/jobs/{id}/cancel` | Cookie and CSRF | `202 {Job}`; cancel remaining scan work, retain already indexed items |
| `GET /admin/v1/storage/roots` | Administrator cookie | `{Items: [{Path, Available}], Configured}`; `Available` includes directory read permission |

The native user shape is `{Id, Name, IsAdministrator, IsDisabled, HasPassword, CreatedAt}`. Errors have `{Error: {Code, Message}, RequestId}`. `401` signals invalid/missing/revoked authentication; `403` signals rejected origin, setup token, or CSRF. Mutations require JSON and CSRF protection once authenticated. The cookie is scoped to `/admin`, uses `HttpOnly` and `SameSite=Strict`, and is secure by default. CSRF values remain in browser memory and can be recovered with the authenticated session endpoint.

Library/item counts reflect persisted libraries and non-folder media records. Active sessions in this increment are active authentication sessions, not a count of playing media clients.

Library records contain `Id`, `Name`, `CollectionType`, `Paths`, `CreatedAt`, and nullable `LastScanAt`. Scan jobs contain `Id`, `LibraryId`, `Status`, `Error`, `Scanned`, `Added`, `Updated`, `CreatedAt`, and nullable start/finish timestamps. Native statuses are `pending`, `running`, `completed`, `failed`, `cancelled`, and `interrupted`. A completed scan can have a warning describing individual media entries that could not be inspected; existing records are retained in that case. Job history is currently capped at the most recent 1,000 entries.

The administrator UI refreshes active work at bounded intervals, pauses polling while hidden, stops after terminal state, and allows explicit refresh after errors. A lost create response has an unknown outcome: the UI asks the administrator to check the list before attempting another creation.

## Initial Emby API adapter

| Method and path | Current behavior / limits |
| --- | --- |
| `GET /emby/System/Info/Public` | Stable server identity and setup state; Goby product/version is reported truthfully |
| `GET /emby/System/Info` | Requires an Emby user session; currently the minimal public identity projection, not the complete upstream SystemInfo DTO |
| `GET`, `HEAD`, `POST /emby/System/Ping` | Confirmed reference behavior: `text/plain`, length 11, GET/POST body `Emby Server`, HEAD body empty |
| `GET /emby/Users/Public` | Enabled ordinary accounts; administrators are hidden from this initial public list |
| `POST /emby/Users/AuthenticateByName` | `{Username, Pw}` and Emby client/device metadata; returns `User`, `SessionInfo`, `AccessToken`, `ServerId` |
| `POST /emby/Users/{Id}/Authenticate` | `{Pw}` and client/device metadata; selected-user authentication |
| `GET /emby/Users/{Id}` | Current account or administrator; a different ordinary user's account is denied |
| `GET /emby/Users/Query` | Administrator only; supports `StartIndex`/`Limit` and query-result envelope; other upstream filters remain to be implemented |
| `POST /emby/Sessions/Logout` | Revokes the caller's token and disconnects that authentication session's WebSockets |
| `GET /emby/Sessions` | Bare array; ordinary users see their own active client sessions, administrators see enabled users; Id/DeviceId/presence filtering; ControllableByUserId selects declared, connected sessions under current ownership rules |
| `POST /emby/Sessions/Capabilities`, `.../Capabilities/Full` | Validated query/JSON declarations replace the authenticated session's capabilities; stale Id hints cannot target another session; 204 |
| `GET /embywebsocket` with RFC 6455 Upgrade | Authenticated UserDataChanged and command events; aliases `/`, `/emby`, `/emby/`, `/emby/socket`; [event guide](../development/websocket-events.md) |
| `POST /emby/Sessions/{Id}/Playing` | Validated PlayRequest to an owned session or an administrator-authorized target; both users' item access and target playback permission checked |
| `POST /emby/Sessions/{Id}/Playing/{Command}` | Playstate command; trusted controller/target IDs; 204 is acceptance, not a player acknowledgement |
| `POST /emby/Sessions/{Id}/Command`, `.../Command/{Command}` | GeneralCommand; full body preserves string arguments, named route has empty arguments as captured; current authorization rechecked before output |
| `GET`, `POST /emby/Items/{Id}/PlaybackInfo` | Authorized source facts and original-file negotiation; supported DeviceProfile constraints and explicit limits; no transcoding |
| `GET`, `HEAD /emby/Videos/{Id}/stream`, `stream.{Container}`, `original.{Container}` | Original video bytes with standard ranges and conditional requests; current token, playback policy, library access, and source snapshot required |
| `GET`, `HEAD /emby/Audio/{Id}/stream`, `stream.{Container}`, `original.{Container}` | Equivalent original audio delivery |
| `GET`, `HEAD /emby/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` and `/Items/...` | Indexed external SRT/WebVTT with token/playback/library authorization, exact source validation, conversion and conditional responses |
| The same subtitle routes with `/{StartPositionTicks}/Stream.{Format}` | Reference-aligned cue-start filtering and time offsets; an explicit query start takes precedence; [subtitle guide](../development/external-subtitles.md) |
| `POST /emby/Sessions/Playing`, `.../Progress`, `.../Stopped` | Bound playback-session reports, durable user state, idempotent terminal events; 204 empty success |
| `POST /emby/Sessions/Playing/Ping` | Refresh an owned active playback session; known/unknown nonempty keys return 204, missing key returns the reference 400 text error |
| `GET /emby/Users/{UserId}/Items/Resume` | User-specific positions, current library ACL, initial duration/percentage thresholds and deterministic ordering |
| `POST`, `DELETE /emby/Users/{UserId}/PlayedItems/{Id}`, `.../FavoriteItems/{Id}`; `POST .../{Id}/Delete` | Per-user state; recursive folder watched changes and derived unplayed counts; flag-response DTO without ItemId/Key |
| `GET /emby/Users/{UserId}/Views` | Authorized library roots with collection types |
| `GET /emby/Users/{UserId}/Items/Root` | Stable virtual navigation root |
| `GET /emby/Users/{UserId}/Items` and `GET /emby/Items` | ACL-filtered browsing/search, recursive parents, IDs/types/media-type filters, paging and selected sorts |
| `GET /emby/Users/{UserId}/Items/{Id}` | Authorized item detail and probe metadata, including the item's path; also resolves visible catalog entities by positive decimal ID |
| `GET /emby/Users/{UserId}/Items/Latest` | Bare array; default grouping maps episodes to series and audio to albums before paging |
| `GET /emby/Shows/NextUp` | SeriesId selects the unplayed sequence after the watched cursor; global mode selects one continuation per series under a documented Goby policy; [evidence boundary](../development/next-up.md) |
| `GET /emby/Shows/{Id}/Seasons` | Series seasons in numeric order |
| `GET /emby/Shows/{Id}/Episodes` | Episodes with `Season`/`SeasonId` filtering and season/episode ordering; the query-result envelope is confirmed by the reference capture |
| `GET /emby/Genres`, `/emby/Tags`, `/emby/Studios`, `/emby/Persons` | ACL-filtered entity lists with search, selected source-item filters, paging, and pre-pagination totals; Tags returns `{Name, Id}` entries with string IDs |
| `GET /emby/Genres/{Name}`, `/emby/Studios/{Name}`, `/emby/Persons/{Name}` | A single visible entity DTO; no list envelope and no fabricated entity user state |
| `GET /emby/Items/{Id}/Images` | Authenticated, library-authorized image metadata array; actual dimensions and size, with authorized local paths |
| `GET`, `HEAD /emby/Items/{Id}/Images/{Type}` and `.../{Type}/{Index}` | Public indexed local artwork, bounded resize/conversion, conditional ETags, and source-validated caching |
| `GET /emby/Library/VirtualFolders/Query` | Administrator library query projection; broader query/options remain incomplete |
| `POST /emby/Library/VirtualFolders` | Initial administrator library creation and optional refresh; observed `204` success; full LibraryOptions support is pending |
| `POST /emby/Library/VirtualFolders/Delete` | Administrator catalog removal, preserving media |
| `POST /emby/Library/Refresh` | Administrator scan request for configured libraries; observed `204` success |

The parser accepts `Emby` and legacy `MediaBrowser` authorization schemes, the `X-Emby-Authorization` alternative, four separate `X-Emby-*` client/device headers, `X-Emby-Token`, legacy `X-MediaBrowser-Token`, and query `api_key` for issued user tokens. Conflicting token or identity values are rejected. Caller-provided user/role claims never establish authority. Static application API keys are a separate future implementation and are not created by these routes.

Confirmed authentication failures use the reference `text/plain` bodies and byte lengths: wrong/unknown credentials, missing client/device metadata, and missing/invalid access tokens. Native administrator errors remain JSON. Other Emby error cases are not yet claimed to match the reference.

The `/emby` namespace supports token-client CORS, including the observed OPTIONS response, origin reflection for valid HTTP(S)/opaque-null origins, credentials/preflight headers and private-network access headers. This does not expose `/admin/v1` through CORS or make administrator cookies authenticate Emby requests.

Administrator-cookie sessions and Emby-token sessions cannot be substituted for one another. Token secrets are stored as SHA-256 digests, sessions are revocable and expire, and current account disable/demotion state is checked when resolving a token. Emby user tokens currently have a 30-day lifetime; this is a Goby policy, not a proven Emby lifetime match.

The Emby user projection reflects current `EnableMediaPlayback` and library-access policy; explicit playback denial also applies to administrators. Transcoding, remuxing, and deletion capabilities remain false. Unsupported Emby paths return an error. Full policy/configuration projections, query filters, device management, user edits, API keys, and the complete media API remain scheduled work. Error DTO/status details and version negotiation also need reference-server/client evidence.

Item fields currently include identity, hierarchy, type, creation time and selected `Overview`, `MediaStreams`, `MediaSources`, `Path`, and `Chapters` projections. Default list results omit paths and probe structures; an explicit field selection or authorized item detail includes them as observed in the reference. Authorization is applied before any projection. `EnableImages=false` removes image fields, and `EnableUserData=false` suppresses user data when present. Source stream indices and probe sizes/ticks are preserved. Current probe snapshots can advertise original delivery when user policy permits; PlaybackInfo reopens and verifies the source before negotiation. Wire source IDs use `mediasource_{ItemId}`, and wire containers use canonical names such as `mp4` instead of an arbitrary first ffprobe alias. Indexed artwork populates image tags, with `ImageTypeLimit` and `EnableImageTypes` selection.

The currently recognized Emby resources also accept root aliases and case variants of route literals. Dynamic IDs and escaped entity names are preserved. Media routes accept HEAD/Range/If-Range through the same authentication and CORS boundary. See [original playback and user state](../development/direct-playback.md) for negotiation, authorization, state transitions, resource limits, and the required rescan after upgrading probe data. [Client sessions](../development/client-sessions.md) expose validated capabilities and the latest authorized Playing/Paused record per authentication session, with paths and UserData omitted from NowPlayingItem. [External text subtitles](../development/external-subtitles.md) have stable indexed tracks and credential-bearing delivery URLs in item/source projections. [WebSocket events](../development/websocket-events.md) deliver committed user-state notifications and initial remote commands. Embedded subtitle extraction, additional formats/events, and consumer-client acceptance remain open.

Local NFO data contributes `ProductionYear`, `PremiereDate`, `OriginalTitle`, `CommunityRating`, and `OfficialRating` when present, for detail requests or when selected with a same-named `Fields` value. These scalars are absent from the default item list, as confirmed by the NFO reference captures. Missing scalar values are omitted, while an explicit zero rating remains zero.

`Fields=ProviderIds,Genres,Tags,Studios,People` includes provider identifiers, `Genres` strings together with `GenreItems`, `TagItems`, `Studios`, and `People`. In the pinned reference, `Fields=Tags` selects `TagItems` objects rather than a `Tags` string array. Detail requests include these collections as well. Requested missing collections are empty arrays and missing provider identifiers are an empty object.

Studios, genre items, and tag items use objects with `Name` and persistent numeric IDs; people use persistent string IDs, `Name`, `Type`, and optional `Role`. `TagItems` is sorted by name using a lowercase comparison and an original-string tie break; the reference sample confirms name ordering for its two ASCII tags. NFO credit order controls stable array order rather than adding an unsupported `SortOrder` response field. Entity lists/details use string IDs, including the minimal tag-list entries. Entities without visible item associations remain inaccessible, while their stored identity can be reused after a later rescan. Generated entity images and entity-specific user state remain pending. The scanner applies names, overview, and numbering; the DTO does not reapply a conflicting NFO season. The raw sidecar and its internal source path/hash are not exposed. See [local metadata](../development/local-metadata.md) for supported files, values, migration backfill, and failure behavior.

Indexed item image contents are public, as observed in the pinned reference, while image enumeration requires a valid token and library access. This policy does not make catalog metadata, user avatars, media streams, or mutations anonymous. The bitmap service validates the indexed file before cache hits and conditional responses; changed sources require a rescan. See [local artwork](../development/local-artwork.md) for naming, supported query options, resource limits, and deliberate differences from reference error/size behavior.

Library authorization currently implements administrator access plus `EnableAllFolders` and `EnabledFolders`. Counts and grouping occur after authorization filtering. Default ordinary users can access all libraries unless restricted. Subfolder exclusions, parental restrictions and the full policy editor remain work; this increment does not claim those policies are enforced.

The query adapter supports `ParentId`, `Recursive`, `SearchTerm`, `StartIndex`, `Limit`, `Ids`, `IncludeItemTypes`, `MediaTypes`, and a limited single-key `SortBy` set. User-state filters include `IsPlayed`, `IsFavorite`, and the documented initial `Filters` subset in the playback guide. Entity filters include `Genres`, `Tags`, `Studios`, `Person`, `GenreIds`, `TagIds`, `StudioIds`, `PersonIds`, and `PersonTypes`. Name lists use a pipe delimiter; IDs accept pipes or commas. Same-dimension values use OR, separate dimensions use AND, and person/type conditions match the same credit association. Other upstream filters, default subtleties, composite sort expressions and the complete field model remain compatibility work.

## Delivery and health

`/admin/` serves the built React/MUI application, with route fallback for UI navigation. `/admin/v1` always goes through the API router, including unknown routes. `/healthz` exposes minimal liveness; `/readyz` checks PostgreSQL and the catalog ownership session. The service runs as an unprivileged Linux user in the test deployment.

There is no consumer web player by design. Original-file and external SRT/WebVTT endpoints are implemented; broader subtitle handling, the HLS job controller and hardware playback remain open in the [active delivery plan](../planning/delivery-and-verification.md). Richer catalog/media types, generated or embedded artwork, and the complete query/policy surface remain work.

The [M4a engine](../development/transcode-engine.md) now implements internal conversion planning, PostgreSQL job records, FFmpeg execution and cache supervision, with verified software media output. Its client-facing HLS HTTP graph, full-duration seek scheduling and policy/configuration integration remain open. Internal engine tests do not change the currently advertised playback capabilities or establish third-party-client conversion support.
