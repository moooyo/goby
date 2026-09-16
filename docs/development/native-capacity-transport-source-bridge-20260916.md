# Native capacity transport source bridge

The one-byte regex correction passed one remote source check. At byte offset
21343, the new source adds only `r`; the AST is identical when source locations
are excluded, and all 631 string constants retain their values. Python 3.13
reported the old invalid-escape warning and compiled the new source with
warnings treated as errors. The target module was neither imported nor executed.
Exact source and evidence pins are in the [checkpoint](native-capacity-transport-source-bridge-20260916.json).

One test ran once, with no failure or skip. The earlier eight transport groups
and six pool groups were not rerun. Root read the actual result, stdout, stderr
and closure; independent review passed. The bridge applies only to transport
SHA `a87681751f1eea4881fa549be4eb7fa5e6eb06aee030051271eab4f163190928`.
New scan-overlap and clock-domain changes need their own affected checks.

The same independent review closed the historical result-review gap for the
eight transport and six pool groups. It matched their saved source, dispatch,
raw stream and nested receipt pins, including 12 exchange receipts and 12 reaped
short stub children. Expected negative pool outcomes remain failed and retain
their reservations; harness fallback was not accepted as closure. These fixture
results do not prove native TCP, Goby responses, scanner activity or reader main
execution. No historical group was replayed, and no old receipt was modified.

The dispatch requested a 128 MiB unit, zero swap, 25% CPU, a 60-second runtime
limit and a 1 MiB per-file output limit. The short-lived unit was already
collected when properties were read, so there is no retained live-property or
peak-memory observation. The launcher and test were waited, the test PID was
absent, the unit was absent, the cgroup was absent or empty, and scratch was
empty. No application, database or native-capacity result follows from this
source-only check.
