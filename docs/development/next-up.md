# Next-up episode queries

`GET /emby/Shows/NextUp` returns an Items/TotalRecordCount envelope. Supported
filters are UserId, SeriesId, ParentId, StartIndex, and Limit. UserId defaults to
the authenticated account; another ordinary user's state is inaccessible.
Fields, EnableImages, ImageTypeLimit, EnableImageTypes, and EnableUserData use the
existing item projection. The default page size is 100, capped at 1,000; an
explicit HTTP Limit=0 returns no items while preserving the total count.

## Series-directed behavior

With SeriesId, Goby returns all unplayed episodes after the highest watched
episode in season/episode order. A series with no watched episode returns an
empty result. A later watched episode advances the cursor past earlier gaps.
Pagination and TotalRecordCount apply to the complete remaining sequence, not
one item per series.

The isolated Emby 4.9.5.0 captures cover both two-second episodes and a separate
series with three ten-minute episodes. Both return S01E02 and S02E01 after S01E01
is marked watched. A separate short-episode control marks only S01E02 and returns
S02E01, leaving the earlier S01E01 gap behind the cursor. Full rewatching behavior, mixed played
and partially watched episodes, and multiple versions need additional evidence.

## Global behavior and evidence boundary

Without SeriesId, Goby returns one continuation per series with playback history:
the first unplayed episode after its highest watched episode. When only incomplete
playback history exists, it returns the first unplayed episode. Favorite-only
state does not create playback history. Series are ordered by most recent
playback activity, then stable series/item ordering.

This is an explicit Goby design and a remaining reference-compatibility gap.
The reference global query returned no entries throughout the synthetic samples,
including after actual Started/Stopped events, after a 30-second wait, with
ParentId, with ten-minute episodes, and after explicitly declaring Video/Audio
client capabilities. Its SeriesId-directed queries returned
the remaining sequence. These controls did not establish why global results were
empty; duration, immediate caching, media-capability declarations, and manual watched marking alone did not
explain the samples. Goby's global result must not be presented as a verified
match for that reference setup. Real-client and broader library verification
remain necessary before a general NextUp compatibility claim.

## Catalog and authorization boundaries

Permissions, playback history, counts, and page selection share one PostgreSQL
snapshot. ParentId restricts eligible episodes; the cursor can come from an
earlier season of the same series outside that parent. Unauthorized or missing
explicit series/parent IDs return the same not-found result. Playback state
belongs to the selected user, and current library policy filters before counting.

Traversal stays within each library, terminates cycles, and does not fold a
nested Series into its parent Series. Known season/episode numbers sort first;
missing numbers sort last. Zero remains an actual special-season/episode number.
UserData and CanPlay are loaded for the selected page in batches. The query does
not change watched state, create a play session, or perform filesystem I/O.

See the [reference report](../research/reference-server.md) for exact requests
and the [implemented surface](../api/implemented.md) for the rest of the API.
