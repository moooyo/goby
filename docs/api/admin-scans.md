# Administrator libraries and media scans

This is a Goby-owned API. It does not add an Emby query parameter or claim
equivalence to Emby's metadata replacement operations. The dashboard is for
administration only; it contains no media player.

## Phase 3 library editing and directory contract

The library-editing and directory operations in this section passed the selected
library/server and real administrator UI scopes. The
[phase 3 record](../development/amd-media-phase3-20260919.md) owns their acceptance
and closed resources; earlier scan evidence remains independently scoped.

| Method and route | Input | Success |
| --- | --- | --- |
| `GET /admin/v1/libraries/{id}` | No query | `200 {Library}` with saved edit state |
| `PATCH /admin/v1/libraries/{id}` | Required `Revision`; optional fields below | `200 {Library, Job?, ScanError?}` |
| `GET /admin/v1/storage/directories` | Optional `Path`, `StartIndex`, `Limit` | `200 {Path, ParentPath?, Items, TotalRecordCount, StartIndex, Limit}` |
| `POST /admin/v1/storage/directories/validate` | `{Path}` | `200 {Path, Available: true}` with canonical path |

These routes require a current native administrator cookie; mutations also
require same-origin and CSRF checks. Unknown input fields are rejected.
Library detail extends the existing Library DTO with opaque decimal-string
`Revision`, `LibraryOptions: {EnableLocalMetadata, EnableLocalImages}` and
`RegisteredPaths: [{Id, Path, ItemCount}]`.

An edit accepts `Name`, a complete replacement `Paths` array of 1 through 32
approved directories, explicit `PathReplacements: [{From, To}]`, a partial
`LibraryOptions` object containing either supported Boolean, Boolean
`AcknowledgePathRemoval`, and Boolean `Scan`. Omitted fields preserve current
values. The revision is mandatory; stale revisions return `409 library_conflict`.
An active scan or pending deletion returns `409 scan_busy`; a path outside
approved roots is denied with `403`. Root changes, including separate binding
approval, advance the library revision; callers must not assume an increment
of exactly one. A no-op does not advance it.

Unchanged roots retain their root identity. An explicit `From`/`To` replacement
declares a filesystem move already performed by the administrator, with the
same relative media tree. It preserves registered root/item identities, user
state, metadata controls and access rules. The API does not move files and does
not infer moves from an arbitrary delete/add pair. Removing a root without a
replacement requires `AcknowledgePathRemoval: true`; it removes that root's
catalog records and dependent user state, while retaining all media files.

Saving does not scan by default. Only `Scan: true` requests subsequent admission;
a returned `ScanError` does not roll back a committed library edit. Follow the
returned job independently. Both local import options default to true and are
also accepted during native creation. Disabling an option stops later NFO or
directory-artwork imports and retains previously accepted source data and
manual overrides. Re-enabling requires a scan to refresh those sources.

Directory browsing is bounded to configured approved roots. Empty `Path`
lists those roots. Child rows contain `Name` and canonical `Path`; `ParentPath`
never escapes the approved root. Paging defaults to 100 and caps at 200;
enumeration is bounded to 10,000 entries, four workers and ten seconds.
Validation is read-only: it neither creates a directory nor writes a probe file.
Neither surface accepts network credentials or arbitrary unrestricted paths.

### Compatibility library and directory adapters

Current Emby administrator logins and permitted application keys can use these
bounded adapters. They recheck authority; native cookies are a separate surface.
Paths below are relative to `/emby` and retain normal namespace aliases.

| Route | Supported contract |
| --- | --- |
| `POST /Library/VirtualFolders/Name` | `{Id, NewName}` |
| `POST /Library/VirtualFolders/Paths` | `{Id, Path}` or `{Id, PathInfo: {Path}}`, with optional `RefreshLibrary` |
| `POST /Library/VirtualFolders/Paths/Delete` | `{Id, Path}`, with optional `RefreshLibrary` |
| `POST /Library/VirtualFolders/LibraryOptions` | `{Id, LibraryOptions: {DisabledLocalMetadataReaders}}`; exact `[]` or `["Nfo"]` |
| `GET /Environment/DefaultDirectoryBrowser` | `{Path: ""}` |
| `GET /Environment/DirectoryContents` | `Path`, `IncludeDirectories=true`, `IncludeFiles=false`; bare directory rows `{Name, Path, Type: "Directory"}` |
| `GET /Environment/ParentPath` | JSON string, empty at an approved root |
| `POST /Environment/ValidatePath` | `Path` query and `{}` body; `204` for a valid directory |

Library writes return `204` and a revision ETag; optional `If-Match` enables
revision checking. Virtual-folder query results include Revision and the
supported LibraryOptions projection, and compatibility creation accepts that
same narrow options object. Unsupported options and NetworkPath/credentials
are rejected. `/Paths/Update` is not a native move adapter: its upstream
network-path contract does not identify the original root. Use the native
explicit replacement for identity-preserving moves. DirectoryContents is
limited to 200 results and rejects larger directories with `422` rather than
silently returning an incomplete list. File browsing, `IsFile`,
`ValidateWriteable` and writeability probes are unsupported.

The administrator editor loads the saved revision, preserves drafts on errors,
requires explicit root-removal acknowledgement and offers approved-directory
selection. A conflict or unknown network outcome requires reload before another
save; a successful edit and its optional scan remain separate outcomes.
Source: [library transactions](../../internal/library/library_editing.go),
[native handlers](../../internal/server/library_editing.go),
[compatibility handlers](../../internal/server/library_editing_emby.go), and
[directory service](../../internal/library/server_directories.go).
Migration [0036](../../internal/database/migrations/0036_library_editing.sql)
adds edit revisions and selected options without rewriting earlier migrations.
Old-row preservation and native recovery compatibility remain phase 3 gates.

## Starting work

`POST /admin/v1/libraries/{id}/scan` requires the native administrator cookie
and `X-CSRF-Token`. Emby login tokens do not authorize this route.

An empty request body remains a normal scan for existing callers. A nonempty
body must be a UTF-8 `application/json` object of at most 4096 bytes. The only
optional field is `ForceProbe`, with an actual JSON boolean value:

```json
{"ForceProbe": true}
```

`{}` and `{"ForceProbe": false}` request normal scanning. Unknown fields,
different field casing, duplicates, nulls, extra JSON values, malformed JSON
and query parameters return `400 invalid_input`. A nonempty body without the
JSON media type returns `415 unsupported_media_type`. A rejected request does
not admit a scan job.

| Mode | Behavior |
| --- | --- |
| Normal | Discover supported files and reuse valid cached media facts when file identity, timestamps and the current probe version agree. Local sidecar checks still run. |
| ForceProbe | Re-read every eligible media file even when that cache is valid. Rebuild supported private playback indexes using the configured tools and the existing analysis limits. Local sidecar checks still run. |

Success is `202 {"Job": ...}` after durable admission, not scan completion.
One library can have only one queued or running scan. Another request receives
`409 scan_busy`; it does not silently convert or replace the existing job.
A missing library returns `404 not_found`. A full queue also returns `409`.

## Task representation and persistence

Every job in the start response, library-creation response, task list and
cancellation response includes `ForceProbe: boolean`. Other fields retain
their existing shapes:

| Field | Meaning |
| --- | --- |
| `Id`, `LibraryId` | Stable task and library identifiers. |
| `ForceProbe` | The admitted mode, preserved after completion, cancellation or interruption. |
| `Status` | `pending`, `running`, `completed`, `failed`, `cancelled` or `interrupted`. |
| `Error` | Empty when there is no warning; otherwise a bounded operational message. A completed scan may have warnings. |
| `Scanned`, `Added`, `Updated` | Visited eligible files and accepted catalog additions/updates. A successfully forced refresh of an existing record counts as updated even if the refreshed facts equal the preceding values. These are not counts of rebuilt indexes. |
| `CreatedAt`, `StartedAt`, `FinishedAt` | Persisted timestamps; the latter two can be null. |

`GET /admin/v1/jobs` returns `{Items, TotalRecordCount}` for the existing
bounded recent task list. `POST /admin/v1/jobs/{id}/cancel` retains its existing
cookie/CSRF checks and cancellation semantics. Cancellation stops further work;
already committed file updates remain. Restarted workers mark abandoned active
jobs interrupted rather than silently resuming them.

Migration 0015 adds `scan_jobs.force_probe boolean NOT NULL DEFAULT false`.
Earlier task history therefore retains normal-scan meaning. The migration does
not invalidate media caches or schedule a scan. Library creation with `Scan`
and Emby library refresh continue to admit normal scans.

## Preservation and operating limits

Refresh retains stable item IDs and uses the existing transaction that composes
automatic metadata with current administrator overrides and locks. It never
resets favorites, watched state, play counts or resume positions. Local NFO
changes can still update automatic metadata just as in a normal scan. Source
changes can therefore advance metadata revisions; this operation is not a
promise to freeze every automatic field.

An inaccessible or changing source, failed probe, or rejected source snapshot
retains its preceding valid catalog entry. The existing opened-descriptor,
root-containment and post-probe identity checks still apply. Cancellation
propagates into active probing and analysis processes.

The action can read and decode substantial portions of every source and take
longer than a cached scan. The existing two scan workers and per-source analysis
budgets still apply. Optional index analysis may be unsupported or exhaust its
budget without failing the base media probe. A completed refresh does not
guarantee a usable index for every file. Playback still checks source/tool
identity and proves a restart before using fast video seeking; otherwise it
uses the supported linear path. No private index fields become client API data.

After upgrading FFmpeg, use **Libraries > Refresh media details** for the
libraries whose cached playback indexes should be rebuilt. Normal scans do not
invalidate same-version caches merely because a tool executable changes.
Follow the task outcome and warnings in **Tasks**. The dialog keeps an
unconfirmed network outcome distinct from a rejected request so administrators
can inspect task history before trying again.

See the [video seeking contract](../development/video-fast-seek.md) for supported
index/proof formats, linear audio behavior, hardware limits and fallback rules.
