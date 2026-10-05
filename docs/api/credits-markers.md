# Credits markers

Goby's established credits path stores an administrator-supplied start for a
Movie or Episode and publishes `CreditsStart` to the player. Manual/import
storage and explicit source chapters retain their existing contract.

Automatic credits detection for movies and TV is implemented and has passed its
separate seven-phase real Docker acceptance plus focused algorithm, media,
HTTP, browser, library, and recovery checks. Those results are distinct from
the earlier manual-marker acceptance. Neither path rewrites the media file.

## Administrator routes

These native routes use `/admin/v1`, not the `/emby` prefix. They require a
current administrator session. Writes retain the native origin and
`X-CSRF-Token` checks used by the other administrator endpoints. There are no
query parameters.

| Method and path | Behavior |
| --- | --- |
| `GET /admin/v1/items/{id}/credits` | Read current source identity, revision, duration, stored override, and effective point. |
| `PUT /admin/v1/items/{id}/credits` | Save one source-bound start with both revision preconditions. |
| `DELETE /admin/v1/items/{id}/credits` | Clear the override with both revision preconditions and return the new state. |

Only non-folder `Movie` and `Episode` items with an available indexed source,
positive duration, and stream information are eligible. The source is reopened
and revalidated before editing; a catalog row alone does not establish current
file availability. Series, Seasons, and other item types are not marker targets.

The administrator media-item list exposes the editor for Movies and Episodes.
The editor displays current duration and effective/stored values, accepts start
time in seconds, and converts it to ticks. It guards unsaved edits and requires
an explicit reset action. On a revision conflict, reload current values before
resubmitting rather than overwriting another edit.

## Read response

All three successful operations return HTTP `200` with `CreditsDetail`:

```json
{
  "ItemId": "example-item",
  "MediaSourceId": "mediasource_example-item",
  "SourceRevision": "opaque-current-source-revision",
  "Revision": "1",
  "DurationTicks": 1800000000,
  "Automatic": null,
  "Effective": { "StartTicks": 1500000000, "Provenance": "Manual" },
  "Override": { "StartTicks": 1500000000, "Provenance": "Manual" },
  "OverrideStale": false,
  "LastEditedBy": "example-administrator",
  "LastEditedAt": "2026-10-04T00:00:00Z",
  "Detected": [],
  "DetectedStale": false,
  "DetectedRevision": "0",
  "DetectedStatus": "not_analyzed",
  "DetectedReason": "",
  "DetectedUpdatedAt": null
}
```

The example identities and timestamp are illustrative. Treat `SourceRevision`
as opaque. `Revision` is a canonical non-negative decimal string, initially
`"0"` before any stored row; it is not a JavaScript number or a generic metadata
revision. `DurationTicks` and `StartTicks` are integer 100-nanosecond ticks:
10,000,000 ticks equal one second.

| Field | Meaning |
| --- | --- |
| `Automatic` | A valid explicit source chapter with `Provenance="Chapter"`, otherwise the first current detected interval's start with `Provenance="Detected"`, otherwise `null`. |
| `Override` | The stored Manual/Import point, including a stale point retained for inspection, or `null`. |
| `OverrideStale` | The stored source identity differs from the current source or its point is outside the current duration. A stale point is never effective. |
| `Effective` | A valid current override, otherwise the explicit chapter point, otherwise the current automatic point, otherwise `null`. |
| `LastEditedBy`, `LastEditedAt` | Most recent saved/reset editor and timestamp. Untouched items have an empty editor and `null` timestamp. A deleted user can leave an empty editor. |
| `Detected` | Complete stored interval list with `StartTicks`, `EndTicks`, and analyzer `Source`, retained for inspection even when no longer effective. |
| `DetectedStale` | Stored detection evidence no longer matches the current target, support sources, profile, or other required identity. Stale intervals are not published. |
| `DetectedRevision`, `DetectedStatus`, `DetectedReason`, `DetectedUpdatedAt` | Independent detection revision/status/reason/timestamp. The status distinguishes `not_analyzed`, `qualified`, and `no_result`; it is separate from manual edit revision. Qualified, non-stale evidence can remain stored while publication is disabled. |

Source identity includes the root binding, relative path, file identity, size,
modification time, and probed media facts. A root rebind, source replacement,
or changed probe can invalidate a stored override without deleting its record.
The credits revision is independent from the intro-marker revision.

## Save and clear

Read the current state, then submit exactly these four fields to `PUT`:

```json
{
  "Revision": "0",
  "SourceRevision": "opaque-current-source-revision",
  "StartTicks": 1500000000,
  "Provenance": "Manual"
}
```

`StartTicks` must satisfy `0 <= StartTicks < DurationTicks`. Zero is a valid
explicit start and differs from an omitted field. `Provenance` accepts only
`Manual` or `Import`; the native editor saves `Manual`. `Import` records the
origin of a supplied point and does not select a bulk-import endpoint.

To clear an override, `DELETE` requires a JSON body with exactly these fields:

```json
{
  "Revision": "1",
  "SourceRevision": "opaque-current-source-revision"
}
```

Both writes compare the supplied credits revision and source revision with the
current values, including inside the write transaction. Saving or clearing
advances the revision. Clearing preserves a versioned empty record, so an old
editor cannot restore a deleted value with a stale revision. It clears only
the override; a valid explicit source chapter becomes effective again.
If no explicit chapter is available, a current enabled detected result can
become effective. Clearing the manual override does not itself schedule analysis
or manufacture a new detection.

Bodies must be JSON objects with exact, case-sensitive field names. Missing,
unknown, duplicate, `null`, wrong-type, fractional-tick, and trailing JSON values
are rejected. Do not send `StartTicks` or `Provenance` in a reset body.

| Status and error code | Meaning |
| --- | --- |
| `400 invalid_input` | Invalid ID, query, revision format, body, provenance, or start range. |
| `401` / `403` | Authentication, administrator authorization, resource access, origin, or CSRF checks failed. |
| `404 not_found` | The eligible item was not found. |
| `409 credits_revision_conflict` | Credits or source changed since the caller's read. Reload before editing. |
| `503 library_unavailable` | The source, storage, or required indexed media facts are unavailable. |

The existing catalog notification path records an item update after a save or
reset. Audit logging records actor, item, and new revision. Database migration
`0056_credits_markers.sql` adds `item_credits_state`; the schema-56 PostgreSQL-17
backup catalog includes its rows, constraints, and relationships.

## Automatic credits analysis

`LibraryOptions.EnableCreditsDetection` defaults to false and is independent
from intro detection, BIF previews, background clips, and waveforms. The selected
scope includes physical Movies and Episodes in movie, TV, and mixed libraries.
Movies use their own source evidence; TV processing can additionally use
multiple eligible episode sources for matching. A library-wide task uses
`media.credits_analysis` and the `CreditsAnalysisRequested` event, with existing
task progress, cancellation, recovery, and publication fences.

Request selected analysis through `POST /admin/v1/media-analysis/runs` with
the complete JSON body:

```json
{
  "Kind": "credits",
  "RequestId": "client-generated-unique-request-id",
  "LibraryIds": [],
  "ItemIds": ["example-item-id"],
  "Force": false
}
```

This uses the existing native administrator session, origin, and CSRF checks.
Selection limits are 64 distinct library IDs and 256 distinct item IDs, each
at most 128 UTF-8 bytes without spaces or control characters. Supply a stable
request ID when retrying an uncertain admission response. The task center
provides progress and cancellation; metadata/playback reads do not start analysis.
Success returns HTTP `202` with `RunId`, `TaskId`, and `Admitted`. Untouched task
schedules receive a daily interval and `CreditsAnalysisRequested` trigger;
administrator schedule edits or deliberately cleared triggers are preserved.
An identical normalized request replay returns `Admitted=false` instead of
creating another run. Explicit requests may analyze a disabled library, but
its automatic intervals remain unpublished until policy permits publication.

The algorithm reuses the previously selected Intro Skipper project at fixed
commit `6e0cb179007ac4c16cd9f358e9a617e791e9bf06`. It combines the credits-tail
audio path with the upstream CreditsPass components: chapter candidates,
black-frame analysis, entropy fallback, candidate combination, and time
adjustment. This is not a claim of shared source or algorithm identity with
Emby's closed-source implementation. Detailed port and license records are
maintained with the implementation, separately from this API contract.

The fixed visual defaults inspect the final 450 seconds of an Episode or 900
seconds of a Movie; named chapter analysis retains its upstream whole-source
matching policy. The episode audio matcher fingerprints its own final 450
seconds, requires independent same-season support, and converts each pair back
to its own source clock. Unrelated movies are never compared for shared audio.
Arbitrary configured .NET chapter expressions and the legacy black-frame
analyzer are outside the fixed-default port. See the
[CreditsPass port](../../internal/creditsskipper/README.md) and
[audio matcher](../../internal/introskipper/README.md) for algorithm and license
provenance.

Results retain every final source-bound credits interval and its analyzer
provenance. Gaps containing intervening content are not flattened into one long
credits interval. The compatibility `Automatic` point and standard chapter
marker represent the first current start; `Detected` retains the full native
interval result. Consumer item/source DTOs use `GobyCreditsIntervals` for the
complete interval contract.

Publication requires current target/source identity, profile/publication
revision, cohort/support evidence, task authority, and enabled library policy.
Target replacement, changed support/hierarchy, or profile changes make old
evidence unavailable for automatic projection. Disabling the library option
withdraws detected publication without deleting manual markers. Valid manual
or explicit chapter points take precedence over detected intervals.
A policy change clears automatic publication without changing the stored
detection revision, status, result, or timestamp. It does not itself mark
evidence stale. Re-enabling requests analysis but does not reactivate retained
evidence: intervals require a subsequent successful publication under current
policy and publication fences, potentially from a still-valid in-flight run.
A retained `qualified`/non-stale row alone does not prove that automatic markers
are active.

`no_result` publishes no automatic boundaries and never becomes a guessed
final-duration interval. It can mean completed analysis with
`DetectedReason=no_credits_detected`, or a reasoned abstention such as
`source_unavailable`, `insufficient_cohort`, or `analysis_limit`. Read the reason
and task/runtime state rather than interpreting every no-result row as a
successful complete scan. Execution/tool errors fail the task instead of
manufacturing a result. The player end-of-file fallback is a presentation
behavior, not fabricated analysis.

Migration `0059_credits_analysis.sql` adds separate detection/support evidence
and the library/task policy. It does not overwrite intro or manual-marker state.
Its own acceptance includes current source/support checks, disabled publication,
manual precedence, reanalysis, and recovery; earlier schema-56 manual-marker
results are retained under their original scope.

## Consumer projection and playback

A valid effective point is appended to the item's and playback source's
`Chapters` as:

```json
{
  "StartPositionTicks": 1500000000,
  "Name": "Credits Start",
  "MarkerType": "CreditsStart",
  "ChapterIndex": 3
}
```

`ChapterIndex` is illustrative: the shared projection sorts chapters by position
and assigns indexes after adding ordinary chapters and intro/credits markers.
The legacy manual/source point emits only `CreditsStart`; the automatic
increment also emits its first valid start as the standard `CreditsStart`.
No `CreditsEnd` is added to standard `MarkerType`. A source chapter
for the legacy point path is evidence only when exactly one trimmed title equals the reserved case-sensitive
`CreditsStart` value and its start is valid. Ordinary names such as `End` and
duplicate reserved chapters do not establish a legacy point. Automatic chapter
candidate analysis is a separate, source-bound detector path.

The official Emby [MarkerType reference](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Entities.MarkerType.html)
currently lists `CreditsStart` alongside Chapter/IntroStart/IntroEnd, but not
`CreditsEnd`. Complete intervals therefore use an explicit Goby extension
rather than inventing a standard enum. The public enum alone does not prove
an original Emby detector's algorithm, movie/episode coverage, or accuracy.

Item detail, or an item query requesting `Fields=Chapters`, exposes the
extension alongside Chapters. MediaSource/PlaybackInfo DTOs also expose it:

```json
{
  "GobyCreditsIntervals": [
    {
      "StartPositionTicks": 1500000000,
      "EndPositionTicks": 1650000000,
      "Source": "Chapter"
    }
  ]
}
```

The interval above is illustrative. Native `Detected` retains the complete
analyzer result; consumer projection sorts intervals and takes the union of
overlapping intervals, assigning `Source=Combined` when their sources differ.
Real gaps are retained. Each entry has its own start, end, and provenance.
Manual/import or a
reserved source point can project one interval from that point to the source
duration. When `GobyCreditsIntervals` exists it is authoritative, including an
empty array for no current intervals. The player does not recover a stale
chapter point when that array is empty. Only older responses without the field
use the legacy single-`CreditsStart` fallback. Native `Detected` uses
`StartTicks`/`EndTicks`; the consumer extension uses
`StartPositionTicks`/`EndPositionTicks`.

The negotiated source is authoritative as a whole: if present, missing or empty
source metadata does not fall back to an item's older metadata. A present empty
or malformed extension does not fall back to Chapters. For older responses
without the extension, exactly one valid `CreditsStart` yields a legacy interval
from that position to the source end.

With valid intervals, the next-episode cue appears only while actual playback
is inside the current interval. Each interval/source change or departure from
an interval resets the ten-second countdown. Pause, buffering, and seeking do
not consume it; entering an actual gap/coda hides the cue instead of extending
the preceding interval. This does not guarantee reaching a later coda: autoplay
can advance after ten playing seconds inside an earlier credits interval.
Canceling suppresses the cue and autoplay for that episode across track changes
and seeking. With autoplay disabled, the cue remains a manual next-episode action.

If no valid intervals are resolved, including an authoritative empty extension,
the player retains the last-15-seconds/end-of-file fallback. Natural end-of-file
advance is independent of detected interval coverage. Advancing during credits reports the actual source
position and retires the old playback session; it does not fabricate an
end-of-file position or force a watched flag.

Implementation: [store](../../internal/library/credits_markers.go),
[HTTP handlers](../../internal/server/credits_markers.go),
[chapter projection](../../internal/server/intro_markers.go),
[administrator editor](../../web/admin/src/CreditsEditorDialog.tsx), and
[player](../../web/player/src/pages/PlayerPage.tsx). The real Docker capability
run passed save/publication, paused/canceled countdown, automatic advance with
actual stop position, and clear/publication withdrawal. Its evidence and the
focused revision/source lifecycle regressions are recorded in the
[player acceptance record](../../web/player/ACCEPTANCE.md).

The later automatic run passed seven phases with generated chapter, visual,
and independently matched episode-audio sources. Chapter/black-frame cases
returned 60–90 seconds. Audio cases returned 536.5038851–600 and
548.5038851–612 seconds, about 3.496 seconds before the fixtures' music begins
at 540/552 seconds. The pinned algorithm and thresholds were preserved. These
results establish the selected integration and corpus behavior, not universal
recognition accuracy or exact-second boundaries. See
[the automatic runtime record](../../.artifacts/player-credits-detection/results.json).
