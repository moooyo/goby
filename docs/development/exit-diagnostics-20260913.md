# Bounded process-exit diagnosis

Status: implementation, 60 targeted tests and a complete 2,262-test remote race
run passed; Linux build complete. Candidate admission and deployment remain
pending.
This increment follows priority 1 of the
[current execution plan](../planning/current-execution-plan.md).

## Finding and change

The retained [runtime incident](runtime-drift-20260913.md) reports unexpected
exits with `server.stopped` and `error_class=unclassified`. The old classifier
could inspect selected exported standard-error fields, but did not traverse the
standard `errors.Join` used by the final shutdown path. `generationError` also
discarded useful listener classifications, and recovered panics became ordinary
fixed-text errors. These source findings explain lost diagnostic detail; they
do not identify which condition caused the historical exits.

The new classifier traverses only known concrete standard wrappers and its own
classified-error type, with a 16-level and 64-node limit. It does not format raw
error text or call arbitrary `Is`, `As` or `Unwrap` hooks. Fixed classes identify
panic, unavailable/busy deployment leases, listener failures and address-in-use
errors while preserving safe sentinel identity for existing recovery checks.
Opaque third-party wrappers can still remain unclassified. This is bounded
diagnostic coverage, not a promise to explain every possible process failure.

The Serve worker records a safe class before cancelling its generation. This
keeps the coordinator's cancellation branch from losing that class when it wins
the select before the Serve result arrives. Existing cancellation, shutdown and
recovery decisions are retained; the change adds no retry or automatic restart.
A separate source review traced the legitimate pgx wrappers and the shutdown
sentinel consumers and found no concrete control-flow regression from the
conservative matcher. Runtime evidence remains the responsibility of the tests.

## Verification boundary

The [source freeze](exit-diagnostics-source-freeze.json) records the seven changed
files and remote formatting. The first formatting attempt found one missing
closing brace, corrected before the source freeze; its original source remains
in the new preparation scope. No product tests ran against that invalid input.

The [targeted verification](exit-diagnostics-targeted.json) used a fresh isolated
PostgreSQL environment and passed 60 tests with zero failures or skips across
both
`cmd/goby` and `internal/diagnostics` with race instrumentation. It includes
untrusted error methods, typed nil and incomparable values, depth/width/cycle
budgets, whitelist rejection, sentinel retention and final `server.stopped`
records. The real `g.Start()` regression acquires a deployment lease and blocks
at the actual cancel callback, checking both listener failure and panic before
the result channel can publish. A missing-database skip does not satisfy it.

The same final archive passed the [full verification](exit-diagnostics-full-verification.json):
2,262 tests across all 25 packages in one complete run, zero failures/skips and
Linux amd64 build success. The closeout independently checked command/log hashes,
actual Go result events, both real Start regression subcases, the binary and
changed source pins, and current cgroup/PostgreSQL cleanup. No old audit count
is presented as testing this change. Candidate admission remains separate.

The [frontend input check](audited-candidate-frontend-reuse.json) matched all 64
unchanged inputs and permits reuse of the existing 41 mocked-API tests. A
[fresh isolated build](audited-candidate-frontend-build.json) produced 57 asset
files. Its transient unit was collected after exit; retained systemd journal
start/success events prove completion, rather than default properties returned
for a missing unit. An initial read-only closeout assumption was corrected;
the actual asset build ran once.

## Host and remaining work

SSH initially timed out during banner exchange. After the user restored access,
a [fresh baseline](resumed-delivery-reboot-baseline.json) established a host
reboot at `2026-09-13T06:18:45Z`, inactive source55 and primary services, and the
same installed binaries. This does not establish the cause of the reboot or
re-attest historical database state. No old service or experiment was restarted.

The original application exit cause remains unresolved. Candidate stability,
core original-client playback, main migration/rollback and remaining M2-M6 gates
require their own evidence under the [candidate plan](audited-candidate-admission-plan.md).
