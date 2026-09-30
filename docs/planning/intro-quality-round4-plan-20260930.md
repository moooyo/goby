# Intro quality: version-mismatch diagnosis

Baseline: `4957fe4`, the completed conservative common-interior repair.
Continue in the isolated `codex/intro-quality` worktree. All verification,
formatting, builds and source decoding run only on `test-env`.

The selected diagnostic and tooling increment is complete. The
[result](../development/intro-quality-round4-20260930.md) records the bounded
source/descriptor findings, new visual-stage trace, 33-case result equivalence
and verification. No tested descriptor became a product recognition change.
The next bounded mechanism to investigate is a higher-resolution,
region-preserving representation that separates scene motion from overlays.

## Selected work

Investigate the retained Beverly Hillbillies B1/B2/B7 version mismatch.
Review actual source-frame correspondences independently of matcher output,
then separate spatial, temporal, photometric and editorial differences.
The preceding finite geometry expansion failed its necessary matching-frame
condition; do not repeat it as the main improvement strategy.

Use a bounded descriptor diagnosis with existing 16-by-16 refinement rasters.
Compare the current luminance descriptor with three fixed alternatives:
midrank, local mean residual and signed central gradient. Keep the original
contrast/hash gates, and report each individual gate plus complete interval
coverage rather than ranking only successful frame pairs. NASA narrative
windows and One Step Beyond static-presenter windows are controls, not
positive training labels. Alternative metrics are diagnostic observations,
not equivalent production evidence merely because their threshold numerals
are unchanged.

Adopt a product change only when the source evidence and bounded diagnostics
support a specific mechanism. Preserve independent source support, complete
pairwise witnesses, minimum duration and protected-content semantics. A
measurement-domain or wire change requires explicit compatibility treatment.
No source IDs, labels or source-specific parameters may enter production.

The investigation also exposed an observability gap: the corpus report records
the audio-led route but not the production visual fallback. Add a bounded
visual-stage trace to `AnalyzeWithDiagnostics`, observing the real calibrated
and coarse discovery/publication calls without rerunning them. Record quorum,
calibration/pair/group rejection stages and consensus recovery. Keep aggregate
counts complete after trace truncation, distinguish completed lookups from
unique pair measurements, and retain partial diagnostics after failure without
granting publication authority. The separate visual experiment must not enter
the production trace. Prove exact result, error and comparison-budget equality
with ordinary `Analyze`; these diagnostics do not claim a recall gain.

## Evaluation and resources

All 33 previously evaluated cases are now development/regression material.
If a candidate emerges, select and source-label new episode originals before
its first evaluation. Freeze selection, source identities, labels, executable
and source digests. Preserve failed first attempts and keep repeat observations
out of the independent denominator. A negative investigation is not a claimed
recall improvement, and unsupported positives remain misses.

Use `/opt/goby-intro-quality-20260930-round4-01` for private diagnostics.
Reuse retained source media and features; do not copy large source/document
trees or acquire new media until the candidate evaluation scope is selected.
Recover disk space only from explicitly identified redundant transport
archives after verifying byte-identical local recovery copies remotely.
Preserve authoritative source trees, media, databases and prior evidence.
This scope does not include a Docker release, push, merge or deployment.
