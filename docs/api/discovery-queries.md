# Discovery queries and Suggestions

This document describes the selected phase 3 adapter behavior. Source and test
implementation are not a claim that phase acceptance has run. The phase delivery
record contains executed verification results.

## Supported query matrix

`Items`, user `Items`, season/episode browsing and `Suggestions` share the selected
item predicates below. Each repository operation resolves current credential
authority before counting, ordering and paging. Application credentials retain
their independent authority; an explicit `UserId` selects state, not credentials.

| Selector | Source and semantics |
| --- | --- |
| `Filters=Likes` / `Dislikes` | Exact current-user `likes=true` / `likes=false`; absent or null likes match neither. |
| `Filters=IsFolder` / `IsNotFolder` | Catalog `is_folder=true` / `false`, equivalent to `IsFolder`. |
| `IsPlayed`, `IsFavorite`, `IsFavoriteOrLikes`; corresponding existing Filters | Current selected-user state. Favorite-or-likes is the union of favorite and positive likes. Separate predicates intersect. |
| `Filters=IsPlayed` / `IsUnplayed` | Equivalent explicit `IsPlayed=true` / `false`. Conflicts with `IsPlayed` or another played filter return `400`. |
| `Filters=IsResumable` | Existing duration/position and hide-from-resume rules. Missing facts are never resumable. |
| `ArtistStartsWithOrGreater`, `AlbumArtistStartsWithOrGreater` | Lexical lower-case bound on the same first effective display credit used by the respective sort. No second-credit match can move a row before its bucket. |
| `IsMissing`, `IsPlaceHolder` | True for active expected episodes without matching physical media; false for physical catalog entries. |
| `IsVirtualUnaired` | True only for an expected missing episode with an explicit future premiere date. |
| `IsUnaired` | True only for an Episode with an explicit future premiere date, whether physical or expected. Unknown dates are not evidence of future airing. |

Existing types, media types, item/parent IDs, recursive scope, music/entity
relationships, exclusions, years, date ranges, community rating, name bounds,
overview/subtitle/HD predicates remain supported. An unsupported `Filters` value
or sort key returns `400`; recognized selectors validate their actual values.
The new artist bounds and missing selectors reject empty, repeated and conflicting
case aliases. Missing selectors preserve absent/false/true states. Historical
unselected query keys retain their existing compatibility-hint behavior; their
acceptance is not a promise that Goby filters on them.

`SortBy` accepts up to eight distinct keys and either one direction or one per
key. Existing keys remain `SortName`, `Name`, `DateCreated`, `IndexNumber`,
`ParentIndexNumber`, `ProductionYear`, `PremiereDate`, `CommunityRating`, `Runtime`,
`DatePlayed` and `PlayCount`. Added music keys are:

| Key | Backing value |
| --- | --- |
| `Album` | Current physical MusicAlbum name, or a track's nearest same-library physical album name. |
| `Artist` | First own Artist association ordered by display position and persistent entity ID. |
| `AlbumArtist` | First own AlbumArtist association; only an absent own group inherits the nearest physical album's group. |

Music names use database lower-case lexical ordering. Missing values sort last
in either direction. Stable item ID breaks final ties in the last requested
direction. Album traversal uses current authorization, stays in one library,
and terminates corrupt parent cycles. `Random` is not an admitted sort.

## Missing episode discovery

Expected facts are separate from physical `items`. A numbering gap does not
create one. The expected-episode import contract records a bounded administrator
source and explicit entries; failed imports do not replace previously accepted
facts. The public projection carries no source payload or administrator
provenance.

By default, general browsing includes physical catalog entries. The logged-in
user's `DisplayMissingEpisodes=true` preference also admits authorized expected
facts. Application keys do not receive personal display defaults.

| Explicit condition | Resulting precedence |
| --- | --- |
| `IsMissing=false` or `IsPlaceHolder=false` | Excludes expected rows even when the preference is true. |
| `IsMissing=true`, `IsPlaceHolder=true`, `IsVirtualUnaired=true` or `IsUnaired=true` | Admits the expected population, then applies every supplied predicate. |
| `IsVirtualUnaired=false` | Excludes future expected rows; a true display preference can still show past/unknown-date expected rows. |
| No positive selector and preference false | Does not implicitly add expected rows. |
| Explicit expected ID | Resolves the active authorized expected row; false missing/placeholder predicates still exclude it. |

Contradictory classification predicates intersect to an empty population; they
do not override each other. The mixed physical and expected population is
filtered, counted and paged in one database transaction with the same ordering.
Matching real media removes the missing projection without changing that real
item's identity or state. Current series and structural season authorization
apply to expected rows. Ambiguous duplicate physical seasons are not used as a
fallback path around a hidden season.

Expected detail IDs resolve through the same current fact and authorization
rules. Their DTO has `LocationType=Virtual`, `IsMissing=true`, `IsPlaceHolder=true`,
`IsVirtualUnaired` and `IsUnaired`; download/resume capabilities are false. They
have no `Path`, `MediaSources`, `MediaStreams`, duration or writable `UserData`.
They do not enter NextUp, playback queues, Resume, InstantMix, Suggestions or
physical item counts. Their IDs do not authorize playback or state writes.

## Source-backed Fields

Field names are case insensitive. Detail projections include retained values;
list projections include the selected values when requested.

| Fields | Backing source |
| --- | --- |
| `LocationType` | `FileSystem` for an indexed physical path; always `Virtual` for an expected episode. No remote-location inference. |
| `Size`, `Bitrate` | Positive retained probe facts; absent facts are omitted. |
| `VideoCodec`, `Width`, `Height` | First non-attached-picture video stream. Artwork dimensions never masquerade as video dimensions. |
| `AudioCodec` | First retained audio stream. |
| `GenreItems`, `TagItems` | Aliases of existing `Genres` and `Tags` metadata projection, using retained associations. |
| Existing supported fields | `Path`, `Overview`, `Container`, `MediaStreams`, `MediaSources`, `Chapters`, descriptive metadata (`ProductionYear`, `PremiereDate`, `OriginalTitle`, `CommunityRating`, `OfficialRating`, `ProviderIds`, `Genres`, `Tags`, `Studios`, `People`) and authorized image aspect ratio. |

Existing identity, hierarchy, music relations and user-state projection remain
available on their established routes. `EnableUserData=false` and
`EnableImages=false` still control those projections. Unimplemented Fields are
tolerated as compatibility hints and omitted; no fabricated provider rating,
production status, chapter image or arbitrary metadata is returned.

## Suggestions

`GET /Users/{UserId}/Suggestions` and its `/emby`/case aliases implement the pinned
SDK `QueryResult_BaseItemDto` contract: `{Items, TotalRecordCount}`. The actual
endpoint is distinct from NextUp, Similar and InstantMix. It uses a documented
local selection policy and makes no external recommendation/provider calls.

The default is recursive catalog discovery with limit 20. Candidates are
authorized non-folder Movie, Episode, Audio, Video or MusicVideo rows with actual
retained media facts. Explicit item query predicates constrain this population.
Ranking prefers the selected user's favorite or positive-like entries, then
unplayed entries, then newest catalog creation time and stable ascending ID.
An explicit `SortBy` replaces the default ranking. All user-state predicates,
ranking and DTO state use the same subject read transaction. The count describes
the full filtered population; `Limit=0` returns an empty Items array with that
count.

`HidePlayedInSuggestions=true` supplies `IsPlayed=false` only if no explicit
played predicate was supplied. `IsPlayed=true/false` and
`Filters=IsPlayed/IsUnplayed` take precedence; contradictory explicit forms return
`400`. Favorite/like predicates alone do not override this played default.
Changing played state changes the next Suggestions read, without changing
another user's state. An application key remains preference-neutral even with
an explicit target user. This preference is never applied to InstantMix.
