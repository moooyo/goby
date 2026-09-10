# Native backup and recovery API

Status: implemented, accepted and deployed in M5j, schema 23/probe 6.
The [engineering record](../development/backup-recovery.md) links the final
1605-test race suite, administrator browser/offline-CLI journey, protected
deployment and main-service backup/download workflow. These routes do not implement the upstream Emby BackupRestore
archive or plugin API.

All operations require a live administrator cookie. Mutations also require
same-origin checks and `X-CSRF-Token`. Application keys and Emby login tokens do
not authorize this API. Every response has `Cache-Control: no-store`. Native
errors use the existing `Error.Code`, `Error.Message`, and `RequestId` envelope.

## Operations

| Method | Route | Body / result |
| --- | --- | --- |
| GET | `/admin/v1/backups/status` | `StatusView` |
| GET | `/admin/v1/backups` | `BackupPage`; optional `StartIndex` and `Limit` |
| GET | `/admin/v1/backups/{Id}` | `{ "Backup": BackupView }` |
| POST | `/admin/v1/backups` | `{RequestId, Passphrase}`; `202 {Operation}` |
| POST | `/admin/v1/backups/import` | Age file bytes as `application/octet-stream`; `X-Backup-Request-Id`; `202 {Operation}` |
| GET / HEAD | `/admin/v1/backups/{Id}/file` | Attachment with fixed safe name, length, SHA-256 ETag, and byte-range support |
| DELETE | `/admin/v1/backups/{Id}` | `{RequestId, SHA256}`; `202 {Operation}` |
| GET | `/admin/v1/backup-operations` | `OperationPage`; optional `StartIndex` and `Limit` |
| GET | `/admin/v1/backup-operations/{Id}` | `{ "Operation": OperationView }` |
| POST | `/admin/v1/backup-operations/{Id}/cancel` | `{Revision}`; `202 {Operation}` |
| POST | `/admin/v1/restores/plans` | `{RequestId, BackupId, SHA256, Passphrase, RestoreDefaults, ReplaceRollback, GenerationRevision}`; `202 {Operation}` |
| POST | `/admin/v1/restores/{Id}/apply` | `{Revision, GenerationRevision}`; `202 {Operation}` |
| POST | `/admin/v1/restores/rollback` | `{RequestId, GenerationRevision}`; `202 {Operation}` |

JSON request bodies are strict UTF-8 objects of at most 16 KiB: unknown,
duplicate, missing required, invalid surrogate, and trailing fields are rejected.
Request IDs are exactly 32 lowercase hexadecimal characters. The server assigns
independent random operation and object IDs. A retry of an admitted request does
not replace its passphrase or other arguments. Non-secret argument conflicts
are rejected. Idempotency records remain available for at least seven days;
clients must never reuse an attempt ID for a new request. The bounded journal
rejects new work instead of evicting recent records early. Pagination defaults
to `StartIndex=0`, `Limit=25`; limits are 1–100.
Routes without documented query parameters reject queries.

HTTP errors use `400 invalid_input`, `422 invalid_archive`, `404 not_found`,
`409 conflict`, `409 busy`, `409 target_not_ready`, `507 capacity_exceeded`,
`413 payload_too_large`, `503 backup_unavailable`, and `408 request_timeout`.
Authentication and origin/CSRF failures retain the existing 401/403 contract.
These HTTP error codes are separate from the durable operation error codes.

Passphrases must contain 12–1024 valid UTF-8 bytes and are preserved exactly.
They are never returned, logged, archived as configuration, or persisted in the
operation journal. The first accepted request determines the passphrase. An
interrupted upload requires a new attempt ID; uploads do not resume partial
bytes. Imports remain unverified until complete authenticated extraction,
PostgreSQL validation, and key validation succeed.

## Projections

The Go definitions in `internal/recovery/api_types.go` are the field-name
reference. Native field names use PascalCase. Optional source information is
JSON `null`; unavailable text fields use an empty string. Counts that can exceed
safe JavaScript integers, sizes, revisions, and schema versions use decimal
strings. UTC timestamps use RFC 3339. Page counts and limits are bounded integers.

- `BackupView`: `Id`, `Kind`, `State`, `CreatedAt`, `UpdatedAt`, `SizeBytes`,
  `SHA256`, `Verified`, `ErrorCode`, and nullable `Source`.
- Backup kinds: `generated`, `imported`. States: `writing`, `ready`, `failed`,
  `cancelled`, `interrupted`, and `deleting`.
- `SourceView`: `ServerId`, `ServerName`, `GobyVersion`, `SchemaVersion`,
  `CreatedAt`, and `Tables` containing `{Name, Rows}`. Imported backups do not
  expose unvalidated manifest content as verified source information.
- `OperationView`: `Id`, `RequestId`, `Revision`, `Kind`, `State`, `Phase`,
  `BackupId`, `CreatedAt`, `UpdatedAt`, `ErrorCode`, nullable `Source`,
  `RestoreDefaults`, `ReplaceRollback`, `CanCancel`, `CanApply`, and
  `GenerationRevision`.
- Operation kinds: `create`, `import`, `delete`, `restore`, `rollback`.
  States: `pending`, `running`, `ready`, `applying`, `completed`, `failed`,
  `cancelled`, `interrupted`. Phases describe actual work: `admission`, `upload`,
  `snapshot`, `encryption`, `publication`, `validation`, `staging`, `ready`,
  `activation`, `rollback`, `cleanup`, `finished`.
- `StatusView`: availability and fixed reason codes, restore availability,
  `Busy`, `ActiveOperationId`, `GenerationRevision`, `Limits`, `Storage`, and
  `Rollback`. Limits contain `MaxBackupBytes`, `MaxStoredBytes`, `MaxBackups`,
  `MinPassphraseBytes`, and `MaxPassphraseBytes`. Storage contains `Bytes` and
  `Objects`.
- `Rollback`: `Available`, `MustReplace`, nullable `CreatedAt`, `ServerName`,
  `Generation`, and `UnavailableReason`. `MustReplace` means planning another
  restore requires explicit consent to discard the older inactive copy.

Availability reasons are fixed codes, never database URLs or paths:
`storage_unavailable`, `tools_unavailable`, `database_unavailable`,
`recovery_database_not_configured`, `recovery_required`, or empty when available.
Operation error codes additionally include `invalid_archive`, `capacity_exceeded`,
`target_not_ready`, `source_changed`, `authority_changed`, `audit_unavailable`,
`operation_cancelled`, `operation_interrupted`, and `activation_failed`.

## Workflow

Creation and import are durable jobs. The page polls operations while they are
active and offers a download only after the object is ready. Downloads use
browser attachment streaming; the dashboard should not buffer a multi-gigabyte
backup in a JavaScript Blob. Sensitive download preparation and streaming must
revalidate current administrator authority.

Planning requires the selected object's exact SHA-256 and the current generation
revision. The source is decrypted and checked, and a separately configured
inactive database is built and normalized. `RestoreDefaults` controls only the
five logical deployment defaults. Network, storage, hardware, secrets, and
approved media roots remain target deployment policy. `ReplaceRollback` must be
true when an older inactive copy must be cleared; the API cannot silently infer
that consent. A ready plan provides `CanApply=true` and a fresh revision.

Applying a plan drains the current application, preserves its database and
matching configuration/key as a rollback copy, activates the staged generation,
and requires startup acceptance. Restored credentials are revoked, so users sign
in again with the restored account passwords. The administrator page must not
interpret a `202` response as proof of successful activation. Completed status
and subsequent authenticated operation establish the result.

Rollback selects only the verified retained copy owned by this deployment.
Explicit rollback revokes its old credentials before activation. Failed startup
before publication to clients may return to the previously running generation.
Every transition remains subject to durable state and database ownership checks.
Cancellation is permitted only when `CanCancel=true`; an accepted activation is
not cancelled by disconnecting its initiating HTTP request.
