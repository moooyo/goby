# Documentation guide

Research and verification updated: **2026-09-10, Asia/Shanghai**.

Implementation status: **service foundation, catalog ingestion, local metadata, entities, indexed artwork, original playback, user state, initial events/remote control, authenticated HLS VOD with seeking, Universal/progressive audio, progressive MP4 video, ordered audio/video PlaybackInfo profiles, native user management, persistent administrator metadata editing, login-session administration, application keys with independent client contexts, and explicit media refresh are implemented; full compatibility remains in progress**. The required stack is Go, PostgreSQL with pgx/v5, FFmpeg, and a React/MUI administrator dashboard. Linux is the deployment target; the dashboard contains no consumer playback page. Current stable Go/FFmpeg pins and verification permissions are recorded in the toolchain document below. Nonzero video-copy seeking, efficient long-source seeking, additional input/timing profiles, packed-audio HLS, broader subtitle/output support, hard resource isolation, and actual GPU execution remain work.

The official reference corpus contains **1476 records**. The [user-device study](research/devices-reference.md) adds 348 records with 346 complete HTTP exchanges and two audits, preserving the preceding 1128-record checkpoint `a631dd3`. The earlier [application-key management](research/api-key-reference.md), [playback](research/api-key-playback-reference.md), [client-context](research/api-key-context-reference.md), and [target-scope](research/api-key-scope-reference.md) evidence remains intact. Records also include supporting probes and earlier preserved incomplete responses; they are not counts of implemented endpoints or successful client workflows. Current schema **16**, through `0016_application_keys.sql`, adds independent credentials, persisted client contexts, and nullable playback ownership while preserving old records. Schema 15 introduced explicit media-refresh task modes; schema 14 introduced metadata state. Probe cache version **6** adds private restart indexes and requires a normal rescan when upgrading older probe snapshots.

Device research distinguishes a global reported-ID registry from user credentials and client sessions. It records decimal-string registry IDs, optional `CustomName` clearing, both deletion routes returning `204` and invalidating the selected device's observed ordinary logins, and re-registration with a new ID and cleared options. The reference's zero total counts and ignored sorting are recorded separately. Goby's device-management implementation and application-key device/deletion behavior remain pending.

The [M5d verification](development/verification-m5d-application-keys.md) passed **1046 top-level race tests across twelve packages**, with zero skips or race findings, plus 75 focused tests across seven packages. Its isolated browser workflow passed in 6.603685 seconds with 33 real issued keys, two client contexts, revocation, and restart persistence; the final deployed workflow also passed with actual WebSockets and playback-context recovery. [M4g verification](development/verification-m4g-media-refresh.md) retains the preceding real-media refresh evidence, and [M5c verification](development/verification-m5c-sessions.md) records login-session management. [M4f verification](development/verification-m4f-video-seek.md) and its [operating contract](development/video-fast-seek.md) separate verified software video restart from linear audio I/O and hardware decoding. [Implementation progress](development/progress.md) keeps M4, M5, M6, and the complete goal explicitly unfinished.

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
| [User-device reference](research/devices-reference.md) | Reported-ID registry, custom names, ordinary-login revocation, re-registration, and preserved partial/complete captures; device implementation remains pending |
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
