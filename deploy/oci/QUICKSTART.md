# Docker quick start

Use a matching release toolkit, `current-release.json`, and software or AMD image
archive. The catalog binds immutable image IDs, archive sizes and SHA-256 hashes,
application identity, and deployment companion hashes. The optional independent
React/Vite player has its own image/archive entry; it is not embedded in Go.
Do not mix companions or image archives from different releases.

## Prepare once

Use a Linux amd64 host with rootful Docker Engine, Docker Compose supporting
`env_file.format: raw`, and Python 3.10 or newer. Run the helper with `sudo`.
Provision external PostgreSQL 17 and an ordinary owner role first; the toolkit
does not install PostgreSQL. Its address must be reachable from Docker.
Existing media must be readable and traversable by backend UID/GID `10001:10001`.
The player runs separately as UID/GID `101:101` and never mounts the media.

Extract the operations toolkit into `/download/goby-docker-operations`. Place the
selected backend archive in `/download/profile`. Put the player archive in that
directory too, or use `--player-release-dir` to select a separate directory.
The helper prefers `current-release.json` in the backend release directory and
otherwise uses the one beside itself. It verifies every catalogued companion.

Store the one-line PostgreSQL URL in `/private/db-url`, with permissions `0600`
or stricter. Keep it out of command arguments, source control and captured output.
Choose a new installation directory, then run:

```sh
sudo python3 /download/goby-docker-operations/goby-docker.py prepare \
  --release-dir /download/profile --directory /srv/goby \
  --media-dir /srv/media --database-url-file /private/db-url \
  --public-url http://localhost:8080 --host-port 8096 --profile software \
  --with-player --player-host-port 8080
```

Omit `--with-player` and its options for a backend-only installation; old catalogs
without a player entry remain supported. The player host port defaults to `8080`
when selected and must differ from the backend port. Both bind to host loopback.
`GOBY_PUBLIC_URL` should be the origin users access, including the player origin
when it is the public entry point. The player proxies `/emby/` and `/admin/` to
its own backend, preserving range requests and WebSocket upgrades.

`prepare` verifies each selected archive, loads a missing image, copies the
helper and Compose companions, and creates private `goby.env`, `deployment.env`
and `setup-token.txt`. It creates state/cache/log directories for UID 10001
without changing media ownership. It reports the setup-token file path, not its
contents, and **does not start services**. Existing installations are never
overwritten by `prepare`.

For a private database CA, add `--database-ca-file /private/db-ca.pem`. A separate
database for application-managed restore can use
`--recovery-database-url-file /private/restore-db-url`.

## Select AMD and persistent sidecars

For AMD, select the matching AMD backend archive and add:

```sh
--profile amd --render-node /dev/dri/renderD128
```

The render node must be an AMD character device. The helper records its numeric
group, uses the release's scoped seccomp profile, and checks device access as
UID 10001. An accessible render device is a prerequisite, not proof that every
driver or Dolby Vision profile works. Use the accepted image/toolchain and the
release acceptance record; see the [AMD guide](README.amd.md).

Media mounts remain read-only by default. Add `--writable-media` only for an
approved writable library root when enabling background clips, per-track audio
waveforms, or bitmap subtitle timelines. It selects
`compose.background-previews.yaml`; it does not change the application settings
or enable automatic generation. The older `--writable-subtitles` option remains
available for subtitle download publication and its machine-ID mount; both
options can be selected together.

Grant targeted filesystem access to UID/GID `10001:10001` for each approved
source directory and its existing generated sidecar directories. A writable
Docker bind does not bypass host permissions. The helper **never recursively
changes media ownership or permissions**. `check` writes and removes one unique
probe in a sampled source file's parent, or the media root when there is no
sample. It does not claim that every nested library directory is writable.

Generated clips, waveforms and bitmap timelines stay beside their media under
`backdrops/` in separate owned namespaces. They are persistent media assets,
not transcode cache. Turning generation off, stopping containers or cleaning
transcode cache does not delete them. Replacing an existing valid generated
asset requires an explicit regeneration request. Back up original media and
sidecars together; a database backup does not contain their file bytes.

## Start and operate

```sh
sudo python3 /srv/goby/goby-docker.py check --directory /srv/goby
sudo python3 /srv/goby/goby-docker.py start --directory /srv/goby
sudo python3 /srv/goby/goby-docker.py status --directory /srv/goby
sudo python3 /srv/goby/goby-docker.py logs --directory /srv/goby --lines 100
sudo python3 /srv/goby/goby-docker.py stop --directory /srv/goby
```

All commands use the services selected during preparation. `check` verifies
immutable local images, Compose, service UIDs, sampled storage permissions and
AMD device access when selected. It does not authenticate PostgreSQL or run
media generation. `start` uses loaded images with no build/pull and waits up to
90 seconds (`--wait-timeout` changes the bound). It requires backend readiness,
player health when selected, and an API proxy response identifying that same
backend. `status` includes individual service records for player installations.

For `status`, exit zero means the query succeeded; read `status: ready` to confirm
readiness. A failed `start` returns nonzero and retains containers and data. A
database outage can produce `not_ready` or an exited backend with
`database_connection_lost`. Restore connectivity and run `start` again. Changed
image IDs or host ports require recreation; mismatches prevent probing another
installation. `logs` includes both selected services with service prefixes,
redacts configured secrets, and accepts 1-500 lines per service.

After a ready start, open `http://localhost:8080/` for the player and
`http://localhost:8080/admin/` for setup, or use the backend's port for `/admin/`.
Complete setup with the private token file. Use your chosen tunnel or HTTPS
reverse proxy for remote access; see the [full guide](README.md). Both containers
have read-only root filesystems. `restart: "no"` makes host-reboot startup an
explicit operator action. `stop` stops the selected player before the backend,
retaining containers, network and persistent data.

## Enable automatic processing

Generation remains opt-in after installation and upgrade. Select the relevant
library and item options in the administrator UI and follow work in **Tasks**.
Background clips default to 25 silent seconds in H.264/BT.709 SDR. Selecting an
AMD image or a writable media mount does not turn on automatic backgrounds.
Player source priority is configured independently of generation.

Movies, TV and mixed libraries can enable **Automatic seek previews**. TV
libraries can enable intro detection; movies and TV support the independent
credits detector. TV matching requires sufficient independent episodes, and
absence of a reliable result leaves playback unchanged. Disabling a detector
withdraws its automatic markers.
The [full operator guide](README.md) and
[player deployment guide](README.player.md) describe these options. The source
repository's API documentation records their precise contracts and accepted
limitations. Externally supplied SUP/IDX+SUB timelines do not imply that
original bitmap subtitle playback or burning is available.

## Update manually

The helper has **no automatic upgrade or database rollback command**. Preserve
the old image IDs, catalog, Compose files, helper and private configuration.
Stop both selected services, take a consistent PostgreSQL backup, and back up
matching state including `application-key-master.key` when present. Preserve
the separate source-media/sidecar backup as well.

Verify the new backend and player archives against the matching release catalog,
then load them with `docker image load --input <archive>`. Replace deployment
companions/helper from that verified toolkit and set `GOBY_OCI_IMAGE` and, when
selected, `GOBY_PLAYER_IMAGE` in the existing private `deployment.env` to the
recorded immutable IDs. Preserve private settings, setup token, paths, ports and
selected overlays. Run `check`, `start` and `status`, then verify login, catalog,
playback and retained generated assets. `start` recreates services whose image
or configuration changed.

An older backend-only installation remains backend-only with the new helper.
To add the player manually, first stop the installation, install the verified
`compose.player.yaml` companion, add its name to `installation.json`'s
`compose_files`, and add a `player` object with `image_id` and `host_port`.
Add matching `GOBY_PLAYER_IMAGE`, `GOBY_PLAYER_HOST_PORT`, and
`GOBY_PLAYER_API_UPSTREAM='http://goby:8096'` entries to `deployment.env`.
The player port must differ from the backend port. For writable sidecars, add
`compose.background-previews.yaml` to `compose_files`, set `writable_media` to
`true`, and grant targeted host access before running `check`. Do not rerun
`prepare` on an existing installation.

A schema rollback requires the matching database/state backup; switching image
IDs alone is insufficient. Stop all selected services before restoring, restore
the old compatible companions and image IDs together, and then run the old
version's checks. See [update and rollback](README.md#update-and-rollback) for
the complete procedure. The release receipt identifies exact supported versions;
do not use an earlier schema-52 image to open a newer schema-61 catalog.
