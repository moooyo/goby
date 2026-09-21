# Phase 3 consolidated build and regression runner

Status: source only; no execution or acceptance is established here. This runner
is released only after the entire phase's source and fixtures are integrated.
It runs on the isolated owned VM106, never on a developer workstation.

`media-analysis-phase3-regression.py --profile PRIVATE_JSON` performs the full
ordinary Go package regression, all new Phase 3 Python test sources, a fresh
administrator build, ordinary and embedded application builds, and the embedded
command package tests. A zero exit status is evidence for these operations only;
it does not accept either catalog-capacity tier or any fault/restart scenario.
Optional hardware/browser/mount selectors are not silently enabled. Every Go
parent skip remains explicit for final scope review.

The sole infrastructure owner creates the private PostgreSQL runtime and starts
this script in one exclusive systemd unit. All commands run serially within the
one heavy-worker slot. PostgreSQL has a separate cgroup. Seven fresh databases
are required: one ordinary integration database and an independent source/target
pair for each of `internal/backuppg`, `internal/recoverydb`, and
`internal/recovery`. No pair is reused across packages. The runner does not
start, stop or drop PostgreSQL; the infrastructure owner closes it only after
the runner and database connections are actually closed. All seven databases
and original evidence remain retained.

## Frozen inputs

The root-owned mode-0600 profile contains:

- `schema_version: 1`, `released: true`, `vmid: 106`, `machine_id`, `run_id`,
  `owner_id`, and the exact 40-character `source_revision`.
- `campaign_root`, containing existing `sources/` and `build-work/` directories.
- `source_directory`, the extracted immutable source beneath `sources/`.
- `source_manifest: {path, sha256}`. The manifest has `source_revision` and a
  sorted `files` array of `{path, bytes, sha256}`, with relative POSIX paths.
  Its complete inventory must match the extracted tree, without symlinks or
  special files.
- `runtime_context: {path, sha256}`, the actual setup result described below.
- `unit` and `cgroup`, binding this process as the unit's actual MainPID and its
  exact unified cgroup. No unrelated worker may share that cgroup.
- `budgets: {total_seconds, max_allocated_bytes, free_floor_bytes}`. Freeze these
  from actual admission. The total may be at most 12 hours; owned allocation
  may be at most 20 GiB and the free-space floor must be at least 2 GiB.

The runtime context has matching schema/release/guest/source fields,
`mode: "regression-only"`, `owner_file`, `owned_root`, and
`postgres: {unit, pid, start_ticks, cgroup_path, system_identifier}`. The owner
file binds the actual owner and source. The pinned live PostgreSQL PID/start and
cgroup are rechecked before commands; test fixtures independently verify their
actual database/migration behavior.

`tools` maps `go`, `node`, `npm_cli`, `ffmpeg`, `ffprobe`, `intro_fingerprint`,
`pg_dump`, `pg_restore`, and `psql` to root-owned immutable `{path, sha256}`
files. `environment` provides `GOBY_TEST_DATABASE_URL` and optionally the fixed
`GOBY_TEST_RECOVERY_PORT`. It contains no reusable backup pair or arbitrary loader
environment. `environment_by_package` maps the three complete module/package
names to their exclusive `GOBY_TEST_BACKUP_SOURCE_DATABASE_URL`,
`GOBY_TEST_BACKUP_TARGET_DATABASE_URL`, `GOBY_TEST_BACKUP_DISPOSABLE_DATABASES=1`,
and `GOBY_TEST_RECOVERY_PORT` values. Credentials are never printed.
The ordinary database/role names use `goby_phase3_`; all six backup
database/role names use `goby_backup_phase3_`, preserving the existing disposable
backup fixtures' name guard. Every URL is a nonempty loopback URI on port 55461
with no connection-target query overrides. Canonical database names, rather than
credential or IPv4/IPv6 spelling differences, establish pair independence.

## Execution and evidence

The runner creates a new `build-work/<run_id>` directory. It copies and verifies
the immutable source, uses private module/compiler/npm caches, and rechecks every
original source input before each command and at completion. No retry overwrites
a prior run. Raw output, per-command receipts and Go parent-result summaries stay
mode 0600 under `private/`; only sanitized stage status is printed.

Every command has a finite deadline and output cap. The runner samples allocated
space and remaining filesystem capacity. CPU/memory/task caps belong to the
frozen systemd profile and its independent external observer; the runner does
not substitute its disk observations for those resource measurements. Child
closure covers the exact owned unit cgroup, including descendant cgroups and
children that changed process groups. Failure cleanup uses identity-checked
pidfds only for actual members of that cgroup. Forced cleanup is retained as a
failed command, never relabeled as normal termination.

`regression-result.json` binds all stages, package inventory, output binaries,
the actual frontend directory and complete regular-file byte/hash inventory,
source revision, runner identity, disk observations and child closure. The
frontend inventory is rechecked after the builds and regressions. Deployment
copies only these bound artifacts into its independently permissioned service
directory; it does not make the root-private build workspace readable to Goby.
The report leaves
`capacity_accepted`, `fault_recovery_accepted`, and `postgres_closed` false.
The external controller must retain its independent unit/resource closure and
the later PostgreSQL closure before phase delivery. A campaign-level exception
retains `regression-failure.json` when initialization had completed. Review raw
logs privately and screen them before any public evidence export.
