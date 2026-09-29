# Automatic seek previews and expanded intro evaluation

Status: **BIF automation verified; expanded intro assessment completed, broader
recognition not accepted**. Baseline: `869c665`. See the
[delivery and assessment record](../development/bif-intro-expansion-20260930.md).

The user selected automatic BIF generation and expanded episode-intro accuracy
evaluation, using public sources. Docker remains the only delivery form. Tests,
decoding, source verification and runtime checks run on `test-env`; downloading
public inputs through the workstation is allowed when remote HTTPS is unavailable.

## Automatic preview workflow

- Add `EnablePreviewGeneration`, default false, for movie, TV and mixed video
  libraries. Enabling the option and completing a scan request background work.
- Install a daily and dedicated-event schedule only for untouched task definitions.
  Preserve administrator choices and keep intro and preview requests independent.
- A real analysis-profile change requests regeneration for enabled libraries.
  Automatic work is selected only from enabled libraries; an empty selection never
  becomes all libraries. Interrupted automatic work is requested again with current
  policy, while explicit cancellation, timeout and manual work are not replayed.
- Disabling automatic generation stops new automatic publication. Existing valid
  previews remain readable. Keep errors and progress in Tasks; remove manual
  generation as the normal UI path while retaining compatible APIs.
- Verify three BIF widths, timeline/JPEG delivery, HTTP ranges, cache reuse,
  configuration-triggered rebuilding and disable/recreation persistence in Docker.

## Finite source population and independence

Before running any detector, select twelve intact public source episodes across
four previously untested series. Separate them by series:

| Role | Series | Cases |
| --- | --- | --- |
| Calibration | The Beverly Hillbillies | B1, B2, B7 |
| Calibration | Hubblecast | H114, H116, H118 |
| Held-out series | One Step Beyond | O1, O2, O3 |
| Held-out series | NASA Space to Ground | N386, N388, N389 |

Retain original URLs, upstream metadata, license statements, actual file digests
and sizes privately. Internet Archive public-domain metadata is an uploader
declaration, not an independently established worldwide rights conclusion.
Official ESA/NASA reuse conditions and credits remain attached to those sources.
No source media is redistributed in Goby's image or public evidence package.

Use independent source-only visual review with actual presentation timestamps,
finer boundary frames when needed, and explicitly identified machine-ASR
hypotheses. Do not describe assistant review as human annotation or direct audio
listening. Preserve short title idents, episode-specific cold opens, sponsor
segments and repeated ending promotions as separate semantic observations.
Do not stretch labels to match detector thresholds. Unknown historical seasons
remain unknown; private matcher cohorts do not invent TV-season metadata.

Freeze labels and a five-second boundary tolerance before extraction/evaluation.
Use calibration failures for bounded implementation changes if needed, then
freeze the candidate before the first held-out-series detector run. Never move
held-out results into calibration while continuing to call them unseen results.
Report detected positives, misses, deliberate abstentions, false positives,
boundary errors and protected-content overlap separately. Repeated runs are not
additional independent episodes. Existing The Big Picture evidence is regression
evidence only, not part of the new-series denominator.

## Closure

The original twelve-source population was retained. Three Robin Hood episodes
were added explicitly to investigate another opening format; they also had short
openings and did not establish long-theme coverage. NASA N386 lacked audio in
both its medium and original encodes. Its extraction failure remains recorded;
the next chronological episode N390 was reviewed and frozen before the first NASA
matcher call. There are 16 reviewed original episodes, 17 media files and 15
evaluable episodes. Alternate encodes do not increase the original-episode count.

The unchanged production matcher returned no result for all 15 evaluable cases:
12 source-reviewed openings were missed and three short-ident negatives were
correctly left without markers. No broad accuracy improvement or support expansion
is accepted. Duration-only calibration experiments did not help; weaker quality
gates were not adopted. Short openings and different opening/audio versions remain
substantive recognition work. The machine-readable
[assessment](../development/intro-accuracy-assessment-20260930.json) retains this
negative result independently of the completed BIF implementation.

Retain original failed attempts and rerun only affected checks. Generate the new
recovery catalog from actual PostgreSQL 17. Build software/AMD Docker application
layers after verification, bind their archive identities in the current catalog,
close owned resources, preserve private data/evidence, and record Git publication
separately. Online scraping, new GPU qualification and historical capacity/fault
campaigns are not part of this increment.
