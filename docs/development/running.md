# Running Goby during development

The current implementation supports PostgreSQL initialization, administrator setup/login, users, media libraries, bounded scans, local NFO metadata, persistent catalog entities, indexed local artwork, task control, original-file playback, external SRT/WebVTT, durable per-user playback state, client capabilities/session views, and NextUp queries. Embedded/additional subtitles, conversion, client events, and the remaining compatibility surface are still being implemented; this is not yet a production media replacement.

## Build inputs

- Go 1.27.1; the module pins the supported toolchain minimum.
- PostgreSQL, verified here with 17.11; connect using `GOBY_DATABASE_URL`.
- Node compatible with the locked frontend dependencies. The initial frontend was built with Node 26.1.0; see [frontend instructions](../../web/admin/README.md).
- FFmpeg/ffprobe 9.0.1. Scans use ffprobe for source metadata. Original playback serves unchanged bytes; conversion and hardware execution are not yet advertised.

The source uses pgx/v5 with bounded pooling, parameterized SQL, and transactional migrations. There is no SQLite driver or SQLite storage mode.

## Build on Windows

These are compilation commands, authorized for local use in the current task:

```powershell
npm --prefix web/admin ci
npm --prefix web/admin run build
go build ./...
```

Do not run the application, tests, browser probes, or FFmpeg checks on the local Windows machine without additional authorization. Functional verification uses `ssh test-env`.

## Linux deployment

1. Build the frontend with the locked npm dependencies and build the Go binary on Linux.
2. Install the binary as `/usr/local/bin/goby` and the complete `web/admin/dist` directory as `/usr/share/goby/admin`. This increment uses packaged read-only assets rather than embedding them into the binary.
3. Create an unprivileged `goby` user/group, a database and role dedicated to Goby, and the directories referenced by [goby.service](../../deploy/linux/goby.service).
4. Copy [the environment example](../../deploy/linux/.env.example) to `/etc/goby/goby.env`, restrict it to the service manager, and replace all example secrets. A systemd `EnvironmentFile` may remain root-readable only because systemd reads it before switching to the service user.
5. Configure `GOBY_PUBLIC_URL` as the exact browser-facing origin. Deploy `/admin`, `/admin/v1`, and `/emby` on that origin. Set `GOBY_TRUSTED_PROXIES` to the actual proxy CIDRs and have the proxy append or replace `X-Forwarded-For` correctly, so login limits apply to individual clients. Forwarded headers from untrusted peers are ignored. This increment requires an origin without a subpath; reverse-proxy subpath support remains a compatibility task.
6. Use HTTPS and secure cookies for remote access. `GOBY_COOKIE_SECURE=false` is an explicit setting for an isolated HTTP test instance, not the production default.
7. Start the service. Migrations run before the listener. Visit `/admin/`, enter the one-time `GOBY_SETUP_TOKEN`, and create the first administrator. Bootstrap closes atomically and remains closed after a restart.

An empty `GOBY_SETUP_TOKEN` prevents startup until setup has completed. After initialization, the deployment secret can be removed from the environment and the service restarted. Never include a real database password or setup token in Git.

The systemd unit deliberately does not hide every device with `PrivateDevices=true`; later GPU profiles need configured render/NVIDIA device access. That does not imply that GPU access or hardware playback has already been verified.

## Configuration

| Environment variable | Meaning |
| --- | --- |
| `GOBY_DATABASE_URL` | Required PostgreSQL URL; configure TLS according to the database deployment |
| `GOBY_STARTUP_TIMEOUT` | Startup and upgrade time budget as a Go duration; default `5m`, allowed `1s` through `30m` |
| `GOBY_LISTEN` | HTTP listen address, default `:8096` |
| `GOBY_PUBLIC_URL` | Exact administrator/client-facing HTTP(S) origin, default `http://localhost:8096` |
| `GOBY_SERVER_NAME` | Display name, default `Goby` |
| `GOBY_SETUP_TOKEN` | One-time deployment secret of at least 24 bytes; required before setup |
| `GOBY_COOKIE_SECURE` | Secure administrator cookies, default `true` |
| `GOBY_TRUSTED_PROXIES` | Comma-separated trusted proxy CIDRs for `X-Forwarded-For`; empty by default |
| `GOBY_WEB_DIR` | Built administrator asset directory, default `web/admin/dist` |
| `GOBY_FFMPEG` | FFmpeg executable path, default `ffmpeg`; consumed by subsequent media stages |
| `GOBY_FFPROBE` | ffprobe executable path, default `ffprobe`; consumed by subsequent media stages |
| `GOBY_MEDIA_ROOTS` | Administrator-approved media directories; colon-separated on Linux; empty by default |

Configure media roots before creating a library. The service must be able to traverse and read those directories; the root endpoint checks actual directory readability under the service identity. Libraries can select only directories within the configured roots. An unavailable mount prevents its scan, retains existing catalog data, and does not prevent the identity/dashboard service from starting.

The scanner supports movie, TV, music, and mixed libraries, with two concurrent probe workers and a bounded queue. Library deletion removes its items, image records, and entity associations while preserving files. Shared/orphan entity identities remain stored but are hidden from browsing unless associated with visible items. Filesystem deletion is not implemented. Symbolic links within scan traversal are skipped; registered root components are opened through anchored directory handles. Network URL/manifest sources are not accepted as ordinary self-contained media files.

The scanner also reads [local NFO metadata](local-metadata.md). A normal library scan detects sidecar changes even when media probing is cached. Valid sidecar removal restores scanner-derived values; malformed or inaccessible sidecars retain the last valid metadata with a warning. Schema migration `0003` stores these local overrides separately from probe data. No new configuration variable is required.

Migration `0004` backfills persistent genre/tag/studio/person identities from stored NFO data. Migration `0005` stores [local artwork](local-artwork.md); existing files become indexed during a library scan. Image conversion uses a bounded in-memory cache and needs no writable cache directory. Indexed image contents are public under the compatible ImageService contract; image enumeration still requires authentication and library access. No arbitrary path or URL can be requested through the image-content route.

Migration `0006` adds durable playback sessions and user state. Run a normal scan of existing libraries after upgrading: probe version 2 includes Linux ctime and extra codec facts needed for original-file delivery. Older snapshots remain unavailable for playback until rescanned. See [direct playback](direct-playback.md) for endpoints, current capabilities, and session/resume policy. Scans are not automatically scheduled by this upgrade.

Migration `0007` adds validated client capability snapshots to authentication sessions; `0008` adds validated player hints to playback sessions. These upgrades need no new configuration or rescan beyond the earlier probe-version requirement. [Client sessions](client-sessions.md) distinguishes online presence, login expiration, current playback, and supported declarations. [NextUp](next-up.md) documents series-directed behavior and the remaining global-query reference gap.

Migration `0009` adds indexed [external subtitles](external-subtitles.md). Run a normal library scan to discover existing SRT/WebVTT sidecars; no FFmpeg extraction is needed for these standalone text formats. Every subtitle download checks the current source and account permissions, including before returning 304. Native VTT uses wire Codec=vtt, and both native and converted delivery URLs carry the current user's token.

One catalog writer process may own a PostgreSQL database/schema at a time. It holds a dedicated advisory-lock session and executes short catalog/job write transactions on that same session. Use a direct PostgreSQL connection or a session-preserving connection pool; transaction/statement pooling is unsupported. If the session is lost, old work cannot reconnect through the pool and overwrite a successor's state. Restart the service to recover ownership; `/readyz` reports the lost session. Ordinary request or task cancellation does not interrupt a started short write transaction or discard the owner connection.

Database upgrades use the startup deadline rather than the ordinary 15-second statement limit. The migration transaction temporarily disables `statement_timeout`, including while it waits for the migration lock, and restores the connection setting on commit or rollback. A caller deadline, service termination, or the migration's 30-minute upper bound still cancels the work. Connection establishment retains its shorter limits. Increase `GOBY_STARTUP_TIMEOUT` for a large metadata backfill before starting the service; this setting is not a guarantee that an arbitrary catalog finishes within that budget.

Administrator passwords must be nonempty. Passwords may contain at most 72 UTF-8 bytes and are hashed with bcrypt; ordinary client accounts may be configured without a password. Such accounts must still authenticate to receive a token. Username uniqueness uses Unicode simple case folding. Password length and account policy will be exposed consistently in the full user-management milestone.

## Test deployment

The dedicated test host has a root-only `/opt/goby-test/test.env` and a separate `/opt/goby-test/browser.env`; neither is part of the repository. Tests create randomly named PostgreSQL schemas and clean up only those schemas.

The maintained [foundation deployment script](../../scripts/test-env/run-foundation.sh) installs a dedicated non-root service at `http://127.0.0.1:18096`, using previously transferred source and built frontend assets. It does not expose this test service publicly. The test deployment has its own administrator and disposable data. [prepare-media-fixtures.sh](../../scripts/test-env/prepare-media-fixtures.sh) creates small synthetic movie, TV, and music inputs inside an ownership-marked `/opt/goby-fixtures` directory and updates the protected test configuration.

Remote test commands, after loading the protected test environment:

```sh
go test -race -count=1 ./...
```

The presence of `GOBY_TEST_DATABASE_URL` is required to execute integration tests. A run reporting skipped PostgreSQL tests is not sufficient verification. On the current constrained test host, Go module/build caches live in dedicated `/dev/shm/goby-go-*` directories to avoid filling the root filesystem.

The current host also has an owned 512 MiB tmpfs at `/opt/goby-test/exec-scratch`, mounted with `nosuid,nodev` and mode 0700. Verification sets `GOTMPDIR` and `TMPDIR` to this executable scratch directory. This avoids consuming the constrained root filesystem without changing the shared `/dev/shm` mount's execution policy. The directory is dedicated to Goby verification and is not an application media or persistent-data location.

See [environment evidence](test-env.md) for exact installed versions and the distinction between compiled hardware interfaces and real hardware execution.
