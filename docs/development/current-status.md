# Current implementation and delivery status

This file is the concise current-state index. Historical handoff checkpoints and
immutable verification receipts retain their original results. Do not interpret
an old deployment paragraph elsewhere as a fresh process observation.

## Latest increment

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

The last independently attested candidate is source55/schema28; the primary is
source32/schema27. This is retained checkpoint evidence, not a current live
process assertion. The source55 product publication authority is
`16d75c38064008680fa60839c637efee2f12f2ae`.

TOOL05 proxy identity repair and diagnostic04 are complete. Preparation05's four
calibrations and 269 requests were completed and replayed. The reference NextUp
matrix has not run; its unused bound output and existing proxy remain preserved.
Consumed preparation, observer, diagnostic, and matrix scopes must not be rerun.
See [the exact resume boundary](nextup-live-identity-closeout-05.md#resume-boundary).

## Delivery gates

| Area | Implemented/verified boundary | Remaining release work |
| --- | --- | --- |
| Foundation | Linux Go service, PostgreSQL, native/compatibility identity | Preserve migration, upgrade and failure-recovery guarantees |
| Catalog | Scanning, local metadata/artwork, stable identities, schema28 root binding, bounded storage observation ownership | Capacity profile, actual blocked-NAS measurements, reboot and filesystem matrix |
| Playback | Direct playback/state, selected subtitles/events, HLS and progressive software paths | Complete pinned original-client journey and broader formats/seeks |
| Administration | Users, metadata, sessions, keys, devices, tasks, settings, activity/logs, native backups; audit fixes verified | Selected policy/executor/provider extensions |
| NextUp/reference | Preparation and bounded tools verified | Actual matrix, independent wire reconstruction, separate Goby comparison |
| Original-client refresh | Reference v4 and Goby v7 observations are negative | Positive gate remains unmet; no Goby-specific failure is established |
| Main upgrade | Candidate upgrade accepted | Positive-client gate and fresh independent upgrade preparation |
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
