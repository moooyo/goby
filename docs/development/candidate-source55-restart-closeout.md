# Source55 candidate restart closeout

At `2026-09-13T06:01:44Z`, an independent attestation verified that the original
source55/schema28 candidate was running and ready. Exactly one service-start
call was made. The binary, complete database, recovery bytes and media/data
outside diagnostics remained unchanged. The primary, reference application,
existing proxy and both closed client workers were preserved.

The [independent terminal](candidate-source55-restart-attestation-02-terminal.json),
SHA-256 `f0067fe182ee60e13759461382643718b77bc7d97850e77d24cfa7504f9b82e0`,
is the current process authority. It does not rewrite earlier worker results.

| Current candidate binding | Value |
| --- | --- |
| Unit | `goby-client-m3e.service` |
| PID / start ticks | `1814145` / `20136395` |
| Invocation | `3d9fccdb4f4d4f129ee02b33f6c73ce1` |
| Executable | `/opt/goby-client-m3e/goby` |
| Executable SHA-256 | `6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81` |
| Executable device / inode | `2049` / `2825322` |
| Service UID | `995` (`goby`) |
| Endpoint | `127.0.0.1:18198` |
| Source / schema | source55 / 28 |

The attestor verified exactly three complete 200 responses: `/healthz` with
`Status=ok`, `/readyz` with `Status=ready`, and `/emby/System/Info/Public` with
the retained server identity. No cookie was issued. The source-bound process
and listener were checked before and after each request. The single PostgreSQL
deployment lease remained held by the candidate's own connection; the helper
did not acquire that application lease.

Complete database snapshots before the start, after the start, and before/after
the independent probes matched, excluding only capture time. This includes
rows, sequences, schema and ownership, with 83 sessions, 70 devices and 185
activity entries. Non-diagnostic data and media trees matched exactly. Active
diagnostic contents were deliberately excluded; their directory identity,
owner and mode remained bound. Normal startup and request logging is not
described as a zero-write operation.

## Preserved unsuccessful observations

The first recovery scope stopped before any start call because its checker
expected lowercase `completed` instead of the actual `Completed` scan-job
enum. Its [terminal](candidate-source55-restart-01-terminal.json) remains
`failed_before_start` with `startCalls=0`. The five jobs were already completed
and all required task/encoding queues were empty; no database repair was needed.

The new recovery02 scope made the one acknowledged start. Its first process
binding sample failed before HTTP and its
[terminal](candidate-source55-restart-02-terminal.json) remains
`recovery_required`, SHA-256
`2476acb99b1309152b06170026e503b7be19d3480ddd41e491aa22d3f2e11760`.
The raw failing sample was not retained, so its exact failed field and cause
remain unknown. Later samples matched the expected running process. The
independent attestation proves current readiness and preservation without
reclassifying that original failure or issuing another start.

The first independent attestor stopped before HTTP because its audit hook
rejected Python's `os.posix_spawn` backend after permitting the corresponding
read-only `subprocess.Popen`. Its
[failed receipt](candidate-source55-restart-attestation-01-terminal.json) is
retained. The final source permits only one backend event matching the current
approved executable, argv and environment. It completed 57 permitted read-only
subprocesses and three public connections, with zero forbidden service calls,
writes or reads. Both independent attestors made zero service-start calls.

Private execution roots are under `/opt/goby-test/exec-work-m3e`:
`candidate-source55-restart-01`, `candidate-source55-restart-02`, and
`candidate-source55-restart-attestation-01`/`02`. All are consumed. Do not rerun
their operators or rewrite their journals.

## Remaining boundaries

The original `client-fixture.json` remains immutable and still contains its
historical PID. A future operation must explicitly bind this new independent
process receipt in a fresh input instead of treating that historical PID as
current. The primary remains source32/schema27, PID1778525. Architecture-audit
fixes remain published but undeployed; this recovery installed no new binary.

The [earlier service exits](runtime-drift-20260913.md) remain unexplained by the
retained sanitized logs. Readiness recovery does not establish their root cause
or guarantee that they cannot recur. Positive NextUp/client refresh evidence,
the [separate Goby comparison](nextup-goby-comparison-contract.md), primary
upgrade and remaining M2-M6 work remain open. M7 remains deferred.

All compilation, probes and database/runtime verification ran through
`ssh test-env`; no local execution or verification fallback was used.
