# Audited candidate admission

Status: an isolated candidate is running, pending live admission under the
[current execution plan](../planning/current-execution-plan.md). No candidate
has been admitted by this document. Remote verification uses `ssh test-env`.

## Selected product and existing evidence

The selected product includes R01-R21 and diagnostic commit `a9c541a`.
The [diagnostic verification](exit-diagnostics-full-verification.json) passed
2,262 tests across 25 packages with race instrumentation and a Linux build.
The frozen source archive is
`74247d7584cb750645474deb2a4fc564f5944a32cd9875b2644209d65fbc89df`;
the installed candidate binary is
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
Reuse it unchanged for the new product verification; do not reopen its old runs.
It supports `cmd` packages, runs packages serially, reserves recoverydb for last,
retains failures and verifies its own PostgreSQL/process cleanup. Its private
network's port15432 is not the existing host workspace cluster.

## Completed product verification

The following sequence is complete for the frozen snapshot. Reuse the linked
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

Create a separate candidate installation, systemd unit, endpoint, database/role,
state directory, credentials and frontend assets after the product gate passes.
Choose actual names and a free port from fresh observations and record them in
the execution input. Do not overwrite source55, reuse its private runtime.env,
clone its recovery binding, or redirect an old reference/client proxy.

The [host reboot](resumed-delivery-reboot-baseline.json) invalidated previous
process identities. Existing source55 and primary files remain controls; their
inactive service states are the fresh baseline. Inspect the reference/client
hosting prerequisites before the later browser phase. Any necessary restoration
uses a fresh owned operation; it is not a rerun of a consumed experiment.

Bootstrap the candidate with its own administrator and ordinary accounts. Use
read-only copies of the approved movie/TV, MP3/FLAC and SRT/WebVTT fixture bytes
in new owned roots. Record semantic identity mappings and initial state through
the candidate's public APIs. Candidate-specific PostgreSQL inspection may verify
durability; no reference database or vendor source inspection is allowed.

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

## Current scope and bounded live admission

The new scope is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`, with PostgreSQL
on loopback 25498 and Goby on loopback 28498. The reserved browser gateway
origin is `http://127.0.0.1:28496`. Provisioning started each new unit once.
Its private manifest has SHA256
`022ca1e48aac6159750df72157dcddff3738e67962012f556825e26b0f0dfb09`.
It contains secrets and must not be copied into published evidence. Initial
source state has 28 migrations and zero users; the separate recovery database
is empty. These initial facts are not a durable claim about later state.

Complete independent process, listener, lease, database, recovery and isolation
inspection before bootstrap. Then perform one owned bootstrap/seed operation:
one administrator, six ordinary scenario accounts, one ordinary control account
with no library access, and three libraries using verified copies of approved
fixtures. Save actual catalog mappings and private credential descriptors.
Intent and uncertain write outcomes must survive a controller failure. Only
the controller's own confirmed credentials are revoked at its closeout.

Freeze a separate admission input after seeding. Use at most 120 HTTP requests
and 15 minutes, including ten reserved cleanup requests. Observe one continuous
ten-minute stability window with eleven health/readiness samples, fixed process
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
