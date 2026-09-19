# Embedded music metadata and artist identities

## Current phase 3 music contract

The current music additions passed the selected
[phase 3 scopes and closeout](amd-media-phase3-20260919.md).
Music tag facts use `CurrentMusicMetadataVersion = 3`,
independently of the technical media-probe version. Existing persisted
`music_source` remains Version 1 with optional extension fields; old source
hashes, identities and manual controls are preserved rather than relabeled as
newly extracted facts. A scan refreshes older music facts before using the new
fields. Historical source15/18/20 evidence below does not verify these additions.

| Embedded format tag | Accepted meaning |
| --- | --- |
| `title`, `album`, `artist`, `album_artist` | Existing bounded scalar values; album-artist aliases remain supported |
| `artists`, `album_artists`/`albumartists`, `composers`, `genres` | Explicit plural credits: a JSON-array-valued string or semicolon-separated list |
| `composer`, `genre` | Single literal credit; used when the corresponding plural tag is absent |
| `track`/`tracknumber`, `disc`/`discnumber` | Positive `N` or `N/Total`; total cannot be smaller than the number |
| `year`, `date` | Four-digit year; date accepts `YYYY`, `YYYY-MM` or `YYYY-MM-DD`; supplied year and date must agree |
| MusicBrainz recording/track, album, release-group and artist identifier tags | Valid UUID-shaped identifiers, stored under the corresponding supported provider keys |

Tag matching is case-insensitive with deterministic alias-collision rejection.
Scalar artist, album-artist and composer punctuation is never guessed to separate
people. Explicit plural lists retain first occurrence order, remove exact
duplicates, reject empty members and allow at most 64 entries. Accepted text is
bounded to 1,024 UTF-8 bytes per value and 4,096 bytes across supported text;
invalid facts do not become partial accepted metadata. This remains an
audio-only extraction path; an attached cover is not an ordinary video stream.
Only a complete date produces PremiereDate; partial dates do not invent a
missing month or day.

Composers create persistent Person relationships with `Type: "Composer"`;
DTOs use real stored IDs. Album publication still requires a complete accepted
member snapshot. Explicit album-artist groups require exact member consensus;
all-missing album artists may use the existing uniform-track-artist fallback.
Genre and composer values form an ordered member union. Album dates and
MusicBrainz album identifiers require consensus instead of selecting an
arbitrary member. Source precedence, manual overrides/locks and failure
preservation remain the existing metadata contract.

| Route relative to `/emby` | Current scope |
| --- | --- |
| `GET /Artists`, `/Artists/{Name}` | Current authorized music-artist associations; name detail also resolves an album-artist-only identity |
| `GET /Artists/AlbumArtists`, `/Artists/AlbumArtists/{Name}` | Album-artist role; `/AlbumArtists` and its name detail are aliases |
| `GET /MusicGenres`, `/MusicGenres/{Name}` | Authorized music genre relationships, existing Genre identity with `Type: "MusicGenre"` |

Lists return `{Items, TotalRecordCount}`, apply current source visibility and
supported filters before counting/paging, and use independent entity favorites.
The native administrator artwork browser uses `GET /admin/v1/music/artists`
with `Role=Artist|AlbumArtist`, and `GET /admin/v1/music/genres`; both return
`{Items, TotalRecordCount, StartIndex, Limit}`. Native list options are
`SearchTerm`, `ParentId`, `IsFavorite`, `StartIndex`, and `Limit` (default 25,
maximum 200). This does not expose arbitrary upstream artist families such as
Prefixes, InstantMix or artist Similar.

Native metadata editing adds Album, Artists and AlbumArtists for supported
music items, plus Audio track/disc numbers, within the existing revision,
override, lock and reset model. It does not edit embedded file tags. See the
[metadata API](../api/admin-metadata.md). Album edits change the saved album tag,
not an Audio item's physical AlbumId/AlbumRef or the effective album name;
edit the MusicAlbum Name to change that display name. MusicBrainz provider keys
normalize case and reject conflicting aliases. Accepted recording/release-group
IDs feed the existing provider path's known-ID lookup. Online MusicBrainz integration and
local tag extraction have distinct evidence; provider-specific online acceptance
remains deferred.

Source: [tag extraction](../../internal/media/music_metadata_extended.go),
[source preservation](../../internal/library/metadata_music_source.go),
[music entity queries](../../internal/library/music_entities.go), and
[route adapters](../../internal/server/music_entities.go).
Migration [0039](../../internal/database/migrations/0039_music_metadata.sql)
updates automatic metadata composition for Audio numbering while preserving
historical rows and manual controls. The selected upgrade/recovery scopes passed
within the phase 3 evidence and PostgreSQL profile boundaries.
The first source03 library run exposed missing music activity field mapping and
allowlist support. The repair adds typed Album/Artists/AlbumArtists activity
fields and forward [migration 0041](../../internal/database/migrations/0041_music_activity.sql),
preserving frozen 0036 through 0040 and the schema40 baseline. Source06's selected
database/activity and music/navigation repair scopes passed; later composed
server, real UI, backup/recovery and resource closeout are recorded separately
in the [phase 3 record](amd-media-phase3-20260919.md).

## Historical music probe 2 contract and evidence

This increment indexes the observed audio format tags `title`, `album`,
`artist`, and `album_artist`. It addresses the original client's album initialization path with
real source metadata and persistent relationships. It does not establish
complete music-client compatibility. The controlled source15 scan and the
[source18 original-client MP3/FLAC runs](verification-m3e-source18-audio.md)
now establish the core playback and persisted-history journeys. MP3 retains
its original Home harness failure; FLAC completed Home. Auxiliary Similar and
ThemeMedia errors and broader music behavior remain open.

## Accepted source facts

The existing descriptor-bound ffprobe call already requests format tags.
`Info.EmbeddedMusic` now stores inspected facts with music metadata version 2.
The technical probe version stays at 6: a music metadata update must not make
already valid video or audio sources unavailable for playback. Audio scans
check the music version separately and refresh old or missing music facts.
An absent object is an unextracted cache. Probe version 1 remains decodable
but predates `album_artist` extraction, so an audio scan refreshes it before
treating the track as a complete current album member. Version 2 with empty
fields is an inspected source without the four supported tags.

Only audio sources without an ordinary video stream use this extraction;
attached artwork does not turn an audio file into video. Supported tag names
are ASCII case-insensitive. Equal repeated values are accepted; conflicting
values, wrong types, invalid Unicode, controls, or values above 1,024 bytes
fail extraction without publishing invented replacement values. The four
decoded values together are bounded to 4,096 bytes. Unknown tags are ignored.
Artist remains one exact scalar: commas, semicolons, slashes, and other
punctuation are not guessed to be multiple-artist separators. `album_artist`
is likewise one exact observed scalar; `albumartist` and `album artist` are
not aliases. Composer, track, and disc tags remain outside this increment.

Audio files retain their physical item IDs, paths, and parents. An accepted
title supplies the automatic display name before the filename fallback.
The existing local-NFO and administrator precedence remains: local name wins
over embedded name, and existing locked values and overrides still apply.

## Music sources and album publication

The accepted source is stored separately from NFO metadata in
`item_metadata_state.music_source`. Its versioned JSON contains an optional
Name and Album, plus Artists and AlbumArtists string arrays. This persisted
source format stays at version 1, independently of music probe version 2;
existing version 1 sources remain readable during scans, edits, and recovery.
A canonical hash
participates in the metadata source key. Embedded-only sources must reach
automatic, effective, and association synchronization even when no NFO exists.
Native metadata edits retain this source and cannot silently erase its music
relationships. The native editable-field list is unchanged.

Albums remain physical directory groups. After a root is completely read,
accepted audio members are aggregated within the same library, stopping at a
nested MusicAlbum. All members must have inspected music facts. A nonempty
album label shared by every member supplies the album's automatic name;
otherwise the physical directory fallback remains. When every accepted member
has the same nonempty `album_artist`, that explicit value supplies AlbumArtists
even when track artists differ. If all members omit it or contain only blank
values, the previous fallback remains: one nonempty track artist shared by
every member supplies the album-artist relationship. Partial explicit values
or conflicting explicit values produce an empty AlbumArtists array; a uniform
track artist must not hide contradictory album-artist facts. Exact spelling
and whitespace are preserved when comparing nonblank values. This is a Goby
aggregation policy; the reference evidence does not establish every missing
or conflicting-tag combination. Missing or mixed values never produce an
invented Unknown Artist or an arbitrary first artist. Artists retain the
ordered union of real member artist values.

Each album publishes its source, automatic/effective metadata, and
relationships in one owned transaction after the root succeeds. Member
failures, unread facts, or old version 1 probe members preserve the previous
album source. Publication is
per album, matching existing per-file scan acceptance: cancellation retains
already committed albums as accepted facts and leaves unprocessed albums
unchanged. Both old and new album parents are refreshed after accepted moves.
Unchanged sources do not rotate IDs or rewrite metadata revisions. Sources
are bounded to 1 MiB and collections to 1,024 entries; overflow does not
silently truncate the artist set.

## Persistent identities and reads

MusicArtist uses the existing catalog entity registry's persistent numeric
identity and name normalization. Artist and AlbumArtist are separate
association roles sharing the same identity. A bounded credit group forms
part of the relationship key; legacy free-form credit text alone cannot
create a music relationship. Empty or orphaned identities are not returned
merely because a registry row exists.

MusicAlbum and Audio ArtistItems use persisted Artist relationships.
MusicAlbum AlbumArtists use its accepted AlbumArtist relationships. Audio
stores its own explicitly observed `album_artist` relationship and prefers
that relationship in its DTO. With no own AlbumArtists, it inherits its real
physical album's accepted relationship. Audio AlbumId and Album continue to
reference the nearest same-library physical album. The direct
ParentId is preserved. An individual performer in a mixed album does not
automatically become the album artist. Composer collections remain empty.

Numeric artist item details require a currently visible associated source.
ArtistIds and AlbumArtistIds filter real roles. AlbumArtistIds uses the same
own-before-physical-album source for Audio and the corresponding music types;
an own relationship to B cannot fall back to parent A merely because A was
requested. AlbumIds follows the same
physical album relation as the Audio DTO. ExcludeItemIds excludes actual
items. These predicates apply together with current library permissions
before totals and pagination. They do not manufacture artists or return
unfiltered rows for an unmatched identifier. Artist metadata editing,
favorites, and the full artist endpoint family are not implied by numeric
identity navigation.

The [retained original-client music queries](m3e-reference-music-navigation-contract.json)
establish AlbumArtistIds with MusicAlbum and ExcludeItemIds, and AlbumIds with
MusicVideo. MusicVideo is recognized as a catalog filter; this does not add
music-video scanning or playback workflows. AlbumIds follows physical
same-library album ancestry, not an inferred match between arbitrary tag
strings. ArtistIds is supported from the pinned API model, but was absent from
this navigation capture.

The captured related-item sort is
`ProductionYear,PremiereDate,SortName` with
`Descending,Descending,Ascending`. Audio navigation also requests DatePlayed
and PlayCount descending. Ordered sort fields are validated and applied to
effective metadata or the selected user's stored item history before paging.
Unknown metadata sorts last; ties retain a stable item-ID order. This specifies
Goby's ordering boundary and does not infer unobserved folder-history rules.

ListItemIds is a recognized boundary guard, not membership support. Ordinary
Items queries first apply every implemented condition in the current ACL and
catalog snapshot. A zero candidate count proves that the membership
intersection is empty, so the response can truthfully return zero items and
zero total. Any nonempty candidate set returns HTTP 501 `unsupported_filter`
without items or a membership total. Limit zero and out-of-range pages cannot
bypass this check. Latest, Resume, NextUp, and entity-list paths reject this
unimplemented predicate explicitly. No list membership table or positive
membership behavior is introduced by this increment.

## Migration and evidence boundary

Migration 25 adds the accepted music-source column and bounded role groups,
allows MusicArtist identities, and extends association synchronization. It
does not rewrite migration 24 or infer tags for old items. Existing rows
receive only their declared default source and legacy group values; actual
metadata is populated through a later scan. Catalog 25 must be exported from
the trusted PostgreSQL 17 migration path, with catalogs 23 and 24 retained for
older archives. Restore and candidate-upgrade checks must preserve historical
data while allowing only the declared new defaults before scanning.

The retained fixture generator supplied real artist, album, and title tags
for MP3 and FLAC. The reference album returned real ArtistItems and
AlbumArtists, while Goby's old album omitted them. Adding empty collections
changed the caught client error from an undefined array access to an
undefined Name access; that partial projection was not successful album
acceptance. The real metadata and identity chain still requires remote tests
and a subsequent original-client run, without response substitution or
fabricated fixture identities.

The later [positive music capture](m3e-reference-auxiliary-positive-music.json)
and [artist-role controls](m3e-reference-auxiliary-positive-music-roles.json)
include a mixed physical album whose tracks have ArtistA and ArtistB while
both explicitly contain `album_artist=ArtistA`. Those observed relationships
motivate this extraction change. They do not by themselves define Similar
ranking, role filtering, or all missing-tag cases. The production increment
requires remote parser, scan-transaction, DTO, and query verification; no
schema migration or historical catalog rewrite is part of it.
