# Current implementation and delivery status

This file is the concise current-state index. Historical handoff checkpoints and
immutable verification receipts retain their original results. Do not interpret
an old deployment paragraph elsewhere as a fresh process observation.

## Current plan

Execution resumed on 2026-09-13 after the planning review. The revised
[execution plan](../planning/current-execution-plan.md) prioritizes bounded exit
diagnosis, an audited candidate, core original-client regression and a new main
upgrade contract. Global NextUp and automatic-refresh research are parked until
new discriminating evidence justifies another bounded experiment. Their feature
gates and complete M2-M6 requirements remain open. Priority 1 is verified;
priority 2 has a running independent candidate with live admission pending. No new candidate
admission or main deployment is claimed by this status update.

The independently confirmed
[late restore cancellation race](restore-cancellation-race.md) is fixed. Both windows
were reproduced on unchanged code. The fix and a real persistence-conflict
regression passed targeted tests and the
[full remote race run](restore-cancellation-full-verification.json): 2,264 tests,
25 packages, zero failures/skips, and a Linux amd64 build. The seeded candidate
and client host remain retained. The
[candidate binary transition](audited-candidate-cancellation-transition.json)
completed with all 35 tables and owned state unchanged and PostgreSQL continuous.
The [backup-capacity configuration correction](audited-candidate-backup-capacity.md)
also completed with the existing failed history and all 35 tables preserved.
Live admission04 is running on that new runtime epoch.

The test host rebooted at `2026-09-13T06:18:45Z`. A fresh read-only
[baseline](resumed-delivery-reboot-baseline.json) found the source55 candidate
and source32 primary inactive, with their original installed bytes retained;
PostgreSQL 17 main was active. Earlier PIDs and transient units are historical.
No old service was started and no database state was re-attested by this check.

## Latest increment

The [process-exit diagnostic increment](exit-diagnostics-20260913.md) passed 60
targeted tests and a complete 2,262-test race run across all 25 packages, with
zero failures/skips and a Linux amd64 build. Known lease, panic and listener
failure classes now survive final shutdown wrappers. The original exit cause
remains unresolved. A fresh frontend build contains 57 assets, with all 64
inputs matching the earlier 41-test mocked-API verification. These artifacts
are selected for the [new candidate](audited-candidate-admission-plan.md);
they are installed only in that new isolated candidate. Its
[independent inspection](audited-candidate-runtime-inspection.json) verified
artifact/configuration, process/listener/lease identity, schema28, an empty
separate recovery database and three public health responses. One bootstrap
then created eight accounts and three libraries, copied fourteen approved media
files, and completed all three scans. Its
[read-only closeout](audited-candidate-seed-closeout.json) binds ten public
catalog entries to thirteen stored rows, preserves the actual English subtitle
code `en`, and proves both tool sessions revoked with their exact type/token
bindings. Original checker failures remain retained. Bootstrap and scans were
not repeated. Live admission and original-client acceptance remain pending.

The first live-admission attempt stopped in preflight with zero HTTP requests,
before login or backup creation. Its captured source snapshot exposed another
checker assumption about the ordinary user's empty stored default policy.
The corrected complete preflight passed a captured-data replay. Admission02
then performed five normal requests and two cleanup requests before its checker
incorrectly required an empty healthy-transcoding reason instead of `ready`.
Its administrator session was revoked; no backup or restore was admitted.
Both scopes remain consumed. After the product fix and candidate switch,
Admission03 passed authentication/storage checks but its first backup was
correctly refused: the default 8 GiB scratch reservation exceeded approximately
3.9 GB available disk space. Its
[failure closeout](audited-candidate-admission03-failure-closeout.json) verified
three new revoked sessions, two devices and eight expected audit rows, with all
earlier rows and the other 32 tables exact. No restore was admitted in that attempt.
The small fixture now has explicit 64 MiB object/256 MiB total limits. Admission04
has completed backup creation/download and ready-plan cancellation; its stability
window, session cleanup and final preservation reconciliation are still pending.
All failed records remain retained and the unchanged product verification is reused.

The [replacement main upgrade contract](audited-main-upgrade-plan.md) is a draft.
It explicitly separates schema28 forward restoration from source32/schema27
rollback and supports a main service that remains inactive after reboot.
No main upgrade or restart has been performed in this increment.

The resumed round closed after reference matrix07, two bounded original-client
discoveries, a separate Goby comparison contract and independently verified
source55 candidate recovery. Both clients completed cleanup but produced zero
physical `Shows/NextUp` requests, including two selected Suggestions windows.
All 198 protected roots and complete Goby state passed independent preservation.
See [client closeout](nextup-client-discovery-closeout.md),
[comparison contract](nextup-goby-comparison-contract.md) and
[candidate recovery](candidate-source55-restart-closeout.md).

Reference NextUp matrix07 completed 158 actual requests and passed independent
wire reconstruction plus runtime/identity/preservation closure. All ten global
responses were empty; `reference_global_positive_unresolved` remains the result.
The 186 protected roots and complete Goby state were preserved. See the
[matrix07 checkpoint](nextup-global-reference-matrix-07.md). The subsequent
client discoveries did not supply a positive selector rule or client acceptance.

The 2026-09-13 architecture audit identified 21 actionable code and integration
findings. R01-R21 are implemented and remote regression verification is complete;
see [the remediation record](audit-remediation-20260913.md) and
[the verification receipt](audit-remediation-20260913-verification.json).
The accepted evidence covers 2,255 top-level Go tests across all 25 packages with
race instrumentation: 24 complete passing package runs, plus the server package's
full run and one explicitly recorded fresh retry after a fixture timeout during
host memory pressure. The 41 mocked-API browser tests, frontend build and Linux
amd64 build passed. The remediation was published as `a623375`; deployment
remains pending.
Product fixes and regression checks do not establish a new reference matrix,
original-client acceptance, or production deployment.

## Last accepted deployment before the host reboot

The installed candidate is source55/schema28 and was independently verified
ready at `2026-09-13T06:01:44Z` as PID1814145, invocation
`3d9fccdb4f4d4f129ee02b33f6c73ce1`, after one start of the original binary.
The primary remains source32/schema27, PID1778525. Their binaries and complete
Goby state are unchanged; audit fixes remain undeployed. The
[recovery receipt](candidate-source55-restart-closeout.md) supersedes the earlier
failed-service observation without rewriting its history or identifying its
original exit cause.
The source55 product publication authority is
`16d75c38064008680fa60839c637efee2f12f2ae`.

TOOL05 proxy identity repair and diagnostic04 are complete. Preparation05's four
calibrations and 269 requests were completed and replayed. The reference NextUp
matrix07 has now run and its original bound matrix05 output is consumed. The
existing proxy remains preserved.
Consumed preparation, observer, diagnostic, and matrix scopes must not be rerun.
Client01/client02 and candidate recovery scopes are also consumed. The
[comparison contract](nextup-goby-comparison-contract.md) is parked, not the next
default action. Positive global/client evidence remains unmet for the relevant
feature claims; follow the revised execution plan before creating another run.

## Delivery gates

| Area | Implemented/verified boundary | Remaining release work |
| --- | --- | --- |
| Foundation | Linux Go service, PostgreSQL, native/compatibility identity; diagnostic/audit fixes verified and installed in an isolated candidate | Live candidate admission, migration and recovery safety |
| Catalog | Scanning, local metadata/artwork, stable identities, schema28 root binding, bounded storage observation ownership | Capacity profile, actual blocked-NAS measurements, reboot and filesystem matrix |
| Playback | Direct playback/state, selected subtitles/events, HLS and progressive software paths | Complete pinned original-client journey and broader formats/seeks |
| Administration | Users, metadata, sessions, keys, devices, tasks, settings, activity/logs, native backups; audit fixes verified | Selected policy/executor/provider extensions |
| NextUp/reference | 158-request matrix and bounded client discovery independently closed; global rule remains unresolved | Parked until new discriminating evidence; comparison only for a concrete product decision |
| Original-client refresh | Reference v4 and Goby v7 observations are negative | Automatic-refresh feature gate remains unmet; no Goby-specific failure is established |
| Main upgrade | Historical source55 candidate upgrade accepted; original main plan superseded | Audited candidate, core client regression and fresh migration/backup/rollback preparation; internal promotion remains partial |
| Hardware | Software baseline and explicit hardware configuration | Actual remote GPU decode, encode, and combined-path evidence |
| Packaging | Linux amd64/systemd with external frontend assets | arm64/OCI/embedded assets, supported profile, license and notices |
| M7 | Deferred | Explicit feature selection and independent acceptance |

## Verification policy

All compilation, formatting tools, tests, browser checks, media probes and runtime
verification use `ssh test-env`. Local verification requires explicit permission
in the current task. An unavailable test environment blocks verification; it
does not authorize a local fallback.

## Supported capacity boundaries

Missing-item reconciliation currently requires a complete proof within 4096
directory handles, 262144 entries, 131072 seen IDs and 64 MiB of observation
storage. Its SQL candidate/closure budget is separately 32768 rows and 32 MiB,
with per-row overhead that can reach the byte limit first. These budgets cover
the library, not only the deleted files. Exhaustion retains missing records and
reports a skipped cleanup while additions and updates may still complete.

Global NextUp query cost and concurrent scan/homepage latency have not been
accepted at a representative large-catalog scale. Neither a small API page size
nor the passing correctness suite establishes that capacity. Existing events
are not durable; supported clients must refetch state after reconnect.
