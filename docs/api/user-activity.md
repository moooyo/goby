# Recorded user activity

The administrator user list and profile sheet expose recorded sign-in activity
and current watched-item summaries. The interface does not infer presence,
individual playback history, or elapsed viewing time from these values.

## Native contract

`GET /admin/v1/users/activity?UserIds=<comma-separated IDs>` accepts between one
and 200 distinct IDs. It requires the native administrator cookie. The account
and credential are checked again inside the read transaction; an Emby token or
an application key does not authorize this endpoint. Responses are private and
are never cached.

The response contains `ObservedAt` and a non-null `Items` array. Existing
requested users have the following fields; missing users are omitted:

| Field | Meaning |
| --- | --- |
| `Id` | Exact account ID. |
| `LastLoginDate` | Most recent successfully committed administrator or Emby login, or null when no reliable record exists. |
| `LastActivityDate` | Latest sampled authenticated request for one of those logins, or null. |
| `LastPlayedDate` | Latest recorded per-item `last_played_at`, or null. This can include manually marked played state. |
| `PlayedItemCount` | Count of currently stored movie and episode items marked played, each counted once. |

The browser batches large directories without unbounded request fan-out. It
keeps the previous successful snapshot visible after a failed refresh and labels
unavailable data separately from a confirmed zero or an unknown timestamp.

## Persistence and authorization

Migration 74 creates the independent `user_activity` table and backfills only
actual retained login timestamps. Account creation is not evidence of sign-in.
Future login summaries commit atomically with credential creation and its audit
record. Session revocation and cleanup leave the account summary intact;
deleting the account removes it through its foreign key.

Emby activity uses the existing 15-second session sampling interval. Native
administrator requests use the same interval after origin and CSRF validation.
Sampling never extends credential expiry or changes the account-management
revision. Writers lock the account, then the credential, before updating the
summary, and recheck current authority before committing. Login and device
activity retain the existing device-before-summary order. No trigger regenerates
these summaries during backup restoration.

The watched count is current item state. It is not the sum of `play_count`, a
per-session history, a lifetime count of deleted media, or measured watch hours.
