# Reference baseline observer 03 independent acceptance

Run `nextup-preparation04-baseline-observation-20260913-03` captured the
complete current reference baseline and received independent acceptance at
`2026-09-12T22:05:27.003682+00:00`. The independent terminal records
`status=complete_baseline_independently_accepted` and
`baselineUsableForFreshPreparation=true`, with `matrixFixtureReleased=false`.

This capture follows the independently sealed
[preparation 04 failure and separate UserData recovery](nextup-global-reference-preparation-04.md).
It observes the retained population without repeating fixture creation or
reusing the earlier eight-user, ten-library baseline.

## Frozen implementation and verification

The dedicated [observer](../../scripts/test-env/observe-nextup-preparation04-baseline.py)
and [guards](../../scripts/test-env/test-observe-nextup-preparation04-baseline.py)
were frozen under
`/opt/goby-test/exec-work-m3e/nextup-preparation04-baseline-observer-tool-01/revision-02`.

| Frozen component | SHA-256 |
| --- | --- |
| Observer source | `126d625a9b68871b88a78158584d0fb65720fd605436edf288164336625efbc6` |
| Guard source | `b22478e59006e2d9c12af6e69948e5bbbbc16d43fb62d6c66b722e5e883e0a28` |
| Existing transport source | `d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1` |

Revision 02 passed 101 remote guards and two compilation checks using root
SSH and `/usr/bin/python3 -I -B`. The guards cover complete synthetic
64-request execution, actual journal bytes, tuple response headers, the
frozen HTTP parser with synthetic sockets, ownership and failure handling,
and completed-evidence replay. The historical input boundary is synthetic
in those guards; actual parent and recovery evidence was separately admitted
before this observation.

Revision 01's 96 passing guards remain historical evidence. Subsequent
review found that completed evidence could substitute an external identical
state copy or bind an inappropriate unit command. Revision 02 fixes the
actual private output paths and verifies the complete `observe` argv,
including the actual input digest and recomputed plan digest. The earlier
revision and its report remain unchanged.

The verification reports are the
[initial revision 01 report](nextup-preparation04-baseline-observer-verification-01-initial.json),
[revision 02 report](nextup-preparation04-baseline-observer-verification-02.json),
[101-guard report](nextup-preparation04-baseline-observer-guards-02.json), and
[compilation report](nextup-preparation04-baseline-observer-compile-02.json).

## Actual complete observation

The run recorded exactly 64 requests: 62 normal requests and two closure
requests. One existing-administrator login preceded all 61 snapshot GETs.
Logout returned an empty HTTP 204, followed by HTTP 401 using the exact same
token and client metadata. The existing proxy on port 18197 was reused
unchanged. The observer created no users, libraries, media copies, or
playback state and issued no DELETE request.

The read count derives from the actual scope:
`GET = 5 + libraries + 2 × users + details = 5 + 12 + 20 + 24 = 61`.
The complete snapshot includes server information, the user roster,
library definitions, all scoped catalogs and subject projections,
preferences, full item details, Devices, and System/Configuration.

The accepted baseline contains 10 users, 12 libraries, 98 devices, and 24
full detail witnesses:

- The previous 12 full administrator-subject detail witnesses remain
  preserved.
- All 12 new P/Q detail witnesses were freshly read under this observer's
  administrator token, covering six episodes per subject. Their actual
  identity, season and series relationships, runtime, and exact four-key
  zero UserData were verified against the acknowledged historical evidence.
  Historical own-token detail DTOs were not copied into the new snapshot.
- The 96 previously observed devices remain present. Exactly one separately
  closed recovery device and one current observer device complete the
  98-device roster, each bound to its actual login identity. Permitted
  authentication-date changes remain restricted to their proven windows.

The restored P4/A1 subject projection now has exact zero UserData. The old
catalog projection and full item UserData were checked as distinct response
shapes. The separately accepted recovery and all newly read full P/Q
details establish the required full zero state; other retained public
documents remain preserved under the explicit authentication exceptions.

## Independent replay and process closure

The independent audit invoked
`verify_completed_evidence({manifest, independent}, read_bytes=...)` from
the frozen observer source. It rebuilt the entire baseline from all 64
actual raw responses and compared every generated intent, reservation,
response, final state, preservation result, authentication closure, and
terminal with retained exact bytes. The completed output inventory matched
all 207 entries: 204 files and three directories. The audit issued no
business HTTP requests.

The controlled unit was
`goby-nextup-preparation04-baseline-observer-03.service`, invocation
`475e6e5130ca4ab5947f37bafc60d535`. It completed successfully with exit
status 0, MainPID 0, and active/exited state. Independent checks confirmed
former PID 1515339 absent and the cgroup empty. Its full `observe` command
matched the frozen source, input, and plan.

All 158 retained roots, service records, and main-file records remained
equal. Complete Goby state remained equal except for its recorded capture
timestamp, retaining 83 sessions, 70 devices, and 185 activity entries.
The original observer terminal remains
`status=awaiting_independent_attestation` and `baselineReleased=false`;
independent acceptance did not rewrite that worker record.

See the [independent terminal](nextup-global-baseline-observer-03-independent-terminal.json)
and [actual verification report](nextup-global-baseline-observer-03-verification.json).

## Private scope and handoff pins

All paths below are relative to `/opt/goby-test/exec-work-m3e`. The raw
requests, credentials, full baseline, private state, and authentication
closure remain in the owned remote scopes.

| Evidence | Path or SHA-256 |
| --- | --- |
| Input root | `nextup-global-reference-baseline-inputs-03` |
| Execution and independent evidence root | `reference-nextup-global-baseline-execution-03` |
| Consumed observer output root | `reference-nextup-global-baseline-observation-03` |
| Input `input.json` | `cc59138eda23e99832263d3d85feda89f92a39fda69e7c4146a0609e8ee4a320` |
| Frozen plan digest | `60ea6d44c9389239370ad56ac5885b70126d29fd0804a52fe76a4519e174a309` |
| Independent `independent-terminal.json` | `f5b2521936e18b00787c4d8ec068410d6d142363f74dfba9051577016005bba1` |
| Output `private/public-baseline.json` | `518bf101500b94c1b1c07c2c90934f96ee741265b5058e16a4103b4d45439a0f` |
| Output `private/closed-authentication.json` | `ea9b9311e3c6b7a30270e37c429a725b6a3f6aea86d6a892274aed7c6456e736` |
| Execution `wire-index.json` | `0506f8efd4445120186ccf5427337176e84f8a0ce2877051ff5c1e49a14a9fca` |
| Execution `completed-scope-files.json` | `86bb7472230a24e33aa4a8b047b75a35b8912d6431440a8128c71fe522557e58` |
| Execution `preservation-before.json` | `77baa27e16bf527e5b676ff497e4bd6b03c626c9cda924312048e0769570e98f` |
| Execution `preservation-after.json` | `972e3a9bdff3c4580da0784cf6057e835fe1a3e13b96df83e424093094428d3b` |

## Acceptance boundary and next preparation

This result supplies a complete baseline usable for a fresh preparation.
At this acceptance checkpoint, the producer still requires release-v3
integration and population/budget adaptation for 10 users, 12 libraries,
and 24 detail witnesses. Its new `baselineObservation` evidence pair must
bind this manifest and independent terminal, and its public baseline must
match the accepted `afterPublic` bytes through the frozen replay interface.

A subsequent preparation needs a fresh input, output, and execution scope,
with its own planned account, library, and media creation. Preparation 04,
its separate recovery, and observer 03 remain consumed historical scopes.
This observation does not release a matrix fixture, execute the NextUp
matrix, or establish client acceptance.
