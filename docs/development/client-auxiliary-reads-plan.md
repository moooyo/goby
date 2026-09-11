# Post-source18 auxiliary client reads

Status: bounded read-only audit complete; contracts and implementation remain
open. This work follows the source18 audio increment. It does not change the
frozen candidate or establish complete Emby compatibility.

## Observed routes

The original client requests `GET /emby/Items/{Id}/Similar` and
`GET /emby/Items/{Id}/ThemeMedia`. Neither route is registered in Goby.
Source18 audio retains their HTTP 404 JSON responses and associated page errors.
User-path and Albums/Artists/Movies/Shows aliases have not been accepted.

The existing reference run
`reference-av-source12-subtitle-diagnostic-01/observation.json` records Similar
HTTP 200 at request indices 255 and 325, and ThemeMedia HTTP 200 at indices 256,
298 and 326. Its Similar keys include Limit, UserId, ImageTypeLimit, Fields,
EnableTotalRecordCount and, at the album stage, ExcludeArtistIds. Values and
bodies were not retained. An earlier masked `/Items/{value}/{value}` request
must not be treated as proof that Similar traffic was absent.

## Similar contracts to capture

Capture owned empty and nonempty result DTOs, counts, self exclusion, target
and candidate type scope, ranking and ties, pagination, total-count suppression,
ExcludeArtistIds, field projection and current access/error boundaries.
The pinned QueryResult_BaseItemDto model defines a shape, not a ranking rule.

Existing item queries provide authorized counting, pagination and DTO assembly.
They provide no similarity scorer or ExcludeArtistIds filter. A generic item
listing or a permanent empty response would not establish this contract.

## ThemeMedia contracts to capture

The [existing movie extras capture](m3e-reference-movie-extras-contracts.json)
observed three result objects: ThemeSongsResult, ThemeVideosResult and
SoundtrackSongsResult. For its single theme-free movie, each had empty Items
and TotalRecordCount zero. Direct enabled reads reported numeric owners 9/9/0;
inherited reads reported 1/1/0; disabled reads reported 0/0/0.

Defaults, individual enable flags, ancestor selection, actual resources and
access errors remain unknown. Resolve the numeric OwnerId contract before
choosing any mapping. A hexadecimal Goby item ID, a synthetic root string or a
copied reference constant is not established evidence.

## Implementation boundary

Authorize the current requested item through the existing Emby subject and
GetItemFor path before returning even an empty result. Apply candidate access
checks before Similar ranking, counting and pagination. Reuse item, image and
user-data projections, and guarded OpenMediaFor delivery for actual themes.
Introduce theme ownership and attachment modeling only after its contract is
captured. Preserve all original media and existing reference state.

The next step is an independently owned, bounded reference-contract capture.
This audit performed no new HTTP requests, login, probe or database mutation,
and did not inspect the reference implementation or client source.
