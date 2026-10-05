# Per-track audio waveforms

Goby generates a source-aligned peak/RMS envelope for each original internal
audio stream of a Movie or Episode. These are measured audio amplitudes, not
decorative curves, fingerprints, spectrograms, or loudness certification.
Generation is opt-in; reading a descriptor or waveform never schedules work.

The player uses the handoff's single-color bar presentation of measured peaks,
with up to 180 display bins. It keeps real invalid gaps and digital silence
distinct, and adapts the number of bars to the available width. RMS data remains
in the file and API; the media-information view has no separate RMS overlay or
technical legend. Its accessible description explains the displayed values.

Waveforms are permanent media sidecars in an independent namespace:

```text
<source parent>/backdrops/goby-waveforms/<sha256(exact source basename)>/
  .owner.json
  manifest.json
  gen-<generation id>.gawf
```

The basename includes its extension. Association does not depend on item IDs or
absolute parent paths. Moving a source folder with its sidecars on the same
filesystem or rebuilding the database can preserve association after reindexing.
Renaming a source changes the directory key. Source identity includes file and
audio/timeline facts; copying to a different filesystem can change that identity.

No LRU, expiry, cache clearing, scan, or source/configuration change deletes or
automatically replaces completed waveform files. Ordinary requests reuse even
retained stale material. Only a new explicit `Force` operation replaces it after
successful complete generation. Failed/canceled work retains the previous
publication. Successful Force can retire the validated superseded generation;
it does not clean arbitrary unknown files or preserve every historical version.

Unlike background clips, stale waveforms are not served: an envelope aligned to
an old source must not be drawn against a changed timeline. It stays on disk
until explicit regeneration. Unknown ownership, damaged manifests/payloads, and
conflicting files are errors; Force does not authorize overwriting untrusted
material.

## Authenticated consumer API

```text
GET /emby/Items/{Id}/AudioWaveforms
GET /emby/Items/{Id}/AudioWaveforms/{StreamIndex}?buckets=4096&tag=<artifact tag>
```

The descriptor accepts only optional `UserId` and `api_key` query parameters.
The binary route additionally requires `buckets` and `tag`. Parameter names are
case-sensitive, must be unique and nonempty, and unknown parameters or a bare
query delimiter are invalid. `StreamIndex` is canonical decimal `0..4095`.
`buckets` is exactly one of `512`, `1024`, `2048`, or `4096`; there is no default
level. Use the descriptor's returned level URL instead of constructing a tag.

Normal media authorization applies. Ordinary users need visible media and
playback permission. `UserId` selects state projection, not authorization, and
cannot grant access to another user. Application-key media rights remain the
authenticated key's rights, narrowed by any selected user/library visibility.
Revoked credentials cannot continue reading.

The descriptor returns `Available` and, when applicable, `Stale`, `MediaSourceId`,
`SourceVersion`, `DurationTicks`, and `Streams`. Each stream has `StreamIndex`,
`Channels`, `SampleRate`, optional `ChannelLayout`, and `Levels`; each level
contains `BucketCount` and `Url`. URLs preserve an explicitly supplied `UserId`
but do not copy `api_key`; the client must still authenticate. No host path is
published.

- Missing material, folders, other item types, and unprobed items return
  `{"Available":false}`.
- Retained material for an obsolete source returns
  `{"Available":false,"Stale":true}` without an old timeline or level URLs.
- Current valid material includes the source identity, whole-item duration,
  original stream indexes, and four available levels per stream.

`SourceVersion` is a `waveform-source-v1-<SHA256>` snapshot fingerprint. It is
neither a database revision nor the artifact tag. The tag binds the generation
and payload; the binary response ETag also distinguishes stream and level.
Descriptors use `Cache-Control: private, no-store`. Binary responses use
`application/octet-stream`, `X-Content-Type-Options: nosniff`, and
`Cache-Control: private, no-cache`, with conditional and range-request support.

## GAWL binary response

The public response is one **GAWL** level. The source-side complete bundle uses
the different **GAWF** storage format; clients do not parse the bundle directly.
Every numeric header and sample field below is little-endian.

| Offset | Type | Value |
| ---: | --- | --- |
| 0 | 4 bytes | ASCII `GAWL` |
| 4 | uint16 | Version `1` |
| 6 | uint16 | Header size `32` |
| 8 | uint32 | Original audio stream index |
| 12 | uint32 | Bucket count `N` |
| 16 | uint64 | Whole-item duration in ticks |
| 24 | uint32 | Validity bitmap size `N / 8` |
| 28 | 4 bytes | Reserved, all zero |

The header is followed by `N` interleaved pairs of `Peak uint16, RMS uint16`,
then the `N/8`-byte validity bitmap. Exact total length is `32 + 4*N + N/8`.
Bucket `i` is valid when `bitmap[i/8] & (1 << (i%8))` is nonzero, with the least
significant bit first. Invalid buckets must have zero peak and RMS. A valid
zero/zero pair means measured digital silence; an invalid bucket means no
covered samples and must remain a gap. RMS cannot exceed peak.

Values are linear amplitudes quantized as
`round(clamp(amplitude, 0, 1) * 65535)`. Divide by 65535 for linear amplitude;
the integers are not dBFS values. Peak is the maximum absolute sample across
all channels in a bucket. RMS is the square root of summed channel energy
divided by sample-frame count times channel count. Opposing channel polarity
does not cancel the waveform as a mono downmix would.

The time axis is the entire item's `[0, DurationTicks)` interval, evenly divided
into `N` buckets; 10,000,000 ticks equal one second. Each stream retains its
presentation offset. Leading delay and genuine holes stay invalid, and negative
or post-item samples are excluded. The extractor uses original timestamps and
validated presentation origin rather than trimming and restarting each track at
zero. Untrusted overlap/backward timestamps fail extraction.

The 4096-bucket base accumulates actual peaks, energy, and sample counts before
quantization. Lower levels aggregate those quantities; their RMS is not an
arithmetic average of already quantized finer RMS values. Clients should validate
header identity, lengths, reserved bytes, duration/stream/level agreement, bitmap
consistency, and amplitude invariants before rendering.

## Administrator and task API

`LibraryOptions.EnableAudioWaveformGeneration` defaults to false for eligible
`movies`, `tvshows`, and `mixed` libraries. It is independent of background clips,
BIF previews, and intro detection. An explicit selected-item request does not
require automatic generation to be enabled.

| Method and path | Contract |
| --- | --- |
| `GET /admin/v1/audio-waveforms/items` | List queue state with optional `LibraryId`, `SearchTerm`, `State`, `StartIndex`, and `Limit`. |
| `GET /admin/v1/items/{id}/audio-waveforms` | Read one item state plus current `Artifact`; accepts no query. |
| `POST /admin/v1/media-analysis/runs` | Queue/start a waveform selection with the complete body below. |

```json
{
  "Kind": "waveform",
  "RequestId": "client-generated-unique-request-id",
  "LibraryIds": [],
  "ItemIds": ["example-item-id"],
  "Force": false
}
```

Native routes require the administrator session cookie; writes also require
same-origin and `X-CSRF-Token` checks. The strict UTF-8 JSON body has exact fields
and a 64 KiB limit. Library IDs are limited to 64, item IDs to 256, and each ID
to 128 UTF-8 bytes, without duplicates. Empty collections select all eligible
scope; when both are supplied, items must belong to the selected libraries.
`RequestId` is nonempty and at most 128 bytes.

Success returns `202` with `RunId`, `TaskId`, `Admitted`, and `Queued`. Retry an
uncertain result with the same request ID and actor/selection. Durable receipts
prevent duplicate work; changing selection under that ID conflicts. A Force
operation identity is retained in the publication so replay/recovery cannot
regenerate an already completed operation. Current source, task fence, and
administrator/session authority are checked again before protected work.

The task key is `media.audio_waveform_generation`. It uses the existing task
center, progress, cancellation, recovery, and shared media-analysis slot.
Untouched scheduling uses daily and `AudioWaveformGenerationRequested` triggers;
administrator schedule edits are preserved. Eligible scans/enablement can request
automatic processing. Batches yield after eight items when event continuation
is available; a manual child drains accepted work when that event is removed.
Generic task start does not introduce a Force parameter. Cancellation settles
earlier requests while preserving later arrivals; interrupted work resumes with
fresh authorization. Reads never create directories or queue writes.

The inventory returns `{Items, TotalRecordCount, StartIndex, Limit}`. Defaults
are 0/50; limits are `StartIndex <= 1000000` and `1 <= Limit <= 200`.
`SearchTerm` is at most 256 UTF-8 bytes without NUL. States are `missing`,
`pending`, `running`, `ready`, `failed`, and `cancelled`.

Item detail includes `ItemId`, `LibraryId`, `Name`, `SourceRevision`,
`DurationTicks`, `AudioStreamCount`, `State`, `RequestedRevision`,
`CompletedRevision`, `RunId`, `Reused`, `ErrorCode`, and request/start/finish
timestamps. Revision values are strings and timestamps may be null. Native
`Artifact` includes `Available`, `Stale`, and optional `Profile`, `Generation`,
`DurationTicks`, `Size`, and track summaries with sample counts/coverage.
Queue `ready` is not proof of a current source-aligned file; inspect `Artifact`.

| Status/code | Meaning |
| --- | --- |
| `400 invalid_input` | Invalid query, stream index, level, tag shape, or input body. |
| `401` / `403` | Authentication, playback, administrator, origin, or CSRF rejection. |
| `404 not_found` | Missing/invisible item or requested track/level. |
| `404 audio_waveform_changed` | The requested version tag no longer matches. |
| `404 audio_waveform_stale` | Retained data belongs to an obsolete source axis. |
| `409 audio_waveform_conflict` | Conflicting request receipt or untrusted/damaged stored material. |

## Generation, deployment, and recovery limits

The Linux executor uses the admitted FFmpeg 9.0.1 binary and timestamp evidence.
It decodes original stream indexes sequentially without downmixing/resampling
the source merely to make a graph. Every admitted internal track must succeed
before a complete bundle is published; later or failing tracks are not skipped.
Limits include 64 tracks, 1–64 channels per track, sample rates up to 384000 Hz,
source duration up to 12 hours, source size up to 1 TiB, a 3600-second per-item
timeout, and a 4 MiB bundle. Missing time bases, non-finite PCM, source/tool
changes, or untrusted timestamps fail rather than fabricate samples.

The existing [media-write overlay](../../deploy/oci/compose.background-previews.yaml)
also enables this namespace. Default `/media` and container root settings stay
read-only unless the overlay is selected. Backend UID/GID `10001:10001` needs
targeted source-directory write permission. Private namespace directories use
`0700`, files `0600`, and the shared newly created `backdrops` parent `0755`.

Schema 58 stores only waveform queue/request receipts and related policy/event
state. Database backup does not package source-side GAWF/manifest/ownership
files. Restore validates database relationships without opening, rebuilding,
or deleting sidecars, and recovered work still requires current authorization.
Back up/move source files and their sidecars together.

Focused browser, HTTP, task, real FFmpeg, PostgreSQL recovery, and all eight live
Docker phases passed. The [live result](../../.artifacts/player-waveforms/results.json)
records original track indexes, all four levels, real UI rendering, reuse,
failed-Force preservation, stale-axis rejection, and explicit replacement.
Exact scope is recorded in [ACCEPTANCE.md](../../web/player/ACCEPTANCE.md).
Actual codec cases include AAC,
AC3, EAC3, FLAC, Opus, and PCM, plus delayed/multitrack/opposing-polarity and
cancellation checks. This does not imply every codec or source is accepted.

Implementation: [HTTP](../../internal/server/audio_waveforms.go),
[queue](../../internal/library/audio_waveform_queue.go),
[source-side storage](../../internal/library/audio_waveform_files.go),
[generation](../../internal/library/audio_waveform_files_generate.go), and
[sample aggregation](../../internal/media/audio_waveform_accumulator.go).
