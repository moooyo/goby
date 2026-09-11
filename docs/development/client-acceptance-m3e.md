# M3e real-client acceptance

Status: **source32 is published; the original Movie dual-user flow and precise
candidate media-root extension passed. Nonempty positive-extra acceptance is next.**
The [product checkpoint](source32-product-publication.json) is `b9bb7b1`.
Its [complete remote regression and build](m3e-source32-extras-full.json) passed
1,873 top-level race tests across 24 packages with zero failures/skips, all six
cleanup checks true and unit exit code 0. The current source32/schema27 candidate
is PID748513/start ticks6996875, binary SHA-256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
Its [schema26-to27 upgrade](m3e-source32-candidate-upgrade.json) retains original
PID746709/start ticks6930051 in the immutable upgrade receipt; the later
extension owns the new process. The candidate still has 13 items, three
libraries, 35 tables and two empty Extra tables. This is not a completed M3
milestone. Remaining M3/M4/M5/M6 requirements are active; M7 remains deferred.
All builds and verification execute through `ssh test-env`.

The [actual input07 original-Movie run](m3e-source32-cross-user-original-movie.json)
passed for both ordinary users, public SHA-256
`9f50d54cb291f4dfc24009fdb0084e6af06a443b2a353c16f02b7f337f95029a`.
Each completed Home-to-Movie-to-Home, exactly one genuine PlaybackInfo200 with
finished transfer, complete SpecialFeatures200 `[]`, and zero page errors.
Own200/foreign403, four unchanged item-UserData projections, preferences,
Configuration and Policy, UI logout204 and exact-token401 all passed. Each
WebSocket opened and closed once, with zero active sockets and no cleanup
failures. Each user retained one blocked-resource console warning/error; the
result does not claim a console free of errors. Input07's 114 pure guards/two
syntax checks and six live Music lineage guards are separate supporting evidence.

The [scoped database comparison](m3e-source32-cross-user-original-movie-comparison.json)
passed, SHA-256 `0a2a1e54a95a52e15e8f4a037914ac05dc760ca561d40d5b3d5f53bcea196898`.
Recorded play rows increased from 22 to 24 and selected A/B auth rows from 51 to 53. The
two eligible old Prepared rows became Expired; two new Prepared rows and two
new auth rows were added, and both new auth rows were revoked. Five UserData
rows stayed unchanged, with zero references and encoding jobs. This proves
the declared preparation scope, not whole-database preservation. Every used
scope and before image is historical; a later run needs a fresh baseline.

The [exact root extension](m3e-source32-extra-root-extension.json) then passed,
SHA-256 `09bf775ff6582b0a3dbc6c52026b4da7f0ba59066884c08a4c1cea4d3ba1391e`.
It appended only `/opt/goby-fixtures/client-special-features-m3e-v1/Movies`,
preserving all 35 table rows/sequences, credentials, recovery state and media.
It performed zero HTTP calls, library creation or scans. The [first preflight failure](m3e-source32-extra-root-preflight-failed.json)
remains separate: tool01 and its mock used `encoding_states` instead of actual
`encoding_jobs`, and stopped before output-directory creation with no
state/environment/service change. Tool02 fixed the name, added an actual
catalog regression, and passed two syntax checks, 11 pure guards, preflight
and execution. Tool01 and all original receipts remain unchanged.
See [extras verification](verification-m3e-extras.md) for the exact tool,
completion-receipt, current fixture-state and runtime hashes.

Next, independently set up the nonempty positive candidate fixture and verify
its original-client flow. The empty-array Movie pass and added root permission
do not establish indexed extra delivery. Primary schema27, library restriction
and complete compatibility remain open; completed media/reference phases must
not be replayed.

Current main deployment: [source28/schema26](m3e-main-schema26-completed.json),
active/running as PID688833/start ticks5620918, binary SHA-256
`83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae`.
The independent 13-call native completion passed with one new owned session,
logout204/session401 and no additional service writes, migrations or restores.
The historical source28 isolated candidate was PID682417/start ticks5168373 with the
same binary. Full source28 regression passed 1,830 tests across 24 packages, and
its scoped Similar/ThemeMedia/album/Home flow passed. The product checkpoint is
`608e2088`; the main-deployment/evidence checkpoint `53144e6` is pushed to
`origin/main`. Neither checkpoint establishes complete client acceptance.

The historical [input05 dual-user preparation run](m3e-source28-cross-user-preparation-01.json),
public summary SHA-256
`91a8d3bc7a2e604cd62f5041b7a9403463baacb88c53901649c11a52f8b7295a`,
passed 100 pure guards and the explicitly selected `acceptance-preparation`
scope. Both users completed real UI Home to Movie detail, actual PlaybackInfo200,
return Home and UI logout204/exact-token401. Own/foreign checks and four UserData
projections, preferences, Configuration and Policy comparisons passed.
However, each user recorded one unclassified `ui_movie` page error. The driver
did not reject page errors; its passed result proves only the recorded flow and
state scope. **Complete client acceptance remains open.** The later
[input06 diagnostic](m3e-source28-special-features-blocker.json) enforced strict
page-error rejection and failed on each user's SpecialFeatures404/`Response`
error. That historical failure led to source32's scoped correction above;
neither input05 nor input06 is relabeled as a successful complete client run.

The private before/after comparison retained 18 old play rows: 17 unchanged,
with one eligible B row Expired. A's single reference to revoked authentication
was removed. Two new Prepared rows were added for two new Emby authentication
rows, both revoked after logout. All 47 old auth rows and five UserData rows
were unchanged; encoding remained zero. That historical result was 20 plays,
49 auth rows and zero references. Input06 consumed that 20/49 baseline and
reached 22 plays/51 auth rows; input07 subsequently consumed 22/51 and reached
24 plays/53 selected A/B auth rows under its separate scope. The 18/47, 20/49
and 22/51 scopes and before images are historical and **must not be reused**
as new authority. A future run requires a fresh private snapshot and comparator
scope. The [library restriction matrix](m3e-library-restriction-plan.md) has
not executed. Raw reports and all historical successes/failures remain preserved.

Historical isolated candidate: [source20/schema25](verification-m3e-source20-similar.md),
binary SHA-256 `a5028f865d3638f020c758a77bf1d431ca755699b7767a25b711509a8f8c12a9`,
PID 581075/start ticks 3900536. Its complete 1,763-test race suite and 30-table
same-schema preservation passed. The original client verified its owned Similar
200 response and completed transfer with unchanged item state and exact-token
logout proof. The full auxiliary flow remains failed at ThemeMedia 404 and did
not return Home. The primary service then remained source18. This was a partial
candidate checkpoint, not completed M3 acceptance or a new primary deployment.

Previous isolated candidate checkpoint: source18, schema 25, executable SHA-256
`665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b`,
PID 506532/start ticks 2681316. [19 targeted regressions, build and protected
schema25 replacement](m3e-source18-targeted-upgrade.json) passed, preserving all
30 tables and the existing music metadata, user data and preferences. The
earlier controlled [music scan](m3e-music-scan.json) passed with original media
preserved. Source18 adds a narrowly scoped source-ID alias for owned correlated
Audio reports. Its [actual MP3 and FLAC core journeys](verification-m3e-source18-audio.md)
passed with eight successful state reports each, persistent user data, stop and
UI logout/token rejection. MP3 retains a known return_home uniqueness-check
failure despite the saved Home DOM; FLAC completed the calibrated full flow and
actual Home route. Similar/ThemeMedia 404 responses and page errors remain
unresolved. Both sessions are closed. [Full source18 regression](m3e-source18-full.json)
passed 1,741 top-level race tests across 24 packages with no failures or skips.

The historical [main source18 deployment](m3e-source18-main-deployment.json) passed.
It ran schema25 and the same source18 binary, PID539535/start ticks3115871,
before the current source28 deployment. The source20 candidate PID581075/start
ticks3900536 was also later replaced by source28. The
deployment preserved all old business columns and sequences through the
29-to-30-table schema25 migration and retained old archives.
Health, readiness, administration, login and seven reads passed, followed by
logout HTTP 204 and exact-token HTTP 401. The empty transcode cache's seven
independent guards and actual creation also passed. No main restore or old
rollback was performed. The deployment evidence SHA-256 is
`02ed027b353488ab31cb9e4ac3e7cfc4547422bb1a57e7f9cfdfdd945aad0bf3`.
The [partial checkpoint record](verification-m3e-source18-checkpoint.md) records
the deployed product and its verification evidence together.

The first
[two preparation attempts](m3e-schema25-preparation-failed-01-02.json) failed
safely before Go helper execution, dump, rehearsal or migration and remain
historical evidence. Tool03 subsequently passed 47 remote guards and its build;
the [fresh preparation](m3e-schema25-preparation-source18.json) then passed a
same-snapshot schema23 dump, independent schema23-to25 restore rehearsal and
cleanup while preserving the exact old main state. The retained `prepared.json`
is under `run-20260911T062405Z-975ef4c2e0c2b3078d517dc5` in
`/opt/goby-test/backups/client-schema25-v1`, SHA-256
`b1d686b1c8aed83aa545dd02618c22793cc8b371f6659a0adc1f40710ddfd0ce`.
That preparation preceded the successful main deployment above. Both earlier
failed preparation attempts remain retained and are not resumed or relabeled.

The following earlier candidate checkpoints are historical evidence. The
[source16 audio UI evidence](verification-m3e-source16-audio.md) showed MP3/FLAC
media HTTP 206, advancement, pause, both seeks and resume, but its
Playing/Progress/Stopped returned HTTP 404 and playback history was unchanged.
Both UI logouts and exact-token rejection passed. [Full-source16 regression](m3e-source16-full.json)
passed 1,739 top-level race tests across 24 packages with no failures or skips,
a build matching the installed candidate and complete owned cleanup.
The [partial diagnostic](verification-m3e-source16-audio-report-diagnostic.md)
confirmed the bare item ID in MediaSourceId with matching nonce, item, token
and device. ErrorCode body collection remained incomplete. Source17's new
test-stage assertion failed after 18 other tests passed; source18 corrected
that boundary without changing production behavior. Source18 core audio
acceptance and full-source regression passed as scoped above. The historical full
source15 attempt [finished with 1,722 passes and two failures](m3e-source15-full-failed.json),
with no skips. Two old entity-projection expectations omit the new empty music
collections; source16 corrected their explicit expectations. The source15 final
recoverydb package and full build did not run. The earlier [full source12 run](m3e-source12-full.json)
passed 1,693 race tests across all 24 packages with no failures or skips, built
the exact candidate bytes, and completed all owned cleanup. Source15 subtitle
acceptance passed; its then-open music playback failures were superseded by the
source18 core audio evidence above. Source11's earlier
[26 profile/NextUp regressions](m3e-profile-nextup-source11-tests.json) and build
passed. The [full source11 attempt](m3e-source11-full-failed.json) failed one
legacy device assertion after 1,668 top-level passes; the last recoverydb package
and full build did not run. The malformed-query assertion is corrected in
source12 inputs. Its [reviewed empty test pair was removed](m3e-source11-full-disposal.json),
preserving all failure evidence. The earlier source09
[two endpoint regressions](m3e-endpoint-source09-tests.json) and complete
`backuppg`/`recovery` package run of [70 tests](m3e-source09-backup-recovery-tests.json)
passed. The updated [backup runner passed 45 guards](m3e-backup-runner-guards.json).
These scoped counts are not added together or called full repository acceptance.

The source11 original-client run completed the [core movie workflow](m3e-goby-movie-client.json):
advancing 320x180 video frames, pause, UI seeks to 178.355 and 118.236 seconds,
stop, logout/login and resume near 119.75 seconds. All eleven actual playback
reports returned 204, and both logout tokens were independently rejected with
401. The [durable readback](m3e-goby-movie-durable-state.json) confirms play count
2, position 122.860455 seconds and stopped counted sessions. An unstarted
prepared record remains bound to a revoked credential; it is not a live player.
SpecialFeatures, ThemeMedia, Intros and other item auxiliary reads still return
404, with eight `Response` and four `undefined` page errors retained. This is
scoped core playback evidence, not an error-free or complete compatible client.

The [original rejected DeviceProfile](m3e-client-profile-rejection.json) exposed
string-valued `MinSegments` and `IsRequired`; the old decoder also ignored
`MaxStaticBitrate`. The [complete safe profile](m3e-original-web-playback-profile.json)
now drives a dedicated bounded adapter and HTTP regression. Source11 introduced
unknown-field rejection, which also rejected the retained successful reference
requests containing `VideoStreamIndex: 0`. Source12 restores the previous behavior
of ignoring unmodeled extensions after the complete JSON tree passes duplicate,
Unicode and resource checks. Only the two observed scalar fields receive string
conversion. Unmodeled fields never participate in authorization or stream
selection; nonzero `VideoStreamIndex` selection remains unsupported. Other routes
retain their prior decoder behavior. Static bitrate intersects original-file
limits as a documented Goby policy; its precedence is not an observed reference matrix. The retained
[source10 failure](m3e-profile-source10-failed.json) was a canonical-JSON test
comparing explicit empty slices with omitted slices. Source11 corrects that
expectation without changing the production decoder; source10 was not deployed.

A real [display preference workflow](m3e-goby-preferences-client.json)
changed Genre display limit from 1 to 2 and back to 1 with two HTTP 204 writes,
then UI logout 204 and an independent exact-token HTTP 401 check. Optional user-menu
discovery still returns 404. The explicit default key remains persisted; this
does not claim restoration of the initial empty dictionary.
The subsequent [process-restart gate](m3e-preference-process-restart.json) used
the UI to retain the nondefault value 2, replaced the candidate process through
the preservation-checked upgrade, then verified 2 in a fresh browser and restored
1 through the UI. It proves process replacement, not a host reboot.

The [reference AV checkpoint](verification-m3e-reference-av.md) covers original
MP3/FLAC and external SRT/VTT flows on an independent reference account. The
Goby AV/TV runs use a new [receipted ordinary account](m3e-goby-av-user.json);
[27 operator guards](m3e-add-viewer-guards.json), actual creation and inspection
passed while preserving the original users and data. Before the controlled Music
scan, Goby used the album label `Music` and two same-title `M3e Client Audio`
leaves instead of the reference tag titles. The completed scan now supplies
`M3e Synthetic Album`, `M3e MP3` and `M3e FLAC` with the same physical IDs and paths;
source18 UI evidence binds the selected track independently of its display name.

The [source11 AV/TV runs](verification-m3e-goby-av-source11.md) finished with
three recorded blockers: a blank music album page after an undefined-array
exception, identical `en` subtitle labels, and repeated Specials in TV browsing.
All owned sessions were closed, including separately labelled API cleanup where
UI logout was blocked. These runs are not playback acceptance. The reference
[TV browse-only flow](verification-m3e-reference-tv.md) passed while retaining
the original client's cross-season episode list. Source12 installed the tested
music collection/count, subtitle-label and [TV query-filter](client-tv-query-filters.md)
fixes. The [fresh source12 TV flow](verification-m3e-goby-source12-tv.md) completed
over the ordinary-only fixture, with auxiliary route/page errors retained.
On source12, music still reached a blank album with a caught `Name` lookup error.
Its subtitle labels worked, but SRT selection received native SRT text without an
opening cue; the reference delivered VTT. These historical failures are retained
alongside the later source15 subtitle and source18 audio results. `IsStandaloneSpecial`
is unmodeled. [Real music-tag indexing](client-music-metadata-plan.md) and explicit
profile projection for unselected subtitle candidates are installed in source15.
The [source15 subtitle and ordinary-TV runs](verification-m3e-source15-subtitle-tv.md)
passed, including actual opening/seek-window cues, Off, stop and exact-token
logout rejection. Auxiliary errors remain recorded. The subsequent music scan
indexed the real title/album/artist tags with unchanged physical IDs, retaining
the historical playback data. The [source15 MP3 attempt](m3e-source15-mp3-blocked-summary.json)
now renders the album and selects the exact track, but Audio/universal returns
HTTP 400 before decoded playback. The subsequent state reports return 404.
User data and preferences remained unchanged. The error dialog blocked UI
logout; separate exact-token API cleanup returned 204 then 401. [FLAC subsequently](verification-m3e-source15-scanned-music.md)
reproduced HTTP 400 with `invalid_audio_request` and an observed Container list
containing qualified `container|codec` capabilities. Its real UI logout returned
204 followed by exact-token 401. Neither failed run establishes audio playback
acceptance. The MP3 API cleanup remains separate from UI logout evidence.

The source18/schema25 main deployment is historical and has been replaced by
the accepted source28/schema26 main checkpoint above. No complete M3 compatibility
is claimed. Historical audio evidence retains its scope; broader milestones remain open.

The [source07 runtime diagnostic](m3e-client-home-configuration-blocker.json)
records `Cannot read properties of undefined (reading 'includes')` immediately
after successful initialization responses. A bounded CDP observer retained only
sanitized exception first lines and resumed immediately; it did not inspect
source, stacks, scopes or object properties. This supports the missing-array
hypothesis but does not identify the exact member. Debugger timing is diagnostic
only. Source08 then connected `users.configuration` through every user-loading
path and projected the 15 observed fields with non-null arrays; its
[15 targeted regressions](m3e-configuration-source08-tests.json) passed. Subsequent
debugger-free UI runs displayed the home content. Emby user logout now returns
the observed empty 204, and actual browser checks confirm token rejection.

Source07's [22 query/HTTP tests](m3e-collections-source07-tests.json) cover real
Playlist/BoxSet filtering, counts, pagination and ACLs. Source06's
[12 tests](m3e-preferences-source06-tests.json) cover the complete client query
including `X-Emby-Language`; source05's preference/schema and raw restore checks
remain historical scoped evidence. Collection creation/editing is still absent.

`System/Endpoint` uses current Emby authority and the resolved client address.
The [reference contract](m3e-reference-movie-extras-contracts.json) proves anonymous
401 and authenticated loopback `{IsLocal:true, IsInNetwork:true}`. Goby's explicit
additional policy treats the actual local connection address as local, and
private/link-local unicast addresses as in-network. Existing trusted-proxy
resolution applies. This classification never grants permissions; non-loopback
reference parity has not been sampled. The same evidence bundle records observed
empty Intros/SpecialFeatures/ThemeMedia contracts and their precise limitations.

## Resumed environment

The initial SSH observation found a recently rebooted test host. PostgreSQL 17's
primary cluster on port 5432 was running. `goby-foundation-test.service` was
inactive with MainPID 0; the transient `goby-emby-reference.service` did not exist.
The old reference runtime, scratch PostgreSQL data and dependency links under
`/dev/shm` were absent. Persistent source, evidence, media and database directories
remained available. An old owner record is not evidence that a process is live.

At the initial resumed observation, `/opt/goby-dev/goby` was byte-identical to M5j, SHA-256
`a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81`.
The retained official `emby-server-deb_4.9.5.0_amd64.deb` also matches its pinned
SHA-256 `1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843`.
The reviewed reference prepare/start scripts recreated only the missing runtime
from that package and started the existing reference data in its private network
namespace. That start reported PID 325210; verify its current identity before use.
Those initial actions did not start or replace the primary service. The later
source18 main deployment and its distinct process identity are recorded above.

The new private work directory is `/opt/goby-test/exec-work-m3e`, root-owned mode
0700, with marker `goby-m3e-client-acceptance-v1`. The
[persistent PostgreSQL workspace](m3e-postgres-workspace-acceptance.json) passed
44 guards and actual initialization, stop/start, data and credential retention,
and concurrent-lock checks. This establishes process restart, not a host reboot.
The [Goby client fixture](m3e-goby-client-fixture.json) is ready on port 18198,
using its own database on port 15432 and its own users. Three completed scans
indexed one movie, three episodes and two audio items. The
[owned media manifest](m3e-client-media.json) includes 600-second 30 fps H.264/AAC
video, external SRT/WebVTT and 180-second MP3/FLAC audio.
They must preserve port 5432, M5j recovery/backup stores, existing media, credentials
and all retained evidence. Each fixture uses three separate recovery directories.

## Actual client and evidence boundary

Use the unmodified Web Client shipped with the pinned official Emby 4.9.5.0
package. The [official Web Client documentation](https://emby.media/support/articles/Web-Client.html)
describes its browser playback behavior; the
[official feature matrix](https://emby.media/support/articles/Premiere-Feature-Matrix.html)
lists full playback for PC/mobile browsers without a Premiere prerequisite.
This is an external test dependency, not a Goby consumer playback page or a
redistributed part of the product. Do not inspect or incorporate server internals.

A loopback-only test proxy may supply the original public `/web/` resources from
the reference HTTP service and route API/media traffic to the selected backend.
It must not substitute DTOs, hide failed routes, inject credentials, rewrite
client logic, synthesize playback events, or disable browser security to pass a
gate. Normal reverse-proxy transport handling must preserve status, bodies,
Range behavior, streaming cancellation and WebSocket messages.

The [proxy's 16 independent transport tests](m3e-client-proxy-transport.json)
passed, including real WebSocket frames, Range, chunked messages, half-close,
disconnect propagation, request isolation and bounded backpressure. They use
synthetic peers and do not establish media-client compatibility. Ordinary HTTP
connections are closed between requests, so this proxy provides no keepalive
acceptance evidence.

Pin the browser build, Playwright version, package and served client resource
hashes, source revision and media hashes. Use API setup only for owned fixture
preparation and independent state observation. Login, browsing, playback, seek,
stop and resume must be driven through the real client UI.

## Required acceptance work

| Gate | Required evidence | Current status |
| --- | --- | --- |
| Bootstrap and login | Real client initializes, shows the selected server, and authenticates an owned ordinary user on each backend | Scoped reference and Goby UI evidence passed; auxiliary compatibility gaps remain |
| Movie, TV and music browse | Visible libraries, item details, season/episode hierarchy and accurate item/media identities | Movie, ordinary TV and scanned music evidence recorded; specials and broader catalog cases remain open |
| Direct video | H.264/AAC playback with advancing media time and decoded video frames, genuine client start/progress reports | Source11 core movie workflow passed; auxiliary reads remain unresolved |
| Seek, stop and resume | UI pause, forward/backward seek, stop, persisted position, reload/login and UI resume at the observed position | Scoped movie lifecycle passed; source18 audio controls, stop and history passed |
| Direct audio | Real supported MP3/FLAC flows with method and media capability recorded separately | Source18 MP3 core passed with original Home harness failure preserved; FLAC full workflow passed |
| External subtitles | UI track selection, actual subtitle delivery and timing through seek for the supported SRT/WebVTT paths | Source15 SRT/VTT and ordinary TV checks passed within their fixture scope |
| Sessions and isolation | Real client reports and session changes; a second owned user cannot observe or change the first user's protected state | Input07 original-Movie dual-user navigation/preparation, SpecialFeatures200 empty arrays, zero page errors, own200/foreign403, unchanged state projections and UI logout204/exact-token401 passed. One console warning/error per user remains. Nonempty positive extras and library restriction remain open; input05/input06 scopes are historical |
| Reference comparison | The same client/profile flows against the pinned reference, with exact supported differences and unresolved failures retained | Scoped reference/client journeys recorded; full matrix and auxiliary differences remain open |
| Regression and publication | Relevant remote regression, complete final-source checks, reviewed deployment where needed, documentation and commit/push | Source32 full 1,873-test race suite/build, candidate schema27 upgrade, scoped input07 client flow and precise root extension passed; product b9bb7b1 is published. Primary remains source28/schema26. Nonempty positive-fixture acceptance and complete milestones remain open |

Existing CORS support already handles implemented compatibility paths; do not
broaden the administrator origin policy based on a missing-route symptom.
Early static review found missing preferences and bandwidth/branding families;
the later observed exchanges and fixes above supersede that initial inventory.
The global NextUp behavior remains an explicitly unverified Goby policy.

The [initial anonymous reference reads](m3e-reference-bootstrap-read.json) record
`Branding/Configuration` as HTTP 200 with `{}`, `Branding/Css` as HTTP 200 with an
empty `text/css` body, and authentication-required `System/Endpoint` and
`Playback/BitrateTest` as HTTP 401. These five read observations are not a
completed client workflow and are not added to the historical 2,462-record
full-exchange reference corpus.

## Historical first actual client blocker

The [unmodified client against the accepted M5j binary](m3e-client-version-rejection.json)
read `System/Info/Public` successfully but stopped at its server-update dialog
before login. Goby's product release `0.1.0-dev` was being used as the client's
protocol-version input. The reference's [login-page control](m3e-reference-client-bootstrap.json)
loaded without API/page/console errors and hashed all 196 served client resources.

The compatibility response now separates the pinned API baseline (`Version:
4.9.5.0`) from the actual application release (`GobyVersion`) and keeps
`ProductName: Goby`. Native administration continues to use the actual product
version. This identifies the intended wire contract, not a completed operation
matrix or a claim that Goby is the Emby product. The
[targeted remote regression and Linux build](m3e-bootstrap-version-tests.json)
passed all six bootstrap/authentication/identity tests with race detection and no
skips. The subsequent client runs and source18 full regression are recorded above;
this bootstrap checkpoint alone was not client acceptance.

The [controlled fixture upgrade](m3e-candidate-version-upgrade.json) retained the
old executable and preserved the fixture's server identity, schema, credentials,
catalog, settings and recovery state. The next
[unmodified-client observation](m3e-client-version-login-page.json) reached Goby's
login page; the update dialog was gone. This is a resolved bootstrap blocker,
not complete login or playback acceptance. Missing branding routes still caused
two failed requests and one page error.

## Historical form login compatibility

The next real [Goby UI login](m3e-client-form-login-rejection.json) returned HTTP
415. The browser submitted `application/x-www-form-urlencoded; charset=UTF-8`,
where the existing Goby handler accepted only JSON. The
[same UI against the fresh reference](m3e-reference-client-login.json) submitted
exactly `Username` and `Pw`, authenticated with HTTP 200, and displayed the three
owned libraries and latest movie/music/TV entries. It also sent capability JSON
under `text/plain` and fetched `UserSettings`, making these concrete subsequent
initialization contracts. URLencoded login adaptation is in progress; credentials
must come from the bounded body, never the query string or an ambiguous duplicate.

The [fresh reference fixture](m3e-reference-fixture-acceptance.json) has independent
data, three owned accounts and three libraries. Its 41 preparation guards,
bootstrap, ordinary-user access, initialization-session revocation and reuse
checks passed. The first real browser login retained its own session; preparation
logout evidence must not be confused with later browser cleanup. That first
browser run also observed an external HTTP 404; subsequent controlled runs will
explicitly restrict traffic to the selected local server, with blocked external
requests reported separately rather than substituted with successful responses.

The [network-isolated reference login](m3e-reference-client-login-isolated.json)
then passed with no local API failures or page errors. Requests to the external
registration service were explicitly blocked; the local WebSocket remained a
real connection. The reference client requires its normal Service Worker during
startup, so network confinement covers that worker rather than disabling it.

The [source03 targeted regression](m3e-bootstrap-source03-tests.json) passed 30
top-level race tests and a Linux build. It includes URLencoded authentication,
empty branding projections and capability JSON carried as `text/plain`. A prior
[test-fixture failure](m3e-bootstrap-source02-failed.json) is retained separately.
Source03 was installed only in the isolated candidate, preserving its data and
credentials. Its [next actual UI request](m3e-client-query-auth-rejection.json)
now reaches authentication but returns the reference-style missing `appName`
error: the client sends `X-Emby-Client`, device and version metadata in the query,
with no Authorization header. After successful reference login, it also sends
`X-Emby-Token` in the query. Supporting those explicit carriers, with the existing
cross-carrier conflict checks, is in progress. Passwords and usernames must still
come only from the login body.

The [initialization contract study](m3e-reference-client-initialization.json)
separately establishes empty UserSettings defaults, their separation from
UserConfiguration, DisplayPreferences client-dependent defaults, the local
System/Endpoint response and bounded authenticated bitrate-test bytes. Preference
write shape, persistence and authorization remain under investigation; the
default empty response alone does not justify a read-only preference stub.
The [preference storage and migration plan](client-preferences-plan.md) preserves
the separate configuration domains and versioned backup/restore obligations.

## Historical source04 checkpoint and next work

The [expanded source04 regression](m3e-bootstrap-source04-tests.json) passed all
47 targeted race tests without skips or failures, then built the Linux binary
from the same 585 Go/module/SQL inputs. It covers the explicit query credential
carriers, cross-carrier conflicts, token-safe diagnostics, native/compatibility
boundaries, normal/chunked text-JSON playback and durable state. This is not the
complete repository suite. The
[candidate-only upgrade](m3e-candidate-source04-upgrade.json) retained prior bytes
and verified the existing fixture identity/data/credentials/recovery state.

The [real client on source04](m3e-client-preferences-blocker.json) now receives
authentication HTTP 200 and capability HTTP 204. It then receives UserSettings
HTTP 404 and displays a sign-in error; successful authentication is not successful
entry to the home page. That failed workflow retained an owned login session and
must not be reported as cleaned up.

The [reference movie workflow](m3e-reference-movie-client.json) completes real
playback, pause, forward/backward seek, stop, UI logout, a fresh UI login, resume
near the saved 119-second position and a second stop/logout. Decoded frames and
actual Range/206 responses are recorded. Four message-less page errors and six
blocked external requests remain explicit observations. The successful final
run's two sessions were logged out; earlier failed attempts retain private
storage-state files and still need controlled cleanup.

The [reference display-preference workflow](m3e-reference-preferences-client.json)
provides the legal write shape: `POST /UserSettings/{UserId}/Partial`, `text/plain`,
with a JSON string map such as `{"genreLimitOnDetails":"2"}`. Both the UI change
from 1 to 2 and its restoration to 1 returned HTTP 204, followed by verified UI
logout. An explicit stored default may remain, so the current baseline must be
read before further research; do not clear it merely because the initial GET
was empty. Partial merge/null/deletion and full-write semantics are being checked
on that isolated account before implementing persistence and schema evolution.

The following service handles belong to the historical source04 checkpoint and
are not current ownership selectors. The source18 candidate identity is recorded
at the start of this document:

- Goby client fixture: `goby-client-m3e.service`, PID 342622, start ticks 558288,
  host port 18198; UI/API proxy entry is 18196.
- Fresh reference: `goby-emby-client-m3e.service`, PID 332054, start ticks 357218,
  private namespace port 18097; proxy entry is 18197.
- Shared historical reference: `goby-emby-reference.service`, PID 325210,
  start ticks 252540, separate namespace; do not use it for preference mutations.
- The active dual proxy is `goby-client-proxy-m3e-fresh.service`; its saved
  identity is `/opt/goby-test/exec-work-m3e/dual-proxy-status-02.json`.

At this checkpoint, the next work was preference persistence, its migration and
old-backup gates, and repeated original-client checks. Later evidence above
records those scoped outcomes. The broader M3/M4/M5/M6 backlog remains open.

## Historical schema24 runtime progress

The [trusted schema24 catalog](m3e-schema24-catalog.json) was generated from actual
migrations in one new owned PostgreSQL 17 database on port 15432, then that empty
fixture was removed after ownership and idle checks. The schema23 catalog remains
available. The [18 targeted source05 tests](m3e-preferences-source05-tests.json)
passed for migration, domain, parser and HTTP behavior. The
[three real backup/restore tests](m3e-schema24-restore-targeted.json) passed for
current nonempty preferences, a genuine schema23 archive migrated to schema24,
and exact catalog JSON; both test databases/roles were removed and HBA restored.

The [protected candidate upgrade](m3e-schema24-candidate-upgrade.json) preserved
all old 29 tables' rows/columns and the explicitly inspected relation/sequence
identity, ACL, source, credentials and recovery facts. Only the new migration
and empty user_settings table appeared. This operation upgraded the isolated
client fixture, not the main service.

The first schema24 UI retry exposed omitted `X-Emby-Language` handling in the
preference query guard. Source06 adds that bounded, non-partitioning metadata and
a test using the complete observed query. Its next
[real home attempt](m3e-client-home-type-rejection.json) authenticates and loads
preferences, but the movie workflow stops at home content. The
[recorded query values](m3e-client-browse-query.json) identify exactly two failures:
`IncludeItemTypes=Playlist` and `BoxSet`, each with `Recursive=true` and
`Fields=PrimaryImageAspectRatio`. Correct type normalization must retain the real
catalog query, ACL filtering, counts and pages; creation/editing of those content
types remains separate planned work.

The [browser session cleanup](m3e-browser-session-cleanup.json) logged out three
exactly bound failed reference sessions and revoked one exactly matched Goby
session, including cleanup of its temporary native administrator session. Two
reference observation sessions without direct state/token bindings remain
explicitly listed. Later runs must keep private storage-state evidence whenever
they retain an authenticated session, even in diagnostic observation mode.

Raw browser state, credentials, query-bearing URLs and private traces stay in
protected remote storage. Published records contain deliberate sanitized
projections and distinguish client decoding from HTTP/API-only checks. A passing
single-media journey must not be reported as the entire M3 or full project gate.
