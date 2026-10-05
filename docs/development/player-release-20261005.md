# Player and persistent media-analysis Docker release — October 5, 2026

## Status and selected scope

Release `2026-10-05-player-media` is complete within the selected Docker delivery
scope. The software backend, AMD backend and standalone player images were
built and exported from the frozen application source below. The ten-phase software
upgrade, player-proxy, persistence and backup-based rollback journey, all twelve
integrated AMD phases, produced-clip color readback and owned-resource closure
passed. The [release catalog](../../deploy/oci/current-release.json) binds the
accepted images, archives and operations companions. Immutable build receipts
intentionally retain `runtimeAccepted: false`; they describe construction, while
this completed runtime record supplies the separate acceptance evidence.

This delivery packages the already implemented consumer player and media
analysis into Docker archives and Compose operations. The Go executable embeds
the React/MUI administrator UI. The React/Vite consumer player has its own nginx
image, assets, notices, port and lifecycle. It is not embedded in the backend.
The selected delivery remains Linux amd64 on Docker Engine, with an external
PostgreSQL 17 database. No registry publication or public deployment is included.

The release includes:

- The restored player design and accepted desktop scrolling behavior, background
  source priority, media-information timelines and existing playback APIs.
- Permanent source-side background clips, per-track audio waveforms, and embedded
  PGS/DVD plus external SUP/multilingual IDX+SUB subtitle timelines.
- Opt-in intro/credits detection and BIF seek previews through the existing task
  and library controls.
- Strict AMD Dolby Vision background conversion for P5, P8.1, P8.4, P8.2 and
  complete P7 MEL. Generated backgrounds remain silent H.264/BT.709 SDR clips.

The default background duration is 25 seconds. Generation stays disabled until
enabled or explicitly requested. Existing completed media sidecars persist
beside their source; only explicit Force regeneration replaces them. Cache
maintenance, scans, profile edits and disabling generation do not clear them.

## Frozen application and image identities

Application revision: `7aaaeed44526848737270089ab0227d2d410f864`.
Database schema: **61**.
Backend executable SHA-256:
`81d5ee066c3eca21c72f48ff87d5ec24601af3dcf9ec3374c8e7c69846c3c14b`.
Both backend profiles use that executable, including its administrator assets.
Documentation and operations closeout commits may follow this revision without
changing these immutable application-image identities.

| Component | Immutable image ID |
| --- | --- |
| Software backend | `sha256:f61d774ca909094d917b3c4e9a806397718fafc20e9fe6e73800335dc6e42d2c` |
| AMD backend | `sha256:6c94ffc9fd2936d075c0362025a6dde560a94fd810eceeb93248e10c8c0edf5c` |
| Standalone player | `sha256:7140ce5c531a302b0aca595ee47ce98c52cd08ed73d5a00e5a27972d1ad94192` |

The delivery root is
`C:/Users/moooyo/.codex/worktrees/165c/goby/.artifacts/player-release-20261005/delivery`.
Each component has its own directory, `build-receipt.json`, image inspection,
`image-id.txt` and `SHA256SUMS`. The companion files and
`goby-docker-operations.zip` at that root form the operations delivery;
the toolkit does not duplicate the image archives. The ZIP contains the Compose
and environment companions, helper, catalog, quick start and operator guides at
its top level. Extended API/research links in those guides refer to the matching
source checkout; it does not package the complete repository documentation.

| Directory and archive | Bytes | Archive SHA-256 |
| --- | ---: | --- |
| `software/goby-linux-amd64-image.tar` | 976,523,776 | `f94d71c6520bdc8bd63e09857d12f0bf83074b986d538e3a1300b0bff423f44a` |
| `amd/goby-linux-amd64-amd-image.tar` | 1,325,367,808 | `72bb66e2836a90d44e061158dfa34bd774ffa86b8de76525e068ad977e07a575` |
| `player/goby-player-linux-amd64-image.tar` | 68,103,168 | `1952fa675beaf9a44e34dc410a2a3df7f43c7d87a83da05434faffaa543dfae3` |

Image IDs identify the runtime images. Archive hashes identify these particular
exports; a Docker save or builder-version difference can change archive bytes
without being an application change. Install from the accepted catalog and
matching receipts, not a mutable local tag.

## Source and toolchain binding

`scripts/build-oci-application.py` requires the release output, exact revision,
an immutable base image, and a matching `git archive` plus source receipt.
It rejects a source archive whose content is not bound to that revision.
The frozen source archive is `source-03.tar`, 201,472,000 bytes, SHA-256
`15b42f0613ba4470f0bdd89310e371c666a2de18202e4508df93653f4ecfc9a1`.
`application-source.json` records that binding with the inherited tool hashes.
The final image executable and configuration are read back from the image,
rather than accepted from an inherited checksum file.

The primary FFmpeg/ffprobe, AMD private libraries and drivers retain their
existing pinned media-image provenance. A separate distribution FFmpeg serves
the native fingerprint pipeline:

| Analysis input | Pinned value |
| --- | --- |
| Fingerprint helper | `/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint` |
| Helper SHA-256 | `ddaf899d0bbaa98533907910453fb19e0dc573fdb618711de7855b956c27888c` |
| Analysis FFmpeg | `/usr/bin/ffmpeg`, Debian package `7:7.1.5-0+deb13u1` |
| APT snapshot | `20260915T000000Z` |
| Analysis FFmpeg SHA-256 | `e8a8d46f5225f3062cec7c07fb145d58ae73c603cb740dcd5bad34bfb54e455a` |

`/usr/share/goby/media-analysis.json` binds the helper and analysis executable;
`runtime-executables.sha256` records selected runtime binaries. This separate
analysis dependency does not replace the primary playback or strict AMD Dolby
Vision FFmpeg. Installing the tools does not enable any library feature.

`scripts/build-player-oci.py` freezes the selected player inputs, excludes
existing output, dependencies, local environment files and test fixtures, runs
the lockfile build in a digest-pinned Node image, then reads back served assets
and licenses from the immutable nginx image. Its receipt records:

- Node: `mirror.gcr.io/library/node@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1`.
- nginx: `mirror.gcr.io/nginxinc/nginx-unprivileged@sha256:0c79d56aee561a1d81c63f00eee5fb5fe29279560cdc55e91425133104c7fbe6`.
- `SOURCE_DATE_EPOCH=1791210049` from the selected commit.
- The asset and notice inventory in `player-bundle-manifest.json`, with retained
  `player-dist/` and `player-notices/` readbacks.

## Deployment and persistence

Use the [quick start](../../deploy/oci/QUICKSTART.md) and operations helper.
The backend-only path remains available. `prepare --with-player` adds the
independent player and accepts `--player-release-dir` when its archive is in
a separate directory. The helper defaults the player host port to `8080`;
manual Compose use must explicitly set `GOBY_PLAYER_HOST_PORT`. The host binding
is loopback-only, and nginx proxies the backend API on the private Compose
network. The player listens inside its container on port `8080` as UID/GID
`101:101`; the backend retains UID/GID `10001:10001`.

`--writable-media` adds `compose.background-previews.yaml` for source-side
generation. Without it, media remains read-only. With it, the host still needs
targeted permissions or ACLs allowing the backend to create the approved
sidecar trees; do not recursively change an existing collection's ownership.
All automatic generation remains opt-in. The container root filesystem remains
read-only and the state, cache and log volumes remain separate from media.

| Permanent sidecar | Source-side namespace |
| --- | --- |
| Background clips | `backdrops/goby/<source-filename-sha256>/` |
| Audio waveforms | `backdrops/goby-waveforms/<source-filename-sha256>/` |
| Bitmap subtitle intervals | `backdrops/goby-subtitle-timelines/<source-filename-sha256>/` |

The key includes the exact original filename and extension. Include these
directories when moving or backing up media. Database/state backups preserve
the analysis configuration, queues and receipts, but do not contain the source
media or its generated sidecars. A failed or canceled regeneration keeps the
previous publication. External bitmap timeline records remain excluded from
playback selection until an actual delivery/burn-in implementation is selected.

For an upgrade, retain the old image/catalog, back up the database and private
state, preserve the media and sidecars, verify the new archive hashes, and
recreate only the selected Compose services with the new immutable IDs. Keep
the existing state directory mounted at its original host path: Goby's state
ownership metadata includes identity fences, so a plain copy of those files
into a different directory is not an accepted state-migration procedure. A
backup copy is recovery material, not permission to bypass that fence; use the
supported restore workflow when relocation is needed. The
application migrates the database to schema 61. Do not point an older executable
at an already migrated database as a rollback strategy: restore the matching
pre-upgrade database/state backup before returning to its old image. Keep
generated source-side assets independently backed up.

## Verification status and limits

The software journey passed all ten phases on `test-env` against the recorded
software and player image IDs:

- The schema-52 installation upgraded to schema 61 with account, media-item ID,
  favorite, play-count and playback-progress state preserved. The new automatic
  generation switches stayed off and the upgrade did not enqueue generation.
- An explicit five-second start produced a 25-second, 600-frame silent H.264
  `yuv420p` background. Authenticated delivery and the player reverse proxy
  returned the published bytes.
- Helper stop/start preserved the generated video and manifest, their hashes
  and modification times. Existing published material was reused.
- A third isolated database restored the pre-upgrade schema-52 dump and ran the
  old image with the retained deployment/state identity. Login and user state
  passed, followed by another successful return to the schema-61 deployment.
- All five owned software verification containers stopped with exit code 0.
  Database dumps, state backups and private configuration remain on the remote
  verification host; they were not copied into the public delivery companions.

The original attempt to copy fenced state into a new directory was correctly
rejected. The accepted upgrade retained the original state mount and deployment
identity. This result does not authorize arbitrary metadata copying or pairing
a database bound to one deployment with fresh unrelated state. The private
Chinese software report is retained at
`.artifacts/player-release-20261005/software-upgrade/REVIEW.md`.

The first 45-second fixture used its default start of 30 seconds and correctly
produced the remaining 15 seconds. That observation is retained separately from
the explicit-start 25-second acceptance above. Software SDR output is recorded
as H.264/`yuv420p` without HDR metadata; the strict BT.709 contract belongs to the
separately tested Dolby Vision conversion path.

The integrated AMD run passed all twelve phases with exit code 0 against the
recorded AMD and player image IDs:

- P5, P8.4 and P8.2 each completed administrator-UI generation, observed GPU
  execution and 25 seconds of natural player playback.
- Automatic generation remained off; ordinary consumer reads did not create
  work. The selected Vulkan settings were checked against actual execution.
- Ordinary generation reused existing clips. A forced write failure preserved
  the previous publication. Observed GPU cancellation left no helper process.
- Restart preserved the three clips' bytes, hashes and modification times;
  subsequent ordinary requests reused them.

The raw run is `live-backgrounds-1791211185738`; the private Chinese summary is
retained at `.artifacts/player-release-20261005/amd-live/REVIEW.md`. Final
read-only color checks used the exact generated files published by this Docker
task journey; their results are retained in
`.artifacts/player-release-20261005/delivered-colors/results.json`:

| Check | Result |
| --- | --- |
| P5/P8.4 corresponding-scene RGB mean absolute error | 4.74414 |
| P5/P8.4 corresponding-scene luma correlation | 0.996525 |
| P8.2 independent scalar oracle, 16 center color patches | Mean error 0.336263; maximum 1.953125, within mean 3 / maximum 8 limits |

The P5/P8.4 check compares corresponding published scene content across profiles;
it is not an independent absolute-color oracle. The P8.2 analytic fixture has
its independent scalar oracle. These checks complement the prior native profile
evidence rather than claiming arbitrary-source color accuracy.

The existing [player acceptance](../../web/player/ACCEPTANCE.md) and
[DV implementation record](dolby-vision-background-research.md) establish their
specific feature checks. In particular, P5/P8.4/P8.2 passed 21 selected native
AMD media cases before this packaging increment. P5 and P8.4 include official
sample material; P8.2 uses original analytic SDR-plus-RPU fixtures. Those
receipts are not evidence that the new container journey has passed.

Owned-resource closure passed after the final readback:

- Both AMD application containers stopped with exit code 0, `OOMKilled=false`
  and PID 0, then were removed. The owned Compose network was removed.
- All three DV inputs, the original native fixtures and generated manifests/MP4s
  retained their hashes, modification times and inodes. No FFmpeg/ffprobe helper,
  open DRM descriptor or partial output remained; owned temporary storage was
  empty.
- The private Docker engine, SSH daemon and tunnel stopped. CT104 IP forwarding
  was restored to its original value of 0. The GPU configuration was unchanged.
- The owned PostgreSQL container stopped with exit code 0 and
  `OOMKilled=false`; its receipt is retained as `database-closed.json`. Private
  database/state backups remain on the verification host.

The CT104 disk expansion to 20 GiB and five added iptables dependency packages
remain as explicitly recorded infrastructure preparation. Earlier private-engine
setup failures remain in the evidence and did not change the application images.
Closure does not claim removal of retained fixtures, private backup material or
these documented worker preparations.

P7 FEL reconstruction, external SUP/IDX+SUB playback or burn-in, additional
GPU/driver combinations, installed third-party-player launch compatibility,
public registry distribution and broader recognition accuracy are not added by
this release. The source and license inventory remains available in
[third-party notices](../../THIRD_PARTY_NOTICES.md); internal artifact delivery
does not resolve Goby's project-license or public-distribution decisions.

The September 29/30 delivery, recovery, provider, BIF and intro reports retain
their original application and image identities. This release does not sum their
test counts into a new full-suite result or convert historical failures into
passing evidence.
