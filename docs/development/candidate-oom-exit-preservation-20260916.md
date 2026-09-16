# Candidate preservation after the September 16 application exits

The two candidate source databases and their recovery databases exactly match their latest accepted September 15 **after-restart** snapshots. Both applications remain `failed` with `MainPID=0`; this checkpoint does not establish application recovery or a new runtime binding.

The execution scope is `/opt/goby-test/candidate-oom-exit-recovery-20260916/stage-one-r02`. Exact source, input, result, snapshot, dispatch, and baseline pins are recorded in [the checkpoint metadata](candidate-oom-exit-preservation-20260916.json).

| Candidate | Database role | Rows before / after | Tables | Sequences |
|---|---|---:|---:|---:|
| A | source | 230 / 230 | 35 | 5 |
| A | recovery | 147 / 147 | 35 | 5 |
| B | source | 141 / 141 | 35 | 5 |
| B | recovery | 131 / 131 | 35 | 5 |

Every selected row field and sequence fact matches; only `capturedAt` is excluded. All four table and sequence difference lists are empty. No earlier incident snapshot was substituted for these baselines.

One reader invocation completed 10 read-only SQL sessions, within the 12-session limit. Its recorded observation time was 661 ms against a 180-second observation budget. The dispatch exited zero. All 36 reader commands and 17 outer commands closed; all 10 SQL sessions have commit, frontend closure, and backend-gone evidence. Metadata and execution guards each match before and after, and both locks were released. HTTP requests, business SQL writes, application starts, and service changes were all zero.

The same three postmasters, five protected files, eight configuration hashes and metadata, and reference metadata were retained. The guard includes the historical hashes of the main binary and shared deployment lock. Environment files were hash-only and the master key was metadata-only.

The initial source/input preflight rejected an admitted reader whose filename did not match its fixed expected self path, before any SQL. That source remains saved. `candidate-disk-full-readonly.admitted-r02.py` corrects the expected filename once; query and closure bodies are unchanged. The rejection is retained as preparation history, not counted as another SQL observation or a product failure.

The independent `review_gates_and_evidence` review passed for the saved SQL intent and raw output, snapshots, exact baseline comparison, protected state, and command/backend/lock closure. It was a read-and-report review and produced no separate `independent-review.json`; the checkpoint therefore has no review receipt pin. The self-path preflight rejection was outside that scoped independent review.

Native-store checks, diagnostics/log preservation checks, and actual application restart have not been performed for this incident. Logical equality does not prove physical database integrity, application health, or client acceptance. Catalog and row observations use separate read-only transactions, and sequence facts are non-MVCC. The exit cause remains unproven by this work.

The September 15 recovery is historical evidence, not current application liveness. This September 16 input and output scope is consumed and must not be replayed.
