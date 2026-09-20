# Selected compatibility phase 4 integrated acceptance

Status: source preparation for the coordinator's consolidated final run. This
document is not a test result. Do not compile, generate media, launch a browser
or receiver, bind listeners, or probe runtime state until all Phase 4 code is
frozen and a new owned remote scope has been admitted.

The journey combines the actual native administrator UI, a declared owned
browser reference client, and the independent `goby-notification-receiver`
HTTPS receiver and console consumer. All business endpoints, authentication,
configuration, playback, session commands, and notification delivery are real.
The fixture does not alter a commercial client, add a product player, replace
business responses, or bypass CSP or TLS verification.

## Execution boundary

The Linux Go fixture is
`TestSelectedCompatibilityPhase4BrowserIntegration`, built with
`goby_embed_admin,goby_browser_integration`. The driver is
`selected-compatibility-phase4-browser.mjs`. The coordinator supplies the
existing private database, Node, Playwright, browser-cache and artifact
environment variables plus `GOBY_SELECTED_PHASE4_EXECUTION_CONFIG` and a unique
`GOBY_SELECTED_PHASE4_RUN_ID`. The execution marker is
`goby-selected-phase4-execution-v1`.

The execution JSON declares the real FFmpeg and ffprobe paths/hashes, the
independently compiled receiver binary path/hash, and private canonical CA,
server certificate and server key paths. CA and certificate hashes are pinned.
The Go fixture verifies the CA file and supplies its explicit trust pool via
`WithNotificationTrustRoots` / notification runtime `RootCAs` on every server
generation. Certificate-chain and hostname verification remain enabled.
Do not add `SSL_CERT_FILE` as a second implicit trust path or install the test CA
globally. The receiver endpoint is an owned HTTPS loopback address with an
appropriate IP SAN; it is never an arbitrary third-party destination.

The browser input, named by `GOBY_SELECTED_PHASE4_CONTEXT`, is a private `0600`
file inside a new owned `0700` artifact directory. Its marker is
`goby-selected-phase4-browser-fixture-v1`. It contains only this run's accounts,
four-digit profile PIN, generated media IDs, the exact current/restarted Goby
origins, reserved next port, receiver endpoint and disposable receiver/target
secrets. Tokens are obtained through actual login, never injected into browser
storage or supplied as fake authentication results. Secrets and full media or
WebSocket URLs are excluded from safe outputs and screenshots.

After source and embedded administrator assets are frozen, the coordinator runs
the exact test in the admitted remote environment:

```sh
"<admitted-go>" test -count=1 -timeout=15m \
  -tags=goby_embed_admin,goby_browser_integration \
  ./internal/server -run '^TestSelectedCompatibilityPhase4BrowserIntegration$' -v
```

The browser has a ten-minute deadline. Real media remains short; this is not a
benchmark. Existing proprietary hosts and all earlier phase receipts remain
untouched. The root-owned browser profile does not establish a non-root
deployment profile, and final host cgroup/database closure requires the
coordinator's separate evidence.

## Native configuration and actual consumers

The administrator sets the viewer's profile PIN through the existing native
local-credentials dialog. A normal password login remains the authentication
step. The reference client then reads only its own authenticated ProfilePin
projection and exercises its explicitly identified local profile-lock control
with one wrong entry and one correct entry. This is not server PIN login and
does not claim another client's PIN interface was tested.

Native Settings saves an exact current CAS revision with CPU decoding/encoding,
two threads, H.264 `fast` / `capped_crf` / CRF 23, and a new desired HTTP port.
The old listener must continue serving; the next port stays reserved by an
owned guard until restart. Go independently performs current server video
negotiation, actual HTTP output consumption and real decoded-video/audio checks
after this save. Its admitted execution snapshot must contain the saved
threads and H.264 controls. Direct-engine tests alone cannot substitute for
this settings-to-server-consumer path.

The fixture uses the real `StartupHTTPBinding`, network listen,
`PublishHTTPBinding`, `HTTPConnectionContext`, Serve/Shutdown/join and
`WithdrawHTTPBinding` lifecycle. After normal client logouts and old-listener
retirement, a fresh application binds the persisted desired port exactly.
The native UI reconnects in a new browser context, performs a real password
login, observes desired=active with no restart required, and saves Threads 3
through fresh cookie/CSRF and CAS. This second deliberate update is recorded
separately from restart persistence.

## Browser reference client

Only `/__selected-phase4-consumer` is fixture-owned. It serves a fixed document
with a profile-lock input, media controls and an actual audio element. Its CSP
permits same-origin connections/scripts, the exact paired owned WebSocket
authority, and local media blobs; the native
application's policy is unchanged. Every other route reaches the actual Goby
handler. The driver installs the declared reference-client logic and genuine
DOM handlers without fabricating media events, samples, clocks or API responses.

The target authenticates with fixed client/device/version metadata, consumes
actual artist/album/track, InstantMix and Search/Hints/detail responses, then
posts real capabilities and subscribes over a real WebSocket. It sends
`SessionsStart` for each connection and requires actual Sessions snapshots;
there is no invented welcome or acknowledgement contract.

Current PlaybackInfo negotiates progressive MP3 output. The reference client
retains that returned server preparation ID separately and preserves the URL's
actual output/source parameters. It supplies a new non-`play_` client playback
reference for the media request, following the existing correlation contract.
Reports and cancellation use that same client reference plus the real target
SessionId. Go independently resolves the mapping and requires the actual
Started/Stopped row to have `ClientCorrelated=true`. The unstarted canonical
negotiation row is retained and distinguished from user playback; it is not
deleted or relabelled to manufacture a count.

Real decoded audio must advance its HTMLMediaElement clock and produce nonzero
samples through a MediaElementAudioSource -> analyser -> destination graph.
There is no oscillator or injected signal. A separate administrator Emby login
admits Pause. The target must actually receive Playstate, pause its audio,
report measured position and paused state, and agree with a fresh Sessions
snapshot. A disconnect, local resume and resubscription demonstrate current
state recovery without replaying an old command. Browser output graph evidence
does not assert that a person heard a physical speaker.

## Matched notification transport

The supported transport is explicitly `GobyWebhookV1`. Native Notifications
configures the HTTPS receiver, allowed loopback network and write-only receiver
credential. The target uses its own real Sessions/Notifications registration
with a private target token and the supported event IDs.

The coordinator starts two independent processes from the pinned reference
binary: `--mode receive` with TLS/credential/target files and private receipt
directory, and `--mode consume` over that directory. The receiver checks
timestamp, version, HMAC and target token before writing the original payload
and accepting its EventId. The separate console consumer observes that receipt
and emits its own consumed EventId. Sender outcome, receiver acceptance, and
client consumption must all bind the same payload generation.

For the first Test event, the controlled receiver uses
`--transient-failures 1` to issue a real 503 before a successful retry. This
fault is at the independent HTTPS endpoint and does not mock Goby. Test 202
means enqueue only; it never marks delivery complete. Later delivery cases use
real favorite/userdata changes rather than bypassing the Test rate limit.

The WebSocket is explicitly closed before an actual userdata event is delivered.
A same-library source denied by policy is then edited through the native
metadata UI. After the durable event is evaluated, the registration must gain
no delivery, receipt, LastOutcome change or updated_at change; a private
dispatcher cursor may advance. Thus a visible parent cannot disclose the
hidden source's activity.

Target-token rotation uses actual CAS and fences old work. Before the next
event, the observer replaces the independent receiver's private token file.
The next actual event must have a new EventId and the new registration revision
as its generation. Revocation and global transport disable are followed by
real source mutations and independent no-new-delivery observations. Already
accepted messages are not claimed to have been withdrawn. No APNs, FCM,
proprietary push, OS toast or human-read receipt is implied.

## Ordered evidence

The 18 stages are `account-pin`, `runtime-saved`, `client-capabilities`,
`audio-playing`, `remote-paused`, `session-reconnected`, `audio-stopped`,
`notifications-configured`, `notification-retried`, `notification-offline`,
`notification-hidden`, `notification-rotated`, `notification-new-target`,
`notification-revoked`, `notifications-disabled`, `listener-restarted`,
`restarted-csrf`, and `cleanup`.

Each writes a bound `stage-<phase>-request.json`. The independent Go observer
records actual store/files/runtime observations and writes a
`stage-<phase>-database.json` acknowledgement with marker
`goby-selected-phase4-stage-database-v1`, the same run/phase, and
`Observed:true`, `Complete:true`, `ExpectedFilesVerified:true`.
Database failure produces an immediate failed acknowledgement with safe facts,
not a blind wait for impossible success.

The runtime-save acknowledgement also includes completed `ManagedVideoEvidence`.
Notification delivery acknowledgements include the actual EventId,
RegistrationId, Generation, Kind, attempt count, receiver acceptance and
separate client consumption. Hidden-source, rotation, revocation and disable
acknowledgements include their actual evaluated/fenced no-side-effect evidence.
The listener-restart acknowledgement gives only the predeclared new owned
origin. A status response or sender admission alone is never acceptance.

`browser-result.json` retains safe HTTP statuses, reference identities, profile
lock outcomes, capabilities and subscription snapshots, command action/report
observations, actual audio samples, runtime desired/active state, independent
delivery evidence and masked screenshots. Named failures preserve bounded
diagnostics. Normal completion requires all 18 checks, zero page errors and
foreign requests, actual logouts, and the driver's separate closure of browser,
receiver/client processes, listeners, workers, subscriptions, delivery attempts,
media readers, caches, database schema, media root and private credentials.
Terminal histories remain observable; active resources and fallback revocations
must be zero.

All protocol, execution-matrix, restart/recovery/no-replay, authority and
backup/restore suites still require their own recorded actual results. This
profile is evidence for the named native UI and owned matched clients, not
universal client or hardware compatibility.
