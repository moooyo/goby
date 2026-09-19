# Selected compatibility phase 1 browser acceptance

Status: revised source awaiting its next admitted remote run. The retained r02
attempt passed the three native administrator stages and then failed before any
original-client authentication response was observed. Its fixture had no
`/web/` route; the product's administrator `WebDirectory` fallback could not
serve that entry point. The failed receipts remain unchanged. The revised
fixture admits the complete original asset host explicitly and reports entry
document and asset failures separately from credential failures.

The retained r03 attempt confirmed that the index, loader, and logo were
proxied successfully with complete matching hashes. Startup then waited before
loading the application because Playwright's blocked-service-worker mode
resolved `register()` without registering a worker, while the original loader
waited for `navigator.serviceWorker.ready`. The revised original-client context
allows its real worker; the native administrator context still blocks workers.

The retained r04 attempt registered the real worker and observed `ready` while
its active worker was still `activating`. The immediate `activated` assertion
was a fixture timing error. The revised observer keeps the final `activated`
requirement and waits for the actual `statechange` event within one ten-second
deadline. It records the observed states and distinguishes activation,
redundancy, a missing active worker, readiness rejection, and timeout. No worker
state, registration result, or readiness promise is replaced.

The retained r05 attempt passed real local-password sign-in and its database
check. Its same-tab reload did not show a PIN prompt because the original Web
router retains PIN validation in `sessionStorage`. The revised journey closes
that tab and opens a new tab without an opener in the same browser context.
This naturally preserves the saved token and device preference in
`localStorage`, while starting fresh per-tab storage. All observers are attached
to the new page, and no storage values are edited. The two denied requests in
the old r05 report remain unclassified because that report did not retain their
addresses or intercepting transport.

A context file, saved preference, returned
chapter interval, screenshot, or successful media response is not a passing
browser journey. The coordinator must freeze the integrated phase 1 source and
admit the entire verification run before executing this harness.

`TestSelectedCompatibilityPhase1BrowserIntegration` owns a disposable PostgreSQL
schema, real generated media, the application listener, application restart,
browser, accounts, and cleanup. Its browser driver is
`scripts/test-env/selected-compatibility-phase1-browser.mjs`. Native administrator
actions use the embedded application. Playback and local-password sign-in use
the unmodified retained Emby Web application. Application responses are not
mocked, and the driver does not call private player methods, alter media time,
inject seek/end events, or install a replacement player.

## Remote resources

Read-only SSH inventory on September 20, 2026 confirmed these paths. This was
file and resource inventory, not a runtime readiness check:

| Resource | Path or observation |
| --- | --- |
| Host | `ssh test-env`, reachable |
| Node | `/root/.cache/agentic-review-toolchain/node-v24.20.0-linux-x64/bin/node` |
| Playwright 1.63.0 | `/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright/index.js` |
| Playwright CLI | `/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright/cli.js` |
| Browser cache | `/root/.cache/ms-playwright` |
| Chromium | `/root/.cache/ms-playwright/chromium-1243/chrome-linux64/chrome` |
| Headless shell | `/root/.cache/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-linux64/chrome-headless-shell` |
| Required Go toolchain | `/opt/goby-toolchains/go1.27.1/bin/go`, present with executable permissions |
| Lower-version Go | `/usr/local/go/bin/go`; static `VERSION` says `go1.27.0` |
| Default Go | `/usr/lib/go-1.26/bin/go`; static `VERSION` says `go1.26.7` |
| Media tools | `/usr/bin/ffmpeg`, `/usr/bin/ffprobe` |
| Accepted patched media prefix | `/opt/goby-amd-media-20260919-50f45177f297/toolchains/ffmpeg-9.0.1-goby-cb8b6d298456` |
| PostgreSQL client | `/usr/lib/postgresql/17/bin/psql` |
| PostgreSQL 17 tools | `/usr/lib/postgresql/17/bin/{initdb,pg_ctl,postgres,psql,createdb,pg_isready}` |
| Existing worker accounts | `goby` UID 995/GID 986; `postgres` UID 103/GID 106 |
| Capacity | Approximately 31 GiB root free; 1.1 GiB `/tmp` free; 4.5 GiB available memory; no swap |

The retained Playwright `browsers.json` selects Chromium revision `1243`, which
matches the existing cache. Do not download another browser or silently use a
different Go/media executable. The coordinator selects and records the actual
admitted toolchain hashes and versions when verification begins. Prefer an
owned directory below `/opt/goby-test` for media and build scratch space because
`/tmp` capacity is limited. Do not read, truncate, or reuse historical fixture
databases, credentials, processes, or evidence directories.

The current `go.mod` requires Go `1.27.1`; neither lower-version Go file is an
admitted substitute. The patched FFmpeg prefix and its `bin` and `lib` ancestors
are root-only. So are `/root` and `/opt/goby-test`, which contain the retained
browser and module caches. A non-root worker needs a new owned runtime copy with
the complete dependent-library closure; do not change historical artifact
permissions to make them accessible. This particular browser fixture requires
root, while a private PostgreSQL server must run as its owned non-root account.

The exact retained client root is:

```text
/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server
```

The original client assets are served only by the owned acceptance host. Goby's
`WebDirectory` configures the administrator asset fallback and does not mount
`/web/`. The original client's `/web/modules/apphost.js` and processed index are
generated by its original host and are absent from the raw package directory;
the raw directory alone is not a complete runnable client asset set. The
acceptance host must preserve the original asset bytes and route every business
API and WebSocket request to the actual Goby application.

The retained official host was inspected without restarting or changing it.
It remains `goby-core-av-original-client-01.service`, PID `366598`, process start
ticks `506485`, network namespace `net:[4026532544]`, with its listener on port
`28497` inside that namespace. The public server identity is
`9b45875a4abd417fb19ef7f71ad81e7a`, version `4.9.5.0`. Authorized read-only HTTP
returned these complete identity-encoded assets:

| Original response | Bytes | SHA-256 |
| --- | --- | --- |
| `/web/index.html` | 15101 | `dd1664b5b50260e3dc6d7bf82951d8230b37591412d726535eb1714369f1bd40` |
| `/web/modules/apphost.js` | 10540 | `11cb7865e7e09be7e4c89d963245dc7142a496f68c1b000b6d6edd61ca0fd9b8` |

The read-only inspection preserved its process, boot, executable and namespace
identity. These observations are admission inputs, not an authorization to stop,
restart, reconfigure, or use any prior host's accounts or media. The fixture
requires an explicit private host configuration and verifies that identity
again when it runs. Its only upstream routes are GET/HEAD requests below
`/web/`; license, user, catalog, playback, and WebSocket routes remain Goby's.
The namespace transport needs a root worker with permission to enter the
retained host's network namespace. Complete asset hashes are retained without
persisting proprietary response bodies.

The package is not copied into this repository, browser results, build assets,
or a redistribution archive. Record its asset inventory and hash alongside the
frozen product source. The observed client version is 4.9.5.0; a different asset
tree requires a new contract review.

## Admission and execution

The current task authorizes local compilation and unit tests. Actual integration,
media execution, and browser acceptance run only on `ssh test-env`, after all
phase code is ready for unified verification. All remote commands below belong
to the coordinator's single admitted verification slot.
Set up the owned PostgreSQL role/database and private environment with the
existing test-environment workflow; the browser test creates its own schema.
Never point `GOBY_TEST_DATABASE_URL` at an existing client acceptance database.

This browser fixture intentionally requires a root-owned test scope because its
retained assets, private contexts, source-file checks, and process observers
assume that ownership. Its success does not establish a non-root deployment
profile. Ordinary integration checks may use an independently owned non-root
worker where suitable.

Build the administrator assets from the frozen source first. Verify that the
embedded assets and frozen `web/admin/dist` match before browser acceptance.
The root's broader phase gate also owns affected ordinary tests, migration
catalog generation, ordinary/embedded builds, and recovery tests.

The remote shell contract is:

```sh
export GOBY_SELECTED_PHASE1_RUN_ID="selected-phase1-<unique-run>"
export GOBY_TEST_BROWSER_NODE=/root/.cache/agentic-review-toolchain/node-v24.20.0-linux-x64/bin/node
export GOBY_TEST_PLAYWRIGHT_MODULE=/opt/goby-test/inactive-dependencies-m5h/node_modules/playwright/index.js
export PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright
export GOBY_TEST_BROWSER_ARTIFACTS_DIR="<owned-0700-artifact-parent>"
export GOBY_SELECTED_PHASE1_CLIENT_HOST_CONFIG="<private-root-owned-original-host.json>"
export GOBY_FFMPEG="<admitted-ffmpeg>"
export GOBY_FFPROBE="<admitted-ffprobe>"
export GOBY_TEST_DATABASE_URL="<private-owned-integration-database>"
"<admitted-go>" test -count=1 -timeout=12m \
  -tags=goby_embed_admin,goby_browser_integration \
  ./internal/server -run '^TestSelectedCompatibilityPhase1BrowserIntegration$' -v
```

The placeholders are intentionally not runnable defaults. The coordinator uses
the private environment file instead of putting the database URL in command
history, process arguments, task messages, or committed documentation. Run the
shell inside `ssh test-env` from the frozen remote source directory. The driver
is launched by Go and is not a standalone provisioner.

The original-host configuration is a canonical root-owned `0600` regular file
with this explicitly inspected identity. The fixture rejects another epoch or
host instead of silently choosing a service:

```json
{
  "Marker": "goby-selected-phase1-client-host-v1",
  "Origin": "http://127.0.0.1:28497",
  "PID": 366598,
  "StartTicks": 506485,
  "BootId": "4de83999-7586-4716-83d1-0d81c9343126",
  "NetworkNamespace": "net:[4026532544]",
  "Executable": "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/EmbyServer",
  "ExecutableSHA256": "c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2",
  "ServerId": "9b45875a4abd417fb19ef7f71ad81e7a",
  "Version": "4.9.5.0"
}
```

`asset-hashes.json` retains the original host binding and each proxied response's
method, path, MIME type, status, byte count, and SHA-256. Its `Complete` field
means that the upstream response reached EOF within bounds while the host
identity remained pinned; it does not independently prove that the browser
consumed the resource. Browser workflow and media evidence remain required.

## Private context and evidence

Go writes `private-context.json` with mode `0600` in a new owned `0700` output
directory. `GOBY_SELECTED_PHASE1_CONTEXT` gives its path, and the separately
provided `GOBY_SELECTED_PHASE1_RUN_ID` must match `RunId`. The marker is
`goby-selected-phase1-browser-fixture-v1`. The context includes the one owned
loopback `BaseURL`, real server/user/item/library IDs, fixture names, disposable
normal/local passwords and profile PIN, and the bounded output paths.

Only the private input contains those passwords and PIN. The driver disables
tracing, records no authentication response bodies, and permits requests only
to the fixture's `127.0.0.1` origin. The original client requires a real Service
Worker to complete startup; the driver observes its actual ready registration,
scope, active state, and script path without modifying registration or readiness.
A deny-only HTTP/CONNECT proxy covers each browser context, including worker
traffic that page routing cannot intercept. Its exact-origin bypass permits
only the fixture's HTTP/WebSocket authority. It opens no upstream connection for
denied traffic and proves its sockets and listener closed after browser exit.
Foreign HTTP or
WebSocket requests are denied and make the full journey fail. It never supplies
a synthetic success response to a denied request. Result files retain safe
scenario names, media event facts, HTTP status, fixture item IDs, and hashed
play-session identities. Raw errors, token-bearing URLs, browser storage,
headers, and proprietary source are not copied into the report.

HTTP and WebSocket guards use the same exact host and port check, mapping only
`ws` to `http` and `wss` to `https`. Denied requests retain a bounded scheme,
hostname, port, safe path class, phase, and intercepting transport, never a
query string or user-info value. This distinguishes a genuine external request
from a guard classification error without rewriting historical evidence.

Original-client entry diagnostics retain the navigation HTTP status, final path
without query parameters, MIME type, bounded failed asset paths/statuses,
request-failure paths, and coarse console-error categories. Each sign-in records
the current safe operation name. A missing or non-HTML entry document fails
immediately, before a login-control timeout, without retaining credentials or
raw browser exceptions.

Context creation, observer installation, page creation, proxy-sensitive
navigation, and worker readiness each have their own safe operation name.
Known proxy, closed-target, missing-executable, network, and timeout errors map
to fixed cause labels; alternate-control failures retain those labels for each
failed alternative without copying raw exception messages or credential data.

Coarse warning categories distinguish Playwright's blocked-worker warning from
other warnings. `ServiceWorkerReady` describes the actual activated registration;
`ControlsCurrentPage` may initially be false because readiness does not require
the first page to have been claimed by that worker.

The native screenshots show the open intro dialog after its saved state is
visible, and the local-credential dialog before any secret fields are filled.
CSS animations are disabled for these native snapshots so a modal fade does not
obscure the final UI. Media playback time is not changed.
Screenshots supplement the real UI mutations and independent database checks;
they do not establish acceptance on their own.

An original-client named-operation failure also attempts one private diagnostic
screenshot of that same owned `/web/` page. It masks inputs, textareas, editable
content, and any visible fixture secret text. Capture is limited to 2.5 seconds
and 8 MiB, with a one-second abortable file write to a new `0600` file. The result
records the filename or a safe unavailability reason. Screenshot failure cannot
replace the original operation error or prevent browser and proxy cleanup.
The same named-operation diagnostics cover PIN entry, item navigation, playback,
next-episode behavior, and sign-out. They preserve the innermost error and the
last bounded set of credential-free playback reports.

For each ordered stage the browser writes `stage-<phase>-request.json` with
`{RunId, Phase, State:"complete"}`. Go independently checks its database and source files, or
performs the owned restart, then writes `stage-<phase>-database.json` with:

```json
{
  "Marker": "goby-selected-phase1-stage-database-v1",
  "RunId": "<this-run>",
  "Phase": "<this-stage>",
  "Complete": true,
  "SourcesUnchanged": true
}
```

A stage is incomplete without this acknowledgement. Terminal play-session
history is retained as evidence; session cleanup means no active authentication
sessions, live playback sessions, encoding workers, or listeners remain, not
that history has been deleted. Fallback cleanup is reported separately from a
successful UI sign-out. Go independently checks owned schema/media cleanup and
removes the private context after browser shutdown.

An observed original-client intro registration dependency may instead write
`State:"blocked", Reason:"original_client_entitlement"`. Only the ShowButton and
AutoSkip stages admit this disposition. Go still requires the actual movie
playback lifecycle, current preference, and unchanged source, then writes
`Complete:false, Observed:true, Blocked:true`. The browser continues independent
stages and cleanup, preserving the blocked stage and false feature check.
Neither the browser nor the Go driver can report complete acceptance in this
case.

## Real consumer requirements

The native administrator workflow saves a separate local password and a
four-digit profile PIN, enables local-password sign-in, saves intro/next-episode
preferences, rejects an inverted intro interval, imports a reviewed JSON
interval, resets it, and creates a manual interval from three to eight seconds.
The movie has no automatic markers, so its later
skip behavior depends on the saved manual interval. The two real episodes
contain explicit `IntroStart` and `IntroEnd` chapters at those same positions;
ordinary chapter presence is not accepted as an intro.

Local-password sign-in must succeed through the original client's form. The
profile PIN is a post-authentication device profile lock, not another server
sign-in credential. PIN acceptance must use the actual original-client lock
prompt, reject a wrong PIN, and accept the saved PIN. A successful fresh password
login normally validates the profile and therefore does not itself prove a PIN
prompt. After that login, the original router asks whether to prompt for a PIN
when returning to the app. The driver clicks Yes in that actual confirmation,
closes the current tab, opens a new tab without an opener in the same context,
then enters a wrong and correct value in the original four input fields.
The new tab restores the real saved session and starts fresh per-tab PIN
validation. Reloading the same tab would preserve that validation. No new
authentication request may replace the PIN check. Subsequent fresh contexts
select No in the same device prompt.
It is not acceptable to write a fake device preference, invoke an
internal client function, or compare the PIN in harness code instead.

For `ShowButton`, the real `.btnSkipIntro` becomes visible inside the interval
and a browser click produces exactly one completed media seek to eight seconds.
For `None`, playback traverses the interval without that button or seek. For
`AutoSkip`, the original application performs the seek without a driver click.
All cases require decoded frame advancement, a real started report, a real
stopped report, and the matching database state. The driver observes platform
media events without assigning `currentTime` or `playbackRate`.

The original client detail route needs `serverId`. Its
`.btnPlay.btnMainPlay[data-mode="play"]` explicitly starts from zero. A resume
button is not interchangeable. The playback manager constructs its episode
queue using `Shows/{SeriesId}/Episodes`; it owns automatic transition after the
actual media `ended` event. The enabled journey requires the second episode to
start once with a distinct play-session identity, decode frames, and finish
without another automatic start. The disabled journey requires the first
episode to finish without starting the next. An explicit user next action may
still work when autoplay is disabled and is a separate backend/client case.

Short fixtures do not satisfy the original client's five-minute threshold for
an advance next-episode preview card. The absence of that preview is not a
failure. A visible Stop control is preferred for cleanup; Back is accepted only
when a real stopped report and paused media prove it actually stopped playback.

The application restarts on the same owned origin, after which the browser
refetches the saved settings and signs in with the local password again. The
final administrator action clears local credentials and disables that sign-in
path. Backend tests separately prove old credentials and revoked tokens fail;
clearing a field on screen alone does not establish revocation.

The fourteen ordered stages are `admin-credentials`, `admin-preferences`,
`admin-intro`, `original-local-login`, `profile-pin`, `next-enabled`,
`next-disabled`, `intro-show-button`, `intro-none`, `intro-auto-skip`, `restart`,
`persisted`, `credentials-cleared`, and `cleanup`. Next-episode cases select
`IntroSkipMode=None` to isolate transition behavior from intro licensing.
The final durable preferences are `AutoSkip` and next-episode autoplay disabled.

## Server licensing and original-client dependency

Goby's selected policy has no Premiere feature tier. Its own relevant license
compatibility APIs report the supported service as authorized while retaining
normal account, library, and playback authorization. Those real APIs and their
actual consumer path belong to the integrated source under test.

Static inspection found that the original video OSD's intro action calls
`validateFeature("dvr", {viewOnly: true})`. Its registration path can POST to
`https://mb3admin.com/admin/service/registration/validateDevice` from a clean
device profile. Returning a local `/Registrations/dvr` DTO does not satisfy that
path. The retained client's independent external registration dependency can
therefore block the real intro consumer even when Goby's license and chapter
APIs work correctly.

This harness deliberately does not manufacture a supporter state, forge a
registration response, preload a fake successful registration cache, or bypass
the client gate. A denied foreign request or missing actual seek is retained
as a blocked client dependency. The coordinator must verify the integrated
server licensing adapter and distinguish its actual behavior from this fixed
external client dependency. An intercepted external response alone cannot
establish production end-to-end success. A browser's custom HLS engine check or
a DTO test does not replace missing original intro behavior.

## Coverage and closeout

`browser-result.json` has marker `goby-selected-phase1-browser-result-v1`.
`Complete` is true only after every declared check, ordered database stage,
successful cleanup acknowledgement, and zero browser/foreign-request errors.
Failures retain the last phase and a credential-free error code. The coordinator
must also require the Go driver result to prove owned cleanup; browser success
alone is insufficient.

This browser profile covers actual direct playback of small H.264/AAC MP4
fixtures. Its observed `PlayMethod` is retained; it does not claim every
direct/remux/transcode/HLS profile. The phase's separate media and integration
checks own alternative playback methods, resume and explicit-start boundaries,
wrong/empty credentials, trusted-local rules and spoofed proxy input, isolation,
rate limits, stale revisions, source replacement, permission revocation,
unavailable next episodes, migration, and backup/restore. Record their actual
test names and results with the final frozen source rather than marking them
passed from this document.

The prepared affected test inventory includes
`TestStoreLocalPasswordAuthenticationIsolationThrottleAndRevocation`,
`TestStoreProfilePinEncryptionOwnerProjectionAndAtomicPatch`,
`TestExplicitChapterIntro`, `TestStoreIntroLifecycle`,
`TestAdminIntroMarkersLifecycle`,
`TestHTTPOriginalClientEpisodeQueueIsCompleteAndDoesNotOwnAutoplay`,
`TestEpisodePlaybackQueueRetainsFullOrderedReplayableSeriesAndCurrentAuthority`,
`TestEpisodePlaybackQueueRejectsOversizedSeriesWithoutTruncating`,
`TestPlaybackSourceProjectsTheSameIntroMarkersAsTheItem`, and
`TestPersistedResumePreferenceOnlyChangesImplicitPlaybackStarts`. This list is
an execution input, not a result; reconcile names with the frozen source and
include the completed migration/recovery and account HTTP cases.

The final closeout retains source/asset/tool/client identities, all failed
attempts and their causes, successful consumer results, and independent owned
cleanup evidence. No verification or phase-completion claim is made by adding
these harness files.
