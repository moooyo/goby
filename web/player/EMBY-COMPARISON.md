# Emby API comparison for the player gaps

Reviewed on October 4, 2026. The original player review listed six handoff
limitations. The audit established that these were a mixture of unused Goby
endpoints, omitted compatibility behavior, unavailable source data, and custom
product features. This record retains that attribution and updates the selected
implementation follow-up. Runtime completion is recorded in
[ACCEPTANCE.md](ACCEPTANCE.md).

## Evidence and limits

The primary versioned baseline is the official Emby.SDK 4.9.5.0 export at
[`bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json),
retained in [the local snapshot](../../docs/sources/emby-sdk-openapi.snapshot.json).
Current official REST and plugin/model references were also read directly over
HTTPS. The search gateway was unavailable; failed guessed URLs were not used
as evidence.

The first documentation audit did not restart the inactive reference service.
The selected follow-up then created a separate isolated Emby 4.9.5.0 Docker
reference with generated SDR, PQ, HLG, and resolution-boundary samples. Its
HTTP observations are retained in the
[reference results](../../.artifacts/player-live-20261004/emby-filter-reference.json),
with the remote original at
`/opt/goby-test/player-live-20261004-165c/artifacts/emby-filter-reference.json`.
The [reference helper](../../scripts/test-env/player-reference-filters.py)
defines the sample and query matrix. This extends the original audit with
runtime filter evidence; it does not establish every upstream version, media
format, or automatic marker detector.

## Comparison and current status

| Handoff area | Original Emby evidence | Goby and player status | Attribution and accepted scope |
| --- | --- | --- | --- |
| Silent background clips | ThemeVideos/ThemeMedia, LocalTrailers, and ThumbnailSet exist. No exact universal stage-clip generator was identified in the inspected core API. | Goby's persistent source-side generation passed its 12-phase Docker run. The strict Vulkan/CPU-H.264 path passed bounded Profile 8.1 and complete Profile 7 MEL checks on authorized CT104. The October 5 native AMD extension adds Profile 5, Profile 8.4, and Profile 8.2 and passed all 21 required cases; Profile 8.2 uses original analytic material. | A Goby extension producing silent BT.709 SDR sidecars. FEL reconstruction is deferred. This is not a new Docker deployment, Dolby certification, or universal source/device coverage. |
| Next episode at the credits boundary | ChapterInfo has marker/timestamp fields; the official MarkerType reference includes CreditsStart but not CreditsEnd. | The manual/source path remains authoritative. The opt-in movie/TV detector reuses the chosen Intro Skipper project and passed seven real Docker phases. Complete intervals use GobyCreditsIntervals. | Public Emby marker compatibility and a Goby interval extension are distinct from algorithms. The observed audio starts were about 3.496 seconds early; no exact accuracy or common closed-source Emby implementation is claimed. |
| Mixed name search with explicit year/video-quality filters | Items exposes SearchTerm plus separate selectors. The official 4.1.1.0 export declares mixed Search/Hints; the pinned 4.9.5.0 export omits that route. Neither short description promises all originally requested free-text OR semantics. | The player consumes Goby's mixed Search/Hints, preserves item/entity identities and pagination, and navigates entity results through explicit filters. Year and video filters are separate controls. | The user accepted this implemented scope. Original-title/year/resolution free-text interpretation is not a pending requirement. |
| Handoff hours estimate and optional exact viewing time | Runtime/progress metadata supports a content-duration estimate. No actual-viewing-duration aggregate was found in the inspected core operations. | ViewingStatistics now aggregates visible watched runtimes once plus bounded unfinished progress; the player displays the estimated hours alongside watched counts. | The handoff estimate is implemented. Exact elapsed-time analytics remains an optional upgrade, not a design requirement. |
| Server-side 4K/HDR filtering with correct totals | ExtendedVideoTypes is an official HTTP query. Is4K and dimension bounds appear in request models. The isolated 4.9.5.0 HTTP run confirms width-based 4K behavior. | Database filtering/counting supports Is4K and ExtendedVideoTypes, including ordinary HDR/HDR10, HLG, and Dolby Vision. Series-wall descendant matching uses GobyAggregateVideoFilters. General width/height bound parameters are not implemented. | The user accepted existing 4K/HDR/HLG/Dolby Vision filtering. Exact HDR10+ classification is outside scope, not unfinished work; generic PQ still does not prove HDR10+ dynamic metadata. |
| Per-track waveform lanes | No core waveform-envelope endpoint or sample-envelope model was found. | The new AudioWaveforms API serves measured source-aligned peak/RMS levels from permanent per-source files. The player renders original-track lanes with validity gaps. | An implemented Goby extension, with opt-in generation, explicit Force replacement, and stale-axis rejection. |

## Direct evidence and implementation decisions

### Credits

The versioned enum is `Chapter, IntroStart, IntroEnd, CreditsStart`. The
[official MarkerType reference](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Entities.MarkerType.html)
and [Items response reference](https://dev.emby.media/reference/RestAPI/ItemsService/getItems.html)
establish the public marker contract.

Goby's accepted manual/source path emits `CreditsStart` through its shared
[chapter projection](../../internal/server/intro_markers.go). Its
[native credits API](../../docs/api/credits-markers.md) stores manual/import
points against the current source revision and rejects stale edits. A source
replacement invalidates the old override. This is a compatible timestamp
publication path; Emby's administrator write API is not asserted to be identical.

The limited official recheck on October 4, 2026 confirmed the MarkerType page
still lists Chapter, CreditsStart, IntroStart, and IntroEnd, without CreditsEnd.
The official support homepage was accessible, but the earlier Intro-Detection
URL returned 404 and public forum search returned 403; the web search gateway
was unconfigured. These failed lookups are not evidence that Emby lacks an
automatic feature. No additional movie/TV detector coverage, accuracy, or
algorithm claim is made from the public enum alone.

The new automatic increment is explicitly based on the previously selected
[Intro Skipper project](https://github.com/intro-skipper/intro-skipper/tree/6e0cb179007ac4c16cd9f358e9a617e791e9bf06),
not on Emby's closed-source code. It combines credits audio matching with the
upstream CreditsPass components, preserves multiple source-bound intervals,
and retains manual/source precedence. Standard Chapters carries the first
CreditsStart; complete bounds use `GobyCreditsIntervals` on item/source DTOs,
whose present empty array is authoritative. This explicit Goby extension avoids
inventing a CreditsEnd value in the standard enum. The automatic increment
passed seven real Docker phases plus focused checks. Chapter/black-frame
fixtures returned 60–90 seconds, while two audio boundaries were approximately
3.496 seconds early under unchanged upstream thresholds. See
[the acceptance record](ACCEPTANCE.md) for that bounded evidence.

### HDR and 4K filters

The official [Items query reference](https://dev.emby.media/reference/RestAPI/ItemsService/getItems.html)
declares `ExtendedVideoTypes`. The
[enum](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Entities.ExtendedVideoTypes.html)
includes `Hdr10`, `Hdr10Plus`, `HyperLogGamma`, and `DolbyVision`. HDR and 4K are
independent; `VideoTypes` describes file/disc types rather than resolution.

The official [BaseItemsRequest](https://dev.emby.media/reference/pluginapi/MediaBrowser.Controller.Api.BaseItemsRequest.html)
and [InternalItemsQuery](https://dev.emby.media/reference/pluginapi/MediaBrowser.Controller.Entities.InternalItemsQuery.html)
declare `Is4K`, `MinWidth`, `MinHeight`, `MaxWidth`, and `MaxHeight`. The pinned
GET parameter table does not enumerate every inherited property, so the
original audit correctly treated 4K as model evidence pending an HTTP check.
Goby implements `Is4K` for the selected 4K control; this increment does not add
`MinWidth`, `MinHeight`, `MaxWidth`, or `MaxHeight` query support.

The final reference sample has 17 Movies and four Episodes, including the
resolution boundaries and multi-video-stream cases. The original-server run
accepted `Is4K` over HTTP and classified
3800-pixel-wide and wider samples as 4K. A 3840x1600 scope sample qualifies;
a 1920x2160 portrait sample does not. Goby uses `Width >= 3800` accordingly.
Standard Series/Season containers have no own video stream and do not acquire
episode characteristics implicitly. The player sends
`GobyAggregateVideoFilters=true` on its Series wall to request a Series with a
matching descendant Episode. That behavior is labeled as a Goby extension.

The original multi-video observation applies `Is4K` to the primary/first video
stream and `ExtendedVideoTypes` to any video stream, so standard combined
selectors can be satisfied by different streams. Goby's Series aggregation
extension requires one descendant Episode and one matching video stream.
Filtering precedes result pagination and counting. Goby uses stored
PQ/HLG/Dolby Vision facts and does not
infer HDR10+ dynamic metadata from a generic PQ signal. See
[video-filter semantics](../../docs/api/video-catalog-filters.md) for input validation,
unknown metadata, aggregation, and HDR10+ behavior. The user accepted the
implemented 4K, ordinary HDR/HDR10, HLG, and Dolby Vision scope; precise HDR10+
classification is not an outstanding requirement.

### Search

The [Goby search-hint contract](../../docs/api/search-hints.md),
[route](../../internal/server/search_hints.go), and
[mixed search query](../../internal/library/search_hints.go) already provided
ranked and paginated media/entity name results before this frontend increment.
The [player adapter](src/lib/api.ts) now calls that route.

A Person or Genre result remains an entity. Selecting it opens associated
media through `PersonIds` or `GenreIds`; it is not silently relabeled as a movie.
Typed `GobyReference` values keep a media ID separate from an equal entity ID.
Pagination advances by all received mixed results, and counts are the server's
mixed total. `Years` remains an explicit filter. Original-title matching in an
original Emby version would need separate behavior evidence; the short
`SearchTerm` description alone does not prove or disprove it.

The user accepted mixed name search plus independent year/video-quality filters
as the product scope. Unified original-title/year/resolution free-text semantics
are therefore not a pending compatibility or frontend task.

### Background media

Emby documents [GET /Items/{Id}/ThemeVideos](https://dev.emby.media/reference/RestAPI/LibraryService/getItemsByIdThemevideos.html),
and its SDK also declares LocalTrailers and ThumbnailSet. Goby's registered
route is [GET /Items/{Id}/ThemeMedia](../../internal/server/themes.go), whose
`ThemeVideosResult` provides the theme videos. The player uses that route and
falls back to [LocalTrailers](../../internal/server/extras.go).

Eligible desktop stages start browser-compatible 8-bit original assets muted,
without PlaybackInfo or viewing-history reports. A later handoff restoration
adds user-gesture sound control for existing audible theme/trailer sources only.
Generated previews remain audio-free by specification. Known HDR or unsupported
sources are skipped. Small/coarse-pointer screens and reduced motion retain
still artwork. That accepted ThemeMedia integration itself does not generate
an excerpt or transcode a preview.

The user subsequently selected a separate
[persistent background-preview API](../../docs/api/background-previews.md).
Its source implementation generates silent H.264 MP4s beside each source,
defaults to 25 seconds within 1280x720, and permits only explicit Force to
replace a completed clip. The player defaults to theme, generated clip, then
trailer, with user-controlled ordering and a still-only option. Its 12-phase
Docker acceptance and focused backend/media/recovery verification passed and
are recorded separately in
[ACCEPTANCE.md](ACCEPTANCE.md). It is a Goby extension rather than a claim about
an original Emby generator.

### Watch time and waveforms

The original handoff's `meVals` function sums full runtimes of watched items
and runtime multiplied by progress fraction for unfinished items, then rounds
the total to hours. Its README labels that estimate as hours; it does not
specify elapsed wall-clock viewing time. The earlier audit expanded this into
an exact-analytics requirement. That stronger requirement is optional and is
not necessary to reproduce the handoff. The current
[ViewingStatistics API](../../docs/api/viewing-statistics.md) and player now
implement that estimate alongside watched movie/episode counts. They do not
introduce elapsed-time event analytics.

The pinned UserItemDataDto and ItemCounts models contain no actual-watched-hours
aggregate, and the inspected core route inventory has no waveform-envelope
contract. Goby's [audio waveform API](../../docs/api/audio-waveforms.md) is a
new extension with source-side peak/RMS data, not an original Emby field filled
from existing metadata. Playback/statistics plugins are separate surfaces
outside this core comparison; this audit does not preclude their capabilities.

## Selected follow-up

The user selected real Goby plus independent-player Docker acceptance first,
then mixed search, existing background media, 4K/HDR filters, and manual credits.
The six-phase real playback acceptance and the eight-phase real capability
acceptance both passed. The latter covers mixed search navigation, real indexed
4K/HDR totals, silent theme decoding, credits save/publication, paused/canceled
countdowns, automatic advance with the actual stop position, and clearing the
marker. Focused regressions and the two runtime receipts are recorded separately
in [ACCEPTANCE.md](ACCEPTANCE.md).

The user later selected persistent generated stage clips; this increment passed
its implementation and acceptance scope. The implemented mixed name search with
separate filters and existing 4K/HDR/HLG/Dolby Vision filters are accepted;
unified free-text semantics and precise HDR10+ classification are not pending.
The subsequently approved increment implements bounded DV background generation,
per-track measured waveforms, and the handoff's hours estimate. Exact elapsed-time
analytics remains optional. A subsequent automatic credits increment for movies
and TV is implemented and accepted within its recorded corpus and lifecycle
scope, using the previously chosen Intro Skipper project. Manual/source markers
retain precedence. It does not reopen completed steps 1–3 or expand the original
Emby algorithm evidence.
