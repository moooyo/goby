# Linux amd64 OCI delivery

For installation, start with the [Docker quick start](QUICKSTART.md). The
operations helper provides `prepare`, `check`, `start`, `status`, `logs` and
`stop`; [current-release.json](current-release.json) is the shared catalog for
the current software and AMD image/archive identities. The
[BIF automation and intro assessment result](../../docs/development/bif-intro-expansion-20260930.md)
and [result manifest](../../docs/development/bif-intro-expansion-results-20260930.json)
record the updated application, focused checks and actual automatic preview workflow.
Git integration is recorded separately. The earlier
[operations result](../../docs/development/docker-operations-20260930.md) retains
its 14 helper tests, original software journey, failed attempts and closure.

The separate small toolkit is
`D:/Code/goby/.artifacts/bif-intro-20260930/goby-docker-operations.zip`.
The software image is in
`D:/Code/goby/.artifacts/bif-intro-20260930/software`; application source
is `33445db2e2e64b6871116332c44605261a1bf2d4`, with schema 52. The toolkit does not
include another copy of the image archive. The manual configuration, source-build
and recovery details below remain reference material for the selected profile.
The current software image is
`sha256:45dc7d9ff3eefbe79f6c8205ce2fda332777d1c1f2ecc22ef491fe7ab51bf89d`.
It includes the integrated Material 3 dashboard. Both final profiles passed actual
administrator UI and BIF-preservation checks. Owned containers, networks and
database clients are absent; private PostgreSQL is stopped and unrelated services
are unchanged. Earlier `0014bef` images are not the current installation target.
Further TMDB/OpenSubtitles work and new scraper research are deferred; provider
credentials are not required by this installation path.

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

## Build the archive

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

The October 1 Intro Skipper recipe update explicitly enables FFmpeg's
`chromaprint` muxer with `libchromaprint-dev` at build time and
`libchromaprint1` at runtime, both from the fixed Debian snapshot. This library
is separate from the retained helper's vendored Chromaprint 1.6.1. New builds
record the library version, copyright notice, muxer options and a short raw
fingerprint in `/usr/share/goby/toolchain`. The updated image has not been built
or accepted by this source change; previous delivery results remain historical.
Availability gates do not establish fingerprint parity with a reference build.

Enable **Automatic intro detection** in each desired TV library. Existing
libraries remain off after the schema 51 upgrade until that setting is enabled.
The server processes enabled libraries in the background, including after a
successful scan, and makes qualified intervals available for playback. No review
or manual-correction step is required; unmatched episodes play unchanged. Open
Tasks for progress, failures and stop controls. The unchanged detector needs at
least three independent episodes and examines the first 600 seconds.

Enable **Automatic seek previews** in a Movies, TV shows or Mixed media library.
The schema 52 `EnablePreviewGeneration` option defaults to false for existing
libraries. Enablement, successful scans and preview-profile changes request
background generation; the default schedule combines daily work and request
events. Tasks shows progress, failures and stop controls. No manual selection,
build or Force step is required. Disabling the option retains existing valid
previews. The default interval is still 10 seconds and BIF delivery is unchanged.

The broader intro assessment did not establish broader recognition support:
all 12 reviewed positive cases were missed, while three NASA short-ident negative
cases produced no false positives. Detector v3 and its thresholds are unchanged.
Short intros and differing audio/video versions still need recognition work;
the earlier The Big Picture acceptance remains limited to its original scope.
Optional source-media rewrite and OCR are disabled in this default read-only
profile. GPU devices, Vulkan/libplacebo Dolby Vision processing, other container
runtimes and arm64 are outside this delivery's acceptance scope.

## Update and rollback

1. Verify and load the next archive before changing the running installation.
   Retain the previous archive, its immutable image ID, and deployment config.
2. Stop Goby gracefully. Take a consistent PostgreSQL backup and a matching copy
   of the complete state directory, especially `application-key-master.key`.
   Retain the private environment file; treat these backups as secrets.
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
