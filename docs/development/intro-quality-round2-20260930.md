# Intro quality: fixed spatial refinement

Status: the second recognition increment is complete within its selected scope
and passed its first independent six-episode evaluation. Baseline: `7fb7caa`;
detector v5, execution version 5, schema 53 and GAFB feature codec v3. Final
verification and owned-resource closure are recorded in the
[result manifest](intro-quality-round2-results-20260930.json) and
[independent score](intro-quality-round2-score-20260930.json). The existing Docker
release catalog is unchanged; this source increment is not a new image or
production deployment.

## Behavior and evidence

The new visual route uses a separate 16 by 16 grayscale observation every
100 milliseconds within the first 120 seconds. The original audio and coarse
500-millisecond visual streams remain unchanged. Each episode uses one fixed
spatial transform for its entire observed prefix; individual frames cannot
choose a different transform to hide conflicting content. The finite geometry
grid allows small vertical scale and horizontal/vertical shift adjustments
inside a fixed central crop. Every group retains one canonical source, a
consistent clock map and complete pairwise evidence from independent episodes.

The full geometry and clock audit is included in diagnostic output. Its digest
and the closed `MeasurementPolicy` are persisted with the candidate and bound
into group identity. Calibrated evidence is not reinterpreted as the old coarse
measurement. Current readers reject unknown policy/digest combinations; frozen
v4 readers preserve previous data without granting v5 publication authority.

The RMS, coverage, independent-source and minimum-duration quality gates remain
unchanged. A fixed 0.3-second uncertainty band is removed once from both ends
of the final shared interval, then every pair is measured again on that same
interior. A result shorter than eight seconds is rejected. This prevents the
clock's permitted residual from becoming supposedly certain skippable content.
The first unguarded development candidate crossed one sponsor boundary by
0.2919103 seconds; that failed prototype remains recorded separately.

All geometry/time candidates remain bounded. Safe lower bounds reject pairs
that cannot pass any permitted transform, and fixed-size caches reuse exact
views and hypotheses. Equivalence tests compare these optimizations with the
exhaustive calculation. The v5 calculation budget is explicitly 200 million
work units, raised from v4's 100 million after measuring the complete six- and
nine-source workloads; this changes resource admission, not similarity gates.
The view cache is limited to 128 MiB. Budget exhaustion returns no partial
publishable result. This is not a general capacity or latency guarantee.

If a cohort has enough independent usable refinement sources, those sources
use calibrated evidence. Coarse evidence is retained for an independently
supported subset without refinement, or for the whole cohort when refinement
quorum is unavailable. A failed or limited calibrated search cannot silently
fall back to a more permissive result.

## Extraction, cache and compatibility

The new sampling stage shares the original extraction deadline and source/tool
admission. Its integer-PTS selection avoids floating-point `0.1` errors at exact
frame boundaries. Complete source timestamps, output counts, process exit and
final identities remain required. When complete auditing proves that a low-rate
or VFR source has no frame in some 100-millisecond slots, only that explicit
cadence condition may yield no refinement while preserving valid audio/coarse
evidence. Unknown timestamps, process failures, source/tool changes, limits and
cancellation remain failures of the overall extraction.

The refinement profile is `r16-120s-100ms-optcad2`. GAFB v3 retains up to 1,200
timestamped rasters and allows a 512 KiB payload; historical v1/v2 readers keep
their 256 KiB limit. Schema 53 changes only the cache payload constraint. Old
cache bytes, configuration and schema catalogs remain unchanged. The schema-53
catalog was generated from actual PostgreSQL 17, not inferred. Backup validation
preserves the old cap for schema 50-52 and adopts the new cap only for schema 53.
Historical v4 admissions/results, including visual results, retain independent
wire types, qualification rules and original fingerprints.

## Results and limits

| Population | Result | Evidence boundary |
| --- | --- | --- |
| Previously observed development corpus, 15 positives and 6 negatives | 4 safe positive detections, 11 misses, 6 correct abstentions, no protected-content overlaps after the fixed guard | Known development/regression material, not a new holdout |
| First new Robin Hood cohort, R16/R17/R18 | 3 of 3 safe detections; maximum endpoint error 2.6101747 seconds; no sponsor, title or narrative overlap | Three new independent episodes of a known series, selected and source-labeled before candidate execution |
| First new NASA cohort, N394/N395/N396 | 3 of 3 correct abstentions; no false positives | Three new source-reviewed short-ident negatives |

The positive and negative results used the same frozen candidate. The label and
candidate digests, first-run outputs and independent scoring are retained. The
five-second endpoint tolerance is separate from strict protected-content checks;
only one microsecond is reserved for serialization rounding. No output means
undefined precision/boundary error, not a perfect boundary score.

R13 now qualifies in the known mixed-support development set. R14/R15 were
qualified by v4 in the earlier mixed-support follow-up but are now withheld
because the calibrated route finds competing windows. This is a known recall
regression in that cohort, not a claim that v5 improves every previous result.
Larger support pools do not automatically improve every episode. Beverly Hillbillies, Hubblecast and
One Step Beyond still have unrecognized openings in the retained population.
The short Hubble bumper must not be extended over its different episode title
to manufacture a complete opening; static presenter imagery is insufficient
evidence for One Step Beyond. This increment does not establish recognition
across arbitrary series, versions, long libraries or a random population.

The new source selection was the next natural R16-R18 and N394-N396 sequence.
The source-only review discloses assistant visual assessment, sampled actual PTS
and uncertainty; it is not human annotation or direct audio review. A missing
license statement on R17 remains unknown. A prematurely transferred partial
R18 file was rejected and retained; the complete original was later verified
without replacing the selection. Original media is not redistributed.

## Verification

All formatting, tests, builds, decoding and runtime checks run on `test-env`.
The first combined-media test attempt omitted fixture JSON from its transfer;
the three resulting file-not-found failures are preserved and the complete
media-package repeat passed after transferring the unchanged fixtures.

The new extractor processed 28 selected records: 27 complete feature sets and
the original missing-audio N386 exclusion, with zero extraction failures. Every
successful source has 1,200 refinement observations. All seven batch resumes
preserved feature and checkpoint bytes exactly, with one attempt per source.
The excluded source is never counted as a correct negative. New holdout data
was extracted before the frozen candidate's first matcher execution.

| Verification scope | Recorded result |
| --- | --- |
| Final detector package with race | 150 parent passes |
| Composed media checks | 539 unique parent passes, 32 unrelated environment/profile skips; the actual Docker subset passed 87 with no skips |
| Database migrations | 429 parent passes, no failures/skips; schema 53 and five catalog test groups passed |
| Library analysis and calibrated publication | 108 unique parent passes; the added cache-to-calibrated-matcher-to-playback case passed with two existing cases repeated |
| Backup and recovery | 15 backup and 4 recovery parent passes, including real large-cache archive/restore and v4 history isolation |
| Server and tasks | 57 server and 30 task parent passes in bounded Docker workers |
| Corpus operator | Complete focused race suite passed; production extraction/resume of all 28 selected records passed |
| Builds | Final corpus operator and application development builds passed |

No repeated parent is added twice to a composed unique count. The owned test
containers and networks are absent. PostgreSQL 55999 was stopped after zero
client sessions, retaining all databases, source media, incomplete transfers and
failed/accepted evidence. The original main checkout's unrelated changes remain
untouched. No Docker release, merge or remote publication is implied.
