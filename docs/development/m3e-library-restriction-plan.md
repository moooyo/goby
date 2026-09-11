# M3e temporary Movies restriction and restoration

Status: **planned; not executed**. This is a static implementation plan, not
acceptance evidence. Execute only through `ssh test-env`, after the separate
dual-user read-only browser run has finished. This gate temporarily removes
Movies access from ordinary viewer B, proves the existing session observes the
change, and restores B's complete supported account and policy values. It does
not establish full M3 compatibility or whole-database equality.

## Candidate and scope

Use the isolated candidate, not the primary service. Bind a fresh run to the
[accepted source28 upgrade](m3e-source28-candidate-upgrade.json), its existing
fixture identity and Music lineage, and record the actual process identity.
Stop on a binding mismatch; an old PID alone is not current identity evidence.

| Field | Expected value |
| --- | --- |
| API / original client | `http://127.0.0.1:18198` / `http://127.0.0.1:18196` |
| Schema / source | `26` / `/opt/goby-test/exec-work-m3e/source-attempt-28` |
| Source manifest SHA-256 | `72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df` |
| Candidate executable SHA-256 | `83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae` |
| Music chain | `/opt/goby-test/exec-work-m3e/client-music-upgrade-chain-source28.json` |
| Music chain SHA-256 | `b8d00674951487aa541dcd8d648d77f91293cc496c4f5002ea288af56660a63a` |
| Viewer A | `34b4c24f6568659af7ce17938fae7f81` |
| Viewer B | `ecbbe4cb82403879bc4b4f78894c5738`, `m3e-client-viewer` |
| Movies | `a9993591e72f0f2e7babcbf8b9c50790` |
| Music | `6383d20008836e137559698c29b10395` |
| TV | `a34ce665fb75421ef7551570f353d705` |

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

1. Establish independently owned A/B sessions and an administrator session.
   Fresh-read B and require the bound ID, name, enabled state and ordinary role.
   Save its complete native GET response privately and construct the original
   restore body before any policy write. Snapshot A/B's four existing media
   UserData projections (movie, MP3, FLAC, album), preferences, Configuration and
   Policy after login. Record the baseline audit boundary.
2. Confirm the current three library IDs. Discover `{movie}` with B's token at
   `/emby/Users/{B}/Items?ParentId={Movies}&Recursive=true&IncludeItemTypes=Movie&Fields=Path&Limit=64`.
   Require the unique `M3e Client Movie`, its actual item ID and
   `/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4`. Do not substitute
   the Movies library ID for the movie ID. Run the baseline matrix below.
3. Fresh GET B immediately before restriction; require its supported account
   and Policy values still equal the saved baseline. Submit that fresh revision
   and change only the folder controls. If original `EnableAllFolders=true`,
   set it to `false` and allow every current library except Movies: for this
   confirmed three-library fixture, use
   `["6383d20008836e137559698c29b10395","a34ce665fb75421ef7551570f353d705"]`.
   If original `EnableAllFolders=false`, remove Movies only from the original
   list without granting any other library. Preserve all four playback flags
   and the original name/role/disabled values.
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
   repeat the original A/B UserData, preference and Configuration comparisons.
6. Complete owned client logout and exact-token rejection checks. End the
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

## HTTP acceptance matrix

`{A}`, `{B}` and `{Movies}` refer to the bound IDs above; `{movie}` is discovered
from the baseline. Requests use the named actor's valid token throughout.

| Actor and GET route | Baseline | B restricted | Restored |
| --- | --- | --- | --- |
| B: `/emby/Users/{B}/Items/{movie}` | 200, bound movie | **404 `not_found`** | 200, same movie ID |
| B: `/emby/Users/{B}/Items?ParentId={Movies}&Recursive=true&IncludeItemTypes=Movie` | 200, bound movie present | **404 `not_found`** | 200, bound movie present |
| B: `/emby/Users/{B}/Views` | 200, all baseline views | 200, Movies absent; Music/TV retained | 200, original view IDs |
| B: `/emby/Users/{B}/Items?Ids={movie}` | 200, movie present | 200, empty Items/count 0 | 200, movie present |
| A: `/emby/Users/{A}/Items/{movie}` | 200 | 200 | 200 |
| A: `/emby/Users/{B}/Items/{movie}`; B: `/emby/Users/{A}/Items/{movie}` | **403 `access_denied`** | **403 `access_denied`** | **403 `access_denied`** |
| B: `/emby/Items/{movie}/Similar?UserId={B}` | 200 | 404 | 200; empty results allowed |
| B: `/emby/Items/{movie}/ThemeMedia?UserId={B}` | 200 | 404 | 200; empty groups allowed |

Same-user hidden resources return 404 because library visibility filters their
lookup. Cross-user targeting returns 403. A Views 200 alone proves neither
denial nor restoration: compare its IDs and the movie-detail response as well.
Retaining `EnableAllFolders=true` would defeat restriction. Do not use invalid
credentials, account disabling or malformed Policy to manufacture a denial.

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
