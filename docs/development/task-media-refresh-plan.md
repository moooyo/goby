# Fixed native media-refresh task

Status: **this fixed increment is accepted; focused/browser and final ordinary
regression evidence and resource closure are complete**.
The [first run](m5-refresh-first-verification.json) passed 63 task tests and 26
library tests, with one library-test failure before force probing; server and
browser plans did not execute. The test now reads its persisted JSONB metadata
baseline before checking manual values and preservation. All assertions remain;
the [corrected run](m5-refresh-corrected-verification.json) passed all 109 ordinary
focused tests, including this case, but failed its real browser scenario during
the interval portion. Its failed result and closed resources are independently
reviewed. The browser driver now uses canonical CSRF headers, and the fresh case
asserts the existing UTC default without Escape and preserves primary failure
stages. Only the browser test inputs changed. The subsequent [fresh browser
verification](task-media-refresh-verification.json) passed all eight checks,
three real runs, six real probes, two cancellation gates and exact logout
204/same-cookie 401, with no emergency revocation. Independent review confirms
the source bridge to the 109 ordinary tests and resource closure. The
[final intended-source ordinary regression](m5-final-regression-verification.json)
then passed all 25 packages from the beginning, with 2,295 top-level passes,
zero failures and one explicit mount-profile skip, plus the Linux amd64 build.
Independent result and closure reviews passed. Its first zero-dispatch loop
metadata rejection and first saved-evidence reader rejection remain preserved;
neither required another product-test run. The paused mount profile and two
helpers that return without their opt-in remain unexecuted. This result does
not complete M5 as a whole or admit core-client replay or main promotion.
This M5 increment adds the fixed `library.refresh_media` task,
named `Refresh media details`, using the existing scheduler, scanner and native
administrator UI. It does not expand M7, providers, a consumer player or Emby
aliases. Schema remains 28; public methods, native routes and DTOs are unchanged.

The earlier [preflight resource incident](m5-refresh-preflight-incident.json) stopped the
input preflight before any test runtime. Disk exhaustion interrupted both
retained candidate databases and ended both applications' leases. The audited
cache clean restored capacity. Read-only database/lifecycle checks and the
[unchanged application recovery](candidate-disk-full-recovery.json) then passed
independent review. The new M5 input binds the replacement runtime identities;
its preparation passed before the first worker was dispatched. That failed
worker and its resources are closed; the corrected source uses a new scope.
The original source archive, planning amendment and failed inputs remain intact.

## Contract

- `library.scan` and compatibility `RefreshLibrary` remain normal cached scans.
  The new definition has an empty internal Emby key and is absent from every
  Emby task list and by-ID read/start/stop/trigger operation. Store authorization
  also rejects its compatibility-audience mutation.
- Registration preserves old IDs, enabled states, revisions and history. New
  definitions create neither runs nor schedules. Existing normal request
  fingerprint bytes remain compatible; new fingerprints bind the actual key.
- Each admitted run snapshots its `task_key` and current libraries. That key
  fixes `ForceProbe`; callers still send only the existing optional `RequestId`.
  Child/job association checks include `job.force_probe` at admission, reuse,
  stop and recovery. Unknown keys and inconsistent modes fail closed.
- Both definitions share the existing bounded scan queue. An independent scan
  makes a child wait; no adoption or cancellation of another owner's job is
  allowed. Completed work and all run/request/occurrence history stay retained.
- Each task accepts at most 32 triggers. Startup accounts for at most 64 across
  the two definitions; timed dispatch makes progress across both, even with
  a one-item batch. No automatic schedule is installed by this increment.
- Successful forced probing also rebuilds effective entity associations for
  existing media, including themes and extras. Ordinary cached scans and failed
  probes retain their existing behavior; metadata overrides, locks and UserData
  must remain preserved.

## Bounded acceptance sequence

1. Bind the reviewed documentation and helper inputs to the frozen production/test
   source. Preserve the earlier source archive and preparation receipt. Review native-only exposure, exact
   normal fingerprint compatibility, preserved registry state, all 64 startup
   rules, cross-definition due ordering and child/job mode guards. Run focused
   task/library/server regressions on `ssh test-env`, including mismatches,
   foreign active scans, cancellation, maximum runtime and restart recovery.
   Go/API checks must also prove RequestId replay returns the original durable
   run, concurrent admissions coalesce per definition, and cancellation never
   adopts or stops an independent scan. These are separate from browser evidence.
2. In a new isolated fixture, establish a small real-media catalog with current
   probe/source versions, manual metadata and UserData. Remove exactly one owned
   Genre association from `item_entities`. An ordinary scan must use the cache
   and retain that gap; successful forced refresh must perform real probing and
   restore the exact association. The focused failure case must retain its
   previous state. Check media/NFO bytes, source identity and all declared
   metadata/UserData fields, including UserData timestamps and `xmin`.
3. Use the actual administrator Tasks page to discover the new card, start a
   run and inspect its children/jobs and final counters. This browser scenario
   creates new RequestIds; it does not claim to exercise retry or replay.
   Start a separately bounded active run and actually cancel it; a conditional
   skip because it already finished does not pass cancellation. Confirm both
   owned children enter active work and then reach their cancelled state.
4. Preview and save one explicit 30-second interval rule, observe its scheduled run, then
   remove the rule with the current revision. Choose timing and workload before
   execution so exactly one scheduled run and three total task runs are admitted:
   manual completion, manual cancellation and scheduled completion. Preserve the retired
   rule and occurrence history; do not substitute an empty preview for firing.
5. With the same owned instance and current authority confirmed, remove owned
   rules first on failure, then close owned active runs and newly
   issued credentials with terminal/401 checks. Reconcile the declared database
   deltas, retained histories, source files and resources. Preserve any failed
   receipt; do not repeat business actions merely to obtain a passing report.

The existing `verify-tasks.py` / `scheduled-tasks.spec.ts` workflow and
`verify-media-refresh.py` / `media-refresh.spec.ts` real-probe assertions are the
reuse points. Their old fixed database paths, 27-table snapshot, single-task
assumptions and fixture inputs are not valid for a new run without explicit
adaptation. Reuse their bounded primitives and existing runner; do not build a
new general framework or run a consumed historical operator unchanged.

## Resources and completion gate

SSH was reachable at 2026-09-14T12:54:07Z; a read-only check observed 800,792,576
free root bytes and no active M5 refresh verification unit. This is not a
reservation. Before any execution, freeze a fresh disk/RAM/process budget and
an owned cleanup boundary. Use an independently budgeted RAM compilation area
and small ext4 media fixtures; do not consume the paused mount experiment's RAM,
modify main/candidate state or borrow old client actors.

This is a production change: focused checks precede one final verification of
the intended frozen product snapshot before changed-version acceptance. Old
M5f and media-refresh receipts retain their original scope and are not added
to new test counts. Implementation, browser acceptance, independent evidence
review and resource closure must all be recorded before this increment passes.
No scope, output receipt, passing count or deployment is assumed in advance.

Those gates are now complete for this increment. The final ordinary source
archive is `f5b70c003eb43cfa0a58fb2eba88a13b7137f3c6c3f1da99307c821e80e336c5`;
all non-documentation members match the accepted browser source. The ordinary
binary is separate from an embedded release build. Test workers and PostgreSQL
closed, all four private archives passed readback, and ext4, its loop binding
and the independent RAM volume closed. Original failures, source and private
evidence remain retained. The current execution plan advances to an internal
amd64 embedded systemd package under its own fresh installation scope.
