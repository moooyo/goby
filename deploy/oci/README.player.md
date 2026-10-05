# Standalone player release

This guide is also shipped in the small operations toolkit. Loading and
deployment use its companion files and the separately supplied player archive.
The source-build script and any repository-relative development links require
the matching source checkout; the toolkit contains no image archives.

The React/Vite player is a separate nginx image. It is never embedded in the
Go binary or in the administrator bundle. The backend, AMD media toolchain,
and player can be pinned to separate immutable image identities in the same
deployment. The runtime uses UID/GID `101:101` and listens on port `8080`.

## Build and export

Run on a Linux Docker host. Repository verification runs on `test-env`. Select
the exact release source revision and digest-pinned Node/nginx base images.
Use the commit timestamp as `SOURCE_DATE_EPOCH`; do not substitute a mutable
image tag for a digest. A fresh output directory is required.

```sh
python3 scripts/build-player-oci.py \
  --output-dir /srv/goby-release/player \
  --image goby-player:release \
  --revision "$RELEASE_REVISION" \
  --source-date-epoch "$RELEASE_EPOCH" \
  --node-image "$NODE_IMAGE_WITH_DIGEST" \
  --nginx-image "$NGINX_IMAGE_WITH_DIGEST"
```

The builder freezes an explicit set of player inputs, runs `npm ci` against
the lockfile in the pinned Node image, type-checks and builds Vite, then exports
the final image by immutable ID. Existing `dist`, `node_modules`, `.env`, test
fixtures, and browser credentials are excluded from the build context.
`--prepare-only` freezes and records the inputs without calling Docker.

Outputs include:

- `goby-player-linux-amd64-image.tar`, loadable with `docker image load`.
- `build-receipt.json`, binding source revision and inventory, base digests,
  fixed timestamp, final image ID, archive SHA-256, and companion hashes.
- `player-bundle-manifest.json`, recording the served assets, actual HTML
  module/style references, and retained package license texts.
- `player-dist/` and `player-notices/`, read back from a never-started container
  created from that immutable image. The container is removed after readback.
- `SHA256SUMS`, `compose.player.yaml`, and this guide.

The image retains installed production-package MIT/OFL and other license
texts under `/usr/share/doc/goby-player`, including external-application-mark
attribution. It does not include npm build tools or `node_modules` in the
served web root. This inventory does not change the repository's existing
public-distribution or project-license decisions.

Pinned inputs and a fixed timestamp make the build recipe repeatable. The
recorded archive hash identifies that export; Docker save implementation and
builder-version differences can still change archive bytes. A build receipt
does not claim runtime, GPU, upgrade, or browser acceptance.

## Load and deploy

Verify the archive and companions before loading them:

```sh
cd /srv/goby-release/player
sha256sum --check SHA256SUMS
docker image load --input goby-player-linux-amd64-image.tar
```

Use the `imageId` in `build-receipt.json` as `GOBY_PLAYER_IMAGE`; the local build
tag is a convenience, not the deployment identity. The release catalog's
optional `player` entry binds the same image ID and archive. The deployment
helper can prepare the backend and player together using `--with-player` and
`--player-release-dir` when their archives are in separate directories.

Apply `compose.player.yaml` after the backend Compose file and any AMD/media
write overlays. `GOBY_PLAYER_API_UPSTREAM` defaults to `http://goby:8096`.
The overlay requires an explicit `GOBY_PLAYER_HOST_PORT`; the deployment
helper's `prepare` command defaults that value to `8080`. The host binding is
loopback-only. The player proxies
`/emby`, `/embywebsocket`, and `/admin` to the backend. Its health endpoint is
`/healthz`. The release overlay never rebuilds or pulls an image during start.

For an explicit development image build, use `docker build` with
`web/player/Dockerfile` and `web/player` as its context. Release acceptance
must use the exported image ID and release receipt instead.
