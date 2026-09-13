# Current implementation and delivery status

This file is the concise current-state index. Historical handoff checkpoints and
immutable verification receipts retain their original results. Do not interpret
an old deployment paragraph elsewhere as a fresh process observation.

## Current plan

Execution is paused for the 2026-09-13 planning review. The revised
[execution plan](../planning/current-execution-plan.md) prioritizes bounded exit
diagnosis, an audited candidate, core original-client regression and a new main
upgrade contract. Global NextUp and automatic-refresh research are parked until
new discriminating evidence justifies another bounded experiment. Their feature
gates and complete M2-M6 requirements remain open. This review changes no source
code, runtime or historical verification result.

## Latest increment

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
amd64 build passed. The containing Git commit records this verified increment
on main; deployment remains pending.
Product fixes and regression checks do not establish a new reference matrix,
original-client acceptance, or production deployment.

## Accepted deployment checkpoint

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
| Foundation | Linux Go service, PostgreSQL, native/compatibility identity; audit fixes verified but undeployed | Bounded exit diagnosis, audited candidate admission, migration and recovery safety |
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
