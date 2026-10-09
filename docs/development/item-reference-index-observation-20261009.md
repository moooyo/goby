# Item-reference index observation

Migration 64 adds one item-leading btree to each of `user_item_data`,
`play_sessions`, and `media_operations`. The operation index excludes only rows
whose live `item_id` is null. `source_item_id` remains immutable historical
evidence; it is not substituted for the actual foreign key. No foreign-key
action, row shape, or publication trigger changes.

## Remote execution

Run only in the admitted remote PostgreSQL 17 environment, serially with other
database workers and using the shared Go caches. The existing integration
fixture creates and drops its own random schema. It does not modify preexisting
schemas. The operator supplies `GOBY_TEST_DATABASE_URL` privately.

```sh
GOBY_TEST_ITEM_REFERENCE_INDEX_OBSERVATION=1 \
  go test -p=1 ./internal/database \
  -run '^TestItemReferenceIndexesPostgreSQL17Observation$' -count=1 -v
```

The fixture starts at the exact published schema 63 and seeds 6,000 items,
129 users, 24,000 user-state rows, 30,000 playback sessions across all five
states, and 30,000 retained media operations, including 6,000 detached rows.
It uses revoked and unrevoked login histories, favorites, resume positions,
played rows, terminal operations, and historical source IDs distinct from the
live FK. This is a bounded synthetic database observation, not a natural-load
or media-scan throughput benchmark. Logs include actual relation and index
sizes so the operator can account for the admitted storage budget.

The same retained rows are observed before and after the trusted migration.
`VACUUM (ANALYZE)` prepares each phase; sequential scans remain enabled. Each
mutation runs in a rolled-back transaction. The log records full
`EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON)` results for the three explicit
referencing-row mutations, three 128-parent deletion trials with real FK
triggers, and 512-row inserts into each referencing table. The direct child
statements expose lookup plans and buffers; they are observations of equivalent
FK action statements, not captured internal trigger plans. The parent deletion
reports actual trigger times and the statement's buffers. The schema-64 child
plans must select their new indexes without planner forcing.

`sql_locked_ms` is the client wall-clock interval for the explained statement
and rollback after acquiring a library row lock, including protocol round trips.
The plan's `Execution Time` is the separate server-side statement observation.
Neither measures the production Go catalog-owner mutex, filesystem traversal,
or end-to-end scan completion. Timing, cache warmth, aborted tuple versions,
and WAL full-page images can affect individual samples; retain all raw trials
and compare buffer work separately from elapsed time. The insert observations
and index sizes expose maintenance cost rather than assuming indexes are free.

## Release catalog and regression coverage

Generate `internal/backuppg/catalogs/schema-64-postgresql-17.json` from an
independently owned, new empty PostgreSQL 17 database with the existing
`scripts/test-env/generate-backuppg-catalog.go` helper and explicit version `64`.
Never edit the schema-63 catalog to synthesize the successor. The catalog test
requires identical historical objects plus only the three indexes' nine
catalog objects, unchanged copy columns and FK constraints, and a complete
authenticated recovery drop plan.

`TestItemReferenceIndexesFreshSchemaPreservesDeletionSemantics`,
`TestItemReferenceIndexesUpgradePreservesSchema63`, and
`TestItemReferenceIndexesRecoveryMigrationRollsBackWithCaller` cover fresh
migration, repeated normal/recovery upgrade, exact history retention, caller
rollback, active and terminal playback rows, userless playback, detached
operations, and the existing publication deletion barrier.
`TestPostgreSQLItemReferenceIndexesRestorePreservesHistoryAndDeletion` covers
real schema-63 and schema-64 archives, original row fingerprints and sequences,
trusted migration, and restored CASCADE/SET NULL behavior.

This document describes the fixture and acceptance boundaries. Execution logs
and measured results must be recorded separately by the remote operator.
