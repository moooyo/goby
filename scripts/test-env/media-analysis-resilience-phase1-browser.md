# Compatibility long-tail browser acceptance

Status: authored for consolidated remote verification; not an execution result.
Do not compile, generate media, launch a process/browser/listener, inspect remote
runtime state, or execute this fixture until the coordinator freezes the whole
phase. This increment does not inherit earlier local verification authorization.
All compilation, integration and actual browser execution belongs on `test-env`.

## Scope and ownership

The Linux test `TestMediaAnalysisResiliencePhase1BrowserIntegration` uses build
tags `goby_embed_admin,goby_browser_integration`. Its Go fixture is
`internal/server/media_analysis_resilience_phase1_browser_integration_test.go`;
the browser driver is `media-analysis-resilience-phase1-browser.mjs`.

The fixture runs the actual embedded native administrator UI and two declared
compatibility consumers: an Emby administrator and an ordinary viewer. Each uses
a separate browser context and real password authentication. Only the static
`/__media-analysis-phase1-consumer` document belongs to the test. Its buttons
follow real returned account and search identities into actual detail requests.
Every business endpoint reaches Goby unchanged. No business-response mocks,
browser-storage injection, proprietary-client patches or commercial licensing
workarounds establish acceptance. This fixture does not claim media playback,
hardware execution or parity with arbitrary third-party clients.

The fixture generates short actual media through pinned FFmpeg, imports actual
NFO documents through the scanner and records source hashes. Three authored
audio samples distinguish a retained embedded cover, a directory sidecar, and
a new embedded cover arriving after extraction is disabled. Browser images
must decode actual authenticated response bytes; the observer independently
checks hashes and pixels. A separately
authenticated Go observer checks PostgreSQL, exact catalog identities, scan
jobs, revisions, source files and runtime cleanup at every stage. Browser values
cannot substitute for those independent observations.

## Frozen input contract

Use a fresh owned artifact directory, schema, media root and run ID. Never reuse
the closed selected-compatibility phase 4 run directory, service name, database
pair, receipt or private environment as a new execution scope.

The coordinator supplies the existing admitted environment variables:

- `GOBY_TEST_DATABASE_URL`
- `GOBY_TEST_BROWSER_NODE`
- `GOBY_TEST_PLAYWRIGHT_MODULE`
- `PLAYWRIGHT_BROWSERS_PATH`
- `GOBY_TEST_BROWSER_ARTIFACTS_DIR`
- `GOBY_MEDIA_ANALYSIS_PHASE1_EXECUTION_CONFIG`
- `GOBY_MEDIA_ANALYSIS_PHASE1_RUN_ID`

The private execution JSON has marker `goby-media-analysis-phase1-execution-v1`
and canonical `FFmpegPath`, `FFmpegSHA256`, `FFprobePath`, `FFprobeSHA256` fields.
Source, tool and embedded frontend identities must match their frozen inputs.
No notification receiver, OCR model, HLS engine or new npm dependency is needed.

Go creates `GOBY_MEDIA_ANALYSIS_PHASE1_CONTEXT` as a private `0600` document in
the owned `0700` run directory. Its marker is
`goby-media-analysis-phase1-browser-fixture-v1`. It contains the current owned
origin, account credentials, independently authored user-directory facts,
library/item identities and result location. Tokens are acquired through real
login, remain in consumer closures, and are excluded from result files and
screenshots. Browser children receive no database credentials.

After the complete phase is frozen, the coordinator may run the following only
in the admitted remote scope, with the same frozen frontend assets:

```sh
"<admitted-go>" test -count=1 -timeout=12m \
  -tags=goby_embed_admin,goby_browser_integration \
  ./internal/server -run '^TestMediaAnalysisResiliencePhase1BrowserIntegration$' -v
```

This guide does not authorize executing that command during implementation.

## Ordered real journey

1. `authentication`: native administrator sign-in and independent administrator
   and viewer compatibility logins, with real persisted sessions.
2. `user-saved`: native Manage user changes Bravo to hidden and disabled, using
   the loaded revision. No tested consumer authenticates as the edited account.
3. `user-query`: consume five independently expected user-query cases: combined
   filters with descending paging, hidden-and-disabled, inclusive name bound,
   count-only and beyond-end. Verify current authorization, malformed requests,
   `no-store`, exact ordered IDs and totals; click a returned account identity.
4. `specials-before`: consume true/false/count-only standalone queries and the
   actual placed-special detail. Season zero alone is not the standalone oracle.
5. `metadata-saved`: native metadata forms set Series status to Ended and End
   date to December 31, 2025, then set the placed special's two season-placement
   values to zero while retaining its before-episode value of two. Both saves
   use exact CAS revisions; physical season/episode identity is unchanged.
6. `specials-after`: request a real native television scan, observe its actual
   terminal job, and consume the changed standalone result. Original NFO facts
   must not overwrite explicit administrator metadata.
7. `sorting-saved`: native Settings sets `Sorting.SortRemoveWords` to `The`.
   The authored The Amber/Bravo order changes from Bravo/Amber to Amber/Bravo
   immediately through the real query consumer, not merely through a settings
   response. Defaults remain the declared empty list.
8. `sorting-conflict`: keep an unsaved `The`/`a` sorting draft while an independent
   real native Settings PUT changes only the additional width to 640. The stale
   form PUT must return 409, preserve and lock its draft, and leave stored
   Sorting at `The`. Explicit reload reads the actual winning revision without
   submitting another mutation.
9. `sorting-selective-reset`: recreate the dirty sorting draft and reset only
   `TranscodingMaxWidth` through the native dialog. Stored Sorting remains
   `The`; the unsaved sorting text remains in the form. Cancelled discard keeps
   the draft, confirmed discard restores the saved sorting, and neither issues
   an additional settings mutation.
10. `sorting-response-loss`: clear the sorting textbox and forward its real PUT
    with Playwright `route.fetch`. Only after the actual backend returns 200 is
    that response withheld from the original UI request. No success response
    or business state is fabricated. The UI must lock the unconfirmed draft,
    actual GET and PostgreSQL must show the committed empty array, and the real
    catalog consumer must observe Bravo/Amber. This stage has its own independent
    acknowledgement before any restore.
11. `sorting-restored`: explicitly reload the committed empty sorting value,
    proving that refresh did not replay the lost-response write. Then make a new
    deliberate native save of `The`; verify Amber/Bravo through the real catalog
    consumer and a second independent PostgreSQL acknowledgement.
12. `sorting-consumed`: a real native movie scan retains the configured order.
   Only after independent acknowledgement does the fixture change its owned
   Movie A NFO to the declared imported title and record the new source hash.
13. `imports-disabled`: native library editing disables local metadata import
   and requests Scan after saving. Observe that specific real job completing.
14. `imports-retained`: actual catalog consumption retains the previously
    imported title and order despite the changed source NFO.
15. `imports-enabled`: re-enable local metadata import and explicitly request
    another actual scan through the same native form.
16. `imports-consumed`: the actual catalog now exposes the authored changed
    title and corresponding order, while item identity remains stable.
17. `embedded-disabled`: turn off Extract embedded audio artwork in the native
    music-library form, preserving the local-image and metadata master flags.
    The administrator consumer discovers Goby Embedded Artwork through
    `/Libraries/AvailableOptions` and reads its empty Audio `ImageFetchers` array
    through `/Library/VirtualFolders/Query`. After independent confirmation,
    copy the declared new audio source into its owned album directory.
18. `embedded-absent`: run a real native music scan and discover the newly
    indexed Audio ID through Items. Its Primary image must return 404. The
    retained embedded cover and independent directory sidecar must both remain
    readable and actually decode in the browser, with independently verified
    bytes and pixel identities.
19. `embedded-enabled`: post the discovered Audio `TypeOptions` provider to
    `/Library/VirtualFolders/LibraryOptions` using the current quoted revision
    in `If-Match`. Require the real 204/new ETag, compatible readback, and a fresh
    native library form showing extraction enabled and the master flags intact.
20. `embedded-present`: rescan the music library. The same new Audio ID now has
    the authored embedded cover; browser decoding and independent HTTP/PG pixel
    and hash observations prove publication. Both pre-existing image sources
    remain unchanged. This is extraction admission, not deletion of older art.
21. `fields`: consume mixed-case list `Fields`, navigate a detail with mixed-case
    `ExcludeFields=MediaStreams`, and verify removal from both the top level and
    nested MediaSources while core identity remains. Query transport language
    `x-emby-language=zh-CN` must not change metadata. Follow the real Search/Hints
    navigation URL for the newly imported title into the same physical item.
22. `restart`: close and rebuild the complete application/runtime over the same
    owned origin. This is application restart evidence, not an OS reboot.
23. `persisted`: reload native settings/metadata/library controls and repeat
    actual account, standalone, sorted-catalog, projected search and image
    consumption without replaying edits, imports or scans. Fresh native revisions
    must match the previously accepted values; cached JS revisions alone do not
    establish persistence.
24. `cleanup`: real compatibility Logout and native Sign out, then independent
    session/resource closure. Successful acceptance must need no fallback logout.

The four settings recovery stages extend the original twenty-stage journey;
earlier source identities and execution receipts remain unchanged. The fixture
HTTP wrapper only observes actual completed Settings PUT and reset POST status
codes. It does not replace their handler or inspect/mutate request bodies. Go
compares those independently observed deltas with the browser observations:
`PUT 200, PUT 409`; `POST 200`; `PUT 200`; `PUT 200`. Each stage must advance the
durable revision by exactly one. In addition to the expected width/sorting
values and actual catalog ordering, a complete managed-settings row comparison
excludes only revision, update timestamp, additional width and sorting columns;
all unrelated persisted values remain identical. No SQL mutation prepares these
fault cases, and failed/stale writes cannot be reclassified as successful ones.

Every phase writes a bound `stage-<phase>-request.json`. The observer returns
`stage-<phase>-database.json` with marker
`goby-media-analysis-phase1-stage-database-v1`, the same run/phase, and independent
completion/source-verification flags. Failed observations remain failed and wake
the browser immediately. Named failure diagnostics and masked screenshots
supplement the evidence; they cannot replace it.

## Composed acceptance and closure

The browser result uses marker `goby-media-analysis-phase1-browser-result-v1`.
It requires all twenty-four checks and acknowledgements, zero page errors and
foreign requests, real navigations, fresh source facts and normal logout.
The driver separately records joined browser descendants, server listeners and
workers, schema/media/private-context cleanup and immutable execution inputs.
Root-owned browser success does not establish a non-root deployment profile.

The browser journey complements rather than replaces the affected identity,
library, settings and HTTP matrices. Separate tests must cover migration from
the published schema, exact current recovery catalogs, normal and recovery
migration paths, and actual encrypted backup/restore of account policy, typed
metadata/placement, explicit sort provenance and sorting settings. Preserve
unrelated historical metadata, secrets, user state, host-owned settings and
restored-credential revocation rules. No raw archive validator or mocked UI
alone establishes restored runtime consumption.

Use an explicitly owned Go build cache in the remote runner. Each package scope
records source and binary SHA, exact selectors, parent failures/skips, original
JSONL and process-group closure. Original failed aggregates are immutable;
affected passing repairs may be composed only where product/input equivalence
is established. Do not add overlapping pass counts or reinterpret a skipped
hardware gate as acceptance. Final PostgreSQL/service/cgroup closure is a
separate coordinator obligation; retain evidence while closing owned runtime.
