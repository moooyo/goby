# Movie SpecialFeatures implementation plan

Latest execution: **continuation tool03 scanned the existing library successfully
(`Completed`, 6/6/0), then failed an incorrect parent-count assertion.** Job
`d7aa0acaee023dd4c82ea7a303c354ca` retained the library/root IDs below. The
inherited `84af.capture` expected `SpecialFeatureCount=3`, whereas the correct
sampled response omits that field and returns `LocalTrailerCount=1`, matching
the 20-case reference. Do not alter product behavior to satisfy this assertion.
The [failure](m3e-positive-continuation-count-check-failed.json) is retained;
both new admin/viewer tokens were revoked with logout204/exact401. No full/range
media request had started.

The [response proof](m3e-positive-protocol-response-proof.json) passed six direct
reads, eight lists and the parent read: all 15 complete200, with correct
membership, paths, owner and sources. The [snapshot review](m3e-positive-protocol-snapshot-review.json)
proves original 13-item/all-old-row preservation, new 9 items/four Extra resources/
three markers, three revoked sessions, exactly nine new audits and exact
sequences. Current counts are 35 tables, 22 items, 4 libraries, 5 UserData rows,
24 plays, zero encoding, 61 global sessions and 135 audits. Current state is
`continuing_special_features_fixture`/`scan_complete`, SHA-256
`0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83`;
PID748513/start ticks6996875, schema27, binary and runtime remain unchanged.

Next is the authorized independent protocol-finalization/inspection chain in
[extras verification](verification-m3e-extras.md): reuse the 15 responses and
perform only one full200/Range206 pair per resource under a fresh ordinary AV
token/device, with logout204/exact401, 11 HTTP exchanges total. Preserve all
old `550b6f83cd804485cf227ee3514fb6dec0af550d61ec5926303ce589047877f8` snapshot rows; only one session/device and two audits
may be added. Global 62 sessions/137 audits are anticipated, not actual results.
Do not rescan/recreate or mark failed trees successful. Positive UI, ledger
and main consumers must use the final chain. Main consumer03 passed 26 guards
and its build but is undeployed; primary remains 26. Both execution failures
and both preflight diagnostics remain preserved.

Historical setup01 execution: **it created the positive library with create201, then
failed at its private phase checkpoint before any scan.** The [12 pure guards](m3e-positive-fixture-tool01-guards.json),
two syntax checks and preflight passed. The [retained failure](m3e-positive-fixture-create-failed.json)
and [diagnosis](m3e-positive-fixture-phase-diagnosis.json) show that saved stage
`library_acknowledged` was followed by rejection of
`phase-library_acknowledged.json` under `[a-z0-9-]+`. Neither root SELECT nor
scan POST ran. This is an operator filename defect, not a product scan failure.
The original administrator logged out 204/exact401; the viewer never logged in.

Library `57a85c1ca5b6c7ae602c587755250b2f` and root
`604d2c0f5c78919a6ee360cda2048066` now exist at the exact permitted Movies
path with relative path `.`. The v1 profile is paused at phase
`preparing_special_features_fixture`, stage `library_acknowledged`, state
SHA-256 `513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1`.
PID748513/start ticks6996875, runtime and binary were unchanged; counts at
that failure were 35 tables, 14 items, 4 libraries/roots, 14 metadata rows, 15 Theme
owner rows, 59 global auth rows and 129 activity entries. All old rows/media
were preserved; both Extra tables were empty at that checkpoint.

The subsequent tool03 run used the authorized independent continuation/inspection
scopes recorded in [extras verification](verification-m3e-extras.md), binding
the original failure, create201 acknowledgment and old evidence trees. Do not
recreate/delete the library, replay setup01 or write success into its old v1
tree. Use a new administrator for the existing-library scan, then a viewer
for protocol and complete/range delivery. The three-stage profile and UI,
ledger and main consumers must separate creation, continued scan and viewer
actors. The independent snapshot review now proves three sessions/nine audits
and exact sequences. The scan completed; only the remaining full/range checks
may be continued under the separate finalization scopes. Primary remains source28/schema26.

Established checkpoint: **source32 published; remote regression, build,
candidate upgrade, original-Movie dual-user flow and precise media-root extension passed**.
The candidate now runs source32/schema27; the primary remains source28/schema26.
The schema27 catalog was generated on
PostgreSQL17. After two retained source30 fixture failures, the corrected
[source31 targeted run](m3e-source31-extras-targeted.json) passed 105 tests
with zero failures/skips and complete cleanup. Its full run later found stale
fixture table inventories and was stopped. Native Trailer name-control fixes
were also added after static review. The source32 expanded targeted run passed
137 race tests, followed by the [complete remote regression and build](m3e-source32-extras-full.json):
1,873 top-level race tests across 24 packages, zero failures/skips, all six
cleanup checks true and unit exit code 0. Its source manifest is
`a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65`;
the built executable SHA-256 is
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
The [candidate schema26-to27 upgrade](m3e-source32-candidate-upgrade.json)
completed with ready/complete status as PID746709/start ticks6930051. It
preserved all preexisting rows, columns, relation OIDs, ACLs and sequences in
the old 33 tables, plus credentials, media and recovery state. Only the
expected schema27 migration row was appended to an old table; both new tables
are empty. That upgrade preserved runtime configuration. The [product checkpoint](source32-product-publication.json)
is published at `b9bb7b1`. The [original-Movie dual-user flow](m3e-source32-cross-user-original-movie.json)
then passed Home-to-Movie-to-Home, genuine PlaybackInfo200/finished transfer,
SpecialFeatures200 `[]`, zero page errors, own200/foreign403, unchanged four
item-UserData projections/preferences/Configuration/Policy and UI logout204/
exact-token401. Each WebSocket closed without cleanup failure; each user
retained one blocked-resource console warning/error. Its [scoped comparison](m3e-source32-cross-user-original-movie-comparison.json)
permits the recorded preparation/authentication changes and is not a
whole-database preservation claim.

The [precise extension](m3e-source32-extra-root-extension.json) subsequently
added only `/opt/goby-fixtures/client-special-features-m3e-v1/Movies`, preserving
all 35 table rows/sequences, credentials, recovery state and media, with zero
HTTP calls, library creation or scans. That extension recorded 13 items, three
libraries, 35 tables and two empty Extra tables at PID748513/start ticks6996875,
before the later setup01 creation above.
The original upgrade's PID746709 receipt remains unchanged. The [first preflight failure](m3e-source32-extra-root-preflight-failed.json)
is retained: tool01 and its mock used `encoding_states` instead of actual
`encoding_jobs`; no output directory or state/environment/service change
occurred. Tool02 corrected the name, added an actual catalog regression and
passed two syntax checks, 11 pure guards, preflight and execution.

Next, independently finalize the remaining full/range checks and verify the
positive original-client flow. Completed media/reference phases must not be replayed.
Positive-extra acceptance, primary schema27, library restriction and the full
M3/M4/M5/M6 scope remain open; earlier failures remain retained.

The historical source28 actual-client failure was a `404` for
`GET /emby/Users/{UserId}/Items/{Id}/SpecialFeatures`. The parent task confirmed
the retained failure evidence with two hash comparisons. Existing
[movie extras contracts](m3e-reference-movie-extras-contracts.json) establish an
empty response for sampled movies. The subsequent
[positive reference capture](m3e-special-features-positive-contracts.md)
establishes three SpecialFeatures with `Type=Video`, `ExtraType=Clip` for
featurettes and `DeletedScene` for deleted scenes, plus a separate
`Type=Trailer`/`ExtraType=Trailer` LocalTrailers resource. Detailed projection
uses the actual Movie parent ID; defaults omit ParentId and media sources.
The observed scope supplies initial naming, membership and DTO evidence;
ordering stability, remaining layouts, access variants and playback still
need their own gates.

The product objective is to index and deliver real movie attachments, including
an authorized empty result when no active attachment is indexed. A permanent
empty-array handler is not the implementation or acceptance target.

## Proposed data model

Use an additive migration 27 with two tables:

- `item_extra_resources`: resource item ID as primary key, owner item ID,
  bounded resource kind, and active status; both IDs reference `items`.
- `extra_reserved_paths`: registered root ID, canonical relative path, and
  directory flag; directory reservations cover their entire subtree.

This proposal produces a **35-table schema 27** from the current 33-table
schema 26. Preserve existing migrations, catalogs, and the theme-owner numeric
namespace. New `items` rows continue receiving their existing theme-owner
mapping through the schema-26 trigger, without exposing that number through
SpecialFeatures.

Each attachment uses a stable `items.id`, its own probed media facts and user
state, and `parent_id = owner_item_id`. Internal `Video` is the proposed playable
type. Initial external projections follow the observed Video/Clip,
Video/DeletedScene and Trailer/Trailer mappings. The trailer item name is
the Movie title plus ` - Trailer`; its media source name retains the actual
filename stem. Other category mappings remain unproven.

An active attachment must be a nonfolder resource whose ordinary Movie owner,
resource, and registered root share the same library and root. Its path must be
reserved. Inactive history retains resource identity, library, parent relation,
and reservation, while allowing the owner to move or cease to qualify for active
ownership. Theme and extra membership must be permanently mutually exclusive
for a resource ID, including inactive history. Separate table primary keys do
not establish this cross-table invariant.

## Product integration points

| Area | Existing integration point | Planned behavior |
| --- | --- | --- |
| Path classification | `internal/library/scan.go`: `walk`; `theme_paths.go` | Identify proved extra layouts before ordinary scanning, album detection, and movie-owner discovery. Honor the outermost auxiliary reservation boundary. |
| Probe and identity | `themes_scan.go`: `inspectScannedMedia`; `scan.go`: `findStoredFileForRole` | Reuse anchored descriptors, probe cache, ctime, and rename checks. Replace the theme boolean with explicit roles and shared claims; reject cross-role identity reuse. |
| Publication | `themes_scan.go`: `persistThemeMarkers`, `publishThemeOwner`, `deactivateInvalidThemeChildren` | Reserve before promotion; publish a complete owner batch under ordered item locks. Preserve prior accepted resources after incomplete scans; retire only with complete-scan and verified-absence authority. |
| Authorization and query | `application_key_access.go`: `beginSubjectRead`; `query.go` | Add an association-based query with owner, resource, application authority, target-user scope, and UserData in one read snapshot. Ordinary browsing excludes attachments; the separately observed explicit-Ids retrieval may return valid Extras while retaining all parent/filter/ACL intersections and the existing Theme exclusion. |
| Visibility | `database/theme_visibility.go`; `library/theme_visibility.go` | Current-schema ordinary visibility excludes both permanent roles and both reservation sets. Direct visibility permits only ordinary items or active, valid, mutually exclusive attachments. Preserve historical version semantics separately. |
| Protocol adapter | `server/libraries.go`, `server/namespace.go`, `server/items.go` | Register and canonicalize the user-item SpecialFeatures route; reuse `itemUser`, request subjects, DTO projection, image switches, and UserData switches. Add consistent attachment attributes to list and direct-item projections. |
| Delivery and state | `media_source.go`, `play_sessions.go`, `userdata.go` | Reuse the existing Video source ID, authorized playback, anchored file delivery, and per-user state. Keep lock-then-recheck classification ordering and ordinary-only folder aggregates. |

Only an accepted, unambiguous Movie may own the initial implementation. Extra
files must not first become ordinary Movies: that would pollute browsing and
can also make existing theme-owner discovery report multiple movies. Failed
probe, ambiguous ownership, changed directories, or resource overflow must not
publish a truncated or guessed owner population.

`findStoredFileForRole(..., theme=true)` currently permits any stored role.
It cannot be reused unchanged. Both scanners and both direct predicates must
reject simultaneous theme/extra membership, and publication must serialize
role changes on the affected item rows.

## Metadata and sidecars

Use the stable-ID and native override/lock preservation boundary in
`persistThemeFile` and `syncScannedMetadata`. Do not infer inheritance of the
movie's NFO, genres, people, artwork, or subtitle files from the existing theme
implementation.

Theme publication currently does not call `scanImages` or `scanSubtitles`.
Extra sidecar support therefore requires explicit indexing and stable directory
observations. Generic Video image candidates include directory poster/fanart;
positive evidence must settle whether a same-directory extra may use those or
only its own filename-prefixed images. External subtitles must belong to the
resource's own source and basename.

Native metadata listing excludes auxiliary resources, but
`metadata_store.go:readMetadataRecord` permits administrator edits by known
Video ID without an ordinary/direct visibility check, including inactive
resources. Preserve or explicitly revise that existing policy; do not assume
the central visibility change covers the administrative editor.

## Migration and history decision

Migration 27 must explicitly account for an extra reservation that makes an
existing active theme owner cease to be ordinary. For example, a newly accepted
extra directory layout may already contain a catalog item that owns active
theme resources. Adding the reservation changes the owner's eligibility even
if no old business row is updated.

It is invalid to preserve those old rows unchanged, enable the new reservation,
and leave the upgraded database rejected by strict current-schema theme-state
validation. Hiding the theme in direct reads does not repair its persisted
active state. Weakening validation to accept this contradiction is not a
solution.

Select transactional deactivation of precisely affected active Theme
relationships. Validate schema 26 with its original predicates first. Derive
new reservations only from canonical stored paths and the accepted layout
scope, without guessing attachment owners or creating attachment items during
migration. Lock affected owners and active resource rows in deterministic ID
order, publish markers, and set only the relationships whose owners lose
ordinary eligibility through these new markers to inactive. Strict schema-27
validation must pass before the same transaction commits. Do not broadly
deactivate every currently invalid Theme relationship, which could hide old
corruption.

This transition explicitly permits the affected relationship `active` flags
to change. Preserve their resource/owner identities, item rows, sequences,
metadata and UserData, and leave unrelated/inactive history unchanged. Apply
the same atomic rule when a later scan publishes a reservation or changes an
owner. Both normal migration and recovery migration need the source/current
semantic checks within their existing transaction boundary.

## Backup and restore integration

Add the schema-27 PostgreSQL 17 catalog baseline and historical-version coverage
alongside the migration. `internal/backuppg/catalog.go`, row fingerprints, dump
decoding, and recovery drop plans are catalog-driven; no new archive format is
proposed.

Preserve the schema-26 validation boundary. `ValidateThemeState` currently
reaches `ThemeOrdinaryItemSQL` through its resource predicate. If that predicate
unconditionally queries the new extra tables, restoring a schema-26 archive
fails before migration because those tables do not exist. Freeze the v26
internal predicate or make the internal semantics version-aware. Version 27
must enforce the combined visibility and cross-role invariants without relaxing
active theme-owner eligibility.

`backuppg/restore.go` validates source-schema semantics before original row
fingerprints, then migrates and validates current semantics, runs the finalizer,
and validates again. Integrate the new checks at all applicable stages and in
`backuppg/snapshot.go`, `backuppg/recovery.go`, and the shared semantic-error
wrapper. Historical validation must remain valid before the new tables exist;
post-migration validation must prove the selected transition succeeded.

## Evidence and verification gates

Implementation cannot be accepted before positive reference evidence settles
recognized layouts, nested and conflicting layouts, owner selection, filename
and metadata naming, DTO defaults and selected Fields, `Type`/`ExtraType`,
ordering and request controls, and non-Movie, missing, and unauthorized seeds.
Observed empty responses do not determine these contracts or Emby internals.

All verification must run through `ssh test-env` unless local verification is
explicitly authorized in the current task. If that environment is unavailable,
verification is blocked; do not fall back to local tests or runtime probes.

Required gates for a future candidate:

- Positive real-file scan, association query, direct detail, original delivery,
  and independent two-user state; exact observed protocol projections.
- Ordinary browse, Latest, Similar, entities, counts, and folder state remain
  free of active and inactive extras; direct access rejects invalid or inactive
  resources and respects current user/application authority.
- Ambiguous ownership, both-role conflicts, reserved subtrees, rename/hardlink
  identity, failed probe, source replacement, partial scans, cancellation,
  overflow, disappearance, and owner changes preserve publication invariants.
- Nonempty active/inactive backup round-trip and corruption rejection for
  parent, root, owner, reservation, and cross-role state; finalizer failure
  rolls back and permits a clean retry.
- Schema-26 archive restore reaches schema 27 without querying new tables in
  the old-schema phase. Cover an existing active theme owner affected by a new
  extra reservation, proving the selected migration transition rather than
  bypassing validation.
- Freeze and verify the candidate artifact, run the required remote regression
  gates, then repeat the original client's movie flow with two users and real
  positive extras. A missing-route fix alone does not complete M3/M4/M5/M6.

This planning change performs no tests, builds, HTTP requests, runtime probes,
reference capture, migration, or deployment.
