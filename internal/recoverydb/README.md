# Local database slot ownership

This adapter binds the deployment's fixed `primary` and `recovery` database
slots to local generations. The binding is one JSON value under
`goby.recovery.binding.v1` in `public.server_settings`. The current database
lease and the protected local lifecycle store remain independent authorities.

Construct a store with `New(pool, lease, Config{Postgres, DeploymentID, Slot})`.
`Postgres.SourceURL` identifies that specific pool and slot. Deployment identity
and slot come from local configuration and lifecycle control. Archive content,
database markers, and HTTP payloads cannot establish that identity. Every
operation requires the matching lease and a context cancelled when it ends.
`BoundContext` binds a whole restore and its final commit to the target lease.

`Read` runs before migration. It checks actual low-privilege database ownership
without requiring the current schema version. Its `ReadResult` distinguishes an
empty preconfigured public schema, a legacy `server_settings` table without a
binding row, and a present raw value. Invalid JSON remains distinct from absence.
The coordinator must reject a foreign marker before allowing migration.

`BindInitial` permits an absent marker to be created only for the configured
primary at local lifecycle revision zero, with an empty generation ID. Matching
existing initial identity is idempotent. It runs after trusted migration has
produced the current schema.

`Stamp` uses exact raw-value compare-and-swap. The expected bytes must have been
read locally after validating and normalizing the inactive target. The desired
marker must match the local deployment and slot and name a nonempty generation.
A stale expectation conflicts even if another call wrote the same desired value.

Use `StampTx` inside `backuppg.RestoreFinalized` or
`backuppg.RestoreOfflineFinalized` for production activation. These APIs verify
raw archive facts first, invoke a trusted Go finalizer, and commit after it
succeeds. Master-key and approved-root validation, credential/runtime
normalization, and the local marker can become visible atomically. Errors and
cancellation roll back the entire new target. `StampTx` preserves caller
transaction ownership, requires READ COMMITTED, and verifies the connection
configuration and actual server peer against the leased pool.

`Capture` reads the raw marker and every trusted table fingerprint inside one
`backuppg.OpenSnapshot` transaction. It accepts only the configured local
deployment/slot claim. Its `Retained` record includes database and role, complete
marker bytes, schema/migration facts, and every table's row count and digest.
Save it only in protected local control after the old active generation has
stopped ingress and drained all writers, including audit and maintenance work.

`ResetOwnedTarget(ctx, activeState, retained)` accepts only the inactive slot
named by that saved local proof. It checks deployment, generation, marker,
database/role, complete business-row fingerprints, schema, ownership, grants,
comments/security labels/default ACLs, and external objects and dependencies.
The standard public schema comment and its existing public USAGE are preserved.

Reset uses READ COMMITTED and acquires the complete trusted table set in a fixed
order with ACCESS EXCLUSIVE locks. It refreshes schema and fingerprints after
waiting, so a writer that commits while reset waits causes a conflict. Deletion
selectors come from the compiled schema artifact. All known triggers,
constraints, indexes, defaults, functions, and tables are removed using
RESTRICT. The final DROP SCHEMA also uses RESTRICT; a newly introduced unknown
object remains outside the deletion plan and causes rollback. The same
transaction recreates the empty schema with its prior owner. Cancellation or
failure rolls back earlier DDL. Successful reset changes the namespace OID;
failed reset preserves it.

The coordinator must keep the inactive database quiescent and exclusively
reserved throughout this protocol. The lease fences cooperating Goby processes
on one host; independent external DDL clients and routing proxies require
operator exclusion. The adapter delegates user intent, durable transition
records, generation publication, and HTTP lifecycle to the coordinator.

Integration tests require exclusive use of the two complete public schemas in
the dedicated PostgreSQL 15432 pair fixture, with distinct ordinary roles and
explicit ownership markers. Final database and role removal belongs to that
operator; the fixture never uses a fallback `DROP SCHEMA CASCADE` cleanup.
