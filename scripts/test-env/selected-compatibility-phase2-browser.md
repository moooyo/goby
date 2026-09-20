# Selected compatibility phase 2 native browser acceptance

Status: source prepared for the coordinator's consolidated phase 2 run. This
guide is not an execution result. Do not run the fixture, generate media, probe
tools, build the application, or start a browser until all phase 2 delivery code
is integrated and the coordinator admits verification.

`TestSelectedCompatibilityPhase2BrowserIntegration` and
`selected-compatibility-phase2-browser.mjs` exercise the embedded native
administrator application against a disposable PostgreSQL schema and real
FFmpeg, ffprobe, bitmap subtitles, Tesseract, and pinned models. Application
responses are not mocked. The fixture does not use Emby Web, a proprietary asset
host, a patched consumer, or an external license service.

## Fixture and ownership

The Go fixture creates a new private media root and real short H.264/AAC sources.
The removal movie contains two separately identified embedded text subtitle
streams and explicit chapters. The browser prepares one selected stream for
removal, reviews the ready result, and explicitly applies it. Go verifies the
actual replacement, the byte-identical retained original, remaining streams,
catalog identity, and unchanged seeded user data. It also fully decodes the
published video and audio with the pinned FFmpeg executable and fetches the
retained French subtitle as real WebVTT through the authorized product route.
A queued or ready result must leave the source unchanged.

The OCR movie contains the authored `overlap-pgs.mks` stream from
`scripts/test-env/bitmap-subtitle-fixtures.py`, muxed with real audio and video.
The generator uses an independently pinned Noto font, records authored timing,
expected text and source pixel hashes, and never downloads dependencies. The
fixture must retain a zero media origin or prove its independently measured
offset; it cannot infer expected timing from the OCR output being tested.
The selected native OCR journey requires the `eng` and `chi_sim` models.

Two owned audio sources carry different embedded image pixels. Actual authorized
image HTTP requests and image decoding independently observe an extracted cover
and a generated library image. Their evidence is labelled HTTP consumption,
not a claim that the browser displayed an image it did not load.

All destructive file actions are confined to these generated media copies. The
fixture keeps the successful removal's original inode and bytes until its final
owned-media cleanup. It must not delete, replace, scan, or reuse unrelated media,
old acceptance databases, historical worker directories, or earlier receipts.

## Explicit execution inventory

The future root-owned Linux runner supplies these environment variables:

| Variable | Purpose |
| --- | --- |
| `GOBY_SELECTED_PHASE2_RUN_ID` | New bounded run identifier |
| `GOBY_SELECTED_PHASE2_EXECUTION_CONFIG` | Canonical private `0600` JSON inventory below |
| `GOBY_TEST_DATABASE_URL` | Private connection to this run's owned PostgreSQL environment |
| `GOBY_TEST_BROWSER_NODE` | Admitted, root-owned Node executable |
| `GOBY_TEST_PLAYWRIGHT_MODULE` | Absolute retained Playwright module file |
| `PLAYWRIGHT_BROWSERS_PATH` | Admitted existing Chromium cache |
| `GOBY_TEST_BROWSER_ARTIFACTS_DIR` | Existing root-owned `0700` artifact parent |

The execution JSON has marker `goby-selected-phase2-execution-v1`, absolute
`FFmpegPath`, `FFprobePath`, `PythonPath`, and `FontPath`, and the corresponding
`FFmpegSHA256`, `FFprobeSHA256`, `PythonSHA256`, and `FontSHA256`. Its `OCR` field
uses the product inventory shape:

```json
{
  "engine": "tesseract",
  "executable": "/admitted/tool/tesseract",
  "toolSHA256": "<actual-lowercase-sha256>",
  "tessdataDirectory": "/admitted/models",
  "models": [
    {"id": "eng", "language": "eng", "filename": "eng.traineddata", "sha256": "<actual-lowercase-sha256>"},
    {"id": "chi_sim", "language": "chi_sim", "filename": "chi_sim.traineddata", "sha256": "<actual-lowercase-sha256>"}
  ]
}
```

The same execution JSON also requires `HlsBundlePath` and `HlsBundleSHA256` for
the retained HLS.js `1.6.0-beta.2` library. The exact reviewed AMD library is:

```text
/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-01/package/opt/emby-server/system/dashboard-ui/modules/hlsjs/hls.js
04a55387b26d6becff5b87b470b9b19c3fe41d25c0cd3c1e6c3d885a56572fdb
```

The harness loads only this hash-checked library with its minimal AMD export
adapter. It neither starts Emby Web nor calls its registration services. The
bundle remains in its retained private installation and is not redistributed.
All inventoried source and tool files must remain root-owned, canonical regular
files with one link and no group/world write permission. A Windows archive
extracted with mode `0664` must be hardened in a new execution scope before
admission; do not alter a previous failed scope's metadata or receipt.

These placeholders are not runnable defaults. The font must match the generator's
fixed release blob and size in addition to the operator-supplied SHA-256. Python
needs Pillow and FreeType. The runtime uses one media-operation worker, a bounded
queue, short per-job timeouts, and an owned scratch directory with an explicit
budget. It does not substitute a fake executable or replace model output with
expected text. Tool, model, font, generator, source, candidate and image hashes
are retained as evidence without copying secret environment values.

After freezing the source, building its embedded administrator assets, and
preparing the private environment, the coordinator runs the following only on
the admitted `test-env` scope:

```sh
"<admitted-go>" test -count=1 -timeout=12m \
  -tags=goby_embed_admin,goby_browser_integration \
  ./internal/server -run '^TestSelectedCompatibilityPhase2BrowserIntegration$' -v
```

The Go test launches the driver. It does not need a separate Playwright test
runner or a new dependency installation. Browser work has an eight-minute
deadline and individual native jobs are bounded separately. This is a small
functional fixture, not a capacity benchmark. The root-owned browser profile
does not establish a non-root deployment profile. PostgreSQL itself uses the
coordinator's separately owned database account.

## Private browser context and observations

Go writes a bounded `0600` context inside a new owned `0700` directory and passes
its path as `GOBY_SELECTED_PHASE2_CONTEXT`. The marker is
`goby-selected-phase2-browser-fixture-v1`; `RunId` must match the separately
provided environment variable. The context carries only this run's loopback
origin, fixture accounts, source item/library identifiers, stream identities,
model IDs, and output paths. Disposable passwords and database credentials do
not enter browser result files, screenshots, request URLs, or task messages.

The driver uses exact native dialog names and real UI controls. It blocks
foreign HTTP/WebSocket traffic and service workers. It does not inject client
state, modify browser storage or history, synthesize worker completion, issue
an Apply request outside the UI, or simulate media events. Safe diagnostics
retain the current phase/operation, response status, operation state/revision,
and bounded error categories. Failure screenshots are limited, mask inputs and
secret text, and cannot replace the underlying error or prevent cleanup.
Native screenshots disable UI animation so a dialog fade does not obscure
the review view.

Initialization seeds the manual overview through the real metadata store using
the current revision, a non-null override object, and an explicit empty
`LockedFields` array. Setup failures retain safe error categories, Go error
types, PostgreSQL SQLSTATE codes, and allowlisted metadata field names. Raw
error messages, rejected values, private file paths, credentials, and connection
strings are excluded from these diagnostics.

The fixture wraps the real media-operation executors only for diagnostics. It
joins the idle coordinator before installing the wrappers and starts a fresh
coordinator using its already admitted configuration and inventory, without
another probe or recovery pass. Execute, Apply, Discard, progress values,
callback errors, and returned results remain delegated unchanged. Each call
creates a private `executor-<number>-<phase>/receipt.json` containing safe error
classifications, progress/callback outcomes, selected stream, and execution
budgets. No new public response fields or product logging are introduced.

On executor failure, `private-error.json` retains the bounded error tree,
including joined errors, after removing known passwords, access/fencing tokens,
database connection strings, URI credentials, and credential-shaped fields.
It is a private `0600` artifact, never copied into console output, browser
results, public DTOs, or the driver's summary. Limits and any truncation are
explicit in the file. Inspect it only within the owned private evidence scope.

The wrapper opens the selected authored source read-only. At the real
`remuxing` or `copying` progress callback it also opens the newly created
candidate read-only. The product may unlink a failed candidate normally; the
held descriptor allows the wrapper to preserve its actual final bytes after
the executor and child processes return. Failure samples are bounded to 32 MiB
each and saved as private `source-at-entry.bin` and
`candidate-held-descriptor.bin`, with hashes, inode/size/time observations, and
an explicit unlinked flag. An empty candidate is recorded as empty, and an
unavailable sample is never represented as captured. Descriptors close before
the executor returns to its coordinator. These retained diagnostic copies are
outside the media root and survive its ordinary cleanup; they are not used as
playback or publication inputs.

A separate browser context provides a private HTMLVideoElement test consumer
for the published OCR subtitle at the fixture-only `/__selected-phase2-media`
route. The companion `/__selected-phase2-hls.js` route serves the admitted
library bytes unchanged; the browser independently checks the delivered hash.
These two routes wrap only the owned test server. Every authentication, media,
subtitle, and administrator request reaches the real application. The test
page's explicit policy permits same-origin scripts and MSE blob media without
bypassing or weakening the administrator application's content security policy.
It signs in as the actual viewer and negotiates
the actual PlaybackInfo route with an explicit zero start and owned subtitle
index. HLS.js consumes the returned master and real audio/video/subtitle bytes.
The consumer does not add a product player, inject text cues, fake media events,
change the server clock, or replace any media response. It switches captions
using only the library's public `subtitleTrack` API and ordinary media seeks.
Every observation requires three new advancing video frame callbacks, a completed
seek, actual playback clock advancement, and the matching native TextTrack
active cues. The same HLS instance, playback scope, and producer must survive
selection, off, and reselection. Tokens and full media URLs stay private; the
safe result records only the playback/source/device/revision identities.

Every stage writes `stage-<phase>-request.json` with `RunId` and `Phase`. Operation
stages also provide the actual server-created `OperationId`; OCR review provides
the actual submitted `CueEdits` and saved `Revision`/`ResultHash`. Go independently
queries persisted state and reads owned source/output files. Successful replies
have this shape:

```json
{
  "Marker": "goby-selected-phase2-stage-database-v1",
  "RunId": "<this-run>",
  "Phase": "<this-stage>",
  "Complete": true,
  "ExpectedFilesVerified": true
}
```

`ExpectedFilesVerified` deliberately does not mean that every pathname remains
unchanged: exactly one explicit removal publication is expected to replace its
source, while its retained original and all unrelated sources must be preserved.
Before checks and on failure, Go retains safe database and file observations.
Failure replies use `Complete:false, Observed:true, Failed:true` and the error
code `database_stage_verification_failed`, so the browser does not wait blindly
for a success acknowledgement that will never arrive.

## Ordered journey

1. `authentication`: sign in with the real native administrator form.
2. `removal-ready`: open the selected movie's metadata and Media processing
   dialog, select its exact embedded stream and Matroska profile, and choose
   Prepare for review. Wait for the real worker's ready state; prove that no
   implicit publication occurred and that the original source is unchanged.
3. `removal-applied`: choose Apply reviewed result and Confirm apply. Require
   the completed publication, retained-original notice, exact remaining streams,
   stable item/user data, and actual file evidence.
4. `ocr-ready`: prepare the real PGS stream with explicitly selected `eng` and
   `chi_sim`, SRT output, and known subtitle properties. Wait for real recognition;
   require authored English text, real confidence values, and source bitmap
   images that actually decode in the browser. Go compares retained cue pixels
   and timing with independent authored fixture facts.
5. `ocr-reviewed`: edit cue 1 to `Phase 2 reviewed subtitle` at 1.25–2.4 seconds,
   keep it included, and exclude cue 2. Apply must remain disabled while dirty.
   Save cue changes through the real CAS endpoint. The saved result must have
   a new revision/hash; original recognition and bitmap provenance remain intact.
6. `ocr-applied`: explicitly confirm publication of that reviewed result. Go
   reads the actual owned subtitle through an authorized HTTP consumer and
   verifies the published bytes, included/excluded cues, timing and properties.
7. `subtitle-selected`: use the independent private consumer to play the actual
   OCR item from zero and select the published track. Seek to 1.45 seconds and
   require the revised text and 1.25–2.4 second interval in actual active cues,
   accompanied by new decoded frames. Go binds the real HLS producer and its
   authorized source/playback/session scope.
8. `subtitle-off`: set the public subtitle selection to `-1`, repeat the real
   seek, and require empty displayed active cues while decoded frames and the
   playback clock advance. The actual original producer must remain unchanged.
9. `subtitle-reselected`: select the same owned track again and repeat the real
   frame-and-cue observation without reconstructing HLS or renegotiating playback.
10. `subtitle-stopped`: destroy the consumer, detach its media element, call the
    real ActiveEncodings DELETE for that exact device/playback identity, sign
    out the viewer, and close its context. Go requires actual readers, runtime
    sessions, producer resources, and cache leases to close. Terminal database
    history is preserved; it is not deleted to manufacture empty tables.
11. `cancel-ready`: prepare a second real removal against the remaining subtitle
   of the already edited movie. Wait for its actual ready candidate without
   applying it.
12. `cancelled`: choose Cancel operation and Confirm cancellation; require the
   recorded cancelled state, discarded unpublished candidate, and unchanged
   edited source. The earlier retained original remains preserved.
13. `artwork`: independently fetch and decode the embedded cover and generated
   library image using real authorization and owned image inputs. Browser
   acceptance requires the Go image observations rather than a placeholder URL.
14. `restart`: close and rebuild the complete server runtime on the same owned
    origin. No worker, catalog, resource lease, or publication task is reused.
15. `persisted`: use the real Tasks → Media processing tab and open all three
    operation records. Published/cancelled states, source identities, revisions
    and result hashes must match; no new preparation or Apply is performed.
    Go verifies that restart did not silently rerun publication or rewrite media.
16. `cleanup`: navigate out of dialogs, use real native Sign out, require DELETE
    `/admin/v1/session` to return 204 and the sign-in page, close the browser
    context, and obtain independent session/runtime cleanup acknowledgement.

The browser records marker `goby-selected-phase2-browser-result-v1`. `Complete`
requires every ordered stage, every named check, zero page errors/foreign
requests, and cleanup. The Go driver must separately prove application, browser
process, schema, media, temporary-context and worker closure. A successful
admission response, a ready result, or a screenshot is not acceptance.

## Coverage boundary

This profile exercises native orchestration with small real media and one
reviewed bilingual PGS source. Existing real backend tests must additionally
cover the wider subtitle conversion/HLS matrix, the admitted DVD/PGS language
matrix, source replacement, publication interruption/recovery barriers, current
authority changes, broader container preservation, cancellation races, artwork
transforms and backup/restore. The browser fixture does not claim those cases
from synthetic state or extrapolate to every media container or OCR model.
The coordinator records their exact source/tool identities and results with
this run before closing phase 2. Earlier phase receipts remain unchanged.
