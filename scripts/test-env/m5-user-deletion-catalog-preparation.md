# Schema 29 catalog generation preparation

Status: static preparation only. Nothing in this document has been executed.
The active Programs full worker must finish and close before this separate job
starts. This is a catalog bootstrap, not an M5 test result or a product build.

## Source and output

The branch includes frontend integration `4796aa1`; the Go content and original
runtime-test source are retained from
`26e855490d5a7cfd93d770fe5b8c68926303d357`. After this adapter is reviewed and
committed, use the complete tracked Git archive of that actual new commit from
`codex/m5-user-deletion`. The draft deliberately leaves `SOURCE=None` until then.
Create fresh archive/manifest/preparation pins; do not consume a Programs source
archive or a historical execution input. No generated administrator assets are
needed. Keep the extracted source read-only and write the result outside it.
The committed archive contains the inert adapter template with `DRAFT_ONLY=True`
and unbound data. Bind a separately reviewed remote copy only after the archive
exists; the execution input pins that copy and the immutable source archive
independently. The bound executor is outside the source tree, avoiding an
archive/adapter self-hash cycle. Its copied worker entry must match the bound
executor bytes exactly. Do not commit a circular source-archive pin into the
source archive it describes.

The generator's compile dependency closure is `go.mod`, `go.sum`,
`scripts/test-env/generate-backuppg-catalog.go`, and all non-test Go files in
`internal/{backuppg,backupformat,database,storagebinding}`. Preserve all 29
`internal/database/migrations/*.sql` files and all six existing
`internal/backuppg/catalogs/schema-{23,24,25,26,27,28}-postgresql-17.json` files.
In particular, `database/root_binding_state.go` imports `storagebinding`.
The complete tracked archive avoids another manually incomplete source bundle;
it does not imply that its documentation and fixtures are compiled.

The generator is an explicit `//go:build ignore` source-file invocation. It
checks that the target has no non-system relations, calls `database.Migrate`,
then calls `backuppg.ExportCatalog`. Export reads the actual PG 17 catalog using
a repeatable-read, read-only transaction and validates migration history.
It does not call `loadCatalog`, so a pre-existing schema-29 JSON is unnecessary.
Never synthesize this output by editing the schema-28 baseline.

## Reuse and fixed tool references

Reuse the owned PostgreSQL and command lifecycle from:

- `/opt/goby-test/m5-final-regression-20260914/private/verify-m5-final-20260914.py`,
  43763 bytes, SHA256
  `3290bba192f1fd6e2b15d328a7c7ba2cbf3c8058ad7c43b7ef8e0eb2e5c64f7f`.
  Relevant methods are `Worker.command` (356), `start_postgres` (458),
  `process_identity` (451), and `cleanup` (606).
- Its companion `private/m5-final-regression-prepare.py`, 29086 bytes, SHA256
  `86e1e16de130c441dd16861d3ae7635eddaf18e352c7d87a0db87dcb98d35945`,
  supplies the pinned source review and infrastructure-loading primitives.
- Its `private/tool-pins.json`, 1915 bytes, SHA256
  `79e91ddd2c1150b9dc30f3889d378e407d1ac9d6899f56bb6b0d53fc2970ee41`.
  It pins Go 1.27.1 at `/opt/goby-toolchains/go1.27.1/bin/go` and the PG 17
  executables under `/usr/lib/postgresql/17/bin`, including `initdb`,
  `postgres`, `pg_ctl`, and `psql`. Recheck the actual selected files against
  the saved pins at admission. Other tools in that historical manifest are
  fingerprinted dependencies of the unchanged helper; this job does not run
  FFmpeg, FFprobe, GCC, gofmt, pg_dump, or pg_restore.
- The frozen modules at
  `/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356/modules`;
  sibling `module-cache-manifest.json` SHA256
  `4b72187093d2c87aed22857cd5464e33cbd6cb1349d0052474f9d27235c97f03`:
  2119 files, 61499652 bytes. Copy/hash them using the inherited inventory
  primitive; never recreate the removed `/dev/shm` cache links or download modules.

Retain `start_postgres` unchanged, including its three newly created low-privilege
roles/databases. Only `goby_backup_audit_source` receives the generator. The
other two empty databases are incidental to reuse of the reviewed lifecycle;
they are not backup/restore test targets. The whole cluster is disposable.
This avoids a new single-database implementation and its partial-start cleanup.
The helper uses a fresh data directory, an owned private bind mount, a private
network namespace, and namespace-local loopback port 15432. Its passwords exist
only for this new cluster and stay in private process memory/receipts.

Do not run `run-client-backup-tests.py --mode catalog` or `verify-backuppg.py`
unchanged: their saved catalog workflows use a pre-existing scratch cluster and
temporarily modify its roles/HBA, and their schema allowlists predate schema 29.
The [adapter draft](m5-user-deletion-catalog-prepare.py) derives the pinned M5
`supervisor` with fixed catalog mode/scope/unit, catalog resource limits and the
new worker entry point. It overrides `Worker.admission` and `Worker.verify`;
the inherited command, PG start and cleanup method bodies remain unchanged.
No package discovery, tests, or Goby build are part of this job.
After static review and explicit binding, its only outer invocation is
`python3 -I -B <scope>/private/m5-user-deletion-catalog-prepare.py --input <scope>/private/input.json --input-sha256 <sha256>`.
The default draft refuses execution before opening execution resources.

## Proposed bounded execution

Use a fresh exclusive scope such as
`/opt/goby-test/m5-user-deletion-catalog-<new-run-id>` and a new one-shot unit.
Bind its source, helper, tool, module, and current protection pins in a new input.
The protection purpose is `schema29-catalog-generation-with-disposable-postgres`;
old failed/running-state receipts are ancestry, not current authorization.

Use one owned, preallocated 2 GiB ext4 workspace volume for source, module copy,
compiler cache/tmp, new PG data, logs, and output. This is the smallest already
reviewed disk-volume capacity profile being reused, not a measured minimum for
the generator. No RAM filesystem and no second 512 MiB test-fixture image are
needed. Reuse the disk-volume create/identity/close primitives from the reviewed
`/opt/goby-test/livetv-programs-disk-final-20260916/private/verify-livetv-programs-final-r03.py`
(57717 bytes, SHA256
`c87ae09be2858ca29af5062edde5241b9a5d4f76f3ac50124848ff1e7f5a2560`);
retain preallocation, eager ext4 initialization, FD-origin ownership, visible
mount identification, and fail-closed deletion rules. Redirect their owned
paths to this new scope; do not call the Programs execution entry point.

Proposed bounds: `MemoryMax=1536M`, `MemorySwapMax=0`, `CPUQuota=150%`,
`TasksMax=256`, `LimitNOFILE=4096`, `PrivateNetwork=yes`, `PrivateMounts=yes`,
`PrivateTmp=yes`, `ProtectSystem=strict`, `ProtectHome=yes`,
`NoNewPrivileges=yes`, `Restart=no`, `KillMode=control-group`, `UMask=0077`.
Only the owned workspace is writable; extracted source is read-only.
The root worker drops to the existing `postgres` OS account for PG commands.
Check the actual unit properties, not merely the requested values.

Allow 300 seconds for the single `go run` command, 420 seconds for total payload
work, `RuntimeMaxSec=600`, and a 180-second unit stop budget. The inherited
supervisor has a 630-second wait and 210-second stop-command limit within its
900-second bound. The outer adapter separately allows 1260 seconds for source
preparation and supervision, followed by at most 180 seconds for materialization
and owned volume closure. These explicit outer allowances replace the earlier
proposal that counted only the 900-second supervisor bound.
The generator's existing 120-second context starts after compilation
and remains unchanged. Keep the inherited PG start timeout of 30 seconds and
pidfd stop wait of 30 seconds. A command failure is final for this input; do not
automatically retry against the partly migrated database.

At future admission require the full worker to be terminal, at least 3 GiB
`MemAvailable`, and root free space for the unallocated 2 GiB volume plus a
256 MiB retained-evidence allowance and 512 MiB reserve. Source preparation
already written on root is part of measured usage, not double-subtracted.
These are proposed catalog-only limits, not assertions about current capacity.
The generated catalog is limited to 4 MiB. Each other retained file is limited
to 128 MiB, and all materialized evidence together is limited to 256 MiB.
The inherited worker command checks impose 128 MiB for stdout and 16 MiB for
stderr; the unit additionally has a 128 MiB per-file `LimitFSIZE`. The owned
2 GiB volume supplies the aggregate workspace hard bound. On failure preserve its owned
backing file if identity or closure is uncertain and report that capacity as
still allocated. No new resource is allocated by this preparation document.

The command environment is an explicit dictionary, not inherited host settings:
`PATH=/opt/goby-toolchains/go1.27.1/bin:/usr/sbin:/usr/bin:/sbin:/bin`,
`LANG=C.UTF-8`, `LC_ALL=C.UTF-8`, owned `HOME`, `TMPDIR`, `GOTMPDIR`,
`GOCACHE`, and copied `GOMODCACHE`; `GOPROXY=off`, `GOSUMDB=off`,
`GOTOOLCHAIN=local`, `GOWORK=off`, `GOFLAGS=-mod=readonly`, `GOENV=off`,
`GOMEMLIMIT=512MiB`, `GOMAXPROCS=2`, `CGO_ENABLED=0`.
Its only `GOBY_*` entries are `GOBY_TEST_BACKUP_DISPOSABLE_DATABASES=1`
and the source URL returned by this invocation's `start_postgres`.
Do not put that URL in argv, a public summary, or the repository.

This is the payload draft, not a standalone admitted launcher:

```python
def generate_schema29_catalog(self):
    # Called only after catalog-specific source, unit, volume, and guard admission.
    self.report.update(singleRunFullSuite=False, ordinary_suite_passed=False,
                       build_executed=False, catalog_generation_only=True)
    self.start_postgres()
    output = self.scope / "schema-29-postgresql-17.json"
    require(not output.exists(), "catalog_output_already_exists")
    environment = {key: value for key, value in self.environment.items()
                   if not key.startswith("GOBY_")}
    environment.update(
        CGO_ENABLED="0",
        GOBY_TEST_BACKUP_DISPOSABLE_DATABASES="1",
        GOBY_TEST_BACKUP_SOURCE_DATABASE_URL=
            self.environment["GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"],
    )
    self.command("generate-schema29-catalog", [
        GO, "run", "-p=1", "scripts/test-env/generate-backuppg-catalog.go", output,
    ], timeout=300, environment=environment)
    # Read and pin the exclusive 0600 regular output before closure.
    # Apply the actual-output checks below; success is published only after closure.
```

The thin adapter must set the proposed deadlines/environment and use its own
catalog-only status. The inherited `singleRunFullSuite=True` initializer and
full-suite pass label must not leak into its receipt. Go compilation for this
generator is recorded explicitly; it is not an ordinary/embedded product build.
Any fail-closed placeholders are limited to the fresh scope/nonce/unit, source
archive/manifest pins, and fresh current protection descriptor. Resolve these
in one execution input after the active full job closes, without new platforms
or unrelated gates.

## Actual-output review, closure, and return to the branch

1. Read the output through an owned regular-file handle; require root ownership,
   mode 0600, no symlink, one link, length at most 4 MiB, stable identity during
   reading, and SHA256. Preserve the generated bytes exactly. Parse with duplicate
   JSON keys and nonfinite numbers rejected. Require `version=29`,
   `postgresql_major=17`, `catalog.Schema=""`, 29 contiguous migrations, 35 tables,
   and five sequences. Match every migration name/hash to the pinned source.
   Migration 29 must be `0029_user_deletion_activity.sql`, SHA256
   `cc2bc10bdd78866854559485b05c34018b4d6ea2ec9e25c880ff234609388564`.
2. Review the actual normalized `objects` fingerprint and schema-28-to-29 object
   difference. Only `activity_entries_action_check` and
   `activity_entries_action_resource_check` should gain `user.deleted`, with
   resource kind `user`. `catalog.Constraints` is the foreign-key inventory and
   should not change. This comparison reviews an already generated real artifact;
   it must never be used to manufacture or repair the artifact.
3. In `finally`, stop outstanding owned command process groups; use the inherited
   PG identity check and pidfd SIGINT/poll closure, then remove its private bind.
   Require `postmaster.pid` absent, source unchanged, and only the worker left
   in its cgroup. The outer owner then requires terminal unit/MainPID 0/empty
   cgroup before materialization and volume closure. If start outcome or identity
   is unknown, retain the failure and let the owned-unit closure contain descendants; do not signal an
   unproven PID or report `resourcesClosed=true`.
4. Before removing the workspace, materialize the catalog and small private
   receipts/logs to exclusive files under the owning scope, read them back and
   pin them. Then unmount the workspace, detach its loop, and delete only the
   exact FD-origin backing file, retaining the final volume-closure receipt.
   Retain source/tool/input pins, fresh-cluster identity, generator
   exit/status, actual catalog hash/counts, command and unit closure, volume
   closure, and unchanged protected metadata. Generated data and closure are
   separate facts. A catalog may exist after a failed run; that does not make
   the run accepted. PG data and raw diagnostic logs stay remote/private.
5. After independent review of the actual artifact and closure, transfer only
   the pinned catalog bytes and safe review summary to a temporary local staging
   path. Compare transferred byte length/SHA with the remote receipt before
   adding the exact bytes to
   `internal/backuppg/catalogs/schema-29-postgresql-17.json`. Preserve schema
   23–28 files and migrations 1–28. Do not run Go, SQL, tests, or build
   verification locally.
   Freeze a new source archive that includes the real catalog before the M5
   verification input is prepared; the generator's original source pin remains
   separate from this later verification source pin.

Catalog generation leaves all tests pending, including
`TestPublishedRecoveryCatalogsRetainHistoricalMigrationPrefixes`,
`TestPostgreSQLSchema29ArchivePreservesCommittedUserDeletion`,
`TestPostgreSQLSchema28ArchiveRejectsLaterUserDeletedAction`, and
`TestHTTPDeleteManagedUserStopsRealEncoderAndSocketsWithoutInterruptingOtherUser`.
The last test's real PIDFD/FFmpeg/WebSocket fixture is already formatted in the
selected branch but has not run. Generate the real catalog first, then run the
separately scoped M5 backend/API/browser acceptance against the later frozen
source; no earlier Programs or schema-28 result substitutes for those outcomes.
