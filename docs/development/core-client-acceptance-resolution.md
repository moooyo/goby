# Core client acceptance resolution

Status: active on 2026-09-15 following the execution-plan review. This work
resolves concrete core-client blockers before another native capacity profile.
It preserves the full M2-M6 scope and the original failed client results.

## Target and evidence ownership

The next intended delivery artifact is the retained E11 embedded amd64 binary,
SHA256 `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`,
from the [verified internal package](systemd-package-build-verification.json).
Its [source bridge](systemd-package-source-checkpoint.json) binds 864 tracked
inputs and separate evidence for 57 generated assets. Git inspection at
`8d53e1d` found no changes from `beaea34` in the product source, module files,
administrator source or packaging. Selecting the artifact does not establish
current runtime admission or transfer old client acceptance.

The [candidate transition decision](e11-candidate-transition-decision.md) selects
the existing audited candidate A as the E11 transition target. Its established
client/catalog/actor lineage will be retained through diagnosis and final
acceptance. Candidate B remains preserved as independent embedded-candidate
evidence; no third candidate or actor reset is selected.

The [source comparison](core-artifact-source-comparison.md) now binds the actual
A/B/E11 differences: core audio/playback/authentication files are unchanged,
while E11 changes asset selection and the shared task/scan lifecycle. Its startup
can add the media-refresh task definition. Include that explicit state delta in
the candidate transition instead of assuming that unchanged migrations imply a
zero-write start.

| Artifact | Retained purpose | Remaining condition |
| --- | --- | --- |
| Audited client candidate `b0d6769...` | Selected existing target for the E11 transition; retain its diagnostic and MP3/FLAC history | Diagnosis and transition require their own reviewed current-state inputs; final acceptance must bind E11 |
| Embedded candidate `59096592...` | Preserve completed provision, seed and admission as independent evidence | It is not the selected final-client transition target; do not move/reset actors or transfer its admission to A/E11 |
| E11 package `7a681218...` | Intended next internal delivery artifact | A bounded candidate transition/admission must bind its actual state before final core-client acceptance |

Follow the [A-to-E11 state contract](e11-candidate-transition-decision.md#transition-state-contract)
to prepare one bounded transition, including the possible new media-refresh task
definition and the actual administrator asset-selection mode. This selection is
not deployment authorization. Preserve both existing instances and all consumed
history; existing configurations remain hash-only and master/key files stat-only.

The next movie baseline must use the latest complete closed-state snapshot, not
only the last movie snapshot. Subtitles01 retained two foreign audio references;
they must remain exact. The zero-residue requirement applies to the movie actor,
while every foreign row and sequence remains bound to the reviewed current
snapshot. Historical movie provenance separately fixes its actor, terminal
history and UserData; it does not replace the current full-library baseline.

## Concrete blocker queue

| Order | Known problem | Next action and decisive result | Stop condition |
| --- | --- | --- | --- |
| 1 — complete in its tool scope | Saved subtitle context291 identifies physical294, but the matcher also accepted physical293 | [35 remote guards and the exact saved 293/294 replay](core-response-identity-verification.json) passed independent review. Context291 remains associated with 294 and is excluded from 293 | No inference about why 293 ended; the failed subtitle run stays failed |
| 2 — offline contract verified | Movie controller v3 embeds movie05 state and cannot accept the later closed state | [Version-4 contract verification](reviewed-movie-baseline-verification.json) passed Python 47, JavaScript 46 and 12 log/history checks with independent review. Both implementations load actual movie05, movie06 and latest Subtitles01 baselines, preserving 35 tables, five sequences and two foreign references | New component admission remains required. The known old closer is rejected before live work; no browser input is admitted by this tool checkpoint |
| 3 — Response identified; diagnostic branch finished | [TV browse02](audited-tv-browse02-diagnostic.json) uniquely binds native-rejection-1/context262 to physical264, GET `/emby/LiveTv/Programs`, HTTP 404. Its one pageerror remains a separate unaccepted UI result | Review this exact unsupported query's product contract. The pinned 4.9.5 API snapshot omits its successful response schema; prepare one bounded authenticated query against the existing empty-library original-client host to establish the no-program response shape before choosing a real zero-EPG capability | No more TV diagnostic tooling or replay of this input. Do not turn the fallback into an unproved empty-success stub, infer full Live TV support, or waive the old errors |
| 4 | Final core acceptance does not yet bind E11 | Prepare the [selected A-to-E11 transition](e11-candidate-transition-decision.md) and its final movie/episode/subtitle journeys; close the bounded TV branch before the actual transition, without waiting indefinitely for old-error attribution. Reuse audio evidence only through its explicit source/transition bridge | Candidate selection is complete; no transition input is admitted. No main promotion before actual core acceptance and current recovery/deployment prerequisites |

Each completed action must reduce a named uncertainty or produce a concrete
source correction and its relevant evidence. If saved records cannot distinguish
a hypothesis, record the missing observable explicitly instead of repeating the
same identity review. An additional browser run must state its artifact, client
build, retained actor/state, observable, expected alternatives and original
request/time/cleanup boundaries before dispatch.

The next reference question is restricted to response shape on the existing
owned Emby 4.9.5.0 host. Reuse its checked process/listener namespace, the existing
bounded connection/response primitives, and the startup report's private
credentials descriptor. A new fixed input may contain only login, one GET
Programs, logout, and a same-token Sessions request expecting 401. The old
startup runner, wizard, consumed input and fixed route/token restrictions remain
unchanged. The empty host has no corresponding Series: omit `LibrarySeriesId`
and state explicitly that this does not prove its filtering or ordinary-user
permissions. Do not read reference source/database or environment-file content,
change egress, or build a general reference harness for this question.

If a supported zero-EPG query is selected after that evidence, specify its actual
meaning, authentication, subject/Series authorization and strict input rules
before implementation. Do not return local Episodes as television programs or
hide authorization/input/internal failures. POST Programs, channels, tuners,
DVR and Live TV playback remain deferred. Any product fix creates a successor
to E11 with its own relevant and final-source verification.

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
