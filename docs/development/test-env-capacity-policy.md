# Test-environment cache and capacity policy

Use this runbook for selected maintenance and for an authorized task's closeout
on `test-env`. Routine cleanup of its confirmed disposable resources belongs to
that closeout. Reuse the existing remote owner and task records.

## Before starting a task

Record the effective Go executable, `GOCACHE`, `GOMODCACHE`, `GOTMPDIR` and `TMPDIR`.
Use the values actually passed to the worker, including explicitly selected
isolated environments. Do not silently replace an admitted environment.

Inspect current persistent availability and, when using memory-backed paths,
current memory availability. Historical free-space receipts are not reservations.
Plan headroom for the selected workload: cache growth, source/archive copies,
compiler scratch, media fixtures, raw logs, exports and resource closeout.
Size headroom for the current workload instead of reusing a historical threshold.

If unexpected growth invalidates the plan, stop admitting new work and reconcile
the cause with the owner. Preserve failed raw evidence. Repeated cache cleaning
under an ongoing workload is not a substitute for a sufficient capacity budget.

## Choose one cache mode

| Mode | Build cache | Module cache | Lifecycle |
| --- | --- | --- | --- |
| Ordinary run | Reuse the current shared `GOCACHE` | Reuse the current shared `GOMODCACHE` | Avoid per-task copies; maintain the shared cache separately |
| Explicit isolated run | Create an empty, budgeted private `GOCACHE` | Reuse shared modules unless the selected contract requires isolation | Reclaim private build cache after actual worker exit |

For ordinary root-operated runs, the current shared defaults are
`/root/.cache/go-build` and `/root/go/pkg/mod`. Reuse them unless the caller has
explicitly selected another cache environment; avoid introducing another shared
location with duplicate dependencies.

Do not copy a large build or module cache into each new task or source variant.
An explicitly required frozen dependency environment remains valid for its
selected scope; this default does not amend it or require rebuilding it.
Cache paths belong in the task record so later cleanup can identify their owner.

Memory-backed private build caches consume RAM. Bound them by the task's capacity
plan and use persistent storage when the selected workload requires it.
A private cache is disposable compiler output, not an archive of test evidence.

## Separate compiler scratch from media fixtures

`GOTMPDIR` is compiler scratch. Give it a task-owned path on storage appropriate
to the compiler budget, and reclaim its disposable contents after worker exit.
`TMPDIR` controls test temporary files and may carry media fixture requirements.
Tests depending on physical filesystem identity, native enumeration, ctime,
rename or other filesystem behavior must keep their selected fixture filesystem.
Do not move all fixtures to tmpfs merely because the compiler cache uses RAM.
Keep compiler scratch, media/data fixtures and retained evidence in distinct paths.

## Reclaim only confirmed inactive compiler output

First confirm actual owner completion and current liveness of relevant Go,
compiler, linker and test processes. An old PASS or closure receipt alone is not
a fresh liveness check. Do not clean a cache still referenced by an active worker.

Resolve the exact canonical cache path, reject unexpected symlinks or ownership,
and confirm the expected Go build-cache layout. An ambiguous directory stays.
Use the selected Go executable with that exact `GOCACHE` to run `go clean -cache`.
This is build-cache cleanup; do not substitute `go clean -modcache`.

For a private cache, reclaim its confirmed disposable contents; retaining an
empty cache root is acceptable. Reclaim task-owned compiler scratch after exit;
remove empty fixture
directories only after their own cleanup. Do not infer ownership from a name.

An entire old root is not a cache. Preserve source snapshots, independent
binaries, raw logs, receipts, databases, media and retained artifacts. Their
relocation or disposal requires its own selected retention and cleanup scope.
Do not prune shared Docker objects, other projects, module dependencies or old
environments as a side effect of compiler-cache maintenance.

## Keep the closeout small

Record cache mode and paths, owner completion, the exact cleanup target and
result, cache allocated bytes before/after, and filesystem availability before/after.
Keep persistent and memory observations separate. Concurrent activity can change
availability, so do not attribute its entire delta to the deleted cache.
Retain unknown causes and incomplete metadata explicitly instead of reconstructing
a successful receipt. Reuse existing task records; no new daemon or large receipt
framework is needed.

At each task's actual close, reclaim its inactive private build cache and scratch.
Keep source/data/evidence retention separate from that routine lifecycle.
Shared-cache maintenance uses a confirmed idle window and the same exact-path
checks; it is not an automatic prerequisite or a reason to replay old verification.

See the [October 5 capacity governance record](test-env-capacity-governance-20261005.md)
for this maintenance run's measured occupancy, cleanup and retained resources.
