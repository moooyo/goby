# Reference NextUp matrix 07

Checkpoint: `2026-09-13T05:00:23Z`. The reference matrix completed once with
158 actual HTTP requests, followed by independent wire reconstruction and
runtime/identity/preservation verification. Its outcome is
`reference_global_positive_unresolved`: all ten global NextUp responses were
empty. This is a completed negative protocol observation, not a positive
selection rule, Goby comparison, or original-client acceptance.

## Recovered progress and preexisting runtime drift

The preceding `a623375` architecture-audit increment was independently checked
against its retained remote evidence. All five controller-to-worker report
bindings matched. The 24 complete package runs and the server package's
542 passes plus one exact fresh retry account for 2,255 unique top-level Go
tests. The 41 frontend tests, build artifacts and recorded source differences
also matched. Fifteen sampled regression groups covering 28 files contain
105 passing top-level test entries. This was read-only evidence review, not a
new full-suite run.

The first matrix07 preservation capture detected two changes since diagnostic04:
the candidate had exited with status 1, and the primary had automatically
restarted three times. Their binaries and the complete Goby database remained
unchanged. The [failed capture record](nextup-global-reference-matrix-07-prelaunch-failure.json)
is retained. A new capture revision explicitly bound those two already observed
service differences, then required exact before/after equality. Historical
service identity equality was not relabeled as passing. See the
[runtime investigation](runtime-drift-20260913.md).

## Actual execution and observations

Paths use `/opt/goby-test/exec-work-m3e` as `W`.

- New attestation: `W/reference-nextup-global-matrix-attestation-07`.
- New operator output: `W/nextup-global-reference-operator-runs-07/operator-07`.
- New execution records: `W/reference-nextup-global-matrix-execution-07`.
- Original bound matrix output, now consumed:
  `W/nextup-global-reference-runs-05/matrix-05`.
- Unit: `goby-nextup-global-reference-matrix-07.service`.
- Invocation: `b5e5526bf57b4dffb9ac1bac919c101e`; former PID `1805768`.

The actual restricted unit passed admission and replayed the 269 preparation05
requests before matrix HTTP. Preparation05 and all older scopes were not run
again. The existing proxy was not restarted. The frozen TOOL05 operator,
release-v3 producer, transport and matrix were unchanged.

The matrix used 116 normal requests and 42 cleanup requests. All ten global
responses contained `Items=[]` and `TotalRecordCount=0`. The fourteen SeriesId
controls retained their expected ordered sequences: after P completes A1,
A returns A2/A3; after B1, B returns B2/B3; after A2, A returns A3. Q's separately
created history yields A2/A3 and B2/B3. All three persisted activity-date
comparisons were strictly increasing. No global ranking rule follows from
empty global results. EXT, R5 and R6 did not run.

Cleanup restored all twelve actor/episode UserData projections to their
complete zero baselines, retained series/season summaries and account
policy/configuration/preferences, and closed both exact recorder tokens.
Reference authentication/device history remains an owned run artifact; no
whole-reference-database equality or deletion is claimed.

## Independent closure

The [wire reconstruction](nextup-global-reference-matrix-07-independent-wire.json)
replayed all 158 actual request/response pairs through the frozen pure Matrix.
It verified the complete set of 477 private files and 159 exported matrix
files. From the 158 raw wire inputs it byte-reconstructed all 319 derived
private files and 159 exports, including request headers and bodies,
reservation states, response exports, final transport state and result.
It made zero network, subprocess or
unexpected-write attempts. The original monotonic clock was not recorded;
logical-clock replay and actual response timestamps are labeled separately.
The operator's separate export terminal is hash-bound, not independently
redaction-reconstructed by this wire verifier.

The [runtime closure](nextup-global-reference-matrix-07-independent-runtime.json)
independently verified the unit argv, invocation, exit0/MainPID0, absent former
PID and cgroup, and all 475 identity callbacks (`1 + 3 * 158`). Every callback
retains the exact deleted proxy executable display and metadata proof for the
same file object. The complete receipt chain, post-persistence hashes and fresh
metadata-only observations match. No bound reference application/proxy
executable bytes or reference database bytes were read by that verifier.
Preservation checks did hash the protected Goby binaries.

All 186 protected roots match before/after and fresh independent enumeration;
the historical 181-root set remains exact. Complete Goby state matches both
captures and the retained v7 snapshot, excluding only capture time: 83 sessions,
70 devices and 185 activity entries. The explicitly reconciled preexisting
candidate-failed/primary-running states remain unchanged during the matrix.

The worker's terminal remains `awaiting_operator_commit` with
`completionCommitted=false`. Its separate durable
[commit](nextup-global-reference-matrix-07-operator-commit.json) records
`matrix_protocol_complete`. Both independent reports retain their distinct
proof boundaries; their combined evidence closes this protocol run without
rewriting worker or historical records.

| Record | SHA-256 |
| --- | --- |
| Attestation | `c2dfa248e6823b0e9bc4aa04f9034d9ca0c872de80895f4ce5a50db32c66021a` |
| Unit file | `1d39043e34da230dff75d2902944fbcc981a5985f53485797d3f6305cf258242` |
| Operator commit | `46e5811f1b2099ad49a20a4915fe0d83bd6adeb3ea0cdd5a51d455b714ba2b18` |
| Independent wire reconstruction | `4689a670ad80efcd0334c3f1956e540a986cfbc735da8f4e6b93acd68d804d8c` |
| Independent runtime closure | `782ab2ddb4e1d979f9f93a42b3c1d9cbb9b1078413d5e8a354242ff715eeae93` |
| Preservation before | `aec42acbc472c58a1741a64a8d502d3859cad0a7a85ac7cd285885995a8f98e8` |
| Preservation after | `86b172a43e89f0dd3433831ccb3d5ec21c5a4a04c15c8b00c5bf278b6a906687` |

New setup, assembly, capture, launch and independent verification helpers were
reviewed and compiled remotely. Their actual one-shot executions establish
this checkpoint. No local tests, builds, syntax checks or runtime probes ran.
The earlier SSH signing refusal occurred before remote execution; the subsequent
successful connection did not repeat a dispatched setup operation.

## Next boundary

The matrix output and operator scope are consumed and must never be resumed.
Preserve all preceding preparation, observer, diagnostic, failed and successful
matrix scopes and the existing proxy. The next allowed reference step is a
separate bounded original-client Home/TV discovery. Its allowance is at most
two playback attempts and 1,200 seconds; an initial navigation-only discovery
can use zero playback attempts. If the client's actual global responses also
remain empty, retain the unresolved result instead of changing Goby to always
return empty or running R5/R6 without the positive gate.

The separate Goby comparison, positive client refresh gate, candidate recovery,
primary upgrade and remaining M2-M6 gates remain open. M7 remains deferred.
