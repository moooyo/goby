# Automatic library intro workflow - September 30, 2026

Status: **COMPLETE within the selected automatic library intro scope**. The
composed backend checks, Node/UI campaigns, actual automatic Docker workflow,
both image archive export/import checks and owned-resource closure passed.
Git integration is recorded separately; no merge or push is claimed here.
Exact receipts and toolkit identity are bound by the
[result manifest](library-intro-automation-results-20260930.json).

See the [execution plan](../planning/library-intro-automation-plan-20260930.md),
[API contract](../api/media-analysis.md),
[native UI](media-analysis-native-ui.md) and
[runtime contract](media-analysis-runtime.md). Baseline: `40e02bb`.

## Recorded delivery identities

| Artifact | Identity |
| --- | --- |
| Application source | `95607ffa505c7a9368b1bfd8ec3d545d009285d3` |
| Application SHA-256 | `b85fa1ecd2cbe8b56643abfdffc8e312bcf346d7f27d488767fecabe69805838` |
| Application size | 51,382,640 bytes |
| Software image | `sha256:24aca9db2d30ab8d2e0e7275bbdc4b4d395d355c99a79e0d5421353345b86ce9` |
| Software archive | 440,235,520 bytes; SHA-256 `3062c7114a8a63c78cb592c7c0d17a3a60fca06a485d61437105ca70df86d768` |
| AMD image | `sha256:3a8467bcbb97c82b4675b16d06364a3b27a06c9605df2f6374e0f95ede1e12b7` |
| AMD archive | 992,706,048 bytes; SHA-256 `4061ddb4ea39fcfcc2f37d099fe288aa770fc6325b85af2ec3da9e165352942a` |

Both archives passed export and actual import. The inherited media-tool/base
layers remain unchanged; this application update does not claim a new GPU
campaign. Both archives were downloaded to the local delivery root
`D:/Code/goby/.artifacts/intro-automation-20260930`, in its `software` and `amd`
profile directories. `goby-docker-operations.zip` belongs beside those directories
and uses their matching archives. Its final size/hash and current-release
catalog identity are recorded in the result manifest.

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
| Legacy rejections | An explicit false-to-true transition retires rejection flags and advances decision revisions while retaining decision rows and audit history |
| Interrupted automatic work | Graceful shutdown and crash recovery persist a request for fresh admission under current library policy; explicit user stops/manual runs do not auto-retry |
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

## Verification results

| Evidence | State |
| --- | --- |
| Composed backend checks on `test-env` | 135 parent passes: database 10, library 64, tasks 30, server 10, backuppg 21 |
| Current UI campaign | 27 passes and one retained historical full-Phase-3 skip |
| Node source checks | 11 passes, zero skips |
| Actual Docker automatic workflow `runtime01` | PASS without analysis-start or candidate-decision calls |
| Policy, durable scheduling, shutdown/crash request replacement and compatibility | Covered by the selected backend composition; original failures retained and only affected scopes rerun |
| Software/AMD application layers and archive export/import | PASS with unchanged inherited base layers |
| Actual AMD-image application/UI check | PASS with software axes: startup, catalog/disabled-policy state, login, library switch and absence of human-correction controls |
| Release catalog and local delivery | Profile archives downloaded; toolkit/catalog identities bound by the result manifest |
| Owned-resource closure | PASS; exact disposition below |
| Git integration | Separate; no completion claim |

`runtime01` exercised the actual production application in Docker. A library
option PATCH alone triggered a background run. All three reused FH positives
became `qualified`, and PlaybackInfo exposed the same detected intervals. N1/N2
were singleton sources: both returned `no_result` with `insufficient_cohort` and
no marker. Three episodes in a disabled control library never published a marker.

A completed rescan triggered a second `system_event` analysis run with the same
passing outcomes. Disabling the enabled library immediately withdrew its three
markers. Container recreation retained the disabled option and absent markers;
the application then stopped cleanly. This journey made no explicit analysis-start
or candidate-decision request. Package tests retain the separate re-enable and
interrupted-work replacement coverage; the clean recreation is not a new crash
injection claim.

The AMD image separately passed application startup, retained catalog and disabled
library policy, and the actual UI login/library-switch/no-correction-controls
checks using software axes. The first two UI attempts timed out because an exact
Username label did not include the required-field asterisk. The fixture switched
to the project's existing `/^Username/` locator and passed. No product fix was
needed; original logs remain retained. This check does not claim GPU execution.

## Owned-resource closure

| Resource | Final observed state |
| --- | --- |
| Owned containers | Zero |
| Owned Compose networks | Zero |
| Dedicated PostgreSQL on port `55996` | Zero clients before normal fast stop |
| Application HTTP port `38963` | Closed |
| Private data and evidence | Retained, including original failed attempts |
| Three protected services | IDs, PIDs and start times unchanged from the initial observation |

The fixture closure concerns only owned resources. Git integration remains a
separate record and does not expand the accepted runtime or accuracy scope.

## Accuracy and delivery boundary

The journey reused FH1–FH3 and N1/N2 from the previously accepted corpus.
Their repeated use establishes automation behavior, not new holdout
accuracy. The matcher, its thresholds, at least three independent supporting
episodes and first-600-second extraction horizon are unchanged. Earlier
[accuracy evidence](media-analysis-resilience-phase2-20260921.md#final-phase-2-acceptance)
remains limited to its declared same-series episode population.

This increment does not resume online scraping, broaden GPU/media accuracy,
reopen historical capacity/fault matrices or add another deployment form.
Delivery remains Docker application updates with the existing accepted tools.
