# Account credentials and playback behavior

The selected phase 1 implementation uses schema 43. Its backend, migration,
recovery, administrator and supported original-client journeys have composed
results. The user accepted adapter delivery for third-party clients without
requiring resolution of original Web commercial licensing. Its blocked enabled
intro modes remain recorded limitations. See the
[execution record](../development/selected-compatibility-phase1-20260920.md).
This contract does not claim deployment or full original-client parity.

## Separate credentials and profile state

| Value | Storage and consumer |
| --- | --- |
| Account password | Existing normal authentication and administrator reset rules |
| Local password | Independent bcrypt hash; ordinary Emby login from an authorized local peer |
| Profile PIN | Four ASCII digits encrypted with authenticated AES-GCM; the authenticated original client checks its profile gate locally |

Profile PIN is not a login password. Only the authenticated owner may receive
plaintext in Emby user/configuration responses, including login and Me. Public
lists, other-user projections, native administration and application keys never
receive it. Configuration/Partial and the existing configuration write adapter
accept permitted PIN edits; null clears it. PIN and companion preferences commit
atomically. Configuration JSON never stores plaintext PINs.

PIN encryption uses the protected application-key master with a separate purpose
and account-bound authenticated data. A PIN-only installation must retain that
master even without application keys. Backup validates both credential families
and includes the required master inside the encrypted archive. Missing or wrong
key material is an error; retained ciphertext prevents silent key replacement.

## Native credential administration

| Method and route | Contract |
| --- | --- |
| GET /admin/v1/users/{id}/local-credentials | Administrator cookie; UserId, decimal-string Revision, HasLocalPassword, HasProfilePin and EnableLocalPassword |
| PUT /admin/v1/users/{id}/local-credentials | Administrator cookie and CSRF; Revision and EnableLocalPassword required; LocalPassword and ProfilePin optional |

Omitted secrets are retained; empty strings clear them. Local passwords are
bounded to 72 UTF-8 bytes. A replacement PIN requires four ASCII digits and an
existing normal password. Reads report presence only. PUT returns Credentials
and CurrentSessionRevoked; stale revision writes fail.

Local-password/enablement changes revoke affected sessions. PIN-only edits
retain sessions for the authenticated profile editor. Normal password changes
and resets clear both shortcuts, disable local-password use and revoke sessions.
Account, device and library policies remain independent.

Local-password login uses AuthenticateByName/by-ID. Source classification uses
the peer and configured GOBY_TRUSTED_PROXIES rules. Untrusted forwarded addresses
cannot establish local authority; an invalid/missing client address from a
trusted proxy fails closed. Five failed local-password attempts impose a durable
five-minute block. Such sessions remain local-bound on every resolution and
revalidation, even if their bearer token is later presented remotely.

## Intro intervals

| Method and route | Contract |
| --- | --- |
| GET /admin/v1/items/{id}/intro | Administrator; source, effective/chapter/override intervals and stale-override status |
| PUT /admin/v1/items/{id}/intro | Revision, SourceRevision, StartTicks, EndTicks and Manual/Import provenance |
| DELETE /admin/v1/items/{id}/intro | Revision and SourceRevision; reset preserves an incremented revision tombstone |

Intervals apply to indexed Movie/Episode sources in 100 ns ticks and require
0 <= start < end <= duration. An absent override row has revision 0.
SourceRevision incorporates root binding, file identity and probe facts; it is
an invalidation identity, not a credential. Replacement/reprobe cannot transfer
an old override. Stale overrides stay inspectable, while current explicit
chapters may supply the effective interval. Changed unrescanned media is unavailable.

Only an unambiguous IntroStart/IntroEnd chapter pair supplies an automatic
interval. An ordinary Opening chapter is not detection; content detection stays
deferred. Effective SDK MarkerType entries appear consistently in item Chapters
and PlaybackInfo MediaSource.Chapters. IntroSkipMode accepts None, ShowButton
and AutoSkip. The client owns the seek; the server does not also rewrite the
requested start or playback report.

## Next episode

EnableNextEpisodeAutoPlay is writable and projected to its client consumer.
The observed original-client Episodes request with IsMissing=false and
IsVirtualUnaired=false, without pagination, receives the complete authorized
playable queue up to 1,000 items. Larger queues return 422 instead of truncation.
Explicit pagination retains its existing contract. NextUp supplies ordering and
authorization predicates, while played episodes remain available for explicit
replay. Reading a queue creates no playback grant or user-history mutation;
each playback request rechecks current source and authority.

## Free feature licensing

GET /emby/Registrations/{Feature} and its namespace aliases return Goby's local
declaration: Name, IsRegistered=true, IsTrial=false and the non-expiring sentinel
ExpirationDate=9999-12-31T23:59:59Z. Authentication is required. This is neither an
Emby purchase nor a capability list or user permission grant. Excluded DVR/DLNA
and other unavailable operations remain unavailable. The adapter does not
redirect the modern original Web client's hardcoded external registration
service; that dependency retains its separate browser acceptance result.
