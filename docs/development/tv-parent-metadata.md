# Authorized TV parent metadata

This product increment addresses the concrete relationship-field difference in
the [TV browse01 review](audited-tv-browse01-evidence-review.md). The browser's
unknown page error remains separate. A corrected DTO does not retroactively
accept TV browse01 or explain the error that preceded its episode request.

## Observed contract

The retained Emby 4.9.5.0 fixtures establish these default fields:

| Item | Fields when the relationship exists |
| --- | --- |
| Season | SeriesId and SeriesName |
| Episode | SeriesId, SeriesName, SeasonId and SeasonName |

The [seasons](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/shows-seasons.json)
and [episodes](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/shows-episodes.json)
requests omit `Fields`. The
[default recursive list](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/items-recursive-default-fields.json),
[Path-only projection](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/items-recursive-path-only.json)
and [image/userdata-disabled projection](../../tests/compatibility/fixtures/reference/emby-4.9.5.0/items-recursive-projection-off.json)
retain the same type-specific relationships. The earlier review also binds
complete Season and Episode detail wire responses. These observations support
default relationship fields; the OpenAPI definitions alone do not describe
their projection conditions.

## Implementation and boundaries

`library.Item` carries nullable Series and Season references containing only
ID and Name. The shared item query projects them inside its existing authorized
read transaction. It performs at most two indexed parent lookups as part of
that query, without an additional application query for each DTO. Detail,
list and batch reads share the same projection.

Each edge must follow `parent_id`, remain inside the item's library, identify
an ordinary visible item of the expected Series/Season folder type, and avoid
repeating an item already visited in the two-edge path. Permanent auxiliary
classification and reserved paths remain excluded even when an association is
inactive. A valid direct Season can remain visible when its Series edge is
invalid. A direct Episode-to-Series edge supplies only Series fields. Missing,
wrong-type, non-folder and cross-library edges supply no unproven reference.
The projection does not traverse arbitrary folders or validate the entire
ancestor graph.

Names come from the current stored catalog names, including successful metadata
overrides; they are not reconstructed from index numbers or filenames. Legitimate
virtual parents may have empty paths and may occupy another root in the same
authorized library. No filesystem read or same-root restriction is added.
The server emits proved relationships in default lists and details, preserves
the item's own identity and ParentId behavior, and does not fabricate empty IDs
or a Season's self-reference. Other item types receive no TV fields.

No migration, frontend, Live TV feature, global NextUp policy, authentication
mutation or deployed service change is part of this source increment.

## Verification

All formatting and verification run on `test-env`. The source archive is based
on the previously verified cancellation-fix archive; only nine reviewed library
and server files differ. The other 4,861 original files, migrations and frontend
inputs remain exact. Five files are new, bringing the archive to 4,870 files.

The first targeted race run passed 20 top-level tests and failed one new HTTP
fixture. Its assertions incorrectly normalized the scanned name `Season 02`
to `Season 2` and used an unsupported item-detail route. The product correctly
preserved the actual catalog name. The fixture now uses the implemented user
item-detail routes and verifies cross-library parents for both ordinary and
administrator readers. The original failed scope remains retained and closed;
no existing service or database was changed by verification.

The focused HTTP rerun passed with race instrumentation and zero skips. Its
worker and PostgreSQL are closed, and its source remained exact. The other
20 passing targeted checks are reused because only the failed fixture changed.
The [final full verification](tv-parent-metadata-full-verification.json) also
passed: 2,270 tests in 25 packages with race instrumentation, zero failures or
skips, and a Linux amd64 build. Independent saved-artifact reconciliation
confirmed every raw package result, all six new tests, the 4,870-file
archive/manifest/source agreement, binary identity and complete worker/PG cleanup.

The selected replacement binary SHA-256 is
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`
(29,561,402 bytes). Its source manifest is
`bce4d22a4c51dacca4660a6c8e8e3fac816141cd612a7b32b87367799e495cff`,
and source archive is
`b363afdcf707471c3a95288d04441bb7be89699010b09ca89c4c783e10436177`.
The currently running candidate is unchanged. Follow the
[bounded transition plan](tv-parent-candidate-transition-plan.md) before
installation, affected live admission or new original-client execution.

During the full run, available disk space fell to about 300 MB. A separately
recorded release removed only the reproducible Go build-cache directories in
the two already closed targeted scopes, reclaiming 518,562,973 logical bytes.
Their source, test logs, reports, module caches, PostgreSQL data and artifacts
remain; the original reports are unchanged. The active full scope was excluded
and continued without restart. The release descriptor is included in the
targeted-verification record.
