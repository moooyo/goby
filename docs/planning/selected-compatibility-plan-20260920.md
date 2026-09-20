# Selected playback and client compatibility execution plan

Status: **phase 1 closed under the user-approved adapter boundary; phase 2 consolidated verification and scoped repairs in progress**.
Recorded on September 20, 2026. The implementation baseline is product commit
`80198b6`, with the completed increment documented at `f6f2d16` on `main`.
This plan records the next work; it does not reopen the completed AMD/media
three-phase increment or claim new verification or deployment.
Execution uses `codex/selected-client-compatibility` in an isolated checkout.
The user authorized local compilation and unit tests, remote integration/E2E on
`test-env`, and merge to `main` plus push after all four phases are complete.
Within each phase, implement all delivery code before running the consolidated
verification. Local actual E2E remains prohibited.

## Scope decision

The user selected the remaining playback/account, subtitle, artwork, music,
management configuration, search and client-protocol functionality described
below. Existing implementations and accepted results remain the baseline.

| Disposition | Scope |
| --- | --- |
| Selected now | Four functional phases below, including their native administration, applicable compatibility adapters, remote verification and documentation |
| Explicitly excluded by the user | Live TV, EPG, DVR/scheduled recording, tuners, DLNA, external channels and group/synchronized playback |
| Still deferred | Offline synchronization/download packages, OCI delivery/playback/upgrade/recovery/public HTTPS, non-AMD hardware, native arm64, provider-specific online acceptance, broader capacity/storage-fault/host-durability profiles, unrelated extensions, licensing and external release decisions |
| Existing exclusions retained | Consumer web player, Emby Connect/cloud identity, Emby package installation and proprietary binary-plugin compatibility |

Exclusion is a product scope decision, not a postponed milestone. Do not add
excluded families through settings, discovery, protocol aliases or test fixtures.
Existing generic dynamic sources, HLS, bounded time shifting, remote session
commands and the zero-source Programs adapter remain supported within their
current contracts; this decision does not request their removal. External
channels are distinct from the already integrated metadata/subtitle providers.
General recommendation engines, automatic intro detection, BIF previews, themes
and game/book media remain deferred; local music Similar/InstantMix and skipping
explicitly sourced intro intervals are selected here.

## Delivery sequence

Each phase includes its own contract inventory, implementation, native UI where
needed, migrations, remote tests, relevant builds and documentation closeout.
Finish and verify a phase before starting the next phase's product changes.
Independent work inside a phase may run in parallel. There is no separate
preparation phase or acceptance-only final phase.
Any route, configuration or event correction required for a phase's actual
consumer belongs to that phase; phase 4 owns the remaining cross-cutting
contracts and cannot be used to postpone an earlier phase's acceptance.

| Phase | Deliverable | Main dependency | Required acceptance | Status |
| --- | --- | --- | --- | --- |
| 1. Accounts and playback behavior | PIN/local-password authentication, intro markers and skipping, next-episode behavior and their real preference consumers | Existing identity, preferences, playback reporting and NextUp | Login/revocation and actual movie/episode playback journeys; source-bound timing, permissions, restart and recovery | Closed; adapter acceptance and successful client journeys retained, original-client entitlement limitation accepted by the user |
| 2. Subtitle and artwork processing | Embedded subtitle removal, bitmap OCR, embedded audio covers, generated collages and missing image transformations | Phase 1 baseline; existing media jobs, indexed streams, artwork and backup | Real modified media, OCR cues and rendered images; cancellation, source replacement, cache invalidation and restore | Delivery code implemented; consolidated verification and scoped repairs in progress |
| 3. Music, search and discovery | Artist prefixes/Similar/InstantMix, Search/Hints, selected missing query contracts, missing-episode and suggestion preferences | Phase 2 covers/artwork; current catalog, user state and preference stores | Authorized music/search/discovery journeys, exact filtering/counting/paging and restart persistence | Not started |
| 4. Management configuration and client protocols | Backed configuration fields, policy/library/device projections, remaining aliases/events and external notification transport | Closed phases 1-3 | Real settings consumers, client event/command/notification journeys and final cross-phase regression/build/upgrade/recovery | Not started |

### Phase 1: Accounts and playback behavior

| Task | Implementation and boundary | Completion evidence |
| --- | --- | --- |
| A1. Profile PIN and local-password authentication | Implement the actual distinct contracts: encrypted four-digit profile PIN for the authenticated client's profile gate, and hashed local password for trusted-local-network login; current authority, reset and local-login attempt limits apply | Real profile gate and login behavior, remote denial, spoofed proxy input, isolation and restart; only the already authenticated owner may receive ProfilePin; no public/cross-user/log/plaintext-backup disclosure |
| A2. Intro data and lifecycle | Add source-bound intro intervals from explicit chapter markers, local import and administrator editing; retain provenance and manual override rules for movies and episodes | Known markers, absent markers, invalid/overlapping intervals, changed media and restart; chapter presence alone does not imply an intro interval |
| A3. Intro playback behavior | Connect None/ShowButton/AutoSkip to actual marker APIs and client behavior; define one owner for each seek to avoid duplicate client/server skipping | Actual client seeks, resume/progress/play counts, direct play/remux/transcode/HLS, explicit start and boundary cases; unsupported client behavior stays visible |
| A4. Next-episode behavior | Connect EnableNextEpisodeAutoPlay to ordered, authorized next-episode selection and compatible client consumption; prevent replays/duplicates and honor explicit user action | End-of-episode transition, last episode, unavailable/unauthorized next item, disabled preference, disconnect and current-authority changes |
| A5. Administration and durable state | Add the required account/preference/intro controls and task visibility; preserve existing credential, session and preference boundaries | Administrator/user journeys, concurrent changes, migration, restart and backup/restore of new state |

The server cannot make an arbitrary unmodified client show a button or auto-play
an item. Acceptance must name an actual client that consumes each contract;
storing a Boolean or returning an interval alone is insufficient. This phase
does not add a consumer player. DisplayMissingEpisodes and HidePlayedInSuggestions
belong to phase 3, where their catalog consumers are implemented together.

The user explicitly selected original-client PIN compatibility after inspection
of the retained Emby Web distribution: it compares Configuration.ProfilePin
locally and updates it through Configuration/Partial. PIN is therefore an
authenticated profile lock, not a replacement for password authentication.
Encrypted at-rest storage and owner-only authenticated projection replace the
initial plan's blanket prohibition on PIN in DTOs. Administrator management
returns PIN presence rather than plaintext.

On September 20, after the original-client runs, the user selected delivery of
the compatibility adapter for third-party clients and explicitly removed the
original Web client's commercial authorization restrictions as a blocking gate.
The delivered server adapter includes source-bound IntroStart/IntroEnd markers,
persisted skip preferences and authenticated free feature registration. Client
seeks remain client-owned. This decision closes phase 1 using the recorded
backend/media contracts and successful account/autoplay/administration journeys;
it does not turn the original client's blocked skips or four page errors into
passing results, and does not claim untested third-party-client behavior.
No modified original consumer Web distribution is required for this delivery.

### Phase 2: Subtitle and artwork processing

| Task | Implementation and boundary | Completion evidence |
| --- | --- | --- |
| B1. Embedded subtitle removal | Implement an explicit administrator media-edit operation for the selected writable local container profiles; remove the chosen stream by remuxing, validate the result and publish safely; read-only/dynamic sources return a truthful rejection | Actual stream removal with unrelated A/V streams, chapters, attachments and metadata preserved; playback, stable item/user state, active readers and failure-before-publication |
| B2. Bitmap subtitle OCR | Add cancellable PGS/DVD bitmap-to-text jobs, explicit language/model selection, timed SRT/WebVTT output and review/correction; define the initial engine, language and fixture inventory in this phase | Known cue text and timings, forced/SDH metadata where available, empty/overlapping cues, multilingual samples, confidence/error reporting and actual playback switching |
| B3. Embedded audio covers | Extract validated embedded covers from selected supported music containers, with explicit precedence against managed images and sidecars | Tagged MP3/FLAC and other admitted fixtures, malformed/large images, source replacement, rescan and stable artwork identity |
| B4. Generated collages | Add deterministic library/genre collages using currently authorized source artwork; define empty/partial fallback and regeneration triggers | ACL-sensitive content, source deletion/change, deterministic order, concurrency and cache separation |
| B5. Image transformations | Implement crop, orientation, custom background/foreground composition, played/progress/count overlays and animation-preserving transformations for declared formats | Visual fixtures, GIF frame timing, transparency, overlay correctness, dimensions/resource limits and conditional/cache responses |
| B6. Jobs, management and preservation | Integrate progress/cancel/review/apply operations with current tasks and native UI; invalidate affected media, subtitle and image identities only after valid publication | Interrupted writes, cancellation, stale source, current permissions, rescan, migration/restart and backup/restore of owned derivatives |

Subtitle removal changes a media file and must be a deliberate product operation;
verification uses owned media copies. The existing database backup does not
become a backup of original media. Specify temporary-space, replacement,
recovery and active-reader behavior before accepting removal. OCR must produce
usable text and timing; installing an engine alone does not deliver the feature.
Initial OCR languages and writable container profiles are recorded explicitly,
not described as universal language/container coverage.

### Phase 3: Music, search and discovery

| Task | Implementation and boundary | Completion evidence |
| --- | --- | --- |
| C1. Music API gaps | Add artist/item prefix operations, artist Similar and album Similar aliases; implement a shared InstantMix core for supported item/song/album/artist/genre/playlist seeds with documented local metadata rules | Positive and empty libraries, Chinese/Latin prefix buckets, multi-artist/compilation data, entity/item identity, exclusions, bounded deduplication and playable results |
| C2. Search/Hints | Add the actual compatibility adapter over authorized catalog/entity data; define types, matching/ranking, totals, paging and image references | Movies/TV/music/people searches, Chinese/Latin names, empty queries, duplicate names and restricted libraries; actual client navigation from a hint |
| C3. Query contract completion | Enumerate currently missing filters, Fields, multi-key sorts and aliases needed by these selected journeys; implement source-backed predicates/projections rather than inert parameters | Cross-filter truth tables, deterministic ties, paging/count equivalence, permission changes and explicit unsupported behavior |
| C4. Missing episodes | Build source-attributed missing-episode facts from imported/local or already available metadata, connect DisplayMissingEpisodes, and distinguish placeholders from playable items | Known missing/present/special episodes, no-evidence behavior, later media arrival, refresh and exclusion from playable queues |
| C5. Suggestions and user state | Identify and implement the actual Suggestions consumer from the SDK/client contract, then connect HidePlayedInSuggestions with explicit query precedence and isolated per-user state; do not apply it to InstantMix without contract evidence | Played/unplayed transitions, favorite/like combinations, mixed permissions, preference changes and restart |
| C6. Music/discovery administration | Expose the needed preference, metadata and artwork controls; update schema/recovery coverage for new facts | Native administrator journey and actual client artist-to-album-to-track, mix and search journeys |

Use local metadata and controlled fixtures for these deliverables. New calls to
TMDB, MusicBrainz or OpenSubtitles and their online acceptance remain deferred.
Missing-episode facts require a real source; a numbering gap alone is not proof.
Existing ListItemIds membership, music tags, core entity APIs, NextUp and selected
query sorting are retained and extended only where a concrete gap exists.

### Phase 4: Management configuration and client protocols

| Task | Implementation and boundary | Completion evidence |
| --- | --- | --- |
| D1. Configuration inventory | Map the selected server, encoding, device, library and policy fields to stored values and real consumers; record defaults, full/partial/reset behavior, authority and reload/restart requirements | A field-level implementation matrix; no writable field is complete while its consumer is absent |
| D2. Backed management settings | Implement applicable bind/port settings, device options, CPU/AMD selection, encoder threads/quality/CRF/tone mapping and relevant metadata/library/policy options; keep unsupported/excluded families absent | Actual consumers, invalid combinations, atomic updates, restart reporting and persistence; existing jobs retain their execution snapshot, new jobs use new values and affected capability caches invalidate |
| D3. Management adapters and UI | Complete applicable system/capability projections and management routes for this inventory, with usable administrator forms and task controls | Native and compatibility reads/writes agree; stale writes, unauthorized actions and partial/reset semantics are exercised |
| D4. Client wire contracts | Complete remaining cross-feature route/case/legacy aliases, DTO fields, errors, session/event and remote-command contracts; phases 1-3 already own adapters required for their acceptance | Recorded client requests, correct response semantics, reconnect, missed refresh recovery, delivery ordering and revocation; existing subscriptions and LibraryChanged are regressions, not new features |
| D5. External notifications | Implement a selected documented transport, source-authorized event routing, secure device/session token registration/rotation/revocation, bounded queues/retries and invalid-target handling | A private controlled receiver observes actual messages, revocation/disable and retry behavior; matched client delivery is recorded separately; vendor tokens are accepted only with a real matching adapter and stay out of DTO/log/plaintext backup output |
| D6. Integrated closeout | Verify the integrated account-to-browse-to-play-to-state-refresh and administrator journeys; reconcile all new state with the final artifact and migration/recovery paths | Final affected regression scope, ordinary/embedded builds, current administrator assets, migration/restart/backup/restore and owned-resource closure |

D5 does not claim APNs/FCM or proprietary Emby push delivery through an arbitrary
webhook. The implementation matrix must name the supported transport and clients.
Metadata/subtitle provider-online deferral does not remove this selected feature;
an external delivery gate needs the selected service's actual credentials. If
those are unavailable, record controlled-transport acceptance and the outstanding
real-delivery gate separately instead of claiming end-to-end push completion.
Phase 4 cannot close while its required delivery evidence is outstanding.
Likewise, exposing CPU/AMD settings does not add non-AMD hardware acceptance.
Full original Emby Web parity outside the selected journeys remains separate.

## Common acceptance and handoff rules

- Complete all code for a phase before consolidated verification. Local
  compilation and unit tests are authorized. Integration tests, actual media
  execution and E2E/browser verification use `ssh test-env`; actual E2E never
  runs locally. Selected AMD checks retain the authorized CT 104 profile via
  `ssh pve`. Remote verification workloads remain coordinated and serialized.
- Each phase first reconciles current source against its contract inventory.
  Record every selected gap as implemented, blocked with a concrete prerequisite,
  or still pending. Do not silently move an unimplemented selected task to a
  later phase or substitute a mock success.
- Verify real consumers and outputs in addition to route/DTO tests. Use actual
  supported clients for client-owned behavior; a media harness supplements but
  does not replace those journeys.
- Cover relevant permissions, per-user isolation, source replacement, concurrent
  writes, interruption/cancellation, migration, restart and recovery. Size and
  duration budgets must be measured within the admitted remote profile.
- Integrate and build each phase's final source, repair failed behavior, and run
  the affected verification before closeout. The last phase also covers final
  cross-phase interactions; unchanged accepted checks need not be replayed solely
  to create new receipts.
- Record source/artifact/tool/client identities, results and retained failures;
  close owned workers/databases and preserve unrelated services and evidence.
- Update the implemented API, feature contracts, support matrix, execution plan,
  current status and handoff before marking a phase complete. Implementation,
  acceptance, merge/publication and deployment remain separate statuses.
- Preserve unrelated local OCI, packaging, helper and historical draft changes.
  Old Programs/W/H1 attempts are not an automatic execution queue for this plan.
- After all four phases pass their requirements, update the handoff, merge the
  completed branch into `main` and push. Verify the remote ref and keep unrelated
  working-tree changes outside that publication.

## Source references

- [Current handoff](../development/handoff.md)
- [Preference contract](../development/client-preferences-plan.md)
- [Subtitle/media contract](../development/advanced-media.md)
- [Artwork contract](../development/local-artwork.md)
- [Music metadata](../development/music-metadata.md)
- [Navigation/query contract](../development/next-up.md)
- [Configuration contract](../api/configuration.md)
- [Client sessions](../development/client-sessions.md)
- [Implemented API inventory](../api/implemented.md)
