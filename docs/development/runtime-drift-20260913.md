# Preexisting Goby runtime drift on 2026-09-13

Follow-up: [independent source55 recovery](candidate-source55-restart-closeout.md)
verified the original candidate running and ready at `2026-09-13T06:01:44Z`.
The historical failures and the unresolved exit cause below remain unchanged.

The first matrix07 preservation capture stopped before any matrix business
request because two Goby service identities differed from the retained
diagnostic04 checkpoint. The [original prelaunch failure](nextup-global-reference-matrix-07-prelaunch-failure.json)
remains a failed observation. Read-only `systemctl show`, bounded service and
kernel journal inspection, PostgreSQL service metadata, and binary hashes
established the facts below. No service was restarted or changed during this
investigation; raw logs and sensitive runtime data were not exported.

## Observed service timeline

All times in this table are UTC on 2026-09-13.

| Time | Observed event |
| --- | --- |
| 00:27:15 | `goby-client-m3e.service`, former PID `1458051`, exited with code 1. Its `Restart=no` policy left it failed with MainPID 0. |
| 00:27:16 | `goby-foundation-test.service`, former PID `762090`, exited with code 1. |
| 00:27:19 | The primary's `Restart=on-failure` policy automatically started PID `1703857`. |
| 00:32:35 | Primary PID `1703857` exited with code 1. |
| 00:32:38 | The primary automatically started PID `1704380`. |
| 03:32:41 | Primary PID `1704380` exited with code 1. |
| 03:32:44 | The primary automatically started PID `1778525`, invocation `af71e306c80b4d11a7683ac819562e48`. |

At inspection, the candidate retained invocation
`d29ea64c63274346a79c7b3a7938f36e` and its failed state. The primary was
active/running with `NRestarts=3`. These changes predated matrix07 execution;
the matrix did not cause these exits or restarts.

## Deployment identity and cause boundary

| Artifact inspected | SHA-256 | Retained deployment |
| --- | --- | --- |
| `/opt/goby-client-m3e/goby` | `6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81` | Candidate source55 |
| `/opt/goby-dev/goby` and `/proc/1778525/exe` | `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620` | Primary source32 |
| Separately verified architecture-audit build | `b644295d2eb7d58c1351f12396a46e621d5819d77ee653e575c81cff6d7b5665` | Verification artifact; not either deployed binary |

The candidate and primary hashes match their historical deployment receipts.
Their file modification times remain 2026-09-12 10:10:08 UTC and
2026-09-11 19:05:05 UTC respectively. The new primary process executes the
unchanged primary binary. The [architecture-audit verification receipt](audit-remediation-20260913-verification.json)
therefore does not represent a silent deployment to either service.

Service logs retain `server.stopped` with `error_class=unclassified`. Host
memory-pressure messages occur near the exits, including the 03:29-03:32 UTC
pressure window already recorded by the audit verification. The first two
exit episodes preceded its first remediation verification scope at
02:26:02 UTC. These observations establish timing, not a specific exit cause
or attribution of the host pressure to an audit workload. In particular,
they do not prove OOM termination or a database-lease timeout.

`postgresql@17-main.service` retained PID `886`, `NRestarts=0`, and its
2026-09-10 21:52:21 UTC start time. No PostgreSQL restart was observed. A
bounded inspection of the existing PostgreSQL service log found four client
receive-error events in the relevant windows, without sufficient ownership
context to attribute them to these Goby exits. The exact application exit
cause remains unresolved by the retained sanitized evidence.

## Continuation boundary

Reference matrix07 continues with an explicit fresh service baseline that
records these two preexisting differences. Its new before/after preservation
comparison must preserve that measured state while retaining the earlier
failed capture and historical identities unchanged. This document claims no
matrix acceptance. Candidate recovery remains separate work requiring a new
owned recovery scope; it is not a prerequisite for the isolated reference
matrix and was not performed here.
