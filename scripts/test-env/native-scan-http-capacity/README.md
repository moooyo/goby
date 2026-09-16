# Native scan and HTTP capacity source checkpoint

Status: native execution remains held. The September 15 pause is preserved;
the September 16 operator corrections have passed their bounded component
checks. These sources are not an approved installation or executable workload
input.

Start with the [session handoff](../../../docs/development/session-handoff-20260915-native-capacity.md),
the [source manifest](../../../docs/development/native-scan-http-capacity-source-snapshot.json),
and the [measurement plan](../../../docs/development/native-scan-http-capacity-plan.md).
The source manifest records the original September 15 saved bytes. It is not
the current revised-source manifest. The
[September 16 component checkpoint](../../../docs/development/native-capacity-component-verification-20260916.md)
binds the changed controller, observers and closure, the new post-close result
reader, and their four check files. Git text conversion is
disabled for pinned Python sources and the original adaptation note so a clone
does not silently change their SHA256 values.

All execution and verification belongs on `ssh test-env`, using
`/usr/bin/python3 -I -B`; local execution is not authorized. The helpers retain
fixed remote paths, source pins and admission checks. Do not run them directly
from this repository, relax their guards, or replay an existing test output or
consumed installation input. No fresh controller input or execution decision
has been issued.

The reader has twelve passing remote synthetic groups. Control transport has
eight passing groups on its retained candidate-03 revision; the saved transport
adds one unverified raw-string prefix to remove a Python warning. The pool has
six passing real child-lifecycle fixture groups. These are separate scopes and
do not prove native Goby capacity. The new measurement, closure, controller
failure-routing and result-reader groups passed 47 methods in one corrected
run. They cover one-shot observations, inclusive measurement costs, enforced
child output limits and failure closure routes. A dual-publication failure's
incorrect final status was fixed without relaxing its test.

The [result reader](capacity-result-reader.md) preserves useful latency and
resource observations while separating product records, measurement gaps and
resource closure. The existing collection does not bind actual scan-job active
state to client monotonic intervals, so it cannot prove scan overlap. That
collection gap, the transport's one-byte source bridge and current runtime/
artifact admission still need resolution before the native profile resumes.

`native-catalog-journey.py` is the unchanged pinned private helper copied from
the earlier installation source. Its old execution input is consumed; reuse
here is limited to the definitions imported by the new reviewed controller.
The support adaptation note describes its original static adaptation checkpoint;
later source checks are recorded in the preparation checkpoint and handoff.
