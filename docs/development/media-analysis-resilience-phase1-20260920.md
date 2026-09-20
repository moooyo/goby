# Compatibility long-tail phase 1 execution record

Status: **product and test source complete; entering consolidated remote verification**.

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
| Integrated administration/client journey | New twenty-stage Go fixture, browser driver and execution guide integrated | `TestMediaAnalysisResiliencePhase1BrowserIntegration`, real HTTP/PG/media scan/native controls, restart and owned-resource closure |

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
results, not passing product, recovery or client suites. The exported catalog is
now included for the final phase verification artifact build.

Private admission, setup, compilation and generation receipts are retained under
`D:/Code/goby/.git/media-analysis-resilience-20260920/phase1` and the owned remote
root. Credentials and DSNs remain only in the private runtime context.

The dedicated browser sources are
`internal/server/media_analysis_resilience_phase1_browser_integration_test.go`
and `scripts/test-env/media-analysis-resilience-phase1-browser.mjs`, with the
adjacent execution guide. Their current twenty-stage inventory covers native
account changes and queried selection, source-backed TV facts, sorting/import
consumers, field projection/exclusion, search navigation, a real runtime restart
and normal cleanup, including dynamic embedded-artwork selection, new audio
source arrival, disabled extraction, retained images, re-enabled extraction and
actual decoded image evidence. Final controls and independent observations must match the
integrated source before executing the batch.

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

No merge, push or deployment has occurred for this increment. Phases 2 and 3
remain required and have not started.
