# September 16 candidate application recovery

Both original applications were restored once with their original binaries and
configuration. The completed restart and the separate post-start diagnostics
observation each passed independent review. The [checkpoint](candidate-oom-exit-recovery-completed-20260916.json)
pins both executions, their dispatch results and their independent reviews.

The [pre-start preservation record](candidate-oom-exit-auxiliary-preservation-20260916.json)
remains a historical zero-start checkpoint. Its first failed log-inventory
attempt and the subsequent specifically bounded correction remain preserved.
The startup scope is consumed; do not repeat either application start to obtain
a new input or runtime binding.

## Recorded recovery result

A then B each started once. No PostgreSQL restart, binary/configuration change,
login or business HTTP request was made. Four health/readiness responses and
two application-owned deployment leases passed. Eight read-only SQL sessions
closed after comparing four databases across startup: each database retained
all 35 selected tables and five sequences exactly. The row totals were A source
230, A recovery 147, B source 141 and B recovery 131.

The three postmasters, five non-application units, five protected files, eight
configuration hashes, reference process/unit and historical main/deployment-lock
hashes were preserved. The saved failed application invocations remain in the
before-state. The resulting application identities are:

| Candidate | PID | Start ticks | Invocation ID |
| --- | ---: | --- | --- |
| A | 1648477 | 27509409 | `77c3831899be4bba9e2620c430593d60` |
| B | 1648540 | 27509490 | `72472b81fa2f43aea6fe48e6920fa37c` |

All 78 outer commands, 28 reader commands and eight SQL backends closed. The
dispatch exited zero without timeout, and both lock unlock and descriptor close
succeeded. The applications intentionally remained running.

## Separate post-start diagnostics result

All eight old A logs and five old B logs retained their bytes, hashes, metadata
and registry entries. Each application added one active log; only its identity
was inspected, without reading its body or asserting append-sensitive metadata
stability. Both ownership tokens were unchanged and the cache contained no jobs.

The protected before/after state exactly matched the completed restart's after
state. Twenty metadata commands, descriptors, the lock and the observer closed.
The phase accounted for 44 observed/evidence files, 722520 bytes read and 12782
bytes written in 291 milliseconds. These figures exclude inherited global
protection hashing, base-helper reads, metadata streams and outer receipt I/O.
No additional SQL, HTTP or service action ran in this phase.

The restart execution's original `postStartLogReadbackExecuted=false` remains
unchanged. This later execution and its own review establish post-start
preservation. The latter review inspected saved driver closure evidence, not
an independently pinned driver implementation.

## Remaining authority and delivery work

These are completed observations, not a fresh liveness check. They do not
establish the cause of the original exits, physical backup integrity or client
acceptance. The September 15 consumer envelope remains historical; a consumer
binding for the recovered runtime and the [verified Programs artifact](programs-complete-source-full-result-20260916.json)
is still required before transition.

The separate [schema-29 catalog bootstrap](m5-user-deletion-catalog-generation-20260916.json)
has now completed with independent artifact and resource-closure reviews while
preserving the original recovered applications. The user's
[selected cleanup and additional disk expansion](test-env-root-maintenance-20260916.md)
have since completed; resume serialized work with current resource admission.
The complete M2-M6 objective remains open, with M7 deferred.
