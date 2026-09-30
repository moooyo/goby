# Intro quality: observed boundaries and reproducible regional evaluation

The sixth increment delivers a separate, read-only
[`intro-region-eval` research CLI](../../scripts/test-env/intro-region-eval/README.md)
and repairs the private regional matcher's demonstrated endpoint overreach.
The twelve-second moving fixture now returns contained observed intervals;
the original eight-second fixture conservatively abstains. This is a tooling
and boundary-method increment, not a real-corpus recognition gain. All four
retained real cohorts still return no group, including Beverly.

Baseline is `38943e4` on `codex/intro-quality`. Application source remains
`4817cb3`: detector v5, execution version 5, schema 53 and GAFB v3 are unchanged.
The accepted Docker release catalog is unchanged. The
[plan](../planning/intro-quality-round6-plan-20261001.md) and
[result manifest](intro-quality-round6-results-20261001.json) bind scope,
implementation, verification and retained failures.

## Observed component boundaries

Round five found the correct synthetic clocks but returned `[18.7, 28.1]`
for a known `[20, 28]` shared segment. Aggregate coverage had admitted
unmatched endpoints. The new `observed-common-component-v2` policy preserves
fixed geometry, clocks, one-to-one frame ownership, support, motion,
state/period rules and work budgets. It changes boundary evidence as follows:

1. A proposal must stay in one maximal continuous full-support component.
   Mapped source indices must advance by exactly one; unsupported observations
   and ownership discontinuities split the component.
2. Edge certificates use the existing 500 ms motion scale inside that same
   component. Project each source's actual first/last observed PTS into the
   anchor; intersect those envelopes and the original candidate interval.
   Do not add 100 ms to the last observation.
3. Require at least eight seconds in every projected source, then fully
   remeasure the unchanged support and motion/state/period evidence. Do not
   lower support, rematch frames or join components to rescue a failure.
4. Preserve disjoint valid components or returned intervals from every source
   across clocks. Ambiguity in a non-anchor source also makes the entire cohort
   abstain, even when the anchor component is the same.

A contained, fully validated larger interval can dominate a child only under
identical clocks, support and global component identity. This avoids repeated
audits within the original budgets. A failed parent never prunes children;
an unaudited child receives no inherited metrics or passing-window credit.

## Reproducible corpus tool

Run on Linux `test-env` with Go 1.27.1:

```sh
go run ./scripts/test-env/intro-region-eval \
  -manifest /private/data/cohort.json \
  -output /private/reports/new-evaluation.json
```

The generic three-source manifest requires distinct source IDs, media hashes
and caller-established `episodeKey` values. Different encodes of one episode
must share a key and cannot provide independent support. The evaluator verifies
the retained extraction contract and exact manifest, gzip, raster and frame
hashes. It does not reopen original media, rerun the decoder, or independently
establish the caller's episode facts. An explicit adapter may bind the exact
old manifest through `derivedFrom`; it does not prove semantic equivalence.

The tool uses bounded regular-file reads with no symlink traversal, strict
JSON/gzip admission, complete source-stat identity fields and actual-PTS slot
checks. New private reports are published atomically without overwriting old
evidence. Cancellation, timeout, invalid input and budget failure preserve
available diagnostics but clear all groups; incomplete execution is never
counted as abstention. The default deadline is two minutes, with an operational
override capped at ten minutes. Detector parameters and work budgets have no
overrides. No database, player, media extraction or network integration is added.

Every report carries `productionResult: false`, `independentHeldout: false`,
exact input bindings and embedded non-test source digests. Final implementation
SHA256 is
`66e54013928949a4e647aa0a0cf931a308734ac0b3f0035c907a208c185231e8`.
The README specifies input preparation, report interpretation and separately
enabled full-prefix verification. Real-media reproduction requires the
separately retained private extractions; media is not checked into the repository.

## Verification and observed results

All execution, formatting checks and verification ran through `ssh test-env`.
The standalone validation module preserves the repository module path and
Go 1.27.1 requirement; the new package uses only the standard library.

| Check | Result and scope |
| --- | --- |
| Final default suite | 50 parent tests passed; the full-prefix parent was explicitly skipped |
| Final race suite | The same 50 parent tests passed; the full-prefix parent was explicitly skipped |
| Separately enabled full-prefix suite | One parent containing four complete pipeline cases passed |
| Twelve-second moving fixture | One group: `[20,31.9]`, `[27,38.9]`, `[34,45.9]`; zero synthetic source-interval crossings |
| Original eight-second moving fixture | Zero groups; the observed hull lasts only 7.9 seconds |
| Shared-static and different-content fixtures | Both completed with zero groups |
| Boundary and ambiguity counterexamples | Moving-island/gap bridging, non-anchor ambiguity, unknown PTS, reused ownership and real-cadence drift checks passed |
| Longer continuous component | Twenty-second positive completes within the original budgets after dominance repair |
| Actual CLI Beverly evaluation | Three sources admitted; completed abstention; result and work counters exactly match the frozen private engine |
| Actual CLI two-second deadline | Nonzero exit, `deadline-exceeded` at nomination, three sources admitted and zero groups |
| Private final real-corpus repeats | All four known cohorts / twelve sources completed; zero groups |

The twelve-second candidate has all 25 support and dynamic patches and
1000-per-mille coverage. It uses 172,817,809 patch comparisons, 36,801,137
lookups, 274,944,000 rendered pixels and three retained hypotheses. The final
Beverly CLI uses 168,979,477 comparisons, 41,544,000 lookups, 274,944,000 pixels
and 77 hypotheses. These are bounded research measurements, not production
capacity acceptance.

The full-prefix run preceded the final source-stat admission hardening. Its
four outcomes and work counters are reused only across the unchanged engine;
source equivalence was checked independently. The final default/race suites
and actual Beverly/deadline runs include that admission fix. Do not describe
this as 51 race passes or a fresh application, database, UI or capacity campaign.
The private predecessor's 33 unique race passes were composed from two logs,
not one all-green run; the imported final CLI suite covers those checks together.

## Retained failures and limits

- Round five's original eight-second fixture and overreach remain recorded.
  Its deterministic generator is copied byte for byte. The old test demanding
  success is not imported; the new safety expectation requires abstention, and
  the separate twelve-second fixture supplies the required nonempty positive.
- The first edge-certificate attempt still joined a 0.7-second moving island
  to a twelve-second core across a 0.7-second nonmatching protected gap. Its
  failing output is retained. Full-component continuity closes that defect.
- A twenty-second component initially exceeded the 100-million lookup budget.
  Exact dominance fixed redundant audits without increasing any work limit.
- The first non-anchor counterexample assertion incorrectly required identical
  retained child endpoints. Its correction checks the same global anchor
  component, overlapping anchor intervals, disjoint second-source evidence and
  cohort abstention. That amendment did not change the engine, and the original
  failed test logs remain.
- CLI review repaired Unicode JSON-key aliases, cleanup of unowned temporary
  files, path-dependent failure classification and incomplete stat identities.
  Final focused checks cover these cases; earlier reports remain separate.

Strict observation continuity sacrifices recall. Missing support splits a
component, and actual 100 ms samples under 0.98/1.02 affine clocks encounter
ownership breaks roughly every fifty frames. The real-cadence mapping test
therefore expects abstention. An artificially stretched-PTS arithmetic test is
not evidence that speed tolerance was preserved.

Observed bounds cannot prove continuous semantic agreement between sampled
frames, or distinguish a repeated narrative segment from an intro. The finite
single-geometry/clock search is heuristic. These twelve real sources are already
seen development material, not a new holdout. Frozen positive labels and strict
protected-content scoring are unchanged; unsupported full openings remain
misses. Neither synthetic containment nor completed evaluation establishes
new precision, recall or suitability for automatic skip markers.

## Next recognition work and delivery boundary

The next substantive task is to establish enough independent same-variant
episode support for complete opening targets, then freeze new source-reviewed
positive and protected-content controls before evaluation. Keep the shorter
shared interior distinct from the full opening. Any proposal to tolerate gaps
or speed differences must first pass the retained boundary and ambiguity
counterexamples and then demonstrate actual-corpus recognition.

This increment adds only the research CLI and documentation. No application,
extraction profile, codec, schema, historical reader or release changes were
made. No new PostgreSQL cluster, container or dependency installation was
required. Previous database data, media, observations, source snapshots and
failed evidence are retained; shared caches were not deleted. Git integration
is local only, with no push, merge or deployment.
