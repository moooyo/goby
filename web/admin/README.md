# Goby administrator dashboard

React, TypeScript, and MUI provide an administrator-only Material Design interface. The dashboard has no media browser or player. It includes first-run setup, overview, users, libraries and storage bindings, metadata, sessions, devices, application keys, tasks and schedules, settings, activity/logs, and native backup/recovery. Consult the [current status](../../docs/development/current-status.md) for verification and deployment boundaries.

## Build

Use Node.js 20.19 or Node.js 22.12 and later. Install the pinned dependency tree with `npm ci`, then run `npm run build`. The build performs TypeScript compilation and writes production files to `dist/`.

The base URL is `/admin/`. Serve `dist/index.html` for dashboard paths such as `/admin/access/users`, and serve `dist/assets/*` as static assets. API routes under `/admin/v1/` must take precedence over the SPA fallback. Return `Cache-Control: no-store` for the HTML entry and API responses; hashed assets can use immutable caching. Ordinary builds serve the packaged `dist/` directory through `GOBY_WEB_DIR`; the [embedded administrator build](../../docs/development/embedded-administrator-build.md) includes production assets with the `goby_embed_admin` tag. [Focused remote checks and amd64/arm64 build results](../../docs/development/embedded-administrator-verification.json) are recorded; native platform and deployment acceptance remain separate. Generated `dist/` content is not committed here.

For remote development, `npm run dev` proxies `/admin/v1/` to `http://127.0.0.1:8096`. Set `GOBY_DEV_API` to change that target. Configure the backend `GOBY_PUBLIC_URL` to match the dashboard origin used by the browser so origin checks also work through the development proxy. Run builds, type checks, tests, and browser verification through `ssh test-env`; local verification requires explicit authorization in the current task.

MUI uses Emotion to insert style elements at runtime. A deployment content security policy must allow these styles with a supported nonce strategy or an appropriate `style-src` directive. Scripts and font files are served from the dashboard origin.

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

The dashboard uses five navigation destinations: Overview, Media, Access, System, and Settings. Each group has its own tabs. The last visited tab in each group is stored in `sessionStorage` under `goby.dashboard.tabs`; authentication never uses browser storage. The navigation rail becomes a bottom bar below 840px, and tab strips scroll horizontally.

Canonical routes include `/admin/media/libraries`, `/admin/media/libraries/:id/items`, `/admin/access/users`, `/admin/system/tasks`, and `/admin/settings/general`. Existing flat dashboard URLs redirect in place. Settings are split into General, Transcode limits, Hardware acceleration, and Metadata & subtitles, with Online providers as the fifth tab. The four editing tabs share one draft, revision, save action, and discard action. Internal tab changes preserve drafts; leaving settings invokes the existing navigation guard. Pending settings mutations or diagnostics prevent tab changes.

The overview polls real server counts, recent activity, and the [system status endpoint](../../docs/api/admin-system-status.md) every five seconds. Unsupported measurements remain unavailable, failed refreshes are marked as stale, and navigation cancels outstanding reads. Storage totals count each readable volume once.

## Visual direction

Dashboard v2 follows the supplied Material Design 3 handoff. Primary blue `#1F5FBF`, selection container `#D8E4FA`, canvas `#EDF1F8`, white panels, tonal cards `#F3F6FB`, and ink `#181C23` form the palette. Manrope carries interface text and numbers, Noto Sans SC supplies Chinese glyphs, and JetBrains Mono identifies paths and server records. Fonts and rounded SVG icons ship with the application and require no external font service.

An 88px navigation rail sits beside a rounded white workspace. Content fills the workspace and scrolls independently. Tables adapt to compact cards, settings use two-column description/form groups, and saved or unsaved changes share a sticky action bar. Existing security checks, confirmations, API validation, and mutation recovery remain in the underlying workflows.

## Dashboard v2 regression checks

`dashboard-v2.spec.ts` uses intercepted API fixtures to cover all page routes at desktop and phone widths, navigation history, shared settings drafts, compare-and-swap conflicts, and telemetry errors. The workflow suites retain detailed mutation and safety assertions. These synthetic checks complement the live administrator and delivery tests; they do not establish backend behavior on their own.

Set `GOBY_BROWSER_CHANNEL=chrome` or `msedge` to use an installed browser, or leave it unset for Playwright Chromium. All suites accept `GOBY_SMOKE_BASE_URL`. Keep parallel runs in separate output directories. See [dashboard v2 verification](../../docs/development/dashboard-v2-verification.md) for the recorded delivery scope and results.
