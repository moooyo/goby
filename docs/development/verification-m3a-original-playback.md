# M3a original playback verification

Date: 2026-09-09. This verifies one engineering increment, not the complete M3
milestone or general Emby-client acceptance. All functional, database, media, and
HTTP verification ran through `ssh test-env`. Local work was limited to source
editing and the explicitly authorized `go build ./...` compilation check.

## Delivered scope

- GET/POST PlaybackInfo with current authorized source facts, DeviceProfile
  evaluation, explicit original fallback, and truthful conversion limits.
- Authenticated original video/audio GET/HEAD, standard byte/suffix/multipart
  ranges and conditional requests, root aliases, and route-literal case handling.
- PostgreSQL playback sessions and user data, ownership checks, duplicate report
  handling, resume filtering, watched/favorite changes, and recursive folder state.
- Current user-policy projection, probe cache version 2, Linux ctime snapshots,
  and bounded source opening without whole-file hashing before delivery.
- Forty-two additional playback reference captures and four recursive-folder
  captures, bringing the audited reference inventory to 153 records. These are
  synthetic reference observations, not consumer-client acceptance tests.

See [the playback guide](direct-playback.md) for exact behavior and limits and
[the reference report](../research/reference-server.md) for captured differences.

## Compilation and test evidence

Local `go build ./...` passed. The final source snapshot was transferred to the
owned `/opt/goby-test/verify-m3a-20260909` directory. After loading the protected
test environment and checking that PostgreSQL and FFmpeg variables were present,
the remote command was:

```sh
go test -race -count=1 -v ./...
```

All nine test-bearing packages passed, with no skipped tests. The remote log is
`/opt/goby-test/m3a-race.log`. Package times reported by Go were:

| Package | Result | Time |
| --- | --- | --- |
| artwork | PASS | 1.595 s |
| config | PASS | 1.015 s |
| database | PASS | 2.777 s |
| identity | PASS | 17.513 s |
| library | PASS | 16.653 s |
| media | PASS | 2.511 s |
| metadata | PASS | 1.650 s |
| playback | PASS | 1.032 s |
| server | PASS | 102.051 s |

The new tests exercise actual PostgreSQL schemas and real `httptest.Server`
requests. They cover current account/library/playback policy before stream
conditionals; source replacement, inode/size/mtime/ctime changes; probe upgrades;
stream cancellation/open-worker bounds; negotiation and request conflicts;
user/authentication-session/device/item/source binding; duplicate and concurrent
reports; position bounds; folder cycles/cross-library boundaries/atomic rollback;
per-user query state and resume; and expired/session-capacity cleanup.

Early runs identified three test-contract corrections: MP4 must use its canonical
wire container instead of the first ffprobe alias; original-file URLs remain
usable with valid authorization after their correlation play session stops; and
optional FFprobe reference-frame facts must follow actual raw output. The media
fixture now explicitly writes H.264 color metadata rather than assuming generic
encoder flags preserved it. Unknown production facts were not fabricated to
make the test pass. Started/Progress/Stopped status expectations use the runtime
reference's 204 rather than the older SDK's 200.

## Deployed media verification

The prepared source was installed using the maintained foundation deployment
script. The service became ready on `127.0.0.1:18096`, running as the unprivileged
`goby` user. Read-only database inspection confirmed schema version 6.

The maintained [verification script](../../scripts/test-env/verify-direct-playback.py)
then created a marked synthetic fixture, an owned library, and a reusable ordinary
account through the administrator API. It completed with `status=passed`:

| Check | Observed result |
| --- | --- |
| Native authenticated library creation/scan | One movie, completed without a warning |
| Matching PlaybackInfo and mixed-case root alias | DirectPlay/DirectStream true, Transcoding false; stable owned play-session reuse |
| Generated original URL | Relative, token-authenticated, source/device/play-session parameters match |
| HEAD / first-byte Range | 200 / 206 |
| Full original HTTP bytes | Match the generated media |
| FFmpeg decoding of fetched bytes through stdin | H.264 and AAC, 600 video frames, 600.0 seconds, completed |
| Missing/invalid media token | 401 |
| Started and 120-second Progress | 204; one play, durable 120/600-second position |
| Detail and Resume | Matching position and membership |
| Stopped and duplicate Stopped | 204; state stable |
| Played and Favorite | Correct flags, played item removed from Resume |
| Cleanup | User state reset, viewer/admin sessions revoked, no cleanup errors |

No credential or token-bearing playback URL is included in this report. Private
fixture state remains mode 0600 on the test host. The synthetic media remained
unchanged, and the owned library/account are retained for repeatable verification.
A subsequent normal scan of the existing synthetic library completed without a
warning; read-only inspection found its source at probe version 2.

## Remaining scope

External subtitle serving, NextUp, client capabilities/session enumeration,
WebSocket events, remuxing, transcoding/HLS, and real consumer-client workflows
remain open. The administrator frontend was unchanged in this increment; previous
frontend verification is not presented as a new browser test. No GPU was available,
so hardware decoding/encoding remains unverified. The service is a local test
deployment, not a production release. Older media snapshots require a manual
library rescan after upgrading; automatic upgrade scanning is not implemented.
