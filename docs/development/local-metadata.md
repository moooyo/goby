# Local NFO metadata

This document describes Goby's local ingestion contract. It is not a claim that every Emby or Kodi NFO option is implemented. The scanner reads descriptive metadata from bounded, local XML files; it never follows an NFO URL or downloads a referenced image, subtitle, or media source.

## File selection

| Catalog item | Candidate relative to the media directory | Required XML root |
| --- | --- | --- |
| Movie | The media basename with `.nfo`, then `movie.nfo` | `movie` |
| Episode | The media basename with `.nfo` | `episodedetails` |
| Series directory | `tvshow.nfo` | `tvshow` |
| Season directory | `season.nfo` | `season` |
| Music album directory | `album.nfo` | `album` |

Names are case-sensitive on Linux. For example, `Film (2026).mkv` uses `Film (2026).nfo` before `movie.nfo`. The fallback is shared by movies in the same directory, so use a matching basename for each movie when several movies share a directory. An invalid preferred file does not fall through to a less specific candidate.

Folders and media types are established by the scanner's existing movie, television, and music hierarchy. NFO files describe those records; they do not create a different hierarchy. The parser also recognizes an `artist` document, but artist ingestion and artist catalog endpoints are not implemented by this increment. Individual audio tracks do not yet read track NFO files.

## Values

| XML input | Stored meaning |
| --- | --- |
| `title`, `sorttitle`, `originaltitle`, `plot` | Display name, sort name, original title, and overview |
| `name` on `album` or `artist` | Fallback name when `title` is absent |
| `year` | Production year from 1 through 9999 |
| `premiered`, otherwise `aired` | Premiere date; date-only or RFC3339, normalized to UTC |
| `rating` | Finite decimal community rating from 0 through 10 |
| `mpaa` | Descriptive official rating; this does not add parental-policy enforcement |
| Repeated `genre`, `tag`, `studio` | Ordered unique strings |
| `uniqueid type="..."`, `imdbid`, `tmdbid`, `tvdbid` | Provider identifiers; known keys normalize to `Imdb`, `Tmdb`, `Tvdb` |
| `actor` with `name`, `role`, `order` | Actor credits with optional role and zero-based sort order |
| `director`, `writer`, `credits` | Director or writer credits |
| `episode` in `episodedetails` | Episode number, including explicit zero |
| `season` in `episodedetails` or `season` | Parent season number or season number, subject to hierarchy checks |

Provider identifiers are values rather than URLs. Unknown providers require bounded ASCII alphanumeric keys; identifiers permit ASCII alphanumerics plus `.`, `_`, `:`, and `-`. Conflicting duplicate scalar values or provider identifiers reject the document. Unknown elements are ignored after XML structure and size checks. Recognized scalar fields must contain text rather than child markup; use XML escaping or CDATA for a plot.

An episode's NFO may change its episode number. It cannot assign a different season than the physical hierarchy already establishes. A conflicting season number is ignored with a scan warning, while the remaining valid descriptive values are retained. The same rule prevents `season.nfo` from renumbering its physical season directory. Item IDs, probe data, and media paths remain under scanner control.

Date-only input currently denotes midnight UTC in Goby. The reference capture on an Emby host configured for `Asia/Shanghai` converted `2024-01-02` to `2024-01-01T16:00:00Z`, indicating a host-timezone interpretation for that sample. This is a recorded compatibility difference; timezone policy is not inferred from a single capture. Explicit RFC3339 offsets are normalized to UTC by Goby's parser.

## Refresh and failure behavior

Every scan checks sidecars, including when media size and modification time allow the ffprobe result to be reused. The applied document, its SHA-256 digest, and its root-relative source path are persisted separately from probe information. Source paths and raw NFO contents are not included in the Emby item DTO.

- Changing a valid sidecar updates metadata without requiring a media-file change.
- Removing all applicable sidecars clears the local override and restores scanner-derived names and numbering on the next successful inspection.
- An unreadable, malformed, wrong-kind, oversized, or unstable sidecar preserves the last valid metadata and produces a scan warning.
- An inaccessible media root retains existing catalog records and their metadata. It is not interpreted as removal of every sidecar.
- Removing a library removes catalog records while retaining media and sidecars on disk.

The task UI displays scan warnings. [Native metadata editing and field locks](../api/admin-metadata.md) now save a separate administrator layer over the latest accepted source snapshot. Normal NFO updates and removal continue to update that source; manual overrides and locked values remain until explicitly removed. Online providers and automatic watch-based refresh remain planned work. The entity filters below do not add parental-rating or other access policies.

Migration `0014` initializes metadata state without rewriting existing catalog or NFO rows. It retains the original sparse projection when no administrator controls apply. Updating an unrelated source field preserves unknown extensions in unchanged fields, including existing person credits. Only effective metadata drives the current entity associations, and those changes commit with the item update. Identical file rescans compare against automatic values rather than manual display values, so an existing title or episode-number override alone does not falsely report the file as updated.

## Persistent catalog entities

Migration `0004` assigns stable catalog identities to genres, tags, studios, and people. It backfills already stored NFO documents during the database upgrade without requiring another filesystem scan. Subsequent NFO writes and entity associations commit in the same catalog transaction; an association failure also rolls back the corresponding media/metadata write.

Genre, tag, and studio references inside item DTOs use numeric 64-bit IDs. People inside item DTOs use decimal string IDs. Entity list/detail IDs are strings, including the minimal `{Name, Id}` objects returned by `/Tags`; this distinction matches the reference. Roles, credit types, and ordering belong to the item/person association, so one person can have several credits. Names normalize consistently in PostgreSQL; a fixed-size SHA-256 key permits long entity names, with the complete normalized value checked to prevent merging different names on a hash collision.

`/Genres`, `/Tags`, `/Studios`, and `/Persons` expose only entities associated with the requesting user's visible library items. Genre, studio, and person names have detail routes; positive decimal entity IDs can also be read through `/Users/{UserId}/Items/{Id}`. Removing a sidecar or library removes the corresponding associations. Orphaned entity rows retain their identity for future reuse but are not returned by these browsing routes.

Item queries support `GenreIds`, `TagIds`, `StudioIds`, `PersonIds`, `Genres`, `Tags`, `Studios`, `Person`, and `PersonTypes`. Name lists use `|`; ID lists accept `|` or commas. Values within a dimension use OR and separate dimensions use AND. Person identifiers/names and credit types must match the same credit association. Authorization is applied before filtering, counting, grouping, or pagination. These filters do not implement parental ratings, subfolder exclusions, or the rest of the upstream query surface.

## Input boundaries

Sidecars are opened through the configured library root, checked as regular files, and read through the same file handle. Final symlinks and special files are rejected; Linux nonblocking opens prevent a replaced FIFO from indefinitely blocking the scanner. Identity, size, and modification checks detect changes during a read.

The parser accepts UTF-8, an optional UTF-8 BOM, and an optional XML 1.0 declaration. It rejects DOCTYPE, directives, unsupported processing instructions, external entities, namespaced roots, invalid numeric/date values, and partial documents. Limits are 2 MiB per document, 64 nesting levels, 16,384 elements, 64 attributes per element, 64 KiB per field, and 1,024 entries per collection. Display and sort names have a stricter 1,024-byte UTF-8 limit so accepted metadata fits the catalog's PostgreSQL sort index; album/artist fallback names follow the same bound. It returns no partially parsed metadata on failure.

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
