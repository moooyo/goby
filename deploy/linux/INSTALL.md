# Internal Linux amd64 systemd candidate

This package is for internal evaluation. Goby's project license and complete
distribution notices remain unresolved; possession of this archive does not
grant permission to redistribute it. The package contains an embedded
administrator UI, not PostgreSQL, FFmpeg, a reverse proxy or a consumer player.
Building it does not prove installation, upgrade, restart or client acceptance.

## Before installation

Use Linux amd64 with systemd, a separately provisioned PostgreSQL 17 database
and an ordinary database role allowed to create and migrate Goby's own schema.
Give this installation its own database and secrets; never select an existing
test candidate, another application's database or a database superuser.
Install the selected FFmpeg and FFprobe 9.0.1 build separately. The environment
example uses `/opt/ffmpeg/9.0.1/bin`; change both paths if the verified tools are
elsewhere. Backup operations also require compatible `pg_dump` and
`pg_restore`; this package does not run or validate a backup.

Verify the archive SHA256 against its separately supplied, trusted
`package-manifest.json`, then inspect and extract it into a new staging
directory. Its only top directory is `goby-linux-amd64-systemd`, containing
five regular files: `goby`, `manifest.json`, `goby.service`,
`goby.env.example`, and `INSTALL.md`. The binary mode is 0755 and the other
payload modes are 0644. Reject unexpected files, links, paths, sizes, hashes or
modes. The external package manifest binds all members and the original build
manifest; the build manifest identifies the binary, sources and embedded assets.
These hashes do not replace a trusted source for the manifest itself.

The unit runs as the local `goby` user and `goby` group. Check both with
`getent passwd goby` and `getent group goby`. Reuse an existing appropriate
system account without changing its UID, GID, groups, home or ownership of
unrelated files. If the account is absent, the host administrator must create
an approved unprivileged service account before installation. This package
does not allocate numeric IDs or create/delete accounts.

This procedure is a fresh install, not an upgrade. Confirm that the destination
binary, configuration, unit, aliases, drop-ins, enablement links and service
directories are unclaimed. If any already exist or the unit is loaded, stop
and use the installation's own upgrade/recovery procedure. Do not overwrite
files or recursively chown directories that belong to an earlier installation.

## Install and configure

The shipped unit uses these exact paths:

| Purpose | Path and ownership |
| --- | --- |
| Executable | `/usr/local/bin/goby`, root-owned 0755 |
| Unit | `/etc/systemd/system/goby.service`, root-owned 0644 |
| Environment | `/etc/goby/goby.env`, root-owned 0600; parent root-owned 0700 |
| Working directory and persistent state | `/var/lib/goby`, owned by the service |
| Cache | `/var/cache/goby`, owned by the service |
| Logs | `/var/log/goby`, owned by the service |
| Runtime directory | `/run/goby`, managed by systemd |

From the verified staging directory, and only after the fresh-path checks:

```sh
sudo install -m 0755 -o root -g root goby /usr/local/bin/goby
sudo install -m 0644 -o root -g root goby.service /etc/systemd/system/goby.service
sudo install -d -m 0700 -o root -g root /etc/goby
sudo install -m 0600 -o root -g root goby.env.example /etc/goby/goby.env
```

Edit `/etc/goby/goby.env` privately before starting the service. Replace the
example database password and setup token with independently generated random
secrets; the setup token must have at least 24 bytes. Set a unique server name,
the real database URL, approved read-only media roots, and actual tool paths.
Do not put secrets in command-line arguments, public logs or source control,
and do not source this file as shell code. Systemd reads it as an environment
file even though the unprivileged application cannot read `/etc/goby` itself.

Keep `GOBY_WEB_DIR` unset for the embedded UI. A nonempty value deliberately
selects that external directory and does not fall back to embedded files when
the directory is unavailable. Ordinary non-embedded binaries require an
external administrator bundle and an explicit suitable directory. Do not copy
an administrator directory alongside this embedded package to hide a failure.

The template keeps secure cookies enabled and shows an HTTPS public origin.
For a normal HTTPS deployment, configure the actual reverse proxy and only its
trusted CIDRs; prefer a loopback listen address behind a local proxy. A direct
isolated HTTP test needs its own explicit `GOBY_PUBLIC_URL`, loopback listen
address, empty trusted-proxy list and `GOBY_COOKIE_SECURE=false` runtime input.
That test override is not the product's default or HTTPS deployment guidance.

Let systemd create its declared State/Cache/Logs/RuntimeDirectory paths. Keep
the unit's `User`, `Group`, `NoNewPrivileges`, `ProtectSystem`, `ProtectHome`,
`PrivateTmp`, empty capability set, `RestrictSUIDSGID` and umask unchanged.
Media outside the service directories must be readable by `goby` and remains
read-only under the unit's filesystem policy. Do not broaden writable paths
or run as root to bypass a failed startup or scan.

## First setup and normal stop/start

```sh
sudo systemctl daemon-reload
sudo systemctl start goby.service
sudo systemctl show goby.service -p MainPID -p User -p Group -p ActiveState -p Result
```

Confirm a healthy process and the expected listener before continuing. Open
`/admin/` at the configured public origin, complete first-admin setup with the
private setup token, then log in with the new administrator. Add only the
intended media libraries and confirm an explicit scan completes. Log out and
confirm the old credential is rejected. Inspect server logs for startup or
database errors without publishing their private contents. The unit's
`Restart=on-failure` policy is not proof of a successful initial startup.

For a normal restart check, first record the catalog and permitted UserData,
then run:

```sh
sudo systemctl stop goby.service
sudo systemctl show goby.service -p MainPID -p ActiveState -p Result -p ExecMainStatus
sudo systemctl start goby.service
```

Require the first process to exit normally and its cgroup to be empty before
starting again. Confirm a different process runs the same installed binary,
setup remains initialized, and the catalog and UserData persist before any
new scan or edit. Log in and log out again. A forced kill, automatic failure
restart or unexpected data change does not pass this check. Stopping Goby does
not stop PostgreSQL. This procedure does not enable boot startup or establish
host-reboot, power-loss or PostgreSQL-restart durability.

## Stop, preserve data and remove only this installation

Use `systemctl stop goby.service` and verify the exact owned application process
has exited and its cgroup is empty. Preserve the database together with its
matching private application key, recovery/control metadata and backup objects
using the supported backup/recovery procedure before any data removal. The
default application key is `/var/lib/goby/application-key-master.key`; never
print it or assume a database-only copy is a usable recovery point.

Record and verify the identity and hashes of files owned by this installation
before removing its binary, unit and any explicitly created isolation drop-in.
Reload systemd and verify the removed unit is no longer loaded from a fragment.
This procedure created no enablement links; do not delete unfamiliar links or
drop-ins. Keep `/etc/goby`, `/var/lib/goby`, `/var/cache/goby` and `/var/log/goby`
by default. Systemd may already remove `/run/goby` on stop. Data/configuration
disposal needs a separate explicit inventory and verified preservation plan;
never use a blanket recursive delete or remove the shared `goby` account.

An isolated acceptance fixture must additionally close its own credentials,
database cluster, namespace and temporary resources, while retaining private
evidence. That fixture's cleanup does not authorize deleting any other
installation. No upgrade, GPU, native arm64, OCI, host durability or core-video
result is implied by this package or these instructions.
