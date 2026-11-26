# Administrator Design 3 walkthrough

## Scope

Reviewed against all 21 boards in the supplied **Goby administrator Design 3**
handoff. The handoff designates eight boards as unchanged; their deviations are
recorded without replacing already working behavior with prototype examples.
The implementation uses the existing native administrator API and the shared
light/dark theme tokens. No server, schema, player, or authorization contract
is changed.

## Board-by-board review

| Board | Implemented behavior and remaining differences |
| --- | --- |
| Main — Overview | Server identity and uptime; five operational status cells; shared actionable issues; up to six per-library finished scans; actual running work and the next scheduled library scan. No playback list, activity feed, CPU/memory cards, or counts footer. Timestamp, scan-history, backup and hardware limits are described below. |
| Header | The bell and overview show the same sorted issues, counts, and independent unavailable/stale states. The bell retains refresh and notification settings. The existing account menu, language, appearance, and sign-out actions remain. |
| Sidebar | Thirteen primary destinations in the specified groups. Sessions and Devices are removed, including their counters and controls. Old paths and saved tab values resolve to Users. Existing secondary catalog artwork, hardware, and provider routes remain under their current destinations. |
| Palette | Removed the two retired pages. Device, session, and login searches in English and Chinese resolve to Users. Existing keyboard traversal, theme toggle and sign-out remain. |
| Overview Light | The same overview uses the existing light theme; no separate layout or hard-coded color values. |
| Overview Phone | The same overview uses the navigation drawer, compact status grid, and stacked sections at phone widths. |
| Libraries | A table combines native library kind, total item count, latest observed scan, exact registered-directory accessibility, and actual last scan. Storage rows show read/write state, library names, observed bytes and capacity usage without aggregation or forecasts. Library settings, bindings, scan, refresh, delete, and processing actions use the existing requests. |
| LibraryItems | A single content-type selector and title search become `Types` and `SearchTerm` on the server. With one selected item, Re-identify searches the configured provider and applies a specifically chosen candidate against the read revision. Locked fields and manual overrides remain unchanged. Other tabs and editor flows remain. Unsupported item/provider combinations are explained rather than offered as working operations. |
| MediaAnalysis | Automation coverage only reports saved settings and applicability, not guessed per-library errors. Failures are actual runs and link to that run. Cache shows used bytes, ready/building entries and the configured cap; cleanup reports the actual response. Missing fields are unavailable rather than shown as estimates. |
| Users | Fifty accounts per page, visible-page login reads first, independent inactive/disabled/administrator groups, and cancellable sequential batches with explicit conflict and uncertainty outcomes. The drawer contains Permissions, Profile, Login devices and Password, with role-specific confirmation and current-session revocation. Existing optional avatar and local credentials controls are retained as secondary actions. |
| Activity | Real server-side severity, one action and date-range filters with counts; the search is current-page only. Login/playback events appear without playback titles, media identifiers, or fingerprints in the list, detail, copy, or page export. The native diagnostic log viewer and file download retain their existing contract. |
| SettingsGeneral | Server maintenance shows the current playing-client count snapshot and broadcasts a byte-validated message with actual queue dispositions. The restart dialog can open a prefilled reminder without scheduling or starting a restart. Restart still requires the exact `RESTART` confirmation. |
| Notifications | The target editor explains frequent login notifications, the 256-item target queue, and how overflow is dropped and counted. Existing target management and delivery status remain. |
| Login (unchanged) | Uses the real bootstrap/login gate, live request ID on errors, and an empty username by default. The board's prefilled demo login and standalone link to Setup are not production affordances; the desktop brand placement is unchanged. |
| Setup (unchanged) | The real four-step wizard uses existing status indicators, a library/directory picker and a preview of network changes. It reports whether a scan was actually requested rather than showing the illustration's fixed success message. |
| Collections (unchanged) | The current list submits title search, pages real results, and shows content type/lock state instead of the board's illustrative member names. Initial members are staged on selection; successful creation keeps the editor open. Existing search, membership and locking safety are retained. |
| ApiKeys (unchanged) | The current UI includes revoked keys by default with a 50-row page and an overflow-menu filter. It shows the creation IP, not a connection channel inferred from that IP. The full key is shown once at creation; the masked list does not expose a fabricated suffix or offer another reveal action. |
| Tasks (unchanged) | The existing cards, 24-hour history, filters, scan history and schedule editor remain. The board's extra history status filter, task-name-to-schedule shortcut, and constant backup passphrase callout are not part of this revision. Exact run links are supported from Overview and Media analysis; trigger links still open the shared schedule editor. |
| Backups (unchanged) | The current screen uses actual storage/readiness observations and an explicit restore/rollback workflow instead of the board's fixed summary cards. The archive list distinguishes generated and imported but does not infer a manual/automatic origin. Retention remains an explicit value; the import flow selects an encrypted `.age` archive. |
| SettingsTranscode (unchanged) | The current controls show actual hardware/codec observations and deployment/default settings; run diagnostics against a fixed sample. A `bt.2390` option and diagnostic playback simulation for a chosen item/client are not supported by the current contract and were not enabled for this revision. |
| SettingsMetadata (unchanged) | Existing fields, server byte limits and independent provider route remain. Credential tests use saved configuration; an arbitrary common credential limit or a prototype's simulated success is not shown. |

## Limits of the retained contracts

- The [scan jobs API](../api/admin-scans.md#task-representation-and-persistence)
  is a global listing of at most 1,000 recent admissions and has no per-library
  filter or next page. A library outside that window can have a recorded last
  successful scan but no accessible counts or failure details. The interface
  marks this as missing recent history instead of asserting that it never ran.
  A failed scan retains existing records, but accepted changes made before the
  failure may persist; the UI does not promise a full roll back.
- The overview response reports a connected PostgreSQL database when the
  read succeeds. A failed response makes the check unavailable; it does not
  certify the reason or fabricate a degraded timestamp. The API does not date
  storage threshold crossings, unreadability, or policy changes. Only events
  with recorded times are ordered by occurrence within each severity.
- The [backup policy](../api/backup-policy.md) contains the latest automatic
  **run**, not the timestamp of the most recent successful manual or automatic
  backup across all archives. The overview labels the returned time accordingly.
  [System status](../api/admin-system-status.md) gives hardware/software slot
  counts, but not the adapter identity of each slot. A device label, when shown,
  is explicitly the configured choice, not proof of what each job used.
- The analysis inventory has verified intro and seek-preview data, but no
  single per-item projection of current credits, background clips, waveform and
  subtitle-timeline artifacts. The independently paginated, source-specific
  result views are retained. Run summaries provide child counts and aggregate
  error details, not ETA, per-library failure totals or per-source errors.
  Cache cleanup reports observed removals instead of estimating reclaimable bytes.
- Re-identification only works for item types supported by a configured metadata
  provider. Season and episode candidates use saved catalog positions; the
  search result contract does not supply posters or a filename-match score.
- Account batches use saved observations and conditional mutations. The API
  cannot atomically verify a login did not occur between selecting the inactivity
  group and disabling a user. A sign-out batch does not include logins issued
  after its active-session snapshot. The list is fixed at fifty accounts per page
  as specified in the handoff text, not the board's illustrative 100-row option.
- The playback audience may change before submission; the server takes a new
  snapshot when admitting a message, and a queue receipt cannot prove that a
  client displayed it. The activity API retains exact severity/action filters
  and the server's configured retention. It does not expose login device names
  or detailed scan/task outcomes in the activity rows. The log viewer does not
  invent a currently-writing marker or total storage figure; its original JSONL
  download can contain opaque diagnostic correlation IDs.
- No automatic account policies, watched-title views, per-player controls,
  delayed restart, ignore/snooze, missing-metadata counters, or storage forecasts
  were added. These are deliberately excluded by the handoff.

## Verification

- The production build and TypeScript check passed on `test-env`. The Node
  suite passed 65 utility and packaging cases. The final Playwright run recorded
  562 passes, including all 24 Design 3 cases, 48 failures, and 13 skips. Each
  of the 48 failures also reproduced on unchanged `main` under its matching
  baseline check (one was rerun individually after the initial baseline timed
  out). They include old task/media/notification fixtures and playback cases
  that require specialized runtime fixtures. This is not a clean full-suite run.
- Browser checks exercised the overview and bell together, a 20,000-account
  dataset with real request sizes and pagination, role safeguards, stopped or
  conflicted batches, real provider payload shapes and redacted activity exports.
  Desktop light and dark plus phone widths were visually inspected with rendered
  screenshots. An empty first-run PostgreSQL database completed setup and sign-in.
- The isolated Goby backend and browser confirmed scan completion, cancellation,
  media-file preservation, and revocation of two real test-client logins. A live
  maintenance test counted three active playback clients without exposing the
  title, excluded an idle client, displayed queued/offline/unsupported outcomes,
  and observed the connected client receive the message through the authenticated
  WebSocket. A separate reminder test confirmed no restart request was sent.
- Two additional opt-in account journeys did not complete as a clean pair:
  the deletion journey reached the stale-revision check before a locator
  correction; a subsequent retry was blocked at sign-in by the loopback login
  limiter. The long user-management journey reached password reset; the next
  wrong-password check received the same limiter's HTTP 429 rather than 401.
  The targeted account, role, credential, and active-login cases passed.
- The task-owned services were shut down. Build scratch and disposable tools
  were reclaimed; the shared Go caches, source snapshots, database, media
  fixtures, and raw evidence were retained separately.
