# Goby

Goby is a Linux media server with an Emby-compatible API, implemented in Go with
PostgreSQL and FFmpeg. Its React + Material UI dashboard is for administration;
playback uses supported third-party clients. The project license has not yet
been selected.

The selected compatibility, media-analysis, resilience, and Linux amd64 OCI
deliveries are complete within their recorded acceptance boundaries. Start with
[current status](docs/development/current-status.md) and the
[current execution plan](docs/planning/current-execution-plan.md). Older dated
records preserve their original results and failures; they are not a cumulative
list of current tasks.

Goby's only official delivery form is a Docker image running on Docker Engine.
The current delivery is an importable image archive with Docker Compose;
software and AMD are profiles of that same delivery form. The
[Docker delivery policy](docs/planning/docker-delivery-policy.md) defines this
support boundary.

## Implemented features

| Area | Current selected scope |
| --- | --- |
| Library and accounts | Persistent media identities and user state, bounded scans, local NFO metadata, artwork, library permissions, account/device/session management, playlists and collections, music discovery, Search/Hints and NextUp. See the [implemented API surface](docs/api/implemented.md). |
| Playback | Authenticated direct and Range delivery, progressive and HLS remux/transcode, selected H.264/HEVC/AV1 outputs, TS/fMP4/packed-audio HLS, text and bitmap subtitle processing, and source-proven nonzero copy seeking. [Media contracts](docs/development/advanced-media.md) define the supported combinations. |
| Media analysis | Background episode-intro detection, administrator review and overrides, source-bound BIF seek previews, cancellation, cache management and restart recovery. [Media analysis](docs/api/media-analysis.md) and [seek previews](docs/api/seek-previews.md) define the contracts. Accuracy and consumer acceptance remain limited to the [recorded Phase 2 corpus and journeys](docs/development/media-analysis-resilience-phase2-20260921.md). |
| Administration | Users, libraries, metadata editing, activity/logs, scheduled tasks, managed CPU/AMD selection and encoding settings, notifications, and backup/recovery. [Managed execution settings](docs/api/managed-execution-settings.md) distinguish changes for new work from listener settings that require restart. |
| Recovery | Goby encrypted backups, restore planning, activation/rollback and an offline CLI available inside the image, plus accepted migration, process/database restart, selected storage-fault and isolated guest-recovery scenarios. See [backup and recovery](docs/development/backup-recovery.md) and the [functional closeout](docs/development/phase3-functional-closeout-20260929.md). |

AMD decode, encode and GPU processing have actual execution evidence for the
recorded devices, drivers and media combinations. Hardware and software fallback
retain their separate boundaries; an AMD result does not establish support for
every GPU or codec tuple. See the [AMD media contract](docs/development/amd-video-processing.md).

## Run Goby

Use Docker Engine and the Docker Compose guide for the selected Linux amd64
image profile. Both profiles include the embedded administrator dashboard and
use an external PostgreSQL 17 server.

- [Linux amd64 software image and Compose guide](deploy/oci/README.md): archive
  import, database configuration, persistent paths, startup, backup, upgrade and
  rollback. The [delivery record](docs/development/oci-delivery-20260929.md)
  binds the accepted image, tools and application source.
- [Linux amd64 AMD image and Compose extension](deploy/oci/README.amd.md): device
  selection, numeric render-group access, media settings and the recorded
  GFX1150/Mesa profile. The [AMD delivery record](docs/development/oci-amd-delivery-20260929.md)
  binds its actual GPU and production-container HTTP results.

Both OCI profiles passed archive loading and runtime acceptance. The software
profile also passed encrypted recovery, persistence, a schema 29-to-50 upgrade
and backup-based rollback. The AMD extension retains the same application bytes.
These archive deliveries do not establish registry publication or a production
deployment. A future registry would provide another distribution channel for
the same Docker images, not another delivery form. No registry publication has
occurred.

The application executable, recovery CLI, FFmpeg/ffprobe and native analysis
helper inside the image are image components, not standalone supported
deliverables. PostgreSQL and the reverse proxy remain external dependencies;
this policy does not prescribe their deployment method.

## Scope and future work

The [credential-free online-provider increment](docs/development/online-providers-20260930.md)
also accepts MusicBrainz album search, metadata application/refresh and Docker
restart persistence. The [provider guide](deploy/oci/README.providers.md) covers
configuration and optional subtitle write access. TMDB and OpenSubtitles have
offline contract coverage; their real online acceptance awaits credentials.

There is no remaining implementation or acceptance gate in the completed selected
scopes. Further work requires a separately selected delivery or feature scope:
credentialed TMDB/OpenSubtitles online acceptance, additional clients and media combinations,
other GPU/driver profiles within Docker, an additional Docker image architecture
such as Linux arm64, registry distribution, or production/public-HTTPS
deployment. Current acceptance remains Linux amd64. Provider adapters already
exist; only the recorded MusicBrainz online profile is currently accepted.

Standalone binaries, systemd installation packages, DEB/RPM packages, Windows
installers and other native packages are unsupported delivery forms, not
deferred work. Non-Docker runtimes are not promised. Native builds remain useful
for development and historical verification fixtures; their existence does not
create a supported installation option.

Historical strict capacity/SLO profiles, the complete old fault matrix and
physical power-loss durability were not accepted by the revised functional
closeout. Offline synchronization, general recommendations and game/book media
remain unselected. These boundaries do not reopen completed functional or OCI
journeys.

Live TV/EPG/DVR/tuners, DLNA, external channels, synchronized group playback,
a consumer web player, Emby Connect/cloud identity, Emby package installation and
proprietary binary-plugin compatibility are explicitly outside the selected
product scope. See the [implementation scope](docs/api/implementation-scope.md)
for the distinction between implemented, deferred and excluded capabilities.

## Documentation and licensing

The [documentation guide](docs/README.md), [API inventory](docs/api/implemented.md)
and [Linux architecture](docs/architecture/linux-go-react.md) provide detailed
contracts and source references. The official research baseline is the pinned
Emby SDK 4.9.5.0 export; its 535-operation inventory describes upstream contracts,
not a count of implemented or accepted Goby operations. Historical research and
deployment evidence remains available through [source provenance](docs/sources/README.md)
and the [handoff](docs/development/handoff.md).

The [development build and run guide](docs/development/running.md) and
[toolchain policy](docs/development/toolchain.md) support contributor work and
verification fixtures. They do not define additional official delivery forms.

Existing third-party texts and attribution are collected in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and its
[versioned inventory](docs/development/third-party-notices-inventory.json).
The OCI images include the recorded notices and original license texts. Goby's
project-license decision and complete public-distribution licensing remain open;
internal artifact acceptance does not settle either question.
