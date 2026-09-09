# Native administrator metadata management

This document defines Goby's native administrator metadata API and the contract
used by its metadata editor. Edits save a separate administrator layer in the
catalog; they do not modify media files or write NFO files. Reads use the latest
source snapshot already accepted by a library scan, without reading the source
files again.

These routes and durable field locks are a Goby-owned contract. They do not
establish compatibility with Emby metadata mutation, refresh, or reset routes.
The differences, including `SortName` versus `ForcedSortName`, are documented in
[the pinned SDK and isolated reference study](../research/metadata-reference.md).
The completed M5b suite, browser workflow and deployment checks are recorded in
[the verification report](../development/verification-m5b-metadata.md). They do
not complete the broader administrator or client-compatibility milestones.

## Authentication and routes

Every route requires an active native administrator session in the
`goby_session` cookie. An Emby playback token is not an administrator cookie.
The current account must still be enabled and have administrator access.
Administrator catalog access is not filtered by that account's playback library
policy; the list route's library ID defines the requested catalog scope.

Writes also require `X-CSRF-Token`, the configured same-origin policy, and
`Content-Type: application/json`. If supplied, `Origin` must match the configured
public URL; `Sec-Fetch-Site: cross-site` is rejected. Use the CSRF token returned
by the native administrator session API.

| Method and route | Request | Successful response |
| --- | --- | --- |
| `GET /admin/v1/libraries/{id}/items` | Library ID and the list query below | `200`, a bounded item list |
| `GET /admin/v1/items/{id}/metadata` | Item ID; no query parameters | `200`, `MetadataDetail` |
| `PUT /admin/v1/items/{id}/metadata` | Item ID; no query parameters; `MetadataUpdate` JSON | `200`, the resulting `MetadataDetail` |

Path IDs must contain 1 to 256 UTF-8 bytes, with no surrounding whitespace or
control characters. They are opaque identifiers, not paths. Invalid IDs produce
`400 invalid_input`. A missing library or a missing/unsupported item produces
`404 not_found`.

## Library item list

The list includes supported items anywhere within the selected library,
including folders and descendants. It excludes the library's own root item.
It never combines items from other libraries. The library, count, and page are
read from one database snapshot.

All query parameter names and type tokens are case-sensitive. Each parameter
may appear at most once. Unknown parameters, alternate casing, invalid UTF-8,
and malformed query encoding are rejected rather than ignored.

| Parameter | Default | Accepted value |
| --- | --- | --- |
| `SearchTerm` | Empty | At most 1,024 UTF-8 bytes before trimming, without control characters. Surrounding whitespace is trimmed. Matches a case-insensitive literal substring of the current effective item name. |
| `Types` | All supported types | A comma-separated list of distinct exact tokens: `Movie`, `Video`, `Series`, `Season`, `Episode`, `Folder`, `MusicArtist`, `MusicAlbum`, `Audio`. Omitted or empty means all. Spaces around tokens are not accepted. |
| `StartIndex` | `0` | Canonical decimal integer from `0` to `2147483647`. |
| `Limit` | `50` | Canonical decimal integer from `1` to `200`. |

Canonical pagination excludes values such as `+1`, `01`, `1.0`, and whitespace.
Search is not a regular expression or SQL wildcard expression. Results are
ordered by the lowercase effective sort name, then item ID. Offset pagination
uses that order; later scans or edits can change it between requests.

```http
GET /admin/v1/libraries/example-library/items?SearchTerm=Pilot&Types=Episode&StartIndex=0&Limit=50
```

```json
{
  "Library": {
    "Id": "example-library",
    "Name": "Example shows",
    "CollectionType": "tvshows"
  },
  "Items": [
    {
      "Id": "example-episode",
      "LibraryId": "example-library",
      "ParentId": "example-season",
      "ParentName": "Season 1",
      "Name": "Pilot",
      "Type": "Episode",
      "Path": "/media/shows/Example/Season 1/Pilot.mkv",
      "IsFolder": false,
      "IndexNumber": 1,
      "ParentIndexNumber": 1,
      "ProductionYear": 2025,
      "HasOverrides": false,
      "LockedFieldCount": 0
    }
  ],
  "TotalRecordCount": 1,
  "StartIndex": 0,
  "Limit": 50
}
```

`Items` is always an array, including an empty page. `TotalRecordCount` is the
number of matching items before pagination. `IndexNumber` is populated for
Season and Episode summaries; `ParentIndexNumber` is populated for Episodes.
Unsupported or absent numeric summary values are `null`. Missing parent
identity/name is represented by an empty string.

`HasOverrides` and `LockedFieldCount` describe all saved controls, including
inactive controls retained after reclassification. A manual override equal to
the automatic value still counts as an override. Fetch metadata detail before
editing; a list row is not a revision-bearing edit snapshot.

## Metadata detail

GET and successful PUT return the same object with exactly these eleven
top-level properties. There is no additional `Metadata` wrapper.

| Property | Shape and meaning |
| --- | --- |
| `Item` | `{Id, LibraryId, ParentId, ParentName, Name, Type, Path, IsFolder}`. All values are strings except `IsFolder`, a boolean. `Name` is the effective catalog name. Identity, type, hierarchy, and path are read-only context. |
| `Revision` | A positive canonical decimal string, such as `"17"`. Treat it as an opaque concurrency token, not a JavaScript number or a client-incremented counter. |
| `Automatic` | Complete `MetadataValues` from the latest accepted scanner source. It continues to change behind manual values and locks. |
| `Effective` | Complete `MetadataValues` after applying currently active locks and overrides. |
| `Overrides` | A sparse object containing every saved manual override. Absence of a key means there is no manual override for that field. |
| `LockedValues` | A sparse object containing the stored value of every saved lock. Clients cannot write this object directly. |
| `LockedFields` | An array of exact field names whose values occur in `LockedValues`; returned in sorted order. |
| `EditableFields` | An array of fields currently editable and lockable for this item type. Use this response to enable controls. |
| `InactiveFields` | Saved control fields that are currently inapplicable to the item's type. They remain in `Overrides` and/or `LockedValues`, but do not affect `Effective`. |
| `LastEditedBy` | The ID of the last administrator who changed the saved controls, or an empty string when none is recorded. |
| `LastEditedAt` | The last such edit's timestamp in UTC, or `null` when none is recorded. |

`Overrides` and `LockedValues` are objects even when empty. The three field
lists are arrays even when empty. `Automatic` and `Effective` always include all
fifteen properties in the following table. Collections use `[]` or `{}`, never
`null`; nullable scalars retain absence as `null`.

### Metadata values and manual input limits

The limits below govern administrator-supplied values and new locks. Source
values are projections of accepted source metadata, not an invitation to copy
the entire projection into `Overrides`.

| Field | Value and manual input rules |
| --- | --- |
| `Name` | Nonempty string, trimmed, at most 1,024 UTF-8 bytes. |
| `SortName` | Nonempty string, trimmed, at most 1,024 UTF-8 bytes. Independent of `Name`. |
| `Overview` | String, at most 65,536 UTF-8 bytes. Empty string clears it; whitespace is preserved. |
| `OriginalTitle` | String, at most 65,536 UTF-8 bytes. Empty string clears it; whitespace is preserved. |
| `OfficialRating` | String, at most 65,536 UTF-8 bytes. Empty string clears it; whitespace is preserved. This is the content-rating label, not the numeric community score. |
| `ProductionYear` | Integer from `1` to `9999`, or `null` to clear. |
| `PremiereDate` | ISO date `YYYY-MM-DD`, strict RFC3339 timestamp, or `null` to clear. Values are normalized to UTC; the resulting UTC year must be from `1` to `9999`. |
| `CommunityRating` | Finite JSON number from `0` to `10`, or `null` to clear. |
| `ProviderIds` | Object of provider names to identifier strings, with at most 1,024 entries. Use `{}` to clear; rules below. |
| `Genres` | Array of at most 1,024 nonempty strings. Use `[]` to clear; rules below. |
| `Tags` | Array of at most 1,024 nonempty strings. Use `[]` to clear; rules below. |
| `Studios` | Array of at most 1,024 nonempty strings. Use `[]` to clear; rules below. Native studios are names, not `{Name, Id}` objects. |
| `People` | Array of at most 1,024 credit objects. Use `[]` to clear; rules below. |
| `IndexNumber` | Editable and lockable only for an Episode: a non-null integer from `0` to `2147483647`. A Season's number and Audio numbering are read-only. |
| `ParentIndexNumber` | Read-only for every type. Represents an Episode's structural season number when available; otherwise `null`. |

All supplied text must be valid UTF-8 and contain no NUL character. JSON null
is not accepted for string or collection fields. Numeric strings are not
numbers. Neither `Name` nor `SortName` can be explicitly cleared: remove its
override and lock to use its automatic value.

Editing `Name` does not rewrite `SortName`. With no sort override or lock,
`SortName` continues to use `Automatic.SortName`, even when a manual title is
different. To change both, send both fields. This native API has no
`ForcedSortName` property.

Dates returned by the API use UTC timestamps. A date-only input means midnight
UTC. Timestamp input may include a numeric offset and up to nine fractional
second digits; it must describe a valid instant within the accepted UTC year
range. The administrator editor uses a UTC date control and saves an edited
date at midnight UTC.

For `Genres`, `Tags`, and `Studios`, each name is trimmed, must remain nonempty,
and is limited to 65,536 UTF-8 bytes. Exact duplicates after trimming are
removed while retaining first occurrence order. Names containing punctuation
or commas remain single entries; these fields are JSON arrays, not delimited
strings.

Provider keys are trimmed ASCII letters/digits, 1 to 64 bytes. Provider values
are trimmed, nonempty ASCII letters/digits with optional `._:-` separators,
at most 256 bytes. Case-insensitive spellings of `imdb`, `tmdb`, and `tvdb`
normalize to `Imdb`, `Tmdb`, and `Tvdb`. Other provider key casing is preserved.
Keys that normalize to the same provider cannot carry conflicting IDs.

Each manual `People` entry accepts only these exact properties:

| Property | Rule |
| --- | --- |
| `Name` | Required, trimmed, nonempty string, at most 65,536 UTF-8 bytes. |
| `Type` | Required, trimmed, exact enum: `Actor`, `Director`, `Writer`, `Producer`, `GuestStar`, `Composer`, `Conductor`, or `Lyricist`. |
| `Role` | Optional string, at most 65,536 UTF-8 bytes. Empty or omitted means no role; whitespace is preserved. |
| `SortOrder` | Optional integer from `0` to `2147483647`, or `null`. Omitted means `null`; explicit zero is retained. |

Returned credit objects include all four properties. Array order and separate
credits are retained; the same person may appear in more than one role. The
native write shape does not accept entity IDs, image tags, or arbitrary nested
properties. Replacing `People` replaces the complete credit list.

## Update request and value precedence

PUT requires exactly three top-level properties: `Revision`, `Overrides`, and
`LockedFields`. All are required and non-null. `Overrides` must be an object;
`LockedFields` must be an array of distinct strings. Field names are exact and
case-sensitive. Read-only wrapper fields, unknown metadata fields, and unknown
credit properties are rejected.

```json
{
  "Revision": "17",
  "Overrides": {
    "Name": "A manually edited title",
    "SortName": "manually edited title, a",
    "Overview": "",
    "ProductionYear": null,
    "Genres": ["Drama", "Science Fiction"],
    "ProviderIds": {"Imdb": "tt1234567"},
    "People": [
      {"Name": "Example performer", "Type": "Actor", "Role": "Lead", "SortOrder": 0}
    ]
  },
  "LockedFields": ["Name", "Overview"]
}
```

This is a **complete replacement of the saved control sets**, not a patch of
their individual members. `Overrides` is sparse because only manually
controlled fields belong in it, but every existing override to retain must be
included in the next PUT. The same rule applies to `LockedFields`. In the
example, any previous override or lock not included is removed. Each supplied
array or provider object also replaces that field's whole value.

For each currently applicable field, resolution is:

```text
manual override > saved locked value > latest automatic value
```

| Override present | Lock present | Effective source |
| --- | --- | --- |
| No | No | Latest `Automatic` value |
| No | Yes | Saved `LockedValues` value |
| Yes | No | Manual `Overrides` value |
| Yes | Yes | Manual `Overrides` value; the saved lock remains underneath it |

A lock retained from the previous revision retains its exact saved value. A
new lock captures the prospective effective value after this request's
overrides and retained locks have been applied. Thus adding an override and a
lock together captures that manual value. Changing an override while retaining
an existing lock does not recapture the lock.

For example, an automatic overview `A` can be locked as `A`. A later manual
override `B` takes precedence while the retained lock remains `A`. If a scan
then discovers `C`, the response reports automatic `C`, effective `B`, and
locked `A`. Removing only the override reveals `A`. Removing both the override
and lock reveals `C`. Removing only the lock leaves manual `B` in effect.

The editor's **Use automatic** action removes both the override and the lock
for that field. It does not store the currently displayed automatic value as a
new override, and it does not run a scan. To return every field to its current
automatic value, send the current revision with empty control sets:

```json
{"Revision":"18","Overrides":{},"LockedFields":[]}
```

An explicit empty value is still a manual override: `"Overview":""`,
`"ProductionYear":null`, `"PremiereDate":null`, `"CommunityRating":null`,
`"Genres":[]`, and `"ProviderIds":{}` suppress the automatic value. Missing a
key from `Overrides` instead removes the manual layer and inherits the saved
lock, if any, or the automatic value. A lock can also pin a valid empty value.

## Type-specific and inactive controls

The current supported types share the ordinary descriptive fields. Only an
Episode adds editable `IndexNumber`. Changing an Episode number does not move
the item, change its physical parent, or change its season membership. Season
numbering, `ParentIndexNumber`, and Audio indexes cannot be edited or locked.
Type, IDs, media stream indexes, paths, and parent relationships are not writable
metadata fields.

A scan can reclassify a stable item ID. A saved field that is no longer
applicable is retained in `Overrides` and/or `LockedValues` and listed in
`InactiveFields`. Its value is excluded from the effective projection. For
example, an Episode's saved number becomes inactive if the item becomes a
Video.

An update may retain an inactive override unchanged, retain its existing lock,
or remove either saved control. It cannot change the inactive override, add a
new inactive override, or add a lock where no saved inactive lock exists. To
clear it completely, omit the override and remove its name from `LockedFields`.
If a later scan restores the applicable type, any retained controls become
active again. Removed controls do not return. This lets clients round-trip
saved data without applying an obsolete field to the wrong item type.

## Revisions, transactions, and source preservation

Every edit must use the revision from the detail snapshot it was based on.
An accepted change to either saved control set advances the revision and records
the actor and edit time. A normalized no-op PUT returns the current detail
without advancing `Revision` or changing `LastEditedBy`/`LastEditedAt`. Reordering
the lock list is not a new lock capture. Adding an explicit override equal to
the automatic value is a control change, even when the effective value stays
the same.

Scans also advance the revision when the accepted automatic values or source
identity change. This includes NFO content-hash changes even when the projected
values are identical, NFO presence/path changes, and changes to the item's type,
folder status, parent, root, or media path. A visible manual value can therefore
stay the same while its revision changes. Scanner changes do not rewrite the
last administrator edit attribution.

A stale PUT fails with `409 revision_conflict` and commits no edit. Reload the
detail, inspect the latest automatic values and controls, and deliberately
rebuild the intended request. Do not merely replace the revision on an old
payload. If the response is lost or cannot be confirmed, read the detail before
retrying: the write may already have committed.

Saved controls, effective item values, and related catalog entities are updated
in one transaction. Entity synchronization failure rolls back the edit rather
than leaving a partially updated item. The store locks and rechecks the current
administrator account and authentication session, then checks authorization
again before commit, including clock-based session expiry. Authentication at
HTTP entry is not treated as permanent permission to commit.

The automatic source and manual controls are stored separately. Unrecognized
source JSON properties, including large numeric values, are retained internally
without round-tripping through a floating-point generic representation. The
sparse source projection preserves omission/null distinctions and unknown
properties for fields this editor does not control. Unchanged source `People`
entries also preserve nested extensions during unrelated edits or source
refreshes; replacing or locking `People` uses the supported credit projection
and does not promise to copy arbitrary source extensions into that control.
This is semantic preservation, not a promise to retain JSON whitespace or key
order.

No edit rewrites source media, NFO contents, source hashes, or filesystem
hierarchy. Scans keep accepting fresh automatic metadata while saved controls
determine which values remain effective. The API does not expose raw NFO or
unknown source properties as writable fields. Administrator mutation logs record
actor ID, item ID, and resulting revision without metadata values or local
paths.

## Validation and errors

The request body is limited to 1 MiB of UTF-8 JSON; the normalized edit must
also fit within 1 MiB. The decoder accepts exactly one JSON object, rejects
duplicate keys at every object depth, limits nesting to 16 levels, and limits
JSON value nodes to 65,536. Invalid, excessive, or oversized bodies return
`400 invalid_input`; the body size limit does not produce a separate metadata
`413` contract. Unknown write fields are rejected even when their values would
be ignored by a generic JSON decoder.

Errors use the native envelope:

```json
{
  "Error": {
    "Code": "invalid_input",
    "Message": "Check the highlighted metadata fields.",
    "Fields": {"IndexNumber": "Must be an integer from 0 to 2147483647."}
  },
  "RequestId": "example-request-id"
}
```

`Fields` is present for field validation and may identify a value field, a
control set, `Revision`, `Body`, `Id`, `Query`, or a list query parameter. Clients
must also handle an error without `Fields`.

| Status | Code | Meaning |
| --- | --- | --- |
| `400` | `invalid_input` | Invalid ID, query, JSON body, field value, control, or revision syntax. |
| `401` | `authentication_required` | No administrator cookie was supplied. |
| `401` | `invalid_credentials` | The cookie credentials are invalid or no longer active. |
| `403` | `administrator_required` | The resolved account does not have administrator access. |
| `403` | `access_denied` | Store-level administrator/session revalidation denied access. |
| `403` | `origin_denied` | The write did not satisfy the origin policy. |
| `403` | `csrf_invalid` | The write lacked the correct CSRF token. |
| `404` | `not_found` | The requested library or editable item was not found. |
| `409` | `revision_conflict` | Saved metadata or its automatic source changed after the supplied revision. |
| `415` | `unsupported_media_type` | The write was not sent as `application/json`. |
| `500` | `internal_error` | The operation could not be completed or confirmed. Reload before retrying a write. |

## Administrator editor behavior

The library item page searches and pages through the scoped list, then loads a
fresh detail snapshot when opening an item. The editor distinguishes
**Automatic**, **Locked automatic**, and **Manual override** states, displays
the automatic and saved locked values for comparison, and shows a new lock as
pending until save. A manual override can coexist with a lock. Unlocking it
leaves the manual value; **Use automatic** clears both layers.

Controls follow `EditableFields` and `InactiveFields`. Inactive saved values
are shown separately with a remove action. Collection editors use one row per
entry, one identifier per provider, and separate rows for separate credits.
An empty year/date/rating control saves `null`; an empty Episode number is a
validation error. Empty name/sort-name controls are also errors.

Saving sends the complete draft control sets and the loaded revision. Success
replaces the draft with the returned detail and refreshes the list. Field errors
remain associated with the relevant editor controls. A revision conflict keeps
the draft visible but requires a reload and review before another save. An
unconfirmed outcome, including a network failure or invalid success response,
also requires a reload. Closing, reloading, or navigating away with unsaved
changes uses the editor's discard guard.

## Relationship to Emby metadata operations

Native `LockedFields` accepts the native editable set, including fields whose
names do not occur in the pinned Emby `MetadataFields` enum. There is no native
`LockData`, `ForcedSortName`, refresh-mode, or reset property in this request.
Returning to automatic values means removing native controls and using the
latest accepted scanner snapshot.

The reference study observed that Emby custom sorting depended on
`ForcedSortName` plus a `SortName` lock, and that NFO import, refresh, and reset
could alter lock state. Those observations do not weaken the native retained
pin contract or make the native request an Emby `BaseItemDto` update. See
[Metadata Update, Sorting, and Locks](../research/metadata-reference.md) for the
pinned schema, complete HTTP captures, and the boundaries of that evidence.
