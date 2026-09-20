# Compatibility long-tail phase 1 execution record

Status: **verified, resources closed, merged to main and pushed on September 21, 2026**.

The [delivery results](media-analysis-resilience-phase1-results-20260921.json)
record the composed source boundaries, original failures, three explicit skips
and actual resource closure. The final acceptance section below closes phase 1.
Earlier entries are chronological evidence of consumed attempts, not open work.

The [approved three-phase plan](../planning/media-analysis-resilience-plan-20260920.md)
starts from `2fd9182` on `codex/media-analysis-resilience`. The original checkout
and its unrelated changes are preserved. At the initial source freeze, no tests,
builds, runtime probes or remote environment operations have run for this increment. Formatting and
static source review are implementation work, not passing verification evidence.

## Concrete contract inventory

| Requirement | Implementation ownership and current state | Acceptance gate |
| --- | --- | --- |
| `Users/Query` | Source complete: current administrator/application authority, hidden/disabled filtering, literal normalized name boundary, ordering, filtered count and pagination; [contract](../api/user-query.md) | Identity and HTTP query truth tables, current-authority changes and actual administration/consumer journey |
| Standalone-special predicate | Source complete: real legacy placement facts, native editing and typed filtering before count/page; [evidence and boundary](tv-metadata-facts.md) | NFO, metadata override/lock/clear, positive/negative filters, ordinary episodes, permission/count/page checks and browser navigation |
| `Status` and `EndDate` | Source complete: explicit source facts, nullable native fields, canonical status/date, Fields/detail projection and audit names | Real source scan, native editing, omitted unknown facts, persistence and restoration |
| `Fields` casing and `ExcludeFields` | Source complete: bounded shared parsing, exclusions after capability/image processing, nested media-source facts and preserved identity; [contract](../api/catalog-projection.md) | Actual lowercase client requests; selection/exclusion conflicts; nested stream/path removal; unchanged membership; auth and bounds |
| `CanDelete` and `CanDownload` | Source complete: current actor and policy, bounded batches, retained source/dependency/binding state, no filesystem I/O; [contract](../api/item-capabilities.md) | Positive/negative operation eligibility, policy/ACL/revocation, collections, source conflicts and HTTP projection/exclusion |
| UI language transport | Source complete: one bounded `X-Emby-Language` query hint, shared compatibility parser, complete episode queue retained | Strict management routes, ordinary-client queue above 100 episodes, conflicting/malformed carriers and native boundaries |
| Sort removal configuration | Source complete: persisted word list, explicit-sort provenance, real catalog rebuild, bounded statements, final authority recheck and settings/native adapters | Unicode/default/explicit/manual/locked sorting, concurrency/CAS, source refresh, native browser and recovery |
| Library options | Source complete: `EnableEmbeddedArtwork` and the advertised Audio `Goby Embedded Artwork` fetcher select real embedded-cover extraction independently from directory sidecars | Native/compatibility writes and discovery agree; actual subsequent scans consume options; disabled/re-enabled and partial/reset behavior |
| Schema and durable state | Source complete: schema49 sorting/provenance and audit additions, raw metadata/sorting validation and real encrypted restore checks; actual schema49 catalog generated from the frozen source | Historical migration and catalog preservation, actual exported schema49 baseline, archive integrity, restored settings/facts and credential normalization |
| Integrated administration/client journey | New twenty-four-stage Go fixture, browser driver and execution guide integrated | `TestMediaAnalysisResiliencePhase1BrowserIntegration`, real HTTP/PG/media scan/native controls, restart and owned-resource closure |

The retained original-client request corpus establishes the field and UI-language
gaps. It does not make the new code tested or prove proprietary-server parity.
The current eight-key item sort, Search/Hints, Suggestions, missing/unaired facts,
music relationships, image aspect ratios and namespace aliases remain baseline.
No new route alias or global error rewrite was supported by the audited corpus.

`BasicSyncInfo` belongs to deferred offline synchronization. The requested
`PresentationUniqueKey` remains an explicitly unimplemented grouping contract:
historical first-party code uses primary video versions and series/season
presentation equivalence. Goby's Latest aggregation, playlist membership and
multipart media are different relations. This phase does not synthesize a
grouping field from those facts or claim complete upstream field parity.

## Verification preparation

The initial complete source freeze is `cdf2a8b7f2996b1750a276718e568953647a78b5`.
Its source archive is 140,922,880 bytes with SHA-256
`b95675c3bea4ed90009d176ad804ece187f386b678e911e68f0dc79cb22ca166`.
Remote admission identified Go 1.27.1, Node 24.20.0, npm 9.2.0 and PostgreSQL
17.11 on Linux/amd64. The owned verification root is
`/opt/goby-media-analysis-resilience-20260920-p1a`; its isolated PostgreSQL
runtime has six disposable databases. Three unrelated active services were
recorded for preservation. Build/test workers are bounded separately from the
owned PostgreSQL service; no local verification or shared-host restart occurred.

The remote catalog exporter compiled successfully from that source and applied
the exact embedded migration prefix to a fresh PostgreSQL 17 database. The
generated `schema-49-postgresql-17.json` is 880,331 bytes with SHA-256
`3d78ea7558a3323544858d16e00dce1abc03102c0d7183706053f1b97d28a633`.
The exporter process group is closed. These are compiler/catalog-production
results, not passing product, recovery or client suites. That initial catalog
was committed at `d49f6e224a22b43de006e1f80d116db54c4bdbcf` and used for the first
consolidated build and verification batch.

The first build passed for frontend assets, twelve test binaries and both
ordinary/embedded applications. The original `verification-01` batch retains
its failures. Observed completed passing groups include metadata, activity,
identity, database, backuppg and recoverydb; this is not a passing whole batch.
The 10,000-file real-media scan/cache regression passed in 402.77 seconds on its
admitted profile. It does not establish concurrent playback or phase 3 capacity.

Verification exposed a product regression: ordinary lowercase key derivation
changed the retained filename/embedded-title case of auxiliary media, causing
cached/forced rescans and owner-derived notifications to rewrite accepted state.
The repair gives auxiliary source generation, cache comparison and policy
rebuild the same case-preserving rule. Manual controls and inactive permanent
roles remain protected. Old failing assertions are retained. This unpublished
schema49 DDL repair changes the migration digest to
`fc32b3a69ab5d90dc9b6073714007d9de0376d9e5cac9cdf061469da740b03e3`.
The repaired source was frozen at `d4b44e8a74f4650cd6f94e555390c75b4e082f6f`.
Its exporter compiled remotely and generated the replacement catalog from a
seventh, fresh disposable database. The replacement is 880,671 bytes with
SHA-256 `672687e280b656a05e7a9cd920455641e3dcb376dc10bec3a6d34a619af36b84`.
The original remote database/catalog and all original results remain evidence.

Other source repairs correct the old settings migration/DTO fixture shapes and
give the standalone metadata-edit test a real catalog owner. Their original
protected-field comparisons are retained and extended for the new state.

The first consolidated batch is terminal and failed: **2,309 passing parents,
16 failed parents and three explicit skips**, with all twelve controller process
groups closed. The failures comprise the settings migration fixture, four
auxiliary library cases, the standalone write fixture, eight settings DTO/HTTP
parents, the auxiliary HTTP notification case, and the initial browser fixture.
The skips are the opt-in full mount helper and two AMD profiles; they are not
passing executions. Initial backup, recovery-store and full recovery groups
passed in their original source/catalog scope.

The original twenty-stage browser binary panicked in its baseline observer
before launching the browser. It borrowed an older playback helper that assumed
a configured HLS runtime; this phase deliberately has no transcoder. The new
observer records configured/present components and actual joined resources,
without creating lazy state or inventing zero counts for absent components.
This test-source repair does not alter the historical helper or product code.

The first six-suite mocked UI run closed its server, process group and temporary
profiles, with 54 passing cases and one failed management-save expectation that
omitted Sorting. The repaired expectation now also protects a nonempty retained
rule list. The two new Node source-test files passed all seven cases remotely.
The final affected backend, mock repair and expanded browser results remain
pending; these component results do not close phase 1.

The corrected catalog and expanded browser inputs were frozen at
`3251a354b62018661c752062282c8df104400c94`. Its archive SHA-256 is
`c62243dfb6d0ececbbf5dd9ba7c9ca023db60acc61f54259dadd312fc18dbebb`.
`product-build-02` passed the frontend, twelve test binaries and ordinary/embedded
applications, with all build process groups closed. `mocked-browser-02` passed
the repaired management-save case with the nonempty retained Sorting rules;
the earlier 54 unchanged passing cases retain their original source boundary.
The repaired case has no unexpected, flaky or skipped result, and its server,
process group, dependency link and temporary profiles are closed.

`verification-02` is terminal with **493 passing parents, three failed parents
and no skips**, across eight closed process groups. Settings (55), database
(65), affected server (74), backuppg (120) and recovery (33) passed on this
source. All original auxiliary and standalone library failures passed, but the
new sorting/cache witness failed: an ordinary owner movie was repeatedly counted
as updated after a sorting-rule change. The candidate scanner compared its raw
folded title with the already generated key. This requires a product repair and
renewed ordinary-scan coverage, including the actual 10,000-file case.

The scanner repair adds its comparison key to the existing stored-file read,
using the same MVCC snapshot as accepted metadata and settings without another
per-item SQL round trip. Generated keys use the configured rules; explicit
online or conservative historical keys retain their recorded provenance. New
source/probe/type/hash changes still require publication under the current owner
transaction. The strengthened cache witness separately checks the ordinary
owner, auxiliary snapshots, probe count and notification silence. A new
`TestCachedOrdinaryScanPreservesExplicitOnlineAndHistoricalSortProvenance`
protects repeated cached visits both with rules and after clearing them.
These repairs require new remote build and execution evidence; the schema49
migration and recovery catalog are unchanged.

The actual browser reached authentication, then timed out before submitting an
account change because two MUI switches were incorrectly selected as checkboxes.
Only those driver locators are corrected; the remaining checkbox selectors were
checked against their actual components. The failed run's observer, workers,
HTTP requests/listener, private context, media root and schema all closed. Its
two remaining sessions required recorded fallback revocation; that cleanup is
not a successful normal client logout journey.

The recovery-store fixture correctly rejected the already consumed initial
database pair because its public namespace was not empty. The old pair and
rejection remain evidence. `verification-03` passed all twelve recovery-store
parents on a fresh independently owned pair in 30.19 seconds; its process group
and worker closed. This is an environment repair, not a weakened emptiness guard.
No current source is accepted or published by these partial results.

The scanner and first locator repair were frozen at
`398756d07be4b6814724aa8acb20518ec033c8f8` (archive SHA-256
`69bf902eb4e60a578b13cab4fa435c060d31d57868f2e0c15e706251a78c9797`).
`product-build-03` stopped before compilation because its frontend reuse guard
mistook a generated contribution manifest for a source input. The original
failed guard remains retained. `product-build-04` compared all 96 tracked
frontend inputs against both frozen Git archives, verified 71 retained asset
files, and passed the library, server and browser test builds plus both
application builds. All five build process groups closed.

`verification-04` runs the complete library package, including the actual
10,000-file scan, plus affected HTTP tests and the full browser journey. Its
browser reached a successful account PUT and saved-state screenshot, then
failed because the successful form has both a dismissible alert and a footer
button named Close. The driver now selects the visible footer text in all five
affected account/metadata closures. The remaining control roles and dialog
scopes were compared with the actual components and existing browser tests.
This is a driver-only repair; complete browser acceptance is still pending.

The complete `verification-04` library scope passed **817 parents** with the
original opt-in mount-helper skip, in 756.99 seconds. Its actual 10,000-file
case passed in 441.06 seconds. The strengthened auxiliary/cache witness and the
new explicit online/historical provenance case passed. The affected HTTP scope
passed all 74 parents in 50.74 seconds. Both scopes and their controller process
groups closed. These are the current scanner repair results at `398756d`;
the failed browser remains a separate unclosed acceptance requirement.

The footer-locator driver repair was frozen at
`8d10acfbb2b95e2b464448eb48dd93d05165a960`. Its browser-only build passed with
the same verified frontend assets. `verification-05` completed five independent
stages, including native user saving, compatible user queries and native TV
metadata edits, then timed out while waiting for an actual scan. The driver
incorrectly compared the native job DTO with capitalized database states;
the native contract returns lowercase states. The shared browser wait now uses
the exact native terminal values. No product code or business assertion changes
in this driver repair. The failed run closed its processes and owned runtime
resources; complete 24-stage acceptance remains pending.

Private admission, setup, compilation and generation receipts are retained under
`D:/Code/goby/.git/media-analysis-resilience-20260920/phase1` and the owned remote
root. Credentials and DSNs remain only in the private runtime context.

The dedicated browser sources are
`internal/server/media_analysis_resilience_phase1_browser_integration_test.go`
and `scripts/test-env/media-analysis-resilience-phase1-browser.mjs`, with the
adjacent execution guide. Their current twenty-four-stage inventory covers native
account changes and queried selection, source-backed TV facts, sorting/import
consumers, field projection/exclusion, search navigation, a real runtime restart
and normal cleanup, including dynamic embedded-artwork selection, new audio
source arrival, disabled extraction, retained images, re-enabled extraction and
actual decoded image evidence. Four additional stages cover a real stale-CAS
conflict, a selective reset with an unsaved sorting draft, an actually committed
clear whose response is dropped, and explicit restoration after refresh. HTTP
write counts, revisions and database/catalog state are independently observed.
The initial `d49f6e2` binary contains twenty stages; the final expanded journey
requires a new test artifact and separate source-bound result.

After all phase product/test code is complete, freeze the source and use fresh
owned remote resources. Build frontend assets, ordinary/embedded applications
and affected test artifacts on `test-env`. Generate the schema49 recovery catalog
from the exact embedded migration prefix on fresh PostgreSQL 17, then bind the
final artifacts to that generated baseline. Do not reuse prior unit names,
consumed run IDs, private environment files or old acceptance as new evidence.

The consolidated batch covers affected metadata, activity, identity, settings,
database, library, server, backup/recovery and application boundaries, plus
selected native frontend checks and the actual browser journey. Preserve all
failures and skips; repair and repeat affected scopes. Record tool/client/source
identities, state/artifact evidence and closure of owned resources.

## Final phase 1 acceptance

The final ordinary scanner and application source is `398756d`; later source
changes affect only the browser driver and this execution record. The accepted
browser source is `2c11c6641869ea3cf112595a42dc31b1a46e7cad`, whose source archive
SHA-256 is `7e5b855e8d3174351053f5fd21ae90306838404df5bf401fca22676f93ac5472`.
`browser-build-06` passed and `verification-06` completed all 24 real browser
stages in 28.40 seconds, with independent PostgreSQL acknowledgements. Six
actual scans completed, one runtime restart preserved catalog/settings/users/
images, and eight actual image decodes agreed with independent bytes, hashes,
dimensions and pixel observations. Conflict, selective reset and committed
response-loss recovery were exercised through real mutations. Normal native
and compatible logout completed with zero fallback session revocations.

Both runtime generations joined successfully, and all observed active resources
returned to zero. Their three retained immutable image-cache entries total
1,536 bytes and are recorded separately. The fixture had no configured HLS
runtime; absent components are not represented by invented zero counters.
No arbitrary third-party or original-client parity is inferred.

The composed Go inventory has **2,328 unique passing parents**, **zero pending or
failed current parents**, and the three original explicit mount/AMD skips.
Retries are not added to this count. The complete repaired library scope owns
817 passes and the mount skip. Corrected settings/database/catalog/backup and
recovery scopes retain their exact sources; unrelated unchanged cases retain
the original full-run source. The delivery JSON maps every selected requirement
and binds the complete private ledger by hash. All original failures remain.

The six selected mocked UI suites compose **55 passing cases**: 54 unchanged
cases and the repaired save case. All 71 frontend artifact files are byte-
identical between the two full builds. Seven frontend source tests also passed
in the recorded bounded remote command; no standalone TAP file is invented.
The final ordinary and embedded application builds passed at `398756d`.

Final inspection and closure verified all **17 owned worker units** terminal,
their process groups gone, and the exact admitted PostgreSQL incarnation closed
cleanly. Independent readback found no process, listener, socket, lock or pidfile.
All nine database directories and the original evidence remain preserved; the
three unrelated service PIDs, start times, invocation IDs and executable hashes
are unchanged. No directory or database was deleted by closeout. The resource
receipts are `phase1-resource-closure-01.json` and
`phase1-resource-readback-01.json` in the retained remote logs directory.

Phase 1 is accepted within the selected matrix. Delivery commit
`59ce0747a5b6947a088fe9eec22a8fa72d77a183` was fast-forwarded into `main` and
pushed to `origin/main`; the remote ref was read back at that exact commit.
The original checkout's unrelated change inventory is unchanged. This
documentation follow-up records publication separately from tested sources;
no deployment occurred. Automatic intro detection/BIF previews and the
large-library concurrency/storage/guest-restart phase remain required and have
not started product implementation. This acceptance does not complete the
three-phase goal.
