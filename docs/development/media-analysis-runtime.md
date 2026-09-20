# Media analysis runtime

Implementation record for phase 2. This document describes the code contract;
the phase has not yet completed its consolidated remote verification.

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

The native media-analysis page stores its own configuration revision in
PostgreSQL. Defaults are automatic publication enabled, a ten-second minimum
preview interval, quality 80, 128 GiB per source, 1,200 seconds per source task,
and 128 MiB of persisted compact feature data. Configuration updates require the
complete profile and its current revision. They withdraw old automatic/preview
publications and cached features while retaining decisions and audit history.

The two analysis tasks are published in both native and compatibility task
collections, including truthful unavailable status when execution is disabled.
Selection may combine library and item identifiers; selected items must belong
to the selected libraries. An empty selection means all supported libraries.
Request receipts are immutable and can recover a response lost after admission.
An incompatible active selection/profile returns a conflict. Scheduled conflicts
defer that occurrence without consuming its event cursor or delaying unrelated
definitions.

Intro work uses at most 32 same-season sources per child and at most 16 selected
episode identities for publication. A source's complete content hash establishes
independence; a shared opening fingerprint does not establish an independent
episode. The normalized stream policy selects a default local audio/video stream
first, then its original stream index. External tracks and attached pictures are
excluded. A current source-bound feature cache can avoid repeated extraction.
Force requests repeat extraction.
Preview tasks similarly reuse only a complete set of current variants whose
actual sealed bytes and BIF indexes pass acquisition. A missing variant causes
regeneration; an unsafe cache is a failure, not a cache miss. Force regenerates
all width variants.
Reuse and HTTP delivery also bind database dimensions and exact nominal/actual
timeline hashes to the same generation's sealed application manifest. A valid
BIF hash alone cannot authorize unrelated metadata from a corrupted reference.

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

Qualified automatic intros are resolved against the target and supporting files
for single-item details and playback information. Manual/import and explicit
chapter intervals take precedence. Batch catalog DTOs retain their inexpensive
manual/chapter projection rather than claiming physical verification for every
support file on a list request. Stale or temporarily unprovable automatic evidence
is hidden without making ordinary playback fail.

Restore invalidates automatic publication proofs and discards feature/preview
references tied to the earlier runtime. Settings, manual state and audit history
remain durable. See the phase execution record and recovery contract for required
verification; authored source and fixtures are not passing evidence.
