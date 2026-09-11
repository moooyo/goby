# Development handoff

Development resumed on 2026-09-11 at the user's request. The active next increment
is [M3e real-client acceptance](client-acceptance-m3e.md), following the priorities
below. The previous round closed after M5j native backup/recovery, deployment,
documentation and publication to `origin/main` at commit `4a840fb`.
The partial [source18 M3e checkpoint](verification-m3e-source18-checkpoint.md) is
deployed and published to `origin/main` at `f339b69`. The complete planned server
and full Emby compatibility remain unfinished. The next active work is the
[Similar and ThemeMedia contract capture](client-auxiliary-reads-plan.md), using
separate synthetic comparison fixtures while retaining the source18 deployment.

The initial [20-request auxiliary reference capture](m3e-reference-auxiliary-reads-v1.json)
passed, preserving the old catalog, user state and media and revoking its new
recorder token. Similar on the original MP3 returned the real FLAC peer; theme
defaults and independent enable flags were recorded. The
[new auxiliary media](m3e-auxiliary-media.json) also passed generation and remote
profile verification at `/opt/goby-fixtures/client-aux-m3e-v1`, manifest SHA-256
`dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7`.
The [three-library reference attachment](m3e-reference-auxiliary-libraries.json)
passed: new Movies/TV/Music IDs are 20/57/66. All five existing accounts and their
old visible media data were preserved. The reference now has six libraries;
historical three-library bootstrap/capture inputs must not be rerun.
Positive movies/music/themes/music-roles/artist-exclusion captures passed with
separate recorder tokens and immutable evidence roots. Two controlled metadata
combinations were restored after capture: one genre plus one tag qualifies,
but two shared People do not. The preliminary equal-weight Person score is
therefore contradicted. The subsequent eligibility/ranking variants passed:
one genre plus studio qualifies, one genre plus actor does not, two genres plus
studio outranks the two-feature peers, and two genres plus actor exchanges order
with those peers. Every temporary metadata change was restored; only explicitly
recorded ETag differences remained. The working scorer removes Person credit.
Neither generator nor any completed reader may be rerun or adopt an existing path.

Source19 is a preliminary frozen development snapshot at
`/opt/goby-test/exec-work-m3e/source-attempt-19`, manifest SHA-256
`cdf8775d7ffd0973046d83fef48d8ab3a813105164f807755ae9783a18545708`.
Its [30 targeted music tests](m3e-source19-music-targeted.json) passed remotely
with both disposable pairs removed and the original HBA/catalog preserved.
Music metadata probe version 2 accepts explicit album_artist, while technical
probe version 6 and stored music_source version 1 remain compatible. This is
not a Similar scoring pass, complete regression, new binary or deployment.
The deployed main and isolated candidate remain source18/schema25.
The [separate source19 Similar regression](m3e-source19-similar-targeted-failed.json)
finished with 11 top-level passes and one timeout failure. The 1,050-candidate
whole-catalog test reached its existing 90-second deadline. The complete workload
remains required while the SQL is optimized. Its unit is terminal and HBA was
restored. The independently reviewed empty pair was subsequently
[removed by its exact owned disposal operator](m3e-source19-similar-disposal.json),
preserving the original failed report, log, receipt snapshots, credentials and
output directory. The preexisting cluster catalog and original HBA remain intact.
Do not restart the completed run or reduce its dataset/timeout to obtain a pass.

[Source20](m3e-source20-snapshot.json) is frozen at
`/opt/goby-test/exec-work-m3e/source-attempt-20`, manifest SHA-256
`456f90464a0c18d8f1a31e268a67a5c65dcf1264a7ee69fc45be4d085d94e945`.
It changes the Similar implementation and its reference-constrained expectations;
the music metadata increment is identical to the source19 targeted pass.
Source20 passed [43 targeted remote race tests](m3e-source20-targeted.json), with
zero failures/skips and complete owned cleanup. The unchanged 1,050-candidate
fixture was seeded in 377.86 ms and queried in 85.96 ms. The [complete-source run](m3e-source20-full.json)
`client-backup-run-20260911_081348_169f35f30b8a` passed 1,763 race tests across
all 24 packages, with zero failures/skips and complete owned cleanup. Its
26,866,941-byte executable SHA-256 is
`a5028f865d3638f020c758a77bf1d431ca755699b7767a25b711509a8f8c12a9`.
The [same-schema candidate upgrade](m3e-source20-candidate-upgrade.json) passed,
preserving every existing row/sequence across 30 tables and all old runtime,
recovery and credential files. The current candidate is PID 581075/start ticks
3900536, schema25. The main service remains source18/PID 539535/ticks 3115871.
The source15-to16-to18-to20 Music chain is
`client-music-upgrade-chain-source20.json`, SHA-256
`996117c1df9e6a609905e6f1ed539829b5a61bb94eae4c6a02560ed52e0f0497`.

The [source20 client checkpoint](verification-m3e-source20-similar.md) is partial.
Final browser input07 passed [11 pure mock guards and six lineage guards](m3e-source20-browser-guards-final.json).
The complete current 17-file harness closure is independent of the older frozen
source20 operator copies. [Three initial UI attempts](m3e-source20-auxiliary-album-failed-01-03.json)
stopped before auxiliary requests while correcting synthetic-album Path
omission and the original client's absent card data attributes. All logged out
and proved exact-token rejection; none was relabeled or reset.
The [fourth original-client observation](m3e-source20-auxiliary-album.json) binds
the actual album URL, two track rows and Request objects. Similar returned 200
and completed transfer. Four item UserData projections and preferences remained
unchanged; there was no playback and logout returned 204 followed by exact-token
401. ThemeMedia returned 404 without an observed transfer completion, and one
page error remains. The original workflow stayed failed in album-wait and did
not return Home. This is scoped Similar evidence, not a complete auxiliary or
M3 acceptance pass. All four browsers are closed.
The [publication comparison](m3e-source20-staged-source-check.json) establishes
3,186 byte-identical product/test files plus one explicitly reviewed test-only
CRLF-to-LF JSON normalization. All production source, 17 browser files and eight
executed reference/operator files matched. The original reference provenance
inside that fixture was retained; no additional runtime change was made.
The [input02 failure](m3e-source20-browser-guards-failed-02.json) remains preserved;
its mock wait assertion and nonprivate input-directory mode are not accepted
browser preparation. Do not run a browser from that directory.

Workspace-only, uncommitted Theme work is outside source20: `0026_theme_owners.sql` and its
migration tests establish a separate positive numeric owner namespace, with
an independent newly created identity sequence. They do not create Item aliases
or touch the old entity sequence. The unpublished migration still needs theme
attachment storage, restore completeness checks, the new schema catalog and
full integration. Do not include this WIP in a schema25 source20 deployment.
`internal/library/theme_paths.go` and its seven unrun test functions are also
outside source20. They classify reserved theme directories separately from
direct resource candidates, with no scanner or catalog integration yet.
The ordinary-catalog exclusion must cover counts, entity visibility, album
aggregation, NextUp and folder UserData as well as primary lists; direct active
theme item/media authorization requires a separate path. A draft
`upgrade-main-schema25.py` contains only unverified preflight/comparison
primitives; its mutating entry point is deliberately unavailable and must not
be used as deployment evidence.

The first resumed observation found a rebooted `test-env`: the persistent primary
PostgreSQL cluster was active, but Goby was stopped and the transient reference
service and memory-backed test cluster were absent. The installed Goby binary
still matched the accepted M5j SHA-256. Historical process IDs below are no longer
live identities. M3e records the new isolated verification environment; never
reuse a historical PID or recreate missing scratch data from an old owner record.

The previous isolated M3e candidate was source18/schema25, executable SHA-256
`665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b`,
PID 506532/start ticks 2681316. Its [19 targeted regressions, build and protected
schema25 replacement](m3e-source18-targeted-upgrade.json) passed. Source is
`/opt/goby-test/exec-work-m3e/source-attempt-18`, manifest SHA-256
`fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b`.
All 30 existing tables were preserved, including nonempty music metadata, four
music-role links, user data and preferences. Source18 client validation now uses
the explicit two-hop source15-to16-to18 upgrade chain, SHA-256
`68571d8ca7f418f5dbab4f41d233f00ed8a3e462829ea8ac93e8ec29c2b8283f`,
at `client-music-upgrade-chain-source18.json`. Source17 was never installed.
The [source18 audio record](verification-m3e-source18-audio.md) now establishes
both core journeys: actual playback, pause, both seeks, resume, stop, eight
Playing/Progress/Stopped HTTP 204 responses per format, persisted play count
and position, and UI logout followed by exact-token HTTP 401. MP3 retains its
original return_home harness failure: the saved DOM had already returned Home,
where new Continue Listening and Latest Music sections legitimately duplicated
the album link; its old SPA route was not captured. No MP3 replay or historical
reset was performed. FLAC completed the corrected full flow, including the
actual `/web/index.html#!/home` route and receipted Music library card. Two
auxiliary 404 responses and two page errors remain per run. Both browser sessions
are closed. [Full source18 regression](m3e-source18-full.json) passed 1,741 top-level race
tests across all 24 packages with no failures or skips in
`client-backup-run-20260911_052641_fbc90d022811`. The final build matched the
candidate, both disposable pairs were removed and the original HBA/catalog
were preserved. That source snapshot remains immutable; its process was later
replaced by the source20 candidate above.

The [main source18 deployment](m3e-source18-main-deployment.json) passed and now
runs schema25 with that same executable SHA-256, PID 539535/start ticks 3115871.
This main process is separate from the current isolated candidate PID 581075/start
ticks 3900536. All old business columns and sequences were preserved through the
29-to-30-table schema25 migration, together with the old archives.
Health, readiness, administration, login and seven reads passed, then logout
returned 204 and the exact token was rejected with 401. The empty transcode cache
passed seven independent guards and actual creation. No main restore or old
rollback was performed. Deployment evidence SHA-256 is
`02ed027b353488ab31cb9e4ac3e7cfc4547422bb1a57e7f9cfdfdd945aad0bf3`.
This is a deployed partial M3e checkpoint, not completion of the full milestones;
Similar/ThemeMedia and page-error gaps remain. The checkpoint records the
deployed product and its verification evidence together.

Actual main-service preparation has now passed with tool03. The earlier
[two attempts stopped safely](m3e-schema25-preparation-failed-01-02.json)
before any Go helper execution, database dump, rehearsal or migration. The
first retained 423 material files and exposed recursive mkdir's non-private
intermediate directory; tool revision 02 now creates every component as 0700
and passed 44 guards. The second completed all 444 material copies but rejected
the one-second difference between the PID-file time and SQL postmaster start.
Tool03 separated the SQL and OS timestamp identity checks and passed 47 remote
guards and its build. The subsequent
[fresh preparation](m3e-schema25-preparation-source18.json) passed the same-snapshot
schema23 dump, independent schema23-to25 restore rehearsal and owned cleanup,
with the old main state preserved exactly. Its retained receipt is
`/opt/goby-test/backups/client-schema25-v1/run-20260911T062405Z-975ef4c2e0c2b3078d517dc5/prepared.json`,
SHA-256 `b1d686b1c8aed83aa545dd02618c22793cc8b371f6659a0adc1f40710ddfd0ce`.
Both historical failed runs remain under
`/opt/goby-test/backups/client-schema25-v1`; do not resume, delete or chmod them.
That preparation preserved the then-stopped M5j/schema23 main state. The later
successful deployment above migrated and started source18/schema25 while
preserving the recorded old data and archives; it did not restore the main
database or apply a historical rollback.

The preceding [full-source16 verification](m3e-source16-full.json)
passed 1,739 top-level race tests across all 24 packages with no failures or skips
in `client-backup-run-20260911_043127_5d9524c1f6be`. Its final build matched the
then-installed source16 candidate, both disposable pairs were removed, HBA was restored and
the preexisting cluster catalog remained unchanged. Original-client audio acceptance
uses the exact completed upgrade to connect the existing scan receipt to the
new candidate process. The source16 audio attempts below remain historical
failure evidence, superseded for the scoped audio gate by source18 above.

The historical [source16 MP3 and FLAC UI runs](verification-m3e-source16-audio.md) received
HTTP 206 with the expected audio MIME types and complete actual playback
advancement, pause, forward/backward seeking and resume. Both still fail the
strict stop gate: Playing, six Progress requests and Stopped each return HTTP
404, and user playback data remains unchanged. Both UI logouts returned 204,
showed the login page and rejected the exact token with 401. The input archives
and process pins are preserved. The playback-report correlation investigation
below explained the source18 fix. These source16 runs do not establish audio
journey or progress-persistence acceptance; source18 supplies that later evidence
and its full regression has passed.
The [single bounded diagnostic](verification-m3e-source16-audio-report-diagnostic.md)
confirmed matching nonce, item, token and device values after media HTTP 206,
while MediaSourceId was the bare item ID rather than its `mediasource_` form.
The small error response bodies could not be read, so the diagnostic remains
incomplete for ErrorCode collection; the independent ID comparisons are retained.
Its UI logout and exact-token rejection passed.

[Source17](m3e-source17-snapshot.json) adds only a narrow owned correlated Audio
source alias after the current item authorization lock, retaining the stored
canonical source and all other rejection boundaries. Its [targeted attempt](m3e-source17-targeted-failed.json)
had 18 passes and one new test-stage snapshot failure: denied reports were
correctly rejected, but the assertion also covered the subsequent existing
preparation cleanup that expires inaccessible sessions. Source17 was never
built or installed. Its independently reviewed empty pair was removed and all
failure evidence retained. [Source18](m3e-source18-snapshot.json) changes only
that test's stage boundaries and exact cleanup assertions, manifest SHA-256
`fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b`.
The same targeted suite passed all 19 tests on source18, followed by its build
and protected replacement. The source17 failure is retained as a test-boundary
finding; no production change was made between source17 and source18.

The preceding source15 [schema24-to-25 replacement](m3e-source15-targeted-upgrade.json)
preserved every old business column and added only the two reviewed default
columns. [Real subtitle and ordinary TV checks](verification-m3e-source15-subtitle-tv.md)
passed on source15, including UI stop/logout and exact-token HTTP 401.
Both browser runs are closed. The [controlled Music scan](m3e-music-scan.json)
then passed using [42 verified guards](m3e-music-scan-guards.json). Album ID
`002c2ea2c77769ae9b0b84b6e788998b` now displays `M3e Synthetic Album`; the unchanged
MP3/FLAC IDs display `M3e MP3` and `M3e FLAC`. MusicArtist ID 1 and four real role
relationships were indexed. Original media bytes, item IDs/paths/parents, all
user state and both unstarted revoked-credential Prepared records were preserved.
The scan's native administrator cookie was revoked and independently rejected.
The [source15 MP3 attempt](m3e-source15-mp3-blocked-summary.json) selected the
exact track row, but the client's Audio/universal media request returned HTTP
400 before decoding. Its subsequent Playing/Progress/Stopped reports returned
404. User data and preferences were unchanged. The error dialog prevented UI
logout; exact owned-token API cleanup returned 204 followed by 401, which is
cleanup evidence rather than UI acceptance. The [bounded FLAC run](verification-m3e-source15-scanned-music.md) also received
HTTP 400 with `invalid_audio_request`; its observed Container capability list
includes `container|codec` items rejected by the source15 parser. FLAC UI logout
returned 204 and the exact token was subsequently rejected with 401. Source16
adds explicit qualified capability parsing without weakening output
selectors, permission checks or bitrate limits.
The [full source15 suite](m3e-source15-full-failed.json) finished with 1,722
top-level passes, two failures and no skips across 23 completed packages in
`client-backup-run-20260911_034930_0b4570e66109`. It failed
`TestQueryLatestProjectsSourceAndRepresentativeEntities` and
`TestItemQueriesProjectEntityDisplayNamesAndCreditOrder`. Both old expected
structs omitted the new empty Artist/AlbumArtist collections. Source16 made
those expectations explicit while retaining every legacy entity
ID, name and order assertion. The final recoverydb package and full build did
not run. The retained pair was independently confirmed empty with no active
connections or external dependencies and [removed by its reviewed operator](m3e-source15-full-disposal.json),
preserving the failed receipt, log, report and preexisting cluster catalog.
Source15 is not a full-regression pass. The subsequent runner also records the explicit
source manifest digest in its full report; all [73 existing guards](m3e-runner-manifest-guards.json)
passed remotely.

[Source16](m3e-source16-snapshot.json) is frozen as a complete source15 clone with
seven reviewed overlays: the qualified Universal capability parser and its
tests, the two corrected library expectations, the report manifest field, and
the audio contract. Its manifest SHA-256 is
`066aad0a220342e01428dedd359a65d5822c790b9f7dd0d3facc4d7799f38a72`
across 3,796 files. Every migration and trusted catalog is unchanged. All 28
targeted audio and library regressions passed in
`client-backup-run-20260911_042847_73f4d02c4437`, with exact pair cleanup; the build
and same-schema upgrade also passed. In-flight client-lineage and
deployment tooling are separate inputs and were not copied into source16.

The independent schema25 deployment operator is implemented in
`scripts/test-env/deploy-client-schema25.py`, with
`test-deploy-client-schema25.py` and `migrate-client-schema25.go`. The old
`deploy-backup-recovery.py` remains unchanged and must not be rerun.
Its [42 memory-only guards](m3e-schema25-deployment-tool-guards.json) and
[separate Go helper build](m3e-schema25-deployment-helper-build.json) passed
through SSH. At that historical tool checkpoint the helper had not executed and
no fresh main backup or rehearsal had run; tool03's later successful preparation
and deployment are recorded above. The preliminary tool source is
`/opt/goby-test/exec-work-m3e/tool-build-source16-schema25-01`, manifest SHA-256
`562d35476b1602284ceaccc1da30271d7497542fb06a49e5ed0f9e63b57f9097`.
Artifacts are in `schema25-tool-build-01`; helper binary SHA-256 is
`a5f8afb49ee610c31d5f70260c5fcf306ebdd74103e11344acb5a20a469bfd60`.
This tool build is bound to source16. If the final product source changes,
create a new complete tool-source manifest/build against that final source;
do not relabel this report. Source18's client and full-regression evidence and
the later preparation and main deployment now satisfy those completed gates;
broader compatibility remains open.
The source16-bound [read-only deployment preflight](m3e-schema25-preflight-source16.json)
also passed: artifact/source/guard/build bindings and the protected stopped M5j
state matched, with no operator writes. Its archived assets are a new private
copy of the preserved M5j artifact, SHA-256
`98b3cbc869bf9369464b8cdbb961b845118197999be6f9c934599507ab0f6cee`.
This preflight did not create a fresh operational backup, run the helper,
rehearse a restore, migrate a database, install assets or start the service.
The independent [source18 helper build](m3e-schema25-deployment-helper-source18-build.json)
was recorded in `schema25-tool-build-source18-01`, from
`tool-build-source18-schema25-01`, manifest SHA-256
`8f6c37edaae54c8359a11a76b869cc4fdd191278f48841ee808998143f14e7f8`.
Its binary matches the preceding helper bytes; its new build report is bound
to the actual source18 manifest. The unchanged Python operator/guard inputs
reused the exact prior 42-case guard report. This historical build precedes the
tool02/tool03 revisions and the successful fresh preparation above.
The bounded [post-source18 auxiliary-read plan](client-auxiliary-reads-plan.md)
records the remaining Similar/ThemeMedia evidence and contract gaps. It is a
read-only follow-on audit, not a product change or an empty-response workaround.

The [full source12 suite](m3e-source12-full.json) passed 1,693 race tests across
24 packages, including the final recoverydb package, with no failures or skips.
That build matched the earlier source12 candidate. Both disposable pairs
were removed, HBA was restored and the preexisting catalog was unchanged. That
run is terminal; it is not complete-source15 regression evidence.

Source11 established the original client home, core movie lifecycle
and display-preference workflow. Movie play/pause/forward/backward seek,
stop and UI logout/login/resume completed with real HTTP 204 state reports and
two exact-token logout checks. Auxiliary movie reads still return 404 and cause
recorded page errors, so complete Emby compatibility is not claimed. The
nondefault preference process-restart gate passed and restored the original
viewer's explicit `genreLimitOnDetails=1`. See the active record for evidence.

Source11 passed 26 targeted profile/NextUp regressions and built successfully.
Its [full repository attempt failed](m3e-source11-full-failed.json): 1,668
top-level tests passed, but one legacy device assertion expected HTTP 400 for a
malformed query that the credential parser now rejects with HTTP 401. The final
recoverydb package and full build did not run. The assertion is corrected in the
source12 inputs. The [reviewed empty disposable pair was removed](m3e-source11-full-disposal.json),
with its failed receipt, report and log preserved; that failed run is terminal.
Source10's single canonical JSON test failure is retained; its corrected test
separates empty-slice representation from semantic equality. Source10 was never
deployed. M5j is the previous published baseline; the main service now runs the
source18/schema25 checkpoint and its verification evidence recorded above.

The fixture now has one explicitly added ordinary AV account,
`34b4c24f6568659af7ce17938fae7f81`, with a protected creation receipt and private
`goby-av-browser.json` alias in the work directory. The updated fixture operator
SHA-256 is now `85b1c7ac16646d0af19cd67d649b72f3df6cb7e08eab4357b94d5b6e6ab818de`;
all 37 guards and the real schema25 upgrade passed. The earlier three-account
operator is retained in `fixture-operator-schema25-replacement-01/previous.py`.
Subsequent inspect/upgrade must use the schema25 operator or a verified successor;
the older operators cannot accept the current fixture. Original users, preferences, playback state and
credentials were preserved. The original viewer's legitimate movie position is
now approximately 122.86 seconds with play count 2. AV/TV client runs must use
the added account. The source11 AV/TV runs finished and closed their owned
sessions. [Reference AV](verification-m3e-reference-av.md)
and [TV browsing](verification-m3e-reference-tv.md) passed within their documented
scope. The historical [Goby source11 AV/TV](verification-m3e-goby-av-source11.md) was blocked:
the music album page is blank, subtitle labels are both `en`, and TV browsing
shows repeated Specials. Source12 changes address the missing music
collections/actual child count, external subtitle labels and observed TV query
filters. A separate static review found that the newly strict PlaybackInfo
decoder rejects existing successful reference requests containing the unmodeled
`VideoStreamIndex: 0` extension. Source12 restores the previous bounded
extension-tolerance contract; it does not implement nonzero video-track
selection. Its targeted/full regression and isolated upgrade passed; music and
subtitle acceptance were still open at that source12 checkpoint and were later
resolved within the source15/source18 scopes above. The initial
source12 upgrade preflight rejected a 0600 nonsecret manifest where its reviewed
contract requires 0644; it stopped before service mutation. Correcting that
file's mode left every source byte unchanged and allowed the protected upgrade.

The [source12 original-client TV flow](verification-m3e-goby-source12-tv.md) has completed: two seasons, three ordinary
episodes, detail and Home navigation, unchanged user data, and UI logout followed
by exact-token HTTP 401 (`goby-av-source12-tv-01`). Auxiliary HTTP/page errors
remain recorded; this is ordinary-fixture browsing acceptance. Source12 music stopped at a blank
album page: the four collections and real count now arrive, but the caught
exception changed from reading `0` to reading `Name`. Subtitle labels now work;
selecting SRT delivers the native SRT URL/text and no opening cue, while the
reference delivers VTT. [Controlled UI response observations](verification-m3e-goby-source12-av.md)
confirmed that three PlaybackInfo responses retained native SRT candidate URLs
before local selection without renegotiation. The source15 candidate now projects
the explicit profile onto all supported original-source candidates while retaining
Off and default item descriptions. The source15 UI subsequently rendered SRT
and VTT opening and seek-window cues, restored Off and stopped successfully.
SRT's converted VTT matched the reference bytes; native VTT retains its documented
formatting difference while displaying the same fixture cues.

The [schema25 music increment](client-music-metadata-plan.md) added real music tag indexing and MusicArtist
relations on schema25, plus a separately evidenced subtitle-candidate negotiation
fix. Schema25 is installed on the isolated candidate. The music design keeps technical probe
version 6 and adds independent music metadata version 1, preserves physical item
IDs and existing metadata overrides/locks, and adds MusicArtist identities with
Artist/AlbumArtist roles. A new `item_metadata_state.music_source` JSONB column
defaults exactly to `{}` for old rows; there are still 30 tables. The entity
relationship `credit_group` defaults to 0 for old rows and uses bounded
groups 1/2 for Artist/AlbumArtist. This group enters the new primary key while
unbounded legacy `credit_type` text remains unchanged. Both new-column defaults
need explicit preservation checks. [73 new runner guards](m3e-schema25-runner-guards.json)
and [trusted catalog25 generation](m3e-schema25-catalog.json) passed. The bootstrap
source13 is catalog-only, with manifest SHA-256
`e927174f3da71e16cc21e92148183178df892c3ff51c831f160cf8adda65c14b`;
its generated catalog SHA-256 is
`e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b`.
The owned generation pair was removed and HBA restored. The independent
[source14 migration/recovery gates](m3e-schema25-targeted.json) passed 12 tests,
including the real schema23/24 encrypted transitions and long legacy credits.
[All 37 candidate upgrade guards](m3e-schema25-fixture-guards.json) also passed
against the actual catalog25; no candidate mutation was performed. Source14 is
a schema/backup verification source, with manifest
`abbb5dee98e3546a4b70b333bd91874a0f730d02a698f19b744d65e425313785`.
Source15's targeted regression and build passed before its historical install;
its full-suite and original-client audio failures are preserved above. Source18
is now installed, its full suite passed, and its scoped audio outcomes are recorded.
Existing
migrations and catalogs23/24 stay immutable.
Only the owned Music library was scanned, through the separately verified
one-shot operator at `music-scan-review-02/scan-client-music.py`, SHA-256
`dca0a057c82b0ac29734c320cdfe6aab57a31abae9469ef8dc10c3749a13658e`.
Its fixed output `client-music-scan-v1` is complete and must not be rerun or
adopted. Receipt SHA-256 is
`e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b`.
The two scan runtime scripts are separate from the immutable source15 build
inputs. Review01's [preflight rejection](m3e-music-scan-preflight-01.json) occurred
before any login or scan intent; the revised guard preserved the two legitimately
unstarted records instead of deleting or relabeling them.

## Project and accepted baseline

Goby is an open-source, independently implemented Emby-compatible backend for
Linux, using Go and PostgreSQL exclusively. React/MUI provides a Material Design
administrator dashboard; an end-user web playback client is outside scope.
Use Chinese for user-facing communication and English for code and documentation.
Work directly on `main` and commit/push accepted increments. Toolchain pins are
Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11.

Before M5j, the accepted deployment is M5i `58f98a6`, schema 22/probe 6,
PID 3668655, UID 995, start ticks `26912384`; original Emby PID 3131777.
These recorded identities must be rechecked before acting on a service.
[Progress](progress.md) and the linked per-increment reports establish:

- M0: local API inventory, 2,462 Emby 4.9.5.0 reference records and 240 preserved
  media sources. Research captures are not product/client acceptance.
- M1/M2: identity/setup, non-root service, safe ingestion, catalog/ACL browsing,
  local NFO, entities and indexed local artwork within their documented scopes.
- M3/M4: original playback/state, sessions/NextUp, external SRT/WebVTT, selected
  events/control, HLS and progressive conversion, profiles, restart and refresh.
- M5a–M5i: managed users/metadata/sessions/keys/devices, library task scheduling,
  settings, bounded configuration compatibility, and transactional activity/logs.
  M5i passed 1,380 race tests plus its separate browser, restart and deployment gates.

## M5j final acceptance record

The following results belong to the final source and accepted deployment.
Historical and narrower test counts are not added to the full-suite count.

| Gate | Final result and durable evidence |
| --- | --- |
| Final source30 regression and executable | Passed: 1605 race tests / 24 packages; Linux amd64, CGO disabled — [full suite](m5j-final-full-race.json), [build](m5j-final-build.json) |
| Complete browser/process/offline CLI, transitions/restarts and cleanup | Passed: three browser journeys, six generation checks and three restarts — [runtime acceptance](m5j-runtime-acceptance.json) |
| Joined source, executable, assets and acceptance inputs | Passed: 576 installable source inputs, 40 production web inputs and 57 assets — [final source gate](m5j-final-source-gate.json) |
| Protected deployment and native create/download workflow | Passed; 29-table restore rehearsal before upgrade, then a retained encrypted backup and verified logout — [deployment](m5j-deployment-evidence.json), [deployed workflow](m5j-deployed-backup-workflow.json) |
| Final binary/asset identity, schema/probe and PID/start ticks | Schema 23/probe 6; PID 3750313, UID 995, ticks `28911319`; binary SHA-256 `a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81`; 57 current assets |
| Publication and resource cleanup | Published on `origin/main`; owned verification databases/roles/cgroups removed and HBA restored. [Final source reconciliation](m5j-final-git-reconciliation.json) and [service/backup state](m5j-final-state.json) preserve the final checks. See Git history for this closeout commit. |

M5j product code is implemented: encrypted archives, durable operations, native
HTTP/UI, inactive-slot ownership/reuse, generation activation/rollback and offline
CLI. A real HTTP cleanup race was found: normal-EOF deadline cleanup cancelled a
keep-alive request. The fix and focused regression are recorded in
[HTTP body lifetime evidence](m5j-http-body-lifetime.json). The new whole source30
suite and complete real-runtime journey passed. Source25 and
command26 results are now historical scoped evidence, not final-source acceptance.
UI a6 remains the same 27 mocked-API browser cases and 57 production assets.
The first deployment installed the candidate but could not finalize its report:
systemd emitted repeated `EnvironmentFiles` lines and the original parser kept
only the last one. [That failed attempt](m5j-deployment-failed-attempt-1.json)
is retained. [54 deployment guards](m5j-deployment-remediation-tests.json) passed
before read-only forward finalization. That finalization rechecked all installed
artifacts, original data, master/media/logs, recovery ownership and process
identity; it did not restart or restore the service. The main workflow then
passed in 2.373 seconds, preserving all old rows and retaining seven audit records
and one new revoked administrator session. Its downloaded archive was checked
by size, SHA-256 and age header; that main-service archive was not restored.

## Native operator workflow and locations

The [backup/recovery guide](backup-recovery.md) and [native API](../api/backups.md)
are the operational contract. Emby BackupRestore/plugin routes and its archive
format remain unsupported. The archive includes the database, five allowed
logical defaults and the matching application-key master; media and deployment
credentials are excluded. Preserve original media mounts and the database/master pair.

The accepted M5j deployment uses these paths. Recheck current ownership and
generation before an operator action; these paths are not scratch space.

| Purpose | Linux path |
| --- | --- |
| Lifecycle/generations (`GOBY_RECOVERY_STATE_DIR`) | `/var/lib/goby-test/recovery-m5j` |
| Encrypted store (`GOBY_BACKUP_DIR`) | `/var/lib/goby-test/backups-m5j` |
| Operation journal (`GOBY_RECOVERY_OPERATIONS_DIR`) | `/var/lib/goby-test/recovery-operations-m5j` |
| Private recovery database environment | `/opt/goby-test/recovery-m5j.env` |
| Inactive recovery database / role | `goby_recovery_m5j` on the primary PostgreSQL cluster at port 5432 |
| Main runtime / existing administrator credentials | `/opt/goby-test/runtime.env` / `/opt/goby-test/browser.env` |
| Private verification evidence | `/opt/goby-test/exec-work-m5j/` |
| Operational pre-upgrade backup | `/opt/goby-test/backups/m5j-20260910/` |

`verify-backup-recovery-deployed.py` checks the accepted candidate/deployment,
logs in with the existing native administrator, creates one backup, waits for its
completed job, verifies HEAD/range/full attachment bytes and SHA, then proves
logout by HTTP 401 and SQL. It does not restore, delete, change settings or touch media.
A successful run retains the encrypted object in the native store and a copy plus
its passphrase outside all three strict stores:

- `/var/lib/goby-test/operator-secrets-m5j/attempt-01/`: UID 995, mode 0700;
  `<RequestId>.passphrase`, `<BackupId>.age`, `request.json` and `backup.json`: mode 0600.
- `backup.json` pairs the accepted file, digest and passphrase filename. Preserve
  `request.json` and the sole passphrase even when admission is uncertain.
- Safe before/after hashes and the report are under
  `/opt/goby-test/exec-work-m5j/deployed-backup-workflow-01/`, root-only mode 0700.
  Later explicitly selected attempts use their own numbered directories.

For offline recovery, stop the service and use the installed binary with the
same deployment UID/environment. Follow the documented import → plan → apply
sequence; obtain IDs/revisions from actual output. Passphrases use a protected
0600 file or stdin, never command arguments or logs. Start the normal service
after accepted CLI output and sign in with a restored account. The CLI's
`--accept-no-rollback` acknowledges its documented offline boundary, not permission
to erase an unrelated database. Do not add arbitrary files inside the strict stores.

`deploy-backup-recovery.py` is a **one-shot M5i-to-M5j deployment operator** pinned
to that baseline. Its recovery mode is for an unpublished interrupted attempt,
not a general rollback tool. After publication, do not rerun it or restore a
historical M5i/M5j operational backup over newer accepted business state.
Future upgrades need newly reviewed ownership, baseline and preservation gates.

## Resume and later-round priorities

Start with this final record, [progress](progress.md), the backup guide and the
tracked reports above. They are the durable authority for a cloned repository;
no ignored checkpoint file or old live-session handle is required. If a final
M5j record conflicts with current state, reconcile the exact evidence before
editing or operating the service. The resumed scope follows these priorities;
each increment still needs its own source, runtime and publication evidence.

| Suggested later priority | Still open |
| --- | --- |
| P1 — M3 / client acceptance | More events/subscriptions, broader subtitles, global NextUp parity and reproducible real-client flows. |
| P2 — M4 | Nonzero copied-video seeking, efficient audio I/O, more tracks/formats, aggregate isolation and actual GPU decode **and** encode. |
| P3 — remaining M5 / metadata | More task executors, full policies, providers, broader configuration fields/sections and metadata/artwork reconciliation. |
| P4 — M6 | Differential client/reference coverage, Linux distribution/architecture/GPU matrix, large-catalog upgrades, operations and recovery coverage. |

M7 remains deferred. Software success is not GPU verification; matching routes
alone is not third-party playback compatibility. These priorities remain open
after M5j acceptance.

All tests, validation, builds, runtime/media probes and browser checks run through
`ssh test-env`. The resumed task does not authorize local verification. If SSH is
unavailable, report verification blocked. Keep main PostgreSQL `5432` protected;
use owned fixtures on isolated `15432`, serialize HBA and memory-heavy work, and
give every fixture three unique private recovery directories. Never weaken KDF
strength, clear an arbitrary populated slot, or delete retained evidence to pass a gate.
