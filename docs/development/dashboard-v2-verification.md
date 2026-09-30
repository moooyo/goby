# Dashboard v2 local delivery verification

Date: September 30, 2026.

This change implements the supplied Dashboard v2 handoff in the existing React 19, TypeScript, and MUI administrator application. It preserves the server-management scope, existing API validation and mutation safeguards, and the automatic TV-library intro workflow introduced on `main` during implementation.

## Delivered behavior

- Five navigation destinations, group tabs with session-local last-tab memory, canonical grouped URLs and legacy URL redirects, an account menu, and an adaptive bottom navigation bar below 840px.
- Material 3 blue/white surfaces, packaged Manrope/Noto Sans SC/JetBrains Mono fonts, rounded icons, cards, tables, compact mobile lists, menus, dialogs, and independent content scrolling.
- Rebuilt Overview, Libraries and item drilldown, Catalog artwork, Playlists and collections, Media analysis, Users, Devices, Sessions, API keys, Tasks, Activity and logs, Notifications, Backups and recovery, and all settings/provider pages.
- Four settings tabs share a complete draft, exact revision and compare-and-swap save, discard operation, conflict recovery, and busy-operation navigation protection.
- Real administrator-only host telemetry, explicit unavailable values, volume deduplication, and occupied transcode-slot counts. Sampling never performs filesystem I/O in an HTTP request or while holding the collector lock. At most one background sample is admitted; observations expire after 15 seconds. Overview resources refresh independently with cancellation, deadlines, and hidden-page suspension.
- Automatic intro detection remains a TV-library opt-in. Analysis results are read-only, while seek-preview generation, diagnostics, receipt recovery, and cancellation remain available.

## Environment and artifact

The user explicitly authorized local build, runtime, and browser verification for this task. Builds ran on Windows. Docker and PostgreSQL ran in a dedicated daemon inside this same machine's WSL2 Debian distribution, with separate application/integration databases, synthetic credentials, generated media, loopback listeners, and read-only media. No remote test server or production installation was used.

The integration includes `origin/main` at `869c6654f6c5dd5d2430a4ae405b3c17428c953f`. The tested application source is `2b8d366`; later commits only record verification or adapt tests unless explicitly noted below.

| Artifact | Verified identity |
| --- | --- |
| Linux amd64 binary with embedded dashboard | `d3a341ddeed218b298cd4bdef9ed27026c7fdd64b2daec633ae2c5720d8faa46` |
| Locally tested Docker image | `sha256:ac6aef7f3fb0006c11fdaf693dd83425a29c9367fb92cbcd47484ee371e86090` |
| Application database schema | 51 |
| Served frontend assets | 197 files, each matched to the production build |
| Packaged legal payload | 189 files, each matched to the source payload |

The live binary was hashed inside the container. Every emitted frontend file was fetched over HTTP and compared by SHA-256; the favicon, readiness endpoint, and authenticated telemetry endpoint returned 200. The three font families have retained upstream license text and 113 font-asset byte mappings in the notices inventory.

## Completed checks

| Check | Result and scope |
| --- | --- |
| `npm run build` | TypeScript and production Vite bundle passed |
| Frontend Node tests | 15 passed: library options, automatic analysis projection, schedule triggers, sorting semantics |
| `dashboard-v2.spec.ts` | 54 passed after main integration: 19 pages at 1440x834 and 375x812, old URLs, last-tab memory, keyboard account menu, shared drafts/CAS, pending-save guards, exact breakpoint, unavailable/stalled telemetry |
| Final mobile analysis detail check | 2 passed after adapting preview output rows into phone cards |
| Account-menu follow-up | Passed with real server-name loading |
| Authentic administrator tests | 2 passed on the final Docker image: user creation, cookie attributes, persistent sessions, phone navigation, and rejection of a stale mutation after an account switch |
| Authentic delivery workflow | Passed on the final Docker image: create/scan movie and TV libraries, inspect real catalog items and scan counts, edit/restore metadata, create/reveal/revoke an API key, preserve/save/reload/restore a multi-tab settings draft, and visit system/provider pages on desktop and phone |
| Media preservation | All 9 generated media, NFO, and artwork files matched their before/after SHA-256 values; no files were added, removed, or changed |
| Workflow regression campaign | 71 passed: backups, notifications, scheduled-task admission/stop safety, runtime settings, diagnostics, management configuration, metadata management, and media analysis |
| Additional preserved workflows | 47 passed with no skips: episode rosters, media processing, permission failures, storage-binding consent/revisions, local credentials, and selected playback administration |
| Main integration media checks | 9 media-analysis and 6 management-configuration cases passed with automatic intro behavior retained |
| API contract checks | 89 passed for activity/root-binding validation, exact revisions, and no conflict replay |
| Windows Go tests | Host observations, blocked sampling, cache expiry, five-second polling, recovery, shutdown, and administrator HTTP endpoint tests passed against the isolated PostgreSQL database |
| Local WSL Go tests | Telemetry race tests, HTTP auth/credential isolation, and library-intro option checks passed |
| Visual inspection | 44 captured reference states, including every page at desktop/phone width, scrolled settings save bars, and expanded analysis results; representative dialogs also captured in live workflows |

The Windows library test package includes pre-existing references to Linux-only test helpers, so its targeted library-intro checks ran successfully in local WSL. A Windows Go cross-test-binary compiler crash was also bypassed by building and running the same selected tests natively in local WSL. Neither failure was counted as a pass.

## Evidence and boundaries

Full local logs, screenshots, file manifests, Docker identity checks, and the disposable runner remain under ignored `.artifacts/dashboard-v2/`. Credentials and session secrets are excluded from this document and committed evidence. The screenshot index maps each capture to its route, viewport, and state.

The final authentic workflow captured 23 additional screenshots and recorded no page errors, failed administrator API responses, or unexpected console errors. Metadata and server settings were restored, and its application key was revoked. Test libraries and generated media remain only in the disposable environment.

Synthetic browser checks intercept API calls and exercise frontend state transitions; authentic browser delivery uses the actual Docker service and PostgreSQL. The real media is two generated movies and one generated episode. This verifies administrator delivery and CPU-host telemetry; it does not claim a new GPU, playback-client compatibility, or production restore campaign. Existing recovery and secret-handling workflows receive targeted synthetic coverage.

The original main worktree's unrelated modifications are preserved. Docker image publication, release-catalog replacement, and production deployment are outside this request; Git integration and push are recorded by the resulting repository history.
