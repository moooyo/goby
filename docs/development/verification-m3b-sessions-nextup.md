# M3b client sessions and NextUp verification

Date: 2026-09-09. This is a completed engineering increment within the still-open
M3 milestone. It does not establish full Emby compatibility or client acceptance.
All functional verification ran through SSH on the Linux test host. The only
local verification command was the explicitly authorized `go build ./...`.

## Scope and reference evidence

- Client capability reports and session arrays, with current ownership, presence,
  role and account-state checks.
- Persisted partial player hints and the latest authorized Playing/Paused record
  per authentication session; terminal/duplicate-report behavior is preserved.
- Series-directed NextUp sequences, query projection/counting/pagination, and an
  explicitly documented Goby global-continuation policy.
- Playback Ping aligned with observed 204 success, inert unknown IDs, and the
  missing-key 400 text response.

The [reference report](../research/reference-server.md) adds 79 audited fixtures:
26 session/capability/Ping records, 33 short-episode NextUp controls, 12 independent
ten-minute episode controls, and eight capability-selection controls. The total
is now 232. Earlier fixtures remain unchanged. The series-directed continuation
rule is supported; global reference NextUp remained empty even after playback
events, delayed reads, longer media and Video/Audio capability declarations.
The [NextUp guide](next-up.md) records this unresolved compatibility gap.

## Build and complete Linux suite

`go build ./...` passed locally. The prepared source snapshot was transferred to
`/opt/goby-test/verify-m3b-20260909`. The protected test environment supplied
PostgreSQL and the pinned Go 1.27.1 / FFmpeg 9.0.1 tools. Module/build caches used
the established Goby directories in `/dev/shm`; temporary build/test files used
the dedicated executable 512 MiB tmpfs at `/opt/goby-test/exec-scratch`.

The complete remote command was `go test -race -count=1 -v ./...`. All nine
test-bearing packages passed with no skipped tests; the log is
`/opt/goby-test/m3b-race.log`.

| Package | Result | Reported time |
| --- | --- | --- |
| artwork | PASS | 1.994 s |
| config | PASS | 1.021 s |
| database | PASS | 3.145 s |
| identity | PASS | 25.809 s |
| library | PASS | 20.497 s |
| media | PASS | 2.827 s |
| metadata | PASS | 1.807 s |
| playback | PASS | 1.064 s |
| server | PASS | 133.770 s |

The new tests cover capability JSON bounds and cleanup, live account/role/token
checks, own-versus-administrator visibility, presence versus login expiration,
HTTP capability hints, player-state partial merges and explicit zero values,
invalid-update rollback, concurrent reports, terminal immutability, and
per-authentication-session selection before global limits. NextUp tests cover
series cursor expansion, nullable/special numbering, cycles and nested series,
library/user boundaries, counts and pages, requested fields and projection
switches, and the explicitly chosen global policy. Existing playback, scanning,
metadata, artwork, identity, and migration suites also passed.

## Deployed checks

The maintained foundation script installed the prepared source and started the
service as user `goby` on `127.0.0.1:18096`. Read-only inspection confirmed schema
version 8 and an active service. The updated
[direct-playback verifier](../../scripts/test-env/verify-direct-playback.py)
passed against that service:

- A stale capability Id hint updated only the current authenticated session;
  GET Sessions returned the expected array and declarations, with remote control
  false.
- Negotiated original HTTP delivery retained correct HEAD/Range/authentication;
  fetched bytes matched the source and FFmpeg decoded all 600 H.264 frames and
  600 seconds of H.264/AAC media.
- After Started and 120-second Progress, NowPlayingItem identified the indexed
  item without paths or UserData; volume zero, muted/seek flags and rate 1.25
  persisted when later reports omitted them. Ping returned 204.
- Stopped removed NowPlayingItem, duplicate Stop did not change user state, and
  the verifier restored its user's flags/resume and revoked its sessions.

The [NextUp verifier](../../scripts/test-env/verify-nextup.py) also passed against
the deployed service, then passed using its existing ownership record. It used
an owned Goby library reading the three synthetic ten-minute TV files, without
changing the Emby reference catalog or source bytes. It verified the earlier-gap
cursor, complete remaining sequence, parent filtering, page offset/limit,
Limit=0 totals, and Goby's documented global result. Its user state was restored.

The first attempted library creation correctly returned 503 because the new
synthetic root and season directories were mode 0700. They were made traversable
by the non-root service, and the fixture generator now explicitly applies 0755
after its restrictive umask. Media files and prior reference records were not
changed. This was a fixture-access correction, not an authorization bypass.

## Remaining work

External subtitles, WebSocket/events, remote commands, conversion/HLS, real
consumer-client acceptance, broader policies/administration, and the global
NextUp reference gap remain open. No GPU execution was verified. The React/MUI
frontend was unchanged; this increment does not claim a new browser test. The
test service is not a production release, and completing this increment does not
close the full project goal.
