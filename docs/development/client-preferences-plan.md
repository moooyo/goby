# Client preference persistence and schema evolution

## Selected phase 1 extension

The [account/playback contract](../api/playback-accounts.md) extends the completed
phase 3 baseline below. IntroSkipMode and EnableNextEpisodeAutoPlay have writable
consumers; ProfilePin uses encrypted state and owner-only authenticated
projection, while local-password enablement belongs to credential management.
Configuration/Partial is supported. DisplayMissingEpisodes and
HidePlayedInSuggestions remain phase 3 of the next plan. Original-client
acceptance is in progress; earlier successful records do not validate this
extension by implication.

## Current phase 3 integration boundary

UserSettings, User Configuration and DisplayPreferences are separate contracts.
The current [phase 3 increment](amd-media-phase3-20260919.md) adds selected
configuration and display-preference behavior with recorded persistence,
authority, consumer and selected API/media/administrator UI acceptance. Only fields connected
to the selected consumers belong to that increment. Retained client-only values
and unsupported playback features must not be described as server behavior.

The earlier feature-wave statement that `POST /Users/{Id}/Configuration`
already persisted was incorrect. Its [historical correction](feature-wave-20260919.md)
remains valid for that source. The phase 3 implementation is a later change and
does not retroactively validate the earlier claim or merge UserSettings into
User Configuration.

### Selected configuration and display-preference contract

Native `GET /admin/v1/users/{id}/preferences` returns
`{UserId, Revision, Configuration}`. `PUT` requires the saved decimal-string
Revision and a partial Configuration object. It uses a dedicated configuration
revision, independent of user-management revisions and UserSettings. A stale
revision conflicts; a committed preference change does not require a new login.
The native administrator cookie and mutation CSRF checks remain required.

Compatibility `GET` and `POST /emby/Users/{Id}/Configuration` read or atomically
merge the same persisted preferences; POST returns an empty `200`. Compatibility
writes have no required native revision. Current user logins can target themselves; administrators can
target another user. Application keys are rejected for these preference APIs.
Transactions recheck current account/session lifetime, device, remote-access,
schedule and preference policy. User DTO projection reads the stored values.

| Writable configuration | Default and consumer |
| --- | --- |
| `AudioLanguagePreference`, `SubtitleLanguagePreference` | Empty by default; valid bounded ISO/BCP-47 language tags select streams only when no explicit request selection exists |
| `PlayDefaultAudioTrack` | `true`; default audio selection |
| `RememberAudioSelections`, `RememberSubtitleSelections` | `true`; remember valid selections against current source identity; subtitle `-1` means off and differs from unset |
| `SubtitleMode` | `Smart`; exact `Default`, `Always`, `OnlyForced`, `None`, `Smart`, or `HearingImpaired` |
| `ResumeRewindSeconds` | `0`; integer `0..300`; applied for playback negotiation with `IsPlayback=true` and no explicit StartTime |
| `OrderedViews`, `MyMediaExcludes`, `LatestItemsExcludes` | Empty arrays; at most 1,024 bounded opaque IDs; view order/exclusion and Latest filtering |
| `HidePlayedInLatest`, `HidePlayedInMoreLikeThis` | `true` and `false`; current-user Latest and Similar filtering |

Preference request bodies accept JSON bytes as application/json or text/plain,
bounded to 128 KiB; native and Configuration routes reject business query
parameters. Languages are at most 64 bytes;
view identifiers are nonempty, trimmed, control-free strings of at most 256
bytes. Nulls and invalid/unknown or duplicate aliases reject the whole patch.
Omitted writable fields retain their current values.

Fields without a selected server consumer remain read-only compatibility
projections: `DisplayMissingEpisodes`, `EnableLocalPassword`,
`EnableNextEpisodeAutoPlay`, `HidePlayedInSuggestions`, and `IntroSkipMode`.
A same-value echo is accepted; attempting to change one is rejected. ProfilePin
accepts only an empty value because PIN authentication is not implemented.
The administrator editor omits write controls for these unsupported behaviors.

`GET` and `POST /emby/DisplayPreferences/{Id}` address a separate
`(user_id, Client, Id)` row, not a session/device-name key. UserId and Client
are required scope values; POST may supply Client in its body, agreeing with
any query Client. They use the same
user-login/administrator targeting boundary and reject application keys. GET
returns a decimal-string Revision; `"0"` means no stored row. POST can carry a
revision for compare-and-swap and returns an empty `200`. Fields are `SortBy`, `SortOrder`, and
`CustomPrefs`; defaults are SortName, Ascending and an empty map. A document is
limited to 64 KiB, 256 custom keys and 2,048 rows per user. Custom keys are at
most 256 bytes and values at most 16 KiB. Unknown custom values are opaque
client layout data, not newly implemented server features.
Duplicate keys in CustomPrefs are rejected rather than silently collapsed.

When Client plus DisplayPreferencesId (or the parent ID) identifies a saved
preference, an item list can use its default sorting; explicit query sorting
wins. If implicit preference access is denied, the list skips that default
instead of turning ordinary browsing into a preference-access denial.
Audio/subtitle defaults likewise never replace explicit request selection.
Remembered stream selections require the current media identity and matching
stream kind, including the active external subtitle's identity and content
fingerprint. Replacing a sidecar cannot inherit an old selection merely by
reusing its stream index. Static playback preferences apply to user logins,
after opening the current source; application-key requests do not acquire
personal remembered state. Positive rewind subtracts from a valid saved resume
position and clamps at zero; an explicit start, missing/out-of-duration resume,
or zero rewind leaves the request's start unchanged.
The server applies rewind to conversion/HLS planning. Direct original-file
playback still needs the client to consume the returned User.Configuration
timing preference; the server does not rewrite original-file bytes to seek them.
Hearing-impaired selection uses technical probe version 8 facts;
an old snapshot is not silently described as containing those facts. This
technical version is independent of music metadata version 3.

For user logins, Views applies saved order and exclusions after current library authorization;
explicit supported name sorting overrides saved order. Latest applies its
library exclusions before grouping/pagination. HidePlayed defaults apply to
Latest and Similar only when the request did not supply an explicit played-state
selector. Dynamic sources apply language, subtitle mode and default-track
preferences after probing the actual live source. They never reuse static
catalog resume positions or remembered stream indices from another generation.

Application-key catalog requests use neutral preferences for Views, Latest and
Similar, even with an explicit target UserId. That target still selects current
library ACLs and UserData, and explicit query filters remain effective. Neither
the target's stored fields nor fresh personal defaults are injected. Display
default sorting likewise skips application keys, while the separate
DisplayPreferences API rejects them. This boundary does not prohibit UserDto
reads allowed by the existing key authority. Source06 exposed the preference
coupling as a product regression; the selected repairs and final composed server
coverage passed in the phase 3 ledger. The original failed run is retained.

Source: [validated preferences](../../internal/identity/user_preferences.go),
[isolated persistence and authority](../../internal/identity/user_preferences_store.go),
[HTTP adapters](../../internal/server/user_preferences.go), and
[stored selections](../../internal/library/playback_preferences.go),
[playback defaults](../../internal/server/playback_preferences.go),
[dynamic-source integration](../../internal/server/dynamic_sources.go),
[Views/Latest consumers](../../internal/server/items.go), and
[Similar consumer](../../internal/server/similar.go).
The selected migration/restart, API/media and Goby administrator journeys passed
their recorded gates. Full original Emby Web compatibility remains outside this
acceptance; it is not inferred from the administrator workflow.
Migration [0037](../../internal/database/migrations/0037_user_preferences.sql)
adds the independent preference/state storage. It is separate from the historical
schema-24 UserSettings plan below; previous migration bytes and accepted old
configuration must be preserved through the integrated upgrade and recovery gate.

### UserData and legacy playback reports

The selected state adapters passed the recorded phase 3 scope. Paths below are
relative to `/emby` and require current selected-user
authority and current item/entity visibility. They return `200` UserData with
ItemId included. Rating and Likes are omitted when null; HideFromResume is
emitted only when true.

| Route | Supported operation |
| --- | --- |
| `GET /Users/{UserId}/Items/{Id}/UserData` | Read actual selected-user state |
| `POST /Users/{UserId}/Items/{Id}/UserData` | Atomic supported-field patch |
| `POST /Users/{UserId}/Items/{Id}/HideFromResume?Hide=...` | Required explicit Boolean and no body |
| `POST /Users/{UserId}/Items/{Id}/Rating?Likes=...` | Required explicit Boolean and no body; stores rating 10 or 0 plus Likes |
| `DELETE /Users/{UserId}/Items/{Id}/Rating`; POST `.../Rating/Delete` | Clear Rating and Likes; no body |

Writable patch fields are PlaybackPositionTicks, PlayCount, IsFavorite, Played,
LastPlayedDate, Rating, Likes and HideFromResume. Position is nonnegative and
bounded by known media duration; a source with unknown duration accepts only
zero. PlayCount is an integer from 0 through 2,147,483,647. LastPlayedDate is a
nullable RFC3339 timestamp, Rating a nullable finite number from 0 through 10,
and Likes a nullable Boolean. Missing values preserve state; null clears only
the nullable fields. ItemId/ServerId echoes must agree with the request/server.
Derived PlayedPercentage and UnplayedItemCount are rejected as writes.

HideFromResume is durable and excludes an item from Resume without discarding
its progress. A numeric Rating update clears obsolete Likes unless the same
patch supplies Likes. Marking a media item Played clears its position; ordinary
played-state bookkeeping and notifications remain active. Generic folder
playback fields are rejected because their values are derived; use the existing
PlayedItems operation for supported recursive changes. Entity IDs use independent
[entity state](local-metadata.md#persistent-catalog-entities), never an associated
media row; nonzero entity progress and HideFromResume=true are invalid.

Legacy POST `/Users/{UserId}/PlayingItems/{Id}` and `.../Progress`, plus DELETE
on the base route or POST `.../Delete`, translate to the existing Started,
Progress and Stopped report handler. Optional JSON bodies are bounded to 16 KiB;
supported query/body values must agree, including item, user, source and play
identities. Reports must belong to the current logged-in user; application keys
and cross-user administrator reports are rejected. The original state machine
owns idempotent counts, pause/stop, heartbeat, notifications and resource cleanup.
Legacy success is empty `200`; modern Sessions/Playing reports retain `204`.
No second playback state machine or implicit administrator impersonation is added.

Source: [state and legacy HTTP adapters](../../internal/server/userdata_management.go),
[media state transaction](../../internal/library/userdata_update.go), and
[entity state transaction](../../internal/library/entity_user_data.go).

## Historical M3e planning and evidence

Status at that checkpoint: implementation and integration in progress for M3e. The original Web
Client and five isolated reference studies now establish UserSettings GET and
Partial semantics. A bare dictionary Full POST returns HTTP 400 with the
reference message `Expected configuration type is UserSettings`; no replacement
semantics are claimed for that request. The new independent table, handlers and
schema-24 catalog are present. Eighteen targeted migration/domain/parser/HTTP
race tests passed, while complete regression, cross-version restore and real
candidate-client acceptance remain pending.

## Observed contract

The [actual UI request](m3e-reference-preferences-client.json) sends a single-level
JSON object to `/UserSettings/{UserId}/Partial` as `text/plain`. The
[merge/deletion study](m3e-user-settings-study-v2.json) also accepts JSON and
octet-stream MIME, retains unspecified keys, inserts new keys and deletes null
values. The [value study](m3e-user-settings-study-v3.json) establishes scalar
conversion, case-insensitive top-level keys, first spelling with the final value,
and last-wins duplicate keys. The
[existing-value and identity control](m3e-user-settings-study-v4.json) proves empty
strings delete existing keys and the sampled ordinary GET requests select the
authenticated user's map regardless of the path's other known user ID.

The [nested-value control](m3e-user-settings-study-v5.json) proves ordered recursive
conversion without adding JSON/CSV quotes: nested duplicates and case variants
remain in order, null becomes literal `null`, empty strings become empty text,
and empty containers retain `{}`/`[]`. This is the observed conversion, not a
claim that the endpoint implements the publicly documented JSV format.

The [final study audit](m3e-user-settings-study-safety.json) accounts for 120
settings-study requests and one public OpenAPI read. All seven new recorder
tokens were logged out and denied afterward. Every batch restored its acknowledged
baseline; the final viewer2 baseline contains explicit
`{"genreLimitOnDetails":"1"}` after the real UI restored its visible default.
The primary viewer's playback history/configuration/policy and the media were
preserved. Administrator/application-key GET targeting and nonempty cross-user
Partial authorization remain unverified reference cases; do not infer them from
an empty no-op response.

## Storage boundaries

Keep UserSettings, DisplayPreferences, and UserConfiguration separate. The
existing `users.configuration` column belongs to UserConfiguration. Its current
absence from the public identity model does not make it available for unrelated
client settings. `managed_settings` is a server-wide, revisioned runtime policy
store and is also unsuitable for client preferences.

Prefer independent preference rows with user foreign keys. If the final
UserSettings evidence confirms the observed user-global scope, use the user ID
as its key. DisplayPreferences needs the actual client and preference-ID keys
specified by its contract; do not substitute session IDs, User-Agent or device
names. Payload limits must cover total bytes, key count, depth and legal values.
Authorization must be rechecked inside the transaction, with the established
account-before-session lock order. Partial updates must apply to the latest
locked data. Merge depth, null handling, deletion and replacement are evidence
questions, not arbitrary implementation choices.

## Migration and backup obligations

1. Append a migration after 0023. Preserve every historical migration byte and
   the existing schema-23 PostgreSQL catalog. New preference rows should preserve
   all preexisting account, configuration and business data.
2. Generate and retain a schema-24 catalog from a real, fresh PostgreSQL 17
   fixture after applying the exact migrations. A new table needs deterministic
   keys/order; even a new column changes the catalog signature. Do not generate
   a catalog by hand or reuse the schema-23 signature.
3. Existing restore code already creates the archive's trusted source schema,
   validates COPY data against that source catalog, applies later migrations,
   checks the current catalog and runs the atomic finalizer. Preserve this
   version distinction. An archive's source summary and table fingerprints stay
   at their actual source version even when the result reaches schema 24.
4. Add real schema-23 archive to schema-24 restore coverage, including preserved
   old configuration and new default preferences, online/offline paths, failed
   finalization, restart and rollback. Also restore a schema-24 archive containing
   nonempty preferences for multiple users/clients. Tests that only create the
   newest schema do not cover the cross-version path.
5. Update only assertions that mean "migrated to latest": current database,
   settings/library migration tests, activity backup coverage and latest catalog
   diagnostics. Retain historical schema-23 fixtures and reports with their
   original meaning. Audit recovery-manager fixture cleanup for any new tables.
6. Add a reviewed schema-23 to schema-24 candidate upgrade path. The current
   client-fixture operator requires schema 23 and byte-identical selected state
   before/after an executable replacement. Its preservation snapshot covers
   selected tables, not all 29 tables. A schema migration needs explicit old-row
   and old-column preservation, a single appended migration, correct new-table
   defaults, and unchanged media, configuration, credentials and recovery files.
   Only after that gate may the fixture's recorded schema advance.
7. Keep historical M5j deployment scripts pinned to their historical baseline.
   A future main-service upgrade needs a fresh protected deployment gate; it
   cannot reuse the one-shot M5i-to-M5j operator or restore an old backup over
   newer accepted state.

No archive-format change is required merely to add preference tables. Native
archives already include versioned PostgreSQL data and migration facts. Current
recovery cleanup should continue revoking credentials and ending running work
without deleting restored preferences.

Relevant implementation: `internal/backuppg/catalog.go`,
`internal/backuppg/restore.go`, `internal/backuppg/recovery.go`,
`internal/backuppg/recovery_plan.go`, `internal/recovery/restore.go`, and
`scripts/test-env/generate-backuppg-catalog.go`.
