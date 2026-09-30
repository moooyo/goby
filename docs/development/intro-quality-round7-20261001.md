# Intro quality: complete-target scoring and new source evidence

The seventh increment adds independent source-target scoring to
[`intro-region-eval`](../../scripts/test-env/intro-region-eval/README.md).
It distinguishes observed common interiors, complete opening coverage,
protected-content violations and blocked execution. It also expands real
regional evaluation beyond the previous corpus and preserves the first
outcomes. This increment does not change the matching algorithm or establish
a production recall gain.

Baseline is `c4859ef`. Application behavior remains `4817cb3`, detector v5,
execution version 5, schema 53 and GAFB v3. The Docker release catalog is
unchanged. The [plan](../planning/intro-quality-round7-plan-20261001.md) and
[manifest](intro-quality-round7-results-20261001.json) bind the source,
selection, labels, original failures and remote verification.

## Scoring without another matcher run

The new mode reads an existing research report and a separate label document;
both require exact SHA256 arguments. It never calls the matcher or gives
discovery access to labels:

```sh
go run ./scripts/test-env/intro-region-eval -mode score \
  -report /private/reports/evaluation.json -report-sha256 REPORT_SHA256 \
  -labels /private/labels/frozen.json -labels-sha256 LABEL_SHA256 \
  -output /private/reports/new-score.json
```

The original evaluation command remains compatible. Source ID, original
episode key and media hash must agree between reports and labels. Bounded
strict parsing rejects missing/null fields, malformed intervals, JSON aliases,
identity drift and contradictory completion claims. Cheap fixed-protocol
witness checks do not remeasure descriptors or authenticate external reports.
Output uses the existing private atomic no-overwrite writer.

Each candidate reports target coverage, missing head/tail, endpoint error,
outside-target duration and every positive protected overlap. Endpoint
differences and the one-microsecond protection decision use exact decimal
arithmetic. Any protected overlap above that serialization epsilon fails,
even if both endpoints are within the historical five-second tolerance.

The fields deliberately separate three questions:

- `legacyEndpointRuleHit`: exactly one candidate passes the old endpoint and
  protection rule.
- `strictFullTargetCovered` and `strictContainedInTarget`: separate complete
  coverage and no-overreach facts for each candidate.
- `fullTargetCoverageMatch`: one candidate covers the full target and passes
  the legacy rule. This permits its documented endpoint tolerance and does
  not claim exact target boundaries.

For example, `[20,31.9]` against `[20,32]` can pass the old endpoint rule while
still missing 0.1 seconds of the target. Multiple candidates are neither joined
nor reduced to whichever best fits a label. Positive empty results remain
misses; negative empty results count only when evaluation completed. A timed-out
evaluation remains blocked, with no coverage or negative success. `missReason`
distinguishes no candidate, protected overlap, multiple candidates, incomplete
targets and boundary errors. Cancellation clears partial scores.

Episode identity, variant support and source-only review are explicitly
caller-supplied assertions. Variant declarations cannot alter targets or remove
misses. The scorer binds provenance bytes but does not prove their authenticity
or freeze chronology. All results remain `productionResult: false` and
`independentHeldout: false`.

## Source selection and review

R19/R20/R21 were selected from retained media before regional evaluation.
Their existing source-only labels describe independent Robin Hood episodes
with an actor-credit, archer-action and title opening, without a visible
sponsor. Original targets and narrative guards were copied unchanged. These
episodes already had application-detector results and are development data;
their first regional run is not a new held-out accuracy assessment.

New Beverly episodes 3, 4 and 5 were selected in natural episode order from
primary Archive.org metadata, using the standard H.264 rendition consistently.
The retained item records identify
[episode 3](https://archive.org/metadata/Beverly_Hillbillies_Ep03_Meanwhile_Back_At_The_Cabin),
[episode 4](https://archive.org/metadata/Beverly_Hillbillies_Ep04_The_Clampetts_Meet_Mrs_Drysdale),
[episode 5](https://archive.org/metadata/Beverly_Hillbillies_Ep05_Jed_Buys_Stock)
and the later [episode 6 supplement](https://archive.org/metadata/Beverly_Hillbillies_Ep06_Trick_Or_Treat).
The three downloaded H.264 source files total 456,064,212 bytes. Official size/MD5 bindings,
SHA256, source identity and source-only decode receipts are retained. Metadata
can establish the selected upload and declared episode identity, but cannot
establish an identical opening cut.

Each of four fresh source reviewers, without inherited algorithm history,
independently reviewed one assigned episode's opening and context sheets plus
selected boundary frames. Each review covers approximately one-second samples through 119.099249
seconds and ten-second samples through 590.492077 seconds. This is assistant
visual annotation, without audio or continuous playback. The coordinator had
algorithm context but copied the primary targets and guards mechanically.

| Source | Primary source-visible opening target, seconds | Protected episode material, seconds |
| --- | --- | --- |
| B3 | `[0,26.522102]` | `[28.023353,590.492077]` |
| B4 | `[2.502085,55.546289]` | `[56.547123,590.492077]` |
| B5 | `[1.501251,29.024187]` | `[29.524604,590.492077]` |
| B6 | `[1.501251,28.523770]` | `[30.025021,590.492077]` |

B4 includes the origin-story montage before its car/title sequence. B3 starts
already inside a short title presentation; B5 and B6 have leading black before
that presentation. Individual reviewers' black-separator conventions differ:
B5's primary end includes an uncertain black transition, whereas the others
use the visible-image boundary. Those primary labels, alternate brackets and
uncertainties remain unchanged rather than being adjusted to a candidate.
Boundaries are midpoint estimates from roughly one-second sample brackets,
not frame-exact edit points. B3's zero start is censored by the source beginning;
the completeness of an earlier broadcast opening is unknown.

After seeing source-level cut differences, one explicit amendment selected
the next natural episode, B6, before any B3/B4/B5 matcher output existed. Its
149,433,073-byte H.264 source file received the same source-only preparation and an
independent review. B3/B5/B6 form a separate short-presentation follow-up;
B4 and the original B3/B4/B5 result remain in the record. B3 and B5 had already
been evaluated by that follow-up, so it is not another unseen three-episode
sample. Exact common full-target support remains `unknown`; shared credit
structure is not proof of identical action extent or fade boundaries.

New 96-by-96, 100 ms observations use the retained round-five extraction audit
with new source/output bindings. For new media, explicitly named
`source-probe-adapter` files supply the necessary source metadata. They are not
production feature caches; the historical `frozenFeaturePath` manifest field
binds these adapters for compatibility. The container independently rechecks
source/tool hashes, geometry, PTS selection and complete raster readback.

## Frozen regional outcomes

All new regional runs use the unchanged round-six implementation digest
`66e54013928949a4e647aa0a0cf931a308734ac0b3f0035c907a208c185231e8`.
The complete positive targets were frozen before each cohort's first matcher
invocation; no detector thresholds or work budgets were changed.

| Cohort | First completed result | Diagnostic implication |
| --- | --- | --- |
| R19/R20/R21 | Zero groups; all three positive targets missed | All 48 clock combinations fail aggregate appearance; no boundary candidate exists |
| B3/B4/B5 | Zero groups; all three positive targets missed | Of 1,296 clock combinations, 245 pass aggregate appearance and none pass group motion; boundary search is not reached |
| B3/B5/B6 follow-up | Zero groups; all three positive targets missed | Of 2,754 clock combinations, 2,469 pass aggregate appearance and 42 pass aggregate group motion; none pass window appearance and no boundary candidate exists |

The old B1/B2/B7 report was scored without rerunning discovery and remains
three positive misses. The retained real two-second deadline report scores
all three cases as blocked and exits nonzero; it contributes no successful
abstention. These outcomes distinguish a completed evaluation, correct
accounting and actual recognition. The current evidence does not support
blaming these cohort misses on strict observed-boundary continuity. All newly
reviewed episodes are positive cases; no new negative cohort or new precision
estimate is provided. The B356 follow-up overlaps the B345 population and must
not be counted as three additional independent episodes.

The next bounded recognition task is to diagnose fixed-geometry selection and
candidate-window coverage on the retained B356 evidence. This cohort supplies
aggregate motion-qualified clocks, but none yields the required spatially
distributed support at the frozen 850-per-mille window coverage threshold.
Preserve the current boundary and protected-content controls while isolating
that earlier failure; extending observed bounds cannot repair absent window
appearance support. Full-target variant correspondence also remains unproven.

## Verification and retained limits

The final default and race suites each pass the same 63 parent tests on
`test-env`, including 13 new scoring tests. The explicitly gated full-prefix
test is skipped in both. Its preceding-round evidence is reused only for the
unchanged matching engine; this round does not claim a fresh full-pipeline,
application, database, UI or capacity campaign. Actual scoring covers retained
and new real reports plus blocked execution.

Final scorer implementation SHA256 is
`432443cd9d0a27aee0aea818f55c6ec2a72115a7187824e1454c3d1550ad6076`.
The final README-only introduction correction is separately bound after source
freeze; all tested Go bytes remain unchanged. Independent code review closed
decimal-output, cancellation, malformed-array and impossible-witness admission
issues. No local verification was performed.

Original operational failures remain: Robin's first container probe could not
read a mode-0600 driver, fixed only by making that script readable; its first
post-run receipt used the wrong nested source-ID field, fixed without another
matcher run. Initial Archive.org TLS failure used local download-only transport
as a fallback; all media validation and decoding still ran remotely. Robin's
outer receipt error prevented persistence of the child process exit code;
its successful completion is established by the immutable report, and the
missing exit status is retained as unknown. A later
transient SSH signing failure recovered on a normal retry.
A follow-up preflight also assumed the wrong label-archive location; resolving
the existing B6 archive and reused B345 labels by their unchanged hashes fixed
the harness before the B356 matcher ran. All first reports remain unchanged.

Media, rasters, labels, original reports and recoverable private archives are
retained. Source files and large temporary artifacts live in owned `/tmp`
directories rather than consuming the constrained remote root filesystem.
No application migration, new dependency, database, release, push, merge or
deployment is included. The isolated worktree is the only checkout changed.
