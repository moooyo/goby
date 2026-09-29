# Media analysis runtime

Current runtime contract, including schema-52 automatic seek previews. The BIF
workflow is **verified within its selected scope**. The expanded intro assessment
is complete, but broader recognition is **not accepted**: all 12 source-reviewed
openings were missed, while three short-ident negatives correctly received no
intro. The [current checkpoint](bif-intro-expansion-20260930.md)
separates those outcomes and binds the application candidate. The previous
automatic-intro increment retains its completed backend, UI, Docker and
resource-closure scope in its [record](library-intro-automation-20260930.md).
Earlier Phase 2 verification is
complete, including its client lifecycle and independent resource closure. See the
[Phase 2 execution record](media-analysis-resilience-phase2-20260921.md) and
[recorded results](media-analysis-resilience-phase2-results-20260922.json).
The [original fixture failure](media-analysis-resilience-phase2-20260921.md#first-fourteen-source-run-and-cancellation-fixture-correction)
remains part of that history.

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
  "fingerprintPath": "/opt/goby/bin/goby-intro-fingerprint"
}
```

The cache must be a private, independently owned directory outside media roots,
other caches, operation scratch, diagnostics and recovery stores. The configured
limits count file lengths and reservations, not filesystem allocation units.
Leave free-space margin for metadata and the underlying filesystem.

An optional `fingerprintSHA256` pins the helper's exact lowercase SHA-256.
The runtime also captures FFmpeg and FFprobe hashes at startup and binds jobs to
the held executable identities. The analysis profile currently requires Linux,
FFmpeg 9.0.1 and FFprobe 9.0.1. See the
[fingerprint helper](../../tools/intro-fingerprint/README.md) for its pinned
Chromaprint/KissFFT source, license notices, build and protocol. Missing helper
availability prevents intro execution while independently available preview
generation remains usable. No executable path is accepted from an HTTP request.

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

The two analysis tasks are published in both native and compatibility task
collections, including truthful unavailable status when execution is disabled.
Selection may combine library and item identifiers; selected items must belong
to the selected libraries. Empty explicit selection follows the task's supported
scope; automatic admission remains restricted to libraries enabled for that kind.
Request receipts are immutable and can recover a response lost after admission.
An incompatible active selection/profile returns a conflict. Scheduled conflicts
defer that occurrence without consuming its event cursor or delaying unrelated
definitions.

The unchanged `introdetect-v3` matcher requires at least three independent
episodes. Intro work
uses at most 32 same-season sources per child and at most 16 selected
episode identities for publication. A source's complete content hash establishes
independence; a shared opening fingerprint does not establish an independent
episode. The normalized stream policy selects a default local audio/video stream
first, then its original stream index. External tracks and attached pictures are
excluded. A current source-bound feature cache can avoid repeated extraction.
Force requests repeat extraction.
The completed expanded assessment returned `no_result` for all 15 evaluable
originals: 12 missed source-reviewed openings, three correct short-ident negatives,
zero qualified intervals and zero false positives. Precision and boundary error
are undefined without emitted intervals. Calibration experiments and held-out
series retain separate roles. No experiment was adopted and no quality gate was
relaxed; short/variant openings and insufficient consistent audiovisual evidence
remain limitations rather than accepted expanded recognition.

Intro extraction admits at most the first 600 seconds. Its visual request covers
only complete sampling intervals within that horizon: with the default 500 ms
interval, a 137.005-second source requests visual samples before 137.0 seconds.
It does not invent a frame at 137.0 seconds or copy an earlier frame into that
slot. A source shorter than one sampling interval has no admissible intro visual
window. `IntroFeatures.WindowTicks` remains the overall admitted audio horizon;
`VisualWindowTicks` records the visual request's relative end separately.
The fixed `intro-visual-complete-slots-v1` policy participates in the extraction
profile, so historical profiles cannot silently supply current feature data.
Generic visual requests and explicit end times retain their strict slot contract.
Missing internal slots, truncated bytes, unmatched timestamps and decoder errors
still reject extraction; this rule changes the intro request, not those checks.

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
