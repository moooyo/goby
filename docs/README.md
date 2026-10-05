# Documentation guide

## Current status

The [October 5 player and media-analysis release](development/player-release-20261005.md)
packages application source `7aaaeed44526848737270089ab0227d2d410f864`, schema 61,
as separate software, AMD and player image archives. The administrator UI stays
embedded; the consumer player stays independently built and deployed.
Release `2026-10-05-player-media` passed the ten-phase software upgrade/rollback
journey, twelve-phase AMD task/GPU/player journey, produced-clip color checks
and owned-resource closure. The release record keeps those exact-image results
separate from the earlier feature tests below. Start with the [quick start](../deploy/oci/QUICKSTART.md),
[player archive guide](../deploy/oci/README.player.md) and
[release catalog](../deploy/oci/current-release.json) for installation.

The October 4, 2026 user decision selects Goby's own consumer player and
supersedes its previous exclusion. The [standalone React + Vite player](../web/player/README.md)
builds and deploys separately from the Go executable and embedded administrator
dashboard. Its optional Compose service uses the existing backend APIs. The
[backend capability assessment](../web/player/BACKEND-CAPABILITIES.md) identifies
handoff features without server support. Existing backend release receipts
below retain their original scope; the October 5 release is the first combined
release record to bind a separate player archive.

The subsequent [persistent background-preview increment](api/background-previews.md)
passed its 12-phase real Docker acceptance, focused browser/task/media checks,
and PostgreSQL 17 recovery on `test-env`. Generated clips remain beside their
source until explicit regeneration. The [player acceptance record](../web/player/ACCEPTANCE.md)
keeps these results separate from the earlier player and backend receipts.
Mixed name search with separate year/video-quality filters and existing
4K/HDR/HLG/Dolby Vision filtering are the accepted scope. Unified cross-field
free-text search and exact HDR10+ classification are not pending requirements.
The subsequent approved increment implements estimated content hours, source-side
per-track waveforms, and bounded Profile 8.1/complete Profile 7 MEL background
generation. Waveform/HTTP/browser/recovery checks and the authorized CT104 DV
checks have separate receipts; the live waveform journey passed all eight phases
and is recorded in the player acceptance document. Exact elapsed-time analytics remains optional.
The October 5 DV extension implements Profile 5, Profile 8.4, and Profile 8.2
through the same strict path. All 21 required native AMD media cases passed,
alongside the remote focused suite and Go backend build. Profile 8.2 coverage
uses original analytic material; Profile 7 FEL reconstruction remains deferred.
This receipt does not establish a new Docker deployment. The
[DV implementation record](development/dolby-vision-background-research.md)
separates the new corpus, implementation fixes, and results from the October 4
acceptance.
Automatic movie/TV credits detection now has its own completed seven-phase
real Docker acceptance and focused checks. It reuses the previously selected
Intro Skipper project, defaults off per library, and publishes source-bound
intervals without replacing manual/source authority. The observed audio
boundaries were about 3.496 seconds early; accepted integration is not universal
or exact-second recognition. See [credits markers](api/credits-markers.md) and
the [player acceptance record](../web/player/ACCEPTANCE.md).

The renewed handoff audit identified further frontend omissions already
supportable by existing Goby APIs. The restored interactions and presentation
passed the player build, 101 distinct automated checks, staged visual comparison,
and read-only real-service walkthrough. This work covers
hover/seek previews, independent filters, subtitle/media details, stage behavior,
recent-Series grouping, existing-preview audio, and external-player choices;
it is not a new backend implementation or a general external-app compatibility
claim. See the [restoration record](../web/player/ACCEPTANCE.md).

The selected [bitmap subtitle timeline increment](api/subtitle-timelines.md)
adds source-aligned display intervals for embedded PGS/DVD tracks. Generation
defaults off and stores permanent files beside the media source. Consumer rows
appear only for valid nonempty data, with labels and lanes omitted together;
diagnostics remain in the administrator/task UI. Focused media, task, HTTP,
browser, and schema-60 recovery checks passed on `test-env`, followed by all seven integrated
Docker phases and desktop/mobile visual acceptance. The initial missing-state
and first-generation evidence remains separate from the final run with an
existing artifact. The later schema-61 extension adds source-bound external SUP
and multilingual IDX+SUB timeline generation and passed its separate remote
regression and Docker acceptance. External bitmap playback delivery and burn-in
remain outside that timeline contract. Exact scope and retained failures are
recorded in the
[player acceptance record](../web/player/ACCEPTANCE.md).

The historical September 30 [BIF automation and intro assessment increment](development/bif-intro-expansion-20260930.md)
uses the dashboard-integrated application source
`33445db2e2e64b6871116332c44605261a1bf2d4`, schema 52.
BIF automation is complete within its scope. The expanded intro assessment is
complete, but broader recognition remains unaccepted and requires improvement.

Enable **Automatic seek previews** in a Movies, TV shows or Mixed media library.
The new option defaults to false. Enablement, successful scans and profile changes
request background generation, with daily/event defaults. Tasks retains progress,
failures and stop controls; no manual build/Force step is needed. Disabling
preview generation preserves existing valid outputs. The 10-second default and
BIF delivery protocol are unchanged.

The initial BIF scope recorded 168 focused backend checks, 13 Node tests and 33 UI passes, with one
historical fixture-dependent UI skip. The actual Docker generation, scan reuse,
10-to-20-second profile rebuild, disable and recreation journey passed.
Those pre-merge results retain their source scope. The
[result manifest](development/bif-intro-expansion-results-20260930.json) separately
binds the final dashboard integration and both then-current Docker profiles' actual
administrator UI and retained-BIF checks. Final closure passed: no owned
containers, networks or database clients remain, private PostgreSQL is stopped,
and unrelated services are unchanged. This is not a new full-product or GPU claim.

The expanded intro sample has 15 evaluable files from five series. All 12
visually/source-reviewed positive intros returned `no_result` and were missed;
three NASA short-ident negative cases had no false positives. N386's missing
audio is retained separately; N390 was frozen as its next natural replacement
before matching. Detector v3 and thresholds are unchanged. Short intros and
differing audio/video versions still need recognition improvements. The earlier
The Big Picture evidence remains valid only within its original scope.
Git integration is recorded separately.

The earlier [Docker operations toolkit](development/docker-operations-20260930.md)
retains its 14 helper tests, software operations journey and resource closure
on its original application. Revised Phase 3, intro/BIF and earlier image/tool
receipts remain historical evidence within their own source boundaries.

The [online-provider increment](development/online-providers-20260930.md)
accepts the selected real MusicBrainz album workflow and offline contracts.
The user has deferred TMDB/OpenSubtitles online work and new scraper research;
credentials are not a current blocker. MusicBrainz acceptance is unchanged. Use the
[Docker provider guide](../deploy/oci/README.providers.md) for configuration.

The [Docker delivery policy](planning/docker-delivery-policy.md) makes Docker
Engine images the only official delivery form. The current image archive and
Docker Compose workflow has software and AMD profiles. Native installation
packages and non-Docker runtimes are not supported alternatives or deferred
delivery obligations.

These deliveries do not establish a production deployment. Use the current
result record for owned-resource closure and retained evidence. Earlier documents may
contain then-current schema numbers, worker identities, incomplete milestone
labels, or instructions to resume a campaign. Those are historical checkpoints,
not instructions to reopen completed work.

Start with these sources of current status and delivery policy:

| Document | Read it for |
| --- | --- |
| [October 5 player release](development/player-release-20261005.md) | Frozen application, schema, three image archives, analysis-tool binding and integrated acceptance status |
| [Current status](development/current-status.md) | Completed implementation and acceptance, supported profiles, and deployment boundaries |
| [Development handoff](development/handoff.md) | Latest delivery, retained artifacts, closed resources, and continuation context |
| [Current execution plan](planning/current-execution-plan.md) | Completed selected scopes and the distinction between current work and historical plans |
| [Docker delivery policy](planning/docker-delivery-policy.md) | The single official delivery form, current Docker profiles, and unsupported installation forms |

## Deployment and use

Use the [Docker quick start](../deploy/oci/QUICKSTART.md) as the recommended
installation entry. `goby-docker.py` exposes `prepare`, `check`, `start`, `status`,
`logs` and `stop`; [current-release.json](../deploy/oci/current-release.json)
binds both backend profile archives and the optional player archive to immutable
image IDs. Software
and AMD are profiles of one Docker image delivery form. Image acceptance is
specific to its recorded platform, tools and hardware; an archive or cross-build
alone does not establish another platform's support.

| Document | Read it for |
| --- | --- |
| [Docker quick start](../deploy/oci/QUICKSTART.md) | Prepare an installation and use the installed helper for checking, starting, diagnosing and stopping it |
| [Current Docker release catalog](../deploy/oci/current-release.json) | Accepted software/AMD/player image IDs, archive identities and companion hashes |
| [Software OCI operator guide](../deploy/oci/README.md) | Import the Linux amd64 image archive, configure external PostgreSQL, start Compose, and perform update/rollback |
| [AMD OCI operator guide](../deploy/oci/README.amd.md) | Import the AMD image and apply the GPU Compose and seccomp companions for the accepted hardware profile |
| [Standalone player archive guide](../deploy/oci/README.player.md) | Build, verify and load the independent nginx player archive with pinned source and image inputs |
| [Standalone Goby player](../web/player/README.md) | Develop and build the React + Vite player and add its independent nginx service with the optional Compose extension |
| [Transcoding configuration](development/transcoding-configuration.md) | Media tools, hardware selection, runtime limits and cache configuration |
| [Application-key operations](development/application-keys.md) | Persistent master-key ownership and restoring a database with its matching master key |
| [Native backup and recovery](development/backup-recovery.md) | Encrypted archives, durable operations, offline recovery and generation switching |
| [Media-analysis runtime](development/media-analysis-runtime.md) | Native intro helper, analysis inventory, cache and execution lifecycle |
| [Media-analysis administrator UI](development/media-analysis-native-ui.md) | Library intro/preview automation, generated outputs, task progress and errors |
| [Media-analysis recovery](development/media-analysis-recovery.md) | Analysis state and artifact behavior during backup, restore and restart |

The OCI deliveries include an embedded React/MUI administrator dashboard,
FFmpeg/ffprobe and PostgreSQL client tools. The PostgreSQL server, media,
application secrets and reverse proxy are external. The administrator dashboard
remains for administration; Goby's separate player provides the consumer UI.
The Docker-only policy does not change
how the external PostgreSQL server or reverse proxy may be deployed. Executables,
recovery commands and native helpers included in the image are components of
that image, not additional delivery forms. Consult the selected Docker guide
before changing the media write policy. The toolkit ZIP is
`D:/Code/goby/.artifacts/bif-intro-20260930/goby-docker-operations.zip`;
it is used with the matching current image archive from
`D:/Code/goby/.artifacts/bif-intro-20260930/software` or `amd`.
The small toolkit does not duplicate those archives.

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
| Goby consumer player | [Player build and deployment](../web/player/README.md), [backend capability assessment](../web/player/BACKEND-CAPABILITIES.md), [acceptance](../web/player/ACCEPTANCE.md), [search hints](api/search-hints.md), [video filters](api/video-catalog-filters.md), [credits markers](api/credits-markers.md), [persistent background previews](api/background-previews.md), [audio waveforms](api/audio-waveforms.md), [bitmap subtitle timelines](api/subtitle-timelines.md), [estimated viewing statistics](api/viewing-statistics.md) |
| Player research and implementation follow-up | [Dolby Vision background generation](development/dolby-vision-background-research.md), [per-track audio waveforms](development/audio-waveform-research.md); original source/documentation research retained separately from later approved implementation and runtime evidence |
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

## Development

The [build and run guide](development/running.md) documents source-based
development, configuration and migrations. The
[toolchain and hardware policy](development/toolchain.md) records build inputs
and profile-specific requirements. Native builds and fixture processes are
development tools; they do not define a supported binary or native-package
installation path.

## Acceptance records and history

Use the current closeouts for completion claims and exact artifact identities.
The result manifests retain the detailed counts, hashes, failed attempts,
repeats and exclusions; this guide does not combine counts across different
sources or interpret historical research totals as current test coverage.

| Scope | Record and evidence |
| --- | --- |
| Automatic BIF previews | [Current result](development/bif-intro-expansion-20260930.md) and [manifest](development/bif-intro-expansion-results-20260930.json); completed library-driven generation, reuse, configuration rebuild and retention scope |
| Expanded intro recognition | The same record preserves the completed assessment and all 12 missed positive cases; recognition extension is not accepted and short/variant intros need improvement |
| Automatic TV-library intros | [Automation result](development/library-intro-automation-20260930.md) and [manifest](development/library-intro-automation-results-20260930.json); library opt-in, background results, automatic scan follow-up, disable/restart behavior and focused UI/task checks |
| Docker installation and operations | [Operations result](development/docker-operations-20260930.md); 14 helper tests, composed software runtime journey, retained failures and resource closure; Git integration is recorded separately |
| Credential-free online providers | [Provider result](development/online-providers-20260930.md); accepted MusicBrainz album flow and offline/configuration scope at source `76d64bf`; original image receipts are preserved |
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
| Native installation and packaging history | [Linux systemd installation](../deploy/linux/INSTALL.md) and its dated package evidence are retained history; native packages are no longer a supported or deferred delivery form |
| Original research inventory | [Source provenance](sources/README.md), [API catalog](api/catalog.md), [machine-readable inventory](api/inventory.json), [data models](api/models.md) |
| Reference-server observations | [Reference baseline](research/reference-server.md), [client compatibility](research/client-compatibility.md), [playback and transcoding](research/playback-and-transcoding.md), [administrator/Linux study](research/admin-dashboard-and-linux.md) |
| Focused reference studies | [Configuration](research/configuration-reference.md), [configuration mutation](research/configuration-mutation-reference.md), [encoding width](research/encoding-width-reference.md), [observability](research/observability-reference.md), [task reads](research/scheduled-tasks-reference.md), [task mutation](research/scheduled-tasks-mutation-reference.md) |
| Playback research | [HLS](research/hls-reference.md), [audio](research/audio-reference.md), [audio profiles](research/audio-profile-reference.md), [progressive video](research/video-progressive-reference.md), [copy-seek diagnostics](research/video-copy-seek/README.md), [WebSockets](research/websocket-reference.md) |

Research classifications such as planned, deferred and excluded describe their
captured inventory, not today's implementation status. The catalog, models and
JSON snapshots can be read offline. Their sample server addresses and incomplete
upstream schema metadata are research inputs, not deployment settings or Goby's
published API specification.

## Remaining recognition work

BIF automation is complete, but recognition of short intros and differing
audio/video versions remains substantive unfinished work after the expanded
assessment. Git integration is recorded separately.

## Optional future work

The directions below remain separate from recognition work and do not reopen
older accepted deliveries:

- Production Docker deployment and operations, and registry publication if
  selected. A registry would distribute the same Docker images; it would not
  introduce another delivery form. The completed archive/Compose profiles do
  not claim that either has occurred.
- Project licensing and final distribution notices. The project license remains
  undecided; [third-party notices](../THIRD_PARTY_NOTICES.md) describe the retained
  dependency material without granting a complete public-distribution claim.
- TMDB/OpenSubtitles online acceptance and new scraper research only after the
  user explicitly resumes that deferred work. The accepted MusicBrainz profile
  remains unchanged; provider credentials do not block current operations.
- Additional Docker GPU/driver profiles, a separately selected Linux arm64
  Docker image profile, and client/media combinations beyond the recorded
  Linux amd64 software and AMD profiles.
- Historical strict capacity/SLO targets, the complete fault matrix, sustained
  overload and physical power-loss coverage, if those requirements are selected
  again. They are outside the revised Phase 3 functional closeout.
- Unselected features such as offline sync/download packages and broader
  recommendation or media-category work, as bounded by the
  [selected compatibility plan](planning/selected-compatibility-plan-20260920.md).

Live TV/EPG/DVR/tuners, DLNA, external channels, group playback, Emby's proprietary
web application/WebAppService and Emby cloud services remain explicitly excluded from the selected
scope. They are not unfinished implementation obligations for these deliveries.
Standalone binaries, systemd packages, DEB/RPM packages, Windows installers and
other native packages are also outside the delivery policy. They are not a
future-work queue, and non-Docker runtimes carry no support promise.
