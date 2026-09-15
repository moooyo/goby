# Core client acceptance resolution

This document records core-client decisions, evidence ownership and acceptance
requirements. The [immediate queue](../planning/current-execution-plan.md#immediate-queue)
is authoritative for execution order and admission; [current status](current-status.md)
records active work and completed results. This is not a second execution queue.
The full M2-M6 scope and original failed client results are preserved; M7 is deferred.

## Target and evidence ownership

The retained delivery baseline is the E11 embedded amd64 binary,
SHA256 `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`,
from the [verified internal package](systemd-package-build-verification.json).
Its [source bridge](systemd-package-source-checkpoint.json) binds 864 tracked
inputs and separate evidence for 57 generated assets. Git inspection at
`8d53e1d` found no changes from `beaea34` in the product source, module files,
administrator source or packaging. Selecting the artifact does not establish
current runtime admission or transfer old client acceptance. The
[Programs query increment](live-tv-programs.md) changes product source, so an
artifact containing it needs its own verified source/build identity. Assign
binary and package identities from actual outputs; E11 retains its original
accepted installation scope. Actual successor bindings belong in current status.

The [candidate transition decision](e11-candidate-transition-decision.md) selects
the existing audited candidate A for one direct transition to the Programs successor. Its established
client/catalog/actor lineage will be retained through diagnosis and final
acceptance. Candidate B remains preserved as independent embedded-candidate
evidence; no third candidate or actor reset is selected.

The [source comparison](core-artifact-source-comparison.md) records the compared
A/B/E11 differences: core audio/playback/authentication files are unchanged,
while E11 changes asset selection and the shared task/scan lifecycle. Its startup
can add the media-refresh task definition. Include that explicit state delta in
the candidate transition instead of assuming that unchanged migrations imply a
zero-write start. That comparison remains evidence for A/B/E11; the Programs
successor needs its own final source/build bridge.

| Artifact | Retained purpose | Reuse or admission boundary |
| --- | --- | --- |
| Audited client candidate `b0d6769...` | Selected existing target for one direct Programs-successor transition; retain its diagnostic and MP3/FLAC history | Recovery and final journey entry contracts must be ready before the transition; final acceptance binds the actual successor |
| Embedded candidate `59096592...` | Preserve completed provision, seed and admission as independent evidence | It is not the selected final-client transition target; do not move/reset actors or transfer its admission to the Programs successor |
| E11 package `7a681218...` | Retained accepted package/build baseline for the Programs successor | A new product build and explicit evidence-reuse bridge are required; do not relabel E11 as containing the new route |
| Programs successor | The frozen [focused scope](live-tv-programs-focused-verification.json) has 14 top-level and 118 subtest passes with independent review | Final-source verification, actual artifacts and transition prerequisites have separate evidence. Use [current status](current-status.md#current-gates) and the authoritative queue for their progress |

Follow the [A-to-successor state contract](e11-candidate-transition-decision.md#transition-state-contract)
to prepare one bounded transition, including the possible new media-refresh task
definition and the actual administrator asset-selection mode. This selection is
not deployment authorization. Preserve both existing instances and all consumed
history; existing configurations remain hash-only and master/key files stat-only.

Every transition requires its reviewed recovery route and typed final-journey
entry contracts. Their completion and any permitted parallel work are recorded
in the authoritative queue. Python/JavaScript tool changes alone do not invalidate
Go verification. Old-binary task reconciliation, including any refresh-definition
disablement, must be explicit in the recovery contract; do not assume automatic rollback.

Each movie baseline must use the admitted complete closed-state snapshot, not
only the last movie snapshot. Browse02 records a historical checkpoint of 22
sessions, 14 plays, six UserData rows and two retained foreign audio references.
It is not fresh authority after a later admission or incident. The zero-residue
requirement applies to the movie actor,
while every foreign row and sequence remains bound to the reviewed current
snapshot. Historical movie provenance separately fixes its actor, terminal
history and UserData; it does not replace the current full-library baseline.

## Evidence checkpoints and acceptance requirements

| Checkpoint | Retained evidence | Boundary |
| --- | --- | --- |
| Response identity correction | [35 remote guards and the exact saved 293/294 replay](core-response-identity-verification.json) passed independent review. Context291 binds physical294 and is excluded from physical293 | This corrects the matcher; it does not explain why 293 ended or waive the failed subtitle run |
| Movie v4 state contract | [Python 47, JavaScript 46 and 12 log/history checks](reviewed-movie-baseline-verification.json) passed independent review using actual Movie05, Movie06 and Subtitles01 records | The recorded 35-table/five-sequence/two-reference contract is component evidence, not live admission or a contract for every other scenario |
| Programs diagnostic, reference and focused scope | [TV browse02](audited-tv-browse02-diagnostic.json), the [four-request reference observation](reference-programs-verification.json) and the [focused product result](live-tv-programs-focused-verification.json) retain their independent reviews | Inputs are consumed. Reuse only their exact scopes; no full Live TV or final client acceptance follows |

| Gate | Required result | Boundary |
| --- | --- | --- |
| Candidate transition | Verified actual source/artifacts, admitted current state and runtime, typed journey inputs, and the [bounded transition/recovery contract](e11-candidate-transition-decision.md) | Preserve A's external asset override and declared state deltas; no intermediate original-E11 deployment, actor reset or assumed rollback |
| G1 | Declared movie, episode and external SRT/VTT journeys on the admitted successor, plus the explicit audio reuse bridge | Each journey uses its preceding complete closeout. Historical failures retain their original scope |
| G2 | The [accepted E11 internal installation slice](internal-amd64-installation-acceptance.json), with explicit applicability review for a successor | Retain the original failed sealer and composite acceptance; a changed artifact does not automatically inherit every claim or require an unchanged installer replay |
| G3 | Actual core acceptance plus applicable current preservation/recovery prerequisites and the bounded upgrade workflow | No main promotion from component checks, metadata or owned-state closure alone |

Each completed action must reduce a named uncertainty or produce a concrete
source correction and its relevant evidence. If saved records cannot distinguish
a hypothesis, record the missing observable explicitly instead of repeating the
same identity review. An additional browser run must state its artifact, client
build, retained actor/state, observable, expected alternatives and original
request/time/cleanup boundaries before dispatch.

The completed reference question was restricted to response shape on the existing
owned Emby 4.9.5.0 host. It reused the checked process/listener namespace, existing
bounded connection/response primitives, and the startup report's private
credentials descriptor. The one consumed input contained login, one GET
Programs, logout and same-token Sessions; actual statuses were 200, 200, 204 and 401. The old
startup runner, wizard, consumed input and fixed route/token restrictions remain
unchanged. The host has no corresponding Series, so the request omitted
`LibrarySeriesId`. This does not prove its filtering or ordinary-user
permissions. Do not read reference source/database or environment-file content,
change egress, or build a general reference harness for this question.

The selected zero-EPG query's meaning, authentication, subject/Series
authorization and strict input rules are defined in its product contract.
It does not return local Episodes as television programs or hide
authorization/input/internal failures. POST Programs, channels, tuners,
DVR and Live TV playback remain deferred. This product change creates a successor
to E11 with its own relevant and final-source verification.

The earlier [user-interrupted ordinary run](live-tv-programs-full-interruption.json)
completed 341 tests across 10 packages, with zero failures/skips and no build in
that attempt. Its additional 109 raw identity passes are excluded. This historical
partial result is separate from focused verification and the
[later final-regression failure and incident](programs-final-regression-incident.json).
That incident introduces recovery prerequisites; their resolution and subsequent
execution belong in the authoritative queue/status, not a worker plan here.
Remaining M2-M6 profiles keep their own prerequisites; M7 remains deferred.

## Retained subtitle and movie interpretation

The [saved response-identity review](audited-subtitles-client01-review.md#read-only-response-identity-review-2026-09-14)
establishes that context291 has physical294's response `X-Request-Id`, while
physical293 has a different ID. The old 3507.211510 ms comparison remains a
historical failed checker result; it is not a proven product cancellation delay.
Those saved records lack physical293's matching browser response/cancellation
evidence and do not identify the two separate primitive-undefined page errors.

The [new saved metadata readback](core-response-identity-verification.json) also
found that Movie05 context286 has physical289's response ID, not physical288's,
and context336 contradicts physical342's response ID. The older saved-movie
replay's six-partial-response acceptance is bound to its original matcher; it
does not transfer to the corrected source. Preserve its original report and
independent database/owned-state evidence. A current movie interpretation must
resolve the unmatched physical attempts rather than restore the false pairing.

## Execution and verification boundary

Use `ssh test-env` for every test, syntax check, saved-evidence replay and runtime
verification. Keep raw evidence and credentials on the remote host. A changed
offline matcher needs its focused guards and actual saved-record replay, not a
new full product regression or business request. Review the resulting source
diff and receipt independently before integrating it into a future live input.
The old frozen runtime inputs remain consumed and unchanged.

The verified v4 movie contract points to an explicitly reviewed baseline document. That document
separately binds current closeout/snapshot/manifest/source epoch and the four
movie-provenance pins. Prepared expiration permissions are explicit; they are
not discovered and authorized from a snapshot automatically. The first saved
replay exposed an incorrect nested-field path, which was corrected in both
languages and reverified against the original records. Preserve that failed
attempt as well as the passing follow-up.
