# M3e SpecialFeatures reference capture plan

Status: **planned; not executed**. This document authorizes no HTTP request,
fixture mutation, scan or deployment by itself. The next operator must select
an explicit scope and execute only through `ssh test-env`. Use the reference
API as a black box and official documentation; do not inspect server or client
implementation source. Do not return a permanent empty array merely to pass the
current empty fixture.

## Proven starting point

The [retained movie-extras contracts](m3e-reference-movie-extras-contracts.json)
and [capture receipt](m3e-reference-movie-extras.json) establish the following
for one existing synthetic Movie on the pinned Emby 4.9.5.0 reference:

| Request | Observed result | Boundary |
| --- | --- | --- |
| `GET /emby/Users/{UserId}/Items/{MovieId}/SpecialFeatures` | HTTP200, `application/json; charset=utf-8`, complete two-byte `[]` | No special-feature resource existed |
| Same route with `Fields=Path,MediaSources,MediaStreams,Overview` | Same HTTP200 and empty array | Does not prove nonempty projection behavior |
| `GET /emby/Users/{UserId}/Items/{MovieId}/Intros` | HTTP200, complete `{"Items":[],"TotalRecordCount":0}` | No intro was configured |
| `GET /emby/Users/{UserId}/Items/{ItemId}/LocalTrailers` | No actual case in this capture | The array contract is currently a documentation claim |

That recorder completed 11 business reads and its own logout204/token401
controls, preserving the six original media items and user configuration/policy.
Its fixed reference identity was PID332054/start ticks357218, executable SHA-256
`c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2`.
These historical values are inputs for a fresh identity check, not proof that
the process is still live. The completed capture root must not be rerun.

The original-client input06 diagnosis now identifies each user's failing request
as the owned Movie's SpecialFeatures GET. Each received HTTP404 JSON with 104
bytes and one sanitized `Response` page error. This proves the current missing
client exchange; it does not establish nonempty resource semantics.

## Official documentation and unresolved contracts

[Movie Naming](https://emby.media/support/articles/Movie-Naming.html#movie-extras)
documents video extras in direct child directories of a movie folder. The
supported names are `extras`, `specials`, `shorts`, `scenes`, `featurettes`,
`behind the scenes`, `deleted scenes`, `interviews` and `trailers`. Further
nesting is unsupported. Add the main movie before its extras. The page says
these categories appear together in the Extras row; that does not prove which
API supplies each category.

The [SpecialFeatures API](https://dev.emby.media/reference/RestAPI/UserLibraryService/getUsersByUseridItemsByIdSpecialfeatures.html)
declares user authentication and a bare `BaseItemDto[]`. Its optional parameters
are `Fields`, `EnableImages`, `ImageTypeLimit`, `EnableImageTypes` and
`EnableUserData`; it declares no sorting or pagination parameters. The local
[generated contract](../api/services/UserLibraryService.md#operation-getusersbyuseriditemsbyidspecialfeatures)
retains these declarations. They do not establish default values or failure
behavior on the target server build.

[Item Information](https://dev.emby.media/doc/restapi/Item-Information.html)
associates `SpecialFeatureCount` with Movies and Series. Nonempty Movie results
therefore do not close Series support. The REST DTO exposes `ExtraType` as a
string; the separate [ExtraType enum](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Entities.ExtraType.html)
does not specify directory-to-value mapping.

[LocalTrailers](https://dev.emby.media/reference/RestAPI/UserLibraryService/getUsersByUseridItemsByIdLocaltrailers.html)
also declares `BaseItemDto[]`. [Trailer Naming](https://emby.media/support/articles/Trailers.html)
documents both a direct `trailers` directory and a same-directory
`MovieName (year)-trailer.ext` file. Capture overlap or separation between
SpecialFeatures and LocalTrailers; do not infer it from their shared UI row.

[Intros](https://dev.emby.media/reference/RestAPI/UserLibraryService/getUsersByUseridItemsByIdIntros.html)
uses the different query-result envelope. [Cinema Intros](https://emby.media/support/articles/Cinema-Intros.html)
describes separate playback/configuration behavior. This plan does not enable
Cinema Intros, change global settings, install plugins or request internet
trailers. Positive Intros support remains a separate feature gate.

These public pages are not proof of exact behavior in the pinned 4.9.5.0 build.
The following observations are still missing:

| Area | Required evidence before claiming the corresponding support |
| --- | --- |
| Membership and ownership | Actual acceptance of each documented category; nested/nonvideo exclusion; parent versus child IDs; generic catalog visibility; trailer overlap |
| Names and DTO types | Actual `Name`, `SortName`, `Type`, `MediaType`, `ExtraType`, `IsFolder`, `ParentId`, path and field presence/types; filename or metadata intent alone is insufficient |
| Counts and projection | Parent `SpecialFeatureCount`/`LocalTrailerCount`, returned membership, default versus explicit Fields, image switches and user-data omission |
| Ordering | Repeated default order, same-category and cross-category ordering, and stability after an owned rescan; no undocumented SortBy/Limit behavior is presumed |
| Access and errors | Anonymous/revoked token, own versus foreign UserId, library denial, missing ID and existing non-Movie owner; determine how parent scope affects child access. Preventing an authorization bypass is a Goby requirement, not an already captured reference result |
| Media capability | Actual indexed source/stream DTO and authorized byte delivery for returned resources; listing is not decoding, seeking or real-client playback acceptance |
| Broader owner types | Empty Series/Episode behavior now; positive Series behavior before claiming complete Series support |

## New fixture, separate from all retained roots

Proposed media root: `/opt/goby-fixtures/client-special-features-m3e-v1`.
Proposed private evidence root:
`/opt/goby-test/exec-work-m3e/reference-special-features-v1`.
Both names are proposals, not created resources. Require absence before setup;
never adopt or overwrite an existing directory. Record an exclusive marker,
directory identities, file hashes and a receipt binding the new library ID.

Create one new owned Movies library. Preserve the current six reference
libraries, all five existing users' configuration/policy and relevant UserData,
all original media bytes and all historical capture trees. Place no file inside
`client-m3e` or the existing auxiliary fixture. Refresh only the new library;
do not rerun the old three-library or six-library fixture operators.

Prepare the main films first, scan the new library and confirm their actual
Movie IDs/types/paths through the API. Only then add extras and refresh that
same new library. Generate small, owned synthetic clips with recorded probe
facts; use no embedded title tags initially. A filename or sidecar is not an
accepted server field until the API reports it.

The smallest useful first scope distinguishes featurettes, deleted scenes and
local trailers while providing two same-category ordering witnesses:

```text
Movies/
  M3e Special Features Positive (2026)/
    M3e Special Features Positive (2026).mp4
    featurettes/
      Zeta Bonus.mp4
      Alpha Bonus.mp4
      notes.txt
      nested/Hidden Nested.mp4
    deleted scenes/Middle Deleted Scene.mp4
    trailers/Delta Local Trailer.mp4
  M3e Special Features Empty (2026)/
    M3e Special Features Empty (2026).mp4
```

Create `Zeta` before `Alpha` and record file times; retain array order as observed.
This distinguishes some name/order hypotheses, not a unique private algorithm.
Do not substitute arbitrary requested `SortBy` values for an unrecorded default.
The nested video and text file are explicit exclusion controls. Record whether
they become ordinary items, extras or neither; do not silently omit contrary
results from the catalog observation.

The full documented Movie-layout scope adds one direct video to each remaining
directory: `extras`, `specials`, `shorts`, `scenes`, `behind the scenes` and
`interviews`. Also compare the separately documented flat `-trailer` form in a
distinct Movie directory. Each scope must be selected before mutation. A first
three-category pass must remain partial until these other layouts are captured.

## Bounded capture sequence

1. Recheck the official package/executable, PID/start ticks, private namespace,
   server identity and existing six-library/five-user baseline. Use fresh,
   independently identified recorder sessions; never borrow a browser token.
   Save the authentication acknowledgment privately and prove server/user/device
   ownership before business requests or logout. An unproved token is quarantined.
2. Perform receipted setup in its own stage. Capture the exact new paths and
   accepted catalog; stop if the main movies are misidentified or fixture
   ownership cannot be proved. Preserve missing extras and differing endpoint
   membership as observations; not every documented directory is presumed to
   appear in SpecialFeatures. Do not compensate by editing responses or metadata guesses.
3. Read the positive parent's detail/count fields and SpecialFeatures default
   result. Repeat the unchanged default request three times, then request the
   bounded detailed field set. Include `Path`, `ParentId`, `SortName`,
   `MediaStreams` and `Overview`; sample the previously accepted `MediaSources`
   selector separately rather than assuming it populates a nonempty result.
4. Capture `EnableUserData=false` and `EnableImages=false`. ImageTypeLimit and
   EnableImageTypes need an actual image-positive resource before their behavior
   can be claimed. Preserve exact null/omission/empty-array distinctions.
5. Read LocalTrailers for the same parent. Then read
   `/emby/Users/{UserId}/Items/{ChildId}` for one representative child from each
   returned collection. Compare IDs and membership with SpecialFeatures, parent
   counts and one ordinary recursive catalog read. Do not merge the two arrays.
6. Read SpecialFeatures for the new empty Movie and for the existing owned
   empty Series/Episode. Resolve the latter from the current receipt and confirm
   their actual type/path; do not treat historical IDs as current authority.
7. Record anonymous, revoked-token, foreign-UserId and missing-ID cases, including
   exact status and bounded error shape. Use an existing ordinary account's
   current restricted policy for library-denial cases only if that profile is
   actually available. Do not change any of the five old policies. Otherwise
   mark that case blocked pending separately approved owned-account setup.
8. Re-read parent/result identities and all protected old business state, then
   close only this recorder's proven sessions and confirm exact-token rejection.
   Preserve failure evidence and the new owned fixture; no automatic global
   cleanup, password reset, library deletion or retry is implied.

The initial business-read selection should remain about 20-24 explicit cases.
The executable operator must count setup, identity checks, preservation reads
and authentication separately, set a total bound and reserve cleanup capacity.
Do not reuse an old 12/20-request recorder budget or existing evidence root.
Adding a category, user-policy mutation, rescan, image fixture or media request
requires an explicit selected case, not an unbounded discovery loop.

Capture bounded complete response bodies privately, with methods, parameters,
status, MIME and original array order. Publish only controlled projections,
resource mappings and hashes; never tokens, passwords or query-bearing media
URLs. New recorder session/device/audit history is recorded separately from
preserved old configuration, policy, UserData and media.

After positive HTTP contracts are understood, a separate actual-client run must
open the Movie Extras row and, for any claimed playable profile, select the
real resource through the unmodified UI. Any PlaybackInfo preparation effects
need their own authorized state ledger. This planned API capture alone does
not close the current page-error/client acceptance gate or the entire
SpecialFeatures, LocalTrailers or Intros feature family.
