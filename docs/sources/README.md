# Source provenance and document limitations

Initial documentation was collected on **2026-09-09, Asia/Shanghai** through read-only HTTPS requests; the built-in search gateway was unavailable. Later implementation work added an [isolated official Emby 4.9.5.0 reference](../research/reference-server.md) and 65 audited HTTP captures. No sample server address embedded in an upstream export was used as a research target.

## Primary API baseline

| Field | Value |
| --- | --- |
| Repository | [MediaBrowser/Emby.SDK](https://github.com/MediaBrowser/Emby.SDK) |
| Fixed revision | [`bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/commit/bdd0dd7c0801f6e069dff2795d80cddae6f91791) |
| Commit timestamp | `2026-05-18T19:48:46Z` |
| Commit message | `SDK Version 4.9.5.0` |
| Version evidence | [SampleCode/RestApi/Version.txt at the fixed revision](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/SampleCode/RestApi/Version.txt), containing `4.9.5.0 Release` |
| API export | [Documentation/Download/openapi_v2_noversion.json](https://raw.githubusercontent.com/MediaBrowser/Emby.SDK/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json) |
| Local snapshot | [emby-sdk-openapi.snapshot.json](emby-sdk-openapi.snapshot.json) |
| Format | Swagger 2.0; the export itself has no `info.version` or `info.title` |
| Inventory | 422 paths; 535 HTTP operations; 70 services with operations; 333 definitions |
| SHA-256 of saved UTF-8 snapshot | `663e425f5a4abae99a9389ad9c16afb2bbcd3aac6b01c211bf295f426483531d` |

The SDK version identifies this research artifact. It is not a statement that 4.9.5.0 is the latest available server release or that every client uses precisely this surface. The export declares 70 tags, all represented by operations.

## Historical comparison

| Field | Value |
| --- | --- |
| Official browser | [Static API Browser](https://swagger.emby.media/?staticview=true) |
| Actual browser input | [openapi.json](https://swagger.emby.media/openapi.json) |
| Local snapshot | [emby-openapi.snapshot.json](emby-openapi.snapshot.json) |
| Self-reported version | `4.1.1.0` |
| Format | OpenAPI 3.0.1 |
| HTTP Last-Modified observed | `Mon, 08 Jun 2020 21:54:15 GMT` |
| SHA-256 of saved UTF-8 snapshot | `475716b917e1397a09c7be3c154fe84c7ada6770bde4603678b6275145d1988a` |

The historical export includes installation-specific/plugin entries. Its `servers` field points at a sample third-party installation. That address is not a research target or a Goby configuration default.

## Evidence hierarchy

1. For a future compatibility release, recorded behavior from the named reference server and client versions is the acceptance evidence.
2. For this research, use the fixed SDK export for exact route spelling, parameters, and schema structure.
3. Use the [official REST narrative](https://dev.emby.media/doc/restapi/index.html) for workflows and behavioral context.
4. Cross-check the [official REST Reference](https://dev.emby.media/reference/RestAPI.html). These live HTML pages can change and can contain rendering errors.
5. Retain older sources as explicit legacy evidence, never as silent replacements for missing current contracts.

Resolve conflicts explicitly. The hierarchy does not justify accepting unsafe authentication metadata or inventing an undocumented response.

## Known defects and differences

| Observation | Consequence |
| --- | --- |
| New export lacks `info.title`, `info.version`, and a useful `embyauth` definition; its `info` object contains a trailing comma | Strict JSON parsers may reject the raw snapshot. PowerShell accepted it for extraction; the operation inventory is reserialized JSON. This is not a validated, ready-to-generate Goby specification. Preserve the source unchanged as evidence. |
| `/emby` appears in `x-original-basePath`, rather than standard `basePath` | Use the documented `/emby` base explicitly when creating the compatibility transport. |
| New metadata labels login/public endpoints as requiring a user | Derive the bootstrap flow from the official authentication narrative; confirm the exact anonymous route allowlist on the reference server. |
| Some old streaming metadata says no authentication is needed | Enforce media authorization in Goby. Check how authenticated stream URLs are constructed and passed to clients. |
| New user and virtual-folder listing routes use `/Query`; old ones use the collection root | Track old collection-root routes as aliases pending capture, with version-specific response shapes. |
| `/Search/Hints` appears in the historical export and is absent from the pinned newer export | Search in the newer baseline uses `SearchTerm` on item queries; do not present Hints as a confirmed newer API. |
| New logs use `/System/Logs/Query` and `/System/Logs/{Name}` | Preserve old log routes only as deliberate compatibility adapters. |
| Some collection DELETE operations omit parameters while POST `/Delete` counterparts have bodies | Do not infer a usable request contract from an empty parameter list. |
| Some response schemas are missing or describe content as unknown | Record unresolved schema shape; acquire real fixtures before implementing that contract. |
| HTML can render object references as arrays | Use the pinned JSON structure as the research model baseline; verify actual payloads separately. |
| Upstream comments disagree on time units | Playback tick fields use the documented 100 ns convention: 10,000 ticks per millisecond; check each field rather than treating every duration as ticks. |

The exports are upstream research artifacts with their own provenance. They do not establish the license of Goby's future implementation. No Emby server implementation or web-player code has been imported. Preserve upstream notices and decide the project license before the first source release.

## Implementation sources

- [Go HTTP routing changes](https://go.dev/doc/go1.22) and [Go relational database access](https://go.dev/doc/database/): standard HTTP routing and persistence foundations.
- [PostgreSQL documentation](https://www.postgresql.org/docs/current/), [PostgreSQL concurrency control](https://www.postgresql.org/docs/current/mvcc.html), [SQL dump and restore](https://www.postgresql.org/docs/current/backup-dump.html), and [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool): the required relational database service, transactional concurrency, connection pooling, and backup/restore foundation.
- [Go stable release metadata](https://go.dev/dl/?mode=json) and [FFmpeg downloads](https://ffmpeg.org/download.html): verified on 2026-09-09 as Go 1.27.1 and FFmpeg 9.0.1 (released 2026-08-12). See the [toolchain record](../development/toolchain.md) for pinning and verification policy.
- [React](https://react.dev/) and [Material UI](https://mui.com/material-ui/getting-started/): administrator frontend foundations; see the dashboard research for specific component and package evidence.
- [FFmpeg documentation](https://ffmpeg.org/documentation.html): media probing, muxing, transcoding, and hardware integration; see the playback research for specific references.

These sources support implementation choices. They do not prove Emby wire compatibility.

## Superseded implementation research

The initial research considered [SQLite WAL](https://www.sqlite.org/wal.html), [appropriate SQLite uses](https://www.sqlite.org/whentouse.html), and the [SQLite backup API](https://www.sqlite.org/backup.html). The user's implementation request on 2026-09-09 requires PostgreSQL. These links are retained solely as historical provenance; SQLite deployment, single-writer assumptions, and file-based database backup procedures are not part of the active implementation design.
