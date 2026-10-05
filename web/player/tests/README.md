# Player acceptance

This directory provides isolated browser acceptance data. No fixture code, sample media, or prototype runtime is imported by the production application.

The October 5 review of the other player pages is consolidated in
[DESIGN-REVIEW.md](../DESIGN-REVIEW.md). Its original-HTML comparisons use matching
data and state; their screenshots are separate from the 142 distinct functional
regression cases, all of which passed across the completed groups. The last
shared rail-color change passed three overlapping cases. All module visual
reviews and final production/Docker build/deployment are complete. The separate
final-image read-only live walkthrough passed 14 page/viewport groups with 30
screenshots and unchanged viewing/session/task/queue state. Follow that record
for final versus intermediate evidence rather than counting retained
screenshots, live page groups, or focused reruns as new distinct fixture cases.

The fixture HTTP server binds only to `127.0.0.1`. It serves the built `dist` directory and implements the supported Goby/Emby request shapes used by the player. Its mutations affect an in-memory test catalogue only. The test login is `reviewer` / `goby-player-test`; it is not an account on a real Goby server.

## Fixture run

Run local verification only when authorized for the current task. From `web/player`, after dependencies are installed:

```powershell
npm run build
npx playwright install chromium
$env:GOBY_FIXTURE_PORT = '4180'
npm run test:e2e
```

The port variable isolates the fixture state and Playwright output from parallel visual reviews. Test screenshots, reports, API audit attachments, and image caches are written under `.artifacts/player-acceptance` at the repository root. Non-default ports add a port suffix to the report, results JSON, and Playwright output directory.

Complete the build before starting browser tests and leave that `dist` unchanged
through the run. Replacing lazy chunks while a browser is using them can create
a stale-chunk failure unrelated to the behavior being tested. Concurrent runs
need separate built source directories as well as separate ports if either run
can rebuild assets.

The selected follow-up runs checks on `ssh test-env`. For the existing owned
source mirror, a focused run can be started from local PowerShell with:

```powershell
ssh test-env 'cd /opt/goby-test/player-live-20261004-165c/source/web/player; GOBY_FIXTURE_PORT=4180 npm run test:e2e -- tests/credits.spec.ts tests/catalog-theme.spec.ts'
```

This assumes that dependencies and the matching production build are already
prepared in that source mirror. It does not deploy or test the live Docker
service. Use the repository's designated verification environment unless local
checks are explicitly authorized in the current task.

For a manual review, start the server and open its printed address:

```powershell
$env:GOBY_FIXTURE_PORT = '4174'
node tests/fixture-server.mjs
```

`/__fixture/state` exposes the request/event audit. `POST /__fixture/reset` restores the in-memory data; it does not erase the accumulated unknown-route audit. `POST /__fixture/config` injects deliberate response failures or empty data for tests.

## Manual demo mode

Set `GOBY_FIXTURE_DEMO=1` before starting `tests/fixture-server.mjs` to use
populated presentation data in an ordinary browser. The default acceptance
fixture remains unchanged. The demo includes 19 movies, 7 series, 10 seasons,
65 episodes, favorites, resume state, artwork, thumbnails, audio waveforms,
and text subtitle intervals. `deep-1-1` additionally replays the handoff's
two PGS tracks and English SRT track. Its compact reference data is stored in
`data/manual-demo-timeline.json`; these are presentation fixtures, not results
of analyzing the generated sample video. Dynamic backgrounds default off and
can be enabled in Settings.

With the production bundle already built, start the local demo from this
directory's parent (`web/player`):

```powershell
$env:GOBY_FIXTURE_DEMO = '1'
$env:GOBY_FIXTURE_PORT = '45174'
$env:GOBY_DEMO_LONG_MEDIA_FILE = (Resolve-Path '../../.artifacts/player-demo/deep-48min.webm').Path
node tests/fixture-server.mjs
```

The optional long-media file must be generated before using that command:
loop `tests/assets/sample.webm` with FFmpeg's `-stream_loop -1 -t 2880 -c copy`.
It lets the featured episode actually resume at the handoff's 22:52 position.
Other items use the existing 30-second sample. Playback negotiation describes
the VP8/Opus/WebVTT demo stream separately from the handoff's media metadata.
This fixture has no transcoder and does not play full movies or original
TrueHD/PGS tracks. Writes affect only the process's in-memory state; restarting
the server restores the seed. The login remains the fixture account above.

## Data and media provenance

`data/catalogue.json` contains only the fictional catalogue metadata and image IDs supplied in the design handoff. `import-handoff.mjs` can regenerate it from the handoff's `goby-core.js`; production code never executes that runtime. The fixture preserves the same Picsum image IDs and image aspect ratios as the reference. Images are fetched on first use and cached in the artifact directory, so initial visual verification requires network access.

`assets/sample.webm` is a generated 30-second VP8/Opus video. It contains a moving progress line, time text, and a quiet synthesized tone. It is served with HTTP byte ranges and decoded by the browser's actual media element. The catalogue retains the design's movie/episode metadata while `PlaybackInfo` advertises the short test source duration; resume positions are reduced to seconds within that source. This deliberately shortens playback acceptance while retaining recognizable design data.

To regenerate the video, run `node tests/generate-media.mjs`. The script uses Playwright Chromium and an FFmpeg remux to add a finite duration and seek metadata. On Windows it discovers the FFmpeg binary installed by Playwright; elsewhere provide `GOBY_FFMPEG` or put `ffmpeg` on `PATH`.

The optional `/reference/` route serves the original handoff read-only from `GOBY_HANDOFF_DIR` (default `D:/Code/design_handoff_goby_player`) and redirects its Picsum assets to the same cache. It is intended for visual comparison only and is not needed by the acceptance tests.

`assets/background.mp4` is a separate generated fixture for the subsequent
background-preview integration. The owned FFmpeg preparation produced a
58,754-byte, three-second, 320x180 silent H.264 MP4. It contains synthetic test
images, not an excerpt from the user's library. The fixture-only
`/__fixture/background.mp4` route serves it with the generated-preview request
shape; the production bundle does not import this file.

## Scope

The tests exercise login and session restoration, backend errors, catalogue filtering and search, favorites and watched writes, season navigation, media information, persisted preferences, mobile bounds, and actual video playback. Playback assertions cover resume/seek, timed progress, stop ordering, stream changes, speed, subtitles, episode selection, auto-hide, completion and next-episode cancellation.

`catalog-theme.spec.ts` adds mixed-result pagination, equal item/entity IDs,
entity-ID navigation, server-filter query/count behavior, silent static theme
playback, compatible trailer fallback, reduced-motion/small-screen stills, and
failure/navigation cleanup. `credits.spec.ts` covers marker validation, source
chapter precedence, playing-time countdown, pause/buffer/seek behavior,
cancellation across stream replacement, manual mode, actual stop position,
and the unmarked end-of-file fallback. The direct-source fixture also advertises
HLS capability metadata to detect confusing available conversion with the
selected direct playback URL.

`background-preview.spec.ts` exercises the generated-file fallback, source
priority ordering, persistence across reload and accounts, older/malformed
preference defaults, still-only mode, decode/metadata failures, and abandoned
lookups during navigation. It checks real decoding of the short generated
fixture and that background reads emit no PlaybackInfo, Playing, or generation
requests. The final browser regression passed 40 cases on `test-env`: ten new
background cases, six catalog/theme cases, and 24 existing cases. Actual
source-side generation passed a separate 12-phase Docker run. Nine administrator
Node checks, real FFmpeg 9.0.1 media checks, and PostgreSQL 17 recovery checks also
passed. Their exact scope, retained failures, and reruns are recorded in
[ACCEPTANCE.md](../ACCEPTANCE.md).

`subtitle-timelines.spec.ts` covers source-aligned embedded PGS/DVD interval
documents, strict source/version/stream and URL validation, actual interval
gaps, empty or malformed results, stage/account cancellation, and bounded
request concurrency. A missing or unusable text/bitmap timeline removes both
its label and lane; track selection remains independent. The fixture returns
`Available:false` by default and never runs generation. The 35 new browser
cases and all 101 earlier player cases passed on `test-env`; real bitmap
extraction and the deployed Docker journey have separate evidence in
[ACCEPTANCE.md](../ACCEPTANCE.md). Those are historical functional receipts,
not proof that the populated timeline matches the handoff. In particular, the
12-second live fixture has no artwork, thumbnail set, audio tracks, or saved
resume position; its stale capture intentionally hides the subtitle rows.
Neither that capture nor the seven-phase lifecycle pass establishes complete
design fidelity. The retained mobile capture also shows overlapping ruler
labels despite having no horizontal document overflow.

## Same-data timeline visual correction

Status: completed within the documented visual-fixture scope. Build, focused
regression, final badge checks, final same-data comparisons, and the preceding
read-only live review passed. The matching player production/Docker builds and
[67 focused browser cases](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-regression.log)
passed on `test-env`. The final source refinements passed a
[five-case layout recheck](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-layout-final.log);
these overlap the 67 and must not be reported as 72 distinct cases.
Two [final badge endpoint cases](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-head-final.log)
also passed and overlap the same 67. Fifteen same-data captured states with
45 primary screenshots passed on the final badge version, with `failures=[]`
in the [final result](../../../.artifacts/timeline-visual-review-20261005/after/results.json).
Existing generation and persistence acceptance remains separate and must not
be rerun or relabeled merely to imply visual coverage.

Use the same deterministic data and viewing state on both sides of the
comparison: loaded artwork, twelve filmstrip frames, two audio waveforms,
three subtitle rows, source duration, active track choices, and saved resume
position. The final reference is the original handoff's `deep` season-one first
episode: 48 minutes, 3840 x 1608 video, and 22:52 saved progress. Both PGS tracks
have 33 reference intervals; the text track has 35. Reference SVG amplitudes
are represented by valid GAWL fixture payloads, not claimed as real-media
analysis output. Keep the handoff's actual styles as the reference; do not
normalize away implementation differences by copying the implementation's
styles into it. Generation, source fencing, and real extraction retain their
separate real-media tests.

The correction covers twelve filmstrip frames; compact subtitle codec labels;
removal of invented left-edge lane stripes; two-digit minutes in the resume
badge; the audio label's two-pixel line gap; one visible layer of measured peak
amplitudes at 56% bar width; removal of the added technical legend; adaptive
five/three/two ruler labels; and a resume badge constrained to the track. It
places the badge above the ruler (`top: -25px`) for track containers up to
340 px, removing mobile text occlusion while preserving desktop and axis layout.
It also respects the server's display language, uses compact `TrueHD Atmos 7.1`
and `AC3 5.1` specifications, and displays integer Mbps values without `.0`.
Track code and codec labels do not shrink when a subtitle name is long; the
name is ellipsized instead. Waveform sampling uses untransformed `clientWidth`
to avoid StageDeck scale effects. Media-page bottom padding follows the handoff:
`clamp(40px, 7vh, 72px)` on desktop and `100px + safe-area-inset-bottom` on mobile.
Waveform display uses at most 180 bins, not necessarily 180 bars: invalid gaps
can split bins, and those gaps must remain visible. The API, peak/RMS storage,
subtitle intervals, sidecar lifecycle, and valid-only subtitle row policy do
not change.

The final deployed player image is
`sha256:2bcfffd9c5162fdaa0c2e37e409d40d090cac288224655885901c87e80ca1e84`;
the backend remains at its earlier `sha256:3460d1ccc03070898b1cb9615fa46be2f9dc39308569f86d8b61efa03540fb2a`.
The [final deployment receipt](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-deployment-final.json)
records hashes of the three changed production files and matching contents for
257 player build assets. The [final production build](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-build-final.log)
and [final Docker build](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-image-final.log)
passed. The earlier `9f0c2b7c...` and `9aae8518...` images are intermediate
stages. The latter has its own
[deployment receipt before the badge refinement](../../../.artifacts/timeline-visual-correction-20261005/timeline-visual-deployment-before-bubble.json).

Review the complete populated timeline at desktop and mobile sizes using the
following checks. Final captures establish the presentation results; focused
regression separately covers invalid-data and error handling:

- Twelve loaded frames, both audio rows, and all three valid subtitle rows are
  present before capturing a complete-state image.
- Row labels align with their lanes, codec names do not crowd the language
  labels, and the selected tracks use the same reference accent.
- Subtitle blocks retain actual timing and the existing 14.4-pixel height in a
  24-pixel lane. Do not fake block density by changing interval coverage.
- Ruler labels remain `HH:MM:SS` and readable at the actual container bounds:
  five above 340 px, three above 200 through 340 px, and two at or below 200 px.
  Rendered checks passed with three labels at a 393-pixel viewport and two at
  375 pixels, without overlap. This fixes a mobile overlap inherited from the
  original handoff rather than reproducing that defect.
- The resume line stays at its true time while its badge remains within the
  track near the beginning and end. Also review a typical middle position.
  Both final endpoint cases passed across 320-, 375-, 393-, and 1440-pixel
  viewports. For narrow tracks, assert `badge.bottom <= tick.top - 1` in
  addition to horizontal containment to prevent the badge covering ruler text.
  All six final 393/375-pixel implementation states have 2 px of vertical
  clearance above the timestamps.
- Missing, stale, empty, failed, or unsupported subtitle data still hides the
  whole corresponding label/lane pair independently of selection/playback.

Final desktop position and row dimensions match the reference exactly. The
0.13-pixel mobile position difference comes from the exact 3840/1608 media
aspect ratio rather than the handoff's rounded 2.39. Waveform display-bin counts
remain stable across selected, unselected, and missing-subtitle states: 179 at
1440 px, 35 at 393 px, and 32 at 375 px.

GAWL display-bin aggregation can produce small shape differences from the
prototype's random waveform paths. Compare layout, density, selection, and
measured timing without asserting that random and measured paths are
pixel-identical. The HUD reflects real source/playback state and seconds-based
timecodes. These intentional data representations do not imply a claim of
pixel identity for the whole page.

The [read-only live result before the badge refinement](../../../.artifacts/timeline-visual-correction-20261005/live/run-2026-10-04T22-43-39-761Z/results.json)
passed at two viewports with four PNG captures on `9aae8518...`. Both PGS/DVD codec labels have
`clientWidth = scrollWidth = 22` px. Actual intervals/gaps are preserved, with
no left markers, overlapping ruler labels, or horizontal overflow. Observed
application requests are GET/HEAD only; task, user, playback-session, queue,
and source-file summaries remain unchanged. The real short-source review does
not replace the complete handoff fixture comparison and is not evidence of a
repeated live run on the final `2bcfffd9...` image. The later badge-only change
has its own final endpoint checks and matching source/image receipt.

The frozen [visual review](../../../.artifacts/timeline-visual-review-20261005/REVIEW.md)
and [acceptance record](../ACCEPTANCE.md) link the final paired images and
measurements. The six mobile implementation timeline images are cropped
directly from their original bottom-of-scroll screenshots using
[saved JSON coordinates](../../../.artifacts/timeline-visual-review-20261005/after/mobile-crops.json)
to include the badge above the timeline box. No pixels are redrawn or
substituted. Original element clips are retained as backups, not additional
scenarios; the total remains 15 captured states and 45 primary screenshots.
The harness source matches locally and remotely. The 67 focused cases overlap
earlier suites; do not add them to the historical 136 as new distinct tests or
use them as a substitute for the separate final visual evidence.

The fixture has no transcoder. A constrained-quality negotiation returns `NoCompatibleStream`, allowing the tests to verify error recovery and session cleanup. Passing these tests does not establish that a deployed Goby server's HLS encoder, GPU, storage, network, external applications, or real media library works. The separate backend profile-contract check exercises the real Go playback planner without claiming an FFmpeg runtime test.

## Real Docker acceptance

The separate real-backend workspace is
`/opt/goby-test/player-live-20261004-165c` on `test-env`, owned by the sentinel
`goby-player-live-20261004-165c`. Its `source` directory is the matching source
mirror. The Docker project contains a real Goby server, private PostgreSQL 17,
and the independently built player nginx service at
`http://127.0.0.1:38974`. The backend and player use read-only root filesystems.

The [setup helper](../../../scripts/test-env/player-live-setup.py) provisions
the explicitly owned environment, generated media, synthetic account, private
configuration, and scan. It expects the accepted base image and application
build inputs already prepared; it is an acceptance helper, not a general
installer. Do not point it at an existing user deployment. Its private files
are not committed or copied into public reports.

With that workspace and the corresponding images already prepared, run the
real playback harness from local PowerShell:

```powershell
ssh test-env 'cd /opt/goby-test/player-live-20261004-165c/source/web/player; GOBY_LIVE_ROOT=/opt/goby-test/player-live-20261004-165c node scripts/live-acceptance.mjs'
```

The harness reads `browser.private.json` on the remote host, exercises the
real player origin, and writes sanitized evidence to `artifacts/live-browser`.
It checks original MP4 decoding, actual software HLS conversion, rendered
SRT-to-WebVTT subtitles, HLS seek, audio selection, persisted progress/resume,
episode switching, and FFmpeg retirement. The movie is 180 seconds so Goby's
real minimum-duration resume policy applies. Test login secrets do not appear
in the retained result JSON.

The selected capability increment has a separate
[fixture preparation helper](../../../scripts/test-env/player-live-capabilities-fixture.py)
and [browser harness](../scripts/live-capabilities.mjs). After that generated
fixture and the new backend/player images are prepared in the owned workspace:

```powershell
ssh test-env 'cd /opt/goby-test/player-live-20261004-165c/source/web/player; GOBY_LIVE_ROOT=/opt/goby-test/player-live-20261004-165c node scripts/live-capabilities.mjs'
```

This harness checks real mixed search, filters, theme media, and administrator
credits through the same deployed origin. The October 4 run passed all eight
phases; its captured results are in
[live-capabilities/results.json](../../../.artifacts/player-live-20261004/live-capabilities/results.json).
The separate original-playback run passed six phases. Completion, retained
failures, reruns, and exclusions are stated in [ACCEPTANCE.md](../ACCEPTANCE.md).

The later [background generation harness](../scripts/live-background-previews.mjs)
uses the same explicitly owned remote environment with the schema-57 images,
approved writable media bind, and synthetic source files. It exercises actual
automatic and explicit generation, settings-only edits, request replay, file
retention, player decoding, restart, source changes, failed/canceled Force,
and successful explicit replacement. Its 12-phase result is retained at
[player-background-live/results.json](../../../.artifacts/player-background-live/results.json).
This harness deliberately mutates its synthetic library and controlled media
permissions; it is not a probe for an existing user deployment.

The [final evidence directory](../../../.artifacts/player-background-live/final-evidence)
also retains the successful player image build, administrator checks, actual
FFmpeg and PostgreSQL recovery checks, final task/API/migration regressions,
and post-recovery read-only verification. Failed earlier build and disk-capacity
attempts remain in that directory and are distinguished from the successful
reruns in the acceptance record. All checks for this increment ran on `test-env`.

Original Emby filter comparison is a third, distinct flow using
[player-reference-filters.py](../../../scripts/test-env/player-reference-filters.py)
and an isolated official Emby 4.9.5.0 container. It does not replace Goby's
runtime acceptance or prove automatic credits recognition.

The later waveform/hours increment passed 63 Playwright cases: 19 waveform
and four viewing-statistics cases plus the existing 40-case regression. Tests
cover binary response validation, peak/RMS and validity gaps, source identity,
account/route cancellation, estimated-hours semantics, and honest unavailable
states. The real waveform Docker journey separately passed eight phases,
including actual original-track generation, UI rendering, reuse/restart,
failed Force, stale-axis rejection, and explicit replacement. Its retained
[results](../../../.artifacts/player-waveforms/results.json) distinguish three
optional BIF ThumbnailSet 404 responses after deliberate source changes from
waveform/page failures, of which there are none.

The [waveform evidence](../../../.artifacts/player-waveforms/final-evidence)
contains successful player/admin builds, 17 administrator checks, focused
HTTP/task/migration tests, actual FFmpeg codec/timing cases, and PostgreSQL 17
schema-58 recovery. The authorized CT104 DV media cases have a separate
[closure record](../../../.artifacts/player-dv-20261004/evidence/closure.json).
Neither fixture tests nor those bounded media cases imply automatic credits
detection, all-codec coverage, or a general production GPU rollout.

The renewed handoff-restoration increment passed 101 distinct automated cases
on `test-env`: the 95-case regression, five hover cases, and one additional
cross-platform effective-player fallback case. A final 18-case external/detail/
background run is an overlapping focused recheck, not 18 more distinct cases.
Logs are under [player-handoff-20261005](../../../.artifacts/player-handoff-20261005)
and [handoff-hover](../../../.artifacts/handoff-hover). They cover existing-data
presentation, seek thumbnails, subtitle styles, filter choices, transitions,
source sound policy, protocol URL serialization, and fallback selection.
Actual external-app installation/launch is outside these tests. Completed visual
and read-only real-service review is recorded separately in
[ACCEPTANCE.md](../ACCEPTANCE.md).

The later 16-case catalog/hover/background check and final five-hover-case rerun
overlap the 101 distinct cases. Visual evidence has 60 initial comparison images,
24 intermediate review images, two diagnostic hover-anchor images, and six final
focused images; the stages must not be combined into a claim that every capture
shows final styling. Six separate real-service captures preserve unchanged
watching state and task history, with the unavailable ThumbnailSet fallback and
two ready waveform tracks recorded explicitly.
