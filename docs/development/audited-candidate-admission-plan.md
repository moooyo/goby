# Audited candidate admission

Status: preparation in progress under the
[current execution plan](../planning/current-execution-plan.md). No candidate
has been admitted by this document. Remote verification uses `ssh test-env`.

## Selected product and existing evidence

Use product commit `a623375`, including R01-R21, plus the bounded exit-diagnostic
fix once remotely verified. A new build is required for changed backend bytes;
the previous audit binary is not a build of the diagnostic fix. Schema remains
28 unless source review and the frozen manifest establish an explicit change.

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

## Verification sequence

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
