# Implemented API surface: administration, catalog, and playback

The next increment implements the [schema43 account/playback contract](playback-accounts.md):
native local credentials, encrypted owner-only ProfilePin, Configuration/Partial,
writable intro/next preferences, source-bound intro administration and chapter
projections, bounded complete episode queues, and free server-local feature
registration. The [phase 1 record](../development/selected-compatibility-phase1-20260920.md)
separates passing backend/administrator/client journeys from the original Web
client's blocked enabled intro modes. The user accepted the compatibility
adapter boundary for third-party clients and phase 1 is closed under that
boundary. No untested client parity or deployment is claimed.

The selected phase 2 source adds the [native media-processing API](media-processing.md)
for embedded-subtitle removal and reviewed bitmap OCR, owned subtitle delivery,
MP3/FLAC/M4A embedded covers, authorized library/genre collages and additional
image transformations. Schema 45 is current. Implementation is complete, while
consolidated verification and repairs remain open in the
[phase 2 record](../development/selected-compatibility-phase2-20260920.md).
This paragraph supersedes older unsupported labels for those selected features
without changing historical acceptance records or claiming untested clients.

This file tracks implementation separately from the immutable upstream research inventory. The [full catalog](catalog.md) contains upstream contracts and initial scope labels; its generated `planned-unimplemented` field records the research baseline, not the current implementation tracker.

The [phase 3 integration record](../development/amd-media-phase3-20260919.md)
owns the current library, preferences, artwork, music, navigation and management
increment. The selected phase 3 contracts below are **verified and closed within
the recorded boundaries** on final source16, with explicit evidence/input
equivalence for earlier execution sources. They do not inherit acceptance merely
from phase 1/2 or the historical wave. A stored field or accepted request alone
does not establish a consumer workflow. Unsupported, read-only, provider-deferred
and full-original-client boundaries remain explicit; deployment is not implied.

The closed [AMD media phase 1](../development/amd-media-phase1-20260919.md) adds
HEVC and AV1 encoding to the existing playback routes, explicit
`VideoCodec`/`VideoProfile`/`VideoBitDepth` selectors, and `VideoRange` or
`VideoRangeType` for supported SDR/HDR10 output. HEVC supports MPEG-TS and MP4;
AV1 supports MP4, including fMP4 HLS. Progressive MP4 can burn selected text or
bitmap subtitles with `SubtitleOffsetTicks`. Broader copy seeking uses
source-bound random-access proof and an explicit source clock; see the
[media contract](../development/advanced-media.md),
[AMD processing](../development/amd-video-processing.md), and
[copy-seek contract](../development/copy-seek-compatibility.md).
Copied and VAAPI-encoded HEVC MP4 use `hev1`; software x265 output uses `hvc1`.
Encoder fallback rechecks the projected output framing against client constraints.
Hardware output is admitted for the exact requested tuple before an immutable
job is created; permitted software fallback preserves the requested format.
These source changes have completed phase 1 verification and do not imply full client,
GPU or Dolby Vision profile acceptance. No new user Configuration or
DisplayPreferences write API is delivered by phase 1; those remain phase 3.

The closed [phase 2 record](../development/amd-media-phase2-20260919.md) tracks
fixed multiple HLS text renditions, independent subtitle views, standard subtitle
playlist adapters and bounded dynamic output replay. These routes and service
configuration are implemented in source. Selected unit, HTTP/media, formal AMD,
v3 CPU/browser scopes and final builds passed; owned PostgreSQL/worker and
documentation closeout is complete. The browser result is a
native-video/HLS.js harness scope, not full original Emby Web compatibility.
[Continuous publication](../development/live-publication.md)
and [dynamic source ownership](../../internal/dynamicsource/README.md) define the
media and caption-completeness contracts.

The [September 19 feature wave](../development/feature-wave-20260919.md) adds
advanced subtitles/media processing, playlists/collections, policies, management
settings/tasks, and provider adapters. Its scoped consolidated remote acceptance
and main integration are complete; the [verification record](../development/feature-wave-verification-20260919.md)
owns the evidence. Provider-specific testing remains explicitly user-deferred.

The [advanced-media source contract](../development/advanced-media.md) records
the new output formats, subtitle delivery, dynamic-source limits, and source
anchors. "Implemented" below describes the delivered source contract, while
acceptance remains limited to the recorded profiles and scenarios. OCI,
Live TV business features, deployment and general hardware support remain
outside this wave.

Use [current status](../development/current-status.md) for the active implementation, verification, and deployment boundaries, and [the audit remediation record](../development/audit-remediation-20260913.md) for the current repair scope. Historical source-specific results below retain their original acceptance limits.

The routes below exist in source. Historical authentication, permission and ingestion workflows passed Goby's PostgreSQL-backed HTTP tests on Linux within their recorded increments. Selected behavior was also corrected using [real Emby 4.9.5.0 captures](../research/reference-server.md). The [M3e record](../development/client-acceptance-m3e.md) includes scoped original-client movie, TV, subtitle and audio evidence. Per-increment results and remaining limits are recorded in the [implementation progress](../development/progress.md). These results do not accept the September 19 additions or establish complete differential compatibility or every client/media profile.

The M5j native backup/recovery routes were accepted and deployed at schema
23/probe 6, with historical PID 3750313. The final 1605-test race suite, UI a6, complete
browser/process/offline-CLI journey, protected deployment and main-service
backup/download workflow passed. See the
[backup/recovery engineering record](../development/backup-recovery.md).
These native routes do not implement the Emby BackupRestore plugin contract.
After the test-host reboot, the M3e source18 checkpoint completed a fresh backup,
restore rehearsal and protected schema23-to25 main upgrade. Its
[deployment](../development/m3e-source18-main-deployment.json) passed readiness,
native login/read/logout and historical-archive checks as PID 539535. The
isolated acceptance candidate remains a separate service and database. See
[current status](../development/current-status.md) for their accepted deployment
checkpoints and the [handoff](../development/handoff.md) before operating either.

The official reference inventory contains 2462 sanitized JSON records. The [activity/log study](../research/observability-reference.md) adds 96 to the preceding 2366: 94 complete HTTP exchanges, one initial connection-refused readiness record, and one audit; all 76 capture HTTP exchanges are complete. Its [report](../development/m5i-observability-reference.json) is reference evidence, not product acceptance. The earlier [4K encoding-width study](../research/encoding-width-reference.md) added 61 records to the preceding 2305. The [fresh configuration mutation study](../research/configuration-mutation-reference.md) added 254 records after the [read study](../research/configuration-reference.md) brought the corpus to 2051. The [fresh ScheduledTasks mutation study](../research/scheduled-tasks-mutation-reference.md) reached the earlier 1965 checkpoint with 171 records. The earlier [task read study](../research/scheduled-tasks-reference.md), [key-device](../research/key-devices-reference.md), [ordinary user-device](../research/devices-reference.md), [key playback](../research/api-key-playback-reference.md), [client-context](../research/api-key-context-reference.md), and [target-scope](../research/api-key-scope-reference.md) evidence remain intact. Record totals include observations, probes and preserved incomplete responses; they are not counts of implemented endpoints or complete playback successes. Native metadata editing and its durable lock guarantees remain a separate contract from the observed Emby mutation routes.

Reference total configuration returns administrator `200` with 60 fields and ordinary-viewer `200` with exactly `{}`. Named encoding/devices/DLNA reads return administrator `200`, viewer `403`, and anonymous `401`; an unknown name returns administrator `500`. The earlier mutation study observes a Partial `500` after an in-process name change and three key-authorized baseline no-op writes returning `204`; it does not establish persistence or changed-value key writes. M5h's five [ConfigurationService operations](configuration.md) originally implemented ServerName, read-only IsStartupWizardCompleted and encoding.TranscodingMaxWidth. Current source also exposes backed metadata language/country/provider settings and closed subtitles/tasks sections. The full upstream objects remain unsupported. Native settings retains four name modes, Encoding and closed Management sections, with the selected phase 3 acceptance recorded separately.

In the separate [official-reference width execution](../research/encoding-width-reference.md), software `libx264` converts the same 4K source to 1280x720 at configured width 1280 and 3840x2160 at zero. Both eight-frame outputs pass complete decoding. This supports removing the extra width cap at zero in the sampled flow, not disabling all limits or establishing every client's behavior.

Device research observes ordinary-device grouping, name clearing, revocation and re-registration, plus key-authorized ordinary-device management. Deleting the fresh instance's shared server device revoked both tested keys and all five tested scopes; cached Session DTOs were not evidence of remaining authority. Hidden header-device Info/deletion and key recreation after server-device deletion were not sampled. The reference's zero counts and ignored sorting do not define Goby behavior. M5e device administration has passed product acceptance and is included below. Its new key-device generation on recreation is a Goby safety design, not an observed reference contract.

The initial ScheduledTasks read capture records 22 definitions, all sampled as `Idle`. The separate fresh capture observes real starts, both running-stop forms returning `204` before new `Cancelled` results, idle-stop `500`, administrator unknown-ID `404`, and bounded trigger writes. Unknown/mixed-invalid arrays return `400` without partial installation from the sampled empty baseline. Scheduled firing, weekly/system-event execution, DST, maximum-runtime enforcement, and key authority remain unverified reference behavior. Goby's [task API](tasks.md) now has separate product acceptance for its implemented executor, receipts, authorization, and scheduling policies; it does not advertise all reference tasks or unsupported system events.

The preceding [M5h configuration increment](../development/verification-m5h-configuration.md) passed 1252 top-level race tests across fourteen tested packages, with zero test skips and no race findings, 16 browser checks with two 28-table restarts, protected deployment with a real isolated restore, and the main-service configuration workflow. Its schema-21 migration through `0021_configuration_compatibility.sql` preserved all old fields across 28 tables; probe cache version remained 6. [M5g native settings](../development/verification-m5g-settings.md), [M5f](../development/verification-m5f-tasks.md), [M5e](../development/verification-m5e-devices.md), [M5d](../development/verification-m5d-application-keys.md), and earlier reports retain their completed historical increments. M4, M5, M6, and the full compatibility goal remain unfinished.

## Administrator API

All names are Goby-owned. JSON bodies and responses use the field names shown here.

The four activity/log rows below and their four compatibility counterparts are
**M5i complete and deployed**, with a React/MUI `/admin/observability` page and
schema-22 activity storage. The [full remote race suite](../development/m5i-full-race-summary.json)
passed **1380 top-level tests across 17 tested packages**, with zero skips or
race findings. [Browser/restarts](../development/m5i-observability-browser.json),
[protected deployment](../development/m5i-deployment-evidence.json), and the
[main-service workflow](../development/m5i-deployed-observability.json) passed.
That earlier M5i deployment used schema 22/probe 6 and PID 3668655. See the
[activity/log contract](observability.md) and [implementation evidence](../development/observability.md).

| Method and route | Request | Response / access |
| --- | --- | --- |
| `GET /admin/v1/bootstrap` | None | `{Initialized: boolean}`; minimal anonymous setup status |
| `POST /admin/v1/bootstrap` | `{SetupToken, Name, Password}` | `201 {User}`; one-time deployment secret, atomic first administrator creation |
| `POST /admin/v1/session` | `{Name, Password}` | `{User, CSRFToken}` and opaque HttpOnly cookie; administrator credentials required |
| `GET /admin/v1/session` | Session cookie | `{User, CSRFToken}` |
| `DELETE /admin/v1/session` | Cookie and `X-CSRF-Token` | `204`; revoke session and clear cookie |
| `GET /admin/v1/settings` | Administrator cookie; no query | `200 {Revision, ServerNameMode, Defaults, Overrides, Effective, Sources, Encoding, Management, ManagementDefaults, ManagementEffects, UpdatedAt, Deployment}`; saved policy and explicit next-work/next-admission effects, without provider credentials |
| `PUT /admin/v1/settings` | Cookie, CSRF, required `{Revision, Overrides}` with all five override fields; optional `ServerNameMode`, exact `Encoding: {TranscodingMaxWidth}`, and complete typed `Management`; no query | `200`, atomic replacement with CAS; omitted Encoding/Management preserves its current value, stale revision returns `409` |
| `POST /admin/v1/settings/reset` | Cookie, CSRF, exact `{Revision, Fields}`; no query | `200`, committed settings with CAS; native fields, `TranscodingMaxWidth`, `Management`, or its Metadata/Subtitles/Tasks section selectors; name reset selects deployment mode, width reset removes only the extra cap |
| `GET /admin/v1/activity` | Administrator cookie; optional `StartIndex`, `Limit`, `MinDate`, `Severity`, `Action`, `ActorId` | `200 {Items, TotalRecordCount, StartIndex, Limit, RetentionDays}`; native ID/count/revision strings, filtered total, default 50/max 200 |
| `GET /admin/v1/logs` | Administrator cookie; optional `StartIndex`, `Limit` | `200 {Items, TotalRecordCount, StartIndex, Limit, Status}`; registered safe JSONL files, native byte-count strings, default 50/max 200 |
| `GET /admin/v1/logs/{name}/lines` | Administrator cookie; optional `StartIndex`, `Limit` | `200 {Items, StartIndex, NextIndex, TotalRecordCount, SnapshotSize}`; fixed byte snapshot, default 200/max 500 string lines |
| `GET /admin/v1/logs/{name}/download` | Administrator cookie and download origin check; no query, optional bounded Range header | Safe JSONL attachment; native HEAD/ranges, fixed length, bounded lifetime/current-authority rechecks |
| `GET /admin/v1/sessions` | Administrator cookie; optional `UserId`, `Kind`, `Status`, `DeviceId`, `SearchTerm`, `StartIndex`, `Limit` | `{Items, TotalRecordCount, StartIndex, Limit}`; safe login metadata and current-session identity |
| `POST /admin/v1/sessions/{id}/revoke` | Cookie, CSRF, empty JSON object, no query parameters | `{SessionId, UserId, Kind, RevokedAt, CurrentSessionRevoked}`; fresh actor checks, idempotent single-login revocation and post-commit runtime retirement |
| `GET /admin/v1/api-keys` | Administrator cookie; optional `StartIndex`, `Limit`, `SearchTerm`, `IncludeRevoked` | `200 {Items, TotalRecordCount, StartIndex, Limit}`; eight safe metadata fields per row, actual filtered count, no token |
| `POST /admin/v1/api-keys` | Cookie, CSRF, exact `{AppName}`, no query | `201 {Key, AccessToken}`; independent non-expiring server credential |
| `POST /admin/v1/api-keys/{id}/reveal` | Cookie, CSRF, `{}`, no query | `200 {Id, AccessToken}`; explicit recovery of an active token; revoked key returns `409` |
| `POST /admin/v1/api-keys/{id}/revoke` | Cookie, CSRF, `{}`, no query | `200 {Id, RevokedAt}`; idempotent parent-credential revocation and retirement of all its client contexts |
| `GET /admin/v1/devices` | Administrator cookie; optional `SearchTerm`, `StartIndex`, `Limit` | `200 {Items, TotalRecordCount, StartIndex, Limit}`; fourteen safe fields per ordinary device, actual filtered count |
| `POST /admin/v1/devices/{id}/options` | Cookie, CSRF, exact `{Revision, CustomName}`, no query | `200`, updated Device object; empty string clears the override; stale revision returns `409` |
| `POST /admin/v1/devices/{id}/delete` | Cookie, CSRF, exact `{Revision}`, no query | `200 {Id, DeletedAt, RevokedLoginCount}`; soft-delete one generation, revoke its ordinary credentials, retain history |
| `GET /admin/v1/tasks` | Administrator cookie; no query | `200 {Items, TotalRecordCount}`; stable task definitions, current/last runs and schedules |
| `GET /admin/v1/tasks/{id}` | Administrator cookie; no query | `200 {Task}` |
| `POST /admin/v1/tasks/{id}/runs` | Cookie, CSRF, `{}` or optional `{RequestId}`, no query | `202 {Run, Admitted}`; durable admission, request replay or coalescing into an active run |
| `GET /admin/v1/tasks/{id}/runs` | Administrator cookie; optional `StartIndex`, `Limit` | `200 {Items, TotalRecordCount, StartIndex, Limit}`; run history |
| `GET /admin/v1/task-runs/{id}` | Administrator cookie; optional child `StartIndex`, `Limit` | `200 {Run, Children}`; `Children` is a paged result |
| `POST /admin/v1/task-runs/{id}/cancel` | Cookie, CSRF, `{}`, no query | `202 {Run}`; idempotent durable cancellation of that run's owned work |
| `PUT /admin/v1/tasks/{id}/triggers` | Cookie, CSRF, exact `{Revision, ScheduleTimezone, Triggers}`, no query | `200 {Task}`; full revision-checked schedule replacement |
| `POST /admin/v1/tasks/{id}/triggers/preview` | Cookie, CSRF, exact `{ScheduleTimezone, Triggers}`, no query | `200 {ServerTime, Items}`; three future times per timed rule or a startup event, without mutation |
| `GET /admin/v1/overview` | Administrator cookie | Server identity, database status, real account/session counts, current feature flags |
| `GET /admin/v1/capabilities` | Administrator cookie | Implementation flags and pinned toolchain targets; unavailable media/hardware features report false |
| `GET /admin/v1/users` | Administrator cookie | `{Items: User[], TotalRecordCount}` |
| `POST /admin/v1/users` | Cookie, CSRF header, `{Name, Password, IsAdministrator}` | `201 {User}` |
| `GET /admin/v1/users/{id}` | Administrator cookie | `200 {User: ManagedUser}`; saved supported policy and revision |
| `PUT /admin/v1/users/{id}` | Cookie, CSRF, complete `{Revision, Name, IsAdministrator, IsDisabled, Policy}` | `200 {User: ManagedUser, CurrentSessionRevoked}`; revision and last-enabled-administrator checks |
| `POST /admin/v1/users/{id}/password` | Cookie, CSRF, `{Revision, Password}` | `200 {User: ManagedUser, CurrentSessionRevoked}`; nonempty password reset and transactional target-session revocation |
| `GET`, `PUT /admin/v1/users/{id}/preferences` | Cookie; PUT requires CSRF and `{Revision, Configuration}` | Independent configuration revision; selected persistent preference patch and actual consumers; [contract](../development/client-preferences-plan.md) |
| `GET`, `PUT`, `DELETE /admin/v1/users/{id}/image`; `GET .../image/content` | Cookie; mutations require CSRF and quoted If-Match; raw bounded image upload | Stored Primary avatar, protected preview and revisioned deletion; [avatar contract](../development/local-artwork.md#user-avatars-and-public-login-visibility) |
| `DELETE /admin/v1/users/{id}` | Cookie, CSRF, `{Revision}` | Revision-checked account deletion with last-enabled-administrator protection and retirement of the deleted account's authority |
| `GET /admin/v1/features` | Administrator cookie; no query | `{Items}` with the supported user-feature catalog; this is not an upstream system-feature inventory |
| `GET`, `POST /admin/v1/playlists`, `/admin/v1/collections`; `GET`, `POST`, `PATCH`, `DELETE .../{Id}` | Administrator cookie; CSRF for mutations | Native playlist/BoxSet list, create, detail, metadata/access update, and container deletion; media files are retained |
| `GET`, `POST`, `DELETE .../{Id}/items`; `POST .../{Id}/items/delete` | Same native collection authority | Ordered playlist entries or unique BoxSet membership; current container and member access apply |
| `GET /admin/v1/playlists/{Id}/items/preview`; `POST .../items/{ItemId}/move/{NewIndex}` | Same native collection authority | Bounded add preview and stable entry-ID reordering |
| `GET /admin/v1/libraries` | Administrator cookie | `{Items: Library[], TotalRecordCount}` |
| `POST /admin/v1/libraries` | Cookie, CSRF, `{Name, CollectionType, Paths, Scan}` and optional supported `LibraryOptions` | `201 {Library, Job?}`; optional `ScanError` if catalog creation succeeded but initial scan admission failed |
| `GET`, `PATCH /admin/v1/libraries/{id}` | Cookie; PATCH requires CSRF and `Revision`, with selected name/path/options fields | Saved edit state; revision-checked root replacement/removal, explicit move preservation and optional separate scan; [contract](admin-scans.md) |
| `DELETE /admin/v1/libraries/{id}` | Cookie and CSRF | `204`; remove catalog records, retain every media file; active scans prevent removal |
| `POST /admin/v1/libraries/{id}/scan` | Cookie and CSRF; empty body or optional JSON `ForceProbe` boolean; no query | `202 {Job}`; durable normal/forced media scanning with per-library deduplication; [contract](admin-scans.md) |
| `GET /admin/v1/libraries/{id}/roots` | Administrator cookie; no query | `200 {Items, TotalRecordCount}`; registered root IDs, configured and relative paths, and current revision strings |
| `GET /admin/v1/libraries/{id}/roots/{rootId}/binding` | Administrator cookie; no query | `200 {Binding}`; current status, approved/observed topology and fingerprints, and recorded binding authority |
| `PUT /admin/v1/libraries/{id}/roots/{rootId}/binding` | Cookie, CSRF, exact `{Revision, ObservedFingerprint, AcknowledgeMissingRemoval: true}`; no query | `200 {Binding}`; approve the reviewed observation with revision checks; changed binding or observation returns `409 root_binding_conflict`; no scan is started |
| `GET /admin/v1/libraries/{id}/items` | Administrator cookie; optional `SearchTerm`, `Types`, `StartIndex`, `Limit` | Library-scoped lightweight summaries and paging; no user-state writes |
| `GET /admin/v1/items/{id}/metadata` | Administrator cookie; no query parameters | Item context, revision, automatic/effective values, overrides, locks and inactive settings |
| `PUT /admin/v1/items/{id}/metadata` | Cookie, CSRF, complete `{Revision, Overrides, LockedFields}` | Atomic effective metadata/entity update; stale source or editor revision returns 409 |
| `GET /admin/v1/jobs` | Administrator cookie | Recent scan jobs, persisted `ForceProbe` mode, real progress and outcomes |
| `POST /admin/v1/jobs/{id}/cancel` | Cookie and CSRF | `202 {Job}`; cancel remaining scan work, retain already indexed items |
| `GET /admin/v1/storage/roots` | Administrator cookie | `{Items: [{Path, Available}], Configured}`; `Available` includes directory read permission |
| `GET /admin/v1/storage/directories`; `POST .../validate` | Cookie; POST requires CSRF | Bounded approved-root directory browsing and read-only canonical-path validation; [contract](admin-scans.md) |
| `GET /admin/v1/music/artists`, `/admin/v1/music/genres` | Administrator cookie; selected filters/paging, artist `Role` | Authorized persistent music entities for management; selected phase 3 scope verified |
| `GET /admin/v1/entities` | Administrator cookie; supported Kind, search and paging | Independent persistent entity management list, default 25/max 100 |
| `GET /admin/v1/{items\|entities}/{id}/images`; `GET .../{Type}/{Index}` | Administrator cookie | Revisioned effective collection and protected preview |
| `PUT`, `DELETE /admin/v1/{items\|entities}/{id}/images/{Type}/{Index}`; `POST .../{Type}/reorder`, `/reset` | Cookie, CSRF, opaque revision; raw upload or strict operation body | Managed media/entity artwork with source-aware conflicts and explicit restoration of automatic images; [contract](../development/local-artwork.md) |

### September 19 management source boundary

The expanded user-policy editor covers library/subfolder access, parental and
tag filters, access schedules, device/remote access, stream concurrency and
remote bitrate, preference and remote-control permissions, content/subtitle
management, and restricted user features. These are enforced by the relevant
catalog, authentication, media, and command paths; they are not a claim that
every upstream policy field exists. Source anchors:
[writable policy fields](../../internal/server/user_management.go#L21),
[policy model](../../internal/identity/policy.go#L29), and
[catalog restrictions](../../internal/library/policy_access.go).

`Management.Metadata` contains `EnableInternetProviders`,
`PreferredMetadataLanguage`, and `MetadataCountryCode`;
`Management.Subtitles` contains `DownloadLanguages`, `DownloadMovieSubtitles`,
and `DownloadEpisodeSubtitles`; `Management.Tasks` contains `MaxConcurrent`,
`CacheRetentionDays`, and `CacheMaxEntries`. Updates require all fields of a
supplied Management object and share the settings revision. Hardware, paths,
credentials, and process budgets remain deployment settings. See the
[closed decoder](../../internal/server/admin_settings_management.go#L10) and
[runtime effects](../../internal/server/settings_dtos.go#L29).

Provider adapters are integrated in source, but provider-specific acceptance
is **user-deferred**. The native API exposes `GET /admin/v1/providers`,
`GET /admin/v1/items/{id}/providers/provenance`, POST operations at
`.../providers/search`, `apply`, `refresh`, `images`, `image`,
`subtitles/search`, and `subtitles/download`, and
`GET .../providers/image-preview`. Startup provider configuration and the
managed enable switch jointly govern availability; no credential is returned
by these status/configuration DTOs. The implementation includes TMDB,
MusicBrainz, and OpenSubtitles adapters, not an arbitrary-provider plugin API.
See [registered routes](../../internal/server/providers.go#L22) and
[provider configuration](../../internal/server/provider_configuration.go#L8).

### Phase 3 administrator workflows

The phase 3 administrator source adds library editing/directory selection,
user preferences, media/entity/avatar artwork management, music metadata controls
and system-event scheduling within the existing interface. The dialogs retain
drafts on errors, block navigation during a mutation, label reorder controls
and distinguish loading, rejected, conflicting and unknown outcomes. Native
403 access_denied does not itself end an administrator session; loss of current
administrator authority uses the authentication flow. Selected browser,
restart/permission and final desktop/mobile visual checks passed in the
[administrator workflow contract](../../scripts/test-env/phase3-admin-browser.md).

### Native backup/recovery

These M5j routes are part of the accepted deployment. They require a live
native administrator cookie; mutations additionally require same-origin and
CSRF checks. Emby login tokens and application keys do not authorize them.
The [backup API contract](backups.md) defines the complete DTO, strict input,
revision, idempotency and error rules. None of these routes is an Emby
BackupRestore/plugin alias or an upstream-compatible archive implementation.

| Method and route | Request | Response / access |
| --- | --- | --- |
| `GET /admin/v1/backups/status` | Cookie; no query | `200 StatusView`; backup/restore availability, limits, storage, active operation and generation/rollback state |
| `GET /admin/v1/backups` | Cookie; optional `StartIndex`, `Limit` | `200 BackupPage`; default 25/max 100 |
| `GET /admin/v1/backups/{id}` | Cookie; no query | `200 {Backup}` |
| `POST /admin/v1/backups` | Cookie, CSRF, exact `{RequestId, Passphrase}` | `202 {Operation}`; durable encrypted backup creation |
| `POST /admin/v1/backups/import` | Cookie, CSRF, age bytes as `application/octet-stream`, `X-Backup-Request-Id` | `202 {Operation}`; imported bytes remain unverified until restore-plan validation |
| `GET /admin/v1/backups/{id}/file`; `HEAD` on the same route | Cookie and download origin check; no query; optional Range | Immutable encrypted attachment with length and SHA-256 ETag; bounded current-authority checks |
| `DELETE /admin/v1/backups/{id}` | Cookie, CSRF, exact `{RequestId, SHA256}` | `202 {Operation}`; durable deletion of the selected object |
| `GET /admin/v1/backup-operations` | Cookie; optional `StartIndex`, `Limit` | `200 OperationPage`; default 25/max 100 |
| `GET /admin/v1/backup-operations/{id}` | Cookie; no query | `200 {Operation}` |
| `POST /admin/v1/backup-operations/{id}/cancel` | Cookie, CSRF, exact `{Revision}` | `202 {Operation}`; only when cancellable |
| `POST /admin/v1/restores/plans` | Cookie, CSRF, exact `{RequestId, BackupId, SHA256, Passphrase, RestoreDefaults, ReplaceRollback, GenerationRevision}` | `202 {Operation}`; validate and stage in an independently owned inactive slot |
| `POST /admin/v1/restores/{id}/apply` | Cookie, CSRF, exact `{Revision, GenerationRevision}` | `202 {Operation}`; drain and perform a guarded generation transition |
| `POST /admin/v1/restores/rollback` | Cookie, CSRF, exact `{RequestId, GenerationRevision}` | `202 {Operation}`; select only an explicitly retained, verified rollback copy |

An admission `202` is not completion or activation acceptance. Final operation
state and the subsequent authenticated generation establish the result.
Passphrases are never returned or persisted in the operation journal.

### Existing administrator contracts

The native list/create/session user shape remains `{Id, Name, IsAdministrator, IsDisabled, HasPassword, CreatedAt}`. Managed detail adds `Revision` as an opaque positive decimal string and the supported `Policy` fields listed in the management source boundary above. The six original library/playback fields remain required in native updates; newly supported fields can be omitted to retain their saved values. Updates reject duplicate, unknown, null where not permitted, or wrongly typed fields; stale revisions and attempts to remove the last enabled administrator return 409 without changing state. Saved policy is distinct from effective administrator access and configured conversion capability. The [historical native user contract](admin-users.md) describes the earlier subset; current source also implements native account deletion and the bounded Emby user mutation adapters below.

Client configuration remains separate from authority. Missing or invalid
`IntroSkipMode` now projects `None`, because intro skipping is not provided;
valid stored `ShowButton` or `AutoSkip` preferences are retained without
rewriting persisted configuration. This does not add a general UserDto
configuration-write API by itself. The later phase 3 selected write contract is
documented in [client preferences](../development/client-preferences-plan.md).

[Native application-key management](application-keys.md) returns exactly `Id`, `AppName`, `CreatedAt`, `LastUsedAt`, `RevokedAt`, `CreatedBy`, `IPAddress`, and `Status` in safe rows. IDs are decimal strings; `LastUsedAt`, `RevokedAt`, and `CreatedBy` are nullable. POST bodies are strict UTF-8 JSON objects limited to 4 KiB. App names and literal search terms are limited to 256 UTF-8 bytes without controls. Native list limits are 1–200, default 50, with newest-first deterministic ordering and actual counts even for empty pages. Key handlers disable caching; secrets appear only in native creation and explicit reveal responses. Creation/reveal require the persistent `GOBY_API_KEY_MASTER_KEY_FILE` vault, with safe lazy creation and Linux ownership/mode checks; unavailable secrets return `503` while safe metadata and revocation remain usable. See [operations and recovery](../development/application-keys.md).

[Native Devices](devices.md) lists ordinary registry generations independently of native administrator sessions and application-key contexts. IDs and revisions are positive decimal strings. Device POST bodies are strict 4 KiB JSON; custom names and literal search are bounded to 256 UTF-8 bytes. Manual name edits use revisions that routine activity preserves, and clearing restores each session's raw reported name. `ActiveLoginCount` measures current authorization, not online presence. Repeat deletion of a known generation returns its original timestamp and zero newly revoked credentials; a later login registers a new generation. The hidden shared-key family is not exposed through native device routes. Device handlers disable caching and never expose credentials or ciphertext.

[Native Tasks](tasks.md) exposes `library.scan`, `library.refresh_media`, `metadata.refresh`, `subtitle.download`, and `cache.maintain`. Provider work and cache maintenance require their actual configuration and executors; provider execution acceptance remains user-deferred. Definition, run, trigger, and child IDs are opaque 32-character hexadecimal strings; revisions and native tick values are decimal strings. Strict JSON bodies are bounded to 32 KiB, rules to 32, and history/child pages to 200 rows. A nonempty visible-ASCII `RequestId` has a durable per-definition receipt, including when requests coalesce; retries after completion return the original run. Schedules support interval, daily, weekly, startup and the closed phase 3 system-event rules, named timezones, preview, and explicit maximum-runtime limits. Empty schedules preserve manual starts. Definitions/runs remain separate from per-library scan jobs. Phase 3 adapter/event additions passed the recorded selected verification; see [current ownership/timing](../development/tasks.md).

[Native activity and logs](observability.md) read separate stores. The deployed
M5i increment defines twenty-one transactional activity actions; the M5j
candidate adds backup/recovery activity within its separate acceptance scope.
Actions are inserted in their owning business transactions, with fixed
descriptions and changed-field names rather than request values. Default
activity retention is 30 days with one bounded batch per minute. Diagnostic
files use a service-owned private Linux directory, per-event sanitization,
rotation, seven-day retention, and at most eight concurrent snapshots. The
administrator page offers filtering, details, line preview, and cookie-based
downloads. No arbitrary filesystem log reader or media player is added. All
eight M5i handlers disable caching; the declared increment has passed complete
Go, browser/restart, protected deployment, and main-service acceptance.

Native errors have `{Error: {Code, Message, Fields?}, RequestId}`. `401` signals invalid/missing/revoked authentication; `403` signals rejected origin, setup token, CSRF, or a failed administrator recheck inside a metadata transaction. Mutations require JSON and CSRF protection once authenticated. The cookie is scoped to `/admin`, uses `HttpOnly` and `SameSite=Strict`, and is secure by default. CSRF values remain in browser memory and can be recovered with the authenticated session endpoint. Password reset and disable revoke target sessions; demotion revokes administrator-cookie sessions. `CurrentSessionRevoked=true` clears the caller's cookie and requires sign-in rather than an automatic mutation replay.

[Native metadata editing](admin-metadata.md) keeps scanned values, sparse manual overrides and locked snapshots separate. File/NFO refreshes update the automatic layer, including its revision, without discarding administrator controls. Restoring automatic values removes both a field's override and lock. Physical season structure remains read-only; inactive settings after reclassification are retained and can be cleared. Effective display columns and entity associations commit together, and a no-op edit preserves its revision and timestamps. Editor writes do not modify media/NFO files or claim compatibility with Emby's update/reset operations.

Library/item counts reflect persisted libraries and non-folder media records. Active sessions in this increment are active authentication sessions, not a count of playing media clients.

Library records contain `Id`, `Name`, `CollectionType`, `Paths`, `CreatedAt`, and nullable `LastScanAt`. Scan jobs contain `Id`, `LibraryId`, `Status`, `Error`, `Scanned`, `Added`, `Updated`, `CreatedAt`, and nullable start/finish timestamps. Native statuses are `pending`, `running`, `completed`, `failed`, `cancelled`, and `interrupted`. A completed scan can have a warning describing individual media entries that could not be inspected; existing records are retained in that case. Job history is currently capped at the most recent 1,000 entries.

The administrator UI refreshes active work at bounded intervals, pauses polling while hidden, stops after terminal state, and allows explicit refresh after errors. A lost create response has an unknown outcome: the UI asks the administrator to check the list before attempting another creation.

## Emby API adapter

The [historical configuration contract](configuration.md) defines the original five closed projections. Current source adds preferred metadata language and country to the server configuration, retaining four persisted name modes and shared native revisions. Unsupported fields are rejected atomically. Compatibility writes require Emby token authority; native administrator cookies and CSRF tokens remain a separate interface. Other configuration fields and named sections remain unimplemented.

| Method and path | Current behavior / limits |
| --- | --- |
| `GET /emby/System/Info/Public` | Stable server identity and setup state; `Version` identifies the pinned 4.9.5.0 API baseline, while `ProductName: Goby` and `GobyVersion` retain the actual product identity/release. Native administration retains the product version; the API baseline does not claim complete compatibility |
| `GET /emby/Branding/Configuration`; `GET`, `HEAD /emby/Branding/Css` and `/Css.css` | Anonymous reads of Goby's current empty branding defaults, including root/case aliases and compatibility CORS. No custom disclaimer/stylesheet or branding write interface is provided |
| `GET /emby/System/Info` | Requires an Emby login or application key; currently the minimal public identity projection, not the complete upstream SystemInfo DTO |
| `GET /emby/System/Endpoint` | Requires an Emby login or application key; `{IsLocal, IsInNetwork}` from the resolved client connection, with trusted-proxy handling and no-store caching. Only authenticated loopback reference parity is observed; Goby's private/link-local classification grants no permissions |
| `GET`, `HEAD`, `POST /emby/System/Ping` | Confirmed reference behavior: `text/plain`, length 11, GET/POST body `Emby Server`, HEAD body empty |
| `GET /emby/System/Configuration` | Administrator/key: read-only `IsStartupWizardCompleted`, configured `ServerName`, metadata language/country and `EnableInternetProviders`; ordinary viewer: exact `200 {}`; never the full upstream object |
| `GET /emby/System/Configuration/{key}` | Administrator/key, closed `encoding`, `subtitles` and Goby `tasks` sections; viewer `403`; authorized `devices`/`dlna` return `501`, unknown sections `404` |
| `POST /emby/System/Configuration` | Administrator/key; supported name and metadata replacement; omitted language/country/provider enable reset to `en`/`US`/`false`, absent/null name selects unset; optional read-only setup-state echo; empty `204`, atomic validation |
| `POST /emby/System/Configuration/Partial` | Administrator/key; absent name preserves current state, supplied name and supported metadata fields update atomically; optional setup-state echo must match; empty `204` |
| `POST /emby/System/Configuration/{key}` | Administrator/key; replace the selected closed section using native defaults for omitted fields; unrelated settings preserved; empty `204`; [configuration contract](configuration.md) |
| `GET /emby/System/ActivityLog/Entries` | Administrator/key, `StartIndex`/`Limit`/`MinDate`, numeric IDs and compact query envelope; omitted Limit returns up to 200 items with total zero; explicit nonpositive Limit returns no items under the documented count rules |
| `GET /emby/System/Logs/Query` | Administrator/key, `StartIndex`/`Limit`, registered safe-file DTOs with numeric byte sizes; default max 200; real total retained even on empty/beyond-end pages |
| `GET /emby/System/Logs/{Name}/Lines` | Administrator/key, `StartIndex`/`Limit`; omitted/zero Limit gives empty Items with real snapshot line count; explicit max 500, negative values rejected |
| `GET /emby/System/Logs/{Name}` | Administrator/key, optional `Sanitize=true/false`; every mode remains sanitized, with fixed attachment snapshot and bounded rechecks; all four compatibility observability HEAD routes return fixed 404 |
| `GET /emby/Users/Public` | Current public-login visibility under enabled/hidden/remote/device rules; administrators excluded; stored avatar tags do not grant image authority |
| `GET`, `HEAD /emby/Users/{Id}/Images/Primary[/0]` | Private own/administrator/key authority, or anonymous current public-login visibility; invalid supplied token never falls back to public access |
| `POST`, `DELETE /emby/Users/{Id}/Images/Primary[/0]`; POST deletion aliases | Authorized own-avatar or administrator/key target management, optional If-Match, `204`; no anonymous mutation |
| `GET /emby/Users` | Bare user DTO array; administrator or application-key credential required |
| `POST /emby/Users/AuthenticateByName` | JSON or URL-encoded form `{Username, Pw}` and Emby client/device metadata; credentials stay in the request body; returns `User`, `SessionInfo`, `AccessToken`, `ServerId` |
| `POST /emby/Users/{Id}/Authenticate` | JSON or URL-encoded form `{Pw}` and client/device metadata; selected-user authentication |
| `GET /emby/Users/{Id}` | Current account, administrator, or application key; another ordinary user's account is denied. User DTOs project validated persisted configuration and non-null view/exclusion arrays |
| `GET`, `POST /emby/Users/{Id}/Configuration` | Current user login or administrator targeting; application keys rejected; GET saved projection, POST atomic supported-field merge and empty `200`; unsupported consumer fields are read-only echoes |
| `GET`, `POST /emby/DisplayPreferences/{Id}` | User/client/preference-ID isolation with required UserId/Client scope; optional write CAS, supported default-sort consumer and bounded opaque CustomPrefs; empty `200` POST |
| `GET /emby/Users/Query` | Administrator or application key; supports `StartIndex`/`Limit` and query-result envelope; other upstream filters remain to be implemented |
| `POST /emby/Users/New`; `POST /emby/Users/{Id}` | Administrator user sessions only, application keys rejected; bounded creation/copy and rename; a UserDto update does not write embedded Policy/Configuration |
| `DELETE /emby/Users/{Id}`; `POST .../{Id}/Delete` | Administrator user sessions only, application keys rejected; last-enabled-administrator protection and runtime credential retirement |
| `POST /emby/Users/{Id}/Password`; `POST .../{Id}/Policy` | Supported policy changes require an administrator user session; ordinary users can change their own password with the current password; application keys rejected |
| `GET /emby/Features` | Privileged supported user-feature catalog; `FeatureType=System` returns an empty list |
| `GET /emby/UserSettings/{Id}` | Persisted string dictionary, separate from User.Configuration. Ordinary callers read their own dictionary even when the path names another user, matching the sampled reference; administrator/key targeting follows explicit Goby authority |
| `POST /emby/UserSettings/{Id}/Partial` | Authorized merge of bounded JSON objects, including observed text/plain and octet-stream JSON; case-insensitive keys, observed scalar/nested conversion, null/empty-string deletion, transactional revalidation, and empty 204. Ordinary cross-user writes are denied |
| `POST /emby/UserSettings/{Id}` | Sampled dictionary replacement remains unsupported: 400 `Expected configuration type is UserSettings`, without mutation. See [the observed preference contract](../development/client-preferences-plan.md) for exact scope and limits |
| `GET /emby/Auth/Keys` | Administrator or application key; active rows expose full `AccessToken`, numeric IDs, `UserId: 0`, optional `DateLastActivity`, and actual total count; default limit 200 |
| `POST /emby/Auth/Keys?App={label}` | Administrator or application key; `204`, empty body; duplicate labels issue independent credentials |
| `DELETE /emby/Auth/Keys/{Key}`, `POST .../{Key}/Delete` | Administrator or application key; token-path revocation returns `204`, including unknown or previously revoked targets |
| `GET /emby/Devices` | `200` for an administrator or application key; current ordinary devices with actual total count and deterministic activity/ID ordering; shared key devices are omitted |
| `GET /emby/Devices/Info?Id={lookup}` | Privileged ordinary/shared device lookup by numeric generation or reported alias; unknown numeric generation returns empty `204`, unknown reported alias returns `404` |
| `GET /emby/Devices/Options?Id={lookup}` | Privileged lookup; `200 {CustomName}` or `{}`; unknown numeric generation returns `200 {}`, unknown reported alias returns `404` |
| `POST /emby/Devices/Options?Id={lookup}` | Privileged name change; missing/null/empty `CustomName` clears; `204`; never creates options for an absent device |
| `DELETE /emby/Devices?Id={lookup}` | Privileged removal of the addressed ordinary or shared generation; `204`, with idempotent numeric retries and scoped credential retirement |
| `POST /emby/Devices/Delete?Id={lookup}` | Compatibility alias for the same `204` removal contract |
| `GET /emby/ScheduledTasks` | Administrator or application key; `200` bare array; optional `IsHidden`/`IsEnabled`, only executable compatibility definitions |
| `GET /emby/ScheduledTasks/{id}` | Administrator or application key; `200` TaskInfo; unknown definition returns `404` |
| `POST /emby/ScheduledTasks/{id}/Triggers` | Administrator or application key; bounded JSON trigger array; `204` full replacement, with current native timezone retained |
| `POST /emby/ScheduledTasks/Running/{id}` | Administrator or application key; `204` durable start/coalescing by definition ID |
| `DELETE /emby/ScheduledTasks/Running/{id}` | Administrator or application key; `204` cancellation of the atomically selected pending/running execution; idle/already-stopping returns `500` under the documented stop contract |
| `POST /emby/ScheduledTasks/Running/{id}/Delete` | Same compatibility stop contract |
| `POST /emby/Sessions/Logout` | Empty `204`; normal login revokes the caller and disconnects its WebSockets; an application key revokes the parent and retires every client context |
| `GET /emby/Sessions` | Bare array; ordinary users see their own active sessions, privileged credentials also see other allowed login and key contexts; real context IDs, Id/DeviceId/presence filtering and current controllability rules |
| `POST /emby/Sessions/Capabilities`, `.../Capabilities/Full` | Validated query/JSON declarations replace the authenticated client context's capabilities; stale Id hints cannot target another context; `204` |
| `GET /embywebsocket` with RFC 6455 Upgrade | Authenticated LibraryChanged, UserDataChanged and commands; per-socket SessionsStart/Stop subscriptions add current-authority Sessions snapshots; aliases `/`, `/emby`, `/emby/`, `/emby/socket`; [subscription contract](../development/client-sessions.md) |
| `POST /emby/Sessions/{Id}/Playing` | Validated PlayRequest to an owned or privileged-authorized target context; applicable controller/target item access and target playback permission checked |
| `POST /emby/Sessions/{Id}/Playing/{Command}` | Playstate command; trusted controller/target IDs; 204 is acceptance, not a player acknowledgement |
| `POST /emby/Sessions/{Id}/Command`, `.../Command/{Command}` | GeneralCommand; full body preserves string arguments, named route has empty arguments as captured; current authorization rechecked before output |
| `GET`, `POST /emby/Items/{Id}/PlaybackInfo` | Authorized original-source DTOs and configured conversion; Audio/Video items preserve ordered HTTP/HLS TranscodingProfiles and recheck projected output conditions; progressive results use executable standard media URLs without starting a producer for user media or reserving a progressive job; bounded synthetic hardware admission may run |
| `GET`, `HEAD /emby/Videos/{Id}/stream`, `stream.{Container}` | Compatible original legacy delivery or supported progressive MP4 with H.264/HEVC/AV1 video and AAC audio; exact targets, ceilings, copy flags, source clock, subtitle burn-in, and current permissions checked |
| `GET`, `HEAD /emby/Videos/{Id}/original.{Container}` | Explicit original video bytes with standard ranges and conditional requests; current token, playback policy, library access, and source snapshot required |
| `GET`, `HEAD /emby/Audio/{Id}/universal`, `universal.{Container}` | Original audio capability negotiation, progressive conversion, or supported HLS output; current token, playback policy, library access, source snapshot and scoped playback ownership checked |
| `GET`, `HEAD /emby/Audio/{Id}/stream`, `stream.{Container}` | Original audio or explicitly selected progressive conversion; the supported HLS protocol selection uses the same audio planner |
| `GET`, `HEAD /emby/Audio/{Id}/original.{Container}` | Explicit original audio bytes with standard ranges and conditional requests; this route never converts the source |
| `GET`, `HEAD /emby/Videos/{Id}/master.m3u8`, `.../main.m3u8`; Audio equivalents | Authenticated legacy source-timeline TS or generated MPEG-TS/fMP4/packed-audio HLS; optional encoded multi-variant output and selected WebVTT rendition; immutable negotiated output revision |
| `GET`, `HEAD /emby/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.ts`; Audio equivalent | Finalized MPEG-TS, on-demand seek production, ranges/validators and current token/session/library/policy/source checks |
| `GET`, `HEAD /emby/Videos/{Id}/hls2/{PlaylistId}/{Artifact}`; Audio equivalent | Authorized generated media playlists, initialization/media segments and fixed text-subtitle renditions; phase 2 adds actual-interval subtitle segments and independent selected/off/offset views; only negotiated artifacts are served |
| `GET`, `HEAD /emby/Videos/{Id}/subtitles.m3u8`, `/live_subtitles.m3u8` | Phase 2 source adapters for an owned HLS revision or dynamic presentation and an explicit supported subtitle view; complete client acceptance remains open |
| `POST /emby/LiveStreams/Open`, `/MediaInfo`, `/Close`; `GET`, `HEAD /emby/LiveStreams/{LiveStreamId}/hls/{Artifact}` | Configured HTTP(S) leases and bounded retained TS/fMP4 replay with subtitle/presentation ownership, current policy and cleanup; recorded phase 2 media/browser scopes and resource/documentation closeout passed; no Live TV business subsystem |
| `DELETE /emby/Videos/ActiveEncodings`, `POST .../Delete` | DeviceId plus PlaySessionId; `204` cleanup scoped to the authenticated client context, unknown/foreign keys inert |
| `GET`, `HEAD /emby/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` and `/Items/...` | Indexed external or supported embedded text, SRT/WebVTT/ASS output, current authorization and source validation, bounded extraction and conditional responses |
| The same subtitle routes with `/{StartPositionTicks}/Stream.{Format}` | Cue-start filtering, end/timestamp options and bounded signed `SubtitleOffsetTicks`; explicit query start takes precedence; no inferred unit for upstream `SubtitleOffset` |
| `GET`, `HEAD /emby/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream` and `/Items/...` | Bounded indexed TrueType/OpenType font extraction with content checks and current media authorization |
| `DELETE /emby/Videos/{Id}/Subtitles/{Index}`; `POST .../{Index}/Delete`; Items equivalents | Authorized deletion of indexed external subtitle files with recovery and affected-output retirement; embedded subtitles remain read-only |
| `GET /emby/Items/{Id}/RemoteSearch/Subtitles/{Language}`; `POST .../Subtitles/{SubtitleId}` | User-session provider search/download with source/session-bound results and subtitle permissions; application keys rejected; provider acceptance is user-deferred |
| `GET`, `HEAD /emby/Items/{Id}/Download`, `/File` | Original media download with separate download permission, source checks, ranges and current-authority revalidation |
| `GET /emby/Items/{Id}/DeleteInfo`; `DELETE /emby/Items/{Id}`; `POST .../{Id}/Delete` | Policy-gated media deletion preview and actual regular-file deletion with catalog cleanup/recovery; folder/container and unsupported-source limits apply |
| `POST /emby/Playlists`, `/emby/Collections`; `GET`, `POST`, `PATCH`, `DELETE .../{Id}` | Persisted playlist/BoxSet creation, detail, updates and container deletion with current ownership/access rules; no media-file deletion |
| `GET`, `POST`, `DELETE .../{Id}/Items`; `POST .../{Id}/Items/Delete` | Authorized membership reads and edits; ordered repeatable playlist entries, unique BoxSet members |
| `GET /emby/Playlists/{Id}/AddToPlaylistInfo`; `POST .../{Id}/Items/{ItemId}/Move/{NewIndex}` | Bounded preview and playlist entry reordering; entry identity is distinct from catalog item identity |
| `POST /emby/Sessions/Playing`, `.../Progress`, `.../Stopped` | Bound reports and idempotent terminal events; logins persist personal state, application-key playback stays userless; token-only reports recover an owned client context; `204` |
| `POST /emby/Sessions/Playing/Ping` | Refresh an owned active playback, including token-only key context recovery; unknown/foreign nonempty IDs return inert `204`, missing ID returns the reference `400` text error |
| `POST /emby/Users/{UserId}/PlayingItems/{Id}`, `.../Progress`; `DELETE` base or POST `.../Delete` | Strict current-login legacy adapters into the existing playback state machine; query/body identities must agree; empty `200`; application keys/cross-user reports rejected |
| `GET /emby/Users/{UserId}/Items/Resume` | User-specific positions, current library ACL, initial duration/percentage thresholds and deterministic ordering |
| `GET`, `POST /emby/Users/{UserId}/Items/{Id}/UserData` | Current-authority selected-user state and atomic bounded patch; actual independent entity state for numeric entity IDs; `200` UserData |
| `POST /emby/Users/{UserId}/Items/{Id}/HideFromResume`; `POST`, `DELETE .../Rating`; POST `.../Rating/Delete` | Durable Resume exclusion or supported rating/likes update/removal; preserves authority and existing notifications; [state contract](../development/client-preferences-plan.md#userdata-and-legacy-playback-reports) |
| `POST`, `DELETE /emby/Users/{UserId}/PlayedItems/{Id}`, `.../FavoriteItems/{Id}`; `POST .../{Id}/Delete` | Per-user state; recursive folder watched changes and derived unplayed counts; flag-response DTO without ItemId/Key |
| `GET /emby/Users/{UserId}/Views` | Authorized library roots with collection types |
| `GET /emby/Users/{UserId}/Items/Root` | Stable virtual navigation root |
| `GET /emby/Users/{UserId}/Items` and `GET /emby/Items` | ACL-filtered browsing/search, recursive parents, IDs/types/media-type filters, paging and selected sorts |
| `GET /emby/Users/{UserId}/Items/{Id}` | Authorized item detail and probe metadata, including the item's path; also resolves visible catalog entities by positive decimal ID |
| `GET /emby/Items/{Id}/Ancestors` | Authorized same-library parents, nearest first, bounded cycle-safe traversal; bare array |
| `GET /emby/Items/Counts` | Current ACL and optional selected-user favorite counts; actual music entities and active trailer extras |
| `GET /emby/Videos/{Id}/AdditionalParts` | Strict consecutive same-source-family part/CD grouping after the seed; missing/hidden middle parts do not form a stack; [navigation contract](../development/next-up.md) |
| `GET /emby/Items/{Id}/Similar` | Same-type authorized candidates, reference-constrained scoring, returned-page counts and artist exclusions; source20's tests and scoped original-client 200 transfer are historical evidence; see [current status](../development/current-status.md) for present deployment and acceptance boundaries |
| `GET /emby/Users/{UserId}/Items/Latest` | Bare array; default grouping maps episodes to series and audio to albums before paging |
| `GET /emby/Shows/NextUp` | SeriesId selects the unplayed sequence after the watched cursor; global mode selects one continuation per series under a documented Goby policy; [evidence boundary](../development/next-up.md) |
| `GET /emby/Shows/{Id}/Seasons` | Series seasons in numeric order; proved same-library SeriesId/SeriesName are default relationship fields |
| `GET /emby/Shows/{Id}/Episodes` | Episodes with `Season`/`SeasonId` filtering and season/episode ordering; proved Series/Season IDs and catalog names are default fields in lists and details; [parent projection boundary](../development/tv-parent-metadata.md) |
| `GET /emby/Genres`, `/emby/Tags`, `/emby/Studios`, `/emby/Persons` | ACL-filtered entity lists with search, selected source-item filters, paging, and pre-pagination totals; Tags returns `{Name, Id}` entries with string IDs |
| `GET /emby/Genres/{Name}`, `/emby/Studios/{Name}`, `/emby/Persons/{Name}` | A single visible entity DTO with actual independent selected-user state; no list envelope |
| `GET /emby/Artists`, `/Artists/AlbumArtists`, `/AlbumArtists`, `/MusicGenres`, and their `/{Name}` details | Authorized actual music-role relationships; selected filters/counts/projection and independent entity state; [music contract](../development/music-metadata.md) |
| `GET /emby/Items/{Id}/Images` | Authenticated, library-authorized image metadata array; actual dimensions and size, with authorized local paths |
| `GET`, `HEAD /emby/Items/{Id}/Images/{Type}` and `.../{Type}/{Index}` | Current authorized media/entity artwork, bounded resize/conversion and source validation before cached/conditional delivery |
| `POST`, `DELETE /emby/Items/{Id}/Images/{Type}[/{Index}]`; POST deletion and `.../{Index}/Index` aliases | Current management authority, bounded image upload/delete/reorder and effective-source conflict checks; no anonymous mutation |
| `GET /emby/Library/VirtualFolders/Query` | Administrator library query with Revision and selected LibraryOptions projection; full upstream options remain unsupported |
| `POST /emby/Library/VirtualFolders` | Administrator library creation, optional refresh and selected local metadata-reader options; `204` |
| `POST /emby/Library/VirtualFolders/Name`, `/Paths`, `/Paths/Delete`, `/LibraryOptions` | Supported rename/root/options adapters; optional revision If-Match, `204` plus ETag; native API owns explicit identity-preserving moves; [contract](admin-scans.md) |
| `GET /emby/Environment/DefaultDirectoryBrowser`, `/DirectoryContents`, `/ParentPath`; `POST /Environment/ValidatePath` | Bounded approved-directory navigation and read-only validation; no file listing, write probe, credentials or unrestricted path access |
| `POST /emby/Library/VirtualFolders/Delete` | Administrator catalog removal, preserving media |
| `POST /emby/Library/Refresh` | Administrator or application key; `204` through the same durable full-library task admission, with every selected library retained as a child |

The parser accepts `Emby` and legacy `MediaBrowser` authorization schemes, the `X-Emby-Authorization` alternative, four separate `X-Emby-*` client/device headers, `X-Emby-Token`, legacy `X-MediaBrowser-Token`, and query `api_key` for issued Emby logins and application keys. Conflicting token or identity values are rejected. Caller-provided user/role claims never establish authority. Existing root-prefix and case-insensitive literal aliases include `Auth/Keys`, `Devices`, `Info`, `Options`, and `Delete`; identifier/token values remain opaque and case-sensitive. The M5i activity/log adapter guarantees its canonical paths and existing root-prefix alias, without adding unmeasured case-variant coverage for its new literals.

The [activity/log compatibility contract](observability.md#compatibility-surface)
keeps observed GET authority and paging distinct from the native API. It uses
actual Goby user associations, fixed activity descriptions, and registered file
creation times. Bounded invalid-input `400` and missing-file `404` responses,
private cache headers, always-sanitized downloads, and exact snapshot/range
handling are declared product boundaries. The reference's parsing/missing-file
`500` behavior and unresolved submicrosecond date rounding are not reproduced.
The passing reference study, full Go suite, browser/restart checks, and
main-service workflow retain their separate scopes; they do not establish a
complete third-party-client compatibility matrix.

The six non-camera [DeviceService operations](devices.md) require an ordinary Emby administrator or application key; ordinary users receive `403`, even for their own device. Numeric IDs address one generation without falling back to a reported alias. Ordinary removal revokes that generation's Emby logins while preserving native cookies and unrelated key contexts. Direct shared-server removal revokes all attached parent keys and their contexts. A removed shared alias cannot fall through to an ordinary device with the same reported string. Both migrations and the [registry operations guide](../development/devices.md) keep credential/playback history separate from current device registration. Complete device/Session wire equivalence and hidden header-device reference behavior remain outside this acceptance.

The six [ScheduledTaskService operations](tasks.md) reuse current administrator/key authorization and native definition IDs. The five selected mappings include `RefreshLibrary`, `DownloadSubtitles`, and explicitly Goby-prefixed keys for forced media refresh, metadata refresh and cache maintenance. `LastExecutionResult.Id` remains the definition ID; native history uses distinct run IDs. Pending/running projects as `Running`, stopping as `Cancelling`, and idle as `Idle`; progress counts terminal work children, including the cache executor's single global child. Compatibility ticks are JSON numbers and invalid replacements are atomic. `SystemEventTrigger` accepts only ServerStarted, LibraryChanged and ConfigurationChanged; the last two are declared Goby extensions. Their durable behavior passed the selected phase 3 scope; complete upstream scheduling/client parity is not claimed.

Application keys are server-wide privileged credentials with no user owner or expiry. A real parent credential owns persisted contexts keyed by client name and device ID, capped at 256 including the default; changing device name or version updates context metadata. `Sessions.Id` identifies the actual context, and application sessions omit `UserId` and `UserName`. Parent revocation closes all its WebSockets and conversion work. An owned `PlaySessionId` can restore a context only after parent-token authentication; query `DeviceId` never grants authority.

An explicit target user controls application-key catalog ACLs and state projection; global userless catalog results omit `UserData`. Favorite/played mutations target only the named real user, while key playback reports do not write personal state. PlaybackInfo validates explicit user IDs even for minimal requests. A key request without user, profile, or explicit audio selection omits `DefaultAudioStreamIndex`; this does not establish every user audio-preference rule. Profiles with nonempty `TranscodingProfiles` require `UserId` and combine global limits with the target's audio/video-transcoding and remux permissions. Account disablement or `EnableMediaPlayback` alone does not disable those key-negotiated conversion capabilities. The [key contract](application-keys.md) documents intentional pagination and error differences from the reference.

Application-key Views, Latest and Similar use neutral catalog preferences:
an explicit target UserId still governs ACLs/UserData, but does not inject that
user's personal order, exclusions or HidePlayed defaults. Explicit query filters
remain effective. DisplayPreferences default sorting also skips keys; its
separate preference API rejects keys. Existing authorized UserDto reads remain
available. The source06 coupling regression and source10 selected repair result
are retained in the [phase 3 ledger](../development/amd-media-phase3-results-20260919.json).

Confirmed authentication failures use the reference `text/plain` bodies and byte lengths: wrong/unknown credentials, missing client/device metadata, and missing/invalid access tokens. Native administrator errors remain JSON. Other Emby error cases are not yet claimed to match the reference.

The `/emby` namespace supports token-client CORS, including the observed OPTIONS response, origin reflection for valid HTTP(S)/opaque-null origins, credentials/preflight headers and private-network access headers. This does not expose `/admin/v1` through CORS or make administrator cookies authenticate Emby requests.

Administrator-cookie sessions and Emby credentials cannot be substituted for one another. Login tokens are stored as SHA-256 digests, are revocable and expire, and resolve against current account disable/demotion state. Emby user tokens currently have a 30-day lifetime; this is a Goby policy, not a proven Emby lifetime match. Non-expiring application keys store a token hash plus credential-bound AES-256-GCM ciphertext for explicit native recovery and compatibility full-token listing. Missing master files do not silently generate replacement keys, and hash-based authentication remains available.

The Emby user projection reflects supported saved policy and the enabled runtime's audio/video/remux switches; explicit playback denials apply to administrator logins too. Native and Emby user mutations implement the bounded account contracts above, not every upstream UserDto/Policy field. Media deletion now supports one indexed local regular file on Linux, with current deletion permission or an allowed deletion folder, verified persistent root binding, and recoverable catalog/filesystem work. Directories and items with dependent media are rejected; recovery retries remain bound to the original storage host. Application-key media authority is separate from user-account mutation authority. See [deletion target checks](../../internal/library/media_mutations_target.go#L12) and [recovery](../../internal/library/media_mutations.go#L265). Unsupported Emby paths return an error. Complete query fields, device/Session wire behavior, error parity and version negotiation still require broader evidence.

Item fields currently include identity, hierarchy, type, creation time and selected `Overview`, `MediaStreams`, `MediaSources`, `Path`, and `Chapters` projections. Default list results omit paths and probe structures; an explicit field selection or authorized item detail includes them as observed in the reference. Authorization is applied before any projection. `EnableImages=false` removes image fields, and `EnableUserData=false` suppresses user data when present. Source stream indices and probe sizes/ticks are preserved. Current probe snapshots can advertise original delivery when user policy permits; PlaybackInfo reopens and verifies the source before negotiation. Wire source IDs use `mediasource_{ItemId}`, and wire containers use canonical names such as `mp4` instead of an arbitrary first ffprobe alias. Indexed artwork populates image tags, with `ImageTypeLimit` and `EnableImageTypes` selection.

Phase 3 adds bounded ExcludeItemTypes, Years, premiere/creation-date ranges,
minimum community rating, name-boundary and HasOverview/HasSubtitles/IsHD
predicates, plus CommunityRating/Runtime/ParentIndexNumber sorting. The
[navigation contract](../development/next-up.md#phase-3-selected-navigation-contract)
defines their exact validation and grouping boundaries. Unknown SortBy/Filters
remain errors; unimplemented query hints are inert and unimplemented Fields
are omitted, not claimed as supported. Search/Hints is not part of this selected
adapter scope; SearchTerm remains available. Selected protocol coverage passed
in the phase 3 record; broader reference/client parity is not implied.

The currently recognized Emby resources also accept root aliases and case variants of route literals. Dynamic IDs and escaped entity names are preserved. HEAD requests share the normal authentication and CORS boundary. Original media and finalized HLS segments support ranges and conditional requests; progressive audio/video have the distinct HTTP behavior below. See [original playback and user state](../development/direct-playback.md) for the historical direct-delivery contract. [Client sessions](../development/client-sessions.md) expose validated capabilities and the latest authorized Playing/Paused record per client context, with paths and UserData omitted from NowPlayingItem. Subtitle delivery now includes bounded embedded text extraction, ASS, font attachments, HLS WebVTT and supported HLS/progressive MP4 burn-in; [advanced media](../development/advanced-media.md) states the exact scope. [WebSocket events](../development/websocket-events.md) deliver committed user-state notifications and remote commands. Additional formats/events and complete consumer-client acceptance remain open.

Original media is reauthorized before headers and during long responses on a five-second watcher cycle. A confirmed permanent token/access/source failure cancels only the affected response, closes its descriptor, and interrupts blocked writes; after headers the response aborts rather than ending as a successful truncated transfer. Revocation is not instantaneous and cannot retract buffered bytes. Transient database/storage errors or timeouts preserve an existing grant for later revalidation. The native account mutation transaction and the delivery watcher remain separate responsibilities.

Universal audio treats `Container` and the optional route suffix as original-format capabilities; `TranscodingContainer` selects a conversion output. Omitted `Container` imposes no original-format restriction, and a compatible original takes priority over conversion fallback settings while respecting supported bitrate, sample-rate and channel ceilings. When conversion is needed, omitted or empty `TranscodingProtocol` selects progressive output, and `hls` selects MPEG-TS HLS. The implemented progressive outputs are MP3, AAC/ADTS, AAC in fragmented MP4/M4A, FLAC, OGG with Vorbis/Opus/FLAC, and signed 16-bit PCM WAV, with bounded copy/remux combinations. `Static=true` and the `original.{Container}` route deliver the complete original file; original-file `StartTimeTicks` does not become an estimated byte offset. See [Universal and progressive audio](../development/audio-playback.md) for selector precedence, exact output targets, codec limits and error behavior.

[Audio PlaybackInfo profiles](../development/audio-profile-playback.md) choose the first authorized and constructible declared HTTP/HLS output. An omitted or empty audio profile Protocol is treated as HTTP and Context as Streaming; Goby consistently emits `TranscodingSubProtocol=http`, explicitly normalizing the reference's omitted/empty DTO distinction. Required `CodecProfiles` and `ContainerProfiles` conditions are checked against projected output rather than borrowed source facts. The response retains original `MediaSources` facts while the progressive URL carries `Static=false`, concrete output settings, the original stream index, canonical play/source/device ownership and token. Encoding URLs disable automatic copy; approved copy URLs use `AudioCodec=copy`. The subsequent GET rechecks current source/policy and accepts a new `StartTimeTicks` seek through the ordinary route. No unsupported Static-only profile is turned into an unconstructible conversion URL.

[Progressive video PlaybackInfo](../development/progressive-video-playback.md) preserves HTTP/HLS profile order and treats omitted/empty Protocol and Context as HTTP/Streaming. Its supported progressive output is MP4 with H.264, HEVC or AV1 video and AAC for existing audio. `MediaSources` retains original-source facts; client constraints are evaluated against the separate projected output. The standard `/Videos/{Id}/stream.mp4` URL carries actual source stream indexes, concrete copy/encode settings, and a source-relative start. Nonzero copied video requires codec-specific random-access and runtime packet evidence. Standard profile negotiation can align to a proven preceding point within ten seconds and publishes the actual start with `CopyTimestamps=true`; exact/manual requests retain their explicit clock contract. Supported AAC-LC copying requires matching native-clock packet evidence; permitted AAC encoding or absent audio remain alternatives. Unproven copy requests fail or a normal codec request chooses permitted encoding. Encoded seeks retain the verified format clock and supported software restart/linear fallback paths; deinterlacing retains the required linear history. Supported software/AMD color processing, deinterlacing and progressive subtitle burn-in retain their documented source and backend restrictions. Exact output targets remain independent of ceilings; URL fields cannot inject a clock origin, duration, filter, or hardware configuration. See [advanced media](../development/advanced-media.md) and [copy seeking](../development/copy-seek-compatibility.md) for the implemented contract and completed selected-profile phase 1 acceptance.

`AudioChannels` is an exact target. `MaxAudioChannels` and `TranscodingMaxAudioChannels` are separate ceilings, with the stricter bound applied to conversion; an exact target above the bound is rejected. On Universal/legacy audio, the transcoding-only ceiling applies after compatible original-file selection and therefore cannot alone force an accepted original through conversion. HLS audio also treats these fields independently rather than reporting a duplicate-name conflict. Video stream requests treat explicit output ceilings as conversion requirements under their own route contract.

Progressive GET sends a complete selected representation with truthful MIME, `Accept-Ranges: none`, and no estimated Content-Length, ETag or Content-Range. A Range header is ignored with HTTP 200; seeking uses a new `StartTimeTicks` request. HEAD validates the source, plan, ownership and current permissions without starting an encoder. FFmpeg writes through nonseekable `pipe:4` into the private append-only `stream.bin`; the reader waits through temporary EOF and returns successful EOF only after durable successful completion and consumption of the final bytes. Failure or cancellation after headers aborts HTTP/1 or resets the HTTP/2 stream instead of presenting truncated media as a normal completed response. Progressive consumers share the existing conversion manager, queue, quotas, reader limits and media response slots.

Universal audio and converted audio/video can accept a fresh client-generated `PlaySessionId` without a preceding PlaybackInfo request. Migration `0012` binds client nonces to one canonical `play_...` identity and item/media source. Migration `0016` extends that ownership to `(nullable user, parent credential, application client, device, client nonce)` while retaining existing login references. Reports, Ping and cleanup resolve that owned binding; HLS revisions, child URLs and encoding jobs use canonical identities. Reusing a nonce cannot change its resource or revive a stopped or expired play, and another credential or client context cannot take over the reference. Preparation and media access do not substitute for playback-progress reports. See [client playback references](../development/client-playback-references.md) and [application-key operations](../development/application-keys.md) for lifecycle, tombstones and limits.

Probe cache version 5 introduced explicitly measured `FormatStartKnown`/`FormatStartTicks` for the progressive video clock. Current version 7 retains those facts, integer audio sample counts and effective packet boundaries, and extends private HEVC/AV1 restart, AAC-copy and Dolby Vision evidence. Format origin is independent of the first audible sample and is not assumed zero when missing. Normal library rescans are required after upgrading older probe versions: stale snapshots block indexed media and external-subtitle delivery until refreshed. Current metadata with unknown conversion timing can still permit original-file playback. Matroska/WebM's quantized packet clock remains outside the exact audio-only conversion subset; that restriction does not exclude separately supported video conversion with a verified format origin.

Ogg physical-page integrity, stream topology, codec headers, packet/frame association and priming/discard rules must all hold; arbitrary Ogg chaining and legacy FLAC mapping are not supported. Integer samples carry through audio seeking and resampling without reconstruction from rounded ticks. WAV uses an accurate prewritten RIFF header and bounded PCM output; substantially short or excessive output fails. Audio HLS merges an encoded tail of at most one output frame into the preceding segment and uses measured packet facts for copy, preserving total duration and stable numbering without advertising an unproducible tail URL. The [audio guide](../development/audio-playback.md) documents proof bounds, sample quantization, RIFF size limits and remaining timing profiles.

Local NFO data contributes `ProductionYear`, `PremiereDate`, `OriginalTitle`, `CommunityRating`, and `OfficialRating` when present, for detail requests or when selected with a same-named `Fields` value. These scalars are absent from the default item list, as confirmed by the NFO reference captures. Missing scalar values are omitted, while an explicit zero rating remains zero.

`Fields=ProviderIds,Genres,Tags,Studios,People` includes provider identifiers, `Genres` strings together with `GenreItems`, `TagItems`, `Studios`, and `People`. In the pinned reference, `Fields=Tags` selects `TagItems` objects rather than a `Tags` string array. Detail requests include these collections as well. Requested missing collections are empty arrays and missing provider identifiers are an empty object.

Studios, genre items and tags use objects with Name and persistent numeric IDs; people use persistent string IDs, Name, Type and optional Role. TagItems keeps its documented name order, and NFO credit order controls arrays without adding an unsupported SortOrder response field. Entity list/detail IDs are strings. Entities without visible associations remain inaccessible while retaining reusable identities. Phase 3 adds managed entity artwork and independent selected-user state; it does not generate collages or propagate entity ratings/played flags into associated media. The scanner retains hierarchy rules and does not expose raw sidecars or private hashes/paths. See [local metadata](../development/local-metadata.md) and [artwork](../development/local-artwork.md); these additions have recorded selected phase 3 acceptance.

Current media/entity artwork requires authentication and current catalog visibility before bytes, cache hits or conditional responses are returned. The pinned reference's public local-image observation remains historical and is not the phase 3 access rule. Public-login avatar visibility is a separate contract. Local indexed sources are validated before delivery and require a rescan after changes; managed payloads use their committed identity. See [artwork](../development/local-artwork.md) for uploads, deletion masks, restoration, ordering, limits and the recorded verification boundary.

Library authorization combines the supported account scope with `EnableAllFolders`, `EnabledFolders`, subfolder exclusions, parental/unrated/tag restrictions, and applicable feature gates. Counts, grouping and collection membership are filtered before paging. Device, remote-access and access-schedule rules also govern authentication/current authority; media policy adds bitrate and simultaneous-stream admission. Default ordinary users retain legacy library access unless restricted. Administrator and application-key exceptions remain explicit in the relevant operation; unsupported upstream policy fields do not acquire effects merely by being stored. See [catalog policy predicates](../../internal/library/policy_access.go#L51), [runtime media policy](../../internal/server/media_policy.go), and [managed policy](../../internal/identity/policy.go).

The query adapter supports `ParentId`, `Recursive`, `SearchTerm`, `StartIndex`, `Limit`, `Ids`, `IncludeItemTypes`, `MediaTypes`, and a limited single-key `SortBy` set. Optional `IsFolder`, `IsSpecialSeason`, and `IsSpecialEpisode` filters use actual typed catalog facts before counting or paging; season-zero classification and the unmodeled `IsStandaloneSpecial` boundary are documented in [TV query filters](../development/client-tv-query-filters.md). User-state filters include `IsPlayed`, `IsFavorite`, and the documented initial `Filters` subset in the playback guide. Entity filters include `Genres`, `Tags`, `Studios`, `Person`, `GenreIds`, `TagIds`, `StudioIds`, `PersonIds`, and `PersonTypes`. Name lists use a pipe delimiter; IDs accept pipes or commas. Same-dimension values use OR, separate dimensions use AND, and person/type conditions match the same credit association. Other upstream filters, default subtleties, composite sort expressions and the complete field model remain compatibility work.

MusicAlbum and Audio DTOs expose non-null music arrays derived from actual accepted tags and authorized persistent relationships. Phase 3 music probe version 3 adds explicit plural credits, composers, genres, track/disc numbers, dates and supported MusicBrainz IDs while preserving stored music-source version 1. A track's own album-artist group takes precedence over physical-album fallback; album publication requires complete accepted members and the documented consensus rules. MusicAlbum `ChildCount` counts actual same-library direct children. See [music metadata](../development/music-metadata.md) for limits, source precedence and the recorded phase 3 acceptance boundary. Historical source20 introduced music probe 2 and passed [43 targeted remote tests](../development/m3e-source20-targeted.json) and [1,763 full-source tests](../development/m3e-source20-full.json); its [candidate/client evidence](../development/verification-m3e-source20-similar.md) remains source-bound. Historical source18 MP3/FLAC evidence is likewise unchanged and does not validate this increment; consult [current status](../development/current-status.md).

Source20 introduced `GET /emby/Items/{Id}/Similar` for authorized catalog items,
using the observed item projection switches and same-type candidate scope.
Authorization, effective metadata scoring and UserData share a repeatable-read
snapshot. All eligible candidates are scored before pagination; the returned
count is the page length, including when `EnableTotalRecordCount=false`.
Movie `Limit=0` returns at most one result; music `Limit=0` returns none.
Explicit sorting overrides score order, while equal-score order is randomized.
`ExcludeArtistIds` considers the candidate's own Artist and effective AlbumArtist
groups. The scorer is a Goby model fitted to the [recorded comparison fixtures](../development/client-auxiliary-reads-plan.md),
not a uniquely recovered private ranking algorithm. Visible entity seeds and
unindexed `ListItemIds` remain unsupported. At that historical checkpoint,
ThemeMedia, wider aliases and broader music compatibility were open, and the
source18 deployment returned the recorded auxiliary 404 responses. Later
ThemeMedia and auxiliary increments have their own implementation and evidence;
those old 404 results do not describe the current source. Follow
[current status](../development/current-status.md) for current acceptance limits.

Music browsing supports the observed three-key `ProductionYear,PremiereDate,SortName` order and corresponding directions, plus current-user played-date/play-count ordering. Artist/album filters use actual authorized associations. Persisted Playlist/BoxSet containers now participate in catalog/detail and collection-parent queries; `ListItemIds` uses current authorized membership. Playlist results preserve order and duplicate entries through stable `PlaylistItemId` values, with counts based on visible entries. BoxSets use unique membership and can traverse authorized descendants. Both derive played summaries from visible members; only BoxSets accept bulk played-state mutations. Container deletion removes references, not media files. See [membership queries](../../internal/library/collections_query.go#L38), [membership filters](../../internal/library/query.go#L607), and [user-state distinction](../../internal/library/userdata.go#L254).

Subtitle labels retain indexed language, title, native codec and stream identity across the supported external/embedded representations. Observed English language identifiers display as `English`; unknown identifiers and custom titles retain their source text, and recorded forced/SDH flags remain visible. ASS-to-SRT/WebVTT conversion does not preserve ASS styling; use native ASS delivery or supported HLS burn-in when the client requires that styling.

## Delivery and health

`/admin/` serves the built React/MUI application, with route fallback for UI navigation. `/admin/v1` always goes through the API router, including unknown routes. `/healthz` exposes minimal liveness; `/readyz` checks PostgreSQL, the catalog ownership session, and the configured diagnostic store. An unhealthy diagnostic store returns `503 diagnostics_not_ready`. This policy is deployed with M5i. The service runs as an unprivileged Linux user in the test deployment.

There is no consumer web player by design. Original-file, Universal/progressive audio, progressive MP4 video, advanced text subtitles, HLS/progressive burn-in, TS/fMP4/packed-audio and encoded multi-variant HLS, configured dynamic-source conversion, software/AMD processing and proof-gated nonzero video-copy seeks are implemented in source. Playlists/collections and the supported management/policy interfaces are also connected. Historical M4e/M5a passes retain their own scope. The preceding September 19 feature wave is accepted within its recorded consolidated scope; provider-specific acceptance remains user-deferred. Phase 1 selected AMD/media execution, composed regression, both builds and resource/documentation closure are complete. Arbitrary input/timing/profile combinations, full third-party-client and Emby compatibility, OCI delivery work, and Live TV channels/tuners/EPG/DVR remain unsupported or unaccepted. See [advanced media](../development/advanced-media.md), the [phase 1 record](../development/amd-media-phase1-20260919.md), and the [feature-wave record](../development/feature-wave-20260919.md).

The [conversion engine](../development/transcode-engine.md) supplies PostgreSQL job records, FFmpeg execution and shared cache supervision for HLS and progressive audio/video. The adapters apply current policy, retain canonical playback ownership, address immutable output revisions and cancel owned work; HLS additionally builds source-global timelines. Configuration and exact supported limits are documented separately; command construction and software-only tests do not establish every client or hardware profile.
