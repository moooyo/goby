# Series display order

Series ordering has an independent revision and native administrator contract.
It does not share the metadata draft revision. Every mutation requires the
administrator cookie, the current CSRF token, and fresh administrator
authorization inside the owned catalog transaction.

## Native endpoints

All paths below are relative to `/admin/v1`.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/series/{id}/display-order` | Read the saved order, source identity, coverage, and mapping. An unconfigured series has revision `"0"` and `Aired` order. |
| GET | `/series/{id}/display-order/groups` | List this series' supported TMDB DVD and Absolute groups. The series must have a TMDB identifier and the provider must be enabled and configured. |
| GET | `/series/{id}/display-order/groups/{group}` | Preview one explicitly selected group, its mapping digest, the current order revision, and catalog/expected-episode coverage. This performs no persistent write. |
| PUT | `/series/{id}/display-order` | Save `Revision`, `DisplayOrder`, `GroupId`, and `MappingSHA256`. `Aired` requires empty group and digest values. Alternate orders require the exact digest from a successful preview. |

`DisplayOrder` is `Aired`, `Dvd`, or `Absolute`. Revision values are canonical
decimal strings, including values above JavaScript's safe integer range. The
server refetches an alternate group and verifies its digest before committing.
A changed group produces a conflict instead of applying an unreviewed mapping.

The provider group must belong to the series' TMDB group list. A group for a
different show cannot be imported by supplying its identifier. Multiple groups
with the same order remain distinct choices; the first result is never selected
automatically. Requests use fixed official provider hosts, bounded responses,
and the existing credential and network policy.

## Identity and ordering

TMDB episode-group details supply the original aired season/episode pair and
provider episode identity. Group and episode ranks determine the new display
sequence. They are never interpreted as filenames or written over canonical
catalog numbers.

The selected sequence controls series/season episode browsing with the default
or episode-number sort, Next up, and the episode playback queue. Explicit date,
rating, or personal sorts continue to express the requested sort. Changing an
order preserves item IDs, paths, physical parents, user playback progress, and
the administrator-imported expected episode authority. Series item detail and
the `DisplayOrder` field projection expose the selected value on the compatible
API.

Activation requires an unambiguous mapping for every physical and active
expected episode. The preview reports unmapped episodes and saving an incomplete
mapping leaves all prior facts unchanged. The mapping is limited to 2,000
provider episodes and is replaced atomically.

A later scan or roster edit can introduce unmapped episodes. They remain
explicitly unknown and appear after mapped episodes, including descending
sorts. They do not silently fall back to aired-order numbering. The management
panel reports incomplete coverage and allows a refreshed complete group to be
selected. Changing the series' TMDB identity also invalidates the old source:
`SourceMatches` becomes false and its old ranks are not reused. Returning to
`Aired` requires no provider connection and removes the active mapping.

This capability orders correctly identified catalog episodes. It does not
guess the identities of DVD-numbered files, rename media, infer a missing
episode from a numbering gap, or replace local expected-episode provenance.

## User interaction

The series metadata sheet contains a separate episode-order section. It owns
its own draft, source preview, and save operation. Unsaved metadata must be
resolved before editing the order. The editor combines both navigation guards
and does not submit one form when the other save button is activated.

A lost or invalid save response retains the order draft and blocks another
write until a read confirms the current saved state. The browser does not
automatically replay the mutation.

## Sources and validation scope

- [TMDB series episode groups](https://developer.themoviedb.org/reference/tv-series-episode-groups)
- [TMDB episode-group details](https://developer.themoviedb.org/reference/tv-episode-group-details)
- [Emby item metadata update and DisplayOrder](https://dev.emby.media/reference/RestAPI/ItemUpdateService/postItemsByItemid.html)

Focused provider, catalog, and browser regression tests cover this contract.
Their execution results belong to the task's verification receipt; the presence
of test sources alone does not establish a passing run.
