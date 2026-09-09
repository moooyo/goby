# M5c: administrator login-session management

Verification date: 2026-09-10 (Asia/Shanghai).
Status: **passed for this increment**.

This increment adds the native [login-session API](../api/admin-sessions.md)
and the administrator Sessions page. It preserves the preceding M4f playback
pipeline. It does not implement a complete Emby device registry, application
keys, device-wide sign-in restrictions, or the remaining administrator scope.

## Source and environment

Functional verification uses `ssh test-env`, Go 1.27.1, FFmpeg 9.0.1 and
PostgreSQL 17.11. Go integration tests and the disposable browser database use
port 15432; the retained application database uses port 5432. No local functional
test, media command or browser probe was run. Authorized local Linux/amd64 Go
compilation and the frontend production build both passed.

The final source snapshot is `/opt/goby-test/verify-m5c-20260910`, with 303
Go/module files. Schema remains 14 and probe version remains 6. The isolated
browser used executable SHA-256
`f1eafa5d95c5647464d69a7839791a61f3a9f5d30c3e4a30996a31f6a2a28e13`
and administrator asset archive SHA-256
`a6ed40395cc5af4bf0c5a8e9aed2c94a4261c45b0b0b10c21a23cf091b5fe854`.

## Focused database and HTTP acceptance

The [focused race run](m5c-targeted-summary.json) passed 15 top-level tests with
zero skips. Identity completed in 18.822 seconds and server in 90.325 seconds.
The checks include status/pagination/filtering, strict UTF-8/query/body parsing,
the sixteen-field UTC projection, absence of token/hash/capability data, native
cookie/CSRF versus Emby-token separation, stale actors, idempotency and
self-revocation.

Database barriers observe actual blocked connections rather than assuming
ordering from a delay. Reciprocal administrator revocations cannot both commit
as still-authorized actors. A trigger pauses an already-executed target update;
after the database clock proves the actor expired, the final authorization
check rolls back both ordinary revocation and self-revocation. A valid exact
self-revocation commits, and the old actor cannot authorize another mutation.

Real HTTP integration connects multiple WebSockets and HLS scopes. Holding the
target authentication row demonstrates that runtime retirement occurs after
the database commit; only the targeted session loses its subscriptions and
conversion scope. A same-user sibling remains usable. The original-file test
also checks interruption of a blocked response through its existing watcher,
while a sibling login continues transferring. Nonempty user state, metadata
and account management revisions are preserved.

## Browser acceptance and restart

The [final browser workflow](m5c-sessions-browser.json) passed in **11.240
seconds**, with one scenario, zero unexpected failures, zero skips and zero
flaky tests. It runs a UID-995 application against its own database and
low-privilege role. Node is 20.19.2 and Playwright is 1.63.0.

Three test users, two real same-device Emby logins and 62 historical records
exercise the list. The history includes 52 expired records, eight revoked
records and two disabled records, including an administrator login whose
account has lost that role. The fixture-control login and browser login bring
the initial total to 66 records. This covers real pagination beyond one page
without performing dozens of password hashes merely to seed history.

The scenario checks user/kind/status/device/literal-search filters, UTF-8 search
limits, pagination, refresh, desktop/mobile overflow, and cancellation without a
mutation. Single revocation sends the expected CSRF-protected empty JSON object
and refreshes the list. The target token receives 401 while its same-device
sibling remains authorized. A later normal sign-in on the same device succeeds.
Revoking the current administrator login clears its cookie and returns to the
login screen.

The restart check compares complete authentication/account/catalog snapshots
before probing tokens again. It preserves the recorded revocation, confirms
the revoked token still fails and the sibling still succeeds, and retains the
self-revocation and replacement login. All ten cleanup checks pass: database,
role, application, tagged processes, private temporary files and runtime are
removed, HBA bytes are restored, and the preexisting database catalog and shared
service remain unchanged.

The [first attempt](m5c-sessions-browser-attempt-1.json) stopped because the
prepared root-owned executable had mode 0700, preventing UID 995 from executing
the held descriptor. Its bytes were unchanged when execute permission was
corrected. The [second attempt](m5c-sessions-browser-attempt-2.json) exposed a
verifier type error: PostgreSQL serializes OID as a JSON string, while the guard
expected an integer. The guard now casts OID to bigint. The verifier also makes
its public asset directories readable under a restrictive caller umask and
checks executable permission up front. Both failed attempts cleaned up their
temporary resources. These fixes changed verification setup, not product code
or frontend assets.

The captured [desktop](screenshots/sessions-desktop.png),
[mobile](screenshots/sessions-mobile.png),
[revocation dialog](screenshots/sessions-revoke-mobile.png), and
[signed-out](screenshots/sessions-signed-out.png) views were inspected. The UI
distinguishes an authorized login from online presence and identifies the
current administrator login.

## Complete suite and deployment

`go test -race -count=1 -v -p 2 ./...` passed **954 top-level tests** across all
twelve tested packages, with zero skipped tests and no race findings. The
command package has no tests. Go and the SSH wrapper exited zero. The
[complete summary](m5c-full-race-summary.json) preserves every package duration
and the log hash. The 303-file Go/module manifest has canonical JSON SHA-256
`99fad87ed1f32ecb753dbe25527c909f15761029f2c6eeeb52a42f768c472cf3`.

The final Linux build is byte-identical to the executable used by the accepted
browser workflow. The [deployment audit](m5c-deployment-evidence.json) verifies
PID 3431702, UID 995, all 303 installed Go/module files, schema 14, eleven probe-6
media records, and all 35 current administrator assets. The actual HTTP index
and JavaScript bytes match those assets. Old immutable assets are retained for
earlier loaded clients. Every row in all seventeen public tables is unchanged
across the binary/interface upgrade; no migration or rescan is required.

The [live session workflow](m5c-deployed-sessions.json) passed its first exclusive
attempt with one administrator login and two same-device Emby logins on the
existing test account. It checks actual native list fields/filtering, UTC
timestamps, current-session identity, rejection of Emby credentials at native
routes, target-only revocation, unchanged repeat timestamps and self-revocation.
The revoked token receives 401 while the sibling remains authorized. All three
new logins end revoked, all preexisting rows remain byte-identical, and the
service's PID/start time/UID/binary remain unchanged. The run issues 23 GETs,
ten POSTs and one DELETE, with no source-media or playback-state operation.

A private schema-14/probe-6 database dump, the M4f executable, preceding index
and retained-asset manifest remain under `/dev/shm/goby-m5c-deployment-backup`.
This deployment backup is not a shipped product backup/restore feature.

## Remaining scope

Devices are client declarations, not verified physical identities. Bulk device
operations, application keys, full policies, provider integration, system
settings, audit/log browsing, product backup/restore and complete compatibility
release coverage remain open. This dashboard remains administrator-only.
