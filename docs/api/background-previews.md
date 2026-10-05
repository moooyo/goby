# Persistent background previews

Goby can generate a short silent background video for an indexed Movie or
Episode. The result is a media sidecar beside the original source, independent
of the disposable BIF/analysis cache. The player reads existing artifacts; an
ordinary page view, metadata lookup, or stream request never generates one.

This is a Goby extension, separate from the previously accepted ThemeMedia/
trailer integration. Its 12-phase real Docker acceptance, 40 browser cases,
focused task/HTTP checks, actual FFmpeg output, and PostgreSQL 17 recovery passed
on `test-env`. Exact coverage and retained reruns are recorded in the
[player acceptance record](../../web/player/ACCEPTANCE.md).

## Persistence and source association

For each exact original filename, including its extension, the storage layout is:

```text
<source parent>/backdrops/goby/<sha256(exact source filename)>/
  .owner.json
  .generation.lock
  manifest.json
  gen-<unique generation id>.mp4
```

The hash uses the filename, not a database item ID or an absolute host path.
Different episodes in one directory therefore have separate artifacts. Moving
the source folder together with its sidecars, or recreating the library/database,
preserves the file association. Renaming the source filename changes the lookup
directory. The manifest and ownership checks prevent unrelated files from being
claimed as generated previews.

Completed previews are persistent. There is no LRU, expiry, cache-clear, or
automatic source/profile-change deletion for these files. Disabling generation,
editing the output profile or manual start, rescanning, and changing the source
do not replace a completed preview. Normal work reuses it. Only a claimed
explicit `Force=true` operation may regenerate and replace it. A failed or
canceled regeneration preserves the previous playable artifact. Successful
Force may retire the validated superseded generation; this is not an archive
of every historical generation.

Publication writes a new temporary file, validates it, creates a new immutable
generation filename, and atomically switches the manifest. Unknown ownership,
invalid manifests, or conflicting existing material cause an error without
automatically clearing those materials. Force is not permission to erase
unknown files. Interruption/recovery uses the operation identity to avoid
repeating an already published explicit regeneration.

`SourceChanged` reports a difference between the manifest's captured source
snapshot and the current indexed source snapshot. It does not hide the old
preview and is not a fresh hash check of the original video's current bytes.
Existing artifacts remain playable until explicit replacement. A database
backup does not contain these source-side MP4s; include the sidecars in media
backup and folder-move operations.

## Administrator controls and configuration

`LibraryOptions.EnableBackgroundPreviewGeneration` defaults to `false` and is
independent from intro detection and BIF seek previews. It is available for
`movies`, `tvshows`, and `mixed` libraries. Enabling it, completing an eligible
scan, and explicit requests can notify the background task. Disabling it stops
automatic selection without deleting completed clips. Explicit selected-item
generation does not require the automatic library option.

The native administrator UI has a background-preview section in Media Analysis,
a per-library automatic option, and a Movie/Episode item dialog for an optional
start time, generation, and Force regeneration. Tasks supplies progress, failure,
cancel, and recovery controls. Settings/profile edits do not themselves generate
or replace a clip.

Native routes require the current administrator session. Writes retain native
origin and `X-CSRF-Token` checks and revalidate administrator authority when
committing changes or publishing work. These paths are relative to the server
root, without the `/emby` prefix.

| Method and path | Contract |
| --- | --- |
| `GET /admin/v1/background-previews/configuration` | Return `Revision`, `Profile`, `Defaults`, and `UpdatedAt`. No query parameters. |
| `PUT /admin/v1/background-previews/configuration` | Submit complete `{Revision, Profile}` and return the current configuration. |
| `GET /admin/v1/background-previews/items` | List queued/item state using `LibraryId`, `SearchTerm`, `State`, `StartIndex`, and `Limit`. |
| `GET /admin/v1/items/{id}/background-preview` | Read item/source revision, editorial start, queue state, and the current artifact. No query parameters. |
| `PUT /admin/v1/items/{id}/background-preview` | Submit `{Revision, SourceRevision, StartTicks}` to save or clear the editorial start. |
| `POST /admin/v1/media-analysis/runs` | Submit a background selection/request receipt and start task processing. |

The independent profile defaults and bounds are:

| Field | Default | Allowed values |
| --- | --- | --- |
| `DurationSeconds` | 25 | Integer 5–60 |
| `MaxWidth` | 1280 | 640, 960, 1280, or 1920 |
| `VideoBitrate` | 1500000 | Integer 250000–8000000 bits per second |
| `MaxItemRuntimeSeconds` | 1200 | Integer 30–7200 seconds of processing time per item |

`MaxItemRuntimeSeconds` is the generation timeout, not a maximum movie duration.
The output fits a display box of `MaxWidth` by `MaxWidth * 9 / 16`, preserving
the source display aspect ratio. A 1280 width setting is not a promise that
every source produces exactly 1280x720. Bitrate is the encoder target. The
default duration differs from the original 24-second mockup and can be configured
to 24 through the same profile.

Example profile update, using the current revision from GET:

```json
{
  "Revision": "1",
  "Profile": {
    "DurationSeconds": 25,
    "MaxWidth": 1280,
    "VideoBitrate": 1500000,
    "MaxItemRuntimeSeconds": 1200
  }
}
```

Revisions are canonical decimal strings. Configuration revisions are positive;
untouched item settings can have revision `"0"`. An unchanged configuration does
not advance its revision. A profile update does not reset completed queue entries
or request replacement work.

Per-item editing also requires the current opaque `SourceRevision`:

```json
{
  "Revision": "0",
  "SourceRevision": "opaque-current-source-revision",
  "StartTicks": 0
}
```

Ticks are 100-nanosecond units: 10,000,000 ticks equal one second. A manual start
must satisfy `0 <= StartTicks < DurationTicks`; zero is valid. `StartTicks: null`
clears the manual choice and restores automatic interval selection for future
generation. Both forms edit settings only. Generation additionally requires the
selected interval to precede known credits and the source end.

Configuration and item-edit bodies require exact JSON field names and complete
fields; unknown/duplicate fields are rejected. Use `application/json`. The
configuration/item body limit is 32 KiB.

## Task requests, idempotence, and recovery

The task key is `media.background_preview_generation`. It uses the same serial
analysis worker slot as intros and seek previews, while keeping its own durable
queue and request identities. An untouched task has daily and
`BackgroundPreviewGenerationRequested` event triggers. Existing administrator
task configuration remains authoritative.

To request one item, submit all five fields:

```json
{
  "Kind": "background",
  "RequestId": "client-generated-unique-request-id",
  "LibraryIds": [],
  "ItemIds": ["example-item-id"],
  "Force": false
}
```

`LibraryIds` allows at most 64 unique IDs and `ItemIds` at most 256. Both empty
select all eligible physical Movies/Episodes; supplying both constrains the
items to the selected libraries. IDs are nonempty, bounded identifiers.
`RequestId` is nonempty, at most 128 UTF-8 bytes, and has no control characters
or surrounding whitespace. This request uses strict UTF-8 JSON, a 64 KiB body
limit, and no query parameters.

Successful task admission returns HTTP `202` with `RunId`, `TaskId`, `Admitted`,
and `Queued`. The selected work and its system event are persisted before task
admission, so a later admission failure does not erase the queued request.
Retry an uncertain response with the same `RequestId` and identical selection.
The same actor/selection receipt is replayed without a second Force operation;
changing the actor or selection while reusing that ID returns a conflict.

Manual requests retain the initiating administrator/session identity. Revoking
that authority prevents subsequent protected source access or publication; a
detached job record alone grants no authority. Task fences and current queue
revisions also prevent an obsolete claim from publishing over a newer request.
The Force operation ID is stored with the artifact, so recovery of that same
operation reuses a completed publication instead of generating it again.

The executor can yield after eight items and notify a continuation so other
analysis work can run. If its continuation event has been removed, it drains
remaining queued work rather than leaving the remainder dependent on an absent
event. Progress/cancellation uses the generic task API:

```text
GET  /admin/v1/tasks
GET  /admin/v1/tasks/{id}
POST /admin/v1/tasks/{id}/runs
GET  /admin/v1/tasks/{id}/runs
GET  /admin/v1/task-runs/{id}
POST /admin/v1/task-runs/{id}/cancel
```

The generic start accepts an optional `RequestId`, not item selection or Force.
It processes the automatic policy and queued work. Native cancel accepts `{}`
and is idempotent. A stop settles earlier outstanding requests as canceled;
requests arriving after that stop remain pending. Server interruption leaves
unfinished work pending for recovery rather than deleting artifacts. The
existing Emby scheduled-task start/stop routes can also control this task, but
do not introduce a Force parameter.

Upgrading profile support does not automatically requeue old `failed` entries
that were rejected by the previous profile gate. If the saved source already
has verified RPU evidence and only the profile gate rejected generation, an
administrator can use the existing generate-missing-clips action or submit a
new background request with `Force: false`. If the saved RPU validation reason
is `decoder_error`, first complete **Libraries > Refresh media details**
(`ForceProbe: true`) to rebuild source evidence, then request generation again.
See [scan modes](admin-scans.md). Existing artifacts are reused first; the
upgrade needs no cache clearing, sidecar migration, or new UI switch. Use
`Force: true` only when deliberately replacing an existing artifact.

## Status and errors

Item detail contains `ItemId`, `LibraryId`, `Name`, `Revision`, `SourceRevision`,
`DurationTicks`, `StartTicks`, `State`, `RequestedRevision`, `CompletedRevision`,
`RunId`, `Reused`, `ErrorCode`, `RequestedAt`, `StartedAt`, `FinishedAt`, and
`Artifact`. Timestamps may be null. States are `missing`, `pending`, `running`,
`ready`, `failed`, and `cancelled`.

The list response is `{Items, TotalRecordCount, StartIndex, Limit}`. Pagination
defaults to 0/50; `StartIndex` is 0–1000000 and `Limit` is 1–200. `SearchTerm`
matches names without case sensitivity and is limited to 256 UTF-8 bytes.
The list reads database state only, so `missing`/`ready` does not by itself prove
whether a persistent file exists after a database reset or external file change.
Read item `Artifact` or the consumer descriptor for current file availability.

| Status/code | Meaning |
| --- | --- |
| `400 invalid_input` / `415 unsupported_media_type` | Invalid request shape, values, or content type. |
| `401` / `403` | Session, administrator, media access, origin, or CSRF checks failed. |
| `409 background_preview_conflict` | Settings/source/receipt preconditions changed; reload or resolve the request before retrying. |
| `409 source_changed` | The source changed across a native operation. |
| `409 task_disabled` / `503 task_unavailable` | The selected task cannot currently admit work. |
| `503 analysis_unavailable` | The native background service or source work is unavailable. |

Queue error codes include `cancelled`, `server_interrupted`, `processing_limit`,
`material_conflict`, `request_changed`, `request_authority_revoked`,
`unsupported_source`, `unusable_pictures`, `source_changed`, `source_unavailable`,
`dependencies_unavailable`, `media_directory_not_writable`, and
`generation_failed`. These are operational results, not host paths or raw tool
command lines.

## Consumer reads and player behavior

Authenticated consumer endpoints are:

```text
GET /emby/Items/{Id}/BackgroundPreview
GET /emby/Items/{Id}/BackgroundPreview/stream.mp4
```

Only authorized physical Movie/Episode artifacts are returned. A Series or
Season does not inherit a generated episode clip: the player requests the
focused Episode for its episode stage. Missing artifacts return HTTP `200`
with `{"Available":false}` from the descriptor. Conflicting/corrupt material
can return an error and is not silently reported as an ordinary missing file.
The player proceeds to the next configured source or still artwork after an
unavailable descriptor or failed decode.

An available descriptor includes `Available`, `StartPositionTicks`,
`RunTimeTicks`, `Width`, `Height`, `Size`, optional `SourceChanged`, and
`StreamUrl`. Zero/false metadata can be omitted; callers must not require
`StartPositionTicks` to be present when the actual start is zero. No host path,
operation ID, or file-management action is exposed. `StreamUrl` contains the
current version tag and uses the same authenticated player origin.

Descriptor responses use `Cache-Control: private, no-store`. Streams are finite
`video/mp4` with `X-Content-Type-Options: nosniff`, a version ETag, and
`Cache-Control: private, no-cache`. They support byte ranges and conditional
requests. If a `tag` is supplied, exactly one matching current tag is required;
a stale tag returns `404 background_preview_changed`. These reads create no
task, PlaybackInfo negotiation, viewing history, or encoding session.

The player's default source order is theme video, generated clip, then local
trailer. Users may reorder that list or choose still artwork only. Preferences
persist per user. Moving backgrounds run only on eligible fine-pointer desktop
stages with reduced motion disabled; leaving the stage, hiding the page,
switching to mobile/reduced motion, or selecting still-only releases the source.
Playback starts muted. An explicit user gesture may enable sound only when an
existing theme/trailer contains an actual audio stream; generated previews keep
their audio-free output contract and offer no sound track. A failed candidate
falls through to the next source. This frontend sound control does not change
the generated-file profile or cause a new generation job.

## Encoding and deployment boundaries

The generation runtime is Linux software FFmpeg/ffprobe. It produces H.264
High Profile, 8-bit `yuv420p`, 24 fps MP4 with no audio, subtitle, or data stream,
bounded to 64 MiB. Output is probed and fully decoded before publication.
Startup capability inspection proves a synthetic SDR encode/decode path;
it does not establish every source codec, HDR path, or GPU capability.

Interval selection uses a manual start first, otherwise a validated intro end
plus three seconds, otherwise 5% of source duration clamped to 30–300 seconds.
The source end and valid CreditsStart bound the clip, shortening it if needed.
Detected Episode intros require their support-source evidence to remain valid.
The automatic path can try at most three nearby candidates when material is
predominantly black/static. A manual start is never silently moved. This is a
bounded usability check, not a best-scene or aesthetic scoring system.

Supported SDR inputs and adequately described HDR10/PQ or HLG can be encoded;
those HDR paths use software tone mapping to BT.709 SDR. The subsequent Dolby
Vision integration reuses strict software HEVC decoding, Vulkan/libplacebo
processing, and CPU H.264 encoding. The October 4 accepted subset is Profile 8.1
and complete Profile 7 MEL. The October 5 extension adds Profile 5, Profile 8.4,
and Profile 8.2, with separate native AMD media acceptance on authorized CT104.
These profiles have distinct native base-layer representations:
IPTPQc2 for Profile 5, HLG for Profile 8.4, and BT.709 SDR for Profile 8.2. The
renderer consumes native pixels and per-frame RPU data before resizing or frame
rate conversion; it does not relabel those inputs as PQ. Required source and
per-frame RPU/layer evidence must pass the strict renderer. FEL residual
reconstruction remains outside the selected scope.

Dolby Vision work requires currently enabled Vulkan tone mapping, an explicitly
admitted DRM render device, and the pinned strict toolchain. Device capability
is inspected during admitted generation and current administrator policy is
rechecked on each use. Ordinary SDR availability is not a DV capability gate.
Missing capability, contradictory metadata, or an unsupported source fails
generation without selecting a silent base-layer fallback. Existing saved
clips retain their normal persistence policy.

The DV input decoder explicitly enables `-err_detect crccheck+explode` so RPU
CRC corruption cannot be ignored. This does not change ordinary SDR/HDR input
options. The source scanner permits only the exact `hevc_mp4toannexb` diagnostic
`No parameter sets in the extradata` during syntax validation, after complete
access-unit/CRC evidence passes; all other diagnostics still fail validation.
A successful scan records optional `InBandParameterSets` evidence for those
MP4s. Generation then decodes from the source beginning to retain their in-band
VPS/SPS/PPS, trims to the selected interval, and applies strict DV processing.
Other sources retain the existing fast-seek path. The pinned toolchain and
private renderer patch are unchanged.

The October 4 acceptance covered eight actual DV profile/fixture scenarios in
11 passing executions, including
three output-retaining reruns, on explicitly authorized CT104. There were no
failures or skips. They include Profile 8.1, Profile 7 MEL, neutral RGB checks,
RPU-negative controls, cancellation, and a 25-second window from an original
28-second source. Bad-CRC and valid-CRC invalid-mapping P8 inputs produced zero
output. A residual P7 BL+RPU input without EL was rejected at admission; this
does not establish runtime reconstruction of full EL/FEL material. See the
[DV research and implementation record](../development/dolby-vision-background-research.md)
for evidence and limitations. This proves the selected generation path, not
every Dolby profile, device, decoder, or a universal GPU deployment.

The October 5 final matrix passed all 21 required cases with no skips: 12
new-profile analytic color/window/rejection/cancellation cases, two official
Profile 5/8.4 windows beginning at 60 seconds, one same-scene cross-profile
comparison, and six legacy regressions. The separate remote focused suite
passed 41 top-level tests containing 153 subtests, and the Go backend build
passed. The [final case summary](../../.artifacts/player-dv-profiles-20261005/final/evidence/case-summary.json)
and [closure record](../../.artifacts/player-dv-profiles-20261005/final/closure.json)
retain identities and cleanup evidence; the full history retains four failed
executions before the fixes, not just final passes. Profile 8.2 coverage uses
original actual-SDR-base/non-identity-RPU material with an independent scalar
oracle, without a public commercial-source reference. This is native AMD media
acceptance, not a new Docker deployment or all-device/profile certification.
All profiles still produce the same silent H.264/BT.709 SDR sidecar; this is not
original-file browser playback or Dolby Vision output.

The default Compose media bind remains read-only. To generate, use the optional
[`compose.background-previews.yaml`](../../deploy/oci/compose.background-previews.yaml)
overlay and targeted source-directory permissions for backend UID/GID
`10001:10001`. The private `goby`/per-source directories are `0700`, and files
are `0600`; a newly created shared `backdrops` parent is `0755`. A writable bind
does not bypass host filesystem permissions. The container root filesystem
remains read-only. See the [operator guide](../../deploy/oci/README.md).

Migration `0057_background_previews.sql` stores independent profile, item
settings, durable queue, and request-receipt state. The schema-57 backup catalog
preserves those records, including historical revoked/deleted identities where
valid; validation checks structure and relationships without opening media or
reauthorizing past actions. Actual PostgreSQL 17 verification passed 22 semantic
cases and a complete background-state restore. Execution after restore checks
current authority again.

Database backup/restore neither packages nor cleans or overwrites source-side
MP4, manifest, and ownership files. Those database records track work; removing
them does not remove a media sidecar. Preserve the original media and its
sidecars together in a separate media backup. Post-recovery read-only checks
confirmed schema 57, API 200, clip range 206, the retained MP4 hash, and no
residual FFmpeg/ffprobe processes.

Implementation references:
[HTTP](../../internal/server/background_previews.go),
[task executor](../../internal/server/background_preview_executor.go),
[file publication](../../internal/library/background_clip_files.go),
[encoding](../../internal/media/background_clip.go), and
[player source selection](../../web/player/src/components/ThemePreview.tsx).
