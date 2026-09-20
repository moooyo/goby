# Media analysis, compatibility and resilience execution plan

Status: **active; phase 1 verified, closed and published; phases 2 and 3 required**.

The user approved this three-phase consolidation on September 20, 2026 and
authorized implementation, consolidated verification after each phase's code is
complete, merging to `main`, and pushing the verified delivery. The baseline is
`2fd9182aca8ddd1afed6a9af2ab377a82b58a05f`. Development uses the isolated
`codex/media-analysis-resilience` checkout. Unrelated changes in the original
checkout remain outside this increment.

## Execution and scope

Complete all implementation, management surfaces, migrations, tests and recovery
support for a phase before starting its consolidated verification. Independent
implementation inside a phase may proceed in parallel. Close the phase's actual
acceptance and documentation before starting the next phase's product changes.
Failures require repairs and affected verification; an implementation or passing
mock does not substitute for a required real consumer. Accepted unchanged
evidence remains valid within its original scope.

All tests, builds, validation and runtime probes for this increment execute on
`test-env`; local verification has not been authorized for this new task.
Historical local-test authorization in earlier execution records does not extend
to this increment. Host reboot and forced-reset acceptance require an isolated,
owned guest. Shared `test-env` and the physical PVE host must not be rebooted.
The `ui-ux-pro-max` skill remains disabled.

Live TV, EPG, DVR/scheduled recording, tuners, DLNA, external channels and group
playback remain explicitly excluded. Other unselected features and delivery
profiles remain deferred, including offline synchronization, OCI, provider-online
acceptance and additional hardware/platforms. Existing generic dynamic sources
and time shifting retain their contracts. Original Emby Web commercial gating
is not an acceptance gate for the selected server adapters; actual supported
consumer coverage must still be recorded truthfully.

## Phase 1: Compatibility API long tail

Freeze a finite field/route matrix from the current implementation, pinned SDK,
retained client requests and primary protocol evidence. The existing eight-key
item sorting and Search/Hints implementation are baseline, not missing features.

| Work | Required delivery and acceptance |
| --- | --- |
| User query | Implement `IsHidden`, `IsDisabled`, `NameStartsWithOrGreater` and `SortOrder` on `Users/Query`, with bounded parsing, current authority, deterministic ordering, filtering before totals/paging, and source-backed DTOs. |
| Standalone specials | Establish actual placement semantics independently of season-zero classification; ingest and preserve the required metadata, expose applicable fields and implement the predicate consistently before counting and paging. |
| Backed configuration and library options | Select the missing settings with real consumers, including sorting configuration and applicable library import options. Deliver validation, defaults, partial/full updates, runtime behavior, native controls where needed and durable state together. |
| Remaining wire contracts | Complete the selected field, query, literal alias and error gaps required by actual account, browse, search and management consumers. Preserve authentication and opaque-ID boundaries. |
| Composed acceptance | Run affected database, identity, library, settings, HTTP, migration, backup/recovery and real client/admin journeys against the integrated source. Publish the supported matrix and retained limits. |

## Phase 2: Automatic intro analysis and BIF seek previews

This phase owns both media-analysis features and their shared bounded task and
derivative lifecycle. Each feature retains its own correctness checks inside one
consolidated phase verification.

| Work | Required delivery and acceptance |
| --- | --- |
| Content-based intro detection | Evaluate audio fingerprints and visual/timing confirmation on labeled real media. The first supported population is repeated intros across episodes of one series/season, including variants and cold opens. Record source revision, algorithm version, confidence, candidate evidence and no-result reasons. |
| Intro publication and consumers | Preserve effective precedence: valid manual/import override, explicit reserved chapters, then qualifying detected interval. Automatically publish qualifying results; expose uncertain results for review. Support batch analysis, cancellation, rerun and correction without overwriting administrator decisions. Verify false positives, misses, boundary error and actual skip consumption. |
| BIF protocol | Establish binary format, discovery, image access, width handling, authentication, missing-result and applicable HTTP delivery semantics from primary specification and a named consumer. Implement `Items/{Id}/ThumbnailSet` and `Videos/{Id}/index.bif` with the confirmed contracts. |
| Preview generation | Generate source-bound thumbnails and BIF with sampling, dimensions and output budgets. Publish atomically; invalidate stale derivatives; expose generation, rebuild, cancel, errors and storage management. Preview reads must not start unbounded transcoding. |
| Shared lifecycle | Implement admission/backpressure, progress, resource budgets, cancellation, interrupted-run behavior, temporary-file cleanup, cache identity/eviction and backup/recovery semantics. Apply current media authority even to cached delivery. |
| Composed acceptance | Use real media for labeled intro accuracy, BIF decoding and timestamp/visible-frame checks, plus named consumer skip/drag-preview journeys. Include no-intro, recap, changing openings, single/insufficient evidence, VFR, rotation, long/short media, source replacement, concurrent manual edits, revocation and interrupted publication. |

Movie or isolated-episode detection is not inferred from cross-episode matching.
Those items retain explicit chapter/manual/import behavior and a truthful
insufficient-evidence result until a supported content algorithm exists.

## Phase 3: Large-library concurrency and fault/restart recovery

The integrated workload includes scanning, searching, playback, intro analysis
and preview generation. Reuse one controlled environment and corpus while
recording distinct conclusions for each workload and injected fault.

| Work | Required delivery and acceptance |
| --- | --- |
| Capacity contract | Start with 10,000 and 100,000 catalog-item tiers; separately record actual media counts, bytes, formats and scan work. Pin CPU, RAM, PostgreSQL, storage and concurrency. Establish an idle baseline and freeze service/resource thresholds before acceptance runs. |
| Compound load | Cover cold, cached and incremental scans together with Unicode search, filters, exact counts, shallow/deep paging, Resume/Latest, direct playback, remux/transcode, seek, intro analysis and preview generation. Include sorting-policy rebuilds and simultaneous metadata editing at both catalog tiers, measuring rollback and owner survival on statement timeout. Prove actual operation overlap. |
| Resource engineering | Use observed query plans, pool waits, CPU, memory, I/O and child-process behavior to improve indexes, query shape, queue admission and isolation. Preserve playback priority and background fairness; overload has bounded queue/rejection behavior. |
| Storage faults | Inject blocked read/metadata operations, mount loss, changed root/nested mounts, permission failures, full storage and PostgreSQL disconnect/lock waits. Never treat inaccessible storage as missing files or bulk-remove catalog entries. Healthy roots remain serviceable. |
| Recovery correctness | Restore original storage with stable item identity and user state; require explicit rebind for replacement storage. Distinguish request timeout from actual worker termination. Verify resource return, partially published work and temporary output cleanup. |
| Process and host restart | Separately exercise process crash, PostgreSQL restart, clean guest OS reboot and forced guest reset with external observations. Cover late mounts, readiness, interrupted task state and orphaned derivatives/processes. Preserve acknowledged durable state; reconnect and resume from persisted playback progress after recovery. |
| Composed acceptance | Report latency distributions, failures, playback start/seek behavior, resource peaks, supported concurrency and recovery time for each admitted profile. Complete final integrated regression and migration/backup/recovery checks for all new state; close owned resources and retain failures/evidence. |

Do not claim power-loss durability from a service restart or uninterruptible I/O
termination from an HTTP timeout. An unavailable dedicated reboot guest leaves
that acceptance requirement pending rather than silently reducing the scope.

## Delivery records

Each phase's record must identify the implemented field/task inventory, source
and artifact identities, actual selectors/consumers, original failures and repair
results, environment/resource closure, remaining requirements and next action.
Update current status and handoff. Merge and push only verified phase deliveries;
read back the remote commit. Publication and deployment remain separate states.

| Phase | Implementation | Verification | Main publication |
| --- | --- | --- | --- |
| 1. Compatibility API long tail | Complete in the selected matrix | Accepted composed regression, 24-stage actual browser, builds and resource closure | Merged and pushed at `59ce074`; exact remote ref read back |
| 2. Intro analysis and BIF previews | Source complete, including shared tasks, extraction, matching, derivatives, persistence and management | Initial full builds and regression complete; targeted repairs and actual media/browser acceptance pending | Pending |
| 3. Concurrency and recovery | Not started | Not started | Pending |
