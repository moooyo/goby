# Intro quality: visual failure diagnostics and version-mismatch evidence

Status: complete within the selected diagnostic and tooling scope.
Baseline: `4957fe4`. This increment makes the corpus evaluation explain the
production visual fallback and narrows the Beverly mismatch investigation.
It does not change recognition thresholds or claim an increase in recall.
The [verification manifest](intro-quality-round4-results-20260930.json) binds
the implementation, retained-corpus equivalence and private diagnostic evidence.

## Production visual diagnostics

The previous `AnalyzeWithDiagnostics` trace covered the audio-led pipeline.
When the visual fallback returned no group, that trace could not distinguish
insufficient independent sources from failed calibration, missing pair
hypotheses, incomplete group support or rejection at publication. The corpus
report now includes `Diagnostics.Visual`, observed during the actual fallback.

The report distinguishes considered, attempted, completed and skipped work.
Calibrated and coarse branches each retain their independent/eligible source
counts, profile quorum, completion and discovered groups. Stage entries record
calibration, completed pair lookups, complete-group search, boundary guards and
ambiguous-source recovery. These are observed stage outcomes: an empty group
search does not invent a more specific low-level cause.

The visual trace has a separate limit of 128 entries. Aggregate counts continue
after truncation. Pair lookups include cache reuse, with cache hits identified
separately; they are not counts of unique measurements. The collector adds no
candidate search, measurement or comparison-budget charge. Public research
discovery and the corpus operator's optional visual experiment remain separate
from production diagnostics. Completed branch observations can survive an
overall failure, but never authorize a partial publishable result.

The normal `Analyze` API, candidate identities, measurements, extraction
profile, detector v5, schema 53 and GAFB v3 retain their existing behavior.
The [corpus operator guide](../../scripts/test-env/intro-corpus.md) documents
the new trace and its interpretation.

## Beverly source evidence

Independent source-frame inspection confirms shared frontal driving footage in
B1/B2/B7. The same occupants, vehicle, furniture and sequence of gestures recur.
However, credit overlays do not follow the same timing as the underlying
actions. The sources also differ in head/tail edits, tone and framing.

| Corresponding action phase | B1 PTS seconds | B2 PTS seconds | B7 PTS seconds |
| --- | ---: | ---: | ---: |
| Rear-left woman lowers her hat | 12.760634 | 12.260217 | 6.505421 |
| Rear-left woman holds her hat and turns | 16.763970 | 16.263553 | 11.009174 |
| Front-left occupant points toward image-left | 19.516264 | 19.266055 | 13.511259 |

These are approximate action correspondences, not pixel-identical frame labels.
No middle hard cut or mirror was observed in the reviewed frontal sequence.
The approximately 0.25-second difference in anchor spans is comparable to
action-phase uncertainty; a precise speed ratio is not established. B7 is
brighter and has a different encoded aspect ratio. A clock derived from matching
credit text must not automatically be treated as the clock of the moving scene.
This was assistant source-only visual inspection, without audio listening or
detector output. No new source media was acquired.

## Bounded descriptor experiments

The private experiments reused the retained 16-by-16 rasters with the fixed
neutral transform. Three alternatives were fixed before execution: cell
midrank, local-mean residual and signed spatial gradient. NASA narrative and
One Step Beyond presenter windows were tested alongside Beverly. All results,
including a corrected half-open-window/sparse-reporting attempt, remain retained.

B1/B2's normalized full-image luminance gate is the immediate bottleneck in
this experiment: 271/191 frames pass usability, and many pairs pass the hash,
but the luminance gate permits only three frame pairs involving two independent
B1 frames. Removing hash or the center gate does not improve that joint result.

The table reports the loose necessary distinct-frame upper bound followed by
the best eight-second coverage on the fixed 50 ms offset grid. This is not a
continuous-search upper bound or a production recognition result.

| Pair | Current luminance | Midrank | Local residual | Spatial gradient |
| --- | ---: | ---: | ---: | ---: |
| B1/B2 | 2 / 11 per mille | 0 / 0 | 78 / 290 | 113 / 500 |
| B1/B7 | 86 / 488 | 60 / 406 | 122 / 581 | 138 / 755 |
| B2/B7 | 164 / 770 | 159 / 701 | 143 / 827 | 183 / 850 |

Gradient correspondence improves in this limited search but does not yield a
complete three-source result. The B2/B7 coverage winner has no original-hash
transitions; that says nothing about every other possible path. Original
periodicity checks also remain part of this diagnostic, so this is not a
fully calibrated replacement metric. All four appearance representations have
zero joint matches in the NASA control pairs. Static OSB can have perfect
appearance coverage while retaining zero jointly moving pairs, reinforcing the
need to preserve dynamic evidence requirements.

One additional fixed mechanism tested whether subtracting each source's own
frame from 500 ms earlier could remove stationary overlays. Signal gates were
fixed beforehand: raw-difference RMS and pooled standard deviation must each
reach two eight-bit gray levels. No lag, geometry or threshold sweep followed.
The joint independent-frame bounds for B1/B2, B1/B7 and B2/B7 were 39/69/62;
best grid coverage was 209/383/476 per mille. Hash removed no further support.
Two pairs fail even the necessary 68-frame bound for eight seconds before
additional measurement padding, so a different clock alone cannot repair this
fixed representation. Descriptor expansion stopped at that point.

The same-action error grids affect both center and outer cells. They do not
establish that text contaminating global normalization is the sole cause.
Overlay changes, approximate temporal correspondence, photometric differences
and low-resolution pooling remain confounded. Numerically reusing distance
thresholds does not establish equivalent distributions or false-positive rates
for a new descriptor; none of these alternatives entered the product.

## Verification scope and next work

Focused tests cover actual calibrated/coarse discovery, independent/profile
quorum, rejection stages, crossing-window recovery, retained ambiguity, cache
reuse, trace truncation, cancellation and budget failure. The retained corpus
comparison checks entire result objects, including IDs and comparison counts,
against the preceding source increment. All 33 retained episodes are already
development material; mixed-cohort repeats add no independent cases. Existing
misses remain misses. There is no new held-out accuracy claim this round.

| Verification | Result |
| --- | --- |
| Detector package with race | 163 parent tests passed; no failures or skips |
| Corpus operator with race | 15 parent tests passed; no failures or skips |
| Retained-corpus transparency | Ten cohorts, 42 observations and 33 distinct episodes; complete results and comparison counts equal the preceding baseline |
| Actual corpus operator in Docker | Beverly admission and analysis passed; separate visual experiment left the four-entry production trace unchanged; checkpoint bytes unchanged |
| Builds | Application and corpus development builds passed |

Beverly's production trace now exposes one rejected calibration, one pair with
no hypothesis and an anchor with insufficient pair support. Robin's six-source
and mixed cohorts both record two recovered ambiguous sources. This explains
observed execution without changing the previous 12 safe detections, 12 correct
negative abstentions or nine positive misses across the 33 retained cases.

All formatting, tests, builds, source decoding and verification run on
`test-env`. No database migration or persistence change requires another
PostgreSQL campaign. Existing stopped clusters, original media and historical
evidence remain retained. Seven redundant transport archives were reclaimed
only after remote byte comparison with local recovery copies; authoritative
source trees and shared caches were left intact.

The next recognition experiment should isolate spatial detail and overlay
effects with a bounded higher-resolution, region-preserving representation.
It should follow scene motion rather than shared credit lettering and retain
independent foreground and static-scene controls. A viable candidate still
needs new frozen episode originals before first evaluation. Simply loosening
global similarity thresholds or changing the clock has no support from this
round's evidence. The accepted Docker catalog is unchanged; no image release,
merge, push or deployment is included.
