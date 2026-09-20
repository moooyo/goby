# Media analysis

Media analysis is an explicit background task. It does not run during scanning,
item listing, playback negotiation, or preview HTTP delivery. The task keys are
`media.intro_analysis` and `media.preview_generation`. Both use the shared
`media.analysis` worker group. A task that lacks its configured local tools is
unavailable; the server does not invent completed work or tool identities.

The native endpoints are documented by their typed request/response structures
in `internal/server/admin_media_analysis.go`. Configuration writes use the
independent analysis revision: `PUT /admin/v1/media-analysis/configuration` accepts
`{Revision, Profile}`. Revisions are canonical decimal strings. They are never
JavaScript numbers, wall-clock timestamps, or generic server settings revisions.

## Configuration and execution identity

| Profile property | Default | Allowed range |
| --- | --- | --- |
| `AutoPublishIntros` | `true` | Boolean |
| `PreviewIntervalSeconds` | `10` | 2–120 |
| `PreviewQuality` | `80` | 40–95 |
| `MaxSourceBytes` | 137438953472 | 1–1099511627776 |
| `MaxItemRuntimeSeconds` | 1200 | 1–7200 |
| `FeatureCacheMaxBytes` | 134217728 | 1048576–536870912 |

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

## Intro evidence and decisions

Detected results live separately from `item_intro_state`. The effective order is:

1. A source-valid administrator `Manual` or `Import` interval.
2. One explicit `IntroStart`/`IntroEnd` chapter pair.
3. A qualified, enabled, unsuppressed detection with current source, profile,
   cohort and independent support.

The old manual-edit API still accepts only `Manual` and `Import` provenance.
Workers never write the manual table. The native accept action copies a selected
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
accuracy probability. Its initial thresholds are not a claim of measured accuracy.
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

Normal restart preserves configuration and current derivative references while
the task manager interrupts stale execution claims. Restore first validates raw
stored profiles, binary features, timeline bounds, result/audit shapes and all
cross-table authority facts. Normalization then increments the positive restoration
epoch, rejecting bigint overflow atomically, disables every automatic detection,
and clears feature-cache rows and preview references. It preserves immutable job
and source history, decisions, preview-clear tombstones and detection audit.
Manual intro state retains its existing source-validity rules. Restored source
roots require fresh validation; old worker authority is never replayed. Derived
files may be absent on the destination and must be regenerated explicitly.
