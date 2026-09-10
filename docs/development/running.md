# Running Goby during development

The current implementation supports PostgreSQL initialization, administrator setup/login, users, media libraries, bounded scans, local NFO metadata, persistent catalog entities, indexed local artwork, task control, original-file playback, external SRT/WebVTT, durable per-user playback state, client capabilities/session views, user-state events, initial remote control, and NextUp queries. Authenticated MPEG-TS HLS adds full VOD manifests, seeking, remux, and supported audio/video conversion. Universal and legacy audio routes provide original, progressive, or MPEG-TS HLS delivery with scoped client playback references; Audio and Video PlaybackInfo select supported HTTP/HLS TranscodingProfiles in their declared order. The dashboard also manages metadata, login sessions, ordinary devices, and independent application keys. Additional audio timing/input/profile cases, packed-audio HLS, broader subtitles/formats, hard resource isolation, actual GPU execution, and complete client acceptance remain unfinished; this is not yet a production media replacement. The dashboard remains an administrator interface without a consumer web player.

The current database schema is **19** and the probe cache version remains **6**.
The [M5f task increment](verification-m5f-tasks.md) adds durable library-wide
tasks, schedule rules, request receipts, and owned scan-child associations.
It creates no automatic schedule and does not backfill old scans into task
runs. The preceding [M5e device increment](verification-m5e-devices.md) adds ordinary
device generations and a separate shared application-key server registry.
Migrations 17 and 18 preserve existing catalog, playback, credential, and key
history; they require no media rescan. The separate rescan requirement for
older probe-cache versions still applies.

The `test-env` virtual disk was expanded to 97 GiB on 2026-09-10, and the root
partition/ext4 filesystem grew online with `growpart` and `resize2fs`. It then
reported roughly 96G total and 60G available; existing service PIDs, root
identity, and boot partitions were preserved. See the
[capacity evidence](test-env-disk-growth.json). This resolves the earlier root
capacity shortage. The subsequent M5f deployment uses schema 19.

## Build inputs

- Go 1.27.1; the module pins the supported toolchain minimum.
- PostgreSQL, verified here with 17.11; connect using `GOBY_DATABASE_URL`.
- Node compatible with the locked frontend dependencies. The initial frontend was built with Node 26.1.0; see [frontend instructions](../../web/admin/README.md).
- FFmpeg/ffprobe 9.0.1. Scans use ffprobe for source metadata and bounded audio timing inspection; copied-video HLS uses packet seekpoints. Original playback serves unchanged bytes; supported HLS and progressive audio/video conversion share bounded FFmpeg workers. Video hardware decode/encode can be configured but actual GPU execution remains unverified.

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
7. Review [transcoding configuration](transcoding-configuration.md). Conversion is enabled by default, with a dedicated `/var/cache/goby/transcodes` cache beneath the systemd-managed private cache parent. Configure process/storage/output limits for the host; a custom cache needs a writable parent owned by `goby` and an appropriate service write exception. Configure hardware selections only with the matching driver and device access.
8. Keep `GOBY_API_KEY_MASTER_KEY_FILE` in a persistent private directory owned by `goby`. The supplied unit creates `/var/lib/goby` with mode `0700`; its default master file is created lazily with mode `0600`. A custom parent must already exist, remain private and be writable through the service sandbox. Back up this file and PostgreSQL together; see [application-key operations](application-keys.md).
9. Start the service. Migrations run before the listener. Visit `/admin/`, enter the one-time `GOBY_SETUP_TOKEN`, and create the first administrator. Bootstrap closes atomically and remains closed after a restart.

An empty `GOBY_SETUP_TOKEN` prevents startup until setup has completed. After initialization, the deployment secret can be removed from the environment and the service restarted. Never include a real database password or setup token in Git.

The systemd unit deliberately does not hide every device with `PrivateDevices=true`; configured VAAPI/QSV/CUDA/NVENC paths require the corresponding render/NVIDIA device access. Device access is not granted automatically, and actual hardware execution remains unverified on the current test host.

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
| `GOBY_API_KEY_MASTER_KEY_FILE` | Persistent 32-byte application-key master file; default `application-key-master.key` resolved against the process working directory; see [key operations](application-keys.md) |
| `GOBY_FFMPEG` | FFmpeg executable path, default `ffmpeg`; used by HLS and progressive audio workers |
| `GOBY_FFPROBE` | ffprobe executable path, default `ffprobe`; used by scans, exact audio timing inspection, and copied-video HLS timeline probes |
| `GOBY_TRANSCODING_ENABLED` | Enable configured conversion, default `true`; set `false` to disable it |
| `GOBY_TRANSCODE_CACHE` | Dedicated conversion cache, default `/var/cache/goby/transcodes` |
| `GOBY_HW_DECODER`, `GOBY_HW_ENCODER`, `GOBY_HW_DEVICE` | Independent hardware selections; software decoding/encoding and no device by default |
| `GOBY_MEDIA_ROOTS` | Administrator-approved media directories; colon-separated on Linux; empty by default |

The [transcoding configuration reference](transcoding-configuration.md) lists every `GOBY_TRANSCODE_*` setting and accepted range. Defaults allow two running jobs, one per user/authentication session, two configured FFmpeg threads, a 20 GiB cache, an 8 GiB per-job limit, and a 512 MiB free-space reserve. Output planning defaults to 20 Mbps, 1920x1080, and up to eight audio channels. These admission and periodic monitoring limits are not hard filesystem or cgroup ceilings. Settings are loaded at startup, and invalid explicit settings fail even when conversion is disabled.

Progressive audio uses these same process, queue, cache and reader budgets; it needs no additional environment variables or separate encoder pool. Its bitrate target/ceiling applies to encoded media, not instantaneous HTTP transfer speed. Original delivery remains available when conversion is disabled and the source/current account permits it. See [audio playback](audio-playback.md) for output formats, `Container` versus `TranscodingContainer`, and exact targets versus maximum limits.

Configure media roots before creating a library. The service must be able to traverse and read those directories; the root endpoint checks actual directory readability under the service identity. Libraries can select only directories within the configured roots. An unavailable mount prevents its scan, retains existing catalog data, and does not prevent the identity/dashboard service from starting.

The scanner supports movie, TV, music, and mixed libraries, with two concurrent probe workers and a bounded queue. Library deletion removes its items, image records, and entity associations while preserving files. Shared/orphan entity identities remain stored but are hidden from browsing unless associated with visible items. Filesystem deletion is not implemented. Symbolic links within scan traversal are skipped; registered root components are opened through anchored directory handles. Network URL/manifest sources are not accepted as ordinary self-contained media files.

The scanner also reads [local NFO metadata](local-metadata.md). A normal library scan detects sidecar changes even when media probing is cached. Valid sidecar removal restores scanner-derived values; malformed or inaccessible sidecars retain the last valid metadata with a warning. Schema migration `0003` stores these local overrides separately from probe data. No new configuration variable is required.

Migration `0004` backfills persistent genre/tag/studio/person identities from stored NFO data. Migration `0005` stores [local artwork](local-artwork.md); existing files become indexed during a library scan. Image conversion uses a bounded in-memory cache and needs no writable cache directory. Indexed image contents are public under the compatible ImageService contract; image enumeration still requires authentication and library access. No arbitrary path or URL can be requested through the image-content route.

Migration `0006` adds durable playback sessions and user state. Probe version 2 introduced Linux ctime and extra codec facts needed for original-file delivery; version 3 introduced exact audio timing, version 4 extended its supported Ogg subset, version 5 added independent container-origin facts, and current version 6 adds optional private H.264 restart evidence. Older snapshots remain unavailable for media/subtitle reads until rescanned. See [direct playback](direct-playback.md) for endpoints, current capabilities, and session/resume policy. Database upgrades do not automatically schedule scans.

Migration `0007` adds validated client capability snapshots to authentication sessions; `0008` adds validated player hints to playback sessions. These upgrades need no new configuration or rescan beyond the earlier probe-version requirement. [Client sessions](client-sessions.md) distinguishes online presence, login expiration, current playback, and supported declarations. [NextUp](next-up.md) documents series-directed behavior and the remaining global-query reference gap.

Migration `0009` adds indexed [external subtitles](external-subtitles.md). Run a normal library scan to discover existing SRT/WebVTT sidecars; no FFmpeg extraction is needed for these standalone text formats. Every subtitle download checks the current source and account permissions, including before returning 304. Native VTT uses wire Codec=vtt, and both native and converted delivery URLs carry the current user's token.

[WebSocket events and remote control](websocket-events.md) use the existing authentication and catalog schema; no new migration or rescan is needed. A reverse proxy must forward the RFC 6455 upgrade for `/embywebsocket` (or a supported alias) and permit long-lived connections with protocol Ping/Pong. These connections require an Emby token, never an administrator cookie. In-memory notifications are not replayed after reconnect or service restart; clients must refresh current state through the HTTP APIs.

Migration `0010` adds durable encoding-job records for the [conversion engine](transcode-engine.md); `0011` expands bounded plan JSON to 128 KiB for immutable VOD source cut points. The [HLS adapter](hls-playback.md) now connects playback negotiation to full-duration manifests, authenticated segments, stable global numbering, seek production, and encoding cleanup for video and audio HLS. Compatible results advertise the supported delivery after applying the configured limits and current user permissions. Negotiation does not start an encoder; workers start when segment production is needed.

Migrations `0010` and `0011` alone require no rescan beyond the probe-version requirement. Enabled conversion initializes its owned cache and recovers interrupted job records during startup. Output revisions and timelines remain in memory, so clients must prepare playback again after a restart; durable authentication and user progress remain in PostgreSQL. Live/adaptive playlists, fMP4 HLS, packed-audio HLS, and HLS subtitle delivery remain outside the implemented HLS subset. Progressive fragmented MP4 audio and video are separate HTTP outputs.

Migration `0012` adds [client playback references](client-playback-references.md). Universal clients can supply a fresh `PlaySessionId` without calling PlaybackInfo first. The nonce binds to a canonical server play inside the complete user/authentication-session/device scope and one item/source. Reuse cannot retarget a stopped, expired, or tombstoned reference. Reports and cleanup resolve owned aliases to the same canonical identity; preparing a reference does not mark content played.

Run a normal scan of existing libraries after upgrading to **probe cache version 6**, including libraries last scanned with version 5. Optional [video restart analysis](video-fast-seek.md) runs during scanning even when conversion is disabled; unsupported analysis preserves ordinary media facts. A same-version scan still reuses unchanged cached data, so missing indexes or a changed FFmpeg binary do not alone force rebuilding them. Runtime proof rejects stale evidence and retains linear decoding. Existing `FormatStartKnown` and signed `FormatStartTicks` fields preserve explicit zero separately from a missing container clock. They do not borrow the audio presentation origin or infer a container clock from packet timestamps. Exact audio/Ogg sample timing remains independent. Older snapshots are not accepted by media or subtitle reads until rescanned. Current metadata with unproven audio timing can still support original-file delivery; Matroska/WebM's quantized packet clock remains outside the exact audio-only conversion subset, not a blanket exclusion from video conversion. Metadata, artwork, identities and user state remain in PostgreSQL.

Migration `0013` adds a user-management revision with default 1. [Native user management](../api/admin-users.md) provides complete account/policy updates and password reset with optimistic conflict detection, transaction-time administrator checks, final-enabled-administrator protection and session revocation. The revision is a decimal string on the wire. This migration does not schedule the separate probe-version scan.

Migration `0014` adds `item_metadata_state`, initializes it for existing items and supplies an insert trigger for new catalog rows. It preserves the previous NFO projection and every pre-existing table row. [Native metadata editing](../api/admin-metadata.md) stores automatic snapshots, manual overrides, locked values and revisions separately; effective item columns and entity links change in the same catalog transaction. This upgrade needs no new configuration and no probe rescan. Media/NFO files remain unchanged by editor writes. The complete administrator milestone, including provider integration and backup/restore, remains open.

M5c [login-session administration](../api/admin-sessions.md) was introduced against the existing schema-14 authentication records without a migration or probe rescan. The Sessions page lists and filters native administrator (`admin`) and ordinary Emby (`emby`) login history and revokes one selected login; current administrator authorization is checked in the transaction. Application-key parent credentials also use the database's `sessions` table after migration 16, but they remain on the API keys management surface and are excluded from the native login-session list. Self-revocation signs the dashboard out. Active login validity is distinct from online presence, and a single login revocation does not block a later sign-in on the same device.

Migration `0015` adds the persisted `force_probe` scan-job mode with a false default for historical jobs. The administrator can explicitly [refresh cached media details](verification-m4g-media-refresh.md) to re-probe unchanged files and rebuild eligible private playback indexes. Ordinary same-version scans continue to reuse valid cached facts; the schema migration itself schedules no scan.

The [audio adapter](audio-playback.md) implements Universal and legacy stream selection, while [Audio PlaybackInfo](audio-profile-playback.md) adds ordered HTTP/HLS DeviceProfiles and rechecks applicable conditions on projected output facts. The response preserves original MediaSource DTOs and emits a standard progressive URL with concrete output settings; negotiation starts or reserves no progressive encoding job. GET rechecks current source and permissions and accepts client-side `StartTimeTicks` changes for seeking. Its reader follows private append-only `stream.bin` until durable completion and the final bytes; partial failure aborts the HTTP response instead of reporting a normal end. Progressive HEAD starts no encoder and reports no estimated length. Integer source samples determine accurate WAV headers and output bounds, and audio HLS merges unproducible short tails while preserving total timeline duration. Additional audio timing/profile cases remain separate work.

[Progressive video](progressive-video-playback.md) uses the same manager and the ordinary `/emby/Videos/{Id}/stream.mp4` route. Ordered video profiles select supported HTTP or HLS output. The MP4 subset supports H.264/AAC copy or encoding at zero start and H.264 encoding for nonzero seeks; copied-video nonzero seek is declined. [Verified video seeking](video-fast-seek.md) can skip prefix software video decoding when private restart evidence and runtime proof are valid. Other inputs retain linear decoding, and audio history remains linear, so long seeks can still hit the 45-second startup deadline. General copied-video seeking, HDR tone mapping, and actual GPU execution remain unverified.

One catalog writer process may own a PostgreSQL database/schema at a time. It holds a dedicated advisory-lock session and executes short catalog/job write transactions on that same session. Use a direct PostgreSQL connection or a session-preserving connection pool; transaction/statement pooling is unsupported. If the session is lost, old work cannot reconnect through the pool and overwrite a successor's state. Restart the service to recover ownership; `/readyz` reports the lost session. Ordinary request or task cancellation does not interrupt a started short write transaction or discard the owner connection.

Database upgrades use the startup deadline rather than the ordinary 15-second statement limit. The migration transaction temporarily disables `statement_timeout`, including while it waits for the migration lock, and restores the connection setting on commit or rollback. A caller deadline, service termination, or the migration's 30-minute upper bound still cancels the work. Connection establishment retains its shorter limits. Increase `GOBY_STARTUP_TIMEOUT` for a large metadata backfill before starting the service; this setting is not a guarantee that an arbitrary catalog finishes within that budget.

Administrator passwords must be nonempty. Passwords may contain at most 72 UTF-8 bytes and are hashed with bcrypt; ordinary client accounts may be created without a password. Such accounts must still authenticate to receive a token. Username uniqueness uses Unicode simple case folding. The native password-reset contract requires a nonempty replacement. Full user-policy coverage and user deletion remain planned work.

## Test deployment

The dedicated test host has a root-only `/opt/goby-test/test.env` and a separate `/opt/goby-test/browser.env`; neither is part of the repository. Tests create randomly named PostgreSQL schemas and clean up only those schemas.

The maintained [foundation deployment script](../../scripts/test-env/run-foundation.sh) installs a dedicated non-root service at `http://127.0.0.1:18096`, using previously transferred source and built frontend assets. It does not expose this test service publicly. The test deployment has its own administrator and disposable data. Its dedicated `/dev/shm/goby-transcodes-test` cache uses `goby:goby` ownership and mode `0700`, with a separate deployment ownership record. The script only appends absent conversion settings; its default test limits are 128 MiB total, 32 MiB per job, and 16 MiB minimum free space. It also provisions `/var/lib/goby-test/application-key-vault` as a private service-owned directory, adds its exact service write scope, and appends the default master path only when unset. It never replaces an existing master; a custom configured path requires matching directory permissions and a service write exception. [prepare-media-fixtures.sh](../../scripts/test-env/prepare-media-fixtures.sh) creates small synthetic movie, TV, and music inputs inside an ownership-marked `/opt/goby-fixtures` directory and updates the protected test configuration.

The current M5f deployment runs schema 19 as UID 995, PID 3570491, with probe
version 6. Its [deployment evidence](m5f-deployment-evidence.json) includes a
complete pre-stop backup, a real isolated database restore with all 21 old
tables equal, and exact preservation through the migration. The task
definition initially has no schedules or execution history. See
[task execution and scheduling](tasks.md) for startup, missed schedules,
shutdown, and ownership behavior, and the
[verification report](verification-m5f-tasks.md) for acceptance boundaries.
The [deployed task workflow](m5f-deployed-tasks.json) completed all five existing
libraries, preserved cached media and prior account/metadata state, and restored
its temporary schedule to empty. Execution and retired trigger history remain
stored; a normal scan can advance directory and library scan timestamps.

The preceding M5e deployment checkpoint ran schema 18 as UID 995, PID 3535438,
with probe version 6. Its [deployment evidence](m5e-deployment-evidence.json)
preserves all prior business fields, 21 catalog items, 11 media files, and the
matching application-key master without scheduling a scan. The
[deployed device workflow](m5e-deployed-devices.json) passed in 1.824 seconds
and preserved the prior rows and key namespace while cleaning up its newly
issued credentials and ordinary device generations. The
[verification report](verification-m5e-devices.md) records the full race suite,
isolated browser/restart results, and recovery limits; it does not claim a
completed product backup/restore workflow.

Remote test commands, after loading the protected test environment:

```sh
go test -race -count=1 ./...
```

The M4c playback-reference capacity test uses a separate PostgreSQL 17.11 cluster at `127.0.0.1:15432`, prepared by [prepare-postgres-scratch.sh](../../scripts/test-env/prepare-postgres-scratch.sh). Its data, socket and log directories live in the independently owned 1 GiB `/dev/shm/goby-pg-m4c` tmpfs; the dedicated `goby_test` role has no superuser, role-creation, database-creation or replication privileges. This cluster is disposable verification storage, not a replacement for the service's persistent PostgreSQL database on port 5432. Repeated preparation checks the owner marker, mount, cluster and listener identities without rotating credentials or rebuilding data.

For that capacity check, run the following inside the remote SSH shell from the transferred repository. Load the original environment first, then the root-only override; only the two Goby database URLs change, leaving toolchain and FFmpeg settings intact:

```sh
bash scripts/test-env/prepare-postgres-scratch.sh
set -a
. /opt/goby-test/test.env
. /opt/goby-test/m4c-test.env
set +a
export GOCACHE=/dev/shm/goby-go-cache GOMODCACHE=/dev/shm/goby-go-mod
export GOTMPDIR=/opt/goby-test/exec-work-m5f TMPDIR=/opt/goby-test/exec-work-m5f
go test -race -count=1 ./internal/library -run '^TestStoreCorrelatedPlaybackReferenceCapacityIncludesTombstonesAndAllowsReuse$'
```

Do not put this override in the Goby service environment. Stop the scratch instance only through the script's identity-checked `--stop` mode when its tests have finished; stopping retains its data for reuse. A deliberate unmount or reboot loses tmpfs data, and the script refuses to silently recreate an already recorded cluster.

Root filesystem free space was restored during M4c test-host maintenance. The host's persistent PostgreSQL instance was observed with `max_wal_size=128MB` and `min_wal_size=32MB`; these are test-host settings, not new Goby production defaults. `max_wal_size` is a checkpoint target, not a hard WAL ceiling. The scratch cluster separately uses the same WAL targets and a 1 GiB aggregate filesystem limit, which can still fill during oversized tests.

The presence of `GOBY_TEST_DATABASE_URL` is required to execute integration tests. A run reporting skipped PostgreSQL tests is not sufficient verification. On the current constrained test host, Go module/build caches live in dedicated `/dev/shm/goby-go-*` directories to avoid filling the root filesystem.

The current host also has an owned 768 MiB tmpfs at `/opt/goby-test/exec-scratch`, mounted with `nosuid,nodev` and mode 0700. It retains private verification evidence and isolated browser artifacts. After the root disk expansion, Go verification uses the separate root-owned directory `/opt/goby-test/exec-work-m5f` on the persistent filesystem for `GOTMPDIR` and `TMPDIR`. This avoids exhausting the evidence tmpfs without changing the shared `/dev/shm` mount's execution policy. Both directories are dedicated to verification and are not application media or persistent business-data locations.

See [environment evidence](test-env.md) for exact installed versions and the distinction between compiled hardware interfaces and real hardware execution.

Migration `0016` adds independent application keys and their client contexts,
with nullable user/context ownership in playback records. Existing users,
logins, playback records and saved user data are retained. The administrator
API keys page creates, reveals and revokes server-wide credentials; ordinary
login-session management stays separate. See the [key API contract](../api/application-keys.md).

Migration `0017` backfills an ordinary device registry from nonempty reported
IDs on existing Emby logins, including expired and revoked history. It adds
`sessions.device_registry_id` without changing those credentials' original
fields. Native administrator and application-key credentials remain outside
this association. The [Devices page and API](../api/devices.md) rename or remove
an ordinary generation; removal revokes all associated ordinary logins and
retires their WebSocket/conversion consumers while preserving history. A later
login registers a new generation. Authorization counts are not online presence.

Migration `0018` adds the hidden shared application-key server-device registry.
Existing key rows retain numeric generation `1`, their ciphertext, and all
client contexts. Later shared generations use the ordinary device sequence to
avoid identifier reuse. The shared record is absent from device lists and
native device mutations; direct privileged compatibility operations can address
it and revoke every attached parent key and its contexts. An ordinary device
removal never revokes a key solely because a context reports the same ID.
Native dashboard logins remain independently managed. These upgrades preserve
probe-cache version 6 and do not issue or require a rescan; see
[device implementation and migration details](devices.md).

The supplied systemd unit creates its persistent state directory with mode
`0700`. The default application-key master file is created lazily there with
mode `0600` by the service user. If selecting another path, create its private
parent directory and grant that exact directory in the service sandbox.
Preserve this master file together with PostgreSQL backups; replacing it cannot
recover existing encrypted keys. Normal logins remain usable if the key vault
is unavailable, and metadata-only key listing and revocation do not decrypt
secrets. Product backup/restore and master-key rotation are separate unfinished
features.
