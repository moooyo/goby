# Media analysis, compatibility and resilience execution plan

Status: **active; phases 1 and 2 published; the phase 3 session JIT repair at 89b6670 passed all 50 remote regression stages and both builds. Scope11 passed preparation, baseline, rebind and resource admission, but its actual compound repeat failed with another PostgreSQL OOM at 512 MiB. The deployed observer naturally exited 1; failure collection and runtime closure are independently complete, with original failures and the unrestarted PG state preserved. The JIT repair alone was insufficient. Capacity/recovery acceptance and publication remain pending**.

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

Source implementation is integrated. The
[Phase 3 execution record](../development/media-analysis-resilience-phase3-20260922.md)
separates the retained scope10 failure from the current scope11 failure and
accepted repairs.
The session JIT repair at `89b667083a1c5608b9d7e554df27de721ac23c51` passed all
**50 remote regression stages**: 35 ordinary Go packages with **4,279 parent
passes, zero failures and 18 explicit parent skips**; 24 embedded parents; and
91 tests across eight Python scripts. One Vulkan subcase skip remains separate.
Focused library 10, focused database 2 and race 15 passes are separate repeats.
Both Go builds passed; all 73 frontend artifacts were reused from `8b6cb21`
only after comparing input and artifact hashes. The observer passed with four
transient PostgreSQL-file `du` gaps, no persistent loss and zero OOM events;
missing measurements were not zero-filled. Worker, observer, PostgreSQL and
collector are independently closed. This regression cluster shut down cleanly
with 33 custom databases: the restored 26 plus seven new databases. Its complete
physical cluster is now preserved externally after exact guest-copy retirement.
It is separate from the failed capacity cluster below. Scope09's failed fixtures
and the original 26-database regression cluster retain completed external
archival/readback and exact redundant guest-copy retirement.

Scope10 preparation, memory restoration and rebind passed. Compound01 failed
admission on a PostgreSQL banner/SQL version-string mismatch and closed its
controls. Compound02 corrected its execution manifest while preserving the
original preparation references, passed actual admission, and entered cold
concurrency. At **2026-09-23 02:41:37 UTC**, the **512 MiB PostgreSQL cgroup OOM**
killed catalog-owner backend `586572`; PostgreSQL, Goby and the Actor exited.
The driver did not accept or complete the journey. Partial remux-start p95 of
approximately 8,023.7 ms also exceeded the unchanged 5,000 ms target.

The original scope10 failure's physical runtime closure is complete. Its observer
was externally withdrawn after remaining active when the service PIDs disappeared;
its evidence is preserved. Cleanup errors and held fixture restoration remain
recorded. The failed capacity database control state is `in production`; no
database restart, clean shutdown, durable job-state result or fixture rollback
is claimed. Existing log analysis identifies recursive roots/user data JIT plans
as diagnostic leads;
it does not prove a single SQL cause. The Goby session JIT repair is now verified
within the complete regression scope above. A separate observer terminal-handling
candidate passed 15 isolated Python tests on VM106 and closed its worker. Its
scope11 adaptation was subsequently deployed and naturally exited 1 in the new
failure. The effective unit graph was observed without stop propagation;
simulated failure propagation was not tested. No full 10k/100k journey, overload profile or any of the 28
fault/recovery cases is accepted.
External preservation and exact guest-tree plus redundant tar/manifest
retirement are complete for both the four selected scope10 failed-state trees
and the separate clean 33-database regression cluster. Full readback covered
350,821 failed-state members and 13,747 regression-cluster members. All archival,
transfer, readback, retirement and admission controls are independently closed;
each PVE archive retains all four files. The failed scope10 PostgreSQL tree and
its original failure state remain untouched and unrestarted.

Only the per-run `GOCACHE` directories for `regression-scope09repair01` and
`regression-scope10repair01` were additionally removed. Source, frontend,
binaries, module caches, results and logs remain. Cache-cleanup closure measured
21,970,751,488 free bytes, meeting the unchanged 21,676,163,072-byte initial space
requirement. It does not establish a new capacity admission. Scope11 subsequently
passed fresh bootstrap identity/resource admission and deployed the verified
build. Actual SQL-version matching, managed lowering, Actor access and complete
fixture preparation passed. The fixture records a 9,342-item seed catalog plus
658 pending media files, 5,780 media paths, all 4,200 stress directories and
336,000 nonmedia entries. The 340,273 unique inodes cover the entire fixture.
The observer passed resource observation without persistent loss. The collector
and all preparation controls are independently closed; the shared parent is
absent, App memory is restored to 1280 MiB, and PG remains at 512 MiB with both
original native lifetimes preserved. Final preparation closure is
`99cbafc450deb9b2bb0de9913e7d910e97688738ab477773c612a540816177a8`.
Baseline and all ten rebind checks subsequently passed and their workers closed
independently. The new manifest changed only `run_id`, preserving original inputs.
All compound admission gates passed and the full cold/cached/incremental driver
was dispatched. At **2026-09-23 07:28:11 UTC**, PG again OOM-killed at 512 MiB,
killing backend `700735`. The driver failed with `http_status`,
`accepted=false` and `execution_complete=false`; partial remux-start latency p95
was 7,859.178006 ms against the unchanged 5,000 ms target. The deployed observer
naturally exited 1 through its failed-terminal branch, retaining `complete=false`
and unknown cgroup closure. The JIT repair alone was insufficient for this workload.
Failure collection and original runtime/launcher/parent/collector closure are
independently complete. App/PG autostart is disabled; failures were not reset.
PG remains `in production`, unrestarted, with unchanged control-file hash; no SQL
readback or filesystem rollback occurred, and durable job state is unknown.
The current fixture and PG tree remain unarchived and undeleted. The retained
PG log contains 3,355 plan entries with zero JIT entries; no new OOM root cause
is established. Next analyze that retained evidence and decide a bounded repair
at unchanged limits. Capacity remains unaccepted.
Phase 3 remains unpublished;
phases 1 and 2 are merged and pushed at
`e41febbb36687d04340f5c651f4bf1bf376a4310`.

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
| 2. Intro analysis and BIF previews | Complete at product `49fc4ec`, with fixture-only `e94173f` correction | Accepted builds/regression, calibration/controls, all 14 real cases, four preview consumers, actual skip, cancellation, prune, restart, and independent resource closure; original failures and scope limits retained in the delivery results | Merged and pushed at `feb5004`; exact remote ref read back |
| 3. Concurrency and recovery | Core sources and Goby session JIT repair integrated at `89b6670` | Complete 50-stage regression, both builds and regression closure accepted. Scope11 preparation, baseline, rebind and resource admission passed; the actual compound repeat failed with PG OOM at 512 MiB. The deployed observer exited naturally; failure collection and runtime closure are independently complete. Full capacity journeys, overload and fault/reboot acceptance remain pending | Pending |
