# Local NFO metadata

This document describes Goby's local ingestion contract. It is not a claim that every Emby or Kodi NFO option is implemented. The scanner reads descriptive metadata from bounded, local XML files; it never follows an NFO URL or downloads a referenced image, subtitle, or media source.

Phase 3 additions passed their selected [remote scopes](amd-media-phase3-20260919.md).
Per-library `EnableLocalMetadata` defaults to true. Disabling it stops later
NFO import without deleting the accepted source or administrator controls;
re-enabling takes effect on a subsequent scan. See
[library editing](../api/admin-scans.md#phase-3-library-editing-and-directory-contract).
Extended embedded music tags, composer relationships and supported artist/genre
routes have their separate [music contract](music-metadata.md#current-phase-3-music-contract).
They do not add artist/track NFO discovery or remote URL following.

## File selection

| Catalog item | Candidate relative to the media directory | Required XML root |
| --- | --- | --- |
| Movie | The media basename with `.nfo`, then `movie.nfo` | `movie` |
| Episode | The media basename with `.nfo` | `episodedetails` |
| Series directory | `tvshow.nfo` | `tvshow` |
| Season directory | `season.nfo` | `season` |
| Music album directory | `album.nfo` | `album` |

Names are case-sensitive on Linux. For example, `Film (2026).mkv` uses `Film (2026).nfo` before `movie.nfo`. The fallback is shared by movies in the same directory, so use a matching basename for each movie when several movies share a directory. An invalid preferred file does not fall through to a less specific candidate.

The scanner establishes the hierarchy without moving files. Television libraries
recurse through grouping folders and use local evidence to recognize a show: a
`tvshow.nfo` marker even when metadata import is disabled, directly named
`SxxExx` videos from one show, a populated immediate `Season N`/`Specials`
directory (including one `Disc N`, `Disk N`, or `CD N` level), or loose videos
in a leaf directory. An episode in a physical season belongs to that season
unless its filename explicitly names a different one. A flat folder of episodes
from multiple named shows is grouped by filename unless it has an explicit local
show marker or season structure. Evidence deeper in a grouping folder does not
make that folder a show. The NFO import still validates the document; it cannot
move a season or replace a file's identity. A directory can be a show regardless
of its name, including `All`.

Scans skip the directory subtrees named exactly `#recycle`, `@eaDir`, and
`$RECYCLE.BIN`. Earlier catalog records under these paths and their attached
media are hidden from catalog reads without deleting records, user data, or
media files. File names and virtual show names are not directory components.
An ordinary rescan can correct a grouping folder previously classified as a
show without changing its registered root or the IDs of items at unchanged paths.

The parser also recognizes an `artist` document, but artist NFO ingestion is
not implemented by this increment. Current artist catalog endpoints use the
music relationship contract above. Individual audio tracks do not yet read
track NFO files.

## Values

| XML input | Stored meaning |
| --- | --- |
| `title`, `sorttitle`, `originaltitle`, `plot` | Display name, sort name, original title, and overview |
| `name` on `album` or `artist` | Fallback name when `title` is absent |
| `year` | Production year from 1 through 9999 |
| `premiered`, otherwise `aired` | Premiere date; date-only or RFC3339, normalized to UTC |
| `rating` | Finite decimal community rating from 0 through 10 |
| `mpaa` | Official rating used by the existing content policy |
| Repeated `genre`, `tag`, `studio` | Ordered unique strings |
| `uniqueid type="..."`, `imdbid`, `tmdbid`, `tvdbid` | Provider identifiers; known keys normalize to `Imdb`, `Tmdb`, `Tvdb`, including `TMDB-TV` as `Tmdb` |
| `actor` with `name`, `role`, `order` | Actor credits with optional role and zero-based sort order |
| `director`, `writer`, `credits` | Director or writer credits |
| `episode` in `episodedetails` | Episode number, including explicit zero |
| `season` in `episodedetails` or `season` | Parent season number or season number, subject to hierarchy checks |

Local provider keys and values are opaque UTF-8 text, bounded to 64 and 256 bytes respectively after trimming, without embedded control characters. Unknown keys keep their spelling. An ID can contain slashes or resemble a URL; importing it does not make it a request target or a valid lookup ID. Online adapters validate their own identifiers. Empty or missing types, `type="default"`, blank IDs, and the literal `0` are ignored. For a repeated normalized key, the last non-placeholder ID wins in document order; `default="true"` does not affect priority. A metadata lock can freeze these accepted values without changing them. Manual overrides and online provider inputs keep their own validation.

Conflicting duplicate values in other recognized scalar fields still reject the document. Unknown or inapplicable elements are ignored after XML structure and size checks. Used scalar fields must contain text rather than child markup; use XML escaping or CDATA for a plot.

Number fields are read only for the levels that use them. Series `tvshow.nfo` does not validate unused `season` and `episode` placeholders, including `-1`. Actual season and episode numbering keeps the nonnegative range; `0` remains distinct from an unset number. An episode's NFO may change its episode number, but cannot assign a different season than the physical hierarchy already establishes. A conflicting season number is ignored with a scan warning while other valid descriptive values are retained. The same rule prevents `season.nfo` from renumbering its physical season directory. Item IDs, probe data, and media paths remain under scanner control.

Date-only input currently denotes midnight UTC in Goby. The reference capture on an Emby host configured for `Asia/Shanghai` converted `2024-01-02` to `2024-01-01T16:00:00Z`, indicating a host-timezone interpretation for that sample. This is a recorded compatibility difference; timezone policy is not inferred from a single capture. Explicit RFC3339 offsets are normalized to UTC by Goby's parser.

## Two episodes in one file

A physical episode with two adjacent episode numbers in its filename, such as `Show.S01E03E04` or `Show.S01E03-E04`, can expose `IndexNumberEnd: 4` alongside `IndexNumber: 3`. The S/E tag must end the filename stem, except for an optional ` - 第 N-M 集` label at the end. Both decimal numbers in that label must match the S/E range (leading zeros are allowed). A conflicting label or any other trailing text does not establish a pair. The range comes from the catalog path regardless of NFO presence. If the current season or starting number no longer agrees with the filename, the end number is omitted. The file still has one item ID, one source, and one playback position. This does not change missing-episode matching.

Both synchronous scanning and deferred local import can merge a matching `.nfo` for a paired file immediately inside its physical season directory. The sidecar must contain exactly two complete, unwrapped `episodedetails` roots in episode order, each with one explicit `season` and `episode` matching the file and season. A single document still uses the ordinary NFO rules. Both roots share the input, structure, and collection limits. The roots must have equal official ratings and the same tags (or both omit them). Goby joins nonempty titles with a comma and two spaces, and plots with two line feeds. All other fields, including provider IDs, come from the first root.

A rejected pair does not publish either root. A scan can retain the last accepted merge for the same media, root, and hierarchy, but does not retain it on a different binding. A pending NFO, or a root or hierarchy remap that has not yet been verified by a scan, keeps the item hidden from viewers whose rating or tag policy depends on that evidence. Disabling NFO reading does not retire pending verification; independent artwork work can still complete. A verified replacement or confirmed absence can clear the pending phase. These decisions intentionally differ from some [observed Emby fallbacks](../../tests/compatibility/fixtures/reference/emby-4.10.2.0/nfo-multi-episode.md).

## Refresh and failure behavior

Every scan checks sidecars, including when media size and modification time allow the ffprobe result to be reused. The applied document, its SHA-256 digest, and its root-relative source path are persisted separately from probe information. Source paths and raw NFO contents are not included in the Emby item DTO.

- Changing a valid sidecar updates metadata without requiring a media-file change.
- Removing all applicable sidecars clears the local override and restores scanner-derived names and numbering on the next successful inspection.
- An unreadable, malformed, wrong-kind, oversized, or unstable sidecar preserves the last valid metadata and produces a scan warning.
- An inaccessible media root retains existing catalog records and their metadata. It is not interpreted as removal of every sidecar.
- Removing a library removes catalog records while retaining media and sidecars on disk.

The task UI displays scan warnings. [Native metadata editing and field locks](../api/admin-metadata.md) save a separate administrator layer over the latest accepted source snapshot. When local import is enabled, normal NFO updates and removal continue to update that source; manual overrides and locked values remain until explicitly removed. Online provider adapters have a separate implementation and user-deferred acceptance boundary; this local contract does not add automatic watch-based refresh. Entity filters below use the current catalog access rules without defining additional policy fields.

Migration `0014` initializes metadata state without rewriting existing catalog or NFO rows. It retains the original sparse projection when no administrator controls apply. Updating an unrelated source field preserves unknown extensions in unchanged fields, including existing person credits. Only effective metadata drives the current entity associations, and those changes commit with the item update. Identical file rescans compare against automatic values rather than manual display values, so an existing title or episode-number override alone does not falsely report the file as updated.

## Rereading a single existing item

Use the [native local NFO reread](../api/admin-metadata.md#reread-local-nfo)
for a deliberately selected replacement on an already indexed movie, episode,
season or series. It does not scan, call online providers, process artwork or
write media-side files. Unlike scanning, this operation does not interpret a
missing NFO as an instruction to clear existing metadata, does not renumber
items, and does not retire work that belongs to an existing scan generation.
Review the external replacement first; this is an import command, not a check
that a sidecar describes the right show or that ratings are harmless.

## Advisory series identity audit

Use the [native one-library audit](../api/admin-metadata.md#series-nfo-audit)
for a syntactically valid `tvshow.nfo` that may describe another series. A
folder's binding establishes where the document came from, not which show it
describes. The audit compares current stable sidecars with their accepted
versions, not manual names, online metadata or saved recovery copies. A second
physical directory whose own name and year agree can strengthen a warning,
but duplicate provider IDs, separate copies and local titles are not proof of
identity. Warnings never change scan acceptance, playback access or recovery
eligibility. A lack of warnings is not verification. This is separate from
repairing malformed or empty files.

## Automatic regeneration of damaged sidecars

For movie and TV libraries, the importer checks a suspicious preferred NFO with
an independent bounded read before recording damage. Zero bytes, invalid
recognized encoding, or malformed XML can qualify. Short reads, access errors,
unsupported encodings or fields, wrong document types, and failed episode pair
binding are not proof of damage. The scan must already be complete before the
import task hands off to the separate **Regenerate local NFO sidecars** task. A repeated
observation of the same source and damaged file version does not issue another
request or reset the three-attempt limit.

Writing is off by default. On Linux, `GOBY_NFO_RECOVERY_ROOTS` lists exact
registered root paths from the media allowlist, colon-separated. Set it only
where the deployment can give Goby exclusive sidecar write access and the
filesystem provides same-directory atomic replacement and synchronization. Do
not enable it on a share written to by another media manager or uncoordinated
client. A listed path does not grant permissions to a separately registered
child root or override a read-only mount. If multiple registered roots cover
one sidecar directory, Goby does not write there. NFO reading remains a separate
per-library option.

On opted-in roots Goby retains one private, bounded copy of the last accepted
original for a uniquely owned movie or episode sidecar or a physical series or
season. The copy follows the proven item, media, directory, and root binding,
not just a path or matching text. Paired episodes require both accepted original
roots. Overrides, online results, damaged fragments, backups for a different
source, and shared `movie.nfo` are never used to regenerate a file. The copies
are included in the private PostgreSQL backup, not item DTOs. There is no
backfill from projected or pre-migration metadata. If a file was already bad
when first observed, supply a trusted replacement or deliberately remove the
sidecar; Goby does not invent titles, tags or ratings.

The writer stages the saved bytes next to the damaged file, synchronizes and
closes the temporary file, reopens it for exact read-back, then rechecks the
candidate and source before replacement. The pending NFO gate stays in place
until an independent local import accepts the visible file or confirms the
applicable candidates are absent. Artwork, administrator edits and watch data
are separate. A lost write acknowledgement is not permission to repeat a
replacement: the next attempt re-observes the live file. After the third write
attempt, only read-only verification may continue. It can hand an already
repaired file back to the importer without granting a fourth write. When no
trusted source is available, the path is not authorized for writing, or attempts
are exhausted, the
task leaves the NFO pending rather than accepting unknown rating or tag values.
A subsequent external replacement can still be accepted by the normal importer.

## Persistent catalog entities

Migration `0004` assigns stable catalog identities to genres, tags, studios, and people. It backfills already stored NFO documents during the database upgrade without requiring another filesystem scan. Subsequent NFO writes and entity associations commit in the same catalog transaction; an association failure also rolls back the corresponding media/metadata write.

Genre, tag, and studio references inside item DTOs use numeric 64-bit IDs. People inside item DTOs use decimal string IDs. Entity list/detail IDs are strings, including the minimal `{Name, Id}` objects returned by `/Tags`; this distinction matches the reference. Roles, credit types, and ordering belong to the item/person association, so one person can have several credits. Names normalize consistently in PostgreSQL; a fixed-size SHA-256 key permits long entity names, with the complete normalized value checked to prevent merging different names on a hash collision.

`/Genres`, `/Tags`, `/Studios`, and `/Persons` expose only entities associated with the requesting user's visible library items. Genre, studio, and person names have detail routes; positive decimal entity IDs can also be read through `/Users/{UserId}/Items/{Id}`. Removing a sidecar or library removes the corresponding associations. Orphaned entity rows retain their identity for future reuse but are not returned by these browsing routes.

Phase 3 adds independent entity images and per-user state with recorded selected
acceptance. Entity UserData is stored independently from associated
media and is visible only while a current authorized association exists.
Supported updates include favorite/played flags, play count, LastPlayedDate,
nullable Rating from 0 through 10, and nullable Likes. Entities are not playable:
a nonzero position or HideFromResume=true is invalid; zero/false may clear those
fields. An entity update never marks all associated media watched or rated.
There is no shared userless application-key state. Current subject authorization
and the generic UserDataChanged path apply. See
[entity state](../../internal/library/entity_user_data.go) and
[artwork management](local-artwork.md).

Item queries support `GenreIds`, `TagIds`, `StudioIds`, `PersonIds`, `Genres`, `Tags`, `Studios`, `Person`, and `PersonTypes`. Name lists use `|`; ID lists accept `|` or commas. Values within a dimension use OR and separate dimensions use AND. Person identifiers/names and credit types must match the same credit association. Authorization is applied before filtering, counting, grouping, or pagination. These filters do not implement parental ratings, subfolder exclusions, or the rest of the upstream query surface.

## Input boundaries

Sidecars are opened through the configured library root, checked as regular files, and read through the same file handle. Final symlinks and special files are rejected; Linux nonblocking opens prevent a replaced FIFO from indefinitely blocking the scanner. Identity, size, and modification checks detect changes during a read.

The parser accepts UTF-8 (with or without a BOM) and UTF-16 LE or BE with a BOM. An optional XML 1.0 encoding declaration must agree with the input bytes. It does not guess encodings or replace invalid byte sequences with replacement characters. It rejects DOCTYPE, directives, unsupported processing instructions, external entities, namespaced roots, invalid values in used numeric/date fields, and partial documents. Limits are 2 MiB of raw input (including any BOM), 64 nesting levels, 16,384 elements, 64 attributes per element, 64 KiB of decoded UTF-8 per XML field, and 1,024 entries per collection. Display and sort names have a stricter 1,024-byte UTF-8 limit so accepted metadata fits the catalog's PostgreSQL sort index; album/artist fallback names follow the same bound. It returns no partially parsed metadata on failure.

[The Emby 4.10.2.0 differential capture](../../tests/compatibility/fixtures/reference/emby-4.10.2.0/nfo-differential.md) records the reference behavior behind this import support. Goby still enforces item kind and physical hierarchy, preserves the previous snapshot for a rejected document, and replaces the local source fields on successful import. It does not copy the reference server's lossy decoding, old-ID retention when a key disappears from a valid document, or different movie fallback behavior.

## Example

Place this file beside `Example Film.mkv` as `Example Film.nfo`, then run the library scan from the administrator dashboard:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Example Film</title>
  <sorttitle>Example Film</sorttitle>
  <year>2026</year>
  <premiered>2026-01-10</premiered>
  <plot>A synthetic film used to demonstrate local metadata.</plot>
  <genre>Drama</genre>
  <tag>Local collection</tag>
  <rating>7.5</rating>
  <actor>
    <name>Example Actor</name>
    <role>Lead</role>
    <order>0</order>
  </actor>
</movie>
```

Retrieve the authorized item through the Emby API after the scan completes. Selected descriptive fields use the normal `Fields` parameter and detail projection; see the [implemented API surface](../api/implemented.md). This workflow supplies metadata for external clients and does not create a consumer web player.
