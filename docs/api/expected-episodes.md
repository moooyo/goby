# Expected episode rosters

Goby projects missing episodes only from an explicit, versioned local roster
imported by a current native administrator. A provider ID, an episode count, a
numbered gap between files, and the absence of a file are not episode evidence.
This adapter does not call metadata providers or read a client-supplied path or
URL. The administrator is the authority for the declared episode records.

## Native administration

The routes are `GET`, `PUT`, and `DELETE`
`/admin/v1/series/{id}/episode-roster`. They require native administrator
authentication; writes also require the existing CSRF protection. Every store
operation rechecks the current administrator. Writes lock the account/session
before the physical Series row and perform compare-and-swap in one owned
catalog transaction.

`PUT` replaces the complete current roster:

```json
{
  "Revision": "0",
  "Source": {"Key": "local-episode-list", "Label": "Declared episodes", "Revision": "2026-09-20"},
  "Entries": [
    {"Key": "season-1-episode-2", "SeasonNumber": 1, "EpisodeNumber": 2, "Name": "Second episode", "PremiereDate": "2020-01-02"},
    {"Key": "season-1-episode-3", "SeasonNumber": 1, "EpisodeNumber": 3, "Name": "Date not announced"}
  ]
}
```

The outer `Revision` is a canonical nonnegative signed-64-bit decimal string.
An untouched Series has revision `"0"`, state `absent`, `Source: null`, and an
empty `Entries` array. Source revision is an independent, administrator-supplied
version label. Reusing the same source key and source revision for different
content returns `409 source_revision_conflict`. Stale outer revisions return
`409 revision_conflict`. An identical current payload is a no-op after CAS
succeeds, including a change only to entry ordering.

Limits are 512 KiB of request JSON, 512 KiB of normalized retained payload, and
2,000 entries. Key and source revision strings are required and limited to 128
UTF-8 bytes. Source label is required but may be empty, up to 256 bytes. Name is
required but may be empty, up to 512 bytes; virtual display uses `Episode N` for
an empty name. All strings reject surrounding whitespace, control characters,
and invalid Unicode. Keys are case-sensitive. Each entry key and each pair of
season and episode numbers must be unique. Both numbers are integers from zero
through 9,999; season zero remains an explicit special. The parser rejects
unknown fields, duplicate fields, null values, and ambiguous numeric types.

`PremiereDate` is an optional canonical Gregorian `YYYY-MM-DD` date. Omit it
when unknown; null, empty strings, and timestamp strings are rejected. A known
date after the current UTC day is `unaired`; a known date on or before that day
is `aired`; an omitted date remains `unknown`. Unknown is not proof of either
airing status. An explicit empty roster is valid and active.

`DELETE` accepts only `{"Revision":"current-revision"}` and explicitly
withdraws the active authority. It retains the source, a revision tombstone,
immutable imports, and retired facts. Withdrawing an absent or already
withdrawn roster is a no-op after CAS succeeds. Successful reads and writes
return the complete detail with `Cache-Control: no-store`:

- `SeriesId`, `SeriesName`, `Revision`, and `State` (`absent`, `active`, or `withdrawn`).
- `Source` with `Kind: "admin_import"`, key, label, source revision, parser version `1`, and SHA-256.
- Active `Entries` with the supplied fields, stable `Id`, `Availability`, `Airing`, `AvailableItemIds`, and `AvailableItemCount`.
- `RetiredCount`, `LastEditedBy`, and `LastEditedAt`.

Availability references contain the first eight physical IDs in stable ID
order; `AvailableItemCount` is the exact total. `available` always has a
nonempty reference array. Retired entries do not appear in the active array.
Import and withdrawal payloads remain in durable history, independently of
physical episode arrival or removal. They contain the normalized source and
entries, parser version, source revision, SHA-256, actor, and import revision.
The source hash includes the source label. Changing the label therefore also
requires a new source revision.

## Physical availability and virtual discovery

Facts live in `expected_episodes`, outside `items`. A physical match must be an
ordinary non-folder Episode with a root, a nonempty path, a retained media
object, the same Series and explicit episode number, and the declared season.
The Series relationship must be direct or through one real Season parent in
the same library. A matching physical episode suppresses its missing
projection without replacing that physical ID or its user data. A later file
removal reveals the same stable missing identity only while its declared fact
remains active. Availability is catalog presence, not a guarantee that a client
can play the media. Account policy hiding an existing physical episode does not
turn it into a missing episode.

The virtual ID uses a distinct `missing-` namespace and a versioned hash of
Series ID, source key, and entry key. Reactivating the same source/key retains
the ID. Replacing the source key explicitly retires the old identities.

General Items discovery can include these facts through the user's
`DisplayMissingEpisodes` preference or explicit missing/virtual selectors.
Physical and virtual candidates share one authorized transaction, filtering,
sorting, counting, and pagination. Both the Series and any unique physical
Season parent must be visible. If multiple ordinary Season rows claim the same
number, missing projection for that season is suppressed until the ambiguity
is resolved; the imported native source remains unchanged.

Missing details use the normal item routes and advertise `LocationType:
"Virtual"`, `IsMissing: true`, `IsPlaceHolder: true`, `IsVirtualUnaired`,
`IsUnaired`, `CanDownload: false`, and `SupportsResume: false`. They do not expose
paths, media streams or sources, writable user data, or source import payloads.
Unknown dates omit `PremiereDate` and do not set the unaired flags. Physical
arrival or explicit withdrawal makes the former missing detail return 404.

Missing facts are excluded from NextUp, resume, playback queues, mixes,
suggestions, physical media access, and user-state writes. A valid PlaybackInfo
request for a missing ID returns 404 `not_found`; it cannot create a playback
session or encoding job.

## Owner lifecycle and backup

If a scanner reclassifies the same physical Series ID as another type or an
auxiliary resource, the saved roster becomes dormant. Its revisions, source
hashes, import history, and expected identities remain unchanged. Native roster
reads/writes reject that owner and consumer queries do not project its facts.
If the same ID becomes an ordinary Series again, the retained authority applies
to the current catalog. This does not create a synthetic withdrawal or new
source revision. Deleting the owner item removes its dependent roster and
history through foreign keys; deleting an episode does not.

Schema 46 retains current rosters, immutable imports, and expected facts.
Backup validation checks exact canonical payload bytes and hashes, source
revision consistency, stable identities, active and retired history, and the
current import pointer. Restore preserves the source evidence and revisions;
it does not fetch providers, scan invented items, or replay import side effects.
