# Embedded music metadata and artist identities

This increment indexes the observed audio format tags `title`, `album`, and
`artist`. It addresses the original client's album initialization path with
real source metadata and persistent relationships. It does not establish
complete music-client compatibility. The controlled source15 scan and the
[source18 original-client MP3/FLAC runs](verification-m3e-source18-audio.md)
now establish the core playback and persisted-history journeys. MP3 retains
its original Home harness failure; FLAC completed Home. Auxiliary Similar and
ThemeMedia errors and broader music behavior remain open.

## Accepted source facts

The existing descriptor-bound ffprobe call already requests format tags.
`Info.EmbeddedMusic` now stores inspected facts with music metadata version 1.
The technical probe version stays at 6: a music metadata update must not make
already valid video or audio sources unavailable for playback. Audio scans
check the music version separately and refresh old or missing music facts.
An absent object is an unextracted cache; version 1 with empty fields is an
inspected source without the supported tags.

Only audio sources without an ordinary video stream use this extraction;
attached artwork does not turn an audio file into video. Supported tag names
are ASCII case-insensitive. Equal repeated values are accepted; conflicting
values, wrong types, invalid Unicode, controls, or values above 1,024 bytes
fail extraction without publishing invented replacement values. The three
decoded values together are bounded to 3,072 bytes. Unknown tags are ignored.
Artist remains one exact scalar: commas, semicolons, slashes, and other
punctuation are not guessed to be multiple-artist separators. Raw
album_artist, composer, track, and disc tags are outside this increment.

Audio files retain their physical item IDs, paths, and parents. An accepted
title supplies the automatic display name before the filename fallback.
The existing local-NFO and administrator precedence remains: local name wins
over embedded name, and existing locked values and overrides still apply.

## Music sources and album publication

The accepted source is stored separately from NFO metadata in
`item_metadata_state.music_source`. Its versioned JSON contains an optional
Name and Album, plus Artists and AlbumArtists string arrays. A canonical hash
participates in the metadata source key. Embedded-only sources must reach
automatic, effective, and association synchronization even when no NFO exists.
Native metadata edits retain this source and cannot silently erase its music
relationships. The native editable-field list is unchanged.

Albums remain physical directory groups. After a root is completely read,
accepted audio members are aggregated within the same library, stopping at a
nested MusicAlbum. All members must have inspected music facts. A nonempty
album label shared by every member supplies the album's automatic name;
otherwise the physical directory fallback remains. A consistent single real
artist across all members supplies the album-artist relationship. This is an
explicit Goby aggregation policy, not a claim that an album_artist tag was
present. Missing or mixed values never produce an invented Unknown Artist or
an arbitrary first artist. Artists retain the ordered union of real member
artist values.

Each album publishes its source, automatic/effective metadata, and
relationships in one owned transaction after the root succeeds. Member
failures or unread facts preserve the previous album source. Publication is
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
AlbumId and Album reference its nearest same-library physical album, and
Audio AlbumArtists inherit that album's accepted relationship. The direct
ParentId is preserved. An individual performer in a mixed album does not
automatically become the album artist. Composer collections remain empty.

Numeric artist item details require a currently visible associated source.
ArtistIds and AlbumArtistIds filter real roles; AlbumIds follows the same
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
