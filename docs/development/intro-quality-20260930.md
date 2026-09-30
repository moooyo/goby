# Intro quality and resumable corpus evaluation

Status: the selected implementation and focused verification are complete;
recognition improvement remains narrow. Baseline: `cfa12ff`.
The existing schema-52 Docker release catalog is unchanged by this source work.
No replacement Docker image, registry publication or production deployment is
claimed. The [result manifest](intro-quality-results-20260930.json) records the
source, evidence and remaining recognition limits.
The [independent corpus score](intro-quality-corpus-score-20260930.json) keeps
first evaluations, repeats and the mixed-support follow-up separate.

## Recognition policy

The v4 matcher retains the acoustic/visual pipeline and adds a conservative
visual fallback when that pipeline produced no group. A qualified fallback
requires a complete pairwise witness from at least three independent episodes.
Aliases remain a transitive union of episode, source and content identity.

The additional 8 by 8 normalized-luma descriptor complements the existing dHash.
Both the whole image and its center must agree, so a repeated studio background
cannot alone corroborate different foreground content. Dynamic anchors nominate
fixed source-clock offsets. Every admitted group is remeasured on its final
common interval, including unmatched samples and gaps. Static images, strong
periodic loops, ambiguous competing intervals and limited searches cannot grant
automatic visual publication.

Several complete support groups can describe the same opening. The publication
step selects one existing witness per episode when their windows are the same
or nested. It never combines support from incomplete groups or unions intervals.
Crossing or disjoint windows retain `competing_intervals` and no marker. This
repairs an initial implementation that treated every repeated support group as
ambiguity, even when its opening interval was consistent.

The visual policy is deliberately narrow: the first 120 seconds or first half of
the episode, whichever is shorter; 8 to 90 seconds of repeated content; at least
85 percent matched coverage, four visual states and three transitions. It does
not extrapolate the common sequence into a sponsor, an episode-specific title,
or narrative. Full-frame and central normalized RMS limits are 550 and 650
permille, respectively; corresponding samples also retain a dHash distance gate.
The largest allowed gap is 2.1 seconds, accounting for differing sampled phases
across fast cuts without filling longer unobserved sections.

Visual evidence is stored explicitly as `VisualEvidence`; audio metrics remain
zero for this route. Admission rechecks complete support identities, group and
candidate agreement, execution version, and actual source-duration limits.
Historical v3 admissions/results retain frozen readers and cannot be upgraded
into current execution authority. GAFB v2 stores the additional descriptor;
GAFB v1 remains readable with no invented descriptor. The extraction profile
changes, so old feature cache identities cannot masquerade as new extraction.

`AnalyzeWithDiagnostics` adds bounded stage observations to the same analysis
execution. It distinguishes missing nominations, short runs, evidence rejection,
visual confirmation and complete groups. Trace truncation does not truncate
aggregate counts. Diagnostics are not an accuracy score or publication authority.

## Corpus tooling

The [corpus tool](../../scripts/test-env/intro-corpus.md) persists per-original
extraction attempts and validates source, label, extractor and feature identities
before reuse. Missing required streams become explicit exclusions; other failures
remain failures, and neither category counts as a successful negative result.
Incomplete, stale or insufficiently independent cohorts do not run the matcher.
The optional visual report retains its separate discovery observations.

## Evaluation results and boundary

The previous 15 originals are development/regression material. They cannot be
relabeled unseen holdout data. Their production v3 baseline had zero detections
among 12 source-reviewed positive openings. The v4 candidate detects the three
Robin Hood openings within the predeclared five-second boundary tolerance; the
other nine positive originals remain missed. All three original NASA short-ident
negatives remain without candidates. The largest measured Robin boundary error
is approximately 2.004 seconds, ending early rather than extending into narrative.

Three naturally subsequent Robin Hood episodes and three naturally subsequent
NASA episodes were selected independently. Source-only labels were frozen before
their first candidate runs. These are new episodes of known series, not new-series
generalization or random population accuracy. Labels disclose assistant visual
review, frame sampling and boundary uncertainty; they are not human annotation
or audio review.

| Population and role | Recorded outcome | Interpretation |
| --- | --- | --- |
| Previous expanded corpus, 12 positives and 3 negatives | 3 detections, 9 misses, 3 correct abstentions, no observed false positives | Development/regression improvement only |
| New R13/R14/R15 evaluated as a three-episode cohort | 0 detections, 3 misses | First held-out positive evaluation failed; it remains failed after the publication repair |
| New N391/N392/N393 short-ident negative cohort | 3 correct abstentions, no observed false positives | First held-out negative result; the final-candidate repeat does not add independent samples |
| Supplemental six-episode Robin support pool after inspecting the failed result | R12/R14/R15 qualified; R10/R11 retained competing-window abstentions; R13 missed | Follow-up regression/diagnostic evidence, not another unseen accuracy trial |

R13 does not have enough continuous descriptor agreement with both other new
episodes. Increasing comparison budget or repairing duplicate support selection
does not resolve that limitation. No threshold was relaxed after seeing the new
holdout. Broader short-opening and differing-version recognition remains open.
Future work should study geometry or scene-alignment robustness with another
independently frozen evaluation, while retaining these original misses.

The first attempted positive-cohort container could not open the UID-10001
checkpoint when launched as capability-dropped UID 0. The runner was corrected
to use the extraction owner; the original failed attempt is retained. The matcher
did not run in that failed preparation attempt.

## Verification and delivery

All tests, formatting, media decoding, builds and runtime checks used `test-env`.

| Scope | Result | Boundary |
| --- | --- | --- |
| Final detector package | 129 parent passes with the race detector | Includes bounded diagnostics, adversarial visual evidence and publication-selection regressions |
| Media package, composed ordinary and actual Docker execution | 531 unique parent passes, 32 explicit skips | The actual media subset had 79 passes and no skips; unrelated hardware and external profiles remain outside this increment |
| Corpus operator | 14 parent behavior tests, race checks passed | Real Docker missing-audio exclusion, extraction, exact resume and full-case accounting also passed |
| Library analysis | 94 unique parent passes | Includes the added durable feature-cache to real matcher to publication/playback test; two affected parents were repeated |
| Backup and recovery | 12 backup and 3 recovery parent passes | v1-v3 history, v4 admission/result relations, restore and current-authority separation |
| Server and tasks | 57 server and 30 task parent passes | Actual PostgreSQL and bounded Docker workers; source scope precedes the publication-only repair |
| Final source builds | Application and corpus binaries built successfully | Development build evidence; no new supported Docker artifact was produced |

Repeated checks are not added to the unique counts. Earlier failures and source
scopes remain in the manifest. Original corpus media, private checkpoints and
source labels are retained outside the repository. No unrelated workstation
changes were included in this increment.

Owned verification containers and networks are absent. The private PostgreSQL
instance was stopped after confirming zero clients; its data, original media,
failed attempts and verification evidence remain retained.
