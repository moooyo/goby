# Running-job observation component verification

The collection change passed **61 unittest methods**, followed by **12 reader
and eight transport groups**. The initial pool attempt rejected the controller's
private network namespace before creating any pool child. A new pool-only scope
then passed all **six groups**, with 12 real stub lifetimes and no harness
fallback. Independent source, raw-result and closure review passed. These are
separate component scopes, not product-test totals. Exact pins are in the
[checkpoint](native-capacity-running-observation-verification-20260916.json).

Jobs are observed after the existing second and third detail slots, giving
requests an ordinary polling interval in which overlap may be established.
The original reader start, scan workload and absolute deadline are unchanged.
Two later detail calls are omitted to fund the additional observations: at most
89 detail calls, two observation calls and one terminal jobs call per phase.
The task-poll cap remains 184 and the total HTTP cap remains 600.

The result reader binds actual saved response bytes to the same run, child, job,
library and monotonic clock domain. It separately reports full and partial
request intersections with the guaranteed Running interval. Running is persisted
job state, including possible finalization work; continuous filesystem or
ffprobe activity is not claimed. Completed or short scans and missing clock
bindings can remain incomplete without changing the workload to obtain a pass.

All three check units retained 128 MiB memory, zero swap, 25% CPU, 32 tasks,
a 240-second runtime limit and an 8 MiB per-file limit. The pool-only unit needed
`PrivateNetwork=no` for the unchanged host-network controller contract and used
`RestrictAddressFamilies=AF_UNIX`; private mounts, temporary paths and filesystem
restrictions remained in place. All selected checker parents were waited and
closed, their cgroups were empty, and scratch was empty. The prior failed attempt
and expected negative pool outcomes remain preserved.

The six runtime files stayed identical across the three scopes. Only checker
fixtures and strict source/output path selection changed afterward; the already
passing groups were not repeated. Native admission still needs current runtime
and artifact bindings, a fresh owned fixture and actual workload evidence.
