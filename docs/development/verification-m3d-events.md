# M3d WebSocket and remote-control verification

Date: 2026-09-09. This increment adds authenticated WebSocket notifications and
initial remote-command routing. It does not complete the M3 client-acceptance
gate or the full implementation goal. Local verification was limited to the
authorized `go build ./...`; all tests and deployed exchanges ran through
`ssh test-env` on Linux.

## Reference evidence

The [WebSocket investigation](../research/websocket-reference.md) adds 25 records:
three connection transcripts and 22 HTTP exchanges. The combined reference
directory now has 342 JSON records. The audit compared the new sanitized records
with their private originals, checked for credential leakage, and preserved all
634 pre-existing raw/export files by SHA-256.

The new controls establish live-connection/capability behavior for the owned
target, exact Playstate and GeneralCommand envelopes, full-body string arguments
versus the named route's empty arguments, and an offline command's 204 response.
The distinct ordinary caller's accepted reference request is recorded together
with its shared-device policy; it is not treated as a complete authorization
model. Neither administrator text nor binary SessionsStart produced a Sessions
event in the short observation windows.

## Compilation and Linux tests

The final local `go build ./...` passed. The final Go source, scripts, and reference
fixtures were transferred to `/opt/goby-test/verify-m3d-20260909`. Verification
loaded the protected PostgreSQL/media environment and used dedicated Goby caches;
both GOTMPDIR and TMPDIR pointed to the owned executable tmpfs. Credentials were
not printed or copied into repository artifacts.

The complete remote command was `go test -race -count=1 -v ./...`. All eleven
test-bearing packages passed with no skipped tests. The retained complete log is
`/opt/goby-test/m3d-race.log`.

| Package | Result | Reported time |
| --- | --- | --- |
| artwork | PASS | 2.380 s |
| config | PASS | 1.013 s |
| database | PASS | 3.145 s |
| events | PASS | 1.124 s |
| identity | PASS | 28.986 s |
| library | PASS | 24.082 s |
| media | PASS | 2.922 s |
| metadata | PASS | 2.054 s |
| playback | PASS | 1.030 s |
| server | PASS | 168.013 s |
| subtitle | PASS | 1.141 s |

New coverage includes:

- Hub quotas, immutable shared event data, concurrent publishers, user/session
  isolation, bounded slow-consumer eviction, and cancellation.
- Revalidation without retaining a token or extending expiration, with current
  role/policy/account state and rejected owner/kind/session combinations.
- Latest user-data pages, ancestors and recursive descendants, pagination beyond
  one page, cycles, current ACL, and unsupported-item filtering.
- Notification coalescing, changes during an active read, ordered pages and
  shared MessageId, per-user/global overflow, failed-query resynchronization,
  old-task isolation after reconnect, and cancellation-aware shutdown.
- Real RFC 6455 connections through all five aliases, token/header/query and
  cookie separation, Origin rejection, four-socket quota/recovery, Ping/Pong,
  inert text/binary messages, logout, expiration/disable, and repeated shutdown.
- Queued user-data filtering after ACL changes, identical publication identity
  across same-user sockets, and actual HTTP favorite-triggered notifications.
- Pending 101 writes interrupted during shutdown, quota/WaitGroup release, late
  successful hijacks fenced from readiness, and unsupported-deadline rejection.
- Remote-command ownership/admin rules, invalid targets, full/named bodies,
  bounded JSON/arguments/item lists, signed relative versus absolute seek bounds,
  current target playback access, and queued command permission revocation.

Earlier identity, migration, library, metadata, artwork, playback, and subtitle
tests passed in the same run. The bounded handshake fix was found during code
review and included before this full suite. Initial subsystem and targeted
server runs are retained as `m3d-domain-race.log` and `m3d-targeted-race.log`.

## Deployed workflow

The maintained deployment script built the tested source and restarted only
`goby-foundation-test.service`. Readiness passed on `127.0.0.1:18096`; systemd
reported `User=goby`, `ActiveState=active`, and `SubState=running`. This increment
uses schema version 9 and requires no migration or rescan.

The [deployed verifier](../../scripts/test-env/verify-websocket.py) uses the owned
direct-playback media read-only, two new sessions for its reusable viewer, one
separately recorded isolation account, and a dedicated administrator session.
Its [sanitized result](m3d-deployed-websocket.json) is included locally and retained
on the test host as `/opt/goby-test/m3d-deployed-websocket.json`.

The first deployed execution passed with exit code 0. No verifier or production
fix was needed after deployment.

| Check | Observed result |
| --- | --- |
| Authenticated upgrade aliases | All five returned 101 |
| Missing/invalid token | 401 before upgrade |
| Live connection without/with media control declaration | SupportsRemoteControl false/true; RFC Ping/Pong passed |
| Favorite update | Newly committed HTTP state reached both same-user sessions with shared MessageId and payload |
| User isolation | Neither ordinary account received the other's state notifications |
| Full/named GeneralCommand | Full SetVolume preserved string `"37"`; named command had empty Arguments |
| Administrator Pause and PlayRequest | Only the targeted session received the expected message; no fabricated NowPlayingItem |
| Other ordinary user's control request | 404, no command delivered |
| Offline target | 204, SupportsRemoteControl false, no replay on reconnect |
| Declaration toggle while connected | Current SupportsRemoteControl followed the declaration |
| Logout | Both sockets for the revoked session closed; old token failed HTTP and WebSocket with 401 |
| Independent sibling | Stayed connected and answered RFC Ping; revoked session disappeared from listing |
| Cleanup | User state restored; capabilities reset; all new login sessions revoked; media unchanged; no cleanup errors |

The new reusable isolation account's ownership/credential record is root-only
0600 on the test host. No password, token, credential URL, or raw notification
payload is included in the result artifact.

## Remaining scope

The [event guide](websocket-events.md) documents initial limits and deliberate
behavior differences: authentication before 101, explicit same-user/admin
control, signed relative offsets without a matching reference capture, and
required JSON bodies for PlayRequest. HTTP 204 does not confirm client execution.
Queued events have no durable replay, and clients must refresh after reconnect.

Session-list subscriptions, library-change broadcasts, the complete shared-device
and remote-control policy matrix, remaining command families, richer subtitle
handling, conversion/HLS/hardware, broad administration, and real third-party
client release evidence remain open. No new browser test or hardware execution
is claimed. The React/MUI frontend has no consumer playback page.
