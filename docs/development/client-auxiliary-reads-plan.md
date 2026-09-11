# Post-source18 auxiliary client reads

Status: the initial capture, five positive comparison captures, and metadata
combination v2/v3 captures are complete. Metadata v1 remains a preserved failed
attempt. These are reference protocol observations, not original-client
acceptance or a recovered private scoring specification.

The deployed product remains source18. Source19 music tests passed, but its
Similar test run retained a complete-catalog timeout. Source20 contains a frozen
scoring/SQL revision with 43 targeted and 1,763 complete-source remote race tests
passed. It is installed only on the isolated candidate. The original client
completed its Similar response, but ThemeMedia and the complete auxiliary
navigation remain open. The evidence and remaining gates are recorded below.

## Observed routes

The original client requests `GET /emby/Items/{Id}/Similar` and
`GET /emby/Items/{Id}/ThemeMedia`. Neither route is registered in deployed source18.
Source18 audio retains their HTTP 404 JSON responses and co-occurring page errors.
User-path and Albums/Artists/Movies/Shows aliases have not been accepted.

The existing reference run
`reference-av-source12-subtitle-diagnostic-01/observation.json` records Similar
HTTP 200 at request indices 255 and 325, and ThemeMedia HTTP 200 at indices 256,
298 and 326. Its Similar keys include Limit, UserId, ImageTypeLimit, Fields,
EnableTotalRecordCount and, at the album stage, ExcludeArtistIds. Values and
bodies were not retained. An earlier masked `/Items/{value}/{value}` request
must not be treated as proof that Similar traffic was absent.

## Similar evidence boundary

The completed captures cover owned empty/nonempty results, self exclusion,
movie/audio/album examples, observed ordering, pagination, count behavior and
role-sensitive ExcludeArtistIds. They do not establish every item type, field
projection, malformed-input case or access/error boundary. The pinned
QueryResult_BaseItemDto model defines a shape, not a ranking rule.

Source18 item queries provide authorized counting, pagination and DTO assembly,
but no Similar scorer or ExcludeArtistIds filter. A generic item listing or a
permanent empty response would not establish the nonempty observed contract.

## ThemeMedia evidence boundary

The [existing movie extras capture](m3e-reference-movie-extras-contracts.json)
observed three result objects: ThemeSongsResult, ThemeVideosResult and
SoundtrackSongsResult. For its single theme-free movie, each had empty Items
and TotalRecordCount zero. Direct enabled reads reported numeric owners 9/9/0;
inherited reads reported 1/1/0; disabled reads reported 0/0/0.

The later positive capture adds actual theme resources, enable flags and
series/season ancestor selection. Numeric OwnerId values below are observations
of those actual owners, not constants to copy into Goby. A hexadecimal Goby item
ID or a synthetic root string does not by itself satisfy that wire contract.
Soundtrack matching and broad access/error behavior remain unproved.

## Implementation boundary

Authorize the current requested item through the existing Emby subject and
GetItemFor path before returning even an empty result. Apply candidate access
checks before Similar ranking, counting and pagination. Reuse item, image and
user-data projections, and guarded OpenMediaFor delivery for actual themes.
Use the captured theme ownership and attachment relationships when designing
their model. Preserve all original media and existing reference state.

The next implementation gate is ThemeMedia. The frozen source20 Similar
revision passed complete verification and scoped original-client reads. Theme
implementation and a complete real-client follow-up remain
separate. This document update only reads retained evidence; it performs no
HTTP, login, probe, scan or database mutation and reads no reference or client
implementation source.

## Source18 error timing audit

In both source18 audio observations, zero-based requests 251 and 252 are the
Similar and ThemeMedia HTTP 404 responses. The two page errors contain only
`Error: Response`, with no route, request ID or action phase. Their timestamps
fall between `audio-home-before` and `audio-album-tracks`, before the first
media request. This establishes co-occurrence during album navigation; it does
not prove which response caused each error. The later MP3 Home locator failure
is a separate event. Request `elapsed_ms` records dispatch, not response arrival.

The next browser observation should record action, request, response and error
times on one clock, with the current action phase. A bounded album-navigation
flow can exercise these reads without another playback or history reset.

## Positive fixture design

Before generation, the existing reference media manifest was rechecked without
HTTP, probing or mutation: all 14 recorded members, including the marker,
retained their exact bytes and file identities. It contains six playable items
and no theme assets or controlled genre/studio/person comparisons. The new
sibling fixture supplies those comparisons without changing or hard-linking
old files.

The [official theme naming guide](https://emby.media/support/articles/Theme-Songs-Videos.html),
read on 2026-09-11, documents `theme.<audio-extension>`, a `theme-music`
subdirectory, and a `backdrops` subdirectory for theme videos. The guide describes
folder attachment; API defaults, numeric OwnerId and inheritance were therefore
observed independently. Access behavior and soundtrack matching remain bounded
by the cases actually captured, not by this naming guide.

The completed fixture covers movie metadata with independent shared-feature
variations, albums with same/different/mixed artists, and series/season theme
inheritance. Media generation, reference library attachment and protocol reads
have separate receipts. Only the newly created library IDs may be refreshed;
the existing three libraries and user playback state must be retained.

[Generation passed](m3e-auxiliary-media.json) on the remote host: 43 files,
25 media profiles and 19 primary media items (12 movies, two episodes and five
tracks in four albums), totaling 16,603,617 bytes. The manifest SHA-256 is
`dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7`.
The old media bytes, inode identities and hard-link counts were preserved.
The mixed album intentionally has track artists A/B and a common album artist A;
this distinguishes role-dependent exclusion instead of assuming those roles
are equivalent. No reference library was created by the generation step.

[Reference attachment](m3e-reference-auxiliary-libraries.json) then completed
three separately receipted libraries with 55 HTTP attempts. It retained the
original three libraries, all five current account configurations/policies,
their visible original-item metadata/UserData projections, and all old/new
media. Its recorder token was revoked. The accepted index report SHA-256 is
`457a4d88f86617c448b980f31b7dd5b7d8bbeb31e3cfe807103371859f2d7998`.

## Initial reference capture

[The 20-request capture](m3e-reference-auxiliary-reads-v1.json) completed with
17 business reads and an independently owned login, logout 204 and exact-token
401. Catalog metadata, UserData, viewer Configuration/Policy and original media
were unchanged. All responses were complete; no browser session was used.

| Case | Observed result |
| --- | --- |
| Similar on movie 9 or album 13 | HTTP 200, empty Items and TotalRecordCount 0 |
| Similar on album 13, excluding its actual artist 12 | HTTP 200, empty result; this does not prove exclusion behavior on nonempty candidates |
| Similar on MP3 11 | HTTP 200 with FLAC 10, actual artist/album relationships, UserData and TotalRecordCount 1 despite EnableTotalRecordCount=false |
| ThemeMedia defaults on movie/album/MP3 | HTTP 200, three empty result objects; song/video OwnerId equals numeric seed 9/13/11, soundtrack OwnerId 0 |
| Movie EnableThemeSongs=false | Only song OwnerId becomes 0; video OwnerId remains 9 |
| Movie EnableThemeVideos=false | Only video OwnerId becomes 0; song OwnerId remains 9 |
| Movie InheritFromParent=false | Same owners and empty contents as the default request |
| Anonymous Similar and ThemeMedia | HTTP 401 |
| ThemeMedia for decimal 999999999 | HTTP 200, empty song/video results with OwnerId 2; soundtrack OwnerId 0 |

The last ID was absent from the complete owned-media catalog and its observed
artist references. This does not prove global absence or malformed-ID behavior.
Its fallback owner must not be copied as a constant. The nonempty MP3 result
establishes a real peer candidate, but does not yet establish general scoring,
ties, pagination or total-count suppression semantics.

## Completed positive captures

Each row below includes its own fresh recorder login, logout 204 and exact-token
401. Request totals include authentication and four preservation reads; they
are not counts of distinct business cases. All five runs report unchanged
catalog metadata, UserData and IDs, unchanged recorded Configuration/Policy,
and unchanged old/new media bytes, identities and manifests. They performed
zero business writes and used no browser session.

| Capture | HTTP attempts | Distinct cases | Main evidence |
| --- | ---: | ---: | --- |
| [Movies](m3e-reference-auxiliary-positive-movies.json) | 17 | 10 | Nonempty/empty results, exclusion, order, counts, pagination and explicit sort |
| [Music](m3e-reference-auxiliary-positive-music.json) | 19 | 12 | Directed album/audio candidates and nonempty artist exclusions |
| [Music roles](m3e-reference-auxiliary-positive-music-roles.json) | 20 | 13 | Mixed artists, ArtistType variants, music Limit=0 |
| [Artist exclusion](m3e-reference-auxiliary-positive-artist-exclusion.json) | 11 | 4 | Candidate role union and absence of parent-album artist leakage |
| [Themes](m3e-reference-auxiliary-positive-themes.json) | 20 | 13 | Actual songs/video, flags and ancestor selection |

The JSON bundles retain each case's request/response and source-file hashes.
Their `report.approvedFixtureCatalog` is the accepted-feature witness: source
NFO content or a descriptive fixture name alone is not proof that a feature
was indexed. Movie Seed 35 actually has genres Drama 53 and Adventure 54,
TagItems SharedOne 55 and SharedTwo 56, studio SharedStudio 50, actor
CommonActor 51 and director CommonDirector 52. Tags are established through
the returned TagItems even when a separate Tags property is absent.

| Role | Actual identities |
| --- | --- |
| Movie controls | Seed 35; AllMatches 44; GenreBoth 37; GenreOne 40; TagBoth 41; Actor 49; Director 47; Studio 34; NoShared 38 |
| Music artists | ArtistA 77; ArtistB 79 |
| Albums | SeedAlbum 80; SameArtistAlbum 82; MixedAlbum 78; DifferentArtistAlbum 81 |
| Tracks | Seed A 74; Same A 76; Mixed A 73; Mixed B 72; Different B 75 |
| TV/theme owners | Series 59; Season01 60; Season02 61; episodes 63/64 |

Movie Seed returned `[44,37,41]` and `[44,41,37]` on repeated baseline reads.
The one-genre, one-tag, actor, director, studio and year-only controls did not
qualify in that baseline. Excluding 44 left 37/41. `Limit=0` returned only 44;
`Limit=3,StartIndex=3` returned an empty result with count 0. The observed
TotalRecordCount followed the returned page, including when
EnableTotalRecordCount=false. SortName descending returned `[41,37,44]`,
overriding the normal score order. These observations do not establish a stable
tie order. The proposed ParentId case was explicitly omitted because the
capture did not prove its requested library-parent identity.

Music results were directed: Seed A 74 returned Same A 76 and Mixed A 73;
Different B 75 returned none. Mixed A 73 returned 76/74 and its same-album peer
72, while Mixed B 72 returned all four other tracks. SeedAlbum 80 returned
82/78; DifferentArtistAlbum 81 returned none; MixedAlbum 78 returned the other
three albums. ArtistType=Artist and ArtistType=AlbumArtist produced the same
memberships in the tested cases, with varying order. Music `Limit=0` returned
an empty result. Artist entity 77 as the seed also returned an empty result;
this one example does not establish all entity-seed behavior.

Excluding ArtistB removed MixedAlbum 78 from SeedAlbum results but did not remove
Mixed A track 73 from Seed A results: the track does not inherit the other
track's ArtistB credit for exclusion. The additional exclusion capture shows
that album-artist roles also matter. Mixed A 73 excluding ArtistA became empty,
including removal of its ArtistB/AlbumArtistA peer 72. Mixed B 72 excluding
ArtistA retained only Different B 75; excluding ArtistB retained 73/76/74.
MixedAlbum 78 excluding ArtistA retained only album 81.

The theme capture returned song 36 for Seed 35, songs 46/45 for AllMatches 44,
and video 39 for NoShared 38. Direct enabled song/video groups used the requested
item's numeric owner even if a group was empty. Disabled groups used owner 0.
Inherited Season01/episode63 songs came from series 59 with song 62; Season02
and episode64 used season 61 with song 65. The corresponding inherited empty
video groups reached owner 1. With inheritance disabled, Season01 retained
empty groups owned by 60, and Season02 retained its own song owned by 61.
SoundtrackSongsResult stayed empty with owner 0 in every captured case. These
are attachment/DTO observations; they do not prove theme playback delivery or
justify a fixed root-owner constant for another catalog.

## Metadata combination evidence

Only the new auxiliary GenreOne 40 and Actor 49 were eligible for temporary
metadata writes; Seed 35 was read-only. The operators cloned all original
EDIT_FIELDS, durably recorded each intent, verified accepted details, observed
Similar, and restored original business metadata once. GenreItems were checked
against the seed's actual 53/54 references, not ignored as arbitrary derived
data. Authentication cleanup required proof of the exact new administrator and
device before logout 204 and exact-token 401.

| Attempt | HTTP attempts | Actual result |
| --- | ---: | --- |
| [v1 failed](m3e-reference-similarity-combinations-v1-failed.json) | 27 | The Tags write retained an empty TagItems body; POST 204 was followed by unchanged business metadata. No combination formed, no Similar case completed, and 49 was not written. Recovery observed the original state and sent no restore. |
| [v2](m3e-reference-similarity-combinations-v2.json) | 37 | 40 with one genre plus actual TagItem 55 entered the result, count 4. 49 with the actual actor plus director remained excluded, count 3. Both were restored. |
| [v3 eligibility](m3e-reference-similarity-combinations-v3-eligibility.json) | 37 | 40 with one genre plus studio 50 entered, count 4. 49 with one genre plus its actor remained excluded, count 3. Both were restored. |
| [v3 ranking](m3e-reference-similarity-combinations-v3-ranking.json) | 43 | Two genres plus studio on 40 ranked immediately after 44 in all four reads. Two genres plus the original actor on 49 interleaved with 37/41 across four reads. All returned counts were 4; both were restored. |

The v1 no-op is a failed request-shape experiment, not a similarity-model
counterexample. Its report retains `retained_for_review` and only an Etag
change. Every attempt records preserved old/new media, the read-only seed's
full detail, and all five users' visible original-six/target-two item
projections. Successful v2/v3 runs restored both full business metadata and
EDIT_FIELDS. Automatic timestamp/Etag differences are separately allowed and
recorded; raw-detail equality for modified items is not claimed. UserData
preservation means exact equality of every returned field in the bounded Items
projection, not a claim about fields the API did not return.

## Working model and its limits

A normalized working model fitting these samples gives one point per distinct
shared genre, tag and studio, gives Person zero points, and requires a score of
at least two. The one-genre/one-tag and one-genre/one-studio results support this
model. The actual two-person and genre/person exclusions contradict the earlier
equal-unit Person hypothesis. The ranking observations are consistent with
studio contributing beyond eligibility and with no distinguishable person
contribution at this sample's resolution.

For music, a fitting directed model takes the union of the seed's actual
Artist and AlbumArtist identities, counts distinct shared identities separately
in each candidate role, and adds one for the same non-null physical album.
The eligibility threshold is again two. Missing album identities must not
match each other. Candidate exclusion checks its effective Artist/AlbumArtist
roles rather than importing unrelated credits from a parent album. This
explains Mixed B's wider reach and Mixed A's same-album peer while preserving
the observed exclusions.

These unit contributions and threshold are an implementation working model,
not uniquely identified private weights or a claim to the reference's internal
algorithm. Equivalent rescalings, hidden tie behavior and other unobserved
features remain possible. Year-only nonmatches do not establish a universal
zero year weight. Do not score unaccepted NFO fields, invent absent artists,
equate missing albums, or turn this finite fixture into a complete compatibility
claim. Regression tests should protect the observed memberships, exclusions,
page counts and rank constraints without requiring one accidental tie order.

## Frozen attempts and remaining verification

All existing evidence roots under `/opt/goby-test/exec-work-m3e` are immutable
attempts. Do not rerun or overwrite `reference-auxiliary-reads-v1`,
`reference-auxiliary-positive-v1-{movies,music,music-roles,artist-exclusion,themes}`,
`reference-similarity-combinations-v1`, `reference-similarity-combinations-v2`,
or `reference-similarity-combinations-v3-{eligibility,ranking}`. New authorized
work requires its own reviewed inputs, evidence root and independently owned
session. No old session, control directory or failed mutation is adopted.

[Source19](m3e-source19-snapshot.json) is uninstalled. Its separate
[music run](m3e-source19-music-targeted.json) passed 30 top-level tests, but the
[Similar run](m3e-source19-similar-targeted-failed.json) passed 11 and failed one:
the complete-catalog test exceeded its existing 90-second fixture deadline with
1,050 low-score candidates and a later high-score candidate. The failed
evidence remains; shortening the candidate population or extending the timeout
would not resolve the recorded problem. The earlier equal-unit Person model
was also contradicted by the independent reference combinations.

[Source20](m3e-source20-snapshot.json) freezes the reference-constrained scoring
and complete-catalog SQL optimization, while retaining the source19 music
metadata revision. [Targeted verification](m3e-source20-targeted.json) passed
43 tests with no failures/skips and complete disposable-pair cleanup. The
unchanged 1,050-candidate fixture took 377.86 ms to prepare and 85.96 ms to query.
[Complete verification](m3e-source20-full.json) passed 1,763 race tests across
24 packages with no failures/skips. The built executable passed a separate
[schema25 candidate replacement](m3e-source20-candidate-upgrade.json), preserving
all prior 30-table data and sequences. The main deployment remains source18.

The [original-client album observation](m3e-source20-auxiliary-album.json) verifies
the exact album URL, two track rows and completed Similar 200 transfer, with
unchanged four-item UserData and preferences, no playback and exact-token logout
proof. Its original workflow remains failed: ThemeMedia returned 404 without an
observed transfer completion, one page error remained, and Home was not reached.
The [preceding harness failures](m3e-source20-auxiliary-album-failed-01-03.json)
remain distinct records. The observed card omits data-id/type; absent attributes
remain unknown until the detail and request identities establish the owner.
The remaining page error followed ThemeMedia response headers and preceded
Similar response headers on one clock. This is timing evidence, not an explicit
error-to-request correlation or complete ThemeMedia acceptance.
