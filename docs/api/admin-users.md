# Native administrator user management

This is a Goby-owned administrator contract. It does not describe Emby user
mutation routes. The M5a increment has passed its complete Linux and browser
acceptance, recorded in [the verification report](../development/verification-m4e-video-and-users.md).
The complete administrator milestone remains in progress.

All routes require the existing administrator cookie. Writes also require
`X-CSRF-Token`, an accepted same-origin request, and `application/json`. Request
bodies are bounded to 1 MiB. The detail and mutation contracts reject unknown
write fields, duplicate JSON keys, null values, and missing required fields.

## Routes

| Method and route | Request | Successful response |
| --- | --- | --- |
| `GET /admin/v1/users` | None | Existing `{Items: User[], TotalRecordCount}` |
| `POST /admin/v1/users` | Existing `{Name, Password, IsAdministrator}` | Existing `201 {User}` |
| `GET /admin/v1/users/{id}` | Target user ID | `200 {User: ManagedUser}` |
| `PUT /admin/v1/users/{id}` | Complete `ManagedUserUpdate` | `200 {User: ManagedUser, CurrentSessionRevoked: boolean}` |
| `POST /admin/v1/users/{id}/password` | `{Revision: string, Password: string}` | `200 {User: ManagedUser, CurrentSessionRevoked: boolean}` |

The existing `User` shape stays `{Id, Name, IsAdministrator, IsDisabled,
HasPassword, CreatedAt}`. List, creation, bootstrap, and session responses do not
become editable detail snapshots. Fetch the detail endpoint before updating a
user.

## Editable detail and policy

`ManagedUser` contains every existing `User` field plus `Revision` and `Policy`.
`Revision` is a positive, canonical decimal string, such as `"1"`. Treat it as
an opaque concurrency token; the browser must not increment or convert it to a
JavaScript number.

```json
{
  "User": {
    "Id": "example-user-id",
    "Name": "Example member",
    "IsAdministrator": false,
    "IsDisabled": false,
    "HasPassword": true,
    "CreatedAt": "2026-09-09T00:00:00Z",
    "Revision": "1",
    "Policy": {
      "EnableAllFolders": false,
      "EnabledFolders": ["example-library-id"],
      "EnableMediaPlayback": true,
      "EnablePlaybackRemuxing": true,
      "EnableAudioPlaybackTranscoding": true,
      "EnableVideoPlaybackTranscoding": false
    }
  }
}
```

`ManagedUserUpdate` requires `Revision`, `Name`, `IsAdministrator`, `IsDisabled`,
and the entire six-field `Policy` shown above. It must not contain read-only
fields such as `Id`, `CreatedAt`, or `HasPassword`.

| Field | Meaning and limits |
| --- | --- |
| `Name` | Login/display name; trimmed, 1-128 Unicode characters, no controls; the existing Unicode normalization enforces uniqueness. |
| `IsAdministrator` | Administrator capability stored independently from policy JSON. Promotion requires an existing nonempty password. |
| `IsDisabled` | Prevents new login and authorization; disabling revokes all existing authentication sessions. |
| `Policy.EnableAllFolders` | Gives ordinary members access to all configured libraries when true. |
| `Policy.EnabledFolders` | Selected existing library IDs; at most 256 entries, each 1-256 UTF-8 bytes without surrounding whitespace or controls; deduplicated and sorted. Empty with `EnableAllFolders:false` means no library access. |
| `Policy.EnableMediaPlayback` | Master playback permission; applies to administrators as well as members. |
| `Policy.EnablePlaybackRemuxing` | Allows supported remux plans within configured server limits. |
| `Policy.EnableAudioPlaybackTranscoding` | Allows supported audio conversion within configured server limits. |
| `Policy.EnableVideoPlaybackTranscoding` | Allows supported video conversion within configured server limits. |

The detail policy is the **saved configuration**, not a claim that every
permission is currently effective. Administrators always have access to all
libraries. A disabled account has no authenticated access. Playback denial and
configured server limits still apply to administrators; permitting transcoding
does not enable a disabled server engine or create unsupported codec support.
The dashboard retains saved folder selections when administrator access is
enabled so they remain explicit if the account is later changed to a member.

Only the supported fields are exposed. Unknown internal policy keys remain
stored when supported values are updated. `IsAdministrator` and `IsDisabled`
policy mirrors are synchronized with their authoritative account columns.
Malformed stored policy values are projected conservatively; no raw policy or
credential data is returned to the browser.

## Password and session behavior

The password endpoint requires the detail's current `Revision` and a nonempty
new password of at most 72 UTF-8 bytes. The browser can confirm the new password
locally; confirmation is not an API field. Plaintext passwords are never read
back, included in responses, or logged.

Password reset replaces the password and revokes **every existing target
authentication session** in the same transaction. Disabling also revokes all
target sessions, so enabling the account later requires a new login. Demotion
revokes target administrator cookie sessions; ordinary Emby sessions retain
only the permissions of the account's current role and policy.

`CurrentSessionRevoked:true` means this successful mutation revoked its own
caller. The response clears the administrator cookie and the dashboard returns
to sign-in. It must not retry the mutation or make a second authenticated logout
request using the already revoked session.

Subsequent media requests check current credentials and policy. Existing
WebSocket and conversion sessions revalidate their authorization at bounded
intervals. Original-file HTTP responses also revalidate before headers and every
five seconds, with bounded database/source checks. A definitive revocation or
source change closes the source and interrupts a blocked response write; a
transient database or storage observation preserves the existing grant. This
is periodic enforcement, not instantaneous termination at the mutation's commit.
Shutdown cancels original responses and joins their watchers before closing the
catalog. Advisory client remote-control messages are not a replacement for
credential revocation.

## Concurrency and errors

Migration `0013_managed_users.sql` adds `users.management_revision`. Each
successful profile/policy update or password reset increments the revision and
updates `updated_at`. A request with an older revision returns `409` and makes
no account, policy, password, or revocation changes. Re-read the user before
deliberately reapplying a draft. Network failure after sending a mutation has an
unknown outcome; refresh the detail instead of replaying automatically.

Management mutations serialize the last-enabled-administrator check and lock
accounts in deterministic ID order before authentication rows. The transaction
revalidates the actor's stored administrator role, disabled state, session kind,
revocation, and expiration. A stale principal passed from middleware cannot
authorize a write after its session or role has changed. Client session
operations follow the same account-before-authentication lock order. Password
hashing runs before acquiring database locks.

The last enabled administrator cannot be disabled or demoted, including through
competing requests. Native account creation also revalidates administrator
authority inside its insertion transaction.

Errors use `{Error: {Code, Message, Fields?}, RequestId}`. Field paths retain API
casing, for example `Name`, `Password`, and `Policy.EnabledFolders`.

| Status / code | Meaning |
| --- | --- |
| `400 invalid_input` | Invalid or incomplete input; field messages identify editable values. |
| `401 authentication_required` / `invalid_credentials` | Missing, revoked, expired, disabled, or demoted administrator authentication. |
| `403 csrf_invalid` / `origin_denied` | The browser request failed the existing CSRF/origin contract. |
| `404 not_found` | Target user does not exist. |
| `409 revision_conflict` | Another successful update changed the target revision. |
| `409 last_administrator` | The requested change would remove the last enabled administrator. |
| `415 unsupported_media_type` | The mutation body is not `application/json`. |

## Remaining administrator scope

This increment does not implement user deletion, unsupported user policies,
Emby user mutation adapters, device/application-key management, metadata editing,
general task scheduling, settings/diagnostic pages, audit/log browsing, or
backup/restore. They remain part of the full administrator delivery plan. The
dashboard has no consumer playback page.
