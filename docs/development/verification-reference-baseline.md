# Reference baseline and first interoperability corrections

Recorded on 2026-09-09. The official Emby 4.9.5.0 reference package, setup, isolated network namespace and 65 audited captures are documented in [reference-server.md](../research/reference-server.md). The reference is separate from Goby's PostgreSQL test deployment and does not expose a host-network listener.

The captured responses revealed differences not safely inferable from the generated API export. This increment corrects:

- Ping's exact text, content type and GET/POST/HEAD byte semantics.
- Legacy `MediaBrowser` authorization, four separate client/device headers, and `X-MediaBrowser-Token`.
- Confirmed authentication error status/text/content-length behavior.
- Browser-client OPTIONS/CORS handling on `/emby`, with the native admin API remaining separate.
- Empty HTTP 204 results for Emby library creation and refresh.
- Explicit list `Path` projection, fuller authorized item detail and media-source paths, and disabled image-field suppression.

Path projection now follows observed API behavior for authorized items. Default list queries still omit those paths, and library/account permissions are applied before returning any data. The earlier blanket omission was a design assumption, not a documented Emby contract.

## Verification

- Authorized local `go build ./...` passed.
- Remote `go test -race -count=1 ./internal/server` passed in 35.800 seconds after the authentication, CORS, Ping and projection changes.
- The subsequently added `TestHTTPLibraryMutationStatusesMatchReference` passed remotely with `-race` in 2.098 seconds. It checks both 204 responses and the actual completed scan side effect.
- New PostgreSQL-backed authentication tests cover accepted carriers, exact error text/length/type, conflicting inputs, legacy-token logout and continued privilege isolation.
- Unit tests cover Ping's three methods and preflight behavior, including the absence of compatibility CORS on `/admin/v1`.
- Projection tests retain default-list omission checks and verify requested/detail paths only after authorization. Existing restricted-library tests continue to pass.

The fixtures were audited on the remote host against private originals. Headers, JSON types, numbers, booleans and nulls are preserved; passwords/tokens are redacted. Synthetic IDs remain unchanged to avoid corrupting unrelated numeric strings. No reference credentials were committed.

## Coverage limits

This is selected contract adoption, not a claim of complete Emby compatibility. The capture has no real third-party-client pass, no decoded HLS segment comparison, and no full WebSocket or subtitle exchange. Music grouping needs richer tagged fixtures. Additional query defaults, errors, options and policies remain open. A runtime OpenAPI export and an automated broader differential harness are still release work.

The native administrator API is a Goby contract and retains its own session cookies, CSRF checks and JSON error format. Reference CORS behavior does not change those authorization boundaries. The current parser deliberately rejects conflicting identities/tokens; reference precedence for conflicting carriers remains unmeasured.
