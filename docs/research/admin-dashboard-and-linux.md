# Administrator Dashboard and Linux Implementation Research

Research date: 2026-09-09.

## Scope and conclusion

Build a Go media server with an Emby-compatible HTTP API and a React + Material UI administrator dashboard. The dashboard covers server operations, users, libraries, metadata, tasks, sessions, configuration, logs, and recovery. It does not include a consumer home screen, watch page, browser media player, or personal playback interface. Existing third-party clients still require the backend playback, subtitle, artwork, session, and user-state APIs described elsewhere in this documentation.

The recommended deployment is one Go application on Linux, with an embedded dashboard, local persistent application state, mounted media directories, and separate FFmpeg processes. Start with a modular monolith rather than separately deployed management, scanner, and API services. Both HTTP surfaces call the same application services and authorization layer.

This document separates **observed contracts** from **proposed implementation**. An observed route is evidence that an API is documented; it is not evidence that its runtime behavior has been tested against Emby or implemented by this project.

## Evidence baseline and version limits

The current retained runtime corpus has 828 records. The [progressive-video reference study](video-progressive-reference.md) adds 92 records, including 61 complete HTTP exchanges, twelve PlaybackInfo requests and one Range control. The remaining new records include probes, observations and preserved incomplete responses. The study does not establish successful nonzero copied-video seeking. Current implementation and acceptance results are tracked separately in [implementation progress](../development/progress.md).

The primary endpoint baseline is the official `MediaBrowser/Emby.SDK` snapshot at commit `bdd0dd7c0801f6e069dff2795d80cddae6f91791`. The commit is associated with SDK 4.9.5.0, and its `SampleCode/RestApi/Version.txt` identifies `4.9.5.0 Release`. The local copy is [emby-sdk-openapi.snapshot.json](../sources/emby-sdk-openapi.snapshot.json). The source file itself has incomplete `info` metadata and an empty `embyauth` security definition, so it is a research input, not an independently validated generator-ready contract. [Pinned SDK source](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json), [SDK version marker](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/SampleCode/RestApi/Version.txt).

Emby's REST introduction links to a static Swagger browser. Its downloaded `openapi.json` identifies version `4.1.1.0`, and differs from the newer SDK and current reference pages. Do not silently combine their response types or treat the static browser as the newest server contract. [REST introduction](https://dev.emby.media/doc/restapi/index.html), [static Swagger document](https://swagger.emby.media/openapi.json).

| Area | Static 4.1.1.0 document | Newer SDK and reference evidence | Consequence |
| --- | --- | --- | --- |
| User list | `GET /Users` | `GET /Users/Query` | The newer result is `QueryResult_UserDto`; a legacy alias may require a different envelope. |
| Library list | `GET /Library/VirtualFolders` | `GET /Library/VirtualFolders/Query` | Use an explicit compatibility profile, including pagination and envelope differences. |
| Log list | `GET /System/Logs` | `GET /System/Logs/Query` | The newer result is `QueryResult_LogFile`. |
| Log download | `GET /System/Logs/Log` | `GET /System/Logs/{Name}` | Parameter location changed; do not derive the newer endpoint from an old generated client. |
| Mutation aliases | Primarily `DELETE` operations | Additional `POST .../Delete` operations | Register documented method/path pairs explicitly and preserve authorization on every alias. |
| Backup | No `BackupApi` in the inspected static document | `BackupRestore` routes and `MBBackup.Api.*` schemas | Documentation does not establish universal availability or a portable backup format. |

The current [UserService](https://dev.emby.media/reference/RestAPI/UserService.html), [LibraryStructureService](https://dev.emby.media/reference/RestAPI/LibraryStructureService.html), [SessionsService](https://dev.emby.media/reference/RestAPI/SessionsService.html), and [SystemService](https://dev.emby.media/reference/RestAPI/SystemService.html) reference pages independently confirm the newer route families above. Exact support for older clients and newer server versions remains a compatibility decision and requires captured runtime examples from chosen target versions.

All Emby paths below are relative to the documented `/emby` base path. For example, `GET /System/Info` means `GET /emby/System/Info`. Any support for unprefixed aliases should be tracked separately. The reference says JSON and XML are supported; JSON-first implementation must be declared a compatibility subset until XML behavior is implemented and checked. [REST introduction](https://dev.emby.media/doc/restapi/index.html).

## Dashboard pages and observed API counterparts

These are the administration-facing operations to prioritize. They are selected evidence, not the complete endpoint catalog. The native dashboard can use `/admin/v1` application endpoints for richer workflows, with the Emby counterparts below backed by the same services.

| Dashboard page | Observed Emby method and path | Data or behavior to preserve | Delivery recommendation |
| --- | --- | --- | --- |
| Sign in and current administrator | `POST /Users/AuthenticateByName`; `GET /Users/{Id}`; `POST /Sessions/Logout` | Authentication result and token lifecycle; `UserDto.Policy.IsAdministrator` is relevant but the server must enforce authorization independently of the UI. | First milestone. Use native dashboard session handling as described below. |
| Overview | `GET /System/Info`; `GET /Sessions`; `GET /ScheduledTasks`; `GET /System/ActivityLog/Entries` | `SystemInfo`, `SessionInfo[]`, task status, and paged activity records are separate models. | Show active streams, scan progress, failures, storage status, and restart requirements. |
| Users | `GET /Users/Query`; `GET /Users/{Id}`; `POST /Users/New`; `POST /Users/{Id}`; `DELETE /Users/{Id}`; `POST /Users/{Id}/Delete` | List filters include `IsHidden`, `IsDisabled`, `StartIndex`, `Limit`, `NameStartsWithOrGreater`, and `SortOrder`; create returns a `UserDto`. | Account creation, name/profile editing, disable/enable, and account removal. |
| Permissions and passwords | `POST /Users/{Id}/Policy`; `POST /Users/{Id}/Configuration`; `POST /Users/{Id}/Configuration/Partial`; `POST /Users/{Id}/Password` | `UserPolicy`, `UserConfiguration`, and `UpdateUserPassword` have different meanings. A password operation is documented as user-authenticated, not sufficient evidence that any user can modify any account. | Enforce owner/admin rules, reject privilege escalation, and preserve the final enabled administrator. |
| API keys | `GET /Auth/Keys`; `POST /Auth/Keys?App={name}`; `DELETE /Auth/Keys/{Key}`; `POST /Auth/Keys/{Key}/Delete` | The create operation requires query parameter `App`; list supports `StartIndex` and `Limit`. Success response schemas are unspecified in the SDK. | Label each integration, issue a separate credential, revoke independently, and audit mutations. Do not invent an Emby response shape. |
| Libraries | `GET /Library/VirtualFolders/Query`; `POST /Library/VirtualFolders`; `POST /Library/VirtualFolders/Name`; `POST /Library/VirtualFolders/LibraryOptions`; `POST /Library/VirtualFolders/Delete` | New library body includes `Name`, `CollectionType`, `RefreshLibrary`, `Paths`, and `LibraryOptions`; rename uses `Id` and `NewName`; removal uses `Id` and `RefreshLibrary`. | Core support for movie, series, and music libraries; use explicit form sections for supported options. |
| Media paths | `POST /Library/VirtualFolders/Paths`; `POST /Library/VirtualFolders/Paths/Update`; `POST /Library/VirtualFolders/Paths/Delete`; `GET /Environment/DirectoryContents`; `POST /Environment/ValidatePath` | Path operations use library `Id`; addition can include `Path`, `PathInfo`, and `RefreshLibrary`. Directory listing accepts `Path`, `IncludeFiles`, and `IncludeDirectories`. | Browse only configured Linux media roots; display the path as seen inside the service/container. |
| Library scanning | `POST /Library/Refresh`; `POST /Items/{Id}/Refresh`; `GET /ScheduledTasks` | A full library scan and metadata refresh are different operations. Item refresh exposes `Recursive`, refresh modes, and replacement flags. Scan success is documented as an empty `200` response, not completed work. | Enqueue work, expose task progress, and retain scan history in the native management API. |
| Metadata editor | `GET /Items`; `GET /Items/{ItemId}/MetadataEditor`; `POST /Items/{ItemId}`; `POST /Items/RemoteSearch/Movie`; `POST /Items/RemoteSearch/Series`; `POST /Items/RemoteSearch/Apply/{Id}` | Search/list results, editor capabilities, `BaseItemDto` updates, and provider identification are separate contracts. | Administrative search, edit, identify, refresh, and field locking; no play controls are required. |
| Images and subtitles | `GET /Items/{Id}/Images`; `POST /Items/{Id}/Images/{Type}`; `DELETE /Items/{Id}/Images/{Type}`; `GET /Items/{Id}/RemoteImages`; `POST /Items/{Id}/RemoteImages/Download`; `GET /Items/{Id}/RemoteSearch/Subtitles/{Language}` | The image upload summary specifies base64-encoded content; exact content types and bodies require endpoint-specific implementation. | Poster selection and subtitle management can follow basic metadata editing. The backend still serves images and subtitles to clients. |
| Scheduled tasks | `GET /ScheduledTasks`; `GET /ScheduledTasks/{Id}`; `POST /ScheduledTasks/Running/{Id}`; `DELETE /ScheduledTasks/Running/{Id}`; `POST /ScheduledTasks/Running/{Id}/Delete`; `POST /ScheduledTasks/{Id}/Triggers` | Task IDs, execution state, progress, last result, and triggers must be stable; the trigger body is an array in the SDK. | Run, request cancellation, edit schedules, and view failures. Cancellation is cooperative and may take time. |
| Sessions and devices | `GET /Sessions`; `GET /Devices`; `GET /Devices/Info`; `GET /Devices/Options`; `POST /Devices/Options`; `DELETE /Devices`; `POST /Devices/Delete`; `POST /Sessions/{Id}/Message`; `POST /Sessions/{Id}/Playing/{Command}` | Device `Id` is a required query parameter for info/options/deletion. Session list is a user-authenticated API and needs per-user visibility rules outside the admin UI. | Show active sessions, transcode decisions, and device access. Deleting a device, ending a session, and revoking credentials are distinct actions. |
| Server settings | `GET /System/Configuration`; `POST /System/Configuration`; `POST /System/Configuration/Partial`; `GET /System/Configuration/{Key}`; `POST /System/Configuration/{Key}` | Named configuration payloads are not fully typed in the SDK. Some settings have OS-specific or Emby-specific meanings. | Implement an explicit supported settings model; indicate whether changes are immediate or require a restart. |
| Logs and activity | `GET /System/Logs/Query`; `GET /System/Logs/{Name}`; `GET /System/Logs/{Name}/Lines`; `GET /System/ActivityLog/Entries` | Log download accepts `Sanitize`; line retrieval is `QueryResult_String`; activity accepts `StartIndex`, `Limit`, and `MinDate`. | Paged activity, bounded log viewing, sanitized downloads, and sensitive-field redaction. |
| Maintenance | `POST /System/Restart`; `POST /System/Shutdown` | These are documented application operations; they do not authorize rebooting or powering off the Linux host. | Expose only when the process supervisor integration can fulfill the requested behavior. |
| Optional extension management | `GET /Plugins`; `GET /Plugins/{Id}/Configuration`; `POST /Plugins/{Id}/Configuration`; `DELETE /Plugins/{Id}`; `POST /Plugins/{Id}/Delete` | The HTTP contract does not establish Emby binary plugin compatibility. | Evaluate a Goby-owned extension registry later; do not advertise Emby binary plugin support. |
| Emby package distribution | `GET /Packages`; `POST /Packages/Installed/{Name}` | Emby's package distribution is a separate system. | Excluded from current scope. Goby releases and optional extensions use their own distribution mechanism; never fabricate package-installation success. |
| Backup and recovery | `GET /BackupRestore/BackupInfo`; `POST /BackupRestore/Restore`; `POST /BackupRestore/RestoreData` | The newer SDK uses `MBBackup.Api.AllBackupsInfo`, `RestoreOptions`, and `DataRestoreOptions`. `RestoreOptions` includes `RestoreServerId` and `UseFiles`; `DataRestoreOptions` includes `Users`. | Provide a project-native backup system first. Treat Emby backup interoperability as a separate researched feature. |

The route and model details in this table are taken from the [pinned SDK source](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json). The schema's broad `200/400/401/403/404/500` response lists are not proof that every listed code occurs in every implementation scenario.

## Authentication and authorization design

**Observed:** Emby documents a client identity `Authorization: Emby ...` header, login through `/Users/AuthenticateByName`, an `AccessToken` in the authentication result, and subsequent `X-Emby-Token` headers. Explicit logout revokes the token. API keys may use `X-Emby-Token` or `api_key` in the query string. The documentation recommends one key per integration. [User authentication](https://dev.emby.media/doc/restapi/User-Authentication.html), [API key authentication](https://dev.emby.media/doc/restapi/API-Key-Authentication.html).

**Proposed:** Keep third-party authentication behavior in the Emby adapter. For the same-origin administrator UI, use an opaque server-side session delivered through an `HttpOnly` cookie, with `Secure` in TLS deployments and an appropriate `SameSite` setting. Add CSRF protection and origin checks for native mutations. The Go server can translate this session into the same principal used by application services; an additional BFF service is unnecessary. Never embed a static administrator API key into the React bundle.

Server middleware must require administrator authentication and authorization throughout `/admin/v1`, with a minimal explicit exception list for setup status, one-time setup submission, and session login. Setup submission still requires the one-time deployment secret and becomes unavailable after initialization; login requires credential verification and rate limiting. A React route guard only controls presentation. Recheck administrative status on protected mutations and invalidate administrative sessions after disabling the account or removing its privilege. Separate a named API credential's rights from a user's browser session and make any project-specific credential scopes explicit extensions rather than claimed Emby fields.

Model authorization at the service and resource layers. The observed `UserPolicy` contains `IsAdministrator`, `IsDisabled`, `EnableRemoteAccess`, `EnabledFolders`, `EnableAllFolders`, `ExcludedSubFolders`, `EnabledDevices`, `EnableAllDevices`, `EnableMediaPlayback`, transcode/remux permissions, `EnableContentDeletion`, `EnableContentDeletionFromFolders`, and remote-control permissions. Apply those decisions consistently to queries, artwork, downloads, playback, session control, and writes. Merely accepting or returning policy fields does not implement their restrictions. [UserPolicy in the pinned SDK](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json).

Use an audited bootstrap path for creating the first administrator, such as a one-time CLI bootstrap token tied to local deployment access. No `Startup` route family was present in the inspected newer SDK snapshot, so a setup wizard must be documented as a project-native interface unless another version-specific source is obtained. Do not ship default administrator passwords.

## Management workflows and backend responsibilities

### Libraries, paths, and scans

The dashboard should distinguish creating a library, attaching/removing a path, scanning files, refreshing metadata, and deleting media. In particular, the SDK describes `DELETE /Items` and `DELETE /Items/{Id}` as deleting from both the library and file system. A library removal is a different operation. Use a deletion preview, exact affected paths, a server-side deletion policy, and an explicit destructive action in the UI; use `GET /Items/{Id}/DeleteInfo` where appropriate. [LibraryService reference](https://dev.emby.media/reference/RestAPI/LibraryService.html).

Treat an approved media root as a security boundary. Normalize Linux paths, check actual filesystem permissions under the service UID, define symlink behavior, reject traversal outside allowed roots, and defend against symlink changes between validation and file access. Avoid returning an unrestricted host directory browser. An existing NFS/SMB mount is just a Linux path to the server; mounting shares belongs to deployment administration, not an API that shells out with a supplied password.

Use a durable scan queue with incremental checkpoints, bounded parallel file probing, duplicate work suppression, and clear retry/cancellation states. Filesystem watchers are a latency optimization; the authoritative mechanism remains periodic reconciliation. Linux `inotify` can overflow its event queue and does not observe changes made remotely on network filesystems. On overflow or a missed subtree, mark it dirty and schedule a reconciliation scan rather than assuming the catalog is current. [Linux inotify manual](https://man7.org/linux/man-pages/man7/inotify.7.html).

An unavailable mount must produce an unavailable-library state, not immediate mass deletion of catalog entries. Track mount availability and complete a successful scan before applying missing-file removal policy. Maintain opaque stable item IDs in the database; a pathname should not be the public item ID.

### Metadata and task execution

Keep parsed filenames, file probe information, provider metadata, and administrator overrides distinct in storage. User edits and locked fields must survive later automatic refreshes. Provider credentials stay on the backend. Provider requests need bounded timeouts, retries with backoff, rate limiting, response limits, and URL policy checks for metadata/image fetches.

Task records should include a stable ID, type, state, progress, timestamps, error summary, and cancellation request. Keep progress monotonic within an execution and expose the difference between pending, running, cancellation requested, cancelled, succeeded, and failed. The compatibility adapter maps these states to the selected Emby task contract. Trigger editing must define timezone, interval semantics, missed execution handling, and concurrent-run policy; the dashboard should show those choices explicitly.

### Sessions, logs, backup, and recovery

Session observability belongs in the admin UI even though it has no player. Include client/device identity, user, item, direct-play/remux/transcode decision, bitrate, elapsed time, and resource usage when available. A remote stop command is a control action on an existing client session; it does not require implementing a browser watch page. Server-side credential revocation and stream termination remain separate from advisory remote-control messages.

Store audit events separately from debug logs. Audit sensitive mutations, actor, target, outcome, and request identifier without logging passwords, raw tokens, or provider secrets. The compatibility `Sanitize` flag is observed evidence, but the native dashboard should redact secrets by default rather than depending on the caller to ask. Bound log reads and allow only identifiers from the known log index.

A native backup should have a versioned manifest, schema version, configuration, user and policy records, stable server/item identities where required, and a consistent PostgreSQL snapshot. Treat artwork/cache as selectable data and exclude media files by default with an explicit description. Use `pg_dump --format=custom` with a compatible PostgreSQL client and restore through `pg_restore` into a separate database before activation. Coordinate application-owned metadata files with the database snapshot; do not copy a running database's storage files as the application backup procedure. [PostgreSQL SQL dump and restore](https://www.postgresql.org/docs/current/backup-dump.html).

Restore should check the manifest and version before maintenance mode, stage data separately, preserve a rollback copy, and activate the result only after consistency checks. Restoring user credentials and server identity affects existing client sessions and must have explicit semantics. Compatibility with Emby's `MBBackup` payloads or archive format is unconfirmed and must not be advertised merely because the routes exist.

## Proposed native management API

These paths are **project design proposals**, not Emby endpoints. They should have a separate OpenAPI specification and versioning policy. The canonical namespace and initial route choices are maintained in the [architecture](../architecture/linux-go-react.md); the names can change before a public contract is frozen.

| Proposed method and path | Purpose |
| --- | --- |
| `GET /admin/v1/bootstrap`; `POST /admin/v1/bootstrap` | Minimal setup status and one-time first-administrator creation. |
| `POST /admin/v1/session`; `DELETE /admin/v1/session`; `GET /admin/v1/session` | Same-origin administrator session lifecycle. |
| `GET /admin/v1/overview` | Aggregate health, task, session, and storage summaries without changing Emby DTOs. |
| `GET /admin/v1/capabilities` | Report implemented features, configuration options, and permitted actions. |
| `GET /admin/v1/libraries`; `POST /admin/v1/libraries`; `PATCH /admin/v1/libraries/{id}` | Typed native library management backed by shared services. |
| `POST /admin/v1/libraries/{id}/scan`; `GET /admin/v1/jobs`; `POST /admin/v1/jobs/{id}/cancel` | Return stable native job identifiers and expose asynchronous work. |
| `GET /admin/v1/storage/roots`; `POST /admin/v1/storage/validate` | Inspect approved mounts and validate media paths. |
| `GET /admin/v1/settings`; `PATCH /admin/v1/settings` | Return supported, redacted configuration and typed validation errors. |
| `GET /admin/v1/audit`; `GET /admin/v1/diagnostics` | Filter audit events and report relevant deployment state without exposing secrets. |
| `POST /admin/v1/backups`; `GET /admin/v1/backups`; `GET /admin/v1/backups/{id}`; `POST /admin/v1/restores/plan`; `POST /admin/v1/restores/{id}/apply` | Native snapshot and staged recovery workflows. |
| `GET /admin/v1/events` | Optional server-sent event stream for task and operational updates. This does not replace the Emby WebSocket contract. |

Use native `202 Accepted` plus a job reference for asynchronous management requests where appropriate, but preserve observed Emby status/body behavior in the compatibility adapter. Do not automatically wrap every Emby result in a new project JSON envelope. Return structured native validation errors with field paths, error codes, and a request ID that the dashboard can use.

## React and Material UI implementation

**Observed:** Material UI is an open-source React component library implementing Material Design. Its documented default installation uses `@mui/material`, `@emotion/react`, and `@emotion/styled`; React and React DOM are peer dependencies. MUI uses `ThemeProvider` and `createTheme` for application-wide customization. [Material UI overview](https://mui.com/material-ui/getting-started/overview/), [installation](https://mui.com/material-ui/getting-started/installation/), [theming](https://mui.com/material-ui/customization/theming/).

**Proposed:** Use React with TypeScript, Material UI, and a small route-based SPA served from `/admin`. Pin compatible stable dependency versions and lockfiles during implementation; this research does not select unverified version numbers. Use one theme for palette, typography, spacing, shape, and light/dark behavior. A permanent navigation drawer on desktop and a temporary drawer on narrow screens can hold Overview, Libraries, Metadata, Users, Sessions, Tasks, Settings, and Maintenance.

Use MUI tables or the Community Data Grid for paged administrative data, `TextField`/`Select`/`Switch` for settings, `Dialog` for destructive operations, `Alert` for persistent failures, and `Snackbar` for transient completion feedback. Keep loading, empty, error, denied, stale, and unsaved-change states deliberate. Admin pages must remain keyboard accessible, with visible focus and text labels for icon actions. Metadata thumbnails support identification and editing rather than a consumer poster-wall browsing experience.

MUI X is open core: Community packages are MIT-licensed, while Pro/Premium features need commercial licenses. Keep the open-source default dashboard on Material UI and Community components; do not accidentally make core user management depend on a paid grid feature. This is a package selection constraint, not a claim about this project's eventual license. [MUI X overview](https://mui.com/x/introduction/), [MUI X licensing](https://mui.com/x/introduction/licensing/).

Generate or hand-maintain TypeScript clients from the reviewed native management specification, not directly from the incomplete upstream snapshot. Keep request transport, authentication, server-state caching, and DTO-to-view-model conversion out of page components. Abort stale reads on navigation and invalidate related queries after successful mutations. Start with bounded polling; add the native event stream when background tasks need faster updates. Keep dashboard events distinct from protocol-compatible client WebSocket messages.

## Go application architecture

The following names describe logical responsibilities. Use the [canonical package layout](../architecture/linux-go-react.md) when creating implementation directories.

| Module | Responsibility |
| --- | --- |
| `transport/emby` | Exact Emby routes, headers, query parsing, authentication adapters, DTOs, response formatting, and version profiles. |
| `transport/admin` | Native management endpoints, session cookies, CSRF checks, and typed request/response contracts. |
| `identity` | Users, password credentials, API tokens, policies, sessions, and authorization decisions. |
| `library` | Library configuration, stable item identity, catalog queries, directory reconciliation, and deletion policy. |
| `metadata` | Provider interfaces, matching, administrator overrides, field locks, and artwork/subtitle ingestion. |
| `jobs` | Durable task queue, scheduling, cancellation, progress, and retry policy. |
| `media` | FFprobe ingestion, media stream information, playback sessions, and bounded FFmpeg process supervision. |
| `operations` | Configuration, audit events, logs, diagnostics, backups, and maintenance state. |
| `storage` | Database migrations and repositories, filesystem abstraction, cache layout, and transactional boundaries. |
| `web` | Built React assets embedded in the Go executable or served from a packaged read-only directory. |

Use explicit constructors and interfaces at provider, repository, and process boundaries. A heavy dependency-injection framework is unnecessary. A handler should parse input and map an application result; it should not own a scanner, open a global database transaction for a long task, or directly construct an FFmpeg shell command.

The standard library supports the essential host functions: `net/http` for HTTP, `embed.FS` for static assets, and `os/exec` for subprocesses. Configure `ReadHeaderTimeout`, request-size limits, client timeouts, and graceful shutdown. Streaming endpoints need appropriately long lifetimes instead of a short global response timeout. `http.Server.Shutdown` does not itself manage hijacked connections such as WebSockets, so track and close those and active media jobs explicitly. [Go HTTP documentation](https://pkg.go.dev/net/http), [Go embed documentation](https://pkg.go.dev/embed).

Invoke an absolute FFmpeg/FFprobe executable with an argument array and no shell. `exec.CommandContext` can interrupt a process when its context ends; on Linux, also manage process groups and escalation deadlines when child processes must be terminated. Bound concurrency, temporary bytes, logs, and job runtime independently. The exact FFmpeg build and codecs require a Linux compatibility matrix. [Go process execution documentation](https://pkg.go.dev/os/exec).

The current [progressive-video adapter](../development/progressive-video-playback.md) shares the existing conversion manager and delivers fragmented MP4 through the standard video stream route. Probe version 5 preserves the container clock separately from audio presentation timing. Encoded nonzero seeks currently decode the prefix; long-media startup cost and additional copy-seek paths remain engineering work. The [copied-video controls](video-copy-seek/README.md) retain counterexamples where successful FFmpeg completion still loses or presents the wrong source frames.

PostgreSQL is the required database from the initial implementation, accessed through `pgx/v5` and a bounded `pgxpool`. It runs as a separate database service and may share the Linux application host or use a configured remote database host. Use short transactions, unique constraints, indexes guided by catalog queries, transactional migration records and an advisory lock for migration serialization. Concurrent writers coordinate through PostgreSQL constraints and row locks; the application does not need a single-writer queue. Do not hold transactions during scans or media subprocesses. Keep application media/cache files on their configured mounts, and let the PostgreSQL service manage its own data storage. [pgxpool documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [PostgreSQL concurrency control](https://www.postgresql.org/docs/current/mvcc.html).

Do not use Go's dynamic `plugin` package as the default third-party extension mechanism. Its documentation warns about build/toolchain compatibility and operational limitations. Begin with compiled provider interfaces; if external extensibility is needed, define a versioned process/RPC contract with resource and capability limits. Supporting Emby package-management HTTP routes does not imply compatibility with its plugin runtime. [Go plugin documentation](https://pkg.go.dev/plugin).

## Linux deployment design

Linux is the only production target. Windows development does not create a Windows server-support requirement. Begin with one clearly documented Linux architecture and expand the release matrix, for example from `linux/amd64` to `linux/arm64`, only after remote integration coverage exists.

| Deployment concern | Proposed behavior |
| --- | --- |
| Process identity | Dedicated unprivileged `goby` UID/GID, no root application process, no shell-based administrative helpers. |
| Configuration and state | Read-only deployment configuration under `/etc/goby`; writable application state under `/var/lib/goby`; derived cache and transcodes under `/var/cache/goby`; runtime data under `/run/goby`. Paths are configurable for containers. |
| Database | Required PostgreSQL service with independent persistent storage; explicit connection credentials/TLS, bounded pool, migrations, health reporting, and database backup/restore procedure. |
| Media permissions | Read-only media mounts by default; grant write access only for explicitly enabled metadata export or media deletion. Preserve the distinction between application policy and Unix filesystem permissions. |
| Native packaging | A Go binary, frontend assets, a version-pinned FFmpeg strategy, service account setup, and a systemd unit. Verify on the selected Linux distribution rather than assuming all systemd/kernel features exist. |
| Containers | Non-root OCI image; persistent config/state volumes; read-only media volumes unless write features are enabled; bounded cache/transcode storage; no Docker socket or privileged container requirement. |
| Network | Same-origin `/admin` and `/admin/v1` behind TLS; preserve `/emby` and WebSocket upgrades. Trust forwarded headers only from configured proxies, and do not infer client access policy from arbitrary `X-Forwarded-For`. |
| Logs | Structured application logs to stdout/stderr for journald/container collection; bounded local log files or a safe indexed export adapter for Emby log endpoints. Redact tokens in URL query strings and headers. |
| Hardware acceleration | Explicit optional device access, driver/runtime documentation, and independent per-codec hardware decode and encode checks followed by combined pipeline checks. Grant only required render/GPU devices; the base CPU-only deployment should not need them. |
| Resource control | Separate transcode concurrency/size limits, task pools, HTTP request limits, and supervisor CPU/memory limits. Include minimum free-space handling before creating segments or backups. |
| Lifecycle | Handle SIGTERM, stop admission of new long-running work, cancel or drain tasks, close WebSockets, terminate child process groups, flush state, and exit within a documented deadline. |

A deployment that upgrades older probe snapshots must schedule normal library scans before relying on indexed playback. Native user management also adds migration 0013; database migration and media rescanning are separate steps. The [running guide](../development/running.md) describes both. Existing media responses periodically revalidate policy, including original-file responses, while supervisor limits still need separate CPU/memory enforcement for expensive prefix decoding.

For systemd, use `NoNewPrivileges=true`, a constrained capability bounding set, and filesystem protection such as `ProtectSystem=strict` with only required writable paths/directories. `PrivateDevices=true` is suitable for a CPU-only service but hides the normal device tree; GPU deployments need a deliberately different device policy and actual device access. Kernel and container environments can affect whether some protections apply. [systemd execution settings source](https://github.com/systemd/systemd/blob/main/man/systemd.exec.xml).

Use supervisor restart policy deliberately. `Restart=on-failure` permits a clean application shutdown to remain stopped, while an explicit restart can use a reserved exit status declared in `RestartForceExitStatus`. A clean shutdown must not unexpectedly be turned into a restart by an unconditional policy. In containers, expose only operations the configured supervisor can honor; a capability response can disable unsupported maintenance actions. Do not let the API execute arbitrary `systemctl`, reboot, or host shell commands. [systemd service lifecycle source](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).

Avoid Linux-inapplicable UI controls. Network share discovery, Windows drive letters, `AutoRunWebApp`, and automatic startup registration should not appear as working features merely because a broad Emby configuration DTO contains similarly named fields. Map supported settings explicitly; report unsupported mutations clearly. Do not round-trip credentials from `ServerConfiguration` into frontend state.

## Remaining decisions and acceptance work

The following items are unresolved research or implementation work, not implied support:

1. Choose a tested Emby server/API version range and representative third-party clients. Verify legacy list aliases and pagination envelopes rather than assuming all routes from multiple versions are interchangeable.
2. Capture actual API-key list/create responses, named/partial configuration behavior, delete aliases with incomplete parameter definitions, task trigger semantics, log bytes/content types, and image upload content handling. The SDK alone does not answer these completely.
3. Choose JSON-only initial compatibility or include XML. Public documentation mentions both; the supported matrix must say which is implemented.
4. Determine backup route availability and whether the `MBBackup` format has a published interoperable contract. Keep native backup/restore independent of that investigation.
5. Integrate the required Go 1.27.1, FFmpeg 9.0.1, and PostgreSQL/pgx/v5 baseline; pin compatible React/MUI, deployment PostgreSQL, and optional GPU driver versions. Select supported Linux distributions and CPU architectures, then verify the complete release configuration. The [toolchain policy](../development/toolchain.md) records the official release sources and hardware verification requirements.
6. Complete native settings concurrency, restore identity handling, media deletion safeguards, and unavailable-mount policy before exposing those mutations. The implemented [native user contract](../api/admin-users.md) supplies revision checks, final-administrator protection and session revocation for its supported account/policy operations; broader administrator scope remains open.
7. Verify ordinary users cannot reach admin APIs or use alternate Emby aliases to bypass policy; include per-library item/artwork/download/playback restrictions and cross-user session controls.
8. Exercise remote Linux scenarios for interrupted scans, inotify overflow recovery, absent NFS/SMB mounts, database backup/restore, cancellation of FFmpeg process groups, supervisor shutdown/restart, low disk space, and browser refresh under the `/admin` base path.

Progressive-video acceptance also requires actual HEAD and Range behavior, owned playback references, current-policy enforcement, source-clock mapping, and complete decoded A/V position checks. A route returning 200 or an encoder returning zero is insufficient. Retain explicit nonzero copied-video rejection until a verified implementation replaces it, and use real-client workflows to establish the final compatibility boundary.

Only document reading and writing were performed for the original research. The current implementation task authorizes local builds to detect compilation/build errors; tests, validators, smoke tests, and runtime/media probes must run through `ssh test-env`. Packages/software may be installed or removed on that remote environment as needed. An unavailable `test-env` blocks the affected verification rather than authorizing a local fallback.
