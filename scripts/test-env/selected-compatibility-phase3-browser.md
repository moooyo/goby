# Selected compatibility phase 3 browser acceptance

Status: source prepared for consolidated verification. This guide is not an
execution receipt. Do not build, generate media, launch the fixture, or run a
browser until all Phase 3 product and acceptance code is frozen and the
coordinator admits a new owned scope on `test-env`.

The fixture combines the actual native administrator UI with an explicitly
private browser consumer of the compatibility adapters. It uses real
PostgreSQL, scanned media, authentication, queries, images, playback negotiation,
HTMLAudioElement, and a connected Web Audio output graph. It does not run Emby
Web, patch a commercial client, mock business responses, or claim that arbitrary
third-party applications have been tested.

## Admission and ownership

`TestSelectedCompatibilityPhase3BrowserIntegration` has Linux, embedded-admin,
and browser-integration build tags. It owns a disposable database schema, short
generated media, a private artifact directory, and a loopback HTTP listener.
Existing Phase 1/2 resources, receipts, proprietary hosts, and application data
are not reused or modified.

The execution inventory is a canonical private `0600` JSON file named by
`GOBY_SELECTED_PHASE3_EXECUTION_CONFIG`. Its marker is
`goby-selected-phase3-execution-v1`; it declares `FFmpegPath`, `FFmpegSHA256`,
`FFprobePath`, and `FFprobeSHA256`. All tools and authoring scripts must have
actual recorded hashes and admitted filesystem ownership. Audio and video are
generated only when the remote fixture is explicitly run, with the real tools.
No OCR model, HLS JavaScript bundle, external registration service, or new npm
installation is required.

The coordinator also supplies `GOBY_TEST_DATABASE_URL`,
`GOBY_TEST_BROWSER_NODE`, `GOBY_TEST_PLAYWRIGHT_MODULE`,
`PLAYWRIGHT_BROWSERS_PATH`, `GOBY_TEST_BROWSER_ARTIFACTS_DIR`, and the unique
`GOBY_SELECTED_PHASE3_RUN_ID`. The Go fixture writes the private browser input
and passes its path in `GOBY_SELECTED_PHASE3_CONTEXT`. The input marker is
`goby-selected-phase3-browser-fixture-v1`. Passwords remain in that temporary
input or memory. Tokens are obtained through actual authentication and never
written to safe result files or screenshots. The runtime passes no database
connection string to the browser process.

After source and embedded administrator assets have been frozen, the
coordinator can run the following only in the admitted remote scope:

```sh
"<admitted-go>" test -count=1 -timeout=12m \
  -tags=goby_embed_admin,goby_browser_integration \
  ./internal/server -run '^TestSelectedCompatibilityPhase3BrowserIntegration$' -v
```

The browser has a seven-minute deadline. Media is short and functional; this is
not a capacity benchmark. The root-owned browser scope does not establish a
non-root deployment profile. Host service/cgroup and PostgreSQL closure remain
separate coordinator responsibilities in addition to the fixture's own receipts.

## Authored facts and consumer

The visible television library starts with a real S01E01. The administrator
imports exactly three expected facts: S01E02 with a known past date, S01E03 with
no date, and S01E04 with a known future date. Another scanned series has no
roster. No numbering gap is treated as proof that an episode exists. Later, the
fixture creates and scans an actual S01E02 using an owned media copy, then checks
that the roster links to the physical item and its virtual projection disappears.

Two visible movies and a denied movie provide an isolated Suggestions set.
Three visible music albums have one real track each; two share artist, album
artist, and genre relationships, while the third is unrelated fallback. Denied
sources include matching metadata and a hidden-only entity. A real playlist
contains a repeated visible track and a denied member. At least the selected
audio source has an embedded cover that the real image adapter serves.

The sole fixture-only consumer route is `/__selected-phase3-consumer`. It serves
a fixed document with selection buttons, a safe detail panel, an image, an audio
element, and play/stop controls. Every business/API/image/media request reaches
the unchanged Goby handler. Its CSP explicitly permits only same-origin
connections/scripts and local media/image blobs. The administrator application's
CSP remains unchanged; the browser does not enable a CSP bypass.

The driver installs the declared private consumer logic, authenticates normally,
and consumes actual endpoint responses. Click handlers follow returned search
navigation/image URLs and explicit artist/album relationships. They render safe
identity labels rather than private source paths. No server state, browser
storage, synthetic media clock, fabricated cue, oscillator, or replacement
network response is used to manufacture successful consumption.

## Ordered journey

1. `authentication`: use the real native administrator login form.
2. `preferences`: save both `Display missing episodes` and `Hide played items
   from Suggestions` in the native user-preferences dialog. Observe the returned
   revision and durable preference fields, the actual frontend success notice,
   and the settled saved controls before leaving the dialog.
3. `roster-reviewed`: import the complete source/entries JSON through the real
   file input after the current load enables it. Inspect its three-row preview while the stored roster remains
   absent. A preview must not implicitly save facts.
4. `roster-saved`: choose Save roster. The request must contain the exact
   reviewed entries/source and loaded CAS revision. Observe accepted provenance,
   stable virtual IDs, and absence of available physical matches.
5. `missing-queries`: use the authenticated consumer to query the preference,
   explicit missing/placeholder overrides, future facts, and unknown dates.
   Missing detail has virtual/non-downloadable/non-resumable flags and no source
   path, media source, or user data; PlaybackInfo rejects it with 404. NextUp,
   Resume, and the physical episode queue exclude these virtual facts.
6. `suggestions-played`: mark movie A played through the real API, observe it
   disappear by default, and retrieve it with explicit played filters. Unmark it
   to restore A+B, then mark it again. Denied IDs remain absent throughout.
7. `music-navigation`: click artist -> albums, album artist -> albums, and album
   -> tracks using the actual adapter responses and stable entity/item IDs.
8. `music-mixes`: exercise Item(Audio/Album), Song, Album, Artist, Playlist,
   genre-ID, and genre-name seeds. Require real playable Audio, seed priority,
   deduplication, authorized fallback, stable counts, and count-only/limited
   responses. A denied seed returns 404. Select the actual song result for play.
9. `search-navigation`: obtain physical and typed entity hints, follow their
   returned detail URLs by clicking consumer controls, and fetch/decode the real
   audio cover. Bind its byte hash and dimensions. A roster-only episode name
   produces no search hint; denied items and hidden-only entities stay absent.
10. `audio-playing`: click Play selected mix track, negotiate current
    PlaybackInfo from zero, and consume its actual progressive MP3 URL. Send
    Started and Progress with real identity/position. Require at least two
    seconds of advancing media time, multiple advancing AudioContext samples,
    and nonzero RMS/peak from the decoded media connected to the destination.
11. `audio-stopped`: click Stop playback, send the actual final position, retire
    exact ActiveEncodings, detach the element, and close AudioContext. Go must
    observe terminal playback/counting and resource closure. Terminal history is
    preserved instead of deleting rows to manufacture empty tables.
12. `physical-arrival`: Go creates and scans the actual owned S01E02 and returns
    its real item ID through the independent stage acknowledgement.
13. `arrival-queries`: the native roster now shows one available and two missing.
    Actual queries suppress the old virtual detail, include the arrived physical
    episode in the queue, and preserve preexisting episode identity/user data.
14. `restart`: close the complete server runtime and reconstruct it on the same
    owned origin. Record exact durable facts, preferences, and source fingerprints
    before and after, retaining the separately verified terminal playback history;
    no media is implicitly replayed.
15. `persisted`: reopen native preferences and roster review, then repeat bounded
    adapter reads. Require identical revisions/provenance/availability and the
    same expected preference and music projections.
16. `cleanup`: perform viewer Logout and native Sign out, each with actual 204
    responses, close both browser contexts, and require independent zero-active
    resource/session observations. A successful run needs no fallback revocation.

## Evidence and failure handling

Every stage writes `stage-<phase>-request.json`; the independent Go observer
returns a bound `stage-<phase>-database.json` with marker
`goby-selected-phase3-stage-database-v1`, `Observed:true`, `Complete:true`, and
`ExpectedFilesVerified:true`. Operation-specific safe identities bind query,
roster, arrival, and playback observations. Failed database checks retain safe
facts and write an immediate `Failed:true` acknowledgement. No unobserved stage
is reported as complete.

`browser-result.json` records all 16 stages/checks, safe API statuses and IDs,
query/mix sets, actual image bytes/hash/dimensions, audio clock/output samples,
page/foreign-request errors, screenshots, and cleanup results. Named operations
capture bounded masked screenshots on failure. Input/textarea/editable fields
and passwords are masked, and native animation is disabled for stable captures.
Full media URLs and access tokens are excluded. Original Phase 1/2 receipts
remain immutable.

Success requires all stages/checks, zero page errors, zero foreign requests,
matching embedded assets, and the Go driver's separate process/listener/schema/
media/private-context closure evidence. Samples from a browser audio graph
establish decoded output processing, not whether a human heard a physical
speaker. This profile does not prove every container/model/client permutation,
every ACL race, or all failure recovery paths; the coordinator must record the
appropriate real backend suites separately before closing Phase 3.
