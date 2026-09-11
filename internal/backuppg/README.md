# PostgreSQL backup engine

`OpenSnapshot` exports one bounded, read-only, repeatable-read transaction.
`Snapshot.Tx()` and `Snapshot.Context()` are available for additional witnesses
that must refer to the same snapshot. Callers must serialize access and close
the snapshot. The transaction changes its idle timeout locally because the
ordinary connection pool's 30-second idle limit is insufficient for a dump.

`Snapshot.Facts` hashes every trusted table, including migration history and
its original timestamps. Rows are ordered by a trusted unique identity's column
text representations with the `C` collation and `NULLS FIRST`. A primary key is
preferred; the userless client-reference table instead uses its actual complete
`UNIQUE NULLS NOT DISTINCT` constraint. Each PostgreSQL 17 `to_jsonb(row)::text`
value is prefixed with its unsigned, eight-byte big-endian UTF-8 byte length
before hashing. UTC, ISO dates, PostgreSQL intervals, hexadecimal bytea output,
and exact PostgreSQL numeric serialization are fixed for the transaction. The
engine retains one row at a time and rejects rows above 32 MiB.

`Snapshot.Dump` runs PostgreSQL 17 `pg_dump --format=custom` with the same
exported snapshot and an exact, quoted schema selector. It writes a caller-owned
private regular file before packaging and encryption. A `bytes.Buffer` is also
accepted for bounded tests. Arbitrary streams are rejected because a blocked Go
reader or writer cannot be cancelled reliably. The complete private file must
be discarded on any failure.

Source connection configuration is a single explicit PostgreSQL URI with
host, database, user, and SSL mode. It must equal the pool's original URI; decoded
identity fields and TLS policy are independently compared with the live pool
configuration. Supported libpq parameters retain their semantics; unknown
parameters, services, pool-specific options, implicit user identity, and
unverifiable TLS callbacks are rejected. In particular, pgx configurations
using a `verify-ca` callback are not supported by this first implementation.
Use percent encoding for a literal plus sign in query values. The dump retains
the original TLS hostname and pins the source transaction's TCP peer address.
Connection-pooling proxies that can route the same endpoint to different
PostgreSQL servers are not supported. Credentials are passed only in a controlled
child environment, never arguments, output, or returned errors.

`Restore` requires a separately configured, otherwise empty database and a
distinct, low-privilege role. The coordinator must reserve that database
exclusively. The engine never creates a database or role and never clears an
existing database. It rejects superuser, `CREATEDB`, `CREATEROLE`, replication,
RLS bypass, inherited administrative authority, and inappropriate schema grants.

`RestoreOffline` is the separate disaster-recovery entry point when the original
database is unavailable. It takes the original database and role names only
from the operator's trusted deployment `Options.SourceURL`, supplied through
configuration, a service unit, or the process environment. Archive content and
HTTP request fields must never supply this URI. The URI is strictly parsed;
names are decoded exactly once, remain case-sensitive, and cannot exceed
PostgreSQL's 63-byte identifier limit. The target's actual database and role
must both differ, even across hosts, and source-role membership is rejected.
No original-source DNS lookup, connection, TLS-file read, or SQL occurs. This
separation guarantee depends on the integrity of deployment configuration. The
existing `Restore` still reads source identity from its live read-only pool;
both entry points share all target and archive validation below.

`RestoreFinalized` and `RestoreOfflineFinalized` additionally require a trusted
Go finalizer. It runs after raw data/schema verification and trusted upgrades,
before the target commits. Use it for application validation, normalization,
and local ownership stamping that must be atomic with restored data. It must
not commit, roll back, or retain the transaction. Its error or cancellation
rolls the entire new target back. Plain restore entry points retain their
original contract; finalizer code never comes from archive or HTTP content.

The target is constructed from compiled migrations at the archived version.
`pg_restore` receives no connection and only decodes the custom archive using
`--data-only --no-owner --no-acl --file=-`. A strict PostgreSQL 17 text parser
accepts only a complete fixed preamble, trusted table COPY sections, trusted
sequence values, and the matching footer. It never executes archive SQL.
COPY data is validated without changing bytes and sent with synthesized COPY
statements through `pgconn.CopyFrom`. Newly created, trusted foreign keys are
temporarily removed to accommodate cycles and recreated with validation after
COPY. User triggers are temporarily disabled so restored data is not regenerated.
Every source table fingerprint, migration filename, server identity, and schema
signature must match before a target commit. Only compiled migrations can
upgrade older supported native archives. The coordinator must then normalize
credentials and runtime state before activation.

Sequence values are not MVCC facts. The engine preserves the values that
`pg_dump` observes later, separately from table snapshot fingerprints, and
requires the next allocated value to be within the trusted sequence range and
above all restored IDs. It does not claim sequences share the exported snapshot.

Schema 26 adds independent theme owner IDs, permanent reserved paths, and
resource classifications. Backup and recovery require exactly one virtual-root
owner and complete item-to-owner coverage. Resource rows must retain a valid
kind, nonfolder media type, registered root in the same library, canonical
parent/owner relationship, and reserved-path coverage. Active resources also
require an ordinary owner in that root, or the actual library CollectionFolder.
Inactive history remains valid when its owner moves between roots in the same
library or becomes reserved; it remains hidden from ordinary and direct reads.
These checks use the same database predicates as catalog visibility. They do
not allocate IDs or repair missing records. They run at snapshot/locked-recovery
inspection, after restored source data, after migration, and after a finalizer.
Archives from schemas 23, 24, and 25 keep their source semantics until the
trusted migration reaches 26. The independent theme sequence uses the existing
next-value validation rather than a shared numeric namespace.

Schema 27 adds permanent extra reservations and Movie resource classifications.
The source archive is validated using its recorded schema version before any
migration runs. All original table fingerprints and sequence bounds must match
first. The trusted migration may then reserve existing extra directories and
deactivate only active theme links whose owners fall inside those reservations;
it never infers an extra relationship from a historical path. Post-migration
validation uses combined catalog visibility and rejects shared Theme/Extra
resource IDs, including inactive history. Active extras require an ordinary
Movie owner in the same root. Inactive extras retain their parent, library,
canonical resource path and reservation while their owner may change eligibility.
The same semantic boundary runs after a trusted finalizer and during locked
recovery inspection. Any failure rolls back the entire target transaction.
Historical schema 26 catalog and Theme predicates remain unchanged.

Schema baselines under `catalogs/` are release artifacts generated using
`ExportCatalog` on a fresh database built from the compiled migrations. They
must be reviewed, verified on PostgreSQL 17, and committed. The first supported
native schema is whichever verified baseline is present; missing baselines
fail closed. Historical migration rows contain version, filename, and applied
timestamp, not SQL checksums. Baseline and archive checksum lists identify the
compiled SQL independently. No archive or live migration history is trusted to
supply executable DDL.

All external commands have time and byte limits, a private process group,
Linux parent-death signaling, and a pinned creating thread. The child leader is
kept waitable until the group has been retired to avoid signalling a recycled
process-group ID. Standard error is bounded and discarded. The decoder receives
no inherited PostgreSQL, loader, or credential environment.

Integration tests require two dedicated databases, each named with the
`goby_backup_` prefix and owned by independent ordinary roles:

- `GOBY_TEST_BACKUP_SOURCE_DATABASE_URL`
- `GOBY_TEST_BACKUP_TARGET_DATABASE_URL`
- `GOBY_TEST_BACKUP_DISPOSABLE_DATABASES=1`
- `GOBY_TEST_PG_DUMP` and `GOBY_TEST_PG_RESTORE` absolute PostgreSQL 17 paths

The tests create and remove only freshly generated schemas. Database and role
provisioning, baseline generation, and all test execution belong to the remote
test operator.
