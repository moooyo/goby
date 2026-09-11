# Client preference persistence and schema evolution

Status: implementation and integration in progress for M3e. The original Web
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
