# Goby administrator dashboard

React, TypeScript, native accessible controls, and static CSS provide the administrator interface. The November 2026 design uses a compact sidebar, right-side editors, dark and light themes, and Chinese and English. It includes first-run setup, overview, users, libraries and storage bindings, metadata, sessions, devices, application keys, tasks and schedules, settings, activity/logs, and native backup/recovery. Consult the [current status](../../docs/development/current-status.md) for backend and deployment boundaries.

## Build

Use Node.js 20.19 or Node.js 22.12 and later. Install the pinned dependency tree with `npm ci`, then run `npm run build`. The build performs TypeScript compilation and writes production files to `dist/`.

The base URL is `/admin/`. Serve `dist/index.html` for dashboard paths such as `/admin/access/users`, and serve `dist/assets/*` as static assets. API routes under `/admin/v1/` must take precedence over the SPA fallback. Return `Cache-Control: no-store` for the HTML entry and API responses; hashed assets can use immutable caching. Ordinary builds serve the packaged `dist/` directory through `GOBY_WEB_DIR`; the [embedded administrator build](../../docs/development/embedded-administrator-build.md) includes production assets with the `goby_embed_admin` tag. [Focused remote checks and amd64/arm64 build results](../../docs/development/embedded-administrator-verification.json) are recorded; native platform and deployment acceptance remain separate. Generated `dist/` content is not committed here.

For remote development, `npm run dev` proxies `/admin/v1/` to `http://127.0.0.1:8096`. Set `GOBY_DEV_API` to change that target. Configure the backend `GOBY_PUBLIC_URL` to match the dashboard origin used by the browser so origin checks also work through the development proxy. Run builds, type checks, tests, and browser verification through `ssh test-env`; local verification requires explicit authorization in the current task.

The dashboard has no MUI or Emotion dependency and does not inject runtime style elements. Stylesheets, scripts, and fonts are packaged and served from the dashboard origin. Presentation values in the compatibility controls still use inline style attributes; deployment CSP must account for those attributes separately from external stylesheets.

## Session behavior

The server owns the HttpOnly session cookie. Requests use same-origin credentials. CSRF tokens are held only in memory and obtained from the session endpoint. A CSRF mismatch clears the local session and returns to login; mutations are never automatically replayed under an identity that may have changed in another tab. Neither credentials nor setup tokens are stored in browser storage. First-run setup returns to login rather than assuming a session was created.

## Libraries and scan tasks

Libraries use absolute Linux paths inside the configured media roots. The dashboard displays root availability, validates basic path syntax, and leaves canonical path authorization to the server. Creating a library can request an initial scan. Deleting a library removes catalog records while keeping media files on disk; a confirmation explains this before the request is sent.

Tasks display actual scanned, added, and updated counts instead of an estimated percentage. Pending or running tasks can be cancelled. The task page schedules its next refresh five seconds after the previous request finishes, and only while active tasks remain. Terminal states, request errors, and navigation stop automatic refreshes. Cancelling a task keeps catalog entries that were already scanned.

## Browser verification

Run browser verification against a disposable Goby test server in `test-env`, or locally when the user explicitly authorizes local verification in the current task. Set `GOBY_SMOKE_BASE_URL`, `GOBY_SMOKE_NAME`, and `GOBY_SMOKE_PASSWORD`; a fresh server also needs `GOBY_SETUP_TOKEN`. Install Chromium there with `npx playwright install chromium` when required. The suite creates the first administrator when needed, one normal user, and another administrator per run. It verifies login, cookie attributes, user creation, logout, session persistence after reload, mobile navigation, and rejection of a stale form after another tab changes administrator accounts. Screenshots are written to `test-results/`. Network traces are disabled because authentication requests contain secrets.

The library test additionally needs `GOBY_SMOKE_MEDIA_PATH` pointing to an allowed directory containing real probeable media and `GOBY_SMOKE_MEDIA_FILE` pointing to one file inside it. Without these variables the fixture-dependent test is explicitly skipped. With fixtures configured, it verifies library creation, a real scan, completed task counts, mobile layouts, and library deletion without changing the fixture file.

Cancellation verification requires `GOBY_SMOKE_CANCEL_MEDIA_PATH` with enough real media files for an active scan. On the authorized Linux test host, `python3 e2e/prepare-cancel-fixture.py` prepares 200 hard links under `/opt/goby-fixtures/cancel` from `GOBY_SMOKE_MEDIA_FILE` without duplicating media data or deleting files. Point the cancellation variable at that directory. The cancellation test must observe and cancel an active task; it fails if the scan finishes before the action and never treats that race as a pass or skip.

## Navigation and settings

The 232px sidebar exposes all fifteen primary destinations, organized into Media, Access, System, and Settings. Below 1240px it becomes a 264px modal navigation drawer; phone layouts below 640px use 16px content insets. Tables use compact columns below 1000px. Ctrl/Cmd+K opens keyboard-searchable navigation. The account menu controls independently persisted language and appearance preferences (`goby.admin.language` and `goby.admin.theme`); authentication never uses browser storage.

Canonical routes include `/admin/media/libraries`, `/admin/media/libraries/:id/items`, `/admin/access/users`, `/admin/system/tasks`, and `/admin/settings/general`. All nineteen existing canonical routes and flat dashboard URL aliases remain supported. Library detail combines items, artwork, directories, analysis features, and scan history. Settings navigation combines hardware with Transcoding and online providers with Metadata & subtitles. Editing sections share one draft, revision, save action, and discard action. Internal navigation preserves drafts; leaving settings invokes the navigation guard. Pending mutations or diagnostics prevent navigation. Ctrl/Cmd+S submits the active editor through its existing validation and revision checks.

The overview polls real server counts, recent activity, and the [system status endpoint](../../docs/api/admin-system-status.md) every five seconds. Unsupported measurements remain unavailable, failed refreshes are marked as stale, and navigation cancels outstanding reads. Storage totals count each readable volume once.

## Capability workflows

The first-run workflow authenticates after the existing operator-authorized
bootstrap, offers an initial library within deployment roots, configures remote
access, then completes by logging out. Unknown writes require readback before
retry. The authenticated setup session is not stored in browser preferences.

General settings exposes real [release observations](../../docs/api/server-updates.md)
from a deployment-selected Goby feed. Sessions displays retained playback stop
events with current user and media names. See the
[core API follow-up](../../docs/development/admin-core-api-20261106.md) for the
upstream evidence and remaining core contract boundaries.

Current playback and remote controls use the native administrator cookie and
CSRF boundary. The standalone player separately receives commands through its
authenticated Emby-compatible WebSocket. A successful command response records
acceptance; subsequent player reports establish the observed playback change.

Managed settings retain their common draft and revision. Access-policy and
credential operations have separately confirmed saves and keep their drafts
alongside that editor. Their navigation guards compose with the parent draft,
and pending operations preserve uncertain-result protection.

A browser without an explicit language choice follows the server's default
interface language. Choosing a language creates a personal browser preference;
later server changes preserve it. Appearance remains an independent preference.

The [November capability implementation](../../docs/development/admin-capabilities-20261106.md)
records native playback control, media and user summaries, provider credential
editing, administrator notification targets, automatic backup policy, global
access policy, graceful restart, codec observations, episode ordering, and
automatic missing-image downloads. It also distinguishes remaining product and
deployment decisions from missing administrator projections.

The current review loop binds backend and standalone-player sources and
production assets together with the administrator sources, tests, and bundle.

## Visual direction

The supplied November 4, 2026 prototype selects the sidebar-and-sheet direction. Dark surfaces use `#0C0C0D`, `#111113`, `#17171A`, and `#202024`, with amber `#E3B46A`. Light surfaces use `#F3F2EE`, `#FAF9F6`, white, and `#EBE9E3`, with amber `#D69D48`. Shared semantic tokens live in `web/shared/tokens.css`. Noto Serif SC carries page titles, Noto Sans SC interface copy, IBM Plex Mono technical values, and League Gothic display numerals. Fonts and linear SVG icons are bundled and require no external font service.

The header is 56px high. Page titles use 28px serif type (24px on phones), cards use 14px corners, and standard inputs and pill buttons are 34px high. Settings form a narrow single column with labels beside their controls. Metadata and user sheets are 540px wide, series editors 600px, and new-library sheets 480px. Content scrolls independently, and editors retain sticky action bars. Security checks, destructive confirmations, API validation, revision conflicts, and uncertain-mutation recovery remain in the business workflows. Unsupported server capabilities are explained instead of being simulated.

## Design regression checks

The administrator design suites use intercepted API fixtures to cover all routes at desktop and phone widths, navigation history, theme and language persistence, keyboard controls, sheet focus, shared drafts, compare-and-swap conflicts, uncertain writes, telemetry errors, exact storage-volume matching, and localized counts. The workflow suites retain detailed mutation and safety assertions. Synthetic browser checks do not establish database-backed behavior on their own.

`node e2e/design-review-loop.mjs` captures a complete evidence round and records a separate visual review. Its exit condition is two consecutive passing, visually reviewed rounds with no new modification recommendations for the same source hash. See [the review protocol](e2e/design-review-checklist.md) for commands, evidence handling, and capability-difference tracking.

Set `GOBY_BROWSER_CHANNEL=chrome` or `msedge` to use an installed browser, or leave it unset for Playwright Chromium. All suites accept `GOBY_SMOKE_BASE_URL`. Keep parallel runs in separate output directories. See [the November design rewrite](../../docs/development/admin-design-rewrite-20261104.md) for the current design scope, capability decisions, and verification limits.
