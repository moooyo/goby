# Audited candidate admission

Status: the isolated candidate is seeded, and live admission is paused under the
[current execution plan](../planning/current-execution-plan.md). The cancellation
fix passed full verification and its single binary transition closed. Admission03
then failed at backup scratch capacity and is closed, along with admission01/02.
One explicit environment revision must close before a new admission scope runs.
No candidate has been admitted. Remote verification uses `ssh test-env`.

## Selected product and existing evidence

The selected admission target adds the ready-plan cancellation fix to R01-R21
and diagnostic commit `a9c541a`. Its frozen archive is
`b7120b6f323ace203fe7b49c56cd669ce5b9ff3ef8f3ceddb4359c2951aa658b`.
Its [full race verification and build](restore-cancellation-full-verification.json)
passed 2,264 tests across 25 packages. The
[closed binary transition](audited-candidate-cancellation-transition.json) installed
`477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f`.
The upcoming capacity-profile revision preserves that binary, full report and
source manifest; it does not require a new Go verification run.

The earlier [diagnostic verification](exit-diagnostics-full-verification.json) passed
2,262 tests across 25 packages with race instrumentation and a Linux build.
Its source archive was
`74247d7584cb750645474deb2a4fc564f5944a32cd9875b2644209d65fbc89df`;
the initially installed candidate binary was
`a9b25b6b3e9f04b528ca77cd0a0dd548ae4c2715c06a1a56a6def23c6e00e2d7`.
Schema remains 28. The matching frontend has 57 freshly built assets;
its 64 inputs match the earlier 41-test mocked-API verification.

The [audit receipt](audit-remediation-20260913-verification.json) covers 2,255
Go tests through composed package evidence, 41 mocked-API frontend tests and a
frontend build. Reuse unchanged frontend results after matching their 64 source
and configuration files and the actual built assets. Do not label those mocked
tests as original-client or live candidate acceptance.

The existing isolated runner at
`/opt/goby-test/audit-fixes-upload-bd962cdf0722/verify-isolated.py` can create fresh
private network/mount/PostgreSQL scopes for `pure`, `target` and `full` modes.
Its exact bytes are bound by the [reboot baseline](resumed-delivery-reboot-baseline.json).
The completed cancellation-fix verification reused it unchanged in a fresh scope;
do not reopen its old runs.
It supports `cmd` packages, runs packages serially, reserves recoverydb for last,
retains failures and verifies its own PostgreSQL/process cleanup. Its private
network's port15432 is not the existing host workspace cluster.

## Completed diagnostic product verification

The following sequence is complete for the earlier diagnostic snapshot. Reuse the linked
receipts; these steps are not a request to repeat the full suite or build.

1. Transfer the tracked source snapshot plus the exact diagnostic changes into
   a new preparation directory. Format changed Go files remotely, return those
   bytes to the workspace, then freeze a new archive and manifest.
2. Run the affected `cmd/goby` and `internal/diagnostics` regressions with race
   instrumentation, including hostile error methods, wrapper budgets and the
   generation cancellation path. A correction requires its own targeted rerun.
3. For the final frozen product, run the full package suite and Linux amd64 build
   once in a fresh isolated scope. Require real package coverage and explicit
   failures/skips rather than inferring coverage from a zero exit code alone.
4. Reconcile source, build, cleanup and reused frontend inputs into one concise
   product receipt. Keep the historical audit results and failures unchanged.

The shared test host has about 12 GiB RAM. The reused runner caps its unit at
3 GiB and 150 percent CPU with serial package execution. Run one heavy unit at a
time. Check current disk headroom before dispatch; retained evidence is not a
disposable cache to delete for space. Stop and retain the responsible scope if
the resource or ownership checks fail.

## Candidate ownership and admission

The provisioned candidate has its own installation, systemd unit, endpoint,
database/role, state directory, credentials and frontend assets. Its names and
ports are recorded in the original provision manifest. Do not overwrite source55,
reuse its private runtime.env, clone its recovery binding, or redirect an old
reference/client proxy.

The [host reboot](resumed-delivery-reboot-baseline.json) invalidated previous
process identities. Existing source55 and primary files remain controls; their
inactive service states are the fresh baseline. Inspect the reference/client
hosting prerequisites before the later browser phase. Any necessary restoration
uses a fresh owned operation; it is not a rerun of a consumed experiment.

Bootstrap, account/library creation, fixture copying and the three scans are
complete and must not be repeated. Reuse the closed seed's actual semantic
mappings and private credential descriptors. Candidate-specific PostgreSQL
inspection may verify durability; no reference database or vendor source
inspection is allowed.

Require a frozen source/binary/asset/schema/process identity; healthy and ready
responses; correct startup, lease and recovery ownership; relevant live checks
for changed authorization, storage and backup paths; and explicit owned cleanup.
Before execution, define the bounded stability workload/window and record
unexpected exits, resource pressure and recovery results. Preserved historical
state is cited as history unless freshly measured; do not claim all old evidence
roots or databases were reverified.

Admission releases this candidate for the existing core client journeys. It
does not establish original-client playback, NextUp, automatic refresh, a main
upgrade or M2-M6 completion. The main upgrade still requires a fresh contract and
independent backup/restore/rollback rehearsal after the core client gate.

The one-time seed is now [closed](audited-candidate-seed-closeout.json): eight
accounts, three libraries, three completed scans and fourteen read-only copied
files. Its ten public catalog entries correspond to thirteen stored rows because
the database also contains the three library collection roots. Both seed-tool
sessions are revoked. The original seed and first reconciliation failures remain
retained; subsequent closure performed no business writes or HTTP requests.

## Current scope and bounded live admission

The provisioned scope is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`, with PostgreSQL
on loopback 25498 and Goby on loopback 28498. The reserved browser gateway
origin is `http://127.0.0.1:28496`. Provisioning started each new unit once.
Its private manifest has SHA256
`022ca1e48aac6159750df72157dcddff3738e67962012f556825e26b0f0dfb09`.
It contains secrets and must not be copied into published evidence. At initial
provisioning the source had 28 migrations and zero users, and the separate
recovery database was empty. These are historical facts, not the current seeded
state or the identity of a later runtime epoch.

Independent runtime inspection and the one-time seed are complete. Admission01
stopped during preflight with zero HTTP requests. Admission02 stopped at its
overview assertion and revoked its one new administrator session, with same-token
401 proof. Neither attempt created a backup or restore plan; both failure scopes
remain closed.

The b712 binary transition preserved seeded data and PostgreSQL continuity.
Its closed epoch
records the current full report, source manifest, installed binary, processes and
lease. Admission03 completed authentication and storage checks, then its first
backup failed with `capacity_exceeded`: the default 8 GiB scratch reservation
exceeded the available filesystem space. Its
[failure closeout](audited-candidate-admission03-failure-closeout.json) confirms
36 normal and six cleanup requests, all three new sessions revoked, no restore,
and exact preservation of previous rows and the other 32 tables. Preserve the
current six revoked sessions, three devices and the failed zero-byte generated
object/create operation. The recovery database remains empty; do not resume 03.

The [small-fixture capacity correction](audited-candidate-backup-capacity.md)
adds `GOBY_BACKUP_MAX_OBJECT_BYTES=67108864` and
`GOBY_BACKUP_MAX_TOTAL_BYTES=268435456`, retaining
`GOBY_BACKUP_MIN_FREE_BYTES=67108864`. One atomic environment replacement and
candidate restart must produce an `environment_revision` epoch linked to its
previous binary epoch. Current source/binary, PostgreSQL and owned data remain
unchanged. The existing backup status request must confirm effective decimal
limits `67108864` and `268435456`; no extra HTTP request or budget is added.

Admission input version 2 selects `runtimeEpoch`, `seedRuntimeBinding`,
`runtimeHelper` and the matching `compiledCatalog`, together with its own helper,
run ID, output and budgets. It derives current source authority from the epoch.
The original seed, its executor and its business mappings remain separate
provenance; do not rewrite old seed source/process fields to describe the new
runtime. Transition validation retains the admission02 helper as its historical
pure validator, while the admission v2 executable has a separate source pin.

Freeze the next v2 admission input only after the environment epoch closes. Use at most 120 HTTP
requests and 15 minutes: 110 normal requests within 720 seconds and ten cleanup
requests within the remaining 180 seconds. Observe one continuous
ten-minute stability window with eleven health/readiness pairs, fixed process
and lease identity, and start/end resource counters. Run the following work
serially inside that window; polling is bounded by the same request/time limit.

1. Verify native administrator login, cookie/CSRF separation, ordinary-token
   rejection at native backup routes, and rejection of conflicting credential
   carriers. Confirm a supported matching query carrier still works.
2. Verify the seed's three completed scans and exact item/media mappings.
   Compare visible libraries and item access for the ordinary scenario actor
   and the denied control actor. Preserve their initial durable playback state.
3. Create one encrypted backup with a new request ID and retained private
   passphrase. Require completed operation, ready object, complete bounded
   download and matching size/SHA256. Plan restoration into the new inactive
   database, require a ready plan, then cancel that exact plan and verify its
   terminal state. Cancellation retains the staged inactive database and its
   ownership marker; require `Rollback.MustReplace=true`, no available rollback,
   and an unchanged active generation. Do not apply or roll back here.
4. Close the exact controller sessions and compare owned source data, fixtures,
   process identity, lease, operation state and resource counters. Retain the
   encrypted backup, passphrase, operation/audit history and accounts as declared
   candidate-owned state, together with the cancelled plan's inactive stage.
   A later replacement must explicitly bind this stage and use
   `ReplaceRollback=true`; it must not assume the target is still empty.
   Preserve failed or uncertain operations for review.

Do not infer candidate admission from an HTTP 202, readiness alone, or the
existing test suite. Unexpected exit, readiness failure, lost lease, OOM or
memory-limit failures, unexplained data changes or unfinished cleanup fail this admission.
The window is a bounded integration check, not an availability guarantee.
Real media advancement, seek/resume durability and user isolation under playback
remain priority 3. Actual restore application and rollback remain priority 4.

## Original-client hosting preparation

The approved 4.9.5.0 package is extracted into a new persistent scope with a
new program-data directory and isolated service. The first preparation started
the service once but its IPv4-only listener checker missed the actual IPv6
wildcard socket; it sent zero HTTP requests. The
[failed preparation](audited-original-client-hosting-preparation.json) is retained.
The gateway's corrected listener check passed
[11 remote cases](audited-client-gateway-listener-verification.json), including
rejection of a competing IPv4 listener. A separate
[read-only reconciliation](audited-original-client-hosting-reconciliation.json)
made one successful public request against the original invocation, with zero
new starts, and produced the [hosting binding](audited-original-client-hosting.json).
The package, executable and launcher were unchanged. This establishes a host
for subsequent `/web` delivery; it does not establish any browser journey.
