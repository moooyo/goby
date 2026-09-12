# Global NextUp preparation and cleanup contract design

Status: **the corrected producer passed 140 remote guards; preparation02 stopped
and was independently sealed after known cleanup. A later complete baseline is accepted.**
Its [implementation contract](nextup-global-preparation-implementation.md) and
[current verification](nextup-global-preparation-verification-03.json) retain
the separate code and evidence boundary.
The owned source-media prerequisite was subsequently checked remotely. Written on
2026-09-13 from owned scripts and retained public-response receipts, this is the
preparation producer design for the [acceptance plan](nextup-global-acceptance-plan.md),
[matrix](../../scripts/test-env/nextup-global-matrix.py), verified transport and
reviewed outer operator. The [accepted observer02 baseline](nextup-global-baseline-observer-02.md)
supports a new preparation scope; no successful matrix fixture has been released.
This design is not a live authorization receipt. No ID, process identity,
source digest, or historical state below may be copied into a new live manifest
without the corresponding fresh observation.

## Scope and authority

Prepare two new TV libraries LA/LB, one distinct series A/B in each, S01E01,
S01E02, and S02E01 in each series, and two newly owned ordinary actors P/Q.
Each episode is an independent copy of approved 600-second, 30 fps synthetic
bytes. Account credentials, device IDs, and recorder sessions are independent.
The completed exposed playback state is zero for all twelve actor/episode
pairs. No matrix request or browser activity runs during preparation.

Use a new exclusive preparation directory and new media roots outside every
sealed LibraryChanged v4 and earlier evidence/media root. Reject a new root
that equals, contains, or is contained by a sealed root; obtain that inventory
from the retained ownership receipts. Sharing the existing protected parent
directory does not authorize modifying its other children. Do not replay v4,
replace its lock, update its receipts, restart its services, or rescan its
libraries. Retain the completed v4 terminal and preservation digests as release
inputs, then recheck the existing coordination lock and current identities.

The reference transport endpoint is the existing owned host proxy at
`http://127.0.0.1:18197`; the original server remains in its separate network
namespace. Bind the reference process with metadata only, and separately bind
the owned proxy process, exact listener socket, host namespace, forwarding
configuration, and upstream reference namespace/endpoint. The transport owner
is finalizing the exact process schema. Do not serialize the old assumption
that the original process owns the host listener or that the worker shares the
original namespace. Do not open or hash the original executable. In particular,
do not call `prepare-client-reference.py`'s `preconditions()` or import/run an
old recorder's main path: its legacy preconditions hash that executable.

Only the existing owned administrator credential may administer preparation.
Create one new preparation session for it and one for each new actor; revoke
all three exact tokens before publishing success. Matrix logins are new later
sessions. Existing accounts and their tokens are not cleanup targets.

## Reusable synthetic media evidence

The owned [media receipt](m3e-client-media.json) declares H.264/AAC, 600 seconds,
and 30 fps. The local receipt file read for this design has SHA-256
`d071081ea17decbc07d3191ddddd4ab5415717ac907f7e86c5c8c302878bc064`.
The subsequent [remote source observation](nextup-global-source-media-observation.json),
SHA-256 `be5fd7ebfd81bfec75bfc107fa106e2cfc51b5e6eebb9093d8dedc2e8a3bc630`,
confirmed that the current remote manifest has the same digest. It verified the
Movie source's 48,786,888 bytes and media digest while holding the existing
reference lock, without modifying the source or creating new media. Its four
existing hard links remain intact. Admission and copying must recheck these
identities; the observation is not a fixture preparation success receipt.
The following historical paths all have recorded media
SHA-256 `7265bc56bd7f495bcbd5224adcf6df94478a99d1994ba713274a194f8f9db088`:

| Owned source path | Evidence |
| --- | --- |
| `/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4` | `m3e-client-media.json`, and `SOURCE_MOVIE`/`SOURCE_MOVIE_SHA` in `capture-client-library-changed-reference.py`. |
| `/opt/goby-fixtures/client-m3e/TV/M3e Client Series/Season 01/M3e Client Series S01E01.mp4` | Media receipt, and episode construction in `prepare-client-media.py`. |
| `/opt/goby-fixtures/client-m3e/TV/M3e Client Series/Season 01/M3e Client Series S01E02.mp4` | Same receipt and generator. |
| `/opt/goby-fixtures/client-m3e/TV/M3e Client Series/Season 02/M3e Client Series S02E01.mp4` | Same receipt and generator. |

The historical generation manifest is
`/opt/goby-fixtures/client-m3e/manifest.json`. The remote observation above
established its current digest and the Movie source's owner/file identity.
Before copying, the producer must recheck only this owned synthetic
source/manifest, read its bounded bytes, and match those recorded facts.
Do not infer current bytes from a historical preservation boolean. The source
episodes were hard links inside the old media population; do not alter their
links or reuse hard links in the new population. Make six new independent
files, hash each destination, and retain source/destination identities and
sizes. Check the source again after copying.

Use the existing owned naming/NFO pattern from `prepare-client-media.py`:
`<series>/Season 01/<series> S01E01.mp4`, S01E02, and Season 02/S02E01.
Write two `tvshow.nfo` files and six matching `episodedetails` files containing
only the new stable titles and season/episode numbers. Copy no subtitles,
artwork, music, trailers, or alternate versions. Freeze and hash the NFOs and
ownership markers in a separate tree receipt; the transport media receipts
currently describe only the three episode files per root. Enforce at most
128 MiB per episode, six media files total, and a separately bounded metadata
population. No media generator or original bundled executable is needed.

## Public requests with owned precedent

All mutations are one-shot, journaled before dispatch, and bound to acknowledged
owned IDs. Dynamic route parameters may only substitute IDs returned by an
earlier allowed response. Freeze the literal query ordering/body encoding in
the preparation plan. The following status expectations come from owned
operators; only retained completed responses establish that a historical branch
actually ran.

| Purpose | Exact request and body | Required evidence |
| --- | --- | --- |
| Public identity | `GET /emby/System/Info/Public`, no body | HTTP 200; current `Id`, `Version`, and expected reference identity. |
| Login | `POST /emby/Users/AuthenticateByName`, UTF-8 form fields `Username` and `Pw`, with `Content-Type: application/x-www-form-urlencoded` | HTTP 200; `ServerId`, `User.Id`, `User.Name`, `User.Policy.IsAdministrator`, `SessionInfo.UserId`, session/device identity, and exact token ownership. Secrets remain private. |
| Account absence | `GET /emby/Users`, no body, administrator token | Complete bounded list; both proposed names absent case-insensitively; freeze existing IDs and existing Configuration/Policy values. |
| New ordinary account | `POST /emby/Users/New`, `{"Name": username}` | HTTP 200; fresh distinct `Id`, exact `Name`, and `Policy.IsAdministrator=false`. Never adopt an account found by name. |
| New password | `POST /emby/Users/{newUserId}/Password`, `{"Id": newUserId, "NewPw": newPassword, "ResetPassword": false}` | HTTP 200 or 204; only the newly acknowledged account; independent persisted private secrets. |
| Policy | `POST /emby/Users/{newUserId}/Policy`, complete observed policy with the exact changes below | HTTP 200 or 204, then full profile read and own-token visibility proof. |
| Profile | `GET /emby/Users/{actorId}`, no body | HTTP 200; complete Configuration/Policy, exact account, disabled/admin false, and two precise folder grants. |
| Preferences | `GET /emby/usersettings/{actorId}`, no body | HTTP 200; preserve complete value and field presence; this matches the matrix's lowercase route. |
| Library inventory | `GET /emby/Library/VirtualFolders/Query`, no body, administrator token | HTTP 200; complete `Items`/count; distinct new `ItemId`, exact `Locations`, `Name`, `CollectionType`, and `LibraryOptions`. |
| Create library | `POST /emby/Library/VirtualFolders`, the exact body below | HTTP 200 or 204 with absent/empty body; one new acknowledged library per unique root/name. Follow with inventory; do not expect the create response to supply its ID. |
| Scoped scan | `POST /emby/Items/{newLibraryItemId}/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=ValidationOnly`, `{}` | HTTP 200 or 204, absent/empty body; one dispatch per new library. Never call `/emby/Library/Refresh`. |
| Views | `GET /emby/Users/{actorId}/Views`, no body | HTTP 200; map exact view IDs and ownership, with only LA/LB visible to each ordinary actor. |
| Scoped catalog | `GET /emby/Users/{actorId}/Items?Recursive=true&Fields=Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear&EnableUserData=true&EnableTotalRecordCount=true&Limit=256&ParentId={scopeId}`, no body | HTTP 200; complete count, unique IDs, exact owned paths and descendants. No global scan or arbitrary query forwarding. |
| Full detail | `GET /emby/Users/{actorId}/Items/{ownedItemId}`, no body | HTTP 200; full item identity, relations, runtime/streams, and complete UserData. |
| Exact logout | `POST /emby/Sessions/Logout`, no body; then `GET /emby/Sessions` with that same token | Normal success requires 204 then 401. An already-invalid 401 may be a retained reconciliation fact, not a fabricated acknowledged logout. |

The policy body is a copy of the just-observed new account's complete `Policy`,
with only these deliberate settings:

```json
{
  "IsAdministrator": false,
  "IsDisabled": false,
  "EnableAllFolders": false,
  "EnabledFolders": ["{LA.policyFolderId}", "{LB.policyFolderId}"],
  "EnableMediaPlayback": true,
  "EnableAudioPlaybackTranscoding": true,
  "EnableVideoPlaybackTranscoding": true,
  "EnablePlaybackRemuxing": true,
  "EnableContentDeletion": false,
  "EnableContentDownloading": false
}
```

This fragment is not the entire POST body. Preserve the other returned policy
fields and retain the before/after bodies. `prepare-client-av-user.py:256`
uses management `ItemId` values for the two `EnabledFolders` grants and checks
the profile and Movie/Audio details, but those detail requests use its admin
token. The initial `reference-nextup-later-partial.py:276` restricted branch
did not complete the final experiment; its continuation used all-folders
access. These sources do not prove that the candidate policy-folder mapping
permits all TV full details under P/Q's own tokens. An acknowledged policy or
visible view alone is insufficient. Do not fall back to all-folders access.

For each library, adapt the owned `prepare-client-reference.py:281` template
by changing only its name, root paths, and `CollectionType` to `tvshows`:

```json
{
  "Name": "{new stable library name}",
  "CollectionType": "tvshows",
  "RefreshLibrary": false,
  "Paths": ["{new owned media root}"],
  "LibraryOptions": {
    "PathInfos": [{"Path": "{new owned media root}"}],
    "SampleIgnoreSize": 0,
    "EnableRealtimeMonitor": false,
    "EnableChapterImageExtraction": false,
    "ExtractChapterImagesDuringLibraryScan": false,
    "EnableMarkerDetection": false,
    "EnableMarkerDetectionDuringLibraryScan": false,
    "DownloadImagesInAdvance": false,
    "SaveLocalMetadata": false,
    "SaveLocalThumbnailSets": false,
    "SaveSubtitlesWithMedia": false,
    "SaveLyricsWithMedia": false,
    "MetadataSavers": [],
    "SubtitleDownloadLanguages": [],
    "LyricsDownloadLanguages": [],
    "AutomaticRefreshIntervalDays": 0,
    "EnableEmbeddedTitles": true,
    "EnableAutomaticSeriesGrouping": false,
    "TypeOptions": "{exact expansion described below}"
  }
}
```

`TypeOptions` must be an array, not the placeholder string above. Expand it in
the fixed order Movie, Series, Season, Episode, MusicArtist, MusicAlbum, Audio;
each object is `{"Type": type, "MetadataFetchers": [], "MetadataFetcherOrder": [],
"ImageFetchers": [], "ImageFetcherOrder": [], "ImageOptions": []}`. Compare every
returned option with this body, permitting no `NetworkPath` mapping or enabled
provider. This is the observed fixed template; do not add guessed options.
The limited refresh precedent is `capture-client-library-changed-reference.py:487`.
It requires two equal, complete catalog observations with both refresh-state
fields absent, while the other library definitions remain unchanged.

## Catalog identity and the zero baseline

Keep `libraryId`, `viewId`, `sourceRootId`, `policyFolderId`, and `scopeItemId`
as distinct fields even if a particular target returns equal raw values.
`libraryId` comes from virtual-folder `ItemId`; `viewId` from actual Views;
`sourceRootId` from the series' observed parent, followed by its own full
detail and exact native-root path proof. The matrix requires
`scopeItemId == viewId` and A/B's parent IDs equal their source-root IDs. A name
match, numeric ID pattern, or a library management response is not that proof.
An absent view/root relation is a missing public fact, not an invitation to
inspect the original database or implementation.

Freeze exactly two Series, four Seasons, and six Episodes. Verify Type, Id,
ParentId, SeriesId, IndexNumber, ParentIndexNumber, library affiliation,
`RunTimeTicks=6000000000`, and observed video AverageFrameRate/RealFrameRate of
30. Require one study series per library and no extra playable media or
alternate source. Retain complete original DTOs and their response receipts.
Both ordinary actors must independently read both catalogs and every full
detail. Do not use the administrator token for their visibility proof.

Before calibration, read all six episode details and all six series/season
details for both P and Q. Every episode needs boolean Played=false, integer
PlayCount=0, integer PlaybackPositionTicks=0, and absent or null LastPlayedDate.
Freeze the complete UserData object, including field presence/types; absence
and explicit null are distinct when comparing the final result. Preserve
profiles and preferences. A projected list cannot supply this baseline.

## Target-bound cleanup calibration

The retained [fresh safety receipt](m3e-reference-nextup-safety.json) establishes
historical full-detail restoration of viewer2 episodes 17 and 18 by individual
DELETE PlayedItems requests. The implementation is the narrow cleanup branch
at `reference-nextup-fresh.py:565`, with before/after full details at lines
594-605. `LastPlayedDate:null` in UserData POST did not clear the retained date.
The historical receipt therefore supports the method below, but not a current
`verifiedForBoundTarget=true` attestation. The public repository summary does
not embed the original raw DELETE responses; preserve the original private
receipt references when citing the historical execution.

The later-partial receipt is not a zero-state cleanup proof. Its new account
retained episode 17 at 120 seconds; its cleanup closed owned sessions/tokens.
It references the earlier viewer2 cleanup rather than repeating DELETE.

Calibrate two newly owned pairs, P/A1 and Q/B1, each using two independently
journaled public lifecycles: first a 120-second partial, restore it, then a
completion at the authoritative 600-second duration, and restore it. All eight
requests in a lifecycle use that actor's own preparation token. This covers
both actors' actual DELETE permissions and both library scopes, plus nonzero
position and played/count/date history. Do not substitute admin-authenticated
details or borrow P's cleanup result for Q. Do not increase the scope if a
result is inconclusive. For each pair X/E, each lifecycle has these requests:

1. Full X/E detail equal to the frozen zero baseline.
2. `POST /emby/Items/{E}/PlaybackInfo` with `{"UserId": X, "IsPlayback": true}`;
   require HTTP 200, one owned source with 6000000000 runtime ticks, and the
   actual nonempty PlaySessionId/MediaSourceId.
3. `POST /emby/Sessions/Playing` with the acknowledged ItemId, MediaSourceId,
   PlaySessionId, and X's actual SessionId, plus RunTimeTicks=6000000000,
   PositionTicks=0, CanSeek=true, IsPaused=false, IsMuted=false,
   PlayMethod="DirectStream", PlaybackRate=1; require HTTP 204.
4. `POST /emby/Sessions/Playing/Progress` with that same body, requested
   PositionTicks of 1200000000 or 6000000000, and EventName="TimeUpdate";
   require HTTP 204.
5. `POST /emby/Sessions/Playing/Stopped` with the same acknowledged identity
   tuple, the requested position, Failed=false, IsAutomated=false; require 204.
6. Full X/E detail. Establish the actual expected changed playback fields,
   reject unrelated UserData drift, and preserve the full before-DELETE DTO.
7. `DELETE /emby/Users/{X}/PlayedItems/{E}`, **no body or query**; require 200.
8. Full X/E detail. Require complete UserData equal to the initial baseline.

After each actor's first reset compare that actor's six series/season summaries.
After all four resets compare all twelve episode details, all twelve series/season
details, profiles, and preferences. Unknown session ownership, a lost mutation
response, unexpected effects, or a failed DELETE stops normal work. Reconcile
only explicitly owned acknowledged responsibilities; do not retry the mutation,
broaden to series resets, or post guessed UserData fields.

These calibrations leave legitimate authentication/playback/audit history even
after exposed UserData returns to zero. The preparation receipt must disclose
that fact. It may say the accounts were created with no history and the final
exposed state is zero; it must not say that no playback ever occurred or that
the database equals its pre-creation state. The current transport proof requires
the calibrated actor/item to be P/Q and one of the six episodes. If the planned
"no playback history" requirement means untouched audit history at matrix
entry, that requirement conflicts with this consumer: change the reviewed
contract before execution rather than silently treating zero UserData as
untouched history.

## Minimum phases and independent request budget

The producer must be a new script with a pure plan/freeze mode and an explicit
remote execution entry. Old operators have fixed historical roots, IDs,
population assumptions, and broader routes; do not parameterize and rerun
their main functions. Journal intent, reserved ordinal, actual private
response, sanitized export, and state transition separately. Use exclusive
files and fsync, no generic retry, no automatic resume, and no overwritten
evidence directory. Unused request capacity authorizes no new operation.

Proposed preparation maximum: **320 HTTP requests**, **240 normal**, and **80
reserved for cleanup**, separately from the matrix's 300-request budget and
the browser allowance. Allow at most 15 seconds per request, 1,200 seconds of
normal work, and 600 seconds of cleanup. Bound each response to 1 MiB, request
body to 32 KiB, and aggregate charged response bytes to 384 MiB, with at least
80 MiB plus 80 sentinel bytes reserved for cleanup. Freeze the exact encoding
and enumerate every observation slot before dispatch.

| Phase | Maximum normal requests | Contents |
| --- | ---: | --- |
| Administrator login and full before snapshot | 32 | One login and the 31-request preserved public snapshot described below; reuse its public identity, Users, and virtual-library responses. |
| Two new libraries and scans | 42 | Each create POST is followed by its own unique library lookup and durable identity registration before the next library is created. Two scoped refresh POSTs follow; at most twelve rounds of one inventory plus two scoped catalogs require two identical complete ready rounds. Stop observation at 100 seconds. |
| Explicit root/view mapping | 5 | Administrator Views; two full view details and two full source-root details. |
| Two new ordinary accounts | 8 | Each create, password, exact policy, and profile acknowledgement. |
| Own-token baseline | 36 | For each actor: login, profile, preferences, Views, two scoped catalogs, six episode details, six summary details. |
| Four cleanup calibrations | 44 | P/A1 partial and complete, Q/B1 partial and complete: four eight-request lifecycles including DELETE/proof, plus six summary proofs after each actor's partial reset. |
| Final own-token zero baseline | 28 | Twelve episode details, twelve summaries, and four profile/preferences reads. |
| Full after snapshot | 37 | The expanded eight-user/ten-library public snapshot; reuse its catalog/roster definitions for the final preservation comparison. |
| Total normal maximum | **232** | Eight unused normal slots authorize no additional route or retry. |

Success then spends six cleanup requests to log out the three exact owned
tokens and prove each rejected. The complete success path is at most **238**
requests. The maximum enumerated failure cleanup set is **77**: two known
pending stop slots, two reconciliation details and two conditional DELETE
slots for P/A1 and Q/B1, twelve episode details, twelve summary details, four
profile/preferences reads, the 37-request full after snapshot, and six
logout/rejection requests. The two stop slots cover the two actors; sequential
lifecycles never leave two known unstopped sessions for one actor. A new
lifecycle cannot start until that actor's previous stop/reset is verified.
Select only applicable routes with acknowledged ownership and measured
baseline. If a response is lost, do not pretend that this route set solves
unknown token or mutation ownership; retain a recovery-required result.

| Cleanup phase | Maximum cleanup requests | Scope |
| --- | ---: | --- |
| Stop known playback | 2 | One most recent acknowledged pending context per actor. |
| Reconcile and conditionally reset | 4 | Own-token detail and conditional DELETE for P/A1 and Q/B1. |
| Prove zero state | 24 | Twelve own-token full episode details and twelve summaries. |
| Prove account state | 4 | P/Q profiles and preferences. |
| Prove preserved public scope | 37 | The complete bounded after snapshot. |
| Retire exact tokens | 6 | Three logout requests and three same-token rejection requests. |
| Maximum cleanup set | **77** | Eighty reserved slots; no unenumerated retry uses the remaining three. |

The retained v4 snapshot implementation at
`observe-client-library-changed-reference-ui.py:804` costs exactly
`11 + libraryCount + 2 * userCount` GETs: public identity, Users, virtual
libraries, one bounded catalog per library, one item-ID projection and one
preferences read per user, the six already bound old library/root/target/anchor
details, Devices, and System/Configuration. The current retained population is
six users/eight libraries, giving 31 before; eight users/ten libraries gives
37 after. Freeze the actual current old IDs and those six detail routes from
the release evidence. If that roster differs, stop before mutation and refreeze
the route count; do not let the planner discover more requests during dispatch.

The snapshot reads use the same fixed catalog fields above. User item reads
replace ParentId with `Ids={sorted frozen catalog IDs}`; preferences use the
historically retained `/emby/UserSettings/{userId}` route. Devices and server
configuration use `GET /emby/Devices` and `GET /emby/System/Configuration`, no
body. These are preserved existing public-state reads, not changes or scan
requests. The after snapshot adds new users/libraries for completeness, but
compare old catalog membership/DTOs and old per-user item projections only
against the frozen old item set. New owned library visibility may appear for
existing all-folders users; classify that exact addition instead of treating
the newly created catalog as unexplained old-state drift. Preserve existing
policy/configuration/preferences; permit only observed preparation-owned
authentication timestamps and exact new device/session records. The new actors'
administrator-token snapshot projections supplement, and never replace, the
separately budgeted own-token full-detail baselines.

Before HTTP, remotely stage the source-bound owned media and fresh private
directories, prove no sealed-root overlap, and acquire the existing fixture
lock. These filesystem operations have their own exact file/byte/time budget
and cannot create service units, modify old roots, or run generators. At the
end, retain accounts, catalog, media, device/login records, and protocol audit
history. Delete none automatically. Compare only the captured old public
scope and owned file identities; do not claim whole-database preservation.

## Receipts consumed by the matrix and transport

The transport currently requires exactly eight private receipt descriptors:
`preparation`, `coordination`, `catalog`, `cleanup`, `policy-P`, `policy-Q`,
`media-LA`, and `media-LB`. Every receipt uses schemaVersion=1,
`kind="nextup-global-" + receiptName`, runId, target, and facts; descriptors
carry an exact absolute path and SHA-256. The final metadata/proxy process
schema must be taken from the frozen transport source, not this working design.

| Receipt | Consumer facts |
| --- | --- |
| preparation | Current serverId, process and endpoint bindings, version, fresh matrix evidenceRoot, credentialStoreSha256, catalogReceiptSha256, actorIds, and retainedArtifacts exactly `["accounts", "catalog", "media", "protocol-audit"]`. |
| coordination | Existing lock path/device/inode, fixtureRoot, releasedAt, and the current process binding; accompany it with preserved v4 terminal/release references. |
| catalog | Exactly `libraries` and `items` matching the matrix map. Library rows include mediaRootReceiptSha256; item rows include mediaSha256, the observed numbers/runtime/frame rate, and semantic relations. |
| policy-P/Q | `previousUserIds`, `createdUser` containing Id/Name, and the full `observedProfile`; preserve actual creation/password/policy/login/visibility responses in supporting evidence. |
| media-LA/LB | `sourceRootId`, `rootPath`, and `files` keyed by the three episode symbols, each with `path`, `sha256`, and `sizeBytes`; retain NFO/tree/source-copy proofs separately. |
| cleanup | Exact `contractVersion: 2`, current process/serverId, both acknowledged preparation actors/logins, and four calibrations ordered P/A1/partial, P/A1/complete, Q/B1/partial, Q/B1/complete. Each calibration retains beforeZero, beforeDelete, delete, and afterDelete response envelopes. |

The [transport's four-calibration contract](nextup-global-transport-implementation.md#four-calibration-cleanup-receipt)
now enforces both actors and both playback modes. Its latest
[91-guard remote result](nextup-global-transport-tool02-verification-01.json)
binds transport SHA-256
`d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1`.
The two logins and sixteen observations must reference eighteen distinct actual
private response receipts. Envelopes retain increasing ordinals, actual
completion timestamps, ordered request headers, exact routes, and original
response values. Actual token and device metadata bind every GET and DELETE
to its acknowledged preparation actor; the old single-proof shape is rejected.

Keep complete private wire records, full baseline comparisons, stage ordering,
and all three terminal logout proofs as supporting producer evidence. The
DELETE response body remains exactly the observed value. A hand-written boolean,
copied historical response, or successful fake guard result is not a live
preparation receipt.

The separate private credential store contains schemaVersion, runId, and
exactly P/Q actors with credentialRef/userId/username/password. Passwords must
be distinct and at least 32 characters; no token goes into the matrix manifest.
Hash the final credential store before the preparation receipt. Publish media,
policy, cleanup, coordination, and catalog receipts first; then preparation;
then the matrix and execution manifests. This avoids receipt-digest cycles.
The future matrix evidenceRoot must still be unused when transport begins;
preparation journals live in a different root.

## Remaining gates

The producer implementation and remote fake guards are complete; no verified
new live fixture is established by this file. The
[authority observation](nextup-global-authority-observation.json) records the
current application/proxy metadata, retained baseline, old controller logout,
and stopped v4 units. The [Goby observation](nextup-global-goby-preservation.json)
confirms its full sealed v7 state, except capture time, remains unchanged.
Before dispatch, the root task must assemble and recheck the actual release,
credentials, media approval, source-bound plan, and outer execution/preservation
inputs. Actual destination visibility and restricted account access still need
their public responses. All verification runs through
`ssh test-env`; no local fallback is permitted.

Unknown public facts remain explicit gates: P/Q's actual restricted TV access,
view/source-root/policy-folder identity relations, successful indexing under
the unchanged sandbox, and the current target's complete zero-state DELETE
restoration. Failure of any gate must preserve the new scope and return its
specific missing evidence. Neither vendor implementation access nor guessed
public behavior is required or permitted to fill those gaps.

The transport read during this review accepts only the reference target;
Goby execution needs its own source/executable/schema/current-fixture contract.
This reference preparation design cannot be copied to Goby as an execution
authorization. No global NextUp selection, ordering, count, pagination, flag,
or actual-client behavior is established by preparation or calibration.
