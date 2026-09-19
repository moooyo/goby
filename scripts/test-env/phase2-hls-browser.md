# Phase 2 native HLS browser acceptance harness

Status: authored; not executed. The parent must first freeze the integrated
phase 2 source, prepare its owned fixture server, and admit the run on `test-env`.
This harness does not add a consumer player, change production web assets, or
connect to an existing reference server.

## Engine and entry point

The Playwright entry point is
`web/admin/e2e/phase2-hls-media.spec.ts`, using the repository's existing
`@playwright/test` and Chromium installation. No new npm dependency is needed.
The engine is the retained official client package's existing AMD module:

```text
/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/dashboard-ui/modules/hlsjs/hls.js
```

Read-only inspection identified HLS.js `1.6.0-beta.2`, exported as the AMD module's
default export, with SHA-256:

```text
04a55387b26d6becff5b87b470b9b19c3fe41d25c0cd3c1e6c3d885a56572fdb
```

The harness reads that local bundle, verifies its hash, and loads it with a
minimal `define(["exports"], factory)` adapter. Workers and debug logging are
disabled. It does not execute the Emby Web application. All browser requests
must remain on the fixture's explicitly owned loopback origin.

## Private input contract

`GOBY_PHASE2_BROWSER_CONTEXT` names a new absolute JSON file owned by the test
process, with mode `0600`, inside an owned `0700` artifact directory. The file
must be a bounded regular file, not a symlink. Its `RunId` must match
`GOBY_PHASE2_BROWSER_RUN_ID`. The parent removes the input after the browser
and fixture server have stopped. Do not put actual tokens or this input in the
repository, screenshots, traces, reports, or task messages.

Required top-level fields:

| Field | Meaning |
| --- | --- |
| `Marker` | `goby-phase2-hls-browser-fixture-v1` |
| `RunId` | Unique admitted run identifier, 8–128 safe ASCII characters |
| `BaseURL` | Owned `http://127.0.0.1:<ephemeral-port>` origin; protected reference and database ports are rejected |
| `Token` | Current fixture user's authentication token, passed only in memory or request headers |
| `HlsBundlePath` | Absolute local path to the exact reviewed engine above |
| `ArtifactsDir` | The input file's private parent directory |
| `ResultPath` | New `.json` file directly inside `ArtifactsDir` |
| `Finite` | Finite source fixture described below |
| `Dynamic` | Dynamic source fixture described below |

All URL/path fields are relative to the owned origin and cannot contain a host,
fragment, or credentials in their authority. Playback URLs may retain their
normal authenticated query values inside the private context, but are never
copied into the result. Controls and observations use `/__phase2-*` fixture-only
paths. Playback and stop requests use actual product routes.

`Finite` fields:

- `PlaybackURL`: an already negotiated master URL with its immutable Goby HLS
  revision and complete playback/source/device scope. It must expose at least
  two distinct text subtitle languages.
- `ObservePath`: private observation GET described below.
- `StopPath`: the actual scoped `DELETE /Videos/ActiveEncodings` URL.
- `Tracks`: two to eight objects containing `StreamIndex`, `Language`,
  `CueText`, `StartSeconds`, and `EndSeconds`. Languages must be distinct.
  These facts come from the authored subtitle fixtures, not parsed server VTT.
- `ProbeSeconds`: a known source-time point inside every test cue both before
  and after the positive delay. Keep at least 0.15 seconds away from boundaries.
- `TimelineOffsetSeconds`: the independently measured actual media PTS minus
  source PTS. The parent must establish this from the returned A/V media and
  source markers; it must not copy the production subtitle-clock calculation.
- `PixelRGB`: the known RGB color at `ProbeSeconds`, three integer components.
- `OffsetTicks`: a positive caption delay between 2,000,000 and 10,000,000 ticks.

The first and second tracks need cues lasting long enough for browser scheduling
and a seek to `ProbeSeconds`; a 3–5 second continuous caption is suitable. Use a
known source color covering the same interval. The finite source needs enough
remaining duration to avoid ending while the view changes are exercised.

`Dynamic` fields:

- `PlaybackURL`: an already negotiated dynamic HLS master URL with an authorized
  text rendition and a bounded retained window.
- `ObservePath` and `StopPath`: private observation GET and actual scoped DELETE.
- `DisconnectPath` and `ReconnectPath`: fixture-only POSTs controlling this
  run's upstream transport. The first response acknowledges actual upstream
  closure; reconnect starts a new real transport epoch. Neither action changes
  unrelated streams, settings, or retained reference servers.
- `SubtitleLanguage` and `CuePrefix`: identify real continuously generated live
  captions. Each cue must describe the current media interval and retain the
  same language/prefix after reconnect.
- `PauseSeconds`: 3–8 seconds of observed server-window advancement while the
  actual video remains paused. Configure retention longer than this pause.
- `ExpectedExpiredStatus`: the product's explicit `404` or `410` contract for an
  actually observed segment URL after eviction. This is asserted exactly.

Use short real segments and a retained window that allows replay, then evicts
the first observed fragment within 40 seconds. Continue source ingestion while
the browser is paused. The source must outlive the complete dynamic workflow.
The reconnect must publish a genuine discontinuity visible to the HLS engine.

Both observation GETs return this shape without tokens or media paths:

```json
{
  "ProducerIds": ["owned-producer-id"],
  "ActiveJobs": 1,
  "Epoch": 1,
  "MediaSequence": 5,
  "WindowStartTicks": 50000000,
  "WindowEndTicks": 140000000
}
```

`ProducerIds` must be derived from actual owned runtime/manager state, not the
request plan or a constant. Finite views retain one producer ID. Dynamic pause,
replay and live-edge return retain their producer; disconnect/reconnect may
create a new epoch and producer. `Epoch` and retained-window ticks likewise
come from actual current state. The observer must enforce this fixture's token,
item, playback reference, source and ownership before returning evidence.

## Coverage and evidence

The harness renders a private test page through Playwright request interception,
then runs the retained HLS engine against actual Goby URLs. It checks:

1. Real decoded video frames and source-color pixels; actual native text-track
   `cuechange` events and cue start/end against independently supplied source
   facts and transport offset.
2. Selection of both languages, removal of the previous language, subtitle off,
   and a delayed caption view. The offset reload uses public `loadSource` and
   native media seeking; it does not modify HLS.js controller internals.
3. Unchanged actual A/V producer identity across selection/off/delay. Reloading a
   client manifest for the offset is not evidence of producer reuse by itself.
4. Dynamic captions, a stable paused media clock while server ingestion and
   live playlist updates continue, actual resume, replay within the current
   observed HLS fragment window, and return to the engine's live sync position.
5. Actual transport reconnection, an increased observed server epoch and HLS
   discontinuity counter, newly presented frames/cues, and exact expiry of the
   original fragment URL after eviction.

The result is a private credential-free JSON file recording snapshots, cue
events, media clocks, source observations, engine identity, errors, and cleanup
disposition. It does not record full playback URLs. A failure still writes a
partial result with `Complete=false` and attempts to stop its active owned play.
No playback report, screenshot, or trace is treated as proof of audio waveform
synchronization; the phase 1 media tests cover that separate contract.

Every explicit load, resume, seek and live-edge return now waits for three new,
advancing `requestVideoFrameCallback` observations. Seek and live transitions
also require their own `seeked` event. The frame presentation timestamp must be
after that transition started, its browser frame counter must exceed the
captured counter, and the video must be playing, no longer seeking, and have a
decoded frame. Media-clock versus presented-frame tolerance remains 0.3 seconds.
The result retains each transition's baseline, recent callbacks, three settled
frames, completion status and wait duration. It also attempts to preserve a
final snapshot before cleanup when an assertion fails.

After the actual paused position resumes and produces new frames, the harness
explicitly returns to the current live synchronization point before selecting
a replay target. It records that settled, retained-window `ReplayOrigin`, checks
that the producer is unchanged, then requests exactly one second backward inside
the retained bounds. The existing backward-distance, decoded-clock and final
return-to-live assertions remain in force. Client-buffered bytes can outlive the
server's current window; resuming those bytes is not a valid origin for a new
server-window rewind. Retention is not extended to accommodate the test.

The first actual run, `phase2-browser01` on source 12, completed finite eight-track
selection/off/delay and dynamic pause/resume. Its replay snapshot had media time
11.012313 and presented frame time 11.0. The following live-edge request had
already assigned media time 9.738708 while the last callback still reported
11.0 and the frame counter remained 19. The failed 0.3-second assertion therefore
observed the previous seek's frame; it did not establish a product decoding
failure. The presentation barrier addresses that observation race without
relaxing the assertion and subsequently passed the actual browser workflow.

`phase2-browser04` on source 18 with the formal v3 runtime established another
fixture precondition: the resumed client was correctly presenting 4.976609
seconds (last decoded frame 4.958333), while its newest playlist retained
`[6,18)` seconds. Clamping a rewind to that window therefore selected a forward
position and correctly failed the unchanged backward-distance assertion. The
new explicit live-to-`ReplayOrigin` step separates successful client-buffer
resume from a fresh retained-window rewind. This refinement awaits execution.

## Remote execution after fixture agreement

Copy the frozen browser source and existing Playwright config into the owned
browser work directory using the established phase 1 workflow. Keep the engine
file, context and output outside the frozen source tree. Preserve the existing
Node/Playwright versions and browser cache. After the parent verifies source
identity and prepares the owned server, run only the named test:

```sh
GOBY_PHASE2_BROWSER_CONTEXT=/private/run/context.json \
GOBY_PHASE2_BROWSER_RUN_ID=phase2-browser-owned-run \
PLAYWRIGHT_BROWSERS_PATH=/existing/private/browser-cache \
node /existing/playwright/cli.js test e2e/phase2-hls-media.spec.ts \
  --grep '^phase2 native HLS browser verifies subtitle views and bounded dynamic playback$' \
  --project=chromium --workers=1 --retries=0
```

The paths above are placeholders, not executable admission details. The parent
still owns fixture readiness, process lifetime, per-run ownership, source-bound
verification, retained evidence and teardown. This document does not authorize
starting the server or running the browser before that agreement.

## Owned Go fixture launcher

`TestPhase2HLSBrowserFixtureServer` in
`internal/server/phase2_browser_fixture_integration_test.go` now implements the
private input and observation contract under the `goby_browser_integration`
build tag. It requires these additional launcher settings:

| Environment variable | Value |
| --- | --- |
| `GOBY_PHASE2_BROWSER_DIRECTORY` | New owned `0700` run directory |
| `GOBY_PHASE2_BROWSER_RUN_ID` | Same run ID passed to Playwright |
| `GOBY_PHASE2_HLS_BUNDLE` | Reviewed local HLS.js bundle path |
| `GOBY_TEST_DATABASE_URL` | Parent-admitted disposable integration database |
| `GOBY_FFMPEG`, `GOBY_FFPROBE` | Exact phase 2 media toolchain binaries |

The existing integration-account, HLS and dynamic-fixture helpers accept an
optional timeout only for this explicit browser profile; ordinary callers keep
their original 90-second deadline. This fixture has a 240-second context. The
Playwright test has a 150-second inner deadline and fixture API requests have a
12-second bound, leaving room for failure JSON, stop requests and screenshots
before the orchestrator's 180-second Node deadline. Keep
the parent's outer process/service limit at 360 seconds so bounded cleanup can
finish after a test failure.

Preparation creates a second finite catalog item with eight real indexed SRT
sidecars. Its 24 fps source is green from 3 through 6 seconds, captions span 3
through 8 seconds, and the probe is at 4.25 seconds. The fixture independently
decodes the actual returned fMP4 segments and probes the source frame timestamp;
`finite-clock-evidence.json` preserves the source digest, output frame PTS, and
resulting transport offset before it publishes `context.json`.

The dynamic fixture reuses the authenticated HTTP source contract, then replaces
the finite burst with an actual paced looping FFmpeg MPEGTS response. Its source
clock grows across loops while the browser pauses. The retention store uses the
real wall clock, not the deterministic clock used by ordinary store tests.
The disconnect control cancels and waits for the first actual upstream FFmpeg
process to finish; a new HTTP connection waits at the resume gate. The eight
real document tracks remain available across the new source epoch.

The fixture writes its token only to private `context.json`; observers read
actual session/manager records and `Store.Snapshot`, with current principal,
item, source and playback checks. An authenticated observation may `Touch` the
retained window as a real consumer. It does not acquire or fabricate upstream
generations, job states, source timestamps or producer identities.

After Playwright exits, the parent writes a new `0600` `owned-stop.json` in the
same directory:

```json
{
  "Marker": "goby-phase2-hls-browser-stop-v1",
  "RunId": "the-exact-admitted-run-id"
}
```

The Go fixture validates that marker and the browser result, then independently
requires all owned sessions, active jobs, retained media/readers, stream slots
and upstream processes to have retired through the actual stop routes before
fixture-wide shutdown. A failure still closes both runtimes, the actual upstream
process groups and HTTP servers. It removes only its own context file and writes
credential-free `fixture-result.json`. The parent retains the browser and media
evidence and performs its outer process/database ownership audit.
