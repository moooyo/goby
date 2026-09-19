# Bounded user copying

Phase 3 extends the selected copy facets with source-bound acceptance in the
[phase 3 record](amd-media-phase3-20260919.md).
The current contract below includes independent entity state and the added
language preferences. Earlier user-copy evidence does not automatically accept
these extensions.

`POST /Users/New` accepts `Name`, `CopyFromUserId`, and `UserCopyOptions` from
`CreateUserByName` in the pinned [SDK snapshot](../sources/emby-sdk-openapi.snapshot.json).
The `Library.UserCopyOptions` enumeration contains exactly `UserPolicy`,
`UserConfiguration`, and `UserData`. These names are case-sensitive; duplicate
or unknown selections are rejected. A nonempty selection requires a source ID.

The snapshot does not declare a default selection, and the repository has no
reference capture establishing one. Goby's explicit behavior is that omitted or
empty options copy no facets. A supplied source ID must still identify an
existing account. The operation returns `200` with the new `UserDto`.

| Selection | Copied state |
| --- | --- |
| `UserPolicy` | The supported, validated `identity.ManagedPolicy` configuration fields, including library restrictions, content restrictions, devices, schedules, playback capabilities, and stream limits. |
| `UserConfiguration` | The supported client preference fields listed below. |
| `UserData` | Public item state: identity, playback position, play count, favorite/played state, last played date, Rating, Likes and HideFromResume. Independent entity state copies its entity identity, zero position, play count, favorite/played state, date, Rating and Likes. New rows receive fresh modification timestamps. |

Every result is a new enabled, passwordless member with a fresh password hash.
Administrator status, disabled state, lockout state, login failure counts,
passwords, PINs, tokens, sessions, device history, active playback, application
keys, collection ownership, sharing grants, avatars/artwork and DisplayPreferences
are not copied. Private remembered source/stamp/audio/subtitle selection fields
are not copied. Unknown policy
and configuration properties are excluded. Copying a source administrator does
not promote the new account; an administrator must set a password and use the
separate policy operation to promote it.

The supported `UserConfiguration` fields are `AudioLanguagePreference`,
`SubtitleLanguagePreference`, `DisplayMissingEpisodes`,
`EnableLocalPassword`, `EnableNextEpisodeAutoPlay`, `HidePlayedInLatest`,
`HidePlayedInMoreLikeThis`, `HidePlayedInSuggestions`, `IntroSkipMode`,
`LatestItemsExcludes`, `MyMediaExcludes`, `OrderedViews`, `PlayDefaultAudioTrack`,
`RememberAudioSelections`, `RememberSubtitleSelections`, `ResumeRewindSeconds`,
and `SubtitleMode`. Their source types and values must be valid. The language
fields use the same bounded language-tag validation as preferences;
adding them does not widen the unknown-field allowlist. The separate
opaque `UserSettings` document is not a `UserConfiguration` object and is not
copied by this selection.

Copying requires a current native or Emby administrator user session. The
transaction acquires the existing managed-user advisory lock, locks the actor
and source accounts in identifier order, and rechecks the administrator's
credential and current policy. Referenced media items are locked before entity
foreign-key/state locks. Public item and entity state copy in the same
transaction. Normal source-state writes use account locks and cannot change
the copied data during the operation. Authorization is checked again after
audit persistence and before commit. An invalid source, duplicate destination
name, copy failure, or expired/revoked actor leaves no new account or partial
media state.

`UserData` copying is limited to 100,000 combined persisted item and entity rows per request; an
oversized source is rejected atomically rather than truncated. Configuration
documents are limited to 1 MiB and 256 properties, and each supported view list
is limited to 1,024 identifiers. Existing policy bounds remain in force.

Test source covers explicit facet combinations, enum validation, secret exclusion,
source preservation, stale authority, rollback after a business failure, and
the HTTP response contract. Test presence alone is not executed acceptance;
consult the source-bound [phase 3 results](amd-media-phase3-results-20260919.json)
for completed scopes and the recorded server/client/recovery boundaries.
