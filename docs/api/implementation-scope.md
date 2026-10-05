# Emby API implementation scope

Status: **current scope and historical contract inventory, updated October 5,
2026**. The selected compatibility, media-analysis, revised functional-recovery,
and Linux amd64 software/AMD OCI deliveries are complete within their recorded
boundaries. See the [implemented surface](implemented.md),
[current status](../development/current-status.md), and
[current execution plan](../planning/current-execution-plan.md). The route
selection tables below describe contracts and original implementation order;
they are not a list of unfinished tasks. Actual client evidence is scoped to
the recorded versions, adapters and journeys, not full Emby compatibility.

The September 20, 2026 user decision selected the account/playback,
subtitle/artwork, music/search and management/client-protocol work in the
[four-phase execution plan](../planning/selected-compatibility-plan-20260920.md).
All four phases are closed within their recorded acceptance boundaries. Phase 1
uses the user's compatibility-adapter boundary: original Web
commercial licensing restrictions are retained as client limitations and do not
block third-party-client delivery. Actual results remain scoped to tested
contracts and journeys. Live TV, EPG, DVR/scheduled recording, tuners,
DLNA, external channels and group playback are explicitly excluded, replacing
their earlier deferred disposition. Other unselected work remains deferred.

The later [three-phase increment](../planning/media-analysis-resilience-plan-20260920.md)
completed selected API/configuration gaps, automatic episode-intro analysis,
BIF previews and the revised concurrency/recovery scope. The
[software OCI delivery](../development/oci-delivery-20260929.md) and
[AMD OCI extension](../development/oci-amd-delivery-20260929.md) subsequently
completed their Linux amd64 archive/Compose profiles. Earlier blanket deferrals
do not apply to those accepted scopes. Registry publication, production
deployment and additional Docker image profiles remain separate.

The objective is an independent Linux media backend that existing
Emby-compatible clients can connect to. The October 4, 2026 user decision also
selects Goby's own consumer player, superseding the previous blanket exclusion.
The [React + Vite player](../../web/player/README.md) builds and deploys
independently and consumes existing browsing and playback APIs. It is not
embedded in the Go executable; the React/MUI administrator dashboard retains
its current packaging. Emby's proprietary web application and WebAppService
remain excluded. The [backend capability assessment](../../web/player/BACKEND-CAPABILITIES.md)
records design features without corresponding backend behavior. Goby's project
license remains undecided; completed internal deliveries do not establish
complete public-distribution licensing.

The selected player follow-up includes the Goby-specific
[bitmap subtitle timeline API](subtitle-timelines.md). It extracts real display
intervals from embedded PGS/DVD tracks into permanent source-side material,
using a default-off library option and the existing task system. Consumer reads
never generate data, and the player omits labels and lanes without valid current
intervals. External SUP and IDX+SUB files remain outside this implementation.
This extension does not imply an original Emby route or change the existing
subtitle playback/burn-in contract. Focused verification and integrated
acceptance have separate scopes in the
[player record](../../web/player/ACCEPTANCE.md).

The full upstream inventory is [535 operations](catalog.md), with [local request/response models](models.md). That count describes the fixed SDK export, not the complete behavior of every Emby release. The newer baseline is **SDK 4.9.5.0 Release**; see [source provenance](../sources/README.md).

## Official delivery form

The [September 30 Docker delivery policy](../planning/docker-delivery-policy.md)
selects a Goby image running on Docker Engine as the only official delivery
form. The current delivery uses an importable image archive and Docker Compose.
Software and AMD are profiles of that one form, currently accepted on Linux
amd64 within their recorded boundaries.

Standalone binaries, systemd installation packages, DEB/RPM packages, Windows
installers and other native packages are unsupported, not deferred obligations.
Native builds remain development tools and historical fixtures. Non-Docker
runtimes are not promised. Any future arm64 work must be selected as a Docker
image profile; current platform support does not expand through this policy.

The CLI, FFmpeg/ffprobe and native analysis helper included in an image are image
components, not separate delivery forms. Registry publication is an optional
distribution channel for the same images and has not occurred. Deployment of
the external PostgreSQL server or reverse proxy is not prescribed by this policy.

## What compatibility means

The target is a shared Emby-compatible backend that general-purpose clients can connect to, authenticate with, browse, and use to play supported media without modifying the client. An implemented route or a successful JSON response is insufficient: authentication and device/token conventions, DTO shapes and values, playback negotiation, actual HTTP media and subtitle delivery, and playback reports/resume behavior must work together.

Official documentation and pinned SDK exports are the starting contract. Reference-server exchanges establish observed behavior, and real-client tests establish that complete workflows work. Claims remain bounded by the verified features, media profiles, and versions; implementing an API family alone does not prove every client can play.

Common protocol flows guide implementation now; a user-provided client shortlist is not a prerequisite. Testing specific clients measures and improves coverage of the shared backend rather than defining a separately customized backend for each client. Goby's React/MUI dashboard remains administrator-only. The separate Goby player and supported third-party clients provide playback interfaces over the existing compatibility APIs; selecting the player does not claim additional backend media profiles.

## Delivery definitions

| Stage | Deliverable | Compatibility claim allowed after verification |
| --- | --- | --- |
| P0 | Server setup, users/policy, library ingestion, client login/browse, direct video/audio playback, basic subtitles, progress, administrator operations | The tested direct-play workflows and media profiles only |
| P1 | Remux/transcode/HLS, full playback lifecycle, broader music/TV experience, richer user data, metadata management, client aliases/events | The exact tested client versions and feature/media profiles |
| P2 | Additional upstream features needed for broader feature coverage | Only each feature that passes its acceptance matrix |
| Deferred | Sync/offline packages, unselected optional extensions, additional Docker image profiles, provider online acceptance and production deployment | No support claim until scheduled and implemented |
| Unsupported delivery forms | Standalone binaries, systemd/native packages, DEB/RPM, Windows installers and non-Docker runtimes | Neither supported nor deferred obligations; native builds are development or historical-fixture artifacts |
| Excluded | Live TV/EPG/DVR/tuners, DLNA, external channels, synchronized parties, Emby consumer web application, Emby Connect/cloud identity, Emby package distribution and proprietary binary plugin compatibility | Explicitly outside the user-selected product scope |

P0 and P1 describe the original implementation order, not current completion
status or a promise of complete upstream parity. Deferred features require a
new scope decision. Do not return successful empty results merely to inflate
endpoint coverage.

One bounded client-navigation addition is the
[Programs query for the current zero-EPG-source profile](../development/live-tv-programs.md).
It is an authenticated, input-validated catalog-subject query with a real empty
program set; current authorization and storage errors remain errors. Its
reference shape and product verification are recorded separately. This does
not schedule or claim tuners, channels, program ingestion, DVR or Live TV
playback, and it does not by itself pass original-client acceptance.

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

Software and selected AMD profiles have recorded acceptance, including the
Linux amd64 OCI deliveries. Additional device, driver and media combinations
need their own evidence. Historical strict capacity/SLO targets are outside
the revised functional closeout; further performance claims require an explicit
profile. See [playback and transcoding](../research/playback-and-transcoding.md)
and the [current closeout boundaries](../development/phase3-functional-closeout-20260929.md#claims-outside-this-closeout).

## Protocols outside the REST inventory

| Protocol | Scope | Evidence required |
| --- | --- | --- |
| Emby WebSocket | Include session/event infrastructure early; implement the messages required by supported client workflows | Handshake path/query, authenticated session binding, `MessageType`/`Data` envelopes, subscriptions, heartbeat, reconnect and event payload captures |
| UDP discovery | Optional P0/P1 convenience for LAN clients; manual server URL remains the initial setup path | Official `who is EmbyServer?` request on UDP 7359 and JSON identity response; bind/address behavior on Linux/container networks |
| HTTP media semantics | Mandatory for any advertised playback profile | GET/HEAD, 200/206/304/416 as applicable, byte offsets, lengths, range edges, content types, cache validators, request cancellation |

These behaviors do not appear as a complete set of operations in Swagger. They must be tracked in addition to endpoint counts.

## Selected, deferred and excluded families

| Family | Decision and rationale |
| --- | --- |
| Live TV, EPG, DVR, tuner management | Explicitly excluded by the September 20 user decision; existing generic dynamic-source opening and time shifting are retained |
| DLNA profiles, discovery and SOAP services | Explicitly excluded by the September 20 user decision; this does not remove ordinary server discovery |
| Sync/offline downloads | Deferred: transfer/job/package semantics exceed simple authorized file download |
| External channels | Explicitly excluded by the September 20 user decision; integrated metadata/image/subtitle providers are a separate capability |
| Metadata/image/subtitle provider online acceptance | MusicBrainz's selected album workflow is [accepted](../development/online-providers-20260930.md); TMDB/OpenSubtitles live workflows remain pending credentials, with offline contracts and configuration prepared |
| Party/group synchronization | Explicitly excluded by the September 20 user decision; ordinary remote session commands are retained |
| BackupApi | Upstream plugin/core provenance and archive compatibility remain unselected; native Goby encrypted backup/recovery is already implemented and has separate accepted evidence |
| PluginService | Optional future Goby extension registry; no implied compatibility with Emby binary plugins |
| External notifications, local music Similar/InstantMix, intro skipping | Delivered within the four-phase plan's recorded scopes: GobyWebhookV1, local metadata discovery and sourced intro intervals; no vendor push or arbitrary client parity is implied |
| Automatic episode-intro analysis and BIF previews | Delivered in [Phase 2](../development/media-analysis-resilience-phase2-20260921.md); source-bound tasks, review/override, actual skip and named preview-consumer evidence retain the recorded corpus and client limits. Movie/isolated-episode content detection is not inferred |
| Theme media | Local theme-resource scanning, ownership, authorized ThemeMedia reads and delivery are implemented; the [theme record](../development/verification-m3e-theme.md) retains its source-specific acceptance. General recommendation or arbitrary-client behavior is not implied |
| General recommendations and game/book media | Deferred; not implied by local Similar/InstantMix, theme media or episode-intro analysis |
| Goby consumer web player | Selected by the October 4, 2026 user decision: standalone React + Vite application, independent build/deployment, optional Docker Compose integration; see the [player guide](../../web/player/README.md) |
| Emby WebAppService and proprietary consumer web application | Excluded; the Goby player does not implement or distribute Emby's web application |
| ConnectService and Emby cloud registration | Excluded: use Goby local accounts and configured server URLs |
| PackageService / Emby package installation | Excluded: Goby's official application delivery is a Docker image; this does not implement Emby package distribution or binary-plugin installation |

Unsupported features need an explicit capability decision and appropriate error
behavior. Supported Goby local features are free, and the authenticated feature
registration adapter reports that policy. This does not implement Emby cloud
identity or claim authorization from an external licensing service. Do not
substitute Jellyfin contracts for missing Emby evidence.

## Legacy candidates from the historical export

| Candidate | Newer baseline / decision |
| --- | --- |
| `GET /Users` | Newer `GET /Users/Query`; keep response array/envelope differences separate |
| `GET /Library/VirtualFolders` | Newer `GET /Library/VirtualFolders/Query`; add old form only with a target fixture |
| `GET /System/Logs` and `GET /System/Logs/Log` | Newer `/System/Logs/Query` and `/System/Logs/{Name}` |
| `GET /Search/Hints` | Implemented as an authenticated [legacy adapter](search-hints.md), including its root alias; recorded selected-phase acceptance does not establish that every client invokes or interprets it |
| Root routes without `/emby` | The [selected compatibility namespace](selected-management.md#literal-aliases-and-library-removal-refresh) supports root aliases and declared case-insensitive literals; opaque IDs and escaped segment boundaries retain their meaning |
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
