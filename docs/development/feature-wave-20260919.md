# Media, collections, and management implementation wave

Historical scope correction: the original P3 completion label incorrectly
included the user-configuration write API. This wave implemented account,
password, policy and deletion compatibility, but
`POST /emby/Users/{Id}/Configuration` was absent at its closeout. P3a and P3b
remain separate below so that the omission stays traceable. The later
[AMD media phase 3](amd-media-phase3-20260919.md) implemented and separately
verified P3b; its product commit `80198b6aa8a163b696ceaff64da831847d82e496` was
merged into `main` and pushed on September 20, 2026. This later result does not
rewrite the original wave's verification or imply production deployment.

## Historical user decision and execution order

For this earlier wave, the user resumed development on September 19, 2026 with
the following order. The new three-phase plan instead closes implementation,
remote verification and documentation within each phase before the next starts.

1. Complete the code for subtitles/media processing, playlists/collections,
   and permissions/administration.
2. Integrate all three areas before executing consolidated functional acceptance.
3. Repair defects and verify the final integration source.
4. Merge the accepted changes into `main` and push to `origin/main`.

OCI playback, upgrades, recovery, and deployment work are deferred. Existing
accepted evidence is retained; old consumed execution scopes are not resumed.
Live TV channels, tuners, EPG, DVR, DLNA, offline sync, and group playback remain
outside this wave. Generic dynamic media-source conversion is included.

Development for this historical wave used `codex/media-library-management`, starting from `a8e525b` with
the existing uncommitted work preserved. Existing changes were copied to a local
Git-private baseline before edits. Related inherited changes must be reviewed
explicitly before integration; unrelated OCI and utility changes remain separate.

## Completion requirements

Every row requires connected implementation. Except for the provider-specific
acceptance explicitly deferred below, meaningful test coverage and acceptance
of the final integration source are required. Source preparation alone does not
establish runtime support. Unfinished requirements remain open.

| ID | Required behavior | Final disposition |
| --- | --- | --- |
| S1 | Extract embedded text subtitles; convert supported text formats and preserve language, forced/default, and stream identities | Implemented and verified |
| S2 | Deliver ASS styles and bounded font attachments; provide bitmap subtitle delivery or burn-in | Implemented and verified |
| S3 | Subtitle selection, off/switch, offsets, source windows, and authorized HLS renditions | Implemented and verified |
| S4 | Real remote subtitle search, download, registration, and playable delivery | Code integrated; provider acceptance deferred by user |
| M1 | Multiple actual aligned HLS renditions and adaptive switching | Implemented and verified |
| M2 | fMP4 initialization/media segments and packed-audio HLS | Implemented and verified |
| M3 | Authenticated dynamic-source open/info/close, lifecycle, conversion, cancellation, and reconnection | Implemented and verified |
| M4 | HDR-to-SDR tone mapping and deinterlacing composed with scaling and subtitles | Implemented and verified |
| M5 | Nonzero video-copy seek with defined keyframe/timebase behavior and audio synchronization | Implemented and verified |
| M6 | Preserve existing direct, remux, progressive, encoded-seek, and MPEG-TS paths | Implemented and verified |
| C1 | Persistent playlists and BoxSet collections, create/edit/delete and stable identities | Implemented and verified |
| C2 | Ordered playlist entries, duplicates, add/remove/move/preview, and pagination | Implemented and verified |
| C3 | Ownership, sharing, locks, and current member authorization across libraries | Implemented and verified |
| C4 | Catalog, details, membership filters, hierarchy, counts, images, user data, and notifications | Implemented and verified |
| C5 | Media/user deletion and access changes preserve consistent collection state | Implemented and verified |
| P1 | Subfolder, parental rating/unrated/tag, and user/device/access policies with actual consumers | Implemented and verified |
| P2 | Shared policy enforcement for queries/counts/details/images/subtitles/playback and mutations | Implemented and verified |
| P3a | User creation/update/password/policy/deletion compatibility with revocation and last-admin protection; implemented portion of original P3 | Implemented and verified |
| P3b | User-configuration write compatibility through `POST /emby/Users/{Id}/Configuration`; missing portion of original P3 | Not implemented in this historical wave; subsequently implemented and separately accepted in [AMD media phase 3](amd-media-phase3-20260919.md) |
| P4 | Playback, remux/transcode, download/delete, remote-control, preference, bitrate, and concurrent-session policy consumers | Implemented and verified |
| A1 | Typed additional configuration, validation/default/reset and explicit reload semantics | Implemented and verified |
| A2 | Real scheduled metadata/subtitle/maintenance executors with durable progress, cancellation and restart handling | Lifecycle and cache execution verified; online execution deferred |
| A3 | Online movie/TV metadata/images and music metadata: search, match, apply, refresh, source provenance and locks | Code integrated; provider acceptance deferred by user |
| A4 | Administrative UI for the new controls and workflows, with errors and authorization changes handled | Implemented and verified |
| D1 | Migrations, published migration manifest, backup schema catalog, and upgrade/restart compatibility | Implemented and verified |
| D2 | Updated API/support documentation and review of all intended commits | Implemented and verified |
| V1 | Consolidated remote build, automated regression, database integration, browser and actual media/client acceptance | Passed within the documented software and browser scope |
| V2 | Final accepted source merged into main and pushed to origin/main | Completed at feature commit `39893aa` |

## Shared contracts and ownership

The integration owner coordinates server lifecycle/route registration, shared
playback and permission consumers, database migration numbering, backup catalogs,
and final source identity. Component owners may edit disjoint implementation and
test files concurrently. They must communicate changes to shared interfaces.

Reserved migration numbers are 0030 for collections, 0031 for online providers,
0032 for management extensions, 0033 for styled subtitles, 0034 for durable
media deletion, and 0035 for independent dynamic playback state. No source change is accepted solely because
its corresponding component has tests in the repository.

Initial real providers are TMDB (movie/TV metadata and images), MusicBrainz
(music metadata), and OpenSubtitles (subtitle search and download). Credentials
are operator configuration, never public DTO or log fields. The user confirmed
that TMDB/OpenSubtitles credentials are not configured and explicitly selected
code integration only for online providers, deferring this area's testing.
Provider-specific acceptance is therefore not a merge blocker for this wave.
It must remain recorded as untested; fake responses cannot establish actual
service compatibility. The integrated source must still build, and the other
features retain their consolidated acceptance requirement.

Media images must obey the requesting subject's current library and parental
policy. Public login-user avatars are a separate bootstrap contract.

## Verification policy

During implementation, author tests and review source without executing builds,
test suites, validators, runtime probes, or browser/media checks. Once all areas
are integrated, run verification only through `ssh test-env`, using isolated
native services and PostgreSQL databases and preserving existing protected
services. If that host or an external dependency is unavailable, record the
specific blocked acceptance; never fall back to local verification.

Consolidated acceptance includes migrations and restart persistence; actual
subtitle/media output and named client workflows; list/share/reorder and policy
changes; management/provider/task UI; authorization and revocation; cancellation,
resource limits, failure recovery, and existing feature regression. Repair
failures before merging. Rechecks must cover the impact of the final changes.

## Current checkpoint

The implemented rows completed their scoped consolidated acceptance. At this
wave's closeout, P3b was unimplemented and was assigned to the later phase 3;
the original P3 completion label did not establish support for that API. The
later phase 3 acceptance is linked above and does not change this checkpoint. Actual
media, core/database, identity, archive/recovery, and browser checks passed;
both native builds completed, the intended source matched the remote copy,
and all owned workers and PostgreSQL closed with protected identities intact.
Online-provider acceptance remains user-deferred. Feature commit `39893aa` was
merged into `main` and pushed to `origin/main`. See the
[verification record](feature-wave-verification-20260919.md) for exact scope,
original failures, passing successors, artifacts and remaining project boundaries.
