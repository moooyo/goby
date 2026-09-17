# M5 focused reconciliation diagnostic

The single `e1f47328ab38` focused diagnostic passed and did not reproduce the
original failures. The [pinned record](m5-reconciliation-focused-diagnostic-result-20260917.json)
captures its source, evidence, timing and owned-resource closure. The original
[`fcf3c8f4a474` full run](m5-combined-full-reconciliation-failure-20260917.md)
remains failed; no root cause or product fix is established.

The two selected top-level tests and both music subtests each ran once and
passed. Go reported 16.501 seconds. The three removal callbacks reached the
final-task-context phase with active owned, proof and task contexts and no
observed error:

| Case | Proof elapsed before finalizers | Callback elapsed | Final elapsed |
| --- | ---: | ---: | ---: |
| `OldVersionOneMember` | 4,588 ms | 4,592 ms | 4,594 ms |
| `InvalidTypedMetadata` | 4,968 ms | 4,971 ms | 4,973 ms |
| Scan replacement | 4,431 ms | 4,434 ms | 4,437 ms |

The slowest proof observation was only about 32 ms below its 5-second budget.
This timing is diagnostic evidence, not a proven explanation of the full-run failure.
Independent review closed 64 adapter and 23 worker commands, 67 recorded PIDs,
owned PostgreSQL, the worker unit/cgroup, outer group, three volumes, loop/backing
references and lock. Protected state remained exact and source records unchanged.

The earlier `255a27096f6d` source-admission rejection retains adapter/SSH exit 1: zero worker
or test invocations, no volumes and no lock acquisition, supported separately by
the failure supplement. The original incomplete closure and false `lockReleased`
and `protectedUnchanged` fields remain intact. Only the diagnostic consumer predicate
was corrected to accept the existing `prepared_source_only` source status.

At this checkpoint the phase-timing overlay was prepared only. Its later
[separate result](m5-reconciliation-phase-timing-result-20260917.md) does not
change this attempt's outcome. This diagnosis-only source is
not a release source, and this attempt supplies no full-suite, build, release,
frontend or browser acceptance. This documentation pass read saved records only
and ran no tests, validators, builds or runtime probes.
