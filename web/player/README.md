# Goby player

Goby's consumer player is a standalone React, TypeScript and Vite application.
It implements the Goby player design handoff and uses the existing `/emby` API.
Its build output is `web/player/dist`; the Go executable does not embed or serve
these files. The existing React/MUI administrator dashboard remains independent
and continues to use its current Go packaging.

The October 4, 2026 scope decision selects Goby's own player. It does not select
Emby's proprietary web application, WebAppService, cloud account system or
offline synchronization. The [backend capability assessment](BACKEND-CAPABILITIES.md)
describes design features that cannot currently be backed by server behavior.
The [implementation and acceptance record](ACCEPTANCE.md) contains the design
walkthrough, fixes, initial local checks, six passed remote Docker playback
phases, eight passed real capability phases, and 12 passed persistent-background
Docker phases. The background increment also passed 40 browser cases, focused
Go checks, actual FFmpeg output checks, and PostgreSQL 17 backup/recovery.
The later waveform/hours increment passed 63 Playwright cases, 17 administrator
checks, and eight real waveform Docker phases. Bounded DV generation passed
eight profile/fixture scenarios in 11 executions on authorized CT104. These
receipts retain their separate source, media, and worker scope. A renewed
handoff audit subsequently found frontend omissions already supportable by
existing backend data. Their restoration build, 101 distinct automated cases,
staged visual comparisons, and read-only real-service walkthrough passed. The
acceptance record distinguishes earlier comparison screenshots from the final
hover-anchor and media-information recaptures.

The player supports mixed media/person/genre search through `Search/Hints`,
server-side 4K/HDR/HLG/Dolby Vision controls, and optional backgrounds from theme
videos, generated source-side clips, or local trailers. The default priority is
theme, generated, then trailer; each user can reorder the sources or select
still artwork only in Settings. Backgrounds start muted; an explicit gesture
may enable real audio in an existing theme/trailer. Generated previews remain
silent. Background reads never schedule generation.
The player consumes source-bound `CreditsStart` markers
for the next-episode cue. See [video filters](../../docs/api/video-catalog-filters.md) and
[manual credits](../../docs/api/credits-markers.md) for server semantics.
The accepted search scope is mixed name search plus separate year/video-quality
filters. The accepted video-filter scope is 4K, ordinary HDR/HDR10, HLG, and
Dolby Vision; unified original-title/year/resolution free-text search and exact
HDR10+ classification are not pending requirements.
Year/quality browsing is a separate mode from mixed Search/Hints name results;
the UI explains the switch. The user explicitly excluded intersecting mixed
name results with year/quality filters; it is out of scope, not a pending task.

Poster hover cards, artwork-following color, real seek thumbnails, media-format
fields, consistent text-subtitle styling, current-track labels, recent-Series
deduplication, stage transitions/focus, and horizontal edge controls now use
the existing APIs and handoff presentation behavior. The external-player list
includes every handoff choice and official marks, with a distinction between
URL builders and manual original-URL opening. Selection uses an available
per-item choice, then the global choice, then Goby. Actual installed-handler
launching is not claimed; see [protocol evidence](THIRD-PARTY.md).

The later increment implements [estimated content hours](../../docs/api/viewing-statistics.md)
from watched runtimes and unfinished progress, and
[per-track peak/RMS waveforms](../../docs/api/audio-waveforms.md) from actual audio.
Waveform files persist beside the source, require opt-in or explicit generation,
and are not served on a stale source timeline. The existing media-write overlay
supports both waveform and background-video namespaces.

[Bitmap subtitle timelines](../../docs/api/subtitle-timelines.md) cover
embedded PGS/DVD, external SUP, and each language in an external IDX/SUB pair
through a dedicated source-aligned interval API.
Generation defaults off and stores permanent GSTL sidecars beside the source;
it does not use OCR or a GPU. The media-information view shows a subtitle's label
and lane only after valid nonempty text/bitmap intervals load. Missing, failed,
unsupported, stale, and empty results have no status row; subtitle selection
and playback are independent. Diagnostics belong in the administrator/task UI.
External bitmap records are marked `GobySubtitleTimelineOnly`: their valid
coverage is visible in media information, while their as-yet unsupported
playback delivery is excluded from track selection. Text and embedded bitmap
playback retain their existing behavior.
Focused checks and the seven-phase real Docker journey passed on `test-env`,
including desktop/mobile rendering, missing/stale row hiding, reuse, explicit
replacement, and preservation after a real directory permission failure.
The [acceptance record](ACCEPTANCE.md) separates the initial missing-state
evidence from the final run that retained its existing artifact.

The [background generator](../../docs/api/background-previews.md) now admits
Profile 8.1 and complete Profile 7 MEL through strict Vulkan processing with
CPU H.264 encoding on a supported device. The October 5 implementation adds
Profile 5, Profile 8.4, and Profile 8.2; its native AMD media acceptance passed
all 21 required cases, recorded separately in [ACCEPTANCE.md](ACCEPTANCE.md).
Profile 8.2 coverage uses original analytic material. FEL reconstruction remains
deferred, and this receipt does not establish a new Docker deployment.
All generated output remains silent BT.709 SDR. Exact elapsed-time analytics
remains an optional upgrade;
the implemented hours figure follows the handoff estimate. The user has now
selected automatic credits detection for movies and TV, reusing the previously
chosen Intro Skipper project. The implemented detector passed its seven-phase
real Docker acceptance and focused regressions; manual/source markers retain
precedence. Its full
multi-interval data uses `GobyCreditsIntervals`, while standard chapters keep
`CreditsStart`. It remains opt-in per library and preserves explicit no-result
and stale-evidence behavior. The accepted audio sample boundaries were about
3.496 seconds early, so the result is not an exact-second accuracy promise.
See [credits markers](../../docs/api/credits-markers.md) and
[ACCEPTANCE.md](ACCEPTANCE.md) for the contract and separate evidence.

## Local development

Use a Node version satisfying `package.json` and the committed `package-lock.json`.
Run these commands from the repository root in PowerShell:

```powershell
Set-Location web/player
npm ci
$env:GOBY_DEV_API = 'http://127.0.0.1:8096'
npm run dev
```

Open `http://127.0.0.1:5174`. `GOBY_DEV_API` selects the existing Goby backend for
the Vite development proxy; its default is `http://127.0.0.1:8096`. `/emby` and
`/admin` remain same-origin paths in the browser. Use a configured Goby account
for sign-in. The backend still requires its own database, media and runtime
configuration; starting Vite does not start those services.

If the administrator dashboard will also be used through this origin, configure
the backend's `GOBY_PUBLIC_URL` as `http://127.0.0.1:5174` for that development
session and use `GOBY_COOKIE_SECURE=false` for local HTTP. Its administrator
origin checks are independent of the player token authentication. Production
HTTPS deployments keep secure cookies enabled.

The player uses hash navigation and is served at `/`. A page URL such as
`/#/movies` does not require backend route changes. Browser-visible API and
media requests use the same origin, so user tokens do not need to be sent to a
separately exposed cross-origin server.

## Build and acceptance

```powershell
npm run build
npm run preview
```

`build` runs the TypeScript check before writing Vite assets. `preview` previews
the compiled assets and is not a production service. For API-backed acceptance,
use the Vite development server with `GOBY_DEV_API` or the nginx container below;
do not assume that a static asset server proxies `/emby`.

The browser acceptance command is `npm run test:e2e`; install the Playwright
browser once with `npx playwright install chromium` if it is absent. Run local
checks only when the current task authorizes them; otherwise follow the
repository's designated verification environment. The October 4 player task
explicitly authorized its initial local build and acceptance; the subsequent
Docker and capability checks run on `ssh test-env`. Reproduction details for
the fixture and real-backend harnesses are in [tests/README.md](tests/README.md).
A successful frontend build alone does not establish backend media compatibility.

## Independent Docker image

For release archives, use the [player release guide](../../deploy/oci/README.player.md)
and `scripts/build-player-oci.py`. It binds the source revision, digest-pinned
Node/nginx inputs, exported image, asset readback and notices. The
[October 5 release record](../../docs/development/player-release-20261005.md)
binds the software, AMD and player images and tracks integrated acceptance.
An explicit development build from the repository root is also available:

```powershell
docker build -t goby-player:local web/player
```

The multi-stage Dockerfile runs `npm ci` and `npm run build`, then copies only
the compiled player into an unprivileged nginx image. Build arguments
`NODE_IMAGE` and `NGINX_IMAGE` allow release tooling to supply digest-pinned
images; the defaults are development tags, not an immutable accepted release.

The container listens on port `8080`. At startup, `GOBY_API_UPSTREAM` supplies
the server origin (default `http://goby:8096`, without a trailing slash or path).
It is a runtime nginx setting, so changing the backend address does not require
rebuilding the JavaScript. The entrypoint renders `nginx.conf` into `/tmp` and
works with a read-only root filesystem and a writable `/tmp` mount.

nginx serves the player at `/`, proxies `/emby`, `/embywebsocket` and `/admin`,
and preserves request paths, authentication, byte ranges and WebSocket upgrade
headers. API responses and streaming media are not proxy-cached or buffered.
Hashed Vite assets can be cached; the application HTML is revalidated. Access
logs omit query strings because authorized media URLs may contain tokens.
`/healthz` checks the static player service only; it does not assert backend,
database or playable-media readiness.

## Optional Compose integration

Keep the environment and persistent paths from the existing
[Docker operator guide](../../deploy/oci/README.md). The optional
[`compose.player.yaml`](../../deploy/oci/compose.player.yaml) adds a `player`
service on the same Compose network as `goby`. The release overlay is image-only:
it does not build or pull. Load the verified archive first and set its immutable
image ID. The operations helper's `prepare --with-player` generates these
settings, including a default host port of `8080`; direct Compose use requires
an explicit port:

```powershell
$env:GOBY_PLAYER_HOST_PORT = '8080'
$env:GOBY_PLAYER_IMAGE = 'sha256:7140ce5c531a302b0aca595ee47ce98c52cd08ed73d5a00e5a27972d1ad94192'
docker compose --env-file /path/to/compose.env -f deploy/oci/compose.yaml -f deploy/oci/compose.player.yaml up -d
```

Replace `/path/to/compose.env` with the existing private Compose environment
file. Select the image ID from the accepted catalog for the intended deployment;
the example above names the October 5 archive, whose runtime status is recorded
separately. For AMD, insert `-f deploy/oci/compose.amd.yaml` before the player
extension. Open `http://127.0.0.1:8080`; the administrator dashboard is available
at `/admin/` on the same origin. `GOBY_PLAYER_API_UPSTREAM` overrides its runtime
backend origin.

Set `GOBY_PUBLIC_URL` in the backend's private application environment file to
the actual external player origin, including any nondefault port. Administrator
origin validation and advertised server URLs use that value. For an isolated
local HTTP deployment it is `http://127.0.0.1:8080` with
`GOBY_COOKIE_SECURE=false`; for a production HTTPS origin use its HTTPS URL and
keep `GOBY_COOKIE_SECURE=true`.

To generate background clips, audio waveforms, or bitmap subtitle timelines,
add the independent
[`compose.background-previews.yaml`](../../deploy/oci/compose.background-previews.yaml)
overlay after the base/AMD files. It changes only the existing `/media` bind to
writable; the default Compose file and container root remain read-only:

```powershell
docker compose --env-file /path/to/compose.env -f deploy/oci/compose.yaml -f deploy/oci/compose.background-previews.yaml -f deploy/oci/compose.player.yaml up -d
```

The host's approved source directories must allow backend UID/GID `10001:10001`
to create the selected `backdrops/goby/<source-filename-sha256>/`,
`backdrops/goby-waveforms/<source-filename-sha256>/`, and/or
`backdrops/goby-subtitle-timelines/<source-filename-sha256>/` trees.
The key hashes the exact source basename, including its extension. Private
namespace directories use `0700` and files `0600`; a newly created shared
`backdrops` parent is `0755`.
Generated files stay beside their source, outside the disposable analysis cache.
Enable each generation option independently per library or explicitly queue an
item. All three default to false and are independent of BIF seek previews.
The overlay grants filesystem access but does not enable generation by itself.

Existing completed clips remain readable when generation is disabled or its
profile/source changes. Waveform and subtitle timeline files also persist, but
obsolete source axes are not served. Only explicit Force regeneration replaces
an existing artifact; a failed or canceled generation preserves its previous
publication. Include sidecars when moving or backing up source folders. See the
[operator instructions](../../deploy/oci/README.md) for permissions and the
[background](../../docs/api/background-previews.md),
[waveform](../../docs/api/audio-waveforms.md), and
[subtitle timeline](../../docs/api/subtitle-timelines.md) contracts for durable
task behavior.

The new listener binds to host loopback by default. A production HTTPS edge
proxy can forward this origin, including streaming and WebSocket traffic; its
trusted forwarding configuration, TLS and externally advertised URLs must be
configured together with Goby. The October 5 packaging increment adds a player
archive and operations-helper integration. Release `2026-10-05-player-media`
passed its frozen-image software and AMD journeys; the identities and results
are recorded in the [release record](../../docs/development/player-release-20261005.md).
The isolated real-backend Docker playback run passed six phases, and the
subsequent capability run passed eight phases, as recorded in
[ACCEPTANCE.md](ACCEPTANCE.md). The later persistent-background run passed
12 phases, including automatic/manual generation, reuse, retained files after
profile/source changes, failed/canceled regeneration, and explicit replacement.
These establish the selected deployment/media and capability paths at their
original sources. They do not replace frozen-image release acceptance or imply
a public production rollout.
