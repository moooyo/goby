# Automatic library intro workflow - September 30, 2026

Status: **IMPLEMENTATION AND VERIFICATION IN PROGRESS**. This record describes
the selected contract and required evidence. It does not claim a passing test,
browser journey, Docker image, resource closure or Git publication.

See the [execution plan](../planning/library-intro-automation-plan-20260930.md),
[API contract](../api/media-analysis.md),
[native UI](media-analysis-native-ui.md) and
[runtime contract](media-analysis-runtime.md). Baseline: `40e02bb`.

## Selected behavior

The normal workflow is to enable **Automatic intro detection** in a TV library,
then let background work publish qualified matches for playback. Progress and
errors stay visible. Uncertain or absent results do not publish a marker and do
not require human approval. Preview generation retains its separate manual flow.

| Area | Current implementation contract |
| --- | --- |
| Library policy | `LibraryOptions.EnableIntroDetection`, default false; true is valid only for `tvshows` |
| Enable request | Creating an enabled library or a false-to-true edit commits durable `IntroAnalysisRequested` |
| Scan completion | Both independent and task-owned successful scans of enabled libraries record another request |
| Default schedule | Untouched task definitions receive a 24-hour interval plus the dedicated event trigger; prior custom/cleared/disabled choices are preserved |
| Automatic selection | Only enabled TV libraries; empty eligibility does not select disabled libraries |
| Publication | Current qualified evidence is effective automatically; `review` and `no_result` do not auto-publish |
| Disable/re-enable | Disable immediately withdraws detected publications; re-enable requires new work |
| Administrator UI | Library switch, status, progress and errors; no accept/reject/reset or manual intro-edit entry points |
| Compatibility | Existing Manual/Import/chapter state, audit data and old API contracts remain readable/usable |

The library switch is the sole current policy control for automatic intro
publication. The legacy `AutoPublishIntros` Boolean remains in wire and historical
profiles; new writes canonicalize it to true. It is not a second UI switch.
Existing source, authority, cohort, task-fence and suppression checks still apply.

Schema 51 preserves existing library rows while treating a missing option as
false. It adds the dedicated event and allows the TV-only option. Only an old
global false publication setting is migrated to true with an analysis revision
increment and withdrawal of automatic detections, feature cache and preview
references. Already-true settings do not receive that invalidation. Stored
historical profiles and manual/imported facts are not relabeled as new results.

For Emby library options, `EnableMarkerDetection` maps to the switch. A supplied
`EnableMarkerDetectionDuringLibraryScan` requires the main flag in the same
request with the same value. `IntroDetectionFingerprintLength` remains exactly
10 minutes; contradictory modes and unsupported lengths are rejected.

## Verification still required

| Evidence | State |
| --- | --- |
| Focused migration/library/event/scheduler/publication/recovery/HTTP checks on `test-env` | Pending |
| Remote administrator build and current workflow checks | Pending |
| Actual Docker enable-to-background-result journey without manual analysis-start/decision calls | Pending |
| Disable/re-enable, scan completion, durable requests and restart behavior | Pending |
| Updated software/AMD application layers and matching release catalog | Pending |
| Owned-resource closure and retained failure records | Pending |
| Git integration | Separate; no completion claim |

The actual journey will reuse FH1–FH3 and N1/N2 from the previously accepted
corpus. Their repeated use establishes automation behavior, not new holdout
accuracy. The matcher, its thresholds, at least three independent supporting
episodes and first-600-second extraction horizon are unchanged. Earlier
[accuracy evidence](media-analysis-resilience-phase2-20260921.md#final-phase-2-acceptance)
remains limited to its declared same-series episode population.

This increment does not resume online scraping, broaden GPU/media accuracy,
reopen historical capacity/fault matrices or add another deployment form.
Delivery remains Docker application updates with the existing accepted tools.
