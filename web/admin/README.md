# Goby administrator dashboard

React, TypeScript, and MUI provide an administrator-only Material Design interface. The dashboard has no media browser or player. The initial milestone includes first-run setup, administrator sessions, a live server overview, and user creation. Future modules stay disabled until their server APIs are implemented.

## Build

Use Node.js 20.19 or Node.js 22.12 and later. Install the pinned dependency tree with `npm ci`, then run `npm run build`. The build performs TypeScript compilation and writes production files to `dist/`.

The base URL is `/admin/`. Serve `dist/index.html` for dashboard paths such as `/admin/users`, and serve `dist/assets/*` as static assets. API routes under `/admin/v1/` must take precedence over the SPA fallback. Return `Cache-Control: no-store` for the HTML entry and API responses; hashed assets can use immutable caching. M1 serves the packaged `dist/` directory through `GOBY_WEB_DIR`. A future build may embed these files into the Go binary. Generated `dist/` content is not committed here.

For remote development, `npm run dev` proxies `/admin/v1/` to `http://127.0.0.1:8096`. Set `GOBY_DEV_API` to change that target. Configure the backend `GOBY_PUBLIC_URL` to match the dashboard origin used by the browser so origin checks also work through the development proxy. Run tests and browser verification in `test-env`; local authorization covers build compilation only.

MUI uses Emotion to insert style elements at runtime. A deployment content security policy must allow these styles with a supported nonce strategy or an appropriate `style-src` directive. Scripts and font files are served from the dashboard origin.

## Session behavior

The server owns the HttpOnly session cookie. Requests use same-origin credentials. CSRF tokens are held only in memory and obtained from the session endpoint. A CSRF mismatch clears the local session and returns to login; mutations are never automatically replayed under an identity that may have changed in another tab. Neither credentials nor setup tokens are stored in browser storage. First-run setup returns to login rather than assuming a session was created.

## Remote browser verification

Run `npm run test:e2e` only in `test-env`, against a disposable Goby test server. Set `GOBY_SMOKE_BASE_URL`, `GOBY_SMOKE_NAME`, and `GOBY_SMOKE_PASSWORD`; a fresh server also needs `GOBY_SETUP_TOKEN`. Install Chromium there with `npx playwright install chromium` when required. The suite creates the first administrator when needed, one normal user, and another administrator per run. It verifies login, cookie attributes, user creation, logout, session persistence after reload, mobile navigation, and rejection of a stale form after another tab changes administrator accounts. Screenshots are written to `test-results/`. Network traces are disabled because authentication requests contain secrets.

## Visual direction

Deep sea `#163F4B`, sea `#007E87`, ink `#193A46`, muted blue `#5D737D`, fog `#D9E7EB`, and canvas `#F3F7F8` form the palette. Manrope carries headings and the wordmark; Public Sans carries forms, navigation, and tables; system monospace identifies server records. Fonts ship with the application and require no external font service.

A quiet server status strip anchors the overview. A deep sea navigation rail groups management tools around the server identity, while the work area uses white surfaces, restrained spacing, and clear Material controls. Values always come from the server. Disabled future navigation is descriptive and does not open placeholder screens.
