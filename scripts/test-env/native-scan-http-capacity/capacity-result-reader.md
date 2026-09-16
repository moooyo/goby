# Post-close native capacity result reader

`capacity-result-reader.py` analyzes the existing fixed profile's saved records.
It does not import the controller, launch a process, open a connection, query a
database, inspect a live service, change a corpus, or write evidence. Its current
state is **paired Running-state contracts and affected component checks verified;
native profile still pending**. The separate 61-method and reader/transport/pool
scopes retain identical runtime source hashes and independent review. Their
checker-only adjustments do not change the runtime code or turn fixture results
into a native profile result.

The only production entry is the exact scope
`/opt/goby-test/native-scan-http-capacity-20260915`. The CLI accepts a pinned
`execution.json` or the existing private controller fallback. An example after
the profile and its resource closure have actually completed is:

```sh
python3 -I -B capacity-result-reader.py \
  --execution /opt/goby-test/native-scan-http-capacity-20260915/execution.json \
  --execution-sha256 <actual-saved-execution-sha256>
```

This command is documentation, not a dispatch authorization. No current native
execution input or successful native result is implied. The historical paused
mount experiment remains separate.

## Read boundary

The reader follows exact, caller-selected relative filenames beneath that one
scope; input descriptors cannot choose another file or directory. It opens
directories and regular files with no-follow descriptors, requires private
root-owned directories and single-link mode `0600` files, checks file identity
before and after reading, and rechecks named directory identities. It checks
SHA-256 and declared sizes with per-role bounds and a cumulative 384 MiB read
limit. Duplicate JSON keys, nonfinite numbers, replaced files, wrong pins, and
cross-run bindings are rejected with fixed error codes.

Only the execution, small component records, SQL parsed payloads and receipts,
reader final/request/raw evidence, four scan-observation event/control receipts,
the reader context, resource samples, and optional measurement cost snapshots are
opened. The exact `private/capacity-reader.py` and `private/capacity-transport.py`
files are read only to bind source bytes; neither is imported or executed. It
does not follow configuration, credential input, database, archive-member,
arbitrary source, or tool paths mentioned inside those records.
Raw HTTP bytes are hash checked and discarded; they are not included in output.
Private payload values and credentials are not printed. Archive preservation
and process closure are reported as recorded facts, not newly observed facts.

Missing files or absent components produce explicit coverage gaps. Contradictory
pins or unsupported record shapes reject the analysis. Missing physical closure
prevents deeper SQL and reader/raw analysis. Missing preservation proof does not
erase usable measurements after physical closure; the separate closure result
remains incomplete.

## Result interpretation

- `resourceClosure` distinguishes recorded physical closure from recorded
  preservation. It checks application, two infrastructure units, actual reader
  children, commands, lock release, and namespace closure independently of the
  workload outcome. Existing archive readback flags are not fresh archive hashes.
- `productChecks` reports upstream saved checks with receipt and scan-lifecycle
  readback. `recorded_pass` is not a re-execution of the full ACL, catalog,
  credential, HTTP parser, or product acceptance suite. A missing or empty reader
  does not satisfy the reader-evidence check.
- `measurement` reports the twelve cold/cached × page64/count ×
  demonstrated/none/indeterminate groups. The existing internal shape `page`
  means page size 64. Each group preserves intent and dispatch denominators,
  per-reader counts, non-dispatches, failures, unavailable evidence/timing, and
  successful complete-response latency observations. The fixed 240-request
  allowance is a ceiling; unused allowance is not counted as requests.

Latency starts at the reader's recorded pre-connect dispatch timestamp, which
differs from the control transport's first-positive-send timestamp. Header,
body-complete, and connection-close latency quantiles use only complete,
successful, correctly recorded responses with intact evidence. Median uses the
middle observation or the mean of the two middle observations; p95 uses nearest
rank `ceil(0.95*n)`. Empty groups have `n=0` and null quantiles, never zero latency.
Control HTTP exchanges are excluded from these latency groups.

The reader preserves clock-anchor read widths and observed offset ranges,
including wall-clock regressions and whether a constant offset fits the observed
anchors. It also shows a conditional mapping of recorded scan-job wall intervals
through the observed offset envelope. These are descriptions of observations,
not proof of clock continuity between them.

Old records without a clock domain or paired job observations retain the gap:
**Missing a record that binds actual scan-job active state to a client monotonic
interval.** Bare monotonic values alone no longer grant either demonstrated
overlap or terminal exclusion under this new measurement contract.

New observations use two existing natural observation points. Readers start
immediately through the original ready/start protocol. The first jobs GET follows
the slot 2 task detail; the second follows the slot 3 task detail. The existing
five-second periodic wait supplies these points. `last_poll` is captured at detail
completion, before the additional GET, so that GET time does not add another five
seconds. Initial completion or completion at slot 2 fills remaining observations
immediately without another wait and grants no Running interval. Failed,
cancelled, interrupted or stopping tasks get no extra business observations.

Each observation binds the actual control HTTP receipt, its raw body, task/run
request identity, child/job/library relation, collection point and detail slot.
Early lists may contain zero or one new job; cached lists must still retain both
old cold jobs exactly. Terminal jobs cannot return to Running, disappear, change
identity, or regress counters. The final SQL lifecycle independently binds each
observed job. Unknown or foreign jobs are rejected.

Controller, pool, each observed GET, and each reader record their actual
`CLOCK_MONOTONIC` implementation, boot ID, and time-namespace device/inode at both
ends of the relevant work. All must be available, unchanged, and equal. Reader
source, input, context and start pins must also match the pool. No clock metadata
is backfilled into old records. Context, ready, start and cancel schemas are
unchanged; the start anchor must precede both observations.

Two `running` responses for the same job establish only the conservative interval
from the **first response body completion to the second request dispatch**. A
reader is matched to the job for its own library. Complete connection intervals
wholly inside that interval and intervals with only a positive intersection are
counted separately. Missing clocks or source bindings, a short scan, a late-started
child, or missing observations cannot be promoted to demonstrated overlap.
Clock-bound `task_terminal` cancellation can still establish a later exclusion.
`window_end` and worker liveness provide no such proof.

The guarantee concerns **persisted scan-job Running state**, which may include
waiting for database finalization. It does not establish continuous filesystem or
ffprobe work. Simultaneous HTTP within proven Running intervals is reported
separately from ordinary simultaneous HTTP. Actual uncovered filesystem-scan
time remains unavailable.

Resource output reports sample spacing, gaps beyond the existing two-second
target, sampled main-process RSS, available cgroup memory observations, and CPU
counter observations. Main PostgreSQL RSS is not total PostgreSQL RSS. Missing
counters remain unavailable. It does not estimate unobserved maxima or CPU use.

Optional `measurementCosts` uses the controller's exact version 1
`inclusive_non_additive` schema. Repeated phase names remain separate stages.
Ownership, metrics, pool, storage-walk, evidence-write, and command-wait elapsed
times overlap and are never added together or subtracted from HTTP latency. This
accounts only for wrapped paths, excludes unwrapped helper work, and excludes
the final cost/result publication. Older records without these fields explicitly
report unavailable cost data.

The two extra GETs per phase replace detail HTTP at slots 30 and 60, both beyond
the 120-second reader window. Those slots still advance and retain ownership and
resource observation. There are at most 91 observation slots, 89 actual detail
GETs, two jobs observations and one terminal jobs GET: 92 task-poll requests per
phase, 184 overall. Setup 112, cleanup 64 and reader 240 ceilings remain distinct;
their total remains 600. The original 450-second scan deadline, 1200-second business
deadline, reader deadlines, corpus and task/reader start/cancel protocols do not
change. Actual `pollCount` remains the count of detail HTTP calls;
`pollSlotCount` and `skippedDetailSlots` describe scheduling separately.

There is no idle control, slowdown estimate, SLO, saturation claim, representative
throughput result, or operating-system cold-cache claim. `capacityAccepted` and
`wholeM2Accepted` remain false. A permanently indeterminate overlap result does
not satisfy the complete M2 capacity objective. New native input and dispatch
still require separate review. `recorded_with_scope_limits`, when all required
records and per-reader/shape Running coverage exist, is a measurement description,
not capacity or delivery acceptance.

## Component verification

`test_capacity_result_reader.py` contains synthetic saved-record and pure-math
contracts using only standard-library temporary files. It covers separated
outcomes, failure denominators, empty/missing records, cross-run cancellation and
scan-job bindings, clock discontinuity, interval classification, quantiles,
non-additive repeated-phase costs, resource gaps, pin/path/size limits, hardlinks,
symlinks, replacement during reads, and malformed JSON. The fixture intentionally
does not pretend to be a real catalog or native scan acceptance run.

The earlier 20 test methods passed on `test-env` as part of the corrected
[47-method component check](../../../docs/development/native-capacity-component-verification-20260916.md).
That check ran no Goby workload, application SQL, HTTP, or native scan. Actual
closed profile records remain unavailable.

This revision adds paired-running, partial/full intersection, foreign/rebound
identity, state/counter regression, missing/changed clock, natural-slot scheduling,
terminal fallback and bounded clock-read contracts. The separate isolated
test-env run passed 61 pure methods: result-reader 28, measurement 14,
failure-routing 8 and scan-observations 11. The pool checker was present as source
but was neither executed nor imported by that run. No test or syntax check was
run while preparing the subsequent pool-checker-only correction; that correction
does not change the six runtime sources or those four unittest sources.
The [subsequent component verification](../../../docs/development/native-capacity-running-observation-verification-20260916.md)
passed the affected reader, transport and pool groups with independent result
and closure review. Historical inputs, snapshots, component receipts and the
paused mount experiment are unchanged.

The reader and transport checks used
`/opt/goby-test/native-scan-http-capacity-running-checks-20260916`, with sources in
`private/source` and separate `private/reader-checks`, `private/transport-checks`
and `private/pool-checks` outputs. Reader 12 and transport eight groups passed.
All six pool cases rejected the private network namespace before creating any
pool child. That failed scope is consumed and preserved. The new pool-only scope
`/opt/goby-test/native-scan-http-capacity-running-pool-checks-20260916-r02`
passed all six groups using the real host network namespace with
`RestrictAddressFamilies=AF_UNIX`, while retaining the other resource and file
isolation limits. Its checker changed only the strict scope string; the six
runtime sources stayed identical. Neither successful group was replayed, and
neither execution scope is reusable.

The zero-request pool stub explicitly emits unavailable `clockDomainBefore` and
`clockDomainAfter` values (`version=1`, `available=false`,
`code=clock_domain_unavailable`). These are lifecycle fixtures, not real clock
observations or overlap evidence. The pool check accepts only the exact pool,
reader and checker filenames beneath the new `private/source` directory; hashes
remain supplied and checked by its existing CLI. The new-source 12 reader,
eight transport and six pool groups are verified only within those fixture
boundaries. They do not establish actual scan overlap or native capacity.
