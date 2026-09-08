# Implemented API surface: foundation increment

This file tracks implementation separately from the immutable upstream research inventory. The [full catalog](catalog.md) contains upstream contracts and initial scope labels; its generated `planned-unimplemented` field records the research baseline, not the current implementation tracker.

The routes below exist in source. Authentication and permission workflows have passed Goby's own PostgreSQL-backed HTTP tests on Linux. They have **not** been compared against a live reference Emby release or a real media client, so they are not claims of complete Emby behavioral compatibility.

## Administrator API

All names are Goby-owned. JSON bodies and responses use the field names shown here.

| Method and route | Request | Response / access |
| --- | --- | --- |
| `GET /admin/v1/bootstrap` | None | `{Initialized: boolean}`; minimal anonymous setup status |
| `POST /admin/v1/bootstrap` | `{SetupToken, Name, Password}` | `201 {User}`; one-time deployment secret, atomic first administrator creation |
| `POST /admin/v1/session` | `{Name, Password}` | `{User, CSRFToken}` and opaque HttpOnly cookie; administrator credentials required |
| `GET /admin/v1/session` | Session cookie | `{User, CSRFToken}` |
| `DELETE /admin/v1/session` | Cookie and `X-CSRF-Token` | `204`; revoke session and clear cookie |
| `GET /admin/v1/overview` | Administrator cookie | Server identity, database status, real account/session counts, current feature flags |
| `GET /admin/v1/capabilities` | Administrator cookie | Implementation flags and pinned toolchain targets; unavailable media/hardware features report false |
| `GET /admin/v1/users` | Administrator cookie | `{Items: User[], TotalRecordCount}` |
| `POST /admin/v1/users` | Cookie, CSRF header, `{Name, Password, IsAdministrator}` | `201 {User}` |

The native user shape is `{Id, Name, IsAdministrator, IsDisabled, HasPassword, CreatedAt}`. Errors have `{Error: {Code, Message}, RequestId}`. `401` signals invalid/missing/revoked authentication; `403` signals rejected origin, setup token, or CSRF. Mutations require JSON and CSRF protection once authenticated. The cookie is scoped to `/admin`, uses `HttpOnly` and `SameSite=Strict`, and is secure by default. CSRF values remain in browser memory and can be recovered with the authenticated session endpoint.

Library/item counts are zero while those domains are unavailable. Active sessions in this increment are active authentication sessions, not a count of playing media clients.

## Initial Emby API adapter

| Method and path | Current behavior / limits |
| --- | --- |
| `GET /emby/System/Info/Public` | Stable server identity and setup state; Goby product/version is reported truthfully |
| `GET /emby/System/Info` | Requires an Emby user session; currently the minimal public identity projection, not the complete upstream SystemInfo DTO |
| `GET`, `HEAD`, `POST /emby/System/Ping` | Empty success body based on the initial snapshot contract; reference payload still needs comparison |
| `GET /emby/Users/Public` | Enabled ordinary accounts; administrators are hidden from this initial public list |
| `POST /emby/Users/AuthenticateByName` | `{Username, Pw}` and Emby client/device metadata; returns `User`, `SessionInfo`, `AccessToken`, `ServerId` |
| `POST /emby/Users/{Id}/Authenticate` | `{Pw}` and client/device metadata; selected-user authentication |
| `GET /emby/Users/{Id}` | Current account or administrator; a different ordinary user's account is denied |
| `GET /emby/Users/Query` | Administrator only; supports `StartIndex`/`Limit` and query-result envelope; other upstream filters remain to be implemented |
| `POST /emby/Sessions/Logout` | Revokes the caller's token |

The parser accepts `Authorization: Emby ...`, `X-Emby-Authorization`, `X-Emby-Token`, and query `api_key` for issued user tokens. Conflicting token values are rejected. Caller-provided user/role attributes never establish authority. Static application API keys are a separate future implementation and are not created by these routes.

Administrator-cookie sessions and Emby-token sessions cannot be substituted for one another. Token secrets are stored as SHA-256 digests, sessions are revocable and expire, and current account disable/demotion state is checked when resolving a token. Emby user tokens currently have a 30-day lifetime; this is a Goby policy, not a proven Emby lifetime match.

The initial Emby user projection disables media/transcode/deletion capabilities until those services exist. Unsupported Emby paths return an error. Full policy/configuration projections, query filters, device/session reporting, user edits, API keys, and the complete media API remain scheduled work. Error DTO/status details and version negotiation also need reference-server/client evidence.

## Delivery and health

`/admin/` serves the built React/MUI application, with route fallback for UI navigation. `/admin/v1` always goes through the API router, including unknown routes. `/healthz` exposes minimal liveness; `/readyz` checks PostgreSQL. The service runs as an unprivileged Linux user in the test deployment.

No consumer web player, library scanner, stream endpoint, HLS job controller, or working hardware pipeline is included in this increment. Those requirements remain in the [active delivery plan](../planning/delivery-and-verification.md).
