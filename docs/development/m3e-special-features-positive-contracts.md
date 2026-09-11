# M3e positive SpecialFeatures contracts

The first positive reference capture completed through `ssh test-env`. The
[checkpoint](m3e-special-features-positive-checkpoint.json) binds the executed
operator, two media stages and both actual reference reports. The exact safe
exports are [mains](m3e-reference-special-features-mains.json) and
[extras](m3e-reference-special-features-positive.json).

One new Movies library, ID `83`, was added to the pinned reference. Main Movies
`87` and `88` were indexed and proved empty before extras generation. The
generator then added five synthetic H264/AAC videos, including a nested
exclusion control, and one text control. The final media root contains 13
files. Only the new library was refreshed. The old six libraries, five users'
protected API projections/configuration/policy and all protected media were
preserved. Both phases used separate admin and ordinary-viewer sessions; all
four new tokens have owned-identity proof, logout204 and exact-token401.

## Actual ordinary-viewer results

All 24 selected business responses completed with HTTP200 and JSON MIME. They
cover two Movies, an existing Series and an existing Episode, each through
SpecialFeatures, LocalTrailers and Intros with default and explicit Fields.

| Input path relative to the positive Movie directory | Endpoint | Id | Name | Type | ExtraType |
| --- | --- | --- | --- | --- | --- |
| `featurettes/Alpha Bonus.mp4` | SpecialFeatures | 91 | Alpha Bonus | Video | Clip |
| `deleted scenes/Middle Deleted Scene.mp4` | SpecialFeatures | 90 | Middle Deleted Scene | Video | DeletedScene |
| `featurettes/Zeta Bonus.mp4` | SpecialFeatures | 92 | Zeta Bonus | Video | Clip |
| `trailers/Delta Local Trailer.mp4` | LocalTrailers | 89 | M3e Special Features Positive - Trailer | Trailer | Trailer |

SpecialFeatures is a bare array in the displayed Alpha/Middle/Zeta order.
LocalTrailers is a separate one-element array; its resource does not appear in
SpecialFeatures. Both defaults include `Id`, `Name`, `Type`, `MediaType`,
`ExtraType`, `IsFolder`, `RunTimeTicks`, `ServerId`, `UserData`, `ImageTags` and
`BackdropImageTags`. `MediaType` is `Video`, `IsFolder` is false and duration is
120000000 ticks. No default `ParentId`, `Path`, `SortName`, `MediaSources` or
`MediaStreams` is present.

The explicit Fields request includes those five selectors plus Overview,
Genres, Tags, People, Studios, ProviderIds, DateCreated and ProductionYear.
It supplies `ParentId=87`, actual paths, names in `SortName`, a real source
`mediasource_{Id}` and H264/AAC stream facts. The trailer's media source name
remains `Delta Local Trailer`, distinct from the item name. The requested
Overview and ProductionYear are absent for these untagged extras. Requested
People is `[]` for the three SpecialFeatures but absent for the Trailer.
The full exports preserve all other field distinctions.

The empty Movie `88`, Series `14` and Episode `18` return `[]` from both array
endpoints. All four owners return `{"Items":[],"TotalRecordCount":0}` from
Intros. These are empty existing-owner observations, not positive Series or
Cinema Intros support.

The new-library member request (`ParentId=83`, `Recursive=true`) has no item
type filter and returns three Folders and the two main Movies. None of the
four extras, nested video or text control appears in that scoped query, and
neither endpoint returns the nested or text control. The separate old-state
preservation catalog explicitly excludes `Trailer` in IncludeItemTypes and
cannot independently establish trailer visibility. These observations do not
prove global unindexability of nested files or all possible query behavior.

## Additional projection and access observations

The separate [v2 projection capture](m3e-reference-special-features-projections-v2.json)
completed 20 fixed business GETs and 32 total requests. Its original safe export
SHA-256 is `5a1a9e35047776a5cf35f79d0e896631c8f122f283a47f949b490f9779d9df7d`.
The ordinary recorder's explicit 64-item baseline, four direct child projections
and all media were preserved; the new token completed logout204/exact-token401.
Seven-library/five-user ownership is bound to the earlier completed receipts,
not a newly performed administrator-wide state audit.

The [v1 attempt](m3e-reference-special-features-projections-failed-01.json)
stopped before all 20 business cases. Its baseline requested the 64 explicit
IDs without IncludeItemTypes and received a complete 200 response containing
59 items, including the four extras but omitting five old MusicAlbums. The
baseline completeness assertion therefore rejected the attempt. Its reported
empty accepted-before IDs do not mean the response was empty. V2 restored the
type selector, including Trailer, and actually received all 64 IDs; it did not
remove the five albums from the expected set. Both attempts revoked their new
tokens, and the v1 evidence remains unchanged.

Explicit `Ids` queries can return these extras, unlike the earlier unfiltered
ParentId=83 recursive enumeration. This evidence supports a distinct known-ID
retrieval path and must not be generalized into ordinary attachment browsing.
The observed request had Recursive=true and no ParentId. Combined parent/ID
filters still require their own scope and authorization checks.

The positive Movie's default direct detail and selected ItemCounts Fields
response both contain `LocalTrailerCount: 1` and omit `SpecialFeatureCount`.
All four child details preserve their actual Movie parent and omit VideoType;
the three SpecialFeatures include PartCount while the Trailer does not.
EnableUserData=false removes UserData. EnableImages=false removes ImageTags
and BackdropImageTags. Fields=MediaSources alone adds MediaSources, Container,
Bitrate and Size without adding top-level MediaStreams. Two repeated default
SpecialFeatures reads preserve Alpha/Middle/Zeta order.

Anonymous SpecialFeatures and LocalTrailers return 401. Direct detail for the
fixed absent-ID candidate returns 404, whereas its SpecialFeatures returns
200 with an empty array. LocalTrailers for an absent ID was not selected.
The reference also returns 200 when this ordinary token names another existing
user in both endpoint paths. Goby's existing explicit cross-user isolation
requirement remains 403; this is a documented policy difference, not matching
reference-error evidence or permission to relax that requirement.

## Remaining boundaries

Zeta was generated before Alpha, whereas both observed SpecialFeatures arrays
are Alpha/Middle/Zeta. This is consistent with name ordering, but two requests
with different field selections do not prove a stable sorting algorithm or
rescan behavior. The capture does not exercise sorting or pagination controls.

The selected catalog projection does not contain either count; the subsequent
direct-detail observations above establish only its sampled positive values
and omissions. Denied-library cases and ordering across rescans remain open.
No byte-delivery or original-client playback claim follows from source
capability booleans. Other documented directory
categories, the flat trailer suffix, image-positive resources and positive
Series extras remain outside this fixture scope.

Product work can now use observed Clip, DeletedScene and Trailer membership,
names and projections. Preserve the migration and authorization invariants in
the [implementation plan](special-features-implementation-plan.md); the
source28 Movie page-error gate remains open until the implementation and fresh
client verification pass. Do not rerun either completed reference phase or
either completed media phase.
