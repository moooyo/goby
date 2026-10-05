# Bitmap subtitle timelines

Goby extracts source-aligned display intervals from embedded PGS
(`hdmv_pgs_subtitle`) and DVD (`dvd_subtitle`) streams, external SUP files,
and external IDX/SUB pairs belonging to a Movie or Episode.
The player draws those intervals in the media-information view. Generation is
opt-in; reading a descriptor or interval document never schedules work.

This is a Goby extension, not an original Emby subtitle-timeline contract. It
does not convert subtitles to text or change subtitle selection, delivery, or
burn-in. Extraction uses the existing bitmap decoder's actual display semantics
and discards each image after collecting its interval. It does not run OCR,
Tesseract, a video decoder, or a GPU. External inputs use native bounded SUP and
MPEG-PS/SPU parsing and reuse the same PGS/DVD display decoders.

## External source discovery and playback scope

The scanner associates sidecars with the most specific matching video basename.
SUP is one file and one track. An IDX requires exactly one same-directory,
same-stem SUB file; a standalone SUB does not create a track. Every language in
an IDX/SUB pair has its own stable public stream index, distinct from its demux
ordinal. Public indexes share the text/owned/bitmap namespace and retired
indexes are never reassigned. There are at most 32 active external/owned tracks
and 4096 retained external/owned identities per item.

Scanning validates structure and records component identity, byte length,
modification/change times and SHA-256 without generating a timeline. Incomplete
or unstable directory observations cannot retire previous tracks. Symlinks,
nonregular files, ambiguous companion names, and path-bearing IDX directives
are rejected. Generation borrows explicitly authorized file descriptors and
never asks a decoder to discover another file by pathname.

External bitmap records describe timeline availability, not a newly supported
playback delivery method. Item/source `MediaStreams` project them with
`IsExternal:true`, `IsTextSubtitleStream:false`, `SupportsExternalStream:false`,
and `GobySubtitleTimelineOnly:true`, without a delivery URL or private component
paths. The Goby player draws valid timelines but excludes these tracks from
automatic, remembered and manual playback selection. Existing embedded bitmap
and external text playback remain unchanged. External bitmap burn-in, original
file delivery and paired-file deletion are separate capabilities; the existing
single-file text subtitle removal endpoint cannot delete an IDX or its SUB.

## Player visibility

A subtitle row appears only after valid, nonempty data is available for that
stream and source clock. The label and lane are included or omitted together.
Missing, unsupported, failed, stale, empty, or invalid responses do not create
status placeholders in the consumer view. The same rule applies to the existing
SRT/WebVTT cue lanes. Subtitle selection and playback remain available through
their existing routes even when the media-information lane is absent.

The player checks source identity/version, duration, stream index, interval
bounds, and the returned URL before rendering. It loads only in the active
media-information stage, limits concurrency to four requests, and cancels work
when leaving that stage or changing account/source. It neither generates data
nor carries an old timeline across stages without rechecking the descriptor.

Task state, failure reasons, unsupported-source diagnostics, and decoder warnings
are administrator information. Queue state alone does not determine consumer
availability: if a failed Force attempt preserves a valid current artifact,
the player can still display that artifact without showing the task failure.

## Persistent source-side material

```text
<source parent>/backdrops/goby-subtitle-timelines/<sha256(exact source basename)>/
  .owner.json
  manifest.json
  gen-<generation id>.gstl
```

The basename includes its extension. The association excludes item IDs and the
absolute parent path. Reindexing after a database rebuild or a same-filesystem
folder move can preserve it; renaming the source changes the directory key.
File identity, size, modification/change times, indexed subtitle facts, and
source-clock facts bind the artifact to its source. External components and
their public/demux index mapping also participate in this binding. Every read
and publication rechecks all bound components, including their hashes; shared
IDX/SUB components are checked once per operation. A restored database retains
the mapping, while a rebuild that changes indexes requires explicit
regeneration. Copying across filesystems can change file identity.

These are permanent media assets. There is no LRU, expiry, or cache-cleanup
policy. Scans, disabled generation, and changed source/configuration facts do
not remove completed files or automatically replace them. Ordinary requests
reuse retained material, including stale material. Stale intervals stay on disk
but are not served against a changed source clock; explicit regeneration is
required to make them current.

Only a new explicit `Force` operation replaces an existing publication after
all admitted tracks complete successfully. Failure, cancellation, or a failing
later track preserves the previous files. A successful replacement can retire
its validated superseded generation; it does not delete arbitrary files or
retain every historical generation. Unknown ownership, malformed manifests,
damaged payloads, and conflicting files require administrator review. Force
does not authorize overwriting untrusted material.

## Authenticated consumer API

```text
GET /emby/Items/{Id}/SubtitleTimelines
GET /emby/Items/{Id}/SubtitleTimelines/{StreamIndex}?tag=<artifact tag>
```

The descriptor accepts only optional `UserId` and `api_key`. The track route
additionally requires `tag`, a nonempty value of at most 256 bytes. Query names
are case-sensitive, must occur once, and must have nonempty values. Unknown
parameters and a bare query delimiter are invalid. `StreamIndex` is canonical
decimal `0..2147483647`. Use the returned URL instead of constructing the tag.

Normal media authorization applies. Ordinary users require visible media and
playback permission. `UserId` selects a state projection; it does not grant
another user's rights. Application keys retain their own media permissions,
narrowed by any selected user/library visibility. Authorization and the current
source publication are checked again before a track is served.

Missing or unusable material returns `{"Available":false}`. An obsolete source
may add `"Stale":true`, without old intervals or track URLs. Folders, unsupported
item types, and unprobed items also have no available descriptor. Missing or
invisible item IDs still return the normal authorization/not-found response.
A current descriptor has this shape:

```json
{
  "Available": true,
  "MediaSourceId": "example-source-id",
  "SourceVersion": "subtitle-timeline-source-v1-<sha256>",
  "DurationTicks": 120000000,
  "Streams": [
    {
      "StreamIndex": 1,
      "Codec": "hdmv_pgs_subtitle",
      "IntervalCount": 2,
      "Url": "/emby/Items/example-item-id/SubtitleTimelines/1?tag=<artifact-tag>"
    }
  ]
}
```

`SourceVersion` uses `subtitle-timeline-source-v1-` for an unchanged embedded-only
source and `subtitle-timeline-source-v2-` when external bitmap components are
bound. It is a source snapshot fingerprint, not a database revision or the
artifact tag. Each URL preserves an explicitly supplied `UserId` but does not
copy `api_key`; the client must still authenticate. No host path, subtitle text,
image, or decoder warning is exposed by the consumer API.

The track response is JSON, not the source-side GSTL bundle:

```json
{
  "MediaSourceId": "example-source-id",
  "SourceVersion": "subtitle-timeline-source-v1-<sha256>",
  "StreamIndex": 1,
  "DurationTicks": 120000000,
  "Intervals": [
    { "StartTicks": 10240000, "EndTicks": 56320000 },
    { "StartTicks": 66560000, "EndTicks": 87040000 }
  ]
}
```

Ten million ticks equal one second. Intervals are half-open
`[StartTicks, EndTicks)`, ordered, nonempty, and bounded by the whole item's
`[0, DurationTicks)` axis. Overlapping or adjacent displays from one track are
unioned; positive gaps remain gaps. The clock follows the indexed presentation
origin, never the first subtitle cue. A forced flag does not change the time
axis or remove ordinary displays from a track's coverage.
If the decoder closes a final display using declared packet duration or the
indexed source end, its warning remains in the administrator artifact summary.

Descriptors use `Cache-Control: private, no-store`. Track responses use
`application/json`, `X-Content-Type-Options: nosniff`, and
`Cache-Control: private, no-cache`. Their ETag distinguishes generation and
stream, with conditional and range-request support. Each track is bounded to
10,000 intervals and a 1 MiB JSON response. An obsolete tag must be refreshed
through the descriptor; it cannot retrieve a superseded publication.

## Administrator and task API

`LibraryOptions.EnableSubtitleTimelineGeneration` defaults to false for
`movies`, `tvshows`, and `mixed` libraries. It is independent of background
clips, audio waveforms, BIF previews, and intro/credits detection. A manual item
request does not require automatic generation to be enabled.

| Method and path | Contract |
| --- | --- |
| `GET /admin/v1/subtitle-timelines` | List item queue state with optional `LibraryId`, `SearchTerm`, `State`, `StartIndex`, and `Limit`. |
| `GET /admin/v1/items/{id}/subtitle-timelines` | Read item state and its current `Artifact`; accepts no query. |
| `POST /admin/v1/media-analysis/runs` | Queue and start selected work with the complete body below. |

```json
{
  "Kind": "subtitle-timeline",
  "RequestId": "client-generated-unique-request-id",
  "LibraryIds": [],
  "ItemIds": ["example-item-id"],
  "Force": false
}
```

Native administrator routes require a current administrator session cookie;
writes also require the existing same-origin and `X-CSRF-Token` checks. The
strict UTF-8 JSON body has exact fields and a 64 KiB limit. Library IDs are
limited to 64, item IDs to 256, and each ID to 128 UTF-8 bytes, without
duplicates. Empty collections select all eligible scope. If both collections
are supplied, selected items must belong to the selected libraries.
`RequestId` is nonempty and at most 128 bytes.

Success returns `202` with `RunId`, `TaskId`, `Admitted`, and `Queued`. Retry an
uncertain result with the same request ID, actor, and selection. Durable receipts
prevent duplicate admission; reusing an ID with a different selection conflicts.
The publication retains the Force operation identity so replay or recovery
cannot regenerate an already completed operation. Current task authority,
source revision, and administrator/session rights are checked before protected
work and again at publication.

The task key is `media.subtitle_timeline_generation`. It uses existing task
progress, cancellation, recovery, and the shared media-analysis slot. Untouched
scheduling has daily and `SubtitleTimelineGenerationRequested` triggers;
administrator schedule changes are preserved. Eligible scans/enablement request
automatic processing, which admits absent queue records rather than replacing
completed material. Batches yield after eight items when event continuation is
available; a manual child drains admitted work when that event is removed.
Generic task start does not introduce a Force parameter. Reads do not create
directories, queue entries, or task history.

The inventory returns `{Items, TotalRecordCount, StartIndex, Limit}`. Pagination
defaults to 0/50, with `StartIndex <= 1000000` and `1 <= Limit <= 200`.
`SearchTerm` is at most 256 UTF-8 bytes without NUL. State values are `missing`,
`pending`, `running`, `ready`, `failed`, and `cancelled`.

Item detail includes `ItemId`, `LibraryId`, `Name`, `SourceRevision`,
`DurationTicks`, `SubtitleStreamCount`, `State`, `RequestedRevision`,
`CompletedRevision`, `RunId`, `Reused`, `ErrorCode`, and nullable
request/start/finish timestamps. Revision values are strings.
`SubtitleStreamCount` counts eligible embedded PGS/DVD streams and indexed
external bitmap tracks, not all subtitle tracks. `Artifact` contains `Available`, `Stale`, and optional `Profile`,
`Generation`, `DurationTicks`, `Size`, and track summaries with `StreamIndex`,
`Codec`, `IntervalCount`, and `Warnings`. A null `Warnings` value means no
warnings and must be treated as an empty list. Queue `ready` is not proof of
current source-aligned material; inspect `Artifact` too.

The administrator runtime projection includes `SubtitleTimelineAvailable` and
`SubtitleTimelineReasons`. Missing dependencies make this feature unavailable
without preventing server startup. Representative item error codes distinguish
`unsupported_source` (a recognized source feature the extractor cannot handle),
`decode_failed` (damaged or invalid bitmap data), `processing_limit`,
`dependencies_unavailable`, `media_directory_not_writable`, `source_changed`,
`material_conflict`, and `generation_failed`. An unsupported source is a failed
generation with an error code, not another queue-state enum. None of these
labels appears as a consumer timeline row.

| Status/code | Meaning |
| --- | --- |
| `400 invalid_input` | Invalid query, canonical stream index, tag, or input body. |
| `401` / `403` | Authentication, playback, administrator, origin, or CSRF rejection. |
| `404 not_found` | Missing/invisible item or requested track. |
| `404 subtitle_timeline_unavailable` | No valid current artifact can serve the requested track. |
| `404 subtitle_timeline_changed` | The requested tag or publication no longer matches. |
| `404 subtitle_timeline_stale` | An administrator/storage operation encountered an obsolete source artifact. |
| `409 subtitle_timeline_conflict` | Conflicting receipt or untrusted/damaged stored material. |

## Storage format, limits, and deployment

Embedded-only source-side bundles retain **GSTL v1**, little-endian. Its 50-byte header
contains magic `GSTL`, version `uint16(1)`, reserved `uint16(0)`, the 32-byte
FFprobe SHA-256, duration `int64`, and track count `uint16`. Each track contains
stream index `uint16`, codec `uint8` (`1` PGS, `2` DVD), warning count `uint8`,
interval count `uint32`, warning IDs `uint8`, then start/end `int64` pairs.
Warning IDs are controlled and append-only within v1. The bundle contains no
text, pixels, source IDs, or host paths; its manifest binds source and generation
identity. Public clients consume the JSON API and do not parse this bundle.

Bundles with external inputs use **GSTL v2** and the profile
`subtitle-timeline-v2;source=bitmap-display;clock=container;intervals=union`.
The header and other track fields remain the same, except version is 2 and
stream indexes are `uint32` bounded to signed 32-bit range. Readers accept both
versions. Existing v1 files are not rewritten by scans or ordinary requests.
Adding an external track makes an older bundle stale and requires explicit
Force regeneration to include the new track.

The Linux executor uses the admitted FFprobe 9.0.1 binary. Startup inventories
and pins that executable; work rejects a changed binary or source. Eligible
physical Movie/Episode sources need a current probe, a valid same-library root,
a duration up to 12 hours, a size up to 1 TiB, and at least one embedded or
indexed external bitmap track. Limits include 64 tracks, 10,000 decoded display intervals per track,
a 3600-second item timeout, and an 8 MiB complete bundle. The bitmap decoder
also bounds packets, image dimensions, and raster work. Every admitted track
must produce valid nonempty coverage before the bundle is published; tracks
that fail or exceed a budget are not silently skipped. Valid unsupported DVD
features are distinguished from truncated/corrupt data.

External source components have a combined 256 MiB operation budget and IDX
text is bounded to 4 MiB. IDX language ordinals range from 0 to 31. Native SUP
timestamps preserve the initial subtitle-free interval and handle the 90 kHz
wrap; IDX timestamps retain their explicit per-language delay. Displays are
clipped to the video presentation interval. Non-default global alpha, enabled
custom colors and forced-only IDX presentation directives remain unsupported
rather than silently producing an inaccurate timeline.

The existing [media-write overlay](../../deploy/oci/compose.background-previews.yaml)
supports this namespace alongside backgrounds and waveforms. The default
`/media` mount is read-only; the overlay makes that bind writable while the
container root stays read-only.
Backend UID/GID `10001:10001` needs targeted permission to create the source-side
tree. Private namespace directories use `0700`, files `0600`, and a newly
created shared `backdrops` parent `0755`. The overlay only permits writes;
enable the library option or explicitly queue an item to generate material.

Schema 60 adds queue/request receipts and opt-in/event state. Database backup
does not package GSTL, manifest, or ownership files. Restore validates database
relationships without opening, rebuilding, or deleting those sidecars, and
recovered work still checks current authorization. Back up or move source files
and their sidecars together.

Schema 61 adds `item_bitmap_subtitles` with separate SUP/IDX/SUB component
snapshots and immutable public indexes. Backups preserve these records and
validate their component schema, hashes, root relationships and cross-family
index uniqueness. Restoring does not access or recreate the source-side files.

Focused media, library, task, HTTP, migration, and backup checks have passed on
`test-env`, including nine actual PGS/DVD containers. The player build, 35 new
browser cases, 101 existing player regressions, administrator build and 33 Node
checks initially passed. Integrated administrator review then found and fixed
the valid `Warnings:null` response boundary; the final administrator build and
40 Node checks passed. Backup checks cover 45 tests with 246 subscenarios and the
PostgreSQL 17 schema-60 catalog. The integrated Docker journey passed all seven
phases, including real two-track output, desktop/mobile rendering, reuse, Force
replacement, permission-failure preservation, and stale-source hiding. Initial
missing-state/first-generation evidence remains separate from the final run
with an already retained artifact. See the
[live record](../../.artifacts/player-subtitle-timelines/ACCEPTANCE.md) and
[player acceptance](../../web/player/ACCEPTANCE.md) for exact scope and retained
failures; these results do not establish every bitmap feature or container.

Implementation: [HTTP](../../internal/server/subtitle_timelines.go),
[task executor](../../internal/server/subtitle_timeline_executor.go),
[queue](../../internal/library/subtitle_timeline_queue.go),
[storage](../../internal/library/subtitle_timeline_files.go),
[generation](../../internal/library/subtitle_timeline_files_generate.go),
[display extraction](../../internal/media/subtitle_timeline.go), and
[GSTL codec](../../internal/media/subtitle_timeline_codec.go).
