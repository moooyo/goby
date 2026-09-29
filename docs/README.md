# Documentation guide

## Current status

As of September 29, 2026, the revised Phase 3 functional scope, Linux amd64
software OCI delivery, and selected AMD OCI extension are complete. The latest
integrated delivery was merged into `main` and pushed at `6c574ca`. Automatic
intro analysis and BIF previews are implemented and accepted within their
recorded profiles. The current OCI application binary is built from `b9bfa7c`;
image and deployment companion identities are recorded separately.

These deliveries do not establish a production deployment. Their verification
runtimes are closed, with data and evidence retained. Earlier documents may
contain then-current schema numbers, worker identities, incomplete milestone
labels, or instructions to resume a campaign. Those are historical checkpoints,
not instructions to reopen completed work.

Start with these three sources of current status:

| Document | Read it for |
| --- | --- |
| [Current status](development/current-status.md) | Completed implementation and acceptance, supported profiles, and deployment boundaries |
| [Development handoff](development/handoff.md) | Latest delivery, retained artifacts, closed resources, and continuation context |
| [Current execution plan](planning/current-execution-plan.md) | Completed selected scopes and the distinction between current work and historical plans |

## Deployment and use

Choose the operator guide for the intended runtime profile. Image acceptance is
specific to its recorded platform, tools and hardware; an archive or cross-build
alone does not establish another platform's support.

| Document | Read it for |
| --- | --- |
| [Software OCI operator guide](../deploy/oci/README.md) | Import the Linux amd64 image archive, configure external PostgreSQL, start Compose, and perform update/rollback |
| [AMD OCI operator guide](../deploy/oci/README.amd.md) | Import the AMD image and apply the GPU Compose and seccomp companions for the accepted hardware profile |
| [Build and run](development/running.md) | Application startup configuration, PostgreSQL, migrations and development execution |
| [Transcoding configuration](development/transcoding-configuration.md) | Media tools, hardware selection, runtime limits and cache configuration |
| [Application-key operations](development/application-keys.md) | Persistent master-key ownership and restoring a database with its matching master key |
| [Native backup and recovery](development/backup-recovery.md) | Encrypted archives, durable operations, offline recovery and generation switching |
| [Media-analysis runtime](development/media-analysis-runtime.md) | Native intro helper, analysis inventory, cache and execution lifecycle |
| [Media-analysis administrator UI](development/media-analysis-native-ui.md) | Analysis policy, tasks, progress, decisions and preview management |
| [Media-analysis recovery](development/media-analysis-recovery.md) | Analysis state and artifact behavior during backup, restore and restart |
| [Linux systemd installation](../deploy/linux/INSTALL.md) | The separate native installation contract and its stated package boundaries |
| [Toolchain and hardware policy](development/toolchain.md) | Tool sources and profile-specific build and runtime requirements |

The OCI deliveries include an embedded React/MUI administrator dashboard,
FFmpeg/ffprobe and PostgreSQL client tools. The PostgreSQL server, media,
application secrets and reverse proxy are external. The administrator dashboard
is not a consumer playback application. Consult the selected guide before using
an older deployment recipe or changing the media write policy.

## Functional and compatibility contracts

The compatibility target is that supported Emby-compatible clients can connect,
browse and play supported media without client modifications. Matching route
names alone does not establish compatibility: authentication, DTOs, negotiation,
media/subtitle delivery and playback reporting must agree within a tested flow.
The accepted client/profile boundaries remain explicit in the delivery records.

| Area | Documents |
| --- | --- |
| Implemented surface and scope | [Implemented APIs](api/implemented.md), [implementation scope](api/implementation-scope.md) |
| Architecture | [Linux Go/React architecture](architecture/linux-go-react.md) |
| Library scans and item management | [Administrator scans](api/admin-scans.md), [metadata API](api/admin-metadata.md), [local metadata](development/local-metadata.md), [local artwork](development/local-artwork.md) |
| Accounts, sessions and devices | [Users](api/admin-users.md), [login sessions](api/admin-sessions.md), [application keys](api/application-keys.md), [devices](api/devices.md) |
| Tasks and settings | [Task API](api/tasks.md), [execution and scheduling](development/tasks.md), [native settings](api/settings.md), [configuration compatibility](api/configuration.md) |
| Activity and diagnostics | [Observability API](api/observability.md), [activity/log implementation](development/observability.md), [media diagnostics](api/admin-media-diagnostics.md) |
| Direct and progressive playback | [Original playback](development/direct-playback.md), [audio playback](development/audio-playback.md), [audio profiles](development/audio-profile-playback.md), [progressive video](development/progressive-video-playback.md) |
| Transcoding and seeking | [Conversion engine](development/transcode-engine.md), [HLS playback](development/hls-playback.md), [video seeking](development/video-fast-seek.md), [external subtitles](development/external-subtitles.md) |
| Client state and events | [Playback references](development/client-playback-references.md), [client sessions](development/client-sessions.md), [WebSocket events](development/websocket-events.md), [NextUp](development/next-up.md) |
| Intro analysis and BIF previews | [Media-analysis API](api/media-analysis.md), [runtime](development/media-analysis-runtime.md), [administrator UI](development/media-analysis-native-ui.md) |
| Backup and restore | [Backup API](api/backups.md), [native recovery](development/backup-recovery.md), [analysis recovery](development/media-analysis-recovery.md) |

Emby route examples are relative to `/emby` unless a document says otherwise.
`/admin` and `/admin/v1` are Goby namespaces. Reference research and product
contracts serve different purposes; a captured reference response is not a
claim that Goby implements or accepts every behavior of that server.

## Acceptance records and history

Use the current closeouts for completion claims and exact artifact identities.
The result manifests retain the detailed counts, hashes, failed attempts,
repeats and exclusions; this guide does not combine counts across different
sources or interpret historical research totals as current test coverage.

| Completed scope | Record and evidence |
| --- | --- |
| Revised Phase 3 functional acceptance | [Closeout](development/phase3-functional-closeout-20260929.md), [results](development/phase3-functional-closeout-results-20260929.json), [publication](development/phase3-functional-publication-20260929.md) |
| Software OCI archive and Compose | [Delivery record](development/oci-delivery-20260929.md), [results](development/oci-delivery-results-20260929.json) |
| AMD OCI archive and Compose | [Delivery record](development/oci-amd-delivery-20260929.md), [results](development/oci-amd-delivery-results-20260929.json) |
| OCI Git integration | [Publication record](development/oci-publication-20260929.md); completes the separate merge/push step without changing image or runtime receipts |
| Media analysis and resilience Phase 1 | [Execution record](development/media-analysis-resilience-phase1-20260920.md), [results](development/media-analysis-resilience-phase1-results-20260921.json) |
| Automatic intro analysis and BIF Phase 2 | [Execution record](development/media-analysis-resilience-phase2-20260921.md), [results](development/media-analysis-resilience-phase2-results-20260922.json) |
| Selected compatibility increment | [Plan and scope](planning/selected-compatibility-plan-20260920.md), [Phase 1](development/selected-compatibility-phase1-20260920.md), [Phase 2](development/selected-compatibility-phase2-20260920.md), [Phase 3](development/selected-compatibility-phase3-20260920.md), [Phase 4](development/selected-compatibility-phase4-20260920.md) |
| Earlier AMD/media compatibility increment | [Phase 1](development/amd-media-phase1-20260919.md), [Phase 2](development/amd-media-phase2-20260919.md), [Phase 3](development/amd-media-phase3-20260919.md) |

### Historical implementation and reference material

These documents retain their original dates and scopes. An old pending item,
service state, deployment receipt or milestone label does not supersede the
current closeouts above.

| Collection | Entry points |
| --- | --- |
| Implementation chronology | [Progress history](development/progress.md), historical sections in [current status](development/current-status.md) and [handoff](development/handoff.md) |
| Earlier administration increments | [Devices](development/verification-m5e-devices.md), [tasks](development/verification-m5f-tasks.md), [settings](development/verification-m5g-settings.md), [configuration](development/verification-m5h-configuration.md), [observability](development/verification-m5i-observability.md), [backup/recovery](development/backup-recovery.md) |
| Original research inventory | [Source provenance](sources/README.md), [API catalog](api/catalog.md), [machine-readable inventory](api/inventory.json), [data models](api/models.md) |
| Reference-server observations | [Reference baseline](research/reference-server.md), [client compatibility](research/client-compatibility.md), [playback and transcoding](research/playback-and-transcoding.md), [administrator/Linux study](research/admin-dashboard-and-linux.md) |
| Focused reference studies | [Configuration](research/configuration-reference.md), [configuration mutation](research/configuration-mutation-reference.md), [encoding width](research/encoding-width-reference.md), [observability](research/observability-reference.md), [task reads](research/scheduled-tasks-reference.md), [task mutation](research/scheduled-tasks-mutation-reference.md) |
| Playback research | [HLS](research/hls-reference.md), [audio](research/audio-reference.md), [audio profiles](research/audio-profile-reference.md), [progressive video](research/video-progressive-reference.md), [copy-seek diagnostics](research/video-copy-seek/README.md), [WebSockets](research/websocket-reference.md) |

Research classifications such as planned, deferred and excluded describe their
captured inventory, not today's implementation status. The catalog, models and
JSON snapshots can be read offline. Their sample server addresses and incomplete
upstream schema metadata are research inputs, not deployment settings or Goby's
published API specification.

## Optional future work

The selected functional and OCI scopes have no remaining completion gate.
Further work needs its own chosen scope; the following does not reopen those
accepted deliveries:

- Production installation and operations for a real deployment, and registry
  publication if selected. The completed archive/Compose deliveries do not claim
  that either has occurred.
- Project licensing and final distribution notices. The project license remains
  undecided; [third-party notices](../THIRD_PARTY_NOTICES.md) describe the retained
  dependency material without granting a complete public-distribution claim.
- Provider-specific online acceptance, including the selected adapters' actual
  external services, beyond their existing code and offline coverage.
- Additional GPU/driver profiles, native arm64 acceptance, and client/media
  combinations beyond the recorded software and AMD profiles.
- Historical strict capacity/SLO targets, the complete fault matrix, sustained
  overload and physical power-loss coverage, if those requirements are selected
  again. They are outside the revised Phase 3 functional closeout.
- Unselected features such as offline sync/download packages and broader
  recommendation or media-category work, as bounded by the
  [selected compatibility plan](planning/selected-compatibility-plan-20260920.md).

Live TV/EPG/DVR/tuners, DLNA, external channels, group playback, a consumer Web
player and Emby cloud services remain explicitly excluded from the selected
scope. They are not unfinished implementation obligations for these deliveries.
