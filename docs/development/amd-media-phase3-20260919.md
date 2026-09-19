# Library and client/management compatibility execution record

Status: **complete within the recorded functional, profile and client boundaries**.

This is phase 3 of the approved
[three-phase plan](../planning/amd-media-compatibility-plan-20260919.md).
Phases 1 and 2 retain their separate verified and closed records. Phase 2's
final selected source is `8f1de3f4509b36b06a5e1a331166d1e1c6a64fd7`
(`source19`); its ordinary and embedded builds used source18, whose production
code is unchanged in source19. These results do not verify phase 3 changes.

The final selected phase 3 source is `source16`, tree
`f9b57d3d99d5b9c22fea0a4b511298a914804e06`: 1,625 selected files, 232 changed
from the phase 2 baseline, and the real PostgreSQL 17 schema-41 catalog.
Functional verification is complete within the scopes below. The final ordinary
server inventory has 812 unique passing parent cases and one explicit AMD skip,
with zero remaining cases; this is composed coverage, not one full passing run.
Source16/ui09 passed its build, ten mocked checks and all twelve real database/UI
stages. Both application builds passed on actual source14 and remain applicable
through explicitly unchanged production inputs. Maximum-image backup and
recovery gates passed at the recorded 1 GiB PostgreSQL profile; the failed
512 MiB run is retained. All original failures remain visible. Forty-two workers
are terminal; owned PostgreSQL, listener, socket and pidfile are closed; source16
and original receipts are unchanged. The 172-file hashed evidence handoff is
complete. Phase 3 and the approved three-phase increment are complete within
these boundaries. Deployment, a 512 MiB pass and full-project/full-Emby parity
are not claimed.
The [machine-readable ledger](amd-media-phase3-results-20260919.json) keeps each
source, scope and acceptance boundary separate.

## Implementation inventory

| Area | Required behavior | Current state |
| --- | --- | --- |
| Library editing | Rename, root additions/replacements/removals, supported options, approved-directory browsing and validation, stable identities and explicit rescan behavior | Verified in selected library/server scopes and the real administrator journey |
| Preferences and user state | Persistent user Configuration and client-scoped DisplayPreferences, real consumers, HideFromResume, supported ratings/UserData and legacy PlayingItems | Application-key regression repaired; selected identity/media/server and real UI scopes verified |
| Images and avatars | Upload/delete/reorder, independent entity artwork/state, current authority and separate public-login avatar visibility | Selected artwork/server, real UI and exact maximum-image backup scopes verified; PostgreSQL profile is 1 GiB |
| Music | Artist/album-artist/music-genre APIs, local tags, composer relationships, track/disc numbers and explicit multi-artist handling | Activity repair and selected music/server/UI scopes verified; provider-online acceptance remains deferred |
| Navigation and events | Ancestors/counts/additional parts, supported query contracts, session subscriptions, refresh events and global NextUp behavior | Selected protocol/query/WebSocket coverage verified; full original Emby Web parity remains outside this acceptance |
| Management | Configuration consumers, validation/default/reset/reload behavior, native task compatibility and supported system-event triggers | Selected settings/task/server and real UI/restart scopes verified |
| Administrator UI | Integrated workflows, useful error/loading states, permissions, draft preservation and accessible controls using the existing design | Source16/ui09 build, 10 mocked checks, 12/12 real stages and final desktop/mobile visual review passed |
| Schema integration | Ordered immutable migration additions, previous-row preservation and current native backup/recovery compatibility | Schema40/41 retained; migration/activity, maximum-image backup and recovery scopes verified; owned resources closed |

The earlier feature-wave claim that `POST /Users/{Id}/Configuration` already
persisted was corrected before implementation. The existing `UserSettings`
API remains a distinct contract. No implementation or verification claim is
made merely by accepting or returning a configuration field.

Integration also aligns native backup limits with the supported 20 MiB managed
image upload size. PostgreSQL's serialized image row can exceed the previous
32 MiB COPY/JSON row limit. Both the decoder and row-fingerprint path now share
a 64 MiB serialized-row ceiling; archive-wide limits and lower caller-supplied
row limits remain enforced. A real PostgreSQL dump/validate/restore regression
with an exact maximum-size valid PNG passed in source12 at the 1 GiB profile.
This ceiling is not an RSS limit: row assembly, protocol buffers and JSON
processing can retain additional memory.
The first maximum-image backup attempt and the subsequent query/environment
repairs are recorded separately below; the fixture and row ceiling were not
reduced to obtain a smaller test.

## Verification and closeout gate

Ordinary formatting, compilation, tests, runtime and browser verification run
only on `test-env`. The approved CT 104 exception remains limited to AMD checks.
At the verification checkpoint, Git integration was not yet authorized.
The user separately authorized the September 20 merge/push recorded below.
No local product verification or production deployment was authorized or
performed as part of this increment.
Parallel source changes are integrated into an immutable selected snapshot
before coordinated remote verification starts.

The required checks include migrations and restart persistence, current
permissions and revocation, concurrent changes, real consumers of preferences
and management options, administrator/client workflows, task/event lifecycles,
cross-phase playback regressions, both application builds, and owned-resource
closure. Failures and intentional scope limits must remain visible. A completed
unit or API response is not a substitute for the corresponding user journey.

Phase 2 evidence is retained. Its stopped owned PostgreSQL cluster was streamed
into a private local cold archive, checked for matching bytes, SHA-256 and stable
source metadata, then removed from its exact remote task-owned directory.
The archive is 96,459,344 bytes with SHA-256
`ce66e2a6ef3e7ffdb63e4c50d7c6d233130f891a3665c03b41852007098182a0`.
The original phase 2 closure receipt and selected source remain unchanged.

Phase 3 has an independent PostgreSQL 17.11 cluster under `postgres-phase3`,
owned unit `goby-amd-media-phase3-postgres.service`, and six disposable
databases with separate ordinary owner roles, including the later independent
catalog41 database. Archive and recovery pairs are
separate from the main integration database. The bootstrap administrator URL
is retained privately and is not supplied to ordinary verification workers.
Preparation retained fsync and synchronous commit and used the existing
initial 512 MiB PostgreSQL memory limit. The later OOM recovery section records
the current 1 GiB limit. Its non-secret preparation receipt has SHA-256
`e644480ccfaa6e500833be1b722766bfa84911bb0a54cbbb9cfcd9eb0af8eac3`.
These are historical environment-preparation facts, not product verification results.

The user approved expanding VM 101's `scsi0` to an absolute 154 GiB and growing
the root partition/filesystem online. This maintenance completed without a
reboot or service stop. Readback recorded a 165,356,240,896-byte disk,
165,222,006,272-byte root partition and 165,222,002,688-byte ext4 allocation;
available filesystem space was 33,660,755,968 bytes at closeout. Disk/partition/
filesystem UUIDs, the root start sector, both boot partitions, VM process and
guest boot identity were unchanged. All 21 originally running services retained
their process/start identities, including the phase 3 PostgreSQL service.
The guest result SHA-256 is
`f379a851c7966007a9f331259b4f925616de8461d55330b8859358423da6da09`;
the host result SHA-256 is
`972a6c66ed6553de7ef1e2d37e06418160f861f82da64711dd62c368e333320f`.
Raw VM configuration and partition backups remain private. The phase 3 database
was included in final owned-resource closeout; its stopped data remains retained.

## First selected source and formatting

The first implementation snapshot, `source01`, has selected tree
`740f999dcb2bb51441aceb67427bbde769ab2e0c`: 1,427 selected files, with 201 changed
files from the final phase 2 baseline. Remote gofmt processed 1,239 Go files and
changed 116. Its patch was applied back to the working tree. The resulting
`source02` tree is `6439683b48ecf942e730f088bba19c20c5681721`; all 1,427 remote
files match, and its only differences from source01 are the recorded formatter
changes. The source copy contains the selected package fixtures and explicit
helpers; the full original phase 2 copy remains retained.

The initial SSH compilation attempt failed during agent signing before remote
command execution; that transport failure is not a product test result. The
subsequent `compile01` completed on source02 with `-race -run ^$`: 29 package
entries, zero test events selected and an unchanged selected source. This is a
compilation result, not a functional test pass.

## Selected sources and catalog preservation

| Source | Git tree | Selected files | Change boundary |
| --- | --- | --- | --- |
| source01 | `740f999dcb2bb51441aceb67427bbde769ab2e0c` | 1,427 | Initial phase 3 implementation snapshot |
| source02 | `6439683b48ecf942e730f088bba19c20c5681721` | 1,427 | Remote formatting changed 116 Go files; compilation and schema40 generation source |
| source03 | `f7579ec5c7adeeb0e88bdd0f3bb4a423374c768e` | 1,428 | Adds the real schema40 baseline; first functional/UI scopes |
| source04 | `22161a1e4652dce025ba9794a81de8fb449dfa57` | 1,433 | Production repairs, exact historical-oracle updates, UI diagnostics/ID repairs and migration 0041 |
| source05 | `b2c8a06edbde17072dd28d39b29c7b11c098a113` | 1,433 | Five-file remote formatting result; schema41 generation source |
| source06 | `8ed99151d2999f670794ed2648b72ef13066419b` | 1,434 | Adds the real schema41 baseline; earlier repair/UI verification source |
| source07 | `b8d6901e0662d6420da6b922195c29b4b46819cf` | 1,434 | Local preparation only; first real UI login-locator repair; not run |
| source08 | `0ffe928773db2d790d36f04bbdec2b101c292338` | 1,434 | Local preparation only; server repairs before capture inventory repair; not run |
| source09 | `0ffe928773db2d790d36f04bbdec2b101c292338` | 1,624 | Adds 190 unchanged scheduled-task/subtitle captures to the selected manifest |
| source10 | `f4e59dc9992bf0006d372283740936c8b668506c` | 1,624 | Four-file remote formatting result; server repair, UI, backup01 and cross-media source |
| source11 | `42402bff653b69fe6fba5d9b1ee817c481271272` | 1,625 | Row-fingerprint production/test/README changes and UI recorder repair |
| source12 | `acd9a2288eedf7e2b8edbf4b9aa8bb567affc849` | 1,625 | One-file remote formatting result; actual backup02/recovery source |
| source13 | `db5fd608093a8e0cdaff8ce40df317e92c2eb719` | 1,625 | Historical-theme oracle and FilePayload repairs; actual backup/server repair source |
| source14 | `5f68975b3ae9bda8786a362a51f2c33c437eeb0a` | 1,625 | Live spec only; actual final application builds and composed server coverage |
| source15 | `9fb1b79e26f4a2eeb4c8bcebd6289876e7e76fbd` | 1,625 | Live spec locators/waits only; real06 passed 12/12 stages |
| source16 | `f9b57d3d99d5b9c22fea0a4b511298a914804e06` | 1,625 | Screenshot readiness in live spec only; final ui09 build/mock/real and visual review passed |

Source08 and source09 share a Git tree but have different selected manifests.
The 190 added files are unchanged `scheduled-tasks-fresh-m5f` and `subtitle-m3c`
captures omitted from the earlier overlay, not newly fabricated reference data.
Neither local-only source07 nor source08 is represented as an executed source.
After source13, only the live browser specification changes. Go, modules,
runtime helpers and production frontend inputs remain byte-identical; the bundle
remains `72dd3d2a112ab31325a66993a671825e92d3e2d466534e359ea9d5f7153f4b30`.
Each result retains its actual execution tree. Input equivalence does not turn
a source13 test run or source14 build into a newly executed source16 run.

Schema40 was exported from actual PostgreSQL 17 using source02 and remains
retained. The music metadata activity repair appends
[0041_music_activity.sql](../../internal/database/migrations/0041_music_activity.sql)
to permit the selected Album/Artists/AlbumArtists changed-field names; it does
not rewrite frozen migrations 0036 through 0040 or expose metadata values in
activity. Schema41 was exported from a new empty catalog41 database using
source05. Its catalog contains 48 tables, seven sequences and 74 foreign keys.
Neither catalog-generation result alone establishes backup or restore acceptance.

| Retained catalog | File SHA-256 | Schema SHA-256 |
| --- | --- | --- |
| [schema40](../../internal/backuppg/catalogs/schema-40-postgresql-17.json) | `af5c0dc1e7a410a940e66b844e0573701a78b314f71cacb9c9b8298b4d91a388` | `6ca7746d8fc4eaeb0e7c02ebae13293b2060ae36fbca441fa969cccca9cb546c` |
| [schema41](../../internal/backuppg/catalogs/schema-41-postgresql-17.json) | `4f7edd90ec7c6ab5b9378444e82e7e858a8060b295940f18d4ed5fd6709c2632` | `a3906c2b4f06e0d9e8c028a2437368c5fcaf55a2d354bad6cf2d82c3550c875d` |

## Verification results retained so far

Go counts below are runner events and include both parent and subtest events.
They are not unique test counts. Repair runs overlap the originals and must
not be added to form a new total. UI counts are the selected browser checks.

| Run | Source | Recorded result | Interpretation |
| --- | --- | --- | --- |
| compile01 | source02 | 29 package entries; zero tests selected; passed | Compilation only |
| core01 | source03 | 1,571 pass / 12 fail / 0 skip events | Identity, artwork, settings and tasks passed; database had 11 failure events and activity one |
| library01 | source03 | 2,443 pass / 9 fail / 1 skip events | Includes two production defects and outdated fixtures/oracles; original result retained |
| schemaRepair01 | source06 | 688 pass / 0 fail / 0 skip events | Complete database and activity packages in this scope passed |
| libraryRepair01 | source06 | 51 pass / 0 fail / 0 skip events | Selected failed and related music/navigation cases passed; not a full library rerun |
| uiBuild01 | source03 preparation | Failed before Node | Helper raw-manifest hash preflight rejected the run; no frontend build result |
| uiBuild02 | source03 | Passed | Bundle SHA-256 `d470a960b3d28f7c007ea692cc5c5b07a8d4bf06d08415c52567e24b19ec44cb` |
| uiMock01 | source03 | 7 pass / 3 timeout / 0 skip | Failed selected mocked UI scope; original diagnostics retained |
| uiBuild03 | source06 | Passed | Current selected UI build; final ordinary/embedded application builds remain pending |
| uiMock02 | source06 | 10 pass / 0 fail / 0 skip | Selected mocked UI checks passed |
| uiReal01 | source06 / ui03 | Failed before login; 0 of 12 database stages | Exact Username label lookup did not match the required-field label; harness failure, not a product result |
| server01 | source06 | 2,812 pass / 22 fail / 1 skip events; closed | Thirteen top-level failures; original receipt retained |
| serverRepair01 | source10 | 84 pass / 0 fail / 0 skip events | Selected server repairs; overlaps server01, not a full rerun |
| uiBuild04 / uiMock03 | source10 / ui04 | UI build and 10 checks passed | Bundle `72dd3d2a112ab31325a66993a671825e92d3e2d466534e359ea9d5f7153f4b30` |
| uiReal02 | source10 / ui04 | Failed after 4 of 12 database stages | First entity upload appeared saved; CDP body eviction prevented the recorder from finishing artwork acceptance |
| backup01 | source10 | 460 pass / 39 fail / 0 skip events; closed | First maximum-image fingerprint caused PostgreSQL OOM; later connection/schema failures were consequential |
| crossMedia01 | source10 | 3,690 pass / 0 fail / 13 skip events | All eight selected package scopes passed; skipped profiles are not accepted |
| backup02 | source12 | 499 pass / 4 fail / 0 skip events | Exact 20 MiB dump/validate/restore and canonical fingerprint passed; historical-theme UserData expectations failed |
| backupRepair01 | source13 | 15 pass / 0 fail / 0 skip events | All related theme cases passed; overlaps backup02 |
| recovery01 | source12 | 203 pass / 1 fail / 0 skip aggregate events | Complete recovery-manager package passed; recoverydb port preflight failed before opening its database |
| recoverydb02 | source12 | 181 pass / 0 fail / 0 skip events | Complete package with explicit recovery port 54919 |
| serverRepair02 | source13 | 84 pass / 0 fail / 0 skip events | Later recorded server repair run |
| serverCoverage01 | source14 | 812 unique parent passes + 1 allowed AMD skip; remaining 0 | Reconciled final 813-case inventory; no single-full-pass claim |
| uiReal03 | source12 / ui05 | 4/12 stages | CDP body eviction persisted |
| uiReal04 | source13 / ui06 | 5/12 stages | Artwork/avatars passed; Album tag locator matched section and input |
| uiReal05 | source14 / ui07 | 5/12 stages | Music save/image succeeded; alert Close and footer Close were ambiguous |
| uiReal06 | source15 / ui08 | 12/12 stages passed | One restart, no page/foreign-request errors, natural session/resource cleanup |
| uiBuild09 / uiMock08 / uiReal07 | source16 / ui09 | Build, 10/0/0 mocked checks and 12/12 real stages passed | Final screenshot readiness and actual desktop/mobile review passed |
| finalBuild01 | source14 | Ordinary and embedded CGO-disabled builds passed | Reused for source16 only through unchanged production inputs |

The source03 core failures concerned old exact projections, table inventories
and activity-action expectations. Repairs retain original columns and assert
the new defaults explicitly; the original failing receipt is not replaced.
The library failures exposed production defects in AdditionalParts raw-SQL
escaping and the music metadata activity field mapping/allowlist. Source04
repairs both, including the forward schema41 constraint migration. Other
library failures concerned the expanded metadata projection and current-probe
fixtures. The one original skip is
`TestRootBindingFullScanMountNamespaceHelper`; no zero-skip claim is made for
library01.

The first UI helper failed a raw manifest hash comparison before Node executed.
The helper now copies manifest bytes without parse/re-serialization; its
recorded SHA-256 is
`09fe70b5a31a02035146a3a825569fbfae760bfe76a1dd3d86cff1e9466247fb`.
The three source03 mock timeouts were in permission/library-dialog/Library-name
locator chains. The production LibraryEditor was not changed for that locator
repair; subsequent checks add ARIA diagnostics and textbox-role selection.
Separate duplicate IDs in artwork/preferences dialogs were repaired. Source06's
ten passing mocked checks do not replace the real browser journey.

The source06/ui03 real attempt failed before authentication: the live spec used
an exact Username `getByLabel` locator against a required-field label. No database
journey stage ran (0 of 12), so it supplies no product workflow acceptance or
product failure. The application is PID0; the source is unchanged; private
context/schema/media are removed; one fallback revocation completed; HTTP and
worker resources are closed. This is closure of that UI attempt, not final
phase PostgreSQL/worker closeout. The following spec repair changed the locator
and shortened per-action timeouts while retaining the 420-second overall bound.
The later uiReal02 attempt completed authentication and three additional
database stages before its separate response-capture failure.

## Server repair and second real UI attempt

Server01 closed on unchanged source06 with 2,812 pass, 22 fail and one skip
event. Its thirteen top-level failures include a real application-key regression:
catalog requests with a target UserId inherited personal preferences. The repair
uses neutral preferences for application-key Views, Latest and Similar while
retaining target ACL/UserData and explicit filter behavior. DisplayPreferences
defaults already skip application keys; this does not prevent authorized UserDto
reads. Other failures concerned full music metadata shapes and editable Audio
numbering, artwork/avatar Header fixtures, obsolete unimplemented-image and
Live TV root-alias expectations, retaining the old ended hardware-recovery
window, and rebinding notifiers after a fixture replaced its Store. Ten failure
events came from omitted scheduled-task reference captures in the selected
overlay. The original server skip is
`TestHTTPHardwareAV1PaddingFallsBackToExactSoftwareOutput`.

Source10 serverRepair01 and source13 serverRepair02 each passed 84 selected
events without failures or skips. The original complete run and targeted
results remain separate within the composed coverage below. The server01 receipt SHA-256 is
`6b18a961408d026203b91323d83c7289b6bb1bb01b85e55699f1cc331b09b6fa`.

UiReal02 completed authentication, library conflict, library editing and
preferences stages. On the first entity upload, the UI displayed a saved 16x16
image, but Chromium CDP evicted the response body before the recorder retrieved
it. Eight of twelve stages, including artwork, remain unaccepted. The source
was unchanged and the attempt's resources closed. The next spec captures the
original response bytes immediately and asserted no navigation, but real03 on
source12 still failed at 4/12. In Chromium 153.0.8010.12, the disk-backed File's
unknown size was accounted as approximately 4 GiB of Inspector post data,
exceeding a single-resource cache even though the PNG itself was 86 bytes.
Source13 uses a memory-backed FilePayload containing those same validated bytes.
The product and HTTP operation are unchanged; there is no mutation resend or
GET fallback. File stat/hash, real response JSON, UI, database and no-navigation
assertions remain required.

Real04 then passed artwork/avatars but stopped at an Album tag locator matching
both a section and its input. Real05 saved the music metadata/image but found
both success-alert and footer Close buttons. Role-scoped textbox/rowheader/footer
actions and a schedule-loading wait resolved these harness ambiguities. Real06
passed all twelve stages. Source16 changes only screenshot readiness: the earlier
mobile capture caught menu fade and the desktop capture was not positioned on
the music fields. Latest real07 passed all twelve stages again; the coordinator
viewed both final PNGs. The desktop shows Album tag and both artists clearly,
and the 390-pixel mobile artist list has no menu obstruction, clipping or overlap.

## Maximum-image backup OOM and owned PostgreSQL recovery

Source10 backup01 closed with 460 pass, 39 fail and zero skip events. The first
exact 20 MiB valid PNG fingerprint exhausted the owned PostgreSQL 512 MiB
cgroup; MemoryPeak was 537,595,904 bytes. Later connection/schema errors followed
that termination and are not independent evidence of 39 product defects.
The preserved inspection receipt SHA-256 is
`9f042ca019adb28de13748740d21f35150f4bde90f5a61d776496970361ec809`.
Raw journal records containing SQL remain private and are not copied here.

The fingerprint query redundantly serialized a row with `to_jsonb` twice.
Source11/12 changes it to one per-row evaluation through LATERAL with OFFSET 0.
JSON bytes, row ordering, hash meaning, the 64 MiB serialized-row ceiling and
the exact 20 MiB fixture are unchanged. Byte-exact and hash-oracle test source
was added. Source12 backup02 passed the exact 20 MiB dump/validate/restore and
canonical fingerprint witness at 1 GiB; PostgreSQL MemoryPeak was 756,084,736
bytes (about 721 MiB). Its four failure events were historical schema26 theme
UserData whole-row expectations after the new columns. Source13 compares all
original schema26 columns explicitly and the new defaults separately; owner,
item, resource and parent comparisons remain full-row. The related 15-event
backupRepair01 passed. The 499/4 aggregate remains failed in history; the repair
does not become an additional 15 unique tests or erase the original receipt.

The same owned cluster recovered normally through WAL at a 1 GiB memory limit,
Swap 0 and CPU quota 150%. Its six database OIDs/owners and twenty other protected
process identities were preserved; test orphans remain for owned cleanup.
The recovered PostgreSQL identity is PID 3136363, start ticks 54971786. The
recovery receipt SHA-256 is
`b9e457593cfa81afd7b2c2402abc0f84c179664b07d186c3a0a6da67317fc656`.
Environment recovery is distinct from product recovery acceptance. The recorded
1 GiB backup pass does not erase the failed 512 MiB run or accept that budget.

## Accepted package and composed server evidence

The original core01 aggregate remains failed (1,571/12/0). Its complete passing
package subset is identity 555, artwork 64, settings 121 and tasks 158 events:
898/0/0 in that exact subset. Counts were selected from the original Go JSON,
SHA-256 `0a5d545bae96516165afe32049f5ef75a63cb2ca20dde6ce61d1056e1871b09e`.
Database/activity acceptance uses the separate 688/0/0 source06 repair scope.

Recovery01 also retains its failed aggregate (203/1/0). The complete
`internal/recovery` package passed 73/0/0 events in the original Go JSON,
SHA-256 `59ecf5de24e218008826dd2b0ac6c7abc6e92b2be00ce19838fd8782851d3f69`.
The recoverydb failure was an omitted GOBY_TEST_RECOVERY_PORT preflight before
database opening; the full recoverydb02 package passed 181/0/0 after explicitly
setting port 54919. No aggregate failure is rewritten as a pass.

ServerCoverage01 reconciles the final compiled ordinary inventory of 813 parent
cases to 812 unique passes and one allowed AMD skip, with remaining 0. Its
coverage report SHA-256 is
`0bc3ce9b24224c43d9e88e4d381ac66ead2bd00452e41c92d2a9d4eb5fa0f776`.
The helper executes no test case and claims no single full pass. It retains the
actual source13 pass evidence and allows ordinary-input equivalence only for
`web/admin/e2e/*.spec.ts` changes. Go/module/runtime/reference/production-frontend
input changes would invalidate that equivalence. Original server01's 22 failure
events and its explicit AMD skip remain preserved.

## Final administrator journey and application builds

Source16/ui09 real07 completed authentication, library conflict, library edits,
preferences, artwork/avatars, music, schedules, restart, persistence, permission
revocation, permission denial and cleanup: 12/12 database stages. It recorded
zero page errors, zero foreign requests, one server restart, zero active sessions
before fallback, zero fallback revocations and no forced cleanup. Its owned
context, schema, media, sessions, listener and workers closed naturally. It did
not exercise media playback or online providers; those are not inferred from an
administrator UI result. Final artifacts are under
`phase3-ui09/artifacts/phase3-browser-913096410` in the retained evidence root.
Driver SHA-256 is
`208840119be9f2bf0ab1ec784701db01bba84b9e896be441f0627f518db378c9`;
browser SHA-256 is
`ffac7aaf4de17e45b702304f4a3ceca4905d080ce2903fac1ad5eb13be989e11`.

FinalBuild01 actually ran on source14 with CGO_ENABLED=0:

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| Ordinary application | 37,642,055 | `f0520129d6d55997efdb158884a851da1ce1ff0f3b7f94c04a2a94702374fa0b` |
| Embedded application | 38,918,723 | `2d2668a1606a867dbc1295836e580fdced4a175d4fa6b728b8c22fd9ea9d9e30` |

The unchanged production Go/module/runtime inputs and identical frontend bundle
make these artifacts applicable to source16. They are not described as rebuilt
on source16. Later build09 is the final selected frontend build, not a third
application-build receipt.

## Cross-phase scope and closeout

Source10 crossMedia01 passed the selected config, media, playback, transcode,
dynamicsource, subtitle, timeshift and events package scopes: 3,690 pass, zero
fail and thirteen skip events. The skip details remain receipt-bound; no
zero-skip, skipped-profile or new GPU acceptance is claimed. Earlier phase 1/2
records retain their independent source/profile boundaries.

Functional gates are complete within these recorded profiles and evidence
boundaries. Closeout preflight passed for all 1,625 source16 entries, 42 terminal
workers, twenty unchanged protected process identities, zero private contexts,
zero transient environment configurations and the six database identities.
Its SHA-256 is
`0d11a015b29e7fc19b71a500114f638c74c34c282c352fce8f5c1e779f140e53`.
That preflight preceded the actual PostgreSQL stop. Final closure is now
accepted: `closed=true`, all 42 workers terminal with PID0, PostgreSQL
inactive/dead with Result=success and ExecMainStatus=0, the old postmaster absent,
and no port 54919 listener or owned socket/pidfile. PGDATA and the six database
directories remain retained. Twenty protected service identities are unchanged;
all 1,625 selected source16 files and every original pass/fail receipt remain
unchanged. Private contexts and transient environment configurations are absent.

The final closure receipt is 126,583 bytes, with matching local/remote SHA-256
`c81400f7dea32562549e293515ec8e8f78c4329cf76945064b9874b5af1dbb59`.
The intent SHA-256 is
`d0ec692dd47f8278aff7fa0735884fed21b921a9e046da07e103b8e72a0a085f`.
The local handoff contains 172 files: 143 required and 29 supplemental, each
checked by size and SHA-256. Its ledger SHA-256 is
`927216c842aa9d238eb3995fc8aa7736e47b52d7d1570cd80b1a4408b231bd99`;
the 388,464-byte archive SHA-256 is
`aefe171fd3185a98d67ec3b00cfff17fb50525543ce0b87a93f2e398c59990d1`.
No scope was rerun merely to generate another receipt. This closes the selected
increment, while retaining OCI, non-AMD GPU, provider-online, full original
Emby Web and broader capacity/platform boundaries. The verification closeout
did not itself include repository publication or production deployment; the
later authorized Git integration is recorded separately below.

The API inventory, owned configuration/admin guides, execution plan, status,
progress and handoff are reconciled with final source16 and the closed evidence.
The supported scope and deferred profiles remain explicit; no historical
phase 1/2 result or original failed receipt was rewritten.

## Repository integration on September 20, 2026

After the verification and resource closeout above, the user explicitly
authorized merging the selected increment into main and pushing it. Product
commit `80198b6aa8a163b696ceaff64da831847d82e496`, based on
`b15de9a1dc7205aebd8ff0eea53326618233b983`, was fast-forward merged from the
development branch `codex/amd-media-compatibility` into `main` and pushed to
`origin/main`. The coordinator read back that product commit from the remote
main ref and confirmed that the complete product-commit Git tree is exactly
`f9b57d3d99d5b9c22fea0a4b511298a914804e06`, matching verified source16. All 1,625
selected manifest entries also match. The current branch is main.

This follow-up documentation commit records the integration and recovery context;
its own future SHA is not self-referenced. Runtime acceptance remains bound to
the original executed sources and explicit input equivalence, including actual
source14 application builds. Integration did not rerun the product, deploy it,
restart retained databases or alter any original failure/closure evidence.
Unrelated pre-existing local changes remain outside the selected increment.
