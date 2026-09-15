# Native user deletion increment

This independent M5 increment is being prepared on `codex/m5-user-deletion`,
based on `964a949`. It is not part of the frozen Programs artifact and has no
runtime, browser, migration or release acceptance yet. The current Programs
diagnosis, artifact and client gates remain separate.

The native administrator contract is `DELETE /admin/v1/users/{id}` with the
loaded detail's canonical `{Revision: string}` body. Success returns
`200 {CurrentSessionRevoked: boolean}`. Existing cookie, CSRF, origin, strict JSON,
revision and error conventions apply. Self deletion is permitted when another
enabled administrator remains; its successful response clears the admin cookie
and the dashboard returns to sign-in without issuing logout again.

Deletion must be atomic with the `user.deleted` activity event. It revalidates
the actor inside the transaction, serializes the final enabled-administrator
check, and retains deterministic account/session lock order. Self deletion uses
the expiry read from the locked credential for its final database-clock check;
it does not add an exception to the general administrator authorization helper.

Existing foreign keys remove the target's authentication and personal state.
Server-owned application keys, shared devices, library/media records and media
files remain; creator/last-user attribution follows the existing SET NULL rules.
Saved activity identities remain historical values. After commit, process-local
event, HLS and encoding consumers of the deleted credentials must be retired.
Deleting encoding_jobs rows alone is not process cancellation.

The dashboard uses an explicit confirmation against a loaded revision. Cancel
does not submit, concurrent clicks cannot submit twice, and conflict or unknown
network outcome requires a fresh state read before another decision. Existing
unsaved drafts and navigation protection remain effective.

Migration 0029 extends the audit action and action/resource constraints without
rewriting historical migrations. Its schema29 PostgreSQL17 catalog must be
generated from a newly owned disposable database with the existing
`scripts/test-env/generate-backuppg-catalog.go`; no catalog may be inferred or
copied from schema28. That artifact and all execution remain pending the
coordinated test-environment window.

Remote acceptance must cover transaction/revision/expiry and last-administrator
races; audit-failure rollback; exact cascade and shared-resource preservation;
consumer retirement; strict HTTP inputs and authority; and administrator browser
confirmation, conflict, uncertain outcome and self deletion. Fresh/upgrade schema
checks and version-appropriate backup validation accompany the real catalog.
Relevant regressions and final frozen-source verification are required before
this separate increment is accepted or merged for delivery.

Core transaction, consumer-retirement and migration changes have received
independent static review. A 36-file preparation snapshot was retained at
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/m5-user-deletion-source-20260916-01`.
Its 28 Go files were formatted on `ssh test-env`, and the new SQL bytes matched
the appended migration-manifest digest. The preparation record is
`source-preparation.json` (24,538 bytes, SHA-256
`27d5d6cc7bd3638f9358479ef2163bd1a338d6a8a658cb92decdb0b272a71754`).
Formatting is not a Go compile, type check, migration test or browser result.
The snapshot precedes this evidence note and is not an executable worker input.

Backup/restore test wiring now separates source and target versions. Explicit
schema23-27 source fixtures retain their original facts and migrate to target29.
The root-binding cases retain explicit source28 archives, compare every historical
row and sequence, and expect target29. Current snapshot and extra fixtures use
schema29 for both ends. The pure historical catalog28 parser and migration boundaries
remain unchanged.

Two additional regressions are prepared: a schema29 archive roundtrip that
retains a committed deletion's historical actor/session IDs and exact table and
sequence facts, and a schema28 decoded stream containing the later user.deleted
action. The latter preserves the original archive, computes matching modified
row fingerprints through a rollback-only temporary view, and requires rejection
before the latest-version upgrade or finalization, with an empty rolled-back target. Its unchanged
archive control first reaches a deliberately refusing current-schema finalizer.
These are test definitions, not observed results.

The eleven changed test files were formatted remotely. Their source-preparation
record is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/m5-restore-source-20260916-01/source-preparation.json`
(8,211 bytes, SHA-256
`796c7bb8a13cdf80fa39760ac865072e0fac30907e13d49b2f1c8fcfa093c5a5`).
No tests, build, SQL or service operation ran during this preparation.

The actual schema29 catalog, backup/restore version-boundary acceptance and all
runtime verification remain open. No skip or synthetic catalog replaces that work.
