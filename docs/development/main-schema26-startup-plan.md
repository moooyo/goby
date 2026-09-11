# Primary schema26 startup preservation plan

Status: **historical plan; primary schema26 deployment completed**. The
[separate completion](m3e-main-schema26-completed.json) passed its 13-call native
smoke using a new owned session and performed zero service writes, migrations
or restores. The first
actual attempt stopped source18/schema25 and failed during baseline column-ACL
capture. The later [tool03 repair](m3e-main-schema26-baseline-repair-failed-01.json)
completed a fresh same-snapshot baseline and dump, then failed before rehearsal
because `observe_rehearsal` queried absent PostgreSQL17 `pg_authid.rolconfig`.
No main migration ran in those two attempts. The main then remained schema25 with 15 activity rows; its
post-failure observation found zero other database connections and zero
rehearsal databases or roles. These counts do not describe the `sessions` table.
The subsequent [tool04 attempt](m3e-main-schema26-start-verification-failed-01.json)
completed rehearsal and cleanup, main schema26 migration, installation and
start. Primary PID688833/start ticks5620918 is active/running with source28. The operator retained
a failed `start-requested` result after its effective-identity assertion, before
smoke. Later UID995/GID986 match the fixed expected identity; the cause is not
established. Subsequent independent read-only service, installed-state and
private-preservation checks passed without changing tool04. All three failed
trees remain preserved. The new owned-session smoke and formal post-start
completion subsequently passed, ending with logout204 and the paired session401.
The process remained PID688833/start ticks5620918. Activity increased from 15
to 17 through legitimate login/logout history; post-start whole-table equality
is not claimed. No further migration or restart belongs to this completed operation.

The retained startup-plan deadline was `2026-09-11T18:56:44.739406Z`. Its validity
and 900-second startup reserve governed the executed PostgreSQL-time gates.
This historical document does not extend that deadline or authorize a replay.

## Retained preflight finding

The [first read-only preflight](m3e-main-schema26-preflight-failed-01.json)
passed product, tool and release-input checks, then stopped at the historical
operator's one-day activity-age threshold. At the retained observation the
primary held 15 activity rows, six older than one day and none older than 30
days. All five runnable-trigger or unfinished-work counts were zero.

The earliest activity timestamp was `2026-09-10T10:26:41.150555Z`. With the
actual 30-day policy, its natural expiry boundary is
`2026-10-10T10:26:41.150555Z`. The comparison is strict: records are eligible
when `created_at < clock_timestamp() - retention`.

At the retained preflight, the running process, systemd manager, unit and both fixed environment files
had no `GOBY_ACTIVITY_RETENTION_DAYS` assignment. PassEnvironment and
UnsetEnvironment also did not name that key. The following source files are
byte-identical between the installed source18 and proposed source28:

- `internal/config/observability.go`: default retention is 30 days.
- `internal/server/activity_retention.go`: the first prune tick is after one
  minute, followed by one-minute ticks.
- `internal/activity/store.go`: PostgreSQL time and bounded 1,000-row batches
  determine deletion eligibility.

The upgrade must preserve the actual policy and existing history. It must not
delete old activity rows, change retention configuration, or rely on finishing
before the first timer tick.

## Preserved startup checks

A private, explicitly hashed startup-plan document binds the primary database
and role OIDs, process lifetime, published deployment receipt, unit/drop-in
bytes, both environment files, source/full-report/binary proofs, retention
implementation and effective 30-day policy. Read-only configuration evidence
must include all seven possible override locations.

The plan's absolute PostgreSQL-clock deadline must be no more than six hours
after its observation. Every relevant state check and mutation intent must
recheck the plan, fixed configuration, current database time, all five active
work counts and zero activity rows eligible at any time before the deadline.
Immediately before service start and smoke login, at least 900 seconds must
remain. An expired or changed plan does not authorize continued deployment.

The historical one-day count remains evidence. It is not substituted for the
explicitly pinned effective retention policy. The old published operator is
not edited or patched; the new operator owns these additional startup checks.

## Data and failure boundaries

The independent helper retains the existing migration-transaction checks:
all old 30 tables, columns, rows, relation and index OIDs, raw ACLs, sequence
state and deployment binding must remain unchanged before commit. Only the
three Theme tables and their declared objects may be added. The backup is
restored only into a freshly receipted rehearsal database; the primary is
never restored.

The source18 service was stopped after durable intent and exact process
checks in the retained actual attempt. Failure or uncertain commit results retain evidence and require
forward repair; no automatic replay, rollback, old-binary restart or backend
termination is authorized by this plan.

If a new native smoke-test cookie has already been issued, the narrowly paired
logout cleanup still applies even when subsequent plan checks or local evidence
publication fail. That cleanup does not authorize further deployment steps.

Tool03's helper build, 26 memory guards and repair preflight passed before its
successful backup and retained rehearsal-query failure. Tool04 bound its
new forward root and completed rehearsal, migration, installation and start;
its retained post-start assertion failure now has an independent read-only
verification pass and a separate successful owned-session smoke/finalization
against the unchanged running process. The
[tool04 build and guards](m3e-main-schema26-tool04-verification.json),
[helper regression](m3e-main-schema26-helper-regression-02.json),
[catalog regression](m3e-main-schema26-catalog-regression-01.json) and
[completion receipt](m3e-main-schema26-completed.json) keep those evidence
boundaries explicit. The primary deployment gate is closed. Full
M3/M4/M5/M6 completion and the separate dual-user client workflow are not
claimed here.
