# Current execution plan

Reviewed on 2026-09-14 after MP3, FLAC and Episode01. Status: **product and
candidate verified; MP3 and FLAC accepted; video client acceptance open**.
This is the active queue. [Current status](../development/current-status.md)
records accepted facts. [Delivery and verification](delivery-and-verification.md)
retains all M2-M6 obligations; M7 remains deferred. Historical plans and consumed
inputs are evidence, not alternative execution instructions.

The goal remains an independent Linux media server using Go, PostgreSQL and
FFmpeg, an administrator-only React/MUI dashboard and unmodified compatible
clients. This review adds no consumer player and removes no release obligations.

## Review conclusion

The delivery direction remains appropriate: verify the product, admit an
isolated candidate, establish supported real-client behavior, then rehearse
recovery and promote main. Immediate ordering and completion claims needed
correction. Episode01 has already run. Its visible playback completion cannot
pass acceptance while three page errors remain unclassified. Those errors do
not invalidate accepted MP3/FLAC results or justify unrelated full-suite reruns.

The next increment must add a small amount of discriminating evidence. Use
native rejection reason metadata in the existing adapter. Defer Debugger,
async stacks and extra network instrumentation unless a concrete later question
establishes their value. Preserve the page-error rule and every original failure.

## Accepted baseline

| Area | Accepted result | Remaining limit |
| --- | --- | --- |
| Product | TV parent metadata and earlier fixes passed [2,270 tests/25 packages with race instrumentation and a Linux build](../development/tv-parent-metadata-full-verification.json), without failures or skips | Reuse this exact source and binary; this is not client acceptance |
| Candidate | [TV successor transition](../development/tv-parent-candidate-transition-closeout.json) and [affected admission05](../development/tv-parent-affected-admission-closeout.json) passed | Admission05 checks changed TV projections/access and reuses admission04 for unchanged contracts; main is separate |
| MP3/FLAC | Both declared journeys, physical delivery, durable state and owned cleanup [passed](../development/audited-flac-client01-closeout.json), with zero page errors | Only these client/media profiles pass; no audible-output or general codec claim |
| Episode01 | Full TV browsing and playback controls completed; [owned state is closed](../development/audited-episode-client01-closeout.json) | Three page errors preserve the formal rejection; neither episode nor overlapping TV browse acceptance passes |
| Movie | Movie05's two counted play chains and movie06's pre-playback failure have closed owned state | Four old page errors remain unknown; movie06 did not play; the old movie05 baseline is stale for another run |
| Main | Source32/schema27 installation retained; [replacement upgrade contract](../development/audited-main-upgrade-plan.md) remains a draft | No main/source55 restart or upgrade is established by these client increments |

The selected binary is
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`,
under epoch `76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac`.
Episode01 closed twenty revoked sessions, eleven play rows, five userdata rows,
two retained audio references and no encoding jobs. Its new unstarted Prepared
row is explained by recorded detail PlaybackInfo responses and stays retained.
All earlier rows remain preserved. Later inputs bind this closed state, not
the old seventeen- or nineteen-session snapshots.

## Immediate queue

| Order | Concrete deliverable | Completion or stop condition |
| --- | --- | --- |
| 1. Episode01 closeout — complete | Replay saved physical/durable evidence while reproducing the original UI rejection; close workers and check candidate/PostgreSQL continuity | Preserve all three errors, the counted Stopped row and the uncounted Prepared row; no business replay |
| 2. Minimal rejection observation | Add bounded native unhandledrejection primitive/Response metadata to the existing adapter; verify only the changed behavior remotely | Preserve pageerror propagation, redaction and Stop/Logout. A Response request ID must uniquely match response metadata; timing cannot attribute an error |
| 3. First subtitle increment | Freeze one unconsumed subtitle input against the current state, verified adapter and fixed client/media under existing limits | Verify supported SRT/VTT, playback, physical delivery, durable state and cleanup. Collect diagnostics as secondary evidence; close owned state before another run |
| 4. Select the next video correction | Review the subtitle outcome and any new direct error evidence | No recurrence only means this scenario did not reproduce the error. Undefined stays unattributed. Do not retry video or add another diagnostic layer automatically |

The [Episode01 review](../development/audited-episode-client01-review.md) fixes
the diagnostic hypothesis, limits and remote checks. Limit this diagnostic
increment to one focused work session, with a 60-minute investigation ceiling
before reassessment. Finish implementation and synthetic verification before
admitting the subtitle input. Subtitle acceptance is that run's primary result;
diagnostic collection does not authorize extra playback attempts.

If two successive tool/checker failures block the same planned observation,
pause that experiment and reassess its shared cause and value. Preserve failed
evidence and reconcile actual state. Do not create renamed attempts or another
general runner. Missing direct causes remain explicit; independent source
review and release preparation can proceed while affected claims remain open.

Movie, TV browse and episode actors are consumed. A future run needs reviewed
retained-state input and saved-state verification through the existing contract.
Do not reset accounts, use a null baseline, reuse stale movie05 state, or add
per-attempt constants. Overlapping coverage closes another gate only when its
complete declared checks and overall closeout pass. Episode01 does not qualify.

## Remaining delivery order

1. Close supported core original-client behavior: login/browse, movie and TV
   direct play, pause/seek/stop/resume, MP3/FLAC, external subtitles, durable
   state and user isolation. Reuse accepted unchanged journeys and controls.
2. Complete fresh main safety/recovery evidence and one native recovery point.
   Rehearse two distinct isolated restorations: selected binary to schema28,
   and actual old source32 binary to schema27. Then admit a bounded promotion
   and post-upgrade workflow, subject to the core gate.
3. Close remaining M2-M6 capacity, blocked-storage/reboot, wider media/transcode,
   administration extensions, actual GPU profiles, arm64/OCI/embedded assets,
   license/notices and feature compatibility. Publish exact support rows.

Read-only main preparation can proceed independently. Promotion still requires
core acceptance and fresh safety/recovery evidence. The old source55-specific
main plan and runners remain superseded. A new-binary schema27 restore migrates
to schema28 and does not establish old-binary rollback. A cancelled ready
restore retains its inactive staged database; bind that occupied state explicitly.

## Gate boundaries

| Gate | Blocks | Independent work that remains possible |
| --- | --- | --- |
| Source identity, authorization, ownership, preservation and recovery safety | Affected admission, mutation or promotion | Read-only diagnosis and preparation |
| Supported core real-client regression | Main promotion claiming that playback workflow | Unrelated API/unit work and main read-only preparation |
| Positive global NextUp and client behavior | Complete NextUp selection/order/refresh claims | Unrelated verified playback and an explicitly partial internal checkpoint |
| Positive LibraryChanged automatic refresh | Automatic-refresh compatibility claims | Core playback and an internal upgrade with the limitation recorded |
| Complete supported release profile | M6 completion or broad compatibility release | Partial milestones with exact scope and known gaps |

Keep global NextUp and automatic-refresh discovery parked. Matrix07's empty
global results and two zero-history clients' missing NextUp requests are
inconclusive. Reopen only for a recorded decision with new discriminating
evidence, such as a client-issued query or positive public reference state.
Do not replay consumed experiments, reset budgets or treat missing requests
as empty responses. The [comparison contract](../development/nextup-goby-comparison-contract.md)
remains parked. Reference inspection permits public APIs, visible behavior and
network/error metadata; vendor source and databases remain excluded.

Historical service exits remain unresolved. Existing diagnostics and bounded
candidate stability support further work; readiness cannot establish the old
cause. Do not repeat starts to invent causality. A newly evidenced unsafe
condition blocks affected promotion until resolved. Missing hardware blocks
its profile, not unrelated software checks. License/notices must close before
external distribution. A limited internal release is not M2-M6 completion.

## Execution and verification

All tests, builds, validators and runtime/media/browser checks run through
`ssh test-env`; no local fallback is authorized. Read-only reviews may run in
parallel. Serialize shared-fixture writes and heavy remote jobs. Check available
resources before admitting a new browser or heavy verification scope.

Map each changed behavior to the smallest meaningful remote check. Tool-only
or documentation edits do not require another full product suite. Product
changes require relevant regressions and one final full verification of the
frozen snapshot. High-risk ownership, credential, migration and recovery
transitions retain independent review and closure.

Reuse tested operators and immutable receipts. Pin the history actually consumed
and distinguish it from freshly checked live resources. Do not claim narrower
checks reverified every historical root. Keep DTO/storage mappings explicit;
preserve raw failed evidence privately and publish safe projections.

Each increment fixes expected results, permitted writes, protected boundaries,
limits and stop conditions before execution. Close owned resources before the
next business scenario. Unknown errors cannot be waived from timestamps alone;
they do not justify adding Live TV, enabling external egress or synthesizing
vendor registration responses.

Publish one concise closeout covering identity, relevant checks, product result,
cleanup, limitation and next action. Commit and push a reviewable checkpoint
under the existing policy. Documentation, tool verification and owned-state
closure must not be reported as client acceptance or deployment completion.
