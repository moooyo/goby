# Main read-only preparation review

Reviewed on 2026-09-14. This increment resolves configured paths and the
captured lifecycle selection, actual main PostgreSQL cluster and application
database bindings. The [safe checkpoint](audited-main-readonly-preparation.json)
pins the private source records and separates these facts from startup admission.
It does not admit main startup, backup creation, migration, restoration or
promotion. The [execution plan](../planning/current-execution-plan.md) and
[main upgrade contract](audited-main-upgrade-plan.md) retain those separate gates.

## Configuration and captured stores

The private capture binds the loaded main unit, its permanent drop-ins and
ordered environment files to the preceding
[service/file identity observation](audited-main-readonly-identity.json).
The observed environment files use simple assignments; later files take
precedence. Manager environment supplied no `GOBY_` keys. Secret values remain
only in the protected remote evidence directory and were not exported into
verification processes.

The configured primary endpoint is `127.0.0.1:5432/goby_test`; the recovery
endpoint is `127.0.0.1:5432/goby_recovery_m5j`. Each uses its matching named
role. This identifies configured targets; it does not authenticate their stored
passwords. The working directory is `/var/lib/goby-test`, with service user and
group `goby`. Relevant paths are:

| Purpose | Configured path |
| --- | --- |
| Lifecycle | `/var/lib/goby-test/recovery-m5j` |
| Recovery operations | `/var/lib/goby-test/recovery-operations-m5j` |
| Backup store | `/var/lib/goby-test/backups-m5j` |
| Default master | `/var/lib/goby-test/application-key-vault/master.key` |
| Web assets | `/opt/goby-dev/admin` |
| Transcode cache | `/dev/shm/goby-transcodes-test` |
| Logs | `/var/log/goby-test` |

Captured store metadata establishes deployment
`f58d5e0c8ff49fca916499e666bffd9f`. The lifecycle marker and registry agree,
the baseline digest is the empty-content digest, no active manifest or
activation journal exists, and no generation is registered. This derives
`primary`, revision `0`, empty generation and `default` master selection.
The selected master has the expected 32-byte file metadata; its contents and
cryptographic relationship to application keys have not been checked.

The recovery-control current file matches the published CAS candidate at
revision `5`, with predecessor revision `4`. Its sole operation is a completed,
finished create. There is no transition; slot declarations are primary active
and recovery unclaimed. These are captured file declarations, not proof that
the recovery database is empty or safe to overwrite.

One generated, ready backup object remains. Its catalog token, operation
reference, identity, size and modification time agree with captured metadata.
The recorded archive is 177,366 bytes, schema `23`, 29 tables, created on
2026-09-10. No archive body or master contents were read. Its recorded verified
flag does not establish current digest verification or restorability, and this
historical object cannot supply the required fresh schema27 recovery point.

The first parser attempt failed because empty systemd command-array properties
were omitted. That failure remains recorded. A subsequent unit-object lookup
found the inactive unit unloaded. The bounded configuration selection uses the
captured simple environment profile; it does not infer empty startup hooks from
omitted properties. Full Go configuration validation and the complete startup
command chain remain open.

A later filesystem observation finds 520,519,680 available bytes at the backup
root. Source32 calls `space(0)` during `backupstore.Open`; with the captured
defaults this requires 536,870,912 minimum-free bytes plus 33,619,968 bytes of
metadata reserve, or 570,490,880 bytes. The observed deficit is 49,971,200 bytes.
Consequently that open-time capacity check would fail at this observation.
No store was opened, capacity policy changed or files deleted. Resolve capacity
explicitly before a startup decision and recheck it at admission; this sample
does not guarantee future free space.

## Actual PostgreSQL identity

One read-only connection to the `postgres` database used the fixed Unix socket
at `/var/run/postgresql`, port `5432`, as OS/database user `postgres`.
The live backend was bound to postmaster PID `893`, its executable, cgroup,
network namespace and owned listeners before the transaction was committed.

The cluster system identifier is `7683277964552005578`, server version
`170011`, with data at `/var/lib/postgresql/17/main` and configuration under
`/etc/postgresql/17/main`. Postmaster start time is
`2026-09-13T06:18:52.630333Z`. Both Goby main and the source55 control remained
inactive, and the captured OS identity was stable across the read.

| Database | Database OID | Owner role OID | Role inheritance |
| --- | --- | --- | --- |
| `goby_test` | 16385 | 16384 | Enabled |
| `goby_recovery_m5j` | 994944 | 994943 | Disabled |

Both roles can log in and lack superuser, create-database, create-role,
replication and bypass-RLS privileges. Neither has role configuration entries,
and no membership involving either role was found. Main retains its observed
PUBLIC CONNECT/TEMPORARY grants; the recovery database grants the recorded
database privileges only to its owner. No ACL or role was changed.

The existing deployment lock was matched to its complete fresh file identity,
acquired nonblockingly without creation or rewriting, and released. The
transaction committed, the frontend exited, and the backend's exit was
observed. This lock coordinates only participating operators; it does not
prove the absence of unrelated writers.

Two subsequent, separate read-only repeatable-read connections observed the
application databases through the same fixed socket. Each first checked the
catalog and the ordinary table definitions before reading any application
values. Only migration history and the `server_id` and
`goby.recovery.binding.v1` settings were selected. Both transactions committed,
both frontends and backends exited, and the deployment lock was released with
unchanged identity. No shared snapshot across the two databases is claimed.

Main has the exact ordered migration version/name prefix `1..27`, 35 ordinary
tables, five sequences and 103 indexes. Its public namespace OID is `2200`,
owned by `pg_database_owner` OID `6171`. Server ID is
`b051277b616acbdf528b9ff9589121cb`. The 98-byte recovery marker agrees with
deployment `f58d5e0c8ff49fca916499e666bffd9f`, primary slot and empty generation;
its raw SHA256 is
`561d55f90342e77f20c7cb69660aba17226656465bc86e266048a98ad83ff675`.
This corroborates the independently derived lifecycle selection.

The recovery database has the same public-namespace owner and OID, with no
non-system relation objects observed. Its migration and settings tables are
absent, so neither was queried for rows. This is a bounded catalog observation,
not a complete empty-database admission or authority to overwrite it. The
native restore target inspection and ownership contract remain required.

These queries do not compare the full trusted catalog, fingerprint business
tables, inspect sequence values or prove historical SQL execution digests.
The existing deployment lock also does not exclude non-cooperating DDL; this
identity observation is not a future migration freeze or preservation baseline.

## Remaining preparation

Static review of source32 (`b9bb7b1cf11e07e011a6e1726ddb9d53ef8e1fe9`)
narrows the next startup observations:

| Area | Required observation and resulting boundary |
| --- | --- |
| Migration | The observed ordered version/name prefix is exactly 1..27. Full trusted-catalog comparison remains open; a maximum version alone would not exclude missing migrations in source32 |
| Binding and identity | The observed marker and server ID are present and consistent. Bootstrap prerequisites still need review; `ServerID()` always performs an upsert, even when its value remains unchanged |
| Interrupted work | Identify queued/running scans and pending/running/stopping task runs with their child relationships. Startup can interrupt them and add completion activity |
| Scheduling | Check task definitions, enabled triggers, schedule revisions, due times, library IDs and the database clock. Initialization can change definitions, record missed occurrences and admit startup work before serving HTTP |
| Transcoding | Bind effective enablement, encoding jobs and the actual cache inventory. Startup can create cache control files, remove legal job directories and mark active encoding jobs interrupted |
| Retention | Bind effective retention and the count/age of expired activity rows. The first retention tick can delete pre-existing activity |

The settled create and absent transition do not trigger recovery replay under
the reviewed source32 branches. `settings.New` reads its existing singleton;
it does not initialize missing defaults. These source conclusions narrow the
expected-write review and do not replace fresh runtime or data observations.

1. Check the complete old-binary startup command, effective Go configuration,
   required startup writes, pending work, retention and available resources.
   Resolve the observed backup-store capacity deficit through an explicit
   space or main-policy decision. The required 512 MiB minimum free plus
   metadata reserve is not currently available; candidate overrides do not
   apply to main. Preserve existing evidence and application data.
2. Resolve the private recovery materials and application-key witness, then
   prepare the bounded native schema27 backup scope and two isolated restore
   proofs required by the main upgrade contract. Core client acceptance still
   blocks the main startup/backup route and promotion under the current plan.

No local verification, application HTTP request, service start, business SQL
write, migration or restore was performed in this increment. No product
code changed, so the accepted full product verification remains reusable.
