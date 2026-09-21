# Media analysis archive and recovery boundary

Schema 50 adds typed task admission and eleven analysis tables. Its raw archive
gate runs after the schema-49 compatibility gate; historical migration prefixes
and their existing checks remain unchanged. Inspection, snapshot capture and raw
restore all use the same resource-state validator. No analysis table is removed
from catalog fingerprints or silently ignored because its data will later be
invalidated.

## Raw validation

The archive retains facts, not live authority. Validation does not require an old
credential to remain enabled, a historical root to remain mounted, or a cache
file to exist. It checks the stored admission graph and exact payload semantics.

Historical execution versions 1 and 2 use independent frozen admission and
result decoders, including their original canonical fingerprints and qualification
rules. Current execution version 3 does not reinterpret those measurements.
Unavailable executions map to the matching detector version for all three
generations. Raw restore validates the original history before normalization
withdraws publication authority; a stale historical observation cannot become a
current worker, accepted detection or feature-cache authority.

| State | Required raw invariants |
| --- | --- |
| `analysis_settings` | Exactly one bounded profile; positive configuration revision and publication epoch. |
| `analysis_run_profiles` | Analysis task ownership; closed profile/execution JSON; fingerprint over the exact revision, epoch and execution/profile values; correct intro/preview execution family. |
| `analysis_work` | Same parent run, task, library and scope as the task child; deterministic child identity; immutable force selection; at least one target in a bounded source window. |
| `analysis_work_sources` | At most 32 contiguous positions per child, same admitted library, selected targets, bounded identifiers and original source/profile budgets. Historical manual/decision/preview revisions remain evidence. |
| `analysis_feature_cache` | Exact length-delimited item/source/profile cache key; bounded versioned binary payload; internal content and algorithm identity match the row; bounded row and aggregate byte budgets. |
| `analysis_detections` | Stored result uses its exact typed version, status and interval; profile/cohort/source bind to the admitted work; automatic publication requires the current profile and epoch. |
| `analysis_detection_sources` | Exact union of the result's real target/support identities, hashes and intervals, bound to the same admission window. Abstention has no fabricated content hash. |
| `analysis_intro_decisions` | Explicit retained source and positive revision; no inference of new manual authority. |
| `analysis_intro_audit` | Closed result/decision evidence matching its action and source. Historical actors are retained facts. |
| `analysis_preview_state` | Positive per-item clear tombstone; preserved independently of filesystem derivatives. |
| `analysis_previews` | Correct task/profile/source binding, admitted dimensions/size/count, and exact bounded nominal/actual timeline covering the recorded source duration. |

Task `analysis_input` permits only typed LibraryIds, ItemIds and Force, with
bounded sorted unique identifiers. Configuration fingerprints and child scope
carriers must agree with analysis ownership. Native, compatibility user,
application-key and system sources retain their distinct authority shapes.
Application-key identity never loses its original client binding. Nonempty peer
addresses are canonical unzoned IP literals. These facts do not revive revoked
sessions or constitute a reconstructed task publication fence.

The source window cannot independently reconstruct a full-season cohort hash;
that hash remains bound to its admission and is rechecked against the live
cohort only during a new authorized operation. Raw verification does not invent
unrecorded episodes or require historical sources to match the current catalog.

## Restore normalization

Only after the original raw archive and its fingerprints pass does the recovery
finalizer apply the following changes in its existing transaction:

1. Lock `analysis_settings` and advance `publication_epoch` exactly once for this
   normalization. Preserve profile values, configuration revision and timestamps.
2. Set retained detections' `auto_published` flags to false. Preserve their
   revisions, evidence, intervals and audit history.
3. Remove all `analysis_previews` references and `analysis_feature_cache` rows.
   No archived cache key can authorize opening the original host's cache.
4. Preserve admission profiles, work/source snapshots, explicit intro decisions,
   manual intro overrides, audit records and preview clear tombstones.

An exhausted or invalid epoch rejects the complete transaction. It never wraps,
resets or leaves a partially authorized target. Repeating an explicit restore
normalization advances the epoch again even when no derivative rows remain.
Ordinary restart validation does not invoke this finalizer.

Existing recovery subsequently interrupts unfinished task runs and children;
it does not replay their executors or recreate their process-local fences.
New analysis requires a fresh admission with the restored target's new epoch.
Filesystem cache bytes are not imported, opened, deleted or regenerated by this
database normalization. Their lifecycle remains with the target's owned cache.

The recovery result reports the new analysis publication epoch and actual counts
of withdrawn automatic detections, removed preview references and removed feature
rows. These normalization counts are distinct from the exact raw archive witness.

## Verification status

The accompanying test sources cover historical-version gates, malformed typed
selection and actor carriers, binary and relational corruption, raw round-trip
preservation, epoch precision/exhaustion and transactional invalidation. Actual
PostgreSQL/archive checks belong to the consolidated remote Phase 2 verification;
source delivery alone is not a passing runtime result.
