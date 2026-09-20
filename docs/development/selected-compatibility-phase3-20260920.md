# Selected compatibility phase 3 execution record

Status: **closed from the recorded consolidated and affected repair scopes**.
Phase 2 closed at `257415e` after its recorded acceptance, repair and resource
closure. This phase implements music, search and discovery on
`codex/selected-client-compatibility`. The original client's commercial licensing
is not an acceptance gate under the user's selected adapter boundary.

All delivery code, test sources and native administrator changes were frozen
before consolidated verification. Local compilation and unit tests
are authorized; actual database, media, integration and browser execution remains
on `test-env`. The ui-ux-pro-max skill remained disabled. This record composes
passing scopes with their original revisions and retains earlier failed runs as
failed. It does not claim a final whole-suite rerun, final merge, push or deployment.

## Cohesive implementation groups

| Group | Delivery | Recorded coverage |
| --- | --- | --- |
| C1 Music discovery | Artist/item prefix lists, artist Similar, album Similar alias and a shared InstantMix resolver for item/song/album/artist/genre/playlist seeds | Authorized playable results, typed identities, metadata ranking, Chinese/Latin prefixes, playlist deduplication and actual browse/mix/play consumption |
| C2 Search | Mixed authorized item/entity Search/Hints, combined ranking/count/paging and explicit navigation/image references | Chinese/Latin and duplicate names, type switches, restrictions, stable pages and actual hint navigation |
| C3 Query contracts | Presence-aware missing/unaired selectors, like/dislike/folder forms, music ordering and selected source-backed fields | Predicate truth tables, explicit unsupported behavior, source-backed projections and count/page equivalence |
| C4 Expected episode facts | Versioned administrator-imported roster, source provenance, durable active/retired facts and separate unplayable discovery projection | Explicit source facts, unknown dates, special episodes, no-evidence behavior, replacement/withdrawal and later physical arrivals |
| C5 Suggestions | Actual Suggestions endpoint using documented local catalog/user-state selection and HidePlayedInSuggestions precedence | Played and preference transitions, explicit override, user isolation and current authority |
| C6 Administration and durability | Writable discovery preferences, roster import/review/withdrawal UI, schema evolution, archive/recovery and browser fixture | Native controls, CAS, restart, exact retained facts, recovery validation and owned-resource closure |

Music, search, shared queries/Suggestions, expected facts, native UI and
recovery/acceptance have separate implementation owners. Shared route namespace,
registration, preference persistence, migration manifest and final integration
remain coordinated. The complete code freeze was `327aa3c`, followed by the schema 46 catalog at
`460c33a`. Compilation and execution receipts record those identities separately.
Read-only source and contract inspection is not runtime acceptance.

## Scope and contract decisions

Missing episodes require a bounded, explicit administrator roster for an existing
Series. A numbering gap alone creates no expected fact. Imported dates are UTC
calendar dates; absent dates remain unknown. Source replacement and withdrawal
are explicit operations with decimal-string CAS revisions. Expected facts remain
separate from physical items, and have no playable source or invented filesystem
path. Discovery combines the admitted physical and virtual populations before
counting and paging. Playable queues, Resume, mixes and Suggestions retain real
media requirements.

Search keeps internal item/entity references distinct even when string IDs
collide. Compatibility IDs remain strings. Goby-specific navigation and image
URLs provide an explicit route to their authorized owner, including entity names
that collide with route literals. The older Search/Hints specification is
documented separately from the current pinned SDK, which does not expose that
endpoint; implementation does not imply upstream parity.

The two persisted discovery preferences become writable with their consumers.
Explicit query filters take precedence over preference defaults, and application
keys do not acquire a user's preference authority merely by naming a UserId.
HidePlayedInSuggestions does not change InstantMix semantics.

Provider-specific online acceptance remains deferred: this phase uses local
metadata and controlled fixtures, without new TMDB, MusicBrainz or OpenSubtitles
calls. Live TV, EPG, DVR, tuners, DLNA, external channels and group playback remain
excluded. Phase 4 configuration/protocol/notification product work has not begun.

## Verification composition

The [machine-readable result record](selected-compatibility-phase3-results-20260920.json)
contains source/tree identities, compiled and executed binary hashes, selectors,
retained failures and private receipt hashes. Private evidence is retained under
`D:/Code/goby/.git/selected-compatibility-20260920/phase3` and
`/opt/goby-selected-compatibility-20260920-p3a` on `test-env`. Counts from
intersecting scopes are not additive; passing parent counts exclude subtests.

The frontend build passed at `327aa3c`. At `460c33a`, all ten Go artifacts
compiled: seven package test binaries, the tagged native browser test binary,
and ordinary/embedded Linux applications. Schema 46 adds explicit roster/import
history/expected-episode tables. The raw PostgreSQL 17 export is 786,506 bytes,
SHA-256 `9cd74ea71f30e07473a7effd7a3831b0a36fba7e35d27464181e0dfeb22f2d59`.
Historical migration digests and catalogs were not changed.

| Receipt | Revision and retained result | Scope |
| --- | --- | --- |
| `verification-01.json` | `460c33a`, **FAIL** | Identity 182, database 63, backuppg 112, recoverydb 12, recovery 29 and native browser 1 parent passed. Library passed 796 parents and failed one; server passed 882 and failed one. |
| `verification-repair-01.json` | `22ffb4b`, **PASS for selected scopes** | The single failed library parent and single failed server parent each passed after test-only fixture repairs. |
| `mocked-browser-01-receipt.json` | `460c33a`, **FAIL** | 17 expected and 7 unexpected results: all 12 selected-playback administrator cases passed; roster had 5 passes and 7 failures. No skips, flaky results, report errors or unhandled API requests. |
| `mocked-browser-02-receipt.json` | `4ff431a`, **FAIL** | Roster passed 11 cases and failed one alert-text expectation; zero skipped/flaky results, report errors or unhandled API requests. |
| `mocked-browser-03-receipt.json` | `dff0172`, **FAIL** | The one remaining roster case failed because its previous-draft baseline was captured before import completion. |
| `mocked-browser-04-receipt.json` | `9362813`, **PASS for one selected case** | The invalid/oversized import case passed after waiting for the actual successful import and preview before reading the prior draft. |

The initial library failure was the historical migration fixture's table
inventory, which had not included the newly introduced roster tables. The
server failure came from a duplicate `Content-Type` in the test request helper.
Commit `22ffb4b` changes only those two Go test files. Product migration,
authorization and roster behavior were not relaxed. Both targeted parents
passed and both process groups closed. The first batch remains failed.

The initial mocked roster failures were required-label locator mismatches.
Commit `4ff431a` changes only `episode-roster.spec.ts` to resolve the two required
fields by accessible textbox role. Existing production assets and backend code
are unchanged. Mocked UI coverage is kept separate from actual backend/browser
acceptance. The second run passed 11 roster cases and failed the invalid/oversized
import alert-text expectation. Commit `dff0172` corrected the specific message
expectations, but the focused third run exposed a premature empty draft baseline.
Commit `9362813` waits for the success notice and completed preview before reading
the actual prior draft. The invalid-input, UTF-8, size, preserved draft/source and
never-write assertions remain intact. The fourth run passed that one case.

The closure receipt composes the latest result by `(file, title, project)` across
all four reports: 24 unique expected cases, comprising 12 selected-playback and
12 roster cases. Every receipt has zero flaky/skipped results, report errors and
unhandled API requests. This composition is not a final complete-suite rerun;
the first three mocked runs keep their failed status. From `460c33a` through
`9362813`, the only changed files are the two Go fixtures and roster browser
specification. Native/browser and ordinary/embedded binaries remain those built
at `460c33a`, and frontend assets remain those built at `327aa3c`.

The first batch retains the explicit mount-namespace helper and AMD-specific
server skips. Those profiles are not counted as passing executions. The native
browser parent and the two targeted repair parents had no skips. Source
snapshots, earlier logs and failed receipts are preserved; later documentation
never changes their `complete: false` values.

## Native browser acceptance

Run `selected-phase3-460c33a-browser01` passed all 16 stages: authentication,
preferences, roster review/save, missing queries, Suggestions played-state
changes, music navigation/mixes, search navigation, audio playing/stopped,
physical arrival, arrival queries, restart, persisted state and cleanup. Every
stage has independent database/file acknowledgement. There were zero page
errors and foreign requests, and no fallback session revocations.

Native controls exercised actual authenticated preference writes and roster
preview/save. The imported source has one aired, one unknown-date and one future
episode. The discovery paths keep missing facts unplayable: detail can return
200 while `PlaybackInfo` returns 404, with physical-only playback queues. A
series with no source evidence has no inferred missing episodes. Actual media
copy and scan changed missing results from three to two, made the arrived item
physical, and retired its virtual discovery reference without altering the
accepted roster source facts.

The private adapter consumer observed artist/album/track navigation and eight
representative InstantMix routes, each returning the exact three expected
tracks. Suggestions reflected played/unplayed transitions and preference/query
precedence. Search returned 12 hints across item/entity reference kinds; actual
item/entity detail navigation and a decoded authorized 64-by-64 image returned
200. These are local catalog adapters, not online-provider acceptance or
arbitrary third-party-client parity.

Actual source-backed audio advanced from 1.232 to 2.072 seconds across eight
samples. The running Web Audio graph observed nonzero RMS/peak values from the
media element, with its output connected to the destination and no synthetic
signal. This proves audio signal in the browser graph; physical speaker output
was not observed. `Playing`, `Progress` and `Stopped` were each sent once and
returned 204. The independent database observation verified the actual source
plan, a counted playback and final play count of one. `ClientCorrelated=false`
is preserved as an explicit boundary for phase 4 protocol acceptance.

Stop also returned 204 for active encodings, detached the media element and
closed the audio context. The one producer's terminal history remains; its
cache and contexts closed, with zero active jobs, HLS sessions, stream slots,
original requests and media-policy leases. The viewer remains authenticated at
this intermediate stop stage and is logged out during cleanup. Final cleanup
has zero active sessions before fallback, and no fallback revocation was needed.

The real server restarted once; the complete before/after database snapshot and
retained source files/facts were identical. The native driver verified frozen
embedded assets and unchanged execution inputs, then closed its process group,
observed descendants, HTTP listener and server workers. It removed its owned
schema, media root and private context. Seven retained screenshots cover native
preferences/roster, actual search image/audio and post-arrival persistence;
native sensitive fields are masked. These captures do not establish unrestricted
visual review of masked values.

## Resource closure

`closure-01.json` records phase-owned PostgreSQL stopping from PID `3310590`
to `MainPID=0`, inactive/dead, with the data preserved. All six owned verification
and mocked UI units have `MainPID=0` and empty control groups or are no longer
loaded. Failed unit results remain failed. The native test, Go repair and mocked
runs separately record their process-group, listener, connection and temporary
resource closure. No new tests ran while closing resources.

The unrelated `goby-core-av-original-client-01.service` remains active at PID
`366598` with its original invocation identity. Original client service and
assets are unchanged. Evidence, earlier failed receipts and PostgreSQL data
remain retained. The phase is closed and phase 4 implementation may begin.

## Related records

- [Machine-readable results](selected-compatibility-phase3-results-20260920.json)
- [Music discovery API](../api/music-discovery.md)
- [Search hints API](../api/search-hints.md)
- [Discovery queries](../api/discovery-queries.md)
- [Expected episode rosters](../api/expected-episodes.md)
- [Four-phase execution plan](../planning/selected-compatibility-plan-20260920.md)
- [Closed phase 2](selected-compatibility-phase2-20260920.md)
- [Current status](current-status.md)
- [Handoff](handoff.md)
