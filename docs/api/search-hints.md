# Search hint adapter

Goby implements the authenticated legacy search adapter at
GET /emby/Search/Hints. The compatibility namespace also accepts the root alias
and case-insensitive route literals. Query parameter names retain their declared
case. This adapter returns SearchHints and TotalRecordCount, not Items.

The retained 4.1.1.0 OpenAPI export declares Search/Hints in
[emby-openapi.snapshot.json](../sources/emby-openapi.snapshot.json). The pinned
4.9.5.0-associated SDK export does not declare this route. The controls, ranking,
string identities, and extensions below are Goby's explicit adapter contract.
They do not establish that an original Emby Web version invokes this endpoint,
or that every third-party client interprets all optional hint fields.

## Selection and pagination

| Control | Goby behavior |
| --- | --- |
| SearchTerm | Required exactly once; valid UTF-8, at most 1024 bytes, without NUL. Surrounding Unicode whitespace is trimmed. An empty result after trimming returns an empty array and zero count after authentication and catalog authorization. |
| IncludeMedia | Omitted means true. Selects ordinary physical/catalog items, including folders and visible playlists/collections. CollectionFolder roots, attachment resources, missing-episode facts, and unsupported external/live items are outside this population. |
| IncludePeople, IncludeGenres, IncludeStudios, IncludeArtists | Each omitted flag means true. An entity requires at least one currently authorized ordinary source item. Artist and album-artist credits qualify MusicArtist only on Audio, MusicAlbum, or MusicVideo items. Orphan entities and untyped/free-form music credits do not qualify. Tags are not a hint category. |
| IncludeItemTypes | A comma-delimited or repeated list of the supported result types. Intersects the category flags; it does not re-enable a disabled category. |
| ExcludeItemTypes | Removes the indicated types after inclusion. Exclusion wins when a type occurs in both lists. |
| MediaTypes | Audio or Video, comma-delimited or repeated. Scopes physical matches and the authorized source membership that qualifies an entity. An artist associated with matching Audio may still appear without a MediaType field of its own. |
| IsMovie, IsSeries | Optional Boolean predicates on each result's type. Explicit false excludes that type; explicit true keeps only that type. All supplied predicates intersect. |
| StartIndex | Default zero; an integer from zero through 2147483647. A page beyond the total returns an empty array and the same total. |
| Limit | Default 20; an integer from zero through 1000. Zero is a count-only request. Excessive values are rejected, not silently clamped. |
| UserId | The existing catalog subject selection. An ordinary login cannot borrow another user's scope. An application key may select a target's current catalog policy; omitting the target retains the key's own server scope. It never borrows personal playback preferences. |
| EnableImages | Optional Goby projection control; defaults true. False omits image tags, URLs, and aspect ratio. |

Supported result types are Folder, Movie, Series, Season, Episode, Video, Audio,
MusicAlbum, MusicVideo, Playlist, BoxSet, Person, Genre, Studio, and MusicArtist.
Names in type/media lists are case-insensitive and duplicates are removed.
There are at most 32 supplied values per type/media control. Scalar duplicates,
empty scalar Booleans/pagination, an explicitly empty type list, unsupported
types, and supplied IsNews/IsKids/IsSports controls return 400 invalid_input.
The last three classifications have no admitted source facts, including for
explicit false. Unrecognized unrelated compatibility parameters are ignored.

The name search uses PostgreSQL ILIKE with escaped literal percent, underscore,
and backslash characters. Latin case folding follows the database's configured
Unicode locale. Chinese text is matched literally; there is no pinyin,
transliteration, token expansion, accent removal, or fuzzy search.

All physical and entity matches are combined before ranking, counting and
pagination. Rank is case-folded exact name, then name prefix, then name
substring. Ties use lower(name), original name, owner kind, result type and
string ID, in that order, with PostgreSQL C collation for a fixed byte order.
Entity sorts before Item when earlier tie keys agree. Decimal IDs sort as
strings. Duplicate names from different owners remain separate hits. Count,
page, metadata, source membership and image projection share one repeatable-read
transaction with the current Subject authorization.

## Hint fields and identity

Each hint has string Id and ItemId, Name, MatchedTerm, Type and IsFolder.
MatchedTerm is the full matching catalog/entity name. The historical export's
numeric Id, ItemId and AlbumId declarations are not used to coerce opaque Goby
IDs or to fabricate new numeric identities.

Physical hints can include the following existing, source-backed facts:

- ProductionYear from effective metadata.
- IndexNumber for Season, Episode or Audio, and ParentIndexNumber for Episode or
  Audio.
- MediaType and RunTimeTicks when persisted media facts exist.
- Series and SeriesId from an authorized television parent projection.
- Artists and AlbumArtist from persisted music associations; a track without its
  own album-artist group may inherit its authorized physical album's group.
- Album and string AlbumId only for the authorized physical album relationship.

No server filesystem paths, credentials, MediaSources, synthetic song/episode
counts, broadcast dates, recommendation scores, or invented media facts are
returned.

The following fields are Goby extensions:

| Field | Meaning |
| --- | --- |
| GobyReference | An object with Kind equal to Item or Entity and the original string Id. This is the unambiguous identity; equal wire IDs in different kinds are distinct. |
| GobyNavigationUrl | A relative /emby URL for the selected owner, with path segments escaped and optional UserId preserved. It contains no authentication token. |
| PrimaryImageUrl, ThumbImageUrl, BackdropImageUrl | A relative URL for index zero of an existing image belonging to this exact owner, with UserId and the source tag. Omitted when absent. |

Item navigation uses /emby/Items/{Id}. Entity navigation uses the authenticated
Goby extension GET /emby/Search/Entities/{Id}, which resolves a canonical
positive entity ID directly. It avoids both numeric physical-ID collisions and
artist names reserved by literal routes, such as AlbumArtists or InstantMix.
The entity response uses the existing authorized entity detail projection and
adds GobyReference and GobyNavigationUrl.

Image metadata is selected in the same authorized search snapshot. Item images
retain managed/provider/sidecar/embedded precedence; numeric item IDs never
trigger an entity-image merge. Entity images use their managed owner or the
authorized genre collage manifest. Image URLs use existing item image routes or
typed entity-family name image routes. Each later image/detail request
revalidates its current authority independently; a hint is not a capability.

Legacy PrimaryImageTag, ThumbImageTag/ThumbImageItemId and
BackdropImageTag/BackdropImageItemId are also returned when the implicit item-ID
image route is unambiguous in the same subject snapshot. For an entity whose ID
collides with a visible direct physical item, these implicit references are
omitted while its explicit image URLs remain available. An inaccessible
physical collision does not disclose a collision bit. PrimaryImageAspectRatio
is emitted only with positive source dimensions.

## Verification scope

The implementation includes source tests for mixed rank/count/page, Chinese and
literal matching, category precedence, typed collisions, source authorization,
image ownership, empty/count-only reads, application target/revocation, and
HTTP navigation/image consumption. Execution belongs to the consolidated
phase-3 verification after the phase's code freeze. No pass or original-client
compatibility claim follows merely from these test sources.
