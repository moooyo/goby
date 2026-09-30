# Intro quality: recover the common interior of complete witnesses

Status: the third source increment is complete within its selected scope.
Baseline: `23c27d1`. It repairs the R14/R15 mixed-cohort recall regression and
passes its first new three-positive and three-negative episode evaluation.
The [result manifest](intro-quality-round3-results-20260930.json) binds the
candidate, verification and resource closure; the
[independent score](intro-quality-round3-score-20260930.json) preserves every
population, miss and scoring limit. These remain local source changes. The
accepted Docker catalog and installation artifacts have not changed.

## Selection repair

Previously, crossing visual candidate windows made a source ambiguous even
when every complete witness supported the same substantial interior. The
selector now computes each source's exact common intersection from every
original legal witness before filtering or remeasuring any recovery candidate.
A later failed audit or measurement cannot remove counterevidence and enlarge
that intersection.

Each recovery retains one original group's complete source membership, fixed
spatial geometry and clock offsets. All member intersections are projected into
that group's anchor clock, intersected with its original guarded interval, and
every edge is measured again. The existing eight-second minimum, coverage,
motion, independent-source and similarity gates still apply. Recovery does not
union windows, add support, choose new geometry or apply the 0.3-second guard a
second time. A disjoint or insufficient intersection remains ambiguous.

Only sources with an originally empty selection can recover. Existing qualified
results and group IDs remain intact. New intervals receive IDs bound to their
members and remeasured metrics. Budget exhaustion or cancellation returns no
partial result. Detector v5, execution version 5, schema 53, GAFB v3, extraction
profile and resource limits remain as defined by the preceding unreleased
increment; this repair adds no storage or measurement format.

## Results

| Population | Result | Evidence boundary |
| --- | --- | --- |
| Known Robin Hood R10-R15, alone and with N388-N390 | 6/6 safe detections; R14/R15 restored; negative additions remain unmarked | Development regression; mixed observations add no independent cases |
| Previous R16-R18 and N394-N396 holdout | 3/3 safe detections and 3/3 correct abstentions | Previously observed regression material |
| All 27 retained independent cases | 9/18 positive safe hits, 9 positive misses; 9/9 correct negative abstentions | Includes Beverly, Hubblecast and One Step Beyond misses; not an unseen estimate |
| First new R19/R20/R21 | 3/3 safe detections; maximum endpoint error 2.5935776 seconds | New episodes of a known series, source-labeled before frozen candidate execution |
| First new N398/N399/N400 | 3/3 correct abstentions | New source-reviewed short-ident negatives |

There are no measured protected-content overlaps or endpoint failures in these
published intervals. The unchanged scorer permits five seconds of error only
at the positive target endpoints. Protection against sponsor, episode-title and
narrative content is strict, with one microsecond reserved for serialization.
An empty result for a positive target remains a miss; it is not a perfect
boundary score. The maximum endpoint error across retained positives is
2.6101747 seconds.

R10-R13's complete episode results, including their selected IDs, equal the
preceding increment in both the six-source and mixed nine-source cohorts.
R14 recovers `[1.1236807, 13.5116945]` seconds; R15 recovers
`[1.2210606, 13.6138138]`. The measured work is 141,037,455 comparisons for the
six-source cohort and 152,553,901 for the mixed nine-source cohort, within the
unchanged 200-million limit. These cases do not establish general capacity.

## Independent evaluation and corpus operation

Selection, original source SHA-256 values, source-only labels and candidate
source/binary digests were frozen before the first new matcher run. All six
sources extracted successfully with 1,200 refinement observations each. Resumes
preserved every feature and checkpoint byte, with one attempt per source.
Both complete cohorts ran without exclusions, changed options or label edits.
The corpus inventory adapter retains the six original source identities and
records its separate digest alongside the original inventory digest.

Robin sources use the metadata-listed 512Kb MPEG4 rendition selected uniformly
for size before acquisition or viewing. Their resolution and timing differ
from earlier encodings. Bounded official discovery did not locate N397; the
documented N400 replacement was selected before acquisition, labeling or
matching. N397 is not counted as evaluated. N400's metadata music-license
statement is retained without inferring rights to the entire clip. Source
media and original rights metadata remain private and are not redistributed.

Two fresh-context source reviewers assigned labels from sampled actual-PTS
frames without detector outputs or thresholds. The preparation coordinator
accidentally saw previous algorithm and score summaries; that exposure is
disclosed, and the coordinator mechanically retained the reviewers' labels.
This is assistant visual assessment, not human annotation or direct audio
listening. A receipt correction repaired the digest of a still-being-written
log; original receipts and the correction remain available. Labels, source
identities, selection and reviewer reports did not change.

The independent scorer remained byte-identical to the preceding increment.
Eight retained cohorts contain 36 observations but only 27 distinct cases;
the mixed cohort adds no cases. All retained cases are development material.
The new six are reported separately, without turning subsequent repeats into
new independent evidence. Original N388-N390 labels lack source SHA-256 fields;
the other 24 retained cases and all six new cases have label/source identity
correspondence. Sampled labels and the finite, nonrandom population limit the
protection and generalization claims.

## Remaining misses and next work

Beverly Hillbillies, Hubblecast and One Step Beyond still contribute nine
positive misses. Their original semantic targets remain unchanged. A private,
finite Beverly diagnostic fixed B1's neutral geometry and tried 540 global B2
geometries across scale, crop and shift. None met the necessary number of
independent matching frames: the maximum was six on B1 and 21 on B2, below the
68 required by an eight-second interval at current cadence and coverage.
This rejects that specific grid expansion as a useful immediate repair; it
does not prove that every descriptor or transform must fail.

The next useful recognition task is a bounded descriptor/scene-correspondence
investigation for the Beverly version mismatch, followed by a newly frozen
episode evaluation if it produces an implementable improvement. Expanding the
same geometry grid is not supported by this diagnostic. Hubble's short bumper
must not be extended across an episode-specific title, and One Step Beyond's
static presenter is still insufficient moving evidence. Neither case should
be converted into a successful negative to improve the score.

## Verification and closure

All formatting, tests, builds, decoding and runtime checks ran on `test-env`.

| Verification | Result |
| --- | --- |
| Final detector package with race | 157 parent passes, no failures or skips |
| New real PostgreSQL integration with race | GAFB refinement cache, actual crossing-witness discovery, recovery, publication and playback passed |
| Pre-fix integration control | The same test with the `23c27d1` selector fails with `competing_intervals`, reproducing the defect |
| Retained public corpus | Eight sequential cohorts completed; their frozen production source hashes matched before and after execution |
| New actual-media corpus | Six extractions and exact resumes passed; two first analyses and independent strict scores passed |
| Builds | Final application and corpus development builds passed |

The integration fixture uses synthetic raster observations to exercise the
complete storage/publication path; actual decoding is established separately
by the six-source extraction. Existing unchanged media, migration, backup,
server/task and corpus-unit evidence retains its previous scope; no historical
capacity, fault or GPU matrix was rerun or claimed as newly passed.

The owned PostgreSQL cluster on port 56000 stopped after zero client sessions;
no owned containers remain. Source media, databases and all failed/accepted
evidence are retained. Two redundant source-transport archives were removed
only after remotely verifying byte-identical local recovery copies. Shared
build caches and unrelated services were not cleaned. The original checkout's
unrelated changes remain untouched. No merge, push, image release or deployment
is included.
