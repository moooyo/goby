# Delivery plan and remote compatibility verification

Status: **long-term delivery scope; M2-M6 remain incomplete**. Use the
[current execution plan](current-execution-plan.md) for work order and the
[current status](../development/current-status.md) for accepted implementation,
verification and deployment facts. The 2026-09-13 review prioritized an audited
candidate, core client regression and a fresh upgrade contract. The 2026-09-14
dependency review allows a separately admitted source32 backup and isolated
recovery rehearsals before video acceptance; new-binary main promotion still
requires both core and complete safety/recovery evidence. Independent M2-M6
work follows its own prerequisites. NextUp and automatic-refresh gaps retain their feature gates
without blocking every unrelated internal increment. The project requires
PostgreSQL, stable Go/FFmpeg releases and Linux hardware decoding. Documentation
updates alone do not close any milestone or reference evidence gap.

The September 16 [combined M5 frontend build](../development/m5-final-frontend-verification-20260916.md)
passed one actual `tsc --noEmit && vite build` invocation, with separate independent
execution/resource and artifact/source reviews. Complete S1 preserves the 5400
frozen source rows at `138522b` and adds 59 actual dist files; the original
contribution sidecar and command receipt remain separately pinned. Processes,
unit and cgroup closed while the build workspace was retained. The future Vite
config-loader warning remains recorded. The later release build must use
`--frontend-contributions` and exact final asset matching after relocation.
Combined Go/DB/HTTP/browser and final build acceptance remain pending. The
[storage prerequisite](../development/m5-full-storage-verification-20260916.md)
has now passed in its third scope with independent review, four sentinels on three
filesystems, complete archive readback and owned-resource closure. The original
missing-directory and payload `PermissionError` failures are preserved; neither
has been relabeled, and the exact permission-failing syscall was not recorded.
The two source corrections did not widen budgets or permissions. The successful
small probe does not prove full 3 GiB capacity. The subsequent [actual combined
full attempt](../development/m5-combined-full-capacity-failure-20260916.md) failed
its unchanged 4 GiB memory gate: all 60 internal samples were insufficient,
although root disk space was adequate. Adapter exit 1, zero worker/Go/build/
volume/archive and independent resource closure are retained. The consumed scope
cannot be replayed; a fresh scope with sufficient capacity remains required.
No specific memory consumer or adapter RSS/peak was established.

The [Programs runtime/product preparation](../development/programs-current-runtime-preparation-20260916.json)
now includes the completed unique observer: two acknowledged read-only SQL
commands, one lease-method call, unchanged A identity and scoped resource closure.
The published `db22e3d...` envelope passed frozen-producer in-memory/file validation
against 40 unique saved records with no additional SQL/HTTP/service calls.
The actual product receipt is preserved and copied byte-for-byte into the full
scope. Query backend disappearance, observer/outer numeric PGIDs and a post-close
lock-inode check were not separately measured; the application lease backend is
expected to persist. Final consumer pins and 142 checks remain in preparation,
unexecuted. The original 42/28/68 results and transition hold remain unchanged;
no transition or client acceptance is claimed.

The latest fixed M5 media-refresh increment is committed and pushed as
`5faf854`. Its [final ordinary regression](../development/m5-final-regression-verification.json)
passed one complete 25-package run with 2,295 passes, zero failures and one
declared mount-profile skip; focused/browser acceptance, independent reviews
and resource closure also passed. The next
[internal systemd package](../development/systemd-package-build-verification.json)
has passed three builds, focused checks, 26 guards, independent review and
build resource closure. The later r04 runtime completed two nonroot starts/stops,
272 requests, six observers and three snapshots; its seal rejected a valid Emby
plain-text 401 and the final sealing SQL observer did not run. Full installation
acceptance is now established by the separately reviewed archived final-state supplement; the original sealer failure remains unchanged. See [internal amd64 acceptance](../development/internal-amd64-installation-acceptance.json). These increments retain their own source and
execution scopes and do not close complete M5, M6 or core-video acceptance.

The [fresh source32 backup](../development/audited-main-native-backup-completed.json),
both [selected rollback/restart](../development/isolated-selected-online-rollback.json)
and [actual source32 recovery/restart](../development/isolated-source32-recovery.json)
proofs, and [final fixture disposal](../development/isolated-restore-disposal.json)
are complete. [M6 embedded-asset focused verification](../development/embedded-administrator-verification.json)
has also passed its race checks, manifest guards, actual amd64 build and arm64
cross-build, with artifacts preserved and the build tmpfs closed. It is partial
M6 evidence: that immutable build record did not execute a native runtime.
The independent
[real-media catalog rescan/ACL target](../development/catalog-rescan-acl-verification.json)
has since passed its in-process Store-reopen checks, followed by a separate
[native amd64 catalog restart](../development/native-catalog-restart-verification.json).
That root-profile run passed once with the same PG, nine core tables' rows/xmin
preserved, 156 cumulative HTTP requests and complete independent evidence/resource
closure. It does not establish nonroot operation, PG restart, playback/browser,
nonzero resume, native arm64/GPU, capacity or host durability. That native
restart checkpoint does not claim main/candidate deployment or full regression.
The later [ordinary-suite continuation](../development/full-regression-continuation-verification.json)
completed 25 packages across two phases, with 2,276 passes, zero failures and
one explicit mount-profile skip, plus an ordinary amd64 build and a production
source bridge to the separately verified embedded artifact. A [fresh nonroot
candidate](../development/fresh-embedded-candidate-checkpoint.json) has since
been provisioned once. Corrected initial inspection and one seed passed
independent review; eight users, three libraries/scans and fourteen independent
media files are now bound to it. Both original inspection failures and their
separate diagnostic remain unchanged. Current operator guards total 213,
separate from the Go 2,276 results. Native admission has now exited 0 after
601,357 milliseconds, with 82 normal and six cleanup requests, no failure or
cleanup failures, and its inactive restore stage retained. Its report records
`admitted_for_core_client` while keeping `clientAcceptance=false`. Independent
saved-evidence review passed, confirming eleven health samples spanning exactly
600,000 milliseconds, owned credential cleanup, declared data deltas and the
retained cancelled stage. Inspection, seed and admission are complete and
consumed; none is queued for repetition.
The 600-second health window and 129-request cap remained unchanged; the actual
request count is 88.
The separate post-admission metadata check preserved candidate/PostgreSQL and
protected identities, without SQL, HTTP or service changes. Its observed
1,233,956,864 free root bytes are not a budget reservation; a new heavy phase
requires its own current capacity check.

The [independent catalog capacity/isolation baseline](../development/catalog-capacity-isolation-verification.json)
has now passed one targeted race test, independent review and resource closure.
Its SQL-seeded 10,000 leaves/442 folders retained ACL-safe query behavior and
all ten UserData rows during a controlled transaction block and rollback. This
is an in-process handler/SQL observation, not real media scanning, native
TCP/service performance, RSS/SLO or a kernel filesystem stall. The initial
reviewer-only log-ownership rejection remains preserved; the test was not rerun.
The new test is separate from the earlier 2,276-test ordinary suite.

The following [real small-media capacity increment](../development/catalog-real-media-capacity-verification.json)
has now passed its new 10,000-leaf test and affected seven-file shared-fixture
regression, with zero failures/skips and production unchanged. Independent
review passed once; both archives were reread, all processes closed and RAM
was ordinarily unmounted. Cold scan, cached rescan, one replacement
and in-process Store reopen provide tiny-file correctness and measured cost,
not HTTP/service performance, native service/PG/host restart or a throughput SLO.
The two targeted results are separate from the historical 2,276-test suite.

This increment is complete and consumed. Prioritize core video when new discriminating
evidence or a justified correction supports a bounded journey. No repeated
identity review, consumed input or new generic observer is queued. Other
M2-M6 work follows its own resource and safety prerequisites, with heavy work
serialized on the shared environment. Representative throughput, actual blocked
I/O and host durability retain their separate gates. The latest real-file
closure observed 1,124,679,680 free root bytes,
not a reservation for another phase. The saved subtitle response-identity review is complete; its
cancellation and page-error gaps remain open. The M2 full-scan mount experiment remains paused after
two launcher failures, with compilation only and no executed scan stages.
[Third-party notices](../../THIRD_PARTY_NOTICES.md) provide a verified
initial collection for locked dependencies and actual fonts. Project-license,
relevant upstream/inclusion gaps and the complete packaging/release gate remain
open; this partial evidence does not narrow the M2-M6 scope.

The full ordinary-suite report must explicitly retain the one expected opt-in
`TestRootBindingFullScanMountNamespaceHelper` skip; all other skips/failures are
rejected. Older mount helpers' default returns do not prove privileged-profile
execution. Strong-filesystem tests require an owned ext4 GOTMPDIR, with compiler
scratch/cache budgeted separately because root-disk space is low. None of this
reopens the paused mount experiment or narrows its missing acceptance evidence.

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

The resumed task on 2026-09-11 requires **all verification on the remote host**.
Local builds, unit/integration tests, validators, smoke tests, server execution,
HTTP probes, browser checks and FFmpeg/media probes are not authorized. Earlier
reports that record permitted local compilation describe their historical tasks.

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

## Original evidence-gap inventory

This is the initial research inventory, not today's priority queue. Some rows
now have scoped implementation or reference evidence. Consult the current
execution plan and status before opening work; retain any broader unsupported
claim as an open requirement rather than repeating completed research.

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

## Historical initial implementation sequence

The following sequence records the initial architecture plan. Its foundation,
authentication and ingestion work is already implemented; do not treat those
items as new tasks. Current increments and remaining gates are tracked above.

1. Create a Go 1.27.1 module, HTTP transport skeleton, native admin API contract, Linux packaging skeleton with PostgreSQL and FFmpeg 9.0.1 dependencies, and React/MUI admin shell.
2. Implement one shared authentication/authorization model, PostgreSQL repositories and transactional migrations with pgx/v5, and first-administrator workflow.
3. Build local library ingestion and the smallest accurate browse/PlaybackInfo DTO set, including media probing and safe IDs.
4. Close the direct-play client workflow with resume state and the management pages needed to operate it.
5. Extend the same planner/session model to transcoding and broader clients; expand contracts from recorded evidence instead of guessed payloads.

The original research phase installed no product code or dependencies. Implementation is now active; the milestone evidence, committed source, and build/test records determine what is installed and verified. A target version in documentation is not proof that the matching binary is installed on `test-env`.
