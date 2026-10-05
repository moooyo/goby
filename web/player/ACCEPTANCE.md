# Goby player implementation and acceptance

Initial record: October 4, 2026. Automatic-credits follow-up: October 5, 2026.
External-bitmap playback source follow-up: October 6, 2026; feature-source
runtime acceptance and later main-integration evidence are recorded separately
in the final section below.
Design baseline:
`D:/Code/design_handoff_goby_player/Goby-Player-A-v3.dc.html` and its v3 handoff.
Initial backend source baseline: `b30e78c8924380cd404588890f20dd0949394dde`.
The follow-up adds working-tree changes for search/theme integration, video
filters, schema-56 credits markers, and subsequent schema-57 background-preview
work; the initial baseline alone does not identify those later changes.

The player is implemented as an independent React + TypeScript + Vite package.
It builds to `web/player/dist`, uses the real Goby `/emby` API, and has a separate
nginx Dockerfile and optional Compose service. No player files are embedded in
Go. The fictional design catalogue and sample video exist only in the isolated
test server; neither the prototype runtime nor its sample media enters `dist`.

The selected follow-up completed real Goby plus independent-player Docker
acceptance first, then the selected capability increment. All six playback
phases and all eight capability phases passed. The same six playback phases
also passed again on the final capability images. Focused browser and Go
regressions also passed. The user then selected persistent generated background
clips; their 12-phase real Docker acceptance and focused regressions also passed
on `test-env`. The later approved increment implements estimated content hours,
per-track waveforms, and bounded Dolby Vision background generation. Its checks
and final live-waveform status are recorded separately below. Exact elapsed-time
analytics remains optional. The later movie/TV automatic credits increment
passed its seven-phase real Docker journey and focused regressions. Its
algorithm/corpus limits and distinct receipts are recorded below.

The October 5 bitmap subtitle timeline follow-up adds default-off generation
for embedded PGS/DVD tracks and hides complete subtitle rows without valid
nonempty intervals. Its schema-60 focused results and seven-phase real Docker
pass have their own section below. Those functional and lifecycle receipts do
not establish complete visual agreement with the handoff. A subsequent user
review exposed timeline presentation differences and an overlapping mobile
ruler. The same-data visual correction below has passed its build and 67-case
focused regression, a final five-case overlapping recheck, and a read-only
live-service review before the final mobile-badge refinement. The final badge
change passed two overlapping endpoint cases. The final same-data comparison
passed all 15 captured states, with 45 primary screenshots and no failures;
its bounded visual conclusion is recorded below.

The subsequent October 5 review of home, catalogues, favorites/search, detail,
settings, playback, and shared navigation/backgrounds is consolidated in
[DESIGN-REVIEW.md](DESIGN-REVIEW.md). It distinguishes original-HTML same-data
visual evidence from functional regression and preserves intermediate findings.
All module reviews and 142 distinct regression cases passed. The last shared
rail-color change passed three overlapping cases; the final production/Docker
builds and matching-image deployment passed remotely. The final-image read-only
real-service walkthrough passed 14 page/viewport groups with 30 screenshots and
unchanged viewing/session/task/queue summaries. These results retain separate
scopes from the earlier timeline and real-media playback receipts.

## Initial design and fixture verification

- `npm run build`: passed TypeScript and Vite production compilation.
- Chromium browser acceptance: 18 distinct scenarios cover authentication,
  restored sessions, 401/403 and network failures, empty libraries, library and
  genre selection, ordering, unfinished filters, search, favorites, watched
  state, season switching, media information, persisted preferences, mobile
  layouts, and real media-element playback.
- Playback uses an actual 30-second VP8/Opus file with HTTP byte ranges. Checks
  cover resume, decoded data, seeking, playback speed, subtitles, audio changes,
  incompatible-quality recovery, four-second progress, ordered session cleanup,
  control auto-hide, episode selection, natural completion, and next-episode
  cancellation. Intro interval validation uses actual marker types.
- The catalogue/flow tests attach request audits. All 13 recorded API audits
  contain zero unknown fixture routes. The test server writes only to its
  in-memory catalogue.
- The final full run completed 17 of 18 scenarios successfully. Its mobile
  scenario loaded an obsolete lazy-chunk URL while an overlapping production
  build replaced `dist`; that scenario passed when rerun against the final build. Raw
  reports retain the build-overlap failure rather than removing its evidence.
  The [final scenario summary](../../.artifacts/player-acceptance/acceptance-summary.md)
  records 18/18 scenarios passed and retains the separate full-run/rerun reports.
- A separate layout review checked home, movies, details, and settings at
  375x667, 844x390, 720x600, 768x1024, and 1024x768: 20 page/viewport combinations
  with no horizontal document overflow. Browser acceptance also covers
  1440x900, 393x852, and 390x844.
- A real touch gesture at 375x667 scrolls overflowing detail content without
  changing the page. At the end of that inner scroll, the final action is above
  the mobile navigation bar.
- `go test ./internal/playback -run '^(TestPlanConversion|TestEvaluateExternal|TestEvaluateIndexedExternal)' -count=1`:
  passed the relevant existing backend planner/subtitle tests locally.
- `git diff --check`: passed for the repository documentation edits.

The entry JavaScript is approximately 250 kB uncompressed and 79 kB gzip.
The lazily loaded player chunk includes hls.js and is approximately 605 kB
uncompressed and 189 kB gzip. Vite emits its standard 500 kB chunk-size warning
for that player chunk; compilation succeeds. The four design font families are
self-hosted, with Chinese glyph subsets loaded as needed by the browser.

## Real Docker playback acceptance: completed

The follow-up ran on `ssh test-env` in the owned
`/opt/goby-test/player-live-20261004-165c` workspace. A real Goby backend with
its embedded administrator dashboard, PostgreSQL 17, and the independently built
nginx player ran in the `goby-player-165c` Docker project. Browser requests used
the player origin and its same-origin API/media proxy. The backend and player
containers both used read-only root filesystems.

The isolated library contains generated H.264/AAC media, two actual audio
tracks, external SRT subtitles, one 180-second movie, and a two-episode series.
A synthetic administrator and private test database are used; the acceptance
does not use the user's media or production account.

| Phase | Observed result |
| --- | --- |
| Login and catalog | A real account signs in through the player and sees scanned Movie/Series/Episode data. |
| Original playback | Chromium decodes the actual 1920x1080, 180-second original MP4. |
| Conversion and subtitles | Real software FFmpeg HLS conversion plays at the requested 720p maximum, and SRT-to-WebVTT subtitle text renders. |
| Seek, audio, and progress | HLS seeks to 30 seconds, the second subtitle interval renders, audio stream index 2 is selected, and server progress is persisted. |
| Resume | Reloading the player resumes from the backend's saved source position and the item appears in Resume. |
| Episode navigation and retirement | The episode drawer switches episodes; terminal reports are sent and no FFmpeg process remains after stop. |

All six phases passed, with no recorded HTTP or page errors. The retained
[step-1 summary](../../.artifacts/player-live-20261004/step1-summary.json) binds
container image IDs, read-only state, and the relevant source hashes. The
[browser record](../../.artifacts/player-live-20261004/step1-browser.json) retains
negotiation/report/media evidence, and the
[HLS screenshot](../../.artifacts/player-live-20261004/hls-subtitles.png) captures
actual subtitle rendering. The remote originals are under the owned root's
`artifacts` directory. Credentials remain in private remote files.

After the capability work and final image rebuild, the six playback phases
were rerun against `goby-player-backend:step2` and `goby-player-ui:step2`.
All six passed again, with no HTTP/page failures and no remaining FFmpeg
process after stop. The
[final-image playback regression](../../.artifacts/player-live-20261004/step1-regression-browser.json)
is a separate receipt from the original step-1 run; it confirms that the new
capabilities preserved original playback, HLS/subtitles, seek/audio/progress,
resume, episode navigation, and session cleanup.

This run found and fixed three implementation issues:

- Read-only nginx startup also needs FastCGI, uWSGI, and SCGI temporary paths
  under writable `/tmp`, in addition to client/proxy temporary paths.
- A direct MP4 must not be sent to hls.js merely because its source advertises
  HLS conversion capability. The selected URL/method now decides playback.
- Actual backend track labels need localized language/codec/channel rendering
  when the server's display label is an untranslated technical string.

## Selected capability increment: completed

The source now implements the following selected work:

| Capability | Implemented behavior |
| --- | --- |
| Mixed search | Search/Hints media/person/genre results retain typed identity, server totals, and offsets across mixed pages. Person and genre selections open associated media; year remains explicit. |
| Existing theme media | Desktop stage motion uses compatible original ThemeMedia or LocalTrailers silently, with no PlaybackInfo or history events. Navigation, hidden pages, reduced motion, unsupported sources, and failures retain/release to artwork. |
| 4K/HDR filters | Is4K and ExtendedVideoTypes predicates precede pagination/counting. Is4K uses the original-Emby-observed first non-attached video width of at least 3800. Series-wall descendant filtering uses GobyAggregateVideoFilters. General width/height bound query parameters are not implemented. |
| Manual credits | An administrator can read, save, and clear source-bound Movie/Episode credits with revision conflict protection. Public chapters carry CreditsStart. The player uses a ten-playing-second countdown with pause/buffer/cancel behavior and reports the actual stop position. |

Focused verification on `test-env` has passed 30 browser scenarios across the
current fixture suite: 12 general acceptance cases, 12 credits/existing-player
cases, and six catalog/theme cases. One general acceptance assertion still
expected the former disabled 4K control; it was updated for the implemented
server query and passed in a separate rerun. Retained reports preserve that
earlier failure. Selected Go credits, intro, search-hint, catalog/restore, and
video-filter regressions also passed. The filter checks include standard
multi-video-stream semantics and the stricter Goby descendant aggregation.
The retained [filter test log](../../.artifacts/player-live-20261004/video-filter-tests.log)
records that focused run. These groups are not added to the historical initial
18-scenario total.

The [live capability harness](scripts/live-capabilities.mjs) then ran against
the rebuilt backend and player images on the same isolated Docker origin. All
eight phases passed, with `failures=[]` in the final
[browser results](../../.artifacts/player-live-20261004/live-capabilities/results.json).
The [step-2 summary](../../.artifacts/player-live-20261004/step2-summary.json)
binds this increment's runtime evidence separately from the earlier
[playback harness](scripts/live-acceptance.mjs) receipt.

That final summary records live schema version 56, the backend binary hash,
and both immutable Docker image IDs. All 49 compared production source files
match the remote build inputs. Player, administrator, backend, and both Docker
builds passed. The backend and player run with read-only root filesystems;
the player and database health checks are healthy, and the backend is running.
The receipt includes the final-image six-phase playback regression and confirms
encoder retirement. These are acceptance image identities, not a published
production release.

| Phase | Observed result |
| --- | --- |
| Consumer authentication | The real account signs in through the independent player origin. |
| Mixed search navigation | Search/Hints returns media, person, and genre results; person, genre, and explicit year navigation reach the corresponding catalog queries. |
| Indexed 4K/HDR filters | The generated real library yields one 4K item and two HDR items, with matching displayed server totals. |
| Silent theme media | The actual theme video decodes at 1280x720 for three seconds with muted audio. It creates no playback session, viewing-history report, or encoder. Reduced motion and mobile layout release its source. |
| Administrator save and publication | The editor saves a Manual start at five seconds, and the source-bound value appears in item/source chapters and PlaybackInfo as CreditsStart. |
| Pause and cancellation | A paused cue remains at ten seconds; canceling retains the current episode for at least twelve additional seconds. |
| Automatic next episode | Ten playing seconds after the marker, the next episode starts. The old 60-second episode reports its actual stop at 15.241081 seconds, without inventing an end-of-file position. |
| Administrator clear | Clearing returns Override and Effective to null for the unmarked source and removes the public CreditsStart marker. |

The initial capability attempt exposed duplicate IDs on the administrator
credits dialog title and its inner typography. The accessible title association
was corrected, the backend/dashboard image rebuilt, and all eight phases rerun
successfully. Earlier failure artifacts are retained beside the final result.
The separate theme visual check waited until the fade-in completed. The
[visible-stage screenshot](../../.artifacts/player-live-20261004/live-capabilities/theme-video.png)
shows actual colored video, and the
[visual measurements](../../.artifacts/player-live-20261004/live-capabilities/theme-visual.json)
record opacity 1, source time 0.837206 seconds, and a decoded 1280x720 source
covering the 1440x900 stage. It remained muted with no PlaybackInfo or Playing
requests. This replaces the initial screenshot captured before the fade became
visible as the evidence for the moving stage.

The original Emby 4.9.5.0 filter observation was also run in a separate isolated
reference container with 17 Movies and four Episodes. It confirms the
width-based 4K boundary and keeps standard
container semantics separate from Goby's descendant aggregation. See
[EMBY-COMPARISON.md](EMBY-COMPARISON.md) and
[video-filter semantics](../../docs/api/video-catalog-filters.md).

## Persistent background-preview increment: completed

The subsequent source implementation generates background MP4s beside their
Movie/Episode source files. This is a separate increment from the accepted
schema-56 images and the original ThemeMedia/trailer integration above.

| Area | Implemented behavior |
| --- | --- |
| Persistent storage | Each source filename has a deterministic `backdrops/goby/<sha256>/` directory. A manifest selects an immutable generated MP4. Ordinary scans, source/profile/start changes, disabling generation, and cache maintenance do not remove or replace a completed clip. Only explicit Force regeneration can replace it; failure/cancellation preserves the old clip. |
| Work admission | A per-library automatic option defaults to false. Explicit administrator starts use durable selection/request receipts, and all generation uses the existing task system and shared serial analysis slot. Playback/metadata reads do not enqueue work. |
| Selection and output | Manual start takes priority over validated intro-end plus three seconds, then a deterministic early-source position. Known credits bound the interval. Up to three automatic candidates can avoid predominantly black/static pictures; a manual position is never silently moved. The default output is silent H.264 MP4, 25 seconds, within 1280x720, at a requested 1.5 Mbps. |
| Controls | Independent profile configuration, per-item start time, generation/Force actions, and task progress/cancellation are available in the administrator UI. The player stores per-user source priority and a still-only preference. |
| Deployment | The optional `compose.background-previews.yaml` changes only `/media` to writable. Default media mounts and the container root filesystem retain their existing read-only settings. Sidecar ownership/permissions must allow the backend UID to write. |

All verification for this increment ran on `ssh test-env`. The final browser
regression passed 40 cases: ten new background-preview cases, six catalog/theme
cases, and 24 existing cases. Nine administrator Node checks passed. The player,
administrator, backend, and Docker images built successfully. The earlier
`player-step3-build.log` retains a TypeScript nullable-element failure; the
successful final player build is in
[player-step3-image.log](../../.artifacts/player-background-live/final-evidence/player-step3-image.log),
which records the completed Vite build and image export. These counts are not
added to the earlier 30-scenario or schema-56 Docker receipts.

The real deployed generation journey passed all 12 phases with `failures=[]`
in [results.json](../../.artifacts/player-background-live/results.json):

| Phase | Observed result |
| --- | --- |
| Default and read behavior | Automatic generation is off by default; an unavailable descriptor triggers no task, and the task/event controls are visible. |
| Automatic opt-in | Enabling the library option creates a silent H.264 25-second 1280x720 clip; disable/re-enable and scan preserve the completed publication. |
| Manual start | Saving a 12.5-second start changes settings without queuing generation. |
| Explicit initial generation | The administrator creates the source-adjacent 25-second H.264 clip with zero audio streams; a range request returns 206. |
| Replay and reuse | Replaying the request and ordinary generation preserve the existing bytes and timestamps. |
| Profile and option changes | Changing the configured duration to five seconds and disabling automatic generation retains the existing 25-second clip. |
| Player source priority | Generated-first preference plays a visible, muted 1280x720 background at opacity 1 without changing playback history. |
| Container restart | Restart reuses the same generation and SHA-256. |
| Source changes | The API reports SourceChanged while the previous clip remains available. |
| Failed regeneration | A write-permission failure reports media_directory_not_writable and retains the old publication. |
| Cancellation | Canceling an observed running encoder leaves the old publication intact. |
| Successful Force | Explicit regeneration publishes a new five-second clip, retires the validated old generation, and returns 404 for the stale version tag. |

Focused backend tests include source/ownership checks, revision and request
receipts, revoked manual authority, recovery, and publication rollback through
directory synchronization and read-back/close failures. The complete
`internal/tasks` package passed in 78.341 seconds. Targeted existing library and
server regressions passed in 5.587 and 4.512 seconds, covering LibraryOptions,
LibraryPreview, PreviewGeneration, HTTPAdminTask, HTTPScheduledTask, and
ManagementCompatibility. Their final logs are
[task regression](../../.artifacts/player-background-live/final-evidence/background-task-regression-final.log)
and [existing API regression](../../.artifacts/player-background-live/final-evidence/background-existing-api-regression-final.log).

The first supplemental regression attempt exhausted disk space in the temporary
PostgreSQL environment. Only owned duplicate build artifacts were removed,
owned PostgreSQL WAL was reduced, and only the test database was recreated
before rerunning successfully. The original failed
[task log](../../.artifacts/player-background-live/final-evidence/background-task-regression.log)
and [API log](../../.artifacts/player-background-live/final-evidence/background-existing-api-regression.log)
are retained. The application database `player` was not recreated.

Actual FFmpeg 9.0.1 generation passed SDR, HDR10, HLG, and configured 1080p
checks; see the [media log](../../.artifacts/player-background-live/final-evidence/background-media-actual.log).
Actual PostgreSQL 17 backup/restore passed 22 semantic cases plus the complete
background-state recovery flow; see the
[backup log](../../.artifacts/player-background-live/final-evidence/background-backup-actual.log).
Three top-level migration integration tests also passed in 1.308 seconds:
concurrent/idempotent migration, preservation of the schema-51 preview policy,
and preservation of historical intro publication authority, including normal
and recovery paths. Five existing test files had version assertions updated
for schema 57; historical snapshots remain intact and the new event has an
independent assertion. See the
[migration regression log](../../.artifacts/player-background-live/final-evidence/background-migration-regression-final.log).
These are focused migration/media/recovery claims, not a pass for every database
package test or all codecs. The assertion-only updates did not change production
code or require another image build.

The [post-recovery read-only check](../../.artifacts/player-background-live/final-evidence/background-post-recovery.json)
confirmed public API 200, schema 57, generated-stream range 206, a matching
current MP4 SHA-256, and no residual FFmpeg/ffprobe processes. The source
comparison recorded 164 normalized file-hash matches against the remote source
used for testing, and remote `gofmt -l` returned no paths. The
[source manifest](../../.artifacts/player-background-live/final-evidence/step3-remote-source-manifest.json)
and logs retain that verification scope.

Visual evidence includes the
[generated background](../../.artifacts/player-background-live/generated-background.png),
[source priority setting](../../.artifacts/player-background-live/generated-first-setting.png),
[retained clip after profile change](../../.artifacts/player-background-live/profile-change-retains-clip.png),
and [completed explicit regeneration](../../.artifacts/player-background-live/explicit-regeneration-ready.png).

See [background previews](../../docs/api/background-previews.md) for the current
contract, persistent-media rules, source limits, and task lifecycle. In
particular, that schema-57 receipt covers the SDR and HDR10/HLG paths. The later
bounded Dolby Vision implementation has its own evidence below. The original
receipt does not claim all-codec, GPU, or production-release acceptance.

## Waveforms, estimated hours, and Dolby Vision increment: completed

The approved schema-58 source adds permanent per-audio-track peak/RMS sidecars,
the handoff-style estimated-hours statistic, and a bounded DV background path.
Waveform data uses a separate `backdrops/goby-waveforms` namespace, with no
LRU/automatic replacement. An obsolete source timeline is hidden from consumer
responses while its file remains; explicit Force can replace it. The hours
estimate sums watched runtimes once plus bounded unfinished progress and never
claims elapsed viewing time. Intro matching remains the only automatic boundary
detector; manual/source CreditsStart and the existing cue remain unchanged.

Player/administrator builds and final images passed and were deployed on
`test-env` with schema 58. Sixty-three Playwright cases passed: 19 waveform and
four estimated-hours cases in
[the new group](../../.artifacts/player-waveforms/final-evidence/browser-results-4203.json),
plus [40 prior cases](../../.artifacts/player-waveforms/final-evidence/browser-results-4204.json).
Seventeen administrator Node checks passed. The initial disconnected player
image attempt was canceled during dependency installation; it is retained in
`player-step4-image.log`. The successful build is
[player-step4-image-final.log](../../.artifacts/player-waveforms/final-evidence/player-step4-image-final.log).

Actual FFmpeg waveform checks passed for multitrack delayed audio, opposing
channel polarity, AAC, AC3, EAC3, FLAC, Opus, PCM, and cancellation. Library/task
checks passed; the final six waveform and two viewing-statistics HTTP tests
passed in 8.848 seconds. Targeted task/migration regressions passed in
32.083/3.453 seconds. The
[final evidence directory](../../.artifacts/player-waveforms/final-evidence)
retains the logs and desktop/mobile screenshots. PostgreSQL 17 schema-58 catalog
export covers 79 tables; 21 waveform and 22 background semantic cases plus two
actual restore flows passed, without putting source-side files in the archive
or deleting them during restore.

The final real waveform Docker journey passed all eight phases with exit 0 and
`failures=[]` in [results.json](../../.artifacts/player-waveforms/results.json).
The separate first-generation record retains evidence that the artifact was
initially absent and generated by an observed FFmpeg `f32le` process; the final
rerun correctly reused that persistent result. It contains original stream
indexes 1 and 2, a 63,528-byte bundle, and four levels per track.

| Phase | Observed result |
| --- | --- |
| Automatic default and read behavior | Automatic waveform generation defaults off and descriptor reads create no task. |
| Original-track generation | Both original audio indexes have current measured data at 512/1024/2048/4096 buckets. |
| Consumer display and estimate | The player renders real peak/RMS lanes and correctly displays 303866520 content ticks as zero rounded hours, without playback/history or generation writes. |
| Ordinary batch and option changes | Reuse and library toggles preserve waveform hash and modification time. |
| Restart | The container reopens the same persistent publication after restart. |
| Failed Force | A media-write permission failure preserves the old generation and readable levels. |
| Stale source | Changed source identity yields Available=false/Stale=true and old-level 404 while ordinary requests retain the saved file. |
| Explicit replacement | After restoring source mtime, Force publishes a current source axis and invalidates old version URLs. |

Cleanup restored automatic-generation policy, metadata settings, source mtime,
and permissions. Filesystem change-time cannot be restored, so the final Force
uses the resulting current identity. Existing background MP4 hash/mtime and
user playback state stayed unchanged, with no residual media processes.
Three source-change-related `ThumbnailSet` 404 responses are listed separately
under `optionalMediaResponses`; waveforms and page execution had no failures.
BIF behavior was not changed.

Earlier harness attempts clicked before the existing 1050 ms StageDeck lock
expired. The harness now waits and completed the stable full run; both earlier
timing-failure records and first-generation evidence remain retained. These
attempts do not establish a new production defect. Root review also confirmed
the [real waveform view](../../.artifacts/player-waveforms/consumer-real-waveforms.png),
[hours display](../../.artifacts/player-waveforms/estimated-viewing-hours.png),
and the retained desktop/mobile fixture screenshots.

Dolby Vision generation was separately exercised on explicitly authorized CT104.
Eight profile/fixture scenarios passed in 11 executions, including three
output-retaining reruns, with no failures/skips. They cover Profile 8.1 and
complete Profile 7 MEL, neutral RGB, RPU-negative controls, cancellation, and
a 25-second clip from an original 28-second source. Final P8 bad-CRC and
valid-CRC invalid-mapping controls produced zero output. A residual P7 BL+RPU
input without EL was rejected at admission with zero output; that test does
not establish complete EL/FEL runtime reconstruction.
The [closure record](../../.artifacts/player-dv-20261004/evidence/closure.json)
records tool/source identities, saved outputs, and no remaining worker processes
or render-device handles. Processing uses strict Vulkan/libplacebo and CPU H.264
encoding. In this October 4 increment, Profile 5 remained disabled, Profile
8.2/8.4 were not admitted, and FEL residual reconstruction was not implemented.
This media-path evidence is not
universal GPU/profile acceptance or a production deployment claim.

The [final read-only check](../../.artifacts/player-waveforms/final-evidence/waveforms-final-read.json)
confirmed schema 58, two current non-stale waveform tracks, a GAWL range response
with status 206, and the expected 303866520-tick/zero-hour estimate. No media
processes remained, and both backend/player image roots were read-only. The
112 Go files formatted on the remote worker were returned with matching
contents; production code did not change after the accepted image builds.

## October 5 Dolby Vision profile extension: completed native AMD media acceptance

The user selected Profile 5, then Profile 8.4 and Profile 8.2; only Profile 7 FEL
reconstruction is deferred. The implemented admission policy enables these
three profiles after the existing strict-toolchain/admitted-Vulkan-device
dependency check. Source-bound scan evidence and per-frame RPU validation are
still required. Software HEVC decoding preserves native base pixels and RPU
data; strict color processing precedes geometry and frame-rate conversion,
with CPU H.264 encoding producing the existing silent BT.709 SDR sidecar.

The final focused suite on `test-env` passed 41 top-level tests containing 153
subtests, with no failures/skips, and the Go backend build passed. The final
native AMD matrix on authorized CT104 passed all 21 required cases with no
skips: 12 analytic color/default-window/bad-frame/cancellation cases across the
three profiles; two official 25-second windows beginning at 60 seconds; one
same-scene Profile 5/8.4 consistency comparison; and six legacy Profile 8.1,
Profile 7 MEL, window, bad-CRC, bad-mapping, and residual-rejection regressions.
The full history retains 33 executions, comprising 29 passes and four failures
before the fixes. These counts do not erase the unsuccessful attempts or add
top-level and child tests together.

Actual media acceptance required three corrections. DV input decoding now
explicitly enables `-err_detect crccheck+explode` to reject RPU CRC corruption.
Syntax scanning narrowly accepts the exact `hevc_mp4toannexb` empty-extradata
warning for legitimate in-band parameter sets after complete access-unit/CRC
validation, while other warnings remain failures. Successful scanning captures
optional `InBandParameterSets` evidence; the affected source decodes from the
beginning and trims to the requested interval before strict processing, while
ordinary fast seeking is unchanged. The pinned tools/private renderer patch
were not rebuilt.

The corpus uses official CC BY 4.0 Dolby/Netflix Sol Levante samples for Profile
5 and Profile 8.4 plus original analytic sequences with independent scalar
conversion oracles. Their final BT.2390 SDR display mapping is shared with the
product; they are not independent end-to-end Dolby reference renderers. Profile
8.2 uses original SDR base pixels and non-identity RPU metadata, without a
public commercial-source reference video or Dolby certification. The official
Profile 5 output is 4,666,702 bytes for 25 seconds,
with a 28.095-second run; Profile 8.4 is 4,659,991 bytes with a 28.013-second run.
Elapsed times include setup and output validation, not just encoding.
The official same-scene comparison measured RGB mean absolute error 4.7441/255
and spatial luma correlation 0.996525; it is a consistency check between two
published versions, not an independent SDR master.

The [final case summary](../../.artifacts/player-dv-profiles-20261005/final/evidence/case-summary.json)
and [closure record](../../.artifacts/player-dv-profiles-20261005/final/closure.json)
retain identities, final passes, artifact integrity, and no remaining worker
processes, render-device handles, or temporary entries. The detailed
[review](../../.artifacts/player-dv-profiles-20261005/REVIEW.md) and
[implementation record](../../docs/development/dolby-vision-background-research.md)
retain the fixes and coverage limits. This is native AMD media acceptance, not
a new Docker deployment or universal device/profile acceptance. Default-off
generation, permanent source-adjacent storage, and explicit Force replacement
retain their existing contracts. No original-DV browser-playback or DV-output
capability is added.

Previously rejected `failed` entries are not automatically requeued by this
profile upgrade. If saved RPU evidence was already verified and only the old
profile gate rejected generation, the existing administrator action for
generating missing clips or a new `Force: false` request can retry it. An old RPU
`decoder_error` first requires a completed **Libraries > Refresh media details**
(`ForceProbe: true`) before generation is retried. Existing artifacts are
reused. No new UI switch, cache clearing, or sidecar migration is required;
Force is only for an intentional replacement.

## Automatic credits increment: completed detection acceptance

The user selected movie and TV credits detection using the previously chosen
Intro Skipper project, pinned at
`6e0cb179007ac4c16cd9f358e9a617e791e9bf06`. This is reuse of that open-source
project, not an assertion that Emby's closed-source detector uses the same
algorithm. Port/license work is recorded separately with the implementation.

The implemented contract includes `EnableCreditsDetection=false` by default,
`media.credits_analysis`, and `CreditsAnalysisRequested`. Movie analysis can use
one source; TV analysis can also use eligible support episodes. Audio matching
and the full CreditsPass chapter/black-frame/entropy/combination/time-adjustment
path retain multiple source-bound intervals. Manual/import and reserved chapter
points keep precedence. Changed target/support/profile/library policy withdraws
invalid automatic publication, and `no_result` does not synthesize a tail.

Standard chapters retain `CreditsStart`; full interval bounds are published
through `GobyCreditsIntervals` on item and source DTOs. An explicit empty array
is authoritative, and legacy single-start fallback is for responses without
that extension. The official Emby marker enum does not document CreditsEnd, so
this work does not add it as an alleged standard marker.

The backend and player step-5 images built and deployed successfully. Pure
algorithm and actual-media credits checks passed. Five HTTP groups cover
multiple intervals, manual precedence, and four invalidation branches. Fourteen
library groups plus two child branches passed, including actual
AnalyzeCredits-to-Publish execution after correcting a nil Reasons integration
mismatch. The browser suite passed 15 credits cases and 57 other cases, for 72
total; 35 administrator checks passed. Backup/recovery checks passed 29 top-level
tests containing 139 child scenarios, recorded in
[credits-backup-actual.log](../../.artifacts/player-live-20261004/credits-backup-actual.log).
The two counts describe hierarchy and are not added together. Four parent/nine
child actual background/waveform regressions also passed on the new base.

The real Docker journey passed all seven phases with no recorded failures in
[results.json](../../.artifacts/player-credits-detection/results.json):

| Phase | Observed result |
| --- | --- |
| Default policy and read behavior | Credits detection defaults off; administrator, item, and playback reads create no analysis runs. |
| Detection and publication | Opt-in triggers analysis. Chapter and black-frame Movie cases produce 60–90-second intervals; independent Episode tail audio produces source-bound Chromaprint intervals. A negative case produces no_result and no intervals. |
| Player cue | The real player displays its cue within the detected episode interval and hides it before the interval and after seeking out. Multiple-interval gaps are covered by the separate browser suite. |
| Manual precedence | A 70-second manual override takes priority, retains detection evidence, and clearing it restores the valid automatic interval. |
| Library disable | Disabling retracts automatic markers while retaining the detection revision/evidence. |
| Target/support invalidation | Changing the target or independently matched support hides old intervals before rescan, with source_changed/support_changed reasons and retained evidence. |
| Reanalysis | Restored mtime and explicit reanalysis produce current bound results. Filesystem ctime cannot be restored and is handled by fresh source identity. |

The audio fixtures enter their shared music at 540 and 552 seconds. The pinned
matcher returned 536.5038851–600 and 548.5038851–612 seconds: approximately
3.496 seconds early. Thresholds and the upstream timing behavior were not tuned
to force an exact fixture boundary. This accepts the bounded integration and
observed corpus behavior, not exact-second or universal credits recognition.
Detected intervals also do not guarantee playing every later coda: autoplay
can advance while still inside an earlier credits interval.

Evidence screenshots include [visual detection](../../.artifacts/player-credits-detection/visual-detection.png),
[the player cue](../../.artifacts/player-credits-detection/detected-credits-player-cue.png),
[manual reset](../../.artifacts/player-credits-detection/automatic-after-manual-reset.png),
and [disabled publication with retained evidence](../../.artifacts/player-credits-detection/disabled-retains-evidence.png).
Screenshot review identified a placeholder-image span affected by progress-bar
CSS. The dedicated progress class was fixed, the player image rebuilt/deployed,
and two focused real-Docker visual paths passed with `errors=[]`. The
[placeholder](../../.artifacts/player-credits-detection/cue-placeholder-fixed.png)
and [loaded image](../../.artifacts/player-credits-detection/cue-loaded-image-fixed.png)
both render at 358x201.375 pixels; the loaded image has natural width 1920.
The independent three-pixel progress bar is `aria-hidden=true` and does not
resize or cover the image content. The
[result](../../.artifacts/player-credits-detection/cue-fixed-results.json)
and [geometry evidence](../../.artifacts/player-credits-detection/cue-image-geometry.json)
record restored user state, the test TV library disabled, and no residual media
processes. This one-off verification remains an artifact rather than a permanent
test suite: report 72 automated cases plus these two visual paths separately.
The native/public contract is documented in
[credits markers](../../docs/api/credits-markers.md).

## Backend-supported handoff restoration: completed

A renewed October 5 handoff audit found additional frontend omissions despite
the earlier feature and fixture closeouts. Existing backend data already
supported these behaviors. They were not new backend gaps, and the earlier
records must not be interpreted as proof that every handoff detail was restored.

| Omission | Implemented restoration |
| --- | --- |
| Poster hover interaction and background color following | Hover cards expose real metadata/actions and follow the complete card rectangle, matching the handoff anchor. Non-stage pages without a hover use the gold accent independently of the background image. Derived accents use 24 hue buckets, adjacent-bin weight 0.6, and bounded 60–70 lightness. Poster secondary text keeps at most two real genres. |
| Seek hover images | The player uses real ThumbnailSet frames tied to the active source, with a time-only fallback when unavailable. |
| Independent HLG, Dolby Vision, and year controls | HLG/DV have distinct server-filter choices; the search page exposes independent year/quality browsing and explains when that choice leaves mixed name search. |
| Mixed genre strips on separate catalog pages | Movie and Series pages supply their respective IncludeItemTypes to Genres instead of requesting Movie,Series for both. The backend already supported this selector. |
| Mobile catalog tools | The catalog toolbar retains the handoff's single-row arrangement on narrow screens. |
| Media information details | Existing stream/source fields supply resolution, codec, frame rate, bitrate, dynamic range, audio layout, and selected-track labels. Three specification blocks return to a vertical stack, with the handoff HUD typeface, spacing, clamped top position, and uppercase HUD/specification text. Unknown values retain an unavailable state. |
| Subtitle styling and current-track feedback | Text-subtitle size/style is shared with Settings and the active track label is shown. Bitmap subtitle styling remains part of source/burn-in behavior. |
| Stage behavior and navigation | Outgoing artwork/video transitions, depth/focus treatment, restored focus, and horizontal edge controls follow the handoff. |
| Repeated series in recent viewing | Recent episodes are deduplicated by Series identity while retaining corresponding series navigation/artwork. |
| Preview sound | A user gesture can enable an existing theme/trailer's real audio. Playback starts muted, and generated previews retain their required audio-free output. |
| External player completeness | All handoff player names and official marks are present with platform filtering. Supported URL builders and manual-URL combinations are distinguished. A transient notice offers reopen/copy actions. |
| Cross-platform remembered player choice | Resolve an available per-item player first, then the available global player, then Goby. Unsupported remembered choices do not erase a valid default. |

The final `goby-player-ui:step6-final` image built and deployed on `test-env` as
`sha256:a05fa96a88d8bd7b79c374e78cb6233cd7bd0726086c83cde7d6f1ea461a8304`.
The backend remains
`sha256:50fd6d8db8583e4bc80833d7ead7bcfcf45347fdea3cb2940d036ea9f90df82f`;
this restoration did not change backend code. The review host's compiled assets
were exported from the same final player image. Both container roots remain
read-only. The initial `86605b68...` and intermediate `0014977b...` images are
earlier evidence stages, not the current image. The
[source manifest](../../.artifacts/player-handoff-20261005/handoff-source-manifest.json)
records the source snapshot separately from the staged image/visual evidence.

Automated coverage passed 101 distinct cases: 95 regression cases, five hover
cases, and one added effective-player fallback case. The final 18-case focused
run passed external-player, detail, and background cases; it overlaps those
101 and is not an additional distinct total. Evidence:
[95-case regression](../../.artifacts/player-handoff-20261005/handoff-regression.log),
[hover checks](../../.artifacts/handoff-hover/handoff-hover-tests.log),
[final focused checks](../../.artifacts/player-handoff-20261005/handoff-final-focused.log),
[production build](../../.artifacts/player-handoff-20261005/handoff-build-final.log),
and [player image build](../../.artifacts/player-handoff-20261005/handoff-player-image-build.log).
The final build retained the expected large hls.js chunk warning and completed.
A subsequent 16-case catalog/hover/background recheck also passed; it overlaps
the same 101 distinct cases and does not enlarge that total.
The final anchor correction passed the five hover cases again, also without
adding new distinct cases.

The then-current visual comparison and read-only real-service walkthrough
finished within the scope recorded below. They did not establish complete
timeline fidelity: the later user review identified additional differences.
The evidence retains distinct stages rather than treating every screenshot as
final styling:

| Evidence stage | Scope and interpretation |
| --- | --- |
| [Initial comparison](../../.artifacts/player-handoff-20261005/visual-review/results.json) | 60 reference/implementation images at 1440x900, 1040x780, and 393x852. No page errors, broken images, or horizontal overflow; the paired review identified the styling/query omissions corrected afterward. These images include superseded styling. |
| [Intermediate corrections](../../.artifacts/player-handoff-20261005/visual-review/reviewed/results.json) | 24 paired images across the same three viewports using the reviewed image. They cover gold accent, catalog filtering/layout, and media-information changes with no errors, broken images, or overflow. Hover position and HUD case were subsequently refined. |
| Normalized hover-anchor comparison | Two intermediate diagnostic images in `visual-review/hover-anchor` exposed the 30-pixel error from using the poster rectangle instead of the complete card rectangle. They are not final evidence. |
| [Final focused comparison](../../.artifacts/player-handoff-20261005/visual-review/final/results.json) | Six paired images from the final image: desktop hover and desktop/mobile media information. Both hover boxes are x=206, y=157, width=369, height=396. The two HUD and three specification elements use computed uppercase at both media-information sizes. Errors and horizontal overflow are zero. |
| [Real-service read-only walkthrough](../../.artifacts/player-handoff-20261005/visual-review/real/results.json) | Six captures from the reviewed image, before the final anchor/case-only refinement. Real hover/filter/detail/media views passed; two existing waveform tracks became ready. Watching state and media-task history remained unchanged, and no analysis request was made. |

The real library had no current ThumbnailSet and returned 404; the frontend
used its existing still/timestamp fallback without generating new thumbnails.
That data-availability result is recorded separately from UI failure. The final
two geometry/case changes have their own focused screenshots, while the other
functionality remains covered by the preceding real-service walkthrough.

Historical final-image screenshots for that increment are available for
[desktop hover](../../.artifacts/player-handoff-20261005/visual-review/final/1440x900-hover-implementation.png),
[desktop media information](../../.artifacts/player-handoff-20261005/visual-review/final/1440x900-media-info-implementation.png),
and [mobile media information](../../.artifacts/player-handoff-20261005/visual-review/final/393x852-media-info-implementation.png).

These checks do not establish successful launching of installed external
applications. Native reopen links
and copy actions are available, but browser/OS association and actual application
startup remain untested. Launch URLs stay in transient notice state and are not
saved as player preferences.

The later bitmap subtitle timeline increment below supplies the previously
missing embedded PGS/DVD interval contract; playback/burn-in remains independent.
The user explicitly excluded intersecting mixed Search/Hints with year/quality: it is out of scope,
not a pending task. The implemented independent modes remain accepted. Adding
audio to generated previews would change the explicit
silent-output requirement and is not a required restoration item.

## Bitmap subtitle timeline increment

The approved schema-60 increment implements
[embedded PGS/DVD display intervals](../../docs/api/subtitle-timelines.md).
Generation defaults off, reuses the existing bitmap decoder without OCR/GPU
processing, and publishes permanent source-side GSTL files in
`backdrops/goby-subtitle-timelines/<sha256(exact source basename)>/`.
All admitted tracks must succeed before publication. Ordinary requests reuse
retained files; only explicit Force replaces them, and failure/cancellation
preserves the previous publication. A stale source axis remains on disk but is
not served to the player.

Consumer subtitle labels and lanes now use one filtered row list. A row appears
only after valid nonempty text or bitmap intervals load for the active source
clock. Missing, failed, unsupported, stale, empty, or malformed results create
no placeholder row. A failed Force can leave the previous current artifact
visible; its failure is administrator information. Subtitle selection and
playback remain independent of timeline availability. External SUP and IDX+SUB
were outside that schema-60 increment; the schema-61 follow-up below adds their
source-bound timelines without claiming external bitmap playback delivery.

Focused verification ran on `ssh test-env`, with no local build/runtime checks
for this increment:

| Evidence | Result and scope |
| --- | --- |
| [Player build](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-player-build.log) and [new browser group](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-player-tests.log) | TypeScript/Vite build and 35 new cases passed. Checks cover real row filtering, source/version/index validation, interval gaps, malformed data, bounded requests, account changes, stage reentry, cancellation, and selection independence. |
| [Existing player regression](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-player-regression.log) | All 101 existing cases passed against the matching built assets. |
| [Final administrator build](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-admin-build-final.log) and [final Node checks](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-admin-tests-final.log) | Build and 40 cases passed for task/library controls, strict data decoding, request replay, progress, and diagnostic presentation, including valid `Warnings:null` summaries. The earlier 33-case/build receipt predates the integrated administrator fix. |
| [Core](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-core.log) and [media](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-media.log) checks | Library, task, HTTP, migration, media-format, and existing bitmap-decoder checks passed. HTTP checks include real storage publication, authorization, source/tag changes, corrupted material, response bounds, and read-only behavior. |
| [Actual media extraction](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-media-actual.log) | Nine PGS/DVD containers passed exact interval checks, including English/Chinese, forced cues, and overlapping displays with preserved gaps. Capability pinning, cancellation before/after a track, and second-track failure with no partial bundle also passed. |
| [Backup checks](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-backup-actual.log) and [catalog export](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-backup-catalog60.log) | 45 tests with 246 subscenarios passed. The PostgreSQL 17 schema-60 catalog contains 83 tables. Historical restore rules remain scoped to their schema, and sidecar bytes are outside database restore. |
| [Initial backend build](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-backend-build.log), [player image](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-player-image.log), and [deployment](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-deployment.json) | Separate backend/player images built and deployed with schema 60; runtime reports subtitle timeline generation available. The initial backend image predates the integrated administrator fix, and this record alone does not establish the live generation journey. |

The [desktop](../../.artifacts/subtitle-timeline-20261005/subtitle-timelines-desktop.png)
and [mobile](../../.artifacts/subtitle-timeline-20261005/subtitle-timelines-mobile.png)
captures are fixture-backed presentation checks. They show the valid-only
subtitle row structure and do not claim real deployed source extraction or
complete handoff fidelity.

Integrated administrator review found that a valid artifact with no warnings
can serialize `Warnings:null`. The administrator decoder rejected that value
and hid the detail response. The decoder now accepts the nullable field as an
empty warning list; malformed non-null warnings remain invalid. The final
40-case/build receipt above includes this correction. The rebuilt backend image
is `sha256:3460d1ccc03070898b1cb9615fa46be2f9dc39308569f86d8b61efa03540fb2a`,
with binary SHA-256
`037708e52d61b7b8cae3f0f874b932f1e848b4900ddd2d1ffe763973c7bcc95e`.
The independent player image remains
`sha256:645ff00b2df5665d143671db556986cffdd181e61299ca3e0d0e2901726a9221`.
The earlier backend image and 33-case administrator run retain historical scope.

The complete [live record](../../.artifacts/player-subtitle-timelines/ACCEPTANCE.md)
retains the first generation, administrator correction, script timing failures,
and final pass. The
[final result](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/results.json)
passed all seven functional/lifecycle phases with `failures=[]` and
`cleanupComplete=true`. This result does not count as a pass for complete-page
design restoration:

| Phase | Observed result |
| --- | --- |
| Automatic default and reads | The isolated library defaults off, and descriptor/detail reads create no analysis runs. The final run started with retained material; it does not claim another initially missing state. |
| Administrator generation and exact intervals | The manual request reused the retained 114-byte GSTL. Its public track JSON and source-side binary match the independent oracle for both original stream indexes. The first non-reused generation is retained separately below. |
| Consumer rendering | Both labels and lanes render at 1440 x 900 and 393 x 852. SVG endpoints match the oracle to less than one tick, preserving the positive PGS gap. Anonymous descriptor/track reads return 401, and a player token does not authorize native administrator detail. |
| Ordinary reuse and explicit Force | Ordinary batches and option toggles preserve the generation, hash, and modification times. Force publishes a new generation/tag; old URLs return 404. Unchanged input produces identical interval bytes. |
| Failed Force preservation | An actual directory permission failure reports `media_directory_not_writable`. The existing 114-byte artifact and its current intervals remain readable and visible. |
| Stale-source hiding | A source mtime change preserves the file but makes the descriptor unavailable/stale and the old track URL return 404. Both consumer labels and lanes disappear. An ordinary request reuses the retained stale material without rebuilding it. |
| Current-source restoration | Restoring mtime, rescanning, and explicit Force publishes current intervals and restores both rows. The filesystem ctime cannot be restored, so the new artifact binds to the resulting source stamp. |

The isolated source is a 12-second 320 x 180 H.264 MKV without audio. Its two
embedded tracks copy authored PGS/DVD packets without OCR. The
[independent oracle](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/oracle.json)
comes from the authored input manifest and measured before/after mux packet
hashes and timestamps, not the API under test. PGS stream 1 covers
`[1.024, 5.632)` and `[6.656, 8.704)` seconds; forced Chinese DVD stream 2 covers
`[2.560, 5.632)`. The indexed origin and measured mux offset are both zero.

The [first run](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-02-51-925Z/results.json)
records the genuine missing state, zero timeline task history, no label/lane
before generation, and first successful task with `reused:false`. That run then
failed on the valid `Warnings:null` administrator response described above;
it is not a complete journey pass. The two following runs retained successful
administrator checks but failed on acceptance-script screenshot synchronization.
The listener was subscribed after a restored stage had already fetched its
descriptor; subscribing before navigation fixed the script. Those two failures
are retained as harness timing failures. The final complete run honestly starts
with its existing artifact rather than deleting it to simulate a fresh source.

Final screenshots cover the
[administrator artifact](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/administrator-generated.png),
[desktop rows](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/desktop-generated.png),
[mobile rows](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/mobile-generated.png),
[hidden stale rows](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/desktop-stale-hidden.png),
[administrator stale material](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/administrator-stale-preserved.png),
and restored
[desktop](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/desktop-restored.png)/
[mobile](../../.artifacts/player-subtitle-timelines/run-2026-10-04T22-09-08-591Z/mobile-restored.png)
views. The synthetic source has no artwork or generated seek thumbnails; its
existing artwork fallback and optional ThumbnailSet 404 remain expected data
conditions. It is a 12-second source with no audio tracks or saved resume
position. These captures cannot establish the appearance of the handoff's
complete filmstrip, multiple waveforms, subtitle rows, and resume marker.
The stale screenshot specifically verifies required row hiding, not the
populated design. The mobile capture also retains overlapping ruler labels;
the absence of document overflow did not detect that presentation defect.

The library is left with automatic generation off. Directory permissions and
source mtime are restored, original user playback state is unchanged, and no
helper process remains. The owned synthetic source and its current sidecar are
retained. Consumer viewing made no playback negotiation/reporting or generation
requests. This is isolated acceptance, not production rollout or universal
PGS/DVD/container coverage.

The [final source/image check](../../.artifacts/player-subtitle-timeline-20261005/subtitle-timeline-source-image-final.json)
matches 2,803 source files to the remote tested source, the running backend
binary to its recorded SHA-256, and all 256 player build assets to the image.
The only additional nginx asset is its base image's `50x.html`.
The schema-60 migration SHA-256 remains
`e8c704760274d884a37991275073bd639550bd785e80130a42646e0e6dde2251`.
Both application containers run with read-only root filesystems. These final
identities, not the initial image receipt above, identify the accepted deployment.

## Timeline visual correction: same-data comparison completed

The user challenged the earlier design-restoration conclusion after comparing
the short-source capture with the populated handoff timeline. The earlier
seven-phase result remains valid for its generation, publication, reuse,
authorization, and hiding checks. It is not evidence that the complete media
information page matches the design. Source-data differences must be recorded
separately from frontend presentation defects.

The current frontend correction restores the following details without changing
the backend APIs, waveform peak/RMS storage, subtitle sidecars, generation
policy, or interval semantics:

| Finding | Current source correction |
| --- | --- |
| The filmstrip was limited to nine frames | Sample up to twelve real thumbnail frames; keep the existing artwork fallback when thumbnails are absent. |
| Audio and subtitle lanes had an extra full-height left stripe | Remove the invented three-pixel stripe so time zero does not imply data. |
| Raw bitmap codec identifiers crowded subtitle names | Display compact PGS/DVD/SRT labels and retain the raw codec as a tooltip. Keep the track code and codec from shrinking; ellipsize a long subtitle name instead. |
| Audio and bitrate text differed from the handoff | Respect the server's display language, render specifications such as `TrueHD Atmos 7.1` and `AC3 5.1` with a single separator space, and omit an unnecessary `.0` from integer Mbps values. |
| The resume badge used a full hour-prefixed timecode | Use the handoff duration format with two-digit minutes below one hour; keep ruler labels in `HH:MM:SS`. |
| Audio label lines used a three-pixel gap | Restore the handoff's two-pixel gap. |
| Waveforms added two visible amplitude layers and a technical legend | Render a single layer of real peak amplitudes at 56% bar width, with at most 180 display bins. Preserve every invalid gap; a gap can split a bin into multiple bars. Keep silence/gap semantics and accessible descriptions, and remove the extra visible legend. RMS remains stored and available through the unchanged API. |
| Five ruler labels overlapped on narrow tracks | Use the actual track container width: five labels above 340 px, three from above 200 through 340 px, and two at or below 200 px. |
| A resume badge could extend beyond the track or cover mobile ruler labels | Clamp the badge within the track while preserving the true marker position. At container widths up to 340 px, place the badge at `top: -25px`, above the ruler; keep the desktop badge and axis layout unchanged. |
| StageDeck scaling could affect waveform sampling density | Measure the lane's `clientWidth`, so display-bin selection follows its layout width rather than an animated transformed rectangle. |
| Media information retained a small vertical offset from the handoff | Restore media-page bottom padding to `clamp(40px, 7vh, 72px)` on desktop and `100px + safe-area-inset-bottom` on mobile. |

Subtitle row height and block height already match the handoff: a 24-pixel
lane and a `1000 x 10` SVG with blocks from y=2 to y=8 produce 14.4-pixel blocks.
Sparse intervals in a short source are not a height defect. Real intervals must
not be widened, split, or fabricated to imitate the prototype's random blocks.
Likewise, gold versus blue is not itself a defect because the handoff derives
its accent from artwork. The user's gold annotation rectangle is not a design
element.

The completed same-data comparison uses deterministic media data in the
reference and implementation: loaded artwork, twelve filmstrip frames, two
audio waveforms, three subtitle tracks, selected-track state, duration, and a
saved resume position. The original handoff's `deep` season-one first episode
has a 48-minute runtime, 3840 x 1608 video, and a 22:52 resume position in this
comparison. Reference SVG intervals become two PGS API tracks and one text
track; reference audio amplitudes become valid GAWL fixture payloads consumed
by the production parser. These are controlled fixture signals, not real-media
extraction results. Actual extraction remains covered by the separate
real-media receipts above. Complete-state comparisons and unavailable/stale-state
checks retain their separate purposes. Display-bin peak aggregation can produce
small shape differences from the prototype's randomized waveforms; this does
not justify claiming pixel-identical paths.
The HUD also presents real source/playback state and seconds-based timecodes.
These intentional data representations are distinct from the corrected layout
defects; this work does not claim pixel identity for the entire page.

The matching player production build and Docker build passed on `test-env`.
The final deployed player image is
`sha256:2bcfffd9c5162fdaa0c2e37e409d40d090cac288224655885901c87e80ca1e84`.
The backend remains
`sha256:3460d1ccc03070898b1cb9615fa46be2f9dc39308569f86d8b61efa03540fb2a`.
Source/image correspondence records the three changed production source files
and matches all 257 player build assets to the container in the
[final deployment receipt](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-deployment-final.json).
The earlier `9f0c2b7c...` and `9aae8518...` images are intermediate evidence,
not the final deployment. The latter is retained in the
[deployment before the badge refinement](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-deployment-before-bubble.json).

| Evidence for this correction | Status |
| --- | --- |
| Matching production build and focused regression | Passed on `test-env`: [67 focused browser cases](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-regression.log), the [final production build](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-build-final.log), [final Docker build](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-image-final.log), a [five-case layout recheck](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-layout-final.log), and [two final badge endpoint cases](../../.artifacts/timeline-visual-correction-20261005/timeline-visual-head-final.log). The five and two cases overlap the 67; neither increases the distinct count. |
| Complete-state reference/implementation desktop and mobile images | Passed all 15 final captured states with 45 primary screenshots and `failures=[]` in the [final result](../../.artifacts/timeline-visual-review-20261005/after/results.json). Desktop position and row dimensions match exactly; the mobile vertical difference is 0.13 px because production uses the exact 3840/1608 aspect ratio instead of rounded 2.39. Twelve loaded frames, both 33-interval PGS tracks, and the 35-interval text track match the reference data. Selected, unselected, and unavailable/empty states pass. |
| Narrow-container ruler | Passed the rendered checks: 393-pixel viewport uses three readable labels and 375-pixel viewport uses two, with no overlap. The original handoff's inherited five-label mobile overlap is an intentional prototype defect correction. |
| Read-only live-service review before the badge refinement | Passed at two viewports with four captures on `9aae8518...`. PGS/DVD codec labels are fully readable, each with `clientWidth = scrollWidth = 22` px; actual intervals and the positive PGS gap remain exact. There are no invented left markers, ruler-label overlap, or horizontal overflow. All observed application requests are GET/HEAD. Task, user, playback-session, queue, and source-file summaries remain unchanged. This run is not relabeled as live verification of the later `2bcfffd9...` image. |
| Final resume-badge endpoint and paired-image checks | Passed both endpoint cases across 320-, 375-, 393-, and 1440-pixel viewports. The narrow-track assertion requires `badge.bottom <= tick.top - 1`. All six final 393/375-pixel implementation states place the badge 2 px above the timestamps, retaining horizontal bounds and true marker position. |
| Stable waveform display density | Final selected, unselected, and unavailable-subtitle states consistently render 179 display bins at 1440 px, 35 at 393 px, and 32 at 375 px. Layout width is unaffected by StageDeck scaling. |

The [final visual review](../../.artifacts/timeline-visual-review-20261005/REVIEW.md)
records reference provenance, extracted fixture inputs, exact geometry, colors,
fonts, loaded images, and scope. Primary pairs are the
[desktop reference](../../.artifacts/timeline-visual-review-20261005/after/1440x900-reference-selected-timeline.png)/
[desktop implementation](../../.artifacts/timeline-visual-review-20261005/after/1440x900-implementation-selected-timeline.png)
and [mobile reference](../../.artifacts/timeline-visual-review-20261005/after/393x852-reference-selected-bottom.png)/
[mobile implementation](../../.artifacts/timeline-visual-review-20261005/after/393x852-implementation-selected-bottom.png).
The six mobile implementation timeline crops include the badge outside the
timeline box. They are direct subimages of the original bottom-of-scroll
captures, using the saved coordinates in
[mobile-crops.json](../../.artifacts/timeline-visual-review-20261005/after/mobile-crops.json);
no pixels were redrawn or substituted. Original element-bounded clips remain
as historical backups and do not increase the 15-state/45-primary-image count.
The comparison harness source matches between the local and remote workspaces.

The [read-only result before the badge refinement](../../.artifacts/timeline-visual-correction-20261005/live/run-2026-10-04T22-43-39-761Z/results.json)
retains the two-viewport observations and unchanged-state summaries. Its
[desktop timeline](../../.artifacts/timeline-visual-correction-20261005/live/run-2026-10-04T22-43-39-761Z/timeline-1440x900.png)
and [mobile timeline](../../.artifacts/timeline-visual-correction-20261005/live/run-2026-10-04T22-43-39-761Z/timeline-393x852.png)
show the real retained PGS/DVD artifact on `9aae8518...`. They confirm that
image's source-data presentation and read-only behavior; this short real
source remains separate from the full same-data handoff fixture and the later
badge-only refinement.

The 67 focused cases overlap earlier coverage and are not added to the
historical 136 player cases as new distinct tests. Neither those tests nor the
earlier seven lifecycle phases establish visual fidelity on their own. The
final paired review establishes the populated timeline layout and state
behavior within its stated fixture scope. Mobile ruler/badge readability
adaptations are intentional; actual HUD state, seconds-based timecodes, and
width-dependent waveform peak aggregation remain documented representations,
not unresolved layout defects or a claim of whole-page pixel identity.

## Design differences corrected during review

| Finding | Final implementation |
| --- | --- |
| The mobile search/avatar buttons were positioned inside the bottom glass bar | Tools are siblings of the backdrop-filter container and remain in the top-right corner |
| Desktop hero thumbnails wrapped into two rows | All six thumbnails remain in one row from 1100 px; the information column yields space |
| Font family names did not match the bundled variable fonts | All pages use the actual Noto Sans/Serif SC, League Gothic, and IBM Plex Mono families |
| Extra hero padding and an unnecessary stage top scrim changed alignment and contrast | Hero text/actions use the handoff gutter and bottom offset; stage artwork is not covered by that top scrim |
| Secondary-page side scrims were too dark | Blur, side scrim, ambient dimming, and accent glow are distinct fixed layers |
| Background page animations could accumulate scale | Depth animations have bounded lifetimes and cancel preceding depth animations; reduced motion skips them |
| Paging could consume a gesture intended to read overflowing content | Wheel, keyboard, and touch first respect inner-scroll boundaries; inertial wheels and the 1050 ms lock are handled |
| The episode strip could move a season tab away before a quick click completed | Pointer expansion waits 150 ms; immediate focus expansion is restricted to keyboard focus |
| Strip activation could target the previously focused episode | Keyboard navigation and activation use the current episode; inactive pages are inert |
| Latest-series requests returned no items on the actual backend contract | The adapter requests playable Episode items with server grouping to obtain Series representatives |
| Negotiated root media URLs could bypass the standalone API proxy | Media URLs are normalized into the `/emby` namespace while retaining authentication and stream parameters |
| Slow preference/profile requests could replace newer choices or a changed account | Requests are account-bound; preference loads check local edit revisions and saves retain a local cache |
| Playback could use catalogue duration instead of the active source clock | Progress, seek, resume, completion, and next-episode behavior use the negotiated/decoded timeline |
| Media information could imply that thumbnail and subtitle timeline data were missing APIs | Existing ThumbnailSet frames and real WebVTT cue intervals are used; the later schema-60 increment adds embedded PGS/DVD intervals and omits subtitle labels and lanes without valid data |
| Subtitle timeline loading could remain incomplete after leaving and reentering the page | Entering the media page restarts a canceled track request |
| Plain-HTTP installations may lack the Clipboard API | Copying a stream provides a manual selectable URL fallback |
| Returning from playback discarded the detail page position | Page position and the last non-player route are retained per user |
| External application letter tiles and incomplete choices remained prototype omissions | The complete platform-filtered list now uses first-party artwork, including the extracted official PotPlayer mark; verified URL-builder contracts are separated from manual opening |
| The administrator credits dialog title and its inner typography shared an ID, making the accessible name ambiguous | The dialog now has one unique title ID and an unambiguous `aria-labelledby` association |
| Background publication could report a post-rename synchronization/read-back error after selecting the new generation | Manifest replacement remains provisional through synchronization and complete read-back/close validation; failed publication restores the previous selection before returning an error |
| The next-episode progress selector also styled spans inside a missing-image placeholder | A dedicated progress class keeps the three-pixel indicator separate; both missing-image and loaded-image paths passed focused real-Docker visual checks |

The catalogue data differs intentionally from the original prototype where the
backend has no matching metadata. For example, a Series container may not expose
the episode's codec badges. Ratings, track names, timestamps, and artwork are
not fabricated to make screenshots identical.

## Visual evidence

The review uses identical viewport sizes and matching design image IDs, with
reduced motion enabled to make still-image comparisons repeatable. The normal
motion implementation is also checked against the handoff's durations and
curves. This is a visual/interaction walkthrough, not a claim that every rendered
pixel is equal despite different media metadata and live finish times.

| View | Design reference | Implementation |
| --- | --- | --- |
| Home, desktop | [Reference](../../.artifacts/player-acceptance/visual-review/reference-home-desktop.png) | [Implementation](../../.artifacts/player-acceptance/visual-review/home-desktop.png) |
| Detail overview, desktop | [Reference](../../.artifacts/player-acceptance/visual-review/reference-detail-desktop.png) | [Implementation](../../.artifacts/player-acceptance/visual-review/detail-desktop.png) |
| Episode stage | Handoff episode stage | [Implementation](../../.artifacts/player-acceptance/episodes-desktop.png) |
| Cast and similar titles | Handoff cast/recommendation page | [Implementation](../../.artifacts/player-acceptance/people-desktop.png) |
| Media information | Handoff monitor and timeline | [Implementation](../../.artifacts/player-acceptance/media-desktop.png) |
| Actual video player | Handoff playback controls | [Implementation](../../.artifacts/player-acceptance/player-desktop.png) |
| Small-screen detail after scrolling | Handoff overflow rule | [Implementation](../../.artifacts/player-acceptance/detail-small-mobile-scrolled.png) |
| Existing theme video, desktop | Handoff moving stage; generated acceptance media | [Visible decoded video](../../.artifacts/player-live-20261004/live-capabilities/theme-video.png) |

Additional desktop/mobile screenshots, the 20-layout matrix, raw Playwright
reports, traces, and API audits are in `.artifacts/player-acceptance`.
Reproduction instructions are in [tests/README.md](tests/README.md).

## Accepted scope and remaining capability boundaries

The user accepted mixed media/person/genre name search plus independent
year/video-quality filters, and existing 4K, ordinary HDR/HDR10, HLG, and Dolby
Vision filtering. Unified original-title/year/resolution free-text interpretation
and exact HDR10+ classification are outside this accepted scope, not unfinished
requirements. The underlying name-query and probe-metadata limits remain
documented in the [capability assessment](BACKEND-CAPABILITIES.md).

The remaining capability boundaries have the following status:

1. Background generation beyond the accepted SDR/HDR10/HLG and strict Profile
   5, 8.1, 8.4, 8.2, and complete Profile 7 MEL corpus. Profile 8.2 acceptance is
   limited to original analytic material; FEL is deferred and is not
   reconstructed. Every DV path still requires the admitted Vulkan environment
   and strict source/RPU evidence. New Docker deployment and other devices are
   outside the native AMD profile-extension receipt.
2. Automatic credits precision outside the accepted corpus. Movie/TV detection
   and its lifecycle have passed the selected acceptance above, but the native
   matcher can place boundaries early and does not establish universal accuracy.
   Manual/source markers retain precedence.
3. Exact elapsed viewing analytics. The handoff-style estimated content-hours
   statistic is now implemented alongside watched movie/episode counts. Exact
   elapsed time is an optional upgrade, not a design-reproduction requirement.
4. Advanced external bitmap features and new playback acceptance. The schema-60
   increment implements embedded PGS/DVD cue lanes; schema 61 adds source-bound
   SUP and multilingual IDX/SUB timeline generation. The October 6 source
   follow-up adds encoded external-bitmap playback independently of those
   timelines. Its nine feature-source backend/player phases passed, as recorded
   in the final section below; it is not yet in the accepted Docker catalog.
   Original bitmap-file download,
   paired deletion and unsupported IDX presentation directives remain outside
   the selected playback contract.

Per-track measured waveforms are implemented; their verification is tracked in
the schema-58 increment above rather than listed as an absent API.

Client-derived accent colors, server-persisted item choices, intro skipping,
favorites, watched state, BIF still frames, and subtitle cue timing are not
listed as missing backend capabilities.

## Boundaries outside backend feature availability

The initial local fixture run did not include Docker or a real Goby database.
The later remote Docker run supplies the selected real-media playback evidence
described above. It covers software H.264/AAC conversion and text subtitles,
not GPU processing, HDR tone mapping, every subtitle/codec combination, or a
production rollout. The eight-phase capability run adds real search, filters,
theme, and credits evidence within that same isolated environment. Runtime
resources and image identities belong to their recorded
acceptance workspace; this is not a published immutable player release archive.

External application launching depends on installed OS protocol handlers.
IINA, mobile VLC, MX Player, Infuse, Windows PotPlayer, desktop mpv 0.41+ with a
registered handler, and Windows/Android Dandanplay have URL builders. Desktop
VLC, Stellar, macOS Dandanplay, and nPlayer retain manual original-URL opening.
No installed-application launch was accepted here. These device/protocol
boundaries are distinct from backend gaps; artwork and contract provenance are
documented in [THIRD-PARTY.md](THIRD-PARTY.md).

## External SUP and multilingual IDX/SUB timeline increment

The schema-61 increment adds a separate bitmap-sidecar catalog with stable public
indexes and component identity/hash snapshots. A SUP creates one track; an IDX
and its uniquely matched SUB can create multiple language tracks. The generator
borrows authorized descriptors, preserves absolute subtitle clocks and IDX
delays, and publishes GSTL v2 only after every admitted track succeeds. GSTL v1
remains readable and unchanged when no external track is added. Component or
catalog changes invalidate delivery while retaining the previous material;
only successful explicit Force regeneration replaces it.

These tracks expose `GobySubtitleTimelineOnly`, not a fabricated external text
URL or newly implemented bitmap playback path. The player shows their valid
media-information lanes but excludes them from default, remembered and manual
playback selection. Real language-code titles and codec-only metadata now yield
readable language labels while preserving custom titles and codec badges.

All verification for this increment ran on `ssh test-env`:

- Native media regression passed 62 top-level tests and 218 subtests. One
  pre-existing opt-in fixture availability case lacked its old corpus settings
  and was skipped. An independent FFprobe oracle confirmed the authored IDX/SUB
  demux ordinals and packet timestamps.
- Source-side timeline persistence passed 26 top-level tests without skips,
  including real PostgreSQL, file changes, index fences, Force and FD cleanup.
- The broader subtitle/sidecar library regression passed 83 top-level tests.
  Two pre-existing provider tests require specific mount/permission conditions
  and were skipped. The narrow-query regression explicitly counts bitmap reads
  and still excludes every subtitle family when they are not requested.
- Final backup/migration verification on disk-backed PostgreSQL 17 passed
  26 top-level tests and 63 subtests, including real snapshot/restore, invalid
  archive rejection, and old/new migration targets. The catalog has 84 tables.
- The final player production build and all 51 subtitle-timeline browser cases
  passed. Raw metadata labels, large public indexes, missing/stale states and
  playback preference isolation are covered.

The [Chinese real-service report](../../.artifacts/external-subtitle-20261005/live/REVIEW.md)
and its raw receipts retain the separate Docker/API/browser evidence. The live
environment uses a real 20-second H.264 file with no embedded subtitles and
three external bitmap tracks. It is isolated from the previous deployment and
the user's local preview. It does not establish external bitmap playback,
unrestricted IDX custom presentation commands, or a formal player release.

Earlier failed attempts remain documented: an undersized disposable PostgreSQL
volume exhausted storage; legacy query/migration test expectations required
updating for the new table and explicit recovery target; three initial player
test expectations incorrectly treated Default subtitle mode as Smart mode.
The final runs corrected those issues without weakening source or authorization
guards. Retried/overlapping cases must not be added to the counts above.

## External SUP and multilingual IDX/SUB encoded playback — October 6, 2026

The current source increment connects indexed external bitmap subtitles to the
existing encoded-subtitle path. It does not change schema 61, require timeline
generation, or modify the production player UI. Item and PlaybackInfo stream
metadata use `DeliveryMethod: Encode` and stop setting
`GobySubtitleTimelineOnly` for these tracks. The existing player can select a
track, switch between language tracks and choose Off through normal playback
negotiation. The selected path requires video transcoding and never invents a
WebVTT URL, text stream or original-bitmap download endpoint.

The stable public stream index, catalog tag, source identity and private demux
ordinal remain bound. A multilingual IDX/SUB pair is authorized and checked
as two components; its language identifier is not used as the demux ordinal.
The runner receives synchronous borrowed descriptors and creates bounded,
fixed-name private job files before starting FFmpeg. It normalizes the validated
IDX syntax, preserves source clocks, and maps the bitmap canvas onto the primary
video before output scaling. Original SUP/IDX/SUB files remain read-only and
are never rewritten or deleted. Timeline generation stays independent and
default-off. Unsupported IDX presentation features fail explicitly.

Focused verification on `test-env` passed 14 library cases/27 subtests, 33 media
cases/61 subtests, 20 transcode cases/47 subtests, seven server cases/24 subtests,
and two cancellation-race cases/two subtests. One pre-existing opt-in media
availability fixture was skipped. Type checking and all 55 player cases passed
(four new selection cases and 51 existing timeline cases, no skips). Three
embedded-bitmap HDR/seek regression cases passed using the existing accepted
software image's media tools after the host FFmpeg was found to lack `zscale`.
No production guard was changed to bypass that dependency.

The real backend and unchanged independent player passed all nine phases in
`live-1791231754984-73515dc3`. The feature source is
`2bc39dbea06b0d0abe3ea1c903f7032ead91ea2f`; the candidate image is
`sha256:276ce680bee830905561db8d2b21820ddbca47c46760a47f5de49fa3c0be202c`.
The run verified SUP display/clear events during continuous playback, the two
IDX language tracks' disjoint timing windows, Off and switching, player seeks,
actual MP4 subtitle offsets and nonzero starts. Each original SUP, IDX or SUB
component was separately changed: a previously working negotiated URL then
returned `503 / video_unavailable` without media bytes while the backend stayed
ready. Restoring, rescanning and renegotiating restored the expected pixels.
Timeline availability remained false, no generation task was queued and no
source-side timeline directory appeared. Media helpers retired after playback.

The independent authored glyph/time specification checked 27 browser native
frames and 12 decoded API frames. Its pixel thresholds were fixed before
candidate-output observation and were not relaxed after failures. The existing
Smart/Chinese subtitle preference remained intact: generation defaults off,
but newly playable matching tracks still follow the user's subtitle policy.
The first live attempt incorrectly assumed Subtitle Off; the second encountered
an ambiguous Play-button locator. The harness then explicitly selected Off and
used the actual bottom-bar button. Original failed runs are retained.

After feature acceptance, main advanced to
`d4678637097ce78235d883d5b1b62fa4ff516d4c`. Merge
`d7276ee8528817bd94ba42bd90c47092e84a64f6` preserves all 25 feature Go files
and adds the 23-file main delta. Targeted identity/library/server integration
race checks passed 25 top-level cases and 57 subtests, with no failures or
skips. The dedicated database was stopped again and the new compiler scratch
was reclaimed after worker exit. The nine-phase candidate run is not relabeled
as a run of this merge or counted a second time. Existing uncommitted work in
the primary checkout is outside this clean-source verification boundary.

The [implementation record](../../docs/development/external-bitmap-playback-20261006.md)
tracks the source contract, final verification and retained failures. The
accepted `2026-10-06-main-images` catalog still identifies backend source
`2ba10e3613cac9da0a2e2c8ae7317bba229fdd56` and the unchanged player from
`7aaaeed44526848737270089ab0227d2d410f864`; it has not been replaced by this
source increment. The original schema-60/schema-61 timeline paragraphs above
retain their historical capability and image boundaries.
