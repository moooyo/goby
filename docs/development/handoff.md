# Development handoff

Current operational state: the primary service is **active/running** at
source28/schema26, PID 688833/start ticks 5620918, executable SHA-256
`83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae`.
The [primary deployment completion](m3e-main-schema26-completed.json) **passed**,
closing the main-service gate. The independent completion made 13 native calls
using one new owned session, including logout204 and the paired session401.
It performed zero service writes, migrations or restores and preserved all
three historical failure trees. Activity increased from 15 to 17 only through
legitimate login/logout history; post-start whole-table equality is not claimed.
Tool04 completed the real rehearsal and cleanup, main migration, installation
and start. Its [terminal result remains failed](m3e-main-schema26-start-verification-failed-01.json)
at `start-requested`, after 71 intents, with
`The candidate process has an unexpected effective identity.` No smoke ran.
A later `/proc` observation confirms UID995/GID986, matching the fixed operator's
expectations. A transient startup observation is possible but is not a proven
cause. The unchanged tool04 identity check, installed-state check and private
preservation check subsequently passed in an independent read-only review.
The subsequent formal completion and owned-session smoke passed without
repeating migration or restart. The original failed terminal remains unchanged.

The first main-schema26 attempt stopped the service, then failed in baseline
capture with `column_acl_capture_failed`. The subsequent
[tool03 repair attempt](m3e-main-schema26-baseline-repair-failed-01.json) completed
a fresh baseline and dump from one snapshot, then failed in `observe_rehearsal`
before any rehearsal because its SELECT references absent PostgreSQL17 column
`pg_authid.rolconfig`. No main migration ran in either of those earlier attempts.
The main then remained schema25 with 15 activity rows; that read-only observation found zero other database
connections and zero rehearsal databases or roles. These are not authentication
session row counts. Both earlier failed trees remain preserved alongside the
later tool04 start-verification failure; none is relabeled as a successful run.

The isolated candidate remains source28/schema26, PID 682417/start ticks 5168373.
Its [product checkpoint](source28-product-publication.json) is committed and
pushed to `origin/main` at `608e2088ca6aef150833d3f9bbc954c03e5aef5b`, tree
`da8fcd68b41c691c17c5dd0b4d760e38d3606ec3`. The commit contains only 73 staged
internal product/test/catalog files; active tools and documentation are separate.
Six verified harness files are recorded in `07183be`, and ten main/disposal
tool files in `b4a7f0c`. These tooling commits preserve the same product
checkpoint and are published with this evidence update.
The corrected Theme product source28 passed 72 targeted
race regressions and all 1,830 tests across 24 packages, with zero failures or
skips, complete owned cleanup and a successful application build in
`client-backup-run-20260911_114525_fe88ae8d9829`. The run's source
manifest is `72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df`.
The candidate 25-to26 upgrade, six live Music lineage guards and original-client
auxiliary-album flow passed. Similar and ThemeMedia both completed HTTP200
transfers, Home was reached, four UserData projections and preferences stayed
unchanged, and UI logout204 was followed by exact-token401. No page error was
observed; one blocked-resource console error remains recorded. The first new
dual-user browser attempt stopped before submitting A's login and closed its
browser. Input03's subsequent anonymous prelogin diagnostic passed with all
202 observed network requests completed and normal Service Worker startup;
no credentials were submitted. The latest
[input04 dual-user run](m3e-source28-cross-user-failed-02.json) then passed both
users' UI login/detail reads, own/foreign authority checks, state preservation,
WebSocket transport closure and exact UI logout proofs, but the overall run
still failed at `browse_A` before return Home. Automatic PlaybackInfo was
blocked; its database preparation effects remain under scope review. Full
dual-user acceptance and the [planned library restriction matrix](m3e-library-restriction-plan.md)
remain open; that policy gate has not executed.
Main-schema26 tooling's original build and 15 memory guards, its earlier
read-only retention rejection, and its later stopped-service baseline failure
are separate evidence. One real PostgreSQL17 read-only ACL regression passed.
Tool03 then passed its helper build, 26 memory guards and repair preflight;
its successful fresh backup and later rehearsal-query failure remain distinct.
Primary schema26 deployment and its formal smoke/finalization are complete.
Complete dual-user and M3/M4/M5/M6 acceptance remain open.

Development resumed on 2026-09-11 at the user's request. The active next increment
is [M3e real-client acceptance](client-acceptance-m3e.md), following the priorities
below. The previous round closed after M5j native backup/recovery, deployment,
documentation and publication to `origin/main` at commit `4a840fb`.
The partial [source18 M3e checkpoint](verification-m3e-source18-checkpoint.md) was
deployed and published to `origin/main` at `f339b69`; source28 has
now replaced that primary installation as recorded above. The complete planned server
and full Emby compatibility remain unfinished. The
[Similar and ThemeMedia contract capture](client-auxiliary-reads-plan.md) used
separate synthetic comparison fixtures while retaining the source18 deployment.
Similar was subsequently published at `61be0dd`; the Theme product checkpoint
is now published at `608e2088`, and primary deployment completed separately.
Tooling publication and broader client acceptance remain current work.

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
At that historical checkpoint, the main and isolated candidate were source18/schema25.
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
recovery and credential files. That upgrade installed PID 581075/start ticks
3900536, schema25, subsequently replaced by source28 below. The main service
then remained source18/PID 539535/ticks 3115871; that main process is now stopped.
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

The Theme product changes are outside source20 and are now published in the
source28 product checkpoint. Schema26 adds a separate
positive numeric owner namespace, permanent reserved paths, and active/inactive
resource associations. It does not create Item aliases or consume old identity
sequences. Shared predicates now distinguish ordinary catalog visibility from
authorized direct access to active resources. Library queries, aggregates,
folder UserData, media/subtitle/image access and current-session projections
use those boundaries. ThemeMedia query/HTTP drafts implement independent song
and video inheritance, genre-only owner fallback and the separate OwnerId
namespace. Backup/restore checks complete owner coverage and cross-row resource
validity. Scanner integration and its regressions are included in the accepted
source28 build and scoped candidate/client results below. Primary deployment
has now passed separately; broader client and milestone completion remain open.

The restored SSH session confirmed both service processes and the original
verification-cluster HBA. Sixty stable Go files were formatted remotely.
`/opt/goby-test/exec-work-m3e/source-attempt-21` is frozen exclusively for the
schema26 catalog bootstrap, with 3,906 source files and manifest SHA-256
`d37af7f30fa18cbbd71157393c575886528ab49c800af6db401a3939596b7b2b`.
Its unfinished scanner copy is not an accepted product snapshot. The extended
runner explicitly supports schema26/33 tables while retaining immutable
schema23/24/25 catalogs. Its 97 remote memory-only guards passed, followed by
[successful catalog generation](m3e-source21-theme-catalog.json): artifact SHA-256
`e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de`.
Both disposable pairs were removed and the original HBA and preexisting cluster
catalog were preserved. No service was upgraded.
The separate database/backup-only snapshot `source-attempt-22` has 3,907 files,
manifest SHA-256
`a310305aebe670ca8e613943f64b83386bcf41d64afa9befbb2d37a4d9bf0238`.
Its [first targeted run](m3e-source22-theme-database-failed.json) passed 11 tests and failed three tests in their shared
sequence snapshot helper: PostgreSQL does not give sequences a composite row
type for `to_jsonb(v)`. The helper now explicitly captures all three sequence
fields. The failed run and its original evidence remain terminal and retained.
Its separately reviewed empty pair was disposed without changing the original
receipt, logs, credentials, HBA or preexisting catalog; disposal report SHA-256
`2381bbab9058d3a7112b64775fb41926da24653cc59eac50a9d1267156e5a15b`.
Do not rerun either completed operator or relabel the original failure.
The corrected database-only `source-attempt-23` retains 3,907 files with manifest
SHA-256 `b0c1a07d0a0204536d5e89ec6ffe39315ef8fd96ece4577fab26e51f40c6029f`.
Only the sequence test helper changed from source22; its identical 14-test
selection [passed remotely](m3e-source23-theme-database.json), with zero failures
or skips and complete owned cleanup. It is not an integrated product snapshot.

The first library/server Theme snapshot is `source-attempt-24`, 3,908 files,
manifest `3df8ab5eb5f9d641de302e457fa0fe5a61c6fbe681223929e087fc95c9bfeffc`.
Both race-test binaries compiled remotely. The 48-test run
`client-backup-run-20260911_101731_c0db70c07036` passed 46 and failed two test
fixtures: the legacy metadata test omitted three new tables from its exact
addition list, and a foreign-root authorization fixture collided with another
valid item's unique path before reaching authorization. Both fixtures are now
corrected. The [terminal failure](m3e-source24-theme-targeted-failed.json) retains
its original evidence. Its exact empty pair was separately disposed, preserving
the original receipt, logs, credentials, HBA and preexisting catalog; disposal
report SHA-256 is `85a6763e16da66eb24020bd8dd9d245a8fa6d8ba75e156522f8869e6fd678c01`.
`source-attempt-25` contains all 638 current Go inputs, 3,909 files overall,
manifest `8ec6ffbe1675bc7db036d4741e0952641dbc444bc23ff046b3f4aeac5656baed`.
Relative to source24, it changes only the two test fixtures and adds the final
three scanner review regressions plus one real-media HTTP case. Its two race-test
binaries compiled remotely, and [all 52 targeted regressions](m3e-source25-theme-targeted.json)
passed with zero failures/skips and complete owned cleanup. This includes actual
ffprobe scanning and TCP complete/range byte delivery, retirement denial and
revoked-token denial in the synthetic real-media case. Full race regression and
the build workflow started against the same immutable source25, run
`client-backup-run-20260911_102522_aa52920473bf`. The [completed failed attempt](m3e-source25-theme-full-failed.json) found
`TestStoreSimilarAuthorizationScoringAndUserDataShareOneReadSnapshot` still
matching the old `SELECT type FROM items` query. Its intended permission writer
never ran after the production query gained the `i` alias. The test now matches
the actual query and explicitly asserts that the writer ran. The main package
group completed with 1,813 passing tests and that one failure; the final
`recoverydb` package and application build did not run. This attempt cannot
authorize an upgrade. Its exact empty pair was independently disposed, with
the recreated target public namespace pinned to OID 5791149; the original
receipt remains unchanged. Disposal report SHA-256 is
`68c0db5a658342ce7d5778565c6c3c5234a309008497e0e68e355469e53a2d69`.
`source-attempt-26` is frozen with 3,909 files and manifest
`df27faddb9864a9fa7fce00112c373261c8c4f08f32cfacaaf0e7bcbb72783a1`.
Only that Similar test differs from source25; production bytes are identical.
The [corrected Similar regression](m3e-source26-similar-snapshot-fix.json) passed
in `client-backup-run-20260911_105855_454c0c7f4aaa`, with complete owned cleanup.
The [complete-source run](m3e-source26-theme-full-failed.json)
`client-backup-run-20260911_105904_88799d500ae6` is terminal: 1,824 tests passed,
and `TestRecoveryDatabaseStoreIntegration` failed when the Theme semantic
validator replaced a cancelled finalizer's context error with `ErrDatabase`.
The other 23 packages passed; the application build did not run. The failed
report, log, receipt and restored HBA are retained. Read-only observation saved
both disposable databases as private custom dumps and captured their unchanged
rows, sequences and object identities. The source has the exact 33-table catalog
and one explicit owner-default ACL on `public.users`, left by the test's
GRANT/REVOKE sequence; the target is empty. Its independently reviewed operator
passed 12 remote ACL guards and disposed both pairs while preserving the original
evidence, dumps, HBA and preexisting catalog. Disposal report SHA-256 is
`bc6a6c11e2b1e31efaa2f96172641c3421d465c107cf03d260426010e6ced8bf`.
This attempt cannot authorize candidate deployment.
A later static review also found that
`containsAudio` reclassified basenames after the walker had already filtered
root-relative Theme paths. Ordinary Linux `Album/C:Track.mp3` therefore lost
its MusicAlbum boundary. Source27 removes that redundant classification and
adds root/subdirectory music/mixed scanner regressions plus canonical Theme
controls. Source28 additionally preserves cancellation and deadline errors
before and during backup semantic validation, with three focused test groups.
It is frozen at `source-attempt-28`, with 3,911 files and manifest SHA-256
`72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df`.
Its [targeted recovery/Theme run](m3e-source28-theme-targeted.json)
`20260911_114314_f0eb92b8c61a` passed all 72 race regressions with zero
failures/skips and complete owned cleanup. The complete recovery integration
test passed, including refused, cancelled, lease-lost and committed finalizers.
The [complete-source race suite and build](m3e-source28-full.json) passed in
`client-backup-run-20260911_114525_fe88ae8d9829`: 1,830 tests across all 24
packages, zero failures/skips and complete owned cleanup. Report SHA-256 is
`8b2b755020d333de114458b064edc9d3f5e9df048d0a4d356a49f0cb80d2eab1`;
the 27,570,299-byte binary SHA-256 is
`83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae`.
The [candidate upgrade](m3e-source28-candidate-upgrade.json) preserved all old
30-table rows, sequences and private/recovery files and installed schema26 at
PID 682417/start ticks 5168373. Its completed evidence is
`client-upgrade-92afabdded84813d3af44b6b0f86ff47/completed.json`, SHA-256
`954c4c8885e379ab70bdeb3cda84bdd47247768a6053e66cc43dec9566d04975`;
report `client-fixture-report-16937ed58a2985fd63d76c38.json`, SHA-256
`1c07e1eee7e9c00c9e44df78212e733a6915bb7a04e0e816461927e133d1c14c`.
The four-hop Music chain is `client-music-upgrade-chain-source28.json`, SHA-256
`b8d00674951487aa541dcd8d648d77f91293cc496c4f5002ea288af56660a63a`.
All six live lineage guards passed at `source28-music-lineage-guards-01`.
The [original-client album flow](m3e-source28-auxiliary-album.json) passed at
`source28-auxiliary-album-ui-01`, observation SHA-256
`0db4a10867673030e5fb5c730f245d92b9ab9e945f111fccd3386e25dd6e8ab6`.
Both auxiliary transfers completed HTTP200, the real UI returned Home, all
four UserData projections and preferences were unchanged, and logout204 plus
exact-token401 completed. There was no playback or page error. One console
error for a client-blocked resource is retained. The browser is closed.
The retained source26 database evidence is at
`/opt/goby-test/exec-work-m3e/source26-retained-database-evidence-01/observations.json`,
SHA-256 `b145cd7ab9f609bb9f8bf7448e2303bdd1384e65657f216496360d1eb85f2291`.
Its two custom dumps preserve the nonempty source and empty target before
disposal; no ACL or database repair was performed. The subsequent targeted
run uses source28/schema26 and all of `internal/database`,
`internal/backuppg`, `internal/library`, `internal/server`, then
`internal/recoverydb`, selecting
`Theme|TestRecoveryDatabaseStoreIntegration|TestPostgreSQL.*Schema(23|24|25)|TestMetadataMigrationFromThirteen`.
The whole recovery integration test ran, including finalizer cancellation,
lease loss, refusal and commit. The complete-source run and build follow
successful targeted verification; no source26 or source27 binary is eligible.
Its embedded operator/browser copies remain historical; the pending
schema26 deployment operator and lineage checker are verified separately.

The [schema26 candidate guards](m3e-schema26-upgrade-guards.json) passed 50
tests and 207 pure lineage checks. An initial 120-second guard timeout is
retained; a test-only local-scope reuse of already-validated catalogs removed
redundant reads while keeping production checks and the deadline unchanged.
The final Python guard run took 70.31 seconds. Operator SHA-256 is
`df9f0d33b9ff3136a6958a91d855faf47d703e7f5368a953cb0d182aa12bbd3b`;
its [read-only candidate inspection](m3e-schema26-preupgrade-inspect.json)
passed against the existing source20/schema25 process. The [operator code was
installed](m3e-schema26-operator-installed.json) under the fixture lock, retaining
the old code at `fixture-operator-schema26-replacement-01/previous.py` and
preserving the fixture state bytes. This did not replace the candidate binary
or restart its service. The [new browser input](m3e-schema26-browser-input.json)
at `source26-browser-input-01` passed all 17 syntax checks and retains the 15
unchanged source20 input07 dependencies. It replaces only the fixture loader
and lineage guard. It was subsequently used by the successful source28 flow.
Source21 remains the historical catalog-only bootstrap; source26 is a failed
baseline, and source28 is the corrected product snapshot with complete
regression, candidate upgrade and scoped auxiliary client acceptance passed.
Do not mutate any frozen source.

A draft
`upgrade-main-schema25.py` contains only unverified preflight/comparison
primitives; its mutating entry point is deliberately unavailable and must not
be used as deployment evidence.

Independent main-schema26 tooling was corrected through the retained failures
and completed deployment below. `schema24_test_gates` owns `migrate-main-schema26.go`;
`persistent_test_cluster` owns `upgrade-main-schema26.py` and coordinates its
memory guards. These tools are outside the frozen source28 product snapshot.
Their [remote build and 15 memory guards](m3e-main-schema26-tool-verification.json)
passed. Tool03 later completed the fresh backup described below, and tool04
completed rehearsal, migration, installation and start. Independent read-only
verification and formal owned-session smoke/finalization subsequently passed. Their scope is
the existing primary PostgreSQL5432/goby_test identity, with a fresh schema25
backup, separate receipted rehearsal and a protected 25-to26 migration. They
must preserve old rows, sequences, relation OIDs, ACLs and private/archive files;
the main database is never restored. The old schema25 tools remain immutable.
The source28 full run, protected candidate upgrade, Music chain and original
auxiliary-album workflow have now passed as recorded above.
The completed primary upgrade used the separately verified tooling and
successful product/client evidence linked below. Remaining unfinished inputs
must stay outside accepted checkpoint publication.
The Go helper was remotely formatted and imported back into the workspace,
with source SHA-256 `621306bfd3068ea40b49d0c74de3e61b54a5b9e3cd210cb29ce74b11dc4fe32d`.
Its built binary is `main-schema26-build-01/migrate-main-schema26`, SHA-256
`832487e56c62ff187fe069f9c5fc8144508d1f3e062654d5efdd7ad0c897e1a5`.
The new operator SHA-256 is
`b930b6474eebf5571a76a2a14083cad200493ccb9a1f9748fbfb603ec0193237`;
the 15-test guard SHA-256 is
`619fc02d64c50c164f81eb3fe732e35c4fffef80a71d86fee8da9aff45f24076`.
The independent frozen tool snapshot is at
`/opt/goby-test/exec-work-m3e/tool-build-main-schema26-01` (device 2049, inode
3177253), with evidence in `main-schema26-build-01/base-snapshot.json`.
It contains source28 plus its original manifest and the three new tools.
`main-schema26-tool-inputs.json` binds all 3,915 files; its SHA-256 is
`c522bf32b52af8e5aab3ac128eb5fa32a6845c7627f65e5a8fd3a776b85e59cf`.
Both Python files passed remote syntax compilation. The helper build and
all 15 guarded memory tests passed; source membership and bytes remained intact.
Build report SHA-256 is
`a96450f5f0c1d8e5804fd964e891d5672b09aeaf9d736a7214213342fe28e0f8`;
guard report SHA-256 is
`b7490b7d5adda3d6e949e2d5bbcfa98723a4b81fcd64c4cc8b870e4641edb432`.
Both reports are under `main-schema26-build-01`. The guards' matching unit
stdout was recovered from the private system journal into
`guards-01-journal.json`; empty systemd wrapper stdout was not treated as a pass.
The snapshot's published dependency is
`tool-build-source18-schema25-03/scripts/test-env/deploy-client-schema25.py`,
SHA-256 `6622a3b8d38d3956cbf7104ca88dd4f4227d67788c3b350be692d8f19df443ad`.
That initial tool preparation read the original published receipts and product
source without accessing the primary database or service. Later execution
reached the retained failures below. Backup, rehearsal, migration and start
subsequently completed, followed by the separate passing formal smoke/finalization; the old
primary tools must not be rerun.

The subsequent [first actual main preflight](m3e-main-schema26-preflight-failed-01.json)
is retained in `main-schema26-preflight-input-01`. The source18 asset archive
`schema25-tool-build-source18-03/candidate-assets.tar.gz`, SHA-256
`98b3cbc869bf9369464b8cdbb961b845118197999be6f9c934599507ab0f6cee`,
matches all 57 published asset members; source18/source28 web inputs are equal.
The exact main service pin SHA-256 is
`6b2904c4525aaf40d7d68cf0eda967fd395944b6d459079be282379eac364c8f`,
and the explicitly scoped release attestation SHA-256 is
`3fece4bbebaced7caff0eb5a5eba4314ee69334d2fb5d37bccdb3f7582076d01`.
Both are private files in that input directory; the release scope excludes
dual-user acceptance and full compatibility. Preflight arguments SHA-256 is
`9ff217273c19b09cf556e551d44d59ae67145af59263559d5ae285293d57388a`.
That historical preflight stopped in legacy `quiescent()` before any mutation. All five
active-work/trigger counts are zero. Activity has 15 rows, six older than one
day, zero older than 30 days, and earliest timestamp
`2026-09-10T10:26:41.150555Z`. At that preflight, the main process, manager, unit and both fixed
environment files have no retention override; PassEnvironment and
UnsetEnvironment also do not name the key. The actual default is 30 days, and
the three relevant retention source files match source18/source28 exactly.
Tool02 subsequently used a separately pinned startup plan with at most six
hours of validity and a 900-second startup reserve. Configuration, database
time, active-work counts and retention eligibility remain independent gates;
no retention setting or historical activity was changed to bypass them.
Keep tool01 and its failed preflight evidence immutable.

The later actual main attempt is retained at
`/opt/goby-test/backups/main-schema26-v1/run-20260911T125911Z-a3ceb3e924e2ea287959135c`.
Its terminal evidence SHA-256 is
`c0048eec237292cbda89e7b43c2c6b112bf74d22d44cc1b38419662af36b714f`.
The recorded transition is `stopped` to failed `baseline`, with
`column_acl_capture_failed`. At that failure the primary was **STOPPED** with
the old source18 executable and schema25 database, retaining 15 activity rows.
No main migration occurred in that run. The independently scoped
`/opt/goby-test/exec-work-m3e/main-schema26-acl-regression-01` passed one actual
PostgreSQL17 read-only regression. This is scoped ACL-capture evidence, not a
successful deployment. Preserve this failed run independently of the later
repair attempt below.

Tool03's helper build, 26 memory guards and repair preflight passed. Its
[actual repair-baseline attempt](m3e-main-schema26-baseline-repair-failed-01.json)
is retained at
`/opt/goby-test/backups/main-schema26-baseline-repair-v1/run-20260911T131730Z-85526803ca1a8b52a26f0ab7`.
It reached `backed-up`: the fresh baseline and custom dump share one snapshot,
with SHA-256 values
`5366745c3c6dbd6259cb7988d8d2d1986065a16e738f5ebe488c6f4f14ca5082`
and `c821a16a2e2c56c1d094fda822c5bfce32cecb6cc523ddb5706a950c9d6ac7e5`.
Before any rehearsal, `observe_rehearsal` failed because its SELECT reads
`r.rolconfig` from `pg_authid`, which has no such column in PostgreSQL17.
The terminal evidence SHA-256 is
`acc7b4d173f3975399cf58dc1c6b1e9a27658941d2d028bad9424cb287f61107`.
Read-only reproduction confirmed that query failure. The preserved observation
`/opt/goby-test/exec-work-m3e/main-schema26-repair-failure-review-01/failure-observation.json`,
SHA-256 `7ac34458ccea3b168d76a1981d717996d8ba050a48fb01fdc6c673565ec53764`,
records the still-stopped schema25 main, 15 activity rows, no other
`pg_stat_activity` connections to database OID16385 excluding the observer,
and zero rehearsal databases or roles. It does not report an empty business
`sessions` table. Tool04 subsequently performed the distinct forward attempt
below. Both earlier failed trees remain immutable. The retained startup deadline is
`2026-09-11T18:56:44.739406Z`; forward work must recheck that gate and its startup
reserve. Full private baselines and material inventories are not published.

The [tool04 attempt](m3e-main-schema26-start-verification-failed-01.json) is retained at
`/opt/goby-test/backups/main-schema26-rehearsal-repair-v1/run-20260911T132850Z-7f17a39a6e1e08ce9639d6a1`.
The rehearsal completed and was removed. Main schema26 migration is recorded
in `main-migrated.json`, with its post-migration capture completed. Installation
and start completed: the primary is active/running as PID688833 with the
source28 executable. The original terminal still reports failure at
`start-requested`, after 71 intents and before smoke. Its effective-identity
error is retained verbatim in the public summary. Later observed UID995/GID986
match the fixed operator's expected values; the cause remains unresolved.
The later independent read-only review passed the original tool04
`exact_service`, `verify_installed` and `preserved_private` checks, allowing only
normal log append. It binds PID688833/start ticks5620918, schema26, 15 activity
rows, 22 theme-owner rows, zero reserved paths/resources and zero rehearsal
databases/roles. Its observation is
`/opt/goby-test/exec-work-m3e/main-schema26-post-start-review-02/failure-observation.json`,
SHA-256 `003928387a5ee8a656c34410beae89a7159ed50fac374ecc07f37ce730c12365`.
The original terminal SHA-256 is
`6a250c98ad0baf5e770e565e7790967f9be9465e62eadc9e4a0f0075d6bd6c43`;
`main-migrated.json` SHA-256 is
`2cdd286ddf9012d8acc5423250967098a8d42180927cf935491904e2e193f5db`,
and the completed post-migration capture SHA-256 is
`e95c284b08f18cba6417f17cf7d724799d22e75012b4a3fea35cfa65dcefc2ec`.
Preserve this third failed tree. Its later formal completion used only a new
owned-session smoke and final evidence, with no further migration or restart.

The [tool04 build and 27 memory guards](m3e-main-schema26-tool04-verification.json),
[helper root/ACL regression](m3e-main-schema26-helper-regression-02.json) and
[read-only PostgreSQL17 catalog regression](m3e-main-schema26-catalog-regression-01.json)
provide separate tool evidence. The final [primary completion](m3e-main-schema26-completed.json),
public summary SHA-256
`99a990f10a465c5cee88361a142e6f848f28b9757cc8a18da0008833632b3c69`,
passed in the new root
`/opt/goby-test/backups/main-schema26-post-start-v1/run-20260911T134029Z-ffa629d4c596db46c07beeeb`.
Its terminal SHA-256 is
`f77348323a7040d1cbe2c67a96055e1961fe88f1c4ffc73058cc154dbbcb65b9`.
All 13 native calls passed: health/readiness, administrator index, new session
login/readback, overview, capabilities, libraries, tasks, backups/status,
logout204 and the final session401. No backup creation, archive deletion or
restore was requested. The process remained PID688833/start ticks5620918;
schema26 retained 22 theme-owner rows, zero reserved paths/resources and no
rehearsal database or role. Old state was preserved at migration commit, and
the new login/logout audit history remains, raising activity from 15 to 17.
The completion performed zero service mutations, migrations and restores.
All three original failed trees remain unchanged and retain failed status.
This closes the primary deployment gate, not full dual-user or M3 acceptance.

The reviewed product files were committed and pushed to `origin/main` as the
[source28 product checkpoint](source28-product-publication.json), commit
`608e2088ca6aef150833d3f9bbc954c03e5aef5b`. Only the 73 staged internal
product/test/catalog changes were included. The published tree
`da8fcd68b41c691c17c5dd0b4d760e38d3606ec3` was exported to
`theme21-transfer-01/source28-product-index-01.tar.gz`. Its remote comparison
with source28 found no missing or extra product/test files: 3,212 are byte
identical, and the same source20 JSON fixture has only the already-reviewed
CRLF-to-LF normalization. The fixture and its consumer are unchanged from
source20, parsed JSON is equal, and no production literal/embed consumer exists.
Review report: `theme21-transfer-01/source28-product-index-01-reviewed.json`,
SHA-256 `46d192f31fc1644b9009e7a4f1dfc6220813a46e3c0c5b53f67ac41cbec798f6`.
This comparison excludes documentation and operator/browser scripts. The
published tree exactly matches this reviewed tree and reuses the successful
1,830-test source28 full run and matching candidate build. Publication closes
the product checkpoint only; primary deployment later passed through its own
completion evidence. Active tooling/documentation and complete M3 acceptance
remain separate. Later checkpoints must separately bind
those other accepted inputs.

The separate dual-viewer original-client read-isolation harness is frozen.
Its [25 pure mock guards and two syntax checks](m3e-cross-user-input-guards.json)
passed remotely. Input closure: `cross-user-input-01`, containing the two new
scripts plus unchanged `client-browser-goby-fixture.mjs` and
`client-browser-session-proof.mjs` from `source26-browser-input-01`.
Harness SHA-256 is `c2622a85c02deadc2f27a6e466e20931b98bff2ae7492e1bdabd9703dcb8f80f`;
guard SHA-256 is `d12c33f50c50ce652d410e671f0ced5e21db1bb0f83539eac6db7ee99bb0fcfa`.
The [first actual browser attempt](m3e-source28-cross-user-failed-01.json) at
`client-cross-user-source28-01` failed in `login_A` before any login request
was submitted. A had three successful GET reads, zero network guard counters
and no page errors; it closed with no cleanup failure. B never started and no
token was captured. The report SHA-256 is
`6224b7e1753ee91c0f06ba16acc090d0bfb2c04937e883d9524415599c396b61`.
Elapsed time was 15.82 seconds, Playwright1.63.0/Chrome153.0.8010.12. This failed
attempt and input01 remain immutable. Input02 added safe diagnostics and an
explicit prelogin mode; both new syntax checks and all 35 pure guards passed.
Its harness SHA-256 is
`9ccbcf5fbfabd2635b2a724b1c65d87d2cc7824aac3b7e7d8c344475125c3554`,
and guard SHA-256 is
`c340f354b9ca9778d450d4b6e209a287b67641278c00eda23c5d3e3bddada646`.
The [fresh anonymous startup diagnostic](m3e-source28-prelogin-diagnostic-01.json)
at `client-cross-user-prelogin-source28-01` reproduced the empty document and
login-control timeout, with no credential fill or submission. It observed a
Service Worker warning, zero controller/registrations, an empty body and only
three successful reads; report SHA-256 is
`48dd4d537d17190a8563437bd13a9b5605441b60b37e21181d111a727fac0bed`.
The earlier [client acceptance record](client-acceptance-m3e.md) already
establishes that the normal client requires its Service Worker at startup.
Input03 preserves normal Service Workers and uses an enforced forwarding policy
proxy for browser/worker HTTP traffic without a same-origin bypass. Its fresh
anonymous prelogin run at
`/opt/goby-test/exec-work-m3e/client-cross-user-prelogin-source28-02/report.json`
passed, SHA-256
`94f3034e0ac4ccb4c5c4bf0f05ba36bea36c55d7684d0ead49180a2e2a32791d`.
All 202 observed network requests completed and Service Worker startup worked.
No credentials were submitted. This is diagnostic-only evidence, not login,
dual-user isolation or playback acceptance. At that stage actual acceptance
still awaited explicit WebSocket handling. The backend's
inbound envelopes are inert, but outbound controls can affect the client, so
an HTTP GET classification alone is insufficient. Preserve all old input/output
directories and never relabel diagnostics as user-isolation acceptance.

The [input04 actual run](m3e-source28-cross-user-failed-02.json), public summary
SHA-256 `acb8adac9aa30c9b671f36b9f8a2d316c70220ec8dabe3d598ed31624cda44a6`,
followed 92 passing guards. Both ordinary accounts completed UI login200 and
owned Movie detail UI200 transfers. Own API reads200, admin/foreign reads403,
four unchanged UserData projections and preserved preferences/configuration/policy
are recorded. Both browsers completed UI logout204 with exact-token401 and
closed. Each WebSocket CONNECT200/upstream101 handshake was delivered and the
connection closed; this is transport evidence, not a full event/control matrix.
The immutable actual report is `client-cross-user-source28-02/report.json`,
SHA-256 `71d16d2df285637ea67497dce78bfaa6e20ebbacca0559c7af731919475c42a8`.
It remains failed at `browse_A`: return Home was not completed. Each original
client also attempted automatic PlaybackInfo on the detail page, which the
observer blocked. That endpoint performs database preparation writes and must
not be classified as read-only. No causal explanation for the missing Home
step is asserted. Input05 Home diagnostics and preparation-impact review are
ongoing; no additional preparation permission or full dual-user pass is inferred.
The harness uses existing AV viewer A and initial
viewer B. It is designed to prove own-user reads and cross-user denial, compare four media
UserData projections and preferences after UI login and before logout, and
require each exact logout token to be rejected. Normal authentication history
may change; the report does not claim whole-database equality. Required CLI
inputs are candidate SHA, source path and manifest SHA, original Music receipt
SHA, complete Music chain path/SHA and a new direct WORK child output directory.
No policy mutation or playback was admitted in these attempts. Temporary library
policy restriction/restore remains a separate [planned, unexecuted gate](m3e-library-restriction-plan.md),
including the distinct hidden-library404 and cross-user403 matrix.

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

The historical [main source18 deployment](m3e-source18-main-deployment.json) passed
and ran schema25 with that same executable SHA-256, PID 539535/start ticks 3115871.
That historical primary process was stopped during the main-schema26 work;
the new primary is source28/PID688833 as recorded above.
The isolated candidate is source28/PID 682417/start ticks 5168373.
All old business columns and sequences were preserved through the
29-to-30-table schema25 migration, together with the old archives.
Health, readiness, administration, login and seven reads passed, then logout
returned 204 and the exact token was rejected with 401. The empty transcode cache
passed seven independent guards and actual creation. No main restore or old
rollback was performed. Deployment evidence SHA-256 is
`02ed027b353488ab31cb9e4ac3e7cfc4547422bb1a57e7f9cfdfdd945aad0bf3`.
This historical deployment is a partial M3e checkpoint, not completion of the
full milestones. Its auxiliary failures remain retained separately from the
later source28 candidate's scoped success. The checkpoint records the product
and its verification evidence together.

Historical schema25 main-service preparation passed with its tool03. The earlier
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
deployed. M5j is the previous published baseline; the main retains the
historical source18/schema25 checkpoint; source28/schema26 is now running with
primary acceptance completed separately as recorded above.

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
