# Post-close native capacity result reader

`capacity-result-reader.py` analyzes the existing fixed profile's saved records.
It does not import the controller, launch a process, open a connection, query a
database, inspect a live service, change a corpus, or write evidence. Its current
state is **synthetic contracts verified; no native profile result available**.

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
reader final/request/raw evidence, resource samples, and optional measurement
cost snapshots are opened. It does not follow configuration, credential input,
database, archive-member, source, or tool paths mentioned inside those records.
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

**Missing a record that binds actual scan-job active state to a client monotonic
interval.** Existing records therefore cannot produce demonstrated overlap. A
hash-bound `task_terminal` cancellation for the same phase, run, and input digests
can prove that a later request was outside that run's scan activity. Other
requests remain indeterminate. `window_end`, worker liveness, HTTP interval
intersection, or a plausible wall-clock mapping cannot establish active scan
overlap. Simultaneous closed HTTP intervals are reported separately; actual
uncovered scan time remains unavailable.

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

There is no idle control, slowdown estimate, SLO, saturation claim, representative
throughput result, or operating-system cold-cache claim. `capacityAccepted` and
`wholeM2Accepted` remain false. A permanently indeterminate overlap result does
not satisfy the complete M2 capacity objective; collection changes require a
separate reviewed adjustment before the original profile resumes.

## Component verification

`test_capacity_result_reader.py` contains synthetic saved-record and pure-math
contracts using only standard-library temporary files. It covers separated
outcomes, failure denominators, empty/missing records, cross-run cancellation and
scan-job bindings, clock discontinuity, interval classification, quantiles,
non-additive repeated-phase costs, resource gaps, pin/path/size limits, hardlinks,
symlinks, replacement during reads, and malformed JSON. The fixture intentionally
does not pretend to be a real catalog or native scan acceptance run.

The 20 new test methods passed on `test-env` as part of the corrected
[47-method component check](../../../docs/development/native-capacity-component-verification-20260916.md).
That check ran no Goby workload, application SQL, HTTP, or native scan. Actual
closed profile records remain unavailable. Do not rerun the historical reader,
transport, or pool checks merely to exercise this analyzer.
