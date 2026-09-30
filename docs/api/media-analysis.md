# Media analysis

Current intro execution uses the pure Go `internal/introskipper` port of
Intro Skipper 12.0.4.0, pinned to
`6e0cb179007ac4c16cd9f358e9a617e791e9bf06`. It preserves the upstream Introduction
raw-candidate matcher, including pair support and its at-most-five-second start
snap. It runs inside Goby without a Jellyfin or C# runtime. See the
[port record](../development/intro-skipper-port-20261001.md) for scope and
verification status. Source recipes have changed; a new built or deployed image
is not implied by this API contract.

Media analysis executes in background tasks. Library options independently
enable automatic intro detection and automatic seek previews. Enabling an option
or completing a scan requests later work through its dedicated durable event;
the scan itself does not perform extraction or matching. Item listing, playback
negotiation and preview HTTP delivery do not start analysis. The task keys are
`media.intro_analysis` and `media.preview_generation`. Both use the shared
`media.analysis` worker group. A task that lacks its configured local tools is
unavailable; the server does not invent completed work or tool identities.

The earlier schema-52 automatic BIF workflow passed its selected backend, UI
and actual Docker checks at source `33445db2e2e64b6871116332c44605261a1bf2d4`.
Its v3 intro assessment missed all 12 source-reviewed openings among 15
evaluable originals and abstained on three short-ident negatives. That remains
historical evidence in the
[BIF and intro checkpoint](../development/bif-intro-expansion-20260930.md),
separate from the current implementation and its known upstream behavior.
The earlier automatic-intro delivery retains its completed, source-bound scope
in the [previous increment record](../development/library-intro-automation-20260930.md).

The native endpoints are documented by their typed request/response structures
in `internal/server/admin_media_analysis.go`. Configuration writes use the
independent analysis revision: `PUT /admin/v1/media-analysis/configuration` accepts
`{Revision, Profile}`. Revisions are canonical decimal strings. They are never
JavaScript numbers, wall-clock timestamps, or generic server settings revisions.

## Library policy and automatic requests

`LibraryOptions.EnableIntroDetection` defaults to `false` and may be enabled only
for `tvshows` libraries. Missing values in existing library rows mean disabled;
unrelated options and existing library revisions are preserved by migration.
The library switch is the current control for automatic intro publication.
`LibraryOptions.EnablePreviewGeneration` independently defaults to `false` for
`movies`, `tvshows` and `mixed` libraries; it cannot be enabled for music libraries.
The native UI calls this second switch **Automatic seek previews**.

Creating an enabled TV library or changing the option from false to true commits
the durable `IntroAnalysisRequested` event. Successful completion of an enabled
library's independent or task-owned scan records another request. The task
manager consumes these requests separately from scanning. Automatic runs select
only enabled TV libraries; an empty eligible selection does not expand to all
libraries.

Creating a preview-enabled library, a false-to-true preview edit, or successful
completion of its independent or task-owned scan commits
`PreviewGenerationRequested`. Automatic preview admission selects only eligible
preview-enabled libraries. The intro and preview counters are independent, and
neither empty automatic selection becomes an all-library request.

Each untouched analysis-task definition receives a 24-hour interval and its
dedicated event trigger. Existing custom schedules, deliberately cleared triggers
and disabled task definitions are preserved. Thus a saved library option does not
override an administrator's explicit task scheduling decision. A request arriving
during an active analysis run remains pending for a fresh source snapshot.

Disabling the intro library option immediately withdraws its detected publications.
Changing it back to true requests new work; it does not reactivate old automatic
results. Stored evidence remains inspectable, and historical source-valid
Manual/Import markers and explicit chapter markers retain their precedence.
An explicit false-to-true transition retires that library's legacy rejection
flags, advances their decision revisions and retains the decision rows and audit
history. It still requires fresh publication rather than promoting old evidence.

Disabling preview generation prevents new automatic admission and publication;
it leaves existing source-valid BIF and thumbnail references readable. It is not
a cache-clear operation. Source/profile invalidation, explicit clear and cache
eviction retain their ordinary effects. Explicit compatibility preview-start APIs
remain available independently of the automatic option.

The Emby library-option mapping uses `EnableMarkerDetection` as the main switch.
If `EnableMarkerDetectionDuringLibraryScan` is supplied, the main switch must
also be supplied and both booleans must agree. `IntroDetectionFingerprintLength`
accepts only the legacy integer `10`, representing the maximum ten-minute
extraction horizon. Actual new-work windows use `Profile.IntroSkipper`; this
compatibility field does not edit those options. Responses project both booleans
from the single library option and project the fixed length; no separate
scan-only mode is implemented.
The preview mapping uses `EnableChapterImageExtraction`; if
`ExtractChapterImagesDuringLibraryScan` is supplied, the main preview switch must
also be supplied and both booleans must agree. Both response fields project the
single preview-generation option.

## Configuration and execution identity

| Profile property | Default | Allowed range |
| --- | --- | --- |
| `AutoPublishIntros` | `true` | Legacy Boolean wire field; new writes canonicalize to `true` |
| `PreviewIntervalSeconds` | `10` | 2–120 |
| `PreviewQuality` | `80` | 40–95 |
| `MaxSourceBytes` | 137438953472 | 1–1099511627776 |
| `MaxItemRuntimeSeconds` | 1200 | 1–7200 |
| `FeatureCacheMaxBytes` | 134217728 | 1048576–536870912 |
| `IntroSkipper.AnalysisPercent` | `25` | Integer 1–50, percent |
| `IntroSkipper.AnalysisLengthLimit` | `10` | Integer 1–10, minutes |
| `IntroSkipper.MinimumIntroDuration` | `15` | Integer 1–600, seconds |
| `IntroSkipper.MaximumIntroDuration` | `120` | Integer from `MinimumIntroDuration` through 600, seconds |
| `IntroSkipper.MaximumFingerprintPointDifferences` | `6` | Integer 0–32, differing fingerprint bits |
| `IntroSkipper.MaximumTimeSkip` | `3.5` | Finite number 0–30, seconds between matching points |
| `IntroSkipper.InvertedIndexShift` | `2` | Integer 0–32, neighboring fingerprint-key radius |

`Profile.IntroSkipper` and all seven fields are required on configuration
writes. The dashboard exposes the same options and units. Missing values are
not defaulted during a write; zero remains meaningful for the three options
whose ranges include it. `MaximumTimeSkip` is a matching-gap parameter, not an
intro start/end offset. There are no custom boundary-offset controls. Duration
limits preserve the pinned upstream selection rule, including its asymmetric
right-hand candidate maximum check.

`AutoPublishIntros` remains in stored/wire profiles for compatibility. It is not
a second current publication switch: new configuration writes normalize it to
`true`, including requests carrying legacy `false`. The administrator UI does
not expose it. Historical execution profiles retain their original values.

`MaxItemRuntimeSeconds` limits processing time, not source duration. Indexed media
duration must be positive and at most 12 hours. `PreviewIntervalSeconds` is a
minimum: complete long timelines increase the interval to the smallest integral
number of seconds that fits 4096 frames. The result is
`max(configuredSeconds, ceil(durationTicks / (4096 * 10000000)))` seconds.

A real profile change increments the analysis revision, withdraws preview
references and automatic intro publication, and clears disposable features.
Detection evidence and administrator decisions remain auditable. A no-op CAS
preserves the revision and derived references. Invalidation emits a bounded
library resynchronization covering the configuration's whole catalog audience;
it does not claim that every item changed. Notification journal capacity errors
roll back the configuration and its invalidation atomically.

A real profile change also commits a fresh request for each analysis kind with
enabled libraries: `IntroAnalysisRequested` and/or `PreviewGenerationRequested`.
The newly admitted work captures the new profile. A no-op update creates neither
an invalidation nor a rebuild request. A preview interval or quality change thus
requests automatic rebuilding without a manual start.

Cache root and disk quota are startup configuration. Paths and receiver secrets
do not enter this profile. Admission receives an already inspected tool inventory
and performs no filesystem access or process invocation inside its SQL owner.
The immutable fingerprint includes the complete profile, its revision,
restoration epoch, execution version 6, FFmpeg/FFprobe identities, and either
the preview policy or the Intro Skipper extraction profile and exact seven
matcher options. For intro execution, `FingerprintSHA256` binds the effective
Chromaprint-capable FFmpeg executable, `DetectorVersion` is `intro-skipper-v1`,
and `IntroSkipperOptions` equals the admitted `Profile.IntroSkipper`.
`DetectorOptions` and `VisualIntervalTicks` carry no legacy audiovisual policy
in this execution. Available preview execution advertises
exactly widths 240, 320 and 400. An unavailable execution snapshot contains only
version, availability and one fixed reason; it has no fabricated hash values.

## Admission and ownership

Admission, generic task children, immutable job profile, work windows and source
facts commit in one owned SQL transaction. The library does not import the task
manager. Its `AnalysisFence` callback is a process capability supplied by the
task manager, checked before business locks and again as the final database
operation after journal and catalog event flushing. Request cancellation is
checked around both boundaries. A task ID or deserialized work object alone is
not execution authority. Interrupted or replaced claim tokens cannot publish.

Leaf requests contain at most 256 unique item IDs and 64 unique library IDs.
Intro cohorts remain within one physical library, series and positive-numbered
season. The independent episode identity combines library, series, season number
and positive episode number; alternate encodes do not manufacture independent
episodes. Specials and incomplete hierarchy abstain. Movies and auxiliary videos
can receive explicit unsupported-type intro results, but never a detected intro.

Whole-season analysis partitions targets into at most 16 per window and adds
nearby support up to 32 total sources. Every selected episode has one publication
owner. A leaf request may read unselected episodes in its same cohort as support
but never writes their results. Preview children each own one source. The full
cohort roster has a deterministic invalidation digest, so a changed member outside
the current window also invalidates that work. Admission has an explicit 100000
source ceiling and fails rather than silently truncating an over-limit population.
The existing protected owner transaction also bounds admission time; large-library
admission throughput must be measured on the deployment rather than inferred
from the matcher window size.

Snapshots bind the indexed source stamp, root binding, hierarchy, duration, size,
manual intro revision, decision generation and preview-clear generation. Sources
are reopened through the catalog's contained media descriptor path outside SQL
transactions. Analysis retains its worker slot until actual filesystem work and
descriptor cleanup finish after cancellation. HTTP requests retain their existing
bounded early-return source-worker behavior.

The executor supplies a complete-file SHA-256 after a bounded full read and verifies
its held source before and after extraction. A prefix fingerprint is not content
identity. Duplicate episode, source, or complete-content identities do not count as
independent support. The feature cache uses a versioned compact binary codec,
not JSON. New intro extraction writes GAFB v4 with up to 5,000 complete raw
`uint32` fingerprint points, the exact binary64 extraction horizon, profile
and complete-content digest. It does not mix in legacy audio bins, visual
samples or uncertainty. The current payload ceiling is 512 KiB; GAFB v1/v2
retain their historical 256 KiB limits. V1–v3 remain readable in their original
formats. The configured global byte limit and 8,192-row ceiling are enforced
by LRU eviction. Cache keys bind item, source revision and immutable profile;
old visual features cannot serve as raw fingerprint data for a new job.

## Intro evidence and compatibility decisions

Detected results live separately from `item_intro_state`. The effective order is:

1. A source-valid administrator `Manual` or `Import` interval.
2. One explicit `IntroStart`/`IntroEnd` chapter pair.
3. A qualified, enabled, unsuppressed detection with current source, profile,
   cohort and independent support.

Qualified current results become effective automatically for enabled TV
libraries, subject to the existing source/support checks and precedence.
`review` and `no_result` do not publish an automatic marker and do not create a
required human-review step. The normal administrator workflow shows status,
progress and errors; it has no accept/reject/reset or manual intro editor entry.

Compatibility APIs remain available. The old manual-edit API still accepts only
`Manual` and `Import` provenance. Workers never write the manual table. The
retained native accept action copies a selected
qualified or review candidate into the manual layer through the same source and
manual-revision CAS. Reject records a source-bound suppression tombstone. Reset
clears suppression but does not automatically promote an old candidate; rerun
analysis to republish. Decisions increment a persistent generation even when no
detection exists. The public decision revision is the maximum of detection and
decision revisions, including canonical `"0"` before either exists.

Detection DTO statuses are `not_analyzed`, `qualified`, `review`, `no_result` and
`stale`. `SourceRevision` and `ManualRevision` describe the current source and
manual CAS in the same read. Historical candidate support retains its original
source keys. The absent DTO has revision `"0"`, an empty candidate, reason
`not_analyzed`, and an unrecorded timestamp. Arrays are never omitted merely
because they are empty. A rejection is represented by `Suppressed`, independently
of the underlying evidence status. Replacement does not carry suppression to new
source bytes.

New detections expose `IntroSkipperCandidate` with `Interval`, `UpstreamCommit`
and the actual two-member `Support` pair. Each support entry retains episode,
source, whole-content and algorithm identities and its winning-pair interval.
The peer's eventual final candidate can differ from that recorded pair.
Legacy detections retain `Candidate`, including historical visual evidence;
the two candidate fields are not populated together. New pair evidence is not
converted into visual metrics or a three-episode consensus claim.

The current matcher requires two distinct episode/source/content identities
and retains ordered first-valid-pair behavior. The default audio window is the
whole source below five minutes; otherwise it is the first 25 percent, capped
at ten minutes. The configurable values above replace those defaults in new
admissions. Stereo raw Chromaprint points keep the upstream clock
`4096 / 11025 / 3` seconds per point. The matcher retains its intrinsic
`start <= 5 seconds` snap to zero. It adds no source-PTS correction, custom
offset, visual verification, three-source clique, chapter/silence adjustment,
keyframe snap or end-snap postprocessing.

Current automatic publication uses that upstream candidate behavior with
Goby's source, task, profile and authorization fences. The retained
[native evaluation](../development/intro-skipper-native-evaluation-20261001.md)
found three protected-content overlap cases under the stricter corpus reference
policy. Those measurements remain visible; they are not an additional online
quality gate that changes upstream candidate selection. A resource failure
returns no partial candidate. Exhausting the comparison budget records
`comparison_budget_exceeded` and the task still reports failure. A source that
cannot be opened or completely hashed fails the child instead of inventing
a no-result content identity. Historical
[v5 audiovisual policy](../development/intro-quality-round2-20260930.md) remains
readable and is separate from this execution.

Single-item and playback delivery resolve actual target and support sources under
current authorization before advertising a detected interval. A hidden, removed,
rebound, rescanned or physically replaced support withdraws the automatic result.
Failure to verify derived evidence hides that result without making otherwise
valid ordinary playback fail. Cheap paged inventory and bulk DTOs may retain audit
candidate status but do not claim a physically verified detected effective marker.

## Previews

The database stores only source-bound references: random opaque cache key, seal,
dimensions, SHA-256, bytes, frame count, effective interval and a packed little-endian
timeline of `(nominalTicks, actualTicks)` int64 pairs. There are at most three width
variants per item, 4096 frames, 2048 pixels of height and 128 MiB per variant. Actual
size must also be at least `72 + 12 * frameCount` bytes for the BIF header, complete
index and JPEG start/end markers. This is a necessary metadata bound; the cache and
BIF reader still verify actual bytes. Actual
timestamps are nondecreasing; duplicate actual frames are valid for sparse frame
rates. Nominal positions cover the full duration at the effective interval.

All admitted widths publish in one transaction after actual source revalidation.
Non-forced work can reuse only a complete width set with the same frozen profile
and current source/clear generation. The runtime verifies every cached artifact's
seal and content hash and revalidates the source before marking that work complete.
Force bypasses both feature and preview reuse.
The filesystem cache independently seals and owns files. Missing, corrupt or evicted
derivatives degrade to absent previews and can be regenerated. HTTP never starts
extraction. Clear withdraws references and advances `analysis_preview_state` even
when no reference currently exists, fencing work admitted before the clear. It
never deletes or opens a user-supplied filesystem path. Pruning operates only on
the configured derivative cache with a current, authorized database keep-key set
and runtime coordination against concurrent publication.

## Restart, backup and restore

Schema 50 adds `analysis_settings`, `analysis_run_profiles`, `analysis_work`,
`analysis_work_sources`, `analysis_feature_cache`, `analysis_detections`,
`analysis_detection_sources`, `analysis_intro_decisions`, `analysis_intro_audit`,
`analysis_preview_state` and `analysis_previews`. Task history gains closed analysis
input, profile identity, source authority fields and nonempty analysis child scopes.
Historical non-analysis tasks retain their original columns and empty analysis
defaults. Schema 49 and every earlier published migration remain unchanged.

Schema 51 adds the library option and dedicated task event without rewriting
existing library option rows. Only an old global `auto_publish_intros=false`
setting is migrated to true: its analysis revision increments, automatic
detections are withdrawn, and disposable feature/preview references are cleared.
An already-true setting does not undergo that profile invalidation. Historical
Manual/Import data, decisions and audit records are preserved.

Schema 52 adds the default-false preview library option and independent
`PreviewGenerationRequested` event. Existing library option rows, revisions and
prior schedule choices are retained. It does not replace the BIF format or the
source/profile-bound preview storage contract.

Schema 53 added GAFB v3 refinement storage. Schema 54 adds the closed
`analysis_settings.intro_skipper_options` object with the seven upstream
defaults. The migration does not increment configuration revisions or
publication epochs, rewrite old executions, clear derivatives or withdraw
existing source-valid v5 detected markers. Historical executions 1–5 and
GAFB v1–v3 retain their original decoders and hashes. New work uses execution
6 and GAFB v4. A later real profile update still performs the ordinary atomic
invalidation and rebuild request described above.

Normal restart preserves configuration and current derivative references while
the task manager interrupts stale execution claims. Graceful shutdown of unfinished
automatic intro or preview work records its corresponding durable request.
Startup recovery does the same for abandoned automatic runs after a crash,
atomically with interruption of the old claims. The replacement admission
evaluates current library policy; it
does not resume old execution authority. Explicit user stops, maximum-runtime
stops and manual runs do not generate automatic replacement work.

Restore first validates raw
stored profiles, binary features, timeline bounds, result/audit shapes and all
cross-table authority facts. Normalization then increments the positive restoration
epoch, rejecting bigint overflow atomically, disables every automatic detection,
and clears feature-cache rows and preview references. It preserves immutable job
and source history, decisions, preview-clear tombstones and detection audit.
Manual intro state retains its existing source-validity rules. Restored source
roots require fresh validation; old worker authority is never replayed. Derived
files may be absent on the destination and require newly admitted generation
after source-root validation; old archived references are not reused as proof.
