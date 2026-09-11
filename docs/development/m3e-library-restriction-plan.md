# M3e temporary Movies restriction and restoration

Status: **planned; not executed**. This is a static implementation plan, not
acceptance evidence. The source32/schema27 primary deployment, candidate
positive Extras UI and its scoped comparison have passed; their completed
operations must not be replayed. Execute this new gate only through
`ssh test-env`. It temporarily removes access to the **original Movies library**
from ordinary viewer B, proves that the same token observes the change, and
restores B's complete supported account and policy values. The separate positive
Extras Movies library remains accessible to B throughout. This does not
establish full M3 compatibility or whole-database equality.

## Candidate and scope

Use the isolated candidate, not the primary service. Bind a fresh run to the
[accepted source32 upgrade](m3e-source32-candidate-upgrade.json), its completed
[root extension](m3e-source32-extra-root-extension.json), the nonempty profile
receipts below and Music lineage. Record the actual process identity.
Stop on a binding mismatch; an old PID alone is not current identity evidence.
The primary also runs source32/schema27, but its service and database are outside
this gate. The [current handoff](handoff.md) records that separation.

| Field | Expected value |
| --- | --- |
| API / original client | `http://127.0.0.1:18198` / `http://127.0.0.1:18196` |
| Schema / source | `27` / `/opt/goby-test/exec-work-m3e/source-attempt-32` |
| Source manifest SHA-256 | `a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65` |
| Candidate executable SHA-256 | `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620` |
| Candidate service / process | `goby-client-m3e.service`; PID `748513`, start ticks `6996875`, boot `6bdfc486-7bc8-412f-82b5-70095a09dde7` |
| Fixture state SHA-256 | `5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1` |
| Runtime SHA-256 | `d8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df` |
| Full source32 verification SHA-256 | `408c49ff73e66c505865494e8e2e843954fd682a4b09fb5287835c027718ee78` |
| Music chain | `/opt/goby-test/exec-work-m3e/client-music-upgrade-chain-source32.json` |
| Music chain SHA-256 | `c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a` |
| Viewer A | `34b4c24f6568659af7ce17938fae7f81` |
| Viewer B | `ecbbe4cb82403879bc4b4f78894c5738`, `m3e-client-viewer` |
| Original Movies: restrict only this library | `a9993591e72f0f2e7babcbf8b9c50790` |
| Music | `6383d20008836e137559698c29b10395` |
| TV | `a34ce665fb75421ef7551570f353d705` |
| Positive Extras Movies: retain access | `57a85c1ca5b6c7ae602c587755250b2f` |
| Positive Extras root | `604d2c0f5c78919a6ee360cda2048066`; `/opt/goby-fixtures/client-special-features-m3e-v1/Movies` |
| Original movie item | `268051d3ca734aefcf94e245fb25ad55` |
| Positive Extras movie / empty control | `1ac8b0f4188531e8b701bfda055d7a34` / `1a4cea4fe0e5212e11291ab69e31341f` |

The four-library, 22-item identity is backed by these immutable receipts;
`W` means `/opt/goby-test/exec-work-m3e` in this table.

| Artifact | Path / SHA-256 |
| --- | --- |
| Schema27 upgrade completed | `W/client-upgrade-aaff5430e8e7fc52b1850fabe2097d89/completed.json`; `66c2b1f13113c5969887e963641e7ba04743f33da609e748a648117bf2edf26b` |
| Root extension completed | `W/client-special-features-root-extension-v1/completed.json`; `9d5404b4a6c1a9c5be404f453ee93cbdfa8e036399624b8897e940b7fccebcaf` |
| Nonempty profile completed, version 3 | `W/client-special-features-protocol-finalization-v1/completed.json`; `b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36` |
| Protocol/media finalization report | `W/client-special-features-protocol-finalization-v1/report.json`; `929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d` |
| Independent profile inspection | `W/client-special-features-finalization-inspection-v1/report.json`; `fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e` |

The [positive UI report](m3e-positive-original-client.json), SHA-256
`b061a2ae791b77c5db30959ec70d7bff710f13d92fe1bddf4eba901b3ebbcca4`,
and [passed scoped comparison](m3e-positive-client-comparison.json), SHA-256
`b2143ce110482191b6ee75342166d39e8af6e4cf14f881c1a91440cbbbe46531`,
are later than the setup-point inspection. The accepted A/B scope now records
26 play rows, 57 authentication rows, seven UserData rows and zero references or
encoding rows. These are scoped counts, not global authentication totals.
The accepted private after-observation is
`/opt/goby-test/exec-work-m3e/positive-prepare-observation-finalized-01/after.json`,
with SHA-256
`74640ff8ce353ad61b41364fbeedaff4fa2c14e55075ce55869e8be85963a10b`.
Require its explicitly supplied path and digest when connecting a new baseline;
do not search for or adopt another observation. Take a fresh read-only snapshot
for this gate. State-file equality alone does not prove current database
equality, and the setup-point inspector must not be rerun against legitimate
post-UI history as though that history were absent.

Keep credentials, cookies and tokens private. B's credentials are the `viewer`
entry in `/opt/goby-test/exec-work-m3e/browser.json`; use the established
administrator credentials for native writes. Neither A nor B authorizes those
writes. Preserve all frozen inputs, prior output directories and fixture files.
Use a new evidence directory and coordinate mutations with the existing
`/opt/goby-test/exec-work-m3e/client-fixture.lock`; do not create or replace that
lock. No migration, restart, scan, playback, password, metadata, preference or
UserData mutation belongs to this gate.

## Native request contract

1. `POST /admin/v1/session` with `{"Name":"...","Password":"..."}` returns
   the administrator `goby_session` cookie and `CSRFToken`.
2. `GET /admin/v1/users/{B}` returns `{"User":{...}}`; also read A and
   `GET /admin/v1/libraries` for the baseline.
3. `PUT /admin/v1/users/{B}` requires the administrator cookie,
   `Content-Type: application/json`, and `X-CSRF-Token`. Use the configured
   `Origin: http://127.0.0.1:18196` when sending an Origin header.

The PUT body is a complete replacement of supported fields, not a patch:

| Required field | Type / source |
| --- | --- |
| `Revision` | Current positive canonical decimal **string**, from a fresh GET |
| `Name` | Original B name, string |
| `IsAdministrator` | Original B value, boolean; must be `false` |
| `IsDisabled` | Original B value, boolean; must be `false` |
| `Policy.EnableAllFolders` | Boolean |
| `Policy.EnabledFolders` | Non-null array of existing library ID strings |
| `Policy.EnableMediaPlayback` | Original boolean |
| `Policy.EnablePlaybackRemuxing` | Original boolean |
| `Policy.EnableAudioPlaybackTranscoding` | Original boolean |
| `Policy.EnableVideoPlaybackTranscoding` | Original boolean |

All five top-level fields and all six Policy fields are required exactly once
with exact casing. Unknown, missing, duplicate or null fields are rejected.
Do not submit GET-only `Id`, `HasPassword` or `CreatedAt`. Do not convert revision
to a JSON number or increment it to construct the next request. Folder IDs are
validated, sorted and deduplicated; the maximum input length is 256.

## Execution and recovery

1. Establish new, independently owned A/B sessions and an administrator session.
   Fresh-read B and require the bound ID, name, enabled state and ordinary role.
   Save its complete native GET response privately and construct the original
   restore body before any policy write. Preserve the current complete UserData
   rows, preferences, Configuration, catalog and media through a fresh read-only
   database/file baseline. Include the positive movie and its four resources
   alongside the original movie, MP3, FLAC and album; do not reuse the old
   five-UserData baseline. Record the baseline audit boundary and distinguish
   this gate's new authentication history from the completed UI run.
2. Confirm the current four library IDs and the receipted positive Extras
   root/item mapping. Discover `{movie}` with B's token at
   `/emby/Users/{B}/Items?ParentId={Movies}&Recursive=true&IncludeItemTypes=Movie&Fields=Path&Limit=64`.
   Require the unique `M3e Client Movie`, the pinned item ID above and
   `/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4`. Do not substitute
   the Movies library ID for the movie ID. Require B can access both original
   Movies and positive Extras before mutation. Run the baseline matrix below.
3. Fresh GET B immediately before restriction; require its supported account
   and Policy values still equal the saved baseline. Submit that fresh revision
   and change only the folder controls. If original `EnableAllFolders=true`,
   set it to `false` and allow every current library except Movies: for this
   confirmed four-library fixture, use the canonical sorted list
   `["57a85c1ca5b6c7ae602c587755250b2f","6383d20008836e137559698c29b10395","a34ce665fb75421ef7551570f353d705"]`.
   If original `EnableAllFolders=false`, remove Movies only from the original
   list without granting any other library. Preserve all four playback flags
   and the original name/role/disabled values. Stop if the baseline does not
   already grant the intended original Movies and positive Extras access;
   this gate must not grant access to make its control pass.
4. Require PUT 200, `CurrentSessionRevoked=false`, and revision `r+1`, where `r`
   is the accepted pre-write revision. Fresh GET must match the intended
   restricted state. Reuse the same B token for the restricted matrix; A must
   retain its own movie access. Record explicit HTTP checks as API evidence,
   not as original-client UI evidence. Any UI observation must bind an actual
   completed client request and the corresponding visible state.
5. Restore in the cleanup path even if a restricted check fails. Fresh GET B,
   require the state and revision match this run's known restriction, and PUT
   the saved original name, role, disabled state and **all six original Policy
   fields**, with this fresh revision. Require 200,
   `CurrentSessionRevoked=false`, and normally `r+2`. Fresh GET must equal the
   original supported values apart from revision. Run the restored matrix and
   repeat the fresh A/B UserData, preference and Configuration comparisons.
6. Close only this run's A/B credentials with `POST /emby/Sessions/Logout`,
   require 204, then use each exact same token for `GET /emby/System/Info` and
   require 401. These are API cleanup results; an actual client UI logout must
   be recorded separately if a UI phase is later added. End the
   administrator session with `DELETE /admin/v1/session` plus cookie and CSRF;
   expect 204 and rejection of that exact cookie afterward. Retain success or
   failure evidence and explicitly report whether restoration was confirmed.

Do not blindly retry a PUT after a lost response. Reconcile with fresh GET:
unchanged original state at `r` means restriction did not commit; this run's
restricted state at `r+1` permits restoration. An unexpected revision or account
change must not be overwritten with the complete baseline body. Preserve the
evidence and report recovery required if restoration cannot be established.
Never issue a restore PUT when no restriction committed, or an extra no-op PUT
merely to confirm restoration.
Reserve each restriction/restore intent durably before dispatch. A 409
`revision_conflict` is not permission to overwrite a newer account state.
After a lost restore response, a fresh GET showing the original supported
values at the expected new revision can confirm restoration without another
PUT; otherwise retain a recovery-required result. Do not reset credentials or
revoke all sessions to manufacture cleanup. Journal failure must still allow
bounded cleanup of proven owned credentials while their process/state pins
remain valid, and must leave the overall result failed if evidence is incomplete.

## HTTP acceptance matrix

`{A}`, `{B}` and `{Movies}` refer to the bound IDs above; `{movie}` is confirmed
from the baseline. `{positive}` is the positive Extras movie ID above. Requests
use the named actor's same valid token in all three phases. These are expected
source32 contracts to test, not recorded results of this unexecuted matrix.

| Actor and GET route | Baseline | B restricted | Restored |
| --- | --- | --- | --- |
| B: `/emby/Users/{B}/Items/{movie}` | 200, bound movie | **404 `not_found`** | 200, same movie ID |
| B: `/emby/Users/{B}/Items?ParentId={Movies}&Recursive=true&IncludeItemTypes=Movie` | 200, bound movie present | **404 `not_found`** | 200, bound movie present |
| B: `/emby/Users/{B}/Views` | 200, four baseline views | 200, original Movies absent; Music/TV/positive Extras retained | 200, all original view IDs |
| B: `/emby/Users/{B}/Items?Ids={movie}` | 200, movie present | 200, empty Items/count 0 | 200, movie present |
| A: `/emby/Users/{A}/Items/{movie}` | 200 | 200 | 200 |
| A: `/emby/Users/{B}/Items/{movie}`; B: `/emby/Users/{A}/Items/{movie}` | **403 `access_denied`** | **403 `access_denied`** | **403 `access_denied`** |
| B: `/emby/Items/{movie}/Similar?UserId={B}` | 200 | 404 | 200; empty results allowed |
| B: `/emby/Items/{movie}/ThemeMedia?UserId={B}` | 200 | 404 | 200; empty groups allowed |
| B: `/emby/Users/{B}/Items/{movie}/SpecialFeatures` and `/LocalTrailers` | 200, empty arrays | 404 for this existing hidden movie | 200, empty arrays |
| B: `/emby/Users/{B}/Items/{positive}` | 200, bound positive movie | 200, same movie | 200, same movie |
| B: `/emby/Users/{B}/Items/{positive}/SpecialFeatures` | 200, three bound resources | 200, same three resources/order | 200, same three resources/order |
| B: `/emby/Users/{B}/Items/{positive}/LocalTrailers` | 200, one bound trailer | 200, same trailer | 200, same trailer |

Same-user hidden resources return 404 because library visibility filters their
lookup. Cross-user targeting returns 403. A Views 200 alone proves neither
denial nor restoration: compare its IDs and the movie-detail response as well.
Retaining `EnableAllFolders=true` would defeat restriction. Do not use invalid
credentials, account disabling or malformed Policy to manufacture a denial.
Use the profile's exact Alpha/Middle/Zeta/trailer IDs rather than discovering
an arbitrary attachment. Source32 returns an empty SpecialFeatures array for a
truly missing owner, but 404 for an existing inaccessible owner; the known
original movie is essential to that distinction. Do not demand a fabricated
`SpecialFeatureCount`: the accepted parent contract omits it and reports
`LocalTrailerCount: 1` on the positive movie.

## Reusable implementation and the next bounded increment

No dedicated restriction operator currently implements this plan. The next
increment is one new candidate-only API operator with a new evidence root,
pure guards and a read-only preflight before execution within the existing
authorized candidate-validation scope.
Use one administrator login and one fresh login per ordinary actor; permit
only the listed GETs, one restriction PUT, one restoration PUT and exact owned
logout/401 cleanup. Keep restoration and cleanup request/time budgets reserved.
No browser navigation is needed for this first API matrix: the existing Movie
UI drivers intentionally issue PlaybackInfo and therefore cannot serve as a
read-only policy checker without a separate preparation scope.

| Reusable component | Applicable contract and limit |
| --- | --- |
| [Native user routes](../../internal/server/admin_users.go), `managedUser`, `updateManagedUser`, `managedUserRevision` | Fresh GET, exact complete PUT and canonical revision string; 409 conflict handling. |
| [Managed user store](../../internal/identity/managed_users.go), `UpdateManagedUser`, `validateManagedLibraries` | Transactional current actor/revision checks; only folder policy changes; selected libraries are locked against deletion. |
| [Authentication resolution](../../internal/identity/store.go), `Resolve` | Every valid token resolves current user Policy. Folder-only edits do not take the disable/demotion revocation path. |
| [Positive fixture loader](../../scripts/test-env/client-browser-special-features-fixture.mjs), `loadSpecialFeaturesFixture`, `validatePositiveBundle` | Read/hash the actual version-3 profile, both failed histories, source/runtime/process and four-library identity. This is an immutable identity contract, not permission to rerun completed UI. |
| [Positive ledger observer](../../scripts/test-env/inspect-client-positive-prepare-scope.py), `read_record`, `projection`, `statement` | Reuse protected artifact reading and complete read-only observations. Its current authority/comparator is tied to the completed positive-PI scope; a new restriction comparator must explicitly allow B revision/Policy/audit changes and use the latest after-observation. |
| [Finalization operator](../../scripts/test-env/finalize-client-special-features-fixture.py), `make_finalizer`, and [original transport](../../scripts/test-env/prepare-client-special-features-fixture.py), `Actor`, `Setup.request` | Reuse reviewed identity, bounded transport and owned cleanup ideas only. Their fixed workflow/route/row-count gates do not authorize native PUT or this new matrix; do not invoke their completed workflows or weaken old validators. |
| [Browser session proof](../../scripts/test-env/client-browser-session-proof.mjs), `createBrowserSessionProof` | If a later separately scoped UI run is required, bind the exact token from actual UI logout and verify 401 in a fresh guarded context. It does not turn API cleanup into UI evidence. |

The new pure guards must cover a same-count wrong library set, accidental
removal of the Extras library, stale-revision 409, a lost write acknowledgement,
foreign drift before restoration, reuse of the same B token, and bounded
owned-session cleanup after evidence I/O failure. Freeze the new input closure
and pass remote guards/preflight before any policy write. This document update
does not implement or execute that operator.

## Preservation and permitted differences

- Each successful native PUT advances B's `management_revision` by one,
  updates `updated_at`, and records one transactional `user.updated` audit fact,
  including on a no-op. The intended two writes therefore leave revision `r+2`
  and two B update facts with `Source=native`, the administrator actor, B as
  resource, revisions `r+1`/`r+2`, and Count 1. ChangedFields should contain only
  the folder fields actually changed, normally `EnableAllFolders` and
  `EnabledFolders`, or only `EnabledFolders` for an already restricted baseline.
- Audit inspection may use
  `/admin/v1/activity?Action=user.updated&ActorId={adminId}&MinDate={timestamp}&Limit=200`,
  then filter the returned resource ID for B. `ResourceId` is not a supported
  query parameter. Session login/logout adds separate audit facts; total audit
  growth need not be exactly two.
- Restoring six projected fields does not guarantee raw Policy JSON equality.
  Native updates materialize supported defaults and account role flags, while
  preserving unknown keys in an existing Policy object. Login, heartbeat,
  capability reporting and logout can also change sessions, devices, activity
  rows and sequences. Preserve those legitimate facts.
- Claim only the observed scope: B's supported account/Policy values restored,
  A's supported values unchanged, the specified access matrix, and the compared
  UserData/preferences/Configuration unchanged. Catalog, media/private files,
  all database rows, sequences, and raw Policy equality require their own
  explicit before/after evidence. This plan makes none of those broader claims.

The contracts above are derived from
[native user handling](../../internal/server/admin_users.go),
[managed user mutation](../../internal/identity/managed_users.go),
[catalog access](../../internal/library/query.go), and the existing
[Similar](../../internal/server/similar_integration_test.go) and
[ThemeMedia](../../internal/server/themes_integration_test.go) HTTP tests.
Reading those tests is not execution of this acceptance plan.
