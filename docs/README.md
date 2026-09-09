# Documentation guide

Research date: **2026-09-09, Asia/Shanghai**.

Implementation status: **service foundation, catalog ingestion, local metadata, entities, indexed artwork, original playback, user state, initial events/remote control, authenticated HLS VOD with seeking, and Universal/progressive audio are implemented; full compatibility remains in progress**. The required stack is Go, PostgreSQL with pgx/v5, FFmpeg, and a React/MUI administrator dashboard. Linux is the deployment target; the dashboard contains no consumer playback page. Current stable Go/FFmpeg pins and verification permissions are recorded in the toolchain document below. Progressive video, progressive PlaybackInfo profile coverage, additional audio timing/input profiles, packed-audio HLS, broader subtitle/output support, hard resource isolation, and actual GPU execution remain required work.

The reference corpus currently contains **610 records**: 436 earlier records and 174 records from the [audio reference study](research/audio-reference.md). These include supporting probe/provenance observations as well as HTTP captures; they are not counts of implemented endpoints or successful client workflows. Migration `0012` and probe cache version 3 are covered in the upgrade instructions below.

## Compatibility target

The target is for general-purpose Emby-compatible clients to connect and play supported media without client modifications. Matching API paths alone does not establish playback compatibility: the shared protocol must also match authentication, DTOs, negotiation, media/subtitle delivery, and playback reporting. Official documentation provides the baseline; reference-server captures and real-client tests provide evidence. Work proceeds through common client flows without requiring the user to supply a client list. See the [compatibility definition](api/implementation-scope.md#what-compatibility-means) for the acceptance boundary.

## Reading order

| Document | Purpose |
| --- | --- |
| [Implementation scope](api/implementation-scope.md) | Required API families, staged delivery, explicit exclusions, and legacy candidates |
| [Implemented surface](api/implemented.md) | Current Go handlers, tested workflows, and remaining compatibility limits |
| [Build and run](development/running.md) | Startup configuration, PostgreSQL deployment, migrations, and remote verification |
| [Local metadata](development/local-metadata.md) | NFO discovery, values, refresh behavior, and input boundaries |
| [Local artwork](development/local-artwork.md) | Indexed images, public binary retrieval, transforms, caching, and resource limits |
| [Original playback](development/direct-playback.md) | Negotiation, authenticated ranges, durable progress, watched/favorite state, and probe upgrade requirements |
| [Universal and progressive audio](development/audio-playback.md) | Original/progressive/HLS selection, supported audio outputs, exact timing, streaming failures and current limits |
| [Client playback references](development/client-playback-references.md) | Scoped client nonces, canonical playback identities, tombstones and migration 0012 |
| [Client sessions](development/client-sessions.md) | Capability declarations, presence, player-state projection, ownership, and Ping behavior |
| [WebSocket events](development/websocket-events.md) | Authenticated connections, user-state notifications, remote commands, authorization and resource limits |
| [Conversion engine](development/transcode-engine.md) | Planners, PostgreSQL jobs, bounded FFmpeg execution, progressive audio, hardware selection and HLS VOD production |
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
| [WebSocket reference](research/websocket-reference.md) | Audited upgrade paths, token-scoped events, message envelopes and limits of observed behavior |
| [HLS reference](research/hls-reference.md) | Full VOD manifests, seek hints, successful transcode segments, preserved remux failures and cleanup |
| [Audio reference](research/audio-reference.md) | Universal defaults, original delivery, progressive seeking, AAC/TS HLS and preserved reference inconsistencies |

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
