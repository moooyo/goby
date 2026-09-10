# Documentation guide

Research and verification updated: **2026-09-10, Asia/Shanghai**.

Implementation status: **service foundation, catalog ingestion, local metadata, entities, indexed artwork, original playback, user state, initial events/remote control, authenticated HLS VOD with seeking, Universal/progressive audio, progressive MP4 video, ordered audio/video PlaybackInfo profiles, native user management, persistent administrator metadata editing, login-session administration, application keys with independent client contexts, device administration, durable library-wide tasks and scheduling, native managed settings, and explicit media refresh are implemented; full compatibility remains in progress**. The required stack is Go, PostgreSQL with pgx/v5, FFmpeg, and a React/MUI administrator dashboard. Linux is the deployment target; the dashboard contains no consumer playback page. Current stable Go/FFmpeg pins and verification permissions are recorded in the toolchain document below. The ConfigurationService adapter, additional task executors, broader configuration, nonzero video-copy seeking, efficient long-source seeking, additional input/timing profiles, packed-audio HLS, broader subtitle/output support, hard resource isolation, and actual GPU execution remain work.

The official reference corpus contains **2305 records**. The [fresh configuration mutation study](research/configuration-mutation-reference.md) adds 254 records: 17 setup records and 237 capture records. The [configuration read study](research/configuration-reference.md) previously brought the corpus from 1965 to 2051 with 86 records. The [fresh ScheduledTasks mutation study](research/scheduled-tasks-mutation-reference.md), earlier [task read study](research/scheduled-tasks-reference.md), [key-device](research/key-devices-reference.md), [ordinary user-device](research/devices-reference.md), [application-key management](research/api-key-reference.md), [playback](research/api-key-playback-reference.md), [client-context](research/api-key-context-reference.md), and [target-scope](research/api-key-scope-reference.md) evidence remain intact. Records include supporting probes and preserved incomplete responses; they are not counts of implemented endpoints or successful client workflows. Deployed schema **20**, through `0020_managed_settings.sql`, adds nullable settings overrides while retaining probe cache version **6**.

Configuration reads return 60 total fields to the administrator but exactly `{}` with `200` to the ordinary viewer. Named encoding/devices/DLNA reads return administrator `200`, viewer `403`, and anonymous `401`. The fresh mutation study preserves a mixed invalid partial update's `500` despite a name change visible in the same process. Three key-authorized write controls are baseline no-ops returning `204`; they do not prove changed-value writes or restart persistence. The accepted [native settings API](api/settings.md) has its own revision, reset, and publication contract; the Emby ConfigurationService adapter remains unimplemented.

Device research distinguishes registry IDs, credentials, and session projections. It records ordinary-device grouping and revocation, key-authorized ordinary-device rename/delete, and deletion of one server device shared by two keys. All five tested key scopes then return `401`, even while three metadata Session DTOs remain cached. List omission does not prove hidden header-device absence; those Info/deletion paths and post-delete key recreation remain unsampled. An independent offline audit preserves the original finalization failure and verifies unchanged exports, credential cleanup, and old evidence; separate teardown removes the fresh fixture. Goby's accepted [M5e device contract](api/devices.md) documents its own generation and immediate-name-projection rules without claiming complete reference wire equivalence.

The initial task read study observed 22 definitions, all `Idle`. The fresh mutation study adds manual starts, both actual-running cancellation forms (`204` followed by `Cancelled`), idle-stop `500`, administrator unknown-ID `404`, and accepted trigger writes returning `204`. Sampled unknown and mixed-invalid arrays return `400` without installing the valid prefix. Accepted reference configuration does not establish scheduled firing, weekly/system-event execution, DST, or maximum-runtime enforcement; reference key authority remains unsampled. Goby's [task API](api/tasks.md) and [runtime contract](development/tasks.md) separately define and implement receipts, coalescing, owned scans, scheduling, and supported authorization. System-event triggers remain unsupported.

The [M5g native settings verification](development/verification-m5g-settings.md) passed **1222 top-level race tests across fourteen packages**, with zero test skips or race findings, isolated browser acceptance with two restarts, protected deployment, and the main-service workflow. The deployed executable, 419 Go/module/SQL inputs, and 50 current assets match the accepted artifacts. The 0.701-second live workflow temporarily changes all five fields, restores the original nullable overrides with CAS, and revokes its new native credential with an independent `401` and final SQL barrier. Old business rows remain exact; media/planning execution and a main restart were outside this workflow. M5g completes this native increment only. [M5f](development/verification-m5f-tasks.md), [M5e](development/verification-m5e-devices.md), [M5d](development/verification-m5d-application-keys.md), and earlier reports retain their historical evidence; [implementation progress](development/progress.md) keeps M4, M5, M6, and the full goal unfinished.

Application-key creation and secret recovery need the persistent file configured by `GOBY_API_KEY_MASTER_KEY_FILE`; its default relative path is `application-key-master.key`. The [operations contract](development/application-keys.md) explains private-directory deployment, Linux service ownership and mode checks, safe lazy creation, and restoring the database together with its matching master file.

## Compatibility target

The target is for general-purpose Emby-compatible clients to connect and play supported media without client modifications. Matching API paths alone does not establish playback compatibility: the shared protocol must also match authentication, DTOs, negotiation, media/subtitle delivery, and playback reporting. Official documentation provides the baseline; reference-server captures and real-client tests provide evidence. Work proceeds through common client flows without requiring the user to supply a client list. See the [compatibility definition](api/implementation-scope.md#what-compatibility-means) for the acceptance boundary.

## Reading order

| Document | Purpose |
| --- | --- |
| [Implementation scope](api/implementation-scope.md) | Required API families, staged delivery, explicit exclusions, and legacy candidates |
| [Implemented surface](api/implemented.md) | Current Go handlers, tested workflows, and remaining compatibility limits |
| [Verified video seeking](development/video-fast-seek.md) | Private restart evidence, decoder scope, linear audio history, and resource/cache limits |
| [Administrator media scans](api/admin-scans.md) | Normal scans, explicit media re-probing, persistent task modes and index rebuilding limits |
| [Administrator metadata API](api/admin-metadata.md) | Library item management, automatic/manual/locked layers, revisions and type changes |
| [Build and run](development/running.md) | Startup configuration, PostgreSQL deployment, migrations, and remote verification |
| [Native user management](api/admin-users.md) | Revision-checked account/policy edits, password reset, last-administrator protection and session revocation |
| [Administrator login sessions](api/admin-sessions.md) | Login history, filters, single-session revocation, self-sign-out and authorization boundaries |
| [Application-key API](api/application-keys.md) | Native secret-safe management, Emby key contracts, client contexts, target-user scope, and playback boundaries |
| [Application-key operations](development/application-keys.md) | Master-file configuration, encrypted recovery, backup requirements, revocation, and migration 0016 |
| [Device administration API](api/devices.md) | Three native and six non-camera compatibility operations, naming/revisions, lookup and removal semantics |
| [Device registry operations](development/devices.md) | Ordinary and shared device generations, credential isolation, concurrency, and migrations 0017/0018 |
| [Task administration API](api/tasks.md) | Eight native and six compatibility operations, run receipts, typed schedules, and DTO/error contracts |
| [Task execution and scheduling](development/tasks.md) | Owned transactions and scans, 100 ns scheduling, DST/missed-work policy, recovery, shutdown, and administrator UI |
| [M5f verification](development/verification-m5f-tasks.md) | Full race, two-restart browser acceptance, migration/deployment, and the main-service task workflow |
| [Native settings API](api/settings.md) | Three administrator operations, five nullable overrides, deployment defaults, revisions, reset, and strict input |
| [Managed settings operation](development/settings.md) | Commit/publication, request snapshots, planning boundaries, restart behavior, and administrator UI |
| [M5g native verification](development/verification-m5g-settings.md) | Complete race, browser/restarts, protected schema-20 deployment, and settings restoration on the main service |
| [Configuration reference](research/configuration-reference.md) | Read-only permission and field observations, private-wire redaction, preservation, and unobserved write/runtime behavior |
| [Configuration mutation reference](research/configuration-mutation-reference.md) | Fresh-instance writes, partial-failure visibility, baseline key controls, exact restoration, and unobserved persistence |
| [Local metadata](development/local-metadata.md) | NFO discovery, values, refresh behavior, and input boundaries |
| [Local artwork](development/local-artwork.md) | Indexed images, public binary retrieval, transforms, caching, and resource limits |
| [Original playback](development/direct-playback.md) | Negotiation, authenticated ranges, durable progress, watched/favorite state, and probe upgrade requirements |
| [Universal and progressive audio](development/audio-playback.md) | Original/progressive/HLS selection, supported audio outputs, exact timing, streaming failures and current limits |
| [Audio profile negotiation](development/audio-profile-playback.md) | Ordered PlaybackInfo profiles, projected constraints, standard media URLs and current-policy execution |
| [Progressive video playback](development/progressive-video-playback.md) | Standard MP4 URLs, ordered HTTP/HLS profiles, format-clock origin, copy/encode/seek boundaries and HTTP behavior |
| [Client playback references](development/client-playback-references.md) | Scoped client nonces, canonical playback identities, tombstones and migration 0012 |
| [Client sessions](development/client-sessions.md) | Capability declarations, presence, player-state projection, ownership, and Ping behavior |
| [WebSocket events](development/websocket-events.md) | Authenticated connections, user-state notifications, remote commands, authorization and resource limits |
| [Conversion engine](development/transcode-engine.md) | Planners, PostgreSQL jobs, bounded FFmpeg execution, progressive audio/video, hardware selection and HLS VOD production |
| [HLS playback](development/hls-playback.md) | Full VOD manifests, global segment addressing, seek production, authentication and cleanup |
| [Transcoding configuration](development/transcoding-configuration.md) | Startup settings, hardware choices, cache ownership and Linux service deployment |
| [Next-up queries](development/next-up.md) | Series-directed continuation, pagination, and the explicit global-query evidence gap |
| [External subtitles](development/external-subtitles.md) | Sidecar indexing, SRT/WebVTT delivery, time semantics, authorization and resource limits |
| [Full API catalog](api/catalog.md) | Every HTTP operation in the pinned official SDK export |
| [Machine-readable inventory](api/inventory.json) | Endpoint metadata, source references, planning classification, and implementation status |
| [Data models](api/models.md) | Offline reference for all 333 upstream schema definitions |
| [Client compatibility](research/client-compatibility.md) | Authentication, discovery, browsing, DTOs, user data, and protocol details |
| [Playback and transcoding](research/playback-and-transcoding.md) | Media negotiation, streams, subtitles, sessions, FFmpeg, and WebSocket behavior |
| [Administrator dashboard](research/admin-dashboard-and-linux.md) | React/MUI pages, management API mappings, and Linux operational concerns |
| [Architecture](architecture/linux-go-react.md) | Go modules, storage, jobs, security boundaries, deployment, and frontend design |
| [Delivery and verification](planning/delivery-and-verification.md) | Dependencies, completion gates, remote verification, and unresolved evidence |
| [Toolchain and hardware policy](development/toolchain.md) | Stable release pins, official sources, PostgreSQL integration, local build permission, and remote decode/encode verification |
| [Source provenance](sources/README.md) | Fixed upstream revisions, snapshot hashes, and known document defects |
| [Live reference baseline](research/reference-server.md) | Official Emby 4.9.5.0 isolation, audited HTTP captures and observed differences |
| [User-device reference](research/devices-reference.md) | Reported-ID registry, custom names, ordinary-login revocation, re-registration, and preserved partial/complete captures |
| [Key-device reference](research/key-devices-reference.md) | Shared server-device revocation, stale session projections, hidden-device limits, independent audit recovery, and fresh-fixture teardown |
| [ScheduledTasks reference](research/scheduled-tasks-reference.md) | Read-only task definitions, permission responses, observed triggers, and preserved task/device state |
| [ScheduledTasks mutation reference](research/scheduled-tasks-mutation-reference.md) | Owned fresh-instance starts/stops, trigger-write acceptance, no-partial-update controls, and completed fixture cleanup |
| [Scheduled tasks plan](research/scheduled-tasks-plan.md) | M5f task/run ownership, authorization, scheduling, core/UI implementation, and remaining acceptance work |
| [WebSocket reference](research/websocket-reference.md) | Audited upgrade paths, token-scoped events, message envelopes and limits of observed behavior |
| [HLS reference](research/hls-reference.md) | Full VOD manifests, seek hints, successful transcode segments, preserved remux failures and cleanup |
| [Audio reference](research/audio-reference.md) | Universal defaults, original delivery, progressive seeking, AAC/TS HLS and preserved reference inconsistencies |
| [Audio profile reference](research/audio-profile-reference.md) | HTTP/HLS profile order, protocol/context defaults, exact output conditions and measured response completeness |
| [Video profile reference](research/video-progressive-reference.md) | Progressive MP4, HTTP/HLS order, measured Range/HEAD behavior and preserved copy-seek failures |
| [Copied-video seek diagnostics](research/video-copy-seek/README.md) | Independent Linux edit-list, preroll, source-clock and buffered-remux controls; not a shipped copy-seek capability |

## Scope and evidence labels

- **Upstream fact:** observable in a cited official document or pinned SDK export; this does not imply runtime verification.
- **Goby design:** a proposed implementation decision, including every `/admin/v1` endpoint.
- **Unverified behavior:** a contract detail that needs a target server/client exchange, especially where the upstream sources conflict.
- **Legacy candidate:** a route found in older material but absent from the current research baseline.

The research inventory labels operations **planned, deferred, or excluded**; it is not a live implementation status report. Read the [implementation progress](development/progress.md) and its verification reports for delivered increments. Compatibility claims require recorded runtime evidence. Catalog classifications identify candidate service families; they do not make every operation in a family an MVP requirement. The endpoint selections in [implementation scope](api/implementation-scope.md) control initial delivery.

All Emby route examples are relative to the documented `/emby` base unless otherwise stated. `/admin` and `/admin/v1` are separate Goby namespaces. JSON is the first delivery format; XML and alternate route aliases require explicit compatibility coverage before being advertised.

## Offline use

The catalog, service pages, model reference, and JSON snapshots can be read directly from disk. No application server, package install, or browser-based API explorer is required. Follow schema references into the model reference or the original JSON for nested constraints.

The upstream exports contain sample server information and incomplete schema metadata. Treat them as research inputs. Do not send requests to the server addresses embedded in those files, and do not use either export as Goby's published API specification without a reviewed normalization step.
