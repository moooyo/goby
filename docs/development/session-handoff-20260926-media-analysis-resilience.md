# Media analysis and resilience handoff - September 26, 2026

## Closeout decision and acceptance boundary

The user stopped further acceptance work and requested a merge into `main`, a
push, and a handoff. This is a source checkpoint, not Phase 3 acceptance.
The product revision is `cd91390a78ec5b03831a77dbd4ac97265d116847`; the following
documentation commit records the closeout. Read Git history for that commit's
actual identity and the final published `main` tip.

Phases 1 and 2 retain their previously recorded acceptance. Phase 3 remains
incomplete: neither complete capacity tier, either tier's overload/fault work,
the 28-case real fault matrix, nor the final 54-stage regression is accepted.
The six required VM boot transitions are part of those 28 cases, not six
additional cases. No production deployment or pull request is part of this
closeout. Resume only when the user asks to continue.

The existing `goby` heartbeat has been paused. No new test was dispatched after
the closeout instruction. The 19 pre-existing unrelated dirty entries in
`D:/Code/goby` remain outside this delivery. Preserve them, including the local
Dockerfile snapshot-fetch and recipe-permission changes.

This document supersedes the current-state and next-action sections of the
[September 24 handoff](session-handoff-20260924-media-analysis-resilience.md).
Earlier failure and verification records remain historical evidence.

## Code included since the previous published checkpoint

The implementation branch is `codex/media-analysis-resilience`, with a clean
product worktree at
`C:/Users/moooyo/.codex/worktrees/media-analysis-resilience/goby` before this
documentation update. Eleven committed changes follow published base
`a73f510498a4695e0db9987983c225fbc95ac87a`:

| Commit | Change |
| --- | --- |
| `18d4690` | Repair transcode admission under retained-history pressure. |
| `cbf3b88` | Reuse identical capacity HTTP evidence without reducing the workload. |
| `9ceed92` | Compose complete fault evidence and support expanded guest inventory. |
| `6a832b6` | Preserve unknown FFmpeg copy-timestamp progress; include the pinned toolchain patch. |
| `58d2bf4` | Verify toolchain signatures without requiring an agent daemon. |
| `86fd13c` | Compile the progress regression against public FFmpeg headers. |
| `c987081` | Allow the pinned successor process observer in capacity workloads. |
| `8cd7a3d` | Repair deep-preview frame-selection expressions. |
| `98489a5` | Bound scan-claim lookups and specialize Movie projections. |
| `30673a2` | Defer Movie page projections until after pagination. |
| `cd91390` | Reuse media-tool digest state and avoid count-only page reads. |

The private scan instrumentation and the later performance candidate patches
are not applied to this product revision. No new code validation was run as
part of the documentation/merge closeout; the remote results below retain their
original source and scope.

## Verification completed before closeout

| Scope | Actual result and limit |
| --- | --- |
| Affected development tests | 59 ordinary and 59 race tests passed and their workers closed. These are separate executions, not 118 unique tests. |
| Formatting02 | Passed. |
| Formal suite05 | Nine stages passed and closed. The library/server/media ordinary parent totals are 979/1,014/552: 2,545 passes, 11 source-explained skips, zero failures. The 73 race passes are reported separately. |
| Fresh media02, full media02, preview replay | Passed and independently closed within their recorded native-media scope. |
| Product07 | Current product delivery and independent closure passed. This is product/build evidence, not capacity acceptance. |
| Reader02 | The complete reader qualification passed in its actual 64 MiB child / 128 MiB parent and closed independently. Do not repeat earlier failed readers or count a diagnostic as this qualification. |
| Growth02 | All four storage-growth stages passed and closed; the guest is at 125 GiB. No resize or reboot is needed merely to resume. |
| Current125 source05 / scope24 hardware seeds | Materialized successfully and independently closed. Only source/profile/manifest seeds were produced; no scope24 business workload ran. |

Product07 delivery is 91,675 bytes, SHA-256
`59ad3a2321ba3466982dcbeb4540f8886d6599dc6b27d132b92127c8e4e1fecd`.
Its independent closure is 8,626 bytes, SHA-256
`6e43a37144cc0c831682fd672ff74b14f552cc98023b7dba645c68c3e280ca7b`.
Reader02's result is 6,596 bytes, SHA-256
`6921f482470bdafd87ec3c6c9d1d054926268d3974933a9bbdc02a54217e5fe2`.
These are safe references; private runtime facts, operators, credentials and
database URLs must remain outside Git and public documentation.

## Latest failure and diagnostic findings

The last formal 10k attempt, scope23, failed with `request_budget` during its
cold phase, after 4,975 of 5,766 expected scanned items. Cached and incremental
phases did not run. Its result remains failed; the Actor, observer and controls
were closed, and App/PostgreSQL were normally retired. Scope23's result is
22,915 bytes, SHA-256
`b4987896fc4c845b670927aaa63738f7ec9161f29b7c935fcc62b622425ca82c`.

A separate instrumented cold03 diagnostic also exhausted the request budget:
5,104 scanned / 5,103 updated / zero added, against the expected
5,766 / 5,108 / 658. Its target job lasted about 1,157.18 seconds before
cancellation. This private diagnostic binary is not a qualified product build.
Preparation03 and memory restoration passed; the diagnostic Actor and observer
closed, App/PG shut down normally, and the diagnostic control parent closed.
The observer's result has `complete=true` but `diagnostic_complete=false`
because of prefix/backlog gaps. Do not describe its timing evidence as complete.

The retained target-job telemetry attributes approximately 43.7% of media-step
elapsed time to probing, 13.9% to subtitles, 10.5% to progress, 9.3% to images,
and 2.0% to before/after catalog snapshots. Nested timings are not additive.
The sampled business CPU window averaged App 90.5% plus PG 78.0%, using the
one-CPU percentage scale. The Actor's whole-service CPU total covers a different
window. These observations identify costs; they do not prove one exclusive
bottleneck or demonstrate a performance fix.

The private diagnostic had unnecessarily shared CPUs 0-1 with auxiliary work.
Fresh scope24 assigns Actor CPUs 2-3 and observer/control CPUs 4-5 while keeping
App and PG on CPUs 0-1 and preserving their original limits. Do not claim that
the historical formal23 Actor had the same CPU restriction: its retained unit
has blank `AllowedCPUs`/`CPUAffinity`, and its historical effective mask was not
retained. The benefit of scope24's new placement has not been measured.

Elapsed time also accumulated in repeated execution-tool repairs: stale scan
configuration hashes, file-mode propagation, cross-UID `/proc` access, observer
identity assumptions, and publication/admission ordering. These were operational
failures, not new product acceptance. Resume with the frozen execution entry
points below; avoid rebuilding optional audit layers before real 10k progress.

## Environment and exact stopping point

VM106 has 8 vCPUs, 8 GiB RAM, no swap, and a configured 125 GiB disk. The observed
boot is `47ff6bec-0355-4b1e-ab4c-4d0cf9f3d8ce`; machine identity is
`6f86a64c2e50efa70a16e74c07d5eed1`. At closeout on September 25 around 19:22 UTC
(September 26 around 03:22 Asia/Shanghai), read-only inspection found no running,
activating or deactivating `goby*` service and no process under the reserved
60000-64999 UIDs. Retained failed native units and all evidence were preserved.
Reobserve before relying on these historical facts.

Use these path abbreviations for continuation:

| Name | Path |
| --- | --- |
| `P3` | `D:/Code/goby/.git/media-analysis-resilience-20260920/phase3` |
| `L` | `P3/scan-claim-repair01` |
| `Q` | `P3/query-page-repair01` |
| `N` | `P3/capacity-runtime-10k-24-binding01` |
| `R` | `/opt/goby-phase3-campaign-20260922-01` on VM106 |
| `B24` | `R/private/tier10k24-binding01` |
| `C05` | `R/private/capacity-current125-source05` |
| `O24` | `R/private/capacity-tier10k-24` |
| `S24` | `/var/lib/goby-phase3/tier10k-24` |
| `CODE` | `/opt/goby-phase3-runtime-setup-20260922-01` |

Fresh24 is stopped at **source upload only**:

- The complete source packet is frozen at `N/source-freeze.json`:
  21,060 bytes, SHA-256
  `5f56acec8313d42615af551eb1e49b5667efe1a1cccfb0fa25455db3ae64d889`.
  Its original 37 core, five entry and two closer members remain intact.
- `N/source-recipe.json` is 29,419 bytes, SHA-256
  `607ac6e8e8a5abc745d355414c7596870b96270e00d3497f7e15a7629ed710cf`.
  The source-origin map and actual core parameters are alongside it.
- The already completed hardware worker is
  `goby-phase3-capacity-current125-source05-scope24-core.service`, original
  PID 1724759, invocation `2791b7136c7a4f789d282ce7f7119e0f`.
  Its independent closure, `C05/scope24-core-independent-closure.json`, is
  5,595 bytes, SHA-256
  `58ca4aaf18fc453d4d3c87199d58870bc8bcec4bad5387ccb6de13bb1a9affb8`.
- Reuse `B24/reviewed/current125-product07-05/source-index.json`,
  18,136 bytes, SHA-256
  `4e5995c4a019e88174a4d79b3f42da4368fedf7efe52dfdbdc4849270300a71a`.
  Do not rerun this consumed hardware batch or recreate its existing directories.
- ROOT created `N/root-stage01/` with exact source copies, a staging manifest,
  and two release copies changing only the materializer/publisher's top-level
  release assignment. Frozen candidate files remain unchanged.
- `N/root-stage01.tar` was uploaded to
  `B24/sourceupload01/package.tar`: 1,230,848 bytes, SHA-256
  `97758d1fc39c0a51e91bb491ff001e4b9dadb7127ad2ce61b5feb11e10d46f4b`.
  It was **not extracted or executed**. There is no materialized core, no
  capacity publication, no scope24 account/service/database/fixture, and no
  `S24` runtime directory. The upload is not a source-publication receipt.

The current operational binding window03 is September 25
18:55:29.145679900-22:55:29.145679900 UTC. It was a finite ROOT-selected operating
window under the continuing task, not a new user approval or an extension of
the expired unattended period. The closeout instruction stops new dispatches
even before that window expires. On a later resume, recheck actual authority,
clock, boot and freshness; preserve old bindings and publish any necessary
successor separately. Do not blindly execute a staged release with an expired
or mismatched window.

## Remaining work, in execution order

1. Resume the actual fresh24 chain in `N/RUNBOOK.md`, after refreshing its
   current authority and live inputs. Verify the staged tar and origin map;
   materialize the 37 core members; observe unused accounts/IDs/ports and real
   VM/PVE archive capacity; publish capacity inputs and provision. Hardware
   seeds are already complete. The five later entry readers are not a
   prerequisite for the first full compound.
2. Complete preparation, its SQL/version and Actor access gates, original
   producer/observer/collector closure, App memory restoration and control-parent
   closure. Confirm App 1280 MiB and PG 512 MiB before the benchmark. A prepared
   fixture is not accepted capacity. Guarded recorders/closers must run in their
   specified native control units, not directly in an unrestricted SSH cgroup.
3. Publish the original compound helpers; obtain actual baseline and execution
   rebind evidence; run the complete 10k cold/cached/incremental journey. Keep
   two query lanes, three playback modes, productive intro/BIF overlap, exact
   totals/order, metadata editing and sorting-timeout rollback. Preserve the
   40,000-request budget, pacing, all SLOs, business CPU/memory and independent
   closure requirements. No partial scan or cleanup success supplies acceptance.
4. After accepted compound, complete that tier's foundation/volume preparation,
   catalog/filesystem reconciliation, fault extension and sentinel, read-only
   media remount and normal-deployment overload. Bind fresh current-source
   after-compound, preboot, tool, external-controller and PVE evidence as required
   by each consumer. Historical graph recovery is not a blanket prerequisite
   for a new 10k run; specific later consumers still require their stated inputs.
5. Execute and independently close 14 real fault cases for 10k in this order:
   blocked read; blocked metadata; mount loss; changed root mount; changed nested
   mount; permission failure; PostgreSQL owner disconnect; PostgreSQL lock wait;
   App crash; PostgreSQL restart; normal guest reboot; PVE reset; late-mount
   reboot; isolated derivative-cache ENOSPC. The last case requires the actual
   normal13 gate and normal-to-fault-cache transition. Preserve every original
   journal, request, ACK, native identity and failure.
6. Admit a fresh 100k tier and repeat its own preparation, all three compound
   phases, reconciliation, overload and 14 distinct fault cases. Both tiers and
   their six actual boot transitions remain pending. Maintenance reboots and
   service restarts do not fill the required boot cells.
7. Compose the complete external 28-case matrix and close its original process
   tree. Run a current-source final54 successor in a fresh regression/PG scope:
   package inventory, 35 full ordinary packages, two race stages, nine Python
   suites, both fresh builds and their metadata/version witnesses, and the full
   embedded command suite. Close worker, PG and collector independently, then
   finish the acceptance ledger and final documentation.

The exact case IDs, witnesses and 54 stage names are preserved in
`P3/full-phase3-remaining-inventory-20260925-01.md`. Its earlier current-state
paragraphs are superseded by this handoff and the latest execution log; its
original scope and dependency inventory still apply. There is no reliable
completion-time estimate until a complete capacity run succeeds.

## Preserved optional candidates and evidence recovery

The following remain private SOURCE-only candidates, unmerged and untested:

- `L/scan-stage-cost01-product-candidates01`: duplicate outer query-visibility
  removal and a ForceProbe identity-snapshot specialization. The latter is low
  priority given the measured catalog-snapshot share.
- `L/scan-stage-cost01-subtitle-noop-candidate01`: skip two projection reads only
  after the original checks establish empty retirement and inspection sets;
  retain the real commit and error behavior. One regression test accompanies it.
- `L/scan-stage-cost01-product-regression-source01`: baseline regression sources.
  Before applying a candidate, run its new tests on the remote baseline with
  the real private test DB environment; missing-DB skips provide no credit.
  `L/scan-stage-cost01-go-regression-reuse-notes.md` identifies the existing small
  runner and proven PG setup API. Do not create a large new harness for this.

Do not remove ForceProbe, fresh tool-byte/version checks, seek/decoding witnesses
or workload pacing to obtain a pass. Images/progress optimizations are notes,
not implemented fixes; none of these candidates has a demonstrated benefit.

Historical recovery copied 6,613 readable original forms and closed the raw-nine
collection. Missing original paths remain missing. Official Debian bytes supplied
the matching old Python payload without installation/execution; a single offline
rebuild reproduced the old Goby binary exactly. Recovered content must not be
relabelled as an original retained path/stat record. Graph06's real walk remains
FAILED/CLOSED with 16,809 unresolved forms and 29,088 failed edges; source QA
passes do not change it. Graph07 source and the 97-batch original-host follow-up
inventory are prepared but not run. Eleven VM metadata inventory batches passed
and closed. Preserve these artifacts and defer further recovery until a concrete
downstream dependency needs it.

## Continuation rules and operational records

All tests, builds, imports, AST/syntax checks and runtime probes remain remote
on the owned VM106; local PowerShell text/Git/hash and captured-data work is
allowed. ROOT is the sole remote executor. Reuse existing agents for SOURCE/DATA
work; do not spawn new agents for this continuation. Original `test-env`/VM101
is read-only artifact recovery only, with no tests, writes or services.

Use the existing SSH configuration at
`P3/guest106-creation-01/ssh/config` with alias `goby-phase3-106`. Never copy private
keys, runtime-facts bodies, operator contexts or DB URLs into the repository.
Do not clear failed native states, remove evidence, reuse consumed scopes or
infer process closure from an observation timeout. Native failure and successful
maintenance closure are separate facts.

The authoritative operational continuation log is
`P3/execution-current-20260922.md`. Safe captured results are under
`Q/capacity23-actual`, `Q/capacity23-retirement01`,
`Q/scan-stage-cost01-cold03-actual`, and `Q/current125-source05-actual`.
The closed diagnostic timing summary is 9,605 bytes, SHA-256
`79ed3c4c20ad700d0454e3e198605cb46282c63f9d7f70d564cb8b8933ad12ac`.
The local closeout preservation records are under
`P3/closeout-20260926-01`. These private records are intentionally not committed.

The user's closeout authorization covers this merge and push. It does not turn
the pending Phase 3 acceptance into a pass or authorize an automatic restart of
the paused work.
