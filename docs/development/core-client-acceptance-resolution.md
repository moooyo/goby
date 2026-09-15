# Core client acceptance resolution

Status: active on 2026-09-15 under the revised delivery order. The current target
is the Programs successor, with no binary or package identity assigned yet. This work
resolves concrete core-client blockers before another native capacity profile.
It preserves the full M2-M6 scope and the original failed client results.

## Target and evidence ownership

The retained delivery baseline is the E11 embedded amd64 binary,
SHA256 `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`,
from the [verified internal package](systemd-package-build-verification.json).
Its [source bridge](systemd-package-source-checkpoint.json) binds 864 tracked
inputs and separate evidence for 57 generated assets. Git inspection at
`8d53e1d` found no changes from `beaea34` in the product source, module files,
administrator source or packaging. Selecting the artifact does not establish
current runtime admission or transfer old client acceptance. The
[Programs query increment](live-tv-programs.md) now changes product source, so
the next candidate must be a newly verified and built successor. No successor
binary or package identity is assigned until those artifacts exist; E11 retains its original
accepted installation scope.

The [candidate transition decision](e11-candidate-transition-decision.md) selects
the existing audited candidate A for one direct transition to the Programs successor. Its established
client/catalog/actor lineage will be retained through diagnosis and final
acceptance. Candidate B remains preserved as independent embedded-candidate
evidence; no third candidate or actor reset is selected.

The [source comparison](core-artifact-source-comparison.md) now binds the actual
A/B/E11 differences: core audio/playback/authentication files are unchanged,
while E11 changes asset selection and the shared task/scan lifecycle. Its startup
can add the media-refresh task definition. Include that explicit state delta in
the candidate transition instead of assuming that unchanged migrations imply a
zero-write start. That comparison remains evidence for A/B/E11; the Programs
successor needs its own final source/build bridge.

| Artifact | Retained purpose | Remaining condition |
| --- | --- | --- |
| Audited client candidate `b0d6769...` | Selected existing target for one direct Programs-successor transition; retain its diagnostic and MP3/FLAC history | Recovery and final journey entry contracts must be ready before the transition; final acceptance binds the actual successor |
| Embedded candidate `59096592...` | Preserve completed provision, seed and admission as independent evidence | It is not the selected final-client transition target; do not move/reset actors or transfer its admission to the Programs successor |
| E11 package `7a681218...` | Retained accepted package/build baseline for the Programs successor | A new product build and explicit evidence-reuse bridge are required; do not relabel E11 as containing the new route |
| Programs successor | Source implementation checkpoint; 14 top-level and 118 subtest passes independently reviewed. Full regression was safely interrupted for user review; no binary/package exists | Prepare recovery and final journey entry contracts in parallel with frozen-Go-source verification/build; both must complete before one A transition |

Follow the [A-to-successor state contract](e11-candidate-transition-decision.md#transition-state-contract)
to prepare one bounded transition, including the possible new media-refresh task
definition and the actual administrator asset-selection mode. This selection is
not deployment authorization. Preserve both existing instances and all consumed
history; existing configurations remain hash-only and master/key files stat-only.

The recovery route and final episode/subtitle entry contracts are not complete.
Complete them before replacing A. Pure-tool preparation may proceed in parallel
with verification/build of frozen Go source; Python/JavaScript tool changes alone
do not invalidate that Go verification. Returning to the old binary would disable the new refresh task
definition; that effect must be addressed by a concrete recovery plan, not
described as an already implemented automatic candidate-transition rollback.

The next movie baseline must use the latest complete closed-state snapshot, not
only the last movie snapshot. Browse02's current saved state has 22 sessions,
14 plays, six UserData rows and two retained foreign audio references;
they must remain exact. The zero-residue requirement applies to the movie actor,
while every foreign row and sequence remains bound to the reviewed current
snapshot. Historical movie provenance separately fixes its actor, terminal
history and UserData; it does not replace the current full-library baseline.

## Concrete blocker queue

| Order | Known problem | Next action and decisive result | Stop condition |
| --- | --- | --- | --- |
| 1 — complete in its tool scope | Saved subtitle context291 identifies physical294, but the matcher also accepted physical293 | [35 remote guards and the exact saved 293/294 replay](core-response-identity-verification.json) passed independent review. Context291 remains associated with 294 and is excluded from 293 | No inference about why 293 ended; the failed subtitle run stays failed |
| 2 — offline contract verified | Movie controller v3 embeds movie05 state and cannot accept the later closed state | [Version-4 contract verification](reviewed-movie-baseline-verification.json) passed Python 47, JavaScript 46 and 12 log/history checks with independent review. Both implementations load actual movie05, movie06 and latest Subtitles01 baselines, preserving 35 tables, five sequences and two foreign references | New component admission remains required. The known old closer is rejected before live work; no browser input is admitted by this tool checkpoint |
| Complete — diagnostic, reference and focused product scope | [TV browse02](audited-tv-browse02-diagnostic.json) and the [four-request reference observation](reference-programs-verification.json) are consumed. The [Programs focused result](live-tv-programs-focused-verification.json) has 14 top-level and 118 subtest passes with independent review | Reuse their exact source/evidence scopes; no further reference, focused or diagnostic run is queued without an invalidating change | Full Live TV and client acceptance are not claimed; original failures remain unchanged |
| 1. Recovery and journey entry preparation | Final episode/subtitle contracts and a concrete transition recovery route are not ready | Complete the [successor transition prerequisites](e11-candidate-transition-decision.md), including old-binary task-reconciliation effects and each actor's retained-state allowance | Do not replace A or keep a changed candidate live while developing these contracts |
| 2. Frozen-source verification and build, parallel with pure-tool preparation | The [full run](live-tv-programs-full-interruption.json) was user-interrupted and safely closed: 10/25 packages, 341 passes, 0 failures, 0 skips; 109 partial identity passes excluded; no build | Use one planned worker for all 25 ordinary packages and the ordinary build, then append one embedded amd64 systemd package build in that same worker. Bind actual outputs after execution | The planned worker has not run; no binary/package or full pass exists. Tool-only edits do not require Go full-suite repetition; do not relaunch a consumed input |
| 3. One A transition | A is selected; the successor is not built or admitted | Switch A directly once to the verified Programs successor, preserving its external asset override and declared state deltas | No intermediate original-E11 deployment and no assumed automatic rollback |
| 4. Final client acceptance | Movie, episode and subtitles remain open | Execute the three declared journeys on the admitted successor and close the explicit audio reuse bridge, using each preceding closeout's current state | No stale or zero-history baseline; new failures retain their own scope and closure |
| 5. G3 | Main promotion remains open | Bind the accepted successor to current preservation/recovery prerequisites and the bounded upgrade workflow | No main promotion before actual core acceptance and applicable safety prerequisites |

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

The accepted focused result is separate from the interrupted ordinary suite.
The latter completed 341 tests across 10 packages; its additional 109 raw
identity passes are incomplete package evidence and are not added to that total.
No successor build occurred. The next work follows the ordered queue above;
remaining M2-M6 profiles progress under their own prerequisites and M7 remains deferred.

## Current subtitle interpretation

The [saved response-identity review](audited-subtitles-client01-review.md#read-only-response-identity-review-2026-09-14)
establishes that context291 has physical294's response `X-Request-Id`, while
physical293 has a different ID. The old 3507.211510 ms comparison remains a
historical failed checker result; it is not a proven product cancellation delay.
The current gap is physical293's missing matching browser response/cancellation
evidence. The two primitive-undefined page errors remain separate and unresolved.

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

The v4 input points to an explicitly reviewed baseline document. That document
separately binds current closeout/snapshot/manifest/source epoch and the four
movie-provenance pins. Prepared expiration permissions are explicit; they are
not discovered and authorized from a snapshot automatically. The first saved
replay exposed an incorrect nested-field path, which was corrected in both
languages and reverified against the original records. Preserve that failed
attempt as well as the passing follow-up.
