# Music discovery adapters

The selected phase 3 music increment adds these authenticated Emby-compatible
reads. They use the normal user or application credential, current library and
content policy, and the existing music item/entity DTOs. They do not implement
Live TV, external channels, synchronization, remote music providers, or a player.

| Route | Seed / result |
| --- | --- |
| `GET /Items/Prefixes` | Complete authorized item-name prefix inventory |
| `GET /Artists/Prefixes` | Complete authorized artist-name prefix inventory |
| `GET /Artists/{Id}/Similar` | Numeric MusicArtist entity; MusicArtist DTO page |
| `GET /Albums/{Id}/Similar` | Opaque physical MusicAlbum item; existing item Similar page |
| `GET /Items/{Id}/InstantMix` | Item-first dispatch, then typed numeric music entity lookup |
| `GET /Songs/{Id}/InstantMix` | Opaque Audio item |
| `GET /Albums/{Id}/InstantMix` | Opaque physical MusicAlbum item |
| `GET /Playlists/{Id}/InstantMix` | Opaque visible Playlist item |
| `GET /Artists/InstantMix?Id=...` | Canonical positive decimal MusicArtist entity |
| `GET /MusicGenres/InstantMix?Id=...` | Canonical positive decimal Genre entity with visible music |
| `GET /MusicGenres/{Name}/InstantMix` | Escaped music genre name, including embedded `/` |

The normal `/emby` prefix and route-literal case aliases apply. Opaque item IDs
and escaped names retain their spelling. `Artists/AlbumArtists/{Name}` remains
a legacy name-detail alias, including an artist named `similar`; a common
dispatcher prevents overlapping Go ServeMux patterns. Explicit entity detail
navigation is also available through the search adapter's typed entity route.

## Contract evidence

The routes and `NameValuePair` / `QueryResult_BaseItemDto` result shapes come
from the pinned [SDK contract](../sources/emby-sdk-openapi.snapshot.json).
The SDK has no public recommendation algorithm. All weights, tie behavior,
fallback, and Unicode bucket rules below are explicit Goby choices.

The older [4.1.1 contract](../sources/emby-openapi.snapshot.json) declares query
`Id` on `Artists/InstantMix` and `MusicGenres/InstantMix`; the pinned newer SDK
omits it. Goby deliberately accepts exactly one such `Id` for these two routes.
An absent, empty, malformed, repeated, or noncanonical entity ID is rejected.

Read-only inspection of the retained original Web 4.9.5.0 bundle established:

- `modules/tabbedview/listcontroller.js` maps prefix results using `i.Name`,
  clears paging/projection controls for prefix enumeration, and sends
  `NameStartsWithOrGreater`, `ArtistStartsWithOrGreater`, or
  `AlbumArtistStartsWithOrGreater` when jumping to a prefix.
  SHA-256: `26a7e232755742188c95d1d58dd3c57e9823562dea2d17ce553333adb7db01b8`.
- `modules/tabbedview/artiststab.js` sends `ArtistType=Artist,AlbumArtist` for
  its combined artist list, or `AlbumArtist` for album artists.
  SHA-256: `6579ff04e17d9ab952afcd243596bab881f392a7dc7652c972283bc58a9325be`.
- `modules/emby-apiclient/apiclient.js` sends `getInstantMixFromItem` to
  `Items/{id}/InstantMix`.
  SHA-256: `33a3bbd17ae019ca6964262e7741b26a45f94414aaf6ea79a1aa86465b221d12`.

These are source observations, not positive original-server captures or actual
browser acceptance. Original consumer licensing is outside the delivery gate.

## Prefixes and artist roles

Prefixes return a bare array of `{ "Name": "A", "Value": "2" }`. `Name` is
the first non-whitespace Unicode character, uppercased when Unicode supplies a
case mapping. Han characters remain their original character; no pronunciation
or pinyin is inferred. Digits, accented Latin letters, other scripts, and
punctuation keep their actual initial instead of an invented wildcard bucket.
`Value` is the decimal count as a string, a Goby rule; the observed Web source
consumes only `Name`. Empty/control-only names do not produce buckets.

SQL trims the complete Unicode White_Space set before selecting an initial.
Counts merge case-equivalent initials and count distinct artists for artist
queries, not their number of track associations. Buckets use stable Unicode
code-point order. The complete authorized filtered population is aggregated
before pagination; `StartIndex`, `Limit`, and sorting do not truncate buckets.

`ArtistType` accepts `Artist`, `AlbumArtist`, or a comma-separated combination
in either order, as one query value. Unknown roles, duplicate roles, empty
components, and repeated query values fail. Artist lists and prefixes share
this behavior. Existing item filters select source membership; name/favorite
artist filters retain independent entity semantics.

Item `ArtistStartsWithOrGreater` and `AlbumArtistStartsWithOrGreater` compare
the same first ordered display credit used by the corresponding sort. Track
album artists use their complete own group when present, otherwise the nearest
authorized physical album. Missing credits do not match a lower bound.
Name prefix predicates continue to compare item names, including when an item
has a distinct custom SortName; they do not silently redefine that existing
query contract.

## Similar

The album route reuses the established `Items/{Id}/Similar` implementation and
checks that the seed is a MusicAlbum in the same authorization transaction. It
does not reinterpret an Audio ID as an album or change existing item scores,
random equal-score ties, artist exclusions, explicit sort, or page-length count.

Artist Similar uses an independent metadata profile. The profile contains the
distinct Genre, Tag, and Studio entities on visible music with own Artist or
effective AlbumArtist credit. Each distinct shared feature contributes one
point; at least one point is required. Duplicate tracks and repeated roles do
not multiply a feature. Hidden source items cannot supply seed features or
candidate score. People, provider assertions, and popularity do not contribute.

Candidate filters select artist membership but do not narrow seed authority or
the full authorized profile. The seed is excluded; `ExcludeArtistIds` excludes
candidate entity IDs. Default order is score descending, then lowercased name
and numeric ID. Explicit Name/SortName order replaces score order; other entity
sorts are rejected. An empty profile returns no unrelated artists.
`TotalRecordCount` is the returned page length, matching the established Similar
adapter contract, including `Limit=0` and distant pages. Images and independent
entity user state are projected in the same repeatable-read subject transaction.

## InstantMix

All seven routes use one seed resolver and queue generator. A normal item ID is
opaque, including decimal strings. `Items/{Id}` first resolves the catalog item;
an existing hidden or unsupported item is not reinterpreted as an entity. Only
when no item exists may a canonical decimal ID resolve to a visible MusicArtist
or music Genre. Explicit entity routes remain unambiguous even if an item has
the same public string ID. Orphan entities and genres found only on nonmusic
items cannot authorize a seed.

Audio seeds select that track; albums select tracks with that nearest physical
album; artists select own Artist or effective AlbumArtist tracks; genres select
their credited tracks. Playlists require current container visibility and each
member's current source permission. Repeated playlist entries contribute one
track identity; output does not claim a `PlaylistItemId` for that deduplicated
queue. A visible seed with no playable tracks returns an empty queue.

Candidates are physical, nonfolder Audio rows with current probe version,
positive change time and duration, an audio stream, a matching library/root
relationship, nonempty indexed path/identity, positive file size, and a recorded
modification time. A declared positive probe size must match indexed size.
Sources in prepared or catalog-committed media publication are excluded.
Missing-episode facts never enter this population. Playback permission is
required. This is catalog admission, not a filesystem probe or future access
grant: PlaybackInfo and every media open retain their current authorization,
root containment, source-identity, and publication checks.

The complete authorized seed and candidate populations share one subject
transaction. Candidate search, state filters, artist/item exclusions, library
scope, and paging do not alter seed authority. Shared credit IDs are deduplicated
across own Artist and effective AlbumArtist roles. Default score is:

| Relationship | Points |
| --- | ---: |
| A playable seed track | 16 |
| Physical album shared with a playable seed track | 8 |
| Each shared artist entity | 4 |
| Each shared genre entity | 2 |
| Each shared tag or studio entity | 1 |

Album seed metadata supplements its track metadata. Higher scores come first;
equal scores use lowercased SortName, then opaque item ID. Unrelated authorized
tracks score zero and form a deterministic fallback tail. There is no random
number source, external recommendation lookup, or learned preference model.
Explicit supported item sorting replaces score order. Scores are not wire DTO
fields and do not imply that seed tracks always outrank every high-feature
candidate.

HTTP defaults to 100 rows and bounds a requested page to 1,000. Explicit zero
returns an empty non-null array with the complete candidate count. Unlike
Similar, InstantMix `TotalRecordCount` is the full filtered candidate population;
distant pages retain it. Mix reads never write history, played/favorite state,
sessions, or playlists. Suggestions and Similar hide-played preferences are
not applied to an explicit mix; explicit requested state filters still apply.

## Verification boundary

Phase 3 checks cover typed namespaces and collisions, all seed adapters,
authorized metadata ranking/fallback, deterministic pages, zero/distant counts,
playlist privacy and deduplication, current playback policy, incomplete source
snapshots, Unicode initials, independent artist roles/state, and the legacy
album-artist name route. The [phase 3 execution record](../development/selected-compatibility-phase3-20260920.md)
separately records the composed backend checks and actual browse/mix/audio
consumer journey, including retained failures and the exact client boundary.
