# Startup and migration timeout verification

Date: 2026-09-09, Asia/Shanghai.

`GOBY_STARTUP_TIMEOUT` now accepts a Go duration from 1 second through 30 minutes, defaulting to 5 minutes. The application startup context uses that value. Database connection establishment retains its existing 10-second overall and 5-second connection limits.

`database.Migrate` bounds work by both the caller's context and a 30-minute maximum. Its transaction uses `SET LOCAL statement_timeout=0` before acquiring the migration advisory lock. Ordinary database connections retain their normal statement timeout after migration succeeds or rolls back. No existing SQL migration was changed by this increment.

The authorized Windows builds of the configuration/database packages and `cmd/goby` passed. On Linux `test-env`, a snapshot containing the existing five committed migrations passed:

```sh
go test -race -count=1 ./internal/config ./internal/database
```

Results: configuration 1.007 s; database 2.571 s. Tests cover duration defaults and boundaries, an observed migration-lock wait exceeding a 200 ms ordinary statement timeout, caller cancellation/deadlines, success on the same backend connection, failure rollback, and restoration of `SHOW statement_timeout`. Existing concurrent/idempotent migration tests also passed.

The in-progress playback migration was excluded from this isolated verification snapshot. Playback integration and deployment have their own subsequent evidence. These tests prove timeout scoping and cancellation; they are not a throughput benchmark for a large production catalog.
