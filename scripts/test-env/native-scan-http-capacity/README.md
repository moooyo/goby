# Native scan and HTTP capacity source checkpoint

Status: paused before native execution admission on 2026-09-15, at the user's
request. These are saved candidate operators and synthetic checks, not an
approved installation or executable workload input.

Start with the [session handoff](../../../docs/development/session-handoff-20260915-native-capacity.md),
the [source manifest](../../../docs/development/native-scan-http-capacity-source-snapshot.json),
and the [measurement plan](../../../docs/development/native-scan-http-capacity-plan.md).
The source manifest records the exact saved bytes. Git text conversion is
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
do not prove native Goby capacity. Controller orchestration checks have been
designed but not written. The controller, preparation, application, observers
and preservation integration still need the remaining changed-risk verification.

`native-catalog-journey.py` is the unchanged pinned private helper copied from
the earlier installation source. Its old execution input is consumed; reuse
here is limited to the definitions imported by the new reviewed controller.
The support adaptation note describes its original static adaptation checkpoint;
later source checks are recorded in the preparation checkpoint and handoff.
