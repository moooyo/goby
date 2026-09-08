# M1 foundation verification

Recorded on 2026-09-09. This report applies to the foundation source delivered with this report. It verifies Goby's own behavior and security boundaries, not complete Emby interoperability.

## Results

| Check | Environment | Result |
| --- | --- | --- |
| `go build ./...` | Authorized local Windows compiler, Go 1.27.1 | Passed; no application or tests executed locally |
| `npm run build` in `web/admin` | Authorized local Node 26.1.0, locked React/MUI/TypeScript/Vite | Passed; TypeScript compilation and production assets, no chunk-size warning |
| `go test -race -count=1 ./...` for initial foundation | Remote Linux, real PostgreSQL, random isolated schemas | Database and identity packages passed; at that point server tests were still being added |
| `go test -race -count=1 ./internal/server` after all server fixes | Remote Linux, real PostgreSQL | Passed in 8.688 seconds, including HTTP flows, parser cases, proxy trust and rate-limit isolation |
| Playwright real administrator UI | Remote Chromium 1.63.0 Playwright bundle, real Goby service | 2 tests passed in 8.1 seconds; no page exceptions or font/CSP errors |
| Non-root service build/start | Remote Linux systemd | Rebuilt from current foundation source; `User=goby`, `ActiveState=active`, `/readyz` successful |
| FFmpeg/ffprobe and PostgreSQL provisioning | Remote Linux | See [environment report](test-env.md); Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11 |

No PostgreSQL integration test was treated as passed based on an environment-variable skip. The dedicated `GOBY_TEST_DATABASE_URL` was loaded on the remote host.

## Coverage

- Atomic first-administrator setup, including competing bootstrap requests, invalid setup secret and repeated setup.
- Migration idempotence, concurrent migration calls and refusal of unknown migration history.
- Persistent server/user identity, Unicode case-folded username uniqueness and bcrypt byte limits.
- Hashed token storage, expiration, revocation, disabled accounts and administrator demotion.
- Separation of administrator-cookie and Emby-token sessions; caller-supplied user/role claims cannot grant access.
- Administrator cookie flags, CSRF rejection, origin checks, user creation, logout and revoked-session rejection.
- Emby login object shape and token carriers, self/admin user access, cross-user denial and administrator-only user query.
- Unknown administrator APIs return JSON errors instead of the SPA page.
- Trusted proxy chain parsing rejects spoofed/untrusted forwarding data and keeps different clients' login quotas separate.
- Browser setup, login, real overview, user creation, logout, login again, refresh persistence and mobile navigation.
- Cross-tab administrator replacement rejects the stale tab's mutation once, does not replay it under the new account, and sends the old tab back to login.

The initial unauthenticated session request returns an expected HTTP 401. That expected browser network message is distinct from an unexpected page exception. The final browser run had no page exceptions and no font/CSP failures. Font assets are served from the application origin instead of inlined data URLs.

Desktop overview, desktop users, mobile overview and mobile user-list screenshots were visually inspected. Mobile user records retain account status and access information without requiring page-level horizontal scrolling. Screenshots are local review artifacts under `.artifacts/admin-ui`, not production assets or committed test credentials.

## Corrections made during review

The Go router's API fallback and SPA handler were adjusted to avoid method/path specificity conflicts. Frontend CSRF recovery no longer automatically replays a write after the session changes. Proxy-aware login limiting now uses only explicitly trusted CIDRs, with right-to-left `X-Forwarded-For` processing. Both security corrections have regression coverage.

## Limits and follow-up

This increment has no library scanner, media delivery, HLS job controller, complete user-policy editor, API-key manager, or backup/restore implementation. It has not been compared against a real Emby reference server or third-party playback application. The initial Emby DTOs and query handling remain partial, explicitly documented in [implemented.md](../api/implemented.md).

The installed FFmpeg includes VAAPI/QSV/NVDEC/NVENC interfaces, but the test VM has no GPU. Real hardware decode, encode, filter transfer, tone mapping, concurrency and fallback are not verified. They remain requirements for the hardware playback stage.

The test service remains on the remote loopback address. A container release, embedded frontend packaging, public TLS/proxy deployment, source license selection, complete backup/upgrade procedure, and the rest of the planned media feature set remain delivery work.
