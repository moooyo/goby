# Bounded NextUp original-client discovery closeout

Both actual client workers completed their zero-history discovery and cleanup.
Neither recorded a physical `Shows/NextUp` request. Both independent client
reconstructions passed, and a separate final preservation closeout verified
all 198 protected roots against fresh filesystem observations and complete
Goby state. These are completed bounded observations with an unresolved
positive client gate.

The [matrix07 reference observation](nextup-global-reference-matrix-07.md)
preceded these runs and restored both actors' episode history to zero. The
[client01 input contract](nextup-client-discovery-input.md) and
[client02 Suggestions continuation](nextup-client-discovery-02-input.md) define
the separate browser scopes and their shared 1,200-second ceiling.

## Actual observations

| Fact | Client01 | Client02 |
| --- | --- | --- |
| Visible journey | Home, LA TV with default Shows, Home, LB TV with default Shows | Home, LA TV, LA Suggestions, Home, LB TV, LB Suggestions |
| Navigation actions after the initial Home | 3 | 5, including one Suggestions click per library |
| Suggestions observation window | Not exercised | 20 seconds per library after the single visible click |
| Worker elapsed time | 10,586 ms | 50,592 ms |
| State-recorder requests | 34 | 34 |
| Playback attempts | 0 | 0 |
| Physical `Shows/NextUp` requests | 0 | 0 |
| Worker outcome | `actual_client_global_request_unobserved` | `actual_client_global_request_unobserved` |
| Independent client reconstruction | Passed, fourth checker receipt | Passed, first checker receipt |
| Before/after outer preservation | 192 protected roots and complete Goby state preserved | 198 protected roots and complete Goby state preserved |

The cumulative worker elapsed time was **61,178 ms**. Client02 used the remaining
1,189-second ceiling and retained the final 360 seconds for cleanup; it did not
reset the original discovery allowance. The **68 recorder requests** exclude
the browser's own traffic. Each run used two separate state-recorder logins and
one actual UI login. Across both runs, all six logins have independently verified
logout and rejection of the same token with HTTP 401. Both independent proofs
reconstructed their recorder closures and UI logout/token-rejection chains.
The UI 401 proof uses retained exact-token verification metadata, not a
separately retained raw 401 response body. Complete episode/summary UserData,
preferences and accepted profile fields match; full profiles retain the
authentication timestamps produced by these owned logins.

Client02 recorded selection of Suggestions in both libraries, but neither
20-second window supplied the missing client request. An unobserved request is
not an empty client response. These runs therefore establish neither the
client's global query parameters nor a client-side positive-selection rule.
They also do not compare client behavior against the watched states in matrix
R1-R4, exercise playback, or establish playback-driven refresh.

## Independent verification and retained failures

Client01's actual worker was not repeated. Three separate read-only checker
invocations failed and remain preserved:

1. The independent decoder lacked the Brotli dependency required by an actual
   compressed response. The dependency was installed for the subsequent check.
2. A checker classified the observed `GET /LiveTv/Channels` JSON metadata read
   as media traffic. Its narrow public metadata classification was corrected.
3. A checker expected an `/usr/bin/env` wrapper from a documentation example.
   The actual unit used `flock --no-fork` followed directly by `/usr/bin/node`,
   with `PLAYWRIGHT_BROWSERS_PATH` declared in the unit environment. The checker
   was corrected to the actual frozen command.

The fourth independent receipt passed at
`W/reference-nextup-client-discovery-execution-01/independent-client-terminal-04.json`,
SHA-256 `f123a6421aa3ff86eda190dc81bd5338b7fcdeb8ac31e09b5555f8c74cd2f001`.
Here `W` is `/opt/goby-test/exec-work-m3e`. The failed checker receipts retain
their original outcomes; checker repair did not rerun browser business actions.

The [client01 independent receipt](nextup-client-discovery-01-independent.json)
retains that result. The [client02 independent receipt](nextup-client-discovery-02-independent.json),
SHA-256 `f04cd6a1b00caf16073accc7b972b2d8d42cb1ea30dee94ff3a831104f65ce04`,
independently verifies the five actions, two selection-marker transfers and
20-second response windows, complete file sets, 34 recorder requests, cleanup,
source/runtime bindings and the cumulative 61,178 ms budget.

The [targeted visual review](nextup-client-discovery-visual-review.json)
inspected client01's default Shows and logout screens,
and both client02 Suggestions screens. Suggestions is visibly selected and
the panels show Latest Episodes; no NextUp panel is shown in those retained
screens. Client02's logout image is byte-identical to the reviewed client01
logout image. This is a targeted visual review, not review of every screenshot.
Raw screenshots, credentials, tokens and sensitive response bodies were not
published; temporary screenshot review copies remain under the local `.git`
directory. Script receipts keep their original machine-only visual-review flags.

## Preservation and next boundary

Client01's before/after preservation captures retain SHA-256
`06d1c6bc29956a01ea4866e609d26ae0a6d9ef1e4265ab261f7fcd7262988509`
and `4c7c359ba8a8c5f67451c37bc5c357bc27a17486f8e524eb21daa3d95ed8478e`.
Both runs' complete Goby comparisons preserve 83 sessions, 70 devices and 185
activity entries, excluding only snapshot capture time. The candidate's
preexisting failed state and the primary's running state remained the recorded
baselines. The larger client02 protection set includes the newly consumed
client01 scopes.

Client02's before/after SHA-256 values are
`19d10cebf550702f02c48ca24baae354928cab847e774c58451dba364cd58723`
and `2a0c21b02de7e4a8d8def62fe31e1b66deb9ebff7c2ee6fb02516d03618e09ec`.
The separate [preservation closeout](nextup-client-discovery-preservation-closeout.json),
SHA-256 `58ca2da84067beb3a8f26fd7d6bd1cb3aca71b6b54a884f2803ef7dd6af52b6d`,
re-enumerated all 198 protected roots, rechecked service and main-file identities,
and compared a fresh full Goby snapshot. It did not rewrite either client
verifier's `outerPreservationReceiptRequired` field.

The browser output, inputs and actual execution scopes are consumed. Do not
resume their journals, repeat their business operations, rewrite their results,
or restart the existing reference proxy. Further hypotheses require their own
bounded public controls. The [separate Goby comparison contract](nextup-goby-comparison-contract.md)
defines what the existing evidence can support; no global selector change
follows from these client observations. The later
[candidate recovery](candidate-source55-restart-closeout.md) has its own source,
runtime and preservation attestation. Positive global/client evidence, playback-driven refresh, primary
upgrade and the remaining M2-M6 gates remain open; M7 remains deferred.
