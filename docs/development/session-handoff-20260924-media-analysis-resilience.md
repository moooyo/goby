# Media analysis and resilience session handoff - September 24, 2026

The [September 26 handoff](session-handoff-20260926-media-analysis-resilience.md)
supersedes this document's current-state and resume instructions. This document
is retained as historical evidence; its scope13 service state is no longer live.

## Closeout decision

The user requested that the current code be merged into `main` and pushed, then
handed off for continuation in another session. Product/source checkpoint
`e257694646b74f0d6e50d1d7196423a93e743e97` was fast-forwarded into `main` and
pushed on September 24. This is a checkpoint publication. Phase 3 capacity and
fault-recovery acceptance remains open. Resume the full approved scope; do not
replace its acceptance requirements with the narrower checks already passed.

No pull request or production deployment was performed. The original
`D:/Code/goby` checkout has 19 unrelated dirty paths that must remain intact.
The implementation worktree is
`C:/Users/moooyo/.codex/worktrees/media-analysis-resilience/goby`, branch
`codex/media-analysis-resilience`. Documentation closeout follows the product
commit; use Git to read the final documentation commit rather than guessing it.

## Scope and delivery state

| Work package | State |
| --- | --- |
| Compatibility API long tail | Phase 1 accepted and published. |
| Automatic episode-intro detection and source-bound BIF/drag previews | Phase 2 accepted and published within the recorded corpus and client-adapter scope. |
| Large-library scan/search/playback concurrency, storage blockage and host restart recovery | Implementation and test tooling published; complete acceptance pending. |

Live TV, EPG, DVR/scheduled recording, tuners, DLNA, external channels and group
synchronized playback remain explicitly excluded. Other unselected features
remain deferred. Preserve the accepted third-party-client adapter boundary.
Profile PINs remain encrypted at rest, returned only to the owner's logged-in
client for profile locking; password authentication remains the login mechanism.

All tests, builds and runtime checks for this increment are remote-only on the
owned VM106 verification environment. Use PowerShell for local Git/source work.
Do not run local verification. `ui-ux-pro-max` remains disabled for this task.

## Changes included in the published checkpoint

The branch includes bounded disk-backed scan evidence and reconciliation,
PostgreSQL staging/paging, storage observation, runtime resource reporting and
the capacity/fault test drivers. Follow-up repairs cover catalog ownership and
admission lock contention, PostgreSQL JIT/retained statement memory behavior,
leaf UserData queries and notifications, and joint video/AAC remux seeking.

The last two product/test commits are:

- `a1d30b6`: bind each of the three playback modes to a fixed, distinct native
  authenticated client session for the same viewer. Query/direct retain the
  primary session. The private Actor context is v2; public manifest/result
  schemas and global/user/session limits remain unchanged. CPU utilization now
  uses timestamps surrounding each service's actual counter read.
- `e257694`: preserve a bounded first progress-parser rejection and the previous
  complete progress update, then log it through the actual diagnostic handler
  after job finalization. Logs contain fixed classifications, bounded numeric
  values and a line digest. Raw stderr, paths, credentials and arbitrary text
  are excluded. Parsing, cancellation and `ErrProgress` priority remain intact.

The diagnostic commit has not established the cause of the earlier intermittent
transcode stream failure.

## Verification already completed

The last complete source-equivalent regression/build composition is bound to
`3832a4d8fb3000137e68ade09d1136dcd6101eff`: 35 ordinary Go packages, 4,282 parent
passes, zero parent failures and 18 explicit skips. The composition reuses 47
accepted scopes and adds three freshly completed scopes; the original failed
run remains failed. Python, embedded, focused and race scopes retain their
separate evidence and must not be added together as unique test coverage.

The current `e257694` checkpoint has a separate, successful remote verification:

| Scope | Result |
| --- | --- |
| Complete diagnostics package | 45 parent tests passed, zero failed/skipped. |
| Selected transcode regression | 18 parent tests passed, zero failed/skipped; includes real FFmpeg encoding, remux/audio output, video containers and seeking, parser rejection and Manager-to-handler logging. |
| Preparation/workload Python suites | 26 + 54 tests passed. |
| Ordinary and embedded-front-end application builds | Both built successfully from the actual source archive. |
| Verification worker closure | Native worker and its cgroup independently confirmed closed. |

The 73 frontend artifacts were reused only after all tracked frontend inputs and
artifact hashes matched. Current verification is focused regression and builds;
complete regression for the final resumed delivery remains required.

Private evidence root on VM106:
`/opt/goby-phase3-campaign-20260922-01/private/progress-diagnostics-repair01`.
The `build-delivery.json` SHA-256 is
`9e1c927737bc1be04ea924651c350ab12d5cc752d8edcc1c7492f9f4461e44f9`.
Ordinary binary SHA-256:
`b253e29711c3a683f0fe2fab16fd2f8ad58c015770871e0eea3012aef96ce685`.
Embedded binary SHA-256:
`f0b13f0edf90a9062074a1a98a41dc03cd6d1cb0b6cc2435103dc625993941cd`.
Keep credentials and raw private contexts outside the repository.

## Current failure and diagnosis

The last actual 10k compound run, scope12, failed during cold execution after
approximately 79 seconds. Cached and incremental phases did not execute.

- A progressive transcode response stopped after partial delivery. Its encoding
  job recorded `process_progress`; the rejected progress line was not captured.
- Remux-start p95 was approximately 9,061 ms against the unchanged 5,000 ms
  target. All playback lanes shared one authentication session, so the configured
  one-job-per-session limit serialized the two conversion modes. The fixture
  correction now uses distinct real sessions; integrated latency still needs
  remeasurement.
- CPU sampling reported approximately 222.7% against 205%. The denominator used
  an earlier broker timestamp rather than the counter-read time. The correction
  is tested, but historical measurements are not retroactively accepted.

Twenty independent executions of the same runner/Plan/media passed: ten at
ordinary speed and ten at a 25% CPU quota. They did not include the full
Manager/HTTP/scan combination and did not resolve the original stream failure.
The next real compound run should use the new structured rejection diagnostic.
Do not relax the parser, timeouts, concurrency limits or acceptance thresholds
merely to obtain a passing result.

Scope12 App and PostgreSQL were shut down cleanly and their exact units disabled.
Its failed compound evidence and all retained data remain intact. Older failed
scope10/11 PostgreSQL generations must not be restarted or cleaned speculatively.

## Test environment at pause

The authorized VM106 expansion is complete: 53 GiB configured disk, 5 GiB RAM,
two CPUs and no swap. The expansion is not a fault-matrix reboot result.

Fresh scope13 is provisioned from `e257694`. Its application and PostgreSQL are
retained idle for continuation. Preparation temporarily reduced the application
cap to 512 MiB; PostgreSQL is also capped at 512 MiB. The application must return
to 1280 MiB after preparation closes, before compound acceptance. The shared
preparation control slice is empty with a 128 MiB maximum and 64 MiB high mark.
No Actor workload is running.

Scope13 memory lowering and real Actor/Goby isolation/broker access passed.
The first preparation controller attempt then failed before starting the Actor:
the observer's `READY` flag was left false while the release operation changed
only `RELEASED`. This was a controller release mistake. Its publisher and observer
are terminal, their original processes/cgroups are absent, and the failed native
states and evidence are preserved.

Independent readback confirmed: the Actor unit has never started; its entry,
output and fixture paths do not exist; no Actor-UID process exists; bootstrap is
uninitialized; all eleven checked business tables are empty. The same provisioned
application/database can support the Actor's first launch after fresh checks.
Reobserve actual identities and state before relying on this saved observation.

A source-only successor is prepared under the local private
`phase3/capacity-runtime-10k-13-binding01/preparation-recovery02` directory. It
uses new observer/publisher/source/control-output and SQL-gate namespaces and
preserves the old failed attempt. It has not been released or executed.

Release details matter:

1. The observer uses a top-level `READY` gate.
2. SQL gate and final dispatcher use top-level `RELEASED` gates.
3. The launcher template stays `RELEASED=False`, with both version-gate hashes
   set to `None`, until the dispatcher obtains real SQL evidence and renders it.
4. Change only the intended variable assignment. Global replacement of the
   string `RELEASED = False` also corrupts the dispatcher's template markers.
5. Bind new source hashes, a fresh SQL-gate time window, and current native facts.
   Preserve the existing profile/operator/manifest and first-use Actor namespace.
6. Release the collector only after actual worker/observer results and identities
   exist. Update downstream compound inputs to the actual successor receipts.

The provision release window and the first SQL-gate window expire. Expiry does
not authorize replaying either completed provision or a consumed control attempt.
Use the successor's fresh release window and revalidate current state.

## Resume procedure and remaining acceptance

Start with the latest local private handoff:
`D:/Code/goby/.git/media-analysis-resilience-20260920/phase3/execution-current-20260922.md`.
It contains exact native PID/start/invocation/cgroup identities, pins, SSH-config
location, safe receipts and the authoritative latest transition. This file and
the checkpoint are local operational memory, not repository publication inputs.
The successor's `bindings-required.md` lists its exact remaining bindings.

1. Inspect `main`, the implementation worktree, the private handoff and the same
   VM/service handles. Preserve unrelated local edits. Keep the original failed
   controllers and old PostgreSQL generations untouched.
2. Finish and review the recovery02 controller release. Confirm every actual
   release gate explicitly before dispatch. Prove the Actor is still unused,
   then complete preparation, independent closure and App memory restoration.
3. Complete fresh 10k and 100k cold/cached/incremental scan/search/playback runs
   with intro/BIF work, latency/resource measurements, sorting and metadata
   rollback checks, after-compound reconciliation and overload/recovery checks.
4. Complete all 28 fault cases, 14 per tier: blocked storage reads/metadata,
   root/nested mount loss, permissions, PostgreSQL disconnection/locks/restart,
   process crash, normal VM restart, forced reset, late-mount restart and ENOSPC.
   Retain durable external acknowledgements, state readback, actual descriptor
   closure and resumed playback. Partial cases do not establish matrix acceptance.
5. Run final integrated regression, update delivery evidence and documentation,
   and publish the remaining accepted changes. This checkpoint merge does not
   reduce these outstanding requirements.

The active objective is paused at the user's request for continuation in another
session. No recurring automation was created and no test controller should
restart automatically.
