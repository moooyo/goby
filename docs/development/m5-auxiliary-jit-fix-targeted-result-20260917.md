# M5 auxiliary-query JIT correction: targeted verification

The single `7eee542f28d2` targeted run on source `9e70e4b` passed eight top-level
tests and sixteen children, each once, with zero failures or skips. Go reported
3.418 seconds; original SSH session `20715` exited 0. Independent saved-result
and resource review passed. Exact pins are in the [checkpoint](m5-auxiliary-jit-fix-targeted-result-20260917.json).

The three-file change disables JIT only around the auxiliary snapshot query and
restores the incoming effective setting. The complete SQL and five-second
proof budget are unchanged. Both original reconciliation tests and both music
children passed. Other coverage includes JIT on/off with commit/rollback,
restoration after bounded resync and read failures, and continued real `ownedTx`
use after ordinary caller cancellation.

Default cached-statement and existing generic-statement checks preserve full
results and statement identity/counters. The generic statement was created and
executed with `jit=on`, then reused with its generic counter advancing from one
to two. These checks do not directly observe JIT code generation. The recorded
reconciliation test durations are whole-test times, not proof-interval measurements.

The owned PostgreSQL, unit/cgroup, three volumes and lock closed. All 64 adapter
and 23 worker commands closed with protected state and source exact. The original
failed full and diagnostic attempts retain their outcomes.

The separate full attempt `0add3118b99f` is still running under original SSH session `43043`
at this checkpoint. Full regression and build outcomes remain unknown; this
targeted result does not supply them. The documentation update only read saved
JSON, with no tests, probes, SQL, service actions or large-archive reads.
