# Selected compatibility phase 3 execution record

Status: **delivery code frozen; consolidated build and verification starting**.
Phase 2 closed at `257415e` after its recorded acceptance, repair and resource
closure. This phase implements music, search and discovery on
`codex/selected-client-compatibility`. The original client's commercial licensing
is not an acceptance gate under the user's selected adapter boundary.

All delivery code, test sources and native administrator changes are ready
before consolidated verification. Local compilation and unit tests
are authorized; actual database, media, integration and browser execution remains
on `test-env`. The ui-ux-pro-max skill remains disabled. No phase 3 passing result,
final merge, push or deployment is claimed by this implementation record.

## Cohesive implementation groups

| Group | Delivery | Acceptance to record after code freeze |
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
remain coordinated. Tests are consolidated after every group reports source
completion; read-only source and contract inspection is not runtime acceptance.

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

## Related records

- [Four-phase execution plan](../planning/selected-compatibility-plan-20260920.md)
- [Closed phase 2](selected-compatibility-phase2-20260920.md)
- [Current status](current-status.md)
- [Handoff](handoff.md)
