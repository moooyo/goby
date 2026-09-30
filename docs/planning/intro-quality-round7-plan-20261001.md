# Intro quality: complete-target scoring and source support

Status: complete within this scoring/source-evidence increment. The
[result](../development/intro-quality-round7-20261001.md) and
[manifest](../development/intro-quality-round7-results-20261001.json) retain
all first outcomes and the unresolved real-recognition gap. No production
recognition or release change is accepted by this increment.

Baseline: `c4859ef`. Application detector v5/execution 5/schema 53/GAFB v3 and
the accepted Docker catalog remain unchanged. All execution, formatting and
verification run through `ssh test-env`; use the isolated `codex/intro-quality`
worktree. The main checkout's unrelated changes remain untouched.

## Selected work

Add an independent scoring mode to the research evaluator. It consumes an
exactly bound previous report and separately frozen source-only labels without
executing a matcher or passing labels into discovery. Preserve source IDs,
original episode keys, media hashes, extraction and implementation identity.
Caller-declared episode and variant facts are provenance, not inferred proof.

Keep complete opening targets distinct from observed shared interiors. Report
target coverage, missing head/tail, outside-target duration, endpoint errors
and protected-content overlap. Preserve the historical five-second endpoint
rule as a separately named measurement; it must not excuse protected overlap
or imply strict complete-target coverage. An incomplete evaluation is blocked,
not a correct negative; an empty complete positive is a miss. Scored results
remain research-only and do not authorize application markers.

Use bounded strict input parsing, exact SHA256 bindings and the existing
no-overwrite report writer. Cover identity drift, malformed/incomplete reports,
negative false positives, protected overlap, truncated interiors, empty misses
and blocked execution. Do not change the frozen research matching engine.

## Source support and real runs

Existing Beverly material contains only B1/B2/B7, with distinct opening cuts.
Retain their positive labels and record them as known development misses.
The same three-way interior is not evidence for complete opening support.

Select the retained R19/R20/R21 cohort before new regional results: source-only
review records independent episodes with actor credits, archer action and the
series title, without a visible sponsor. Use original frozen targets and
protected narrative ranges. These are already seen development episodes,
including prior application-detector evaluation. Their first 96-by-96 regional
evaluation is a new method baseline, not new held-out accuracy. Shared sequence
structure alone does not establish identical full-extent variants.

Reuse the audited round-five extraction machinery with new source/output
bindings and unchanged PTS, identity, geometry and byte limits. Run the frozen
round-six evaluator once and retain the first result, including abstention or
failure. Keep subsequent scoring separate from algorithm execution.

Investigate previously unused Beverly episodes 3, 4 and 5 in natural episode
order using primary archive metadata. Establish source identity and inspect
opening material before assigning labels or claiming a shared variant. Freeze
any new source-only labels before matching. Metadata availability or matching
file names do not prove identical opening edits. Do not silently substitute
another encoding of an existing episode or select cases by detector success.

One source-only amendment selected natural-next episode B6 after reviewing
B345's cut differences and before any B345 matcher output. Its separate
B356 follow-up preserves the original mixed-cut cohort and the existing B3/B5
labels. This is a source-selected development follow-up, not another independent
three-episode holdout. No further source expansion is part of this increment.

## Resources and delivery

Remote root storage is constrained. Use task-owned `/tmp` directories for new
media, rasters and build intermediates, with small retained records under
`/opt/goby-intro-quality-20261001-round7-01`. Retain source bytes and evidence
locally before any owned temporary-data reclamation; never delete shared caches
or old authoritative corpora. No new database, dependency installation, release,
push, merge or deployment is required by this increment.
