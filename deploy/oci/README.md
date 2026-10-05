# Linux amd64 OCI delivery

This guide is also shipped in the small operations toolkit. Its installation,
configuration and upgrade steps use the supplied companion files. Links into
`docs/` or `web/` and source-build scripts refer to the matching source checkout;
those extended API, research and development documents are not copied into the
toolkit. The toolkit contains no image archives.

For installation, start with the [Docker quick start](QUICKSTART.md). The
operations helper provides `prepare`, `check`, `start`, `status`, `logs` and
`stop`. `prepare --with-player` selects the independent nginx player;
`--writable-media` adds source-side generation access. The
[release catalog](current-release.json) binds accepted image/archive identities.
Release `2026-10-05-player-media` has completed archive, integrated container/GPU,
player, persistence, schema-upgrade and backup-based rollback acceptance.

The current software image is
`sha256:f61d774ca909094d917b3c4e9a806397718fafc20e9fe6e73800335dc6e42d2c`,
at application source `7aaaeed44526848737270089ab0227d2d410f864`, schema 61.
Its archive directory is
`C:/Users/moooyo/.codex/worktrees/165c/goby/.artifacts/player-release-20261005/delivery/software`.
The [October 5 release record](../../docs/development/player-release-20261005.md)
binds the backend, [AMD profile](README.amd.md), [player image](README.player.md),
toolchain and acceptance status. The operations toolkit accompanies these
archives and does not include another image copy. The administrator dashboard
remains embedded in the Go executable; the player is built and deployed separately.

The September 30 schema-52
[BIF automation result](../../docs/development/bif-intro-expansion-20260930.md),
[manifest](../../docs/development/bif-intro-expansion-results-20260930.json), and
earlier [operations result](../../docs/development/docker-operations-20260930.md)
retain their original image, test and resource-closure evidence. The October 5
runtime results are recorded separately. Further TMDB/OpenSubtitles work
and new scraper research remain deferred; their credentials are not required
by this installation path.

The original software profile was verified within its selected scope. Its actual
build, archive import, media, encrypted recovery, and image/database upgrade and
rollback passed. Image identity, hashes, retained failures, and tested boundaries
are recorded in [the delivery result](../../docs/development/oci-delivery-20260929.md).

Effective **2026-09-30**, Docker Engine images are Goby's only supported
deployment form under the [Docker delivery policy](../../docs/planning/docker-delivery-policy.md).
The software and AMD variants are profiles of that same delivery form. Current
delivery uses an image archive and Compose; a registry is an optional distribution
channel. Standalone binary and systemd installation packages are unsupported and
are not future delivery work.

This delivery consists of a loadable image archive, its SHA-256 and build receipt,
`compose.yaml`, and `goby.env.example`. It runs on Linux amd64 Docker with Compose
support for `env_file.format: raw`. The selected profile uses a rootful engine
without user namespace remapping and container UID/GID `10001:10001`.
It uses an external PostgreSQL 17 server and does not publish to a registry.
PostgreSQL and the reverse proxy may run outside Docker; this policy applies to
the Goby application deployment.

The earlier [online-provider increment](../../docs/development/online-providers-20260930.md)
updated the application at source `76d64bf`. Its original image and source receipts
remain historical; use the current release catalog for installation. Its
[provider guide](README.providers.md) covers MusicBrainz, private provider
configuration and the optional writable-subtitle overlay. The original receipts
below retain their September 29 source and recovery evidence.

## Build the application archive

The October 5 release uses `scripts/build-oci-application.py` to layer a frozen
application onto an immutable accepted software or AMD media image. Supply the
release directory, exact `--revision`, matching `--source-archive` and
`--source-receipt`, immutable `--base-image`, `--profile`, fresh `--output-dir`
and local `--image` label. The source receipt must bind a `git archive` of that
revision; an arbitrary working-tree archive is rejected. The builder reads back
the resulting executable, tool hashes and analysis configuration without
starting the application. See the [release record](../../docs/development/player-release-20261005.md)
for the frozen inputs.

Both profiles keep the primary media FFmpeg/ffprobe and native fingerprint
helper pinned independently. The fingerprint pipeline uses Debian's separate
`/usr/bin/ffmpeg` 7.1.5 from the `20260915T000000Z` package snapshot. It does not
replace the primary playback or AMD processing FFmpeg. The image's
`/usr/share/goby/media-analysis.json` and `runtime-executables.sha256` bind the
selected paths and hashes. `--install-analysis-ffmpeg` installs that pinned
analysis executable when the selected base does not yet contain it.

Build the separate player with `scripts/build-player-oci.py` and digest-pinned
Node/nginx images as described in the [player release guide](README.player.md).
The Go release continues to embed only the administrator assets.

## Build the base media image

The following original recipe constructs the software media-tool baseline.
It is retained for reproducibility; the application-layer builder above is the
October 5 release path.

Run from a clean source checkout on the Linux build host. Use the project's Go
toolchain, Node 22.12 or newer, npm, Python 3, Docker and Buildx. Repository
verification runs on `test-env`. Use fresh output directories for each build.

```sh
revision="$(git rev-parse HEAD)"
output="$(mktemp -d /var/tmp/goby-oci-delivery.XXXXXXXX)"
npm --prefix web/admin ci --no-audit --no-fund
npm --prefix web/admin run build
go mod download
node scripts/build-release.mjs --arch amd64 \
  --output-dir "$output/release" \
  --frontend-contributions web/admin/.artifacts/frontend-contributions.json
python3 scripts/build-oci.py --release-dir "$output/release" \
  --output-dir "$output/image" --image goby:linux-amd64-local \
  --revision "$revision" --jobs 2
```

The release builder embeds the administrator UI and builds with `CGO_ENABLED=0`.
Its binary is an intermediate image-build input, not a supported standalone
installation package. Source builds and native execution remain available for
internal development and verification.
The image builder verifies the binary and manifest, builds its media tools, then
exports `goby-linux-amd64-image.tar`. It retains logs, `build-receipt.json`,
`image-id.txt`, `image-inspect.json`, and `SHA256SUMS`; it does not start Goby.
The frontend contribution report remains a private build sidecar.

The base is Debian trixie slim, fetched through `mirror.gcr.io` at the exact
manifest digest
`sha256:abc9cb88a5587630d7f915f47b23b0668fe250fbfc6457aa4d52b534c1bbf73f`.
APT uses the fixed `20260915T000000Z` snapshots. See
[source-pins.json](source-pins.json) for dependency inputs. The mirror changes
the fetch route, not the selected Debian content.

## Load and configure

Copy the image archive and companion files to the Docker host. Compare the
archive digest with the supplied delivery record, then load it from that directory:

```sh
sha256sum --check SHA256SUMS
docker image load --input goby-linux-amd64-image.tar
image_id="$(cat image-id.txt)"
docker image inspect "$image_id" --format '{{.Id}} {{.Os}}/{{.Architecture}} {{.Config.User}}'
```

Use the immutable `sha256:...` image ID recorded in the build receipt. The archive
is saved by image ID and need not create a tag when loaded. Compose disables pulls.

Provision a dedicated PostgreSQL 17 database and ordinary owner role. The role
must be able to migrate Goby's schema; do not use a database superuser. The database
hostname must be reachable from the container: `localhost` addresses the container
itself. PostgreSQL, its storage and its backup policy are external to this Compose
file. Image clients `pg_dump` and `pg_restore` use PostgreSQL major version 17.

For a new installation, create private writable directories and a private config:

```sh
sudo install -d -m 0700 /etc/goby
sudo install -m 0600 goby.env.example /etc/goby/goby.env
sudo install -d -m 0700 -o 10001 -g 10001 \
  /srv/goby/state /srv/goby/cache /srv/goby/logs
```

Edit `/etc/goby/goby.env` before startup. Set the database URL, public URL, server
name and an independent setup token of at least 24 random bytes. Keep the file
private and out of the image build context. For HTTPS, retain secure cookies and
set only the actual reverse proxy CIDRs. An isolated direct HTTP installation
needs its own HTTP public URL and `GOBY_COOKIE_SECURE=false`.

The existing media directory must be readable and traversable by UID/GID 10001.
The container mounts it read-only. Keep state, cache and logs separate from media;
do not recursively change ownership of an existing media collection.

The schema-61 images include [generated background previews](../../docs/api/background-previews.md),
[per-track audio waveforms](../../docs/api/audio-waveforms.md), and
[bitmap subtitle timelines](../../docs/api/subtitle-timelines.md).
Deployments can opt into the separate
[`compose.background-previews.yaml`](compose.background-previews.yaml) extension.
It changes only the existing `/media` bind to writable, using the same
`GOBY_MEDIA_DIR`; `compose.yaml` remains read-only by default. The container's
root filesystem remains read-only. Apply the extension after the base and any
AMD overlay, and retain it in subsequent Compose operations:

```powershell
docker compose --env-file deployment.env -f compose.yaml -f compose.background-previews.yaml up -d
```

Generated MP4s and their manifest live beside each original source under
`backdrops/goby/<source-filename-sha256>/`; waveform files use the independent
`backdrops/goby-waveforms/<source-filename-sha256>/` namespace, and subtitle
timelines use `backdrops/goby-subtitle-timelines/<source-filename-sha256>/`.
The key hashes the exact source basename, including its extension. The same
overlay enables writing all three namespaces. Approved source directories must
allow UID/GID `10001:10001` to create these trees. Their private directories use mode `0700`
and files use `0600`; their shared `backdrops` parent uses `0755` when created
by Goby. Arrange targeted host
permissions or ACLs without recursively changing the collection's ownership.
Enabling the library option alone cannot make a read-only or unwritable mount
writable. None of the generation options is enabled automatically by the overlay.

These sidecars are persistent media assets: ordinary scans, profile/source
changes, disabling generation, and cache maintenance do not delete or replace
them. Only explicit Force regeneration can replace an existing artifact; a
failed or canceled attempt preserves its previous publication. Include the
sidecars when copying or backing up the media collection. Database/state
backups are not a backup of these files. Schema-57 backup/restore preserves the
profile, manual starts, queue, and request receipts without clearing or
overwriting source-side MP4/manifest/ownership files; restored execution checks
current authorization again.

Schema 58 adds waveform queue/request receipts to database backup/restore,
without including or modifying source-side waveform files. Waveforms retain
their files after source changes but stop serving an obsolete source timeline;
this differs from background clips, whose older publication remains playable.
An explicit Force regeneration replaces waveform data for the current source.
Enable `LibraryOptions.EnableAudioWaveformGeneration` independently from the
background-video option, or explicitly queue an item through its administrator
controls. Both options default to false.

Schema 60 adds subtitle timeline queue/request receipts and the independent
`LibraryOptions.EnableSubtitleTimelineGeneration` option, also defaulting to
false. Embedded PGS/DVD extraction uses the pinned FFprobe 9.0.1 and the bitmap
decoder on Linux without OCR, Tesseract, or a GPU. Manual item generation does
not require enabling automatic generation. The administrator task key is
`media.subtitle_timeline_generation`; runtime availability and item failures
are visible in the administrator media-analysis UI. External SUP and IDX+SUB
timelines are not implemented.

The source-side namespace contains `.owner.json`, `manifest.json`, and a GSTL
generation file. Ordinary requests reuse retained files, including stale ones;
stale intervals are not served against a changed source clock. Explicit Force
publishes a replacement only after every admitted track succeeds. The player
omits subtitle labels and lanes without valid nonempty data. Schema-60 database
backup/restore does not include, rebuild, or delete these files, so media backups
must include this namespace too. See the
[subtitle timeline contract](../../docs/api/subtitle-timelines.md) for bounds,
source checks, and recovery semantics.

The isolated subtitle timeline Docker journey passed all seven phases on
`test-env`, including a real directory permission failure, retained files,
stale-source hiding, and explicit replacement. Desktop/mobile and administrator
views passed against the same deployment. The
[acceptance record](../../web/player/ACCEPTANCE.md) keeps the initial missing
state and first successful generation separate from the complete final run,
which reused the already published artifact before testing new Force operations.

The isolated `test-env` Docker acceptance passed all 12 background-preview
phases, including permission failure, cancellation, restart reuse, and explicit
replacement. Actual FFmpeg 9.0.1 SDR/HDR10/HLG and configured 1080p checks and
PostgreSQL 17 recovery passed within the
[recorded scope](../../web/player/ACCEPTANCE.md). This source-build feature does
not change the image/archive identities in the historical release catalog or
claim a production deployment.

Create `deployment.env` beside `compose.yaml`, filling in the actual image ID and
paths. This file supplies Compose variables; the separate `goby.env` supplies
application secrets and settings.

```dotenv
GOBY_OCI_IMAGE=sha256:REPLACE_WITH_RECORDED_IMAGE_ID
GOBY_CONFIG_FILE=/etc/goby/goby.env
GOBY_HOST_PORT=8096
GOBY_STATE_DIR=/srv/goby/state
GOBY_CACHE_DIR=/srv/goby/cache
GOBY_LOG_DIR_HOST=/srv/goby/logs
GOBY_MEDIA_DIR=/srv/media
GOBY_CPU_LIMIT=2.0
GOBY_MEMORY_LIMIT=2g
GOBY_PID_LIMIT=256
```

Run Compose as an account able to read the private application config:

```sh
docker compose --env-file deployment.env -f compose.yaml config --quiet
docker compose --env-file deployment.env -f compose.yaml up -d
docker compose --env-file deployment.env -f compose.yaml ps
docker compose --env-file deployment.env -f compose.yaml logs --tail 100 goby
```

The host port binds to loopback. Open `/admin/` through the configured reverse
proxy, or through the selected local HTTP port, and complete administrator setup.
Check that the expected library is visible and play a known media item. The
container has a read-only root filesystem and persists state, caches and logs in
the three host directories. It uses `restart: "no"`; startup after host reboot
is an explicit operator action for this profile.

Stop gracefully with:

```sh
docker compose --env-file deployment.env -f compose.yaml stop
```

## Included media capabilities

FFmpeg/ffprobe 9.0.1 include software H.264 (`libx264`), HEVC (`libx265`), AV1
(`libaom-av1`), `zscale`/`tonemap`, and subtitle rendering. The image also contains
the native Chromaprint intro helper and its installed notices. Its exact SHA-256
is recorded in `/usr/share/goby/media-analysis.json`; Compose enables that file
and stores analysis artifacts under `/var/cache/goby/analysis`.

The current image config selects the separate pinned `/usr/bin/ffmpeg` for
Intro Skipper's Chromaprint extraction, as described above. Its dependencies
are separate from the retained helper's vendored Chromaprint 1.6.1. The earlier
October 1 base-recipe change also added a Chromaprint-capable primary FFmpeg
build option; the October 5 application layer instead records its actual
inherited tools and separate distribution analysis executable. Availability
gates alone do not establish fingerprint parity with a reference build.

Enable **Automatic intro detection** in each desired TV library. Existing
libraries remain off after the schema 51 upgrade until that setting is enabled.
The server processes enabled libraries in the background, including after a
successful scan, and makes qualified intervals available for playback. No review
or manual-correction step is required; unmatched episodes play unchanged. Open
Tasks for progress, failures and stop controls. The selected Intro Skipper
engine uses the seven `Profile.IntroSkipper` options, including the configured
analysis percentage and length limit; see the
[media-analysis contract](../../docs/api/media-analysis.md).

Enable **Automatic seek previews** in a Movies, TV shows or Mixed media library.
The schema 52 `EnablePreviewGeneration` option defaults to false for existing
libraries. Enablement, successful scans and preview-profile changes request
background generation; the default schedule combines daily work and request
events. Tasks shows progress, failures and stop controls. No manual selection,
build or Force step is required. Disabling the option retains existing valid
previews. The default interval is still 10 seconds and BIF delivery is unchanged.

The historical September 30 intro assessment did not establish broader recognition support:
all 12 reviewed positive cases were missed, while three NASA short-ident negative
cases produced no false positives. Detector v3 and its thresholds were unchanged
in that assessment; it is not a test of the current Intro Skipper engine.
Short intros and differing audio/video versions still need recognition work;
the earlier The Big Picture acceptance remains limited to its original scope.
Optional source-media rewrite and OCR are disabled in this default read-only
profile. GPU devices, Vulkan/libplacebo Dolby Vision processing, other container
runtimes and arm64 are outside this delivery's acceptance scope.

## Update and rollback

The schema-61 images include [automatic credits analysis](../../docs/api/credits-markers.md)
for movie, TV, and mixed libraries through `EnableCreditsDetection`, defaulting
to false, and the `media.credits_analysis` task. Its separate step-5 images
passed the seven-phase Docker detector journey and focused regressions. The
earlier results retain their own image identities. It reuses the
selected Intro Skipper project, retains manual/source marker authority, and
withdraws invalid automatic publication after source/support/policy changes.
Enabling analysis does not grant source-media write permission. Accuracy is
bounded by the recorded corpus; see the observed early audio boundaries in
[ACCEPTANCE.md](../../web/player/ACCEPTANCE.md).

1. Verify and load the next archive before changing the running installation.
   Retain the previous archive, its immutable image ID, and deployment config.
2. Stop Goby gracefully. Take a consistent PostgreSQL backup and a matching copy
   of the complete state directory, especially `application-key-master.key`.
   Retain the private environment file; treat these backups as secrets.
   Keep the existing state mounted at its original path during an ordinary
   image upgrade. Ownership metadata has inode/path fences; copying it into a
   new directory is not a supported migration. Use the supported restore
   workflow for relocation rather than bypassing that protection.
3. Change only `GOBY_OCI_IMAGE` to the new recorded image ID. Retain the same state,
   cache, log and media mounts. Run `config --quiet`, then `up -d`, and check login,
   library visibility and playback. If the old release used an earlier media
   probe format, run a library scan with `ForceProbe` to refresh the snapshots.
   The tested schema 29 upgrade needs this step for probe version 6 to 8;
   original item IDs and user state remain preserved.
   Schema 51 leaves the new intro-detection option off for existing libraries;
   enable it in the desired TV library settings after upgrading.
   Schema 52 similarly leaves automatic seek previews off until enabled for
   a Movies, TV shows or Mixed media library; existing intro settings are retained.
   The later background, waveform, subtitle-timeline and credits options also
   default to off. Schema migration does not itself request their generation.
4. To roll back after a schema change, stop the new container and restore the
   matching pre-update database and state/master-key backup before selecting the
   previous image. Switching an old binary onto a newly migrated database is not
   a general rollback method. Preserve failed-update data for diagnosis.

Full application-managed restore also needs its separately provisioned target
database and `GOBY_RECOVERY_DATABASE_URL`; it is not created by Compose. Do not
remove persistent directories or use volume-deleting cleanup as part of an update.
Application-managed restore revokes imported credentials. Sign in again and
issue replacement application keys after recovery. The master-key file is
created lazily when the first application key is issued; preserve it once present.

FFmpeg source archives, local patches, build records and dependency package
versions are retained under `/usr/share/goby/toolchain`; helper notices are under
`/opt/goby-intro-fingerprint/share/goby-intro-fingerprint`. The collected
application notices and original texts are under `/usr/share/doc/goby`.
Goby's project license
is still undecided, and this internal delivery does not claim complete public
distribution licensing. No registry publication is part of this workflow.
