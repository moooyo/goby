# Docker quick start

Use the current schema 52 software or AMD Docker image and its matching
`current-release.json`. Both application profiles use source
`33445db2e2e64b6871116332c44605261a1bf2d4` for automatic library seek previews.
The integrated Material 3 dashboard and retained BIF outputs passed actual checks
in both final profiles. Owned containers, networks and database clients are
absent; private PostgreSQL is stopped and unrelated services are unchanged.
Use the current catalog rather than the earlier `0014bef` image receipts.
The repository result is `docs/development/bif-intro-expansion-20260930.md`;
Git integration is recorded separately. The earlier Docker operations journey,
14 helper tests and closure remain evidence for the existing helper contract.
Further online-provider work is deferred and no provider credentials are required.
The instructions and linked local guides below are included in the toolkit.

## Prepare once

Use a Linux amd64 host with rootful Docker Engine, Docker Compose supporting
`env_file.format: raw`, and Python 3.10 or newer. Run the helper with `sudo`.
Provision an external PostgreSQL 17 database and ordinary owner role first;
the toolkit does not install PostgreSQL. Its address must be reachable from the
container. Existing media must be readable/traversable by UID/GID `10001:10001`.

Extract `goby-docker-operations.zip` into `/download`; its top-level directory is
`goby-docker-operations/`. The current toolkit is in
`D:/Code/goby/.artifacts/bif-intro-20260930`, with software and AMD archives
in its `software` and `amd` subdirectories. Copy the selected current image archive
to the separate `/download/profile` directory. Earlier provider/intro-automation images
do not include this automation change. The small toolkit does not contain
duplicate images. `current-release.json`
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

## Enable automatic processing

### Automatic seek previews

For seek previews, create or edit a **Movies**, **TV shows** or **Mixed media**
library and enable **Automatic seek previews**. The new option defaults to off,
including for existing libraries after the schema 52 upgrade. Enablement,
successful scans and preview-profile changes request background generation;
the default task schedule also runs daily. There is no separate manual build or
Force step. The default preview interval remains 10 seconds.

Turning this option off stops new automatic generation and retains existing
valid previews, including after container recreation. Open **Tasks** to follow
progress, read errors or stop work; **Media analysis** shows generated outputs.

### Enable automatic intros

Create or edit a **TV shows** library and enable **Automatic intro detection**.
The setting defaults to off, including for libraries that existed before the
schema 51 upgrade. Once enabled, background work analyzes eligible episodes;
successful library scans request another analysis automatically.

Qualified matches become available for playback without approval or manual
correction. If no reliable match is found, playback stays unchanged. The detector
still needs at least three independent episodes and examines the first 600
seconds. Compatible players consume the resulting intro markers; this does not
promise that every player implements automatic skipping.

Open **Tasks** to follow progress, read failures or stop running intro work.
Media analysis shows current results and the library-settings entry.
Disabling the library option withdraws detected markers; re-enabling requests
fresh work. No scraper or provider credentials are required.

Intro accuracy remains limited. The expanded assessment missed all 12 reviewed
positive intros; three NASA short-ident negative cases produced no false
positives. Detector v3 and its thresholds are unchanged. Short intros and
differing audio/video versions need further recognition improvements; BIF
automation does not establish broader intro support.

## Update manually

The helper has **no upgrade or automatic database rollback command**. Verify the
new archive against its receipt; retain the old image ID and deployment files.
Stop Goby and take a consistent database backup plus a matching state backup,
including `application-key-master.key` when present. Load the verified archive,
change `GOBY_OCI_IMAGE` in `deployment.env` to its immutable ID, then run `check`,
`start` and `status`. Confirm login, catalog and playback. A schema rollback needs
the matching database/state backup; switching image IDs alone is insufficient.
See [update and rollback](README.md#update-and-rollback) for the complete procedure.
