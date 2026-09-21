# Real media-analysis Phase 2 browser acceptance

Status: source delivery only. No test, build, runtime probe, browser, decoder or
corpus download was executed while writing this harness. A missing corpus is an
unconfigured gate, not acceptance. Earlier synthetic episodes and alternate
encodes of one episode are not independent real-content accuracy evidence.

## Entrypoints and ownership

The opt-in Go entrypoint is
`TestMediaAnalysisResiliencePhase2BrowserIntegration`, with build tags
`linux`, `goby_embed_admin`, and `goby_browser_integration`. It requires the owned
Linux `test-env`, root, and the isolated integration database. The existing
fixture creates and drops a unique schema. Media copies and analysis cache live
in owned temporary directories; the original corpus is never changed or deleted.
The Go test timeout must exceed its 90-minute bounded fixture lifetime.

The Go entrypoint invokes these repository sources:

- `media-analysis-phase2-evaluate.py --admit-only` freezes and validates the real
  manifest, independent review references, source identities and tool hashes before
  creating an application or admitting a task. Admission runs no decoder.
- `media-analysis-phase2-browser.mjs` operates the real embedded native UI and
  actual HTTP endpoints. It never fulfills a business request with synthetic
  data. Requests outside the current owned origin are blocked and counted.
- `media-analysis-phase2-consumer.mjs` is an explicitly named Goby protocol
  adapter around Video.js. It is not an Emby SDK or Emby Web implementation.
  The independent BIF plugin remains unchanged.
- `media-analysis-phase2-evaluate.py` then independently parses and decodes BIF
  data and source frames, evaluates the frozen source-reviewed labels and preserves
  failed/pending counts. Its subprocess groups are bounded and joined.

The Go observer separately checks HTTP detections against the current library
owner, captures selected durable analysis state, binds preview hashes/timelines
to real publication rows, and checks a complete close/new-server/restart journey.
It checks the actual processed copies before and after work; their bytes must
match the labeled originals. Python independently holds and rechecks both the
original source and the actual processed copy. Nanosecond file identities stay
exact Go/Python JSON integers and never pass through browser JavaScript.

## Required environment

| Variable | Required value |
| --- | --- |
| `GOBY_MEDIA_ANALYSIS_PHASE2_MANIFEST` | Absolute private real-media manifest described in [the frozen manifest contract](media-analysis-phase2-manifest.md) |
| `GOBY_MEDIA_ANALYSIS_PHASE2_EXECUTION_CONFIG` | Absolute private JSON execution inventory below |
| `GOBY_MEDIA_ANALYSIS_PHASE2_RUN_ID` | Unique 8–128 character owned run identifier |
| `GOBY_TEST_DATABASE_URL` | Owned integration database, never a production database |
| `GOBY_TEST_BROWSER_NODE` | Canonical existing Node executable |
| `GOBY_TEST_PLAYWRIGHT_MODULE` | Canonical existing Playwright module entry |
| `PLAYWRIGHT_BROWSERS_PATH` | Existing compatible Chromium cache |
| `GOBY_TEST_BROWSER_ARTIFACTS_DIR` | Existing private artifact parent, identical to manifest `artifacts_root` |

Only an absent manifest variable produces an explicit `unconfigured` test skip.
A configured invalid manifest or missing dependency fails. A successful `go test`
exit with this test skipped must not be used to close the real-corpus gate.

This browser journey accepts at most 32 real cases and 16 GiB of byte-identical
media copies. Required previews use a shared 2–120 second configured minimum
interval. The selected named consumer profile requires browser-playable H.264
MP4 files for cases requiring previews, including at least one holdout-positive
case for actual detected-intro skip. Other formats can be detector-only cases.
Automatic benchmark cases must initially have no effective manual, imported or
reserved chapter intro marker, no previous candidate and no rejection. This
keeps higher-priority markers from hiding an automatic false positive or making
a correct detection look like an abstention. The separate native decision
journey establishes and checks Manual priority only after automatic observations
have been retained. Embedded source chapters are never stripped to meet this gate.
Unsupported playback fails honestly; the harness does not transcode or relabel
sources merely to create independent positive support. Split larger corpora into
explicitly scoped runs while retaining each run's holdout and support rules.

Independent source review and provenance are prerequisites. `human_review` and
`assistant_source_review` are distinct supported label methods. Assistant review
must retain actual visual/audio/timing evidence and is reported with
`human_reviewed=false`; it is not called human accuracy. Both methods freeze
labels before detector output and reject detector-derived or synthetic labels.
The harness neither downloads a corpus nor manufactures annotations.
Freeze an explicit `required_categories`
subset matching the admitted corpus; do not claim omitted categories or use a
stable label to stand in for source-replacement fault injection.

Episode-level, season-level and series-level holdout are declared explicitly.
The runtime always isolates different splits in different libraries and analysis
cohorts. An episode split demonstrates new independent episodes in the same
series, not new-season or new-series generalization. Variants, physical copies,
duplicate hashes and the same original episode cannot cross splits.

Manifest version1 retains its original selection and reporting behavior for
replay. Version2 additionally isolates evaluation roles and requires an explicit
`consumer_case_id`. Its `calibration` cases share a runtime library/Season when
their split and original series/season match, regardless of which attempt first
included them. Thus old C1-C3 and new C4-C6 can form one six-episode calibration
population. Old H1-H3 use a separate `regression` population; FH1-FH3 use a
separate `fresh_holdout` population. N1/N2 remain original singleton populations.
No original ground truth, source, preview obligation or threshold is rewritten.

The v2 admission verifies `original_attempt_refs` against immutable historical
manifests, label files and named receipts. Every observed case remains present;
old holdout cannot be relabeled fresh, and one transitive episode/variant/hash/
file identity cannot span roles. Actual support IDs still come from the server
and are independently checked against the role-separated corpus. Do not repeat
a source to reach the minimum support of three.

The explicitly designated v2 consumer must be a fresh-role positive MP4 with
required preview and independent identity. It is consumed first, with no
fallback if the real detector abstains or playback fails. All other original
required previews remain in the journey. The existing `ConsumerCaseId` also
selects the subsequent native decision journey, after automatic results have
been retained. Version1 continues selecting its first eligible holdout-positive
MP4 as before. The private context and receipts retain `ManifestVersion`, role
mapping and the designated ID; the source-to-catalog artifact records actual
role isolation without claiming a historical season.

Version2 requires at least three accepted independent fresh positives and two
accepted regression negatives. Its old holdout results do not inflate the fresh
gate. Fresh-negative specificity is explicitly `not_covered`; it is not a
zero-minimum pass. All 14 declared cases still require complete actual evidence,
and every miss, false positive, unsafe boundary or pending case blocks the run.
The 14-case plan fits existing 32-case/16GiB bounds without raising them; actual
source sizes and work must still pass those bounds.

Freeze the matcher/profile before fresh source review and freeze independent
labels before detector observation. Keep all earlier failures and partial media
results. A new matcher/source requires a new exact build/run attribution; these
source changes do not assert that such a release or a runnable 14-case manifest
already exists. Reuse original C1 Quality81/33-sample RGB calibration only after
reviewing unchanged source, preview implementation/profile, tools, browser and
thresholds. Do not claim an intro-matcher rerun from an old BIF result.

Unknown original seasons remain `original_season_id="unknown"` and require an
episode split. Their positive-numbered `Season 01` catalog container is named
`Unseasoned corpus cohort` and recorded as `grouping_basis="test_indexing_container"`,
`actual_catalog_season_number=1`. This only meets the product's indexed Episode
model; it does not assert a historical season. The artifact
`catalog-source-mapping.json` preserves every original identity and this mapping.
Known numeric seasons retain their original number. Other opaque season labels
retain their label in the mapping without claiming the numeric fixture alias is
a historical season. Episode variants retain one catalog episode identity.

## Execution inventory and pinned consumer

The execution JSON has these exact operational fields:

```json
{
  "Marker": "goby-media-analysis-phase2-execution-v1",
  "PythonPath": "/owned/tools/python3.13",
  "PythonSHA256": "operator-pinned-lowercase-sha256",
  "FingerprintPath": "/owned/tools/goby-intro-fingerprint",
  "FingerprintSHA256": "operator-pinned-lowercase-sha256",
  "VideoJSPath": "/owned/consumer/video.js",
  "VideoCSSPath": "/owned/consumer/video-js.css",
  "PluginModulePath": "/owned/consumer/plugin/src/videojs-bif.js",
  "PluginParserPath": "/owned/consumer/plugin/src/parser.js",
  "PluginComponentPath": "/owned/consumer/plugin/src/component/bif-mouse-time-display.js",
  "PluginDOMPath": "/owned/consumer/plugin/src/util/dom.js",
  "PluginLicensePath": "/owned/consumer/plugin/LICENSE",
  "VideoLicensePath": "/owned/consumer/videojs-LICENSE"
}
```

This is a shape example, not an admitted file. FFmpeg/ffprobe canonical paths and
hashes come from `manifest.tools`. The same tools are configured in the actual
Goby runtime and used for independent verification. The fingerprint helper is
the existing source-delivered tool described by
[`tools/intro-fingerprint/PROTOCOL.md`](../../tools/intro-fingerprint/PROTOCOL.md).
Python uses its standard library. No npm installation, webpack, online license
call or dependency download occurs in the test.

The named consumer uses the unchanged four ES modules at
[videojs-bif-updated commit 69989cee](https://github.com/samueleastdev/videojs-bif-updated/tree/69989cee2f5ffed5151042cc9aef2280ea9ce5f6).
Its [package](https://github.com/samueleastdev/videojs-bif-updated/blob/69989cee2f5ffed5151042cc9aef2280ea9ce5f6/package.json)
declares Video.js `^7.6.5`; this harness fixes exactly 7.6.5. The source modules
have no runtime imports of the package's declared lodash/jdataview dependencies.
The fixture only maps their original extensionless module URLs; it does not
rewrite or wrap plugin methods.

The plugin's [MIT license](https://github.com/samueleastdev/videojs-bif-updated/blob/69989cee2f5ffed5151042cc9aef2280ea9ce5f6/LICENSE)
and Video.js's [Apache license notice](https://github.com/videojs/video.js/blob/de21baf34f40e38209522f16e69fab8a97fa493e/LICENSE)
are required inputs and served alongside the consumer. Video.js 7.6.5's official
package git head is `de21baf34f40e38209522f16e69fab8a97fa493e`.

| Unmodified input | SHA-256 |
| --- | --- |
| [Video.js 7.6.5 browser JS](https://vjs.zencdn.net/7.6.5/video.js) | `59a717e69bec72ad009181785a1a65b674d1c01e77e04bdc718deb02a9b97671` |
| [Video.js 7.6.5 CSS](https://vjs.zencdn.net/7.6.5/video-js.css) | `e4444f0ec2ddd0aa024154b22470afa5d065650e9c07cd4593ba3047c1480f1f` |
| Plugin `src/videojs-bif.js` | `d61c5708988931ffab5eefe02c0bb78c653767d7cb5ed9d8cd6e4cc1c6fa1b06` |
| Plugin `src/parser.js` | `49dd250b3cd5bc86782217821d01f1dc72bbb65593e70ae8009b8966e7329d15` |
| Plugin `src/component/bif-mouse-time-display.js` | `e49e97ca494377300ada90356fcf9a6be7709d5b3c7b2d53cfd8bb24fe67527c` |
| Plugin `src/util/dom.js` | `b9bf9700baa7543442c537a13687f040cc10cb431e580045fc20c4befd519667` |
| Plugin `LICENSE` | `426f2636e9b4c8cccb067ea34d71a8fd04eb2cde8227a129d76274962b3f2919` |
| Video.js `LICENSE` | `c0d958248a3de7537966f6659e0176fa6a35f23afbbe37a7b23fcc096a5b3b5d` |

These hashes were recorded by reading fixed primary source assets during source
preparation, not by executing them. The plugin parser indexes images by uniform
header separation. The delivered consumer profile therefore independently
requires BIF raw ordinal timestamps `0..N-1` and the actual interval in header
milliseconds, while normal decoded thumbnail positions remain unchanged.

## Observed journeys and evidence limits

The native journey performs real sign-in, complete profile writes, a second real
writer producing a CAS conflict, retained-draft reload, explicit library starts,
terminal task observations, candidate acceptance as Manual, reject/reset while
retaining Manual priority, a Force preview run with actual cancellation, and
cache pruning. A Force run that already completed cannot pass cancellation; it
is recorded as a failed journey rather than renamed cancelled.

For each required preview case, the real plugin downloads Goby's BIF, receives
actual pointer events over Video.js's seek bar and displays its JPEG image.
First/middle/last hovers capture full visible screenshots, actual JPEG hashes,
decoded RGBA hashes/pixels, actual pointer-derived time and the plugin's tooltip.
Displayed JPEG hashes are independently mapped back to every equivalent BIF
index. A unique match records that index; duplicate JPEGs record a set and null
for unique identity. The expected slot must belong to the set and match the
actual tooltip/time and source-PTS pixels. The report never calls a requested
index an observed internal plugin index.

The protocol adapter obtains actual item-detail and PlaybackInfo marker pairs.
It enters the detected interval, clicks the real skip button and requires at
least three newly presented video frames after that click, not earlier frames
with matching timestamps. Playback reports and logout use real endpoints.
The BIF run also checks HEAD, 304, single-range bytes, rejected multi-range and
anonymous denial. ThumbnailSet supplies the tag/position for actual JPEG image
requests; decoded pixels must agree with the corresponding BIF image within the
manifest's pre-frozen calibration thresholds. Actual seek-bar dragging changes
the playing media element and produces a new frame near its requested time.

Normal restart creates a fresh Server after workers/readers are joined. The
native browser signs in again and checks unchanged configuration; the Go
observer checks selected durable state and absence of active analysis replay.
All owned browser contexts, credentials, listeners and runtime operations are
closed. Residual credential revocation is failure cleanup, never successful
normal cleanup.

The source-only parser contract suite is
`python3.13 -B scripts/test-env/test-media-analysis-phase2-evaluate.py -v` on the
designated remote worker. In addition to the five original SAR methods, seven
schema/role methods cover v1 replay, the six-member calibration population,
immutable original attempts and thresholds, consumer eligibility and aliases,
cross-role support rejection, and fresh gate counts. These are parser contracts
using metadata fixtures; they are not decoded real media or corpus acceptance.
Writing this revision did not execute that suite, a browser, build or worker.

Source-replacement injection and authority-revocation mechanics are explicitly
not claimed by this browser journey. The separate package/HTTP acceptance scope
must cover them. This is evidence for the named BIF consumer and the declared
protocol adapter, not Roku application parity, universal third-party support,
or bypass of an original commercial client's entitlement checks.

Each execution gets a fresh private artifact directory. Raw failures, partial
case records, admission/evaluation outcomes and immutable HTTP/screenshot/BIF
files remain available. A failed browser journey still invokes corpus evaluation
on its partial observations. Missing evidence remains pending; earlier failures
are not overwritten by a later successful run. The transient credential context
is removed at cleanup. Do not publish private manifests or observation files.
