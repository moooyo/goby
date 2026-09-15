# Native capacity measurement decision

Date: 2026-09-15. Status: **NO-GO for native execution; keep the saved capacity
implementation frozen while the higher-value core and installation gates receive
a bounded disposition.** This is a planning and result-contract decision, not an
execution input. No capacity fixture, service, task or new verification run is
authorized by this document.

The [source checkpoint](native-scan-http-capacity-source-snapshot.json),
[handoff](session-handoff-20260915-native-capacity.md) and
[measurement plan](native-scan-http-capacity-plan.md) remain retained evidence.
This decision narrows the value and completion claims of the proposed increment;
it does not relax its ownership, resource, credential or closure requirements.
The current review made no source changes or local/remote verification calls.

## Delivery value and current priority

The useful question is: with the retained native binary, fixed resource limits
and 1,000 tiny media leaves, do normal cold and cached task scans complete while
restricted catalog GETs return correct authorized data, and what absolute HTTP
latencies and resource observations occur during demonstrated scan overlap?

This would fill the native task/TCP observation gap between the existing
10,000-leaf SQL and real-media tests. It would support a concrete follow-up:
investigate an observed correctness, isolation or bounded-operation defect, or
retain one limited native observation and move to another delivery gap. It does
not determine maximum supported library size, saturation throughput, acceptable
latency, representative media/storage performance or scan-induced slowdown. The
profile has no product SLO and no matched, adequately sampled idle control. An
empty-catalog check is not a latency baseline for a populated catalog.

The present critical path has more immediate value:

The later [internal amd64 G2 acceptance](internal-amd64-installation-acceptance.json)
has now closed the installation evidence decision. Capacity remains held while
the selected [core TV Response question](core-tv-response-observation-decision.md)
gets its retained-state and component/current-runtime entry contract. The
installation bullet below records the starting state of this allocation decision.

- Core movie/episode/subtitle acceptance still blocks main promotion. A next
  action must answer a discriminating question from retained evidence or verify
  a justified product correction; it must not replay a consumed journey.
- The fourth installation runtime passed, its sealer rejected a valid plain-text
  401, and the final sealing observer did not run. The evidence/resource closure
  is retained, but overall installation acceptance remains open. Existing
  evidence should first establish the smallest justified disposition; this does
  not admit a fifth installation attempt.

These facts are recorded in the [active execution queue](../planning/current-execution-plan.md)
and [handoff](session-handoff-20260915-native-capacity.md). Finishing a large
capacity operator does not close either gate. Capacity execution therefore stays
frozen until those higher-value next actions are completed or recorded as
blocked with specific missing evidence. This is an allocation-of-effort decision,
not a new technical dependency requiring every core or M2-M6 obligation to pass
before independent work can proceed. Reconsider capacity against the remaining
queue at that point; elapsed time or sunk implementation cost is not a GO reason.

## Minimum useful result

Keep one application invocation, two sequential normal task admissions, the same
1,000-leaf corpus, two restricted readers per phase, and the existing request,
byte, memory and lifetime ceilings. Do not add a sampler, increase the corpus,
slow the scanner or repeat an attempt to obtain a preferred measurement.

Report three independent outcomes:

| Outcome | Required evidence |
| --- | --- |
| Product checks | Exact task/child/job identities and normal terminal states; expected cold/cached counts; catalog, ACL and seeded UserData checks; unchanged corpus. A failed check retains its own failure rather than becoming a generic measurement failure. |
| Measurement completeness | For each phase, valid page and count samples from each restricted reader with demonstrated active-scan overlap, plus a demonstrated simultaneous-reader interval during active scanning. Retain resource availability and timing gaps. These coverage conditions establish that the question was observed; they are not a performance target or statistical confidence claim. |
| Resource closure | Reader joins, task drain if required, credential closure, APP/PG/anchor exit evidence, private archive readback, mount/unit/lock closure and protected-state readback under the existing contract. |

A phase may provide useful partial evidence even when coverage is incomplete.
For example, correct cached catalog responses after the scan ended remain valid
responses, but do not demonstrate reads during that cached scan. Only call the
full two-phase observation complete when both phases satisfy the coverage row.
Zero samples, uncertain overlap and unavailable metrics must remain explicit.
Neither a successful operator exit nor passing component fixtures establish
measurement completeness or capacity acceptance.

## Result contract

Produce one concise public result from immutable private receipts after owned
runtime resources are closed. Keep credentials, raw business responses and
master-bearing state private. Bind the source/artifact, corpus, CPU and memory
limits, PostgreSQL profile, filesystem, HTTP connection policy and actual task,
worker and request inventory to that result.

The primary HTTP grouping is **phase x query shape x actual scan overlap**:

- Phase: `cold` or `cached`.
- Query shape: `page-64` or `count-only`.
- Actual scan overlap: `demonstrated`, `none`, or `indeterminate`.

Retain per-reader/user sample counts inside each group so one user's samples
cannot hide missing coverage for the other. Do not combine cold and cached
latencies, page and count costs, or overlapping and nonoverlapping requests into
a headline percentile. Setup and settled boundary checks have their own table;
they are not substituted for concurrent-reader observations.

Each HTTP group records:

| Field | Definition |
| --- | --- |
| Counts | Intended, dispatched, complete, valid-success, timeout, transport/protocol failure, correctness failure and unavailable-timing counts; keep these categories and their denominators explicit. |
| Latency | Dispatch-to-header and dispatch-to-body-complete durations for complete valid responses, in milliseconds. Report minimum, median, p95 and maximum with the exact sample count. Errors/timeouts are separate observations, never silently dropped from the request inventory or converted into successful latencies. |
| Quantile method | Sort the observed values. Use the middle value for an odd-sized median and the mean of the two middle values for an even-sized median. Use nearest rank `ceil(0.95 * n)` for p95. For `n = 0`, statistics are unavailable. Small samples are descriptive observations without a tail-latency assurance claim. |
| Coverage | First/last dispatch, observed time span, dispatch spacing, per-reader counts, observation-window end reason, active-scan intersection intervals and simultaneous-reader intersections. Report portions of the scan not covered by readers. |
| Response context | Status, bytes, query shape, request ID, correctness outcome and private receipt references. During cold ingestion, retain catalog count/page-size context rather than treating growing pages as equivalent work. |

Retain server `StartedAt`/`FinishedAt` values separately from controller monotonic
observations. Bind overlap to actual child/scan-job lifetimes, not a task POST or
a run that can still be pending. The request interval is dispatch through body
completion; an incomplete exchange without a defensible endpoint has uncertain
coverage and is not used as proof of a complete overlapping sample.

For every wall-to-monotonic conversion, retain the bracketing clock anchors,
anchor widths, timestamp resolution, observed offset variation and the resulting
uncertainty interval. A positive intersection with a conservatively established
active interval is `demonstrated`; separation from every possible active interval
is `none`; a boundary ambiguity or unsupported clock mapping is `indeterminate`.
A wall-clock discontinuity must not be assigned an invented finite error bound.
Keep server-reported duration and controller observation bounds separate; polling
delay is not scanner duration. Two reader processes being alive is not evidence
that two HTTP exchanges overlapped.

Resource reporting retains actual sample timestamps, interval/gap distributions,
phase coverage, cgroup memory/CPU counters and available lifetime peaks. Label
process RSS maxima as sampled maxima and identify which processes were sampled.
Do not call the PostgreSQL main-process RSS total PostgreSQL RSS or cgroup memory
RSS. Missing counters are unavailable, never zero. Preserve CPU quota and tmpfs
limits as part of the interpretation.

Record controller observation overhead by phase and operation: ownership checks,
metric reads, storage enumeration, pool observation, evidence writes and command
waits. Retain operation counts, elapsed monotonic time and controller CPU time
where measured; separate subprocess time from controller CPU and avoid summing
nested spans twice. Existing reader/transport evidence-write timings remain
separate from HTTP latency. Unmeasured overhead is `unavailable`; no numeric
overhead estimate or causal subtraction from HTTP latency is justified by this
static review.

## Small-change feasibility from the saved source

The saved implementation offers two bounded opportunities. It does not yet prove
that every concern can be removed by a small edit.

| Concern and source evidence | Bounded approach | Limit |
| --- | --- | --- |
| `capacity-workload.py:770-771` invokes ownership and sampling consecutively; `capacity-controller.py:404-418` repeats `app.check_owned()`; `capacity-application.py:199-202,502-506` launches `systemctl show` for each check. | Reuse one accepted ownership observation within the same loop iteration. Preserve its scope, failure propagation and freshness contract; `capture_metrics` already accepts an observation. | Do not reuse it across an HTTP/SQL dispatch, phase transition or unrelated call. Keep a fresh standalone check for callers that have no valid observation. |
| `capacity-observers.py:794-798` rejects samples that are not yet due only after the caller already performed ownership work. | Expose a side-effect-free due decision before optional sampling work, using the existing clock/sample state. Retain the independent ownership/pool checks needed to detect a failure. | Skipping a metric sample cannot skip mandatory identity or reader-failure handling. No extra sampling process is needed. |
| `capacity-controller.py:287-300` walks the private evidence/log trees and the entire owned fixture; reservations and event writes also call this path. | Investigate reducing duplicate full scans within one safe observation boundary. Preserve pre-dispatch reservations and use conservative known writer bounds; keep full inventories at admission, phase boundaries and closure, with actual free-space checks as required. | A complete incremental-allocation replacement is not established. Asynchronous readers, APP logs, command capture and archives are separate writers. A ledger is valid only when every writer has an enforced before-write bound and outstanding reservations remain charged. Do not replace the existing checks with counters that omit these writers. |
| `capacity-workload.py:746-761` admits the task and reads its detail before creating readers; `capacity-reader-pool.py:707-727` then writes inputs, spawns and awaits readiness. | Preserve and report the admission-to-first-dispatch gap and the 120-second reader window against the 450-second scan ceiling. | Prearming readers requires changing the frozen input/start identity protocol; it is not just an ownership/sampling refactor. It is outside the current small-change allowance. No design can guarantee useful overlap from the present source alone. |

The known closure output-growth concern also remains: `capacity-closure.py:317-343`
reserves a declared stream bound but checks actual growth after the inherited
capture returns. Removing storage observations does not resolve this gap. A
future admission must establish a conservative enforced capture bound or remain
NO-GO. This document does not declare an existing hard limit proved.

## Finite remaining work and reopening decision

The following is the complete work list for reconsidering this increment. It is
not a request to execute it while the capacity freeze is in effect.

1. Record the bounded disposition of the higher-value core and installation
   actions, then compare capacity with the remaining independent deliverables.
   Reopen it only for the concrete native-observation question above.
2. Review the retained eight transport and six pool groups against their exact
   sources without replaying unchanged tests. Preserve the earlier twelve reader
   groups. If the one-byte raw-string correction is used, bridge that exact
   change remotely; do not transfer results across unmatched source pins.
3. Implement only the within-iteration ownership reuse, early due decision and
   bounded overhead accounting that are justified by the table above. Keep the
   existing writer/closure safeguards. If correctness requires a new ledger,
   launch protocol or general controller abstraction, stop and reconsider scope.
4. Finish one post-close result reader for this result contract. Check the finite
   classification cases: empty groups, mixed query/phase groups, small samples,
   failed exchanges, definite/no/uncertain overlap, clock discontinuity, sampling
   gaps and nested overhead accounting. Verification belongs on `ssh test-env`;
   this document claims no implementation or verification has occurred.
5. Close the already identified admission blockers: first-error retention,
   reader-join/zero-child evidence, credential and infrastructure close ordering,
   constructor/signal/deadline failure routing, fallback eligibility and
   before-dispatch resource reservations. Use bounded changed-path checks and
   retained unchanged evidence. Do not grow this into a new general framework or
   a repeated full product regression. An unresolved mandatory closure or write
   bound ends the admission review as NO-GO.
6. Only after those items pass, freeze one exact input and a reviewed GO decision,
   obtain fresh environment/absence/capacity evidence, and perform at most the
   declared native profile. Close it before independent result review. A fixture
   must never remain live while implementation or review is pending.

The GO decision must list the expected decision value, the accepted coverage
risk, exact sources, resolved closure/resource blockers and the remaining
execution budget. It must not treat this document, a historical observation or a
consumed input as execution authority. If the likely result cannot answer the
question without changing the workload or reader protocol, defer this increment
and choose another delivery gap.

During any future admitted run, retain the measurement plan's stop conditions:
identity/protection drift, unknown mutation outcome, unowned task/scan state,
ACL/correctness failure, incomplete evidence, reader failure, application
error/restart/OOM and time/space exhaustion stop new work and enter owned closure.
Missing overlap is a measurement-completeness outcome, not a reason to change
the running budget, repeat a scan, or delay closure. Report product failure,
measurement incompleteness and closure failure separately, preserving the first
failure and any unfinished responsibility.
