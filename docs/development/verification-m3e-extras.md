# M3e persistent Movie extras verification

Latest execution: **positive-fixture setup01 failed after creating its library;
no scan was dispatched. An independent continuation is authorized but has not run.**
Its read-only preflight was exercised separately and has not dispatched a
business request. The [first continuation diagnostic](m3e-positive-continuation-preflight-failed.json)
found that the guard model expected zero for `session.login.affected_count`,
whereas the retained actual audit rows and source use one. A new profile v2
corrects that expectation and passed eight remote guard groups, including the
actual retained audit projection. The unchanged continuation transport passed
14 guards with that new profile.
The [next read-only diagnostic](m3e-positive-continuation-layout-preflight-failed.json)
then found a reused extension-stage check still required 13 items and three
libraries. The valid create-only state has 14 items and four libraries. A
separate continuation-specific quiescence check is being prepared; it must
preserve all playback/task/encoding fences without trimming the snapshot.
Both diagnostics retained the same current state hash, created no continuation
output directory and made zero HTTP requests or database mutations.

The [12 pure guards](m3e-positive-fixture-tool01-guards.json), two syntax checks
and preflight passed before the actual attempt. A single create request
returned 201 and created library `57a85c1ca5b6c7ae602c587755250b2f`, with root
`604d2c0f5c78919a6ee360cda2048066`. Its path and allowed root are exactly
`/opt/goby-fixtures/client-special-features-m3e-v1/Movies`, with relative path `.`.
The [retained failure](m3e-positive-fixture-create-failed.json) has SHA-256
`9ce73d72d9ce002dce06230a73213294903800b3b49b06e9b9aa33ad69eda2c0`.

`checkpoint('library_acknowledged')` saved fixture state, then its private
`phase-library_acknowledged.json` write was rejected by the tool's own
`[a-z0-9-]+` filename rule. The [read-only/memory diagnosis](m3e-positive-fixture-phase-diagnosis.json),
SHA-256 `ddc675bfb4e1e0d9c66d53d2203da1ed86cf8d81203a819cb686c3fff9b92190`,
reproduced rejection for every underscore-bearing phase. The root SELECT and
scan POST were never reached; this is an operator checkpoint failure, not a
product scanning failure. The original administrator session logged out 204
and its exact token returned 401. No viewer login or new scan occurred.

Fixture state is paused at phase `preparing_special_features_fixture`, stage
`library_acknowledged`, under the original v1 profile marker. Current state
SHA-256 is `513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1`.
Candidate PID748513/start ticks6996875, runtime configuration and source32
binary are unchanged. The current database has 35 tables, 14 items (the old 13
plus one new CollectionFolder), 4 libraries, 4 roots, 14 metadata rows, 15 Theme
owner rows, 59 whole-database authentication rows (58 plus the new revoked
administrator session) and 129 activity rows (126 plus 3). All old rows and media
were preserved; both Extra tables remain empty. These global auth counts are
distinct from the earlier browser comparison's selected A/B auth scope.

The authorized independent continuation uses
`/opt/goby-test/exec-work-m3e/client-special-features-fixture-continuation-v1`
and `/opt/goby-test/exec-work-m3e/client-special-features-continuation-inspection-v1`.
It must bind the original failure, create201 acknowledgment and complete old
evidence trees. It will scan only the existing library using a new administrator,
then use a viewer for protocol and complete/range delivery checks. It must not
recreate/delete the library, replay setup01 or write success into the old v1
tree. Frozen tool01 SHA-256
`84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465`
and its failed result remain immutable.

The three-stage profile must distinguish the original creation actor, continued
scan actor and viewer. New authentication and audit rows require actual evidence;
three sessions/nine audit entries from the original baseline are anticipated,
not an observed final result. UI, ledger and primary-deployment consumers need
the new chain. Verified main27 tool02 remains undeployed and needs that consumer
revision. No continuation scan has run; primary remains source28/schema26.

Established checkpoint: **source32 regression, build, candidate upgrade,
original-Movie dual-user flow and precise media-root extension passed**.
The primary remains source28/schema26. The source32/schema27 candidate now runs
as PID748513/start ticks6996875. The original Movie's SpecialFeatures404/page-error
gate is closed within its empty-extra scope; nonempty positive-fixture and
original-client acceptance remain open.

The implementation adds permanent Movie extra associations and reservations,
shared current-schema visibility, anchored scanning with atomic owner batches,
SpecialFeatures/LocalTrailers arrays and resource DTOs. Historical schema26
semantic validation remains independent of the new tables. Migration27
deactivates only active Theme relationships whose previously ordinary owners
are covered by newly derived extra reservations; identities and UserData remain.
See the [implementation plan](special-features-implementation-plan.md) and
[actual reference contracts](m3e-special-features-positive-contracts.md).

All execution below ran through `ssh test-env`; no local tests, builds or
runtime probes were performed. Formatting also ran remotely.

## Catalog and operator gates

The schema27 runner and its pure guards were frozen with SHA-256
`7cf91591efaeca841907cfe1950d0bfcb6c33ef091a17fe54c69db167a74c22a`
and `babc9813c543440d50399bb5181cbdcfab133061f9f6cd46aa55be5eadaf0b8f`.
Remote syntax checks and 108 memory-only guards passed. Schema24 remains the
default; schema27 requires an explicit argument, complete migration history,
the immutable schema23/24/25/26 catalogs and exact 35-table current catalog.

[Source29 catalog generation](m3e-schema27-catalog-generated.json) passed from
the frozen bootstrap production closure, manifest
`ee7b432ae2b5e42dd6da7a80eef0d96c8f6b79355781261e2be67ade76c016c7`.
The generated PostgreSQL17 catalog SHA-256 is
`1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d`.
Both disposable pairs were removed, the original HBA restored exactly and the
preexisting cluster catalog preserved. This bootstrap does not establish final
product source equivalence, full tests or a deployable build.

## Retained first targeted failure

[Source30](m3e-source30-extras-targeted-failed.json), manifest
`8586ac30215cd293e4a13e107b9bcf06b1592a0d3ac353084fd311129f869a1f`,
ran the Extra/SpecialFeatures/LocalTrailers/Schema26/Theme test selection in
database, backuppg, library and server. There were 94 top-level passes,
two top-level failures and zero skips. Backuppg and server passed; database
and library each contained one failing top-level case:

- The normal and recovery branches of the migration-preservation test used
  a whole-row JSON conversion unsupported for a sequence relation. The test
  now selects explicit sequence state, OID and definition without weakening
  the original identity/state preservation assertion.
- The real-source delivery test used a legacy fixture prober lacking the
  required probe version and file ctime. The test now supplies these facts
  from the opened descriptor and retains byte-for-byte delivery and cache
  checks. Production freshness validation is unchanged.

The unit is terminal with exit1. HBA restoration passed. The two owned empty
databases were retained and subsequently [disposed](m3e-source30-extras-disposal.json)
by the operator bound to their exact OIDs, empty objects, zero connections,
original receipt and frozen runner. Four small remote guards passed first.
The disposal report SHA-256 is
`50643e6a67f6f11c91b372e5c70b9a5408fcfb987a25416e4890521f7c2af715`.
Preexisting cluster state and all original failure files were preserved;
no force operation or backend termination was used. This does not change
the original failure into a pass.

## Corrected targeted run

[Source31 targeted verification](m3e-source31-extras-targeted.json) passed
105 top-level race tests with zero failures and zero skips. Its 3997-file
source manifest is
`0468c7e557c82249a866b81ae7b22924c2dc456bf5cd333af45b9a6d1ad7aa92`.
The run includes the two fixed fixtures and the additional explicit-Ids,
parent count, field-switch and concurrent-snapshot cases. Both disposable
pairs were removed, HBA restored exactly and the preexisting catalog preserved.
The report SHA-256 is
`a5182130cc2fff41540c139893cd42d082716ea5ad758883987b1c31202ec24a`.
This targeted pass alone does not authorize deployment.

## Superseded full run and final-source fixes

The [source31 full run](m3e-source31-extras-full-failed.json) recorded 1189
top-level passes and nine failures before the root operator stopped the exact
owned unit. One schema13-to-current metadata test still expected the old added
table set. The native recovery fixture cleanup also omitted the two new tables;
eight subsequent recovery cases correctly rejected the nonempty test database.
Both explicit table inventories are now corrected in the worktree. The stop
intent is retained under `specialfeatures27-transfer-01/source31-stop-intent.json`
on test-env. The report remains failed, SHA-256
`92177a7b3c3f1797ed24109aacd03c575caebac80faccd4d820df3e640853779`;
unit exit0 after the controlled stop does not make the test stream successful.
HBA restoration passed. Residual test objects were subsequently disposed through
the independent repair recorded below; do not restart the finished run.

Independent static review also found that the Trailer DTO overwrote native
Name and SortName overrides/locks with its automatic Movie-derived name.
The worktree now reads each field's control-key presence in the same authorized
snapshot and applies automatic naming only to an uncontrolled field. List,
direct and explicit-Ids paths share this logic. New real native HTTP edit and
forced-rescan cases preserve resource identity, source filename, control
layers and both users' state. These changes are covered by the new frozen
source32 targeted and full runs below; the earlier source31 targeted result
does not cover them.

The first exact source31 disposal attempt failed during its transaction's
comparison checks. A [separate read-only diagnosis](m3e-source31-disposal-sql-diagnosis.json)
reproduced `operator does not exist: text = jsonb`: the catalog query returns
text, whereas the comparison uses jsonb. An explicit jsonb cast passed the
same read-only checks. Complete before/after snapshots prove both databases
and the original receipt unchanged; no DROP or LOCK ran in that diagnosis,
and the failed disposal committed no DDL. A separately bound repair attempt
preserves the original disposer, failure evidence and guard scope.

The independent [source31 repair disposal](m3e-source31-extras-disposal.json)
passed after seven remote memory guards. Its report SHA-256 is
`e02d75c28dc823db5406bcd067659bffe502a19524c187f5fe1cfd010b6628e4`.
The exact server-test schema matched all 1109 schema27 catalog objects and
its dependency closure; the two public residual tables in each database were
empty and matched their compiled schema27 subset. Transactional rechecks
preceded removal of only those recorded objects. The unmodified runner then
confirmed genuinely empty databases and removed both exact database/role pairs.
All original failure files, the first disposal attempt, HBA and preexisting
cluster catalog were preserved. No backend termination or force deletion ran.

## Verified source32 regression and build

The verified product source is `source-attempt-32`, manifest
`a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65`,
with 4002 files and the corrected native-control/rescan tests. Its
[expanded targeted run](m3e-source32-extras-targeted.json) passed 137 top-level
race tests with zero failures/skips and complete owned cleanup. It includes
the original failing metadata/recovery paths, native name controls and the
real forced-rescan case. The report SHA-256 is
`fbebbcef409d9c06ba1d3b7874749621471b38555774504f9062222fa135d796`.
The [complete race suite and build](m3e-source32-extras-full.json) passed from
the same frozen source. The terminal run is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260911_163635_51b90bc25d4b`.
It recorded 1,873 top-level passes across all 24 packages, zero failures and
zero skips. All six cleanup checks are true and the unit exited with code 0. The exact
report SHA-256 is
`408c49ff73e66c505865494e8e2e843954fd682a4b09fb5287835c027718ee78`.
The produced `tmp/goby-linux-amd64` executable is 28,172,723 bytes with SHA-256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
This closes source32's complete regression/build gate and preserves all earlier
failed and stopped runs as separate evidence.

## Candidate upgrade

The candidate upgrade operator now supports explicit 26-to27 and 27-to27
inputs while retaining historical schema25/26 bindings. Its
[55 remote guards](m3e-schema27-candidate-tool-guards.json) passed using the
actual source20/schema25, source28/schema26 and source32/schema27 catalogs.
An earlier guard setup rejected source31's private 0600 manifest because the
existing candidate source contract requires ordinary 0644 source files. The
new source32 snapshot follows that contract inside a protected 0700 root;
the original source31 permissions and failed guard evidence were not rewritten.
No candidate migration or service action was performed by these guards.

The actual [schema26-to27 candidate upgrade](m3e-source32-candidate-upgrade.json)
passed with ready/complete status. Its original source32 process was PID746709/start
ticks6930051, executable SHA-256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
The retained upgrade directory is
`/opt/goby-test/exec-work-m3e/client-upgrade-aaff5430e8e7fc52b1850fabe2097d89`;
its completion receipt SHA-256 is
`66c2b1f13113c5969887e963641e7ba04743f33da609e748a648117bf2edf26b`.
The exact fixture report at
`/opt/goby-test/exec-work-m3e/client-fixture-report-794b5ae666c8d2b6a5999233.json`
has SHA-256
`e9d502f16722d73b754d810de5b8ff5d7f6ccba512bd3eece3b1a1fc375828d8`.
All preexisting rows, columns, relation OIDs, ACLs and sequence state in the
old 33 tables were preserved, along with credentials, media and recovery state.
The expected schema27 migration row is the only addition to an old table.
Both new tables are empty; the candidate contains 35 tables, 13 items and
three libraries. That upgrade preserved its runtime configuration hash and
did not extend media roots. Its original process and receipt remain unchanged
historical evidence after the later extension described below.

The [product checkpoint](source32-product-publication.json) is committed and
pushed to `origin/main` at `b9bb7b1cf11e07e011a6e1726ddb9d53ef8e1fe9`.
Its 56 changed internal files are separate from active tooling and evidence.
All 765 checked product-source manifest members matched the local worktree
before staging; no additional product test run was needed for publication.

The new five-hop Music upgrade chain retains the old four hops and appends
the actual schema26-to27 completion. Its SHA-256 is
`c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a`.
All [six live lineage guards](m3e-source32-music-lineage.json) passed without
HTTP or database writes; the old chain, fixture and source files were unchanged.
The [new input07 browser harness](m3e-source32-cross-user-input07-guards.json)
passed two syntax checks and 114 pure guards on test-env. These remain separate
harness and lineage evidence from the actual original-client run below.

## Original Movie dual-user flow

The [actual input07 run](m3e-source32-cross-user-original-movie.json) passed for
both ordinary users. Each completed Home-to-Movie-to-Home, exactly one genuine
PlaybackInfo200 with finished transfer, and a complete SpecialFeatures200 `[]`.
Both recorded zero page errors, own-user200/foreign-user403, unchanged four
item-UserData projections, preferences, Configuration and Policy, and UI
logout204 followed by exact-token401. Each WebSocket opened and closed once,
with zero active sockets and no cleanup failures. Each also retained one
blocked-resource console warning/error; this is not a claim of zero console
errors. The public report SHA-256 is
`9f50d54cb291f4dfc24009fdb0084e6af06a443b2a353c16f02b7f337f95029a`.

The [separate scoped database comparison](m3e-source32-cross-user-original-movie-comparison.json)
passed, SHA-256
`0a2a1e54a95a52e15e8f4a037914ac05dc760ca561d40d5b3d5f53bcea196898`.
The observer and comparator passed [six pure guards and three syntax checks](m3e-schema27-prepare-scope-guards.json)
on test-env before the actual observations.
Recorded play rows increased from 22 to 24 and selected A/B authentication rows
from 51 to 53. Only the two eligible old Prepared rows became Expired; the run
added two new Prepared rows and two new authentication rows, both revoked at
logout. Five UserData rows stayed unchanged; references and encoding jobs
remained zero. This is the declared preparation scope, not whole-database
preservation. Input05/input06, their before/after images and the 20/49 and 22/51
starting points are consumed historical evidence and must not be reused as
authority for another run. Future preparation requires a fresh scoped baseline.

## Exact media-root extension

The [first preflight failure](m3e-source32-extra-root-preflight-failed.json)
remains retained, SHA-256
`3037c6e5e28dcce5e2b0255e53ec0db6dfbaac062f50978e879658bbc5a24d5f`.
Tool01 queried `encoding_states`, but the actual table is `encoding_jobs`.
Its pure mock repeated the wrong name and failed to detect the defect. The
preflight stopped before creating its output directory; fixture state,
environment and service were unchanged. Tool01 is not rewritten or relabeled.

Tool02 corrects that one table name and adds a memory regression against the
actual 35-table catalog. Its script SHA-256 is
`7a9cda4a36ebbe9c2311031db1f6a57265eef8fc8807057a3d650ef6c16b2a46`.
[Two syntax checks, 11 pure guards and preflight](m3e-schema27-extra-root-tool02-guards.json), followed by the actual
[extension](m3e-source32-extra-root-extension.json) passed. The public extension
report SHA-256 is
`09bf775ff6582b0a3dbc6c52026b4da7f0ba59066884c08a4c1cea4d3ba1391e`.
Only `/opt/goby-fixtures/client-special-features-m3e-v1/Movies` was appended to
`GOBY_MEDIA_ROOTS`; the scope was not broadened to `/opt/goby-fixtures`.
All 35 table rows, sequences, credentials, recovery state and media were
preserved. The operation performed zero HTTP calls, library creation or scans.
At extension completion the candidate had 13 items, three libraries and 35
tables, with both Extra tables empty. The later setup01 creation changes those
counts only as recorded at the top of this document.

The extension started PID748513/start ticks6996875 with the same source32
binary. Its historical completion-state SHA-256 is
`6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe`;
runtime-configuration SHA-256 is
`d8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df`.
The completion receipt is
`/opt/goby-test/exec-work-m3e/client-special-features-root-extension-v1/completed.json`,
SHA-256 `9d5404b4a6c1a9c5be404f453ee93cbdfa8e036399624b8897e940b7fccebcaf`.
The preceding upgrade's `new_process` remains PID746709 in its immutable receipt.

## Outstanding positive-fixture and primary gates

The independent nonempty-profile validator passed [two remote syntax checks
and six pure guard groups](m3e-schema27-positive-profile-guards.json). Its full
35-table membership and column sets come from the generated schema27 catalog.
It retains the existing empty-profile validator and separately constrains the
owned new library, items, resources, sessions, audit records and sequence
increments. These synthetic guard results do not establish a real indexed
candidate fixture. The later single-create/scan attempt and its checkpoint
failure are recorded above.
An [independent read-only baseline check](m3e-schema27-positive-profile-baseline.json)
also passed against the then-current candidate: 35 tables, 13 items, three
libraries and two empty Extra tables. It checked the full existing structure
and preserved the current state/process, with zero HTTP, database mutations
or service actions. It is not a substitute for the setup operation's fresh
before snapshot or a positive-fixture result.

The [main schema27 tools](verification-m3e-main-schema27-tools.md) passed their
remote syntax checks, 24 pure guards and helper build. Their first compilation
failure is retained separately. Actual primary deployment remains dependent
on the outstanding positive-fixture/client evidence and a revision of its
consumer for the new continuation chain; tool02 has not been deployed.

Next, independently continue the already-created library under the bound
continuation/inspection scopes above, then run the positive original-client
flow. The original Movie's empty-array success and
the permission extension do not establish indexed extra delivery. Completed
media/reference phases must not be replayed. Positive extra media delivery,
positive two-user original-client acceptance and primary schema27 deployment
remain unverified; the primary is still source28/schema26.
Library restriction/restore and broader M3/M4/M5/M6 work also remain open.
The initial three-category Movie fixture
does not establish remaining layouts, positive Series extras or Cinema Intros.
