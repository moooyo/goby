# AMD media and client compatibility plan

Status: **the approved three-phase increment is verified and closed within its recorded boundaries**.
Recorded on September 19, 2026 against `main` at `b15de9a`.
The user approved execution after the three-phase revision. Implementation used
`codex/amd-media-compatibility`, with existing unrelated work preserved. On
September 20, 2026, code commit `80198b6aa8a163b696ceaff64da831847d82e496` was
fast-forward merged into `main` and pushed to `origin/main`; the
[handoff](../development/handoff.md) records the repository checkpoint.
Phase 1 source, selected software/AMD verification, builds and documentation
closeout are complete. See the
[phase 1 execution record](../development/amd-media-phase1-20260919.md).
Its `source11` acceptance remains unchanged. The
[phase 2 implementation record](../development/amd-media-phase2-20260919.md)
tracks final source-bound results, preserved failures and accepted owned
PostgreSQL/worker/documentation closeout. Phase 2 is closed. The
[phase 3 record](../development/amd-media-phase3-20260919.md) and
[results ledger](../development/amd-media-phase3-results-20260919.json) track
preserved failures and final source16/schema41 acceptance. Composed server
coverage, selected package/media scopes, maximum-image backup/recovery at 1 GiB,
the final twelve-stage real UI journey, both actual source14 application builds
and explicit input equivalence are accepted. Forty-two workers and owned
PostgreSQL runtime are closed with source/receipt preservation and a hashed local
handoff. The failed 512 MiB profile is not accepted. This completes the selected
increment, not full original Emby Web, OCI, non-AMD, provider-online or broader
capacity/platform delivery; no deployment is claimed.

## Selected scope and priority

The user selected the following work, with advanced media and AMD acceleration
as the highest priority. OCI playback, image/deployment work, upgrades and
container recovery remain deferred. Intel, NVIDIA and other GPU profiles are
also deferred; they must not inherit acceptance from an AMD result.

Remote inventory found the AMD device on `pve`, with no GPU mapped into VM 101
(`test-env`). The user approved a dedicated GPU-test LXC as an explicit
verification-environment exception. CT 104 (`goby-amd-worker`) now exposes the
render node to its non-root worker. Capability enumeration is recorded in the
phase 1 execution record, alongside selected actual-media results and their
closed scope and profile boundaries.

The previous software-media, collections and management increment retains its
recorded evidence. At that feature-wave/planning baseline, its P3 completion
table overstated user configuration writes: `POST /Users/{Id}/Configuration`
was absent from the router, and UserDto updates did not persist embedded
Configuration. The corrected [feature-wave record](../development/feature-wave-20260919.md)
retains that historical error. Phase 3 now implements the dedicated persistent
adapter and selected consumers with separate recorded acceptance.
The existing `UserSettings` partial API remains a separate contract.

## Phases and completion gates

The user requested exactly three phases grouped by function. Execute them in
order, with advanced media and AMD first. Each phase includes implementation,
remote verification, necessary repairs and documentation closeout before the
next phase starts. Preparation belongs to its owning phase; there is no separate
preparation or final acceptance phase. Work within a phase may be delegated in
parallel, while shared verification workloads remain coordinated and serialized
on `test-env`.

| Phase | Functional deliverable | Remote verification before closeout | Documentation closeout |
| --- | --- | --- | --- |
| 1. Media processing and AMD | Establish the actual AMD/toolchain baseline and supported profile contracts; implement HEVC/AV1 output, applicable AMD decode/encode, Dolby Vision conversion, accelerated HDR/deinterlacing/subtitle burn-in, progressive MP4 burn-in and broader copy/remux seeking | Inspect real encoded/copied output and exercise representative clients; verify color, cadence, subtitle rendering, seek accuracy, A/V sync, hardware decode/encode/combined execution, fallback, cancellation, builds and affected regressions | Update codec/container/profile support, actual AMD capabilities and CPU/GPU boundaries, configuration, media contracts, source-bound results, current status and handoff |
| 2. Subtitles and dynamic playback | Build on the closed phase 1 media paths; implement multiple HLS subtitle tracks, rolling subtitle windows, standard subtitle-playlist routes, switching/off/offsets, dynamic-source subtitles and bounded replay/time shifting | Exercise real-client subtitle switching, pause/resume, seeking within the retained window and live-edge return; verify media/subtitle clocks, reconnection, discontinuities, eviction, authority, storage limits, cleanup, builds and affected phase 1 regressions | Update subtitle and dynamic-source APIs, buffering/restart/retention policy, supported playback combinations, source-bound results, current status and handoff |
| 3. Library and client/management compatibility | Implement library editing/directory browsing, preferences, richer user state, images/avatars/entity state, music tags/APIs, navigation/query/event compatibility, NextUp/refresh journeys, management configuration and task/system-event contracts | Verify administrator and client workflows, persistence, permissions, concurrent edits, migrations, restart behavior, events and tasks; complete final cross-phase regressions, builds and owned-resource closure on the integrated source | Update API inventory/contracts, administrator/configuration guidance, support matrix, verification records, execution plan, progress and handoff; retain explicit unsupported or unverified profiles |

## Detailed feature inventory

### Phase 1: Media processing and AMD

- Establish the remote AMD/device/toolchain baseline and define the selected
  codec, color, seek and subtitle-processing contracts before implementation.
- HEVC and AV1 output, including codec/profile/bit-depth negotiation, encoder
  limits, compatible delivery containers and truthful client capabilities.
- AMD hardware decode and encode measured independently and together; retain
  H.264 and existing direct/remux/progressive/HLS behavior.
- Dolby Vision input handling and color conversion, with source profile,
  layer structure, metadata use and output color space explicitly recorded.
- AMD-accelerated HDR processing, deinterlacing and text/bitmap subtitle burn-in;
  progressive MP4 subtitle burn-in as well as the existing HLS path.
- Broader copy/remux seek support with source validation, random-access
  boundaries, audio synchronization and an explicit fallback policy.

### Phase 2: Subtitles and dynamic playback

The selected implementation, verification, final builds and owned PostgreSQL/
worker/documentation closeout are complete within the recorded scope.
The [phase 2 record](../development/amd-media-phase2-20260919.md) owns the current
fixed eight-track subtitle-view and bounded stored-output replay contracts.

- Multiple HLS subtitle renditions, rolling subtitle windows, standard
  `subtitles.m3u8` and `live_subtitles.m3u8` compatibility routes, selection,
  switching, off and offsets.
- Dynamic-source subtitle tracks and bounded time-shift storage, replay,
  pause/resume, seek, live-edge return and resource lifecycle.

### Phase 3: Library and client/management compatibility

Library and user features:

- Rename libraries; add, replace and remove media paths; edit the selected
  library options; browse and validate server directories within approved roots.
  Preserve stable catalog identities, user state and access rules where the
  operation contract requires it; document path-move and rescan behavior.
- Implement `DisplayPreferences` and `POST /Users/{Id}/Configuration`, with
  validated fields, current authority, persistence and appropriate user/client
  isolation. Connect settings to their actual consumers.
- Implement HideFromResume, supported UserData updates and ratings, and adapt
  legacy PlayingItems reports into the existing playback state machine.
- Upload, delete and reorder media artwork; manage user avatars; add images
  and independent user state for supported person/genre/studio entities.
  Distinguish public-login avatar visibility from protected media artwork.
- Add Artists, AlbumArtists and MusicGenres API families, richer local music
  tags, composer relationships, track/disc numbers and explicit multi-artist
  handling. Integrate the existing MusicBrainz metadata path without treating
  it as proof of local tag extraction or provider-service acceptance.

Navigation, events and administration:

- Add supported ancestors, counts and additional-parts navigation; complete
  the agreed filter, field and sort contracts required by client journeys.
  Evaluate legacy adapters such as Search/Hints against observed client needs.
- Implement real session-list subscriptions and reconcile current event
  delivery with client refresh. Existing LibraryChanged, UserDataChanged and
  remote commands remain supported.
- Close the selected global NextUp selection/order and automatic-refresh
  journeys. Distinguish an implementation defect from missing or inconclusive
  reference/client evidence; avoid repeating exhausted discovery work.
- Enumerate additional management configuration fields and their consumers,
  validation/default/reset/reload rules. Publish implemented native task
  capabilities through the compatibility API where their contracts apply.
- Define supported system-event triggers and their lifecycle, coalescing,
  cancellation and persistence behavior before exposing them.

## Proposed technical boundaries

These are recommended defaults for final scope agreement, not new acceptance
claims or grounds for silently dropping a requested feature.

1. **Dolby Vision:** target supported Dolby Vision inputs converted to HDR10 or
   SDR. Define single-layer and dual-layer profile behavior separately, including
   enhancement-layer limits. Authoring new Dolby Vision encoded output is a
   separate scope decision. Applying metadata must affect pixels correctly;
   relabeling color tags alone does not complete the feature.
2. **AMD output:** HEVC/AV1 output is a product requirement. Hardware support is
   specific to the actual GPU, driver and toolchain. If the available GPU cannot
   encode a selected codec, implement the software output and record that the
   corresponding AMD hardware profile remains unavailable/unverified; a CPU
   result must not be described as hardware acceptance.
3. **Accelerated subtitles:** typography or subtitle layout may execute on the
   CPU while video processing/composition/encoding use the GPU. Record transfers
   and actual execution stages. A mixed pipeline is not an all-GPU pipeline.
4. **Copy seeking:** define supported random-access points and presentation
   accuracy. Arbitrary frame-exact seeking with packet copying is not promised
   for every source; alignment or permitted transcoding remains explicit.
5. **Time shifting:** use a configurable bounded window of media the server has
   actually buffered. During phase 2 preparation, set duration/storage/concurrency
   limits based on the intended usage and measured resources. Define restart retention and
   reconnection semantics explicitly. Earlier content requires prior buffering
   or an upstream replay source. Full channel/tuner/EPG management, scheduled
   recording and provider-specific historical catch-up remain separate work.

## Verification and documentation rules

Ordinary inventory, formatting, builds, tests, runtime probes, browser work and
software-media execution run through `ssh test-env`. The user explicitly
authorized AMD-specific verification in the dedicated `pve` CT 104, reached
through `ssh pve` and `pct exec 104`. No local verification is authorized.
Remote work must preserve existing services and evidence, use owned resources and
record the actual source, toolchain, GPU and selected fixture identities.

Complete and integrate each phase's implementation before its remote
verification. Repair discovered failures and verify the affected final source,
then close its documentation before starting the next phase. Do not postpone
phase 1 or 2 verification and documentation until phase 3. Phase 3 closeout also
covers final cross-phase interactions and the required regression scope; it is
not a separate fourth phase. Do not repeat unchanged passing checks solely to
create another receipt.
Tests must inspect real media output and client behavior, not only command
construction, route registration, JSON fields or encoder enumeration.

For each phase:

- Record implementation, remote verification and deployment separately.
- Verify permissions, persistence, concurrent mutation, cancellation, recovery
  and cleanup appropriate to the changed behavior.
- Update the implemented API inventory, detailed API contracts, media support
  matrix, configuration/administrator documentation and relevant acceptance
  record before calling the phase complete.
- Update current status and the handoff with remaining failures or blocked
  profiles. Preserve original historical results and their source boundaries.
- Update the execution plan, progress and support/delivery matrix for that
  phase; unresolved required behavior must not be marked complete.

At phase 3 closeout, reconcile the final integrated source and all three phase
records without transferring acceptance across changed source implicitly.
Documentation and code remain English; user-facing
conversation remains Chinese. Implementation approval does not by itself
authorize production promotion, publication or unrelated cleanup.

## Explicitly deferred work

OCI playback and container deployment/upgrade/recovery remain deferred by the
user's latest decision. Non-AMD GPU execution, native arm64, broader host/storage
delivery claims and external release decisions remain independent obligations.
Provider-specific online acceptance remains under the existing deferral unless
the user changes it; new music and subtitle source integration must preserve
that distinction.

Live TV channels, tuners, EPG, scheduled recording, DLNA, offline sync, group
playback and unrelated extensions are not implicitly added by dynamic-source
time shifting. The consumer web player, Emby Connect and proprietary binary
plugin compatibility remain outside the project's current scope.

## Reference anchors

- [Current implementation inventory](../api/implemented.md)
- [Current advanced-media contract](../development/advanced-media.md)
- [Toolchain and hardware verification policy](../development/toolchain.md)
- [Existing user-management routes](../../internal/server/user_management.go)
- [FFmpeg VAAPI encoders](https://ffmpeg.org/ffmpeg-codecs.html#VAAPI-encoders)
- [FFmpeg libplacebo filter](https://ffmpeg.org/ffmpeg-filters.html#libplacebo)

The FFmpeg pages were retrieved during planning. They describe available
interfaces; they do not establish what the installed remote build or AMD
device can execute.
