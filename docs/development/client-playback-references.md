# Client-generated playback references

Universal-audio clients may generate a PlaySessionId for a stream URL without
calling PlaybackInfo first. Goby binds that reference to a canonical `play_...`
identity instead of treating the client value as a globally unique credential.
Migration `0012` introduces this binding without rewriting existing playback or
encoding-job identifiers.

## Scope and creation

The unique reference scope is `(user, authentication session, device, client
nonce)`. Each binding targets one internal play and one item/media source.
Different authentication sessions can use the same nonce independently. Different
nonces can describe separate concurrent plays of the same source, subject to the
existing activity limits.

Only the correlated preparation entry point creates a new binding. Current Emby
authentication, account policy, library access, item and source are checked in
the transaction. Reusing a nonce can refresh only the same live target. A different
resource, stopped/expired target, or tombstone cannot become a new play through
that old reference.

References are nonblank UTF-8 strings of at most 256 bytes without control
characters. The reserved `play_` namespace only resolves an existing internal ID;
it never falls back to creating a client alias. Existing historical internal IDs
retain their lookup priority. Caller-provided identifiers never widen authority.

## Resolution and lifecycle

Preparation, playback reports, Ping and owned-session lookups resolve existing
aliases through the same complete owner scope. They return canonical IDs to the
caller inside the server. HLS revisions, generated child URLs, encoding jobs and
cleanup use those canonical IDs. ActiveEncodings resolves an owned reference
before cancelling; unknown or foreign identifiers remain inert.

Default preparation and legacy reports without an explicit ID continue using
their existing default active-source session. They do not accidentally select a
correlated play. `client_correlated=false` retains the original partial unique
index; correlated rows have their own one-to-one reference identity.

Playback history remains bounded. If an old play row is removed, its binding is
retained with a NULL target while the authentication session is still valid. This
tombstone prevents an old URL from recreating a stopped play after history cleanup.
Deleting a user or authentication session cascades the associated bindings.
Preparation also prunes at most 256 bindings belonging to revoked/expired
authentication sessions per cleanup call.

The active limits remain 32 plays per authentication session and 128 per user.
Bindings, including tombstones, have separate limits of 65,536 per authentication
session and 262,144 per user. A full binding budget rejects new references while
allowing a valid existing one to be resolved. The current 30-day authentication
policy bounds the normal lifetime of an unused reference namespace.

## Persistent invariants

The database retains unique scope and one-to-one target constraints, foreign keys,
the reserved-name check, and tombstones. Mutations preserve the existing user-data-
before-play-row lock order and per-user creation-capacity lock. Creating or resolving
a reference does not report a played position, increase play count or mark media
watched. Terminal reports remain idempotent and cannot revive a session.

The [audio reference study](../research/audio-reference.md) records the vendor
guide's per-URL uniqueness rule and observed reuse failures. Goby does not copy
the sampled behavior in which reusing one reference across different sources
returned old audio bytes.
