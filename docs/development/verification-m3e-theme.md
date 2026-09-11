# M3e Theme implementation verification

Status: **product checkpoint published; primary schema26 deployment completed; broader client acceptance open**.
Source28/schema26 passed 1,830 race tests across 24 packages and is installed
on the isolated candidate. The original-client auxiliary album workflow passed;
the primary is now source28/schema26, active/running as PID688833/start ticks5620918. The earlier
[tool03 main repair attempt](m3e-main-schema26-baseline-repair-failed-01.json)
completed a fresh same-snapshot baseline and dump, then failed before rehearsal
on an invalid PostgreSQL17 `pg_authid.rolconfig` observation query. No primary
migration ran in that failed attempt. The later
[tool04 result](m3e-main-schema26-start-verification-failed-01.json) completed
rehearsal/cleanup, main migration, installation and start, then failed its
post-start effective-identity assertion before smoke. Later UID995/GID986 match
the fixed operator; a startup transient is possible but unproven. A subsequent
independent read-only review passed the unchanged service, installed-state and
private-preservation checks. Formal owned-session smoke/finalization
[subsequently passed](m3e-main-schema26-completed.json) without any additional
service mutation, migration or restore. The three failed runs retain their
original status. The prior
[Similar checkpoint](verification-m3e-source20-similar.md) is still partial:
its original-client ThemeMedia request returned 404 and the workflow did not
return Home. The work below does not relabel that observation.

The [source28 product checkpoint](source28-product-publication.json) was committed
and pushed to `origin/main` at `608e2088ca6aef150833d3f9bbc954c03e5aef5b`.
Its tree `da8fcd68b41c691c17c5dd0b4d760e38d3606ec3` is the exact reviewed
product tree. The commit contains 73 changed internal product/test/catalog
files and excludes active tools and documentation. Existing source28 full-test,
build and correspondence evidence binds this checkpoint. Primary deployment
is established by its separate completion report; complete M3 acceptance is
not established by product publication or deployment. Later verified harness
and main/disposal tool changes are recorded in `07183be` and `b4a7f0c`
respectively and are in the published history. The main-deployment/evidence
checkpoint `53144e6` has been pushed to `origin/main`.

The independent post-start completion also passed its
[six isolated guards](m3e-main-schema26-completion-guards.json). A subsequent
read-only observation identifies the two new activity entries as native
[`session.login` and `session.revoked`](m3e-main-schema26-authentication-activity.json).
The original deployment failure receipts remain unchanged.

## Implemented scope

`GET /emby/Items/{Id}/ThemeMedia` has explicit independent song/video controls,
optional parent inheritance and the observed empty soundtrack group. The query
authorizes the subject, seed, ancestors, resources and user projection in one
read-only repeatable-read snapshot. Durable positive `OwnerId` values belong to
a separate namespace; they are not item aliases. Disabled groups remain empty
with owner zero. Parent inheritance resolves songs and videos independently.

Positive reference captures explicitly selected their detailed fields, so an
unqualified ThemeMedia request uses the ordinary list projection. Resources
retain their own Audio/Video identities, owner ParentId and matching ExtraType.
Genre fallback uses the owner only when the resource has no own genres and no
explicit genre override or lock, including an explicitly empty control. People,
studios, tags and years do not inherit. The direct and Theme projections share
this rule. Positive reference evidence establishes genre fallback for audio;
the symmetric video rule and native override/lock precedence are explicit Goby
choices beyond that sample. Source audio tags retain their meaning; the reference fixtures did
not establish that embedded titles should be ignored.

Schema26 adds `theme_owner_ids`, `theme_reserved_paths` and
`item_theme_resources`, with an independent new identity sequence. Existing
item rows and old sequences are preserved. Backfill reserves only known
canonical historical layouts and does not guess attachment owners. Backup and
restore validate complete owner coverage and resource relationships, including
the separately constrained inactive history after an owner moves or changes
role. Reads never repair missing identities.

Ordinary browsing, counts, entity visibility, Similar, Latest, NextUp, album
aggregation and folder UserData exclude every permanent resource association
and reserved path. Authorized direct access admits only active resources with
valid ownership and shape. Existing public-image policy is retained within
that active-resource boundary. State writes recheck classification after
acquiring item locks so a concurrent reservation cannot modify hidden state.

Scanning separates reserved directories from direct candidates. It publishes
bounded owner groups atomically, counts both resource kinds across roots and
rejects ambiguous owners or competing song layouts. Complete-root absence
proof is required before deactivation; unknown, present or uncertain old paths
remain retained. The final scan review addressed deferred replacement order,
retained resources in cross-root layout checks and synthetic root Series
ownership. These constraints are Goby implementation choices where the
reference observations do not uniquely establish an algorithm.

## Remote evidence

All formatting, compilation, tests and runtime/media checks run through
`ssh test-env`. No local verification was performed.

- [Source21 catalog](m3e-source21-theme-catalog.json): 97 memory-only operator
  guards passed. Real PostgreSQL17 schema26 generation produced a 33-table
  catalog with SHA-256
  `e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de`.
- [Source22 database failure](m3e-source22-theme-database-failed.json): 11
  regressions passed and three failed because their shared test helper tried
  to convert a sequence relation to a composite JSON row. The original failed
  run remains preserved. A separately reviewed operator removed only its empty
  owned pair.
- [Source23 database verification](m3e-source23-theme-database.json): the
  corrected identical selection passed 14 race regressions with zero failures
  or skips, including historical schema23/24/25 archives and Theme semantics.
- [Source24 library/server verification](m3e-source24-theme-targeted-failed.json) compiled both race-test binaries, then
  passed 46 of 48 regressions. Its two failures were test fixtures: a stale
  expected-table list and a unique-path collision before an authorization
  assertion. Both corrections are included in source25; the failure remains
  preserved separately.
- [Source25](m3e-source25-theme-targeted.json) contains all 638 current Go inputs, final scanner review regressions
  and one real-media HTTP case. Both race-test binaries compiled; all 52 Theme
  race regressions passed with zero failures/skips and complete owned cleanup.
  The [full attempt](m3e-source25-theme-full-failed.json) passed 1,813 tests but
  failed one stale Similar concurrency-test SQL hook. Its final recoverydb
  package and build did not run, so it cannot authorize deployment. Manifest SHA-256 is
  `8ec6ffbe1675bc7db036d4741e0952641dbc444bc23ff046b3f4aeac5656baed`.
- Source26 changes only that test hook and requires the concurrent writer to
  have run. The [corrected single regression](m3e-source26-similar-snapshot-fix.json)
  passed with complete cleanup. Its [complete-source run](m3e-source26-theme-full-failed.json)
  passed 1,824 tests and failed recovery finalizer cancellation: the new Theme
  validation replaced the caller's context error with a generic database error.
  No application binary was built. The retained database pair was read without
  mutation and saved as private custom dumps before a separate disposal review;
  manifest SHA-256 is
  `df27faddb9864a9fa7fce00112c373261c8c4f08f32cfacaaf0e7bcbb72783a1`.
  A later review found a separate ordinary-album regression in this source:
  basename-only Theme reclassification incorrectly rejects `C:Track.mp3`
  after root-relative classification already accepted it as ordinary media.
  Source27 removes the redundant pass and adds root/subdirectory music/mixed
  scanner regressions. Source28 also fixes context error propagation and adds
  three focused validation test groups. Its 3,911-file manifest is
  `72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df`.
  Source26 cannot authorize deployment. The source28 [72-test targeted run](m3e-source28-theme-targeted.json)
  passed with zero failures/skips and complete owned cleanup. It includes the
  complete recovery integration test and all four transactional finalizer
  outcomes. The [complete source28 race suite and application build](m3e-source28-full.json)
  passed in `client-backup-run-20260911_114525_fe88ae8d9829`: 1,830 tests
  across all 24 packages, zero failures/skips and complete owned cleanup.
- [Candidate operator and lineage guards](m3e-schema26-upgrade-guards.json)
  passed 50 operator tests and 207 pure lineage checks. The initial guard timeout
  is preserved; only redundant reads in the test helper were removed.
  [Browser input preparation](m3e-schema26-browser-input.json) passed 17 syntax
  checks. This input subsequently passed the source28 client workflow below.
- The new operator [inspected the existing candidate](m3e-schema26-preupgrade-inspect.json)
  successfully and was [installed under the fixture lock](m3e-schema26-operator-installed.json),
  with its predecessor retained. Only operator code changed; the application
  process and fixture state remained at the inspected schema25 checkpoint at
  that preparation stage; the later upgrade is recorded below.
- [Source28 candidate upgrade](m3e-source28-candidate-upgrade.json) preserved
  all old 30-table rows/sequences and private/recovery files through schema26.
  Executable SHA-256 is
  `83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae`;
  candidate PID is 682417/start ticks5168373. Six live Music chain guards passed.
- [Original-client album acceptance](m3e-source28-auxiliary-album.json) completed
  both Similar and ThemeMedia HTTP200 transfers, bound the actual album URL and
  two track rows, returned Home, preserved all four UserData projections and
  preferences, and completed UI logout204 plus exact-token401. There was no
  playback or page error. One client-blocked-resource console error is retained.
  This establishes the scoped empty Theme response journey for the original
  synthetic album; positive resource delivery remains separate HTTP test evidence.
- [Initial primary-tool verification](m3e-main-schema26-tool-verification.json)
  passed the independent helper build and all 15 synthetic memory guards.
  The first actual main attempt stopped the service and failed at baseline
  column-ACL capture. A real PostgreSQL17 read-only regression then passed.
  [Tool03](m3e-main-schema26-baseline-repair-failed-01.json) passed its helper
  build, 26 memory guards and repair preflight. Its fresh baseline/dump passed,
  but `observe_rehearsal` failed before any rehearsal on absent
  `pg_authid.rolconfig`. The main then remained STOPPED at schema25 with 15 activity
  rows; zero other database connections and zero rehearsal databases/roles were
  observed. Both earlier failed trees remain preserved.
- [Tool04](m3e-main-schema26-start-verification-failed-01.json) completed the
  real rehearsal and cleanup, main migration, installation and service start.
  The main is source28/schema26, active/running at PID688833. Its terminal still
  reports the post-start identity assertion failure at `start-requested`, with
  71 intents and no smoke. Subsequent UID995/GID986 match the fixed expected
  identity; no cause is established. The later independent read-only review
  passed, binding PID688833/start ticks5620918, schema26/activity15 and 22
  theme-owner rows, with no reserved paths/resources or rehearsal database/role.
  Its SHA-256 is `003928387a5ee8a656c34410beae89a7159ed50fac374ecc07f37ce730c12365`.
  The original failed terminal remains retained separately from the passing
  completion below.
- [Final tool04 verification](m3e-main-schema26-tool04-verification.json) passed
  its helper build and 27 memory guards. The
  [helper root/ACL regression](m3e-main-schema26-helper-regression-02.json) and
  [read-only PostgreSQL17 catalog regression](m3e-main-schema26-catalog-regression-01.json)
  retain their scoped results; they are not counted as client acceptance.
- [Primary deployment completion](m3e-main-schema26-completed.json) passed in
  `main-schema26-post-start-v1/run-20260911T134029Z-ffa629d4c596db46c07beeeb`.
  Public summary SHA-256 is
  `99a990f10a465c5cee88361a142e6f848f28b9757cc8a18da0008833632b3c69`;
  terminal SHA-256 is
  `f77348323a7040d1cbe2c67a96055e1961fe88f1c4ffc73058cc154dbbcb65b9`.
  All 13 native health/admin/session/read/logout calls passed, using only a
  new owned session and ending with logout204 and the paired session401.
  Completion performed zero service writes, migrations and restores. Main
  PID688833/start ticks5620918, source28/schema26 and the 22 theme-owner rows
  remain; reserved paths/resources and rehearsal databases/roles are zero.
  Activity15-to17 is the retained legitimate login/logout history. No post-start
  whole-table equality is claimed. All three old failure trees remain unchanged.
- [Cross-user input guards](m3e-cross-user-input-guards.json) passed 25 pure
  checks. The [first real attempt](m3e-source28-cross-user-failed-01.json) stopped
  before submitting A's login, with three successful read requests and a closed
  browser. It does not establish authentication or isolation acceptance.
  Input03's anonymous prelogin diagnostic subsequently passed at
  `client-cross-user-prelogin-source28-02/report.json`, SHA-256
  `94f3034e0ac4ccb4c5c4bf0f05ba36bea36c55d7684d0ead49180a2e2a32791d`:
  all 202 observed network requests completed, normal Service Workers worked,
  and no credentials were submitted. This remains diagnostic-only evidence.
- [Input04 dual-user partial result](m3e-source28-cross-user-failed-02.json)
  followed 92 passing guards. Both ordinary users completed actual UI login200
  and owned Movie detail UI200 transfers; own reads200, admin/foreign reads403,
  four unchanged UserData projections and preserved preferences/configuration/policy
  are recorded. Both UI logout204 requests have exact-token401 proof, and both
  browsers closed. Real WebSocket CONNECT200/upstream101 transport also closed.
  Overall status remains failed at `browse_A`: return Home did not complete,
  and the clients' automatic detail-page PlaybackInfo requests were blocked.
  PlaybackInfo has database preparation effects and is not relabeled read-only.
  That failed input04 report remains unchanged.
- [Input05 preparation scope](m3e-source28-cross-user-preparation-01.json),
  public SHA-256 `91a8d3bc7a2e604cd62f5041b7a9403463baacb88c53901649c11a52f8b7295a`,
  passed 100 pure guards and actual Home-to-Movie-to-Home UI journeys for both
  users, including a genuine PlaybackInfo200 response each, own/foreign checks,
  four state projections and UI logout204/exact-token401. The selected mode was
  explicitly `acceptance-preparation`, admitting bounded preparation writes.
  Its private comparison retained 18 old play rows, with 17 unchanged and one
  eligible B row Expired; A's one revoked-auth reference was removed. Two new
  Prepared rows and two new, subsequently revoked Emby auth rows were recorded.
  All 47 old auth rows and five UserData rows stayed unchanged; encoding was zero.
  **Complete client acceptance remains open:** each client recorded one
  unclassified `ui_movie` page error. The driver did not reject page errors,
  so its passed result is not promoted beyond the flow/state scope. Raw reports
  and all earlier successes/failures remain unchanged.

Input06 is adding a strict page-error gate and sanitized diagnostics from normal
browser events. The next candidate baseline is 20 plays, 49 auth rows and zero
references; the two new Prepared rows already have revoked authentication.
Use a fresh private snapshot and a newly bound comparison scope, not input05's
old 18-play/47-auth comparator or before image. Library restriction has not
executed and remains behind page-error diagnosis.

Catalog and completed database runs preserved the original HBA and preexisting
cluster metadata. Source24's separately reviewed disposal also preserved its
original evidence. Frozen sources and terminal operators must not be mutated,
adopted or rerun.

## Remaining gates

The primary deployment gate is closed. Full dual-user client isolation,
[temporary library restriction/restore](m3e-library-restriction-plan.md) and
remaining tooling/browser/evidence publication remain open. The restriction
plan, including its 404-versus403 matrix, has not executed. The product-only
checkpoint and the main-deployment/evidence checkpoint `53144e6` are already
pushed; new input06 work and its evidence still need their own completion gate.
The successful retained backup is a separate gate; its existence does not
authorize replaying a failed directory or bypassing the startup deadline.
The new real-media case
uses synthetic files in test-owned temporary directories and actual ffprobe,
scanner and TCP byte delivery; it does not establish original-client decoding,
presentation or playback controls. The standalone ThemeSongs/ThemeVideos routes,
soundtrack relationships and broader M3/M4/M5/M6 completion are not claimed.
