# Administrator media scans

This is a Goby-owned API. It does not add an Emby query parameter or claim
equivalence to Emby's metadata replacement operations. The dashboard is for
administration only; it contains no media player.

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
