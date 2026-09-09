# Delivery plan and remote compatibility verification

Status: **service foundation, ingestion, metadata/artwork, and original playback increments delivered; full client playback remains in progress**. The current implementation request replaces the initial database proposal with PostgreSQL, requires stable Go/FFmpeg releases, and includes Linux hardware decoding. Completion gates below remain requirements until evidence is recorded; documentation updates alone do not close the reference-server evidence gaps.

## Milestones and dependencies

| Milestone | Work | Completion gate |
| --- | --- | --- |
| M0: Contract baseline | Pin source revisions, define target reference-server image/version, normalize selected schemas, collect permitted golden exchanges | Reviewed route/auth/DTO matrix and resolved bootstrap contracts; no invented unknown response schemas |
| M1: Linux service foundation | Go service, PostgreSQL/pgxpool, versioned migrations, users/policies/tokens, first-admin setup, React/MUI shell, logging and packaging | Administrator setup/login, PostgreSQL persistence/restart and migration concurrency, least-privilege service operation, public/auth/admin access separation |
| M2: Media catalog | Linux roots, scanner, ffprobe, local metadata, images, item hierarchy, query filters/search, administrator library pages | A representative movie/TV/music library survives rescan/restart; inaccessible mounts do not trigger mass removal; ACL-safe lists/counts |
| M3: P0 client playback | Discovery/identity as needed, client login/browse, PlaybackInfo, direct video/audio, subtitles, user state, basic sessions/events | Supported clients complete login to browse to play to seek to stop to resume on the documented direct-play media set |
| M4: P1 media pipeline | Remux, audio/video transcode, hardware decode and encode profiles, HLS/segments, track changes, burn-in, resource budgets, session lifecycle, richer music/playlist behavior | Remote media/transport matrix passes; hardware decode and encode results recorded separately and together; bounded jobs clean up after cancel/crash; unsupported profiles fail predictably |
| M5: Administrator completeness | Metadata editing, users/devices/tasks/keys, decode/encode diagnostics, audit/log UI, settings, PostgreSQL backup and restore | Administrator journeys pass; pg_dump/pg_restore recovery is rehearsed; no consumer web player exists |
| M6: Compatibility release | Prioritized client variants, legacy aliases where evidenced, packaging/hardware matrix, operational docs | Publish supported client/server/API/media profiles and known gaps with traceable results |
| M7: Feature expansion | P2/deferred families selected by evidence and user need | Each added feature gets its own contracts, permissions, migrations and interoperability gate |

M2 catalog work and the administrator UI can proceed in parallel after M1 service contracts stabilize. M4 needs M3 session and source identities. Do not build a production transcode scheduler around placeholder `MediaSourceInfo` data.

The target is broad API compatibility; staging prevents claiming full compatibility when only a route list or direct-play subset exists. Client names are not needed to begin architecture and catalog work. Before publishing a release, record the actual client builds exercised; different clients expose different gaps in the same advertised API.

## Verification environment policy

The current task explicitly authorizes **local builds only to detect compilation/build errors**. This includes Go compilation and the administrator frontend production build. It does not authorize local unit/integration tests, validators, smoke tests, server execution, HTTP probes, or FFmpeg/media capability probes.

Run all tests, acceptance checks, and runtime/media verification through **`ssh test-env`** on the remote Linux environment. The user authorizes installing and removing packages/software in that environment as needed for this task. If that host is unavailable, record the affected verification as blocked and do not fall back to local execution. Use the [toolchain policy](../development/toolchain.md) for pinned releases and hardware evidence requirements.

This research task retrieved official documentation, extracted endpoint/schema metadata, and authored local documents. It did not run tests, a build, a schema validator, a server, a media probe, or a live Emby request. Document generation and manual source reading are not runtime compatibility evidence.

During implementation, establish the remote repository/work directory and required tools explicitly. Transfer only project files and synthetic/licensed fixtures. Do not assume the remote path, deployed server, credentials, or network topology. A protected test runner should use a dedicated reference Emby installation and a separate Goby installation with independent disposable PostgreSQL databases and state.

## Development and commit policy

The user authorizes direct development on `main` during this initial development stage. Complete each milestone in a reviewable checkpoint, document its build/test evidence and remaining limitations, then create a milestone commit and push it to the configured remote before proceeding to the next milestone. Preserve unrelated user changes and inspect the actual branch/remote state before committing. A partial milestone checkpoint must be labeled partial and must not be reported as passing its completion gate.

M0 documentation maintenance updates the chosen architecture and toolchain without implying that the reference-server contract freeze is complete. Track actual delivery status in the [implementation progress](../development/progress.md) and remote verification evidence as work lands; the original upstream inventory is a research catalog rather than an implementation test report.

## Compatibility harness design

1. Pin the reference server version/build, runtime API export, client versions, media fixture hashes, and host/GPU/container configuration.
2. Exercise identical user workflows against both servers using isolated users/libraries. Record sanitized requests, responses, headers, and state transitions.
3. Normalize only intrinsically variable data such as assigned IDs, tokens, dates, and host addresses using explicit mappings. Do not normalize away wrong types, missing fields, sort order, authorization failures, or timing-unit errors.
4. Compare HTTP contract and state effects. Treat optional fields, unknown schemas, documented bugs, and client quirks as reviewed cases with reasons.
5. Play the returned media URLs with real target clients and inspect the remote media pipeline. A JSON fixture match cannot establish decoding, seeking, A/V sync, or subtitle correctness.
6. Maintain a machine-readable compatibility report per operation and scenario: `unimplemented`, `implemented-unverified`, `verified`, `partial`, `unsupported`, or `blocked`, with evidence and version/profile scope.

Never use third-party servers listed in downloaded Swagger files as test targets. No client capture, external account, or production library was assumed in this research.

## Required scenario matrix

| Area | Cases | Evidence |
| --- | --- | --- |
| Bootstrap and identity | Fresh setup, setup already completed, public info, hidden users, correct advertised base URL, reverse proxy | Request/response and persisted state fixtures |
| Authentication | Correct/incorrect password, disabled user, revoked token, logout, API key, token carriers, conflicting principals, rate limits | Status/body/header comparison; permission checks |
| Authorization | User A requests user B state; library restrictions; image enumeration/stream/subtitle/download bypass attempts; admin-only writes | No protected metadata/media bytes exposed; indexed item image contents follow the separately recorded public ImageService contract |
| Browse/search | Movies, series, seasons, episodes, albums, audio; Unicode names; empty library; filters, Fields, sort, pagination, Resume/Latest | Exact field types, envelopes, order, totals and user data |
| Filesystem | Symlink escapes, long/Unicode paths, partial copies, rename/move, permission loss, NFS/SMB disconnect, inotify overflow | Stable identity; reconciliation; no accidental bulk removal |
| HTTP media | GET/HEAD, full/range/suffix/invalid ranges, conditional requests, zero-length file, cancellation, proxy path prefix | Status/headers/length/range correctness and client playback |
| Direct media | MP4/H.264/AAC, MKV variants, MP3/FLAC/AAC audio as supported by each client, multiple source versions | Negotiated method agrees with actual client capability |
| Conversion | Remux, audio-only conversion, full video transcode, bitrate/resolution limits, unsupported codec | PlaybackInfo truthfulness, usable output, bounded resource consumption |
| HLS | Master/media manifests, generated segment paths, missing/expired segment, seek, reload, concurrent segment requests, disconnect | Decodable timeline, no stale session data, cleanup after expiry |
| Subtitles | UTF-8 text, alternate encodings, embedded/external tracks, forced/default flags, offset/seek, bitmap burn-in, fonts | Correct timing, selected language/track, authorized access |
| State | Start/progress/pause/seek/stop, duplicate/out-of-order events, disconnect/reconnect, server restart, simultaneous users | Correct resume position/play count, isolated sessions, idempotent completion |
| WebSocket | Token/session binding, message envelopes, subscriptions, heartbeat, reconnect, remote commands | Actual client exchanges and event-order expectations |
| Administration | Users/policies/keys, roots/scans, metadata edits, task cancel, log access, settings/restart, restore plan/apply | Audited actions, predictable feedback and permission boundaries |
| Operations | Disk full, transcode timeout, child-process crash, service termination, PostgreSQL connection loss/lock contention, pg_dump/pg_restore, migration race/failure | Bounded failure, recoverable state, documented degradation, schema and identity preservation |
| Linux packaging | amd64/arm64, container/systemd, read-only media, optional LAN discovery, selected GPU profiles | Reproducible install/start/upgrade and explicit hardware results |

Use software transcoding as the shared correctness baseline while implementing hardware support. Each VAAPI/QSV/NVIDIA profile must list GPU, driver, FFmpeg build, supported input/output codec profiles and bit depths, tone-mapping behavior, and failure/fallback policy. Record source hardware decoding and output hardware encoding as independent tests, then exercise their combined pipeline, including frame transfer, scale/filter paths, subtitles, and cancellation. Encoder enumeration or an available GPU device does not prove decoding. CPU-only success does not verify GPU compatibility, and one GPU does not verify a vendor's entire range. If `test-env` lacks a required GPU, keep the matching hardware acceptance row blocked with the missing prerequisite recorded.

## API contract review checklist

This is a design checklist for future reviews, not a suite executed during this task.

- Exact method/path/casing and API base, including HEAD and documented POST delete aliases.
- Parameter location, requiredness, lists/encoding, defaults, enum values, and malformed-input errors.
- Request JSON shape and unknown-field handling; response status/content type, model, null/omission/array behavior.
- Authentication and resource policy independent of untrusted caller identifiers.
- Persistent state effects, idempotency, ordering, cancellation, and event publication.
- URL generation, proxy host/base path, token propagation and redaction, cache/range behavior.
- Source version, unresolved document conflicts, accepted divergence, and evidence-linked support status.

## Highest-priority evidence gaps

| Gap | Resolve before |
| --- | --- |
| Real server build and runtime API export | M0 contract freeze; the SDK is a starting baseline |
| Public endpoint allowlist and login headers | M1 external authentication exposure |
| Missing response schemas, including episode listing and selected preference writes | Relevant M2/M3 feature completion |
| Streaming query fields omitted from generated docs and generated HLS segment URL shape | M3/M4 playback release |
| WebSocket path, session binding and client-used event payloads | Any claim of event/remote-control compatibility |
| Root aliases and older collection/search routes | A release that claims older client support |
| Exact unsupported-feature and error responses | Client-facing compatibility acceptance |
| Plugin/core origin of backup routes and archive format | Any Emby backup import claim |
| Chosen dependencies, project license and distribution notices | First implementation/source/package release |

## Initial implementation backlog

1. Create a Go 1.27.1 module, HTTP transport skeleton, native admin API contract, Linux packaging skeleton with PostgreSQL and FFmpeg 9.0.1 dependencies, and React/MUI admin shell.
2. Implement one shared authentication/authorization model, PostgreSQL repositories and transactional migrations with pgx/v5, and first-administrator workflow.
3. Build local library ingestion and the smallest accurate browse/PlaybackInfo DTO set, including media probing and safe IDs.
4. Close the direct-play client workflow with resume state and the management pages needed to operate it.
5. Extend the same planner/session model to transcoding and broader clients; expand contracts from recorded evidence instead of guessed payloads.

The original research phase installed no product code or dependencies. Implementation is now active; the milestone evidence, committed source, and build/test records determine what is installed and verified. A target version in documentation is not proof that the matching binary is installed on `test-env`.
