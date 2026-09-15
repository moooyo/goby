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

The [source comparison](core-artifact-source-comparison.md) now binds the actual
A/B/E11 differences: core audio/playback/authentication files are unchanged,
while E11 changes asset selection and the shared task/scan lifecycle. Its startup
can add the media-refresh task definition. Include that explicit state delta in
the candidate transition instead of assuming that unchanged migrations imply a
zero-write start.

| Artifact | Retained purpose | Remaining condition |
| --- | --- | --- |
| Audited client candidate `b0d6769...` | Reproduce and explain saved movie/episode/subtitle evidence; retain its MP3/FLAC acceptance | Any further diagnostic needs a reviewed current closed-state input and a discriminating question |
| Embedded candidate `59096592...` | Retain completed provision, seed and admission | It is not E11; do not run a final acceptance sequence against it and silently transfer the result |
| E11 package `7a681218...` | Intended next internal delivery artifact | A bounded candidate transition/admission must bind its actual state before final core-client acceptance |

Do not create another candidate merely to avoid the existing state. Before a
transition, choose the existing candidate whose retained catalog/actors and
admission lineage permit the smallest justified change; preserve both existing
instances until that choice and its recovery/cleanup contract are reviewed.
Existing configurations remain hash-only and master/key files stat-only.

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
| 3 — question selected, entry contract pending | Movie/episode/subtitle page errors remain unattributed; Subtitles01 captured primitive undefined without a request association | The [TV response observation decision](core-tv-response-observation-decision.md) selects one no-playback browse using the existing native hook to seek a unique Response/request binding. Old reference movie/subtitle already recorded errors and are not queued for blind repetition | TV retained-state and component/current-runtime admission are required before execution. Undefined, no unique binding or no recurrence ends this diagnostic branch; original errors remain unresolved |
| 4 | Final core acceptance does not yet bind E11 | After the preceding decision, define one candidate transition/admission and the original-client movie/episode/subtitle sequence against the intended artifact. Reuse only unchanged, source-bound proofs | No main promotion before actual core acceptance and current recovery/deployment prerequisites |

Each completed action must reduce a named uncertainty or produce a concrete
source correction and its relevant evidence. If saved records cannot distinguish
a hypothesis, record the missing observable explicitly instead of repeating the
same identity review. An additional browser run must state its artifact, client
build, retained actor/state, observable, expected alternatives and original
request/time/cleanup boundaries before dispatch.

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
