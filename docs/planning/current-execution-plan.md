# Current execution plan

Reviewed on 2026-09-15 after candidate recovery and the M5 media-refresh
increment passed focused/browser acceptance, final ordinary regression and
independent result/resource closure reviews.
Status: **the fixed M5 refresh increment is accepted and committed; the internal
amd64 systemd package build is verified and closed; its first installation
prepared successfully but failed at controller entry before Goby started.
Owned resources are now preserved and closed; installation attempts are paused
pending execution review. The scoped source checkpoint is committed as `beaea34`; core-video
and complete release gates remain open**. Earlier
regression and candidate admissions retain their original source and execution
scopes.
This is the active queue. [Current status](../development/current-status.md)
records accepted facts. [Delivery and verification](delivery-and-verification.md)
retains all M2-M6 obligations; M7 remains deferred. Historical plans and consumed
inputs are evidence, not alternative execution instructions.

The [M5 preflight resource incident](../development/m5-refresh-preflight-incident.json)
required candidate recovery before M5 verification. The root filesystem filled before M5 testing
started. Both candidate PostgreSQL instances recorded ENOSPC/PANIC and recovery;
both applications lost their database lease and exited. One audited Go cache
clean restored 7,582,253,056 available root bytes. The subsequent read-only
recovery checkpoint matched all rows in 35 tables and all five sequences in
each of four retained databases against its saved baseline. Native backup,
lifecycle, control and staged-generation checks also passed independent review.
These are logical preservation and native-state observations, not physical
database integrity acceptance. Both unchanged applications have now started
once, passed liveness/readiness and acquired their own new database leases.
All four databases' tables and sequences matched again after startup, and
retained native state and old diagnostic logs remained intact. Independent
review of the [completed application recovery](../development/candidate-disk-full-recovery.json)
passed. The first isolated M5 focused/browser verification is now
[failed and independently closed](../development/m5-refresh-first-verification.json):
89 top-level passes, one library-test failure and no server/browser execution.
The [corrected four-plan run](../development/m5-refresh-corrected-verification.json)
passed all 109 ordinary focused tests but failed its browser scenario at the
interval step. Independent review and resource closure passed; the separate
zero-dispatch capacity rejection also remains preserved. The subsequent
[browser-only correction and verification](../development/task-media-refresh-verification.json)
passed its complete scenario and independent review/closure. The accepted
focused coverage is 109 retained ordinary tests plus one new browser Go test,
with a verified source bridge; it is not a single 110-test run or full suite.
The [final-source ordinary regression](../development/m5-final-regression-verification.json)
has now passed all 25 packages from the beginning: 2,295 top-level passes,
zero failures and one declared mount-profile skip, plus the Linux amd64 build.
Independent result and closure reviews passed. The initial zero-dispatch loop
metadata rejection and first reader rejection are retained; one actual full
worker ran. All private archives passed readback and the owned ext4, loop and
RAM resources closed. The ordinary focused counts are not added to this total.
Preserve all old admissions, original preflight/cleanup rejections and private
incident logs.

The pre-incident recovery review on 2026-09-14 recorded baseline `0b4e012` and 24
uncommitted M5 source/documentation changes. At that historical checkpoint,
focused tests, browser acceptance and final changed-source regression were still
pending. The focused and browser work has since closed as described above.
The earlier source archive remains a retained preparation snapshot; it does not
cover later corrections or this planning amendment.

The immediate queue is:

1. Continue from partial M6 checkpoint `beaea34`, which retains the completed
   M5 source checkpoint `5faf854` and the
   [verified systemd package build](../development/systemd-package-build-verification.json).
   Three actual builds, focused checks and 26 guards passed independent review;
   artifacts and private evidence are retained, and the build tmpfs is closed.
   Preserve this exact package for any subsequently reviewed installation work.
2. Retain the closed [first installation attempt](../development/systemd-installation-first-attempt.json).
   Preparation passed; the runtime controller then raised `NameError` because
   it lacked `import sys`, before any application start, HTTP or runtime SQL.
   The owned PostgreSQL and network anchor stopped with their processes and
   cgroups closed and protected state unchanged. PostgreSQL's private log records
   shutdown completion; process-exit codes were not retained. Corrected failure
   preservation and resource closure passed,
   including private archives, retained configuration/media and five installation
   copies. The first preservation checker also
   rejected the actual stopped-unit receipt because process-exit codes were
   unavailable. It ran ten metadata commands and moved or removed no paths.
   New installation attempts remain paused. Preserve both failures and reassess
   the execution approach before any further installation business. The import-only correction passed remote
   symbol and invalid-input entry checks; no business input was replayed.
   Actual installation acceptance must still use the shipped nonroot systemd
   template for actual installation, bootstrap/login, small real-media catalog
   and normal stop/start preservation checks. Recheck target-path absence and
   capacity; retain unit hardening and protect every existing candidate and PG.
   The recovery-plan review corrected the combined lifetime budget: the owned
   anchor now has a 3,600-second ceiling, and all three handoffs enforce remaining
   time from its original start, including phase and shutdown reserves. The
   correction passed independent static review, 28 remote synthetic boundary
   checks and remote syntax checks of all seven helpers. Those results did not
   cover the missing runtime import. Preserve the consumed inputs and original
   failure; a subsequent attempt needs an explicit reviewed decision based on
   the demonstrated correction. Do not keep a live fixture waiting for more
   operator development.
   Close the new installation and its resources before accepting the increment.
3. The installation result and its limitations are committed and pushed in
   partial M6 checkpoint `beaea34`. The separately retained
   [source bridge](../development/systemd-package-source-checkpoint.json)
   verified that snapshot. All 863 backend,
   module and package inputs matched the E11 source; the 58 generated inputs
   retain their separate artifact evidence. Continue the actual distribution
   component inventory and outstanding third-party notice/source materials.
   Project-license selection is still pending user input. In parallel, review
   the stopped-unit evidence contract before admitting more installation work.
   Reuse the completed package build and M5 regression while their bound inputs
   remain unchanged; expand verification only for a concrete change or failure.
4. Keep movie, episode and subtitle acceptance open. Prioritize a bounded
   retained-state journey when new discriminating evidence or a justified
   correction supports it; this gate can progress independently of packaging.
   Core acceptance still blocks new-binary main promotion.
5. Continue the remaining M2-M6 obligations under their own prerequisites.
   This fixed M5 increment does not complete M5, pass core video acceptance,
   authorize main promotion or complete the overall goal. M7 remains deferred.

The goal remains an independent Linux media server using Go, PostgreSQL and
FFmpeg, an administrator-only React/MUI dashboard and unmodified compatible
clients. This review adds no consumer player and removes no release obligations.

The [native catalog restart increment](../development/native-catalog-restart-verification.json)
has passed once and its independent evidence/resource closure is complete.
The saved embedded amd64 binary ran through two distinct native processes in
a root/private-network profile. Catalog/ACL/counts and favorite/played rows
remained consistent across normal stop/start, with no second bootstrap POST,
scan or UserData mutation and no PG restart. This establishes neither nonzero
playback resume nor browser, nonroot, capacity, native arm64/GPU or host-durability
acceptance. The consumed run must not be replayed.

The completed ordinary regression used source frozen at `badf396`. The [partial regression checkpoint](../development/full-regression-partial-verification.json)
failed overall at a 570-second package timeout; Go exited 1 naturally after
576.293 seconds. Preserve 11 complete packages/475 top-level passes. Library's
537 partial passes and one opt-in skip do not complete its package, and the
raw 1,012 passes are not a passing suite. The active test had run only 9.497893
seconds; historical library/server/total durations were
782.265/992.994/2755.104 seconds, exceeding the old command/business budgets.

The [completed continuation](../development/full-regression-continuation-verification.json)
passed the other 14 packages and ordinary Linux amd64 build on the same
`badf396` source, 25-package inventory and 12 tool pins. It used the corrected
TEST=1500/COMMAND=1560/BUSINESS=4200/RUNTIME=4800-second limits. Independent
review combines the 11 retained complete packages/475 passes with 14 new
complete packages/1,801 passes: 25 packages, 2,276 passed, zero failed and one
explicit `TestRootBindingFullScanMountNamespaceHelper` skip. The earlier
537 partial Library passes are excluded, and older opt-in helpers' default
returns do not prove those profiles ran. The ordinary suite is complete across
two phases; neither the original failed run nor the paused profile is relabeled.

The ordinary binary is `d6fc493d5664f85b6c7258c48d05081050457c1eac4965414528291695c66e53`
(29,560,432 bytes). A separate comparison matched all 374 production Go,
Go-embed-resource and module files to the embedded M6 artifact `59096592...`.
This supports an exact production-source bridge, not binary identity or a
`goby_embed_admin` full-suite claim. The embedded artifact retains its separate
build, focused-tagged and root-profile runtime proofs.

Independent review and sealing passed. Parent/worker/manifest records and all
private archives were reread; processes, PG, cgroups, ext4 mount/loop, RAM and
the lock closed. The source archive remains stored once in the first scope.
Original timeout/archive-capacity failures remain preserved. Two older closed
build caches were reclaimed after their zero-command lock rejection was
preserved, without changing source/modules/logs/databases/artifacts. The final
closure observed 1,561,907,200 free root bytes; future work still needs a
fresh capacity check. That regression checkpoint retains its original
pre-provision state and 61 guard results.

The later [fresh-candidate checkpoint](../development/fresh-embedded-candidate-checkpoint.json)
records one actual independent nonroot provision using embedded `59096592...`,
57 administrator assets and no external administrator directory. The app and
PostgreSQL use separate nonroot accounts. Provisioning observed schema 28,
28 migrations, zero users and 1,481,416,704 free root bytes. The candidate's
direct HTTP port is 28698; the reserved browser origin on 28696 does not establish
a gateway. Preserve this installation and its private manifest. Current remote
operator guards passed 42 preparation, 31 inspector, 83 seed, 39 admission and
18 legacy TV checks: 213 in total. Superseded guard revisions are not counted
again, and these results remain separate from the Go 2,276 count. The final
seed revision adds bounded private failure capture; the admission revision adds
a private runtime-inspection failure sink without expanding business budgets.

The original initial-inspection attempts remain failed. The first made no SQL
or HTTP call: `systemctl show` omitted empty PostgreSQL `EnvironmentFiles`.
Typed D-Bus reads established that property and unchanged unit identity.
The corrected second attempt made one SQL call and no HTTP call, then rejected
unrecorded command output. A separate bounded read-only diagnostic established
that `PGPASSFILE=/dev/null` emits a 55-byte warning even when psql exits 0.
The diagnostic's frontend/backend closed, no other postgres/psql backend
remained and the postmaster identity stayed unchanged. This diagnosis does
not relabel the original failed inspection or its incomplete closure record.

The reviewed correction uses an explicitly absent password-file path within
the owned scope, retains strict stderr checks and captures failed-command
outputs and exit facts. The corrected initial inspection passed, with independent
review: nine SQL calls, 63 HTTP requests and 1,107,438 response bytes checked all
57 embedded assets and five HTML references. It made zero business writes,
preserved process/protected identities and closed its readers. This new result
does not rewrite either original failure.

One fresh seed then passed and was independently reviewed: 49 normal requests
plus four cleanup requests, 14 independent media copies totaling 201,156,949
bytes, eight users, three libraries/scans, ten public catalog DTOs and thirteen
stored items. Both owned credentials completed logout 204 / exact-credential
401 checks and stored revocation; no playback ran. The review confirms seven
closed SQL frontend commands, four cluster-identity and 109 server/listener
checks. It does not invent per-SQL backend-absence receipts or a final complete
PostgreSQL process-identity receipt that this seed did not record.

The same candidate's native admission exited 0 after 601,357 milliseconds.
Its final receipt reports `admitted_for_core_client` and
`candidateAdmissionComplete=true`, with 82 normal and six cleanup requests
(88 actual requests), no failure and no cleanup failures. Its inactive restore
stage remains retained. The 600-second health window and 129-request cap were
unchanged; the cap is not an actual request count. Independent saved-evidence
review passed, confirming all 88 HTTP exchanges, eleven health samples spanning
exactly 600,000 milliseconds, nine TV checks and all three logout 204 /
same-token 401 pairs with stored revocation. It verified the declared source
delta, a matching 290,038-byte backup download, the retained cancelled stage,
and reader/runtime evidence. `clientAcceptance=false` remains explicit.
Inspection, seed and this admission input are consumed;
no repeat, reset, reprovision or old-actor replay is queued.

A separate post-admission metadata checkpoint matches all five protected
identities and the candidate/PostgreSQL PID, boot, start ticks, UID, executable
inode, cgroup and listeners to their provisioned identities. Both owned libpq
password-file paths remain absent and its metadata commands closed, with zero
SQL, HTTP, service changes or replay. It observed 1,233,956,864 free root bytes
and 8,227,144 KiB MemAvailable. This is the later capacity observation, not a
reservation or retroactive evidence for the seed's missing SQL-backend records;
any new heavy build needs a separate current resource budget.

This candidate admission completed independently of the paused mount
experiment and unresolved video diagnosis. Later product edits require their
affected checks and full regression of the final intended snapshot; unchanged
historical evidence retains its original scope.

The [catalog capacity/isolation baseline](../development/catalog-capacity-isolation-verification.json)
then passed one targeted race test, zero failures/skips. SQL seeded two libraries
with 10,000 leaves and 442 folders. Twenty timed page/count-only checks retained
ACL isolation around an owned transaction block, cancellation and rollback;
all ten UserData rows kept every field, timestamp and `xmin` through closure.
This is an in-process HTTP-handler/SQL profile, not physical media scanning,
native TCP/service performance, a release SLO or a kernel filesystem stall.
The single new test is separate from the earlier 2,276-test ordinary suite;
its base `892c536` changes no Go/module files relative to `badf396`.

Corrected independent review passed without rerunning the test. Its original
reviewer-only rejection of a PostgreSQL-owned log remains preserved. All private
evidence and the closed-PG archive were reread; normal PG shutdown and ordinary
unmount closed the independent 3 GiB RAM scope. Empty underlying directories
remain, protected state stayed exact, and closure observed 1,190,330,368 free
root bytes. The following real small-media scan/rescan increment has now run;
do not repeat this consumed SQL baseline or reopen the paused mount experiment.

The [real-media capacity checkpoint](../development/catalog-real-media-capacity-verification.json)
records two passing tests, zero failures/skips and nineteen completed commands.
The new 10,000-leaf test took 293.55 seconds; the existing seven-file test passed
in a new independent scope as regression for the changed shared test helper.
Production files are unchanged. These targeted results neither rerun the old
seven-file scope nor add two tests to the historical 2,276-test suite.
Cold scan, in-process Store reopen, cached rescan and replacement of one movie
with no UserData passed their selected identity, hierarchy, ACL and seeded
UserData checks. Independent review passed once, including source/tool bindings,
capacity markers and cleanup/protection. Both archives were reread, PG/worker
and unit/cgroup closed, and RAM was ordinarily unmounted. Empty underlying
directories remain; the private PG archive stays remote. This increment is
complete and consumed, with no production change or whole-suite rerun required.
Capacity, blocked I/O and the remaining M4-M6 obligations may progress in
parallel under their own prerequisites. Completed backup/recovery workflows
stay closed; a new client journey still needs a discriminating question or a
justified correction, and main promotion retains both acceptance and recovery
requirements.

## Review conclusion

The delivery direction remains appropriate: verify the product, admit an
isolated candidate, establish supported real-client behavior and recovery
evidence, then promote main when both pass. Immediate ordering and completion claims needed
correction. Episode01 has already run. Its visible playback completion cannot
pass acceptance while three page errors remain unclassified. Those errors do
not invalidate accepted MP3/FLAC results or justify unrelated full-suite reruns.

The bounded native-rejection increment is complete. Its actual subtitle
observation confirms undefined reasons without identifying a response or cause.
Main preparation resolved configured paths, lifecycle selection, PostgreSQL and
database identities, schema27 binding and bounded startup candidates. Both
capacity configurations passed actual parsing; the missing empty cache directory
was prepared. The old executable and 415 installed administrator assets are
now archived and verified. The capacity profile is installed. The separate key observer stopped at a
helper-permission precondition before execution; authentication remained unproved
at that [material checkpoint](../development/audited-main-recovery-materials.md).

The [native-backup input](../development/audited-main-native-backup-plan.md) has
now been consumed. After a corrected reserved-prefix preflight, one source32
start reached a strict environment rejection before HTTP. Systemd's two memory
pressure variables were omitted from the adapter, and full configuration
acceptance incorrectly preceded the controller's ability to stop its own
invocation. Independently reviewed ownership recovery stopped that exact
process. The [actual closeout](../development/audited-main-native-backup-closeout.json)
preserves all 402 rows and five sequences; capacity and the restart fence remain
installed at that point. That input paused after two controller failures; its
changed cache/log baseline and original failed receipts remain preserved.

The [ownership correction](../development/audited-main-backup-ownership-correction.md)
then passed remote service, retained-file and controller guards and independent
review. Its separately admitted child phase completed one native backup and
source-key witness, one full download, same-cookie logout and exact data/file
reconciliation. The [completed checkpoint](../development/audited-main-native-backup-completed.json)
binds a 196,310-byte schema27 archive with 405 rows. The operational database has
408 rows, preserving all 402 old rows and adding one revoked session plus five
audits. A final shared-parent cleanup rejection remains in the execution receipt;
reviewed policy-only cleanup removed the exact restored fence and empty owned
directory. Main is inactive, capacity remains installed and no business request
was repeated. Those backup and cleanup inputs remain consumed.

The ordering needed a substantive correction: paused video investigation must
not also prevent the old source32 backup and isolated recovery work. Those
actions require their own safety admission; core acceptance continues to block
new-binary main promotion. Separate backup inputs from backup outputs, and allow
independent M2-M6 obligations to progress under their own prerequisites. Both
declared restoration/restart sequences and their final fixture disposal have
completed. Continue independent M2-M6 work and reopen retained-state video
diagnosis only with new discriminating evidence or a justified correction.
The [selected online rollback closeout](../development/isolated-selected-online-rollback.json)
records B serve/login, online A plan/apply/login and the committed rollback's
completion at B/revision3. The first online process hit its 512 MiB memory
limit after the single rollback request; its failed receipt remains unchanged.
Post-OOM comparison proved A410 with only its rollback-request audit and B412
exact. A reviewed 2 GiB unit limit and `GOMEMLIMIT=768MiB` allowed one new serve
invocation to resume that same operation, without HTTP or another request.
After normal app stop, A410 remained exact; B413 contained only the declared
credential revocation, generation binding and restore audit. That recovery
checkpoint is preserved separately from the subsequent restart result.
Post-rollback authentication and same-mount PG/application restart have now
passed: 16 complete HTTP responses, two new login/logout/401 pairs, ten naturally
closed read-only observers, A410 exact and B413-to416-to419 with only the two
declared authentication deltas. Both app invocations stopped normally; one
normal PG shutdown/restart retained the mount, cluster identity and B/revision3.
That selected checkpoint records PG529062 and unchanged anchor516461; it remains
historical evidence and is not relabeled as the later source32 state.

The [actual installation return and source32 proof](../development/isolated-source32-recovery.json)
then each succeeded once. Two exclusive renames restored all 416 old installation
files and retained all 58 selected files. Native source32 restored the same
archive into independent D at schema27: all 35 source/target fingerprints
matched, with 17 credential revocations, one expired play, one binding and no
schema28 migration. D advanced405-to406-to409-to412 through apply and two owned
login/logout workflows; every other field and all five sequence expectations
matched. Fourteen complete HTTP responses, five CLI commands and ten read-only
observers closed. Two app invocations and one same-mount PG restart stopped
normally at their required boundaries. That source32 checkpoint retains PG532226
and original anchor516461 as historical identities. Selected files and protected
main/candidate state stayed exact.

The [final disposal](../development/isolated-restore-disposal.json) then succeeded
once. Final A410/B419 reads matched all35 tables and five sequences per database;
both observers closed. Normal PG shutdown, ordinary unmount of the exact768 MiB
tmpfs and final anchor stop removed the temporary cluster and network namespace.
All61 owned runtime unit files were preserved privately and removed; their
units are inactive with no FragmentPath. Inactive A's credential responsibility
ended through cluster disposal, without changing its revoked_at value. Private
working/evidence files and the two dedicated OS accounts remain explicitly
retained. No recursive deletion, account deletion or protected-host change was
part of this closure. Do not queue these consumed recovery operations again.
Preserve the page-error rule
and every failure; do not extend the generic observation framework.

## Accepted baseline

| Area | Accepted result | Remaining limit |
| --- | --- | --- |
| Earlier client-candidate product | TV parent metadata and earlier fixes passed [2,270 tests/25 packages with race instrumentation and a Linux build](../development/tv-parent-metadata-full-verification.json), without failures or skips | This proof belongs to the earlier audited/client candidate's exact source and binary; it is not the new embedded artifact's regression or client acceptance |
| Earlier audited/client candidate | [TV successor transition](../development/tv-parent-candidate-transition-closeout.json) and [affected admission05](../development/tv-parent-affected-admission-closeout.json) passed | Admission05 checks changed TV projections/access and reuses admission04 for unchanged contracts; neither transfers to the fresh embedded candidate or main |
| Fresh embedded candidate | [Initial inspection, seed and native admission](../development/fresh-embedded-candidate-checkpoint.json) passed independent review on embedded `59096592...`; admission used 88 actual requests with no cleanup failures | Client acceptance is false and the inactive cancelled stage remains retained. Ordinary regression, source binding and candidate admission do not retroactively pass the earlier client journeys |
| MP3/FLAC | Both declared journeys, physical delivery, durable state and owned cleanup [passed](../development/audited-flac-client01-closeout.json), with zero page errors | Only these client/media profiles pass; no audible-output or general codec claim |
| Episode01 | Full TV browsing and playback controls completed; [owned state is closed](../development/audited-episode-client01-closeout.json) | Three page errors preserve the formal rejection; neither episode nor overlapping TV browse acceptance passes |
| Subtitles01 | SRT/VTT selection, visible cues, seek and Off/stop/logout completed; [owned state is closed](../development/audited-subtitles-client01-closeout.json) | Two native undefined errors remain unattributed; one media cancellation timing check remains unresolved; formal acceptance stays open |
| Movie | Movie05's two counted play chains and movie06's pre-playback failure have closed owned state | Four old page errors remain unknown; movie06 did not play; the old movie05 baseline is stale for another run |
| Main | Native backup, selected rollback/restart, [actual old installation return/source32 recovery](../development/isolated-source32-recovery.json) and [final cluster/credential disposal](../development/isolated-restore-disposal.json) passed. Original failures and private evidence remain preserved; main is inactive | Core video acceptance still blocks new-binary main promotion. M2 host durability and the remaining M2-M6 release obligations are unchanged |
| M6 embedded assets | Focused build/tagged checks and root-profile native amd64 runtime passed; the completed ordinary suite separately binds the same 374 production Go/embed/module files | Distinct ordinary and embedded binaries; no tagged full-suite or native arm64 claim. The fresh nonroot admission result has its separate review boundary above. OCI, GPU, license and full M6 remain open |
| Native catalog restart | One real stop/start run passed 156 cumulative HTTP requests, nine core tables' rows/xmin and seven media checks; independent review and closure passed | Same PG and root profile only. No playback/nonzero-resume/browser, nonroot, PG restart, capacity or host-durability claim |
| Catalog capacity/isolation baseline | [One targeted race test](../development/catalog-capacity-isolation-verification.json) passed on 10,000 SQL-seeded leaves/442 folders, preserving ACL behavior and all ten UserData rows during an owned transaction block and rollback; review and closure passed | No real media/scan throughput, native TCP/service performance, RSS/SLO, whole-catalog raw-row preservation or actual filesystem-stall claim. This new test is not part of the earlier 2,276-test full-suite count |
| Real small-media capacity | [The 10,000-leaf scan/rescan test and affected seven-file regression](../development/catalog-real-media-capacity-verification.json) passed independent review and resource closure, with zero failures/skips and production unchanged | Tiny-file measured cost and in-process Store reopen do not establish HTTP/service performance, service/PG/host restart, real filesystem stalls, representative throughput or complete M2 |

The earlier audited/client candidate uses binary
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`,
under epoch `76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac`.
Subtitles01 closed twenty-one revoked sessions, thirteen play rows, six userdata
rows, two retained audio references and no encoding jobs. Its unstarted Prepared
row is explained by later detail PlaybackInfo responses and stays retained.
All earlier rows remain preserved. Later inputs bind this closed state, not
the earlier episode or audio snapshots. Ownership/cleanup closure does not
resolve the separately recorded media cancellation timing discrepancy.

## Immediate queue

The order sets the immediate focus, not a serial dependency across every row.
Independent reviews and implementation may run in parallel; paused experiments
retain their own stop conditions. Prefer bounded product changes and existing
verification tools over extending launcher or observation infrastructure.
On the shared test environment, serialize heavy builds/tests and shared-fixture
writes. Recheck current disk, RAM and execution limits before each such phase;
historical capacity readings are not reservations. Keep the paused M2 RAM
workspace outside every new resource budget.

| Order | Concrete deliverable | Completion or stop condition |
| --- | --- | --- |
| 0. Recover from the observed disk-full incident — complete | Preserve the independently reviewed restart and diagnostic evidence; use the recorded replacement runtime identities for dependent work | Both unchanged applications started once and passed health/readiness, unique-lease ownership and four-database/native preservation checks. Independent review passed; readers and the deployment lock are closed. Do not start either service again for observation or input rebinding; preserve the original incident and admissions |
| 0a. Fixed M5 refresh increment — complete | Native task, focused Go/API and real administrator-browser acceptance, final ordinary regression and independent result/resource closure | Focused coverage is 109+1 across two source-bound phases. The final source passed one complete 25-package run with 2,295 passes/0 failures/1 declared skip and the ordinary Linux build. Preserve every earlier failure and the paused profile gap |
| 0b. Internal amd64 embedded systemd package — partial checkpoint committed, installation paused | Retain checkpoint `beaea34`, its source bridge, verified package and independently closed failed installation; review the execution approach and continue independent release evidence | Three builds, seven focused top-level tests (26 including subtests), 26 package guards and build closure passed. The runtime entry and first preservation checker failed; both failures remain preserved and owned resources are now closed. Nonroot application restart, public distribution, upgrade, core-video, native arm64/GPU and OCI remain separate gates |
| 1. Core video and subtitle acceptance | Prioritize this promotion gate when new discriminating evidence or a justified product correction supports a bounded journey on a reviewed current candidate and retained-state input | No new journey is admitted yet. Context291 identifies physical294, not293; the earlier cancellation and original page errors remain unresolved. Preserve the completed identity review. Without new grounds, keep this gate open and continue independent work; do not repeat the review, replay consumed actors or add another observation framework |
| 2. Independent remaining M2-M6 delivery | Separately address representative capacity and safely isolated blocked I/O; continue host durability, media/transcode, administration, native hardware/architecture, OCI, support-matrix and notice work under their own prerequisites | Require measurable limits and bounded shutdown. This track may progress independently, while heavy work follows the shared-resource limits above; small catalogs, root-profile restarts and cross-builds do not close the release gates |
| 3. Main promotion | Once core acceptance passes, bind the verified/admitted intended artifact to current preservation/recovery prerequisites and a bounded upgrade/post-upgrade workflow | Completed isolated recovery proofs remain accepted history. They neither authorize replay of consumed inputs nor verify later untested code; refresh only prerequisites invalidated by actual changes |

The completed independent increment is [native scheduled media refresh](../development/task-media-refresh-plan.md):
register `library.refresh_media`, retain ordinary `library.scan` behavior, and
verify explicit ForceProbe execution, request replay, cancellation, scheduling
and administrator UI behavior. Implementation, static review and remote formatting
are complete. The first run passed all 63 task tests but failed one of 27 library
tests before its force-probe step; server/browser checks were not executed.
Its result and resource closure are independently reviewed. The corrected test
uses a persisted metadata baseline and has passed all 27 library tests, including
successful repair and failed-probe preservation. Tasks and server groups also
passed. The complete browser scenario subsequently passed after the two
browser-test-only corrections, with independent source-bridge and resource
closure review. The plan now binds
entity-repair checks to a missing Genre association, assigns replay and independent
scan isolation to Go/API tests, and limits browser evidence to its actual manual,
cancelled and scheduled workflow. Focused/browser checks and the final ordinary
regression are complete, with independent reviews and resource closure. This
increment does not admit a core-client replay or main promotion.

The completed partial checkpoint is the internal amd64 systemd package in row 0b.
Its build-script, environment-template and installation-document changes are
committed and pushed in `beaea34`; the shipped unit is unchanged. The
[actual package builds and guards](../development/systemd-package-build-verification.json)
passed independent review and resource closure. The verified archive is retained
outside the now-closed build tmpfs. Installation preparation passed, but the
runtime entry failed before Goby started. That failed scope is now preserved and
closed; installation remains paused. The staged-source bridge and partial
checkpoint are complete. Follow the independent release-evidence work and
execution review in the immediate queue. The M5
source checkpoint `5faf854` is committed and pushed. Follow the
[package plan](../development/systemd-package-plan.md), preserve existing
build compatibility, and keep new installation acceptance separate from
historical custom-unit candidate proofs. Project licensing remains unresolved,
so no external distribution is admitted by this package work.

The M2 full-scan mount experiment remains paused outside this execution order.
Its helper compiled, but PostgreSQL and all seven scan stages remain unexecuted.
Reopening requires new evidence supporting a reviewed correction to the failed
UID transition. Preserve its launcher evidence; no third renamed attempt,
repeated review without new evidence or unsupported `NoNewPrivileges`
attribution is queued.

The completed real-file increment used committed `a2f0009` plus two test-only
changes. Its affected shared-fixture regression and resource closure passed;
these edits do not require another full product suite. Select the next heavy
execution from the remaining evidence gaps, not an inferred percentage complete.
If a later increment exposes a production defect, review the fix and run
its relevant checks, then verify the final intended product snapshot before
claiming changed-version acceptance.

The 10,000 leaves are an initial correctness and measured-cost baseline using
tiny valid media files. Store reopen is not a native service or host restart,
and this profile does not close real storage stalls, host durability, realistic
media throughput or release-capacity claims. Keep the independent remaining
gates visible; unresolved video acceptance continues to block the corresponding
main promotion despite the completed catalog increment.

The latest real-file closure observed 1,124,679,680 root bytes available. Further
builds need a separately budgeted compiler scratch/cache location after a fresh
capacity check. Ordinary strong-filesystem tests require an exclusive owned
ext4 GOTMPDIR; do not substitute tmpfs or confuse it with compiler scratch.
Preserve the paused M2 workspace;
do not repurpose its RAM mount or assume historical free-space figures.

The [M2 preparation checkpoint](../development/m2-fullscan-mount-preparation.json)
records a missing assumed module cache, followed by successful helper compilation
and `runuser: cannot set user id: Operation not permitted` before `initdb`.
Its unit is terminal with no owned process; the compiled helper and evidence are
retained. Compilation does not pass the full-scan deletion-protection scenario.
The independent [catalog rescan/ACL target](../development/catalog-rescan-acl-verification.json)
has now passed one race test with four state checkpoints, not four tests. Seven
real media files in two ext4 libraries covered movie/episode identity, a T2 move
between albums within the mixed library, direct/derived counts and ACL-safe
queries. All ten original UserData rows and `xmin` stayed exact across
`Store.Close/New` and cache-aware rescan. The first before-move failure was a
test API-contract error (`ListEntities` with `MusicArtist`); correcting it to
artist detail and artist/album-artist filters required no production fix.
Both attempts and their closed-PG archives were retained; normal PG stop and ordinary
unmount closed this separate 3 GiB tmpfs scope. Native service/PG restart, host
durability, representative capacity and the paused mount's seven stages remain
unproved by this result.
The [embedded build checkpoint](../development/embedded-administrator-verification.json)
now records 22 handler tests/subtests plus two ordinary and two tagged provider
tests with race instrumentation, all passing. Eleven negative manifest cases,
ordinary-provider tests without `dist` and the expected tagged-compile rejection
also passed their declared checks. Actual amd64/arm64 embedded artifacts were
retained; all 57 assets and five HTML references were checked, the 911-file
source archive was reread, and ordinary unmount closed the M6 build tmpfs.
This completed increment is not pending work. The later ordinary regression is
now separately complete with its explicit profile gap and production-source
bridge. Tagged full-suite and native arm64 remain unproved; fresh candidate
admission and its independent review have passed separately. No result
inherits the older client candidate's 2,270-test proof.

The later native run used two processes, PIDs 557934 and 557978, with normal SIGTERM
stops, exit 0, complete shutdown and no ERROR-level application log events.
Its first 89 requests became 156 cumulatively after restart; eight owned
logout 204 / same-credential 401 pairs left zero unrevoked
sessions at both schema28 checkpoints. Nine core tables' rows/xmin matched,
including five UserData rows, and all seven media files totaling 66,189 bytes
kept their metadata/hashes. The 800-byte embedded index matched the M6 manifest.
All 31 worker commands succeeded. Independent review SHA256
`3cb9b9e2b7e789cf0dde23585fb8707f65baf590f069ef96661f6c3f2e51fbee`
matched 156 HTTP pairs/request-ID log bindings and both raw SQL snapshots.
Closure SHA256
`b4d4993692477d448a67d8ac7acbf0c4ee637013b2d8e2c781db5f979ea9eb0f`
sealed two private archives totaling 11,061,258 bytes, all app/PG/worker/cgroup
closure and ordinary unmount of the independent 3 GiB RAM. The ext4 media and
empty underlying RAM directory remain retained; protected metadata is unchanged.

## Completed and consumed checkpoints

These entries preserve prior decisions and results; they are not the next queue.

| Checkpoint | Completed work | Retained boundary |
| --- | --- | --- |
| 1. Episode01 closeout — complete | Replay saved physical/durable evidence while reproducing the original UI rejection; close workers and check candidate/PostgreSQL continuity | Preserve all three errors, the counted Stopped row and the uncounted Prepared row; no business replay |
| 2. Minimal rejection observation — complete | Bounded native primitive/Response metadata and the subtitle detail-wait correction passed [targeted verification](../development/native-rejection-observer-verification.json) | 83 component checks, 35 controller guards and three saved integrations passed; pageerror rejection and cleanup remain unchanged |
| 3. First subtitle increment — consumed | The declared journey completed; native reasons are confirmed as undefined; owned state and workers are closed | Preserve the two original errors and unresolved partial293 timing; subtitle acceptance remains open and the input cannot be repeated |
| 4. Source32 backup start — consumed; cleanup closed | Two controller failures are preserved; the second installed capacity/fence and started source32 once before environment rejection. The exact invocation was stopped, with zero HTTP and exact database preservation | Do not execute this input again, remove its evidence or reset its file baseline. Native backup creation and key authentication did not occur |
| 5. Reviewed native backup — complete | Stop authority was corrected before strict configuration acceptance; a separately admitted child phase created/downloaded the fresh archive and reconciled all data/files. Policy-only cleanup closed the final shared-parent rejection | Retain the original failure and new completion checkpoint. This input is consumed; do not create another backup or start main for a cleanup retry |
| 6. Isolated recovery proofs and disposal — complete | Selected rollback/restart, actual old installation return and independent source32 recovery/restart passed. [Final disposal](../development/isolated-restore-disposal.json) sealed exact A410/B419, normal PG shutdown, ordinary unmount, anchor/unit closure and inactive A credential disposal | Preserve all consumed inputs, original failures, private evidence and explicitly retained accounts/files. No recovery replay is queued; core video, M2 host durability and complete M2-M6 remain open |
| 7. Distribution notices — partial parallel progress | [Original legal texts and versioned inventory](../../THIRD_PARTY_NOTICES.md) cover current Go requirements, production npm entries and actual fonts; source/copy checks passed remotely | Resolve project license and the relevant upstream/inclusion gaps against the actual future package. This collection does not close M6 or external distribution |
| 8. Embedded administrator assets — scoped verification complete | Focused race tests, actual embedded amd64 build, arm64 cross-build, source/asset guards and artifact/tmpfs closure passed | Preserve the exact verification and artifacts. No new service/deployment, native arm64 or full changed-version acceptance is claimed |
| 9. Real-media catalog rescan/ACL target — complete | One race test passed four state checkpoints; same-inode album move, direct/derived counts, ACL queries and all ten UserData rows/xmin remained correct across Store reopen and cached rescan; scope closed | Preserve the first test-contract failure and both sources/logs. This is not native service/PG restart, representative capacity or the paused mount proof |
| 10. Subtitle response-identity review — complete | Saved context291 response ID matches physical294 and excludes293 | The old cancellation/timing failure and page errors remain unresolved; no new business request, repeated review or new observer layer is authorized by this result |
| 11. Native catalog restart — complete | One root-profile native amd64 run passed across two processes with the same PG, stable catalog/ACL/UserData, eight logout pairs and 156 cumulative requests; independent review and closure passed | Preserve the consumed run and its artifacts. This is not PG restart, playback/nonzero resume, browser, nonroot, capacity, host durability, full regression or main promotion |
| 12. Ordinary full regression — complete with explicit profile gap | Same-source 11+14 complete packages yield 2,276 passes/0 failures/1 opt-in skip; ordinary Linux build, independent review and volume/archive closure passed | Exclude the first partial Library counts, retain its timeout, and preserve the paused mount gap. The separate 374-file embedded-source bridge is not tagged full-suite or candidate admission |
| 13. Fresh embedded initial inspection, seed and admission — complete and consumed | All three stages passed independent review; native admission exited 0 with eleven health samples spanning exactly 600,000 ms, 88 actual requests, no cleanup failures and its inactive stage retained | Preserve both original inspection failures, the separate diagnostic and seed evidence limits. Client acceptance remains false; no replay or earlier-client acceptance claim follows |
| 14. SQL catalog capacity/isolation baseline — complete and consumed | One race test passed with 10,000 leaves/442 folders, twenty timed query samples, ACL isolation and ten UserData rows exact across transaction blocking/cancellation/rollback; independent review and resource closure passed | Preserve the initial reviewer-only log-ownership rejection. No test rerun, physical scan, actual filesystem stall, native service benchmark or addition to the prior full-suite count |
| 15. Real small-media capacity — complete and consumed | The new 10,000-leaf cold/cached scan and Store-reopen test plus affected seven-file regression passed; independent review, both archive readbacks and ordinary RAM unmount completed | Production is unchanged; do not add these two results to the historical 2,276-test count. UserData was seeded after cold scan, with no final post-Close snapshot. This tiny-file profile does not pass throughput SLO, service/PG/host restart, storage stalls or complete M2-M6 |

The [Episode01 review](../development/audited-episode-client01-review.md) records
the completed diagnostic hypothesis, limits and remote checks. Its implementation
and synthetic verification preceded the one subtitle input, which is consumed.
Future diagnostic work requires a separate discriminating question and retains
the one-session, 60-minute investigation ceiling before reassessment. Diagnostic
collection does not authorize extra playback attempts.

The [diagnostic implementation](../development/native-rejection-observer.md)
completed this bounded increment. Its initial synthetic redirect fixture failed
and was corrected within the same scope of work; both verification workers are
closed. The product remained unchanged. Do not reopen diagnostic tooling by default.
The [subtitle result review](../development/audited-subtitles-client01-review.md)
now includes the completed response-identity inspection: context291's response
belongs to physical294, not293. Native undefined still provides no error-cause
association, and partial293 retains its unexplained cancellation/timing failure.
Keep these gaps and stop the same video/diagnostic cycle; the read-only review
itself is not queued again. The independent
recovery work has now closed. Advance the remaining M2-M6 requirements under
their own prerequisites; recovery completion cannot pass core acceptance or
admit promotion.

If two successive tool/checker failures block the same planned observation,
pause that experiment and reassess its shared cause and value. Preserve failed
evidence and reconcile actual state. Do not create renamed attempts or another
general runner. Missing direct causes remain explicit; independent source
review and release preparation can proceed while affected claims remain open.

Movie, TV browse, episode and subtitle actors are consumed. A future run needs reviewed
retained-state input and saved-state verification through the existing contract.
Do not reset accounts, use a null baseline, reuse stale movie05 state, or add
per-attempt constants. Overlapping coverage closes another gate only when its
complete declared checks and overall closeout pass. Episode01 does not qualify.

## Remaining work and dependencies

1. Retain the completed native archive, both distinct restoration/restart proofs,
   actual old installation return and final cluster/credential disposal records.
   No consumed recovery input is replayed. Continue independent M2-M6 obligations
   under their own prerequisites; retained private files and OS accounts remain
   explicitly inventoried rather than silently treated as removed.
2. Core acceptance remains open for movie/TV playback and external subtitles.
   Reuse accepted MP3/FLAC and unchanged controls. Resume a consumed video path
   only with a reviewed retained-state input and a discriminating question or
   justified correction; no automatic replay or new generic instrumentation.
3. Main promotion is the join: require both supported core acceptance and the
   complete safety/recovery proofs, then admit its bounded upgrade and workflow.
4. Independent M2-M6 capacity, blocked-storage/reboot, media/transcode,
   administration, actual GPU, arm64/OCI/embedded assets, license/notices and
   feature work may proceed under their own prerequisites. Main promotion is
   not a blanket dependency. Publish only the support rows actually proved.

Main preparation, the admitted old-binary backup and both isolated recovery
rehearsals completed independently of core video acceptance and are retained
history, not work to execute again. Promotion still requires core acceptance
and safety/recovery prerequisites bound to its intended artifact and current
state. Refresh only prerequisites invalidated by actual changes. The old source55-specific
main plan and runners remain superseded. A new-binary schema27 restore migrates
to schema28 and does not establish old-binary rollback. A cancelled ready
restore retains its inactive staged database; bind that occupied state explicitly.

## Gate boundaries

| Gate | Blocks | Independent work that remains possible |
| --- | --- | --- |
| Source identity, authorization, ownership, preservation and recovery safety | Affected admission, mutation or promotion | Read-only diagnosis and preparation |
| Supported core real-client regression | New-binary main promotion claiming that playback workflow | Completed source32 backup and isolated recovery evidence remain reusable within their original scopes; unrelated M2-M6 work can continue |
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

The intended full-suite profile has exactly one known opt-in skip:
`TestRootBindingFullScanMountNamespaceHelper` calls `t.Skip` when its privileged
profile is not enabled. Report that skip by exact name; do not claim zero skips
or execute the paused seven-stage experiment to suppress it. Every other skip
or failure is rejected. The two older mount helpers can return normally without
their opt-in environment, so their passing test nodes do not establish that
those mount profiles executed. The completed ordinary suite remains accepted
for its frozen source. A future product change's final verification retains
this explicit evidence boundary and the separate ext4-fixture/compiler budgets
above; it does not replay the consumed ordinary-regression inputs.

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
