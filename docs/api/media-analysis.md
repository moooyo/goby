# Media analysis

The current source includes [detector v4 and resumable corpus tooling](../development/intro-quality-20260930.md).
Its visual fallback carries optional `Candidate.VisualEvidence` and zero joint
audio/visual `Metrics`; qualified evidence still requires three independent
episodes and current publication authority. The existing Docker catalog remains
the v3 checkpoint described below. Broader recognition is still limited.

Media analysis executes in background tasks. Library options independently
enable automatic intro detection and automatic seek previews. Enabling an option
or completing a scan requests later work through its dedicated durable event;
the scan itself does not perform extraction or matching. Item listing, playback
negotiation and preview HTTP delivery do not start analysis. The task keys are
`media.intro_analysis` and `media.preview_generation`. Both use the shared
`media.analysis` worker group. A task that lacks its configured local tools is
unavailable; the server does not invent completed work or tool identities.

The schema-52 automatic BIF workflow has passed its selected backend, UI and
actual Docker checks. The expanded intro assessment is complete, but broader
recognition is not accepted: v3 missed all 12 source-reviewed openings among 15
evaluable originals and correctly abstained on three short-ident negatives.
No accuracy improvement is claimed; short and variant opening recognition remains
to be improved. Final application source
`33445db2e2e64b6871116332c44605261a1bf2d4` includes the dashboard integration;
both software and AMD profiles passed actual UI and exact retained-BIF checks,
and owned-resource closure passed. See the
[BIF and intro checkpoint](../development/bif-intro-expansion-20260930.md).
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
accepts only the integer `10`, matching the existing ten-minute extraction
horizon. Responses project both booleans from the single library option and
project the fixed length; no separate scan-only mode is implemented.
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
The immutable fingerprint includes the complete profile, its revision, restoration
epoch, execution version, FFmpeg/FFprobe identities, and either the preview policy
or the fingerprint library, detector options, stream selection and audiovisual
sampling policy. The intro execution uses the fixed versioned default detector
options and 0.5-second visual sampling. Available preview execution advertises
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
independent support. The feature cache uses a versioned compact binary codec, not
JSON. Each payload is at most 256 KiB, with 8192 audio bins and 4096 visual samples;
the global configured byte limit and an 8192-row ceiling are enforced by LRU
eviction. Its key binds item, source revision and immutable profile. A conflicting
whole-content or algorithm identity cannot overwrite an existing key.

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

The matcher emits integer similarity and coverage measurements, not a calibrated
accuracy probability. Source detector v4 retains the acoustic evidence policy
and adds a separately measured visual fallback when no acoustic group exists.
Extraction uses at most the first 600 seconds; visual fallback searches at most
the first 120 seconds or first half of the episode, with 8-to-90-second intervals.
Both routes require at least three independent episodes. `VisualEvidence`
records visual-only measurements without inventing acoustic support. Same or
nested support witnesses select one existing complete group per episode;
crossing or disjoint windows do not publish. See the
[v4 result](../development/intro-quality-20260930.md) for exact policy and the
failed new positive cohort. The prior [v3 expanded evaluation](../development/bif-intro-expansion-20260930.md)
remains the original zero-of-twelve positive result; its absence of emitted
intervals made precision and boundary error undefined. The current Docker
catalog still uses that earlier detector and does not include this source change.
Review candidates, competing intervals, missing modalities, insufficient support,
analysis boundaries and search limits do not auto-publish. A completely exhausted
comparison budget records `comparison_budget_exceeded` with no candidate and the
task still reports the failure. A source that cannot be opened or completely
hashed fails the child instead of inventing a no-result content identity.

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
