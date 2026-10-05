# Player backend capabilities and design gaps

This audit compares the standalone player with the current Goby source and the
`design_handoff_goby_player` v3 handoff. It describes implemented HTTP contracts,
not a promise that a particular library already contains artwork, subtitles, or
completed analysis. The player calls the real Emby-compatible API; the acceptance
fixture is a separate test server and is not bundled into the application.

The [original Emby API comparison](EMBY-COMPARISON.md) separates documented
upstream contracts, original-server observations, and Goby extensions. The
selected follow-up connects mixed search and existing background media, adds
server-side video filters, and publishes source-bound credits markers. The
[acceptance record](ACCEPTANCE.md) records six real Docker playback phases and
eight real capability phases, all passed, alongside the focused regressions
and remaining feature boundaries. Persistent generated background clips passed
a subsequent 12-phase real Docker run, with focused FFmpeg, task, browser, and
PostgreSQL recovery verification recorded separately.

## Supported contracts used by the player

| Area | Contract and implementation evidence |
| --- | --- |
| Authentication and account | `POST /emby/Users/AuthenticateByName` accepts `Username` and `Pw`; client/device metadata is sent in `X-Emby-Authorization`. The token, user, and server are retained locally, and `POST /emby/Sessions/Logout` revokes the session. See [authentication](../../internal/server/emby.go) and [routes](../../internal/server/server.go). |
| Libraries and catalog | `/Users/{id}/Views`, `/Users/{id}/Items`, item detail, `/Shows/{id}/Seasons`, and `/Shows/{id}/Episodes` support catalog browsing, paging, metadata, and season order. List requests explicitly request metadata and stream fields. See [catalog DTOs and queries](../../internal/server/items.go). |
| Latest additions | `/Users/{id}/Items/Latest` filters playable source items before grouping. The player adapter translates a requested `Series` type to `Episode` with `GroupItems=true`; the server then returns the series representative. Sending `Series` directly to this endpoint produces no source items. See [latest implementation](../../internal/library/latest.go) and [contract tests](../../internal/library/latest_test.go). |
| Continue watching and next episode | `/Users/{id}/Items/Resume` and `/Shows/NextUp` are backed by persisted user progress. The episode endpoints expose real episode ordering. See [play state](../../internal/server/playstate.go), [next-up route](../../internal/server/client_sessions.go), and [episode queues](../../internal/server/episode_playback_queue.go). |
| Favorites and watched state | `POST` and `DELETE /Users/{id}/FavoriteItems/{itemId}` and `/PlayedItems/{itemId}` return updated `UserData`; folder watched mutations can affect descendant items. See [play-state handlers](../../internal/server/playstate.go). |
| People and recommendations | Detail `People`, `/Persons`, and `/Items/{id}/Similar` expose actual indexed metadata and backend similarity results. See [entities](../../internal/server/entities.go) and [similarity](../../internal/server/similar.go). |
| Mixed search | `/Search/Hints` returns media, people, and genres in one ranked, counted, paginated response. The player preserves typed `GobyReference` identities, including colliding item/entity IDs, and follows people or genres to their associated media through explicit ID filters. Years remain an explicit selector. See [search hints](../../docs/api/search-hints.md). |
| Resolution and HDR filters | `Is4K` and `ExtendedVideoTypes` filter before pagination/counting. The player now has separate 4K, HDR, HLG, and Dolby Vision controls plus an independent year selector. `Is4K` uses the first non-attached video stream's width, at least 3800 pixels. `GobyAggregateVideoFilters=true` allows a matching Episode to qualify a Series. General width/height bound query parameters are not implemented. See [video filters](../../docs/api/video-catalog-filters.md). |
| Background source selection | The default order is inherited ThemeMedia, generated BackgroundPreview, then LocalTrailers, followed by artwork. Users can reorder sources or disable motion. Stages start muted; an explicit user gesture can enable sound only for an existing theme/trailer with an audio stream. Generated H.264 MP4s remain silent. Reads create no PlaybackInfo, encoding session, history event, or generation request. See [stage selection](src/components/ThemePreview.tsx). |
| Persistent generated backgrounds | The optional library setting and explicit administrator task generate source-side clips for Movies and Episodes. Completed files survive profile/source changes and disabled generation; only explicit Force replaces them. The default is 25 seconds within 1280x720 at 1.5 Mbps. SDR/HDR10/HLG use the software path; admitted Profile 5, 8.1, 8.4, 8.2, and complete Profile 7 MEL use strict Vulkan processing with CPU H.264 encoding. The October 5 native AMD extension passed its bounded corpus; Profile 8.2 uses original analytic material and FEL is not reconstructed. See [background previews](../../docs/api/background-previews.md). |
| Artwork | `/Items/{id}/Images/{Type}` and `/Images/Backdrop/0` use authenticated image tags, width limits, and real indexed artwork. Missing images use the UI's fallback, not a third-party placeholder. See [image projection](../../internal/server/images_dto.go). |
| Original-file streaming | `/emby/Videos/{id}/stream?Static=true` and the negotiated `/videos/{id}/original.{container}` aliases serve the authorized original bytes, ranges, and source MIME type. Browser playback declares MP4/H.264/AAC capabilities and conditionally WebM. Arbitrary MKV files are not declared browser compatible. See [stream routes](../../internal/server/streams.go), [video handler](../../internal/server/video_http.go), and [MIME types](../../internal/media/source.go). |
| Playback negotiation | `POST /Items/{id}/PlaybackInfo` accepts the actual device profile, source, stream indexes, bitrate, source-clock start, and direct/transcode flags. The UI consumes the returned source and URL; unsupported media or policy failures remain errors. See [request schema](../../internal/playback/models.go) and [negotiation](../../internal/server/playback_info.go). |
| Quality conversion | The player requests 1080p at 20 Mbps, 720p at 8 Mbps, or 480p at 3 Mbps using profile output dimensions and total streaming bitrate. Lower-quality selection disables original delivery. Encoding still depends on configured encoder capacity and current user permissions; labels are requested maximums, not promises of an exact output bitrate. See [conversion profiles](../../internal/playback/video_profiles.go). |
| Audio and subtitles | Stream indexes identify real tracks. Direct media uses negotiated external WebVTT; HLS advertises WebVTT manifest renditions, and bitmap PGS/DVD subtitle selection can use the existing burn-in path when the server permits it. The player does not attach a second external track over HLS subtitle renditions. See [subtitle DTOs](../../internal/server/subtitles_dto.go), [HLS subtitle selection](../../internal/playback/hls_subtitles.go), and [subtitle clocks](../../internal/subtitle/hls.go). |
| Progress and cleanup | Started, Progress, and Stopped events use the negotiated `PlaySessionId`, `MediaSourceId`, actual source position, and selected stream indexes. Stopped is terminal. `/Videos/ActiveEncodings` provides explicit resource cleanup. See [reports](../../internal/server/playstate.go) and [stop routes](../../internal/server/hls_http.go). |
| Intro skip | Detail and playback sources include `Chapters` with `IntroStart` and `IntroEnd` when source-bound intro analysis or manual markers exist. The browser performs the seek. See [intro projection](../../internal/server/intro_markers.go). |
| Credits cue and automatic analysis | Opt-in movie/TV credits analysis reuses the previously chosen Intro Skipper project and has passed a separate seven-phase real Docker run. Multi-interval data uses GobyCreditsIntervals; standard chapters retain the first CreditsStart without adding a CreditsEnd enum. Manual/source points take precedence. Source/support/profile changes and disabled policy prevent invalid automatic publication. See [credits markers](../../docs/api/credits-markers.md). |
| Preferences | `/Users/{id}/Configuration` supports audio/subtitle language, subtitle mode, autoplay, and intro skipping. `/DisplayPreferences/{scope}` stores client settings in `CustomPrefs`. The player uses separate per-item scopes for quality, track indexes, and external-player choice, plus a global display scope; local state is an immediate cache. Read/write failures are surfaced, and stale reads cannot overwrite a newer local selection. See [preference endpoints](../../internal/server/user_preferences.go) and [storage limits](../../internal/identity/user_preferences_store.go). |
| Viewing counts and recent history | Played item counts can be requested with `IsPlayed=true` and `Limit=0`, separately for movies and episodes. `SortBy=DatePlayed` and real `LastPlayedDate` support recent viewing. Library-wide counts are separate and are not presented as watched counts. See [sort fields](../../internal/library/item_sort.go) and [count queries](../../internal/server/items.go). |
| Estimated content hours | `/Users/{UserId}/ViewingStatistics` sums each visible watched Movie/Episode runtime once and bounded progress for unfinished items. The player displays the rounded estimate; this is the handoff's content-duration concept, not elapsed viewing analytics. See [viewing statistics](../../docs/api/viewing-statistics.md). |
| Per-track audio waveforms | `/Items/{Id}/AudioWaveforms` describes existing source-aligned peak/RMS envelopes and its stream-index route serves bounded binary data. The player renders actual waveform lanes with missing-data gaps. Opt-in tasks store permanent sidecars separately from background MP4s; reads do not generate data and stale source axes are not served. See [audio waveforms](../../docs/api/audio-waveforms.md). |
| Bitmap subtitle timelines | `/Items/{Id}/SubtitleTimelines` and its stream-index route return existing source-aligned display intervals for embedded PGS/DVD, external SUP, and each language track in an external IDX/SUB pair. Default-off tasks persist source-side GSTL files without OCR or GPU processing. Only valid nonempty data creates a label and lane. External bitmap records carry `GobySubtitleTimelineOnly` and do not advertise unavailable playback delivery. See [subtitle timelines](../../docs/api/subtitle-timelines.md). |

## Frontend omissions found in the renewed handoff audit

The earlier capability inventory did not enumerate every missing interaction or
presentation detail. The October 5 walkthrough found additional frontend
omissions for which Goby already supplied the required data. Those omissions
must not be described as absent backend capabilities or dismissed by the earlier
feature/fixture closeouts.

The source now connects poster hover cards, artwork-driven hover color with the
non-stage idle gold accent, real seek-preview frames, separate HLG/Dolby Vision/
year controls, catalog-specific Movie/Series genre queries, additional
real media-format fields, consistent text-subtitle styling and selected-track
labels, the single-row mobile toolbar and restored media-information layout,
stage transitions/focus, Series-deduplicated recent viewing, horizontal
edge controls, preview audio for existing audible assets, and the complete
platform-filtered external-player list with official marks. These use existing
contracts or client presentation logic. The build/deployment, 101 distinct
automatic checks, staged three-viewport visual review, and read-only real-service
walkthrough passed. Initial and intermediate screenshots remain marked as such;
the final hover-anchor/HUD refinements have separate evidence in
[ACCEPTANCE.md](ACCEPTANCE.md).

## Accepted search and video-filter scope

The user accepted the implemented mixed media/person/genre name search through
[Search/Hints](../../docs/api/search-hints.md), followed by explicit entity,
year, and video-quality filters. Original titles are not matched by the current
name query, and a free-text year or resolution does not become a cross-field OR
query. Those behaviors are outside the accepted search scope and are not
unfinished work.

The user also accepted the existing 4K and ordinary HDR/HDR10, HLG, and Dolby
Vision filter capabilities with server-side totals. The broad HDR control
includes PQ sources; the stored probe facts do not prove HDR10+ dynamic metadata.
Exact HDR10+ subtype classification is not required and is not a pending gap.
See [filter semantics](../../docs/api/video-catalog-filters.md) for the unchanged
technical behavior.

Mixed Search/Hints name results and year/video-quality browsing are explicit
modes. Selecting year/quality can leave mixed name search, as the UI explains.
The user explicitly excluded intersecting the full media/person/genre hint
union with year/quality filters. That combined query is out of scope, not a
pending task; the implemented independent modes remain the accepted behavior.

## Capability boundaries

1. **Background generation for every possible source.** The source-side
   [background-preview pipeline](../../docs/api/background-previews.md) now
   implements configurable clip generation; it is separate from BIF seek
   thumbnails. Its strict DV path supports Profile 5, 8.1, 8.4, 8.2, and the
   complete Profile 7 MEL subset on an admitted Vulkan device. The October 5
   native AMD extension passed all 21 required cases, as recorded separately in
   the [acceptance record](ACCEPTANCE.md). Profile 8.2 is covered by original
   actual-SDR-base/RPU analytic material, without a public commercial-source
   reference. FEL reconstruction remains deferred; this media receipt does not
   establish a new Docker deployment or other-device acceptance.
   Unsupported or insufficient source facts still fail rather than implying
   universal codec/color coverage. A generated clip requires enabled or requested
   work and writable source-side storage. If no usable source exists, the player
   retains artwork. Small/coarse-pointer screens, reduced motion, and the user's
   still-only preference intentionally keep artwork. Generation support remains
   distinct from the existing Dolby Vision catalog filter.

2. **Credits accuracy outside the accepted corpus.** Single-source Movie and
   multi-source TV analysis have passed the selected integration/lifecycle
   acceptance, with library opt-in defaulting off. The pinned matcher placed
   the audio sample starts about 3.496 seconds before the known music starts;
   thresholds were not adjusted to force a match. Source-bound evidence does
   not guarantee universal semantic accuracy or exact-second boundaries.
   `no_result` can represent an actual no-match or a reasoned abstention and
   never becomes a fabricated tail. Manual/source points retain precedence.
   No identity with Emby's closed-source algorithm is asserted.

3. **Exact elapsed viewing analytics.** The current player shows watched
   movie/episode counts and estimated content hours. The handoff does not measure
   elapsed viewing time: `meVals` in
   `D:/Code/design_handoff_goby_player/goby-core.js` sums full runtimes for watched
   items and runtime multiplied by progress fraction for unfinished items, then
   rounds the total to hours. It is an estimate of content watched. The handoff
   README labels it as hours without requiring actual-time analytics.
   The new [statistics API](../../docs/api/viewing-statistics.md) implements
   that estimate with a precision-preserving total and explicit estimate flag.
   Exact elapsed-time analytics remains a separate optional upgrade, not a
   prerequisite for matching the design. Existing user data exposes position,
   play count, watched state, and last-played date, but no exact viewing-duration
   aggregate. See [user data](../../internal/server/playstate.go) and
   [catalog counts](../../internal/server/navigation.go). Exact analytics has
   not been selected for implementation.

4. **External bitmap playback and advanced bitmap features.** The selected
   [timeline increment](../../docs/api/subtitle-timelines.md) now includes
   source-bound external SUP and multilingual IDX/SUB display intervals.
   External bitmap delivery/burn-in and paired-file management remain separate
   from this timeline capability. `GobySubtitleTimelineOnly` prevents these
   tracks from entering playback selection. Unsupported IDX presentation
   directives still fail explicitly. Generation and current, nonempty material
   are required before a timeline lane appears; missing data creates no status
   row. The earlier seven-phase embedded-only Docker receipt remains separate
   from the schema-61 external increment in [ACCEPTANCE.md](ACCEPTANCE.md).

Generated background clips remain audio-free by the user's established output
specification. The new preview-sound action uses only audio already present in
theme videos or trailers. Adding audio to generated clips would change that
specification and is not a required design fix.

These boundaries do not reopen completed implementation steps 1–3 or create
new development commitments. Generated stage clips use persistent source-side
files, and their acceptance is separate from the completed search, existing-media,
filtering, and manual-credits work. The later waveform, estimated-hours, and
bounded DV implementation has its own acceptance scope in [ACCEPTANCE.md](ACCEPTANCE.md).

## Design differences that are not missing backend functionality

- The handoff's fake plots, ratings, titles, tags, cast, artwork, and technical
  badges must be replaced by indexed values. Empty metadata is a library-data
  issue, not a justification to fabricate facts.
- There is no persisted curated six-title hero selection or server-computed
  poster accent field. Latest additions can supply the hero; the browser can
  use its accent fallback or derive colors from readable artwork. These are
  presentation policies and do not prevent the player from working.
- External-player icons and URL handling are frontend/device integration.
  The full platform-filtered list and official application marks are present.
  Builders exist for PotPlayer on Windows, mpv 0.41+ on desktop with a registered
  handler, Dandanplay on Windows/Android, IINA, mobile VLC, MX Player, and Infuse.
  Desktop VLC, Stellar Player, macOS Dandanplay, and nPlayer use a manual stream
  URL because a first-party browser-launch contract was not established for
  those combinations. This is not a missing Goby stream API.
  Browser-installed application/handler behavior was not tested. The reopen/copy
  notice holds the authorized launch URL only in current UI state; preferences
  retain player choice rather than that URL. Selection resolves a platform-valid
  per-item choice, then the global choice, then the built-in player. See
  [application marks and protocol evidence](THIRD-PARTY.md).
- Subtitle variants are selected from actual metadata. Generic `chi`/`zho`
  without a variant-specific language tag or title cannot prove simplified,
  traditional, or bilingual text. The client uses title hints where available.
- The API declares HLS rather than progressive HTTP conversion. HLS playlists
  advertise a complete source timeline and an `EXT-X-START` position, so the
  requested resume offset must not be added again to `currentTime`. This does
  not establish media-clock acceptance for every possible encoder/container.
  The real Docker acceptance covers the selected H.264/AAC source, software
  HLS conversion, seek/resume, and SRT-to-WebVTT path.
  See [HLS manifest](../../internal/server/hls_generated_window_manifest.go) and
  [timestamp research](../../docs/development/media-client-timestamps.md).

## Verification scope

The initial local TypeScript/build and browser acceptance validate the
standalone frontend and deterministic fixture contracts. The subsequent
`ssh test-env` acceptance runs a real Goby, PostgreSQL, and independent nginx
player in isolated Docker services with generated media and a synthetic account.
Its six completed playback phases exercise actual decoding, software HLS,
subtitles, seek/audio changes, persisted resume, next-episode navigation, and
encoder retirement. Neither environment uses the user's private library.
The subsequent eight-phase real capability run covers mixed search, 4K/HDR
counts, silent theme decoding, and administrator/player credits behavior.
Persistent background generation passed 12 real Docker phases, including
source-adjacent storage, request reuse, disabled/profile/source-change retention,
failed/canceled Force preservation, and successful explicit replacement.
Actual FFmpeg 9.0.1 SDR/HDR10/HLG and 1080p checks, 40 browser cases, nine
administrator Node checks, task regressions, and PostgreSQL 17 recovery passed
on `test-env`. The selected source/color coverage remains bounded by those tests.
The subsequent schema-58 increment passed 63 Playwright cases, 17 administrator
checks, focused HTTP/task/migration and real FFmpeg waveform checks, 21 waveform
plus 22 background backup semantic cases, and two actual recovery flows. Its
real waveform Docker journey passed eight phases. Separately, authorized CT104
DV generation passed the selected Profile 8.1/complete Profile 7 MEL cases;
the detailed receipt keeps this bounded GPU evidence separate from general
software/container acceptance.
Source implementation, focused regression checks, and integrated runtime results
are recorded separately in [ACCEPTANCE.md](ACCEPTANCE.md); they do not imply GPU,
all-codec, production deployment, or universal original-Emby compatibility.
