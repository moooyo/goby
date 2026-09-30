# Intro quality: second recognition increment

Baseline: `7fb7caa`. Continue on `codex/intro-quality` with remote-only execution
and the existing Docker-only delivery boundary. The previous increment's
results, first held-out misses and later diagnostic repeats remain unchanged.

The selected increment is now complete. The
[result](../development/intro-quality-round2-20260930.md) records the successful
first new positive/negative cohorts, the known development misses and the
R14/R15 mixed-cohort recall regression. Broader recognition remains open.

## Development scope

The previously evaluated R10-R15, N388-N393 and expanded Beverly Hillbillies,
Hubblecast and One Step Beyond cases are development/regression material.
Investigate spatial normalization, candidate nomination and source-clock
alignment using their retained media and extracted features. Distinguish a
confirmed implementation defect from a recognition limitation. Do not weaken
independent source identity or complete-support requirements merely to produce
outputs. Preserve sponsor, episode-specific title, narrative and ending regions.

Changes must have a bounded, explicit policy and reproducible evidence. Preserve
whole-window transformations and complete witness remeasurement; do not choose
arbitrary per-frame transformations to hide contradictory content. Descriptor
or extraction changes require a new profile and faithful cache representation.

## Independent evaluation

Select the next natural Robin Hood episodes R16/R17/R18 and NASA episodes
N394/N395/N396 before running a candidate. Source-only review determines their
labels rather than the anticipated positive/negative role. Freeze source IDs,
content hashes, labels, protected ranges and uncertainty before evaluation.
Keep original media and source attribution private; never infer a missing
license statement from adjacent episodes. No media is redistributed.

Freeze the candidate before its first new-cohort execution. Report each cohort
alone first, then identify any later mixed-support diagnostics separately. A
post-observation repair and repeat cannot be relabeled unseen evidence. Retain
the five-second endpoint scoring tolerance and the separate strict protected
content rule; a partial common bumper is not a full-title-block detection.

The target is a demonstrated gain on the known miss families and recognition on
new positive originals while retaining the negative and protected-content
controls. Report remaining misses explicitly; completion of an assessment alone
does not establish recognition improvement. Existing accepted behavior receives
focused regression, cache/publication and historical-data compatibility checks.

## Environment and delivery

Use `/opt/goby-intro-quality-20260930-round2-01` on `test-env` for owned work.
Bound new source acquisition to approximately 650 MB and keep diagnostics small.
Prior evidence and original data remain retained. Formatting, tests, decoding,
builds and runtime probes are remote-only. Do not restart old PostgreSQL or
verification campaigns without a direct need. No registry publication,
production deployment or additional platform qualification is included.
