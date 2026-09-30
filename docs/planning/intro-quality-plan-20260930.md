# Intro recognition quality and corpus tooling

Baseline: `cfa12ff`, schema 52. This increment improves short-opening and
version-variant recognition and the frozen-corpus evaluation tool. Existing
Docker/BIF delivery and unrelated workstation changes retain their boundaries.

The selected implementation and focused verification are complete. See the
[result](../development/intro-quality-20260930.md) for the three-of-twelve known
positive improvement, the failed zero-of-three new positive cohort, successful
new negative controls, and the separately identified mixed-support follow-up.
Broader recognition remains open; no new Docker image or deployment is claimed.

## Evidence and implementation sequence

1. Preserve the previous 15 evaluated originals and their frozen labels. Their
   known results are development/regression evidence in this increment, not new
   unseen holdout evidence. Keep the missing-audio original separately recorded.
2. Add bounded, opt-in stage diagnostics without changing ordinary analysis.
   Record audio nominations, short or rejected runs, visual confirmation and
   independent-group outcomes rather than treating every no-result as equivalent.
3. Make extraction resumable with source, labels, feature bytes and extraction
   identity checks. Persist exclusions and failures per original; incomplete or
   invalid cohorts must not silently become successful matcher inputs.
4. Investigate candidate discovery and short-interval evidence using retained
   source features. Changes must not invent audio support, relax independent
   episode identity, extend a common bumper over episode-specific material, or
   turn a diagnostic interval into an automatic marker without qualification.
5. Freeze any proposed automatic policy before evaluating new independent
   source material. Report short and variant cases, misses, false positives,
   boundary error and protected-content overlap separately. A partial common
   bumper is not a complete title-block boundary hit. Preserve existing accepted
   detector behavior through focused regression and adversarial controls.

## Acceptance and environment

Tests, formatting, builds, decoding and runtime experiments run on `test-env`.
Use an owned directory and retain original evidence. No existing PostgreSQL or
Docker service needs restarting for feature-only experiments.

Tool acceptance requires interrupted/failed batches to remain auditable,
validated completed work to be reusable, stale inputs to be rejected, and
excluded originals to stay outside correct-negative counts. Detector acceptance
requires demonstrated improvement against the previous zero-of-twelve positive
recall, explicit remaining misses, and no emitted interval in the tested
protected-content or negative ranges. The existing five-second evaluation
boundary tolerance remains separate from detector uncertainty metrics.

No registry publication, production deployment, new GPU profile, or historical
capacity campaign is included. An unverified candidate must remain explicitly
experimental; do not relabel assessment completion as recognition acceptance.
