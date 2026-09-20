# Native media processing API

Media processing prepares an embedded subtitle removal or a bitmap subtitle OCR
result for administrator review. Preparation does not publish the result. The
administrator must confirm the current operation revision, source revision and
result hash in a separate apply request. These operations are independent of the
recurring maintenance task definitions.

All routes require a current native administrator cookie. Emby tokens and
application keys are not accepted. Mutations also require the existing
`X-CSRF-Token` and same-origin checks. The store rechecks native administrator
authority inside each transaction. Responses use `Cache-Control: no-store`.

## Routes

| Method | Route | Response |
| --- | --- | --- |
| GET | `/admin/v1/media-operations/capabilities` | Capabilities object |
| GET | `/admin/v1/items/{id}/media-processing` | Current source and selectable streams |
| POST | `/admin/v1/items/{id}/media-operations` | `202 {Operation, Admitted}` |
| GET | `/admin/v1/media-operations` | `{Items, TotalRecordCount, StartIndex, Limit}` |
| GET | `/admin/v1/media-operations/{id}` | `{Operation}` |
| GET | `/admin/v1/media-operations/{id}/review` | `{Operation, Items, TotalRecordCount, StartIndex, Limit}` |
| PUT | `/admin/v1/media-operations/{id}/review` | `{Operation}` |
| GET | `/admin/v1/media-operations/{id}/cues/{ordinal}/image` | Protected PNG bytes |
| POST | `/admin/v1/media-operations/{id}/apply` | `202 {Operation, Admitted}` |
| POST | `/admin/v1/media-operations/{id}/cancel` | `202 {Operation}` |
| POST | `/admin/v1/media-operations/{id}/recover` | `202 {Operation, Admitted}` |

Identifiers in route parameters are lowercase 32-character hexadecimal IDs.
Cue ordinals are canonical nonnegative decimal integers. JSON property names and
query names are case-sensitive. Unknown, duplicate, null, missing required,
incorrectly typed, invalid UTF-8 and lossy Unicode fields are rejected. Mutation
bodies require `application/json`, exactly one object, and at most 512 KiB.
Clients cannot supply a filesystem path, URL, executable, command, model file,
private execution snapshot, queue limit, or arbitrary tool argument.

`Revision` and every tick value are decimal **strings**, including values above
JavaScript's exact integer range. Byte counts in result summaries are also
decimal strings. Stream indices, cue ordinals, progress counters and page counts
are JSON numbers. Do not convert revision or tick strings through a floating
point number when constructing a later request.

## Capability and source discovery

Capabilities contain:

```json
{
  "Enabled": true,
  "Available": true,
  "UnavailableReason": "",
  "MaxConcurrent": 1,
  "MaxQueued": 16,
  "WritableProfiles": ["matroska-v1"],
  "OCR": {
    "Available": true,
    "UnavailableReason": "",
    "Models": [{"Id": "eng", "Language": "eng"}],
    "OutputFormats": ["srt", "vtt"]
  }
}
```

This example illustrates the shape, not a default availability promise. Runtime
admission supplies availability from the operator's configured and verified
tools, models and resources. Merely configuring a model does not prove it can
execute. Disabled or unavailable capabilities remain explicit; clients must not
infer support from a successful HTTP response. A subsequent start can still fail
if resources, source identity, authority or configuration change.

The item source response contains `ItemId`, `MediaSourceId`, `SourceRevision`,
`Container`, `Streams`, and the same `Capabilities` object. Each stream contains
`Index`, `Codec`, `CodecType`, `Language`, `Title`, `IsDefault`, `IsForced`,
`IsHearingImpaired`, `IsExternal`, and `IsTextSubtitleStream`. Original filenames,
storage paths and private probe witnesses are omitted. Select an indexed embedded
subtitle stream, not an external subtitle or attached picture. OCR accepts the
supported PGS and DVD bitmap subtitle codecs; removal uses an admitted writable
container profile.

## Prepare and inspect

Subtitle removal preparation uses all of these fields:

```json
{
  "RequestId": "prepare-subtitle-removal-01",
  "Kind": "remove_embedded_subtitle",
  "MediaSourceId": "current-source-id",
  "SourceRevision": "current-source-revision",
  "StreamIndex": 3,
  "Parameters": {"Profile": "matroska-v1"}
}
```

Supported profile selectors are `matroska-v1` and `mp4-movtext-v1`, subject to the
runtime inventory and the actual container/stream validation. Preparation builds
and validates a candidate using the profile's removal engine before it can become
ready. It does not replace the original file or claim a backup has already been
published.
The [media preservation contract](../development/media-edit-publication.md)
defines Matroska remuxing and the restricted MP4 structural edit.

OCR uses `Kind: "subtitle_ocr"` with the same top-level fields and these exact
parameters:

```json
{
  "ModelIds": ["eng"],
  "OutputFormat": "vtt",
  "Language": "eng",
  "Title": "Reviewed captions",
  "IsDefault": false,
  "IsForced": false,
  "IsHearingImpaired": false
}
```

Choose one to three distinct advertised model IDs (`eng`, `chi_sim`, `chi_tra`)
and `srt` or `vtt`. Model aliases are not accepted. Language and title can be
empty. Language is at most 32 ASCII letters, digits or hyphens; title is at most
512 UTF-8 bytes without control characters or surrounding whitespace. These
parameters are recorded with the operation. Model IDs are public selectors;
model filenames and executable paths are server-owned.

`RequestId` is required, contains 1–128 UTF-8 bytes, and has no controls or
surrounding whitespace. Retain the exact request ID and payload after an uncertain
network outcome. Replaying an identical receipt returns its existing operation
with `Admitted: false`; reusing the ID for a different request conflicts. A
successful `202` confirms admission, not completed execution or publication.

Operation objects expose `Id`, `Kind`, `State`, `Revision`, `ItemId`, `LibraryId`,
`MediaSourceId`, `SourceRevision`, `StreamIndex`, `Parameters`, `Progress`,
`ResultSummary`, `ResultHash`, `PublicationPhase`, timestamps, bounded error code
and message, `CanCancel`, `CanReview`, `CanApply`, `CanRecover`, `Applied`, and
`TargetPresent`. `Progress` contains `Stage`, `Processed`, and `Total`; a total of
zero does not establish a percentage. Private journal documents, credential
identities, worker tokens, receipt fingerprints and storage paths are omitted.

States are `queued`, `running`, `ready`, `applying`, `completed`, `failed`,
`cancelled`, `interrupted`, `stale`, and `recovery_required`. Use current action
flags for the UI and still handle a rejected concurrent action. A changed source
can leave a useful OCR review draft readable while making application invalid.
Asynchronous removal failures use `preservation_unproven` when the retained media
cannot be proven unchanged, or `resource_limit` when size or complexity limits
reject the source. Their messages remain fixed and do not expose tool diagnostics.

Removal summaries whitelist `ContainerProfile`, `RemovedStreamIndex`,
`PreservedStreamCount`, `OriginalBytes`, `CandidateBytes`, and `BackupRetained`.
The administrator should review the selected removed stream and the preservation
and storage implications before applying. `BackupRetained` describes an actual
retained original after publication; it is not permission to remove that backup.

OCR summaries whitelist `EngineSHA256`, `ModelID`, `ModelSHA256`,
`Models` (`ID`, `SHA256`), `CueCount`, `WarningCount`, `Warnings`, and, after publication,
`AppliedStreamIndex`, `AppliedContentSHA256`, and `AppliedFormat`. Hashes identify
the actual engine, models and published content. Unknown executor summary keys
are never echoed. `Models` and `Warnings` are arrays, including when empty.

The list accepts optional `ItemId`, `Kind`, `State`, `StartIndex`, and `Limit`.
The cue review list accepts only `StartIndex` and `Limit`. Default `Limit` is 50;
valid limits are 1–100. `StartIndex` is a canonical nonnegative decimal integer
up to 2147483647. Query values appear at most once. Other routes reject all query
parameters, including an empty query delimiter. JSON responses are capped at
2 MiB; request a smaller page after `response_limit`.

## OCR review and image evidence

Each review cue contains `Ordinal`, `OriginalStartTicks`, `OriginalEndTicks`,
`OriginalText`, `StartTicks`, `EndTicks`, `Text`, `Included`, nullable `Confidence`,
`Warnings`, `ImageSHA256`, `IsForced`, and `IsHearingImpaired`. Confidence is on a
0–100 scale when present. Original recognition values and bitmap evidence remain
unchanged when an administrator corrects the editable values.

Update one to 100 distinct cues atomically:

```json
{
  "Revision": "4",
  "Edits": [
    {"Ordinal": 0, "StartTicks": "10000000", "EndTicks": "30000000", "Text": "Hello", "Included": true}
  ]
}
```

Every cue edit requires all five fields. Text is at most 4096 UTF-8 bytes and
allows line breaks but no other control characters. An included cue cannot be
empty. Ticks are nonnegative, end is strictly later than start, and publication
validates the complete result against the indexed source duration. Valid overlap
is preserved. A successful edit increments the operation revision and changes
its result hash. Apply must use the newly returned values. A stale edit returns
`409 media_operation_conflict` without partially changing cues.

Images use the same administrator cookie and can be displayed with a same-origin
image element. The route returns at most 1 MiB of PNG data, `nosniff`, and a digest
ETag. Conditional responses reauthorize and reread the stored image first. A
revoked session cannot reuse a matching ETag to obtain a successful response.

## Apply, cancel and recovery

Apply and recovery use this exact confirmation body:

```json
{
  "Revision": "5",
  "SourceRevision": "reviewed-source-revision",
  "ResultHash": "64-lowercase-hex-characters-from-the-reviewed-operation",
  "RequestId": "apply-reviewed-result-01"
}
```

The example hash is descriptive; actual requests require exactly 64 lowercase
hexadecimal characters. Apply confirms the exact reviewed result. Subtitle
removal replaces the selected source only after publication checks and retains
the original for the operation's recovery protocol. OCR publishes a server-owned
text subtitle; preparing or editing a draft does not make it a playable track.
Recovery is an explicit action for a removal operation in `recovery_required`,
using the current displayed confirmation. It does not accept user-specified
paths or an arbitrary rollback target. Retain apply/recovery receipt IDs and
payloads across uncertain responses just as for preparation.

Cancel uses only `{"Revision":"5"}`. Cancellation is a request: running process
and storage workers must finish their joined shutdown before the final state is
reported. Publication can cross a point where cancellation is no longer allowed.
Refresh the operation after a state or revision conflict; do not assume a failed
HTTP response means the source was unchanged.

Error envelopes follow `{Error: {Code, Message, Fields?}, RequestId}`. Invalid
fields return 400; unsupported content type returns 415; missing resources return
404. Conflict codes are `media_operation_conflict`, `source_changed`,
`media_operation_state`, `media_operation_busy`, and `recovery_required` (409).
Unavailable execution returns `media_operation_unavailable` (503). Authentication
and native administrator errors preserve the existing 401/403 conventions.
Internal errors and runtime errors do not return SQL, commands, credentials or
private filesystem paths.
