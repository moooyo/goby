# Television metadata facts and standalone specials

Goby retains series status, end dates and episode placement as source metadata,
with the existing manual override and locked-value layers. Unknown facts stay
absent in compatibility item DTOs and appear as `null` in the complete native
metadata editor values. No status, end date or placement is inferred from a
filename, an episode gap or a missing media file.

## Source evidence and scope

The pinned Emby SDK revision is
[`bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/tree/bdd0dd7c0801f6e069dff2795d80cddae6f91791).
Its `Api.BaseItemsRequest` declares `IsStandaloneSpecial` separately from
`IsSpecialEpisode`, but gives no predicate. The official
[special ordering guide](https://github.com/EmbySupport/Emby.Docs/blob/ab6918100411391e027fa18f30851d99485c28e0/Ordering-TV-Specials.md)
distinguishes S00 special identity from the explicit metadata that places a
special in the regular-season timeline.

The historical first-party Emby
[EpisodeNfoParser](https://github.com/MediaBrowser/Emby/blob/ed925c3367a47ea32d43e664a7fb3f5ddafbc4b7/MediaBrowser.XbmcMetadata/Parsers/EpisodeNfoParser.cs)
maps `airsbefore_season`, `airsafter_season` and `airsbefore_episode` to three
nullable integer facts. The first-party Jellyfin
[EpisodeNfoParser](https://github.com/jellyfin/jellyfin/blob/50866380c95758244f79bd4fbb01b9474e80bd81/MediaBrowser.XbmcMetadata/Parsers/EpisodeNfoParser.cs)
also recognizes the `displayseason`, `displayafterseason` and `displayepisode`
aliases. Goby admits those aliases, rejects conflicting values, and retains
physical season and episode numbering separately.

The fixed SDK's
[SeriesStatus](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/reference/pluginapi/MediaBrowser.Model.Entities.SeriesStatus.html)
contains `Continuing` and `Ended`. The historical Emby
[SeriesNfoParser](https://github.com/MediaBrowser/Emby/blob/ed925c3367a47ea32d43e664a7fb3f5ddafbc4b7/MediaBrowser.XbmcMetadata/Parsers/SeriesNfoParser.cs)
reads `status`, and its
[BaseNfoParser](https://github.com/MediaBrowser/Emby/blob/ed925c3367a47ea32d43e664a7fb3f5ddafbc4b7/MediaBrowser.XbmcMetadata/Parsers/BaseNfoParser.cs)
reads `enddate`. Goby uses its existing strict date/RFC3339 parser and UTC
normalization, rather than importing a server-local timezone assumption.

The standalone classification is Goby's explicit interpretation of the
documented special-versus-placement distinction: a season-zero Episode is
standalone if neither season-placement field is positive. This is not a
capture of the proprietary server's internal SQL. The current SDK also exposes
`SpecialEpisodeNumbers.SortParentIndexNumber` and `SortIndexNumber`, with a
legacy conversion method whose implementation is not published there. This
increment does not guess that encoding or claim all upstream ordering parity.

## Accepted fields

| Field | Source and native editing | Value |
| --- | --- | --- |
| `Status` | `tvshow/status`; editable for Series | `Continuing`, `Ended`, or null; accepted case variants normalize to the canonical spelling |
| `EndDate` | `enddate`; editable for supported metadata items | Date or RFC3339 source value, normalized to UTC; null means unknown |
| `AirsBeforeSeasonNumber` | Episode `airsbefore_season` or `displayseason`; editable for Episode | Null or integer 0 through 2147483647 |
| `AirsAfterSeasonNumber` | Episode `airsafter_season` or `displayafterseason`; editable for Episode | Null or integer 0 through 2147483647 |
| `AirsBeforeEpisodeNumber` | Episode `airsbefore_episode` or `displayepisode`; editable for Episode | Null or integer 0 through 2147483647 |

All five fields use the existing metadata API: `PUT
/admin/v1/items/{id}/metadata` replaces `Overrides` and `LockedFields` with a
current `Revision`. Explicit null clears the selected fact; removing that
override restores the automatic source value. Existing locks, rescans,
authorization checks, conflict detection and catalog notifications apply.
No new structural item columns or metadata backfill are required. Historical
metadata JSON without the new keys continues to decode with unknown values.
The activity field allowlist must admit the five new field names.

`Status` and `EndDate` follow the normal `Fields` or detail projection. Episode
placement facts and the derived `IsStandaloneSpecial` also follow requested
fields/detail. These facts do not change physical ancestry or introduce a new
playback queue sort algorithm. In particular, `AirsBeforeEpisodeNumber` alone
cannot identify a regular season and does not remove a special from the
standalone group.

For `/Items`, user Items, and Shows Episodes queries, the optional
`IsStandaloneSpecial` boolean joins current authorization, parent scope and
other filters in shared SQL before both counting and pagination. False negates
the complete typed classification. It does not exclude unrelated media types.

## Verification inventory

The added source tests cover legacy/alias placement parsing, status and UTC
end dates, unknown facts, invalid bounds and conflicts. Store tests cover
positive and negative standalone cases, regular and unrelated items,
authorization, pages and filtered totals, and clearing/restoring placement
through the real metadata store. DTO tests cover requested fields and unknown
omission. The phase browser journey exercises the native controls against
scanned NFO fixtures. These are verification targets; execution results belong
to the consolidated remote phase record.
