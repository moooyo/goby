# Current implementation and delivery status

Reviewed on 2026-09-14 after M6 embedded-asset verification and artifact/resource
closure, with M2 still paused. The [execution plan](../planning/current-execution-plan.md)
is the active queue. Complete M2-M6 delivery remains in scope; M7 is deferred.
Historical handoffs, PIDs, experiment inputs and verification receipts retain
their original meanings and are not fresh deployment observations.

## Current gates

| Gate | Accepted result | Next required result |
| --- | --- | --- |
| Product correctness | R01-R21, diagnostics, cancellation fixes and TV parent metadata are verified; the selected baseline's latest full run passed 2,270 tests/25 packages and a Linux build | Reuse this exact product proof during client acceptance; later working-tree changes require their own verification |
| Audited candidate | TV successor installed; admission05 passed changed TV projections/access and reused the original admission04 contracts | Reuse the selected product and bind the next input to the latest closed state |
| Core original client | MP3/FLAC passed; video/subtitle journeys retain formal failures with closed owned state; native diagnostics confirm two undefined reasons in Subtitles01 | New discriminating evidence or a justified correction before retained-state video acceptance; no automatic replay |
| Main recovery and deployment | Native archive/key witness, both distinct restore/restart proofs, actual old-installation return and final cluster/credential disposal passed. Original failures and private evidence remain preserved; fixture processes/namespace/runtime unit files are closed, and main is inactive | Core video acceptance still blocks new-binary main promotion. M2 host-reboot/power-loss durability and the remaining complete M2-M6 release requirements stay open |
| Complete release | Implemented foundations and historical scoped controls | Remaining M2-M6 capacity, operations, media, hardware, packaging, license and feature evidence |

The [M2 full-scan mount checkpoint](m2-fullscan-mount-preparation.json) is paused
after two launcher failures. The first assumed a nonexistent module-cache path;
the reviewed second attempt compiled the helper with race instrumentation, then
`runuser` failed to change UID before `initdb`. No PostgreSQL daemon or helper test
started, and none of the seven planned scan stages ran. The failed unit has no
owned process, while its RAM workspace, compiled helper and evidence remain
retained. The UID failure's cause remains unassigned; no third renamed attempt
is queued. This result does not accept full-scan deletion protection or complete M2.

The [M6 embedded administrator increment](embedded-administrator-verification.json)
now has actual remote results. All 22 handler tests/subtests and two ordinary
plus two embedded provider tests passed with race instrumentation, without
failures or skips. The actual embedded amd64 binary is 30,678,868 bytes, SHA256
`59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312`.
All 57 production assets and five HTML references were independently checked;
its manifest binds 848 source files. Eleven negative manifest cases passed,
including seven pre-Go rejections and four synthetic Go mutation guards. The
two ordinary provider race tests also passed without `dist`, while an actual
tagged compile without `dist` rejected as expected.

An actual arm64 cross-build produced 28,598,324 bytes, SHA256
`11e6e4c0bfdfb1cb4ed9c6debf6f2b7cdf272e2e4abefdd6a35b32ee8870aa3d`,
with ELF machine 183 confirmed. Both artifacts/manifests, the verified 911-file
source archive and command logs were retained, and ordinary unmount closed the
owned M6 build tmpfs. Protected state and the frozen module cache stayed exact.
The [build document](embedded-administrator-build.md) preserves the detailed
receipts and scope. No new binary ran as a service or was deployed; full
regression and candidate admission for this changed version are still pending,
and the old 2,270-test proof is not inherited. Native arm64, OCI, GPU, licensing
and full M6 remain open. Immediate work returns to a bounded independent M2
catalog/operations gap selection and retained core-video evidence review,
without restarting the paused M2 launcher or consumed client/recovery scenarios.

The [material checkpoint](audited-main-recovery-materials.md) preserves the old
executable and 415 administrator files in a verified 16,590,013-byte archive.
At that checkpoint private credential metadata was reconciled, while key and
passphrase authentication remained unproved. The standalone key observer stopped at a helper-mode mismatch
before any database connection or master read. The native backup's same-snapshot
`WitnessBackup` was the required authentication step; no repeat observer is queued.
The later native create passed that witness. The isolated selected native plan
has now authenticated the archive/passphrase and restored generation master;
restored-account login has since passed on selected B and A, including B after
rollback and process restart. The separate actual source32 restoration and
authentication/restart have also passed, as recorded below.

The [native-backup scope](audited-main-native-backup-plan.md) is consumed. Its
first controller rejected a retained preparation filename before locking. The
corrected controller started source32 once, then rejected systemd's two memory
pressure environment variables before login or backup. Full environment checking
preceded assignment of stop authority, so separate, reviewed ownership recovery
was required to stop the exact invocation. That input paused after two
controller failures and was not executed again.

The [post-stop closeout](audited-main-native-backup-closeout.json) confirms main
and source55 inactive, 35 tables/402 rows and all five sequences exact, zero new
sessions/activity/backups, preserved lifecycle/control/old archive/master
metadata and unchanged external boundaries. Two cache marker files now exist;
the old 413-byte diagnostic entry and one new 451-byte log are closed. The
installed capacity and temporary fence were exact. These became the retained
inputs for the reviewed correction; the old empty-cache/ten-log baseline is stale.

The [ownership correction](audited-main-backup-ownership-correction.md) establishes
stop authority before configuration/readiness checks and precisely admits the
observed pressure variables. Its 12 service, 10 retained-file and 11 controller
guard groups passed remotely. A separately admitted child phase then completed
one create, one full download, 13 HTTP calls and the same-cookie logout rejection.
The [completed checkpoint](audited-main-native-backup-completed.json) records
archive `69f5e597c99f65099647cd3322d3174c`, 196,310 bytes, SHA256
`0a61cbd6c6ff23543ba873ed1a44dd33ecce2f8702f956e24096862996c7874f`.
Its native source-key witness covers four application-key rows. The archive
contains 405 rows; the final operational database has 408, with all 402 old rows
preserved, one new revoked administrator session and five ordered activity rows.
The other four sequences remain exact. Control revision is9, one new ready object
exists, the two cache markers and eleven old logs are exact, and a twelfth log
closed. Main and source55 are inactive.

The original execution receipt retains a final shared-parent metadata rejection
during empty-directory cleanup. Its recovery restored the fence; a separate
reviewed cleanup then removed only that exact fence and owned empty directory.
Capacity remains installed and the original restart policy is restored. No
HTTP, backup create or service start was repeated for cleanup. The final archive
bytes and candidate/PostgreSQL boundaries were checked again.
The [revised restoration plan](audited-main-isolated-restore-plan.md) requires
offline recovery into B, online application into A, actual native rollback to B,
and same-mount PG/application restart. Two offline applications do not retain a
usable rollback image. The separate source32 proof also requires replacing the
same isolated installation with the preserved old binary and 415 assets.

The [isolated infrastructure](isolated-restore-infrastructure.json) checkpoint records
an independent PostgreSQL17 cluster, dedicated PG/application identities, a
private network namespace and a capped 768 MiB PostgreSQL tmpfs. All 158 checks
across three sandbox profiles passed; its maintenance identity backend exited
naturally and protected main/control/candidate state remained exact. Five remote
transport guards covered process-group closure before provisioning. The subsequent
[target/material preparation](isolated-restore-targets.json) created four empty
ordinary-role databases, copied all 474 selected/old installation files and prepared
separate private configuration/archive/passphrase inputs. All five preparation SQL
backends closed naturally. This completed preparation was followed by the
[selected initial restore and activation](isolated-selected-initial-activation.json).
One native import/plan and one offline apply completed, with all source/staged
table fingerprints matched. At that checkpoint B was active at lifecycle revision1 with407 rows:
all406 staged rows remained exact and one complete system restore audit was
added. Its activity sequence advanced1; the other four stayed exact. Native
master/config descriptors match the archive, the default master remains absent,
and all CLI/observer processes were closed.

The [online rollback closeout](isolated-selected-online-rollback.json) now records
B serve/login, a second native plan into A, online apply at revision2 and A
login. After the sole rollback request returned202, the selected process was
OOM-killed at its 512 MiB unit limit. Its original failed execution remains
preserved. Independent post-OOM comparison proved A410 with only the new
rollback-request audit and all B412 rows exact. The control store retained the same
authorized rollback in `retiring`, with A/revision2 still current.

A reviewed unit revision changed only `MemoryMax=2G` and added
`GOMEMLIMIT=768MiB`. One new serve invocation resumed operation
`6957d09764e1d0dbfe98339a5f00685d`, without HTTP or new plan/apply/rollback
requests, then stopped normally. The independent result proves A410 exact and
B413: one session revoked, one binding changed and one `restore.applied` audit
added, with the activity sequence advancing1 and the other four exact. That
checkpoint selected B/revision3, generation `2a66c343e7265878629a21c7ea0336f0`, with no
transition or lifecycle journal. At that closeout the app was inactive and the
original PG516649 and anchor516461 were still live.

The subsequent restart completion passed once, with 16 complete HTTP responses
and no HTTP errors or unknown commits. Both old credentials returned401 at B;
this does not revoke the retained A row. Two new administrator logins each
closed with DELETE204 and same-cookie401. B advanced413-to416-to419, each time
adding only one revoked session and two audits; all other table fields and all
five sequence expectations matched. A410 remained exact and ten read-only
observer backends closed naturally. The app started and stopped normally twice.
One normal SIGINT shutdown/restart of the isolated PG retained the exact mount,
system identifier and B/revision3 generation. At that checkpoint PG was PID529062, start ticks
7006822, invocation `d820096e54ef47ebae60300554e8824e`; anchor516461 is unchanged.
Main/source55 and protected resources remained exact, and the complete source32
tree was unchanged. This selected checkpoint remains historical and unchanged.

A saved-input preflight failure is retained: default Python import resolution
selected `/root/inspect.py` instead of the standard library, before HTTP, SQL
or product execution. The input check passed under `-I -B`; the actual restart
workflow was executed once. Both this failure and the earlier OOM remain in the
[combined checkpoint](isolated-selected-online-rollback.json).

The [actual installation return and source32 closeout](isolated-source32-recovery.json)
now records two successful, single executions. Two exclusive renames returned
the shared installation to all 416 old files (40,869,739 bytes), retaining all
58 selected files (30,667,532 bytes); this installation step ran no product,
HTTP or SQL. The source32 workflow then restored the original archive into
independent D while C remained empty. Source405 and staged405 matched all 35
source/target fingerprints at schema27, with 17 credential revocations, one
expired play and one new binding; migration28 was not applied. Apply added one
restore audit to406 rows. Two login/logout workflows advanced D to409 and412,
preserving all other fields and all five sequence expectations.

All 14 HTTP responses completed without error or unknown commit; both cookies
closed with logout204 and same-cookie401. Five native CLI commands and ten
read-only observer backends closed, two app starts/stops were normal, and one
normal PG SIGINT shutdown/restart retained the mount and cluster identity.
At its closeout source32 was inactive at D/revision1, generation
`5f41cda3df2b444bf83236099819c74d`, deployment
`d4d1469c3a03a1b8a398521ebf19b032`. PG532226 has start ticks7112069 and invocation
`450ca1fbec084033ad38ddac2afd5030`; original anchor516461 was unchanged at that
source32 checkpoint. The selected tree and protected main/candidate state stayed exact.

The [final fixture disposal](isolated-restore-disposal.json) then completed once.
Final A410/B419 observations matched all35 tables and five sequences per database,
with both observer backends naturally closed. PG stopped normally by SIGINT;
ordinary unmount removed the exact mount319/dev55/768 MiB tmpfs, and the anchor
stopped normally last. All owned PIDs, cgroups and the private network namespace
are gone. All61 exact runtime unit files were preserved in private evidence and
removed, followed by reload; every unit is inactive with no FragmentPath and no
runtime unit file remains. The temporary cluster, A/B/C/D and their roles are
disposed, closing inactive A's credential responsibility without fabricating or
updating revoked_at. Protected main/source55/candidate state and the original
backup, passphrase and old installation archive remain unchanged.

The fixture working/evidence files and dedicated `goby-r69pg`/`goby-r69app` OS
accounts remain explicitly retained; no recursive deletion or account deletion
was performed. Both recovery/restart proofs and their declared disposal are
complete, while core video acceptance, main promotion and full M2-M6 remain open.

Persistent originals remain outside tmpfs; same-mount process restarts will not establish host-reboot
or power-loss durability, so the M2 durability gate remains open.

[Third-party notices](../../THIRD_PARTY_NOTICES.md) now retain 182 original legal
texts and one toolchain VERSION file, with exact-version records for all 11
current Go requirements, all 87 production npm entries and nine built font
assets. [Remote source/copy/font checks](third-party-notices-verification.json)
passed. The inventory remains incomplete:
historical/optional package gaps, upstream icon scope, the project license and
actual distribution contents remain open. This does not close the M6 release gate.
The [staged Git artifact check](third-party-notices-git-verification.json) also
confirmed that all 183 legal/version files and the inventory retained their
verified bytes through Git; targeted attributes prevent newline conversion.

The dependency review separates pre-backup operational inputs from archive and
post-workflow outputs. Independent M2-M6 work can advance under its own gates;
promotion is not a blanket prerequisite. None of these changes waives the
original client errors, timing gap or full release requirements.

## Selected product and candidate

The [audit remediation](audit-remediation-20260913.md) is published as `a623375`.
The [exit diagnostic increment](exit-diagnostics-20260913.md) preserves known
lease, panic and listener failure classes; it does not identify the historical
unexpected exits. The later [restore cancellation fix](restore-cancellation-race.md)
is included in the selected binary. Its [full remote verification](restore-cancellation-full-verification.json)
passed 2,264 tests across 25 packages with race instrumentation and a Linux amd64
build, with zero failures or skips. The 57 frontend assets reuse 41 mocked-API
cases from 64 unchanged source files.

The isolated candidate is
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`, binary SHA-256
`b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`.
Its earlier [binary transition](audited-candidate-cancellation-transition.json) and
[backup-limit correction](audited-candidate-backup-capacity.md) preserved all
35 source tables. The [seed closeout](audited-candidate-seed-closeout.json)
binds eight accounts, three libraries, fourteen media files, thirteen stored
items and ten public catalog entries. Seed, scans and hosting were not rebuilt
for later checker failures.

[Live admission04](audited-candidate-live-admission-closeout.json) passed a full
ten-minute window, 79 complete responses, a 290,550-byte backup download,
ready-plan cancellation and exact owned cleanup. The inactive staged recovery
database remains retained; it is not an empty slot. This is admission evidence
for the preceding `477d26ad...` binary. The [original-client host initialization](audited-original-client-host-startup-closeout.json)
is also closed, with an empty hosting library and its setup credential revoked.

The [TV successor transition](tv-parent-candidate-transition-closeout.json)
completed exactly one stop, replacement and start after 103 remote tool checks.
New epoch `76d7cc71...` runs PID 486706.
All source and inactive tables/sequences, fifteen revoked sessions, seven plays,
two userdata rows, controls and media remained exact. The PostgreSQL process,
configuration and original-client hosting remained unchanged. Old admission04
does not independently admit the new binary.

[Affected admission05](tv-parent-affected-admission-closeout.json) now passes
the new TV list/detail projections and restricted-user access. Its 24 complete
responses include a 60-second health/readiness window and both same-token
logout rejections. Exact reconciliation found two new sessions, two devices
and four audit rows; all seventeen sessions are revoked, all prior playback
and userdata remain exact, and the inactive stage/control files are preserved.
The selected controller passed thirty guards and three actual saved-receipt
integrations. The final [actual lineage reader review](tv-parent-client-lineage-review.md)
also passes, including the fixed public report and two unknown historical
credential IDs. The [first MP3 input](audited-mp3-client01-plan.md) is reviewed
against the exact seventeen-session state. This is API/runtime admission, with
core client acceptance open.

The [MP3 core scenario](audited-mp3-client01-closeout.json) now passes: 287
physical exchanges, 2,880,702 bytes of completed audio delivery, one counted
Stopped lifecycle, count1/55.346884-second userdata and no page errors. All
eighteen sessions are revoked; both browser/gateway workers are closed. The
single retained universal-audio reference is bound to the stopped play and
revoked credential. Old state is exact. The separately reviewed
[first FLAC input](audited-flac-client01-plan.md) preserved this full state.
FLAC also [passed](audited-flac-client01-closeout.json):287 exchanges,
3,127,772 bytes of completed media delivery, count1/55.342315-second userdata,
zero page errors and fully closed workers. At that closeout nineteen sessions were revoked,
with nine plays, four userdata rows and two retained audio references.

The [episode entry](audited-episode-client01-plan.md) was consumed against that
state. Its full TV browse and playback controls completed, but three page errors
correctly failed formal acceptance. The [owned closeout](audited-episode-client01-closeout.json)
reconciles 349 physical exchanges, one counted Stopped chain with count 1 and
59.157962-second userdata, and one unstarted Prepared row from later detail
PlaybackInfo. That closeout recorded twenty revoked sessions, eleven plays, five userdata rows,
two audio references and no encoding jobs remain. All prior rows are preserved.
Both workers exited with code 0 and are absent; candidate/PostgreSQL continuity is checked.
This does not pass episode or overlapping TV browse acceptance. The
[bounded rejection review](audited-episode-client01-review.md) led to the completed
diagnostic and subtitle increments below. Existing product verification and audio acceptance
remain reusable; no episode replay or new diagnostic framework is planned.

The [native rejection increment](native-rejection-observer.md) is now verified:
83 component checks, 35 controller guards and three actual saved integrations
passed. A first synthetic redirect fixture failure is preserved; the corrected
fixture uses its own private loopback server. Both synthetic browser scopes and
the temporary server are closed, with zero original-client runs. The same
increment reused the established movie readiness helper for the subtitle entry.
Original pageerror rejection remains unchanged; metadata association does not
resolve the old movie/episode errors. The [first subtitle entry](audited-subtitles-client01-entry.json)
passed with all35 tables/sequences matching the Episode01 closeout and an
unused actor. That input has now been consumed.

Subtitles01 completed SRT/VTT selection, cue visibility, seek and return to Off,
stop and logout. [Owned closure](audited-subtitles-client01-closeout.json) preserves
all old rows and records twenty-one revoked sessions, thirteen plays, six userdata
rows, two retained audio references and no encoding jobs. Both workers exited
and candidate/PostgreSQL continuity remains exact. Formal acceptance stays open:
two native primitive-undefined errors remain unattributed, and partial media 293
has a 3507.211510 ms context/physical end-time difference outside the unchanged
2000 ms limit. State/ownership closure does not waive this media evidence gap.
Follow the [result decision](audited-subtitles-client01-review.md) into independent
main read-only preparation, preserving all consumed inputs and original failures.

## Movie checkpoints

The [v3 input contract](audited-client-v3-input.md) integrates occupied movie05
history and request-ID-bound server cancellation evidence into the existing
controller/closeout. [Verification](audited-client-v3-verification.json) passed
71 new remote checks plus an actual read-only stdout capture. The unchanged
adapter and movie-lifecycle tests were reused for that exact revision. The
saved movie05 replay now reconciles physical media and durable state, including
an explicitly cancelled zero-byte response that contributes no media delivery.
Its original failed result and four errors with unrecoverable causes remain.

Movie06 consumed its [one diagnostic execution decision](audited-movie06-execution-decision.md).
It failed before playback at `movie_detail_title`: the item URL had changed,
but two movie titles from Continue Watching and Latest Movies remained visible
on Home. The helper incorrectly required a unique title. The new stdout capture
worked even with browser exit 1. No Playing or media request occurred, so zero
page errors in this run does not resolve movie05's errors.

The [movie06 owned-state closeout](audited-core-movie06-owned-state-closeout.json)
passed using saved evidence only. All fourteen sessions are revoked and both
workers are closed. Of 35 tables, 31 are exact; the four changed tables contain
one new session/device, two audit rows, one new unstarted Prepared play and the
explained expiration of the previous unstarted play. All older plays remain,
including two exact counted Stopped rows. The complete userdata row is unchanged:
play count 2 and resume position 1,217,878,390 ticks. No playback reference or
encoding job remains. The 263-row physical ledger includes 261 complete observed
HTTP exchanges, one CONNECT handshake and one rejection before upstream.

The transition readiness correction now [passes 19 remote tests](audited-movie06-readiness-verification.json),
including a regression that reproduces the original failure against unchanged
source. It preserves real action uniqueness, delayed readiness, separate play
lifecycles and owned logout. At that historical checkpoint the controller's
source selection had not changed. Later integration is recorded above; the
readiness result alone was a tooling checkpoint.

Movie execution stays paused under the [plan review](audited-movie06-plan-review.md).
Before another movie attempt, its per-attempt baseline constants must become an
explicitly reviewed closed-state input with matching saved-state regression.
The current v3 controller remains bound to movie05 and cannot admit movie06's
changed state. No movie07 input or run is claimed. The independent existing
actors do not depend on that movie-only adjustment. TV browse subsequently
consumed its own decision, as recorded below. MP3 and FLAC have since passed;
episode is consumed with closed owned state and failed formal acceptance.
Native rejection diagnostics have since passed and Subtitles01 is now consumed
with the explicit gaps above. Each future business run needs a frozen decision,
serialized execution and complete closure. Passing an independent scenario
does not resolve movie errors or authorize main promotion.

Earlier failures remain consumed and linked through their closeouts:
[movie01](audited-core-movie01-prelogin-closeout.json),
[movie02](audited-core-movie02-prelogin-closeout.json),
[movie03](audited-core-movie03-failure-closeout.json),
[movie04 alignment](audited-movie-offline-alignment.json),
[movie05 state](audited-core-movie05-owned-state-closeout.json) and
[movie05 evidence correction](audited-movie05-evidence-contract.md).
They establish neither passing client acceptance nor a reason for automatic
retries. Product verification remains reusable because the binary is unchanged.

## Historical TV browse checkpoint and product correction

[TV browse01](audited-tv-browse01-execution-decision.md) completed the visible
library-to-series, two season selections, Episode 2-1 detail, Home return and
logout workflow in the original client. Its observed cross-season list does not
prove strict season filtering. The browser then failed the adapter's
`candidate_item_detail_binding_changed` assertion; one page error remains
unclassified. The [evidence review](audited-tv-browse01-evidence-review.md)
identifies a missing `SeriesId`: seed mapping derived it from parent edges,
while the checker treated it as a required observed response field. Other
identity, parent, index and duration fields matched.

The [owned-state closeout](audited-core-tv-browse01-owned-state-closeout.json)
passed from saved evidence. Of 35 tables, 30 are exact. The new TV session/device,
two audit rows, one unstarted Prepared play and one zero-history userdata row
are fully explained. All fifteen sessions are revoked; both workers are closed;
all six movie plays and its complete userdata are exact. There are no playback
references, encoding jobs, Playing reports or media requests. Candidate,
PostgreSQL, lease and log prefix remain exact as recorded.

The [adapter correction](audited-tv-browse01-adapter-verification.json) passed
55 remote adapter tests and seven saved-response/failure checks. It preserves
requested-item and parent identity, rejects conflicting SeriesId when present,
and permits redacted diagnostics only for the precisely classified
non-credential assertion. The original failed result and unknown page error
remain. At that checkpoint the controller still pinned the prior adapter and
the correction had not been used by a new browser run. The later verified
integration and MP3/FLAC/Episode01 results above supersede that pending action.

An existing reference Episode detail also proves a distinct product difference:
SeriesId, SeasonId and their names were absent from Goby's mapper. The
[authorized parent metadata increment](tv-parent-metadata.md) now implements
the observed default Episode and Season fields through the existing read
transaction, with same-library ordinary-parent constraints. Its 21 effective
[focused checks](tv-parent-metadata-targeted-verification.json) passed: twenty
unchanged checks plus a corrected HTTP fixture rerun. The first fixture failure
remains retained. [Full verification](tv-parent-metadata-full-verification.json)
passed 2,270 tests/25 packages with race instrumentation and a Linux build,
without failures or skips. Raw test events, source/archive/manifest and binary
were independently reconciled; the isolated worker/PG are closed. Binary
`b0d6769c...` from archive `b363afdc...` is installed through the completed
[current-state transition](tv-parent-candidate-transition-plan.md) and passed
[affected live admission](tv-parent-affected-admission-closeout.json). This is not a Live TV feature expansion;
the nearby `/LiveTv/Programs` 404 is only a timing lead for the unresolved error.
Admission05 and the first audio/episode inputs have since passed entry review;
episode's remaining page errors keep video acceptance open.
The consumed TV actor now needs retained-state admission before any future
rerun, just as movie does. No main service change or complete core acceptance
is claimed.

## Main and parked investigations

The host rebooted at `2026-09-13T06:18:45Z`. The [reboot baseline](resumed-delivery-reboot-baseline.json)
found source55 and source32 main inactive with their installed bytes retained,
and PostgreSQL 17 main active. It made no database preservation attestation.
No main/source55 restart or upgrade occurred in this client increment. Old PIDs
and the [pre-reboot source55 recovery](candidate-source55-restart-closeout.md)
are historical, not current live identities.

The [fresh main identity record](audited-main-readonly-identity.json), captured at
2026-09-13T17:54:09Z, again finds main and source55 inactive. Main's source32
executable matches its historical SHA and 28,172,723-byte size; PostgreSQL's unit
remains active with PID 893. Units, ordered environment-file references and fixed
file metadata are stable during the read. Environment/master contents and
database/recovery state were not read; no lock was acquired and no service,
migration or restore action occurred. This record supplies the initial baseline
for the following private review, not upgrade admission or data preservation proof.

The later [read-only preparation checkpoint](audited-main-readonly-preparation.json)
resolves configured paths and captured primary/revision0/default lifecycle
selection. One settled create corresponds to the retained schema23 backup;
that historical archive is not a new schema27 recovery point. Three fixed
PostgreSQL read-only connections confirm the main5432 cluster/database/role
identities, exact main migration prefix 1..27, 35 tables and matching recovery
marker. The recovery database has no observed non-system relations or settings
table; complete empty-target admission is not claimed. All three transactions
committed, their owned frontends/backends exited, and the existing deployment
lock was released unchanged. Main and source55 remain inactive.

The [preparation review](audited-main-readonly-preparation.md) also records an
actual startup obstacle: backup storage had 520,519,680 available bytes, below
source32's 570,490,880-byte open threshold with current defaults and metadata
reserve. Capacity, the complete startup/configuration chain, pending work,
private recovery materials and key witness remained open at that checkpoint.
No policy change, deletion or startup was attempted. Even settled stores and complete migration
history cannot establish a zero-write normal startup because `ServerID()`
performs an upsert and subsystems can reconcile work and retention.

The subsequent [startup/capacity checkpoint](audited-main-startup-preparation.json)
passed the actual configuration loader for current defaults and a prepared
64 MiB object / 256 MiB total / 128 MiB minimum-free profile. Its conservative
creation floor is 302,055,424 bytes; final free space was 512,126,976 bytes.
At that checkpoint the profile was staged and installed settings were unchanged. The default
8 GiB scratch reservation explains why fixing only the earlier open threshold
would not make native backup creation fit.

The bounded main read found no scan, task-run, child or encoding recovery
candidates, no eligible triggers and no thirty-day-expired activity. Setup is
complete, the managed singleton exists and the task definition matches compiled
values. The first read stopped before connecting because only socket timestamps
differed from the earlier observation. The equality check was too strict for
fields PostgreSQL normally refreshes. Its narrow correction passed seventeen
remote saved-snapshot checks, then the
single actual transaction committed and its owned connection/lock closed.

The [startup review](audited-main-startup-preparation.md) records the explicit
filesystem prerequisite: an absent tmpfs cache path was exclusively created
empty as `goby:goby`, `0700`, without application markers or a service start.
At that checkpoint one unclosed diagnostic file contained 413 complete bytes and
required a registry closure update on startup; no truncation or current-age/count
pruning was expected. The then-installed `Restart=on-failure` required a finite
one-invocation policy before starting.
The later [material checkpoint](audited-main-recovery-materials.md) preserves
the old installation and closes the standalone key observer's preflight failure
without executing it. The later [startup-only closeout](audited-main-native-backup-closeout.json)
superseded those runtime prerequisites at that checkpoint: one source32 start
and owned stop were closed; capacity and the fence were installed, both cache
markers existed and all eleven diagnostic entries were closed. That input was paused
after two controller failures. The later
[reviewed correction](audited-main-backup-ownership-correction.md) used this exact
retained state and completed the native backup/source-key witness. Selected
online rollback and same-mount restart have subsequently closed through the
[A410/B419 checkpoint](isolated-selected-online-rollback.json). Actual source32
installation return/recovery/restart has since passed its [separate checkpoint](isolated-source32-recovery.json).
Final cluster/credential disposal has also [completed](isolated-restore-disposal.json).
Main promotion remains blocked by core video acceptance. The backup's
removed-fence state is retained in its original completed checkpoint.

The [replacement main upgrade contract](audited-main-upgrade-plan.md) requires
one fresh native archive and two distinct isolated restorations: new binary to
schema28, and actual source32 binary to schema27. Its source review identifies
an existing native HTTP backup-creation path; fresh authority, startup effects
and bounded ownership must be reviewed before using it. No main operation is
admitted by this draft or by candidate admission alone.

Global NextUp and automatic-refresh research remain parked. The
[matrix07 record](nextup-global-reference-matrix-07.md) contains 158 requests and
ten empty global results. Two [original-client discoveries](nextup-client-discovery-closeout.md)
issued no physical NextUp request. Existing preservation closures remain valid
for their original scopes; another run does not freshly reverify all 198 roots.
The [Goby comparison contract](nextup-goby-comparison-contract.md) requires new
discriminating evidence or a concrete product decision before reopening.

## Remaining release work

| Area | Remaining acceptance obligation |
| --- | --- |
| Foundation and recovery | Main migration and bounded post-upgrade workflow against the admitted artifact/current state; isolated selected rollback, actual old-binary restoration and restart/disposal proofs are complete |
| Catalog and operations | Representative capacity, blocked storage, reboot and filesystem measurements |
| Playback | Complete pinned original-client journeys, broader direct-play/transcode formats, seeks and subtitle cases |
| Administration | Selected policy, executor and provider extensions from the delivery plan |
| NextUp and refresh | Positive selector/ordering/client behavior and automatic-refresh evidence for those feature claims |
| Hardware | Actual GPU decode, encode and combined-path profiles |
| Packaging | Native arm64/OCI and deployed embedded-bundle/profile acceptance, support rows, project license and dependency notices; focused embedded tests and amd64/arm64 build artifacts are already recorded |
| M7 | Deferred until explicit feature selection and separate acceptance |

A partial internal deployment does not close M2-M6 or claim broad compatibility.
Missing hardware blocks that profile; it does not block unrelated software work.
License and notices must be resolved before external distribution.

## Verification policy

All compilation, formatting tools, tests, browser checks, media probes and runtime
verification use `ssh test-env`. Local verification requires explicit permission
in the current task. An unavailable test environment blocks verification; it
does not authorize a local fallback. Reuse unchanged verification and limit new
checks to the changed risk. No product-wide rerun is needed for these tool edits.

## Supported capacity boundaries

Missing-item reconciliation currently requires a complete proof within 4096
directory handles, 262144 entries, 131072 seen IDs and 64 MiB of observation
storage. Its SQL candidate/closure budget is separately 32768 rows and 32 MiB,
with per-row overhead that can reach the byte limit first. These budgets cover
the library, not only the deleted files. Exhaustion retains missing records and
reports a skipped cleanup while additions and updates may still complete.

Global NextUp query cost and concurrent scan/homepage latency have not been
accepted at a representative large-catalog scale. Neither a small API page size
nor the passing correctness suite establishes that capacity. Existing events
are not durable; supported clients must refetch state after reconnect.
