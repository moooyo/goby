# Docker quick start

Status: **COMPLETE within the selected Docker operations scope**. The actual
software Docker journey, 14 helper unit tests and owned-resource closure passed.
This guide reuses the accepted software or AMD image; no image rebuild is needed.
Further online-provider work is deferred and no provider credentials are required.
The repository report is `docs/development/docker-operations-20260930.md`;
the instructions and linked local guides below are included in the toolkit.

## Prepare once

Use a Linux amd64 host with rootful Docker Engine, Docker Compose supporting
`env_file.format: raw`, and Python 3.10 or newer. Run the helper with `sudo`.
Provision an external PostgreSQL 17 database and ordinary owner role first;
the toolkit does not install PostgreSQL. Its address must be reachable from the
container. Existing media must be readable/traversable by UID/GID `10001:10001`.

Extract `goby-docker-operations.zip` into `/download`; its top-level directory is
`goby-docker-operations/`. Keep the selected profile's existing image archive in
the separate `/download/profile` directory. Already downloaded
software/AMD archives do not need to be downloaded again; the small toolkit does
not contain duplicate images. `current-release.json`
binds the archive hash and immutable image ID; the helper prefers a manifest in
the release directory, then the one beside itself. Store the one-line database
URL in `/private/db-url`, with permissions `0600` or stricter. Keep it out of
command arguments, source control and captured output. Choose a new installation
directory, then run:

```sh
sudo python3 /download/goby-docker-operations/goby-docker.py prepare \
  --release-dir /download/profile --directory /srv/goby \
  --media-dir /srv/media --database-url-file /private/db-url \
  --public-url http://localhost:8096 --host-port 8096 --profile software
```

`prepare` verifies the archive, loads the image if missing, copies the helper and
Compose companions, and creates private `goby.env`, `deployment.env` and
`setup-token.txt`. It creates the installation's state/cache/log directories for
UID 10001 without changing media ownership. It reports the setup-token file path,
not its contents, and **does not start Goby**.

For a private database CA, add `--database-ca-file /private/db-ca.pem`. A separate
database for application-managed restore can use
`--recovery-database-url-file /private/restore-db-url`. For AMD, select the AMD
archive with `--profile amd --render-node /dev/dri/renderD128`; the device must
exist. Default media is read-only. Select `--writable-subtitles` only for an
approved writable media directory; see [subtitle requirements](README.providers.md#optional-subtitle-write-access).

## Start and operate

```sh
sudo python3 /srv/goby/goby-docker.py check --directory /srv/goby
sudo python3 /srv/goby/goby-docker.py start --directory /srv/goby
sudo python3 /srv/goby/goby-docker.py status --directory /srv/goby
sudo python3 /srv/goby/goby-docker.py logs --directory /srv/goby --lines 100
sudo python3 /srv/goby/goby-docker.py stop --directory /srv/goby
```

`check` checks Compose, storage mounts and UID permissions; it does not prove
database authentication. `start` waits up to 90 seconds by default for readiness
(`--wait-timeout` changes that bound). `status` reports safe diagnostic codes.
Use actual startup/readiness to establish database connectivity.

For `status`, exit zero means the query succeeded; read `status: ready` to confirm
readiness. A database outage can produce `not_ready` or an exited application
with `database_connection_lost`. Restore connectivity and run `start` again.
A failed `start` returns nonzero and retains the container/data. A changed host
port requires recreation; `container_port_mismatch` prevents probing another
instance. `logs` redacts configured secrets and accepts 1–500 lines.

After a ready start, open `http://localhost:8096/admin/` on the Docker host, or
through your chosen tunnel, and complete setup using the private token file.
The published port binds to loopback. For production HTTPS, configure the public
URL and reverse proxy using the [full guide](README.md). The container stays
non-root with a read-only root filesystem. `restart: "no"` means startup after a
host reboot is an explicit operator action. `stop` retains the container, its
network and persistent data; it does not retire the installation.

## Update manually

The helper has **no upgrade or automatic database rollback command**. Verify the
new archive against its receipt; retain the old image ID and deployment files.
Stop Goby and take a consistent database backup plus a matching state backup,
including `application-key-master.key` when present. Load the verified archive,
change `GOBY_OCI_IMAGE` in `deployment.env` to its immutable ID, then run `check`,
`start` and `status`. Confirm login, catalog and playback. A schema rollback needs
the matching database/state backup; switching image IDs alone is insufficient.
See [update and rollback](README.md#update-and-rollback) for the complete procedure.
