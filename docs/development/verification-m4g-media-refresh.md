# M4g administrator media refresh verification

M4g adds explicit re-probing of unchanged media to the existing administrator
scan workflow. It closes the operational gap where a same-version cache kept
missing or tool-incompatible optional indexes across ordinary scans. It does
not change the media seek proof, authorize a client-supplied index, or guarantee
that every format can produce an index.

The [native contract](../api/admin-scans.md) defines `ForceProbe`, task history,
strict input handling, preservation rules and limits. Migration 0015 adds one
boolean to scan jobs; every historical job defaults to normal scanning. The
dashboard adds a confirmation dialog and labels refresh tasks separately.

## Build and targeted checks

The authorized local Go Linux/amd64 build and `npm run build` both pass. No
local functional test, runtime probe or browser was executed. Linux verification
uses Go 1.27.1, FFmpeg/ffprobe 9.0.1, PostgreSQL 17.11, and port-15432 disposable
test state through `ssh test-env`.

The [targeted race run](m4g-targeted-summary.json) passes all fifteen top-level
tests across database, library and server packages with zero skips. Package
times are 1.087, 1.816 and 4.469 seconds respectively. It covers:

- Historical schema-14 task preservation and the new false default, including
  every prior task status, constraints and migration idempotency.
- Same-version cache reuse versus explicit re-probing, identical accepted
  results counted as updates, optional-index absence, failed or changing sources,
  nonempty administrator metadata and user-state preservation.
- Two worker admission, queued/running deduplication, request-context detachment,
  cancellation and persisted mode after restart/interruption recovery.
- Exact JSON boolean/casing, duplicate keys, unknown fields, malformed UTF-8,
  query rejection, actual 4096-byte limits independent of Content-Length,
  cookie/CSRF separation and rejected input admitting no jobs.
- Every job projection retaining the mode, while library creation and Emby
  refresh retain their ordinary-scanning default.

These domain/HTTP tests use deterministic fake probes to check cache and
transaction behavior. Actual FFmpeg index reconstruction is verified below.

## Browser, real media and restart

The [isolated acceptance run](m4g-media-refresh-browser.json) passes on its
first attempt: one browser scenario in **14.572 seconds**, zero unexpected
failures, skips or flaky tests. The application runs as UID 995 against its
dedicated low-privilege PostgreSQL role/database. Node 20.19.2 and Playwright
1.63.0 drive the real dashboard.

The fixture is a 97,364-byte, three-second 160x90 H.264/AAC file with NFO and
SRT sidecars. Its source directory and files are root-owned and read-only to
the application. The initial real scan creates a probe-6 index with three
restart entries. API requests then set nonempty overrides, locks, four entity
associations, favorite/watched state and play history.

Only the new disposable database has its optional `VideoSeekIndexes` field
removed; `ProbeVersion` stays 6 and the source identity stays unchanged. Empty
POST, `{}` and the normal dashboard scan all preserve that missing cache entry.
The explicit dashboard refresh sends exactly `{"ForceProbe":true}` with CSRF,
counts one update and reconstructs the three-entry private index. This isolates
the new force behavior from the preexisting stale-version upgrade behavior.

Cancel and Escape close the mobile confirmation without issuing any scan POST.
The browser verifies focus restoration, normal/refresh task labels, completion,
reload persistence and no horizontal overflow. The captured
[desktop library](screenshots/media-refresh-libraries-desktop.png),
[mobile dialog](screenshots/media-refresh-dialog-mobile.png),
[desktop tasks](screenshots/media-refresh-tasks-desktop.png) and
[mobile tasks](screenshots/media-refresh-tasks-mobile.png) show the actual UI.

The runner compares full manual metadata state, revision/audit fields, entity
associations, subtitles, source identity/hierarchy, original media-source DTO,
favorites and play history before and after. Only expected scan timestamps and
the private index are excluded from that comparison. It also verifies source
hashes and absence of index internals from public DTOs. All five job records,
their modes and the restored index survive an actual application restart.

All ten cleanup checks pass: the isolated database, role, application and
private temporary files are removed, the exact HBA bytes are restored, and the
preexisting database catalog and shared service remain unchanged. No reference
Emby or shared Goby media was mutated by this browser run.

## Complete regression and deployment

The [full Linux race run](m4g-full-race-summary.json) passes **969 top-level
tests** across all twelve tested packages, with zero skips or race findings.
Go and the SSH wrapper both exit zero. The server package takes 430.429 seconds
and transcode takes 56.865 seconds. The 308 Go/module files have canonical
manifest SHA-256
`e056554ac010b9b7f6d1225488c8f7a140d440bbaf3da736098216eab0a42455`.

The accepted browser binary SHA-256 is
`8f1c71d462b0534601cb148920470c5c5e6f312e62144fc2e0bdc0b39108358e`;
the administrator asset archive SHA-256 is
`42fea93320f44809652c9d35ead5552bc6c50c59fa93160c58f3f6a3a5925e43`.

The [deployment audit](m4g-deployment-evidence.json) confirms that same binary
and all 35 current assets on the shared test service, including actual HTTP
index/entry bytes. The service runs as UID 995, PID 3452400, with schema 15 and
probe cache 6. All 308 installed Go/module files match the verified manifest;
the fifteen migration files also match the source snapshot. Prior immutable
assets remain available.

The upgrade preserves all preexisting business fields across seventeen public
tables. Its only database changes are the new migration record and the false
`force_probe` default on the 36 historical jobs. The protected backup at
`/dev/shm/goby-m4g-deployment-backup` retains the preceding executable/index and
a private schema-14/probe-6 PostgreSQL dump. This operational backup is not a
shipped product backup/restore feature.

The [deployed refresh workflow](m4g-deployed-media-refresh.json) passes on its
first exclusive attempt. It logs in once, refreshes each of the five existing
libraries sequentially, and logs out. All eleven existing media records are
successfully re-probed and counted as updates, with no new media or libraries.
The five new task modes and lifecycle timestamps agree between HTTP and SQL.
Six media files retain six private indexes with 65 entries; the other formats
are not required to produce indexes.

All existing metadata/entity/subtitle/user-data rows, authentication history and
the 36 preceding task rows remain exact. Catalog identity and media facts are
preserved apart from admitted scan timestamps and optional private index facts.
Hashes and identities of all eleven source files stay unchanged. One already
expired, unfinalized Prepared playback is preserved exactly; the verifier does
not rewrite or normalize playback history. The new administrator login ends
revoked, cleanup reports no errors, and service PID/start time/UID/binary remain
unchanged during the workflow. It issues eleven GETs, six POSTs and one DELETE.

## Remaining scope

Index generation and runtime proof remain optional and bounded. Real GPU
execution, aggregate process memory isolation, efficient linear audio I/O,
nonzero copied-video seeking, additional formats/tracks/subtitles and full
client release acceptance remain open. API keys, broader device/task/settings
management, online providers, audit/log browsing and product backup/restore
remain separate administrator work. This increment does not complete the
entire conversion or administrator milestone.
