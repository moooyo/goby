# Media analysis runtime

Current intro execution uses the in-process Go port of Intro Skipper 12.0.4.0,
commit `6e0cb179007ac4c16cd9f358e9a617e791e9bf06`, with raw FFmpeg Chromaprint
extraction. The [port record](intro-skipper-port-20261001.md) separates matching
parity, production extraction and integration verification. New work uses
execution version 6, GAFB v4 and schema 54. A Jellyfin host or C# runtime is not
required. Source/toolchain recipe updates do not claim a newly built or deployed
Docker image.

The earlier schema-52 [BIF and intro checkpoint](bif-intro-expansion-20260930.md)
retains its verified software/AMD UI and BIF scope, original v3 intro misses and
resource-closure evidence. Later
[v5 refinement](intro-quality-round2-20260930.md) and the
[native Intro Skipper evaluation](intro-skipper-native-evaluation-20261001.md)
remain separate algorithm records. None of those historical measurements is
silently relabeled as a new production integration result.

## Deployment inventory

`GOBY_MEDIA_ANALYSIS_FILE` names an exact JSON object in a bounded regular file.
Omitting the variable leaves execution disabled and opens no derivative cache.
For example:

```json
{
  "enabled": true,
  "cacheDirectory": "/var/cache/goby/media-analysis",
  "cacheMaxBytes": 2147483648,
  "cacheMaxEntries": 512,
  "maxEntryBytes": 536870912,
  "maxFileBytes": 134217728,
  "introFFmpegPath": "/opt/ffmpeg/9.0.1/bin/ffmpeg"
}
```

The cache must be a private, independently owned directory outside media roots,
other caches, operation scratch, diagnostics and recovery stores. The configured
limits count file lengths and reservations, not filesystem allocation units.
Leave free-space margin for metadata and the underlying filesystem.

`introFFmpegPath` is optional. It selects the executable used for raw intro
fingerprints; omission falls back to the main configured FFmpeg path. An optional
`introFFmpegSHA256` requires that explicit path and pins its lowercase SHA-256.
The executable must expose the `chromaprint` muxer with raw output, algorithm 1
and a disabled silence threshold. Startup captures and revalidates the admitted
tool identity, and jobs bind the effective intro binary in `FingerprintSHA256`.
A missing muxer makes intro execution unavailable; it does not authorize the
legacy helper or an alternate extraction recipe as a silent fallback.

The existing FFmpeg/FFprobe inventory and preview requirements remain separate.
`fingerprintPath` and `fingerprintSHA256` remain supported deployment fields for
the [legacy fingerprint helper](../../tools/intro-fingerprint/README.md), but
that helper is not required by new Intro Skipper extraction. Current extraction
uses Linux descriptor-based execution and held source/tool identities. No tool
path is accepted from an HTTP request. Verify a configured binary's capabilities
against the updated source recipe; an earlier deployed image need not contain
the newly required muxer.

## Editable policy and task behavior

TV libraries have a default-false `LibraryOptions.EnableIntroDetection` option.
It is the current policy control for automatic intro publication. Enabling it
records a durable `IntroAnalysisRequested` event, as does successful completion
of independent and task-owned scans of enabled libraries. Extraction and matching
run in the background task, not in the scanning request.

Movie, TV and mixed video libraries independently expose default-false
`LibraryOptions.EnablePreviewGeneration`, labelled **Automatic seek previews**.
Enabling it and completing independent or task-owned scans record
`PreviewGenerationRequested`. Intro and preview requests have separate durable
counters. Neither scan performs the extraction itself.

The first installation of each untouched enabled analysis-task definition adds a
24-hour interval and its dedicated event trigger. Existing custom, cleared or
disabled schedule choices are preserved. Initial event cursors include requests
committed before default-trigger installation. Automatic intro runs select only
enabled TV libraries; automatic preview runs select only preview-enabled movie,
TV and mixed libraries. An empty eligible set never becomes an all-library scan
or analysis. Events that overlap active work retain a request for a fresh snapshot.

Graceful shutdown of unfinished automatic intro or preview work persists its
corresponding replacement request. Startup recovery also requests fresh admission
for abandoned automatic runs after a crash. Old claims become interrupted;
current library policy is re-evaluated for new work. Explicit user stops,
maximum-runtime stops and manual runs do not create automatic retries.

The native media-analysis page stores its own configuration revision in
PostgreSQL. Defaults are a ten-second minimum
preview interval, quality 80, 128 GiB per source, 1,200 seconds per source task,
and 128 MiB of persisted compact feature data. Configuration updates require the
complete profile and its current revision. They withdraw old automatic/preview
publications and cached features while retaining decisions and audit history.
A real profile change also commits the relevant intro and preview requests for
enabled libraries. For example, a preview interval change from ten to twenty
seconds rebuilds all three widths through a new automatic run. A no-op CAS does
not invalidate references or emit a rebuild request.
The legacy `AutoPublishIntros` Boolean remains in the wire profile, with new
writes canonicalized to true and no UI toggle. Schema 51 migrates only old false
settings to true, increments their revision and invalidates their derived
references. Historical profiles and manual/imported state remain compatible.
Schema 52 adds the preview option and its event without rewriting existing library
rows or replacing administrator schedule choices. Missing preview options on
older rows mean disabled.

The dashboard also edits all seven `Profile.IntroSkipper` settings. Defaults
are 25 percent, ten minutes, a 15–120-second intro duration range, six differing
fingerprint bits, a 3.5-second matching gap and an inverted-index shift of two.
The [API contract](../api/media-analysis.md#configuration-and-execution-identity)
lists their exact units and bounds. These are upstream matching/extraction
options; there is no custom boundary-offset control. Schema 54 installs the
default options without advancing the current revision or publication epoch,
clearing derivatives or withdrawing existing source-valid v5 markers. Historical
execution versions 1–5 remain readable with their original admission hashes.

The two analysis tasks are published in both native and compatibility task
collections, including truthful unavailable status when execution is disabled.
Selection may combine library and item identifiers; selected items must belong
to the selected libraries. Empty explicit selection follows the task's supported
scope; automatic admission remains restricted to libraries enabled for that kind.
Request receipts are immutable and can recover a response lost after admission.
An incompatible active selection/profile returns a conflict. Scheduled conflicts
defer that occurrence without consuming its event cursor or delaying unrelated
definitions.

The `intro-skipper-v1` matcher requires an independent pair. Intro work retains
at most 32 same-season sources per child and at most 16 selected publication
targets. Episode keys establish original-work identity; distinct complete-file
hashes additionally prevent duplicate content from counting twice. The task
retains admitted cohort order because the upstream first-valid-pair decision
is order-sensitive.

Current audio selection prefers the local stream with the greatest channel
count, then the lowest original stream index. The extraction helper can apply
upstream preferred-language fallback, but the current task requests no language
preference. Default-track flags add no priority. External tracks and attached
pictures are excluded. Extraction uses stereo `-f chromaprint -fp_format raw`.
The full raw sequence remains on the upstream fingerprint clock, without
source-PTS shifts or trimming to the old audiovisual bins.

At defaults, sources shorter than five minutes use their full duration;
otherwise the requested window is the first 25 percent, capped at ten minutes.
The admitted profile freezes the configured percentage, time limit and matcher
options. GAFB v4 stores up to 5,000 raw words, exact extraction horizon and
source/profile bindings. It cannot contain legacy visual, refinement or audio
uncertainty fields. Historical GAFB v1–v3 remain readable; a new job cannot reuse
them as raw fingerprints. A current source-bound v4 cache can avoid repeated
extraction, while Force requests repeat it.

The port keeps the upstream `start <= 5 seconds` snap to zero and actual
two-source support. It omits `TimeAdjustmentHelper`, chapter/silence adjustment,
keyframe and end snapping, custom offsets, visual confirmation and three-source
quality gates. The three protected-range overlap cases in the retained native
assessment remain known behavior under the stricter offline reference policy;
they do not become an extra runtime rejection rule.

Preview tasks similarly reuse only a complete set of current variants whose
actual sealed bytes and BIF indexes pass acquisition. A missing variant causes
regeneration; an unsafe cache is a failure, not a cache miss. Force regenerates
all width variants.
Reuse and HTTP delivery also bind database dimensions and exact nominal/actual
timeline hashes to the same generation's sealed application manifest. A valid
BIF hash alone cannot authorize unrelated metadata from a corrupted reference.

The preview library option governs future automatic admission and publication,
not access to existing valid derivatives. Disabling it preserves current BIF and
thumbnail references, including across normal recreation. A running automatic
worker rechecks the policy before publication and cannot publish after a completed
opt-out. Source/profile invalidation, explicit clearing and cache eviction still
withdraw affected references. Compatible explicit preview requests remain
independent of this automatic option.

Preview work admits one source per child and writes three width variants. The
effective whole-second interval is the greater of the configured interval and
the minimum that fits 4,096 source slots. Thus twelve hours with a ten-second
setting uses eleven-second slots. Each BIF is at most 128 MiB and respects the
configured file limit. Scratch, all final variants and cache control records fit
inside one reservation. A small deployment budget can make a source unavailable;
it never authorizes an oversized output or a partially ready generation.
Generated BIFs store zero-based frame ordinals with the actual interval in
milliseconds as the header multiplier. This preserves exact nominal timestamps
and supports the pinned Video.js BIF consumer's fixed-interval indexing without
modifying that consumer. ThumbnailSet positions remain the same media ticks.

The generic task manager shares one analysis slot and rotates between waiting
runs at child boundaries. A canceled process or storage operation retains its
slot until it actually returns. Per-source deadlines include hashing, extraction,
feature-cache writes and preview publication. Cohort matching and intro publication
share a separate bounded deadline. Playback requests and preview GETs do not enqueue analysis.
An intro child also has a two-hour overall deadline across its complete source
loop, matching and publication, even when each source's individual limit is high.

## Publication, reads and cleanup

Files become a sealed, pinned generation before database publication. The
database transaction rechecks the task capability, source/cohort stamps,
configuration and administrator decisions. A shared cancellable gate serializes
that transaction plus `Keep` against native reference pruning. An uncertain
database commit retains the generation for later reconciliation; it is never
assumed to have rolled back merely because the caller received an error.

Preview responses use at most four concurrent derivative leases. They check the
current credential, media ACL and physical source, register for source retirement,
then acquire an immutable cache lease. Revalidation precedes conditional responses
and repeats during long reads. Source replacement, revoked access, shutdown or
the request deadline cancels delivery and closes the lease. Known credential
expiry is an independent deadline.

The native prune operation checks the reviewed configuration revision and current
administrator authority, preserves currently referenced generations, and removes
only owned unused entries. Its counts include only actual removals; active readers,
builders and pending publications remain charged. Shutdown cancels admission and
work, joins actual operations, closes the derivative cache, and then permits the
catalog owner to close. Timing out a shutdown caller does not complete that work.

Qualified results publish automatically for enabled TV libraries. `review` and
`no_result` publish no automatic marker and require no human decision. The current
UI exposes library switches, status/progress/errors, configuration and cache
controls. Normal manual preview-start and Force controls are removed; retained
request receipts can still be resolved. Historical accept/reject/reset,
Manual/Import and explicit preview-start APIs remain compatible.

Turning off the intro library option immediately withdraws detected publications;
turning it back on requests new work instead of promoting retained evidence.
An explicit false-to-true transition retires the library's legacy rejection
flags, advances decision revisions and preserves decision/audit records. Manual,
imported and chapter markers remain independent.
Qualified automatic intros are resolved against the target and supporting files
for single-item details and playback information. Manual/import and explicit
chapter intervals take precedence. Batch catalog DTOs retain their inexpensive
manual/chapter projection rather than claiming physical verification for every
support file on a list request. Stale or temporarily unprovable automatic evidence
is hidden without making ordinary playback fail.

Restore invalidates automatic publication proofs and discards feature/preview
references tied to the earlier runtime. Settings, manual state and audit history
remain durable. Regeneration uses a new admission after current source-root
validation rather than reviving an archived execution. See the
[Phase 2 execution record](media-analysis-resilience-phase2-20260921.md) for actual
verification outcomes and the [recovery contract](media-analysis-recovery.md)
for archive and normalization boundaries.
