# Server access policy and administrator restart

The native administrator API manages the global connection boundary separately
from listener bindings and account policies. The policy has its own persistent
singleton and decimal-string revision. Existing settings revisions are unchanged.

## Access and language

`GET /admin/v1/server-policy` returns:

- `Revision`, `UpdatedAt`, `EnableRemoteAccess`, `LocalNetworkSubnets`, and
  `UICulture` (`zh-CN` or `en-US`).
- `Client.Address` and `Client.IsLocal`, computed from the trusted connection
  address rather than a client-supplied classification.
- `Restart.Available` and `Restart.InProgress`.
- `Updates`, with `CanSelfUpdate=false`, `Delivery="docker"`,
  deployment-derived `ConfiguredSource`, and the running Goby version.

Remote access defaults to enabled. The initial local subnets are RFC 1918 IPv4,
IPv4 link-local, IPv6 ULA, and IPv6 link-local networks. Loopback always remains
local even when the administrator replaces the list with an empty array.
Configuration accepts at most 32 distinct canonical CIDRs without host bits.

`POST /admin/v1/server-policy/preview` takes `Revision`, `EnableRemoteAccess`,
`LocalNetworkSubnets`, and `UICulture`. It validates the saved revision and
returns `WouldDisconnect` and the proposed `Client.IsLocal` classification.
Preview never writes or publishes settings.

`PUT /admin/v1/server-policy` takes those fields plus the required boolean
`ConfirmDisconnect`. A policy that denies the current client requires explicit
confirmation. The response includes `DisconnectExpected`, allowing the interface
to explain that the saved boundary prevents another request from that address.
The interface retains drafts and requires saved-state review after an uncertain
write; it never automatically repeats a mutation.

Transactions check live administrator authority before taking policy-row locks,
after waiting, and as their final database operation. The revision is compared
against the locked row. A request admitted before another administrator's policy
change cannot reopen access from an address that the newly committed policy
already denies. Committed policy is published before a successful response.
Policy activity uses the separate `settings` resource ID `server_policy` and
does not record address ranges in the activity payload.

The global boundary applies before CORS, authentication, and WebSocket upgrade
dispatch. It covers login, native APIs, Emby APIs, media delivery, ping, and
static application assets. `/healthz` and `/readyz` remain available for the
deployment's health checks and expose no account or media content.

Published policy changes cancel admitted requests that lose access and interrupt
blocked transport reads and writes. WebSockets register against their independent
socket lifetime so an existing upgraded connection is also closed. The policy
update itself can finish delivering its bounded CAS result, including a confirmed
self-disconnection response.

The server uses the existing `GOBY_TRUSTED_PROXIES` configuration when resolving
forwarded addresses. Headers from untrusted peers are ignored; malformed trusted
chains have an unknown address and cannot obtain local access. A deployment
behind a reverse proxy must configure that trust boundary correctly before it
relies on client-network restrictions. An untrusted proxy is otherwise just the
direct connection peer. No private subnet is implicitly trusted as a proxy.

Individual account restrictions remain additional restrictions. A global local
subnet entry never falsifies a recorded peer address or bypasses an account's
existing remote-access rule. Endpoint classification reports the configured
global local-network boundary.

The anonymous bootstrap response publishes the default interface culture. New
administrator browsers use it when they have no saved language choice; a saved
personal language preference takes priority. Updating the default does not
rewrite personal browser preferences or request authentication data.

## Restart and updates

`POST /admin/v1/server/restart` requires the current policy `Revision` and literal
`Confirm=true`, as well as native administrator authentication, same-origin
validation, and CSRF. The interface additionally requires typing `RESTART`.

`WithAdministratorRestart` is an explicit process-owner capability. Without it,
the API reports restart as unavailable and rejects the operation. The HTTP
handler does not send process signals, exit the process, assume a Docker restart
policy, or access the Docker socket. Accepted requests are fenced against
duplicates and handed to the process owner after the acknowledgment. HTTP 202
means restart was requested; it does not mean a replacement generation is ready.

`POST /emby/System/Restart` provides the core compatibility operation with fresh
Emby administrator or owned application-key authority. It accepts no request
body and acknowledges with HTTP 200 and an empty body. Native cookies cannot
authorize it. The two API audiences share the same pending request latch and
generation owner; duplicate and unavailable requests return explicit Goby HTTP
409 errors. `SystemInfo.CanSelfRestart` reflects that owner's presence and does
not become false merely because a restart was requested.

The separate [server update observation API](server-updates.md) checks an
operator-configured Goby HTTPS feed. Its default is explicitly unconfigured;
failed checks never imply that the running version is current. The deployment
owner replaces Docker images. This API does not install images, offer automatic
application updates, or introduce a writable base-path contract.

## Verification scope

Focused tests cover canonical subnet validation, loopback and unknown peers,
atomic publication and active-request revocation, CAS conflicts, restart
restoration, revoked administrator authority, exact revisions above JavaScript's
safe integer range, CSRF, trusted-proxy spoofing, CORS and WebSocket admission,
self-lockout confirmation, and injected restart ownership. Browser cases cover
save, cancellation, uncertain writes, typed restart confirmation, and responsive
presentation. Test execution receipts belong to the task's verification record.
