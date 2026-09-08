# Client Compatibility Research

Research date: 2026-09-09. Scope: Emby-compatible authentication, users, discovery, sessions, browsing, search, images, and user state. This is a research contract and implementation proposal, not a claim that Goby already implements these operations.

Goby will provide an administrator dashboard only. Existing media clients still need the browsing, playback negotiation, streaming, subtitle, and progress APIs. Omitting an Emby-style browser player does not remove those backend requirements.

## Evidence and compatibility baseline

The primary machine-readable source is the official [Emby SDK OpenAPI document at commit `bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json), retained locally as [the SDK snapshot](../sources/emby-sdk-openapi.snapshot.json). The [commit metadata](https://github.com/MediaBrowser/Emby.SDK/commit/bdd0dd7c0801f6e069dff2795d80cddae6f91791) identifies the 2026-05-18 SDK 4.9.5.0 update, and the pinned [version file](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/SampleCode/RestApi/Version.txt) declares `4.9.5.0 Release`. The JSON itself does not declare an API version in `info`; the SDK release association must remain provenance rather than a claim of observed server behavior.

The official [REST reference](https://dev.emby.media/reference/RestAPI.html) was read to cross-check service routes. Narrative guides supply workflow details absent from generated schemas. The official [static Swagger document](https://swagger.emby.media/openapi.json), retained as [the older snapshot](../sources/emby-openapi.snapshot.json), reports version `4.1.1.0`. It is useful for legacy comparison, not as the current baseline. Do not connect to example servers embedded in a downloaded specification.

Evidence labels used below:

- **Documented:** present in the pinned SDK snapshot or the linked official narrative guide. This does not mean tested against a running server.
- **Proposed:** a Goby design or prioritization decision that needs implementation and compatibility verification.
- **Unresolved:** incomplete, conflicting, or version-sensitive evidence that must be settled using an explicitly selected server/client version matrix.

Observed source limitations:

1. The generated security metadata labels `POST /Users/AuthenticateByName`, `GET /Users/Public`, and `GET /System/Info/Public` as requiring user authentication. The narrative login workflow starts by obtaining public users before obtaining a token. Do not generate middleware permissions mechanically from these labels. Pre-login endpoint behavior must be captured separately.
2. Some HTML DTO tables display object references as arrays. For example, the login reference displays `UserDto[]` and `SessionInfo[]`, while the pinned JSON defines `AuthenticationResult.User` and `.SessionInfo` as object references. Use the JSON for these structures.
3. `GET /Shows/{Id}/Episodes` declares a successful response of unknown content in the pinned schema. Its response envelope remains a capture requirement; a presumed envelope must not silently become a contract.
4. `GET /Users/Query` appears in the newer snapshot, while the older snapshot provides `GET /Users`. `GET /Search/Hints` appears only in the older snapshot. Their absence from the newer snapshot is not proof that a live newer server rejects them.
5. The [SDK client guidance](https://dev.emby.media/home/sdk/apiclients/index.html) explicitly presents generated clients as unsupported starting points that may contain issues. The [Go client page](https://dev.emby.media/home/sdk/apiclients/Go/README.html) is an example client, not a server implementation or a recommended production module layout.
6. A C# repository linked by the WebSocket narrative guide, `MediaBrowser/Emby.ApiClient`, has a 2023-12-09 deprecation update; the linked `MediaBrowser.ApiInteraction/ApiWebSocket.cs` file was unavailable when researched. It was not used as evidence for a current WebSocket URL.

## Wire conventions and authentication

The [REST overview](https://dev.emby.media/doc/restapi/index.html) documents the base URL as `http[s]://hostname:port/emby/{apipath}` and documents JSON and XML request/response formats. Routes below are relative to `/emby`. Supporting a root-path alias, lowercase paths, alternative historical prefixes, or arbitrary configured base paths is a **proposed compatibility option**, not established by that overview. Record actual client requests before adding aliases.

The [authentication guide](https://dev.emby.media/doc/restapi/User-Authentication.html) describes client identity on every request:

```http
Authorization: Emby Client="Example Client", Device="Living Room", DeviceId="stable-device-id", Version="1.0.0"
Content-Type: application/json
```

The [login reference](https://dev.emby.media/reference/RestAPI/UserService/postUsersAuthenticatebyname.html) additionally documents `X-Emby-Authorization` as an alternative header name and a `Token` attribute inside the `Emby` header. Its schema also lists the optional-context `UserId` identity attribute. These client/device attributes describe the request; they must never by themselves establish authorization.

Documented login request and response structure:

```http
POST /emby/Users/AuthenticateByName
X-Emby-Authorization: Emby Client="Example Client", Device="Living Room", DeviceId="stable-device-id", Version="1.0.0"
Content-Type: application/json

{"Username":"alice","Pw":"example-password"}
```

```json
{
  "User": { "Id": "opaque-user-id", "Name": "alice" },
  "SessionInfo": { "Id": "opaque-session-id", "DeviceId": "stable-device-id" },
  "AccessToken": "opaque-access-token",
  "ServerId": "stable-server-id"
}
```

The response example shows selected properties, not an exhaustive response fixture. The narrative guide calls the body password field `pw`; the schema names it `Pw`. Request field case tolerance is an explicit verification item. The documented password is plaintext inside the transport, so the **proposed deployment** terminates HTTPS before allowing remote login.

After successful login, the documented token header is `X-Emby-Token`. The [API key guide](https://dev.emby.media/doc/restapi/API-Key-Authentication.html) also documents `api_key` in the query string for static API keys. The WebSocket guide uses `api_key` with a user authentication token. User-token query use on arbitrary REST endpoints is not established by the API key page alone and must be checked in the selected client matrix.

**Proposed authentication implementation:** support the documented identity headers and token carriers in a dedicated compatibility parser; store high-entropy opaque token hashes server-side; associate tokens with server, user, device, creation time, and revocation state; redact tokens and passwords from logs. Resolve any conflicting token carriers deterministically and reject ambiguous identities instead of allowing a caller-supplied `UserId` to override the token subject. Persist a stable server ID independently of the hostname, container ID, or process lifetime.

### Authentication and user route selection

These routes are documented by [UserService](https://dev.emby.media/reference/RestAPI/UserService.html), [SessionsService](https://dev.emby.media/reference/RestAPI/SessionsService.html), and [SystemService](https://dev.emby.media/reference/RestAPI/SystemService.html). Priorities are proposed: **Core** maps to P0; **Next** identifies follow-up work; **Legacy** needs old-client demand and a captured contract. The [implementation scope](../api/implementation-scope.md) is the canonical project schedule.

| Priority | Method and route | Input or result | Important behavior |
|---|---|---|---|
| Core | `GET /System/Info/Public` | `PublicSystemInfo` | Server identity and addresses; pre-login access metadata is unresolved. |
| Core | `GET /Users/Public` | `UserDto[]` | Only users permitted to appear on login screens. An empty array is a valid login scenario. |
| Core | `POST /Users/AuthenticateByName` | `AuthenticateUserByName` to `AuthenticationResult` | Body fields `Username`, `Pw`; success is HTTP 200. |
| Core | `POST /Users/{Id}/Authenticate` | `AuthenticateUser` to `AuthenticationResult` | Body contains `Pw`; supports a selected public user. |
| Core | `POST /Sessions/Logout` | Empty successful response in the schema | Explicit logout revokes the access token according to the narrative guide. |
| Core | `GET /Users/{Id}` | `UserDto` | Subject/administrator access rules must be verified and enforced. |
| Core | `GET /System/Info` | `SystemInfo` | Full server information; preserve the stable ID used to scope saved credentials. |
| Core, admin | `GET /Users/Query` | Filters and pagination to `QueryResult_UserDto` | Newer user enumeration route; documented as administrator authentication. |
| Core, admin | `POST /Users/New` | User creation request | Create local users for third-party clients. |
| Core, admin | `POST /Users/{Id}` | User update request | User name and permitted account edits; not a generic grant of policy-edit permission. |
| Core, admin | `POST /Users/{Id}/Policy` | `UserPolicy` | Library, device, remote access, playback, and administrator permissions. |
| Core | `POST /Users/{Id}/Password` | `UpdateUserPassword` | Schema fields: `Id`, `NewPw`, `ResetPassword`; permission and reset semantics require capture. |
| Core, admin | `DELETE /Users/{Id}`; `POST /Users/{Id}/Delete` | Empty successful response | Both forms are in the newer reference. |
| Next | `POST /Users/ForgotPassword`; `POST /Users/ForgotPassword/Pin` | Recovery request/response DTOs | Define safe local administrator recovery before advertising compatibility. |
| Core, admin | `GET /Auth/Keys`; `POST /Auth/Keys` | Creation requires query `App` | Newer schema does not specify successful response bodies; capture required. |
| Core, admin | `DELETE /Auth/Keys/{Key}`; `POST /Auth/Keys/{Key}/Delete` | API key revocation | Administrator-only in the proposed implementation. |
| Next | `GET /System/Endpoint` | `EndPointInfo` | Endpoint locality influences some clients; proxy/locality behavior requires evidence. |
| Core | `GET`, `POST`, `HEAD /System/Ping` | Empty successful response in the pinned schema | Do not invent an `Emby Server` response string without a fixture. |
| Legacy | `GET /Users` | `UserDto[]` in the old snapshot | Do not replace `/Users/Query` with this old response shape. |

The narrative guide requires authentication even when `HasPassword` is false. A 401 during normal use can indicate token revocation. The [parental-control guide](https://dev.emby.media/doc/restapi/Parental-Control.html) specifies a 401 plus `X-Application-Error-Code: ParentalControl` when parental restrictions deny access. This is a specific exception to any generic assumption that all authorization failures must be 403.

## Library navigation and search

The [browsing guide](https://dev.emby.media/doc/restapi/Browsing-the-Library.html) defines the normal flow as user views, then items scoped by `ParentId`, then item detail. A view's `CollectionType` selects a specialized presentation; a null value denotes a mixed view. `IsFolder`, `Type`, and `MediaType` are distinct properties and must remain distinct in the DTO mapper.

Routes below are documented by [ItemsService](https://dev.emby.media/reference/RestAPI/ItemsService.html), [UserLibraryService](https://dev.emby.media/reference/RestAPI/UserLibraryService.html), [UserViewsService](https://dev.emby.media/reference/RestAPI/UserViewsService.html), and [TvShowsService](https://dev.emby.media/reference/RestAPI/TvShowsService.html).

| Priority | Method and route | Input or result | Compatibility requirement |
|---|---|---|---|
| Core | `GET /Users/{UserId}/Views` | `IncludeExternalContent`; `QueryResult_BaseItemDto` | Return only authorized top-level libraries/views. |
| Core | `GET /Users/{UserId}/Items/Root` | `BaseItemDto` | Stable root object for clients using generic navigation. |
| Core | `GET /Users/{UserId}/Items` | Rich query; `QueryResult_BaseItemDto` | Primary browsing/search route with user-scoped state. |
| Core | `GET /Items` | Same broad query family; query `UserId` | Alternative route used by clients; optional `UserId` is not permission to query any account. |
| Core | `GET /Users/{UserId}/Items/{Id}` | `BaseItemDto` | Detail representation includes more fields than a list item. |
| Core | `GET /Users/{UserId}/Items/Latest` | `ParentId`, `Limit`, types, `IsPlayed`, `GroupItems`, fields | Response is a bare `BaseItemDto[]`, not `QueryResult_BaseItemDto`. |
| Core | `GET /Users/{UserId}/Items/Resume` | Rich item query; `QueryResult_BaseItemDto` | Resume ordering and thresholds require behavioral fixtures. |
| Core | `GET /Shows/NextUp` | Required query `UserId`; optional `SeriesId`, `ParentId`, pagination | Returns `QueryResult_BaseItemDto`; episode eligibility/order are behavior to implement. |
| Core | `GET /Shows/{Id}/Seasons` | Series ID and query filters | TV hierarchy. |
| Core | `GET /Shows/{Id}/Episodes` | `Season`, `SeasonId`, other item filters | The current source leaves the response content unknown. |
| Next | `GET /Shows/Upcoming`; `GET /Shows/Missing` | User and episode filters | Preserve virtual/unplayable semantics if supported. |
| Next | `GET /Items/{Id}/Ancestors`; `GET /Items/{Id}/Similar` | User/item context | Breadcrumbs and related titles. |
| Next | `GET /Users/{UserId}/Items/{Id}/SpecialFeatures`; `GET /Users/{UserId}/Items/{Id}/LocalTrailers`; `GET /Users/{UserId}/Items/{Id}/Intros` | Item context | Details may cause clients to request these automatically. |
| Next | `GET /Genres`; `GET /MusicGenres`; `GET /Artists`; `GET /Artists/AlbumArtists`; `GET /Persons`; `GET /Studios` | Facet-specific query contracts | Implement with music/facet milestones, using the dedicated service contracts. |
| Legacy | `GET /Search/Hints` | Old `SearchHintResult` contract | Present in 4.1.1.0, absent from the pinned newer snapshot; do not make it the primary search API. |

The pinned `QueryResult_BaseItemDto` contains `Items` and `TotalRecordCount`; it does not define a `StartIndex` response field. The request pagination parameters are `StartIndex` and `Limit`. Do not automatically add response fields from another media server's model.

Implement these query dimensions before describing generic browsing as compatible:

| Query dimension | Documented parameters | Implementation note |
|---|---|---|
| Scope | `UserId`, `ParentId`, `Recursive`, `Ids`, `ExcludeItemIds` | Apply access control before filtering, paging, and counting. |
| Search | `SearchTerm`, name-prefix filters | Ranking, Unicode matching, accent handling, and tokenization are not specified by the route schema. |
| Pagination | `StartIndex`, `Limit`, `StartItemId` | Keep a deterministic tie-breaker across pages; negative/oversized limits require captured behavior. |
| Sorting | `SortBy`, `SortOrder` | The guide supports comma-separated fields and corresponding comma-separated orders. |
| Types | `IncludeItemTypes`, `ExcludeItemTypes`, `MediaTypes`, `IsFolder` | Several parameters accept comma-separated values. |
| User state | `Filters`, `IsPlayed`, `IsFavorite`, `EnableUserData` | Examples include `IsResumable`, `IsPlayed`, `IsUnplayed`, and `IsFavorite` filters. |
| Projection | `Fields`, `EnableImages`, `EnableImageTypes`, `ImageTypeLimit` | Preserve the lighter list response and additional requested fields. |
| Metadata | `Genres`, `Tags`, `Studios`, `Artists`, provider and rating filters | Several textual facets use a pipe delimiter, unlike the comma-delimited type/field filters. |

The [latest-items guide](https://dev.emby.media/doc/restapi/Latest-Items.html) documents `GroupItems=true` by default and returning a containing series/group with `ChildCount`. It also lists `StartIndex`, which is absent from the pinned newer latest-items operation. Treat pagination and grouping defaults as version-sensitive fixtures, not a reason to silently widen the schema.

**Proposed storage/query design:** keep internal media entities separate from Emby DTOs; generate user views from allowed libraries; use stable opaque public IDs; store the media hierarchy and indexed query attributes in the database; keep user state keyed by user and item; centralize DTO projection and authorization so `/Items` and `/Users/{UserId}/Items` do not drift. Search must return only authorized items, including counts and facets.

## Images and cache behavior

[ImageService](https://dev.emby.media/reference/RestAPI/ImageService.html) documents the following client read surface:

| Priority | Method and route | Purpose |
|---|---|---|
| Core | `GET`, `HEAD /Items/{Id}/Images/{Type}` | Primary art and other single images. |
| Core | `GET`, `HEAD /Items/{Id}/Images/{Type}/{Index}` | Indexed backdrops, chapters, screenshots, and other image types. |
| Core | `GET`, `HEAD /Users/{Id}/Images/{Type}` | Login/user profile image. |
| Core | `GET`, `HEAD /Users/{Id}/Images/{Type}/{Index}` | Indexed user-image route. |
| Core, admin | `GET /Items/{Id}/Images` | Metadata about available images. |
| Next | `GET`, `HEAD /Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}` | Compact image URL with encoded transform arguments. Preserve the documented path-variable spelling. |
| Next | `GET`, `HEAD /{Facet}/{Name}/Images/{Type}` and indexed variant | `{Facet}` expands to `Artists`, `Genres`, `MusicGenres`, `Persons`, `Studios`, or `GameGenres`. These are documented separate routes, not a free-form router wildcard. |

The [image guide](https://dev.emby.media/doc/restapi/Images.html) documents tags that indicate availability, a 404 for unavailable images, explicit or maximum dimensions, indexed image types, and parent-image inheritance. Supplying a matching `Tag` enables strong URL-based caching; without it the guide describes conditional caching. An image update changes the tag. Exact cache headers, validators, and 304 behavior still require captured responses.

The current schema documents `Format` values `original,gif,jpg,png`, `Quality` from 0 to 100 with a documented default of 90, and `MaxWidth`, `MaxHeight`, `Width`, `Height`, `CropWhitespace`, `EnableImageEnhancers`, `BackgroundColor`, `ForegroundLayer`, `AutoOrient`, and `KeepAnimation`. The narrative page contains a `jpp` typo and mentions additional overlays; do not turn that typo into a supported encoder format. Overlay parameters and the compact path need separate implementation coverage.

Important DTO fields are `ImageTags`, `BackdropImageTags`, `PrimaryImageAspectRatio`, and the parent image ID/tag pairs. Return tags only when the corresponding image is actually available. A null image reference, missing image reference, and empty dictionary should not be normalized without client evidence.

**Proposed Linux implementation:** serve original files and cached variants through a bounded image pipeline; key variants by source content/tag plus transform parameters; cap dimensions, decoded pixels, and concurrency; restrict file reads to managed image storage. Keep asset generation separate from request parsing. Image uploads and administrative edits belong to the administrator API plan, but their changes must invalidate the compatible tags.

## User state and preferences

[PlaystateService](https://dev.emby.media/reference/RestAPI/PlaystateService.html) and [UserLibraryService](https://dev.emby.media/reference/RestAPI/UserLibraryService.html) document both mutation routes and legacy progress forms.

| Priority | Method and route | Contract |
|---|---|---|
| Core | `POST /Users/{UserId}/FavoriteItems/{Id}` | Mark favorite; returns `UserItemDataDto`. |
| Core | `DELETE /Users/{UserId}/FavoriteItems/{Id}`; `POST /Users/{UserId}/FavoriteItems/{Id}/Delete` | Unmark favorite; preserve aliases where supported by the newer reference. |
| Core | `POST /Users/{UserId}/PlayedItems/{Id}` | Mark played; optional `DatePlayed` query uses `yyyyMMddHHmmss`; returns `UserItemDataDto`. |
| Core | `DELETE /Users/{UserId}/PlayedItems/{Id}`; `POST /Users/{UserId}/PlayedItems/{Id}/Delete` | Mark unplayed. |
| Core | `POST /Sessions/Playing` | `PlaybackStartInfo`; schema success is HTTP 200 with an empty response. |
| Core | `POST /Sessions/Playing/Progress` | `PlaybackProgressInfo`; schema success is HTTP 200 with an empty response. |
| Core | `POST /Sessions/Playing/Stopped` | `PlaybackStopInfo`; schema success is HTTP 200 with an empty response. |
| Next | `POST /Users/{UserId}/Items/{Id}/HideFromResume` | Required boolean query `Hide`; returns `UserItemDataDto`. |
| Next | `POST /Users/{UserId}/Items/{ItemId}/UserData` | `UserItemDataDto` body; successful response is empty. Partial-update semantics are not documented. |
| Next | `POST /Users/{UserId}/Items/{Id}/Rating` | Required boolean query `Likes`; returns `UserItemDataDto`. Do not assume this operation accepts a numeric star rating. |
| Next | `DELETE /Users/{UserId}/Items/{Id}/Rating`; `POST /Users/{UserId}/Items/{Id}/Rating/Delete` | Clear personal rating. |
| Core | `POST /Sessions/Playing/Ping` | Playback-session liveness; exact cleanup behavior requires fixtures. |
| Legacy/Next | `POST /Users/{UserId}/PlayingItems/{Id}`; `POST /Users/{UserId}/PlayingItems/{Id}/Progress`; `DELETE /Users/{UserId}/PlayingItems/{Id}`; `POST /Users/{UserId}/PlayingItems/{Id}/Delete` | These alternate progress routes remain in the newer reference. Do not mix their request shape with session progress DTOs. |

`UserItemDataDto` defines `PlaybackPositionTicks` as int64, `PlayCount`, `IsFavorite`, `Played`, `LastPlayedDate`, `PlayedPercentage`, `UnplayedItemCount`, `Rating`, `Key`, and `ItemId`. The schema also includes `ServerId` with a note that the Emby server itself does not use it. Goby should preserve numeric widths, distinguish absent fields from zero/false when update semantics require it, and avoid exposing a Go `time.Duration` directly as the wire value.

The [check-in guide](https://dev.emby.media/doc/restapi/Playback-Check-ins.html) specifies periodic progress every 10 seconds plus immediate updates after player interaction, and says the server estimates progress between reports. It describes `ReportPlaybackProgress` as the equivalent WebSocket message. The newer DTOs distinguish start/progress from stop fields; use their actual shapes instead of the guide's simplified claim that stop has identical contents. Completion thresholds, resume cutoffs, event reordering, duplicate stop handling, and play-count increments remain unresolved server behavior.

Relevant progress identifiers are `ItemId`, `MediaSourceId`, `PlaySessionId`, `LiveStreamId`, `SessionId`, and optional `PlaylistItemId`; they identify different things. A progress update can include `PositionTicks`, selected tracks, pause/mute state, queue information, `PlayMethod`, and `EventName`. Do not equate the authenticated session ID with the per-playback `PlaySessionId`.

**Proposed implementation:** make progress updates transactionally update persistent user state and session state; prevent an expired or unrelated playback session from overwriting a newer session; compute played/resume status using documented configuration plus captured reference behavior; publish `UserDataChanged` only after persistence succeeds. Use monotonic server receipt time for session liveness, without trusting it as a replacement for the reported media position.

Preferences are client requirements even when Goby has no media browser UI. [DisplayPreferencesService](https://dev.emby.media/reference/RestAPI/DisplayPreferencesService.html) and UserService document:

- `POST /Users/{Id}/Configuration` with `UserConfiguration`, and `/Configuration/Partial` with a body whose actual partial semantics require capture.
- `GET /DisplayPreferences/{Id}?UserId=...&Client=...` and `POST /DisplayPreferences/{DisplayPreferencesId}?UserId=...` with `DisplayPreferences`.
- `GET /UserSettings/{UserId}`, `POST /UserSettings/{UserId}`, and `POST /UserSettings/{UserId}/Partial`.
- `GET` and `POST /Users/{UserId}/TypedSettings/{Key}` for typed settings.

The pinned user-settings schema describes GET as a string dictionary but POST as an array of strings; this asymmetry must be resolved before implementing writes. A compatibility layer may preserve opaque settings it does not itself interpret, but must still authorize the account and cap stored size.

## Discovery, sessions, capabilities, and WebSocket

The [discovery guide](https://dev.emby.media/doc/restapi/Locating-the-Server.html) documents a UDP broadcast to port `7359` containing `who is EmbyServer?`, with a JSON response containing `Address`, `Id`, and `Name`. This is not an HTTP route. A proposed Linux implementation binds the configured LAN interface, advertises a reachable configured address, and provides a deployment option to disable discovery. Container network mode and broadcast reachability are deployment test cases.

The [Emby Connect guide](https://dev.emby.media/doc/restapi/Emby-Connect.html) describes a separate optional hosted service and recommends manual address entry first. Goby's initial plan should support manual server address entry and local discovery. Compatibility with Emby Connect, hosted account exchange, device activation, or commercial client licensing is not established by a local REST implementation and must not be advertised as part of the initial milestone.

Documented session operations from [SessionsService](https://dev.emby.media/reference/RestAPI/SessionsService.html):

| Priority | Method and route | Key contract |
|---|---|---|
| Core | `GET /Sessions` | Filters: `ControllableByUserId`, `DeviceId`, `Id`; response: `SessionInfo[]`. |
| Core | `POST /Sessions/Capabilities` | Required query `Id`; comma-separated `PlayableMediaTypes`, `SupportedCommands`; booleans `SupportsMediaControl`, `SupportsSync`. |
| Core | `POST /Sessions/Capabilities/Full` | Required query `Id`; `ClientCapabilities` body. |
| Next | `GET /Sessions/PlayQueue` | Retrieve a session queue; capture success shape before claiming support. |
| Next | `POST /Sessions/{Id}/Viewing` | Client viewing/browsing command. |
| Next | `POST /Sessions/{Id}/Playing` | Remote play request. |
| Next | `POST /Sessions/{Id}/Playing/{Command}` | Remote stop/pause/seek/queue control. |
| Next | `POST /Sessions/{Id}/Command`; `POST /Sessions/{Id}/Command/{Command}` | General remote commands. |
| Next | `POST /Sessions/{Id}/Message`; `POST /Sessions/{Id}/System/{Command}` | Message/system control; authorize the controlling account. |
| Next | `POST /Sessions/{Id}/Users/{UserId}`; `DELETE /Sessions/{Id}/Users/{UserId}`; `POST /Sessions/{Id}/Users/{UserId}/Delete` | Additional session users; isolate their permissions and state deliberately. |

`ClientCapabilities` includes `PlayableMediaTypes`, `SupportedCommands`, `SupportsMediaControl`, `SupportsSync`, `DeviceProfile`, `PushToken`, `PushTokenType`, `IconUrl`, and `AppId`. `DeviceProfile` contains direct-play/transcoding/container/codec/subtitle/response profiles and bitrate information. Client capabilities inform playback decisions; they must not grant permissions or cause the server to claim an encoder or feature it cannot supply.

`SessionInfo` includes `Id`, `UserId`, `DeviceId`, `Client`, `ApplicationVersion`, `LastActivityDate`, `NowPlayingItem`, `PlayState`, `SupportedCommands`, `SupportsRemoteControl`, and optional transcoding information. These are necessary for the administrator's active-session page even without an administrator-side player.

The [WebSocket guide](https://dev.emby.media/doc/restapi/Web-Socket.html) says to derive `ws:`/`wss:` from the server HTTP address and append `api_key` and `deviceId`. It does not establish a universal `/socket` or `/embywebsocket` path. The exact path, proxy-base-path handling, handshake status, heartbeat, and reconnect behavior require selected-client captures.

Documented message envelopes have `MessageType` and `Data`. `Data` may be text or an object depending on the message. Core event candidates are `UserDataChanged` with `UserId` and `UserDataList`, `UserUpdated`, and `UserDeleted`; administrator lifecycle notifications include `RestartRequired`, `ServerRestarting`, and `ServerShuttingDown`. Remote-control messages include `GeneralCommand`, `Play`, and `Playstate`. Follow the exact event-specific shape rather than forcing all `Data` values into one DTO.

The [remote-control guide](https://dev.emby.media/doc/restapi/Remote-Control.html) documents `PlayNow`, `PlayNext`, `PlayLast`, selected stream indices, start positions, and general-command arguments. It states that `StartPositionTicks` is ignored for `PlayNext` and `PlayLast`. Commands must be filtered by user permission and client support. The guide and WebSocket page differ on names such as `ToggleOsdMenu` versus `ToggleOsd`; preserve observed variants only after client evidence.

## Proposed compatibility verification plan

No tests, runtime probes, builds, or compatibility sessions were executed in this research. Only public documentation/source retrieval, static reading, and document authoring were performed. Future verification must run through `ssh test-env`; if that environment is unavailable, verification is blocked until it becomes available or the user explicitly authorizes local verification.

1. Pin an Emby reference server release and a small named client/version matrix. Record whether each client is a third-party client, official app, platform-specific app, or an SDK sample. Record manual-address and local-discovery results separately. Do not infer compatibility with untested clients from matching endpoint names.
2. Build an isolated Linux fixture library with a movie, a series with two seasons and specials, multiple media sources, an audio album, missing artwork, indexed backdrops, long/non-ASCII names, and two users with disjoint library access. Use media owned or licensed for testing.
3. Capture sanitized requests and responses for first connection, public users, password and passwordless login, token reuse, logout/revocation, selected-user login, administrator user edits, and wrong-user resource requests. Capture token-header variants independently.
4. Compare DTO field names, objects versus arrays, missing/null/empty behavior, int64 values, dates, `Content-Type`, response bodies, and status codes. Keep authorization expectations in a reviewed overlay because the source metadata is contradictory.
5. Exercise views, root, details, paging, multi-field sorting, empty search, Unicode search, recursive filters, projection fields, latest grouping, resume, next-up, and hidden/private content. Compare total counts after authorization filters.
6. Capture GET/HEAD image headers, unavailable images, resize/format options, tags before/after image changes, conditional requests, and inherited images. Verify request size bounds as implementation security checks, not presumed Emby behavior.
7. Exercise start, pause, seek backwards, track selection, progress, stop, duplicate stop, network interruption, concurrent devices, manual played/unplayed, favorite, hide-from-resume, and next-up updates. Verify both stored user state and administrator session display.
8. Capture WebSocket handshake URLs and event payloads through direct and reverse-proxy connections. Test disconnect/reconnect and expired tokens. Verify that one user never receives another user's private state events.
9. Maintain a route/behavior matrix with `documented`, `implemented`, `contract-tested`, `client-tested`, and `unsupported` states. A working `/Items` handler alone is not a passed search/browse contract.
10. Resolve source gaps before promoting affected endpoints: public-route authentication, `/Shows/{Id}/Episodes` response, partial settings updates, optional API aliases, old search hints, exact WebSocket URL, and actual API-key result shapes.

The initial acceptance target should be a named subset: direct/manual connection, login, authorized library browsing, detail/artwork, playback handoff, persisted progress, favorites, and correct administrator session visibility. Playback delivery and Linux transcoding are separate implementation workstreams that must pass alongside this client contract.
