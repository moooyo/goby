# M5b: persistent administrator metadata

Verification date: 2026-09-09. Status: **passed for this increment**.

This checkpoint delivers native administrator metadata editing and preserves
the previously verified media pipeline. It does not complete Emby metadata
mutation compatibility or the full administrator milestone. Separate unfinished
video-seek work was deliberately excluded from this checkpoint's source and
deployment; it remains part of the active goal.

## Source and environment

All functional, database, media and browser checks ran through `ssh test-env`.
Authorized local Go and frontend builds also passed. The Linux toolchain remains
Go 1.27.1, FFmpeg/ffprobe 9.0.1 and PostgreSQL 17.11. The application database is
on port 5432; Go tests use isolated schemas on the owned 15432 scratch cluster,
and each browser workflow uses its own temporary database and low-privilege role.

The tested snapshot is `/opt/goby-test/verify-m5b-20260909`. It combines the final
M5b changes with the media/playback/transcode baseline from `4100e8373a52dd264297f1f28352b27064759587`.
Schema 14 and probe version 5 are the deployed checkpoint. All **281 Go/module
files** matched the assembled source archive. Their aggregate manifest hash is
`3717491238ebbfd1283e85b31eb866490e71943449589f521a0b084ac4620387`.

The final Linux executable hash is
`210c6c075cd80db403c1a28f1a5672d9cdbdb93a81ef76dbba61dc4d83eea197`.
Both browser workflows used this same executable. The final administrator asset
archive hash is
`b46cf5b31f66707046f58e08c6b9da6ba536ef78cb88d1f4fa1034bcfa3fdc08`.

The [deployment evidence](m5b-deployment-evidence.json) verifies the running
executable, all 281 staged Go/module files, all 31 final administrator assets,
and actual HTTP index/JavaScript bytes against the verified artifacts.

## Complete Go verification

`go test -race -count=1 -v -p 2 ./...` passed: **888 top-level tests**, all twelve
tested packages, zero skipped tests and zero race findings. The command package
has no tests. The Go and SSH wrapper exit codes are both zero. The
[machine-readable summary](m5b-full-race-summary.json) retains every package,
timestamps and the complete log hash.

| Package | Duration |
| --- | ---: |
| artwork | 1.424 s |
| config | 1.019 s |
| database | 2.830 s |
| events | 1.076 s |
| identity | 97.212 s |
| library | 23.227 s |
| media | 4.485 s |
| metadata | 1.523 s |
| playback | 1.185 s |
| server | 333.087 s |
| subtitle | 1.112 s |
| transcode | 52.869 s |

The metadata tests cover sparse overrides and locked snapshots, explicit empty
values, revision conflicts, source/hash changes, NFO removal, no-op writes,
current administrator authorization, transaction rollback and effective entity
projections. Rename/reclassification tests retain inactive episode settings
without applying them to a Movie; removing those settings prevents their later
return when the item becomes an Episode again. Structural season fields remain
read-only, and an editable episode number cannot be null.

Source-preservation tests retain unknown top-level and unchanged nested person
fields. Repeated scans of a manually titled/numbered file report zero updates
and leave its row, metadata state and entity associations unchanged; a rejecting
entity-write trigger independently confirms that those scans did not rebuild
the associations. Folder scan bookkeeping retains its earlier behavior.

HTTP checks exercise exact native query/body contracts, nested duplicate-key
rejection, public versus internal DTO fields, UTC timestamps, cookie/CSRF/Emby
token separation, stale-actor rejection and absence of user-state writes during
metadata browsing. The final suite includes the new `InactiveFields` envelope;
two earlier assertions still expected the previous ten-field envelope and were
updated to eleven fields without changing production behavior.

## Migration and deployment

The service restarted as UID 995 (`goby`), PID 3379452, with schema 14 and its
existing database. The [before](m5b-migration-before.json) and
[after](m5b-migration-after.json) migration observations compare all existing rows
in ten stable tables, including the complete pre-existing user revisions.
Every row remains identical. All 21 catalog items receive metadata state with
revision 1, empty controls, no editor audit values and the exact previous NFO
projection. The new source-key values match the current catalog context.

The [deployed read-only workflow](m5b-deployed-metadata-read.json) passed on its
first attempt with no HTTP retries. It verifies all five libraries and sixteen
non-root items. Its 39 GET requests leave all seventeen public tables unchanged.
One login and logout create and revoke only the workflow's own session; all 73
pre-existing authentication rows and all other existing rows remain unchanged.
The revoked cookie receives 401. This workflow does not open source media or
NFO files. Actual editing and rescanning are covered by the isolated browser
workflow using the same executable, without changing the retained catalog.

The dedicated migration test also covers SQL NULL, JSON null, arrays and scalar
legacy metadata values, mature 64-bit user revisions and existing related
entities/images/subtitles. Migration 0014 adds state and an insert trigger; it
does not rewrite the old catalog. No rescan is required for this schema upgrade.

A private PostgreSQL custom-format dump, old executable and old administrator
assets are retained in the marked `/dev/shm/goby-m5b-deployment-backup` directory.
The dump remains on the test host and is not part of the repository. Creating
this deployment backup is not an implemented product backup/restore workflow.

## Browser acceptance

The [metadata workflow](m5b-metadata-browser.json) passed in **31.870 seconds**
with one complete scenario, no failures, no skips and no flaky tests. It uses
28 small real media files in three dedicated libraries, with root-owned media
that the application can only read. The test covers searching/filtering/paging,
multi-field edits and collections, source comparisons, locks across NFO changes
and normal rescans, restoring automatic values, two-editor revision conflicts,
unsaved changes, mobile controls and inactive settings after reclassification.
All 28 original media byte sequences remain unchanged. Restart preserves the
complete item/metadata-state/entity snapshot.

The [first browser attempt](m5b-metadata-browser-attempt-1.json) completed scanning
and the first complex save before a test locator matched both the dialog footer
and alert-dismiss buttons named Close. The selector was narrowed to the footer's
visible text. Its cleanup passed, and the final run used unchanged application
code and assets.

The shared isolation runner gained small overridable hooks. Its default behavior
was separately checked by the [user-management regression](m5b-users-browser-regression.json),
which passed in **12.869 seconds** with no failures or skips. Both final runs
pass all ten cleanup checks: their temporary databases, roles, processes,
runtimes and credential files are removed, original HBA bytes are restored, and
the shared service and pre-existing database catalog remain unchanged.

Desktop and mobile screenshots were inspected for accessible, unobscured
controls and absence of horizontal overflow:
[item management](screenshots/metadata-items-desktop.png),
[metadata editor](screenshots/metadata-editor-desktop.png),
[inactive settings](screenshots/metadata-inactive-desktop.png), and
[mobile editor](screenshots/metadata-editor-mobile.png).

## Reference and remaining scope

The [metadata reference study](../research/metadata-reference.md) adds 130 records
to the prior 828: 106 complete HTTP exchanges and 24 supporting observations.
The old raw/export pairs and existing media hashes are preserved. Its sorting,
lock and forced-refresh results do not establish equivalence with Goby's
separate native administrator contract.

Online metadata providers, Emby mutation adapters, complete policies,
devices/API keys, generic tasks, settings, audit/log browsing and product backup
and restore remain open. The dashboard still contains no consumer player.
Video fast-seek analysis, actual GPU execution and broader client acceptance
remain separate required work.
