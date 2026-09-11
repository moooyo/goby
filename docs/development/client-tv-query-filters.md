# Original-client television query filters

The original Emby Web 4.9.5.0 Series page issued the same boolean query values
against the frozen Goby source11 fixture and the isolated reference. The safe
capture is retained in
[m3e-goby-av-source11-summary.json](m3e-goby-av-source11-summary.json), including
its `reference_tv_boolean_control` section for `reference-av-tv-10`. The earlier
[reference TV summary](m3e-reference-tv-summary.json) records the successful
reference UI journey and its cross-season episode list; it does not contain the
later boolean values.

| Original request | Observed values on both servers |
| --- | --- |
| `GET /Shows/{Id}/Seasons` | `IsSpecialSeason=false` |
| Main Series `GET /Users/{UserId}/Items` | `Recursive=true`, `IsFolder=false`, `IsStandaloneSpecial=false` |
| Specials `GET /Users/{UserId}/Items` | `Recursive=true`, `IsFolder=false`, `IsSpecialEpisode=true` |
| `GET /Shows/{Id}/Episodes` | `IsStandaloneSpecial=false` |

Goby previously discarded the folder and special-item flags. The recursive
Series query could therefore return season folders among episode cards, and
the separate Specials query could repeat the ordinary episodes. This was a
query selection failure, not evidence that the catalog needed different rows.

## Implemented selection

`library.Query` retains three optional booleans. An absent flag adds no
predicate, and an explicit `false` compares against the entire classification:

| Field | Catalog classification |
| --- | --- |
| `IsFolder` | `i.is_folder` |
| `IsSpecialSeason` | `i.type = 'Season' AND i.index_number = 0` |
| `IsSpecialEpisode` | `i.type = 'Episode' AND i.parent_index_number = 0` |

Each present classification is compared with its parameterized boolean value.
All predicates combine with the existing parent scope, item types, current
library authorization, and other query filters. A false special-episode flag
does not implicitly remove movies, videos, folders, or other item types.
Ordinary folders and non-episode media with default zero indexes are not
special seasons or special episodes.

The shared `itemQuerySQL` filter is used for both `count(*)` and the paged item
selection. No DTO array is filtered after paging, and no special-item response
is hard-coded empty. Existing latest-item queries with absent flags retain
their previous predicates. Next-up keeps its separate query and ordering.

## Evidence and remaining boundary

The [pinned SDK export](../sources/emby-sdk-openapi.snapshot.json), revision
`bdd0dd7c0801f6e069dff2795d80cddae6f91791`, exposes `IsFolder` and
`IsSpecialSeason` as boolean query parameters on item and Shows queries. Its
`Api.BaseItemsRequest` schema declares `IsSpecialEpisode` and
`IsStandaloneSpecial` as separate booleans. The source provenance is recorded
in [the source register](../sources/README.md).

The numbered special classifications above use Goby's existing catalog model:
the scanner retains the `SxxExx` season and episode numbers, and NFO parsing
preserves season zero as `Season.IndexNumber=0` or
`Episode.ParentIndexNumber=0`. The current real-client comparison has three
ordinary episodes in seasons one and two, so it establishes the observed
ordinary-versus-special exclusion failure rather than a complete reference
matrix for every special-episode arrangement. Store and HTTP regression
fixtures include nonempty zero-season results to exercise the actual catalog
selection on both boolean branches.

`IsStandaloneSpecial` remains unmodeled. The pinned schema provides its type
but no rule that proves equivalence with `IsSpecialEpisode`; the existing
metadata model also has no independently established standalone-special or
episode-placement facts. This change does not add a guessed predicate,
reinterpret that field, or claim its compatibility. The observed `false`
requests over the ordinary-only fixture do not establish its behavior on
positive special-item cases. That requires additional reference evidence and,
if necessary, explicit catalog facts.

## Verification handoff

Both focused tests passed in the [source12 remote targeted run](m3e-source12-targeted-upgrade.json),
using isolated PostgreSQL databases. The source12 build and protected candidate
replacement also passed. The [fresh original-client TV flow](verification-m3e-goby-source12-tv.md)
then completed over the ordinary-only fixture, preserving user state and
verifying logout; its auxiliary route/page errors and standalone-special
limitations remain recorded separately.
The focused selection is:

```sh
go test ./internal/library ./internal/server -run 'TestQueryTVFilters|TestHTTPTVFilters' -count=1
```

The tests cover real positive special rows, both folder branches, typed
negation, combined predicates, authorization before pagination, filtered total
counts, and the original ordinary-only Series query shape. A passing test run
does not replace a fresh original-client comparison after deploying the next
candidate.
