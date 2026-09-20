# Real-media phase 2 acceptance contract

Status: source delivery only. Neither this verifier nor its decoder commands
have been executed as part of this delivery. A checked-in script is not a
passing corpus result. Obtain execution authorization before using these
commands; use the designated Linux `test-env`, not the local Windows machine.

`media-analysis-phase2-evaluate.py` uses Python's standard library. It does not
download media, install tools, manufacture labels, start application tasks, or
create source media. The operator provides real, licensed media; frozen labels
from independent human or assistant source review; pinned FFmpeg and ffprobe executables; and private observations captured
from actual HTTP tasks and a real browser consumer.

## Commands and result states

Run admission before bootstrap or task execution:

```text
python3 media-analysis-phase2-evaluate.py --admit-only --manifest /private/corpus/manifest.json --output /private/corpus/run/admission.json
```

After real application tasks and browser observations finish:

```text
python3 media-analysis-phase2-evaluate.py --manifest /private/corpus/manifest.json --observations /private/corpus/run/observations.json --output /private/corpus/run/result.json
```

All CLI paths are absolute. The output must not already exist. Admission does
not invoke either media executable. Full evaluation really decodes JPEGs and
source frames using the pinned tools. Linux `/proc/self/fd` is required; source
and executable descriptors stay open, and bounded child process groups are
terminated and reaped. Windows execution is rejected rather than silently
removing these safeguards.

Exit status `0` means admitted or passed, `1` means failed, and `2` means pending.
A configured invalid manifest is a failure. Only a caller with no configured
manifest may skip this opt-in corpus acceptance test. Missing observations,
missing run-time source snapshots or processed-input proof, pending tasks, missing previews, and missing
first/middle/last visible hovers cannot produce a passing result. Tool failure,
invalid evidence, source replacement, and malformed media are failures.

JSON keys below are case-sensitive and use lowercase snake case. Unknown keys,
duplicate object keys, non-finite numbers, and incompatible schema versions are
rejected. All ticks use `10,000,000` ticks per second. Integer timestamps,
nanosecond stat values, device IDs, and inode IDs must remain exact JSON integers:
the Go harness must preserve them, and browser JavaScript must not parse and
reserialize the manifest, admission, or source snapshot objects.

## Private evidence references

An `EvidenceRef` is exactly:

```json
{"path": "/private/corpus/evidence/source-review-labels.json", "sha256": "64 lowercase hexadecimal characters"}
```

References are real files with SHA-256 checked before and after reading.
`artifacts_root` is an existing canonical absolute directory owned by the
current UID with no group or other permission bits. Evidence and observation
files, BIF downloads, and output directories live beneath it with the same
ownership and privacy rules. Evidence may be next to the manifest; it need not
be inside the current run directory. Symlinked paths are rejected. Source media
and executable paths may be outside `artifacts_root`, but must be canonical
absolute regular files and remain unchanged.

An evidence reference provides traceability, not automatic proof of content
authorship, licensing, label independence, or a truthful browser capture.
Review and record these facts before freezing the manifest; either human
review or independent assistant review of the actual source is admitted.
Do not turn a self-declared `kind` value
into purported independent verification of provenance. Raw HTTP evidence may
contain private identifiers or credentials; keep it private. Public result
records contain case IDs, hashes, classifications, metrics, and reason codes,
never source paths, authors' names, URLs, credentials, or raw decoder logs.

## Manifest, schema version 1

The exact top-level fields are:

| Field | Type and meaning |
| --- | --- |
| `schema_version` | Integer `1` |
| `corpus_id` | Stable identifier |
| `label_revision` | Nonempty frozen label revision, at most 256 characters |
| `split_unit` | `series`, `season`, or `episode` |
| `artifacts_root` | Private canonical absolute directory |
| `tools` | Exactly `ffmpeg` and `ffprobe`, each `{path, sha256}` |
| `thresholds` | Object described below |
| `cases` | Between 1 and 256 case objects |

Identifiers use `[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}`. SHA-256 values have exactly
64 lowercase hexadecimal characters. Paths and prose are not identifiers.

The required `thresholds` fields are:

| Field | Constraint |
| --- | --- |
| `min_independent_positive_episodes` | Integer 3 through 256 |
| `min_holdout_positive_cases` | Integer 1 through 256 |
| `min_holdout_negative_cases` | Integer 1 through 256 |
| `max_boundary_error_ticks` | Integer 0 through 300,000,000 |
| `max_rgb_mae` | Finite number 0 through 255, frozen using calibration |
| `max_rgb_p95_error` | Finite number 0 through 255, frozen using calibration |
| `required_categories` | Optional unique list from the category vocabulary; omission requires every category |

RGB thresholds are explicit calibration policy, not measured defaults or
suggested production claims. Freeze the manifest before the run and bind its
exact bytes with `manifest_sha256`. Never rewrite labels or thresholds from
observed detector output. A changed manifest requires a new admission and run.

Each case has exactly these fields:

| Field | Type and meaning |
| --- | --- |
| `case_id` | Unique identifier |
| `split` | `calibration` or `holdout` |
| `categories` | Nonempty unique category list |
| `series_id`, `season_id`, `episode_id` | Stable physical episode identities |
| `variant_group` | Global identity shared by alternate encodes or dubs of the same episode |
| `source` | Source object below |
| `provenance` | Source and labeling provenance object below |
| `expected` | Frozen label object below |
| `preview` | Preview requirements below |

Categories are `normal_op`, `cold_open`, `recap`, `changing_op`,
`same_music_different_visuals`, `dub`, `insufficient_evidence`, `missing_audio`,
`vfr`, `source_replacement`, `no_intro`, `logo`, and `silence`.

Cases in one selected split unit cannot cross calibration and holdout. Shared
`variant_group`, SHA-256, or physical file identity also cannot cross that split.
Independent support is the transitive union of episode identity, variant group,
SHA-256, and device/inode identity. Different paths, duplicate bytes, alternate
dubs, and alternate encodes of one episode do not create independent support.
Variants may remain useful cases, but cannot inflate support or holdout counts.

An `episode` split isolates the original `(series_id, season_id, episode_id)`
identity and all its transitive variant/content/file aliases across calibration
and holdout. Its reported `scope` is `independent_new_episodes`: it measures
same-series, new independent episodes and does not establish generalization to
new seasons or series. The existing `series` and `season` split options retain
their stricter selected-unit isolation. The harness must keep actual libraries
and analysis cohorts separated by `(split, series_id, season_id)`; a holdout
episode cannot borrow calibration support.

Use the literal `season_id: "unknown"` when the original historical season is
not established. This marker is admitted only with `split_unit: "episode"`.
Never invent an original season to satisfy a test layout. The harness's
`Season 01` directory and numbering are a `test_indexing_container`, not a
claim about historical season identity. Reports retain that hierarchy role,
flag unknown original seasons, and state the generalization exclusions.

`source` has exactly:

```text
path: canonical absolute real source path
sha256: frozen full-file SHA-256
size_bytes: integer 1 through 1 TiB
video_stream_index: integer 0 through 4095, the actual container stream index
format_start_ticks: integer between -12 hours and +12 hours
duration_ticks: integer greater than 0 and no more than 12 hours
```

`provenance` has exactly:

```text
kind: "real" (synthetic test fixtures are rejected)
license_ref: nonempty license citation or reference
author: nonempty real content author/rights-holder record
authorship_evidence: EvidenceRef
label_method: "human_review" or "assistant_source_review"
labeler: nonempty identity of the human or assistant performing source review
label_evidence: EvidenceRef
```

Label evidence must describe the actual source review, its identity, the source
excerpts or observations supporting the labels, the timing method, labeling
date/revision, and narrative boundaries. Record the review modalities explicitly:
direct listening must not be claimed when an assistant cannot consume audio.
In that case, independently generated timestamped transcripts and audio-signal
evidence may support the viewed frames. Preserve their source/tool identities,
disclose transcription uncertainty, and do not treat an ASR boundary alone as
exact ground truth. Assistant source review is allowed and does not require an
external human reviewer. It must inspect evidence derived from the actual
source video/audio and create independent annotations; a plot
summary, detector output, or synthetic repeated signal cannot substitute for
that inspection. Freeze labels and their evidence hashes before detector
outputs are observed. Never derive labels from those outputs or change labels
or thresholds to make a run pass. A license reference still requires a
recorded rights/provenance review.

Public case results include `label_method` and `human_reviewed`; assistant
source review always has `human_reviewed: false`. The overall report lists
`label_methods`, uses `label_method: "mixed"` when both methods occur, and is
`human_reviewed: true` only when every case declares `human_review`. These fields
identify the evidenced labeling method, not automatic authentication of the
reviewer's identity. Assistant-labeled results must not be called human accuracy
or externally human-validated ground truth.

`expected` has exactly:

```text
kind: "positive" or "negative"
intro: {start_ticks, end_ticks} for a positive; null for a negative
safe: {start_ticks, end_ticks} for a positive; null for a negative
start_tolerance_ticks: integer within max_boundary_error_ticks
end_tolerance_ticks: integer within max_boundary_error_ticks
narrative_intervals: list of {start_ticks, end_ticks}, at most 256
```

Intervals are half-open, nonempty, source-relative, and within the media
duration. `intro` must lie within `safe`; `safe` must not overlap labeled
narrative intervals. `safe` is the widest interval the labeler permits an
automatic skip to cover. Tolerance never grants permission to cross that safe
boundary. A negative means automatic publication is forbidden, including
ambiguous recap/logo/silence or intentionally insufficient-evidence cases.

`preview` has exactly:

```text
required: boolean
width: 240, 320, or 400
interval_ticks: integer 2 through 120 seconds, a whole number of seconds
pts_tolerance_ticks: integer 0 through 10,000,000
```

`interval_ticks` is the configured minimum. A long source uses
`max(configured_interval, ceil(duration_ticks / (4096 * TicksPerSecond)) * TicksPerSecond)`.
The BIF header/index supplies the actual interval. The evaluator does not
incorrectly require the configured minimum when the 4096-frame limit forces
the larger interval. `pts_tolerance_ticks` applies only to optional independently
captured `actual_ticks`; it does not permit missing, invented, or out-of-order
PTS. The v3 profile defines exact tick flooring for its independently selected
real frame: supplied `actual_ticks` must match that selected frame, and tolerance
cannot authorize another frame or a made-up timestamp. Repeated selected source
ticks are allowed and need not be greater than their nominal slot.
For a one-frame BIF there is no observable inter-frame interval. The report
sets `interval_observed` to `false` and records the configured effective plan
instead of claiming that two index timestamps were observed.

## Admission output

`--admit-only` validates the full manifest, frozen source-review evidence, source
identity and full-file SHA-256, split/duplicate constraints, and pinned tool
hashes. It does not run either tool. On success it creates exactly:

```text
{
  schema_version: 1,
  manifest_sha256: SHA256_OF_EXACT_MANIFEST_BYTES,
  admitted: true,
  cases: [
    {
      case_id: CASE_ID,
      source: {
        sha256: SOURCE_SHA256,
        size_bytes: INTEGER,
        device: INTEGER,
        inode: INTEGER,
        mtime_ns: INTEGER,
        ctime_ns: INTEGER
      }
    }
  ]
}
```

This admission is private, because physical file identity belongs with the
private source record. Preserve the exact admission and manifest hashes in the
run. Do not use admission as the post-run snapshot: the harness must hash and
stat again after the real HTTP tasks and capture both snapshots in observations.

## Observation, schema version 1

The exact top-level fields are:

```text
schema_version: 1
manifest_sha256: hash of the exact admitted manifest bytes
run_id: stable identifier for this capture
captured_at: nonempty timestamp text, at most 64 characters
origin: "real_http"
cases: list of at most 256 unique observations using manifest case IDs
```

A case observation requires `case_id`. Its remaining fields are `task`,
`source_before`, `source_after`, `processed_source`, `detection`, and `preview`. They may be absent
while capture is incomplete; absence becomes `pending`, never a passing case.
Every complete `source_before` and `source_after` has:

```text
sha256: full-file SHA-256 measured by the real harness
size_bytes: integer
device: integer
inode: integer
mtime_ns: integer
ctime_ns: optional integer
```

Both snapshots must match the frozen source and the evaluator's current held
descriptor/path identity. The evaluator independently rehashes and restats
before and after verification. The driver must take snapshots around the
actual run, not copy source facts from a stale manifest or admission record.

`processed_source` is required for every passing case, including cases that do
not require previews. It binds the actual input opened by the application,
which can be a private byte-identical library copy rather than the original
manifest source. It has exactly:

```text
path: canonical absolute path of the actual application input
source_before: {sha256, size_bytes, device, inode, mtime_ns, ctime_ns}
source_after: {sha256, size_bytes, device, inode, mtime_ns, ctime_ns}
item_id: the actual HTTP/library item identifier
source_revision: the actual HTTP/library source revision, at most 256 characters
```

Unlike the optional `ctime_ns` on original-source snapshots, both processed
snapshots require all six fields. The harness captures them after normal scan
and before any analysis, and again after the real browser/task work. It must
retain the relationship between the processed path, item ID, and source revision
from actual library ownership. Merely rehashing the untouched original source
does not audit the bytes that the application processed.

The processed path may be outside `artifacts_root`, such as a Go-owned temporary
library directory. It must be a canonical regular file owned by the current
UID without group or other permission bits. Its full-file SHA-256 and size must
equal the frozen original source. Python opens it independently, verifies both
exact snapshots, uses that held descriptor for preview source decoding, and
retains both original and processed descriptors until the final whole-run
hash/stat recheck completes. Missing processed proof remains `pending`.

The referenced `detection.evidence` must preserve the actual native HTTP
envelope `{Status: 200, Body: ...}`. `Body.Id`, `Body.SourceRevision`, and
`Body.Detection.SourceRevision` must match `processed_source.item_id` and
`processed_source.source_revision`. This binds the processed-input receipt to
the corresponding HTTP result instead of accepting unused identity strings.
The raw HTTP and processed paths stay private and are absent from public results.

`task` has exactly:

```text
run_id: real task run identifier
status: "succeeded", "failed", "cancelled", or "pending"
http_evidence: EvidenceRef for the captured real task request/result
```

The harness maps its actual terminal success state to `succeeded`. A pending
stub may contain just `status: "pending"`. Failed or cancelled work fails
acceptance rather than masquerading as an intentional detection abstention.

`detection` has:

```text
status: "published", "abstained", "miss", "pending", or "failed"
support_case_ids: unique list of real supporting case IDs from observed evidence
reason: nonempty text captured from or explaining the actual result
evidence: EvidenceRef for actual detection/result HTTP evidence
start_ticks: required only for "published"
end_ticks: required only for "published"
```

A pending stub may contain just `status: "pending"`. Non-published terminal
states must omit both interval fields. Do not convert unknown/pending into
`miss` or `abstained`. A published positive must have at least the frozen
independent support threshold, counting the target episode once; listed support
must come from the same series/season and split. Expected labels must
not be fabricated into observed support. A labeled negative cannot
qualify as positive support.

A ready `preview` has exactly the following required fields, plus optional
`actual_ticks`:

```text
status: "ready"
bif_path: canonical absolute private path downloaded by the real HTTP/browser run
bif_sha256: SHA-256 of that response body
width: integer
height: integer
nominal_ticks: integer array captured from the actual ThumbnailSet/BIF evidence
actual_ticks: optional integer array from an actual database/audit record
browser_frames: array described below
http_evidence: EvidenceRef for actual preview requests/results
```

Before completion, `{status: "pending"}` or `{status: "missing"}` is pending;
`{status: "failed"}` fails. HTTP exposes nominal `PositionTicks`, not original
source PTS. Omit `actual_ticks` unless a real independent audit supplies it.
Never fill it by copying nominal timestamps. The evaluator computes source PTS
from the original media independently and reports its own frame matches.

Every `browser_frames` entry has exactly:

```text
index: integer BIF index
jpeg_sha256: SHA-256 of the exact embedded BIF JPEG bytes
rgba_sha256: SHA-256 of the browser-decoded full width*height*4 RGBA raster
width: integer decoded natural width
height: integer decoded natural height
pixels: array of {x, y, rgba: [R, G, B, A]}
visible: true
hover_seconds: finite number derived from the actual mouse event and seek-bar DOM geometry
tooltip_seconds: integer seconds parsed from the visible .bif-time tooltip
observed_jpeg_sha256: actual visible DOM image SHA-256, equal to jpeg_sha256
observed_equivalent_indexes: sorted unique indexes whose BIF JPEG bytes have that actual DOM hash
observed_index: the sole matching index when unique; null when multiple BIF entries are byte-equivalent
```

Capture at least index `0`, `floor(frame_count / 2)`, and `frame_count - 1`,
deduplicating them for a one-frame archive. The pixel array contains exactly
the distinct four corners and center (`floor(width/2)`, `floor(height/2)`), each
once, with integer byte channels and alpha `255`. `hover_seconds` must be
measured from the actual event and rendered seek-bar geometry, not copied from
the requested hover target. `tooltip_seconds` must equal `floor(hover_seconds)`;
both values must fall inside the evaluated nominal slot. `visible` is a real consumer visibility
assertion, not a successfully decoded detached image. The browser driver must
observe the visible seek-bar image and retain its screenshot/HTTP evidence.

The raw JPEG hash binds browser evidence to the exact BIF slice. The RGBA hash
is retained as independent browser evidence, not compared for exact equality
with FFmpeg: JPEG decoder rounding and browser color behavior can differ. The
fixed browser pixel samples are compared to the actual FFmpeg decode using
the frozen per-channel error threshold.

Python independently hashes every BIF JPEG slice and recomputes the complete
equivalence set. The reported set must match exactly and include `index`.
Repeated byte-identical JPEGs can establish correct visible bytes at the
observed hover time, but cannot uniquely identify the plugin's internal index.
They use `observed_index: null` and are reported as `index_evidence:
"byte_equivalence"`. A sole hash match uses `unique_jpeg_match` and its sole
index. The result always states `plugin_internal_index_observed: false` because
this evidence comes from visible DOM bytes rather than direct internal-state
inspection. Byte equivalence does not bypass source-PTS or source-pixel checks,
and the requested index must never be substituted for an observed index.

## Independent format and source verification

The parser reads the eight-byte magic `89 42 49 46 0d 0a 1a 0a`, little-endian
version `0`, count and millisecond multiplier, zeroed reserved header, and
exactly `N+1` timestamp/offset pairs. A zero multiplier means `1000`. The final
timestamp is `0xffffffff` and its offset is EOF. Counts are at most 4096,
archives at most 128 MiB, and individual JPEGs at most 2 MiB. Offsets are
bounded and strictly increasing. A permitted first-image padding gap does not
become image data. The independent consumer profile requires a uniform nominal
timeline beginning at zero; nonuniform files cannot pass this consumer gate.
More specifically, this consumer indexes by the header multiplier: raw frame
timestamps must be the ordinals `0, 1, ..., N-1`, and the multiplier in
milliseconds must equal the effective sampling interval. Uniform timestamps
encoded as `0, 10, 20` with a one-second multiplier do not satisfy this consumer
profile even though they describe a valid generic BIF timeline.

Every embedded JPEG is actually decoded through pinned FFmpeg into bounded
RGB24. Marker checks or JPEG header inspection alone never count as decoding.
For every browser-observed frame, including first/middle/last, the evaluator
also decodes the independently selected original source frame and compares
RGB mean absolute error and the 95th percentile of absolute channel error.
Source comparison coverage is reported explicitly; it does not claim that
every JPEG has been compared to its source when only representative hovers
were captured.

Pinned ffprobe independently obtains original packet PTS and decoded-frame PTS
using `+nofillin-genpts`; missing, duplicate, or unordered timestamps fail.
Frame PTS must exist in original packet PTS. This membership check does not
claim a complete packet-position-to-frame identity proof or eliminate every
possible decoder repair. Tick mapping is exact rational arithmetic:
`floor(pts * time_base * TicksPerSecond - format_start_ticks)`. Selection itself
compares exact rational source timestamps before that flooring. Under
`source-pts-display-preceding-hold-jpeg-v3`, binary search selects the greatest
decoded source PTS at or before the nominal slot, proving its presentation
interval `[sourcePTS, nextSourcePTS)`. A long VFR frame may therefore serve
multiple nominal slots, and `ActualTicks` can repeat or be less than nominal.

Before the first real source frame, the preview policy explicitly holds that
first frame backward while retaining its real positive `ActualTicks`. After
the complete source decode reaches real video EOF, the preview policy holds
the last decoded frame until the admitted container duration. These are
declared preview holds, not claims that natural video presentation starts
earlier or that the video stream itself lasts as long as the container. No
last nominal slot is dropped, no PTS is invented, and the successful full
ffprobe decode is required before any final-frame hold can be accepted.

The independent RGB comparison selects the proven original decoded ordinal
and checks its real PTS again. Repeated slots can reuse the same previously
verified source raster with a one-frame memory bound; the report distinguishes
slot comparisons from unique source decodes. It records the selection policy,
source ordinal, source PTS, next source PTS where present, and hold counts.
Source PTS remain strictly ordered in the complete decoded trace; selected
slot PTS are nondecreasing and may be equal. No frame-number multiplication,
guessed frame rate, `fps`, `tpad`, seek rebasing, or nominal-to-actual timestamp
copying is used. Orthogonal display matrices and
sample aspect ratio are independently checked; output geometry follows the
actual rotated display ratio and square output pixels.
The separate source RGB decode must also emit exactly one matching
`showinfo@phase2_source` PTS/time-base record; merely asking a filter for a PTS
does not constitute successful timestamp verification.

Decoder runs have bounded output, diagnostics, source pixels, source frames,
input formats/protocols, one decode thread, and wall time. A case has a
30-minute decoding budget and each child a 10-minute ceiling. Exceeding a
budget is a reported failure, not silently reduced coverage. The verifier
does not claim that a bounded successful process establishes codec security.
SIGTERM and SIGINT handlers only set a cancellation flag. Bounded hash reads,
timestamp/pixel loops, and process selection loops check it cooperatively;
handlers never throw through child-ownership registration. The process cleanup
path sends TERM with a 250 ms grace, then KILL, and waits within a two-second
teardown budget before descriptor scopes are released. A failure to reap is
an explicit failure, never successful cleanup. This source-level budget is
not an executed cancellation result or a guarantee against uninterruptible
kernel I/O. A cancellation cannot write a passing/admitted result.

## Acceptance and retained counts

Each case retains `false_positive`, `miss`, `abstention`, `boundary`,
`narrative_safety`, `pending`, and `failure` counters. A positive abstention is
both an abstention and a miss; it cannot pass. A negative abstention can pass
the no-publication condition when its other required evidence is complete.
A published negative is a false positive. Tolerance failures are boundary
failures. Leaving the independently labeled safe interval or overlapping narrative content is
a narrative safety failure even if a numerical boundary tolerance was met.

Acceptance requires zero false positives, positive misses, boundary failures,
and narrative safety failures; no failed or pending case; the independent
positive threshold; independent holdout positive and negative coverage; and
every required category. Incomplete corpus coverage remains pending. Counts
are retained per case and per category; category totals can overlap because
one case may intentionally belong to several categories.

The result also records exact manifest/observation hashes, independent support
counts, BIF facts, actual JPEG decode count, source-comparison count, observed
source PTS, representative pixel-error measurements, and browser/raster hashes.
Reports do not create a substitute corpus, redefine ground truth from current
detector behavior, or convert pending evidence into successful acceptance.

`mechanical_coverage_exclusions` explicitly lists source-replacement fault
injection, cancellation fault injection, authorization revocation, and HTTP
range/cache behavior. These require separate real HTTP mechanics scenarios.
A stable case labeled `source_replacement` cannot establish a replacement
experiment. If that category is required in this manifest, its category gate
remains pending with `mechanical_coverage_out_of_scope`. Freeze an explicit
`required_categories` subset for this real-content corpus and retain the
separate mechanics result. Category labels alone never establish those faults.
The default full vocabulary therefore cannot silently promote absent mechanics
into a passing comprehensive claim. Public results include a hash of the label
revision rather than its potentially private free-form text.

## Contract references

The independent wire parser follows the pinned
[Roku BIF version-zero specification](https://github.com/rokudev/dev-doc/blob/27e4a3cdfdeffabaf64b9959193e45e39bb12ad3/docs/DEVELOPER/media-playback/trick-mode/bif-file-creation.md#L155).
Uniform timestamps are required for the pinned
[Video.js BIF consumer](https://github.com/samueleastdev/videojs-bif-updated/tree/69989cee2f5ffed5151042cc9aef2280ea9ce5f6).
Source-relative PTS and display transforms align with Goby's
`source-pts-display-preceding-hold-jpeg-v3` and `orthogonal-display-sar-v1` profiles in
`internal/media/analysis_preview.go`, `analysis_preview_hold.go`, `analysis_preview_hold_log.go`,
`analysis_visual_pts.go`, and `analysis_geometry.go`. These references specify
the implemented contract; they are not records of a successful corpus run.
